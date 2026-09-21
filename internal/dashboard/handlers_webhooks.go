package dashboard

// handlers_webhooks.go — Webhook management HTTP handlers.
//
// Routes registered in server.go:
//
//	GET    /api/webhooks                    — list configured webhooks
//	POST   /api/webhooks                    — create a new webhook
//	PUT    /api/webhooks/{id}               — update an existing webhook
//	DELETE /api/webhooks/{id}               — remove a webhook
//	POST   /api/webhooks/{id}/test          — fire a test ping to one webhook
//	POST   /api/webhooks/test               — fire a test ping to an ad-hoc URL
//	GET    /api/webhooks/failures           — recent delivery failure log
//
// All mutations delegate to loadSettings / saveSettings so the change is
// reflected immediately on the next rebuild.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cajasmota/grafel/internal/notifications"
)

// ─────────────────────────────────────────────────────────────────────────────
// handleListWebhooks — GET /api/webhooks
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleListWebhooks(w http.ResponseWriter, _ *http.Request) {
	settings, err := loadSettings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	hooks := settings.Webhooks
	if hooks == nil {
		hooks = []notifications.WebhookConfig{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"webhooks": hooks})
}

// ─────────────────────────────────────────────────────────────────────────────
// handleCreateWebhook — POST /api/webhooks
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	var cfg notifications.WebhookConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := validateWebhookConfig(cfg); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	settings, err := loadSettings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Reject duplicate IDs.
	for _, existing := range settings.Webhooks {
		if existing.ID == cfg.ID {
			writeErr(w, http.StatusConflict, "webhook id already exists: "+cfg.ID)
			return
		}
	}

	settings.Webhooks = append(settings.Webhooks, cfg)
	if err := saveSettings(settings); err != nil {
		s.auditor.Err("webhook_create", "", nil, err.Error())
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditor.OK("webhook_create", "", map[string]any{"id": cfg.ID})
	writeJSON(w, http.StatusCreated, map[string]any{"webhook": cfg})
}

// ─────────────────────────────────────────────────────────────────────────────
// handleUpdateWebhook — PUT /api/webhooks/{id}
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id required")
		return
	}

	var patch notifications.WebhookConfig
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	patch.ID = id // always use path param as authoritative ID

	if err := validateWebhookConfig(patch); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	settings, err := loadSettings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	found := false
	for i, h := range settings.Webhooks {
		if h.ID == id {
			settings.Webhooks[i] = patch
			found = true
			break
		}
	}
	if !found {
		writeErr(w, http.StatusNotFound, "webhook not found: "+id)
		return
	}

	if err := saveSettings(settings); err != nil {
		s.auditor.Err("webhook_update", "", nil, err.Error())
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditor.OK("webhook_update", "", map[string]any{"id": id})
	writeJSON(w, http.StatusOK, map[string]any{"webhook": patch})
}

