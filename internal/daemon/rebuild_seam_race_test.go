package daemon

// rebuild_seam_race_test.go — pins the #6059 fix for the rebuild* test-seam
// family (rebuildRPCTimeout, rebuildWaitInterval, rebuildWaitStartupWindow,
// rebuildWaitTimeout, rebuildWaitStaleAfter, rebuildWaitClock,
// rebuildEngineAliveFn).
//
// The hazard is NOT the seams themselves — it is that production resolves every
// one of them from a goroutine the test does not own. Service.Rebuild runs on a
// net/rpc serving goroutine and awaitRebuildCompletion polls from it, while a
// fixture's t.Cleanup writes the knob during teardown. As plain package vars
// that pairing is an unsynchronised write/read and the detector reported it as
// `TestRebuild_SingleFlight_NoConcurrentOverlap` on roughly 1 `-race` run in 8.
//
// None of the seven is the benign read-once-at-startup shape (contrast
// listenFn), so all seven were converted.

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingWaitClock is a fake clock that records how often it is used, so the
// clock seam's vacuity can be checked the same way the func seam's is.
type countingWaitClock struct{ uses *int64 }

func (c countingWaitClock) Now() time.Time {
	atomic.AddInt64(c.uses, 1)
	return time.Unix(0, 0)
}
func (c countingWaitClock) Sleep(time.Duration) { atomic.AddInt64(c.uses, 1) }

// resolveAllRebuildSeams resolves every member of the family exactly the way
// production does (Service.Rebuild for the RPC cap; awaitRebuildCompletion for
// the wait knobs, the clock and the liveness probe). Returns the durations it
// saw so a caller can assert on them.
func resolveAllRebuildSeams() (rpc, interval, startup, timeout, stale time.Duration) {
	rpc = rebuildRPCTimeout()
	interval = rebuildWaitInterval()
	startup = rebuildWaitStartupWindow()
	timeout = rebuildWaitTimeout()
	stale = rebuildWaitStaleAfter()
	_ = rebuildWaitClock().Now()
	_ = rebuildEngineAliveFn()
	return
}

// installAllRebuildSeamOverrides installs one override for every member of the
// family and returns a single LIFO restore closure plus the NUMBER OF SEAM
// WRITES it performed. The count is returned rather than incremented by the
// caller so a writer-side vacuity counter cannot survive an edit that removes
// the writes: there is no way to get a non-zero n without having written.
func installAllRebuildSeamOverrides(i int, aliveCalls, clockUses *int64) (restore func(), writes int) {
	rA := setRebuildRPCTimeoutForTest(time.Duration(i%97+1) * time.Millisecond)
	rB := setRebuildWaitIntervalForTest(time.Duration(i%89+1) * time.Millisecond)
	rC := setRebuildWaitStartupWindowForTest(time.Duration(i%83+1) * time.Millisecond)
	rD := setRebuildWaitTimeoutForTest(time.Duration(i%79+1) * time.Millisecond)
	rE := setRebuildWaitStaleAfterForTest(time.Duration(i%73+1) * time.Millisecond)
	rF := setRebuildWaitClockForTest(countingWaitClock{uses: clockUses})
	rG := setRebuildEngineAliveForTest(func() bool {
		atomic.AddInt64(aliveCalls, 1)
		return false
	})
	return func() {
		rG()
		rF()
		rE()
		rD()
		rC()
		rB()
		rA()
	}, 7
}

