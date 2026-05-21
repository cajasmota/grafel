package dashboard

// handlers_mcp_activity.go — SSE + history endpoints for MCP query activity
//
// Routes registered in server.go:
//
//	GET /api/mcp-activity/stream   — live Server-Sent Events (all tool calls)
//	GET /api/mcp-activity/history  — last N events from disk log
//
// Wire format (SSE):
//
//	event: connected
//	data: {"subscribed_at":<unix-ms>}\n\n
//
//	event: mcp_activity
//	data: <JSON-encoded mcp.MCPActivityEvent>\n\n
//
//	event: heartbeat
//	data: {}\n\n
//
//	event: close
//	data: {}\n\n   (sent before server closes the stream)
//
// Phase 1 of epic #1157: Jarvis-style real-time MCP query visualization.
// This file is backend-only; the frontend (Phase 2) subscribes and animates.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/cajasmota/archigraph/internal/mcp"
)

// handleMCPActivityStream streams real-time MCP tool call events over SSE.
// Multiple simultaneous subscribers are supported; they never block each other.
func (s *Server) handleMCPActivityStream(w http.ResponseWriter, r *http.Request) {
	if s.mcpActivityBroker == nil {
		writeErr(w, http.StatusServiceUnavailable, "mcp activity broker not available")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Proxy-friendly SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	ch, cancel := s.mcpActivityBroker.SubscribeAll()
	defer cancel()

	subscribedAt := time.Now().UnixMilli()
	writeSSEEvent(w, "connected", fmt.Sprintf(`{"subscribed_at":%d}`, subscribedAt))
	flusher.Flush()

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			writeSSEEvent(w, "close", "{}")
			flusher.Flush()
			return

		case e, ok := <-ch:
			if !ok {
				writeSSEEvent(w, "close", "{}")
				flusher.Flush()
				return
			}
			data, err := json.Marshal(e)
			if err != nil {
				continue
			}
			writeSSEEvent(w, "mcp_activity", string(data))
			flusher.Flush()

		case <-heartbeat.C:
			writeSSEEvent(w, "heartbeat", "{}")
			flusher.Flush()
		}
	}
}

// handleMCPActivityHistory returns the last N events from the on-disk JSONL
// log. Query param: ?limit=N (default 50, max 500).
func (s *Server) handleMCPActivityHistory(w http.ResponseWriter, r *http.Request) {
	if s.mcpActivityLog == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"events": []any{},
			"count":  0,
			"note":   "disk activity log not configured",
		})
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 500 {
		limit = 500
	}

	events, err := mcp.ReadHistory(s.mcpActivityLog, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reading activity log: "+err.Error())
		return
	}
	if events == nil {
		events = []mcp.MCPActivityEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"count":  len(events),
	})
}
