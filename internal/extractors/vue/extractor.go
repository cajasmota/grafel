// Package vue implements a regex-based extractor for Vue 3 Single File
// Components (.vue files).
//
// A Vue SFC has up to three top-level sections:
//
//	<template> … </template>   HTML with directives and bindings
//	<script> or <script setup>  JS/TS logic
//	<style [scoped]> … </style> CSS (ignored for entity extraction)
//
// This extractor does NOT rely on tree-sitter (no tree-sitter-vue grammar in
// go-tree-sitter). Instead it uses a layered regex approach:
//
//  1. Component name derived from the filename (PascalCase convention).
//  2. <script> / <script setup> block boundary located via regex + offset.
//  3. Inside the script block:
//     - defineProps / defineEmits / defineExpose (Composition API macros) → SCOPE.Operation
//     - Composition API calls: ref, reactive, computed, watch, watchEffect,
//     onMounted, onUnmounted, provide, inject, nextTick, … → CALLS edges
//     - Vuex / Pinia: useStore, defineStore, mapState, mapGetters, mapActions → CALLS
//     - Vue Router: useRouter, useRoute → CALLS
//     - Options API: components, props, emits, methods → extracted as operations
//     - export default component object → SCOPE.Component (subtype="vue_component")
//  4. <template> block: child component references (<ChildComponent />) → RENDERS edges
//  5. Import statements → IMPORTS edges on the file entity (mirrors JS extractor)
//
// Entity kind mapping (allowlist-compliant):
//
//	File-level entity          → SCOPE.Component (subtype="file")
//	Component (default export) → SCOPE.Component (subtype="vue_component")
//	defineProps call           → SCOPE.Operation  (subtype="define_props")
//	defineEmits call           → SCOPE.Operation  (subtype="define_emits")
//	defineExpose call          → SCOPE.Operation  (subtype="define_expose")
//	Options API method         → SCOPE.Operation  (subtype="method")
//	Composition API setup call → CALLS edge on component entity
//	<ChildComponent /> ref     → RENDERS edge on component entity
//	import statement           → IMPORTS edge on file entity
//
// OTel span: "indexer.extract.vue"
//
// Error handling: on any parse failure the extractor returns the component-name
// entity with quality_score=0.3 and enrichment_status="degraded" — never panics.
package vue

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("vue", &Extractor{})
}

// Extractor implements extractor.Extractor for Vue SFC (.vue) files.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "vue" }

// --- compiled regexps --------------------------------------------------------

