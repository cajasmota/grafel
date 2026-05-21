package dashboard

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"mime"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"sync"

	"github.com/cajasmota/archigraph/internal/audit"
	"github.com/cajasmota/archigraph/internal/jobs"
	"github.com/cajasmota/archigraph/internal/mcp"
	"github.com/cajasmota/archigraph/internal/notifications"
	"github.com/cajasmota/archigraph/internal/perf"
	"github.com/cajasmota/archigraph/internal/progress"
)

// Server is an embedded HTTP dashboard. It is intentionally small: it
// composes a chi-style router by hand on top of net/http so we keep the
// stdlib-only constraint from the issue body.
type Server struct {
	cfg             Config
	registry        RegistryStore
	graphs          *GraphCache
	hub             *wsHub
	listener        net.Listener
	srv             *http.Server
	rng             *rand.Rand
	daemonStartedAt time.Time // zero when not embedded inside a daemon process

	// progressBroker is the shared pub/sub bus for real-time indexing progress.
	// It is optional: when nil the /api/index-progress endpoints return 503.
	// Set via SetProgressBroker before calling Serve.
	progressBroker *progress.Broker

	// mcpActivityBroker is the fan-out bus for MCP tool call events (epic #1157).
	// Optional: when nil, /api/mcp-activity/stream returns 503.
	// Set via SetMCPActivityBroker before calling Serve.
	mcpActivityBroker *mcp.MCPActivityBroker

	// mcpActivityLog is the absolute path to the on-disk JSONL activity log.
	// Empty string disables the /api/mcp-activity/history endpoint.
	mcpActivityLog string

	// recallRunner is the function injected by cmd/archigraph that runs the
	// full indexer against a named golden fixture and returns the
	// quality.JSONReport as a JSON byte slice. Optional: when nil the
	// POST /api/quality/recall endpoint returns 503.
	// Set via SetRecallRunner before calling Serve.
	recallRunner func(fixtureName string) ([]byte, error)

	// jobQueue is the enrichment job dispatch queue (#1244). Optional: when nil
	// POST /api/enrichments/{group}/trigger returns 503 and the jobs list is empty.
	// Set via SetJobQueue before calling Serve.
	jobQueue *jobs.Queue

	// auditLog is the append-only disk sink for audit entries (#1258).
	// Optional: nil disables disk logging; the in-memory broker still works.
	// Set via SetAuditLog before calling Serve.
	auditLog *audit.Log

	// auditBroker fans audit entries to SSE subscribers (#1258).
	// Optional: nil disables /api/audit/stream (returns 503).
	// Set via SetAuditBroker before calling Serve.
	auditBroker *audit.Broker

	// auditor is the combined writer (disk + broker). Mutation handlers call
	// s.auditor.OK / s.auditor.Err. Initialised by SetAuditLog / SetAuditBroker.
	auditor *audit.Writer

	// watcher is the daemon's file watcher. Optional: when nil, the
	// POST /api/diagnostics/force-rescan endpoint returns 503.
	// Set via SetWatcher before Serve (#1270).
	watcher watcherForceRescan

	// perfRecorder is the lazy-init recorder for perf-history.jsonl (#1319).
	// Guarded by perfMu; initialised on first call to perfComponents().
	perfRecorder *perf.Recorder
	perfMu       sync.Mutex

	// webhookDispatcher fires quality-event notifications to configured URLs
	// after every rebuild (#1341). Optional: nil disables the test-ping
	// endpoints but does not prevent build or read-only webhook list routes.
	// Set via SetWebhookDispatcher before Serve.
	webhookDispatcher webhookDispatcherIface
}

// watcherForceRescan is the subset of the watch.Watcher surface used by
// the dashboard. The interface keeps the dashboard package free of a
// direct import of internal/daemon/watch.
type watcherForceRescan interface {
	ForceRescan()
	// Stats returns (repos, dirs, totalEvents, dropped).
	Stats() (int, int, uint64, uint64)
}

