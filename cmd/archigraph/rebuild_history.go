package main

// rebuild_history.go — capture quality metrics after every daemon rebuild
// and persist them to health-history.jsonl (#1329).
//
// All metric collection is best-effort: errors are logged and silently
// skipped so a failed scan never blocks the rebuild response.

import (
	"fmt"
	"os"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/quality"
	"github.com/cajasmota/archigraph/internal/quality/audit"
	"github.com/cajasmota/archigraph/internal/registry"
	"github.com/cajasmota/archigraph/internal/secrets"
)

// appendRebuildHistory measures the current graph quality for a group and
// appends one HealthEntry to the JSONL history file.
//
// root is the daemon layout root (typically ~/.archigraph).
// group is the archigraph group name.
// cfg is the already-loaded group configuration.
// rebuiltPaths is the list of repo paths that were successfully indexed.
//
// The function is designed to run in a goroutine: all errors are surfaced
// via the returned error rather than panicking.
func appendRebuildHistory(root, group string, cfg *registry.GroupConfig, rebuiltPaths []string) error {
	entry := quality.HealthEntry{
		Timestamp: time.Now().UTC(),
		Group:     group,
	}

	// ── Core metrics (orphan + bug rate) ─────────────────────────────────────
	// Re-use the audit machinery that handleQualityComposite uses.
	totalEntities := 0
	totalOrphans := 0
	totalImports := 0
	goodImports := 0

	for _, repoPath := range rebuiltPaths {
		rep, err := audit.AuditPath(repoPath, false)
		if err != nil || len(rep.Repos) == 0 {
			continue
		}
		rr := rep.Repos[0]
		totalEntities += rr.Entities
		totalOrphans += rr.Orphans
		totalImports += rr.ImportsTotal
		goodImports += rr.ImportsToIDFormat[audit.ImportFormatHex] +
			rr.ImportsToIDFormat[audit.ImportFormatExtQualified]
	}

	entry.TotalEntities = totalEntities
	if totalEntities > 0 {
		entry.OrphanRate = 100.0 * float64(totalOrphans) / float64(totalEntities)
	}
	if totalImports > 0 {
		entry.BugRate = 100.0 * float64(totalImports-goodImports) / float64(totalImports)
	}
	entry.HealthScore = quality.ComputeHealthScore(entry.OrphanRate, entry.BugRate)

	// ── Coverage + cycles + entity counts ────────────────────────────────────
	// Walk all repos in the group and aggregate graph-level stats.
	totalProduction := 0
	coveredProduction := 0
	totalCycles := 0
	totalFlows := 0
	totalEndpoints := 0
	hasCoverage := false
	hasCycles := false

	for _, repoPath := range rebuiltPaths {
		stateDir := stateDirForRepoPath(repoPath)
		doc, err := loadGraphFromStateDir(stateDir)
		if err != nil || doc == nil {
			continue
		}

		// Coverage.
		cov := graph.ComputeCoverage(doc)
		hasCoverage = true
		totalProduction += cov.TotalProduction
		coveredProduction += cov.CoveredProduction

		// Cycles. Pass nil pagerank — uniform weights are fine for counting.
		cycles := graph.FindImportCycles(doc.Entities, doc.Relationships, nil)
		hasCycles = true
		totalCycles += len(cycles)

		// Entity type counts.
		for _, e := range doc.Entities {
			switch e.Kind {
			case "http_endpoint", "http_endpoint_definition", "http_endpoint_call":
				totalEndpoints++
			}
			if isProcessFlow(e) {
				totalFlows++
			}
		}
	}

	entry.TotalFlows = totalFlows
	entry.TotalEndpoints = totalEndpoints

	if hasCoverage && totalProduction > 0 {
		covPct := 100.0 * float64(coveredProduction) / float64(totalProduction)
		entry.CoveragePct = &covPct
	}
	if hasCycles {
		entry.Cycles = &totalCycles
	}

	// ── Auth-uncovered endpoint count ─────────────────────────────────────────
	// Count endpoints without an auth annotation by scanning relationship kinds.
	authUncovered := countAuthUncoveredEndpoints(rebuiltPaths)
	entry.AuthUncovered = &authUncovered

	// ── Secrets ──────────────────────────────────────────────────────────────
	// Run a quick secret scan across all repos. Cap per-file size at 256 KB to
	// bound memory; skip on scan error rather than failing the whole snapshot.
	const maxSecretFileSizeBytes = 256 * 1024
	totalSecrets := 0
	for _, repoPath := range rebuiltPaths {
		findings, err := secrets.ScanPath(repoPath, maxSecretFileSizeBytes)
		if err != nil {
			// Non-fatal: log and skip.
			fmt.Fprintf(os.Stderr, "archigraph: secret scan %s: %v (skipped)\n", repoPath, err)
			continue
		}
		totalSecrets += len(findings)
	}
	entry.Secrets = &totalSecrets

	return quality.AppendEntry(root, entry)
}

// isProcessFlow returns true for entity kinds that represent process flows.
func isProcessFlow(e graph.Entity) bool {
	switch e.Kind {
	case "process", "SCOPE.Process":
		return true
	}
	return len(e.Kind) > 14 && e.Kind[:14] == "SCOPE.Process."
}

// stateDirForRepoPath returns the daemon state directory for a repo path.
// Delegates to the same helper used by the daemon indexer.
func stateDirForRepoPath(repoPath string) string {
	return daemon.StateDirForRepo(repoPath)
}

// countAuthUncoveredEndpoints returns the number of http_endpoint entities
// that have no outgoing HAS_AUTH or REQUIRES_AUTH relationship across all
// rebuilt repos.
func countAuthUncoveredEndpoints(repoPaths []string) int {
	uncovered := 0
	for _, repoPath := range repoPaths {
		stateDir := stateDirForRepoPath(repoPath)
		doc, err := loadGraphFromStateDir(stateDir)
		if err != nil || doc == nil {
			continue
		}

		// Build set of endpoint IDs that have an auth edge.
		authed := make(map[string]bool, 16)
		for _, r := range doc.Relationships {
			if r.Kind == "HAS_AUTH" || r.Kind == "REQUIRES_AUTH" || r.Kind == "auth" {
				authed[r.FromID] = true
			}
		}
		// Count endpoint entities not in the authed set.
		for _, e := range doc.Entities {
			switch e.Kind {
			case "http_endpoint", "http_endpoint_definition":
				if !authed[e.ID] {
					uncovered++
				}
			}
		}
	}
	return uncovered
}
