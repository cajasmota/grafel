package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/cajasmota/archigraph/internal/agentpatterns"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/daemon/sched"
	"github.com/cajasmota/archigraph/internal/daemon/transport"
	"github.com/cajasmota/archigraph/internal/daemon/watch"
	"github.com/cajasmota/archigraph/internal/gitmeta"
)

// Config configures Run. Fields are required unless documented otherwise.
type Config struct {
	Layout       Layout           // on-disk paths (see DefaultLayout)
	Index        IndexFunc        // injected from cmd/archigraph
	Rebuild      RebuildFunc      // injected from cmd/archigraph
	QualityAudit QualityAuditFunc // injected from cmd/archigraph (Phase E)
	Logger       *log.Logger      // optional; defaults to stderr

	// Phase B optional wiring. When all four are non-nil the daemon
	// starts the fsnotify watcher + scheduler and registers every
	// repo returned by ReposToWatch. The Index field above remains
	// the synchronous RPC entrypoint; the scheduler uses
	// SchedulerIndex for fast (algo-skipped) reactive reindexes.
	ReposToWatch   func() []string                                       // repos to subscribe at startup
	GroupsForRepo  func(repoPath string) []string                        // for cross-repo link debounce
	SchedulerIndex func(ctx context.Context, repo string, ref string) error // fast reindex (skip algo pass); ref is the git branch captured at enqueue time
	SchedulerLinks func(ctx context.Context, group string) error
	SchedulerAlgo  func(ctx context.Context, repo string) error

	// MaxRSSBudgetMB caps the total predicted RSS of concurrently
	// running index jobs. 0 disables admission control (legacy
	// behaviour). The CLI sets this via --max-rss-budget on the
	// daemon subcommand or the ARCHIGRAPH_MAX_RSS_BUDGET_MB env var.
	MaxRSSBudgetMB int64

	// RSSHistoryPath is where the scheduler persists per-repo observed
	// peak RSS for predictor calibration. Empty disables history.
	RSSHistoryPath string

	// PatternDecayInterval controls how often the confidence time-decay
	// pass runs. Default (zero value) → 6 hours. Set to a shorter interval
	// for testing.
	PatternDecayInterval time.Duration

	// PatternGroupDirs is a function that returns a map of group-name →
	// patterns directory (the dir that contains patterns.json). When nil,
	// the decay scheduler is not started. Populated by cmd/archigraph.
	PatternGroupDirs func() map[string]string

	// Phase D — MCP RPC surface (ADR-0017 #832).
	// Both fields are optional; when nil, MCPToolList returns an empty
	// catalog and MCPToolCall returns a "not configured" error block.
	// Injected from cmd/archigraph (which imports internal/mcp) to avoid
	// the import cycle that would arise from importing internal/mcp here.
	MCPListTools MCPListToolsFunc
	MCPCallTool  MCPCallToolFunc

	// DashboardServe is an optional hook that starts the embedded HTTP
	// dashboard alongside the daemon process (#929/#931). When non-nil,
	// Run calls it in a goroutine with the daemon's context so the
	// dashboard shuts down when the daemon shuts down.
	//
	// The hook is injected from cmd/archigraph (which imports both
	// internal/daemon and internal/dashboard). Keeping it here as a
	// function value avoids the import cycle that would arise if
	// internal/daemon imported internal/dashboard directly.
	//
	// The hook receives the bind address and port to listen on, and the
	// daemon logger. It should block until ctx is done.
	DashboardServe func(ctx context.Context, bind string, port int, logger *log.Logger) error

	// DashboardPort is the TCP port for the embedded dashboard HTTP server
	// (#929/#931). When 0 the dashboard is disabled. Default production
	// value is 47274. Configurable via ARCHIGRAPH_DASHBOARD_PORT env or
	// ~/.config/archigraph/daemon.toml.
	DashboardPort int

	// DashboardBind is the bind address for the dashboard TCP listener.
	// Defaults to "127.0.0.1" (loopback-only).
	DashboardBind string

	// WatcherConfig tunes the file watcher. Zero value uses built-in
	// defaults (5 s debounce, 50-event bulk threshold, 30 s heartbeat).
	// Populated from daemon.toml or CLI flags (watcher_debounce_ms,
	// watcher_bulk_threshold). Added in #1270.
	WatcherConfig watch.Config

	// OnWatcherReady is called with the live watcher after it is
	// successfully created and repos are subscribed. Allows callers
	// (e.g. cmd/archigraph) to wire the watcher into the dashboard
	// without creating an import cycle. Added in #1270.
	OnWatcherReady func(w *watch.Watcher)

	// WatcherMgrStats, when non-nil, is queried by the Status RPC to report
	// PH2a watcher pause/resume slot counts. Set by cmd/archigraph after
	// onWatcherReady creates the DefaultManager. PH2a #2096.
	WatcherMgrStats watcherMgrStatsIface

	// MaxConcurrentGroups controls how many groups can be indexed in
	// parallel during a Rebuild RPC (cold start or forced rebuild).
	// 0 or 1 → serial (legacy behaviour). Default when unset: 2.
	// Configurable via --max-concurrent-groups on the daemon subcommand
	// or ARCHIGRAPH_MAX_CONCURRENT_GROUPS env var. Added in #1276.
	MaxConcurrentGroups int

	// DaemonMode is the operational mode the daemon was booted in (S7 #2157).
	// One of "background", "workstation", "readonly". Empty string means
	// the caller did not specify a mode (treated as background).
	// Surfaced in Status RPC so `archigraph status` can display it.
	DaemonMode string
}

