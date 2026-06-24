// Package personas contains structural validation tests for grafel persona files.
//
// Each persona lives in skills/grafel-consult/personas/*.md and must satisfy the
// invariants defined here. These tests act as a CI gate preventing template drift.
package personas

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// personaDir returns the absolute path to skills/grafel-consult/personas/
// relative to this test file's source location (two levels up from internal/personas/).
func personaDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile = .../internal/personas/validate_test.go
	// repo root = two directories up
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	dir := filepath.Join(repoRoot, "skills", "grafel-consult", "personas")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("cannot resolve persona dir: %v", err)
	}
	return abs
}

// allowedModels contains every accepted value for the "model" frontmatter key.
// Empty string is allowed (model may be omitted).
var allowedModels = map[string]bool{
	"opus":   true,
	"sonnet": true,
	"haiku":  true,
	"":       true,
}

// requiredSectionsPrimary are sections every non-limited persona must have, IN ORDER.
// "Limited" personas (those with ## Current-state limitations) use a slightly different
// READ heading and a custom save block — see requiredSectionsLimited below.
var requiredSectionsPrimary = []string{
	"## Role",
	"## READ Protocol",
	"## ANALYSIS lens",
	"## Communication styles for this domain",
	"## When to ask for an expert (Consult-Out)",
	"## Response shape",
	"## When the user asks to save this analysis",
	"## Lifecycle telemetry",
}

// requiredSectionsLimited are sections a limited persona must have (Current-state
// limitations present, READ heading is "## READ instructions" not "## READ Protocol").
var requiredSectionsLimited = []string{
	"## Current-state limitations",
	"## Role",
	"## READ instructions",
	"## ANALYSIS lens",
	"## Communication styles for this domain",
	"## When to ask for an expert (Consult-Out)",
	"## Response shape",
	"## When the user asks to save this analysis",
	"## Lifecycle telemetry",
}

// sharedSkillReadRef is the exact substring that must appear in the READ Protocol body
// of primary (non-limited) personas.
const sharedSkillReadRef = "grafel-graph-read"

// sharedSkillWriteRef is the exact substring that must appear in the save section body
// of primary personas.
const sharedSkillWriteRef = "grafel-graph-write"

// telemetryCall is the MCP tool that must appear in every persona's Lifecycle telemetry section.
// Post-consolidation (#5546), the dedicated grafel_persona_event tool was absorbed into the
// canonical grafel_event tool (discriminated by kind=persona).
const telemetryCall = "grafel_event"

// frontmatter holds the parsed YAML frontmatter of a persona file.
type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Model       string `yaml:"model"`
}

// parseFrontmatter extracts the YAML between the opening --- and closing --- delimiters.
// Returns the parsed struct and the remaining body (after the closing ---).
func parseFrontmatter(t *testing.T, filename, content string) (frontmatter, string) {
	t.Helper()
	if !strings.HasPrefix(content, "---") {
		t.Errorf("%s: file does not start with YAML frontmatter delimiter ---", filename)
		return frontmatter{}, content
	}
	// Find the closing ---
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx == -1 {
		t.Errorf("%s: no closing --- found for YAML frontmatter", filename)
		return frontmatter{}, content
	}
	rawYAML := rest[:idx]
	body := rest[idx+4:] // skip \n---

	var fm frontmatter
	if err := yaml.Unmarshal([]byte(rawYAML), &fm); err != nil {
		t.Errorf("%s: frontmatter YAML parse error: %v", filename, err)
	}
	return fm, body
}

// sectionOffsets returns a map from section heading (e.g. "## Role") to the byte offset
// at which it first appears in body. Headings must be at the start of a line.
func sectionOffsets(body string) map[string]int {
	offsets := make(map[string]int)
	lines := strings.Split(body, "\n")
	pos := 0
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		if strings.HasPrefix(trimmed, "## ") {
			if _, seen := offsets[trimmed]; !seen {
				offsets[trimmed] = pos
			}
		}
		pos += len(line) + 1 // +1 for the newline
	}
	return offsets
}

