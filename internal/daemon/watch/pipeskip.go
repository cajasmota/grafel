package watch

import "os"

// unwatchableEntryMode reports whether an entry of this mode is one fsnotify's
// kqueue backend refuses to watch: addWatch returns ("", nil) — success with NO
// watch established — as soon as os.Lstat reports os.ModeNamedPipe or
// os.ModeSocket (backend_kqueue.go:363-368). No descriptor is opened, so grafel
// must neither charge one nor count the entry among a directory's watched
// entries (#7245).
//
// The mode must be the UNRESOLVED one — os.Lstat, or os.DirEntry.Type() —
// because that is what addWatch tests. A symlink pointing at a FIFO IS watched
// (internalWatch passes listDir=true, which skips addWatch's readlink branch,
// so unix.Open runs and follows the link) and is charged like any other entry.
//
// Deliberately NOT gated by GOOS. Every caller is already behind
// `cost.perEntry() > 0`, which is this package's discriminator for "does the
// backend take a descriptor per entry at all" — see fdCostModel.perEntry, whose
// own comment says per-entry charges vanish on a per-watch backend "rather than
// being GOOS-gated by hand". inotifyCostModel.perEntry() is 0, so on Linux
// every one of these sites short-circuits before reaching this predicate and
// the question is moot. A hand-rolled kqueue GOOS list would be a second,
// drifting copy of a discriminator the package already has.
func unwatchableEntryMode(m os.FileMode) bool {
	return m&(os.ModeNamedPipe|os.ModeSocket) != 0
}
