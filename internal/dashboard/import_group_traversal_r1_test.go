package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/registry"
)

// #6186 R1 (found on round-2 review): handleV2GraphImport is a fifth writer
// that derives a group name from an uploaded archive (manifest.Group, or the
// `name=` query override) and never calls registry.ValidateGroupName
// anywhere on that path before using it to construct filesystem paths:
//
//	registry.ConfigPathFor(finalGroup)            (config file)
//	daemon.RepoDocsDir(finalGroup, repoSlug)       (per-repo docs)
//	daemon.BusinessDocsDir(finalGroup)             (group docs)
//
// All three filepath.Join the raw name, which collapses "..". The F6 fix
// enumerated AddGroup's four known callers and missed this one — the
// takeaway from round 2 is "validate the derivation, not the caller list",
// hence the write-side ConfigPathForNew/RepoDocsDirForNew/
// BusinessDocsDirForNew helpers added alongside this fix.
//
// This test imports an archive whose manifest.Group is a traversal string
// and asserts nothing was written outside the config/docs roots, using the
// exact escaped paths the reviewer's probe used.
func TestGraphImport_RejectsGroupNameEscapingConfigDir(t *testing.T) {
	srv, _, _ := buildGraphExportTestServer(t)

	// Export "alpha" (kind=graph is enough: the config-path escape does not
	// require docs).
	er := httptest.NewRequest("GET", "/api/v2/groups/alpha/export?format=zip&kind=graph", nil)
	ew := httptest.NewRecorder()
	srv.routes().ServeHTTP(ew, er)
	if ew.Code != http.StatusOK {
		t.Fatalf("export: expected 200, got %d: %s", ew.Code, ew.Body.String())
	}
	archive := ew.Body.Bytes()

	escapedConfigPath, err := registry.ConfigPathFor("../../../../tmp/pwn")
	if err != nil {
		t.Fatalf("ConfigPathFor: %v", err)
	}

	body, contentType := multipartZip(t, archive)
	ir := httptest.NewRequest("POST",
		"/api/v2/groups/import?name="+"..%2F..%2F..%2F..%2Ftmp%2Fpwn", body)
	ir.Header.Set("Content-Type", contentType)
	iw := httptest.NewRecorder()
	srv.routes().ServeHTTP(iw, ir)

	if iw.Code == http.StatusOK {
		t.Fatalf("import with a traversal group name succeeded (200): %s", iw.Body.String())
	}

	if _, statErr := os.Stat(escapedConfigPath); statErr == nil {
		t.Fatalf("SaveGroupConfig wrote outside the config directory at %s (#6186 R1)",
			escapedConfigPath)
	}
}

// TestGraphImport_RejectsGroupNameEscapingDocsDir is the docs-tree half of
// R1: kind=all restoration derives destDocsDir/destBiz from finalGroup via
// daemon.RepoDocsDir/BusinessDocsDir and MkdirAll's + unzips into them.
func TestGraphImport_RejectsGroupNameEscapingDocsDir(t *testing.T) {
	srv, _, _ := buildGraphExportTestServer(t)

	techDir := daemon.RepoDocsDir("alpha", "alpha-repo")
	mustWrite(t, filepath.Join(techDir, "overview.md"), "# Alpha\n")
	bizDir := daemon.BusinessDocsDir("alpha")
	mustWrite(t, filepath.Join(bizDir, "capabilities.md"), "# Caps\n")

	er := httptest.NewRequest("GET", "/api/v2/groups/alpha/export?format=zip&kind=all", nil)
	ew := httptest.NewRecorder()
	srv.routes().ServeHTTP(ew, er)
	if ew.Code != http.StatusOK {
		t.Fatalf("export: expected 200, got %d: %s", ew.Code, ew.Body.String())
	}
	archive := ew.Body.Bytes()

	escapedRepoDocs := daemon.RepoDocsDir("../../../../tmp/pwn", "alpha-repo")
	escapedBizDocs := daemon.BusinessDocsDir("../../../../tmp/pwn")

	body, contentType := multipartZip(t, archive)
	ir := httptest.NewRequest("POST",
		"/api/v2/groups/import?name="+"..%2F..%2F..%2F..%2Ftmp%2Fpwn", body)
	ir.Header.Set("Content-Type", contentType)
	iw := httptest.NewRecorder()
	srv.routes().ServeHTTP(iw, ir)

	if iw.Code == http.StatusOK {
		t.Fatalf("import with a traversal group name succeeded (200): %s", iw.Body.String())
	}

	if _, statErr := os.Stat(escapedRepoDocs); statErr == nil {
		t.Fatalf("extractZipPrefix wrote outside the docs directory at %s (#6186 R1)", escapedRepoDocs)
	}
	if _, statErr := os.Stat(escapedBizDocs); statErr == nil {
		t.Fatalf("extractZipPrefix wrote outside the docs directory at %s (#6186 R1)", escapedBizDocs)
	}
}

// TestGraphImport_RejectsControlCharacterGroupName pins that the import path
// gets the same widened validation (control chars, NUL, whitespace-only,
// over-length — #6186 F5) as the other four writers, not just the path-
// separator subset.
func TestGraphImport_RejectsControlCharacterGroupName(t *testing.T) {
	srv, _, _ := buildGraphExportTestServer(t)

	er := httptest.NewRequest("GET", "/api/v2/groups/alpha/export?format=zip&kind=graph", nil)
	ew := httptest.NewRecorder()
	srv.routes().ServeHTTP(ew, er)
	archive := ew.Body.Bytes()

	body, contentType := multipartZip(t, archive)
	ir := httptest.NewRequest("POST",
		"/api/v2/groups/import?name="+"g%0A%5BService%5D", body) // "g\n[Service]"
	ir.Header.Set("Content-Type", contentType)
	iw := httptest.NewRecorder()
	srv.routes().ServeHTTP(iw, ir)

	if iw.Code == http.StatusOK {
		var env v2Envelope
		_ = json.Unmarshal(iw.Body.Bytes(), &env)
		t.Fatalf("import with a control-character group name succeeded (200): %s", iw.Body.String())
	}
}
