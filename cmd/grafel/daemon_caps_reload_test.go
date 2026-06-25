package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/caps"
)

// TestResolveDaemonGOMAXPROCSWith covers the #5137 env>file>half-cores-default
// precedence for the daemon's own in-process GOMAXPROCS, including the
// host-ceiling no-op. Resource-safe default (v0.1.1): when neither env nor
// cpu.json pins a value the resolver returns half the host cores (6 on a
// 12-core host) rather than 0 ("no cap").
func TestResolveDaemonGOMAXPROCSWith(t *testing.T) {
	const host = 12
	const halfDefault = host / 2
	cases := []struct {
		name    string
		env     string
		fileVal int
		want    int
	}{
		{"none-defaults-half", "", 0, halfDefault},
		{"file-only", "", 3, 3},
		{"env-only", "5", 0, 5},
		{"env-beats-file", "5", 9, 5},
		{"file-at-host-noop", "", 12, 0},
		{"file-above-host-noop", "", 20, 0},
		{"env-at-host-noop-ignores-file", "12", 3, 0},
		{"file-nonpositive-defaults-half", "", 0, halfDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", tc.env)
			if got := resolveDaemonGOMAXPROCSWith(host, tc.fileVal); got != tc.want {
				t.Fatalf("resolveDaemonGOMAXPROCSWith(host=%d, env=%q, file=%d) = %d, want %d",
					host, tc.env, tc.fileVal, got, tc.want)
			}
		})
	}
}

// writeCPUJSON writes cpu.json into dir and bumps its mtime forward so the
// caps.Store's (mtime,size) cache key changes deterministically.
func writeCPUJSON(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, caps.FileName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write cpu.json: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(path, future, future)
	return path
}

// assertLiveGOMAXPROCS checks that runtime.GOMAXPROCS(0) reflects a freshly
// applied `target`, but ONLY when target fits within the real host core count.
//
// The #5137 host-core-count de-flake: the apply function resolves its target
// purely from the SYNTHETIC host count the test passes in, so its return values
// are deterministic on any machine — but the live runtime.GOMAXPROCS(0) readback
// of a target that exceeds the real NumCPU is PLATFORM-DEPENDENT. On linux/darwin
// runtime.GOMAXPROCS(32) reports back 32 even on a 12-core box (no clamp); on a
// 2-core windows CI runner the runtime clamps and reports 2. Neither ==target nor
// ==min(target,NumCPU) is portable, so we only assert the readback in the regime
// where every platform agrees (target <= NumCPU → readback == target) and skip it
// otherwise. The apply logic itself is still fully proven by the deterministic
// (n, prev, changed) return-value assertions at every step.
func assertLiveGOMAXPROCS(t *testing.T, label string, target int) {
	t.Helper()
	if target > runtime.NumCPU() {
		t.Logf("%s: skipping live GOMAXPROCS readback (target=%d > NumCPU=%d; "+
			"effective value is platform-dependent — apply proven via return values)",
			label, target, runtime.NumCPU())
		return
	}
	if got := runtime.GOMAXPROCS(0); got != target {
		t.Fatalf("%s: runtime.GOMAXPROCS=%d, want %d", label, got, target)
	}
}

