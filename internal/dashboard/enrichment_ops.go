package dashboard

// enrichment_ops.go — per-surface application of LLM enrichment operations.
//
// Issue #1103 — finish the LLM-enrichment epic across Paths + Flows + Topology
// by translating the per-entity YAML frontmatter fields (disqualified,
// merged_into, rank, group, group_label) into actual graph-data OPERATIONS:
//
//   * merge       — two candidate IDs declared equivalent; the graph collapses
//                   them into one canonical entity on the surface.
//   * disqualify  — a candidate flagged as not-a-real-X; hidden from the main
//                   list and surfaced under a separate "rejected" tab/array.
//   * rank        — explicit score override; entries with explicit ranks float
//                   to the top of the surface ordering (ties fall back to the
//                   default sort).
//   * group       — candidates clustered under a label; emitted in a per-
//                   surface enrichment_summary for the dashboard sidebars.
//
// The enrichment store IS the set of doc files referenced by docgen-state.json
// (the same store the existing surfaces already read via
// extractEnrichmentFromFile / extractFlowDocs / applyTopologyEnrichment).
// EnrichmentOps is the cached, indexed view of that store.

import (
	"os"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/mcp"
)

// EnrichmentOps is an indexed, per-group view of LLM enrichment operations.
//
// All maps are keyed by the entity ID exactly as it appears on the surface
// (prefixed `<repo>:<localID>` for Paths/Flows; bare `<localID>` for Topology
// — callers may pre-strip the prefix before looking up if needed).
type EnrichmentOps struct {
	// Disqualified is the set of entity IDs flagged as not-a-real-X.
	// Applies to Paths, Flows, Topology.
	Disqualified map[string]bool

	// MergedInto maps an entity ID to its canonical replacement. Chains are
	// resolved at load time so callers can perform a single lookup.
	// E.g. A→B and B→C collapses to A→C and B→C.
	MergedInto map[string]string

	// Ranks holds explicit numeric overrides. Higher == surfaces first.
	Ranks map[string]float64

	// Groups maps entity ID → group key.
	Groups map[string]string

	// GroupLabels maps a group key → human-readable label (group_label).
	// Set once per group (first wins; conflicts ignored).
	GroupLabels map[string]string
}

// NewEnrichmentOps returns an empty, non-nil EnrichmentOps. Callers can use
// the zero-value methods safely (every Apply* is a no-op for an empty store).
func NewEnrichmentOps() *EnrichmentOps {
	return &EnrichmentOps{
		Disqualified: map[string]bool{},
		MergedInto:   map[string]string{},
		Ranks:        map[string]float64{},
		Groups:       map[string]string{},
		GroupLabels:  map[string]string{},
	}
}

// LoadEnrichmentOps walks every doc file referenced by docgenState, parses its
// YAML frontmatter, and indexes the operations by entity_id.
//
// docgenState may be nil — that case returns an empty (but non-nil) EnrichmentOps
// so callers can apply unconditionally.
//
// The pathResolver maps a raw docPath (as stored in docgenState.GeneratedPaths)
// to an absolute filesystem path. Tests pass a stub; the real callers wrap
// getDocFilePath(group, ...).
func LoadEnrichmentOps(docgenState *mcp.DocgenState, pathResolver func(string) string) *EnrichmentOps {
	ops := NewEnrichmentOps()
	if docgenState == nil || docgenState.GeneratedPaths == nil || pathResolver == nil {
		return ops
	}

	for _, docPath := range docgenState.GeneratedPaths {
		fullPath := pathResolver(docPath)
		fm := parseFrontmatterFile(fullPath)
		if fm == nil || fm.EntityID == "" {
			continue
		}
		id := fm.EntityID
		if fm.Disqualified {
			ops.Disqualified[id] = true
		}
		if fm.MergedInto != "" {
			ops.MergedInto[id] = fm.MergedInto
		}
		if fm.Rank != 0 {
			ops.Ranks[id] = fm.Rank
		}
		if fm.Group != "" {
			ops.Groups[id] = fm.Group
			if fm.GroupLabel != "" {
				if _, exists := ops.GroupLabels[fm.Group]; !exists {
					ops.GroupLabels[fm.Group] = fm.GroupLabel
				}
			}
		}
	}

	// Resolve merge chains transitively (A→B, B→C becomes A→C).
	resolveMergeChains(ops.MergedInto)
	return ops
}

// parseFrontmatterFile is a lower-level reader used by the ops loader: unlike
// extractEnrichmentFromFile it does NOT require the document to also carry a
// summary or kind. An LLM-emitted ops-only doc (just entity_id + merged_into,
// for instance) still ingests correctly.
func parseFrontmatterFile(path string) *EnrichmentFrontmatter {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseFrontmatterBytes(data)
}

