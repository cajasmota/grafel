// Package statusfile is the engine↔consumer status-plane sidecar (#5725
// core, #5729-W1). It exists so a statusline / CLI / future serve process can
// answer "what is grafel doing for THIS repo right now?" with a fast,
// non-blocking, always-terminating file read — never a daemon RPC that can
// block behind an in-flight index.
//
// The daemon (engine) is the SOLE writer: it atomically (tmp+rename) writes
// one small JSON file per repo to
//
//	$GRAFEL_HOME/status/<sha256(abs_repo_path)[:16]>.json
//
// (GRAFEL_HOME defaults to ~/.grafel; honors the same override as
// internal/registry.HomeDir). The file is updated whenever that repo's index
// state changes (start/complete/dirty) and on a periodic heartbeat so a
// reader can also detect an engine that died mid-index (stale HeartbeatAt).
//
// A reader (grafel status --json, a statusline, or the future standalone
// `serve` process per ADR-0024) calls Read, which does a single os.ReadFile —
// no socket dial, no lock, no RPC. If the file is absent or stale the reader
// falls back to "unknown" rather than hanging.
package statusfile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/registry"
)

// File is the on-disk status-plane schema for one repo. Fields are additive
// only — a reader must tolerate a file written by an older engine version
// missing newer fields (all are zero-valued, not an error).
type File struct {
	// EnginePID is the process ID of the daemon that wrote this file. Lets a
	// reader detect a stale file left behind by a since-restarted daemon
	// (compare against the currently-running daemon's pid, e.g. via the
	// pidfile) without needing a live RPC.
	EnginePID int `json:"engine_pid"`
	// HeartbeatAt is the wall-clock time this file was last written — either
	// because this repo's index state changed, or the periodic heartbeat
	// tick fired. A reader treats a HeartbeatAt older than a few missed
	// heartbeat intervals as "engine may be gone" and falls back to
	// "unknown" rather than trusting stale data.
	HeartbeatAt time.Time `json:"heartbeat_at"`
	// Version is the engine's self-reported version (mirrors proto.PingReply.Version).
	Version string `json:"version"`

	// RepoPath is the absolute on-disk path this file describes.
	RepoPath string `json:"repo_path"`
	// IndexedRef is the git ref (branch/tag) the on-disk graph reflects.
	IndexedRef string `json:"indexed_ref,omitempty"`
	// IndexedCommit is the exact commit SHA the on-disk graph reflects
	// (#5727) — short form; the fuller identifier a status-plane consumer
	// typically wants for an at-a-glance freshness check.
	IndexedCommit string `json:"indexed_commit,omitempty"`
	// Entities / Relationships are the graph's persisted counts (cheap
	// header-derived values — see graph.PersistedStatsFromDir), not a full
	// decode.
	Entities      int64 `json:"entities"`
	Relationships int64 `json:"relationships"`
	// GraphFBMtime is the on-disk graph.fb file's modification time
	// (UnixNano), letting a reader detect a graph that was rewritten after
	// this status file was last refreshed.
	GraphFBMtime int64 `json:"graph_fb_mtime,omitempty"`

	// Indexing is true ONLY while the graph for RepoPath is NOT yet queryable —
	// the EXTRACTION phase of an in-flight index, before the first queryable
	// graph.fb is written for this run. It flips to false at the
	// extraction→enrichment boundary (see Enhancing). A reader that wants "is
	// the graph usable yet?" should treat indexing=true as "not yet".
	Indexing bool `json:"indexing"`
	// Enhancing is true while the graph IS queryable but the long background
	// ENRICHMENT tail (cross-repo links, flows, group algorithms, warming) for
	// the current index run is still running. indexing=false && enhancing=true
	// means "queryable now, still improving in the background" — a terminal
	// SUCCESS for completion classifiers, never a failure. Both false means
	// idle/done. The pair is never simultaneously true.
	Enhancing bool `json:"enhancing,omitempty"`
	// QueueLen is the number of index jobs queued behind this repo (or,
	// process-wide, the scheduler's queue depth — see the writer for which
	// scope it publishes).
	QueueLen int `json:"queue_len,omitempty"`
	// LastErr is the most recent index error for this repo, or "" if the
	// last completed index succeeded.
	LastErr string `json:"last_err,omitempty"`

	// LastRebuildFailure records the most recent FAILED `grafel rebuild` for
	// this repo — e.g. the per-repo watchdog SIGKILL (#5143's
	// defaultPerRepoRebuildTimeout) or any other hard rebuild failure — so a
	// watchdog firing is never silent (#5822 sub-ask 3). Before this field
	// existed, a watchdog timeout discarded the result entirely: nothing was
	// persisted, so `grafel status` kept showing the PREVIOUS (stale) index
	// with no error or warning, and the only trace was daemon.err.
	//
	// This is an ADDITIONAL marker, never a replacement for the last-good
	// graph: a repo can have both a fresh, queryable graph.fb from an OLDER
	// successful run AND a LastRebuildFailure from a more recent failed
	// attempt. It is nil when no rebuild has ever failed, or once cleared by
	// a subsequent SUCCESSFUL rebuild of this repo (see
	// internal/daemon.ClearRebuildFailure) — a stale FAILED line must never
	// persist after a good rebuild.
	LastRebuildFailure *RebuildFailure `json:"last_rebuild_failure,omitempty"`

	// State mirrors indexstate.RepoState.State (one of "current", "queued",
	// "indexing", "dirty") — added #5729 PR3 so grafel_index_status can be
	// reconstructed by a serve process with no in-process scheduler. Empty on
	// a file written by a pre-PR3 engine; a tolerant reader treats "" the same
	// as StateCurrent (a repo with a materialized graph and no known live
	// state is not indexing).
	State string `json:"state,omitempty"`
	// HeadRef is the ref captured at the latest enqueue (the ref the pending/
	// in-flight work targets), or empty when nothing is pending. Mirrors
	// indexstate.RepoState.HeadRef.
	HeadRef string `json:"head_ref,omitempty"`
	// Dirty is true when a coalesced follow-up reindex is pending (#5138).
	// Mirrors indexstate.RepoState.Dirty.
	Dirty bool `json:"dirty,omitempty"`

	// --- Engine-global fields (#5729 PR3) ---
	// These are only meaningful on the ENGINE-LIVENESS sidecar (the file keyed
	// on the daemon root, not on any single repo — see
	// internal/daemon.engineLivenessStatusKey) and are the process-wide
	// superset a serve process needs to answer grafel_index_status's
	// concurrency/parsing/busy fields and grafel_whoami/grafel_status's
	// warming fields WITHOUT touching the engine's in-process scheduler
	// memory. They are additive/omitempty so a per-repo file (which never
	// sets them) round-trips unchanged.

	// EngineStartedAt is the wall-clock time the writing engine process
	// booted (captured once, not on every heartbeat tick).
	EngineStartedAt time.Time `json:"engine_started_at,omitempty"`
	// Busy mirrors indexstate.Snapshot.Busy: an index job, a group-algo pass,
	// OR an in-process parse is running somewhere in the engine.
	Busy bool `json:"busy,omitempty"`
	// ParseInFlight mirrors indexstate.Snapshot.ParseInFlight (#5630).
	ParseInFlight int `json:"parse_in_flight,omitempty"`
	// EngineInFlight mirrors indexstate.Snapshot.InFlight (index-job count).
	EngineInFlight int `json:"engine_in_flight,omitempty"`
	// EngineGroupAlgoInFlight mirrors indexstate.Snapshot.GroupAlgoInFlight.
	EngineGroupAlgoInFlight int `json:"engine_group_algo_in_flight,omitempty"`
	// EngineBusyStartedAt mirrors indexstate.Snapshot.StartedAt (zero when idle).
	EngineBusyStartedAt time.Time `json:"engine_busy_started_at,omitempty"`
	// ConcurrencyActive/Queued/Cap mirror indexstate.IndexConcurrency (#5493).
	ConcurrencyActive int `json:"concurrency_active,omitempty"`
	ConcurrencyQueued int `json:"concurrency_queued,omitempty"`
	ConcurrencyCap    int `json:"concurrency_cap,omitempty"`
	// WarmIndexInFlight/WarmPendingAlgo/WarmPendingLinks mirror
	// daemon.WarmingSnapshot (#5690) — the fields grafel_whoami/grafel_status
	// use to report "warming: post-index enrichment in flight".
	WarmIndexInFlight bool `json:"warm_index_in_flight,omitempty"`
	WarmPendingAlgo   int  `json:"warm_pending_algo,omitempty"`
	WarmPendingLinks  int  `json:"warm_pending_links,omitempty"`

	// --- Process metrics (wizard CPU/RAM readout) ---
	// These are only meaningful on the ENGINE-LIVENESS sidecar (same scope as
	// the Engine-global fields above): RSS/CPU are per-PROCESS, not per-repo,
	// so a per-repo status file never sets them. Populated by whichever
	// process actually runs the index — the standalone `grafel engine` child
	// in split mode (the DEFAULT), or the monolith daemon process itself when
	// split mode is disabled — from its OWN process stats
	// (internal/process.RSSBytes / CPUPercent) on every heartbeat write. A
	// reader (e.g. the wizard's index-progress TUI) uses these to show a live
	// "CPU / RAM" readout during a long enrichment phase so the overall
	// progress bar sitting near 100% doesn't look stuck. Both are
	// best-effort: a platform/measurement failure leaves them at zero, and a
	// tolerant reader must omit the readout entirely rather than render a
	// misleading "0%" / "0.0 GB".

	// RSSMB is the writing process's resident-set size in megabytes.
	RSSMB int64 `json:"rss_mb,omitempty"`
	// CPUPct is the writing process's instantaneous CPU percent (per
	// internal/process.CPUPercent — can exceed 100% on a multi-threaded
	// process using more than one core, matching `ps`/Activity Monitor
	// convention). Best-effort: 0 when unavailable (e.g. no platform
	// implementation) — a reader must not treat 0 as "idle", only as
	// "unknown", and should omit the CPU portion of the readout when zero.
	CPUPct float64 `json:"cpu_pct,omitempty"`
}

