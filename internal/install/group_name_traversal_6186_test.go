package install_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/registry"
)

// TestApply_RejectsTraversalBeforeWritingConfig pins #6186 F6 (found on
// review) for install.Apply, one of the four real callers of
// registry.AddGroup. Apply computes registry.ConfigPathFor(opts.Group) and
// calls registry.SaveGroupConfig with it BEFORE registry.AddGroup ever
// validates the name; ConfigPathFor filepath.Joins the raw name, which
// collapses "..", so an unvalidated "../../pwned" writes a config file
// outside the intended config directory before the (too-late) AddGroup
// rejection.
func TestApply_RejectsTraversalBeforeWritingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // #6288: os.UserHomeDir() reads this on Windows
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	escapedPath, err := registry.ConfigPathFor("../../pwned")
	if err != nil {
		t.Fatalf("ConfigPathFor: %v", err)
	}

	repo := t.TempDir()
	cfg := &registry.GroupConfig{
		Name:  "../../pwned",
		Repos: []registry.Repo{{Slug: "r", Path: repo}},
	}
	if _, err := install.Apply(install.Options{
		Group:   "../../pwned",
		Config:  cfg,
		BinPath: "/usr/local/bin/grafel",
	}); err == nil {
		t.Fatal("Apply with group \"../../pwned\" succeeded, want rejection (#6186 F6)")
	}

	if _, statErr := os.Stat(escapedPath); statErr == nil {
		t.Fatalf("SaveGroupConfig wrote outside the config directory at %s despite the name "+
			"being rejected (#6186 F6)", escapedPath)
	}
}
