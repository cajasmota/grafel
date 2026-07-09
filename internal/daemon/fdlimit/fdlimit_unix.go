//go:build linux || darwin

package fdlimit

import "syscall"

// Raise raises the RLIMIT_NOFILE soft limit toward target (clamped to the hard
// limit), and returns the previous and new soft limits.
//
// Guarantees:
//   - It NEVER lowers the soft limit. If target is below the current soft
//     limit, the current limit is left untouched and returned unchanged.
//   - It is best-effort: on any Setrlimit failure it tries progressively
//     smaller candidates (handling the Darwin quirk where setting Cur to an
//     "unlimited" hard Max fails with EINVAL — the effective ceiling there is
//     min(Max, kern.maxfilesperproc)). If none succeed, it returns the
//     unchanged current limit together with the last error.
//
// Callers should treat a non-nil error as a warning, not a fatal condition.
func Raise(target uint64) (old, updated uint64, err error) {
	var lim syscall.Rlimit
	if err = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return 0, 0, err
	}
	old = lim.Cur

	// Desired soft limit: at least the current (never lower), at most target.
	desired := target
	if desired < old {
		desired = old
	}

	// Try a descending set of candidates. The first that Setrlimit accepts
	// wins. Each candidate is clamped to the hard Max (raising Cur above Max
	// is rejected for unprivileged processes on Linux). On Darwin the hard Max
	// is often "unlimited" and the real cap is lower, so we fall back to
	// smaller concrete targets on EINVAL.
	for _, cand := range candidates(desired, lim.Max) {
		if cand <= old {
			// Would not raise (or would lower); skip.
			continue
		}
		trial := lim
		trial.Cur = cand
		if e := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &trial); e == nil {
			return old, cand, nil
		} else {
			err = e
		}
	}

	// Nothing raised the limit; report the still-current value. err holds the
	// last Setrlimit error (nil if every candidate was already <= old).
	return old, old, err
}

// candidates returns a descending, de-duplicated list of soft-limit targets to
// attempt, each clamped to the hard max. hardMax==0 is treated as "unknown"
// and applies no clamp.
func candidates(desired, hardMax uint64) []uint64 {
	clamp := func(v uint64) uint64 {
		if hardMax != 0 && v > hardMax {
			return hardMax
		}
		return v
	}
	raw := []uint64{clamp(desired), clamp(DefaultTarget), clamp(10240)}
	out := make([]uint64, 0, len(raw))
	seen := make(map[uint64]bool, len(raw))
	for _, v := range raw {
		if v == 0 || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
