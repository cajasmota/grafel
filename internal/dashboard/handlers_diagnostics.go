package dashboard

// handlers_diagnostics.go — GET /api/diagnostics and POST /api/diagnostics/kill-stale
//
// Ports the `archigraph doctor` CLI output to a JSON REST surface so the web
// Diagnostics page can render daemon + per-group health without a terminal.
//
// Routes registered in server.go:
//
//	GET  /api/diagnostics             — full health snapshot
//	POST /api/diagnostics/kill-stale  — terminate stale daemon processes

import (
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/cajasmota/archigraph/internal/cli"
	"github.com/cajasmota/archigraph/internal/install/mcpreg"
	"github.com/cajasmota/archigraph/internal/process"
	"github.com/cajasmota/archigraph/internal/registry"
	"github.com/cajasmota/archigraph/internal/version"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// DiagnosticsReply is the wire shape for GET /api/diagnostics.
type DiagnosticsReply struct {
	CheckedAt string             `json:"checked_at"`
	Daemon    DaemonDiagnostics  `json:"daemon"`
	Groups    []GroupDiagnostics `json:"groups"`
	Nominal   bool               `json:"nominal"` // true when no issues found anywhere
}

// DaemonDiagnostics covers the daemon / process-level health section.
type DaemonDiagnostics struct {
	// Process state
	Running       bool    `json:"running"`
	Status        string  `json:"status"` // "running" | "unknown"
	PID           int     `json:"pid"`
	UptimeSeconds int64   `json:"uptime_seconds"`
	UptimeHuman   string  `json:"uptime_human"`
	RSSМБ         float64 `json:"rss_mb"`

	// Binary info (mirrors /api/info)
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`

	// Infrastructure checks
	SocketReachable        bool `json:"socket_reachable"`
	WorkspaceWritable      bool `json:"workspace_writable"`
	DashboardPort          int  `json:"dashboard_port"`
	DashboardPortAvailable bool `json:"dashboard_port_available"`
	LaunchAgentInstalled   bool `json:"launch_agent_installed"` // macOS-only
	MCPClaudeCode          bool `json:"mcp_claude_code"`
	MCPWindsurf            bool `json:"mcp_windsurf"`

	// Registry
	RegistryPath string `json:"registry_path"`
	GroupCount   int    `json:"group_count"`

	// Watcher stats (#1270) — non-zero when the watcher is running.
	WatcherRepos         int    `json:"watcher_repos,omitempty"`
	WatcherDirs          int    `json:"watcher_dirs,omitempty"`
	WatcherTotalEvents   uint64 `json:"watcher_total_events,omitempty"`
	WatcherDropped       uint64 `json:"watcher_dropped,omitempty"`
	WatcherForceRescanOK bool   `json:"watcher_force_rescan_available"`
}

// GroupDiagnostics covers one group's health.
type GroupDiagnostics struct {
	Name                 string            `json:"name"`
	Status               string            `json:"status"` // "HEALTHY" | "DEGRADED" | "FAILED"
	DaemonManaged        bool              `json:"daemon_managed"`
	TotalEntities        int               `json:"total_entities"`
	TotalRelationships   int               `json:"total_relationships"`
	TotalCrossRepoEdges  int               `json:"total_cross_repo_edges"`
	OrphanEntities       int               `json:"orphan_entities"`
	OrphanRate           float64           `json:"orphan_rate"`
	BugRate              float64           `json:"bug_rate"`
	PendingRepairs       int               `json:"pending_repairs"`
	PendingEnrichments   int               `json:"pending_enrichments"`
	WatcherRepoCount     int               `json:"watcher_repo_count"`
	WatcherDirCount      int               `json:"watcher_dir_count"`
	WatcherEventsDropped int               `json:"watcher_events_dropped"`
	LastWatcherActivity  string            `json:"last_watcher_activity,omitempty"`
	Repos                []RepoDiagnostics `json:"repos"`
	IssuesFound          []IssueDiagnostic `json:"issues_found"`
}

// RepoDiagnostics covers one repo within a group.
type RepoDiagnostics struct {
	Slug           string `json:"slug"`
	Path           string `json:"path"`
	Status         string `json:"status"` // "OK" | "STALE" | "MISSING"
	LastIndexedAt  string `json:"last_indexed_at,omitempty"`
	LastIndexedAge string `json:"last_indexed_age"`
	Entities       int    `json:"entities"`
	Relationships  int    `json:"relationships"`
	CrossRepoEdges int    `json:"cross_repo_edges"`
}

// IssueDiagnostic is one auto-detected problem with an optional remediation hint.
type IssueDiagnostic struct {
	Description string `json:"description"`
	Remediation string `json:"remediation,omitempty"`
}

// KillStaleReply is the wire shape for POST /api/diagnostics/kill-stale.
type KillStaleReply struct {
	Killed []KilledProcess `json:"killed"`
	DryRun bool            `json:"dry_run"`
}

// ForceRescanReply is the wire shape for POST /api/diagnostics/force-rescan.
type ForceRescanReply struct {
	Triggered bool   `json:"triggered"`
	Message   string `json:"message"`
}

// KilledProcess is one stale process that was found (and optionally killed).
type KilledProcess struct {
	PID     int    `json:"pid"`
	PPID    int    `json:"ppid"`
	Exe     string `json:"exe"`
	Killed  bool   `json:"killed"`
	KillErr string `json:"kill_err,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Handlers
// ─────────────────────────────────────────────────────────────────────────────

// handleDiagnostics — GET /api/diagnostics
//
// Runs the same health-check logic as `archigraph doctor` and returns it as
// structured JSON. Designed to be cheap enough to poll every 30 s.
func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	reply := DiagnosticsReply{
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// ── Daemon section ──────────────────────────────────────────────────────
	reply.Daemon = s.buildDaemonDiagnostics()

	// ── Per-group section ───────────────────────────────────────────────────
	groups, err := registry.Groups()
	if err == nil {
		healthReports := cli.ComputeDoctorHealth(groups)
		for _, gh := range healthReports {
			reply.Groups = append(reply.Groups, convertGroupHealth(gh))
		}
	}

	// ── nominal flag ────────────────────────────────────────────────────────
	reply.Nominal = isNominal(reply)

	writeJSON(w, http.StatusOK, reply)
}

// handleDiagnosticsKillStale — POST /api/diagnostics/kill-stale
//
// Terminates stale archigraph daemon processes (PPID=1 + /tmp binary, or a
// daemon binary different from the currently-running one). Pass
// ?dry_run=true to list without killing.
func (s *Server) handleDiagnosticsKillStale(w http.ResponseWriter, r *http.Request) {
	dryRun := r.URL.Query().Get("dry_run") == "true"

	myPID := os.Getpid()
	selfExe, err := os.Executable()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "os.Executable: "+err.Error())
		return
	}

	procs, err := process.FindByName("archigraph")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "process scan: "+err.Error())
		return
	}

	var killed []KilledProcess
	for _, p := range procs {
		if p.PID == myPID {
			continue
		}
		exe := p.Exe
		if exe == "" {
			exe = p.Name
		}
		isTmp := len(exe) >= 4 && exe[:4] == "/tmp"
		isDifferentDaemon := containsLower(exe, "daemon") && exe != selfExe
		if !(p.PPID == 1 && isTmp) && !isDifferentDaemon {
			continue
		}
		kp := KilledProcess{PID: p.PID, PPID: p.PPID, Exe: exe}
		if !dryRun {
			if kerr := process.Kill(p.PID); kerr != nil {
				kp.KillErr = kerr.Error()
			} else {
				kp.Killed = true
			}
		}
		killed = append(killed, kp)
	}
	if killed == nil {
		killed = []KilledProcess{}
	}
	writeJSON(w, http.StatusOK, KillStaleReply{Killed: killed, DryRun: dryRun})
}

