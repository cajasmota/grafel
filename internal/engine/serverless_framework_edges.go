// Serverless Framework (serverless.yml) topology extraction — #3519 (epic #3512).
//
// Before this pass, serverless.yml was handled only by a regex side-channel in
// internal/enrichers/deployment_topology_extractor.go (extractServerless) that
// pulled `functions:` keys and `path:` values into flat DeploymentTopologyEntry
// records — NOT graph entities, with no edges. The companion
// serverless_edges.go pass detects Lambda *SDK invocations in code* and emits
// `aws-lambda:<name>` synthetics, but `resolveServerlessYMLName` was an explicit
// STUB (handler symbol == logical name) because the serverless.yml topology join
// was deferred to #927.
//
// This pass wires the real thing. For a serverless.yml it emits first-class
// graph entities and edges:
//
//   - functions.<name>  → SCOPE.ServerlessFunction keyed `aws-lambda:<name>` —
//     the SAME synthetic ID serverless_edges.go emits, so a Lambda handler
//     defined in code (HANDLES edge) and the serverless.yml declaration collapse
//     onto a single node. provider runtime/region land as function metadata.
//   - functions.<name>.handler (e.g. `src/handler.hello`) → a HANDLES edge from
//     the resolved code symbol (file.method) to the function. This also
//     populates the package-level serverlessYMLHandlerIndex so the previously
//     stubbed resolveServerlessYMLName can map a handler symbol → logical name.
//   - events:
//     http / httpApi → http_endpoint_definition (verb+path) + SERVES edge
//     function → endpoint, using the canonical `http:<METHOD>:<path>` ID so
//     the endpoint collapses with any code-side definition / consumer call.
//     sqs / sns / stream / kinesis → a synthetic queue/topic entity + TRIGGERS
//     edge from the queue/topic to the function (the event source triggers
//     the function).
//     schedule → SCOPE.ScheduledJob + TRIGGERS edge job → function.
//   - resources: (raw CloudFormation passthrough) is intentionally NOT parsed
//     here — that is the CFN pass's job; duplicating it would double-emit.
//
// # Scope guard
//
// Append-only — never modifies or removes existing entities or edges, so it
// cannot regress the surrounding pipeline's bug-rate. Only fires for a file the
// content sniffer recognises as a serverless.yml (top-level service: + provider:
// + functions:, or a serverless.yml/.yaml basename).
//
// Closes #3519. Epic #3512.
package engine

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

// slsScheduledJobKind / slsServesEdge / slsTriggersEdge alias the canonical
// kind constants this pass emits so the producer-kind guard (producer_kinds_test)
// stays green via the typed constants already declared elsewhere in the package.
const (
	slsServesEdgeKind   = "SERVES"   // function → endpoint
	slsTriggersEdgeKind = "TRIGGERS" // event source / schedule → function
)

// serverlessYMLHandlerIndex maps a handler *symbol* (the trailing dotted method
// of a `handler:` value, e.g. "hello" from "src/handler.hello") to its logical
// serverless.yml function name. Populated as serverless.yml files are parsed so
// the formerly-stubbed resolveServerlessYMLName can perform the topology join
// without re-reading the YAML at code-pass time. Append-only within a run.
var serverlessYMLHandlerIndex = map[string]string{}

// slsServiceKeyRe / slsProviderKeyRe / slsFunctionsKeyRe detect the three
// top-level keys that together identify a Serverless Framework manifest.
var (
	slsServiceKeyRe   = regexp.MustCompile(`(?m)^service\s*:`)
	slsProviderKeyRe  = regexp.MustCompile(`(?m)^provider\s*:`)
	slsFunctionsKeyRe = regexp.MustCompile(`(?m)^functions\s*:`)
)

// isServerlessFrameworkFile reports whether (path, src) is a Serverless
// Framework manifest. Recognised by basename (serverless.yml/.yaml) OR by the
// co-occurrence of the three signature top-level keys.
func isServerlessFrameworkFile(path, src string) bool {
	base := strings.ToLower(filepath.Base(path))
	if base == "serverless.yml" || base == "serverless.yaml" {
		return true
	}
	return slsServiceKeyRe.MatchString(src) &&
		slsProviderKeyRe.MatchString(src) &&
		slsFunctionsKeyRe.MatchString(src)
}

