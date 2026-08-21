// secrets_tools.go — MCP handler for grafel_secrets (#1322).
//
// Walks source files in every loaded repo and flags hardcoded credentials:
// API keys, passwords, JWT tokens, AWS credentials, private keys.
//
// Suppression rules:
//   - Files in test directories (/test/, /tests/, /testdata/, *.test.*, etc.)
//   - Lines with the opt-out comment: // grafel: ignore-secret
//   - Values that match common placeholder patterns (example, REPLACE_ME, etc.)
//
// Findings are severity-graded (critical → high → medium → low) and include a
// masked value + suggested environment variable name.
package mcp

import (
	"context"
	"fmt"
	"sort"

	"github.com/cajasmota/grafel/internal/secrets"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// scanSecrets is the seam #6483's payload test drives.
//
// It exists because the assertion that matters — "a skipped file reaches the
// MCP client" — must run on every GOOS, and the only real way to produce a
// skip is a FIFO, which cannot be created on windows-latest. Stubbing here
// keeps the payload contract under test on all three CI platforms; the real
// scanner's own behaviour is pinned separately in internal/secrets.
var scanSecrets = secrets.ScanPath

// handleSecrets is the MCP handler for grafel_secrets.
func (s *Server) handleSecrets(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, nil) // scan all repos in the group
	limit := argInt(req, "limit", 200)
	severityFilter := argString(req, "severity", "")

	// Validate severity filter if provided.
	if severityFilter != "" {
		valid := map[string]bool{"critical": true, "high": true, "medium": true, "low": true}
		if !valid[severityFilter] {
			return mcpapi.NewToolResultError(fmt.Sprintf("invalid severity %q: must be critical|high|medium|low", severityFilter)), nil
		}
	}

	type findingOut struct {
		Repo            string `json:"repo"`
		File            string `json:"file"`
		Line            int    `json:"line"`
		Kind            string `json:"kind"`
		MaskedValue     string `json:"masked_value"`
		Severity        string `json:"severity"`
		SuggestedEnvVar string `json:"suggested_env_var"`
	}

	type rollupOut struct {
		Repo     string       `json:"repo"`
		File     string       `json:"file"`
		Count    int          `json:"count"`
		Severity string       `json:"severity"`
		Findings []findingOut `json:"findings"`
	}

	// skipOut is a file the scan did NOT read (#6483). Without it this
	// payload answers "is this repo clean?" with an unqualified yes while a
	// file in the tree was never opened: the scanner's own report goes to
	// stderr, which in the daemon is the daemon log the MCP client never
	// reads.
	type skipOut struct {
		Repo   string `json:"repo"`
		File   string `json:"file"`
		Reason string `json:"reason"`
		Kind   string `json:"kind,omitempty"`
	}
	skippedFiles := []skipOut{}

	bySeverity := map[string]int{
		"critical": 0,
		"high":     0,
		"medium":   0,
		"low":      0,
	}

	var allFindings []findingOut
	scannedRepos := 0

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		// The repo path lives in the registry config; we get it from the
		// LoadedRepo.Path field. If the doc was loaded, the path is valid.
		repoPath := r.Path
		if repoPath == "" {
			continue
		}

		scan, err := scanSecrets(repoPath, 0)
		if err != nil {
			continue // non-fatal: skip unreadable repos
		}
		scannedRepos++

		for _, sk := range scan.Skipped {
			skippedFiles = append(skippedFiles, skipOut{
				Repo:   r.Repo,
				File:   sk.Rel,
				Reason: sk.Reason,
				Kind:   sk.Kind,
			})
		}

		for _, f := range scan.Findings {
			bySeverity[string(f.Severity)]++
			if severityFilter != "" &&
				secrets.SeverityRank(f.Severity) < secrets.SeverityRank(secrets.Severity(severityFilter)) {
				continue
			}
			allFindings = append(allFindings, findingOut{
				Repo:            r.Repo,
				File:            f.File,
				Line:            f.Line,
				Kind:            f.Kind,
				MaskedValue:     f.MaskedValue,
				Severity:        string(f.Severity),
				SuggestedEnvVar: f.SuggestedEnvVar,
			})
		}
	}

	// Sort by severity descending, then repo, then file, then line.
	sort.SliceStable(allFindings, func(i, j int) bool {
		ri := secrets.SeverityRank(secrets.Severity(allFindings[i].Severity))
		rj := secrets.SeverityRank(secrets.Severity(allFindings[j].Severity))
		if ri != rj {
			return ri > rj
		}
		if allFindings[i].Repo != allFindings[j].Repo {
			return allFindings[i].Repo < allFindings[j].Repo
		}
		if allFindings[i].File != allFindings[j].File {
			return allFindings[i].File < allFindings[j].File
		}
		return allFindings[i].Line < allFindings[j].Line
	})

	total := len(allFindings)
	if limit > 0 && len(allFindings) > limit {
		allFindings = allFindings[:limit]
	}

	// Group into per-file rollups for readability.
	type fileKey struct{ repo, file string }
	rollupMap := map[fileKey][]findingOut{}
	for _, f := range allFindings {
		k := fileKey{f.Repo, f.File}
		rollupMap[k] = append(rollupMap[k], f)
	}
	keys := make([]fileKey, 0, len(rollupMap))
	for k := range rollupMap {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].repo != keys[j].repo {
			return keys[i].repo < keys[j].repo
		}
		return keys[i].file < keys[j].file
	})
	rollups := make([]rollupOut, 0, len(keys))
	for _, k := range keys {
		ff := rollupMap[k]
		highest := secrets.Severity("low")
		for _, f := range ff {
			if secrets.SeverityRank(secrets.Severity(f.Severity)) > secrets.SeverityRank(highest) {
				highest = secrets.Severity(f.Severity)
			}
		}
		rollups = append(rollups, rollupOut{
			Repo:     k.repo,
			File:     k.file,
			Count:    len(ff),
			Severity: string(highest),
			Findings: ff,
		})
	}

	return jsonResult(map[string]any{
		"scanned_repos":  scannedRepos,
		"total_findings": total,
		"truncated":      total > len(allFindings),
		"by_severity":    bySeverity,
		"files":          rollups,
		// skipped_files is what makes "total_findings: 0" interpretable: a
		// non-empty list means the answer is "clean, and N files were not
		// read", which is not the same answer (#6483).
		"skipped_files": skippedFiles,
		"tip":           "Add '// grafel: ignore-secret' to suppress a specific line. Replace hardcoded values with the suggested env var.",
	}), nil
}
