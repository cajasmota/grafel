package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
)

// realExe creates an actual file and returns its path. Required for the same
// reason as in internal/daemon/watchscan (#6187): version skew is only declared
// between two paths that BOTH resolve on disk, so a foreign-watcher fixture
// built from string literals silently stops being a foreign-watcher fixture.
func realExe(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture premise broken: %s must exist: %v", p, err)
	}
	return p
}

// twoRealExes builds a self/foreign pair and asserts the premise that they are
// two distinct, existing binaries — otherwise "foreign" would not be provable
// and the sweep under test would correctly reap nothing, passing the
// no-unload assertions for entirely the wrong reason.
func twoRealExes(t *testing.T) (self, foreign string) {
	t.Helper()
	root := t.TempDir()
	self = realExe(t, filepath.Join(root, "installed"), "grafel")
	foreign = realExe(t, filepath.Join(root, "gopath"), "grafel")
	if filepath.Clean(self) == filepath.Clean(foreign) {
		t.Fatalf("fixture premise broken: %q and %q must differ", self, foreign)
	}
	fs, _ := os.Stat(self)
	ff, _ := os.Stat(foreign)
	if os.SameFile(fs, ff) {
		t.Fatalf("fixture premise broken: self and foreign must be different files")
	}
	return self, foreign
}

// #6187, second half. When the sweep reaps a foreign watcher and NO watcher is
// left running for that repo, the OS unit is deregistered.
//
// Killing the process alone leaves incoherent on-disk state: the plist says the
// job is loaded, nothing is running, and since #6179 the watcher exits 0 so
// launchd will never bring it back. Booting the unit out makes the two agree,
// so `grafel doctor` / `grafel start` see an absent watcher instead of a
// phantom loaded one.
func TestReaper_foreignReapUnloadsWatcherUnit(t *testing.T) {
	self, foreign := twoRealExes(t)

	var killed []int
	var unloaded []string
	r := NewReaper(ReaperConfig{
		SelfExe:     func() (string, error) { return self, nil },
		ManagedRepo: managedClean("/work/repo-a"),
		ListWatchProcs: func() ([]process.WatchProc, error) {
			return []process.WatchProc{
				{PID: 100, Exe: foreign, Repo: "/work/repo-a"},
			}, nil
		},
		KillWatchProc:     func(pid int) error { killed = append(killed, pid); return nil },
		UnloadWatcherUnit: func(repo string) { unloaded = append(unloaded, repo) },
	})

	res := r.Sweep()
	if res.ForeignWatchersReaped != 1 || !reflect.DeepEqual(killed, []int{100}) {
		t.Fatalf("reaped=%d killed=%v, want 1 / [100]", res.ForeignWatchersReaped, killed)
	}
	if want := []string{filepath.Clean("/work/repo-a")}; !reflect.DeepEqual(unloaded, want) {
		t.Fatalf("unloaded = %v, want %v", unloaded, want)
	}
}

// A repo that still has a live watcher after the sweep must NOT have its unit
// booted out. This is the case that makes an unconditional bootout dangerous: a
// hand-started stale-binary watcher is reaped while the launchd-owned one is
// perfectly healthy, and deregistering the unit would take the healthy watcher
// down with it. Nothing is incoherent here — the plist says loaded and a
// watcher IS running — so there is nothing to repair.
func TestReaper_foreignReapKeepsUnitWhenWatcherSurvives(t *testing.T) {
	self, foreign := twoRealExes(t)

	var killed []int
	r := NewReaper(ReaperConfig{
		SelfExe:     func() (string, error) { return self, nil },
		ManagedRepo: managedClean("/work/repo-a"),
		ListWatchProcs: func() ([]process.WatchProc, error) {
			return []process.WatchProc{
				{PID: 100, Exe: foreign, Repo: "/work/repo-a"}, // reaped
				{PID: 101, Exe: self, Repo: "/work/repo-a"},    // survives
			}, nil
		},
		KillWatchProc: func(pid int) error { killed = append(killed, pid); return nil },
		UnloadWatcherUnit: func(repo string) {
			t.Fatalf("must not unload %s: a watcher for it is still running", repo)
		},
	})

	res := r.Sweep()
	if res.ForeignWatchersReaped != 1 || !reflect.DeepEqual(killed, []int{100}) {
		t.Fatalf("reaped=%d killed=%v, want 1 / [100] (the premise: exactly one reap, one survivor)", res.ForeignWatchersReaped, killed)
	}
}

