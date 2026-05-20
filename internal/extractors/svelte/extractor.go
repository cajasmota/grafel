// Package svelte implements a regex-based extractor for Svelte single-file
// components (.svelte files).
//
// A Svelte SFC has three optional sections:
//
//	<script [lang="ts"]> ... </script>   — component logic
//	HTML template                        — markup with Svelte directives
//	<style> ... </style>                 — scoped CSS
//
// Extracted entities:
//
//	Whole file               → SCOPE.Component    subtype="svelte_component"
//	export let <prop>        → SCOPE.Operation    subtype="prop"
//	let <x> = $state(…)      → SCOPE.Operation    subtype="rune_state"
//	$derived(…) declaration  → SCOPE.Operation    subtype="rune_derived"
//	$effect(…) call          → SCOPE.Operation    subtype="rune_effect"
//	<ChildComponent />       → RENDERS edge
//
// Svelte 5 runes ($state, $derived, $effect, $props, $bindable) are
// recognised; Svelte 4 lifecycle helpers (onMount, onDestroy, …) and stores
// (writable, readable, derived, get) are captured as CALLS edges via the
// resolver slice (dynamic_patterns_svelte.go).
//
// Registers itself via init() and is imported by registry_gen.go.
package svelte

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("svelte", &Extractor{})
}

// Extractor implements extractor.Extractor for Svelte SFC files.
type Extractor struct{}

// Language returns the canonical language key.
func (e *Extractor) Language() string { return "svelte" }

// ── compiled regexps ────────────────────────────────────────────────────────

var (
	// scriptBlockRE captures the content between <script …> and </script>.
	// Handles optional lang="ts" / lang="js" attributes. Non-greedy inner
	// match so nested tags in template are not consumed.
	scriptBlockRE = regexp.MustCompile(`(?si)<script(?:[^>]*)>(.*?)</script>`)

	// exportLetRE matches `export let propName` declarations inside a <script>
	// block.  Captures the property name.
	// Examples: `export let count = 0`, `export let label: string`
	exportLetRE = regexp.MustCompile(`(?m)^\s*export\s+let\s+([A-Za-z_$][A-Za-z0-9_$]*)`)

	// stateRuneRE matches `let name = $state(…)` (Svelte 5 rune).
	// Captures the variable name.
	stateRuneRE = regexp.MustCompile(`(?m)^\s*(?:let|const)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*\$state\s*\(`)

	// derivedRuneRE matches `let name = $derived(…)` (Svelte 5 rune).
	// Captures the variable name.
	derivedRuneRE = regexp.MustCompile(`(?m)^\s*(?:let|const)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*\$derived\s*\(`)

	// effectRuneRE matches a top-level `$effect(…)` call (Svelte 5 rune).
	// Captures nothing beyond the match itself — effects are anonymous.
	effectRuneRE = regexp.MustCompile(`(?m)^\s*\$effect\s*\(`)

	// propsRuneRE matches `const { a, b } = $props()` or `let props = $props()`.
	// Captures the text between `{` and `}` for destructured forms, or the
	// bare binding name for the non-destructured form.
	propsRuneRE = regexp.MustCompile(`(?m)^\s*(?:let|const)\s+(?:\{([^}]*)\}|([A-Za-z_$][A-Za-z0-9_$]*))\s*=\s*\$props\s*\(`)

	// bindableRuneRE matches `let x = $bindable(…)` (Svelte 5 rune).
	bindableRuneRE = regexp.MustCompile(`(?m)^\s*(?:let|const)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*\$bindable\s*\(`)

	// childComponentRE finds PascalCase component tags in the template section.
	// Matches both self-closing `<Foo />` and opening `<Foo>` / `<Foo ...>`.
	// PascalCase tag → Svelte component by convention (lower-case → HTML element).
	childComponentRE = regexp.MustCompile(`<([A-Z][A-Za-z0-9]*)\b`)
)

