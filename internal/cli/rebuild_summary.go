package cli

// rebuild_summary.go — client-side post-rebuild summary computation and rendering.
//
// After `archigraph rebuild` completes the daemon has written a fresh graph.fb
// (and optional graph.json) plus enrichment-candidates.json into each repo's
// .archigraph/ directory and a <group>-links.json under ~/.archigraph/groups/.
// This file reads those artefacts to build the rich summary table requested in
// issue #989, without requiring any daemon protocol changes.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
)

// RebuildSummary is the aggregated post-rebuild statistics across all repos
// in a group. Computed client-side by reading the graph artefacts.
type RebuildSummary struct {
	Group string

	// Totals.
	TotalEntities      int
	TotalRelationships int

	// Per-kind breakdowns. Keys are display-normalised entity/relationship Kind values.
	EntityByKind map[string]int
	RelByKind    map[string]int

	// Special counts derived from entity kinds.
	HTTPEndpoints int // entities with kind == "http_endpoint"
	ProcessFlows  int // SCOPE.Process entities emitted by Pass 7

	// Cross-repo edges loaded from <group>-links.json.
	CrossRepoEdges int

	// Candidate counts loaded from each repo's enrichment-candidates.json.
	// EnrichmentCandidates is the number of unique subject entities needing
	// enrichment (one-per-entity, issue #1134). EnrichmentActions is the total
	// number of pending action items across those entities.
	EnrichmentCandidates int // unique subjects (entities) needing enrichment
	EnrichmentActions    int // total pending actions across all subjects
	RepairCandidates     int

	// Orphan proxy — entities with no incoming relationships.
	OrphanEntities int
	OrphanRate     float64 // 0–100

	// Elapsed is the wall-clock duration of the rebuild.
	Elapsed time.Duration
}

// kindRow is one row in a top-N kind table.
type kindRow struct {
	Kind  string
	Count int
}

