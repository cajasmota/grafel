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

	"github.com/cajasmota/grafel/internal/audit"
	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/jobs"
	"github.com/cajasmota/grafel/internal/mcp"
	"github.com/cajasmota/grafel/internal/notifications"
	"github.com/cajasmota/grafel/internal/perf"
	"github.com/cajasmota/grafel/internal/progress"
	"github.com/cajasmota/grafel/internal/registry"
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

	// historyRoot is the directory containing health-history.jsonl.
	// Defaults to daemon.DefaultLayout().Root; injectable for tests.
	historyRoot string

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

	// recallRunner is the function injected by cmd/grafel that runs the
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

	// actionJobs tracks async v2 action jobs (rebuild/reset) so the
	// Operations + Settings screens can poll/stream their status without the
	// triggering HTTP handler ever blocking on the work (#1512). Always
	// non-nil; initialised in NewServer.
	actionJobs *actionJobRegistry

	// rebuildRunner, when non-nil, replaces the default daemon-RPC rebuild path
	// used by the v2 async rebuild/reset endpoints (#1512). Test-only injection
	// point so the async job lifecycle can run without a live daemon.
	rebuildRunner rebuildRunner

	// tierQuerier provides HOT/WARM/COLD tier status for the
	// GET /api/v2/groups/:group/refs endpoint (PH2 of epic #2087 #2090).
	// Optional: when nil, the endpoint falls back to the pre-PH2 hot/cold
	// heuristic (current ref = hot, others = cold).
	tierQuerier TierQuerier

	// worktreeQuerier reports which (repoPath, ref) pairs originated from a
	// linked git worktree (PH3 of epic #2087 #2091). Optional: when nil,
	// all refs report source="branch".
	worktreeQuerier WorktreeQuerier

	// watcherQuerier provides watcher pause/resume state for the
	// GET /api/v2/groups/:group/refs endpoint (PH2a of epic #2087 #2096).
	// Optional: when nil, watcher_state is reported as "unknown".
	watcherQuerier WatcherQuerier

	// applyToolDelta is the in-process tool-selection delta applier used by
	// the tools settings PUT handler (#5257). Defaults to
	// install.ApplyToolDelta; injectable in tests so the handler never touches
	// the real (live) machine's rules files / MCP registrations.
	applyToolDelta applyToolDeltaFunc
}

// applyToolDeltaFunc matches install.ApplyToolDelta's signature so the tools
// settings handler can be tested with a mock that records the delta and never
// writes to the live filesystem (#5257).
type applyToolDeltaFunc func(cfg *registry.GroupConfig, group, binPath string, prevTools, nextTools []string, ops *install.ToolDeltaOps) (*install.ToolDeltaResult, error)

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
		cfg:        cfg,
		registry:   store,
		graphs:     NewGraphCache(60 * time.Second),
		hub:        h,
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
		actionJobs: newActionJobRegistry(),
	}
	// auditor starts as a no-op writer; replaced when SetAuditLog/SetAuditBroker is called.
	srv.auditor = audit.NewWriter(nil, nil)
	return srv, nil
}

// daemonRoot returns the daemon root directory used for reading
// health-history.jsonl. Uses historyRoot when set (for tests); falls back
// to daemon.DefaultLayout().Root at runtime.
func (s *Server) daemonRoot() string {
	if s.historyRoot != "" {
		return s.historyRoot
	}
	layout, err := daemon.DefaultLayout()
	if err != nil {
		return ""
	}
	return layout.Root
}

// SetDaemonStartedAt records when the parent daemon process started so
// the /api/info endpoint can report uptime. Call this from cmd/grafel
// after the daemon's embedded server is wired up.
func (s *Server) SetDaemonStartedAt(t time.Time) {
	s.daemonStartedAt = t
}

