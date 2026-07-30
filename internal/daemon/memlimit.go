package daemon

import (
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"

	"github.com/cajasmota/grafel/internal/process"
)

// memLimitEnv overrides the Go soft memory limit (GOMEMLIMIT) the daemon
// applies at startup. The value is in MEGABYTES. A value <= 0, "off", or
// "0" disables the daemon-applied limit entirely (the Go runtime default —
// effectively unlimited — is left in place, or whatever a real GOMEMLIMIT
// env already set). Added for #3648.
const memLimitEnv = "GRAFEL_DAEMON_MEMLIMIT_MB"

// memLimitFraction is the fraction of total system RAM used as the default
// soft limit when the operator has not pinned a value via memLimitEnv. 0.40
// is deliberately conservative: the daemon is a background developer tool
// that should leave the bulk of RAM for the editor, language servers,
// browser, and the page cache. On an 8GB laptop this resolves to ~3.2GB
// (then capped, see memLimitCeilingMB). The previous 0.75 fraction is what
// let the runtime hoard a multi-GB reclaimable arena on big-RAM hosts —
// measured live on a 64GB box, the old 12GB limit left ~1.5GB of
// heap_released sitting idle. The cap (not the fraction) is what bites on
// any reasonably large host; the fraction only governs small hosts, where
// it backs off proportionally rather than pinning a large absolute value.
const memLimitFraction = 0.40

// memLimitCeilingMB caps the fraction-of-RAM result so a big-RAM host can
// never hand the daemon a lax limit it will hoard against. Measured live:
// pinning the limit at 1536MB dropped idle phys_footprint 2.6GB → 1.75GB
// (heap_released 1571MB → 370MB), and a legitimate large reindex peaks at
// ~1-1.5GB heap per job. 2560MB (2.5GB) sits comfortably above that working
// set — and because the limit is SOFT, a job that briefly exceeds it just
// makes Go GC harder rather than OOM-kill the indexer — while being tight
// enough that the runtime promptly returns idle arena to the OS instead of
// retaining it. This ceiling is the constraint that actually fires on any
// host with more than ~6.25GB RAM (0.40 * 6.25GB ≈ 2.5GB).
const memLimitCeilingMB int64 = 2560

// memLimitFloorMB is the smallest soft limit we will ever apply. On a tiny
// host (or when TotalMemoryMB returns 0) we must not set a limit so low that
// a normal index would constantly hit it and thrash; 2GB is comfortably
// above the per-job peak heap. Note floor (2048) < ceiling (2560), so on
// hosts between ~5GB and ~6.25GB RAM the resolved limit lands in that band
// untouched by either clamp.
const memLimitFloorMB int64 = 2048

// MemLimitServeShare is the fraction of the INSTALLATION-WIDE soft memory
// limit granted to the serve (read) plane when the daemon runs split
// (ADR-0024 default). The engine (write) plane gets the remainder.
//
// Why 30/70 rather than an even split (#6045):
//
//   - The engine is the write plane — scheduler, watcher, extraction,
//     fbwriter — and is where the heavy allocation happens. A legitimate large
//     reindex peaks at ~1-1.5GB Go heap per job (measured, see
//     memLimitCeilingMB), so an even split of the 2560MB default ceiling
//     (1280MB) would sit BELOW the engine's normal working set and make the
//     runtime GC continuously against a limit it cannot honour.
//   - serve's large data structure — the graph_cache mmap — is file-backed and
//     NOT Go-heap, so GOMEMLIMIT does not account for it at all. What serve
//     actually allocates on the heap is MCP request/response marshalling and
//     dashboard JSON: small, bursty, short-lived.
//
// 0.30 of the 2560MB default gives serve 768MB and the engine 1792MB, which
// clears the measured per-job peak with headroom while keeping the pair's TOTAL
// equal to the single advertised figure. Both limits remain SOFT: a plane that
// briefly exceeds its share GCs harder, it is not OOM-killed.
const MemLimitServeShare = 0.30

// SplitMemLimitMB divides an installation-wide soft limit (MB) into the serve
// and engine shares. The two shares sum to EXACTLY totalMB — the engine
// absorbs the rounding — which is the whole point of #6045: the pair must not
// be able to exceed the number `grafel status` advertises.
//
// A non-positive totalMB means "limit disabled" and propagates as-is to both
// shares; splitting must never synthesize a limit where there was none.
func SplitMemLimitMB(totalMB int64) (serveMB, engineMB int64) {
	if totalMB <= 0 {
		return totalMB, totalMB
	}
	serveMB = int64(float64(totalMB) * MemLimitServeShare)
	if serveMB < 1 {
		serveMB = 1 // never hand a plane a zero limit
	}
	if serveMB >= totalMB {
		serveMB = totalMB - 1
	}
	return serveMB, totalMB - serveMB
}

