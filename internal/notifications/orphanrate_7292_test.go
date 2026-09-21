package notifications

// orphanrate_7292_test.go — QualitySnapshot.OrphanRate and .TotalEntities were
// a bare float64 and a bare int, so a payload that measured neither (the
// dashboard's test ping) shipped orphan_rate:0 / total_entities:0 and rendered
// "Orphan Rate: 0.00%" — a perfect score nobody computed, printed beside the
// "not measured" that #7271 and #7287 had already won for bug rate and health
// score. Both are pointers now, with NO omitempty, so the wire carries an
// explicit null.
//
// Every consumer of the two fields gets its own row in BOTH directions. The
// consumer set was derived by compiling against the changed type, not by hand:
// marshalSlack, marshalDiscord, CheckBudgets, RegressionDetected, and the
// generic json.Marshal. summaryText reads neither field — the row below pins
// that, so a future orphan rate added to a title cannot arrive unguarded.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func iptr7292(v int) *int { return &v }

// measuredSnap7292 and unmeasuredSnap7292 differ on exactly the two fields
// under test, so every assertion separating them separates them on
// measuredness alone.
func measuredSnap7292() QualitySnapshot {
	return QualitySnapshot{
		Group: "g1", OrphanRate: fptr7287(12.5), TotalEntities: iptr7292(100),
		BugRate: fptr7287(7.25), HealthScore: fptr7287(88.0),
	}
}

func unmeasuredSnap7292() QualitySnapshot {
	return QualitySnapshot{
		Group: "g1", OrphanRate: nil, TotalEntities: nil,
		BugRate: fptr7287(7.25), HealthScore: fptr7287(88.0),
	}
}

// ────────────────────────────────────────────────────────────────────────────
// The wire
// ────────────────────────────────────────────────────────────────────────────

// wireProblems7292 lists the ways a generic payload body breaks the contract.
// It is a function rather than inline assertions so that
// TestWireProblems7292_FiresOnAPlantedViolation can prove the absence rows
// below are capable of failing.
func wireProblems7292(body string) []string {
	var probs []string
	if !strings.Contains(body, `"orphan_rate":null`) {
		probs = append(probs, "no explicit orphan_rate:null")
	}
	if strings.Contains(body, `"orphan_rate":0`) {
		probs = append(probs, "fabricated orphan_rate:0")
	}
	if !strings.Contains(body, `"total_entities":null`) {
		probs = append(probs, "no explicit total_entities:null")
	}
	if strings.Contains(body, `"total_entities":0`) {
		probs = append(probs, "fabricated total_entities:0")
	}
	return probs
}

func TestWireProblems7292_FiresOnAPlantedViolation(t *testing.T) {
	// The exact body shape this issue was filed about.
	planted := `{"quality":{"group":"test","orphan_rate":0,"bug_rate":null,"health_score":null,"total_entities":0}}`
	probs := wireProblems7292(planted)
	want := map[string]bool{
		"no explicit orphan_rate:null":    true,
		"fabricated orphan_rate:0":        true,
		"no explicit total_entities:null": true,
		"fabricated total_entities:0":     true,
	}
	if len(probs) != len(want) {
		t.Fatalf("planted violation reported %d problems, want %d: %v", len(probs), len(want), probs)
	}
	for _, p := range probs {
		if !want[p] {
			t.Errorf("unexpected problem %q", p)
		}
	}
}

