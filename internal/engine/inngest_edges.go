// Inngest producer → event-topic attribution — #5482 (epic #5479, Phase 2).
//
// Inngest is an event-driven durable-function platform for Node/JS-TS. Code
// emits an event with `inngest.send({ name: "user/created", … })` or, inside a
// step function, `step.sendEvent("id", { name: "user/created" })`. That event
// NAME is the cross-process rendezvous point: the `inngest.send` producer and
// the `inngest.createFunction({ id, event: "user/created" }, …)` consumer are
// talking over the same logical topic.
//
// Phase 1 of the epic already landed:
//   - #5480 — the consumer SCOPE.Function entity per createFunction call.
//   - #5481 — one SCOPE.MessageTopic entity per DISTINCT event name, keyed by
//     Name = the bare event name (e.g. "user/created") with a
//     `topic_id = "event:<name>"` property, emitted by the custom JS extractor
//     internal/custom/javascript/inngest.go.
//
// This pass (#5482) wires the producer side: for each `inngest.send` /
// `sendEvent` call, it emits a PUBLISHES_TO edge
//
//	PUBLISHES_TO  enclosing fn → SCOPE.MessageTopic:<event-name>
//
// reusing the SAME PUBLISHES_TO edge kind the Kafka / BullMQ / RabbitMQ
// producer passes already emit, so the cross-repo topic linker
// (internal/links/topic_pass.go, P7) and the dashboard topology/flows panels
// understand it with no new code. The edge's ToID is the `Kind:Name` stub
// `SCOPE.MessageTopic:<name>`, which the reference resolver binds to the
// topic entity #5481 created — this pass never re-emits the topic, it only
// adds the producer edge.
//
// Enclosing-scope resolution mirrors the Kafka Node producer pass: the
// enclosing function/handler/route name is resolved via findEnclosingNodeName,
// which already falls back to the synthetic "module" scope when no enclosing
// function is found within its lookback window — so an `inngest.send` at module
// top level (or one whose enclosing scope cannot be resolved) still attributes
// to `Function:module` rather than dropping the edge.
//
// Append-only — never modifies or removes existing entities or edges, so this
// pass cannot regress the surrounding pipeline's bug-rate. Refs #5482, #5479.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// inngestTopicEntityKind is the entity kind the #5481 custom extractor stamps
// for each Inngest event topic. The producer edge's ToID resolves against it by
// `Kind:Name`, so this pass must reference the identical kind.
const inngestTopicEntityKind = "SCOPE.MessageTopic"

// inngestEdgeSupportsLanguage reports whether applyInngestEdges can emit edges
// for `lang`. Inngest is a Node library, so only JS/TS qualify.
func inngestEdgeSupportsLanguage(lang string) bool {
	switch lang {
	case "javascript", "typescript":
		return true
	default:
		return false
	}
}

// inngestEdgeStr matches a single-, double-, or back-quoted string literal,
// capturing the inner value. Kept consistent with the custom extractor.
const inngestEdgeStr = "['\"`]([^'\"`]+)['\"`]"

var (
	// Gate: only run when the file actually imports / requires inngest, so a
	// stray `.send(` from another library is not misattributed. Mirrors the
	// custom extractor's reInngestImport.
	reInngestEdgeImport = regexp.MustCompile("(?:from\\s+['\"`]inngest['\"`]|require\\(\\s*['\"`]inngest['\"`]\\s*\\))")

	// `<recv>.send(` / `<recv>.sendEvent(` — the producer side. Capture the
	// receiver so the same attribution gate (import present, or receiver
	// named/ending in `inngest`, or the step object) can be applied.
	reInngestEdgeSend = regexp.MustCompile(`([A-Za-z_$][A-Za-z0-9_$.]*)\.(?:send|sendEvent)\s*\(`)

	// Event payload `name: "..."` key inside a send() call. An array of
	// payloads yields one match per `name:` key in the bounded send region.
	reInngestEdgeNameKey = regexp.MustCompile(`\bname\s*:\s*` + inngestEdgeStr)
)

