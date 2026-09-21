package notifications

// healthscore_7287_test.go — an uncomputed health score must not reach a
// webhook consumer, or a person reading Slack/Discord, disguised as a measured
// number (#7287). QualitySnapshot.BugRate went to *float64 in #7271; the score
// derived from it stayed a bare float64 one line below, so an unmeasured
// rebuild emitted {"bug_rate":null,"health_score":100} and rendered the 100
// beside the word "not measured".
//
// Every render site gets its own row in both directions. A DEAD verdict on the
// Slack field says nothing about the Discord field or about either of the two
// summaryText lines that put the score in a message title.

import (
	"strings"
	"testing"
	"time"
)

func fptr7287(v float64) *float64 { return &v }

// unmeasuredSnap7287 is a snapshot with a real orphan rate and a real entity
// count but no bug rate and therefore no score — the shape rebuildQualitySnapshot
// produces for a rebuild that never measured an IMPORTS edge.
func unmeasuredSnap7287() QualitySnapshot {
	return QualitySnapshot{Group: "g1", OrphanRate: 12.5, TotalEntities: 100}
}

// measuredSnap7287 differs from unmeasuredSnap7287 on exactly the two fields
// under test, so every assertion below that separates them separates them on
// measuredness alone.
func measuredSnap7287() QualitySnapshot {
	return QualitySnapshot{
		Group: "g1", OrphanRate: 12.5, TotalEntities: 100,
		BugRate: fptr7287(7.25), HealthScore: fptr7287(88.0),
	}
}

// ────────────────────────────────────────────────────────────────────────────
// The wire
// ────────────────────────────────────────────────────────────────────────────

