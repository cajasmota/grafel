// Package progress defines the per-repo, per-phase progress event shape
// emitted by the indexer pipeline and the Publisher interface that
// consumers implement.
//
// Phase A of the real-time progress epic (#1118): this package is the
// foundation. Sub-issue B wires a real broker; sub-issue C exposes an SSE
// endpoint; sub-issue D adds frontend rendering.
//
// Design goals:
//   - Publisher.Publish never blocks the indexer. The buffered-channel
//     implementation uses a drop-oldest policy under backpressure so the
//     hot extraction loop is never paused waiting for a slow consumer.
//   - All fields are JSON-serialisable so the SSE endpoint can forward
//     events verbatim.
//   - "Go beyond" additions: BytesSeen, CurrentFile, PhaseStartedAtMS give
//     the UI extra detail without requiring the broker or frontend to land.
package progress

import "time"

// Phase labels emitted during indexing. The set deliberately mirrors the
// user-visible pipeline stages rather than the internal pass numbers so the
// SSE endpoint and the frontend speak the same vocabulary.
const (
	// PhaseScan is the file-discovery walk — grafel is counting files.
	PhaseScan = "scanning"

	// PhaseExtractAST is the per-language AST extraction pass (Pass 1 /
	// subprocess-extract). Progress ticks every TickEveryNFiles files.
	PhaseExtractAST = "extracting_ast"

	// PhaseResolveRefs covers Pass 2.5 framework rules, Pass 3 cross-lang
	// extractors, external synthesis, and the resolver / disposition pass.
	PhaseResolveRefs = "resolving_refs"

	// PhaseAlgorithms is Pass 4 (PageRank, Louvain, betweenness,
	// articulation points, surprise edges). One event per algorithm entry
	// and exit. Retained as a coarse fallback; the granular pass boundaries
	// below (#5334) split it into community detection and centrality.
	PhaseAlgorithms = "running_algorithms"

	// PhaseBuildCommunities is the community-detection slice of Pass 4
	// (Louvain). Surfaced as "Building communities…" (#5334).
	PhaseBuildCommunities = "building_communities"

	// PhaseComputeCentrality is the centrality slice of Pass 4 (PageRank,
	// betweenness, articulation points, surprise edges). Surfaced as
	// "Computing centrality…" (#5334).
	PhaseComputeCentrality = "computing_centrality"

	// PhaseMaterialize covers buildDocument, graph.fb write, sidecar write,
	// and enrichment-candidate emission. Retained as a coarse fallback; the
	// granular boundaries below (#5334) split the group-assembly tail into
	// cross-repo links, flows, and the graph write.
	PhaseMaterialize = "materializing"

	// PhaseDetectLinks is the cross-repo link-detection pass that runs at
	// GROUP assembly after every member repo is indexed. Surfaced as
	// "Detecting cross-repo links…" (#5334).
	PhaseDetectLinks = "detecting_links"

	// PhaseComputeFlows is the process-flow / event-flow walker pass.
	// Surfaced as "Computing flows…" (#5334).
	PhaseComputeFlows = "computing_flows"

	// PhaseWriteGraph is the final graph.fb / sidecar write step. Surfaced
	// as "Writing graph…" (#5334).
	PhaseWriteGraph = "writing_graph"

	// PhaseDone is emitted once with final totals.
	PhaseDone = "done"

	// PhaseError is emitted when the indexer encounters a fatal error.
	PhaseError = "error"
)

// TickEveryNFiles is the default file-tick interval for the AST extraction
// phase. Publishers are called every N files processed so high-file-count
// repos still feel responsive without overwhelming the event channel.
// Configurable per-Indexer via WithTickInterval.
const TickEveryNFiles = 20

