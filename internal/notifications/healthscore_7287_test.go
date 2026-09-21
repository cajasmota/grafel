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
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

func fptr7287(v float64) *float64 { return &v }

// unmeasuredSnap7287 is a snapshot with a real orphan rate and a real entity
// count but no bug rate and therefore no score — the shape rebuildQualitySnapshot
// produces for a rebuild that never measured an IMPORTS edge.
func unmeasuredSnap7287() QualitySnapshot {
	return QualitySnapshot{Group: "g1", OrphanRate: fptr7287(12.5), TotalEntities: iptr7292(100)}
}

// measuredSnap7287 differs from unmeasuredSnap7287 on exactly the two fields
// under test, so every assertion below that separates them separates them on
// measuredness alone.
func measuredSnap7287() QualitySnapshot {
	return QualitySnapshot{
		Group: "g1", OrphanRate: fptr7287(12.5), TotalEntities: iptr7292(100),
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
	// score returns the health score's OWN rendering at this site, extracted
	// from the site's real output. Every assertion below compares against this
	// rather than grepping the whole body, because a substring check on the
	// whole body is satisfied by a superstring: "88.00%" contains "88.0", so a
	// Contains(got, "88.0") row stays green when a site calls the wrong helper.
	// formatOptionalPct sits twelve lines from formatOptionalScore, takes the
	// same *float64, returns the same string for nil, and is named two lines
	// below in the same fields slice — so that swap compiles, and the score
	// ships as a percentage. Extracting the field pins its shape instead of
	// blacklisting one wrong spelling of it.
	score func(string) (string, error)
}{
	{"slack", func(s QualitySnapshot) (string, error) {
		b, err := marshalSlack(WebhookPayload{Event: EventRebuildComplete, Timestamp: time.Now().UTC(), Quality: s})
		return string(b), err
	}, func(body string) (string, error) {
		var p struct {
			Attachments []struct {
				Fields []struct {
					Title string `json:"title"`
					Value string `json:"value"`
				} `json:"fields"`
			} `json:"attachments"`
		}
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			return "", err
		}
		if len(p.Attachments) != 1 {
			return "", fmt.Errorf("want 1 attachment, got %d", len(p.Attachments))
		}
		var found []string
		for _, f := range p.Attachments[0].Fields {
			if f.Title == "Health Score" {
				found = append(found, f.Value)
			}
		}
		if len(found) != 1 {
			return "", fmt.Errorf("want exactly 1 \"Health Score\" field, got %d", len(found))
		}
		return found[0], nil
	}},
	{"discord", func(s QualitySnapshot) (string, error) {
		b, err := marshalDiscord(WebhookPayload{Event: EventRebuildComplete, Timestamp: time.Now().UTC(), Quality: s})
		return string(b), err
	}, func(body string) (string, error) {
		var p struct {
			Embeds []struct {
				Fields []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"fields"`
			} `json:"embeds"`
		}
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			return "", err
		}
		if len(p.Embeds) != 1 {
			return "", fmt.Errorf("want 1 embed, got %d", len(p.Embeds))
		}
		var found []string
		for _, f := range p.Embeds[0].Fields {
			if f.Name == "Health Score" {
				found = append(found, f.Value)
			}
		}
		if len(found) != 1 {
			return "", fmt.Errorf("want exactly 1 \"Health Score\" field, got %d", len(found))
		}
		return found[0], nil
	}},
	{"summary/rebuild_complete", func(s QualitySnapshot) (string, error) {
		return summaryText(WebhookPayload{Event: EventRebuildComplete, Quality: s}), nil
	}, healthParenthetical7287},
	{"summary/quality_regressed", func(s QualitySnapshot) (string, error) {
		return summaryText(WebhookPayload{Event: EventQualityRegressed, Quality: s}), nil
	}, healthParenthetical7287},
}

// healthParenthetical7287 extracts what a summary title puts in its "(health …)"
// slot. Anchored to the end of the title so it captures the whole slot: a
// capture that stopped at the first non-digit would itself be satisfied by
// "88.00%".
var healthSlotRe7287 = regexp.MustCompile(`\(health (.+)\)$`)

func healthParenthetical7287(title string) (string, error) {
	m := healthSlotRe7287.FindStringSubmatch(title)
	if m == nil {
		return "", fmt.Errorf("no \"(health …)\" slot in %q", title)
	}
	return m[1], nil
}

// scoreAt runs a site and returns the health score's own rendering, failing the
// test if the extraction itself did not work. Without this, an extractor that
// silently returned "" would make every equality row below compare "" to "" and
// grade nothing.
func scoreAt(t *testing.T, i int, snap QualitySnapshot) string {
	t.Helper()
	site := renderSites7287[i]
	body, err := site.build(snap)
	if err != nil {
		t.Fatalf("%s: build: %v", site.name, err)
	}
	got, err := site.score(body)
	if err != nil {
		t.Fatalf("%s: could not extract the health score from the output, so the "+
			"assertion would grade nothing: %v\n%s", site.name, err, body)
	}
	if got == "" {
		t.Fatalf("%s: extracted an empty health score from:\n%s", site.name, body)
	}
	return got
}

// TestRenderSites_UncomputedHealthScoreSaysNotMeasured is the forbidden row at
// every site, asserted as an exact equality on the score's own rendering rather
// than as a grep over the whole body. The snapshot deliberately carries
// OrphanRate 12.5 and TotalEntities 100, so the rows below are scoped to the
// score's slot and not to "there are no digits anywhere".
func TestRenderSites_UncomputedHealthScoreSaysNotMeasured(t *testing.T) {
	for i, site := range renderSites7287 {
		t.Run(site.name, func(t *testing.T) {
			if got := scoreAt(t, i, unmeasuredSnap7287()); got != "not measured" {
				t.Errorf("%s renders an uncomputed health score as %q, want %q",
					site.name, got, "not measured")
			}
		})
	}
}

// TestRenderSites_UncomputedHealthScore_PlantedViolationFires proves the rows
// above are reachable rather than vacuously green, and proves each extractor is
// reading the score's slot and not some constant. It feeds every site the value
// the shipped code fed them — what ComputeHealthScore returns when the bug rate
// is read as 0 — and requires each site to surface it.
func TestRenderSites_UncomputedHealthScore_PlantedViolationFires(t *testing.T) {
	planted := unmeasuredSnap7287()
	planted.HealthScore = fptr7287(87.5) // what the pre-#7287 code passed through

	for i, site := range renderSites7287 {
		t.Run(site.name, func(t *testing.T) {
			got := scoreAt(t, i, planted)
			if got != "87.5" {
				t.Errorf("planted violation did not reach %s's health-score slot "+
					"(extracted %q), so that site's forbidden row grades nothing", site.name, got)
			}
		})
	}
}

// TestRenderSites_MeasuredHealthScoreStillRenders is the opposite direction at
// every site. The equality is exact and it is what closes the helper-confusion
// hole: a Contains(body, "88.0") row here passed with every site swapped to
// formatOptionalPct, which renders a 0–100 composite score as "88.00%".
func TestRenderSites_MeasuredHealthScoreStillRenders(t *testing.T) {
	for i, site := range renderSites7287 {
		t.Run(site.name, func(t *testing.T) {
			got := scoreAt(t, i, measuredSnap7287())
			if got != "88.0" {
				t.Errorf("%s renders a measured health score as %q, want %q — a 0–100 "+
					"composite score is not a percentage, and formatOptionalPct compiles here",
					site.name, got, "88.0")
			}
		})
	}
}

// TestRenderSites_ScoreIsNotAPercentage states the same property as a class
// rather than as one value: the equality row above says what the rendering IS,
// this says what it may never be.
//
// It is strictly subsumed by that row today, and the distinction is worth
// stating plainly rather than dressing up: a rendering containing "%" cannot
// equal "88.0", so the equality row has already failed by the time this one
// looks, this row can never fail alone, and any mutant it kills is a duplicate
// kill. Its value is entirely prospective — it is the row that still rejects a
// percentage if someone later loosens the expected values — not extra grading
// of anything the equality row leaves open.
func TestRenderSites_ScoreIsNotAPercentage(t *testing.T) {
	for _, snap := range []QualitySnapshot{measuredSnap7287(), unmeasuredSnap7287()} {
		for i, site := range renderSites7287 {
			t.Run(site.name, func(t *testing.T) {
				got := scoreAt(t, i, snap)
				if strings.Contains(got, "%") {
					t.Errorf("%s renders the health score as %q — the score is a 0–100 "+
						"composite, not a percentage; formatOptionalPct is the adjacent "+
						"helper with the same signature", site.name, got)
				}
			})
		}
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
	base := QualitySnapshot{Group: "g1", OrphanRate: fptr7287(5), BugRate: fptr7287(2), HealthScore: fptr7287(93)}
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
