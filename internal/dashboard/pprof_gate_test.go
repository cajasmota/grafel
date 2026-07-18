// pprof_gate_test.go — RED/GREEN coverage for #5822 sub-ask 2: expose
// net/http/pprof on the dashboard mux, gated OFF by default.
//
// Two invariants under test:
//  1. GRAFEL_DEBUG_PPROF=1 (gate ON) + a loopback bind (the default) mounts
//     /debug/pprof/* on s.routes() and GET /debug/pprof/heap serves an actual
//     pprof payload — NOT the SPA's index.html fallback.
//  2. Gate OFF (env unset/falsy) — the pprof route is absent: the request
//     falls through to the SPA catch-all (or 404, since the test binary embeds
//     no real dist/index.html) and MUST NOT return pprof content.
package dashboard

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPprofGate_OnLoopback_ServesHeapProfile(t *testing.T) {
	t.Setenv(EnvDebugPprof, "1")

	cfg := DefaultConfig() // Bind: "127.0.0.1"
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	h := srv.routes()
	req := httptest.NewRequest("GET", "/debug/pprof/heap?debug=1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("gate ON: GET /debug/pprof/heap = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "heap profile:") {
		t.Fatalf("gate ON: expected pprof heap profile payload, got: %s", body)
	}
	if strings.Contains(strings.ToLower(body), "<!doctype html") || strings.Contains(strings.ToLower(body), "<html") {
		t.Fatalf("gate ON: /debug/pprof/heap returned SPA HTML instead of a pprof payload: %s", body)
	}
}

func TestPprofGate_Off_DoesNotServePprof(t *testing.T) {
	// Explicitly unset (in case the parent process env carries it).
	t.Setenv(EnvDebugPprof, "")

	cfg := DefaultConfig()
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	h := srv.routes()
	req := httptest.NewRequest("GET", "/debug/pprof/heap?debug=1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "heap profile:") {
		t.Fatalf("gate OFF: /debug/pprof/heap must not serve a pprof payload, got: %s", body)
	}
	// Falls through to the SPA catch-all (404 in this test binary, which embeds
	// only a placeholder dist/) — never a 200 pprof body.
	if rec.Code == 200 && strings.Contains(body, "heap profile:") {
		t.Fatalf("gate OFF: pprof leaked through with 200 + heap payload")
	}
}

func TestPprofGate_OnNonLoopbackBind_StillNotServed(t *testing.T) {
	// Safety net: even with the env gate ON, a (misconfigured) non-loopback
	// bind must never expose pprof.
	t.Setenv(EnvDebugPprof, "1")

	cfg := DefaultConfig()
	cfg.Bind = "0.0.0.0"
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	h := srv.routes()
	req := httptest.NewRequest("GET", "/debug/pprof/heap?debug=1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "heap profile:") {
		t.Fatalf("non-loopback bind must never serve pprof even with the gate ON, got: %s", body)
	}
}

func TestPprofGate_EmptyBind_FailsClosed(t *testing.T) {
	// Security hardening: an empty bind makes the listener bind ALL interfaces
	// (net.Listen("tcp", ":port")), NOT loopback — so isLoopbackBind must fail
	// closed and pprof MUST NOT be served even with the env gate ON.
	t.Setenv(EnvDebugPprof, "1")

	cfg := DefaultConfig()
	cfg.Bind = ""
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	if pprofEnabled(cfg.Bind) {
		t.Fatal("pprofEnabled(\"\") must be false — empty bind exposes all interfaces")
	}

	h := srv.routes()
	req := httptest.NewRequest("GET", "/debug/pprof/heap?debug=1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "heap profile:") {
		t.Fatalf("empty bind must never serve pprof even with the gate ON, got: %s", body)
	}
}
