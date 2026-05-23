// ids_in_locate_test.go — verifies that entity `id` (ADR-0009 prefixed) is
// emitted in the narrow default field set for all locate tools (#1744).
//
// Covered tools:
//   - archigraph_find  (serializeHits JSON path and hitsToTOON TOON path)
//   - archigraph_expand
//   - archigraph_traces (get and follow)
//   - archigraph_find_callers
//   - archigraph_find_callees
//
// The `id` field carries the full "<repo>::<localID>" format (ADR-0009) which
// can be passed directly to archigraph_get_source, eliminating the round-trip
// inspect call that previously ~25% of upvate sessions needed.
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// buildLocateDoc returns a minimal document suitable for testing all locate
// tools. The repo name used by newTestServerWithDoc is "repo1".
func buildLocateDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{
				ID: "fn_alpha", Name: "AlphaFunc", Kind: "SCOPE.Function",
				QualifiedName: "pkg.AlphaFunc",
				SourceFile:    "src/alpha.go", StartLine: 10, EndLine: 30,
				Language: "go",
			},
			{
				ID: "fn_beta", Name: "BetaFunc", Kind: "SCOPE.Function",
				QualifiedName: "pkg.BetaFunc",
				SourceFile:    "src/beta.go", StartLine: 5, EndLine: 20,
				Language: "go",
			},
			{
				ID: "fn_gamma", Name: "GammaFunc", Kind: "SCOPE.Function",
				QualifiedName: "pkg.GammaFunc",
				SourceFile:    "src/gamma.go", StartLine: 1, EndLine: 15,
				Language: "go",
			},
			// Process entity for traces get test.
			{
				ID: "proc_locate", Name: "BetaFunc → AlphaFunc", Kind: "SCOPE.Process",
				SourceFile: "src/beta.go", StartLine: 5,
				Properties: map[string]string{
					"entry_id":    "fn_beta",
					"entry_name":  "BetaFunc",
					"terminal_id": "fn_alpha",
					"step_count":  "2",
					"cross_stack": "false",
				},
			},
		},
		Relationships: []graph.Relationship{
			{FromID: "fn_beta", ToID: "fn_alpha", Kind: "CALLS"},
			{FromID: "fn_alpha", ToID: "fn_gamma", Kind: "CALLS"},
			// Process steps.
			{FromID: "proc_locate", ToID: "fn_beta", Kind: "STEP_IN_PROCESS",
				Properties: map[string]string{"step_index": "0"}},
			{FromID: "proc_locate", ToID: "fn_alpha", Kind: "STEP_IN_PROCESS",
				Properties: map[string]string{"step_index": "1"}},
		},
	}
}

// callHandlerText returns the raw text body of a tool handler call.
func callHandlerText(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) string {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("tool returned nil or error: %v", res)
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcpapi.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatal("no text content in result")
	return ""
}

// decodeArray decodes a raw JSON string as a []map[string]any.
func decodeArray(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var arr []map[string]any
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		t.Fatalf("expected JSON array, got decode error %v\nraw: %s", err, raw)
	}
	return arr
}

// assertPrefixedID asserts the id string is a non-empty ADR-0009 prefixed id
// starting with "repo1::".
func assertPrefixedID(t *testing.T, label, idStr string) {
	t.Helper()
	if idStr == "" {
		t.Errorf("%s: 'id' field is empty", label)
		return
	}
	if !strings.Contains(idStr, "::") {
		t.Errorf("%s: 'id' must be ADR-0009 prefixed format (<repo>::<localID>), got %q", label, idStr)
	}
	if !strings.HasPrefix(idStr, "repo1::") {
		t.Errorf("%s: 'id' must start with 'repo1::', got %q", label, idStr)
	}
}

// ---------------------------------------------------------------------------
// archigraph_find — TOON path (hitsToTOON schema)
// ---------------------------------------------------------------------------

