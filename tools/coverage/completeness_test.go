package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// These tests grade #6868: `backfill --check` and `validate`'s
// grouped-completeness check must be ONE decision, not two walks that
// happen to agree.
//
// The issue was filed on the premise that they already agreed on every
// input and that `validate` therefore masked the backfill gate step
// end-to-end. They did not agree. `missingTaxonomyCells`' gate is the
// record's SUBCATEGORY; validate's copy ran only from
// validateGroupedRecord, i.e. only when rec.IsGrouped(). A record whose
// subcategory declares a taxonomy but whose capabilities are not in the
// grouped shape — the shape `add` produces, and precisely the shape
// backfill exists to fill — was invisible to validate and caught only by
// `backfill --check`, at step 2/5, with only that step's annotation.
//
// So the two guards were a mirror in the doc comment and not in the code.
// The fix collapses them onto one predicate and moves validate's call
// site off IsGrouped(); these tests are what fails if either half comes
// undone.

// completenessNeedle is the distinctive tail of a grouped-completeness
// message. Matching on it rather than on the whole error text is what
// lets these tests isolate the completeness messages on registries that
// also trip other validation rules.
const completenessNeedle = "taxonomy but absent from record"

// completenessMessages returns the completeness messages
// validateRegistry produced, and nothing else.
//
// It reads BOTH channels on purpose. Which cells the two guards disagree
// about is independent of how severely validate files them, and
// completenessGateIsError moves every one of these messages from Errors
// to Warnings wholesale. An Errors-only helper would therefore report
// "no completeness messages" the moment the knob is flipped, and every
// test built on it would go red — including the one whose whole job is
// to make that flip safe. A severity knob documented as flippable has to
// survive being flipped, so severity is asserted where it is the
// subject (TestCompletenessGateSeverityIsHonoured,
// TestCompletenessSeverityKnobDecidesWhoCatchesIt) and nowhere else.
func completenessMessages(reg *Registry) []string {
	res := validateRegistry(reg, ".")
	var out []string
	for _, m := range append(append([]string{}, res.Errors...), res.Warnings...) {
		if strings.Contains(m, completenessNeedle) {
			out = append(out, m)
		}
	}
	return out
}

// wantCompletenessMessages renders the message validate is obliged to
// emit for each cell backfill would seed, so the two can be compared as
// SETS rather than as two counts that happen to match.
func wantCompletenessMessages(reg *Registry, plans []seedPlan) []string {
	idx := map[string]int{}
	for i, rec := range reg.Records {
		idx[rec.ID] = i
	}
	sub := map[string]string{}
	for _, rec := range reg.Records {
		sub[rec.ID] = rec.Subcategory
	}
	out := make([]string, 0, len(plans))
	for _, p := range plans {
		out = append(out, fmt.Sprintf("records[%d] (%s): lane key %q (group %q) declared by subcategory %q taxonomy but absent from record",
			idx[p.RecordID], p.RecordID, p.Key, p.Group, sub[p.RecordID]))
	}
	sort.Strings(out)
	return out
}

// groupedFixtureRegistry returns a registry whose single record is
// COMPLETE against the http_backend taxonomy: every declared lane key
// carries a cell. Callers then remove exactly one property.
func groupedFixtureRegistry(t *testing.T) *Registry {
	t.Helper()
	rec := Record{
		ID:          "lang.go.framework.chi",
		Category:    "http_framework",
		Subcategory: "http_backend",
		Language:    "go",
		Label:       "chi",
		Groups:      map[string]map[string]Capability{},
	}
	groups := groupsForSubcategory("http_backend")
	if len(groups) == 0 {
		t.Fatal("http_backend has no group taxonomy; these tests grade nothing")
	}
	for _, g := range groups {
		for _, key := range g.Keys {
			owner := groupForCapability("http_backend", key)
			if owner != g.Name {
				continue
			}
			if rec.Groups[owner] == nil {
				rec.Groups[owner] = map[string]Capability{}
			}
			rec.Groups[owner][key] = Capability{Status: StatusMissing, Issue: "seed"}
		}
	}
	reg := &Registry{SchemaVersion: SchemaVersion, Records: []Record{rec}}
	if plans := planBackfill(reg, "", ""); len(plans) != 0 {
		t.Fatalf("fixture is not complete: backfill would seed %d cell(s)", len(plans))
	}
	return reg
}

