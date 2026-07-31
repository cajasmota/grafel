package daemon

import (
	"os/exec"
	"sync"
	"testing"
)

// TestEngineChildCommandSeam_ConcurrentOverrideAndRead pins the fix for #6056.
//
// The hazard is NOT the test seam itself — it is that the supervisor's respawn
// loop (engineSupervisor.run) reads the seam REPEATEDLY from a live goroutine,
// while a test's deferred restore() writes it during cleanup. Contrast
// listenFn, which is read exactly once at startup and is therefore clean.
//
// This test reproduces that exact shape without a real supervisor: a live
// reader goroutine resolves the seam in a loop while the test installs and
// restores overrides. It has two independent failure modes, so it cannot
// silently stop protecting:
//
//   - Under -race (the CI tag / workflow_dispatch path, and the local
//     backstop): if the seam ever goes back to a plain package var, the
//     write/read pair is unsynchronised and the detector fails this test.
//   - Without -race: the restore-semantics assertions below still go RED if
//     Set/restore stops being LIFO-correct or stops reverting to the
//     production default.
func TestEngineChildCommandSeam_ConcurrentOverrideAndRead(t *testing.T) {
	// Resolve the temp root once, up front: calling t.TempDir() inside the
	// loops would take testing's own mutex on every iteration, creating
	// happens-before edges that hide the very race this test exists to catch.
	root := t.TempDir()

	stop := make(chan struct{})
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
			// Resolve the seam the way engineSupervisor.run does. We only
			// resolve it; we never Start() the returned command.
			if cmd := engineChildCommand("/nonexistent/selfexe", root); cmd == nil {
				panic("engine child command resolved to nil")
			}
		}
	}()

	var mu sync.Mutex
	invoked := 0
	mkA := func(selfExe, r string) *exec.Cmd {
		mu.Lock()
		invoked++
		mu.Unlock()
		return exec.Command("true")
	}
	mkB := func(selfExe, r string) *exec.Cmd {
		mu.Lock()
		invoked++
		mu.Unlock()
		return exec.Command("true")
	}

	for i := 0; i < 2000; i++ {
		restoreA := SetEngineChildCommandForTest(mkA)
		restoreB := SetEngineChildCommandForTest(mkB)
		// Resolve under the override too, so the seam is exercised in both
		// directions rather than only written.
		_ = engineChildCommand("/nonexistent/selfexe", root)
		restoreB()
		restoreA()
	}

	close(stop)
	wg.Wait()

	// Restore semantics (these fail without -race too): after every override
	// has been restored, the seam must resolve back to the production default,
	// i.e. `<selfExe> engine --foreground`.
	cmd := engineChildCommand("/nonexistent/selfexe", root)
	if len(cmd.Args) < 3 || cmd.Args[1] != "engine" || cmd.Args[2] != "--foreground" {
		t.Fatalf("after restore, seam did not resolve to defaultEngineChildCommand: args=%v", cmd.Args)
	}

	// Guard against a vacuous fixture: if no override was ever actually
	// invoked, this test would be exercising nothing.
	mu.Lock()
	n := invoked
	mu.Unlock()
	if n == 0 {
		t.Fatal("seam override was never invoked — fixture cannot exhibit the race it claims to guard")
	}
}