var (
	// <script …> or <script setup …> (optional lang="ts"|"js")
	reScriptOpen  = regexp.MustCompile(`(?i)<script(\s[^>]*)?>`)
	reScriptClose = regexp.MustCompile(`(?i)</script>`)

	// <template …>
	reTemplateOpen  = regexp.MustCompile(`(?i)<template(\s[^>]*)?>`)
	reTemplateClose = regexp.MustCompile(`(?i)</template>`)

	// lang="ts" attribute on <script>
	reLangTS = regexp.MustCompile(`(?i)lang=["']ts["']`)

	// setup attribute on <script>
	reSetupAttr = regexp.MustCompile(`(?i)\bsetup\b`)

	// Composition API macros (script setup only)
	reDefineProps  = regexp.MustCompile(`(?m)\bdefineProps\s*[(<]`)
	reDefineEmits  = regexp.MustCompile(`(?m)\bdefineEmits\s*[(<]`)
	reDefineExpose = regexp.MustCompile(`(?m)\bdefineExpose\s*\(`)

	// Composition API calls we capture as CALLS edges
	reCompositionCall = regexp.MustCompile(`(?m)\b(ref|reactive|computed|watch|watchEffect|onMounted|onUnmounted|onBeforeMount|onBeforeUnmount|onUpdated|onBeforeUpdate|onActivated|onDeactivated|provide|inject|nextTick|useRouter|useRoute|useStore|defineStore|mapState|mapGetters|mapActions|useI18n|useFetch|useAsyncData|useNuxtApp|createApp|defineComponent)\s*[(<]`)

	// Import statement: import X from 'y' or import { X } from 'y'
	reImport = regexp.MustCompile(`(?m)^import\s+.+\s+from\s+['"]([^'"]+)['"]`)

	// Options API: method name inside a methods: { … } block
	// Captures standalone identifier followed by ( on a methods-like line
	reOptionsMethod = regexp.MustCompile(`(?m)^\s{2,}(\w+)\s*\(`)

	// PascalCase tag in template: <ComponentName or <ComponentName />
	// Matches tags that start with an uppercase letter (component refs).
	// Avoids matching HTML built-ins which are always lowercase.
	rePascalTag = regexp.MustCompile(`<([A-Z][A-Za-z0-9]*)(?:\s|/>|>)`)

	// export default { … } or export default defineComponent({…})
	reExportDefault = regexp.MustCompile(`(?m)\bexport\s+default\b`)

	// name: 'ComponentName' or name: "ComponentName" inside options object
	reComponentName = regexp.MustCompile(`(?m)\bname\s*:\s*['"]([^'"]+)['"]`)

	// methods: { block — used to scope options method extraction
	reMethodsBlock = regexp.MustCompile(`(?m)\bmethods\s*:`)

	// Context provide/inject (issue #2854 — Structure/context_extraction).
	// provide(KEY, value) / provide('key', value) — the first argument is the
	// injection key (Symbol identifier or string literal).
	reProvide = regexp.MustCompile(`(?m)\bprovide\s*\(\s*([A-Za-z_$][A-Za-z0-9_$]*|['"][^'"]+['"])`)
	// inject(KEY) / inject('key', default) — first argument is the key.
	reInject = regexp.MustCompile(`(?m)\binject\s*\(\s*([A-Za-z_$][A-Za-z0-9_$]*|['"][^'"]+['"])`)

	// Composable usage (issue #2854 — Structure/hook_recognition). Vue
	// composables follow the `useXxx` convention, the framework's hook
	// analogue. We capture imported/local composable call sites. The built-in
	// useRouter/useRoute/useStore/useI18n/etc. are already captured by
	// reCompositionCall as CALLS; here we additionally model the composable as
	// a hook USE so the hook_recognition cell has a dedicated signal.
	reComposableCall = regexp.MustCompile(`(?m)\b(use[A-Z][A-Za-z0-9_$]*)\s*\(`)

	// Composable definition: `function useFoo(` / `const useFoo = (` —
	// a local custom composable (hook) declaration.
	reComposableDef = regexp.MustCompile(`(?m)\b(?:function\s+(use[A-Z][A-Za-z0-9_$]*)|(?:const|let)\s+(use[A-Z][A-Za-z0-9_$]*)\s*=\s*(?:async\s*)?(?:function|\())`)

	// ── Data Flow (issue #2855) ──────────────────────────────────────────────

	// defineProps destructured / Options-API prop names. We parse the props
	// surface in two shapes:
	//   defineProps<{ a: string; b?: number }>()   (type-literal generic)
	//   defineProps({ a: String, b: Number })       (runtime object)
	//   props: { a: …, b: … }  / props: ['a','b']   (Options API)
	reDefinePropsGeneric = regexp.MustCompile(`(?s)\bdefineProps\s*<\s*\{(.*?)\}\s*>\s*\(`)
	reDefinePropsObject  = regexp.MustCompile(`(?s)\bdefineProps\s*\(\s*\{(.*?)\}\s*\)`)
	reDefinePropsArray   = regexp.MustCompile(`(?s)\bdefineProps\s*\(\s*\[(.*?)\]\s*\)`)
	reOptionsPropsObject = regexp.MustCompile(`(?s)\bprops\s*:\s*\{(.*?)\}`)
	reOptionsPropsArray  = regexp.MustCompile(`(?s)\bprops\s*:\s*\[(.*?)\]`)
	// A property declaration inside a props block, e.g. `title: String`,
	// `count?: number`. Anchored on a member separator (start, `;`, `,`, or
	// newline) so multiple props on one line (`{ a: string; b: number }`) are
	// all captured, not just the first.
	rePropName = regexp.MustCompile(`(?:^|[;,\n{])\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*[?:]`)
	// Quoted string entries for the array form: 'title', "count".
	reQuotedName = regexp.MustCompile(`['"]([A-Za-z_$][A-Za-z0-9_$]*)['"]`)

	// Pinia state stores: `const x = useXxxStore()` / defineStore('id', …) /
	// storeToRefs(store). Pinia is Vue's canonical state container.
	rePiniaStore     = regexp.MustCompile(`(?m)\b(use[A-Z][A-Za-z0-9_$]*Store)\s*\(`)
	rePiniaDefine    = regexp.MustCompile(`(?m)\bdefineStore\s*\(\s*['"]([^'"]+)['"]`)
	rePiniaStoreRefs = regexp.MustCompile(`(?m)\bstoreToRefs\s*\(`)
	// Reactive local state: ref(…) / reactive(…). The Composition-API reactivity
	// primitives are also state_management signals.
	reReactiveState = regexp.MustCompile(`(?m)\b(?:const|let)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*(ref|reactive|shallowRef|shallowReactive)\s*\(`)

	// Data fetching: fetch()/axios.*/useFetch()/useAsyncData()/$fetch(). The
	// composable forms (useFetch/useAsyncData) are Nuxt's data layer; fetch/
	// axios are the generic browser/lib clients.
	reDataFetch = regexp.MustCompile(`(?m)\b(fetch|\$fetch|useFetch|useAsyncData|useLazyFetch|useLazyAsyncData)\s*\(|\baxios\s*(?:\.\s*(get|post|put|patch|delete|request))?\s*\(`)

	// Template branch conditions: v-if / v-else-if / v-else / v-show. These are
	// Vue's conditional-rendering directives (branch_conditions).
	reVueBranch = regexp.MustCompile(`\b(v-if|v-else-if|v-else|v-show)\b`)

	// ── Template directives (issue #2876 — Vue Internals/directive_recognition) ─
	// Vue template directives are `v-<name>` attributes. We recognise the full
	// built-in set plus user-defined ones. The branch directives (v-if/v-else/
	// v-show) are also captured by reVueBranch for Data-Flow branch_conditions;
	// here we model the directive itself as a first-class template idiom. We
	// capture the directive name and (for v-on:click / @click and :prop / v-bind)
	// the argument so the directive entity carries the bound event/attribute.
	//   v-model / v-model:value
	//   v-for="item in items"
	//   v-bind:href / :href            (shorthand `:`)
	//   v-on:click  / @click           (shorthand `@`)
	//   v-html / v-text / v-slot / v-cloak / v-once / v-pre / v-memo
	//   custom: v-focus, v-tooltip, …
	reVueDirective = regexp.MustCompile(`(?:\b(v-[a-z][a-z0-9-]*)(?::([a-zA-Z][\w.-]*))?|(?:^|\s)([:@])([a-zA-Z][\w.-]*))(?:=|\s|>|/)`)

	// ── Slots (issue #2876 — Vue Internals/slot_extraction) ──────────────────
	// Slot outlets declared in a child component's <template>:
	//   <slot />                      default slot
	//   <slot name="header" />        named slot
	// Slot content provided by a parent at a usage site:
	//   <template #header>            shorthand named-slot
	//   <template v-slot:footer>      explicit v-slot
	//   <template v-slot="{ row }">   default scoped slot
	reSlotOutlet = regexp.MustCompile(`(?i)<slot\b([^>]*?)/?>`)
	reSlotName   = regexp.MustCompile(`(?i)\bname\s*=\s*["']([^"']+)["']`)
	reSlotUse    = regexp.MustCompile(`(?i)<template\b[^>]*?(?:#([a-zA-Z][\w.-]*)|v-slot:([a-zA-Z][\w.-]*)|(v-slot)\b)`)

	// ── Navigation (issue #2856) ─────────────────────────────────────────────

	// vue-router route table: createRouter({ routes: [ … ] }). We locate the
	// createRouter call and then scan the routes array for `path:` entries.
	reCreateRouter = regexp.MustCompile(`\bcreateRouter\s*\(`)
	// A `path: '/segment'` entry inside a vue-router route record.
	reVueRoutePath = regexp.MustCompile(`\bpath\s*:\s*['"]([^'"]*)['"]`)
	// Imperative navigation: router.push('/x') / router.replace('/x') /
	// router.push({ name: 'x' }). Capture the method and the first argument's
	// leading string literal when present.
	reVueRouterNav = regexp.MustCompile(`(?m)\b(?:router|\$router)\s*\.\s*(push|replace)\s*\(\s*(?:['"]([^'"]*)['"]|\{[^}]*\bname\s*:\s*['"]([^'"]*)['"])`)
	// Template <router-link to="/x"> / <RouterLink :to="…"> / <nuxt-link
	// to="/x">. Capture the `to` attribute string value.
	reRouterLink = regexp.MustCompile(`(?si)<(?:router-link|RouterLink|nuxt-link|NuxtLink)\b[^>]*?\b:?to\s*=\s*"([^"]*)"`)

	// ── Lifecycle / state_setter_emission (issue #2856) ──────────────────────

	// Pinia/component state setter via $patch: store.$patch({ … }) /
	// store.$patch(fn). The receiver is the store binding name.
	reVuePatch = regexp.MustCompile(`(?m)\b([A-Za-z_$][A-Za-z0-9_$]*)\s*\.\s*\$patch\s*\(`)
	// ref().value assignment: `count.value = …` / `count.value += …`. The
	// captured identifier is the ref binding being mutated.
	reVueRefAssign = regexp.MustCompile(`(?m)\b([A-Za-z_$][A-Za-z0-9_$]*)\s*\.\s*value\s*(?:=|\+=|-=|\*=|/=|\?\?=|\|\|=|&&=)\s*[^=]`)
)

// jsKeywords are identifiers that appear before "(" but are NOT method
// declarations (control flow, builtins, etc.).
var jsKeywords = map[string]bool{
	"if": true, "else": true, "while": true, "for": true, "switch": true,
	"return": true, "throw": true, "try": true, "catch": true, "finally": true,
	"function": true, "class": true, "new": true, "typeof": true, "instanceof": true,
	"void": true, "delete": true, "await": true, "async": true, "yield": true,
	"import": true, "export": true, "default": true, "const": true, "let": true,
	"var": true, "of": true, "in": true, "super": true, "this": true,
	"constructor": true, "get": true, "set": true, "static": true,
	"true": true, "false": true, "null": true, "undefined": true,
}

