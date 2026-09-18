package watch

import "os"

// unwatchableEntryMode reports whether an entry of this mode is one the
// platform's fsnotify backend refuses to watch, so grafel must neither charge a
// descriptor for it nor count it among a directory's watched entries (#7245).
//
// The mode must be the UNRESOLVED one — os.Lstat, or os.DirEntry.Type() —
// because that is what addWatch tests. A symlink pointing at a FIFO is watched,
// and charged.
//
// On every platform whose backend has not been read for this property the
// answer is false, which is the pre-#7245 arithmetic exactly. See
// backendSkipsPipesAndSockets.
func unwatchableEntryMode(m os.FileMode) bool {
	if !backendSkipsPipesAndSockets {
		return false
	}
	return m&(os.ModeNamedPipe|os.ModeSocket) != 0
}
