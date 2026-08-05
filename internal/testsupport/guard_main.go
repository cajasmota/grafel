package testsupport

import (
	"fmt"
	"os"
)

// GuardRealHomeMain is the TestMain-time, fail-closed guard for an entire
// package whose tests resolve config/state/socket paths from the environment.
//
// It does NOT redirect HOME (individual tests must still call IsolateHome for
// that). Instead, when GRAFEL_TEST_REQUIRE_ISOLATED_HOME=1 is set (e.g. in
// CI), it aborts the whole test binary before a single test runs if the
// process started with HOME pointing at the real user home and no isolation
// env is in effect — turning a "this test corrupted my live config" incident
// into a loud, immediate failure.
//
// Usage:
//
//	func TestMain(m *testing.M) {
//	    testsupport.GuardRealHomeMain()
//	    os.Exit(m.Run())
//	}
//
// By default (env not set) it is a no-op so local `go test` keeps working even
// from a real HOME — the per-test IsolateHome/GuardRealHome guards still apply.
func GuardRealHomeMain() {
	if os.Getenv("GRAFEL_TEST_REQUIRE_ISOLATED_HOME") != "1" {
		return
	}
	// #6134 — GRAFEL_DAEMON_ROOT ALONE IS NOT SANDBOXING, and treating it as
	// such is what disarmed this guard against the very defect it exists for.
	//
	// EnvRoot is read by daemon.DefaultLayout, so it moves the socket, pidfile
	// and log dir. It is NOT read by daemon.StoreDir(), which resolves
	// $GRAFEL_HOME (else ~/.grafel) + "/store" — and the daemon's startup tail
	// runs MigrateToRefStore and PruneStaleGenerations against exactly that
	// path. So a binary with only EnvRoot set still RELOCATES and DELETES parts
	// of the developer's live store, which is the incident class this guard
	// advertises. Returning early on it made the guard blind to it.
	//
	// The store follows the HOME/GRAFEL_HOME axis, so that is what has to be
	// checked; GRAFEL_HOME set to anything is sufficient, since homeDir()
	// prefers it over the real home unconditionally.
	if os.Getenv(envGrafelHome) != "" {
		return
	}
	if eff := effectiveHome(); realUserHome != "" && eff == realUserHome {
		fmt.Fprintf(os.Stderr,
			"testsupport: REFUSING to run — HOME (%q) is the real user home and "+
				"GRAFEL_TEST_REQUIRE_ISOLATED_HOME=1. These tests can corrupt the live "+
				"~/.claude.json / ~/.codeium / ~/.grafel or kill the live daemon. Run under a "+
				"sandbox HOME (export HOME=$(mktemp -d); export GRAFEL_DAEMON_ROOT=$HOME/.grafel; "+
				"export GRAFEL_HOME=$HOME/.grafel). GRAFEL_DAEMON_ROOT alone is NOT enough: it "+
				"moves the socket/pid/log but not the store, which follows GRAFEL_HOME (#6134).\n",
			eff,
		)
		os.Exit(2)
	}
}