// InvalidateGroup drops the dashboard's in-memory graph state for a single
// group, releasing its materialised *graph.Document slices, re-derived Pass-4
// algorithm results, mmap readers, and pre-built search index back to the GC.
// The next request for the group lazily reloads it from disk.
//
// #5238: this is the eviction lever the tier manager calls on a WARM→COLD
// transition. The MCP graph cache already drops its (cheap, mmap'd) reader on
// COLD, but the dashboard GraphCache previously only aged out via its own
// access-driven TTL — an idle group that was never re-requested held its full
// materialised Document on the heap indefinitely. Wiring this into the tier
// demotion lets a cold group's derived heap be reclaimed promptly.
//
// Safe to call for an unknown / not-yet-loaded group (no-op).
func (s *Server) InvalidateGroup(group string) {
	if s == nil || s.graphs == nil {
		return
	}
	s.graphs.Invalidate(group)
}

// SetProgressBroker wires the shared indexer progress broker into the server
// so that /api/index-progress endpoints can stream live events. Call this from
// cmd/grafel (or any daemon entrypoint) before Serve.
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
// Call this from cmd/grafel before Serve.
func (s *Server) SetRecallRunner(fn func(fixtureName string) ([]byte, error)) {
	s.recallRunner = fn
}

// SetJobQueue wires the enrichment job queue into the dashboard server so
// that POST /api/enrichments/{group}/trigger dispatches real jobs and
// GET /api/enrichments/{group}/jobs returns live status.
// Call this from cmd/grafel (or any daemon entrypoint) before Serve.
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

// TierQuerier is the narrow interface the dashboard uses to read the
// tiered-hibernation state (PH2 of epic #2087 / issue #2090). The narrow
// interface keeps the dashboard package free of a direct dependency on
// internal/daemon/tier.
type TierQuerier interface {
	// TierForRef returns the tier string ("hot"/"warm"/"cold"/"expired")
	// for the given (repoPath, ref) pair. Returns "cold" for unknown slots.
	TierForRef(repoPath, ref string) string
}

// SetTierQuerier wires the PH2 tiered-hibernation state machine so that
// GET /api/v2/groups/:group/refs returns real HOT/WARM/COLD status. Call
// from the daemon entrypoint before Serve.
func (s *Server) SetTierQuerier(q TierQuerier) {
	s.tierQuerier = q
}

// WorktreeQuerier is the narrow interface the dashboard uses to identify
// which (repoPath, ref) pairs originated from a linked git worktree (PH3
// of epic #2087 #2091). The narrow interface avoids a direct import of
// internal/daemon/worktree.
type WorktreeQuerier interface {
	// IsWorktreeRef returns true when the (repoPath, ref) pair corresponds to
	// a linked git worktree discovered by the PH3 watcher.
	IsWorktreeRef(repoPath, ref string) bool
}

// SetWorktreeQuerier wires the PH3 worktree store so that
// GET /api/v2/groups/:group/refs can annotate worktree-derived refs with
// source="worktree". Call from the daemon entrypoint before Serve.
func (s *Server) SetWorktreeQuerier(q WorktreeQuerier) {
	s.worktreeQuerier = q
}

// WatcherQuerier is the narrow interface the dashboard uses to read the
// PH2a watcher pause/resume state. The narrow interface keeps the dashboard
// package free of a direct dependency on internal/daemon/watch.
// PH2a of epic #2087 (#2096).
type WatcherQuerier interface {
	// IsPaused reports whether the fsnotify subscription for (repoPath, ref)
	// is currently paused.
	IsPaused(repoPath, ref string) bool
}

