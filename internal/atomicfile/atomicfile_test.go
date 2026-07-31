package atomicfile_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cajasmota/grafel/internal/atomicfile"
	"github.com/cajasmota/grafel/internal/testsupport"
)

func TestWriteFile_WritesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	want := []byte(`{"hello":"world"}`)
	if err := atomicfile.WriteFile(path, want, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

// TestWriteFile_AppliesPerm pins the Chmod. os.CreateTemp always creates the
// temp file 0600, so WITHOUT an explicit Chmod every destination lands at 0600
// regardless of the requested perm. Deleting the Chmod from the helper must
// fail this test — that is the whole point of it (#5978 lost exactly this).
//
// The 0444 row is load-bearing on Windows and only there. Windows has no Unix
// permission bits (see testsupport.AssertPerm), so the 0600/0644/0755 rows all
// collapse to "the file is writable" and a deleted Chmod would sail past them.
// 0444 is the one requested mode Windows CAN represent — as
// FILE_ATTRIBUTE_READONLY — so without the Chmod that row fails there too.
func TestWriteFile_AppliesPerm(t *testing.T) {
	for _, perm := range []os.FileMode{0o600, 0o644, 0o755, 0o444} {
		t.Run(fmt.Sprintf("%04o", perm), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "out.bin")
			if err := atomicfile.WriteFile(path, []byte("x"), perm); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			testsupport.AssertPerm(t, path, perm)
			// Leave it writable so TempDir cleanup can remove it on Windows.
			_ = os.Chmod(path, 0o666)
		})
	}
}

// TestWriteFile_PermNotUmaskMasked pins the documented behaviour change: the
// mode is applied verbatim by Chmod and is NOT narrowed by the process umask,
// unlike the os.WriteFile form this replaces.
func TestWriteFile_PermNotUmaskMasked(t *testing.T) {
	old := setUmask(t, 0o077)
	defer setUmask(t, old)

	dir := t.TempDir()
	path := filepath.Join(dir, "wide.json")
	if err := atomicfile.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o644 {
		t.Fatalf("mode under umask 077 = %04o, want 0644 (perm must not be umask-masked)", got)
	}
}

// TestWriteFile_OverwritesExisting checks the rename replaces an existing
// destination and resets its mode to the requested one.
func TestWriteFile_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := os.WriteFile(path, []byte("old-and-longer-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.WriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want %q", got, "new")
	}
	testsupport.AssertPerm(t, path, 0o644)
}

// TestWriteFile_NoTempLeftBehind asserts the success path leaves the
// destination directory holding exactly the destination.
func TestWriteFile_NoTempLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := atomicfile.WriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name() != "out.json" {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Fatalf("directory = %v, want only out.json", names)
	}
}

// TestWriteFile_RemovesTempOnError drives the rename failure path: the
// destination path is an existing DIRECTORY, so os.Rename fails and the temp
// file must not survive.
func TestWriteFile_RemovesTempOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dest")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.WriteFile(path, []byte("payload"), 0o644); err == nil {
		t.Fatal("WriteFile over a directory: want error, got nil")
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name() != "dest" {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Fatalf("directory = %v, want only the dest dir (temp must be removed)", names)
	}
}

// TestWriteFile_MissingDirErrors documents that WriteFile does NOT create the
// destination directory: os.CreateTemp fails there exactly as os.WriteFile
// would have. Callers keep their own MkdirAll.
func TestWriteFile_MissingDirErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nope", "out.json")
	if err := atomicfile.WriteFile(path, []byte("x"), 0o644); err == nil {
		t.Fatal("WriteFile into a missing directory: want error, got nil")
	}
}

