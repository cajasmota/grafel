// Package svelte implements a regex-based extractor for Svelte single-file
// components (.svelte files).
//
// A Svelte SFC has three optional sections:
//
//	<script [lang="ts"]> ... </script>   — component logic
//	HTML template                        — markup with Svelte directives
//	<style> ... </style>                 — scoped CSS
//
// Extracted entities:
//
//	Whole file               → SCOPE.Component    subtype="svelte_component"
//	export let <prop>        → SCOPE.Operation    subtype="prop"
//	let <x> = $state(…)      → SCOPE.Operation    subtype="rune_state"
//	$derived(…) declaration  → SCOPE.Operation    subtype="rune_derived"
//	$effect(…) call          → SCOPE.Operation    subtype="rune_effect"
//	<ChildComponent />       → RENDERS edge
//
// Svelte 5 runes ($state, $derived, $effect, $props, $bindable) are
// recognised; Svelte 4 lifecycle helpers (onMount, onDestroy, …) and stores
// (writable, readable, derived, get) are captured as CALLS edges via the
// resolver slice (dynamic_patterns_svelte.go).
//
// Registers itself via init() and is imported by registry_gen.go.
package svelte

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
	extractor.Register("svelte", &Extractor{})
}

// Extractor implements extractor.Extractor for Svelte SFC files.
type Extractor struct{}

// Language returns the canonical language key.
func (e *Extractor) Language() string { return "svelte" }

// ── compiled regexps ────────────────────────────────────────────────────────

