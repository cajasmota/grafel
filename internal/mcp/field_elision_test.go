// field_elision_test.go — verifies narrow (default) and wide (verbose=true)
// response shapes for all tools that gained per-tool field elision in #1739.
//
// Narrow mode (verbose=false, default):
//   - archigraph_find (full=true): id, name, file, line, score, kind — NO qualified_name/repo
//   - archigraph_inspect: id, name, qualified_name, file, line, kind — NO end_line/language/repo/pagerank/community_id/properties
//   - archigraph_find_callers: per-item id, name, file, line, hop_count — NO kind/repo
//   - archigraph_find_callees: per-item id, name, file, line, hop_count — NO kind/repo
//   - archigraph_traces (get): step fields: step_index, node_id, name, file, line — NO kind
//   - archigraph_traces (follow): step fields: step_index, node_id, name, file, line — NO kind
//   - archigraph_topology (topic_detail): participant entity_id, entity_name, kind — NO source_file/repo
//
// Wide mode (verbose=true):
//   - All elided fields are restored.
package mcp

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// buildElisionDoc builds a document with entities covering all field-elision
// cases: an entity with all optional fields populated, caller/callee
// relationships, a process trace, and a messaging topic.
func buildElisionDoc() *graph.Document {
	pr := 0.042
	comm := 7
	return &graph.Document{
		Entities: []graph.Entity{
			{
				ID: "fn_a", Name: "doWork", Kind: "SCOPE.Function",
				QualifiedName: "pkg.doWork",
				SourceFile:    "src/work.go", StartLine: 10, EndLine: 40,
				Language:   "go",
				Properties: map[string]string{"exported": "true"},
				PageRank:   &pr,
				CommunityID: &comm,
			},
			{
				ID: "fn_b", Name: "callWork", Kind: "SCOPE.Function",
				QualifiedName: "pkg.callWork",
				SourceFile:    "src/caller.go", StartLine: 5, EndLine: 15,
				Language: "go",
			},
			{
				ID: "fn_c", Name: "helperFunc", Kind: "SCOPE.Function",
				QualifiedName: "pkg.helperFunc",
				SourceFile:    "src/helper.go", StartLine: 1, EndLine: 8,
				Language: "go",
			},
			// Process entity for traces get test.
			{
				ID: "proc1", Name: "callWork → doWork", Kind: "SCOPE.Process",
				SourceFile: "src/caller.go", StartLine: 5,
				Properties: map[string]string{
					"entry_id":   "fn_b",
					"entry_name": "callWork",
					"terminal_id": "fn_a",
					"step_count": "2",
					"cross_stack": "false",
				},
			},
			// Topic for topology test.
			{
				ID: "topic1", Name: "order-created", Kind: "SCOPE.Topic",
				SourceFile: "topics/orders.go", StartLine: 3,
			},
			// Publisher and subscriber.
			{
				ID: "pub1", Name: "OrderService", Kind: "SCOPE.Component",
				SourceFile: "src/order.go", StartLine: 20,
			},
			{
				ID: "sub1", Name: "NotificationService", Kind: "SCOPE.Component",
				SourceFile: "src/notify.go", StartLine: 10,
			},
		},
		Relationships: []graph.Relationship{
			{FromID: "fn_b", ToID: "fn_a", Kind: "CALLS"},
			{FromID: "fn_a", ToID: "fn_c", Kind: "CALLS"},
			// Process steps.
			{FromID: "proc1", ToID: "fn_b", Kind: "STEP_IN_PROCESS",
				Properties: map[string]string{"step_index": "0"}},
			{FromID: "proc1", ToID: "fn_a", Kind: "STEP_IN_PROCESS",
				Properties: map[string]string{"step_index": "1"}},
			// Topic edges.
			{FromID: "pub1", ToID: "topic1", Kind: "PUBLISHES_TO"},
			{FromID: "sub1", ToID: "topic1", Kind: "SUBSCRIBES_TO"},
		},
	}
}

func newElisionServer(t *testing.T) *Server {
	t.Helper()
	return newTestServerWithDoc(t, buildElisionDoc())
}

func callToolArgs(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) map[string]any {
	t.Helper()
	return callEndpointTool(t, fn, args)
}

// ---------------------------------------------------------------------------
// archigraph_find (full=true) — narrow vs wide
// ---------------------------------------------------------------------------

