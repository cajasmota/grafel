package javascript

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_svelte", &svelteExtractor{})
}

type svelteExtractor struct{}

func (e *svelteExtractor) Language() string { return "custom_js_svelte" }

var (
	reSvelteDefineProps = regexp.MustCompile(
		`const\s+\{[^}]*\}\s*=\s*\$props\(\)|let\s+\{[^}]*\}\s*=\s*\$props\(\)`,
	)
	reSvelteDefinePropsLegacy = regexp.MustCompile(
		`export\s+let\s+(\w+)`,
	)
	reSvelteLoad = regexp.MustCompile(
		`export\s+(?:async\s+)?(?:const\s+load|function\s+load)\b`,
	)
	reSvelteFormActions = regexp.MustCompile(
		`export\s+const\s+actions\s*=`,
	)
	reSvelteHTTPHandler = regexp.MustCompile(
		`export\s+(?:async\s+)?function\s+(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*\(`,
	)
	reSvelteDynParam  = regexp.MustCompile(`\[([^\]]+)\]`)
	reSvelteGroupPath = regexp.MustCompile(`\([^)]+\)`)
	// `params.<name>` access inside a load()/actions handler — a read of a route
	// param whose declared source is the `[name]` route segment (issue #2880).
	reSvelteParamsRead = regexp.MustCompile(`\bparams\.([A-Za-z_$][A-Za-z0-9_$]*)`)

	// Static generation + render-mode page options (issue #2858). SvelteKit
	// page-options exports select the render strategy per route:
	//   `export const prerender = true`  → static generation
	//   `export const ssr = false`       → client-only (SPA) — no server render
	//   `export const csr = false`       → server-only, no client hydration
	reSveltePrerender = regexp.MustCompile(`export\s+const\s+prerender\s*=\s*(true|['"]auto['"])`)
	reSvelteSSROption = regexp.MustCompile(`export\s+const\s+ssr\s*=\s*(false|true)`)
	reSvelteCSROption = regexp.MustCompile(`export\s+const\s+csr\s*=\s*(false|true)`)
)

func normalizeSveltePath(fp string) string {
	result := reSvelteDynParam.ReplaceAllStringFunc(fp, func(s string) string {
		inner := s[1 : len(s)-1]
		if strings.HasPrefix(inner, "...") {
			return "{" + inner[3:] + "*}"
		}
		return "{" + inner + "}"
	})
	result = reSvelteGroupPath.ReplaceAllString(result, "")
	for strings.Contains(result, "//") {
		result = strings.ReplaceAll(result, "//", "/")
	}
	return result
}

// svelteRouteParamNames returns the dynamic param names declared by a
// SvelteKit route path, stripping the `...` rest prefix and SvelteKit matcher
// suffixes (`[id=integer]` → `id`).
func svelteRouteParamNames(fp string) []string {
	var names []string
	for _, m := range reSvelteDynParam.FindAllStringSubmatch(fp, -1) {
		inner := strings.TrimPrefix(m[1], "...")
		if eq := strings.IndexByte(inner, '='); eq >= 0 {
			inner = inner[:eq] // drop `=matcher`
		}
		if inner != "" {
			names = append(names, inner)
		}
	}
	return names
}

// svelteRouteParamSet returns the declared route params as a lookup set.
func svelteRouteParamSet(fp string) map[string]bool {
	set := make(map[string]bool)
	for _, n := range svelteRouteParamNames(fp) {
		set[n] = true
	}
	return set
}

// emitSvelteRouteParams emits one route_param node per dynamic segment in a
// SvelteKit route — the declared source for `params.<name>` reads.
func emitSvelteRouteParams(fp, routePath, filePath, language string, add func(types.EntityRecord)) {
	for _, m := range reSvelteDynParam.FindAllStringSubmatch(fp, -1) {
		raw := m[1]
		catchAll := strings.HasPrefix(raw, "...")
		inner := strings.TrimPrefix(raw, "...")
		if eq := strings.IndexByte(inner, '='); eq >= 0 {
			inner = inner[:eq]
		}
		if inner == "" {
			continue
		}
		ent := makeEntity("param:"+inner, "SCOPE.Pattern", "route_param", filePath, language, 1)
		setProps(&ent, "framework", "svelte", "param_name", inner, "route_path", routePath,
			"source_segment", "["+raw+"]", "catch_all", fmt.Sprintf("%v", catchAll),
			"provenance", "INFERRED_FROM_SVELTEKIT_ROUTE_PARAM")
		add(ent)
	}
}

