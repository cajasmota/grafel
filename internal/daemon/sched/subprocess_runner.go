package sched

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
)

// SubprocessIndexEnabled reports whether the daemon should run each
// reindex job as a short-lived child process (S5 of issue #2155) instead
// of calling the Index function in-process.
//
// Default logic (resource-safe defaults, v0.1.1):
//   - GRAFEL_SUBPROCESS_INDEXER=false/0/no → always OFF (opt-out)
//   - GRAFEL_SUBPROCESS_INDEXER=true/1/yes → always ON
//   - unset                                 → ON (default)
//
// Why default ON: the in-process path runs the reindex at the daemon's own
// GOMAXPROCS (= host core count) with no per-job CPU bound — the runaway the
// dogfooding report observed (300–998% CPU, ~10 cores, for 10–20 min per
// push). The subprocess path forks `grafel index-internal`, which the
// extract coordinator bounds to GRAFEL_EXTRACT_GOMAXPROCS (default 2) cores
// per child, so background reindexes cannot saturate the host on a fresh
// `curl|bash` install that sets no env vars. It also keeps the daemon heap
// flat (the original #2155 motivation). Operators who need the legacy
// in-process behaviour can still force it with GRAFEL_SUBPROCESS_INDEXER=0.
//
// The env var is read once at program start via init() to avoid per-call
// os.Getenv overhead in the hot admission loop.
var subprocessIndexerEnabled atomic.Bool

func init() {
	subprocessIndexerEnabled.Store(subprocessIndexEnabledFromEnv())
}

// subprocessIndexEnabledFromEnv resolves the default-on toggle from the
// process environment. Unset → ON; an explicit falsy value → OFF; any other
// value → ON. Exposed (lower-case) so tests can re-resolve after t.Setenv.
func subprocessIndexEnabledFromEnv() bool {
	v := strings.TrimSpace(os.Getenv("GRAFEL_SUBPROCESS_INDEXER"))
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	default:
		// "", "1", "true", "yes", or anything else → default ON.
		return true
	}
}

// SubprocessIndexEnabled returns the current subprocess-indexer toggle
// value. Exposed for testing and the daemon status endpoint.
func SubprocessIndexEnabled() bool {
	return subprocessIndexerEnabled.Load()
}

// ipcEvent is one JSON line emitted by the child process on stdout.
type ipcEvent struct {
	Event string `json:"event"`
	Repo  string `json:"repo,omitempty"`
	Ref   string `json:"ref,omitempty"`
	Error string `json:"error,omitempty"`
}

// RunSubprocessIndex forks `grafel index --internal` for a single
// reindex job and waits for it to exit. The daemon stays at ~5MB extra
// overhead per in-flight reindex (IPC reader goroutine + wait state).
//
// Arguments:
//
//	ctx        — cancelled when the daemon wants to abort the job
//	repoPath   — absolute path of the repository
//	ref        — git ref captured at enqueue time (may be "")
//	skipPasses — pass names forwarded via --skip-pass
//	logger     — daemon's slog.Logger for structured event lines
//
// The child's stderr is copied line-by-line to logger (prefixed with the
// repo basename) so the daemon log file includes child extractor output
// without growing the daemon's own heap.
//
// Cancellation: ctx.Done() sends SIGTERM to the child. The child is
// expected to exit on SIGTERM; if it does not, the parent waits and the
// context timeout (if any) will eventually unblock the caller.
func RunSubprocessIndex(ctx context.Context, repoPath, ref string, skipPasses []string, logger *slog.Logger) error {
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("subprocess-indexer: resolve binary: %w", err)
	}

	args := []string{
		"index-internal",
		"--repo=" + repoPath,
		"--ref=" + ref,
	}
	if len(skipPasses) > 0 {
		args = append(args, "--skip-pass="+strings.Join(skipPasses, ","))
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	// Daemon's state dirs are inherited via the env (GRAFEL_DAEMON_ROOT,
	// GRAFEL_HOME). Do NOT set cmd.Env explicitly so the child inherits
	// the daemon's full environment.
	cmd.Env = os.Environ()

	// Pipe child stdout for IPC JSON lines.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("subprocess-indexer: stdout pipe: %w", err)
	}
	// Pipe child stderr for log forwarding.
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("subprocess-indexer: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("subprocess-indexer: start: %w", err)
	}

	pid := cmd.Process.Pid
	if logger != nil {
		logger.Info("subprocess-indexer: started", "pid", pid, "repo", repoPath, "ref", ref)
	}

	// Drain child stderr in a goroutine — each line forwarded to the daemon
	// log. This goroutine exits naturally when the child closes stderr (on
	// normal exit or crash).
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		sc := bufio.NewScanner(stderrPipe)
		for sc.Scan() {
			if logger != nil {
				logger.Info("[child]", "pid", pid, "line", sc.Text())
			}
		}
	}()

	// Drain child stdout for IPC events.
	var lastEvent ipcEvent
	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		sc := bufio.NewScanner(stdoutPipe)
		for sc.Scan() {
			var ev ipcEvent
			if jerr := json.Unmarshal([]byte(sc.Text()), &ev); jerr != nil {
				continue // not a JSON line — ignore
			}
			lastEvent = ev
			if logger != nil {
				logger.Info("subprocess-indexer: event", "event", ev.Event, "repo", ev.Repo, "ref", ev.Ref)
			}
		}
	}()

	// Wait for both pipe goroutines and the process itself.
	<-stdoutDone
	<-stderrDone
	waitErr := cmd.Wait()

	if logger != nil {
		if waitErr != nil {
			logger.Error("subprocess-indexer: exited with error", "pid", pid, "err", waitErr)
		} else {
			logger.Info("subprocess-indexer: completed successfully", "pid", pid)
		}
	}

	if waitErr != nil {
		// Distinguish context-cancellation (SIGTERM was sent by us) from a
		// genuine child failure.
		if ctx.Err() != nil {
			return fmt.Errorf("subprocess-indexer: cancelled: %w", ctx.Err())
		}
		if lastEvent.Error != "" {
			return errors.New(lastEvent.Error)
		}
		return fmt.Errorf("subprocess-indexer: child exit: %w", waitErr)
	}
	return nil
}