// TestApplyDaemonGOMAXPROCSFromCaps is the #5137 live re-apply proof: editing
// cpu.json and re-invoking the apply function (what the SIGHUP handler does)
// changes runtime.GOMAXPROCS with no restart, and clearing the cap restores the
// host default. The test restores the original GOMAXPROCS on exit so it does not
// leak global state into other tests.
//
// The apply function's target is derived from the synthetic `host` constant, so
// the returned (n, prev, changed) tuple is deterministic on every machine. Only
// the live runtime.GOMAXPROCS(0) readback depends on the real core count, so it
// goes through assertLiveGOMAXPROCS, which skips the check when the requested
// value exceeds NumCPU (where the effective value is platform-dependent) —
// making the test robust on a 1-/2-core CI runner.
func TestApplyDaemonGOMAXPROCSFromCaps(t *testing.T) {
	orig := runtime.GOMAXPROCS(0)
	t.Cleanup(func() { runtime.GOMAXPROCS(orig) })

	// Use a fixed synthetic host count well above any cap we set so the
	// host-ceiling no-op never interferes.
	const host = 64
	t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", "") // env unset → file drives it

	dir := t.TempDir()
	store := caps.NewStore(caps.DefaultPath(dir))

	// 1. cpu.json caps the daemon to 2 → applied live. 2 ≤ NumCPU on any host
	//    we run on (Go requires ≥1 core), so the readback is exactly 2.
	writeCPUJSON(t, dir, `{"daemon_gomaxprocs": 2}`)
	n, _, changed := applyDaemonGOMAXPROCSFromCaps(store, host)
	if n != 2 || !changed {
		t.Fatalf("first apply: got (n=%d, changed=%v), want (2, true)", n, changed)
	}
	assertLiveGOMAXPROCS(t, "first apply", 2)

	// 2. Re-apply with no change → no-op (target unchanged at 2).
	if _, _, changed := applyDaemonGOMAXPROCSFromCaps(store, host); changed {
		t.Fatalf("re-apply with unchanged file should report changed=false")
	}

	// 3. Raise the cap to 5 → re-applied without restart. The function's target
	//    is 5 on every host (resolved from the synthetic host=64), so n and prev
	//    are deterministic everywhere. The live readback is only asserted when the
	//    host actually has ≥5 cores; on a <5-core runner the effective value is
	//    platform-dependent (this is exactly what flaked on a 2-core windows
	//    runner — the readback is now skipped there, the apply still proven by the
	//    return values).
	writeCPUJSON(t, dir, `{"daemon_gomaxprocs": 5}`)
	n, prev, changed := applyDaemonGOMAXPROCSFromCaps(store, host)
	// prev is the PREVIOUS effective GOMAXPROCS — i.e. what step 1 left in place.
	// Step 1 requested 2; on a host with ≥2 cores that is exactly 2, but on a
	// 1-core host the runtime can only run 1, so accept min(2, NumCPU).
	wantPrev := 2
	if runtime.NumCPU() < 2 {
		wantPrev = runtime.NumCPU()
	}
	if n != 5 || prev != wantPrev || !changed {
		t.Fatalf("raise: got (n=%d, prev=%d, changed=%v), want (5, %d, true)", n, prev, changed, wantPrev)
	}
	assertLiveGOMAXPROCS(t, "raise", 5)

	// 4. Clear the cap → restore the resource-safe DEFAULT (half cores), not
	//    fully-uncapped host (v0.1.1). Clearing cpu.json means "no operator
	//    override", which now resolves to the half-cores default rather than
	//    the Go host default. The default is computed from the synthetic host
	//    (32 on host=64); the live readback is only asserted when the host has
	//    ≥32 cores (skipped otherwise), the apply proven by the return value.
	writeCPUJSON(t, dir, `{}`)
	wantDefault := defaultDaemonGOMAXPROCS(host) // 32 on host=64
	n, _, changed = applyDaemonGOMAXPROCSFromCaps(store, host)
	if n != wantDefault || !changed {
		t.Fatalf("clear: got (n=%d, changed=%v), want (default=%d, true)", n, changed, wantDefault)
	}
	assertLiveGOMAXPROCS(t, "clear", wantDefault)
}

// TestApplyDaemonGOMAXPROCSFromCaps_NilStore: a nil store with no env cap
// resolves to the resource-safe half-cores default (v0.1.1), not the host
// default. Uses a synthetic host count so the assertion is deterministic
// regardless of the test machine's core count.
func TestApplyDaemonGOMAXPROCSFromCaps_NilStore(t *testing.T) {
	orig := runtime.GOMAXPROCS(0)
	t.Cleanup(func() { runtime.GOMAXPROCS(orig) })
	t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", "")

	const host = 16
	wantDefault := defaultDaemonGOMAXPROCS(host) // 8
	runtime.GOMAXPROCS(host)                     // start above the default so a change is observable
	n, _, _ := applyDaemonGOMAXPROCSFromCaps(nil, host)
	if n != wantDefault {
		t.Fatalf("nil store: n=%d, want default %d", n, wantDefault)
	}
	// The returned n is computed from the synthetic host=16; the live readback is
	// only asserted when the host actually has ≥8 cores (the effective value of an
	// over-NumCPU request is platform-dependent), keeping this robust on a <8-core
	// CI runner.
	assertLiveGOMAXPROCS(t, "nil store", wantDefault)
}
