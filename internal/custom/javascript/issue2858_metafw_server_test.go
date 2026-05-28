package javascript_test

import "testing"

// issue2858_metafw_server_test.go — proving fixtures for the meta-framework
// Server (server_components, hydration_boundaries) + Data Flow (data_loaders) +
// Build (static_generation) coverage cells closed by issue #2858, for the
// React-based meta-frameworks (Next.js, Remix, Gatsby) plus Nuxt and SvelteKit.
//
// Each fixture is a hand-written, dependency-manifest-free source snippet
// exercised through the registered custom extractors. Astro's equivalents live
// in internal/extractors/astro/issue2858_server_test.go.

// ── Next.js (App Router, RSC) ────────────────────────────────────────────────

func TestNextjs2858UseClientHydrationBoundary(t *testing.T) {
	src := `'use client'
import { useState } from 'react'
export default function Counter() {
  const [n, setN] = useState(0)
  return <button onClick={() => setN(n + 1)}>{n}</button>
}
`
	ents := extract(t, "custom_js_nextjs", fi("app/counter/page.tsx", "typescript", src))
	if !containsSubtype(ents, "client_boundary") {
		t.Error("expected 'use client' hydration boundary (hydration_boundaries)")
	}
	// A 'use client' module is NOT an implicit Server Component.
	if containsSubtype(ents, "server_component") {
		t.Error("'use client' module should not be tagged as implicit server_component")
	}
}

func TestNextjs2858ServerComponentDefault(t *testing.T) {
	// App-Router page with no 'use client' → implicit Server Component.
	src := `export default async function Page() {
  const data = await fetch('https://api.example.com/posts').then(r => r.json())
  return <ul>{data.map((p) => <li key={p.id}>{p.title}</li>)}</ul>
}
`
	ents := extract(t, "custom_js_nextjs", fi("app/blog/page.tsx", "typescript", src))
	if !containsSubtype(ents, "server_component") {
		t.Error("expected implicit RSC server_component (server_components)")
	}
}

func TestNextjs2858ServerActionAndModuleSuffix(t *testing.T) {
	src := `'use server'
export async function createPost(form: FormData) {}
`
	ents := extract(t, "custom_js_nextjs", fi("app/posts/actions.ts", "typescript", src))
	if !containsSubtype(ents, "server_boundary") {
		t.Error("expected 'use server' server boundary (server_components)")
	}
	if !containsSubtype(ents, "server_action") {
		t.Error("expected server_action entity")
	}
	// *.server.ts module suffix → server-only boundary.
	mod := extract(t, "custom_js_nextjs", fi("app/lib/db.server.ts", "typescript", `export const db = connect()`))
	if !containsSubtype(mod, "server_boundary") {
		t.Error("expected *.server.ts server boundary (server_components)")
	}
}

func TestNextjs2858DataLoadersAndStaticGeneration(t *testing.T) {
	pages := `export async function getStaticProps() { return { props: {} } }
export async function getStaticPaths() { return { paths: [], fallback: false } }
`
	ents := extract(t, "custom_js_nextjs", fi("pages/posts/[id].tsx", "typescript", pages))
	if !containsSubtype(ents, "data_loader") {
		t.Error("expected getStaticProps/getStaticPaths data_loader (data_loaders)")
	}
	if !containsSubtype(ents, "static_generation") {
		t.Error("expected static_generation marker for getStaticPaths (static_generation)")
	}

	app := `export async function generateStaticParams() { return [{ id: '1' }] }
export const dynamic = 'force-static'
export const revalidate = 3600
export default function Page() { return <div /> }
`
	a := extract(t, "custom_js_nextjs", fi("app/posts/[id]/page.tsx", "typescript", app))
	if !containsEntity(a, "SCOPE.Operation", "generateStaticParams") {
		t.Error("expected generateStaticParams data_loader (data_loaders)")
	}
	if !containsEntity(a, "SCOPE.Pattern", "dynamic:force-static") {
		t.Error("expected route-segment dynamic config (static_generation)")
	}
	if !containsEntity(a, "SCOPE.Pattern", "revalidate:3600") {
		t.Error("expected revalidate segment config (static_generation)")
	}
}

// ── Remix ─────────────────────────────────────────────────────────────────────

func TestRemix2858LoaderServerBoundaryAndDataLoader(t *testing.T) {
	src := `import { json } from '@remix-run/node'
export async function loader({ params }) { return json({ id: params.id }) }
export async function action({ request }) { return null }
export default function Route() { return <div /> }
`
	ents := extract(t, "custom_js_remix", fi("app/routes/posts.$id.tsx", "typescript", src))
	if !containsSubtype(ents, "data_loader") {
		t.Error("expected loader data_loader (data_loaders)")
	}
	if !containsSubtype(ents, "server_boundary") {
		t.Error("expected loader/action server boundary (server_components)")
	}
}

func TestRemix2858ServerClientModuleSuffix(t *testing.T) {
	s := extract(t, "custom_js_remix", fi("app/utils/session.server.ts", "typescript", `export const getSession = () => {}`))
	if !containsSubtype(s, "server_boundary") {
		t.Error("expected *.server.ts server boundary (server_components)")
	}
	c := extract(t, "custom_js_remix", fi("app/utils/dom.client.ts", "typescript", `export const ls = window.localStorage`))
	if !containsSubtype(c, "client_boundary") {
		t.Error("expected *.client.ts hydration boundary (hydration_boundaries)")
	}
}

