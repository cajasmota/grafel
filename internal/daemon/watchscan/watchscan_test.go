package watchscan

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

// managedSet builds a Managed predicate from a fixed set of repo paths. Both
// the stored set and the lookup are filepath.Clean'd so the forward-slash test
// literals match Compute's internally-cleaned repo paths on EVERY OS — on
// Windows, Clean rewrites "/work/repo-a" to "\work\repo-a", and Compute calls
// Managed with that cleaned form.
func managedSet(repos ...string) func(string) bool {
	m := map[string]bool{}
	for _, r := range repos {
		m[filepath.Clean(r)] = true
	}
	return func(p string) bool { return m[filepath.Clean(p)] }
}

// A foreign-exe watcher for a MANAGED repo is selected; a same-exe watcher and
// an UNRELATED-repo watcher are skipped.
func TestCompute_ForeignWatcherSelected_UnrelatedSkipped(t *testing.T) {
	self := "/home/u/.grafel/bin/grafel"
	plan := Compute(Deps{
		SelfExe: self,
		Managed: managedSet("/work/repo-a"),
		List: func() ([]Proc, error) {
			return []Proc{
				// foreign-version watcher for a managed repo → reap.
				{PID: 100, Exe: "/home/u/go/bin/grafel", Repo: "/work/repo-a"},
				// same-exe watcher for a managed repo → keep.
				{PID: 101, Exe: self, Repo: "/work/repo-a"},
				// foreign watcher for an UNMANAGED repo → never touched.
				{PID: 102, Exe: "/home/u/go/bin/grafel", Repo: "/other/repo-z"},
			}, nil
		},
	})
	if got, want := plan.PIDs(), []int{100}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PIDs() = %v, want %v (only the foreign managed-repo watcher)", got, want)
	}
	if len(plan.Foreign) != 1 || plan.Foreign[0] != 100 {
		t.Fatalf("Foreign = %v, want [100]", plan.Foreign)
	}
}

// Among multiple SAME-exe watchers for one managed repo, exactly one survives.
func TestCompute_DuplicateCollapse_KeepsOne(t *testing.T) {
	self := "/opt/grafel"
	plan := Compute(Deps{
		SelfExe: self,
		Managed: managedSet("/work/repo-a"),
		List: func() ([]Proc, error) {
			return []Proc{
				{PID: 300, Exe: self, Repo: "/work/repo-a"},
				{PID: 200, Exe: self, Repo: "/work/repo-a"},
				{PID: 400, Exe: self, Repo: "/work/repo-a"},
			}, nil
		},
	})
	// All three are own-exe; two are duplicates. The kept one is the own-exe
	// match with the lowest PID (200).
	if got, want := plan.PIDs(), []int{300, 400}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PIDs() = %v, want %v (200 survives)", got, want)
	}
}

// A foreign watcher is preferred for reaping; the own-exe survivor is kept even
// if it has a higher PID than the foreign one.
func TestCompute_PrefersOwnExeSurvivor(t *testing.T) {
	self := "/opt/grafel"
	plan := Compute(Deps{
		SelfExe: self,
		Managed: managedSet("/work/repo-a"),
		List: func() ([]Proc, error) {
			return []Proc{
				{PID: 10, Exe: "/stale/grafel", Repo: "/work/repo-a"}, // foreign
				{PID: 20, Exe: self, Repo: "/work/repo-a"},            // own → keep
			}, nil
		},
	})
	if got, want := plan.PIDs(), []int{10}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PIDs() = %v, want %v (foreign reaped, own kept)", got, want)
	}
}

// An unknown (empty) exe is never declared a mismatch — we don't reap a watcher
// merely because we couldn't read its executable. With a single such watcher it
// is kept.
func TestCompute_UnknownExeKept(t *testing.T) {
	plan := Compute(Deps{
		SelfExe: "/opt/grafel",
		Managed: managedSet("/work/repo-a"),
		List: func() ([]Proc, error) {
			return []Proc{{PID: 50, Exe: "", Repo: "/work/repo-a"}}, nil
		},
	})
	if pids := plan.PIDs(); len(pids) != 0 {
		t.Fatalf("PIDs() = %v, want empty (unknown exe is not a mismatch)", pids)
	}
}

// A lister error → empty plan (best-effort; never destabilize the daemon).
func TestCompute_ListErrorIsNoOp(t *testing.T) {
	plan := Compute(Deps{
		SelfExe: "/opt/grafel",
		Managed: managedSet("/work/repo-a"),
		List:    func() ([]Proc, error) { return nil, errors.New("ps failed") },
	})
	if pids := plan.PIDs(); len(pids) != 0 {
		t.Fatalf("PIDs() = %v, want empty on lister error", pids)
	}
}

// nil List or nil Managed disables the plan entirely.
func TestCompute_NilDepsAreNoOp(t *testing.T) {
	if pids := Compute(Deps{Managed: managedSet("/x")}).PIDs(); len(pids) != 0 {
		t.Fatalf("nil List: got %v, want empty", pids)
	}
	if pids := Compute(Deps{List: func() ([]Proc, error) { return []Proc{{PID: 1, Repo: "/x", Exe: "/stale"}}, nil }}).PIDs(); len(pids) != 0 {
		t.Fatalf("nil Managed: got %v, want empty", pids)
	}
}

// Empty SelfExe means we cannot establish skew → no foreign reaping (only
// duplicates, by PID, would still collapse — verified separately).
func TestCompute_EmptySelfExeNoForeignReap(t *testing.T) {
	plan := Compute(Deps{
		SelfExe: "",
		Managed: managedSet("/work/repo-a"),
		List: func() ([]Proc, error) {
			return []Proc{{PID: 1, Exe: "/anything/grafel", Repo: "/work/repo-a"}}, nil
		},
	})
	if pids := plan.PIDs(); len(pids) != 0 {
		t.Fatalf("PIDs() = %v, want empty when self-exe unknown", pids)
	}
}
