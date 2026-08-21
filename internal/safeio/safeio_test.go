package safeio

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestKindNamesEveryEntryType runs on EVERY platform, including Windows where
// syscall.Mkfifo does not exist. Kind is a pure function over a FileMode, so
// the FIFO / device / socket / door arms assert for real there rather than
// no-opping — the vacuous-fixture shape this repo has been removing all cycle.
func TestKindNamesEveryEntryType(t *testing.T) {
	for _, tc := range []struct {
		mode os.FileMode
		want string
	}{
		{0, "regular"},
		{os.ModeDir, "directory"},
		{os.ModeSymlink, "symlink"},
		{os.ModeNamedPipe, "named-pipe"},
		{os.ModeDevice, "device"},
		{os.ModeDevice | os.ModeCharDevice, "device"},
		{os.ModeSocket, "socket"},
		{os.ModeIrregular, "other"},
	} {
		if got := Kind(tc.mode); got != tc.want {
			t.Errorf("Kind(%v) = %q, want %q", tc.mode, got, tc.want)
		}
	}
}

// TestStatGateAcceptsRegularRejectsDirectory is the portable half of the type
// gate: a directory is the one non-regular entry every platform can create.
func TestStatGateAcceptsRegularRejectsDirectory(t *testing.T) {
	root := t.TempDir()
	reg := filepath.Join(root, "ok.txt")
	if err := os.WriteFile(reg, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Stat(reg, FollowSymlinks); err != nil {
		t.Errorf("regular file rejected: %v", err)
	}
	if _, err := Stat(root, FollowSymlinks); !errors.Is(err, ErrNotRegular) {
		t.Errorf("directory = %v, want ErrNotRegular", err)
	}
	if _, err := Stat(filepath.Join(root, "missing"), FollowSymlinks); !os.IsNotExist(err) {
		t.Errorf("missing path = %v, want IsNotExist (callers distinguish it from a type refusal)", err)
	}
}

// TestErrNotRegularNamesTheKind pins that the refusal is DIAGNOSABLE. A file
// that vanishes with an opaque error is the reporting failure #6338 exists to
// fix; the caller must be able to say "named-pipe", not just "failed".
func TestErrNotRegularNamesTheKind(t *testing.T) {
	root := t.TempDir()
	_, err := Stat(root, FollowSymlinks)
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Errorf("error %v does not name the entry kind", err)
	}
	if !strings.Contains(err.Error(), root) {
		t.Errorf("error %v does not name the path", err)
	}
}

// TestSymlinkPolicy pins the two policies apart. FollowSymlinks is what the
// scanners use — the walker mints a file entity for a symlink-to-file, so
// rejecting them all would delete legitimate coverage rather than being
// conservative — and RejectSymlinks exists for callers that must not be
// redirected out of a tree.
func TestSymlinkPolicy(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	if _, err := Stat(link, FollowSymlinks); err != nil {
		t.Errorf("FollowSymlinks rejected a symlink to a regular file: %v", err)
	}
	if _, err := Stat(link, RejectSymlinks); !errors.Is(err, ErrNotRegular) {
		t.Errorf("RejectSymlinks on a symlink = %v, want ErrNotRegular", err)
	}
	dangling := filepath.Join(root, "dangling.txt")
	if err := os.Symlink(filepath.Join(root, "nope"), dangling); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	if _, err := Stat(dangling, FollowSymlinks); err == nil {
		t.Error("dangling symlink accepted")
	}
}

// TestReadFileCapsTheRead pins the byte cap. It exists because a character
// device opens fine and never reaches EOF, so "the read will end at EOF" is
// not a bound — the cap is, and an uncapped read is what turns /dev/zero into
// an OOM instead of a hang.
func TestReadFileCapsTheRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(p, []byte(strings.Repeat("a", 5000)), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := ReadFile(p, FollowSymlinks, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 100 {
		t.Errorf("read %d bytes, want the 100-byte cap", len(b))
	}
	b, err = ReadFile(p, FollowSymlinks, 0)
	if err != nil || len(b) != 5000 {
		t.Errorf("uncapped read = %d bytes, %v; want 5000, nil", len(b), err)
	}
}

// TestOpenReturnsPromptlyOnADirectory is the portable liveness assertion: the
// gate must decide without ever opening, on every platform.
func TestOpenReturnsPromptlyOnADirectory(t *testing.T) {
	root := t.TempDir()
	done := make(chan struct{})
	go func() {
		f, err := Open(root, FollowSymlinks)
		if err == nil {
			f.Close()
			t.Error("directory opened")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Open blocked on a directory")
	}
}
