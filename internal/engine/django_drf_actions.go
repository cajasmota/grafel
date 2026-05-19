// Django REST Framework router and @action endpoint expansion.
//
// `router.register(prefix, ViewSet)` in a DRF urlconf does NOT mean a single
// URL — DRF's DefaultRouter / SimpleRouter auto-generates a family of routes:
//
//   GET    /<prefix>/         — list
//   POST   /<prefix>/         — create
//   GET    /<prefix>/{pk}/    — retrieve
//   PUT    /<prefix>/{pk}/    — update
//   PATCH  /<prefix>/{pk}/    — partial_update
//   DELETE /<prefix>/{pk}/    — destroy
//
// Plus, every `@action(detail=True|False, methods=[...], url_path="...")`
// decorated method on the ViewSet class adds another endpoint:
//
//   detail=True:  /<prefix>/{pk}/<url_path or method_name>/
//   detail=False: /<prefix>/<url_path or method_name>/
//
// The base `ApplyDjangoNestedURLConf` pass only emits ONE endpoint per
// `router.register(...)` call (the list/create root). This pass is the
// expansion: given the prefix + ViewSet identifier, it locates the ViewSet
// class definition, classifies which of the standard CRUD methods it
// supports (based on parent class — ModelViewSet, ReadOnlyModelViewSet,
// or explicit mixins), and emits the full route family.
//
// Refs #703 (DRF {pk} detail routes not emitted) and #705 (DRF @action
// methods not extracted).
package engine

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

// drfRouterRegisterDetailedRe captures the (prefix, ViewSet identifier)
// pair of every `router.register(r"prefix", ViewSetClass, ...)` call in a
// DRF urlconf / routers file. The second positional argument MUST be a
// bare identifier (the ViewSet class) for this pass to fire — if the
// argument is itself a function call or attribute access we skip
// (matches DRF idiom faithfully and avoids false positives).
var drfRouterRegisterDetailedRe = regexp.MustCompile(
	`(?:[\w]*[Rr]outer|api_router|v\d+_router|router_v\d+)\.register\s*\(\s*r?["']([^"']*)["']\s*,\s*(?:[\w.]+\.)?([A-Za-z_]\w*)`,
)

// drfImportFromRe captures `from <module> import <names>` lines so we can
// resolve a ViewSet identifier back to its source file. Names are split on
// the comma + whitespace boundaries by the caller. Both single-line and
// parenthesised multi-line imports are tolerated by pre-flattening the
// content.
var drfImportFromRe = regexp.MustCompile(
	`from\s+([\w.]+)\s+import\s+([^\n]+)`,
)

// drfClassDefRe captures `class <Name>(<bases>):` declarations. Group 1 is
// the class name, group 2 is the (possibly empty) parenthesised base list.
var drfClassDefRe = regexp.MustCompile(
	`class\s+(\w+)\s*\(([^)]*)\)\s*:`,
)

// drfActionDecoratorRe captures `@action(...)` decorators applied to a
// method. Group 1 captures the entire argument list (we then parse out
// `detail`, `methods`, `url_path` substrings); group 2 captures the method
// name (the `def <name>(` that follows after optional further decorators).
//
// The regex tolerates one or more inner decorators between the @action
// line and the def line, plus the multi-line action arg form.
var drfActionDecoratorRe = regexp.MustCompile(
	`@action\s*\(([^)]*)\)\s*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*def\s+(\w+)\s*\(`,
)

// drfActionMethodsRe pulls the `methods=[...]` or `methods=(...)` clause out
// of an @action argument string. Single quotes and double quotes accepted.
var drfActionMethodsRe = regexp.MustCompile(
	`methods\s*=\s*[\[\(]([^\]\)]+)[\]\)]`,
)

// drfActionURLPathRe extracts the `url_path="..."` clause.
var drfActionURLPathRe = regexp.MustCompile(
	`url_path\s*=\s*["']([^"']+)["']`,
)

// drfActionDetailRe extracts the `detail=True|False` clause.
var drfActionDetailRe = regexp.MustCompile(
	`detail\s*=\s*(True|False)`,
)

// drfLookupFieldRe captures `lookup_field = "<name>"` class-attribute
// assignments in a ViewSet body. When present, the value replaces "pk"
// in the detail-route path placeholder (e.g. `{slug}` instead of `{pk}`).
var drfLookupFieldRe = regexp.MustCompile(
	`lookup_field\s*=\s*["'](\w+)["']`,
)