// RebuildFailure is the "last rebuild FAILED" marker recorded onto a per-repo
// File (see File.LastRebuildFailure) — #5822 sub-ask 3. It surfaces a hard
// rebuild failure (most notably the per-repo watchdog SIGKILL, #5143) that
// would otherwise be discarded silently, leaving `grafel status` showing a
// stale index with no indication anything went wrong.
type RebuildFailure struct {
	// Reason is a short human-readable cause, e.g. "repo index timed out
	// after 30m0s (still running in background)" — the same message surfaced
	// to daemon.err at the moment the watchdog fired.
	Reason string `json:"reason"`
	// At is the wall-clock time the failure was recorded.
	At time.Time `json:"at"`
	// Ref is the git ref the failed rebuild was targeting, when known.
	Ref string `json:"ref,omitempty"`
	// Commit is the abbreviated commit SHA the failed rebuild was targeting,
	// when known.
	Commit string `json:"commit,omitempty"`
}

// statusSubdir is the directory name under GRAFEL_HOME holding one file per
// repo.
const statusSubdir = "status"

// readFile is os.ReadFile, indirected so a test can inject a transient
// failure (e.g. a simulated Windows sharing violation) without touching real
// disk I/O — see readFileWithRetry.
var readFile = os.ReadFile

// isRetryableReplaceErrorFn defaults to the platform-specific
// isRetryableReplaceError (see sharing_violation_windows.go /
// sharing_violation_other.go) but is a var so a darwin/linux test can inject a
// fake classifier and exercise both readFileWithRetry's and renameWithRetry's
// retry control flow without needing a real Windows syscall error. Both the
// read side (a concurrent os.Open racing a rename's replace) and the write
// side (os.Rename replacing a file a reader holds open) share this one
// classifier: on Windows both surface the same transient "replace an open
// file" errors, and on POSIX it is a no-op so neither path retries.
var isRetryableReplaceErrorFn = isRetryableReplaceError

