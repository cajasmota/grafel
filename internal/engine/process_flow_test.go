package engine

import (
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// buildChainDoc constructs a synthetic graph: one entry function calls
// step1, which calls step2, …, up to depth+1 nodes total.
func buildChainDoc(repo string, depth int) *graph.Document {
	doc := &graph.Document{Repo: repo}
	prev := ""
	for i := 0; i <= depth; i++ {
		name := "handleSubmit"
		if i > 0 {
			name = "step" + strconv.Itoa(i)
		}
		id := "n" + strconv.Itoa(i)
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:         id,
			Name:       name,
			Kind:       "SCOPE.Function",
			Language:   "go",
			SourceFile: "main.go",
		})
		if prev != "" {
			doc.Relationships = append(doc.Relationships, graph.Relationship{
				ID:     "r" + strconv.Itoa(i),
				FromID: prev,
				ToID:   id,
				Kind:   "CALLS",
			})
		}
		prev = id
	}
	return doc
}

func countProcesses(doc *graph.Document) int {
	n := 0
	for _, e := range doc.Entities {
		if e.Kind == EntityKindProcess {
			n++
		}
	}
	return n
}

func TestProcessFlow_EmitsLinearChain(t *testing.T) {
	doc := buildChainDoc("r", 4) // 5 nodes
	stats := RunProcessFlow(doc, DefaultProcessFlowConfig())
	if stats.Processes != 1 {
		t.Fatalf("want 1 process, got %d", stats.Processes)
	}
	if got := countProcesses(doc); got != 1 {
		t.Fatalf("emitted entity count = %d, want 1", got)
	}
	var stepEdges, entryEdges int
	for _, r := range doc.Relationships {
		switch r.Kind {
		case RelationshipKindStepInProcess:
			stepEdges++
		case RelationshipKindEntryPointOf:
			entryEdges++
		}
	}
	if stepEdges != 5 {
		t.Errorf("step edges = %d, want 5", stepEdges)
	}
	if entryEdges != 1 {
		t.Errorf("entry edges = %d, want 1", entryEdges)
	}
}

func TestProcessFlow_DepthCap(t *testing.T) {
	// 20-deep chain, MaxDepth=10 → emitted chain length ≤ 11 (entry + 10).
	doc := buildChainDoc("r", 20)
	cfg := DefaultProcessFlowConfig()
	cfg.MaxDepth = 10
	RunProcessFlow(doc, cfg)
	var procChain string
	for _, e := range doc.Entities {
		if e.Kind == EntityKindProcess {
			procChain = e.PropGet("chain")
			break
		}
	}
	if procChain == "" {
		t.Fatal("no Process emitted")
	}
	steps := strings.Split(procChain, ",")
	if len(steps) > 11 {
		t.Errorf("depth-capped chain has %d steps, want ≤ 11", len(steps))
	}
}

func TestProcessFlow_BranchingCap(t *testing.T) {
	// Entry node with 10 outgoing CALLS, each to a leaf. BranchingFactor=4
	// should result in only 4 surviving leaf chains.
	doc := &graph.Document{Repo: "r"}
	doc.Entities = append(doc.Entities, graph.Entity{
		ID: "entry", Name: "handleSubmit", Kind: "SCOPE.Function", Language: "go", SourceFile: "x.go",
	})
	for i := 0; i < 10; i++ {
		id := "leaf" + strconv.Itoa(i)
		doc.Entities = append(doc.Entities, graph.Entity{
			ID: id, Name: "leaf", Kind: "SCOPE.Function", Language: "go", SourceFile: "x.go",
		})
		// Add a second hop so MinSteps (=3) is satisfied.
		mid := "mid" + strconv.Itoa(i)
		doc.Entities = append(doc.Entities, graph.Entity{
			ID: mid, Name: "mid", Kind: "SCOPE.Function", Language: "go", SourceFile: "x.go",
		})
		doc.Relationships = append(doc.Relationships,
			graph.Relationship{ID: "e1_" + strconv.Itoa(i), FromID: "entry", ToID: mid, Kind: "CALLS"},
			graph.Relationship{ID: "e2_" + strconv.Itoa(i), FromID: mid, ToID: id, Kind: "CALLS"},
		)
	}
	cfg := DefaultProcessFlowConfig()
	cfg.BranchingFactor = 4
	stats := RunProcessFlow(doc, cfg)
	if stats.Processes > 4 {
		t.Errorf("branching-capped processes = %d, want ≤ 4", stats.Processes)
	}
	if stats.TruncatedFanout == 0 {
		t.Errorf("expected fanout truncation to be reported")
	}
}

