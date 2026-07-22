// Package flows implements the per-repo FLOW side-table (#5904 PR-b, the #5915
// companion to the description side-table in internal/graph/descriptions).
//
// # Why this exists
//
// The cross-repo link pass (internal/cli.runPhantomEdgePass) enriches a group's
// flows: it injects phantom cross_repo CALLS edges, STRIPS the index-baked
// intra-repo SCOPE.Process / SCOPE.EventFlow entities, and re-synthesises
// cross-repo-AWARE flow entities in their place. Historically it then PERSISTED
// that result by REWRITING the whole graph to disk (fbwriter.WriteGraphGen +
// graph.WriteAtomic). For a single-file graph that is merely wasteful; for a
// #5901 SEGMENT-SET graph it is a correctness hazard: the resident Document is a
// COLLAPSED union of every segment, so re-serialising it as one flat
// graph.<gen>.fb re-materialises the whole graph in memory (the #5915 P1 OOM)
// and silently discards the segmented layout.
//
// This side-table removes the whole-graph rewrite from the flow path. The
// cross-repo-aware flow DELTA is written to a small per-repo sidecar:
//
//	<stateDir>/flows.json
//
// and merged back onto the graph at READ time. The graph file / generation is
// never touched, so a segment-set stays a segment-set and no collapse/OOM can
// occur.
//
// # REPLACE, not additive (the critical subtlety)
//
// Flow entities have TWO producers. The NORMAL INDEX bakes INTRA-repo flows into
// graph.fb; the phantom pass STRIPS those and RE-EMITS cross-repo-aware ones.
// After this change graph.fb STILL contains the index-baked intra-repo flows,
// and the sidecar holds the cross-repo-aware flows. Therefore the read overlay
// is REPLACE, not additive: when a fresh flow sidecar exists it must SUPPRESS
// the baked SCOPE.Process / SCOPE.EventFlow entities + their STEP_IN_* / entry /
// seed edges and substitute the sidecar's, AND ADD the phantom cross_repo CALLS
// edges (which are not baked). A naive additive merge would DOUBLE every flow.
// Apply() enforces this.
//
// # Staleness / graceful degradation
//
// The sidecar records a SOURCE-FRESHNESS KEY derived from
// graph.CurrentGraphDescriptor(stateDir) at write time (never CurrentGraphPath —
// a segment-set's freshness derives from the gen dir / manifest, the #5915 P2
// discipline). A read whose stored source_key no longer equals the current key
// is STALE and treated as absent. On absence/corruption/staleness MergeInto is a
// no-op and the graph's own BAKED intra-repo flows are shown — a correct
// degraded state (valid, just not cross-repo-enriched, until the link pass
// re-runs). Never doubled, never empty.
package flows

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// FileName is the fixed basename of the per-repo flow sidecar.
const FileName = "flows.json"

// currentVersion is the on-disk schema version stamped into every write.
const currentVersion = 1

// Flow entity + edge kinds. Duplicated as literals (not imported from
// internal/engine) to keep this low-level graph sidecar free of the engine
// dependency; they mirror engine.EntityKindProcess / EntityKindEventFlow and the
// STEP/ENTRY/SEED relationship kinds exactly.
const (
	kindProcess   = "SCOPE.Process"
	kindEventFlow = "SCOPE.EventFlow"

	edgeStepInProcess   = "STEP_IN_PROCESS"
	edgeEntryPointOf    = "ENTRY_POINT_OF"
	edgeStepInEventFlow = "STEP_IN_EVENT_FLOW"
	edgeSeedOfEventFlow = "SEED_OF_EVENT_FLOW"
)

// Sidecar is the on-disk <stateDir>/flows.json document. It holds the cross-repo
// aware flow DELTA the phantom pass produced: the re-synthesised flow entities,
// their step/entry/seed edges, and the phantom cross_repo CALLS edges.
type Sidecar struct {
	// Version is the on-disk schema version.
	Version int `json:"version"`
	// ComputedAt is when the sidecar was last written (informational).
	ComputedAt time.Time `json:"computed_at"`
	// SourceKey is the graph-freshness key (see CurrentSourceKey) at write time.
	// A read whose SourceKey differs from the current key is stale → ignored.
	SourceKey string `json:"source_key"`
	// Entities are the cross-repo-aware SCOPE.Process / SCOPE.EventFlow entities
	// (with ALL their props: step_count, entry_id/name, terminal_id,
	// chain_labels, cross_stack, entry_kind, channel_count, …).
	Entities []graph.Entity `json:"entities"`
	// Relationships are the STEP_IN_PROCESS / STEP_IN_EVENT_FLOW (+ entry/seed)
	// edges of those flows PLUS the phantom cross_repo CALLS edges.
	Relationships []graph.Relationship `json:"relationships"`
}

// Path returns the sidecar path for a repo state dir.
func Path(stateDir string) string {
	return filepath.Join(stateDir, FileName)
}

