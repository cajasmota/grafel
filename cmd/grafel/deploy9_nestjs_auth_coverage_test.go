package main

// deploy9_nestjs_auth_coverage_test.go — two-layer regression guard for the
// deploy-9 finding: on acme-backend-v2 (acme-v2), grafel_auth_coverage
// reported `covered: 0 / overall_coverage: 0` for ALL 305 endpoints because the
// app gates routes with a GLOBAL guard + metadata decorators (@RequirePage /
// @Authenticated / @Public ...) rather than @UseGuards — and the JS/TS auth
// resolver only recognised @UseGuards. The fix teaches the resolver the
// metadata-decorator family (method + class level, with @Public opt-out).
//
// The fixture (testdata/deploy9_nestauth) copies the REAL controller shapes
// (buildings.controller.ts → devices.controller.ts; auth.controller.ts) and the
// REAL decorators (shared/auth.decorators.ts), not edited in acme-backend-v2.
//
// LAYER 1 — FULL PIPELINE: Index() → graph.{fb,json} → assert each guarded
// endpoint carries the auth signal (auth_required=true + auth_guard + the page),
// and the @Public endpoints carry auth_required=false (NOT protected).
//
// LAYER 2 — MCP: load the pipeline graph into the REAL grafel_auth_coverage
// handler via mcp.NewServer + GetTool(...).Handler and assert covered > 0 and
// the @Public endpoints are correctly flagged no-auth.
//
// Fail-before/pass-after: before the fix every endpoint resolves to
// method="unknown" (no auth_required, no auth_guard) → covered == 0; after the
// fix the six guarded routes carry the signal → covered == 6.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/mcp"
)

// deploy9GuardedRoutes are the endpoints the fixture protects via a metadata
// decorator (method- or class-level). Each must carry the auth signal after the
// fix. `page` is the literal page/permission the decorator names ("" when the
// decorator carries none, e.g. @Authenticated / @InternalKeyOrAuth / inherited).
var deploy9GuardedRoutes = []struct {
	verb, path, page string
}{
	{"GET", "/api/v1/devices", "devices.read"},                      // @RequirePage('devices.read')
	{"POST", "/api/v1/devices", "devices.write"},                    // @RequirePage('devices.write')
	{"GET", "/api/v1/devices/summary", "devices.read,reports.read"}, // @AnyPage('devices.read','reports.read')
	{"GET", "/api/v1/devices/{id}/notes", ""},                       // @Authenticated()
	{"DELETE", "/api/v1/devices/{id}", "devices.write"},             // @RequirePage('devices.write')
	{"PATCH", "/api/v1/devices/{id}", "devices.write"},              // @RequirePage('devices.write')
	{"POST", "/api/v1/auth/webhook", ""},                            // @InternalKeyOrAuth()
	{"GET", "/api/v1/auth/me", ""},                                  // inherits class-level @Authenticated()
}

// deploy9PublicRoutes are the genuinely-public routes (@Public()). They must NOT
// be flagged protected, and grafel_auth_coverage must report them no-auth.
var deploy9PublicRoutes = []struct{ verb, path string }{
	{"GET", "/api/v1/devices/health"}, // method-level @Public()
	{"POST", "/api/v1/auth/login"},    // @Public() overriding class @Authenticated()
	{"POST", "/api/v1/auth/register"}, // @Public()
}

func deploy9FindEndpoint(eps []*graph.Entity, verb, path string) *graph.Entity {
	for _, e := range eps {
		if e.PropLen() == 0 {
			continue
		}
		if e.PropGet("verb") == verb && e.PropGet("path") == path {
			return e
		}
	}
	return nil
}

