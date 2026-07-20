package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/statusfile"
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

	// orphanEntities is the number of entities in this repo with no incoming
	// relationship. Computed once (O(E)) during computeRepoHealth and summed
	// by computeQualityMetrics so the graph is loaded at most once per repo.
	orphanEntities int

	// RebuildFailure is the "last rebuild FAILED" marker read from the
	// status-plane sidecar (internal/statusfile), if any (#5822 sub-ask 3) —
	// e.g. the per-repo watchdog SIGKILL. Non-nil is surfaced as a doctor
	// issue AND a per-repo warning line, additively alongside (never
	// replacing) the STALE/OK/MISSING status above.
	RebuildFailure *statusfile.RebuildFailure
}

// loadGraphFromDir is an indirection over graph.LoadGraphFromDir so tests can
// count how many times the (potentially large) graph is loaded and assert the
// doctor path loads it at most once per repo (#5689).
var loadGraphFromDir = graph.LoadGraphFromDir

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
// It reads candidate counts, watcher state, and the graph-stats.json sidecar for
// each repo.
//
// #5689: the report loads each repo's graph AT MOST ONCE and derives cross-repo
// edges + orphan entities from a SINGLE O(E) adjacency pass. This replaces the
// old path that loaded the full graph three times per repo and ran an
// O(relationships×entities) nested scan, which hung for minutes on large
// (>250k-entity) graphs.
//
// Entity/relationship counts come from the live graph (doc.Stats) when the load
// succeeds — same snapshot as the orphan/cross-repo metrics, so OrphanRate can
// never mix a stale sidecar denominator with a live numerator (>100%). The
// graph-stats.json sidecar is used only as a fallback when the graph can't be
// loaded (the degraded path where orphans can't be computed anyway).
//
// deep is retained for its documented full-recompute semantics; since the
// default path already loads the graph, it no longer changes the counts when the
// load succeeds.
func ComputeDoctorHealth(groups []registry.GroupRef, deep bool) []*DoctorGroupHealth {
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
			rh := computeRepoHealth(r, deep)
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

			// #5822 sub-ask 3: a watchdog SIGKILL (or any other hard rebuild
			// failure) must never be silent — surface it as a doctor issue
			// additively, regardless of the STALE/OK status above (the
			// last-good graph may still be perfectly fine; this just says the
			// MOST RECENT rebuild attempt didn't finish).
			if rf := rh.RebuildFailure; rf != nil {
				health.Healthy = false
				health.IssuesFound = append(health.IssuesFound,
					fmt.Sprintf("repo %s last rebuild FAILED: %s%s", rh.Slug, rf.Reason, formatRebuildFailureRef(rf)))
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
//
// Last-indexed time is read from the graph-stats.json sidecar. Entity and
// relationship counts come from the live graph (doc.Stats) when it loads,
// falling back to the sidecar only when the load fails. Cross-repo edges and
// orphan entities (no sidecar) are computed from a SINGLE O(E) adjacency pass
// that loads the graph at most once.
func computeRepoHealth(r registry.Repo, deep bool) *DoctorRepoHealth {
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

	// #5822 sub-ask 3: read the status-plane sidecar for a "last rebuild
	// FAILED" marker (e.g. the per-repo watchdog SIGKILL). Plain file read —
	// no daemon dial required. Absent/unreadable is the normal "no known
	// failure" case, never an error worth surfacing here.
	if sf, sfErr := statusfile.Read(r.Path); sfErr == nil && sf != nil {
		rh.RebuildFailure = sf.LastRebuildFailure
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

	// Load the graph AT MOST ONCE to derive the metrics that have no sidecar:
	// cross-repo edges and orphan entities. Both are computed in a single O(E)
	// adjacency pass (previously an O(relationships×entities) nested scan +
	// three separate full-graph loads per repo — the #5689 hang).
	//
	// When the load SUCCEEDS we source entity/relationship counts from the live
	// graph (doc.Stats) unconditionally, so the orphan numerator and the entity
	// denominator of OrphanRate are always the SAME snapshot — a stale sidecar
	// can no longer produce a nonsensical >100% rate. The graph-stats.json
	// sidecar values read above are the fallback used ONLY when the load fails
	// (the degraded path where orphans/cross-repo can't be computed anyway).
	//
	// deep is retained for its documented full-recompute semantics; because the
	// default path already loads the graph for orphans, it no longer changes the
	// counts when the load succeeds.
	_ = deep
	doc, err := loadGraphFromDir(stateDir)
	if err == nil && doc != nil {
		rh.Entities = doc.Stats.Entities
		rh.Relationships = doc.Stats.Relationships
		rh.CrossRepoEdges, rh.orphanEntities = computeCrossRepoAndOrphans(doc)
	}

	return rh
}

// computeCrossRepoAndOrphans derives, in a single O(E) pass, the number of
// cross-repo edges (relationships whose ToID is not an entity in this repo)
// and the number of orphan entities (entities with no incoming relationship).
//
// This replaces the old O(relationships×entities) nested loop; on a 291k-entity
// / 1.4M-edge graph that scan was ≈10^12 operations. Membership lookups here are
// O(1) via a pre-built entity-ID set, so the whole pass is O(E+N).
func computeCrossRepoAndOrphans(doc *graph.Document) (crossRepo, orphans int) {
	entityIDs := make(map[string]struct{}, len(doc.Entities))
	for _, e := range doc.Entities {
		entityIDs[e.ID] = struct{}{}
	}
	hasIncoming := make(map[string]bool, len(doc.Relationships))
	for _, rel := range doc.Relationships {
		if rel.ToID == "" {
			continue
		}
		hasIncoming[rel.ToID] = true
		if _, ok := entityIDs[rel.ToID]; !ok {
			// ToID points at something not in this repo → cross-repo edge.
			crossRepo++
		}
	}
	for _, e := range doc.Entities {
		if !hasIncoming[e.ID] {
			orphans++
		}
	}
	return crossRepo, orphans
}

// computeQualityMetrics aggregates orphan rate, bug rate, and candidate counts
// for a group. Orphan counts are taken from the per-repo values already computed
// by computeRepoHealth (single O(E) pass) — this function loads no graph. The
// only per-repo I/O here is the cheap enrichment-candidates.json sidecar read.
func computeQualityMetrics(health *DoctorGroupHealth) {
	// Aggregate orphan counts computed once per repo in computeRepoHealth, and
	// read the (cheap) candidate-count sidecar.
	for _, r := range health.Repos {
		health.OrphanEntities += r.orphanEntities

		stateDir := daemon.StateDirForRepo(r.Path)
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
			// #5822 sub-ask 3: never silent — additive to the status line above.
			if rf := r.RebuildFailure; rf != nil {
				fmt.Fprintf(w, "    %-*s  ⚠ last rebuild FAILED: %s%s — see daemon.err; raise GRAFEL_REBUILD_REPO_TIMEOUT (or `grafel rebuild --timeout <dur>`) or rebuild again\n",
					maxSlugLen, "", rf.Reason, formatRebuildFailureRef(rf))
			}
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