var (
	// scriptBlockRE captures the content between <script …> and </script>.
	// Handles optional lang="ts" / lang="js" attributes. Non-greedy inner
	// match so nested tags in template are not consumed.
	scriptBlockRE = regexp.MustCompile(`(?si)<script(?:[^>]*)>(.*?)</script>`)

	// exportLetRE matches `export let propName` declarations inside a <script>
	// block.  Captures the property name.
	// Examples: `export let count = 0`, `export let label: string`
	exportLetRE = regexp.MustCompile(`(?m)^\s*export\s+let\s+([A-Za-z_$][A-Za-z0-9_$]*)`)

	// stateRuneRE matches `let name = $state(…)` (Svelte 5 rune).
	// Captures the variable name.
	stateRuneRE = regexp.MustCompile(`(?m)^\s*(?:let|const)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*\$state\s*\(`)

	// derivedRuneRE matches `let name = $derived(…)` (Svelte 5 rune).
	// Captures the variable name.
	derivedRuneRE = regexp.MustCompile(`(?m)^\s*(?:let|const)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*\$derived\s*\(`)

	// effectRuneRE matches a top-level `$effect(…)` call (Svelte 5 rune).
	// Captures nothing beyond the match itself — effects are anonymous.
	effectRuneRE = regexp.MustCompile(`(?m)^\s*\$effect\s*\(`)

	// propsRuneRE matches `const { a, b } = $props()` or `let props = $props()`.
	// Captures the text between `{` and `}` for destructured forms, or the
	// bare binding name for the non-destructured form.
	propsRuneRE = regexp.MustCompile(`(?m)^\s*(?:let|const)\s+(?:\{([^}]*)\}|([A-Za-z_$][A-Za-z0-9_$]*))\s*=\s*\$props\s*\(`)

	// bindableRuneRE matches `let x = $bindable(…)` (Svelte 5 rune).
	bindableRuneRE = regexp.MustCompile(`(?m)^\s*(?:let|const)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*\$bindable\s*\(`)

	// childComponentRE finds PascalCase component tags in the template section.
	// Matches both self-closing `<Foo />` and opening `<Foo>` / `<Foo ...>`.
	// PascalCase tag → Svelte component by convention (lower-case → HTML element).
	childComponentRE = regexp.MustCompile(`<([A-Z][A-Za-z0-9]*)\b`)

	// Context (issue #2854 — Structure/context_extraction). Svelte's
	// dependency-injection context API: setContext(key, value) is the
	// provider, getContext(key) is the consumer. The first argument is the
	// context key (Symbol identifier or string literal).
	setContextRE = regexp.MustCompile(`\bsetContext\s*\(\s*([A-Za-z_$][A-Za-z0-9_$]*|['"][^'"]+['"])`)
	getContextRE = regexp.MustCompile(`\bgetContext\s*\(\s*([A-Za-z_$][A-Za-z0-9_$]*|['"][^'"]+['"])`)

	// ── Data Flow (issue #2855) ──────────────────────────────────────────────

	// Svelte store declarations: `const count = writable(0)` /
	// readable / derived / tweened / spring. Svelte stores are the framework's
	// state container (state_management).
	storeDeclRE = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:const|let)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*(writable|readable|derived|tweened|spring)\s*\(`)

	// Data fetching call sites: fetch(…) — including the SvelteKit `load`/form
	// data layer which uses the platform fetch — plus axios.* clients.
	dataFetchRE = regexp.MustCompile(`(?m)\bfetch\s*\(|\baxios\s*(?:\.\s*(?:get|post|put|patch|delete|request))?\s*\(`)

	// Template branch conditions: {#if …}, {:else if …}, {#each …}, {#await …}.
	// Svelte's logic blocks are its conditional/iterative rendering constructs.
	branchBlockRE = regexp.MustCompile(`\{#(if|each|await)\b|\{:else\s+if\b|\{:else\b|\{:then\b|\{:catch\b`)

	// ── Navigation (issue #2856) ─────────────────────────────────────────────
	//
	// Svelte itself ships NO built-in router — routing in a plain Svelte SPA is
	// provided by an ecosystem library (svelte-routing, svelte-spa-router). We
	// recognise those genuine client-side routing idioms (SvelteKit's
	// file-system routing + `goto` are covered by the sveltekit framework
	// record, not here).
	//
	// svelte-routing: <Route path="/x"> declares a route; <Link to="/x"> links
	// to it; navigate('/x') navigates imperatively.
	routeTagRE   = regexp.MustCompile(`(?si)<Route\b[^>]*?\bpath\s*=\s*"([^"]*)"`)
	linkToRE     = regexp.MustCompile(`(?si)<Link\b[^>]*?\bto\s*=\s*"([^"]*)"`)
	navigateCall = regexp.MustCompile(`(?m)\bnavigate\s*\(\s*['"]([^'"]*)['"]`)
	// svelte-spa-router uses `push('/x')` / `replace('/x')` from the library.
	spaRouterCall = regexp.MustCompile(`(?m)\b(?:push|replace)\s*\(\s*['"](/[^'"]*)['"]`)

	// ── Lifecycle / state_setter_emission (issue #2856) ──────────────────────
	//
	// Svelte store setters: <store>.set(v) / <store>.update(fn). Gated on the
	// store bindings collected from storeDeclRE so unrelated .set/.update calls
	// (Set.add, Map.set) are not misread.
	storeSetterRE = regexp.MustCompile(`(?m)\b([A-Za-z_$][A-Za-z0-9_$]*)\s*\.\s*(set|update)\s*\(`)
	// Auto-subscribe store assignment: `$count = v` / `$count += v`. The `$`
	// prefix is Svelte's reactive store-value accessor; assigning to it writes
	// through to the store.
	storeDollarAssignRE = regexp.MustCompile(`(?m)\$([A-Za-z_$][A-Za-z0-9_$]*)\s*(?:=|\+=|-=|\*=|/=)\s*[^=]`)
)

// Extract parses the Svelte SFC source and returns entity records.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor.svelte")
	ctx, span := tracer.Start(ctx, "indexer.extract.svelte",
		trace.WithAttributes(attribute.String("language", "svelte")),
	)
	defer span.End()

	_ = ctx // used only for span

	src := string(file.Content)
	if len(src) == 0 {
		span.SetAttributes(
			attribute.Int("file_line_count", 0),
			attribute.Int("entity_count", 0),
		)
		return nil, nil
	}

	lineCount := strings.Count(src, "\n") + 1
	componentName := componentNameFromPath(file.Path)

	var entities []types.EntityRecord

	// ── 1. Whole-file component entity ───────────────────────────────────────
	componentEntity := types.EntityRecord{
		Name:         componentName,
		Kind:         "SCOPE.Component",
		Subtype:      "svelte_component",
		SourceFile:   file.Path,
		Language:     "svelte",
		StartLine:    1,
		EndLine:      lineCount,
		Signature:    componentName + ".svelte",
		QualityScore: 0.85,
	}
	entities = append(entities, componentEntity)

	// ── 2. Extract <script> block ─────────────────────────────────────────────
	scriptContent, scriptStartLine := extractScriptBlock(src)
	if scriptContent != "" {
		scriptEntities := extractScriptEntities(scriptContent, scriptStartLine, file.Path)
		entities = append(entities, scriptEntities...)

		// Context provide/consume (#2854 — Structure/context_extraction).
		ctxEntities, ctxRels := extractContext(scriptContent, scriptStartLine, file.Path, componentName)
		entities[0].Relationships = append(entities[0].Relationships, ctxRels...)
		entities = append(entities, ctxEntities...)

		// Data Flow (#2855): state_management (stores) + data_fetching.
		dfEntities, dfRels := extractScriptDataFlow(scriptContent, scriptStartLine, file.Path, componentName)
		entities[0].Relationships = append(entities[0].Relationships, dfRels...)
		entities = append(entities, dfEntities...)

		// Navigation (#2856): imperative svelte-routing navigate()/push().
		navRels := extractScriptNavigation(scriptContent, scriptStartLine, file.Path, componentName)
		entities[0].Relationships = append(entities[0].Relationships, navRels...)

		// Lifecycle (#2856): state_setter_emission — store .set/.update and
		// $store reactive assignment.
		setterEnts := extractStateSetters(scriptContent, scriptStartLine, file.Path, componentName)
		for i := range setterEnts {
			entities[0].Relationships = append(entities[0].Relationships, types.RelationshipRecord{
				FromID: file.Path,
				ToID:   setterEnts[i].Name,
				Kind:   "CONTAINS",
			})
		}
		entities = append(entities, setterEnts...)
	}

	// ── 3. Extract RENDERS edges + branch conditions from the template ───────
	templateContent, templateStartLine := extractTemplateSection(src)
	if templateContent != "" {
		renderRels := extractChildComponents(templateContent, templateStartLine, file.Path, componentName)
		if len(renderRels) > 0 {
			// Attach RENDERS relationships to the component entity.
			entities[0].Relationships = append(entities[0].Relationships, renderRels...)
		}
		// Data Flow (#2855): branch_conditions from {#if}/{#each}/{:else if}.
		brEntities, brRels := extractBranchConditions(templateContent, templateStartLine, file.Path, componentName)
		entities[0].Relationships = append(entities[0].Relationships, brRels...)
		entities = append(entities, brEntities...)

		// Navigation (#2856): <Route path="…"> / <Link to="…"> svelte-routing
		// directives emit NAVIGATES_TO edges.
		linkRels := extractRouteDirectives(templateContent, templateStartLine, file.Path, componentName)
		entities[0].Relationships = append(entities[0].Relationships, linkRels...)
	}

	span.SetAttributes(
		attribute.Int("file_line_count", lineCount),
		attribute.Int("entity_count", len(entities)),
	)
	return entities, nil
}

// componentNameFromPath derives the Svelte component name from the file path.
// e.g. "src/lib/MyButton.svelte" → "MyButton"
func componentNameFromPath(path string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, ".svelte")
	if name == "" {
		return "Component"
	}
	return name
}

// extractScriptBlock finds the first <script …> … </script> block and returns
// its inner content and the 1-based line number of the first content line.
func extractScriptBlock(src string) (string, int) {
	m := scriptBlockRE.FindStringSubmatchIndex(src)
	if m == nil {
		return "", 0
	}
	// m[2]..m[3] is the inner content (capture group 1)
	inner := src[m[2]:m[3]]
	// Count lines before the start of the inner content.
	startLine := strings.Count(src[:m[2]], "\n") + 1
	return inner, startLine
}

// extractTemplateSection returns the non-script, non-style portion of the
// file (the HTML template) along with its first content line number.
// This is a best-effort extraction: we strip <script> and <style> blocks.
func extractTemplateSection(src string) (string, int) {
	// Remove all <script …>…</script> blocks.
	stripped := scriptBlockRE.ReplaceAllString(src, "")
	// Remove <style …>…</style> blocks (simple pattern, same approach).
	styleRE := regexp.MustCompile(`(?si)<style(?:[^>]*)>.*?</style>`)
	stripped = styleRE.ReplaceAllString(stripped, "")
	if strings.TrimSpace(stripped) == "" {
		return "", 0
	}
	return stripped, 1
}

// extractScriptEntities scans the <script> inner content and extracts:
//   - export let <prop>            → SCOPE.Operation (subtype="prop")
//   - let/const x = $state(…)     → SCOPE.Operation (subtype="rune_state")
//   - let/const x = $derived(…)   → SCOPE.Operation (subtype="rune_derived")
//   - $effect(…)                  → SCOPE.Operation (subtype="rune_effect")
//   - $props() destructure        → SCOPE.Operation (subtype="prop") per named prop
//   - let/const x = $bindable(…)  → SCOPE.Operation (subtype="rune_state")
func extractScriptEntities(script string, scriptStartLine int, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	// lineOf returns 1-based absolute line for a given line index inside script.
	lineOf := func(idx int) int {
		return scriptStartLine + idx
	}

	// export let props
	for _, m := range exportLetRE.FindAllStringSubmatchIndex(script, -1) {
		name := script[m[2]:m[3]]
		lineIdx := strings.Count(script[:m[0]], "\n")
		entities = append(entities, types.EntityRecord{
			Name:         name,
			Kind:         "SCOPE.Operation",
			Subtype:      "prop",
			SourceFile:   filePath,
			Language:     "svelte",
			StartLine:    lineOf(lineIdx),
			EndLine:      lineOf(lineIdx),
			Signature:    "export let " + name,
			QualityScore: 0.85,
		})
	}

	// $state rune
	for _, m := range stateRuneRE.FindAllStringSubmatchIndex(script, -1) {
		name := script[m[2]:m[3]]
		lineIdx := strings.Count(script[:m[0]], "\n")
		entities = append(entities, types.EntityRecord{
			Name:         name,
			Kind:         "SCOPE.Operation",
			Subtype:      "rune_state",
			SourceFile:   filePath,
			Language:     "svelte",
			StartLine:    lineOf(lineIdx),
			EndLine:      lineOf(lineIdx),
			Signature:    "let " + name + " = $state(...)",
			QualityScore: 0.8,
		})
	}

	// $derived rune
	for _, m := range derivedRuneRE.FindAllStringSubmatchIndex(script, -1) {
		name := script[m[2]:m[3]]
		lineIdx := strings.Count(script[:m[0]], "\n")
		entities = append(entities, types.EntityRecord{
			Name:         name,
			Kind:         "SCOPE.Operation",
			Subtype:      "rune_derived",
			SourceFile:   filePath,
			Language:     "svelte",
			StartLine:    lineOf(lineIdx),
			EndLine:      lineOf(lineIdx),
			Signature:    "let " + name + " = $derived(...)",
			QualityScore: 0.8,
		})
	}

	// $effect rune (anonymous — name them by position)
	effectMatches := effectRuneRE.FindAllStringIndex(script, -1)
	for i, m := range effectMatches {
		lineIdx := strings.Count(script[:m[0]], "\n")
		name := "$effect"
		if i > 0 {
			name = fmt.Sprintf("$effect_%d", i)
		}
		entities = append(entities, types.EntityRecord{
			Name:         name,
			Kind:         "SCOPE.Operation",
			Subtype:      "rune_effect",
			SourceFile:   filePath,
			Language:     "svelte",
			StartLine:    lineOf(lineIdx),
			EndLine:      lineOf(lineIdx),
			Signature:    "$effect(() => { ... })",
			QualityScore: 0.75,
		})
	}

	// $props() rune — destructured: `const { a, b } = $props()`
	for _, m := range propsRuneRE.FindAllStringSubmatchIndex(script, -1) {
		lineIdx := strings.Count(script[:m[0]], "\n")
		if m[2] >= 0 {
			// Destructured form: group 1 = "a, b, c"
			inner := script[m[2]:m[3]]
			for _, field := range strings.Split(inner, ",") {
				field = strings.TrimSpace(field)
				// Handle default values: `label = "hello"` → name is "label"
				if idx := strings.IndexAny(field, "=:"); idx >= 0 {
					field = strings.TrimSpace(field[:idx])
				}
				if field == "" {
					continue
				}
				entities = append(entities, types.EntityRecord{
					Name:         field,
					Kind:         "SCOPE.Operation",
					Subtype:      "prop",
					SourceFile:   filePath,
					Language:     "svelte",
					StartLine:    lineOf(lineIdx),
					EndLine:      lineOf(lineIdx),
					Signature:    "$props(): " + field,
					QualityScore: 0.85,
				})
			}
		} else if m[4] >= 0 {
			// Non-destructured form: group 2 = variable name
			name := script[m[4]:m[5]]
			entities = append(entities, types.EntityRecord{
				Name:         name,
				Kind:         "SCOPE.Operation",
				Subtype:      "prop",
				SourceFile:   filePath,
				Language:     "svelte",
				StartLine:    lineOf(lineIdx),
				EndLine:      lineOf(lineIdx),
				Signature:    "let " + name + " = $props()",
				QualityScore: 0.85,
			})
		}
	}

	// $bindable rune
	for _, m := range bindableRuneRE.FindAllStringSubmatchIndex(script, -1) {
		name := script[m[2]:m[3]]
		lineIdx := strings.Count(script[:m[0]], "\n")
		entities = append(entities, types.EntityRecord{
			Name:         name,
			Kind:         "SCOPE.Operation",
			Subtype:      "rune_state",
			SourceFile:   filePath,
			Language:     "svelte",
			StartLine:    lineOf(lineIdx),
			EndLine:      lineOf(lineIdx),
			Signature:    "let " + name + " = $bindable(...)",
			QualityScore: 0.8,
		})
	}

	return entities
}

// extractContext scans the <script> content for Svelte context API calls
// (issue #2854 — Structure/context_extraction). setContext(key, …) provides a
// context; getContext(key) consumes one. Each distinct (role, key) pair yields
// a SCOPE.Operation entity (subtype "provide_context"/"inject_context") and a
// USES edge from the component to that context operation.
func extractContext(script string, scriptStartLine int, filePath, componentName string) ([]types.EntityRecord, []types.RelationshipRecord) {
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	seen := map[string]bool{}

	emit := func(re *regexp.Regexp, subtype, role string) {
		for _, m := range re.FindAllStringSubmatchIndex(script, -1) {
			if len(m) < 4 {
				continue
			}
			key := normalizeContextKey(script[m[2]:m[3]])
			if key == "" {
				continue
			}
			dedupe := role + ":" + key
			if seen[dedupe] {
				continue
			}
			seen[dedupe] = true
			lineNum := scriptStartLine + strings.Count(script[:m[0]], "\n")
			name := role + ":" + key
			ents = append(ents, types.EntityRecord{
				Name:         name,
				Kind:         "SCOPE.Operation",
				Subtype:      subtype,
				SourceFile:   filePath,
				Language:     "svelte",
				StartLine:    lineNum,
				EndLine:      lineNum,
				Signature:    subtype + "(" + key + ")",
				QualityScore: 0.8,
				Properties: map[string]string{
					"component":    componentName,
					"context_key":  key,
					"context_role": role,
					"framework":    "svelte",
				},
			})
			rels = append(rels, types.RelationshipRecord{
				FromID: filePath,
				ToID:   name,
				Kind:   "USES",
				Properties: map[string]string{
					"component":    componentName,
					"context_key":  key,
					"context_role": role,
					"framework":    "svelte",
				},
			})
		}
	}

	emit(setContextRE, "provide_context", "provider")
	emit(getContextRE, "inject_context", "consumer")
	return ents, rels
}

// extractScriptDataFlow scans the <script> content for Svelte Data-Flow signals
// (issue #2855): state_management (writable/readable/derived/tweened/spring
// stores — Svelte's reactive state container) and data_fetching (fetch/axios
// call sites). Each emits a SCOPE.Operation entity and a USES edge from the
// component file to it.
func extractScriptDataFlow(script string, scriptStartLine int, filePath, componentName string) ([]types.EntityRecord, []types.RelationshipRecord) {
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord

	emit := func(name, subtype, sig string, props map[string]string, off int) {
		lineNum := scriptStartLine + strings.Count(script[:off], "\n")
		props["component"] = componentName
		props["framework"] = "svelte"
		ents = append(ents, types.EntityRecord{
			Name:         name,
			Kind:         "SCOPE.Operation",
			Subtype:      subtype,
			SourceFile:   filePath,
			Language:     "svelte",
			StartLine:    lineNum,
			EndLine:      lineNum,
			Signature:    sig,
			QualityScore: 0.8,
			Properties:   props,
		})
		rels = append(rels, types.RelationshipRecord{
			FromID: filePath,
			ToID:   name,
			Kind:   "USES",
			Properties: map[string]string{
				"component": componentName,
				"framework": "svelte",
				"subtype":   subtype,
			},
		})
	}

	// state_management: store declarations.
	stateSeen := map[string]bool{}
	for _, m := range storeDeclRE.FindAllStringSubmatchIndex(script, -1) {
		name := script[m[2]:m[3]]
		prim := script[m[4]:m[5]]
		if stateSeen[name] {
			continue
		}
		stateSeen[name] = true
		emit(name, "state_store", prim+"() "+name,
			map[string]string{"state_lib": "svelte-store", "primitive": prim}, m[0])
	}

	// data_fetching: fetch / axios call sites.
	fetchSeen := map[string]bool{}
	idx := 0
	for _, m := range dataFetchRE.FindAllStringIndex(script, -1) {
		raw := strings.TrimSpace(script[m[0]:m[1]])
		kind := "fetch"
		if strings.HasPrefix(raw, "axios") {
			kind = "axios"
		}
		key := kind
		if fetchSeen[key] {
			continue
		}
		fetchSeen[key] = true
		name := "fetch:" + kind
		if idx > 0 {
			name = fmt.Sprintf("fetch:%s_%d", kind, idx)
		}
		idx++
		emit(name, "data_fetch", kind+"(…)",
			map[string]string{"fetch_kind": kind}, m[0])
	}

	return ents, rels
}

// extractBranchConditions scans the Svelte template for logic-block branches
// ({#if}, {:else if}, {#each}, {#await}) and returns SCOPE.Operation
// subtype="branch_condition" entities + USES edges (issue #2855 — Data Flow /
// branch_conditions).
func extractBranchConditions(template string, templateStartLine int, filePath, componentName string) ([]types.EntityRecord, []types.RelationshipRecord) {
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	seen := map[string]int{}
	for _, m := range branchBlockRE.FindAllStringIndex(template, -1) {
		raw := strings.TrimSpace(template[m[0]:m[1]])
		// Canonicalise the block label.
		kind := svelteBranchKind(raw)
		if kind == "" {
			continue
		}
		seen[kind]++
		idx := seen[kind]
		name := kind
		if idx > 1 {
			name = fmt.Sprintf("%s_%d", kind, idx)
		}
		lineNum := templateStartLine + strings.Count(template[:m[0]], "\n")
		ents = append(ents, types.EntityRecord{
			Name:         name,
			Kind:         "SCOPE.Operation",
			Subtype:      "branch_condition",
			SourceFile:   filePath,
			Language:     "svelte",
			StartLine:    lineNum,
			EndLine:      lineNum,
			Signature:    "template " + raw,
			QualityScore: 0.8,
			Properties: map[string]string{
				"component":   componentName,
				"branch_kind": kind,
				"framework":   "svelte",
			},
		})
		rels = append(rels, types.RelationshipRecord{
			FromID: filePath,
			ToID:   name,
			Kind:   "USES",
			Properties: map[string]string{
				"component":   componentName,
				"branch_kind": kind,
				"framework":   "svelte",
			},
		})
	}
	return ents, rels
}

// extractScriptNavigation scans a Svelte <script> block for imperative
// client-side routing calls (issue #2856 — Navigation/router_pattern). Svelte
// has no built-in router, so this targets the ecosystem libraries:
// svelte-routing's navigate('/x') and svelte-spa-router's push('/x')/
// replace('/x'). Returns NAVIGATES_TO edges from the component file.
func extractScriptNavigation(script string, scriptStartLine int, filePath, componentName string) []types.RelationshipRecord {
	var rels []types.RelationshipRecord
	seen := map[string]bool{}
	emit := func(route, via string, off int) {
		route = strings.TrimSpace(route)
		if route == "" || seen[via+"|"+route] {
			return
		}
		seen[via+"|"+route] = true
		lineNum := scriptStartLine + strings.Count(script[:off], "\n")
		rels = append(rels, types.RelationshipRecord{
			FromID: filePath,
			ToID:   "route:" + route,
			Kind:   "NAVIGATES_TO",
			Properties: map[string]string{
				"route":     route,
				"via":       via,
				"caller":    componentName,
				"framework": "svelte",
				"line":      fmt.Sprintf("%d", lineNum),
			},
		})
	}
	for _, m := range navigateCall.FindAllStringSubmatchIndex(script, -1) {
		emit(script[m[2]:m[3]], "navigate_call", m[0])
	}
	for _, m := range spaRouterCall.FindAllStringSubmatchIndex(script, -1) {
		emit(script[m[2]:m[3]], "spa_router_call", m[0])
	}
	return rels
}

// extractRouteDirectives scans the Svelte template for svelte-routing's <Route
// path="…"> route declarations and <Link to="…"> navigation links (issue #2856
// — Navigation/router_pattern), returning NAVIGATES_TO edges from the component
// file.
func extractRouteDirectives(template string, templateStartLine int, filePath, componentName string) []types.RelationshipRecord {
	var rels []types.RelationshipRecord
	seen := map[string]bool{}
	emit := func(re *regexp.Regexp, via string) {
		for _, m := range re.FindAllStringSubmatchIndex(template, -1) {
			route := strings.TrimSpace(template[m[2]:m[3]])
			if route == "" || seen[via+"|"+route] {
				continue
			}
			seen[via+"|"+route] = true
			lineNum := templateStartLine + strings.Count(template[:m[0]], "\n")
			rels = append(rels, types.RelationshipRecord{
				FromID: filePath,
				ToID:   "route:" + route,
				Kind:   "NAVIGATES_TO",
				Properties: map[string]string{
					"route":     route,
					"via":       via,
					"caller":    componentName,
					"framework": "svelte",
					"line":      fmt.Sprintf("%d", lineNum),
				},
			})
		}
	}
	emit(routeTagRE, "route_table")
	emit(linkToRE, "link")
	return rels
}

// extractStateSetters scans a Svelte <script> block for state-mutation points
// (issue #2856 — Lifecycle/state_setter_emission) and returns SCOPE.Operation
// subtype="state_setter" entities, each carrying a WRITES_TO edge to the store
// it mutates. Two idioms are recognised:
//
//	store method:   <store>.set(v) / <store>.update(fn)  → WRITES_TO <store>
//	$-assignment:   $count = v / $count += v             → WRITES_TO count
//
// `.set`/`.update` are gated on the writable/readable store bindings collected
// from storeDeclRE so unrelated `.set` calls (Set.add, Map.set) are excluded.
func extractStateSetters(script string, scriptStartLine int, filePath, componentName string) []types.EntityRecord {
	var ents []types.EntityRecord
	seen := map[string]bool{}

	// Collect store bindings to gate .set/.update.
	stores := map[string]bool{}
	for _, m := range storeDeclRE.FindAllStringSubmatch(script, -1) {
		stores[m[1]] = true
	}

	emit := func(name, stateName, sig string, props map[string]string, off int) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		lineNum := scriptStartLine + strings.Count(script[:off], "\n")
		props["component"] = componentName
		props["framework"] = "svelte"
		props["state"] = stateName
		props["subtype"] = "state_setter"
		ents = append(ents, types.EntityRecord{
			Name:         name,
			Kind:         "SCOPE.Operation",
			Subtype:      "state_setter",
			SourceFile:   filePath,
			Language:     "svelte",
			StartLine:    lineNum,
			EndLine:      lineNum,
			Signature:    sig,
			QualityScore: 0.8,
			Properties:   props,
			Relationships: []types.RelationshipRecord{{
				ToID: "state:" + stateName,
				Kind: "WRITES_TO",
				Properties: map[string]string{
					"setter":    name,
					"state":     stateName,
					"component": componentName,
					"framework": "svelte",
				},
			}},
		})
	}

	// Store .set / .update.
	for _, m := range storeSetterRE.FindAllStringSubmatchIndex(script, -1) {
		store := script[m[2]:m[3]]
		method := script[m[4]:m[5]]
		if !stores[store] {
			continue
		}
		emit(store+"."+method, store, store+"."+method+"(…)",
			map[string]string{"setter_kind": "store_method", "method": method}, m[0])
	}

	// $store auto-subscribe assignment.
	for _, m := range storeDollarAssignRE.FindAllStringSubmatchIndex(script, -1) {
		store := script[m[2]:m[3]]
		emit("$"+store+"=", store, "$"+store+" = …",
			map[string]string{"setter_kind": "store_assign"}, m[0])
	}

	return ents
}

