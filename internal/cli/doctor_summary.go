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

// DoctorRepoHealth summarizes the health of a single repo within a group.
type DoctorRepoHealth struct {
	Slug           string
	Path           string
	Status         string // "OK" or "STALE" or "MISSING"
	LastIndexed    time.Time
	LastIndexedAge string
	Entities       int
	Relationships  int
	CrossRepoEdges int
}

// DoctorGroupHealth aggregates health metrics for a group and all its repos.
type DoctorGroupHealth struct {
	GroupName string
	Healthy   bool
	Status    string // "HEALTHY", "DEGRADED", "FAILED"

	// Daemon management
	DaemonManaged bool // true if group has a corresponding watcher

	// Watcher stats (if available)
	WatcherRepoCount     int
	WatcherDirCount      int
	WatcherEventsDropped int
	LastWatcherActivity  string

	// Per-repo stats
	Repos []*DoctorRepoHealth

	// Aggregated quality metrics
	TotalEntities       int
	TotalRelationships  int
	TotalCrossRepoEdges int
	BugRate             float64 // unresolved-edges percentage
	OrphanEntities      int
	OrphanRate          float64
	PendingRepairs      int
	PendingEnrichments  int

	// Issues found
	IssuesFound []string // human-readable issue descriptions
}

// ComputeDoctorHealth aggregates daemon and group health into a comprehensive report.
// It reads graph files, candidate counts, and watcher state for each group.
func ComputeDoctorHealth(groups []registry.GroupRef) []*DoctorGroupHealth {
	var result []*DoctorGroupHealth

	for _, g := range groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}

		health := &DoctorGroupHealth{
			GroupName:   g.Name,
			Healthy:     true,
			Status:      "HEALTHY",
			Repos:       make([]*DoctorRepoHealth, 0),
			IssuesFound: make([]string, 0),
		}

		// Aggregate per-repo health
		for _, r := range cfg.Repos {
			rh := computeRepoHealth(r)
			health.Repos = append(health.Repos, rh)
			health.TotalEntities += rh.Entities
			health.TotalRelationships += rh.Relationships
			health.TotalCrossRepoEdges += rh.CrossRepoEdges

			// Track stale repos
			if rh.Status == "STALE" {
				health.Healthy = false
				health.IssuesFound = append(health.IssuesFound,
					fmt.Sprintf("repo %s hasn't been indexed in >24h (last: %s)", rh.Slug, rh.LastIndexedAge))
			}
		}

		// Sort repos by slug for consistent output
		sort.Slice(health.Repos, func(i, j int) bool {
			return health.Repos[i].Slug < health.Repos[j].Slug
		})

		// Compute aggregated quality metrics
		computeQualityMetrics(health)

		// Determine overall status
		if !health.Healthy {
			health.Status = "DEGRADED"
		}

		result = append(result, health)
	}

	return result
}

// computeRepoHealth assembles the health snapshot for a single repo.
func computeRepoHealth(r registry.Repo) *DoctorRepoHealth {
	rh := &DoctorRepoHealth{
		Slug:           r.Slug,
		Path:           r.Path,
		Status:         "OK",
		LastIndexedAge: "(never)",
	}

	// Check if repo path exists
	if _, err := os.Stat(r.Path); err != nil {
		rh.Status = "MISSING"
		return rh
	}

	stateDir := daemon.StateDirForRepo(r.Path)

	// Load graph-stats.json sidecar for basic counts
	sidecarPath := filepath.Join(stateDir, "graph-stats.json")
	if data, err := os.ReadFile(sidecarPath); err == nil {
		var side graph.GraphStatsSidecar
		if json.Unmarshal(data, &side) == nil {
			rh.Entities = side.TotalEntities
			rh.Relationships = side.TotalRelationships
			if !side.ComputedAt.IsZero() {
				rh.LastIndexed = side.ComputedAt
				rh.LastIndexedAge = formatTimeSince(side.ComputedAt)
				// Mark as stale if not indexed in >24h
				if time.Since(rh.LastIndexed) > 24*time.Hour {
					rh.Status = "STALE"
				}
			}
		}
	} else {
		// Fallback to graph.fb mtime
		graphPath, modtimeNano := daemon.FindGraphFile(r.Path)
		if graphPath != "" {
			mtime := time.Unix(0, modtimeNano)
			rh.LastIndexed = mtime
			rh.LastIndexedAge = formatTimeSince(mtime)
			if time.Since(rh.LastIndexed) > 24*time.Hour {
				rh.Status = "STALE"
			}
		}
	}

	// Load full graph to count cross-repo edges
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err == nil && doc != nil {
		rh.Entities = doc.Stats.Entities
		rh.Relationships = doc.Stats.Relationships
	}

	// Count cross-repo edges (edges pointing outside this repo)
	rh.CrossRepoEdges = countCrossRepoEdgesForRepo(r.Slug, stateDir)

	return rh
}