// resolveMergeChains follows each merged_into target until a non-merged target
// is reached (or a cycle is detected). Cycles short-circuit to the first
// target encountered so we never loop forever.
func resolveMergeChains(merged map[string]string) {
	for src := range merged {
		visited := map[string]bool{src: true}
		dst := merged[src]
		for {
			next, ok := merged[dst]
			if !ok || visited[next] {
				break
			}
			visited[dst] = true
			dst = next
		}
		merged[src] = dst
	}
}

// CanonicalID returns the canonical ID for an entity, following any merge
// chain. Returns the input unchanged when no merge is recorded.
func (ops *EnrichmentOps) CanonicalID(id string) string {
	if ops == nil {
		return id
	}
	if dst, ok := ops.MergedInto[id]; ok {
		return dst
	}
	return id
}

// IsDisqualified reports whether the entity ID was flagged as not-a-real-X.
func (ops *EnrichmentOps) IsDisqualified(id string) bool {
	if ops == nil {
		return false
	}
	return ops.Disqualified[id]
}

// Rank returns the explicit rank override (or 0 if none). Higher == better.
func (ops *EnrichmentOps) Rank(id string) float64 {
	if ops == nil {
		return 0
	}
	return ops.Ranks[id]
}

// Group returns the group key for an entity (empty string when ungrouped).
func (ops *EnrichmentOps) Group(id string) string {
	if ops == nil {
		return ""
	}
	return ops.Groups[id]
}

// ---------------------------------------------------------------------------
// Generic helpers used by all three surfaces.
// ---------------------------------------------------------------------------

// EnrichmentGroupSummary is one row in the per-surface enrichment_summary
// payload — emitted at the top level of Paths/Flows/Topology responses so the
// dashboard sidebars can render the LLM-inferred clusters.
type EnrichmentGroupSummary struct {
	Group string `json:"group"`
	Label string `json:"label,omitempty"`
	Count int    `json:"count"`
}

