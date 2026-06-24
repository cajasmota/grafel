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

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extreg.Register("custom_js_nextjs", &nextjsExtractor{})
}

type nextjsExtractor struct{}

func (e *nextjsExtractor) Language() string { return "custom_js_nextjs" }

var (
	// App-Router Route Handler verb exports — both the function-declaration
	// form (`export async function GET(`) and the const arrow / function-expr
	// form (`export const GET = `) (#5486). Group 1 (function) or group 2
	// (const) is the verb.
	reNextjsHTTPHandler = regexp.MustCompile(
		`export\s+(?:async\s+)?function\s+(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*\(` +
			`|export\s+const\s+(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*=`,
	)
	reNextjsServerSideProps = regexp.MustCompile(
		`export\s+(?:async\s+)?function\s+(getServerSideProps|getStaticProps|getStaticPaths)\s*\(`,
	)
	reNextjsServerActionFn = regexp.MustCompile(
		`export\s+(?:async\s+)?function\s+(\w+)\s*\(`,
	)
	// Function-level inline Server Action (#5487): `async function f(){ 'use server'; … }`
	// or an arrow `const f = async () => { 'use server'; … }`. The directive is the
	// FIRST statement of the body, so each such function is an action regardless of
	// whether the whole module carries a file-level `'use server'`.
	reNextjsInlineServerAction = regexp.MustCompile(
		`(?:async\s+function\s+(\w+)\s*\([^)]*\)|(?:export\s+)?const\s+(\w+)\s*=\s*async\s*\([^)]*\)\s*=>)\s*\{\s*['"]use server['"]`,
	)
	// Wrapped Server Action (#5487): the `action()`-wrapper idiom (next-safe-action /
	// zsa / custom factories) — `export const doThing = action(schema, async (input)=>{…})`
	// or `action(async ()=>{})`. Group 1 = exported const name; the rest of the line
	// is inspected for an optional leading validation-schema argument. The callee is
	// gated to the action-wrapper name set (reNextjsActionWrapperCallee) so an ordinary
	// `const x = foo()` is not misread as an action.
	reNextjsWrappedServerAction = regexp.MustCompile(
		`export\s+const\s+(\w+)\s*=\s*(\w+)\s*\(([^)]*)`,
	)
	// reNextjsWrappedActionSchema captures a validation-schema first argument when the
	// wrapped action is called as `action(schema, async …)` — a bare identifier or
	// member ref (e.g. `createPostSchema`, `schemas.createPost`) immediately followed
	// by a comma before the handler. A leading `async`/`function`/`(` means the first
	// arg is the handler itself (no schema).
	reNextjsWrappedActionSchema = regexp.MustCompile(
		`^\s*([A-Za-z_$][\w$.]*)\s*,`,
	)
	reNextjsDynParam  = regexp.MustCompile(`\[([^\]]+)\]`)
	reNextjsGroupPath = regexp.MustCompile(`\([^)]+\)`)

	// App-Router data loaders + SSG markers (issue #2858).
	// generateStaticParams() is the App-Router equivalent of the Pages-Router
	// getStaticPaths — it drives static generation of dynamic segments.
	reNextjsGenerateStaticParams = regexp.MustCompile(
		`export\s+(?:async\s+)?function\s+(generateStaticParams)\s*\(`,
	)
	reNextjsGenerateMetadata = regexp.MustCompile(
		`export\s+(?:async\s+)?function\s+(generateMetadata)\s*\(`,
	)
	// `export const dynamic = 'force-static'|'force-dynamic'|'auto'|'error'`
	// and `export const revalidate = N` are the App-Router route-segment config
	// knobs that select static vs dynamic rendering.
	reNextjsRouteSegmentDynamic = regexp.MustCompile(
		`export\s+const\s+dynamic\s*=\s*['"](force-static|force-dynamic|auto|error)['"]`,
	)
	reNextjsRouteSegmentRevalidate = regexp.MustCompile(
		`export\s+const\s+revalidate\s*=\s*(\d+|false)`,
	)

	// Next.js middleware (issue #2878, middleware_runtime_detection). A
	// project-root `middleware.{ts,js}` exporting `middleware()` (or a default)
	// runs in the Edge runtime by default; `export const config = { matcher, runtime }`
	// declares the path matcher and (optionally) opts the function into the
	// 'nodejs' runtime. We detect the middleware export and the runtime/matcher
	// from its `config` object.
	reNextjsMiddlewareExport = regexp.MustCompile(
		`export\s+(?:async\s+)?function\s+middleware\s*\(|export\s+default\s+(?:async\s+)?function\s+middleware\b|export\s+const\s+middleware\s*=`,
	)
	reNextjsConfigRuntime = regexp.MustCompile(`\bruntime\s*:\s*['"](edge|nodejs|experimental-edge)['"]`)
	reNextjsConfigMatcher = regexp.MustCompile(`\bmatcher\s*:`)

	// next.config detection (issue #2878, next_config_detection). The
	// `next.config.{js,ts,mjs,cjs}` file is the framework's build/runtime config;
	// it is recognised by file name and its `defineConfig`/`NextConfig`/default
	// export shape.
	reNextjsConfigExport = regexp.MustCompile(
		`export\s+default\b|module\.exports\s*=|\bdefineConfig\s*\(|:\s*NextConfig\b`,
	)
)