// ungroupedRecord is the shape the two guards used to disagree about: a
// record whose subcategory declares a group taxonomy, carrying no
// grouped cells at all. `coverage add` produces exactly this.
func ungroupedRecord() Record {
	return Record{
		ID:          "lang.go.framework.echo",
		Category:    "http_framework",
		Subcategory: "http_backend",
		Language:    "go",
		Label:       "Echo",
	}
}

// twoLanguageRegistry returns two incomplete records under the same
// grouped subcategory, in two different languages, deliberately stored
// in an order that is the REVERSE of their sorted order.
//
// The reversal is what makes the ordering assertion non-vacuous:
// planBackfill walks reg.Records in slice order, so an unsorted result
// would lead with "lang.zig..." and a sorted one must lead with
// "lang.go...".
func twoLanguageRegistry() *Registry {
	go1 := ungroupedRecord()
	go1.ID = "lang.go.framework.echo"
	go1.Language = "go"
	zig := ungroupedRecord()
	zig.ID = "lang.zig.framework.zap"
	zig.Language = "zig"
	zig.Label = "Zap"
	return &Registry{SchemaVersion: SchemaVersion, Records: []Record{zig, go1}}
}

// TestPlanBackfillFiltersScopeThePlan grades the claim planBackfill's doc
// comment makes about langFilter / subFilter — that they narrow the PLAN
// while the predicate underneath stays unscoped, which is exactly why a
// filtered `backfill --check` is deliberately narrower than `validate`
// and why the agreement invariant above is stated over the UNFILTERED
// plan.
//
// Before this, the filters were asserted only in prose. Inverting the
// language comparison (`!=` to `==`) left the whole package green.
func TestPlanBackfillFiltersScopeThePlan(t *testing.T) {
	reg := twoLanguageRegistry()

	unfiltered := planBackfill(reg, "", "")
	langs := map[string]int{}
	for _, p := range unfiltered {
		langs[p.Language]++
	}
	if len(langs) != 2 || langs["go"] == 0 || langs["zig"] == 0 {
		t.Fatalf("the unfiltered plan must span both languages, got %v; the filter cases below would be vacuous", langs)
	}

	for _, lang := range []string{"go", "zig"} {
		got := planBackfill(reg, lang, "")
		if len(got) == 0 {
			t.Fatalf("--language %s planned nothing", lang)
		}
		for _, p := range got {
			if p.Language != lang {
				t.Fatalf("--language %s planned a cell for %q (%s); the filter selects the wrong side", lang, p.Language, p.RecordID)
			}
		}
		if len(got) != langs[lang] {
			t.Fatalf("--language %s planned %d cell(s), but that language contributes %d to the unfiltered plan", lang, len(got), langs[lang])
		}
	}

	// The filter narrows the plan and nothing else: validate is
	// unscoped, so it still reports BOTH records' cells.
	if n := len(completenessMessages(reg)); n != len(unfiltered) {
		t.Fatalf("validate reported %d completeness message(s) for %d unfiltered cell(s); validate must stay unscoped", n, len(unfiltered))
	}

	// A subcategory filter naming a subcategory no record carries
	// selects nothing — the complement of the positive cases above,
	// without which a filter that ignored its argument would pass.
	if got := planBackfill(reg, "", "static_site"); len(got) != 0 {
		t.Fatalf("--subcategory static_site planned %d cell(s) for a registry with no such record", len(got))
	}
	if got := planBackfill(reg, "", "http_backend"); len(got) != len(unfiltered) {
		t.Fatalf("--subcategory http_backend planned %d cell(s), want all %d", len(got), len(unfiltered))
	}
}

