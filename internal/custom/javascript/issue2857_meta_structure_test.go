package javascript_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

// rawHasProp runs the named extractor and reports whether an entity with the
// given Name carries Properties[key]==want — used for route_path / router
// convention assertions that the Kind/Subtype/Name-only extract() helper can't
// see.
func rawHasProp(t *testing.T, extractor, path, lang, src, name, key, want string) bool {
	t.Helper()
	e, ok := extreg.Get(extractor)
	if !ok {
		t.Fatalf("extractor %q not registered", extractor)
	}
	ents, err := e.Extract(context.Background(), fi(path, lang, src))
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	for _, ent := range ents {
		if ent.Name == name && ent.Properties[key] == want {
			return true
		}
	}
	return false
}

// issue2857_meta_structure_test.go — proving fixtures for the meta-framework
// Structure (component_extraction, hook_recognition) + Routing (router_pattern,
// route_extraction) coverage cells closed by issue #2857. These are
// hand-written, dependency-manifest-free source snippets exercised through the
// registered custom extractors.

// ── Next.js (React-based): component + hook recognition ───────────────────────

func TestNextjs2857PageComponentAndHooks(t *testing.T) {
	src := `
import { useState, useEffect } from 'react'
import { useMyData } from '../hooks/useMyData'

export default function ProfilePage() {
  const [count, setCount] = useState(0)
  useEffect(() => {}, [])
  const data = useMyData()
  return <div>{count}{data}</div>
}
`
	ents := extract(t, "custom_js_nextjs", fi("app/profile/page.tsx", "typescript", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "ProfilePage") {
		t.Error("expected ProfilePage React component (component_extraction)")
	}
	if !containsEntity(ents, "SCOPE.Operation", "call:useState") {
		t.Error("expected useState hook call (hook_recognition)")
	}
	if !containsEntity(ents, "SCOPE.Operation", "call:useMyData") {
		t.Error("expected custom useMyData hook call (hook_recognition)")
	}
}

func TestNextjs2857CustomHookDefinition(t *testing.T) {
	src := `
import { useState } from 'react'
export function useToggle() {
  const [on, setOn] = useState(false)
  return [on, () => setOn(!on)]
}
`
	ents := extract(t, "custom_js_nextjs", fi("app/dashboard/page.tsx", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "useToggle") {
		t.Error("expected useToggle custom hook definition (hook_recognition)")
	}
}

func TestNextjs2857ApiRouteNoComponent(t *testing.T) {
	// .ts API route handlers must NOT pick up React component/hook entities.
	src := `export async function GET() { return Response.json({}) }`
	ents := extract(t, "custom_js_nextjs", fi("app/api/users/route.ts", "typescript", src))
	for _, e := range ents {
		if e.Kind == "SCOPE.UIComponent" {
			t.Errorf("API route .ts should not emit UIComponent, got %q", e.Name)
		}
	}
}

// ── Remix (React-based): component + hook recognition + router pattern ────────

func TestRemix2857ComponentHooksRouter(t *testing.T) {
	src := `
import { useState } from 'react'
export function loader() { return {} }
export default function PostRoute() {
  const [n] = useState(0)
  return <article>{n}</article>
}
`
	ents := extract(t, "custom_js_remix", fi("app/routes/posts.$id.tsx", "typescript", src))
	if !containsSubtype(ents, "route_component") {
		t.Error("expected Remix route_component (component_extraction)")
	}
	if !containsEntity(ents, "SCOPE.Operation", "call:useState") {
		t.Error("expected useState hook call (hook_recognition)")
	}
	// route_path derived from routes/ file convention proves router_pattern.
	if !rawHasProp(t, "custom_js_remix", "app/routes/posts.$id.tsx", "typescript", src,
		"loader:/posts/{id}", "route_path", "/posts/{id}") {
		t.Error("expected loader route_path /posts/{id} (router_pattern)")
	}
}

// ── Gatsby (React-based): file route + programmatic route + component + hook ──

func TestGatsby2857PageRouteAndComponent(t *testing.T) {
	src := `
import { useStaticQuery, graphql } from 'gatsby'
export default function AboutPage() {
  const data = useStaticQuery(graphql` + "`" + `query { site { id } }` + "`" + `)
  return <main>{data}</main>
}
`
	ents := extract(t, "custom_js_gatsby", fi("src/pages/about.tsx", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "/about") {
		t.Error("expected /about file-system route (route_extraction + router_pattern)")
	}
	if !containsSubtype(ents, "page_route") {
		t.Error("expected page_route subtype (router_pattern)")
	}
	if !containsEntity(ents, "SCOPE.UIComponent", "AboutPage") {
		t.Error("expected AboutPage React component (component_extraction)")
	}
	if !containsEntity(ents, "SCOPE.Operation", "call:useStaticQuery") {
		t.Error("expected useStaticQuery hook call (hook_recognition)")
	}
}

func TestGatsby2857IndexRoute(t *testing.T) {
	src := `export default function Home() { return <div /> }`
	ents := extract(t, "custom_js_gatsby", fi("src/pages/index.tsx", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "/") {
		t.Error("expected index → / route")
	}
}

func TestGatsby2857DynamicRoute(t *testing.T) {
	src := `export default function Post() { return <article /> }`
	if !rawHasProp(t, "custom_js_gatsby", "src/pages/blog/[slug].tsx", "typescript", src,
		"/blog/{slug}", "route_path", "/blog/{slug}") {
		t.Error("expected dynamic /blog/{slug} route (router_pattern)")
	}
}

func TestGatsby2857ProgrammaticRoute(t *testing.T) {
	src := `
exports.createPages = async ({ actions }) => {
  actions.createPage({ path: '/products/widget', component: require.resolve('./tpl.tsx') })
}
`
	ents := extract(t, "custom_js_gatsby", fi("gatsby-node.js", "javascript", src))
	if !containsSubtype(ents, "programmatic_route") {
		t.Errorf("expected programmatic_route from createPage (router_pattern), got %#v", ents)
	}
}
