// Package csharp provides regex-based custom extractors for C# source files.
// Each extractor targets a specific framework and registers via init().
package csharp

import (
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

func makeEntity(name, kind, subtype, filePath, language string, lineNum int) types.EntityRecord {
	e := types.EntityRecord{
		Name:             name,
		Kind:             kind,
		Subtype:          subtype,
		SourceFile:       filePath,
		StartLine:        lineNum,
		EndLine:          lineNum,
		Language:         language,
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
		Properties: map[string]string{
			"kind":    kind,
			"subtype": subtype,
		},
	}
	e.ID = e.ComputeID()
	return e
}

func setProps(e *types.EntityRecord, kv ...string) {
	if len(kv)%2 != 0 {
		return
	}
	for i := 0; i < len(kv); i += 2 {
		e.Properties[kv[i]] = kv[i+1]
	}
}

// csClassRef builds the resolvable class reference `Class:<Name>` used as the
// TO endpoint of an edge whose target type is declared in ANOTHER file. This is
// the established C# cross-file convention, not a new one: object_mapping.go's
// MAPS_TO edges address both of their endpoints this way, and the resolver's
// by-name pass binds the stub to the separately-extracted class entity wherever
// it is declared. `java/quartz.go` reaches its cross-file job class through the
// exact same shape (javaClassRef, #6741 arm 2).
//
// The address is file-independent by design: a same-file declaration resolves
// through the same lookup, so a caller never needs a second, same-file-only
// edge for the same hop (ADR-0028 §3 — one hop, one pair).
func csClassRef(className string) string { return "Class:" + className }

// csJobProducesEdge builds the PRODUCES relationship a background-job producer
// carries to the job type it dispatches (#6741 arm 3, ADR-0028).
//
// Shape: the one-hop degenerate form ADR-0028 §1 blesses — the framework
// producer entity → the job class, carrying the `task:<framework>:<id>` address
// as a property rather than minting a separate work-unit node. It is the form
// java/quartz.go already ships, kept here for consistency across the two
// languages that emit this edge.
//
// This SUPPLEMENTS the CALLS edge the generic C# extractor derives from the
// dispatch lambda; it never suppresses it (ADR-0028 §2). And it is not a second
// ENQUEUES: the C# job path emits none — internal/engine/scheduled_jobs_edges.go
// routes Hangfire recurring jobs through synthesizeCSharpHangfireRecurring,
// which mints a SCOPE.ScheduledJob plus TRIGGERS, while its five ENQUEUES
// emitters are Sidekiq / Resque / RQ / asynq / dramatiq only — none of them C#
// (ADR-0028 §3; dramatiq is the fifth, added by #6741 arm 4, and #6767's
// dramatiq decision rests on it).
func csJobProducesEdge(framework, jobType, taskID, provenance string) types.RelationshipRecord {
	return types.RelationshipRecord{
		ToID: csClassRef(jobType),
		Kind: string(types.RelationshipKindProduces),
		// types.Props is binary-searched, so the keys must stay sorted.
		Properties: types.Props{
			{K: "framework", V: framework},
			{K: "job_class", V: jobType},
			{K: "provenance", V: provenance},
			{K: "task_id", V: taskID},
		},
	}
}

// csharpPrimitives are C# built-in types that should not be emitted as schema entities.
var csharpPrimitives = map[string]bool{
	"string": true, "int": true, "long": true, "double": true, "float": true,
	"bool": true, "char": true, "byte": true, "short": true, "void": true,
	"object": true, "decimal": true, "uint": true, "ulong": true,
	"String": true, "Int32": true, "Int64": true, "Boolean": true,
	"IActionResult": true, "ActionResult": true, "Task": true,
	"IEnumerable": true, "List": true, "IList": true, "Array": true,
	"Ok": true, "NotFound": true, "BadRequest": true, "Unauthorized": true,
}

// csMessageProducesEdge builds the PRODUCES relationship a MESSAGE-BUS dispatch
// site carries to the message contract it dispatches (#6767).
//
// Sibling of csJobProducesEdge, and the same one-hop degenerate form ADR-0028
// §1 blesses: the producer entity → the type the call site names, carrying the
// framework's `task_id` address as a property rather than minting a work-unit
// node. It differs only in what the target IS — a message/event contract class
// (`new OrderSubmitted{...}`) rather than a job class — which is recorded as
// `message_type` instead of `job_class` so a reader can tell the two hops apart
// without consulting the framework property.
//
// Why PRODUCES and not ENQUEUES: #6741's split is by TARGET SHAPE, not by
// framework — PRODUCES for dispatch onto a CLASS, ENQUEUES for dispatch onto a
// FUNCTION. Every site that reaches here names a class.
//
// Its five callers are masstransit.go (publish, send), mediatr.go (send,
// publish), handle_messages.go (publish, send), wolverine.go (publishAsync,
// sendAsync, invokeAsync) and coravel.go (mail).
//
// ADR-0028 §3 — WHAT CAN DOUBLE THIS EDGE, and what cannot. No ENGINE pass can:
// no synthesis in internal/engine mentions MassTransit / MediatR / Wolverine /
// NServiceBus / Coravel (zero hits), and csharp/redis.go's PUBLISHES_TO needs a
// string-literal channel argument, so `.Publish(new T())` cannot reach it.
//
// The passes double EACH OTHER, though, and that is the live hazard. `.Publish(
// new T())` and `.Send(new T())` are spelled identically by MassTransit,
// MediatR and NServiceBus/Rebus — mtSendRe/mtPublishRe and mxSendRe/mxPublishRe
// are byte-identical — and Coravel's mailer reaches the same `.Send(new T())`
// bytes for a `Mailable`-suffixed type. Signal gates alone do NOT settle it: a
// MassTransit consumer that dispatches through IMediator passes BOTH gates
// honestly, and before #6767 that produced two PRODUCES edges for one hop.
// csSharedDispatchVerbOwner is the arbiter; see its doc for the precedence.
//
// taskID may be EMPTY, and then no `task_id` property is written. That is not a
// convenience: `task_id` is a JOIN KEY, and a key nothing else mints is not a
// key. The four buses each have a consumer entity carrying the same
// `<framework>:message:<T>` address, so their edge carries it; Coravel's mailer
// has no Mailable-side entity to join with, so it carries none rather than
// inventing a `mail:coravel:` namespace with one end.
func csMessageProducesEdge(framework, msgType, taskID, provenance string) types.RelationshipRecord {
	// types.Props is binary-searched, so the keys must stay sorted.
	props := types.Props{
		{K: "framework", V: framework},
		{K: "message_type", V: msgType},
		{K: "provenance", V: provenance},
	}
	if taskID != "" {
		props = append(props, types.PropKV{K: "task_id", V: taskID})
	}
	return types.RelationshipRecord{
		ToID:       csClassRef(msgType),
		Kind:       string(types.RelationshipKindProduces),
		Properties: props,
	}
}

// csSharedDispatchVerbOwner names the ONE pass allowed to emit a PRODUCES edge
// for the shared `.Publish(new T())` / `.Send(new T())` dispatch verbs in this
// file (#6767).
//
// The problem it solves is measured, not theoretical. MassTransit, MediatR and
// NServiceBus/Rebus spell these two verbs identically — mxSendRe and mtSendRe
// are the same regex, byte for byte — and Coravel's mailer matches the same
// `.Send(new T())` bytes whenever the type ends in `Mailable`. Until #6767 that
// only minted duplicate INERT entities; the moment those entities carry an
// edge, one queue hop gets two PRODUCES relationships, which is exactly the
// double ADR-0028 §3 forbids and which this whole issue exists to remove.
//
// A signal gate per pass is NECESSARY BUT NOT SUFFICIENT, and it is worth being
// precise about why, because "just gate MediatR" is the obvious fix and it does
// not work: a MassTransit consumer that dispatches through `IMediator` — an
// ordinary .NET shape — satisfies BOTH gates truthfully, so both passes still
// fire on both call sites. Only a single owner settles it.
//
// PRECEDENCE, most specific marker first:
//
//  1. NServiceBus/Rebus — IHandleMessages<> / IAmInitiatedBy<> are that
//     family's and nothing else's.
//  2. MassTransit — IConsumer<> / ISaga / MassTransitStateMachine<> /
//     IPublishEndpoint / ISendEndpoint / ConsumeContext<>.
//  3. MediatR — IMediator / IRequest* / INotification* / using MediatR. Last of
//     the three because its markers are the most generic English.
//  4. Coravel's mailer defers to all three (see coravel.go): a `Mailable` in a
//     file that is unambiguously a service-bus surface is that bus's message.
//
// This deliberately UNDER-reports on a genuinely mixed file: the MediatR hop in
// a MassTransit consumer gets no PRODUCES edge. That is the repo's standing
// preference — the same call synthesizeDramatiqSendEdges makes with its
// evidence guard — because an absent edge is a gap a reader can see, while a
// doubled edge silently corrupts every traversal that counts hops.
func csSharedDispatchVerbOwner(src string) string {
	switch {
	case hmSignalRe.MatchString(src):
		return "nservicebus"
	case mxSignalRe.MatchString(src):
		return "masstransit"
	case mtSignalRe.MatchString(src):
		return "mediatr"
	}
	return ""
}
