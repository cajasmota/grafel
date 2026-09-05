package quality

import (
	"bytes"
	"encoding/json"
	"errors"
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

	// #6488 arm D. The aggregate extraction totals. Pointers, not ints,
	// because absence is the failure this models: a zero-valued int is
	// indistinguishable from a key that was never written, and "no ceiling was
	// recorded for this fixture" is exactly what must not read as a figure.
	EntityExtractedTotal *int `json:"entity_extracted_total"`
	RelExtractedTotal    *int `json:"relationship_extracted_total"`
}

// fixtureKeyOrder is the order scripts/quality/ratchet.py's build() inserts
// keys into a per-fixture entry, and therefore the order --update-baseline
// writes them in.
var fixtureKeyOrder = []string{
	"entity_found",
	"entity_expected",
	"relationship_found",
	"relationship_expected",
	"entity_extracted_total",
	"relationship_extracted_total",
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

// TestBaselineRecordsAnExtractionCeiling checks that every graded fixture
// carries both aggregate extraction totals, and that each one is at least the
// must-have count it is a superset of (Refs #6488 arm D).
//
// What this observes is presence and internal consistency of the recorded
// figures — NOT that the ratchet compares them. That is graded in
// scripts/quality/test_ratchet.py, which runs `check` and reads its exit
// status; nothing about a number sitting in a JSON file can show it is read.
//
// The reason presence needs a gate of its own: ratchet.py's check() treats a
// fixture whose baseline entry carries no total as a failure demanding a
// re-record, and that failure is only ever seen by whoever runs the quality
// job. This test is in the cheap `go test` leg, so a baseline that lost a
// ceiling — a hand-edit, a bad merge, a stale --update-baseline from a binary
// that predates the field — is reported on the PR that does it.
func TestBaselineRecordsAnExtractionCeiling(t *testing.T) {
	doc := loadBaseline(t)
	for name, base := range doc.Fixtures {
		if base.ExpectationsMissing {
			continue
		}
		if base.EntityExtractedTotal == nil {
			t.Errorf("fixture %q: no entity_extracted_total — nothing bounds how "+
				"many entities it may emit; run %q", name, doc.Regenerate)
		} else if *base.EntityExtractedTotal < base.EntityFound {
			t.Errorf("fixture %q: entity_extracted_total %d is below entity_found %d — "+
				"the ceiling cannot be under the floor it contains",
				name, *base.EntityExtractedTotal, base.EntityFound)
		}
		if base.RelExtractedTotal == nil {
			t.Errorf("fixture %q: no relationship_extracted_total — nothing bounds "+
				"how many relationships it may emit; run %q", name, doc.Regenerate)
		} else if *base.RelExtractedTotal < base.RelFound {
			t.Errorf("fixture %q: relationship_extracted_total %d is below "+
				"relationship_found %d — the ceiling cannot be under the floor it "+
				"contains", name, *base.RelExtractedTotal, base.RelFound)
		}
	}
}

// TestBaselineFixtureKeysAreInWriterOrder pins the ORDER of the keys inside
// every per-fixture entry of the committed baseline.json (Refs #6488 arm D).
//
// This is not covered by the canonicality gate below, and that is not an
// assumption: serialize() in scripts/quality/ratchet.py hands json.dumps a
// document `canon` parsed straight back from disk, and json.dumps emits a
// mapping's own insertion order — so canon's output echoes whatever order the
// FILE already has and every reordering round-trips clean. serialize()'s
// docstring says as much; it was re-read against the code before this test was
// written, and it holds.
//
// Order matters here for the reason canonicality matters at all: baseline.json
// is generated, and the next legitimate --update-baseline rewrites it in the
// writer's order. If the committed order differs, that regeneration shows up
// as churn on lines nobody touched, which is what hides the one deliberate
// figure a baseline diff exists to show. Arm D inserted two keys per entry, so
// a disagreement between build()'s insertion order and the committed order is
// live for the first time.
//
// The writer's own order is pinned separately, in
// test_update_records_both_totals_after_the_recall_figures in
// scripts/quality/test_ratchet.py. Neither test alone says the two agree; both
// name the same sequence, so a change to one that is not made to the other
// goes red.
func TestBaselineFixtureKeysAreInWriterOrder(t *testing.T) {
	raw, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("read %s: %v", baselinePath, err)
	}
	got := fixtureEntryKeys(t, raw)
	if len(got) == 0 {
		t.Fatalf("%s: parsed no fixture entries — the reader, not the file, is "+
			"what this test would otherwise be grading", baselinePath)
	}
	for name, keys := range got {
		if !slicesEqual(keys, fixtureKeyOrder) {
			t.Errorf("fixture %q: keys are %v, want %v — regenerate with %q "+
				"rather than hand-editing", name, keys, fixtureKeyOrder,
				loadBaseline(t).Regenerate)
		}
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fixtureEntryKeys returns, per fixture name, the keys of that fixture's entry
// in the order they appear in the raw bytes. encoding/json's map decoding
// throws that order away, so this walks the token stream instead.
//
// Every value inside a fixture entry is a scalar today; one that is not is a
// shape change this reader must not silently skip over, so it fails.
func fixtureEntryKeys(t *testing.T, raw []byte) map[string][]string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	mustToken := func() json.Token {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("%s: reading token: %v", baselinePath, err)
		}
		return tok
	}
	expectDelim := func(want rune, what string) {
		tok := mustToken()
		if d, ok := tok.(json.Delim); !ok || rune(d) != want {
			t.Fatalf("%s: expected %q at %s, got %v", baselinePath, want, what, tok)
		}
	}

	expectDelim('{', "top level")
	for dec.More() {
		key, ok := mustToken().(string)
		if !ok {
			t.Fatalf("%s: top-level key is not a string", baselinePath)
		}
		if key != "fixtures" {
			// Skip the value, whatever shape it has.
			var discard json.RawMessage
			if err := dec.Decode(&discard); err != nil {
				t.Fatalf("%s: skipping %q: %v", baselinePath, key, err)
			}
			continue
		}
		out := map[string][]string{}
		expectDelim('{', "fixtures")
		for dec.More() {
			name, ok := mustToken().(string)
			if !ok {
				t.Fatalf("%s: fixture name is not a string", baselinePath)
			}
			expectDelim('{', "fixture "+name)
			var keys []string
			for dec.More() {
				k, ok := mustToken().(string)
				if !ok {
					t.Fatalf("%s: fixture %q has a non-string key", baselinePath, name)
				}
				if d, isDelim := mustToken().(json.Delim); isDelim {
					t.Fatalf("%s: fixture %q key %q holds a %v, not a scalar — this "+
						"reader would skip past it", baselinePath, name, k, d)
				}
				keys = append(keys, k)
			}
			expectDelim('}', "end of fixture "+name)
			out[name] = keys
		}
		return out
	}
	t.Fatalf("%s: no fixtures object", baselinePath)
	return nil
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
//
// #6275 (the fold-into-twin + dedup-by-ID + twin_of anchor-id fixes at
// cmd/grafel/index.go and internal/engine/classfold.go) closed FOUR of the
// five pairs above: java-spring-mini.entity_found, java-spring-mini.
// relationship_found, elixir-phoenix-mini.entity_found, and python-django-
// mini.relationship_found all returned to their recorded `was` figure, and
// ratchet.py self-cleared their known_regressions entries on --update-
// baseline. Removing them from this list is that same reviewable act the
// comment above describes — done deliberately here, not as a side effect of
// touching baseline.json. python-django-mini.entity_found remains: its
// residual miss (SCOPE.Component User) is #6276, a DIFFERENT mechanism than
// #6275 (the base class node there folds into a bare "Model"-kind record,
// never colliding with any #6104 twin or ormlink sentinel at all) — see the
// note on that known_regressions entry in baseline.json for the full
// re-diagnosis, including python-django-mini's ActiveUserManager, which WAS
// #6275's mechanism and is fixed.
//
// #6276 closed that last pair, so the list is now EMPTY. The fix is the mirror
// of #6275's fold rule: a record that some other record names as its #6104
// merge-facet anchor (grafel.twin_of) is no longer an eligible fold SOURCE, so
// the base SCOPE.Component User can no longer be folded into the Pass 2.5
// bare-"Model" record while the Django custom pass's SCOPE.Schema/model facet
// is anchored on it. python-django-mini went 26/29 -> 29/29 entities and 8/13
// -> 12/13 relationships; no other fixture's figures moved. Removing the pair
// here is the same reviewable act the comment above describes.
//
// An empty list is a legitimate state, not a dormant one: the two tests below
// still run, and TestEveryKnownRegressionIsAnnotated's loop is what a future
// entry re-enters. Adding a bare floor below a previously-reached level without
// adding its pair here is what the block exists to catch.
var mustAnnotate = []struct{ fixture, metric string }{}

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

// runCanon runs `ratchet.py canon` over one file and returns its exit code plus
// combined output.
//
// What this gates: the byte-level encoding of the document — ensure_ascii's
// \uXXXX escaping of non-ASCII, two-space indent with the (',', ': ')
// separators that implies, no HTML escaping of < > &, Python's float repr, and
// the trailing newline.
//
// What it does NOT gate: key ORDER. `canon` re-serialises `json.loads(text)`,
// which echoes back whatever order the file already has, so an alphabetically
// reordered baseline passes. Catching that would mean comparing against the
// order the builder's dicts are constructed in, which is only observable by
// running the builder — i.e. a full indexer run — and is therefore out of
// reach for a check that reads nothing but the file. Do not read the list
// above as including order.
//
// The check lives on the Python side deliberately: baseline.json is written by
// `json.dump(doc, indent=2)` with the default ensure_ascii=True, and that call
// reproduces the encoding rather than re-implementing it. A Go re-marshal would
// have to hand-roll every item in the gated list as a second formatter, which
// can itself drift — precisely the failure this gate exists to prevent. It is
// *driven* from Go so that it runs in the ordinary `go test
// ./internal/quality/...` CI leg.
//
// That last sentence used to carry a second clause: ".github/workflows/
// quality.yml, where the ratchet itself runs, is dispatch-only and therefore
// gates no pull request". #6231 made quality.yml always-on, so it is now false
// and has been removed. Note what changed and what did not: the ORIGINAL
// motive for driving this from Go — that otherwise nothing would run it on a
// PR — no longer applies, because quality.yml would now run it too. The
// placement is kept anyway, on the narrower ground that this check reads a
// checked-in file and needs no built binary, so it belongs in the cheap
// `go test` leg rather than behind a fixture-indexing job. Nothing this
// function asserts ever depended on the dispatch-only property; the clause was
// rationale, not premise.
func runCanon(t *testing.T, path string) (int, string) {
	t.Helper()
	out, err := exec.Command("python3", ratchetScript(t), "canon", path).CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), string(out)
	}
	t.Fatalf("run ratchet.py canon %s: %v\n%s", path, err, out)
	return -1, ""
}

