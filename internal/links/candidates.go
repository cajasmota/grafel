package links

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/cajasmota/grafel/internal/atomicfile"
)

// readDoc loads a Document from disk. A missing file returns an empty
// document — never an error.
func readDoc(path string) (*Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Document{Version: SchemaVersion}, nil
		}
		return nil, err
	}
	d := &Document{}
	if err := json.Unmarshal(b, d); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if d.Version == 0 {
		d.Version = SchemaVersion
	}
	return d, nil
}

// writeDoc writes the document atomically (tmp + rename), pretty-printed.
func writeDoc(path string, d *Document) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if d.Links == nil {
		d.Links = []Link{}
	}
	// Stable order: by source, then target, then method.
	sort.Slice(d.Links, func(i, j int) bool {
		a, b := d.Links[i], d.Links[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		return a.Method < b.Method
	})
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b, 0o644)
}

// writeFileAtomic writes b to path via a UNIQUE temp file in path's own
// directory, then renames it over path.
//
// The temp name comes from os.CreateTemp and NOT from path+".tmp" (#5978). A
// temp name derived from the destination is shared by every writer aiming at
// that destination, so two concurrent link passes over one group interleave
// inside one temp file: they overwrite each other's bytes (the destination can
// end up holding a torn mix), and the writer that renames second fails with
// ENOENT because the first already moved the file away. On the scan-cache sink
// in string_pass.go that rename error is discarded, so the collision there is
// invisible at runtime.
//
// The temp file is created in path's directory so the rename stays within one
// filesystem and is therefore atomic; it is removed on every error path.
//
// MODE: perm is applied verbatim, NOT masked by the process umask — os.CreateTemp
// makes the file 0600 and an explicit Chmod sets exactly what the caller asked
// for. os.WriteFile, which this replaced, passes perm through open(2) and so
// WAS umask-masked. Under a restrictive umask (077) these files therefore widen
// from 0600 to 0644. That is deliberate: the destinations are per-user state
// under the grafel home, and a deterministic mode is easier to reason about
// than one that depends on the umask of whichever process (daemon, CLI, child)
// happened to run the pass.
//
// The body is now internal/atomicfile.WriteFile (#6018 generalised this exact
// helper) rather than a second copy of it. That copy was still calling
// os.Rename directly, which on Windows cannot replace a read-only destination
// and loses races against other open handles — so this package's own
// concurrency tests failed there while the shared helper's passed (#6053).
// Keep this a one-line delegation; a private copy is how the bug got back in
// the first place. (For the avoidance of a wrong number: the tree still holds
// five OTHER hand-rolled Windows rename-retry loops — graph/groupalgo,
// graph/descriptions, graph/flows, statusfile, install — and consolidating
// them is a separate sweep, deliberately not done on this release-blocking
// branch. See internal/atomicfile/rename.go.)
func writeFileAtomic(path string, b []byte, perm os.FileMode) error {
	return atomicfile.WriteFile(path, b, perm)
}

// loadRejections reads the rejection file and returns a set keyed by
// (source|target|method). Missing file → empty set.
func loadRejections(path string) (map[string]bool, error) {
	d, err := readDoc(path)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, l := range d.Links {
		out[rejectionKey(l.Source, l.Target, l.Method)] = true
	}
	return out, nil
}

func rejectionKey(source, target, method string) string {
	return source + "|" + target + "|" + method
}

// methodSet is a small helper used by passes to declare which methods
// they own (for segregated overwrite).
type methodSet map[string]bool

func newMethodSet(methods ...string) methodSet {
	m := methodSet{}
	for _, x := range methods {
		m[x] = true
	}
	return m
}

// replaceByMethod replaces every entry in `path` whose method is in
// `owned` with `incoming`. Entries with other methods are preserved.
// `incoming` is also filtered against the rejection set.
func replaceByMethod(path string, owned methodSet, incoming []Link, rejects map[string]bool) (added, skipped int, err error) {
	doc, err := readDoc(path)
	if err != nil {
		return 0, 0, err
	}
	var preserved []Link
	for _, l := range doc.Links {
		if !owned[l.Method] {
			preserved = append(preserved, l)
		}
	}
	// Filter incoming through rejection set + dedupe by id.
	seen := map[string]bool{}
	var fresh []Link
	for _, l := range incoming {
		if rejects[rejectionKey(l.Source, l.Target, l.Method)] {
			skipped++
			continue
		}
		if seen[l.ID] {
			continue
		}
		seen[l.ID] = true
		fresh = append(fresh, l)
		added++
	}
	doc.Links = append(preserved, fresh...)
	if err := writeDoc(path, doc); err != nil {
		return 0, 0, err
	}
	return added, skipped, nil
}
