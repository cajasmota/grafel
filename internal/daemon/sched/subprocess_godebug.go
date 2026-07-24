package sched

import "strings"

// madvDontNeedSetting is the GODEBUG knob that makes the Go runtime release
// freed heap pages with MADV_DONTNEED instead of MADV_FREE, so the reclaim
// shows up as an immediate RSS drop rather than a lazy one under pressure.
const madvDontNeedSetting = "madvdontneed=1"

// withMadvDontNeed returns env with GODEBUG carrying madvdontneed=1, preserving
// any inherited GODEBUG settings (they are merged, never clobbered) and leaving
// an explicit operator madvdontneed=0 choice alone.
//
// The MERGE is the whole point of this function. Note it is NOT needed to
// defeat a duplicate key: os/exec runs dedupEnv over cmd.Env before exec and
// "preserve[s] the last occurrence of each key", so a plain append would in
// fact reach the child with the appended value winning. (That is also why the
// neighbouring GOMAXPROCS append in subprocess_runner.go is safe — dedupEnv
// keeps the appended value. Worth stating explicitly, because runtime.gogetenv
// returns the FIRST match, which invites the opposite conclusion; the child
// simply never sees two entries.) What a plain append WOULD lose is any other
// inherited GODEBUG setting, since the appended entry replaces the inherited
// one wholesale. Hence: strip, merge, re-append exactly one entry (#5954).
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