// TestDeploy9_NestJSMetadataAuth_FullPipelineAndMCP is the two-layer test.
func TestDeploy9_NestJSMetadataAuth_FullPipelineAndMCP(t *testing.T) {
	// Isolate per-repo state in a temp dir (never touch ~/.grafel).
	stateDir := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", stateDir)

	fixture, err := filepath.Abs("testdata/deploy9_nestauth")
	if err != nil {
		t.Fatalf("abs fixture: %v", err)
	}

	// FULL PIPELINE: empty outPath → Index writes per-repo state (graph.fb +
	// graph.json) under GRAFEL_DAEMON_ROOT at daemon.GraphPathForRepo(fixture).
	// The MCP server discovers the SAME artifact via FindGraphFileAnyRef(fixture),
	// so both layers read the identical pipeline output.
	if err := Index(fixture, "", "deploy9_nestauth", nil, false, false,
		WithExportJSON(true)); err != nil {
		t.Fatalf("Index: %v", err)
	}

	graphPath, _ := daemon.FindGraphFileAnyRef(fixture)
	if graphPath == "" {
		t.Fatal("pipeline graph not discoverable under daemon state dir")
	}
	doc, err := graph.LoadGraphFromDir(filepath.Dir(graphPath))
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}

	var eps []*graph.Entity
	for i := range doc.Entities {
		if doc.Entities[i].Kind == "http_endpoint_definition" {
			eps = append(eps, &doc.Entities[i])
		}
	}
	if len(eps) == 0 {
		t.Fatal("no http_endpoint_definition entities emitted — pipeline did not see the NestJS controllers")
	}

	// ---- LAYER 1: auth stamping on the producer endpoints ----------------
	t.Run("Layer1_FullPipeline_AuthStamping", func(t *testing.T) {
		for _, r := range deploy9GuardedRoutes {
			ep := deploy9FindEndpoint(eps, r.verb, r.path)
			if ep == nil {
				t.Errorf("guarded endpoint %s %s not emitted", r.verb, r.path)
				continue
			}
			if ep.PropGet("auth_required") != "true" {
				t.Errorf("%s %s: auth_required=%q, want true (fail-before: empty)",
					r.verb, r.path, ep.PropGet("auth_required"))
			}
			// MCP signal-1 key must be present so auth_coverage's cheap property
			// check fires.
			if ep.PropGet("auth_guard") == "" {
				t.Errorf("%s %s: no auth_guard signal-1 key stamped", r.verb, r.path)
			}
			if r.page != "" {
				if got := ep.PropGet("auth_page"); got != r.page {
					t.Errorf("%s %s: auth_page=%q, want %q", r.verb, r.path, got, r.page)
				}
				if got := ep.PropGet("auth_permissions"); got != r.page {
					t.Errorf("%s %s: auth_permissions=%q, want %q", r.verb, r.path, got, r.page)
				}
			}
		}
		for _, r := range deploy9PublicRoutes {
			ep := deploy9FindEndpoint(eps, r.verb, r.path)
			if ep == nil {
				t.Errorf("public endpoint %s %s not emitted", r.verb, r.path)
				continue
			}
			if ep.PropGet("auth_required") == "true" {
				t.Errorf("%s %s: auth_required=true, want false/unset (@Public must not be flagged protected)",
					r.verb, r.path)
			}
			if ep.PropGet("auth_guard") != "" {
				t.Errorf("%s %s: auth_guard=%q stamped on a @Public route",
					r.verb, r.path, ep.PropGet("auth_guard"))
			}
		}
	})

	// ---- LAYER 2: grafel_auth_coverage MCP handler -------------------
	t.Run("Layer2_MCP_AuthCoverage_CoveredGreaterThanZero", func(t *testing.T) {
		// Point a registry at the pipeline-produced graph (graph.fb in outDir).
		regPath := filepath.Join(t.TempDir(), "registry.json")
		reg := map[string]any{
			"groups": map[string]any{
				"deploy9": map[string]any{
					"repos": map[string]any{
						"deploy9_nestauth": map[string]any{
							"path": fixture,
						},
					},
				},
			},
		}
		regBytes, err := json.Marshal(reg)
		if err != nil {
			t.Fatalf("marshal registry: %v", err)
		}
		if err := os.WriteFile(regPath, regBytes, 0o644); err != nil {
			t.Fatalf("write registry: %v", err)
		}

		srv, err := mcp.NewServer(mcp.Config{RegistryPath: regPath})
		if err != nil {
			t.Fatalf("mcp.NewServer: %v", err)
		}
		// Release the mmap'd graph.fb Reader before any TempDir cleanup runs.
		// On Windows the mapping locks graph.fb (which lives under the outer
		// test's stateDir/GRAFEL_DAEMON_ROOT), so leaking it makes that
		// dir's RemoveAll fail with "Access is denied" (#4285).
		t.Cleanup(srv.Close)
		st := srv.MCP.GetTool("grafel_auth_coverage")
		if st == nil {
			t.Fatal("grafel_auth_coverage not registered")
		}

		req := mcpapi.CallToolRequest{}
		req.Params.Name = "grafel_auth_coverage"
		req.Params.Arguments = map[string]any{"group": "deploy9", "format": "full"}
		res, err := st.Handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if res == nil || res.IsError {
			t.Fatalf("tool returned error: %+v", res)
		}

		out := extractDeploy9JSON(t, res)

		// overall_coverage > 0 and total covered > 0 (fail-before: both 0).
		summaries, _ := out["repo_summaries"].([]any)
		if len(summaries) == 0 {
			t.Fatalf("no repo_summaries in result: %v", out)
		}
		totalCovered := 0
		for _, s := range summaries {
			sm, _ := s.(map[string]any)
			if c, ok := sm["covered"].(float64); ok {
				totalCovered += int(c)
			}
		}
		if totalCovered == 0 {
			t.Fatalf("covered == 0 (the deploy-9 bug): NestJS metadata-decorator auth not recognised")
		}
		if oc, _ := out["overall_coverage"].(float64); oc == 0 {
			t.Fatalf("overall_coverage == 0 (the deploy-9 bug)")
		}
		// Exactly the 8 guarded routes are covered.
		if totalCovered != len(deploy9GuardedRoutes) {
			t.Errorf("covered=%d, want %d (the guarded routes)", totalCovered, len(deploy9GuardedRoutes))
		}

		// The @Public endpoints must be present and flagged no-auth.
		endpoints, _ := out["endpoints"].([]any)
		byPath := map[string]map[string]any{}
		for _, raw := range endpoints {
			ep, _ := raw.(map[string]any)
			if ep == nil {
				continue
			}
			verb, _ := ep["method"].(string)
			path, _ := ep["path"].(string)
			byPath[verb+" "+path] = ep
		}
		for _, r := range deploy9PublicRoutes {
			ep, ok := byPath[r.verb+" "+r.path]
			if !ok {
				t.Errorf("public endpoint %s %s missing from auth_coverage results", r.verb, r.path)
				continue
			}
			if ha, _ := ep["has_auth"].(bool); ha {
				t.Errorf("%s %s (@Public): has_auth=true, want false", r.verb, r.path)
			}
		}
		// And every guarded route must report has_auth=true.
		for _, r := range deploy9GuardedRoutes {
			ep, ok := byPath[r.verb+" "+r.path]
			if !ok {
				t.Errorf("guarded endpoint %s %s missing from results", r.verb, r.path)
				continue
			}
			if ha, _ := ep["has_auth"].(bool); !ha {
				t.Errorf("%s %s: has_auth=false, want true", r.verb, r.path)
			}
		}
	})
}

// extractDeploy9JSON pulls the JSON object out of an MCP tool result's first
// text content block.
func extractDeploy9JSON(t *testing.T, res *mcpapi.CallToolResult) map[string]any {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("empty result content")
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(mcpapi.TextContent); ok {
			text = tc.Text
			break
		}
	}
	if text == "" {
		t.Fatalf("no TextContent in result: %T", res.Content[0])
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal result JSON: %v\n%s", err, text)
	}
	return out
}
