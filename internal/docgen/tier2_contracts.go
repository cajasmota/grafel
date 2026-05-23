// Package docgen — Tier 2 cross-page contract checks (issue #1760).
//
// This file implements the four cross-page contracts that Tier 2 enforces
// across a slice of pages:
//
//  1. checkFlowDuplication  — same flow (mermaid block body) in 2+ pages.
//  2. checkPatternLinks     — pattern entity mentioned in one page but absent
//     from related pages.
//  3. checkAnchorConsistency — anchor format mismatches (<entity-id>#<section>
//     shape is required for cross-page links).
//  4. checkSliceMermaidBudget — total mermaid count across the slice exceeds
//     the configured budget.
package docgen

import (
	"fmt"
	"regexp"
	"strings"
)

// Violation is a single cross-page contract failure.
type Violation struct {
	// Kind is one of: "flow-duplication", "pattern-link",
	// "anchor-consistency", "mermaid-budget".
	Kind string
	// Message is a human-readable description.
	Message string
	// PageA and PageB identify the pages involved (entity IDs).
	PageA string
	PageB string
}

// CheckSliceContracts runs all four cross-page contract checks against the
// slice and returns the combined violation list.
// Exported so CLI and tests can call it directly.
func CheckSliceContracts(pages []PageOutput, mermaidBudget int) []Violation {
	var out []Violation
	out = append(out, checkFlowDuplication(pages)...)
	out = append(out, checkPatternLinks(pages)...)
	out = append(out, checkAnchorConsistency(pages)...)
	out = append(out, checkSliceMermaidBudget(pages, mermaidBudget)...)
	return out
}

// ---------------------------------------------------------------------------
// 1. Flow duplication
// ---------------------------------------------------------------------------

// flowBodyRE extracts the body of a mermaid fenced block.
var flowBodyRE = regexp.MustCompile("(?s)```mermaid\n(.*?)```")

// checkFlowDuplication detects the same mermaid block body appearing in 2+
// pages of the slice. Identical flows indicate over-fragmented documentation
// (the disconnected-flows merge problem from the audit).
func checkFlowDuplication(pages []PageOutput) []Violation {
	type occurrence struct {
		pageID string
	}
	// Map from trimmed mermaid body → list of pages that contain it.
	seen := make(map[string][]string)
	for _, p := range pages {
		for _, m := range flowBodyRE.FindAllStringSubmatch(p.MD, -1) {
			body := strings.TrimSpace(m[1])
			if body == "" {
				continue
			}
			seen[body] = append(seen[body], p.EntityID)
		}
	}

	var violations []Violation
	for body, pageIDs := range seen {
		if len(pageIDs) < 2 {
			continue
		}
		// Deduplicate page IDs (a page could embed the same block twice).
		unique := deduplicateStrings(pageIDs)
		if len(unique) < 2 {
			continue
		}
		// Emit one violation per pair.
		for i := 0; i < len(unique)-1; i++ {
			for j := i + 1; j < len(unique); j++ {
				snippet := body
				if len(snippet) > 60 {
					snippet = snippet[:60] + "…"
				}
				violations = append(violations, Violation{
					Kind:    "flow-duplication",
					Message: fmt.Sprintf("identical flow block in pages %q and %q: %q", unique[i], unique[j], snippet),
					PageA:   unique[i],
					PageB:   unique[j],
				})
			}
		}
	}
	return violations
}

// ---------------------------------------------------------------------------
// 2. Pattern link check
// ---------------------------------------------------------------------------

// patternEntityRE matches lines in the "patterns" section that declare a
// pattern entity: "- **<EntityName>**" or "* <EntityName>" style bullets.
// We look for bold entity references which are the standard pattern-section format.
var patternEntityRE = regexp.MustCompile(`\*\*([A-Za-z][A-Za-z0-9_\-\.]+)\*\*`)

// sectionBodyRE extracts the body of a named section from a tier1-generated page.
// A section starts at <a id="<slug>"></a> and ends at the next <a id= or end of string.
var sectionBodyRE = regexp.MustCompile(`(?s)<a id="([^"]+)"></a>\s*(.*?)(?:<a id="|$)`)

