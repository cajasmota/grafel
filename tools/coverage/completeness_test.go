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
// lets these tests count completeness errors alone on registries that
// also trip other validation rules.
const completenessNeedle = "taxonomy but absent from record"

// completenessErrors returns the completeness messages validateRegistry
// produced, and nothing else.
func completenessErrors(reg *Registry) []string {
	res := validateRegistry(reg, ".")
	var out []string
	for _, e := range res.Errors {
		if strings.Contains(e, completenessNeedle) {
			out = append(out, e)
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
			got := completenessErrors(reg)
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
// Before #6868 this record passed `validate` clean and failed only at
// `backfill --check`. It is not a contrived shape: planBackfill's own
// doc comment names it ("a freshly `add`-ed record has an empty flat
// capabilities map and IsGrouped()==false"). Re-gating validate's
// completeness call on rec.IsGrouped() makes this test red.
func TestValidateFlagsTaxonomyRecordThatCarriesNoGroups(t *testing.T) {
	reg := &Registry{SchemaVersion: SchemaVersion, Records: []Record{ungroupedRecord()}}
	if reg.Records[0].IsGrouped() {
		t.Fatal("the record under test must NOT be grouped; that is the whole point of it")
	}
	got := completenessErrors(reg)
	if len(got) == 0 {
		t.Fatal("validate reported no completeness error for a record whose subcategory taxonomy it satisfies none of")
	}
	// Name one specific declared lane, so the assertion cannot be
	// satisfied by an unrelated completeness message.
	wantKey := `lane key "route_extraction" (group "Routing")`
	found := false
	for _, e := range got {
		if strings.Contains(e, wantKey) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %s to be reported; got:\n%s", wantKey, strings.Join(got, "\n"))
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
	msgs := completenessErrors(reg)
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
