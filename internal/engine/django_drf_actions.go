// Django REST Framework router and @action endpoint expansion.
//
// `router.register(prefix, ViewSet)` in a DRF urlconf does NOT mean a single
// URL — DRF's DefaultRouter / SimpleRouter auto-generates a family of routes:
//
//	GET    /<prefix>/         — list
//	POST   /<prefix>/         — create
//	GET    /<prefix>/{pk}/    — retrieve
//	PUT    /<prefix>/{pk}/    — update
//	PATCH  /<prefix>/{pk}/    — partial_update
//	DELETE /<prefix>/{pk}/    — destroy
//
// Plus, every `@action(detail=True|False, methods=[...], url_path="...")`
// decorated method on the ViewSet class adds another endpoint:
//
//	detail=True:  /<prefix>/{pk}/<url_path or verbatim-method-name>/
//	detail=False: /<prefix>/<url_path or verbatim-method-name>/
//
// When no `url_path=` is given DRF uses the method name VERBATIM as the segment
// (underscores preserved: `do_thing` → `do_thing`); an explicit `url_path` is
// used verbatim including embedded `(?P<name>regex)` capture groups, which the
// canonicaliser rewrites to `{name}` path params (#5230 / #5298).
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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/engine/httproutes"
	"github.com/cajasmota/grafel/internal/frameworks/baseknowledge"
	"github.com/cajasmota/grafel/internal/types"
)

// drfDbgEnabled gates the diagnostic stderr tracing for the DRF ghost-path
// investigation. Set GRAFEL_DRF_DBG=1 to enable.
var drfDbgEnabled = os.Getenv("GRAFEL_DRF_DBG") == "1"

// drfRouterRegisterDetailedRe captures the (routerVar, prefix, ViewSet identifier)
// triple of every `router.register(r"prefix", ViewSetClass, ...)` call in a
// DRF urlconf / routers file. The second positional argument MUST be a
// bare identifier (the ViewSet class) for this pass to fire — if the
// argument is itself a function call or attribute access we skip
// (matches DRF idiom faithfully and avoids false positives).
//
// Group 1: router variable name (e.g. "router", "api_router")
// Group 2: register prefix (e.g. "users")
// Group 3: ViewSet class name (e.g. "UserViewSet")
var drfRouterRegisterDetailedRe = regexp.MustCompile(
	`([\w]*[Rr]outer|api_router|v\d+_router|router_v\d+)\.register\s*\(\s*r?["']([^"']*)["']\s*,\s*(?:[\w.]+\.)?([A-Za-z_]\w*)`,
)