// TestPlanBackfillOrderingIsTotalAndStable grades sortSeedPlans' claim
// that dry-run output and the resulting registry write are "byte-stable
// across runs" — which was prose only: making sortSeedPlans a no-op left
// the whole package green.
//
// Both halves are asserted, because they fail independently: a total
// order over (RecordID, Group, Key), and the same order on a repeated
// run over map-backed inputs.
func TestPlanBackfillOrderingIsTotalAndStable(t *testing.T) {
	reg := twoLanguageRegistry()
	got := planBackfill(reg, "", "")
	if len(got) < 2 {
		t.Fatalf("need at least two cells to have an order at all, got %d", len(got))
	}

	// Non-vacuity: the records are stored in reverse sorted order, so an
	// unsorted walk leads with the zig record.
	if reg.Records[0].ID <= reg.Records[1].ID {
		t.Fatal("fixture is no longer stored in reverse order; the ordering assertion below would hold with no sort at all")
	}
	if got[0].RecordID != reg.Records[1].ID {
		t.Fatalf("plan leads with %q, want the alphabetically-first record %q — the walk order was not sorted", got[0].RecordID, reg.Records[1].ID)
	}

	less := func(a, b seedPlan) bool {
		if a.RecordID != b.RecordID {
			return a.RecordID < b.RecordID
		}
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		return a.Key < b.Key
	}
	for i := 1; i < len(got); i++ {
		if !less(got[i-1], got[i]) {
			t.Fatalf("plan is not totally ordered by (RecordID, Group, Key) at %d: %+v then %+v", i, got[i-1], got[i])
		}
	}

	// Stability across runs. The presence set and the record's groups
	// are maps, so an unsorted plan is free to differ run to run; this
	// is the half that grades "across runs" rather than "sorted once".
	for run := 0; run < 8; run++ {
		again := planBackfill(twoLanguageRegistry(), "", "")
		if len(again) != len(got) {
			t.Fatalf("run %d planned %d cell(s), first run planned %d", run, len(again), len(got))
		}
		for i := range got {
			if again[i] != got[i] {
				t.Fatalf("run %d diverged at %d: %+v, first run had %+v", run, i, again[i], got[i])
			}
		}
	}
}

// TestBackfillAndValidateAgreeOnCompleteness is the pinned invariant: on
// every input, the cells `backfill` would seed and the completeness
// errors `validate` reports are the SAME SET — same records, same keys,
// same owning groups — and in particular one is empty exactly when the
// other is.
//
// Comparing sets rather than "both non-empty" is deliberate. Two guards
// that both fire, on different cells, is the drift this is here to
// catch, and a non-emptiness assertion cannot see it.
func TestBackfillAndValidateAgreeOnCompleteness(t *testing.T) {
	withDroppedCell := func(t *testing.T) *Registry {
		reg := groupedFixtureRegistry(t)
		delete(reg.Records[0].Groups["Routing"], "route_extraction")
		return reg
	}
	cases := []struct {
		name string
		reg  func(*testing.T) *Registry
		// wantMissing is whether this input is incomplete at all. It
		// keeps the agreement assertion from passing vacuously on a
		// predicate that reports nothing for every input.
		wantMissing bool
	}{
		{
			name:        "complete grouped record",
			reg:         groupedFixtureRegistry,
			wantMissing: false,
		},
		{
			name:        "grouped record missing one declared cell",
			reg:         withDroppedCell,
			wantMissing: true,
		},
		{
			// The historical divergence: backfill reported ~50 cells,
			// validate reported none, because validate's copy only ran
			// for rec.IsGrouped().
			name: "record with a taxonomy subcategory and no grouped cells",
			reg: func(t *testing.T) *Registry {
				return &Registry{SchemaVersion: SchemaVersion, Records: []Record{ungroupedRecord()}}
			},
			wantMissing: true,
		},
		{
			name: "record with no subcategory",
			reg: func(t *testing.T) *Registry {
				rec := ungroupedRecord()
				rec.Subcategory = ""
				rec.Capabilities = map[string]Capability{"endpoint_synthesis": {Status: StatusFull}}
				return &Registry{SchemaVersion: SchemaVersion, Records: []Record{rec}}
			},
			wantMissing: false,
		},
		{
			name: "subcategory with no group taxonomy",
			reg: func(t *testing.T) *Registry {
				rec := ungroupedRecord()
				rec.Subcategory = "static_site"
				rec.Capabilities = map[string]Capability{"build_extraction": {Status: StatusMissing, Issue: "x"}}
				if len(groupsForSubcategory("static_site")) != 0 {
					t.Fatal("static_site grew a group taxonomy; this case no longer covers the no-taxonomy branch")
				}
				return &Registry{SchemaVersion: SchemaVersion, Records: []Record{rec}}
			},
			wantMissing: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := tc.reg(t)
			plans := planBackfill(reg, "", "")
			got := completenessMessages(reg)
			want := wantCompletenessMessages(reg, plans)

			if (len(plans) > 0) != tc.wantMissing {
				t.Fatalf("backfill planned %d cell(s), want incomplete=%v", len(plans), tc.wantMissing)
			}
			if (len(got) > 0) != tc.wantMissing {
				t.Fatalf("validate reported %d completeness error(s), want incomplete=%v:\n%s",
					len(got), tc.wantMissing, strings.Join(got, "\n"))
			}
			if len(got) != len(want) {
				t.Fatalf("backfill would seed %d cell(s) but validate reported %d completeness error(s); the two guards have drifted",
					len(want), len(got))
			}
			sort.Strings(got)
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("guard disagreement at %d:\nbackfill implies: %s\nvalidate says:    %s", i, want[i], got[i])
				}
			}
		})
	}
}

