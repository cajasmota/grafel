package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cajasmota/grafel/internal/executil"
	"github.com/cajasmota/grafel/internal/process"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// supervise.go is the serve-side engine supervisor (ADR-0024 Phase 1 / PR2,
// epic #5729). When the serve/engine split is ON, the serve process spawns a
// `grafel engine` child and keeps it alive: it health-gates the child via the
// engine-global liveness statusfile, relaunches it on crash with exponential
// backoff, gracefully drains it on serve shutdown (SIGTERM → bounded wait →
// SIGKILL, reaped — no orphan), and only gives up (surfacing a fatal so the OS
// unit recycles serve) when the child crash-loops at the backoff ceiling, or
// when the child cannot be SPAWNED at all — a construction failure (cmd.Start
// erroring, no process ever created) is classified, logged and budgeted
// separately from a crash, because the crash budget's recovery rule is
// unreachable for it (#7087).
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
	// defaultEngineMaxSpawnFailures: how many CONSECUTIVE construction failures
	// (cmd.Start returning an error — no child process ever existed) are
	// tolerated before serve declares the engine UNSPAWNABLE (#7087).
	//
	// This is deliberately a SEPARATE, much smaller budget than the crash-loop
	// one above, because the crash budget's recovery rule is structurally
	// unreachable for a construction failure: backoff/ceilingHits reset only
	// once a child has stayed up past healthyUptime, which requires a child to
	// have existed. A deterministic Start error (bad exe path, missing binary,
	// unusable inherited handle — see #7083) therefore walks the ENTIRE crash
	// budget (~91s at production tuning) re-attempting something that fails
	// identically every time, and dies with a message blaming a crash loop
	// that never happened.
	//
	// It is not ZERO because some construction failures ARE transient (EAGAIN
	// under fork pressure, a briefly-locked binary mid-upgrade): the counter
	// resets on every successful Start, so a hiccup followed by a good spawn
	// costs nothing.
	defaultEngineMaxSpawnFailures = 3
	// defaultEngineDrainTimeout bounds the graceful SIGTERM→exit wait before the
	// supervisor escalates to SIGKILL during drain.
	defaultEngineDrainTimeout = 5 * time.Second
	// engineHealthStaleMultiplier: a liveness heartbeat older than this many
	// heartbeat intervals marks the engine DEGRADED.
	engineHealthStaleMultiplier = 3
)

// engineChildCommandFunc builds the exec.Cmd that launches the engine child.
type engineChildCommandFunc func(selfExe, root string) *exec.Cmd

// engineChildCommandOverride holds the test seam that substitutes a
// helper-process command (the standard os/exec subprocess-testing pattern) for
// a real grafel binary. nil (the zero value) means "use production's
// defaultEngineChildCommand"; production never stores anything here.
//
// #6056: this MUST stay an atomic. engineSupervisor.run resolves the seam
// repeatedly from a live goroutine, so a test's deferred restore() writing it
// during cleanup races that loop. Making it an atomic.Pointer removes the
// plain-access form entirely: there is no way to read or write it
// unsynchronised, so the protection cannot be silently dropped by a caller
// that forgets to join the supervisor first. (Contrast listenFn, read exactly
// once at startup — repeated reads from a live goroutine are the hazard, not
// the seam pattern.)
var engineChildCommandOverride atomic.Pointer[engineChildCommandFunc]

// engineChildCommand resolves the effective child-spawn constructor: the test
// override when one is installed, else the production default.
func engineChildCommand(selfExe, root string) *exec.Cmd {
	if fn := engineChildCommandOverride.Load(); fn != nil {
		return (*fn)(selfExe, root)
	}
	return defaultEngineChildCommand(selfExe, root)
}