// nextjsActionWrapperCallees is the configurable set of higher-order
// Server-Action wrapper factory names (#5487). The `action()`-wrapper idiom
// (next-safe-action `action`/`actionClient`, zsa, and common custom factories)
// wraps a server-action handler in a validating/authorizing closure. A
// `const x = <callee>(…)` is only treated as a wrapped Server Action when the
// callee is in this set OR the file/inline `'use server'` context applies, so an
// ordinary `const x = foo()` is not misread as an action.
var nextjsActionWrapperCallees = map[string]bool{
	"action":                 true,
	"actionClient":           true,
	"authAction":             true,
	"safeAction":             true,
	"adminAction":            true,
	"publicAction":           true,
	"protectedAction":        true,
	"createServerAction":     true,
	"createSafeActionClient": true,
}

var (
	nextjsPageFiles           = map[string]bool{"page": true, "layout": true, "loading": true, "error": true, "not-found": true, "template": true, "default": true}
	nextjsStructural          = map[string]bool{"layout": true, "loading": true, "error": true, "not-found": true, "template": true, "default": true}
	nextjsPagesRouterNonRoute = map[string]bool{"_app": true, "_document": true, "_error": true}
)

func normalizeNextjsPath(filePath string) string {
	// Normalize path: [param] -> {param}, [...param] -> {param*}, [[...param]] -> {param?}
	result := reNextjsDynParam.ReplaceAllStringFunc(filePath, func(s string) string {
		inner := s[1 : len(s)-1] // strip brackets
		if strings.HasPrefix(inner, "...") {
			return "{" + inner[3:] + "*}"
		}
		return "{" + inner + "}"
	})
	// Strip route groups (group) - invisible in routing
	result = reNextjsGroupPath.ReplaceAllString(result, "")
	// Normalize double slashes
	for strings.Contains(result, "//") {
		result = strings.ReplaceAll(result, "//", "/")
	}
	return result
}

