package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/registry"
)

func groupWith(mode string, seconds int) *registry.GroupConfig {
	c := &registry.GroupConfig{}
	c.Features.ChangeDetection = mode
	c.Features.ChangePollIntervalSeconds = seconds
	return c
}

func TestResolveChangeDetection(t *testing.T) {
	cases := []struct {
		name     string
		cfgs     []*registry.GroupConfig
		wantPoll bool
		wantIv   time.Duration
	}{
		{"no groups", nil, false, 0},
		{"all fsnotify", []*registry.GroupConfig{groupWith("", 0), groupWith("fsnotify", 0)}, false, 0},
		{"auto does not enable poll in arm A", []*registry.GroupConfig{groupWith("auto", 0)}, false, 0},
		{"one poll group, default interval", []*registry.GroupConfig{groupWith("poll", 0)}, true, registry.DefaultChangePollInterval},
		{"one poll group among fsnotify ones", []*registry.GroupConfig{groupWith("fsnotify", 0), groupWith("poll", 5)}, true, 5 * time.Second},
		{"smallest interval wins", []*registry.GroupConfig{groupWith("poll", 60), groupWith("poll", 5), groupWith("poll", 120)}, true, 5 * time.Second},
		{"a non-polling group's interval is ignored", []*registry.GroupConfig{groupWith("fsnotify", 1), groupWith("poll", 60)}, true, 60 * time.Second},
		{"nil entries are skipped", []*registry.GroupConfig{nil, groupWith("poll", 10)}, true, 10 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			poll, iv := resolveChangeDetection(tc.cfgs)
			if poll != tc.wantPoll || iv != tc.wantIv {
				t.Fatalf("got (%v, %v), want (%v, %v)", poll, iv, tc.wantPoll, tc.wantIv)
			}
		})
	}
}

// writeFleet lays down an isolated GRAFEL_HOME holding registry.json plus one
// group config per body, and returns nothing — daemonChangeDetection reads it
// through the real registry loader.
func writeFleet(t *testing.T, groupBodies ...string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)

	type ref struct {
		Name       string `json:"name"`
		ConfigPath string `json:"config_path"`
	}
	var refs []ref
	for i, body := range groupBodies {
		name := "g" + string(rune('a'+i))
		p := filepath.Join(home, name+".json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref{Name: name, ConfigPath: p})
	}
	reg := map[string]any{"version": 1, "groups": refs}
	b, err := json.Marshal(reg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "registry.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// #6932 review, V3: resolveChangeDetection was split out and is well tested,
// but daemonChangeDetection — the half that actually READS the fleet — had one
// production call site and zero tests. Under a mutant returning (false, 0),
// `"change_detection": "poll"` in fleet.json did nothing and the whole suite
// stayed green: both ends of the boot wiring ungraded while only the extracted
// middle was pinned (#6533).
func TestDaemonChangeDetection_ReadsTheFleet(t *testing.T) {
	t.Run("poll group turns it on with its interval", func(t *testing.T) {
		writeFleet(t,
			`{"group":"ga","features":{"change_detection":"poll","change_poll_interval_seconds":7}}`,
			`{"group":"gb","features":{"watchers":true}}`,
		)
		poll, iv := daemonChangeDetection()
		if !poll || iv != 7*time.Second {
			t.Fatalf("got (%v, %v), want (true, 7s)", poll, iv)
		}
	})
	t.Run("no poll group leaves it off", func(t *testing.T) {
		writeFleet(t,
			`{"group":"ga","features":{"change_detection":"fsnotify"}}`,
			`{"group":"gb","features":{"change_detection":"auto"}}`,
		)
		poll, iv := daemonChangeDetection()
		if poll || iv != 0 {
			t.Fatalf("got (%v, %v), want (false, 0)", poll, iv)
		}
	})
	t.Run("an unreadable group config does not hide a poll group", func(t *testing.T) {
		writeFleet(t,
			`{ this is not json`,
			`{"group":"gb","features":{"change_detection":"poll"}}`,
		)
		poll, iv := daemonChangeDetection()
		if !poll || iv != registry.DefaultChangePollInterval {
			t.Fatalf("got (%v, %v), want (true, %v)", poll, iv, registry.DefaultChangePollInterval)
		}
	})
	t.Run("empty fleet", func(t *testing.T) {
		writeFleet(t)
		poll, iv := daemonChangeDetection()
		if poll || iv != 0 {
			t.Fatalf("got (%v, %v), want (false, 0)", poll, iv)
		}
	})
}
