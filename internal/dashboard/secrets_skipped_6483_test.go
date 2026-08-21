package dashboard

// secrets_skipped_6483_test.go — GET /api/quality/secrets/{group} must tell
// the caller which files it did NOT read (#6483).
//
// This endpoint is the dangerous one, and the reason is in its own query
// string: max_size is caller-supplied. ?max_size=1 makes the scanner skip
// every file in the repo, and before #6483 the reply to that request was an
// unqualified {"total_findings": 0} — indistinguishable, on the wire, from a
// genuinely clean repo.
//
// The tests below drive the real handler over HTTP with a real temp repo
// containing a real key, rather than stubbing the scanner: the whole defect
// lives in the handler's own plumbing, so a seam placed under it would move
// the assertion off the code that was wrong.

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/registry"
)

// seedSecretsRegistry wires a temp GRAFEL_HOME registry with one group and
// one repo, and writes body into <repo>/creds.go. Returns the repo path.
func seedSecretsRegistry(t *testing.T, group, repoSlug, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	cfgHome := filepath.Join(home, "config")
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	cfgDir := filepath.Join(cfgHome, "grafel")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	repoPath := filepath.Join(home, "repos", repoSlug)
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "creds.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(cfgDir, group+".fleet.json")
	cfg := registry.GroupConfig{Name: group, Repos: []registry.Repo{{Slug: repoSlug, Path: repoPath}}}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(cfgPath, cfgData, 0o644); err != nil {
		t.Fatal(err)
	}
	reg := registry.Registry{Version: 1, Groups: []registry.GroupRef{{Name: group, ConfigPath: cfgPath}}}
	regData, _ := json.MarshalIndent(reg, "", "  ")
	if err := os.WriteFile(filepath.Join(home, "registry.json"), regData, 0o644); err != nil {
		t.Fatal(err)
	}
	return repoPath
}

// getSecretsRaw drives the endpoint and returns the raw JSON body, so the
// tests can assert on KEY PRESENCE and not only on decoded Go values — a nil
// slice and an absent key decode identically into SecretScanReply.
func getSecretsRaw(t *testing.T, group, query string) []byte {
	t.Helper()
	url, done := newTestServer(t, newFakeStore(), DefaultConfig())
	defer done()
	resp, err := http.Get(url + "/api/quality/secrets/" + group + query)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestQualitySecretsReportsSkippedFilesOverHTTP is the endpoint-level guard.
//
// Killing mutant: delete the `for _, sk := range scan.Skipped` loop in
// handleQualitySecrets. skipped_files vanishes from the body while
// total_findings stays 0, and — before this test existed — the whole
// ./internal/dashboard package stayed green. The layering argument #6483's
// commit message makes about internal/secrets applies verbatim here: an
// internal/secrets test cannot observe a handler that never copies the field
// onto the wire.
func TestQualitySecretsReportsSkippedFilesOverHTTP(t *testing.T) {
	seedSecretsRegistry(t, "demo", "api",
		"package p\n\nvar awsKey = \"AKIAIOSFODNN7REAL000\"\n")

	// max_size=1: every file in the repo is over the cap, so the scanner
	// opens nothing. This is a request a dashboard client can make today.
	raw := getSecretsRaw(t, "demo", "?max_size=1")

	var reply SecretScanReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		t.Fatalf("decode: %v (body %s)", err, raw)
	}
	if reply.TotalFindings != 0 {
		t.Fatalf("max_size=1 should have found nothing; got %d", reply.TotalFindings)
	}
	if len(reply.SkippedFiles) != 1 {
		t.Fatalf("the reply says total_findings:0 for a repo whose only file "+
			"holds a live AWS key and was never opened, and names no skips: %s", raw)
	}
	sk := reply.SkippedFiles[0]
	if sk.Repo != "api" {
		t.Errorf("repo = %q, want %q", sk.Repo, "api")
	}
	if sk.File != "creds.go" {
		t.Errorf("file = %q, want %q (Rel, not the absolute path)", sk.File, "creds.go")
	}
	if sk.Reason != "too_large" {
		t.Errorf("reason = %q, want %q", sk.Reason, "too_large")
	}
}

// TestQualitySecretsAlwaysEmitsSkippedFilesKey pins the field's ABSENCE
// semantics on a security surface.
//
// With `omitempty` on a nil slice the key disappears entirely from a clean
// repo's reply, and a JS client writing the natural `if (r.skipped_files
// ?.length)` cannot distinguish "the scanner read everything" from "this
// build of the server does not report skips" — reintroducing exactly the
// ambiguity the field was added to remove. An always-present `[]` says
// "asked, and the answer is none".
func TestQualitySecretsAlwaysEmitsSkippedFilesKey(t *testing.T) {
	seedSecretsRegistry(t, "demo", "api", "package p\n")

	raw := getSecretsRaw(t, "demo", "")

	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	v, ok := body["skipped_files"]
	if !ok {
		t.Fatalf("a clean repo's reply omits skipped_files entirely; "+
			"\"nothing was skipped\" and \"skips are not reported\" must not "+
			"look the same to a client: %s", raw)
	}
	if string(v) != "[]" {
		t.Errorf("skipped_files = %s, want [] for a repo with nothing skipped", v)
	}
}
