// Generic string-literal event-identity detection — GAP-005.
//
// SCOPE.EventBusEvent (event_bus_edges.go, #927) is grafel's first-class
// event node for MANAGED event buses (EventBridge/EventGrid/CloudEvents),
// keyed by a bus-prefixed <source>:<detail-type> pair. This pass
// generalizes the same idea — mint a synthetic event node from a
// string-literal event-type key and join producer↔consumer through it — to
// a plain envelope `{eventType:"X"}` carried over ANY channel (Kinesis,
// SQS, Kafka, ...) with NO managed-bus `source` concept. The cross-repo
// join key is the VERBATIM event-type string itself (deliberately not
// case-folded — see EntityKindEventType doc).
//
// Two independent sub-detectors:
//
//  1. Producer (Go + JS/TS for MVP): at a publish call-site, scan the
//     call's argument literal for a key in the allowlist {eventType, type,
//     eventName, detailType, detail-type} bound to a STRING LITERAL. The
//     precision gate is CO-LOCATION — the allowlisted key/string pair must
//     appear inside the parenthesized argument of a recognized publish
//     call (mirrors the windowed-extraction gating event_bus_edges.go uses
//     for eventType:'X' object literals, generalized to any channel and to
//     the balanced-paren argument instead of a fixed-size text window). A
//     bare string anywhere else in the file — or an allowlisted key that
//     isn't inside a publish call's argument — never mints a node.
//
//  2. Consumer — IaC event-source-mapping FilterCriteria (folds in
//     GAP-003): AWS `aws_lambda_event_source_mapping` (Terraform) and
//     serverless.yml `stream.filterPatterns` declare a `data.eventType` (or
//     bare `eventType`/`detail-type`) array of values the mapping filters
//     on. Each value mints/joins an EventType node with a SUBSCRIBES_TO
//     edge from the mapped Lambda. Generalizes the HCL event_pattern
//     parsing at event_bus_edges.go:220-264 to the FilterCriteria.Pattern
//     shape.
//
// # Scope guard
//
// Append-only — this pass never modifies or removes existing entities or
// edges, so it cannot regress the bug-rate of the surrounding pipeline, nor
// the separate SCOPE.EventBusEvent modeling (#927) which is untouched.
//
// Refs GAP-005 (design: .grafel/research/design-gap-005-event-identity.md).
package engine

import (
	"fmt"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// eventTypeKind is the SCOPE kind for the generic string-literal
// event-identity synthetic entity (GAP-005). Distinct from eventBusEventKind
// (#927, managed-bus-specific).
const eventTypeKind = "SCOPE.EventType"

// eventTypeID returns the canonical, VERBATIM (not case-folded) synthetic ID
// for an event-type string. Identical across repos/channels for cross-repo
// linking — the string itself is the wire contract.
func eventTypeID(verbatim string) string {
	return "event:type:" + verbatim
}

// eventTypeEmissionCapPerName bounds the number of PUBLISHES_TO /
// SUBSCRIBES_TO edges a single event-type string may accumulate FROM ONE
// FILE'S pass invocation (this pass runs per-file — the cap is PER-FILE, not
// corpus-wide aggregate). It is a same-file belt-and-suspenders guard so a
// hot event string touched by many producer/consumer call-sites in a single
// file cannot fan out unboundedly; it does NOT bound the group-wide total
// the way internal/links/topic_pass.go's topicEmissionCapPerName (an
// aggregate cross-repo cap on the link pass) does. Value borrowed from that
// cap for consistency, but the scope is narrower. The entity node itself is
// always minted (orphan-safe); only excess EDGES beyond the cap are dropped.
// A package-level var (not a const) so tests can lower it temporarily
// without duplicating the pass.
var eventTypeEmissionCapPerName = 1024

// applyEventTypeEdges is the entry-point pass. Append-only; runs after
// applyEventBusEdges in detector.go's Detect method.
func applyEventTypeEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	path := args.Path
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(content)

	seenEnt := map[string]bool{}
	seenEdge := map[string]bool{}
	emissionCount := map[string]int{}

	emitEventType := func(verbatim string, props map[string]string) {
		id := eventTypeID(verbatim)
		if seenEnt[id] {
			return
		}
		seenEnt[id] = true
		merged := map[string]string{
			"event_type":   verbatim,
			"pattern_type": "event_type_identity",
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		entities = append(entities, types.EntityRecord{
			Name:               id,
			Kind:               eventTypeKind,
			SourceFile:         "",
			Language:           lang,
			Properties:         merged,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	// emitEdge mints the event-type node (idempotent) and appends a
	// PUBLISHES_TO / SUBSCRIBES_TO edge, subject to the per-name fan-out cap.
	emitEdge := func(fromID, verbatim, kind string, props map[string]string) {
		if fromID == "" || verbatim == "" {
			return
		}
		emitEventType(verbatim, props)

		id := eventTypeID(verbatim)
		if emissionCount[id] >= eventTypeEmissionCapPerName {
			return
		}
		toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
		key := kind + "|" + fromID + "|" + toID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		emissionCount[id]++
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fromID,
			ToID:       toID,
			Kind:       kind,
			Properties: props,
		})
	}

	// Producer side — Go + JS/TS only for MVP.
	switch lang {
	case "go":
		applyEventTypeProducerGo(src, emitEdge)
	case "javascript", "typescript":
		applyEventTypeProducerJSTS(src, emitEdge)
	case "java":
		applyEventTypeProducerJava(src, emitEdge)
	}

	// Consumer side — IaC event-source-mapping FilterCriteria (GAP-003 fold-in).
	switch lang {
	case "hcl", "terraform":
		applyEventTypeConsumerHCL(src, emitEdge)
	}
	if strings.Contains(path, "serverless") && strings.HasSuffix(path, ".yml") {
		applyEventTypeConsumerServerlessYML(src, emitEdge)
	}
	// SAM / CloudFormation templates (the dominant IaC form) — any
	// .yaml/.yml carrying a CFN/SAM marker. Gated internally by
	// cfnTemplateGateRe so a serverless.yml (already handled above) or a
	// non-CFN yaml never mints here. Runs regardless of the engine's `lang`
	// tag for the file, matching the path-based dispatch above.
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		applyEventTypeConsumerCFN(src, emitEdge)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}
