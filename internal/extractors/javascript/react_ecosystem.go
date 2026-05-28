// react_ecosystem.go — issue #2894 PR1.
//
// React Ecosystem framework_specific group: first-class extraction of the two
// dominant real-world React/React-Native state + data idioms that no generic
// capability column expresses on its own:
//
//   redux_store_extraction
//     - Redux:         createStore / combineReducers, connect(mapState,mapDispatch)
//     - Redux Toolkit: configureStore, createSlice (→ reducers + actions),
//                      createEntityAdapter
//     - react-redux:   useSelector / useDispatch / useStore (decorated as
//                      USES_HOOK by the generic hook pass; here we additionally
//                      decorate the slice/store entities)
//
//   redux_async_flow
//     - Redux Toolkit: createAsyncThunk
//     - Redux-Saga:    takeEvery / takeLatest watcher effects + put/call/select
//     - Redux-Observable: epics (combineEpics / *Epic functions using ofType)
//     - redux-thunk:   thunk action creators (functions returning (dispatch)=>)
//
//   rtk_query_extraction
//     - RTK Query:     createApi / injectEndpoints → endpoint entities. Endpoints
//                      are cross-repo-HTTP-linkable like the backend endpoints;
//                      we stamp the http-ish metadata so the link pass can pick
//                      them up later (the query/mutation string is the path).
//
//   tanstack_query_extraction
//     - TanStack/React Query: useQuery / useMutation / useInfiniteQuery +
//                      QueryClient + queryKey (+ cache invalidation via
//                      invalidateQueries). HIGH priority — the dominant data layer.
//
// Discipline (#2839 prefer-decorate): no new EntityKind / RelationshipKind is
// introduced. Slices/stores/apis are emitted as SCOPE.Component subtype="…"
// decorated entities (mirroring the context-factory convention in
// handleVariableDeclarator); slice reducers/actions and RTK-Query/TanStack
// endpoints are emitted as SCOPE.Operation subtype="…" with a CONTAINS edge
// from the owning slice/api so traces can descend into them — the exact shape
// zustand_store.go uses for store actions (#2626).
//
// The detectors key on the import package (robust against renames) AND fall
// back to the call-callee name so files that re-export the factory still fire.
// Built reusably (TanStack Query + Redux are not React-only); Vue/Svelte/
// Angular wiring is a deferred follow-up (see issue #2894).
package javascript

import (
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
	sitter "github.com/smacker/go-tree-sitter"
)

// Property `via` values stamped on React-Ecosystem entities/edges so the
// resolver and coverage tooling can classify provenance.
const (
	propViaReduxSlice    = "redux_slice"
	propViaReduxStore    = "redux_store"
	propViaReduxAsync    = "redux_async"
	propViaRTKQuery      = "rtk_query"
	propViaTanstackQuery = "tanstack_query" // stamped on TanStack-Query USES_HOOK edges
)

// reactEcosystemImports records which ecosystem packages are imported in the
// current file, keyed by the local name bound to each relevant factory/hook.
// A package is "present" if any binding originates from it; the per-name maps
// let the detectors confirm a call leaf resolves to the right factory even
// when it is aliased (`import { createSlice as slice }`).
type reactEcosystemImports struct {
	// factory local-name → canonical factory name (createSlice, configureStore,
	// createStore, combineReducers, createApi, createAsyncThunk,
	// createEntityAdapter, QueryClient).
	factories map[string]string
	// hook local-name → canonical hook name (useQuery, useMutation,
	// useInfiniteQuery, useQueryClient, useSelector, useDispatch, useStore).
	hooks map[string]string
	// saga effect local-name → canonical effect (takeEvery, takeLatest, put,
	// call, select, fork, all).
	sagaEffects map[string]string

	hasRedux    bool // redux or @reduxjs/toolkit
	hasReactRdx bool // react-redux
	hasRTKQuery bool // @reduxjs/toolkit/query
	hasTanstack bool // @tanstack/*-query or react-query
	hasSaga     bool // redux-saga
}

// canonical factory names we track.
var reactEcosystemFactories = map[string]bool{
	"createStore": true, "combineReducers": true, "configureStore": true,
	"createSlice": true, "createAsyncThunk": true, "createEntityAdapter": true,
	"createApi": true, "QueryClient": true,
}

