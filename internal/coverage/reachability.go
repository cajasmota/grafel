package coverage

import (
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// Static test-reachability (#5037).
//
// Without executing anything, this pass answers "which production functions /
// endpoints are exercised by at least one test?" purely from the graph. It
// runs a forward BFS from every Test entity over TESTS and CALLS edges (both
// point source→target — a test TESTS its subject, a caller CALLS its callee)
// and stamps every reached production entity as test_reachable.
//
// This complements #5036's dynamic LCOV line coverage: line coverage tells you
// what % of lines ran; reachability tells you which architectural surfaces have
// *zero* test path reaching them at all (orphans). See ReachabilityCrossSignal
// for the documented seam that combines the two.
//
// Like the LCOV attribution code, this is a pure transformation: it does not
// mutate inputs, touch Kinds, or call the indexer/daemon. The live wiring is a
// follow-up (see ApplyReachability and the package doc).

// Property keys stamped onto reachability-attributed entities. Stored in
// EntityRecord.Properties per the prefer-Properties-over-Kinds rule, mirroring
// the LCOV PropCoverage* keys.
const (
	// PropTestReachable is "true"/"false": is this production entity reachable
	// from at least one test over TESTS+CALLS edges?
	PropTestReachable = "test_reachable"
	// PropReachingTests is a comma-joined, capped list of test entity IDs that
	// reach this entity. Capped at reachingTestsCap; the full count is in
	// PropReachingTestCount.
	PropReachingTests = "reaching_tests"
	// PropReachingTestCount is the integer count of distinct reaching tests
	// (NOT capped — PropReachingTests may be truncated).
	PropReachingTestCount = "reaching_test_count"
	// PropReachDepth is the minimum hop count from any test to this entity.
	// A direct TESTS target is depth 1.
	PropReachDepth = "reach_depth"
)

// reachingTestsCap bounds the PropReachingTests list length so a widely-tested
// utility doesn't bloat the property map. The count is preserved separately.
const reachingTestsCap = 16

// reachabilityEdgeKinds are the edge kinds traversed by the reachability BFS.
// TESTS seeds the frontier (test → subject); CALLS propagates through the
// production call graph (caller → callee). Both are source→target oriented, so
// the BFS follows FromID → ToID.
var reachabilityEdgeKinds = map[string]bool{
	string(types.RelationshipKindTests): true,
	string(types.RelationshipKindCalls): true,
}

// Reachability is the computed static test-reachability for one production
// entity. Unreached production entities are also emitted (Reachable=false) so
// the caller can stamp an explicit negative rather than leaving the signal
// ambiguous.
type Reachability struct {
	EntityID      string
	Reachable     bool
	ReachingTests []string // distinct test entity IDs, sorted; uncapped here
	ReachDepth    int      // min hops from a test; 0 when unreachable
}

// Properties renders the reachability as the property map merged onto the
// entity. The reaching-tests list is capped at reachingTestsCap; the full count
// is always recorded.
func (r Reachability) Properties() map[string]string {
	props := map[string]string{
		PropTestReachable: strconv.FormatBool(r.Reachable),
	}
	if r.Reachable {
		props[PropReachDepth] = strconv.Itoa(r.ReachDepth)
		props[PropReachingTestCount] = strconv.Itoa(len(r.ReachingTests))
		list := r.ReachingTests
		if len(list) > reachingTestsCap {
			list = list[:reachingTestsCap]
		}
		props[PropReachingTests] = strings.Join(list, ",")
	}
	return props
}

// isProductionEntity reports whether an entity is "production" for the purpose
// of test-reachability roll-ups: real application surface area that we expect a
// test to exercise. The filter EXCLUDES:
//
//   - Test scaffolding: SCOPE.Pattern entities with subtype "test_suite" (the
//     kind/subtype the Go/Python/JS test extractors reuse for test entities),
//     plus anything tagged "test".
//   - Data shapes / contracts: Schema, Model, Enum, Constant, Variable, Table.
//   - Config & docs: Config, Document, Heading, MarkdownDocument, Section,
//     Template, Stylesheet, DesignDecision, Pattern (non-test patterns are
//     advisory, not call-graph surface).
//   - Aggregation / synthetic nodes: Module, Project, Package, Reference,
//     External, ExternalAPI, ExternalService, ScopeUnknown.
//
// It INCLUDES executable surface: Function, Operation, Method-bearing Class,
// Service, Endpoint, Route, Command, ServerlessFunction, View, UIComponent,
// GrpcMethod, DataAccess, DataLoader, Handler-ish kinds — i.e. anything a test
// path could plausibly reach and that we want a tested/untested verdict on.
//
// Endpoints are deliberately PRODUCTION here (they get their own reachability
// via their handler, see endpointReachability) so per-group roll-ups can report
// endpoint coverage too.
func isProductionEntity(e types.EntityRecord) bool {
	if isTestEntity(e) {
		return false
	}
	switch types.EntityKind(e.Kind) {
	case // executable / addressable surface — production
		types.EntityKindFunction,
		types.EntityKindOperation,
		types.EntityKindClass,
		types.EntityKindComponent,
		types.EntityKindService,
		types.EntityKindEndpoint,
		types.EntityKindRoute,
		types.EntityKindCommand,
		types.EntityKindServerlessFunction,
		types.EntityKindView,
		types.EntityKindUIComponent,
		types.EntityKindGrpcMethod,
		types.EntityKindGrpcService,
		types.EntityKindDataAccess,
		types.EntityKindDataLoader,
		types.EntityKindCustomValidator,
		types.EntityKindHTTPEndpointDefinition:
		return true
	default:
		return false
	}
}

// isTestEntity reports whether an entity is test scaffolding (and so neither a
// reachability target nor counted in production roll-ups). Tests are emitted as
// SCOPE.Pattern + subtype "test_suite" by the Go/Python/JS/Ruby extractors; we
// also honor a "test" tag and an explicit "test_suite" subtype on any kind for
// robustness across frameworks.
func isTestEntity(e types.EntityRecord) bool {
	if e.Subtype == subtypeTestSuite {
		return true
	}
	for _, t := range e.Tags {
		if strings.EqualFold(t, "test") {
			return true
		}
	}
	return false
}

const subtypeTestSuite = "test_suite"

// entitiesGraph indexes a batch of entities for traversal: id→entity and the
// adjacency list restricted to reachability edge kinds.
type entitiesGraph struct {
	byID  map[string]types.EntityRecord
	adj   map[string][]string // FromID → []ToID over TESTS+CALLS
	tests []string            // IDs of test entities (BFS seeds)
}

// buildGraph constructs the traversal index from a flat entity batch. Edges are
// read off each entity's embedded Relationships (FromID/ToID — the canonical
// field names; a past bug used the wrong ones). A relationship's FromID may be
// empty on extractor output (the edge implicitly originates at its host
// entity), so we fall back to the host entity's ID in that case.
func buildGraph(entities []types.EntityRecord) *entitiesGraph {
	g := &entitiesGraph{
		byID: make(map[string]types.EntityRecord, len(entities)),
		adj:  make(map[string][]string),
	}
	for _, e := range entities {
		id := entityID(e)
		g.byID[id] = e
		if isTestEntity(e) {
			g.tests = append(g.tests, id)
		}
	}
	for _, e := range entities {
		hostID := entityID(e)
		for _, rel := range e.Relationships {
			if !reachabilityEdgeKinds[rel.Kind] {
				continue
			}
			from := rel.FromID
			if from == "" {
				from = hostID
			}
			if rel.ToID == "" {
				continue
			}
			g.adj[from] = append(g.adj[from], rel.ToID)
		}
	}
	return g
}

// Reachability computes static test-reachability for every production entity in
// the batch. It runs one multi-source BFS from all test entities at once,
// tracking, per reached node, the minimum depth and the set of reaching tests.
//
// Only production entities (isProductionEntity) appear in the output. Reached
// production entities get Reachable=true with their depth and reaching tests;
// production entities never reached get an explicit Reachable=false. Test
// entities, schemas, configs, etc. are excluded entirely.
//
// Pure: inputs are not mutated.
func ComputeReachability(entities []types.EntityRecord) []Reachability {
	g := buildGraph(entities)

	// reached[id] = min depth; reachers[id] = set of test IDs that reach it.
	reached := make(map[string]int)
	reachers := make(map[string]map[string]bool)

	// Multi-source BFS. Each test seeds the frontier at depth 0; its direct
	// TESTS/CALLS targets are depth 1. We run a separate BFS per test so the
	// "which tests reach X" set is exact, but cap total work by skipping a
	// (test, node) pair once visited for that test.
	for _, testID := range g.tests {
		visited := map[string]bool{testID: true}
		type qi struct {
			id    string
			depth int
		}
		queue := []qi{{id: testID, depth: 0}}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, next := range g.adj[cur.id] {
				if visited[next] {
					continue
				}
				visited[next] = true
				d := cur.depth + 1
				if old, ok := reached[next]; !ok || d < old {
					reached[next] = d
				}
				if reachers[next] == nil {
					reachers[next] = map[string]bool{}
				}
				reachers[next][testID] = true
				queue = append(queue, qi{id: next, depth: d})
			}
		}
	}

	out := make([]Reachability, 0, len(entities))
	for _, e := range entities {
		if !isProductionEntity(e) {
			continue
		}
		id := entityID(e)
		depth, ok := reached[id]
		r := Reachability{EntityID: id, Reachable: ok, ReachDepth: depth}
		if ok {
			r.ReachingTests = sortedKeys(reachers[id])
		}
		out = append(out, r)
	}
	return out
}

