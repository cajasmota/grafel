package mcp

// get_source_resolution_4272_test.go — regression coverage for #4272.
//
// THE BUG (root cause of the ~8% archigraph_get_source error rate, the busiest
// MCP tool): handleGetNodeSource resolved a "<repo>::<localid>" prefixed
// entity_id ONLY via r.LabelIndex.ByID[localid]. When the prefixed arg pointed
// at an entity that resolves by qualified_name or label (not by its raw id) — or
// when the "<repo>::" prefix was present but the subsequent fallback ran
// LookupAll on the STILL-PREFIXED string — resolution fell through to
// "node not found", even though the entity was in the index all along. This
// mirrors the #4243 effective_contract prefix gap.
//
// These tests assert handleGetNodeSource resolves an entity by:
//
//	(a) bare local id
//	(b) "<repo>::"-prefixed id
//	(c) qualified_name (bare AND "<repo>::"-prefixed — the prefixed-by-qname form
//	    is the case that ERRORED pre-fix)
//	(d) label
//
// and (e) returns a HELPFUL not-found for a genuinely-missing arg: the error
// names the attempted forms and offers a did-you-mean nearest match, so the
// caller can self-correct instead of re-erroring.
//
// Non-vacuous proof: cases (c-prefixed) and (e) FAIL on the pre-fix handler —
// (c-prefixed) returned "node not found", and (e) returned a bare
// "node not found: <arg>" with no attempted-forms / suggestion.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// writeSrcFile writes content to a real file under dir and returns its absolute
// path. get_source reads e.SourceFile directly when it is absolute (the test
// LoadedRepo has no Path), so entities point at these temp files.
func writeSrcFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write src file: %v", err)
	}
	return p
}

// getSourceResolutionDoc builds a single-repo graph whose one real entity has a
// distinct id, qualified_name, and label, each pointing at a real source file so
// the read path succeeds for whichever form resolves.
func getSourceResolutionDoc(t *testing.T) *graph.Document {
	t.Helper()
	dir := t.TempDir()
	src := writeSrcFile(t, dir, "service.py", strings.Join([]string{
		"class OrderService:",
		"    def place_order(self, cart):",
		"        # MARKER_PLACE_ORDER_BODY",
		"        return self.repo.save(cart)",
		"",
	}, "\n"))
	return &graph.Document{
		Repo: "upvate-core",
		Entities: []graph.Entity{
			{
				ID:            "ent_place_order_42",
				Name:          "place_order",
				QualifiedName: "core.services.order_service.OrderService.place_order",
				Kind:          "SCOPE.Operation", Subtype: "method",
				SourceFile: src, StartLine: 2, EndLine: 4, Language: "python",
			},
			// A second, similarly-named entity so the did-you-mean suggestion has a
			// near neighbour to surface for a typo'd arg.
			{
				ID:            "ent_place_order_sync_9",
				Name:          "place_order_sync",
				QualifiedName: "core.services.order_service.OrderService.place_order_sync",
				Kind:          "SCOPE.Operation", Subtype: "method",
				SourceFile: src, StartLine: 2, EndLine: 4, Language: "python",
			},
		},
	}
}

func getSourceOK(t *testing.T, srv *Server, arg string) string {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": arg}
	res, err := srv.handleGetNodeSource(context.Background(), req)
	if err != nil {
		t.Fatalf("get_source(%q) error: %v", arg, err)
	}
	if res.IsError {
		t.Fatalf("get_source(%q) tool error: %s", arg, extractResultText(t, res))
	}
	return extractResultText(t, res)
}

func TestGetSource_ResolvesByBareLocalID_4272(t *testing.T) {
	srv := newTestServer(t, getSourceResolutionDoc(t))
	text := getSourceOK(t, srv, "ent_place_order_42")
	if !strings.Contains(text, "MARKER_PLACE_ORDER_BODY") {
		t.Errorf("(a) bare id: body not returned, got:\n%s", text)
	}
}

func TestGetSource_ResolvesByPrefixedID_4272(t *testing.T) {
	srv := newTestServer(t, getSourceResolutionDoc(t))
	text := getSourceOK(t, srv, "upvate-core::ent_place_order_42")
	if !strings.Contains(text, "MARKER_PLACE_ORDER_BODY") {
		t.Errorf("(b) prefixed id: body not returned, got:\n%s", text)
	}
}

func TestGetSource_ResolvesByQualifiedName_4272(t *testing.T) {
	srv := newTestServer(t, getSourceResolutionDoc(t))
	qn := "core.services.order_service.OrderService.place_order"
	text := getSourceOK(t, srv, qn)
	if !strings.Contains(text, "MARKER_PLACE_ORDER_BODY") {
		t.Errorf("(c) qualified_name: body not returned, got:\n%s", text)
	}
}

// TestGetSource_ResolvesByPrefixedQualifiedName_4272 is the load-bearing
// regression: a "<repo>::<qualified_name>" arg. Pre-fix this fell through to
// "node not found" because the prefixed branch only consulted ByID[local] and
// the cross-repo fallback ran LookupAll on the still-prefixed string.
func TestGetSource_ResolvesByPrefixedQualifiedName_4272(t *testing.T) {
	srv := newTestServer(t, getSourceResolutionDoc(t))
	arg := "upvate-core::core.services.order_service.OrderService.place_order"
	text := getSourceOK(t, srv, arg)
	if !strings.Contains(text, "MARKER_PLACE_ORDER_BODY") {
		t.Errorf("(c-prefixed) prefixed qualified_name: body not returned, got:\n%s", text)
	}
}

func TestGetSource_ResolvesByLabel_4272(t *testing.T) {
	srv := newTestServer(t, getSourceResolutionDoc(t))
	text := getSourceOK(t, srv, "place_order")
	if !strings.Contains(text, "MARKER_PLACE_ORDER_BODY") {
		t.Errorf("(d) label: body not returned, got:\n%s", text)
	}
}

// TestGetSource_HelpfulNotFound_4272 asserts the clearer-error change: a
// genuinely-missing arg yields an error that (1) names the attempted resolution
// forms and (2) offers a did-you-mean nearest match — so the caller can
// self-correct. Pre-fix the error was a bare "node not found: <arg>".
func TestGetSource_HelpfulNotFound_4272(t *testing.T) {
	srv := newTestServer(t, getSourceResolutionDoc(t))
	req := mcpapi.CallToolRequest{}
	// Typo of a real label: close enough that did-you-mean should surface it.
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "place_ordr"}
	res, err := srv.handleGetNodeSource(context.Background(), req)
	if err != nil {
		t.Fatalf("get_source error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error for missing arg, got success:\n%s", extractResultText(t, res))
	}
	msg := extractResultText(t, res)
	if !strings.Contains(msg, "place_ordr") {
		t.Errorf("not-found error should echo the arg, got: %s", msg)
	}
	// (1) attempted forms named.
	if !strings.Contains(strings.ToLower(msg), "tried") {
		t.Errorf("not-found error should list attempted forms (id/qualified_name/label), got: %s", msg)
	}
	// (2) did-you-mean nearest match — the close label should be suggested.
	if !strings.Contains(strings.ToLower(msg), "did you mean") || !strings.Contains(msg, "place_order") {
		t.Errorf("not-found error should offer a did-you-mean nearest match (place_order), got: %s", msg)
	}
}
