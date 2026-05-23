// Package notifications provides webhook delivery for quality events.
//
// After every rebuild, the caller evaluates quality budgets (orphan rate,
// bug rate, secret count, cycle count). If any metric regresses beyond its
// threshold, a JSON POST is fired to each configured webhook URL.
//
// Three payload shapes are supported:
//
//	"slack"   — {"text": "…", "attachments": […]}
//	"discord" — {"content": "…", "embeds": […]}
//	"generic" — the canonical WebhookPayload struct
//
// Delivery is retried up to 3 times with exponential back-off (1s, 2s, 4s).
// Each attempt has a 10-second timeout. Failures are recorded in an in-memory
// ring buffer (last 100 entries) exposed via FailureLog().
package notifications

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// Configuration types (persisted in AppSettings.Webhooks)
// ────────────────────────────────────────────────────────────────────────────

// EventType classifies which quality events trigger a webhook.
type EventType string

const (
	EventRebuildComplete  EventType = "rebuild_complete"
	EventQualityRegressed EventType = "quality_regression"
	EventBudgetExceeded   EventType = "budget_exceeded"
	EventSecretFound      EventType = "secret_found"
)

// WebhookFlavor controls the JSON shape posted to the URL.
type WebhookFlavor string

const (
	FlavorSlack   WebhookFlavor = "slack"
	FlavorDiscord WebhookFlavor = "discord"
	FlavorGeneric WebhookFlavor = "generic"
)

// MuteWindow is a daily time range (local time) during which delivery is
// suppressed so on-call engineers are not paged at 3 am.
type MuteWindow struct {
	// StartHour and EndHour are 0–23 in the local timezone.
	StartHour int `json:"start_hour"`
	EndHour   int `json:"end_hour"`
}

// WebhookConfig is one configured destination. Multiple destinations per
// group or per event type are supported via the slice in AppSettings.
type WebhookConfig struct {
	// ID is a stable, user-visible identifier (e.g. "slack-platform-team").
	ID string `json:"id"`
	// URL is the webhook endpoint. Must be https in production.
	URL string `json:"url"`
	// Secret, when non-empty, is used to sign the request body with HMAC-SHA256.
	// The signature is sent as X-Archigraph-Signature: sha256=<hex>.
	Secret string `json:"secret,omitempty"`
	// Flavor determines the JSON shape. Defaults to FlavorGeneric when empty.
	Flavor WebhookFlavor `json:"flavor,omitempty"`
	// Events is the set of events that trigger this webhook. An empty slice
	// means "fire on every event".
	Events []EventType `json:"events,omitempty"`
	// Group, when set, limits delivery to a specific archigraph group.
	// Empty means "all groups".
	Group string `json:"group,omitempty"`
	// Mute defines a daily quiet window. Nil means always deliver.
	Mute *MuteWindow `json:"mute,omitempty"`
	// Enabled allows a hook to be saved but temporarily disabled.
	Enabled bool `json:"enabled"`
}

// ────────────────────────────────────────────────────────────────────────────
// Payload types
// ────────────────────────────────────────────────────────────────────────────

// QualitySnapshot is the measured state at the time of the event.
type QualitySnapshot struct {
	Group         string   `json:"group"`
	OrphanRate    float64  `json:"orphan_rate"`
	BugRate       float64  `json:"bug_rate"`
	HealthScore   float64  `json:"health_score"`
	TotalEntities int      `json:"total_entities"`
	Cycles        *int     `json:"cycles,omitempty"`
	Secrets       *int     `json:"secrets,omitempty"`
	CoveragePct   *float64 `json:"coverage_pct,omitempty"`
}

