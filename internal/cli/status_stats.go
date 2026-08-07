package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// migrationLiveWindow is how recent a statusfile heartbeat must be for the
// status command to believe an engine is actually running the format
// migration. The engine rewrites every repo's statusfile on a 5s heartbeat
// (internal/daemon/statuswriter.go:45), so 60s is a dozen missed ticks —
// comfortably past a slow tick or a busy machine, well short of leaving a
// stopped daemon looking alive.
const migrationLiveWindow = 60 * time.Second

// statusNow is the clock ComputeStatusSummary measures heartbeat freshness
// against. A package var purely so the liveness boundary is testable at exact
// offsets — a wall-clock test can never observe "exactly migrationLiveWindow
// old" as live, because time passes between seeding the file and reading it.
var statusNow = time.Now

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

	// ReindexRequired is how many repos in this group are still waiting to be
	// reindexed after a graph-format version bump (#6167). Read straight from
	// the per-repo statusfile the engine refreshes every heartbeat — no graph
	// load, no daemon dial. Zero on the overwhelming majority of runs, and
	// PrintStatusSummary prints nothing when it is zero.
	ReindexRequired int

	// MigrationFailed is how many repos the engine has GIVEN UP migrating
	// (#6167 review finding 2). These still count in ReindexRequired — the
	// graph really is stale — but nothing is coming to fix them automatically,
	// so they are reported separately with the manual command.
	MigrationFailed int

	// MigrationLive is true when at least one repo's statusfile was
	// heartbeated recently enough to believe an engine is actually running and
	// working the migration. Review finding 3: statusfile.Read is a plain
	// unmarshal with no freshness gate, so a stopped daemon leaves
	// ReindexRequired=true on disk indefinitely — and the status line then
	// reassured the user that an automatic migration was under way when
	// nothing at all was running. Derived from statusfile.HeartbeatAt, so it
	// still needs no daemon dial.
	MigrationLive bool
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

	// RefUnknown is true when this repo's per-ref state directory could NOT be
	// resolved because git could not be run (#5822 D): a fired 2s deadline, a
	// fork EAGAIN under load, an OOM-killed child. `status` is a fresh process
	// on every invocation, so its git cache is always cold and it forks ~5 git
	// processes per run — which is exactly the population that loses this race
	// on a loaded machine.
	//
	// It is a FOURTH state, alongside the three RepoStatus already models:
	//
	//   - healthy       → counts are real;
	//   - no graph yet  → LastIndexedAge "(never)", counts genuinely zero;
	//   - GraphLoadError → a graph exists but could not be read;
	//   - RefUnknown    → we do not know WHICH graph to look at.
	//
	// When it is set, every count on this repo is MISSING, not zero, and the
	// repo contributes nothing to the group totals. That is the whole point of
	// the flag: the old code resolved the empty ref to the "_unknown" sentinel
	// directory, found no graph there (none ever exists there), and rendered
	// "0 entities · 0 rels · indexed (never)" — a healthy-looking zero that a
	// user cannot tell apart from a repo that was never indexed. On the next
	// run git succeeded and the numbers came back, which is the "counts swing
	// between runs on an unchanged repo" report.
	//
	// Never set when a ref was requested explicitly with --ref: that path needs
	// no git capture at all.
	RefUnknown bool

	// RebuildFailure is the "last rebuild FAILED" marker read from the
	// status-plane sidecar (internal/statusfile), if any (#5822 sub-ask 3).
	// Non-nil means the most recent rebuild attempt for this repo hard-failed
	// (e.g. the per-repo watchdog SIGKILL) — surfaced by PrintStatusSummary so
	// this is never silent. It does NOT mean the graph above is stale/wrong;
	// LastIndexed/Entities/etc. above may still reflect an OLDER successful
	// run, and this marker sits alongside it as an additional warning. Cleared
	// (nil again) once a subsequent rebuild of this repo succeeds.
	RebuildFailure *statusfile.RebuildFailure

	// ReindexRequired is true when this repo's on-disk graph.fb was written by
	// an older grafel than this binary accepts, so it is queued for (or
	// undergoing) an automatic format migration (#6167). Sourced from the
	// statusfile, which the engine recomputes from the graph.fb header on every
	// heartbeat (internal/daemon/statuswriter.go:135).
	ReindexRequired bool

	// MigrationFailed mirrors statusfile.ReindexMigrationFailed: the engine
	// tried staleReindexMaxAttempts times and gave up on this repo.
	MigrationFailed bool

	// GraphLoadError is non-empty when this repo HAS a graph artefact on disk
	// but it could not be read: a truncated/garbage graph.fb (the flatbuffers
	// accessors PANIC on those), an unreadable segment-set manifest, an
	// incompatible format version, or a nil Document from a load that reported
	// no error. It is the third state, distinct from BOTH:
	//
	//   - healthy      → GraphLoadError == "" and the counts below are real;
	//   - no graph yet → GraphLoadError == "" with LastIndexedAge "(never)".
	//
	// The string-valued "the last load failed, here is why" shape mirrors the
	// representation the rest of the codebase already uses for exactly this
	// condition: internal/mcp's LoadedRepo.loadErr (state.go), surfaced as the
	// dashboard's LoadError and as tools.go's "unavailable" list. See #6013
	// (this defect), #6010 (a nil Doc from the same failed-load state reaching
	// BuildLabelIndex) and #5969 (the malformed-graph class).
	//
	// When set, the Files / IndexedRef / IndexedSHA / IsWorktree fields and the
	// group's HTTPEndpoints / ProcessFlows / orphan-rate contributions from
	// this repo are MISSING, not zero — which is precisely why reporting the
	// failure matters: silently under-reported numbers read as a healthy,
	// smaller repo.
	GraphLoadError string
}

