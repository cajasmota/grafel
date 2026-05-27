// Issue #2692 — production-pipeline guards for ASP.NET Core, Elixir
// Phoenix, and Rust Rocket endpoint synthesis + handler attribution.
//
// Each per-framework subtest runs the full Indexer.Run on a fixture under
// testdata/audit2678_other_trio/<framework>/ and asserts:
//
//   - the expected http_endpoint_definition synthetics exist for the
//     framework's representative endpoints (count check + presence check);
//   - source_file of each definition points at the file containing the
//     handler body (controller/handler source), NOT the routing /
//     registration site (true for annotation-based frameworks by
//     construction, and verified through ResolveHTTPEndpointHandlers'
//     #2680 rebind for the cross-file Phoenix case);
//   - source_line of each definition equals the 1-based line of the
//     handler's `def` / method declaration, matching the convention
//     established by #2677 (DRF) and #2678 (Flask, FastAPI, JS/TS, Go,
//     PHP/Laravel).
//
// The fixtures live alongside this test file; keep the asserted line
// numbers in sync with the source files in testdata/audit2678_other_trio.
package main

import (
	"strings"
	"testing"
)

// TestIssue2692_OtherTrio_EndpointAttribution_ASPNet runs the production
// pipeline against the ASP.NET Core fixture and asserts the synthetic
// endpoints attribute to WidgetsController.cs at the method declaration
// lines.
func TestIssue2692_OtherTrio_EndpointAttribution_ASPNet(t *testing.T) {
	doc := runIndexerOn(t,
		"testdata/audit2678_other_trio/aspnet",
		"issue2692_aspnet", nil)

	// Expected endpoints + the LINE of the method declaration in
	// Controllers/WidgetsController.cs.
	//
	// The C# extractor anchors method_declaration entities at the start
	// of the leading attribute_list (this is the tree-sitter convention
	// and matches Spring/JAX-RS where the @-annotation line is the method
	// entity's start_line). The resolver rebind (#2680) therefore lands
	// the synthetic at the [Http...] attribute line, which is the
	// conventional "method def line" for attribute-routed C#.
	//
	// [HttpGet]         on line 9  → method_declaration starts L9   (List)
	// [HttpGet("{id}")] on line 15 → method_declaration starts L15  (Get)
	// [HttpPost]        on line 21 → method_declaration starts L21  (Create)
	cases := []struct {
		id       string
		wantLine int
	}{
		{"http:GET:/api/widgets", 9},
		{"http:GET:/api/widgets/{id}", 15},
		{"http:POST:/api/widgets", 21},
	}
	for _, tc := range cases {
		found := false
		for _, e := range doc.Entities {
			if e.Kind != "http_endpoint_definition" && e.Kind != "http_endpoint" {
				continue
			}
			if e.ID != tc.id && e.Name != tc.id {
				continue
			}
			found = true
			if !strings.HasSuffix(e.SourceFile, "WidgetsController.cs") {
				t.Errorf("aspnet %s: source_file=%q, want suffix WidgetsController.cs", tc.id, e.SourceFile)
			}
			if e.StartLine != tc.wantLine {
				t.Errorf("aspnet %s: start_line=%d, want %d (method def line)", tc.id, e.StartLine, tc.wantLine)
			}
			if e.Properties["framework"] != "aspnet_core" {
				t.Errorf("aspnet %s: framework=%q, want aspnet_core", tc.id, e.Properties["framework"])
			}
		}
		if !found {
			t.Errorf("aspnet: missing http_endpoint_definition %s", tc.id)
		}
	}
}

