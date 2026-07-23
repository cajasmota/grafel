package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// StatusSummary aggregates per-repo and per-group statistics for the status
// command. Computed client-side by reading the graph-stats.json sidecars.
type StatusSummary struct {
	RepoStats map[string]*RepoStatus // keyed by repo slug
	GroupName string

	// Aggregated totals.
	TotalEntities        int
	TotalRelationships   int
	EnrichmentCandidates int
	RepairCandidates     int

	// Cross-repo edges loaded from <group>-links.json.
	CrossRepoEdges int

	// Flows and endpoints are derived from entity kinds during graph reading.
	HTTPEndpoints int
	ProcessFlows  int
}

// RepoStatus contains per-repo statistics.
type RepoStatus struct {
	Slug           string
	Path           string
	Entities       int
	Relationships  int
	Files          int
	LastIndexed    time.Time
	LastIndexedAge string // formatted duration like "5m ago"

	// Phase 0 git metadata (#2088). Empty when the graph predates this
	// feature or was built from a non-git directory.
	IndexedRef string // branch name, or "" for detached HEAD / non-git
	IndexedSHA string // 12-char abbreviated commit hash, or ""
	IsWorktree bool   // true when the repo was a linked git worktree at index time

	// PH1c (#2087): cold refs — other refs that have a graph on disk but
	// are not the currently active (hot) ref. Nil when none exist.
	ColdRefs []string

	// RebuildFailure is the "last rebuild FAILED" marker read from the
	// status-plane sidecar (internal/statusfile), if any (#5822 sub-ask 3).
	// Non-nil means the most recent rebuild attempt for this repo hard-failed
	// (e.g. the per-repo watchdog SIGKILL) — surfaced by PrintStatusSummary so
	// this is never silent. It does NOT mean the graph above is stale/wrong;
	// LastIndexed/Entities/etc. above may still reflect an OLDER successful
	// run, and this marker sits alongside it as an additional warning. Cleared
	// (nil again) once a subsequent rebuild of this repo succeeds.
	RebuildFailure *statusfile.RebuildFailure
}