// sortedKeys returns the deterministically-sorted keys of a string set.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ApplyReachability merges the reachability properties onto a copy of the
// entities, keyed by entityID. It returns a new slice; inputs are not mutated.
// This mirrors ApplyAttributions and is the same indexer-hook SEAM: the live
// enrichment pass can call it on a batch after extraction. It is deliberately
// NOT wired into the daemon write path in v1 (see package doc / follow-ups).
func ApplyReachability(entities []types.EntityRecord, reach []Reachability) []types.EntityRecord {
	byID := make(map[string]Reachability, len(reach))
	for _, r := range reach {
		byID[r.EntityID] = r
	}
	out := make([]types.EntityRecord, len(entities))
	copy(out, entities)
	for i := range out {
		r, ok := byID[entityID(out[i])]
		if !ok {
			continue
		}
		if out[i].Properties == nil {
			out[i].Properties = map[string]string{}
		} else {
			cp := make(map[string]string, len(out[i].Properties))
			for k, v := range out[i].Properties {
				cp[k] = v
			}
			out[i].Properties = cp
		}
		for k, v := range r.Properties() {
			out[i].Properties[k] = v
		}
	}
	return out
}

// EndpointReachability is an endpoint-level verdict: an endpoint is
// test-reachable if it is reached directly (an e2e test TESTS the endpoint) OR
// its handler function is reachable. The handler is linked by a HANDLES edge
// (handler --HANDLES--> endpoint), so we resolve the handler as the FromID of
// any HANDLES edge whose ToID is the endpoint.
type EndpointReachability struct {
	EndpointID string
	HandlerID  string // "" when no handler edge was found
	Reachable  bool
	ViaHandler bool // true when reachability came from the handler, not a direct TESTS
}

