// NATS publish/subscribe detection — wave 3 of #726.
//
// For every NATS publish or subscribe call site this pass can statically
// recognize, we emit a synthetic `SCOPE.Queue` entity keyed by the subject
// name, plus PUBLISHES_TO or SUBSCRIBES_TO edges. The synthetic subject ID
// is identical across repos (`nats:<subject>`), so the existing import-
// channel linker matches producer and consumer sides on shared entity ID
// without any new cross-repo matching code — same technique used by the
// other wave-1/2 broker passes.
//
// Libraries/frameworks covered:
//   - Go nats.go: nc.Publish(subject, data), nc.Subscribe(subject, handler),
//     nc.QueueSubscribe(subject, queue, handler), nc.Request(subject, data, timeout)
//   - Node nats.js: nc.publish(subject, data), sub = nc.subscribe(subject);
//     for await (const m of sub), nc.request(subject, data)
//   - Python nats.py: await nc.publish(subject, payload),
//     await nc.subscribe(subject, cb=handler)
//   - Java: connection.publish(subject, data),
//     subscription = connection.subscribe(subject),
//     dispatcher.subscribe(subject, handler)
//   - NATS JetStream: js.publish(subject, ...), js.subscribe(subject, ...)
//     — emits jetstream=true property
//   - NATS Streaming (STAN, deprecated): stan.Publish / stan.QueueSubscribe
//     — emits nats_streaming=true property
//   - request/reply pattern: nc.Request / nc.request / nc.request(...)
//     — emits pattern=request_reply property
//
// Subjects with wildcards (orders.*, inventory.>) are valid subscriber-side;
// emitted as-is.
//
// Emit: SCOPE.Queue (reuses queueEntityKind) + PUBLISHES_TO + SUBSCRIBES_TO.
// ID format: nats:<subject>.
//
// Refs #726.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// natsSynthesisSupportsLanguage reports whether applyNATSEdges can emit
// synthetics for `lang`.
func natsSynthesisSupportsLanguage(lang string) bool {
	switch lang {
	case "java", "kotlin", "javascript", "typescript", "python", "go":
		return true
	default:
		return false
	}
}