// renameFn is os.Rename, indirected so a test can inject a transient failure
// (e.g. a simulated Windows ERROR_ACCESS_DENIED) without needing a real
// syscall error — see renameWithRetry.
var renameFn = os.Rename

// renameRetrySleep is time.Sleep, indirected so a test can make the backoff a
// no-op and exercise renameWithRetry's control flow without wall-clock delay.
var renameRetrySleep = time.Sleep

// readRetryAttempts/readRetryBackoff bound the retry Read performs when its
// os.Open hits a transient replace-open-file error (Windows only — see
// isRetryableReplaceError). This budget is SYMMETRIC with the write side's
// renameRetry* budget below because both race the SAME NTFS replace window from
// opposite ends: while a writer's os.Rename replaces the target, a concurrent
// reader's os.Open sees ERROR_SHARING_VIOLATION ("the process cannot access the
// file because it is being used by another process"). Under contention (the
// 8-writer + looping-reader stress in TestWrite_ConcurrentSameRepo_NoTornRead,
// amplified now that writes themselves retry ~190ms and keep the target under
// near-constant rename churn) a short 20ms budget occasionally exhausts and
// surfaces a spurious error, so the reader must be as patient as the writer.
//
// 20 attempts × a small 10ms fixed backoff = 19 sleeps × 10ms = 190ms
// worst-case total blocking before a persistent read error surfaces. This costs
// NOTHING in the normal case: with no concurrent write the very first os.Open
// succeeds and returns immediately (zero added latency on the statusline hot
// path). The extended budget is only ever consumed while a write is actively
// churning the target — a condition that resolves within that window — and a
// genuine persistent error (e.g. a real permission failure) still surfaces
// after the bounded budget rather than looping forever. Kept as SEPARATE named
// constants from renameRetry* (not re-coupled) even though they land on the
// same total, so the two budgets can be tuned independently.
const (
	readRetryAttempts = 20
	readRetryBackoff  = 10 * time.Millisecond
)

