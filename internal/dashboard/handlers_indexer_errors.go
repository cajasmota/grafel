package dashboard

// handlers_indexer_errors.go — GET /api/indexer-errors
//
// Returns recent typed indexer errors from the audit log, enriched with
// the canonical remediation hint and docs URL from the errors registry.
// This powers the /diagnostics page's "Indexer Errors" section and can
// be polled by the CLI's `archigraph doctor` output.
//
// Route registered in server.go:
//
//	GET /api/indexer-errors?n=50&code=IDX-002

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/audit"
	idxerrors "github.com/cajasmota/archigraph/internal/errors"
)

// IndexerErrorReply is the JSON envelope for GET /api/indexer-errors.
type IndexerErrorReply struct {
	CheckedAt string               `json:"checked_at"`
	Total     int                  `json:"total"`
	Errors    []IndexerErrorRecord `json:"errors"`
}

// IndexerErrorRecord is one entry from the audit log, enriched with
// remediation metadata.
type IndexerErrorRecord struct {
	Timestamp string `json:"timestamp"`
	Code      string `json:"code,omitempty"`
	Operation string `json:"operation"`
	Target    string `json:"target,omitempty"`
	Message   string `json:"message"`
	FilePath  string `json:"file_path,omitempty"`
	Hint      string `json:"hint,omitempty"`
	DocsURL   string `json:"docs_url,omitempty"`
}

// handleIndexerErrors — GET /api/indexer-errors
//
// Query params:
//   - n     int    max records to return (default 50, max 500)
//   - code  string filter by error code, e.g. "IDX-002" (optional)
func (s *Server) handleIndexerErrors(w http.ResponseWriter, r *http.Request) {
	n := 50
	if raw := r.URL.Query().Get("n"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 500 {
			n = parsed
		}
	}
	codeFilter := strings.ToUpper(r.URL.Query().Get("code"))

	// Use the server's configured audit log when available, otherwise fall back
	// to the default on-disk path so the handler works outside the daemon too.
	var logPath string
	if s.auditLog != nil {
		logPath = s.auditLog.Path()
	} else {
		logPath = audit.DefaultLogPath()
	}

	// Read more than n so we can filter and still return n results.
	entries, err := audit.ReadHistory(logPath, n*4, "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "read audit log: "+err.Error())
		return
	}

	var records []IndexerErrorRecord
	for _, e := range entries {
		if e.Result != "error" {
			continue
		}
		// Only surface entries that look like indexer operations.
		if !isIndexerOperation(e.Operation) {
			continue
		}
		code := extractCode(e)
		if codeFilter != "" && code != codeFilter {
			continue
		}
		rec := IndexerErrorRecord{
			Timestamp: e.Timestamp,
			Code:      code,
			Operation: e.Operation,
			Target:    e.Target,
			Message:   e.Error,
		}
		if fp, ok := e.Params["file_path"]; ok {
			if fs, ok := fp.(string); ok {
				rec.FilePath = fs
			}
		}
		rec.Hint, rec.DocsURL = hintAndDocs(code)
		records = append(records, rec)
	}

	// Trim to n most-recent (ReadHistory already returns in chronological order).
	if len(records) > n {
		records = records[len(records)-n:]
	}
	if records == nil {
		records = []IndexerErrorRecord{}
	}

	writeJSON(w, http.StatusOK, IndexerErrorReply{
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Total:     len(records),
		Errors:    records,
	})
}

// isIndexerOperation returns true for audit operations that originate from
// the indexer pipeline.
func isIndexerOperation(op string) bool {
	switch op {
	case "index", "index_file", "ast_extract", "ast_parse",
		"manifest_scan", "cross_repo_resolve", "symlink_walk",
		"rebuild", "reset":
		return true
	}
	return false
}

// extractCode looks for an error code in the audit entry's Params map
// (key "error_code") or falls back to parsing the Error string prefix.
func extractCode(e audit.Entry) string {
	if v, ok := e.Params["error_code"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	// Fallback: error strings formatted as "[IDX-NNN] ..."
	if len(e.Error) > 9 && e.Error[0] == '[' {
		end := strings.Index(e.Error, "]")
		if end > 1 {
			return e.Error[1:end]
		}
	}
	return ""
}

// hintAndDocs returns the canonical hint and docs URL for a given error code
// string. The values come from the internal/errors registry (the single
// source of truth for IDX-NNN remediation text); constructing an IndexerError
// is the cheapest way to read back the canonical hint + docs URL without
// exporting the registry's private hint()/docsURL() helpers.
//
// internal/errors imports only the standard library, so there is no import
// cycle with the dashboard package.
func hintAndDocs(code string) (hint, docsURL string) {
	if code == "" {
		return "", ""
	}
	ie := idxerrors.New(idxerrors.Code(code), "", "", 0, nil)
	return ie.Hint, ie.DocsURL
}