func TestProcessFlow_DedupByEntryTerminal(t *testing.T) {
	// Diamond: entry → A → terminal, entry → B → terminal.
	// Both paths share (entry, terminal). Expect 1 Process per unique
	// (entry, terminal) pair regardless of intermediate branches.
	doc := &graph.Document{Repo: "r"}
	for _, n := range []string{"entry", "A", "B", "terminal"} {
		doc.Entities = append(doc.Entities, graph.Entity{
			ID: n, Name: n, Kind: "SCOPE.Function", Language: "go", SourceFile: "x.go",
		})
	}
	for _, e := range [][3]string{
		{"entry", "A", "1"},
		{"entry", "B", "2"},
		{"A", "terminal", "3"},
		{"B", "terminal", "4"},
	} {
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID: "e" + e[2], FromID: e[0], ToID: e[1], Kind: "CALLS",
		})
	}
	// Rename so name match prefers `entry` not utility.
	doc.Entities[0].Name = "handleSubmit"
	stats := RunProcessFlow(doc, DefaultProcessFlowConfig())
	if stats.Processes != 1 {
		t.Errorf("dedupe: got %d processes, want 1", stats.Processes)
	}
}

func TestProcessFlow_CrossStack(t *testing.T) {
	// #754 — repo-aware cross_stack: a chain that lands on a CONSUMER-side
	// synthetic http_endpoint (pattern_type=http_endpoint_client_synthesis)
	// crosses a repo boundary. The consumer synthetic is the bridge node
	// the cross-repo HTTP linker pairs with a producer in another repo.
	doc := &graph.Document{Repo: "r"}
	doc.Entities = []graph.Entity{
		{ID: "entry", Name: "handleSubmit", Kind: "SCOPE.Function", Language: "ts", SourceFile: "x.ts"},
		{ID: "svc", Name: "callService", Kind: "SCOPE.Function", Language: "ts", SourceFile: "x.ts"},
		graph.Entity{
			ID: "ep", Name: "http:POST:/api/orders", Kind: "http_endpoint", Language: "ts", SourceFile: "api.ts",
		}.WithProperties(map[string]string{
			"pattern_type": "http_endpoint_client_synthesis",
		},
		),
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "entry", ToID: "svc", Kind: "CALLS"},
		{ID: "2", FromID: "svc", ToID: "ep", Kind: "CALLS"},
	}
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	var got string
	for _, e := range doc.Entities {
		if e.Kind == EntityKindProcess {
			got = e.PropGet("cross_stack")
			break
		}
	}
	if got != "true" {
		t.Errorf("cross_stack = %q, want true", got)
	}
}

func TestProcessFlow_CrossStackViaFetches(t *testing.T) {
	// #754 — chain reaches a consumer endpoint via a FETCHES edge. The
	// presence of the FETCHES edge alone is the canonical cross-stack
	// signal: caller → consumer http_endpoint represents a real
	// cross-repo call site.
	doc := &graph.Document{Repo: "r"}
	doc.Entities = []graph.Entity{
		{ID: "entry", Name: "handleSubmit", Kind: "SCOPE.Function", Language: "ts", SourceFile: "x.ts"},
		{ID: "svc", Name: "callService", Kind: "SCOPE.Function", Language: "ts", SourceFile: "x.ts"},
		graph.Entity{
			ID: "ep", Name: "http:POST:/api/orders", Kind: "http_endpoint", Language: "ts", SourceFile: "x.ts",
		}.WithProperties(map[string]string{"pattern_type": "http_endpoint_client_synthesis"}),
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "entry", ToID: "svc", Kind: "CALLS"},
		{ID: "2", FromID: "svc", ToID: "ep", Kind: "FETCHES"},
	}
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	var got, reason string
	for _, e := range doc.Entities {
		if e.Kind == EntityKindProcess {
			got = e.PropGet("cross_stack")
			reason = e.PropGet("cross_stack_reason")
			break
		}
	}
	if got != "true" {
		t.Errorf("cross_stack = %q, want true (via FETCHES)", got)
	}
	if reason == "" {
		t.Errorf("cross_stack_reason missing; want FETCHES-step annotation")
	}
}

