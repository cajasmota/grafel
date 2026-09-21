package dashboard

// webhook_ping_healthscore_7287_test.go — the two test-ping handlers built
// their payload with a literal `HealthScore: 100` beside a nil BugRate, so a
// user clicking "test" in the dashboard sent a receiver exactly the payload
// #7287 was filed about: {"bug_rate":null,"health_score":100}. A ping measures
// nothing, so it now sends an explicit null for both.
//
// Both handlers get their own row. They build the payload independently, from
// two separate struct literals.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/notifications"
)

// captureWebhookTarget stands up a receiver and returns it plus a pointer to
// the body it was sent.
func captureWebhookTarget(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &body
}

func assertPingIsHonest7287(t *testing.T, label, body string) {
	t.Helper()
	if body == "" {
		t.Fatalf("%s: receiver got no body; the rows below would pass vacuously", label)
	}
	// Control: this really is the ping payload, so the assertions below are
	// looking at the object that carries the score.
	if !strings.Contains(body, `"grafel test ping"`) {
		t.Fatalf("%s: body is not the ping payload:\n%s", label, body)
	}
	if !strings.Contains(body, `"health_score":null`) {
		t.Errorf("%s: ping does not carry an explicit null health_score:\n%s", label, body)
	}
	if strings.Contains(body, `"health_score":100`) {
		t.Errorf("%s: ping fabricates a perfect health score for a run that measured nothing:\n%s", label, body)
	}
	if !strings.Contains(body, `"bug_rate":null`) {
		t.Errorf("%s: control — the bug rate should already be null here:\n%s", label, body)
	}
}

func TestPingPayload_AdhocHasNoFabricatedHealthScore(t *testing.T) {
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
	assertPingIsHonest7287(t, "adhoc", *body)
}

func TestPingPayload_ByIDHasNoFabricatedHealthScore(t *testing.T) {
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
	assertPingIsHonest7287(t, "by-id", *body)
}

// TestPingPayload_SlackFlavourDoesNotPrintAScore is the rendered half: the
// ping's null must reach a person as words, not disappear into a "0.0".
func TestPingPayload_SlackFlavourDoesNotPrintAScore(t *testing.T) {
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
		if f.Title != "Health Score" {
			continue
		}
		found = true
		if f.Value != "not measured" {
			t.Errorf("slack ping Health Score = %q, want %q", f.Value, "not measured")
		}
	}
	if !found {
		t.Fatalf("no Health Score field in the slack ping; the row above graded nothing:\n%s", got)
	}
}
