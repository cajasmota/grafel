package javascript_test

import "testing"

// issue2878_metafw_idioms_test.go — proving fixtures for the meta-framework
// framework_specific *idiom* cells closed by issue #2878. These are the
// first-class framework idioms the generic meta_framework columns
// (server_components / data_loaders / hydration_boundaries / static_generation /
// route_extraction, closed by #2858 / #2880) cannot express:
//
//   Next.js : use_client_server_directive, server_actions,
//             middleware_runtime_detection, next_config_detection
//   Nuxt    : nuxt_auto_import, nuxt_server_routes
//   Remix   : remix_loader_action_pair
//   SvelteKit: sveltekit_load_function, sveltekit_form_actions
//   Gatsby  : gatsby_graphql_pagequery
//
// (Astro's idiom cells — astro_island_directive, astro_frontmatter_fetch — are
// proven in internal/extractors/astro/issue2878_idioms_test.go.)
//
// Each fixture is hand-written and dependency-manifest-free, exercised through
// the registered custom extractors.

// ── Next.js: 'use client' / 'use server' directive boundary ──────────────────

func TestNext2878UseClientServerDirective(t *testing.T) {
	client := `'use client'
import { useState } from 'react'
export default function Toggle() {
  const [on, setOn] = useState(false)
  return <button onClick={() => setOn(!on)}>{String(on)}</button>
}
`
	c := extract(t, "custom_js_nextjs", fi("app/toggle/page.tsx", "typescript", client))
	if !containsEntity(c, "SCOPE.Pattern", "use client") {
		t.Error("expected 'use client' directive boundary (use_client_server_directive)")
	}

	server := `'use server'
export async function submit(form: FormData) {}
`
	s := extract(t, "custom_js_nextjs", fi("app/lib/actions.ts", "typescript", server))
	if !containsEntity(s, "SCOPE.Pattern", "use server") {
		t.Error("expected 'use server' directive boundary (use_client_server_directive)")
	}
}

// ── Next.js: server actions ('use server' + exported async fns) ──────────────

