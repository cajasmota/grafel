package links

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