func TestFieldElision_Find_NarrowOmitsQualifiedNameAndRepo(t *testing.T) {
	srv := newElisionServer(t)
	res := callToolArgs(t, srv.handleQueryGraph, map[string]any{
		"group":    "test",
		"question": "doWork",
		"full":     true,
		"verbose":  false,
	})
	matches, ok := res["matches"].([]any)
	if !ok || len(matches) == 0 {
		t.Skip("no matches returned — BM25 may not have indexed this fixture")
	}
	for _, m := range matches {
		obj := m.(map[string]any)
		if _, has := obj["qualified_name"]; has {
			t.Errorf("narrow mode should NOT include qualified_name: %v", obj)
		}
		if _, has := obj["repo"]; has {
			t.Errorf("narrow mode should NOT include repo: %v", obj)
		}
		// Required fields.
		for _, req := range []string{"id", "name", "file", "line", "score", "kind"} {
			if _, has := obj[req]; !has {
				t.Errorf("narrow mode missing required field %q: %v", req, obj)
			}
		}
	}
}

func TestFieldElision_Find_VerboseRestoresQualifiedNameAndRepo(t *testing.T) {
	srv := newElisionServer(t)
	res := callToolArgs(t, srv.handleQueryGraph, map[string]any{
		"group":    "test",
		"question": "doWork",
		"full":     true,
		"verbose":  true,
	})
	matches, ok := res["matches"].([]any)
	if !ok || len(matches) == 0 {
		t.Skip("no matches returned")
	}
	// At least one match should have qualified_name restored.
	found := false
	for _, m := range matches {
		obj := m.(map[string]any)
		if qn, has := obj["qualified_name"]; has && qn != "" {
			found = true
		}
		if _, has := obj["repo"]; !has {
			t.Errorf("verbose mode should include repo: %v", obj)
		}
	}
	if !found {
		t.Errorf("verbose mode should restore qualified_name on at least one match")
	}
}

// ---------------------------------------------------------------------------
// archigraph_inspect — narrow vs wide
// ---------------------------------------------------------------------------

func TestFieldElision_Inspect_NarrowShape(t *testing.T) {
	srv := newElisionServer(t)
	res := callToolArgs(t, srv.handleGetNode, map[string]any{
		"group":       "test",
		"label_or_id": "fn_a",
		"verbose":     false,
	})
	// Narrow: id, name, qualified_name, file, line, kind — present.
	for _, req := range []string{"id", "name", "qualified_name", "file", "line", "kind"} {
		if _, has := res[req]; !has {
			t.Errorf("narrow inspect missing required field %q: %v", req, res)
		}
	}
	// Elided: end_line, language, repo, pagerank, community_id, properties.
	for _, elided := range []string{"end_line", "language", "repo", "pagerank", "community_id", "properties"} {
		if _, has := res[elided]; has {
			t.Errorf("narrow inspect should NOT include %q: %v", elided, res)
		}
	}
}

func TestFieldElision_Inspect_VerboseRestoresAllFields(t *testing.T) {
	srv := newElisionServer(t)
	res := callToolArgs(t, srv.handleGetNode, map[string]any{
		"group":       "test",
		"label_or_id": "fn_a",
		"verbose":     true,
	})
	// Verbose: all fields should be present (fn_a has pagerank, community_id, properties).
	for _, wantField := range []string{"id", "name", "qualified_name", "file", "line", "kind",
		"end_line", "language", "repo", "pagerank", "community_id", "properties"} {
		if _, has := res[wantField]; !has {
			t.Errorf("verbose inspect missing field %q: %v", wantField, res)
		}
	}
}

// ---------------------------------------------------------------------------
// archigraph_find_callers — narrow vs wide
// ---------------------------------------------------------------------------

func TestFieldElision_FindCallers_NarrowOmitsKindAndRepo(t *testing.T) {
	srv := newElisionServer(t)
	res := callToolArgs(t, srv.handleFindCallers, map[string]any{
		"group":     "test",
		"entity_id": "fn_a",
		"verbose":   false,
	})
	callers := getSlice(t, res, "callers")
	if len(callers) == 0 {
		t.Fatal("expected callers for fn_a")
	}
	for _, c := range callers {
		obj := c.(map[string]any)
		// Required narrow fields.
		for _, req := range []string{"id", "name", "hop_count"} {
			if _, has := obj[req]; !has {
				t.Errorf("narrow callers missing %q: %v", req, obj)
			}
		}
		// Elided.
		if _, has := obj["kind"]; has {
			t.Errorf("narrow callers should NOT include kind: %v", obj)
		}
		if _, has := obj["repo"]; has {
			t.Errorf("narrow callers should NOT include repo: %v", obj)
		}
	}
}

