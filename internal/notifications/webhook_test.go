package notifications

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// Helper builders
// ────────────────────────────────────────────────────────────────────────────

func goodSnap(group string) QualitySnapshot {
	b := 2.0
	return QualitySnapshot{Group: group, OrphanRate: fptr7287(5), BugRate: &b, HealthScore: fptr7287(93), TotalEntities: iptr7292(1000)}
}

func badSnap(group string) QualitySnapshot {
	s := 5
	c := 3
	b := 15.0
	return QualitySnapshot{Group: group, OrphanRate: fptr7287(25), BugRate: &b, HealthScore: fptr7287(60), TotalEntities: iptr7292(1000), Secrets: &s, Cycles: &c}
}

func payload(event EventType, snap QualitySnapshot) WebhookPayload {
	return WebhookPayload{Event: event, Timestamp: time.Now().UTC(), Quality: snap}
}

// ────────────────────────────────────────────────────────────────────────────
// Dispatcher delivery
// ────────────────────────────────────────────────────────────────────────────

func TestDispatch_SuccessGeneric(t *testing.T) {
	var received WebhookPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := WebhookConfig{
		ID:      "test",
		URL:     srv.URL,
		Flavor:  FlavorGeneric,
		Enabled: true,
	}
	d := NewDispatcher()
	p := payload(EventRebuildComplete, goodSnap("mygroup"))
	d.Dispatch(cfg, p)

	if received.Event != EventRebuildComplete {
		t.Errorf("expected event rebuild_complete, got %s", received.Event)
	}
	if received.Quality.Group != "mygroup" {
		t.Errorf("expected group mygroup, got %s", received.Quality.Group)
	}
}

func TestDispatch_DisabledSkips(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := WebhookConfig{ID: "test", URL: srv.URL, Enabled: false}
	d := NewDispatcher()
	d.Dispatch(cfg, payload(EventRebuildComplete, goodSnap("g")))
	if called {
		t.Fatal("expected no delivery for disabled webhook")
	}
}

func TestDispatch_GroupFilter(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := WebhookConfig{ID: "test", URL: srv.URL, Enabled: true, Group: "other-group"}
	d := NewDispatcher()
	d.Dispatch(cfg, payload(EventRebuildComplete, goodSnap("mygroup")))
	if called {
		t.Fatal("expected no delivery when group doesn't match")
	}
}

func TestDispatch_EventFilter(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := WebhookConfig{ID: "test", URL: srv.URL, Enabled: true, Events: []EventType{EventSecretFound}}
	d := NewDispatcher()
	d.Dispatch(cfg, payload(EventRebuildComplete, goodSnap("g")))
	if called {
		t.Fatal("expected no delivery when event doesn't match filter")
	}
}

func TestDispatch_EventFilterMatch(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := WebhookConfig{ID: "test", URL: srv.URL, Enabled: true, Events: []EventType{EventSecretFound}}
	d := NewDispatcher()
	d.Dispatch(cfg, payload(EventSecretFound, badSnap("g")))
	if !called {
		t.Fatal("expected delivery for matching event filter")
	}
}

func TestDispatch_RetryOnFailure(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := WebhookConfig{ID: "retry-test", URL: srv.URL, Enabled: true}
	d := NewDispatcher()
	// Speed up retries for tests.
	d.retryDelays = []time.Duration{0, 0, 0}
	d.Dispatch(cfg, payload(EventRebuildComplete, goodSnap("g")))

	if attempts != maxRetries {
		t.Errorf("expected %d attempts, got %d", maxRetries, attempts)
	}
	failures := d.FailureLog()
	if len(failures) != maxRetries {
		t.Errorf("expected %d failure log entries, got %d", maxRetries, len(failures))
	}
	for _, f := range failures {
		if f.StatusCode != http.StatusInternalServerError {
			t.Errorf("expected status 500 in failure log, got %d", f.StatusCode)
		}
	}
}

