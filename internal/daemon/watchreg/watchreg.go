// Package watchreg is the daemon-owned registry of standalone `grafel watch`
// child processes (issue #5142, follow-up to #5140/#5141).
//
// # Problem
//
// Pre-Phase-B, `grafel watch <repo>` runs as a standalone launchd/systemd
// process. If its owning daemon dies/restarts, the watcher is orphaned. #5141
// gave the watcher a self-reap (exit after N consecutive daemon-unreachable
// failures), but that is best-effort and slow: a watcher that is wedged, paused,
// or whose daemon comes back on a different socket can linger. #5142 asks the
// daemon to OWN the lifecycle — track watcher PIDs and reap dead/orphaned ones.
//
// # Design
//
// Each standalone watcher writes a tiny JSON entry into a shared registry file
// under the daemon root (`~/.grafel/watchers.json`) at startup and removes
// its own entry on clean exit. The daemon's reaper periodically:
//
//  1. drops entries whose PID is no longer alive (signal-0 probe), and
//  2. SIGTERMs + drops entries that are alive but ORPHANED — their recorded
//     owner daemon PID is no longer the live daemon (the daemon that restarted
//     adopts the file by stamping its own PID; any watcher still claiming the
//     old owner is a leftover from a previous daemon generation).
//
// The registry is a single JSON file guarded by an OS file lock for the
// read-modify-write, so concurrent watcher registrations and the daemon sweep
// never corrupt it. All process-touching operations (liveness, kill) are
// injectable so the staleness/reaping logic is unit-testable with fake PIDs.
package watchreg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// FileName is the basename of the watcher registry under the daemon root.
const FileName = "watchers.json"

// Entry records one standalone `grafel watch` process.
type Entry struct {
	// PID is the watcher process id.
	PID int `json:"pid"`
	// Repo is the absolute repo path the watcher polls (diagnostic).
	Repo string `json:"repo"`
	// OwnerDaemonPID is the PID of the daemon that was live when the watcher
	// registered. When the live daemon's PID differs, the watcher is an orphan
	// from a previous daemon generation and is reaped. Zero means "unknown
	// owner" — never treated as orphaned on that basis alone.
	OwnerDaemonPID int `json:"owner_daemon_pid,omitempty"`
	// StartedUnix is the watcher's registration time (diagnostic).
	StartedUnix int64 `json:"started_unix,omitempty"`
}

// Registry is the on-disk watcher registry. It is safe for concurrent use
// both WITHIN a process (an in-process mutex serializes the read-modify-write)
// and ACROSS processes (a sidecar lock file).
type Registry struct {
	path string
	// mu serializes the read-modify-write of watchers.json for goroutines in the
	// SAME process. The sidecar lock file only coordinates SEPARATE processes; it
	// is not re-entrant and, on Windows, a lock file pending deletion by one
	// goroutine can race the O_CREATE|O_EXCL acquire of another (manifesting as
	// "Access is denied"). Serializing in-process callers here makes concurrent
	// Register/Deregister/Sweep deterministic and keeps each process to a single
	// outstanding lock-file holder at a time.
	mu sync.Mutex
}

// New returns a Registry backed by the watchers.json at path (typically
// filepath.Join(layout.Root, FileName)).
func New(path string) *Registry { return &Registry{path: path} }

// DefaultPath returns the conventional watchers.json path under daemonRoot.
func DefaultPath(daemonRoot string) string {
	if daemonRoot == "" {
		return ""
	}
	return filepath.Join(daemonRoot, FileName)
}

// Register adds (or refreshes) the entry for e.PID, stamping StartedUnix when
// unset. An existing entry with the same PID is replaced. The read-modify-write
// is serialized by an OS file lock.
func (r *Registry) Register(e Entry) error {
	if e.StartedUnix == 0 {
		e.StartedUnix = time.Now().Unix()
	}
	return r.mutate(func(entries []Entry) []Entry {
		out := entries[:0]
		for _, x := range entries {
			if x.PID != e.PID {
				out = append(out, x)
			}
		}
		return append(out, e)
	})
}

// Deregister removes the entry for pid (clean watcher exit). Missing pid is a
// no-op.
func (r *Registry) Deregister(pid int) error {
	return r.mutate(func(entries []Entry) []Entry {
		out := entries[:0]
		for _, x := range entries {
			if x.PID != pid {
				out = append(out, x)
			}
		}
		return out
	})
}

// List returns the current entries (sorted by PID for determinism).
func (r *Registry) List() ([]Entry, error) {
	entries, err := r.read()
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].PID < entries[j].PID })
	return entries, nil
}

// SweepDeps are the injectable process primitives the reaper uses, so the
// staleness/reaping logic can be unit-tested with fake PIDs.
type SweepDeps struct {
	// Alive reports whether the process with pid exists (signal-0 probe in
	// production).
	Alive func(pid int) bool
	// Kill sends SIGTERM to pid. Errors are recorded but do not abort the sweep.
	Kill func(pid int) error
	// LiveDaemonPID returns the PID of the currently-live daemon (os.Getpid in
	// production). An entry whose OwnerDaemonPID is non-zero and != this value is
	// an orphan from a previous daemon generation.
	LiveDaemonPID func() int
}

// SweepResult summarises one reaping pass.
type SweepResult struct {
	// Dead are PIDs dropped because the process no longer exists.
	Dead []int
	// Orphaned are live PIDs that were SIGTERM'd + dropped because their owner
	// daemon is no longer the live daemon.
	Orphaned []int
	// KillErrors maps PID → error for orphans whose SIGTERM failed (they are
	// still dropped from the registry — a kill failure usually means the process
	// raced to exit).
	KillErrors map[int]error
}

