package gitmeta_test

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/gitmeta"
)

// writeFakeSlowGit drops a `git` shim onto a fresh dir and prepends that dir to
// PATH so exec.LookPath("git") resolves to the shim. The shim sleeps far past
// any reasonable test deadline, simulating a wedged/U-state git child. It is
// skipped on Windows (no POSIX shell shim).
func writeFakeSlowGit(t *testing.T, sleepSeconds int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-git shell shim is POSIX-only")
	}
	dir := t.TempDir()
	shim := filepath.Join(dir, "git")
	script := "#!/bin/sh\nsleep " + itoa(sleepSeconds) + "\n"
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// TestRunGitBounded_TimesOut proves a wedged git child is killed within the
// configured deadline instead of hanging forever (issue #5286). Without the
// CommandContext deadline this call would block for `sleepSeconds`.
func TestRunGitBounded_TimesOut(t *testing.T) {
	writeFakeSlowGit(t, 60)              // git that sleeps 60s
	t.Setenv(gitmeta.EnvGitTimeout, "1") // 1s deadline

	start := time.Now()
	out, ok := gitmeta.RunGitBounded(t.TempDir(), "diff", "--name-only", "HEAD")
	elapsed := time.Since(start)

	if ok {
		t.Fatalf("expected ok=false on timeout, got ok=true out=%q", out)
	}
	if out != "" {
		t.Fatalf("expected empty output on timeout, got %q", out)
	}
	// Generous upper bound: deadline is 1s; allow scheduling + SIGKILL slack.
	if elapsed > 15*time.Second {
		t.Fatalf("bounded git did not abort near its deadline: took %s", elapsed)
	}
}

// TestRunGitBoundedC_TimesOut is the `git -C <dir>` variant.
func TestRunGitBoundedC_TimesOut(t *testing.T) {
	writeFakeSlowGit(t, 60)
	t.Setenv(gitmeta.EnvGitTimeout, "1")

	start := time.Now()
	out, ok := gitmeta.RunGitBoundedC(t.TempDir(), "rev-parse", "--short", "HEAD")
	elapsed := time.Since(start)

	if ok || out != nil {
		t.Fatalf("expected (nil,false) on timeout, got (%q,%v)", out, ok)
	}
	if elapsed > 15*time.Second {
		t.Fatalf("bounded git -C did not abort near its deadline: took %s", elapsed)
	}
}

// TestGitTimeout_Default_And_Override checks the env override and the
// clamp-to-default safety (a non-positive override must not re-introduce an
// unbounded call).
func TestGitTimeout_Default_And_Override(t *testing.T) {
	t.Setenv(gitmeta.EnvGitTimeout, "")
	if got := gitmeta.GitTimeout(); got != gitmeta.DefaultGitTimeout {
		t.Fatalf("unset: want %s, got %s", gitmeta.DefaultGitTimeout, got)
	}
	t.Setenv(gitmeta.EnvGitTimeout, "7")
	if got := gitmeta.GitTimeout(); got != 7*time.Second {
		t.Fatalf("override 7: want 7s, got %s", got)
	}
	// Non-positive / garbage must clamp to the default (never unbounded).
	for _, bad := range []string{"0", "-5", "abc"} {
		t.Setenv(gitmeta.EnvGitTimeout, bad)
		if got := gitmeta.GitTimeout(); got != gitmeta.DefaultGitTimeout {
			t.Fatalf("override %q: want clamp to %s, got %s", bad, gitmeta.DefaultGitTimeout, got)
		}
	}
}

// TestBoundedGit_DoesNotBlockConcurrentWork is the lock-discipline guarantee
// (issue #5286): while one goroutine is stuck in a (bounded) wedged git call,
// other goroutines must continue freely — the daemon's serve path is never
// gated on the indexer's git op. We model the serve path with a plain shared
// lock that a "serving" goroutine acquires repeatedly while a "slow index"
// goroutine runs a wedged-then-killed git command. The serving goroutine must
// make progress and the slow op must return within its deadline.
func TestBoundedGit_DoesNotBlockConcurrentWork(t *testing.T) {
	writeFakeSlowGit(t, 60)
	t.Setenv(gitmeta.EnvGitTimeout, "1")

	var serveMu sync.Mutex
	serveTicks := 0
	stop := make(chan struct{})

	// "Serve" goroutine: keeps acquiring/releasing a lock and counting ticks.
	// It must NOT be blocked by the in-flight slow git op (which holds no lock).
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			serveMu.Lock()
			serveTicks++
			serveMu.Unlock()
			time.Sleep(time.Millisecond)
		}
	}()

	// "Index" goroutine: runs the wedged-then-killed bounded git op. This is the
	// blocking call that, pre-fix, ran unbounded and wedged the worker.
	idxStart := time.Now()
	_, ok := gitmeta.RunGitBounded(t.TempDir(), "diff", "--name-only", "HEAD")
	idxElapsed := time.Since(idxStart)
	close(stop)
	wg.Wait()

	if ok {
		t.Fatalf("expected the wedged index git op to fail-closed (ok=false)")
	}
	if idxElapsed > 15*time.Second {
		t.Fatalf("index git op did not abort near deadline: %s", idxElapsed)
	}
	// The serve goroutine ran for ~the whole index op (≥1s). It must have made
	// real progress, proving it was never blocked by the slow index op.
	serveMu.Lock()
	ticks := serveTicks
	serveMu.Unlock()
	if ticks < 10 {
		t.Fatalf("serve path appears blocked by slow index op: only %d ticks in %s", ticks, idxElapsed)
	}
}
