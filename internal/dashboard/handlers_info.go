package dashboard

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/cajasmota/archigraph/internal/version"
)

// infoReply is the wire shape for GET /api/info.
// All fields are optional-on-the-wire (omitempty) so future additions
// stay backwards-compatible with older frontend builds.
type infoReply struct {
	Version         string `json:"version"`
	Commit          string `json:"commit"`
	BuiltAt         string `json:"built_at"`
	DaemonStartedAt string `json:"daemon_started_at,omitempty"`
	DashboardPort   int    `json:"dashboard_port,omitempty"`
}

// handleInfo — GET /api/info. Returns build metadata and, when the
// server was started by the embedded-daemon path, runtime info. The
// endpoint is deliberately side-effect-free and cheap — the frontend
// may call it on every popover open.
func (s *Server) handleInfo(w http.ResponseWriter, _ *http.Request) {
	reply := infoReply{
		Version: version.Version,
		Commit:  version.Commit,
		BuiltAt: version.Date,
	}
	// Resolve the bound port from the listener so the frontend can
	// construct the canonical dashboard URL.
	if s.listener != nil {
		if _, portStr, err := net.SplitHostPort(s.listener.Addr().String()); err == nil {
			if p, err := strconv.Atoi(portStr); err == nil {
				reply.DashboardPort = p
			}
		}
	}
	// daemonStartedAt is set when the server is embedded inside a daemon
	// process (non-zero). When serving standalone it remains zero/empty.
	if !s.daemonStartedAt.IsZero() {
		reply.DaemonStartedAt = s.daemonStartedAt.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, reply)
}
