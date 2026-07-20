// Package proto defines the wire types exchanged between the grafel
// CLI client and the long-running daemon over the Unix-domain socket.
//
// Per ADR-0017 the transport is net/rpc with the jsonrpc codec. Service
// methods follow the net/rpc convention: exactly one argument struct and
// one reply pointer; service name is "Daemon".
//
// All types in this package MUST stay backwards-compatible — new fields
// may be added, existing fields must not change meaning. The daemon and
// the client binary may be at different versions during an upgrade.
package proto

// ServiceName is the net/rpc service name registered by the daemon.
const ServiceName = "Daemon"

// PingArgs is the no-op liveness probe argument.
type PingArgs struct{}

// PingReply carries the daemon's self-reported version.
type PingReply struct {
	Version string `json:"version"`
}

// StatusArgs requests a snapshot of daemon state.
type StatusArgs struct{}

// StatusReply summarises the daemon's runtime state. Fields are added
// here as new phases land; clients must tolerate missing fields.
type StatusReply struct {
	Version   string `json:"version"`
	PID       int    `json:"pid"`
	UptimeSec int64  `json:"uptime_sec"`
	// RSSBytes is the daemon's physical-memory reading. As of #3648 it is
	// sourced from the honest process footprint (resident set size), NOT
	// runtime.MemStats.Sys (reserved virtual address space) which it
	// previously — and incorrectly — reported and labeled "actual RSS".
	RSSBytes uint64 `json:"rss_bytes"`

	// Honest memory breakdown (#3648). These are reported alongside RSSBytes
	// so clients can distinguish OS-level resident memory from Go-heap state.
	//
	//   FootprintBytes  — best honest physical-memory number (== RSSBytes).
	//   FootprintLabel  — names exactly what FootprintBytes measured, incl.
	//                     the macOS caveat that RSS under-counts swapped /
	//                     compressed pages (Activity Monitor's phys_footprint
	//                     may be larger).
	//   HeapInuseBytes  — MemStats.HeapInuse: heap spans with live objects.
	//   HeapReleasedBytes — MemStats.HeapReleased: heap returned to the OS
	//                     (rises after debug.FreeOSMemory / GOMEMLIMIT GC).
	//   SysBytes        — MemStats.Sys: total VA reserved from the OS. This is
	//                     the number that USED to be mislabeled as RSS.
	FootprintBytes    uint64 `json:"footprint_bytes,omitempty"`
	FootprintLabel    string `json:"footprint_label,omitempty"`
	HeapInuseBytes    uint64 `json:"heap_inuse_bytes,omitempty"`
	HeapReleasedBytes uint64 `json:"heap_released_bytes,omitempty"`
	SysBytes          uint64 `json:"sys_bytes,omitempty"`

	InFlight   int      `json:"in_flight"`
	Groups     []string `json:"groups,omitempty"`
	StartedAt  string   `json:"started_at"`
	SocketPath string   `json:"socket_path"`
	BinaryPath string   `json:"binary_path,omitempty"`

	// Phase B additions — watcher + scheduler observability. Fields
	// are optional; older clients ignore them.
	WatcherRepos   int                `json:"watcher_repos,omitempty"`
	WatcherDirs    int                `json:"watcher_dirs,omitempty"`
	WatcherEvents  uint64             `json:"watcher_events,omitempty"`
	WatcherDropped uint64             `json:"watcher_dropped,omitempty"`
	QueueLen       int                `json:"queue_len,omitempty"`
	IndexInFlight  []string           `json:"index_in_flight,omitempty"`
	PendingAlgo    []string           `json:"pending_algo,omitempty"`
	PendingLinks   []string           `json:"pending_links,omitempty"`
	IndexedRepos   []IndexedRepoState `json:"indexed_repos,omitempty"`
	RecentLog      []SchedLogEntry    `json:"recent_log,omitempty"`

	// Concurrency-cap additions. BudgetMB=0 means admission control is
	// disabled.
	//
	// RSSUsedMB is the ACTUAL daemon process RSS in MB — informational
	// only; it is NOT used in admission math.
	//
	// AdmissionUsedMB is the sum of predicted MB reserved by currently-
	// admitted jobs; this is what the scheduler compares against
	// RSSBudgetMB (delta-based accounting).
	//
	// InFlightJobs duplicates IndexInFlight with per-job predicted-MB.
	// BlockedJobs lists repos waiting for the budget to free.
	RSSBudgetMB     int64              `json:"rss_budget_mb,omitempty"`
	RSSUsedMB       int64              `json:"rss_used_mb,omitempty"`
	AdmissionUsedMB int64              `json:"admission_used_mb,omitempty"`
	InFlightJobs    []InFlightJobState `json:"in_flight_jobs,omitempty"`
	BlockedJobs     []string           `json:"blocked_jobs,omitempty"`

	// DashboardPort is the TCP port the daemon's embedded dashboard HTTP
	// server is bound to. Zero means the dashboard is not running.
	// Added in #938.
	DashboardPort int `json:"dashboard_port,omitempty"`

	// Rebuild observability fields added in #2097.
	//
	// RebuildGroupsActive is the number of groups whose per-group mutex is
	// currently held (i.e. a Rebuild RPC is actively running for that group).
	// A value >0 while InFlight is low hints at stalled per-group mutexes.
	//
	// RebuildInFlight mirrors InFlight but scoped to Rebuild RPCs only (not
	// Index or QualityAudit calls). Populated server-side for diagnostic clarity.
	//
	// RebuildConcurrencyCap is the current parallel-repo cap for Rebuild RPCs
	// (#2127). Auto-tuned from system memory (floor=2, cap=8); overrideable
	// via GRAFEL_REBUILD_CONCURRENCY or --max-concurrent-groups.
	RebuildGroupsActive   int `json:"rebuild_groups_active,omitempty"`
	RebuildInFlight       int `json:"rebuild_in_flight,omitempty"`
	RebuildConcurrencyCap int `json:"rebuild_concurrency_cap,omitempty"`

	// PH2a (#2096): watcher pause/resume counters.
	// WatcherActiveSlots is the number of (repoPath,ref) slots whose fsnotify
	// subscription is currently active.
	// WatcherPausedSlots is the number of slots whose fsnotify subscription
	// has been suspended because the slot is COLD.
	WatcherActiveSlots int `json:"watcher_active_slots,omitempty"`
	WatcherPausedSlots int `json:"watcher_paused_slots,omitempty"`

	// S7 (#2157): operational mode the daemon booted with.
	// One of "background", "workstation", "readonly". Empty for daemons
	// predating S7 (backwards-compatible; older clients ignore the field).
	DaemonMode string `json:"daemon_mode,omitempty"`
}