// TestValidateFlagsTaxonomyRecordThatCarriesNoGroups pins the direction
// the collapse actually changed, on its own, so it cannot be lost inside
// the agreement test above.
//
// Before #6868 these records passed `validate` clean and failed only at
// `backfill --check`. Not a contrived shape: planBackfill's own doc
// comment names it ("a freshly `add`-ed record has an empty flat
// capabilities map and IsGrouped()==false").
//
// It runs over EVERY un-grouped shape rather than one, because
// "un-grouped" is three distinct values of rec.Capabilities and a
// re-gating predicate can readmit them one at a time. Two mutants,
// each an early return in validateGroupedCompleteness, are what these
// subcases exist to kill:
//
//	if len(rec.Capabilities) == 0 { return }                     // nil + empty
//	if !rec.IsGrouped() && len(rec.Capabilities) > 0 { return }  // flat
//
// The second was ALIVE until this test grew its third subcase: the
// flat-shaped record already had a test (subcategory_test.go's
// flat-shape-forbidden case) but nothing asserted its COMPLETENESS, so
// validate could stop reporting it silently.
func TestValidateFlagsTaxonomyRecordThatCarriesNoGroups(t *testing.T) {
	// The one lane asserted by name in every subcase, so no assertion
	// can be satisfied by an unrelated completeness message.
	const namedLane = `lane key "route_extraction" (group "Routing")`

	cases := []struct {
		name string
		caps map[string]Capability
		// absent is a declared lane the record DOES carry, and which
		// must therefore NOT be reported. Empty when the record carries
		// nothing.
		present string
	}{
		{
			// What `coverage add` leaves in memory.
			name: "nil capabilities",
			caps: nil,
		},
		{
			// What `"capabilities": {}` on disk loads as. Spelled with
			// the explicit literal so a census of this shape by grep
			// finds it — the nil case above does not match that search.
			name: "explicitly empty capabilities",
			caps: map[string]Capability{},
		},
		{
			// Flat shape under a taxonomy subcategory. Also an error for
			// a different reason (flat-shape-forbidden), which is why
			// its completeness went unobserved.
			name:    "non-empty flat capabilities",
			caps:    map[string]Capability{"endpoint_synthesis": {Status: StatusFull}},
			present: "endpoint_synthesis",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := ungroupedRecord()
			rec.Capabilities = tc.caps
			reg := &Registry{SchemaVersion: SchemaVersion, Records: []Record{rec}}
			if reg.Records[0].IsGrouped() {
				t.Fatal("the record under test must NOT be grouped; that is the whole point of it")
			}

			got := completenessMessages(reg)
			if len(got) == 0 {
				t.Fatal("validate reported no completeness message for a record whose subcategory taxonomy it satisfies almost none of")
			}
			found := false
			for _, e := range got {
				if strings.Contains(e, namedLane) {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected %s to be reported; got:\n%s", namedLane, strings.Join(got, "\n"))
			}

			// A cell the record DOES carry is not reported, whatever
			// shape carries it. Without this the subcase would pass on a
			// predicate that ignored the presence set entirely.
			if tc.present != "" {
				needle := `lane key "` + tc.present + `"`
				for _, e := range got {
					if strings.Contains(e, needle) {
						t.Fatalf("%q is present on the record but was reported missing:\n%s", tc.present, e)
					}
				}
			}

			// And the count still equals what backfill would seed, so
			// this cannot drift from the agreement invariant.
			if n := len(planBackfill(reg, "", "")); n != len(got) {
				t.Fatalf("backfill would seed %d cell(s), validate reported %d", n, len(got))
			}
		})
	}
}