func TestNext2878ServerActions(t *testing.T) {
	src := `'use server'
export async function createTodo(form: FormData) {}
export async function deleteTodo(id: string) {}
`
	ents := extract(t, "custom_js_nextjs", fi("app/todos/actions.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "createTodo") {
		t.Error("expected createTodo server_action (server_actions)")
	}
	if !containsEntity(ents, "SCOPE.Operation", "deleteTodo") {
		t.Error("expected deleteTodo server_action (server_actions)")
	}
	if !containsSubtype(ents, "server_action") {
		t.Error("expected server_action subtype (server_actions)")
	}
}

// ── Next.js: middleware runtime detection ────────────────────────────────────

func TestNext2878MiddlewareRuntimeDetection(t *testing.T) {
	// Default (no runtime override) → edge runtime.
	edge := `import { NextResponse } from 'next/server'
export function middleware(request) { return NextResponse.next() }
export const config = { matcher: ['/dashboard/:path*'] }
`
	e := extract(t, "custom_js_nextjs", fi("middleware.ts", "typescript", edge))
	if !containsEntity(e, "SCOPE.Pattern", "middleware") {
		t.Fatal("expected middleware entity (middleware_runtime_detection)")
	}

	// Explicit nodejs runtime override.
	node := `export async function middleware(req) {}
export const config = { runtime: 'nodejs', matcher: '/api/:path*' }
`
	n := extract(t, "custom_js_nextjs", fi("src/middleware.js", "javascript", node))
	if !containsEntity(n, "SCOPE.Pattern", "middleware") {
		t.Error("expected middleware entity with nodejs runtime (middleware_runtime_detection)")
	}

	// A non-middleware file must NOT emit a middleware marker.
	other := `export function middleware() {}`
	o := extract(t, "custom_js_nextjs", fi("app/lib/helpers.ts", "typescript", other))
	if containsSubtype(o, "middleware") {
		t.Error("non-middleware.ts file should not emit a middleware marker")
	}
}

// ── Next.js: next.config detection ───────────────────────────────────────────

func TestNext2878NextConfigDetection(t *testing.T) {
	cfg := `/** @type {import('next').NextConfig} */
const nextConfig = { reactStrictMode: true, images: { domains: ['cdn.example.com'] } }
module.exports = nextConfig
`
	c := extract(t, "custom_js_nextjs", fi("next.config.js", "javascript", cfg))
	if !containsEntity(c, "SCOPE.Pattern", "next.config") {
		t.Error("expected next.config framework_config (next_config_detection)")
	}

	ts := `import type { NextConfig } from 'next'
const config: NextConfig = { experimental: { ppr: true } }
export default config
`
	tc := extract(t, "custom_js_nextjs", fi("next.config.ts", "typescript", ts))
	if !containsSubtype(tc, "framework_config") {
		t.Error("expected next.config framework_config from .ts (next_config_detection)")
	}
}

// ── Nuxt: auto-import ─────────────────────────────────────────────────────────

func TestNuxt2878AutoImport(t *testing.T) {
	// useState / useRoute used with NO import → resolved by Nuxt auto-import.
	page := `<script setup lang="ts">
const counter = useState('count', () => 0)
const route = useRoute()
function inc() { counter.value++ }
</script>
<template><button @click="inc">{{ counter }} {{ route.path }}</button></template>
`
	ents := extract(t, "custom_js_nuxt", fi("pages/counter.vue", "typescript", page))
	if !containsSubtype(ents, "auto_import") {
		t.Fatal("expected auto_import marker (nuxt_auto_import)")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "auto_import:useState") {
		t.Error("expected auto_import:useState marker (nuxt_auto_import)")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "auto_import:useRoute") {
		t.Error("expected auto_import:useRoute marker (nuxt_auto_import)")
	}
}

func TestNuxt2878AutoImportSkipsExplicitImport(t *testing.T) {
	// An explicitly-imported helper is a normal import, not an auto-import.
	src := `<script setup lang="ts">
import { useState } from '#imports'
const n = useState('n', () => 1)
</script>
`
	ents := extract(t, "custom_js_nuxt", fi("pages/explicit.vue", "typescript", src))
	if containsEntity(ents, "SCOPE.Pattern", "auto_import:useState") {
		t.Error("explicitly-imported useState should not be marked as auto_import")
	}
}

// ── Nuxt: server routes (file-system server-route convention) ────────────────

func TestNuxt2878ServerRoutes(t *testing.T) {
	api := `export default defineEventHandler(async (event) => { return { users: [] } })`
	a := extract(t, "custom_js_nuxt", fi("server/api/users.get.ts", "typescript", api))
	if !containsEntity(a, "SCOPE.Pattern", "server_route:users.get") {
		t.Error("expected server_route marker for server/api handler (nuxt_server_routes)")
	}

	routes := `export default defineEventHandler((event) => 'hello')`
	r := extract(t, "custom_js_nuxt", fi("server/routes/hello.ts", "typescript", routes))
	if !containsSubtype(r, "server_route") {
		t.Error("expected server_route marker for server/routes handler (nuxt_server_routes)")
	}

	// A non-server-route file must NOT emit a server_route marker.
	page := `<script setup>const x = 1</script>`
	p := extract(t, "custom_js_nuxt", fi("pages/index.vue", "typescript", page))
	if containsSubtype(p, "server_route") {
		t.Error("a pages/ file should not emit a server_route marker")
	}
}

// ── Remix: loader/action pairing ─────────────────────────────────────────────

func TestRemix2878LoaderActionPair(t *testing.T) {
	both := `import { json } from '@remix-run/node'
export async function loader({ params }) { return json({ id: params.id }) }
export async function action({ request }) { return json({ ok: true }) }
export default function Route() { return <div /> }
`
	b := extract(t, "custom_js_remix", fi("app/routes/posts.$id.tsx", "typescript", both))
	if !containsSubtype(b, "loader_action_pair") {
		t.Error("expected loader_action_pair marker when both loader+action present (remix_loader_action_pair)")
	}

	// Loader only → no pair.
	loaderOnly := `export async function loader() { return null }
export default function Route() { return <div /> }
`
	l := extract(t, "custom_js_remix", fi("app/routes/index.tsx", "typescript", loaderOnly))
	if containsSubtype(l, "loader_action_pair") {
		t.Error("loader-only route should not emit a loader_action_pair marker")
	}
}

// ── SvelteKit: load() function ────────────────────────────────────────────────

func TestSveltekit2878LoadFunction(t *testing.T) {
	universal := `import type { PageLoad } from './$types'
export const load: PageLoad = async ({ fetch }) => {
  const res = await fetch('/api/posts')
  return { posts: await res.json() }
}
`
	u := extract(t, "custom_js_svelte", fi("src/routes/blog/+page.ts", "typescript", universal))
	if !containsEntity(u, "SCOPE.Operation", "load:/blog") {
		t.Error("expected universal load() data_loader (sveltekit_load_function)")
	}

	server := `import type { PageServerLoad } from './$types'
export const load: PageServerLoad = async ({ params }) => { return { id: params.id } }
`
	s := extract(t, "custom_js_svelte", fi("src/routes/post/[id]/+page.server.ts", "typescript", server))
	if !containsSubtype(s, "data_loader") {
		t.Error("expected server load() data_loader (sveltekit_load_function)")
	}
}

// ── SvelteKit: form actions ───────────────────────────────────────────────────

func TestSveltekit2878FormActions(t *testing.T) {
	src := `import type { Actions } from './$types'
export const actions: Actions = {
  default: async ({ request }) => { const data = await request.formData(); return { success: true } },
  delete: async ({ request }) => { return { deleted: true } },
}
`
	ents := extract(t, "custom_js_svelte", fi("src/routes/contact/+page.server.ts", "typescript", src))
	if !containsSubtype(ents, "form_actions") {
		t.Error("expected form_actions marker (sveltekit_form_actions)")
	}
	if !containsEntity(ents, "SCOPE.Operation", "actions:/contact") {
		t.Error("expected actions:/contact form_actions node (sveltekit_form_actions)")
	}
}

// ── Gatsby: GraphQL page query ────────────────────────────────────────────────

func TestGatsby2878GraphqlPageQuery(t *testing.T) {
	src := "import { graphql } from 'gatsby'\n" +
		"export default function BlogPost({ data }) { return <article>{data.markdownRemark.frontmatter.title}</article> }\n" +
		"export const query = graphql`\n" +
		"  query($slug: String!) {\n" +
		"    markdownRemark(fields: { slug: { eq: $slug } }) { frontmatter { title } }\n" +
		"  }\n" +
		"`\n"
	ents := extract(t, "custom_js_gatsby", fi("src/templates/blog-post.tsx", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "pageQuery") {
		t.Error("expected pageQuery data_loader from `export const query = graphql` (gatsby_graphql_pagequery)")
	}
	if !containsSubtype(ents, "data_loader") {
		t.Error("expected data_loader subtype for the page query (gatsby_graphql_pagequery)")
	}
}
