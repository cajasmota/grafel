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
	reSvelteDynParam = regexp.MustCompile(`\[([^\]]+)\]`)
	reSvelteGroupPath = regexp.MustCompile(`\([^)]+\)`)
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
	compName := toPascalCase(stem)

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
		ent := makeEntity(compName, "SCOPE.UIComponent", subtype, file.Path, file.Language, 1)
		setProps(&ent, "framework", "svelte", "route_path", routePath,
			"provenance", "INFERRED_FROM_SVELTE_COMPONENT")
		addEntity(ent)
	}

	// SvelteKit load() function
	if reSvelteLoad.MatchString(src) {
		name := fmt.Sprintf("load:%s", routePath)
		ent := makeEntity(name, "SCOPE.Operation", "loader", file.Path, file.Language, 1)
		setProps(&ent, "framework", "svelte", "route_path", routePath,
			"provenance", "INFERRED_FROM_SVELTE_LOAD")
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

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