// defaultEngineChildCommand launches `grafel engine --foreground` from the
// current executable, in its own process group, with stdio inherited so its
// logs land alongside serve's.
//
// Store-root invariant (production-divergence fix, ADR-0024 PR6 blocker, epic
// #5729): the engine child inherits serve's environment UNCHANGED. It must NOT
// synthesize GRAFEL_DAEMON_ROOT. That env var is the isolated-daemon switch that
// flips the on-disk store layout from the production StoreDir()
// (~/.grafel|$GRAFEL_HOME/store) to <root>/state — see repoBaseDir/requestsRoot/
// StoreRootBase in state_path.go + requests_drain.go, all of which key off
// os.Getenv(EnvRoot), not off Layout.Root.
//
// Serve resolves its own root from that SAME env: DefaultLayout uses
// GRAFEL_DAEMON_ROOT when set (isolated/tests), else ~/.grafel (production, where
// launchd/systemd do NOT set it). So plain os.Environ() inheritance makes the
// child observe the IDENTICAL EnvRoot state serve saw:
//
//   - production (serve has no GRAFEL_DAEMON_ROOT): the child also sees it unset
//     → both resolve StoreDir(). Force-appending EnvRoot=layout.Root (=~/.grafel)
//     here is what broke this: it flipped the child to ~/.grafel/state while serve
//     kept using ~/.grafel/store, so serve-written reindex/rebuild requests were
//     silently dropped and engine-written graph.fb landed where serve never read.
//   - isolated (serve has GRAFEL_DAEMON_ROOT=<tmp>): it is already in os.Environ()
//     and inherited verbatim → both resolve <tmp>/state.
//
// root is retained in the signature (it is Layout.Root, threaded from the
// supervisor). It is used to locate the daemon's own log sinks for the child's
// standard handles (see engineChildSink, #7083), but is still deliberately
// NOT written into the child env.
func defaultEngineChildCommand(selfExe, root string) *exec.Cmd {
	cmd := exec.Command(selfExe, "engine", "--foreground")
	cmd.Env = os.Environ()
	// Standard handles: the daemon's OWN sinks, never the inherited ones
	// (#7083), and stdout/stderr stay SPLIT the way the platform's own service
	// definition splits them. See engineChildSink. A nil Stdout/Stderr is
	// os/exec's documented "connect the child to os.DevNull" — the deliberate
	// fallback when a sink cannot be opened. Leaving the field nil (rather
	// than storing a nil *os.File) matters: os/exec takes its *os.File branch
	// on a typed nil, whose Fd() is ^uintptr(0), which is #7083's own defect.
	if out := engineChildSink(logPathForRoot(root)); out != nil {
		cmd.Stdout = out
	}
	if errSink := engineChildSink(errPathForRoot(root)); errSink != nil {
		cmd.Stderr = errSink
	}
	cmd.SysProcAttr = engineChildSysProcAttr()
	executil.NoWindow(cmd)
	return cmd
}

// engineChildSinkCache caches the engine child's log sinks per path.
//
// A sink has to outlive defaultEngineChildCommand (os/exec duplicates the
// descriptor/handle into the child at Start, but the *os.File stays the
// PARENT's to close) and the constructor has no completion hook — the seam
// returns only an *exec.Cmd. Opening one per spawn would therefore leak a
// descriptor per relaunch in a crash loop. One sink per path, opened lazily
// and held for the life of the process, is what the daemon actually wants
// anyway: these are the same append-only files its own logger and its service
// definition write, under the no-rotation contract (#2300, see layoutFromRoot).
var engineChildSinkCache = struct {
	mu     sync.Mutex
	byPath map[string]*os.File
}{}

