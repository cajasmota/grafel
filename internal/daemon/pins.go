// Package daemon — pin persistence (PH6 of epic #2087 / issue #2094).
//
// Pins prevent EXPIRED-eviction for non-default-branch refs that the user
// explicitly wants to keep indefinitely on disk. They complement the automatic
// isPinnedMain flag (for default branches) by letting users pin arbitrary refs.
//
// On-disk format: ~/.grafel/pins.json
//
//	{
//	  "version": 1,
//	  "pins": [
//	    {"group": "...", "repo": "...", "ref": "...", "pinned_at": "2026-05-25T..."}
//	  ]
//	}
package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/atomicfile"
)

// PinRecord is a single user-managed pin entry.
type PinRecord struct {
	Group    string    `json:"group"`
	Repo     string    `json:"repo"`
	Ref      string    `json:"ref"`
	PinnedAt time.Time `json:"pinned_at"`
}

// pinsFile is the on-disk envelope.
type pinsFile struct {
	Version int         `json:"version"`
	Pins    []PinRecord `json:"pins"`
}

// PinStore manages persistent pin state for the grafel home directory.
// Safe for concurrent use.
type PinStore struct {
	mu   sync.Mutex
	path string
	pins []PinRecord
}

// NewPinStore creates a PinStore backed by path. Call Load() to hydrate.
func NewPinStore(path string) *PinStore {
	return &PinStore{path: path}
}

// DefaultPinStore returns a PinStore rooted at the default grafel home.
func DefaultPinStore() (*PinStore, error) {
	home := homeDir()
	return NewPinStore(filepath.Join(home, "pins.json")), nil
}

// Load reads pins from disk. A missing file is treated as empty (first-run).
func (s *PinStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var f pinsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	s.pins = f.Pins
	return nil
}

// save writes pins atomically. Caller must hold s.mu.
func (s *PinStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	f := pinsFile{Version: 1, Pins: s.pins}
	if f.Pins == nil {
		f.Pins = []PinRecord{}
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	// #6018: unique temp name (s.mu only serialises writers inside ONE process).
	return atomicfile.WriteFile(s.path, data, 0o600)
}

// Pin adds a pin for (group, repo, ref). Idempotent — re-pinning an already
// pinned ref updates PinnedAt and persists.
func (s *PinStore) Pin(group, repo, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.pins {
		if p.Group == group && p.Repo == repo && p.Ref == ref {
			s.pins[i].PinnedAt = time.Now().UTC()
			return s.save()
		}
	}
	s.pins = append(s.pins, PinRecord{
		Group:    group,
		Repo:     repo,
		Ref:      ref,
		PinnedAt: time.Now().UTC(),
	})
	return s.save()
}

// Unpin removes a pin for (group, repo, ref). No-op if not pinned.
func (s *PinStore) Unpin(group, repo, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.pins[:0]
	for _, p := range s.pins {
		if p.Group == group && p.Repo == repo && p.Ref == ref {
			continue
		}
		out = append(out, p)
	}
	s.pins = out
	return s.save()
}

// IsPinned reports whether (group, repo, ref) is pinned.
func (s *PinStore) IsPinned(group, repo, ref string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.pins {
		if p.Group == group && p.Repo == repo && p.Ref == ref {
			return true
		}
	}
	return false
}

// All returns a snapshot of all pin records.
func (s *PinStore) All() []PinRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PinRecord, len(s.pins))
	copy(out, s.pins)
	return out
}
