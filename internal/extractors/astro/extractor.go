// Package astro implements a regex-based extractor for Astro single-file
// components (.astro files).
//
// An Astro SFC has three sections:
//
//	Frontmatter (between --- markers)  — TypeScript with imports, props, data fetching
//	HTML template body                 — markup with component references and expressions
//	<style> blocks                     — scoped CSS (ignored for entity extraction)
//
// Extracted entities:
//
//	Whole file                        → SCOPE.Component   subtype="astro_page" (under pages/) or "astro_component"
//	Frontmatter imports               → IMPORTS edges on the component entity
//	const props = Astro.props         → SCOPE.Operation   subtype="props_binding"
//	const { x } = Astro.props         → SCOPE.Operation   subtype="prop" per named prop
//	<PascalCase />                    → RENDERS edges
//	client:load / client:idle /       → IMPLEMENTS edges (framework island markers)
//	  client:visible / client:only
//
// Astro globals (Astro.url, Astro.params, etc.), content-collection helpers
// (getCollection, getEntry, defineCollection), and view-transition directives
// (transition:name, transition:animate) are handled by the resolver slice
// (dynamic_patterns_astro.go) to prevent dangling CALLS stubs.
//
// Registers itself via init() and is imported by registry_gen.go.
package astro

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
	extractor.Register("astro", &Extractor{})
}

// Extractor implements extractor.Extractor for Astro SFC files.
type Extractor struct{}

// Language returns the canonical language key.
func (e *Extractor) Language() string { return "astro" }

// ── compiled regexps ─────────────────────────────────────────────────────────

var (
	// frontmatterRE captures the content between the opening and closing ---
	// markers that begin an Astro file. The opening --- must be at the very
	// start of the file (optional leading whitespace tolerated).
	frontmatterRE = regexp.MustCompile(`(?s)^\s*---\n(.*?)\n---`)

	// importRE matches TypeScript/JS import statements inside the frontmatter.
	// Captures the module path (single or double quoted).
	//   import Foo from './Foo.astro'
	//   import { bar } from '../lib/bar'
	importRE = regexp.MustCompile(`(?m)^import\s+.+\s+from\s+['"]([^'"]+)['"]`)

	// astroPropsDestructureRE matches:
	//   const { title, description = 'default' } = Astro.props
	// Captures the interior of the braces (group 1).
	astroPropsDestructureRE = regexp.MustCompile(`(?m)const\s+\{([^}]+)\}\s*=\s*Astro\.props`)

	// astroPropsBindingRE matches the non-destructured form:
	//   const props = Astro.props
	// Captures the binding name (group 1).
	astroPropsBindingRE = regexp.MustCompile(`(?m)const\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*Astro\.props`)

	// childComponentRE finds PascalCase component tags (including self-closing).
	// A PascalCase tag in Astro is always a component (lowercase = HTML element).
	childComponentRE = regexp.MustCompile(`<([A-Z][A-Za-z0-9]*)\b`)

	// islandAttrRE detects framework-island client directives on a tag.
	// Matches client:load, client:idle, client:visible, client:only="…",
	// client:media="…".
	islandAttrRE = regexp.MustCompile(`\bclient:(load|idle|visible|only(?:="[^"]*")?|media(?:="[^"]*")?)`)

	// styleBlockRE strips <style …>…</style> from the body before template scan.
	styleBlockRE = regexp.MustCompile(`(?si)<style(?:[^>]*)>.*?</style>`)
)

// Extract parses the Astro SFC source and returns entity records.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor.astro")
	ctx, span := tracer.Start(ctx, "indexer.extract.astro",
		trace.WithAttributes(attribute.String("language", "astro")),
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
	subtype := pageSubtype(file.Path)

	var entities []types.EntityRecord

	// ── 1. Whole-file component entity ──────────────────────────────────────
	componentEntity := types.EntityRecord{
		Name:         componentName,
		Kind:         "SCOPE.Component",
		Subtype:      subtype,
		SourceFile:   file.Path,
		Language:     "astro",
		StartLine:    1,
		EndLine:      lineCount,
		Signature:    componentName + ".astro",
		QualityScore: 0.85,
	}
	entities = append(entities, componentEntity)

	// ── 2. Frontmatter section ───────────────────────────────────────────────
	frontmatter, fmStartLine := extractFrontmatter(src)
	if frontmatter != "" {
		// 2a. Import edges
		importRels := extractImports(frontmatter, fmStartLine, file.Path)
		entities[0].Relationships = append(entities[0].Relationships, importRels...)

		// 2b. Astro.props bindings → SCOPE.Operation entities
		propEntities := extractPropsEntities(frontmatter, fmStartLine, file.Path)
		entities = append(entities, propEntities...)
	}

	// ── 3. Template: child components (RENDERS) and islands (IMPLEMENTS) ─────
	body := extractBody(src)
	if body != "" {
		renderRels, islandRels := extractTemplateRelationships(body, file.Path, componentName)
		entities[0].Relationships = append(entities[0].Relationships, renderRels...)
		entities[0].Relationships = append(entities[0].Relationships, islandRels...)
	}

	span.SetAttributes(
		attribute.Int("file_line_count", lineCount),
		attribute.Int("entity_count", len(entities)),
	)
	return entities, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

// componentNameFromPath derives the component name from the file base name.
// "src/pages/index.astro" → "index"
// "src/components/Header.astro" → "Header"
func componentNameFromPath(path string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, ".astro")
	if name == "" {
		return "Component"
	}
	return name
}

// pageSubtype returns "astro_page" if the file is under a pages/ directory,
// otherwise "astro_component".
func pageSubtype(path string) string {
	// Normalize separators for matching.
	norm := filepath.ToSlash(path)
	if strings.Contains(norm, "/pages/") || strings.HasPrefix(norm, "pages/") {
		return "astro_page"
	}
	return "astro_component"
}