// TestBaselineIsCanonicallyEncoded is the canonicality gate for the ratchet
// floor (Refs #6466).
//
// baseline.json is generated, but nothing checked that the committed bytes are
// what the generator emits. A round-trip through a JSON library with different
// defaults silently rewrote four unrelated escaped em-dashes into literal ones
// inside a three-line change; CI could not see it, and the next legitimate
// --update-baseline would have shown that churn as a spurious diff on lines
// nobody touched. This is the failure that `go run ./tools/coverage fmt
// --check` prevents for docs/coverage/registry.json, and that baseline.json —
// a *test gate*, not documentation — had no equivalent of.
//
// The negative subtests are load-bearing: without them a check that normalised
// whitespace, or unescaped before comparing, would still pass on a canonical
// file, and the gate would only catch gross corruption.
func TestBaselineIsCanonicallyEncoded(t *testing.T) {
	t.Run("committed baseline round-trips byte-for-byte", func(t *testing.T) {
		code, out := runCanon(t, baselinePath)
		if code != 0 {
			t.Fatalf("baseline.json is not what scripts/quality/ratchet.py writes "+
				"(exit %d). Regenerate it with the documented command rather than "+
				"hand-editing, and do not reformat unrelated lines:\n%s", code, out)
		}
	})

	raw, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("read %s: %v", baselinePath, err)
	}

	// Each mutation is a real churn class that has landed, or trivially could:
	// an ensure_ascii=False round-trip, a re-indent, and a stripped trailing
	// newline. A gate that accepts any of them is not a gate.
	mutations := []struct {
		name  string
		apply func(string) string
	}{
		{
			name: "non-ASCII unescaped (ensure_ascii=False round-trip)",
			apply: func(s string) string {
				// "\\u2014" is the six-character escape as it appears on
				// disk; "—" is the literal em-dash an
				// ensure_ascii=False writer would leave in its place.
				return strings.Replace(s, "\\u2014", "—", 1)
			},
		},
		{
			name: "re-indented with four spaces",
			apply: func(s string) string {
				return strings.Replace(s, "\n  \"version\"", "\n    \"version\"", 1)
			},
		},
		{
			name: "trailing newline stripped",
			apply: func(s string) string {
				return strings.TrimRight(s, "\n")
			},
		},
	}

	for _, m := range mutations {
		t.Run("rejects "+m.name, func(t *testing.T) {
			mutated := m.apply(string(raw))
			if mutated == string(raw) {
				t.Fatalf("mutation %q changed nothing — the check would be vacuous", m.name)
			}
			path := filepath.Join(t.TempDir(), "baseline.json")
			if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
				t.Fatalf("write mutated baseline: %v", err)
			}
			code, out := runCanon(t, path)
			if code == 0 {
				t.Fatalf("canonicality check accepted a baseline mutated by %q — "+
					"encoding churn would land unnoticed:\n%s", m.name, out)
			}
		})
	}
}
