package cli

// watch_flap.go implements the watcher-side rapid-restart detector (#6179 F4).
//
// ── Why the supervisor cannot do this on macOS ───────────────────────────────
//
// ThrottleInterval bounds how OFTEN launchd relaunches a job, but it is a flat
// floor rather than a backoff, and launchd never gives up on a KeepAlive job.
// So a watcher that crashes for a reason relaunching cannot fix — a panic on a
// corrupt state file, an OOM, a BinPath a package manager moved out from under
// the plist — relaunches every 60s forever. At the 140 repos #6179 reports that
// is ~2.3 process launches per second, indefinitely: lower amplitude than the
// ~14/s the issue describes, but the same unbounded duration, and every launch
// is still a macOS Background Items event.
//
// systemd has StartLimitBurst for exactly this and the unit now sets it
// (see watchers.StartLimitBurst). launchd has no equivalent, so on macOS the
// give-up has to live inside the process: count our own recent starts, and once
// they are clearly a crash loop, exit 0. KeepAlive={SuccessfulExit:false} then
// leaves the job stopped, which is the only way a launchd job ever stops.
//
// ── What is actually counted, and what that costs ────────────────────────────
//
// This counts STARTS, not crashes. It cannot do otherwise: by the time the
// process is running, "launchd relaunched me because I crashed" and "a human
// just registered me" are indistinguishable from the inside. So every
// legitimate launch counts too — a manual `grafel watch`, or the launch that
// follows a launchctl bootstrap.
//
// That has a sharp consequence, and it is why watchers.ResetWatchStarts exists:
// registering a unit must clear the history, or the documented remedy for a
// tripped detector would be self-defeating (the re-registration is itself a
// counted start, so the revived watcher would give up again). install.Apply and
// install.ReconcileWatcherUnits both reset on registration.
//
// ── Why the thresholds ───────────────────────────────────────────────────────
//
// A healthy watcher starts once per login and runs for days. A crash-looping
// one starts every ThrottleInterval — 60 starts/hour. flapMaxStarts=12 in
// flapWindow=1h trips a crash loop in about twelve minutes.
//
// The margin against human workflow is real but not enormous, precisely because
// starts and not crashes are counted: the person most likely to reach this
// threshold is someone actively debugging watchers, who is also the person
// running `grafel watch` by hand repeatedly. Registration resets remove the
// dominant source (install / group add / reconcile), and 12 leaves room for a
// dozen manual runs an hour; the cost of the higher number is four extra
// minutes of a crash loop, which is the cheaper side of that trade.
//
// The record is per-repo and lives beside the watcher's own log files, so it is
// naturally scoped, naturally cleaned up when the repo is removed, and visible
// to anyone already looking at watcher.err.log for an explanation.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/cajasmota/grafel/internal/install/watchers"
)

const (
	// flapWindow is how far back start timestamps are counted.
	flapWindow = time.Hour
	// flapMaxStarts is the number of starts within flapWindow that means
	// "this is a crash loop, not a workflow".
	flapMaxStarts = 12
)

// flapNow is time.Now, indirected so tests can drive the clock.
var flapNow = time.Now

// flapRecordPath returns the per-repo start-history file. The location is
// owned by the watchers package because the registration paths there must be
// able to reset it (see watchers.ResetWatchStarts).
func flapRecordPath(repo string) string {
	return watchers.WatchStartsPath(repo)
}

// flapRecord is the on-disk start history.
type flapRecord struct {
	// Starts holds the timestamps of recent watcher starts, oldest first,
	// pruned to flapWindow on every read.
	Starts []time.Time `json:"starts"`
}

// readFlapRecord loads the start history, pruned to the window. A missing,
// unreadable or corrupt file yields an empty history: this is a safety valve,
// and a safety valve that fails closed would take out healthy watchers.
func readFlapRecord(path string, now time.Time) flapRecord {
	var rec flapRecord
	b, err := os.ReadFile(path)
	if err != nil {
		return flapRecord{}
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return flapRecord{}
	}
	cutoff := now.Add(-flapWindow)
	kept := rec.Starts[:0]
	for _, ts := range rec.Starts {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	rec.Starts = kept
	return rec
}

// recordWatchStart appends this start to the repo's history and reports whether
// the watcher should give up.
//
// It returns (stop, err). The two results are deliberately separate: a
// deliberate give-up is an exit-0 give-up, so its err is nil — exactly like
// every other entry in watchExitRespawn that is classified do-not-respawn.
// Signalling the give-up through the error alone would make it identical to
// "everything is fine", which is precisely the confusion #6179 is about. The
// caller must branch on stop, not on err.
//
// Any I/O problem is swallowed and returns (false, nil): an unwritable .grafel
// directory must degrade to the previous behaviour (no give-up) rather than
// stopping a watcher that is otherwise healthy.
func recordWatchStart(repo string) (stop bool, err error) {
	now := flapNow()
	path := flapRecordPath(repo)
	rec := readFlapRecord(path, now)
	rec.Starts = append(rec.Starts, now)

	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr == nil {
		if b, mErr := json.Marshal(rec); mErr == nil {
			_ = os.WriteFile(path, b, 0o644)
		}
	}

	if len(rec.Starts) <= flapMaxStarts {
		return false, nil
	}
	return true, watchExit(watchExitFlapping,
		"started %d times in the last %s for %s — treating this as a crash loop and stopping. "+
			"The cause is in this repo's watcher.err.log. To resume once it is fixed, either "+
			"delete %s or re-run 'grafel install' (which re-registers the unit and clears this "+
			"count for you)",
		len(rec.Starts), flapWindow, repo, path)
}
