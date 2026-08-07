package daemon

// state_path_5822_test.go — #5822 defect D at the resolver layer.
//
// StateDirForRepo answers "which per-ref slot holds this repo's state?" by
// running a git capture. It has no way to say "I could not tell you": an
// un-runnable git yields Ref == "", which RefSafeEncode maps to the _unknown
// sentinel — a directory that never holds a graph. Callers that can distinguish
// the two need a resolver that reports the difference.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func initGitRepo5822(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runIn := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runIn("init")
	runIn("checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIn("add", ".")
	runIn("commit", "-m", "init")
	return dir
}

// TestStateDirForRepoResolved_UnrunnableGitIsUnknown pins the new signal.
func TestStateDirForRepoResolved_UnrunnableGitIsUnknown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub git script is POSIX-shell only")
	}
	t.Setenv(EnvRoot, t.TempDir())
	repo := initGitRepo5822(t)

	// A SIGKILLed git is the injected stand-in for a fired 2s deadline / a fork
	// EAGAIN: internal/gitmeta classifies all three as gitUnavailable.
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte("#!/bin/sh\nkill -9 $$\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	realPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir)

	dir, ok := StateDirForRepoResolved(repo)
	if ok {
		t.Fatalf("resolver claims it knows the ref while git cannot be run: %q — "+
			"that answer is the _unknown sentinel, where no graph ever lives (#5822 D)", dir)
	}

	// git schedulable again; HEAD never moved. Same fixture, real answer.
	t.Setenv("PATH", realPath)
	dir, ok = StateDirForRepoResolved(repo)
	if !ok {
		t.Fatal("resolver reports unknown for a healthy git — the fixture or the trust " +
			"signal is wrong, and the assertion above would then hold vacuously")
	}
	if want := StateDirForRepoRef(repo, "main"); dir != want {
		t.Fatalf("resolved %q, want %q", dir, want)
	}
	if strings.Contains(dir, "_unknown") {
		t.Fatalf("resolved to the sentinel: %q", dir)
	}
}