// engineChildSink returns the file the engine child's stdout (or stderr) is
// wired to, opened for APPEND so it never truncates what the daemon's own
// logger has already written there — the byte-offset contract in #2300. It
// returns nil when there is no usable owned sink, which leaves the
// corresponding cmd field nil, i.e. os.DevNull.
//
// #7083: the child used to inherit os.Stdout/os.Stderr, so whether the daemon
// could run its own engine depended on a property of whatever launched it —
// one it does not control and never checks. os/exec passes those *os.Files to
// StartProcess, which DUPLICATES the underlying descriptor/handle into the
// child; on Windows duplicating an unusable handle fails and the spawn fails
// with it. A process started by `Start-Process -WindowStyle Hidden` with no
// -Redirect* flag runs under UseShellExecute=true and has no standard handles
// at all — and run() treats a failed spawn as a crash, so it backs off,
// retries, and gives up.
//
// The daemon therefore OWNS the handles it hands down, unconditionally. It
// does not probe the inherited ones: there is no portable way to ask whether a
// handle the parent was given is usable (the reliable test is to use it, which
// is the failure being avoided), and a probe would leave a second, untested
// code path for exactly the launcher shape we cannot reproduce in CI.
//
// The destinations are the daemon's own two log files, which keeps the split
// the platform service definitions already make: launchd's plist sends the
// daemon's stdout to daemon.log and its stderr to daemon.err
// (internal/daemon/service/launchd_darwin.go), and daemon.err is the file
// `grafel status` and `grafel doctor` tell users to read after a failure. An
// engine-child panic therefore lands where the product says it will, on every
// launcher — including `grafel start` and systemd, which previously sent it to
// daemon.log and to the journal respectively.
//
// The log DIRECTORY is never created here: <root>/logs must already exist
// (EnsureLayout makes it before serve starts). The log FILE is created if
// absent, as O_CREATE implies. An empty root, a missing log directory, or an
// unopenable path yields nil rather than an inherited handle.
func engineChildSink(path string) *os.File {
	// An empty root would make path relative ("logs/daemon.log"), so a stray
	// logs/ directory in the daemon's cwd would receive and cache engine output.
	if path == "" || !filepath.IsAbs(path) {
		return nil
	}

	engineChildSinkCache.mu.Lock()
	defer engineChildSinkCache.mu.Unlock()
	if f, ok := engineChildSinkCache.byPath[path]; ok {
		return f
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		// Not cached: a later spawn retries, in case the directory appears.
		return nil
	}
	if engineChildSinkCache.byPath == nil {
		engineChildSinkCache.byPath = make(map[string]*os.File)
	}
	engineChildSinkCache.byPath[path] = f
	return f
}

// closeEngineChildSinksForTest closes and drops every cached engine-child log
// sink. Tests that point a sink at a t.TempDir root call it in cleanup, so the
// directory can be removed on Windows (where an open file blocks removal).
// Production never calls it: the daemon holds its sink for its whole life.
func closeEngineChildSinksForTest() {
	engineChildSinkCache.mu.Lock()
	defer engineChildSinkCache.mu.Unlock()
	for path, f := range engineChildSinkCache.byPath {
		_ = f.Close()
		delete(engineChildSinkCache.byPath, path)
	}
}

// SetEngineChildCommandForTest overrides how the supervisor spawns the engine
// child for the duration of a test and returns a restore closure. Tests use it
// to spawn a helper subprocess (re-invoking the test binary) rather than a real
// grafel binary. Production code must never call this.
// Overrides nest LIFO: restore() puts back whatever was installed before, so
// an inner Set/restore pair inside an outer one behaves correctly.
//
// fn == nil CLEARS the override (the seam falls back to
// defaultEngineChildCommand), matching setBackgroundAlgoGateForTest /
// setBackgroundAlgoDoneForTest in internal/dashboard. Without this branch a nil
// fn would store a non-nil pointer to a nil func and the next resolve would
// panic on the call — a latent trap for the first caller that clears by passing
// nil rather than by calling restore().
func SetEngineChildCommandForTest(fn func(selfExe, root string) *exec.Cmd) (restore func()) {
	prev := engineChildCommandOverride.Load()
	if fn == nil {
		engineChildCommandOverride.Store(nil)
		return func() { engineChildCommandOverride.Store(prev) }
	}
	next := engineChildCommandFunc(fn)
	engineChildCommandOverride.Store(&next)
	return func() { engineChildCommandOverride.Store(prev) }
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
	// maxSpawnFailures bounds CONSECUTIVE construction failures (cmd.Start
	// errors) on their own budget, separate from the crash-loop one (#7087).
	maxSpawnFailures int
	drainTimeout     time.Duration

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
		layout:           layout,
		logger:           logger,
		backoffInitial:   defaultEngineBackoffInitial,
		backoffMax:       defaultEngineBackoffMax,
		healthyUptime:    defaultEngineHealthyUptime,
		maxCeilingHits:   defaultEngineMaxCeilingHits,
		maxSpawnFailures: defaultEngineMaxSpawnFailures,
		drainTimeout:     defaultEngineDrainTimeout,
	}
}

