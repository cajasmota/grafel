package cli

// rebuild_summary.go — client-side post-rebuild summary computation and rendering.
//
// After `grafel rebuild` completes the daemon has written a fresh graph.fb
// (and optional graph.json) plus enrichment-candidates.json into each repo's
// .grafel/ directory and a <group>-links.json under ~/.grafel/groups/.
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
	"unicode/utf8"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/links"
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
	EnrichmentCandidates int            // unique subjects (entities) needing enrichment
	EnrichmentActions    int            // total pending actions across all subjects
	EnrichmentByKind     map[string]int // action counts by enrichment kind (describe_entity, describe_role, etc)
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
		Group:            group,
		EntityByKind:     make(map[string]int),
		RelByKind:        make(map[string]int),
		EnrichmentByKind: make(map[string]int),
		Elapsed:          elapsed,
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
				// #1217: count all three http endpoint kind strings.
				if e.Kind == "http_endpoint" || e.Kind == "http_endpoint_definition" || e.Kind == "http_endpoint_call" {
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

		enrichSubjects, enrichActions, enrichByKind, repairCount := loadCandidateCounts(stateDir)
		s.EnrichmentCandidates += enrichSubjects
		s.EnrichmentActions += enrichActions
		for kind, count := range enrichByKind {
			s.EnrichmentByKind[kind] += count
		}
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
	// #1217: normalise all three http endpoint kind strings to the same bucket.
	case "http_endpoint", "http_endpoint_definition", "http_endpoint_call":
		return "HTTPEndpoint"
	default:
		return kind
	}
}

