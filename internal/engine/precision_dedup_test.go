package engine

// precision_dedup_test.go — value-asserting regression tests for issue #3729
// (epic #3628 area #24). Each test asserts SPECIFIC before/after counts, not
// len > 0: a symbol emitted under two kinds collapses to exactly one canonical
// node with both kinds' edges preserved; a statement-shaped Operation name is
// dropped; a real operation is kept.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// TestPrecision_MultiKindCollapse_KeepsOneCanonicalAndAllEdges builds a symbol
// emitted under two kinds (Component + View) for the same
// (Name, SourceFile, StartLine), each carrying a distinct edge, then asserts:
//   - before: 2 entities for the symbol
//   - after:  exactly 1 entity, kind == the most-specific (View)
//   - both edges survive, with the Component edge's endpoint rewritten to the
//     canonical View symbolic ID.
func TestPrecision_MultiKindCollapse_KeepsOneCanonicalAndAllEdges(t *testing.T) {
	entities := []types.EntityRecord{
		{Name: "AocViewSet", Kind: "View", SourceFile: "core/views.py", StartLine: 10,
			Properties: map[string]string{"framework": "django"}},
		{Name: "AocViewSet", Kind: "Component", SourceFile: "core/views.py", StartLine: 10,
			Properties: map[string]string{"framework": "falcon", "extra": "x"}},
		// An unrelated single-kind symbol that must pass through untouched.
		{Name: "OtherModel", Kind: "Model", SourceFile: "core/models.py", StartLine: 3},
	}
	rels := []types.RelationshipRecord{
		// Edge anchored to the View (canonical) — must remain.
		{FromID: "View:AocViewSet", ToID: "Route:/aoc", Kind: "ROUTES_TO"},
		// Edge anchored to the Component (phantom) — must be rewritten to View.
		{FromID: "Caller:do_thing", ToID: "Component:AocViewSet", Kind: "CALLS"},
	}

	// Sanity: two entities exist for the symbol before the pass.
	preCount := 0
	for _, e := range entities {
		if e.Name == "AocViewSet" {
			preCount++
		}
	}
	if preCount != 2 {
		t.Fatalf("setup: expected 2 AocViewSet entities before pass, got %d", preCount)
	}

	gotEnts, gotRels := applyPrecisionDedup(entities, rels)

	// Exactly one AocViewSet survives, and it is the View.
	var aoc []types.EntityRecord
	for _, e := range gotEnts {
		if e.Name == "AocViewSet" {
			aoc = append(aoc, e)
		}
	}
	if len(aoc) != 1 {
		t.Fatalf("expected exactly 1 AocViewSet entity after collapse, got %d", len(aoc))
	}
	if aoc[0].Kind != "View" {
		t.Errorf("canonical kind: want View, got %s", aoc[0].Kind)
	}
	// Merged property from the dropped Component must be present; survivor's own
	// framework value must win on conflict.
	if aoc[0].Properties["extra"] != "x" {
		t.Errorf("expected merged property extra=x from dropped Component, got %q", aoc[0].Properties["extra"])
	}
	if aoc[0].Properties["framework"] != "django" {
		t.Errorf("survivor property framework should win (django), got %q", aoc[0].Properties["framework"])
	}

	// The unrelated Model passes through.
	var sawModel bool
	for _, e := range gotEnts {
		if e.Name == "OtherModel" && e.Kind == "Model" {
			sawModel = true
		}
	}
	if !sawModel {
		t.Error("unrelated single-kind Model entity was lost")
	}

	// Total entity count: 3 → 2 (one collapse).
	if len(gotEnts) != 2 {
		t.Errorf("entity count: want 2 after collapse, got %d", len(gotEnts))
	}

	// Both edges survive; the Component edge is rewritten to View.
	if len(gotRels) != 2 {
		t.Fatalf("edge count: want 2 (both preserved), got %d", len(gotRels))
	}
	var sawRoute, sawRewritten bool
	for _, r := range gotRels {
		if r.FromID == "View:AocViewSet" && r.ToID == "Route:/aoc" {
			sawRoute = true
		}
		if r.FromID == "Caller:do_thing" && r.ToID == "View:AocViewSet" {
			sawRewritten = true
		}
		if r.ToID == "Component:AocViewSet" {
			t.Errorf("edge still references dropped Component ID: %+v", r)
		}
	}
	if !sawRoute {
		t.Error("canonical View edge (ROUTES_TO) was lost")
	}
	if !sawRewritten {
		t.Error("phantom Component edge was not rewritten to the canonical View ID")
	}
}

