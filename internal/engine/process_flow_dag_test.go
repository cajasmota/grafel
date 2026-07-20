// Process-flow DAG walker tests (#1945 Phase 1).
//
// Exercises buildFlowDAG directly so we can assert the DAG shape
// (Branches structure, cycle gate, depth + fanout + node caps) without
// the noise of entry-point ranking + the rest of RunProcessFlow.
// End-to-end tests covering the persisted properties live alongside
// the existing process_flow_test.go cases.

package engine

import (
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// newAdj builds a callsAdjacency from a flat list of (from, to) pairs.
// Test-only helper — production paths go through buildCallsAdjacency.
func newAdj(edges ...[2]string) *callsAdjacency {
	a := &callsAdjacency{
		out:         make(map[string][]string),
		in:          make(map[string]int),
		fetches:     make(map[edgeKey]bool),
		phantom:     make(map[edgeKey]bool),
		handlerCont: make(map[edgeKey]bool),
	}
	seen := make(map[string]map[string]bool)
	for _, e := range edges {
		from, to := e[0], e[1]
		if seen[from] == nil {
			seen[from] = make(map[string]bool)
		}
		if seen[from][to] {
			continue
		}
		seen[from][to] = true
		a.out[from] = append(a.out[from], to)
		a.in[to]++
	}
	return a
}

func TestBuildFlowDAG_LinearChainHasNilBranches(t *testing.T) {
	// entry → a → b → c — single linear chain, no fan-out anywhere.
	adj := newAdj([2]string{"entry", "a"}, [2]string{"a", "b"}, [2]string{"b", "c"})
	res := buildFlowDAG("entry", adj, nil, dagWalkConfig{MaxDepth: 8, BranchingFactor: 4, MaxNodes: 50})
	if res.Root == nil {
		t.Fatal("Root is nil")
	}
	if res.BranchCount != 0 {
		t.Errorf("BranchCount = %d, want 0 for linear chain", res.BranchCount)
	}
	if res.NodeCount != 4 {
		t.Errorf("NodeCount = %d, want 4", res.NodeCount)
	}
	// Walk down: each step should have exactly one child until the leaf.
	cur := res.Root
	depth := 0
	for cur != nil {
		if depth < 3 {
			if len(cur.Branches) != 1 {
				t.Errorf("step %d: len(Branches) = %d, want 1", depth, len(cur.Branches))
			}
		} else {
			if cur.Branches != nil {
				t.Errorf("leaf step has non-nil Branches: %v", cur.Branches)
			}
		}
		if len(cur.Branches) == 0 {
			break
		}
		cur = cur.Branches[0]
		depth++
	}
	if got := primaryPath(res.Root); strings.Join(got, ",") != "entry,a,b,c" {
		t.Errorf("primaryPath = %v, want [entry a b c]", got)
	}
}

func TestBuildFlowDAG_TwoWayFanOut(t *testing.T) {
	// entry → {a, b} — single fan-out point. Both children appear in
	// Branches and the leftmost (sorted) is the primary path.
	adj := newAdj([2]string{"entry", "b"}, [2]string{"entry", "a"})
	res := buildFlowDAG("entry", adj, nil, dagWalkConfig{MaxDepth: 8, BranchingFactor: 4, MaxNodes: 50})
	if len(res.Root.Branches) != 2 {
		t.Fatalf("len(Branches) = %d, want 2", len(res.Root.Branches))
	}
	// Sorted by EntityID → "a" then "b".
	if res.Root.Branches[0].EntityID != "a" || res.Root.Branches[1].EntityID != "b" {
		t.Errorf("Branches order = [%s %s], want [a b]",
			res.Root.Branches[0].EntityID, res.Root.Branches[1].EntityID)
	}
	if res.BranchCount != 1 {
		t.Errorf("BranchCount = %d, want 1", res.BranchCount)
	}
	if res.NodeCount != 3 {
		t.Errorf("NodeCount = %d, want 3", res.NodeCount)
	}
}

func TestBuildFlowDAG_NestedFanOut(t *testing.T) {
	// entry → a → {b, c}
	// One fan-out at depth 2. Inner Branches must themselves be inspectable.
	adj := newAdj(
		[2]string{"entry", "a"},
		[2]string{"a", "b"},
		[2]string{"a", "c"},
	)
	res := buildFlowDAG("entry", adj, nil, dagWalkConfig{MaxDepth: 8, BranchingFactor: 4, MaxNodes: 50})
	if len(res.Root.Branches) != 1 {
		t.Fatalf("root.Branches = %d, want 1", len(res.Root.Branches))
	}
	a := res.Root.Branches[0]
	if a.EntityID != "a" {
		t.Fatalf("first branch entity = %q, want a", a.EntityID)
	}
	if len(a.Branches) != 2 {
		t.Errorf("a.Branches = %d, want 2", len(a.Branches))
	}
	for _, child := range a.Branches {
		if child.Branches != nil {
			t.Errorf("inner leaf %q has Branches=%v", child.EntityID, child.Branches)
		}
	}
	if res.BranchCount != 1 {
		t.Errorf("BranchCount = %d, want 1", res.BranchCount)
	}
}

func TestBuildFlowDAG_CyclePrevention(t *testing.T) {
	// entry → a → b → a (cycle back to a). The cycle MUST NOT cause
	// infinite expansion; the second visit of `a` is dropped.
	adj := newAdj(
		[2]string{"entry", "a"},
		[2]string{"a", "b"},
		[2]string{"b", "a"},
	)
	res := buildFlowDAG("entry", adj, nil, dagWalkConfig{MaxDepth: 8, BranchingFactor: 4, MaxNodes: 50})
	// Walk down — b should be a leaf (its only child `a` is on the
	// ancestor path).
	if res.Root == nil {
		t.Fatal("Root nil")
	}
	a := res.Root.Branches[0]
	if a == nil || a.EntityID != "a" {
		t.Fatalf("first branch = %+v, want EntityID=a", a)
	}
	b := a.Branches[0]
	if b == nil || b.EntityID != "b" {
		t.Fatalf("inner branch = %+v, want EntityID=b", b)
	}
	if len(b.Branches) != 0 {
		t.Errorf("b.Branches = %v, want empty (cycle should be cut)", b.Branches)
	}
}

func TestBuildFlowDAG_BranchFanoutCap(t *testing.T) {
	// entry has 7 outgoing edges; BranchingFactor=4 → 4 real children +
	// a "+3 more" overflow sentinel. FanoutTruncated counts the dropped 3.
	edges := [][2]string{}
	for _, name := range []string{"n0", "n1", "n2", "n3", "n4", "n5", "n6"} {
		edges = append(edges, [2]string{"entry", name})
	}
	adj := newAdj(edges...)
	res := buildFlowDAG("entry", adj, nil, dagWalkConfig{MaxDepth: 8, BranchingFactor: 4, MaxNodes: 50})
	if got := len(res.Root.Branches); got != 5 { // 4 real + 1 sentinel
		t.Errorf("len(Branches) = %d, want 5 (4 real + 1 sentinel)", got)
	}
	sentinel := res.Root.Branches[len(res.Root.Branches)-1]
	if sentinel.EntityID != branchOverflowEntityID {
		t.Errorf("last branch is not the overflow sentinel: %+v", sentinel)
	}
	if sentinel.Reason != "fanout_cap" {
		t.Errorf("sentinel.Reason = %q, want fanout_cap", sentinel.Reason)
	}
	if !strings.Contains(sentinel.Name, "3") {
		t.Errorf("sentinel.Name = %q, want to contain dropped count 3", sentinel.Name)
	}
	if res.FanoutTruncated != 3 {
		t.Errorf("FanoutTruncated = %d, want 3", res.FanoutTruncated)
	}
}

func TestBuildFlowDAG_DepthCap(t *testing.T) {
	// 12-deep linear chain, MaxDepth=4 → walker stops at depth 4 and
	// records the dropped edge in DepthTruncated.
	edges := [][2]string{}
	for i := 0; i < 12; i++ {
		from := "entry"
		if i > 0 {
			from = "n" + strconv.Itoa(i)
		}
		to := "n" + strconv.Itoa(i+1)
		edges = append(edges, [2]string{from, to})
	}
	adj := newAdj(edges...)
	res := buildFlowDAG("entry", adj, nil, dagWalkConfig{MaxDepth: 4, BranchingFactor: 4, MaxNodes: 50})
	path := primaryPath(res.Root)
	if len(path) > 4 {
		t.Errorf("primary path length = %d, want ≤ 4 (depth cap)", len(path))
	}
	if res.DepthTruncated == 0 {
		t.Errorf("DepthTruncated = 0, want > 0 — depth cap should have dropped edges")
	}
}

func TestBuildFlowDAG_NodeCap(t *testing.T) {
	// Wide tree: entry has 30 leaves. Default MaxNodes=50 → all fit.
	// Now lower MaxNodes=5 → walker stops after 5 nodes.
	edges := [][2]string{}
	for i := 0; i < 30; i++ {
		edges = append(edges, [2]string{"entry", "leaf" + strconv.Itoa(i)})
	}
	adj := newAdj(edges...)
	res := buildFlowDAG("entry", adj, nil, dagWalkConfig{MaxDepth: 8, BranchingFactor: 30, MaxNodes: 5})
	if res.NodeCount > 5 {
		t.Errorf("NodeCount = %d, want ≤ 5", res.NodeCount)
	}
	if !res.NodeCapHit {
		t.Errorf("NodeCapHit = false, want true")
	}
}

func TestBuildFlowDAG_EncodeDAGJSONRoundTrip(t *testing.T) {
	// The branches_dag property is a JSON-serialised ChainStep tree.
	// Sanity-check that linear chains round-trip and fan-out points
	// surface both children.
	adj := newAdj(
		[2]string{"entry", "a"},
		[2]string{"a", "b"},
		[2]string{"a", "c"},
	)
	byID := map[string]*graph.Entity{
		"entry": {ID: "entry", Name: "handleSubmit", SourceFile: "x.go", StartLine: 10},
		"a":     {ID: "a", Name: "validate", SourceFile: "x.go", StartLine: 20},
		"b":     {ID: "b", Name: "writeDB", SourceFile: "x.go", StartLine: 30},
		"c":     {ID: "c", Name: "writeLog", SourceFile: "x.go", StartLine: 40},
	}
	res := buildFlowDAG("entry", adj, byID, dagWalkConfig{MaxDepth: 8, BranchingFactor: 4, MaxNodes: 50})
	js := encodeDAGJSON(res.Root)
	if js == "" {
		t.Fatal("encodeDAGJSON returned empty")
	}
	for _, want := range []string{`"entity_id":"entry"`, `"entity_id":"a"`, `"entity_id":"b"`, `"entity_id":"c"`, `"name":"validate"`, `"branches"`} {
		if !strings.Contains(js, want) {
			t.Errorf("encoded DAG missing %q\nfull: %s", want, js)
		}
	}
}

func TestProcessFlow_EmitsDAGPropertiesOnBranchedFlow(t *testing.T) {
	// End-to-end: a branched entry should emit ONE Process with
	// is_dag=true, branch_count>0, dag_node_count>chain_len, and a
	// non-empty branches_dag JSON blob.
	doc := &graph.Document{Repo: "r"}
	doc.Entities = []graph.Entity{
		{ID: "entry", Name: "handleSubmit", Kind: "SCOPE.Function", Language: "go", SourceFile: "x.go"},
		{ID: "a", Name: "validate", Kind: "SCOPE.Function", Language: "go", SourceFile: "x.go"},
		{ID: "b", Name: "writeDB", Kind: "SCOPE.Function", Language: "go", SourceFile: "x.go"},
		{ID: "c", Name: "writeLog", Kind: "SCOPE.Function", Language: "go", SourceFile: "x.go"},
	}
	// entry → a, a → b, a → c — branched DAG at `a`.
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "entry", ToID: "a", Kind: "CALLS"},
		{ID: "2", FromID: "a", ToID: "b", Kind: "CALLS"},
		{ID: "3", FromID: "a", ToID: "c", Kind: "CALLS"},
	}
	stats := RunProcessFlow(doc, DefaultProcessFlowConfig())
	if stats.Processes != 1 {
		t.Fatalf("Processes = %d, want 1 (DAG collapses projections)", stats.Processes)
	}
	p := findProcess(doc)
	if p == nil {
		t.Fatal("no Process emitted")
	}
	if p.PropGet("is_dag") != "true" {
		t.Errorf("is_dag = %q, want true", p.PropGet("is_dag"))
	}
	if got := p.PropGet("branch_count"); got != "1" {
		t.Errorf("branch_count = %q, want 1", got)
	}
	if got := p.PropGet("dag_node_count"); got != "4" {
		t.Errorf("dag_node_count = %q, want 4", got)
	}
	if dagJSON := p.PropGet("branches_dag"); dagJSON == "" {
		t.Errorf("branches_dag property is empty")
	} else if !strings.Contains(dagJSON, `"entity_id":"c"`) {
		t.Errorf("branches_dag missing branch child: %s", dagJSON)
	}
}

func TestProcessFlow_LinearFlowMarkedNotDAG(t *testing.T) {
	// A purely linear chain should have is_dag=false and branch_count=0.
	doc := buildChainDoc("r", 4)
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	p := findProcess(doc)
	if p == nil {
		t.Fatal("no Process emitted")
	}
	if p.PropGet("is_dag") != "false" {
		t.Errorf("linear chain is_dag = %q, want false", p.PropGet("is_dag"))
	}
	if p.PropGet("branch_count") != "0" {
		t.Errorf("linear chain branch_count = %q, want 0", p.PropGet("branch_count"))
	}
}