// loadCandidateCounts reads enrichment-candidates.json and returns
// (enrichSubjects, enrichActions, enrichByKind, repairCount).
//
// enrichSubjects is the number of distinct SubjectIDs among non-repair
// candidates — the "X entities need enrichment" display count (#1134).
// enrichActions is the total number of non-repair candidate rows (total
// pending actions across all subjects). enrichByKind is a map of enrichment
// kind to count (e.g., describe_entity: 100, describe_role: 50).
// repairCount is the total number of repair-kind rows.
//
// All return values are zero on any read/parse error.
func loadCandidateCounts(stateDir string) (enrichSubjects, enrichActions int, enrichByKind map[string]int, repair int) {
	enrichByKind = make(map[string]int)
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
			enrichByKind[c.Kind]++
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
//
// #6178: this used to resolve via daemon.DefaultLayout().Root, which honors
// GRAFEL_DAEMON_ROOT (or falls back to plain os.UserHomeDir()+".grafel")
// and does NOT consult GRAFEL_HOME — a different override entirely, and a
// silent no-op for the isolated-run case this bug is about. That mismatch
// pre-dates this fix and was already wrong; it just happened to agree with
// the (also broken) writer, so it went unnoticed.
//
// GRAFEL_DAEMON_ROOT governs the DAEMON's own runtime layout — socket,
// pid file, log, and (absent an override) the external graph store — a
// concern that is orthogonal to where a client-invoked link pass writes
// its group sidecars. runLinksForGroup/RunLinksForGroupCtx (internal/cli/
// links.go) write <group>-links.json under registry.HomeDir() (GRAFEL_HOME),
// same as links.PathsFor, mcp.defaultLinksFile, dashboard.defaultLinksFile,
// and groupalgo.OverlayPath. This reader now uses the same resolver so the
// post-rebuild summary reads back exactly what the link pass this rebuild
// triggered just wrote, instead of silently disagreeing under an isolated
// GRAFEL_HOME. (That pass runs in the daemon, not this client process —
// cmd/grafel/main.go's runLinksHook — but it inherits os.Environ()
// verbatim (internal/daemon/supervise.go), so the same GRAFEL_HOME applies
// to both; the two processes just aren't the same process.)
//
// #6178 round 3: now derives from links.PathsFor directly (the shared
// derivation, internal/links/group_paths.go) instead of re-joining
// "groups"/"<group>-links.json" via registry.HomeDir() by hand.
func loadCrossRepoEdgeCount(group string) int {
	paths, err := links.PathsFor("", group)
	if err != nil {
		return 0
	}
	return countLinksFile(paths.Links)
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
	// #6686: every %-*s below — here and in the relationship and enrichment
	// tables — must take its width from maxKindLen and never re-derive one per
	// row. maxKindLen counts runes to match what %-*s pads in; a per-row
	// len(row.Kind) would reintroduce the byte/rune mismatch at that one site
	// and put its line on a different column from the rest of its table.
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

	// --- Enrichment candidates with breakdown ---
	if s.EnrichmentCandidates > 0 {
		pct := 0.0
		if s.TotalEntities > 0 {
			pct = 100.0 * float64(s.EnrichmentCandidates) / float64(s.TotalEntities)
		}
		colorCode := "" // empty by default (no color)
		if pct >= 80 {
			colorCode = "\033[31m" // red
		} else if pct >= 50 {
			colorCode = "\033[33m" // yellow
		}
		resetCode := ""
		if colorCode != "" {
			resetCode = "\033[0m"
		}

		if s.EnrichmentActions > s.EnrichmentCandidates {
			fmt.Fprintf(w, "Enrichment candidates:  %s%s entities%s (%s pending actions, %.1f%% of total)\n",
				colorCode, fmtInt(s.EnrichmentCandidates), resetCode,
				fmtInt(s.EnrichmentActions), pct)
		} else {
			fmt.Fprintf(w, "Enrichment candidates:  %s%s entities%s (%.1f%% of total)\n",
				colorCode, fmtInt(s.EnrichmentCandidates), resetCode, pct)
		}

		// Per-kind breakdown (top 5)
		topEnrich, otherEnrich := topNKinds(s.EnrichmentByKind, 5)
		if len(topEnrich) > 0 {
			fmt.Fprintf(w, "  Action breakdown:\n")
			enrichColW := maxKindLen(topEnrich, otherEnrich > 0)
			for _, row := range topEnrich {
				fmt.Fprintf(w, "    %-*s  %s\n", enrichColW, row.Kind, fmtInt(row.Count))
			}
			if otherEnrich > 0 {
				fmt.Fprintf(w, "    %-*s  %s\n", enrichColW, "Other", fmtInt(otherEnrich))
			}
		}
	}

	fmt.Fprintf(w, "Repair candidates:      %s\n", fmtInt(s.RepairCandidates))
	if s.TotalEntities > 0 {
		fmt.Fprintf(w, "Orphan entities:        %s (%.1f%%)\n",
			fmtInt(s.OrphanEntities), s.OrphanRate)
	}
}

// maxKindLen returns the maximum RUNE count of Kind values in rows, with a
// minimum of 5 (the rune count of "Other") when withOther is true.
//
// #6686: size this column by rune count, not by len()'s byte count. Every
// PrintRebuildSummary call site pads with fmt's %-*s, which counts runes, so a
// byte-derived width overshoots by (bytes - runes).
//
// Two independent things decide this width, and they fail differently. Keep
// the distinction:
//
//   - THE UNIT (bytes vs runes) is oversize-only. %-*s pads and never
//     truncates, and a string's rune count never exceeds its byte count, so a
//     byte-derived width is never smaller than any row needs. Every row of a
//     table shares this one width and stays in agreement; the whole column is
//     simply too wide and every payload shifts right.
//
//   - THE FLOOR (withOther) can genuinely make the rows ragged. If withOther
//     is wrongly false while every kind is shorter than "Other", the returned
//     width is below 5, and the "Other" row — which is not one of rows — then
//     overflows the column while its siblings are padded to the smaller
//     width. That is the one case in this function where rows inside a single
//     table disagree with each other. It is the reason withOther is a
//     parameter and not something the caller may drop.
//
// This targets RUNE COUNT, NOT TERMINAL DISPLAY WIDTH. The two are not the
// same: a CJK ideograph is one rune but occupies two terminal columns, and an
// emoji may be one rune and two columns, or a grapheme cluster spanning
// several runes. So this aligns the common European-accent case ("cafe" with
// an acute e) exactly, and a kind containing CJK or emoji STILL renders
// ragged. Correcting that needs a wcwidth table or grapheme segmentation, a
// dependency this table does not justify; it is deliberately not done here.
//
// Today the unit mismatch is inert for the entity and relationship tables,
// because every kind they render is an ASCII constant — see
// TestKindConstantsAreASCII, which reads that set from internal/types/kinds.go
// and fails the day a non-ASCII kind is declared. The enrichment breakdown is
// the exception and is NOT inert: its kinds are free-form strings read out of
// enrichment-candidates.json, no invariant covers them, and a non-ASCII one is
// reachable today. The arithmetic has to be right rather than merely lucky.
func maxKindLen(rows []kindRow, withOther bool) int {
	w := 0
	if withOther {
		w = utf8.RuneCountInString("Other")
	}
	for _, r := range rows {
		if n := utf8.RuneCountInString(r.Kind); n > w {
			w = n
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
