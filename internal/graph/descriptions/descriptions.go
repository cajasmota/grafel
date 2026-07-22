// Package descriptions implements the per-repo DESCRIPTION side-table (#5904 PR-a).
//
// # Why this exists
//
// Dashboard enrichment write-back historically persisted an agent-generated
// entity description by mutating the in-memory graph and REWRITING the whole
// graph to disk (fbwriter.WriteGraphGen + graph.WriteAtomic). For a single-file
// graph that is merely wasteful; for a #5901 SEGMENT-SET graph it is a
// correctness hazard: the resident Document is a COLLAPSED union of every
// segment, so re-serialising it as one flat graph.<gen>.fb re-materialises the
// entire graph in memory (the #5915 P1 OOM) and silently discards the segmented
// layout.
//
// The side-table removes the whole-graph rewrite from the description path,
// mirroring the proven group-algo overlay (internal/graph/groupalgo/overlay.go).
// A description is written to a small per-repo sidecar:
//
//	<stateDir>/descriptions.json
//
// and merged back onto entities at READ time (an additive PropSet of
// "description"). The graph file / generation is never touched, so a segment-set
// stays a segment-set and no collapse/OOM can occur.
//
// # Schema + staleness
//
// The sidecar records a SOURCE-FRESHNESS KEY derived from
// graph.CurrentGraphDescriptor(stateDir) at write time (NOT CurrentGraphPath —
// a segment-set's freshness must derive from the gen dir / manifest, the #5915
// P2 discipline). A read whose stored source_key no longer equals the current
// key is STALE and treated as absent: the entity keeps whatever it already
// carried (extractor-native description, or a description baked in before this
// change). This makes reads absence-, corruption-, and staleness-tolerant — a
// missing entry never clears an existing description.
package descriptions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// FileName is the fixed basename of the per-repo description sidecar.
const FileName = "descriptions.json"

// currentVersion is the on-disk schema version stamped into every write.
const currentVersion = 1

// Sidecar is the on-disk <stateDir>/descriptions.json document.
type Sidecar struct {
	// Version is the on-disk schema version.
	Version int `json:"version"`
	// ComputedAt is when the sidecar was last written (informational).
	ComputedAt time.Time `json:"computed_at"`
	// SourceKey is the graph-freshness key (see CurrentSourceKey) at write time.
	// A read whose SourceKey differs from the current key is stale → ignored.
	SourceKey string `json:"source_key"`
	// Results maps entity id → its agent-generated description.
	Results map[string]string `json:"results"`
}

// Path returns the sidecar path for a repo state dir.
func Path(stateDir string) string {
	return filepath.Join(stateDir, FileName)
}

// CurrentSourceKey computes the graph-freshness key for a repo state dir from
// its ACTIVE graph descriptor. The key changes whenever the underlying graph
// changes (a reindex writes a new generation / segment-set), which is exactly
// when previously-written descriptions must be considered stale.
//
//   - GraphSingleFile — key derives from the resolved .fb file's mtime.
//   - GraphSegmentSet — key derives from the gen dir + its manifest mtime (NOT a
//     collapsed single-file path; the #5915 P2 discipline).
//   - GraphAbsent — a JSON-only repo (no graph.fb): fall back to graph.json's
//     mtime so JSON-only repos still get proper staleness. If nothing exists,
//     the key is "" (which is stable and matches itself — descriptions written
//     for a graph-less repo simply never go stale on their own).
func CurrentSourceKey(stateDir string) string {
	desc, err := graph.CurrentGraphDescriptor(stateDir)
	if err != nil {
		// A corrupt/hostile segment-set surfaces an error; refuse to derive a key
		// (callers treat "" as "no fresh graph" and the sidecar stays inert).
		return ""
	}
	switch desc.Kind {
	case graph.GraphSingleFile:
		if fi, statErr := os.Stat(desc.Path); statErr == nil {
			return fmt.Sprintf("single:%s:%d", filepath.Base(desc.Path), fi.ModTime().UnixNano())
		}
		return ""
	case graph.GraphSegmentSet:
		// A segment-set's freshness derives from the gen dir / manifest, never a
		// re-materialised single-file path.
		manifest := filepath.Join(desc.GenDir, graph.ManifestFileName)
		if fi, statErr := os.Stat(manifest); statErr == nil {
			return fmt.Sprintf("seg:%s:%d", filepath.Base(desc.GenDir), fi.ModTime().UnixNano())
		}
		if fi, statErr := os.Stat(desc.GenDir); statErr == nil {
			return fmt.Sprintf("seg:%s:%d", filepath.Base(desc.GenDir), fi.ModTime().UnixNano())
		}
		return ""
	default: // GraphAbsent
		if fi, statErr := os.Stat(filepath.Join(stateDir, "graph.json")); statErr == nil {
			return fmt.Sprintf("json:%d", fi.ModTime().UnixNano())
		}
		return ""
	}
}

// readRaw loads and unmarshals the sidecar at stateDir WITHOUT the staleness
// check. Returns nil on absent / unreadable / corrupt. Used by the upsert
// read-modify-write, which discards stale Results itself.
func readRaw(stateDir string) *Sidecar {
	data, err := os.ReadFile(Path(stateDir))
	if err != nil {
		return nil
	}
	var sc Sidecar
	if err := json.Unmarshal(data, &sc); err != nil {
		return nil
	}
	return &sc
}

// Read loads the sidecar for stateDir and returns (sidecar, true) ONLY when it
// is present, well-formed, and NON-stale (its stored source_key still equals the
// current graph key). Absent, corrupt, and stale all collapse to (nil, false) so
// the apply path is absence-tolerant — a miss never clears an entity's existing
// description.
func Read(stateDir string) (*Sidecar, bool) {
	sc := readRaw(stateDir)
	if sc == nil {
		return nil, false
	}
	if sc.SourceKey != CurrentSourceKey(stateDir) {
		return nil, false // stale: written for a different graph generation
	}
	if len(sc.Results) == 0 {
		return nil, false
	}
	return sc, true
}

// Upsert writes description for entityID into the sidecar via a read-modify-write
// and an atomic tmp+rename. The stored source_key is (re)stamped to the CURRENT
// graph key; if the existing sidecar was written for a different (older) graph
// generation its stale Results are discarded and the upsert starts fresh. The
// graph file / generation is NEVER touched.
func Upsert(stateDir, entityID, description string) error {
	key := CurrentSourceKey(stateDir)
	sc := readRaw(stateDir)
	if sc == nil || sc.SourceKey != key {
		sc = &Sidecar{Results: map[string]string{}}
	}
	if sc.Results == nil {
		sc.Results = map[string]string{}
	}
	sc.Version = currentVersion
	sc.SourceKey = key
	sc.ComputedAt = time.Now().UTC()
	sc.Results[entityID] = description
	return WriteTo(Path(stateDir), sc)
}

// WriteTo atomically writes the sidecar to path via a temp-file + rename
// (single-syscall swap on Unix; a bounded sharing-violation retry on Windows,
// atomicrename_windows.go). The sidecar is never memory-mapped, so this is safe
// on every platform. A nil sidecar is a no-op.
func WriteTo(path string, sc *Sidecar) error {
	if sc == nil {
		return nil
	}
	data, err := json.Marshal(sc)
	if err != nil {
		return fmt.Errorf("marshal descriptions: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdirall: %w", err)
	}
	// Temp file in the SAME directory so os.Rename is an atomic intra-filesystem
	// swap; a pid suffix avoids two concurrent writers clobbering each other.
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := atomicRename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