// Extract processes a .vue SFC and returns entity records.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) (entities []types.EntityRecord, retErr error) {
	tracer := otel.Tracer("extractor.vue")
	ctx, span := tracer.Start(ctx, "indexer.extract.vue",
		trace.WithAttributes(
			attribute.String("file", file.Path),
			attribute.Int("file_size_bytes", len(file.Content)),
		),
	)
	defer func() {
		span.SetAttributes(attribute.Int("entity_count", len(entities)))
		span.End()
	}()
	_ = ctx

	componentName := componentNameFromPath(file.Path)
	src := string(file.Content)

	degraded := func(reason string) []types.EntityRecord {
		return []types.EntityRecord{
			{
				Name:             componentName,
				Kind:             "SCOPE.Component",
				Subtype:          "vue_component",
				SourceFile:       file.Path,
				Language:         "vue",
				QualityScore:     0.3,
				EnrichmentStatus: types.StatusDegraded,
				Metadata: map[string]interface{}{
					"extraction_status": "degraded",
					"degraded_reason":   reason,
				},
			},
		}
	}

	if len(file.Content) == 0 {
		return degraded("empty file"), nil
	}

	// --- 1. File entity (mirrors JS extractor — cross-repo import linker) ----
	fileEntity := extractor.FileEntity(file)
	entities = append(entities, fileEntity)

	// --- 2. Component entity (default export) --------------------------------
	// Try to read the component name from `name: '...'` in the options object;
	// fall back to filename stem.
	optionsName := ""
	if m := reComponentName.FindStringSubmatch(src); m != nil {
		optionsName = m[1]
	}
	if optionsName != "" {
		componentName = optionsName
	}

	hasExportDefault := reExportDefault.MatchString(src)
	componentQuality := 0.85
	if !hasExportDefault {
		componentQuality = 0.7
	}

	componentEntity := types.EntityRecord{
		Name:               componentName,
		QualifiedName:      componentName,
		Kind:               "SCOPE.Component",
		Subtype:            "vue_component",
		SourceFile:         file.Path,
		Language:           "vue",
		QualityScore:       componentQuality,
		EnrichmentStatus:   types.StatusPending,
		EnrichmentRequired: false,
		Properties: map[string]string{
			"component_name": componentName,
			"framework":      "vue",
		},
	}
	// We'll attach edges to this entity; remember its index.
	componentIdx := len(entities)
	entities = append(entities, componentEntity)

	// --- 3. Extract <script> block -------------------------------------------
	scriptSrc, scriptOffset, isSetup := extractScriptBlock(src)

	if scriptSrc != "" {
		// 3a. Import → IMPORTS edges on file entity (index 0)
		importEntities := buildVueImportEntities(file.Path, scriptSrc, scriptOffset, src)
		entities = append(entities, importEntities...)

		// 3b. defineProps / defineEmits / defineExpose (script setup macros)
		if isSetup {
			macros := extractSetupMacros(scriptSrc, scriptOffset, src, file.Path, componentName)
			// Attach CONTAINS edges from component to each macro operation.
			for i := range macros {
				opRef := extractor.BuildOperationStructuralRef("vue", file.Path, macros[i].Name)
				entities[componentIdx].Relationships = append(entities[componentIdx].Relationships, types.RelationshipRecord{
					ToID: opRef,
					Kind: "CONTAINS",
				})
			}
			entities = append(entities, macros...)
		}

		// 3c. Composition API and Options API calls → CALLS edges
		calls := extractScriptCalls(scriptSrc, componentName)
		entities[componentIdx].Relationships = append(entities[componentIdx].Relationships, calls...)

		// 3c-i. Context provide/inject → context entities + edges (#2854).
		ctxEntities, ctxRels := extractContext(scriptSrc, scriptOffset, src, file.Path, componentName)
		entities[componentIdx].Relationships = append(entities[componentIdx].Relationships, ctxRels...)
		entities = append(entities, ctxEntities...)

		// 3c-ii. Composables (hooks) → hook entities + USES_HOOK edges (#2854).
		hookEntities, hookRels := extractComposables(scriptSrc, scriptOffset, src, file.Path, componentName)
		entities[componentIdx].Relationships = append(entities[componentIdx].Relationships, hookRels...)
		entities = append(entities, hookEntities...)

		// 3c-iii. Data Flow (#2855): prop_extraction, state_management,
		// data_fetching. Each emits SCOPE.Operation entities plus CONTAINS
		// edges from the component.
		dfEntities, dfRels := extractDataFlow(scriptSrc, scriptOffset, src, file.Path, componentName)
		entities[componentIdx].Relationships = append(entities[componentIdx].Relationships, dfRels...)
		entities = append(entities, dfEntities...)

		// 3c-iv. Navigation (#2856) — vue-router route table (createRouter)
		// and imperative router.push/replace. Both emit NAVIGATES_TO edges
		// on the component entity.
		navRels := extractScriptNavigation(scriptSrc, scriptOffset, src, componentName)
		entities[componentIdx].Relationships = append(entities[componentIdx].Relationships, navRels...)

		// 3c-v. Lifecycle (#2856) — state_setter_emission: ref().value
		// assignments and Pinia store.$patch mutations each emit a
		// SCOPE.Operation subtype="state_setter" with a WRITES_TO edge to
		// the state it mutates.
		setterEnts := extractStateSetters(scriptSrc, scriptOffset, src, file.Path, componentName)
		for i := range setterEnts {
			opRef := extractor.BuildOperationStructuralRef("vue", file.Path, setterEnts[i].Name)
			entities[componentIdx].Relationships = append(entities[componentIdx].Relationships, types.RelationshipRecord{
				ToID: opRef,
				Kind: "CONTAINS",
			})
		}
		entities = append(entities, setterEnts...)

		// 3d. Options API methods (inside methods: { … })
		if !isSetup {
			methods := extractOptionsMethods(scriptSrc, scriptOffset, src, file.Path, componentName)
			for i := range methods {
				opRef := extractor.BuildOperationStructuralRef("vue", file.Path, methods[i].Name)
				entities[componentIdx].Relationships = append(entities[componentIdx].Relationships, types.RelationshipRecord{
					ToID: opRef,
					Kind: "CONTAINS",
				})
			}
			entities = append(entities, methods...)
		}
	}

	// --- 4. Extract <template> block → RENDERS + branch_condition ------------
	templateSrc, templateOffset, ok := extractTemplateBlock(src)
	if ok {
		renders := extractTemplateRenders(templateSrc, componentName)
		entities[componentIdx].Relationships = append(entities[componentIdx].Relationships, renders...)

		// Data Flow (#2855) — branch_conditions from v-if/v-else/v-show.
		brEntities, brRels := extractBranchConditions(templateSrc, templateOffset, src, file.Path, componentName)
		entities[componentIdx].Relationships = append(entities[componentIdx].Relationships, brRels...)
		entities = append(entities, brEntities...)

		// Navigation (#2856) — <router-link to="…"> directives emit
		// NAVIGATES_TO edges.
		linkRels := extractRouterLinks(templateSrc, templateOffset, src, componentName)
		entities[componentIdx].Relationships = append(entities[componentIdx].Relationships, linkRels...)

		// Vue Internals (#2876) — directive_recognition: v-model/v-for/v-bind/
		// v-on/etc. template directives → SCOPE.Operation subtype="directive"
		// with CONTAINS edges from the component.
		dirEntities, dirRels := extractDirectives(templateSrc, templateOffset, src, file.Path, componentName)
		entities[componentIdx].Relationships = append(entities[componentIdx].Relationships, dirRels...)
		entities = append(entities, dirEntities...)

		// Vue Internals (#2876) — slot_extraction: <slot>/<slot name="x"> outlets
		// and <template #x>/<template v-slot:x> usage → SCOPE.Operation
		// subtype="slot" with CONTAINS edges from the component.
		slotEntities, slotRels := extractSlots(templateSrc, templateOffset, src, file.Path, componentName)
		entities[componentIdx].Relationships = append(entities[componentIdx].Relationships, slotRels...)
		entities = append(entities, slotEntities...)
	}

	// --- 5. Tag relationships with language ----------------------------------
	extractor.TagRelationshipsLanguage(entities, "vue")
	extractor.TagEntitiesLanguage(entities, "vue")

	return entities, nil
}

