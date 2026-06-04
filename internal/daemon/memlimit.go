package daemon

import (
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"

	"github.com/cajasmota/archigraph/internal/process"
)

// memLimitEnv overrides the Go soft memory limit (GOMEMLIMIT) the daemon
// applies at startup. The value is in MEGABYTES. A value <= 0, "off", or
// "0" disables the daemon-applied limit entirely (the Go runtime default —
// effectively unlimited — is left in place, or whatever a real GOMEMLIMIT
// env already set). Added for #3648.
const memLimitEnv = "ARCHIGRAPH_DAEMON_MEMLIMIT_MB"

// memLimitFraction is the fraction of total system RAM used as the default
// soft limit when the operator has not pinned a value via memLimitEnv. 0.75
// leaves a quarter of RAM for the page cache, other processes, and the
// kernel — conservative enough that on a 16GB host the daemon GCs hard
// before it can balloon to the observed 10.2GB peak, but loose enough that a
// single legitimate large reindex (peak heap ~1-1.5GB per job) is nowhere
// near the limit and does not trigger GC thrash.
const memLimitFraction = 0.75

// memLimitFloorMB is the smallest soft limit we will ever apply. On a tiny
// host (or when TotalMemoryMB returns 0) we must not set a limit so low that
// a normal index would constantly hit it and thrash; 2GB is comfortably
// above the per-job peak heap.
const memLimitFloorMB int64 = 2048

// applyMemoryLimit sets a conservative Go soft memory limit (GOMEMLIMIT) so
// the runtime collects more aggressively as it nears the cap, bounding the
// daemon's peak footprint (#3648).
//
// Precedence:
//  1. If the standard GOMEMLIMIT env var is already set, the Go runtime has
//     already honored it — we do nothing and log that we deferred to it.
//  2. ARCHIGRAPH_DAEMON_MEMLIMIT_MB (MB; "off"/"0"/negative disables).
//  3. Default: memLimitFraction of total system RAM, floored at
//     memLimitFloorMB. If total RAM is unknown (0), we fall back to the
//     floor.
//
// This is intentionally a soft limit: Go will exceed it transiently rather
// than OOM-killing the indexer, it just GCs harder — so a legitimate heavy
// reindex still completes, it simply doesn't get to retain a 10GB arena.
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
		// Unparseable override: fall through to the RAM-fraction default
		// rather than guessing.
	}

	totalMB := process.TotalMemoryMB()
	if totalMB <= 0 {
		return memLimitFloorMB, "floor (system RAM unknown)"
	}
	limit := int64(float64(totalMB) * memLimitFraction)
	if limit < memLimitFloorMB {
		limit = memLimitFloorMB
	}
	return limit, "fraction-of-RAM"
}
