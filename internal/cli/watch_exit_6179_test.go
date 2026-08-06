package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWatchExitClassification pins the whole point of #6179 defect 2: every way
// `grafel watch` can terminate is deliberately classified as "launchd should
// respawn" or "launchd must not respawn". The plist's
// KeepAlive={SuccessfulExit:false} contract is only correct if this table is
// correct, because launchd reads the classification off the exit STATUS.
func TestWatchExitClassification(t *testing.T) {
	for _, tc := range []struct {
		reason      watchExitReason
		wantRespawn bool
		why         string
	}{
		{watchExitSignal, false,
			"SIGINT/SIGTERM is an operator (or reaper) instruction to stop; respawning fights it"},
		{watchExitRepoGone, false,
			"the repo path no longer stats — relaunching cannot make it exist"},
		{watchExitGaveUp, false,
			"the consecutive-index-failure ceiling is a deliberate give-up, not a crash"},
		{watchExitUsage, true,
			"a bad argv is a real non-zero failure; launchd never produces it, humans must see it"},
	} {
		got, known := watchExitRespawn[tc.reason]
		if !known {
			t.Fatalf("exit reason %q is unclassified — every exit path must be classified", tc.reason)
		}
		if got != tc.wantRespawn {
			t.Errorf("watchExitRespawn[%q] = %v, want %v — %s", tc.reason, got, tc.wantRespawn, tc.why)
		}
	}
}

// TestWatchExit_DeliberateReasonsReturnNil proves the classification actually
// reaches the process exit status: cli.Execute exits 1 on a non-nil error, so a
// deliberate exit must surface as nil.
func TestWatchExit_DeliberateReasonsReturnNil(t *testing.T) {
	for _, r := range []watchExitReason{watchExitSignal, watchExitRepoGone, watchExitGaveUp} {
		if err := watchExit(r, "because %d", 1); err != nil {
			t.Errorf("watchExit(%q) = %v, want nil (deliberate exit must be exit status 0)", r, err)
		}
	}
	if err := watchExit(watchExitUsage, "bad argv"); err == nil {
		t.Error("watchExit(usage) = nil, want a non-nil error (genuine failure must be exit status non-zero)")
	}
}

// TestRunWatch_MissingRepoExitsSuccessfully covers watch.go's stat guard. A
// watcher whose repo path is gone previously returned an error → exit 1 →
// KeepAlive respawn → exit 1 → forever, at launchd's 10s default. It must now
// exit 0 so launchd leaves it dead.
func TestRunWatch_MissingRepoExitsSuccessfully(t *testing.T) {
	home := withSandboxHome(t)
	missing := filepath.Join(home, "repos", "was-deleted")

	if err := runWatch(missing, "", time.Second); err != nil {
		t.Fatalf("runWatch on a missing repo returned %v; a vanished repo is a deliberate "+
			"give-up and must exit 0 so launchd does not respawn it (#6179)", err)
	}
}

// TestRunWatch_GiveUpAfterIndexFailuresExitsSuccessfully covers the
// consecutive-failure ceiling (shouldDie). #5140 made the watcher exit rather
// than tight-loop; #6179 makes that exit SUCCESSFUL so launchd's KeepAlive does
// not immediately undo it.
//
// This supersedes the old TestRunWatch_BacksOffAndDiesWhenDaemonUnreachable
// assertion that the return was non-nil — it must still exit, but cleanly.
func TestRunWatch_GiveUpAfterIndexFailuresExitsSuccessfully(t *testing.T) {
	home := withSandboxHome(t)
	repo := filepath.Join(home, "repos", "solo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}

	prevBackoff := activeWatchBackoff
	activeWatchBackoff = func() watchBackoffConfig {
		return watchBackoffConfig{base: time.Millisecond, max: 5 * time.Millisecond, maxConsecutive: 3}
	}
	t.Cleanup(func() { activeWatchBackoff = prevBackoff })

	prevTrigger := indexTriggerFunc
	indexTriggerFunc = func(string) error { return errDaemonNotRunning }
	t.Cleanup(func() { indexTriggerFunc = prevTrigger })

	done := make(chan error, 1)
	go func() { done <- runWatch(repo, "", 5*time.Millisecond) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runWatch gave up with %v; the give-up path is deliberate and must exit 0 "+
				"so launchd's KeepAlive does not respawn it every ThrottleInterval (#6179)", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("runWatch did not exit after repeated index failures (#5140 regression)")
	}
}

// TestRunWatch_SignalExitsSuccessfully lives in
// watch_exit_signal_6179_unix_test.go — it raises a real SIGTERM at itself,
// which Windows cannot express (see that file's header for why a portable
// rewrite would assert the opposite of #6179's contract). Everything else in
// this file builds and runs on all three platforms.

// TestWatchCmd_BadArgvStillFails is the counterweight: making deliberate exits
// succeed must not make EVERY exit succeed. A human typing `grafel watch` with
// no repo still gets a non-zero status.
func TestWatchCmd_BadArgvStillFails(t *testing.T) {
	cmd := newWatchCmd()
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Fatal("`grafel watch` with no repo argument must return a non-nil error (exit status 1)")
	}
	if err := cmd.RunE(cmd, []string{"a", "b"}); err == nil {
		t.Fatal("`grafel watch a b` must return a non-nil error (exit status 1)")
	}
}