func (e *nextjsExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.nextjs_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "nextjs"),
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
	stem := strings.TrimSuffix(filepath.Base(fp), filepath.Ext(fp))
	stem = strings.TrimSuffix(stem, ".tsx")
	stem = strings.TrimSuffix(stem, ".ts")
	stem = strings.TrimSuffix(stem, ".jsx")
	stem = strings.TrimSuffix(stem, ".js")

	// Accept both absolute (/app/) and relative (app/) path prefixes.
	isAppRouter := strings.Contains(fp, "/app/") || strings.HasPrefix(fp, "app/")
	isPagesRouter := strings.Contains(fp, "/pages/") || strings.HasPrefix(fp, "pages/")

	// App Router: HTTP method handlers in route.{ts,js,tsx} (#5486). Gate on
	// the `route` basename so page.tsx / arbitrary verb exports under /api/ are
	// NOT treated as Route Handlers; App Router permits route.* anywhere under
	// app/, not only under api/.
	if isAppRouter && stem == "route" {
		seenVerb := make(map[string]bool)
		for _, m := range reNextjsHTTPHandler.FindAllStringSubmatchIndex(src, -1) {
			// Group 1 = function form; group 2 = const form.
			var method string
			if m[2] >= 0 {
				method = src[m[2]:m[3]]
			} else if m[4] >= 0 {
				method = src[m[4]:m[5]]
			} else {
				continue
			}
			if seenVerb[method] {
				continue
			}
			seenVerb[method] = true
			routePath := normalizeNextjsPath(fp)
			// strip app/ prefix and route/page suffixes
			if idx := strings.Index(routePath, "/app/"); idx >= 0 {
				routePath = routePath[idx+4:]
			}
			routePath = strings.TrimSuffix(routePath, "/route")
			routePath = strings.TrimSuffix(routePath, "/page")
			if !strings.HasPrefix(routePath, "/") {
				routePath = "/" + routePath
			}
			name := fmt.Sprintf("%s %s", method, routePath)
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "nextjs", "http_method", method,
				"route_path", routePath, "is_app_router", "true",
				"provenance", "INFERRED_FROM_NEXTJS_API_ROUTE")
			addEntity(ent)
		}
	}

	// Pages Router: page files become route endpoints
	if isPagesRouter && !nextjsPagesRouterNonRoute[stem] {
		routePath := normalizeNextjsPath(fp)
		if idx := strings.Index(routePath, "/pages/"); idx >= 0 {
			routePath = routePath[idx+6:]
		}
		// strip /index suffix
		routePath = strings.TrimSuffix(routePath, "/index")
		// strip file extension
		if ext := filepath.Ext(routePath); ext != "" {
			routePath = strings.TrimSuffix(routePath, ext)
		}
		if !strings.HasPrefix(routePath, "/") {
			routePath = "/" + routePath
		}
		name := routePath
		isAPI := strings.Contains(fp, "/pages/api/")
		subtype := "endpoint"
		if isAPI {
			subtype = "api_route"
		}
		ent := makeEntity(name, "SCOPE.Operation", subtype, file.Path, file.Language, 1)
		setProps(&ent, "framework", "nextjs", "route_path", routePath,
			"is_app_router", "false", "provenance", "INFERRED_FROM_NEXTJS_FILE_PATH")
		addEntity(ent)
	}

	// App Router: page.tsx / layout.tsx structural files
	if isAppRouter && nextjsPageFiles[stem] {
		routePath := normalizeNextjsPath(fp)
		if idx := strings.Index(routePath, "/app/"); idx >= 0 {
			routePath = routePath[idx+4:]
		}
		for suffix := range nextjsPageFiles {
			routePath = strings.TrimSuffix(routePath, "/"+suffix)
		}
		if ext := filepath.Ext(routePath); ext != "" {
			routePath = strings.TrimSuffix(routePath, ext)
		}
		if !strings.HasPrefix(routePath, "/") {
			routePath = "/" + routePath
		}
		var kind, subtype string
		if nextjsStructural[stem] {
			kind = "SCOPE.UIComponent"
			subtype = stem
		} else {
			kind = "SCOPE.Operation"
			subtype = "endpoint"
		}
		name := routePath + "(" + stem + ")"
		ent := makeEntity(name, kind, subtype, file.Path, file.Language, 1)
		setProps(&ent, "framework", "nextjs", "route_path", routePath,
			"file_type", stem, "is_app_router", "true",
			"provenance", "INFERRED_FROM_NEXTJS_FILE_PATH")
		addEntity(ent)
	}

	// Data loaders + static-generation markers (issue #2858).
	//
	// Pages Router: getServerSideProps (SSR), getStaticProps + getStaticPaths
	// (SSG). App Router: generateStaticParams (SSG of dynamic segments),
	// generateMetadata (server-side metadata loader). These are the
	// framework data-loading functions (data_loaders) and the ones that mark a
	// route for static generation (static_generation).
	emitNextDataLoader := func(fnName string, off int, loaderKind, rendering string, ssg bool) {
		ent := makeEntity(fnName, "SCOPE.Operation", "data_loader", file.Path, file.Language, lineOf(src, off))
		setProps(&ent, "framework", "nextjs", "loader_kind", loaderKind, "rendering", rendering,
			"provenance", "INFERRED_FROM_NEXTJS_DATA_LOADER")
		addEntity(ent)
		if ssg {
			sgent := makeEntity("ssg:"+fnName, "SCOPE.Pattern", "static_generation", file.Path, file.Language, lineOf(src, off))
			setProps(&sgent, "framework", "nextjs", "marker", fnName, "rendering", "ssg",
				"provenance", "INFERRED_FROM_NEXTJS_SSG_MARKER")
			addEntity(sgent)
		}
	}
	for _, m := range reNextjsServerSideProps.FindAllStringSubmatchIndex(src, -1) {
		fnName := src[m[2]:m[3]]
		switch fnName {
		case "getServerSideProps":
			emitNextDataLoader(fnName, m[0], fnName, "ssr", false)
		case "getStaticProps", "getStaticPaths":
			emitNextDataLoader(fnName, m[0], fnName, "ssg", true)
		}
	}
	for _, m := range reNextjsGenerateStaticParams.FindAllStringSubmatchIndex(src, -1) {
		emitNextDataLoader("generateStaticParams", m[0], "generateStaticParams", "ssg", true)
	}
	for _, m := range reNextjsGenerateMetadata.FindAllStringSubmatchIndex(src, -1) {
		emitNextDataLoader("generateMetadata", m[0], "generateMetadata", "server", false)
	}
	// Route-segment config: `export const dynamic = 'force-static'` /
	// `export const revalidate = 3600` (static_generation).
	if m := reNextjsRouteSegmentDynamic.FindStringSubmatchIndex(src); m != nil {
		mode := src[m[2]:m[3]]
		ent := makeEntity("dynamic:"+mode, "SCOPE.Pattern", "static_generation", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nextjs", "segment_config", "dynamic", "mode", mode,
			"provenance", "INFERRED_FROM_NEXTJS_ROUTE_SEGMENT_CONFIG")
		addEntity(ent)
	}
	if m := reNextjsRouteSegmentRevalidate.FindStringSubmatchIndex(src); m != nil {
		ent := makeEntity("revalidate:"+src[m[2]:m[3]], "SCOPE.Pattern", "static_generation", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nextjs", "segment_config", "revalidate", "value", src[m[2]:m[3]],
			"provenance", "INFERRED_FROM_NEXTJS_ROUTE_SEGMENT_CONFIG")
		addEntity(ent)
	}

	// React structure: page/layout components + custom hooks + hook calls.
	// Next.js pages are React components, so reuse the shared React component /
	// hook recognition (issue #2857, Structure group). Gated to Next router
	// context (app/ or pages/) AND JSX files, so neither API route .ts handlers
	// nor non-Next React projects pick up nextjs-tagged duplicate components
	// (custom_js_react covers generic React).
	isJSXFile := strings.HasSuffix(fp, ".tsx") || strings.HasSuffix(fp, ".jsx")
	if isJSXFile && (isAppRouter || isPagesRouter) {
		extractReactStructure(src, file.Path, file.Language, "nextjs", addEntity)
	}

	// Server components / hydration boundaries (issue #2858).
	//
	// Next.js App Router uses the React Server Components model: every component
	// is a Server Component by default and opts into client-side interactivity
	// (hydration) with the `'use client'` directive; a `'use server'` directive
	// marks a Server Action module. The `*.server.{ts,tsx}` suffix forces a
	// server-only module in both routers. Gated to the JSX page/layout files of
	// the App Router so non-route .ts utilities don't get tagged.
	hasUseClient := emitRSCBoundary(src, file.Path, file.Language, "nextjs", addEntity)
	isServerModule := strings.HasSuffix(fp, ".server.ts") || strings.HasSuffix(fp, ".server.tsx") ||
		strings.HasSuffix(fp, ".server.js") || strings.HasSuffix(fp, ".server.jsx")
	if isServerModule {
		emitServerOnlyModule(stem, file.Path, file.Language, "nextjs", addEntity)
	}
	// App-Router JSX page/layout with no `'use client'` → implicit Server
	// Component (the RSC default). Gated to genuine route component files so
	// non-route .ts utilities don't get the implicit-server marker.
	if isJSXFile && isAppRouter && nextjsPageFiles[stem] && !hasUseClient {
		addEntity(metafwServerComponentEntity(stem, file.Path, file.Language, "nextjs"))
	}

	// Server Actions (#2858, #5487). Recognised in three forms, each emitted as a
	// SCOPE.Operation/server_action operation bound to its module:
	//
	//  1. File-level `'use server'` directive → every exported async function in
	//     the module is an action.
	//  2. Function-level inline `'use server'` → an `async function f(){ 'use server'; … }`
	//     (or arrow const) is an action regardless of any module-level directive.
	//  3. Wrapped actions → the `action()`-wrapper idiom
	//     `export const doThing = action(schema, async (input)=>{…})`, where the
	//     callee is an action-wrapper factory name (next-safe-action / zsa / custom).
	//     A leading validation-schema argument is captured as `validation_schema`.
	seenAction := map[string]bool{}
	emitAction := func(name string, off int, extraKV ...string) {
		if name == "" || seenAction[name] {
			return
		}
		seenAction[name] = true
		ent := makeEntity(name, "SCOPE.Operation", "server_action", file.Path, file.Language, lineOf(src, off))
		setProps(&ent, "framework", "nextjs", "rendering", "server",
			"provenance", "INFERRED_FROM_NEXTJS_SERVER_ACTION")
		if len(extraKV) > 0 {
			setProps(&ent, extraKV...)
		}
		addEntity(ent)
	}

	// Form 1: file-level `'use server'` → exported async functions are actions.
	if reUseServerDirective.MatchString(src) {
		for _, m := range reNextjsServerActionFn.FindAllStringSubmatchIndex(src, -1) {
			emitAction(src[m[2]:m[3]], m[0])
		}
	}

	// Form 2: function-level inline `'use server'` directive.
	for _, m := range reNextjsInlineServerAction.FindAllStringSubmatchIndex(src, -1) {
		name := ""
		if m[2] != -1 {
			name = src[m[2]:m[3]] // named function
		} else if m[4] != -1 {
			name = src[m[4]:m[5]] // arrow const
		}
		emitAction(name, m[0])
	}

	// Form 3: wrapped actions via the `action()`-wrapper idiom. Gated to the
	// action-wrapper callee set so ordinary `const x = foo()` is not an action.
	for _, m := range reNextjsWrappedServerAction.FindAllStringSubmatchIndex(src, -1) {
		callee := src[m[4]:m[5]]
		if !nextjsActionWrapperCallees[callee] {
			continue
		}
		name := src[m[2]:m[3]]
		args := src[m[6]:m[7]]
		// Capture a leading validation-schema argument (`action(schema, handler)`)
		// when the first arg is a bare identifier / member ref, not the handler.
		var extra []string
		if sm := reNextjsWrappedActionSchema.FindStringSubmatch(args); sm != nil {
			first := sm[1]
			if first != "async" && first != "function" {
				extra = append(extra, "validation_schema", first)
			}
		}
		extra = append(extra, "action_wrapper", callee)
		emitAction(name, m[0], extra...)
	}

	// Middleware runtime detection (issue #2878, middleware_runtime_detection).
	//
	// Next.js runs a root `middleware.{ts,js}` on every matched request. It
	// executes in the Edge runtime by default; `export const config` declares the
	// `matcher` paths and may opt into `runtime: 'nodejs'`. Detecting the
	// middleware export + its runtime/matcher is the idiom — a request-pipeline
	// interceptor distinct from route handlers.
	isMiddlewareFile := stem == "middleware" &&
		(fp == "middleware.ts" || fp == "middleware.js" ||
			strings.HasSuffix(fp, "/middleware.ts") || strings.HasSuffix(fp, "/middleware.js") ||
			strings.HasSuffix(fp, "/src/middleware.ts") || strings.HasSuffix(fp, "/src/middleware.js"))
	if isMiddlewareFile && reNextjsMiddlewareExport.MatchString(src) {
		runtime := "edge" // Next.js middleware defaults to the Edge runtime.
		if m := reNextjsConfigRuntime.FindStringSubmatch(src); m != nil {
			runtime = m[1]
		}
		ent := makeEntity("middleware", "SCOPE.Pattern", "middleware", file.Path, file.Language, 1)
		setProps(&ent, "framework", "nextjs", "runtime", runtime,
			"has_matcher", fmt.Sprintf("%v", reNextjsConfigMatcher.MatchString(src)),
			"provenance", "INFERRED_FROM_NEXTJS_MIDDLEWARE")
		addEntity(ent)
	}

	// next.config detection (issue #2878, next_config_detection). The
	// `next.config.{js,ts,mjs,cjs}` file configures the Next.js build/runtime
	// (rewrites, redirects, images, experimental flags). Recognise it by name +
	// its config-export shape so the project's framework configuration is a
	// first-class, queryable node.
	isNextConfig := stem == "next.config" &&
		(strings.HasSuffix(fp, "next.config.js") || strings.HasSuffix(fp, "next.config.ts") ||
			strings.HasSuffix(fp, "next.config.mjs") || strings.HasSuffix(fp, "next.config.cjs"))
	if isNextConfig && reNextjsConfigExport.MatchString(src) {
		ent := makeEntity("next.config", "SCOPE.Pattern", "framework_config", file.Path, file.Language, 1)
		setProps(&ent, "framework", "nextjs", "config_kind", "next_config",
			"provenance", "INFERRED_FROM_NEXTJS_CONFIG")
		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
