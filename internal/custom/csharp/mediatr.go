package csharp

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_csharp_mediatr", &mediatrExtractor{})
}

// mediatrExtractor detects MediatR in-process CQRS / mediator patterns, the
// de-facto messaging backbone of modern ASP.NET Core applications.
//
// Producers (the dispatch / send side):
//   - _mediator.Send(new FooQuery(...))            → request → handler
//   - _mediator.Publish(new BarNotification(...))  → notification → handler(s)
//
// Consumers (the handler side):
//   - class XHandler : IRequestHandler<FooQuery, Result>      (request handler)
//   - class XHandler : IRequestHandler<FooCommand>            (void-request handler)
//   - class XHandler : INotificationHandler<BarNotification>  (notification handler)
//
// Pipeline:
//   - class L : IPipelineBehavior<TReq, TResp>     (cross-cutting middleware)
//
// Message contracts:
//   - class/record FooQuery : IRequest<Result>     (request message)
//   - class/record FooCommand : IRequest           (void request)
//   - class/record BarNotification : INotification  (notification message)
//
// Each request/notification name is normalised to a task_id so the dispatch
// site and the handler site converge by message type.
//
// EDGES (#6767). `Send`/`Publish` emit a real PRODUCES relationship to the
// request/notification contract they name (`Class:<T>`). Before #6767 this pass
// emitted ZERO relationships and recorded the intent as an inert `edge_kind`
// entity PROPERTY.
//
// The edge fires only when csSharedDispatchVerbOwner names THIS pass as the
// owner of the shared `.Send(new T())` / `.Publish(new T())` verbs in the file.
// MediatR is last in that precedence, so a MassTransit or NServiceBus file that
// also dispatches through IMediator yields the bus's edge and not this one —
// deliberate under-reporting in preference to the ADR-0028 §3 double edge.
//
// The handler and pipeline-behavior halves are deliberately EDGELESS.
// CONSUMES is emitted by nothing in the tree — a repo-wide fact recorded
// in ADR-0028, verified there by grep, and NOT re-verified by the tests here,
// which pin only that THIS pass emits none. The honest form needs the two-hop
// work-unit node #6741 declined to build; `task_id` remains the join key.
type mediatrExtractor struct{}

func (e *mediatrExtractor) Language() string { return "custom_csharp_mediatr" }

var (
	// _mediator.Send(new FooQuery(...))  /  Mediator.Send(new FooQuery())
	mtSendRe = regexp.MustCompile(
		`(?m)\b\w+\.Send\s*(?:<[^>]*>)?\s*\(\s*new\s+(\w+)\s*[\(\{]`,
	)
	// _mediator.Publish(new BarNotification(...))
	mtPublishRe = regexp.MustCompile(
		`(?m)\b\w+\.Publish\s*(?:<[^>]*>)?\s*\(\s*new\s+(\w+)\s*[\(\{]`,
	)
	// class XHandler : IRequestHandler<FooQuery, Result>  /  IRequestHandler<FooCommand>
	mtRequestHandlerRe = regexp.MustCompile(
		`(?m)(?:class|record|struct)\s+(\w+)\s*(?:<[^>]*>)?\s*:\s*[^{};]*\bIRequestHandler\s*<\s*(\w+)`,
	)
	// class XHandler : INotificationHandler<BarNotification>
	mtNotificationHandlerRe = regexp.MustCompile(
		`(?m)(?:class|record|struct)\s+(\w+)\s*(?:<[^>]*>)?\s*:\s*[^{};]*\bINotificationHandler\s*<\s*(\w+)`,
	)
	// class L : IPipelineBehavior<TRequest, TResponse>
	mtPipelineRe = regexp.MustCompile(
		`(?m)(?:class|record|struct)\s+(\w+)\s*(?:<[^>]*>)?\s*:\s*[^{};]*\bIPipelineBehavior\s*<`,
	)
	// class/record FooQuery : IRequest<Result>  or  : IRequest
	mtRequestMsgRe = regexp.MustCompile(
		`(?m)(?:class|record|struct)\s+(\w+)\s*(?:\([^)]*\))?\s*:\s*[^{};]*\bIRequest\b`,
	)
	// class/record BarNotification : INotification
	mtNotificationMsgRe = regexp.MustCompile(
		`(?m)(?:class|record|struct)\s+(\w+)\s*(?:\([^)]*\))?\s*:\s*[^{};]*\bINotification\b`,
	)
	// Guards so handler declarations are not mis-claimed as message contracts.
	mtRequestHandlerWordRe      = regexp.MustCompile(`\bIRequestHandler\b`)
	mtNotificationHandlerWordRe = regexp.MustCompile(`\bINotificationHandler\b`)

	// mtSignalRe gates the file (#6767). Until then this pass had NO gate at
	// all — the only one of the four C# bus passes without one — so its
	// `.Send(new T())` / `.Publish(new T())` regexes, which are byte-identical
	// to MassTransit's, fired on every C# file in the corpus that used those
	// two extremely common verb spellings. On an NServiceBus handler that
	// merely calls `context.Publish(new OrderConfirmed())` it minted a second,
	// mediatr-labelled copy of that dispatch site.
	mtSignalRe = regexp.MustCompile(
		`(?m)\b(?:using\s+MediatR|IMediator\b|IRequestHandler\s*<|INotificationHandler\s*<|` +
			`IPipelineBehavior\s*<|IRequest\b|INotification\b)`)
)

