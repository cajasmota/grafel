package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
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
		} else {
			// Fallback to graph.fb mtime if sidecar doesn't exist.
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
					if e.Kind == "process" || (len(e.Kind) > 14 && e.Kind[:6] == "SCOPE." && e.Kind[6:] == "Process") {
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
		fmt.Fprintf(w, "  %-*s  %5s files  %6s entities  %6s rels  indexed %s%s\n",
			maxSlugLen, slug,
			fmtInt(rs.Files),
			fmtInt(rs.Entities),
			fmtInt(rs.Relationships),
			rs.LastIndexedAge,
			gitSuffix)
	}

	// Print aggregated totals.
	fmt.Fprintf(w, "\n  TOTAL: %s entities · %s relationships · %s cross-repo edges · %s flows · %s endpoints\n",
		fmtInt(s.TotalEntities),
		fmtInt(s.TotalRelationships),
		fmtInt(s.CrossRepoEdges),
		fmtInt(s.ProcessFlows),
		fmtInt(s.HTTPEndpoints))

	// Print pending candidates.
	if s.EnrichmentCandidates > 0 || s.RepairCandidates > 0 {
		fmt.Fprintf(w, "  Pending: %s enrichment candidates · %s repair candidates\n",
			fmtInt(s.EnrichmentCandidates),
			fmtInt(s.RepairCandidates))
	}
}
