package notifications

// bugrate_7271_test.go — an unmeasured bug rate must not reach a webhook
// consumer disguised as a measured zero (#7271). The rebuild path used to send
// BugRate: 0 for every group, beside a real orphan rate.

import (
	"strings"
	"testing"
	"time"
)

func TestQualitySnapshot_UnmeasuredBugRateRendersAsNotMeasured(t *testing.T) {
	p := WebhookPayload{
		Event:     EventRebuildComplete,
		Timestamp: time.Now().UTC(),
		Quality:   QualitySnapshot{Group: "g", OrphanRate: fptr7287(12), HealthScore: fptr7287(88)},
	}

	for _, tc := range []struct {
		flavor string
		build  func(WebhookPayload) ([]byte, error)
	}{
		{"slack", marshalSlack},
		{"discord", marshalDiscord},
	} {
		body, err := tc.build(p)
		if err != nil {
			t.Fatalf("%s: marshal: %v", tc.flavor, err)
		}
		got := string(body)
		if !strings.Contains(got, "not measured") {
			t.Errorf("%s payload does not say the bug rate is unmeasured:\n%s", tc.flavor, got)
		}
		if strings.Contains(got, "0.00%") {
			t.Errorf("%s payload renders an unmeasured bug rate as 0.00%%:\n%s", tc.flavor, got)
		}
	}
}

func TestQualitySnapshot_MeasuredBugRateStillRenders(t *testing.T) {
	b := 7.25
	p := WebhookPayload{
		Event:     EventRebuildComplete,
		Timestamp: time.Now().UTC(),
		Quality:   QualitySnapshot{Group: "g", OrphanRate: fptr7287(12), BugRate: &b, HealthScore: fptr7287(88)},
	}
	body, err := marshalSlack(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(body); !strings.Contains(got, "7.25%") {
		t.Errorf("slack payload lost the measured bug rate:\n%s", got)
	}
}

// TestCheckBudgets_UnmeasuredBugRateBreachesNothing is the forbidden row for
// budgets: reading nil as 0 would silently certify every unmeasured group as
// inside its threshold.
func TestCheckBudgets_UnmeasuredBugRateBreachesNothing(t *testing.T) {
	budgets := QualityBudgets{MaxBugRate: 1}

	if v := CheckBudgets(QualitySnapshot{Group: "g"}, budgets); len(v) != 0 {
		t.Errorf("unmeasured bug rate produced violations: %+v", v)
	}

	// Positive control: the same budget DOES fire on a measured breach, so the
	// assertion above is not passing because budget checking is inert.
	b := 9.0
	v := CheckBudgets(QualitySnapshot{Group: "g", BugRate: &b}, budgets)
	if len(v) != 1 || v[0].Metric != "bug_rate" || v[0].Actual != 9.0 {
		t.Errorf("measured 9%% against a 1%% budget produced %+v, want one bug_rate violation", v)
	}
}

// TestRegressionDetected_NeedsTwoMeasuredBugRates pins the same rule for the
// regression comparison, in both directions.
func TestRegressionDetected_NeedsTwoMeasuredBugRates(t *testing.T) {
	low, high := 1.0, 50.0

	if RegressionDetected(QualitySnapshot{BugRate: &low}, QualitySnapshot{}) {
		t.Errorf("an unmeasured current rate must not be compared as a number")
	}
	if RegressionDetected(QualitySnapshot{}, QualitySnapshot{BugRate: &high}) {
		t.Errorf("an unmeasured previous rate must not be compared as a number")
	}
	// Positive control: two measured rates still detect the regression.
	if !RegressionDetected(QualitySnapshot{BugRate: &low}, QualitySnapshot{BugRate: &high}) {
		t.Errorf("1%% → 50%% is a regression and was not detected")
	}
}

// TestWebhookPayload_UnmeasuredBugRateIsExplicitNull asserts the SERIALISED
// bytes of the generic payload, not the struct. The slack and discord flavours
// are pinned above, but the plain JSON body is what most consumers receive, and
// nothing observed it: adding `,omitempty` to the field's tag would drop
// bug_rate from the body entirely, and a consumer defaulting a missing key to 0
// lands back on the original defect — an unmeasured rate reading as perfect.
func TestWebhookPayload_UnmeasuredBugRateIsExplicitNull(t *testing.T) {
	body, err := marshalPayload(FlavorGeneric, WebhookPayload{
		Event:     EventRebuildComplete,
		Timestamp: time.Now().UTC(),
		Quality:   QualitySnapshot{Group: "g", OrphanRate: fptr7287(12), HealthScore: fptr7287(88)},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, `"bug_rate":null`) {
		t.Errorf("payload does not carry an explicit null bug_rate:\n%s", got)
	}

	// Positive control: a measured rate serialises as the number.
	b := 7.25
	body, err = marshalPayload(FlavorGeneric, WebhookPayload{
		Event:     EventRebuildComplete,
		Timestamp: time.Now().UTC(),
		Quality:   QualitySnapshot{Group: "g", OrphanRate: fptr7287(12), BugRate: &b, HealthScore: fptr7287(88)},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(body); !strings.Contains(got, `"bug_rate":7.25`) {
		t.Errorf("measured bug_rate missing from the body:\n%s", got)
	}
}