// SetWatcherQuerier wires the PH2a watcher state so that
// GET /api/v2/groups/:group/refs returns watcher_state: "active"|"paused".
// Call from the daemon entrypoint after onWatcherReady.
func (s *Server) SetWatcherQuerier(q WatcherQuerier) {
	s.watcherQuerier = q
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

	// Repo Manifest viewer (#1351) — surface AGENTS.md + .grafel/ state per repo.
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

	// API event-flows (#1944 Phase 1) — multi-hop pub/sub chains seeded
	// by channels (SCOPE.MessageTopic / SCOPE.EventBusEvent). Reuses the
	// Flows DAG renderer on the frontend; this surface emits the same
	// chain/branches_dag contract as /api/flows.
	mux.HandleFunc("GET /api/event-flows/{group}", s.handleEventFlowsList)
	mux.HandleFunc("GET /api/event-flows/{group}/{eventFlowId}", s.handleEventFlowDetail)

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

	// Skills surface (#1354) — list installed, browse marketplace, install/remove
	mux.HandleFunc("GET /api/skills/installed", s.handleSkillsInstalled)
	mux.HandleFunc("GET /api/skills/available", s.handleSkillsAvailable)
	mux.HandleFunc("POST /api/skills/install", s.handleSkillsInstall)
	mux.HandleFunc("POST /api/skills/uninstall", s.handleSkillsUninstall)

	// Maintenance ops — rebuild / reset / cleanup (#1200)
	mux.HandleFunc("POST /api/groups/{group}/rebuild", s.handleGroupRebuild)
	mux.HandleFunc("POST /api/groups/{group}/repos/{repo}/rebuild", s.handleRepoRebuild)
	mux.HandleFunc("POST /api/groups/{group}/reset", s.handleGroupReset)
	mux.HandleFunc("POST /api/groups/{group}/repos/{repo}/reset", s.handleRepoReset)
	mux.HandleFunc("GET /api/cleanup/preview", s.handleCleanupPreview)
	mux.HandleFunc("POST /api/cleanup", s.handleCleanup)

	// Quality surface (#1198, #1236)
	mux.HandleFunc("GET /api/quality/orphans/{group}", s.handleQualityOrphans)
	mux.HandleFunc("POST /api/quality/orphans/{group}", s.handleRunQualityOrphans)
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

	// #4255 (epic #4249): GraphQL resolver-effects surface.
	mux.HandleFunc("GET /api/graphql/{group}", s.handleGraphQL)

	// #4256 (epic #4249): IaC / Infrastructure surface — resources grouped by
	// tool with typed config props + grant/event-source/dependency/topology edges.
	mux.HandleFunc("GET /api/iac/{group}", s.handleIaC)

	// #4265 (epic #4249): Data-flow & Taint surface — request-input → sink taint
	// flows (DATA_FLOWS_TO) + ranked security findings (source→sink paths).
	mux.HandleFunc("GET /api/dataflow/{group}", s.handleDataflow)

	// #4266 (epic #4249): Dependency-Injection surface — providers grouped by
	// framework, each listing the consumers it is INJECTED_INTO (NestJS/Spring/
	// Angular/… DI).
	mux.HandleFunc("GET /api/di/{group}", s.handleDI)

	// #4267 (epic #4249): Error-flow surface — exception types rolled up across
	// the group, each listing its THROWS (throwers) and CATCHES (catchers) with
	// an honest uncaught flag (thrown-but-no-typed-catcher-in-graph).
	mux.HandleFunc("GET /api/errorflow/{group}", s.handleErrorFlow)

	// Supporting endpoints
	mux.HandleFunc("GET /api/groups/{group}/communities", s.handleGroupCommunities)
	mux.HandleFunc("GET /api/groups/{group}/god-nodes", s.handleGroupGodNodes)
	mux.HandleFunc("GET /api/groups/{group}/links", s.handleGroupLinks)
	mux.HandleFunc("GET /api/groups/{group}/topics", s.handleGroupTopics)
	mux.HandleFunc("GET /api/source", s.handleSource)
	mux.HandleFunc("GET /api/findings", s.handleListFindings)

	// Multi-ref surface (#2220) — 6 read-only endpoints + /refs listing.
	// All accept ?ref=<name>|@all|@current (missing ?ref= == current HEAD).
	// NOTE: /refs must be registered before the wildcard /{repo}/... below so
	// Go 1.22 ServeMux picks the more-specific static path first.
	mux.HandleFunc("GET /api/groups/{group}/refs", s.handleGroupRefs)
	mux.HandleFunc("GET /api/groups/{group}/stats", s.handleGroupStats)
	mux.HandleFunc("GET /api/groups/{group}/repos/{repo}/entities", s.handleRepoEntities)
	mux.HandleFunc("GET /api/groups/{group}/repos/{repo}/relationships", s.handleRepoRelationships)
	mux.HandleFunc("GET /api/groups/{group}/repos/{repo}/cross-repo-edges", s.handleRepoCrossRepoEdges)
	mux.HandleFunc("GET /api/groups/{group}/repos/{repo}/orphans", s.handleRepoOrphans)
	mux.HandleFunc("GET /api/groups/{group}/repos/{repo}/patterns", s.handleRepoPatterns)

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

	// --- API v2 routes (coexist with v1; v1 routes above are UNCHANGED) ---
	// Bootstrap — called once by WebUI v2 on mount to discover daemon version
	// and registered groups.
	mux.HandleFunc("GET /api/v2/meta", s.handleV2Meta)

	// Landing screen — rich group list + create-group.
	mux.HandleFunc("GET /api/v2/groups", s.handleV2Groups)
	mux.HandleFunc("POST /api/v2/groups", s.handleV2CreateGroup)

	// Create-group / add-repo scan→detect→index wizard (#1517).
	// scan/inspect detects stack + monorepo (no writes); from-scan creates a
	// group + registers repos + enqueues an async index job; repos/scan does
	// the same for an existing group. All index steps return a 202 JobAck the
	// wizard streams via /api/v2/jobs/{id}/stream.
	mux.HandleFunc("POST /api/v2/scan/inspect", s.handleV2ScanInspect)
	mux.HandleFunc("POST /api/v2/groups/from-scan", s.handleV2CreateGroupFromScan)
	mux.HandleFunc("POST /api/v2/groups/{group}/repos/scan", s.handleV2ScanRepos)
	// fs/list powers the ScanWizard's server-side folder browser (#1529): the
	// daemon lists its OWN filesystem so picking a folder yields its absolute
	// path — no manual paste required.
	mux.HandleFunc("GET /api/v2/fs/list", s.handleV2FsList)

	// Generated-markdown docs portal — WebUI v2 (#1552)
	mux.HandleFunc("GET /api/v2/groups/{group}/docs/tree", s.handleV2DocsTree)
	mux.HandleFunc("GET /api/v2/groups/{group}/docs/page", s.handleV2DocsPage)
	// Docs export (#1624): streams an archive of a group's generated docs.
	// Extensible by format (zip first) + kind (all|technical|business).
	mux.HandleFunc("GET /api/v2/groups/{group}/docs/export", s.handleV2DocsExport)
	// Graph export + import (#1627): streams an archive of the indexed store
	// (graph.fb, enrichments, links, embeddings, fleet config); import accepts
	// such an archive and restores the group. Extensible by format + kind.
	// NOTE: registered BEFORE the wildcard /api/v2/groups/{group} GET below so
	// Go 1.22 ServeMux picks the more-specific path first.
	mux.HandleFunc("POST /api/v2/groups/import", s.handleV2GraphImport)
	mux.HandleFunc("GET /api/v2/groups/{group}/export", s.handleV2GraphExport)
	// Graph — the WebUI v2 hero surface payload (nodes/edges/communities/repos).
	// Carries pagerank + source_file for cosmos.gl node sizing + module group-by.
	// PH1c (#2087): accepts ?ref= to query a specific git ref's graph.
	mux.HandleFunc("GET /api/v2/graph/{group}", s.handleV2Graph)

	// PH1c (#2087): list available refs (branches/tags) for each repo in the group.
	// Returns which refs have an indexed graph on disk and their tier (hot/cold).
	mux.HandleFunc("GET /api/v2/groups/{group}/refs", s.handleV2GroupRefs)

	// PH5 (#2093): graph diff — compare two indexed refs for a single repo.
	// GET /api/v2/groups/{group}/repos/{repo}/diff?refA=main&refB=feat%2Fx
	mux.HandleFunc("GET /api/v2/groups/{group}/repos/{repo}/diff", s.handleV2RepoDiff)

	// Settings screen — per-group management surface (#1436, epic #1432).
	mux.HandleFunc("GET /api/v2/groups/{group}", s.handleV2GetGroup)
	mux.HandleFunc("PATCH /api/v2/groups/{group}/features", s.handleV2PatchFeatures)
	mux.HandleFunc("PATCH /api/v2/groups/{group}/docs", s.handleV2PatchDocs)
	mux.HandleFunc("DELETE /api/v2/groups/{group}", s.handleV2DeleteGroup)
	mux.HandleFunc("POST /api/v2/groups/{group}/repos", s.handleV2AddRepo)
	mux.HandleFunc("DELETE /api/v2/groups/{group}/repos/{repo}", s.handleV2RemoveRepo)
	mux.HandleFunc("PATCH /api/v2/groups/{group}/repos/{repo}/monorepo", s.handleV2PatchMonorepo)
	mux.HandleFunc("POST /api/v2/groups/{group}/doctor", s.handleV2Doctor)

	// AI-tool selection settings (#5257, epic #5252): per-group enable/disable
	// of the install targets, applied in-process via install.ApplyToolDelta.
	mux.HandleFunc("GET /api/v2/groups/{group}/tools", s.handleV2GetTools)
	mux.HandleFunc("PUT /api/v2/groups/{group}/tools", s.handleV2PutTools)

	// --- v2 action endpoints (#1512): real REST wrappers over CLI-only ops. ---
	// Rebuild/reset are ASYNC: handler returns 202 + a job id immediately and
	// runs the index in a background goroutine (never blocks the daemon from
	// serving — the #1487 serving-mutex invariant). Poll/stream via /api/v2/jobs.
	mux.HandleFunc("POST /api/v2/groups/{group}/rebuild", s.handleV2RebuildGroupAsync)
	mux.HandleFunc("POST /api/v2/groups/{group}/repos/{repo}/rebuild", s.handleV2RebuildRepoAsync)
	mux.HandleFunc("POST /api/v2/groups/{group}/repos/{repo}/reset", s.handleV2ResetRepoAsync)
	mux.HandleFunc("GET /api/v2/jobs/{id}", s.handleV2JobGet)
	mux.HandleFunc("GET /api/v2/jobs/{id}/stream", s.handleV2JobStream)
	mux.HandleFunc("POST /api/v2/maintenance/cleanup", s.handleV2Cleanup)
	mux.HandleFunc("POST /api/v2/update/apply", s.handleV2UpdateApply)
	mux.HandleFunc("POST /api/v2/patterns/{group}/export", s.handleV2PatternExport)
	mux.HandleFunc("POST /api/v2/patterns/{group}/gc", s.handleV2PatternGC)

	// Topology screen — WebUI v2 (#1440, epic #1432).
	// Wraps the v1 collectTopologyResponse + buildTopicDetail in the v2 envelope.
	// The v1 topology routes above are UNCHANGED.
	mux.HandleFunc("GET /api/v2/topology/{group}", s.handleV2Topology)
	mux.HandleFunc("GET /api/v2/topology/{group}/topic/{topicId}", s.handleV2TopologyDetail)
	// Compound architecture-diagram topology (Model 1, #4810/#4811):
	// nested containment zones + tier lanes + typed/aggregatable edges, for a
	// `group_by` lens (infra|modules|tier).
	mux.HandleFunc("GET /api/v2/topology/{group}/compound", s.handleV2TopologyCompound)
	// --- v2 Pending screen (#1442) ---
	mux.HandleFunc("GET /api/v2/groups/{group}/candidates", s.handleV2Candidates)
	mux.HandleFunc("PUT /api/v2/groups/{group}/candidates/{cid}/hint", s.handleV2CandidateHint)
	// Flows (Process Flow Explorer) — v2 envelope wrappers (#1441).
	// NOTE: /dead-ends and /truncated are registered before any wildcard so
	// Go 1.22 ServeMux picks the more-specific path first.
	mux.HandleFunc("GET /api/v2/groups/{group}/flows", s.handleV2FlowsList)
	mux.HandleFunc("GET /api/v2/groups/{group}/flows/dead-ends", s.handleV2FlowDeadEnds)
	mux.HandleFunc("GET /api/v2/groups/{group}/flows/truncated", s.handleV2FlowTruncated)
	// Paths screen — API & Endpoints Explorer (#1439, epic #1432).
	// NOTE: /orphans must be registered before /{hash} so the static suffix
	// wins Go 1.22+ ServeMux precedence.
	mux.HandleFunc("GET /api/v2/groups/{id}/paths", s.handleV2PathsList)
	mux.HandleFunc("GET /api/v2/groups/{id}/paths/orphans", s.handleV2PathsOrphans)
	mux.HandleFunc("GET /api/v2/groups/{id}/paths/{hash}", s.handleV2PathDetail)
	// #4254 — lazy posture + effective-contract sections for the detail pane.
	mux.HandleFunc("GET /api/v2/groups/{id}/paths/{hash}/posture", s.handleV2PathPosture)
	// #4349 (epic #4348) — endpoint downstream-DAG for the endpoint-flow modal:
	// branching call tree rooted at the endpoint (depth/collapse/semantic).
	mux.HandleFunc("GET /api/v2/groups/{id}/paths/{hash}/downstream-dag", s.handleV2PathDownstreamDAG)
	// #4819 (epic #4820) — endpoint handler control-flow (CFG) for the
	// Downstream-flow Flowchart view: on-demand flowchart of the handler function
	// (reuses the internal/substrate CFG builder; detail = outline|decisions|data|full).
	mux.HandleFunc("GET /api/v2/groups/{id}/paths/{hash}/control-flow", s.handleV2PathControlFlow)
	// #1935 Phase 1 — ShapeTree subtree resolution (lazy class-field walk).
	mux.HandleFunc("GET /api/v2/groups/{id}/shape", s.handleV2Shape)
	// #4499 — shared "source peek": window of source for any file:line ref.
	mux.HandleFunc("GET /api/v2/groups/{id}/source", s.handleV2Source)
	// Module-level GDS analysis (#1384, epic #1380) — SCC + PageRank +
	// betweenness over the aggregated module graph.
	mux.HandleFunc("GET /api/v2/groups/{group}/modules/analysis", s.handleV2ModulesAnalysis)
	// S7a (#2169): daemon mode switcher — read + write the active mode without
	// the user needing a terminal (`grafel mode <m>` equivalent).
	mux.HandleFunc("GET /api/v2/daemon/mode", s.handleV2GetDaemonMode)
	mux.HandleFunc("POST /api/v2/daemon/mode", s.handleV2SetDaemonMode)

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
	// Wire contract (#4516): emit `[]` not `null` for empty array-typed fields
	// so the webui-v2 TS report types (which call .length/.map/.filter) don't
	// crash. The frontend null-guards (#4514) remain as belt-and-suspenders.
	_ = json.NewEncoder(w).Encode(normalizeNilSlices(v))
}

// writeReportJSON writes an indented JSON report with the dashboard wire
// contract applied (nil slices -> []). This is the shared write path for the
// report handlers (graphql, errorflow, di, dataflow, iac, coverage, security,
// nplus1, …) which previously constructed a json.Encoder inline and emitted
// `null` for empty slice fields.
func writeReportJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(normalizeNilSlices(v))
}

// writeErr emits a uniform { "error": "..." } body.
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// gzipWriterPool recycles *gzip.Writer instances to avoid paying the
// allocator on every compressed request.  Reset(w) re-targets an existing
// writer at a new io.Writer so the underlying Huffman tables are reused.
//
// Perf (#1399): on a busy daemon serving many graph requests the pool
// eliminates one ~8 kB heap allocation per request for the gzip internal
// state buffer.
var gzipWriterPool = sync.Pool{
	New: func() any {
		gz, _ := gzip.NewWriterLevel(nil, gzip.DefaultCompression)
		return gz
	},
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
// Perf (#1399): gzip.Writer instances are pooled via gzipWriterPool so the
// allocator is hit once per pool-miss, not once per request.
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
		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(w)
		defer func() {
			_ = gz.Close()
			gzipWriterPool.Put(gz)
		}()
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