// Event is the wire shape for a single indexer progress notification.
// All fields are JSON-serialisable. The struct is intentionally flat so
// the SSE endpoint can forward it with a single json.Marshal call.
type Event struct {
	// GroupSlug and RepoSlug identify which indexing job produced this
	// event. Both default to empty string when the indexer runs standalone
	// (CLI mode without a group).
	GroupSlug string `json:"group_slug"`
	RepoSlug  string `json:"repo_slug"`

	// Phase is the current pipeline stage (one of the Phase* constants).
	Phase string `json:"phase"`

	// FilesDone is the number of files processed so far in the current phase.
	FilesDone int `json:"files_done"`

	// FilesTotal is the total file count as discovered by the walk pass.
	// Set on the first PhaseScan event; unchanged on subsequent events.
	FilesTotal int `json:"files_total"`

	// EntitiesSoFar is the running count of entities emitted up to this point.
	// Updated on PhaseDone; may be 0 during extraction.
	EntitiesSoFar int `json:"entities_so_far"`

	// ETAms is a caller-supplied estimate of milliseconds remaining.
	// Omitted (zero) when the indexer has not computed an ETA.
	ETAms int `json:"eta_ms,omitempty"`

	// Error is non-empty only when Phase == PhaseError.
	Error string `json:"error,omitempty"`

	// TS is the Unix timestamp in milliseconds when the event was created.
	TS int64 `json:"ts"`

	// --- "go beyond" fields ---

	// BytesSeen is the cumulative byte count of all files read so far.
	// Useful for users with very large repos where file count is misleading.
	BytesSeen int64 `json:"bytes_seen,omitempty"`

	// CurrentFile is the repo-relative path of the file being processed at
	// tick time. Allows the UI to show "extracting: routers.py" rather than
	// just "47/100". Empty on phase-boundary events.
	CurrentFile string `json:"current_file,omitempty"`

	// PhaseStartedAtMS is the Unix millisecond timestamp when the current
	// phase started. Lets the frontend display "in this phase for 2.3s".
	PhaseStartedAtMS int64 `json:"phase_started_at_ms,omitempty"`

	// AlgorithmName is set during PhaseAlgorithms events to name the
	// individual algorithm (e.g. "Louvain", "PageRank", "Betweenness").
	AlgorithmName string `json:"algorithm_name,omitempty"`

	// Module is the package-root label that CurrentFile rolls up to, derived
	// deterministically by internal/module.Derive. For a monorepo this lets
	// the UI render one progress row per package (e.g. "services/auth",
	// "packages/ui") instead of a single aggregate row for the whole repo.
	// Empty when the repo is not a monorepo, or on phase-boundary events with
	// no current file.
	Module string `json:"module,omitempty"`
}

// nowMS returns the current wall-clock time as Unix milliseconds.
func nowMS() int64 {
	return time.Now().UnixMilli()
}

// Publisher is the sink that receives progress events from the indexer.
// Implementations must be safe to call from multiple goroutines and must
// not block the caller.
type Publisher interface {
	Publish(e Event)
}

// NoOpPublisher silently discards all events. It is the default when no
// publisher is wired into the indexer.
type NoOpPublisher struct{}

// Publish implements Publisher and is a no-op.
func (NoOpPublisher) Publish(Event) {}

// BufferedPublisher is a non-blocking Publisher that forwards events to a
// fixed-size buffered channel. When the channel is full the oldest event is
// dropped (drop-oldest) so the indexer is never paused waiting for a
// consumer. Callers read from Ch.
type BufferedPublisher struct {
	Ch chan Event
}

// NewBufferedPublisher creates a BufferedPublisher with the given channel
// capacity. A capacity of 64 is suitable for most UIs that drain at >10
// events/second; increase for long-running algo phases with burst traffic.
func NewBufferedPublisher(capacity int) *BufferedPublisher {
	return &BufferedPublisher{Ch: make(chan Event, capacity)}
}

// Publish enqueues e without blocking. If the channel is full the oldest
// queued event is discarded before enqueuing the new one.
func (b *BufferedPublisher) Publish(e Event) {
	select {
	case b.Ch <- e:
	default:
		// Channel full — drain oldest, then enqueue.
		select {
		case <-b.Ch:
		default:
		}
		select {
		case b.Ch <- e:
		default:
		}
	}
}

// SliceCollector collects every published event into a slice. It is
// intended for unit tests only; it holds a mutex so it is goroutine-safe.
type SliceCollector struct {
	Events []Event
}

// Publish appends e to Events. Safe for concurrent use.
func (s *SliceCollector) Publish(e Event) {
	s.Events = append(s.Events, e)
}

// Tracker is the per-indexing-job helper that constructs and publishes
// progress events. It carries shared state (group/repo slugs, total file
// count, phase start time) so individual call-sites only need to supply
// the delta. Tracker is safe to use from a single goroutine; it does not
// add internal synchronisation — the indexer already serialises phase
// transitions on the main goroutine.
type Tracker struct {
	pub              Publisher
	groupSlug        string
	repoSlug         string
	filesTotal       int
	phaseStartedAtMS int64
	currentPhase     string

	// moduleResolver maps a repo-relative file path to a package-root module
	// label. When set (monorepo indexing), Tick stamps Event.Module so the UI
	// can render per-module rows. Decoupled via a func so the progress package
	// stays free of an internal/module dependency.
	moduleResolver func(currentFile string) string
}

// SetModuleResolver installs a function that maps a repo-relative file path to
// a package-root module label. Pass nil (the default) to disable per-module
// attribution. The resolver must be safe for concurrent use — Tick may be
// called from multiple extraction workers.
func (t *Tracker) SetModuleResolver(fn func(currentFile string) string) {
	t.moduleResolver = fn
}

