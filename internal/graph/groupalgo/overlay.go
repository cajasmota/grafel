package groupalgo

// overlay.go — group-algo OVERLAY storage + atomic swap (#5349 A2, epic #5350).
//
// The group-scope algorithm pass (A1) produces per-entity community-ids,
// PageRank, betweenness centrality, god-node / articulation-point flags, plus a
// community summary and corpus stats. A2 persists that result as a SINGLE
// overlay file:
//
//	$GRAFEL_HOME (or ~/.grafel)/groups/<group>-algo.json
//
// living alongside the existing <group>-links*.json sidecars. A single file is
// the key design decision (plan §2.3 option C): one os.Rename = one atomic swap
// point, so a reader either sees the entire previous overlay or the entire new
// one — never a torn read across N files. The write uses the temp-file +
// rename pattern from internal/daemon/algo/cache.go:writeToDisk.
//
// Staleness: the overlay records each source repo's graph.fb mtime
// (source_mtimes[slug] → unix nanos) at compute time. It is stale when ANY
// repo's current graph.fb mtime differs from the stored value — a general/N
// version of cache.go:readFromDisk's single-mtime check. A stale (or absent)
// overlay is NOT applied: consumers fall back to whatever the per-repo graph.fb
// carried (today's per-repo algo values, or the -2/0 sentinels). This makes the
// apply path absence-tolerant — there is NO behavior change until an overlay
// file actually exists (which only A3's scheduler produces in the live daemon).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// EntityOverlay is the per-entity slice of the group-algo result. Fields mirror
// the graph.Entity algo attributes (graph.go:84-89) so the MCP apply step can
// copy them straight onto the in-memory entities by ID.
type EntityOverlay struct {
	CommunityID         int     `json:"community_id"`
	PageRank            float64 `json:"pagerank"`
	Centrality          float64 `json:"centrality"`
	IsGodNode           bool    `json:"is_god_node,omitempty"`
	IsArticulationPoint bool    `json:"is_articulation_point,omitempty"`
}

// Overlay is the on-disk <group>-algo.json document.
type Overlay struct {
	// Group is the group name (informational; the filename is authoritative).
	Group string `json:"group"`
	// ComputedAt is when the group-algo pass that produced this overlay ran.
	ComputedAt time.Time `json:"computed_at"`
	// SourceMtimes records each repo's graph.fb mtime (unix nanos) at compute
	// time. Used for N-way staleness invalidation.
	SourceMtimes map[string]int64 `json:"source_mtimes"`
	// InputHash is the content hash of the community-relevant input graph
	// (graph.CommunityInputHash) of the assembled union this overlay was
	// computed from. The incremental group-algo path (#5309 layer 4) compares it
	// against the freshly assembled union: an equal hash means the deterministic
	// pass would reproduce this overlay byte-for-byte, so the recompute is
	// skipped and this overlay is preserved verbatim (strict parity). Empty on
	// overlays written before this field existed — treated as "always recompute".
	InputHash string `json:"input_hash,omitempty"`
	// Results maps entity id → its group-scope algo values.
	Results map[string]EntityOverlay `json:"results"`
	// Communities is the group community summary (for grafel_clusters /
	// handleListCommunities).
	Communities []graph.CommunityResult `json:"communities,omitempty"`
	// Stats is the corpus-level group-algo stats.
	Stats graph.AlgorithmStats `json:"stats"`
}

// OverlayPath returns the canonical <group>-algo.json path under the grafel
// home, honoring $GRAFEL_HOME. It mirrors the convention used by
// defaultLinksFile (state.go) so the overlay lives next to the -links sidecars.
func OverlayPath(group string) (string, error) {
	h, err := registry.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "groups", group+"-algo.json"), nil
}