// InFlightJobState mirrors sched.InFlightJob on the wire.
type InFlightJobState struct {
	Path        string `json:"path"`
	PredictedMB int64  `json:"predicted_mb"`
}

// IndexedRepoState mirrors sched.RepoSnapshot for the wire.
type IndexedRepoState struct {
	Path        string `json:"path"`
	LastIndex   string `json:"last_index,omitempty"`
	LastAlgo    string `json:"last_algo,omitempty"`
	IndexCount  int64  `json:"index_count"`
	AlgoCount   int64  `json:"algo_count"`
	LastErr     string `json:"last_err,omitempty"`
	LastPeakMB  int64  `json:"last_peak_mb,omitempty"`
	PredictedMB int64  `json:"predicted_mb,omitempty"`

	// #5727/#5729-W1 — the exact commit the on-disk graph was indexed at,
	// and whether it still matches HEAD. Sourced from daemon.IndexedCommitForRepo
	// (diff-manifest sidecar, falling back to the graph.fb header). Empty/false
	// for a repo that has never been indexed, or a graph predating this field.
	IndexedCommit      string `json:"indexed_commit,omitempty"`
	IndexedCommitShort string `json:"indexed_commit_short,omitempty"`
	AtHead             bool   `json:"at_head,omitempty"`
}