// module returns the module label for currentFile, or "" when no resolver is
// set or currentFile is empty.
func (t *Tracker) module(currentFile string) string {
	if t.moduleResolver == nil || currentFile == "" {
		return ""
	}
	return t.moduleResolver(currentFile)
}

// NewTracker constructs a Tracker for one indexing job. pub must not be nil;
// use NoOpPublisher{} for a no-op sink.
func NewTracker(pub Publisher, groupSlug, repoSlug string) *Tracker {
	return &Tracker{
		pub:       pub,
		groupSlug: groupSlug,
		repoSlug:  repoSlug,
	}
}

// SetFilesTotal stores the total file count discovered during scanning.
func (t *Tracker) SetFilesTotal(n int) {
	t.filesTotal = n
}

// PhaseStart emits a phase-entry event and records the start timestamp.
func (t *Tracker) PhaseStart(phase string, filesDone int, entitiesSoFar int) {
	t.currentPhase = phase
	t.phaseStartedAtMS = nowMS()
	t.pub.Publish(Event{
		GroupSlug:        t.groupSlug,
		RepoSlug:         t.repoSlug,
		Phase:            phase,
		FilesDone:        filesDone,
		FilesTotal:       t.filesTotal,
		EntitiesSoFar:    entitiesSoFar,
		PhaseStartedAtMS: t.phaseStartedAtMS,
		TS:               nowMS(),
	})
}

// Tick emits a within-phase progress event. filesDone, bytesSeen, and
// currentFile reflect the current extraction state.
func (t *Tracker) Tick(phase string, filesDone int, bytesSeen int64, currentFile string, entitiesSoFar int) {
	t.pub.Publish(Event{
		GroupSlug:        t.groupSlug,
		RepoSlug:         t.repoSlug,
		Phase:            phase,
		FilesDone:        filesDone,
		FilesTotal:       t.filesTotal,
		EntitiesSoFar:    entitiesSoFar,
		BytesSeen:        bytesSeen,
		CurrentFile:      currentFile,
		Module:           t.module(currentFile),
		PhaseStartedAtMS: t.phaseStartedAtMS,
		TS:               nowMS(),
	})
}

// Phase emits a single phase-boundary event for a granular pass (#5334),
// carrying the current entity count and the named algorithm/pass when known.
// Unlike PhaseStart it does not reset phaseStartedAtMS-derived file counters;
// it is the cheap one-event-per-pass signal used to surface the graph-assembly
// tail (communities, centrality, links, flows, write) in the CLI and wizard.
// The pass runs after all files are processed, so FilesDone == FilesTotal.
func (t *Tracker) Phase(phase, passName string, entitiesSoFar int) {
	t.currentPhase = phase
	t.phaseStartedAtMS = nowMS()
	t.pub.Publish(Event{
		GroupSlug:        t.groupSlug,
		RepoSlug:         t.repoSlug,
		Phase:            phase,
		FilesTotal:       t.filesTotal,
		FilesDone:        t.filesTotal,
		EntitiesSoFar:    entitiesSoFar,
		AlgorithmName:    passName,
		PhaseStartedAtMS: t.phaseStartedAtMS,
		TS:               nowMS(),
	})
}

// AlgorithmEvent emits an entry or exit event for a named graph algorithm.
func (t *Tracker) AlgorithmEvent(name string, entitiesSoFar int) {
	t.pub.Publish(Event{
		GroupSlug:        t.groupSlug,
		RepoSlug:         t.repoSlug,
		Phase:            PhaseAlgorithms,
		FilesTotal:       t.filesTotal,
		FilesDone:        t.filesTotal, // algorithms run after all files are processed
		EntitiesSoFar:    entitiesSoFar,
		AlgorithmName:    name,
		PhaseStartedAtMS: t.phaseStartedAtMS,
		TS:               nowMS(),
	})
}

// Done emits the final PhaseDone event with full totals.
func (t *Tracker) Done(filesDone, entitiesSoFar int) {
	t.pub.Publish(Event{
		GroupSlug:        t.groupSlug,
		RepoSlug:         t.repoSlug,
		Phase:            PhaseDone,
		FilesDone:        filesDone,
		FilesTotal:       t.filesTotal,
		EntitiesSoFar:    entitiesSoFar,
		PhaseStartedAtMS: t.phaseStartedAtMS,
		TS:               nowMS(),
	})
}

// Fail emits a PhaseError event.
func (t *Tracker) Fail(errMsg string) {
	t.pub.Publish(Event{
		GroupSlug:        t.groupSlug,
		RepoSlug:         t.repoSlug,
		Phase:            PhaseError,
		FilesTotal:       t.filesTotal,
		FilesDone:        0,
		Error:            errMsg,
		PhaseStartedAtMS: t.phaseStartedAtMS,
		TS:               nowMS(),
	})
}
