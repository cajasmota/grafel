package install

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// errLocked is a stand-in for the Windows ERROR_SHARING_VIOLATION /
// ERROR_ACCESS_DENIED the OS raises while another process (or the running exe)
// holds the install binary open. isTestLock classifies it.
var errLocked = errors.New("the process cannot access the file because it is being used by another process")

func isTestLock(err error) bool { return errors.Is(err, errLocked) }

// withFastRetries shrinks the retry delay so the bounded loop runs instantly in
// tests, restoring the originals afterwards. It also restores the osRemove /
// osRename seams.
func withFastRetries(t *testing.T) {
	t.Helper()
	origDelay, origRetries := removeBinaryRetryDelay, removeBinaryRetries
	origRemove, origRename := osRemove, osRename
	removeBinaryRetryDelay = time.Millisecond
	removeBinaryRetries = 20
	t.Cleanup(func() {
		removeBinaryRetryDelay = origDelay
		removeBinaryRetries = origRetries
		osRemove = origRemove
		osRename = origRename
	})
}

// TestFreeLockedBinaryPath_TransientLockReleased reproduces the acceptance
// regression (run 27847397341): the install binary is held by the just-stopped
// daemon for a few beats (os.Remove AND os.Rename both fail with a sharing
// violation), then the handle is released. freeLockedBinaryPath must ride out
// the lag, rename the canonical path aside, and report success — so the
// canonical <bin>\grafel.exe is genuinely gone.
func TestFreeLockedBinaryPath_TransientLockReleased(t *testing.T) {
	withFastRetries(t)

	dir := t.TempDir()
	bin := filepath.Join(dir, "grafel.exe")
	if err := os.WriteFile(bin, []byte("locked binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	aside := renamedAsidePath(bin, 4242)

	// Hold the lock for the first 3 attempts, then let the real rename through.
	lockedAttempts := 3
	calls := 0
	osRemove = func(p string) error { return errLocked } // daemon still holds it
	osRename = func(oldp, newp string) error {
		calls++
		if calls <= lockedAttempts {
			return errLocked
		}
		return os.Rename(oldp, newp)
	}

	asideUsed, err := freeLockedBinaryPath(bin, aside, errLocked, isTestLock)
	if err != nil {
		t.Fatalf("freeLockedBinaryPath: unexpected error: %v", err)
	}
	if !asideUsed {
		t.Fatalf("asideUsed = false, want true (path freed via rename-aside)")
	}
	// The canonical install path MUST be gone — the heart of the assertion.
	if _, serr := os.Stat(bin); !os.IsNotExist(serr) {
		t.Fatalf("canonical binary still present after free; stat err = %v", serr)
	}
	if _, serr := os.Stat(aside); serr != nil {
		t.Fatalf("renamed-aside orphan missing: %v", serr)
	}
}

// TestFreeLockedBinaryPath_LockClearsForRemove covers the path where the lock
// clears enough for a retried os.Remove to delete the file outright (no orphan
// left behind) — asideUsed must be false.
func TestFreeLockedBinaryPath_LockClearsForRemove(t *testing.T) {
	withFastRetries(t)

	dir := t.TempDir()
	bin := filepath.Join(dir, "grafel.exe")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	aside := renamedAsidePath(bin, 7)

	removeCalls := 0
	osRemove = func(p string) error {
		removeCalls++
		if removeCalls < 3 {
			return errLocked
		}
		return os.Remove(p) // handle finally released
	}
	osRename = func(oldp, newp string) error { return errLocked } // rename never clears

	asideUsed, err := freeLockedBinaryPath(bin, aside, errLocked, isTestLock)
	if err != nil {
		t.Fatalf("freeLockedBinaryPath: unexpected error: %v", err)
	}
	if asideUsed {
		t.Fatalf("asideUsed = true, want false (cleared via os.Remove, no orphan)")
	}
	if _, serr := os.Stat(bin); !os.IsNotExist(serr) {
		t.Fatalf("canonical binary still present; stat err = %v", serr)
	}
}

// TestFreeLockedBinaryPath_PermanentLockSurfacesError ensures we do NOT weaken
// the guarantee: when the lock never clears, freeLockedBinaryPath returns an
// error so the uninstall reports the genuine failure (binary truly stuck).
func TestFreeLockedBinaryPath_PermanentLockSurfacesError(t *testing.T) {
	withFastRetries(t)
	removeBinaryRetries = 4 // keep the test snappy

	dir := t.TempDir()
	bin := filepath.Join(dir, "grafel.exe")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	aside := renamedAsidePath(bin, 9)

	osRemove = func(p string) error { return errLocked }
	osRename = func(oldp, newp string) error { return errLocked }

	asideUsed, err := freeLockedBinaryPath(bin, aside, errLocked, isTestLock)
	if err == nil {
		t.Fatalf("freeLockedBinaryPath: expected an error on a permanent lock, got nil (asideUsed=%v)", asideUsed)
	}
	// The binary genuinely remains — we must NOT have claimed success.
	if _, serr := os.Stat(bin); serr != nil {
		t.Fatalf("binary unexpectedly gone on permanent lock: %v", serr)
	}
}

// TestFreeLockedBinaryPath_NonLockErrorNotRetried ensures a non-lock error from
// os.Rename is surfaced immediately rather than retried for the full budget
// (only sharing/lock errors are transient).
func TestFreeLockedBinaryPath_NonLockErrorNotRetried(t *testing.T) {
	withFastRetries(t)

	dir := t.TempDir()
	bin := filepath.Join(dir, "grafel.exe")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	aside := renamedAsidePath(bin, 11)

	osRemove = func(p string) error { return errLocked }
	renameCalls := 0
	bogus := errors.New("some non-lock rename failure")
	osRename = func(oldp, newp string) error {
		renameCalls++
		return bogus
	}

	_, err := freeLockedBinaryPath(bin, aside, errLocked, isTestLock)
	if err == nil {
		t.Fatalf("expected error from non-lock rename failure")
	}
	if renameCalls != 1 {
		t.Fatalf("rename retried %d times on a non-lock error; want 1 (no retry)", renameCalls)
	}
}
