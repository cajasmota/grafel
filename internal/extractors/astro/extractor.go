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

	// ── Server / Data Flow / Build markers (issue #2858) ─────────────────────

	// getStaticPathsRE matches the `export function getStaticPaths()` / `export
	// const getStaticPaths = …` data loader that drives static generation of an
	// Astro dynamic route ([slug]) — data_loaders + static_generation.
	getStaticPathsRE = regexp.MustCompile(`export\s+(?:async\s+)?(?:function\s+getStaticPaths|const\s+getStaticPaths\s*=)`)

	// contentCollectionRE matches Astro content-collection data loaders:
	// getCollection('blog') / getEntry('blog', slug) — build-time data loading.
	contentCollectionRE = regexp.MustCompile(`\b(getCollection|getEntry|getEntryBySlug|getEntries)\s*\(`)

	// prerenderRE matches `export const prerender = true|false`. true forces a
	// statically-generated page; false opts a page into on-demand (server)
	// rendering when the project output is "server"/"hybrid".
	prerenderRE = regexp.MustCompile(`export\s+const\s+prerender\s*=\s*(true|false)`)
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
		Properties:   map[string]string{"framework": "astro"},
	}
	// Astro file-system routing (issue #2857, router_pattern): a file under
	// pages/ maps to a route by its path. Record the derived route_path and the
	// router convention on the page entity so route discovery is provable.
	if subtype == "astro_page" {
		routePath := routePathFromAstroPage(file.Path)
		componentEntity.Properties["route_path"] = routePath
		componentEntity.Properties["router"] = "file_system"
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

		// 2c. Server / Data Flow / Build markers (issue #2858). The Astro
		// frontmatter (between --- markers) executes on the server (at build time
		// for static output, per request for SSR) — it is the component's server
		// boundary (server_components). It is also where Astro's data loaders run.
		entities = append(entities,
			extractServerBuildEntities(frontmatter, fmStartLine, file.Path, subtype)...)
	}

	// ── 3. Template: child components (RENDERS) and islands (IMPLEMENTS) ─────
	body := extractBody(src)
	if body != "" {
		renderRels, islandRels := extractTemplateRelationships(body, file.Path, componentName)
		entities[0].Relationships = append(entities[0].Relationships, renderRels...)
		entities[0].Relationships = append(entities[0].Relationships, islandRels...)

		// Hydration boundaries (issue #2858): each `client:*` island is an
		// explicit boundary where the server-rendered HTML becomes an interactive
		// client component. Emit a marker entity per island so the
		// hydration_boundaries capability is directly queryable (the IMPLEMENTS
		// edges above attribute it to the host; this gives it a first-class node).
		for _, isl := range islandRels {
			ent := types.EntityRecord{
				Name:         "island:" + isl.ToID,
				Kind:         "SCOPE.Pattern",
				Subtype:      "client_boundary",
				SourceFile:   file.Path,
				Language:     "astro",
				StartLine:    1,
				EndLine:      1,
				Signature:    isl.Properties["island_directive"] + " " + isl.ToID,
				QualityScore: 0.85,
				Properties: map[string]string{
					"framework":        "astro",
					"hydration":        "client",
					"island_directive": isl.Properties["island_directive"],
					"island_component": isl.ToID,
				},
			}
			entities = append(entities, ent)
		}
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

// astroDynParamRE matches Astro dynamic-route segments: [slug], [...path],
// [[...optional]]. Used to normalize the route path of a page file.
var astroDynParamRE = regexp.MustCompile(`\[+\.?\.?\.?([^\]]+)\]+`)

// routePathFromAstroPage derives the URL route for an Astro page from its file
// path under pages/. Astro's file-system router maps:
//
//	src/pages/index.astro        → /
//	src/pages/about.astro        → /about
//	src/pages/blog/[slug].astro  → /blog/{slug}
//	src/pages/[...path].astro    → /{path*}
func routePathFromAstroPage(path string) string {
	norm := filepath.ToSlash(path)
	idx := strings.Index(norm, "/pages/")
	rel := norm
	switch {
	case idx >= 0:
		rel = norm[idx+len("/pages/"):]
	case strings.HasPrefix(norm, "pages/"):
		rel = norm[len("pages/"):]
	}
	rel = strings.TrimSuffix(rel, ".astro")
	rel = strings.TrimSuffix(rel, "/index")
	if rel == "index" {
		rel = ""
	}
	rel = astroDynParamRE.ReplaceAllStringFunc(rel, func(s string) string {
		inner := strings.Trim(s, "[]")
		if strings.HasPrefix(inner, "...") {
			return "{" + strings.TrimPrefix(inner, "...") + "*}"
		}
		return "{" + inner + "}"
	})
	route := "/" + strings.TrimPrefix(rel, "/")
	return route
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

// extractServerBuildEntities scans the Astro frontmatter for the
// meta-framework Server / Data Flow / Build markers (issue #2858):
//
//   - The frontmatter itself runs server-side → one server_component marker per
//     page/component (server_components).
//   - getStaticPaths() → a build-time data loader that statically generates the
//     dynamic route's pages (data_loaders + static_generation).
//   - getCollection / getEntry (content collections) → build-time data loaders
//     (data_loaders).
//   - `export const prerender = true|false` → explicit static/server render
//     selector (static_generation when true).
//
// All markers are SCOPE.Pattern / SCOPE.Operation entities; no new Kinds.
func extractServerBuildEntities(fm string, fmStartLine int, filePath, subtype string) []types.EntityRecord {
	var out []types.EntityRecord
	lineOf := func(idx int) int { return fmStartLine + strings.Count(fm[:idx], "\n") }

	mk := func(name, kind, st string, line int, props map[string]string) types.EntityRecord {
		props["framework"] = "astro"
		return types.EntityRecord{
			Name:         name,
			Kind:         kind,
			Subtype:      st,
			SourceFile:   filePath,
			Language:     "astro",
			StartLine:    line,
			EndLine:      line,
			QualityScore: 0.85,
			Properties:   props,
		}
	}

	// Server component: the frontmatter is server-rendered. Emit one marker for
	// the file (named by its route subtype) so server_components is provable.
	out = append(out, mk("server:"+componentNameFromPath(filePath), "SCOPE.Pattern", "server_component", 1,
		map[string]string{"rendering": "server", "component_kind": "server",
			"provenance": "INFERRED_FROM_ASTRO_FRONTMATTER"}))

	// getStaticPaths → data loader + static generation.
	if m := getStaticPathsRE.FindStringIndex(fm); m != nil {
		ln := lineOf(m[0])
		out = append(out, mk("getStaticPaths", "SCOPE.Operation", "data_loader", ln,
			map[string]string{"loader_kind": "getStaticPaths", "rendering": "ssg",
				"provenance": "INFERRED_FROM_ASTRO_GET_STATIC_PATHS"}))
		out = append(out, mk("ssg:getStaticPaths", "SCOPE.Pattern", "static_generation", ln,
			map[string]string{"marker": "getStaticPaths", "rendering": "ssg",
				"provenance": "INFERRED_FROM_ASTRO_GET_STATIC_PATHS"}))
	}

	// Content-collection loaders.
	if m := contentCollectionRE.FindStringSubmatchIndex(fm); m != nil {
		fn := fm[m[2]:m[3]]
		out = append(out, mk(fn, "SCOPE.Operation", "data_loader", lineOf(m[0]),
			map[string]string{"loader_kind": fn, "rendering": "ssg",
				"provenance": "INFERRED_FROM_ASTRO_CONTENT_COLLECTION"}))
	}

	// export const prerender = true → static generation marker.
	if m := prerenderRE.FindStringSubmatchIndex(fm); m != nil {
		val := fm[m[2]:m[3]]
		if val == "true" {
			out = append(out, mk("prerender", "SCOPE.Pattern", "static_generation", lineOf(m[0]),
				map[string]string{"marker": "prerender", "rendering": "ssg",
					"provenance": "INFERRED_FROM_ASTRO_PRERENDER"}))
		}
	}

	return out
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
