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
	extreg.Register("custom_js_remix", &remixExtractor{})
}

type remixExtractor struct{}

func (e *remixExtractor) Language() string { return "custom_js_remix" }

var (
	reRemixLoaderFn = regexp.MustCompile(
		`export\s+(?:async\s+)?function\s+loader\s*\(`,
	)
	reRemixLoaderConst = regexp.MustCompile(
		`export\s+const\s+loader\s*=`,
	)
	reRemixActionFn = regexp.MustCompile(
		`export\s+(?:async\s+)?function\s+action\s*\(`,
	)
	reRemixActionConst = regexp.MustCompile(
		`export\s+const\s+action\s*=`,
	)
	reRemixDefaultExport = regexp.MustCompile(
		`export\s+default\s+function\s+([A-Z][A-Za-z0-9_]*)\s*\(`,
	)
	reRemixHandle = regexp.MustCompile(
		`export\s+const\s+handle\s*=`,
	)
	reRemixMeta = regexp.MustCompile(
		`export\s+(?:const|function)\s+meta\b`,
	)
	reRemixLinks = regexp.MustCompile(
		`export\s+(?:const|function)\s+links\b`,
	)
	reRemixHeaders = regexp.MustCompile(
		`export\s+(?:const|function)\s+headers\b`,
	)
	reRemixErrorBoundary = regexp.MustCompile(
		`export\s+(?:default\s+)?function\s+ErrorBoundary\s*\(`,
	)
	reRemixDynParam = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)

	// Static generation (issue #2858). Remix is SSR-first; prerendering is opt-in
	// and configured at build level: the Vite plugin's `prerender: [...]` option,
	// or SPA mode via `ssr: false` in the @remix-run/dev preset. Either marks the
	// app for static generation.
	reRemixPrerender = regexp.MustCompile(`\bprerender\s*:\s*(?:true|\[)`)
	reRemixSPAMode   = regexp.MustCompile(`\bssr\s*:\s*false\b`)
)

func normalizeRemixPath(fp string) string {
	// Remix route file naming: $param -> {param}
	result := reRemixDynParam.ReplaceAllString(fp, "{$1}")
	return result
}

