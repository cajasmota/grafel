package watchers

// watchstarts.go owns the location and lifecycle of the per-repo watcher
// start-history file.
//
// The file itself is written and read by the crash-loop detector in
// internal/cli/watch_flap.go (#6179 F4). Its PATH and its RESET live here
// because both sides need them and only this package is imported by both:
// internal/install registers units, internal/cli runs the watcher.
//
// ── Why registration must reset it (#6179 F4-a) ──────────────────────────────
//
// The detector counts STARTS, not crashes — it cannot distinguish "launchd
// relaunched me because I crashed" from "a human just registered me", because
// by the time the process is running both look identical. A launchctl bootstrap
// IS a start. So without this reset, the documented remedy for a tripped
// detector ("re-run grafel install") was self-defeating: the re-registration
// counted as another start, the freshly bootstrapped watcher saw a
// still-over-threshold history, and gave up again. Within the counting window
// the watcher could not be brought back at all.
//
// Registering a unit is an explicit operator action that says "I want this
// watcher running", so it is exactly the right point to clear the history.

import (
	"os"
	"path/filepath"
)

// WatchStartsPath returns the per-repo watcher start-history file.
func WatchStartsPath(repo string) string {
	return filepath.Join(repo, ".grafel", "watch-starts.json")
}

// ResetWatchStarts deletes the start history for repo, so a watcher that gave
// up as a crash loop starts counting again from zero.
//
// Idempotent: a missing record — or a missing repo — is already the desired
// state and reports success. Registration paths call this unconditionally.
func ResetWatchStarts(repo string) error {
	err := os.Remove(WatchStartsPath(repo))
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	// A repo directory that does not exist at all surfaces as ENOTDIR on some
	// platforms rather than ENOENT; both mean "nothing to clear".
	if pe, ok := err.(*os.PathError); ok {
		if pe.Err == os.ErrNotExist {
			return nil
		}
	}
	return err
}