func TestProcessFlow_InternalHandlerNotCrossStack(t *testing.T) {
	// #754 — regression for fixture-d false-labeling. An intra-repo HTTP
	// handler that IMPLEMENTS a producer-side endpoint synthetic and
	// terminates in an external library is NOT cross_stack (the BFS never
	// leaves the source repo). It IS crosses_external_lib=true.
	doc := &graph.Document{Repo: "r"}
	doc.Entities = []graph.Entity{
		{ID: "h", Name: "handleOrders", Kind: "SCOPE.Function", Language: "py", SourceFile: "api.py"},
		{ID: "svc", Name: "callService", Kind: "SCOPE.Function", Language: "py", SourceFile: "api.py"},
		{ID: "db", Name: "writeRecord", Kind: "SCOPE.Function", Language: "py", SourceFile: "db.py"},
		graph.Entity{
			ID: "ep", Name: "http:POST:/api/orders", Kind: "http_endpoint", Language: "py", SourceFile: "api.py",
		}.WithProperties(map[string]string{"pattern_type": "http_endpoint_synthesis"}),
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "h", ToID: "svc", Kind: "CALLS"},
		{ID: "2", FromID: "svc", ToID: "db", Kind: "CALLS"},
		{ID: "3", FromID: "h", ToID: "ep", Kind: "IMPLEMENTS"},
	}
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	var crossStack, externalLib string
	for _, e := range doc.Entities {
		if e.Kind == EntityKindProcess {
			crossStack = e.PropGet("cross_stack")
			externalLib = e.PropGet("crosses_external_lib")
			break
		}
	}
	if crossStack != "false" {
		t.Errorf("intra-repo handler: cross_stack = %q, want false", crossStack)
	}
	if externalLib != "true" {
		t.Errorf("intra-repo handler touching HTTP boundary: crosses_external_lib = %q, want true", externalLib)
	}
}

func TestProcessFlow_ExternalLibTerminalNotCrossStack(t *testing.T) {
	// #754 — chain terminating in SCOPE.External / SCOPE.ExternalAPI is
	// crosses_external_lib=true but NOT cross_stack=true.
	doc := &graph.Document{Repo: "r"}
	doc.Entities = []graph.Entity{
		{ID: "entry", Name: "handleJob", Kind: "SCOPE.Function", Language: "java", SourceFile: "Job.java"},
		{ID: "svc", Name: "doWork", Kind: "SCOPE.Function", Language: "java", SourceFile: "Job.java"},
		{ID: "ext", Name: "jakarta.enterprise", Kind: "SCOPE.External", Language: "java", SourceFile: ""},
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "entry", ToID: "svc", Kind: "CALLS"},
		{ID: "2", FromID: "svc", ToID: "ext", Kind: "CALLS"},
	}
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	var crossStack, externalLib string
	for _, e := range doc.Entities {
		if e.Kind == EntityKindProcess {
			crossStack = e.PropGet("cross_stack")
			externalLib = e.PropGet("crosses_external_lib")
			break
		}
	}
	if crossStack != "false" {
		t.Errorf("external-lib terminal: cross_stack = %q, want false", crossStack)
	}
	if externalLib != "true" {
		t.Errorf("external-lib terminal: crosses_external_lib = %q, want true", externalLib)
	}
}

func TestProcessFlow_MinStepsDropsTrivial(t *testing.T) {
	// Two-node chain: entry → leaf. Below MinSteps=3 → no Process.
	doc := &graph.Document{Repo: "r"}
	doc.Entities = []graph.Entity{
		{ID: "entry", Name: "handleSubmit", Kind: "SCOPE.Function", Language: "go", SourceFile: "x.go"},
		{ID: "leaf", Name: "leaf", Kind: "SCOPE.Function", Language: "go", SourceFile: "x.go"},
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "entry", ToID: "leaf", Kind: "CALLS"},
	}
	stats := RunProcessFlow(doc, DefaultProcessFlowConfig())
	if stats.Processes != 0 {
		t.Errorf("trivial chain emitted %d processes, want 0", stats.Processes)
	}
}

func TestProcessFlow_LowConfidenceCallsSkipped(t *testing.T) {
	// CALLS edge with confidence=0.3 should not be traversed.
	doc := &graph.Document{Repo: "r"}
	doc.Entities = []graph.Entity{
		{ID: "entry", Name: "handleSubmit", Kind: "SCOPE.Function", Language: "go", SourceFile: "x.go"},
		{ID: "a", Name: "a", Kind: "SCOPE.Function", Language: "go", SourceFile: "x.go"},
		{ID: "b", Name: "b", Kind: "SCOPE.Function", Language: "go", SourceFile: "x.go"},
		{ID: "c", Name: "c", Kind: "SCOPE.Function", Language: "go", SourceFile: "x.go"},
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "entry", ToID: "a", Kind: "CALLS"},
		{ID: "2", FromID: "a", ToID: "b", Kind: "CALLS"},
		graph.Relationship{ID: "3", FromID: "b", ToID: "c", Kind: "CALLS"}.WithProperties(map[string]string{"confidence": "0.3"}),
	}
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	var chain string
	for _, e := range doc.Entities {
		if e.Kind == EntityKindProcess {
			chain = e.PropGet("chain")
			break
		}
	}
	if chain == "" {
		t.Fatal("expected one Process")
	}
	if strings.Contains(chain, "c") {
		t.Errorf("low-confidence edge was traversed; chain=%q", chain)
	}
}

