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
	id    string
	name  string
	kind  string
	score float64
}

// rankEntryPoints scores every Function/Method/Operation/Component entity
// and returns the candidates in descending score order. Candidates with
// score ≤ 0 (heavy utility penalty, no fan-out) are dropped.
func rankEntryPoints(doc *graph.Document, byID map[string]*graph.Entity, adj *callsAdjacency, cfg ProcessFlowConfig) []entryCandidate {
	// HTTP-boundary signal: any entity on either side of an IMPLEMENTS /
	// ROUTES_TO / SERVES edge is almost certainly an entry point (or the
	// endpoint it serves).
	httpBoundary := buildHTTPBoundarySet(doc)

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
		if httpBoundary[e.ID] {
			score += 6.0
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
		}

		if score <= 0 {
			continue
		}
		out = append(out, entryCandidate{id: e.ID, name: e.Name, kind: e.Kind, score: score})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].id < out[j].id
	})
	return out
}

// isEntryCandidate filters to entity kinds that can host a CALLS chain.
// Routes and Endpoints themselves are targets of IMPLEMENTS edges, not
// origins of CALLS; the handler entity (Function/Method/Operation) is what
// actually emits the CALLS chain.
func isEntryCandidate(e *graph.Entity) bool {
	switch e.Kind {
	case "SCOPE.Function", "SCOPE.Operation", "SCOPE.Component", "SCOPE.Class":
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