// handleDiagnosticsForceRescan — POST /api/diagnostics/force-rescan
//
// Triggers ForceRescan on the daemon's file watcher, queuing a full diff
// reconciliation for every registered repo. Returns 503 when the watcher
// is not available (e.g. daemon is not running in this process).
// Added in #1270.
func (s *Server) handleDiagnosticsForceRescan(w http.ResponseWriter, _ *http.Request) {
	if s.watcher == nil {
		writeErr(w, http.StatusServiceUnavailable, "watcher not available")
		return
	}
	s.watcher.ForceRescan()
	writeJSON(w, http.StatusOK, ForceRescanReply{
		Triggered: true,
		Message:   "force rescan triggered for all registered repos",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) buildDaemonDiagnostics() DaemonDiagnostics {
	d := DaemonDiagnostics{
		Version: version.Version,
		Commit:  version.Commit,
		BuiltAt: version.Date,
		PID:     os.Getpid(),
	}

	// Running / uptime
	if !s.daemonStartedAt.IsZero() {
		d.Running = true
		d.Status = "running"
		uptime := time.Since(s.daemonStartedAt)
		d.UptimeSeconds = int64(uptime.Seconds())
		d.UptimeHuman = formatDuration(uptime)
	} else {
		d.Running = true // dashboard itself is running
		d.Status = "running"
	}

	// RSS via /proc/self/status (Linux) or runtime stats (portable fallback)
	d.RSSМБ = getRSSMB()

	// Dashboard port
	if s.listener != nil {
		if _, portStr, err := net.SplitHostPort(s.listener.Addr().String()); err == nil {
			if p, err := strconv.Atoi(portStr); err == nil {
				d.DashboardPort = p
				d.DashboardPortAvailable = true
			}
		}
	}

	// Registry
	regPath, _ := registry.RegistryPath()
	d.RegistryPath = regPath
	if groups, err := registry.Groups(); err == nil {
		d.GroupCount = len(groups)
	}

	// Workspace writable
	homeDir, _ := registry.HomeDir()
	if homeDir != "" {
		testFile := homeDir + "/.archigraph_write_test"
		if f, err := os.Create(testFile); err == nil {
			f.Close()
			os.Remove(testFile)
			d.WorkspaceWritable = true
		}
	}

	// Socket reachable (best-effort: try to stat the socket file)
	socketPath := homeDir + "/sockets/daemon.sock"
	if _, err := os.Stat(socketPath); err == nil {
		d.SocketReachable = true
	}

	// LaunchAgent (macOS only)
	if runtime.GOOS == "darwin" {
		laPath := os.Getenv("HOME") + "/Library/LaunchAgents/com.archigraph.daemon.plist"
		if _, err := os.Stat(laPath); err == nil {
			d.LaunchAgentInstalled = true
		}
	}

	// MCP registrations
	if p, _ := mcpreg.SettingsPath(mcpreg.ClaudeCode); p != "" {
		if _, err := os.Stat(p); err == nil {
			d.MCPClaudeCode = true
		}
	}
	if p, _ := mcpreg.SettingsPath(mcpreg.Windsurf); p != "" {
		if _, err := os.Stat(p); err == nil {
			d.MCPWindsurf = true
		}
	}

	// Watcher stats (#1270)
	if s.watcher != nil {
		repos, dirs, events, dropped := s.watcher.Stats()
		d.WatcherRepos = repos
		d.WatcherDirs = dirs
		d.WatcherTotalEvents = events
		d.WatcherDropped = dropped
		d.WatcherForceRescanOK = true
	}

	return d
}

func convertGroupHealth(gh *cli.DoctorGroupHealth) GroupDiagnostics {
	gd := GroupDiagnostics{
		Name:                 gh.GroupName,
		Status:               gh.Status,
		DaemonManaged:        gh.DaemonManaged,
		TotalEntities:        gh.TotalEntities,
		TotalRelationships:   gh.TotalRelationships,
		TotalCrossRepoEdges:  gh.TotalCrossRepoEdges,
		OrphanEntities:       gh.OrphanEntities,
		OrphanRate:           gh.OrphanRate,
		BugRate:              gh.BugRate,
		PendingRepairs:       gh.PendingRepairs,
		PendingEnrichments:   gh.PendingEnrichments,
		WatcherRepoCount:     gh.WatcherRepoCount,
		WatcherDirCount:      gh.WatcherDirCount,
		WatcherEventsDropped: gh.WatcherEventsDropped,
		LastWatcherActivity:  gh.LastWatcherActivity,
	}

	for _, r := range gh.Repos {
		rd := RepoDiagnostics{
			Slug:           r.Slug,
			Path:           r.Path,
			Status:         r.Status,
			LastIndexedAge: r.LastIndexedAge,
			Entities:       r.Entities,
			Relationships:  r.Relationships,
			CrossRepoEdges: r.CrossRepoEdges,
		}
		if !r.LastIndexed.IsZero() {
			rd.LastIndexedAt = r.LastIndexed.UTC().Format(time.RFC3339)
		}
		gd.Repos = append(gd.Repos, rd)
	}

	for _, issue := range gh.IssuesFound {
		gd.IssuesFound = append(gd.IssuesFound, IssueDiagnostic{
			Description: issue,
			Remediation: remediationHint(issue),
		})
	}
	if gd.Repos == nil {
		gd.Repos = []RepoDiagnostics{}
	}
	if gd.IssuesFound == nil {
		gd.IssuesFound = []IssueDiagnostic{}
	}

	return gd
}

func isNominal(r DiagnosticsReply) bool {
	if !r.Daemon.Running {
		return false
	}
	for _, g := range r.Groups {
		if g.Status != "HEALTHY" {
			return false
		}
		if len(g.IssuesFound) > 0 {
			return false
		}
	}
	return true
}

// remediationHint maps issue descriptions to actionable hints.
func remediationHint(description string) string {
	switch {
	case containsLower(description, "hasn't been indexed in"):
		return "Run 'archigraph index' or use the Rebuild button to re-index this repo."
	case containsLower(description, "missing .git"):
		return "Ensure the repository path is a valid git checkout."
	case containsLower(description, "no graph found"):
		return "Run 'archigraph index <repo-path>' to build the initial graph."
	case containsLower(description, "graph.json present") && containsLower(description, "graph.fb missing"):
		return "Run 'archigraph index' to generate the binary graph format."
	default:
		return ""
	}
}

// containsLower returns true when s contains substr (case-insensitive, using
// a simple byte-level search rather than importing strings to keep the file
// self-contained).
func containsLower(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	// Use stdlib strings — the import block above already brings in the runtime.
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			cs := s[i+j]
			if cs >= 'A' && cs <= 'Z' {
				cs += 32
			}
			ct := substr[j]
			if ct >= 'A' && ct <= 'Z' {
				ct += 32
			}
			if cs != ct {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// formatDuration returns a short human-readable duration string.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return itoa(h) + "h " + itoa(m) + "m"
	}
	if m > 0 {
		return itoa(m) + "m " + itoa(s) + "s"
	}
	return itoa(s) + "s"
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// getRSSMB returns the resident set size in megabytes.
// Uses runtime.ReadMemStats which is portable and does not require /proc.
func getRSSMB() float64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return float64(ms.Sys) / 1024 / 1024
}
