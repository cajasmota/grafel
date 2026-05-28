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
	extreg.Register("custom_js_nuxt", &nuxtExtractor{})
}

type nuxtExtractor struct{}

func (e *nuxtExtractor) Language() string { return "custom_js_nuxt" }

var (
	reNuxtHTTPHandler = regexp.MustCompile(
		`export\s+default\s+defineEventHandler|export\s+default\s+eventHandler|defineEventHandler\s*\(`,
	)
	reNuxtComposable = regexp.MustCompile(
		`export\s+(?:(?:const|function)\s+)?(use[A-Z][A-Za-z0-9_]*)`,
	)
	reNuxtDefinePageMeta = regexp.MustCompile(
		`definePageMeta\s*\(\s*\{`,
	)
	reNuxtMiddleware = regexp.MustCompile(
		`export\s+default\s+defineNuxtRouteMiddleware`,
	)
	reNuxtPlugin = regexp.MustCompile(
		`export\s+default\s+defineNuxtPlugin`,
	)
	reNuxtDynParam = regexp.MustCompile(`\[([^\]]+)\]`)

	// Data loaders (issue #2858): Nuxt's data-fetching composables. These run
	// during SSR on the server and on the client during navigation.
	reNuxtDataLoader = regexp.MustCompile(`\b(useAsyncData|useLazyAsyncData|useFetch|useLazyFetch)\s*\(`)
	// `<ClientOnly>` template tag — an explicit client-only hydration boundary.
	reNuxtClientOnly = regexp.MustCompile(`<ClientOnly\b`)
	// Static generation: `routeRules: { '…': { prerender: true } }` (in
	// nuxt.config) and `defineRouteRules({ prerender: true })` (page-level).
	reNuxtPrerenderRule = regexp.MustCompile(`\bprerender\s*:\s*true\b`)
	reNuxtRouteRules    = regexp.MustCompile(`\b(routeRules|defineRouteRules)\b`)
	// `ssr: false` in nuxt.config switches the app to client-only SPA build.
	reNuxtSSRFalse = regexp.MustCompile(`\bssr\s*:\s*false\b`)
)

var (
	nuxtServerDirs   = map[string]bool{"server/api": true, "server/routes": true}
	nuxtSpecialFiles = map[string]bool{"index": true, "default": true}
)

func normalizeNuxtPath(fp string) string {
	result := reNuxtDynParam.ReplaceAllStringFunc(fp, func(s string) string {
		inner := s[1 : len(s)-1]
		if strings.HasPrefix(inner, "...") {
			return "{" + inner[3:] + "*}"
		}
		return "{" + inner + "}"
	})
	for strings.Contains(result, "//") {
		result = strings.ReplaceAll(result, "//", "/")
	}
	return result
}

