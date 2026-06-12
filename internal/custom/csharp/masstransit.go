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
	extractor.Register("custom_csharp_masstransit", &massTransitExtractor{})
}

// massTransitExtractor detects MassTransit cross-process message-bus usage —
// the dominant .NET service bus (RabbitMQ / Azure Service Bus / Amazon SQS
// transports). Builds on the MediatR (#4922) in-process pattern; these are the
// cross-process publish/send + consume sites that converge by message type.
//
// Producers (the dispatch side):
//   - _publishEndpoint.Publish(new OrderSubmitted{ ... })  → fan-out event
//   - _sendEndpoint.Send(new ProcessOrder{ ... })          → point-to-point command
//   - bus.Publish(new OrderSubmitted())                    → fan-out event
//   - context.Publish(new OrderSubmitted())                → from inside a consumer
//   - context.Send(new ProcessOrder())                     → from inside a consumer
//
// Consumers (the consume side):
//   - class XConsumer : IConsumer<OrderSubmitted>          → message consumer
//
// Sagas / state machines (orchestration):
//   - class XSaga : ISaga, InitiatedBy<T> / Orchestrates<T> / Observes<T>
//   - class XStateMachine : MassTransitStateMachine<TState>
//
// Each message type is normalised to a task_id (masstransit:message:<T>) so the
// publish/send (PRODUCES) site and the consumer (CONSUMES) site converge by
// message contract, exactly as the MediatR extractor converges by task_id.
type massTransitExtractor struct{}

func (e *massTransitExtractor) Language() string { return "custom_csharp_masstransit" }

var (
	// _x.Publish(new OrderSubmitted{...}) / bus.Publish(new T()) / context.Publish(new T())
	mxPublishRe = regexp.MustCompile(
		`(?m)\b\w+\.Publish\s*(?:<[^>]*>)?\s*\(\s*new\s+(\w+)\s*[\(\{]`,
	)
	// _x.Send(new ProcessOrder{...}) / context.Send(new T())
	mxSendRe = regexp.MustCompile(
		`(?m)\b\w+\.Send\s*(?:<[^>]*>)?\s*\(\s*new\s+(\w+)\s*[\(\{]`,
	)
	// class XConsumer : IConsumer<OrderSubmitted>
	mxConsumerRe = regexp.MustCompile(
		`(?m)(?:class|record|struct)\s+(\w+)\s*(?:<[^>]*>)?\s*:\s*[^{};]*\bIConsumer\s*<\s*(\w+)`,
	)
	// class OrderSaga : ISaga ...  (saga persistence + lifecycle correlation)
	mxSagaRe = regexp.MustCompile(
		`(?m)(?:class|record|struct)\s+(\w+)\s*(?:<[^>]*>)?\s*:\s*[^{};]*\bISaga\b`,
	)
	// class OrderStateMachine : MassTransitStateMachine<OrderState>
	mxStateMachineRe = regexp.MustCompile(
		`(?m)(?:class|record|struct)\s+(\w+)\s*(?:<[^>]*>)?\s*:\s*[^{};]*\bMassTransitStateMachine\s*<\s*(\w+)`,
	)
)

func (e *massTransitExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.masstransit_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "masstransit"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	// Cheap signal gate: only pay the regex cost on files that actually touch
	// MassTransit, so unrelated C# (incl. MediatR Send/Publish) is not scanned.
	if !mxHasSignal(src) {
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

	// 1. Producer: Publish(new T{...}) — fan-out event
	for _, idx := range mxPublishRe.FindAllStringSubmatchIndex(src, -1) {
		msgType := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		ent := makeEntity("Publish "+msgType, "SCOPE.Operation", "publish", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "masstransit",
			"pattern_type", "publish",
			"message_type", msgType,
			"task_id", "masstransit:message:"+msgType,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_MASSTRANSIT_PUBLISH",
		)
		add(ent)
	}

	// 2. Producer: Send(new T{...}) — point-to-point command
	for _, idx := range mxSendRe.FindAllStringSubmatchIndex(src, -1) {
		msgType := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		ent := makeEntity("Send "+msgType, "SCOPE.Operation", "send", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "masstransit",
			"pattern_type", "send",
			"message_type", msgType,
			"task_id", "masstransit:message:"+msgType,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_MASSTRANSIT_SEND",
		)
		add(ent)
	}

	// 3. Consumer: class XConsumer : IConsumer<T>
	for _, idx := range mxConsumerRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[idx[2]:idx[3]]
		msgType := src[idx[4]:idx[5]]
		line := lineOf(src, idx[0])
		ent := makeEntity(className, "SCOPE.Service", "consumer", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "masstransit",
			"pattern_type", "consumer",
			"message_type", msgType,
			"task_id", "masstransit:message:"+msgType,
			"edge_kind", "CONSUMES",
			"provenance", "INFERRED_FROM_MASSTRANSIT_CONSUMER",
		)
		add(ent)
	}

	// 4. Saga: class XSaga : ISaga (orchestration / correlated lifecycle)
	for _, idx := range mxSagaRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		ent := makeEntity(className, "SCOPE.Service", "saga", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "masstransit",
			"pattern_type", "saga",
			"edge_kind", "CONSUMES",
			"provenance", "INFERRED_FROM_MASSTRANSIT_SAGA",
		)
		add(ent)
	}

	// 5. State machine: class X : MassTransitStateMachine<TState>
	for _, idx := range mxStateMachineRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[idx[2]:idx[3]]
		stateType := src[idx[4]:idx[5]]
		line := lineOf(src, idx[0])
		ent := makeEntity(className, "SCOPE.Service", "state_machine", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "masstransit",
			"pattern_type", "state_machine",
			"message_type", stateType,
			"edge_kind", "CONSUMES",
			"provenance", "INFERRED_FROM_MASSTRANSIT_STATE_MACHINE",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// mxSignalRe gates the file: a MassTransit using-directive, an IConsumer<>/ISaga/
// MassTransitStateMachine declaration, or an endpoint type. This keeps the shared
// Publish/Send verbs (also used by MediatR) from being mis-attributed to
// MassTransit on files that never reference it.
var mxSignalRe = regexp.MustCompile(
	`(?m)\b(?:using\s+MassTransit|IConsumer\s*<|ISaga\b|MassTransitStateMachine\s*<|IPublishEndpoint|ISendEndpoint|ConsumeContext\s*<)`,
)

func mxHasSignal(src string) bool { return mxSignalRe.MatchString(src) }