func (e *remixExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.remix_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "remix"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	if lang != "typescript" && lang != "javascript" {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.Subtype)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	fp := filepath.ToSlash(file.Path)
	stem := filepath.Base(fp)
	ext := filepath.Ext(stem)
	stem = strings.TrimSuffix(stem, ext)

	// Compute route path from file path
	routePath := normalizeRemixPath(fp)
	// Extract path under routes/
	if idx := strings.Index(routePath, "/routes/"); idx >= 0 {
		routePath = routePath[idx+7:]
	}
	if ext2 := filepath.Ext(routePath); ext2 != "" {
		routePath = strings.TrimSuffix(routePath, ext2)
	}
	// Remix v2 flat file routes: dots become slashes
	routePath = strings.ReplaceAll(routePath, ".", "/")
	if !strings.HasPrefix(routePath, "/") {
		routePath = "/" + routePath
	}

	// loader function/const — Remix's server-side data-loading function
	// (data_loaders). A loader always runs on the server, so it is also a
	// server boundary (server_components). Both run server-side; Remix has no
	// 'use client' opt-in (every route module is server-rendered then hydrated).
	hasLoader := reRemixLoaderFn.MatchString(src) || reRemixLoaderConst.MatchString(src)
	if hasLoader {
		name := fmt.Sprintf("loader:%s", routePath)
		ent := makeEntity(name, "SCOPE.Operation", "data_loader", file.Path, file.Language, 1)
		setProps(&ent, "framework", "remix", "route_path", routePath, "stem", stem,
			"loader_kind", "loader", "rendering", "server",
			"provenance", "INFERRED_FROM_REMIX_LOADER")
		addEntity(ent)
	}

	// action function/const — server-side mutation handler (server boundary).
	hasAction := reRemixActionFn.MatchString(src) || reRemixActionConst.MatchString(src)
	if hasAction {
		name := fmt.Sprintf("action:%s", routePath)
		ent := makeEntity(name, "SCOPE.Operation", "action", file.Path, file.Language, 1)
		setProps(&ent, "framework", "remix", "route_path", routePath, "stem", stem,
			"rendering", "server", "provenance", "INFERRED_FROM_REMIX_ACTION")
		addEntity(ent)
	}

	// Default export (page component)
	for _, m := range reRemixDefaultExport.FindAllStringSubmatchIndex(src, -1) {
		compName := src[m[2]:m[3]]
		ent := makeEntity(compName, "SCOPE.UIComponent", "route_component", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "remix", "route_path", routePath,
			"provenance", "INFERRED_FROM_REMIX_COMPONENT")
		addEntity(ent)
	}

	// Meta
	if reRemixMeta.MatchString(src) {
		name := fmt.Sprintf("meta:%s", stem)
		ent := makeEntity(name, "SCOPE.Pattern", "meta", file.Path, file.Language, 1)
		setProps(&ent, "framework", "remix", "route_path", routePath,
			"provenance", "INFERRED_FROM_REMIX_META")
		addEntity(ent)
	}

	// Links
	if reRemixLinks.MatchString(src) {
		name := fmt.Sprintf("links:%s", stem)
		ent := makeEntity(name, "SCOPE.Pattern", "links", file.Path, file.Language, 1)
		setProps(&ent, "framework", "remix", "route_path", routePath,
			"provenance", "INFERRED_FROM_REMIX_LINKS")
		addEntity(ent)
	}

	// Headers
	if reRemixHeaders.MatchString(src) {
		name := fmt.Sprintf("headers:%s", stem)
		ent := makeEntity(name, "SCOPE.Pattern", "headers", file.Path, file.Language, 1)
		setProps(&ent, "framework", "remix", "route_path", routePath,
			"provenance", "INFERRED_FROM_REMIX_HEADERS")
		addEntity(ent)
	}

	// Handle
	if reRemixHandle.MatchString(src) {
		name := fmt.Sprintf("handle:%s", stem)
		ent := makeEntity(name, "SCOPE.Pattern", "handle", file.Path, file.Language, 1)
		setProps(&ent, "framework", "remix", "route_path", routePath,
			"provenance", "INFERRED_FROM_REMIX_HANDLE")
		addEntity(ent)
	}

	// ErrorBoundary
	if reRemixErrorBoundary.MatchString(src) {
		name := fmt.Sprintf("ErrorBoundary:%s", stem)
		ent := makeEntity(name, "SCOPE.UIComponent", "error_boundary", file.Path, file.Language, 1)
		setProps(&ent, "framework", "remix", "route_path", routePath,
			"provenance", "INFERRED_FROM_REMIX_ERROR_BOUNDARY")
		addEntity(ent)
	}

	// React structure: custom hooks + hook call sites in the route module.
	// Remix routes are React components (the default export is captured above
	// as route_component); this adds hook_recognition (issue #2857) by reusing
	// the shared React detection. Gated to Remix routes/ context so non-Remix
	// React projects don't get remix-tagged duplicate component entities.
	isRoutesFile := strings.Contains(fp, "/routes/") || strings.HasPrefix(fp, "routes/") ||
		strings.Contains(fp, "/app/root.") || strings.HasPrefix(fp, "app/root.")
	if isRoutesFile {
		extractReactStructure(src, file.Path, file.Language, "remix", addEntity)
	}

	// Server components / hydration boundaries (issue #2858).
	//
	// A route module that exports a loader or action is a server boundary
	// (server_components) — those functions execute only on the server. Remix
	// also honours the `.server.{ts,tsx}` / `.client.{ts,tsx}` module-suffix
	// convention: `.server` code is stripped from the client bundle, `.client`
	// code from the server bundle — the explicit hydration boundary.
	if hasLoader || hasAction {
		sb := makeEntity(fmt.Sprintf("server_boundary:%s", routePath), "SCOPE.Pattern", "server_boundary", file.Path, file.Language, 1)
		setProps(&sb, "framework", "remix", "route_path", routePath, "rendering", "server",
			"provenance", "INFERRED_FROM_REMIX_SERVER_BOUNDARY")
		addEntity(sb)
	}
	switch {
	case strings.HasSuffix(fp, ".server.ts") || strings.HasSuffix(fp, ".server.tsx") ||
		strings.HasSuffix(fp, ".server.js") || strings.HasSuffix(fp, ".server.jsx"):
		emitServerOnlyModule(stem, file.Path, file.Language, "remix", addEntity)
	case strings.HasSuffix(fp, ".client.ts") || strings.HasSuffix(fp, ".client.tsx") ||
		strings.HasSuffix(fp, ".client.js") || strings.HasSuffix(fp, ".client.jsx"):
		cb := makeEntity(stem, "SCOPE.Pattern", "client_boundary", file.Path, file.Language, 1)
		setProps(&cb, "framework", "remix", "module_scope", "client", "hydration", "client",
			"provenance", "INFERRED_FROM_CLIENT_MODULE_SUFFIX")
		addEntity(cb)
	}

	// Static generation (issue #2858): prerender list / SPA mode in the Vite
	// config or Remix preset.
	if reRemixPrerender.MatchString(src) || reRemixSPAMode.MatchString(src) {
		marker := "prerender"
		if reRemixSPAMode.MatchString(src) && !reRemixPrerender.MatchString(src) {
			marker = "spa_mode"
		}
		sg := makeEntity("static_generation:"+marker, "SCOPE.Pattern", "static_generation", file.Path, file.Language, 1)
		setProps(&sg, "framework", "remix", "marker", marker, "rendering", "ssg",
			"provenance", "INFERRED_FROM_REMIX_PRERENDER")
		addEntity(sg)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
