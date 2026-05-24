// Entry-point ranking for the process-flow BFS pass (#724).
//
// Each candidate function/method/operation is scored by a small set of
// language-agnostic heuristics. The scoring is intentionally additive
// and capped so that no single signal dominates — empirically this gives
// reasonable distributions across Python (decorator-heavy handlers), Go
// (main + ExportedTitleCase functions), Java/Kotlin (annotated controller
// methods), and JS/TS (default-exported handlers, useEffect bodies).
//
// Signals (additive):
//
//   - Fan-out / fan-in ratio: high callees, low callers → entry-like.
//   - Name pattern: handle*, *Handler, *Controller, *Service, main, run,
//     bootstrap, start, init, on*, useEffect, componentDidMount, …
//   - HTTP boundary: entity with inbound IMPLEMENTS / ROUTES_TO / SERVES
//     from a Route or http_endpoint is almost certainly an entry.
//   - Broker boundary (#797): entity that is the target of a SUBSCRIBES_TO
//     (Kafka @Incoming / @KafkaListener), WS_SUBSCRIBES_TO (WebSocket handler),
//     or TRIGGERS (ScheduledJob → handler) edge is a broker-side entry and
//     receives the same score boost as HTTP handlers.
//   - Exported flag: pythonic dunder-init or capitalised Go identifier,
//     or signature substring " export " — boosted.
//   - Utility penalty: get*, set*, format*, parse*, validate*, *Helper,
//     *Util, *_test — demoted (they are leaves, not entries).
//
// The final list is sorted by descending score, then by canonical ID for
// deterministic output.
package engine

import (
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// entryCandidate carries the scoring breakdown alongside the entity id.
type entryCandidate struct {
	id        string
	name      string
	kind      string
	score     float64
	entryKind string // e.g. "http", "kafka_consumer", "scheduled", "websocket", "webhook", ""
}

// rankEntryPoints scores every Function/Method/Operation/Component entity
// and returns the candidates in descending score order. Candidates with
// score ≤ 0 (heavy utility penalty, no fan-out) are dropped.
func rankEntryPoints(doc *graph.Document, byID map[string]*graph.Entity, adj *callsAdjacency, cfg ProcessFlowConfig) []entryCandidate {
	// HTTP-boundary signal: any entity on either side of an IMPLEMENTS /
	// ROUTES_TO / SERVES edge is almost certainly an entry point (or the
	// endpoint it serves).
	httpBoundary := buildHTTPBoundarySet(doc)

	// #797 — broker-boundary signal: any entity that is the target of a
	// SUBSCRIBES_TO (Kafka @Incoming / @KafkaListener / akka EventBus),
	// WS_SUBSCRIBES_TO (WebSocket @OnMessage / fastapi websocket handler), or
	// TRIGGERS (ScheduledJob → handler) edge is a broker-side entry point.
	// These methods are the functional equivalent of HTTP handlers for their
	// respective transports and deserve the same score boost.
	brokerBoundary := buildBrokerBoundarySet(doc)

	out := make([]entryCandidate, 0, 64)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if !isEntryCandidate(e) {
			continue
		}
		outDeg := len(adj.out[e.ID])
		if outDeg == 0 {
			// Pure leaves are never entries.
			continue
		}
		inDeg := adj.in[e.ID]
		score := float64(outDeg) / float64(inDeg+1)

		// Name pattern boosts. Match the un-prefixed local name only —
		// extractors sometimes pack package paths into Name.
		local := localName(e.Name)
		if entryNameRE.MatchString(local) {
			score += 4.0
		}
		if utilityNameRE.MatchString(local) {
			score -= 3.0
		}

		// HTTP boundary signal — biggest boost. An IMPLEMENTS edge from a
		// route handler to this entity (or vice versa) makes it the
		// canonical entry for that route.
		entryKind := ""
		if httpBoundary[e.ID] {
			score += 6.0
			entryKind = "http"
		}

		// #797 — broker boundary signal. Equal boost to HTTP so Kafka
		// consumers and WebSocket handlers are ranked on par with route
		// handlers. When both signals fire (unusual but not impossible for a
		// handler that both implements an HTTP route AND subscribes to a
		// topic), we take the broker label as the more specific one.
		if bkind, ok := brokerBoundary[e.ID]; ok {
			score += 6.0
			entryKind = bkind // "kafka_consumer", "scheduled", "websocket", "webhook"
		}

		// Exported / public boost.
		if isExportedName(local, e.Language) {
			score += 1.5
		}

		// Per-kind tweak: SCOPE.Operation is the framework-extractor kind
		// used for annotated route handler methods (Java, Spring) and
		// scores higher than a bare Function.
		switch e.Kind {
		case "SCOPE.Operation":
			score += 1.0
		case "SCOPE.Component":
			score += 0.5
		case "SCOPE.JSX":
			// Issue #2024 — JSX component entities are UI entry points;
			// give them a modest boost so React component render functions
			// (e.g. TransferReceive) seed process-flow chains even when
			// their name doesn't match the entryNameRE suffix list.
			score += 2.0
			if entryKind == "" {
				entryKind = "ui_component"
			}
		}

		// Issue #2024 — bonus for PascalCase function names in JSX/TSX source
		// files (React component pattern: TransferReceive, PaymentForm, etc.).
		// These are exported components that drive UI workflows and should seed
		// the BFS even without an HTTP boundary signal.
		if entryKind == "" && isPascalCaseIdentifier(local) && isJSXSourceFile(e.SourceFile) {
			score += 2.0
			entryKind = "ui_component"
		}

		if score <= 0 {
			continue
		}
		out = append(out, entryCandidate{id: e.ID, name: e.Name, kind: e.Kind, score: score, entryKind: entryKind})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].id < out[j].id
	})
	return out
}