// applyNATSEdges runs after applyPubSubEdges and APPENDS SCOPE.Queue
// entities + PUBLISHES_TO / SUBSCRIBES_TO edges for NATS.
// Append-only — never modifies or removes existing entities or edges, so
// this pass cannot regress the surrounding pipeline's bug-rate.
func applyNATSEdges(
	lang string,
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if len(content) == 0 {
		return entities, relationships
	}
	if !natsSynthesisSupportsLanguage(lang) {
		return entities, relationships
	}

	src := string(content)

	// Dedup-by-ID: one SCOPE.Queue entity per subject per file.
	seenQueue := map[string]bool{}
	seenEdge := map[string]bool{}

	emitSubject := func(subjectID, subject string, props map[string]string) {
		if seenQueue[subjectID] {
			return
		}
		seenQueue[subjectID] = true
		merged := map[string]string{
			"broker":       "nats",
			"subject":      subject,
			"pattern_type": "nats_synthesis",
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		// SourceFile left empty so identical subjects collapse to ONE entity
		// per repo and match across repos via the import-channel linker.
		entities = append(entities, types.EntityRecord{
			Name:               subjectID,
			Kind:               queueEntityKind,
			SourceFile:         "",
			Language:           lang,
			Properties:         merged,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	emitEdge := func(callerKind, callerName, subjectID, edgeKind string, props map[string]string) {
		if callerName == "" || subjectID == "" {
			return
		}
		key := edgeKind + "|" + callerKind + ":" + callerName + "|" + subjectID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		base := map[string]string{
			"broker":       "nats",
			"pattern_type": "nats_synthesis",
		}
		for k, v := range props {
			if v != "" {
				base[k] = v
			}
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fmt.Sprintf("%s:%s", callerKind, callerName),
			ToID:       fmt.Sprintf("%s:%s", queueEntityKind, subjectID),
			Kind:       edgeKind,
			Properties: base,
		})
	}

	switch lang {
	case "python":
		synthesizePyNATS(src, emitSubject, emitEdge)
	case "javascript", "typescript":
		synthesizeNodeNATS(src, emitSubject, emitEdge)
	case "java", "kotlin":
		synthesizeJavaNATS(src, emitSubject, emitEdge)
	case "go":
		synthesizeGoNATS(src, emitSubject, emitEdge)
	}

	return entities, relationships
}

// natsSubjectID returns the canonical synthetic ID for a NATS subject.
// Wildcard subjects (orders.*, inventory.>) are preserved as-is.
func natsSubjectID(subject string) string {
	return "nats:" + subject
}

// looksLikeNATSSubject returns true when `s` plausibly looks like a NATS
// subject. NATS subjects are dot-separated tokens; * and > are wildcards
// allowed in subscriber patterns. The NATS wildcard `>` must not be
// confused with the HTML `>` character — we accept it here because NATS
// subjects are extracted from string literals, not HTML.
func looksLikeNATSSubject(s string) bool {
	if s == "" || len(s) > 256 {
		return false
	}
	if strings.ContainsAny(s, "\n\r\t<{}()\"'") {
		return false
	}
	// Must contain at least one alphanumeric character (not just wildcards).
	hasAlnum := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			hasAlnum = true
		case r == '.' || r == '_' || r == '-' || r == '*' || r == '>':
			// Valid NATS subject characters including wildcards.
		default:
			return false
		}
	}
	return hasAlnum
}

// ---------------------------------------------------------------------------
// Go — nats.go / JetStream / NATS Streaming
// ---------------------------------------------------------------------------

// goNATSPublishRe captures nc.Publish(subject, data).
// Group 1 = subject.
var goNATSPublishRe = regexp.MustCompile(`\.Publish\s*\(\s*"([^"\n\r]+)"`)

// goNATSSubscribeRe captures nc.Subscribe(subject, handler).
// Group 1 = subject.
var goNATSSubscribeRe = regexp.MustCompile(`\.Subscribe\s*\(\s*"([^"\n\r]+)"`)

// goNATSQueueSubscribeRe captures nc.QueueSubscribe(subject, queue, handler).
// Groups: 1=subject, 2=queue-group.
var goNATSQueueSubscribeRe = regexp.MustCompile(`\.QueueSubscribe\s*\(\s*"([^"\n\r]+)"\s*,\s*"([^"\n\r]+)"`)

// goNATSRequestRe captures nc.Request(subject, data, timeout).
// Group 1 = subject.
var goNATSRequestRe = regexp.MustCompile(`\.Request\s*\(\s*"([^"\n\r]+)"`)

// goNATSJSPublishRe captures js.Publish(subject, data).
// Group 1 = subject.
var goNATSJSPublishRe = regexp.MustCompile(`\bjs\.Publish\s*\(\s*"([^"\n\r]+)"`)

// goNATSJSSubscribeRe captures js.Subscribe(subject, handler) or
// js.PullSubscribe(subject, ...).
// Group 1 = subject.
var goNATSJSSubscribeRe = regexp.MustCompile(`\bjs\.(?:Subscribe|PullSubscribe|ChanSubscribe)\s*\(\s*"([^"\n\r]+)"`)

// goNATSStanPublishRe captures stan.Publish(subject, data) or sc.Publish(subject, data).
// Group 1 = subject.
var goNATSStanPublishRe = regexp.MustCompile(`(?:\bstan\b|sc)\.Publish\s*\(\s*"([^"\n\r]+)"`)

// goNATSStanSubscribeRe captures sc.Subscribe(subject, ...) or
// sc.QueueSubscribe(subject, ...) — NATS Streaming.
// Group 1 = subject.
var goNATSStanSubscribeRe = regexp.MustCompile(`(?:\bstan\b|sc)\.(?:Subscribe|QueueSubscribe)\s*\(\s*"([^"\n\r]+)"`)

func synthesizeGoNATS(
	src string,
	emitSubject func(subjectID, subject string, props map[string]string),
	emitEdge func(callerKind, callerName, subjectID, edgeKind string, props map[string]string),
) {
	hasNATS := strings.Contains(src, "nats") || strings.Contains(src, "stan") ||
		strings.Contains(src, "JetStream") || strings.Contains(src, "jetstream")
	if !hasNATS {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingGoName(src, offset)
	}

	// jetsreamSubjects and stanSubjects track byte offsets of .Publish/.Subscribe
	// calls that are already handled by the JetStream/STAN passes, so the generic
	// nc.Publish / nc.Subscribe pass below can skip them (avoiding entity-props
	// collision where the generic pass emits first without jetstream/stan flags).
	jsPublishOffsets := map[int]bool{}
	jsSubscribeOffsets := map[int]bool{}

	// JetStream: js.Publish(subject, data) — must run before generic nc.Publish.
	for _, m := range goNATSJSPublishRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, map[string]string{"jetstream": "true"})
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "nats-jetstream",
			"jetstream":       "true",
		})
		jsPublishOffsets[m[0]] = true
	}

	// JetStream: js.Subscribe / js.PullSubscribe / js.ChanSubscribe — before generic.
	for _, m := range goNATSJSSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, map[string]string{"jetstream": "true"})
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "nats-jetstream",
			"jetstream":       "true",
		})
		jsSubscribeOffsets[m[0]] = true
	}

	// NATS Streaming: stan.Publish / sc.Publish — before generic nc.Publish.
	stanPublishOffsets := map[int]bool{}
	stanSubscribeOffsets := map[int]bool{}

	for _, m := range goNATSStanPublishRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, map[string]string{"nats_streaming": "true"})
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "nats-streaming",
			"nats_streaming":  "true",
		})
		stanPublishOffsets[m[0]] = true
	}

	// NATS Streaming: sc.Subscribe / sc.QueueSubscribe.
	for _, m := range goNATSStanSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, map[string]string{"nats_streaming": "true"})
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "nats-streaming",
			"nats_streaming":  "true",
		})
		stanSubscribeOffsets[m[0]] = true
	}

	// Generic nc.Publish(subject, data) — skip offsets already handled above.
	for _, m := range goNATSPublishRe.FindAllStringSubmatchIndex(src, -1) {
		// Check if this offset falls within a JetStream or STAN match.
		skip := false
		for jsOff := range jsPublishOffsets {
			if m[0] >= jsOff && m[0] < jsOff+50 {
				skip = true
				break
			}
		}
		if !skip {
			for stanOff := range stanPublishOffsets {
				if m[0] >= stanOff && m[0] < stanOff+50 {
					skip = true
					break
				}
			}
		}
		if skip {
			continue
		}
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "nats.go",
		})
	}

	// Generic nc.Subscribe(subject, handler) — skip JetStream/STAN handled.
	for _, m := range goNATSSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		skip := false
		for jsOff := range jsSubscribeOffsets {
			if m[0] >= jsOff && m[0] < jsOff+50 {
				skip = true
				break
			}
		}
		if !skip {
			for stanOff := range stanSubscribeOffsets {
				if m[0] >= stanOff && m[0] < stanOff+50 {
					skip = true
					break
				}
			}
		}
		if skip {
			continue
		}
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "nats.go",
		})
	}

	// nc.QueueSubscribe(subject, queue, handler)
	for _, m := range goNATSQueueSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		queueGroup := src[m[4]:m[5]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, map[string]string{"queue_group": queueGroup})
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "nats.go",
			"queue_group":     queueGroup,
		})
	}

	// nc.Request(subject, data, timeout) — request/reply pattern
	for _, m := range goNATSRequestRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, map[string]string{"pattern": "request_reply"})
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "nats.go",
			"pattern":         "request_reply",
		})
	}

}