// A DUPLICATE-only reap never unloads. Duplicates are collapsed precisely
// because one watcher for the repo remains, so the unit is doing its job and
// deregistering it would be a plain regression.
func TestReaper_duplicateReapDoesNotUnloadUnit(t *testing.T) {
	self, _ := twoRealExes(t)

	var killed []int
	r := NewReaper(ReaperConfig{
		SelfExe:     func() (string, error) { return self, nil },
		ManagedRepo: managedClean("/work/repo-a"),
		ListWatchProcs: func() ([]process.WatchProc, error) {
			return []process.WatchProc{
				{PID: 200, Exe: self, Repo: "/work/repo-a"},
				{PID: 300, Exe: self, Repo: "/work/repo-a"},
				{PID: 400, Exe: self, Repo: "/work/repo-a"},
			}, nil
		},
		KillWatchProc: func(pid int) error { killed = append(killed, pid); return nil },
		UnloadWatcherUnit: func(repo string) {
			t.Fatalf("must not unload %s on a duplicate collapse", repo)
		},
	})

	res := r.Sweep()
	sort.Ints(killed)
	if res.ForeignWatchersReaped != 2 || !reflect.DeepEqual(killed, []int{300, 400}) {
		t.Fatalf("reaped=%d killed=%v, want 2 / [300 400] (the premise: a duplicate collapse happened)", res.ForeignWatchersReaped, killed)
	}
}

// If the SIGTERM failed, the watcher is still running. Deregistering the unit
// then would produce exactly the incoherence this half exists to remove, only
// inverted: launchd no longer owns a process that is still alive.
func TestReaper_failedKillDoesNotUnloadUnit(t *testing.T) {
	self, foreign := twoRealExes(t)

	attempted := 0
	r := NewReaper(ReaperConfig{
		SelfExe:     func() (string, error) { return self, nil },
		ManagedRepo: managedClean("/work/repo-a"),
		ListWatchProcs: func() ([]process.WatchProc, error) {
			return []process.WatchProc{
				{PID: 100, Exe: foreign, Repo: "/work/repo-a"},
			}, nil
		},
		KillWatchProc: func(int) error { attempted++; return errors.New("EPERM") },
		UnloadWatcherUnit: func(repo string) {
			t.Fatalf("must not unload %s: its watcher could not be killed", repo)
		},
	})

	res := r.Sweep()
	if attempted != 1 {
		t.Fatalf("kill attempts = %d, want 1 (the premise: a reap was attempted at all)", attempted)
	}
	if res.ForeignWatchersReaped != 0 {
		t.Fatalf("ForeignWatchersReaped = %d, want 0 when the kill failed", res.ForeignWatchersReaped)
	}
}

// A nil UnloadWatcherUnit must leave the sweep working exactly as before —
// every caller that does not wire the hook (tests, non-service deployments)
// still reaps.
func TestReaper_nilUnloadHookIsHarmless(t *testing.T) {
	self, foreign := twoRealExes(t)

	var killed []int
	r := NewReaper(ReaperConfig{
		SelfExe:     func() (string, error) { return self, nil },
		ManagedRepo: managedClean("/work/repo-a"),
		ListWatchProcs: func() ([]process.WatchProc, error) {
			return []process.WatchProc{{PID: 100, Exe: foreign, Repo: "/work/repo-a"}}, nil
		},
		KillWatchProc: func(pid int) error { killed = append(killed, pid); return nil },
	})
	if res := r.Sweep(); res.ForeignWatchersReaped != 1 || !reflect.DeepEqual(killed, []int{100}) {
		t.Fatalf("reaped=%d killed=%v, want 1 / [100]", res.ForeignWatchersReaped, killed)
	}
}