// extractFrontmatter returns the content between the --- markers and the
// 1-based line number of the first content line inside the block.
// Returns ("", 0) if no frontmatter is found.
func extractFrontmatter(src string) (string, int) {
	m := frontmatterRE.FindStringSubmatchIndex(src)
	if m == nil {
		return "", 0
	}
	inner := src[m[2]:m[3]]
	// Line number of the first character of inner content.
	startLine := strings.Count(src[:m[2]], "\n") + 1
	return inner, startLine
}

// extractBody strips the frontmatter block and all <style> elements, returning
// the remaining HTML template.
func extractBody(src string) string {
	// Remove frontmatter.
	stripped := frontmatterRE.ReplaceAllString(src, "")
	// Remove <style> blocks.
	stripped = styleBlockRE.ReplaceAllString(stripped, "")
	return strings.TrimSpace(stripped)
}

// extractImports returns IMPORTS relationship records for each import
// statement found in the frontmatter.
func extractImports(fm string, fmStartLine int, filePath string) []types.RelationshipRecord {
	var rels []types.RelationshipRecord
	seen := make(map[string]struct{})

	for _, m := range importRE.FindAllStringSubmatchIndex(fm, -1) {
		modulePath := fm[m[2]:m[3]]
		if _, exists := seen[modulePath]; exists {
			continue
		}
		seen[modulePath] = struct{}{}
		rels = append(rels, types.RelationshipRecord{
			FromID: filePath,
			ToID:   modulePath,
			Kind:   "IMPORTS",
			Properties: map[string]string{
				"source_module": modulePath,
				"line":          fmt.Sprintf("%d", fmStartLine+strings.Count(fm[:m[0]], "\n")),
			},
		})
	}
	return rels
}

// extractPropsEntities parses Astro.props usages in the frontmatter and emits
// SCOPE.Operation entities.
//
// Destructured form: const { title, description = 'default' } = Astro.props
// → one entity per named prop (subtype="prop").
//
// Non-destructured form: const props = Astro.props
// → one entity for the binding (subtype="props_binding").
func extractPropsEntities(fm string, fmStartLine int, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	lineOf := func(idx int) int {
		return fmStartLine + strings.Count(fm[:idx], "\n")
	}

	// Destructured: const { a, b = 'x' } = Astro.props
	for _, m := range astroPropsDestructureRE.FindAllStringSubmatchIndex(fm, -1) {
		lineNum := lineOf(m[0])
		inner := fm[m[2]:m[3]]
		for _, field := range strings.Split(inner, ",") {
			field = strings.TrimSpace(field)
			// Strip default-value suffix: `label = "hello"` → "label"
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
				Language:     "astro",
				StartLine:    lineNum,
				EndLine:      lineNum,
				Signature:    "Astro.props: " + field,
				QualityScore: 0.85,
			})
		}
	}

	// Non-destructured: const props = Astro.props
	// Only match bindings that are NOT followed by '{', to avoid re-matching
	// the destructured form.
	for _, m := range astroPropsBindingRE.FindAllStringSubmatchIndex(fm, -1) {
		name := fm[m[2]:m[3]]
		// Skip if this is actually part of a destructured match (name == "{")
		if name == "" {
			continue
		}
		lineNum := lineOf(m[0])
		entities = append(entities, types.EntityRecord{
			Name:         name,
			Kind:         "SCOPE.Operation",
			Subtype:      "props_binding",
			SourceFile:   filePath,
			Language:     "astro",
			StartLine:    lineNum,
			EndLine:      lineNum,
			Signature:    "const " + name + " = Astro.props",
			QualityScore: 0.85,
		})
	}

	return entities
}

// extractTemplateRelationships scans the HTML body for:
//   - PascalCase component tags → RENDERS edges (deduplicated)
//   - client:* directives on those tags → IMPLEMENTS edges
func extractTemplateRelationships(body, filePath, componentName string) (renders []types.RelationshipRecord, islands []types.RelationshipRecord) {
	seenRenders := make(map[string]struct{})
	seenIslands := make(map[string]struct{})

	for _, m := range childComponentRE.FindAllStringSubmatchIndex(body, -1) {
		name := body[m[2]:m[3]]
		lineIdx := strings.Count(body[:m[0]], "\n")

		// RENDERS edge (deduplicated per component name).
		if _, exists := seenRenders[name]; !exists {
			seenRenders[name] = struct{}{}
			renders = append(renders, types.RelationshipRecord{
				FromID: filePath,
				ToID:   name,
				Kind:   "RENDERS",
				Properties: map[string]string{
					"from_component": componentName,
					"to_component":   name,
					"line":           fmt.Sprintf("%d", lineIdx+1),
				},
			})
		}

		// IMPLEMENTS edge — detect whether this particular tag occurrence has a
		// client:* directive. We scan forward from the tag open to the next >.
		tagEnd := strings.Index(body[m[0]:], ">")
		if tagEnd < 0 {
			continue
		}
		tagSrc := body[m[0] : m[0]+tagEnd+1]
		if islandAttrRE.MatchString(tagSrc) {
			islandKey := name
			if _, exists := seenIslands[islandKey]; !exists {
				seenIslands[islandKey] = struct{}{}
				directive := islandAttrRE.FindString(tagSrc)
				islands = append(islands, types.RelationshipRecord{
					FromID: filePath,
					ToID:   name,
					Kind:   "IMPLEMENTS",
					Properties: map[string]string{
						"island_directive": directive,
						"host_component":   componentName,
						"framework_island": name,
						"line":             fmt.Sprintf("%d", lineIdx+1),
					},
				})
			}
		}
	}
	return renders, islands
}
