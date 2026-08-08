package atomicfile_test

// writethrough_test.go covers the symlink-following, mode-preserving sibling of
// WriteFile introduced for #6246 (and retrofitted onto #6240's mcpreg fix).
//
// WriteFile's own contract is unchanged and is asserted elsewhere: a symlink at
// path is REPLACED and perm is applied verbatim, both deliberately. These tests
// exist to make sure the new function is the exact inverse on both axes, because
// "which of the two do I want here" is the question every one of the six call
// sites has to answer.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/atomicfile"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// requireSymlinks skips when the platform cannot create a symlink at all. A
// capability PROBE, not a runtime.GOOS check, so a Windows runner with the
// privilege still exercises these.
func requireSymlinks(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
}

func seed(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("old\n"), perm); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod seed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o666) })
}

// ── ResolveWriteTarget ───────────────────────────────────────────────────────

func TestResolveWriteTarget_PlainPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	seed(t, path, 0o600)
	got, err := atomicfile.ResolveWriteTarget(path)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != path {
		t.Fatalf("resolved %q, want %q", got, path)
	}
}

func TestResolveWriteTarget_AbsentPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	got, err := atomicfile.ResolveWriteTarget(path)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != path {
		t.Fatalf("resolved %q, want %q", got, path)
	}
}

// TestResolveWriteTarget_RelativeLink: a relative link target is resolved
// against the LINK's directory, not the process working directory. Getting this
// wrong writes into whatever directory the CLI happened to be run from.
func TestResolveWriteTarget_RelativeLink(t *testing.T) {
	requireSymlinks(t)

	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	seed(t, target, 0o600)
	link := filepath.Join(dir, "link")
	if err := os.Symlink("real", link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	got, err := atomicfile.ResolveWriteTarget(link)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != target {
		t.Fatalf("resolved %q, want %q", got, target)
	}
}

// TestResolveWriteTarget_DanglingLink: the final target is returned even though
// it does not exist, which is what a create must use.
func TestResolveWriteTarget_DanglingLink(t *testing.T) {
	requireSymlinks(t)

	dir := t.TempDir()
	target := filepath.Join(dir, "nothing-here")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	got, err := atomicfile.ResolveWriteTarget(link)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != target {
		t.Fatalf("resolved %q, want %q", got, target)
	}
}

func TestResolveWriteTarget_CycleIsAnError(t *testing.T) {
	requireSymlinks(t)

	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.Symlink(b, a); err != nil {
		t.Fatalf("symlink a: %v", err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatalf("symlink b: %v", err)
	}
	got, err := atomicfile.ResolveWriteTarget(a)
	if err == nil {
		t.Fatalf("a cycle resolved to %q instead of erroring", got)
	}
	if !errors.Is(err, atomicfile.ErrSymlinkChain) {
		t.Fatalf("error does not classify as ErrSymlinkChain: %v", err)
	}
	if got != "" {
		t.Fatalf("a failed resolution returned a usable-looking path %q; a caller that "+
			"ignores the error would rename over it and flatten a link", got)
	}
}

// TestResolveWriteTarget_ChainWithinTheKernelLimitStillResolves pins the budget
// at Linux's MAXSYMLINKS (40) rather than macOS's 32: a 39-hop chain the Linux
// kernel follows must not be rejected here. A local macOS probe cannot see this
// — macOS answers ELOOP first — so it is a Linux-only regression waiting on a
// three-OS CI.
func TestResolveWriteTarget_ChainWithinTheKernelLimitStillResolves(t *testing.T) {
	requireSymlinks(t)

	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	seed(t, target, 0o600)

	prev := target
	var head string
	for i := 0; i < 39; i++ {
		link := filepath.Join(dir, fmt.Sprintf("l%02d", i))
		if err := os.Symlink(prev, link); err != nil {
			t.Skipf("cannot build a 39-link chain here: %v", err)
		}
		prev = link
		head = link
	}
	got, err := atomicfile.ResolveWriteTarget(head)
	if err != nil {
		t.Fatalf("a 39-hop chain (within Linux MAXSYMLINKS=40) failed: %v", err)
	}
	if got != target {
		t.Fatalf("resolved %q, want %q", got, target)
	}
}

func TestResolveWriteTarget_OverlongChainIsAnError(t *testing.T) {
	requireSymlinks(t)

	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	seed(t, target, 0o600)

	prev := target
	var head string
	for i := 0; i < atomicfile.MaxSymlinkHops+5; i++ {
		link := filepath.Join(dir, fmt.Sprintf("l%03d", i))
		if err := os.Symlink(prev, link); err != nil {
			t.Skipf("cannot build a long chain here: %v", err)
		}
		prev = link
		head = link
	}
	if _, err := atomicfile.ResolveWriteTarget(head); !errors.Is(err, atomicfile.ErrSymlinkChain) {
		t.Fatalf("an over-budget chain did not report ErrSymlinkChain: %v", err)
	}
}

// ── ExistingPerm ─────────────────────────────────────────────────────────────

func TestExistingPerm(t *testing.T) {
	dir := t.TempDir()
	there := filepath.Join(dir, "there")
	seed(t, there, 0o444)

	if got := atomicfile.ExistingPerm(there, 0o600); got != 0o444 {
		t.Errorf("existing file: got %04o, want 0444", got)
	}
	if got := atomicfile.ExistingPerm(filepath.Join(dir, "absent"), 0o600); got != 0o600 {
		t.Errorf("absent file: got %04o, want the ifAbsent 0600", got)
	}
}

// TestExistingPerm_ReportsTheTargetNotTheLink: Stat, not Lstat. A symlink's own
// mode is 0777 on Linux and is not the mode any content is stored at, so an
// Lstat here would widen every symlinked destination to 0777 — a worse version
// of the bug being fixed.
func TestExistingPerm_ReportsTheTargetNotTheLink(t *testing.T) {
	requireSymlinks(t)

	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	seed(t, target, 0o600)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if got := atomicfile.ExistingPerm(link, 0o644); got != 0o600 {
		t.Errorf("got %04o, want the target's 0600", got)
	}
}

// ── WriteThrough ─────────────────────────────────────────────────────────────

func TestWriteThrough_PreservesExistingMode(t *testing.T) {
	for _, perm := range []os.FileMode{0o600, 0o444} {
		t.Run(perm.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "f")
			seed(t, path, perm)
			if err := atomicfile.WriteThrough(path, []byte("new\n"), 0o644); err != nil {
				t.Fatalf("WriteThrough: %v", err)
			}
			testsupport.AssertPerm(t, path, perm)
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if string(b) != "new\n" {
				t.Errorf("content = %q, want %q", b, "new\n")
			}
		})
	}
}

