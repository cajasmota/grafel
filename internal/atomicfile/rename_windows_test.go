//go:build windows

package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// TestRenameErrRecoverable_Windows pins the errno classification: only the
// three "someone else is holding this / the flag says no" codes, plus the
// fs.ErrPermission form Go sometimes surfaces instead, are recoverable.
// Everything else must surface immediately.
func TestRenameErrRecoverable_Windows(t *testing.T) {
	for _, e := range []syscall.Errno{
		windows.ERROR_ACCESS_DENIED,
		windows.ERROR_SHARING_VIOLATION,
		windows.ERROR_LOCK_VIOLATION,
	} {
		if !renameErrRecoverable(&os.LinkError{Op: "rename", Err: e}) {
			t.Errorf("renameErrRecoverable(errno %d) = false, want true", uintptr(e))
		}
	}
	if !renameErrRecoverable(&os.LinkError{Op: "rename", Err: fs.ErrPermission}) {
		t.Error("renameErrRecoverable(fs.ErrPermission) = false, want true")
	}

	for _, err := range []error{
		nil,
		&os.LinkError{Op: "rename", Err: syscall.Errno(2)},  // ERROR_FILE_NOT_FOUND
		&os.LinkError{Op: "rename", Err: syscall.Errno(3)},  // ERROR_PATH_NOT_FOUND
		&os.LinkError{Op: "rename", Err: syscall.Errno(17)}, // ERROR_NOT_SAME_DEVICE
		errors.New("not an errno"),
	} {
		if renameErrRecoverable(err) {
			t.Errorf("renameErrRecoverable(%v) = true, want false", err)
		}
	}
}

// TestDestReadOnly_WindowsRoundTrip pins the mapping this fix depends on: Go's
// Windows os.Stat reports 0444 for FILE_ATTRIBUTE_READONLY and 0666
// otherwise, and os.Chmod with a write bit clears the attribute.
func TestDestReadOnly_WindowsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ro.json")
	if err := os.WriteFile(p, []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	if !destIsReadOnly(p) {
		t.Fatal("destIsReadOnly(0444) = false, want true on windows")
	}
	if err := setDestReadOnly(p, false); err != nil {
		t.Fatalf("setDestReadOnly(false): %v", err)
	}
	if destIsReadOnly(p) {
		t.Fatal("read-only attribute survived setDestReadOnly(false)")
	}
	if err := setDestReadOnly(p, true); err != nil {
		t.Fatalf("setDestReadOnly(true): %v", err)
	}
	if !destIsReadOnly(p) {
		t.Fatal("read-only attribute not restored by setDestReadOnly(true)")
	}
	// Leave it writable so t.TempDir cleanup can remove it.
	_ = setDestReadOnly(p, false)
}

// TestDestReadOnly_MissingDestination: WriteFile's common case is a
// destination that does not exist yet. The probe must not claim read-only for
// a file that is not there.
func TestDestReadOnly_MissingDestination(t *testing.T) {
	if destIsReadOnly(filepath.Join(t.TempDir(), "absent")) {
		t.Fatal("destIsReadOnly(missing) = true, want false")
	}
}

// TestDestReadOnly_Directory: renameOver probes the destination path, which
// may be a directory (WriteFile over a directory is an error path the suite
// exercises). A directory must never be treated as a read-only file to clear.
func TestDestReadOnly_Directory(t *testing.T) {
	if destIsReadOnly(t.TempDir()) {
		t.Fatal("destIsReadOnly(directory) = true, want false")
	}
}
