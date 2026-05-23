// find_paths_1690_test.go — regression tests for #1690.
//
// Covers two failure modes from the iter3 calibration:
//
//	(a) The cross-repo links file is written with a repo-prefix that does
//	    not exactly match the registered fleet slug (the historical case
//	    is "<dir-basename-with-underscores>" vs "<fleet-slug-with-dashes>"
//	    — see iter3 #1690 add-rec #1). With the alias map in place,
//	    find_paths must still walk the cross_repo_link edge.
//
//	(b) The cross-repo link's Target lands on an http_endpoint_definition
//	    (not the handler function), and the handler is connected via an
//	    out-of-direction IMPLEMENTS edge (handler → endpoint). find_paths
//	    must walk IMPLEMENTS in reverse so the BFS can resolve the
//	    handler from the endpoint.
package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// scenario (a) — link's Target prefix uses the on-disk dir name with
// underscores while the registered slug uses dashes. find_paths must
// canonicalise both sides to the slug so BFS can continue.
func TestFindPaths_1690_RepoPrefixAlias(t *testing.T) {
	docMobile := &graph.Document{
		Repo: "core-mobile",
		Entities: []graph.Entity{
			{ID: "caller_fn", Name: "createInspectionDeficiency", Kind: "Function", SourceFile: "api.ts"},
			{ID: "ep_call", Name: "http:POST:/inspections", Kind: "http_endpoint_call", SourceFile: "api.ts"},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "caller_fn", ToID: "ep_call", Kind: "FETCHES"},
		},
	}
	docCore := &graph.Document{
		Repo: "upvate-core", // fleet slug
		Entities: []graph.Entity{
			{ID: "handler_fn", Name: "InspectionViewSet.create_deficiency", Kind: "Function", SourceFile: "views.py"},
		},
	}
	srv := newTestServerWithDocs(t, map[string]*graph.Document{
		"core-mobile": docMobile,
		"upvate-core": docCore,
	})
	lg := srv.State.Group("test")
	// The links emitter wrote the upvate-core side as `upvate_core::handler_fn`
	// (path-basename with underscores) — this MUST still resolve.
	lg.Links = []CrossRepoLink{
		{
			Source:   prefixedID("core-mobile", "ep_call"),
			Target:   "upvate_core::handler_fn",
			Relation: "calls",
			Method:   "http",
		},
	}

	res := callEndpointTool(t, srv.handleFindPaths, map[string]any{
		"group": "test",
		"from":  prefixedID("core-mobile", "caller_fn"),
		"to":    prefixedID("upvate-core", "handler_fn"),
	})
	if found, _ := res["found"].(bool); !found {
		t.Fatalf("expected cross-repo path via underscore-prefixed link target, got: %v", res)
	}
	if crosses, _ := res["crosses_repos"].(bool); !crosses {
		t.Errorf("expected crosses_repos=true, got %v", res["crosses_repos"])
	}
	// Path should be: caller_fn -- FETCHES --> ep_call -- calls (xrepo) --> handler_fn
	if hop, _ := res["hop_count"].(float64); hop != 2 {
		t.Errorf("expected 2 hops, got %v", hop)
	}
	// The xrepo edge kind ("calls" via http method) MUST appear in edge_kinds.
	if !containsKind(res, "calls") {
		t.Errorf("expected edge_kinds to include 'calls', got %v", res["edge_kinds"])
	}
}

