// Django URLconf nested include() composition pass.
//
// Problem: Django's URL routing uses two-level include() composition:
//
//	# myproject/urls.py
//	urlpatterns = [
//	    path("api/v1/", include("api.urls")),
//	]
//	# api/urls.py
//	urlpatterns = [
//	    path("users/<int:id>/checklists/", ChecklistView.as_view()),
//	]
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
	"path"
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

			for _, cr := range childRoutes {
				composed := joinDjangoRoutePaths(parentPrefix, cr.pattern)
				canonical := httproutes.Canonicalize(httproutes.FrameworkDjango, composed)
				if canonical == "" || canonical == "/" {
					continue
				}
				id := httproutes.SyntheticID("ANY", canonical)
				if seen[id] {
					continue
				}
				seen[id] = true

				props := map[string]string{
					"verb":         "ANY",
					"path":         canonical,
					"framework":    "django",
					"pattern_type": "urlconf_nested_include",
				}
				// Issue #527 — wire FBV view functions as source_handler so
				// the ResolveHTTPEndpointHandlers pass emits an IMPLEMENTS
				// edge from the view function to this http_endpoint entity.
				// CBV as_view() handlers are handled separately by the CBV
				// pass (django_drf_actions.go) and are left without a
				// source_handler here to avoid conflicts.
				if cr.handler != "" {
					props["source_handler"] = "Controller:" + cr.handler
				}

				out = append(out, types.EntityRecord{
					ID:                 id,
					Name:               id,
					Kind:               httpEndpointKind,
					SourceFile:         relPath,
					Language:           "python",
					Properties:         props,
					EnrichmentRequired: false,
					EnrichmentStatus:   types.StatusPending,
					QualityScore:       0.8,
				})
			}
		}
	}
	return out
}

// childRoute pairs a URL pattern with its resolved view handler reference.
// handler is the bare function name extracted from the path() call (e.g.
// "user_list" from "views.user_list"), or "" when the handler could not be
// resolved to a simple FBV name (CBV as_view() calls, anonymous lambdas, etc.).
type childRoute struct {
	pattern string
	handler string // bare FBV name, or "" for CBVs / unknown
}

// extractChildRoutes returns all route patterns declared in a child file
// (urls.py, routers.py, or any Python file referenced by include()). It
// handles two patterns:
//
//  1. Plain `path("pattern", view)` calls → extract pattern + handler directly.
//  2. DRF `<router>.register("prefix", ViewSet)` calls → extract prefix only.
//
// It also handles one level of recursive string include() nesting (depth
// limit prevents infinite loops on circular imports).
func extractChildRoutes(src string, fileReader NestedURLConfFileReader, filePath string, depth int) []childRoute {
	const maxDepth = 2
	var routes []childRoute

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
		routes = append(routes, childRoute{
			pattern: pattern,
			handler: resolveFBVHandler(handler),
		})
	}

	// DRF router.register() calls — handles routers.py style child files.
	// e.g. `router.register(r"users", UserViewSet)` → yields "users".
	// No FBV handler to resolve for CBV ViewSet registrations.
	for _, m := range djangoRouterRegisterRe.FindAllStringSubmatch(src, -1) {
		routes = append(routes, childRoute{pattern: m[1]})
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
				routes = append(routes, childRoute{
					pattern: joinDjangoRoutePaths(subPrefix, sr.pattern),
					handler: sr.handler,
				})
			}
		}
	}

	return routes
}

// resolveFBVHandler converts a Django path() view argument to a bare function
// name suitable for use as a source_handler reference. It handles:
//
//   - "views.user_list"         → "user_list"   (module-qualified FBV)
//   - "user_list"               → "user_list"   (bare FBV import)
//   - "UserView.as_view()"      → ""            (CBV — no FBV name)
//   - "views.UserView.as_view()"→ ""            (module-qualified CBV)
//
// Returns "" for CBV as_view() calls (they are handled by the existing
// django_drf_actions.go CBV pass which sets its own source_handler).
func resolveFBVHandler(handler string) string {
	// Strip trailing whitespace.
	handler = strings.TrimSpace(handler)
	if handler == "" {
		return ""
	}
	// CBV as_view() calls — skip; the CBV pass handles these separately.
	if strings.Contains(handler, "as_view") {
		return ""
	}
	// Strip module prefix: "views.user_list" → "user_list".
	// Only strip ONE level (the module alias). Names with two dots
	// (e.g. "app.views.fn") are uncommon and we take the last segment.
	if idx := strings.LastIndex(handler, "."); idx >= 0 {
		handler = handler[idx+1:]
	}
	// Must be a valid Python identifier — reject anything that still
	// contains non-identifier characters.
	if !isPythonIdentifier(handler) {
		return ""
	}
	return handler
}

// isPythonIdentifier reports whether s is a valid Python bare identifier
// (letters, digits, underscores; must not start with a digit).
func isPythonIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			// always valid
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// modulePathToFilePath converts a Python module path (e.g. "api.urls" or
// "apps.users.urls") to a repo-relative file path (e.g. "api/urls.py").
// Returns "" when the module path is not convertible (e.g. it references a
// third-party package without a recognisable path component).
//
// We use path.Join (from "path", not "path/filepath") so the result always
// uses forward slashes. Repo-relative paths are always forward-slash-separated
// in the archigraph entity store and file-reader callbacks — using
// filepath.Join here would produce backslash paths on Windows, breaking
// fileReader lookups and causing all nested-include route composition to
// silently produce zero entities on that platform (P6 of #2196).
func modulePathToFilePath(modulePath string) string {
	if modulePath == "" {
		return ""
	}
	// Replace dots with forward slashes and append .py.
	// "api.urls"        → "api/urls.py"
	// "apps.users.urls" → "apps/users/urls.py"
	parts := strings.Split(modulePath, ".")
	return path.Join(parts...) + ".py"
}

// modulePathToFilePath_relToParent tries to resolve a Python module path
// relative to the parent file's directory. This handles the common pattern
// where include("urls") is used within the same app directory.
//
// Like modulePathToFilePath, we use path.Join (forward slashes) so the
// resulting repo-relative path is consistent with the rest of the system on
// all platforms.
func modulePathToFilePath_relToParent(modulePath, parentPath string) string {
	if modulePath == "" || parentPath == "" {
		return ""
	}
	// filepath.Dir is safe here: it normalises the parent path for the
	// platform and we immediately convert back to forward slashes via
	// path.Join for the returned value.
	parentDir := filepath.ToSlash(filepath.Dir(parentPath))
	if parentDir == "." {
		return ""
	}
	parts := strings.Split(modulePath, ".")
	return path.Join(append([]string{parentDir}, parts...)...) + ".py"
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