// extractScriptBlock locates the first <script> or <script setup> section.
// Returns (content, byte-offset-of-content-start, isSetup).
func extractScriptBlock(src string) (content string, offset int, isSetup bool) {
	openLoc := reScriptOpen.FindStringIndex(src)
	if openLoc == nil {
		return "", 0, false
	}
	openTag := src[openLoc[0]:openLoc[1]]
	isSetup = reSetupAttr.MatchString(openTag)

	// Content starts after the closing >
	contentStart := openLoc[1]
	closeLoc := reScriptClose.FindStringIndex(src[contentStart:])
	if closeLoc == nil {
		return "", 0, false
	}
	contentEnd := contentStart + closeLoc[0]
	return src[contentStart:contentEnd], contentStart, isSetup
}

// reTopTemplateOpen matches a top-level SFC <template …> block — anchored to
// the start of a line so a `<template>` mention inside a <script> comment or a
// nested (indented) slot template is not mistaken for the root block.
var reTopTemplateOpen = regexp.MustCompile(`(?im)^<template(\s[^>]*)?>`)

// extractTemplateBlock locates the outermost <template> section. Vue templates
// nest <template> elements (slot content via <template #slot> / v-slot), so the
// closing tag must be matched at the same depth as the opening one rather than
// stopping at the first </template>. The root block is found via a line-anchored
// match so a `<template>` literal inside a <script> comment is not picked up.
func extractTemplateBlock(src string) (content string, offset int, found bool) {
	openLoc := reTopTemplateOpen.FindStringIndex(src)
	if openLoc == nil {
		// Fall back to a non-anchored match for minified/one-line SFCs.
		openLoc = reTemplateOpen.FindStringIndex(src)
	}
	if openLoc == nil {
		return "", 0, false
	}
	contentStart := openLoc[1]

	// Walk forward, tracking <template …> open / </template> close depth.
	depth := 1
	pos := contentStart
	for depth > 0 {
		rest := src[pos:]
		nextOpen := reTemplateOpen.FindStringIndex(rest)
		nextClose := reTemplateClose.FindStringIndex(rest)
		if nextClose == nil {
			return "", 0, false
		}
		if nextOpen != nil && nextOpen[0] < nextClose[0] {
			depth++
			pos += nextOpen[1]
			continue
		}
		depth--
		if depth == 0 {
			return src[contentStart : pos+nextClose[0]], contentStart, true
		}
		pos += nextClose[1]
	}
	return "", 0, false
}

// extractSetupMacros scans a <script setup> block for defineProps, defineEmits,
// and defineExpose calls and emits SCOPE.Operation entities.
func extractSetupMacros(scriptSrc string, scriptOffset int, fullSrc, filePath, componentName string) []types.EntityRecord {
	var out []types.EntityRecord

	type macro struct {
		re      *regexp.Regexp
		name    string
		subtype string
	}
	macros := []macro{
		{reDefineProps, "defineProps", "define_props"},
		{reDefineEmits, "defineEmits", "define_emits"},
		{reDefineExpose, "defineExpose", "define_expose"},
	}

	for _, m := range macros {
		loc := m.re.FindStringIndex(scriptSrc)
		if loc == nil {
			continue
		}
		lineNum := lineOf(fullSrc, scriptOffset+loc[0])
		out = append(out, types.EntityRecord{
			Name:             m.name,
			QualifiedName:    fmt.Sprintf("%s.%s", componentName, m.name),
			Kind:             "SCOPE.Operation",
			Subtype:          m.subtype,
			SourceFile:       filePath,
			Language:         "vue",
			StartLine:        lineNum,
			EndLine:          lineNum,
			QualityScore:     0.85,
			EnrichmentStatus: types.StatusPending,
			Properties: map[string]string{
				"component":  componentName,
				"macro_name": m.name,
			},
		})
	}
	return out
}

// extractScriptCalls scans the script content for Composition API / Vuex /
// Pinia / Vue Router call patterns and returns CALLS edges.
func extractScriptCalls(scriptSrc, componentName string) []types.RelationshipRecord {
	matches := reCompositionCall.FindAllStringSubmatch(scriptSrc, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]types.RelationshipRecord, 0, len(matches))
	for _, m := range matches {
		callee := m[1]
		if callee == "" || seen[callee] {
			continue
		}
		seen[callee] = true
		out = append(out, types.RelationshipRecord{
			ToID: callee,
			Kind: "CALLS",
			Properties: map[string]string{
				"caller":    componentName,
				"framework": "vue",
			},
		})
	}
	return out
}

// extractOptionsMethods scans an Options API script block for method
// declarations inside a `methods: { ... }` block and returns SCOPE.Operation
// entities.
func extractOptionsMethods(scriptSrc string, scriptOffset int, fullSrc, filePath, componentName string) []types.EntityRecord {
	// Find methods: block
	methodsLoc := reMethodsBlock.FindStringIndex(scriptSrc)
	if methodsLoc == nil {
		return nil
	}

	// Find the opening brace after `methods:`
	afterMethods := scriptSrc[methodsLoc[1]:]
	braceIdx := strings.IndexByte(afterMethods, '{')
	if braceIdx < 0 {
		return nil
	}
	blockStart := methodsLoc[1] + braceIdx + 1

	// Find the closing brace via brace counting
	blockEnd := findClosingBrace(scriptSrc, blockStart)
	if blockEnd <= blockStart {
		return nil
	}

	methodsBlock := scriptSrc[blockStart:blockEnd]
	var out []types.EntityRecord
	seen := make(map[string]bool)

	for _, m := range reOptionsMethod.FindAllStringSubmatchIndex(methodsBlock, -1) {
		if len(m) < 4 {
			continue
		}
		name := methodsBlock[m[2]:m[3]]
		if name == "" || jsKeywords[name] || seen[name] {
			continue
		}
		seen[name] = true
		absOffset := scriptOffset + blockStart + m[0]
		lineNum := lineOf(fullSrc, absOffset)
		out = append(out, types.EntityRecord{
			Name:             name,
			QualifiedName:    fmt.Sprintf("%s.%s", componentName, name),
			Kind:             "SCOPE.Operation",
			Subtype:          "method",
			SourceFile:       filePath,
			Language:         "vue",
			StartLine:        lineNum,
			EndLine:          lineNum,
			QualityScore:     0.85,
			EnrichmentStatus: types.StatusPending,
			Properties: map[string]string{
				"component":   componentName,
				"method_name": name,
			},
		})
	}
	return out
}