// SummarizeGroups builds an EnrichmentGroupSummary slice from a list of entity
// IDs by counting per-group membership and attaching group_label when known.
//
// Result is sorted by count desc, then by group key asc (stable).
func (ops *EnrichmentOps) SummarizeGroups(ids []string) []EnrichmentGroupSummary {
	if ops == nil {
		return []EnrichmentGroupSummary{}
	}
	counts := map[string]int{}
	for _, id := range ids {
		if g := ops.Groups[id]; g != "" {
			counts[g]++
		}
	}
	out := make([]EnrichmentGroupSummary, 0, len(counts))
	for g, c := range counts {
		out = append(out, EnrichmentGroupSummary{
			Group: g,
			Label: ops.GroupLabels[g],
			Count: c,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Group < out[j].Group
	})
	return out
}

// ApplyRankSort returns a copy of ids sorted by explicit rank (desc); IDs with
// no explicit rank fall to the end in their original order.
func (ops *EnrichmentOps) ApplyRankSort(ids []string) []string {
	if ops == nil || len(ops.Ranks) == 0 {
		return ids
	}
	out := make([]string, len(ids))
	copy(out, ids)
	sort.SliceStable(out, func(i, j int) bool {
		return ops.Ranks[out[i]] > ops.Ranks[out[j]]
	})
	return out
}

// ---------------------------------------------------------------------------
// Surface-specific appliers.
//
// Each applier accepts the surface's natural data shape, applies all four
// operations, and returns (kept, rejected, mergedAliases, groupSummary).
//
// The applier is intentionally type-erased (operates on map[string]any +
// id-extractor) so it can be reused across slightly different wire-types
// without reflection or duplication.
// ---------------------------------------------------------------------------

// EntryIDOf extracts the stringy entity id from a generic map[string]any entry.
// Returns "" when the entry has no usable identifier.
func EntryIDOf(entry map[string]any) string {
	if entry == nil {
		return ""
	}
	if v, ok := entry["id"].(string); ok && v != "" {
		return v
	}
	if v, ok := entry["process_id"].(string); ok && v != "" {
		return v
	}
	if v, ok := entry["path_hash"].(string); ok && v != "" {
		return v
	}
	return ""
}

// ApplyToEntries applies all four operations to a slice of map[string]any
// entries. The id-extractor parameter selects which key holds the entity ID.
//
// Returns:
//   - kept: entries that are NOT disqualified, with merge-aliases collapsed and
//     sorted by explicit rank (desc, stable).
//   - rejected: entries that ARE disqualified (carries the same shape as kept).
//   - aliases: map of merged-away ID → canonical ID (so callers can record
//     the equivalence on the canonical entry).
//   - groups: enrichment group summary for the kept slice.
//
// Merge semantics: when two entries A and B both appear and A is marked
// merged_into B, the A entry is dropped and B accumulates an "aliases" key
// (list of merged-away IDs). When B is absent the A entry is kept as-is
// (the merge target hasn't been indexed yet).
func (ops *EnrichmentOps) ApplyToEntries(entries []map[string]any) (kept, rejected []map[string]any, aliases map[string]string, groups []EnrichmentGroupSummary) {
	kept = []map[string]any{}
	rejected = []map[string]any{}
	aliases = map[string]string{}
	if ops == nil {
		return entries, rejected, aliases, groups
	}

	// Build a set of present IDs so we can decide whether to drop merged-away
	// entries (we only drop when the canonical target is also present).
	present := map[string]bool{}
	for _, e := range entries {
		if id := EntryIDOf(e); id != "" {
			present[id] = true
		}
	}

	// First pass: partition disqualified vs kept, recording merge aliases.
	for _, entry := range entries {
		id := EntryIDOf(entry)
		if id == "" {
			kept = append(kept, entry)
			continue
		}
		if ops.IsDisqualified(id) {
			entry["disqualified"] = true
			rejected = append(rejected, entry)
			continue
		}
		if dst, merged := ops.MergedInto[id]; merged && dst != id {
			if present[dst] {
				// Drop this entry and remember the alias for the canonical entry.
				aliases[id] = dst
				continue
			}
			// Canonical target not in this surface — keep the entry, but
			// surface the intended merge so the UI can show a hint.
			entry["merged_into"] = dst
		}
		// Surface enrichment fields onto the entry (idempotent — already set
		// by upstream appliers in some flows; harmless when missing).
		if r := ops.Rank(id); r != 0 {
			if _, exists := entry["rank"]; !exists {
				entry["rank"] = r
			}
		}
		if g := ops.Group(id); g != "" {
			if _, exists := entry["group"]; !exists {
				entry["group"] = g
			}
			if lbl := ops.GroupLabels[g]; lbl != "" {
				if _, exists := entry["group_label"]; !exists {
					entry["group_label"] = lbl
				}
			}
		}
		kept = append(kept, entry)
	}

	// Stamp alias lists onto canonical entries (kept).
	if len(aliases) > 0 {
		for _, entry := range kept {
			id := EntryIDOf(entry)
			if id == "" {
				continue
			}
			var matching []string
			for src, dst := range aliases {
				if dst == id {
					matching = append(matching, src)
				}
			}
			if len(matching) > 0 {
				sort.Strings(matching)
				entry["aliases"] = matching
			}
		}
	}

	// Sort kept by explicit rank (desc), stable so existing order survives ties.
	if len(ops.Ranks) > 0 {
		sort.SliceStable(kept, func(i, j int) bool {
			ri := ops.Ranks[EntryIDOf(kept[i])]
			rj := ops.Ranks[EntryIDOf(kept[j])]
			return ri > rj
		})
	}

	// Build group summary over kept IDs.
	keptIDs := make([]string, 0, len(kept))
	for _, e := range kept {
		if id := EntryIDOf(e); id != "" {
			keptIDs = append(keptIDs, id)
		}
	}
	groups = ops.SummarizeGroups(keptIDs)
	return kept, rejected, aliases, groups
}

// ---------------------------------------------------------------------------
// Convenience top-level loader bound to a group name (used by the live
// HTTP handlers; tests can inject the resolver via LoadEnrichmentOps).
// ---------------------------------------------------------------------------

// LoadEnrichmentOpsForGroup is the runtime entry point used by Paths/Flows/
// Topology handlers. It resolves doc paths via getDocFilePath(group, ...).
func LoadEnrichmentOpsForGroup(group string, docgenState *mcp.DocgenState) *EnrichmentOps {
	return LoadEnrichmentOps(docgenState, func(docPath string) string {
		return getDocFilePath(group, docPath)
	})
}

// MatchesEntity reports whether the given entity_id (frontmatter) matches the
// candidate id used on a surface. Topology hashes IDs differently from Paths/
// Flows so callers can opt into looser matching (suffix/contains) here.
func MatchesEntity(frontmatterID, surfaceID string) bool {
	if frontmatterID == "" || surfaceID == "" {
		return false
	}
	if frontmatterID == surfaceID {
		return true
	}
	// Surface IDs are prefixed `<repo>:<localID>`; frontmatter may carry the
	// bare localID. Match on suffix for that case.
	if strings.HasSuffix(surfaceID, ":"+frontmatterID) {
		return true
	}
	if strings.HasSuffix(frontmatterID, ":"+surfaceID) {
		return true
	}
	return false
}