// start resolves the self executable, reaps any stale engine left behind by a
// previous unclean serve death (SECONDARY orphan-engine hardening layer, see
// reapStaleEngine), and launches the supervision goroutine. It returns once
// the goroutine is running (the first spawn happens inside it).
func (s *engineSupervisor) start(ctx context.Context) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve self executable: %w", err)
	}
	s.selfExe = exe

	// SECONDARY layer (ADR-0024 orphan-engine hardening, epic #5729): before
	// spawning OUR engine child, reap any pre-existing one. This catches an
	// orphan left by a previous serve that died UNCLEANLY (SIGKILL / crash /
	// OOM / `launchctl kickstart -k`) before its own graceful drain (this
	// supervisor's terminateChild) or the engine's own parent-death watchdog
	// (the PRIMARY layer, engine_parentwatch.go) had a chance to reap it.
	// Without this, the about-to-be-spawned NEW engine child would run
	// alongside the still-live orphan, both writing graph.fb and clobbering
	// each other's engine-liveness heartbeat (false "engine degraded" in
	// doctor). Safe no-op when engine.pid is absent/dead/not-grafel.
	reapStaleEngine(reapStaleEngineDeps{
		root:     s.layout.Root,
		readPID:  readPID,
		isAlive:  process.IsAlive,
		isGrafel: process.PidIsGrafel,
		kill:     process.Kill,
		waitDead: waitPIDDead,
	})

	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.fatalCh = make(chan error, 1)
	go s.run(ctx)
	return nil
}

// reapStaleEngineDeps abstracts the pre-spawn stale-engine reap's I/O so it
// can be unit-tested without touching real processes or a real daemon root.
// Mirrors service.sweepOrphanEngineDeps (the analogous Uninstall-time sweep)
// but keys off readPID's (int, bool) signature — the SAME helper RunEngine's
// own pidfile plumbing already uses in this package — rather than
// introducing a second (int, error) convention.
type reapStaleEngineDeps struct {
	root     string
	readPID  func(path string) (int, bool)
	isAlive  func(pid int) bool
	isGrafel func(pid int) (bool, error)
	kill     func(pid int) error
	waitDead func(pid int) // blocks briefly for pid to exit; may no-op in tests
}

// reapStaleEngine implements the SECONDARY belt-and-suspenders orphan-engine
// hardening (ADR-0024, epic #5729): serve reaps a stale/lingering engine on
// STARTUP, before spawning its own. It is intentionally conservative: any
// failure to find a live, verified-grafel pid in engine.pid (including the
// common case — it does not exist) is treated as "nothing to do", never an
// error.
//
// PID-reuse safety (mirrors sweepOrphanEngine's #5729 review fix): a stale
// engine.pid can name a pid the OS has since recycled to an unrelated
// process. Before signaling, confirm the pid is actually a grafel process;
// treat isGrafel returning an error OR false as "not ours" and skip the
// kill.
func reapStaleEngine(deps reapStaleEngineDeps) {
	if deps.root == "" {
		return
	}
	pidPath := EnginePIDPath(deps.root)
	pid, ok := deps.readPID(pidPath)
	if !ok || pid <= 0 {
		return
	}
	if !deps.isAlive(pid) {
		return
	}
	if grafelOK, gerr := deps.isGrafel(pid); gerr != nil || !grafelOK {
		return
	}
	_ = deps.kill(pid)
	if deps.waitDead != nil {
		deps.waitDead(pid)
	}
}

