// license_tools.go — MCP handler for grafel_license_audit (#1334).
//
// Reads ExternalPackage / external_dependency entities from loaded graph docs,
// enriches each with detected license metadata, flags incompatible combinations
// between the project license and each dependency license, and surfaces
// transitive GPL/AGPL chains.
//
// Severity mapping:
//
//	error  — strong-copyleft dep (GPL/AGPL) in a non-copyleft project
//	warn   — weak-copyleft dep (LGPL/MPL/EUPL/CDDL) or proprietary dep
//	info   — no conflict detected
//	unknown — license could not be resolved
//
// The project license is inferred from the repo root (LICENSE file, package.json,
// pyproject.toml, Cargo.toml).
//
// Registered MCP tool: grafel_license_audit
package mcp

import (
	"context"
	"fmt"
	"sort"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/licenses"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// handleLicenseAudit is the MCP handler for grafel_license_audit.
func (s *Server) handleLicenseAudit(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, nil)

	includeTransitive := argBool(req, "include_transitive", false)
	severityFilter := argString(req, "severity", "")
	limit := argInt(req, "limit", 500)

	if severityFilter != "" {
		valid := map[string]bool{"error": true, "warn": true, "info": true, "unknown": true}
		if !valid[severityFilter] {
			return mcpapi.NewToolResultError(
				fmt.Sprintf("invalid severity %q: must be error|warn|info|unknown", severityFilter)), nil
		}
	}

	type depOut struct {
		Repo           string   `json:"repo"`
		PackageName    string   `json:"package_name"`
		PackageManager string   `json:"package_manager"`
		Version        string   `json:"version"`
		License        string   `json:"license"`
		LicenseSource  string   `json:"license_source"`
		IsTransitive   bool     `json:"is_transitive"`
		ProjectLicense string   `json:"project_license"`
		Compatibility  string   `json:"compatibility"`
		Severity       string   `json:"severity"`
		Alternatives   []string `json:"alternatives,omitempty"`
	}

	type repoSummary struct {
		Repo                 string             `json:"repo"`
		ProjectLicense       string             `json:"project_license"`
		ProjectLicenseSource string             `json:"project_license_source"`
		TotalDeps            int                `json:"total_deps"`
		IncompatibleCount    int                `json:"incompatible_count"`
		LicenseDensity       map[string]float64 `json:"license_density,omitempty"`
	}

	bySeverity := map[string]int{"error": 0, "warn": 0, "info": 0, "unknown": 0}
	var allDeps []depOut
	var summaries []repoSummary
	scanned := 0

	for _, r := range repos {
		if r.Doc == nil || r.Path == "" {
			continue
		}

		// Collect ExternalPackage entities from the loaded graph.
		packageList := collectExternalPackages(r)
		if len(packageList) == 0 {
			continue
		}

		result, err := licenses.ScanRepoLicenses(r.Path, packageList, len(r.Doc.Entities))
		if err != nil {
			continue
		}
		scanned++

		incompatCount := 0
		for _, dep := range result.Dependencies {
			if dep.IsTransitive && !includeTransitive {
				continue
			}
			sev := compatToSeverity(dep.Compatibility)
			bySeverity[sev]++
			if sev == "error" || sev == "warn" {
				incompatCount++
			}
			if severityFilter != "" && sev != severityFilter {
				continue
			}
			allDeps = append(allDeps, depOut{
				Repo:           r.Repo,
				PackageName:    dep.PackageName,
				PackageManager: dep.PackageManager,
				Version:        dep.Version,
				License:        dep.License,
				LicenseSource:  dep.LicenseSource,
				IsTransitive:   dep.IsTransitive,
				ProjectLicense: result.ProjectLicense,
				Compatibility:  string(dep.Compatibility),
				Severity:       sev,
				Alternatives:   dep.Alternatives,
			})
		}
		summaries = append(summaries, repoSummary{
			Repo:                 r.Repo,
			ProjectLicense:       result.ProjectLicense,
			ProjectLicenseSource: result.ProjectLicenseSource,
			TotalDeps:            len(result.Dependencies),
			IncompatibleCount:    incompatCount,
			LicenseDensity:       result.LicenseDensity,
		})
	}

	// Sort: errors first, then warn, then info, then unknown;
	// within each bucket: repo, then package name.
	sort.SliceStable(allDeps, func(i, j int) bool {
		ri := licSeverityRank(allDeps[i].Severity)
		rj := licSeverityRank(allDeps[j].Severity)
		if ri != rj {
			return ri > rj
		}
		if allDeps[i].Repo != allDeps[j].Repo {
			return allDeps[i].Repo < allDeps[j].Repo
		}
		return allDeps[i].PackageName < allDeps[j].PackageName
	})

	total := len(allDeps)
	if limit > 0 && len(allDeps) > limit {
		allDeps = allDeps[:limit]
	}

	return jsonResult(map[string]any{
		"scanned_repos":  scanned,
		"total_findings": total,
		"truncated":      total > len(allDeps),
		"by_severity":    bySeverity,
		"repos":          summaries,
		"dependencies":   allDeps,
		"tip": "severity=error: strong-copyleft (GPL/AGPL) in non-copyleft project; " +
			"severity=warn: weak-copyleft (LGPL/MPL/EUPL/CDDL) or Proprietary dep; " +
			"use include_transitive=true to surface indirect npm chains. " +
			"alternatives field suggests permissive drop-in replacements.",
	}), nil
}

// collectExternalPackages extracts external_dependency entities from the
// loaded graph document and returns them as the package list format expected
// by licenses.ScanRepoLicenses.
func collectExternalPackages(r *LoadedRepo) []map[string]string {
	if r.Doc == nil {
		return nil
	}
	var out []map[string]string
	r.forEachEntity(func(e *graph.Entity) bool {
		if e.Subtype != "external_dependency" {
			return true
		}
		pm := e.PropGet("package_manager")
		ver := e.PropGet("version")
		if pm == "" {
			return true
		}
		out = append(out, map[string]string{
			"name":            e.Name,
			"package_manager": pm,
			"version":         ver,
		})
		return true
	})
	return out
}

// compatToSeverity maps a licenses.CompatibilityLevel to an MCP severity string.
func compatToSeverity(c licenses.CompatibilityLevel) string {
	switch c {
	case licenses.CompatError:
		return "error"
	case licenses.CompatWarn:
		return "warn"
	case licenses.CompatUnknown:
		return "unknown"
	default:
		return "info"
	}
}

// licSeverityRank returns a numeric rank for sorting (higher = more severe).
func licSeverityRank(s string) int {
	switch s {
	case "error":
		return 3
	case "warn":
		return 2
	case "info":
		return 1
	}
	return 0
}
