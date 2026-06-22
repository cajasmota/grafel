package watchreg

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	return New(filepath.Join(t.TempDir(), FileName))
}

// TestMutate_CreatesParentDir verifies that mutating the registry on a fresh
// install — where the state directory does not exist yet — creates the parent
// directory instead of failing to open the .lock file. On a clean Windows
// install %AppData%\grafel does not exist, and the lock open previously failed
// with "The system cannot find the path specified" (bug 4).
func TestMutate_CreatesParentDir(t *testing.T) {
	// Path two levels below a fresh temp dir: neither nested dir exists yet.
	missingDir := filepath.Join(t.TempDir(), "grafel", "state")
	r := New(filepath.Join(missingDir, FileName))

	if _, err := os.Stat(missingDir); !os.IsNotExist(err) {
		t.Fatalf("precondition: dir should not exist yet, stat err = %v", err)
	}
	if err := r.Register(Entry{PID: 100, Repo: "/a", OwnerDaemonPID: 1}); err != nil {
		t.Fatalf("Register on fresh path: %v", err)
	}
	if _, err := os.Stat(missingDir); err != nil {
		t.Errorf("expected parent dir to be created, stat err = %v", err)
	}
	entries, err := r.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].PID != 100 {
		t.Errorf("expected one entry PID 100, got %v", entries)
	}
}

func pids(entries []Entry) []int {
	out := make([]int, len(entries))
	for i, e := range entries {
		out[i] = e.PID
	}
	return out
}

