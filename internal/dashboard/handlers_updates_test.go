package dashboard

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// isNewerVersion — unit tests
// ─────────────────────────────────────────────────────────────────────────────

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"1.2.3", "0.0.0-dev", true}, // dev build → any release is newer
		{"1.2.3", "1.2.3", false},    // same version → not newer
		{"1.2.4", "1.2.3", true},     // patch bump
		{"2.0.0", "1.9.9", true},     // major bump
		{"1.0.0", "2.0.0", false},    // older release
		{"", "1.0.0", false},         // empty latest
		{"1.0.0", "", false},         // empty current
		{"1.0.0-rc1", "0.9.9", true}, // pre-release vs older stable
	}
	for _, tc := range tests {
		got := isNewerVersion(tc.latest, tc.current)
		if got != tc.want {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/updates/check — offline smoke (GitHub may be unreachable in CI)
// ─────────────────────────────────────────────────────────────────────────────

// TestHandleUpdatesCheck_Shape verifies the handler always returns HTTP 200
// and a valid JSON body regardless of whether the GitHub API is reachable.
// If not reachable (common in unit-test environments) FetchError is set —
// that is the correct graceful-degradation behaviour.
func TestHandleUpdatesCheck_Shape(t *testing.T) {
	s, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/updates/check", nil)
	rec := httptest.NewRecorder()
	s.handleUpdatesCheck(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d; body=%s", rec.Code, rec.Body.String())
	}

	var reply UpdateCheckReply
	if err := json.NewDecoder(rec.Body).Decode(&reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reply.CurrentVersion == "" {
		t.Error("CurrentVersion must not be empty")
	}
	if reply.CheckedAt == "" {
		t.Error("CheckedAt must not be empty")
	}
	t.Logf("check reply: version=%s, latest=%q, available=%v, fetchErr=%q",
		reply.CurrentVersion, reply.LatestVersion, reply.UpdateAvailable, reply.FetchError)
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/updates/apply — SSE stream smoke test with mock runner
// ─────────────────────────────────────────────────────────────────────────────

// mockRunner returns fixed output + nil error, simulating a successful update.
func mockRunner(output string) updateRunFunc {
	return func(_ context.Context, _ []string) ([]byte, error) {
		return []byte(output), nil
	}
}

func TestHandleUpdatesApply_SSEShape(t *testing.T) {
	s, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/updates/apply", nil)
	rec := httptest.NewRecorder()
	// Use mock runner — avoids invoking the real binary in tests.
	s.streamUpdateWith(rec, req, false, mockRunner("hook reinstalled\nupdate complete"))

	body := rec.Body.String()

	// Must be text/event-stream
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %q", ct)
	}
	// Must contain a "connected" event
	if !strings.Contains(body, "event: connected") {
		t.Errorf("SSE body missing 'event: connected'; body=%q", truncate(body, 400))
	}
	// Must contain "output" events with the mocked lines
	if !strings.Contains(body, "hook reinstalled") {
		t.Errorf("SSE body missing mocked output line; body=%q", truncate(body, 400))
	}
	if !strings.Contains(body, "update complete") {
		t.Errorf("SSE body missing 'update complete' line; body=%q", truncate(body, 400))
	}
	// Must contain a "done" event with exit_code 0
	if !strings.Contains(body, "event: done") {
		t.Errorf("SSE body missing 'event: done'; body=%q", truncate(body, 400))
	}
	if !strings.Contains(body, `"exit_code":0`) {
		t.Errorf("expected exit_code 0 in done event; body=%q", truncate(body, 400))
	}
	t.Logf("apply SSE body: %s", truncate(body, 500))
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/updates/refresh-rules — SSE shape with mock runner
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleUpdatesRefreshRules_SSEShape(t *testing.T) {
	s, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	var capturedArgs []string
	captureRunner := func(_ context.Context, args []string) ([]byte, error) {
		capturedArgs = args
		return []byte("refreshing rules-lite (no-op in current build)"), nil
	}

	req := httptest.NewRequest("POST", "/api/updates/refresh-rules", nil)
	rec := httptest.NewRecorder()
	s.streamUpdateWith(rec, req, true, captureRunner)

	body := rec.Body.String()

	// connected event should carry refresh_rules_only=true
	if !strings.Contains(body, `"refresh_rules_only":true`) {
		t.Errorf("expected refresh_rules_only=true in connected event; body=%q", truncate(body, 400))
	}
	// runner must have received --refresh-rules-lite flag
	found := false
	for _, a := range capturedArgs {
		if a == "--refresh-rules-lite" {
			found = true
		}
	}
	if !found {
		t.Errorf("runner was not called with --refresh-rules-lite; args=%v", capturedArgs)
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("SSE body missing 'event: done'")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