// canonical hook names we track (data + redux state hooks).
var reactEcosystemHooks = map[string]bool{
	"useQuery": true, "useMutation": true, "useInfiniteQuery": true,
	"useQueryClient": true, "useSuspenseQuery": true,
	"useSelector": true, "useDispatch": true, "useStore": true,
}

// canonical redux-saga effect names.
var reactSagaEffects = map[string]bool{
	"takeEvery": true, "takeLatest": true, "takeLeading": true,
	"put": true, "call": true, "select": true, "fork": true, "all": true,
}

// isReduxPkg reports whether the import path is a Redux-family package.
func isReduxPkg(p string) bool {
	return p == "redux" || p == "@reduxjs/toolkit" ||
		strings.HasPrefix(p, "@reduxjs/toolkit/")
}

// isTanstackQueryPkg reports whether the import path is a TanStack/React Query
// package (react-query legacy or any framework adapter under @tanstack).
func isTanstackQueryPkg(p string) bool {
	return p == "react-query" ||
		p == "@tanstack/react-query" ||
		(strings.HasPrefix(p, "@tanstack/") && strings.HasSuffix(p, "-query")) ||
		strings.HasPrefix(p, "@tanstack/query")
}

// buildReactEcosystemImports scans importByLocal for the ecosystem packages.
// Returns nil when none are present (fast-path for non-ecosystem files).
func (x *extractor) buildReactEcosystemImports() *reactEcosystemImports {
	if len(x.importByLocal) == 0 {
		return nil
	}
	r := &reactEcosystemImports{
		factories:   map[string]string{},
		hooks:       map[string]string{},
		sagaEffects: map[string]string{},
	}
	any := false
	for local, b := range x.importByLocal {
		if b == nil {
			continue
		}
		p := b.importPath
		imp := b.importedName
		switch {
		case isReduxPkg(p):
			r.hasRedux = true
			any = true
			if reactEcosystemFactories[imp] {
				r.factories[local] = imp
			}
			if strings.HasPrefix(p, "@reduxjs/toolkit/query") {
				r.hasRTKQuery = true
			}
		case p == "react-redux":
			r.hasReactRdx = true
			any = true
			if reactEcosystemHooks[imp] {
				r.hooks[local] = imp
			}
		case isTanstackQueryPkg(p):
			r.hasTanstack = true
			any = true
			if reactEcosystemFactories[imp] {
				r.factories[local] = imp
			}
			if reactEcosystemHooks[imp] {
				r.hooks[local] = imp
			}
		case p == "redux-saga" || strings.HasPrefix(p, "redux-saga/"):
			r.hasSaga = true
			any = true
			if reactSagaEffects[imp] {
				r.sagaEffects[local] = imp
			}
		case p == "redux-observable":
			any = true // epics handled by name fallback
		}
	}
	if !any {
		return nil
	}
	return r
}

// resolveFactory returns the canonical factory name a call's callee resolves to
// (via import binding when available, falling back to the bare leaf name so
// re-exported / namespace-accessed factories still match). Returns "".
func (r *reactEcosystemImports) resolveFactory(x *extractor, call *sitter.Node) string {
	leaf := factoryLeaf(x, call)
	if leaf == "" {
		return ""
	}
	if canon, ok := r.factories[leaf]; ok {
		return canon
	}
	// Fallback: the leaf itself is a canonical factory name (handles
	// `import * as RTK from '@reduxjs/toolkit'; RTK.createSlice(...)`).
	if reactEcosystemFactories[leaf] {
		return leaf
	}
	return ""
}

// factoryLeaf returns the leaf identifier of a call/new callee: bare identifier
// or the .property of a member expression. "" when not reducible.
func factoryLeaf(x *extractor, n *sitter.Node) string {
	if n == nil {
		return ""
	}
	var fn *sitter.Node
	switch n.Type() {
	case "call_expression", "new_expression":
		fn = n.ChildByFieldName("function")
		if fn == nil { // new_expression uses "constructor" in some grammars
			fn = n.ChildByFieldName("constructor")
		}
	default:
		fn = n
	}
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier", "type_identifier", "property_identifier":
		return x.nodeText(fn)
	case "member_expression":
		if prop := fn.ChildByFieldName("property"); prop != nil {
			return x.nodeText(prop)
		}
	case "call_expression":
		// `createApi(...).injectEndpoints(...)` etc.
		return factoryLeaf(x, fn)
	}
	return ""
}

