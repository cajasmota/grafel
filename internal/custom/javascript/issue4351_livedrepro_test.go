package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
)

// Issue #4351 LIVE-REPRO (extractor side).
//
// Byte-copies of REAL core-backend-v3 *.e2e-spec.ts files are committed under
// testdata/e2e_4351. We run the ACTUAL jest extractor over them and assert the
// supertest route-by-string calls are captured onto the one-per-spec
// test_suite's `e2e_route_calls` property — the raw material the resolve pass
// (TestIssue4351_E2ERouteTestsLinkToEndpoints in internal/engine) turns into
// TESTS→http_endpoint_definition edges.
//
// Before #4351 the jest extractor recorded NO route information at all, so the
// resolve pass had nothing to match on and e2e suites never connected to the
// endpoints they exercise.

func loadSpec4351(t *testing.T, base, repoPath string) extreg.FileInput {
	t.Helper()
	p := filepath.Join("testdata", "e2e_4351", base)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read spec %s: %v", p, err)
	}
	return extreg.FileInput{Path: repoPath, Language: "typescript", Content: b}
}

func jestExtract4351(t *testing.T, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_js_jest")
	if !ok {
		t.Fatal("custom_js_jest not registered")
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func suiteRouteCalls(t *testing.T, ents []types.EntityRecord) []string {
	t.Helper()
	for _, e := range ents {
		if e.Subtype == "test_suite" {
			raw := e.Properties["e2e_route_calls"]
			if raw == "" {
				return nil
			}
			return strings.Split(raw, "\n")
		}
	}
	return nil
}

// TestIssue4351_AuthGuardSpec_RouteCallsCaptured runs the real auth-guard e2e
// spec (literal route strings like get('/probe/buildings')) and asserts the
// verb+route pairs land on the suite.
func TestIssue4351_AuthGuardSpec_RouteCallsCaptured(t *testing.T) {
	ents := jestExtract4351(t, loadSpec4351(t,
		"auth-guard.e2e-spec.ts", "test/auth-guard.e2e-spec.ts"))

	calls := suiteRouteCalls(t, ents)
	if len(calls) == 0 {
		t.Fatal("no e2e_route_calls captured from auth-guard spec (#4351)")
	}
	want := map[string]bool{
		"GET /probe/public":          false,
		"GET /probe/buildings":       false,
		"POST /probe/buildings":      false,
		"GET /probe/devices-lite":    false,
		"GET /probe/to-reschedule":   false,
		"GET /probe/devices-lite-composed": false,
	}
	for _, c := range calls {
		if _, ok := want[c]; ok {
			want[c] = true
		}
	}
	for k, got := range want {
		if !got {
			t.Errorf("expected captured route call %q, got=%v", k, calls)
		}
	}
}

// TestIssue4351_AlternateAddressSpec_ConstFoldAndTemplate runs the real
// alternate-address e2e spec which uses `const ROUTE = '/api/v1/alternate-addresses'`
// plus `${ROUTE}/${id}` template calls — exercising const-folding and
// path-param templating in the extractor.
func TestIssue4351_AlternateAddressSpec_ConstFoldAndTemplate(t *testing.T) {
	ents := jestExtract4351(t, loadSpec4351(t,
		"alternate-address-write.e2e-spec.ts", "test/alternate-address-write.e2e-spec.ts"))

	calls := suiteRouteCalls(t, ents)
	if len(calls) == 0 {
		t.Fatal("no e2e_route_calls captured from alternate-address spec (#4351)")
	}
	joined := strings.Join(calls, "|")
	// POST to the folded ROUTE constant.
	if !strings.Contains(joined, "POST /api/v1/alternate-addresses") {
		t.Errorf("ROUTE const not folded for POST; calls=%v", calls)
	}
	// PATCH `${ROUTE}/${id}` → folded base + ${id} param left for the resolver.
	sawTemplatedPatch := false
	for _, c := range calls {
		if strings.HasPrefix(c, "PATCH /api/v1/alternate-addresses/") {
			sawTemplatedPatch = true
		}
	}
	if !sawTemplatedPatch {
		t.Errorf("templated PATCH route not captured; calls=%v", calls)
	}
}

// TestIssue4351_AppSpec_RootRoute covers the trivial get('/') case.
func TestIssue4351_AppSpec_RootRoute(t *testing.T) {
	ents := jestExtract4351(t, loadSpec4351(t,
		"app.e2e-spec.ts", "test/app.e2e-spec.ts"))
	calls := suiteRouteCalls(t, ents)
	found := false
	for _, c := range calls {
		if c == "GET /" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected GET / captured from app spec; calls=%v", calls)
	}
}
