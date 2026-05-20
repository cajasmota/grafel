package resolve

import "regexp"

// svelteDynamicPatterns are per-language patterns for Svelte (.svelte) files.
// Registered via init() into dynamicPatternsByLang.
//
// The Svelte extractor emits CALLS edges from script blocks. Three groups of
// callee shapes appear as unresolvable stubs without this catalog:
//
//  1. Svelte 5 runes — $state, $derived, $effect, $props, $bindable.
//     These are compiler-intrinsics, not functions in the graph.
//
//  2. Svelte lifecycle helpers — onMount, onDestroy, beforeUpdate, afterUpdate,
//     tick. Imported from "svelte", never indexed as user entities.
//
//  3. Svelte stores — writable, readable, derived, get. Imported from
//     "svelte/store".
//
//  4. SvelteKit navigation/routing helpers — goto, pushState, replaceState,
//     invalidate, invalidateAll, preloadData, preloadCode. Imported from
//     "$app/navigation".
//
//  5. SvelteKit page store — the `page` store imported from "$app/stores".
//     Bare identifiers like `$page.url` are stripped to `page` after the
//     receiver is removed; same for `navigating`, `updated`.
//
// All patterns are gated to lang=="svelte" to prevent collisions with Go,
// Python, or JavaScript identifiers of the same name.
var svelteDynamicPatterns = []*regexp.Regexp{
	// ── Svelte 5 runes ────────────────────────────────────────────────────
	// Runes are compiler-intrinsics (prefixed with $); the extractor emits
	// them as SCOPE.Operation entities but may also emit CALLS edges for bare
	// rune invocations that appear as expression statements.
	regexp.MustCompile(`^\$state$`),
	regexp.MustCompile(`^\$derived$`),
	regexp.MustCompile(`^\$effect$`),
	regexp.MustCompile(`^\$effect\.pre$`),   // $effect.pre(() => {})
	regexp.MustCompile(`^\$effect\.root$`),  // $effect.root(() => {})
	regexp.MustCompile(`^\$props$`),
	regexp.MustCompile(`^\$bindable$`),
	regexp.MustCompile(`^\$inspect$`),       // $inspect(value) — Svelte 5 debug rune
	regexp.MustCompile(`^\$host$`),          // $host() — Svelte 5 custom element host rune

	// ── Svelte 4 lifecycle helpers ────────────────────────────────────────
	// Imported from "svelte". These run in the component lifecycle but are
	// never graph entities in the indexed codebase.
	regexp.MustCompile(`^onMount$`),
	regexp.MustCompile(`^onDestroy$`),
	regexp.MustCompile(`^beforeUpdate$`),
	regexp.MustCompile(`^afterUpdate$`),
	regexp.MustCompile(`^tick$`),
	regexp.MustCompile(`^setContext$`),
	regexp.MustCompile(`^getContext$`),
	regexp.MustCompile(`^hasContext$`),
	regexp.MustCompile(`^getAllContexts$`),
	regexp.MustCompile(`^createEventDispatcher$`),

	// ── Svelte store helpers ──────────────────────────────────────────────
	// Imported from "svelte/store".
	regexp.MustCompile(`^writable$`),
	regexp.MustCompile(`^readable$`),
	regexp.MustCompile(`^derived$`),
	regexp.MustCompile(`^get$`),

	// ── SvelteKit navigation ──────────────────────────────────────────────
	// Imported from "$app/navigation".
	regexp.MustCompile(`^goto$`),
	regexp.MustCompile(`^pushState$`),
	regexp.MustCompile(`^replaceState$`),
	regexp.MustCompile(`^invalidate$`),
	regexp.MustCompile(`^invalidateAll$`),
	regexp.MustCompile(`^preloadData$`),
	regexp.MustCompile(`^preloadCode$`),
	regexp.MustCompile(`^beforeNavigate$`),
	regexp.MustCompile(`^afterNavigate$`),
	regexp.MustCompile(`^onNavigate$`),

	// ── SvelteKit stores ──────────────────────────────────────────────────
	// Imported from "$app/stores". After the extractor strips the $ sigil
	// prefix (used as auto-subscription shorthand), these appear as bare
	// identifiers.
	regexp.MustCompile(`^page$`),
	regexp.MustCompile(`^navigating$`),
	regexp.MustCompile(`^updated$`),

	// ── SvelteKit environment / paths ─────────────────────────────────────
	// Imported from "$app/environment" / "$app/paths".
	regexp.MustCompile(`^browser$`),
	regexp.MustCompile(`^dev$`),
	regexp.MustCompile(`^building$`),
	regexp.MustCompile(`^version$`),
	regexp.MustCompile(`^base$`),
	regexp.MustCompile(`^assets$`),

	// ── Svelte transition / animation / action helpers ────────────────────
	// Imported from "svelte/transition", "svelte/animate", "svelte/action".
	regexp.MustCompile(`^fade$`),
	regexp.MustCompile(`^fly$`),
	regexp.MustCompile(`^slide$`),
	regexp.MustCompile(`^scale$`),
	regexp.MustCompile(`^blur$`),
	regexp.MustCompile(`^crossfade$`),
	regexp.MustCompile(`^draw$`),
	regexp.MustCompile(`^flip$`),
}

func init() {
	dynamicPatternsByLang["svelte"] = svelteDynamicPatterns
}
