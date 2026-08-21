package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #6450 — unit cover for the index-time base-URL constant fold.
//
// The golden fixture express-baseurl-mini grades the end-to-end outcome
// (call → definition FETCHES instead of UNRESOLVED_FETCH). What it cannot
// grade is the two GUARDS, because both are unreachable through the
// production synthesizer: a path whose first segment is a placeholder is
// always classified url_kind=dynamic_baseurl (urlKindFromPath →
// hasDynamicBaseURLPath), and a substrate Binding.Ident is always a valid
// identifier (every sniffer's regex captures `[A-Za-z_$][\w$]*`), so an
// ident carrying punctuation could never bind anyway. Deleting either guard
// leaves all 25 goldens green — they are EQUIVALENT mutants at the fixture
// level. These tests pin them directly so the guards are enforced rather
// than merely decorative, and so a future change that makes either branch
// reachable does not silently inherit the permissive behaviour.

func TestLeadingTemplateIdentEngine_6450(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/{BASE}/things", "BASE"},
		{"/{BASE}", "BASE"},
		{"/{$root}/x", "$root"},
		{"/{api_v2}/x", "api_v2"},
		{"/api/things", ""},        // no leading placeholder
		{"/api/{id}", ""},          // placeholder is not leading
		{"", ""},                   // empty
		{"/{}/x", ""},              // empty ident
		{"/{base_url.rstrip(", ""}, // unbalanced + punctuation
		{"/{a.b}/x", ""},           // dotted expression, not a bindable ident
		{"/{a b}/x", ""},           // whitespace
		{"/{a-b}/x", ""},           // operator character
		{"/{`${x}`}/y", ""},        // nested interpolation
	}
	for _, c := range cases {
		if got := leadingTemplateIdentEngine(c.path); got != c.want {
			t.Errorf("leadingTemplateIdentEngine(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestStripURLPrefixEngine_6450(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/api", "/api"},
		{"http://svc.internal/api", "/api"},
		{"https://svc.internal/api/v2", "/api/v2"},
		{"https://svc.internal", ""}, // host only, no path
		{"api", "api"},
	}
	for _, c := range cases {
		if got := stripURLPrefixEngine(c.in); got != c.want {
			t.Errorf("stripURLPrefixEngine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// writeFoldFixture lays down a tiny two-file JS tree and returns its root.
func writeFoldFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "client"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "export const BASE = '/api';\nexport const ORIGIN = 'http://svc.internal/api';\n"
	if err := os.WriteFile(filepath.Join(root, "client", "config.js"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	api := "import { BASE, ORIGIN } from './config';\nfetch(`${BASE}/things`);\n"
	if err := os.WriteFile(filepath.Join(root, "client", "api.js"), []byte(api), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func callRecord(name, path, urlKind string) types.EntityRecord {
	return types.EntityRecord{
		Kind:       httpEndpointCallKind,
		Name:       name,
		SourceFile: "client/api.js",
		Properties: map[string]string{
			"path":            path,
			"url_kind":        urlKind,
			"caller_file":     "client/api.js",
			"verb":            "GET",
			"dynamic_baseurl": "true",
		},
	}
}

func TestFoldConsumerHTTPBaseURLs_FoldsAndFreezesIdentity_6450(t *testing.T) {
	root := writeFoldFixture(t)
	recs := []types.EntityRecord{
		// The declaring file must itself be referenced by some record —
		// buildRepoSubstrateResolver derives its file set from SourceFile,
		// exactly as internal/links.buildResolver does.
		{Kind: "SCOPE.Component", Name: "config", SourceFile: "client/config.js"},
		callRecord("http:GET:/{BASE}/things", "/{BASE}/things", "dynamic_baseurl"),
		callRecord("http:GET:/{ORIGIN}/health", "/{ORIGIN}/health", "dynamic_baseurl"),
	}
	res := FoldConsumerHTTPBaseURLs(recs, root)
	if res.Candidates != 2 || res.Folded != 2 {
		t.Fatalf("candidates=%d folded=%d, want 2/2", res.Candidates, res.Folded)
	}
	if got := recs[1].Properties["path"]; got != "/api/things" {
		t.Errorf("path = %q, want /api/things", got)
	}
	// Scheme + host must be stripped before splicing.
	if got := recs[2].Properties["path"]; got != "/api/health" {
		t.Errorf("path = %q, want /api/health", got)
	}
	// IDENTITY CONTRACT: Name is frozen. Rewriting it would churn the
	// entity ID of every dynamic-base-URL call site on every index.
	if recs[1].Name != "http:GET:/{BASE}/things" {
		t.Errorf("Name moved to %q — the fold must rewrite `path` ONLY", recs[1].Name)
	}
	if recs[1].Properties["url_kind"] != "literal" {
		t.Errorf("url_kind = %q, want literal", recs[1].Properties["url_kind"])
	}
	if _, still := recs[1].Properties["dynamic_baseurl"]; still {
		t.Error("dynamic_baseurl marker survived the fold")
	}
	if recs[1].Properties["substrate_resolved_value"] != "/api" {
		t.Errorf("substrate_resolved_value = %q, want /api", recs[1].Properties["substrate_resolved_value"])
	}
	if recs[1].Properties["substrate_resolved_via"] == "" {
		t.Error("substrate_resolved_via not stamped — the provenance trace is the audit surface")
	}
}

func TestFoldConsumerHTTPBaseURLs_Guards_6450(t *testing.T) {
	root := writeFoldFixture(t)

	t.Run("url_kind guard", func(t *testing.T) {
		recs := []types.EntityRecord{
			{Kind: "SCOPE.Component", Name: "config", SourceFile: "client/config.js"},
			callRecord("http:GET:/{BASE}/things", "/{BASE}/things", "literal"),
		}
		res := FoldConsumerHTTPBaseURLs(recs, root)
		if res.Candidates != 0 || res.Folded != 0 {
			t.Fatalf("candidates=%d folded=%d, want 0/0 — a call already classified "+
				"literal must not be re-folded", res.Candidates, res.Folded)
		}
		if recs[1].Properties["path"] != "/{BASE}/things" {
			t.Errorf("path was rewritten to %q despite url_kind=literal", recs[1].Properties["path"])
		}
	})

	t.Run("unbindable ident", func(t *testing.T) {
		recs := []types.EntityRecord{
			{Kind: "SCOPE.Component", Name: "config", SourceFile: "client/config.js"},
			callRecord("http:GET:/{NOPE}/things", "/{NOPE}/things", "dynamic_baseurl"),
		}
		res := FoldConsumerHTTPBaseURLs(recs, root)
		if res.Candidates != 1 {
			t.Fatalf("candidates=%d, want 1", res.Candidates)
		}
		if res.Folded != 0 {
			t.Fatalf("folded=%d, want 0 — NOPE binds to nothing", res.Folded)
		}
		if recs[1].Properties["path"] != "/{NOPE}/things" {
			t.Errorf("path mutated to %q on a failed resolution", recs[1].Properties["path"])
		}
		if recs[1].Properties["url_kind"] != "dynamic_baseurl" {
			t.Error("url_kind reclassified on a failed resolution")
		}
	})

	t.Run("empty srcRoot is a no-op", func(t *testing.T) {
		recs := []types.EntityRecord{
			{Kind: "SCOPE.Component", Name: "config", SourceFile: "client/config.js"},
			callRecord("http:GET:/{BASE}/things", "/{BASE}/things", "dynamic_baseurl"),
		}
		res := FoldConsumerHTTPBaseURLs(recs, "")
		if res.Candidates != 0 || res.Folded != 0 {
			t.Fatalf("candidates=%d folded=%d, want 0/0", res.Candidates, res.Folded)
		}
		if recs[1].Properties["path"] != "/{BASE}/things" {
			t.Errorf("path mutated with no source root: %q", recs[1].Properties["path"])
		}
	})

	t.Run("definition records are never touched", func(t *testing.T) {
		def := types.EntityRecord{
			Kind:       httpEndpointDefinitionKind,
			Name:       "http:GET:/{BASE}/things",
			SourceFile: "client/api.js",
			Properties: map[string]string{
				"path":     "/{BASE}/things",
				"url_kind": "dynamic_baseurl",
			},
		}
		recs := []types.EntityRecord{def}
		res := FoldConsumerHTTPBaseURLs(recs, root)
		if res.Candidates != 0 || res.Folded != 0 {
			t.Fatalf("candidates=%d folded=%d, want 0/0 — the fold is consumer-side only",
				res.Candidates, res.Folded)
		}
	})
}

// The resolver must not bind an ident declared in a file the use-site does
// not import. Cross-file resolution goes through the IMPORTS hop only.
func TestRepoSubstrateResolver_NoAmbientBinding_6450(t *testing.T) {
	root := writeFoldFixture(t)
	// A second, UNIMPORTED module that also declares BASE with a different
	// value. Resolving from a file that imports neither must fail.
	other := "export const BASE = '/wrong';\n"
	if err := os.WriteFile(filepath.Join(root, "client", "other.js"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	recs := []types.EntityRecord{
		{Kind: "SCOPE.Component", Name: "c", SourceFile: "client/config.js"},
		{Kind: "SCOPE.Component", Name: "o", SourceFile: "client/other.js"},
		{Kind: "SCOPE.Component", Name: "a", SourceFile: "client/api.js"},
		{Kind: "SCOPE.Component", Name: "l", SourceFile: "client/lonely.js"},
	}
	r := buildRepoSubstrateResolver(recs, root)
	if r == nil {
		t.Fatal("resolver is nil")
	}
	if got := r.resolve("client/api.js", "BASE").value; got != "/api" {
		t.Errorf("resolve from the importing file = %q, want /api", got)
	}
	if got := r.resolve("client/lonely.js", "BASE").value; got != "" {
		t.Errorf("resolve from a file with no binding and no import = %q, want empty", got)
	}
}
