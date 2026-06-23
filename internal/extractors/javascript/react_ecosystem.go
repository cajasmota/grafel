// react_ecosystem.go — issue #2894 PR1.
//
// React Ecosystem framework_specific group: first-class extraction of the two
// dominant real-world React/React-Native state + data idioms that no generic
// capability column expresses on its own:
//
//	redux_store_extraction
//	  - Redux:         createStore / combineReducers, connect(mapState,mapDispatch)
//	  - Redux Toolkit: configureStore, createSlice (→ reducers + actions),
//	                   createEntityAdapter
//	  - react-redux:   useSelector / useDispatch / useStore (decorated as
//	                   USES_HOOK by the generic hook pass; here we additionally
//	                   decorate the slice/store entities)
//
//	redux_async_flow
//	  - Redux Toolkit: createAsyncThunk
//	  - Redux-Saga:    takeEvery / takeLatest watcher effects + put/call/select
//	  - Redux-Observable: epics (combineEpics / *Epic functions using ofType)
//	  - redux-thunk:   thunk action creators (functions returning (dispatch)=>)
//
//	rtk_query_extraction
//	  - RTK Query:     createApi / injectEndpoints → endpoint entities. Endpoints
//	                   are cross-repo-HTTP-linkable like the backend endpoints;
//	                   we stamp the http-ish metadata so the link pass can pick
//	                   them up later (the query/mutation string is the path).
//
//	tanstack_query_extraction
//	  - TanStack/React Query: useQuery / useMutation / useInfiniteQuery +
//	                   QueryClient + queryKey (+ cache invalidation via
//	                   invalidateQueries). HIGH priority — the dominant data layer.
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
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/types"
)

// Property `via` values stamped on React-Ecosystem entities/edges so the
// resolver and coverage tooling can classify provenance.
const (
	propViaReduxSlice    = "redux_slice"
	propViaReduxStore    = "redux_store"
	propViaReduxAsync    = "redux_async"
	propViaRTKQuery      = "rtk_query"
	propViaTanstackQuery = "tanstack_query" // stamped on TanStack-Query USES_HOOK edges
	propViaAtomStore     = "atom_store"     // Recoil/Jotai/Valtio/MobX atoms & stores (#2894 PR2)
	propViaSWR           = "swr"            // SWR useSWR/useSWRMutation (#2894 PR2)
	propViaForm          = "form_library"   // React Hook Form / Formik form state (#2894 PR3)
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
	// atom-store factory local-name → (canonicalName, library) for Recoil /
	// Jotai / Valtio / MobX atom & store factories (issue #2894 PR2). Kept as a
	// per-name map because `atom` is exported by BOTH recoil and jotai with
	// different shapes, so the binding's source package disambiguates them.
	atomFactories map[string]atomFactoryBinding

	hasRedux    bool // redux or @reduxjs/toolkit
	hasReactRdx bool // react-redux
	hasRTKQuery bool // @reduxjs/toolkit/query
	hasTanstack bool // @tanstack/*-query or react-query
	hasSaga     bool // redux-saga
	hasSWR      bool // swr (issue #2894 PR2)
	hasRecoil   bool // recoil (issue #2894 PR2)
	hasJotai    bool // jotai (issue #2894 PR2)
	hasValtio   bool // valtio (issue #2894 PR2)
	hasMobx     bool // mobx / mobx-react / mobx-react-lite (issue #2894 PR2)
	hasRHF      bool // react-hook-form (issue #2894 PR3)
	hasFormik   bool // formik (issue #2894 PR3)
	// resolver-factory local-name → canonical resolver schema library
	// (zod | yup | joi | superstruct | ajv | vest) for React Hook Form's
	// `resolver: zodResolver(schema)` linkage (issue #2894 PR3). Bound from the
	// @hookform/resolvers/* packages so an aliased import still resolves.
	resolverFactories map[string]string
}