// ---------------------------------------------------------------------------
// Node — nats.js
// ---------------------------------------------------------------------------

// nodeNATSPublishRe captures nc.publish(subject, data).
// Group 1 = subject.
var nodeNATSPublishRe = regexp.MustCompile("" +
	`\bnc\.publish\s*\(\s*` +
	"[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodeNATSSubscribeRe captures nc.subscribe(subject) or nc.subscribe(subject, opts).
// Group 1 = subject.
var nodeNATSSubscribeRe = regexp.MustCompile("" +
	`\bnc\.subscribe\s*\(\s*` +
	"[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodeNATSRequestRe captures nc.request(subject, data) — request/reply.
// Group 1 = subject.
var nodeNATSRequestRe = regexp.MustCompile("" +
	`\bnc\.request\s*\(\s*` +
	"[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodeNATSJSPublishRe captures js.publish(subject, data).
// Group 1 = subject.
var nodeNATSJSPublishRe = regexp.MustCompile("" +
	`\bjs\.publish\s*\(\s*` +
	"[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodeNATSJSSubscribeRe captures js.subscribe(subject) or
// js.pullSubscribe(subject, durable).
// Group 1 = subject.
var nodeNATSJSSubscribeRe = regexp.MustCompile("" +
	`\bjs\.(?:subscribe|pullSubscribe)\s*\(\s*` +
	"[\"'`]([^\"'`\\n\\r]+)[\"'`]")

func synthesizeNodeNATS(
	src string,
	emitSubject func(subjectID, subject string, props map[string]string),
	emitEdge func(callerKind, callerName, subjectID, edgeKind string, props map[string]string),
) {
	hasNATS := strings.Contains(src, "nats") || strings.Contains(src, "NATS") ||
		strings.Contains(src, "jetstream") || strings.Contains(src, "JetStream")
	if !hasNATS {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingNodeName(src, offset)
	}

	// nc.publish(subject, data)
	for _, m := range nodeNATSPublishRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "nats.js",
		})
	}

	// nc.subscribe(subject)
	for _, m := range nodeNATSSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "nats.js",
		})
	}

	// nc.request(subject, data) — request/reply
	for _, m := range nodeNATSRequestRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, map[string]string{"pattern": "request_reply"})
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "nats.js",
			"pattern":         "request_reply",
		})
	}

	// JetStream: js.publish(subject, data)
	for _, m := range nodeNATSJSPublishRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, map[string]string{"jetstream": "true"})
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "nats-jetstream-js",
			"jetstream":       "true",
		})
	}

	// JetStream: js.subscribe / js.pullSubscribe
	for _, m := range nodeNATSJSSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, map[string]string{"jetstream": "true"})
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "nats-jetstream-js",
			"jetstream":       "true",
		})
	}
}