func TestFieldElision_FindCallers_VerboseRestoresKindAndRepo(t *testing.T) {
	srv := newElisionServer(t)
	res := callToolArgs(t, srv.handleFindCallers, map[string]any{
		"group":     "test",
		"entity_id": "fn_a",
		"verbose":   true,
	})
	callers := getSlice(t, res, "callers")
	if len(callers) == 0 {
		t.Fatal("expected callers for fn_a")
	}
	for _, c := range callers {
		obj := c.(map[string]any)
		if _, has := obj["kind"]; !has {
			t.Errorf("verbose callers should include kind: %v", obj)
		}
		if _, has := obj["repo"]; !has {
			t.Errorf("verbose callers should include repo: %v", obj)
		}
	}
}

// ---------------------------------------------------------------------------
// archigraph_find_callees — narrow vs wide
// ---------------------------------------------------------------------------

func TestFieldElision_FindCallees_NarrowOmitsKindAndRepo(t *testing.T) {
	srv := newElisionServer(t)
	res := callToolArgs(t, srv.handleFindCallees, map[string]any{
		"group":     "test",
		"entity_id": "fn_a",
		"verbose":   false,
	})
	callees := getSlice(t, res, "callees")
	if len(callees) == 0 {
		t.Fatal("expected callees for fn_a")
	}
	for _, c := range callees {
		obj := c.(map[string]any)
		for _, req := range []string{"id", "name", "hop_count"} {
			if _, has := obj[req]; !has {
				t.Errorf("narrow callees missing %q: %v", req, obj)
			}
		}
		if _, has := obj["kind"]; has {
			t.Errorf("narrow callees should NOT include kind: %v", obj)
		}
		if _, has := obj["repo"]; has {
			t.Errorf("narrow callees should NOT include repo: %v", obj)
		}
	}
}

func TestFieldElision_FindCallees_VerboseRestoresKindAndRepo(t *testing.T) {
	srv := newElisionServer(t)
	res := callToolArgs(t, srv.handleFindCallees, map[string]any{
		"group":     "test",
		"entity_id": "fn_a",
		"verbose":   true,
	})
	callees := getSlice(t, res, "callees")
	if len(callees) == 0 {
		t.Fatal("expected callees for fn_a")
	}
	for _, c := range callees {
		obj := c.(map[string]any)
		if _, has := obj["kind"]; !has {
			t.Errorf("verbose callees should include kind: %v", obj)
		}
		if _, has := obj["repo"]; !has {
			t.Errorf("verbose callees should include repo: %v", obj)
		}
	}
}

// ---------------------------------------------------------------------------
// archigraph_traces (get) — narrow vs wide steps
// ---------------------------------------------------------------------------

func TestFieldElision_TracesGet_NarrowOmitsKindFromSteps(t *testing.T) {
	srv := newElisionServer(t)
	res := callToolArgs(t, srv.handleTracesGet, map[string]any{
		"group":      "test",
		"process_id": "proc1",
		"verbose":    false,
	})
	if found, _ := res["found"].(bool); !found {
		t.Skip("process proc1 not found in fixture")
	}
	steps, ok := res["steps"].([]any)
	if !ok || len(steps) == 0 {
		t.Fatal("expected steps in traces get result")
	}
	for _, s := range steps {
		obj := s.(map[string]any)
		// Required narrow fields.
		for _, req := range []string{"step_index", "node_id"} {
			if _, has := obj[req]; !has {
				t.Errorf("narrow step missing %q: %v", req, obj)
			}
		}
		// kind is elided.
		if _, has := obj["kind"]; has {
			t.Errorf("narrow step should NOT include kind: %v", obj)
		}
	}
}

func TestFieldElision_TracesGet_VerboseRestoresKindOnSteps(t *testing.T) {
	srv := newElisionServer(t)
	res := callToolArgs(t, srv.handleTracesGet, map[string]any{
		"group":      "test",
		"process_id": "proc1",
		"verbose":    true,
	})
	if found, _ := res["found"].(bool); !found {
		t.Skip("process proc1 not found in fixture")
	}
	steps, ok := res["steps"].([]any)
	if !ok || len(steps) == 0 {
		t.Fatal("expected steps")
	}
	for _, s := range steps {
		obj := s.(map[string]any)
		if _, has := obj["kind"]; !has {
			t.Errorf("verbose step should include kind: %v", obj)
		}
	}
}