// inngestTopicToID returns the `Kind:Name` ToID stub for the event topic the
// #5481 extractor created. The reference resolver binds it to that entity.
func inngestTopicToID(name string) string {
	return fmt.Sprintf("%s:%s", inngestTopicEntityKind, name)
}

// inngestSendReceiverAttributed reports whether a `<receiver>.send(` /
// `.sendEvent(` call should be attributed to Inngest. Accept when the file
// imports inngest, or the receiver is the conventional `inngest` client (or a
// member access ending in `.inngest`), or it is the `step` object used inside a
// createFunction handler for `step.sendEvent(...)`.
func inngestSendReceiverAttributed(receiver string, hasImport bool) bool {
	if hasImport {
		return true
	}
	if receiver == "inngest" || strings.HasSuffix(receiver, ".inngest") {
		return true
	}
	if receiver == "step" || strings.HasSuffix(receiver, ".step") {
		return true
	}
	return false
}

// applyInngestEdges APPENDS PUBLISHES_TO edges from the enclosing scope of each
// `inngest.send` / `sendEvent` call to the event topic entity (#5481).
// Append-only.
func applyInngestEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	if !inngestEdgeSupportsLanguage(lang) {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(content)

	// Fast pre-filter: a file with no inngest reference and no `.send` /
	// `.sendEvent` call cannot produce an Inngest producer edge.
	if !strings.Contains(src, "inngest") && !strings.Contains(src, "Inngest") {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	if !strings.Contains(src, ".send") {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	hasImport := reInngestEdgeImport.MatchString(src)

	seenEdge := map[string]bool{}
	emitEdge := func(caller, eventName string) {
		if caller == "" || eventName == "" {
			return
		}
		toID := inngestTopicToID(eventName)
		fromID := fmt.Sprintf("Function:%s", caller)
		key := fromID + "|" + toID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: fromID,
			ToID:   toID,
			Kind:   publishesToEdgeKind,
			Properties: map[string]string{
				"framework":       "inngest",
				"messaging_layer": "inngest",
				"event":           eventName,
				"topic_id":        "event:" + eventName,
				"provenance":      "INFERRED_FROM_INNGEST_SEND",
			},
		})
	}

	enclosing := func(offset int) string { return findEnclosingNodeName(src, offset) }

	// Producer side: `<client>.send({ name: "..." })` / `.sendEvent(...)`.
	// The same attribution gate as the custom extractor applies. An array of
	// payloads yields one PUBLISHES_TO per distinct `name:` key in the bounded
	// send() region.
	for _, m := range reInngestEdgeSend.FindAllStringSubmatchIndex(src, -1) {
		receiver := src[m[2]:m[3]]
		if !inngestSendReceiverAttributed(receiver, hasImport) {
			continue
		}
		seg := boundedInngestCallSegment(src, m[1]-1) // m[1]-1 is the '(' offset
		caller := enclosing(m[0])
		for _, nm := range reInngestEdgeNameKey.FindAllStringSubmatch(seg, -1) {
			emitEdge(caller, nm[1])
		}
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// boundedInngestCallSegment returns the source substring from openParen (the
// byte offset of a '(') to its matching ')', inclusive, capped to a sane length
// so a malformed/unterminated call cannot scan the whole file. Mirrors the
// bounded-segment helper in the custom extractor.
func boundedInngestCallSegment(src string, openParen int) string {
	if openParen < 0 || openParen >= len(src) || src[openParen] != '(' {
		return ""
	}
	depth := 0
	const maxScan = 4000
	for i := openParen; i < len(src) && i < openParen+maxScan; i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[openParen : i+1]
			}
		}
	}
	if openParen+maxScan < len(src) {
		return src[openParen : openParen+maxScan]
	}
	return src[openParen:]
}
