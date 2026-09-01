//go:build unix

package dashboard

// secrets_unread_6534_unix_test.go — GET /api/quality/secrets/{group} must
// tell the caller that part of the tree could not be opened at all (#6534).
//
// #6483 covered the per-FILE skips. The walk-error arm was the one path it
// did not cover: an EACCES directory is N unread files, and the reply was an
// unqualified {"total_findings": 0, "skipped_files": []} — byte-identical to
// a genuinely clean repo. The tests drive the real handler over HTTP with a
// real chmod-000 subtree, because the whole defect lives in the plumbing
// between the scanner and the wire.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestQualitySecretsReportsUnreadDirsOverHTTP is the endpoint-level guard.
//
// Killing mutant: delete the `for _, u := range scan.Unread` loop (or the
// ScanComplete assignment) in handleQualitySecrets. The body goes back to
// total_findings:0 with nothing else changed, and — but for this test — the
// whole ./internal/dashboard package stays green.
func TestQualitySecretsReportsUnreadDirsOverHTTP(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores mode bits, so 0o000 is still readable")
	}
	repoPath := seedSecretsRegistry(t, "demo", "api", "package p\n")

	sub := filepath.Join(repoPath, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package p\n\nvar awsKey = \"AKIAIOSFODNN7REAL000\"\n"
	if err := os.WriteFile(filepath.Join(sub, "creds.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })
	if f, err := os.Open(sub); err == nil {
		f.Close()
		t.Skip("this filesystem ignores mode 0o000 on directories")
	}

	raw := getSecretsRaw(t, "demo", "")

	var reply SecretScanReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		t.Fatalf("decode: %v (body %s)", err, raw)
	}
	if reply.TotalFindings != 0 {
		t.Fatalf("precondition failed: the key under the chmod-000 dir was read; body %s", raw)
	}
	if reply.ScanComplete {
		t.Errorf("scan_complete = true for a repo with an unopenable subtree; "+
			"the caller cannot tell this from a clean repo: %s", raw)
	}
	if reply.UnreadDirsTotal != 1 {
		t.Errorf("unread_dirs_total = %d, want 1: %s", reply.UnreadDirsTotal, raw)
	}
	if len(reply.UnreadDirs) != 1 {
		t.Fatalf("unread_dirs = %+v, want 1 entry: %s", reply.UnreadDirs, raw)
	}
	if got := reply.UnreadDirs[0].Dir; got != "sub" {
		t.Errorf("unread_dirs[0].dir = %q, want %q (Rel, not the absolute path)", got, "sub")
	}
	if got := reply.UnreadDirs[0].Repo; got != "api" {
		t.Errorf("unread_dirs[0].repo = %q, want %q", got, "api")
	}
}

// TestQualitySecretsCleanRepoIsScanComplete is the permissiveness guard, and
// it also pins the ABSENCE semantics of the new list the way #6483 pinned
// skipped_files: an always-present [] says "asked, and the answer is none",
// where a missing key reads to a JS client as "this build does not report".
func TestQualitySecretsCleanRepoIsScanComplete(t *testing.T) {
	seedSecretsRegistry(t, "demo", "api", "package p\n")

	raw := getSecretsRaw(t, "demo", "")

	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	v, ok := body["unread_dirs"]
	if !ok {
		t.Fatalf("a clean repo's reply omits unread_dirs entirely: %s", raw)
	}
	if string(v) != "[]" {
		t.Errorf("unread_dirs = %s, want [] for a fully-read repo", v)
	}
	if got := string(body["scan_complete"]); got != "true" {
		t.Errorf("scan_complete = %s for a fully-read repo, want true: a verdict "+
			"that always says incomplete is a verdict nobody reads", got)
	}
	if got := string(body["unread_dirs_total"]); got != "0" {
		t.Errorf("unread_dirs_total = %s, want 0", got)
	}
}
