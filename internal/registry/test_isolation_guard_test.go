package registry

// test_isolation_guard_test.go — verifies the #5443 fail-closed guard:
// a test that would WRITE the registry/fleet config into the REAL user home
// panics LOUDLY, while a write into an isolated TempDir succeeds.

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestGuard_PanicsWhenWritingRealHome proves the guard fires when a fleet
// config write targets a path inside the genuine user home — the exact #5443
// clobber. We do NOT actually create the file; the guard must panic before any
// MkdirAll/WriteFile runs.
func TestGuard_PanicsWhenWritingRealHome(t *testing.T) {
	if realUserHomeAtInit == "" {
		t.Skip("no real user home captured; cannot exercise the escape path")
	}

	// A fleet-config path that lands inside the REAL user home (no isolation).
	escape := filepath.Join(realUserHomeAtInit, ".config", "grafel", "guardtest.fleet.json")

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected guard panic when writing fleet config to real home %q, got none", escape)
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "TEST SANDBOX ESCAPE") || !strings.Contains(msg, "IsolateHome") {
			t.Fatalf("panic message did not mention the guard / remediation: %q", msg)
		}
	}()

	// Must panic before writing anything.
	_ = SaveGroupConfig(escape, &GroupConfig{Name: "guardtest"})
	t.Fatalf("SaveGroupConfig returned without panicking — guard did not fire")
}

// TestGuard_AllowsWriteUnderIsolatedHome proves the guard is inert once the
// target is under a TempDir (the isolated case), and the write actually lands.
func TestGuard_AllowsWriteUnderIsolatedHome(t *testing.T) {
	dir := withHome(t) // sets GRAFEL_HOME + XDG_CONFIG_HOME under a TempDir
	cfgPath, err := ConfigPathFor("isolated")
	if err != nil {
		t.Fatal(err)
	}
	// On Windows t.TempDir is normally nested under %USERPROFILE%. The guard
	// must still recognize the OS temp tree as an isolated destination.
	if !pathWithin(cfgPath, dir) {
		t.Fatalf("isolated config path %q is not under temp root %q", cfgPath, dir)
	}

	if err := SaveGroupConfig(cfgPath, &GroupConfig{Name: "isolated"}); err != nil {
		t.Fatalf("SaveGroupConfig under isolated home should succeed: %v", err)
	}
	got, err := LoadGroupConfig(cfgPath)
	if err != nil || got.Name != "isolated" {
		t.Fatalf("roundtrip under isolated home failed: got=%+v err=%v", got, err)
	}
	_ = dir
}

func TestIsUnsafeTestWritePath_AllowsTempNestedUnderRealHome(t *testing.T) {
	realHome := filepath.Join(t.TempDir(), "user")
	tempRoot := filepath.Join(realHome, "AppData", "Local", "Temp")
	isolated := filepath.Join(tempRoot, "TestGuard", "cfg", "grafel", "group.fleet.json")
	live := filepath.Join(realHome, ".config", "grafel", "group.fleet.json")

	if isUnsafeTestWritePath(isolated, realHome, tempRoot) {
		t.Fatalf("isolated temp path %q was classified as a live-home write", isolated)
	}
	if !isUnsafeTestWritePath(live, realHome, tempRoot) {
		t.Fatalf("live config path %q was not rejected", live)
	}
}

// TestGuard_RegistrySaveAlsoGuarded proves saveTo (registry.json writer, used
// by AddGroup/RemoveGroup/Save) is guarded too — not just the fleet writer.
func TestGuard_RegistrySaveAlsoGuarded(t *testing.T) {
	if realUserHomeAtInit == "" {
		t.Skip("no real user home captured")
	}
	escape := filepath.Join(realUserHomeAtInit, ".grafel", "registry.json")
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected guard panic writing registry.json to real home %q", escape)
		}
	}()
	_ = saveTo(escape, &Registry{Version: 1})
	t.Fatalf("saveTo returned without panicking — guard did not fire")
}
