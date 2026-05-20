package enrichment

// repair_apply implements the reader + apply path for ADR-0015 phase-1
// repair.json (issue #545). Sibling to repair_edge.go, which is the writer
// side (#544).
//
// Lifecycle:
//   1. ReadRepairs(<repo>/.archigraph/repair.json) → []Repair
//   2. ApplyRepairs(doc, repairs, opts) BEFORE the indexer's final
//      ClassifyEndpoints reclassify pass. Mutates doc.Relationships in
//      place: rewrites ToID for binds/reclassifies, drops abandons,
//      tags affected edges with resolved_by="agent-repair" and
//      repair_reasoning=<verbatim text>.
//   3. WriteRepairStats(<repo>/.archigraph/repair_stats.json, stats) so the
//      operator can audit which repairs landed and which were dropped.
//
// Trust model (R1-R7) matches docs/specs/repair-trust-model.md. Every
// rejection records the edge_id and a reason code; the agent re-fetches
// candidates and re-submits.
//
// This module is purely additive. With --enable-repair-apply OFF the
// indexer never calls ApplyRepairs, so the default bug-rate measurements
// are unchanged.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// RepairResolutionKind is the on-disk allowlist. Adding to this set requires
// an ADR amendment per ADR-0015.
const (
	RepairBindToEntity         = "bind_to_entity"
	RepairReclassifyAsExternal = "reclassify_as_external"
	RepairReclassifyAsDynamic  = "reclassify_as_dynamic"
	RepairReclassifyAsResolved = "reclassify_as_resolved"
	RepairAbandon              = "abandon"
)

// allowedResolutionKinds is the closed set enforced by R2 / "resolution
// allowlist". Keep in sync with docs/specs/repair-v1.schema.json.
var allowedResolutionKinds = map[string]bool{
	RepairBindToEntity:         true,
	RepairReclassifyAsExternal: true,
	RepairReclassifyAsDynamic:  true,
	RepairReclassifyAsResolved: true,
	RepairAbandon:              true,
}