// TestPrecision_StatementNoiseDropped_RealOperationKept asserts the
// statement-noise filter drops a non-identifier Operation while keeping a
// legitimately-named one, and drops only edges anchored to the junk node.
func TestPrecision_StatementNoiseDropped_RealOperationKept(t *testing.T) {
	entities := []types.EntityRecord{
		{Name: "@tool", Kind: "Operation", SourceFile: "a.py", StartLine: 4},         // junk
		{Name: "msg = f\"hi\"", Kind: "Operation", SourceFile: "a.py", StartLine: 5}, // junk
		{Name: "my_chain", Kind: "Operation", SourceFile: "a.py", StartLine: 12},     // real
		{Name: "return foo", Kind: "Operation", SourceFile: "a.py", StartLine: 6},    // junk
		// A non-Operation entity with a weird name must NOT be touched.
		{Name: "GET /x", Kind: "Route", SourceFile: "a.py", StartLine: 1},
	}
	rels := []types.RelationshipRecord{
		{FromID: "Operation:@tool", ToID: "Service:agent", Kind: "USES"},    // anchored to junk → dropped
		{FromID: "Operation:my_chain", ToID: "Service:agent", Kind: "USES"}, // anchored to real → kept
	}

	gotEnts, gotRels := applyPrecisionDedup(entities, rels)

	gotNames := map[string]string{} // name → kind
	for _, e := range gotEnts {
		gotNames[e.Name] = e.Kind
	}
	if _, ok := gotNames["@tool"]; ok {
		t.Error("junk Operation '@tool' was not dropped")
	}
	if _, ok := gotNames["msg = f\"hi\""]; ok {
		t.Error("junk Operation 'msg = f\"hi\"' was not dropped")
	}
	if _, ok := gotNames["return foo"]; ok {
		t.Error("junk Operation 'return foo' was not dropped")
	}
	if gotNames["my_chain"] != "Operation" {
		t.Errorf("real Operation 'my_chain' should be kept, got kind %q", gotNames["my_chain"])
	}
	if gotNames["GET /x"] != "Route" {
		t.Error("non-Operation Route 'GET /x' must never be touched by the noise filter")
	}

	// 5 entities → 2 survive (my_chain + GET /x).
	if len(gotEnts) != 2 {
		t.Errorf("entity count: want 2 after noise filter, got %d", len(gotEnts))
	}

	// Only the edge anchored to my_chain survives.
	if len(gotRels) != 1 {
		t.Fatalf("edge count: want 1 (junk-anchored edge dropped), got %d", len(gotRels))
	}
	if gotRels[0].FromID != "Operation:my_chain" {
		t.Errorf("surviving edge should be the my_chain edge, got %+v", gotRels[0])
	}
}

// TestPrecision_OptOut verifies the pass is a no-op when disabled.
func TestPrecision_OptOut(t *testing.T) {
	entities := []types.EntityRecord{
		{Name: "@tool", Kind: "Operation", SourceFile: "a.py", StartLine: 4},
		{Name: "X", Kind: "View", SourceFile: "a.py", StartLine: 1},
		{Name: "X", Kind: "Component", SourceFile: "a.py", StartLine: 1},
	}
	PrecisionDedupEnabled = false
	defer func() { PrecisionDedupEnabled = true }()

	gotEnts, _ := applyPrecisionDedup(entities, nil)
	if len(gotEnts) != 3 {
		t.Errorf("opt-out: expected all 3 entities preserved, got %d", len(gotEnts))
	}
}