// TestCompletenessGateSeverityIsHonoured is the concrete answer to the
// "why keep the backfill gate step at all" question (#6868 option 1).
//
// With completenessGateIsError true, validate reports completeness as an
// ERROR and the gate stops at step 1/5 — `backfill --check` is redundant.
// The constant is documented as a flippable severity knob, and while it
// is false those same messages are WARNINGS, which never fail CI. This
// test states that dependency in code: whichever side of the knob we are
// on, the messages exist, and their severity is what decides whether
// validate alone can fail an incomplete record.
func TestCompletenessGateSeverityIsHonoured(t *testing.T) {
	reg := &Registry{SchemaVersion: SchemaVersion, Records: []Record{ungroupedRecord()}}
	res := validateRegistry(reg, ".")
	count := func(msgs []string) int {
		n := 0
		for _, m := range msgs {
			if strings.Contains(m, completenessNeedle) {
				n++
			}
		}
		return n
	}
	inErrors, inWarnings := count(res.Errors), count(res.Warnings)
	if inErrors+inWarnings == 0 {
		t.Fatal("no completeness message on either channel; the severity knob governs nothing")
	}
	if completenessGateIsError {
		if inErrors == 0 || inWarnings != 0 {
			t.Fatalf("completenessGateIsError is true: want the messages as errors only, got %d error(s) / %d warning(s)", inErrors, inWarnings)
		}
		return
	}
	if inWarnings == 0 || inErrors != 0 {
		t.Fatalf("completenessGateIsError is false: want the messages as warnings only, got %d error(s) / %d warning(s)", inErrors, inWarnings)
	}
}

// TestCompletenessSeverityKnobDecidesWhoCatchesIt runs the knob-false
// world rather than reasoning about it, and is the reason the
// `backfill --check` gate step is KEPT even though `validate` subsumes
// it today.
//
// The step looks like dead weight only while completenessGateIsError is
// true. Flip it and the same messages become advisory warnings:
// res.HasErrors() is false, `validate` exits 0 on an incomplete record,
// and `backfill --check` is the single remaining gate step that fails on
// it. Deleting the step would therefore make the knob silently unsafe to
// flip — the flipper has no way to know the deletion was justified by
// the value they are changing.
func TestCompletenessSeverityKnobDecidesWhoCatchesIt(t *testing.T) {
	reg := &Registry{SchemaVersion: SchemaVersion, Records: []Record{ungroupedRecord()}}
	msgs := completenessMessages(reg)
	if len(msgs) == 0 {
		t.Fatal("no completeness messages to file; the rest of this test would be vacuous")
	}

	asError := &ValidationResult{}
	recordCompletenessMessages(asError, msgs, true)
	if !asError.HasErrors() {
		t.Fatal("with the knob true, completeness must block")
	}
	if len(asError.Warnings) != 0 {
		t.Fatalf("with the knob true, nothing belongs on the advisory channel: %v", asError.Warnings)
	}

	asWarning := &ValidationResult{}
	recordCompletenessMessages(asWarning, msgs, false)
	if asWarning.HasErrors() {
		t.Fatalf("with the knob false, validate must NOT fail an incomplete record: %v", asWarning.Errors)
	}
	if len(asWarning.Warnings) != len(msgs) {
		t.Fatalf("with the knob false, want %d advisory message(s), got %d", len(msgs), len(asWarning.Warnings))
	}

	// The consequence: with validate reduced to advice, `backfill
	// --check` is what still fails. Asserting the gate step's OUTCOME,
	// not merely that planBackfill returns rows.
	step := stepNamed(t, "Guard against incomplete grouped records (#2971)")
	root := t.TempDir()
	regPath := filepath.Join(root, filepath.FromSlash(defaultRegistryPath))
	if err := os.MkdirAll(filepath.Dir(regPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := saveRegistry(regPath, reg); err != nil {
		t.Fatalf("save: %v", err)
	}
	var out, errw bytes.Buffer
	env := &checkEnv{RepoRoot: root, RegistryPath: regPath, Out: &out, Err: &errw}
	if err := step.Run(env); err == nil {
		t.Fatalf("the backfill gate step passed an incomplete record; with the knob false NOTHING would catch it\nstdout:\n%s", out.String())
	}
	// Positive control: the same step is green once the record is
	// complete, so the failure above is the gap and not this tree.
	if _, _, err := runCmd(t, "backfill", "--file", regPath); err != nil {
		t.Fatalf("refill: %v", err)
	}
	if err := step.Run(env); err != nil {
		t.Fatalf("backfill step still failing after refill: %v", err)
	}
}
