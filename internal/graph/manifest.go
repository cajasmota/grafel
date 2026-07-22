// Package graph — manifest.go defines the on-disk manifest for a MULTI-SEGMENT
// generation directory (the #5890 segmented streaming-write epic; this is the
// DARK read-substrate slice #5901, which defines the format + teaches readers
// to route to it but writes NOTHING segmented yet).
//
// # Layout recap
//
// A graph that fits in ONE segment stays a plain graph.<gen>.fb file — no dir,
// no manifest, byte-identical to today (the single-file fast path). Only a
// MULTI-segment graph uses the directory layout:
//
//	graph.<gen>/
//	    seg-0000.fb
//	    seg-0001.fb
//	    ...
//	    manifest.json
//
// The `current` pointer may then name the gen DIR (graph.<gen>) or its
// manifest (graph.<gen>/manifest.json) — so `current` is NO LONGER guaranteed
// to name a *.fb file (see genpath.go's CurrentGraphDescriptor).
//
// # What the manifest records (decisions 3+4+5)
//
//   - A FormatVersion for the manifest schema itself (independent of
//     fbversion.Version, which versions the FlatBuffers *payload* inside each
//     seg-*.fb — this slice does NOT bump fbversion).
//   - One SegmentMeta per segment file, carrying its Kind (entity vs
//     relationship — so the two streams can be listed/counted independently),
//     its entity/relationship counts, and — for entity segments — the MIN and
//     MAX entity-ID key it contains. The reader uses that [min,max] range to
//     SKIP segments that cannot hold a looked-up key, avoiding the O(S)
//     per-segment fan-out (decision 4).
package graph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ManifestFileName is the fixed basename of the per-generation segment
// manifest inside a multi-segment gen dir (graph.<gen>/manifest.json).
const ManifestFileName = "manifest.json"

// ManifestFormatVersion is the CURRENT on-disk schema version of manifest.json.
// It versions the manifest STRUCTURE only; it is deliberately separate from
// fbversion.Version (which versions the FlatBuffers payload of each seg-*.fb).
// A reader rejects a manifest whose FormatVersion it does not understand.
const ManifestFormatVersion = 1

// SegmentKind classifies a segment file as carrying the entity stream or the
// relationship stream. The streams are conceptually independent (decision 5):
// the writer (a later slice) may emit entity-segments and relationship-segments
// separately, and the manifest lists/counts them independently. A single
// segment file MAY in practice carry both (a small graph split only by size),
// in which case Kind names its PRIMARY stream and both counts are populated.
type SegmentKind string

const (
	// SegmentEntities marks a segment whose primary payload is entities.
	SegmentEntities SegmentKind = "entity"
	// SegmentRelationships marks a segment whose primary payload is relationships.
	SegmentRelationships SegmentKind = "relationship"
)

// segFileRe validates a segment filename: seg-<digits>.fb (zero-padded on
// write, but any digit run accepted on read). Mirrors genpath's genFileRe
// hardening: a manifest whose File names fail this pattern (path traversal,
// absolute paths, "..", nested dirs) is REJECTED, so a hostile/corrupt
// manifest can never make a reader open a file outside the gen dir.
var segFileRe = regexp.MustCompile(`^seg-\d+\.fb$`)

// SegmentFileName renders the zero-padded on-disk name for the i-th segment
// (seg-0000.fb, seg-0001.fb, ...). Decision 2's zero-padding keeps a plain
// lexicographic dir listing in segment order. Used by the future streaming
// writer (#5902) and the dark-slice test fixtures.
func SegmentFileName(i int) string {
	return fmt.Sprintf("seg-%04d.fb", i)
}

// SegmentMeta describes one segment file within a gen dir's manifest.
type SegmentMeta struct {
	// File is the BARE filename (seg-NNNN.fb) of the segment, relative to the
	// gen dir. It is never a path — read-side validation (segFileRe) rejects
	// any value containing a separator or "..".
	File string `json:"file"`
	// Kind is the primary stream carried by this segment (entity|relationship).
	Kind SegmentKind `json:"kind"`
	// EntityCount / RelCount are the number of entities / relationships this
	// segment contributes. Either may be zero (a pure-relationship segment has
	// EntityCount==0, and vice-versa).
	EntityCount int `json:"entity_count"`
	RelCount    int `json:"rel_count"`
	// MinKey / MaxKey are the smallest and largest entity-ID keys present in
	// this segment (lexicographic, matching the FlatBuffers `(key)` sort on
	// Entity.id). Populated only for segments that carry entities
	// (EntityCount>0); empty for pure-relationship segments. The reader uses
	// [MinKey,MaxKey] to skip segments during LookupEntityByID (decision 4).
	MinKey string `json:"min_key,omitempty"`
	MaxKey string `json:"max_key,omitempty"`
}

