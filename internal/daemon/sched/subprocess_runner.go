package sched

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
)

// SubprocessIndexEnabled reports whether the daemon should run each
// reindex job as a short-lived child process (S5 of issue #2155) instead
// of calling the Index function in-process.
//
// Default logic (gradual rollout):
//   - ARCHIGRAPH_SUBPROCESS_INDEXER=true/1  → always ON
//   - ARCHIGRAPH_SUBPROCESS_INDEXER=false/0 → always OFF
//   - unset                                 → OFF (existing installs keep old behaviour;
//     new installs set it to "true" during `archigraph install`)
//
// The env var is read once at program start via init() to avoid per-call
// os.Getenv overhead in the hot admission loop.
var subprocessIndexerEnabled atomic.Bool

func init() {
	v := strings.TrimSpace(os.Getenv("ARCHIGRAPH_SUBPROCESS_INDEXER"))
	on := v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	subprocessIndexerEnabled.Store(on)
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

// RunSubprocessIndex forks `archigraph index --internal` for a single
// reindex job and waits for it to exit. The daemon stays at ~5MB extra
// overhead per in-flight reindex (IPC reader goroutine + wait state).
//
// Arguments:
//
//	ctx        — cancelled when the daemon wants to abort the job
//	repoPath   — absolute path of the repository
//	ref        — git ref captured at enqueue time (may be "")
//	skipPasses — pass names forwarded via --skip-pass
//	logger     — daemon's log.Logger for structured event lines
//
// The child's stderr is copied line-by-line to logger (prefixed with the
// repo basename) so the daemon log file includes child extractor output
// without growing the daemon's own heap.
//
// Cancellation: ctx.Done() sends SIGTERM to the child. The child is
// expected to exit on SIGTERM; if it does not, the parent waits and the
// context timeout (if any) will eventually unblock the caller.
func RunSubprocessIndex(ctx context.Context, repoPath, ref string, skipPasses []string, logger *log.Logger) error {
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
	// Daemon's state dirs are inherited via the env (ARCHIGRAPH_DAEMON_ROOT,
	// ARCHIGRAPH_HOME). Do NOT set cmd.Env explicitly so the child inherits
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
		logger.Printf("subprocess-indexer: started pid=%d repo=%s ref=%s", pid, repoPath, ref)
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
				logger.Printf("[child pid=%d] %s", pid, sc.Text())
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
				logger.Printf("subprocess-indexer: event=%s repo=%s ref=%s", ev.Event, ev.Repo, ev.Ref)
			}
		}
	}()

	// Wait for both pipe goroutines and the process itself.
	<-stdoutDone
	<-stderrDone
	waitErr := cmd.Wait()

	if logger != nil {
		if waitErr != nil {
			logger.Printf("subprocess-indexer: pid=%d exited with error: %v", pid, waitErr)
		} else {
			logger.Printf("subprocess-indexer: pid=%d completed successfully", pid)
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
