package cli

// group_watchers_teardown_6192_test.go — re-registering a group with watchers
// off deregisters the units it used to have.
//
// applyGroupConfig is the shared tail of `grafel group add`, `grafel init` and
// the wizard: it saves the config, registers the group, and runs the install
// transaction. All three are synchronous, user-initiated statements about how
// this group should be set up, so "watchers off" can be honoured there rather
// than merely recorded. `grafel update` is deliberately NOT in that list — it
// calls install.Apply directly (internal/cli/update.go), so a self-update still
// cannot tear a user's watchers down as a side effect.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/install/watchers"
)

// unitPathForRepo returns the sandboxed unit path for a (group, repo) pair.
func unitPathForRepo(t *testing.T, group, repo string) string {
	t.Helper()
	p, err := watchers.UnitPath(watchers.Unit{Group: group, Repo: repo})
	if err != nil {
		t.Fatalf("UnitPath: %v", err)
	}
	return p
}

// addGroupWithWatchers runs `group add` for one repo with the given flag value.
func addGroupWithWatchers(t *testing.T, group, repo string, on bool) {
	t.Helper()
	cmd := &cobra.Command{}
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	err := runGroupAddImpl(cmd, group, groupAddFlags{
		repoArgs: []string{repo},
		watchers: on,
		gitHooks: false,
		rules:    false,
		mcp:      false,
		runInst:  true,
		doIndex:  false,
		jsonOut:  true,
	}, "")
	if err != nil {
		t.Fatalf("group add (watchers=%v): %v\n%s", on, err, out.String())
	}
}

// 6192-L: the TRANSITION through the CLI. A group is registered with watchers
// on — install writes and registers its unit — and is then re-registered with
// watchers off. The unit must not survive that.
//
// The two halves are driven in sequence deliberately: asserting "watchers off
// leaves no unit" on a fresh group would pass trivially, because install never
// wrote one. The defect only exists when a unit is already there.
func TestGroupAdd_WatchersOffTearsDownAnExistingUnit(t *testing.T) {
	// applyGroupConfig's teardown calls Loader.Unload, a mutating
	// service-manager verb. Stubbed so the test cannot reach real launchd; the
	// file removal it is asserting on stays entirely real.
	defer watchers.StubServiceCallsForTest()()

	home := withSandboxHome(t)
	repo := filepath.Join(home, "repos", "alpha")
	makeRepo(t, repo)

	addGroupWithWatchers(t, "demo", repo, true)
	unit := unitPathForRepo(t, "demo", repo)
	if _, err := os.Stat(unit); err != nil {
		t.Fatalf("precondition: install with watchers on should have written a unit: %v", err)
	}

	addGroupWithWatchers(t, "demo", repo, false)

	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatalf("re-registering with watchers off left the unit in place (stat err=%v)", err)
	}
}

// 6192-M: re-registering with watchers ON must keep the unit. A teardown that
// ignored the flag would delete the unit install had just written, on every
// single `group add`.
func TestGroupAdd_WatchersOnKeepsTheUnit(t *testing.T) {
	defer watchers.StubServiceCallsForTest()()

	home := withSandboxHome(t)
	repo := filepath.Join(home, "repos", "alpha")
	makeRepo(t, repo)

	addGroupWithWatchers(t, "demo", repo, true)
	unit := unitPathForRepo(t, "demo", repo)

	addGroupWithWatchers(t, "demo", repo, true)

	if _, err := os.Stat(unit); err != nil {
		t.Fatalf("re-registering with watchers ON removed the unit: %v", err)
	}
}
