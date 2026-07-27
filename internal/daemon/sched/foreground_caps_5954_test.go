package sched

// foreground_caps_5954_test.go — the FOREGROUND/BACKGROUND split for the two
// heavy batch children (group-algo, links).
//
// THE HAZARD THESE TESTS ENCODE. Epic #5954 optimised memory and capped every
// child unconditionally. On a 12-core laptop that means a rebuild the user
// typed and is sitting in front of runs its heaviest stage — the group-algo
// pass, measured at 32–53 minutes in the daemon log — on TWO cores, because the
// cap is resolved inside the spawn helper with no notion of what triggered the
// work. The user's policy was always two-sided: "cap it at 25% of the machine
// … interactive remains uncapped". Only the first half shipped.
//
// So the assertions below are derived from the hazard, not from whatever the
// constants happen to be:
//
//   - a foreground (user-awaited) pass must get NEAR-FULL machine capacity —
//     specifically it must never land on the background cap;
//   - a background pass must stay inside the 25% budget (the standing policy,
//     which this change must not regress);
//   - the background default must be PROPORTIONAL to the host — a hardcoded 2
//     is as wrong on a 64-core server as it is on a 12-core laptop — with a
//     floor so a small machine still gets a usable pass;
//   - an explicit operator override wins in BOTH modes and is never clamped.

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// nearFullCapacity is the floor a user-awaited pass must clear: three quarters
// of the machine. Anything at or below the 25% background budget means the
// human is waiting on a throttled pass, which is the whole regression.
func nearFullCapacity() int {
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	return (n*3 + 3) / 4
}

// resetForegroundForTest clears the package foreground registry between tests.
func resetForegroundForTest(t *testing.T) {
	t.Helper()
	resetForegroundGroups()
	t.Cleanup(resetForegroundGroups)
}

// --- the two heavy children, foreground -------------------------------------

func TestGroupAlgoGOMAXPROCS_ForegroundRunsNearFullCapacity(t *testing.T) {
	t.Setenv("GRAFEL_GROUP_ALGO_CPU", "")
	t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "")

	fg := GroupAlgoGOMAXPROCSFor(true)
	if want := nearFullCapacity(); fg < want {
		t.Fatalf("foreground group-algo cap = %d on a %d-core host, want >= %d — "+
			"a rebuild the user is waiting on must not run at the background budget (#5954 wall-time regression)",
			fg, runtime.NumCPU(), want)
	}
	if bg := GroupAlgoGOMAXPROCSFor(false); runtime.NumCPU() >= 8 && fg <= bg {
		t.Fatalf("foreground group-algo cap (%d) <= background cap (%d): the split is not wired", fg, bg)
	}
}

func TestLinksGOMAXPROCS_ForegroundRunsNearFullCapacity(t *testing.T) {
	t.Setenv("GRAFEL_LINKS_CPU", "")
	t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "")

	fg := LinksGOMAXPROCSFor(true)
	if want := nearFullCapacity(); fg < want {
		t.Fatalf("foreground links cap = %d on a %d-core host, want >= %d", fg, runtime.NumCPU(), want)
	}
	if bg := LinksGOMAXPROCSFor(false); runtime.NumCPU() >= 8 && fg <= bg {
		t.Fatalf("foreground links cap (%d) <= background cap (%d): the split is not wired", fg, bg)
	}
}

// GRAFEL_REBUILD_GOMAXPROCS is the EXISTING foreground knob (#5135, already
// honoured by the extract coordinator and the index child). The two batch
// children must join it rather than invent a third mechanism.
func TestForegroundCapsHonourRebuildGOMAXPROCS(t *testing.T) {
	t.Setenv("GRAFEL_GROUP_ALGO_CPU", "")
	t.Setenv("GRAFEL_LINKS_CPU", "")
	t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "9")

	if got := GroupAlgoGOMAXPROCSFor(true); got != 9 {
		t.Errorf("foreground group-algo cap = %d, want 9 (GRAFEL_REBUILD_GOMAXPROCS)", got)
	}
	if got := LinksGOMAXPROCSFor(true); got != 9 {
		t.Errorf("foreground links cap = %d, want 9 (GRAFEL_REBUILD_GOMAXPROCS)", got)
	}
	// It must NOT leak into background work — that is the policy this change
	// must not regress.
	if got := GroupAlgoGOMAXPROCSFor(false); got == 9 {
		t.Errorf("background group-algo cap picked up GRAFEL_REBUILD_GOMAXPROCS (= %d); "+
			"background stays on the 25%%-family budget", got)
	}
}

// --- proportional background defaults ---------------------------------------