// renameRetryAttempts/renameRetryBackoff bound the retry Write performs when
// its atomic-publish os.Rename hits a transient replace-open-file error
// (Windows only — see isRetryableReplaceError). These are DELIBERATELY DECOUPLED
// from the read-side readRetry* budget and are much larger: the read path is a
// single microsecond-scale ReadFile, but the write-side rename must OUTLAST a
// concurrent reader that reopens the target in a tight loop (exactly what
// TestWrite_ConcurrentSameRepo_NoTornRead does: 8 writers + a looping reader on
// windows-latest). The rename can only land in the gap between the reader's
// close and its next open, so we must stay patient long enough to catch one of
// those gaps. 20 attempts × a small 10ms fixed backoff (no single sleep large)
// gives a worst-case total blocking time of 19 sleeps × 10ms = 190ms before a
// persistently failing rename returns the last error — patient enough to
// reliably span a reopen loop's gaps on NTFS, yet firmly BOUNDED so the
// daemon's single serialized statusWriter goroutine (the only caller of Write)
// can never stall by more than ~190ms. On POSIX the classifier is a no-op and
// the loop makes exactly one os.Rename call (unchanged behavior).
const (
	renameRetryAttempts = 20
	renameRetryBackoff  = 10 * time.Millisecond
)

// readFileWithRetry wraps readFile with a short bounded retry when the error
// is a transient replace-open-file error. On POSIX (darwin/linux) Write's
// tmp+rename is atomic at the filesystem level and a concurrent Read never
// observes a sharing conflict, so isRetryableReplaceError always returns
// false there and this degenerates to a single readFile call — identical
// behavior to before this change. On Windows/NTFS, os.Rename's replace can
// transiently deny a concurrent os.Open with ERROR_SHARING_VIOLATION; retrying
// with a small backoff over a bounded budget (readRetryAttempts, ~190ms worst
// case — symmetric with the write side) lets the real status-plane poller ride
// out that window instead of surfacing a spurious failure.
//
// NOTE: the shared isRetryableReplaceError classifier was broadened for the
// write path to also match ERROR_ACCESS_DENIED, so Read INTENTIONALLY inherits
// it too (previously Read retried only on ERROR_SHARING_VIOLATION). This is
// deliberate and benign: a transient ACCESS_DENIED on a concurrent os.Open of a
// same-user 0o600 status file is the same "reader raced the rename's replace"
// window, so retrying it is strictly an improvement — never a way to mask a
// genuine permission error, which would persist past the bounded retry and
// still surface to the caller.
func readFileWithRetry(path string) ([]byte, error) {
	var data []byte
	var err error
	for attempt := 0; attempt < readRetryAttempts; attempt++ {
		data, err = readFile(path)
		if err == nil || !isRetryableReplaceErrorFn(err) {
			return data, err
		}
		if attempt < readRetryAttempts-1 {
			time.Sleep(readRetryBackoff)
		}
	}
	return data, err
}

