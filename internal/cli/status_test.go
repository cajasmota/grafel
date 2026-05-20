package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/registry"
)

func TestStatusMissingFleetConfig(t *testing.T) {
	// Create a temporary archigraph home.
	tmpHome := t.TempDir()
	origHome := os.Getenv("ARCHIGRAPH_HOME")
	defer func() {
		if origHome != "" {
			os.Setenv("ARCHIGRAPH_HOME", origHome)
		} else {
			os.Unsetenv("ARCHIGRAPH_HOME")
		}
	}()
	os.Setenv("ARCHIGRAPH_HOME", tmpHome)

	// Create a registry entry with a missing config file.
	configDir := filepath.Join(tmpHome, ".config", "archigraph")
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
	if err := runStatus(&buf, ""); err != nil {
		t.Fatalf("status failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("config not found")) {
		t.Errorf("Expected 'config not found' in output, got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("archigraph cleanup")) {
		t.Errorf("Expected 'archigraph cleanup' suggestion in output, got: %s", output)
	}
}

func TestStatusExistingFleetConfig(t *testing.T) {
	// Create a temporary archigraph home.
	tmpHome := t.TempDir()
	origHome := os.Getenv("ARCHIGRAPH_HOME")
	defer func() {
		if origHome != "" {
			os.Setenv("ARCHIGRAPH_HOME", origHome)
		} else {
			os.Unsetenv("ARCHIGRAPH_HOME")
		}
	}()
	os.Setenv("ARCHIGRAPH_HOME", tmpHome)

	// Create a valid config file.
	configDir := filepath.Join(tmpHome, ".config", "archigraph")
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
	if err := runStatus(&buf, ""); err != nil {
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
