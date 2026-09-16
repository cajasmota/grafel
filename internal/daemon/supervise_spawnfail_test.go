package daemon

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// supervise_spawnfail_test.go grades #7087: a spawn that never produced a child
// (cmd.Start returning an error) must not share the crash-loop budget, whose
// recovery rule — a child that stayed up past healthyUptime — it can never
// reach. Both directions are graded here; the SECOND is the one that gets
// skipped, so it is explicit.
//
// Everything is asserted from EMITTED artefacts — the fatal error text serve
// surfaces and the supervisor's log stream — plus spawn attempts counted by the
// injected command factory (an external observation of behaviour), never from a
// counter the supervisor keeps about itself.
//
// No assertion here is gated on wall-clock timing (#7062): backoffs are
// sub-millisecond and every wait is a generous outer deadline whose failure
// message names what actually failed.

// TestSupervisorSpawnClassificationNoopChild is the child-process entrypoint for
// the crash-loop arm: re-invoked as `<test binary> -test.run=...`, it starts
// successfully and exits ~immediately, which is a CRASH (a child existed), not a
// construction failure. Inert in the parent suite.
func TestSupervisorSpawnClassificationNoopChild(t *testing.T) {}

// lockedBuf is a concurrency-safe sink for the supervisor's log stream.
type lockedBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// isolateSpawnFailEnv sandboxes the daemon root/home for a supervisor test.
// Cross-platform (unlike isolateSupervisorEnv, which is darwin||linux-tagged):
// #7083's construction failure was a WINDOWS one, so this file must build and
// run there.
func isolateSpawnFailEnv(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	t.Setenv("GRAFEL_HOME", root)
	t.Setenv(EnvDisableSelfDefense, "1")
	return root
}

// unspawnableCommand returns a command whose Start ALWAYS fails, deterministically
// and identically, without ever creating a process: a path that does not exist.
func unspawnableCommand(t *testing.T) *exec.Cmd {
	t.Helper()
	missing := filepath.Join(t.TempDir(), "no-such-engine-binary")
	return exec.Command(missing)
}

// instantExitCommand returns a command that STARTS successfully and exits at
// once — a crash, the path whose budget #7087 says construction failures must
// not share.
func instantExitCommand(selfExe string) *exec.Cmd {
	cmd := exec.Command(selfExe, "-test.run=TestSupervisorSpawnClassificationNoopChild")
	cmd.Env = append(os.Environ(), "GRAFEL_ENGINE_CHILD_HELPER=")
	cmd.SysProcAttr = engineChildSysProcAttr()
	return cmd
}

// tuneForSpawnFailTest applies millisecond-scale tuning shared by both arms of
// the comparison, so the ONLY difference between them is what cmd.Start does.
func tuneForSpawnFailTest(s *engineSupervisor) {
	s.backoffInitial = time.Millisecond
	s.backoffMax = 2 * time.Millisecond
	s.healthyUptime = time.Hour // nothing in these tests ever "recovers" by uptime
	s.maxCeilingHits = 6
	s.maxSpawnFailures = 3
	s.drainTimeout = 2 * time.Second
}

// runUntilFatal starts a supervisor whose child command is built by mk (called
// once per spawn attempt), waits for the fatal, and returns the fatal error, the
// number of spawn attempts, and the emitted log stream.
func runUntilFatal(t *testing.T, mk func() *exec.Cmd) (err error, attempts int, logs string) {
	t.Helper()
	root := isolateSpawnFailEnv(t)

	var mu sync.Mutex
	n := 0
	defer SetEngineChildCommandForTest(func(selfExe, _ string) *exec.Cmd {
		mu.Lock()
		n++
		mu.Unlock()
		return mk()
	})()

	sink := &lockedBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := newEngineSupervisor(layoutFromRoot(root, ""), buildSlogLogger(sink))
	tuneForSpawnFailTest(sup)
	if serr := sup.start(ctx); serr != nil {
		t.Fatalf("supervisor start: %v", serr)
	}
	t.Cleanup(sup.stop)

	select {
	case err = <-sup.fatal():
	case <-time.After(60 * time.Second):
		mu.Lock()
		got := n
		mu.Unlock()
		t.Fatalf("supervisor never surfaced a fatal (spawn attempts so far: %d); logs:\n%s", got, sink.String())
	}
	sup.stop()
	mu.Lock()
	attempts = n
	mu.Unlock()
	return err, attempts, sink.String()
}

