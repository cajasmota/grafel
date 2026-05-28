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

// gatsby.go — Gatsby meta-framework extractor (issue #2857).
//
// Gatsby is React-based. It discovers routes from the file-system convention
// `src/pages/**` (each component file under src/pages/ becomes a route) and
// supports programmatic routes via `createPage({ path, component })` calls in
// gatsby-node.js plus client-only routes via the `@reach/router`-style
// matchPath. Page components are React components and Gatsby ships hooks
// (useStaticQuery) on top of React's own — so component + hook recognition
// reuse the shared React detection (react_shared.go).
//
// Emitted entities:
//
//	src/pages/**.{js,jsx,tsx}      → SCOPE.Operation   subtype="page_route"      (router_pattern / route_extraction)
//	createPage({path,component})   → SCOPE.Operation   subtype="programmatic_route"
//	React function/arrow/class     → SCOPE.UIComponent subtype="component"       (component_extraction, shared)
//	useStaticQuery / useXxx        → SCOPE.Operation   subtype="hook"/"hook_call" (hook_recognition, shared)

func init() {
	extreg.Register("custom_js_gatsby", &gatsbyExtractor{})
}

type gatsbyExtractor struct{}

func (e *gatsbyExtractor) Language() string { return "custom_js_gatsby" }

var (
	// createPage({ path: '/foo', component: ... }) in gatsby-node.js.
	reGatsbyCreatePage = regexp.MustCompile(
		`createPage\s*\(\s*\{[^}]*?\bpath\s*:\s*['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]`,
	)
	// Gatsby dynamic page params use [param] like Next pages router.
	reGatsbyDynParam = regexp.MustCompile(`\[([^\]]+)\]`)
	// Import from the 'gatsby' package — marks a file as Gatsby context even
	// when it lives outside src/pages or src/templates.
	reGatsbyImport = regexp.MustCompile(`(?m)(?:from\s+['"]gatsby['"]|require\(\s*['"]gatsby['"]\s*\))`)
)

func normalizeGatsbyPath(fp string) string {
	result := reGatsbyDynParam.ReplaceAllStringFunc(fp, func(s string) string {
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

func (e *gatsbyExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.gatsby_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "gatsby"),
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

	// File-convention routing: src/pages/**.
	isPagesFile := strings.Contains(fp, "/src/pages/") || strings.HasPrefix(fp, "src/pages/")
	isJSX := strings.HasSuffix(fp, ".tsx") || strings.HasSuffix(fp, ".jsx") ||
		strings.HasSuffix(fp, ".ts") || strings.HasSuffix(fp, ".js")

	if isPagesFile && isJSX {
		routePath := normalizeGatsbyPath(fp)
		if idx := strings.Index(routePath, "/src/pages/"); idx >= 0 {
			routePath = routePath[idx+len("/src/pages"):]
		} else if strings.HasPrefix(routePath, "src/pages/") {
			routePath = routePath[len("src/pages"):]
		}
		if ext2 := filepath.Ext(routePath); ext2 != "" {
			routePath = strings.TrimSuffix(routePath, ext2)
		}
		routePath = strings.TrimSuffix(routePath, "/index")
		if routePath == "" {
			routePath = "/"
		}
		if !strings.HasPrefix(routePath, "/") {
			routePath = "/" + routePath
		}
		ent := makeEntity(routePath, "SCOPE.Operation", "page_route", file.Path, file.Language, 1)
		setProps(&ent, "framework", "gatsby", "route_path", routePath, "stem", stem,
			"router", "file_system", "provenance", "INFERRED_FROM_GATSBY_FILE_PATH")
		addEntity(ent)
	}

	// Programmatic routing: createPage({ path, component }) in gatsby-node.
	for _, m := range reGatsbyCreatePage.FindAllStringSubmatchIndex(src, -1) {
		routePath := src[m[2]:m[3]]
		name := fmt.Sprintf("createPage:%s", routePath)
		ent := makeEntity(name, "SCOPE.Operation", "programmatic_route", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gatsby", "route_path", routePath,
			"router", "create_pages_api", "provenance", "INFERRED_FROM_GATSBY_CREATE_PAGE")
		addEntity(ent)
	}

	// React structure: page/template components + custom hooks + hook calls
	// (useStaticQuery and friends). Reuses the shared React detection. Gated to
	// genuine Gatsby context — a page/template file, or a file that imports from
	// 'gatsby' — so non-Gatsby React projects don't get gatsby-tagged duplicate
	// component entities (custom_js_react already covers generic React).
	isTemplate := strings.Contains(fp, "/src/templates/") || strings.HasPrefix(fp, "src/templates/")
	isGatsbyContext := isPagesFile || isTemplate || reGatsbyImport.MatchString(src)
	isJSXFile := strings.HasSuffix(fp, ".tsx") || strings.HasSuffix(fp, ".jsx")
	if isJSXFile && isGatsbyContext {
		extractReactStructure(src, file.Path, file.Language, "gatsby", addEntity)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