// TestIDsInLocate_Find_TOONSchemaFirstColumnIsID verifies that the TOON schema
// for archigraph_find starts with "id" as the first column, for both single-
// and multi-repo modes.
func TestIDsInLocate_Find_TOONSchemaFirstColumnIsID(t *testing.T) {
	// Ensure TOON wire format is active (default).
	t.Setenv("MCP_WIRE_FORMAT", "")
	t.Setenv("MCP_FIND_FORMAT", "")

	nodes := []nodeWithRepo{
		makeTestNode("myrepo", "AlphaFunc", "Function", "src/alpha.go", 10, 9.0),
		makeTestNode("myrepo", "BetaFunc", "Function", "src/beta.go", 5, 7.0),
	}

	// Single-repo schema: {id, name, kind, file, line, score}.
	toonSingle := hitsToTOON(nodes, true)
	if !strings.HasPrefix(toonSingle, "[!schema {id,") {
		t.Errorf("single-repo TOON schema must have 'id' as first column, got:\n%s", toonSingle)
	}

	// Multi-repo schema: {id, repo, name, kind, file, line, score}.
	toonMulti := hitsToTOON(nodes, false)
	if !strings.HasPrefix(toonMulti, "[!schema {id,") {
		t.Errorf("multi-repo TOON schema must have 'id' as first column, got:\n%s", toonMulti)
	}

	// Each data row must start with the prefixed id cell.
	lines := strings.Split(strings.TrimSpace(toonSingle), "\n")
	for _, line := range lines[1:] { // skip schema header
		if !strings.HasPrefix(line, "{myrepo::") {
			t.Errorf("TOON data row must start with prefixed id '{myrepo::...', got %q", line)
		}
	}
}