func mtHasSignal(src string) bool { return mtSignalRe.MatchString(src) }

func (e *mediatrExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.mediatr_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "mediatr"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	// Cheap signal gate: only pay the regex cost on files that actually touch
	// MediatR, so unrelated C# (incl. MassTransit- and NServiceBus-only files
	// that spell the same Send/Publish verbs) is not mis-attributed (#6767).
	if !mtHasSignal(src) {
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

	// 1. Producer: _mediator.Send(new FooQuery(...)) — request dispatch
	for _, idx := range mtSendRe.FindAllStringSubmatchIndex(src, -1) {
		msgType := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		taskID := "mediatr:request:" + msgType
		ent := makeEntity("Send "+msgType, "SCOPE.Operation", "request_dispatch", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "mediatr",
			"pattern_type", "send",
			"message_type", msgType,
			"task_id", taskID,
			"provenance", "INFERRED_FROM_MEDIATR_SEND",
		)
		if csSharedDispatchVerbOwner(src) == "mediatr" {
			ent.Relationships = append(ent.Relationships, csMessageProducesEdge(
				"mediatr", msgType, taskID, "INFERRED_FROM_MEDIATR_SEND",
			))
		}
		add(ent)
	}

	// 2. Producer: _mediator.Publish(new BarNotification(...)) — notification dispatch
	for _, idx := range mtPublishRe.FindAllStringSubmatchIndex(src, -1) {
		msgType := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		taskID := "mediatr:notification:" + msgType
		ent := makeEntity("Publish "+msgType, "SCOPE.Operation", "notification_dispatch", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "mediatr",
			"pattern_type", "publish",
			"message_type", msgType,
			"task_id", taskID,
			"provenance", "INFERRED_FROM_MEDIATR_PUBLISH",
		)
		if csSharedDispatchVerbOwner(src) == "mediatr" {
			ent.Relationships = append(ent.Relationships, csMessageProducesEdge(
				"mediatr", msgType, taskID, "INFERRED_FROM_MEDIATR_PUBLISH",
			))
		}
		add(ent)
	}

	// 3. Consumer: class XHandler : IRequestHandler<FooQuery, ...>
	for _, idx := range mtRequestHandlerRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[idx[2]:idx[3]]
		msgType := src[idx[4]:idx[5]]
		line := lineOf(src, idx[0])
		taskID := "mediatr:request:" + msgType
		ent := makeEntity(className, "SCOPE.Service", "request_handler", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "mediatr",
			"pattern_type", "request_handler",
			"message_type", msgType,
			"task_id", taskID,
			"provenance", "INFERRED_FROM_MEDIATR_REQUEST_HANDLER",
		)
		add(ent)
	}

	// 4. Consumer: class XHandler : INotificationHandler<BarNotification>
	for _, idx := range mtNotificationHandlerRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[idx[2]:idx[3]]
		msgType := src[idx[4]:idx[5]]
		line := lineOf(src, idx[0])
		taskID := "mediatr:notification:" + msgType
		ent := makeEntity(className, "SCOPE.Service", "notification_handler", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "mediatr",
			"pattern_type", "notification_handler",
			"message_type", msgType,
			"task_id", taskID,
			"provenance", "INFERRED_FROM_MEDIATR_NOTIFICATION_HANDLER",
		)
		add(ent)
	}

	// 5. Pipeline behavior: class L : IPipelineBehavior<TReq, TResp>
	for _, idx := range mtPipelineRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		ent := makeEntity(className, "SCOPE.Pattern", "pipeline_behavior", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "mediatr",
			"pattern_type", "pipeline_behavior",
			"provenance", "INFERRED_FROM_MEDIATR_PIPELINE_BEHAVIOR",
		)
		add(ent)
	}

	// 6. Message contract: class/record FooQuery : IRequest<Result>
	for _, idx := range mtRequestMsgRe.FindAllStringSubmatchIndex(src, -1) {
		msgName := src[idx[2]:idx[3]]
		// IRequestHandler also matches IRequest substring word boundary? No — \bIRequest\b
		// does not match "IRequestHandler" (Handler follows without boundary at 't'->'H'
		// there IS a boundary). Guard: skip handler/pipeline declarations.
		full := src[idx[0]:idx[1]]
		if mtRequestHandlerWordRe.MatchString(full) {
			continue
		}
		line := lineOf(src, idx[0])
		taskID := "mediatr:request:" + msgName
		ent := makeEntity(msgName, "SCOPE.Schema", "request_message", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "mediatr",
			"pattern_type", "request_message",
			"message_type", msgName,
			"task_id", taskID,
			"provenance", "INFERRED_FROM_MEDIATR_REQUEST_MESSAGE",
		)
		add(ent)
	}

	// 7. Message contract: class/record BarNotification : INotification
	for _, idx := range mtNotificationMsgRe.FindAllStringSubmatchIndex(src, -1) {
		msgName := src[idx[2]:idx[3]]
		full := src[idx[0]:idx[1]]
		if mtNotificationHandlerWordRe.MatchString(full) {
			continue
		}
		line := lineOf(src, idx[0])
		taskID := "mediatr:notification:" + msgName
		ent := makeEntity(msgName, "SCOPE.Schema", "notification_message", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "mediatr",
			"pattern_type", "notification_message",
			"message_type", msgName,
			"task_id", taskID,
			"provenance", "INFERRED_FROM_MEDIATR_NOTIFICATION_MESSAGE",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