// extractTemplateRenders scans the template block for PascalCase component
// references and emits RENDERS edges.
func extractTemplateRenders(templateSrc, componentName string) []types.RelationshipRecord {
	matches := rePascalTag.FindAllStringSubmatch(templateSrc, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]types.RelationshipRecord, 0, len(matches))
	for _, m := range matches {
		tag := m[1]
		if tag == "" || seen[tag] || tag == componentName {
			continue
		}
		seen[tag] = true
		out = append(out, types.RelationshipRecord{
			ToID: tag,
			Kind: "RENDERS",
			Properties: map[string]string{
				"renderer":  componentName,
				"framework": "vue",
			},
		})
	}
	return out
}

// extractContext scans a <script> block for Vue dependency-injection context
// provide()/inject() calls (issue #2854 — Structure/context_extraction). Each
// distinct injection key yields a SCOPE.Operation entity (subtype
// "provide_context" or "inject_context") and a USES edge from the component to
// that context operation, with a "role" property distinguishing provider from
// consumer.
func extractContext(scriptSrc string, scriptOffset int, fullSrc, filePath, componentName string) ([]types.EntityRecord, []types.RelationshipRecord) {
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	seen := map[string]bool{}

	emit := func(re *regexp.Regexp, subtype, role string) {
		for _, m := range re.FindAllStringSubmatchIndex(scriptSrc, -1) {
			if len(m) < 4 {
				continue
			}
			key := normalizeContextKey(scriptSrc[m[2]:m[3]])
			if key == "" {
				continue
			}
			dedupe := role + ":" + key
			if seen[dedupe] {
				continue
			}
			seen[dedupe] = true
			lineNum := lineOf(fullSrc, scriptOffset+m[0])
			name := role + ":" + key
			ents = append(ents, types.EntityRecord{
				Name:             name,
				QualifiedName:    fmt.Sprintf("%s.%s", componentName, name),
				Kind:             "SCOPE.Operation",
				Subtype:          subtype,
				SourceFile:       filePath,
				Language:         "vue",
				StartLine:        lineNum,
				EndLine:          lineNum,
				QualityScore:     0.8,
				EnrichmentStatus: types.StatusPending,
				Properties: map[string]string{
					"component":    componentName,
					"context_key":  key,
					"context_role": role,
					"framework":    "vue",
				},
			})
			rels = append(rels, types.RelationshipRecord{
				ToID: name,
				Kind: "USES",
				Properties: map[string]string{
					"component":    componentName,
					"context_key":  key,
					"context_role": role,
					"framework":    "vue",
				},
			})
		}
	}

	emit(reProvide, "provide_context", "provider")
	emit(reInject, "inject_context", "consumer")
	return ents, rels
}

// normalizeContextKey strips quotes from a string-literal injection key, or
// returns a Symbol/identifier key unchanged.
func normalizeContextKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 {
		f, l := raw[0], raw[len(raw)-1]
		if (f == '\'' || f == '"') && f == l {
			return raw[1 : len(raw)-1]
		}
	}
	return raw
}

// extractComposables scans a <script> block for Vue composable (hook) usage and
// local composable definitions (issue #2854 — Structure/hook_recognition). A
// `useXxx(` call site emits a USES_HOOK edge from the component to the
// composable; a local `function useXxx`/`const useXxx =` definition emits a
// SCOPE.Operation entity subtype="vue_composable".
func extractComposables(scriptSrc string, scriptOffset int, fullSrc, filePath, componentName string) ([]types.EntityRecord, []types.RelationshipRecord) {
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord

	// Local composable definitions.
	defined := map[string]bool{}
	for _, m := range reComposableDef.FindAllStringSubmatchIndex(scriptSrc, -1) {
		name := ""
		if m[2] >= 0 {
			name = scriptSrc[m[2]:m[3]]
		} else if len(m) >= 6 && m[4] >= 0 {
			name = scriptSrc[m[4]:m[5]]
		}
		if name == "" || defined[name] {
			continue
		}
		defined[name] = true
		lineNum := lineOf(fullSrc, scriptOffset+m[0])
		ents = append(ents, types.EntityRecord{
			Name:             name,
			QualifiedName:    fmt.Sprintf("%s.%s", componentName, name),
			Kind:             "SCOPE.Operation",
			Subtype:          "vue_composable",
			SourceFile:       filePath,
			Language:         "vue",
			StartLine:        lineNum,
			EndLine:          lineNum,
			QualityScore:     0.8,
			EnrichmentStatus: types.StatusPending,
			Properties: map[string]string{
				"component": componentName,
				"framework": "vue",
			},
		})
	}

	// Composable call sites → USES_HOOK edges.
	seen := map[string]bool{}
	for _, m := range reComposableCall.FindAllStringSubmatch(scriptSrc, -1) {
		hook := m[1]
		if hook == "" || hook == componentName || seen[hook] {
			continue
		}
		seen[hook] = true
		rels = append(rels, types.RelationshipRecord{
			ToID: hook,
			Kind: "USES_HOOK",
			Properties: map[string]string{
				"consumer":  componentName,
				"hook":      hook,
				"framework": "vue",
			},
		})
	}
	return ents, rels
}