// TestPrecision_IsStatementNoiseOperationName tables the NARROW noise
// classifier. true ⇒ junk (dropped); false ⇒ kept. Crucially, legitimate
// call-idiom Operation names used elsewhere in the codebase must be KEPT.
func TestPrecision_IsStatementNoiseOperationName(t *testing.T) {
	cases := []struct {
		name string
		want bool // true = noise (drop)
	}{
		// Junk — dropped.
		{"", true},
		{"@tool", true},
		{"@property", true},
		{"msg = f\"hi\"", true},
		{"x = 1", true},
		{"return foo", true},
		{"yield x", true},
		// Real / kept — including the intentional call-idiom names used by the
		// langchain + trpc detectors.
		{"my_chain", false},
		{"send_email", false},
		{"AocViewSet", false},
		{"RunnableSequence.from(", false}, // langchain composition idiom
		{".bindTools(", false},            // langchain tool-binding idiom
		{"tool(async (", false},           // langchain tool() factory idiom
		{"createTRPCClient<", false},      // trpc client-codegen idiom
		{"x == y", false},                 // comparison, not assignment
		{"a >= b", false},
		{"@tool(", false}, // decorator WITH args is a call idiom — kept
	}
	for _, c := range cases {
		if got := isStatementNoiseOperationName(c.name); got != c.want {
			t.Errorf("isStatementNoiseOperationName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestPrecision_Integration_LangchainToolNoise drives the real rule set through
// the detector and asserts the langchain `@tool` Operation noise (reproducible
// on main) is gone after the pass, while a real chain variable survives.
func TestPrecision_Integration_LangchainToolNoise(t *testing.T) {
	const src = `from langchain.tools import tool

@tool
def search(query: str) -> str:
    return "result"

my_chain = prompt | model | parser
`
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path: "core/agents.py", Content: []byte(src), Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	var toolNoise, realChain int
	for _, e := range res.Entities {
		if e.Kind == "Operation" && e.Name == "@tool" {
			toolNoise++
		}
		if e.Kind == "Operation" && e.Name == "my_chain" {
			realChain++
		}
	}
	if toolNoise != 0 {
		t.Errorf("expected 0 '@tool' statement-noise Operation entities after pass, got %d", toolNoise)
	}
	if realChain != 1 {
		t.Errorf("expected the real 'my_chain' Operation to survive (1), got %d", realChain)
	}
}

// TestPrecision_RouteShapedOperationSurvivesNoiseFilter (#5733, refs #5688)
// asserts a route-shaped SCOPE.Operation(subtype=endpoint, route_path=...)
// — the shape internal/custom/php/frameworks.go stamps for non-Laravel PHP
// frameworks (Slim, CakePHP, CodeIgniter, Yii, WordPress, …) — is EXEMPT
// from dropStatementNoiseOperations even though its captured route path
// ("/users/:id") trips the noise heuristics tuned for source-code
// statements. A genuine statement-noise Operation with the SAME kind must
// still be pruned, so the guard doesn't just disable the filter wholesale.
func TestPrecision_RouteShapedOperationSurvivesNoiseFilter(t *testing.T) {
	entities := []types.EntityRecord{
		// Route-shaped: subtype=endpoint + route_path property. Must survive
		// even though nothing about "/users/:id" looks like a real identifier.
		{
			Name: "/users/:id", Kind: "SCOPE.Operation", Subtype: "endpoint",
			SourceFile: "public/index.php", StartLine: 12,
			Properties: map[string]string{"framework": "slim", "route_path": "/users/:id"},
		},
		// Genuine statement noise with the SAME namespaced kind — must still
		// be dropped; the guard must not blanket-disable the filter.
		{Name: "@tool", Kind: "SCOPE.Operation", SourceFile: "a.php", StartLine: 4},
		// A bare "Operation" kind route (legacy/rule-engine form) with the
		// route-shape properties must ALSO survive.
		{
			Name: "/health", Kind: "Operation", Subtype: "endpoint",
			SourceFile: "routes.php", StartLine: 3,
			Properties: map[string]string{"route": "/health"},
		},
		// Real (non-route) Operation must be kept regardless.
		{Name: "my_chain", Kind: "Operation", SourceFile: "a.php", StartLine: 12},
	}

	gotEnts, _ := applyPrecisionDedup(entities, nil)

	gotNames := map[string]bool{}
	for _, e := range gotEnts {
		gotNames[e.Name] = true
	}
	if !gotNames["/users/:id"] {
		t.Error("route-shaped SCOPE.Operation('/users/:id') must survive the noise filter")
	}
	if !gotNames["/health"] {
		t.Error("route-shaped Operation('/health') must survive the noise filter")
	}
	if gotNames["@tool"] {
		t.Error("genuine statement-noise SCOPE.Operation('@tool') must still be dropped")
	}
	if !gotNames["my_chain"] {
		t.Error("real Operation('my_chain') must be kept")
	}
	if len(gotEnts) != 3 {
		t.Errorf("entity count: want 3 survivors (2 routes + 1 real op), got %d: %+v", len(gotEnts), gotEnts)
	}
}
