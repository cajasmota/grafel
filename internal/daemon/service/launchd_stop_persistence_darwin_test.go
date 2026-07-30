//go:build darwin

package service

// launchd_stop_persistence_darwin_test.go pins the #6044 review findings:
//
//   - item 2: the persistent-stop decision (launchctl disable paired with
//     Unload, launchctl enable paired with Load) had zero coverage — both
//     were bare, result-discarding exec.Command(...).Run() calls, so
//     deleting either line outright left the whole suite green.
//   - item 3: the CLI's "will not restart... even across reboot" claim was
//     unconditional even though the only thing making it true (disable) had
//     its result thrown away.
//
// These tests call the REAL launchdManager.Unload()/Load() and the REAL
// darwin stopService()/uninstall() functions — not a hand-rolled helper —
// with launchctlBootout/launchctlBootstrap/launchctlDisable/launchctlEnable
// all stubbed. That is what makes it safe to run on a dev machine where
// gui/$UID/com.grafel.daemon may be the user's actual live daemon: nothing
// here ever execs a real launchctl bootout/bootstrap/disable/enable.

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// launchctlSpy records every call made through the four seams, in order,
// with the uid (and plistPath, for bootstrap) each was called with.
type launchctlSpy struct {
	order []string

	bootoutCalls, bootstrapCalls, disableCalls, enableCalls int
	lastDisableUID, lastEnableUID, lastBootoutUID           string
	lastBootstrapUID, lastBootstrapPlistPath                string

	bootoutErr   error
	bootstrapErr error
	bootstrapOut []byte
	disableErr   error
	enableErr    error
}

// stubLaunchctl overrides all four launchctl seams for the duration of a
// test and restores them on cleanup.
func stubLaunchctl(t *testing.T) *launchctlSpy {
	t.Helper()
	spy := &launchctlSpy{}

	origBootout, origBootstrap := launchctlBootout, launchctlBootstrap
	origDisable, origEnable := launchctlDisable, launchctlEnable
	t.Cleanup(func() {
		launchctlBootout, launchctlBootstrap = origBootout, origBootstrap
		launchctlDisable, launchctlEnable = origDisable, origEnable
	})

	launchctlBootout = func(uid string) error {
		spy.order = append(spy.order, "bootout")
		spy.bootoutCalls++
		spy.lastBootoutUID = uid
		return spy.bootoutErr
	}
	launchctlBootstrap = func(uid, plistPath string) ([]byte, error) {
		spy.order = append(spy.order, "bootstrap")
		spy.bootstrapCalls++
		spy.lastBootstrapUID = uid
		spy.lastBootstrapPlistPath = plistPath
		return spy.bootstrapOut, spy.bootstrapErr
	}
	launchctlDisable = func(uid string) error {
		spy.order = append(spy.order, "disable")
		spy.disableCalls++
		spy.lastDisableUID = uid
		return spy.disableErr
	}
	launchctlEnable = func(uid string) error {
		spy.order = append(spy.order, "enable")
		spy.enableCalls++
		spy.lastEnableUID = uid
		return spy.enableErr
	}
	return spy
}

// darwinTestOpts builds Options against a sandboxed HOME (so plistPath()
// never touches the real ~/Library/LaunchAgents/com.grafel.daemon.plist) and
// an unreachable socket path (so Probe() deterministically reports "down"
// without any real daemon involved).
func darwinTestOpts(t *testing.T) Options {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return Options{
		BinPath:    filepath.Join(home, "bin", "grafel"),
		SocketPath: filepath.Join(home, "sockets", "daemon.sock"), // never listened on
		LogDir:     filepath.Join(home, "logs"),
	}
}

