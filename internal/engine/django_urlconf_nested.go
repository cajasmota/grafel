// Django URLconf nested include() composition pass.
//
// Problem: Django's URL routing uses two-level include() composition:
//
//   # myproject/urls.py
//   urlpatterns = [
//       path("api/v1/", include("api.urls")),
//   ]
//   # api/urls.py
//   urlpatterns = [
//       path("users/<int:id>/checklists/", ChecklistView.as_view()),
//   ]
//
// The full HTTP route is `/api/v1/users/{id}/checklists`. The per-file
// YAML + AST passes emit separate Route entities for each file independently
// — they cannot compose across files.
//
// This pass runs AFTER Pass 2.5 has finished, with the complete set of
// classified Python source files available. It:
//
//  1. Scans every Python file for `path("<prefix>", include("<module.path>"))` calls.
//  2. Resolves `<module.path>` (e.g. "api.urls") to a repo-relative file
//     path (e.g. "api/urls.py").
//  3. Parses the included file's source for its `path(...)` route declarations.
//  4. For each child route: prepends the parent prefix, calls
//     httproutes.Canonicalize, and emits one `http_endpoint` entity.
//
// The emitted entities have kind=http_endpoint and ID/Name of the form
// `http:ANY:<canonical-path>` — identical to what applyHTTPEndpointSynthesis
// would emit if it had cross-file context. This lets the cross-repo HTTP
// linker (#645) match them against consumer-side fetch/axios calls.
//
// Refs #645 residual analysis.
package engine

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

// djangoIncludeStringRe matches `path("prefix", include("module.path"))` or
// the re_path variant. It captures the parent prefix (group 1) and the
// included module path string (group 2).
//
// Accepted forms:
//   - path('api/v1/', include('api.urls'))
//   - path("api/v1/", include("api.urls"))
//   - re_path(r'^api/v1/', include('api.urls'))
//
// We do NOT match include(<router>.urls) here (attribute access) — that is
// handled by the existing applyDjangoRouteComposition pass in django_routes.go.
var djangoIncludeStringRe = regexp.MustCompile(
	`(?:re_)?path\s*\(\s*r?["']([^"']*)["']\s*,\s*include\s*\(\s*["']([^"'.][^"']*)["']\s*\)`)

// djangoChildPathRe matches a `path(...)` call in a child urls.py and
// captures the route pattern (group 1) and the view/handler reference
// (group 2). The handler may be a bare identifier, a dotted name, or a
// `ClassName.as_view()` call.
//
// We intentionally exclude lines where the second argument is itself an
// include(...) call — recursive nesting is handled by the outer loop below.
var djangoChildPathRe = regexp.MustCompile(
	`(?:re_)?path\s*\(\s*r?["']([^"']*)["']\s*,\s*([\w.]+(?:\s*\.\s*as_view\s*\(\s*\))?)`)

// djangoChildIncludeStringRe detects whether a child file itself
// includes further sub-modules (two levels of nesting). We recurse one
// level only — deeper nesting is uncommon and infinite recursion is
// avoided by the depth limit in resolveIncludedRoutes.
var djangoChildIncludeStringRe = regexp.MustCompile(
	`(?:re_)?path\s*\(\s*r?["']([^"']*)["']\s*,\s*include\s*\(\s*["']([^"'.][^"']*)["']\s*\)`)

// djangoRouterRegisterRe matches `router.register(r"prefix", ViewSet)` calls
// in DRF-style routers files. Captures the route prefix (group 1).
// This handles the case where a child file (e.g. routers.py) uses DRF
// DefaultRouter/SimpleRouter registrations rather than plain path() calls.
var djangoRouterRegisterRe = regexp.MustCompile(
	`(?:[\w]*[Rr]outer|api_router|v\d+_router|router_v\d+)\.register\s*\(\s*r?["']([^"']*)["']`)

// NestedURLConfFileReader is a function that returns the source bytes for a
// repo-relative file path, or nil if the file is not available.
type NestedURLConfFileReader func(relPath string) []byte

