package astro_test

import "testing"

// issue2857_routing_test.go — proving fixtures for Astro Structure
// (component_extraction) + Routing (router_pattern) coverage cells closed by
// issue #2857. Astro uses file-system routing under pages/.

func TestAstro2857PageRouterPattern(t *testing.T) {
	src := `---
const { title } = Astro.props
---
<h1>{title}</h1>`
	recs := mustExtract(t, "src/pages/about.astro", src)
	page := findByName(recs, "about")
	if page == nil {
		t.Fatal("expected page component entity (component_extraction)")
	}
	if page.Subtype != "astro_page" {
		t.Errorf("expected astro_page subtype, got %q", page.Subtype)
	}
	if page.Properties["route_path"] != "/about" {
		t.Errorf("expected route_path /about (router_pattern), got %q", page.Properties["route_path"])
	}
	if page.Properties["router"] != "file_system" {
		t.Errorf("expected router=file_system, got %q", page.Properties["router"])
	}
}

func TestAstro2857IndexRoute(t *testing.T) {
	recs := mustExtract(t, "src/pages/index.astro", `<h1>Home</h1>`)
	page := findByName(recs, "index")
	if page == nil {
		t.Fatal("expected index page entity")
	}
	if page.Properties["route_path"] != "/" {
		t.Errorf("expected index → /, got %q", page.Properties["route_path"])
	}
}

func TestAstro2857DynamicRoute(t *testing.T) {
	recs := mustExtract(t, "src/pages/blog/[slug].astro", `<article />`)
	page := findByName(recs, "[slug]")
	if page == nil {
		t.Fatal("expected dynamic page entity")
	}
	if page.Properties["route_path"] != "/blog/{slug}" {
		t.Errorf("expected /blog/{slug}, got %q", page.Properties["route_path"])
	}
}

func TestAstro2857RestRoute(t *testing.T) {
	recs := mustExtract(t, "src/pages/[...path].astro", `<div />`)
	page := findByName(recs, "[...path]")
	if page == nil {
		t.Fatal("expected rest page entity")
	}
	if page.Properties["route_path"] != "/{path*}" {
		t.Errorf("expected /{path*}, got %q", page.Properties["route_path"])
	}
}

func TestAstro2857ComponentNotPage(t *testing.T) {
	// A component (not under pages/) carries no route_path.
	recs := mustExtract(t, "src/components/Header.astro", `<header />`)
	comp := findByName(recs, "Header")
	if comp == nil {
		t.Fatal("expected component entity (component_extraction)")
	}
	if comp.Subtype != "astro_component" {
		t.Errorf("expected astro_component subtype, got %q", comp.Subtype)
	}
	if _, ok := comp.Properties["route_path"]; ok {
		t.Error("non-page component should not have route_path")
	}
}
