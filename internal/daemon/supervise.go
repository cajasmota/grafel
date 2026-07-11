package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/statusfile"
)

// supervise.go is the serve-side engine supervisor (ADR-0024 Phase 1 / PR2,
// epic #5729). When the serve/engine split is ON, the serve process spawns a
// `grafel engine` child and keeps it alive: it health-gates the child via the
// engine-global liveness statusfile, relaunches it on crash with exponential
// backoff, gracefully drains it on serve shutdown (SIGTERM → bounded wait →
// SIGKILL, reaped — no orphan), and only gives up (surfacing a fatal so the OS
// unit recycles serve) when the child crash-loops at the backoff ceiling.
//
// serve NEVER exits merely because the engine is degraded or dead: it keeps
// answering reads from the last-good graph.fb. The engine is a restartable
// child whose death is a local event, not a service event.

// Supervisor tuning. All are overridable per-instance (see newEngineSupervisor)
// so tests can run the whole spawn/crash/restart loop in milliseconds.
const (
	defaultEngineBackoffInitial = 500 * time.Millisecond
	defaultEngineBackoffMax     = 30 * time.Second
	// defaultEngineHealthyUptime: a child that stays up at least this long is
	// considered to have recovered, so the backoff + crash-loop counters reset.
	defaultEngineHealthyUptime = 60 * time.Second
	// defaultEngineMaxCeilingHits: how many consecutive relaunches AT the
	// backoff ceiling are tolerated before serve declares the engine unkeepable.
	defaultEngineMaxCeilingHits = 3
	// defaultEngineDrainTimeout bounds the graceful SIGTERM→exit wait before the
	// supervisor escalates to SIGKILL during drain.
	defaultEngineDrainTimeout = 5 * time.Second
	// engineHealthStaleMultiplier: a liveness heartbeat older than this many
	// heartbeat intervals marks the engine DEGRADED.
	engineHealthStaleMultiplier = 3
)

// engineChildCommand builds the exec.Cmd that launches the engine child. It is
// a package var so tests can substitute a helper-process command (the standard
// os/exec subprocess-testing pattern) instead of spawning a real grafel binary.
// Production uses defaultEngineChildCommand.
var engineChildCommand = defaultEngineChildCommand

// defaultEngineChildCommand launches `grafel engine --foreground` from the
// current executable, in its own process group, with the daemon root threaded
// through the environment (so the child's DefaultLayout resolves the same root)
// and stdio inherited so its logs land alongside serve's.
func defaultEngineChildCommand(selfExe, root string) *exec.Cmd {
	cmd := exec.Command(selfExe, "engine", "--foreground")
	cmd.Env = append(os.Environ(), EnvRoot+"="+root)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = engineChildSysProcAttr()
	return cmd
}

// SetEngineChildCommandForTest overrides how the supervisor spawns the engine
// child for the duration of a test and returns a restore closure. Tests use it
// to spawn a helper subprocess (re-invoking the test binary) rather than a real
// grafel binary. Production code must never call this.
func SetEngineChildCommandForTest(fn func(selfExe, root string) *exec.Cmd) (restore func()) {
	prev := engineChildCommand
	engineChildCommand = fn
	return func() { engineChildCommand = prev }
}

// engineSupervisor spawns and supervises the split-mode engine child process.
type engineSupervisor struct {
	layout  Layout
	logger  *slog.Logger
	selfExe string

	backoffInitial time.Duration
	backoffMax     time.Duration
	healthyUptime  time.Duration
	maxCeilingHits int
	drainTimeout   time.Duration

	mu       sync.Mutex
	childPID int
	fatalErr error

	stopCh   chan struct{}
	stopOnce sync.Once
	doneCh   chan struct{}
	fatalCh  chan error
}

// newEngineSupervisor constructs a supervisor with production defaults.
func newEngineSupervisor(layout Layout, logger *slog.Logger) *engineSupervisor {
	if logger == nil {
		logger = buildSlogLogger(os.Stderr)
	}
	return &engineSupervisor{
		layout:         layout,
		logger:         logger,
		backoffInitial: defaultEngineBackoffInitial,
		backoffMax:     defaultEngineBackoffMax,
		healthyUptime:  defaultEngineHealthyUptime,
		maxCeilingHits: defaultEngineMaxCeilingHits,
		drainTimeout:   defaultEngineDrainTimeout,
	}
}

// start resolves the self executable and launches the supervision goroutine.
// It returns once the goroutine is running (the first spawn happens inside it).
func (s *engineSupervisor) start(ctx context.Context) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve self executable: %w", err)
	}
	s.selfExe = exe
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.fatalCh = make(chan error, 1)
	go s.run(ctx)
	return nil
}

// fatal returns a receive-only channel that fires once (with a non-nil error)
// if the supervisor gives up keeping the engine alive.
func (s *engineSupervisor) fatal() <-chan error { return s.fatalCh }

// fatalError returns the recorded fatal error (nil if none). Safe to call after
// the run loop has exited.
func (s *engineSupervisor) fatalError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fatalErr
}

// stop requests shutdown (the run loop drains the current child) and blocks
// until the run loop has exited and the child is reaped. Idempotent.
func (s *engineSupervisor) stop() {
	if s.stopCh == nil {
		return // never started
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
	<-s.doneCh
}

func (s *engineSupervisor) setChildPID(pid int) {
	s.mu.Lock()
	s.childPID = pid
	s.mu.Unlock()
}

func (s *engineSupervisor) getChildPID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.childPID
}