// TestRebuildSeams_ConcurrentOverrideAndRead pins #6059. It has three
// independent failure modes, so it cannot silently stop protecting:
//
//   - Phase 1 (no -race needed): each setter must actually take effect and each
//     override must actually be invoked. This is half the vacuity guard — a
//     fixture that cannot exhibit the race it claims to guard fails here rather
//     than passing green. The other half is the writerWrites/readerIters pair at
//     the end of Phase 2: TSan needs a write and a read that are CONCURRENT, and
//     Phase 1 runs before the reader exists, so it cannot speak to that.
//   - Phase 2 (-race): a live reader goroutine resolves all seven seams in a
//     loop while the test installs and restores overrides. If any seam reverts
//     to a plain package var, the write/read pair is unsynchronised and the
//     detector fails this test. This is the shape of the original report.
//   - Phase 3 (no -race needed): after every restore, every seam must resolve
//     back to its compiled-in production default. Goes RED if set/restore stops
//     being LIFO-correct or stops reverting.
func TestRebuildSeams_ConcurrentOverrideAndRead(t *testing.T) {
	// ---- Phase 1: wiring + vacuity, fully synchronised. ----------------------
	var aliveCalls, clockUses int64

	// Every install is registered with t.Cleanup IMMEDIATELY, before anything
	// that can fail. Each t.Fatal below exits via runtime.Goexit, which runs
	// registered cleanups but does NOT touch plain locals — so holding these
	// restores only in locals would leak all seven overrides into every
	// subsequent test in the binary the moment an assertion fired. That is not a
	// cosmetic leak: countingWaitClock.Now() is frozen at time.Unix(0, 0), so in
	// awaitRebuildAck `elapsed` is permanently 0 and the alive override is
	// permanently true — the wait loop can then never time out, and
	// TestRebuild_SplitMode_WaitForCompletion_AckTimeout hot-spins on os.ReadFile
	// until the whole package times out. A one-line regression diagnostic would
	// become a 10-minute red on all three OS legs, i.e. exactly the
	// untrustworthy-red failure mode this branch exists to remove. Phase 3 still
	// calls the restores explicitly and re-asserts; the cleanup re-run is
	// idempotent because it re-stores the same captured `prev`.
	restoreRPC := setRebuildRPCTimeoutForTest(11 * time.Millisecond)
	t.Cleanup(restoreRPC)
	restoreInterval := setRebuildWaitIntervalForTest(12 * time.Millisecond)
	t.Cleanup(restoreInterval)
	restoreStartup := setRebuildWaitStartupWindowForTest(13 * time.Millisecond)
	t.Cleanup(restoreStartup)
	restoreTimeout := setRebuildWaitTimeoutForTest(14 * time.Millisecond)
	t.Cleanup(restoreTimeout)
	restoreStale := setRebuildWaitStaleAfterForTest(15 * time.Millisecond)
	t.Cleanup(restoreStale)
	restoreClock := setRebuildWaitClockForTest(countingWaitClock{uses: &clockUses})
	t.Cleanup(restoreClock)
	restoreAlive := setRebuildEngineAliveForTest(func() bool {
		atomic.AddInt64(&aliveCalls, 1)
		return true
	})
	t.Cleanup(restoreAlive)

	rpc, interval, startup, timeout, stale := resolveAllRebuildSeams()
	for _, c := range []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"rebuildRPCTimeout", rpc, 11 * time.Millisecond},
		{"rebuildWaitInterval", interval, 12 * time.Millisecond},
		{"rebuildWaitStartupWindow", startup, 13 * time.Millisecond},
		{"rebuildWaitTimeout", timeout, 14 * time.Millisecond},
		{"rebuildWaitStaleAfter", stale, 15 * time.Millisecond},
	} {
		if c.got != c.want {
			t.Fatalf("%s override did not take effect: got %v, want %v — fixture cannot exhibit the race it claims to guard", c.name, c.got, c.want)
		}
	}
	if atomic.LoadInt64(&aliveCalls) == 0 {
		t.Fatal("rebuildEngineAliveFn override was never invoked — fixture cannot exhibit the race it claims to guard")
	}
	if atomic.LoadInt64(&clockUses) == 0 {
		t.Fatal("rebuildWaitClock override was never used — fixture cannot exhibit the race it claims to guard")
	}

	// ---- Phase 2: the race shape. -------------------------------------------
	//
	// The liveness override installed above stays in place for the whole phase
	// so the reader never falls through to defaultRebuildEngineAlive, which
	// would do real filesystem I/O on every iteration and slow the loop to the
	// point where it samples almost no interleavings. Inner overrides still
	// nest under it, which is what the writer loop below exercises.
	var readerIters int64
	stop := make(chan struct{})
	readerLive := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Announce BEFORE the loop, not after an iteration: every counted
		// iteration must be concurrent with the writer, so the barrier must not
		// itself contribute one.
		close(readerLive)
		for {
			select {
			case <-stop:
				return
			default:
			}
			resolveAllRebuildSeams()
			atomic.AddInt64(&readerIters, 1)
		}
	}()
	// The writer must not start until the reader goroutine is provably running.
	// Without this the whole 2000-iteration writer loop can complete and close
	// `stop` before the reader is ever scheduled — the fixture then reports
	// itself vacuous, which is exactly what windows-latest did on the v0.2.0
	// gate (macos/ubuntu green). Go gives no scheduling guarantee to a freshly
	// created goroutine on any OS; Windows just loses the coin flip more often
	// under -race with several test binaries sharing the runner's cores.
	<-readerLive

	// writerWrites counts SEAM WRITES performed while the reader goroutine is
	// live. It is incremented only from installAllRebuildSeamOverrides' return
	// value, so it cannot be kept while the writes are removed — see the guard
	// below for why that matters.
	var writerWrites int

	// The loop runs 2000 iterations, and KEEPS GOING past 2000 until the reader
	// has resolved at least once. That extension is a hang guard, not a latency
	// budget: it does not decide whether the assertion holds, it only refuses to
	// stop writing while the reader still has nothing to be concurrent WITH. On
	// a host that hands the reader a core promptly it never runs a single extra
	// iteration; on a starved one it keeps the write side live instead of
	// declaring the fixture vacuous. Bounded by wall clock so a reader that
	// genuinely cannot run still reaches the guard below and fails the test
	// rather than hanging the package.
	writerDeadline := time.Now().Add(30 * time.Second)
	for i := 0; i < 2000 || (atomic.LoadInt64(&readerIters) == 0 && time.Now().Before(writerDeadline)); i++ {
		restore, n := installAllRebuildSeamOverrides(i, &aliveCalls, &clockUses)
		writerWrites += n
		// Resolve under the override too, so each seam is exercised in both
		// directions rather than only written.
		resolveAllRebuildSeams()
		restore()
	}
	// Sampled BEFORE close(stop) so it counts only resolves that happened while
	// the writer was still writing. Reading it after wg.Wait() would also count
	// the iterations the reader gets in during the shutdown handshake, which are
	// concurrent with nothing and would let the guard pass vacuously.
	concurrentIters := atomic.LoadInt64(&readerIters)

	close(stop)
	wg.Wait()
	if concurrentIters == 0 {
		t.Fatal("reader goroutine never resolved a seam — fixture cannot exhibit the race it claims to guard")
	}
	// The writer half of the same guard, and NOT redundant with Phase 1. Phase 1
	// proves the setters work, but it runs before the reader exists. TSan needs a
	// WRITE and a READ that are concurrent and unsynchronised: a plausible future
	// edit — "this test is slow, hoist the setup out of the loop" — leaves the
	// reader spinning over a set of seams nobody is writing, and the whole race
	// protection is silently gone while the vacuity messages still read as though
	// it were guarding. Measured, not assumed: with the plain-var defect present
	// (DATA RACE otherwise reported 5/5) AND this loop's body reduced to
	// read-only resolves, the test WITHOUT this guard passed 5/5 green and none
	// of the other guards fired. With this guard it fails 5/5.
	if writerWrites == 0 {
		t.Fatal("no seam override was written while the reader goroutine was live — fixture cannot exhibit the race it claims to guard")
	}

	// ---- Phase 3: restore semantics. ----------------------------------------
	restoreAlive()
	restoreClock()
	restoreStale()
	restoreTimeout()
	restoreStartup()
	restoreInterval()
	restoreRPC()

	rpc, interval, startup, timeout, stale = rebuildRPCTimeout(), rebuildWaitInterval(), rebuildWaitStartupWindow(), rebuildWaitTimeout(), rebuildWaitStaleAfter()
	for _, c := range []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"rebuildRPCTimeout", rpc, defaultRebuildRPCTimeout},
		{"rebuildWaitInterval", interval, defaultRebuildWaitInterval},
		{"rebuildWaitStartupWindow", startup, defaultRebuildWaitStartupWindow},
		{"rebuildWaitTimeout", timeout, defaultRebuildWaitTimeout},
		{"rebuildWaitStaleAfter", stale, defaultRebuildWaitStaleAfter},
	} {
		if c.got != c.want {
			t.Fatalf("after restore, %s = %v, want the production default %v", c.name, c.got, c.want)
		}
	}
	if _, ok := rebuildWaitClock().(realWaitClock); !ok {
		t.Fatalf("after restore, rebuildWaitClock = %T, want realWaitClock", rebuildWaitClock())
	}
}