// TestRegisterListDeregister covers the basic lifecycle.
func TestRegisterListDeregister(t *testing.T) {
	r := newTestRegistry(t)

	if got, err := r.List(); err != nil || len(got) != 0 {
		t.Fatalf("empty registry: got (%v, %v)", got, err)
	}

	if err := r.Register(Entry{PID: 100, Repo: "/a", OwnerDaemonPID: 1}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := r.Register(Entry{PID: 200, Repo: "/b", OwnerDaemonPID: 1}); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, _ := r.List()
	if !reflect.DeepEqual(pids(got), []int{100, 200}) {
		t.Fatalf("after register: pids = %v, want [100 200]", pids(got))
	}
	if got[0].StartedUnix == 0 {
		t.Fatalf("StartedUnix should be stamped on register")
	}

	// Re-register same PID replaces (no dup).
	if err := r.Register(Entry{PID: 100, Repo: "/a2", OwnerDaemonPID: 2}); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	got, _ = r.List()
	if !reflect.DeepEqual(pids(got), []int{100, 200}) {
		t.Fatalf("re-register should not dup: pids = %v", pids(got))
	}
	if got[0].Repo != "/a2" || got[0].OwnerDaemonPID != 2 {
		t.Fatalf("re-register should replace fields: %+v", got[0])
	}

	if err := r.Deregister(100); err != nil {
		t.Fatalf("deregister: %v", err)
	}
	got, _ = r.List()
	if !reflect.DeepEqual(pids(got), []int{200}) {
		t.Fatalf("after deregister: pids = %v, want [200]", pids(got))
	}

	// Deregister missing PID is a no-op.
	if err := r.Deregister(999); err != nil {
		t.Fatalf("deregister missing: %v", err)
	}
}

// TestSweep_DropsDeadPIDs: an entry whose process is gone is dropped (no kill).
func TestSweep_DropsDeadPIDs(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Register(Entry{PID: 100, OwnerDaemonPID: 7})
	_ = r.Register(Entry{PID: 200, OwnerDaemonPID: 7})

	var killed []int
	res, err := r.Sweep(SweepDeps{
		Alive:         func(pid int) bool { return pid == 200 }, // 100 is dead
		Kill:          func(pid int) error { killed = append(killed, pid); return nil },
		LiveDaemonPID: func() int { return 7 }, // owner matches → 200 is healthy
	})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !reflect.DeepEqual(res.Dead, []int{100}) {
		t.Fatalf("Dead = %v, want [100]", res.Dead)
	}
	if len(res.Orphaned) != 0 {
		t.Fatalf("Orphaned = %v, want none", res.Orphaned)
	}
	if len(killed) != 0 {
		t.Fatalf("dead PIDs must NOT be killed, killed=%v", killed)
	}
	got, _ := r.List()
	if !reflect.DeepEqual(pids(got), []int{200}) {
		t.Fatalf("after sweep: pids = %v, want [200]", pids(got))
	}
}

// TestSweep_ReapsOrphans: a live watcher whose owner daemon is no longer the
// live daemon is SIGTERM'd and dropped.
func TestSweep_ReapsOrphans(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Register(Entry{PID: 100, OwnerDaemonPID: 7}) // owned by old daemon 7
	_ = r.Register(Entry{PID: 200, OwnerDaemonPID: 9}) // owned by live daemon 9

	var killed []int
	res, err := r.Sweep(SweepDeps{
		Alive:         func(int) bool { return true }, // both alive
		Kill:          func(pid int) error { killed = append(killed, pid); return nil },
		LiveDaemonPID: func() int { return 9 },
	})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !reflect.DeepEqual(res.Orphaned, []int{100}) {
		t.Fatalf("Orphaned = %v, want [100]", res.Orphaned)
	}
	if !reflect.DeepEqual(killed, []int{100}) {
		t.Fatalf("orphan must be SIGTERM'd, killed=%v want [100]", killed)
	}
	got, _ := r.List()
	if !reflect.DeepEqual(pids(got), []int{200}) {
		t.Fatalf("after sweep: pids = %v, want [200] (live owned watcher kept)", pids(got))
	}
	if res.Reaped() != 1 {
		t.Fatalf("Reaped() = %d, want 1", res.Reaped())
	}
}

// TestSweep_UnknownOwnerNeverOrphaned: an entry with OwnerDaemonPID==0 is never
// reaped on the orphan basis — we only kill watchers we can prove belong to a
// dead daemon generation.
func TestSweep_UnknownOwnerNeverOrphaned(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Register(Entry{PID: 100, OwnerDaemonPID: 0})

	var killed []int
	res, _ := r.Sweep(SweepDeps{
		Alive:         func(int) bool { return true },
		Kill:          func(pid int) error { killed = append(killed, pid); return nil },
		LiveDaemonPID: func() int { return 9 },
	})
	if res.Reaped() != 0 {
		t.Fatalf("unknown-owner live watcher must not be reaped: %+v", res)
	}
	if len(killed) != 0 {
		t.Fatalf("unknown-owner watcher must not be killed, killed=%v", killed)
	}
}

// TestSweep_KillErrorStillDrops: a SIGTERM failure (process raced to exit) is
// recorded but the orphan is still removed from the registry.
func TestSweep_KillErrorStillDrops(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Register(Entry{PID: 100, OwnerDaemonPID: 7})

	res, _ := r.Sweep(SweepDeps{
		Alive:         func(int) bool { return true },
		Kill:          func(int) error { return errKill },
		LiveDaemonPID: func() int { return 9 },
	})
	if len(res.KillErrors) != 1 || res.KillErrors[100] == nil {
		t.Fatalf("KillErrors should record pid 100: %+v", res.KillErrors)
	}
	got, _ := r.List()
	if len(got) != 0 {
		t.Fatalf("orphan must be dropped even when kill fails: %v", pids(got))
	}
}

// TestSweep_DropsMalformed: a non-positive PID entry is dropped as malformed.
func TestSweep_DropsMalformed(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Register(Entry{PID: 0})
	_ = r.Register(Entry{PID: 50, OwnerDaemonPID: 9})
	r.Sweep(SweepDeps{Alive: func(int) bool { return true }, LiveDaemonPID: func() int { return 9 }})
	got, _ := r.List()
	if !reflect.DeepEqual(pids(got), []int{50}) {
		t.Fatalf("malformed PID 0 should be dropped: pids = %v", pids(got))
	}
}

// TestAdoptOwner rewrites every entry's owner and reports the change count.
func TestAdoptOwner(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Register(Entry{PID: 100, OwnerDaemonPID: 7})
	_ = r.Register(Entry{PID: 200, OwnerDaemonPID: 9})

	changed, err := r.AdoptOwner(9)
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if changed != 1 { // only PID 100 changed (200 was already 9)
		t.Fatalf("AdoptOwner changed = %d, want 1", changed)
	}
	got, _ := r.List()
	for _, e := range got {
		if e.OwnerDaemonPID != 9 {
			t.Fatalf("entry %d owner = %d, want 9", e.PID, e.OwnerDaemonPID)
		}
	}
}

// TestConcurrentRegister exercises the cross-process lock under contention.
func TestConcurrentRegister(t *testing.T) {
	r := newTestRegistry(t)
	var wg sync.WaitGroup
	for i := 1; i <= 20; i++ {
		wg.Add(1)
		go func(pid int) {
			defer wg.Done()
			if err := r.Register(Entry{PID: pid, OwnerDaemonPID: 1}); err != nil {
				t.Errorf("register %d: %v", pid, err)
			}
		}(i)
	}
	wg.Wait()
	got, _ := r.List()
	if len(got) != 20 {
		t.Fatalf("concurrent register lost entries: got %d, want 20", len(got))
	}
}

// TestSweep_NilAliveDefaultsAlive: with no Alive func, entries are treated as
// alive (only orphan reaping applies).
func TestSweep_NilAliveDefaultsAlive(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Register(Entry{PID: 100, OwnerDaemonPID: 9})
	res, _ := r.Sweep(SweepDeps{LiveDaemonPID: func() int { return 9 }})
	if res.Reaped() != 0 {
		t.Fatalf("nil Alive should default to alive, healthy entry kept: %+v", res)
	}
}

var errKill = errTest("kill failed")

type errTest string

func (e errTest) Error() string { return string(e) }