// extractReactEcosystem is the program-level entry point. It walks every
// variable_declarator and emits the decorated slice/store/api + endpoint/hook
// entities and CONTAINS edges. Mirrors emitStoreActionEntities (#2626): runs
// before walk() so emitted entities are present for the rest of extraction.
func (x *extractor) extractReactEcosystem(root *sitter.Node) {
	r := x.buildReactEcosystemImports()
	if r == nil || root == nil {
		return
	}
	for _, d := range findAllNodes(root, "variable_declarator") {
		x.reactEcosystemDeclarator(r, d)
	}
}

// reactEcosystemDeclarator handles `const <name> = <factory>(...)` shapes.
func (x *extractor) reactEcosystemDeclarator(r *reactEcosystemImports, d *sitter.Node) {
	nameNode := d.ChildByFieldName("name")
	valueNode := d.ChildByFieldName("value")
	if nameNode == nil || valueNode == nil || nameNode.Type() != "identifier" {
		return
	}
	name := x.nodeText(nameNode)
	if name == "" {
		return
	}

	// Unwrap `createApi(...).injectEndpoints(...)`: the outer call's object is
	// the createApi call; injectEndpoints adds endpoints to an existing api.
	canon := r.resolveFactory(x, valueNode)
	leaf := factoryLeaf(x, valueNode)

	switch {
	case canon == "createSlice":
		x.emitReduxSlice(name, valueNode)
	case canon == "configureStore" || canon == "createStore":
		x.emitReduxStore(name, valueNode, canon)
	case canon == "createApi":
		x.emitRTKQueryApi(name, valueNode, false)
	case canon == "createAsyncThunk":
		x.emitAsyncThunk(name, valueNode)
	case canon == "createEntityAdapter":
		x.stampEcosystem(name, valueNode, propViaReduxStore, "entity_adapter")
	case leaf == "injectEndpoints":
		// `existingApi.injectEndpoints({ endpoints: builder => ({...}) })`
		x.emitRTKQueryApi(name, valueNode, true)
	}
}

// emitReduxSlice emits a SCOPE.Component subtype="redux_slice" for the slice
// and one SCOPE.Operation subtype="redux_reducer" per reducer key, plus a
// CONTAINS edge slice→reducer. The generated action creators share the reducer
// names (RTK derives them 1:1), so we stamp the reducer count + names on the
// slice entity.
func (x *extractor) emitReduxSlice(name string, valueNode *sitter.Node) {
	call := unwrapCall(valueNode)
	if call == nil {
		return
	}
	reducers, reducerNodes := sliceReducers(x, call)
	sliceName := sliceConfigName(x, call)
	props := map[string]string{
		"kind":          "SCOPE.Component",
		"subtype":       "redux_slice",
		"via":           propViaReduxSlice,
		"reducer_count": strconv.Itoa(len(reducers)),
	}
	if sliceName != "" {
		props["slice_name"] = sliceName
	}
	if len(reducers) > 0 {
		props["reducers"] = strings.Join(reducers, ",")
		props["actions"] = strings.Join(reducers, ",") // RTK: 1 action per reducer
	}
	var rels []types.RelationshipRecord
	for _, rk := range reducers {
		rels = append(rels, types.RelationshipRecord{
			ToID: name + "::" + rk,
			Kind: string(types.RelationshipKindContains),
			Properties: map[string]string{
				"via":  propViaReduxSlice,
				"role": "reducer",
			},
		})
	}
	x.emitWithProps(name, "SCOPE.Component", valueNode, "redux_slice", "createSlice("+sliceName+")", props, rels)
	// Emit each reducer as a standalone operation so traces can descend (#2626).
	for _, rk := range reducers {
		fnNode := reducerNodes[rk]
		rprops := map[string]string{
			"kind":    "SCOPE.Operation",
			"subtype": "redux_reducer",
			"via":     propViaReduxSlice,
			"slice":   name,
		}
		emitNode := fnNode
		if emitNode == nil {
			emitNode = valueNode
		}
		x.emitWithProps(name+"::"+rk, "SCOPE.Operation", emitNode, "redux_reducer", name+"."+rk, rprops, nil)
	}
}