// contains reports whether s is present in xs.
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// toPascalCase converts kebab-case or underscore_case to PascalCase.
func toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' })
	var sb strings.Builder
	for _, p := range parts {
		if len(p) > 0 {
			sb.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	return sb.String()
}

func (e *svelteExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.svelte_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "svelte"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)

	fp := filepath.ToSlash(file.Path)
	isVueFile := strings.HasSuffix(fp, ".svelte")
	isServerFile := strings.HasSuffix(fp, ".server.ts") || strings.HasSuffix(fp, ".server.js") ||
		strings.HasSuffix(fp, "+server.ts") || strings.HasSuffix(fp, "+server.js")

	if lang != "typescript" && lang != "javascript" && !isVueFile {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.SourceFile)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	stem := filepath.Base(fp)
	// Remove SvelteKit route file prefixes: +page, +layout, +error, +server
	for _, prefix := range []string{"+page.svelte", "+layout.svelte", "+error.svelte", "+page.ts", "+layout.ts"} {
		stem = strings.TrimSuffix(stem, prefix)
	}
	ext := filepath.Ext(stem)
	stem = strings.TrimSuffix(stem, ext)
	if stem == "" || stem == "+" {
		// Derive from directory name
		stem = filepath.Base(filepath.Dir(fp))
	}
	// Strip dynamic `[param]` / matcher / catch-all bracket notation from the
	// name (e.g. `[id]` → `Id`, `[page=integer]` → `PageInteger`).
	cleanStem := strings.NewReplacer("[", "", "]", "", "...", "", "=", "_").Replace(stem)
	compName := toPascalCase(cleanStem)

	routePath := normalizeSveltePath(fp)
	if idx := strings.Index(routePath, "/routes/"); idx >= 0 {
		routePath = routePath[idx+7:]
	}
	// Strip file suffixes
	for _, suffix := range []string{"/+page.svelte", "/+layout.svelte", "/+error.svelte",
		"/+server.ts", "/+server.js", "/+page.server.ts", "/+page.server.js"} {
		routePath = strings.TrimSuffix(routePath, suffix)
	}
	if ext2 := filepath.Ext(routePath); ext2 != "" {
		routePath = strings.TrimSuffix(routePath, ext2)
	}
	if !strings.HasPrefix(routePath, "/") {
		routePath = "/" + routePath
	}

	// .svelte SFC component
	if isVueFile {
		base := filepath.Base(fp)
		subtype := "component"
		if strings.HasPrefix(base, "+layout") {
			subtype = "layout"
		} else if strings.HasPrefix(base, "+error") {
			subtype = "error_boundary"
		} else if strings.HasPrefix(base, "+page") {
			subtype = "page"
		}
		// Structure/component_extraction + Routing/router_pattern (issue #2880):
		// the Svelte SFC page/layout/error component, tagged with the derived
		// route_path and the SvelteKit file-system router convention.
		ent := makeEntity(compName, "SCOPE.UIComponent", subtype, file.Path, file.Language, 1)
		setProps(&ent, "framework", "svelte", "route_path", routePath,
			"router", "file_system",
			"provenance", "INFERRED_FROM_SVELTE_COMPONENT")
		addEntity(ent)
	}

	// Routing/router_pattern (issue #2880): one route_param node per dynamic
	// `[name]` (or rest `[...name]`) segment in the route. These are the declared
	// source for `params.<name>` reads inside load()/actions, so def-use can
	// resolve a param read back to its route-segment definition.
	emitSvelteRouteParams(fp, routePath, file.Path, file.Language, addEntity)

	// SvelteKit load() function — the framework data loader (data_loaders).
	// A load() in a `+page.server.ts` / `+layout.server.ts` is server-only (a
	// universal load() in `+page.ts` runs on both). Tag rendering accordingly.
	isServerLoadFile := strings.HasSuffix(fp, "+page.server.ts") || strings.HasSuffix(fp, "+page.server.js") ||
		strings.HasSuffix(fp, "+layout.server.ts") || strings.HasSuffix(fp, "+layout.server.js")
	if reSvelteLoad.MatchString(src) {
		name := fmt.Sprintf("load:%s", routePath)
		rendering := "universal"
		if isServerLoadFile {
			rendering = "server"
		}
		ent := makeEntity(name, "SCOPE.Operation", "data_loader", file.Path, file.Language, 1)
		setProps(&ent, "framework", "svelte", "route_path", routePath,
			"loader_kind", "load", "rendering", rendering,
			"provenance", "INFERRED_FROM_SVELTE_LOAD")
		// Route-param source detection (issue #2880): record which declared
		// `[name]` route segments this load() reads via `params.<name>`. Only
		// params backed by a real route segment are recorded, so def-use can
		// resolve the read to its route_param source node.
		declared := svelteRouteParamSet(fp)
		var read []string
		for _, m := range reSvelteParamsRead.FindAllStringSubmatch(src, -1) {
			if declared[m[1]] && !contains(read, m[1]) {
				read = append(read, m[1])
			}
		}
		if len(read) > 0 {
			setProps(&ent, "params_read", strings.Join(read, ","))
		}
		addEntity(ent)
	}

	// Form actions
	if reSvelteFormActions.MatchString(src) {
		name := fmt.Sprintf("actions:%s", routePath)
		ent := makeEntity(name, "SCOPE.Operation", "form_actions", file.Path, file.Language, 1)
		setProps(&ent, "framework", "svelte", "route_path", routePath,
			"provenance", "INFERRED_FROM_SVELTE_FORM_ACTIONS")
		addEntity(ent)
	}

	// HTTP handlers in +server.ts
	if isServerFile {
		for _, m := range reSvelteHTTPHandler.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			name := fmt.Sprintf("%s %s", method, routePath)
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "svelte", "http_method", method, "route_path", routePath,
				"provenance", "INFERRED_FROM_SVELTE_HTTP_HANDLER")
			addEntity(ent)
		}
	}

	// Props via $props() or export let
	if isVueFile {
		hasProps := reSvelteDefineProps.MatchString(src)
		if !hasProps {
			propMatches := reSvelteDefinePropsLegacy.FindAllStringSubmatchIndex(src, -1)
			hasProps = len(propMatches) > 0
			for _, m := range propMatches {
				propName := src[m[2]:m[3]]
				pent := makeEntity(propName, "SCOPE.Component", "prop", file.Path, file.Language, lineOf(src, m[0]))
				setProps(&pent, "framework", "svelte", "provenance", "INFERRED_FROM_SVELTE_PROP")
				addEntity(pent)
			}
		}
		if hasProps && !reSvelteDefinePropsLegacy.MatchString(src) {
			pent := makeEntity(compName+"Props", "SCOPE.Component", "props", file.Path, file.Language, 1)
			setProps(&pent, "framework", "svelte", "provenance", "INFERRED_FROM_SVELTE_PROPS")
			addEntity(pent)
		}
	}

	// Server components / hydration boundaries (issue #2858).
	//
	// SvelteKit's render model is file-name-driven:
	//   - `+page.server.ts` / `+layout.server.ts` / `+server.ts` run only on the
	//     server (a server boundary — server_components).
	//   - `+page.svelte` is server-rendered then hydrated on the client (a
	//     hydration boundary — hydration_boundaries) unless `csr = false`.
	if isServerLoadFile || isServerFile {
		sb := makeEntity("server_boundary:"+routePath, "SCOPE.Pattern", "server_boundary", file.Path, file.Language, 1)
		setProps(&sb, "framework", "svelte", "route_path", routePath, "rendering", "server",
			"provenance", "INFERRED_FROM_SVELTEKIT_SERVER_MODULE")
		addEntity(sb)
	}
	if isVueFile && strings.HasPrefix(filepath.Base(fp), "+page") {
		hb := makeEntity("hydrate:"+routePath, "SCOPE.Pattern", "client_boundary", file.Path, file.Language, 1)
		setProps(&hb, "framework", "svelte", "route_path", routePath, "hydration", "client",
			"provenance", "INFERRED_FROM_SVELTEKIT_HYDRATION")
		addEntity(hb)
	}

	// Static generation + render-mode page options (issue #2858).
	if m := reSveltePrerender.FindStringIndex(src); m != nil {
		sg := makeEntity("static_generation:prerender", "SCOPE.Pattern", "static_generation", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&sg, "framework", "svelte", "route_path", routePath, "marker", "prerender", "rendering", "ssg",
			"provenance", "INFERRED_FROM_SVELTEKIT_PRERENDER")
		addEntity(sg)
	}
	if m := reSvelteSSROption.FindStringSubmatchIndex(src); m != nil && src[m[2]:m[3]] == "false" {
		// `export const ssr = false` → client-only SPA route (no server render);
		// the whole page is a client hydration boundary.
		cb := makeEntity("spa:"+routePath, "SCOPE.Pattern", "client_boundary", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&cb, "framework", "svelte", "route_path", routePath, "render_mode", "csr_only", "hydration", "client",
			"provenance", "INFERRED_FROM_SVELTEKIT_SSR_OPTION")
		addEntity(cb)
	}
	if m := reSvelteCSROption.FindStringSubmatchIndex(src); m != nil && src[m[2]:m[3]] == "false" {
		// `export const csr = false` → server-only route, no client hydration.
		sb := makeEntity("ssr-only:"+routePath, "SCOPE.Pattern", "server_boundary", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&sb, "framework", "svelte", "route_path", routePath, "render_mode", "ssr_only", "rendering", "server",
			"provenance", "INFERRED_FROM_SVELTEKIT_CSR_OPTION")
		addEntity(sb)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
