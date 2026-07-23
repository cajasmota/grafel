package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/requests"
	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/daemon/watch"
	"github.com/cajasmota/grafel/internal/install/hooks"
	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/perf"
	"github.com/cajasmota/grafel/internal/process"
	"github.com/cajasmota/grafel/internal/progress"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/statusfile"
	"github.com/cajasmota/grafel/internal/version"
)

// rebuildRPCTimeout is the maximum wall-clock time a single Rebuild RPC is
// allowed to run. A rebuild that exceeds this limit is treated as a deadlock
// and returns an error so the RPC client is unblocked. The underlying index
// goroutines are abandoned (they will eventually complete or panic-recover and
// be cleaned up). 2 hours is intentionally conservative — even a full cold
// re-index of a large group should finish well under 30 minutes in practice.
//
// A var (not const) so tests can shrink it to exercise the timeout / single-
// flight paths deterministically; production never reassigns it.
var rebuildRPCTimeout = 2 * time.Hour

// defaultStallWarnInterval is how long a Rebuild RPC may run without producing
// a result before the dead-man detector logs a "possible stall" warning plus a
// goroutine dump. Kept well under rebuildRPCTimeout so a wedged rebuild is
// surfaced (and made diagnosable) long before the hard 2-hour cap. Overridable
// via GRAFEL_STALL_WARN_INTERVAL (a Go duration string) so operators and tests
// can shorten it without a redeploy (#5326).
const defaultStallWarnInterval = 5 * time.Minute

// maxGoroutineDumpBytes caps the goroutine dump captured on a stall so a daemon
// with thousands of goroutines cannot emit a multi-megabyte log line.
const maxGoroutineDumpBytes = 1 << 20 // 1 MiB

// resolveStallWarnInterval returns the dead-man warning interval, honoring the
// GRAFEL_STALL_WARN_INTERVAL override when it parses to a positive duration.
func resolveStallWarnInterval() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("GRAFEL_STALL_WARN_INTERVAL")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	return defaultStallWarnInterval
}

// captureGoroutineDump returns a full goroutine stack dump (all goroutines),
// bounded to maxGoroutineDumpBytes. It is used by the rebuild stall detector so
// the next stall can be root-caused from the daemon log alone (#5326).
func captureGoroutineDump() string {
	buf := make([]byte, maxGoroutineDumpBytes)
	n := runtime.Stack(buf, true)
	return string(buf[:n])
}

// IndexFunc runs a one-shot index. The daemon does not import the
// extractor stack directly — that lives in cmd/grafel — so it
// receives the entrypoint as a function value at construction time.
// Returning the graph.json path and the stats JSON (opaque) keeps the
// wire shape stable as the extractor evolves.
type IndexFunc func(args proto.IndexArgs) (graphPath string, statsJSON string, err error)

// RebuildFunc force-rebuilds a group. As with IndexFunc, the daemon
// stays decoupled from registry + extractor — the entrypoint is
// injected from cmd/grafel at construction.
type RebuildFunc func(args proto.RebuildArgs) (repos []string, warning string, err error)

// QualityAuditFunc runs the audit-orphans analysis for a repo (or
// corpus directory). Returns the pre-formatted markdown (or JSON) report
// and the scalar summary. Like IndexFunc, the heavy audit package lives
// in cmd/grafel and is injected here at construction time.
type QualityAuditFunc func(args proto.QualityAuditRequest) (reply proto.QualityAuditReply, err error)

// watcherMgrStatsIface is the narrow interface used by Service.Status to read
// PH2a watcher pause/resume slot counts without importing internal/daemon/watch.
type watcherMgrStatsIface interface {
	ActiveCount() int
	PausedCount() int
}

// rebuildSession holds in-flight progress state for one rebuild batch.
// It is keyed by the ProgressToken supplied in RebuildArgs.
type rebuildSession struct {
	mu        sync.RWMutex
	startedAt time.Time
	group     string
	repos     []proto.RepoProgressState
	done      bool
	// Totals accumulated as each repo completes.
	totalEntities int64
	totalRels     int64
}

// snapshot returns a copy of the session's current state.
func (rs *rebuildSession) snapshot() proto.IndexProgressReply {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	repos := make([]proto.RepoProgressState, len(rs.repos))
	copy(repos, rs.repos)
	return proto.IndexProgressReply{
		Done:          rs.done,
		GroupName:     rs.group,
		Repos:         repos,
		TotalEntities: rs.totalEntities,
		TotalRels:     rs.totalRels,
		ElapsedSec:    time.Since(rs.startedAt).Seconds(),
	}
}

// updateRepo updates a single repo's state in the session.
func (rs *rebuildSession) updateRepo(idx int, fn func(*proto.RepoProgressState)) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if idx >= 0 && idx < len(rs.repos) {
		fn(&rs.repos[idx])
		rs.repos[idx].UpdatedAt = time.Now().Unix()
	}
}

// addEntities accumulates final entity/rel counts into the session total.
func (rs *rebuildSession) addEntities(entities, rels int64) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.totalEntities += entities
	rs.totalRels += rels
}