// computeQualityMetrics aggregates orphan rate, bug rate, and candidate counts for a group.
func computeQualityMetrics(health *DoctorGroupHealth) {
	// Compute orphan rate from all repos
	totalOrphans := 0
	for _, r := range health.Repos {
		stateDir := daemon.StateDirForRepo(r.Path)
		doc, err := graph.LoadGraphFromDir(stateDir)
		if err != nil || doc == nil {
			continue
		}

		// Track entities with incoming relationships
		hasIncoming := make(map[string]bool)
		for _, rel := range doc.Relationships {
			if rel.ToID != "" {
				hasIncoming[rel.ToID] = true
			}
		}

		// Count orphans
		for _, e := range doc.Entities {
			if !hasIncoming[e.ID] {
				totalOrphans++
				health.OrphanEntities++
			}
		}

		// Load candidate counts (enrichSubjects = unique entities needing enrichment).
		enrichSubjects, _, _, repairCount := loadCandidateCounts(stateDir)
		health.PendingEnrichments += enrichSubjects
		health.PendingRepairs += repairCount
	}

	// Compute rates
	if health.TotalEntities > 0 {
		health.OrphanRate = 100.0 * float64(health.OrphanEntities) / float64(health.TotalEntities)
	}

	// Bug rate is a placeholder for unresolved-edges metric
	// This would be populated from a bug-rate.json or similar in a real scenario
	health.BugRate = 0.0
}

// countCrossRepoEdgesForRepo counts relationships that point to entities in other repos.
// For now, this is a simplified count; in a full implementation it would compare
// entity IDs across repos to identify actual cross-repo edges.
func countCrossRepoEdgesForRepo(slug string, stateDir string) int {
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil || doc == nil {
		return 0
	}

	// Placeholder: count relationships pointing to external IDs.
	// A more sophisticated implementation would check if ToID belongs to a different repo.
	count := 0
	for _, rel := range doc.Relationships {
		// Simple heuristic: if ToID doesn't match any entity in this repo, it's cross-repo
		found := false
		for _, e := range doc.Entities {
			if e.ID == rel.ToID {
				found = true
				break
			}
		}
		if !found && rel.ToID != "" {
			count++
		}
	}
	return count
}

// PrintDoctorHealth writes the enriched health report to w in human-readable format.
func PrintDoctorHealth(w io.Writer, groups []*DoctorGroupHealth) {
	for _, g := range groups {
		statusMark := "✓"
		if g.Status == "DEGRADED" {
			statusMark = "✗"
		} else if g.Status == "FAILED" {
			statusMark = "✗"
		}
		fmt.Fprintf(w, "\nGroup: %s  %s %s\n", g.GroupName, g.Status, statusMark)
		fmt.Fprintf(w, "  Daemon-managed: %v\n", g.DaemonManaged)

		if g.WatcherRepoCount > 0 {
			fmt.Fprintf(w, "  Watcher: %d repos / %d dirs / %d events dropped\n",
				g.WatcherRepoCount, g.WatcherDirCount, g.WatcherEventsDropped)
			fmt.Fprintf(w, "  Last activity: %s\n", g.LastWatcherActivity)
		}

		// Per-repo stats table
		fmt.Fprintf(w, "\n  Per-repo stats:\n")
		maxSlugLen := 0
		for _, r := range g.Repos {
			if len(r.Slug) > maxSlugLen {
				maxSlugLen = len(r.Slug)
			}
		}
		if maxSlugLen < 4 {
			maxSlugLen = 4
		}

		for _, r := range g.Repos {
			statusStr := "OK"
			if r.Status == "STALE" {
				statusStr = "STALE"
			} else if r.Status == "MISSING" {
				statusStr = "MISS"
			}
			fmt.Fprintf(w, "    %-*s  %-5s  indexed %s  %6s entities  %5s rels  %d cross-repo\n",
				maxSlugLen, r.Slug, statusStr, r.LastIndexedAge,
				fmtInt(r.Entities), fmtInt(r.Relationships), r.CrossRepoEdges)
		}

		// Quality section
		fmt.Fprintf(w, "\n  Quality:\n")
		fmt.Fprintf(w, "    Bug-rate (unresolved edges): %.1f%% %s\n",
			g.BugRate, "✓")
		fmt.Fprintf(w, "    Orphan entities: %s (%.1f%%)\n",
			fmtInt(g.OrphanEntities), g.OrphanRate)
		fmt.Fprintf(w, "    Pending repairs: %s\n", fmtInt(g.PendingRepairs))
		fmt.Fprintf(w, "    Pending enrichments: %s\n", fmtInt(g.PendingEnrichments))

		// Issues section
		if len(g.IssuesFound) > 0 {
			fmt.Fprintf(w, "\n  Issues found:\n")
			for _, issue := range g.IssuesFound {
				fmt.Fprintf(w, "    - %s\n", issue)
			}
		} else {
			fmt.Fprintf(w, "\n  Issues found:\n")
			fmt.Fprintf(w, "    [none]\n")
		}
	}
}
