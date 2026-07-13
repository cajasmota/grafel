package progress

// Progress sidecar data-plane (ADR-0024 split-mode progress bridge).
//
// In split mode the engine and serve run as separate processes, each with its
// own in-memory Broker, so live index progress emitted by the engine never
// reaches serve's dashboard SSE. This file is the reusable, process-boundary-
// crossing primitive that a later PR will wire in to bridge them: the engine
// tees its Broker into a SidecarWriter that appends a per-group NDJSON file
// under GRAFEL_HOME/progress; serve tails that file with a SidecarReader and
// republishes into its own Broker.
//
// This package builds ONLY the library pieces + tests. Nothing here is wired
// into cmd/grafel or internal/daemon yet — it is intentionally dead code until
// the follow-up.
//
// Design mirrors internal/progress.BufferedPublisher (non-blocking, drop-oldest)
// and internal/statusfile (deterministic GRAFEL_HOME-derived, hashed path so a
// reader needs zero coordination with the writer).

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/registry"
)

const (
	// progressSubdir is the directory under GRAFEL_HOME holding one NDJSON file
	// per group.
	progressSubdir = "progress"

	// defaultFlushInterval is how often the background goroutine flushes the
	// coalesced latest-state map. It bounds writes to ~run_duration/interval
	// regardless of event volume — a 19k-file monorepo must not produce 19k
	// fsyncs.
	defaultFlushInterval = 200 * time.Millisecond

	// defaultSidecarMaxBytes is the size cap at which the writer compacts the
	// group file (coalesce to latest-per-key + retained terminals). Overridable
	// via GRAFEL_PROGRESS_SIDECAR_MAX_BYTES.
	defaultSidecarMaxBytes int64 = 4 << 20 // 4 MiB

	// defaultSidecarBuffer is the buffered-channel capacity between Publish and
	// the flush goroutine. Drop-oldest applies once full.
	defaultSidecarBuffer = 256
)

// SidecarLine is the on-disk NDJSON schema for one progress notification — a
// thin superset of the fields of Event that a live dashboard needs. One JSON
// object per line. Fields are additive; a reader tolerates older/newer files.
type SidecarLine struct {
	// Seq is a per-file monotonic sequence number assigned at flush time. It
	// lets a reader detect gaps and lets compaction preserve write order.
	Seq int64 `json:"seq"`

	GroupSlug string `json:"group_slug"`
	RepoSlug  string `json:"repo_slug"`
	Module    string `json:"module,omitempty"`

	Phase      string `json:"phase"`
	FilesDone  int    `json:"files_done"`
	FilesTotal int    `json:"files_total"`
	Entities   int    `json:"entities"`

	CurrentFile string `json:"current_file,omitempty"`

	TS int64 `json:"ts"`

	// Done is true for a terminal (PhaseDone / PhaseError) line — a cheap bool
	// so a reader can flag completion without re-classifying Phase. Phase
	// remains authoritative on reconstruction.
	Done  bool   `json:"done"`
	Error string `json:"error,omitempty"`
}

// lineFromEvent projects an Event onto the sidecar wire schema.
func lineFromEvent(e Event) SidecarLine {
	return SidecarLine{
		GroupSlug:   e.GroupSlug,
		RepoSlug:    e.RepoSlug,
		Module:      e.Module,
		Phase:       e.Phase,
		FilesDone:   e.FilesDone,
		FilesTotal:  e.FilesTotal,
		Entities:    e.EntitiesSoFar,
		CurrentFile: e.CurrentFile,
		TS:          e.TS,
		Done:        isTerminalPhase(e.Phase),
		Error:       e.Error,
	}
}

// toEvent reconstructs the Event carried by a sidecar line. Only the fields the
// line schema carries are recovered; the rest are zero.
func (l SidecarLine) toEvent() Event {
	return Event{
		GroupSlug:     l.GroupSlug,
		RepoSlug:      l.RepoSlug,
		Module:        l.Module,
		Phase:         l.Phase,
		FilesDone:     l.FilesDone,
		FilesTotal:    l.FilesTotal,
		EntitiesSoFar: l.Entities,
		CurrentFile:   l.CurrentFile,
		TS:            l.TS,
		Error:         l.Error,
	}
}

// coalesceKey is the (repo, module) identity a line coalesces on: within a
// flush interval only the latest line per key survives.
func (l SidecarLine) coalesceKey() string {
	return l.RepoSlug + "|" + l.Module
}

// isTerminal reports whether a line is a terminal (done/error) line.
func (l SidecarLine) isTerminal() bool {
	return l.Done || isTerminalPhase(l.Phase)
}