// healthy reports whether the engine child is currently HEALTHY: a live child
// is spawned AND the engine-global liveness statusfile names that exact child
// pid AND its heartbeat is fresh. The second return value explains a false
// (DEGRADED) verdict.
func (s *engineSupervisor) healthy() (bool, string) {
	pid := s.getChildPID()
	if pid == 0 {
		return false, "no engine child running"
	}
	f, err := statusfile.Read(engineLivenessStatusKey(s.layout.Root))
	if err != nil {
		return false, "engine liveness file missing"
	}
	if f.EnginePID != pid {
		return false, fmt.Sprintf("liveness pid %d != spawned child pid %d", f.EnginePID, pid)
	}
	maxAge := time.Duration(engineHealthStaleMultiplier) * statusHeartbeatInterval()
	if age := time.Since(f.HeartbeatAt); age > maxAge {
		return false, fmt.Sprintf("stale heartbeat (%s old, max %s)", age.Truncate(time.Millisecond), maxAge)
	}
	return true, ""
}

// EngineHeartbeatStaleAfter returns the max age a liveness heartbeat may be
// before it is considered stale — the SAME threshold engineSupervisor.healthy
// uses (engineHealthStaleMultiplier heartbeat intervals). Exported for
// external readers (`grafel doctor`'s engine-liveness check, ADR-0024 PR5,
// epic #5729) that need the identical staleness definition without
// duplicating the constant.
func EngineHeartbeatStaleAfter() time.Duration {
	return time.Duration(engineHealthStaleMultiplier) * statusHeartbeatInterval()
}

// run is the supervision loop: spawn, wait, relaunch-with-backoff, and finally
// drain on stop/ctx-cancel.
func (s *engineSupervisor) run(ctx context.Context) {
	defer close(s.doneCh)

	backoff := s.backoffInitial
	ceilingHits := 0

	for {
		// Bail before spawning if we've been asked to stop.
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}

		cmd := engineChildCommand(s.selfExe, s.layout.Root)
		startedAt := time.Now()
		if err := cmd.Start(); err != nil {
			s.logger.Error("engine supervisor: spawn failed", "err", err)
			// Treat a failed spawn like a crash: back off and retry.
			if s.backoffAndMaybeGiveUp(ctx, &backoff, &ceilingHits) {
				return
			}
			continue
		}
		pid := cmd.Process.Pid
		s.setChildPID(pid)
		s.logger.Info("engine supervisor: engine child started", "pid", pid, "exe", s.selfExe)

		waitCh := make(chan error, 1)
		go func() { waitCh <- cmd.Wait() }()

		select {
		case <-s.stopCh:
			s.terminateChild(cmd, waitCh)
			return
		case <-ctx.Done():
			s.terminateChild(cmd, waitCh)
			return
		case werr := <-waitCh:
			s.setChildPID(0)
			uptime := time.Since(startedAt)
			s.logger.Warn("engine supervisor: engine child exited",
				"pid", pid, "err", werr, "uptime", uptime.Truncate(time.Millisecond))
			// A child that stayed up long enough counts as recovered: reset the
			// crash-loop bookkeeping so a later, unrelated crash starts fresh.
			if uptime >= s.healthyUptime {
				backoff = s.backoffInitial
				ceilingHits = 0
			}
			if s.backoffAndMaybeGiveUp(ctx, &backoff, &ceilingHits) {
				return
			}
		}
	}
}

// backoffAndMaybeGiveUp sleeps for the current backoff (waking early on
// stop/ctx-cancel), grows it toward the ceiling, and counts consecutive
// relaunches at the ceiling. It returns true when the run loop should exit —
// either because shutdown was requested during the wait, or because the engine
// is unkeepable (in which case it also records + signals the fatal).
func (s *engineSupervisor) backoffAndMaybeGiveUp(ctx context.Context, backoff *time.Duration, ceilingHits *int) (done bool) {
	if *backoff >= s.backoffMax {
		*ceilingHits++
		if *ceilingHits >= s.maxCeilingHits {
			err := fmt.Errorf("engine child crash-looping: %d consecutive relaunches at the %s backoff ceiling",
				*ceilingHits, s.backoffMax)
			s.logger.Error("engine supervisor: giving up — engine unkeepable", "err", err)
			s.mu.Lock()
			s.fatalErr = err
			s.mu.Unlock()
			select {
			case s.fatalCh <- err:
			default:
			}
			return true
		}
	}

	wait := *backoff
	s.logger.Info("engine supervisor: relaunching engine after backoff", "backoff", wait)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-s.stopCh:
		return true
	case <-ctx.Done():
		return true
	case <-timer.C:
	}

	*backoff *= 2
	if *backoff > s.backoffMax {
		*backoff = s.backoffMax
	}
	return false
}

// terminateChild gracefully drains the running child: SIGTERM, wait up to
// drainTimeout, then SIGKILL, always reaping via the existing waitCh (cmd.Wait
// may be called only once, so the run loop's waitCh goroutine owns it and we
// consume its result here). On return the child is reaped — no orphan, no
// zombie.
func (s *engineSupervisor) terminateChild(cmd *exec.Cmd, waitCh chan error) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	s.logger.Info("engine supervisor: draining engine child", "pid", pid)
	_ = signalTerminate(cmd.Process)

	timer := time.NewTimer(s.drainTimeout)
	defer timer.Stop()
	select {
	case <-waitCh:
		s.logger.Info("engine supervisor: engine child exited after SIGTERM", "pid", pid)
	case <-timer.C:
		s.logger.Warn("engine supervisor: engine child did not exit within drain window — SIGKILL",
			"pid", pid, "timeout", s.drainTimeout)
		_ = cmd.Process.Kill()
		<-waitCh // reap
	}
	s.setChildPID(0)
}