// SchedLogEntry is the wire form of a scheduler log entry.
type SchedLogEntry struct {
	Time string `json:"time"`
	Kind string `json:"kind"`
	Repo string `json:"repo,omitempty"`
	Msg  string `json:"msg"`
}

// IndexArgs requests a one-shot index of a single repository. Mirrors the
// flags of the old `grafel index <repo>` subcommand.
type IndexArgs struct {
	RepoPath    string   `json:"repo_path"`
	OutPath     string   `json:"out_path,omitempty"`
	RepoTag     string   `json:"repo_tag,omitempty"`
	SkipPasses  []string `json:"skip_passes,omitempty"`
	Pretty      bool     `json:"pretty,omitempty"`
	JSONStats   bool     `json:"json_stats,omitempty"`
	Repair      bool     `json:"repair,omitempty"`
	RepairApply bool     `json:"repair_apply,omitempty"`
	ExportFB    bool     `json:"export_fb,omitempty"` // deprecated no-op; graph.fb always written since #808
	// PrintSkipped, when true, emits one [skip] line per skipped directory
	// at walk-time showing which rule matched (issue #805).
	PrintSkipped bool `json:"print_skipped,omitempty"`
	// AdditionalSkipDirs extends the walk-time hard-coded skip list with
	// per-group names from fleet.json's additional_skip_dirs field.
	AdditionalSkipDirs []string `json:"additional_skip_dirs,omitempty"`
	ExportJSON         bool     `json:"export_json,omitempty"` // when true, also write graph.json alongside graph.fb (ADR-0016 flip-day)
	// Async, when true, makes the Index RPC ENQUEUE the repo onto the
	// daemon's debounced/coalescing scheduler (the same fast reactive path
	// the file-watcher uses) and ACK immediately, rather than running a full
	// synchronous index and blocking until it completes. Used by git hooks
	// (post-commit/-merge/-checkout) so git writes are never blocked on a
	// reindex, and so concurrent worktrees + commit bursts coalesce into a
	// single per-repo reindex instead of stampeding the daemon (#3366).
	// When false (default) the RPC keeps its synchronous behaviour, used by
	// `rebuild` and manual `grafel index`.
	Async bool `json:"async,omitempty"`
}

// IndexReply carries the post-index summary. The stats are an opaque
// JSON blob (the same shape the old `--json-stats` flag printed) so the
// daemon can extend them without forcing a client release.
type IndexReply struct {
	RepoPath  string `json:"repo_path"`
	GraphPath string `json:"graph_path,omitempty"`
	StatsJSON string `json:"stats_json,omitempty"`
}