// extractDataFlow scans a Vue <script> block for Data-Flow signals (issue
// #2855): component props (defineProps / Options-API props), state management
// (Pinia stores + ref/reactive primitives), and data fetching (fetch / axios /
// useFetch / useAsyncData). It returns SCOPE.Operation entities and CONTAINS
// edges from the component to each.
func extractDataFlow(scriptSrc string, scriptOffset int, fullSrc, filePath, componentName string) ([]types.EntityRecord, []types.RelationshipRecord) {
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord

	emit := func(name, subtype, sig string, props map[string]string, absOffset int) {
		if name == "" {
			return
		}
		lineNum := lineOf(fullSrc, absOffset)
		props["component"] = componentName
		props["framework"] = "vue"
		ent := types.EntityRecord{
			Name:             name,
			QualifiedName:    fmt.Sprintf("%s.%s", componentName, name),
			Kind:             "SCOPE.Operation",
			Subtype:          subtype,
			SourceFile:       filePath,
			Language:         "vue",
			StartLine:        lineNum,
			EndLine:          lineNum,
			Signature:        sig,
			QualityScore:     0.8,
			EnrichmentStatus: types.StatusPending,
			Properties:       props,
		}
		ents = append(ents, ent)
		rels = append(rels, types.RelationshipRecord{
			ToID: name,
			Kind: "CONTAINS",
			Properties: map[string]string{
				"component": componentName,
				"framework": "vue",
				"subtype":   subtype,
			},
		})
	}

	// ── prop_extraction ──────────────────────────────────────────────────────
	propSeen := map[string]bool{}
	emitProp := func(name string, off int) {
		name = strings.TrimSpace(name)
		if name == "" || propSeen[name] {
			return
		}
		propSeen[name] = true
		emit(name, "component_prop", "prop "+name, map[string]string{"prop": name}, off)
	}
	// defineProps<{ … }>() — type-literal generic.
	if m := reDefinePropsGeneric.FindStringSubmatchIndex(scriptSrc); m != nil {
		block := scriptSrc[m[2]:m[3]]
		for _, pm := range rePropName.FindAllStringSubmatch(block, -1) {
			emitProp(pm[1], scriptOffset+m[2])
		}
	}
	// defineProps({ … }) — runtime object.
	if m := reDefinePropsObject.FindStringSubmatchIndex(scriptSrc); m != nil {
		block := scriptSrc[m[2]:m[3]]
		for _, pm := range rePropName.FindAllStringSubmatch(block, -1) {
			emitProp(pm[1], scriptOffset+m[2])
		}
	}
	// defineProps(['a','b']) — array of names.
	if m := reDefinePropsArray.FindStringSubmatchIndex(scriptSrc); m != nil {
		block := scriptSrc[m[2]:m[3]]
		for _, pm := range reQuotedName.FindAllStringSubmatch(block, -1) {
			emitProp(pm[1], scriptOffset+m[2])
		}
	}
	// Options API: props: { … } or props: [ … ].
	if m := reOptionsPropsObject.FindStringSubmatchIndex(scriptSrc); m != nil {
		block := scriptSrc[m[2]:m[3]]
		for _, pm := range rePropName.FindAllStringSubmatch(block, -1) {
			emitProp(pm[1], scriptOffset+m[2])
		}
	}
	if m := reOptionsPropsArray.FindStringSubmatchIndex(scriptSrc); m != nil {
		block := scriptSrc[m[2]:m[3]]
		for _, pm := range reQuotedName.FindAllStringSubmatch(block, -1) {
			emitProp(pm[1], scriptOffset+m[2])
		}
	}

	// ── state_management ─────────────────────────────────────────────────────
	stateSeen := map[string]bool{}
	for _, m := range rePiniaStore.FindAllStringSubmatchIndex(scriptSrc, -1) {
		name := scriptSrc[m[2]:m[3]]
		if stateSeen["store:"+name] {
			continue
		}
		stateSeen["store:"+name] = true
		emit(name, "state_store", "pinia store "+name,
			map[string]string{"state_lib": "pinia", "store": name}, scriptOffset+m[0])
	}
	// pinia_store (#2890): every defineStore('id', …) becomes a dedicated store
	// entity plus its state/getters/actions members (store → member CONTAINS),
	// with a component → store CONTAINS edge — replacing the thin single
	// state_store op that left pinia_store at `partial` in #2876.
	storeEnts, storeRels := extractPiniaStores(scriptSrc, scriptOffset, fullSrc, filePath, componentName)
	ents = append(ents, storeEnts...)
	rels = append(rels, storeRels...)
	for _, m := range reReactiveState.FindAllStringSubmatchIndex(scriptSrc, -1) {
		name := scriptSrc[m[2]:m[3]]
		prim := scriptSrc[m[4]:m[5]]
		if stateSeen["reactive:"+name] {
			continue
		}
		stateSeen["reactive:"+name] = true
		emit(name, "reactive_state", prim+"() "+name,
			map[string]string{"state_lib": "vue-reactivity", "primitive": prim}, scriptOffset+m[0])
	}

	// ── data_fetching ──────────────────────────────────────────────────────────
	fetchSeen := map[string]bool{}
	for _, m := range reDataFetch.FindAllStringSubmatchIndex(scriptSrc, -1) {
		matched := strings.TrimSpace(scriptSrc[m[0]:m[1]])
		// Normalise to the call kind: strip trailing "(" / whitespace.
		kind := matched
		if idx := strings.IndexByte(kind, '('); idx >= 0 {
			kind = strings.TrimSpace(kind[:idx])
		}
		kind = strings.TrimSpace(kind)
		if kind == "" || fetchSeen[kind] {
			continue
		}
		fetchSeen[kind] = true
		safe := strings.NewReplacer("$", "d_", ".", "_", " ", "").Replace(kind)
		emit("fetch:"+safe, "data_fetch", kind+"(…)",
			map[string]string{"fetch_kind": kind}, scriptOffset+m[0])
	}

	return ents, rels
}

// extractBranchConditions scans the <template> block for Vue conditional-
// rendering directives (v-if/v-else-if/v-else/v-show) and returns
// SCOPE.Operation subtype="branch_condition" entities plus CONTAINS edges
// (issue #2855 — Data Flow / branch_conditions).
func extractBranchConditions(templateSrc string, templateOffset int, fullSrc, filePath, componentName string) ([]types.EntityRecord, []types.RelationshipRecord) {
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	seen := map[string]bool{}
	for _, m := range reVueBranch.FindAllStringSubmatchIndex(templateSrc, -1) {
		kind := templateSrc[m[2]:m[3]]
		if kind == "" || seen[kind] {
			continue
		}
		seen[kind] = true
		lineNum := lineOf(fullSrc, templateOffset+m[0])
		safe := strings.ReplaceAll(kind, "-", "_")
		ents = append(ents, types.EntityRecord{
			Name:             safe,
			QualifiedName:    fmt.Sprintf("%s.%s", componentName, safe),
			Kind:             "SCOPE.Operation",
			Subtype:          "branch_condition",
			SourceFile:       filePath,
			Language:         "vue",
			StartLine:        lineNum,
			EndLine:          lineNum,
			Signature:        "template " + kind,
			QualityScore:     0.8,
			EnrichmentStatus: types.StatusPending,
			Properties: map[string]string{
				"component":   componentName,
				"branch_kind": kind,
				"framework":   "vue",
			},
		})
		rels = append(rels, types.RelationshipRecord{
			ToID: safe,
			Kind: "CONTAINS",
			Properties: map[string]string{
				"component":   componentName,
				"branch_kind": kind,
				"framework":   "vue",
			},
		})
	}
	return ents, rels
}

