package csharp

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_csharp_wolverine", &wolverineExtractor{})
}

// wolverineExtractor detects Wolverine cross-process message-bus usage. Unlike
// MassTransit (#4967, IConsumer<T> marker interface) and NServiceBus/Rebus
// (#4967, IHandleMessages<T> marker interface), Wolverine routes by *method
// convention*: a handler is any class with a Handle/Handles/Consume/Consumes
// method whose first parameter is the message type — there is no marker
// interface to anchor on. The producer side dispatches through IMessageBus
// (PublishAsync / InvokeAsync / SendAsync).
//
// Producers (the dispatch side):
//   - bus.PublishAsync(new OrderPlaced())   → fan-out event (PRODUCES)
//   - bus.SendAsync(new ProcessOrder())     → point-to-point command (PRODUCES)
//   - bus.InvokeAsync<TResp>(new Query())   → request/response (PRODUCES)
//
// Consumers (the consume side, convention-based):
//   - class XHandler { public void Handle(OrderPlaced msg) {} }
//   - public Task Handle(OrderPlaced msg) / static Task Handle(...)
//   - public void Consume(OrderPlaced msg)
//
// Each message type is normalised to task_id wolverine:message:<T> so the
// dispatch (PRODUCES) site and the convention handler (CONSUMES) site converge
// by message contract, exactly as the MassTransit / NServiceBus extractors do.
type wolverineExtractor struct{}

func (e *wolverineExtractor) Language() string { return "custom_csharp_wolverine" }

var (
	// bus.PublishAsync(new OrderPlaced{...}) / messageBus.PublishAsync(new T())
	wolPublishRe = regexp.MustCompile(
		`(?m)\b\w+\.PublishAsync\s*(?:<[^>]*>)?\s*\(\s*new\s+(\w+)\s*[\(\{]`,
	)
	// bus.SendAsync(new ProcessOrder{...})
	wolSendRe = regexp.MustCompile(
		`(?m)\b\w+\.SendAsync\s*(?:<[^>]*>)?\s*\(\s*new\s+(\w+)\s*[\(\{]`,
	)
	// bus.InvokeAsync<TResp>(new Query{...}) / bus.InvokeAsync(new Command())
	wolInvokeRe = regexp.MustCompile(
		`(?m)\b\w+\.InvokeAsync\s*(?:<[^>]*>)?\s*\(\s*new\s+(\w+)\s*[\(\{]`,
	)
	// Convention handler method: public Task Handle(OrderPlaced msg) { ... }
	// Matches Handle / Handles / Consume / Consumes with an explicit-typed first
	// parameter. Allows optional static / async / public / private modifiers and
	// any return type (void / Task / Task<T> / ValueTask). The first parameter's
	// declared type is the message contract.
	wolHandleMethodRe = regexp.MustCompile(
		`(?m)\b(?:public|internal|private|protected|static|async|\s)+\s*(?:void|Task|ValueTask)(?:\s*<[^>]*>)?\s+(Handle|Handles|Consume|Consumes)\s*\(\s*(?:\[[^\]]*\]\s*)*(\w+)\s+\w+`,
	)
	// Class/record declaration so a convention handler method can be attributed
	// to its enclosing handler type.
	wolClassDeclRe = regexp.MustCompile(
		`(?m)(?:public\s+|internal\s+|static\s+|sealed\s+|partial\s+)*(?:class|record|struct)\s+(\w+)`,
	)
)

func (e *wolverineExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.wolverine_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "wolverine"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	// Cheap signal gate: only pay the regex cost on files that actually touch
	// Wolverine, so unrelated C# (incl. MediatR / MassTransit Handle/Consume
	// look-alikes) is not mis-attributed.
	if !wolHasSignal(src) {
		return nil, nil
	}

	var out []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name + ":" + ent.Subtype
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, ent)
	}

	// 1. Producer: PublishAsync(new T{...}) — fan-out event
	for _, idx := range wolPublishRe.FindAllStringSubmatchIndex(src, -1) {
		msgType := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		ent := makeEntity("PublishAsync "+msgType, "SCOPE.Operation", "publish", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "wolverine",
			"pattern_type", "publish",
			"message_type", msgType,
			"task_id", "wolverine:message:"+msgType,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_WOLVERINE_PUBLISH",
		)
		add(ent)
	}

	// 2. Producer: SendAsync(new T{...}) — point-to-point command
	for _, idx := range wolSendRe.FindAllStringSubmatchIndex(src, -1) {
		msgType := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		ent := makeEntity("SendAsync "+msgType, "SCOPE.Operation", "send", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "wolverine",
			"pattern_type", "send",
			"message_type", msgType,
			"task_id", "wolverine:message:"+msgType,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_WOLVERINE_SEND",
		)
		add(ent)
	}

	// 3. Producer: InvokeAsync<TResp>(new T{...}) — request/response command
	for _, idx := range wolInvokeRe.FindAllStringSubmatchIndex(src, -1) {
		msgType := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		ent := makeEntity("InvokeAsync "+msgType, "SCOPE.Operation", "invoke", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "wolverine",
			"pattern_type", "invoke",
			"message_type", msgType,
			"task_id", "wolverine:message:"+msgType,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_WOLVERINE_INVOKE",
		)
		add(ent)
	}

	// 4. Consumer: convention Handle/Consume(T msg) method, attributed to its
	// enclosing handler class. Wolverine has no marker interface, so the unit of
	// extraction is the handler type, keyed by the message type of the handler
	// method's first parameter — the same task_id the producer carries.
	classStarts := wolClassDeclRe.FindAllStringSubmatchIndex(src, -1)
	for _, idx := range wolHandleMethodRe.FindAllStringSubmatchIndex(src, -1) {
		methodOff := idx[0]
		msgType := src[idx[4]:idx[5]]
		className := enclosingClassName(classStarts, src, methodOff)
		if className == "" {
			continue
		}
		line := lineOf(src, methodOff)
		ent := makeEntity(className, "SCOPE.Service", "handler", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "wolverine",
			"pattern_type", "handler",
			"message_type", msgType,
			"task_id", "wolverine:message:"+msgType,
			"edge_kind", "CONSUMES",
			"provenance", "INFERRED_FROM_WOLVERINE_HANDLER",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// enclosingClassName returns the name of the nearest class/record/struct
// declaration that starts before methodOff. classStarts is the ordered match
// set from wolClassDeclRe (FindAllStringSubmatchIndex), so the last declaration
// whose start offset precedes the method is the enclosing type. Returns "" when
// no class precedes the method (e.g. a top-level / file-scoped Handle).
func enclosingClassName(classStarts [][]int, src string, methodOff int) string {
	name := ""
	for _, c := range classStarts {
		if c[0] >= methodOff {
			break
		}
		name = src[c[2]:c[3]]
	}
	return name
}

// wolSignalRe gates the file: a Wolverine using-directive or an IMessageBus
// reference. This keeps the convention-based Handle/Consume verbs (also used by
// MassTransit ConsumeContext and ad-hoc code) from being mis-attributed to
// Wolverine on files that never reference it.
var wolSignalRe = regexp.MustCompile(
	`(?m)\b(?:using\s+Wolverine|IMessageBus\b|\.PublishAsync\b|\.InvokeAsync\b|\.SendAsync\b)`,
)

func wolHasSignal(src string) bool { return wolSignalRe.MatchString(src) }