// Reaped reports the total number of entries removed by the sweep.
func (s SweepResult) Reaped() int { return len(s.Dead) + len(s.Orphaned) }

// Sweep reconciles the registry against process liveness and ownership. Dead
// entries are dropped; live-but-orphaned entries are SIGTERM'd and dropped.
// Live entries owned by the current daemon are kept. The whole pass is one
// locked read-modify-write so it never races a concurrent Register/Deregister.
func (r *Registry) Sweep(deps SweepDeps) (SweepResult, error) {
	res := SweepResult{KillErrors: map[int]error{}}
	if deps.Alive == nil {
		deps.Alive = func(int) bool { return true }
	}
	live := 0
	if deps.LiveDaemonPID != nil {
		live = deps.LiveDaemonPID()
	}
	err := r.mutate(func(entries []Entry) []Entry {
		kept := entries[:0]
		for _, e := range entries {
			if e.PID <= 0 {
				continue // malformed — drop.
			}
			if !deps.Alive(e.PID) {
				res.Dead = append(res.Dead, e.PID)
				continue
			}
			// Alive but orphaned: owner recorded, and it isn't the live daemon.
			if e.OwnerDaemonPID > 0 && live > 0 && e.OwnerDaemonPID != live {
				res.Orphaned = append(res.Orphaned, e.PID)
				if deps.Kill != nil {
					if kerr := deps.Kill(e.PID); kerr != nil {
						res.KillErrors[e.PID] = kerr
					}
				}
				continue
			}
			kept = append(kept, e)
		}
		return kept
	})
	sort.Ints(res.Dead)
	sort.Ints(res.Orphaned)
	return res, err
}

// AdoptOwner rewrites every entry's OwnerDaemonPID to newOwner. Called once by a
// freshly-started daemon so watchers spawned under it (and any pre-existing
// watcher this daemon chooses to adopt) are owned by the live daemon; any
// watcher still claiming a DIFFERENT owner after this is a true orphan that the
// next Sweep reaps. Returns the number of entries whose owner changed.
//
// NOTE: AdoptOwner is intentionally NOT called blindly at daemon start — doing
// so would adopt genuine orphans and hide them from the sweep. The daemon calls
// Sweep FIRST (reaping orphans from the dead previous daemon) and only then, if
// it spawns watchers itself, stamps them with its own PID via Register.
func (r *Registry) AdoptOwner(newOwner int) (int, error) {
	changed := 0
	err := r.mutate(func(entries []Entry) []Entry {
		for i := range entries {
			if entries[i].OwnerDaemonPID != newOwner {
				entries[i].OwnerDaemonPID = newOwner
				changed++
			}
		}
		return entries
	})
	return changed, err
}

// --- internal read/write with file locking ---

func (r *Registry) read() ([]Entry, error) {
	b, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // empty registry is valid.
		}
		return nil, err
	}
	if len(b) == 0 {
		return nil, nil
	}
	var entries []Entry
	if err := json.Unmarshal(b, &entries); err != nil {
		// A corrupt registry must not wedge the daemon: treat as empty. The next
		// write overwrites it cleanly.
		return nil, nil
	}
	return entries, nil
}

func (r *Registry) write(entries []Entry) error {
	if entries == nil {
		entries = []Entry{}
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	// Write to a UNIQUE temp file in the same directory (os.CreateTemp picks a
	// collision-free name), then atomically rename it over r.path. A fixed
	// "<path>.tmp" name is unsafe on Windows: if two writers in different
	// processes ever overlap (or a previous crash left the temp open), the
	// reused name collides and the rename can fail. CreateTemp sidesteps that.
	dir := filepath.Dir(r.path)
	f, err := os.CreateTemp(dir, "."+FileName+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := renameReplace(tmp, r.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// renameReplace atomically replaces dst with src. On Unix os.Rename already
// replaces an existing target in a single try. On Windows MoveFileEx (which
// os.Rename uses with MOVEFILE_REPLACE_EXISTING) can still fail transiently
// with a sharing violation / access-denied when dst is momentarily open — by
// a concurrent reader, an indexer, or antivirus. The mutation this guards is
// already serialized by the registry lock, so the only failures left are these
// transient Windows sharing windows; a short bounded retry clears them. Unix
// succeeds on the first iteration and never sleeps.
func renameReplace(src, dst string) error {
	const (
		attempts  = 20
		pollEvery = 5 * time.Millisecond
	)
	var err error
	for i := 0; i < attempts; i++ {
		if err = os.Rename(src, dst); err == nil {
			return nil
		}
		time.Sleep(pollEvery)
	}
	return err
}

// mutate runs fn under the registry lock against the current entries and writes
// the result atomically. fn may reuse the input slice's backing array.
func (r *Registry) mutate(fn func([]Entry) []Entry) error {
	// Serialize same-process callers first: the cross-process lock file is not
	// re-entrant and, on Windows, its acquire/release (O_CREATE|O_EXCL + Remove)
	// races between goroutines. With this mutex held, exactly one goroutine per
	// process contends for the lock file, eliminating the "Access is denied" race
	// entirely for the in-process case and guaranteeing the read-modify-write is
	// atomic w.r.t. other in-process mutators (no lost entries).
	r.mu.Lock()
	defer r.mu.Unlock()

	// Ensure the state directory exists before creating the .lock inside it.
	// On a fresh install the state dir (e.g. %AppData%\grafel on Windows) may
	// not exist yet, and opening the lock file inside a missing directory fails
	// with "The system cannot find the path specified". Idempotent: a no-op
	// when the directory already exists.
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}

	unlock, err := lockFile(r.path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()

	entries, err := r.read()
	if err != nil {
		return err
	}
	return r.write(fn(entries))
}