func indexOfStr(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// --- Unload: bootout + disable pairing (item 2) ----------------------------

func TestUnload_CallsBootoutThenDisable_WithMatchingUID(t *testing.T) {
	spy := stubLaunchctl(t)
	opts := darwinTestOpts(t)
	sm, err := newServiceManager(opts)
	if err != nil {
		t.Fatalf("newServiceManager: %v", err)
	}
	lm := sm.(*launchdManager)

	if err := lm.Unload(); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if spy.bootoutCalls != 1 {
		t.Fatalf("expected exactly one bootout call, got %d", spy.bootoutCalls)
	}
	if spy.disableCalls != 1 {
		t.Fatalf("expected exactly one disable call (the #6044 persistence pairing) — "+
			"deleting this call previously left the whole suite green, got %d", spy.disableCalls)
	}
	bi, di := indexOfStr(spy.order, "bootout"), indexOfStr(spy.order, "disable")
	if bi < 0 || di < 0 || bi > di {
		t.Fatalf("expected bootout before disable, got order %v", spy.order)
	}
	wantUID := strconv.Itoa(os.Getuid())
	if spy.lastBootoutUID != wantUID {
		t.Errorf("bootout uid = %q, want %q", spy.lastBootoutUID, wantUID)
	}
	if spy.lastDisableUID != wantUID {
		t.Errorf("disable uid = %q, want %q", spy.lastDisableUID, wantUID)
	}
}

// TestUnload_RecordsDisableError pins that a genuine disable failure is
// preserved (not discarded) on lastDisableErr for stopService to inspect,
// while Unload's own return contract stays best-effort (nil) — a disable
// hiccup must not fail ensureLoaded's restart/install path.
func TestUnload_RecordsDisableError(t *testing.T) {
	spy := stubLaunchctl(t)
	spy.disableErr = os.ErrPermission
	opts := darwinTestOpts(t)
	sm, err := newServiceManager(opts)
	if err != nil {
		t.Fatalf("newServiceManager: %v", err)
	}
	lm := sm.(*launchdManager)

	if err := lm.Unload(); err != nil {
		t.Fatalf("Unload must stay best-effort even when disable fails, got: %v", err)
	}
	if lm.lastDisableErr == nil {
		t.Fatal("expected lastDisableErr to record the disable failure, got nil")
	}
}

// --- Load: enable + bootstrap pairing (item 2) ------------------------------

func TestLoad_CallsEnableBeforeBootstrap_WithMatchingArgs(t *testing.T) {
	spy := stubLaunchctl(t)
	opts := darwinTestOpts(t)
	sm, err := newServiceManager(opts)
	if err != nil {
		t.Fatalf("newServiceManager: %v", err)
	}
	lm := sm.(*launchdManager)
	if err := lm.WriteUnit(); err != nil {
		t.Fatalf("WriteUnit: %v", err)
	}

	if err := lm.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if spy.enableCalls != 1 {
		t.Fatalf("expected exactly one enable call (the #6044 persistence pairing) — "+
			"deleting this call previously left the whole suite green, got %d", spy.enableCalls)
	}
	if spy.bootstrapCalls != 1 {
		t.Fatalf("expected exactly one bootstrap call, got %d", spy.bootstrapCalls)
	}
	ei, bi := indexOfStr(spy.order, "enable"), indexOfStr(spy.order, "bootstrap")
	if ei < 0 || bi < 0 || ei > bi {
		t.Fatalf("expected enable before bootstrap (otherwise RunAtLoad silently no-ops on a disabled "+
			"service and WaitReady times out with no explanation), got order %v", spy.order)
	}
	wantUID := strconv.Itoa(os.Getuid())
	if spy.lastEnableUID != wantUID {
		t.Errorf("enable uid = %q, want %q", spy.lastEnableUID, wantUID)
	}
	if spy.lastBootstrapUID != wantUID {
		t.Errorf("bootstrap uid = %q, want %q", spy.lastBootstrapUID, wantUID)
	}
	if spy.lastBootstrapPlistPath != lm.plistPath {
		t.Errorf("bootstrap plist path = %q, want %q", spy.lastBootstrapPlistPath, lm.plistPath)
	}
}

// --- stopService: honesty about persistence (item 3) ------------------------

// TestStopService_Success_WhenDisableSucceeds is the fixture-validity anchor:
// a well-behaved disable must let stopService report success.
func TestStopService_Success_WhenDisableSucceeds(t *testing.T) {
	stubLaunchctl(t) // all seams succeed (zero-value errors)
	opts := darwinTestOpts(t)

	if _, err := stopService(opts); err != nil {
		t.Fatalf("stopService: %v", err)
	}
}

// TestStopService_ReportsFailure_WhenDisableFails is the #6044 review item-3
// pin: stopService must NOT claim the persistent-stop guarantee when the
// launchctl disable call that is the only thing making it true actually
// failed. Before this fix the CLI printed "will not restart... even across
// reboot" unconditionally over a discarded error.
func TestStopService_ReportsFailure_WhenDisableFails(t *testing.T) {
	spy := stubLaunchctl(t)
	spy.disableErr = os.ErrPermission
	opts := darwinTestOpts(t)

	_, err := stopService(opts)
	if err == nil {
		t.Fatal("expected stopService to fail when the persistence-critical disable call failed, " +
			"got success (which is what let the CLI assert a guarantee it never verified)")
	}
}

// --- uninstall: no disabled residue after a full teardown (item 6) ---------

// TestUninstall_ReEnablesAfterTeardown pins review item 6: a full uninstall
// removes the plist entirely, so a `launchctl disable` override left behind
// by a prior `grafel stop` (or by teardown's own Unload call) would have
// nothing on disk to explain it. uninstall must clear that override.
func TestUninstall_ReEnablesAfterTeardown(t *testing.T) {
	spy := stubLaunchctl(t)
	opts := darwinTestOpts(t)
	sm, err := newServiceManager(opts)
	if err != nil {
		t.Fatalf("newServiceManager: %v", err)
	}
	if err := sm.WriteUnit(); err != nil {
		t.Fatalf("WriteUnit: %v", err)
	}

	if err := uninstall(opts); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if spy.enableCalls < 1 {
		t.Fatalf("expected uninstall to call enable at least once to clear the disable override "+
			"left by teardown's Unload, got %d calls (order: %v)", spy.enableCalls, spy.order)
	}
	// The plist must still be gone — re-enabling must not resurrect it.
	if _, statErr := os.Stat(sm.(*launchdManager).plistPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected the plist to remain removed after uninstall, stat err = %v", statErr)
	}
}
