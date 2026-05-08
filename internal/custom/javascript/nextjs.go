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
	extreg.Register("custom_js_nextjs", &nextjsExtractor{})
}

type nextjsExtractor struct{}

func (e *nextjsExtractor) Language() string { return "custom_js_nextjs" }

var (
	reNextjsHTTPHandler = regexp.MustCompile(
		`export\s+(?:async\s+)?function\s+(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*\(`,
	)
	reNextjsServerSideProps = regexp.MustCompile(
		`export\s+(?:async\s+)?function\s+(getServerSideProps|getStaticProps|getStaticPaths)\s*\(`,
	)
	reNextjsServerAction = regexp.MustCompile(
		`['"]use server['"]`,
	)
	reNextjsServerActionFn = regexp.MustCompile(
		`export\s+(?:async\s+)?function\s+(\w+)\s*\(`,
	)
	reNextjsDynParam  = regexp.MustCompile(`\[([^\]]+)\]`)
	reNextjsGroupPath = regexp.MustCompile(`\([^)]+\)`)
)

var (
	nextjsPageFiles    = map[string]bool{"page": true, "layout": true, "loading": true, "error": true, "not-found": true, "template": true, "default": true}
	nextjsStructural   = map[string]bool{"layout": true, "loading": true, "error": true, "not-found": true, "template": true, "default": true}
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
	tracer := otel.Tracer("archigraph/custom/javascript")
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
	isAPIRoute := strings.Contains(fp, "/api/") || strings.HasPrefix(fp, "api/") || stem == "route"

	// App Router: HTTP method handlers in route.ts
	if isAppRouter && isAPIRoute {
		for _, m := range reNextjsHTTPHandler.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
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

	// getServerSideProps / getStaticProps / getStaticPaths
	for _, m := range reNextjsServerSideProps.FindAllStringSubmatchIndex(src, -1) {
		fnName := src[m[2]:m[3]]
		rendering := map[string]string{
			"getServerSideProps": "ssr",
			"getStaticProps":     "ssg",
			"getStaticPaths":     "ssg",
		}[fnName]
		ent := makeEntity(fnName, "SCOPE.Operation", "data_fetcher", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nextjs", "rendering", rendering,
			"provenance", "INFERRED_FROM_NEXTJS_DATA_FETCHER")
		addEntity(ent)
	}

	// Server actions ("use server" + exported async functions)
	if reNextjsServerAction.MatchString(src) {
		for _, m := range reNextjsServerActionFn.FindAllStringSubmatchIndex(src, -1) {
			fnName := src[m[2]:m[3]]
			ent := makeEntity(fnName, "SCOPE.Operation", "server_action", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "nextjs", "provenance", "INFERRED_FROM_NEXTJS_SERVER_ACTION")
			addEntity(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
