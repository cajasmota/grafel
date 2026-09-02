package main

import (
	"context"
	"strings"
	"testing"
)

// fakeSource returns canned upstream state keyed by repo slug, so the compare
// logic is exercised without any network access.
type fakeSource struct {
	data map[string]Upstream
	errs map[string]error
}

func (f fakeSource) Latest(_ context.Context, repo string) (Upstream, error) {
	if err := f.errs[repo]; err != nil {
		return Upstream{}, err
	}
	return f.data[repo], nil
}

func pinSet(ps ...Pin) Pins {
	m := map[string]Pin{}
	for _, p := range ps {
		m[p.Source] = p
	}
	return Pins{bySource: m}
}

func newLock() *Lock {
	return &Lock{
		LastVerified: "2026-06-23",
		Grammars: []GrammarSpec{
			{Language: "java", Source: "tree-sitter/tree-sitter-java"},
			{Language: "kotlin", Source: "fwcd/tree-sitter-kotlin"},
			{Language: "swift", Source: "alex-pinkus/tree-sitter-swift"},
			{Language: "unpinned", Source: "example/tree-sitter-unpinned"},
			{Language: "broken", Source: "example/tree-sitter-broken"},
		},
	}
}

func newPins() Pins {
	return pinSet(
		Pin{Source: "tree-sitter/tree-sitter-java", Release: "v0.23.5", Origin: "go.mod"},
		Pin{Source: "fwcd/tree-sitter-kotlin", Release: "0.3.8", Date: "2024-08-03", Origin: "kotlin.go"},
		Pin{Source: "alex-pinkus/tree-sitter-swift", Date: "2026-06-01", Origin: "go.mod"},
		// Pinned, but its upstream lookup fails: unreachable, not unknown.
		Pin{Source: "example/tree-sitter-broken", Release: "v1.0.0", Origin: "go.mod"},
	)
}

func newUpstream() fakeSource {
	return fakeSource{
		data: map[string]Upstream{
			// Genuinely behind: pinned v0.23.5, upstream v0.25.0.
			"tree-sitter/tree-sitter-java": {Release: "v0.25.0", CommitDate: "2025-09-15", Kind: "release"},
			// Equal to upstream: the live false-positive.
			"fwcd/tree-sitter-kotlin": {Release: "0.3.8", CommitDate: "2026-08-01", Kind: "release"},
			// Pseudo-version pin, compared by date.
			"alex-pinkus/tree-sitter-swift": {Release: "0.7.3", CommitDate: "2026-09-01", Kind: "release"},
		},
		errs: map[string]error{
			"example/tree-sitter-broken": context.DeadlineExceeded,
		},
	}
}

func byLanguage(rep Report) map[string]Result {
	by := map[string]Result{}
	for _, g := range rep.Grammars {
		by[g.Language] = g
	}
	return by
}

// TestCheck_EqualToUpstreamIsCurrent pins the live symptom of #6749: kotlin is
// bundled at 0.3.8, which IS the latest upstream release, and the cron reported
// it "~23 mo behind" anyway.
func TestCheck_EqualToUpstreamIsCurrent(t *testing.T) {
	rep := check(context.Background(), newLock(), newPins(), newUpstream())
	k := byLanguage(rep)["kotlin"]

	if k.Stale {
		t.Errorf("kotlin is pinned at 0.3.8 and upstream latest is 0.3.8 — must not be STALE (%+v)", k)
	}
	if k.Unknown {
		t.Errorf("kotlin has a resolved pin; it must not be UNKNOWN (%+v)", k)
	}
	if k.Bundled.Release != "0.3.8" {
		t.Errorf("kotlin bundled = %q, want the real pin 0.3.8", k.Bundled.Release)
	}
	if k.Behind != 0 {
		t.Errorf("kotlin Behind = %v, want 0", k.Behind)
	}
}

// TestCheck_BehindPinIsStale is the under-firing guard: an alarm that never
// fires is as useless as one that always fires.
func TestCheck_BehindPinIsStale(t *testing.T) {
	rep := check(context.Background(), newLock(), newPins(), newUpstream())
	j := byLanguage(rep)["java"]

	if !j.Stale {
		t.Fatalf("java is pinned v0.23.5 against upstream v0.25.0 — must be STALE (%+v)", j)
	}
	if j.Bundled.Release != "v0.23.5" {
		t.Errorf("java bundled = %q, want v0.23.5", j.Bundled.Release)
	}
	if j.Basis != basisRelease {
		t.Errorf("java Basis = %q, want %q (both sides are releases)", j.Basis, basisRelease)
	}
	if rep.StaleCount < 1 {
		t.Errorf("StaleCount = %d, want at least java", rep.StaleCount)
	}
}

// TestCheck_PseudoVersionComparesByDate covers the swift-shaped pin: a
// pseudo-version is a commit, so the comparison falls back to commit dates.
func TestCheck_PseudoVersionComparesByDate(t *testing.T) {
	rep := check(context.Background(), newLock(), newPins(), newUpstream())
	s := byLanguage(rep)["swift"]

	if s.Basis != basisDate {
		t.Errorf("swift Basis = %q, want %q (pseudo-version pin)", s.Basis, basisDate)
	}
	if !s.Stale {
		t.Errorf("swift pinned 2026-06-01 vs upstream 2026-09-01 — must be STALE (%+v)", s)
	}
	if s.Bundled.Date != "2026-06-01" {
		t.Errorf("swift bundled date = %q, want 2026-06-01", s.Bundled.Date)
	}
	if m := months(s.Behind); m != 3 {
		t.Errorf("swift behind = %d mo, want 3", m)
	}
}