// emitReduxStore emits a SCOPE.Component subtype="redux_store" decorated with
// the configured reducer slice keys.
func (x *extractor) emitReduxStore(name string, valueNode *sitter.Node, canon string) {
	call := unwrapCall(valueNode)
	props := map[string]string{
		"kind":    "SCOPE.Component",
		"subtype": "redux_store",
		"via":     propViaReduxStore,
		"factory": canon,
	}
	if call != nil {
		if keys := configureStoreReducerKeys(x, call); len(keys) > 0 {
			props["reducer_slices"] = strings.Join(keys, ",")
		}
	}
	x.emitWithProps(name, "SCOPE.Component", valueNode, "redux_store", canon+"(...)", props, nil)
}

// emitAsyncThunk emits a SCOPE.Operation subtype="redux_async_thunk" for a
// createAsyncThunk action creator (redux_async_flow). The type-prefix string
// (first arg) is stamped so the dispatched action type is queryable.
func (x *extractor) emitAsyncThunk(name string, valueNode *sitter.Node) {
	call := unwrapCall(valueNode)
	props := map[string]string{
		"kind":    "SCOPE.Operation",
		"subtype": "redux_async_thunk",
		"via":     propViaReduxAsync,
		"flavor":  "createAsyncThunk",
	}
	if call != nil {
		if t := firstStringArg(x, call); t != "" {
			props["action_type"] = t
		}
	}
	x.emitWithProps(name, "SCOPE.Operation", valueNode, "redux_async_thunk", "createAsyncThunk("+props["action_type"]+")", props, nil)
}

// emitRTKQueryApi emits a SCOPE.Component subtype="rtk_query_api" for the api
// and one SCOPE.Operation subtype="rtk_query_endpoint" per endpoint, with a
// CONTAINS edge api→endpoint. Endpoints are cross-repo-HTTP-linkable: we stamp
// http_method (query→GET / mutation→derived) + the endpoint's query path so the
// later link pass can wire them to backend endpoints. injected=true for
// injectEndpoints (extends an existing api).
func (x *extractor) emitRTKQueryApi(name string, valueNode *sitter.Node, injected bool) {
	call := unwrapCall(valueNode)
	if call == nil {
		return
	}
	endpoints := rtkQueryEndpoints(x, call)
	props := map[string]string{
		"kind":           "SCOPE.Component",
		"subtype":        "rtk_query_api",
		"via":            propViaRTKQuery,
		"endpoint_count": strconv.Itoa(len(endpoints)),
		"http_linkable":  "true",
	}
	if injected {
		props["injected"] = "true"
	}
	if rp := rtkReducerPath(x, call); rp != "" {
		props["reducer_path"] = rp
	}
	var rels []types.RelationshipRecord
	for _, ep := range endpoints {
		rels = append(rels, types.RelationshipRecord{
			ToID: name + "::" + ep.name,
			Kind: string(types.RelationshipKindContains),
			Properties: map[string]string{
				"via":  propViaRTKQuery,
				"role": "endpoint",
			},
		})
	}
	x.emitWithProps(name, "SCOPE.Component", valueNode, "rtk_query_api", "createApi("+name+")", props, rels)
	for _, ep := range endpoints {
		method := "GET"
		if ep.kind == "mutation" {
			method = "POST"
		}
		eprops := map[string]string{
			"kind":          "SCOPE.Operation",
			"subtype":       "rtk_query_endpoint",
			"via":           propViaRTKQuery,
			"api":           name,
			"endpoint_kind": ep.kind, // query | mutation
			"http_method":   method,
			"http_linkable": "true",
		}
		if ep.path != "" {
			eprops["http_path"] = ep.path
		}
		emitNode := ep.node
		if emitNode == nil {
			emitNode = valueNode
		}
		x.emitWithProps(name+"::"+ep.name, "SCOPE.Operation", emitNode, "rtk_query_endpoint", name+"."+ep.name, eprops, nil)
	}
}

// stampEcosystem emits a lightweight decorated SCOPE.Component for entities we
// recognise but don't decompose (QueryClient instance, createEntityAdapter).
func (x *extractor) stampEcosystem(name string, valueNode *sitter.Node, via, subtype string) {
	props := map[string]string{
		"kind":    "SCOPE.Component",
		"subtype": subtype,
		"via":     via,
	}
	x.emitWithProps(name, "SCOPE.Component", valueNode, subtype, subtype+"("+name+")", props, nil)
}