// Run starts the daemon. It blocks until either:
//   - the Service receives Stop,
//   - the process receives SIGTERM/SIGINT, or
//   - the listener errors fatally.
//
// On exit it removes the socket file and pid file. The function is the
// daemon's entire public surface — cmd/archigraph just imports daemon
// and calls Run.
func Run(ctx context.Context, cfg Config) error {
	logger := cfg.Logger
	if logger == nil {
		logger = log.New(os.Stderr, "archigraph-daemon: ", log.LstdFlags|log.Lmicroseconds)
	}

	// Layer 1 self-defense: refuse to start if a canonical (non-/tmp) daemon
	// is already running and this binary lives under /tmp. This prevents the
	// hot-loop runaway observed on 2026-05-20 where agent-spawned daemons were
	// adopted by launchd after the agent exited and spun at ~1000% CPU.
	if err := SelfDefenseCheck(logger); err != nil {
		return err
	}

	if err := EnsureLayout(cfg.Layout); err != nil {
		return fmt.Errorf("ensure layout: %w", err)
	}

	// PH1b: one-time migration of legacy flat-layout store slots into the
	// per-ref sub-directory layout introduced by PH1a (#2089). Called here
	// (after EnsureLayout, before accepting RPCs) so every read path sees
	// the new layout. Idempotent: already-migrated stores are skipped.
	if storeDir := StoreDir(); storeDir != "" {
		if err := MigrateToRefStore(storeDir); err != nil {
			// Non-fatal: log and continue; the daemon can still serve the
			// old layout (callers fall back gracefully).
			logger.Printf("startup: MigrateToRefStore: %v (non-fatal)", err)
		} else {
			logger.Printf("startup: MigrateToRefStore complete store=%s", storeDir)
		}
	}

	releasePID, err := AcquirePIDFile(cfg.Layout.PIDPath)
	if err != nil {
		return err
	}
	defer releasePID()

	// Remove any stale socket file from a previous crash (Unix only; on
	// Windows named pipes are not filesystem objects and os.Remove is a no-op).
	_ = os.Remove(cfg.Layout.SocketPath)

	listener, err := transport.Listen(cfg.Layout.SocketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Layout.SocketPath, err)
	}
	// On Unix, chmod 0600 makes the socket per-user. The transport package
	// sets an equivalent ACL on Windows named pipes so no explicit Chmod is
	// needed there. chmodSocket is a no-op on Windows.
	if err := chmodSocket(cfg.Layout.SocketPath); err != nil {
		_ = listener.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(cfg.Layout.SocketPath)
	}()

	stopReq := make(chan struct{})
	svc := newService(cfg.Index, cfg.Rebuild, cfg.QualityAudit, cfg.Layout.SocketPath, stopReq, logger, cfg.MaxConcurrentGroups)
	svc.mcpListTools = cfg.MCPListTools
	svc.mcpCallTool = cfg.MCPCallTool
	if cfg.DashboardPort > 0 {
		svc.dashboardPort = cfg.DashboardPort
	}
	// S7 (#2157): wire the operational mode so Status can surface it.
	svc.daemonMode = cfg.DaemonMode
	// PH2a (#2096): wire watcher pause/resume slot counts into Status RPC.
	if cfg.WatcherMgrStats != nil {
		svc.watcherMgrStats = cfg.WatcherMgrStats
	}

	// Layer 2 self-defense: start CPU watchdog for ephemeral /tmp daemons.
	// The watchdog passes the service's real inFlight counter so it can
	// distinguish hot-loops (no work) from legitimate sustained indexing.
	StartCPUWatchdog(&svc.inFlight, logger)

	// Phase B — bring up the scheduler + watcher when the caller
	// supplied the four hooks. They are optional so tests can exercise
	// the bare RPC surface without dragging the extractor into scope.
	if cfg.SchedulerIndex != nil {
		history := sched.LoadRSSHistory(cfg.RSSHistoryPath)
		scheduler := sched.New(sched.Config{
			Index:         cfg.SchedulerIndex,
			Links:         cfg.SchedulerLinks,
			Algorithms:    cfg.SchedulerAlgo,
			GroupsForRepo: cfg.GroupsForRepo,
			Logger:        logger,
			BudgetMB:      cfg.MaxRSSBudgetMB,
			Predict:       sched.PredictRSS,
			History:       history,
			// PH1b: capture the HEAD ref at enqueue time so debounced
			// batches index against the branch that was active when the
			// file-change event fired, not the branch at dispatch time.
			RefCapture: func(repoPath string) string {
				return gitmeta.Capture(repoPath).Ref
			},
		})
		if cfg.MaxRSSBudgetMB > 0 {
			logger.Printf("scheduler: RSS-budget admission control enabled budget=%dMB history=%s",
				cfg.MaxRSSBudgetMB, cfg.RSSHistoryPath)
		}
		scheduler.Start()
		svc.scheduler = scheduler
		defer scheduler.Stop()

		wcfg := cfg.WatcherConfig
		watcher, werr := watch.NewWatcherConfig(wcfg, func(repo string, bulk bool) {
			if bulk {
				logger.Printf("watcher: bulk trigger repo=%s — enqueuing full reindex", repo)
			}
			scheduler.Enqueue(repo)
		}, logger)
		if werr != nil {
			logger.Printf("watcher: disabled (%v)", werr)
		} else {
			svc.watcher = watcher
			defer watcher.Stop()

			// PH1b (Option B): start the .git/HEAD poller alongside the
			// fsnotify watcher. .git/ remains in SkipDirs (no fsnotify
			// noise from git internal object/pack writes), and the poller
			// detects branch switches by reading gitmeta.Capture every 2s.
			//
			// When a branch switch is detected:
			//   1. A synthetic EnqueueRef is sent to the scheduler with the
			//      new ref captured at detection time.
			//   2. The scheduler writes the new index into refs/<new-ref>/,
			//      leaving the old ref's graph untouched on disk.
			headPoller := watch.NewGitHeadPoller(0, func(ev watch.BranchSwitchEvent) {
				logger.Printf("branch-switch detected: %s %s@%s -> %s@%s",
					ev.RepoPath, ev.OldRef, ev.OldSHA, ev.NewRef, ev.NewSHA)
				scheduler.EnqueueRef(ev.RepoPath, ev.NewRef)
			}, logger)
			headPoller.Start()
			defer headPoller.Stop()

			// #1456: subscribe repos OFF the critical boot path.
			if cfg.ReposToWatch != nil {
				go func() {
					t0 := time.Now()
					repos := cfg.ReposToWatch()
					for _, r := range repos {
						if _, err := watcher.AddRepo(r); err != nil {
							logger.Printf("watcher: add repo %s: %v", r, err)
						}
						// Also register with the HEAD poller so branch
						// switches in every watched repo are detected.
						headPoller.AddRepo(r)
					}
					logger.Printf("watcher: subscription complete repos=%d took=%s",
						len(repos), time.Since(t0).Truncate(time.Millisecond))
				}()
			}
			if cfg.OnWatcherReady != nil {
				cfg.OnWatcherReady(watcher)
			}
		}
	}

	// Pattern confidence time-decay scheduler — runs every 6 hours (or a
	// caller-supplied interval for tests). Requires PatternGroupDirs to be
	// non-nil; skipped gracefully when the caller has not provided it.
	if cfg.PatternGroupDirs != nil {
		decayInterval := cfg.PatternDecayInterval
		if decayInterval <= 0 {
			decayInterval = 6 * time.Hour
		}
		decayJob := buildPatternDecayJob(cfg.PatternGroupDirs, logger)
		decaySched := agentpatterns.NewDecayScheduler(decayInterval, decayJob)
		decayCtx, decayCancel := context.WithCancel(ctx)
		go decaySched.Run(decayCtx)
		defer decayCancel()
		logger.Printf("pattern decay scheduler started interval=%s", decayInterval)
	}

	// Dashboard HTTP server — started in a goroutine so it does not
	// block the RPC socket. Shuts down when the daemon context is done.
	// The DashboardServe hook is injected from cmd/archigraph to avoid
	// the import cycle that would arise from importing internal/dashboard here.
	if cfg.DashboardServe != nil && cfg.DashboardPort > 0 {
		bind := cfg.DashboardBind
		if bind == "" {
			bind = "127.0.0.1"
		}
		dashCtx, dashCancel := context.WithCancel(ctx)
		defer dashCancel()
		go func() {
			if err := cfg.DashboardServe(dashCtx, bind, cfg.DashboardPort, logger); err != nil {
				logger.Printf("dashboard: %v", err)
			}
		}()
		logger.Printf("dashboard listening on http://%s:%d/", bind, cfg.DashboardPort)
	}

	server := rpc.NewServer()
	if err := server.RegisterName(proto.ServiceName, svc); err != nil {
		return fmt.Errorf("register %s: %w", proto.ServiceName, err)
	}

	// Signals — we want SIGTERM (systemd, launchd's stop) and SIGINT
	// (Ctrl-C when running in the foreground for development).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	logger.Printf("ready socket=%s pid=%d", cfg.Layout.SocketPath, os.Getpid())

	// Track accepted connections so we can wait for them to drain on
	// shutdown. The waitgroup is decremented when each conn loop returns.
	var connWG sync.WaitGroup
	acceptDone := make(chan struct{})
	go acceptLoop(listener, server, &connWG, logger, acceptDone)

	// Wait for any shutdown trigger.
	select {
	case <-stopReq:
		logger.Printf("stop requested via RPC")
	case sig := <-sigCh:
		logger.Printf("signal %s received", sig)
	case <-ctx.Done():
		logger.Printf("context cancelled: %v", ctx.Err())
	case <-acceptDone:
		// acceptLoop only returns when the listener closes, which we
		// don't do until shutdown — but if the listener dies on its
		// own we should treat that as fatal and exit.
		logger.Printf("listener closed unexpectedly")
		return errors.New("listener closed")
	}

	// Stop accepting new connections, then wait for in-flight ones.
	_ = listener.Close()
	<-acceptDone
	connWG.Wait()
	logger.Printf("graceful shutdown complete")
	return nil
}