// RebuildArgs requests a force-rebuild of a group (optionally narrowed
// to a single repo slug).
type RebuildArgs struct {
	Group string `json:"group"`
	Slug  string `json:"slug,omitempty"`
	Wipe  bool   `json:"wipe,omitempty"`
	// ProgressToken, when non-empty, causes the daemon to store per-repo
	// progress events under this key so the CLI can poll them via the
	// IndexProgress RPC. Clients should use a short unique string (e.g.
	// a timestamp + random suffix). Empty disables progress tracking.
	ProgressToken string `json:"progress_token,omitempty"`
	// Incremental enables diff-aware re-indexing (issue #1339). When true
	// the daemon only re-processes files whose SHA-256 content hash changed
	// since the last successful run. Wipe=true overrides Incremental (a wipe
	// is always a full rebuild).
	Incremental bool `json:"incremental,omitempty"`
	// Interactive marks this rebuild as a human-awaited FOREGROUND request
	// (dashboard wizard index, explicit CLI `grafel index`/`rebuild`/`wizard`,
	// repair) rather than an automatic background watcher/git-hook reindex
	// (#5328). Foreground rebuilds acquire the index gate on the priority lane
	// (jump ahead of queued background work, may use the reserved slot) and run
	// at the higher rebuild CPU cap; background reindexes yield while they run.
	// Defaults to false so an unset caller is treated as background — the safe,
	// throttled classification.
	Interactive bool `json:"interactive,omitempty"`
	// WaitForCompletion asks the daemon to BLOCK this RPC until the group
	// rebuild has actually finished, then return — restoring "err==nil means the
	// rebuild really ran" for callers that need it (e.g. `grafel group add
	// --index`, which reports "indexed": true off this call). See #5790.
	//
	// It only changes behavior in SPLIT MODE, where Service.Rebuild otherwise
	// enqueues a KindRebuild request onto the engine queue and returns
	// immediately (fire-and-forget) — so a nil error meant "enqueue acked", NOT
	// "rebuild done". When set, the serve-side handler enqueues the request and
	// then waits for the engine to drain+ack it (the same request-ack signal the
	// wizard uses via daemon.RebuildRequestPending), returning nil only on real
	// completion and an error on engine-death / never-alive / timeout. In
	// MONOLITH mode Rebuild is already synchronous, so this flag is a harmless
	// no-op there. Defaults to false to preserve the fire-and-forget fast path
	// for callers (watchers, background triggers) that do not await completion.
	WaitForCompletion bool `json:"wait_for_completion,omitempty"`
	// RepoTimeout overrides the per-repo rebuild watchdog
	// (defaultPerRepoRebuildTimeout / GRAFEL_REBUILD_REPO_TIMEOUT) for THIS
	// invocation only — a Go duration string (e.g. "45m"), or "0" to disable
	// the bound entirely. Empty (the default) falls back to the env-configured
	// or built-in default. Lets a caller with a genuinely large monorepo raise
	// the timeout for a single `grafel rebuild --timeout <dur>` without editing
	// the daemon's env and reloading it (#5822 sub-ask 3).
	RepoTimeout string `json:"repo_timeout,omitempty"`
}

// RebuildReply lists the repos that were rebuilt and any warning that
// applies to the whole batch.
type RebuildReply struct {
	// Repos holds the slug of each rebuilt repo (display name).
	Repos []string `json:"repos"`
	// RepoPaths holds the absolute on-disk path of each rebuilt repo, in the
	// same order as Repos. The CLI uses these paths to locate per-repo state
	// directories (graph.fb, graph-stats.json, enrichment-candidates.json)
	// for the post-rebuild summary. Added to fix #1076 where slugs were
	// passed to StateDirForRepo, producing wrong relative paths and zero
	// entity/relationship counts.
	RepoPaths []string `json:"repo_paths,omitempty"`
	Warning   string   `json:"warning,omitempty"`
	// Summary fields — populated when the daemon tracked per-repo stats.
	TotalEntities int64   `json:"total_entities,omitempty"`
	TotalRels     int64   `json:"total_rels,omitempty"`
	ElapsedSec    float64 `json:"elapsed_sec,omitempty"`
}

// IndexProgressArgs polls the progress of a rebuild operation started
// with a ProgressToken.
type IndexProgressArgs struct {
	Token string `json:"token"`
}

// IndexProgressPhase is a phase label for a single-repo progress event.
//
// Values: "queued", "started", "walking", "extracting", "finalizing", "completed", "failed".
type IndexProgressPhase = string

const (
	PhaseQueued     IndexProgressPhase = "queued"
	PhaseStarted    IndexProgressPhase = "started"
	PhaseWalking    IndexProgressPhase = "walking"
	PhaseExtracting IndexProgressPhase = "extracting"
	PhaseFinalizing IndexProgressPhase = "finalizing"
	PhaseCompleted  IndexProgressPhase = "completed"
	PhaseFailed     IndexProgressPhase = "failed"
)