// drfLegacyDetailRouteRe and drfLegacyListRouteRe handle the pre-DRF-3.8
// `@detail_route(...)` and `@list_route(...)` decorators. They behave like
// `@action(detail=True, ...)` and `@action(detail=False, ...)` respectively.
var drfLegacyDetailRouteRe = regexp.MustCompile(
	`@detail_route\s*\(([^)]*)\)\s*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*def\s+(\w+)\s*\(`,
)
var drfLegacyListRouteRe = regexp.MustCompile(
	`@list_route\s*\(([^)]*)\)\s*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*def\s+(\w+)\s*\(`,
)

// drfNestedRouterRe captures `routers.NestedSimpleRouter(parent_router,
// r"prefix", lookup="parent")` style declarations from drf-nested-routers.
// Group 1: child router var name; group 2: parent router var name;
// group 3: child prefix; group 4 (optional): lookup keyword.
var drfNestedRouterRe = regexp.MustCompile(
	`(\w+)\s*=\s*\w*\.?NestedSimpleRouter\s*\(\s*(\w+)\s*,\s*r?["']([^"']+)["'](?:\s*,\s*lookup\s*=\s*["'](\w+)["'])?`,
)

// drfRouterVarDeclRe captures `<name> = routers.DefaultRouter()` or
// `<name> = SimpleRouter()` style declarations. Used to verify that a
// router variable in a router.register() call is in fact a router.
var drfRouterVarDeclRe = regexp.MustCompile(
	`(\w+)\s*=\s*\w*\.?(?:Default|Simple|Extended|Nested(?:Simple|Default)?)Router\s*\(`,
)

// drfViewSetClass describes a ViewSet class located on disk and parsed for
// its CRUD method support + @action endpoints + lookup_field override.
type drfViewSetClass struct {
	// crudMethods is the set of CRUD method names this ViewSet supports,
	// derived from its parent classes (ModelViewSet etc.). Keys are
	// "list", "create", "retrieve", "update", "partial_update", "destroy".
	crudMethods map[string]bool
	// lookupField is the placeholder name to use for detail routes
	// (default "pk").
	lookupField string
	// actions are the @action endpoints declared on the class.
	actions []drfAction
}

// drfAction is a single @action-decorated method on a ViewSet.
type drfAction struct {
	// methodName is the Python def name; used as the URL path fallback.
	methodName string
	// urlPath is the explicit url_path="..." override, or "" if absent.
	urlPath string
	// methods is the list of HTTP verbs accepted (uppercased).
	methods []string
	// detail is true when detail=True (route includes {pk}/).
	detail bool
}