// topNKinds returns up to n entries from a kind map sorted by count desc, and
// the aggregate "other" total for the remaining entries.
func topNKinds(m map[string]int, n int) ([]kindRow, int) {
	rows := make([]kindRow, 0, len(m))
	for k, v := range m {
		rows = append(rows, kindRow{Kind: k, Count: v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Kind < rows[j].Kind
	})
	if n <= 0 || len(rows) <= n {
		return rows, 0
	}
	other := 0
	for _, r := range rows[n:] {
		other += r.Count
	}
	return rows[:n], other
}

// graphStats mirrors the shape of graph-stats.json written by the indexer.
// It is the primary (cheap) source for entity/relationship totals.
type graphStats struct {
	TotalEntities      int `json:"total_entities"`
	TotalRelationships int `json:"total_relationships"`
}

// loadGraphStats reads <stateDir>/graph-stats.json and returns the totals.
// Returns (0, 0) on any read or parse error so callers always get a safe value.
func loadGraphStats(stateDir string) (entities, rels int) {
	data, err := os.ReadFile(filepath.Join(stateDir, "graph-stats.json"))
	if err != nil {
		return
	}
	var st graphStats
	if err := json.Unmarshal(data, &st); err != nil {
		return
	}
	return st.TotalEntities, st.TotalRelationships
}

// ComputeRebuildSummary loads the per-repo graphs and candidate files produced
// by a rebuild and aggregates them into a RebuildSummary. repoPaths is the list
// of absolute on-disk repo paths that were rebuilt (in order). group is the
// group name, used to locate the group-links.json.
//
// Strategy:
//  1. Read graph-stats.json sidecar for cheap entity/relationship totals (it
//     is always written by the indexer alongside graph.fb).
//  2. Load graph.fb via LoadGraphFromDir for per-kind breakdown, HTTP
//     endpoints, process flows, and orphan computation.
//  3. If LoadGraphFromDir fails but the sidecar succeeded, the totals from the
//     sidecar are preserved so the summary shows real numbers instead of zeros.
//
// Errors reading individual files are silently skipped so a partial result
// (e.g. a repo that failed to index) does not prevent summary generation.
func ComputeRebuildSummary(group string, repoPaths []string, elapsed time.Duration) *RebuildSummary {
	s := &RebuildSummary{
		Group:        group,
		EntityByKind: make(map[string]int),
		RelByKind:    make(map[string]int),
		Elapsed:      elapsed,
	}

	// hasIncoming tracks entity IDs that appear as ToID in at least one
	// relationship — used to identify orphans.
	hasIncoming := make(map[string]struct{})

	for _, repoPath := range repoPaths {
		stateDir := daemon.StateDirForRepo(repoPath)

		// Primary: read the pre-computed sidecar for totals (fast, no mmap).
		sidecarEnts, sidecarRels := loadGraphStats(stateDir)

		doc, err := graph.LoadGraphFromDir(stateDir)
		if err == nil && doc != nil {
			// Full graph loaded — use it for per-kind detail and orphan computation.
			for _, e := range doc.Entities {
				s.TotalEntities++
				k := normaliseEntityKind(e.Kind)
				s.EntityByKind[k]++
				if e.Kind == "http_endpoint" {
					s.HTTPEndpoints++
				}
				if strings.HasPrefix(e.Kind, "SCOPE.Process") || e.Kind == "process" {
					s.ProcessFlows++
				}
			}
			for _, r := range doc.Relationships {
				s.TotalRelationships++
				s.RelByKind[r.Kind]++
				if r.ToID != "" {
					hasIncoming[r.ToID] = struct{}{}
				}
			}
			// Orphans — entities in this repo with no incoming relationship.
			for _, e := range doc.Entities {
				if _, ok := hasIncoming[e.ID]; !ok {
					s.OrphanEntities++
				}
			}
		} else if sidecarEnts > 0 || sidecarRels > 0 {
			// Graph load failed (e.g. FB decode error) but the sidecar is present.
			// Fall back to sidecar totals so the summary shows real numbers.
			s.TotalEntities += sidecarEnts
			s.TotalRelationships += sidecarRels
		}

		enrichSubjects, enrichActions, repairCount := loadCandidateCounts(stateDir)
		s.EnrichmentCandidates += enrichSubjects
		s.EnrichmentActions += enrichActions
		s.RepairCandidates += repairCount
	}

	if s.TotalEntities > 0 {
		s.OrphanRate = 100.0 * float64(s.OrphanEntities) / float64(s.TotalEntities)
	}

	s.CrossRepoEdges = loadCrossRepoEdgeCount(group)

	return s
}

// normaliseEntityKind maps raw entity Kind values to the display names used in
// the summary table (Function, Class, Variable, HTTPEndpoint, or pass-through).
func normaliseEntityKind(kind string) string {
	switch kind {
	case "function", "method":
		return "Function"
	case "class", "struct", "interface":
		return "Class"
	case "variable", "constant", "field":
		return "Variable"
	case "http_endpoint":
		return "HTTPEndpoint"
	default:
		return kind
	}
}

// loadCandidateCounts reads enrichment-candidates.json and returns
// (enrichSubjects, enrichActions, repairCount).
//
// enrichSubjects is the number of distinct SubjectIDs among non-repair
// candidates — the "X entities need enrichment" display count (#1134).
// enrichActions is the total number of non-repair candidate rows (total
// pending actions across all subjects). repairCount is the total number of
// repair-kind rows.
//
// All three are zero on any read/parse error.
func loadCandidateCounts(stateDir string) (enrichSubjects, enrichActions, repair int) {
	path := filepath.Join(stateDir, "enrichment-candidates.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	// The file is a bare JSON array of candidate objects (standard shape).
	var arr []struct {
		Kind      string `json:"kind"`
		SubjectID string `json:"subject_id"`
	}
	if err := json.Unmarshal(data, &arr); err != nil {
		// Try object envelope {"candidates": [...]} used by some older emitters.
		var obj struct {
			Candidates []struct {
				Kind      string `json:"kind"`
				SubjectID string `json:"subject_id"`
			} `json:"candidates"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil {
			return
		}
		arr = obj.Candidates
	}
	seenSubjects := make(map[string]struct{})
	for _, c := range arr {
		if c.Kind == "repair_edge" {
			repair++
		} else {
			enrichActions++
			if c.SubjectID != "" {
				seenSubjects[c.SubjectID] = struct{}{}
			}
		}
	}
	enrichSubjects = len(seenSubjects)
	return
}

// loadCrossRepoEdgeCount reads the group-links.json and returns the number of
// confirmed cross-repo edges. Returns 0 on any error.
func loadCrossRepoEdgeCount(group string) int {
	// Locate via daemon layout so ARCHIGRAPH_DAEMON_ROOT test overrides are
	// respected.
	layout, err := daemon.DefaultLayout()
	if err != nil {
		return 0
	}
	// Links files live at <root>/groups/<group>-links.json.
	linksPath := filepath.Join(layout.Root, "groups", group+"-links.json")
	return countLinksFile(linksPath)
}

// countLinksFile reads a links.json and returns the number of link entries.
func countLinksFile(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	var doc struct {
		Links []json.RawMessage `json:"links"`
	}
	if err := json.NewDecoder(f).Decode(&doc); err != nil {
		return 0
	}
	return len(doc.Links)
}

// PrintRebuildSummary writes the human-readable post-rebuild summary table to w.
// The format matches the specification in issue #989.
func PrintRebuildSummary(w io.Writer, s *RebuildSummary) {
	elapsed := fmtDuration(s.Elapsed)
	fmt.Fprintf(w, "\nGroup '%s' rebuilt (%s)\n", s.Group, elapsed)

	// --- Entities ---
	fmt.Fprintf(w, "\nEntities (%s total):\n", fmtInt(s.TotalEntities))
	topEnts, otherEnts := topNKinds(s.EntityByKind, 5)
	colW := maxKindLen(topEnts, otherEnts > 0)
	for _, row := range topEnts {
		fmt.Fprintf(w, "  %-*s  %s\n", colW, row.Kind, fmtInt(row.Count))
	}
	if otherEnts > 0 {
		fmt.Fprintf(w, "  %-*s  %s\n", colW, "Other", fmtInt(otherEnts))
	}

	// --- Relationships ---
	fmt.Fprintf(w, "\nRelationships (%s total):\n", fmtInt(s.TotalRelationships))
	topRels, otherRels := topNKinds(s.RelByKind, 5)
	colWR := maxKindLen(topRels, otherRels > 0)
	for _, row := range topRels {
		fmt.Fprintf(w, "  %-*s  %s\n", colWR, row.Kind, fmtInt(row.Count))
	}
	if otherRels > 0 {
		fmt.Fprintf(w, "  %-*s  %s\n", colWR, "Other", fmtInt(otherRels))
	}

	// --- Derived stats ---
	fmt.Fprintf(w, "\nCross-repo edges:       %s\n", fmtInt(s.CrossRepoEdges))
	fmt.Fprintf(w, "Process flows:          %s\n", fmtInt(s.ProcessFlows))
	fmt.Fprintf(w, "HTTP endpoints:         %s\n", fmtInt(s.HTTPEndpoints))
	if s.EnrichmentActions > s.EnrichmentCandidates {
		fmt.Fprintf(w, "Enrichment candidates:  %s entities (%s pending actions)\n",
			fmtInt(s.EnrichmentCandidates), fmtInt(s.EnrichmentActions))
	} else {
		fmt.Fprintf(w, "Enrichment candidates:  %s\n", fmtInt(s.EnrichmentCandidates))
	}
	fmt.Fprintf(w, "Repair candidates:      %s\n", fmtInt(s.RepairCandidates))
	if s.TotalEntities > 0 {
		fmt.Fprintf(w, "Orphan entities:        %s (%.1f%%)\n",
			fmtInt(s.OrphanEntities), s.OrphanRate)
	}
}

// maxKindLen returns the maximum string length of Kind values in rows, with a
// minimum of 5 (length of "Other") when withOther is true.
func maxKindLen(rows []kindRow, withOther bool) int {
	w := 0
	if withOther {
		w = 5 // len("Other")
	}
	for _, r := range rows {
		if len(r.Kind) > w {
			w = len(r.Kind)
		}
	}
	return w
}

// fmtInt formats a non-negative integer with comma thousands separators.
func fmtInt(n int) string {
	if n < 0 {
		return "-" + fmtInt(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	out := make([]byte, 0, len(s)+len(s)/3)
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out)
}