// Extract parses the Svelte SFC source and returns entity records.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor.svelte")
	ctx, span := tracer.Start(ctx, "indexer.extract.svelte",
		trace.WithAttributes(attribute.String("language", "svelte")),
	)
	defer span.End()

	_ = ctx // used only for span

	src := string(file.Content)
	if len(src) == 0 {
		span.SetAttributes(
			attribute.Int("file_line_count", 0),
			attribute.Int("entity_count", 0),
		)
		return nil, nil
	}

	lineCount := strings.Count(src, "\n") + 1
	componentName := componentNameFromPath(file.Path)

	var entities []types.EntityRecord

	// ── 1. Whole-file component entity ───────────────────────────────────────
	componentEntity := types.EntityRecord{
		Name:         componentName,
		Kind:         "SCOPE.Component",
		Subtype:      "svelte_component",
		SourceFile:   file.Path,
		Language:     "svelte",
		StartLine:    1,
		EndLine:      lineCount,
		Signature:    componentName + ".svelte",
		QualityScore: 0.85,
	}
	entities = append(entities, componentEntity)

	// ── 2. Extract <script> block ─────────────────────────────────────────────
	scriptContent, scriptStartLine := extractScriptBlock(src)
	if scriptContent != "" {
		scriptEntities := extractScriptEntities(scriptContent, scriptStartLine, file.Path)
		entities = append(entities, scriptEntities...)
	}

	// ── 3. Extract RENDERS edges from child components in the template ────────
	templateContent, templateStartLine := extractTemplateSection(src)
	if templateContent != "" {
		renderRels := extractChildComponents(templateContent, templateStartLine, file.Path, componentName)
		if len(renderRels) > 0 {
			// Attach RENDERS relationships to the component entity.
			entities[0].Relationships = append(entities[0].Relationships, renderRels...)
		}
	}

	span.SetAttributes(
		attribute.Int("file_line_count", lineCount),
		attribute.Int("entity_count", len(entities)),
	)
	return entities, nil
}

// componentNameFromPath derives the Svelte component name from the file path.
// e.g. "src/lib/MyButton.svelte" → "MyButton"
func componentNameFromPath(path string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, ".svelte")
	if name == "" {
		return "Component"
	}
	return name
}

// extractScriptBlock finds the first <script …> … </script> block and returns
// its inner content and the 1-based line number of the first content line.
func extractScriptBlock(src string) (string, int) {
	m := scriptBlockRE.FindStringSubmatchIndex(src)
	if m == nil {
		return "", 0
	}
	// m[2]..m[3] is the inner content (capture group 1)
	inner := src[m[2]:m[3]]
	// Count lines before the start of the inner content.
	startLine := strings.Count(src[:m[2]], "\n") + 1
	return inner, startLine
}

// extractTemplateSection returns the non-script, non-style portion of the
// file (the HTML template) along with its first content line number.
// This is a best-effort extraction: we strip <script> and <style> blocks.
func extractTemplateSection(src string) (string, int) {
	// Remove all <script …>…</script> blocks.
	stripped := scriptBlockRE.ReplaceAllString(src, "")
	// Remove <style …>…</style> blocks (simple pattern, same approach).
	styleRE := regexp.MustCompile(`(?si)<style(?:[^>]*)>.*?</style>`)
	stripped = styleRE.ReplaceAllString(stripped, "")
	if strings.TrimSpace(stripped) == "" {
		return "", 0
	}
	return stripped, 1
}

