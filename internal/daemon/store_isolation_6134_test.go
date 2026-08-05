package daemon_test

// store_isolation_6134_test.go — #6134, the hygiene half.
//
// Run()'s startup tail does two things to StoreDir(): MigrateToRefStore, which
// RELOCATES legacy flat-layout artifacts into per-ref sub-directories, and
// PruneStaleGenerations, which DELETES generations beyond keepN. Both take
// StoreDir() — homeDir()/store — not anything derived from cfg.Layout.
//
// isolateDaemonEnv used to set only GRAFEL_DAEMON_ROOT, which DefaultLayout
// reads and StoreDir does not. So every test in this package that starts a
// daemon in-process ran a migration and a pruning pass over the developer's
// real ~/.grafel/store — concurrently with the real daemon serving MCP out of
// it. `go test ./internal/daemon/` mutated live user state.
//
// The assertion below is on the resolved path, not on a call count, and it is
// bidirectional: the store must be INSIDE the sandbox root and must NOT be
// under the real user home. Either half alone is satisfiable by an accident —
// a store under /tmp that is not this test's root would pass the second check,
// and a root that happened to sit under the home dir would pass the first.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestIsolateDaemonEnvAlsoIsolatesTheStore is the RED/GREEN test for the
// #6134 hygiene defect. Before the GRAFEL_HOME pin in isolateDaemonEnv this
// fails with a StoreDir under the developer's real home.
func TestIsolateDaemonEnvAlsoIsolatesTheStore(t *testing.T) {
	root := isolateDaemonEnv(t)

	store := daemon.StoreDir()
	if store == "" {
		t.Fatal("StoreDir() is empty; server.go skips the migration entirely on empty, so this test would be vacuous")
	}

	// (1) The store must live inside the sandbox this test was handed.
	rel, err := filepath.Rel(root, store)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("StoreDir() = %q is not under the isolated daemon root %q (rel=%q, err=%v): "+
			"MigrateToRefStore and PruneStaleGenerations would run against it from daemon.Run",
			store, root, rel, err)
	}

	// (2) And it must not be under the real user home, independently of (1) —
	// this is the check that actually names the damage.
	if realHome := testsupport.RealUserHome(); realHome != "" {
		hrel, herr := filepath.Rel(realHome, store)
		if herr == nil && !strings.HasPrefix(hrel, "..") && hrel != "." {
			t.Fatalf("StoreDir() = %q resolves under the REAL user home %q — running this suite "+
				"migrates and prunes the developer's live store (#6134)", store, realHome)
		}
	}

	// (3) Fixture validity: the layout the daemon would actually use must agree
	// with the root, so a passing (1)+(2) cannot come from an isolation that
	// moved the store somewhere the daemon does not look.
	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if layout.Root != root {
		t.Fatalf("DefaultLayout().Root = %q, want the isolated root %q", layout.Root, root)
	}
}

// TestIsolateDaemonEnvStoreIsWritableSandbox proves the isolated store path is
// a usable location rather than merely a different string — a pin that points
// at an unwritable path would "isolate" the suite by making every daemon
// startup log a warning instead of doing the work.
func TestIsolateDaemonEnvStoreIsWritableSandbox(t *testing.T) {
	isolateDaemonEnv(t)

	store := daemon.StoreDir()
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatalf("mkdir isolated store %q: %v", store, err)
	}
	probe := filepath.Join(store, "probe.txt")
	if err := os.WriteFile(probe, []byte("6134"), 0o600); err != nil {
		t.Fatalf("write into isolated store %q: %v", probe, err)
	}
	got, err := os.ReadFile(probe)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "6134" {
		t.Fatalf("read back %q, want %q", got, "6134")
	}
}