// renameWithRetry is the write-side counterpart of readFileWithRetry (with its
// own, larger renameRetry* budget — see above): it wraps the atomic-publish
// rename with a bounded retry when the error is a transient replace-open-file
// error. On POSIX os.Rename replaces the destination even while a reader holds
// it open, so isRetryableReplaceError always returns false and this makes
// exactly one renameFn call — identical behavior to a bare os.Rename. On
// Windows/NTFS, renaming over a file a concurrent reader holds open can fail
// with ERROR_ACCESS_DENIED (or ERROR_SHARING_VIOLATION); retrying with a small
// backoff lets the publish land in a gap of the reader's reopen loop instead of
// surfacing a spurious hard error. A non-retryable error (e.g. a cross-device
// rename) returns immediately, and the loop is bounded (worst case ~190ms — see
// renameRetryAttempts) so a writer can never hang.
func renameWithRetry(from, to string) error {
	var err error
	for attempt := 0; attempt < renameRetryAttempts; attempt++ {
		err = renameFn(from, to)
		if err == nil || !isRetryableReplaceErrorFn(err) {
			return err
		}
		if attempt < renameRetryAttempts-1 {
			renameRetrySleep(renameRetryBackoff)
		}
	}
	return err
}

// PathFor returns the deterministic on-disk path for repoPath's status file.
// The same repoPath always maps to the same path (sha256-hashed so the file
// name is filesystem-safe and length-bounded regardless of the repo path's
// own length/characters) so a writer and a reader agree without any
// coordination beyond both resolving GRAFEL_HOME the same way.
func PathFor(repoPath string) (string, error) {
	home, err := registry.HomeDir()
	if err != nil {
		return "", fmt.Errorf("statusfile: resolve home dir: %w", err)
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		abs = repoPath
	}
	abs = filepath.Clean(abs)
	sum := sha256.Sum256([]byte(abs))
	hash := hex.EncodeToString(sum[:])[:16]
	return filepath.Join(home, statusSubdir, hash+".json"), nil
}