// scenario (b) — the cross-repo link lands on an http_endpoint_definition
// in the producer repo (not the handler function). The handler is
// connected via `handler --IMPLEMENTS--> http_endpoint_definition`, so
// BFS must walk IMPLEMENTS in reverse.
func TestFindPaths_1690_ReverseImplementsAcrossRepo(t *testing.T) {
	docMobile := &graph.Document{
		Repo: "core-mobile",
		Entities: []graph.Entity{
			{ID: "caller_fn", Name: "createInspectionDeficiency", Kind: "Function", SourceFile: "api.ts"},
			{ID: "ep_call", Name: "http:POST:/inspections", Kind: "http_endpoint_call", SourceFile: "api.ts"},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "caller_fn", ToID: "ep_call", Kind: "FETCHES"},
		},
	}
	docCore := &graph.Document{
		Repo: "upvate-core",
		Entities: []graph.Entity{
			{ID: "handler_fn", Name: "InspectionViewSet.create_deficiency", Kind: "Function", SourceFile: "views.py"},
			{ID: "ep_def", Name: "http:POST:/inspections", Kind: "http_endpoint_definition", SourceFile: "views.py"},
		},
		Relationships: []graph.Relationship{
			// handler --IMPLEMENTS--> endpoint_definition  (so the BFS must
			// walk this edge IN REVERSE to reach handler from endpoint).
			{ID: "r2", FromID: "handler_fn", ToID: "ep_def", Kind: "IMPLEMENTS"},
		},
	}
	srv := newTestServerWithDocs(t, map[string]*graph.Document{
		"core-mobile": docMobile,
		"upvate-core": docCore,
	})
	lg := srv.State.Group("test")
	lg.Links = []CrossRepoLink{
		{
			Source:   prefixedID("core-mobile", "ep_call"),
			Target:   prefixedID("upvate-core", "ep_def"), // lands on the endpoint, NOT the handler
			Relation: "calls",
			Method:   "http",
		},
	}

	res := callEndpointTool(t, srv.handleFindPaths, map[string]any{
		"group": "test",
		"from":  prefixedID("core-mobile", "caller_fn"),
		"to":    prefixedID("upvate-core", "handler_fn"),
	})
	if found, _ := res["found"].(bool); !found {
		t.Fatalf("expected find_paths to walk IMPLEMENTS in reverse, got: %v", res)
	}
	if crosses, _ := res["crosses_repos"].(bool); !crosses {
		t.Errorf("expected crosses_repos=true")
	}
	// Hop chain: caller_fn → ep_call → ep_def → handler_fn = 3 hops.
	if hop, _ := res["hop_count"].(float64); hop != 3 {
		t.Errorf("expected 3 hops (caller→ep_call→ep_def→handler), got %v", hop)
	}
	if !containsKind(res, "IMPLEMENTS_REVERSED") {
		t.Errorf("expected edge_kinds to include 'IMPLEMENTS_REVERSED', got %v", res["edge_kinds"])
	}
}

// scenario (c) — when neither alias nor reverse-IMPLEMENTS applies the
// handler returns the same "no path" shape as before. Guards against
// regression where the new expansion accidentally invents paths.
func TestFindPaths_1690_NoPathStillNoPath(t *testing.T) {
	doc := &graph.Document{
		Repo: "solo",
		Entities: []graph.Entity{
			{ID: "a", Name: "A", Kind: "Function"},
			{ID: "b", Name: "B", Kind: "Function"}, // unconnected
		},
	}
	srv := newTestServerWithDocs(t, map[string]*graph.Document{"solo": doc})
	res := callEndpointTool(t, srv.handleFindPaths, map[string]any{
		"group": "test",
		"from":  "solo::a",
		"to":    "solo::b",
	})
	if found, _ := res["found"].(bool); found {
		t.Errorf("expected found=false on unconnected nodes, got: %v", res)
	}
}

// containsKind reports whether res["edge_kinds"] contains the literal kind.
func containsKind(res map[string]any, want string) bool {
	raw, _ := res["edge_kinds"]
	// JSON path: []any of strings.
	if arr, ok := raw.([]any); ok {
		for _, k := range arr {
			if s, ok := k.(string); ok && s == want {
				return true
			}
		}
	}
	// Direct path when the handler is invoked without round-tripping JSON.
	if arr, ok := raw.([]string); ok {
		for _, k := range arr {
			if k == want {
				return true
			}
		}
	}
	// Fallback: stringify and substring-match.
	b, _ := json.Marshal(raw)
	return strings.Contains(string(b), want)
}