// decorateReduxAsyncFlow stamps redux_saga / redux_epic markers on already-
// emitted function entities whose bodies use redux-saga effects or
// redux-observable epic shapes (redux_async_flow). It is a no-op when neither
// redux-saga nor redux-observable is imported.
//
// Saga watcher functions (those using takeEvery/takeLatest/takeLeading) are
// stamped saga_role="watcher"; saga workers using put/call/select are stamped
// saga_role="worker". Epics (functions whose body calls .pipe(...ofType(...)))
// are stamped redux_epic="true". Decoration only (#2839) — no new entity.
func (x *extractor) decorateReduxAsyncFlow(root *sitter.Node) {
	r := x.buildReactEcosystemImports()
	if r == nil || (!r.hasSaga && !x.fileImportsReduxObservable()) {
		return
	}
	// Map function name → entity index for the operations emitted in this file.
	idxByName := map[string]int{}
	for i := range x.entities {
		e := &x.entities[i]
		if e.Kind == "SCOPE.Operation" && e.SourceFile == x.filePath {
			if _, dup := idxByName[e.Name]; !dup {
				idxByName[e.Name] = i
			}
		}
	}
	scan := func(name string, defNode, body *sitter.Node) {
		if name == "" || body == nil {
			return
		}
		watcher, worker, epic := x.sagaBodyRoles(r, body)
		if !watcher && !worker && !epic {
			return
		}
		props := map[string]string{
			"kind":    "SCOPE.Operation",
			"subtype": "function",
			"via":     propViaReduxAsync,
		}
		switch {
		case watcher:
			props["redux_saga"] = "true"
			props["saga_role"] = "watcher"
		case worker:
			props["redux_saga"] = "true"
			props["saga_role"] = "worker"
		}
		if epic {
			props["redux_epic"] = "true"
		}
		// If the generic walk already emitted this function (non-generator
		// epics via arrow/function), decorate in place; otherwise emit a new
		// SCOPE.Operation (generator declarations are not otherwise walked).
		if idx, ok := idxByName[name]; ok {
			e := &x.entities[idx]
			if e.Properties == nil {
				e.Properties = map[string]string{}
			}
			for k, v := range props {
				if k == "kind" || k == "subtype" {
					continue
				}
				e.Properties[k] = v
			}
			return
		}
		emitNode := defNode
		if emitNode == nil {
			emitNode = body
		}
		x.emitWithProps(name, "SCOPE.Operation", emitNode, "function", "function* "+name, props, nil)
		idxByName[name] = len(x.entities) - 1
	}
	for _, fn := range findAllNodes(root, "function_declaration", "generator_function_declaration") {
		nameNode := fn.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		scan(x.nodeText(nameNode), fn, fn.ChildByFieldName("body"))
	}
	// `const x = function* () {...}` / `const epic = (...) => action$.pipe(...)`.
	for _, d := range findAllNodes(root, "variable_declarator") {
		nameNode := d.ChildByFieldName("name")
		valNode := d.ChildByFieldName("value")
		if nameNode == nil || valNode == nil {
			continue
		}
		if valNode.Type() == "function_expression" || valNode.Type() == "arrow_function" || valNode.Type() == "generator_function" {
			scan(x.nodeText(nameNode), d, valNode.ChildByFieldName("body"))
		}
	}
}

// fileImportsReduxObservable reports whether redux-observable is imported.
func (x *extractor) fileImportsReduxObservable() bool {
	for _, b := range x.importByLocal {
		if b != nil && b.importPath == "redux-observable" {
			return true
		}
	}
	return false
}

// sagaBodyRoles inspects a function body for saga effect calls and epic shapes.
// Returns (watcher, worker, epic). A body using takeEvery/takeLatest/takeLeading
// is a watcher; one using put/call/select/fork is a worker; one calling
// .pipe(...) with ofType(...) is an epic.
func (x *extractor) sagaBodyRoles(r *reactEcosystemImports, body *sitter.Node) (watcher, worker, epic bool) {
	for _, c := range findAllNodes(body, "call_expression") {
		leaf := factoryLeaf(x, c)
		switch leaf {
		case "takeEvery", "takeLatest", "takeLeading":
			watcher = true
		case "put", "call", "select", "fork", "all":
			// Confirm it resolves to a redux-saga effect when imports are known.
			if r.hasSaga {
				worker = true
			}
		case "ofType":
			epic = true
		}
	}
	return watcher, worker, epic
}