// BuildOverlay projects a GroupAlgoResult into the on-disk Overlay shape.
// Returns nil for a nil/empty result so callers can skip writing.
func BuildOverlay(res *GroupAlgoResult) *Overlay {
	if res == nil || res.Results == nil {
		return nil
	}
	r := res.Results
	ov := &Overlay{
		Group:        res.Group,
		ComputedAt:   time.Now().UTC(),
		SourceMtimes: map[string]int64{},
		InputHash:    res.InputHash,
		Results:      make(map[string]EntityOverlay, len(r.CommunityID)),
		Communities:  r.Communities,
		Stats:        r.Stats,
	}
	for slug, mt := range res.SourceMtimes {
		ov.SourceMtimes[slug] = mt
	}
	// Build the per-entity map. CommunityID is the spine: every entity that
	// participated in the pass has a community id (even -1 ungrouped). PageRank
	// and Centrality default to 0 when absent.
	for id, cid := range r.CommunityID {
		ov.Results[id] = EntityOverlay{
			CommunityID:         cid,
			PageRank:            r.PageRank[id],
			Centrality:          r.Centrality[id],
			IsGodNode:           r.GodNodes[id],
			IsArticulationPoint: r.ArticulationPoints[id],
		}
	}
	// Some entities may have a PageRank/Centrality but no community entry in
	// degenerate cases; fold them in so the overlay is a superset.
	for id := range r.PageRank {
		if _, ok := ov.Results[id]; !ok {
			ov.Results[id] = EntityOverlay{
				CommunityID:         -2, // sentinel: not assigned a community
				PageRank:            r.PageRank[id],
				Centrality:          r.Centrality[id],
				IsGodNode:           r.GodNodes[id],
				IsArticulationPoint: r.ArticulationPoints[id],
			}
		}
	}
	return ov
}

// WriteOverlay atomically writes the overlay to <group>-algo.json via a
// temp-file + rename (single-syscall swap; never a torn read). A nil overlay is
// a no-op (nothing to persist — e.g. an empty group).
func WriteOverlay(group string, ov *Overlay) error {
	if ov == nil {
		return nil
	}
	path, err := OverlayPath(group)
	if err != nil {
		return err
	}
	return WriteOverlayTo(path, ov)
}

// WriteOverlayTo is WriteOverlay with an explicit destination path (used by
// tests and by callers that already resolved the path).
func WriteOverlayTo(path string, ov *Overlay) error {
	if ov == nil {
		return nil
	}
	data, err := json.Marshal(ov)
	if err != nil {
		return fmt.Errorf("marshal overlay: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdirall: %w", err)
	}
	// Temp file in the SAME directory as the target so os.Rename is an atomic
	// intra-filesystem swap. A unique suffix (pid) avoids two concurrent writers
	// clobbering each other's temp file mid-write.
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	// atomicRename is a single os.Rename on Unix, and a bounded sharing-violation
	// retry on Windows (atomicrename_windows.go) where renaming over a file a
	// concurrent reader still has open transiently fails with ACCESS_DENIED /
	// ERROR_SHARING_VIOLATION.
	if err := atomicRename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// WriteOverlayFromResult is the convenience A1→A2 bridge: build + write in one
// call. A nil/empty result writes nothing (no-op).
func WriteOverlayFromResult(res *GroupAlgoResult) error {
	if res == nil {
		return nil
	}
	return WriteOverlay(res.Group, BuildOverlay(res))
}

// ReadOverlay loads and validates the overlay for a group. It returns
// (overlay, true) only on a present, well-formed, NON-stale overlay; otherwise
// (nil, false) — absent, corrupt, or stale all collapse to a no-op miss so the
// apply path is absence-tolerant.
//
// currentMtimes maps each repo slug → its current graph.fb mtime (unix nanos).
// The overlay is stale if any slug recorded in source_mtimes has a different
// current mtime (or is missing from currentMtimes — a repo whose graph.fb
// vanished). Repos present in currentMtimes but absent from the overlay's
// source_mtimes do NOT mark it stale on their own (a freshly-added repo simply
// is not yet covered); but the OVERLAY's own recorded sources must all match.
func ReadOverlay(path string, currentMtimes map[string]int64) (*Overlay, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false // absent or unreadable
	}
	var ov Overlay
	if err := json.Unmarshal(data, &ov); err != nil {
		return nil, false // corrupt (e.g. a partial write that — impossibly — leaked)
	}
	if IsOverlayStale(&ov, currentMtimes) {
		return nil, false
	}
	return &ov, true
}

// readOverlayUnconditional loads and unmarshals the overlay at path WITHOUT the
// mtime-staleness check ReadOverlay applies. The incremental group-algo path
// uses the recorded input_hash (a content gate) — not source mtimes — to decide
// whether the prior overlay is reusable, so it must see the overlay even when an
// mtime moved (a docs-only push bumps graph.fb mtime but leaves the community
// input graph, and therefore the hash, unchanged). Returns nil on absent /
// unreadable / corrupt.
func readOverlayUnconditional(path string) *Overlay {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var ov Overlay
	if err := json.Unmarshal(data, &ov); err != nil {
		return nil
	}
	return &ov
}

