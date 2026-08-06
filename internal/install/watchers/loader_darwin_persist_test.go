//go:build darwin

package watchers

// loader_darwin_persist_test.go covers the persistent-stop pairing
// (bootout + `launchctl disable`, and the matching `enable` on Load) and the
// two hazards an adversarial review found in the first cut:
//
//   - `disable` ran BEFORE the not-loaded early return, so Unload was no
//     longer a no-op for a label that was never loaded. Every test calling
//     watchers.Cleanup without a launchctl seam therefore wrote real entries
//     into the developer's launchd disabled database (gui/<uid> is a system
//     database; redirecting $HOME does not sandbox it).
//   - the assertions matched substrings over a joined blob. "enable" is a
//     SUBSTRING of "disable", so an ordering assertion built on strings.Index
//     silently inverts the moment Load also issues a disable, and a call with
//     the wrong uid or a malformed target would pass. Every assertion below is
//     on the exact argv slice.

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

// recordLaunchctl installs a fake launchctl that records exact argv slices.
func recordLaunchctl(t *testing.T) *[][]string {
	t.Helper()
	var calls [][]string
	orig := launchctlRunner
	launchctlRunner = func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return nil, nil
	}
	t.Cleanup(func() { launchctlRunner = orig })
	return &calls
}

func hasArgv(calls [][]string, want []string) bool {
	for _, c := range calls {
		if reflect.DeepEqual(c, want) {
			return true
		}
	}
	return false
}

func argvIndex(calls [][]string, verb string) int {
	for i, c := range calls {
		if len(c) > 0 && c[0] == verb {
			return i
		}
	}
	return -1
}

func testUnit(t *testing.T) Unit {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return Unit{Group: "demo", Repo: filepath.Join(home, "core"), BinPath: "/bin/grafel"}
}

// TestUnload_DisablesWithExactTarget: the disable must name gui/<uid>/<label>
// exactly. A wrong uid or a malformed target silently does nothing.
func TestUnload_DisablesWithExactTarget(t *testing.T) {
	u := testUnit(t)
	if _, err := Write(u); err != nil {
		t.Fatalf("Write: %v", err)
	}
	calls := recordLaunchctl(t)
	// A loaded job: `launchctl list <label>` succeeds (our fake returns nil).
	if err := (darwinLoader{}).Unload(u); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	uid := strconv.Itoa(os.Getuid())
	target := "gui/" + uid + "/" + u.Label()
	if !hasArgv(*calls, []string{"disable", target}) {
		t.Fatalf("no exact `launchctl disable %s`; calls=%v", target, *calls)
	}
	if !hasArgv(*calls, []string{"bootout", target}) {
		t.Fatalf("no exact `launchctl bootout %s`; calls=%v", target, *calls)
	}
}

// TestUnload_IsNoOpWhenNotLoaded is the merge blocker: a label that is not
// loaded must produce NO mutating launchctl call at all. Otherwise every test
// in the tree that calls Cleanup writes into the real launchd database.
func TestUnload_IsNoOpWhenNotLoaded(t *testing.T) {
	u := testUnit(t)
	var calls [][]string
	orig := launchctlRunner
	launchctlRunner = func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) > 0 && args[0] == "list" {
			return nil, os.ErrNotExist // not loaded
		}
		return nil, nil
	}
	t.Cleanup(func() { launchctlRunner = orig })

	if err := (darwinLoader{}).Unload(u); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	for _, c := range calls {
		if len(c) > 0 && c[0] != "list" {
			t.Fatalf("Unload of a not-loaded label must issue no mutating launchctl call, got %v (all: %v)", c, calls)
		}
	}
}

