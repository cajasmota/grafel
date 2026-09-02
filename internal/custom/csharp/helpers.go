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
// which mints a SCOPE.ScheduledJob plus TRIGGERS, while its four ENQUEUES
// emitters are Sidekiq / Resque / RQ / asynq only (ADR-0028 §3).
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
// FUNCTION. Every site that reaches here names a class. Nothing else in the
// repo emits an edge for these four C# buses (no engine synthesis mentions
// MassTransit / MediatR / Wolverine / NServiceBus, and csharp/redis.go's
// PUBLISHES_TO requires a string-literal channel argument, so `.Publish(new T())`
// cannot reach it) — so there is no hop here for ADR-0028 §3 to double.
func csMessageProducesEdge(framework, msgType, taskID, provenance string) types.RelationshipRecord {
	return types.RelationshipRecord{
		ToID: csClassRef(msgType),
		Kind: string(types.RelationshipKindProduces),
		// types.Props is binary-searched, so the keys must stay sorted.
		Properties: types.Props{
			{K: "framework", V: framework},
			{K: "message_type", V: msgType},
			{K: "provenance", V: provenance},
			{K: "task_id", V: taskID},
		},
	}
}