// svelteBranchKind maps a matched logic-block opener to a canonical label.
func svelteBranchKind(raw string) string {
	switch {
	case strings.HasPrefix(raw, "{#if"):
		return "if"
	case strings.HasPrefix(raw, "{#each"):
		return "each"
	case strings.HasPrefix(raw, "{#await"):
		return "await"
	case strings.HasPrefix(raw, "{:else if"):
		return "else_if"
	case strings.HasPrefix(raw, "{:else"):
		return "else"
	case strings.HasPrefix(raw, "{:then"):
		return "then"
	case strings.HasPrefix(raw, "{:catch"):
		return "catch"
	}
	return ""
}

// normalizeContextKey strips quotes from a string-literal context key, or
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

// extractChildComponents scans the template content for PascalCase component
// tags and returns RENDERS relationship records.
//
// Deduplicates by component name so `<Button>` appearing 3 times produces
// one RENDERS edge, not three (avoids count inflation).
func extractChildComponents(template string, templateStartLine int, filePath, componentName string) []types.RelationshipRecord {
	seen := make(map[string]struct{})
	var rels []types.RelationshipRecord

	for _, m := range childComponentRE.FindAllStringSubmatchIndex(template, -1) {
		name := template[m[2]:m[3]]
		// Skip if already seen.
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}

		lineIdx := strings.Count(template[:m[0]], "\n")
		lineNum := templateStartLine + lineIdx

		rels = append(rels, types.RelationshipRecord{
			FromID: filePath,
			ToID:   name,
			Kind:   "RENDERS",
			Properties: map[string]string{
				"from_component": componentName,
				"to_component":   name,
				"line":           fmt.Sprintf("%d", lineNum),
			},
		})
	}

	return rels
}

// max returns the larger of a and b.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