// ---------------------------------------------------------------------------
// Python — nats.py (asyncio client)
// ---------------------------------------------------------------------------

// pyNATSPublishRe captures await nc.publish(subject, payload).
// Group 1 = subject.
var pyNATSPublishRe = regexp.MustCompile(`\bnc\.publish\s*\(\s*["']([^"'\n\r]+)["']`)

// pyNATSSubscribeRe captures await nc.subscribe(subject, cb=handler) or
// await nc.subscribe(subject, queue=group, cb=handler).
// Group 1 = subject.
var pyNATSSubscribeRe = regexp.MustCompile(`\bnc\.subscribe\s*\(\s*["']([^"'\n\r]+)["']`)

// pyNATSRequestRe captures await nc.request(subject, payload).
// Group 1 = subject.
var pyNATSRequestRe = regexp.MustCompile(`\bnc\.request\s*\(\s*["']([^"'\n\r]+)["']`)

// pyNATSJSPublishRe captures await js.publish(subject, payload).
// Group 1 = subject.
var pyNATSJSPublishRe = regexp.MustCompile(`\bjs\.publish\s*\(\s*["']([^"'\n\r]+)["']`)

// pyNATSJSSubscribeRe captures await js.subscribe(subject) or
// js.pull_subscribe(subject, durable).
// Group 1 = subject.
var pyNATSJSSubscribeRe = regexp.MustCompile(`\bjs\.(?:subscribe|pull_subscribe)\s*\(\s*["']([^"'\n\r]+)["']`)

func synthesizePyNATS(
	src string,
	emitSubject func(subjectID, subject string, props map[string]string),
	emitEdge func(callerKind, callerName, subjectID, edgeKind string, props map[string]string),
) {
	hasNATS := strings.Contains(src, "nats") || strings.Contains(src, "NATS")
	if !hasNATS {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingPyName(src, offset)
	}

	// await nc.publish(subject, payload)
	for _, m := range pyNATSPublishRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "nats.py",
		})
	}

	// await nc.subscribe(subject, ...)
	for _, m := range pyNATSSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "nats.py",
		})
	}

	// await nc.request(subject, payload) — request/reply
	for _, m := range pyNATSRequestRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, map[string]string{"pattern": "request_reply"})
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "nats.py",
			"pattern":         "request_reply",
		})
	}

	// JetStream: await js.publish(subject, payload)
	for _, m := range pyNATSJSPublishRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, map[string]string{"jetstream": "true"})
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "nats-jetstream-py",
			"jetstream":       "true",
		})
	}

	// JetStream: await js.subscribe / js.pull_subscribe
	for _, m := range pyNATSJSSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		subject := src[m[2]:m[3]]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, map[string]string{"jetstream": "true"})
		caller := enclosing(m[0])
		emitEdge("Function", caller, subID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "nats-jetstream-py",
			"jetstream":       "true",
		})
	}
}

// ---------------------------------------------------------------------------
// Java / Kotlin — nats.java
// ---------------------------------------------------------------------------

