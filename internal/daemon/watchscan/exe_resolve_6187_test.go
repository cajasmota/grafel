package watchscan

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeExe creates a real, distinct file under dir and returns its absolute
// path. Real files matter: after #6187 the reaper only declares version skew
// when BOTH executable paths resolve on disk, so a fixture built from string
// literals that name nothing would make every "genuinely foreign" assertion
// vacuously pass through the not-resolvable escape hatch instead of through the
// comparison under test.
func writeExe(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// mustExist asserts the test's own premise that p names something on disk. A
// fixture that silently failed to create the file would otherwise be read as
// "not resolvable" and pass the not-reaped assertions for the wrong reason.
func mustExist(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture premise broken: %s must exist on disk: %v", p, err)
	}
}

// mustNotExist asserts the converse premise for the unresolvable cases.
func mustNotExist(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture premise broken: %s must NOT exist (stat err = %v)", p, err)
	}
}

// mustDifferTextually asserts the two paths are not the same string after
// Clean. Every test below exists to probe what happens BEYOND the lexical
// comparison; if the fixture accidentally produced two equal strings, the old
// filepath.Clean path would short-circuit and the assertion would say nothing.
func mustDifferTextually(t *testing.T, a, b string) {
	t.Helper()
	if filepath.Clean(a) == filepath.Clean(b) {
		t.Fatalf("fixture premise broken: %q and %q must differ textually after Clean", a, b)
	}
}

// mustBeSameFile asserts two paths reach the same inode, so a symlink fixture
// that silently failed to link (or linked to the wrong target) cannot pass as
// coverage of "same binary, different spelling".
func mustBeSameFile(t *testing.T, a, b string) {
	t.Helper()
	fa, err := os.Stat(a)
	if err != nil {
		t.Fatalf("fixture premise broken: stat %s: %v", a, err)
	}
	fb, err := os.Stat(b)
	if err != nil {
		t.Fatalf("fixture premise broken: stat %s: %v", b, err)
	}
	if !os.SameFile(fa, fb) {
		t.Fatalf("fixture premise broken: %s and %s must be the SAME file", a, b)
	}
}

// mustBeDifferentFiles is the converse premise for the genuine-skew fixture.
func mustBeDifferentFiles(t *testing.T, a, b string) {
	t.Helper()
	fa, err := os.Stat(a)
	if err != nil {
		t.Fatalf("fixture premise broken: stat %s: %v", a, err)
	}
	fb, err := os.Stat(b)
	if err != nil {
		t.Fatalf("fixture premise broken: stat %s: %v", b, err)
	}
	if os.SameFile(fa, fb) {
		t.Fatalf("fixture premise broken: %s and %s must be DIFFERENT files", a, b)
	}
}

// #6187 (direction 2 of 2): a genuinely foreign binary — a different file, both
// resolvable — is still reaped. Without this the symlink fix would be
// unfalsifiable: "never reap anything" also passes the not-reaped tests.
func TestCompute_GenuinelyForeignExeStillReaped(t *testing.T) {
	root := t.TempDir()
	self := writeExe(t, filepath.Join(root, "installed", "bin"), "grafel")
	stale := writeExe(t, filepath.Join(root, "gopath", "bin"), "grafel")

	mustExist(t, self)
	mustExist(t, stale)
	mustDifferTextually(t, self, stale)
	mustBeDifferentFiles(t, self, stale)

	plan := Compute(Deps{
		SelfExe: self,
		Managed: managedSet("/work/repo-a"),
		List: func() ([]Proc, error) {
			return []Proc{{PID: 100, Exe: stale, Repo: "/work/repo-a"}}, nil
		},
	})
	if got, want := plan.PIDs(), []int{100}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PIDs() = %v, want %v (a different, existing binary is genuine version skew)", got, want)
	}
	if len(plan.Foreign) != 1 || plan.Foreign[0] != 100 {
		t.Fatalf("Foreign = %v, want [100]", plan.Foreign)
	}
}

// #6187: the watcher's recorded executable no longer exists — the ordinary
// state of a stale plist whose BinPath pointed at an install prefix that has
// since been removed or moved. Skew is UNKNOWABLE there, and the fail-safe
// verdict is "not foreign": misclassifying kills the user's watchers
// permanently (#6179 removed the respawn), whereas declining to reap a
// genuinely foreign watcher is recoverable and loud.
func TestCompute_UnresolvableWatcherExeNotReaped(t *testing.T) {
	root := t.TempDir()
	self := writeExe(t, filepath.Join(root, "installed", "bin"), "grafel")
	gone := filepath.Join(root, "removed", "bin", "grafel")

	mustExist(t, self)
	mustNotExist(t, gone)
	mustDifferTextually(t, self, gone)

	plan := Compute(Deps{
		SelfExe: self,
		Managed: managedSet("/work/repo-a"),
		List: func() ([]Proc, error) {
			return []Proc{{PID: 100, Exe: gone, Repo: "/work/repo-a"}}, nil
		},
	})
	if pids := plan.PIDs(); len(pids) != 0 {
		t.Fatalf("PIDs() = %v, want empty (an unresolvable watcher exe is not provable skew)", pids)
	}
}

// #6187, the mirror case: the DAEMON's own executable is the one that no longer
// resolves (it was replaced in place by an upgrade, or read from a stale
// pidfile-era path). Same verdict, same reason — skew cannot be established
// from a path that names nothing.
func TestCompute_UnresolvableSelfExeNotReaped(t *testing.T) {
	root := t.TempDir()
	live := writeExe(t, filepath.Join(root, "installed", "bin"), "grafel")
	goneSelf := filepath.Join(root, "removed", "bin", "grafel")

	mustExist(t, live)
	mustNotExist(t, goneSelf)
	mustDifferTextually(t, live, goneSelf)

	plan := Compute(Deps{
		SelfExe: goneSelf,
		Managed: managedSet("/work/repo-a"),
		List: func() ([]Proc, error) {
			return []Proc{{PID: 100, Exe: live, Repo: "/work/repo-a"}}, nil
		},
	})
	if pids := plan.PIDs(); len(pids) != 0 {
		t.Fatalf("PIDs() = %v, want empty (an unresolvable SELF exe is not provable skew)", pids)
	}
}
