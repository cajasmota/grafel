package dashboard

// handlers_audit.go — Audit log REST + SSE surface (#1258)
//
// Routes registered in server.go:
//
//	GET /api/audit             — last N entries, filterable by operation
//	GET /api/audit/stream      — SSE tail; real-time entries as they are written
//	GET /api/audit/export      — download full log as JSON or CSV
//
// The backend writes ~/.archigraph/audit.jsonl; this handler reads it back.

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/cajasmota/archigraph/internal/audit"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// auditHistoryReply is returned by GET /api/audit.
type auditHistoryReply struct {
	Entries []audit.Entry `json:"entries"`
	Count   int           `json:"count"`
	Note    string        `json:"note,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/audit
// ─────────────────────────────────────────────────────────────────────────────

// handleAuditHistory returns the last N audit entries from disk.
// Query params:
//
//	limit=N      (default 100, max 1000)
//	filter=op    (exact match on Entry.Operation)
func (s *Server) handleAuditHistory(w http.ResponseWriter, r *http.Request) {
	if s.auditLog == nil {
		writeJSON(w, http.StatusOK, auditHistoryReply{
			Entries: []audit.Entry{},
			Note:    "audit log not configured",
		})
		return
	}

	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 1000 {
		limit = 1000
	}
	filterOp := r.URL.Query().Get("filter")

	entries, err := audit.ReadHistory(s.auditLog.Path(), limit, filterOp)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reading audit log: "+err.Error())
		return
	}
	if entries == nil {
		entries = []audit.Entry{}
	}

	// Return newest-first so the frontend default view shows recent actions.
	reverseEntries(entries)

	writeJSON(w, http.StatusOK, auditHistoryReply{
		Entries: entries,
		Count:   len(entries),
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/audit/stream  — SSE real-time tail
// ─────────────────────────────────────────────────────────────────────────────

// handleAuditStream fans out new audit entries to SSE subscribers in real time.
// The broker is optional; when nil the endpoint returns 503.
//
// SSE events:
//
//	event: connected  data: {"subscribed_at": <unix-ms>}
//	event: audit      data: <JSON Entry>
//	event: heartbeat  data: {}
//	event: close      data: {}
func (s *Server) handleAuditStream(w http.ResponseWriter, r *http.Request) {
	if s.auditBroker == nil {
		writeErr(w, http.StatusServiceUnavailable, "audit stream broker not available")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	ch, cancel := s.auditBroker.Subscribe()
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
			writeSSEEvent(w, "audit", string(data))
			flusher.Flush()

		case <-heartbeat.C:
			writeSSEEvent(w, "heartbeat", "{}")
			flusher.Flush()
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/audit/export
// ─────────────────────────────────────────────────────────────────────────────

// handleAuditExport downloads the audit log as JSON or CSV.
// Query param: format=json (default) | csv
func (s *Server) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	if s.auditLog == nil {
		writeErr(w, http.StatusServiceUnavailable, "audit log not configured")
		return
	}

	entries, err := audit.ReadHistory(s.auditLog.Path(), 0, "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reading audit log: "+err.Error())
		return
	}
	if entries == nil {
		entries = []audit.Entry{}
	}
	reverseEntries(entries)

	format := r.URL.Query().Get("format")
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="audit.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"timestamp", "operation", "target", "result", "error"})
		for _, e := range entries {
			_ = cw.Write([]string{e.Timestamp, e.Operation, e.Target, e.Result, e.Error})
		}
		cw.Flush()
		return
	}

	// Default: JSON
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="audit.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(entries)
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func reverseEntries(entries []audit.Entry) {
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
}