// reapStaleEngineWait bounds how long the SECONDARY reap waits for a
// SIGTERM'd stale engine to actually exit before serve proceeds to spawn its
// own engine child — long enough for a normal graceful shutdown, short
// enough to not meaningfully delay serve startup.
const reapStaleEngineWait = 2 * time.Second

// waitPIDDead polls process.IsAlive(pid) until it reports dead or
// reapStaleEngineWait elapses. Production implementation for
// reapStaleEngineDeps.waitDead; tests inject a no-op instead so they never
// sleep on a fake pid that (correctly) never goes dead.
func waitPIDDead(pid int) {
	deadline := time.Now().Add(reapStaleEngineWait)
	for time.Now().Before(deadline) {
		if !process.IsAlive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
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
	// spawnFailures counts CONSECUTIVE construction failures (cmd.Start
	// errors). It is reset by a SUCCESSFUL Start — the only reset a
	// construction failure can actually reach (#7087).
	spawnFailures := 0

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
			// A construction failure is NOT a crash: no child process ever
			// existed, so the crash budget's recovery rule (a child that
			// stayed up past healthyUptime) is unreachable here and the whole
			// budget would be spent re-attempting something that usually fails
			// identically every time (#7087). Distinct verb, distinct counter,
			// distinct (much smaller) budget.
			spawnFailures++
			s.logger.Error("engine supervisor: engine child spawn failed — no child process was created",
				"err", err, "consecutive_spawn_failures", spawnFailures, "max_spawn_failures", s.maxSpawnFailures)
			if spawnFailures >= s.maxSpawnFailures {
				s.giveUp("engine supervisor: giving up — engine child unspawnable",
					fmt.Errorf("engine child unspawnable: %d consecutive spawn failures, no child process was ever created: %w",
						spawnFailures, err))
				return
			}
			if s.waitBackoff(ctx, &backoff) {
				return
			}
			continue
		}
		// A child exists: whatever blocked construction has cleared, so the
		// construction budget starts over. This is the reset the crash budget
		// could never give this path.
		spawnFailures = 0
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
			s.giveUp("engine supervisor: giving up — engine unkeepable",
				fmt.Errorf("engine child crash-looping: %d consecutive relaunches at the %s backoff ceiling",
					*ceilingHits, s.backoffMax))
			return true
		}
	}
	return s.waitBackoff(ctx, backoff)
}

// giveUp records + signals the fatal that makes RunServe exit non-zero so the
// OS unit recycles it, logging msg. The error text is the artefact a user (and
// a test) reads to tell the two unkeepable verdicts apart: a crash loop (a
// child ran and died repeatedly) versus an unspawnable engine (no child was
// ever created).
func (s *engineSupervisor) giveUp(msg string, err error) {
	s.logger.Error(msg, "err", err)
	s.mu.Lock()
	s.fatalErr = err
	s.mu.Unlock()
	select {
	case s.fatalCh <- err:
	default:
	}
}

// waitBackoff sleeps for the current backoff (waking early on stop/ctx-cancel)
// and grows it toward the ceiling. It returns true when the run loop should
// exit because shutdown was requested during the wait. Shared by the crash
// path and the construction-failure path; the give-up accounting is NOT shared
// (see backoffAndMaybeGiveUp and the spawn-failure branch in run).
func (s *engineSupervisor) waitBackoff(ctx context.Context, backoff *time.Duration) (done bool) {
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
		_ = signalKill(cmd.Process)
		<-waitCh // reap
	}
	s.setChildPID(0)
}
