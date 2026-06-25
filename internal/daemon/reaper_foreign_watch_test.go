package daemon

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
)

// managedClean returns a ManagedRepo predicate matching cleaned paths, so the
// test fixtures (forward-slash literals) match watchscan's filepath.Clean
// normalization on EVERY OS — including Windows, where Clean rewrites the
// separators. This mirrors the production makeManagedRepoPredicate, which also
// compares cleaned-absolute paths on both sides.
func managedClean(repos ...string) func(string) bool {
	m := map[string]bool{}
	for _, r := range repos {
		m[filepath.Clean(r)] = true
	}
	return func(p string) bool { return m[filepath.Clean(p)] }
}

// TestReaper_sweepForeignWatchers verifies the #5632 wiring on ALL platforms by
// injecting a FAKE process list through the ListWatchProcs seam (so the unix-
// only live enumeration is bypassed): a foreign-version `grafel watch` process
// for a MANAGED repo is SIGTERM'd, while a same-exe watcher and a watcher for an
// UNMANAGED repo are left alone. Kills are observed via the injected
// KillWatchProc so no real process is touched.
func TestReaper_sweepForeignWatchers(t *testing.T) {
	const self = "/home/u/.grafel/bin/grafel"

	var killed []int
	r := NewReaper(ReaperConfig{
		SelfExe:     func() (string, error) { return self, nil },
		ManagedRepo: managedClean("/work/repo-a"),
		ListWatchProcs: func() ([]process.WatchProc, error) {
			return []process.WatchProc{
				{PID: 100, Exe: "/home/u/go/bin/grafel", Repo: "/work/repo-a"},  // foreign, managed → reap
				{PID: 101, Exe: self, Repo: "/work/repo-a"},                     // own, managed → keep
				{PID: 102, Exe: "/home/u/go/bin/grafel", Repo: "/other/repo-z"}, // foreign, UNMANAGED → keep
			}, nil
		},
		KillWatchProc: func(pid int) error { killed = append(killed, pid); return nil },
	})

	res := r.Sweep()
	if res.ForeignWatchersReaped != 1 {
		t.Fatalf("ForeignWatchersReaped = %d, want 1", res.ForeignWatchersReaped)
	}
	if !reflect.DeepEqual(killed, []int{100}) {
		t.Fatalf("killed = %v, want [100] (only the foreign managed-repo watcher)", killed)
	}
}

// Duplicate same-exe watchers for one managed repo are collapsed to one. Driven
// through the ListWatchProcs seam so it runs on every OS.
func TestReaper_sweepForeignWatchers_DuplicateCollapse(t *testing.T) {
	const self = "/opt/grafel"
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
	})
	res := r.Sweep()
	sort.Ints(killed)
	if res.ForeignWatchersReaped != 2 || !reflect.DeepEqual(killed, []int{300, 400}) {
		t.Fatalf("reaped=%d killed=%v, want 2 killed=[300 400] (200 survives)", res.ForeignWatchersReaped, killed)
	}
}

// With nil ManagedRepo the foreign-watcher sweep is disabled entirely.
func TestReaper_foreignSweepDisabledWhenNoManaged(t *testing.T) {
	called := false
	r := NewReaper(ReaperConfig{
		ListWatchProcs: func() ([]process.WatchProc, error) {
			called = true
			return []process.WatchProc{{PID: 1, Exe: "/stale", Repo: "/x"}}, nil
		},
		KillWatchProc: func(int) error { t.Fatal("must not kill when sweep disabled"); return nil },
	})
	res := r.Sweep()
	if res.ForeignWatchersReaped != 0 {
		t.Fatalf("ForeignWatchersReaped = %d, want 0", res.ForeignWatchersReaped)
	}
	if called {
		t.Fatal("lister must not be invoked when ManagedRepo is nil")
	}
}
