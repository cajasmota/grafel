// Package docgen — Tier 3 repo-level contract checks (issue #1760).
//
// Three contracts are enforced at the repo level:
//
//  1. checkRepoCoverage  — every page-worthy entity has a home page.
//  2. checkPageOwnership — no two pages claim the same entity as primary.
//  3. checkRepoIndex     — repo index.md exists and links to every generated page.
package docgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RepoViolation is a single repo-level contract failure.
type RepoViolation struct {
	// Kind is one of: "repo-coverage", "page-ownership", "repo-index".
	Kind string
	// Message is a human-readable description.
	Message string
	// EntityID is the entity ID involved (when applicable).
	EntityID string
	// PageA and PageB identify the conflicting pages (for ownership conflicts).
	PageA string
	PageB string
}

// CheckRepoContracts runs all three repo-level contract checks and returns the
// combined violation list.
// Exported so CLI and tests can call it directly.
func CheckRepoContracts(repoSlug string, pageWorthyIDs map[string]bool, pages []PageOutput, indexPath string) []RepoViolation {
	var out []RepoViolation
	out = append(out, checkRepoCoverage(repoSlug, pageWorthyIDs, pages)...)
	out = append(out, checkPageOwnership(pages)...)
	out = append(out, checkRepoIndex(pages, indexPath)...)
	return out
}

// ---------------------------------------------------------------------------
// 1. Repo coverage
// ---------------------------------------------------------------------------

// checkRepoCoverage verifies that every page-worthy entity has been covered by
// at least one generated page. An entity is "covered" if its ID appears as the
// primary EntityID of some page in the generated set.
func checkRepoCoverage(_ string, pageWorthyIDs map[string]bool, pages []PageOutput) []RepoViolation {
	// Build the set of covered entity IDs.
	covered := make(map[string]bool, len(pages))
	for _, p := range pages {
		covered[p.EntityID] = true
	}

	var violations []RepoViolation
	for id := range pageWorthyIDs {
		if !covered[id] {
			violations = append(violations, RepoViolation{
				Kind:     "repo-coverage",
				Message:  fmt.Sprintf("page-worthy entity %q has no home page in the generated doc set", id),
				EntityID: id,
			})
		}
	}
	// Sort by entity ID for deterministic output.
	sortRepoViolationsByEntityID(violations)
	return violations
}

// ---------------------------------------------------------------------------
// 2. Page ownership
// ---------------------------------------------------------------------------

// checkPageOwnership verifies that no two pages claim the same entity as their
// primary entity (EntityID). Each entity should have exactly one home page.
func checkPageOwnership(pages []PageOutput) []RepoViolation {
	// Map from entity ID → first page that claimed it.
	ownership := make(map[string]string, len(pages))
	var violations []RepoViolation

	for _, p := range pages {
		if p.EntityID == "" {
			continue
		}
		if first, exists := ownership[p.EntityID]; exists {
			violations = append(violations, RepoViolation{
				Kind:     "page-ownership",
				Message:  fmt.Sprintf("entity %q is claimed as primary by both page %q and page %q", p.EntityID, first, p.MDPath),
				EntityID: p.EntityID,
				PageA:    first,
				PageB:    p.MDPath,
			})
		} else {
			ownership[p.EntityID] = p.MDPath
		}
	}
	return violations
}

// ---------------------------------------------------------------------------
// 3. Repo index
// ---------------------------------------------------------------------------

// checkRepoIndex verifies that the repo index.md exists and contains a link to
// every generated page (by checking that each entity ID's canonical filename
// appears in the index content).
func checkRepoIndex(pages []PageOutput, indexPath string) []RepoViolation {
	var violations []RepoViolation

	// Index must exist.
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		return []RepoViolation{{
			Kind:    "repo-index",
			Message: fmt.Sprintf("repo index.md not found at %q: %v", indexPath, err),
		}}
	}
	indexContent := string(indexData)

	// Every page must have a link in the index.
	for _, p := range pages {
		// The canonical link target in the index is "<entity-id-sanitized>-page.md".
		expectedFilename := sanitizeFilename(p.EntityID) + "-page.md"
		if !strings.Contains(indexContent, expectedFilename) {
			violations = append(violations, RepoViolation{
				Kind:     "repo-index",
				Message:  fmt.Sprintf("repo index.md is missing a link to page %q (expected filename %q)", p.EntityID, expectedFilename),
				EntityID: p.EntityID,
			})
		}
	}

	// Also verify the index links to score.json (convention check).
	if !strings.Contains(indexContent, "score.json") {
		violations = append(violations, RepoViolation{
			Kind:    "repo-index",
			Message: "repo index.md does not link to score.json",
		})
	}

	// Verify index is in the same directory as the pages.
	indexDir := filepath.Dir(indexPath)
	for _, p := range pages {
		if p.MDPath == "" {
			continue
		}
		if filepath.Dir(p.MDPath) != indexDir {
			violations = append(violations, RepoViolation{
				Kind:     "repo-index",
				Message:  fmt.Sprintf("page %q is not in the same directory as index.md (index dir: %q, page dir: %q)", p.EntityID, indexDir, filepath.Dir(p.MDPath)),
				EntityID: p.EntityID,
			})
		}
	}

	return violations
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

// sortRepoViolationsByEntityID sorts violations by entity ID for determinism.
func sortRepoViolationsByEntityID(vs []RepoViolation) {
	// Simple insertion sort — violation slices are small.
	for i := 1; i < len(vs); i++ {
		for j := i; j > 0 && vs[j].EntityID < vs[j-1].EntityID; j-- {
			vs[j], vs[j-1] = vs[j-1], vs[j]
		}
	}
}