// extractDirectives scans the <template> block for Vue template directives
// (issue #2876 — Vue Internals/directive_recognition) and returns
// SCOPE.Operation subtype="directive" entities plus CONTAINS edges from the
// component. Vue's directives are `v-<name>` attributes (v-model, v-for,
// v-bind, v-on, v-html, v-slot, custom directives, …) together with their
// shorthands `:attr` (v-bind) and `@event` (v-on). Each distinct directive
// (keyed by directive + argument) yields one entity so the component's template
// surface is introspectable. The `directive` property holds the canonical
// directive name (shorthand normalised to v-bind / v-on); `arg` holds the
// bound argument when present (e.g. v-on:click → arg="click").
func extractDirectives(templateSrc string, templateOffset int, fullSrc, filePath, componentName string) ([]types.EntityRecord, []types.RelationshipRecord) {
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	seen := map[string]bool{}

	for _, m := range reVueDirective.FindAllStringSubmatchIndex(templateSrc, -1) {
		var directive, arg string
		off := m[0]
		switch {
		case m[2] >= 0: // v-<name>[:arg]
			directive = templateSrc[m[2]:m[3]]
			if m[4] >= 0 {
				arg = templateSrc[m[4]:m[5]]
			}
		case m[6] >= 0: // shorthand `:` or `@`
			short := templateSrc[m[6]:m[7]]
			if m[8] >= 0 {
				arg = templateSrc[m[8]:m[9]]
			}
			if short == "@" {
				directive = "v-on"
			} else {
				directive = "v-bind"
			}
			// Shorthand match consumes a leading whitespace; align the offset to
			// the sigil itself for accurate line numbers.
			off = m[6]
		}
		if directive == "" {
			continue
		}
		key := directive
		if arg != "" {
			key = directive + ":" + arg
		}
		if seen[key] {
			continue
		}
		seen[key] = true

		safe := strings.NewReplacer("-", "_", ":", "_", ".", "_").Replace(key)
		lineNum := lineOf(fullSrc, templateOffset+off)
		props := map[string]string{
			"component": componentName,
			"directive": directive,
			"framework": "vue",
		}
		if arg != "" {
			props["arg"] = arg
		}
		ents = append(ents, types.EntityRecord{
			Name:             safe,
			QualifiedName:    fmt.Sprintf("%s.%s", componentName, safe),
			Kind:             "SCOPE.Operation",
			Subtype:          "directive",
			SourceFile:       filePath,
			Language:         "vue",
			StartLine:        lineNum,
			EndLine:          lineNum,
			Signature:        "template " + key,
			QualityScore:     0.8,
			EnrichmentStatus: types.StatusPending,
			Properties:       props,
		})
		rels = append(rels, types.RelationshipRecord{
			ToID: safe,
			Kind: "CONTAINS",
			Properties: map[string]string{
				"component": componentName,
				"directive": directive,
				"framework": "vue",
			},
		})
	}
	return ents, rels
}

// extractSlots scans the <template> block for Vue slots (issue #2876 — Vue
// Internals/slot_extraction) and returns SCOPE.Operation subtype="slot"
// entities plus CONTAINS edges from the component. Two roles are recognised:
//
//	<slot /> / <slot name="header" />           → role="outlet" (this component
//	                                               declares a slot)
//	<template #header> / <template v-slot:footer>→ role="content" (this component
//	                                               fills a child's slot)
//
// An unnamed <slot> or default v-slot is keyed as "default". The slot name is
// carried in the `slot_name` property so the component's slot surface is
// introspectable.
func extractSlots(templateSrc string, templateOffset int, fullSrc, filePath, componentName string) ([]types.EntityRecord, []types.RelationshipRecord) {
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	seen := map[string]bool{}

	emit := func(name, role string, off int) {
		if name == "" {
			name = "default"
		}
		dedupe := role + ":" + name
		if seen[dedupe] {
			return
		}
		seen[dedupe] = true
		safe := strings.NewReplacer("-", "_", ".", "_").Replace(role + "_" + name)
		lineNum := lineOf(fullSrc, templateOffset+off)
		ents = append(ents, types.EntityRecord{
			Name:             safe,
			QualifiedName:    fmt.Sprintf("%s.%s", componentName, safe),
			Kind:             "SCOPE.Operation",
			Subtype:          "slot",
			SourceFile:       filePath,
			Language:         "vue",
			StartLine:        lineNum,
			EndLine:          lineNum,
			Signature:        "slot " + role + " " + name,
			QualityScore:     0.8,
			EnrichmentStatus: types.StatusPending,
			Properties: map[string]string{
				"component": componentName,
				"slot_name": name,
				"slot_role": role,
				"framework": "vue",
			},
		})
		rels = append(rels, types.RelationshipRecord{
			ToID: safe,
			Kind: "CONTAINS",
			Properties: map[string]string{
				"component": componentName,
				"slot_name": name,
				"slot_role": role,
				"framework": "vue",
			},
		})
	}

	// Slot outlets: <slot /> / <slot name="header" />.
	for _, m := range reSlotOutlet.FindAllStringSubmatchIndex(templateSrc, -1) {
		attrs := templateSrc[m[2]:m[3]]
		name := ""
		if nm := reSlotName.FindStringSubmatch(attrs); nm != nil {
			name = nm[1]
		}
		emit(name, "outlet", m[0])
	}

	// Slot content: <template #name> / <template v-slot:name> / <template v-slot>.
	for _, m := range reSlotUse.FindAllStringSubmatchIndex(templateSrc, -1) {
		name := ""
		if m[2] >= 0 {
			name = templateSrc[m[2]:m[3]]
		} else if m[4] >= 0 {
			name = templateSrc[m[4]:m[5]]
		}
		emit(name, "content", m[0])
	}

	return ents, rels
}

// extractScriptNavigation scans a Vue <script> block for vue-router navigation
// patterns (issue #2856 — Navigation/router_pattern) and returns NAVIGATES_TO
// edges from the component:
//
//	createRouter({ routes: [ { path: '/x' }, … ] })  → one edge per declared path
//	router.push('/x') / router.replace('/x')          → edge to '/x'
//	router.push({ name: 'home' })                      → edge to named route
//
// vue-router is Vue's canonical client-side router; createRouter declares the
// route table and router.push/replace are the imperative navigation verbs.
func extractScriptNavigation(scriptSrc string, scriptOffset int, fullSrc, componentName string) []types.RelationshipRecord {
	var rels []types.RelationshipRecord
	seen := map[string]bool{}

	emit := func(route, via string, off int) {
		route = strings.TrimSpace(route)
		if route == "" || seen[via+"|"+route] {
			return
		}
		seen[via+"|"+route] = true
		rels = append(rels, types.RelationshipRecord{
			ToID: "route:" + route,
			Kind: "NAVIGATES_TO",
			Properties: map[string]string{
				"route":     route,
				"via":       via,
				"caller":    componentName,
				"framework": "vue",
				"line":      fmt.Sprintf("%d", lineOf(fullSrc, scriptOffset+off)),
			},
		})
	}

	// createRouter({ routes: [ … ] }) — scan the routes array for path entries.
	if m := reCreateRouter.FindStringIndex(scriptSrc); m != nil {
		// Bound the scan to the createRouter call's parenthesised argument so we
		// only pick up its routes (best-effort: scan from the call to the end of
		// its matching paren is overkill — the route-path regex is specific
		// enough that scanning the whole script after the call is safe).
		region := scriptSrc[m[0]:]
		for _, pm := range reVueRoutePath.FindAllStringSubmatchIndex(region, -1) {
			route := region[pm[2]:pm[3]]
			display := route
			if display == "" {
				display = "<index>"
			}
			emit(display, "route_table", m[0]+pm[0])
		}
	}

	// router.push / router.replace.
	for _, pm := range reVueRouterNav.FindAllStringSubmatchIndex(scriptSrc, -1) {
		// group 2 = string-literal route, group 3 = named-route name.
		var route string
		if pm[4] >= 0 {
			route = scriptSrc[pm[4]:pm[5]]
		} else if pm[6] >= 0 {
			route = scriptSrc[pm[6]:pm[7]]
		}
		if route == "" {
			continue
		}
		emit(route, "router_call", pm[0])
	}

	return rels
}