// Manifest is the parsed graph.<gen>/manifest.json.
type Manifest struct {
	// FormatVersion is the manifest schema version (ManifestFormatVersion).
	FormatVersion int `json:"format_version"`
	// Segments lists every segment file in the gen dir, in read/open order.
	Segments []SegmentMeta `json:"segments"`
}

// EntitySegments returns the subset of segments that carry entities
// (EntityCount>0), preserving manifest order. Lets a caller enumerate the
// entity stream independently of the relationship stream (decision 5).
func (m *Manifest) EntitySegments() []SegmentMeta {
	if m == nil {
		return nil
	}
	out := make([]SegmentMeta, 0, len(m.Segments))
	for _, s := range m.Segments {
		if s.EntityCount > 0 {
			out = append(out, s)
		}
	}
	return out
}

// RelationshipSegments returns the subset of segments that carry relationships
// (RelCount>0), preserving manifest order.
func (m *Manifest) RelationshipSegments() []SegmentMeta {
	if m == nil {
		return nil
	}
	out := make([]SegmentMeta, 0, len(m.Segments))
	for _, s := range m.Segments {
		if s.RelCount > 0 {
			out = append(out, s)
		}
	}
	return out
}

// TotalEntityCount / TotalRelationshipCount sum the per-segment counts. Cheap
// header-level totals that avoid opening any segment file.
func (m *Manifest) TotalEntityCount() int {
	if m == nil {
		return 0
	}
	n := 0
	for _, s := range m.Segments {
		n += s.EntityCount
	}
	return n
}

func (m *Manifest) TotalRelationshipCount() int {
	if m == nil {
		return 0
	}
	n := 0
	for _, s := range m.Segments {
		n += s.RelCount
	}
	return n
}

// validate enforces the manifest's structural invariants, rejecting a
// malformed or hostile manifest before any segment file is opened:
//
//   - FormatVersion must be a known version (1..ManifestFormatVersion).
//   - At least one segment must be listed.
//   - Every File must be a bare seg-NNNN.fb name (no separators, no "..") so a
//     path-traversal entry can never escape the gen dir.
func (m *Manifest) validate() error {
	if m == nil {
		return fmt.Errorf("graph: nil manifest")
	}
	if m.FormatVersion < 1 || m.FormatVersion > ManifestFormatVersion {
		return fmt.Errorf("graph: unsupported manifest format version %d (this build understands 1..%d)",
			m.FormatVersion, ManifestFormatVersion)
	}
	if len(m.Segments) == 0 {
		return fmt.Errorf("graph: manifest lists no segments")
	}
	for i, s := range m.Segments {
		if s.File == "" {
			return fmt.Errorf("graph: manifest segment %d has empty file name", i)
		}
		// Reject anything that is not a bare seg-NNNN.fb name. filepath.Base
		// strips any directory prefix, so a File that differs from its own Base
		// carries a separator (traversal attempt) and is rejected outright.
		if s.File != filepath.Base(s.File) || strings.Contains(s.File, "..") || !segFileRe.MatchString(s.File) {
			return fmt.Errorf("graph: manifest segment %d has invalid file name %q", i, s.File)
		}
	}
	return nil
}

// ReadManifest reads and validates graph.<gen>/manifest.json from genDir.
// A missing, unreadable, malformed, or hostile manifest yields a non-nil
// error and a nil manifest — callers MUST NOT open any segment on error.
func ReadManifest(genDir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(genDir, ManifestFileName))
	if err != nil {
		return nil, fmt.Errorf("graph: read manifest in %s: %w", genDir, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("graph: parse manifest in %s: %w", genDir, err)
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// WriteManifest atomically writes m as graph.<gen>/manifest.json inside genDir
// (tmp + rename, mirroring WriteGenGraph / WriteCurrentPointer). It validates
// m first so a malformed manifest is never persisted. NOTE: this is the format
// primitive the FUTURE streaming writer (#5902) will call; the dark read slice
// only exercises it from tests to hand-build fixtures — no producer calls it.
func WriteManifest(genDir string, m *Manifest) error {
	if err := m.validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		return fmt.Errorf("graph: mkdir gen dir %s: %w", genDir, err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("graph: marshal manifest: %w", err)
	}
	dst := filepath.Join(genDir, ManifestFileName)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("graph: write manifest tmp: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("graph: rename manifest: %w", err)
	}
	return nil
}
