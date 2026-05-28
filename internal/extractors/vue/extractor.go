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

	// --- 4. Extract <template> block → RENDERS edges -------------------------
	templateSrc, _, ok := extractTemplateBlock(src)
	if ok {
		renders := extractTemplateRenders(templateSrc, componentName)
		entities[componentIdx].Relationships = append(entities[componentIdx].Relationships, renders...)
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

// extractTemplateBlock locates the first <template> section.
func extractTemplateBlock(src string) (content string, offset int, found bool) {
	openLoc := reTemplateOpen.FindStringIndex(src)
	if openLoc == nil {
		return "", 0, false
	}
	contentStart := openLoc[1]
	closeLoc := reTemplateClose.FindStringIndex(src[contentStart:])
	if closeLoc == nil {
		return "", 0, false
	}
	contentEnd := contentStart + closeLoc[0]
	return src[contentStart:contentEnd], contentStart, true
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
