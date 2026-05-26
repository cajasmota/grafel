package main

// testmain_test.go — TestMain with a defensive leak detector for #2083.
//
// Before all tests run: snapshot the entry-names in ~/.archigraph/store/.
// After all tests complete: assert no new entries were created.
//
// This guard catches tests that write per-repo state to the real store
// (i.e. tests that call code touching daemon.StateDirForRepo / StoreDir
// without first pinning ARCHIGRAPH_DAEMON_ROOT or ARCHIGRAPH_HOME to a
// temp dir).

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestMain(m *testing.M) {
	storeDir := realStoreDir()
	before := snapshotStore(storeDir)

	code := m.Run()

	after := snapshotStore(storeDir)
	leaked := diffSets(before, after)
	if len(leaked) > 0 {
		fmt.Fprintf(os.Stderr,
			"\n[#2083 leak-detector] %d new entr%s in real store %s after test run:\n",
			len(leaked), pluralY(len(leaked)), storeDir)
		for _, e := range leaked {
			fmt.Fprintf(os.Stderr, "  %s\n", e)
		}
		fmt.Fprintf(os.Stderr,
			"Each leaking test must pin ARCHIGRAPH_DAEMON_ROOT (or ARCHIGRAPH_HOME) "+
				"to a t.TempDir() before calling any code that invokes daemon.StateDirForRepo.\n\n")
		// Use exit code 1 to signal failure without overriding a passing run
		// that has leaks — we report but let the caller decide.
		if code == 0 {
			code = 1
		}
	}

	os.Exit(code)
}

// realStoreDir returns the store path the daemon would use when no env
// overrides are set — i.e. the real ~/.archigraph/store/ on this host.
// We read it without setting any env vars so we get the user's real path.
func realStoreDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".archigraph", "store")
}

// snapshotStore returns the set of top-level entry names in dir.
// Returns an empty set (not nil) when dir does not exist.
func snapshotStore(dir string) map[string]struct{} {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return map[string]struct{}{}
	}
	set := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		set[e.Name()] = struct{}{}
	}
	return set
}

// diffSets returns names that appear in after but not in before, sorted.
func diffSets(before, after map[string]struct{}) []string {
	var leaked []string
	for name := range after {
		if _, ok := before[name]; !ok {
			leaked = append(leaked, name)
		}
	}
	sort.Strings(leaked)
	return leaked
}

func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