// ── shape helpers ───────────────────────────────────────────────────────────

// unwrapCall returns the call_expression inside valueNode, unwrapping a single
// `await`/parenthesised layer. Returns nil when valueNode is not a call.
func unwrapCall(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	switch n.Type() {
	case "call_expression":
		return n
	case "new_expression":
		return n
	case "parenthesized_expression", "await_expression":
		for i := 0; i < int(n.ChildCount()); i++ {
			if c := unwrapCall(n.Child(i)); c != nil {
				return c
			}
		}
	}
	return nil
}

// firstStringArg returns the first string-literal argument of a call, unquoted.
func firstStringArg(x *extractor, call *sitter.Node) string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		if ch != nil && ch.Type() == "string" {
			return trimStringQuotes(x.nodeText(ch))
		}
	}
	return ""
}

// configObjectArg returns the first object-literal argument of a call.
func configObjectArg(call *sitter.Node) *sitter.Node {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		if ch != nil && ch.Type() == "object" {
			return ch
		}
	}
	return nil
}

// objectPairValue returns the value node for a given key in an object literal.
func objectPairValue(x *extractor, obj *sitter.Node, key string) *sitter.Node {
	if obj == nil {
		return nil
	}
	for i := 0; i < int(obj.ChildCount()); i++ {
		pair := obj.Child(i)
		if pair == nil || pair.Type() != "pair" {
			continue
		}
		kn := pair.ChildByFieldName("key")
		if kn == nil {
			continue
		}
		if strings.Trim(x.nodeText(kn), `"'`+"`") == key {
			return pair.ChildByFieldName("value")
		}
	}
	return nil
}

// sliceConfigName returns the `name:` string from a createSlice config object.
func sliceConfigName(x *extractor, call *sitter.Node) string {
	obj := configObjectArg(call)
	if obj == nil {
		return ""
	}
	v := objectPairValue(x, obj, "name")
	if v == nil || v.Type() != "string" {
		return ""
	}
	return trimStringQuotes(x.nodeText(v))
}

// sliceReducers returns the reducer keys (and their function-value nodes) from
// the `reducers:` object of a createSlice config.
func sliceReducers(x *extractor, call *sitter.Node) ([]string, map[string]*sitter.Node) {
	obj := configObjectArg(call)
	if obj == nil {
		return nil, nil
	}
	reducersObj := objectPairValue(x, obj, "reducers")
	if reducersObj == nil || reducersObj.Type() != "object" {
		return nil, nil
	}
	var keys []string
	nodes := map[string]*sitter.Node{}
	for i := 0; i < int(reducersObj.ChildCount()); i++ {
		child := reducersObj.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "pair":
			kn := child.ChildByFieldName("key")
			if kn == nil {
				continue
			}
			k := strings.Trim(x.nodeText(kn), `"'`+"`")
			if k != "" {
				keys = append(keys, k)
				nodes[k] = child.ChildByFieldName("value")
			}
		case "method_definition":
			// `reducers: { setName(state, action) { ... } }` shorthand.
			kn := child.ChildByFieldName("name")
			if kn == nil {
				continue
			}
			k := strings.Trim(x.nodeText(kn), `"'`+"`")
			if k != "" {
				keys = append(keys, k)
				nodes[k] = child
			}
		}
	}
	return keys, nodes
}

// configureStoreReducerKeys returns the slice keys from `reducer: { a, b }` of a
// configureStore config (or the combined keys when reducer is combineReducers).
func configureStoreReducerKeys(x *extractor, call *sitter.Node) []string {
	obj := configObjectArg(call)
	if obj == nil {
		return nil
	}
	red := objectPairValue(x, obj, "reducer")
	if red == nil {
		return nil
	}
	// reducer: { user: ..., cart: ... }
	if red.Type() == "object" {
		return objectKeys(x, red)
	}
	// reducer: combineReducers({ user: ... })
	if red.Type() == "call_expression" {
		if inner := configObjectArg(red); inner != nil {
			return objectKeys(x, inner)
		}
	}
	return nil
}

