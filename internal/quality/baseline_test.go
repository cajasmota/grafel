package quality

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// baselineDoc mirrors internal/quality/golden/baseline.json. Only the fields
// this test gates are modelled; unknown keys are ignored.
type baselineDoc struct {
	Version          int                        `json:"version"`
	Regenerate       string                     `json:"regenerate"`
	MeasuredOn       string                     `json:"measured_on"`
	MeasuredOnNote   string                     `json:"measured_on_note"`
	Fixtures         map[string]baselineFixture `json:"fixtures"`
	KnownRegressions []knownRegression          `json:"known_regressions"`
}

// knownRegression annotates one recorded figure that is LOWER than it used to
// be. The ratchet cannot distinguish "this floor was always this low" from
// "this floor was lowered deliberately and is tracked" — both are just numbers
// in fixtures{} — so the drop is spelled out here alongside the issue that
// owns it. scripts/quality/ratchet.py carries this list across
// --update-baseline and prints it on every check.
type knownRegression struct {
	Fixture string `json:"fixture"`
	Metric  string `json:"metric"`
	Was     int    `json:"was"`
	Now     int    `json:"now"`
	Issue   string `json:"issue"`
	Note    string `json:"note"`
}

type baselineFixture struct {
	ExpectationsMissing bool `json:"expectations_missing"`
	EntityFound         int  `json:"entity_found"`
	EntityExpected      int  `json:"entity_expected"`
	RelFound            int  `json:"relationship_found"`
	RelExpected         int  `json:"relationship_expected"`
}

const (
	goldenDir    = "golden"
	baselinePath = "golden/baseline.json"
)

func loadBaseline(t *testing.T) baselineDoc {
	t.Helper()
	raw, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("read %s: %v", baselinePath, err)
	}
	var doc baselineDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", baselinePath, err)
	}
	if doc.Version == 0 {
		t.Fatalf("%s: missing version field", baselinePath)
	}
	if doc.Regenerate == "" {
		t.Fatalf("%s: missing regenerate field — the baseline must name the "+
			"command that re-derives it (Refs #6231)", baselinePath)
	}
	return doc
}

func fixtureDirs(t *testing.T) []string {
	t.Helper()
	ents, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatalf("read %s: %v", goldenDir, err)
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() || e.Name()[0] == '.' {
			continue
		}
		names = append(names, e.Name())
	}
	return names
}

// TestBaselineCoversEveryFixture is the cheap, always-run half of the quality
// ratchet (Refs #6231). The expensive half — actually indexing all twenty
// fixtures — lives in scripts/quality/run.sh --ratchet. This test guards the
// structural invariant that makes that gate meaningful: every fixture
// directory is accounted for in the baseline, so a new fixture cannot be added
// and silently escape gating, and a fixture cannot be deleted to make a red
// number disappear.
func TestBaselineCoversEveryFixture(t *testing.T) {
	doc := loadBaseline(t)
	dirs := fixtureDirs(t)

	seen := make(map[string]bool, len(dirs))
	for _, name := range dirs {
		seen[name] = true
		if _, ok := doc.Fixtures[name]; !ok {
			t.Errorf("fixture %q has no baseline entry — run %q so it is gated",
				name, doc.Regenerate)
		}
	}
	for name := range doc.Fixtures {
		if !seen[name] {
			t.Errorf("baseline records fixture %q but no such directory exists "+
				"under internal/quality/golden/", name)
		}
	}
}

// TestBaselineExpectationsPresenceMatchesDisk pins the two-state distinction
// the issue asked to be split out: a fixture either carries expected.json and
// is graded on recall, or carries none and is recorded as ungraded. Silently
// dropping an expected.json (which would make a red fixture vanish from the
// gate rather than fail it) is a test failure.
func TestBaselineExpectationsPresenceMatchesDisk(t *testing.T) {
	doc := loadBaseline(t)
	for _, name := range fixtureDirs(t) {
		base, ok := doc.Fixtures[name]
		if !ok {
			continue // reported by TestBaselineCoversEveryFixture
		}
		_, statErr := os.Stat(filepath.Join(goldenDir, name, "expected.json"))
		onDisk := statErr == nil
		if onDisk && base.ExpectationsMissing {
			t.Errorf("fixture %q now has expected.json but the baseline records it "+
				"as missing — run %q to start gating it", name, doc.Regenerate)
		}
		if !onDisk && !base.ExpectationsMissing {
			t.Errorf("fixture %q has no expected.json but the baseline expects one — "+
				"the expectations file was deleted", name)
		}
	}
}

