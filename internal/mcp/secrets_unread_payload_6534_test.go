package mcp

import (
	"fmt"
	"testing"

	"github.com/cajasmota/grafel/internal/secrets"
)

// TestSecretsToolPayloadCarriesUnreadDirs is #6534 at the surface a user
// actually sees.
//
// A test at the internal/secrets boundary passes as soon as ScanPath counts
// the unopenable directory, whether or not any handler forwards the count. In
// the daemon this payload IS the answer the model reads, so an uncarried
// count leaves the MCP client with "scanned_repos: 1, total_findings: 0" —
// the unqualified clean bill of health #6534 exists to prevent.
//
// Killing mutant: delete "unread_dirs"/"unread_dirs_total"/"scan_complete"
// from handleSecrets' jsonResult map. Every internal/secrets test stays green.
//
// Driven through the scanSecrets seam so it runs on every GOOS: chmod 0o000
// has no Windows equivalent, and the payload contract is not unix-specific.
func TestSecretsToolPayloadCarriesUnreadDirs(t *testing.T) {
	prev := scanSecrets
	scanSecrets = func(root string, _ int64) (secrets.ScanResult, error) {
		return secrets.ScanResult{Unread: []secrets.UnreadDir{{
			Path: root + "/sub",
			Rel:  "sub",
		}}}, nil
	}
	t.Cleanup(func() { scanSecrets = prev })

	payload := callSecretsTool(t, newSecretsPayloadServer(t))

	if got := payload["total_findings"]; got != float64(0) {
		t.Fatalf("total_findings = %v, want 0 (precondition: the stub finds nothing)", got)
	}
	if got, ok := payload["scan_complete"]; !ok || got != false {
		t.Fatalf("scan_complete = %v (present=%v), want false: 'found nothing' and "+
			"'looked at nothing' must not render identically; keys present: %v",
			got, ok, keysOf(payload))
	}
	if got := payload["unread_dirs_total"]; got != float64(1) {
		t.Fatalf("unread_dirs_total = %v, want 1", got)
	}
	list, ok := payload["unread_dirs"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("unread_dirs = %#v, want exactly 1 entry", payload["unread_dirs"])
	}
	entry, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("unread_dirs[0] = %#v, want an object", list[0])
	}
	if entry["dir"] != "sub" {
		t.Errorf("unread_dirs[0].dir = %v, want sub (Rel, not the absolute path)", entry["dir"])
	}
	if entry["repo"] != "r" {
		t.Errorf("unread_dirs[0].repo = %v, want r", entry["repo"])
	}
}

// TestSecretsToolReportsCompleteWhenNothingUnread is the permissiveness half.
//
// It kills the mutant that hard-codes scan_complete to false, or synthesises
// an unread entry on a fully-read scan. A verdict that says "incomplete" for
// every repo is a verdict nobody reads, which is the failure mode the owner
// rejected explicitly (warn-but-still-wrong).
func TestSecretsToolReportsCompleteWhenNothingUnread(t *testing.T) {
	prev := scanSecrets
	scanSecrets = func(string, int64) (secrets.ScanResult, error) {
		return secrets.ScanResult{}, nil
	}
	t.Cleanup(func() { scanSecrets = prev })

	payload := callSecretsTool(t, newSecretsPayloadServer(t))

	if got := payload["scan_complete"]; got != true {
		t.Errorf("scan_complete = %v on a fully-read scan, want true", got)
	}
	if got := payload["unread_dirs_total"]; got != float64(0) {
		t.Errorf("unread_dirs_total = %v on a fully-read scan, want 0", got)
	}
	if list, ok := payload["unread_dirs"].([]any); !ok || len(list) != 0 {
		t.Errorf("unread_dirs = %#v on a fully-read scan, want an empty list",
			payload["unread_dirs"])
	}
}

// TestSecretsToolCapsUnreadDirs pins the bound on the new list, and pins that
// the COUNT survives truncation.
//
// The count is the load-bearing half of #6534: a truncated list that reports
// its own length as the total would understate the gap, which is the same
// class of lie as reporting clean.
func TestSecretsToolCapsUnreadDirs(t *testing.T) {
	const n = maxSkippedFilesReported + 5
	prev := scanSecrets
	scanSecrets = func(string, int64) (secrets.ScanResult, error) {
		out := make([]secrets.UnreadDir, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, secrets.UnreadDir{Rel: fmt.Sprintf("locked%d", i)})
		}
		return secrets.ScanResult{Unread: out}, nil
	}
	t.Cleanup(func() { scanSecrets = prev })

	payload := callSecretsTool(t, newSecretsPayloadServer(t))

	list, ok := payload["unread_dirs"].([]any)
	if !ok {
		t.Fatalf("unread_dirs = %#v, want a list", payload["unread_dirs"])
	}
	if len(list) != maxSkippedFilesReported {
		t.Errorf("unread_dirs has %d entries, want the cap %d", len(list), maxSkippedFilesReported)
	}
	if got := payload["unread_dirs_total"]; got != float64(n) {
		t.Errorf("unread_dirs_total = %v, want %d: the count must survive truncation", got, n)
	}
	if got := payload["unread_dirs_truncated"]; got != true {
		t.Errorf("unread_dirs_truncated = %v, want true", got)
	}
	if got := payload["scan_complete"]; got != false {
		t.Errorf("scan_complete = %v, want false", got)
	}
}
