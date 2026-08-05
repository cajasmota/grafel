package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/install/watchers"
)

// TestRecordWatchStart_TripsOnCrashLoop pins #6179 F4's macOS give-up.
//
// ThrottleInterval only bounds how fast launchd relaunches a crashing watcher,
// never whether it eventually stops — launchd has no StartLimitBurst. Without
// this detector, 140 crash-looping watchers relaunch at ~2.3/s forever, which
// is a lower-amplitude version of the reported storm with the same unbounded
// duration.
func TestRecordWatchStart_TripsOnCrashLoop(t *testing.T) {
	repo := t.TempDir()

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := base
	prev := flapNow
	flapNow = func() time.Time { return now }
	t.Cleanup(func() { flapNow = prev })

	// A crash loop relaunches every ThrottleInterval (60s).
	for i := 1; i <= flapMaxStarts; i++ {
		if stop, err := recordWatchStart(repo); stop || err != nil {
			t.Fatalf("start %d tripped the detector early: stop=%v err=%v", i, stop, err)
		}
		now = now.Add(time.Minute)
	}
	// The next start is over the line. It must be a DELIBERATE exit: nil, so
	// the process exits 0 and KeepAlive={SuccessfulExit:false} leaves it dead.
	stop, err := recordWatchStart(repo)
	if !stop {
		t.Fatal("the crash-loop detector never tripped; without it a crashing watcher relaunches " +
			"every ThrottleInterval forever, since launchd has no StartLimitBurst")
	}
	if err != nil {
		t.Fatalf("the crash-loop give-up returned %v; it must exit 0 like every other "+
			"deliberate give-up, or launchd relaunches it and the detector achieves nothing", err)
	}
	if !watchExitRespawn[watchExitFlapping] == false {
		t.Fatal("watchExitFlapping must be classified as do-not-respawn")
	}
}

// TestRecordWatchStart_RemediationActuallyWorks pins #6179 F4-a.
//
// The give-up message tells the user to re-run `grafel install`. That
// instruction was false: re-registering the unit triggers a launchctl
// bootstrap, the bootstrap's launch is itself a counted start, so the revived
// watcher immediately saw an over-threshold history and gave up again. Within
// the counting window the watcher could not be brought back at all — and the
// person hitting this is by definition someone debugging watchers, i.e. exactly
// the person who accumulated the starts in the first place.
//
// Registration now resets the history (watchers.ResetWatchStarts, called from
// install.Apply and install.ReconcileWatcherUnits). This asserts the property
// end to end: trip it, register, and the next start must survive.
func TestRecordWatchStart_RemediationActuallyWorks(t *testing.T) {
	repo := t.TempDir()

	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	prev := flapNow
	flapNow = func() time.Time { return now }
	t.Cleanup(func() { flapNow = prev })

	// Drive it over the threshold.
	var tripped bool
	for i := 0; i <= flapMaxStarts; i++ {
		stop, _ := recordWatchStart(repo)
		tripped = tripped || stop
		now = now.Add(30 * time.Second)
	}
	if !tripped {
		t.Fatal("setup: the detector never tripped")
	}
	// Confirm it is genuinely stuck without the remedy — this is the state the
	// reviewer measured, and it must stay reproducible if the reset regresses.
	if stop, _ := recordWatchStart(repo); !stop {
		t.Fatal("setup: expected the watcher to remain tripped inside the window")
	}

	// The remedy: re-register the unit. This is what `grafel install` does.
	if err := watchers.ResetWatchStarts(repo); err != nil {
		t.Fatalf("ResetWatchStarts: %v", err)
	}

	// The launch that the bootstrap itself produces must NOT re-trip.
	if stop, err := recordWatchStart(repo); stop || err != nil {
		t.Fatalf("the watcher gave up again immediately after re-registration (stop=%v err=%v). "+
			"The give-up message promises `grafel install` brings it back; if registration does "+
			"not clear the count, that promise is false (#6179 F4-a)", stop, err)
	}
	// And it has real headroom afterwards, not one spare start.
	for i := 0; i < flapMaxStarts-2; i++ {
		now = now.Add(30 * time.Second)
		if stop, _ := recordWatchStart(repo); stop {
			t.Fatalf("re-registration bought only %d starts of headroom; the reset must clear "+
				"the whole history, not trim it", i+1)
		}
	}
}

