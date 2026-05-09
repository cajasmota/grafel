package dashboard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if c.PortRange.Min != 31000 || c.PortRange.Max != 31999 {
		t.Errorf("default range = %d-%d", c.PortRange.Min, c.PortRange.Max)
	}
	if c.Bind != "127.0.0.1" {
		t.Errorf("default bind = %q", c.Bind)
	}
	if c.Auth.Enabled {
		t.Errorf("default auth must be disabled")
	}
	if err := c.Validate(); err != nil {
		t.Errorf("default config invalid: %v", err)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		c    Config
		ok   bool
	}{
		{"min-too-low", Config{PortRange: PortRange{Min: 0, Max: 100}}, false},
		{"max-too-high", Config{PortRange: PortRange{Min: 1, Max: 70000}}, false},
		{"reversed", Config{PortRange: PortRange{Min: 200, Max: 100}}, false},
		{"ok", Config{PortRange: PortRange{Min: 31000, Max: 31999}}, true},
	}
	for _, tc := range cases {
		err := tc.c.Validate()
		if (err == nil) != tc.ok {
			t.Errorf("%s: ok=%v err=%v", tc.name, tc.ok, err)
		}
	}
}

func TestLoadConfig_MissingReturnsDefaults(t *testing.T) {
	// Point HomeDir at a tmp dir with no dashboard.json.
	tmp := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", tmp)
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if c.PortRange.Min != 31000 {
		t.Errorf("expected default min=31000 got %d", c.PortRange.Min)
	}
}

func TestLoadConfig_PartialMerge(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", tmp)
	if err := os.WriteFile(filepath.Join(tmp, "dashboard.json"),
		[]byte(`{"bind":"0.0.0.0"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if c.Bind != "0.0.0.0" {
		t.Errorf("bind = %q", c.Bind)
	}
	if c.PortRange.Min != 31000 || c.PortRange.Max != 31999 {
		t.Errorf("port range not defaulted: %+v", c.PortRange)
	}
}
