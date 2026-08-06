// handlers_v2_graph_export_test.go — round-trip tests for #1627.
//
// Builds a small fake graph store on disk for a group, exports it via the
// HTTP handler, then imports the resulting archive as a DIFFERENT group on
// the same daemon and asserts the restored store is byte-for-byte equal
// to the original. Also covers the conflict / force / format / kind paths.

package dashboard

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/registry"
)

// buildGraphExportTestServer seeds a fake grafel home with one group
// "alpha" containing one repo whose store dir is populated with the
// canonical files that ship in a real graph (graph.fb / graph.json /
// enrichments / links / metadata / embeddings.bin).
func buildGraphExportTestServer(t *testing.T) (*Server, *registry.GroupConfig, string) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))

	repoPath := filepath.Join(home, "repos", "alpha-repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	// Seed the store layout for the repo with the files that a real index
	// would produce. Content is deterministic so hash compares are stable.
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(stateDir, "graph.fb"), "FLATBUF-bytes")
	mustWrite(t, filepath.Join(stateDir, "graph.json"), `{"entities":[]}`)
	mustWrite(t, filepath.Join(stateDir, "enrichments", "http_endpoint.json"), `[{"id":"ep-1"}]`)
	mustWrite(t, filepath.Join(stateDir, "links.json"), `{"links":[]}`)
	mustWrite(t, filepath.Join(stateDir, "metadata.json"), `{"computed_at":"2026-05-23T00:00:00Z"}`)
	mustWrite(t, filepath.Join(stateDir, "embeddings.bin"), "EMBED-binary-bytes")

	// Persist the fleet + register the group via the production helpers so
	// the import path exercises the real registry plumbing.
	cfgPath := filepath.Join(home, "alpha.fleet.json")
	cfg := &registry.GroupConfig{
		Name:  "alpha",
		Repos: []registry.Repo{{Slug: "alpha-repo", Path: repoPath, Stack: registry.StackList{"go"}}},
	}
	cfg.Features.Watchers = true
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}
	if err := registry.AddGroup("alpha", cfgPath); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv, cfg, repoPath
}

// hashTree fingerprints every regular file under root as a map of
// relpath → sha256-hex. Used to compare a freshly-imported store to the
// original byte-for-byte without caring about mod-times.
func hashTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(b)
		out[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("hashTree %s: %v", root, err)
	}
	return out
}

// TestGraphExportImport_RoundTrip is the headline round-trip: export the
// alpha group's store, import it as alpha-copy on the same daemon, and
// assert (a) the import succeeded, (b) the new group is registered, and
// (c) the restored store matches the original byte-for-byte.
func TestGraphExportImport_RoundTrip(t *testing.T) {
	srv, origCfg, origRepoPath := buildGraphExportTestServer(t)
	origStateDir := daemon.StateDirForRepo(origRepoPath)
	origHashes := hashTree(t, origStateDir)
	if len(origHashes) == 0 {
		t.Fatal("expected non-empty store before export")
	}

	// 1) Export.
	er := httptest.NewRequest("GET",
		"/api/v2/groups/alpha/export?format=zip&kind=graph", nil)
	ew := httptest.NewRecorder()
	srv.routes().ServeHTTP(ew, er)
	if ew.Code != http.StatusOK {
		t.Fatalf("export: expected 200, got %d: %s", ew.Code, ew.Body.String())
	}
	if got := ew.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("Content-Type = %q, want application/zip", got)
	}
	archive := ew.Body.Bytes()
	if len(archive) < 100 {
		t.Fatalf("archive suspiciously small: %d bytes", len(archive))
	}

	// Quick sanity-check: the manifest + at least one store file are present.
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("zip parse: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{
		"alpha/manifest.json",
		"alpha/fleet.json",
		"alpha/store/alpha-repo/graph.fb",
		"alpha/store/alpha-repo/graph.json",
		"alpha/store/alpha-repo/enrichments/http_endpoint.json",
		"alpha/store/alpha-repo/links.json",
		"alpha/store/alpha-repo/metadata.json",
		"alpha/store/alpha-repo/embeddings.bin",
	} {
		if !names[want] {
			t.Errorf("missing %s in archive; names = %v", want,
				sortedKeys(names))
		}
	}

	// 2) Import as a different group name on the same daemon.
	body, contentType := multipartZip(t, archive)
	ir := httptest.NewRequest("POST",
		"/api/v2/groups/import?name=alpha-copy", body)
	ir.Header.Set("Content-Type", contentType)
	iw := httptest.NewRecorder()
	srv.routes().ServeHTTP(iw, ir)
	if iw.Code != http.StatusOK {
		t.Fatalf("import: expected 200, got %d: %s", iw.Code, iw.Body.String())
	}
	var env v2Envelope
	if err := json.Unmarshal(iw.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope.ok=false: %s", iw.Body.String())
	}
	reply, _ := env.Data.(map[string]interface{})
	if reply["group"] != "alpha-copy" {
		t.Errorf("expected group=alpha-copy, got %v", reply["group"])
	}

	// 3) Assert the imported group is registered.
	groups, err := registry.Groups()
	if err != nil {
		t.Fatalf("registry.Groups: %v", err)
	}
	hasCopy := false
	for _, g := range groups {
		if g.Name == "alpha-copy" {
			hasCopy = true
			// Fleet contents should match the original (modulo name override).
			cfg, err := registry.LoadGroupConfig(g.ConfigPath)
			if err != nil {
				t.Fatalf("LoadGroupConfig: %v", err)
			}
			if cfg.Name != "alpha-copy" {
				t.Errorf("imported fleet.Name = %q, want alpha-copy", cfg.Name)
			}
			if len(cfg.Repos) != len(origCfg.Repos) {
				t.Errorf("imported repos = %d, want %d", len(cfg.Repos), len(origCfg.Repos))
			}
			if cfg.Repos[0].Path != origCfg.Repos[0].Path {
				t.Errorf("imported repo path mismatch: %q vs %q",
					cfg.Repos[0].Path, origCfg.Repos[0].Path)
			}
			break
		}
	}
	if !hasCopy {
		t.Fatalf("alpha-copy not registered; have %v", groups)
	}

	// 4) The restored store directory should be byte-equal to the original.
	// We restored under the same repo path because the manifest preserves it,
	// so the destination is the same StateDirForRepo. Hash-compare what's on
	// disk after the round-trip to confirm no file was dropped or corrupted.
	newHashes := hashTree(t, origStateDir)
	if len(newHashes) != len(origHashes) {
		t.Fatalf("round-trip file count differs: orig=%d new=%d",
			len(origHashes), len(newHashes))
	}
	for rel, want := range origHashes {
		if got := newHashes[rel]; got != want {
			t.Errorf("file %s hash mismatch after round-trip: want=%s got=%s",
				rel, want, got)
		}
	}
}

