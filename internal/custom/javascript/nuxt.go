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
)

var (
	nuxtServerDirs = map[string]bool{"server/api": true, "server/routes": true}
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

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