// TestRebuildSeamSetters_NilAndZeroClear pins the clear convention (mirroring
// #6056 F6): passing nil / 0 CLEARS the override rather than installing a nil
// pointer or a zero bound. Without it, a nil clock or alive func would panic on
// the next resolve, and a zero duration would turn a 2h cap into an instant
// timeout — silently, from a live serving goroutine.
func TestRebuildSeamSetters_NilAndZeroClear(t *testing.T) {
	// Install real overrides first, so "clear" has something to clear and a
	// no-op implementation cannot pass by accident.
	var aliveCalls, clockUses int64
	defer setRebuildWaitClockForTest(countingWaitClock{uses: &clockUses})()
	defer setRebuildEngineAliveForTest(func() bool {
		atomic.AddInt64(&aliveCalls, 1)
		return true
	})()
	defer setRebuildRPCTimeoutForTest(7 * time.Millisecond)()

	if !rebuildEngineAliveFn() || atomic.LoadInt64(&aliveCalls) == 0 {
		t.Fatal("alive override was not invoked — fixture cannot detect a failed clear")
	}
	if _, ok := rebuildWaitClock().(countingWaitClock); !ok {
		t.Fatal("clock override was not installed — fixture cannot detect a failed clear")
	}
	if got := rebuildRPCTimeout(); got != 7*time.Millisecond {
		t.Fatalf("rpc-timeout override was not installed (got %v) — fixture cannot detect a failed clear", got)
	}

	// nil / 0 must CLEAR, not install.
	restoreNilAlive := setRebuildEngineAliveForTest(nil)
	restoreNilClock := setRebuildWaitClockForTest(nil)
	restoreZeroRPC := setRebuildRPCTimeoutForTest(0)

	if _, ok := rebuildWaitClock().(realWaitClock); !ok {
		t.Fatalf("nil did not clear the clock override: got %T", rebuildWaitClock())
	}
	if got := rebuildRPCTimeout(); got != defaultRebuildRPCTimeout {
		t.Fatalf("0 did not clear the rpc-timeout override: got %v, want %v", got, defaultRebuildRPCTimeout)
	}
	// The alive seam falls through to defaultRebuildEngineAlive, which reads the
	// liveness sidecar. Under a temp root there is none, so it must report false
	// — and must NOT panic, which is what a stored nil func would do.
	t.Setenv(EnvRoot, t.TempDir())
	if rebuildEngineAliveFn() {
		t.Fatal("nil did not clear the alive override: still reporting the override's true")
	}

	// A clear must nest like any other override: restore() puts the previous one
	// back.
	restoreZeroRPC()
	restoreNilClock()
	restoreNilAlive()
	if got := rebuildRPCTimeout(); got != 7*time.Millisecond {
		t.Fatalf("restore() after a zero clear did not reinstate the previous override: got %v", got)
	}
	if _, ok := rebuildWaitClock().(countingWaitClock); !ok {
		t.Fatalf("restore() after a nil clear did not reinstate the previous clock: got %T", rebuildWaitClock())
	}
	before := atomic.LoadInt64(&aliveCalls)
	if !rebuildEngineAliveFn() || atomic.LoadInt64(&aliveCalls) == before {
		t.Fatal("restore() after a nil clear did not reinstate the previous alive override")
	}
}
