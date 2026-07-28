package daemon

import (
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// engineLivenessSuffix is the filesystem suffix engineLivenessStatusKey
// stamps onto the engine-global record's RepoPath field. A reader scanning
// statusfile.ReadAll's output (every per-repo AND the engine-global record)
// uses IsEngineLivenessRecord to skip the engine-global entry when it only
// wants real per-repo rows.
const engineLivenessSuffix = ".engine-liveness"

// IsEngineLivenessRecord reports whether f is the engine-global
// liveness/warming record (as opposed to a real per-repo status file). Used
// by a full-scan reader (statusfile.ReadAll) that must not mistake the
// engine-global sidecar for an actual indexed repo.
func IsEngineLivenessRecord(f *statusfile.File) bool {
	return f != nil && strings.HasSuffix(f.RepoPath, engineLivenessSuffix)
}

// engine_status.go — #5729 PR3. Exposes the engine-global liveness/warming
// sidecar (written by startEngineLivenessHeartbeat in engineplane.go, from
// BOTH the monolith and the standalone engine) to any reader that needs it
// WITHOUT touching in-process scheduler memory — chiefly internal/mcp's
// index_status/whoami/stats handlers (ADR-0024, epic #5729).
//
// This is deliberately the SAME file the serve-side engine supervisor
// (supervise.go) already reads for its HEALTHY/DEGRADED health gate, so
// there is exactly one on-disk source of engine liveness truth.

// EngineLivenessStatus reads the engine-global liveness/warming sidecar for
// the daemon rooted at root (cfg.Layout.Root). It returns the file and
// fresh=true when a heartbeat exists and is within EngineHeartbeatStaleAfter
// — the SINGLE shared stale threshold (= engineHealthStaleMultiplier heartbeat
// intervals) that the serve-side supervisor's health gate (supervise.go
// healthy()) and the doctor's checkEngineLiveness (#5729 PR5) also use, so all
// three consumers agree byte-for-byte on "stale" with no duplicated constant.
//
// fresh=false covers every degraded case a caller must handle gracefully: no
// engine has EVER written the file (os.IsNotExist), or the file exists but
// its HeartbeatAt is stale (engine wedged, crashed, or not yet started this
// boot). In both cases the returned *statusfile.File may be nil (never
// written) or non-nil-but-stale (previous engine lifetime) — callers should
// treat fresh=false as "unknown/warming", never as an error, and never crash
// or fabricate data from a stale/absent file.
func EngineLivenessStatus(root string) (f *statusfile.File, fresh bool) {
	f, err := statusfile.Read(EngineLivenessStatusKey(root))
	if err != nil {
		return nil, false
	}
	if time.Since(f.HeartbeatAt) > EngineHeartbeatStaleAfter() {
		return f, false
	}
	return f, true
}

// WarmingFromStatusFile reconstructs a WarmingSnapshot from the ambient
// engine's liveness/warming sidecar (DefaultLayout's root), instead of a live
// in-process scheduler handle (#5729 PR3). This is the SAME answer in
// monolith and split mode: both write the engine-liveness file identically
// (see startEnginePlane in engineplane.go), so a monolith reading it and
// serve reading it converge on one code path (ADR-0024 "prefer one
// status-file path for both modes"). When the file is missing or stale
// (engine down/starting/degraded), this returns the zero WarmingSnapshot (not
// warming) — the safe "unknown" default — never an error or a crash.
func WarmingFromStatusFile() WarmingSnapshot {
	layout, err := DefaultLayout()
	if err != nil {
		return WarmingSnapshot{}
	}
	f, fresh := EngineLivenessStatus(layout.Root)
	if !fresh || f == nil {
		return WarmingSnapshot{}
	}
	return WarmingSnapshot{
		IndexInFlight: f.WarmIndexInFlight,
		PendingAlgo:   f.WarmPendingAlgo,
		PendingLinks:  f.WarmPendingLinks,
	}
}

// StageGateFromStatusFile reconstructs the heavy write-stage gate's live state
// (#5954) from the ambient engine's liveness sidecar, for a reader with no
// in-process scheduler handle — chiefly a SPLIT-MODE serve process answering
// Service.Status, where the scheduler is in the engine.
//
// It is the exact analogue of WarmingFromStatusFile, and deliberately shares
// its file and its freshness rule, so monolith and split mode converge on one
// path. ok=false means "unknown": no engine has ever written the sidecar, or
// its heartbeat is stale (engine down, starting, or wedged). Callers must leave
// the gate fields EMPTY in that case rather than reporting a stale holder as
// live — a phantom "group-algo holds the token" reading would be worse than no
// reading, given this surface exists to adjudicate exactly that question.
func StageGateFromStatusFile() (g sched.StageGateState, ok bool) {
	layout, err := DefaultLayout()
	if err != nil {
		return sched.StageGateState{}, false
	}
	f, fresh := EngineLivenessStatus(layout.Root)
	if !fresh || f == nil {
		return sched.StageGateState{}, false
	}
	return sched.StageGateState{
		Holder:          f.StageGateHolder,
		Deferred:        f.StageGateDeferred,
		Barging:         f.StageGateBarging,
		Forfeits:        f.StageGateForfeits,
		ForfeitedHolder: f.StageGateForfeitedHolder,
	}, true
}

// GroupAlgoInFlightFromStatusFile reports how many group-algo ANNOTATION passes
// the ambient engine has in flight, read from its liveness sidecar for a reader
// with no in-process scheduler handle (split-mode serve answering
// Service.Status). 0 when the sidecar is missing or stale — "unknown" reported
// as "none pending", the same conservative default the rest of this file uses.
//
// The pass writes the community/pagerank/centrality overlay AFTER the rebuild
// has already reported completion, so without this the only observable symptom
// of "the overlay is still being computed" is that clusters come back empty.
func GroupAlgoInFlightFromStatusFile() int {
	layout, err := DefaultLayout()
	if err != nil {
		return 0
	}
	f, fresh := EngineLivenessStatus(layout.Root)
	if !fresh || f == nil {
		return 0
	}
	return f.EngineGroupAlgoInFlight
}

// RebuildInFlightFromStatusFile reports how many GROUP REBUILDS the ambient
// engine is running, read from its liveness sidecar by a process that has none
// of its own — a SPLIT-MODE serve answering Service.Status (#6014).
//
// This is the exact analogue of GroupAlgoInFlightFromStatusFile, and shares its
// file and its freshness rule, so both modes converge on one path. It exists
// because Service.rebuildInFlight / groupsActiveCount are STRUCTURALLY zero in
// split mode: their increments sit below Service.Rebuild's split-mode early
// return, and the rebuild itself runs in the engine. Serve therefore has no
// local information at all about a running rebuild, so StatusReply.RebuildInFlight
// was a constant 0 — a working rebuild was indistinguishable from a dropped one.
//
// The RPC field was only half the problem. `grafel status` printed its
// `rebuild: in_flight=` line inside a block gated on StatusReply.RSSBudgetMB,
// which is itself populated only from an in-process scheduler — so in split
// mode the line did not print AT ALL, and a correct value here would still have
// been invisible. That gate was hoisted in the same change (internal/cli's
// printDaemonDetail); this accessor is only observable because of it.
//
// LATENCY, honestly: the value is published by a plain 5s ticker
// (startEngineLivenessHeartbeat, defaultStatusHeartbeatInterval) with no
// state-change coalescing — unlike startStatusWriter, which has a notify seam.
// So a rebuild that has just started still reads 0 here for up to one heartbeat
// interval. Group rebuilds are multi-minute, so a ≤5s false zero at the very
// start is acceptable; it is called out because it is this same bug in
// miniature, and anyone tightening it should add a notify on
// rebuildWorker.submit rather than shortening the tick.
//
// 0 when the sidecar is missing or stale ("unknown" reported as "none"), the
// same conservative default as the rest of this file. A stale sidecar's
// last-known count must NEVER be reported as live: this surface exists to
// answer "is a rebuild running RIGHT NOW", so a phantom non-zero would mislead
// exactly the reader it was built for — no better than the wrong zero it
// replaces.
func RebuildInFlightFromStatusFile() int {
	layout, err := DefaultLayout()
	if err != nil {
		return 0
	}
	f, fresh := EngineLivenessStatus(layout.Root)
	if !fresh || f == nil {
		return 0
	}
	return f.EngineRebuildInFlight
}

// FlushRepoStatusFile synchronously recomputes and writes repoPath's per-repo
// status-plane sidecar from the CURRENT indexstate + the on-disk graph.fb, right
// now — the same work the async statusWriter goroutine would do on its next
// coalesced pass or 5s heartbeat, but ON DEMAND and inline.
//
// The engine's DIRECT group-rebuild path (cmd/grafel daemonRebuildFuncCore)
// bypasses the scheduler, so it never triggers the statusWriter's
// SetRepoStates→notify refresh; a rebuilt repo's status file can therefore carry
// a STALE GraphFBMtime (== the pre-rebuild baseline) for up to a heartbeat
// interval after the rebuild returns. In split mode the drain writes the
// request ack the instant rebuildFn returns, so a wizard keying completion on
// that ack (internal/cli RebuildRequestPending) would classify a repo that
// actually succeeded as "produced no graph". daemonRebuildFuncCore calls this
// for every rebuilt repo AFTER its graph.fb is written and BEFORE returning, so
// !RequestPending implies the per-repo status is already fresh and the wizard's
// first classify poll is correct (#5729 blocker #5).
//
// Sourced fields of note: GraphFBMtime is stat'd from the real graph.fb via
// FindGraphFile (so this MUST be called after the graph.fb write to capture the
// fresh mtime); Indexing comes from indexstate (false on the direct path, which
// never registers the repo as indexing). Entities may still be 0 — the
// graph-stats sidecar is a separate follow-up — and that is fine, the wizard's
// classification uses graph_fb_mtime, not entities. Best-effort: a write failure
// is swallowed (nil logger) and never fails the rebuild.
func FlushRepoStatusFile(repoPath string) {
	writeRepoStatusFile(repoPath, nil)
}

// WriteRepoStatusFileForTest synchronously computes and writes repoPath's
// status-plane sidecar from the CURRENT indexstate — exactly what the
// production statusWriter goroutine would do on its next coalesced pass, but
// without spinning up the full ticker/goroutine machinery. Intended for
// tests that call indexstate.SetRepoStates(...) to simulate live scheduler
// state and need the corresponding statusfile to exist on disk so a
// statusfile-driven reader (grafel_index_status et al, #5729 PR3) observes
// it (#5729 PR3 equivalence tests).
func WriteRepoStatusFileForTest(repoPath string) {
	writeRepoStatusFile(repoPath, nil)
}

// RepoStatusFile reads repoPath's per-repo status-plane sidecar. Returns
// ok=false when no engine has ever written a status file for repoPath (a
// never-indexed repo, or one whose status predates #5729 PR3) — callers
// should fall back to a disk-header read (see mcp.diskFallbackRow), never
// treat this as fatal.
func RepoStatusFile(repoPath string) (f *statusfile.File, ok bool) {
	f, err := statusfile.Read(repoPath)
	if err != nil {
		return nil, false
	}
	return f, true
}
