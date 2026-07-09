// Package fdlimit raises the process's RLIMIT_NOFILE (max open file
// descriptors) soft limit at daemon startup.
//
// Background (#5675): a worktree indexing storm can subscribe many working
// trees to the fsnotify watcher, each of which costs ~1 fd per directory on
// Linux inotify. If the process hits its soft fd limit it dies while
// persisting its store; launchd/systemd KeepAlive/Restart relaunches it and
// boot re-discovery restarts the storm — an unbreakable crash-loop. Raising
// the soft limit toward the hard limit is one layer of defense-in-depth so the
// daemon has ample headroom before it can ever exhaust fds.
//
// The raise is always non-fatal and NEVER lowers an already-high limit.
package fdlimit

// DefaultTarget is a sane fd headroom target used when the caller does not
// specify one. 65536 comfortably covers a large multi-worktree fleet on Linux
// inotify while staying within typical hard-limit ceilings.
const DefaultTarget uint64 = 65536
