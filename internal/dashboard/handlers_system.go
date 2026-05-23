package dashboard

// handlers_system.go — System / Daemon control panel endpoints
//
// Exposes daemon process lifecycle and log tail over HTTP so the web UI
// can surface the information without the user dropping to a terminal.
//
// Routes registered in server.go:
//
//	GET  /api/system          — live daemon status snapshot (auto-refresh)
//	POST /api/system/restart  — signal daemon to exit then relaunch via launchd
//	POST /api/system/stop     — SIGTERM the daemon (destructive, confirm required)
//	GET  /api/system/logs     — tail of daemon.log (last N lines, optional SSE)

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/version"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// SystemReply is the wire shape for GET /api/system.
// All fields use omitempty so older frontend builds are not broken by
// future additions.
type SystemReply struct {
	// Process state
	Status        string  `json:"status"` // "running" | "stopped" | "unhealthy"
	UptimeSeconds int64   `json:"uptime_seconds,omitempty"`
	UptimeHuman   string  `json:"uptime_human,omitempty"`
	PID           int     `json:"pid"`
	RSSMb         float64 `json:"rss_mb"`
	RSSBudgetMb   float64 `json:"rss_budget_mb,omitempty"`

	// Paths
	SocketPath   string `json:"socket_path,omitempty"`
	DashboardURL string `json:"dashboard_url,omitempty"`

	// Build info
	Version        string `json:"version"`
	CommitSHA      string `json:"commit_sha"`
	BuiltAt        string `json:"built_at"`
	DaysSinceBuild int    `json:"days_since_build,omitempty"`
	StaleBuild     bool   `json:"stale_build"` // true when >7 days since build
}

// SystemActionReply is the wire shape for POST /api/system/restart and stop.
type SystemActionReply struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// SystemLogsReply is the wire shape for GET /api/system/logs (non-SSE mode).
type SystemLogsReply struct {
	Lines []LogLine `json:"lines"`
	Total int       `json:"total"`
	Path  string    `json:"path"`
}

// LogLine is one entry in the logs viewer.
type LogLine struct {
	Raw      string `json:"raw"`
	Severity string `json:"severity"` // "error" | "warn" | "info" | "debug"
}

// ─────────────────────────────────────────────────────────────────────────────
// Handlers
// ─────────────────────────────────────────────────────────────────────────────

// handleSystem — GET /api/system
//
// Returns a live snapshot of daemon process state. Designed to be cheap
// enough to poll every 5 s from the frontend.
func (s *Server) handleSystem(w http.ResponseWriter, _ *http.Request) {
	reply := s.buildSystemReply()
	writeJSON(w, http.StatusOK, reply)
}

// handleSystemRestart — POST /api/system/restart
//
// Sends SIGTERM to the current process, expecting launchd / systemd to
// restart it via KeepAlive=true. The dashboard page will poll /api/system
// and detect the re-launch. Safe to call from the confirm modal.
func (s *Server) handleSystemRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	pid := os.Getpid()
	writeJSON(w, http.StatusOK, SystemActionReply{
		OK:      true,
		Message: fmt.Sprintf("Sending SIGTERM to daemon (pid %d) — it will restart automatically.", pid),
	})
	// Flush before the process exits.
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		proc, err := os.FindProcess(pid)
		if err != nil {
			return
		}
		_ = proc.Signal(syscall.SIGTERM)
	}()
}

// handleSystemStop — POST /api/system/stop
//
// Danger-zone: SIGTERMs the daemon. Unlike restart, the daemon will NOT
// automatically come back unless the user manually runs `archigraph start`.
// The frontend should show a red confirm modal before calling this.
func (s *Server) handleSystemStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	pid := os.Getpid()
	writeJSON(w, http.StatusOK, SystemActionReply{
		OK:      true,
		Message: fmt.Sprintf("Sending SIGTERM to daemon (pid %d) — restart via 'archigraph start'.", pid),
	})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		proc, err := os.FindProcess(pid)
		if err != nil {
			return
		}
		_ = proc.Signal(syscall.SIGTERM)
	}()
}

// handleSystemLogs — GET /api/system/logs
//
// Returns the tail of daemon.log. Query params:
//   - n=N        — how many lines to return (default 200, max 1000)
//   - q=TEXT     — optional case-insensitive substring filter
//   - severity=  — optional filter: "error" | "warn" (filters out lower lines)
//   - follow=true — SSE stream (text/event-stream); each new line emitted as an event
func (s *Server) handleSystemLogs(w http.ResponseWriter, r *http.Request) {
	layout, err := daemon.DefaultLayout()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "resolve daemon layout: "+err.Error())
		return
	}
	logPath := layout.LogPath

	// Parse query params
	nStr := r.URL.Query().Get("n")
	n := 200
	if nStr != "" {
		if parsed, perr := strconv.Atoi(nStr); perr == nil && parsed > 0 {
			n = parsed
		}
	}
	if n > 1000 {
		n = 1000
	}
	q := strings.ToLower(r.URL.Query().Get("q"))
	sev := strings.ToLower(r.URL.Query().Get("severity"))
	follow := r.URL.Query().Get("follow") == "true"

	if follow {
		s.streamSystemLogs(w, r, logPath, q, sev)
		return
	}

	lines, err := tailLogFile(logPath, n, q, sev)
	if err != nil {
		// Log file may not exist yet (daemon not started with logging enabled).
		writeJSON(w, http.StatusOK, SystemLogsReply{
			Lines: []LogLine{},
			Total: 0,
			Path:  logPath,
		})
		return
	}
	writeJSON(w, http.StatusOK, SystemLogsReply{Lines: lines, Total: len(lines), Path: logPath})
}

