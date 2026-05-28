package javascript_test

import "testing"

// issue2880_metafw_routing_test.go — proving fixtures for the non-React
// meta-framework Structure (component_extraction) + Routing (router_pattern)
// coverage cells closed by issue #2880: Nuxt (Vue-based) and SvelteKit
// (Svelte-based). These are hand-written, dependency-manifest-free source
// snippets exercised through the registered custom extractors.

// ── Nuxt (Vue-based): page component + file-system router pattern ─────────────

func TestNuxt2880PageComponentAndRouter(t *testing.T) {
	src := `
<script setup lang="ts">
const { data } = await useAsyncData('p', () => $fetch('/api/p'))
defineProps<{ id: string }>()
</script>
<template><div>{{ data }}</div></template>
`
	path := "pages/users/[id].vue"
	ents := extract(t, "custom_js_nuxt", fi(path, "typescript", src))

	// Structure/component_extraction: the Vue SFC page component.
	if !containsEntity(ents, "SCOPE.UIComponent", "Id") {
		t.Errorf("expected Nuxt page UIComponent (component_extraction), got %#v", ents)
	}
	if !containsSubtype(ents, "page") {
		t.Error("expected page subtype on Nuxt component")
	}

	// Routing/router_pattern: file-system route convention on the endpoint.
	if !rawHasProp(t, "custom_js_nuxt", path, "typescript", src,
		"/users/{id}", "router", "file_system") {
		t.Error("expected /users/{id} endpoint tagged router=file_system (router_pattern)")
	}
	// router convention also recorded on the component node.
	if !rawHasProp(t, "custom_js_nuxt", path, "typescript", src,
		"Id", "route_path", "/users/{id}") {
		t.Error("expected page component carrying route_path /users/{id}")
	}

	// Routing/router_pattern: declared `[id]` route-param source node.
	if !containsEntity(ents, "SCOPE.Pattern", "param:id") {
		t.Error("expected route_param node param:id (router_pattern)")
	}
	if !rawHasProp(t, "custom_js_nuxt", path, "typescript", src,
		"param:id", "source_segment", "[id]") {
		t.Error("expected param:id source_segment [id]")
	}
}

func TestNuxt2880IndexRouteAndCatchAll(t *testing.T) {
	// index.vue → "/" ; defineComponent name wins over filename.
	idxSrc := `<script>export default defineComponent({ name: 'HomePage' })</script>`
	idx := extract(t, "custom_js_nuxt", fi("pages/index.vue", "javascript", idxSrc))
	if !containsEntity(idx, "SCOPE.UIComponent", "HomePage") {
		t.Errorf("expected HomePage component from defineComponent name, got %#v", idx)
	}
	if !containsEntity(idx, "SCOPE.Operation", "/") {
		t.Error("expected index.vue → / route")
	}

	// catch-all [...slug] → declared catch_all route param.
	caSrc := `<script setup></script><template/>`
	if !rawHasProp(t, "custom_js_nuxt", "pages/docs/[...slug].vue", "typescript", caSrc,
		"param:slug", "catch_all", "true") {
		t.Error("expected catch-all param:slug catch_all=true")
	}
}

func TestNuxt2880NonPageVueNoPageComponent(t *testing.T) {
	// A .vue file outside pages/ must not be tagged as a routed page component.
	src := `<script setup></script><template/>`
	ents := extract(t, "custom_js_nuxt", fi("components/Widget.vue", "typescript", src))
	if containsSubtype(ents, "page") {
		t.Errorf("non-pages .vue must not emit a page component, got %#v", ents)
	}
}

// ── SvelteKit (Svelte-based): page component + router pattern + param source ──

func TestSvelteKit2880PageComponentAndRouter(t *testing.T) {
	src := `
<script lang="ts">
  export let data
</script>
<article>{data.post.title}</article>
`
	path := "src/routes/post/[id]/+page.svelte"
	ents := extract(t, "custom_js_svelte", fi(path, "typescript", src))

	// Structure/component_extraction: the +page.svelte component (subtype page).
	if !containsSubtype(ents, "page") {
		t.Errorf("expected SvelteKit page component (component_extraction), got %#v", ents)
	}
	// Routing/router_pattern: file-system route convention + derived route_path.
	if !rawHasProp(t, "custom_js_svelte", path, "typescript", src,
		"Id", "router", "file_system") {
		t.Error("expected page component tagged router=file_system (router_pattern)")
	}
	if !rawHasProp(t, "custom_js_svelte", path, "typescript", src,
		"Id", "route_path", "/post/{id}") {
		t.Error("expected page component route_path /post/{id}")
	}
	// Declared `[id]` route-param source node.
	if !containsEntity(ents, "SCOPE.Pattern", "param:id") {
		t.Error("expected route_param node param:id (router_pattern)")
	}
}

func TestSvelteKit2880LoadParamsSourceDetection(t *testing.T) {
	// A +page.server.ts load() reading params.id must record params_read=id,
	// wiring the read to the declared [id] route segment (def-use source).
	src := `
export const load = async ({ params }) => {
  const post = await db.get(params.id)
  return { post }
}
`
	path := "src/routes/post/[id]/+page.server.ts"
	if !rawHasProp(t, "custom_js_svelte", path, "typescript", src,
		"load:/post/{id}", "params_read", "id") {
		t.Error("expected load() params_read=id wired to [id] segment (param source detection)")
	}
	// The declared route-param node is the resolvable source.
	ents := extract(t, "custom_js_svelte", fi(path, "typescript", src))
	if !containsEntity(ents, "SCOPE.Pattern", "param:id") {
		t.Error("expected route_param source node param:id")
	}
}

func TestSvelteKit2880UndeclaredParamNotRecorded(t *testing.T) {
	// `params.foo` with no `[foo]` segment must NOT be recorded — honest def-use:
	// only reads backed by a real route segment resolve to a source.
	src := `
export const load = async ({ params }) => {
  return { x: params.notARoute }
}
`
	path := "src/routes/post/[id]/+page.server.ts"
	if rawHasProp(t, "custom_js_svelte", path, "typescript", src,
		"load:/post/{id}", "params_read", "notARoute") {
		t.Error("undeclared param must not be recorded as a route-param read")
	}
	if rawHasProp(t, "custom_js_svelte", path, "typescript", src,
		"load:/post/{id}", "params_read", "id") {
		t.Error("params.id not read in this load(); must not be recorded")
	}
}

func TestSvelteKit2880MatcherParamSource(t *testing.T) {
	// SvelteKit matcher segment `[page=integer]` declares param `page`.
	src := `<script lang="ts"></script>`
	path := "src/routes/blog/[page=integer]/+page.svelte"
	if !rawHasProp(t, "custom_js_svelte", path, "typescript", src,
		"param:page", "param_name", "page") {
		t.Error("expected matcher segment to declare param:page (param_name=page)")
	}
}
