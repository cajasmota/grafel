package sched

// Per-module progress over the subprocess-indexer stdout IPC (#5729 follow-up).
//
// The reindex/rebuild child (`grafel index-internal`) already streams three
// coarse lifecycle lines on stdout — index_start / index_done / index_error —
// which the parent (RunSubprocessIndex) drains for logging and exit-status. That
// is enough for a background reactive reindex, but the human-awaited rebuild /
// wizard first-index needs the SAME per-module progress bars the in-process path
// produces (one row per package, live file counters, phase transitions).
//
// This file adds a second, tagged stdout line type that carries a full
// progress.Event verbatim so the parent can REPUBLISH it into the rebuild's own
// progress.Publisher (the broker / split-mode sidecar tee). The wire shape is
// progress.Event itself (it already has json tags) — we do NOT invent a new
// schema and we do NOT go through the lossy SidecarLine projection, so every
// Event field survives the process boundary byte-faithfully.
//
//	child  Indexer --WithPublisher--> StdoutProgressPublisher --stdout--> parent
//	parent parseSubprocessStdout --republish--> progressPub (broker / sidecar)

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"sync"

	"github.com/cajasmota/grafel/internal/progress"
)

// progressIPCTag is the discriminator value on the "t" field of a stdout IPC
// line that carries a republished progress.Event. Lifecycle lines
// (index_start / index_done / index_error) have no "t" field, so the parent can
// tell the two line types apart with a single field probe.
const progressIPCTag = "progress"

// progressIPCLine is one stdout IPC line carrying a per-module progress.Event
// from the index-internal child to its parent RunSubprocessIndex. The full
// Event is nested under "ev" so all of its fields (module, file counters, phase,
// bytes, current file, …) round-trip with zero loss.
type progressIPCLine struct {
	T  string         `json:"t"`
	Ev progress.Event `json:"ev"`
}

// StdoutProgressPublisher is the progress.Publisher the index-internal child
// wires via WithPublisher when the parent asked for live progress
// (SubprocessIndexOptions.ProgressPub != nil). Each published Event is marshalled
// to a tagged JSON line and written to w (the child's os.Stdout). Writes are
// serialised under a mutex so concurrent extraction-worker Publish calls never
// interleave a partial line on the shared pipe.
type StdoutProgressPublisher struct {
	mu sync.Mutex
	w  io.Writer
}

// NewStdoutProgressPublisher returns a publisher that streams progress events to
// w as tagged newline-delimited JSON. w is typically os.Stdout in the child.
func NewStdoutProgressPublisher(w io.Writer) *StdoutProgressPublisher {
	return &StdoutProgressPublisher{w: w}
}

// Publish marshals e to a tagged IPC line and writes it atomically. A marshal
// failure drops the single event (progress is advisory — never fail the index
// over a dropped tick). Never blocks the extraction hot loop beyond the single
// serialised Write.
func (p *StdoutProgressPublisher) Publish(e progress.Event) {
	b, err := json.Marshal(progressIPCLine{T: progressIPCTag, Ev: e})
	if err != nil {
		return
	}
	b = append(b, '\n')
	p.mu.Lock()
	_, _ = p.w.Write(b)
	p.mu.Unlock()
}

// parseSubprocessStdout drains the child's stdout, republishing every tagged
// progress line into progressPub (when non-nil) and returning the LAST lifecycle
// event seen (index_start / index_done / index_error) so the caller can classify
// the child's exit. Non-JSON lines and unrecognised shapes are ignored. It reads
// until r hits EOF (the child closes stdout on exit), mirroring the previous
// inline scanner.
func parseSubprocessStdout(r io.Reader, progressPub progress.Publisher, pid int, logger *slog.Logger) ipcEvent {
	var last ipcEvent
	sc := bufio.NewScanner(r)
	// A progress line for a deep monorepo path can exceed bufio's 64 KiB default
	// token cap; raise the ceiling so a long CurrentFile never truncates a line
	// into unparseable halves (which would drop that tick).
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var line struct {
			T     string          `json:"t"`
			Event string          `json:"event"`
			Repo  string          `json:"repo"`
			Ref   string          `json:"ref"`
			Error string          `json:"error"`
			Ev    json.RawMessage `json:"ev"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			continue // not a JSON line (e.g. stray child output) — ignore
		}
		if line.T == progressIPCTag {
			if progressPub != nil && len(line.Ev) > 0 {
				var ev progress.Event
				if json.Unmarshal(line.Ev, &ev) == nil {
					progressPub.Publish(ev)
				}
			}
			continue
		}
		if line.Event == "" {
			continue // neither a progress line nor a lifecycle line
		}
		last = ipcEvent{Event: line.Event, Repo: line.Repo, Ref: line.Ref, Error: line.Error}
		if logger != nil {
			logger.Info("subprocess-indexer: event", "event", last.Event, "repo", last.Repo, "ref", last.Ref)
		}
	}
	// CRITICAL (hang guard): a Scanner error — most importantly bufio.ErrTooLong
	// for a single line above the 1 MiB cap — makes Scan() return false EARLY,
	// while the child is still writing. If the drain goroutine returned here the
	// child would block on its full stdout pipe and cmd.Wait() would hang forever
	// — the exact failure class the subprocess indexer exists to kill. So on a
	// scanner error we surface it (never silently swallowed) and then keep
	// draining the pipe to EOF with io.Copy(io.Discard): a pathological oversized
	// line degrades to dropped progress, not a deadlock.
	if err := sc.Err(); err != nil {
		if logger != nil {
			logger.Warn("subprocess-indexer: stdout scan aborted; draining remainder to avoid child stall",
				"err", err, "pid", pid)
		}
		_, _ = io.Copy(io.Discard, r)
	}
	return last
}

// SubprocessIndexOptions carries the extra parameters a human-awaited rebuild /
// wizard first-index needs from RunSubprocessIndex, over and above the coarse
// background reindex the scheduler drives. All fields are optional; the nil
// *SubprocessIndexOptions (the scheduler path) preserves the exact prior
// behaviour byte-for-byte.
type SubprocessIndexOptions struct {
	// ProgressPub, when non-nil, receives every per-module progress.Event the
	// child republishes over stdout. The child only STREAMS progress when this
	// is set (the parent passes --emit-progress), so a background reindex never
	// pays the IPC cost. nil → progress lines are simply never emitted.
	ProgressPub progress.Publisher

	// GroupSlug / RepoSlug stamp the child's progress Tracker (WithProgressSlugs)
	// so republished events key the SAME (group, repo, module) rows the
	// in-process rebuild path emits. RepoSlug is also forwarded as the child's
	// --repo-tag so graph entities carry the config slug (not the dir basename).
	GroupSlug string
	RepoSlug  string

	// Interactive marks a human-awaited foreground rebuild: the child runs its
	// extract sub-subprocesses at the foreground GRAFEL_REBUILD_GOMAXPROCS cap
	// (--interactive) AND the parent sets the child process GOMAXPROCS to the
	// foreground cap instead of the throttled background reindex budget, so the
	// user is not left waiting on a background-throttled first index.
	Interactive bool

	// ForegroundGOMAXPROCS overrides the child-process GOMAXPROCS when
	// Interactive is set. 0 → resolve the GRAFEL_REBUILD_GOMAXPROCS default.
	ForegroundGOMAXPROCS int

	// IncrementalStateDir, when non-empty, enables diff-aware re-indexing in the
	// child (--incremental=<dir>), matching the in-process WithIncremental path.
	IncrementalStateDir string
}
