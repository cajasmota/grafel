package registry

import (
	"encoding/json"
	"testing"
	"time"
)

func TestChangeDetectionMode_DefaultIsFsnotify(t *testing.T) {
	var c GroupConfig
	if got := c.ChangeDetectionMode(); got != ChangeDetectionFsnotify {
		t.Fatalf("unset change_detection must mean fsnotify, got %q", got)
	}
}

func TestChangeDetectionMode_ParsesFromJSON(t *testing.T) {
	cases := map[string]string{
		`{"features":{"change_detection":"poll"}}`:     ChangeDetectionPoll,
		`{"features":{"change_detection":"fsnotify"}}`: ChangeDetectionFsnotify,
		`{"features":{"change_detection":"auto"}}`:     ChangeDetectionAuto,
		`{"features":{"change_detection":"  POLL  "}}`: ChangeDetectionPoll,
		`{"features":{"change_detection":"nonsense"}}`: ChangeDetectionFsnotify,
		`{"features":{"change_detection":""}}`:         ChangeDetectionFsnotify,
		`{"features":{"track_worktrees":true}}`:        ChangeDetectionFsnotify,
	}
	for body, want := range cases {
		var c GroupConfig
		if err := json.Unmarshal([]byte(body), &c); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if got := c.ChangeDetectionMode(); got != want {
			t.Fatalf("%s: mode = %q, want %q", body, got, want)
		}
	}
}

// ARM A ships fsnotify and poll only. "auto" is accepted and documented, and
// until arm B lands (the inotify-budget probe and the announced switch) it
// resolves to fsnotify — the pre-#6932 behaviour — so nobody who writes it
// today silently gets a mode that has no probe behind it.
func TestChangeDetection_AutoBehavesAsFsnotifyUntilArmB(t *testing.T) {
	var c GroupConfig
	c.Features.ChangeDetection = ChangeDetectionAuto
	if got := c.ChangeDetectionMode(); got != ChangeDetectionAuto {
		t.Fatalf("auto must round-trip as a valid mode, got %q", got)
	}
	if c.PollingEnabled() {
		t.Fatal("auto must NOT enable polling in arm A")
	}
	c.Features.ChangeDetection = ChangeDetectionPoll
	if !c.PollingEnabled() {
		t.Fatal("poll must enable polling")
	}
	c.Features.ChangeDetection = ChangeDetectionFsnotify
	if c.PollingEnabled() {
		t.Fatal("fsnotify must not enable polling")
	}
}

func TestChangePollInterval(t *testing.T) {
	var c GroupConfig
	if got := c.ChangePollInterval(); got != DefaultChangePollInterval {
		t.Fatalf("unset interval = %v, want %v", got, DefaultChangePollInterval)
	}
	c.Features.ChangePollIntervalSeconds = 5
	if got := c.ChangePollInterval(); got != 5*time.Second {
		t.Fatalf("interval = %v, want 5s", got)
	}
	// A non-positive value is a typo, not a request for a hot loop.
	c.Features.ChangePollIntervalSeconds = -1
	if got := c.ChangePollInterval(); got != DefaultChangePollInterval {
		t.Fatalf("negative interval = %v, want the default %v", got, DefaultChangePollInterval)
	}
	// And it is floored, so a "1ms" typo cannot pin a core per worktree.
	c.Features.ChangePollIntervalSeconds = 0
	if got := c.ChangePollInterval(); got != DefaultChangePollInterval {
		t.Fatalf("zero interval = %v, want the default %v", got, DefaultChangePollInterval)
	}
}