// CurrentSourceKey computes the graph-freshness key for a repo state dir from
// its ACTIVE graph descriptor. The key changes whenever the underlying graph
// changes (a reindex writes a new generation / segment-set), which is exactly
// when a previously-written flow delta must be considered stale. Mirrors
// descriptions.CurrentSourceKey (same #5915 P2 discipline).
func CurrentSourceKey(stateDir string) string {
	desc, err := graph.CurrentGraphDescriptor(stateDir)
	if err != nil {
		return ""
	}
	switch desc.Kind {
	case graph.GraphSingleFile:
		if fi, statErr := os.Stat(desc.Path); statErr == nil {
			return fmt.Sprintf("single:%s:%d", filepath.Base(desc.Path), fi.ModTime().UnixNano())
		}
		return ""
	case graph.GraphSegmentSet:
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

// readRaw loads and unmarshals the sidecar WITHOUT the staleness check. Returns
// nil on absent / unreadable / corrupt.
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

// Read loads the sidecar and returns (sidecar, true) ONLY when it is present,
// well-formed, non-stale (its stored source_key still equals the current graph
// key), and non-empty. Absent, corrupt, stale, and empty all collapse to
// (nil, false) so the read path falls back to the baked intra-repo flows.
func Read(stateDir string) (*Sidecar, bool) {
	sc := readRaw(stateDir)
	if sc == nil {
		return nil, false
	}
	if sc.SourceKey != CurrentSourceKey(stateDir) {
		return nil, false // stale: written for a different graph generation
	}
	if len(sc.Entities) == 0 {
		return nil, false
	}
	return sc, true
}

// Upsert writes the cross-repo-aware flow delta (entities + relationships) into
// the sidecar via an atomic tmp+rename. The stored source_key is stamped to the
// CURRENT graph key. This is a full REPLACE of the delta (the phantom pass
// re-computes the whole flow layer each run), so no read-modify-merge is needed.
// The graph file / generation is NEVER touched.
func Upsert(stateDir string, entities []graph.Entity, relationships []graph.Relationship) error {
	sc := &Sidecar{
		Version:       currentVersion,
		SourceKey:     CurrentSourceKey(stateDir),
		ComputedAt:    time.Now().UTC(),
		Entities:      entities,
		Relationships: relationships,
	}
	return WriteTo(Path(stateDir), sc)
}

// IsFlowEntityKind reports whether kind is a flow entity kind
// (SCOPE.Process / SCOPE.EventFlow) — the kinds a fresh flow sidecar SUPPRESSES
// from the baked graph and substitutes. Exported so read-time consumers (the MCP
// forEach iterators) can apply the same REPLACE suppression without duplicating
// the literals.
func IsFlowEntityKind(kind string) bool {
	return kind == kindProcess || kind == kindEventFlow
}

// isFlowStructuralEdge reports whether kind is a flow STRUCTURAL edge (step /
// entry / seed) — the edges that bind a flow entity to its steps. These are
// suppressed alongside the baked flow entities on REPLACE. Ordinary CALLS edges
// are NOT in this set (they are real graph edges, never stripped).
func isFlowStructuralEdge(kind string) bool {
	switch kind {
	case edgeStepInProcess, edgeEntryPointOf, edgeStepInEventFlow, edgeSeedOfEventFlow:
		return true
	}
	return false
}

// StripBakedFlows returns copies of doc's entity/relationship slices with every
// baked SCOPE.Process / SCOPE.EventFlow entity removed, plus every flow
// STRUCTURAL edge (STEP_IN_* / ENTRY_POINT_OF / SEED_OF_EVENT_FLOW) that touches
// a removed flow entity. Ordinary edges (CALLS, IMPORTS, …) survive untouched.
// This mirrors internal/cli.stripProcessEntities — the same suppression the
// phantom pass performs before re-synthesising, now reused at read time.
func StripBakedFlows(entities []graph.Entity, relationships []graph.Relationship) ([]graph.Entity, []graph.Relationship) {
	dropped := make(map[string]bool)
	for i := range entities {
		if IsFlowEntityKind(entities[i].Kind) {
			dropped[entities[i].ID] = true
		}
	}
	if len(dropped) == 0 {
		return entities, relationships
	}
	outEnts := make([]graph.Entity, 0, len(entities))
	for i := range entities {
		if !dropped[entities[i].ID] {
			outEnts = append(outEnts, entities[i])
		}
	}
	outRels := make([]graph.Relationship, 0, len(relationships))
	for i := range relationships {
		r := &relationships[i]
		if (dropped[r.FromID] || dropped[r.ToID]) && isFlowStructuralEdge(r.Kind) {
			continue
		}
		outRels = append(outRels, *r)
	}
	return outEnts, outRels
}

// Apply performs the REPLACE merge of a flow sidecar onto doc IN PLACE: it
// SUPPRESSES the baked flow entities + their structural edges (StripBakedFlows),
// then SUBSTITUTES the sidecar's cross-repo-aware flow entities + edges (which
// also carry the phantom cross_repo CALLS edges). A nil sidecar is a no-op.
//
// The result is the exact set the phantom pass would have baked, without ever
// rewriting graph.fb. Because the baked flows are removed first, the merge is
// never additive — no flow is doubled.
func Apply(doc *graph.Document, sc *Sidecar) {
	if doc == nil || sc == nil {
		return
	}
	ents, rels := StripBakedFlows(doc.Entities, doc.Relationships)
	doc.Entities = append(ents, sc.Entities...)
	doc.Relationships = append(rels, sc.Relationships...)
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)
}

// MergeInto reads the flow sidecar for stateDir and, when it is fresh, Apply-s
// the REPLACE merge onto doc, returning true. On absence / corruption /
// staleness it is a no-op and returns false, leaving doc's baked intra-repo
// flows intact (the correct degraded state). This is the shared read-time seam
// for the Doc-path consumers (dashboard load, JSON export, MCP flag-OFF).
func MergeInto(stateDir string, doc *graph.Document) bool {
	sc, ok := Read(stateDir)
	if !ok {
		return false
	}
	Apply(doc, sc)
	return true
}

// WriteTo atomically writes the sidecar to path via a temp-file + rename
// (single-syscall swap on Unix; a bounded sharing-violation retry on Windows,
// atomicrename_windows.go). A nil sidecar is a no-op.
func WriteTo(path string, sc *Sidecar) error {
	if sc == nil {
		return nil
	}
	data, err := json.Marshal(sc)
	if err != nil {
		return fmt.Errorf("marshal flows: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdirall: %w", err)
	}
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
