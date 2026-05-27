// Django signal + Celery cross-file pub/sub topology edges — #1617.
//
// The per-file scheduled-job pass (scheduled_jobs_edges.go) already emits a
// SCOPE.ScheduledJob entity + TRIGGERS edge for every @shared_task / @app.task
// definition, plus a PUBLISHES_TO edge for any `task.delay()` call site that
// lives in the SAME file as the task definition. Real Django/Celery apps almost
// never satisfy that constraint: tasks live in `tasks/*.py` and are dispatched
// from `views/`, `services/`, `signals/`, and `management/commands/`. So the
// same-file pass produced ~4 edges on upvate while 14 dispatch sites and 21
// tasks went unconnected.
//
// Likewise, Django's custom-signal mechanism — `sig = Signal()`, fired with
// `sig.send(...)` / `sig.send_robust(...)`, handled by `@receiver(sig)` — is a
// genuine in-process pub/sub bus that produced ZERO edges: the per-file custom
// extractor only emits HANDLES_SIGNAL → sender model for the built-in
// post_save/pre_save family, never the publisher→handler dispatch.
//
// This file adds two REPO-WIDE passes (they need every file in scope to resolve
// cross-file references):
//
//	ApplyCeleryDispatchEdges   — collects all Celery task defs across the repo,
//	                             then emits CALLS edges from each
//	                             `task.delay()` / `.apply_async()` / `.s()`
//	                             call site's enclosing function to the task
//	                             definition function. CALLS is used (not
//	                             PUBLISHES_TO) so the edge shows up in
//	                             find_callees / find_callers on a task and in
//	                             flows, complementing the existing PUBLISHES_TO →
//	                             SCOPE.ScheduledJob topic edge.
//
//	ApplyDjangoSignalPubSub    — collects all custom `Signal()` definitions and
//	                             their @receiver handlers across the repo, emits
//	                             a SCOPE.MessageTopic per signal, a SUBSCRIBES_TO
//	                             edge from each handler → topic, and a
//	                             PUBLISHES_TO edge from each `sig.send()` call
//	                             site's enclosing function → topic. This renders
//	                             a publisher → signal → handler diagram in
//	                             /topology and /flows.
//
// Both passes are append-only: they never modify or remove existing entities
// or edges, so they cannot regress surrounding passes.
//
// Refs #1617.
package engine

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Celery cross-file dispatch edges
// ---------------------------------------------------------------------------

// celeryTaskDefRe matches a Celery task definition decorated with
// @shared_task / @app.task / @celery.task (with or without arguments) directly
// above a def. Group 1 = function name. Mirrors pyCeleryTaskDecoratorRe in
// scheduled_jobs_edges.go but compiled independently so the two passes stay
// decoupled.
var celeryTaskDefRe = regexp.MustCompile(
	`(?m)@(?:celery\.task|app\.task|shared_task|[\w.]+\.task)[^\n]*\n(?:\s*@[^\n]*\n)*\s*(?:async\s+)?def\s+(\w+)\s*\(`,
)

// celeryDispatchRe matches `<var>.delay(`, `<var>.apply_async(`, `<var>.s(` and
// `<var>.si(` call sites. Group 1 = the variable/attribute chain to the left of
// the call (e.g. `process_model_change` or `self.task` — we use only the final
// identifier as the task name candidate). Group 2 = the dispatch method.
var celeryDispatchRe = regexp.MustCompile(
	`(?m)\b([\w.]+?)\.(delay|apply_async|s|si)\s*\(`,
)