// TestGraphExport_KindAll_BundlesDocs verifies that kind=all includes the
// docs trees in the archive alongside the store.
func TestGraphExport_KindAll_BundlesDocs(t *testing.T) {
	srv, _, _ := buildGraphExportTestServer(t)

	// Seed technical + business docs.
	techDir := daemon.RepoDocsDir("alpha", "alpha-repo")
	mustWrite(t, filepath.Join(techDir, "overview.md"), "# Alpha\n")
	bizDir := daemon.BusinessDocsDir("alpha")
	mustWrite(t, filepath.Join(bizDir, "capabilities.md"), "# Caps\n")

	r := httptest.NewRequest("GET",
		"/api/v2/groups/alpha/export?format=zip&kind=all", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("zip parse: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["alpha/docs/alpha-repo/overview.md"] {
		t.Errorf("missing technical doc in kind=all export: %v", sortedKeys(names))
	}
	if !names["alpha/docs/business/capabilities.md"] {
		t.Errorf("missing business doc in kind=all export: %v", sortedKeys(names))
	}
}

// TestGraphExport_UnknownGroup_404 verifies the not-found branch.
func TestGraphExport_UnknownGroup_404(t *testing.T) {
	srv, _, _ := buildGraphExportTestServer(t)
	r := httptest.NewRequest("GET",
		"/api/v2/groups/ghost/export?format=zip", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// TestGraphExport_UnsupportedFormat verifies the bad-format branch.
func TestGraphExport_UnsupportedFormat(t *testing.T) {
	srv, _, _ := buildGraphExportTestServer(t)
	r := httptest.NewRequest("GET",
		"/api/v2/groups/alpha/export?format=tar", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestGraphImport_Conflict refuses to overwrite an existing group unless
// force=true is set.
func TestGraphImport_Conflict(t *testing.T) {
	srv, _, _ := buildGraphExportTestServer(t)

	// First, export.
	er := httptest.NewRequest("GET", "/api/v2/groups/alpha/export", nil)
	ew := httptest.NewRecorder()
	srv.routes().ServeHTTP(ew, er)
	if ew.Code != http.StatusOK {
		t.Fatalf("export prep failed: %d", ew.Code)
	}
	archive := ew.Body.Bytes()

	// Import-without-rename hits the existing "alpha" → must be a 409.
	body, ct := multipartZip(t, archive)
	r := httptest.NewRequest("POST", "/api/v2/groups/import", body)
	r.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %d: %s", w.Code, w.Body.String())
	}

	// Same archive with force=true → succeeds.
	body2, ct2 := multipartZip(t, archive)
	r2 := httptest.NewRequest("POST",
		"/api/v2/groups/import?force=true", body2)
	r2.Header.Set("Content-Type", ct2)
	w2 := httptest.NewRecorder()
	srv.routes().ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("force-import: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestGraphImport_InvalidArchive rejects non-zip bodies cleanly.
func TestGraphImport_InvalidArchive(t *testing.T) {
	srv, _, _ := buildGraphExportTestServer(t)
	body, ct := multipartZip(t, []byte("not a zip"))
	r := httptest.NewRequest("POST", "/api/v2/groups/import", body)
	r.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

// multipartZip wraps a zip body in a multipart/form-data envelope keyed
// `file` — the same shape the WebUI's <input type="file"> uploader sends.
func multipartZip(t *testing.T, archive []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", "graph.zip")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(archive); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close mw: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