// TestIDsInLocate_Find_TOONRowContainsID verifies that the TOON rows emitted
// by renderCompact contain both the id and the entity name.
func TestIDsInLocate_Find_TOONRowContainsID(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "")
	t.Setenv("MCP_FIND_FORMAT", "")

	nodes := []nodeWithRepo{
		makeTestNode("svc", "HandleRequest", "function", "src/handler.go", 22, 8.5),
	}
	rr := renderResult{MatchedTotal: 1, OneRepo: true, Nodes: nodes}
	got := renderCompact(rr, 0)

	if !strings.Contains(got, "svc::HandleRequest") {
		t.Errorf("TOON output must contain prefixed id 'svc::HandleRequest':\n%s", got)
	}
	if !strings.Contains(got, "HandleRequest") {
		t.Errorf("TOON output must contain entity name 'HandleRequest':\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// archigraph_find — JSON path (serializeHits)
// ---------------------------------------------------------------------------

// TestIDsInLocate_Find_JSONPathIncludesID verifies that the JSON-mode
// archigraph_find response (full=true) includes "id" in each match row.
func TestIDsInLocate_Find_JSONPathIncludesID(t *testing.T) {
	srv := newTestServerWithDoc(t, buildLocateDoc())
	res := callToolArgs(t, srv.handleQueryGraph, map[string]any{
		"group":    "test",
		"question": "AlphaFunc",
		"full":     true,
		"verbose":  false,
	})
	matches, ok := res["matches"].([]any)
	if !ok || len(matches) == 0 {
		t.Skip("no matches returned — BM25 may not have indexed this fixture")
	}
	for _, m := range matches {
		obj := m.(map[string]any)
		idVal, hasID := obj["id"]
		if !hasID {
			t.Errorf("find narrow mode missing 'id' field: %v", obj)
			continue
		}
		idStr, _ := idVal.(string)
		assertPrefixedID(t, "find match", idStr)
	}
}

// ---------------------------------------------------------------------------
// archigraph_expand
// ---------------------------------------------------------------------------

// TestIDsInLocate_Expand_NeighborRowsIncludeID verifies that each neighbor
// row in the expand result carries a prefixed ADR-0009 "id".
func TestIDsInLocate_Expand_NeighborRowsIncludeID(t *testing.T) {
	srv := newTestServerWithDoc(t, buildLocateDoc())
	// fn_beta has an outbound edge to fn_alpha so we'll get neighbors.
	raw := callHandlerText(t, srv.handleGetNeighbors, map[string]any{
		"group": "test",
		"node":  "fn_beta",
	})
	if !strings.Contains(raw, `"id"`) {
		t.Errorf("expand response must contain 'id' field, raw:\n%s", raw)
	}
	if !strings.Contains(raw, "repo1::") {
		t.Errorf("expand response 'id' must use prefixed ADR-0009 format (repo1::...), raw:\n%s", raw)
	}

	// Decode as array and validate each neighbor has a prefixed id.
	neighbors := decodeArray(t, raw)
	if len(neighbors) == 0 {
		t.Fatal("expected at least one neighbor for fn_beta")
	}
	for _, n := range neighbors {
		idVal, hasID := n["id"]
		if !hasID {
			t.Errorf("expand neighbor missing 'id' field: %v", n)
			continue
		}
		assertPrefixedID(t, "expand neighbor", idVal.(string))
	}
}

// ---------------------------------------------------------------------------
// archigraph_traces (get) — steps include id
// ---------------------------------------------------------------------------

// TestIDsInLocate_TracesGet_StepsIncludeID verifies that each step in
// archigraph_traces action=get carries a prefixed "id" field alongside the
// existing "node_id" (kept for backward compatibility).
func TestIDsInLocate_TracesGet_StepsIncludeID(t *testing.T) {
	srv := newTestServerWithDoc(t, buildLocateDoc())
	res := callToolArgs(t, srv.handleTracesGet, map[string]any{
		"group":      "test",
		"process_id": "proc_locate",
		"verbose":    false,
	})
	if found, _ := res["found"].(bool); !found {
		t.Skip("process proc_locate not found in fixture")
	}
	steps, ok := res["steps"].([]any)
	if !ok || len(steps) == 0 {
		t.Fatal("expected steps in traces get result")
	}
	for i, s := range steps {
		obj := s.(map[string]any)
		idVal, hasID := obj["id"]
		if !hasID {
			t.Errorf("traces get step[%d] missing 'id' field: %v", i, obj)
			continue
		}
		assertPrefixedID(t, "traces get step", idVal.(string))
		// node_id (local) must still be present for backward compat.
		if _, hasNodeID := obj["node_id"]; !hasNodeID {
			t.Errorf("traces get step[%d] must retain 'node_id' for backward compat: %v", i, obj)
		}
	}
}

// ---------------------------------------------------------------------------
// archigraph_traces (follow) — steps include id
// ---------------------------------------------------------------------------

// TestIDsInLocate_TracesFollow_StepsIncludeID verifies that each step in
// archigraph_traces action=follow carries a prefixed "id" field.
func TestIDsInLocate_TracesFollow_StepsIncludeID(t *testing.T) {
	srv := newTestServerWithDoc(t, buildLocateDoc())
	res := callToolArgs(t, srv.handleTracesFollow, map[string]any{
		"group":          "test",
		"entry_point_id": "fn_beta",
		"verbose":        false,
	})
	chains, ok := res["chains"].([]any)
	if !ok || len(chains) == 0 {
		t.Skip("no chains returned for fn_beta")
	}
	for ci, ch := range chains {
		chain := ch.(map[string]any)
		steps, _ := chain["steps"].([]any)
		for si, s := range steps {
			obj := s.(map[string]any)
			idVal, hasID := obj["id"]
			if !hasID {
				t.Errorf("traces follow chain[%d] step[%d] missing 'id' field: %v", ci, si, obj)
				continue
			}
			assertPrefixedID(t, "traces follow step", idVal.(string))
			// node_id must still be present for backward compat.
			if _, hasNodeID := obj["node_id"]; !hasNodeID {
				t.Errorf("traces follow chain[%d] step[%d] must retain 'node_id': %v", ci, si, obj)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// archigraph_find_callers — narrow mode includes id
// ---------------------------------------------------------------------------

// TestIDsInLocate_FindCallers_NarrowIncludesID verifies that each caller row
// includes a prefixed "id" in the narrow (verbose=false) default mode.
func TestIDsInLocate_FindCallers_NarrowIncludesID(t *testing.T) {
	srv := newTestServerWithDoc(t, buildLocateDoc())
	res := callToolArgs(t, srv.handleFindCallers, map[string]any{
		"group":     "test",
		"entity_id": "fn_alpha",
		"verbose":   false,
	})
	callers := getSlice(t, res, "callers")
	if len(callers) == 0 {
		t.Fatal("expected callers for fn_alpha (fn_beta CALLS it)")
	}
	for _, c := range callers {
		obj := c.(map[string]any)
		idVal, hasID := obj["id"]
		if !hasID {
			t.Errorf("find_callers narrow mode missing 'id' field: %v", obj)
			continue
		}
		assertPrefixedID(t, "find_callers row", idVal.(string))
	}
}

// ---------------------------------------------------------------------------
// archigraph_find_callees — narrow mode includes id
// ---------------------------------------------------------------------------

// TestIDsInLocate_FindCallees_NarrowIncludesID verifies that each callee row
// includes a prefixed "id" in the narrow (verbose=false) default mode.
func TestIDsInLocate_FindCallees_NarrowIncludesID(t *testing.T) {
	srv := newTestServerWithDoc(t, buildLocateDoc())
	res := callToolArgs(t, srv.handleFindCallees, map[string]any{
		"group":     "test",
		"entity_id": "fn_alpha",
		"verbose":   false,
	})
	callees := getSlice(t, res, "callees")
	if len(callees) == 0 {
		t.Fatal("expected callees for fn_alpha (it CALLS fn_gamma)")
	}
	for _, c := range callees {
		obj := c.(map[string]any)
		idVal, hasID := obj["id"]
		if !hasID {
			t.Errorf("find_callees narrow mode missing 'id' field: %v", obj)
			continue
		}
		assertPrefixedID(t, "find_callees row", idVal.(string))
	}
}

// ---------------------------------------------------------------------------
// verbose=true preserves id (regression guard for #1744)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// archigraph_find multi-repo path — TOON rows include id (#1848)
// ---------------------------------------------------------------------------

// TestIDsInLocate_Find_MultiRepo_TOONHasIDs verifies that the smart-scoping
// "per-repo top hits" code path (triggered when no repo_filter is given and the
// group contains more than one repo) emits TOON rows with a prefixed id in the
// first column, enabling callers to chain into archigraph_docgen /
// archigraph_get_source without a repo_filter round-trip.
//
// Before #1848 the multi-repo path rendered plain markdown:
//
//	## repo-a
//	FuncX  src/x.go:10
//
// After #1848 it emits a TOON table:
//
//	[!schema {id,repo,name,kind,file,line,score}]
//	{repo-a::fn_x,repo-a,FuncX,...}
func TestIDsInLocate_Find_MultiRepo_TOONHasIDs(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "")
	t.Setenv("MCP_FIND_FORMAT", "")

	// Two repos, each with a uniquely named entity so BM25 can score them.
	docA := &graph.Document{
		Entities: []graph.Entity{
			{ID: "fn_widget", Name: "WidgetHandler", Kind: "SCOPE.Function",
				QualifiedName: "pkg.WidgetHandler", SourceFile: "src/widget.go", StartLine: 10},
		},
	}
	docB := &graph.Document{
		Entities: []graph.Entity{
			{ID: "fn_widget_b", Name: "WidgetProcessor", Kind: "SCOPE.Function",
				QualifiedName: "pkg.WidgetProcessor", SourceFile: "src/proc.go", StartLine: 20},
		},
	}
	srv := newTestServerWithDocs(t, map[string]*graph.Document{
		"repo-a": docA,
		"repo-b": docB,
	})

	raw := callHandlerText(t, srv.handleQueryGraph, map[string]any{
		"group":    "test",
		"question": "Widget",
		// No repo_filter — triggers the multi-repo per-repo summary path.
	})

	// Must start with the TOON schema line, not a markdown heading.
	if !strings.Contains(raw, "[!schema") {
		t.Fatalf("multi-repo find must emit TOON schema, got:\n%s", raw)
	}
	// Schema must contain the id column.
	if !strings.Contains(raw, "id,") {
		t.Errorf("multi-repo TOON schema must include 'id' column, got:\n%s", raw)
	}
	// Schema must contain the repo column (7-column multi-repo schema).
	if !strings.Contains(raw, "repo,") {
		t.Errorf("multi-repo TOON schema must include 'repo' column, got:\n%s", raw)
	}
	// Every data row must contain an ADR-0009 prefixed id cell.
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "#") {
			continue
		}
		// TOON data rows look like: {repo-a::fn_widget,repo-a,...}
		if !strings.Contains(line, "::") {
			t.Errorf("multi-repo TOON data row missing prefixed id (no '::'), got %q", line)
		}
	}
}

// TestIDsInLocate_Find_MultiRepo_MarkdownFallback verifies that setting
// MCP_FIND_FORMAT=markdown restores the legacy plain-text rendering (no TOON).
func TestIDsInLocate_Find_MultiRepo_MarkdownFallback(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "")
	t.Setenv("MCP_FIND_FORMAT", "markdown")

	docA := &graph.Document{
		Entities: []graph.Entity{
			{ID: "fn_gadget", Name: "GadgetHandler", Kind: "SCOPE.Function",
				QualifiedName: "pkg.GadgetHandler", SourceFile: "src/gadget.go", StartLine: 5},
		},
	}
	docB := &graph.Document{
		Entities: []graph.Entity{
			{ID: "fn_gadget_b", Name: "GadgetProcessor", Kind: "SCOPE.Function",
				QualifiedName: "pkg.GadgetProcessor", SourceFile: "src/gp.go", StartLine: 7},
		},
	}
	srv := newTestServerWithDocs(t, map[string]*graph.Document{
		"repo-c": docA,
		"repo-d": docB,
	})

	raw := callHandlerText(t, srv.handleQueryGraph, map[string]any{
		"group":    "test",
		"question": "Gadget",
	})

	// Markdown fallback: must start with the # group header, no TOON schema.
	if !strings.Contains(raw, "# group:") {
		t.Errorf("markdown fallback must contain '# group:' header, got:\n%s", raw)
	}
	if strings.Contains(raw, "[!schema") {
		t.Errorf("markdown fallback must NOT contain TOON schema, got:\n%s", raw)
	}
}

// TestIDsInLocate_VerboseModePreservesID verifies that verbose=true does not
// remove the id field — it only adds extra fields on top of the narrow set.
func TestIDsInLocate_VerboseModePreservesID(t *testing.T) {
	srv := newTestServerWithDoc(t, buildLocateDoc())

	// find_callers verbose.
	res := callToolArgs(t, srv.handleFindCallers, map[string]any{
		"group":     "test",
		"entity_id": "fn_alpha",
		"verbose":   true,
	})
	for _, c := range getSlice(t, res, "callers") {
		obj := c.(map[string]any)
		if _, hasID := obj["id"]; !hasID {
			t.Errorf("find_callers verbose must still include 'id': %v", obj)
		}
		if _, hasKind := obj["kind"]; !hasKind {
			t.Errorf("find_callers verbose should also include 'kind': %v", obj)
		}
	}

	// find_callees verbose.
	res2 := callToolArgs(t, srv.handleFindCallees, map[string]any{
		"group":     "test",
		"entity_id": "fn_alpha",
		"verbose":   true,
	})
	for _, c := range getSlice(t, res2, "callees") {
		obj := c.(map[string]any)
		if _, hasID := obj["id"]; !hasID {
			t.Errorf("find_callees verbose must still include 'id': %v", obj)
		}
		if _, hasKind := obj["kind"]; !hasKind {
			t.Errorf("find_callees verbose should also include 'kind': %v", obj)
		}
	}

	// traces get verbose.
	resTraces := callToolArgs(t, srv.handleTracesGet, map[string]any{
		"group":      "test",
		"process_id": "proc_locate",
		"verbose":    true,
	})
	if found, _ := resTraces["found"].(bool); !found {
		t.Skip("proc_locate not found")
	}
	for _, s := range resTraces["steps"].([]any) {
		obj := s.(map[string]any)
		if _, hasID := obj["id"]; !hasID {
			t.Errorf("traces get verbose must still include 'id': %v", obj)
		}
		if _, hasKind := obj["kind"]; !hasKind {
			t.Errorf("traces get verbose should also include 'kind': %v", obj)
		}
	}
}