// SidecarPath returns the deterministic on-disk NDJSON path for groupSlug,
// GRAFEL_HOME/progress/<sha256(groupSlug)[:16]>.ndjson. It hashes the slug with
// the SAME scheme internal/statusfile.PathFor uses to hash repo paths (sha256,
// hex, first 16 chars) and resolves GRAFEL_HOME via the same registry.HomeDir
// helper, so a future reader derives the identical path with zero coordination.
func SidecarPath(groupSlug string) (string, error) {
	home, err := registry.HomeDir()
	if err != nil {
		return "", fmt.Errorf("progress: resolve home dir: %w", err)
	}
	sum := sha256.Sum256([]byte(groupSlug))
	hash := hex.EncodeToString(sum[:])[:16]
	return filepath.Join(home, progressSubdir, hash+".ndjson"), nil
}

// sidecarMaxBytes returns the effective compaction size cap, overridable via
// GRAFEL_PROGRESS_SIDECAR_MAX_BYTES.
func sidecarMaxBytes() int64 {
	if v := os.Getenv("GRAFEL_PROGRESS_SIDECAR_MAX_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return defaultSidecarMaxBytes
}

// SidecarOption configures a SidecarWriter.
type SidecarOption func(*SidecarWriter)

// WithFlushInterval overrides the coalescing flush interval.
func WithFlushInterval(d time.Duration) SidecarOption {
	return func(w *SidecarWriter) {
		if d > 0 {
			w.flushInterval = d
		}
	}
}

// WithMaxBytes overrides the compaction size cap.
func WithMaxBytes(n int64) SidecarOption {
	return func(w *SidecarWriter) {
		if n > 0 {
			w.maxBytes = n
		}
	}
}

// WithBufferSize overrides the Publish→flush channel capacity.
func WithBufferSize(n int) SidecarOption {
	return func(w *SidecarWriter) {
		if n > 0 {
			w.bufSize = n
		}
	}
}

// SidecarWriter is a non-blocking progress.Publisher that appends coalesced
// NDJSON lines to a per-group file. Non-terminal events push to a buffered
// channel (drop-oldest when full, mirroring BufferedPublisher); terminal events
// take a separate guaranteed path (see Publish) so they are never lost to a
// buffer eviction. A single background goroutine coalesces same-key events,
// flushes on a ticker, flushes terminal events immediately, assigns a monotonic
// seq per line, and compacts the file when it crosses the size cap. Publish
// never blocks the indexer.
//
// STICKY-TERMINAL contract for the consumer: a terminal (done/error) line is
// sticky per (repo, module) key — compaction lets a terminal supersede a
// non-terminal for the same key. A stale non-terminal event arriving AFTER a
// terminal (e.g. a slow repo's late tick) can still transiently flush a
// post-terminal non-terminal line; that is safe ONLY because the tailer's fold
// is monotonic — a consumer must treat a key it has already seen terminate as
// done and ignore any later non-terminal line for it.
type SidecarWriter struct {
	groupSlug     string
	path          string
	flushInterval time.Duration
	maxBytes      int64
	bufSize       int

	ch chan Event

	// pendingTerm holds terminal (done/error) lines routed OUTSIDE the lossy
	// drop-oldest channel. A terminal is emitted exactly once per repo and must
	// never be lost to a buffer eviction (a stuck wizard bar is the whole bug
	// this feature fixes), so Publish appends it here under mu and pokes wake;
	// the flush goroutine drains it on every flush. Guaranteed delivery.
	mu          sync.Mutex
	pendingTerm []SidecarLine
	wake        chan struct{}

	// reset carries a Reset request into the flush goroutine so the goroutine
	// (sole owner of the file, seq, and coalescing maps) performs truncation
	// and clears its in-memory pending state — no cross-goroutine data race and
	// a genuinely fresh stream.
	reset chan chan error

	closeOnce sync.Once
	quit      chan struct{}
	stopped   chan struct{}

	// seq is touched only by the flush goroutine.
	seq int64
}

// NewSidecarWriter constructs a writer for groupSlug and TRUNCATES the group
// file so a new run starts a fresh stream (rotation). The background flush
// goroutine starts immediately.
func NewSidecarWriter(groupSlug string, opts ...SidecarOption) (*SidecarWriter, error) {
	path, err := SidecarPath(groupSlug)
	if err != nil {
		return nil, err
	}
	w := &SidecarWriter{
		groupSlug:     groupSlug,
		path:          path,
		flushInterval: defaultFlushInterval,
		maxBytes:      sidecarMaxBytes(),
		bufSize:       defaultSidecarBuffer,
		wake:          make(chan struct{}, 1),
		reset:         make(chan chan error),
		quit:          make(chan struct{}),
		stopped:       make(chan struct{}),
	}
	for _, opt := range opts {
		opt(w)
	}
	w.ch = make(chan Event, w.bufSize)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("progress: mkdir %s: %w", filepath.Dir(path), err)
	}
	// Truncate on a new run (rotation/retention): the previous run's lines are
	// discarded so a tailing reader starting at offset 0 sees only this run.
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return nil, fmt.Errorf("progress: truncate %s: %w", path, err)
	}

	go w.run()
	return w, nil
}