// ApplyDjangoNestedURLConf runs the cross-file URLconf composition pass.
// It returns additional http_endpoint EntityRecords derived from nested
// include() chains. The caller appends these to the existing entity slice.
//
// `fileReader` is a callback used to retrieve file contents by repo-relative
// path. It returns nil when a path is not available (not in the classified
// set, or outside the repo).
//
// `parentFiles` is the set of Python source file paths (repo-relative) that
// should be scanned as potential roots. Only files whose base name ends in
// "urls.py" are scanned — all other Python files are skipped for efficiency.
func ApplyDjangoNestedURLConf(
	parentFiles []string,
	fileReader NestedURLConfFileReader,
) []types.EntityRecord {
	var out []types.EntityRecord
	seen := map[string]bool{}

	for _, relPath := range parentFiles {
		if !isDjangoURLFile(relPath) {
			continue
		}
		content := fileReader(relPath)
		if len(content) == 0 {
			continue
		}
		src := string(content)

		// Find all `path("prefix", include("module.path"))` bindings.
		for _, m := range djangoIncludeStringRe.FindAllStringSubmatch(src, -1) {
			parentPrefix := m[1]
			modulePath := m[2]

			// Resolve the Python module path to a repo-relative file path.
			childRelPath := modulePathToFilePath(modulePath)
			if childRelPath == "" {
				continue
			}

			childContent := fileReader(childRelPath)
			if len(childContent) == 0 {
				// Try common alternative: same directory as parent.
				childRelPath = modulePathToFilePath_relToParent(modulePath, relPath)
				if childRelPath != "" {
					childContent = fileReader(childRelPath)
				}
			}
			if len(childContent) == 0 {
				continue
			}

			childRoutes := extractChildRoutes(string(childContent), fileReader, childRelPath, 0)

			for _, childRoute := range childRoutes {
				composed := joinDjangoRoutePaths(parentPrefix, childRoute)
				canonical := httproutes.Canonicalize(httproutes.FrameworkDjango, composed)
				if canonical == "" || canonical == "/" {
					continue
				}
				id := httproutes.SyntheticID("ANY", canonical)
				if seen[id] {
					continue
				}
				seen[id] = true

				out = append(out, types.EntityRecord{
					ID:         id,
					Name:       id,
					Kind:       httpEndpointKind,
					SourceFile: relPath,
					Language:   "python",
					Properties: map[string]string{
						"verb":         "ANY",
						"path":         canonical,
						"framework":    "django",
						"pattern_type": "urlconf_nested_include",
					},
					EnrichmentRequired: false,
					EnrichmentStatus:   types.StatusPending,
					QualityScore:       0.8,
				})
			}
		}
	}
	return out
}

// extractChildRoutes returns all route patterns declared in a child file
// (urls.py, routers.py, or any Python file referenced by include()). It
// handles two patterns:
//
//  1. Plain `path("pattern", view)` calls → extract pattern directly.
//  2. DRF `<router>.register("prefix", ViewSet)` calls → extract prefix.
//
// It also handles one level of recursive string include() nesting (depth
// limit prevents infinite loops on circular imports).
func extractChildRoutes(src string, fileReader NestedURLConfFileReader, filePath string, depth int) []string {
	const maxDepth = 2
	var routes []string

	// Direct routes (non-include path() calls).
	for _, m := range djangoChildPathRe.FindAllStringSubmatch(src, -1) {
		pattern := m[1]
		handler := m[2]
		// Skip calls where the second argument is `include(...)` — those are
		// recursive includes handled separately below, or DRF
		// `include(router.urls)` which the DRF-router pass handles.
		if strings.HasPrefix(strings.TrimSpace(handler), "include") {
			continue
		}
		routes = append(routes, pattern)
	}

	// DRF router.register() calls — handles routers.py style child files.
	// e.g. `router.register(r"users", UserViewSet)` → yields "users".
	for _, m := range djangoRouterRegisterRe.FindAllStringSubmatch(src, -1) {
		routes = append(routes, m[1])
	}

	// Recursive nested string include() calls in the child file.
	if depth < maxDepth {
		for _, m := range djangoChildIncludeStringRe.FindAllStringSubmatch(src, -1) {
			subPrefix := m[1]
			subModule := m[2]

			subRelPath := modulePathToFilePath(subModule)
			if subRelPath == "" {
				subRelPath = modulePathToFilePath_relToParent(subModule, filePath)
			}
			if subRelPath == "" || fileReader == nil {
				continue
			}
			subContent := fileReader(subRelPath)
			if len(subContent) == 0 {
				continue
			}
			subRoutes := extractChildRoutes(string(subContent), fileReader, subRelPath, depth+1)
			for _, sr := range subRoutes {
				routes = append(routes, joinDjangoRoutePaths(subPrefix, sr))
			}
		}
	}

	return routes
}

// modulePathToFilePath converts a Python module path (e.g. "api.urls" or
// "apps.users.urls") to a repo-relative file path (e.g. "api/urls.py").
// Returns "" when the module path is not convertible (e.g. it references a
// third-party package without a recognisable path component).
func modulePathToFilePath(modulePath string) string {
	if modulePath == "" {
		return ""
	}
	// Replace dots with path separators and append .py.
	// "api.urls"       → "api/urls.py"
	// "apps.users.urls" → "apps/users/urls.py"
	parts := strings.Split(modulePath, ".")
	return filepath.Join(parts...) + ".py"
}

// modulePathToFilePath_relToParent tries to resolve a Python module path
// relative to the parent file's directory. This handles the common pattern
// where include("urls") is used within the same app directory.
func modulePathToFilePath_relToParent(modulePath, parentPath string) string {
	if modulePath == "" || parentPath == "" {
		return ""
	}
	parentDir := filepath.Dir(parentPath)
	if parentDir == "." {
		return ""
	}
	parts := strings.Split(modulePath, ".")
	return filepath.Join(append([]string{parentDir}, parts...)...) + ".py"
}

// isDjangoURLFile reports whether the repo-relative path looks like a Django
// URLconf file. We only scan files whose base name ends in "urls.py" to
// avoid false positives from other Python files that might incidentally
// contain the word "path" or "include".
func isDjangoURLFile(relPath string) bool {
	base := filepath.Base(relPath)
	// Matches: urls.py, myapp_urls.py, api_urls.py, etc.
	return strings.HasSuffix(base, "urls.py")
}