// sectionBody returns the text between section heading `from` and the next `##` heading
// (or end of body), given the full body string.
func sectionBody(body, heading string) string {
	headingRe := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(heading) + `\s*$`)
	loc := headingRe.FindStringIndex(body)
	if loc == nil {
		return ""
	}
	after := body[loc[1]:]
	// Find the next ## heading
	nextRe := regexp.MustCompile(`(?m)^## `)
	next := nextRe.FindStringIndex(after)
	if next == nil {
		return after
	}
	return after[:next[0]]
}

// TestPersonaStructuralInvariants walks all *.md files in the personas directory and
// asserts that each satisfies the structural contract.
func TestPersonaStructuralInvariants(t *testing.T) {
	dir := personaDir(t)
	entries, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no persona .md files found in %s", dir)
	}

	t.Logf("validating %d persona files in %s", len(entries), dir)

	for _, path := range entries {
		path := path // capture
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("cannot read file: %v", err)
			}
			content := string(raw)

			// ── 1. Parse + validate frontmatter ──────────────────────────────────
			fm, body := parseFrontmatter(t, name, content)

			if fm.Name == "" {
				t.Errorf("frontmatter key 'name' is missing or empty")
			}
			if fm.Description == "" {
				t.Errorf("frontmatter key 'description' is missing or empty")
			}
			if !allowedModels[fm.Model] {
				t.Errorf("frontmatter key 'model' has unrecognized value %q (allowed: opus, sonnet, haiku, or omitted)", fm.Model)
			}

			// ── 2. Determine persona class ────────────────────────────────────────
			isLimited := strings.Contains(body, "## Current-state limitations")

			// ── 3. Assert required sections exist in the correct order ────────────
			requiredSections := requiredSectionsPrimary
			if isLimited {
				requiredSections = requiredSectionsLimited
			}

			offsets := sectionOffsets(body)

			prevOffset := -1
			prevHeading := "(start)"
			for _, heading := range requiredSections {
				offset, found := offsets[heading]
				if !found {
					t.Errorf("required section %q not found", heading)
					continue
				}
				if offset <= prevOffset {
					t.Errorf("section order violation: %q must appear after %q", heading, prevHeading)
				}
				prevOffset = offset
				prevHeading = heading
			}

			// ── 4. Shared-skill reference wording (primary personas only) ─────────
			if !isLimited {
				readBody := sectionBody(body, "## READ Protocol")
				if !strings.Contains(readBody, sharedSkillReadRef) {
					t.Errorf("## READ Protocol section must reference %q but does not", sharedSkillReadRef)
				}

				saveBody := sectionBody(body, "## When the user asks to save this analysis")
				if !strings.Contains(saveBody, sharedSkillWriteRef) {
					t.Errorf("## When the user asks to save this analysis section must reference %q but does not", sharedSkillWriteRef)
				}
			}

			// ── 5. Lifecycle telemetry calls grafel_persona_event ─────────────
			telemetryBody := sectionBody(body, "## Lifecycle telemetry")
			if telemetryBody == "" {
				t.Errorf("## Lifecycle telemetry section is empty or missing")
			} else if !strings.Contains(telemetryBody, telemetryCall) {
				t.Errorf("## Lifecycle telemetry must call %q but does not", telemetryCall)
			}
		})
	}
}

// TestPersonaFileCount asserts the expected number of persona files exists so that
// accidentally deleted personas fail CI instead of silently disappearing.
func TestPersonaFileCount(t *testing.T) {
	const expectedCount = 12
	dir := personaDir(t)
	entries, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(entries) != expectedCount {
		t.Errorf("expected %d persona files, found %d — add/remove the expectation if the count is intentionally changing", expectedCount, len(entries))
	}
}
