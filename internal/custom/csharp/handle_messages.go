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
	extractor.Register("custom_csharp_nservicebus", &handleMessagesExtractor{})
}

// handleMessagesExtractor covers the IHandleMessages<T> handler convention shared
// by NServiceBus and Rebus — the two long-standing .NET service buses that route
// by the message type a handler declares. Builds on MassTransit (#4967) /
// MediatR (#4922); these buses use a single shared interface for both products.
//
// Consumers (the handle side):
//   - class XHandler : IHandleMessages<OrderPlaced>   → message handler
//   - class XHandler : IAmInitiatedBy<OrderPlaced>    → saga initiator (NServiceBus)
//
// Producers (the dispatch side):
//   - bus.Send(new ProcessOrder())    / context.Send(...)   → point-to-point command
//   - bus.Publish(new OrderPlaced())  / context.Publish(..) → fan-out event
//
// The Publish/Send producer verbs collide with MediatR + MassTransit, so this
// extractor is gated on an IHandleMessages signal (the unambiguous marker for the
// NServiceBus/Rebus family). Each message type is normalised to a task_id
// (msgbus:message:<T>) so the dispatch site and the handler converge by contract.
type handleMessagesExtractor struct{}

func (e *handleMessagesExtractor) Language() string { return "custom_csharp_nservicebus" }

var (
	// class XHandler : IHandleMessages<OrderPlaced>
	hmHandlerRe = regexp.MustCompile(
		`(?m)(?:class|record|struct)\s+(\w+)\s*(?:<[^>]*>)?\s*:\s*[^{};]*\bIHandleMessages\s*<\s*(\w+)`,
	)
	// class XSaga : IAmInitiatedBy<OrderPlaced>  (NServiceBus saga initiation)
	hmInitiatedByRe = regexp.MustCompile(
		`(?m)(?:class|record|struct)\s+(\w+)\s*(?:<[^>]*>)?\s*:\s*[^{};]*\bIAmInitiatedBy\s*<\s*(\w+)`,
	)
	// bus.Publish(new OrderPlaced()) / context.Publish(new T())
	hmPublishRe = regexp.MustCompile(
		`(?m)\b\w+\.Publish\s*(?:<[^>]*>)?\s*\(\s*new\s+(\w+)\s*[\(\{]`,
	)
	// bus.Send(new ProcessOrder()) / context.Send(new T())
	hmSendRe = regexp.MustCompile(
		`(?m)\b\w+\.Send\s*(?:<[^>]*>)?\s*\(\s*new\s+(\w+)\s*[\(\{]`,
	)
	// Signal gate: the IHandleMessages family marker.
	hmSignalRe = regexp.MustCompile(`(?m)\b(?:using\s+NServiceBus|using\s+Rebus|IHandleMessages\s*<|IAmInitiatedBy\s*<)`)
)

func (e *handleMessagesExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.nservicebus_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "nservicebus"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	if !hmSignalRe.MatchString(src) {
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

	// 1. Consumer: class XHandler : IHandleMessages<T>
	for _, idx := range hmHandlerRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[idx[2]:idx[3]]
		msgType := src[idx[4]:idx[5]]
		line := lineOf(src, idx[0])
		ent := makeEntity(className, "SCOPE.Service", "message_handler", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "nservicebus",
			"pattern_type", "message_handler",
			"message_type", msgType,
			"task_id", "msgbus:message:"+msgType,
			"edge_kind", "CONSUMES",
			"provenance", "INFERRED_FROM_HANDLE_MESSAGES",
		)
		add(ent)
	}

	// 2. Consumer: class XSaga : IAmInitiatedBy<T>
	for _, idx := range hmInitiatedByRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[idx[2]:idx[3]]
		msgType := src[idx[4]:idx[5]]
		line := lineOf(src, idx[0])
		ent := makeEntity(className, "SCOPE.Service", "saga_initiator", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "nservicebus",
			"pattern_type", "saga_initiator",
			"message_type", msgType,
			"task_id", "msgbus:message:"+msgType,
			"edge_kind", "CONSUMES",
			"provenance", "INFERRED_FROM_AM_INITIATED_BY",
		)
		add(ent)
	}

	// 3. Producer: bus.Publish(new T())
	for _, idx := range hmPublishRe.FindAllStringSubmatchIndex(src, -1) {
		msgType := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		ent := makeEntity("Publish "+msgType, "SCOPE.Operation", "publish", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "nservicebus",
			"pattern_type", "publish",
			"message_type", msgType,
			"task_id", "msgbus:message:"+msgType,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_NSERVICEBUS_PUBLISH",
		)
		add(ent)
	}

	// 4. Producer: bus.Send(new T())
	for _, idx := range hmSendRe.FindAllStringSubmatchIndex(src, -1) {
		msgType := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		ent := makeEntity("Send "+msgType, "SCOPE.Operation", "send", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "nservicebus",
			"pattern_type", "send",
			"message_type", msgType,
			"task_id", "msgbus:message:"+msgType,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_NSERVICEBUS_SEND",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
