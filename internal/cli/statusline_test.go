package cli

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestStatuslineGuide_ContainsCoreFacts asserts the full guide (no flags)
// documents the status-file path scheme, the key field names a statusline
// author needs, and all three example snippets plus the wiring section.
func TestStatuslineGuide_ContainsCoreFacts(t *testing.T) {
	guide := statuslineGuideText()

	mustContain := []string{
		"status/",                     // path scheme: $GRAFEL_HOME/status/<hash>.json
		"graph_fb_mtime",              // freshness signal
		"indexing",                    // indexing bool
		"heartbeat_at",                // engine liveness
		"grafel 3m ago",               // minimal snippet (3a)
		"⟳ indexing",                  // icon snippet (3b)
		"not indexed",                 // detailed snippet (3c) mentions caveat / not-indexed state
		"Wiring it in",                // wiring section header
		"statusLine.command",          // Claude Code wiring mention
		"grafel statusline --snippet", // self-reference
	}
	for _, s := range mustContain {
		if !strings.Contains(guide, s) {
			t.Errorf("guide missing expected substring %q", s)
		}
	}
}

// TestStatuslineSnippet_IconOnly asserts --snippet emits ONLY the icon-based
// bash segment: no prose section headers, but the load-bearing bits of the
// icon logic (graph_fb_mtime read + the sha256/status/ path construction).
func TestStatuslineSnippet_IconOnly(t *testing.T) {
	snippet := statuslineIconSnippet()

	mustContain := []string{
		"graph_fb_mtime",
		"status/",
		"shasum -a 256",
	}
	for _, s := range mustContain {
		if !strings.Contains(snippet, s) {
			t.Errorf("snippet missing expected substring %q", s)
		}
	}

	mustNotContain := []string{
		"Wiring it in",
		"Available data",
		"Example segments",
	}
	for _, s := range mustNotContain {
		if strings.Contains(snippet, s) {
			t.Errorf("snippet unexpectedly contains prose header %q", s)
		}
	}
}

// TestStatuslineSnippet_ValidBash pipes the emitted snippet through `bash -n`
// (syntax check only, no execution) to guarantee it's valid, standalone
// bash a user can paste as-is.
func TestStatuslineSnippet_ValidBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available in PATH")
	}
	snippet := statuslineIconSnippet()

	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(snippet)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("bash -n reported a syntax error: %v\nstderr:\n%s\nsnippet:\n%s", err, stderr.String(), snippet)
	}
}

// TestStatuslineCmd_NoArgs_PrintsFullGuide exercises the cobra wiring: no
// args prints the full guide and exits 0.
func TestStatuslineCmd_NoArgs_PrintsFullGuide(t *testing.T) {
	var out bytes.Buffer
	cmd := newStatuslineCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("grafel statusline returned error: %v", err)
	}
	if !strings.Contains(out.String(), "Wiring it in") {
		t.Errorf("expected full guide in output, got:\n%s", out.String())
	}
}

// TestStatuslineCmd_SnippetFlag exercises the cobra wiring for --snippet.
func TestStatuslineCmd_SnippetFlag(t *testing.T) {
	var out bytes.Buffer
	cmd := newStatuslineCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--snippet"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("grafel statusline --snippet returned error: %v", err)
	}
	if strings.Contains(out.String(), "Wiring it in") {
		t.Errorf("--snippet output should not contain prose sections, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "graph_fb_mtime") {
		t.Errorf("--snippet output missing graph_fb_mtime, got:\n%s", out.String())
	}
}