// RepoProgressState holds the current progress snapshot for one repo in
// a rebuild batch.
type RepoProgressState struct {
	// Slug is the short repo name (last path component).
	Slug string `json:"slug"`
	// Path is the absolute on-disk path.
	Path string `json:"path"`
	// Phase is the current lifecycle phase.
	Phase IndexProgressPhase `json:"phase"`
	// Index is 1-based position in the batch (0 if unknown).
	Index int `json:"index"`
	// Total is the total number of repos in the batch.
	Total int `json:"total"`
	// FilesWalked is how many files were seen during the walk phase.
	FilesWalked int `json:"files_walked,omitempty"`
	// FilesExtracted is how many files have been extracted so far.
	FilesExtracted int `json:"files_extracted,omitempty"`
	// Entities is the final entity count (set on completion).
	Entities int64 `json:"entities,omitempty"`
	// Rels is the final relationship count (set on completion).
	Rels int64 `json:"rels,omitempty"`
	// ElapsedSec is seconds since this repo's indexing started.
	ElapsedSec float64 `json:"elapsed_sec,omitempty"`
	// ErrMsg is non-empty when Phase is "failed".
	ErrMsg string `json:"err_msg,omitempty"`
	// UpdatedAt is the Unix timestamp of the last update (for heartbeat detection).
	UpdatedAt int64 `json:"updated_at"`
}

// IndexProgressReply is the poll response for an in-flight rebuild.
type IndexProgressReply struct {
	// Token echoes the request token.
	Token string `json:"token"`
	// Done is true when all repos have reached a terminal state
	// (completed or failed) and the rebuild RPC has returned.
	Done bool `json:"done"`
	// Repos is the per-repo progress snapshot, ordered by index.
	Repos []RepoProgressState `json:"repos"`
	// GroupName is the group being rebuilt.
	GroupName string `json:"group_name,omitempty"`
	// TotalEntities is the sum of entities so far (updated on each completion).
	TotalEntities int64 `json:"total_entities,omitempty"`
	// TotalRels is the sum of rels so far.
	TotalRels int64 `json:"total_rels,omitempty"`
	// ElapsedSec is total wall time since the rebuild started.
	ElapsedSec float64 `json:"elapsed_sec,omitempty"`
}

// StopArgs requests a graceful shutdown of the daemon.
type StopArgs struct{}

// StopReply is empty; the daemon closes the socket once the reply is
// flushed and exits when the last in-flight request finishes.
type StopReply struct{}

// RemoveRepoArgs requests the daemon unregister a single repo from a group.
// The watcher is stopped, the git hook block is removed, and the per-repo
// .grafel/ cache is deleted (unless KeepCache is true). The repo entry
// is removed from the fleet config and the fleet is persisted.
type RemoveRepoArgs struct {
	Group     string `json:"group"`
	Slug      string `json:"slug"`
	KeepCache bool   `json:"keep_cache,omitempty"`
}

// RemoveRepoReply is returned by the RemoveRepo RPC.
type RemoveRepoReply struct {
	// RepoPath is the absolute on-disk path of the removed repo.
	RepoPath string `json:"repo_path"`
	// FreedBytes is the number of bytes reclaimed from .grafel/. Zero
	// when KeepCache was true or the directory did not exist.
	FreedBytes int64 `json:"freed_bytes"`
}

// DeleteGroupArgs requests the daemon tear down every repo in a group and
// remove the group entirely. Per-repo teardown mirrors RemoveRepo for each
// member. The fleet config file and the per-group state directory are deleted.
type DeleteGroupArgs struct {
	Group      string `json:"group"`
	KeepCaches bool   `json:"keep_caches,omitempty"`
}

// DeleteGroupReply is returned by the DeleteGroup RPC.
type DeleteGroupReply struct {
	// RemovedRepos lists the slugs of every repo that was removed.
	RemovedRepos []string `json:"removed_repos"`
	// FreedBytes is the total bytes reclaimed across all per-repo caches.
	FreedBytes int64 `json:"freed_bytes"`
}