// ApplyCeleryDispatchEdges resolves Celery `.delay()` / `.apply_async()` /
// `.s()` call sites across the whole repo to their `@shared_task` / `@app.task`
// definitions, emitting a CALLS edge from the call site's enclosing function to
// the task definition function. Returns only the newly synthesized edges (the
// caller appends them to pass3Records).
//
// pyPaths:    repo-relative paths of every Python file.
// fileReader: returns the source bytes for a repo-relative path.
func ApplyCeleryDispatchEdges(
	pyPaths []string,
	fileReader NestedURLConfFileReader,
) []types.RelationshipRecord {
	if fileReader == nil {
		return nil
	}

	// Pass 1 — collect every task name defined anywhere in the repo. We key on
	// the bare function name; the ToID is a structural ref the resolver binds
	// cross-file via byName. A name defined in more than one file is still a
	// single graph node candidate per the resolver's uniqueness handling, which
	// is acceptable for dispatch edges.
	taskNames := map[string]bool{}
	srcByPath := make(map[string][]byte, len(pyPaths))
	for _, p := range pyPaths {
		content := fileReader(p)
		if len(content) == 0 {
			continue
		}
		srcByPath[p] = content
		s := string(content)
		if !strings.Contains(s, "task") && !strings.Contains(s, "celery") {
			continue
		}
		for _, m := range celeryTaskDefRe.FindAllStringSubmatch(s, -1) {
			taskNames[m[1]] = true
		}
	}
	if len(taskNames) == 0 {
		return nil
	}

	// Pass 2 — scan every file for dispatch call sites and emit CALLS edges.
	var out []types.RelationshipRecord
	seen := map[string]bool{}
	for _, p := range pyPaths {
		content := srcByPath[p]
		if len(content) == 0 {
			continue
		}
		s := string(content)
		if !strings.Contains(s, ".delay(") && !strings.Contains(s, ".apply_async(") &&
			!strings.Contains(s, ".s(") && !strings.Contains(s, ".si(") {
			continue
		}
		for _, idx := range celeryDispatchRe.FindAllStringSubmatchIndex(s, -1) {
			chain := s[idx[2]:idx[3]]
			// Use the final dotted segment as the task-name candidate so both
			// `process_model_change.delay()` and `tasks.process_model_change.delay()`
			// resolve to the same task.
			parts := strings.Split(chain, ".")
			taskName := parts[len(parts)-1]
			if !taskNames[taskName] {
				continue
			}
			caller := enclosingFunction(s, idx[0])
			if caller == "" || caller == taskName {
				continue // unresolved scope or self-dispatch inside the task itself
			}
			key := p + "|" + caller + "|" + taskName
			if seen[key] {
				continue
			}
			seen[key] = true
			// Line: no tree-sitter node here; derive from regex byte offset.
			// strings.Count counts '\n' before idx[0] → 0-based row → +1 → 1-based.
			dispatchLine := strconv.Itoa(strings.Count(s[:idx[0]], "\n") + 1)
			out = append(out, types.RelationshipRecord{
				FromID: "SCOPE.Operation:" + caller,
				ToID:   "Function:" + taskName,
				Kind:   string(types.RelationshipKindCalls),
				Properties: map[string]string{
					"framework":    "celery",
					"pattern_type": "celery_dispatch_synthesis",
					"dispatch":     "async",
					"line":         dispatchLine,
				},
			})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Django custom-signal pub/sub edges
// ---------------------------------------------------------------------------

// signalTopicKind reuses the canonical message-topic entity kind so custom
// Django signals render in /topology alongside Kafka/SNS/PubSub topics.
const signalTopicKind = messageTopicKind // "SCOPE.MessageTopic"

// signalDefRe matches a module-level custom-signal definition:
//
//	mysig = Signal()
//	mysig = django.dispatch.Signal()
//	mysig = Signal(providing_args=[...])
//
// Group 1 = signal variable name.
var signalDefRe = regexp.MustCompile(
	`(?m)^\s*(\w+)\s*=\s*(?:django\.dispatch\.)?Signal\s*\(`,
)

// receiverHandlerRe matches a @receiver(<signal>...) decorator (allowing
// stacked decorators between it and the def) directly above a def, capturing
// both the subscribed signal expression and the handler name in one match.
// Group 1 = signal expression (first positional arg of @receiver).
// Group 2 = handler function name.
var receiverHandlerRe = regexp.MustCompile(
	`(?m)@receiver\s*\(\s*([\w.]+)[^\n]*\)(?:\s*\n\s*@[^\n]*)*\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`,
)

// signalSendRe matches `<signal>.send(` and `<signal>.send_robust(` call sites.
// Group 1 = signal variable name. We only emit a publisher edge when the
// signal name is a known custom signal (collected in pass 1).
var signalSendRe = regexp.MustCompile(
	`(?m)\b(\w+)\.send(?:_robust)?\s*\(`,
)

// ApplyDjangoSignalPubSub collects custom Django signal definitions and their
// @receiver handlers across the whole repo and emits, per custom signal:
//
//   - one SCOPE.MessageTopic entity (the signal),
//   - a SUBSCRIBES_TO edge from each handler function → topic,
//   - a PUBLISHES_TO edge from each `signal.send()` caller's enclosing
//     function → topic.
//
// Built-in signals (post_save, pre_save, …) are intentionally skipped: their
// model linkage is already covered by HANDLES_SIGNAL in the per-file custom
// extractor, and treating them as topics would create one giant fan-in node.
//
// Returns the synthesized entities and relationships; the caller appends them
// to the pass3 record set.
func ApplyDjangoSignalPubSub(
	pyPaths []string,
	fileReader NestedURLConfFileReader,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if fileReader == nil {
		return nil, nil
	}

	// Pass 1 — collect every custom signal definition (name → defining file).
	customSignals := map[string]string{}
	srcByPath := make(map[string][]byte, len(pyPaths))
	for _, p := range pyPaths {
		content := fileReader(p)
		if len(content) == 0 {
			continue
		}
		srcByPath[p] = content
		s := string(content)
		if !strings.Contains(s, "Signal(") {
			continue
		}
		for _, m := range signalDefRe.FindAllStringSubmatch(s, -1) {
			name := m[1]
			if _, ok := customSignals[name]; !ok {
				customSignals[name] = p
			}
		}
	}
	if len(customSignals) == 0 {
		return nil, nil
	}

	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	emittedTopic := map[string]bool{}
	seenEdge := map[string]bool{}

	// topicStub returns the resolver-friendly stub form used on both the
	// entity construction-time ID and on edge endpoints: "<kind>:<name>".
	// The trailing name MUST match EntityRecord.Name verbatim — splitStub
	// in resolve.Index.LookupStatusHint splits on the first ':' and then
	// looks up byKind[kind][name] / byName[name]. Using "django_signal:<n>"
	// as the name segment (as the original #1617 implementation did) broke
	// the resolver lookup so every PUBLISHES_TO / SUBSCRIBES_TO ToID kept
	// its stub form on-disk instead of being rewritten to the topic's hex
	// EntityID, leaving topology queries blind to signal pub/sub at runtime
	// (#1649). The "django_signal:" disambiguator is now carried only in
	// Properties["signal"] (still set below).
	topicStub := func(name string) string {
		return signalTopicKind + ":" + name
	}

	emitTopic := func(name, sourceFile string) string {
		stub := topicStub(name)
		if !emittedTopic[stub] {
			emittedTopic[stub] = true
			ents = append(ents, types.EntityRecord{
				ID:         stub,
				Name:       name,
				Kind:       signalTopicKind,
				SourceFile: sourceFile,
				Language:   "python",
				Properties: map[string]string{
					"framework":    "django_signals",
					"transport":    "django_signal",
					"pattern_type": "django_signal_pubsub_synthesis",
					"signal":       name,
				},
				EnrichmentRequired: false,
				EnrichmentStatus:   types.StatusPending,
				QualityScore:       0.8,
			})
		}
		return stub
	}

	emitEdge := func(fromOp, topicStub, kind string) {
		if fromOp == "" {
			return
		}
		key := kind + "|" + fromOp + "|" + topicStub
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		rels = append(rels, types.RelationshipRecord{
			FromID: "SCOPE.Operation:" + fromOp,
			ToID:   topicStub,
			Kind:   kind,
			Properties: map[string]string{
				"framework":    "django_signals",
				"pattern_type": "django_signal_pubsub_synthesis",
			},
		})
	}

	// Pass 2 — handlers (SUBSCRIBES_TO) and senders (PUBLISHES_TO).
	for _, p := range pyPaths {
		content := srcByPath[p]
		if len(content) == 0 {
			continue
		}
		s := string(content)

		// Subscribers: @receiver(<customsignal>) def handler(...)
		if strings.Contains(s, "@receiver") {
			for _, hm := range receiverHandlerRe.FindAllStringSubmatch(s, -1) {
				signalName := hm[1]
				handler := hm[2]
				if _, ok := customSignals[signalName]; !ok {
					continue // built-in signal — handled elsewhere
				}
				topicID := emitTopic(signalName, customSignals[signalName])
				emitEdge(handler, topicID, string(types.RelationshipKindSubscribesTo))
			}
		}

		// Publishers: <customsignal>.send(...) / .send_robust(...)
		if strings.Contains(s, ".send") {
			for _, idx := range signalSendRe.FindAllStringSubmatchIndex(s, -1) {
				sigName := s[idx[2]:idx[3]]
				if _, ok := customSignals[sigName]; !ok {
					continue
				}
				topicID := emitTopic(sigName, customSignals[sigName])
				caller := enclosingFunction(s, idx[0])
				emitEdge(caller, topicID, string(types.RelationshipKindPublishesTo))
			}
		}
	}

	return ents, rels
}