// extractScriptEntities scans the <script> inner content and extracts:
//   - export let <prop>            → SCOPE.Operation (subtype="prop")
//   - let/const x = $state(…)     → SCOPE.Operation (subtype="rune_state")
//   - let/const x = $derived(…)   → SCOPE.Operation (subtype="rune_derived")
//   - $effect(…)                  → SCOPE.Operation (subtype="rune_effect")
//   - $props() destructure        → SCOPE.Operation (subtype="prop") per named prop
//   - let/const x = $bindable(…)  → SCOPE.Operation (subtype="rune_state")
func extractScriptEntities(script string, scriptStartLine int, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	// lineOf returns 1-based absolute line for a given line index inside script.
	lineOf := func(idx int) int {
		return scriptStartLine + idx
	}

	// export let props
	for _, m := range exportLetRE.FindAllStringSubmatchIndex(script, -1) {
		name := script[m[2]:m[3]]
		lineIdx := strings.Count(script[:m[0]], "\n")
		entities = append(entities, types.EntityRecord{
			Name:         name,
			Kind:         "SCOPE.Operation",
			Subtype:      "prop",
			SourceFile:   filePath,
			Language:     "svelte",
			StartLine:    lineOf(lineIdx),
			EndLine:      lineOf(lineIdx),
			Signature:    "export let " + name,
			QualityScore: 0.85,
		})
	}

	// $state rune
	for _, m := range stateRuneRE.FindAllStringSubmatchIndex(script, -1) {
		name := script[m[2]:m[3]]
		lineIdx := strings.Count(script[:m[0]], "\n")
		entities = append(entities, types.EntityRecord{
			Name:         name,
			Kind:         "SCOPE.Operation",
			Subtype:      "rune_state",
			SourceFile:   filePath,
			Language:     "svelte",
			StartLine:    lineOf(lineIdx),
			EndLine:      lineOf(lineIdx),
			Signature:    "let " + name + " = $state(...)",
			QualityScore: 0.8,
		})
	}

	// $derived rune
	for _, m := range derivedRuneRE.FindAllStringSubmatchIndex(script, -1) {
		name := script[m[2]:m[3]]
		lineIdx := strings.Count(script[:m[0]], "\n")
		entities = append(entities, types.EntityRecord{
			Name:         name,
			Kind:         "SCOPE.Operation",
			Subtype:      "rune_derived",
			SourceFile:   filePath,
			Language:     "svelte",
			StartLine:    lineOf(lineIdx),
			EndLine:      lineOf(lineIdx),
			Signature:    "let " + name + " = $derived(...)",
			QualityScore: 0.8,
		})
	}

	// $effect rune (anonymous — name them by position)
	effectMatches := effectRuneRE.FindAllStringIndex(script, -1)
	for i, m := range effectMatches {
		lineIdx := strings.Count(script[:m[0]], "\n")
		name := "$effect"
		if i > 0 {
			name = fmt.Sprintf("$effect_%d", i)
		}
		entities = append(entities, types.EntityRecord{
			Name:         name,
			Kind:         "SCOPE.Operation",
			Subtype:      "rune_effect",
			SourceFile:   filePath,
			Language:     "svelte",
			StartLine:    lineOf(lineIdx),
			EndLine:      lineOf(lineIdx),
			Signature:    "$effect(() => { ... })",
			QualityScore: 0.75,
		})
	}

	// $props() rune — destructured: `const { a, b } = $props()`
	for _, m := range propsRuneRE.FindAllStringSubmatchIndex(script, -1) {
		lineIdx := strings.Count(script[:m[0]], "\n")
		if m[2] >= 0 {
			// Destructured form: group 1 = "a, b, c"
			inner := script[m[2]:m[3]]
			for _, field := range strings.Split(inner, ",") {
				field = strings.TrimSpace(field)
				// Handle default values: `label = "hello"` → name is "label"
				if idx := strings.IndexAny(field, "=:"); idx >= 0 {
					field = strings.TrimSpace(field[:idx])
				}
				if field == "" {
					continue
				}
				entities = append(entities, types.EntityRecord{
					Name:         field,
					Kind:         "SCOPE.Operation",
					Subtype:      "prop",
					SourceFile:   filePath,
					Language:     "svelte",
					StartLine:    lineOf(lineIdx),
					EndLine:      lineOf(lineIdx),
					Signature:    "$props(): " + field,
					QualityScore: 0.85,
				})
			}
		} else if m[4] >= 0 {
			// Non-destructured form: group 2 = variable name
			name := script[m[4]:m[5]]
			entities = append(entities, types.EntityRecord{
				Name:         name,
				Kind:         "SCOPE.Operation",
				Subtype:      "prop",
				SourceFile:   filePath,
				Language:     "svelte",
				StartLine:    lineOf(lineIdx),
				EndLine:      lineOf(lineIdx),
				Signature:    "let " + name + " = $props()",
				QualityScore: 0.85,
			})
		}
	}

	// $bindable rune
	for _, m := range bindableRuneRE.FindAllStringSubmatchIndex(script, -1) {
		name := script[m[2]:m[3]]
		lineIdx := strings.Count(script[:m[0]], "\n")
		entities = append(entities, types.EntityRecord{
			Name:         name,
			Kind:         "SCOPE.Operation",
			Subtype:      "rune_state",
			SourceFile:   filePath,
			Language:     "svelte",
			StartLine:    lineOf(lineIdx),
			EndLine:      lineOf(lineIdx),
			Signature:    "let " + name + " = $bindable(...)",
			QualityScore: 0.8,
		})
	}

	return entities
}

// extractChildComponents scans the template content for PascalCase component
// tags and returns RENDERS relationship records.
//
// Deduplicates by component name so `<Button>` appearing 3 times produces
// one RENDERS edge, not three (avoids count inflation).
func extractChildComponents(template string, templateStartLine int, filePath, componentName string) []types.RelationshipRecord {
	seen := make(map[string]struct{})
	var rels []types.RelationshipRecord

	for _, m := range childComponentRE.FindAllStringSubmatchIndex(template, -1) {
		name := template[m[2]:m[3]]
		// Skip if already seen.
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}

		lineIdx := strings.Count(template[:m[0]], "\n")
		lineNum := templateStartLine + lineIdx

		rels = append(rels, types.RelationshipRecord{
			FromID: filePath,
			ToID:   name,
			Kind:   "RENDERS",
			Properties: map[string]string{
				"from_component": componentName,
				"to_component":   name,
				"line":           fmt.Sprintf("%d", lineNum),
			},
		})
	}

	return rels
}

// max returns the larger of a and b.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