// ComputeEndpointReachability derives endpoint-level verdicts. It reuses the
// per-entity reachability (so the BFS runs once) and the HANDLES cross-link.
func ComputeEndpointReachability(entities []types.EntityRecord, reach []Reachability) []EndpointReachability {
	reachable := make(map[string]bool, len(reach))
	for _, r := range reach {
		if r.Reachable {
			reachable[r.EntityID] = true
		}
	}

	// handlerOf[endpointID] = handlerID, from HANDLES edges (handler→endpoint).
	handlerOf := make(map[string]string)
	for _, e := range entities {
		hostID := entityID(e)
		for _, rel := range e.Relationships {
			if rel.Kind != string(types.RelationshipKindHandles) {
				continue
			}
			from := rel.FromID
			if from == "" {
				from = hostID
			}
			if rel.ToID != "" {
				handlerOf[rel.ToID] = from
			}
		}
	}

	out := make([]EndpointReachability, 0)
	for _, e := range entities {
		if types.EntityKind(e.Kind) != types.EntityKindEndpoint &&
			types.EntityKind(e.Kind) != types.EntityKindHTTPEndpointDefinition {
			continue
		}
		id := entityID(e)
		er := EndpointReachability{EndpointID: id, HandlerID: handlerOf[id]}
		switch {
		case reachable[id]:
			// Direct TESTS edge to the endpoint (e2e).
			er.Reachable = true
		case er.HandlerID != "" && reachable[er.HandlerID]:
			er.Reachable = true
			er.ViaHandler = true
		}
		out = append(out, er)
	}
	return out
}

// RollUp is a tested/untested summary over a set of production entities. Pct is
// the percentage of production entities that are test-reachable, in [0,100].
type RollUp struct {
	Total     int     // production entities considered
	Reachable int     // of which test-reachable
	Pct       float64 // 100 * Reachable / Total; 0 when Total == 0
}

