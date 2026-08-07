// install_registermcp_6169_test.go covers `grafel install --register-mcp` at
// the CLI boundary.
//
// The flag exists so install.sh can finish a first-ever install (#6169) without
// running `grafel install` proper. That makes the short-circuit — placing the
// flag AHEAD of resolveToolSelection and of the whole RunCopy transaction — the
// safety property, exactly as it is for --refresh-state:
//
//   - resolveToolSelection consults stdinIsTTY() and opens an interactive
//     wizard, which would block a curl|bash install mid-run;
//   - RunCopy step 5 appends /.grafel/ to the .gitignore of the caller's cwd
//     and step 7 writes four git hooks there — a repository the installer was
//     never told about (#6162);
//   - RunCopy step 4 restarts the daemon on a 60s budget and its failure path
//     rolls step 3 back, restoring MCP host configs from snapshots (#6168).
//
// A regression that let --register-mcp fall through would put all of that back
// into the curl installer, so it is asserted here rather than assumed.
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
)

// runInstallRegisterMCP executes `install --register-mcp` (plus extraArgs)
// against a fresh command tree and returns its output and error.
func runInstallRegisterMCP(t *testing.T, extraArgs ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd := newInstallCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(append([]string{"--register-mcp"}, extraArgs...))
	err := cmd.Execute()
	return buf.String(), err
}

// TestInstallRegisterMCP_RegistersAndRecordsOnAVirginMachine is #6169 at the
// CLI boundary: no install.json, no .claude.json, and afterwards both exist.
func TestInstallRegisterMCP_RegistersAndRecordsOnAVirginMachine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := filepath.Join(home, ".claude.json")
	statePath := filepath.Join(home, ".grafel", "install.json")

	out, err := runInstallRegisterMCP(t, "--claude-config-dirs", cfg)
	if err != nil {
		t.Fatalf("install --register-mcp: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, cfg) {
		t.Errorf("output does not name the config it wrote: %q", out)
	}

	raw, rerr := os.ReadFile(cfg)
	if rerr != nil {
		t.Fatalf("config was not written: %v", rerr)
	}
	var doc map[string]any
	if uerr := json.Unmarshal(raw, &doc); uerr != nil {
		t.Fatalf("unmarshal config: %v", uerr)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers["grafel"]; !ok {
		t.Fatalf("no grafel MCP entry written: %s", raw)
	}

	st, serr := install.ReadState(statePath)
	if serr != nil || st == nil {
		t.Fatalf("install.json not recorded: st=%v err=%v", st, serr)
	}
	found := false
	for _, p := range st.MCP.RegisteredPaths {
		if p == cfg {
			found = true
		}
	}
	if !found {
		t.Errorf("MCP.RegisteredPaths = %v, want to contain %s", st.MCP.RegisteredPaths, cfg)
	}
}

// TestInstallRegisterMCP_DoesNotTouchTheCallersRepo is the reason the flag
// exists at all. The curl installer runs from whatever directory the user's
// shell was in; `grafel install` would append to that repo's .gitignore and
// install four git hooks in it.
func TestInstallRegisterMCP_DoesNotTouchTheCallersRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := t.TempDir()
	gitDir := filepath.Join(repo, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "hooks"), 0o755); err != nil {
		t.Fatalf("mkdir .git/hooks: %v", err)
	}
	gitignore := filepath.Join(repo, ".gitignore")
	const original = "node_modules/\n"
	if err := os.WriteFile(gitignore, []byte(original), 0o644); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	if out, rerr := runInstallRegisterMCP(t, "--claude-config-dirs", filepath.Join(home, ".claude.json")); rerr != nil {
		t.Fatalf("install --register-mcp: %v (out=%q)", rerr, out)
	}

	got, rerr := os.ReadFile(gitignore)
	if rerr != nil {
		t.Fatalf("read .gitignore: %v", rerr)
	}
	if string(got) != original {
		t.Errorf(".gitignore was modified:\n got: %q\nwant: %q", got, original)
	}
	entries, rerr := os.ReadDir(filepath.Join(gitDir, "hooks"))
	if rerr != nil {
		t.Fatalf("read hooks dir: %v", rerr)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("git hooks were installed into the caller's repo: %v", names)
	}
}

// TestInstallRegisterMCP_RejectsCombinedFlags: every other install flag asks
// for work --register-mcp explicitly does not do. Silently ignoring them would
// let a user believe they got a full install.
func TestInstallRegisterMCP_RejectsCombinedFlags(t *testing.T) {
	for _, flag := range []string{"--no-hooks", "--force", "--dev", "--tools=claude", "--refresh-state", "--foreground"} {
		t.Run(flag, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			out, err := runInstallRegisterMCP(t, flag)
			if err == nil {
				t.Fatalf("expected --register-mcp %s to be rejected; out=%q", flag, out)
			}
			if !strings.Contains(err.Error(), "--register-mcp") {
				t.Errorf("error should name the flag that limited the operation: %v", err)
			}
		})
	}
}

// TestInstallRegisterMCP_AcceptsClaudeConfigDirs: the one flag that IS
// meaningful here. If it were rejected the installer could not be tested
// hermetically and a multi-profile user could not target their configs.
func TestInstallRegisterMCP_AcceptsClaudeConfigDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a := filepath.Join(home, "a", ".claude.json")
	b := filepath.Join(home, "b", ".claude.json")

	out, err := runInstallRegisterMCP(t, "--claude-config-dirs", a+","+b)
	if err != nil {
		t.Fatalf("install --register-mcp --claude-config-dirs: %v (out=%q)", err, out)
	}
	for _, p := range []string{a, b} {
		if _, serr := os.Stat(p); serr != nil {
			t.Errorf("%s was not registered: %v", p, serr)
		}
	}
}

// TestInstallRefreshState_RejectsRegisterMCP: the reverse guard. --refresh-state
// records a checksum and registers nothing; a user who asked for both and got
// only the former would be told, in effect, that MCP was registered.
func TestInstallRefreshState_RejectsRegisterMCP(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	out, err := runInstallRefreshState(t, "--register-mcp")
	if err == nil {
		t.Fatalf("expected --refresh-state --register-mcp to be rejected; out=%q", out)
	}
	if !strings.Contains(err.Error(), "--register-mcp") {
		t.Errorf("error should name --register-mcp: %v", err)
	}
}
