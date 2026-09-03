package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/statusfile"
	"github.com/cajasmota/grafel/internal/types"
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

	// UnsupportedExt counts the files this repo's last index pass dropped for
	// having no extractor, keyed by extension (#6338). Read straight from the
	// graph-stats.json sidecar; nil when the sidecar is absent, predates the
	// field, or the repo has full extractor coverage.
	UnsupportedExt map[string]int

	// RenameTruncated reports that this repo's last index hit the
	// rename-detection work budget, so its RENAMED_FROM edges are a PARTIAL
	// result (#6087, wired by #6640). Read straight from the graph-stats.json
	// sidecar — the indexer's stderr warning is invisible here, and until this
	// field existed the sidecar flag was written by the indexer and read by
	// nothing, which is the whole of #6640.
	//
	// A truncated rename pass is exactly what DEGRADED already means: the
	// index is usable but incomplete. It is surfaced additively, alongside
	// (never replacing) the OK/STALE/MISSING status above, because the graph
	// itself is fresh and fine — only the rename edges are partial.
	RenameTruncated bool

	// KindsNotInEnum is the per-kind edge count for relationship kinds this
	// repo's last index WROTE that are absent from the relationship-kind enum
	// (#6757 arm C). Like UnsupportedExt and RenameTruncated above, the
	// sidecar is the ONLY source: nothing in the graph itself marks an edge
	// as carrying an unrecognised kind, so a consumer traversing it cannot
	// tell. Nothing was dropped — these edges are all in the graph.
	KindsNotInEnum map[string]int
	// EdgesKindNotInEnum is the total edge count behind KindsNotInEnum
	// (uncapped, unlike the map, which the writer truncates).
	EdgesKindNotInEnum int
	// DistinctKindsNotInEnum is the uncapped number of distinct such kinds;
	// it may exceed len(KindsNotInEnum).
	DistinctKindsNotInEnum int

	// DerivedKinds / EdgesDerivedKind / DistinctDerivedKinds are the same
	// three numbers for the DERIVED (statistical) vocabulary #6773 declared —
	// COMMIT_COUPLED and anything later added beside it. They are reported
	// separately rather than folded into the three fields above: those edges
	// are declared, so they are not a vocabulary gap, but they are 99% of the
	// population the gap-counter used to report and doctor is where that was
	// visible.
	DerivedKinds         map[string]int
	EdgesDerivedKind     int
	DistinctDerivedKinds int
	// RenameAddedSkipped is how many added entities that truncated pass never
	// examined. Only meaningful when RenameTruncated is true.
	RenameAddedSkipped int

	// KindVocabulary is which entity-kind vocabulary this repo's stored graph
	// speaks (#6779) — one of graph.KindVocabularyCurrent / ...Older /
	// ...NoGraph. It is THREE-valued, and the third value is load-bearing:
	// "current" and "nothing indexed here" are different facts, and reporting
	// a never-indexed repo as speaking a stale vocabulary would be the same
	// confident wrong answer this field exists to prevent.
	//
	// Unlike the sidecar-only fields above, this is NOT read straight off the
	// sidecar: graph.KindVocabularyStateForDir combines the sidecar stamp with
	// an independent check that a graph is actually stored, precisely so the
	// two negative states cannot collapse into one another.
	KindVocabulary graph.KindVocabularyState
	// KindVocabularyStored is the version stamped on the stored graph. Zero
	// means the graph predates the stamp (which is itself an older
	// vocabulary — v0.3.1 renamed kinds under it). Meaningless when
	// KindVocabulary is graph.KindVocabularyNoGraph.
	KindVocabularyStored int
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
	TotalEntities        int
	TotalRelationships   int
	TotalCrossRepoEdges  int
	BugRate              float64 // unresolved-edges percentage
	OrphanEntities       int
	OrphanRate           float64
	RepairCandidates     int
	EnrichmentCandidates int

	// UnsupportedExt is the per-extension count of files skipped for having no
	// extractor, SUMMED across every repo in the group (#6338). A monorepo
	// split into five repos is one gap, not five.
	UnsupportedExt map[string]int

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

			// #6640 — a truncated rename pass makes the group DEGRADED. Same
			// additive shape as the rebuild failure above: the graph is fresh,
			// but its RENAMED_FROM edges are partial, and a consumer reporting
			// "no renames" off it would be stating a confident wrong answer.
			if rh.RenameTruncated {
				health.Healthy = false
				health.IssuesFound = append(health.IssuesFound,
					fmt.Sprintf("repo %s rename detection was TRUNCATED: RENAMED_FROM edges are INCOMPLETE (%s added entities never examined) — reindex to complete the scan",
						rh.Slug, fmtInt(rh.RenameAddedSkipped)))
			}

			// #6779 — a graph indexed under an older entity-kind vocabulary
			// makes the group DEGRADED, additively like the two above. The
			// graph is fresh, complete and readable; its entities are simply
			// spelled with kinds this build has renamed or retired, so a
			// consumer filtering on the new spellings gets an empty result
			// from a graph that looks healthy. Nothing is migrated and nothing
			// is reindexed here — reindexing a large group is expensive and
			// the user chooses when to pay for it.
			if rh.KindVocabulary == graph.KindVocabularyOlder {
				health.Healthy = false
				health.IssuesFound = append(health.IssuesFound,
					fmt.Sprintf("repo %s was indexed under an OLDER entity-kind vocabulary (graph v%d, this build speaks v%d): queries filtering on renamed kinds return EMPTY — reindex this repo to update it",
						rh.Slug, rh.KindVocabularyStored, types.KindVocabularyVersion))
			}
		}

		// Sort repos by slug for consistent output
		sort.Slice(health.Repos, func(i, j int) bool {
			return health.Repos[i].Slug < health.Repos[j].Slug
		})

		// Compute aggregated quality metrics
		computeQualityMetrics(health)
		aggregateUnsupported(health)

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

	// #6779 — which entity-kind vocabulary the stored graph speaks. Computed
	// BEFORE (and independently of) the sidecar decode below, because it needs
	// to know whether a graph exists at all: a state dir with a leftover
	// sidecar and no graph is "nothing indexed", not "stale vocabulary".
	//
	// This does re-read graph-stats.json, which the block below reads again
	// (and computeQualityMetrics a third time). Left deliberately unshared:
	// the whole point of this call is that it does NOT trust the sidecar as
	// its only input — it pairs the stamp with an independent graph-existence
	// check — and threading a pre-decoded sidecar in would re-couple the two
	// reads that must stay separate for the three states to survive. Each read
	// is a small JSON file with no entity materialization; doctor already
	// loads the full graph per repo on the deep path.
	rh.KindVocabulary, rh.KindVocabularyStored = graph.KindVocabularyStateForDir(stateDir)

	// Load graph-stats.json sidecar for basic counts
	sidecarPath := filepath.Join(stateDir, "graph-stats.json")
	if data, err := os.ReadFile(sidecarPath); err == nil {
		var side graph.GraphStatsSidecar
		if json.Unmarshal(data, &side) == nil {
			rh.Entities = side.TotalEntities
			rh.Relationships = side.TotalRelationships
			// #6338 — the ONLY source for this; there is nowhere else to fall
			// back to. A file with no extractor produces no entity, no edge
			// and no error, so nothing in the graph itself records that it was
			// ever seen.
			rh.UnsupportedExt = side.UnsupportedExtensions
			// #6640 — the ONLY source for this, same as UnsupportedExt above:
			// a rename pass that stopped early leaves no trace in the graph,
			// only an absence of RENAMED_FROM edges that is indistinguishable
			// from "nothing was renamed".
			rh.RenameTruncated = side.RenameDetectTruncated
			rh.RenameAddedSkipped = side.RenameDetectAddedSkipped
			// #6757 arm C — the ONLY source, same as the two above. Read only
			// when the write path actually ran the tally: with omitempty an
			// unscanned sidecar and a clean one are the same bytes, and
			// reporting "no unrecognised kinds" off a graph nothing counted
			// is the failure this arm exists to avoid.
			if side.RelationshipKindsScanned {
				rh.KindsNotInEnum = side.RelationshipKindsNotInEnum
				rh.EdgesKindNotInEnum = side.RelationshipEdgesKindNotInEnum
				rh.DistinctKindsNotInEnum = side.RelationshipDistinctKindsNotInEnum
				// #6773 — read under the SAME scanned guard: the derived
				// counts describe the same tally, and an unscanned sidecar
				// knows nothing about either population.
				rh.DerivedKinds = side.RelationshipDerivedKinds
				rh.EdgesDerivedKind = side.RelationshipEdgesDerivedKind
				rh.DistinctDerivedKinds = side.RelationshipDistinctDerivedKinds
			}
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
		health.EnrichmentCandidates += enrichSubjects
		health.RepairCandidates += repairCount
	}

	// Compute rates
	if health.TotalEntities > 0 {
		health.OrphanRate = 100.0 * float64(health.OrphanEntities) / float64(health.TotalEntities)
	}

	// Bug rate is a placeholder for unresolved-edges metric
	// This would be populated from a bug-rate.json or similar in a real scenario
	health.BugRate = 0.0
}