func TestRemix2858StaticGeneration(t *testing.T) {
	cfg := `import { vitePlugin as remix } from '@remix-run/dev'
export default { plugins: [remix({ ssr: false })] }
`
	ents := extract(t, "custom_js_remix", fi("vite.config.ts", "typescript", cfg))
	if !containsSubtype(ents, "static_generation") {
		t.Error("expected SPA-mode static_generation marker (static_generation)")
	}
}

// ── Gatsby ──────────────────────────────────────────────────────────────────

func TestGatsby2858StaticGenAndServerComponent(t *testing.T) {
	src := `import { graphql } from 'gatsby'
export default function BlogPost({ data }) { return <article>{data.title}</article> }
export const query = graphql` + "`query { post { title } }`" + `
`
	ents := extract(t, "custom_js_gatsby", fi("src/pages/blog.tsx", "typescript", src))
	if !containsSubtype(ents, "static_generation") {
		t.Error("expected default-static-page + page-query static_generation (static_generation)")
	}
	if !containsSubtype(ents, "server_component") {
		t.Error("expected implicit server_component for static page (server_components)")
	}
	if !containsSubtype(ents, "client_boundary") {
		t.Error("expected hydration boundary for Gatsby page (hydration_boundaries)")
	}
	if !containsEntity(ents, "SCOPE.Operation", "pageQuery") {
		t.Error("expected page GraphQL query data_loader (data_loaders)")
	}
}

func TestGatsby2858ServerDataSSR(t *testing.T) {
	src := `export default function Page() { return <div /> }
export async function getServerData() { return { props: {} } }
`
	ents := extract(t, "custom_js_gatsby", fi("src/pages/live.tsx", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "getServerData") {
		t.Error("expected getServerData data_loader (data_loaders)")
	}
	if !containsSubtype(ents, "server_boundary") {
		t.Error("expected getServerData server boundary (server_components)")
	}
	// getServerData opts the page OUT of default static generation.
	for _, e := range ents {
		if e.Subtype == "static_generation" {
			t.Error("getServerData page should not be marked static_generation")
		}
	}
}

// ── Nuxt ──────────────────────────────────────────────────────────────────────

func TestNuxt2858DataLoaderAndServerBoundary(t *testing.T) {
	page := `<script setup lang="ts">
const { data } = await useAsyncData('users', () => $fetch('/api/users'))
const { data: p } = useFetch('/api/profile')
</script>
<template><ClientOnly><UserList :users="data" /></ClientOnly></template>
`
	ents := extract(t, "custom_js_nuxt", fi("pages/users.vue", "typescript", page))
	if !containsSubtype(ents, "data_loader") {
		t.Error("expected useAsyncData/useFetch data_loader (data_loaders)")
	}
	if !containsSubtype(ents, "client_boundary") {
		t.Error("expected <ClientOnly> hydration boundary (hydration_boundaries)")
	}

	srv := `export default defineEventHandler(async (event) => { return { ok: true } })`
	s := extract(t, "custom_js_nuxt", fi("server/api/users.get.ts", "typescript", srv))
	if !containsSubtype(s, "server_boundary") {
		t.Error("expected Nuxt server-route server boundary (server_components)")
	}
}

func TestNuxt2858StaticGeneration(t *testing.T) {
	cfg := `export default defineNuxtConfig({ routeRules: { '/blog/**': { prerender: true } } })`
	ents := extract(t, "custom_js_nuxt", fi("nuxt.config.ts", "typescript", cfg))
	if !containsSubtype(ents, "static_generation") {
		t.Error("expected routeRules prerender static_generation (static_generation)")
	}
}

func TestNuxt2858ServerComponentSuffix(t *testing.T) {
	ents := extract(t, "custom_js_nuxt", fi("components/Greeting.server.vue", "typescript", `<template><h1>Hi</h1></template>`))
	if !containsSubtype(ents, "server_boundary") {
		t.Error("expected *.server.vue server boundary (server_components)")
	}
}

// ── SvelteKit ─────────────────────────────────────────────────────────────────

func TestSveltekit2858ServerLoadAndBoundary(t *testing.T) {
	src := `import type { PageServerLoad } from './$types'
export const load: PageServerLoad = async ({ params }) => { return { id: params.id } }
`
	ents := extract(t, "custom_js_svelte", fi("src/routes/post/[id]/+page.server.ts", "typescript", src))
	if !containsSubtype(ents, "data_loader") {
		t.Error("expected SvelteKit load data_loader (data_loaders)")
	}
	if !containsSubtype(ents, "server_boundary") {
		t.Error("expected +page.server.ts server boundary (server_components)")
	}
}

func TestSveltekit2858HydrationAndStaticGen(t *testing.T) {
	page := `<script lang="ts">
export const prerender = true
let count = 0
</script>
<button on:click={() => count++}>{count}</button>
`
	ents := extract(t, "custom_js_svelte", fi("src/routes/about/+page.svelte", "typescript", page))
	if !containsSubtype(ents, "client_boundary") {
		t.Error("expected +page.svelte hydration boundary (hydration_boundaries)")
	}
	if !containsSubtype(ents, "static_generation") {
		t.Error("expected prerender static_generation (static_generation)")
	}
}

func TestSveltekit2858RenderModeOptions(t *testing.T) {
	spa := `export const ssr = false`
	s := extract(t, "custom_js_svelte", fi("src/routes/app/+page.ts", "typescript", spa))
	if !containsSubtype(s, "client_boundary") {
		t.Error("expected ssr=false client-only boundary (hydration_boundaries)")
	}
	ssrOnly := `export const csr = false`
	c := extract(t, "custom_js_svelte", fi("src/routes/print/+page.ts", "typescript", ssrOnly))
	if !containsSubtype(c, "server_boundary") {
		t.Error("expected csr=false server-only boundary (server_components)")
	}
}
