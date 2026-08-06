package registry

import (
	"os"
	"path/filepath"
	"testing"
)

// #6186: slugify sanitises the repo segment of a watcher Label but not the
// group segment, and AddGroup only checked non-empty. A group named "g/h"
// makes Unit.Label() = "com.grafel.watcher.g/h.<repo>", and watchers.Write
// does os.MkdirAll(filepath.Dir(path)), so that group creates a DIRECTORY
// inside ~/Library/LaunchAgents (which launchd does not scan) and the
// watcher is silently never registered. A group named "../x" escapes
// ~/Library/LaunchAgents entirely.
//
// Fix: validate at the registry boundary (AddGroup), not by sanitising at
// plist-write time, so the constraint is visible when the group is created.
// This intentionally does NOT touch slugify or Unit.Label() (#6183 owns
// that).
func TestAddGroup_RejectsPathSeparatorInName(t *testing.T) {
	home := withHome(t)
	cfgPath := filepath.Join(home, "xdg", "grafel", "g.fleet.json")
	writeFleetFixture(t, cfgPath)

	for _, bad := range []string{"g/h", "g\\h", "../escape", "."} {
		if err := AddGroup(bad, cfgPath); err == nil {
			t.Errorf("AddGroup(%q) succeeded; want rejection — a group name containing a path "+
				"separator turns into a directory inside ~/Library/LaunchAgents (#6186)", bad)
		}
	}

	groups, _ := Groups()
	if len(groups) != 0 {
		t.Fatalf("expected no groups registered after rejected names, got %d: %+v", len(groups), groups)
	}
}

// TestAddGroup_AcceptsOrdinaryNames pins that the fix does not overreach:
// group names derived from real directory basenames (which commonly contain
// spaces, dots, and underscores) must keep working.
func TestAddGroup_AcceptsOrdinaryNames(t *testing.T) {
	home := withHome(t)
	for _, ok := range []string{"grafel", "my-group", "my_group", "my.group", "My Group"} {
		cfgPath := filepath.Join(home, "xdg", "grafel", ok+".fleet.json")
		writeFleetFixture(t, cfgPath)
		if err := AddGroup(ok, cfgPath); err != nil {
			t.Errorf("AddGroup(%q) failed, want success: %v", ok, err)
		}
	}
}

func writeFleetFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
}