func TestProcessFlow_NilDocumentIsSafe(t *testing.T) {
	RunProcessFlow(nil, DefaultProcessFlowConfig())
}

func TestProcessFlow_DeterministicAcrossRuns(t *testing.T) {
	doc1 := buildChainDoc("r", 5)
	doc2 := buildChainDoc("r", 5)
	RunProcessFlow(doc1, DefaultProcessFlowConfig())
	RunProcessFlow(doc2, DefaultProcessFlowConfig())
	if len(doc1.Entities) != len(doc2.Entities) {
		t.Fatalf("non-deterministic entity count: %d vs %d", len(doc1.Entities), len(doc2.Entities))
	}
	for i := range doc1.Entities {
		if doc1.Entities[i].ID != doc2.Entities[i].ID {
			t.Errorf("entity[%d] id mismatch: %q vs %q", i, doc1.Entities[i].ID, doc2.Entities[i].ID)
		}
	}
}

func TestProcessFlow_PhantomEdgeCrossStack(t *testing.T) {
	// #769 — a phantom CALLS edge (cross_repo="true") injected by the
	// phantom-edge pass should mark the resulting Process cross_stack=true
	// with a cross_stack_reason indicating the phantom edge step.
	doc := &graph.Document{Repo: "fixture-b"}
	doc.Entities = []graph.Entity{
		{ID: "entry", Name: "fetchUsers", Kind: "SCOPE.Function", Language: "ts", SourceFile: "svc.ts"},
		{ID: "caller", Name: "callUsers", Kind: "SCOPE.Function", Language: "ts", SourceFile: "svc.ts"},
		// phantom target lives in another repo — NOT in doc.Entities
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "entry", ToID: "caller", Kind: "CALLS"},
		graph.Relationship{
			ID:     "2",
			FromID: "caller",
			ToID:   "fixture-a-handler-xyz",
			Kind:   "CALLS",
		}.WithProperties(map[string]string{
			"cross_repo":  "true",
			"target_repo": "fixture-a",
			"link_method": "http",
			"via":         "phantom_edge_pass_#769 group=test",
		},
		),
	}
	RunProcessFlow(doc, DefaultProcessFlowConfig())

	var crossStack, reason string
	for _, e := range doc.Entities {
		if e.Kind == EntityKindProcess {
			crossStack = e.PropGet("cross_stack")
			reason = e.PropGet("cross_stack_reason")
			break
		}
	}
	if crossStack != "true" {
		t.Errorf("cross_stack = %q, want true for phantom-edge chain", crossStack)
	}
	if reason == "" {
		t.Errorf("cross_stack_reason should be set for phantom-edge chains")
	}
	if !strings.Contains(reason, "phantom") {
		t.Errorf("cross_stack_reason %q should mention 'phantom'", reason)
	}
}

func TestProcessFlow_PhantomEdgeMinStepsRelaxed(t *testing.T) {
	// #769 — a 2-step chain caller→phantom should survive even when
	// cfg.MinSteps=3, because the phantom edge relaxes the gate.
	doc := &graph.Document{Repo: "b"}
	doc.Entities = []graph.Entity{
		{ID: "caller", Name: "doFetch", Kind: "SCOPE.Function", Language: "ts", SourceFile: "x.ts"},
	}
	doc.Relationships = []graph.Relationship{
		graph.Relationship{
			ID:     "p1",
			FromID: "caller",
			ToID:   "remote-handler",
			Kind:   "CALLS",
		}.WithProperties(map[string]string{
			"cross_repo":  "true",
			"target_repo": "a",
			"link_method": "http",
			"via":         "phantom_edge_pass_#769 group=g",
		},
		),
	}
	cfg := DefaultProcessFlowConfig()
	cfg.MinSteps = 3
	RunProcessFlow(doc, cfg)

	var found bool
	for _, e := range doc.Entities {
		if e.Kind == EntityKindProcess {
			found = true
			if e.PropGet("cross_stack") != "true" {
				t.Errorf("cross_stack = %q, want true", e.PropGet("cross_stack"))
			}
		}
	}
	if !found {
		t.Errorf("expected a Process entity for 2-step phantom chain, got none")
	}
}