// TestBaselineRecordedCountsAreSane keeps the recorded floor from being edited
// into something vacuous: a fixture cannot claim to have found more must-haves
// than it declares, and a graded fixture must declare at least one must-have.
func TestBaselineRecordedCountsAreSane(t *testing.T) {
	doc := loadBaseline(t)
	for name, base := range doc.Fixtures {
		if base.ExpectationsMissing {
			continue
		}
		if base.EntityFound > base.EntityExpected {
			t.Errorf("fixture %q: entity_found %d exceeds entity_expected %d",
				name, base.EntityFound, base.EntityExpected)
		}
		if base.RelFound > base.RelExpected {
			t.Errorf("fixture %q: relationship_found %d exceeds relationship_expected %d",
				name, base.RelFound, base.RelExpected)
		}
		if base.EntityExpected == 0 && base.RelExpected == 0 {
			t.Errorf("fixture %q declares no must-have entities or relationships — "+
				"it cannot fail, so it is not a gate", name)
		}
	}
}

// TestBaselineMeasuredOnIsReachable guards the one field whose entire job is to
// name the commit these figures describe.
//
// `git_sha()` in scripts/quality/ratchet.py records HEAD at measurement time.
// Run --update-baseline on an unpushed commit, then amend or rebase it, and the
// recorded sha is a dangling sibling: it still resolves in the author's object
// store and resolves nowhere else. That happened on this very file — it recorded
// 01705c852, a pre-amend sibling that was never an ancestor of anything pushed.
//
// The check is ancestry, not existence, because existence is exactly what is
// misleading locally. On a shallow clone (actions/checkout defaults to
// fetch-depth 1) history is absent and ancestry is unanswerable, so the test
// degrades to a shape check rather than skipping outright. That is the honest
// coverage: --update-baseline only ever runs on a developer machine — no
// workflow invokes it, per the comment in scripts/quality/run.sh — which is
// where a full clone is, and where the mistake gets made.
func TestBaselineMeasuredOnIsReachable(t *testing.T) {
	doc := loadBaseline(t)
	sha := strings.TrimSpace(doc.MeasuredOn)
	if sha == "" {
		t.Fatal("baseline.json: measured_on is empty — the figures name no commit")
	}
	if doc.MeasuredOnNote == "" {
		t.Error("baseline.json: measured_on_note is empty — measured_on is a BASE " +
			"commit, which is not self-evident and has been misread before")
	}
	for _, r := range sha {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("baseline.json: measured_on %q is not a hex sha", sha)
		}
	}

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	if out, err := exec.Command("git", "rev-parse", "--is-shallow-repository").Output(); err == nil &&
		strings.TrimSpace(string(out)) == "true" {
		t.Skip("shallow clone: history absent, ancestry unanswerable; shape checked above")
	}
	if err := exec.Command("git", "cat-file", "-e", sha+"^{commit}").Run(); err != nil {
		t.Fatalf("baseline.json: measured_on %q is not a commit in this repository", sha)
	}
	if err := exec.Command("git", "merge-base", "--is-ancestor", sha, "HEAD").Run(); err != nil {
		t.Fatalf("baseline.json: measured_on %q is not an ancestor of HEAD — it is most "+
			"likely a pre-amend or pre-rebase sibling that resolves only in this working "+
			"copy; re-record it as the base commit the figures were measured against", sha)
	}
}

// metricField maps a known_regressions metric name onto the recorded figure it
// annotates. Only the two "found" metrics can regress; the "expected" figures
// are properties of expected.json, not of extraction.
func metricField(b baselineFixture, metric string) (int, bool) {
	switch metric {
	case "entity_found":
		return b.EntityFound, true
	case "relationship_found":
		return b.RelFound, true
	}
	return 0, false
}

