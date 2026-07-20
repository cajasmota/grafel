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
func applyMemoryLimit(logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}

	// Respect an explicit GOMEMLIMIT — the runtime already applied it at
	// startup; re-setting from here would clobber the operator's choice.
	if v := os.Getenv("GOMEMLIMIT"); v != "" && v != "off" {
		logger.Info("daemon: GOMEMLIMIT already set by runtime env; not overriding (#3648)", "gomemlimit", v)
		return
	}

	limitMB, source := resolveMemLimitMB()
	if limitMB <= 0 {
		logger.Info("daemon: Go soft memory limit disabled (#3648)", "source", source)
		return
	}
	limitBytes := limitMB * 1024 * 1024
	debug.SetMemoryLimit(limitBytes)
	logger.Info("daemon: applied Go soft memory limit (#3648)",
		"limit_mb", limitMB, "source", source)
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
