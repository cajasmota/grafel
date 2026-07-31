package links

// atomic_write_5978_test.go — concurrent writers to one destination (#5978).
//
// Both link-output sinks in this package write temp-then-rename. When the temp
// name is derived from the destination by string concatenation (path+".tmp"),
// every writer aiming at the same destination shares ONE temp file: the first
// rename moves it away and the second fails with ENOENT, so that writer's
// payload never lands. On the writeDoc path that surfaces as an error the pass
// reports; on the scan-cache path the rename error is discarded, so a whole
// pass's work is dropped silently. These tests drive concurrent writers at one
// destination and assert every writer succeeds and the file that survives is
// one COMPLETE payload — never a torn or partial one.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestWriteDocConcurrentPassesLoseNothing models two (here: eight) link passes
// over the same group writing the same candidates file at once. Every write
// must either land whole or report an error — a rename that silently loses a
// pass's links is the bug.
func TestWriteDocConcurrentPassesLoseNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "candidates.json")

	const writers = 8
	const iterations = 20
	// Each writer owns a distinct link count, so the surviving file identifies
	// exactly which writer won — and any partial write is detectable.
	docFor := func(w int) *Document {
		d := &Document{Version: SchemaVersion}
		for i := 0; i <= w*40; i++ {
			d.Links = append(d.Links, Link{
				ID:     fmt.Sprintf("w%d-l%d", w, i),
				Source: fmt.Sprintf("svc-%d/handler-%d", w, i),
				Target: fmt.Sprintf("svc-%d/endpoint-%d", w, i),
				Method: "GET",
			})
		}
		return d
	}

	var mu sync.Mutex
	var errs []error

	for it := 0; it < iterations; it++ {
		var wg sync.WaitGroup
		for w := 0; w < writers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				if err := writeDoc(path, docFor(w)); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			}(w)
		}
		wg.Wait()

		// Whatever survived must be a complete document from ONE writer.
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("iteration %d: read destination: %v", it, err)
		}
		var got Document
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("iteration %d: destination is not valid JSON (torn write): %v", it, err)
		}
		ok := false
		for w := 0; w < writers; w++ {
			if len(got.Links) == len(docFor(w).Links) {
				ok = true
				break
			}
		}
		if !ok {
			t.Fatalf("iteration %d: destination has %d links, which matches no writer's complete output",
				it, len(got.Links))
		}
	}

	if len(errs) > 0 {
		t.Fatalf("%d of %d concurrent writes failed — a link pass's output was dropped; first: %v",
			len(errs), writers*iterations, errs[0])
	}

	// No temp files may be left behind on the success path.
	assertNoTempLeftovers(t, dir, "candidates.json")
}

// TestWriteFileAtomicConcurrentWritersLoseNothing pins the shared helper both
// sinks use (the scan cache in string_pass.go writes through it too, and it
// discards its rename error — so a collision there is invisible at runtime and
// can only be caught here).
func TestWriteFileAtomicConcurrentWritersLoseNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "abcdef0123456789.json")

	const writers = 8
	const iterations = 20
	payload := func(w int) []byte {
		return []byte(strings.Repeat(fmt.Sprintf("%d", w), 4096*(w+1)))
	}

	var mu sync.Mutex
	var errs []error

	for it := 0; it < iterations; it++ {
		var wg sync.WaitGroup
		for w := 0; w < writers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				if err := writeFileAtomic(path, payload(w), 0o644); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			}(w)
		}
		wg.Wait()

		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("iteration %d: read destination: %v", it, err)
		}
		ok := false
		for w := 0; w < writers; w++ {
			if string(b) == string(payload(w)) {
				ok = true
				break
			}
		}
		if !ok {
			t.Fatalf("iteration %d: destination holds %d bytes matching no writer's complete payload (torn write)",
				it, len(b))
		}
	}

	if len(errs) > 0 {
		t.Fatalf("%d of %d concurrent writes failed — a writer's payload was dropped; first: %v",
			len(errs), writers*iterations, errs[0])
	}
	assertNoTempLeftovers(t, dir, filepath.Base(path))
}

// TestWriteFileAtomicAppliesRequestedMode pins the destination's permission
// bits. os.CreateTemp makes its file 0600, so without an explicit chmod the
// switch away from os.WriteFile would silently narrow every file this helper
// writes — a mode regression no other test in this package would notice.
// The mode is asserted verbatim (not umask-masked): see writeFileAtomic's doc.
func TestWriteFileAtomicAppliesRequestedMode(t *testing.T) {
	dir := t.TempDir()

	for _, perm := range []os.FileMode{0o644, 0o600, 0o444} {
		path := filepath.Join(dir, fmt.Sprintf("mode-%o.json", perm))
		if err := writeFileAtomic(path, []byte("{}"), perm); err != nil {
			t.Fatalf("writeFileAtomic(%o): %v", perm, err)
		}
		// Asserted through testsupport.AssertPerm because Windows cannot
		// represent Unix permission bits at all (#6053); there the 0644/0600
		// rows degrade to "the file is writable" and the 0444 row — which
		// Windows DOES represent, as FILE_ATTRIBUTE_READONLY — is the one that
		// still catches a lost chmod.
		testsupport.AssertPerm(t, path, perm)
		_ = os.Chmod(path, 0o666)
	}

	// And through the real caller, so writeDoc's own choice of mode is covered.
	path := filepath.Join(dir, "candidates.json")
	if err := writeDoc(path, &Document{Version: SchemaVersion}); err != nil {
		t.Fatalf("writeDoc: %v", err)
	}
	testsupport.AssertPerm(t, path, 0o644)
}

func assertNoTempLeftovers(t *testing.T, dir, dest string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range ents {
		if e.Name() == dest {
			continue
		}
		t.Errorf("temp file %q left behind after successful writes", e.Name())
	}
}