// ComputeStatusSummary loads the per-repo graph-stats.json files and enrichment
// candidate counts for a group, aggregating them into a StatusSummary.
// Errors reading individual files are silently skipped so a partial result
// does not prevent summary generation.
func ComputeStatusSummary(group string, repos []registry.Repo) *StatusSummary {
	s := &StatusSummary{
		GroupName: group,
		RepoStats: make(map[string]*RepoStatus),
	}

	// Track entities with incoming relationships to compute orphan rate later.
	hasIncoming := make(map[string]bool)

	for _, r := range repos {
		rs := &RepoStatus{
			Slug:           r.Slug,
			Path:           r.Path,
			LastIndexedAge: "(never)",
		}

		// #5822 sub-ask 3: read the status-plane sidecar for a "last rebuild
		// FAILED" marker (e.g. the per-repo watchdog SIGKILL). This is a plain
		// file read — no daemon dial required — so it works even when the
		// daemon is down, same as the rest of this registry-based summary.
		// Absent/unreadable is the normal "no known failure" case, never an
		// error worth surfacing here.
		if sf, sfErr := statusfile.Read(r.Path); sfErr == nil && sf != nil {
			rs.RebuildFailure = sf.LastRebuildFailure
		}

		stateDir := daemon.StateDirForRepo(r.Path)

		// Load graph-stats.json sidecar for basic counts.
		sidecarPath := filepath.Join(stateDir, "graph-stats.json")
		if data, err := os.ReadFile(sidecarPath); err == nil {
			var side graph.GraphStatsSidecar
			if json.Unmarshal(data, &side) == nil {
				rs.Entities = side.TotalEntities
				rs.Relationships = side.TotalRelationships
				rs.Files = side.TotalFiles // real indexed file count (#1559)
				if !side.ComputedAt.IsZero() {
					rs.LastIndexed = side.ComputedAt
					rs.LastIndexedAge = formatTimeSince(side.ComputedAt)
				}
				s.TotalEntities += side.TotalEntities
				s.TotalRelationships += side.TotalRelationships
			}
		} else if ps, ok := graph.PersistedStatsFromDir(stateDir); ok {
			// Sidecar not yet written (e.g. a graph produced by the daemon's
			// incremental reindex path, which writes graph.fb but no sidecar).
			// Read the real counts + index timestamp cheaply from the graph.fb
			// header so a cold-but-indexed repo reports its true size instead of
			// "0 entities / (never)" (#5442).
			rs.Entities = ps.Entities
			rs.Relationships = ps.Relationships
			s.TotalEntities += ps.Entities
			s.TotalRelationships += ps.Relationships
			if !ps.ComputedAt.IsZero() {
				rs.LastIndexed = ps.ComputedAt
				rs.LastIndexedAge = formatTimeSince(ps.ComputedAt)
			} else if graphPath, modtimeNano := daemon.FindGraphFile(r.Path); graphPath != "" {
				mtime := time.Unix(0, modtimeNano)
				rs.LastIndexed = mtime
				rs.LastIndexedAge = formatTimeSince(mtime)
			}
		} else {
			// graph.fb absent/unreadable — fall back to newest graph file mtime.
			graphPath, modtimeNano := daemon.FindGraphFile(r.Path)
			if graphPath != "" {
				mtime := time.Unix(0, modtimeNano)
				rs.LastIndexed = mtime
				rs.LastIndexedAge = formatTimeSince(mtime)
			}
		}

		// Load full graph document to extract entities with incoming rels + kind-based counts.
		// Errors are silently skipped — graph may not exist yet or may be invalid.
		// This is only for computing derived counts like HTTPEndpoints and ProcessFlows;
		// the main entity/relationship counts come from graph-stats.json.
		func() {
			defer func() {
				// Catch panics from malformed graph files (e.g., in tests).
				if r := recover(); r != nil {
					// Silently ignore panics during graph loading.
				}
			}()
			doc, err := graph.LoadGraphFromDir(stateDir)
			if err == nil && doc != nil {
				rs.Files = doc.Stats.Files
				// Phase 0 git metadata (#2088).
				rs.IndexedRef = doc.IndexedRef
				rs.IndexedSHA = doc.IndexedSHA
				rs.IsWorktree = doc.IsWorktree
				for _, e := range doc.Entities {
					// #1217: count all three http endpoint kind strings.
					if e.Kind == "http_endpoint" || e.Kind == "http_endpoint_definition" || e.Kind == "http_endpoint_call" {
						s.HTTPEndpoints++
					}
					// Check for process flows: SCOPE.Process or process kind.
					if e.Kind == "process" || strings.HasPrefix(e.Kind, "SCOPE.Process") {
						s.ProcessFlows++
					}
				}
				// Track which entities have incoming relationships.
				for _, rel := range doc.Relationships {
					if rel.ToID != "" {
						hasIncoming[rel.ToID] = true
					}
				}
			}
		}()

		// Load enrichment + repair candidate counts.
		// enrichSubjects = unique entities needing enrichment (#1134).
		enrichSubjects, _, _, repairCount := loadCandidateCounts(stateDir)
		s.EnrichmentCandidates += enrichSubjects
		s.RepairCandidates += repairCount

		// PH1c (#2087): discover cold refs — ref directories that have a
		// graph.fb on disk but are not the currently-active (hot) ref.
		// stateDir already points at the hot ref slot; its parent is refs/.
		refsDir := filepath.Dir(stateDir)
		if entries, dirErr := os.ReadDir(refsDir); dirErr == nil {
			hotRefSafe := filepath.Base(stateDir)
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				refSafe := entry.Name()
				if refSafe == hotRefSafe {
					continue // skip the hot ref
				}
				// Only include slots that actually have a graph.
				// #5915 J2 P2: resolve via the segment-aware descriptor.
				// os.Stat(graph.CurrentGraphPath(...)) — the pattern this
				// replaces — only ever resolves a flat .fb path, which is
				// absent for a segment-set ref (graph.<gen>/ dir +
				// manifest.json, no flat .fb), so a fully-indexed segmented
				// cold ref would be silently omitted from ColdRefs.
				refStateDir := filepath.Join(refsDir, refSafe)
				if desc, dErr := graph.CurrentGraphDescriptor(refStateDir); dErr == nil && desc.Kind != graph.GraphAbsent {
					rs.ColdRefs = append(rs.ColdRefs, daemon.RefSafeDecode(refSafe))
				}
			}
			sort.Strings(rs.ColdRefs)
		}

		s.RepoStats[r.Slug] = rs
	}

	s.CrossRepoEdges = loadCrossRepoEdgeCount(group)

	return s
}

// formatTimeSince returns a human-readable duration like "5m ago" relative to now.
func formatTimeSince(t time.Time) string {
	if t.IsZero() {
		return "(never)"
	}
	since := time.Since(t).Truncate(time.Second)
	if since < 0 {
		since = 0
	}
	if since < time.Minute {
		return fmt.Sprintf("%ds ago", int(since.Seconds()))
	}
	if since < time.Hour {
		m := int(since.Minutes())
		return fmt.Sprintf("%dm ago", m)
	}
	if since < 24*time.Hour {
		h := int(since.Hours())
		m := int(since.Minutes()) - h*60
		if m == 0 {
			return fmt.Sprintf("%dh ago", h)
		}
		return fmt.Sprintf("%dh%dm ago", h, m)
	}
	d := int(since.Hours()) / 24
	return fmt.Sprintf("%dd ago", d)
}

