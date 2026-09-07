package watch

// inotifybudget_6932_test.go — #6932 arm B part 1, the budget probe.
//
// The headline property is not arithmetic, it is AGREEMENT: what the probe
// projects must equal what a real subscription actually takes. A projection
// nobody compared to a measurement is arithmetic, so the comparison here is
// against fsnotify's own WatchList — the library's record of what it handed the
// kernel — and not against a counter grafel keeps about itself.
//
// The second property is honesty. The inotify pool is per-UID and host-level,
// so the probe can see its own demand and NOT anyone else's, and off Linux the
// pool does not exist at all. Both facts have to reach the reader, so both are
// asserted on the report's own text.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// budgetRepo builds a tree whose directories are NOT all watchable: one
// hard-coded skip (node_modules, with a child, so a whole subtree is pruned)
// and one .gitignore'd directory. If the probe simply counted directories on
// disk it would over-count here, which is what makes the agreement assertion
// below non-vacuous.
func budgetRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{
		"src", "src/inner", "pkg",
		"node_modules", "node_modules/deep",
		"generated", "generated/nested",
	} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("generated/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "x.go"), []byte("package src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// dirsOnDisk counts every directory in the tree, watchable or not — the number
// the probe must NOT report.
func dirsOnDisk(t *testing.T, root string) int {
	t.Helper()
	n := 0
	if err := filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			n++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// The whole point of the probe: its projection is what a subscription really
// costs. Measured at the backend, in fsnotify's own WatchList.
func TestProjectInotifyBudget_MatchesWhatSubscriptionActuallyTakes(t *testing.T) {
	repo := budgetRepo(t)

	// Projected BEFORE anything subscribes — that is the probe's whole claim.
	proj := ProjectInotifyBudget([]string{repo}, nil)

	w, err := NewWatcherConfig(Config{Debounce: time.Hour}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	defer w.Stop()
	added, err := w.AddRepo(repo)
	if err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	actual := backendWatchList(w)

	// Premises, so a pass cannot come from everything being zero or from a
	// fixture with nothing to skip.
	if actual == 0 {
		t.Fatal("the subscription took 0 paths — the comparison would be vacuous")
	}
	if onDisk := dirsOnDisk(t, repo); onDisk <= actual {
		t.Fatalf("fixture premise broken: %d dirs on disk but %d subscribed — nothing was skipped, "+
			"so a probe that ignored every skip layer would still pass", onDisk, actual)
	}
	// Named, so a fixture that quietly stops exercising one of the two skip
	// layers is a failure rather than a weaker test: the watched set is the
	// root, src, src/inner and pkg — node_modules/** is a hard-coded skip and
	// generated/** is refused by the .gitignore layer.
	if onDisk := dirsOnDisk(t, repo); onDisk != 8 || actual != 4 {
		t.Fatalf("fixture no longer exercises both skip layers: %d dirs on disk, %d subscribed, "+
			"want 8 and 4 (node_modules/** hard-coded, generated/** gitignored)", onDisk, actual)
	}

	if proj.Dirs != actual {
		t.Fatalf("probe projected %d watched dirs, the fsnotify backend actually took %d paths — "+
			"a projection that disagrees with the subscription is not a probe", proj.Dirs, actual)
	}
	if proj.Dirs != added {
		t.Fatalf("probe projected %d dirs, AddRepo reported %d", proj.Dirs, added)
	}
	if proj.Projected != proj.Dirs {
		t.Fatalf("inotify spends one watch descriptor per directory: Projected=%d, Dirs=%d",
			proj.Projected, proj.Dirs)
	}
	if proj.Measured {
		t.Error("a projection walk reported itself as a measurement")
	}
}

// Two repos: the demand is the sum, because each tracked worktree registers as
// its own repo and subscribes its own tree. That multiplier is the reason one
// lane can reach ~10,700 descriptors.
func TestProjectInotifyBudget_SumsAcrossRepos(t *testing.T) {
	a, b := budgetRepo(t), budgetRepo(t)
	one := ProjectInotifyBudget([]string{a}, nil)
	two := ProjectInotifyBudget([]string{a, b}, nil)
	if one.Dirs == 0 {
		t.Fatal("single-repo projection is 0 — vacuous")
	}
	if two.Dirs != 2*one.Dirs {
		t.Fatalf("two identical repos projected %d dirs, want %d", two.Dirs, 2*one.Dirs)
	}
	if two.Repos != 2 {
		t.Fatalf("Repos = %d, want 2", two.Repos)
	}
}

// The per-instance exclude set must reach the projection: a Watcher configured
// to skip a directory does not subscribe it, and the probe must not charge for
// it either.
func TestProjectInotifyBudget_HonoursInstanceExcludes(t *testing.T) {
	repo := budgetRepo(t)
	base := ProjectInotifyBudget([]string{repo}, nil)
	excluded := ProjectInotifyBudget([]string{repo}, map[string]struct{}{"src": {}})
	// src plus src/inner.
	if excluded.Dirs != base.Dirs-2 {
		t.Fatalf("excluding src projected %d dirs, want %d (base %d minus src and src/inner)",
			excluded.Dirs, base.Dirs-2, base.Dirs)
	}
}

// In fsnotify mode the demand is not projected at all — the subscribed set IS
// the demand — and the report says which of the two it is looking at.
func TestWatcherInotifyBudget_FsnotifyModeReportsTheLiveSubscription(t *testing.T) {
	repo := budgetRepo(t)
	w, err := NewWatcherConfig(Config{Debounce: time.Hour}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	defer w.Stop()
	if _, err := w.AddRepo(repo); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	b := w.InotifyBudget()
	if !b.Measured {
		t.Error("fsnotify mode reported a projection where it holds a live subscription")
	}
	if got, want := b.Dirs, backendWatchList(w); got != want {
		t.Fatalf("report says %d dirs, the backend holds %d paths", got, want)
	}
	if b.Repos != 1 {
		t.Fatalf("Repos = %d, want 1", b.Repos)
	}
}

// Poll mode is where the probe earns its keep: nothing is subscribed, so the
// number worth printing is what fsnotify mode WOULD have asked this host for.
// Graded against a real fsnotify subscription of the same tree.
func TestWatcherInotifyBudget_PollModeProjectsWhatFsnotifyWouldCost(t *testing.T) {
	repo := budgetRepo(t)

	polling, err := NewWatcherConfig(Config{Debounce: time.Hour, Delegate: &recordingDelegate{}}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	defer polling.Stop()
	if _, err := polling.AddRepo(repo); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if n := backendWatchList(polling); n != 0 {
		t.Fatalf("poll mode holds %d fsnotify paths; premise of this test is that it holds 0", n)
	}
	b := polling.InotifyBudget()

	subscribing, err := NewWatcherConfig(Config{Debounce: time.Hour}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	defer subscribing.Stop()
	if _, err := subscribing.AddRepo(repo); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	actual := backendWatchList(subscribing)

	if actual == 0 {
		t.Fatal("the fsnotify control subscribed nothing — vacuous")
	}
	if b.Dirs != actual {
		t.Fatalf("poll mode projects %d watch descriptors; fsnotify mode on the same tree takes %d",
			b.Dirs, actual)
	}
	if b.Measured {
		t.Error("poll mode reported its projection as a measurement of a subscription it does not hold")
	}
	if !notesMention(b.Notes, "ZERO inotify watch descriptors") {
		t.Errorf("poll mode's report never says it holds no descriptors right now: %v", b.Notes)
	}
}

func notesMention(notes []string, want string) bool {
	for _, n := range notes {
		if strings.Contains(n, want) {
			return true
		}
	}
	return false
}

// "WOULD EXCEED" is a claim. It must be made when, and only when, grafel's own
// demand is larger than the ceiling — including at the boundary, where equal
// FITS.
func TestInotifyBudget_WouldExceedOnlyWhenItWould(t *testing.T) {
	for _, tc := range []struct {
		limit  string
		exceed bool
	}{
		{"101", false},
		{"100", false}, // exactly the ceiling still fits
		{"99", true},
		{"1", true},
	} {
		t.Setenv(inotifyLimitEnv, tc.limit)
		b := MeasuredInotifyBudget(1, 100)
		if b.Projected != 100 {
			t.Fatalf("limit=%s: Projected = %d, want 100", tc.limit, b.Projected)
		}
		if got := b.WouldExceed(); got != tc.exceed {
			t.Errorf("limit=%s: WouldExceed() = %v, want %v (demand 100)", tc.limit, got, tc.exceed)
		}
		if got := strings.Contains(b.Summary(), "WOULD EXCEED"); got != tc.exceed {
			t.Errorf("limit=%s: summary %q says WOULD EXCEED=%v, want %v", tc.limit, b.Summary(), got, tc.exceed)
		}
	}
}

// An unknown ceiling is not evidence of exceeding one. Whatever the demand, a
// report with no limit must not accuse the host of anything.
func TestInotifyBudget_UnknownLimitNeverClaimsExcess(t *testing.T) {
	b := InotifyBudget{Platform: runtime.GOOS, PoolApplies: true, Dirs: 1 << 20, Projected: 1 << 20}
	if b.LimitKnown() {
		t.Fatal("a zero Limit was treated as a known ceiling")
	}
	if b.WouldExceed() {
		t.Error("WouldExceed() is true with no limit read — not knowing the ceiling is not evidence of crossing it")
	}
	if _, ok := b.Headroom(); ok {
		t.Error("Headroom() claims to be meaningful with no limit")
	}
	if strings.Contains(b.Summary(), "WOULD EXCEED") {
		t.Errorf("summary accuses the host with no ceiling read: %q", b.Summary())
	}
	if !strings.Contains(b.Summary(), "limit unknown") {
		t.Errorf("summary hides that the limit is unknown: %q", b.Summary())
	}
}

// Off Linux there is no per-UID inotify pool at all, and the report must SAY
// that rather than reporting a limit of 0 — which would read as "no headroom".
// Both branches are asserted here rather than skipping one, so the Linux and
// macOS/Windows legs each grade their own half.
func TestInotifyBudget_PlatformAnswerIsHonest(t *testing.T) {
	os.Unsetenv(inotifyLimitEnv)
	b := MeasuredInotifyBudget(1, 50)

	if b.Platform != runtime.GOOS {
		t.Errorf("Platform = %q, want %q", b.Platform, runtime.GOOS)
	}
	if len(b.Notes) == 0 {
		t.Fatal("a report with no caveats at all")
	}

	if inotifyPoolApplies {
		if !b.PoolApplies {
			t.Fatal("Linux reported that the inotify pool does not apply to it")
		}
		if b.LimitSource != inotifyMaxUserWatchesPath {
			t.Errorf("LimitSource = %q, want the sysctl path %q", b.LimitSource, inotifyMaxUserWatchesPath)
		}
		if b.Limit == 0 && b.LimitErr == "" {
			t.Error("no limit and no reason given — the report is silent about why it has no ceiling")
		}
		if b.LimitKnown() && !notesMention(b.Notes, "per-UID") {
			t.Errorf("a report comparing demand against a shared pool omits the shared-pool caveat: %v", b.Notes)
		}
		return
	}

	if b.PoolApplies {
		t.Fatalf("%s claims a per-UID inotify watch pool", runtime.GOOS)
	}
	if b.Limit != 0 || b.LimitKnown() {
		t.Errorf("a platform with no pool reported a usable limit %d", b.Limit)
	}
	if b.WouldExceed() {
		t.Error("a platform with no inotify pool claims to exceed it")
	}
	want := "not applicable on " + runtime.GOOS
	if !strings.Contains(b.Summary(), want) {
		t.Errorf("summary %q does not say %q — silently reporting 0 is the dishonest answer", b.Summary(), want)
	}
	if strings.Contains(b.Summary(), " of 0 ") {
		t.Errorf("summary compares the demand against a fabricated ceiling of 0: %q", b.Summary())
	}
	if !notesMention(b.Notes, "Linux") {
		t.Errorf("notes never explain that inotify is a Linux facility: %v", b.Notes)
	}
	// The directory count is still real, and still reported.
	if b.Dirs != 50 {
		t.Errorf("Dirs = %d, want 50 — the demand is a property of the tree, not of the platform", b.Dirs)
	}
}

// The shared-pool caveat is the one sentence that keeps the number honest: the
// pool is per-UID and host-level, and this probe cannot see the other
// consumers. It must be present on every report that shows a comparison.
func TestInotifyBudget_ComparisonAlwaysCarriesTheSharedPoolCaveat(t *testing.T) {
	t.Setenv(inotifyLimitEnv, "8192")
	repo := budgetRepo(t)
	for name, b := range map[string]InotifyBudget{
		"measured":  MeasuredInotifyBudget(1, 10),
		"projected": ProjectInotifyBudget([]string{repo}, nil),
	} {
		if !b.LimitKnown() {
			t.Fatalf("%s: premise broken, no limit to compare against", name)
		}
		for _, want := range []string{"per-UID", "host-level", "cannot see", "upper bound"} {
			if !notesMention(b.Notes, want) {
				t.Errorf("%s: notes omit %q — the reader would take the headroom as theirs: %v", name, want, b.Notes)
			}
		}
	}
}

// A ceiling supplied by an operator is a what-if, not a reading of this host,
// and the report must not let the two be confused.
func TestInotifyBudget_EnvSuppliedLimitIsLabelledAsSuch(t *testing.T) {
	t.Setenv(inotifyLimitEnv, "128")
	b := MeasuredInotifyBudget(1, 200)
	if b.Limit != 128 {
		t.Fatalf("Limit = %d, want 128 from %s", b.Limit, inotifyLimitEnv)
	}
	if b.LimitSource != inotifyLimitEnv {
		t.Errorf("LimitSource = %q, want %q", b.LimitSource, inotifyLimitEnv)
	}
	if !notesMention(b.Notes, inotifyLimitEnv) {
		t.Errorf("an operator-supplied ceiling is not labelled as one: %v", b.Notes)
	}
	if !b.WouldExceed() {
		t.Error("200 descriptors against a 128 ceiling does not report as exceeding")
	}
}

// A malformed override is ignored rather than parsed into a fabricated ceiling.
func TestInotifyBudget_MalformedEnvLimitIsIgnored(t *testing.T) {
	for _, v := range []string{"", "  ", "abc", "-1", "12.5"} {
		t.Setenv(inotifyLimitEnv, v)
		if n, ok := envInotifyLimit(); ok {
			t.Errorf("%s=%q parsed as %d", inotifyLimitEnv, v, n)
		}
	}
}

// The report is cached because the projection walk is not free and
// /diagnostics is polled — so the staleness has to be BOUNDED, not permanent.
func TestWatcherInotifyBudget_CacheIsBounded(t *testing.T) {
	repoA, repoB := budgetRepo(t), budgetRepo(t)
	w, err := NewWatcherConfig(Config{Debounce: time.Hour}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	defer w.Stop()
	if _, err := w.AddRepo(repoA); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	first := w.InotifyBudget()
	if _, err := w.AddRepo(repoB); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if second := w.InotifyBudget(); second.Dirs != first.Dirs {
		t.Fatalf("the report was recomputed inside the TTL: %d then %d", first.Dirs, second.Dirs)
	}
	// Age the cache past the TTL: the next read must reflect the second repo.
	w.inotifyProbeMu.Lock()
	w.inotifyProbeAt = w.inotifyProbeAt.Add(-2 * inotifyProbeTTL)
	w.inotifyProbeMu.Unlock()
	third := w.InotifyBudget()
	if third.Dirs != 2*first.Dirs {
		t.Fatalf("after the TTL the report says %d dirs for two identical repos, want %d — "+
			"the cache never expires, so the announcement is permanently stale", third.Dirs, 2*first.Dirs)
	}
}
