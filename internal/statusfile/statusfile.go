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

	// Indexing is true while a reindex for RepoPath is in flight right now.
	Indexing bool `json:"indexing"`
	// QueueLen is the number of index jobs queued behind this repo (or,
	// process-wide, the scheduler's queue depth — see the writer for which
	// scope it publishes).
	QueueLen int `json:"queue_len,omitempty"`
	// LastErr is the most recent index error for this repo, or "" if the
	// last completed index succeeded.
	LastErr string `json:"last_err,omitempty"`

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

// statusSubdir is the directory name under GRAFEL_HOME holding one file per
// repo.
const statusSubdir = "status"

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
	if err := os.Rename(tmp, path); err != nil {
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
	data, err := os.ReadFile(path)
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