// drfRouterAttrIncludeRe matches `path("prefix", include(routerVar.urls))` calls
// in a Django urls.py. These represent LOCAL router mounts within the same file.
//
// Group 1: URL path prefix (e.g. "api/v1/")
// Group 2: router variable name (e.g. "router", "api_router")
//
// This is distinct from the string-form include() (handled by djangoIncludeStringRe)
// and is used to build a local routerVar→prefix map so ApplyDjangoDRFRoutes can
// compose the full route path even when the file has no parent string-include.
// Fixes #1124 (105 duplicate paths — same endpoint with AND without /api/v1/ prefix).
var drfRouterAttrIncludeRe = regexp.MustCompile(
	`(?:re_)?path\s*\(\s*r?["']([^"']*)["']\s*,\s*include\s*\(\s*([\w]+)\.urls\s*\)`,
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

// drfActionOpenRe locates the OPEN `@action(` token only. The argument list
// is then walked with a balanced-paren scanner (scanActionDecorator) so
// `@action(...)` calls whose arguments embed `)` characters — e.g.
// `url_path='(?P<id>[^/.]+)'` — are captured intact. Group 1 is unused;
// the scanner returns both the argument body and the def-name itself.
//
// Pre-#2669 this was a single `@action\s*\(([^)]*)\)…def\s+(\w+)\s*\(` regex
// whose `[^)]*` capture truncated at the first inner `)`, silently dropping
// every @action whose url_path embedded Python named-group regex syntax.
// The bug manifested as missing endpoint definitions for NoteViewSet's
// `notes_by_group_and_entity`, `notes_by_entity`, and `entity_type_catalogs`,
// which cascaded into 7 unresolved mobile orphans on upvate/core-mobile.
var drfActionOpenRe = regexp.MustCompile(`@action\s*\(`)

// drfActionPostArgsRe matches the post-arguments tail of an @action: zero or
// more decorator lines followed by `def NAME(`. Anchored at the index just
// after the closing `)` returned by scanActionDecorator.
var drfActionPostArgsRe = regexp.MustCompile(
	`^\s*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*def\s+(\w+)\s*\(`,
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

// drfGenericRegisterRe captures `<anyVar>.register(r"prefix", ViewSetClass, ...)`
// for an arbitrary variable name. Used to collect register() calls on nested
// router variables (e.g. `nested.register(r"companies", CompaniesViewSet)`)
// whose names don't match the name-pattern in drfRouterRegisterDetailedRe.
//
// Group 1: arbitrary variable name
// Group 2: register prefix
// Group 3: ViewSet class name
var drfGenericRegisterRe = regexp.MustCompile(
	`(\w+)\.register\s*\(\s*r?["']([^"']*)["']\s*,\s*(?:[\w.]+\.)?([A-Za-z_]\w*)`,
)

// drfLookupFieldRe captures `lookup_field = "<name>"` class-attribute
// assignments in a ViewSet body. When present, the value replaces "pk"
// in the detail-route path placeholder (e.g. `{slug}` instead of `{pk}`).
var drfLookupFieldRe = regexp.MustCompile(
	`lookup_field\s*=\s*["'](\w+)["']`,
)

// drfLegacyDetailRouteOpenRe / drfLegacyListRouteOpenRe locate the OPEN
// tokens of the pre-DRF-3.8 `@detail_route(` / `@list_route(` decorators.
// The argument body is walked by scanActionDecorator for the same
// nested-paren-safety reason as drfActionOpenRe (#2669).
var drfLegacyDetailRouteOpenRe = regexp.MustCompile(`@detail_route\s*\(`)
var drfLegacyListRouteOpenRe = regexp.MustCompile(`@list_route\s*\(`)

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

// drfExplicitMethodRe detects explicit CRUD method definitions in a ViewSet
// class body. Matches `def <method>(self` where <method> is one of the six
// standard DRF CRUD method names. Used to distinguish methods that are
// explicitly defined (handler entity already emitted by the Python extractor)
// from inherited methods (no entity exists — we must emit a synthetic one).
var drfExplicitMethodRe = regexp.MustCompile(
	`\bdef\s+(list|create|retrieve|update|partial_update|destroy)\s*\(\s*self`,
)

// drfHTTPMethodNamesRe captures the `http_method_names = ['get', 'post', ...]`
// class-attribute assignment that DRF View / ViewSet classes use to restrict
// the set of HTTP verbs accepted by the dispatcher. When present, ONLY the
// listed verbs are routed — every other CRUD method that maps to an
// unlisted verb is silently dropped at request time. Mirroring this gate
// here turns CRUD-method derived verbs that would never actually run into
// no-ops, which is essential for ViewSets like `RefreshViewSet(viewsets.ViewSet,
// TokenRefreshView)` with `http_method_names=['post']` + explicit `def create()`
// — without this gate the verb stays ANY (#1648).
var drfHTTPMethodNamesRe = regexp.MustCompile(
	`http_method_names\s*=\s*[\[\(]([^\]\)]+)[\]\)]`,
)

// crudMethodToVerb maps a DRF CRUD method name to the HTTP verb DRF wires it
// to. This is the canonical DRF dispatch table.
var crudMethodToVerb = map[string]string{
	"list":           "GET",
	"create":         "POST",
	"retrieve":       "GET",
	"update":         "PUT",
	"partial_update": "PATCH",
	"destroy":        "DELETE",
}

// drfViewSetClass describes a ViewSet class located on disk and parsed for
// its CRUD method support + @action endpoints + lookup_field override.
type drfViewSetClass struct {
	// crudMethods is the set of CRUD method names this ViewSet supports,
	// derived from its parent classes (ModelViewSet etc.). Keys are
	// "list", "create", "retrieve", "update", "partial_update", "destroy".
	crudMethods map[string]bool
	// explicitMethods is the subset of crudMethods that are directly defined
	// in the ViewSet class body (not merely inherited). When a method is in
	// crudMethods but NOT in explicitMethods, we emit a synthetic
	// SCOPE.Operation entity for it so source_handler can resolve.
	explicitMethods map[string]bool
	// lookupField is the placeholder name to use for detail routes
	// (default "pk").
	lookupField string
	// actions are the @action endpoints declared on the class.
	actions []drfAction
	// classDefLine is the 1-based line of `class <name>(...)` in the ViewSet's
	// source file. Used as the fallback StartLine for inherited CRUD methods
	// that have no `def` in this file (#2677).
	classDefLine int
	// methodLines maps a CRUD method name (e.g. "list") to the 1-based line of
	// its explicit `def <method>(` declaration. Empty entry means the method is
	// inherited from a mixin and has no body in this file (#2677).
	methodLines map[string]int
	// posture carries the ViewSet-level endpoint posture (pagination /
	// permission / authentication / throttle classes) declared as class
	// attributes. In DRF these apply to EVERY router-generated route the
	// ViewSet backs, so the DRF expansion pass stamps them onto each
	// synthesized http_endpoint (#3864). Zero value means "nothing declared"
	// (honest-partial — no fabricated posture).
	posture drfPosture
	// actionPermissions maps a DRF action name (a CRUD verb like "create" /
	// "list" or an @action method name) to the permission-class list that the
	// ViewSet's `def get_permissions(self):` override or a
	// `permission_classes_by_action = {...}` dict resolves for THAT action
	// (#3933). When present, the per-action list overrides the flat
	// permission_classes union for the matching route. A key of "" carries the
	// default branch (`return [IsAuthenticated()]` / dict `default`) that
	// applies to every action not otherwise listed. nil means no statically
	// resolvable per-action override exists — the route falls back to the flat
	// union posture (honest-partial).
	actionPermissions map[string][]string
	// actionPermissionPages maps a DRF action name (same keying as
	// actionPermissions, with "" as the default branch) to the fine-grained
	// page-key identities resolved for that action — the constant keys of a
	// `PERMISSION_PAGES["<KEY>"]` argument passed to a custom page/action guard
	// in the `get_permissions(self)` branch for that action (#3972). When
	// present, the route surfaces these as `auth_permissions`. nil / a missing
	// action means no resolvable page-key (honest-partial).
	actionPermissionPages map[string][]string
	// resolved reports whether the ViewSet class was actually located and
	// parsed on disk (parseViewSetClass found the `class <name>(...)`
	// declaration). When false, the caller fell back to modelViewSetMethods()
	// without ever reading a class body, so a CRUD verb's body presence is
	// unknown — its route provenance is `synthesized`, not `inherited` (#3831).
	resolved bool
	// serializerClass is the final dotted segment of the ViewSet's class-level
	// `serializer_class = X` attribute (e.g. "RoleSerializer"), or "" when none
	// is statically declared. It is one of the per-verb effective-contract
	// fields T5 (#3835) stamps onto every router-expanded route the ViewSet
	// backs. Honest-partial: a dynamic `get_serializer_class` override is not
	// resolved here, so the field stays "" rather than guessing.
	serializerClass string
	// classBases is the recognised base-class leaf names the ViewSet extends
	// (the `cbv_bases` parity subset — ModelViewSet, the CRUD mixins, ...), in
	// source order. Used by the effective-contract pass (#3835) to attribute an
	// inherited verb to its defining mixin via the baseknowledge pack when the
	// flat crudVerbDefiningMixin table does not cover a custom base.
	classBases []string
}

// crudVerbDefiningMixin maps each DRF CRUD method name to the rest_framework
// mixin class that implements it (the DRF dispatch table). Used to stamp
// `defining_class` on router-expanded routes whose verb is INHERITED from a
// mixin rather than overridden in the ViewSet body (#3831). This is the
// canonical DRF wiring: rest_framework.mixins.{List,Create,Retrieve,Update,
// Destroy}ModelMixin. Both `update` and `partial_update` are implemented by
// UpdateModelMixin (update() + partial_update() live in the same mixin).
var crudVerbDefiningMixin = map[string]string{
	"list":           "ListModelMixin",
	"create":         "CreateModelMixin",
	"retrieve":       "RetrieveModelMixin",
	"update":         "UpdateModelMixin",
	"partial_update": "UpdateModelMixin",
	"destroy":        "DestroyModelMixin",
}

// Route-provenance tag values stamped onto router-expanded http_endpoint
// synthetics (#3831). They answer the #278 disambiguation: is a verb on a
// generated route overridden in the ViewSet body, inherited from a mixin, an
// @action custom route, or a pure router default with no body anywhere?
const (
	// drfProvExplicit — the handler method is defined directly in the ViewSet
	// class body (overridden). defining_class is the ViewSet itself.
	drfProvExplicit = "explicit"
	// drfProvInherited — a CRUD verb the ViewSet exposes via a mixin it does
	// not override. defining_class is the implementing mixin (best-effort).
	drfProvInherited = "inherited"
	// drfProvAction — an @action / @detail_route / @list_route custom route.
	// The decorated method lives in the ViewSet body, so defining_class is the
	// ViewSet.
	drfProvAction = "action"
	// drfProvSynthesized — a router default route for a ViewSet that could not
	// be resolved on disk: the CRUD family is assumed (modelViewSetMethods)
	// with no class body ever read, so no body presence is known anywhere.
	drfProvSynthesized = "synthesized"
)

// drfCRUDProvenance computes the (provenance, defining_class) pair for a CRUD
// route on the given ViewSet (#3831).
//
//   - The ViewSet overrides the method in its body  → explicit, defining_class
//     = the ViewSet class.
//   - The ViewSet was resolved and the method is inherited from a mixin →
//     inherited, defining_class = the implementing mixin from
//     crudVerbDefiningMixin (or "" when the verb maps to an unknown custom
//     base — honest-partial: provenance stays inherited, defining_class
//     omitted).
//   - The ViewSet could NOT be resolved on disk → synthesized (the whole CRUD
//     family is an assumed router default; no body presence is known).
func drfCRUDProvenance(vc drfViewSetClass, viewSetName, method string) (provenance, definingClass string) {
	if vc.explicitMethods[method] {
		return drfProvExplicit, viewSetName
	}
	if !vc.resolved {
		// No class body was ever read — the verb is an assumed router default.
		return drfProvSynthesized, ""
	}
	return drfProvInherited, crudVerbDefiningMixin[method]
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
	// methodLine is the 1-based line of `def <methodName>(` in the ViewSet's
	// source file. Captured so the emitted http_endpoint attributes to the
	// decorated method body rather than routers.py (#2677).
	methodLine int
	// posture carries any per-@action posture override. DRF lets an @action
	// pass `permission_classes=[...]`, `throttle_classes=[...]`,
	// `pagination_class=X`, `authentication_classes=[...]` in the decorator;
	// those apply to THAT action's route only and override the ViewSet-level
	// posture for it (#3864). hasPosture distinguishes "override declared"
	// (even if empty, e.g. permission_classes=[]) from "no override".
	posture    drfPosture
	hasPosture bool
}

// drfPosture captures the DRF endpoint-posture knobs that a ViewSet (or an
// individual @action override) declares: the pagination class, and the
// permission / authentication / throttle class lists. The DRF expansion pass
// translates these into the cross-stack endpoint-property contract
// (paginated/pagination_*, middleware_chain/auth_required, rate_limited) and
// stamps them onto every router-generated http_endpoint the ViewSet backs —
// closing the gap where the inline posture passes never saw these synthetics
// because they live in a separate slice with Kind="http_endpoint" and a
// SourceFile that points at the ViewSet rather than the routers file (#3864).
type drfPosture struct {
	// paginationClass is the final dotted-segment of `pagination_class = X`
	// (e.g. "LimitOffsetPagination"), or "" when none is declared.
	paginationClass string
	// permissionClasses / authenticationClasses / throttleClasses are the
	// class symbols (final dotted segment) declared in the respective
	// `*_classes = [...]` attribute, in source order.
	permissionClasses     []string
	authenticationClasses []string
	throttleClasses       []string
	// permissionPages carries the fine-grained page-key identities that a
	// per-action permission resolution surfaced — the constant keys of a
	// `PERMISSION_PAGES["<KEY>"]` argument passed to a custom permission guard
	// (e.g. `CustomPagePermissionCheck(PERMISSION_PAGES["JURISDICTIONS"])`),
	// captured as ["JURISDICTIONS"] (#3972). These are the real per-action
	// authorisation identity for custom page/action guards; the DRF expansion
	// pass stamps them as the route's `auth_permissions` so grafel_auth_coverage
	// can answer "what page-permission does this route require?". Empty when no
	// page-key argument was resolvable (honest-partial — never fabricated).
	permissionPages []string
}

// empty reports whether the posture declares nothing at all.
func (p drfPosture) empty() bool {
	return p.paginationClass == "" &&
		len(p.permissionClasses) == 0 &&
		len(p.authenticationClasses) == 0 &&
		len(p.throttleClasses) == 0
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

	// seenMethods tracks (viewSetFile, viewSet.method) pairs for which we
	// have already emitted a synthetic SCOPE.Operation entity. Prevents
	// duplicate synthetic method entities when a ViewSet is registered on
	// multiple prefixes (e.g. the bare-prefix + parent-include variants).
	seenMethods := map[string]bool{}

	// currentURLPrefix is set to the parent include() prefix that is active
	// for the current emitCRUDFamily / emitActionRoutes call. The emit
	// closure reads it so every emitted entity carries the url_prefix
	// property for downstream consumers.
	var currentURLPrefix string

	// currentSerializer / currentBases carry the active ViewSet's static
	// serializer_class leaf and recognised base-class leaves into the emit
	// closure (the same indirection currentURLPrefix uses), so the per-verb
	// effective-contract stamp (#3835) can attach the serializer and resolve an
	// inherited verb's defining mixin from the baseknowledge pack. Reset per
	// ViewSet alongside currentURLPrefix.
	var currentSerializer string
	var currentBases []string

	// emit is invoked for every (verb, path) endpoint to materialise. sourceFile
	// is the file the handler lives in (typically the ViewSet's source file —
	// e.g. core/views/building_viewset.py — and ONLY the routers/urlconf file
	// when the ViewSet class cannot be resolved). sourceLine is the 1-based
	// line of `def <handler>(` (for explicit / @action methods) or the class
	// def line (for inherited mixin methods). #2677.
	emit := func(verb, canonical, sourceFile string, sourceLine int, viewSet, methodName string, posture drfPosture, provenance, definingClass string) {
		if canonical == "" || canonical == "/" {
			return
		}
		id := httproutes.SyntheticID(verb, canonical)
		if seen[id] {
			return
		}
		seen[id] = true

		// Issue #699c — set source_handler so the Phase-2 resolver
		// (ResolveHTTPEndpointHandlers) can emit an IMPLEMENTS edge from the
		// ViewSet method entity to this synthetic. The resolver's cross-file
		// globalIdx fallback (PR #753) finds the handler even though it lives
		// in a different file than the urlconf synthetic.
		//
		// When the CRUD method is inherited (not explicitly defined in the
		// ViewSet class body), emitViewSetMethodEntities emits a synthetic
		// SCOPE.Operation entity for it BEFORE the http_endpoint synthetics
		// are added to `out`. The resolver therefore sees a candidate in the
		// merged entity index and resolves successfully.
		//
		// For the ANY-verb detail catch-all (methodName == ""), we skip
		// source_handler because ANY has no single owning handler method.
		props := map[string]string{
			"verb":         strings.ToUpper(verb),
			"path":         canonical,
			"framework":    "django",
			"pattern_type": "drf_router_expanded",
		}
		if currentURLPrefix != "" {
			// Record the parent include() prefix so downstream consumers
			// can strip it when matching against client-side API calls that
			// use a baseURL (e.g. Axios baseURL = "/api/v1"). Fix #800.
			props["url_prefix"] = "/" + strings.Trim(currentURLPrefix, "/")
		}
		if viewSet != "" {
			if methodName != "" {
				qualifiedMethod := viewSet + "." + methodName
				props["drf_view_method"] = qualifiedMethod
				props["source_handler"] = "SCOPE.Operation:" + qualifiedMethod
			} else {
				props["drf_view_method"] = viewSet
				// ANY catch-all: no single handler — leave source_handler unset
				// so the resolver takes the NoHandlerProp keep-path.
			}
		}

		// #3864 — stamp endpoint posture (pagination / auth-middleware-chain /
		// rate-limit) resolved from the backing ViewSet (or a per-@action
		// override) onto this router-expanded synthetic. The inline posture
		// passes never reach these entities (separate slice, Kind="http_endpoint",
		// SourceFile=ViewSet not routers.py), so the DRF expansion is the only
		// place ViewSet-level posture can be attributed to the generated routes.
		stampDRFEndpointPosture(props, posture)

		// #3831 — stamp route provenance (explicit | inherited | action |
		// synthesized) + the defining_class so consumers can disambiguate an
		// overridden verb from an inherited one (the #278 question). provenance
		// is always set for a router-expanded route; defining_class is omitted
		// only in the honest-partial case (unknown custom base).
		if provenance != "" {
			props["provenance"] = provenance
		}
		if definingClass != "" {
			props["defining_class"] = definingClass
		}

		// #3835 (T5) — stamp the per-verb EFFECTIVE CONTRACT by merging this
		// route's provenance/defining_class with the baseknowledge pack's
		// per-verb defaults (default_status, error_statuses incl. the 400-on-
		// invalid for create/update) and the ViewSet's serializer. This is the
		// single artifact T6 (#3836) surfaces to prevent the #278 defect class:
		// an INHERITED create carries effective_status=201, effective_error_statuses=400,
		// effective_source_class=CreateModelMixin even though the ViewSet body is
		// empty. Honest-partial: when the verb's defining base is unknown to the
		// pack, the pack-derived fields are omitted, keeping only what is resolvable.
		stampDRFEffectiveContract(props, methodName, provenance, definingClass, viewSet, currentBases, currentSerializer)

		out = append(out, types.EntityRecord{
			ID:                 id,
			Name:               id,
			Kind:               httpEndpointKind,
			SourceFile:         sourceFile,
			StartLine:          sourceLine,
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
		// the parent prefix.
		//
		// Fix #800: do NOT also emit at the bare (unprefixed) path when the
		// file is reached via a parent include(). The bare-path entity is a
		// structural duplicate of the prefixed one — Django will only ever
		// resolve requests at the prefixed path (e.g. /api/v1/buildings/),
		// never at /buildings/. Emitting both caused 486 duplicate
		// http_endpoint entities on fixture-a (40% inflation).
		//
		// We add the bare prefix ONLY when the file has no parent include()
		// at all (i.e. the router file is at the URL conf root — uncommon but
		// valid, and required so routes still land somewhere rather than being
		// silently dropped).
		parentPrefixes := findParentIncludePrefixes(relPath, parentFiles, fileReader)
		if drfDbgEnabled {
			fmt.Fprintf(os.Stderr, "DRF: file=%s parentPrefixes=%v\n", relPath, parentPrefixes)
		}
		if len(parentPrefixes) == 0 {
			// File is not included from anywhere — emit at bare prefix.
			parentPrefixes = []string{""}
			if drfDbgEnabled {
				fmt.Fprintf(os.Stderr, "DRF: file=%s NO parent found → bare prefix fallback\n", relPath)
			}
		}

		// Find every router.register() call. Each yields (routerVar, prefix, ViewSet).
		// Flatten parenthesised newlines so multi-line register() calls
		// (common DRF style: prefix on line 1, ViewSet on line 2) match the
		// single-line regex.
		flatSrc := flattenParenthesised(src)
		registers := drfRouterRegisterDetailedRe.FindAllStringSubmatch(flatSrc, -1)

		// Map of router variable name -> nested-router parent prefix
		// (from `NestedSimpleRouter(parent, "prefix", lookup="x")`).
		nestedPrefixes := buildNestedRouterPrefixes(flatSrc)

		// Supplement registers with register() calls on nested router variables
		// (e.g. `nested.register(r"companies", CompaniesViewSet)`). The nested
		// router variable names (e.g. "nested", "companies_router") don't match
		// the name-pattern in drfRouterRegisterDetailedRe, so they must be
		// captured separately using the generic register regex.
		// Fixes #1424.
		if len(nestedPrefixes) > 0 {
			registers = appendNestedRegisterCalls(registers, flatSrc, nestedPrefixes)
		}

		if len(registers) == 0 {
			continue
		}

		// Map of router variable name -> local path() prefix from
		// `path("api/v1/", include(routerVar.urls))` calls in THIS file.
		// When parentPrefixes is [""] (no parent include found), these local
		// prefixes provide the correct mount point so we don't fall back to
		// emitting bare-prefix routes. Fix #1124.
		localRouterPrefixes := buildLocalRouterPrefixes(flatSrc)

		// Build a routerVar → []registerPrefix index for nested-router
		// sentinel resolution. Needed by expandRegisterPrefixes when routerVar
		// is a NestedSimpleRouter child (Fixes #1424).
		routerRegisterMap := buildRouterRegisterMap(flatSrc)

		// Resolve imports in this file so a bare ViewSet identifier maps to
		// its defining module + thus its file.
		importMap := parseImports(src)

		for _, m := range registers {
			routerVar := m[1]
			prefix := m[2]
			viewSetName := m[3]

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
			// Also inject the local path() prefix for this router variable
			// (e.g. `path("api/v1/", include(router.urls))` contributes "api/v1/"
			// as the local prefix for "router"). Fix #1124.
			effectivePrefixes := applyLocalRouterPrefix(parentPrefixes, routerVar, localRouterPrefixes)
			composedPrefixes := expandRegisterPrefixes(prefix, effectivePrefixes, nestedPrefixes, routerVar, routerRegisterMap)

			// Issue #699c — emit synthetic SCOPE.Operation entities for each
			// CRUD method that the ViewSet exposes via inheritance but does NOT
			// explicitly define. Without these entities, source_handler cannot
			// resolve against the merged entity index and the http_endpoint
			// synthetics would remain orphaned. emitViewSetMethodEntities must
			// run BEFORE emitCRUDFamily so the method entities appear in `out`
			// ahead of the http_endpoint synthetics (first-writer-wins dedup).
			//
			// We use the ViewSet's source file when available; fall back to the
			// urlconf file. seenMethods prevents duplicate emission when the
			// same ViewSet is registered on multiple prefixes.
			handlerFile := viewFile
			if handlerFile == "" {
				handlerFile = relPath
			}
			emitViewSetMethodEntities(&out, seenMethods, viewSetName, vc, handlerFile)

			// #2677 — endpoint source attribution: when the ViewSet class was
			// resolved successfully, emit entities pointing at the ViewSet's
			// file (and method's def line) so `grafel_inspect` on the
			// endpoint surfaces the real handler, not the empty routers.py
			// registration line. When resolution fails, fall back to relPath
			// so the entity still has a valid source_file.
			endpointSourceFile := viewFile
			if endpointSourceFile == "" {
				endpointSourceFile = relPath
			}

			// expandRegisterPrefixes produces composedPrefixes in the same
			// order as effectivePrefixes, so we can zip them to recover the
			// parent prefix that applies to each composed prefix. This lets
			// the emit closure attach url_prefix correctly.
			currentSerializer = vc.serializerClass
			currentBases = vc.classBases
			for i, fullPrefix := range composedPrefixes {
				if i < len(effectivePrefixes) {
					currentURLPrefix = effectivePrefixes[i]
				} else {
					currentURLPrefix = ""
				}
				emitCRUDFamily(emit, fullPrefix, vc, endpointSourceFile, viewSetName)
				emitActionRoutes(emit, fullPrefix, vc, endpointSourceFile, viewSetName)
			}
			currentURLPrefix = "" // reset for safety
			currentSerializer = ""
			currentBases = nil
		}
	}
	return out
}

// DeduplicateNestedURLConfDRF filters urlconf_nested_include ANY-verb entities
// when drf_router_expanded per-verb entries cover the same path.
//
// When `router.register('users', UserViewSet)` is included via
// `path('api/v1/', include(router.urls))`, the urlconf_nested_include pass
// emits a parent ANY entry for `api/v1/users` and the drf_router_expanded
// pass emits per-method entries (GET/POST/etc.) for the same path. They
// duplicate.
//
// This function detects the overlap and removes the urlconf_nested_include
// ANY entry when a drf_router_expanded per-method entry exists for the same
// (verb, path) combination.
//
// nestedURLConfEntities: output from ApplyDjangoNestedURLConf
// drfEntities: output from ApplyDjangoDRFRoutes
// Returns: filtered nestedURLConfEntities with duplicates removed.
func DeduplicateNestedURLConfDRF(
	nestedURLConfEntities []types.EntityRecord,
	drfEntities []types.EntityRecord,
) []types.EntityRecord {
	if len(nestedURLConfEntities) == 0 || len(drfEntities) == 0 {
		return nestedURLConfEntities
	}

	// Build an index of drf_router_expanded (verb, path) pairs.
	// Map key: "verb:path", value: true if any drf_router_expanded entry exists.
	drfVerbPaths := make(map[string]bool)
	for _, e := range drfEntities {
		if e.Properties == nil {
			continue
		}
		if e.Properties["pattern_type"] != "drf_router_expanded" {
			continue
		}
		verb := e.Properties["verb"]
		path := e.Properties["path"]
		if verb != "" && path != "" {
			drfVerbPaths[verb+":"+path] = true
		}
	}

	// Filter: keep only urlconf_nested_include entries that DON'T have
	// a corresponding drf_router_expanded per-verb entry for the same path.
	var result []types.EntityRecord
	for _, e := range nestedURLConfEntities {
		if e.Properties == nil {
			result = append(result, e)
			continue
		}
		if e.Properties["pattern_type"] != "urlconf_nested_include" {
			result = append(result, e)
			continue
		}

		// This is a urlconf_nested_include entry. Check if ANY per-verb
		// drf_router_expanded entry covers the same path.
		path := e.Properties["path"]
		if path == "" {
			result = append(result, e)
			continue
		}

		// If any per-verb entry exists for this path, drop the ANY entry.
		// We check common HTTP verbs to determine overlap.
		hasDRFCoverage := false
		for _, verb := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
			if drfVerbPaths[verb+":"+path] {
				hasDRFCoverage = true
				break
			}
		}

		if !hasDRFCoverage {
			// No drf_router_expanded entry for this path; keep the nested_include ANY.
			result = append(result, e)
		}
		// else: drop the nested_include ANY entry (don't append to result)
	}

	return result
}

// DeduplicateHTTPSynthesisANY removes ANY-verb http_endpoint entities emitted
// by synthesizeDjangoFromComposed (pattern_type="http_endpoint_synthesis")
// when drf_router_expanded per-verb entries cover the same canonical path.
//
// Root cause (issue #1126): applyHTTPEndpointSynthesis runs per-file during
// Pass 2.5 and calls synthesizeDjangoFromComposed, which always emits
// http:ANY:<path> from each ast_driven Route entity. ApplyDjangoDRFRoutes
// runs later in Pass 2.6b and emits the correct per-verb entries
// (http:GET:..., http:POST:..., etc.). Because the IDs differ (ANY vs GET/
// POST/etc.), both sets coexist in the merged entity list, producing ~200
// spurious ANY paths.
//
// This function removes the ANY synthesis entries for every path already
// covered by at least one drf_router_expanded per-verb entry. Since #1692,
// emitCRUDFamily no longer emits a drf_router_expanded ANY for detail routes
// when per-verb routes are present, so this function is effectively removing
// ALL http_endpoint_synthesis ANY entries that have drf_router_expanded coverage.
//
// synthEntities: the merged entity slice from pass2 (from applyHTTPEndpointSynthesis)
// drfEntities:   output from ApplyDjangoDRFRoutes
// Returns: synthEntities with spurious ANY entries removed.
func DeduplicateHTTPSynthesisANY(
	synthEntities []types.EntityRecord,
	drfEntities []types.EntityRecord,
) []types.EntityRecord {
	if len(synthEntities) == 0 || len(drfEntities) == 0 {
		return synthEntities
	}

	// Build a set of canonical paths covered by drf_router_expanded entries
	// with a concrete (non-ANY) verb. Key: canonical path string.
	drfCoveredPaths := make(map[string]bool)
	for _, e := range drfEntities {
		if e.Properties == nil {
			continue
		}
		if e.Properties["pattern_type"] != "drf_router_expanded" {
			continue
		}
		verb := e.Properties["verb"]
		path := e.Properties["path"]
		if verb != "" && verb != "ANY" && path != "" {
			drfCoveredPaths[path] = true
		}
	}

	if len(drfCoveredPaths) == 0 {
		return synthEntities
	}

	// Filter: remove http_endpoint_synthesis ANY entries whose path is
	// already covered by drf_router_expanded concrete-verb entries.
	result := synthEntities[:0:0]
	for _, e := range synthEntities {
		if e.Kind != httpEndpointKind {
			result = append(result, e)
			continue
		}
		if e.Properties == nil {
			result = append(result, e)
			continue
		}
		if e.Properties["pattern_type"] != "http_endpoint_synthesis" {
			result = append(result, e)
			continue
		}
		if e.Properties["verb"] != "ANY" {
			result = append(result, e)
			continue
		}
		// This is an http_endpoint_synthesis ANY entry. Drop it when the
		// drf_router_expanded pass covers the same path with concrete verbs.
		if drfCoveredPaths[e.Properties["path"]] {
			continue
		}
		result = append(result, e)
	}

	return result
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

	// Determine whether the target file contains any router.register() calls.
	// Used to gate the attribute-form include heuristic below (#1278).
	var targetHasRegister bool
	if targetContent := fileReader(targetRelPath); len(targetContent) > 0 {
		targetHasRegister = drfRouterRegisterDetailedRe.Match(targetContent)
	}

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

		if drfDbgEnabled {
			fmt.Fprintf(os.Stderr, "DRF: scanning candidate=%s for target=%s\n", candidate, targetRelPath)
		}

		// String-form include: path("prefix", include("module.path"))
		for _, m := range djangoIncludeStringRe.FindAllStringSubmatch(src, -1) {
			parentPrefix := m[1]
			modulePath := m[2]
			resolved := modulePathToFilePath(modulePath)
			if drfDbgEnabled {
				fmt.Fprintf(os.Stderr, "DRF:   include match prefix=%q module=%q resolved=%q target=%q\n",
					parentPrefix, modulePath, resolved, targetRelPath)
			}
			if resolved != targetRelPath {
				alt := modulePathToFilePath_relToParent(modulePath, candidate)
				if drfDbgEnabled {
					fmt.Fprintf(os.Stderr, "DRF:   alt=%q\n", alt)
				}
				if alt != targetRelPath {
					continue
				}
			}
			if !seen[parentPrefix] {
				prefixes = append(prefixes, parentPrefix)
				seen[parentPrefix] = true
				if drfDbgEnabled {
					fmt.Fprintf(os.Stderr, "DRF:   FOUND prefix=%q for target=%s from candidate=%s\n",
						parentPrefix, targetRelPath, candidate)
				}
			}
		}

		// #1278 — Attribute-form include: path("prefix", include(routerVar.urls)).
		// When the parent file mounts a router variable via attribute-form include
		// rather than a string module path, djangoIncludeStringRe doesn't match.
		// Heuristic: if the candidate parent file has `path("prefix",
		// include(someVar.urls))` AND the target file contains router.register()
		// calls, treat this as a parent-include relationship and use the prefix.
		//
		// This heuristic is safe because:
		// - We only fire it when the target file actually defines router registrations.
		// - In the vast majority of DRF projects, each urlconf file is mounted at
		//   most one attribute-form router, so false-positive prefix collisions are
		//   extremely unlikely.
		// - If the heuristic over-fires (two router files in same project with same
		//   parent), the worst case is a duplicate http_endpoint at the correct
		//   prefix — the dedup pass removes it.
		if targetHasRegister {
			flatSrc := flattenParenthesised(src)
			for _, m := range drfRouterAttrIncludeRe.FindAllStringSubmatch(flatSrc, -1) {
				parentPrefix := m[1]
				if parentPrefix == "" {
					continue
				}
				// Only apply when the string-form include did NOT already find a
				// prefix for this file — avoid double-adding the same prefix.
				if !seen[parentPrefix] {
					prefixes = append(prefixes, parentPrefix)
					seen[parentPrefix] = true
				}
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

// buildLocalRouterPrefixes scans a flattened Python source for
// `path("prefix", include(routerVar.urls))` patterns and returns a map of
// routerVar → prefix. These represent LOCAL router mounts within the same
// file (as opposed to parent-file includes that findParentIncludePrefixes handles).
//
// For example, given:
//
//	path('api/v1/', include(router.urls))
//	path('api/v2/', include(api_router.urls))
//
// returns {"router": "api/v1/", "api_router": "api/v2/"}.
//
// Used by applyLocalRouterPrefix to fix #1124.
func buildLocalRouterPrefixes(flatSrc string) map[string]string {
	out := map[string]string{}
	for _, m := range drfRouterAttrIncludeRe.FindAllStringSubmatch(flatSrc, -1) {
		prefix := m[1]
		routerVar := m[2]
		if routerVar != "" {
			out[routerVar] = prefix
		}
	}
	return out
}

// appendNestedRegisterCalls supplements the registers slice with register()
// calls found on nested router variables. drfRouterRegisterDetailedRe only
// matches router-named variables (e.g. "router", "api_router"). Nested router
// variables have arbitrary names (e.g. "nested", "companies_router") and are
// only known after buildNestedRouterPrefixes has run.
//
// For each variable name that appears as a key in nestedPrefixes, we scan
// flatSrc with drfGenericRegisterRe and add matching register() calls to
// registers.
func appendNestedRegisterCalls(
	registers [][]string,
	flatSrc string,
	nestedPrefixes map[string]string,
) [][]string {
	for _, m := range drfGenericRegisterRe.FindAllStringSubmatch(flatSrc, -1) {
		routerVar := m[1]
		if _, isNested := nestedPrefixes[routerVar]; !isNested {
			continue
		}
		// Avoid duplicates: only add if not already present (drfRouterRegisterDetailedRe
		// might have matched if the var name happened to match the router pattern).
		alreadyPresent := false
		for _, existing := range registers {
			if len(existing) >= 3 && existing[1] == routerVar && existing[2] == m[2] {
				alreadyPresent = true
				break
			}
		}
		if !alreadyPresent {
			registers = append(registers, m)
		}
	}
	return registers
}

// buildRouterRegisterMap returns a map of routerVar → list of register()
// prefixes for that router variable. For example, given:
//
//	router.register(r"groups", GroupsViewSet)
//	router.register(r"users", UsersViewSet)
//
// returns {"router": ["groups", "users"]}.
//
// Used by expandRegisterPrefixes to resolve $$PARENT:routerVar$$ sentinels
// when the routerVar is a nested-router child.
func buildRouterRegisterMap(flatSrc string) map[string][]string {
	out := map[string][]string{}
	for _, m := range drfRouterRegisterDetailedRe.FindAllStringSubmatch(flatSrc, -1) {
		rv := m[1]
		prefix := m[2]
		out[rv] = append(out[rv], prefix)
	}
	return out
}

// parseNestedSentinel extracts the parentVar and lookup field from a
// sentinel string produced by parentVarLookupChild. The sentinel format is:
//
//	$$PARENT:<parentVar>$$/{<lookup>}/<childPrefix>
//
// Returns (parentVar, lookup, true) on success, or ("", "", false) if the
// sentinel cannot be parsed.
func parseNestedSentinel(sentinel string) (parentVar, lookup string, ok bool) {
	const pfx = "$$PARENT:"
	const sfx = "$$"
	if !strings.HasPrefix(sentinel, pfx) {
		return
	}
	rest := sentinel[len(pfx):]
	endIdx := strings.Index(rest, sfx)
	if endIdx < 0 {
		return
	}
	parentVar = rest[:endIdx]
	tail := rest[endIdx+len(sfx):] // e.g. "/{group}/groups"
	// Extract lookup from first {…} segment.
	if !strings.HasPrefix(tail, "/{") {
		return
	}
	tail = tail[2:] // strip "/{"
	closeBrace := strings.Index(tail, "}")
	if closeBrace < 0 {
		return
	}
	lookup = tail[:closeBrace]
	ok = true
	return
}

// applyLocalRouterPrefix returns the effective parent-prefix list for a
// router variable. When parentPrefixes is the bare-prefix fallback [""],
// and the local-router-prefix map contains a prefix for this routerVar
// (e.g. from `path("api/v1/", include(router.urls))`), the local prefix
// is used instead of "" to avoid emitting bare-path duplicates.
//
// If the file already has a non-empty parent prefix from an outer include()
// chain, the local prefix is composed INSIDE the outer prefix. This handles
// the uncommon case where a child routers.py is reached via two levels of
// include nesting.
//
// Fix #1124: prevents routes registered on a locally-mounted router from
// being emitted at the bare (no-prefix) path when their real mount is under
// a prefix like "api/v1/".
func applyLocalRouterPrefix(parentPrefixes []string, routerVar string, localPrefixes map[string]string) []string {
	localPrefix, hasLocal := localPrefixes[routerVar]
	if !hasLocal {
		return parentPrefixes
	}
	// If parentPrefixes is exactly [""] (bare-prefix fallback because no outer
	// include was found), substitute the local prefix so we don't emit at "/".
	if len(parentPrefixes) == 1 && parentPrefixes[0] == "" {
		return []string{localPrefix}
	}
	// If parentPrefixes already contains real prefixes from an outer include(),
	// compose each with the local prefix. This handles multi-level nesting where
	// a routers.py is both locally mounted and externally included.
	out := make([]string, 0, len(parentPrefixes))
	for _, pp := range parentPrefixes {
		out = append(out, joinDjangoRoutePaths(pp, localPrefix))
	}
	return out
}

// expandRegisterPrefixes returns the set of composed prefixes that the
// given router.register() prefix should land at, given the parent include()
// prefixes for this file.
//
// When routerVar is a nested router (entry in nestedPrefixes), the path is
// composed as:
//
//	outerIncludePrefix + parentRouterRegisterPrefix + /{lookup}/ + registerPrefix
//
// The nestedPrefixes value is a sentinel produced by buildNestedRouterPrefixes
// of the form "$$PARENT:parentVar$$/{lookup}/...". We extract parentVar and
// lookup from the sentinel, look up the parent router's own register() prefixes
// in routerRegisterMap, and compose the full nested path.
//
// When routerVar is a regular router this falls through to the simple
// composition: parentIncludePrefix + "/" + registerPrefix.
func expandRegisterPrefixes(
	registerPrefix string,
	parentPrefixes []string,
	nestedPrefixes map[string]string,
	routerVar string,
	routerRegisterMap map[string][]string,
) []string {
	// Nested router path: resolve the sentinel and compose the full path.
	if sentinel, isNested := nestedPrefixes[routerVar]; isNested {
		if parentVar, lookup, ok := parseNestedSentinel(sentinel); ok {
			if parentRegPrefixes, found := routerRegisterMap[parentVar]; found && len(parentRegPrefixes) > 0 {
				var out []string
				for _, pp := range parentPrefixes {
					for _, parentRegPrefix := range parentRegPrefixes {
						// Compose: outerIncludePrefix/parentRegisterPrefix/{lookup}/registerPrefix
						nestedBase := joinDjangoRoutePaths(parentRegPrefix, "{"+lookup+"}")
						fullPath := joinDjangoRoutePaths(nestedBase, registerPrefix)
						out = append(out, joinDjangoRoutePaths(pp, fullPath))
					}
				}
				if len(out) > 0 {
					return out
				}
			}
		}
	}

	// Regular router: simple composition.
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
	emit func(verb, canonical, sourceFile string, sourceLine int, viewSet, methodName string, posture drfPosture, provenance, definingClass string),
	fullPrefix string,
	vc drfViewSetClass,
	sourceFile string,
	viewSetName string,
) {
	emitOneCRUDFamily(emit, fullPrefix, vc.lookupField, vc, sourceFile, viewSetName)
}

// crudMethodLine returns the 1-based line to attribute to a CRUD endpoint:
// the explicit `def <method>(` line when the ViewSet overrides the method,
// otherwise the `class <name>(...)` declaration line. Returns 0 when neither
// is known so callers leave StartLine unset (#2677).
func crudMethodLine(vc drfViewSetClass, method string) int {
	if line, ok := vc.methodLines[method]; ok && line > 0 {
		return line
	}
	return vc.classDefLine
}

// emitOneCRUDFamily emits the CRUD-route family for a single placeholder
// shape (e.g. `{pk}` or `{id}`). The first call (with the canonical
// `lookup_field`) is the source of truth; subsequent calls with alternate
// placeholders widen the cross-repo match surface (#704 companion).
func emitOneCRUDFamily(
	emit func(verb, canonical, sourceFile string, sourceLine int, viewSet, methodName string, posture drfPosture, provenance, definingClass string),
	fullPrefix string,
	placeholder string,
	vc drfViewSetClass,
	sourceFile string,
	viewSetName string,
) {
	detailBase := fullPrefix + "/{" + placeholder + "}"

	// emitCRUD wraps emit with the #3831 provenance/defining_class computed for
	// the given CRUD method, so each route is tagged explicit | inherited |
	// synthesized with the right defining class. #3864 — each CRUD route inherits
	// the ViewSet-level posture; #3933 — when get_permissions / a
	// permission_classes_by_action dict resolves a per-action permission for this
	// method, that overrides the flat-union permission_classes for THIS route only.
	emitCRUD := func(verb, canonical string, line int, method string) {
		prov, defClass := drfCRUDProvenance(vc, viewSetName, method)
		pos := postureForAction(vc, method)
		emit(verb, canonical, sourceFile, line, viewSetName, method, pos, prov, defClass)
	}

	if vc.crudMethods["list"] {
		emitCRUD("GET", canonicalDjango(fullPrefix), crudMethodLine(vc, "list"), "list")
	}
	if vc.crudMethods["create"] {
		emitCRUD("POST", canonicalDjango(fullPrefix), crudMethodLine(vc, "create"), "create")
	}
	hasDetailVerb := false
	if vc.crudMethods["retrieve"] {
		emitCRUD("GET", canonicalDjango(detailBase), crudMethodLine(vc, "retrieve"), "retrieve")
		hasDetailVerb = true
	}
	if vc.crudMethods["update"] {
		emitCRUD("PUT", canonicalDjango(detailBase), crudMethodLine(vc, "update"), "update")
		hasDetailVerb = true
	}
	if vc.crudMethods["partial_update"] {
		emitCRUD("PATCH", canonicalDjango(detailBase), crudMethodLine(vc, "partial_update"), "partial_update")
		hasDetailVerb = true
	}
	if vc.crudMethods["destroy"] {
		emitCRUD("DELETE", canonicalDjango(detailBase), crudMethodLine(vc, "destroy"), "destroy")
		hasDetailVerb = true
	}
	// Emit an ANY-verb detail fallback ONLY when no per-verb detail routes
	// were emitted above (i.e. the ViewSet class could not be resolved and
	// crudMethods is empty). When per-verb routes are present, the ANY is
	// redundant: it pollutes the index with a duplicate path that the
	// verb-aware matcher must skip, and it defeated the iter3 calibration
	// top add-rec #2 (detail routes still showing method=ANY). Fix #1692.
	if !hasDetailVerb {
		// The ANY catch-all only appears when the ViewSet could not be resolved
		// (empty crudMethods), so the detail route is a pure router default with
		// no body anywhere → synthesized, no defining_class (#3831).
		emit("ANY", canonicalDjango(detailBase), sourceFile, vc.classDefLine, viewSetName, "", vc.posture, drfProvSynthesized, "")
	}
}

// emitActionRoutes emits one http_endpoint per @action method on the
// ViewSet. For detail=True actions the route is
// /<prefix>/{lookup}/<url_path>/; for detail=False it is /<prefix>/<url_path>/.
func emitActionRoutes(
	emit func(verb, canonical, sourceFile string, sourceLine int, viewSet, methodName string, posture drfPosture, provenance, definingClass string),
	fullPrefix string,
	vc drfViewSetClass,
	sourceFile string,
	viewSetName string,
) {
	for _, act := range vc.actions {
		// #3864 — an @action's own posture override (if any) applies to its
		// route; otherwise the route inherits the ViewSet-level posture.
		// #3933 — precedence: an explicit `permission_classes=[...]` kwarg on the
		// @action decorator wins outright; failing that, a per-action permission
		// resolved from get_permissions / permission_classes_by_action overrides
		// the flat-union permission_classes for this action's route.
		actPosture := vc.posture
		if act.hasPosture {
			actPosture = act.posture
		} else {
			actPosture = postureForAction(vc, act.methodName)
		}
		// #5230 / #5298 — URL-segment derivation faithful to DRF semantics:
		//   1. An explicit `url_path="<value>"` kwarg wins VERBATIM, including any
		//      embedded `(?P<name>regex)` capture groups — canonicalDjango rewrites
		//      those to `{name}` path params downstream (stripPythonNamedGroups).
		//   2. With no url_path, DRF uses the Python method name VERBATIM as the URL
		//      segment (underscores preserved). In rest_framework/decorators.py the
		//      @action decorator sets `func.url_path = url_path if url_path else
		//      func.__name__` — only `url_name` (the reverse() lookup name, not the
		//      URL) gets the `_`→`-` replacement. So `do_thing` routes at
		//      `.../do_thing/`, NOT `.../do-thing/`.
		segment := act.urlPath
		if segment == "" {
			segment = drfDefaultActionSegment(act.methodName)
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
			actionLine := act.methodLine
			if actionLine == 0 {
				actionLine = vc.classDefLine
			}
			for _, verb := range methods {
				// #3831 — an @action route's handler is the decorated method,
				// which lives in the ViewSet body, so defining_class is the
				// ViewSet itself.
				emit(verb, canonical, sourceFile, actionLine, viewSetName, act.methodName, actPosture, drfProvAction, viewSetName)
			}
		}
	}
}

// drfDefaultActionSegment returns the URL segment DRF derives from an
// @action-decorated method NAME when no `url_path=` kwarg is supplied
// (#5230 / #5298). DRF uses the method name VERBATIM, with underscores
// PRESERVED: `rest_framework/decorators.py` sets
// `func.url_path = url_path if url_path else func.__name__`, so a method
// `do_thing` is routed at `.../do_thing/`. (The `_`→`-` replacement applies
// only to `url_name`, the reverse() lookup name — NOT the URL segment. The
// earlier #5230 belief that the default url_path was hyphenated conflated the
// two and broke snake_case action paths — #5298.)
func drfDefaultActionSegment(methodName string) string {
	return methodName
}

// emitViewSetMethodEntities emits synthetic SCOPE.Operation entities for each
// CRUD method that the ViewSet exposes via inheritance but does NOT explicitly
// define in its class body (i.e. methods in crudMethods but NOT in
// explicitMethods). These synthetic entities give ResolveHTTPEndpointHandlers
// a target for the source_handler = "SCOPE.Operation:ViewSet.method" property
// set on http_endpoint synthetics, so it can emit IMPLEMENTS edges and resolve
// the orphan.
//
// Without these, a `ModelViewSet` with no overridden methods would have 6
// http_endpoint entities with source_handler set, but none of the CRUD method
// entities in the index (the Python extractor only emits methods it actually
// parses from the file). The resolver would drop all 6 synthetics as
// HandlerDropped.
//
// Design notes:
//   - Uses kind=SCOPE.Operation, subtype=method — identical to what the Python
//     extractor emits for explicitly-defined ViewSet methods.
//   - Name = "<ViewSet>.<method>" — the resolver's globalIdx key (kind, name)
//     will find this entity when looking up source_handler references.
//   - SourceFile = the ViewSet's source file (or the urlconf file as fallback).
//   - QualityScore = 0.7 (below extractor-emitted entities at 1.0, above the
//     http_endpoint synthetic at 0.8) so dedup-by-ID prefers the real entity
//     if the extractor later emits one for the same method.
//   - seenMethods prevents duplicate emission when the same ViewSet is
//     registered on multiple URL prefixes (bare + parent-include variants).
func emitViewSetMethodEntities(
	out *[]types.EntityRecord,
	seenMethods map[string]bool,
	viewSetName string,
	vc drfViewSetClass,
	sourceFile string,
) {
	for method := range vc.crudMethods {
		if vc.explicitMethods[method] {
			// The Python extractor already emitted a real SCOPE.Operation
			// entity for this method — no synthetic needed.
			continue
		}
		key := sourceFile + "\x00" + viewSetName + "." + method
		if seenMethods[key] {
			continue
		}
		seenMethods[key] = true
		qualifiedName := viewSetName + "." + method
		*out = append(*out, types.EntityRecord{
			// ID is left blank — stampEntityIDs in the indexer pipeline
			// will compute it from (repoTag, Kind, Name, SourceFile).
			Name:               qualifiedName,
			Kind:               "SCOPE.Operation",
			Subtype:            "method",
			Language:           "python",
			SourceFile:         sourceFile,
			Signature:          "def " + method + "(self, request, *args, **kwargs)",
			EnrichmentRequired: false,
			QualityScore:       0.7,
			Properties: map[string]string{
				"pattern_type":      "drf_viewset_implicit_method",
				"viewset_class":     viewSetName,
				"inherited_from":    "rest_framework",
				"drf_method_origin": method,
			},
		})
	}
}

// stampDRFEndpointPosture translates a resolved drfPosture into the cross-stack
// endpoint-property contract and writes it onto a router-expanded synthetic's
// Properties map. It mirrors, byte-for-byte, the properties the inline posture
// passes set on http_endpoint_definition entities so a router-expanded
// http_endpoint is indistinguishable from a same-file synthesized one at the
// MCP surface (#3864):
//
//   - pagination_class  → paginated=true + pagination_style / pagination_params /
//     pagination_source (same shape as applyEndpointPagination /
//     drfClassPaginationVerdict). Only recognised classes
//     (drfPaginationClassStyle) flip paginated — an unknown custom paginator
//     stays unstamped (honest-partial).
//   - permission/authentication/throttle_classes → an ORDERED view-scope
//     middleware_chain (same contract as applyPythonMiddlewareCoverage /
//     indexDRFViewMiddleware) plus auth_required when a non-AllowAny permission
//     or any authentication class is declared.
//   - throttle_classes → rate_limited=true + rate_limit_source (the flat
//     rate-limit contract stampJSRateLimit uses; the concrete rate lives in
//     settings, not statically resolvable here, so rate_limit is omitted —
//     honest-partial).
//
// No-op when the posture declares nothing (an un-configured ViewSet's routes
// stay unstamped — the negative case the spec requires).
func stampDRFEndpointPosture(props map[string]string, posture drfPosture) {
	if props == nil || posture.empty() {
		return
	}

	// Pagination — only a recognised DRF paginator flips `paginated`.
	if posture.paginationClass != "" {
		if style, known := drfPaginationClassStyle[posture.paginationClass]; known {
			props["paginated"] = "true"
			props["pagination_style"] = style
			if params := drfDefaultParamsFor(style); len(params) > 0 {
				props["pagination_params"] = strings.Join(uniqueSorted(params), ",")
			}
			props["pagination_source"] = "pagination_class=" + posture.paginationClass
		}
	}

	// Middleware chain (view scope): permission → authentication → throttle, in
	// the same order indexDRFViewMiddleware assembles them.
	var chain []middlewareEntry
	authRequired := false
	for _, sym := range posture.permissionClasses {
		ak := middlewareAuthKind(sym)
		if ak == "" {
			ak = "auth"
		}
		// AllowAny is an explicit "no auth" permission — it must NOT set
		// auth_required, matching DRF semantics.
		if !strings.EqualFold(sym, "AllowAny") {
			authRequired = true
		}
		chain = append(chain, middlewareEntry{
			Name:     sym,
			Expr:     sym,
			Scope:    pythonMWScopeView,
			AuthKind: ak,
		})
	}
	for _, sym := range posture.authenticationClasses {
		ak := middlewareAuthKind(sym)
		if ak == "" {
			ak = "auth"
		}
		authRequired = true
		chain = append(chain, middlewareEntry{
			Name:     sym,
			Expr:     sym,
			Scope:    pythonMWScopeView,
			AuthKind: ak,
		})
	}
	for _, sym := range posture.throttleClasses {
		chain = append(chain, middlewareEntry{
			Name:  sym,
			Expr:  sym,
			Scope: pythonMWScopeView,
		})
	}
	chain = dedupeMiddlewareEntries(chain)
	stampMiddlewareChainEntries(props, chain, pythonMWScopeOrder)

	if authRequired {
		props["auth_required"] = "true"
	}

	// #3972 — fine-grained per-action page-key identity. When a per-action
	// permission resolution captured a `PERMISSION_PAGES["<KEY>"]` argument to a
	// custom page/action guard (e.g. CustomPagePermissionCheck(PERMISSION_PAGES["JURISDICTIONS"])),
	// surface those constant keys as `auth_permissions` — the established
	// cross-language fine-grained-permission property (http_endpoint_jsts_auth.go)
	// so grafel_auth_coverage answers "what page-permission does this route
	// require?". A custom guard with a page-key always requires auth.
	if len(posture.permissionPages) > 0 {
		props["auth_permissions"] = strings.Join(uniqueSorted(posture.permissionPages), ",")
		props["auth_required"] = "true"
	}

	// Throttle classes ⇒ the route is rate-limited.
	if len(posture.throttleClasses) > 0 {
		props["rate_limited"] = "true"
		props["rate_limit_scope"] = "view"
		props["rate_limit_source"] = "throttle_classes=" + strings.Join(posture.throttleClasses, ",")
	}
}

// effective-contract property keys stamped on every router-expanded route by
// stampDRFEffectiveContract (#3835, T5). They are the per-verb EFFECTIVE
// CONTRACT — the merge of route provenance (#3831) + ViewSet posture (#3864) +
// the baseknowledge pack's per-verb defaults (#3832) — that T6 (#3836) surfaces
// via grafel_effective_contract. They are deliberately namespaced
// `effective_*` (plus serializer_class) so they never collide with the raw
// posture / provenance props already on the entity.
const (
	// effective_kind ∈ {explicit, inherited, action}. The verb taxonomy the
	// contract answers: did the ViewSet override this verb in its body
	// (explicit), inherit it from a mixin (inherited), or expose it via an
	// @action (action)? Derived from the route provenance; the synthesized
	// router-default family is reported as `inherited` (an assumed mixin verb)
	// but carries no pack fields when the base is unknown (honest-partial).
	propEffectiveKind = "effective_kind"
	// effective_source_class is the FQN/leaf of the class that defines the
	// verb's body — the ViewSet itself for explicit/action verbs, the
	// implementing mixin for inherited verbs.
	propEffectiveSourceClass = "effective_source_class"
	// effective_status is the verb's default success HTTP status (create→201,
	// update/list/retrieve→200, destroy→204) from the pack. Omitted when the
	// pack has no curated default for the verb (StatusUnknown) — never fabricated.
	propEffectiveStatus = "effective_status"
	// effective_error_statuses is the comma-separated documented non-success
	// status set the verb can produce as part of its contract — the #278 fact:
	// create/update → 400 on invalid payload via is_valid(raise_exception=True).
	// Omitted when none is curated.
	propEffectiveErrorStatuses = "effective_error_statuses"
	// effective_behaviour is the pack's short human-readable behavioural note
	// for the verb (e.g. the is_valid→400 fact). Omitted when none is curated.
	propEffectiveBehaviour = "effective_behaviour"
	// effective_pagination is "true" when the pack marks the verb as paginated
	// (DRF list) AND a pagination posture is in effect on the route. Omitted
	// otherwise.
	propEffectivePagination = "effective_pagination"
	// effective_permission_applicable is "true" when the framework applies the
	// class/project permission classes to this verb by default (every DRF route
	// handler). Omitted when not applicable.
	propEffectivePermissionApplicable = "effective_permission_applicable"
	// serializer_class is the ViewSet's static serializer_class leaf, applicable
	// to every verb it backs. Omitted when none is statically declared.
	propEffectiveSerializerClass = "serializer_class"
)

// stampDRFEffectiveContract computes and stamps the per-verb EFFECTIVE CONTRACT
// onto a router-expanded route's Properties map (#3835, T5). It is the single
// artifact that prevents the #278 defect class: it merges
//
//   - route provenance + defining_class (#3831, passed in), with
//   - the baseknowledge DRF pack's per-verb defaults (#3832 — default_status,
//     error_statuses incl. the implicit 400-on-invalid, pagination/permission
//     applicability, the mixin that owns the body), and
//   - the ViewSet's static serializer_class (#3835).
//
// kind taxonomy (effective_kind):
//   - provenance explicit    → kind=explicit, source_class=the ViewSet. Status
//     from the pack default for the verb (resolved via the ViewSet's recognised
//     bases) when available — the body-parse override is a follow-up; honest-
//     partial omits status when no curated default exists.
//   - provenance inherited   → kind=inherited, source_class=the defining mixin.
//     Full pack contract (status / error_statuses / behaviour / pagination).
//   - provenance action      → kind=action, source_class=the ViewSet. @action
//     handlers have no framework-default status, so the pack fields are omitted
//     (honest-partial — the status lives in the decorated body).
//   - provenance synthesized → kind=inherited (assumed mixin verb) but, because
//     the base was never resolved, the pack lookup uses crudVerbDefiningMixin so
//     a standard CRUD verb still carries its default; a non-CRUD/ANY route omits
//     the pack fields.
//
// HONEST-PARTIAL: an unknown base / verb the pack does not know yields NO
// pack-derived fields — only what is resolvable (kind, source_class, serializer)
// is stamped. A status is NEVER fabricated.
//
// No-op for the ANY-verb detail catch-all (method == "" and verb ANY): there is
// no single owning verb to contract.
func stampDRFEffectiveContract(props map[string]string, method, provenance, definingClass, viewSet string, bases []string, serializer string) {
	if props == nil {
		return
	}

	// serializer_class applies to every verb the ViewSet backs, independent of
	// the pack (resolvable from the ViewSet body alone).
	if serializer != "" {
		props[propEffectiveSerializerClass] = serializer
	}

	// Map provenance → kind. An ANY catch-all (no method, synthesized) is not a
	// single verb — emit no per-verb contract for it beyond the serializer.
	kind := ""
	switch provenance {
	case drfProvExplicit:
		kind = "explicit"
	case drfProvInherited:
		kind = "inherited"
	case drfProvAction:
		kind = "action"
	case drfProvSynthesized:
		if method == "" {
			// ANY router-default catch-all — no owning verb.
			return
		}
		kind = "inherited"
	default:
		return
	}
	props[propEffectiveKind] = kind
	if definingClass != "" {
		props[propEffectiveSourceClass] = definingClass
	}

	// @action verbs have no framework-default contract — their status lives in
	// the decorated body. Stamp kind + source_class + serializer only.
	if kind == "action" {
		return
	}

	// Resolve the pack member for this verb. For an inherited verb the defining
	// mixin is named directly (definingClass); for an explicit override or a
	// synthesized CRUD default we resolve the mixin from the CRUD dispatch table
	// (crudVerbDefiningMixin) and, failing that, scan the ViewSet's recognised
	// bases. Honest-partial: no match → no pack fields.
	contract, ok := resolveVerbPackContract(method, definingClass, bases)
	if !ok {
		return
	}

	if contract.PermissionApplicable {
		props[propEffectivePermissionApplicable] = "true"
	}
	if contract.DefaultStatus != baseknowledge.StatusUnknown {
		props[propEffectiveStatus] = strconv.Itoa(contract.DefaultStatus)
	}
	if len(contract.ErrorStatuses) > 0 {
		parts := make([]string, len(contract.ErrorStatuses))
		for i, s := range contract.ErrorStatuses {
			parts[i] = strconv.Itoa(s)
		}
		props[propEffectiveErrorStatuses] = strings.Join(parts, ",")
	}
	if contract.Behaviour != "" {
		props[propEffectiveBehaviour] = contract.Behaviour
	}
	// Pagination is part of the verb's contract only when the pack marks the
	// verb paginated AND a pagination posture actually took effect on the route
	// (stampDRFEndpointPosture set paginated=true). Honest-partial: a paginated
	// verb with no configured paginator is not reported as paginated.
	if contract.PaginationApplicable && props["paginated"] == "true" {
		props[propEffectivePagination] = "true"
	}
}

// resolveVerbPackContract looks up the baseknowledge DRF pack contract for a
// CRUD verb. It tries, in order: the named defining class (set for inherited
// verbs), the canonical mixin from crudVerbDefiningMixin (the DRF dispatch
// table — covers explicit overrides and synthesized defaults of a standard CRUD
// verb), then each of the ViewSet's recognised bases (covers a custom mixin the
// pack still knows). Returns false when no pack contract names the verb.
func resolveVerbPackContract(method, definingClass string, bases []string) (baseknowledge.Member, bool) {
	if method == "" {
		return baseknowledge.Member{}, false
	}
	reg := baseknowledge.Default()
	tried := map[string]bool{}
	try := func(base string) (baseknowledge.Member, bool) {
		if base == "" || tried[base] {
			return baseknowledge.Member{}, false
		}
		tried[base] = true
		return reg.Member(base, method)
	}
	if m, ok := try(definingClass); ok {
		return m, true
	}
	if m, ok := try(crudVerbDefiningMixin[method]); ok {
		return m, true
	}
	for _, b := range bases {
		if m, ok := try(b); ok {
			return m, true
		}
	}
	return baseknowledge.Member{}, false
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
	classDeclByte := -1
	for _, m := range drfClassDefRe.FindAllStringSubmatchIndex(src, -1) {
		// m[2:4] -> class name range, m[4:6] -> base list range.
		name := src[m[2]:m[3]]
		if name != viewSetName {
			continue
		}
		classBase = src[m[4]:m[5]]
		classBodyStart = m[1] // end of the `class X(...):` line
		classDeclByte = m[0]  // start of `class X(...)`  (#2677)
		break
	}
	if classBodyStart == -1 {
		return out
	}
	// The class declaration was located and its body is about to be parsed —
	// mark the ViewSet resolved so route provenance can distinguish inherited
	// (real mixin, body read) from synthesized (assumed default, no body) (#3831).
	out.resolved = true

	// 1-based line of the `class X(...)` declaration. Used as the fallback
	// StartLine for inherited CRUD methods (#2677).
	out.classDefLine = lineOfByteOffset(src, classDeclByte)

	// Carve out the class body — from classBodyStart to end-of-file OR
	// to the next top-level `class ` / `def ` (column 0). This bounds
	// our @action scan and the lookup_field scan to the right class.
	classBody := extractClassBody(src, classBodyStart)

	out.crudMethods = classifyViewSetParent(classBase)
	if m := drfLookupFieldRe.FindStringSubmatch(classBody); len(m) >= 2 {
		out.lookupField = m[1]
	}
	// #3864 — capture the ViewSet-level endpoint posture (pagination /
	// permission / authentication / throttle classes) so the expansion pass
	// can stamp it on every generated route. In DRF these class attributes
	// apply to every action the ViewSet exposes.
	out.posture = parseDRFPosture(classBody)
	// #3835 — capture the class-level serializer_class so the per-verb effective
	// contract can surface it. Reuse the same regex the response-shape pass uses
	// (drfClassSerializerClassRe, response_shape_python.go) so the two agree on
	// what counts as a static serializer_class. Honest-partial: a dynamic
	// get_serializer_class() override is not resolved, leaving the field "".
	if m := drfClassSerializerClassRe.FindStringSubmatch(classBody); len(m) >= 2 {
		out.serializerClass = m[1]
	}
	// #3835 — record the recognised base classes (cbv_bases parity) so the
	// effective-contract pass can attribute an inherited verb to the mixin the
	// baseknowledge pack knows, even when the flat crudVerbDefiningMixin table
	// does not name a custom base.
	out.classBases = finalDottedSegments(drfClassNames(classBase))
	// #3933 — resolve per-action permission overrides from a
	// `def get_permissions(self):` body that branches on `self.action`, or from a
	// `permission_classes_by_action = {...}` dict idiom. These attach the right
	// permission to the right route (e.g. POST /x → IsAdminUser, GET /x → AllowAny)
	// instead of the flat union parseDRFPosture stamps. Dynamic / non-literal
	// conditions are left unresolved (honest-partial → flat-union fallback).
	out.actionPermissions, out.actionPermissionPages = parseDRFActionPermissions(classBody)
	// classBody starts at classBodyStart in src; pass that offset so
	// extractActions can compute absolute (src-relative) line numbers for
	// each @action's `def NAME(` line (#2677).
	out.actions = extractActions(classBody, src, classBodyStart)

	// Issue #699c — detect which CRUD methods are explicitly defined in
	// the class body. Only methods with an explicit `def <name>(self` in
	// the body are marked explicit; all others are inherited from a mixin
	// or parent class and need a synthetic SCOPE.Operation entity.
	out.explicitMethods = make(map[string]bool)
	out.methodLines = make(map[string]int)
	for _, m := range drfExplicitMethodRe.FindAllStringSubmatchIndex(classBody, -1) {
		if len(m) >= 4 {
			methodName := classBody[m[2]:m[3]]
			out.explicitMethods[methodName] = true
			// #2677 — capture the 1-based line of `def <method>(` in src so
			// the emitted http_endpoint attributes to it.
			out.methodLines[methodName] = lineOfByteOffset(src, classBodyStart+m[0])
		}
	}

	// Issue #1648 — merge explicitly-defined CRUD methods into crudMethods.
	// A ViewSet whose base class doesn't include a CRUD mixin (e.g. bare
	// `viewsets.ViewSet`) can still expose CRUD verbs by directly defining
	// `def create(self, ...)` / `def list(self, ...)` etc. Previously these
	// classes ended up with an empty crudMethods set, which caused
	// emitOneCRUDFamily to skip every per-verb emission and fall through to
	// the ANY catch-all — producing the verb-collapsed
	// `http:ANY:/api/v1/auth/refresh` entries that defeat verb-aware call→def
	// matching on the consumer side. Merging the explicit set restores the
	// per-verb signal for these classes.
	if out.crudMethods == nil {
		out.crudMethods = map[string]bool{}
	}
	for name := range out.explicitMethods {
		out.crudMethods[name] = true
	}

	// Issue #1648 — honour `http_method_names = [...]`. DRF treats this
	// class-level attribute as an absolute filter: the dispatcher only
	// routes requests whose verb appears in the list, regardless of which
	// CRUD methods the class otherwise defines. Without applying this gate
	// here we would emit phantom verbs (e.g. GET/PUT/PATCH/DELETE on a
	// POST-only viewset) which dilutes the verb signal even though the
	// emission is per-verb.
	if m := drfHTTPMethodNamesRe.FindStringSubmatch(classBody); len(m) >= 2 {
		allowed := parseHTTPMethodNames(m[1])
		if len(allowed) > 0 {
			filtered := map[string]bool{}
			for name := range out.crudMethods {
				if verb, ok := crudMethodToVerb[name]; ok && allowed[verb] {
					filtered[name] = true
				}
			}
			out.crudMethods = filtered
		}
	}

	return out
}

// parseHTTPMethodNames parses the comma-separated argument list of
// `http_method_names = [...]` into a set of UPPERCASE HTTP verbs. Single and
// double-quoted entries are both accepted; whitespace and bare identifiers
// are tolerated.
func parseHTTPMethodNames(args string) map[string]bool {
	out := map[string]bool{}
	for _, raw := range strings.Split(args, ",") {
		s := strings.TrimSpace(raw)
		s = strings.Trim(s, "\"'")
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out[strings.ToUpper(s)] = true
	}
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

	// addVerbs pulls the CRUD verb set the named DRF base contributes from
	// the shared knowledge catalog (internal/frameworks/baseknowledge, DRF
	// pack — the single source of truth #3832) and unions it into `out`.
	// We still drive WHICH bases to look for from the literal base-list
	// text via containsIdent so the whole-identifier matching and the
	// substring-trap guard (ReadOnlyModelViewSet vs ModelViewSet) are
	// preserved exactly; only the verb sets are sourced from the pack
	// instead of being duplicated here.
	addVerbs := func(baseName string) {
		hasKnownBase = true
		for verb := range baseknowledge.Default().MembersOf(baseName) {
			out[verb] = true
		}
	}

	// Check ReadOnlyModelViewSet first since "ModelViewSet" is a substring
	// of it; ordering matters.
	if containsIdent(base, "ReadOnlyModelViewSet") {
		addVerbs("ReadOnlyModelViewSet")
	} else if containsIdent(base, "ModelViewSet") {
		addVerbs("ModelViewSet")
	}
	if containsIdent(base, "ListModelMixin") {
		addVerbs("ListModelMixin")
	}
	if containsIdent(base, "CreateModelMixin") {
		addVerbs("CreateModelMixin")
	}
	if containsIdent(base, "RetrieveModelMixin") {
		addVerbs("RetrieveModelMixin")
	}
	if containsIdent(base, "UpdateModelMixin") {
		addVerbs("UpdateModelMixin")
	}
	if containsIdent(base, "DestroyModelMixin") {
		addVerbs("DestroyModelMixin")
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
	out := map[string]bool{}
	for verb := range baseknowledge.Default().MembersOf("ModelViewSet") {
		out[verb] = true
	}
	return out
}

// extractActions returns every @action / @detail_route / @list_route
// declared inside the given class body.
//
// `src` and `bodyOffsetInSrc` are passed so each returned drfAction can carry
// the 1-based src-relative line number of its `def <method>(` line. Callers
// that don't need line attribution may pass src="" and offset=0; methodLine
// will then be 0 (#2677).
func extractActions(body string, src string, bodyOffsetInSrc int) []drfAction {
	var actions []drfAction
	for _, hit := range scanDecoratorCalls(body, drfActionOpenRe) {
		act := parseActionArgs(hit.args, hit.methodName, false)
		act.methodLine = decoratorLineInSrc(src, bodyOffsetInSrc, hit.methodOffset)
		actions = append(actions, act)
	}
	for _, hit := range scanDecoratorCalls(body, drfLegacyDetailRouteOpenRe) {
		act := parseActionArgs(hit.args, hit.methodName, true)
		// detail_route always means detail=True.
		act.detail = true
		act.methodLine = decoratorLineInSrc(src, bodyOffsetInSrc, hit.methodOffset)
		actions = append(actions, act)
	}
	for _, hit := range scanDecoratorCalls(body, drfLegacyListRouteOpenRe) {
		act := parseActionArgs(hit.args, hit.methodName, false)
		// list_route always means detail=False.
		act.detail = false
		act.methodLine = decoratorLineInSrc(src, bodyOffsetInSrc, hit.methodOffset)
		actions = append(actions, act)
	}
	return actions
}

// decoratorLineInSrc maps a methodOffset returned by scanDecoratorCalls (a
// byte index into `body`) to a 1-based src-relative line number. Returns 0
// when src is empty (line tracking disabled).
func decoratorLineInSrc(src string, bodyOffsetInSrc, methodOffsetInBody int) int {
	if src == "" || methodOffsetInBody < 0 {
		return 0
	}
	return lineOfByteOffset(src, bodyOffsetInSrc+methodOffsetInBody)
}

// decoratorCall is one parsed `@<name>(…)` decorator paired with the
// method declared immediately below it.
type decoratorCall struct {
	args         string // body between the outer `(` and the matching `)`
	methodName   string // name from the `def <name>(` line that follows
	methodOffset int    // byte offset of `def` in the class body (#2677)
}

// scanDecoratorCalls walks `body` for every occurrence of `openRe` (which
// MUST anchor on `@<name>(` — i.e. include the opening paren) and pairs
// each occurrence with the balanced argument list and the trailing
// `def <name>(` line.
//
// The balanced-paren scanner respects Python string-literal boundaries
// (`'…'`, `"…"`, with `\` escapes) so a `url_path='(?P<id>[^/.]+)'`
// argument is consumed atomically rather than terminating the scan at
// the first inner `)`. This is the structural fix for #2669: prior code
// used `@action\s*\(([^)]*)\)…` whose negated-class capture silently
// truncated at the first embedded `)`, dropping every @action whose
// url_path embedded Python named-group regex.
func scanDecoratorCalls(body string, openRe *regexp.Regexp) []decoratorCall {
	var out []decoratorCall
	for _, m := range openRe.FindAllStringIndex(body, -1) {
		openParen := m[1] - 1 // openRe is anchored to include the `(`
		if openParen < 0 || openParen >= len(body) || body[openParen] != '(' {
			continue
		}
		argsStart := openParen + 1
		closeIdx, ok := scanBalancedClose(body, argsStart)
		if !ok {
			continue
		}
		args := body[argsStart:closeIdx]
		afterClose := closeIdx + 1
		// Match the `\s*\n…def NAME(` tail. We re-run a small regex on the
		// suffix so the indentation / extra-decorator tolerance is shared
		// with the pre-#2669 behaviour.
		tail := drfActionPostArgsRe.FindStringSubmatchIndex(body[afterClose:])
		if tail == nil {
			continue
		}
		methodNameStart := afterClose + tail[2]
		methodName := body[methodNameStart : afterClose+tail[3]]
		// methodOffset points at `def` so the caller can compute its src-line
		// for endpoint source-line attribution (#2677). The regex tail's
		// group-0 begins at the whitespace/newline before `def`; scan forward
		// to the `def` token to land on the meaningful character.
		defOffset := afterClose + tail[0]
		for defOffset < methodNameStart && body[defOffset] != 'd' {
			defOffset++
		}
		out = append(out, decoratorCall{
			args:         args,
			methodName:   methodName,
			methodOffset: defOffset,
		})
	}
	return out
}

// lineOfByteOffset returns the 1-based line number of `src[offset]`. Returns
// 1 when offset is out of range so callers always get a valid line for
// downstream Properties storage (#2677).
func lineOfByteOffset(src string, offset int) int {
	if offset < 0 {
		return 1
	}
	if offset > len(src) {
		offset = len(src)
	}
	return 1 + strings.Count(src[:offset], "\n")
}

// scanBalancedClose returns the index of the `)` that balances the
// implicit opening `(` immediately before `start`, and true. Returns
// (0, false) if the close is missing. The scanner is aware of:
//   - Python `'…'` / `"…"` string literals (with `\` escapes inside)
//   - Triple-quoted `”'…”'` / `"""…"""` blocks (commonly used in
//     `serializer_class` defaults but harmless to handle here)
//
// Nested `(` / `)` outside string literals increment / decrement the
// depth counter so a `url_path='(?P<id>[^/.]+)'` argument body is
// traversed without falsely closing at the inner `)`.
func scanBalancedClose(body string, start int) (int, bool) {
	depth := 1
	i := start
	for i < len(body) {
		c := body[i]
		switch c {
		case '\\':
			// Outside a string literal a backslash has no special meaning,
			// but allow it through. (Inside literals the string-literal
			// branches handle escapes themselves.)
			i++
		case '\'', '"':
			// Detect triple-quote first so `"""x"""` doesn't get consumed as
			// three empty strings.
			if i+2 < len(body) && body[i+1] == c && body[i+2] == c {
				end := strings.Index(body[i+3:], string([]byte{c, c, c}))
				if end < 0 {
					return 0, false
				}
				i = i + 3 + end + 3
				continue
			}
			// Single-line string literal — walk until the matching quote,
			// honouring backslash escapes.
			j := i + 1
			for j < len(body) {
				if body[j] == '\\' && j+1 < len(body) {
					j += 2
					continue
				}
				if body[j] == c {
					break
				}
				if body[j] == '\n' {
					// Unterminated string on this line — bail and treat
					// as a malformed decorator.
					return 0, false
				}
				j++
			}
			if j >= len(body) {
				return 0, false
			}
			i = j + 1
		case '(':
			depth++
			i++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
			i++
		default:
			i++
		}
	}
	return 0, false
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
	// #3864 — a DRF @action may override the ViewSet's posture for its own
	// route via `permission_classes=[...]`, `throttle_classes=[...]`,
	// `authentication_classes=[...]`, `pagination_class=X` kwargs. When any of
	// those appears in the decorator we record the override (hasPosture=true)
	// so the expansion pass stamps the action's posture instead of the
	// class-level one. An override that sets `permission_classes=[]` is a real
	// "open this action" declaration and is honoured (hasPosture true, empty
	// permissions).
	if p, ok := parseActionPostureOverride(args); ok {
		act.posture = p
		act.hasPosture = true
	}
	return act
}

// drfActionPaginationClassRe captures `pagination_class=X` (or
// `pagination_class = X`) inside an @action decorator's argument list. Group 1
// is the (possibly dotted) class reference.
var drfActionPaginationClassRe = regexp.MustCompile(
	`pagination_class\s*=\s*([A-Za-z_][\w.]*)`,
)

// parseActionPostureOverride scans an @action decorator argument string for any
// of the posture kwargs (permission_classes / authentication_classes /
// throttle_classes / pagination_class). Returns the resolved posture and true
// when at least one appears, or (zero, false) when the decorator declares no
// posture override at all (the route then inherits the ViewSet posture).
func parseActionPostureOverride(args string) (drfPosture, bool) {
	var p drfPosture
	found := false
	for _, am := range drfClassesAttrRe.FindAllStringSubmatch(args, -1) {
		found = true
		kind := am[1] // permission | authentication | throttle
		names := finalDottedSegments(drfClassNames(am[2]))
		switch kind {
		case "permission":
			p.permissionClasses = names
		case "authentication":
			p.authenticationClasses = names
		case "throttle":
			p.throttleClasses = names
		}
	}
	if m := drfActionPaginationClassRe.FindStringSubmatch(args); len(m) >= 2 {
		found = true
		p.paginationClass = finalDottedSegment(m[1])
	}
	return p, found
}

// parseDRFPosture extracts the ViewSet-level posture from a class body: the
// `pagination_class` assignment and the permission/authentication/throttle
// `*_classes` lists. Only the FIRST assignment of each kind in the body is
// used (DRF allows exactly one). Returns the zero value when nothing is
// declared (honest-partial).
func parseDRFPosture(classBody string) drfPosture {
	var p drfPosture
	seenKind := map[string]bool{}
	for _, am := range drfClassesAttrRe.FindAllStringSubmatch(classBody, -1) {
		kind := am[1]
		if seenKind[kind] {
			continue
		}
		seenKind[kind] = true
		names := finalDottedSegments(drfClassNames(am[2]))
		switch kind {
		case "permission":
			p.permissionClasses = names
		case "authentication":
			p.authenticationClasses = names
		case "throttle":
			p.throttleClasses = names
		}
	}
	if cls, ok := nearestPaginationClass(classBody, 0); ok {
		p.paginationClass = cls
	}
	return p
}

// finalDottedSegment returns the last `.`-separated segment of a dotted ref
// (e.g. "permissions.IsAdminUser" → "IsAdminUser").
func finalDottedSegment(ref string) string {
	if idx := strings.LastIndex(ref, "."); idx >= 0 {
		return ref[idx+1:]
	}
	return ref
}

// finalDottedSegments maps finalDottedSegment over a slice, dropping empties.
func finalDottedSegments(refs []string) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if s := finalDottedSegment(strings.TrimSpace(r)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Django CBV (class-based view) generic-method resolution — Issue #786
// ---------------------------------------------------------------------------
//
// Problem: `path("contracts/", ContractListView.as_view())` in a Django
// urls.py causes the YAML rule engine to emit a Route→View ROUTES_TO edge
// targeting `View:<ClassName>`. The existing ApplyDjangoNestedURLConf pass
// emits a single ANY-verb http_endpoint synthetic for each such route but
// sets NO `source_handler`. The Phase-2 resolver takes the NoHandlerProp
// keep-path and the synthetic entity ends up with zero inbound edges —
// orphaned.
//
// Fix: for every `path("...", SomeView.as_view())` call, classify the CBV
// parent class to determine which HTTP method handlers it exposes (get, post,
// put, patch, delete), emit per-verb http_endpoint synthetics with
// `source_handler = "SCOPE.Operation:ClassName.method"`, and emit synthetic
// SCOPE.Operation entities for inherited handlers (mirroring the
// drf_viewset_implicit_method pattern from #783).

// cbvAsViewRe matches `path("pattern", ClassName.as_view())` or the
// `re_path` variant. Captures:
//
//	group 1 — route pattern string
//	group 2 — view class name (bare identifier only; dotted paths are
//	           normalised to the last component by cbvClassName)
var cbvAsViewRe = regexp.MustCompile(
	`(?:re_)?path\s*\(\s*r?["']([^"']*)["']\s*,\s*([\w.]+)\s*\.\s*as_view\s*\(\s*\)`)

// cbvExplicitMethodRe detects explicit HTTP handler definitions (`def get`,
// `def post`, etc.) in a CBV class body. Used to avoid emitting a synthetic
// when the Python extractor already sees the real method.
var cbvExplicitMethodRe = regexp.MustCompile(
	`\bdef\s+(get|post|put|patch|delete|head|options)\s*\(\s*self`)

// cbvClass describes a Django CBV resolved from disk.
type cbvClass struct {
	// httpMethods is the set of HTTP handler method names this CBV
	// exposes (keys: "get", "post", "put", "patch", "delete").
	httpMethods map[string]bool
	// explicitMethods is the subset of httpMethods defined directly in
	// the class body (the Python extractor will emit a real entity for
	// those; no synthetic needed).
	explicitMethods map[string]bool
}

// cbvClassName extracts the simple class name from a dotted reference like
// `views.ContractListView` → `ContractListView`.
func cbvClassName(ref string) string {
	if idx := strings.LastIndex(ref, "."); idx >= 0 {
		return ref[idx+1:]
	}
	return ref
}

// classifyCBVParent returns the HTTP methods a CBV exposes based on the
// text of its base class list.
//
// Django generic CBV hierarchy (simplified):
//
//	View                            — no default; only what the class defines
//	TemplateView, RedirectView,
//	  TemplateResponseMixin         — GET (head is implicit)
//	ListView, DetailView,
//	  BaseListView, BaseDetailView  — GET
//	CreateView, UpdateView,
//	  BaseCreateView, BaseUpdateView,
//	  ProcessFormView, FormView     — GET + POST
//	DeleteView, BaseDeleteView      — GET + POST (confirm form GET, delete POST)
//
// Custom views (unknown base) default to GET+POST — the most common pair
// and a safe over-approximation that avoids under-extraction.
func classifyCBVParent(base string) map[string]bool {
	out := map[string]bool{}
	hasKnown := false

	// Read-only views (GET only).
	readOnlyBases := []string{
		"TemplateView", "RedirectView", "TemplateResponseMixin",
		"ListView", "BaseListView",
		"DetailView", "BaseDetailView",
		"ArchiveMixin", "YearArchiveView", "MonthArchiveView",
		"WeekArchiveView", "DayArchiveView", "TodayArchiveView",
		"DateDetailView",
	}
	for _, b := range readOnlyBases {
		if containsIdent(base, b) {
			hasKnown = true
			out["get"] = true
		}
	}

	// Read-write views (GET + POST).
	readWriteBases := []string{
		"CreateView", "BaseCreateView",
		"UpdateView", "BaseUpdateView",
		"DeleteView", "BaseDeleteView",
		"FormView", "ProcessFormView",
		"LoginView", "LogoutView",
		"PasswordChangeView", "PasswordResetView",
	}
	for _, b := range readWriteBases {
		if containsIdent(base, b) {
			hasKnown = true
			out["get"] = true
			out["post"] = true
		}
	}

	// Generic `View` base — no default handlers (only explicit methods matter).
	if containsIdent(base, "View") && !hasKnown {
		// Covered by the explicit-method scan; return empty so only
		// real explicit methods get entities. Returning hasKnown=true
		// prevents the unknown-fallback below from firing.
		hasKnown = true
	}

	if !hasKnown {
		// Unknown base — safe fallback: GET + POST covers the vast
		// majority of real-world CBVs.
		out["get"] = true
		out["post"] = true
	}
	return out
}

// parseCBVClass scans `src` for `class <viewName>(bases):` and extracts its
// HTTP method support set + explicit method set.
func parseCBVClass(src, viewName string) cbvClass {
	var out cbvClass

	classBodyStart := -1
	classBase := ""
	for _, m := range drfClassDefRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if name != viewName {
			continue
		}
		classBase = src[m[4]:m[5]]
		classBodyStart = m[1]
		break
	}
	if classBodyStart == -1 {
		return out
	}

	classBody := extractClassBody(src, classBodyStart)
	out.httpMethods = classifyCBVParent(classBase)

	// Also pick up HTTP methods that are explicitly defined in the class
	// body — these are included in httpMethods but marked explicit so we
	// skip synthetic emission (the Python extractor already emitted them).
	out.explicitMethods = make(map[string]bool)
	for _, m := range cbvExplicitMethodRe.FindAllStringSubmatch(classBody, -1) {
		if len(m) >= 2 {
			method := m[1]
			out.httpMethods[method] = true // explicit → also in httpMethods
			out.explicitMethods[method] = true
		}
	}
	return out
}

// buildCBVFileIndex scans all Python files for `class FooView(...):`
// declarations (CBVs) and returns a map of class name → repo-relative file.
// Used as a fallback when an import statement cannot pinpoint the module.
func buildCBVFileIndex(
	parentFiles []string,
	fileReader NestedURLConfFileReader,
) map[string]string {
	out := map[string]string{}
	for _, relPath := range parentFiles {
		if !strings.HasSuffix(relPath, ".py") {
			continue
		}
		content := fileReader(relPath)
		if len(content) == 0 {
			continue
		}
		s := string(content)
		// Cheap pre-filter: only files containing view-related keywords.
		if !strings.Contains(s, "View") && !strings.Contains(s, "as_view") {
			continue
		}
		for _, m := range drfClassDefRe.FindAllStringSubmatch(s, -1) {
			name := m[1]
			base := m[2]
			// Require the class to look like a CBV: name ends in "View"
			// or base contains a known Django generic.
			isCBV := strings.HasSuffix(name, "View") ||
				containsIdent(base, "View") ||
				containsIdent(base, "TemplateView") ||
				containsIdent(base, "ListView") ||
				containsIdent(base, "DetailView") ||
				containsIdent(base, "CreateView") ||
				containsIdent(base, "UpdateView") ||
				containsIdent(base, "DeleteView") ||
				containsIdent(base, "FormView")
			if !isCBV {
				continue
			}
			if _, exists := out[name]; !exists {
				out[name] = relPath
			}
		}
	}
	return out
}

// ApplyDjangoCBVRoutes resolves Django class-based view (CBV) routes of the
// form `path("pattern", SomeView.as_view())` into per-verb http_endpoint
// synthetics and (for inherited handlers) synthetic SCOPE.Operation entities.
//
// Mirrors the drf_viewset_implicit_method approach from #783 but for the
// Django generic CBV hierarchy (TemplateView, ListView, DetailView,
// CreateView, etc.).
//
// parentFiles: repo-relative Python file paths.
// fileReader:  resolves a repo-relative path to file bytes.
func ApplyDjangoCBVRoutes(
	parentFiles []string,
	fileReader NestedURLConfFileReader,
) []types.EntityRecord {
	if fileReader == nil {
		return nil
	}

	// Build a global index of CBV class name → file path for import-free
	// fallback resolution.
	cbvFiles := buildCBVFileIndex(parentFiles, fileReader)

	var out []types.EntityRecord
	seenEndpoints := map[string]bool{}
	seenMethods := map[string]bool{}

	for _, relPath := range parentFiles {
		if !isDjangoURLFile(relPath) {
			continue
		}
		content := fileReader(relPath)
		if len(content) == 0 {
			continue
		}
		src := string(content)

		// Flatten parenthesised newlines so multi-line path() calls match.
		flat := flattenParenthesised(src)

		// Resolve import map so bare class names can be traced to their
		// defining module.
		importMap := parseImports(src)

		// Collect parent include() prefixes for this file. Same fix as #800
		// for DRF routes: only emit at bare prefix when the file is NOT
		// reached via a parent include(). Emitting at both bare and prefixed
		// paths produces duplicate http_endpoint entities.
		parentPrefixes := findParentIncludePrefixes(relPath, parentFiles, fileReader)
		if len(parentPrefixes) == 0 {
			parentPrefixes = []string{""}
		}

		for _, m := range cbvAsViewRe.FindAllStringSubmatch(flat, -1) {
			rawPattern := m[1]
			viewRef := m[2]
			viewName := cbvClassName(viewRef)

			// Resolve the CBV class to its source file.
			viewFile := resolveViewSetFile(viewName, importMap, cbvFiles, relPath)
			var vc cbvClass
			if viewFile != "" {
				if fc := fileReader(viewFile); len(fc) > 0 {
					vc = parseCBVClass(string(fc), viewName)
				}
			}
			if vc.httpMethods == nil {
				// Class not found — try a local scan of the urls file itself
				// (some projects define simple CBVs inline).
				vc = parseCBVClass(src, viewName)
			}
			if vc.httpMethods == nil {
				// Final fallback: assume GET + POST.
				vc.httpMethods = map[string]bool{"get": true, "post": true}
			}
			if vc.explicitMethods == nil {
				vc.explicitMethods = map[string]bool{}
			}

			// Emit synthetic SCOPE.Operation entities for inherited methods
			// (not explicitly defined in the class body).
			handlerFile := viewFile
			if handlerFile == "" {
				handlerFile = relPath
			}
			emitCBVMethodEntities(&out, seenMethods, viewName, vc, handlerFile)

			// Emit per-verb http_endpoint synthetics for all parent prefixes.
			for _, pp := range parentPrefixes {
				composed := joinDjangoRoutePaths(pp, rawPattern)
				canonical := canonicalDjango(composed)
				if canonical == "" || canonical == "/" {
					continue
				}

				for verb := range vc.httpMethods {
					upperVerb := strings.ToUpper(verb)
					id := httproutes.SyntheticID(upperVerb, canonical)
					if seenEndpoints[id] {
						continue
					}
					seenEndpoints[id] = true

					qualifiedMethod := viewName + "." + verb
					out = append(out, types.EntityRecord{
						ID:                 id,
						Name:               id,
						Kind:               httpEndpointKind,
						SourceFile:         relPath,
						Language:           "python",
						EnrichmentRequired: false,
						EnrichmentStatus:   types.StatusPending,
						QualityScore:       0.8,
						Properties: map[string]string{
							"verb":           upperVerb,
							"path":           canonical,
							"framework":      "django",
							"pattern_type":   "django_cbv_route",
							"cbv_class":      viewName,
							"source_handler": "SCOPE.Operation:" + qualifiedMethod,
						},
					})
				}
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// DRF ViewSet.as_view({method_map}) routes — Issue #2614
// ---------------------------------------------------------------------------
//
// Problem: DRF ViewSets can be mounted without a router via the explicit
// method-map form:
//
//	_list = NotificationViewSet.as_view({'get': 'list', 'delete': 'delete_all'})
//	urlpatterns = [
//	    path("notifications/", _list, name="notifications-list"),
//	]
//
// Or inline:
//
//	path("notifications/", NotificationViewSet.as_view({'get': 'list'}))
//
// ApplyDjangoDRFRoutes only processes files that contain router.register()
// calls — it never sees the urlpatterns lines above. ApplyDjangoCBVRoutes
// uses cbvAsViewRe which requires as_view() with NO arguments and thus
// misses the dict form. ApplyDjangoNestedURLConf emits a single ANY entity
// for the path but cannot derive the per-verb signal from the method map.
//
// Fix: scan every Django URL file and router file for the two patterns and
// emit per-verb http_endpoint synthetics with source_handler set to the
// ViewSet action method. Refs #2614.

// drfViewSetAsViewAssignRe matches variable assignment of the form:
//
//	_name = ViewSet.as_view({'get': 'list', 'delete': 'delete_all'})
//	_name = views.ViewSet.as_view({"get": "list"})
//
// Captures group 1 = variable name, group 2 = ViewSet class name (last
// segment of dotted path), group 3 = the raw dict body inside { }.
var drfViewSetAsViewAssignRe = regexp.MustCompile(
	`(\w+)\s*=\s*(?:[\w.]+\.)?(\w+)\s*\.\s*as_view\s*\(\s*\{([^}]+)\}`)

// drfViewSetAsViewInlineRe matches a path() call with an inline as_view({dict}):
//
//	path("notifications/", NotificationViewSet.as_view({'get': 'list'}))
//
// Captures group 1 = route pattern, group 2 = ViewSet class name (last
// segment), group 3 = raw dict body.
var drfViewSetAsViewInlineRe = regexp.MustCompile(
	`(?:re_)?path\s*\(\s*r?["']([^"']*)["']\s*,\s*(?:[\w.]+\.)?(\w+)\s*\.\s*as_view\s*\(\s*\{([^}]+)\}`)

// drfViewSetAsViewPathVarRe matches a path() whose handler is a bare
// identifier (the variable assigned by drfViewSetAsViewAssignRe):
//
//	path("notifications/", _notification_list, name="...")
//
// Captures group 1 = route pattern, group 2 = handler variable name.
var drfViewSetAsViewPathVarRe = regexp.MustCompile(
	`(?:re_)?path\s*\(\s*r?["']([^"']*)["']\s*,\s*(_\w+)`)

// drfMethodMapKVRe extracts key-value pairs from a Python dict literal of
// the form 'verb': 'action' or "verb": "action".
var drfMethodMapKVRe = regexp.MustCompile(
	`["'](\w+)['"]\s*:\s*["'](\w+)["']`)

// parseViewSetMethodMap parses the raw body of a {verb: action, ...} dict
// from a ViewSet.as_view() call and returns the map of uppercase HTTP verb
// → ViewSet action name.
func parseViewSetMethodMap(body string) map[string]string {
	out := map[string]string{}
	for _, m := range drfMethodMapKVRe.FindAllStringSubmatch(body, -1) {
		verb := strings.ToUpper(m[1])
		action := m[2]
		out[verb] = action
	}
	return out
}

// ApplyDjangoViewSetAsViewRoutes handles the DRF ViewSet.as_view({dict})
// mounting pattern that appears outside of router.register() — either as a
// pre-bound variable reused in urlpatterns, or as an inline as_view() call
// directly inside a path(). For each such binding it emits one per-verb
// http_endpoint synthetic carrying source_handler and, when the binding is
// reached via a parent include() chain, the url_prefix property.
//
// parentFiles: repo-relative Python file paths.
// fileReader:  resolves a repo-relative path to raw bytes.
func ApplyDjangoViewSetAsViewRoutes(
	parentFiles []string,
	fileReader NestedURLConfFileReader,
) []types.EntityRecord {
	if fileReader == nil {
		return nil
	}

	var out []types.EntityRecord
	seen := map[string]bool{}

	for _, relPath := range parentFiles {
		if !isDjangoURLFile(relPath) && !isDjangoRoutersFile(relPath) {
			continue
		}
		content := fileReader(relPath)
		if len(content) == 0 {
			continue
		}
		src := string(content)

		// Step 1 — collect variable assignments:
		//   _notification_list = views.NotificationViewSet.as_view({'get': 'list'})
		// Map: varName → {HTTP_VERB: action_name, ViewSet: className}
		type varBinding struct {
			viewSetName string
			methodMap   map[string]string
		}
		varBindings := map[string]varBinding{}
		for _, m := range drfViewSetAsViewAssignRe.FindAllStringSubmatch(src, -1) {
			varName := m[1]
			viewSetName := m[2]
			dictBody := m[3]
			methodMap := parseViewSetMethodMap(dictBody)
			if len(methodMap) > 0 {
				varBindings[varName] = varBinding{viewSetName: viewSetName, methodMap: methodMap}
			}
		}

		// Determine parent include() prefixes for prefix composition.
		parentPrefixes := findParentIncludePrefixes(relPath, parentFiles, fileReader)
		if len(parentPrefixes) == 0 {
			parentPrefixes = []string{""}
		}

		// Also apply local path("prefix", include(routerVar.urls)) prefixes.
		flat := flattenParenthesised(src)
		localRouterPrefixes := buildLocalRouterPrefixes(flat)
		// For router files, the local prefix is on the router var; for url
		// files with explicit urlpatterns, there's typically no router var.
		// Apply the first available local prefix as a general URL mount prefix.
		var localMount string
		for _, p := range localRouterPrefixes {
			localMount = p
			break
		}
		if localMount != "" && len(parentPrefixes) == 1 && parentPrefixes[0] == "" {
			parentPrefixes = []string{localMount}
		}

		emitForBinding := func(rawPattern, viewSetName string, methodMap map[string]string) {
			for _, pp := range parentPrefixes {
				composed := joinDjangoRoutePaths(pp, rawPattern)
				canonical := canonicalDjango(composed)
				if canonical == "" || canonical == "/" {
					continue
				}
				urlPrefix := ""
				if pp != "" {
					urlPrefix = "/" + strings.Trim(pp, "/")
				}
				for httpVerb, actionName := range methodMap {
					id := httproutes.SyntheticID(httpVerb, canonical)
					if seen[id] {
						continue
					}
					seen[id] = true
					qualifiedHandler := viewSetName + "." + actionName
					props := map[string]string{
						"verb":           httpVerb,
						"path":           canonical,
						"framework":      "django",
						"pattern_type":   "drf_viewset_asview_route",
						"drf_view_class": viewSetName,
						"source_handler": "SCOPE.Operation:" + qualifiedHandler,
					}
					if urlPrefix != "" {
						props["url_prefix"] = urlPrefix
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

		// Step 2a — inline as_view({dict}) directly inside path():
		//   path("notifications/", NotificationViewSet.as_view({'get': 'list'}))
		for _, m := range drfViewSetAsViewInlineRe.FindAllStringSubmatch(flat, -1) {
			rawPattern := m[1]
			viewSetName := m[2]
			dictBody := m[3]
			methodMap := parseViewSetMethodMap(dictBody)
			if len(methodMap) > 0 {
				emitForBinding(rawPattern, viewSetName, methodMap)
			}
		}

		// Step 2b — pre-bound variable referenced in path():
		//   path("notifications/", _notification_list, ...)
		if len(varBindings) > 0 {
			for _, m := range drfViewSetAsViewPathVarRe.FindAllStringSubmatch(flat, -1) {
				rawPattern := m[1]
				varName := m[2]
				if binding, ok := varBindings[varName]; ok {
					emitForBinding(rawPattern, binding.viewSetName, binding.methodMap)
				}
			}
		}
	}
	return out
}

// emitCBVMethodEntities emits synthetic SCOPE.Operation entities for each
// HTTP handler method that the CBV exposes via inheritance but does NOT
// explicitly define. Mirrors emitViewSetMethodEntities from #783.
func emitCBVMethodEntities(
	out *[]types.EntityRecord,
	seenMethods map[string]bool,
	viewName string,
	vc cbvClass,
	sourceFile string,
) {
	for method := range vc.httpMethods {
		if vc.explicitMethods[method] {
			// Python extractor already emitted a real entity — skip.
			continue
		}
		key := sourceFile + "\x00" + viewName + "." + method
		if seenMethods[key] {
			continue
		}
		seenMethods[key] = true
		qualifiedName := viewName + "." + method
		*out = append(*out, types.EntityRecord{
			Name:               qualifiedName,
			Kind:               "SCOPE.Operation",
			Subtype:            "method",
			Language:           "python",
			SourceFile:         sourceFile,
			Signature:          "def " + method + "(self, request, *args, **kwargs)",
			EnrichmentRequired: false,
			QualityScore:       0.7,
			Properties: map[string]string{
				"pattern_type":      "django_cbv_implicit_method",
				"cbv_class":         viewName,
				"inherited_from":    "django.views",
				"cbv_method_origin": method,
			},
		})
	}
}
