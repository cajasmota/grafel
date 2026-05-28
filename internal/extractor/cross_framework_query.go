package extractor

// cross_framework_query.go — issue #2910.
//
// Cross-framework reuse of the React-ecosystem TanStack Query + Redux idioms.
// TanStack Query and Redux/RTK are NOT React-only:
//
//   - TanStack Query ships first-party framework adapters whose hook/factory
//     shapes differ per framework:
//     React   useQuery / useMutation / useInfiniteQuery / useQueryClient
//     Vue     useQuery / useMutation / useInfiniteQuery  (@tanstack/vue-query)
//     Svelte  createQuery / createMutation / createInfiniteQuery (@tanstack/svelte-query)
//     Angular injectQuery / injectMutation / injectInfiniteQuery
//     (@tanstack/angular-query-experimental)
//   - Redux Toolkit (configureStore / createSlice / createApi / createAsyncThunk)
//     is framework-agnostic and used with Vue, Svelte and Angular. Angular's own
//     idiomatic store is NgRx (createReducer / createAction / createEffect /
//     createFeatureSelector / Store.select / Store.dispatch).
//
// The React extractor (internal/extractors/javascript/react_ecosystem.go) parses
// these with tree-sitter on .ts/.tsx files (Angular is .ts, so RTK/NgRx/TanStack
// inject-* there is already reachable by that AST pass). Vue and Svelte are
// Single-File-Component formats whose <script> blocks are scanned with the
// regex/string passes in their own extractors; this file gives those two
// regex-based extractors a small, dependency-free detector for the
// cross-framework TanStack Query + RTK idioms, mirroring the decorate-only
// discipline (#2839): every match is emitted as a decorated SCOPE.Operation
// (no new EntityKind), keyed on the import package so it no-ops unless a real
// TanStack/Redux adapter is imported.
//
// The detector is intentionally framework-parameterised (lang/framework strings)
// so the Vue and Svelte extractors share one implementation rather than each
// reimplementing the call-shape recognition the issue called out.

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// CrossFrameworkQueryHit is one detected TanStack Query / Redux-RTK call site in
// a Vue/Svelte <script> block, ready to be turned into a decorated
// SCOPE.Operation by the caller.
type CrossFrameworkQueryHit struct {
	// Name is the entity name (unique within the file/component).
	Name string
	// Subtype is the decorated SCOPE.Operation subtype, e.g.
	// "tanstack_query" / "redux_slice" / "rtk_query_api".
	Subtype string
	// Via is the provenance marker ("tanstack_query" | "redux").
	Via string
	// Call is the literal call leaf that matched (useQuery, createSlice, …).
	Call string
	// Offset is the byte offset of the match within the scanned script source
	// (the caller adds its script-block offset to compute the line number).
	Offset int
	// Extra holds detector-specific stamps (e.g. query_kind, store_lib).
	Extra map[string]string
}

// crossFrameworkImports records which cross-framework query/store packages the
// scanned <script> block imports. A detector cell only fires when its gating
// package is present, so a Vue component that merely declares a function named
// useQuery does not get mis-decorated.
type crossFrameworkImports struct {
	hasTanstack bool // @tanstack/{vue,svelte,angular}-query (any adapter)
	hasRTK      bool // @reduxjs/toolkit (framework-agnostic)
}

var reCrossImport = regexp.MustCompile(`(?m)^\s*import\s+[^;]*?\bfrom\s+['"]([^'"]+)['"]`)

// isCrossTanstackQueryPkg reports whether p is a TanStack Query package — any
// framework adapter (react/vue/svelte/solid/angular) or the legacy react-query.
// Kept byte-compatible with javascript.isTanstackQueryPkg so the two stay in
// sync; duplicated (not imported) to avoid an import cycle between the shared
// extractor package and the javascript extractor.
func isCrossTanstackQueryPkg(p string) bool {
	return p == "react-query" ||
		p == "@tanstack/react-query" ||
		(strings.HasPrefix(p, "@tanstack/") && strings.HasSuffix(p, "-query")) ||
		strings.HasPrefix(p, "@tanstack/query") ||
		strings.HasPrefix(p, "@tanstack/angular-query")
}

// isCrossReduxPkg reports whether p is a Redux-family package (framework-agnostic).
func isCrossReduxPkg(p string) bool {
	return p == "redux" || p == "@reduxjs/toolkit" || strings.HasPrefix(p, "@reduxjs/toolkit/")
}

// scanCrossFrameworkImports collects the gating packages imported in script.
func scanCrossFrameworkImports(script string) crossFrameworkImports {
	var imp crossFrameworkImports
	for _, m := range reCrossImport.FindAllStringSubmatch(script, -1) {
		p := m[1]
		switch {
		case isCrossTanstackQueryPkg(p):
			imp.hasTanstack = true
		case isCrossReduxPkg(p):
			imp.hasRTK = true
		}
	}
	return imp
}

// tanstackCallKind maps a cross-framework TanStack Query call leaf to its
// canonical query kind, or "" if the leaf is not a TanStack Query entry point.
// Covers the React (use*), Svelte (create*) and Angular (inject*) adapters; Vue
// uses the same use* names as React.
func tanstackCallKind(leaf string) string {
	switch leaf {
	case "useQuery", "createQuery", "injectQuery":
		return "query"
	case "useMutation", "createMutation", "injectMutation":
		return "mutation"
	case "useInfiniteQuery", "createInfiniteQuery", "injectInfiniteQuery":
		return "infinite_query"
	case "useQueries", "createQueries", "injectQueries":
		return "queries"
	case "useQueryClient", "injectQueryClient", "QueryClient":
		return "query_client"
	}
	return ""
}