// acceptLoop pulls connections off the listener and hands each to
// jsonrpc.ServeConn under the registered server. The waitgroup tracks
// each conn so Run can join them on shutdown.
func acceptLoop(l net.Listener, srv *rpc.Server, wg *sync.WaitGroup, logger *log.Logger, done chan<- struct{}) {
	defer close(done)
	for {
		conn, err := l.Accept()
		if err != nil {
			// Listener closed during shutdown — that's the happy path.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			logger.Printf("accept: %v", err)
			return
		}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			defer c.Close()
			srv.ServeCodec(jsonrpc.NewServerCodec(&loggingConn{Conn: c, log: logger}))
		}(conn)
	}
}

// loggingConn wraps a net.Conn so EOF / read errors get a single log
// line. Without this, jsonrpc swallows the close silently and we have
// no way to confirm clients are actually disconnecting on demand.
type loggingConn struct {
	net.Conn
	log *log.Logger
}

func (c *loggingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err != nil && err != io.EOF {
		// EOF is the normal client disconnect; anything else is worth
		// noting. We don't return the wrapper here, so jsonrpc still
		// sees the original error.
		c.log.Printf("conn read: %v", err)
	}
	return n, err
}

// buildPatternDecayJob constructs the DecayJob that the pattern decay scheduler
// calls on each tick (every 6 hours by default).
//
// The job performs two passes:
//
//  1. Confidence decay (per ADR-0018 + γ spec): for each pattern with
//     last_applied > 30 days ago AND confidence > 0.2, decrement by
//     DecayDeltaPer30Day (0.05) per tick, floored at ConfidenceFloor (0.2).
//  2. Candidate pruning (per ADR-0018 δ spec): for each candidate
//     (is_candidate=true) with last_validated older than the group's
//     `candidate_decay_days` (loaded from patterns-config.json; default
//     90), drop it from the store.
//
// The decay step and the candidate-pruning step share a single load+save
// cycle so the store mutates atomically.
func buildPatternDecayJob(groupDirs func() map[string]string, logger *log.Logger) agentpatterns.DecayJob {
	return func(nowUnix int64) {
		dirs := groupDirs()
		for group, dir := range dirs {
			if dir == "" {
				continue
			}
			patterns, err := agentpatterns.Load(dir)
			if err != nil {
				if logger != nil {
					logger.Printf("pattern decay: load %s: %v", group, err)
				}
				continue
			}
			cfg, cfgErr := agentpatterns.LoadConfig(dir)
			if cfgErr != nil && logger != nil {
				logger.Printf("pattern decay: load config %s: %v (using defaults)", group, cfgErr)
				cfg = agentpatterns.DefaultConfig()
			}
			changed := false

			// Pass 1 — confidence decay.
			for i := range patterns {
				p := &patterns[i]
				if p.LastApplied == 0 {
					continue // never applied — skip decay
				}
				daysSince := float64(nowUnix-p.LastApplied) / 86400.0
				if daysSince <= 30 {
					continue // within the 30-day grace window
				}
				if p.Confidence <= agentpatterns.ConfidenceFloor {
					continue // already at floor
				}
				// Flat decrement per scheduler tick (not proportional to days).
				before := p.Confidence
				newConf := p.Confidence - agentpatterns.DecayDeltaPer30Day
				if newConf < agentpatterns.ConfidenceFloor {
					newConf = agentpatterns.ConfidenceFloor
				}
				p.Confidence = newConf
				if p.Confidence != before {
					changed = true
				}
			}

			// Pass 2 — candidate pruning. Operates only on patterns
			// with is_candidate=true and last_validated older than
			// the configured cutoff. Approved patterns are never
			// auto-pruned (per ADR-0018 Open Question 1).
			if cfg.CandidateDecayDays > 0 {
				cutoff := nowUnix - int64(cfg.CandidateDecayDays)*86400
				kept := patterns[:0]
				pruned := 0
				for _, p := range patterns {
					if p.IsCandidate && p.LastValidated > 0 && p.LastValidated < cutoff {
						pruned++
						continue
					}
					kept = append(kept, p)
				}
				if pruned > 0 {
					patterns = kept
					changed = true
					if logger != nil {
						logger.Printf("pattern decay: pruned %d stale candidates in %s", pruned, group)
					}
				}
			}

			if !changed {
				continue
			}
			if err := agentpatterns.Save(dir, patterns); err != nil {
				if logger != nil {
					logger.Printf("pattern decay: save %s: %v", group, err)
				}
			}
		}
	}
}
