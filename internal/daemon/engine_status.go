package daemon

import (
	"strings"
	"time"

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