// reTanstackCall matches a cross-framework TanStack Query entry-point call.
var reTanstackCall = regexp.MustCompile(`\b(useQuery|useMutation|useInfiniteQuery|useQueries|useQueryClient|createQuery|createMutation|createInfiniteQuery|createQueries|injectQuery|injectMutation|injectInfiniteQuery|injectQueries|injectQueryClient)\s*\(`)

// reRTKFactory matches a Redux-Toolkit factory call.
var reRTKFactory = regexp.MustCompile(`\b(configureStore|createSlice|createApi|createAsyncThunk|createEntityAdapter|createReducer|createStore|combineReducers)\s*\(`)

// rtkFactorySubtype maps an RTK factory leaf to its decorated subtype.
func rtkFactorySubtype(leaf string) string {
	switch leaf {
	case "createSlice":
		return "redux_slice"
	case "configureStore", "createStore":
		return "redux_store"
	case "createApi":
		return "rtk_query_api"
	case "createAsyncThunk":
		return "redux_async_thunk"
	case "createEntityAdapter":
		return "entity_adapter"
	case "createReducer", "combineReducers":
		return "redux_reducer"
	}
	return ""
}

// DetectCrossFrameworkQuery scans a Vue/Svelte <script> block for the
// cross-framework TanStack Query + Redux/RTK idioms and returns the detected
// hits. It is a no-op (nil) when none of the gating packages are imported, so
// non-ecosystem components are untouched. The caller turns each hit into a
// decorated SCOPE.Operation entity with a CONTAINS/USES edge from the component.
//
// NgRx is intentionally NOT scanned here: NgRx is Angular-only, and Angular
// components are .ts files handled by the tree-sitter javascript extractor, not
// by the regex-based Vue/Svelte passes that call this function. Exposing the
// NgRx matcher here would only ever produce dead matches in .vue/.svelte files.
func DetectCrossFrameworkQuery(script string) []CrossFrameworkQueryHit {
	imp := scanCrossFrameworkImports(script)
	if !imp.hasTanstack && !imp.hasRTK {
		return nil
	}
	var hits []CrossFrameworkQueryHit
	seen := map[string]bool{}
	add := func(name, subtype, via, call string, off int, extra map[string]string) {
		if name == "" || seen[subtype+":"+name] {
			return
		}
		seen[subtype+":"+name] = true
		hits = append(hits, CrossFrameworkQueryHit{
			Name: name, Subtype: subtype, Via: via, Call: call, Offset: off, Extra: extra,
		})
	}

	if imp.hasTanstack {
		for _, m := range reTanstackCall.FindAllStringSubmatchIndex(script, -1) {
			leaf := script[m[2]:m[3]]
			kind := tanstackCallKind(leaf)
			if kind == "" {
				continue
			}
			name := "tanstack:" + leaf
			add(name, "tanstack_query", "tanstack_query", leaf, m[0], map[string]string{
				"query_kind": kind,
				"query_call": leaf,
			})
		}
	}

	if imp.hasRTK {
		for _, m := range reRTKFactory.FindAllStringSubmatchIndex(script, -1) {
			leaf := script[m[2]:m[3]]
			subtype := rtkFactorySubtype(leaf)
			if subtype == "" {
				continue
			}
			name := "rtk:" + leaf
			add(name, subtype, "redux", leaf, m[0], map[string]string{
				"redux_factory": leaf,
			})
		}
	}

	return hits
}

// BuildCrossFrameworkQueryEntities turns DetectCrossFrameworkQuery hits into
// decorated SCOPE.Operation entities plus a CONTAINS edge from the component to
// each, for the given language ("vue"|"svelte"). lineFor maps a byte offset in
// the script source to a 1-based source line. Returns (entities, componentRels).
func BuildCrossFrameworkQueryEntities(hits []CrossFrameworkQueryHit, lang, filePath, componentName string, lineFor func(off int) int) ([]types.EntityRecord, []types.RelationshipRecord) {
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	for _, h := range hits {
		ln := 1
		if lineFor != nil {
			ln = lineFor(h.Offset)
		}
		props := map[string]string{
			"component": componentName,
			"framework": lang,
			"via":       h.Via,
			"call":      h.Call,
		}
		for k, v := range h.Extra {
			props[k] = v
		}
		ents = append(ents, types.EntityRecord{
			Name:             h.Name,
			QualifiedName:    componentName + "." + h.Name,
			Kind:             "SCOPE.Operation",
			Subtype:          h.Subtype,
			SourceFile:       filePath,
			Language:         lang,
			StartLine:        ln,
			EndLine:          ln,
			Signature:        h.Call + "(…)",
			QualityScore:     0.8,
			EnrichmentStatus: types.StatusPending,
			Properties:       props,
		})
		rels = append(rels, types.RelationshipRecord{
			ToID: h.Name,
			Kind: "CONTAINS",
			Properties: map[string]string{
				"component": componentName,
				"framework": lang,
				"subtype":   h.Subtype,
				"via":       h.Via,
			},
		})
	}
	return ents, rels
}