// TestEngineSupervisor_RepeatedSpawnFailureStopsSoonerAndIsDistinguishable is
// direction 1. The permissive direction is the STATUS QUO — before #7087 both
// arms gave up after the same number of attempts with the same message — so
// "it eventually gives up" would grade nothing. Every assertion here
// DISTINGUISHES the two paths.
func TestEngineSupervisor_RepeatedSpawnFailureStopsSoonerAndIsDistinguishable(t *testing.T) {
	selfExe, xerr := os.Executable()
	if xerr != nil {
		t.Fatalf("os.Executable: %v", xerr)
	}

	spawnErr, spawnAttempts, spawnLogs := runUntilFatal(t, func() *exec.Cmd { return unspawnableCommand(t) })
	crashErr, crashAttempts, crashLogs := runUntilFatal(t, func() *exec.Cmd { return instantExitCommand(selfExe) })

	// Emitted artefact 1: the fatal error text serve surfaces.
	if !strings.Contains(spawnErr.Error(), "unspawnable") {
		t.Errorf("construction-failure fatal does not name the unspawnable verdict: %q", spawnErr)
	}
	if strings.Contains(spawnErr.Error(), "crash-looping") {
		t.Errorf("construction-failure fatal blames a crash loop that never happened: %q", spawnErr)
	}
	if !strings.Contains(crashErr.Error(), "crash-looping") {
		t.Errorf("crash-loop fatal lost its crash-looping verdict: %q", crashErr)
	}
	if strings.Contains(crashErr.Error(), "unspawnable") {
		t.Errorf("crash-loop fatal claims the engine was unspawnable, but children did run: %q", crashErr)
	}

	// Emitted artefact 2: the log stream. A construction failure says no child
	// process was created; a crash never does.
	if !strings.Contains(spawnLogs, "no child process was created") {
		t.Errorf("construction-failure log stream lacks the distinct spawn-failure verb; logs:\n%s", spawnLogs)
	}
	if strings.Contains(crashLogs, "no child process was created") {
		t.Errorf("crash-loop log stream claims no child process was created; logs:\n%s", crashLogs)
	}
	if !strings.Contains(crashLogs, "engine child exited") {
		t.Errorf("crash-loop log stream lacks the child-exited line; logs:\n%s", crashLogs)
	}

	// Behavioural distinction, observed from OUTSIDE (attempts counted by the
	// injected factory): the construction failure must stop SOONER. With
	// identical tuning, pre-#7087 both arms took the same number of attempts.
	if spawnAttempts >= crashAttempts {
		t.Errorf("a repeated identical construction failure did not stop sooner than a crash loop: %d spawn attempts vs %d crash attempts",
			spawnAttempts, crashAttempts)
	}
	if spawnAttempts != 3 { // maxSpawnFailures
		t.Errorf("construction failure gave up after %d attempts, want 3 (maxSpawnFailures)", spawnAttempts)
	}
}

// TestEngineSupervisor_TransientSpawnFailureStillRecovers is direction 2 — the
// one that gets skipped. "Fail faster" must not become "fail on the first
// hiccup": a construction failure followed by a SUCCESSFUL spawn resets the
// construction budget, so a supervisor that sees more total spawn failures than
// the budget, spread across successful spawns, must never give up.
func TestEngineSupervisor_TransientSpawnFailureStillRecovers(t *testing.T) {
	selfExe, xerr := os.Executable()
	if xerr != nil {
		t.Fatalf("os.Executable: %v", xerr)
	}
	root := isolateSpawnFailEnv(t)

	// Attempt script: fail, fail, succeed — repeating. maxSpawnFailures is 3, so
	// without the reset-on-successful-Start the 3rd failure overall (attempt 5)
	// would be fatal.
	var mu sync.Mutex
	attempts, failures, successes := 0, 0, 0
	defer SetEngineChildCommandForTest(func(_ string, _ string) *exec.Cmd {
		mu.Lock()
		attempts++
		transient := attempts%3 != 0
		if transient {
			failures++
		} else {
			successes++
		}
		mu.Unlock()
		if transient {
			return unspawnableCommand(t)
		}
		return instantExitCommand(selfExe)
	})()

	sink := &lockedBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := newEngineSupervisor(layoutFromRoot(root, ""), buildSlogLogger(sink))
	tuneForSpawnFailTest(sup)
	// The successful spawns here exit instantly, i.e. they crash; give the crash
	// budget enough room that it cannot be what ends this test.
	sup.maxCeilingHits = 1000
	if err := sup.start(ctx); err != nil {
		t.Fatalf("supervisor start: %v", err)
	}
	t.Cleanup(sup.stop)

	// Wait for enough of the script to have run that the budget would have been
	// blown twice over without the reset.
	deadline := time.Now().Add(60 * time.Second)
	for {
		mu.Lock()
		f, s := failures, successes
		mu.Unlock()
		if f >= 4 && s >= 2 {
			break
		}
		if fatalErr := sup.fatalError(); fatalErr != nil {
			t.Fatalf("supervisor gave up on a TRANSIENT construction failure after %d failed and %d successful spawns: %v\nlogs:\n%s",
				f, s, fatalErr, sink.String())
		}
		if time.Now().After(deadline) {
			t.Fatalf("script never progressed far enough to grade recovery: %d failed, %d successful spawns (want >=4 and >=2)\nlogs:\n%s",
				f, s, sink.String())
		}
		time.Sleep(2 * time.Millisecond)
	}

	if err := sup.fatalError(); err != nil {
		t.Fatalf("supervisor gave up despite recovering spawns: %v\nlogs:\n%s", err, sink.String())
	}
	logs := sink.String()
	if strings.Contains(logs, "unspawnable") {
		t.Errorf("supervisor emitted the unspawnable verdict for a recoverable condition; logs:\n%s", logs)
	}
	// Positive control: the transient failures really were construction failures,
	// i.e. this test is grading the path it claims to.
	if !strings.Contains(logs, "no child process was created") {
		t.Errorf("no construction failure was ever emitted — this test graded nothing; logs:\n%s", logs)
	}
	sup.stop()
}