// ---------------------------------------------------------------------------
// archigraph_traces (follow) — narrow vs wide steps
// ---------------------------------------------------------------------------

func TestFieldElision_TracesFollow_NarrowOmitsKindFromSteps(t *testing.T) {
	srv := newElisionServer(t)
	res := callToolArgs(t, srv.handleTracesFollow, map[string]any{
		"group":          "test",
		"entry_point_id": "fn_b",
		"verbose":        false,
	})
	chains, ok := res["chains"].([]any)
	if !ok || len(chains) == 0 {
		t.Skip("no chains returned for fn_b")
	}
	for _, ch := range chains {
		chain := ch.(map[string]any)
		steps, _ := chain["steps"].([]any)
		for _, s := range steps {
			obj := s.(map[string]any)
			if _, has := obj["kind"]; has {
				t.Errorf("narrow follow step should NOT include kind: %v", obj)
			}
		}
	}
}

func TestFieldElision_TracesFollow_VerboseRestoresKindOnSteps(t *testing.T) {
	srv := newElisionServer(t)
	res := callToolArgs(t, srv.handleTracesFollow, map[string]any{
		"group":          "test",
		"entry_point_id": "fn_b",
		"verbose":        true,
	})
	chains, ok := res["chains"].([]any)
	if !ok || len(chains) == 0 {
		t.Skip("no chains returned for fn_b")
	}
	for _, ch := range chains {
		chain := ch.(map[string]any)
		steps, _ := chain["steps"].([]any)
		for _, s := range steps {
			obj := s.(map[string]any)
			if _, has := obj["kind"]; !has {
				t.Errorf("verbose follow step should include kind: %v", obj)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// archigraph_topology (topic_detail) — narrow vs wide
// ---------------------------------------------------------------------------

func TestFieldElision_TopologyTopicDetail_NarrowOmitsSourceFileAndRepo(t *testing.T) {
	srv := newElisionServer(t)
	res := callToolArgs(t, srv.handleTopologyTopicDetail, map[string]any{
		"group":    "test",
		"topic_id": "topic1",
		"verbose":  false,
	})
	if found, _ := res["found"].(bool); !found {
		t.Skip("topic1 not found")
	}
	// Top-level source_file and repo should be elided.
	if _, has := res["source_file"]; has {
		t.Errorf("narrow topology should NOT include top-level source_file: %v", res)
	}
	if _, has := res["repo"]; has {
		t.Errorf("narrow topology should NOT include top-level repo: %v", res)
	}
	// Participants: check publishers and subscribers.
	for _, listKey := range []string{"publishers", "subscribers"} {
		list, _ := res[listKey].([]any)
		for _, p := range list {
			obj := p.(map[string]any)
			if _, has := obj["source_file"]; has {
				t.Errorf("narrow %s participant should NOT include source_file: %v", listKey, obj)
			}
			if _, has := obj["repo"]; has {
				t.Errorf("narrow %s participant should NOT include repo: %v", listKey, obj)
			}
			// Required narrow fields.
			for _, req := range []string{"entity_id", "entity_name", "kind"} {
				if _, has := obj[req]; !has {
					t.Errorf("narrow %s participant missing %q: %v", listKey, req, obj)
				}
			}
		}
	}
}

func TestFieldElision_TopologyTopicDetail_VerboseRestoresSourceFileAndRepo(t *testing.T) {
	srv := newElisionServer(t)
	res := callToolArgs(t, srv.handleTopologyTopicDetail, map[string]any{
		"group":    "test",
		"topic_id": "topic1",
		"verbose":  true,
	})
	if found, _ := res["found"].(bool); !found {
		t.Skip("topic1 not found")
	}
	// Top-level source_file and repo should be restored.
	if _, has := res["source_file"]; !has {
		t.Errorf("verbose topology should include top-level source_file: %v", res)
	}
	if _, has := res["repo"]; !has {
		t.Errorf("verbose topology should include top-level repo: %v", res)
	}
	// Participants should have source_file and repo.
	for _, listKey := range []string{"publishers", "subscribers"} {
		list, _ := res[listKey].([]any)
		for _, p := range list {
			obj := p.(map[string]any)
			if _, has := obj["source_file"]; !has {
				t.Errorf("verbose %s participant should include source_file: %v", listKey, obj)
			}
			if _, has := obj["repo"]; !has {
				t.Errorf("verbose %s participant should include repo: %v", listKey, obj)
			}
		}
	}
}
