//go:build !windows

// The symlink fixtures below need os.Symlink to succeed unprivileged, which is
// not the case on Windows. The exclusion is a BUILD TAG, not runtime.GOOS plus
// t.Skip: a runtime skip still has to compile, and #6218 showed that a test
// guarded only at execution time can break the build on the platforms it was
// meant to spare. The resolvable/unresolvable behaviour these tests share with
// every OS is covered platform-agnostically in exe_resolve_6187_test.go.

package watchscan

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// symlinkExe creates link -> target and returns link. It asserts the link
// genuinely resolves to target's inode, so a fixture that quietly produced a
// dangling or wrong link cannot masquerade as coverage.
func symlinkExe(t *testing.T, target, link string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(link), err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink %s -> %s: %v", link, target, err)
	}
	mustBeSameFile(t, target, link)
	return link
}

// #6187 (direction 1 of 2), the exact production shape: grafel is invoked
// through a shim — a Homebrew symlink in /opt/homebrew/bin, or a plist BinPath
// recorded from a different install prefix — so the daemon's os.Executable()
// and the watcher's executable are DIFFERENT STRINGS naming the SAME binary.
// Before the fix, filepath.Clean called that version skew and every launchd
// watcher for every managed repo was SIGTERMed on each 5-minute sweep; after
// #6179 the first reap was final and silent.
func TestCompute_SymlinkedShimIsNotForeign(t *testing.T) {
	root := t.TempDir()
	real := writeExe(t, filepath.Join(root, "Cellar", "grafel", "bin"), "grafel")
	shim := symlinkExe(t, real, filepath.Join(root, "brew", "bin", "grafel"))

	// Premises: two different spellings, one binary.
	mustExist(t, real)
	mustExist(t, shim)
	mustDifferTextually(t, real, shim)
	mustBeSameFile(t, real, shim)

	plan := Compute(Deps{
		// The daemon was spawned via the shim; the watcher's plist BinPath
		// records the resolved Cellar path (or vice versa — both directions
		// asserted below).
		SelfExe: shim,
		Managed: managedSet("/work/repo-a"),
		List: func() ([]Proc, error) {
			return []Proc{{PID: 100, Exe: real, Repo: "/work/repo-a"}}, nil
		},
	})
	if pids := plan.PIDs(); len(pids) != 0 {
		t.Fatalf("PIDs() = %v, want empty (shim and target are the same binary)", pids)
	}

	// Symmetry: the same must hold with the roles swapped.
	plan = Compute(Deps{
		SelfExe: real,
		Managed: managedSet("/work/repo-a"),
		List: func() ([]Proc, error) {
			return []Proc{{PID: 100, Exe: shim, Repo: "/work/repo-a"}}, nil
		},
	})
	if pids := plan.PIDs(); len(pids) != 0 {
		t.Fatalf("PIDs() = %v, want empty (comparison must be symmetric)", pids)
	}
}

// A symlinked-but-still-ours watcher must also be RECOGNISED as ours when
// picking the survivor among duplicates — otherwise the reaper keeps an
// unidentifiable watcher and reaps the one it can vouch for. PID order is
// deliberately against the desired outcome: the lowest-PID fallback would keep
// 10, so keeping 20 can only come from resolving the symlink.
func TestCompute_SymlinkedOwnExePreferredAsSurvivor(t *testing.T) {
	root := t.TempDir()
	real := writeExe(t, filepath.Join(root, "Cellar", "grafel", "bin"), "grafel")
	shim := symlinkExe(t, real, filepath.Join(root, "brew", "bin", "grafel"))
	mustDifferTextually(t, real, shim)
	mustBeSameFile(t, real, shim)

	plan := Compute(Deps{
		SelfExe: shim,
		Managed: managedSet("/work/repo-a"),
		List: func() ([]Proc, error) {
			return []Proc{
				{PID: 10, Exe: "", Repo: "/work/repo-a"},   // exe unreadable → not skew, but not vouchable
				{PID: 20, Exe: real, Repo: "/work/repo-a"}, // same binary via the shim → keep
			}, nil
		},
	})
	if got, want := plan.PIDs(), []int{10}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PIDs() = %v, want %v (the symlink-identified own-exe watcher survives)", got, want)
	}
	if len(plan.Foreign) != 0 {
		t.Fatalf("Foreign = %v, want empty (this is a duplicate collapse, not skew)", plan.Foreign)
	}
}