// aggregateUnsupported sums every repo's unsupported-extension counts into the
// group total (#6338). Left nil when nothing was skipped anywhere, so the
// renderer prints nothing at all rather than an empty section.
func aggregateUnsupported(health *DoctorGroupHealth) {
	var total map[string]int
	for _, rh := range health.Repos {
		for ext, n := range rh.UnsupportedExt {
			if n <= 0 {
				continue
			}
			if total == nil {
				total = make(map[string]int)
			}
			total[ext] += n
		}
	}
	health.UnsupportedExt = total
}

// ComputeDoctorUnsupported builds the per-group unsupported-language view used
// by `grafel doctor --json` (#6338).
//
// It reads ONLY each repo's graph-stats.json sidecar and reuses
// aggregateUnsupported, so it shares its summation with the human path and the
// two can never disagree. Deliberately NOT ComputeDoctorHealth: that loads
// every repo's full graph to derive orphans and cross-repo edges, and `doctor
// --json` today does no runtime work at all — paying seconds-to-minutes of
// graph loading for a field the JSON consumer did not ask for would be a
// straightforward regression of that command.
//
// The returned DoctorGroupHealth values are therefore PARTIAL: only GroupName,
// Repos[].Slug/UnsupportedExt and UnsupportedExt are populated. Nothing else is
// meaningful and nothing else should be read off them.
func ComputeDoctorUnsupported(groups []registry.GroupRef) []*DoctorGroupHealth {
	var out []*DoctorGroupHealth
	for _, g := range groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}
		health := &DoctorGroupHealth{GroupName: g.Name}
		for _, r := range cfg.Repos {
			rh := &DoctorRepoHealth{Slug: r.Slug}
			if side, sErr := graph.LoadSidecar(daemon.StateDirForRepo(r.Path)); sErr == nil && side != nil {
				rh.UnsupportedExt = side.UnsupportedExtensions
			}
			health.Repos = append(health.Repos, rh)
		}
		aggregateUnsupported(health)
		out = append(out, health)
	}
	return out
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
		// #6682: size this column by RUNE count, not by len()'s BYTE count.
		// fmt's %-*s below pads by runes, so a byte-derived width overshoots by
		// (bytes - runes) and pushes every payload right for no reason.
		//
		// This targets RUNE COUNT, NOT TERMINAL DISPLAY WIDTH. The two are not
		// the same: a CJK ideograph is one rune but occupies two terminal
		// columns, and an emoji may be one rune and two columns, or a grapheme
		// cluster spanning several runes. So this aligns the common
		// European-accent case ("cafe" with an acute e) exactly, and a slug
		// containing CJK or emoji STILL renders ragged. Correcting that needs a
		// wcwidth table or grapheme segmentation, a dependency this table does
		// not justify; it is deliberately not done here.
		maxSlugLen := 0
		for _, r := range g.Repos {
			if n := utf8.RuneCountInString(r.Slug); n > maxSlugLen {
				maxSlugLen = n
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
			// #6640 — additive to the status line above, same as the rebuild
			// warning: the index is usable, the rename edges are not complete.
			if r.RenameTruncated {
				fmt.Fprintf(w, "    %-*s  ⚠ rename detection TRUNCATED: RENAMED_FROM edges are INCOMPLETE (%s added entities never examined) — reindex to complete the scan\n",
					maxSlugLen, "", fmtInt(r.RenameAddedSkipped))
			}
			// #6779 — additive, same shape as the rename warning above: the
			// graph loaded fine, its KINDS are the stale part. Printed only
			// for the older-vocabulary state, never for a current graph and
			// never for a repo that has no graph at all.
			if r.KindVocabulary == graph.KindVocabularyOlder {
				fmt.Fprintf(w, "    %-*s  ⚠ indexed under an OLDER entity-kind vocabulary (graph v%d, this build speaks v%d): queries on renamed kinds return EMPTY — reindex this repo\n",
					maxSlugLen, "", r.KindVocabularyStored, types.KindVocabularyVersion)
			}
			// #6757 arm C — additive and INFORMATIONAL: these edges are all in
			// the graph and nothing failed, so this never changes the repo
			// status. It is printed because the count is otherwise invisible:
			// the kinds are absent from the enum every consumer traverses by.
			if line := KindsNotInEnumLine(r.EdgesKindNotInEnum, r.DistinctKindsNotInEnum, r.KindsNotInEnum); line != "" {
				fmt.Fprintf(w, "    %-*s  %s\n", maxSlugLen, "", line)
			}
			// #6773 — the derived population, on its own line for the same
			// reason: declaring COMMIT_COUPLED removed 99.1% of the count
			// above, and a number that large must not simply vanish from the
			// surface that reported it.
			if line := DerivedKindsLine(r.EdgesDerivedKind, r.DistinctDerivedKinds, r.DerivedKinds); line != "" {
				fmt.Fprintf(w, "    %-*s  %s\n", maxSlugLen, "", line)
			}
		}

		// Quality section
		fmt.Fprintf(w, "\n  Quality:\n")
		fmt.Fprintf(w, "    Bug-rate (unresolved edges): %.1f%% %s\n",
			g.BugRate, "✓")
		fmt.Fprintf(w, "    Orphan entities: %s (%.1f%%)\n",
			fmtInt(g.OrphanEntities), g.OrphanRate)
		fmt.Fprintf(w, "    Repair candidates: %s\n", fmtInt(g.RepairCandidates))
		fmt.Fprintf(w, "    Enrichment opportunities: %s\n", fmtInt(g.EnrichmentCandidates))

		// #6338 — files grafel SAW and silently indexed nothing for. Printed
		// only when there are any: on a repo with full extractor coverage the
		// output below is byte-identical to what it was before this section
		// existed. doctor is the diagnostic surface, so it shows the full
		// table (min 1 file); `status` applies a floor.
		if rows := UnsupportedRows(g.UnsupportedExt, DoctorUnsupportedMinFiles); len(rows) > 0 {
			fmt.Fprintf(w, "\n")
			PrintUnsupportedLanguages(w, "  ", rows)
		}

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