// The background default must scale with the host and never sink below a usable
// floor. Asserted as policy invariants across 1/2/4/12/64 cores rather than as
// a table of magic numbers.
func TestBackgroundBatchGOMAXPROCS_ProportionalWithFloor(t *testing.T) {
	t.Setenv("GRAFEL_GROUP_ALGO_CPU", "")
	t.Setenv("GRAFEL_LINKS_CPU", "")

	cases := []int{1, 2, 4, 12, 64}
	prev := 0
	for _, numCPU := range cases {
		got := backgroundBatchGOMAXPROCSFor(numCPU)

		floor := 2
		if numCPU < floor {
			floor = numCPU
		}
		if floor < 1 {
			floor = 1
		}
		if got < floor {
			t.Errorf("background batch cap on %d cores = %d, below the floor %d — "+
				"a background pass still has to finish", numCPU, got, floor)
		}
		// 25% is the standing background policy (process.IndexCoreBudget). The
		// floor may exceed it on a tiny box; nothing else may.
		ceiling := numCPU / 4
		if ceiling < floor {
			ceiling = floor
		}
		if got > ceiling {
			t.Errorf("background batch cap on %d cores = %d, above the 25%% budget %d — "+
				"background work must not take the machine away from the human", numCPU, got, ceiling)
		}
		if got > numCPU {
			t.Errorf("background batch cap on %d cores = %d, more cores than the machine has", numCPU, got)
		}
		if got < prev {
			t.Errorf("background batch cap is not monotonic in host size: %d cores -> %d, after a smaller host -> %d",
				numCPU, got, prev)
		}
		prev = got
	}

	// The proportionality has to BITE, not just be monotonic: a 64-core server
	// must get materially more than a 12-core laptop.
	if big, small := backgroundBatchGOMAXPROCSFor(64), backgroundBatchGOMAXPROCSFor(12); big <= small {
		t.Errorf("64-core background cap (%d) is not better than the 12-core one (%d) — "+
			"the default is still effectively static", big, small)
	}
	// And the 12-core case must not get WORSE than the shipped behaviour (2).
	if got := backgroundBatchGOMAXPROCSFor(12); got < 2 {
		t.Errorf("12-core background cap = %d, want >= 2 (no background regression)", got)
	}
}

// --- operator overrides win, unclamped, in both modes ------------------------

func TestExplicitOverridesWinInBothModes(t *testing.T) {
	t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "")
	t.Setenv("GRAFEL_GROUP_ALGO_CPU", "7")
	t.Setenv("GRAFEL_LINKS_CPU", "5")

	for _, fg := range []bool{false, true} {
		if got := GroupAlgoGOMAXPROCSFor(fg); got != 7 {
			t.Errorf("GroupAlgoGOMAXPROCSFor(foreground=%v) = %d, want 7 — "+
				"an explicit operator override is an escape hatch and is never clamped", fg, got)
		}
		if got := LinksGOMAXPROCSFor(fg); got != 5 {
			t.Errorf("LinksGOMAXPROCSFor(foreground=%v) = %d, want 5", fg, got)
		}
	}
}

func TestExplicitOverrideOfOneCoreStillHonoured(t *testing.T) {
	t.Setenv("GRAFEL_GROUP_ALGO_CPU", "1")
	t.Setenv("GRAFEL_LINKS_CPU", "1")
	if got := GroupAlgoGOMAXPROCSFor(true); got != 1 {
		t.Errorf("GRAFEL_GROUP_ALGO_CPU=1 in foreground = %d, want 1", got)
	}
	if got := LinksGOMAXPROCSFor(true); got != 1 {
		t.Errorf("GRAFEL_LINKS_CPU=1 in foreground = %d, want 1", got)
	}
}

// --- the foreground-origin registry ------------------------------------------

// A rebuild's follow-on stages (the scheduler's link pass, then the debounced
// group-algo pass) fire AFTER daemonRebuildFuncCore has returned — that is
// precisely why the barge alone is not a sufficient signal. The registry must
// therefore keep the group hot past the hold's release.
func TestForegroundGroupSurvivesTheHoldRelease(t *testing.T) {
	resetForegroundForTest(t)

	if GroupIsForeground("acme") {
		t.Fatal("group is foreground before anything marked it")
	}
	done := MarkGroupForeground("acme")
	if !GroupIsForeground("acme") {
		t.Fatal("group is not foreground while a foreground rebuild holds it")
	}
	if GroupIsForeground("other") {
		t.Fatal("marking one group made an unrelated group foreground")
	}
	done()
	if !GroupIsForeground("acme") {
		t.Fatal("group stopped being foreground the instant the rebuild returned — " +
			"the debounced group-algo pass the user is still waiting for would get background caps (#5954)")
	}
	ClearGroupForeground("acme")
	if GroupIsForeground("acme") {
		t.Fatal("ClearGroupForeground did not clear the linger")
	}
}

func TestForegroundGroupLingerExpires(t *testing.T) {
	resetForegroundForTest(t)

	base := time.Now()
	now := base
	restore := setForegroundClockForTest(func() time.Time { return now })
	defer restore()

	MarkGroupForeground("acme")()
	now = base.Add(foregroundLingerWindow() - time.Second)
	if !GroupIsForeground("acme") {
		t.Fatal("linger expired early")
	}
	now = base.Add(foregroundLingerWindow() + time.Second)
	if GroupIsForeground("acme") {
		t.Fatal("linger never expires — a group that never runs group-algo would stay uncapped forever")
	}
}