// RollUps is the group-level summary plus a per-module breakdown. Module is
// keyed by the entity SourceFile's directory, a stable proxy for "module" that
// needs no extra graph lookups (the graph has no universal module-membership
// edge). Callers that have a better module grouping can compute their own with
// RollUpBy.
type RollUps struct {
	Group   RollUp
	Modules map[string]RollUp
}

// ComputeRollUps returns the group-wide and per-module reachability roll-ups
// from the per-entity Reachability set joined back to the entities (for the
// module key). Only production entities are counted (the reach slice already
// contains exactly those).
func ComputeRollUps(entities []types.EntityRecord, reach []Reachability) RollUps {
	srcByID := make(map[string]string, len(entities))
	for _, e := range entities {
		srcByID[entityID(e)] = e.SourceFile
	}
	keyFn := func(r Reachability) string {
		return moduleKey(srcByID[r.EntityID])
	}
	return RollUps{
		Group:   rollUpAll(reach),
		Modules: RollUpBy(reach, keyFn),
	}
}

// RollUpBy buckets reachability verdicts by an arbitrary key function and
// returns a roll-up per bucket. Exposed so callers with a richer module model
// (e.g. CONTAINS edges) can group however they like.
func RollUpBy(reach []Reachability, key func(Reachability) string) map[string]RollUp {
	buckets := make(map[string]RollUp)
	for _, r := range reach {
		k := key(r)
		b := buckets[k]
		b.Total++
		if r.Reachable {
			b.Reachable++
		}
		buckets[k] = b
	}
	for k, b := range buckets {
		buckets[k] = finalizePct(b)
	}
	return buckets
}

func rollUpAll(reach []Reachability) RollUp {
	var b RollUp
	for _, r := range reach {
		b.Total++
		if r.Reachable {
			b.Reachable++
		}
	}
	return finalizePct(b)
}

func finalizePct(b RollUp) RollUp {
	if b.Total > 0 {
		b.Pct = 100.0 * float64(b.Reachable) / float64(b.Total)
	}
	return b
}

// moduleKey maps a source-file path to its module bucket (the containing
// directory). Files at repo root bucket under ".".
func moduleKey(src string) string {
	if src == "" {
		return "."
	}
	if i := strings.LastIndexAny(src, "/\\"); i >= 0 {
		return src[:i]
	}
	return "."
}

// CrossSignalVerdict classifies an entity by combining static reachability
// (this ticket, #5037) with #5036's dynamic LCOV line coverage. It is the
// documented seam toward archigraph_contract_test_effectiveness (#4893): an
// entity that is statically reachable from a test but has 0% measured line
// coverage is a candidate ineffective / tautological test (a test path exists
// in the graph, yet no production line of the entity actually executed).
//
// v1 only defines and computes the verdict from already-stamped Properties; it
// does NOT wire an MCP tool or a first-class report (both deferred — see PR).
type CrossSignalVerdict string

const (
	// CrossSignalTestedAndRun: reachable AND line_pct > 0 — healthy.
	CrossSignalTestedAndRun CrossSignalVerdict = "tested_and_run"
	// CrossSignalReachableNoLines: reachable BUT line_pct == 0 — candidate
	// ineffective/tautological test (the #4893 signal).
	CrossSignalReachableNoLines CrossSignalVerdict = "reachable_no_lines"
	// CrossSignalUntested: not reachable from any test.
	CrossSignalUntested CrossSignalVerdict = "untested"
	// CrossSignalUnknown: missing one of the two signals (e.g. entity absent
	// from the LCOV report) — cannot cross.
	CrossSignalUnknown CrossSignalVerdict = "unknown"
)

// CrossSignal reads the reachability + LCOV properties already stamped on an
// entity (via ApplyReachability and ApplyAttributions) and returns the combined
// verdict. It is a pure function over Properties so the eventual MCP tool /
// report can call it without re-running either pass.
func CrossSignal(props map[string]string) CrossSignalVerdict {
	reachStr, hasReach := props[PropTestReachable]
	if !hasReach {
		return CrossSignalUnknown
	}
	reachable, _ := strconv.ParseBool(reachStr)
	if !reachable {
		return CrossSignalUntested
	}
	pctStr, hasPct := props[PropCoveragePct]
	if !hasPct {
		// Reachable, but no line-coverage signal to cross with.
		return CrossSignalUnknown
	}
	pct, _ := strconv.ParseFloat(pctStr, 64)
	if pct == 0 {
		return CrossSignalReachableNoLines
	}
	return CrossSignalTestedAndRun
}