// ─────────────────────────────────────────────────────────────────────────────
// SSE log streaming
// ─────────────────────────────────────────────────────────────────────────────

// streamSystemLogs implements GET /api/system/logs?follow=true as a
// Server-Sent Events stream. It tails the log file and emits each new line
// as a "log" event. The stream is closed when the client disconnects.
func (s *Server) streamSystemLogs(w http.ResponseWriter, r *http.Request, logPath, q, sev string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Open the file and seek to EOF so we only stream new lines.
	f, err := os.Open(logPath)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: log file not found: %s\n\n", logPath)
		flusher.Flush()
		return
	}
	defer f.Close()
	if _, err := f.Seek(0, 2); err != nil { // SEEK_END
		return
	}

	scanner := bufio.NewScanner(f)
	ctx := r.Context()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			for scanner.Scan() {
				raw := scanner.Text()
				if !matchesFilter(raw, q, sev) {
					continue
				}
				severity := classifySeverity(raw)
				// SSE format: event: log\ndata: JSON\n\n
				escaped := strings.ReplaceAll(raw, "\n", "\\n")
				fmt.Fprintf(w, "event: log\ndata: {\"raw\":%q,\"severity\":%q}\n\n", escaped, severity)
			}
			flusher.Flush()
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) buildSystemReply() SystemReply {
	reply := SystemReply{
		Status:    "running",
		PID:       os.Getpid(),
		RSSMb:     getRSSMB(),
		Version:   version.Version,
		CommitSHA: version.Commit,
		BuiltAt:   version.Date,
	}

	// Uptime
	if !s.daemonStartedAt.IsZero() {
		uptime := time.Since(s.daemonStartedAt)
		reply.UptimeSeconds = int64(uptime.Seconds())
		reply.UptimeHuman = formatDuration(uptime)
	}

	// Dashboard URL
	if s.listener != nil {
		if _, portStr, err := net.SplitHostPort(s.listener.Addr().String()); err == nil {
			if p, perr := strconv.Atoi(portStr); perr == nil {
				reply.DashboardURL = fmt.Sprintf("http://127.0.0.1:%d/", p)
			}
		}
	}

	// Socket path (best-effort — may fail in test mode)
	if layout, err := daemon.DefaultLayout(); err == nil {
		reply.SocketPath = layout.SocketPath
	}

	// RSS budget from env (mirrors daemon startup logic)
	budgetMB := int64(500)
	if v := os.Getenv("ARCHIGRAPH_MAX_RSS_BUDGET_MB"); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil && parsed >= 0 {
			budgetMB = parsed
		}
	}
	reply.RSSBudgetMb = float64(budgetMB)

	// Build staleness
	if version.Date != "unknown" && version.Date != "" {
		if t, err := time.Parse(time.RFC3339, version.Date); err == nil {
			days := int(time.Since(t).Hours() / 24)
			reply.DaysSinceBuild = days
			reply.StaleBuild = days > 7
		}
	}

	return reply
}

// tailLogFile reads up to n lines from the end of logPath, optionally
// filtered by substring q and severity. Uses a circular buffer so the
// file is only scanned once even when n is large.
func tailLogFile(logPath string, n int, q, sev string) ([]LogLine, error) {
	f, err := os.Open(logPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Read all lines into a circular buffer of size n.
	buf := make([]string, 0, n)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !matchesFilter(line, q, sev) {
			continue
		}
		buf = append(buf, line)
		if len(buf) > n {
			buf = buf[1:]
		}
	}

	lines := make([]LogLine, 0, len(buf))
	for _, raw := range buf {
		lines = append(lines, LogLine{
			Raw:      raw,
			Severity: classifySeverity(raw),
		})
	}
	return lines, nil
}

// matchesFilter reports whether a log line passes the q (substring) and
// sev (minimum severity) filters.
func matchesFilter(line, q, sev string) bool {
	if q != "" && !strings.Contains(strings.ToLower(line), q) {
		return false
	}
	if sev == "" {
		return true
	}
	lineSev := classifySeverity(line)
	switch sev {
	case "error":
		return lineSev == "error"
	case "warn":
		return lineSev == "error" || lineSev == "warn"
	}
	return true
}

// classifySeverity returns a simple severity string based on common log
// keywords. Matches archigraph-daemon log format (stdlib log + custom prefixes).
func classifySeverity(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic"):
		return "error"
	case strings.Contains(lower, "warn") || strings.Contains(lower, "deprecated"):
		return "warn"
	case strings.Contains(lower, "debug") || strings.Contains(lower, "trace"):
		return "debug"
	default:
		return "info"
	}
}

// restartViaBinary attempts to re-exec the current binary using the
// platform's native service manager. On macOS this is launchctl; on Linux
// it is systemctl. Falls back to a direct exec if neither is available.
// This is called by handleSystemRestart after the response is flushed.
func restartViaBinary() {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("launchctl", "kickstart", "-k", "gui/"+strconv.Itoa(os.Getuid())+"/com.archigraph.daemon").Run()
	case "linux":
		_ = exec.Command("systemctl", "--user", "restart", "archigraph").Run()
	}
}