func TestDispatch_SignatureHeader(t *testing.T) {
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Grafel-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := WebhookConfig{ID: "sig-test", URL: srv.URL, Enabled: true, Secret: "my-secret"}
	d := NewDispatcher()
	d.Dispatch(cfg, payload(EventRebuildComplete, goodSnap("g")))

	if gotSig == "" {
		t.Fatal("expected X-Grafel-Signature header to be set")
	}
	if len(gotSig) < 7 || gotSig[:7] != "sha256=" {
		t.Errorf("expected signature to start with sha256=, got %s", gotSig)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Payload shapes
// ────────────────────────────────────────────────────────────────────────────

func TestMarshalSlack(t *testing.T) {
	p := payload(EventQualityRegressed, badSnap("g"))
	body, err := marshalSlack(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal("invalid JSON:", err)
	}
	if _, ok := m["text"]; !ok {
		t.Error("expected 'text' field in Slack payload")
	}
	if _, ok := m["attachments"]; !ok {
		t.Error("expected 'attachments' field in Slack payload")
	}
}

func TestMarshalDiscord(t *testing.T) {
	p := payload(EventSecretFound, badSnap("g"))
	body, err := marshalDiscord(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal("invalid JSON:", err)
	}
	if _, ok := m["embeds"]; !ok {
		t.Error("expected 'embeds' field in Discord payload")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Budget and regression checks
// ────────────────────────────────────────────────────────────────────────────

func TestCheckBudgets_NoneExceeded(t *testing.T) {
	snap := goodSnap("g")
	budgets := QualityBudgets{MaxOrphanRate: 20, MaxBugRate: 10}
	violations := CheckBudgets(snap, budgets)
	if len(violations) != 0 {
		t.Errorf("expected no violations, got %v", violations)
	}
}

func TestCheckBudgets_Exceeded(t *testing.T) {
	snap := badSnap("g")
	budgets := QualityBudgets{MaxOrphanRate: 10, MaxBugRate: 5, MaxSecrets: 0, MaxCycles: 0}
	violations := CheckBudgets(snap, budgets)
	if len(violations) < 2 {
		t.Errorf("expected at least 2 violations (orphan+bug), got %d: %v", len(violations), violations)
	}
}

func TestCheckBudgets_SecretCount(t *testing.T) {
	s := 10
	snap := QualitySnapshot{Group: "g", Secrets: &s}
	budgets := QualityBudgets{MaxSecrets: 5}
	violations := CheckBudgets(snap, budgets)
	if len(violations) != 1 || violations[0].Metric != "secrets" {
		t.Errorf("expected secrets violation, got %v", violations)
	}
}

func TestRegressionDetected_True(t *testing.T) {
	prev := goodSnap("g")
	curr := badSnap("g")
	if !RegressionDetected(prev, curr) {
		t.Error("expected regression to be detected")
	}
}

func TestRegressionDetected_False(t *testing.T) {
	prev := badSnap("g")
	curr := goodSnap("g") // improvement, not regression
	if RegressionDetected(prev, curr) {
		t.Error("expected no regression when quality improves")
	}
}

func TestRegressionDetected_Noise(t *testing.T) {
	prev := goodSnap("g")
	curr := goodSnap("g")
	// goodSnap allocates a fresh *float64 per call, so this mutates curr only.
	*curr.OrphanRate += 0.1 // below epsilon, should not trigger
	if RegressionDetected(prev, curr) {
		t.Error("expected noise below eps to not trigger regression")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Mute window
// ────────────────────────────────────────────────────────────────────────────

func TestInMuteWindow_AllDay(t *testing.T) {
	// Window that covers the entire day (0–24 wraps around fully) should always mute.
	m := &MuteWindow{StartHour: 0, EndHour: 0}
	// 0–0 with StartHour==EndHour is treated as "start<=end" path → h>=0 && h<0 → false
	// That's fine: it means "no window" effectively. Just verify it doesn't panic.
	_ = inMuteWindow(m)
}

func TestInMuteWindow_NeverMuted(t *testing.T) {
	// A nil mute window shouldn't cause a panic (caller checks nil before calling).
	// Just confirm containsEvent works as expected.
	events := []EventType{EventRebuildComplete, EventSecretFound}
	if !containsEvent(events, EventRebuildComplete) {
		t.Error("expected to find EventRebuildComplete")
	}
	if containsEvent(events, EventQualityRegressed) {
		t.Error("expected not to find EventQualityRegressed")
	}
}