// applyServerlessFrameworkEdges is the per-file entry point. Append-only.
func applyServerlessFrameworkEdges(args DetectorPassArgs) DetectorPassResult {
	entities := args.Entities
	relationships := args.Relationships
	if len(args.Content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	// Serverless Framework manifests are YAML.
	if args.Lang != "yaml" {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	src := string(args.Content)
	if !isServerlessFrameworkFile(args.Path, src) {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	manifest := parseServerlessYML(src)
	if len(manifest.functions) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	path := args.Path
	seenEnt := map[string]bool{}
	seenEdge := map[string]bool{}

	emitEntity := func(rec types.EntityRecord) {
		key := rec.Kind + "|" + rec.Name
		if seenEnt[key] {
			return
		}
		seenEnt[key] = true
		entities = append(entities, rec)
	}
	emitEdge := func(fromID, toID, kind string, props map[string]string) {
		if fromID == "" || toID == "" {
			return
		}
		key := kind + "|" + fromID + "|" + toID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fromID,
			ToID:       toID,
			Kind:       kind,
			Properties: props,
		})
	}

	for _, fn := range manifest.functions {
		fnID := lambdaFunctionID(fn.name)
		fnEntityRef := fmt.Sprintf("%s:%s", serverlessFunctionKind, fnID)

		fnProps := map[string]string{
			"provider":      "aws-lambda",
			"function_name": fn.name,
			"pattern_type":  "serverless_framework",
			"iac_tool":      "serverless-framework",
		}
		if fn.handler != "" {
			fnProps["handler"] = fn.handler
		}
		if manifest.runtime != "" {
			fnProps["runtime"] = manifest.runtime
		}
		if manifest.region != "" {
			fnProps["region"] = manifest.region
		}
		if manifest.service != "" {
			fnProps["service"] = manifest.service
		}
		emitEntity(types.EntityRecord{
			Name:             fnID,
			Kind:             serverlessFunctionKind,
			SourceFile:       path,
			Language:         "yaml",
			Properties:       fnProps,
			EnrichmentStatus: types.StatusPending,
			QualityScore:     0.8,
		})

		// HANDLES: handler code symbol → function. The handler value is
		// `path/to/file.method`; the trailing dotted segment is the exported
		// symbol, the rest is the module path. We emit a HANDLES edge from the
		// SCOPE.Function:<symbol> entity (the same FromID shape serverless_edges.go
		// uses for code-side handlers) and register the symbol→logical-name map.
		if symbol := handlerSymbol(fn.handler); symbol != "" {
			serverlessYMLHandlerIndex[symbol] = fn.name
			emitEdge(
				fmt.Sprintf("SCOPE.Function:%s", symbol),
				fnEntityRef,
				serverlessHandlesEdgeKind,
				map[string]string{
					"provider":       "aws-lambda",
					"pattern_type":   "serverless_framework",
					"handler":        fn.handler,
					"handler_module": handlerModule(fn.handler),
				},
			)
		}

		for _, ev := range fn.events {
			switch ev.kind {
			case "http", "httpApi":
				if ev.method == "" || ev.path == "" {
					continue
				}
				method := strings.ToUpper(ev.method)
				canonPath := canonicalizeServerlessPath(ev.path)
				epID := httproutes.SyntheticID(method, canonPath)
				emitEntity(types.EntityRecord{
					ID:            epID,
					Name:          epID,
					QualifiedName: epID,
					Kind:          httpEndpointDefinitionKind,
					SourceFile:    path,
					Language:      "yaml",
					Properties: map[string]string{
						"verb":           method,
						"path":           canonPath,
						"framework":      "serverless-framework",
						"pattern_type":   "serverless_framework",
						"source_handler": fnEntityRef,
						"event_type":     ev.kind,
					},
					EnrichmentStatus: types.StatusPending,
					QualityScore:     0.8,
				})
				// SERVES: function → endpoint.
				emitEdge(fnEntityRef, fmt.Sprintf("%s:%s", httpEndpointDefinitionKind, epID),
					slsServesEdgeKind, map[string]string{
						"pattern_type": "serverless_framework",
						"event_type":   ev.kind,
					})

			case "sqs":
				if ev.source == "" {
					continue
				}
				qID := sqsQueueID(ev.source)
				emitEntity(types.EntityRecord{
					Name:     qID,
					Kind:     queueEntityKind,
					Language: "yaml",
					Properties: map[string]string{
						"broker":       "sqs",
						"queue_name":   sqsQueueDisplayName(ev.source),
						"pattern_type": "serverless_framework",
						"iac_tool":     "serverless-framework",
					},
					EnrichmentStatus: types.StatusPending,
					QualityScore:     0.8,
				})
				// TRIGGERS: queue → function (the event source triggers the fn).
				emitEdge(fmt.Sprintf("%s:%s", queueEntityKind, qID), fnEntityRef,
					slsTriggersEdgeKind, map[string]string{
						"broker":       "sqs",
						"pattern_type": "serverless_framework",
						"event_type":   "sqs",
					})

			case "sns":
				if ev.source == "" {
					continue
				}
				tID := snsTopicID(snsTopicNameFromARN(ev.source))
				emitEntity(types.EntityRecord{
					Name:     tID,
					Kind:     messageTopicKind,
					Language: "yaml",
					Properties: map[string]string{
						"broker":       "sns",
						"topic_name":   snsTopicNameFromARN(ev.source),
						"pattern_type": "serverless_framework",
						"iac_tool":     "serverless-framework",
					},
					EnrichmentStatus: types.StatusPending,
					QualityScore:     0.8,
				})
				emitEdge(fmt.Sprintf("%s:%s", messageTopicKind, tID), fnEntityRef,
					slsTriggersEdgeKind, map[string]string{
						"broker":       "sns",
						"pattern_type": "serverless_framework",
						"event_type":   "sns",
					})

			case "stream", "kinesis":
				if ev.source == "" {
					continue
				}
				streamName := streamNameFromARN(ev.source)
				sID := "stream:" + streamName
				emitEntity(types.EntityRecord{
					Name:     sID,
					Kind:     queueEntityKind,
					Language: "yaml",
					Properties: map[string]string{
						"broker":       "kinesis",
						"stream_name":  streamName,
						"pattern_type": "serverless_framework",
						"iac_tool":     "serverless-framework",
					},
					EnrichmentStatus: types.StatusPending,
					QualityScore:     0.8,
				})
				emitEdge(fmt.Sprintf("%s:%s", queueEntityKind, sID), fnEntityRef,
					slsTriggersEdgeKind, map[string]string{
						"broker":       "kinesis",
						"pattern_type": "serverless_framework",
						"event_type":   ev.kind,
					})

			case "schedule":
				if ev.source == "" {
					continue
				}
				jobID := "serverless-framework:" + fn.name + ":schedule"
				emitEntity(types.EntityRecord{
					Name:     jobID,
					Kind:     scheduledJobKind,
					Language: "yaml",
					Properties: map[string]string{
						"schedule":     ev.source,
						"pattern_type": "serverless_framework",
						"iac_tool":     "serverless-framework",
						"function":     fn.name,
					},
					EnrichmentStatus: types.StatusPending,
					QualityScore:     0.8,
				})
				// TRIGGERS: scheduled job → function.
				emitEdge(fmt.Sprintf("%s:%s", scheduledJobKind, jobID), fnEntityRef,
					slsTriggersEdgeKind, map[string]string{
						"pattern_type": "serverless_framework",
						"event_type":   "schedule",
						"schedule":     ev.source,
					})
			}
		}
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// ---------------------------------------------------------------------------
// Handler symbol resolution (wires the resolveServerlessYMLName stub)
// ---------------------------------------------------------------------------

// handlerSymbol returns the exported symbol of a serverless `handler:` value.
// `src/handler.hello` → `hello`; `handler.main` → `main`; `dir/handler` → "".
// A handler with no dotted method is a Go/Java-style file ref and yields no
// symbol-level HANDLES edge.
func handlerSymbol(handler string) string {
	h := strings.TrimSpace(handler)
	if h == "" {
		return ""
	}
	idx := strings.LastIndex(h, ".")
	if idx < 0 || idx == len(h)-1 {
		return ""
	}
	return h[idx+1:]
}

// handlerModule returns the module-path portion of a serverless `handler:`
// value: `src/handler.hello` → `src/handler`.
func handlerModule(handler string) string {
	h := strings.TrimSpace(handler)
	idx := strings.LastIndex(h, ".")
	if idx <= 0 {
		return h
	}
	return h[:idx]
}

// ---------------------------------------------------------------------------
// Path canonicalisation
// ---------------------------------------------------------------------------

// canonicalizeServerlessPath normalises a serverless.yml event path to the
// canonical `/`-prefixed `{param}` form used by httproutes synthetic IDs.
// Serverless already uses `{proxy+}` / `{id}` curly params, so we mainly ensure
// the leading slash and collapse the `+` greedy marker.
func canonicalizeServerlessPath(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	// `{proxy+}` greedy catch-all → `{proxy}` so the canonical path is stable.
	p = strings.ReplaceAll(p, "+}", "}")
	return p
}

// streamNameFromARN extracts the trailing stream name from a Kinesis/DynamoDB
// stream ARN, or returns the input unchanged when it is already a bare name.
// `arn:aws:kinesis:us-east-1:123:stream/orders` → `orders`.
func streamNameFromARN(arnOrName string) string {
	s := strings.TrimSpace(arnOrName)
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		return s[idx+1:]
	}
	if idx := strings.LastIndex(s, ":"); idx >= 0 {
		return s[idx+1:]
	}
	return s
}