func TestForegroundLingerWindowIsEnvOverridable(t *testing.T) {
	t.Setenv("GRAFEL_FOREGROUND_LINGER", "90s")
	if got := foregroundLingerWindow(); got != 90*time.Second {
		t.Fatalf("foregroundLingerWindow() = %s, want 90s", got)
	}
	t.Setenv("GRAFEL_FOREGROUND_LINGER", "nonsense")
	if got := foregroundLingerWindow(); got <= 0 {
		t.Fatalf("a malformed GRAFEL_FOREGROUND_LINGER disabled the linger (%s); want the policy default", got)
	}
}

// A concurrent background rebuild of a DIFFERENT group must not be dragged into
// foreground caps, and nested holds must not release early.
func TestForegroundGroupRefcounted(t *testing.T) {
	resetForegroundForTest(t)

	d1 := MarkGroupForeground("acme")
	d2 := MarkGroupForeground("acme")
	d1()
	if !GroupIsForeground("acme") {
		t.Fatal("the first release dropped a still-held group")
	}
	d2()
	ClearGroupForeground("acme")
	if GroupIsForeground("acme") {
		t.Fatal("group still foreground after all holds released and cleared")
	}
}

// --- the spawn points actually consult the registry --------------------------

// This is the wiring assertion: the value the group-algo / links children are
// spawned with must come from the registry, not from a naked call to the
// background resolver.
func TestSpawnPointsResolveFromTheForegroundRegistry(t *testing.T) {
	resetForegroundForTest(t)
	t.Setenv("GRAFEL_GROUP_ALGO_CPU", "")
	t.Setenv("GRAFEL_LINKS_CPU", "")
	t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "")

	if got, want := groupAlgoChildGOMAXPROCS("acme"), GroupAlgoGOMAXPROCSFor(false); got != want {
		t.Errorf("group-algo spawn cap for a background group = %d, want the background cap %d", got, want)
	}
	if got, want := linksChildGOMAXPROCS("acme"), LinksGOMAXPROCSFor(false); got != want {
		t.Errorf("links spawn cap for a background group = %d, want the background cap %d", got, want)
	}

	defer MarkGroupForeground("acme")()
	if got, want := groupAlgoChildGOMAXPROCS("acme"), GroupAlgoGOMAXPROCSFor(true); got != want {
		t.Errorf("group-algo spawn cap during a foreground rebuild = %d, want the foreground cap %d — "+
			"this is the 32–53 minute pass the user is waiting on", got, want)
	}
	if got, want := linksChildGOMAXPROCS("acme"), LinksGOMAXPROCSFor(true); got != want {
		t.Errorf("links spawn cap during a foreground rebuild = %d, want the foreground cap %d", got, want)
	}
	// A DIFFERENT group's background pass must stay capped.
	if got, want := groupAlgoChildGOMAXPROCS("unrelated"), GroupAlgoGOMAXPROCSFor(false); got != want {
		t.Errorf("an unrelated group's background group-algo cap = %d, want %d", got, want)
	}
}

// End-to-end at the fork: the GOMAXPROCS the links child is ACTUALLY launched
// with must follow the registry. Asserted through a real fork of a stand-in
// child (the fakeChildScript harness) so a regression that computes the right
// number and then forgets to put it in the env still fails.
func TestRunSubprocessLinks_ForegroundGroupGetsForegroundGOMAXPROCS(t *testing.T) {
	resetForegroundForTest(t)
	t.Setenv("GRAFEL_LINKS_CPU", "")
	t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "11")

	out := filepath.Join(t.TempDir(), "gmp.txt")
	fakeChildScript(t, `printf 'gomaxprocs=%s\n' "$GOMAXPROCS" > `+out)

	defer MarkGroupForeground("hot")()
	if err := RunSubprocessLinks(context.Background(), "hot", nil); err != nil {
		t.Fatalf("RunSubprocessLinks: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read env capture: %v", err)
	}
	if got := strings.TrimSpace(string(b)); got != "gomaxprocs=11" {
		t.Fatalf("links child env %q, want gomaxprocs=11 — the user's rebuild is waiting on this pass", got)
	}

	// And an unrelated group still forks at the background cap.
	if err := RunSubprocessLinks(context.Background(), "cold", nil); err != nil {
		t.Fatalf("RunSubprocessLinks: %v", err)
	}
	b, err = os.ReadFile(out)
	if err != nil {
		t.Fatalf("read env capture: %v", err)
	}
	want := "gomaxprocs=" + strconv.Itoa(BackgroundBatchGOMAXPROCS())
	if got := strings.TrimSpace(string(b)); got != want {
		t.Fatalf("background links child env %q, want %q", got, want)
	}
}