// memPlane identifies which process is applying the soft memory limit, and
// therefore which share of the installation budget it gets.
//
// The share is derived from the plane the process is ACTUALLY running, not
// from an env var handed down by serve. That is deliberate: the engine child
// inherits serve's environment verbatim and defaultEngineChildCommand must not
// mutate it (see its doc comment — synthesizing env for the child is exactly
// what broke the store-root invariant once already). Both processes see the
// same env, the same settings.json and the same host RAM, so both resolve the
// identical TOTAL independently and then take their own slice of it. No
// channel, nothing to keep in sync, and a standalone `grafel engine` started
// by hand still stays inside the installation budget.
type memPlane int

const (
	// memPlaneMonolith is the single-process daemon (escape hatch
	// GRAFEL_SPLIT_MODE=0): one process, so it gets the WHOLE budget.
	memPlaneMonolith memPlane = iota
	// memPlaneServe is the split-mode serve (read) plane.
	memPlaneServe
	// memPlaneEngine is the split-mode engine (write) plane.
	memPlaneEngine
)

func (p memPlane) String() string {
	switch p {
	case memPlaneServe:
		return "serve"
	case memPlaneEngine:
		return "engine"
	default:
		return "monolith"
	}
}

// shareOf returns this plane's slice of an installation-wide limit (MB).
func (p memPlane) shareOf(totalMB int64) int64 {
	if totalMB <= 0 {
		return totalMB
	}
	serve, engine := SplitMemLimitMB(totalMB)
	switch p {
	case memPlaneServe:
		return serve
	case memPlaneEngine:
		return engine
	default:
		return totalMB
	}
}

// memPlaneForDaemonPlane maps the run() plane selector onto the memory plane.
func memPlaneForDaemonPlane(plane daemonPlaneMode) memPlane {
	if plane == planeServeOnly {
		return memPlaneServe
	}
	return memPlaneMonolith
}

// applyMemoryLimit sets a conservative Go soft memory limit (GOMEMLIMIT) so
// the runtime collects more aggressively as it nears the cap, bounding the
// daemon's peak footprint (#3648, tightened in #5237).
//
// Precedence:
//  1. If the standard GOMEMLIMIT env var is already set, the Go runtime has
//     already honored it — we do nothing and log that we deferred to it.
//  2. GRAFEL_DAEMON_MEMLIMIT_MB (MB; "off"/"0"/negative disables).
//  3. daemon_go_memory_limit_mb from settings.json.
//  4. Default: memLimitFraction of total system RAM, clamped to
//     [memLimitFloorMB, memLimitCeilingMB]. If total RAM is unknown (0), we
//     fall back to the floor.
//
// This is intentionally a soft limit: Go will exceed it transiently rather
// than OOM-killing the indexer, it just GCs harder — so a legitimate heavy
// reindex still completes, it simply doesn't get to retain a multi-GB arena.
//
// The resolved value is the budget for the WHOLE INSTALLATION, not for one
// process (#6045). In split mode two processes start — serve and engine — and
// each used to apply the full resolved limit, so the real ceiling was 2x the
// figure `grafel status` advertised. Each process now applies only its plane's
// share (see memPlane / SplitMemLimitMB); the monolith, being one process,
// still gets the whole thing.
func applyMemoryLimit(logger *slog.Logger, plane memPlane) {
	if logger == nil {
		logger = slog.Default()
	}

	// Respect an explicit GOMEMLIMIT — the runtime already applied it at
	// startup; re-setting from here would clobber the operator's choice.
	// NOTE: this path is genuinely per-process — the runtime read the env var
	// before main() in BOTH planes and we cannot retroactively split it. That
	// is the operator's explicit choice, and the log names the plane so the
	// doubling is at least visible.
	if v := os.Getenv("GOMEMLIMIT"); v != "" && v != "off" {
		logger.Info("daemon: GOMEMLIMIT already set by runtime env; not overriding (#3648)",
			"gomemlimit", v, "plane", plane.String())
		return
	}

	totalMB, source := resolveMemLimitMB()
	if totalMB <= 0 {
		logger.Info("daemon: Go soft memory limit disabled (#3648)", "source", source, "plane", plane.String())
		return
	}
	limitMB := plane.shareOf(totalMB)
	limitBytes := limitMB * 1024 * 1024
	debug.SetMemoryLimit(limitBytes)
	logger.Info("daemon: applied Go soft memory limit (#3648, split #6045)",
		"limit_mb", limitMB, "total_mb", totalMB, "plane", plane.String(), "source", source)
}