func (e *nuxtExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.nuxt_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "nuxt"),
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
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.SourceFile)
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

	isVueFile := strings.HasSuffix(fp, ".vue")
	isServerAPI := strings.Contains(fp, "/server/api/") || strings.Contains(fp, "/server/routes/") ||
		strings.HasPrefix(fp, "server/api/") || strings.HasPrefix(fp, "server/routes/")
	isPagesFile := (strings.Contains(fp, "/pages/") || strings.HasPrefix(fp, "pages/")) && isVueFile

	// Nuxt server API/route handlers
	if isServerAPI && reNuxtHTTPHandler.MatchString(src) {
		routePath := normalizeNuxtPath(fp)
		for dir := range nuxtServerDirs {
			if idx := strings.Index(routePath, "/"+dir+"/"); idx >= 0 {
				routePath = routePath[idx+len(dir)+1:]
			}
		}
		if ext2 := filepath.Ext(routePath); ext2 != "" {
			routePath = strings.TrimSuffix(routePath, ext2)
		}
		// Extract HTTP method from filename (e.g. users.get.ts -> GET)
		method := "ANY"
		parts := strings.Split(stem, ".")
		if len(parts) >= 2 {
			lastPart := strings.ToUpper(parts[len(parts)-1])
			switch lastPart {
			case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS":
				method = lastPart
				routePath = strings.TrimSuffix(routePath, "."+strings.ToLower(lastPart))
			}
		}
		if !strings.HasPrefix(routePath, "/") {
			routePath = "/" + routePath
		}
		name := fmt.Sprintf("%s %s", method, routePath)
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, 1)
		setProps(&ent, "framework", "nuxt", "http_method", method, "route_path", routePath,
			"provenance", "INFERRED_FROM_NUXT_SERVER_HANDLER")
		addEntity(ent)
	}

	// Pages → route endpoints
	if isPagesFile {
		routePath := normalizeNuxtPath(fp)
		if idx := strings.Index(routePath, "/pages/"); idx >= 0 {
			routePath = routePath[idx+6:]
		}
		if ext2 := filepath.Ext(routePath); ext2 != "" {
			routePath = strings.TrimSuffix(routePath, ext2)
		}
		if nuxtSpecialFiles[stem] {
			// /index → /
			routePath = strings.TrimSuffix(routePath, "/index")
			if routePath == "" {
				routePath = "/"
			}
		}
		if !strings.HasPrefix(routePath, "/") {
			routePath = "/" + routePath
		}
		ent := makeEntity(routePath, "SCOPE.Operation", "endpoint", file.Path, file.Language, 1)
		setProps(&ent, "framework", "nuxt", "route_path", routePath,
			"provenance", "INFERRED_FROM_NUXT_FILE_PATH")
		addEntity(ent)
	}

	// Composables (use*)
	for _, m := range reNuxtComposable.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "composable", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nuxt", "provenance", "INFERRED_FROM_NUXT_COMPOSABLE")
		addEntity(ent)
	}

	// definePageMeta → page metadata
	for _, m := range reNuxtDefinePageMeta.FindAllStringIndex(src, -1) {
		ent := makeEntity("definePageMeta", "SCOPE.Pattern", "page_meta", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nuxt", "provenance", "INFERRED_FROM_NUXT_PAGE_META")
		addEntity(ent)
	}

	// Route middleware
	if reNuxtMiddleware.MatchString(src) {
		name := strings.TrimSuffix(stem, ".global")
		ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, 1)
		setProps(&ent, "framework", "nuxt", "provenance", "INFERRED_FROM_NUXT_MIDDLEWARE")
		addEntity(ent)
	}

	// Plugin
	if reNuxtPlugin.MatchString(src) {
		ent := makeEntity(stem, "SCOPE.Component", "plugin", file.Path, file.Language, 1)
		setProps(&ent, "framework", "nuxt", "provenance", "INFERRED_FROM_NUXT_PLUGIN")
		addEntity(ent)
	}

	// Data loaders (issue #2858): useAsyncData / useFetch family. Nuxt resolves
	// these on the server during SSR and re-hydrates the payload on the client.
	for _, m := range reNuxtDataLoader.FindAllStringSubmatchIndex(src, -1) {
		fnName := src[m[2]:m[3]]
		ent := makeEntity(fnName, "SCOPE.Operation", "data_loader", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nuxt", "loader_kind", fnName, "rendering", "universal",
			"provenance", "INFERRED_FROM_NUXT_DATA_LOADER")
		addEntity(ent)
	}

	// Server components / boundaries (issue #2858).
	//
	//   - A server-route handler (server/api, server/routes — detected above as
	//     subtype="endpoint") is server-only code → server boundary.
	//   - `*.server.vue` / `*.server.ts` components run only on the server;
	//     `*.client.vue` / `*.client.ts` only on the client (hydration boundary).
	//   - `<ClientOnly>` wraps client-only markup — an explicit hydration island.
	if isServerAPI && reNuxtHTTPHandler.MatchString(src) {
		sb := makeEntity("server_boundary:"+stem, "SCOPE.Pattern", "server_boundary", file.Path, file.Language, 1)
		setProps(&sb, "framework", "nuxt", "rendering", "server",
			"provenance", "INFERRED_FROM_NUXT_SERVER_ROUTE")
		addEntity(sb)
	}
	switch {
	case strings.HasSuffix(fp, ".server.vue") || strings.HasSuffix(fp, ".server.ts") ||
		strings.HasSuffix(fp, ".server.js"):
		emitServerOnlyModule(stem, file.Path, file.Language, "nuxt", addEntity)
	case strings.HasSuffix(fp, ".client.vue") || strings.HasSuffix(fp, ".client.ts") ||
		strings.HasSuffix(fp, ".client.js"):
		cb := makeEntity(stem, "SCOPE.Pattern", "client_boundary", file.Path, file.Language, 1)
		setProps(&cb, "framework", "nuxt", "module_scope", "client", "hydration", "client",
			"provenance", "INFERRED_FROM_CLIENT_MODULE_SUFFIX")
		addEntity(cb)
	}
	if m := reNuxtClientOnly.FindStringIndex(src); m != nil {
		cb := makeEntity("ClientOnly", "SCOPE.Pattern", "client_boundary", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&cb, "framework", "nuxt", "hydration", "client",
			"provenance", "INFERRED_FROM_NUXT_CLIENT_ONLY")
		addEntity(cb)
	}

	// Static generation (issue #2858): prerender route rules / SPA mode.
	if (reNuxtRouteRules.MatchString(src) && reNuxtPrerenderRule.MatchString(src)) || reNuxtSSRFalse.MatchString(src) {
		marker := "route_rules_prerender"
		if reNuxtSSRFalse.MatchString(src) && !reNuxtPrerenderRule.MatchString(src) {
			marker = "spa_mode"
		}
		sg := makeEntity("static_generation:"+marker, "SCOPE.Pattern", "static_generation", file.Path, file.Language, 1)
		setProps(&sg, "framework", "nuxt", "marker", marker, "rendering", "ssg",
			"provenance", "INFERRED_FROM_NUXT_PRERENDER")
		addEntity(sg)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
