package main

import (
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
