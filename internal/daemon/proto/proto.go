// Package proto defines the wire types exchanged between the archigraph
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
	Version    string   `json:"version"`
	PID        int      `json:"pid"`
	UptimeSec  int64    `json:"uptime_sec"`
	RSSBytes   uint64   `json:"rss_bytes"`
	InFlight   int      `json:"in_flight"`
	Groups     []string `json:"groups,omitempty"`
	StartedAt  string   `json:"started_at"`
	SocketPath string   `json:"socket_path"`

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
	// disabled. UsedMB is the sum of predicted RSS reserved by jobs
	// currently running. InFlightJobs duplicates IndexInFlight with
	// per-job predicted-MB so the status formatter can print headroom.
	// BlockedJobs lists repos waiting for the budget to free.
	RSSBudgetMB  int64               `json:"rss_budget_mb,omitempty"`
	RSSUsedMB    int64               `json:"rss_used_mb,omitempty"`
	InFlightJobs []InFlightJobState  `json:"in_flight_jobs,omitempty"`
	BlockedJobs  []string            `json:"blocked_jobs,omitempty"`
}

// InFlightJobState mirrors sched.InFlightJob on the wire.
type InFlightJobState struct {
	Path        string `json:"path"`
	PredictedMB int64  `json:"predicted_mb"`
}

// IndexedRepoState mirrors sched.RepoSnapshot for the wire.
type IndexedRepoState struct {
	Path       string `json:"path"`
	LastIndex  string `json:"last_index,omitempty"`
	LastAlgo   string `json:"last_algo,omitempty"`
	IndexCount  int64  `json:"index_count"`
	AlgoCount   int64  `json:"algo_count"`
	LastErr     string `json:"last_err,omitempty"`
	LastPeakMB  int64  `json:"last_peak_mb,omitempty"`
	PredictedMB int64  `json:"predicted_mb,omitempty"`
}

// SchedLogEntry is the wire form of a scheduler log entry.
type SchedLogEntry struct {
	Time string `json:"time"`
	Kind string `json:"kind"`
	Repo string `json:"repo,omitempty"`
	Msg  string `json:"msg"`
}

// IndexArgs requests a one-shot index of a single repository. Mirrors the
// flags of the old `archigraph index <repo>` subcommand.
type IndexArgs struct {
	RepoPath    string   `json:"repo_path"`
	OutPath     string   `json:"out_path,omitempty"`
	RepoTag     string   `json:"repo_tag,omitempty"`
	SkipPasses  []string `json:"skip_passes,omitempty"`
	Pretty      bool     `json:"pretty,omitempty"`
	JSONStats   bool     `json:"json_stats,omitempty"`
	Repair      bool     `json:"repair,omitempty"`
	RepairApply bool     `json:"repair_apply,omitempty"`
	ExportFB    bool     `json:"export_fb,omitempty"`  // deprecated no-op; graph.fb always written since #808
	// PrintSkipped, when true, emits one [skip] line per skipped directory
	// at walk-time showing which rule matched (issue #805).
	PrintSkipped bool `json:"print_skipped,omitempty"`
	// AdditionalSkipDirs extends the walk-time hard-coded skip list with
	// per-group names from fleet.json's additional_skip_dirs field.
	AdditionalSkipDirs []string `json:"additional_skip_dirs,omitempty"`
	ExportJSON  bool     `json:"export_json,omitempty"`  // when true, also write graph.json alongside graph.fb (ADR-0016 flip-day)
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
}

// RebuildReply lists the repos that were rebuilt and any warning that
// applies to the whole batch.
type RebuildReply struct {
	Repos   []string `json:"repos"`
	Warning string   `json:"warning,omitempty"`
}

// StopArgs requests a graceful shutdown of the daemon.
type StopArgs struct{}

// StopReply is empty; the daemon closes the socket once the reply is
// flushed and exits when the last in-flight request finishes.
type StopReply struct{}
