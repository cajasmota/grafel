//go:build !linux

package watch

import "errors"

// inotifyPoolApplies records that this platform has NO per-UID inotify watch
// pool. macOS spends process file descriptors through kqueue (see
// fdbudget_darwin.go) and Windows holds one handle per watched tree; neither is
// the resource #6932 is about. Build-tagged, not runtime.GOOS-gated (#6218).
const inotifyPoolApplies = false

// errInotifyNotApplicable is returned instead of a fabricated 0. A probe that
// silently reported "limit 0" here would read as "no headroom at all" on a
// platform where the question does not arise.
var errInotifyNotApplicable = errors.New("watch: inotify watch pool is a Linux facility and does not exist on this platform")

// inotifyMaxUserWatchesPath is named for the message only; nothing reads it
// here.
const inotifyMaxUserWatchesPath = "/proc/sys/fs/inotify/max_user_watches"

func readInotifyLimit() (int, string, error) {
	return 0, "", errInotifyNotApplicable
}