// webhookDispatcherIface is the subset of notifications.Dispatcher used by the
// dashboard. The narrow interface keeps server.go free of a direct import of
// the notifications package.
type webhookDispatcherIface interface {
	PostOnceForTest(cfg notifications.WebhookConfig, payload notifications.WebhookPayload) (int, error)
	FailureLog() []notifications.DeliveryFailure
	DispatchAll(cfgs []notifications.WebhookConfig, payload notifications.WebhookPayload)
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
	srv := &Server{
		cfg:      cfg,
		registry: store,
		graphs:   NewGraphCache(60 * time.Second),
		hub:      h,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	// auditor starts as a no-op writer; replaced when SetAuditLog/SetAuditBroker is called.
	srv.auditor = audit.NewWriter(nil, nil)
	return srv, nil
}

// SetDaemonStartedAt records when the parent daemon process started so
// the /api/info endpoint can report uptime. Call this from cmd/archigraph
// after the daemon's embedded server is wired up.
func (s *Server) SetDaemonStartedAt(t time.Time) {
	s.daemonStartedAt = t
}

// SetProgressBroker wires the shared indexer progress broker into the server
// so that /api/index-progress endpoints can stream live events. Call this from
// cmd/archigraph (or any daemon entrypoint) before Serve.
func (s *Server) SetProgressBroker(b *progress.Broker) {
	s.progressBroker = b
}

// SetMCPActivityBroker wires the MCP activity broker into the dashboard server
// so that /api/mcp-activity/stream can fan events to browser subscribers.
// Call from the daemon entrypoint before Serve (epic #1157, Phase 1).
func (s *Server) SetMCPActivityBroker(b *mcp.MCPActivityBroker) {
	s.mcpActivityBroker = b
}

// SetMCPActivityLog sets the path to the on-disk JSONL activity log. When set,
// /api/mcp-activity/history will read recent events from this file.
func (s *Server) SetMCPActivityLog(path string) {
	s.mcpActivityLog = path
}

// SetRecallRunner wires the quality recall function into the server so that
// POST /api/quality/recall can run the full indexer against a golden fixture.
// Call this from cmd/archigraph before Serve.
func (s *Server) SetRecallRunner(fn func(fixtureName string) ([]byte, error)) {
	s.recallRunner = fn
}

// SetJobQueue wires the enrichment job queue into the dashboard server so
// that POST /api/enrichments/{group}/trigger dispatches real jobs and
// GET /api/enrichments/{group}/jobs returns live status.
// Call this from cmd/archigraph (or any daemon entrypoint) before Serve.
func (s *Server) SetJobQueue(q *jobs.Queue) {
	s.jobQueue = q
}

// SetAuditLog wires the audit disk log into the dashboard server.
// Call from the daemon entrypoint before Serve (#1258).
func (s *Server) SetAuditLog(l *audit.Log) {
	s.auditLog = l
	s.auditor = audit.NewWriter(s.auditLog, s.auditBroker)
}

// SetAuditBroker wires the audit SSE broker into the dashboard server.
// Call from the daemon entrypoint before Serve (#1258).
func (s *Server) SetAuditBroker(b *audit.Broker) {
	s.auditBroker = b
	s.auditor = audit.NewWriter(s.auditLog, s.auditBroker)
}

// SetWatcher wires the daemon's file watcher into the dashboard so that
// POST /api/diagnostics/force-rescan can trigger a full diff reconciliation.
// The parameter accepts any value that implements ForceRescan — in production
// this is always a *watch.Watcher. Call before Serve (#1270).
func (s *Server) SetWatcher(w watcherForceRescan) {
	s.watcher = w
}

// SetWebhookDispatcher wires the notification dispatcher into the dashboard so
// that POST /api/webhooks/test and POST /api/webhooks/{id}/test can send real
// pings, and GET /api/webhooks/failures can report delivery errors.
// Call from the daemon entrypoint before Serve (#1341).
func (s *Server) SetWebhookDispatcher(d webhookDispatcherIface) {
	s.webhookDispatcher = d
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

// UseListener hands an already-bound TCP listener to the server. This is
// used by the daemon integration (#929): the daemon binds port 47274 at
// startup and passes the listener in rather than calling Listen().
// Calling Listen() after UseListener returns an error.
func (s *Server) UseListener(l net.Listener) {
	s.listener = l
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

	// Static SPA. The embed root is "dist/", strip that prefix so the
	// browser sees /index.html etc.
	// Unknown paths (SPA client-side routes) fall through to index.html.
	sub, err := fs.Sub(staticFS, "dist")
	if err == nil {
		mux.Handle("/", spaHandler(sub))
	}

	// --- DASH-1 (legacy) endpoints ---
	mux.HandleFunc("GET /api/registry", s.handleListRegistry)
	mux.HandleFunc("GET /api/groups/{group}/graph", s.handleGroupGraph)
	mux.HandleFunc("GET /api/groups/{group}/repos/{repo}/graph", s.handleRepoGraph)
	mux.HandleFunc("POST /api/admin/groups", s.handleCreateGroup)
	mux.HandleFunc("POST /api/admin/groups/{group}/repos", s.handleAddRepo)

	// Repo Manifest viewer (#1351) — surface AGENTS.md + .archigraph/ state per repo.
	// NOTE: the /manifest literal segment must be registered before the /graph wildcard
	// so Go 1.22 ServeMux prefers the more-specific path.
	mux.HandleFunc("GET /api/groups/{group}/repos/{repo}/manifest", s.handleRepoManifest)
	mux.HandleFunc("POST /api/groups/{group}/repos/{repo}/manifest/refresh", s.handleRepoManifestRefresh)

	// --- Phase 1 aggregator endpoints ---

	// First-paint aggregate
	mux.HandleFunc("GET /api/dashboard/init", s.handleDashboardInit)

	// Three-tier graph: compact render payload, labels, entity detail
	mux.HandleFunc("GET /api/graph/{group}", s.handleGraph)
	mux.HandleFunc("GET /api/graph/{group}/labels", s.handleGraphLabels)
	mux.HandleFunc("GET /api/graph/{group}/entity/{id}", s.handleGraphEntity)
	// Landing card thumbnail — top-N node positions for inline SVG preview (#983)
	mux.HandleFunc("GET /api/graph/{group}/layout-snapshot", s.handleGraphLayoutSnapshot)

	// Process flows
	mux.HandleFunc("GET /api/flows/{group}", s.handleFlowsList)
	mux.HandleFunc("GET /api/flows/{group}/dead-ends", s.handleFlowDeadEnds)
	mux.HandleFunc("GET /api/flows/{group}/truncated", s.handleFlowTruncated)
	mux.HandleFunc("GET /api/flows/{group}/{processId}", s.handleFlowDetail)
	mux.HandleFunc("POST /api/flows/{group}/{processId}/trigger-enrichment", s.handleTriggerEnrichment)

	// API paths / contracts
	mux.HandleFunc("GET /api/paths/{group}", s.handlePathsList)
	mux.HandleFunc("GET /api/paths/{group}/orphan-callers", s.handleOrphanCallers)
	mux.HandleFunc("GET /api/paths/{group}/{pathHash}", s.handlePathDetail)

	// Broker topology
	mux.HandleFunc("GET /api/topology/{group}", s.handleTopology)
	mux.HandleFunc("GET /api/topology/{group}/topic/{topicId}", s.handleTopicDetail)
	mux.HandleFunc("GET /api/topology/{group}/orphan-publishers", s.handleOrphanPublishers)
	mux.HandleFunc("GET /api/topology/{group}/orphan-subscribers", s.handleOrphanSubscribers)

	// Docs portal
	mux.HandleFunc("GET /api/docs/{group}", s.handleDocTree)
	mux.HandleFunc("GET /api/docs/{group}/{path...}", s.handleDocPage)

	// Global typeahead search
	mux.HandleFunc("GET /api/search/{group}", s.handleSearch)

	// Pattern store — full CRUD + GC + export (#1189)
	mux.HandleFunc("GET /api/patterns/{group}", s.handlePatterns)
	mux.HandleFunc("GET /api/patterns/{group}/{id}", s.handlePatternDetail)
	mux.HandleFunc("PUT /api/patterns/{group}/{id}", s.handlePatternUpdate)
	mux.HandleFunc("DELETE /api/patterns/{group}/{id}", s.handlePatternDelete)
	mux.HandleFunc("POST /api/patterns/{group}/gc", s.handlePatternGC)
	mux.HandleFunc("POST /api/patterns/{group}/export", s.handlePatternExport)

	// Pending queue — repair candidates + enrichment candidates (#987)
	mux.HandleFunc("GET /api/repairs/{group}", s.handleRepairs)
	mux.HandleFunc("GET /api/enrichments/{group}", s.handleEnrichments)
	// Community-naming queue — separated from entity enrichment (#1301)
	mux.HandleFunc("GET /api/community-naming/{group}", s.handleCommunityNaming)
	// Aggregated enrichment-task view — 1 task per entity with N actions (#1134)
	mux.HandleFunc("GET /api/enrichments/{group}/tasks", s.handleEnrichmentTasks)
	// Candidate apply/reject actions (#1016)
	mux.HandleFunc("POST /api/repairs/{group}/action", s.handleRepairAction)
	mux.HandleFunc("POST /api/enrichments/{group}/action", s.handleEnrichmentAction)
	// Enrichment job dispatch (#1244) — trigger, list, cancel
	mux.HandleFunc("POST /api/enrichments/{group}/trigger", s.handleEnrichmentTrigger)
	mux.HandleFunc("GET /api/enrichments/{group}/jobs", s.handleEnrichmentJobs)
	mux.HandleFunc("POST /api/enrichments/{group}/jobs/{jobId}/cancel", s.handleEnrichmentJobCancel)
	// Batched enrichment (#1285) — N candidates → N jobs in one round-trip
	mux.HandleFunc("POST /api/enrichments/{group}/batch-enrich", s.handleEnrichmentBatch)
	// Per-tier enrichment progress — polled every 3 s by the /pending surface (#1286)
	mux.HandleFunc("GET /api/enrichments/{group}/progress", s.handleEnrichmentProgress)
	// Agent description write-back — persists generated descriptions to graph + frontmatter (#1304)
	mux.HandleFunc("POST /api/enrichments/{group}/write", s.handleEnrichmentWriteback)
	// Pre-run cost estimator — shows token/USD estimate before batch enrichment (#1287)
	mux.HandleFunc("GET /api/enrichments/{group}/estimate", s.handleEnrichmentEstimate)

	// Settings surface (#1206)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handlePutSettings)
	mux.HandleFunc("POST /api/settings/reset", s.handleResetSettings)

	// Webhook notifications (#1341)
	mux.HandleFunc("GET /api/webhooks", s.handleListWebhooks)
	mux.HandleFunc("POST /api/webhooks", s.handleCreateWebhook)
	// NOTE: /api/webhooks/test must be registered before /api/webhooks/{id}
	// so that the static path wins the Go 1.22 pattern-match precedence.
	mux.HandleFunc("POST /api/webhooks/test", s.handleTestWebhookAdhoc)
	mux.HandleFunc("GET /api/webhooks/failures", s.handleWebhookFailures)
	mux.HandleFunc("PUT /api/webhooks/{id}", s.handleUpdateWebhook)
	mux.HandleFunc("DELETE /api/webhooks/{id}", s.handleDeleteWebhook)
	mux.HandleFunc("POST /api/webhooks/{id}/test", s.handleTestWebhookByID)

	// Build / version info
	mux.HandleFunc("GET /api/info", s.handleInfo)

	// Diagnostics (#1187, #1270)
	mux.HandleFunc("GET /api/diagnostics", s.handleDiagnostics)
	mux.HandleFunc("POST /api/diagnostics/kill-stale", s.handleDiagnosticsKillStale)
	mux.HandleFunc("POST /api/diagnostics/force-rescan", s.handleDiagnosticsForceRescan)

	// System / daemon control panel (#1195)
	mux.HandleFunc("GET /api/system", s.handleSystem)
	mux.HandleFunc("POST /api/system/restart", s.handleSystemRestart)
	mux.HandleFunc("POST /api/system/stop", s.handleSystemStop)
	mux.HandleFunc("GET /api/system/logs", s.handleSystemLogs)

	// Update surface (#1199) — check, apply, refresh-rules
	mux.HandleFunc("GET /api/updates/check", s.handleUpdatesCheck)
	mux.HandleFunc("POST /api/updates/apply", s.handleUpdatesApply)
	mux.HandleFunc("POST /api/updates/refresh-rules", s.handleUpdatesRefreshRules)

	// Maintenance ops — rebuild / reset / cleanup (#1200)
	mux.HandleFunc("POST /api/groups/{group}/rebuild", s.handleGroupRebuild)
	mux.HandleFunc("POST /api/groups/{group}/repos/{repo}/rebuild", s.handleRepoRebuild)
	mux.HandleFunc("POST /api/groups/{group}/reset", s.handleGroupReset)
	mux.HandleFunc("POST /api/groups/{group}/repos/{repo}/reset", s.handleRepoReset)
	mux.HandleFunc("GET /api/cleanup/preview", s.handleCleanupPreview)
	mux.HandleFunc("POST /api/cleanup", s.handleCleanup)

	// Quality surface (#1198, #1236)
	mux.HandleFunc("GET /api/quality/orphans/{group}", s.handleQualityOrphans)
	mux.HandleFunc("GET /api/quality/fixtures", s.handleQualityFixtures)
	mux.HandleFunc("POST /api/quality/recall", s.handleQualityRecall)
	mux.HandleFunc("GET /api/quality/composite/{group}", s.handleQualityComposite)
	// #1313: N+1 query anti-pattern detector.
	mux.HandleFunc("GET /api/quality/anti-patterns/{group}", s.handleNPlusOne)
	// #1323: test-coverage graph — link Test entities to production code.
	mux.HandleFunc("GET /api/quality/coverage/{group}", s.handleQualityCoverage)
	// #1322: hardcoded secret detector.
	mux.HandleFunc("GET /api/quality/secrets/{group}", s.handleQualitySecrets)

	// #1330: Security & Quality surface — auth coverage, secrets, cycles.
	mux.HandleFunc("GET /api/security/auth-coverage/{group}", s.handleSecurityAuthCoverage)
	mux.HandleFunc("GET /api/security/secrets/{group}", s.handleSecuritySecrets)
	mux.HandleFunc("GET /api/security/cycles/{group}", s.handleSecurityCycles)

	// Supporting endpoints
	mux.HandleFunc("GET /api/groups/{group}/communities", s.handleGroupCommunities)
	mux.HandleFunc("GET /api/groups/{group}/god-nodes", s.handleGroupGodNodes)
	mux.HandleFunc("GET /api/groups/{group}/links", s.handleGroupLinks)
	mux.HandleFunc("GET /api/groups/{group}/topics", s.handleGroupTopics)
	mux.HandleFunc("GET /api/source", s.handleSource)
	mux.HandleFunc("GET /api/findings", s.handleListFindings)

	// WebSocket push
	mux.HandleFunc("/ws/events", s.handleWSEvents)

	// SSE progress streams (Sub-C of epic #1118)
	mux.HandleFunc("GET /api/index-progress", s.handleIndexProgressAll)
	mux.HandleFunc("GET /api/index-progress/{group}", s.handleIndexProgressGroup)

	// MCP activity stream + history (Phase 1 of epic #1157 — Jarvis)
	mux.HandleFunc("GET /api/mcp-activity/stream", s.handleMCPActivityStream)
	mux.HandleFunc("GET /api/mcp-activity/history", s.handleMCPActivityHistory)

	// Surface 11 — Quality history (#1214) + per-metric trends (#1329)
	mux.HandleFunc("GET /api/quality/history/{group}", s.handleQualityHistory)
	mux.HandleFunc("GET /api/quality/trends/{group}", s.handleQualityTrends)

	// Surface 12 — Web onboarding wizard (#1239)
	mux.HandleFunc("POST /api/onboard/check-path", s.handleOnboardCheckPath)
	mux.HandleFunc("POST /api/onboard/detect-monorepo", s.handleOnboardDetectMonorepo)
	mux.HandleFunc("POST /api/onboard/create-group", s.handleOnboardCreateGroup)

	// Surface 13 — Audit log (#1258)
	mux.HandleFunc("GET /api/audit", s.handleAuditHistory)
	mux.HandleFunc("GET /api/audit/stream", s.handleAuditStream)
	mux.HandleFunc("GET /api/audit/export", s.handleAuditExport)

	// Surface 14 — Indexer error registry (#1268)
	mux.HandleFunc("GET /api/indexer-errors", s.handleIndexerErrors)

	// MCP Setup Wizard (#1247) — one-click install / uninstall / verify
	mux.HandleFunc("GET /api/mcp-setup/hosts", s.handleMCPSetupHosts)
	mux.HandleFunc("POST /api/mcp-setup/install", s.handleMCPSetupInstall)
	mux.HandleFunc("POST /api/mcp-setup/uninstall", s.handleMCPSetupUninstall)
	mux.HandleFunc("POST /api/mcp-setup/verify", s.handleMCPSetupVerify)

	// Performance budget monitor (#1319) — track index time, query p95, RSS
	mux.HandleFunc("GET /api/perf/budgets", s.handlePerfBudgets)
	mux.HandleFunc("POST /api/perf/record", s.handlePerfRecord)

	// OpenAPI 3.0 export — HTTP endpoints → openapi.yaml / openapi.json (#1340)
	// The literal segment "openapi" is more specific than the DSL wildcard
	// {entity_id}/{format}, so Go 1.22+ ServeMux routes here first.
	mux.HandleFunc("GET /api/export/{group}/openapi", s.handleExportOpenAPI)

	// DSL export — Graph subgraph → Mermaid / Graphviz / PlantUML / D2 (#1318)
	mux.HandleFunc("GET /api/export/{group}/{entity_id}/{format}", s.handleExportDSL)

	// Surface 15 — External dependency graph (#1321)
	// GET /api/dependencies/{group}  — declared + used/unused/phantom classification
	mux.HandleFunc("GET /api/dependencies/{group}", s.handleDependencies)

	// #1345: Architectural fitness functions — user-defined rules with CI gates.
	mux.HandleFunc("GET /api/fitness/{group}", s.handleFitness)

	// #1353: Graph snapshots — save, list, delete, diff graph state across time.
	// NOTE: /api/snapshots/{group}/{id}/diff must be registered before
	// /api/snapshots/{group}/{id} so the static suffix wins Go 1.22 precedence.
	mux.HandleFunc("POST /api/snapshots/{group}", s.handleSaveSnapshot)
	mux.HandleFunc("GET /api/snapshots/{group}", s.handleListSnapshots)
	mux.HandleFunc("GET /api/snapshots/{group}/{id}/diff", s.handleSnapshotDiff)
	mux.HandleFunc("DELETE /api/snapshots/{group}/{id}", s.handleDeleteSnapshot)

	return s.withAuth(withGzip(mux))
}

// spaHandler returns an http.Handler that serves static files from fsys.
// For requests whose path does not match an existing file the handler
// falls back to index.html so the React Router can take over on the
// client side. Hashed assets (e.g. main-abc123.js) receive a long-lived
// immutable cache header; everything else gets no-cache.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip leading slash for fs.Stat lookup.
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}

		// Check whether the file exists in the embed.
		if _, err := fs.Stat(fsys, p); err == nil {
			// Apply cache headers before the file server writes them.
			if isCacheable(p) {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache, must-revalidate")
			}
			// Ensure proper MIME type for JavaScript modules (important in
			// some browsers when served from embed.FS).
			if ext := path.Ext(p); ext != "" {
				if mt := mime.TypeByExtension(ext); mt != "" {
					w.Header().Set("Content-Type", mt)
				}
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// Unknown path — serve index.html for SPA client-side routing.
		// Rewrite the request to / so the file server picks up index.html.
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}

// isCacheable reports whether a static asset path carries a content hash
// and can be served with a long-lived immutable cache directive. Vite
// generates hashed names for JS/CSS chunks (e.g. index-BcDe1234.js).
func isCacheable(p string) bool {
	ext := path.Ext(p)
	switch ext {
	case ".js", ".css", ".woff", ".woff2", ".ttf", ".eot":
		base := strings.TrimSuffix(path.Base(p), ext)
		// Vite hashes are 8 hex chars separated by a dash.
		if idx := strings.LastIndex(base, "-"); idx >= 0 {
			suffix := base[idx+1:]
			if len(suffix) >= 6 {
				return true
			}
		}
	}
	return false
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

// withGzip wraps next with transparent gzip compression for clients that
// send Accept-Encoding: gzip.  Only compresses JSON API responses;
// static assets and SSE/WebSocket streams are always passed through as-is.
//
// Perf (#1249): on a 100k-node graph the JSON payload is ~8-12 MiB uncompressed.
// gzip at the default level reduces it to ~1-2 MiB, cutting LAN transfer time
// by ~6x and loopback time by ~3x.  The compression cost (~40 ms at 100k nodes)
// is amortized by staleTime=5min caching in the React Query layer.
//
// SSE and WebSocket paths are excluded: they are streaming protocols that require
// an unbuffered, unflushed write path and must never be gzip-compressed.
func withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		// Only compress API JSON — static assets have their own cache pipeline.
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		// Never compress SSE streams or WebSocket upgrades — they are long-lived
		// streaming connections that write incrementally and must not be buffered.
		if strings.HasSuffix(r.URL.Path, "/stream") ||
			strings.Contains(r.URL.Path, "index-progress") ||
			strings.Contains(r.URL.Path, "mcp-activity") ||
			r.Header.Get("Upgrade") == "websocket" {
			next.ServeHTTP(w, r)
			return
		}
		gz, err := gzip.NewWriterLevel(w, gzip.DefaultCompression)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		defer gz.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length") // length changes after compression
		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, Writer: gz}, r)
	})
}

// gzipResponseWriter wraps http.ResponseWriter so Write goes to the gzip stream.
type gzipResponseWriter struct {
	http.ResponseWriter
	Writer *gzip.Writer
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	return g.Writer.Write(b)
}
