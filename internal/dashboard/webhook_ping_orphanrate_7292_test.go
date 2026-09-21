package dashboard

// webhook_ping_orphanrate_7292_test.go — the ping surface of #7292. Both test
// webhook handlers built their payload from a struct literal that set only
// Group, so OrphanRate and TotalEntities took the zero value of a bare float64
// and int: a user clicking "test" sent a receiver orphan_rate:0 /
// total_entities:0, and the Slack renderer printed "Orphan Rate: 0.00%" next to
// the "not measured" that #7287 had just won for the health score.
//
// Both handlers get their own row: they build the payload from two separate
// struct literals, so a DEAD verdict on one says nothing about the other.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/notifications"
)

// pingProblems7292 lists the ways a ping body breaks the contract. A function
// rather than inline assertions so the planted-violation row below can prove
// the absence assertions are capable of failing.
func pingProblems7292(body string) []string {
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

func TestPingProblems7292_FiresOnAPlantedViolation(t *testing.T) {
	planted := `{"event":"ping","quality":{"group":"test","orphan_rate":0,"bug_rate":null,"health_score":null,"total_entities":0}}`
	if got := len(pingProblems7292(planted)); got != 4 {
		t.Fatalf("planted pre-fix body reported %d problems, want 4: %v", got, pingProblems7292(planted))
	}
	honest := `{"event":"ping","quality":{"group":"test","orphan_rate":null,"bug_rate":null,"health_score":null,"total_entities":null}}`
	if got := pingProblems7292(honest); len(got) != 0 {
		t.Fatalf("honest body reported problems: %v", got)
	}
}

func assertPingMeasuresNothing7292(t *testing.T, label, body string) {
	t.Helper()
	if body == "" {
		t.Fatalf("%s: receiver got no body; the rows below would pass vacuously", label)
	}
	// Control: this really is the ping payload.
	if !strings.Contains(body, `"grafel test ping"`) {
		t.Fatalf("%s: body is not the ping payload:\n%s", label, body)
	}
	if probs := pingProblems7292(body); len(probs) > 0 {
		t.Errorf("%s: %v\n%s", label, probs, body)
	}
	// omitempty cannot satisfy the absence rows by making the keys vanish.
	if !strings.Contains(body, `"orphan_rate"`) || !strings.Contains(body, `"total_entities"`) {
		t.Errorf("%s: a key vanished from the ping instead of carrying null:\n%s", label, body)
	}
	// Control: the two fields #7271/#7287 already fixed are still null here, so
	// this row grades #7292's change rather than a rewritten payload.
	if !strings.Contains(body, `"health_score":null`) || !strings.Contains(body, `"bug_rate":null`) {
		t.Errorf("%s: control — health_score and bug_rate should already be null:\n%s", label, body)
	}
}

func TestPingPayload_AdhocMeasuresNoOrphanRate(t *testing.T) {
	srv, cleanup := testWebhookServer(t)
	defer cleanup()
	target, body := captureWebhookTarget(t)
	srv.SetWebhookDispatcher(notifications.NewDispatcher())

	rec := postJSON(t, srv, "/api/webhooks/test", map[string]any{
		"id": "adhoc", "url": target.URL, "enabled": true, "flavor": "generic",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertPingMeasuresNothing7292(t, "adhoc", *body)
}

func TestPingPayload_ByIDMeasuresNoOrphanRate(t *testing.T) {
	srv, cleanup := testWebhookServer(t)
	defer cleanup()
	target, body := captureWebhookTarget(t)
	srv.SetWebhookDispatcher(notifications.NewDispatcher())

	create := postJSON(t, srv, "/api/webhooks", notifications.WebhookConfig{
		ID: "saved", URL: target.URL, Flavor: notifications.FlavorGeneric, Enabled: true,
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", create.Code, create.Body.String())
	}
	rec := postJSON(t, srv, "/api/webhooks/saved/test", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertPingMeasuresNothing7292(t, "by-id", *body)
}

// TestPingPayload_SlackFlavourDoesNotPrintAnOrphanRate is the rendered half:
// the ping's null has to reach a person as words, not as a flawless "0.00%".
func TestPingPayload_SlackFlavourDoesNotPrintAnOrphanRate(t *testing.T) {
	srv, cleanup := testWebhookServer(t)
	defer cleanup()
	target, body := captureWebhookTarget(t)
	srv.SetWebhookDispatcher(notifications.NewDispatcher())

	rec := postJSON(t, srv, "/api/webhooks/test", map[string]any{
		"id": "adhoc", "url": target.URL, "enabled": true, "flavor": "slack",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := *body
	if got == "" {
		t.Fatal("receiver got no body")
	}
	var payload struct {
		Attachments []struct {
			Fields []struct {
				Title string `json:"title"`
				Value string `json:"value"`
			} `json:"fields"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("decode slack body: %v\n%s", err, got)
	}
	if len(payload.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d:\n%s", len(payload.Attachments), got)
	}
	var found bool
	for _, f := range payload.Attachments[0].Fields {
		if f.Title != "Orphan Rate" {
			continue
		}
		found = true
		if f.Value != "not measured" {
			t.Errorf("slack ping Orphan Rate = %q, want %q", f.Value, "not measured")
		}
	}
	if !found {
		t.Fatalf("no Orphan Rate field in the slack ping; the row above graded nothing:\n%s", got)
	}
}
