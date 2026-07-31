//go:build !windows

package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestPlatformPrimitives_UnixAreInert pins the zero-behaviour-change promise on
// the platforms grafel is developed on. POSIX rename(2) replaces the
// destination regardless of the destination file's own mode and regardless of
// other open handles, so none of the recovery in renameOver may ever engage
// here: WriteFile must remain exactly one os.Rename.
func TestPlatformPrimitives_UnixAreInert(t *testing.T) {
	for _, err := range []error{
		syscall.EACCES, syscall.EPERM, syscall.ENOENT, errors.New("x"), os.ErrPermission,
	} {
		if renameErrRecoverable(err) {
			t.Fatalf("renameErrRecoverable(%v) = true, want false on unix", err)
		}
	}

	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	if err := os.WriteFile(ro, []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	if destIsReadOnly(ro) {
		t.Fatal("destIsReadOnly(0444 file) = true, want false on unix (rename does not care)")
	}
	if err := setDestReadOnly(ro, false); err != nil {
		t.Fatalf("setDestReadOnly: %v", err)
	}
	fi, err := os.Stat(ro)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o444 {
		t.Fatalf("setDestReadOnly changed the mode to %04o; it must be a no-op on unix", fi.Mode().Perm())
	}
}
