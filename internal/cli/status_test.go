package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/registry"
)

func TestStatusMissingFleetConfig(t *testing.T) {
	// Create a temporary grafel home.
	tmpHome := t.TempDir()
	origHome := os.Getenv("GRAFEL_HOME")
	defer func() {
		if origHome != "" {
			os.Setenv("GRAFEL_HOME", origHome)
		} else {
			os.Unsetenv("GRAFEL_HOME")
		}
	}()
	// HOME too, not just GRAFEL_HOME: runStatus now reports the per-repo
	// watcher fleet, and the unit directory is resolved from HOME
	// (~/Library/LaunchAgents). With only GRAFEL_HOME redirected this test
	// globbed the developer's REAL 140 watcher plists and ran a `launchctl
	// list` per unit — read-only, but machine-variable and 140 process spawns.
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))
	os.Setenv("GRAFEL_HOME", tmpHome)

	// Create a registry entry with a missing config file.
	configDir := filepath.Join(tmpHome, ".config", "grafel")
	os.MkdirAll(configDir, 0o755)
	missingConfig := filepath.Join(configDir, "missing.fleet.json")

	reg := &registry.Registry{
		Version: 1,
		Groups: []registry.GroupRef{
			{Name: "missing", ConfigPath: missingConfig},
		},
	}
	registry.Save(reg)

	// Run status.
	var buf bytes.Buffer
	if err := runStatus(&buf, "", "", false); err != nil {
		t.Fatalf("status failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("config not found")) {
		t.Errorf("Expected 'config not found' in output, got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("grafel cleanup")) {
		t.Errorf("Expected 'grafel cleanup' suggestion in output, got: %s", output)
	}
}

func TestStatusExistingFleetConfig(t *testing.T) {
	// Create a temporary grafel home.
	tmpHome := t.TempDir()
	origHome := os.Getenv("GRAFEL_HOME")
	defer func() {
		if origHome != "" {
			os.Setenv("GRAFEL_HOME", origHome)
		} else {
			os.Unsetenv("GRAFEL_HOME")
		}
	}()
	// HOME too, not just GRAFEL_HOME: runStatus now reports the per-repo
	// watcher fleet, and the unit directory is resolved from HOME
	// (~/Library/LaunchAgents). With only GRAFEL_HOME redirected this test
	// globbed the developer's REAL 140 watcher plists and ran a `launchctl
	// list` per unit — read-only, but machine-variable and 140 process spawns.
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))
	os.Setenv("GRAFEL_HOME", tmpHome)

	// Create a valid config file.
	configDir := filepath.Join(tmpHome, ".config", "grafel")
	os.MkdirAll(configDir, 0o755)
	validConfig := filepath.Join(configDir, "valid.fleet.json")
	os.WriteFile(validConfig, []byte(`{
		"name": "valid",
		"repos": [
			{"slug": "test", "path": "/tmp/test"}
		]
	}`), 0o644)

	reg := &registry.Registry{
		Version: 1,
		Groups: []registry.GroupRef{
			{Name: "valid", ConfigPath: validConfig},
		},
	}
	registry.Save(reg)

	// Run status.
	var buf bytes.Buffer
	if err := runStatus(&buf, "", "", false); err != nil {
		t.Fatalf("status failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("Group: valid")) {
		t.Errorf("Expected 'Group: valid' in output, got: %s", output)
	}
	if bytes.Contains([]byte(output), []byte("config not found")) {
		t.Errorf("Unexpected 'config not found' in output: %s", output)
	}
}