// ─────────────────────────────────────────────────────────────────────────────
// handleDeleteWebhook — DELETE /api/webhooks/{id}
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id required")
		return
	}

	settings, err := loadSettings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	filtered := settings.Webhooks[:0]
	found := false
	for _, h := range settings.Webhooks {
		if h.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, h)
	}
	if !found {
		writeErr(w, http.StatusNotFound, "webhook not found: "+id)
		return
	}
	settings.Webhooks = filtered

	if err := saveSettings(settings); err != nil {
		s.auditor.Err("webhook_delete", "", nil, err.Error())
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditor.OK("webhook_delete", "", map[string]any{"id": id})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// ─────────────────────────────────────────────────────────────────────────────
// handleTestWebhookByID — POST /api/webhooks/{id}/test
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleTestWebhookByID(w http.ResponseWriter, r *http.Request) {
	if s.webhookDispatcher == nil {
		writeErr(w, http.StatusServiceUnavailable, "webhook dispatcher not configured")
		return
	}

	id := r.PathValue("id")
	settings, err := loadSettings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	var found *notifications.WebhookConfig
	for i := range settings.Webhooks {
		if settings.Webhooks[i].ID == id {
			found = &settings.Webhooks[i]
			break
		}
	}
	if found == nil {
		writeErr(w, http.StatusNotFound, "webhook not found: "+id)
		return
	}

	testCfg := *found
	testCfg.Enabled = true // force delivery even if currently disabled
	testCfg.Events = nil   // bypass event filter for test pings
	testCfg.Mute = nil     // bypass mute window for test pings

	pingPayload := notifications.WebhookPayload{
		Event:     "ping",
		Timestamp: time.Now().UTC(),
		Quality: notifications.QualitySnapshot{
			Group: "test",
			// No HealthScore: a ping measures nothing, so it emits an explicit
			// null alongside the null bug rate rather than a fabricated 100
			// that the Slack renderer would print beside "not measured" (#7287).
			// This does NOT make the whole ping honest: OrphanRate and
			// TotalEntities are still a bare float64 and int with no unknown
			// state, so the ping also ships orphan_rate:0 and
			// total_entities:0. Same class, explicitly deferred by #7287 along
			// with quality.HealthEntry.OrphanRate.
		},
		Details: map[string]any{"message": "grafel test ping"},
	}

	var deliveryErr error
	var statusCode int
	statusCode, deliveryErr = s.webhookDispatcher.PostOnceForTest(testCfg, pingPayload)

	if deliveryErr != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":     false,
			"status_code": statusCode,
			"error":       deliveryErr.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"status_code": statusCode,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// handleTestWebhookAdhoc — POST /api/webhooks/test
// ─────────────────────────────────────────────────────────────────────────────
// Accepts an ad-hoc config body so the user can test a URL before saving it.

func (s *Server) handleTestWebhookAdhoc(w http.ResponseWriter, r *http.Request) {
	if s.webhookDispatcher == nil {
		writeErr(w, http.StatusServiceUnavailable, "webhook dispatcher not configured")
		return
	}

	var cfg notifications.WebhookConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if cfg.URL == "" {
		writeErr(w, http.StatusUnprocessableEntity, "url is required")
		return
	}
	cfg.Enabled = true
	cfg.Events = nil
	cfg.Mute = nil

	pingPayload := notifications.WebhookPayload{
		Event:     "ping",
		Timestamp: time.Now().UTC(),
		Quality: notifications.QualitySnapshot{
			Group: "test",
			// No HealthScore: a ping measures nothing, so it emits an explicit
			// null alongside the null bug rate rather than a fabricated 100
			// that the Slack renderer would print beside "not measured" (#7287).
			// This does NOT make the whole ping honest: OrphanRate and
			// TotalEntities are still a bare float64 and int with no unknown
			// state, so the ping also ships orphan_rate:0 and
			// total_entities:0. Same class, explicitly deferred by #7287 along
			// with quality.HealthEntry.OrphanRate.
		},
		Details: map[string]any{"message": "grafel test ping"},
	}

	statusCode, deliveryErr := s.webhookDispatcher.PostOnceForTest(cfg, pingPayload)

	if deliveryErr != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":     false,
			"status_code": statusCode,
			"error":       deliveryErr.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"status_code": statusCode,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// handleWebhookFailures — GET /api/webhooks/failures
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleWebhookFailures(w http.ResponseWriter, _ *http.Request) {
	if s.webhookDispatcher == nil {
		writeJSON(w, http.StatusOK, map[string]any{"failures": []any{}})
		return
	}
	failures := s.webhookDispatcher.FailureLog()
	if failures == nil {
		failures = []notifications.DeliveryFailure{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"failures": failures})
}

// ─────────────────────────────────────────────────────────────────────────────
// Validation
// ─────────────────────────────────────────────────────────────────────────────

func validateWebhookConfig(cfg notifications.WebhookConfig) error {
	if cfg.ID == "" {
		return fmt.Errorf("webhook id is required")
	}
	if cfg.URL == "" {
		return fmt.Errorf("webhook url is required")
	}
	switch cfg.Flavor {
	case "", notifications.FlavorGeneric, notifications.FlavorSlack, notifications.FlavorDiscord:
		// valid
	default:
		return fmt.Errorf("flavor must be slack, discord, or generic; got %q", cfg.Flavor)
	}
	for _, e := range cfg.Events {
		switch e {
		case notifications.EventRebuildComplete,
			notifications.EventQualityRegressed,
			notifications.EventBudgetExceeded,
			notifications.EventSecretFound:
			// valid
		default:
			return fmt.Errorf("unknown event type %q", e)
		}
	}
	if m := cfg.Mute; m != nil {
		if m.StartHour < 0 || m.StartHour > 23 {
			return fmt.Errorf("mute.start_hour must be 0–23")
		}
		if m.EndHour < 0 || m.EndHour > 23 {
			return fmt.Errorf("mute.end_hour must be 0–23")
		}
	}
	return nil
}
