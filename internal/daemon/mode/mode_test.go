package mode_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon/mode"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in      string
		want    mode.Mode
		wantErr bool
	}{
		{"background", mode.Background, false},
		{"workstation", mode.Workstation, false},
		{"readonly", mode.Readonly, false},
		{"BACKGROUND", "", true},
		{"", "", true},
		{"unknown", "", true},
	}
	for _, tc := range tests {
		got, err := mode.Parse(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("Parse(%q): expected error, got nil", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Parse(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestModeDefaultsBackground(t *testing.T) {
	d := mode.ModeDefaults(mode.Background)
	if d["ARCHIGRAPH_EAGER_ALGO"] != "false" {
		t.Errorf("background: ARCHIGRAPH_EAGER_ALGO = %q, want false", d["ARCHIGRAPH_EAGER_ALGO"])
	}
	if d["ARCHIGRAPH_HEAP_MAX_PCT"] != "60" {
		t.Errorf("background: ARCHIGRAPH_HEAP_MAX_PCT = %q, want 60", d["ARCHIGRAPH_HEAP_MAX_PCT"])
	}
	if _, ok := d["ARCHIGRAPH_EMBEDDING_URL"]; !ok {
		t.Error("background: ARCHIGRAPH_EMBEDDING_URL key should be present (empty string)")
	}
}

func TestModeDefaultsWorkstation(t *testing.T) {
	d := mode.ModeDefaults(mode.Workstation)
	if d["ARCHIGRAPH_EAGER_ALGO"] != "true" {
		t.Errorf("workstation: ARCHIGRAPH_EAGER_ALGO = %q, want true", d["ARCHIGRAPH_EAGER_ALGO"])
	}
	if d["ARCHIGRAPH_HEAP_MAX_PCT"] != "80" {
		t.Errorf("workstation: ARCHIGRAPH_HEAP_MAX_PCT = %q, want 80", d["ARCHIGRAPH_HEAP_MAX_PCT"])
	}
}

func TestModeDefaultsReadonly(t *testing.T) {
	d := mode.ModeDefaults(mode.Readonly)
	for _, k := range []string{"ARCHIGRAPH_DISABLE_WATCHER", "ARCHIGRAPH_DISABLE_REBUILD", "ARCHIGRAPH_DISABLE_ALGO"} {
		if d[k] != "true" {
			t.Errorf("readonly: %s = %q, want true", k, d[k])
		}
	}
}

func TestApplyDefaults_setsUnset(t *testing.T) {
	// Unset the key, apply background, verify it is set.
	os.Unsetenv("ARCHIGRAPH_EAGER_ALGO")
	t.Cleanup(func() { os.Unsetenv("ARCHIGRAPH_EAGER_ALGO") })

	mode.ApplyDefaults(mode.Background)
	if v := os.Getenv("ARCHIGRAPH_EAGER_ALGO"); v != "false" {
		t.Errorf("ARCHIGRAPH_EAGER_ALGO = %q after ApplyDefaults, want false", v)
	}
}

func TestApplyDefaults_doesNotOverrideExisting(t *testing.T) {
	os.Setenv("ARCHIGRAPH_EAGER_ALGO", "true")
	t.Cleanup(func() { os.Unsetenv("ARCHIGRAPH_EAGER_ALGO") })

	mode.ApplyDefaults(mode.Background)
	if v := os.Getenv("ARCHIGRAPH_EAGER_ALGO"); v != "true" {
		t.Errorf("ARCHIGRAPH_EAGER_ALGO = %q after ApplyDefaults, want true (existing override preserved)", v)
	}
}

func TestSaveLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.config.json")

	cfg := mode.Config{
		Mode:         mode.Background,
		EnvOverrides: map[string]string{"ARCHIGRAPH_HEAP_MAX_PCT": "50"},
	}
	if err := mode.SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got, err := mode.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.Mode != mode.Background {
		t.Errorf("loaded mode = %q, want background", got.Mode)
	}
	if got.EnvOverrides["ARCHIGRAPH_HEAP_MAX_PCT"] != "50" {
		t.Errorf("loaded override = %q, want 50", got.EnvOverrides["ARCHIGRAPH_HEAP_MAX_PCT"])
	}
}

func TestLoadConfig_missingFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := mode.LoadConfig(filepath.Join(dir, "does-not-exist.json"))
	if err != nil {
		t.Fatalf("LoadConfig missing file: %v", err)
	}
	if cfg.Mode != "" {
		t.Errorf("expected empty mode for missing file, got %q", cfg.Mode)
	}
}