// ComputeStatusSummary loads the per-repo graph-stats.json files and enrichment
// candidate counts for a group, aggregating them into a StatusSummary.
// Errors reading individual files are silently skipped so a partial result
// does not prevent summary generation.
func ComputeStatusSummary(group string, repos []registry.Repo) *StatusSummary {
	return ComputeStatusSummaryForRef(group, repos, "")
}

// ComputeStatusSummaryForRef is ComputeStatusSummary for a CALLER-CHOSEN ref
// (#5822 C).
//
// ref == "" keeps the historical behaviour: each repo's state directory is
// derived from its current HEAD. A non-empty ref selects that ref's stored
// graph directly, for every repo in the group.
//
// Threading the ref this far down is the whole of defect C. `grafel status
// --ref main` used to resolve the flag, validate it, print "Note: showing state
// for ref …" — and then hand ComputeStatusSummary no ref at all, so every repo
// went through daemon.StateDirForRepo → gitmeta capture → whatever HEAD pointed
// at right then. The note described one thing and the numbers underneath it
// described another.
//
// A requested ref also needs NO git subprocess, which is why it is immune to
// defect D below.
func ComputeStatusSummaryForRef(group string, repos []registry.Repo, ref string) *StatusSummary {
	s := &StatusSummary{
		GroupName: group,
		RepoStats: make(map[string]*RepoStatus),
	}

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
			// #6167: same free read — the engine recomputes this from the
			// graph.fb header every heartbeat, so it is the freshest answer
			// available without loading a graph.
			rs.ReindexRequired = sf.ReindexRequired
			rs.MigrationFailed = sf.ReindexMigrationFailed
			if sf.ReindexRequired {
				s.ReindexRequired++
			}
			if sf.ReindexMigrationFailed {
				s.MigrationFailed++
			}
			// Liveness: a heartbeat within migrationLiveWindow means an engine
			// is writing these files right now. Any one repo suffices — the
			// writer walks them all on a single loop.
			if !sf.HeartbeatAt.IsZero() && statusNow().Sub(sf.HeartbeatAt) <= migrationLiveWindow {
				s.MigrationLive = true
			}
		}

		// Resolve which per-ref slot to read.
		//
		// An explicitly requested ref is used verbatim — no git, so nothing can
		// fail here. Otherwise the repo's CURRENT ref has to be discovered, and
		// that can fail transiently (#5822 D): when it does, the resolver says
		// so instead of quietly handing back the "_unknown" sentinel, and this
		// repo is recorded as unknown rather than as an indexed-never zero.
		// Everything below would otherwise read a directory that never holds a
		// graph and report confident zeros for a repo that may be fully indexed.
		var stateDir string
		if ref != "" {
			stateDir = daemon.StateDirForRepoRef(r.Path, ref)
		} else {
			resolved, refKnown := daemon.StateDirForRepoResolved(r.Path)
			if !refKnown {
				rs.RefUnknown = true
				rs.LastIndexedAge = "(unknown)"
				s.RepoStats[r.Slug] = rs
				continue
			}
			stateDir = resolved
		}

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

		// Read the graph's header + entity KINDS to fill in the derived counts
		// (HTTPEndpoints, ProcessFlows) and the git metadata; the main
		// entity/relationship counts come from graph-stats.json above.
		//
		// #5995: this used to call graph.LoadGraphFromDir, which materialises
		// EVERY entity and EVERY relationship of the repo's graph onto the Go
		// heap — hundreds of megabytes on a corpus-sized repo — to read four
		// header fields and tally two entity kinds. `status` is the CLI's
		// most-polled command (shell prompts, statusline, editor integrations,
		// watch scripts), so that cost recurs on a timer on a memory-
		// constrained machine (epic #5954).
		//
		// It now streams: graph.OpenGraphStream holds the mmap open and hands
		// rows over one at a time without retaining them, Header() reads the
		// Graph root table directly, and EachEntityKind decodes ONLY the kind
		// string per row. Nothing here holds a row past the visit call.
		//
		// The relationship walk is GONE, not streamed. It populated a
		// `hasIncoming` map ("to compute orphan rate later") that no code ever
		// read — the orphan rate is computed in doctor_summary.go from its own
		// load. Relationships outnumber entities ~8:1 on the real corpus, so
		// that dead walk was the single largest item here.
		//
		// #6013: this used to swallow BOTH failure modes — a `recover()` with
		// an empty body ("Silently ignore panics during graph loading"), and
		// an `if err == nil && doc != nil` that dropped the returned error on
		// the floor. A corrupt/truncated graph.fb (the flatbuffers accessors
		// panic with "slice bounds out of range") therefore produced a
		// plausible, SMALLER-than-reality repo line with no warning at all,
		// indistinguishable from a healthy repo.
		//
		// The recover STAYS — `status` must remain non-fatal and keep
		// reporting every other repo and group — but the failure is now
		// RECORDED on rs.GraphLoadError and rendered by PrintStatusSummary.
		// A repo that simply has no graph yet is NOT a failure: that keeps the
		// existing "0 entities … indexed (never)" shape, so the three states
		// (healthy / not indexed yet / failed) stay distinguishable.
		func() {
			defer func() {
				// Catch panics from malformed graph files.
				if rec := recover(); rec != nil {
					rs.GraphLoadError = scrubPaths(fmt.Sprintf("panic while reading the graph: %v", rec))
				}
			}()
			stream, err := graph.OpenGraphStream(stateDir)
			switch {
			case err != nil:
				// Only a graph that EXISTS and cannot be read is a failure.
				// "neither graph.fb nor graph.json found" is the never-indexed
				// state and stays silent.
				if graphArtifactPresent(stateDir) {
					rs.GraphLoadError = scrubPaths(loadErrorText(stateDir, err))
				}
			case stream == nil:
				// A nil stream with a nil error is the analogue of the nil
				// Document that reaches BuildLabelIndex in #6010 — report it,
				// never treat it as an empty-but-healthy repo.
				rs.GraphLoadError = "graph loaded as a nil document"
			default:
				defer stream.Close()
				// Staged in locals and committed only once the walk has
				// completed. LoadGraphFromDir decoded every row BEFORE its
				// caller saw a single field, so a graph that PANICKED part-way
				// through contributed nothing at all: no git metadata, no
				// partial kind tallies. A streamed read reaches the panic AFTER
				// publishing whatever it has already seen. Without this
				// staging, a truncated graph.fb starts reporting a ref/sha next
				// to its "FAILED to load" warning and folds a partial endpoint
				// count into the group TOTAL — a reported-value change, which
				// #5995 must not make.
				//
				// The invariant this buys is exactly "a PANIC publishes
				// nothing", and no more. A walk that completes but silently
				// visits fewer rows than the graph holds — EachEntityKind skips
				// a nil vector slot with `continue`, and has no way to report
				// that it did — still commits its undercount. That is not a
				// regression: materializeGraphView skips nil slots the same way
				// and the old code committed the same undercount. It is a
				// pre-existing gap in both, noted so this staging is not read
				// as protecting against more than it does.
				//
				// Stats.Files is zero for every .fb-backed graph (the format
				// encodes no file count) and real only for the graph.json
				// fallback — see GraphStream.DocStats.
				//
				// #6115: that zero used to be assigned over rs.Files
				// unconditionally, a few lines below, destroying the real
				// TotalFiles this function had already read out of
				// graph-stats.json — so `status` printed "0 files" for every
				// repo in the shipping format. The graph's count is now taken
				// only when it HAS one; see the commit at rs.Files below.
				files := stream.DocStats().Files
				// Phase 0 git metadata (#2088), straight off the header.
				meta := stream.Header()
				endpoints, flows := 0, 0
				stream.EachEntityKind(func(kind string) bool {
					// #1217: count all three http endpoint kind strings.
					if kind == "http_endpoint" || kind == "http_endpoint_definition" || kind == "http_endpoint_call" {
						endpoints++
					}
					// Check for process flows: SCOPE.Process or process kind.
					if kind == "process" || strings.HasPrefix(kind, "SCOPE.Process") {
						flows++
					}
					return true
				})
				// #6115: a graph-derived file count is authoritative only when
				// the graph actually carries one. No .fb layout does — the
				// Graph root table in internal/graph/schema/graph.fbs has no
				// file-count field, so `files` is STRUCTURALLY zero there, not
				// measured-as-zero — while graph.json does. graph-stats.json is
				// the carrier of the real number and was already read into
				// rs.Files above; overwriting it with a structural zero threw
				// the only true answer away.
				//
				// Guarding on `> 0` rather than on layout keeps both directions
				// honest: a graph.json repo still reports its document count
				// (unchanged), a .fb repo keeps its sidecar count, and a repo
				// with neither still reports 0 — which for a .fb repo without a
				// sidecar is the truthful "not known", the same 0 it printed
				// before. Nothing here can invent a number that was not
				// measured somewhere.
				if files > 0 {
					rs.Files = files
				}
				rs.IndexedRef = meta.IndexedRef
				rs.IndexedSHA = meta.IndexedSHA
				rs.IsWorktree = meta.IsWorktree
				s.HTTPEndpoints += endpoints
				s.ProcessFlows += flows
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

// loadErrorText returns the message to report for a graph that exists but
// could not be opened.
//
// #5995 replaced graph.LoadGraphFromDir with graph.OpenGraphStream here. The
// two resolve a dir identically but WORD their failures differently (e.g.
// "graph.loadFBDocument: …" vs "graph.OpenGraphStream: open …"), and this text
// is printed to the terminal — changing it would change what `status` reports.
// So when the stream fails to open, ask the loader for its wording.
//
// It asks the HEADER-ONLY loader, deliberately. LoadGraphFromDir gives the same
// wording — loadFBDocument and loadFBDocumentHeaderOnly are one function
// (loadFBDoc) and share both the open error and materializeGraphView's
// format-version error, and the segment-set path is literally the same
// loadSegmentSetDocument call — but it is UNBOUNDED on the one input this
// reasoning does not cover: a load that SUCCEEDS where the stream failed.
// `status` is polled constantly, so a generation flip landing between the
// stream's descriptor resolve and its open is the plausible way to get there,
// and the full loader would then materialise the entire graph inside the
// command whose whole purpose is not to. LoadGraphHeaderOnlyFromDir cannot:
// it skips both materialize loops. Its only delegation to the full loader is
// the no-.fb case (graph.json or absent), where OpenGraphStream materialises
// the JSON document too — so that branch costs exactly what it did before.
//
// If the loader somehow does NOT fail, the stream's own error stands rather
// than inventing a success.
func loadErrorText(stateDir string, streamErr error) string {
	if _, lerr := graph.LoadGraphHeaderOnlyFromDir(stateDir); lerr != nil {
		return lerr.Error()
	}
	return streamErr.Error()
}

// fsPathRe matches an absolute (or otherwise multi-segment) filesystem path,
// Unix or Windows. It requires at least one INTERIOR separator so ordinary
// prose like "and/or" is never mistaken for a path.
var fsPathRe = regexp.MustCompile(`(?:[A-Za-z]:)?[/\\](?:[^\s:/\\]+[/\\])+[^\s:/\\]*`)

// scrubPaths reduces every filesystem path in a message to its last segment.
//
// GraphLoadError is printed to the terminal and is exactly the kind of string a
// user pastes into a bug report, and graph-load errors readily embed absolute
// paths — a corrupt segment-set manifest surfaces as
// "graph: resolve segment-set <abs>: graph: parse manifest in <abs>: …",
// which discloses the repository layout. Scrubbing here rather than relying on
// which underlying error happens to fire keeps that true for load failures we
// have not seen yet. The basename is kept so the diagnosis (which generation,
// which file, what went wrong) survives.
func scrubPaths(msg string) string {
	return fsPathRe.ReplaceAllStringFunc(msg, func(p string) string {
		trimmed := strings.TrimRight(p, `/\`)
		if i := strings.LastIndexAny(trimmed, `/\`); i >= 0 {
			trimmed = trimmed[i+1:]
		}
		if trimmed == "" {
			return "<path>"
		}
		return trimmed
	})
}

// graphArtifactPresent reports whether stateDir contains something that is
// MEANT to be a graph. It is what separates the two error outcomes of
// graph.LoadGraphFromDir (#6013):
//
//   - present + load error  → a real failure, surfaced as GraphLoadError;
//   - absent                → the never-indexed state ("neither graph.fb nor
//     graph.json found in …"), which is normal and must stay silent.
//
// A CurrentGraphDescriptor that itself errors counts as PRESENT: the only way
// it errors is a segment-set pointer whose manifest is corrupt, which is a
// broken artefact, not a missing one.
func graphArtifactPresent(stateDir string) bool {
	desc, err := graph.CurrentGraphDescriptor(stateDir)
	if err != nil {
		return true
	}
	if desc.Kind != graph.GraphAbsent {
		return true
	}
	// GraphAbsent only speaks for the .fb layouts; graph.json is the other
	// artefact LoadGraphFromDir accepts.
	if _, statErr := os.Stat(filepath.Join(stateDir, "graph.json")); statErr == nil {
		return true
	}
	return false
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
		// #5822 D: a repo whose ref could not be determined has NO numbers to
		// print. Printing the zero-valued line would render it as a healthy,
		// never-indexed repo — the exact confusion this replaces. The rebuild
		// -failure marker below is ref-independent and still shown.
		if rs.RefUnknown {
			fmt.Fprintf(w, "  %-*s  state UNKNOWN — git could not be run to resolve this repo's current ref; counts are UNAVAILABLE (not zero). Retry, or name the ref with `grafel status --ref <branch>`.\n",
				maxSlugLen, slug)
			if rf := rs.RebuildFailure; rf != nil {
				fmt.Fprintf(w, "  %-*s  ⚠ last rebuild FAILED: %s%s — see daemon.err; raise GRAFEL_REBUILD_REPO_TIMEOUT (or `grafel rebuild --timeout <dur>`) or rebuild again\n",
					maxSlugLen, "", rf.Reason, formatRebuildFailureRef(rf))
			}
			continue
		}
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
		// #6013: a graph that exists but cannot be READ used to be swallowed
		// entirely, so the line above read as a healthy — merely smaller —
		// repo. Say so explicitly, and say which numbers on that line are
		// therefore missing rather than genuinely zero.
		if rs.GraphLoadError != "" {
			fmt.Fprintf(w, "  %-*s  ⚠ graph FAILED to load: %s — file/endpoint/flow counts above are INCOMPLETE; run `grafel reset %s` to rebuild from scratch\n",
				maxSlugLen, "", rs.GraphLoadError, s.GroupName)
		}
	}

	// Print aggregated totals.
	//
	// #6013: every repo whose graph failed to load contributed NOTHING to the
	// flow/endpoint/orphan aggregates, so this headline under-reports. Marking
	// only the per-repo lines would leave the number most people actually read
	// silently wrong.
	//
	// #5822 D adds the second way a repo can contribute nothing: its ref could
	// not be resolved, so we never even learned which graph to read. Same
	// consequence for this headline — an under-report that reads as a fact —
	// so it is disclosed the same way.
	incomplete := 0
	for _, rs := range s.RepoStats {
		if rs.GraphLoadError != "" || rs.RefUnknown {
			incomplete++
		}
	}
	totalSuffix := ""
	if incomplete > 0 {
		noun := "repos"
		if incomplete == 1 {
			noun = "repo"
		}
		totalSuffix = fmt.Sprintf("  ⚠ INCOMPLETE — %d %s could not be read (see above)", incomplete, noun)
	}
	fmt.Fprintf(w, "\n  TOTAL: %s entities · %s relationships · %s cross-repo edges · %s flows · %s endpoints%s\n",
		fmtInt(s.TotalEntities),
		fmtInt(s.TotalRelationships),
		fmtInt(s.CrossRepoEdges),
		fmtInt(s.ProcessFlows),
		fmtInt(s.HTTPEndpoints),
		totalSuffix)

	// Print available optional candidates.
	//
	// #5693: these are OPTIONAL LLM-enrichment/repair opportunities recomputed
	// from the current graph — not a live daemon queue. Nothing auto-drains
	// them (they cost tokens; the user runs enrichment to apply). The wording
	// deliberately avoids "Pending", which reads as a stuck/blocked queue.
	// #6167: a graph-format migration is the one background activity that can
	// legitimately keep the daemon busy for hours, and until now it was
	// completely silent — the user in the originating report read "silent and
	// busy" as "hung", restarted, and re-entered the queue. Naming the cause and
	// showing the fraction converts that into visible progress. Printed only
	// while a migration is actually outstanding, so it adds no steady-state
	// noise (and leaves the #5995 byte-for-byte golden untouched).
	active := s.ReindexRequired - s.MigrationFailed
	if active < 0 {
		active = 0
	}
	// Round-3 LOW 5: gate on `active`, not on ReindexRequired. When every
	// remaining stale repo has been given up on, the old gate still fired and
	// printed "0 of N ... migrating them automatically ... No action needed"
	// directly above the warning that N repos could NOT be migrated — two
	// contradictory lines, with the reassuring one first.
	if active > 0 {
		total := len(s.RepoStats)
		// The denominator is this GROUP's repo count, and the wording says so
		// — a multi-group store has more repos than this, and claiming
		// otherwise would understate the migration (review finding 3).
		switch {
		case !s.MigrationLive:
			// No recent heartbeat: nothing is migrating anything. Telling the
			// user "no action needed" here is precisely backwards.
			fmt.Fprintf(w, "  Format upgrade STALLED: %d of %d repo(s) in this group need a reindex, but the daemon is not running — start it with `grafel start` (or reindex manually with `grafel index <repo>`).\n",
				s.ReindexRequired, total)
		default:
			fmt.Fprintf(w, "  Format upgrade in progress: %d of %d repo(s) in this group awaiting reindex — grafel is migrating them automatically, a few at a time. No action needed.\n",
				active, total)
		}
	}
	if s.MigrationFailed > 0 {
		fmt.Fprintf(w, "  ⚠ %d repo(s) could not be migrated automatically after repeated attempts — reindex them manually with `grafel index <repo>`.\n",
			s.MigrationFailed)
	}

	if s.EnrichmentCandidates > 0 || s.RepairCandidates > 0 {
		fmt.Fprintf(w, "  Available (optional; run enrichment to apply — nothing auto-drains): %s enrichment opportunities · %s repair candidates\n",
			fmtInt(s.EnrichmentCandidates),
			fmtInt(s.RepairCandidates))
	}
}
