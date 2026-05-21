package dashboard

// handlers_secrets.go — Secret-detection HTTP handler.
//
// Route registered in server.go:
//
//	GET /api/quality/secrets/{group}
//
// The handler scans every repository in the named group for hardcoded
// credentials and returns a structured JSON report.  Scanning runs in-process
// (no daemon socket hop): the dashboard server IS the daemon, so calling
// secrets.ScanPath directly is safe.
//
// Query parameters:
//
//	severity   filter to this severity and above (critical|high|medium|low).
//	            Default: all severities.
//	max_size   max file size in bytes to scan per file (default: 524288 = 512 KB).

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/cajasmota/archigraph/internal/secrets"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// QualitySecretFinding is the API-level representation of one detected secret
// returned by GET /api/quality/secrets/{group}.
type QualitySecretFinding struct {
	File            string `json:"file"`
	Line            int    `json:"line"`
	Kind            string `json:"kind"`
	MaskedValue     string `json:"masked_value"`
	Severity        string `json:"severity"`
	SuggestedEnvVar string `json:"suggested_env_var"`
}

// SecretFileRollup groups findings per file.
type SecretFileRollup struct {
	File     string          `json:"file"`
	Repo     string          `json:"repo"`
	Count    int             `json:"count"`
	Severity string          `json:"severity"`
	Findings []QualitySecretFinding `json:"findings"`
}

// SecretScanReply is the wire shape returned by GET /api/quality/secrets/{group}.
type SecretScanReply struct {
	Group         string              `json:"group"`
	TotalFindings int                 `json:"total_findings"`
	BySeverity    map[string]int      `json:"by_severity"`
	Files         []SecretFileRollup  `json:"files"`
	// ScannedRepos is the number of repos that were actually walked.
	ScannedRepos int `json:"scanned_repos"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler
// ─────────────────────────────────────────────────────────────────────────────

// handleQualitySecrets scans all repos in the requested group for hardcoded
// secrets and returns the aggregated report.
func (s *Server) handleQualitySecrets(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	if groupName == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	// Parse optional query parameters.
	severityFilter := r.URL.Query().Get("severity")
	maxSizeStr := r.URL.Query().Get("max_size")
	var maxSize int64
	if maxSizeStr != "" {
		v, err := strconv.ParseInt(maxSizeStr, 10, 64)
		if err == nil && v > 0 {
			maxSize = v
		}
	}

	// Resolve repos from the registry.
	repoPaths, err := repoPathsForGroup(groupName)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q: %v", groupName, err))
		return
	}
	if len(repoPaths) == 0 {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q has no repos", groupName))
		return
	}

	reply := SecretScanReply{
		Group:      groupName,
		BySeverity: map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0},
	}

	for _, rp := range repoPaths {
		findings, sErr := secrets.ScanPath(rp.Path, maxSize)
		if sErr != nil {
			// Non-fatal: continue with remaining repos.
			continue
		}
		reply.ScannedRepos++

		report := secrets.BuildReport(rp.Path, findings)
		for k, v := range report.BySeverity {
			reply.BySeverity[k] += v
		}
		reply.TotalFindings += report.TotalFindings

		for _, fr := range report.Files {
			if !passesSeverityFilter(secrets.Severity(fr.Severity), severityFilter) {
				continue
			}
			var apiFindings []QualitySecretFinding
			for _, f := range fr.Findings {
				if !passesSeverityFilter(f.Severity, severityFilter) {
					continue
				}
				apiFindings = append(apiFindings, QualitySecretFinding{
					File:            f.File,
					Line:            f.Line,
					Kind:            f.Kind,
					MaskedValue:     f.MaskedValue,
					Severity:        string(f.Severity),
					SuggestedEnvVar: f.SuggestedEnvVar,
				})
			}
			if len(apiFindings) == 0 {
				continue
			}
			reply.Files = append(reply.Files, SecretFileRollup{
				File:     fr.File,
				Repo:     rp.Slug,
				Count:    len(apiFindings),
				Severity: string(fr.Severity),
				Findings: apiFindings,
			})
		}
	}

	writeJSON(w, http.StatusOK, reply)
}

// passesSeverityFilter returns true when the finding's severity meets or
// exceeds the requested minimum severity filter.  An empty filter passes all.
func passesSeverityFilter(s secrets.Severity, filter string) bool {
	if filter == "" {
		return true
	}
	return secrets.SeverityRank(s) >= secrets.SeverityRank(secrets.Severity(filter))
}