// Service is the RPC handler registered under proto.ServiceName. All
// public methods follow the net/rpc signature so jsonrpc can invoke
// them: func (s *Service) Method(args *T1, reply *T2) error.
//
// The Service is goroutine-safe by virtue of (a) atomic counters for
// in-flight tracking, and (b) the underlying IndexFunc/RebuildFunc
// being responsible for their own concurrency.
type Service struct {
	startedAt    time.Time
	socketPath   string
	index        IndexFunc
	rebuild      RebuildFunc
	qualityAudit QualityAuditFunc
	stopReq      chan<- struct{}
	stopped      int32 // atomic; 1 once stopReq has been closed
	inFlight     int64

	// maxConcurrentGroups controls how many groups may be rebuilt in
	// parallel inside a Rebuild RPC. 0 and 1 both mean serial; >= 2
	// enables the worker pool introduced in #1276.
	maxConcurrentGroups int

	// groupRebuildMu prevents a concurrent double-rebuild of the same group.
	// Keyed by group name. Each value is a capacity-1 semaphore channel
	// (chan struct{}), loaded-or-stored atomically via sync.Map to avoid the
	// TOCTOU race a plain map + RWMutex would have. Wired into Service.Rebuild
	// in #2097; converted from *sync.Mutex to a semaphore in #5681 so that (a)
	// acquisition is timeout-aware and (b) the guard is RELEASED from the
	// rebuild worker goroutine on real completion — not from the RPC handler —
	// so an RPC-timed-out orphan keeps the guard and a superseding same-group
	// rebuild cannot start a SECOND concurrent in-process rebuild (the engine
	// RSS blow-up: N overlapping runs each holding a multi-GB doc).
	groupRebuildMu sync.Map // map[string]chan struct{} (cap 1)

	// rebuildInFlight counts Rebuild RPCs that are currently inside
	// Service.Rebuild (i.e. past the per-group mutex acquisition). It is
	// separate from inFlight (which counts all RPC kinds) so Status can
	// surface the distinction for deadlock diagnosis (#2097).
	rebuildInFlight int64

	// groupsActiveCount is the number of groups whose per-group mutex is
	// currently held. Incremented when a group mutex is acquired, decremented
	// on release. Exposed via StatusReply.RebuildGroupsActive (#2097).
	groupsActiveCount int64

	// Phase B — populated only when the daemon is run with a watcher
	// + scheduler attached. Both may be nil in test wiring that
	// exercises just the RPC surface.
	watcher   *watch.Watcher
	scheduler *sched.Scheduler

	// watcherMgrStats provides PH2a pause/resume slot counts for the Status RPC.
	// Optional; nil when the watcher manager is not configured.
	watcherMgrStats watcherMgrStatsIface

	// #802 progress tracking — keyed by ProgressToken.
	progressMu sync.RWMutex
	progress   map[string]*rebuildSession

	// progressBroker is the serve-plane indexer progress bus (the SAME
	// *progress.Broker the dashboard SSE subscribes to; see
	// daemon.Config.ProgressBroker / SetProgressBroker). Rebuild uses it to
	// invalidate a group's retained terminal event the instant a NEW rebuild
	// is triggered (#5937) — see the ClearTerminal call in Rebuild. nil in
	// test wiring that does not construct a dashboard/broker.
	progressBroker *progress.Broker

	// Phase D — MCP RPC surface (ADR-0017 #832).
	// Both fields are injected from cmd/grafel to avoid the import
	// cycle that would arise from importing internal/mcp here.
	// nil means "not configured" — MCPToolList returns empty; MCPToolCall
	// returns a structured "daemon not ready" error.
	mcpListTools MCPListToolsFunc
	mcpCallTool  MCPCallToolFunc

	// logger is the daemon's structured logger, forwarded to the MCP
	// dispatcher for per-call debug logging (tool=name elapsed_ms=X repo=Y).
	// Handler selection (text vs JSON) happens at construction time via
	// GRAFEL_DAEMON_LOG_JSON; see newService.
	logger *slog.Logger

	// dashboardPort is the TCP port the embedded dashboard server is
	// bound to. Set by server.go after the dashboard goroutine starts.
	// Zero means dashboard is not running. Read by Status RPC (#938).
	dashboardPort int

	// daemonMode is the operational mode the daemon booted in (S7 #2157).
	// Surfaced by the Status RPC for `grafel status`.
	daemonMode string

	// ── Graceful MCP drain (#5633) ────────────────────────────────────────
	//
	// mcpDrain tracks MCP RPCs (MCPToolCall / MCPToolList) that are currently
	// executing inside the dispatcher. On shutdown Run sets draining=1 (so
	// newly-arriving MCP RPCs return a clean retryable error instead of being
	// half-served and hard-killed), then waits — with a bounded timeout — for
	// mcpDrain to reach zero before closing the socket. This lets a clean
	// daemon restart finish in-flight graph queries rather than dropping them
	// with "connection is shut down to the client".
	mcpDrain sync.WaitGroup
	draining int32 // atomic; 1 once graceful shutdown has begun
}

// newService wires the injected entrypoints onto a fresh Service. The
// stopReq channel is closed by Stop to signal the server loop; the
// service itself never re-closes it (a stopped atomic guards the close).
// logger may be nil; it is forwarded to the MCP dispatcher for debug-level
// per-call logging (tool=name elapsed_ms=X repo=Y).
// maxConcurrentGroups controls how many groups may be rebuilt in parallel
// (0 or 1 → serial; ≥2 → worker pool). Added in #1276.
//
// The logger is a *slog.Logger whose handler (text or JSON) is selected by
// the caller based on GRAFEL_DAEMON_LOG_JSON. Handler selection at
// construction time eliminates the prefix-corruption risk that required the
// startup guard removed in #2375 — slog cannot be misconfigured this way.
func newService(idx IndexFunc, rb RebuildFunc, qa QualityAuditFunc, socketPath string, stopReq chan<- struct{}, logger *slog.Logger, maxConcurrentGroups int) *Service {
	if maxConcurrentGroups < 1 {
		maxConcurrentGroups = 1
	}
	return &Service{
		startedAt:           time.Now(),
		socketPath:          socketPath,
		index:               idx,
		rebuild:             rb,
		qualityAudit:        qa,
		stopReq:             stopReq,
		progress:            make(map[string]*rebuildSession),
		logger:              logger,
		maxConcurrentGroups: maxConcurrentGroups,
	}
}

// SetProgressBroker wires the shared indexer progress broker into the
// service so Rebuild can invalidate a group's retained terminal event at the
// start of a new run (#5937). Mirrors dashboard.Server.SetProgressBroker.
// Optional: leaving it unset (nil, the default) simply means Rebuild skips
// the ClearTerminal call — used by tests that exercise the RPC surface
// without a dashboard/broker.
func (s *Service) SetProgressBroker(b *progress.Broker) {
	s.progressBroker = b
}

// beginDrain marks the service as draining so newly-arriving MCP RPCs are
// rejected with a clean retryable error rather than being half-served and then
// killed mid-flight when the socket closes (#5633). Idempotent.
func (s *Service) beginDrain() {
	atomic.StoreInt32(&s.draining, 1)
}

// isDraining reports whether graceful shutdown has begun.
func (s *Service) isDraining() bool {
	return atomic.LoadInt32(&s.draining) == 1
}

// enterMCP registers an in-flight MCP RPC for the graceful-drain WaitGroup,
// unless the daemon is already draining. It returns false when the caller
// must NOT proceed (the daemon is shutting down) — the caller then returns a
// clean retryable error so the bridge reconnects to the replacement daemon.
func (s *Service) enterMCP() (leave func(), ok bool) {
	if s.isDraining() {
		return func() {}, false
	}
	s.mcpDrain.Add(1)
	// Re-check after Add to close the race where beginDrain + waitDrain ran
	// between the isDraining check and the Add: if we are now draining, the
	// drain waiter may already have observed a zero counter, so undo and bail.
	if s.isDraining() {
		s.mcpDrain.Done()
		return func() {}, false
	}
	return func() { s.mcpDrain.Done() }, true
}