// checkPatternLinks flags when a bold pattern entity reference appears in the
// "patterns" section of one page but there is no mention of that entity name
// anywhere in any other page of the slice.
func checkPatternLinks(pages []PageOutput) []Violation {
	// Extract "patterns" section body per page.
	type pagePatterns struct {
		pageID   string
		patterns []string // entity names declared in the patterns section
	}
	var allPagePatterns []pagePatterns

	for _, p := range pages {
		patternsBody := extractSectionBody(p.MD, "patterns")
		if patternsBody == "" {
			continue
		}
		var names []string
		for _, m := range patternEntityRE.FindAllStringSubmatch(patternsBody, -1) {
			names = append(names, m[1])
		}
		if len(names) > 0 {
			allPagePatterns = append(allPagePatterns, pagePatterns{
				pageID:   p.EntityID,
				patterns: deduplicateStrings(names),
			})
		}
	}

	var violations []Violation
	for _, pp := range allPagePatterns {
		for _, name := range pp.patterns {
			// Check whether this pattern entity name appears in any OTHER page.
			mentionedElsewhere := false
			for _, other := range pages {
				if other.EntityID == pp.pageID {
					continue
				}
				if strings.Contains(other.MD, name) {
					mentionedElsewhere = true
					break
				}
			}
			if !mentionedElsewhere && len(pages) > 1 {
				violations = append(violations, Violation{
					Kind:    "pattern-link",
					Message: fmt.Sprintf("pattern entity %q declared in page %q is unlinked from all other slice pages", name, pp.pageID),
					PageA:   pp.pageID,
				})
			}
		}
	}
	return violations
}

// extractSectionBody returns the markdown body of the section with the given
// slug from a tier1-assembled page.
func extractSectionBody(md, sectionSlug string) string {
	for _, m := range sectionBodyRE.FindAllStringSubmatch(md, -1) {
		if m[1] == sectionSlug {
			return m[2]
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// 3. Anchor consistency
// ---------------------------------------------------------------------------

// crossPageAnchorRE matches markdown links of the form [text](target#section)
// where target is non-empty and does not contain "://".
var crossPageAnchorRE = regexp.MustCompile(`\[([^\]]+)\]\(([^):]+#[^)]+)\)`)

// validAnchorRE defines the required format: `<entity-id>#<section>` where
// entity-id is alphanumeric+dash/underscore and section is a known section slug.
var validAnchorRE = regexp.MustCompile(`^[A-Za-z0-9_\-]+#[A-Za-z][A-Za-z0-9\-]*$`)

// checkAnchorConsistency validates that all cross-page anchor links in the
// slice use the canonical `<entity-id>#<section>` format.
func checkAnchorConsistency(pages []PageOutput) []Violation {
	var violations []Violation
	for _, p := range pages {
		for _, m := range crossPageAnchorRE.FindAllStringSubmatch(p.MD, -1) {
			target := m[2]
			if strings.Contains(target, "://") {
				continue // absolute URL — not a cross-page anchor
			}
			if !validAnchorRE.MatchString(target) {
				violations = append(violations, Violation{
					Kind:    "anchor-consistency",
					Message: fmt.Sprintf("page %q has malformed cross-page anchor %q (want: <entity-id>#<section>)", p.EntityID, target),
					PageA:   p.EntityID,
				})
			}
		}
	}
	return violations
}

// ---------------------------------------------------------------------------
// 4. Mermaid budget
// ---------------------------------------------------------------------------

// checkSliceMermaidBudget flags when the total mermaid block count across the
// slice exceeds the configured budget (default MermaidBudgetSlice = 15).
func checkSliceMermaidBudget(pages []PageOutput, budget int) []Violation {
	total := 0
	for _, p := range pages {
		total += strings.Count(p.MD, "```mermaid")
	}
	if total <= budget {
		return nil
	}
	return []Violation{{
		Kind:    "mermaid-budget",
		Message: fmt.Sprintf("slice has %d total mermaid blocks (budget: %d)", total, budget),
	}}
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

// deduplicateStrings returns a new slice with duplicate strings removed,
// preserving first-occurrence order.
func deduplicateStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
