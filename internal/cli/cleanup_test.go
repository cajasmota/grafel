package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/registry"
)

func TestCleanupDryRun(t *testing.T) {
	// Create a temporary archigraph home.
	tmpHome := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", tmpHome)
	configDir := filepath.Join(tmpHome, "config")
	t.Setenv("XDG_CONFIG_HOME", configDir)

	// Create config files.
	configArchDir := filepath.Join(configDir, "archigraph")
	os.MkdirAll(configArchDir, 0o755)
	existingConfig := filepath.Join(configArchDir, "existing.fleet.json")
	os.WriteFile(existingConfig, []byte(`{"name":"existing"}`), 0o644)

	// Manually create registry.json with one valid and one orphaned entry.
	// Use json.MarshalIndent so Windows path separators are properly escaped.
	regPath := filepath.Join(tmpHome, "registry.json")
	os.MkdirAll(filepath.Dir(regPath), 0o755)
	regObj := registry.Registry{
		Version: 1,
		Groups: []registry.GroupRef{
			{Name: "existing", ConfigPath: existingConfig},
			{Name: "orphaned", ConfigPath: filepath.Join(configArchDir, "orphaned.fleet.json")},
		},
	}
	registryData, err := json.MarshalIndent(regObj, "", "  ")
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	os.WriteFile(regPath, registryData, 0o644)

	// Run cleanup with --dry-run.
	var buf bytes.Buffer
	if err := runCleanup(&buf, true, 0); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("Found 1 orphaned entries")) {
		t.Errorf("Expected \"Found 1 orphaned entries\", got output: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("orphaned")) {
		t.Errorf("Expected group name \"orphaned\", got output: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("--dry-run")) {
		t.Errorf("Expected reference to --dry-run flag, got output: %s", output)
	}

	// Verify the registry is unchanged.
	regAfter, _ := registry.Load()
	if len(regAfter.Groups) != 2 {
		t.Errorf("Expected 2 groups after dry-run, got %d", len(regAfter.Groups))
	}
}

func TestCleanupRemove(t *testing.T) {
	// Create a temporary archigraph home.
	tmpHome := t.TempDir()
	origHome := os.Getenv("ARCHIGRAPH_HOME")
	origXDG := os.Getenv("XDG_CONFIG_HOME")
	defer func() {
		if origHome != "" {
			os.Setenv("ARCHIGRAPH_HOME", origHome)
		} else {
			os.Unsetenv("ARCHIGRAPH_HOME")
		}
		if origXDG != "" {
			os.Setenv("XDG_CONFIG_HOME", origXDG)
		} else {
			os.Unsetenv("XDG_CONFIG_HOME")
		}
	}()
	os.Setenv("ARCHIGRAPH_HOME", tmpHome)
	configDir := filepath.Join(tmpHome, "config")
	os.Setenv("XDG_CONFIG_HOME", configDir)

	// Create an existing config file.
	configArchDir := filepath.Join(configDir, "archigraph")
	os.MkdirAll(configArchDir, 0o755)
	existingConfig := filepath.Join(configArchDir, "existing.fleet.json")
	os.WriteFile(existingConfig, []byte(`{"name":"existing"}`), 0o644)

	// Create a registry with one valid and one orphaned entry.
	reg := &registry.Registry{
		Version: 1,
		Groups: []registry.GroupRef{
			{Name: "existing", ConfigPath: existingConfig},
			{Name: "orphaned", ConfigPath: filepath.Join(configArchDir, "orphaned.fleet.json")},
		},
	}
	registry.Save(reg)

	// Run cleanup without --dry-run.
	var buf bytes.Buffer
	if err := runCleanup(&buf, false, 0); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	output := buf.String()
	if err := checkSubstring(output, "Removed 1 orphaned entries"); err != nil {
		t.Errorf("Expected \"Removed 1 orphaned entries\": %v", err)
	}

	// Verify the registry is updated.
	regAfter, _ := registry.Load()
	if len(regAfter.Groups) != 1 {
		t.Errorf("Expected 1 group after cleanup, got %d", len(regAfter.Groups))
	}
	if regAfter.Groups[0].Name != "existing" {
		t.Errorf("Expected group name \"existing\", got %s", regAfter.Groups[0].Name)
	}
}

func TestCleanupNoOrphans(t *testing.T) {
	// Create a temporary archigraph home.
	tmpHome := t.TempDir()
	origHome := os.Getenv("ARCHIGRAPH_HOME")
	origXDG := os.Getenv("XDG_CONFIG_HOME")
	defer func() {
		if origHome != "" {
			os.Setenv("ARCHIGRAPH_HOME", origHome)
		} else {
			os.Unsetenv("ARCHIGRAPH_HOME")
		}
		if origXDG != "" {
			os.Setenv("XDG_CONFIG_HOME", origXDG)
		} else {
			os.Unsetenv("XDG_CONFIG_HOME")
		}
	}()
	os.Setenv("ARCHIGRAPH_HOME", tmpHome)
	configDir := filepath.Join(tmpHome, "config")
	os.Setenv("XDG_CONFIG_HOME", configDir)

	// Create an existing config file.
	configArchDir := filepath.Join(configDir, "archigraph")
	os.MkdirAll(configArchDir, 0o755)
	existingConfig := filepath.Join(configArchDir, "existing.fleet.json")
	os.WriteFile(existingConfig, []byte(`{"name":"existing"}`), 0o644)

	// Create a registry with only valid entries.
	reg := &registry.Registry{
		Version: 1,
		Groups: []registry.GroupRef{
			{Name: "existing", ConfigPath: existingConfig},
		},
	}
	registry.Save(reg)

	// Run cleanup.
	var buf bytes.Buffer
	if err := runCleanup(&buf, true, 0); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	output := buf.String()
	if err := checkSubstring(output, "No orphaned registry entries found"); err != nil {
		t.Errorf("Expected \"No orphaned registry entries found\": %v", err)
	}
}

func checkSubstring(haystack, needle string) error {
	if !bytes.Contains([]byte(haystack), []byte(needle)) {
		return errors.New("substring not found")
	}
	return nil
}
