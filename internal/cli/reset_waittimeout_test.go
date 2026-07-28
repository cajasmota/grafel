package cli

// reset_waittimeout_test.go — the CLI-side bound on `reset`'s completion wait.
//
// Making `reset` request WaitForCompletion (#5991) turned an RPC that returned
// in milliseconds into one that blocks for the whole rebuild. The daemon's own
// bounds are generous — rebuildWaitTimeout == rebuildRPCTimeout == 2h, with
// faster exits only for a never-live engine (30s) or a live-then-stale one
// (2m) — and net/rpc over the UDS sets no client deadline. So a wedged but
// still-heartbeating engine could hold a `reset --quiet` for two hours in total
// silence, where it used to return instantly. The instant return WAS the bug,
// but the new blocking behaviour needs an escape hatch on scripted paths.
//
// `--wait-timeout` is that hatch. It bounds the CLI's wait only; it defaults to
// unbounded (0) so a legitimately long monorepo reset is never false-failed,
// and its error is deliberately weaker than the confirmed-failure message: the
// CLI gave up waiting, so it does NOT know whether the wipe+rebuild happened.

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// runResetBounded runs the reset command on a goroutine and fails the test if
// it has not returned within limit. Without this an UNBOUNDED mutant would hang
// the whole package until the go-test deadline instead of producing a clean,
// attributable failure — a mutation that only manifests as a hang is not a
// mutation the suite really catches.
func runResetBounded(t *testing.T, buf *bytes.Buffer, limit time.Duration, args ...string) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- runResetCmd(t, buf, args...) }()
	select {
	case err := <-done:
		return err
	case <-time.After(limit):
		t.Fatalf("reset did not return within %s — the wait was not bounded\noutput:\n%s", limit, buf.String())
		return nil
	}
}

// TestReset_WaitTimeout_QuietPath: --quiet is the silent path the bound exists
// for. It must fail with an "unconfirmed", not a "did not happen", message.
func TestReset_WaitTimeout_QuietPath(t *testing.T) {
	withSandboxHome(t)
	// release is never fed → the Rebuild RPC blocks forever.
	svc := &blockingRebuildService{release: make(chan rebuildRelease)}
	stubBlockingDaemon(t, svc)

	var buf bytes.Buffer
	err := runResetBounded(t, &buf, 10*time.Second, "mygroup", "--quiet", "--wait-timeout", "400ms")
	if err == nil {
		t.Fatalf("reset --quiet must fail when the bound expires, got nil\noutput:\n%s", buf.String())
	}
	msg := err.Error()
	for _, want := range []string{"mygroup", "no completion confirmed within", "may or may not"} {
		if !strings.Contains(strings.ToLower(msg), strings.ToLower(want)) {
			t.Fatalf("error %q must report an UNCONFIRMED outcome; missing %q", msg, want)
		}
	}
	// It must NOT claim the wipe definitely did not happen — the CLI stopped
	// waiting, it did not learn the outcome.
	if strings.Contains(msg, "was NOT wiped or rebuilt") {
		t.Fatalf("a give-up is not a confirmed failure; error overstates: %q", msg)
	}
}

// TestReset_WaitTimeout_ProgressPath: the bound is applied to the shared
// outcome channel, so it binds the progress paths too — not just --quiet.
func TestReset_WaitTimeout_ProgressPath(t *testing.T) {
	withSandboxHome(t)
	svc := &blockingRebuildService{release: make(chan rebuildRelease)}
	stubBlockingDaemon(t, svc)

	var buf bytes.Buffer
	err := runResetBounded(t, &buf, 10*time.Second, "mygroup", "--plain", "--wait-timeout", "400ms")
	if err == nil {
		t.Fatalf("reset must fail when the bound expires, got nil\noutput:\n%s", buf.String())
	}
	if !strings.Contains(err.Error(), "no completion confirmed within") {
		t.Fatalf("error %q must report an unconfirmed outcome", err.Error())
	}
}

// TestReset_WaitTimeout_DefaultIsUnbounded: without the flag the CLI must keep
// waiting for the daemon — a default cap would false-fail a legitimately long
// monorepo reset. Proven by outliving the largest internal give-up window.
func TestReset_WaitTimeout_DefaultIsUnbounded(t *testing.T) {
	withSandboxHome(t)
	svc := &blockingRebuildService{release: make(chan rebuildRelease, 1)}
	stubBlockingDaemon(t, svc)

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- runResetCmd(t, &buf, "mygroup", "--plain") }()

	select {
	case err := <-done:
		t.Fatalf("reset gave up on its own with no --wait-timeout (err=%v)\noutput:\n%s", err, buf.String())
	case <-time.After(sseGiveUpWindow + 2*time.Second):
	}
	svc.release <- rebuildRelease{}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("want exit 0 once the daemon confirms, got %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("reset never returned after the daemon answered")
	}
}

// TestReset_WaitTimeout_Invalid: a malformed duration is rejected up front
// rather than silently ignored (which would leave the user unbounded while
// believing they were bounded).
func TestReset_WaitTimeout_Invalid(t *testing.T) {
	withSandboxHome(t)
	svc := &blockingRebuildService{release: make(chan rebuildRelease)}
	stubBlockingDaemon(t, svc)

	var buf bytes.Buffer
	err := runResetBounded(t, &buf, 10*time.Second, "mygroup", "--quiet", "--wait-timeout", "banana")
	if err == nil {
		t.Fatal("a malformed --wait-timeout must be rejected, not ignored")
	}
	if !strings.Contains(err.Error(), "wait-timeout") {
		t.Fatalf("error %q must name the offending flag", err.Error())
	}
}