// atomFactoryBinding records the canonical factory name and the atom-store
// library a binding came from, so the declarator handler emits the right
// subtype (#2894 PR2).
type atomFactoryBinding struct {
	factory string // atom | selector | atomFamily | selectorFamily | atomWithStorage | proxy | observable | makeAutoObservable | makeObservable
	library string // recoil | jotai | valtio | mobx
}

// atomStoreFactories maps each atom-store library to the factory names it
// exports that we treat as an atom/store declaration site (#2894 PR2).
var atomStoreFactories = map[string]map[string]bool{
	"recoil": {"atom": true, "selector": true, "atomFamily": true, "selectorFamily": true},
	"jotai":  {"atom": true, "atomWithStorage": true, "atomWithReset": true, "atomFamily": true},
	"valtio": {"proxy": true, "proxyWithComputed": true},
	"mobx":   {"observable": true, "makeAutoObservable": true, "makeObservable": true},
}

// atomStoreSubtype maps a (library, factory) to the emitted entity subtype.
// Recoil selectors and Jotai derived atoms are "derived" stores; Valtio proxy
// and MobX observable are object stores.
func atomStoreSubtype(library, factory string) string {
	switch library {
	case "recoil":
		if factory == "selector" || factory == "selectorFamily" {
			return "recoil_selector"
		}
		return "recoil_atom"
	case "jotai":
		return "jotai_atom"
	case "valtio":
		return "valtio_proxy"
	case "mobx":
		return "mobx_store"
	}
	return "atom_store"
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
		strings.HasPrefix(p, "@tanstack/query") ||
		// Angular adapter — @tanstack/angular-query-experimental (#2910). Does not
		// end in "-query", so it needs its own prefix check.
		strings.HasPrefix(p, "@tanstack/angular-query")
}

// atomStoreLibrary returns the canonical atom-store library name for an import
// path, or "" if the path is not an atom-store package (#2894 PR2). mobx-react /
// mobx-react-lite (the observer HOC) map to "mobx".
func atomStoreLibrary(p string) string {
	switch {
	case p == "recoil":
		return "recoil"
	case p == "jotai" || strings.HasPrefix(p, "jotai/"):
		return "jotai"
	case p == "valtio" || strings.HasPrefix(p, "valtio/"):
		return "valtio"
	case p == "mobx" || p == "mobx-react" || p == "mobx-react-lite":
		return "mobx"
	}
	return ""
}

// resolverSchemaLibrary maps an @hookform/resolvers/<lib> import path to the
// canonical validation-schema library it bridges (#2894 PR3). React Hook Form's
// `resolver:` option is produced by these adapters (e.g. zodResolver(schema));
// recovering the adapter lets us link the form to its schema library.
func resolverSchemaLibrary(p string) string {
	const prefix = "@hookform/resolvers"
	if p == prefix {
		return "" // bare package re-exports every adapter; per-name handled below
	}
	if !strings.HasPrefix(p, prefix+"/") {
		return ""
	}
	switch strings.TrimPrefix(p, prefix+"/") {
	case "zod":
		return "zod"
	case "yup":
		return "yup"
	case "joi":
		return "joi"
	case "superstruct":
		return "superstruct"
	case "ajv":
		return "ajv"
	case "vest":
		return "vest"
	case "class-validator":
		return "class-validator"
	}
	return ""
}

// resolverFactoryName maps a React Hook Form resolver-adapter export name (e.g.
// zodResolver / yupResolver) to the canonical schema library (#2894 PR3). Covers
// the bare `@hookform/resolvers` package which re-exports each adapter by name.
func resolverFactoryName(imp string) string {
	switch imp {
	case "zodResolver":
		return "zod"
	case "yupResolver":
		return "yup"
	case "joiResolver":
		return "joi"
	case "superstructResolver":
		return "superstruct"
	case "ajvResolver":
		return "ajv"
	case "vestResolver":
		return "vest"
	case "classValidatorResolver":
		return "class-validator"
	}
	return ""
}

