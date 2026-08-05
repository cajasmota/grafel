// install_refreshstate_test.go covers `grafel install --refresh-state` at the
// CLI boundary.
//
// Review finding: the first pass shipped this flag with ZERO coverage in
// internal/cli — the flag, runRefreshState, its three output branches and,
// critically, the short-circuit that places it ahead of the tool-selection
// wizard and the whole install transaction were asserted only by a comment.
// That short-circuit IS the safety property: `grafel install` proper appends
// /.grafel/ to the .gitignore of the caller's cwd and installs four git hooks
// there (RunCopy steps 5 and 7), which is precisely why install.sh may not call
// it. A regression that let --refresh-state fall through would put those
// mutations back into the curl installer.
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
)

// refreshStateEnv points HOME at a temp dir and returns its install.json path.
func refreshStateEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return filepath.Join(home, ".grafel", "install.json")
}

// runInstallRefreshState executes `install --refresh-state` (plus extraArgs)
// against a fresh command tree and returns its output and error.
func runInstallRefreshState(t *testing.T, extraArgs ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd := newInstallCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(append([]string{"--refresh-state"}, extraArgs...))
	err := cmd.Execute()
	return buf.String(), err
}

// TestInstallRefreshState_NoStateReportsAndDoesNotFabricate: an absent
// install.json must stay absent — quick-doctor is already silent in that state,
// and a synthetic record with no skills/MCP manifest would make `grafel doctor`
// report drift that does not exist.
func TestInstallRefreshState_NoStateReportsAndDoesNotFabricate(t *testing.T) {
	statePath := refreshStateEnv(t)

	out, err := runInstallRefreshState(t)
	if err != nil {
		t.Fatalf("install --refresh-state: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "no ~/.grafel/install.json to refresh") {
		t.Errorf("expected the no-state branch, got: %q", out)
	}
	if _, statErr := os.Stat(statePath); !os.IsNotExist(statErr) {
		t.Errorf("install.json must not be created out of nothing (stat err = %v)", statErr)
	}
}

// TestInstallRefreshState_RewritesAStaleRecord exercises the branch the curl
// installer depends on.
func TestInstallRefreshState_RewritesAStaleRecord(t *testing.T) {
	statePath := refreshStateEnv(t)

	self, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable unavailable: %v", err)
	}
	st := install.NewState(install.ModeCopy)
	st.CLI = install.CLIRecord{Path: self, SHA256: strings.Repeat("0", 64)}
	if werr := install.WriteState(statePath, st); werr != nil {
		t.Fatalf("seed state: %v", werr)
	}

	out, err := runInstallRefreshState(t)
	if err != nil {
		t.Fatalf("install --refresh-state: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "install state refreshed") {
		t.Errorf("expected the refreshed branch, got: %q", out)
	}

	got, rerr := install.ReadState(statePath)
	if rerr != nil || got == nil {
		t.Fatalf("ReadState: %v", rerr)
	}
	if got.CLI.Path != self {
		t.Errorf("CLI.Path = %q, want %q", got.CLI.Path, self)
	}
	if got.CLI.SHA256 == strings.Repeat("0", 64) || got.CLI.SHA256 == "" {
		t.Errorf("CLI.SHA256 was not refreshed: %q", got.CLI.SHA256)
	}
}

// TestInstallRefreshState_SecondRunReportsAlreadyCurrent: the installer calls
// this unconditionally, so a repeat run must be a quiet no-op, not an error.
func TestInstallRefreshState_SecondRunReportsAlreadyCurrent(t *testing.T) {
	statePath := refreshStateEnv(t)

	self, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable unavailable: %v", err)
	}
	st := install.NewState(install.ModeCopy)
	st.CLI = install.CLIRecord{Path: self, SHA256: strings.Repeat("0", 64)}
	if werr := install.WriteState(statePath, st); werr != nil {
		t.Fatalf("seed state: %v", werr)
	}

	if out, rerr := runInstallRefreshState(t); rerr != nil {
		t.Fatalf("first run: %v (out=%q)", rerr, out)
	}
	out, err := runInstallRefreshState(t)
	if err != nil {
		t.Fatalf("second run must not error: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "already current") {
		t.Errorf("expected the already-current branch on the second run, got: %q", out)
	}
}

// TestInstallRefreshState_DoesNotTouchTheCallersRepo is the safety property.
// Run from inside a git repo — the shape of `curl … | bash` in a user's project
// — the refresh must leave that repo completely alone: no /.grafel/ appended to
// .gitignore (RunCopy step 5) and no git hooks written (step 7).
func TestInstallRefreshState_DoesNotTouchTheCallersRepo(t *testing.T) {
	statePath := refreshStateEnv(t)

	self, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable unavailable: %v", err)
	}
	st := install.NewState(install.ModeCopy)
	st.CLI = install.CLIRecord{Path: self, SHA256: strings.Repeat("0", 64)}
	if werr := install.WriteState(statePath, st); werr != nil {
		t.Fatalf("seed state: %v", werr)
	}

	// A minimal but convincing git repo as the working directory.
	repo := t.TempDir()
	hooksDir := filepath.Join(repo, ".git", "hooks")
	if mkErr := os.MkdirAll(hooksDir, 0o755); mkErr != nil {
		t.Fatalf("mkdir .git/hooks: %v", mkErr)
	}
	gitignore := filepath.Join(repo, ".gitignore")
	const originalIgnore = "node_modules/\n"
	if werr := os.WriteFile(gitignore, []byte(originalIgnore), 0o644); werr != nil {
		t.Fatalf("write .gitignore: %v", werr)
	}

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if cerr := os.Chdir(repo); cerr != nil {
		t.Fatalf("chdir: %v", cerr)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	// Deliberately NOT fatal on error: the side-effect assertions below are the
	// point of this test and must run either way. A mutant that removes the
	// short-circuit drops into the real install transaction, which may fail for
	// an unrelated environment reason (on macOS the temp HOME blows the 104-char
	// unix socket limit at step 4) BEFORE reaching steps 5 and 7 — so aborting
	// here would let the .gitignore/hooks checks be skipped exactly when they
	// matter most.
	if out, rerr := runInstallRefreshState(t); rerr != nil {
		t.Errorf("install --refresh-state must succeed: %v (out=%q)", rerr, out)
	}

	after, rerr := os.ReadFile(gitignore)
	if rerr != nil {
		t.Fatalf("read .gitignore: %v", rerr)
	}
	if string(after) != originalIgnore {
		t.Errorf("--refresh-state must not touch the caller's .gitignore; got:\n%s", after)
	}

	entries, rerr := os.ReadDir(hooksDir)
	if rerr != nil {
		t.Fatalf("read .git/hooks: %v", rerr)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("--refresh-state must not install git hooks in the caller's repo; found: %v", names)
	}
}

// TestInstallRefreshState_RejectsCombinedFlags: silently ignoring the rest is
// its own trap — every other install flag asks for work --refresh-state does
// not do, so a user combining them would get a state rewrite and believe they
// got an install.
func TestInstallRefreshState_RejectsCombinedFlags(t *testing.T) {
	for _, arg := range []string{"--dev", "--force", "--tools=claude", "--no-hooks", "--copy=false"} {
		t.Run(arg, func(t *testing.T) {
			refreshStateEnv(t)
			out, err := runInstallRefreshState(t, arg)
			if err == nil {
				t.Fatalf("--refresh-state %s must be rejected, got success: %q", arg, out)
			}
			if !strings.Contains(err.Error(), "--refresh-state") {
				t.Errorf("error must explain the conflict, got: %v", err)
			}
		})
	}
}

// TestInstallRefreshState_PlainInstallFlagsStillWork: the conflict guard must
// key on flags the user actually typed, not on defaults — `--copy` defaults to
// true and must not be treated as a conflict.
func TestInstallRefreshState_PlainInstallFlagsStillWork(t *testing.T) {
	refreshStateEnv(t)
	out, err := runInstallRefreshState(t)
	if err != nil {
		t.Fatalf("bare --refresh-state must not trip the conflict guard (--copy defaults to true): %v (out=%q)", err, out)
	}
}
