package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/secrets"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// newSecretsPayloadServer builds the smallest Server handleSecrets accepts:
// one loaded group, one repo with a non-nil Doc and a non-empty Path.
func newSecretsPayloadServer(t *testing.T) *Server {
	t.Helper()
	repoPath := t.TempDir()
	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoPath}}},
	}}
	st := NewState(reg)
	st.mu.Lock()
	st.groups["test"] = &LoadedGroup{
		Name: "test",
		Repos: map[string]*LoadedRepo{"r": {
			Repo: "r",
			Path: repoPath,
			Doc:  &graph.Document{Repo: "r"},
		}},
	}
	st.mu.Unlock()
	return &Server{State: st, Tel: NewTelemetry(0)}
}

func callSecretsTool(t *testing.T, s *Server) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Name = "grafel_secrets"
	req.Params.Arguments = map[string]any{"group": "test"}
	res, err := s.handleSecrets(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}
	txt, ok := mcpapi.AsTextContent(res.Content[0])
	if !ok {
		t.Fatalf("expected text content, got %T", res.Content[0])
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(txt.Text), &payload); err != nil {
		t.Fatalf("payload is not JSON: %v (%s)", err, txt.Text)
	}
	return payload
}

// TestSecretsToolPayloadCarriesSkippedFiles is the test that matters for
// #6483.
//
// The whole defect is that the skip information exists and never reaches the
// caller. A test at the internal/secrets boundary cannot observe that: it
// passes as soon as ScanPath returns the skip, whether or not any handler
// forwards it. In the daemon, handleSecrets' stderr IS the daemon log, which
// the MCP client never reads, so "scanned_repos: 1, total_findings: 0" is an
// unqualified clean bill of health for a repo where a file was never opened.
//
// Killing mutant: delete "skipped_files" from handleSecrets' jsonResult map.
// Every internal/secrets test stays green; only this one dies.
//
// The scan is stubbed through the scanSecrets seam rather than driven by a
// real FIFO so this runs on every GOOS — a FIFO cannot be created on
// windows-latest, and the payload contract is not a unix-specific concern.
func TestSecretsToolPayloadCarriesSkippedFiles(t *testing.T) {
	prev := scanSecrets
	scanSecrets = func(root string, _ int64) (secrets.ScanResult, error) {
		return secrets.ScanResult{Skipped: []secrets.Skip{{
			Path:   root + "/creds.go",
			Rel:    "creds.go",
			Reason: secrets.SkipNotRegular,
			Kind:   "named-pipe",
		}}}, nil
	}
	t.Cleanup(func() { scanSecrets = prev })

	payload := callSecretsTool(t, newSecretsPayloadServer(t))

	if got := payload["total_findings"]; got != float64(0) {
		t.Fatalf("total_findings = %v, want 0 (precondition: the stub finds nothing)", got)
	}
	raw, ok := payload["skipped_files"]
	if !ok {
		t.Fatalf("payload has no skipped_files key, so the caller is told this repo "+
			"is clean while creds.go was never read; keys present: %v", keysOf(payload))
	}
	list, ok := raw.([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("skipped_files = %#v, want exactly 1 entry", raw)
	}
	entry, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("skipped_files[0] = %#v, want an object", list[0])
	}
	if entry["file"] != "creds.go" {
		t.Errorf("skipped_files[0].file = %v, want creds.go", entry["file"])
	}
	if entry["reason"] != secrets.SkipNotRegular {
		t.Errorf("skipped_files[0].reason = %v, want %q", entry["reason"], secrets.SkipNotRegular)
	}
	if entry["kind"] != "named-pipe" {
		t.Errorf("skipped_files[0].kind = %v, want named-pipe", entry["kind"])
	}
	if entry["repo"] != "r" {
		t.Errorf("skipped_files[0].repo = %v, want r", entry["repo"])
	}
}

// TestSecretsToolPayloadOmitsSkippedFilesWhenNothingSkipped is the
// permissiveness guard: a scan that read everything must not manufacture a
// skip entry, or the field stops distinguishing the two answers it exists to
// distinguish.
func TestSecretsToolPayloadOmitsSkippedFilesWhenNothingSkipped(t *testing.T) {
	prev := scanSecrets
	scanSecrets = func(string, int64) (secrets.ScanResult, error) {
		return secrets.ScanResult{}, nil
	}
	t.Cleanup(func() { scanSecrets = prev })

	payload := callSecretsTool(t, newSecretsPayloadServer(t))

	if raw, ok := payload["skipped_files"]; ok {
		if list, isList := raw.([]any); !isList || len(list) != 0 {
			t.Errorf("skipped_files = %#v on a fully-read scan, want absent or empty", raw)
		}
	}
}

// TestSecretsToolCapsSkippedFiles pins the bound on the skip list.
//
// `files` is truncated at `limit` and flags it with `truncated`; skipped_files
// was unbounded. One repo full of >512 KB package-lock.json / .map files
// inflates an MCP payload — which is model context, not a scrollback — with
// no signal that anything was dropped. The cap follows the existing shape:
// truncate, and say so.
func TestSecretsToolCapsSkippedFiles(t *testing.T) {
	prev := scanSecrets
	scanSecrets = func(root string, _ int64) (secrets.ScanResult, error) {
		out := make([]secrets.Skip, 0, maxSkippedFilesReported+7)
		for i := 0; i < maxSkippedFilesReported+7; i++ {
			out = append(out, secrets.Skip{
				Rel:    fmt.Sprintf("vendor/bundle%d.map", i),
				Reason: secrets.SkipTooLarge,
			})
		}
		return secrets.ScanResult{Skipped: out}, nil
	}
	t.Cleanup(func() { scanSecrets = prev })

	payload := callSecretsTool(t, newSecretsPayloadServer(t))

	list, ok := payload["skipped_files"].([]any)
	if !ok {
		t.Fatalf("skipped_files = %#v, want a list", payload["skipped_files"])
	}
	if len(list) != maxSkippedFilesReported {
		t.Errorf("skipped_files has %d entries, want the cap %d: the list was "+
			"unbounded while files is truncated at limit",
			len(list), maxSkippedFilesReported)
	}
	if got := payload["skipped_files_total"]; got != float64(maxSkippedFilesReported+7) {
		t.Errorf("skipped_files_total = %v, want %d", got, maxSkippedFilesReported+7)
	}
	if got := payload["skipped_files_truncated"]; got != true {
		t.Errorf("skipped_files_truncated = %v, want true: a truncated list that "+
			"does not say so is worse than no list, because the count reads as complete", got)
	}
}

// TestSecretsToolSkippedFilesNotTruncatedBelowCap is the permissiveness half:
// a short list must not be flagged as truncated.
func TestSecretsToolSkippedFilesNotTruncatedBelowCap(t *testing.T) {
	prev := scanSecrets
	scanSecrets = func(string, int64) (secrets.ScanResult, error) {
		return secrets.ScanResult{Skipped: []secrets.Skip{
			{Rel: "a.map", Reason: secrets.SkipTooLarge},
		}}, nil
	}
	t.Cleanup(func() { scanSecrets = prev })

	payload := callSecretsTool(t, newSecretsPayloadServer(t))
	if got := payload["skipped_files_truncated"]; got != false {
		t.Errorf("skipped_files_truncated = %v for a 1-entry list, want false", got)
	}
	if got := payload["skipped_files_total"]; got != float64(1) {
		t.Errorf("skipped_files_total = %v, want 1", got)
	}
}
