package install_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/registry"
)

// purgeSandbox builds an isolated grafel home with a canary directory that
// lives OUTSIDE it, and seeds registry.json by writing the file directly.
//
// Writing the JSON by hand is the point, not a shortcut: registry.AddGroup
// validates the name, so a traversing entry cannot be created through the API
// any more. #6194 is only reachable through an entry that predates that
// validation, and hand-writing the file is the only faithful way to reproduce
// a grandfathered registry.
//
// Returns (grafelHome, canaryDir).
func purgeSandbox(t *testing.T, names ...string) (string, string) {
	t.Helper()
	base := t.TempDir()
	osHome := filepath.Join(base, "home")
	grafelHome := filepath.Join(osHome, ".grafel")
	if err := os.MkdirAll(filepath.Join(grafelHome, "groups"), 0o755); err != nil {
		t.Fatalf("mkdir grafel home: %v", err)
	}
	canary := filepath.Join(base, "canary")
	if err := os.MkdirAll(canary, 0o755); err != nil {
		t.Fatalf("mkdir canary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(canary, "precious.txt"), []byte("do not delete"), 0o644); err != nil {
		t.Fatalf("write canary file: %v", err)
	}
	t.Setenv("HOME", osHome)
	t.Setenv("USERPROFILE", osHome)
	t.Setenv("GRAFEL_HOME", grafelHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "xdg"))

	groups := make([]registry.GroupRef, 0, len(names))
	for _, n := range names {
		// A path inside the sandbox home: purge os.Remove()s ref.ConfigPath
		// too, and it must not be able to reach outside the fixture either.
		groups = append(groups, registry.GroupRef{
			Name:       n,
			ConfigPath: filepath.Join(grafelHome, "unused.fleet.json"),
		})
	}
	b, err := json.Marshal(registry.Registry{Version: 1, Groups: groups})
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(grafelHome, "registry.json"), b, 0o644); err != nil {
		t.Fatalf("write registry.json: %v", err)
	}
	return grafelHome, canary
}

// TestUninstallPurge_DoesNotDeleteOutsideStateRoot pins #6194 at the call site.
//
// install.Uninstall(group, purge=true) ran os.RemoveAll on
// registry.StateDirFor(group). filepath.Join collapses "..", so a
// grandfathered registry entry named "../../../canary" made --purge delete a
// directory that has nothing to do with grafel.
//
// The assertion is on the canary's survival, not on the returned error, so it
// cannot be satisfied by a change that merely reports a problem while still
// deleting.
func TestUninstallPurge_DoesNotDeleteOutsideStateRoot(t *testing.T) {
	const evil = "../../../canary"
	_, canary := purgeSandbox(t, evil)

	// Premise: the ungated derivation really does land on the canary. Without
	// this, a refusal below could be refusing something harmless.
	derived, err := registry.StateDirFor(evil)
	if err != nil {
		t.Fatalf("StateDirFor: %v", err)
	}
	if filepath.Clean(derived) != filepath.Clean(canary) {
		t.Fatalf("test premise broken: StateDirFor(%q) = %q, want the canary %q; the fixture no "+
			"longer reproduces the escape and this test would pass regardless of the fix",
			evil, derived, canary)
	}

	// Uninstall is expected to surface the refusal rather than silently skip:
	// a --purge that quietly leaves state behind is its own defect.
	if err := install.Uninstall(evil, true); err == nil {
		t.Errorf("Uninstall(%q, purge) returned nil; want a containment refusal so --purge does "+
			"not silently appear to have succeeded (#6194)", evil)
	}

	if _, err := os.Stat(filepath.Join(canary, "precious.txt")); err != nil {
		t.Fatalf("--purge deleted outside the state root: %s is gone (%v) (#6194)",
			filepath.Join(canary, "precious.txt"), err)
	}
}

// TestUninstallPurge_StillPurgesGrandfatheredContainedGroup is the mutant
// killer for "just stop purging".
//
// "my/group" fails registry.ValidateGroupName but is strictly inside the state
// root. It must still be purged: fixing #6194 with name validation at the
// delete site would make grandfathered registries unpurgeable, which is the
// regression the read-side validation split was written to avoid.
func TestUninstallPurge_StillPurgesGrandfatheredContainedGroup(t *testing.T) {
	const grandfathered = "my/group"
	grafelHome, canary := purgeSandbox(t, grandfathered)

	// Premise: the name is one name-validation rejects, so this test really
	// does distinguish containment from validation.
	if err := registry.ValidateGroupName(grandfathered); err == nil {
		t.Fatalf("test premise broken: ValidateGroupName(%q) accepted the name", grandfathered)
	}

	stateDir := filepath.Join(grafelHome, "groups", "my", "group")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	marker := filepath.Join(stateDir, "graph.db")
	if err := os.WriteFile(marker, []byte("state"), 0o644); err != nil {
		t.Fatalf("write state marker: %v", err)
	}

	if err := install.Uninstall(grandfathered, true); err != nil {
		t.Fatalf("Uninstall(%q, purge) = %v, want success for a contained name", grandfathered, err)
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("--purge did not remove the contained state dir %s (stat err = %v); the "+
			"containment check must refuse escapes, not disable purging", stateDir, err)
	}

	// And it still did not touch anything outside.
	if _, err := os.Stat(filepath.Join(canary, "precious.txt")); err != nil {
		t.Fatalf("canary damaged during a legitimate purge: %v", err)
	}
}
