package engine

import (
	"fmt"
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
	res := FoldConsumerHTTPBaseURLs(recs, root, nil)
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
		res := FoldConsumerHTTPBaseURLs(recs, root, nil)
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
		res := FoldConsumerHTTPBaseURLs(recs, root, nil)
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
		res := FoldConsumerHTTPBaseURLs(recs, "", nil)
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
		res := FoldConsumerHTTPBaseURLs(recs, root, nil)
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
	r := newRepoSubstrateResolver(root, recs, nil)
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

// #6450 review, BLOCKING 1 — the incremental path must not un-fold what a
// full index got right.
//
// On an incremental run `merged` holds ONLY the re-extracted files. When an
// unrelated edit touches the CALLER file but not the declaring module, the
// declaring module appears only in the carried-forward prior-graph
// entities. Deriving the symbol table's file set from `merged` alone made
// the fold stop firing and reverted every previously-resolved FETCHES to
// UNRESOLVED_FETCH — a REGRESSION of a correct graph, not merely a missed
// improvement.
func TestFoldConsumerHTTPBaseURLs_IncrementalKeepsFolding_6450(t *testing.T) {
	root := writeFoldFixture(t)

	// Exactly what an incremental run hands buildDocument: only client/api.js
	// was re-extracted. client/config.js is UNCHANGED, so its entities live
	// in the carry-forward slice.
	newMerged := func() []types.EntityRecord {
		return []types.EntityRecord{
			callRecord("http:GET:/{BASE}/things", "/{BASE}/things", "dynamic_baseurl"),
		}
	}
	carried := []types.EntityRecord{
		{Kind: "SCOPE.Component", Name: "config", SourceFile: "client/config.js"},
	}

	t.Run("with carry-forward it still folds", func(t *testing.T) {
		recs := newMerged()
		res := FoldConsumerHTTPBaseURLs(recs, root, carried)
		if res.Candidates != 1 || res.Folded != 1 {
			t.Fatalf("candidates=%d folded=%d, want 1/1 — an incremental run must "+
				"see the declaring module through the carried-forward entities",
				res.Candidates, res.Folded)
		}
		if got := recs[0].Properties["path"]; got != "/api/things" {
			t.Errorf("path = %q, want /api/things", got)
		}
		// Identity contract holds on the incremental path too.
		if recs[0].Name != "http:GET:/{BASE}/things" {
			t.Errorf("Name moved to %q", recs[0].Name)
		}
	})

	t.Run("without carry-forward the declaring module is invisible", func(t *testing.T) {
		// This is the defect, pinned so the parameter cannot be quietly
		// dropped again: same inputs, no carry-forward, no fold.
		recs := newMerged()
		res := FoldConsumerHTTPBaseURLs(recs, root, nil)
		if res.Folded != 0 {
			t.Fatalf("folded=%d, want 0 — this subtest documents WHY carriedForward "+
				"is required; if the fold now succeeds without it the mechanism "+
				"changed and the comment above is stale", res.Folded)
		}
	})
}

// #6450 review, BLOCKING 2b — sniffing must be lazy and bounded.
//
// The first cut read and regex-sniffed EVERY substrate-language file in the
// repo as soon as one candidate existed: +12.3s on a 7,916-file repo for
// candidates=1 folded=0. A file must be read only when a resolution chain
// actually reaches it.
func TestFoldConsumerHTTPBaseURLs_SniffIsLazy_6450(t *testing.T) {
	root := writeFoldFixture(t)
	recs := []types.EntityRecord{
		{Kind: "SCOPE.Component", Name: "config", SourceFile: "client/config.js"},
		callRecord("http:GET:/{BASE}/things", "/{BASE}/things", "dynamic_baseurl"),
	}
	// 300 decoy modules that no candidate imports. They must be INDEXED (the
	// lookup is path-only, free) but never READ.
	const decoys = 300
	for i := 0; i < decoys; i++ {
		name := fmt.Sprintf("client/decoy%03d.js", i)
		body := fmt.Sprintf("export const DECOY%03d = '/never/read';\n", i)
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		recs = append(recs, types.EntityRecord{Kind: "SCOPE.Component", Name: name, SourceFile: name})
	}

	res := FoldConsumerHTTPBaseURLs(recs, root, nil)
	if res.Folded != 1 {
		t.Fatalf("folded=%d, want 1", res.Folded)
	}
	// api.js (the caller) + config.js (one import hop). Nothing else.
	if res.FilesSniffed != 2 {
		t.Errorf("FilesSniffed=%d, want 2 — only the caller file and the one "+
			"module it imports may be read; %d decoys were in scope", res.FilesSniffed, decoys)
	}
	if res.FilesIndexed <= decoys {
		t.Errorf("FilesIndexed=%d, want > %d — the lookup is path-only and must "+
			"still cover every file", res.FilesIndexed, decoys)
	}
	if res.ReadCapHit {
		t.Error("ReadCapHit set on a run that read 2 files")
	}
}

// The read cap is a hard bound, not a suggestion.
func TestFoldConsumerHTTPBaseURLs_ReadCapIsEnforced_6450(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "client"), 0o755); err != nil {
		t.Fatal(err)
	}
	// One candidate per file, each in its OWN caller file, so every candidate
	// forces a distinct read. More of them than the cap allows.
	n := substrateMaxFileReads + 40
	var recs []types.EntityRecord
	for i := 0; i < n; i++ {
		file := fmt.Sprintf("client/caller%04d.js", i)
		body := "const X = '/api';\nfetch(`${X}/things`);\n"
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(file)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		r := callRecord("http:GET:/{X}/things", "/{X}/things", "dynamic_baseurl")
		r.SourceFile = file
		r.Properties["caller_file"] = file
		recs = append(recs, r)
	}
	res := FoldConsumerHTTPBaseURLs(recs, root, nil)
	if res.Candidates != n {
		t.Fatalf("candidates=%d, want %d", res.Candidates, n)
	}
	if res.FilesSniffed > substrateMaxFileReads {
		t.Errorf("FilesSniffed=%d exceeds the cap %d", res.FilesSniffed, substrateMaxFileReads)
	}
	if !res.ReadCapHit {
		t.Errorf("ReadCapHit=false after %d candidates against a cap of %d — the "+
			"cap must be observable, not silent", n, substrateMaxFileReads)
	}
	if res.Folded == 0 || res.Folded > substrateMaxFileReads {
		t.Errorf("folded=%d — the cap should truncate the work, not abolish it", res.Folded)
	}
}
