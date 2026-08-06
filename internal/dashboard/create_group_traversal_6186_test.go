package dashboard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/registry"
)

// TestCreateGroup_RejectsTraversalBeforeWritingConfig pins #6186 F6 (found on
// review): every real caller of registry.AddGroup — including this one,
// liveStore.CreateGroup — computes the config path with
// registry.ConfigPathFor(name) and calls registry.SaveGroupConfig BEFORE
// registry.AddGroup ever validates the name. ConfigPathFor filepath.Joins
// the raw name, which collapses "..", so
//
//	ConfigPathFor("../../pwned") = <XDG config dir>/../../pwned.fleet.json
//	                              = <two levels up>/pwned.fleet.json
//
// and SaveGroupConfig wrote that file BEFORE AddGroup's rejection ever ran —
// #6186's own headline traversal input still wrote a file outside the
// config directory; the rejection only left the stray file behind.
//
// The fix validates the name before the first write (registry.
// ValidateGroupName, called ahead of SaveGroupConfig), not just at AddGroup.
func TestCreateGroup_RejectsTraversalBeforeWritingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))

	// Compute the exact path the unvalidated code would have written to, the
	// same way the reviewer's probe did.
	escapedPath, err := registry.ConfigPathFor("../../pwned")
	if err != nil {
		t.Fatalf("ConfigPathFor: %v", err)
	}

	if _, err := (liveStore{}).CreateGroup("../../pwned"); err == nil {
		t.Fatal("CreateGroup(\"../../pwned\") succeeded, want rejection (#6186 F6)")
	}

	if _, statErr := os.Stat(escapedPath); statErr == nil {
		t.Fatalf("SaveGroupConfig wrote outside the config directory at %s despite the name "+
			"being rejected (#6186 F6)", escapedPath)
	}
}