// TestLoad_EnablesBeforeBootstrap: without the enable, RunAtLoad silently does
// nothing on a job a previous `grafel stop` disabled. Ordering is asserted on
// argv positions, not on substring offsets.
func TestLoad_EnablesBeforeBootstrap(t *testing.T) {
	u := testUnit(t)
	if _, err := Write(u); err != nil {
		t.Fatalf("Write: %v", err)
	}
	calls := recordLaunchctl(t)
	if err := (darwinLoader{}).Load(u); err != nil {
		t.Fatalf("Load: %v", err)
	}
	uid := strconv.Itoa(os.Getuid())
	if !hasArgv(*calls, []string{"enable", "gui/" + uid + "/" + u.Label()}) {
		t.Fatalf("Load issued no exact `launchctl enable`; calls=%v", *calls)
	}
	ei := argvIndex(*calls, "enable")
	bi := argvIndex(*calls, "bootstrap")
	if bi < 0 || ei > bi {
		t.Fatalf("enable must precede bootstrap; calls=%v", *calls)
	}
}

// TestStatus_GoesThroughTheSeam: the review found `launchctl list` in Status
// was still a bare exec.Command, so the "every launchctl invocation goes
// through launchctlRunner" claim was false and the status path was untestable.
func TestStatus_GoesThroughTheSeam(t *testing.T) {
	u := testUnit(t)
	if _, err := Write(u); err != nil {
		t.Fatalf("Write: %v", err)
	}
	var calls [][]string
	orig := launchctlRunner
	launchctlRunner = func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return []byte("4242\t0\t" + u.Label()), nil
	}
	t.Cleanup(func() { launchctlRunner = orig })

	st, err := (darwinLoader{}).Status(u)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !hasArgv(calls, []string{"list", u.Label()}) {
		t.Fatalf("Status did not go through launchctlRunner; calls=%v", calls)
	}
	if !st.Running || st.PID != 4242 {
		t.Fatalf("Status = %+v, want Running with pid 4242", st)
	}
}

// TestInstalledUnits_ReadsFromDisk: the fleet sweep must be able to see units
// the registry cannot — orphans left by a partially-failed Cleanup, a dropped
// group, or a slug-derivation change.
func TestInstalledUnits_ReadsFromDisk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := UnitDir()
	if err != nil {
		t.Fatalf("UnitDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{
		"com.grafel.watcher.demo.core.plist",
		"com.grafel.watcher.gone.orphan.plist",
		"com.example.other.plist", // must be ignored
		"notaplist.txt",           // must be ignored
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	units, err := InstalledUnits()
	if err != nil {
		t.Fatalf("InstalledUnits: %v", err)
	}
	got := map[string]bool{}
	for _, u := range units {
		got[u.Label()] = true
	}
	if len(got) != 2 || !got["com.grafel.watcher.demo.core"] || !got["com.grafel.watcher.gone.orphan"] {
		t.Fatalf("InstalledUnits = %v, want exactly the two com.grafel.watcher.* labels", got)
	}
}

// TestInstalledUnits_MissingDirIsEmpty: no units installed is not an error.
func TestInstalledUnits_MissingDirIsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	units, err := InstalledUnits()
	if err != nil {
		t.Fatalf("InstalledUnits: %v", err)
	}
	if len(units) != 0 {
		t.Fatalf("want no units, got %v", units)
	}
}

// TestRawLabelRoundTrips: a unit discovered by label alone must address the
// same launchd job and the same file as the derived one, or the disk-glob
// sweep would boot out the wrong thing.
func TestRawLabelRoundTrips(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	derived := Unit{Group: "demo", Repo: "/tmp/core", BinPath: "/bin/grafel"}
	raw := Unit{RawLabel: derived.Label()}
	if raw.Label() != derived.Label() {
		t.Fatalf("label mismatch: %q vs %q", raw.Label(), derived.Label())
	}
	dp, err := UnitPath(derived)
	if err != nil {
		t.Fatalf("UnitPath derived: %v", err)
	}
	rp, err := UnitPath(raw)
	if err != nil {
		t.Fatalf("UnitPath raw: %v", err)
	}
	if dp != rp {
		t.Fatalf("path mismatch: %q vs %q", dp, rp)
	}
}