// resolveMemLimitMB returns the soft-limit in MB and a short tag describing
// where the value came from. A non-positive return means "disabled".
func resolveMemLimitMB() (int64, string) {
	if raw := os.Getenv(memLimitEnv); raw != "" {
		switch raw {
		case "off", "OFF", "false", "0":
			return -1, memLimitEnv + "=" + raw + " (disabled)"
		}
		if mb, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if mb <= 0 {
				return -1, memLimitEnv + " (disabled)"
			}
			return mb, memLimitEnv
		}
		// Unparseable override: fall through to settings/default rather than
		// guessing.
	}
	if mb := ConfiguredGoMemoryLimitMB(); mb > 0 {
		return mb, "settings.json:daemon_go_memory_limit_mb"
	}

	totalMB := process.TotalMemoryMB()
	if totalMB <= 0 {
		return memLimitFloorMB, "floor (system RAM unknown)"
	}
	limit := int64(float64(totalMB) * memLimitFraction)
	// Cap first so a big-RAM host can't grant a lax limit, then floor so a
	// tiny host isn't starved. Ceiling > floor, so the order is stable.
	if limit > memLimitCeilingMB {
		return memLimitCeilingMB, "fraction-of-RAM (capped)"
	}
	if limit < memLimitFloorMB {
		return memLimitFloorMB, "fraction-of-RAM (floored)"
	}
	return limit, "fraction-of-RAM"
}

// ResolveMemLimitMB exposes the resolved Go soft memory limit (in MB) and a
// short source tag for operator-facing surfaces (grafel status / doctor).
// A non-positive limit means the daemon-applied limit is disabled. This does
// NOT account for an explicit GOMEMLIMIT env var, which takes precedence at
// daemon startup; callers that want full fidelity should check GOMEMLIMIT
// themselves (see MemLimitSummary).
func ResolveMemLimitMB() (int64, string) {
	return resolveMemLimitMB()
}

// MemLimitSummary returns the effective Go soft memory limit the daemon
// would apply, honoring the same precedence as applyMemoryLimit:
//
//	explicit GOMEMLIMIT > GRAFEL_DAEMON_MEMLIMIT_MB > settings.json >
//	fraction-of-RAM default.
//
// It returns the limit in MB (<=0 means disabled / unbounded) and a short
// source tag. When an explicit GOMEMLIMIT is set, mb is reported as 0 with
// source "GOMEMLIMIT (runtime env)" because the raw value may carry a unit
// suffix (e.g. "4GiB") that we do not parse here — the tag tells the operator
// to read GOMEMLIMIT directly.
func MemLimitSummary() (mb int64, source string) {
	if v := os.Getenv("GOMEMLIMIT"); v != "" && v != "off" {
		return 0, "GOMEMLIMIT=" + v + " (runtime env)"
	}
	return resolveMemLimitMB()
}

// MemLimitPlaneSummary reports the INSTALLATION-WIDE soft memory limit and how
// it is divided across the running processes, for operator-facing surfaces
// (grafel status / doctor). Added for #6045, where status advertised a single
// figure that two processes each applied in full.
//
//   - totalMB is the whole-installation budget (<=0 means disabled/unbounded).
//   - split reports whether the daemon runs the serve/engine process split.
//   - When split, serveMB+engineMB == totalMB exactly.
//   - When NOT split (monolith escape hatch GRAFEL_SPLIT_MODE=0) there is one
//     process: serveMB == totalMB and engineMB == 0.
//
// Like MemLimitSummary this reads the local process env, which a co-located
// daemon shares.
func MemLimitPlaneSummary() (totalMB, serveMB, engineMB int64, source string, split bool) {
	totalMB, source = MemLimitSummary()
	split = SplitModeEnabled()
	if totalMB <= 0 {
		return totalMB, 0, 0, source, split
	}
	if !split {
		return totalMB, totalMB, 0, source, false
	}
	serveMB, engineMB = SplitMemLimitMB(totalMB)
	return totalMB, serveMB, engineMB, source, true
}