// TestRecordWatchStart_HealthyWatcherNeverTrips: a watcher that starts once per
// login and runs for days must never be stopped by this. The window prunes, so
// starts spread across weeks never accumulate.
func TestRecordWatchStart_HealthyWatcherNeverTrips(t *testing.T) {
	repo := t.TempDir()

	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	prev := flapNow
	flapNow = func() time.Time { return now }
	t.Cleanup(func() { flapNow = prev })

	// Thirty logins, one per day — far more than flapMaxStarts in total.
	for i := 0; i < 30; i++ {
		if stop, err := recordWatchStart(repo); stop || err != nil {
			t.Fatalf("daily login %d tripped the crash-loop detector: stop=%v err=%v", i, stop, err)
		}
		now = now.Add(24 * time.Hour)
	}
}

// TestRecordWatchStart_WindowPrunes proves the count is windowed, not
// cumulative: starts older than flapWindow must drop out of the record.
func TestRecordWatchStart_WindowPrunes(t *testing.T) {
	repo := t.TempDir()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	prev := flapNow
	flapNow = func() time.Time { return now }
	t.Cleanup(func() { flapNow = prev })

	for i := 0; i < flapMaxStarts; i++ {
		if stop, err := recordWatchStart(repo); stop || err != nil {
			t.Fatalf("stop=%v err=%v", stop, err)
		}
		now = now.Add(time.Minute)
	}
	// Jump past the window; the accumulated history must age out.
	now = now.Add(2 * flapWindow)
	rec := readFlapRecord(flapRecordPath(repo), now)
	if len(rec.Starts) != 0 {
		t.Fatalf("record still holds %d starts after %s; the window does not prune",
			len(rec.Starts), 2*flapWindow)
	}
	if stop, err := recordWatchStart(repo); stop || err != nil {
		t.Fatalf("a start after the window elapsed must not trip: stop=%v err=%v", stop, err)
	}
}

// TestRecordWatchStart_UnwritableRepoDoesNotStopTheWatcher: this is a safety
// valve, and a safety valve that fails closed is worse than no valve. A repo
// whose .grafel cannot be written must degrade to the previous behaviour.
func TestRecordWatchStart_UnwritableRepoDoesNotStopTheWatcher(t *testing.T) {
	repo := t.TempDir()
	// Make .grafel a FILE so MkdirAll and WriteFile both fail.
	if err := os.WriteFile(filepath.Join(repo, ".grafel"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < flapMaxStarts*3; i++ {
		if stop, err := recordWatchStart(repo); stop || err != nil {
			t.Fatalf("an unwritable record must never stop a watcher, got stop=%v err=%v", stop, err)
		}
	}
}

// TestRecordWatchStart_CorruptRecordIsIgnored: same fail-open argument for a
// truncated or hand-edited JSON file.
func TestRecordWatchStart_CorruptRecordIsIgnored(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".grafel"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flapRecordPath(repo), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if stop, err := recordWatchStart(repo); stop || err != nil {
		t.Fatalf("a corrupt record must be treated as empty, got stop=%v err=%v", stop, err)
	}
}

// TestRunWatch_CrashLoopExitsSuccessfully wires the detector into runWatch
// end-to-end: a repo whose history is already at the ceiling must make runWatch
// return immediately with nil, never reaching the poll loop.
func TestRunWatch_CrashLoopExitsSuccessfully(t *testing.T) {
	home := withSandboxHome(t)
	repo := filepath.Join(home, "repos", "flapper")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	prev := flapNow
	flapNow = func() time.Time { return now }
	t.Cleanup(func() { flapNow = prev })

	for i := 0; i <= flapMaxStarts; i++ {
		_, _ = recordWatchStart(repo)
		now = now.Add(time.Second)
	}

	done := make(chan error, 1)
	go func() { done <- runWatch(repo, "", time.Hour) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runWatch returned %v; the crash-loop give-up must exit 0", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("runWatch entered the poll loop despite a tripped crash-loop detector")
	}
}