// findProcess returns the first Process entity in doc, or nil.
func findProcess(doc *graph.Document) *graph.Entity {
	for i := range doc.Entities {
		if doc.Entities[i].Kind == EntityKindProcess {
			return &doc.Entities[i]
		}
	}
	return nil
}

// TestProcessFlow_HTTPContinuesIntoBackendHandler is the core #1639 fix:
// an HTTP call that resolves to a SAME-repo backend definition must continue
// the flow INTO the backend handler instead of dead-ending at the
// http_endpoint_definition node. The chain should read:
//
//	caller → http_endpoint_call → http_endpoint_definition → backend handler → repo work
//
// and the flow must NOT be tagged cross-repo (resolution stayed in-repo).
func TestProcessFlow_HTTPContinuesIntoBackendHandler(t *testing.T) {
	doc := &graph.Document{Repo: "r"}
	doc.Entities = []graph.Entity{
		{ID: "caller", Name: "submitOrder", Kind: "SCOPE.Function", Language: "go", SourceFile: "client.go"},
		graph.Entity{ID: "call", Name: "http:POST:/api/orders", Kind: "http_endpoint_call", Language: "go", SourceFile: "client.go"}.WithProperties(map[string]string{"pattern_type": "http_endpoint_client_synthesis"}),
		graph.Entity{ID: "def", Name: "http:POST:/api/orders", Kind: "http_endpoint_definition", Language: "go", SourceFile: "handler.go"}.WithProperties(map[string]string{"pattern_type": "http_endpoint_synthesis"}),
		{ID: "handler", Name: "CreateOrder", Kind: "SCOPE.Function", Language: "go", SourceFile: "handler.go"},
		{ID: "repo", Name: "saveOrder", Kind: "SCOPE.Function", Language: "go", SourceFile: "repo.go"},
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "caller", ToID: "call", Kind: "FETCHES"},
		{ID: "2", FromID: "call", ToID: "def", Kind: "FETCHES"},
		// handler IMPLEMENTS the backend definition (producer side).
		{ID: "3", FromID: "handler", ToID: "def", Kind: "IMPLEMENTS"},
		{ID: "4", FromID: "handler", ToID: "repo", Kind: "CALLS"},
	}
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	p := findProcess(doc)
	if p == nil {
		t.Fatal("expected a Process entity, got none")
	}
	chain := p.PropGet("chain")
	// The chain must reach the backend handler and onward into repo work.
	for _, want := range []string{"call", "def", "handler", "repo"} {
		if !strings.Contains(chain, want) {
			t.Errorf("chain %q missing step %q (did the flow continue into the backend handler?)", chain, want)
		}
	}
	if cs := p.PropGet("cross_stack"); cs != "false" {
		t.Errorf("same-repo HTTP resolution: cross_stack = %q, want false", cs)
	}
}

// TestProcessFlow_CrossRepoOnlyWhenPhantom verifies a chain that crosses a
// genuine repo boundary (phantom edge) IS tagged cross_stack, while a chain
// whose HTTP call resolves into a same-repo handler is NOT.
func TestProcessFlow_CrossRepoOnlyWhenPhantom(t *testing.T) {
	doc := &graph.Document{Repo: "frontend"}
	doc.Entities = []graph.Entity{
		{ID: "entry", Name: "loadDashboard", Kind: "SCOPE.Function", Language: "ts", SourceFile: "app.ts"},
		{ID: "svc", Name: "fetchUsers", Kind: "SCOPE.Function", Language: "ts", SourceFile: "app.ts"},
		graph.Entity{ID: "call", Name: "http:GET:/api/users", Kind: "http_endpoint_call", Language: "ts", SourceFile: "app.ts"}.WithProperties(map[string]string{"pattern_type": "http_endpoint_client_synthesis"}),
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "entry", ToID: "svc", Kind: "CALLS"},
		{ID: "2", FromID: "svc", ToID: "call", Kind: "FETCHES"},
		// Phantom cross-repo edge: the backend lives in another repo.
		graph.Relationship{ID: "3", FromID: "call", ToID: "backend::handler", Kind: "CALLS"}.WithProperties(map[string]string{"cross_repo": "true", "target_repo": "backend"}),
	}
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	p := findProcess(doc)
	if p == nil {
		t.Fatal("expected a Process entity, got none")
	}
	if cs := p.PropGet("cross_stack"); cs != "true" {
		t.Errorf("phantom cross-repo chain: cross_stack = %q, want true", cs)
	}
}
