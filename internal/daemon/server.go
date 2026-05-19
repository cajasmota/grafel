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

	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/daemon/sched"
	"github.com/cajasmota/archigraph/internal/daemon/watch"
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
	ReposToWatch   func() []string                              // repos to subscribe at startup
	GroupsForRepo  func(repoPath string) []string               // for cross-repo link debounce
	SchedulerIndex func(ctx context.Context, repo string) error // fast reindex (skip algo pass)
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

	if err := EnsureLayout(cfg.Layout); err != nil {
		return fmt.Errorf("ensure layout: %w", err)
	}

	releasePID, err := AcquirePIDFile(cfg.Layout.PIDPath)
	if err != nil {
		return err
	}
	defer releasePID()

	// Remove any stale socket from a previous crash. We checked the
	// pid file above so we know no live daemon is using it.
	_ = os.Remove(cfg.Layout.SocketPath)

	listener, err := net.Listen("unix", cfg.Layout.SocketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Layout.SocketPath, err)
	}
	// 0600 makes the socket per-user; the daemon is single-user only.
	if err := os.Chmod(cfg.Layout.SocketPath, 0o600); err != nil {
		_ = listener.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(cfg.Layout.SocketPath)
	}()

	stopReq := make(chan struct{})
	svc := newService(cfg.Index, cfg.Rebuild, cfg.QualityAudit, cfg.Layout.SocketPath, stopReq)

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
		})
		if cfg.MaxRSSBudgetMB > 0 {
			logger.Printf("scheduler: RSS-budget admission control enabled budget=%dMB history=%s",
				cfg.MaxRSSBudgetMB, cfg.RSSHistoryPath)
		}
		scheduler.Start()
		svc.scheduler = scheduler
		defer scheduler.Stop()

		watcher, werr := watch.NewWatcher(2*time.Second, func(repo string) {
			scheduler.Enqueue(repo)
		}, logger)
		if werr != nil {
			logger.Printf("watcher: disabled (%v)", werr)
		} else {
			svc.watcher = watcher
			defer watcher.Stop()
			if cfg.ReposToWatch != nil {
				for _, r := range cfg.ReposToWatch() {
					if _, err := watcher.AddRepo(r); err != nil {
						logger.Printf("watcher: add repo %s: %v", r, err)
					}
				}
			}
		}
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