// moduleIdentRe is the R5 (invalid_module_identifier) regex. Matches the
// JSON-schema pattern from repair-v1.schema.json: leading letter/underscore,
// then letters/digits/_/-/./. — but disallows ".." traversal and leading
// dots so an attacker cannot smuggle "ext:../etc/passwd" into the graph.
var moduleIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_\-./]*$`)

// Repair is one row in repair.json. Mirrors the JSON-schema in
// docs/specs/repair-v1.schema.json. Field tags use the exact on-disk names.
type Repair struct {
	EdgeID         string  `json:"edge_id"`
	Resolution     string  `json:"resolution"`
	TargetEntityID string  `json:"target_entity_id,omitempty"`
	Module         string  `json:"module,omitempty"`
	NewTarget      string  `json:"new_target,omitempty"`
	DynamicReason  string  `json:"dynamic_reason,omitempty"`
	AbandonReason  string  `json:"abandon_reason,omitempty"`
	Confidence     float64 `json:"confidence"`
	Reasoning      string  `json:"reasoning"`
	Source         string  `json:"source"`
	ResolvedAt     string  `json:"resolved_at"`
}

// repairFile is the top-level on-disk shape.
type repairFile struct {
	SchemaVersion int      `json:"schema_version"`
	GeneratedAt   string   `json:"generated_at,omitempty"`
	Repairs       []Repair `json:"repairs"`
}

// RepairRejection records one (edge_id, reason) pair for the stats sidecar.
// Reason codes match docs/specs/repair-trust-model.md.
type RepairRejection struct {
	EdgeID     string `json:"edge_id"`
	Resolution string `json:"resolution,omitempty"`
	Reason     string `json:"reason"`
}

// RepairAppliedRecord records one successfully-applied repair for the
// stats sidecar. Order in the file is by edge_id ascending for
// byte-identical determinism.
type RepairAppliedRecord struct {
	EdgeID     string `json:"edge_id"`
	Resolution string `json:"resolution"`
}

// RepairStaleRecord records a repair whose edge_id no longer matches any
// current candidate (R-stale).
type RepairStaleRecord struct {
	EdgeID     string `json:"edge_id"`
	Resolution string `json:"resolution"`
	ResolvedAt string `json:"resolved_at,omitempty"`
}

// RepairStats is the on-disk stats sidecar shape. Emitted to
// <repo>/.archigraph/repair_stats.json on every index run that read a
// repair.json — even if no repairs applied.
type RepairStats struct {
	SchemaVersion int                   `json:"schema_version"`
	Applied       []RepairAppliedRecord `json:"applied"`
	Rejected      []RepairRejection     `json:"rejected"`
	Stale         []RepairStaleRecord   `json:"stale"`
	AppliedCount  int                   `json:"applied_count"`
	RejectedCount int                   `json:"rejected_count"`
	StaleCount    int                   `json:"stale_count"`
}

// repairPath returns the on-disk path for repair.json.
func repairPath(archigraphDir string) string {
	return filepath.Join(archigraphDir, "repair.json")
}

// repairStatsPath returns the on-disk path for repair_stats.json.
func repairStatsPath(archigraphDir string) string {
	return filepath.Join(archigraphDir, "repair_stats.json")
}

// ReadRepairs reads repair.json from the archigraph dir. Returns nil if
// the file is absent. Tolerates an empty/whitespace file. Unmarshalling
// errors are returned so the caller can surface them; the indexer logs
// and continues with zero repairs.
func ReadRepairs(archigraphDir string) ([]Repair, error) {
	data, err := os.ReadFile(repairPath(archigraphDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var rf repairFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("parse repair.json: %w", err)
	}
	return rf.Repairs, nil
}

// ApplyRepairsOptions are the knobs the indexer threads through.
type ApplyRepairsOptions struct {
	// RepoRoot is the absolute path to the repo being indexed. Reserved
	// for future repairs that need source-content access; unused today.
	RepoRoot string
}

// ApplyRepairs walks doc.Relationships, recomputes each edge's edge_id,
// looks it up in the repair map, validates the repair per R1-R7, and
// applies it in place. Abandoned edges are removed from doc.Relationships.
//
// Returns RepairStats describing what landed and what was rejected.
// The stats slices are sorted by edge_id ascending so on-disk output is
// byte-identical across runs of the same inputs.
//
// Invariants:
//   - doc.Relationships order is preserved for non-abandoned edges.
//   - doc.Entities is untouched (entity-level repairs are out of scope).
//   - Repairs without a matching current edge_id are recorded as stale
//     and never applied (R-stale).
//   - A repair that fails validation is recorded as rejected; the edge
//     is left as-is so the static resolver still has a shot at it.
func ApplyRepairs(doc *graph.Document, repairs []Repair, opts ApplyRepairsOptions) RepairStats {
	stats := RepairStats{
		SchemaVersion: 1,
		Applied:       []RepairAppliedRecord{},
		Rejected:      []RepairRejection{},
		Stale:         []RepairStaleRecord{},
	}
	if doc == nil {
		return stats
	}

	// Build (edge_id → *Repair) and remember the *original* repair for
	// stale-detection. The same edge_id may appear multiple times in
	// repair.json (operator hand-edited duplicate); the last wins, but we
	// still record the dup as rejected with reason "duplicate_edge_id".
	repairByEdge := make(map[string]*Repair, len(repairs))
	for ri := range repairs {
		r := &repairs[ri]
		if _, exists := repairByEdge[r.EdgeID]; exists {
			stats.Rejected = append(stats.Rejected, RepairRejection{
				EdgeID:     r.EdgeID,
				Resolution: r.Resolution,
				Reason:     "duplicate_edge_id",
			})
			continue
		}
		repairByEdge[r.EdgeID] = r
	}

	// Build (entity_id → kind) lookup for R2 (target_entity_not_found)
	// and the CONTAINS-hierarchy lookup for R4 (contradicts_contains).
	byID := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}
	containsParents := buildContainsParents(doc)

	// Walk relationships, recompute edge_ids, mark matches as "seen" so
	// we can detect stale repairs afterwards.
	seenEdgeIDs := make(map[string]bool, len(doc.Relationships))
	kept := doc.Relationships[:0]

	for ri := range doc.Relationships {
		r := &doc.Relationships[ri]
		// Same emission rules as CollectRepairEdgeCandidates — skip
		// already-resolved (16-char hex) and ext: stubs so the edge_id
		// space matches #544's writer exactly.
		stub := r.ToID
		if stub == "" || isHexID(stub) || strings.HasPrefix(stub, "ext:") {
			kept = append(kept, *r)
			continue
		}
		edgeID := repairEdgeID(r.FromID, r.Kind, stub)
		seenEdgeIDs[edgeID] = true

		rep, ok := repairByEdge[edgeID]
		if !ok {
			kept = append(kept, *r)
			continue
		}

		// Validate per R1-R7. R1 (edge_id_unknown) is the seen-set check
		// done later; here the edge_id is known by construction.
		if reason := validateRepair(rep, r, byID, containsParents); reason != "" {
			stats.Rejected = append(stats.Rejected, RepairRejection{
				EdgeID:     edgeID,
				Resolution: rep.Resolution,
				Reason:     reason,
			})
			kept = append(kept, *r)
			continue
		}

		// Apply.
		newR, drop := applyOneRepair(*r, rep)
		if !drop {
			kept = append(kept, newR)
		}
		stats.Applied = append(stats.Applied, RepairAppliedRecord{
			EdgeID:     edgeID,
			Resolution: rep.Resolution,
		})
	}
	doc.Relationships = kept

	// Stale detection — any repair whose edge_id was never seen in the
	// current relationship walk is stale. R-stale per the trust model.
	for edgeID, rep := range repairByEdge {
		if seenEdgeIDs[edgeID] {
			continue
		}
		// If the repair was already rejected (e.g. duplicate), don't
		// double-count it as stale.
		alreadyRejected := false
		for _, rj := range stats.Rejected {
			if rj.EdgeID == edgeID && rj.Reason == "duplicate_edge_id" {
				alreadyRejected = true
				break
			}
		}
		if alreadyRejected {
			continue
		}
		stats.Stale = append(stats.Stale, RepairStaleRecord{
			EdgeID:     edgeID,
			Resolution: rep.Resolution,
			ResolvedAt: rep.ResolvedAt,
		})
	}

	// Sort for deterministic output (ADR-0015 references #486).
	sort.SliceStable(stats.Applied, func(i, j int) bool {
		return stats.Applied[i].EdgeID < stats.Applied[j].EdgeID
	})
	sort.SliceStable(stats.Rejected, func(i, j int) bool {
		return stats.Rejected[i].EdgeID < stats.Rejected[j].EdgeID
	})
	sort.SliceStable(stats.Stale, func(i, j int) bool {
		return stats.Stale[i].EdgeID < stats.Stale[j].EdgeID
	})
	stats.AppliedCount = len(stats.Applied)
	stats.RejectedCount = len(stats.Rejected)
	stats.StaleCount = len(stats.Stale)
	return stats
}

// validateRepair runs R2-R7 trust-model checks. Returns a non-empty reason
// code on rejection; "" means the repair is safe to apply.
//
// R1 (edge_id_unknown) is not handled here — that's the caller's
// "not in current edge set" check. R-stale is the inverse of R1 and is
// also handled by the caller.
func validateRepair(
	rep *Repair,
	edge *graph.Relationship,
	byID map[string]*graph.Entity,
	containsParents map[string]map[string]bool,
) string {
	// R-allowlist (closed set of resolution kinds).
	if !allowedResolutionKinds[rep.Resolution] {
		return "resolution_kind_unsupported"
	}
	// R7 — reasoning must be substantive.
	if strings.TrimSpace(rep.Reasoning) == "" {
		return "reasoning_too_short"
	}
	// R6 — conditional required fields per resolution kind.
	switch rep.Resolution {
	case RepairBindToEntity:
		if rep.TargetEntityID == "" {
			return "missing_required_field"
		}
		// R2 — target entity must exist.
		target, ok := byID[rep.TargetEntityID]
		if !ok || target == nil {
			return "target_entity_not_found"
		}
		// R3 — no self-loop.
		if rep.TargetEntityID == edge.FromID {
			return "self_loop_disallowed"
		}
		// R4 — target cannot be a CONTAINS ancestor of from.
		if ancestors, ok := containsParents[edge.FromID]; ok {
			if ancestors[rep.TargetEntityID] {
				return "contradicts_contains_hierarchy"
			}
		}
	case RepairReclassifyAsExternal:
		if rep.Module == "" {
			return "missing_required_field"
		}
		if !moduleIdentRe.MatchString(rep.Module) ||
			strings.Contains(rep.Module, "..") ||
			strings.HasPrefix(rep.Module, "/") {
			return "invalid_module_identifier"
		}
	case RepairReclassifyAsDynamic:
		if strings.TrimSpace(rep.DynamicReason) == "" {
			return "missing_required_field"
		}
	case RepairReclassifyAsResolved:
		if strings.TrimSpace(rep.NewTarget) == "" {
			return "missing_required_field"
		}
	case RepairAbandon:
		if strings.TrimSpace(rep.AbandonReason) == "" {
			return "missing_required_field"
		}
	}
	return ""
}

// applyOneRepair returns the rewritten relationship and a drop=true flag
// for abandon. The original is copied — the slice element is replaced
// wholesale so callers don't have to worry about aliasing.
func applyOneRepair(r graph.Relationship, rep *Repair) (graph.Relationship, bool) {
	if rep.Resolution == RepairAbandon {
		return r, true
	}
	if r.Properties == nil {
		r.Properties = make(map[string]string, 4)
	}
	// Source-attribution (ADR-0015 #4/8, issue #547) — every applied edge
	// carries three auditable properties:
	//   resolved_by       = "agent-repair" (distinguished from "static")
	//   resolved_by_agent = <repair.Source> e.g. "generate-docs/pass-1a"
	//   repair_reasoning  = verbatim one-sentence reasoning from repair.json
	// Downstream consumers can filter on resolved_by to find all
	// agent-touched edges and read resolved_by_agent to trace the originating
	// skill or pass.
	r.Properties["resolved_by"] = "agent-repair"
	if rep.Source != "" {
		r.Properties["resolved_by_agent"] = rep.Source
	}
	r.Properties["repair_reasoning"] = rep.Reasoning

	switch rep.Resolution {
	case RepairBindToEntity:
		r.ToID = rep.TargetEntityID
		// disposition becomes Resolved on the next ClassifyEndpoints
		// pass because ToID is now a hex entity id.
	case RepairReclassifyAsExternal:
		r.ToID = "ext:" + rep.Module
		// disposition becomes ExternalKnown (assuming the allowlist
		// contains the module; otherwise ExternalUnknown — that's
		// acceptable, the agent told us it's external).
	case RepairReclassifyAsResolved:
		r.ToID = rep.NewTarget
		// Mark resolved-by-agent so the static resolver's
		// re-resolution pass skips this edge.
		r.Properties["repair_kind"] = "reclassify_as_resolved"
	case RepairReclassifyAsDynamic:
		// ToID stays — but tag the edge so the resolver's dynamic
		// classifier treats it as dynamic on the next pass.
		r.Properties["repair_kind"] = "reclassify_as_dynamic"
		r.Properties["dynamic_reason"] = rep.DynamicReason
	}
	return r, false
}

// buildContainsParents returns (entity_id → set of ancestor entity_ids
// via CONTAINS edges) for R4. The walk is bounded to a single depth-first
// traversal; cycles are guarded by a visited set so a pathological
// hand-built graph doesn't loop forever.
func buildContainsParents(doc *graph.Document) map[string]map[string]bool {
	// children[c] = set of parents linked via "CONTAINS"
	directParents := make(map[string]map[string]bool, len(doc.Entities))
	for ri := range doc.Relationships {
		r := &doc.Relationships[ri]
		if r.Kind != "CONTAINS" {
			continue
		}
		if directParents[r.ToID] == nil {
			directParents[r.ToID] = map[string]bool{}
		}
		directParents[r.ToID][r.FromID] = true
	}
	// Walk transitively. For each entity, accumulate the closure of its
	// direct parents.
	out := make(map[string]map[string]bool, len(directParents))
	var walk func(id string, acc map[string]bool, seen map[string]bool)
	walk = func(id string, acc map[string]bool, seen map[string]bool) {
		if seen[id] {
			return
		}
		seen[id] = true
		for p := range directParents[id] {
			acc[p] = true
			walk(p, acc, seen)
		}
	}
	for id := range directParents {
		acc := map[string]bool{}
		walk(id, acc, map[string]bool{})
		out[id] = acc
	}
	return out
}

// ReadRepairStats reads repair_stats.json from the archigraph dir. Returns
// a zero-value RepairStats (with nil slices) if the file is absent — callers
// can distinguish "no stats yet" from "zero stale" via the zero-value check.
func ReadRepairStats(archigraphDir string) (RepairStats, error) {
	data, err := os.ReadFile(repairStatsPath(archigraphDir))
	if err != nil {
		if os.IsNotExist(err) {
			return RepairStats{}, nil
		}
		return RepairStats{}, err
	}
	var s RepairStats
	if err := json.Unmarshal(data, &s); err != nil {
		return RepairStats{}, fmt.Errorf("parse repair_stats.json: %w", err)
	}
	return s, nil
}

// WriteRepairStats writes repair_stats.json to the archigraph dir.
// Always-emit-on-read so audit history is preserved even when no repairs
// applied. The on-disk bytes are stable across runs of the same input
// (see sort.SliceStable above + the schema_version pin).
func WriteRepairStats(archigraphDir string, stats RepairStats) error {
	if err := os.MkdirAll(archigraphDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(repairStatsPath(archigraphDir), data, 0o644)
}

// RepairEdgeID exposes the writer-side hash for callers that need to
// reproduce edge_ids outside this package (the indexer's apply hook
// computes one per relationship). Keeping the name capitalised here while
// the unexported helper stays for #544's internal use means a future
// refactor can move the implementation without breaking either side.
func RepairEdgeID(fromID, relation, originalStub string) string {
	return repairEdgeID(fromID, relation, originalStub)
}
