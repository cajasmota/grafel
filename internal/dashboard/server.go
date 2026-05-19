package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"time"
)

// Server is an embedded HTTP dashboard. It is intentionally small: it
// composes a chi-style router by hand on top of net/http so we keep the
// stdlib-only constraint from the issue body.
type Server struct {
	cfg      Config
	registry RegistryStore
	graphs   *GraphCache
	hub      *wsHub
	listener net.Listener
	srv      *http.Server
	rng      *rand.Rand
}

// NewServer wires a server against the given config and registry-store
// adapter. Pass NewLiveStore() in production; tests pass an in-memory
// fake.
func NewServer(cfg Config, store RegistryStore) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("dashboard: nil RegistryStore")
	}
	h := newWSHub()
	go h.run()
	return &Server{
		cfg:      cfg,
		registry: store,
		graphs:   NewGraphCache(60 * time.Second),
		hub:      h,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}, nil
}

// Listen binds to a random free port within cfg.PortRange. It is
// separated from Serve so callers (and tests) can read back the chosen
// port before traffic starts flowing.
func (s *Server) Listen() (int, error) {
	const maxAttempts = 64
	span := s.cfg.PortRange.Max - s.cfg.PortRange.Min + 1
	tried := make(map[int]struct{}, maxAttempts)
	for i := 0; i < maxAttempts && len(tried) < span; i++ {
		port := s.cfg.PortRange.Min + s.rng.Intn(span)
		if _, seen := tried[port]; seen {
			continue
		}
		tried[port] = struct{}{}
		addr := net.JoinHostPort(s.cfg.Bind, strconv.Itoa(port))
		l, err := net.Listen("tcp", addr)
		if err == nil {
			s.listener = l
			return port, nil
		}
	}
	return 0, fmt.Errorf("dashboard: no free port in %d-%d after %d attempts",
		s.cfg.PortRange.Min, s.cfg.PortRange.Max, maxAttempts)
}

// Serve runs the HTTP server on the listener bound by Listen. It blocks
// until ctx is cancelled or http.Server returns a non-shutdown error.
func (s *Server) Serve(ctx context.Context) error {
	if s.listener == nil {
		return errors.New("dashboard: Serve called before Listen")
	}
	mux := s.routes()
	s.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		err := s.srv.Serve(s.listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// Addr returns the bound TCP address. Useful for tests that do not know
// the port up front.
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// routes builds the http.ServeMux for this server. Kept package-private so
// tests can hit handlers via httptest.NewServer(s.routes()) without going
// through Listen.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Static SPA. The embed root is "static/", strip that prefix so the
	// browser sees /index.html etc.
	sub, err := fs.Sub(staticFS, "static")
	if err == nil {
		mux.Handle("/", http.FileServer(http.FS(sub)))
	}

	// --- DASH-1 (legacy) endpoints ---
	mux.HandleFunc("GET /api/registry", s.handleListRegistry)
	mux.HandleFunc("GET /api/groups/{group}/graph", s.handleGroupGraph)
	mux.HandleFunc("GET /api/groups/{group}/repos/{repo}/graph", s.handleRepoGraph)
	mux.HandleFunc("POST /api/admin/groups", s.handleCreateGroup)
	mux.HandleFunc("POST /api/admin/groups/{group}/repos", s.handleAddRepo)

	// --- Phase 1 aggregator endpoints ---

	// First-paint aggregate
	mux.HandleFunc("GET /api/dashboard/init", s.handleDashboardInit)

	// LoD-aware graph
	mux.HandleFunc("GET /api/graph/{group}", s.handleGraph)
	mux.HandleFunc("GET /api/graph/{group}/entity/{id}", s.handleGraphEntity)

	// Process flows
	mux.HandleFunc("GET /api/flows/{group}", s.handleFlowsList)
	mux.HandleFunc("GET /api/flows/{group}/{processId}", s.handleFlowDetail)

	// API paths / contracts
	mux.HandleFunc("GET /api/paths/{group}", s.handlePathsList)
	mux.HandleFunc("GET /api/paths/{group}/{pathHash}", s.handlePathDetail)

	// Broker topology
	mux.HandleFunc("GET /api/topology/{group}", s.handleTopology)

	// Docs portal
	mux.HandleFunc("GET /api/docs/{group}", s.handleDocTree)
	mux.HandleFunc("GET /api/docs/{group}/{path...}", s.handleDocPage)

	// Global typeahead search
	mux.HandleFunc("GET /api/search/{group}", s.handleSearch)

	// Pattern store
	mux.HandleFunc("GET /api/patterns/{group}", s.handlePatterns)

	// Repair queue (admin)
	mux.HandleFunc("GET /api/repairs/{group}", s.handleRepairs)

	// Supporting endpoints
	mux.HandleFunc("GET /api/groups/{group}/communities", s.handleGroupCommunities)
	mux.HandleFunc("GET /api/groups/{group}/god-nodes", s.handleGroupGodNodes)
	mux.HandleFunc("GET /api/groups/{group}/links", s.handleGroupLinks)
	mux.HandleFunc("GET /api/groups/{group}/topics", s.handleGroupTopics)
	mux.HandleFunc("GET /api/source", s.handleSource)
	mux.HandleFunc("GET /api/findings", s.handleListFindings)

	// WebSocket push
	mux.HandleFunc("/ws/events", s.handleWSEvents)

	return s.withAuth(mux)
}

// withAuth wraps the mux with a bearer-token check when cfg.Auth.Enabled.
// Static asset routes (anything that does not start with /api/) are left
// open so the SPA shell can load before the user supplies credentials.
func (s *Server) withAuth(next http.Handler) http.Handler {
	if !s.cfg.Auth.Enabled {
		return next
	}
	expected := "Bearer " + s.cfg.Auth.Token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			if r.Header.Get("Authorization") != expected {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// writeJSON serializes v to w with the standard JSON content type. Errors
// during encoding are logged via the http.Server error log; the client
// will see a truncated body, which is the best stdlib can do.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr emits a uniform { "error": "..." } body.
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
