package install

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestWaitForDaemonReady_SocketUpHealthzSlow simulates the #5293 repro: the
// daemon's RPC socket comes up quickly but its dashboard /healthz (gated on the
// initial cold index) is still slow / failing. Readiness must SUCCEED on the
// socket within budget — not falsely fail like the old /healthz-gated wait.
func TestWaitForDaemonReady_SocketUpHealthzSlow(t *testing.T) {
	var pings int32
	ping := func(socketPath string) (string, error) {
		// Socket answers immediately (daemon is serving) and reports a version,
		// even though it is still cold-indexing.
		atomic.AddInt32(&pings, 1)
		return "1.2.3-socket", nil
	}
	// /healthz never returns 200 (still cold-indexing) — must NOT fail install.
	healthz := func(port int) (string, error) {
		return "", fmt.Errorf("dashboard still cold-indexing (HTTP 503)")
	}

	version, err := waitForDaemonReady("/tmp/fake.sock", 47274, 2*time.Second, ping, healthz)
	if err != nil {
		t.Fatalf("expected readiness to succeed on the socket, got error: %v", err)
	}
	if version != "1.2.3-socket" {
		t.Errorf("expected version from socket Ping, got %q", version)
	}
	if atomic.LoadInt32(&pings) == 0 {
		t.Error("expected the socket Ping probe to be used")
	}
}

// TestWaitForDaemonReady_HealthzEnrichesVersion verifies that when /healthz IS
// available it enriches the version string, but readiness is still primarily
// gated on the socket.
func TestWaitForDaemonReady_HealthzEnrichesVersion(t *testing.T) {
	ping := func(socketPath string) (string, error) { return "socket-ver", nil }
	healthz := func(port int) (string, error) { return "  healthz-ver  ", nil }

	version, err := waitForDaemonReady("/tmp/fake.sock", 47274, 2*time.Second, ping, healthz)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "healthz-ver" {
		t.Errorf("expected /healthz body to enrich version, got %q", version)
	}
}

// TestWaitForDaemonReady_HealthzHTMLDoesNotOverrideVersion is the regression
// guard for the install "daemon: <!doctype html>..." bug. The daemon dashboard
// has no dedicated /healthz route, so an unmatched GET falls through to the SPA
// catch-all and returns the dashboard index.html (HTTP 200 + HTML). That HTML
// must NEVER replace the real socket-Ping version — install would otherwise
// print the entire HTML document as the "daemon" status line.
func TestWaitForDaemonReady_HealthzHTMLDoesNotOverrideVersion(t *testing.T) {
	ping := func(socketPath string) (string, error) { return "0.1.5.2", nil }
	healthz := func(port int) (string, error) {
		return "<!doctype html><html lang=\"en\"><head><title>grafel</title></head><body></body></html>", nil
	}

	version, err := waitForDaemonReady("/tmp/fake.sock", 47274, 2*time.Second, ping, healthz)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "0.1.5.2" {
		t.Errorf("expected the socket-Ping version to be kept (HTML body ignored), got %q", version)
	}
}

// TestLooksLikeVersion exercises the version-string sanity guard that protects
// the printed "daemon:" status line from non-version bodies (the SPA-fallback
// HTML dump in particular).
func TestLooksLikeVersion(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"0.1.5.2", true},
		{"v0.1.5", true},
		{"1.2.3-socket", true},
		{"  dev  ", true}, // trimmed
		{"", false},
		{"<!doctype html><html></html>", false},
		{"<html><body>hi</body></html>", false},
		{"line1\nline2", false},          // multi-line
		{strings.Repeat("a", 65), false}, // too long
		{"{\"version\":\"1.0\"}", true},  // short JSON is tolerated (no angle brackets / newline)
		{"<svg>", false},                 // angle brackets
	}
	for _, c := range cases {
		if got := looksLikeVersion(c.in); got != c.want {
			t.Errorf("looksLikeVersion(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestWaitForDaemonReady_SocketEventuallyUp verifies the probe keeps polling
// until the socket comes up (slow cold start) and then succeeds.
func TestWaitForDaemonReady_SocketEventuallyUp(t *testing.T) {
	var calls int32
	ping := func(socketPath string) (string, error) {
		if atomic.AddInt32(&calls, 1) < 3 {
			return "", fmt.Errorf("daemon not running yet")
		}
		return "late-ver", nil
	}

	version, err := waitForDaemonReady("/tmp/fake.sock", 47274, 5*time.Second, ping, nil)
	if err != nil {
		t.Fatalf("expected eventual success, got: %v", err)
	}
	if version != "late-ver" {
		t.Errorf("expected late-ver, got %q", version)
	}
}

// TestWaitForDaemonReady_DaemonNeverStarts verifies the check still FAILS
// honestly (with the helpful message) when the socket never answers — install
// must not be weakened into a no-op.
func TestWaitForDaemonReady_DaemonNeverStarts(t *testing.T) {
	ping := func(socketPath string) (string, error) {
		return "", fmt.Errorf("connection refused")
	}

	_, err := waitForDaemonReady("/tmp/never.sock", 47274, 500*time.Millisecond, ping, nil)
	if err == nil {
		t.Fatal("expected readiness to FAIL when the daemon never starts, but it succeeded")
	}
	if !contains(err.Error(), "did not respond on its socket") {
		t.Errorf("expected helpful socket error, got: %v", err)
	}
	if !contains(err.Error(), "grafel start") {
		t.Errorf("expected retry hint in error, got: %v", err)
	}
}

// TestInstallHealthTimeout_EnvOverride verifies the configurable budget.
func TestInstallHealthTimeout_EnvOverride(t *testing.T) {
	t.Setenv("GRAFEL_INSTALL_HEALTH_TIMEOUT_SEC", "120")
	if got := installHealthTimeout(10 * time.Second); got != 120*time.Second {
		t.Errorf("env override: want 120s, got %s", got)
	}

	t.Setenv("GRAFEL_INSTALL_HEALTH_TIMEOUT_SEC", "")
	if got := installHealthTimeout(0); got != defaultInstallHealthTimeout {
		t.Errorf("zero configured: want default %s, got %s", defaultInstallHealthTimeout, got)
	}
	if got := installHealthTimeout(15 * time.Second); got != 15*time.Second {
		t.Errorf("configured passthrough: want 15s, got %s", got)
	}
}
