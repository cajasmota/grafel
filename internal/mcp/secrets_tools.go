// secrets_tools.go — MCP handler for archigraph_secrets (#1322).
//
// Walks source files in every loaded repo and flags hardcoded credentials:
// API keys, passwords, JWT tokens, AWS credentials, private keys.
//
// Suppression rules:
//   - Files in test directories (/test/, /tests/, /testdata/, *.test.*, etc.)
//   - Lines with the opt-out comment: // archigraph: ignore-secret
//   - Values that match common placeholder patterns (example, REPLACE_ME, etc.)
//
// Findings are severity-graded (critical → high → medium → low) and include a
// masked value + suggested environment variable name.
package mcp

import (
	"context"
	"fmt"
	"sort"

	"github.com/cajasmota/archigraph/internal/secrets"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// handleSecrets is the MCP handler for archigraph_secrets.
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

		findings, err := secrets.ScanPath(repoPath, 0)
		if err != nil {
			continue // non-fatal: skip unreadable repos
		}
		scannedRepos++

		for _, f := range findings {
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
		"tip":            "Add '// archigraph: ignore-secret' to suppress a specific line. Replace hardcoded values with the suggested env var.",
	}), nil
}