func TestWriteThrough_UsesIfAbsentPermOnCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := atomicfile.WriteThrough(path, []byte("new\n"), 0o600); err != nil {
		t.Fatalf("WriteThrough: %v", err)
	}
	testsupport.AssertPerm(t, path, 0o600)
}

func TestWriteThrough_WritesThroughSymlink(t *testing.T) {
	requireSymlinks(t)

	target := filepath.Join(t.TempDir(), "real")
	link := filepath.Join(t.TempDir(), "link")
	seed(t, target, 0o600)
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := atomicfile.WriteThrough(link, []byte("new\n"), 0o644); err != nil {
		t.Fatalf("WriteThrough: %v", err)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced by a regular file")
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(b) != "new\n" {
		t.Errorf("target content = %q, want %q", b, "new\n")
	}
	testsupport.AssertPerm(t, target, 0o600)
}

// TestWriteThrough_IsTheInverseOfWriteFile states the contrast the two functions
// exist to draw, in one place, so a reader choosing between them does not have
// to infer it from two package docs.
func TestWriteThrough_IsTheInverseOfWriteFile(t *testing.T) {
	requireSymlinks(t)

	newCase := func(t *testing.T) (link, target string) {
		t.Helper()
		target = filepath.Join(t.TempDir(), "real")
		link = filepath.Join(t.TempDir(), "link")
		seed(t, target, 0o600)
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		return link, target
	}

	t.Run("WriteFile replaces the link and applies perm verbatim", func(t *testing.T) {
		link, _ := newCase(t)
		if err := atomicfile.WriteFile(link, []byte("x\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		fi, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("lstat: %v", err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("WriteFile no longer replaces a symlink — its documented contract changed")
		}
		testsupport.AssertPerm(t, link, 0o644)
	})

	t.Run("WriteThrough follows the link and keeps its mode", func(t *testing.T) {
		link, target := newCase(t)
		if err := atomicfile.WriteThrough(link, []byte("x\n"), 0o644); err != nil {
			t.Fatalf("WriteThrough: %v", err)
		}
		fi, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("lstat: %v", err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("WriteThrough replaced the link")
		}
		testsupport.AssertPerm(t, target, 0o600)
	})
}

func TestWriteThrough_CycleIsAnError(t *testing.T) {
	requireSymlinks(t)

	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.Symlink(b, a); err != nil {
		t.Fatalf("symlink a: %v", err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatalf("symlink b: %v", err)
	}
	if err := atomicfile.WriteThrough(a, []byte("x\n"), 0o600); !errors.Is(err, atomicfile.ErrSymlinkChain) {
		t.Fatalf("cycle did not report ErrSymlinkChain: %v", err)
	}
	fi, err := os.Lstat(a)
	if err != nil {
		t.Fatalf("lstat a: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the cycle flattened the link into a regular file")
	}
}