// TestWire7292_UnmeasuredIsExplicitNull is the forbidden row on the wire. A
// receiver cannot read grafel's source, so the key has to be present and null
// rather than absent (which omitempty would do) or zero.
func TestWire7292_UnmeasuredIsExplicitNull(t *testing.T) {
	body, err := json.Marshal(WebhookPayload{
		Event: "ping", Timestamp: time.Unix(0, 0).UTC(), Quality: unmeasuredSnap7292(),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(body)
	if probs := wireProblems7292(got); len(probs) > 0 {
		t.Errorf("unmeasured payload: %v\n%s", probs, got)
	}
	// omitempty cannot satisfy the rows above by making the keys vanish.
	if !strings.Contains(got, `"orphan_rate"`) || !strings.Contains(got, `"total_entities"`) {
		t.Errorf("a key vanished from the wire:\n%s", got)
	}
}

// TestWire7292_MeasuredValuesSurvive is the other direction: the pointer must
// not have turned a real measurement into a null.
func TestWire7292_MeasuredValuesSurvive(t *testing.T) {
	body, err := json.Marshal(WebhookPayload{
		Event: EventRebuildComplete, Timestamp: time.Unix(0, 0).UTC(), Quality: measuredSnap7292(),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round struct {
		Quality QualitySnapshot `json:"quality"`
	}
	if err := json.Unmarshal(body, &round); err != nil {
		t.Fatalf("round-trip: %v\n%s", err, body)
	}
	if round.Quality.OrphanRate == nil || *round.Quality.OrphanRate != 12.5 {
		t.Errorf("orphan_rate did not survive the wire: %v\n%s", round.Quality.OrphanRate, body)
	}
	if round.Quality.TotalEntities == nil || *round.Quality.TotalEntities != 100 {
		t.Errorf("total_entities did not survive the wire: %v\n%s", round.Quality.TotalEntities, body)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// The renderers — one row per site, both directions
// ────────────────────────────────────────────────────────────────────────────

// renderedField7292 pulls one rendered field value out of a Slack or Discord
// body. It fails the test when the field is missing rather than returning "",
// so a renderer that dropped the field cannot read as a pass. Parameterised by
// title because "Orphan Rate" and "Bug Rate" share formatOptionalPct: a verdict
// on one says nothing about the other.
func renderedField7292(t *testing.T, flavor WebhookFlavor, title string, snap QualitySnapshot) string {
	t.Helper()
	body, err := marshalPayload(flavor, WebhookPayload{
		Event: EventRebuildComplete, Timestamp: time.Unix(0, 0).UTC(), Quality: snap,
	})
	if err != nil {
		t.Fatalf("%s marshal: %v", flavor, err)
	}
	type field struct {
		Title string `json:"title"`
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	var decoded struct {
		Attachments []struct {
			Fields []field `json:"fields"`
		} `json:"attachments"`
		Embeds []struct {
			Fields []field `json:"fields"`
		} `json:"embeds"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("%s decode: %v\n%s", flavor, err, body)
	}
	var fields []field
	switch {
	case len(decoded.Attachments) == 1:
		fields = decoded.Attachments[0].Fields
	case len(decoded.Embeds) == 1:
		fields = decoded.Embeds[0].Fields
	default:
		t.Fatalf("%s: expected exactly one attachment/embed:\n%s", flavor, body)
	}
	for _, f := range fields {
		if f.Title == title || f.Name == title {
			return f.Value
		}
	}
	t.Fatalf("%s: no %s field; any assertion on it would grade nothing:\n%s", flavor, title, body)
	return ""
}

func orphanFieldValue7292(t *testing.T, flavor WebhookFlavor, snap QualitySnapshot) string {
	t.Helper()
	return renderedField7292(t, flavor, "Orphan Rate", snap)
}

func TestRenderers7292_OrphanRate(t *testing.T) {
	for _, flavor := range []WebhookFlavor{FlavorSlack, FlavorDiscord} {
		// Unmeasured: words, not a number. Exact equality, because
		// "not measured" is also a substring of nothing else and a
		// Contains("12.5") would be satisfied by "12.50%" either way.
		if got := orphanFieldValue7292(t, flavor, unmeasuredSnap7292()); got != "not measured" {
			t.Errorf("%s unmeasured Orphan Rate = %q, want %q", flavor, got, "not measured")
		}
		// Measured: the percentage, to two places. This exact string is what
		// separates formatOptionalPct from formatOptionalScore, which would
		// render "12.5" here and pass any looser assertion.
		if got := orphanFieldValue7292(t, flavor, measuredSnap7292()); got != "12.50%" {
			t.Errorf("%s measured Orphan Rate = %q, want %q", flavor, got, "12.50%")
		}
	}
}

// TestRenderers7292_MeasuredZeroIsNotNotMeasured is #7292 run backwards, and
// the direction the fix left ungraded: a rate that WAS measured and came out at
// zero has to print as "0.00%", not as "not measured". rebuild_summary.go
// computes 100*orphans/total, so a graph with no orphaned entity and at least
// one entity yields exactly 0 — the reachability argument is arithmetic; no
// end-to-end run is claimed here.
//
// Orphan rate and bug rate are a pair: both are *float64 rendered by the one
// formatOptionalPct, so a DEAD verdict on either says nothing about the other,
// and a nil-or-zero collapse inside that helper would break both at once.
func TestRenderers7292_MeasuredZeroIsNotNotMeasured(t *testing.T) {
	zero := QualitySnapshot{
		Group: "g1", OrphanRate: fptr7287(0), TotalEntities: iptr7292(40),
		BugRate: fptr7287(0), HealthScore: fptr7287(100),
	}
	for _, flavor := range []WebhookFlavor{FlavorSlack, FlavorDiscord} {
		for _, title := range []string{"Orphan Rate", "Bug Rate"} {
			if got := renderedField7292(t, flavor, title, zero); got != "0.00%" {
				t.Errorf("%s measured-zero %s = %q, want %q", flavor, title, got, "0.00%")
			}
		}
	}
}

// TestFormatOptionalPct7292_ZeroAndNilAreDifferentStrings is the same contract
// at the helper both fields share, one layer below the renderers. Stated as a
// difference rather than two independent values so it cannot pass while the two
// inputs have collapsed onto one output.
func TestFormatOptionalPct7292_ZeroAndNilAreDifferentStrings(t *testing.T) {
	measuredZero := formatOptionalPct(fptr7287(0))
	unmeasured := formatOptionalPct(nil)
	if measuredZero == unmeasured {
		t.Fatalf("a measured 0 and an unmeasured value render identically as %q", measuredZero)
	}
	if measuredZero != "0.00%" {
		t.Errorf("formatOptionalPct(0) = %q, want %q", measuredZero, "0.00%")
	}
	if unmeasured != "not measured" {
		t.Errorf("formatOptionalPct(nil) = %q, want %q", unmeasured, "not measured")
	}
}

// TestSummaryText7292_DoesNotCarryAnOrphanRate pins the claim that summaryText
// is not a consumer of either field — the reason neither has a summary row
// above. If a title ever starts printing an orphan rate, this fails and the
// unmeasured case has to be handled before it can pass again.
func TestSummaryText7292_DoesNotCarryAnOrphanRate(t *testing.T) {
	for _, event := range []EventType{
		EventRebuildComplete, EventQualityRegressed, EventBudgetExceeded, EventSecretFound, "ping",
	} {
		measured := summaryText(WebhookPayload{Event: event, Quality: measuredSnap7292()})
		unmeasured := summaryText(WebhookPayload{Event: event, Quality: unmeasuredSnap7292()})
		if measured != unmeasured {
			t.Errorf("summaryText(%s) changed with the orphan rate/entity count:\n measured: %q\n unmeasured: %q",
				event, measured, unmeasured)
		}
		if strings.Contains(measured, "12.50%") || strings.Contains(measured, "100") {
			t.Errorf("summaryText(%s) printed an orphan rate or entity count: %q", event, measured)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// CheckBudgets — a guard that never fires is as wrong as one that always does
// ────────────────────────────────────────────────────────────────────────────

func violation7292(t *testing.T, metric string, snap QualitySnapshot, budgets QualityBudgets) *BudgetViolation {
	t.Helper()
	for _, v := range CheckBudgets(snap, budgets) {
		if v.Metric == metric {
			return &v
		}
	}
	return nil
}

func orphanViolation7292(t *testing.T, snap QualitySnapshot, budgets QualityBudgets) *BudgetViolation {
	t.Helper()
	return violation7292(t, "orphan_rate", snap, budgets)
}

// TestCheckBudgets7292_UnmeasuredOrphanRateBreachesNothing is a NIL-SAFETY row,
// not a behaviour pin, and is labelled so deliberately. Before #7292 a nil read
// as a bare 0 also breached nothing (0 > any positive threshold is false), so
// this outcome is unchanged by the fix and a mutant that reads nil as 0 here
// passes identically. What it does grade is that the arm neither panics nor
// invents a violation for a snapshot that measured nothing. The arm's real
// decision is graded by the two rows below it.
func TestCheckBudgets7292_UnmeasuredOrphanRateBreachesNothing(t *testing.T) {
	budgets := QualityBudgets{MaxOrphanRate: 10}
	if v := orphanViolation7292(t, unmeasuredSnap7292(), budgets); v != nil {
		t.Errorf("an unmeasured orphan rate breached a budget: %+v", *v)
	}
}

func TestCheckBudgets7292_MeasuredOrphanRateOverThresholdStillBreaches(t *testing.T) {
	// 12.5 > 10. Without this row the nil guard above could be a guard that
	// never fires, which silences the alarm the issue is about, one field over.
	budgets := QualityBudgets{MaxOrphanRate: 10}
	v := orphanViolation7292(t, measuredSnap7292(), budgets)
	if v == nil {
		t.Fatal("a measured 12.5% orphan rate did not breach a 10% budget")
	}
	if v.Threshold != 10 || v.Actual != 12.5 {
		t.Errorf("violation = %+v, want threshold 10 actual 12.5", *v)
	}
}

func TestCheckBudgets7292_MeasuredOrphanRateUnderThresholdDoesNotBreach(t *testing.T) {
	// The bound on the row above: 12.5 < 20, so it is the comparison that
	// decides, not the mere presence of a measurement.
	if v := orphanViolation7292(t, measuredSnap7292(), QualityBudgets{MaxOrphanRate: 20}); v != nil {
		t.Errorf("a 12.5%% orphan rate breached a 20%% budget: %+v", *v)
	}
}

// TestCheckBudgets7292_OnTheBoundaryDoesNotBreach sits a rate exactly ON its
// threshold. Neither 12.5-vs-10 nor 12.5-vs-20 above distinguishes > from >=,
// so without this row the comparison operator is ungraded. Both percentage arms
// get a row: the bug-rate arm one line down in CheckBudgets has the identical
// gap, and a verdict on the orphan arm says nothing about it.
func TestCheckBudgets7292_OnTheBoundaryDoesNotBreach(t *testing.T) {
	onBoundary := QualitySnapshot{
		Group: "g1", OrphanRate: fptr7287(10), BugRate: fptr7287(4),
		TotalEntities: iptr7292(100),
	}
	budgets := QualityBudgets{MaxOrphanRate: 10, MaxBugRate: 4}

	if v := violation7292(t, "orphan_rate", onBoundary, budgets); v != nil {
		t.Errorf("an orphan rate exactly ON its budget breached it: %+v", *v)
	}
	if v := violation7292(t, "bug_rate", onBoundary, budgets); v != nil {
		t.Errorf("a bug rate exactly ON its budget breached it: %+v", *v)
	}

	// Positive control on the same fixture: one step over each threshold must
	// breach, so the two rows above bound the operator from both sides rather
	// than recording an arm that never fires.
	over := onBoundary
	over.OrphanRate = fptr7287(10.01)
	over.BugRate = fptr7287(4.01)
	if v := violation7292(t, "orphan_rate", over, budgets); v == nil {
		t.Error("control: 10.01% over a 10% orphan budget must breach")
	}
	if v := violation7292(t, "bug_rate", over, budgets); v == nil {
		t.Error("control: 4.01% over a 4% bug budget must breach")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// RegressionDetected
// ────────────────────────────────────────────────────────────────────────────

func TestRegressionDetected7292_NeedsTwoMeasuredOrphanRates(t *testing.T) {
	good := QualitySnapshot{Group: "g1", OrphanRate: fptr7287(5)}
	bad := QualitySnapshot{Group: "g1", OrphanRate: fptr7287(40)}
	none := QualitySnapshot{Group: "g1"}

	if RegressionDetected(none, bad) {
		t.Error("an unmeasured previous orphan rate was read as a flawless baseline")
	}
	// NIL-SAFETY ONLY, and labelled so rather than left to read as a decision
	// guard: this row passes with or without the curr.OrphanRate != nil clause,
	// so it grades that the arm does not panic on a half-measured pair, not the
	// clause itself. That is a fact about the caller, not an equivalence — a
	// negative prev, which the history file is never range-checked for, does
	// separate the two forms. No fixture here, deliberately: one would pin the
	// reachability of a snapshot the only production call site cannot build.
	// The prev side is what changes an outcome, and the row above grades it.
	if RegressionDetected(good, none) {
		t.Error("an unmeasured current orphan rate made the comparison panic or fire")
	}
	// Positive control: the same current snapshot IS a regression once the
	// previous side is measured, so the two rows above are real skips rather
	// than a RegressionDetected that never fires on this field.
	if !RegressionDetected(good, bad) {
		t.Error("control: a measured 5% → 40% orphan rate must be a regression")
	}
}

// TestRegressionDetected7292_EpsilonIsBoundedFromAbove pins the noise threshold
// from the side nothing was holding. The existing noise row only shows eps is
// above 0.1; without an upper bound, eps could be widened far enough to stop
// reporting real regressions and every test would stay green.
func TestRegressionDetected7292_EpsilonIsBoundedFromAbove(t *testing.T) {
	base := QualitySnapshot{Group: "g1", OrphanRate: fptr7287(5)}

	// 1.0 is comfortably above the 0.5 the code documents, and far below the
	// ~36.5 that the next-widest assertion in this package would still allow.
	worse := QualitySnapshot{Group: "g1", OrphanRate: fptr7287(6.0)}
	if !RegressionDetected(base, worse) {
		t.Error("a 5% → 6% orphan rate was written off as noise; eps is too wide")
	}
	// The lower bound on the same fixture, so this row cannot pass by eps
	// having collapsed to zero and every drift reading as a regression.
	quiet := QualitySnapshot{Group: "g1", OrphanRate: fptr7287(5.1)}
	if RegressionDetected(base, quiet) {
		t.Error("a 0.1% drift was reported as a regression; eps is too narrow")
	}
}