// TestCheck_UnresolvedPinIsUnknownNotStale is the original defect, inverted:
// "I could not read this" must never render as "24 months stale".
func TestCheck_UnresolvedPinIsUnknownNotStale(t *testing.T) {
	rep := check(context.Background(), newLock(), newPins(), newUpstream())
	u := byLanguage(rep)["unpinned"]

	if !u.Unknown {
		t.Errorf("unpinned has no go.mod or vendored pin — must be UNKNOWN (%+v)", u)
	}
	if u.Stale {
		t.Error("an unresolved grammar must not be counted stale")
	}
	if u.Bundled.Release != "" || u.Bundled.Date != "" {
		t.Errorf("unresolved grammar must carry no invented version, got %+v", u.Bundled)
	}
	if rep.UnknownCount != 1 {
		t.Errorf("UnknownCount = %d, want 1", rep.UnknownCount)
	}

	// The lock/go.mod reconciliation must name it rather than swallow it.
	if !contains(rep.Unpinned, "unpinned") {
		t.Errorf("Unpinned = %v, want it to name \"unpinned\"", rep.Unpinned)
	}
}

// TestCheck_PinAbsentFromLockIsReported is the other reconciliation direction.
func TestCheck_PinAbsentFromLockIsReported(t *testing.T) {
	pins := newPins()
	pins.bySource["tree-sitter/tree-sitter-ghost"] = Pin{
		Source: "tree-sitter/tree-sitter-ghost", Release: "v9.9.9", Origin: "go.mod",
	}
	rep := check(context.Background(), newLock(), pins, newUpstream())

	if !contains(rep.Unlocked, "tree-sitter/tree-sitter-ghost") {
		t.Errorf("Unlocked = %v, want it to name the pinned-but-unlocked grammar", rep.Unlocked)
	}
	// The lock's own rows must not be silently dropped to make counts match.
	if len(rep.Grammars) != len(newLock().Grammars) {
		t.Errorf("reported %d grammars, want all %d lock rows", len(rep.Grammars), len(newLock().Grammars))
	}
}

// TestCheck_OutputChangesWhenThePinChanges is the non-vacuity gate: a table
// that is identical before and after a version bump IS the defect.
func TestCheck_OutputChangesWhenThePinChanges(t *testing.T) {
	render := func(p Pins) string {
		var sb strings.Builder
		writeTable(&sb, check(context.Background(), newLock(), p, newUpstream()))
		return sb.String()
	}

	before := render(newPins())

	bumped := newPins()
	bumped.bySource["tree-sitter/tree-sitter-java"] = Pin{
		Source: "tree-sitter/tree-sitter-java", Release: "v0.25.0", Origin: "go.mod",
	}
	after := render(bumped)

	if before == after {
		t.Fatal("bumping the java pin to upstream latest did not change the report at all")
	}
	if !strings.Contains(before, "v0.23.5") {
		t.Errorf("pre-bump table should show the old pin\n%s", before)
	}
	if strings.Contains(after, "v0.23.5") {
		t.Errorf("post-bump table still shows the OLD pin\n%s", after)
	}
	if a, b := staleOf(t, bumped, "java"), staleOf(t, newPins(), "java"); a || !b {
		t.Errorf("java stale before=%v after=%v, want true then false", b, a)
	}
}

func staleOf(t *testing.T, p Pins, lang string) bool {
	t.Helper()
	return byLanguage(check(context.Background(), newLock(), p, newUpstream()))[lang].Stale
}

func TestCheck_UpstreamErrorIsNotStale(t *testing.T) {
	rep := check(context.Background(), newLock(), newPins(), newUpstream())
	b := byLanguage(rep)["broken"]
	if b.Err == nil {
		t.Error("broken should carry the lookup error")
	}
	if b.Stale {
		t.Error("an errored grammar must not be counted stale")
	}
	if rep.Errored != 1 {
		t.Errorf("Errored = %d, want 1", rep.Errored)
	}
}

func TestMarkdown_ShowsRealPinsAndReconciliation(t *testing.T) {
	pins := newPins()
	pins.bySource["tree-sitter/tree-sitter-ghost"] = Pin{
		Source: "tree-sitter/tree-sitter-ghost", Release: "v9.9.9", Origin: "go.mod",
	}
	rep := check(context.Background(), newLock(), pins, newUpstream())

	var sb strings.Builder
	writeMarkdown(&sb, rep)
	out := sb.String()

	for _, want := range []string{
		"v0.23.5",               // java's real pin, not a constant date
		"tree-sitter-ghost",     // pinned but absent from the lock
		"unpinned",              // in the lock but not pinned
		"#5359",                 // provenance footer preserved
		"2 of 5 grammars stale", // java + swift, NOT kotlin
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q\n%s", want, out)
		}
	}
	// The false premise must be gone for good.
	for _, forbidden := range []string{"smacker", "2024-08-27"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("markdown still references the dead binding %q\n%s", forbidden, out)
		}
	}
	// kotlin equals upstream; it must not appear in the stale table.
	if strings.Contains(out, "fwcd/tree-sitter-kotlin") {
		t.Errorf("kotlin is current and must not be listed as stale\n%s", out)
	}
}

func TestMonths(t *testing.T) {
	// 2024-08-27 -> 2025-09-15 is ~12.5 months.
	d := parseDate("2025-09-15").Sub(parseDate("2024-08-27"))
	if m := months(d); m < 12 || m > 13 {
		t.Errorf("months = %d, want ~12", m)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