// Path returns the on-disk NDJSON path this writer appends to.
func (w *SidecarWriter) Path() string { return w.path }

// Publish enqueues e without blocking.
//
// Terminal events (PhaseDone / PhaseError) take a GUARANTEED path: they are
// appended to a mutex-guarded pending slice the flush goroutine always drains,
// never the lossy channel. This is the fix for the terminal-drop bug — a
// terminal that became the FIFO head could otherwise be evicted by drop-oldest
// once >bufSize newer events piled up behind a stalled goroutine (e.g. a fast
// repo finishing while slow repos keep streaming), leaving the wizard bar stuck
// forever.
//
// Non-terminal events go through the buffered channel with drop-oldest when
// full, matching BufferedPublisher's backpressure discipline so the indexer
// never stalls. (Coalescing means a dropped mid-stream tick is harmless — the
// next tick carries the latest state.)
func (w *SidecarWriter) Publish(e Event) {
	if isTerminalPhase(e.Phase) {
		l := lineFromEvent(e)
		w.mu.Lock()
		w.pendingTerm = append(w.pendingTerm, l)
		w.mu.Unlock()
		// Poke the goroutine to flush promptly; non-blocking because wake has a
		// capacity of 1 and a pending poke already guarantees a wake-up.
		select {
		case w.wake <- struct{}{}:
		default:
		}
		return
	}
	select {
	case w.ch <- e:
	default:
		select {
		case <-w.ch:
		default:
		}
		select {
		case w.ch <- e:
		default:
		}
	}
}

// Reset truncates the group file and clears the flush goroutine's in-memory
// coalescing state and seq counter, starting a genuinely fresh stream without
// tearing down the goroutine. The truncation and state clear happen INSIDE the
// goroutine (which solely owns the file, seq, and maps) so there is no data
// race and no stale pending line leaks into the new run. Safe to call between
// runs that reuse the same writer.
func (w *SidecarWriter) Reset() error {
	done := make(chan error, 1)
	select {
	case w.reset <- done:
		return <-done
	case <-w.stopped:
		// Goroutine already gone (post-Close): truncate directly as a fallback.
		return os.WriteFile(w.path, nil, 0o600)
	}
}

// Close flushes any buffered/coalesced state and stops the background
// goroutine, joining it before returning (no goroutine leak).
func (w *SidecarWriter) Close() error {
	w.closeOnce.Do(func() { close(w.quit) })
	<-w.stopped
	return nil
}

// run is the single flush goroutine. It owns all file writes and the seq
// counter, so Publish (channel send only) is race-free against it.
func (w *SidecarWriter) run() {
	defer close(w.stopped)

	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	latest := make(map[string]SidecarLine)

	// flush drains the guaranteed pending-terminal slice (populated by Publish
	// under mu) together with the coalesced latest-per-key state and writes them
	// in one append.
	flush := func() {
		w.mu.Lock()
		terms := w.pendingTerm
		w.pendingTerm = nil
		w.mu.Unlock()
		w.flush(latest, terms)
		latest = make(map[string]SidecarLine)
	}

	ingest := func(e Event) {
		l := lineFromEvent(e)
		if l.isTerminal() {
			// Terminals normally arrive via pendingTerm (Publish routes them
			// there); tolerate one that reached the channel anyway and give it
			// the same guaranteed, flush-now treatment.
			w.mu.Lock()
			w.pendingTerm = append(w.pendingTerm, l)
			w.mu.Unlock()
			flush()
			return
		}
		latest[l.coalesceKey()] = l
	}

	// drainChannel ingests every event currently buffered on the channel
	// (non-blocking). Used before a terminal flush so the terminal frame
	// includes the non-terminal ticks published just before it, and on quit.
	drainChannel := func() {
		for {
			select {
			case e := <-w.ch:
				ingest(e)
			default:
				return
			}
		}
	}

	for {
		select {
		case e := <-w.ch:
			ingest(e)
		case <-w.wake:
			// A terminal was routed through pendingTerm. Drain any non-terminals
			// buffered before it so they share the terminal's flush, then flush
			// immediately so completion is never delayed by the ticker.
			drainChannel()
			flush()
		case done := <-w.reset:
			// Truncate + clear ALL in-memory state inside the goroutine that
			// owns the file, seq, and maps: a genuinely fresh stream. This
			// includes draining any still-buffered non-terminal events from the
			// channel — otherwise events queued before Reset would ingest after
			// it and leak stale state into the new run.
		drain:
			for {
				select {
				case <-w.ch:
				default:
					break drain
				}
			}
			err := os.WriteFile(w.path, nil, 0o600)
			latest = make(map[string]SidecarLine)
			w.mu.Lock()
			w.pendingTerm = nil
			w.mu.Unlock()
			w.seq = 0
			done <- err
		case <-ticker.C:
			flush()
		case <-w.quit:
			// Drain anything still buffered, then flush and exit. The final
			// flush also drains any pending terminal so a Done published just
			// before Close is never lost.
			for {
				select {
				case e := <-w.ch:
					ingest(e)
				default:
					flush()
					return
				}
			}
		}
	}
}