// objectKeys returns the keys of an object literal (pairs + shorthand).
func objectKeys(x *extractor, obj *sitter.Node) []string {
	var keys []string
	for i := 0; i < int(obj.ChildCount()); i++ {
		ch := obj.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "pair":
			if kn := ch.ChildByFieldName("key"); kn != nil {
				k := strings.Trim(x.nodeText(kn), `"'`+"`")
				if k != "" {
					keys = append(keys, k)
				}
			}
		case "shorthand_property_identifier", "identifier":
			k := strings.Trim(x.nodeText(ch), `"'`+"`")
			if k != "" {
				keys = append(keys, k)
			}
		}
	}
	return keys
}

// rtkEndpoint describes one RTK-Query endpoint.
type rtkEndpoint struct {
	name string
	kind string // query | mutation
	path string // best-effort query path string
	node *sitter.Node
}

// rtkReducerPath returns the `reducerPath:` string from a createApi config.
func rtkReducerPath(x *extractor, call *sitter.Node) string {
	obj := configObjectArg(call)
	if obj == nil {
		return ""
	}
	v := objectPairValue(x, obj, "reducerPath")
	if v == nil || v.Type() != "string" {
		return ""
	}
	return trimStringQuotes(x.nodeText(v))
}

// rtkQueryEndpoints extracts endpoints from the `endpoints: (builder) => ({...})`
// arrow of a createApi / injectEndpoints config.
func rtkQueryEndpoints(x *extractor, call *sitter.Node) []rtkEndpoint {
	obj := configObjectArg(call)
	if obj == nil {
		return nil
	}
	epVal := objectPairValue(x, obj, "endpoints")
	if epVal == nil {
		return nil
	}
	// endpoints is an arrow returning an object literal.
	epObj := findObjectLiteral(arrowBody(epVal))
	if epObj == nil {
		return nil
	}
	var out []rtkEndpoint
	for i := 0; i < int(epObj.ChildCount()); i++ {
		pair := epObj.Child(i)
		if pair == nil || pair.Type() != "pair" {
			continue
		}
		kn := pair.ChildByFieldName("key")
		vn := pair.ChildByFieldName("value")
		if kn == nil || vn == nil {
			continue
		}
		epName := strings.Trim(x.nodeText(kn), `"'`+"`")
		if epName == "" {
			continue
		}
		// value is `builder.query({...})` / `builder.mutation({...})`.
		kind := builderEndpointKind(x, vn)
		if kind == "" {
			continue
		}
		out = append(out, rtkEndpoint{
			name: epName,
			kind: kind,
			path: endpointQueryPath(x, vn),
			node: pair,
		})
	}
	return out
}

// arrowBody returns the body node of an arrow_function (or the node itself when
// it is already an object/parenthesized expression).
func arrowBody(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if n.Type() == "arrow_function" {
		return n.ChildByFieldName("body")
	}
	return n
}

// builderEndpointKind returns "query" or "mutation" for `builder.query(...)` /
// `builder.mutation(...)`. "" otherwise.
func builderEndpointKind(x *extractor, n *sitter.Node) string {
	call := unwrapCall(n)
	if call == nil {
		return ""
	}
	leaf := factoryLeaf(x, call)
	switch leaf {
	case "query":
		return "query"
	case "mutation":
		return "mutation"
	}
	return ""
}

// endpointQueryPath best-effort extracts the path string from a builder endpoint
// config's `query: () => '/path'` (or `query: () => ({ url: '/path' })`).
func endpointQueryPath(x *extractor, n *sitter.Node) string {
	call := unwrapCall(n)
	if call == nil {
		return ""
	}
	obj := configObjectArg(call)
	if obj == nil {
		return ""
	}
	q := objectPairValue(x, obj, "query")
	if q == nil {
		return ""
	}
	body := arrowBody(q)
	if body == nil {
		return ""
	}
	// `=> '/users'`
	if body.Type() == "string" {
		return trimStringQuotes(x.nodeText(body))
	}
	// `=> ({ url: '/users', method: 'POST' })`
	if inner := findObjectLiteral(body); inner != nil {
		if u := objectPairValue(x, inner, "url"); u != nil && u.Type() == "string" {
			return trimStringQuotes(x.nodeText(u))
		}
	}
	// `=> \`/users/${id}\`` — return the raw template text as a hint.
	if body.Type() == "template_string" {
		return trimStringQuotes(x.nodeText(body))
	}
	return ""
}