// javaNATSPublishRe captures connection.publish(subject, data).
// Group 1 = subject.
var javaNATSPublishRe = regexp.MustCompile(`(?:connection|nc|conn)\.publish\s*\(\s*"([^"\n\r]+)"`)

// javaNATSSubscribeRe captures subscription = connection.subscribe(subject).
// Group 1 = subject.
var javaNATSSubscribeRe = regexp.MustCompile(`(?:connection|nc|conn)\.subscribe\s*\(\s*"([^"\n\r]+)"`)

// javaNATSDispatcherSubscribeRe captures dispatcher.subscribe(subject, handler)
// where dispatcher may be any variable name (commonly `d`, `dispatcher`, `sub`).
// We match the pattern: <var>.subscribe("<subject>", ...) in a NATS context.
// To avoid false positives, this is checked only when the file contains NATS imports.
// Group 1 = subject.
var javaNATSDispatcherSubscribeRe = regexp.MustCompile(`\b\w+\.subscribe\s*\(\s*"([^"\n\r]+)"`)

// javaNATSRequestRe captures connection.request(subject, data, ...).
// Group 1 = subject.
var javaNATSRequestRe = regexp.MustCompile(`(?:connection|nc|conn)\.request\s*\(\s*"([^"\n\r]+)"`)

// javaNATSJSPublishRe captures js.publish(subject, ...).
// Group 1 = subject.
var javaNATSJSPublishRe = regexp.MustCompile(`\bjs\.publish\s*\(\s*"([^"\n\r]+)"`)

// javaNATSJSSubscribeRe captures js.subscribe(subject) or
// js.pushSubscribeSync(subject, ...).
// Group 1 = subject.
var javaNATSJSSubscribeRe = regexp.MustCompile(`\bjs\.(?:subscribe|pushSubscribeSync|pullSubscribe)\s*\(\s*"([^"\n\r]+)"`)

func synthesizeJavaNATS(
	src string,
	emitSubject func(subjectID, subject string, props map[string]string),
	emitEdge func(callerKind, callerName, subjectID, edgeKind string, props map[string]string),
) {
	hasNATS := strings.Contains(src, "nats") || strings.Contains(src, "NATS") ||
		strings.Contains(src, "io.nats")
	if !hasNATS {
		return
	}

	className := ""
	if m := classNameRe.FindStringSubmatch(src); len(m) >= 2 {
		className = m[1]
	}

	emitClassEdge := func(subID, edgeKind string, props map[string]string) {
		if className != "" {
			emitEdge("Service", className, subID, edgeKind, props)
		}
	}

	// connection.publish(subject, data)
	for _, m := range javaNATSPublishRe.FindAllStringSubmatch(src, -1) {
		subject := m[1]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, nil)
		emitClassEdge(subID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "nats.java",
		})
	}

	// connection.subscribe(subject)
	for _, m := range javaNATSSubscribeRe.FindAllStringSubmatch(src, -1) {
		subject := m[1]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, nil)
		emitClassEdge(subID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "nats.java",
		})
	}

	// dispatcher.subscribe(subject, handler)
	for _, m := range javaNATSDispatcherSubscribeRe.FindAllStringSubmatch(src, -1) {
		subject := m[1]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, nil)
		emitClassEdge(subID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "nats.java",
			"dispatcher":      "true",
		})
	}

	// connection.request(subject, data) — request/reply
	for _, m := range javaNATSRequestRe.FindAllStringSubmatch(src, -1) {
		subject := m[1]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, map[string]string{"pattern": "request_reply"})
		emitClassEdge(subID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "nats.java",
			"pattern":         "request_reply",
		})
	}

	// JetStream: js.publish(subject, ...)
	for _, m := range javaNATSJSPublishRe.FindAllStringSubmatch(src, -1) {
		subject := m[1]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, map[string]string{"jetstream": "true"})
		emitClassEdge(subID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "nats-jetstream-java",
			"jetstream":       "true",
		})
	}

	// JetStream: js.subscribe / js.pushSubscribeSync / js.pullSubscribe
	for _, m := range javaNATSJSSubscribeRe.FindAllStringSubmatch(src, -1) {
		subject := m[1]
		if !looksLikeNATSSubject(subject) {
			continue
		}
		subID := natsSubjectID(subject)
		emitSubject(subID, subject, map[string]string{"jetstream": "true"})
		emitClassEdge(subID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "nats-jetstream-java",
			"jetstream":       "true",
		})
	}
}