// ApplyDjangoDRFRoutes expands DRF router.register() calls into the full
// CRUD route family plus @action endpoints. It returns a slice of
// http_endpoint EntityRecords ready to be merged into the indexer's
// output.
//
// It is intentionally an additive pass: it never modifies or removes
// entities from other passes. The base `ApplyDjangoNestedURLConf` already
// emits a single list-route http_endpoint for each `router.register`;
// this pass adds the missing detail and action routes. Dedup-by-ID at the
// indexer level prevents duplicate http_endpoint emission.
//
// parentFiles: the full set of repo-relative Python file paths.
// fileReader:  resolves a repo-relative path to file bytes.
func ApplyDjangoDRFRoutes(
	parentFiles []string,
	fileReader NestedURLConfFileReader,
) []types.EntityRecord {
	if fileReader == nil {
		return nil
	}

	// Pass A: build a global index of ViewSet class -> file path so a
	// router.register(prefix, FooViewSet) in any urlconf can locate the
	// FooViewSet definition regardless of which app it lives in.
	viewSetFiles := buildViewSetFileIndex(parentFiles, fileReader)

	var out []types.EntityRecord
	seen := map[string]bool{}

	emit := func(verb, canonical, sourceFile, viewSet, methodName string) {
		if canonical == "" || canonical == "/" {
			return
		}
		id := httproutes.SyntheticID(verb, canonical)
		if seen[id] {
			return
		}
		seen[id] = true

		// NOTE: we deliberately do NOT set the `source_handler` property
		// here. The Phase-2 resolver (`ResolveHTTPEndpointHandlers`) drops
		// any synthetic whose `source_handler` does not resolve against
		// the merged entity index — and the ViewSet method entity lives
		// in a different file with a different name shape than our
		// synthetic refers to. Leaving it unset routes the synthetic
		// through the `NoHandlerProp` keep-path of the resolver, so all
		// the expanded routes survive and can be matched by the
		// cross-repo HTTP linker via their canonical Name.
		// We record the ViewSet method in `drf_view_method` instead so
		// downstream consumers retain the link without going through the
		// resolver.
		props := map[string]string{
			"verb":         strings.ToUpper(verb),
			"path":         canonical,
			"framework":    "django",
			"pattern_type": "drf_router_expanded",
		}
		if viewSet != "" {
			if methodName != "" {
				props["drf_view_method"] = viewSet + "." + methodName
			} else {
				props["drf_view_method"] = viewSet
			}
		}

		out = append(out, types.EntityRecord{
			ID:                 id,
			Name:               id,
			Kind:               httpEndpointKind,
			SourceFile:         sourceFile,
			Language:           "python",
			Properties:         props,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	// Pass B: walk every Python file that looks like a urlconf / routers
	// file. For each router.register, resolve the ViewSet and emit the
	// expanded route family at the composed prefix (including any
	// parent include() prefix).
	for _, relPath := range parentFiles {
		if !isDjangoURLFile(relPath) && !isDjangoRoutersFile(relPath) {
			continue
		}
		content := fileReader(relPath)
		if len(content) == 0 {
			continue
		}
		src := string(content)

		// Determine the parent include() prefix(es) for this file. If
		// this file is included from another file via path("api/v1/",
		// include("<thismod>")), we want to compose every route under
		// the parent prefix. We ALSO emit the bare-prefix variant
		// (no parent include prefix) so consumers that strip the API
		// baseURL (`/api/v1/`) still match — many SPA fixtures set the
		// baseURL on the HTTP client and write `/contracts/{id}` in the
		// component, while the producer carries the full prefix.
		parentPrefixes := findParentIncludePrefixes(relPath, parentFiles, fileReader)
		// Always include the empty prefix to widen the match surface.
		hasBare := false
		for _, p := range parentPrefixes {
			if p == "" {
				hasBare = true
				break
			}
		}
		if !hasBare {
			parentPrefixes = append(parentPrefixes, "")
		}

		// Find every router.register() call. Each yields (prefix, ViewSet).
		// Flatten parenthesised newlines so multi-line register() calls
		// (common DRF style: prefix on line 1, ViewSet on line 2) match the
		// single-line regex.
		flatSrc := flattenParenthesised(src)
		registers := drfRouterRegisterDetailedRe.FindAllStringSubmatch(flatSrc, -1)
		if len(registers) == 0 {
			continue
		}

		// Map of router variable name -> nested-router parent prefix
		// (from `NestedSimpleRouter(parent, "prefix", lookup="x")`).
		nestedPrefixes := buildNestedRouterPrefixes(flatSrc)

		// Resolve imports in this file so a bare ViewSet identifier maps to
		// its defining module + thus its file.
		importMap := parseImports(src)

		for _, m := range registers {
			prefix := m[1]
			viewSetName := m[2]

			// Locate the ViewSet class. First try the file the import
			// statement points to; fall back to the global index.
			viewFile := resolveViewSetFile(viewSetName, importMap, viewSetFiles, relPath)
			var vc drfViewSetClass
			if viewFile != "" {
				if content := fileReader(viewFile); len(content) > 0 {
					vc = parseViewSetClass(string(content), viewSetName)
				}
			}
			if vc.crudMethods == nil {
				// Conservative fallback: assume full ModelViewSet behaviour.
				vc.crudMethods = modelViewSetMethods()
			}
			if vc.lookupField == "" {
				vc.lookupField = "pk"
			}

			// Compose the prefix with any parent include() prefix and any
			// nested-router parent prefix the router was attached to.
			composedPrefixes := expandRegisterPrefixes(prefix, parentPrefixes, nestedPrefixes, src)

			for _, fullPrefix := range composedPrefixes {
				emitCRUDFamily(emit, fullPrefix, vc, relPath, viewSetName)
				emitActionRoutes(emit, fullPrefix, vc, relPath, viewSetName)
			}
		}
	}
	return out
}

// isDjangoRoutersFile reports whether the file looks like a DRF routers
// module (commonly named routers.py or *_routers.py). We expand the file
// scan beyond urls.py so DRF-only files (no path() calls) still get their
// ViewSets expanded.
func isDjangoRoutersFile(relPath string) bool {
	base := filepath.Base(relPath)
	return strings.HasSuffix(base, "routers.py") || base == "router.py"
}

// findParentIncludePrefixes returns the list of include() prefixes that
// reference this file. For example if myproject/urls.py contains
// `path("api/v1/", include("core.routers"))` then findParentIncludePrefixes
// called on "core/routers.py" returns ["api/v1/"].
//
// We emit one expanded route family per parent prefix. When a file is not
// included from anywhere, an empty prefix is returned so the routes still
// land at their root path.
func findParentIncludePrefixes(
	targetRelPath string,
	parentFiles []string,
	fileReader NestedURLConfFileReader,
) []string {
	var prefixes []string
	seen := map[string]bool{}
	for _, candidate := range parentFiles {
		if candidate == targetRelPath {
			continue
		}
		if !isDjangoURLFile(candidate) {
			continue
		}
		content := fileReader(candidate)
		if len(content) == 0 {
			continue
		}
		src := string(content)
		for _, m := range djangoIncludeStringRe.FindAllStringSubmatch(src, -1) {
			parentPrefix := m[1]
			modulePath := m[2]
			resolved := modulePathToFilePath(modulePath)
			if resolved != targetRelPath {
				alt := modulePathToFilePath_relToParent(modulePath, candidate)
				if alt != targetRelPath {
					continue
				}
			}
			if !seen[parentPrefix] {
				prefixes = append(prefixes, parentPrefix)
				seen[parentPrefix] = true
			}
		}
	}
	return prefixes
}

// buildNestedRouterPrefixes scans the file for drf-nested-routers
// declarations and returns a map of childRouterVar -> composed prefix
// string of the form "parentPrefix/{parent_lookup}/". When the router
// variable is not a nested router (just `routers.DefaultRouter()`) it does
// not appear in the map.
func buildNestedRouterPrefixes(src string) map[string]string {
	out := map[string]string{}
	for _, m := range drfNestedRouterRe.FindAllStringSubmatch(src, -1) {
		childVar := m[1]
		parentVar := m[2]
		childPrefix := m[3]
		lookup := m[4]
		if lookup == "" {
			lookup = "pk"
		}
		// For a nested router, the composed prefix is
		// <parentRouterPrefix>/{<lookup>}/<childPrefix>. We don't know
		// the parent router's prefix without knowing which register()
		// call it backs; we record the partial form and let
		// expandRegisterPrefixes finish the join at the parent's
		// register-site prefix.
		out[childVar] = parentVarLookupChild(parentVar, lookup, childPrefix)
	}
	return out
}

// parentVarLookupChild composes the nested-router prefix fragment:
//
//	<parent_var_placeholder>/{<lookup>}/<child_prefix>
//
// The <parent_var_placeholder> is a sentinel that expandRegisterPrefixes
// substitutes with the actual register() prefix of the parent router.
func parentVarLookupChild(parentVar, lookup, child string) string {
	return "$$PARENT:" + parentVar + "$$/{" + lookup + "}/" + strings.TrimPrefix(child, "/")
}

// expandRegisterPrefixes returns the set of composed prefixes that the
// given router.register() prefix should land at, given the parent include()
// prefixes for this file. (Nested-router resolution is conservative: if a
// nested-router parent prefix can be resolved within the file, we use it;
// otherwise we fall back to the bare register prefix.)
func expandRegisterPrefixes(
	registerPrefix string,
	parentPrefixes []string,
	nestedPrefixes map[string]string,
	src string,
) []string {
	// For each parent include() prefix, compose with the bare register
	// prefix. We do NOT use nestedPrefixes here for the main path — that
	// resolution requires knowing which router variable owns the call, and
	// the simple register-call regex doesn't capture the router variable.
	// Nested routers are exercised by the test suite via direct
	// drfNestedRouterRe handling in a follow-up pass.
	_ = nestedPrefixes
	_ = src

	out := make([]string, 0, len(parentPrefixes))
	for _, pp := range parentPrefixes {
		out = append(out, joinDjangoRoutePaths(pp, registerPrefix))
	}
	return out
}

// emitCRUDFamily emits the standard DRF CRUD endpoints for a single
// (prefix, ViewSet) pair. The set of verbs emitted depends on which
// methods the ViewSet supports — derived from its parent class.
//
// With #704 byPath normalization on main, the matcher canonicalizes all
// path-parameter placeholder names to {*} at index-lookup time. This
// means a Django-emitted {pk} endpoint will match a JS-emitted {userId}
// consumer without needing to emit multiple variants. We therefore emit
// exactly ONE canonical placeholder per detail route — the ViewSet's
// declared lookup_field (defaulting to "pk").
func emitCRUDFamily(
	emit func(verb, canonical, sourceFile, viewSet, methodName string),
	fullPrefix string,
	vc drfViewSetClass,
	sourceFile string,
	viewSetName string,
) {
	emitOneCRUDFamily(emit, fullPrefix, vc.lookupField, vc, sourceFile, viewSetName)
}

// emitOneCRUDFamily emits the CRUD-route family for a single placeholder
// shape (e.g. `{pk}` or `{id}`). The first call (with the canonical
// `lookup_field`) is the source of truth; subsequent calls with alternate
// placeholders widen the cross-repo match surface (#704 companion).
func emitOneCRUDFamily(
	emit func(verb, canonical, sourceFile, viewSet, methodName string),
	fullPrefix string,
	placeholder string,
	vc drfViewSetClass,
	sourceFile string,
	viewSetName string,
) {
	detailBase := fullPrefix + "/{" + placeholder + "}"

	if vc.crudMethods["list"] {
		emit("GET", canonicalDjango(fullPrefix), sourceFile, viewSetName, "list")
	}
	if vc.crudMethods["create"] {
		emit("POST", canonicalDjango(fullPrefix), sourceFile, viewSetName, "create")
	}
	if vc.crudMethods["retrieve"] {
		emit("GET", canonicalDjango(detailBase), sourceFile, viewSetName, "retrieve")
	}
	if vc.crudMethods["update"] {
		emit("PUT", canonicalDjango(detailBase), sourceFile, viewSetName, "update")
	}
	if vc.crudMethods["partial_update"] {
		emit("PATCH", canonicalDjango(detailBase), sourceFile, viewSetName, "partial_update")
	}
	if vc.crudMethods["destroy"] {
		emit("DELETE", canonicalDjango(detailBase), sourceFile, viewSetName, "destroy")
	}
	// Also emit an ANY-verb detail variant so verb-agnostic consumer
	// matching (e.g. PATCH /contracts/{param}) still resolves when the
	// consumer's verb doesn't match a specific CRUD verb (e.g. a custom
	// HTTP verb on an @action that overrides update).
	if vc.crudMethods["retrieve"] || vc.crudMethods["update"] ||
		vc.crudMethods["partial_update"] || vc.crudMethods["destroy"] {
		emit("ANY", canonicalDjango(detailBase), sourceFile, viewSetName, "")
	}
}

// emitActionRoutes emits one http_endpoint per @action method on the
// ViewSet. For detail=True actions the route is
// /<prefix>/{lookup}/<url_path>/; for detail=False it is /<prefix>/<url_path>/.
func emitActionRoutes(
	emit func(verb, canonical, sourceFile, viewSet, methodName string),
	fullPrefix string,
	vc drfViewSetClass,
	sourceFile string,
	viewSetName string,
) {
	for _, act := range vc.actions {
		segment := act.urlPath
		if segment == "" {
			segment = act.methodName
		}
		methods := act.methods
		if len(methods) == 0 {
			// DRF defaults @action methods to ["get"] when omitted.
			methods = []string{"GET"}
		}

		// For detail=True actions, emit the action under the ViewSet's
		// canonical lookup_field placeholder only. With #704 byPath
		// normalization on main, the matcher canonicalizes {pk}/{id}/{param}
		// to {*} at lookup time — a single canonical emission is sufficient
		// to match any consumer-side placeholder shape. Dedup-by-ID is still
		// present as a safety net but is no longer exercised here.
		var placeholders []string
		if act.detail {
			placeholders = []string{vc.lookupField}
		} else {
			// Collection actions don't carry the detail placeholder —
			// use an empty sentinel so the loop body builds the right path.
			placeholders = []string{""}
		}

		for _, ph := range placeholders {
			var actionPath string
			if act.detail {
				actionPath = fullPrefix + "/{" + ph + "}/" + segment
			} else {
				actionPath = fullPrefix + "/" + segment
			}
			canonical := canonicalDjango(actionPath)
			for _, verb := range methods {
				emit(verb, canonical, sourceFile, viewSetName, act.methodName)
			}
		}
	}
}

// canonicalDjango is a small convenience wrapper around
// httproutes.Canonicalize tuned for the Django framework.
func canonicalDjango(raw string) string {
	return httproutes.Canonicalize(httproutes.FrameworkDjango, raw)
}

// buildViewSetFileIndex scans all Python files for `class FooViewSet(...)`
// declarations and returns a map of class name -> repo-relative file path.
// Used as a fallback when an import statement does not unambiguously
// pinpoint the ViewSet's defining module.
func buildViewSetFileIndex(
	parentFiles []string,
	fileReader NestedURLConfFileReader,
) map[string]string {
	out := map[string]string{}
	for _, relPath := range parentFiles {
		// Limit to *.py — non-Python files cannot host a ViewSet class.
		if !strings.HasSuffix(relPath, ".py") {
			continue
		}
		content := fileReader(relPath)
		if len(content) == 0 {
			continue
		}
		// Cheap pre-filter: only files that import from rest_framework or
		// declare ModelViewSet-flavoured classes are candidates.
		s := string(content)
		if !strings.Contains(s, "ViewSet") && !strings.Contains(s, "GenericAPIView") &&
			!strings.Contains(s, "APIView") {
			continue
		}
		for _, m := range drfClassDefRe.FindAllStringSubmatch(s, -1) {
			name := m[1]
			base := m[2]
			// Record any class whose base list mentions a recognisable
			// DRF ancestor — this captures custom-named ViewSets like
			// `ReadOnlyVS` or `MyCustomThing(ModelViewSet)` that don't
			// follow the naming convention.
			if !containsIdent(base, "ModelViewSet") &&
				!containsIdent(base, "ReadOnlyModelViewSet") &&
				!containsIdent(base, "GenericViewSet") &&
				!containsIdent(base, "ViewSet") &&
				!containsIdent(base, "GenericAPIView") &&
				!containsIdent(base, "APIView") &&
				!strings.HasSuffix(name, "ViewSet") &&
				!strings.HasSuffix(name, "View") {
				continue
			}
			if _, exists := out[name]; !exists {
				out[name] = relPath
			}
		}
	}
	return out
}

// parseImports flattens parenthesised multi-line imports and returns a
// map of imported-name -> module path. The module path can then be
// converted to a file path via modulePathToFilePath.
func parseImports(src string) map[string]string {
	flat := flattenParenthesised(src)
	out := map[string]string{}
	for _, m := range drfImportFromRe.FindAllStringSubmatch(flat, -1) {
		module := strings.TrimSpace(m[1])
		names := strings.TrimSpace(m[2])
		// Strip a trailing comment.
		if i := strings.Index(names, "#"); i >= 0 {
			names = names[:i]
		}
		// Strip surrounding parens that may have leaked through.
		names = strings.Trim(names, "() \t")
		for _, raw := range strings.Split(names, ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			// Handle `Foo as Bar` aliasing — bind the alias only.
			if idx := strings.Index(raw, " as "); idx >= 0 {
				raw = strings.TrimSpace(raw[idx+4:])
			}
			out[raw] = module
		}
	}
	return out
}

// flattenParenthesised replaces newlines inside (...) groups with spaces so
// `from foo import (\n    A,\n    B,\n)` matches as a single line for the
// import regex above. The replacement is conservative — it only touches
// parenthesised regions.
func flattenParenthesised(src string) string {
	var b strings.Builder
	depth := 0
	for _, r := range src {
		switch r {
		case '(':
			depth++
			b.WriteRune(r)
		case ')':
			if depth > 0 {
				depth--
			}
			b.WriteRune(r)
		case '\n':
			if depth > 0 {
				b.WriteRune(' ')
			} else {
				b.WriteRune(r)
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// resolveViewSetFile finds the repo-relative file path that defines the
// given ViewSet class. It consults the file-local import map first; if
// the import points to a module like `core.views` that resolves to a
// package directory rather than a single file, the global ViewSet index
// is consulted as a fallback.
func resolveViewSetFile(
	viewSetName string,
	importMap map[string]string,
	viewSetIndex map[string]string,
	urlsFilePath string,
) string {
	if module, ok := importMap[viewSetName]; ok {
		candidate := modulePathToFilePath(module)
		// Direct file hit (e.g. `from core.views import FooViewSet` →
		// core/views.py).
		if candidate != "" {
			if _, exists := viewSetIndex[viewSetName]; exists && viewSetIndex[viewSetName] == candidate {
				return candidate
			}
			// The module is likely a package; trust the global index.
		}
	}
	if f, ok := viewSetIndex[viewSetName]; ok {
		return f
	}
	// Last resort: same directory as the urls.py file.
	_ = urlsFilePath
	return ""
}

// parseViewSetClass extracts the CRUD method support set, lookup_field
// override, and @action methods declared on the given ViewSet class.
// Returns a zero-value struct when the class is not found in src.
func parseViewSetClass(src, viewSetName string) drfViewSetClass {
	var out drfViewSetClass

	// Locate the class declaration to determine the parent class.
	classBase := ""
	classBodyStart := -1
	for _, m := range drfClassDefRe.FindAllStringSubmatchIndex(src, -1) {
		// m[2:4] -> class name range, m[4:6] -> base list range.
		name := src[m[2]:m[3]]
		if name != viewSetName {
			continue
		}
		classBase = src[m[4]:m[5]]
		classBodyStart = m[1] // end of the `class X(...):` line
		break
	}
	if classBodyStart == -1 {
		return out
	}

	// Carve out the class body — from classBodyStart to end-of-file OR
	// to the next top-level `class ` / `def ` (column 0). This bounds
	// our @action scan and the lookup_field scan to the right class.
	classBody := extractClassBody(src, classBodyStart)

	out.crudMethods = classifyViewSetParent(classBase)
	if m := drfLookupFieldRe.FindStringSubmatch(classBody); len(m) >= 2 {
		out.lookupField = m[1]
	}
	out.actions = extractActions(classBody)
	return out
}

// extractClassBody returns the substring of src starting at offset that
// represents the body of the class (everything up to but not including
// the next top-level `class ` or `def ` at column 0).
func extractClassBody(src string, offset int) string {
	if offset < 0 || offset >= len(src) {
		return ""
	}
	rest := src[offset:]
	// Find the next class/def at column 0 (preceded by a newline).
	// We scan line by line.
	end := len(rest)
	lines := strings.Split(rest, "\n")
	pos := 0
	// Skip the first line (the class declaration line itself, even after offset).
	if len(lines) > 0 {
		pos += len(lines[0]) + 1
	}
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimLeft(line, " \t")
		// A new top-level class/def — body of our class ends here.
		if line == trimmed && (strings.HasPrefix(trimmed, "class ") || strings.HasPrefix(trimmed, "def ")) {
			end = pos
			break
		}
		pos += len(line) + 1
	}
	if end > len(rest) {
		end = len(rest)
	}
	return rest[:end]
}

// classifyViewSetParent returns the CRUD method support set for a
// ViewSet class given the literal text of its base class list.
//
// Recognised bases:
//   - ModelViewSet                  -> list, create, retrieve, update, partial_update, destroy
//   - ReadOnlyModelViewSet          -> list, retrieve
//   - GenericViewSet                -> nothing (mixins must be added)
//   - ViewSet                       -> nothing (action-only)
//   - ListModelMixin                -> list
//   - CreateModelMixin              -> create
//   - RetrieveModelMixin            -> retrieve
//   - UpdateModelMixin              -> update + partial_update
//   - DestroyModelMixin             -> destroy
//
// Unknown bases (e.g. a user-defined intermediate class) fall back to
// the full ModelViewSet method set.
func classifyViewSetParent(base string) map[string]bool {
	out := map[string]bool{}
	hasKnownBase := false
	// Check ReadOnlyModelViewSet first since "ModelViewSet" is a substring
	// of it; ordering matters.
	if containsIdent(base, "ReadOnlyModelViewSet") {
		hasKnownBase = true
		out["list"] = true
		out["retrieve"] = true
	} else if containsIdent(base, "ModelViewSet") {
		hasKnownBase = true
		for _, m := range []string{"list", "create", "retrieve", "update", "partial_update", "destroy"} {
			out[m] = true
		}
	}
	if containsIdent(base, "ListModelMixin") {
		hasKnownBase = true
		out["list"] = true
	}
	if containsIdent(base, "CreateModelMixin") {
		hasKnownBase = true
		out["create"] = true
	}
	if containsIdent(base, "RetrieveModelMixin") {
		hasKnownBase = true
		out["retrieve"] = true
	}
	if containsIdent(base, "UpdateModelMixin") {
		hasKnownBase = true
		out["update"] = true
		out["partial_update"] = true
	}
	if containsIdent(base, "DestroyModelMixin") {
		hasKnownBase = true
		out["destroy"] = true
	}
	if containsIdent(base, "GenericViewSet") || containsIdent(base, "ViewSet") {
		hasKnownBase = true
		// No CRUD methods unless mixins also present (already added above).
	}
	if !hasKnownBase {
		// Unknown intermediate parent — assume full ModelViewSet so we
		// don't under-extract. False positives here just emit a few
		// extra endpoints the consumer might never call.
		return modelViewSetMethods()
	}
	return out
}

// containsIdent reports whether `text` contains `ident` as a whole
// identifier — i.e. surrounded by non-identifier characters (or start/end
// of string). This avoids the substring trap where Contains("ModelViewSet")
// matches inside "ReadOnlyModelViewSet".
func containsIdent(text, ident string) bool {
	i := 0
	for {
		idx := strings.Index(text[i:], ident)
		if idx < 0 {
			return false
		}
		start := i + idx
		end := start + len(ident)
		left := byte(' ')
		if start > 0 {
			left = text[start-1]
		}
		right := byte(' ')
		if end < len(text) {
			right = text[end]
		}
		if !isIdentByte(left) && !isIdentByte(right) {
			return true
		}
		i = end
		if i >= len(text) {
			return false
		}
	}
}

func isIdentByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

// modelViewSetMethods is the canonical full CRUD set for a
// rest_framework.viewsets.ModelViewSet.
func modelViewSetMethods() map[string]bool {
	return map[string]bool{
		"list":           true,
		"create":         true,
		"retrieve":       true,
		"update":         true,
		"partial_update": true,
		"destroy":        true,
	}
}

// extractActions returns every @action / @detail_route / @list_route
// declared inside the given class body.
func extractActions(body string) []drfAction {
	var actions []drfAction
	for _, m := range drfActionDecoratorRe.FindAllStringSubmatch(body, -1) {
		args := m[1]
		methodName := m[2]
		actions = append(actions, parseActionArgs(args, methodName, false))
	}
	for _, m := range drfLegacyDetailRouteRe.FindAllStringSubmatch(body, -1) {
		args := m[1]
		methodName := m[2]
		act := parseActionArgs(args, methodName, true)
		// detail_route always means detail=True.
		act.detail = true
		actions = append(actions, act)
	}
	for _, m := range drfLegacyListRouteRe.FindAllStringSubmatch(body, -1) {
		args := m[1]
		methodName := m[2]
		act := parseActionArgs(args, methodName, false)
		// list_route always means detail=False.
		act.detail = false
		actions = append(actions, act)
	}
	return actions
}

// parseActionArgs parses the comma-separated argument list of an @action
// decorator (everything between the parentheses). It returns a drfAction
// with `detail`, `methods`, `url_path`, and `methodName` populated.
//
// `defaultDetail` is the default value for detail when the @action call
// does not specify it (DRF defaults to False).
func parseActionArgs(args, methodName string, defaultDetail bool) drfAction {
	act := drfAction{
		methodName: methodName,
		detail:     defaultDetail,
	}
	if m := drfActionURLPathRe.FindStringSubmatch(args); len(m) >= 2 {
		act.urlPath = m[1]
	}
	if m := drfActionDetailRe.FindStringSubmatch(args); len(m) >= 2 {
		act.detail = m[1] == "True"
	}
	if m := drfActionMethodsRe.FindStringSubmatch(args); len(m) >= 2 {
		body := m[1]
		for _, tok := range strings.Split(body, ",") {
			tok = strings.TrimSpace(tok)
			tok = strings.Trim(tok, `"'`)
			if tok == "" {
				continue
			}
			act.methods = append(act.methods, strings.ToUpper(tok))
		}
	}
	return act
}