// TestKnownRegressionsAgreeWithRecordedFloor is what makes the annotation
// self-clearing rather than a comment that rots.
//
// A known_regressions entry claims "this figure used to be Was and is now Now".
// If the recorded floor no longer equals Now, one of two things happened: the
// defect was fixed and the entry is stale, or the figure moved again and the
// entry understates the damage. Either way the entry is lying and must be
// re-stated or deleted. Without this check the block would be prose — it could
// disagree with the numbers three lines above it and nothing would notice.
func TestKnownRegressionsAgreeWithRecordedFloor(t *testing.T) {
	doc := loadBaseline(t)
	for _, kr := range doc.KnownRegressions {
		base, ok := doc.Fixtures[kr.Fixture]
		if !ok {
			t.Errorf("known_regressions names fixture %q, which has no baseline entry", kr.Fixture)
			continue
		}
		got, known := metricField(base, kr.Metric)
		if !known {
			t.Errorf("known_regressions[%s]: metric %q is not one of entity_found, relationship_found",
				kr.Fixture, kr.Metric)
			continue
		}
		if kr.Was <= kr.Now {
			t.Errorf("known_regressions[%s.%s]: was=%d now=%d is not a regression",
				kr.Fixture, kr.Metric, kr.Was, kr.Now)
		}
		if got != kr.Now {
			t.Errorf("known_regressions[%s.%s]: records now=%d but the baseline floor is %d — "+
				"the figure moved; re-state the entry, or delete it if the regression is fixed",
				kr.Fixture, kr.Metric, kr.Now, got)
		}
		if kr.Issue == "" {
			t.Errorf("known_regressions[%s.%s]: no issue — an untracked regression is "+
				"indistinguishable from an accepted one", kr.Fixture, kr.Metric)
		}
		if kr.Note == "" {
			t.Errorf("known_regressions[%s.%s]: no note — the next reader needs the mechanism, "+
				"not just the delta", kr.Fixture, kr.Metric)
		}
	}
}

// mustAnnotate is the closed set of recorded figures that are known to sit
// BELOW a level extraction previously reached, and therefore may not appear in
// baseline.json as a bare number.
//
// It is deliberately in Go rather than derived from baseline.json: otherwise the
// annotation and the thing it annotates can be removed in one edit — delete the
// known_regressions entry and the drop becomes an ordinary-looking floor, with
// the ratchet still green and every test in this file still passing. Adding or
// removing a pair here is the reviewable act.
//
// (#6260 introduced a second such set, ungradedFixtures, on the same reasoning.
// #6273 deleted it: once every fixture is gated there is no legitimate value the
// set can hold, so no configuration of it changes any outcome — verified by
// mutation, see TestGoldenSetIsFullyGraded_6273. mustAnnotate is different, and
// survives, because known_regressions entries legitimately come and go.)
//
// Recorded 2026-08-08 while enabling the custom extractors for the golden
// benchmark (#6260). Those entries are kind-collision artefacts of that pass,
// not lost extraction; see the notes in baseline.json.
//
// The three entity_found pairs were added by #6277, which stopped ormlink's
// MAPS_TO anchor from satisfying an entity expectation. Extraction did not
// change: each of those figures was already this low and the benchmark could
// not see it, because the matcher resolved on Kind+Name and an anchor carries
// the same Kind and Name as the model it stands in for. They are annotated
// rather than left as bare floors for exactly the reason the block exists
// (scripts/quality/ratchet.py:33-40): a reader who sees java-spring-mini stop
// being a 25/25 fixture needs to know the point was never earned, and which
// issue owns the extraction defect that is now visible.
var mustAnnotate = []struct{ fixture, metric string }{
	{"elixir-phoenix-mini", "entity_found"},
	{"java-spring-mini", "entity_found"},
	{"java-spring-mini", "relationship_found"},
	{"python-django-mini", "entity_found"},
	{"python-django-mini", "relationship_found"},
}

// TestEveryKnownRegressionIsAnnotated fails if a tracked drop loses its
// annotation, so the known_regressions block cannot be quietly emptied.
func TestEveryKnownRegressionIsAnnotated(t *testing.T) {
	doc := loadBaseline(t)
	have := map[string]bool{}
	for _, kr := range doc.KnownRegressions {
		have[kr.Fixture+"\x00"+kr.Metric] = true
	}
	for _, want := range mustAnnotate {
		if !have[want.fixture+"\x00"+want.metric] {
			t.Errorf("baseline.json records %s.%s below a level extraction once reached, "+
				"but known_regressions does not explain it — restore the entry, or drop "+
				"the pair from mustAnnotate if the figure has recovered",
				want.fixture, want.metric)
		}
	}
}

// TestKnownRegressionsHaveNoDuplicates keeps two entries from claiming
// different histories for the same figure, which would make the block
// unreadable and let a later drop hide behind an earlier one.
func TestKnownRegressionsHaveNoDuplicates(t *testing.T) {
	doc := loadBaseline(t)
	seen := map[string]bool{}
	for _, kr := range doc.KnownRegressions {
		key := kr.Fixture + "\x00" + kr.Metric
		if seen[key] {
			t.Errorf("known_regressions has two entries for %s.%s", kr.Fixture, kr.Metric)
		}
		seen[key] = true
	}
}