// waitDrain blocks until all in-flight MCP RPCs finish or the timeout elapses.
// Returns true if the drain completed cleanly, false if it timed out. Callers
// MUST have called beginDrain first so no new RPCs are admitted.
func (s *Service) waitDrain(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		s.mcpDrain.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Ping is the trivial liveness probe. Clients use it to distinguish
// "daemon not running" from "daemon running but unhealthy".
func (s *Service) Ping(_ *proto.PingArgs, reply *proto.PingReply) error {
	reply.Version = version.String()
	return nil
}

// Status reports a snapshot of daemon state. Memory is reported honestly
// (#3648): RSSBytes is sourced from the process footprint (resident set
// size) via internal/process, NOT runtime.MemStats.Sys — Sys is the
// reserved virtual address space, which previously over-reported by ~8GB
// and was wrongly labeled "actual RSS". Heap in-use / released / Sys are
// surfaced as distinct fields so clients can see the Go-heap breakdown.
// Phase B fields (watcher + scheduler) are populated when the daemon was
// started with both attached.
func (s *Service) Status(_ *proto.StatusArgs, reply *proto.StatusReply) error {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	reply.Version = version.String()
	reply.PID = os.Getpid()
	reply.UptimeSec = int64(time.Since(s.startedAt).Seconds())
	fp := process.FootprintBytes()
	reply.RSSBytes = fp.Bytes
	reply.FootprintBytes = fp.Bytes
	reply.FootprintLabel = fp.Label
	reply.HeapInuseBytes = ms.HeapInuse
	reply.HeapReleasedBytes = ms.HeapReleased
	reply.SysBytes = ms.Sys
	reply.InFlight = int(atomic.LoadInt64(&s.inFlight))
	reply.RebuildInFlight = int(atomic.LoadInt64(&s.rebuildInFlight))
	reply.RebuildGroupsActive = int(atomic.LoadInt64(&s.groupsActiveCount))
	reply.RebuildConcurrencyCap = s.maxConcurrentGroups
	reply.StartedAt = s.startedAt.UTC().Format(time.RFC3339)
	reply.SocketPath = s.socketPath
	// Report the binary path so clients can detect stale daemons (#855).
	if bin, err := os.Executable(); err == nil {
		reply.BinaryPath = bin
	}
	// Report the dashboard port so `grafel dashboard` can construct
	// the URL without a separate config read (#938).
	reply.DashboardPort = s.dashboardPort

	// S7 (#2157): surface the active operational mode.
	reply.DaemonMode = s.daemonMode

	if s.watcher != nil {
		repos, dirs, events, dropped := s.watcher.Stats()
		reply.WatcherRepos = repos
		reply.WatcherDirs = dirs
		reply.WatcherEvents = events
		reply.WatcherDropped = dropped
	}
	// PH2a (#2096): report pause/resume slot counts.
	if s.watcherMgrStats != nil {
		reply.WatcherActiveSlots = s.watcherMgrStats.ActiveCount()
		reply.WatcherPausedSlots = s.watcherMgrStats.PausedCount()
	}
	if s.scheduler != nil {
		snap := s.scheduler.Snapshot()
		reply.QueueLen = snap.QueueLen
		reply.PendingAlgo = snap.PendingAlgo
		reply.PendingLinks = snap.PendingLinks
		reply.RSSBudgetMB = snap.BudgetMB
		// RSSUsedMB reports actual measured daemon process RSS (informational).
		reply.RSSUsedMB = int64(reply.RSSBytes / (1024 * 1024))
		// AdmissionUsedMB is the scheduler's delta ledger: sum of predicted
		// MB reserved by currently-admitted jobs. This is what the admission
		// logic compares against BudgetMB — not the process RSS.
		reply.AdmissionUsedMB = snap.UsedMB
		reply.BlockedJobs = snap.BlockedJobs
		for _, j := range snap.InFlight {
			reply.IndexInFlight = append(reply.IndexInFlight, j.Path)
			reply.InFlightJobs = append(reply.InFlightJobs, proto.InFlightJobState{
				Path: j.Path, PredictedMB: j.PredictedMB,
			})
		}
		for _, r := range snap.IndexedRepos {
			ir := proto.IndexedRepoState{
				Path:        r.Path,
				IndexCount:  r.IndexCount,
				AlgoCount:   r.AlgoCount,
				LastErr:     r.LastErr,
				LastPeakMB:  r.LastPeakMB,
				PredictedMB: r.PredictedMB,
			}
			// #5727/#5729-W1: surface the exact indexed commit + freshness.
			ci := IndexedCommitForRepo(r.Path)
			ir.IndexedCommit = ci.Commit
			ir.IndexedCommitShort = ci.CommitShort
			ir.AtHead = ci.AtHead
			if !r.LastIndex.IsZero() {
				ir.LastIndex = r.LastIndex.UTC().Format(time.RFC3339)
			}
			if !r.LastAlgo.IsZero() {
				ir.LastAlgo = r.LastAlgo.UTC().Format(time.RFC3339)
			}
			reply.IndexedRepos = append(reply.IndexedRepos, ir)
		}
		for _, e := range snap.RecentLog {
			reply.RecentLog = append(reply.RecentLog, proto.SchedLogEntry{
				Time: e.Time.UTC().Format(time.RFC3339),
				Kind: e.Kind,
				Repo: e.Repo,
				Msg:  e.Msg,
			})
		}
	}
	return nil
}

// requestsDirForRepo is the serve→engine control-plane drop directory for a
// repo (ADR-0024 PR4, epic #5729): a `requests/` subdirectory sibling to
// repair.json / enrichment-candidates.json under the same
// StateDirForRepo(repoPath) root. See internal/daemon/requests.
func requestsDirForRepo(repoPath string) string {
	return filepath.Join(StateDirForRepo(repoPath), "requests")
}

// requestsDirForGroup is the serve→engine control-plane drop directory for a
// GROUP-level request (ADR-0024 PR6 prerequisite, epic #5729 — KindRebuild).
// A rebuild targets a group name (proto.RebuildArgs.Group), not a single
// repo path, so requestsDirForRepo's StateDirForRepo(repoPath) hashing
// doesn't apply directly — there is no repoPath to hash. Rather than teach
// discoverRequestsDirs a second directory shape, this synthesizes a stable
// per-group key ("group:<name>", which can never collide with a real
// absolute repo path's hash) and reuses the exact same repoBaseDir +
// refs/<ref-safe>/requests layout requestsDirForRepo produces, with the
// "_unknown" ref sentinel (a group has no git ref of its own). The result
// still matches discoverRequestsDirs' existing `root/*/refs/*/requests` glob
// verbatim, so the engine finds it with zero discovery-side changes.
//
// This intentionally does NOT resolve the group to its member repos here:
// proto.RebuildArgs.Group is resolved to repos lazily, INSIDE the RebuildFunc
// itself (see cmd/grafel's daemonRebuildFuncCore, which loads the group from
// the registry and iterates cfg.Repos), exactly as it already does when
// Service.Rebuild calls s.rebuild(*args) directly in monolith/engine mode.
// So one group-level request (not one-per-repo) is both simplest and
// correct: the engine drains it and calls the SAME RebuildFunc, which does
// its own group→repos expansion identically to today.
func requestsDirForGroup(group string) string {
	return filepath.Join(repoBaseDir("group:"+group), "refs", RefSafeEncode(""), "requests")
}

// Index runs a single-repo index synchronously. Phase B adds the
// MarkIndexed bookkeeping so an explicit RPC index updates the same
// in-memory state that the watcher-driven path uses.
func (s *Service) Index(args *proto.IndexArgs, reply *proto.IndexReply) error {
	if s.index == nil {
		return errors.New("index entrypoint not configured")
	}
	if args == nil || args.RepoPath == "" {
		return errors.New("repo_path is required")
	}

	// Async fast path (#3366): enqueue onto the debounced/coalescing
	// scheduler — the SAME reactive path the file-watcher uses — and ACK
	// immediately. The scheduler dedups per-repo (an enqueue for a repo
	// already pending or in-flight is coalesced, only the latest ref is
	// kept), so a commit burst or concurrent worktrees collapse into one
	// reindex instead of N back-to-back full reindexes. Used by git hooks
	// so git writes never block on a reindex. Falls back to the synchronous
	// path below when no scheduler is attached (e.g. a watcher-less daemon).
	//
	// ADR-0024 PR4 (epic #5729): in split mode this RPC is answered by the
	// SERVE process, which has no scheduler at all (s.scheduler is always
	// nil there — startEnginePlane, which assigns it, is skipped for
	// planeServeOnly). Falling through to the "no scheduler" branch below
	// would run the synchronous s.index(*args) call — a full reindex — IN
	// THE SERVE PROCESS, exactly the blast-radius coupling the split exists
	// to remove. So when SplitModeEnabled() and Async is requested, drop a
	// KindReindex request file instead of touching the scheduler; the
	// engine's drain loop (engineplane.go) picks it up and calls
	// scheduler.Enqueue(repoPath) itself — the same call this function makes
	// in monolith/engine mode. The two paths are mutually exclusive: split
	// mode NEVER calls s.scheduler.Enqueue from here, and monolith/engine
	// mode NEVER writes a request file.
	if args.Async && SplitModeEnabled() {
		if _, err := requests.Write(requestsDirForRepo(args.RepoPath), requests.Record{
			Kind:     requests.KindReindex,
			RepoPath: args.RepoPath,
		}); err != nil {
			return fmt.Errorf("queue reindex request: %w", err)
		}
		reply.RepoPath = args.RepoPath
		return nil
	}
	if args.Async && s.scheduler != nil {
		s.scheduler.Enqueue(args.RepoPath)
		reply.RepoPath = args.RepoPath
		return nil
	}

	atomic.AddInt64(&s.inFlight, 1)
	defer atomic.AddInt64(&s.inFlight, -1)
	graphPath, stats, err := s.index(*args)
	if s.scheduler != nil {
		s.scheduler.MarkIndexed(args.RepoPath, err)
	}
	if err != nil {
		return err
	}
	reply.RepoPath = args.RepoPath
	reply.GraphPath = graphPath
	reply.StatsJSON = stats
	return nil
}

// Rebuild force-indexes every repo in a group (or one slug). Wipes
// .grafel/ first when args.Wipe is true. Cross-repo link passes
// run inside RebuildFunc so the daemon does not need to know the
// graph package.
//
// When args.ProgressToken is non-empty, per-repo progress is stored
// so the CLI can poll it via IndexProgress while this call blocks.
//
// Concurrency guard (#2097 + #5681): each group gets a per-group
// capacity-1 semaphore (groupRebuildMu) so only one Rebuild RPC per
// group runs at a time. A second concurrent RPC for the same group
// waits until the first finishes — it does NOT race against the same
// output files, and (the #5681 fix) it does NOT start a second
// concurrent in-process rebuild when the first RPC times out: the guard
// is held by the worker goroutine until the heap-heavy rebuild really
// completes, so overlapping runs can never pile multiple multi-GB
// documents into the engine at once. Combined with a per-RPC wall-clock
// timeout (rebuildRPCTimeout) this prevents the indefinite hang observed
// when in_flight=4 and all four RPCs targeted the same group.
func (s *Service) Rebuild(args *proto.RebuildArgs, reply *proto.RebuildReply) error {
	if s.rebuild == nil {
		return errors.New("rebuild entrypoint not configured")
	}
	if args == nil || args.Group == "" {
		return errors.New("group is required")
	}

	// #5937 — run-scoped terminal invalidation. Service.Rebuild is the SINGLE
	// choke point every rebuild trigger passes through in THIS process
	// (whichever mode): in split mode it enqueues the KindRebuild request
	// below and returns, in monolith/engine mode it calls s.rebuild(*args)
	// directly further down — either way this is the earliest point at which
	// serve becomes aware a NEW run for args.Group is starting. Clear the
	// broker's retained terminal for this group HERE, before any new-run
	// event can be published, so a client that connects/reconnects to the SSE
	// stream mid-run never sees a PRIOR run's terminal replayed at it
	// (handlers_progress.go's emitTerminalIfReady has no way to distinguish
	// "stale corpse from an old run" from "legitimate late reconnect" by
	// timestamp alone — see that file's comment). Once THIS run's own
	// terminal is published (live, in monolith; or via the sidecar tailer, in
	// split), it repopulates the map with a value that is unambiguously
	// current until the NEXT Rebuild call clears it again.
	if s.progressBroker != nil {
		s.progressBroker.ClearTerminal(args.Group)
	}

	// ADR-0024 PR6 prerequisite (epic #5729): in split mode this RPC is
	// answered by the SERVE process. Unlike Service.Index (whose split-mode
	// fast path is gated by s.scheduler being nil there), s.rebuild here is
	// assigned unconditionally by newService regardless of plane (see
	// server.go) — so it is NON-NIL in split-mode serve, and the nil check
	// above never trips. Falling through to the synchronous s.rebuild(*args)
	// call below would run a FULL GROUP REBUILD IN THE SERVE PROCESS,
	// exactly the blast-radius coupling the split exists to remove. So when
	// SplitModeEnabled(), drop a KindRebuild request (the full RebuildArgs,
	// JSON-encoded, as Payload — see requests.KindRebuild) into a
	// group-scoped requests dir instead, and return immediately; the
	// engine's drain loop (requests_drain.go's applyRequest) picks it up and
	// calls the SAME RebuildFunc this method calls directly below in
	// monolith/engine mode. The two paths are mutually exclusive by
	// construction: split mode NEVER reaches the s.rebuild(*args) call
	// below, and monolith/engine mode NEVER writes a request file.
	//
	// Fire-and-forget by default, mirroring Index's async fast path: the reply
	// carries no repo/stat/progress data because the rebuild hasn't happened yet
	// by the time this RPC returns. In particular args.ProgressToken-based
	// polling (IndexProgress) is NOT bridged across the process boundary —
	// s.progress lives in serve's memory, but the rebuild itself runs in the
	// engine process, so nothing here would ever mark that session done.
	//
	// WaitForCompletion (#5790) opts INTO a synchronous wait for callers that
	// treat err==nil as "the rebuild ran" (e.g. `grafel group add --index`,
	// which reports "indexed": true). When set, we stamp a scoping token onto
	// the request, enqueue it, then block on awaitRebuildCompletion until the
	// engine drains+acks THAT request (the same request-ack signal the wizard
	// polls via RebuildRequestPending), returning nil only on real completion
	// and a clear error on engine-death / never-alive / timeout. Callers that
	// leave it false keep the immediate fire-and-forget return.
	if SplitModeEnabled() {
		if args.WaitForCompletion && args.ProgressToken == "" {
			// Scope the completion poll to OUR request so a concurrent rebuild
			// of the same group (a different token) is never mistaken for ours.
			args.ProgressToken = uuid.NewString()
		}
		payload, err := json.Marshal(*args)
		if err != nil {
			return fmt.Errorf("encode rebuild request: %w", err)
		}
		dir := requestsDirForGroup(args.Group)
		id, err := requests.Write(dir, requests.Record{
			Kind:    requests.KindRebuild,
			Payload: payload,
		})
		if err != nil {
			return fmt.Errorf("queue rebuild request: %w", err)
		}
		if !args.WaitForCompletion {
			return nil
		}
		// Block until the engine finishes OUR rebuild and read its terminal ack
		// (bounded + failure-aware; see awaitRebuildCompletion). The request is
		// already on disk, so the ack read is race-free, and the engine KEEPS the
		// ack for a WaitForCompletion request so its OK/error outcome is readable.
		return s.awaitRebuildCompletion(dir, id)
	}

	atomic.AddInt64(&s.inFlight, 1)
	defer atomic.AddInt64(&s.inFlight, -1)

	// Per-group single-flight (#2097 + #5681). Load-or-store a capacity-1
	// semaphore for this group so concurrent Rebuild RPCs targeting the same
	// group are serialised rather than racing on the same output files AND,
	// crucially for the #5681 engine-RSS blow-up, so at most ONE in-process
	// group rebuild is alive at a time even across the per-RPC timeout below.
	//
	// Acquisition is timeout-aware: if a prior same-group rebuild still holds
	// the guard (including an RPC-timed-out orphan whose heap-heavy goroutine is
	// still running), this RPC waits up to rebuildRPCTimeout and then returns a
	// timeout — it does NOT start a second concurrent rebuild. The guard is
	// released from the WORKER goroutine on the rebuild's real completion (see
	// releaseGuard below), never from this handler's timeout path, so N
	// re-triggered rebuilds can never coexist and pile up N multi-GB docs.
	semVal, _ := s.groupRebuildMu.LoadOrStore(args.Group, make(chan struct{}, 1))
	groupSem := semVal.(chan struct{})
	acqTimer := time.NewTimer(rebuildRPCTimeout)
	select {
	case groupSem <- struct{}{}:
		acqTimer.Stop()
	case <-acqTimer.C:
		return fmt.Errorf("rebuild group=%s timed out after %s waiting for an in-flight rebuild of the same group to finish", args.Group, rebuildRPCTimeout)
	}
	atomic.AddInt64(&s.groupsActiveCount, 1)
	atomic.AddInt64(&s.rebuildInFlight, 1)
	// releaseGuard releases the per-group semaphore + decrements the counters
	// exactly once. It is called from the worker goroutine on real completion
	// (normal or error), NOT from the RPC-timeout return path, so an orphaned
	// rebuild keeps the guard until its heap is actually freed.
	var releaseOnce sync.Once
	releaseGuard := func() {
		releaseOnce.Do(func() {
			atomic.AddInt64(&s.groupsActiveCount, -1)
			atomic.AddInt64(&s.rebuildInFlight, -1)
			<-groupSem
		})
	}

	if s.logger != nil {
		s.logger.Info("rebuild: start",
			"group", args.Group,
			"token", args.ProgressToken,
			"wipe", args.Wipe,
			"incremental", args.Incremental,
			"slug", args.Slug,
		)
	}

	// Per-RPC timeout: bound the maximum wall-clock time so a stalled
	// indexer cannot wedge the daemon indefinitely (#2097).
	type rebuildResult struct {
		repos   []string
		warning string
		err     error
	}
	resultCh := make(chan rebuildResult, 1)

	var (
		sess  *rebuildSession
		token string
	)
	if args.ProgressToken != "" {
		token = args.ProgressToken
		sess = s.newProgressSession(token, args.Group)
	}

	rebuildStartTime := time.Now()
	go func() {
		// Release the per-group guard on REAL completion, even if this RPC
		// handler already returned on timeout (#5681): the orphaned rebuild
		// holds the guard until here so no superseding same-group rebuild can
		// run concurrently and double the engine heap.
		defer releaseGuard()
		var repos []string
		var warning string
		var err error
		if sess != nil {
			repos, warning, err = s.rebuildWithProgress(sess, *args)
		} else {
			repos, warning, err = s.rebuild(*args)
		}
		resultCh <- rebuildResult{repos: repos, warning: warning, err: err}
	}()

	// Dead-man heartbeat: if no result arrives within deadManInterval, log a
	// warning so operators can detect a stalled rebuild without waiting for the
	// full 2-hour timeout (#2097). When it fires we also capture a full
	// goroutine dump so the NEXT stall is diagnosable straight from the daemon
	// log instead of needing a live `sample`/SIGQUIT against the process (#5326).
	//
	// The ticker goroutine is torn down via stopDeadMan when the result lands.
	// Previously it ran `for range deadMan.C` with no exit path: time.Ticker.Stop
	// does NOT close the channel, so the goroutine blocked forever and leaked one
	// goroutine per Rebuild RPC (#5326). It now selects on a stop channel so it
	// exits as soon as the rebuild completes.
	deadManInterval := resolveStallWarnInterval()
	deadMan := time.NewTicker(deadManInterval)
	stopDeadMan := make(chan struct{})
	var deadManDone sync.WaitGroup
	deadManDone.Add(1)
	go func() {
		defer deadManDone.Done()
		fired := 0
		for {
			select {
			case <-stopDeadMan:
				return
			case <-deadMan.C:
				fired++
				if s.logger != nil {
					s.logger.Warn("rebuild: possible stall — no result yet",
						"group", args.Group,
						"elapsed", time.Since(rebuildStartTime).Truncate(time.Second).String(),
					)
					// Rate-limit the (potentially large) goroutine dump: emit it
					// on the first stall warning only, so a genuinely wedged
					// rebuild logs the stack once rather than every interval.
					if fired == 1 {
						s.logger.Warn("rebuild: goroutine dump (stall diagnostic)",
							"group", args.Group,
							"goroutines", runtime.NumGoroutine(),
							"stack", captureGoroutineDump(),
						)
					}
				}
			}
		}
	}()
	defer func() {
		deadMan.Stop()
		close(stopDeadMan)
		deadManDone.Wait()
	}()

	timer := time.NewTimer(rebuildRPCTimeout)
	defer timer.Stop()

	var res rebuildResult
	select {
	case res = <-resultCh:
		// Normal completion.
	case <-timer.C:
		if s.logger != nil {
			s.logger.Warn("rebuild: RPC timeout — unblocked; background index may still be running",
				"group", args.Group,
				"timeout", rebuildRPCTimeout.String(),
			)
		}
		if sess != nil {
			sess.mu.Lock()
			sess.done = true
			for i := range sess.repos {
				if sess.repos[i].Phase != proto.PhaseCompleted {
					sess.repos[i].Phase = proto.PhaseFailed
					sess.repos[i].ErrMsg = "rebuild RPC timeout"
					sess.repos[i].UpdatedAt = time.Now().Unix()
				}
			}
			sess.mu.Unlock()
		}
		return fmt.Errorf("rebuild group=%s timed out after %s", args.Group, rebuildRPCTimeout)
	}

	if sess != nil {
		// Mark the session done so the final poll returns Done=true.
		sess.mu.Lock()
		sess.done = true
		sess.mu.Unlock()
	}

	if s.logger != nil {
		if res.err != nil {
			s.logger.Error("rebuild: error", "group", args.Group, "err", res.err)
		} else {
			s.logger.Info("rebuild: done", "group", args.Group, "repos", len(res.repos), "warning", res.warning)
		}
	}

	if res.err != nil {
		return res.err
	}
	reply.Repos = res.repos
	reply.Warning = res.warning
	if sess != nil {
		reply.TotalEntities = sess.totalEntities
		reply.TotalRels = sess.totalRels
		elapsedSec := time.Since(sess.startedAt).Seconds()
		reply.ElapsedSec = elapsedSec

		// Record index wall-time for the performance budget monitor (#1319).
		// Best-effort: do not fail the rebuild if recording fails.
		go func() {
			homeDir, _ := registry.HomeDir()
			if homeDir != "" {
				rec := perf.NewRecorder(homeDir + "/perf-history.jsonl")
				_ = rec.Record("index_wall_ms", args.Group, elapsedSec*1000)
			}
		}()
	}

	return nil
}

// newProgressSession creates and registers a new rebuild session for the
// given token. The session is retained in s.progress for polling; expired
// sessions are evicted lazily when a new token arrives.
func (s *Service) newProgressSession(token, group string) *rebuildSession {
	sess := &rebuildSession{
		startedAt: time.Now(),
		group:     group,
	}
	s.progressMu.Lock()
	// Evict sessions older than 10 minutes to bound memory usage.
	for k, v := range s.progress {
		v.mu.RLock()
		elapsed := time.Since(v.startedAt)
		done := v.done
		v.mu.RUnlock()
		if done && elapsed > 10*time.Minute {
			delete(s.progress, k)
		}
	}
	s.progress[token] = sess
	s.progressMu.Unlock()
	return sess
}

// rebuildWithProgress calls RebuildFunc but instruments it with per-repo
// progress events by pre-seeding the session with queued states and
// updating them as repos complete.
//
// The existing RebuildFunc signature does not expose per-repo callbacks,
// so we model progress at the batch level: we first query the group's
// repos to seed the session, then run the full rebuild, then mark
// individual repos completed as the reply lands.
//
// For finer-grained within-repo progress (walk/extract phases), the
// daemon emits periodic heartbeat updates via a background ticker while
// the rebuild is running.
func (s *Service) rebuildWithProgress(sess *rebuildSession, args proto.RebuildArgs) ([]string, string, error) {
	// Seed the session with queued states. We don't know the exact list
	// of repos until RebuildFunc runs, so we put a single placeholder
	// and replace it once the rebuild returns.
	sess.mu.Lock()
	sess.repos = []proto.RepoProgressState{
		{
			Slug:      args.Group,
			Path:      args.Group,
			Phase:     proto.PhaseStarted,
			Index:     1,
			Total:     1,
			UpdatedAt: time.Now().Unix(),
		},
	}
	sess.mu.Unlock()

	repos, warning, err := s.rebuild(args)
	if err != nil {
		// Mark as failed.
		sess.mu.Lock()
		now := time.Now().Unix()
		for i := range sess.repos {
			if sess.repos[i].Phase != proto.PhaseCompleted {
				sess.repos[i].Phase = proto.PhaseFailed
				sess.repos[i].ErrMsg = err.Error()
				sess.repos[i].UpdatedAt = now
			}
		}
		sess.mu.Unlock()
		return nil, warning, err
	}

	// Rebuild succeeded — update the session with real per-repo info.
	sess.mu.Lock()
	now := time.Now().Unix()
	elapsed := time.Since(sess.startedAt).Seconds()
	newStates := make([]proto.RepoProgressState, 0, len(repos))
	for i, r := range repos {
		slug := filepath.Base(r)
		newStates = append(newStates, proto.RepoProgressState{
			Slug:       slug,
			Path:       r,
			Phase:      proto.PhaseCompleted,
			Index:      i + 1,
			Total:      len(repos),
			ElapsedSec: elapsed / float64(len(repos)), // rough per-repo share
			UpdatedAt:  now,
		})
	}
	sess.repos = newStates
	sess.mu.Unlock()

	// Populate the batch entity/relationship totals from the freshly written
	// per-repo graph-stats.json sidecars (#5326). Without this the rebuild
	// reply (and therefore the dashboard "rebuilt N repo(s)" toast) reported
	// "0 entities, 0 relationships" for a fully populated graph because
	// addEntities was never called on this path. We read the sidecar — the same
	// cheap, mmap-free source the CLI rebuild summary uses — rather than the
	// per-repo delta so the reply carries the graph's real TOTAL, which is what
	// the toast wants.
	for _, repoPath := range repos {
		ents, rels := readGraphStatsSidecar(StateDirForRepo(repoPath))
		if ents > 0 || rels > 0 {
			sess.addEntities(ents, rels)
		}
	}

	return repos, warning, nil
}

// readGraphStatsSidecar reads <stateDir>/graph-stats.json and returns the
// entity/relationship totals the indexer wrote alongside graph.fb. It decodes
// only the two fields it needs so the daemon does not have to import the graph
// package (and mmap the full document). Returns (0, 0) on any read/parse error
// so callers always get a safe value (#5326).
func readGraphStatsSidecar(stateDir string) (entities, rels int64) {
	data, err := os.ReadFile(filepath.Join(stateDir, "graph-stats.json"))
	if err != nil {
		return 0, 0
	}
	var st struct {
		TotalEntities      int64 `json:"total_entities"`
		TotalRelationships int64 `json:"total_relationships"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return 0, 0
	}
	return st.TotalEntities, st.TotalRelationships
}

// IndexProgress handles a CLI poll for in-flight rebuild progress.
func (s *Service) IndexProgress(args *proto.IndexProgressArgs, reply *proto.IndexProgressReply) error {
	if args == nil || args.Token == "" {
		return errors.New("token is required")
	}
	s.progressMu.RLock()
	sess, ok := s.progress[args.Token]
	s.progressMu.RUnlock()
	if !ok {
		// Session not found — either expired or the token is wrong.
		// Return Done=true so the CLI doesn't loop forever.
		reply.Token = args.Token
		reply.Done = true
		return nil
	}
	snap := sess.snapshot()
	snap.Token = args.Token
	*reply = snap
	return nil
}

// Stop initiates a graceful shutdown. The first call closes stopReq
// (signalling the server loop); later calls are no-ops. Returning
// immediately lets the client get a clean reply before the socket
// closes; the server drains in-flight work and exits.
func (s *Service) Stop(_ *proto.StopArgs, _ *proto.StopReply) error {
	if atomic.CompareAndSwapInt32(&s.stopped, 0, 1) {
		close(s.stopReq)
	}
	return nil
}

// RemoveRepo unregisters a single repo from a group: stops the watcher,
// removes the git hook block, optionally deletes the per-repo cache, and
// persists the updated fleet config. It does not contact the registry
// directly — fleet persistence is handled via install.Uninstall so all
// teardown logic lives in one place.
func (s *Service) RemoveRepo(args *proto.RemoveRepoArgs, reply *proto.RemoveRepoReply) error {
	if args == nil || args.Group == "" || args.Slug == "" {
		return errors.New("group and slug are required")
	}

	groups, err := registry.Groups()
	if err != nil {
		return err
	}
	var ref *registry.GroupRef
	for i := range groups {
		if groups[i].Name == args.Group {
			ref = &groups[i]
			break
		}
	}
	if ref == nil {
		return fmt.Errorf("unknown group: %s", args.Group)
	}
	cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
	if err != nil {
		return err
	}

	// Find the repo.
	var target *registry.Repo
	for i := range cfg.Repos {
		if cfg.Repos[i].Slug == args.Slug {
			target = &cfg.Repos[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("repo %q not found in group %s", args.Slug, args.Group)
	}
	repoPath := target.Path
	reply.RepoPath = repoPath

	// Stop watcher for this repo.
	if s.watcher != nil {
		s.watcher.RemoveRepo(repoPath)
	}

	// Tear down the OS-level watcher launchd/systemd/schtasks unit + plist so a
	// later recreate does not fight stale state (#5338). Idempotent.
	watchers.Cleanup(args.Group, repoPath, "")

	// Remove git hooks.
	if cfg.Features.GitHooks {
		_ = hooks.Uninstall(repoPath)
	}

	// Optionally delete the per-repo cache.
	if !args.KeepCache {
		cacheDir := StateDirForRepo(repoPath)
		if info, err := os.Stat(cacheDir); err == nil {
			freed, _ := dirSize(cacheDir)
			reply.FreedBytes = freed
			if err := os.RemoveAll(cacheDir); err != nil && s.logger != nil {
				s.logger.Error("remove-repo: delete cache", "dir", cacheDir, "err", err)
			}
			_ = info
		}
	}

	// Remove the repo entry from the fleet config.
	kept := cfg.Repos[:0]
	for _, r := range cfg.Repos {
		if r.Slug != args.Slug {
			kept = append(kept, r)
		}
	}
	cfg.Repos = kept
	if err := registry.SaveGroupConfig(ref.ConfigPath, cfg); err != nil {
		return fmt.Errorf("persist fleet: %w", err)
	}

	return nil
}

// cancelGroupWork signals cancellation of a group's in-flight background work
// (scheduler enrichment passes + an in-flight group rebuild) so a delete stops
// the CPU burn promptly instead of letting an 11-minute pass run to completion.
// Best-effort and non-blocking — it never awaits the cancelled work, so
// DeleteGroup cannot deadlock behind it.
func (s *Service) cancelGroupWork(group string) {
	// In-process path (monolith / engine): cancel directly. Both are no-ops when
	// nothing is in flight for the group.
	if s.scheduler != nil {
		s.scheduler.CancelGroup(group)
	}
	CancelGroupRebuild(group)

	// Split-mode path: the enrichment/rebuild goroutines live in the engine
	// process, unreachable from this serve process. Drop a KindCancelGroup
	// request the engine's drain loop applies (Scheduler.CancelGroup +
	// CancelGroupRebuild on the engine side). The group name travels in RepoPath
	// (see requests.KindCancelGroup). Best-effort: a write failure is logged but
	// never fails the delete.
	if SplitModeEnabled() {
		if _, err := requests.Write(requestsDirForGroup(group), requests.Record{
			Kind:     requests.KindCancelGroup,
			RepoPath: group,
		}); err != nil && s.logger != nil {
			s.logger.Warn("delete-group: queue cancel request failed", "group", group, "err", err)
		}
	}
}

// DeleteGroup tears down every repo in a group and removes the group from
// the registry. Mirrors RemoveRepo for each member repo, then deletes the
// fleet config file and per-group state directory.
func (s *Service) DeleteGroup(args *proto.DeleteGroupArgs, reply *proto.DeleteGroupReply) error {
	if args == nil || args.Group == "" {
		return errors.New("group is required")
	}

	groups, err := registry.Groups()
	if err != nil {
		return err
	}
	var ref *registry.GroupRef
	for i := range groups {
		if groups[i].Name == args.Group {
			ref = &groups[i]
			break
		}
	}
	if ref == nil {
		return fmt.Errorf("unknown group: %s", args.Group)
	}

	// Cancel the group's IN-FLIGHT background work BEFORE tearing down its state
	// (v0.1.8 leak fix): a delete removed the group from the registry but left
	// its in-flight (re)index + enrichment goroutines — betweenness, phantom-edge
	// / link passes, and the multi-minute group rebuild — burning CPU to
	// completion after the group was gone. Cancellation is scoped to THIS group
	// (the scheduler leaves a shared repo's reindex running if a surviving group
	// still references it; other groups' passes use different map keys), and it
	// never blocks: it signals cancel funcs and returns.
	//
	//   - In-process (monolith / engine-in-same-process): the scheduler is
	//     attached to this Service, so cancel its group-algo/link/reindex passes
	//     directly, plus any in-process group rebuild via the package registry.
	//   - Split mode: this RPC runs in the SERVE process, but the enrichment /
	//     rebuild goroutines live in the ENGINE process, so serve cannot reach
	//     them in-process (s.scheduler is nil there). Drop a KindCancelGroup
	//     request; the engine's drain loop invokes the same two cancels.
	s.cancelGroupWork(args.Group)

	cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if cfg != nil {
		// #34 guard: a repo's underlying store, in-repo manifest, and status
		// sidecar may be SHARED — referenced by another still-registered group.
		// Compute the set of repo paths every OTHER group references so per-repo
		// state is only torn down when NO surviving group still needs it.
		sharedPaths := otherGroupRepoPaths(args.Group)

		for _, r := range cfg.Repos {
			// Stop watcher.
			if s.watcher != nil {
				s.watcher.RemoveRepo(r.Path)
			}
			// Tear down the OS-level watcher unit + plist so a later recreate
			// of this group does not fight stale launchd state (#5338).
			watchers.Cleanup(args.Group, r.Path, "")
			// Remove git hooks.
			if cfg.Features.GitHooks {
				_ = hooks.Uninstall(r.Path)
			}

			shared := sharedPaths[canonRepoKey(r.Path)]

			// Delete per-repo cache (the repo store: graph.fb, links, …). Never
			// when another group still references this repo — that would strip a
			// live group's graph out from under it (#34).
			if !args.KeepCaches && !shared {
				cacheDir := StateDirForRepo(r.Path)
				if _, err := os.Stat(cacheDir); err == nil {
					freed, _ := dirSize(cacheDir)
					reply.FreedBytes += freed
					_ = os.RemoveAll(cacheDir)
				}
			}

			// Remove the per-repo orphaned state that outlives the group in the
			// registry (Bug 1): the committed in-repo manifest
			// <repo>/.grafel/group.json (the source of the wizard's stale
			// "already in group" note) and the engine status-plane sidecar
			// ~/.grafel/status/<hash>.json. Same #34 guard: only when no other
			// group still references this repo.
			if !shared {
				manifest := filepath.Join(r.Path, ".grafel", "group.json")
				if _, err := os.Stat(manifest); err == nil {
					_ = os.Remove(manifest)
					// Best-effort: drop the now-likely-empty .grafel dir. Fails
					// harmlessly (and is skipped) if anything else lives there.
					_ = os.Remove(filepath.Join(r.Path, ".grafel"))
				}
				if sp, err := statusfile.PathFor(r.Path); err == nil {
					_ = os.Remove(sp)
				}
			}

			reply.RemovedRepos = append(reply.RemovedRepos, r.Slug)
		}
	}

	// Remove the group from the registry.
	if err := registry.RemoveGroup(args.Group); err != nil {
		return fmt.Errorf("remove group from registry: %w", err)
	}

	// Delete the fleet config file.
	_ = os.Remove(ref.ConfigPath)

	// Delete per-group state directory.
	stateDir, err := registry.StateDirFor(args.Group)
	if err == nil {
		_ = os.RemoveAll(stateDir)
	}

	// Remove the GROUP-LEVEL artifacts keyed by the deleted group name. These
	// are always safe to remove (they belong to this group alone, never shared
	// with another): the link/algo sidecars under ~/.grafel/groups/<group>-*.json
	// and the group store dir(s) ~/.grafel/store/group-<group>-*.
	reply.FreedBytes += removeGroupArtifacts(args.Group)

	return nil
}

// canonRepoKey normalises a repo path for equality comparison (absolute +
// lexically clean) so the #34 shared-repo guard matches the same repo referenced
// by two groups regardless of how each config spelled the path.
func canonRepoKey(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	return filepath.Clean(abs)
}

// otherGroupRepoPaths returns the set of repo paths (canonRepoKey-normalised)
// referenced by every registered group EXCEPT exclude. DeleteGroup consults it
// so a repo shared with a surviving group keeps its store/manifest/status (#34).
func otherGroupRepoPaths(exclude string) map[string]bool {
	out := map[string]bool{}
	groups, err := registry.Groups()
	if err != nil {
		return out
	}
	for _, g := range groups {
		if g.Name == exclude {
			continue
		}
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil || cfg == nil {
			continue
		}
		for _, r := range cfg.Repos {
			out[canonRepoKey(r.Path)] = true
		}
	}
	return out
}

// removeGroupArtifacts deletes the group-level (not per-repo) state keyed by a
// group name: the ~/.grafel/groups/<group>-*.json link/algo sidecars and the
// ~/.grafel/store/group-<group>-* group store dir(s). Returns bytes freed.
//
// The globs are prefix-based and a group name can be a hyphen-prefix of a
// SURVIVING group's name (delete "api" while "api-v2" survives): "api-*.json"
// also matches "api-v2-links.json", and "group-api-*" also matches
// "group-api-v2-<hash>". So each candidate match is attributed to the LONGEST
// registered group-name prefix that owns it, and a match owned by a surviving
// group is left intact — the group-level analog of the per-repo
// otherGroupRepoPaths survivor-exclusion guard (#34).
func removeGroupArtifacts(group string) int64 {
	// Owner-candidate names: the deleted group PLUS every still-registered group
	// (survivors — RemoveGroup already ran, so registry.Groups() excludes the
	// deleted one). Longest-prefix attribution over this set distinguishes
	// "api"'s artifacts from "api-v2"'s.
	names := []string{group}
	if groups, err := registry.Groups(); err == nil {
		for _, g := range groups {
			if g.Name != group {
				names = append(names, g.Name)
			}
		}
	}
	// ownerIsDeleted reports whether subj (a match's basename minus its fixed
	// prefix) is owned by the group being deleted: the deleted group is the
	// LONGEST candidate name N for which subj == N or subj starts with N+"-".
	ownerIsDeleted := func(subj string) bool {
		owner := ""
		for _, n := range names {
			if subj == n || strings.HasPrefix(subj, n+"-") {
				if len(n) > len(owner) {
					owner = n
				}
			}
		}
		return owner == group
	}

	var freed int64
	remove := func(m string) {
		if sz, err := dirSize(m); err == nil {
			freed += sz
		}
		_ = os.RemoveAll(m)
	}

	// groups/<group>-*.json — subject is the basename (e.g. "api-v2-links.json").
	if home, err := registry.HomeDir(); err == nil {
		matches, _ := filepath.Glob(filepath.Join(home, "groups", group+"-*.json"))
		for _, m := range matches {
			if ownerIsDeleted(filepath.Base(m)) {
				remove(m)
			}
		}
	}
	// store/group-<group>-* — subject is the basename with the "group-" prefix
	// stripped (e.g. "api-v2-<hash>").
	matches, _ := filepath.Glob(filepath.Join(StoreDir(), "group-"+group+"-*"))
	for _, m := range matches {
		if ownerIsDeleted(strings.TrimPrefix(filepath.Base(m), "group-")) {
			remove(m)
		}
	}
	return freed
}

// dirSize returns the total number of bytes in a directory tree.
func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.Walk(dir, func(_ string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !fi.IsDir() {
			total += fi.Size()
		}
		return nil
	})
	return total, err
}

// QualityAudit runs the audit-orphans analysis for a repo or corpus
// directory and returns the pre-formatted report. The heavy audit
// package lives in cmd/grafel; it is injected via QualityAuditFunc.
func (s *Service) QualityAudit(args *proto.QualityAuditRequest, reply *proto.QualityAuditReply) error {
	if s.qualityAudit == nil {
		return errors.New("quality audit entrypoint not configured")
	}
	if args == nil || args.RepoPath == "" {
		return errors.New("repo_path is required")
	}
	atomic.AddInt64(&s.inFlight, 1)
	defer atomic.AddInt64(&s.inFlight, -1)
	r, err := s.qualityAudit(*args)
	if err != nil {
		return err
	}
	*reply = r
	return nil
}
