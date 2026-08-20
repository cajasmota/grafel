package install

// doctor_skew_6339_test.go — #6339. `grafel status` needs to surface
// serve/engine version skew, and the explicit requirement is that it reuse
// doctor's ONE detector rather than run a second, independently-driftable
// check. That means checkEngineLiveness must expose the skew in a STRUCTURED
// form (CheckResult.Skew), set in the same branch that writes the human Drift
// string, plus an exported EngineVersionSkew() entry point for callers outside
// this package.
//
// Both directions are asserted: skew present → Skew non-nil with both
// versions; no skew (and every other engine state) → Skew nil. Without the
// second direction a caller that printed the line unconditionally would pass.

import (
	"os"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/statusfile"
	"github.com/cajasmota/grafel/internal/testsupport"
)

func TestCheckEngineLiveness_VersionSkew_ExposesStructuredSkew(t *testing.T) {
	f := &statusfile.File{
		EnginePID:   4242,
		HeartbeatAt: time.Now(),
		Version:     "v0.1.9",
	}
	deps := fakeEngineLivenessDeps(t, 4242, nil, f, nil, 15*time.Second)
	cr := checkEngineLiveness(&State{DaemonVersion: "v0.2.2"}, deps)

	if cr.Skew == nil {
		t.Fatalf("version skew must expose a structured Skew, got nil (drift=%v)", cr.Drift)
	}
	if cr.Skew.Serve != "v0.2.2" || cr.Skew.Engine != "v0.1.9" {
		t.Errorf("Skew = %+v, want Serve=v0.2.2 Engine=v0.1.9", *cr.Skew)
	}
}

// The other direction: every non-skew engine state must leave Skew nil, so a
// status caller that gates on it prints nothing.
func TestCheckEngineLiveness_NoVersionSkew_LeavesSkewNil(t *testing.T) {
	fresh := func(v string) *statusfile.File {
		return &statusfile.File{EnginePID: 4242, HeartbeatAt: time.Now(), Version: v}
	}
	cases := []struct {
		name  string
		deps  engineLivenessDeps
		state *State
	}{
		{
			name:  "matching versions",
			deps:  fakeEngineLivenessDeps(t, 4242, nil, fresh("v0.2.2"), nil, 15*time.Second),
			state: &State{DaemonVersion: "v0.2.2"},
		},
		{
			name:  "monolith mode",
			deps:  fakeEngineLivenessDeps(t, 0, os.ErrNotExist, nil, nil, 15*time.Second),
			state: &State{DaemonVersion: "v0.2.2"},
		},
		{
			name: "stale heartbeat (degraded, but not skew)",
			deps: fakeEngineLivenessDeps(t, 4242, nil,
				&statusfile.File{EnginePID: 4242, HeartbeatAt: time.Now().Add(-time.Hour), Version: "v0.1.9"},
				nil, 15*time.Second),
			state: &State{DaemonVersion: "v0.2.2"},
		},
		{
			name:  "engine version unknown",
			deps:  fakeEngineLivenessDeps(t, 4242, nil, fresh(""), nil, 15*time.Second),
			state: &State{DaemonVersion: "v0.2.2"},
		},
		{
			name:  "serve version unknown",
			deps:  fakeEngineLivenessDeps(t, 4242, nil, fresh("v0.1.9"), nil, 15*time.Second),
			state: &State{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if cr := checkEngineLiveness(tc.state, tc.deps); cr.Skew != nil {
				t.Fatalf("%s must leave Skew nil, got %+v", tc.name, *cr.Skew)
			}
		})
	}
}

// EngineVersionSkew must not blow up or invent a skew when there is no
// install.json / no daemon at all — status calls it on every run.
func TestEngineVersionSkew_NoInstall_NoSkew(t *testing.T) {
	testsupport.IsolateHome(t)
	if got := EngineVersionSkew(); got != nil {
		t.Fatalf("EngineVersionSkew() with no install = %+v, want nil", *got)
	}
}