// TestWriteFile_ReplacesSymlinkDestination pins that a symlink at path is
// REPLACED by a regular file rather than written through to its target. This
// differs from os.WriteFile and is inherent to rename-over-destination (the
// code this replaced renamed too, so it is not a regression).
func TestWriteFile_ReplacesSymlinkDestination(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := atomicfile.WriteFile(link, []byte("written"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("destination is still a symlink; expected it to be replaced")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("symlink target = %q, want it untouched (%q)", got, "original")
	}
}

// TestWriteFile_OverwritesReadOnlyDestination pins that a 0444 destination is
// replaced, where os.WriteFile would fail with EACCES. Also inherent to
// rename: the permission that matters is the DIRECTORY's, not the file's.
func TestWriteFile_OverwritesReadOnlyDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ro.json")
	if err := os.WriteFile(path, []byte("old"), 0o444); err != nil {
		t.Fatal(err)
	}
	// Confirm the contrast rather than asserting it from memory.
	if err := os.WriteFile(path, []byte("nope"), 0o444); err == nil {
		t.Skip("os.WriteFile can write a 0444 file here (running as root?); contrast is meaningless")
	}
	if err := atomicfile.WriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteFile over a 0444 destination: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want %q", got, "new")
	}
}

// TestWriteFile_ConcurrentWritersSameDestination is the #6018 regression test.
//
// Eight goroutines write DIFFERENT payloads to one destination at once. Every
// write must succeed, and the surviving file must be exactly one COMPLETE
// payload — never a torn mix and never a short prefix.
//
// Against the deterministic `path + ".tmp"` shape this fails: the writers all
// O_TRUNC the same temp inode and interleave into it, and whoever renames
// second gets ENOENT because the first already moved it away. That failure is
// reproduced directly by TestDeterministicTempName_FailsConcurrently below, so
// the two tests together show this test actually binds the behaviour.
func TestWriteFile_ConcurrentWritersSameDestination(t *testing.T) {
	const writers = 8
	const iterations = 40

	payloads := makePayloads(writers)
	valid := map[string]bool{}
	for _, p := range payloads {
		valid[string(p)] = true
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "shared.json")

	for it := 0; it < iterations; it++ {
		var wg sync.WaitGroup
		errs := make([]error, writers)
		start := make(chan struct{})
		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				errs[i] = atomicfile.WriteFile(path, payloads[i], 0o644)
			}(i)
		}
		close(start)
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Fatalf("iteration %d writer %d: %v", it, i, err)
			}
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("iteration %d: ReadFile: %v", it, err)
		}
		if !valid[string(got)] {
			t.Fatalf("iteration %d: destination holds a torn write (%d bytes, prefix %q)",
				it, len(got), truncate(got, 32))
		}
		testsupport.AssertPerm(t, path, 0o644)
	}
}

// TestDeterministicTempName_FailsConcurrently is the negative control for the
// test above: it runs the SAME concurrency harness against the old
// `path + ".tmp"` idiom and asserts it does in fact break (torn destination or
// a rename error). If this ever stops failing, the concurrency test above has
// stopped proving anything and must be strengthened.
func TestDeterministicTempName_FailsConcurrently(t *testing.T) {
	const writers = 8
	const iterations = 200

	payloads := makePayloads(writers)
	valid := map[string]bool{}
	for _, p := range payloads {
		valid[string(p)] = true
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "shared.json")

	// The unsafe idiom, verbatim as it appeared across the tree.
	writeDeterministic := func(b []byte) error {
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, b, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}

	for it := 0; it < iterations; it++ {
		var wg sync.WaitGroup
		errs := make([]error, writers)
		start := make(chan struct{})
		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				errs[i] = writeDeterministic(payloads[i])
			}(i)
		}
		close(start)
		wg.Wait()

		for _, err := range errs {
			if err != nil {
				return // rename lost the race — the class, reproduced.
			}
		}
		got, err := os.ReadFile(path)
		if err != nil {
			return
		}
		if !valid[string(got)] {
			return // torn destination — the class, reproduced.
		}
	}
	t.Fatalf("deterministic %q temp names survived %d rounds of %d concurrent writers; "+
		"the harness no longer reproduces #6018 and cannot vouch for the helper",
		".tmp", iterations, writers)
}

func makePayloads(n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		// Distinct lengths and distinct bytes, and big enough that a
		// partial write is visible rather than swallowed by one page.
		out[i] = bytes.Repeat([]byte{byte('A' + i)}, 4096*(i+1)+i)
	}
	return out
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
