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

// TestApplyDaemonGOMAXPROCSFromCaps is the #5137 live re-apply proof: editing
// cpu.json and re-invoking the apply function (what the SIGHUP handler does)
// changes runtime.GOMAXPROCS with no restart, and clearing the cap restores the
// host default. The test restores the original GOMAXPROCS on exit so it does not
// leak global state into other tests.
func TestApplyDaemonGOMAXPROCSFromCaps(t *testing.T) {
	orig := runtime.GOMAXPROCS(0)
	t.Cleanup(func() { runtime.GOMAXPROCS(orig) })

	// Use a fixed synthetic host count well above any cap we set so the
	// host-ceiling no-op never interferes.
	const host = 64
	t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", "") // env unset → file drives it

	dir := t.TempDir()
	store := caps.NewStore(caps.DefaultPath(dir))

	// 1. cpu.json caps the daemon to 2 → applied live.
	writeCPUJSON(t, dir, `{"daemon_gomaxprocs": 2}`)
	n, _, changed := applyDaemonGOMAXPROCSFromCaps(store, host)
	if n != 2 || !changed {
		t.Fatalf("first apply: got (n=%d, changed=%v), want (2, true)", n, changed)
	}
	if got := runtime.GOMAXPROCS(0); got != 2 {
		t.Fatalf("runtime.GOMAXPROCS not applied: got %d, want 2", got)
	}

	// 2. Re-apply with no change → no-op.
	if _, _, changed := applyDaemonGOMAXPROCSFromCaps(store, host); changed {
		t.Fatalf("re-apply with unchanged file should report changed=false")
	}

	// 3. Raise the cap to 5 → applied live without restart.
	writeCPUJSON(t, dir, `{"daemon_gomaxprocs": 5}`)
	n, prev, changed := applyDaemonGOMAXPROCSFromCaps(store, host)
	if n != 5 || prev != 2 || !changed {
		t.Fatalf("raise: got (n=%d, prev=%d, changed=%v), want (5, 2, true)", n, prev, changed)
	}
	if got := runtime.GOMAXPROCS(0); got != 5 {
		t.Fatalf("raise not applied: got %d, want 5", got)
	}

	// 4. Clear the cap → restore the resource-safe DEFAULT (half cores), not
	//    fully-uncapped host (v0.1.1). Clearing cpu.json means "no operator
	//    override", which now resolves to the half-cores default rather than
	//    the Go host default.
	writeCPUJSON(t, dir, `{}`)
	wantDefault := defaultDaemonGOMAXPROCS(host) // 32 on host=64
	n, _, changed = applyDaemonGOMAXPROCSFromCaps(store, host)
	if n != wantDefault || !changed {
		t.Fatalf("clear: got (n=%d, changed=%v), want (default=%d, true)", n, changed, wantDefault)
	}
	if got := runtime.GOMAXPROCS(0); got != wantDefault {
		t.Fatalf("clear not applied: got %d, want default %d", got, wantDefault)
	}
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
	if got := runtime.GOMAXPROCS(0); got != wantDefault {
		t.Fatalf("nil store: runtime.GOMAXPROCS=%d, want default %d", got, wantDefault)
	}
}