// TestWebhookPayload_UncomputedHealthScoreIsExplicitNull asserts the emitted
// bytes, not the struct: a receiver reads JSON. Both halves matter.
//
//   - null, not absent — `,omitempty` on the tag would drop health_score from
//     the body entirely, and a consumer defaulting a missing key to 0 lands
//     back on a number nobody computed. This is the same argument the BugRate
//     comment makes one line above it, and it is asserted here so the argument
//     is not merely written down.
//   - not a number — the original defect was literally `"health_score":100`.
func TestWebhookPayload_UncomputedHealthScoreIsExplicitNull(t *testing.T) {
	body, err := marshalPayload(FlavorGeneric, WebhookPayload{
		Event:     EventRebuildComplete,
		Timestamp: time.Now().UTC(),
		Quality:   unmeasuredSnap7287(),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, `"health_score":null`) {
		t.Errorf("payload does not carry an explicit null health_score:\n%s", got)
	}
	if strings.Contains(got, `"health_score":100`) || strings.Contains(got, `"health_score":0`) {
		t.Errorf("payload presents an uncomputed health score as a number:\n%s", got)
	}
	// The key must be present at all. Without this, `,omitempty` satisfies the
	// two assertions above by making the field vanish.
	if !strings.Contains(got, `"health_score"`) {
		t.Errorf("health_score key is absent from the body; a consumer cannot tell "+
			"an unmeasured score from an older grafel that never sent one:\n%s", got)
	}
	// Control: the orphan rate and group still serialise, so an empty or
	// truncated body cannot pass the rows above.
	if !strings.Contains(got, `"orphan_rate":12.5`) || !strings.Contains(got, `"group":"g1"`) {
		t.Fatalf("the rest of the snapshot did not serialise; the rows above prove nothing:\n%s", got)
	}
}

// TestWebhookPayload_MeasuredHealthScoreStillSerialises is the other direction:
// the fix must not null out a score that was computed.
func TestWebhookPayload_MeasuredHealthScoreStillSerialises(t *testing.T) {
	body, err := marshalPayload(FlavorGeneric, WebhookPayload{
		Event:     EventRebuildComplete,
		Timestamp: time.Now().UTC(),
		Quality:   measuredSnap7287(),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(body); !strings.Contains(got, `"health_score":88`) {
		t.Errorf("measured health_score missing from the body:\n%s", got)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// The four render sites
// ────────────────────────────────────────────────────────────────────────────

// renderSites7287 enumerates every place the score is shown to a person. The
// two summaryText entries are separate rows on purpose: they are separate
// switch arms with separate format strings, and the issue body listed only one
// of them.
var renderSites7287 = []struct {
	name  string
	build func(QualitySnapshot) (string, error)
}{
	{"slack", func(s QualitySnapshot) (string, error) {
		b, err := marshalSlack(WebhookPayload{Event: EventRebuildComplete, Timestamp: time.Now().UTC(), Quality: s})
		return string(b), err
	}},
	{"discord", func(s QualitySnapshot) (string, error) {
		b, err := marshalDiscord(WebhookPayload{Event: EventRebuildComplete, Timestamp: time.Now().UTC(), Quality: s})
		return string(b), err
	}},
	{"summary/rebuild_complete", func(s QualitySnapshot) (string, error) {
		return summaryText(WebhookPayload{Event: EventRebuildComplete, Quality: s}), nil
	}},
	{"summary/quality_regressed", func(s QualitySnapshot) (string, error) {
		return summaryText(WebhookPayload{Event: EventQualityRegressed, Quality: s}), nil
	}},
}

// TestRenderSites_UncomputedHealthScoreSaysNotMeasured is the forbidden row at
// every site: no digits where the score goes.
//
// The snapshot deliberately carries OrphanRate 12.5 and TotalEntities 100, so
// "there are no digits anywhere in the output" is not what is being asserted —
// the assertion is scoped to the score's own rendering.
func TestRenderSites_UncomputedHealthScoreSaysNotMeasured(t *testing.T) {
	for _, site := range renderSites7287 {
		t.Run(site.name, func(t *testing.T) {
			got, err := site.build(unmeasuredSnap7287())
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if !strings.Contains(got, "not measured") {
				t.Errorf("%s does not say the health score is unmeasured:\n%s", site.name, got)
			}
			// The exact shapes the defect produced. ComputeHealthScore returns
			// 100 for a zeroed bug rate and a zeroed orphan rate; 87.5 is what
			// it returns for this snapshot's orphan rate with the bug rate read
			// as 0, which is the number the shipped code actually printed here.
			for _, forbidden := range []string{"health 100.0", "health 87.5", "health 0.0", `"100.0"`, `"87.5"`, `"0.0"`} {
				if strings.Contains(got, forbidden) {
					t.Errorf("%s renders an uncomputed health score as %q:\n%s", site.name, forbidden, got)
				}
			}
		})
	}
}

// TestRenderSites_UncomputedHealthScore_PlantedViolationFires proves the
// forbidden rows above are reachable rather than vacuously green. It feeds the
// same sites the value the shipped code fed them — the number ComputeHealthScore
// returns when the bug rate is read as 0 — and requires every site to trip.
func TestRenderSites_UncomputedHealthScore_PlantedViolationFires(t *testing.T) {
	planted := unmeasuredSnap7287()
	planted.HealthScore = fptr7287(87.5) // what the pre-#7287 code passed through

	for _, site := range renderSites7287 {
		t.Run(site.name, func(t *testing.T) {
			got, err := site.build(planted)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			tripped := strings.Contains(got, "health 87.5") || strings.Contains(got, `"87.5"`)
			if !tripped {
				t.Errorf("planted violation did not appear at %s, so that site's "+
					"forbidden row is unreachable and grades nothing:\n%s", site.name, got)
			}
		})
	}
}

// TestRenderSites_MeasuredHealthScoreStillRenders is the opposite direction at
// every site: the unknown branch must not swallow a real score.
func TestRenderSites_MeasuredHealthScoreStillRenders(t *testing.T) {
	for _, site := range renderSites7287 {
		t.Run(site.name, func(t *testing.T) {
			got, err := site.build(measuredSnap7287())
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if !strings.Contains(got, "88.0") {
				t.Errorf("%s lost a measured health score:\n%s", site.name, got)
			}
			if strings.Contains(got, "not measured") {
				t.Errorf("%s calls a measured health score unmeasured:\n%s", site.name, got)
			}
		})
	}
}

// TestSummaryText_KeepsTheHealthParenthetical pins the title wording choice:
// the score's slot stays in the title and is filled with words, rather than the
// parenthetical being dropped. A title that silently loses "(health …)" reads
// as a message format change, not as a measurement that did not happen, and a
// reader comparing two notifications cannot tell which they are looking at.
func TestSummaryText_KeepsTheHealthParenthetical(t *testing.T) {
	for _, ev := range []EventType{EventRebuildComplete, EventQualityRegressed} {
		got := summaryText(WebhookPayload{Event: ev, Quality: unmeasuredSnap7287()})
		if !strings.Contains(got, "(health not measured)") {
			t.Errorf("summaryText(%s) = %q, want the health parenthetical filled with words", ev, got)
		}
		if !strings.Contains(got, "g1") {
			t.Errorf("summaryText(%s) = %q, lost the group name (control)", ev, got)
		}
	}
}

// TestFormatOptionalScore covers the helper directly, including the fact that
// it reuses formatOptionalPct's wording. Two vocabularies for one idea would
// let a Slack attachment say "not measured" for the bug rate and something
// else for the score in the same four fields.
func TestFormatOptionalScore(t *testing.T) {
	if got := formatOptionalScore(nil); got != "not measured" {
		t.Errorf("formatOptionalScore(nil) = %q, want %q", got, "not measured")
	}
	if got, want := formatOptionalScore(nil), formatOptionalPct(nil); got != want {
		t.Errorf("score and percentage disagree on the unknown wording: %q vs %q", got, want)
	}
	if got := formatOptionalScore(fptr7287(88)); got != "88.0" {
		t.Errorf("formatOptionalScore(88) = %q, want %q", got, "88.0")
	}
	if got := formatOptionalScore(fptr7287(0)); got != "0.0" {
		t.Errorf("formatOptionalScore(0) = %q, want %q — a measured zero is a real score", got, "0.0")
	}
}

// TestSlackAndDiscord_BothFieldsAgreeWhenNothingWasMeasured is the symptom the
// issue captured, asserted as a symptom: the contradiction was a "100.0" and a
// "not measured" sitting in the same attachment. Counting occurrences catches
// a fix that changes only one of the two fields — a whole-output grep for
// "not measured" would already be satisfied by the bug rate alone.
func TestSlackAndDiscord_BothFieldsAgreeWhenNothingWasMeasured(t *testing.T) {
	for _, tc := range []struct {
		flavor string
		build  func(WebhookPayload) ([]byte, error)
	}{
		{"slack", marshalSlack},
		{"discord", marshalDiscord},
	} {
		body, err := tc.build(WebhookPayload{
			Event:     EventRebuildComplete,
			Timestamp: time.Now().UTC(),
			Quality:   unmeasuredSnap7287(),
		})
		if err != nil {
			t.Fatalf("%s: marshal: %v", tc.flavor, err)
		}
		got := string(body)
		// Three: the Health Score field, the Bug Rate field, and the summary
		// text the flavour embeds as its title.
		if n := strings.Count(got, "not measured"); n != 3 {
			t.Errorf("%s: %d occurrences of %q, want 3 (health field, bug-rate field, title):\n%s",
				tc.flavor, n, "not measured", got)
		}
	}
}

// TestRegressionDetected_IgnoresTheHealthScore pins the claim previousSnapshot's
// comment makes about this function: the score is carried across but compares
// nothing. Without this row, "RegressionDetected does NOT compare health scores"
// is a sentence no test observes — and if it ever starts comparing them, the
// unmeasured side has to be handled first.
func TestRegressionDetected_IgnoresTheHealthScore(t *testing.T) {
	base := QualitySnapshot{Group: "g1", OrphanRate: 5, BugRate: fptr7287(2), HealthScore: fptr7287(93)}
	worse := base
	worse.HealthScore = fptr7287(12) // a catastrophic drop, and nothing else changed

	if RegressionDetected(base, worse) {
		t.Error("RegressionDetected fired on a health-score change alone; previousSnapshot's " +
			"comment says it compares orphan rate, bug rate, secrets and cycles only")
	}

	// Positive control: the metrics it does compare still fire, so the row
	// above is not a RegressionDetected that never returns true.
	bugWorse := base
	bugWorse.BugRate = fptr7287(40)
	if !RegressionDetected(base, bugWorse) {
		t.Error("control: a 2% -> 40% bug rate must be a regression")
	}
}