// formatGitRef builds the "@ main (abc12345)" suffix for a repo status line.
// Returns "" when no SHA is available (pre-#2088 graph or non-git repo).
func formatGitRef(ref, sha string, isWorktree bool) string {
	if sha == "" {
		return ""
	}
	label := ref
	if label == "" {
		label = "detached"
	}
	s := fmt.Sprintf(" @ %s (%s)", label, sha)
	if isWorktree {
		s += " [worktree]"
	}
	return s
}

// formatColdRefs builds a " [+ feat-X, feat-Y cold]" suffix listing cold refs.
// Returns "" when there are no cold refs.
func formatColdRefs(refs []string) string {
	if len(refs) == 0 {
		return ""
	}
	// Show at most 3 names to keep lines readable; append "…" when truncated.
	shown := refs
	suffix := ""
	if len(refs) > 3 {
		shown = refs[:3]
		suffix = fmt.Sprintf(", +%d more", len(refs)-3)
	}
	names := ""
	for i, r := range shown {
		if i > 0 {
			names += ", "
		}
		names += r
	}
	return fmt.Sprintf(" [+ %s%s cold]", names, suffix)
}

// formatRebuildFailureRef builds the " (ref … / sha …)" suffix for a
// last-rebuild-FAILED line, describing what the failed rebuild was targeting.
// Returns "" when neither is known (e.g. a non-git repo).
func formatRebuildFailureRef(rf *statusfile.RebuildFailure) string {
	if rf.Ref == "" && rf.Commit == "" {
		return ""
	}
	switch {
	case rf.Ref != "" && rf.Commit != "":
		return fmt.Sprintf(" (ref %s / sha %s)", rf.Ref, rf.Commit)
	case rf.Ref != "":
		return fmt.Sprintf(" (ref %s)", rf.Ref)
	default:
		return fmt.Sprintf(" (sha %s)", rf.Commit)
	}
}

// PrintStatusSummary writes the per-group and per-repo statistics to w.
// The format includes per-repo statistics aligned in columns, followed by aggregated totals.
func PrintStatusSummary(w io.Writer, s *StatusSummary) {
	fmt.Fprintf(w, "\nGroup: %s\n", s.GroupName)

	// Sort repos by slug for consistent output.
	slugs := make([]string, 0, len(s.RepoStats))
	for slug := range s.RepoStats {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	// Find column widths.
	maxSlugLen := 0
	for _, slug := range slugs {
		if len(slug) > maxSlugLen {
			maxSlugLen = len(slug)
		}
	}
	// Minimum width for "Slug" column.
	if maxSlugLen < 4 {
		maxSlugLen = 4
	}

	// Print each repo on one line.
	for _, slug := range slugs {
		rs := s.RepoStats[slug]
		gitSuffix := formatGitRef(rs.IndexedRef, rs.IndexedSHA, rs.IsWorktree)
		coldSuffix := formatColdRefs(rs.ColdRefs)
		fmt.Fprintf(w, "  %-*s  %5s files  %6s entities  %6s rels  indexed %s%s%s\n",
			maxSlugLen, slug,
			fmtInt(rs.Files),
			fmtInt(rs.Entities),
			fmtInt(rs.Relationships),
			rs.LastIndexedAge,
			gitSuffix,
			coldSuffix)
		// #5822 sub-ask 3: a watchdog SIGKILL (or any other hard rebuild
		// failure) must never be silent. This line surfaces ADDITIONALLY to
		// the (possibly still-good, older) graph state printed above — it is
		// a warning, not a replacement for it.
		if rf := rs.RebuildFailure; rf != nil {
			fmt.Fprintf(w, "  %-*s  ⚠ last rebuild FAILED: %s%s — see daemon.err; raise GRAFEL_REBUILD_REPO_TIMEOUT (or `grafel rebuild --timeout <dur>`) or rebuild again\n",
				maxSlugLen, "", rf.Reason, formatRebuildFailureRef(rf))
		}
	}

	// Print aggregated totals.
	fmt.Fprintf(w, "\n  TOTAL: %s entities · %s relationships · %s cross-repo edges · %s flows · %s endpoints\n",
		fmtInt(s.TotalEntities),
		fmtInt(s.TotalRelationships),
		fmtInt(s.CrossRepoEdges),
		fmtInt(s.ProcessFlows),
		fmtInt(s.HTTPEndpoints))

	// Print available optional candidates.
	//
	// #5693: these are OPTIONAL LLM-enrichment/repair opportunities recomputed
	// from the current graph — not a live daemon queue. Nothing auto-drains
	// them (they cost tokens; the user runs enrichment to apply). The wording
	// deliberately avoids "Pending", which reads as a stuck/blocked queue.
	if s.EnrichmentCandidates > 0 || s.RepairCandidates > 0 {
		fmt.Fprintf(w, "  Available (optional; run enrichment to apply — nothing auto-drains): %s enrichment opportunities · %s repair candidates\n",
			fmtInt(s.EnrichmentCandidates),
			fmtInt(s.RepairCandidates))
	}
}