// rhfFormHooks are the React Hook Form hook/helper exports that signal a form is
// being declared/consumed (#2894 PR3). useForm is the entry point; the rest are
// field/array/context/watch helpers. All surface as USES_HOOK via the generic
// hook_recognition pass; here we add the form-specific decoration.
var rhfFormHooks = map[string]bool{
	"useForm": true, "useFormContext": true, "useFieldArray": true,
	"useController": true, "useWatch": true, "useFormState": true,
}

// formikFormHooks are the Formik hook exports (#2894 PR3). useFormik is the
// hook-style entry point; useField/useFormikContext are field/context helpers.
var formikFormHooks = map[string]bool{
	"useFormik": true, "useField": true, "useFormikContext": true,
}

// formikJSXComponents are the Formik render-prop / JSX components (#2894 PR3).
// A component rendering <Formik>/<Field>/<Form>/<FieldArray>/<ErrorMessage>
// is form-bearing even when it never calls a Formik hook directly.
var formikJSXComponents = map[string]bool{
	"Formik": true, "Field": true, "Form": true,
	"FieldArray": true, "ErrorMessage": true, "FastField": true,
}

// buildReactEcosystemImports scans importByLocal for the ecosystem packages.
// Returns nil when none are present (fast-path for non-ecosystem files).
func (x *extractor) buildReactEcosystemImports() *reactEcosystemImports {
	if len(x.importByLocal) == 0 {
		return nil
	}
	r := &reactEcosystemImports{
		factories:         map[string]string{},
		hooks:             map[string]string{},
		sagaEffects:       map[string]string{},
		atomFactories:     map[string]atomFactoryBinding{},
		resolverFactories: map[string]string{},
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
		case p == "swr" || strings.HasPrefix(p, "swr/"):
			r.hasSWR = true
			any = true // useSWR/useSWRMutation surface as USES_HOOK; we decorate keys
		case p == "react-hook-form" || strings.HasPrefix(p, "react-hook-form/"):
			r.hasRHF = true
			any = true // useForm/Controller surface as USES_HOOK; we decorate forms (#2894 PR3)
		case p == "formik" || strings.HasPrefix(p, "formik/"):
			r.hasFormik = true
			any = true // useFormik/<Formik>/<Field> form idioms (#2894 PR3)
		case p == "@hookform/resolvers" || strings.HasPrefix(p, "@hookform/resolvers/"):
			any = true
			// Per-path adapter (zod/yup/...) when imported from the sub-path,
			// else per-export name from the bare package (#2894 PR3).
			if lib := resolverSchemaLibrary(p); lib != "" {
				r.resolverFactories[local] = lib
			} else if lib := resolverFactoryName(imp); lib != "" {
				r.resolverFactories[local] = lib
			}
		default:
			if lib := atomStoreLibrary(p); lib != "" {
				any = true
				switch lib {
				case "recoil":
					r.hasRecoil = true
				case "jotai":
					r.hasJotai = true
				case "valtio":
					r.hasValtio = true
				case "mobx":
					r.hasMobx = true
				}
				if atomStoreFactories[lib][imp] {
					r.atomFactories[local] = atomFactoryBinding{factory: imp, library: lib}
				}
			}
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
func (r *reactEcosystemImports) resolveFactory(x *extractor, call ts.Node) string {
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
func factoryLeaf(x *extractor, n ts.Node) string {
	if n == nil {
		return ""
	}
	var fn ts.Node
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
func (x *extractor) extractReactEcosystem(root ts.Node) {
	r := x.buildReactEcosystemImports()
	if r == nil || root == nil {
		return
	}
	for _, d := range findAllNodes(root, "variable_declarator") {
		x.reactEcosystemDeclarator(r, d)
	}
}

// reactEcosystemDeclarator handles `const <name> = <factory>(...)` shapes.
func (x *extractor) reactEcosystemDeclarator(r *reactEcosystemImports, d ts.Node) {
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
	default:
		// Atom stores (#2894 PR2): Recoil atom/selector, Jotai atom, Valtio
		// proxy, MobX observable/makeAutoObservable.
		if len(r.atomFactories) > 0 {
			if ab := r.resolveAtomFactory(x, valueNode); ab.factory != "" {
				x.emitAtomStore(name, valueNode, ab)
			}
		}
	}
}

// resolveAtomFactory returns the atom-store binding a call's callee resolves to,
// via the import binding (so an aliased `import { atom as a }` still matches).
// Returns the zero binding when the leaf is not a tracked atom factory.
func (r *reactEcosystemImports) resolveAtomFactory(x *extractor, valueNode ts.Node) atomFactoryBinding {
	leaf := factoryLeaf(x, valueNode)
	if leaf == "" {
		return atomFactoryBinding{}
	}
	if ab, ok := r.atomFactories[leaf]; ok {
		return ab
	}
	return atomFactoryBinding{}
}

// emitAtomStore emits a decorated SCOPE.Component for a Recoil/Jotai/Valtio/MobX
// atom or store declaration (#2894 PR2). For Recoil atoms/selectors the `key:`
// string is stamped (atoms are keyed by a globally-unique string). Decorate-only
// (#2839): SCOPE.Component subtype, no new EntityKind.
func (x *extractor) emitAtomStore(name string, valueNode ts.Node, ab atomFactoryBinding) {
	subtype := atomStoreSubtype(ab.library, ab.factory)
	props := map[string]string{
		"kind":         "SCOPE.Component",
		"subtype":      subtype,
		"via":          propViaAtomStore,
		"atom_library": ab.library,
		"atom_factory": ab.factory,
	}
	// Recoil atom/selector configs carry a unique `key:` string.
	if ab.library == "recoil" {
		if call := unwrapCall(valueNode); call != nil {
			if obj := configObjectArg(call); obj != nil {
				if k := objectPairValue(x, obj, "key"); k != nil && k.Type() == "string" {
					props["atom_key"] = trimStringQuotes(x.nodeText(k))
				}
			}
		}
	}
	sig := ab.factory + "(" + name + ")"
	x.emitWithProps(name, "SCOPE.Component", valueNode, subtype, sig, props, nil)
}

// decorateSWR stamps swr=true + the SWR key (first arg) on already-emitted
// component/custom-hook entities whose body calls useSWR / useSWRMutation /
// useSWRInfinite / useSWRSubscription (#2894 PR2). The hook call itself already
// surfaces as a USES_HOOK edge via the generic hook pass (react.go); this pass
// adds the SWR-specific decoration (key + which SWR hook) so the data-fetching
// idiom is queryable. No-op when swr is not imported. Decorate-only (#2839).
func (x *extractor) decorateSWR(root ts.Node) {
	r := x.buildReactEcosystemImports()
	if r == nil || !r.hasSWR {
		return
	}
	idxByName := map[string]int{}
	for i := range x.entities {
		e := &x.entities[i]
		if e.Kind == "SCOPE.Operation" && e.SourceFile == x.filePath {
			if _, dup := idxByName[e.Name]; !dup {
				idxByName[e.Name] = i
			}
		}
	}
	scan := func(name string, body ts.Node) {
		if name == "" || body == nil {
			return
		}
		if !isComponentName(name) && !isReactHookName(name) {
			return
		}
		idx, ok := idxByName[name]
		if !ok {
			return
		}
		var keys []string
		hooks := map[string]bool{}
		for _, c := range findAllNodes(body, "call_expression") {
			leaf := factoryLeaf(x, c)
			switch leaf {
			case "useSWR", "useSWRMutation", "useSWRInfinite", "useSWRSubscription":
				hooks[leaf] = true
				if k := firstStringArg(x, c); k != "" {
					keys = append(keys, k)
				}
			}
		}
		if len(hooks) == 0 {
			return
		}
		e := &x.entities[idx]
		if e.Properties == nil {
			e.Properties = map[string]string{}
		}
		e.Properties["via"] = propViaSWR
		e.Properties["swr"] = "true"
		var names []string
		for h := range hooks {
			names = append(names, h)
		}
		sort.Strings(names)
		e.Properties["swr_hooks"] = strings.Join(names, ",")
		if len(keys) > 0 {
			e.Properties["swr_keys"] = strings.Join(keys, ",")
		}
	}
	for _, fn := range findAllNodes(root, "function_declaration") {
		nameNode := fn.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		scan(x.nodeText(nameNode), fn.ChildByFieldName("body"))
	}
	for _, d := range findAllNodes(root, "variable_declarator") {
		nameNode := d.ChildByFieldName("name")
		valNode := d.ChildByFieldName("value")
		if nameNode == nil || valNode == nil {
			continue
		}
		if valNode.Type() == "arrow_function" || valNode.Type() == "function_expression" {
			scan(x.nodeText(nameNode), valNode.ChildByFieldName("body"))
		}
	}
}

// decorateForms stamps form_library + form-specific metadata on already-emitted
// component/custom-hook entities whose body uses React Hook Form or Formik form
// idioms (#2894 PR3 / issue #2909). The hook calls (useForm/useFormik/...) and
// JSX (<Formik>/<Field>) already surface generically (USES_HOOK / JSX renders);
// this pass adds the form-specific decoration so the idiom is queryable:
//
//	form_library      react_hook_form | formik (formik wins if both, rare)
//	form_hooks        sorted CSV of the form hooks the body uses
//	form_resolver     RHF: schema library bridged by the resolver (zod/yup/...)
//	validation_schema RHF: resolver schema identifier; Formik: validationSchema
//	                  expression text when recoverable
//	form_field_count  RHF: distinct register('name') field names; Formik: <Field
//	                  name="..."> count (literal names only)
//	form_components    formik JSX components rendered (Formik/Field/Form/...)
//
// Decorate-only (#2839): no new EntityKind/RelationshipKind. No-op when neither
// react-hook-form nor formik is imported.
func (x *extractor) decorateForms(root ts.Node) {
	r := x.buildReactEcosystemImports()
	if r == nil || (!r.hasRHF && !r.hasFormik) {
		return
	}
	idxByName := map[string]int{}
	for i := range x.entities {
		e := &x.entities[i]
		if e.Kind == "SCOPE.Operation" && e.SourceFile == x.filePath {
			if _, dup := idxByName[e.Name]; !dup {
				idxByName[e.Name] = i
			}
		}
	}
	scan := func(name string, body ts.Node) {
		if name == "" || body == nil {
			return
		}
		if !isComponentName(name) && !isReactHookName(name) {
			return
		}
		idx, ok := idxByName[name]
		if !ok {
			return
		}
		info := x.collectFormInfo(r, body)
		if info.library == "" {
			return
		}
		e := &x.entities[idx]
		if e.Properties == nil {
			e.Properties = map[string]string{}
		}
		e.Properties["via"] = propViaForm
		e.Properties["form_library"] = info.library
		if len(info.hooks) > 0 {
			e.Properties["form_hooks"] = strings.Join(sortedKeys(info.hooks), ",")
		}
		if len(info.components) > 0 {
			e.Properties["form_components"] = strings.Join(sortedKeys(info.components), ",")
		}
		if info.resolver != "" {
			e.Properties["form_resolver"] = info.resolver
		}
		if info.schema != "" {
			e.Properties["validation_schema"] = info.schema
		}
		if len(info.fields) > 0 {
			e.Properties["form_field_count"] = strconv.Itoa(len(info.fields))
			e.Properties["form_fields"] = strings.Join(sortedKeys(info.fields), ",")
		}
	}
	for _, fn := range findAllNodes(root, "function_declaration") {
		nameNode := fn.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		scan(x.nodeText(nameNode), fn.ChildByFieldName("body"))
	}
	for _, d := range findAllNodes(root, "variable_declarator") {
		nameNode := d.ChildByFieldName("name")
		valNode := d.ChildByFieldName("value")
		if nameNode == nil || valNode == nil {
			continue
		}
		if valNode.Type() == "arrow_function" || valNode.Type() == "function_expression" {
			scan(x.nodeText(nameNode), valNode.ChildByFieldName("body"))
		}
	}
}

// formInfo accumulates the form idioms found in one component/hook body.
type formInfo struct {
	library    string          // react_hook_form | formik
	hooks      map[string]bool // form hook names used
	components map[string]bool // formik JSX components rendered
	fields     map[string]bool // literal field names (register / <Field name>)
	resolver   string          // RHF resolver schema library (zod/yup/...)
	schema     string          // resolver schema identifier / Formik validationSchema text
}

// collectFormInfo walks a function body for React Hook Form and Formik idioms.
// Formik takes precedence on library when both appear (a Formik consumer may
// also import RHF transitively, but the rendered <Formik> is the authoritative
// form). Field names and resolver schema are recovered only from literals.
func (x *extractor) collectFormInfo(r *reactEcosystemImports, body ts.Node) formInfo {
	info := formInfo{
		hooks:      map[string]bool{},
		components: map[string]bool{},
		fields:     map[string]bool{},
	}
	rhf, formik := false, false
	for _, c := range findAllNodes(body, "call_expression") {
		leaf := factoryLeaf(x, c)
		switch {
		case r.hasRHF && rhfFormHooks[leaf]:
			rhf = true
			info.hooks[leaf] = true
			if leaf == "useForm" {
				x.collectRHFResolver(r, c, &info)
			}
		case r.hasFormik && formikFormHooks[leaf]:
			formik = true
			info.hooks[leaf] = true
			if leaf == "useFormik" {
				x.collectFormikSchema(c, &info)
			}
		case r.hasRHF && leaf == "register":
			// register('fieldName') — recover the literal field name.
			if n := firstStringArg(x, c); n != "" {
				info.fields[n] = true
			}
		}
	}
	// JSX: <Controller name="..."> (RHF) and <Formik>/<Field name="..."> (Formik).
	for _, jx := range findAllNodes(body, "jsx_opening_element", "jsx_self_closing_element") {
		nameNode := jx.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		tag := x.nodeText(nameNode)
		switch {
		case r.hasRHF && tag == "Controller":
			rhf = true
			info.hooks["Controller"] = true
			if fn := jsxStringAttr(x, jx, "name"); fn != "" {
				info.fields[fn] = true
			}
		case r.hasFormik && formikJSXComponents[tag]:
			formik = true
			info.components[tag] = true
			if tag == "Formik" {
				x.collectFormikSchemaJSX(jx, &info)
			}
			if tag == "Field" || tag == "FastField" || tag == "FieldArray" {
				if fn := jsxStringAttr(x, jx, "name"); fn != "" {
					info.fields[fn] = true
				}
			}
		}
	}
	switch {
	case formik:
		info.library = "formik"
	case rhf:
		info.library = "react_hook_form"
	}
	return info
}

// collectRHFResolver inspects a useForm({ resolver: zodResolver(schema) }) call
// and records the resolver schema library + schema identifier when recoverable.
func (x *extractor) collectRHFResolver(r *reactEcosystemImports, call ts.Node, info *formInfo) {
	obj := configObjectArg(call)
	if obj == nil {
		return
	}
	v := objectPairValue(x, obj, "resolver")
	if v == nil {
		return
	}
	rc := unwrapCall(v)
	if rc == nil {
		return
	}
	leaf := factoryLeaf(x, rc)
	// Resolve the adapter to its schema library. Prefer the import binding
	// (handles `import { zodResolver as zr }`); fall back to the export-name
	// heuristic (zodResolver → zod) for re-exported / namespace-accessed forms.
	if lib, ok := r.resolverFactories[leaf]; ok {
		info.resolver = lib
	} else if l := resolverFactoryName(leaf); l != "" {
		info.resolver = l
	}
	// Schema identifier is the first argument to the resolver factory.
	if args := rc.ChildByFieldName("arguments"); args != nil {
		for i := 0; i < int(args.ChildCount()); i++ {
			a := args.Child(i)
			if a != nil && a.Type() == "identifier" {
				info.schema = x.nodeText(a)
				break
			}
		}
	}
}

// collectFormikSchema inspects useFormik({ validationSchema: schema }) and
// records the schema expression text when present.
func (x *extractor) collectFormikSchema(call ts.Node, info *formInfo) {
	obj := configObjectArg(call)
	if obj == nil {
		return
	}
	if v := objectPairValue(x, obj, "validationSchema"); v != nil {
		info.schema = strings.TrimSpace(x.nodeText(v))
	}
}

// collectFormikSchemaJSX inspects a <Formik validationSchema={schema}> opening
// element and records the schema expression text.
func (x *extractor) collectFormikSchemaJSX(jx ts.Node, info *formInfo) {
	for i := 0; i < int(jx.ChildCount()); i++ {
		attr := jx.Child(i)
		if attr == nil || attr.Type() != "jsx_attribute" {
			continue
		}
		if attr.ChildCount() == 0 {
			continue
		}
		if x.nodeText(attr.Child(0)) != "validationSchema" {
			continue
		}
		for j := 1; j < int(attr.ChildCount()); j++ {
			v := attr.Child(j)
			if v != nil && v.Type() == "jsx_expression" {
				inner := strings.TrimSpace(x.nodeText(v))
				inner = strings.TrimPrefix(inner, "{")
				inner = strings.TrimSuffix(inner, "}")
				info.schema = strings.TrimSpace(inner)
			}
		}
	}
}

// jsxStringAttr returns the string-literal value of a JSX attribute (e.g.
// `name="email"`), unquoted. Returns "" for expression-container or missing.
func jsxStringAttr(x *extractor, jx ts.Node, attrName string) string {
	for i := 0; i < int(jx.ChildCount()); i++ {
		attr := jx.Child(i)
		if attr == nil || attr.Type() != "jsx_attribute" || attr.ChildCount() == 0 {
			continue
		}
		if x.nodeText(attr.Child(0)) != attrName {
			continue
		}
		for j := 1; j < int(attr.ChildCount()); j++ {
			v := attr.Child(j)
			if v != nil && v.Type() == "string" {
				return trimStringQuotes(x.nodeText(v))
			}
		}
	}
	return ""
}

// sortedKeys returns the keys of a set in lexical order.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// emitReduxSlice emits a SCOPE.Component subtype="redux_slice" for the slice
// and one SCOPE.Operation subtype="redux_reducer" per reducer key, plus a
// CONTAINS edge slice→reducer. The generated action creators share the reducer
// names (RTK derives them 1:1), so we stamp the reducer count + names on the
// slice entity.
func (x *extractor) emitReduxSlice(name string, valueNode ts.Node) {
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
func (x *extractor) emitReduxStore(name string, valueNode ts.Node, canon string) {
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
func (x *extractor) emitAsyncThunk(name string, valueNode ts.Node) {
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
func (x *extractor) emitRTKQueryApi(name string, valueNode ts.Node, injected bool) {
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
func (x *extractor) stampEcosystem(name string, valueNode ts.Node, via, subtype string) {
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
func (x *extractor) decorateReduxAsyncFlow(root ts.Node) {
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
	scan := func(name string, defNode, body ts.Node) {
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

// fileImportsTanstackAngular reports whether the TanStack Query Angular adapter
// (@tanstack/angular-query-experimental) is imported in this file (#2910). The
// Angular adapter is the only TanStack Query entry point whose call shapes are
// inject* (injectQuery/injectMutation/injectInfiniteQuery) rather than the
// React/Vue use* or Svelte create* names, and Angular .ts files are parsed by
// this extractor — so it is detected here, alongside the React idioms, and
// surfaced on the Angular class via angularTanstackQuery (angular.go).
func (x *extractor) fileImportsTanstackAngular() bool {
	for _, b := range x.importByLocal {
		if b != nil && strings.HasPrefix(b.importPath, "@tanstack/angular-query") {
			return true
		}
	}
	return false
}

// angularTanstackInjectKind maps a TanStack Query Angular inject* call leaf to
// its canonical query kind, or "" when the leaf is not a TanStack entry point.
func angularTanstackInjectKind(leaf string) string {
	switch leaf {
	case "injectQuery":
		return "query"
	case "injectMutation":
		return "mutation"
	case "injectInfiniteQuery":
		return "infinite_query"
	case "injectQueries":
		return "queries"
	case "injectQueryClient":
		return "query_client"
	}
	return ""
}

// angularTanstackQuery scans an Angular class body for TanStack Query Angular
// adapter calls (injectQuery/injectMutation/injectInfiniteQuery/…) and emits one
// decorated SCOPE.Operation subtype="tanstack_query" per call, plus a CONTAINS
// edge from the component class (#2910). No-op unless
// @tanstack/angular-query-experimental is imported. Decorate-only (#2839): the
// SCOPE.Operation kind is reused, mirroring how angularDataFetching models
// HttpClient call sites.
func (x *extractor) angularTanstackQuery(body ts.Node, className string) ([]types.EntityRecord, []types.RelationshipRecord) {
	if body == nil || !x.fileImportsTanstackAngular() {
		return nil, nil
	}
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	seen := map[string]bool{}
	for _, call := range findAllNodes(body, "call_expression") {
		leaf := factoryLeaf(x, call)
		kind := angularTanstackInjectKind(leaf)
		if kind == "" {
			continue
		}
		if seen[leaf] {
			continue
		}
		seen[leaf] = true
		start, end := lines(call)
		name := "tanstack." + leaf
		e := types.EntityRecord{
			Name:          name,
			QualifiedName: x.qualify(className + "." + name),
			Kind:          "SCOPE.Operation",
			SourceFile:    x.filePath,
			StartLine:     start,
			EndLine:       end,
			Language:      x.language,
			Subtype:       "tanstack_query",
			Signature:     leaf + "(…)",
			Properties: map[string]string{
				"kind":       "SCOPE.Operation",
				"subtype":    "tanstack_query",
				"via":        propViaTanstackQuery,
				"component":  className,
				"query_kind": kind,
				"query_call": leaf,
				"framework":  "angular",
			},
			EnrichmentStatus: types.StatusPending,
			QualityScore:     0.9,
		}
		e.ID = e.ComputeID()
		ents = append(ents, e)
		rels = append(rels, types.RelationshipRecord{
			ToID: e.ID,
			Kind: "CONTAINS",
			Properties: map[string]string{
				"component": className,
				"subtype":   "tanstack_query",
				"via":       propViaTanstackQuery,
				"framework": "angular",
			},
		})
	}
	return ents, rels
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
func (x *extractor) sagaBodyRoles(r *reactEcosystemImports, body ts.Node) (watcher, worker, epic bool) {
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
func unwrapCall(n ts.Node) ts.Node {
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
func firstStringArg(x *extractor, call ts.Node) string {
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
func configObjectArg(call ts.Node) ts.Node {
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
func objectPairValue(x *extractor, obj ts.Node, key string) ts.Node {
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
func sliceConfigName(x *extractor, call ts.Node) string {
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
func sliceReducers(x *extractor, call ts.Node) ([]string, map[string]ts.Node) {
	obj := configObjectArg(call)
	if obj == nil {
		return nil, nil
	}
	reducersObj := objectPairValue(x, obj, "reducers")
	if reducersObj == nil || reducersObj.Type() != "object" {
		return nil, nil
	}
	var keys []string
	nodes := map[string]ts.Node{}
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
func configureStoreReducerKeys(x *extractor, call ts.Node) []string {
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
func objectKeys(x *extractor, obj ts.Node) []string {
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
	node ts.Node
}

// rtkReducerPath returns the `reducerPath:` string from a createApi config.
func rtkReducerPath(x *extractor, call ts.Node) string {
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
func rtkQueryEndpoints(x *extractor, call ts.Node) []rtkEndpoint {
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
func arrowBody(n ts.Node) ts.Node {
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
func builderEndpointKind(x *extractor, n ts.Node) string {
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
func endpointQueryPath(x *extractor, n ts.Node) string {
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