// buildBrokerBoundarySet returns a map from entity ID to entry-kind label for
// every entity that is the CALLEE side of a broker-subscription edge. These
// are the handler methods that receive messages from a broker and are
// functionally equivalent to HTTP route handlers as BFS entry points.
//
// Edge kinds recognised (#797):
//
//   - SUBSCRIBES_TO (to-side): the entity is a Kafka @Incoming / @KafkaListener
//     or RabbitMQ / SQS consumer handler. Entry kind = "kafka_consumer".
//   - WS_SUBSCRIBES_TO (to-side): the entity is a WebSocket @OnMessage /
//     FastAPI websocket / socket.io handler. Entry kind = "websocket".
//   - TRIGGERS (from-side of ScheduledJob → handler): a ScheduledJob entity
//     fires TRIGGERS at the handler function. The handler entity is the target,
//     so we detect TRIGGERS edges where the from-entity is SCOPE.ScheduledJob.
//     Entry kind = "scheduled".
//   - Webhook http_endpoint entities with is_webhook=true are already scored
//     via the HTTP boundary set (IMPLEMENTS edges); no separate detection needed.
//
// The map value is the most-specific entry-kind label: if a method appears on
// multiple broker-subscription edges, the last-seen label wins (all are valid
// broker entries so the label choice is cosmetic only).
func buildBrokerBoundarySet(doc *graph.Document) map[string]string {
	out := make(map[string]string)
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		switch r.Kind {
		case "SUBSCRIBES_TO":
			// The entity SUBSCRIBES to a topic — it's the consumer handler.
			// The FromID is the method/function entity; the ToID is the topic.
			// We mark the FROM entity as a kafka_consumer entry point.
			if r.FromID != "" {
				if _, already := out[r.FromID]; !already {
					out[r.FromID] = "kafka_consumer"
				}
			}
		case "WS_SUBSCRIBES_TO":
			// The entity is a WebSocket message handler. FromID is the handler;
			// ToID is the ChannelEvent entity.
			if r.FromID != "" {
				if _, already := out[r.FromID]; !already {
					out[r.FromID] = "websocket"
				}
			}
		case "TRIGGERS":
			// TRIGGERS goes from SCOPE.ScheduledJob → handler function.
			// The ToID is the handler — mark it as a scheduled entry.
			if r.ToID != "" {
				if _, already := out[r.ToID]; !already {
					out[r.ToID] = "scheduled"
				}
			}
		}
	}
	return out
}