// TestIssue2692_OtherTrio_EndpointAttribution_Phoenix runs the production
// pipeline against the Phoenix fixture. Routes are declared in router.ex
// but the handler bodies live in
// controllers/user_controller.ex and controllers/widget_controller.ex.
// The resolver rebind (#2680 + #2692 file-hint extension) must repoint
// source_file/start_line to the controller file's `def <action>` line.
func TestIssue2692_OtherTrio_EndpointAttribution_Phoenix(t *testing.T) {
	doc := runIndexerOn(t,
		"testdata/audit2678_other_trio/phoenix",
		"issue2692_phoenix", nil)

	// user_controller.ex:
	//   def index   on line 4
	//   def show    on line 8
	//   def create  on line 12
	// widget_controller.ex:
	//   def index   on line 4
	//   def show    on line 8
	//   def create  on line 12
	type want struct {
		id       string
		fileHint string
		wantLine int
	}
	cases := []want{
		{"http:GET:/api/users", "user_controller.ex", 4},
		{"http:GET:/api/users/{id}", "user_controller.ex", 8},
		{"http:POST:/api/users", "user_controller.ex", 12},
		// resources "/widgets", WidgetController, only: [:index, :show, :create]
		{"http:GET:/api/widgets", "widget_controller.ex", 4},
		{"http:GET:/api/widgets/{id}", "widget_controller.ex", 8},
		{"http:POST:/api/widgets", "widget_controller.ex", 12},
	}
	for _, tc := range cases {
		found := false
		for _, e := range doc.Entities {
			if e.Kind != "http_endpoint_definition" && e.Kind != "http_endpoint" {
				continue
			}
			if e.ID != tc.id && e.Name != tc.id {
				continue
			}
			found = true
			if !strings.HasSuffix(e.SourceFile, tc.fileHint) {
				t.Errorf("phoenix %s: source_file=%q, want suffix %s (resolver should rebind from router.ex to controller file)", tc.id, e.SourceFile, tc.fileHint)
			}
			if e.StartLine != tc.wantLine {
				t.Errorf("phoenix %s: start_line=%d, want %d (`def %s` line in controller)", tc.id, e.StartLine, tc.wantLine, tc.id)
			}
			fw := e.Properties["framework"]
			if fw != "phoenix" && fw != "phoenix_resources" {
				t.Errorf("phoenix %s: framework=%q, want phoenix/phoenix_resources", tc.id, fw)
			}
		}
		if !found {
			t.Errorf("phoenix: missing http_endpoint_definition %s", tc.id)
		}
	}
}

// TestIssue2692_OtherTrio_EndpointAttribution_Rocket runs the production
// pipeline against the Rocket fixture. Rocket attribute macros sit on the
// same `fn` they decorate, so source_file is correct by construction and
// the resolver rebind populates start_line from the SCOPE.Operation entity.
func TestIssue2692_OtherTrio_EndpointAttribution_Rocket(t *testing.T) {
	doc := runIndexerOn(t,
		"testdata/audit2678_other_trio/rocket",
		"issue2692_rocket", nil)

	// src/main.rs:
	//   fn hello        line 5
	//   fn show_user    line 10
	//   fn create_user  line 15
	cases := []struct {
		id       string
		wantLine int
	}{
		{"http:GET:/hello", 5},
		{"http:GET:/users/{id}", 10},
		{"http:POST:/users", 15},
	}
	for _, tc := range cases {
		found := false
		for _, e := range doc.Entities {
			if e.Kind != "http_endpoint_definition" && e.Kind != "http_endpoint" {
				continue
			}
			if e.ID != tc.id && e.Name != tc.id {
				continue
			}
			found = true
			if !strings.HasSuffix(e.SourceFile, "main.rs") {
				t.Errorf("rocket %s: source_file=%q, want suffix main.rs", tc.id, e.SourceFile)
			}
			if e.StartLine != tc.wantLine {
				t.Errorf("rocket %s: start_line=%d, want %d (`fn` line)", tc.id, e.StartLine, tc.wantLine)
			}
			if e.Properties["framework"] != "rocket" {
				t.Errorf("rocket %s: framework=%q, want rocket", tc.id, e.Properties["framework"])
			}
		}
		if !found {
			t.Errorf("rocket: missing http_endpoint_definition %s", tc.id)
		}
	}
}
