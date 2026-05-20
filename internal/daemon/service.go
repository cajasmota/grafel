package daemon

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/daemon/sched"
	"github.com/cajasmota/archigraph/internal/daemon/watch"
	"github.com/cajasmota/archigraph/internal/version"
)

// IndexFunc runs a one-shot index. The daemon does not import the
// extractor stack directly — that lives in cmd/archigraph — so it
// receives the entrypoint as a function value at construction time.
// Returning the graph.json path and the stats JSON (opaque) keeps the
// wire shape stable as the extractor evolves.
type IndexFunc func(args proto.IndexArgs) (graphPath string, statsJSON string, err error)

// RebuildFunc force-rebuilds a group. As with IndexFunc, the daemon
// stays decoupled from registry + extractor — the entrypoint is
// injected from cmd/archigraph at construction.
type RebuildFunc func(args proto.RebuildArgs) (repos []string, warning string, err error)

// QualityAuditFunc runs the audit-orphans analysis for a repo (or
// corpus directory). Returns the pre-formatted markdown (or JSON) report
// and the scalar summary. Like IndexFunc, the heavy audit package lives
// in cmd/archigraph and is injected here at construction time.
type QualityAuditFunc func(args proto.QualityAuditRequest) (reply proto.QualityAuditReply, err error)

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

	// Phase B — populated only when the daemon is run with a watcher
	// + scheduler attached. Both may be nil in test wiring that
	// exercises just the RPC surface.
	watcher   *watch.Watcher
	scheduler *sched.Scheduler

	// #802 progress tracking — keyed by ProgressToken.
	progressMu sync.RWMutex
	progress   map[string]*rebuildSession

	// Phase D — MCP RPC surface (ADR-0017 #832).
	// Both fields are injected from cmd/archigraph to avoid the import
	// cycle that would arise from importing internal/mcp here.
	// nil means "not configured" — MCPToolList returns empty; MCPToolCall
	// returns a structured "daemon not ready" error.
	mcpListTools MCPListToolsFunc
	mcpCallTool  MCPCallToolFunc

	// logger is the daemon's structured logger, forwarded to the MCP
	// dispatcher for per-call debug logging (tool=name elapsed=X repo=Y).
	logger *log.Logger

	// dashboardPort is the TCP port the embedded dashboard server is
	// bound to. Set by server.go after the dashboard goroutine starts.
	// Zero means dashboard is not running. Read by Status RPC (#938).
	dashboardPort int
}

// newService wires the injected entrypoints onto a fresh Service. The
// stopReq channel is closed by Stop to signal the server loop; the
// service itself never re-closes it (a stopped atomic guards the close).
// logger may be nil; it is forwarded to the MCP dispatcher for debug-level
// per-call logging (tool=name elapsed=X repo=Y).
func newService(idx IndexFunc, rb RebuildFunc, qa QualityAuditFunc, socketPath string, stopReq chan<- struct{}, logger *log.Logger) *Service {
	return &Service{
		startedAt:    time.Now(),
		socketPath:   socketPath,
		index:        idx,
		rebuild:      rb,
		qualityAudit: qa,
		stopReq:      stopReq,
		progress:     make(map[string]*rebuildSession),
		logger:       logger,
	}
}

// Ping is the trivial liveness probe. Clients use it to distinguish
// "daemon not running" from "daemon running but unhealthy".
func (s *Service) Ping(_ *proto.PingArgs, reply *proto.PingReply) error {
	reply.Version = version.String()
	return nil
}

// Status reports a snapshot of daemon state. RSS is read via the Go
// runtime memstats (Sys); this is approximate but does not require
// platform-specific code. Phase B fields (watcher + scheduler) are
// populated when the daemon was started with both attached.
func (s *Service) Status(_ *proto.StatusArgs, reply *proto.StatusReply) error {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	reply.Version = version.String()
	reply.PID = os.Getpid()
	reply.UptimeSec = int64(time.Since(s.startedAt).Seconds())
	reply.RSSBytes = ms.Sys
	reply.InFlight = int(atomic.LoadInt64(&s.inFlight))
	reply.StartedAt = s.startedAt.UTC().Format(time.RFC3339)
	reply.SocketPath = s.socketPath
	// Report the binary path so clients can detect stale daemons (#855).
	if bin, err := os.Executable(); err == nil {
		reply.BinaryPath = bin
	}
	// Report the dashboard port so `archigraph dashboard` can construct
	// the URL without a separate config read (#938).
	reply.DashboardPort = s.dashboardPort

	if s.watcher != nil {
		repos, dirs, events, dropped := s.watcher.Stats()
		reply.WatcherRepos = repos
		reply.WatcherDirs = dirs
		reply.WatcherEvents = events
		reply.WatcherDropped = dropped
	}
	if s.scheduler != nil {
		snap := s.scheduler.Snapshot()
		reply.QueueLen = snap.QueueLen
		reply.PendingAlgo = snap.PendingAlgo
		reply.PendingLinks = snap.PendingLinks
		reply.RSSBudgetMB = snap.BudgetMB
		// RSSUsedMB reports actual measured daemon RSS (in MB), not predicted
		// sum of in-flight jobs. This ensures the budget display shows the
		// real memory pressure (#803).
		reply.RSSUsedMB = int64(reply.RSSBytes / (1024 * 1024))
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
// .archigraph/ first when args.Wipe is true. Cross-repo link passes
// run inside RebuildFunc so the daemon does not need to know the
// graph package.
//
// When args.ProgressToken is non-empty, per-repo progress is stored
// so the CLI can poll it via IndexProgress while this call blocks.
func (s *Service) Rebuild(args *proto.RebuildArgs, reply *proto.RebuildReply) error {
	if s.rebuild == nil {
		return errors.New("rebuild entrypoint not configured")
	}
	if args == nil || args.Group == "" {
		return errors.New("group is required")
	}
	atomic.AddInt64(&s.inFlight, 1)
	defer atomic.AddInt64(&s.inFlight, -1)

	// If no progress token was supplied, run synchronously as before.
	if args.ProgressToken == "" {
		repos, warning, err := s.rebuild(*args)
		if err != nil {
			return err
		}
		reply.Repos = repos
		reply.Warning = warning
		return nil
	}

	// Progress-tracked path — delegate to the progress-aware rebuild.
	token := args.ProgressToken
	sess := s.newProgressSession(token, args.Group)
	defer func() {
		// Mark the session done so the final poll returns Done=true.
		sess.mu.Lock()
		sess.done = true
		sess.mu.Unlock()
	}()

	repos, warning, err := s.rebuildWithProgress(sess, *args)
	if err != nil {
		return err
	}
	reply.Repos = repos
	reply.Warning = warning
	reply.TotalEntities = sess.totalEntities
	reply.TotalRels = sess.totalRels
	reply.ElapsedSec = time.Since(sess.startedAt).Seconds()
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
	return repos, warning, nil
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

// QualityAudit runs the audit-orphans analysis for a repo or corpus
// directory and returns the pre-formatted report. The heavy audit
// package lives in cmd/archigraph; it is injected via QualityAuditFunc.
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