// WebhookPayload is the canonical (generic) event body.
type WebhookPayload struct {
	Event     EventType       `json:"event"`
	Timestamp time.Time       `json:"timestamp"`
	Quality   QualitySnapshot `json:"quality"`
	// Details contains event-specific context (e.g. which budgets were exceeded).
	Details map[string]any `json:"details,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// Failure log
// ────────────────────────────────────────────────────────────────────────────

// DeliveryFailure records one failed delivery attempt.
type DeliveryFailure struct {
	WebhookID  string    `json:"webhook_id"`
	URL        string    `json:"url"`
	Event      EventType `json:"event"`
	Attempt    int       `json:"attempt"`
	StatusCode int       `json:"status_code,omitempty"` // 0 when no HTTP response
	ErrMsg     string    `json:"err_msg"`
	OccurredAt time.Time `json:"occurred_at"`
}

const failureRingSize = 100

type failureRing struct {
	mu      sync.Mutex
	entries []DeliveryFailure
	head    int
	full    bool
}

func (r *failureRing) append(f DeliveryFailure) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) < failureRingSize {
		r.entries = append(r.entries, f)
	} else {
		r.entries[r.head] = f
		r.head = (r.head + 1) % failureRingSize
		r.full = true
	}
}

func (r *failureRing) all() []DeliveryFailure {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		cp := make([]DeliveryFailure, len(r.entries))
		copy(cp, r.entries)
		return cp
	}
	out := make([]DeliveryFailure, failureRingSize)
	copy(out, r.entries[r.head:])
	copy(out[failureRingSize-r.head:], r.entries[:r.head])
	return out
}

// ────────────────────────────────────────────────────────────────────────────
// Dispatcher
// ────────────────────────────────────────────────────────────────────────────

const (
	deliveryTimeout = 10 * time.Second
	maxRetries      = 3
)

// Dispatcher holds a shared HTTP client and failure log. It is safe for
// concurrent use. Construct one via NewDispatcher and inject it into the
// dashboard Server via SetWebhookDispatcher.
type Dispatcher struct {
	client   *http.Client
	failures failureRing
	// retryDelays controls the back-off between retries (index 0 = first retry).
	retryDelays []time.Duration
}

// NewDispatcher returns a Dispatcher ready to fire webhooks.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		client: &http.Client{
			Timeout: deliveryTimeout,
		},
		retryDelays: []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second},
	}
}

// FailureLog returns a snapshot of the most recent delivery failures.
func (d *Dispatcher) FailureLog() []DeliveryFailure {
	return d.failures.all()
}

// Dispatch evaluates cfg against the event and fires delivery when the hook
// matches. It blocks until all retries are exhausted (or succeed). Call it
// from a goroutine when async delivery is desired.
func (d *Dispatcher) Dispatch(cfg WebhookConfig, payload WebhookPayload) {
	if !cfg.Enabled {
		return
	}
	// Group filter.
	if cfg.Group != "" && cfg.Group != payload.Quality.Group {
		return
	}
	// Event filter.
	if len(cfg.Events) > 0 && !containsEvent(cfg.Events, payload.Event) {
		return
	}
	// Mute window.
	if cfg.Mute != nil && inMuteWindow(cfg.Mute) {
		return
	}

	body, err := marshalPayload(cfg.Flavor, payload)
	if err != nil {
		d.failures.append(DeliveryFailure{
			WebhookID:  cfg.ID,
			URL:        cfg.URL,
			Event:      payload.Event,
			Attempt:    0,
			ErrMsg:     "marshal: " + err.Error(),
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	var lastErr error
	var lastStatus int
	for attempt := 1; attempt <= maxRetries; attempt++ {
		lastStatus, lastErr = d.postOnce(cfg, body)
		if lastErr == nil {
			return // success
		}
		if attempt < maxRetries {
			delay := d.retryDelays[attempt-1]
			time.Sleep(delay)
		}
		d.failures.append(DeliveryFailure{
			WebhookID:  cfg.ID,
			URL:        cfg.URL,
			Event:      payload.Event,
			Attempt:    attempt,
			StatusCode: lastStatus,
			ErrMsg:     lastErr.Error(),
			OccurredAt: time.Now().UTC(),
		})
	}
}

// DispatchAll fires Dispatch for every webhook in cfgs, each in its own
// goroutine. It returns immediately.
func (d *Dispatcher) DispatchAll(cfgs []WebhookConfig, payload WebhookPayload) {
	for _, cfg := range cfgs {
		go d.Dispatch(cfg, payload)
	}
}

// PostOnceForTest is the exported version of postOnce for use by the dashboard
// test-webhook endpoints. It sends a single attempt with no retries and returns
// (statusCode, error).
func (d *Dispatcher) PostOnceForTest(cfg WebhookConfig, payload WebhookPayload) (int, error) {
	body, err := marshalPayload(cfg.Flavor, payload)
	if err != nil {
		return 0, fmt.Errorf("marshal: %w", err)
	}
	return d.postOnce(cfg, body)
}

// postOnce sends one HTTP POST. Returns (statusCode, error).
func (d *Dispatcher) postOnce(cfg WebhookConfig, body []byte) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), deliveryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "archigraph-webhook/1.0")
	if cfg.Secret != "" {
		sig := signPayload(body, cfg.Secret)
		req.Header.Set("X-Archigraph-Signature", "sha256="+sig)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("non-2xx status %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Payload shaping
// ────────────────────────────────────────────────────────────────────────────

func marshalPayload(flavor WebhookFlavor, p WebhookPayload) ([]byte, error) {
	switch flavor {
	case FlavorSlack:
		return marshalSlack(p)
	case FlavorDiscord:
		return marshalDiscord(p)
	default:
		return json.Marshal(p)
	}
}

func marshalSlack(p WebhookPayload) ([]byte, error) {
	text := summaryText(p)
	color := "#2eb886" // green
	if p.Event == EventQualityRegressed || p.Event == EventBudgetExceeded || p.Event == EventSecretFound {
		color = "#e01e5a"
	}

	fields := []map[string]any{
		{"title": "Group", "value": p.Quality.Group, "short": true},
		{"title": "Health Score", "value": fmt.Sprintf("%.1f", p.Quality.HealthScore), "short": true},
		{"title": "Orphan Rate", "value": fmt.Sprintf("%.2f%%", p.Quality.OrphanRate), "short": true},
		{"title": "Bug Rate", "value": fmt.Sprintf("%.2f%%", p.Quality.BugRate), "short": true},
	}
	if p.Quality.Secrets != nil {
		fields = append(fields, map[string]any{"title": "Secrets", "value": fmt.Sprintf("%d", *p.Quality.Secrets), "short": true})
	}
	if p.Quality.Cycles != nil {
		fields = append(fields, map[string]any{"title": "Cycles", "value": fmt.Sprintf("%d", *p.Quality.Cycles), "short": true})
	}

	payload := map[string]any{
		"text": text,
		"attachments": []map[string]any{
			{
				"color":  color,
				"fields": fields,
				"footer": "archigraph",
				"ts":     p.Timestamp.Unix(),
			},
		},
	}
	return json.Marshal(payload)
}

func marshalDiscord(p WebhookPayload) ([]byte, error) {
	text := summaryText(p)
	color := 3066993 // green decimal
	if p.Event == EventQualityRegressed || p.Event == EventBudgetExceeded || p.Event == EventSecretFound {
		color = 14750250 // red decimal
	}

	fields := []map[string]any{
		{"name": "Health Score", "value": fmt.Sprintf("%.1f", p.Quality.HealthScore), "inline": true},
		{"name": "Orphan Rate", "value": fmt.Sprintf("%.2f%%", p.Quality.OrphanRate), "inline": true},
		{"name": "Bug Rate", "value": fmt.Sprintf("%.2f%%", p.Quality.BugRate), "inline": true},
	}
	if p.Quality.Secrets != nil {
		fields = append(fields, map[string]any{"name": "Secrets", "value": fmt.Sprintf("%d", *p.Quality.Secrets), "inline": true})
	}
	if p.Quality.Cycles != nil {
		fields = append(fields, map[string]any{"name": "Cycles", "value": fmt.Sprintf("%d", *p.Quality.Cycles), "inline": true})
	}

	payload := map[string]any{
		"content": "",
		"embeds": []map[string]any{
			{
				"title":     text,
				"color":     color,
				"fields":    fields,
				"footer":    map[string]any{"text": "archigraph"},
				"timestamp": p.Timestamp.Format(time.RFC3339),
			},
		},
	}
	return json.Marshal(payload)
}

func summaryText(p WebhookPayload) string {
	switch p.Event {
	case EventRebuildComplete:
		return fmt.Sprintf("[archigraph] Rebuild complete — %s (health %.1f)", p.Quality.Group, p.Quality.HealthScore)
	case EventQualityRegressed:
		return fmt.Sprintf("[archigraph] Quality regression detected — %s (health %.1f)", p.Quality.Group, p.Quality.HealthScore)
	case EventBudgetExceeded:
		return fmt.Sprintf("[archigraph] Quality budget exceeded — %s", p.Quality.Group)
	case EventSecretFound:
		sCount := 0
		if p.Quality.Secrets != nil {
			sCount = *p.Quality.Secrets
		}
		return fmt.Sprintf("[archigraph] Secret scan: %d finding(s) — %s", sCount, p.Quality.Group)
	default:
		return fmt.Sprintf("[archigraph] Event: %s — %s", p.Event, p.Quality.Group)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Budget regression detector
// ────────────────────────────────────────────────────────────────────────────

// QualityBudgets holds the maximum acceptable values for each metric.
// A zero value means "no budget set for this metric".
type QualityBudgets struct {
	MaxOrphanRate float64 `json:"max_orphan_rate,omitempty"` // percent 0–100
	MaxBugRate    float64 `json:"max_bug_rate,omitempty"`    // percent 0–100
	MaxSecrets    int     `json:"max_secrets,omitempty"`
	MaxCycles     int     `json:"max_cycles,omitempty"`
}

// BudgetViolation describes a single exceeded budget.
type BudgetViolation struct {
	Metric    string  `json:"metric"`
	Threshold float64 `json:"threshold"`
	Actual    float64 `json:"actual"`
}

// CheckBudgets compares snap against budgets and returns any violations.
func CheckBudgets(snap QualitySnapshot, budgets QualityBudgets) []BudgetViolation {
	var out []BudgetViolation
	if budgets.MaxOrphanRate > 0 && snap.OrphanRate > budgets.MaxOrphanRate {
		out = append(out, BudgetViolation{"orphan_rate", budgets.MaxOrphanRate, snap.OrphanRate})
	}
	if budgets.MaxBugRate > 0 && snap.BugRate > budgets.MaxBugRate {
		out = append(out, BudgetViolation{"bug_rate", budgets.MaxBugRate, snap.BugRate})
	}
	if budgets.MaxSecrets > 0 && snap.Secrets != nil && *snap.Secrets > budgets.MaxSecrets {
		out = append(out, BudgetViolation{"secrets", float64(budgets.MaxSecrets), float64(*snap.Secrets)})
	}
	if budgets.MaxCycles > 0 && snap.Cycles != nil && *snap.Cycles > budgets.MaxCycles {
		out = append(out, BudgetViolation{"cycles", float64(budgets.MaxCycles), float64(*snap.Cycles)})
	}
	return out
}

// RegressionDetected returns true when current is measurably worse than
// previous on any of the key quality metrics (using a 0.5% threshold to
// avoid noise from floating-point drift).
func RegressionDetected(prev, curr QualitySnapshot) bool {
	const eps = 0.5
	if curr.OrphanRate > prev.OrphanRate+eps {
		return true
	}
	if curr.BugRate > prev.BugRate+eps {
		return true
	}
	if curr.Secrets != nil && prev.Secrets != nil && *curr.Secrets > *prev.Secrets {
		return true
	}
	if curr.Cycles != nil && prev.Cycles != nil && *curr.Cycles > *prev.Cycles {
		return true
	}
	return false
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

func signPayload(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func containsEvent(events []EventType, target EventType) bool {
	for _, e := range events {
		if e == target {
			return true
		}
	}
	return false
}

func inMuteWindow(m *MuteWindow) bool {
	h := time.Now().Local().Hour()
	if m.StartHour <= m.EndHour {
		return h >= m.StartHour && h < m.EndHour
	}
	// Wraps midnight (e.g. 22–06)
	return h >= m.StartHour || h < m.EndHour
}