// overlayToResults reconstitutes a *graph.AlgorithmResults from a stored overlay
// for the skip-when-unaffected path: when the input hash matches, the prior
// overlay is exactly what a full recompute would produce, so we rebuild the
// in-memory result from it rather than re-running the algorithms. The per-entity
// maps are keyed by entity id (only entities present in the current union are
// emitted, so a removed entity does not leak — though a hash match implies the
// node set is identical anyway). SurpriseEndpoints is left empty: it is derived
// from SurpriseEdges, which the overlay does not persist, and no current
// consumer of the group result reads it off the reconstituted skip path (the
// overlay the caller re-writes carries Communities + Stats, the MCP-applied
// fields). The community/PageRank/centrality/flag fields — the ones the overlay
// applies to entities — are fully reconstructed.
func overlayToResults(ov *Overlay, entities []graph.Entity) *graph.AlgorithmResults {
	res := &graph.AlgorithmResults{
		CommunityID:        make(map[string]int, len(ov.Results)),
		Centrality:         make(map[string]float64, len(ov.Results)),
		PageRank:           make(map[string]float64, len(ov.Results)),
		GodNodes:           map[string]bool{},
		ArticulationPoints: map[string]bool{},
		SurpriseEndpoints:  map[string]bool{},
		Communities:        ov.Communities,
		Stats:              ov.Stats,
	}
	present := make(map[string]struct{}, len(entities))
	for i := range entities {
		present[entities[i].ID] = struct{}{}
	}
	for id, eo := range ov.Results {
		if _, ok := present[id]; !ok {
			continue
		}
		res.CommunityID[id] = eo.CommunityID
		res.PageRank[id] = eo.PageRank
		res.Centrality[id] = eo.Centrality
		if eo.IsGodNode {
			res.GodNodes[id] = true
		}
		if eo.IsArticulationPoint {
			res.ArticulationPoints[id] = true
		}
	}
	return res
}

// CurrentSourceMtimes returns each repo's current graph.fb mtime (unix nanos)
// keyed by the group-config slug — the SAME keying the overlay's source_mtimes
// uses. Consumers (the MCP apply path) compare this against the overlay to
// detect staleness without re-deriving slugs themselves. A repo with no
// graph.fb yet is simply absent from the map. An unknown group is an error.
func CurrentSourceMtimes(group string) (map[string]int64, error) {
	cfg, err := resolveGroup(group)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, r := range cfg.Repos {
		stateDir := daemon.StateDirForRepo(r.Path)
		fbPath := filepath.Join(stateDir, "graph.fb")
		if fi, statErr := os.Stat(fbPath); statErr == nil {
			out[r.Slug] = fi.ModTime().UnixNano()
		}
	}
	return out, nil
}

// IsOverlayStale reports whether the overlay no longer matches the current
// on-disk graph.fb mtimes. Generalizes cache.go:readFromDisk's single-mtime
// check to N repos: stale if ANY recorded source repo's current mtime differs
// from the stored value (or the repo is missing from currentMtimes).
func IsOverlayStale(ov *Overlay, currentMtimes map[string]int64) bool {
	if ov == nil {
		return true
	}
	for slug, stored := range ov.SourceMtimes {
		cur, ok := currentMtimes[slug]
		if !ok || cur != stored {
			return true
		}
	}
	return false
}

// OverlayNeedsRecompute reports whether a group has an overlay on disk that has
// gone STALE relative to the current per-repo graph.fb mtimes (#5403). It is the
// settled-group freshness predicate the daemon's overlay sweep uses to decide
// whether to proactively re-arm a group-algo pass.
//
// Crucially it returns false for an ABSENT overlay: a group that has never had a
// group-algo pass should be left to the normal first-compute path (the link-pass
// chain after its first reindex), NOT force-recomputed by the sweep. Only an
// overlay that EXISTS and no longer matches the live graphs is "needs recompute".
// A malformed/unreadable overlay also returns false (don't thrash on garbage;
// the next reindex's pass rewrites it). Any error resolving the group or its
// mtimes returns false — the sweep is best-effort and must never wedge.
func OverlayNeedsRecompute(group string) bool {
	path, err := OverlayPath(group)
	if err != nil || path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// Absent (or unreadable) → not "stale"; let the first-compute path run.
		return false
	}
	var ov Overlay
	if err := json.Unmarshal(data, &ov); err != nil {
		return false // corrupt — a fresh pass will overwrite it; don't thrash.
	}
	cur, err := CurrentSourceMtimes(group)
	if err != nil {
		return false
	}
	return IsOverlayStale(&ov, cur)
}