// Write atomically (tmp+rename) persists f as repoPath's status file.
// RepoPath and HeartbeatAt are stamped onto f by the caller before Write is
// called (Write does not mutate f) — callers should generally set
// HeartbeatAt to time.Now().UTC() immediately before calling Write.
func Write(repoPath string, f *File) error {
	path, err := PathFor(repoPath)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("statusfile: mkdir %s: %w", dir, err)
	}

	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("statusfile: marshal: %w", err)
	}

	// Atomic write: temp file in the SAME directory (so rename is same-fs),
	// then rename over the final path. A concurrent Read always sees either
	// the previous complete file or the new complete file, never a partial
	// write — this is the guarantee a poll-safe reader depends on.
	//
	// The temp file MUST have a unique name (os.CreateTemp's random suffix),
	// NOT a fixed path+".tmp": two concurrent Writes to the same repo would
	// otherwise both O_TRUNC and interleave into the SAME tmp inode, then each
	// rename a torn/garbled file into place. Rename is atomic, but it would be
	// publishing corruption. The daemon serializes its own writes via the
	// single coalescing statusWriter goroutine (see internal/daemon), but a
	// unique tmp name makes Write correct under concurrency regardless of who
	// calls it — belt and suspenders (review #5734).
	tmpf, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("statusfile: create tmp: %w", err)
	}
	tmp := tmpf.Name()
	// On any failure past this point, remove the orphan tmp so a crashed/
	// racing writer never litters the status dir.
	cleanup := func() { _ = os.Remove(tmp) }
	if _, err := tmpf.Write(data); err != nil {
		_ = tmpf.Close()
		cleanup()
		return fmt.Errorf("statusfile: write tmp: %w", err)
	}
	if err := tmpf.Chmod(0o600); err != nil {
		_ = tmpf.Close()
		cleanup()
		return fmt.Errorf("statusfile: chmod tmp: %w", err)
	}
	if err := tmpf.Close(); err != nil {
		cleanup()
		return fmt.Errorf("statusfile: close tmp: %w", err)
	}
	if err := renameWithRetry(tmp, path); err != nil {
		cleanup()
		return fmt.Errorf("statusfile: rename: %w", err)
	}
	return nil
}

// Read returns repoPath's current status file. Returns an os.IsNotExist error
// (checkable via os.IsNotExist) when no status file has ever been written for
// repoPath — the caller should treat this as "unknown", not as a hang or a
// fatal error: a repo the engine has never touched, or one whose engine is
// down, is a completely normal state for a poll-safe reader to observe.
//
// This performs exactly one os.ReadFile — no socket, no lock, no RPC — so it
// is safe to call on every keystroke of a statusline without risk of blocking
// behind an in-flight index.
func Read(repoPath string) (*File, error) {
	path, err := PathFor(repoPath)
	if err != nil {
		return nil, err
	}
	data, err := readFileWithRetry(path)
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("statusfile: unmarshal %s: %w", path, err)
	}
	return &f, nil
}

// ReadAll returns every status file currently on disk under
// $GRAFEL_HOME/status, parsed (#5729 PR3). Unlike Read (which requires the
// caller to already know a specific repoPath), ReadAll lets a reader
// reconstruct the FULL repo universe the status plane knows about — e.g. a
// serve process rebuilding grafel_index_status entirely from the status
// plane, including a repo the caller's registry doesn't list (a worktree
// child the engine tracks but that was never a registered fleet repo).
//
// Order is unspecified. A per-file read/parse error is skipped (best-effort;
// one corrupt/torn sidecar must never fail the whole scan — Write's
// atomic-rename means a torn file should never occur, but a reader must
// tolerate one anyway). Returns (nil, nil) — not an error — when the status
// directory does not exist yet (no engine has ever written a status file).
func ReadAll() ([]*File, error) {
	home, err := registry.HomeDir()
	if err != nil {
		return nil, fmt.Errorf("statusfile: resolve home dir: %w", err)
	}
	dir := filepath.Join(home, statusSubdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]*File, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var f File
		if err := json.Unmarshal(data, &f); err != nil {
			continue
		}
		out = append(out, &f)
	}
	return out, nil
}