// isEntryCandidate filters to entity kinds that can host a CALLS chain.
// Routes and Endpoints themselves are targets of IMPLEMENTS edges, not
// origins of CALLS; the handler entity (Function/Method/Operation) is what
// actually emits the CALLS chain.
//
// Issue #2024 — SCOPE.JSX (React/Vue component entities emitted by the
// JS/TS extractor) are now included so that frontend component render
// functions like TransferReceive can seed a process-flow chain when they
// call a service function that eventually reaches an HTTP endpoint.
func isEntryCandidate(e *graph.Entity) bool {
	switch e.Kind {
	case "SCOPE.Function", "SCOPE.Operation", "SCOPE.Component", "SCOPE.Class",
		"SCOPE.JSX":
		return true
	}
	return false
}

// localName strips package qualifications from an entity name. Extractors
// emit names like "module.path.handleSubmit" or "ClassName.method" — for
// the regex match we only care about the trailing identifier.
func localName(n string) string {
	if i := strings.LastIndex(n, "."); i >= 0 && i+1 < len(n) {
		return n[i+1:]
	}
	if i := strings.LastIndex(n, "::"); i >= 0 && i+2 < len(n) {
		return n[i+2:]
	}
	return n
}

// entryNameRE matches identifiers that strongly suggest an entry point.
// The list is intentionally broad to span Python (snake_case), JS/TS
// (camelCase), Go (CamelCase), and Java/Kotlin annotated handler styles.
var entryNameRE = regexp.MustCompile(
	`^(?i)(main|run|start|bootstrap|init|setup|launch|serve|` +
		`on[A-Z]\w*|handle[A-Z_]\w*|process[A-Z]\w*|dispatch[A-Z]?\w*|` +
		`useEffect|componentDidMount|componentWillMount|render|module|` +
		`application|app_factory|create_app|app|getServerSideProps|getStaticProps|` +
		`__main__|__init__|run_server|run_app|listen)$` +
		`|.*(Handler|Controller|Service|Endpoint|Resource|Action|Job|Task|Worker|Listener|Consumer|Subscriber|Producer|Resolver|Mutation|Query|Command|Middleware|Filter|Interceptor|Pipeline|Saga|Reducer|Module|Page|View|Screen|Route|Hook|Cron|Schedule)$`,
)

// utilityNameRE matches identifiers that strongly suggest a leaf utility.
var utilityNameRE = regexp.MustCompile(
	`^(?i)(get|set|is|has|to|from|as|of)[A-Z_]?\w*$|` +
		`^(?i)(format|parse|validate|sanitize|normalize|encode|decode|escape|unescape|hash|sign|verify|serialize|deserialize|stringify|clone|copy|merge|equal|equals|compare|cmp|len|length|size|count|sum|min|max)\w*$|` +
		`.*(Helper|Util|Utils|Helpers|Constants?)$`,
)

// isExportedName approximates "is this symbol publicly visible". Per-
// language conventions:
//   - Go: leading uppercase
//   - Python/Ruby: not starting with underscore
//   - JS/TS/Java/Kotlin: leading uppercase OR camelCase (most code is
//     exported), so we conservatively treat ALL non-underscore identifiers
//     as exported for those languages.
func isExportedName(name, language string) bool {
	if name == "" {
		return false
	}
	first := name[0]
	switch strings.ToLower(language) {
	case "go":
		return first >= 'A' && first <= 'Z'
	case "python", "py", "ruby", "rb":
		return first != '_'
	default:
		return first != '_'
	}
}

// isPascalCaseIdentifier returns true when name starts with an uppercase
// letter and contains at least one lowercase letter — the standard PascalCase
// React component naming convention (e.g. "TransferReceive", "PaymentForm").
// Pure ALL_CAPS names (like constants) are excluded.
func isPascalCaseIdentifier(name string) bool {
	if len(name) < 2 {
		return false
	}
	first := name[0]
	if first < 'A' || first > 'Z' {
		return false
	}
	// Require at least one lowercase character so CONSTANT_STYLE names
	// are not mistaken for React components.
	hasLower := false
	for _, c := range name {
		if c >= 'a' && c <= 'z' {
			hasLower = true
			break
		}
	}
	return hasLower
}

// isJSXSourceFile returns true when path has a .jsx or .tsx extension,
// indicating the file contains JSX syntax and is likely a React component.
func isJSXSourceFile(path string) bool {
	return strings.HasSuffix(path, ".jsx") || strings.HasSuffix(path, ".tsx")
}
