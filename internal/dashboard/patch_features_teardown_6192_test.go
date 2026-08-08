package dashboard

// patch_features_teardown_6192_test.go — turning watchers off in the Settings
// UI now deregisters the units, instead of writing a flag that the machine goes
// on ignoring.
//
// The general "make features.watchers retroactive" change was rejected: the
// dominant way the flag changes is a fleet.json edit, which runs no grafel code
// at all, so a teardown could only fire at some later unrelated command. This
// handler is the opposite case on every axis — synchronous, user-initiated,
// explicitly intentional, and grafel code by definition — and the teardown
// primitive is already called from this same file by handleV2DeleteGroup.

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// seedPatchFeaturesGroup registers a one-repo group in a sandboxed home and
// returns the test server and the repo path. watchersOn seeds the flag; the
// unit file is written separately so a test can choose the starting state.
func seedPatchFeaturesGroup(t *testing.T, watchersOn bool) (*httptest.Server, string) {
	t.Helper()
	// Every home var, not just GRAFEL_HOME: watchers.UnitPath derives from HOME
	// (darwin/windows) or XDG_CONFIG_HOME (linux), and a miss there would write
	// unit files into the developer's real LaunchAgents directory.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GRAFEL_HOME", filepath.Join(home, ".grafel"))

	repo := filepath.Join(home, "repos", "alpha")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	cfgPath := filepath.Join(home, "grp.fleet.json")
	cfg := &registry.GroupConfig{Name: "grp", Repos: []registry.Repo{{Slug: "alpha", Path: repo}}}
	cfg.Features.Watchers = watchersOn
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}
	if err := registry.AddGroup("grp", cfgPath); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return ts, repo
}

// unitPathFor returns the sandboxed on-disk path of a repo's watcher unit.
func unitPathFor(t *testing.T, repo string) string {
	t.Helper()
	p, err := watchers.UnitPath(watchers.Unit{Group: "grp", Repo: repo})
	if err != nil {
		t.Fatalf("UnitPath: %v", err)
	}
	return p
}

// patchFeatures PATCHes the group's feature toggles and asserts a 200.
func patchFeatures(t *testing.T, ts *httptest.Server, body string) {
	t.Helper()
	req, _ := http.NewRequest("PATCH", ts.URL+"/api/v2/groups/grp/features", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH status %d", resp.StatusCode)
	}
}

// 6192-I: the transition the UI actually performs — watchers on with a unit
// installed, then the user turns the toggle off and saves. The unit must go.
func TestV2PatchFeatures_TurningWatchersOffDeregistersTheUnit(t *testing.T) {
	// Cleanup calls Loader.Unload, a mutating service-manager verb. Stubbing it
	// keeps the test off the developer's real launchd while leaving the file
	// removal — the half this test is about — completely real.
	defer watchers.StubServiceCallsForTest()()

	ts, repo := seedPatchFeaturesGroup(t, true)
	if _, err := watchers.Write(watchers.Unit{Group: "grp", Repo: repo, BinPath: "/usr/local/bin/grafel"}); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	unit := unitPathFor(t, repo)
	if _, err := os.Stat(unit); err != nil {
		t.Fatalf("precondition: unit should exist: %v", err)
	}

	patchFeatures(t, ts, `{"watchers":false,"gitHooks":false}`)

	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatalf("turning watchers off left the unit in place (stat err=%v)", err)
	}
	// …and the flag itself is still persisted. A teardown that ran instead of
	// the save, rather than after it, would satisfy the assertion above and
	// silently lose the setting.
	p, err := groupConfigPath("grp")
	if err != nil {
		t.Fatalf("groupConfigPath: %v", err)
	}
	cfg, err := registry.LoadGroupConfig(p)
	if err != nil {
		t.Fatalf("LoadGroupConfig: %v", err)
	}
	if cfg.Features.Watchers {
		t.Fatal("the teardown ran but the flag was not persisted as false")
	}
}

// 6192-J: turning watchers ON must not remove anything. The teardown keys on
// the resulting state, and a mutant that ran it unconditionally would delete
// the unit the user just asked for.
func TestV2PatchFeatures_TurningWatchersOnKeepsTheUnit(t *testing.T) {
	defer watchers.StubServiceCallsForTest()()

	ts, repo := seedPatchFeaturesGroup(t, false)
	if _, err := watchers.Write(watchers.Unit{Group: "grp", Repo: repo, BinPath: "/usr/local/bin/grafel"}); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	unit := unitPathFor(t, repo)

	patchFeatures(t, ts, `{"watchers":true,"gitHooks":false}`)

	if _, err := os.Stat(unit); err != nil {
		t.Fatalf("turning watchers ON removed the unit: %v", err)
	}
}

// 6192-K: keyed on the resulting STATE, not on a change of value.
//
// A group whose flag was already false — flipped by a fleet.json edit, which
// runs no grafel code — carries exactly the residue this issue is about. Saving
// the settings form with watchers still off is the user's one in-UI way to
// clear it, and a teardown gated on "the value changed" would do nothing at all
// in precisely that case.
func TestV2PatchFeatures_ClearsResidueWhenFlagWasAlreadyOff(t *testing.T) {
	defer watchers.StubServiceCallsForTest()()

	ts, repo := seedPatchFeaturesGroup(t, false /* already off */)
	if _, err := watchers.Write(watchers.Unit{Group: "grp", Repo: repo, BinPath: "/usr/local/bin/grafel"}); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	unit := unitPathFor(t, repo)

	patchFeatures(t, ts, `{"watchers":false,"gitHooks":false}`)

	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatalf("a save with watchers already off did not clear the stale unit (stat err=%v)", err)
	}
}