// flush appends the coalesced latest-per-key lines (in stable key order)
// followed by the pending terminal lines, assigning a monotonic seq to each.
// It then compacts the file if it has grown past the size cap.
func (w *SidecarWriter) flush(latest map[string]SidecarLine, terminals []SidecarLine) {
	if len(latest) == 0 && len(terminals) == 0 {
		return
	}
	keys := make([]string, 0, len(latest))
	for k := range latest {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	emit := func(l SidecarLine) {
		w.seq++
		l.Seq = w.seq
		b, err := json.Marshal(l)
		if err != nil {
			return
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	for _, k := range keys {
		emit(latest[k])
	}
	for _, l := range terminals {
		emit(l)
	}
	if buf.Len() == 0 {
		return
	}

	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write(buf.Bytes())
	_ = f.Close()

	if st, err := os.Stat(w.path); err == nil && st.Size() > w.maxBytes {
		// Best-effort: a failed compaction just leaves the (still-correct,
		// larger) file in place.
		_ = compactSidecarFile(w.path)
	}
}

// compactSidecarFile rewrites path in place (tmp+rename) coalescing it to the
// latest line per (repo, module) key plus every terminal line, preserving seq
// order. This bounds a long run's file to O(#keys) regardless of tick volume.
// A torn last line is tolerated (skipped). Non-existent file is a no-op.
func compactSidecarFile(path string) error {
	lines, _, err := readSidecarLines(path, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	latest := make(map[string]SidecarLine)   // non-terminal, latest per key
	terminal := make(map[string]SidecarLine) // terminal, latest per key
	for _, l := range lines {
		if l.isTerminal() {
			terminal[l.coalesceKey()] = l
		} else {
			latest[l.coalesceKey()] = l
		}
	}

	kept := make([]SidecarLine, 0, len(latest)+len(terminal))
	for k, l := range latest {
		// A terminal line for the same key supersedes a non-terminal one.
		if _, done := terminal[k]; done {
			continue
		}
		kept = append(kept, l)
	}
	for _, l := range terminal {
		kept = append(kept, l)
	}
	// Preserve seq (write) order.
	sort.Slice(kept, func(i, j int) bool { return kept[i].Seq < kept[j].Seq })

	var buf bytes.Buffer
	for _, l := range kept {
		b, err := json.Marshal(l)
		if err != nil {
			continue
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}

	dir := filepath.Dir(path)
	tmpf, err := os.CreateTemp(dir, filepath.Base(path)+".compact-*")
	if err != nil {
		return err
	}
	tmp := tmpf.Name()
	if _, err := tmpf.Write(buf.Bytes()); err != nil {
		_ = tmpf.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := tmpf.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// PruneTerminalSidecars deletes group files whose stream has terminated (last
// complete line is a terminal event) AND whose mtime is older than maxAge. A
// live (non-terminal) or fresh file is left untouched. Returns the number of
// files deleted. Best-effort: a single unreadable file is skipped, not fatal.
func PruneTerminalSidecars(maxAge time.Duration) (int, error) {
	home, err := registry.HomeDir()
	if err != nil {
		return 0, fmt.Errorf("progress: resolve home dir: %w", err)
	}
	dir := filepath.Join(home, progressSubdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	cutoff := time.Now().Add(-maxAge)
	deleted := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".ndjson" {
			continue
		}
		full := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		lines, _, err := readSidecarLines(full, 0)
		if err != nil || len(lines) == 0 {
			continue
		}
		if !lines[len(lines)-1].isTerminal() {
			continue
		}
		if err := os.Remove(full); err == nil {
			deleted++
		}
	}
	return deleted, nil
}
