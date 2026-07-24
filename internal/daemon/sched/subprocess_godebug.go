package sched

import "strings"

// madvDontNeedSetting is the GODEBUG knob that makes the Go runtime release
// freed heap pages with MADV_DONTNEED instead of MADV_FREE, so the reclaim
// shows up as an immediate RSS drop rather than a lazy one under pressure.
const madvDontNeedSetting = "madvdontneed=1"

// withMadvDontNeed returns env with GODEBUG carrying madvdontneed=1, preserving
// any inherited GODEBUG settings (they are merged, never clobbered) and leaving
// an explicit operator madvdontneed choice alone. Exactly one GODEBUG entry is
// present in the result: the Go runtime reads the FIRST matching env entry, so
// appending a duplicate key would be a silent no-op (#5954).
func withMadvDontNeed(env []string) []string {
	out := make([]string, 0, len(env)+1)
	inherited := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "GODEBUG="); ok {
			inherited = v
			continue
		}
		out = append(out, kv)
	}

	merged := madvDontNeedSetting
	if inherited != "" {
		already := false
		for _, s := range strings.Split(inherited, ",") {
			if strings.HasPrefix(strings.TrimSpace(s), "madvdontneed=") {
				already = true
				break
			}
		}
		if already {
			merged = inherited
		} else {
			merged = inherited + "," + madvDontNeedSetting
		}
	}
	return append(out, "GODEBUG="+merged)
}