// extractRouterLinks scans a Vue <template> block for <router-link>/<RouterLink>
// (and Nuxt's <nuxt-link>) navigation directives and returns NAVIGATES_TO edges
// from the component (issue #2856 — Navigation/router_pattern). Both the static
// `to="/x"` and bound `:to="…"` forms are matched; a non-literal bound value is
// captured verbatim so the call shape is still introspectable.
func extractRouterLinks(templateSrc string, templateOffset int, fullSrc, componentName string) []types.RelationshipRecord {
	var rels []types.RelationshipRecord
	seen := map[string]bool{}
	for _, m := range reRouterLink.FindAllStringSubmatchIndex(templateSrc, -1) {
		route := strings.TrimSpace(templateSrc[m[2]:m[3]])
		if route == "" || seen[route] {
			continue
		}
		seen[route] = true
		rels = append(rels, types.RelationshipRecord{
			ToID: "route:" + route,
			Kind: "NAVIGATES_TO",
			Properties: map[string]string{
				"route":     route,
				"via":       "router_link",
				"caller":    componentName,
				"framework": "vue",
				"line":      fmt.Sprintf("%d", lineOf(fullSrc, templateOffset+m[0])),
			},
		})
	}
	return rels
}

// extractStateSetters scans a Vue <script> block for state-mutation points
// (issue #2856 — Lifecycle/state_setter_emission) and returns SCOPE.Operation
// subtype="state_setter" entities, each carrying a WRITES_TO edge to the state
// it mutates. Two idioms are recognised:
//
//	ref().value assignment: `count.value = 1` / `count.value += 1`
//	    → setter "count.value=" WRITES_TO state "count"
//	Pinia $patch:           `userStore.$patch({ … })`
//	    → setter "userStore.$patch" WRITES_TO state "userStore"
//
// The ref-binding set is collected first (from `const x = ref(…)`) so a
// `.value =` assignment is only treated as a state setter when its receiver is
// a known reactive ref, avoiding false positives on unrelated `.value` writes.
func extractStateSetters(scriptSrc string, scriptOffset int, fullSrc, filePath, componentName string) []types.EntityRecord {
	var ents []types.EntityRecord
	seen := map[string]bool{}

	// Collect ref/reactive bindings so .value assignments gate on a known ref.
	refs := map[string]bool{}
	for _, m := range reReactiveState.FindAllStringSubmatch(scriptSrc, -1) {
		refs[m[1]] = true
	}

	emit := func(name, stateName, sig string, props map[string]string, off int) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		lineNum := lineOf(fullSrc, scriptOffset+off)
		props["component"] = componentName
		props["framework"] = "vue"
		props["state"] = stateName
		props["subtype"] = "state_setter"
		ent := types.EntityRecord{
			Name:             name,
			QualifiedName:    fmt.Sprintf("%s.%s", componentName, name),
			Kind:             "SCOPE.Operation",
			Subtype:          "state_setter",
			SourceFile:       filePath,
			Language:         "vue",
			StartLine:        lineNum,
			EndLine:          lineNum,
			Signature:        sig,
			QualityScore:     0.8,
			EnrichmentStatus: types.StatusPending,
			Properties:       props,
			Relationships: []types.RelationshipRecord{{
				ToID: "state:" + stateName,
				Kind: "WRITES_TO",
				Properties: map[string]string{
					"setter":    name,
					"state":     stateName,
					"component": componentName,
					"framework": "vue",
				},
			}},
		}
		ents = append(ents, ent)
	}

	// ref().value assignments.
	for _, m := range reVueRefAssign.FindAllStringSubmatchIndex(scriptSrc, -1) {
		name := scriptSrc[m[2]:m[3]]
		if !refs[name] {
			continue
		}
		emit(name+".value=", name, name+".value = …",
			map[string]string{"setter_kind": "ref_assign"}, m[0])
	}

	// Pinia store.$patch.
	for _, m := range reVuePatch.FindAllStringSubmatchIndex(scriptSrc, -1) {
		store := scriptSrc[m[2]:m[3]]
		emit(store+".$patch", store, store+".$patch(…)",
			map[string]string{"setter_kind": "pinia_patch"}, m[0])
	}

	return ents
}

// buildVueImportEntities scans import statements in the script block and emits
// IMPORTS edges on the file entity.
func buildVueImportEntities(filePath, scriptSrc string, scriptOffset int, fullSrc string) []types.EntityRecord {
	matches := reImport.FindAllStringSubmatchIndex(scriptSrc, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]types.EntityRecord, 0, len(matches))
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		modulePath := scriptSrc[m[2]:m[3]]
		if modulePath == "" || seen[modulePath] {
			continue
		}
		seen[modulePath] = true
		lineNum := lineOf(fullSrc, scriptOffset+m[0])
		// Derive a short name (last path segment, no extension)
		name := moduleShortName(modulePath)
		out = append(out, types.EntityRecord{
			Name:             name,
			QualifiedName:    modulePath,
			Kind:             "SCOPE.Component",
			Subtype:          "import",
			SourceFile:       filePath,
			Language:         "vue",
			StartLine:        lineNum,
			EndLine:          lineNum,
			QualityScore:     0.85,
			EnrichmentStatus: types.StatusPending,
			Relationships: []types.RelationshipRecord{
				{
					FromID: filePath,
					ToID:   modulePath,
					Kind:   "IMPORTS",
					Properties: map[string]string{
						"source_module": modulePath,
					},
				},
			},
		})
	}
	return out
}

// findClosingBrace returns the index (in src) of the brace matching the one
// at src[start-1] (i.e. the brace count at start is already 1). Returns -1
// if not found.
func findClosingBrace(src string, start int) int {
	depth := 1
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// componentNameFromPath derives the PascalCase component name from the file path.
// "src/components/MyButton.vue" → "MyButton"
func componentNameFromPath(p string) string {
	base := filepath.Base(p)
	name := strings.TrimSuffix(base, ".vue")
	name = strings.TrimSuffix(name, ".Vue")
	if name == "" {
		return "Unknown"
	}
	return name
}

// moduleShortName returns a short identifier for a module path.
// "./components/MyButton" → "MyButton"
// "vue-router" → "vue-router"
func moduleShortName(mod string) string {
	// Strip trailing slashes
	mod = strings.TrimRight(mod, "/")
	// Get last segment
	if idx := strings.LastIndexAny(mod, "/"); idx >= 0 {
		mod = mod[idx+1:]
	}
	// Strip extension
	if idx := strings.LastIndexByte(mod, '.'); idx > 0 {
		mod = mod[:idx]
	}
	if mod == "" {
		return "unknown"
	}
	return mod
}

// lineOf returns the 1-based line number of the given byte offset in src.
func lineOf(src string, offset int) int {
	if offset <= 0 {
		return 1
	}
	if offset > len(src) {
		offset = len(src)
	}
	return strings.Count(src[:offset], "\n") + 1
}
