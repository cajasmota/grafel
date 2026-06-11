// v2_helpers.go — shared helpers for the /api/v2/... surface.
//
// Every v2 response uses one of two shapes:
//
//	Success (non-paginated):
//	  { "ok": true, "data": <any> }
//
//	Success (paginated):
//	  { "ok": true, "data": [...], "pagination": { "limit": N, "offset": N, "total": N } }
//
//	Error:
//	  { "ok": false, "error": { "code": "<snake_case>", "message": "<human>" } }
//
// SSE events emitted by v2 streaming endpoints use the format:
//
//	event: <type>\ndata: <JSON>\n\n
//
// See API_V2.md for the full contract.

package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// ---------------------------------------------------------------------------
// Envelope types
// ---------------------------------------------------------------------------

// v2Envelope is the wire shape for all non-paginated v2 responses.
type v2Envelope struct {
	OK   bool `json:"ok"`
	Data any  `json:"data,omitempty"`
}

// v2ErrEnvelope is the wire shape for all v2 error responses.
type v2ErrEnvelope struct {
	OK    bool      `json:"ok"`
	Error v2ErrBody `json:"error"`
}

// v2ErrBody carries a machine-readable code and a human-readable message.
type v2ErrBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// v2PageEnvelope is the wire shape for paginated v2 list responses.
type v2PageEnvelope struct {
	OK         bool         `json:"ok"`
	Data       any          `json:"data"`
	Pagination V2Pagination `json:"pagination"`
}

// V2Pagination is the pagination metadata included in paginated responses.
type V2Pagination struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

// ---------------------------------------------------------------------------
// Constructor helpers
// ---------------------------------------------------------------------------

// v2OK wraps data in a success envelope.
func v2OK(data any) v2Envelope {
	return v2Envelope{OK: true, Data: data}
}

// v2Err creates an error envelope with code + message.
func v2Err(code, message string) v2ErrEnvelope {
	return v2ErrEnvelope{OK: false, Error: v2ErrBody{Code: code, Message: message}}
}

// v2Page wraps a list and pagination metadata in a success envelope.
func v2Page(data any, p V2Pagination) v2PageEnvelope {
	return v2PageEnvelope{OK: true, Data: data, Pagination: p}
}

// ---------------------------------------------------------------------------
// Pagination parsing
// ---------------------------------------------------------------------------

const (
	v2DefaultLimit = 50
	v2MaxLimit     = 500
)

// parsePagination extracts limit/offset from query params.
// q may be nil (returns defaults). total is the total item count known by the
// caller; it is embedded in the returned V2Pagination for convenience.
func parsePagination(q url.Values, total int) V2Pagination {
	limit := v2DefaultLimit
	offset := 0
	if q != nil {
		if v := q.Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if v := q.Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}
	}
	if limit > v2MaxLimit {
		limit = v2MaxLimit
	}
	return V2Pagination{Limit: limit, Offset: offset, Total: total}
}

// ---------------------------------------------------------------------------
// HTTP write helpers
// ---------------------------------------------------------------------------

// writeV2JSON writes a v2-enveloped JSON response.
func writeV2JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Wire contract (#4516): nil slice fields serialize as `[]`, not `null`.
	_ = json.NewEncoder(w).Encode(normalizeNilSlices(v))
}

// writeV2Err writes a v2 error response.
func writeV2Err(w http.ResponseWriter, status int, code, message string) {
	writeV2JSON(w, status, v2Err(code, message))
}

// ---------------------------------------------------------------------------
// SSE helpers — same physical format as v1 SSE but type-labeled for v2.
// ---------------------------------------------------------------------------

// writeV2SSEEvent writes a single SSE event block to w.
// Callers must flush after writing. Format:
//
//	event: <eventType>\ndata: <data>\n\n
func writeV2SSEEvent(w http.ResponseWriter, eventType, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
}

// setV2SSEHeaders writes the standard SSE response headers.
// Call before WriteHeader.
func setV2SSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
}
