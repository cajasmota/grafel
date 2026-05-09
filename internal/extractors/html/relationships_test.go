package html_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/html"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractRels runs the registered html extractor against src and returns the
// resulting entity records (which carry embedded RelationshipRecords).
func extractRels(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("html")
	if !ok {
		t.Fatal("html extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "html",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return entities
}

func relsByKind(entities []types.EntityRecord, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == kind {
				out = append(out, r)
			}
		}
	}
	return out
}

// ---- IMPORTS — script src --------------------------------------------------

func TestRelationships_Imports_ScriptSrc(t *testing.T) {
	src := `<html><head><script src="/static/app.js"></script></head><body></body></html>`
	entities := extractRels(t, "page.html", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 1 {
		t.Fatalf("IMPORTS count = %d, want 1", len(rels))
	}
	r := rels[0]
	if r.FromID != "page.html" {
		t.Errorf("FromID = %q, want page.html", r.FromID)
	}
	if r.ToID != "/static/app.js" {
		t.Errorf("ToID = %q, want /static/app.js", r.ToID)
	}
	if got := r.Properties["local_name"]; got != "app.js" {
		t.Errorf("local_name = %q, want app.js", got)
	}
	if got := r.Properties["source_module"]; got != "/static/app.js" {
		t.Errorf("source_module = %q, want /static/app.js", got)
	}
	if got, ok := r.Properties["imported_name"]; !ok || got != "" {
		t.Errorf("imported_name = %q (present=%v), want empty present", got, ok)
	}
}

// ---- IMPORTS — link stylesheet href ---------------------------------------

func TestRelationships_Imports_LinkStylesheet(t *testing.T) {
	src := `<html><head><link rel="stylesheet" href="/css/main.css"></head><body></body></html>`
	entities := extractRels(t, "page.html", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 1 {
		t.Fatalf("IMPORTS count = %d, want 1", len(rels))
	}
	if rels[0].ToID != "/css/main.css" {
		t.Errorf("ToID = %q", rels[0].ToID)
	}
	if rels[0].Properties["local_name"] != "main.css" {
		t.Errorf("local_name = %q, want main.css", rels[0].Properties["local_name"])
	}
}

// Non-stylesheet <link> (e.g. rel="icon") should not produce IMPORTS — the
// extractor only models stylesheet includes today.
func TestRelationships_Imports_LinkIconNotEmitted(t *testing.T) {
	src := `<html><head><link rel="icon" href="/favicon.ico"></head><body></body></html>`
	entities := extractRels(t, "page.html", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 0 {
		t.Fatalf("IMPORTS count = %d, want 0 (non-stylesheet link must not import)", len(rels))
	}
}

// ---- IMPORTS — img src -----------------------------------------------------

func TestRelationships_Imports_ImgSrc(t *testing.T) {
	src := `<html><body><img src="/img/hero.png" alt="hero"></body></html>`
	entities := extractRels(t, "page.html", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 1 {
		t.Fatalf("IMPORTS count = %d, want 1", len(rels))
	}
	if rels[0].ToID != "/img/hero.png" {
		t.Errorf("ToID = %q", rels[0].ToID)
	}
	if rels[0].Properties["local_name"] != "hero.png" {
		t.Errorf("local_name = %q, want hero.png", rels[0].Properties["local_name"])
	}
}

// ---- IMPORTS — multiple mixed asset references -----------------------------

func TestRelationships_Imports_Multiple(t *testing.T) {
	src := `<html>
<head>
  <link rel="stylesheet" href="/css/main.css">
  <link rel="stylesheet" href="https://cdn.example.com/lib.css">
  <script src="/js/app.js"></script>
</head>
<body>
  <img src="/img/a.png">
  <img src="b.svg">
</body>
</html>`
	entities := extractRels(t, "page.html", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 5 {
		t.Fatalf("IMPORTS count = %d, want 5", len(rels))
	}
	wantTargets := map[string]bool{
		"/css/main.css":                    false,
		"https://cdn.example.com/lib.css":  false,
		"/js/app.js":                       false,
		"/img/a.png":                       false,
		"b.svg":                            false,
	}
	for _, r := range rels {
		if _, ok := wantTargets[r.ToID]; ok {
			wantTargets[r.ToID] = true
		}
		if r.FromID != "page.html" {
			t.Errorf("FromID = %q, want page.html", r.FromID)
		}
	}
	for k, seen := range wantTargets {
		if !seen {
			t.Errorf("missing IMPORTS edge to %q", k)
		}
	}
}

// ---- IMPORTS — query string and fragment in local_name --------------------

func TestRelationships_Imports_StripsQueryAndFragment(t *testing.T) {
	src := `<html><head><script src="/js/app.js?v=42#hash"></script></head><body></body></html>`
	entities := extractRels(t, "page.html", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 1 {
		t.Fatalf("IMPORTS count = %d, want 1", len(rels))
	}
	if got := rels[0].Properties["local_name"]; got != "app.js" {
		t.Errorf("local_name = %q, want app.js (query/fragment stripped)", got)
	}
	// source_module preserves the raw value.
	if got := rels[0].Properties["source_module"]; got != "/js/app.js?v=42#hash" {
		t.Errorf("source_module = %q, want raw value preserved", got)
	}
}

// ---- IMPORTS — script with no src is not an import ------------------------

func TestRelationships_Imports_InlineScriptNotEmitted(t *testing.T) {
	src := `<html><head><script>console.log("hi")</script></head><body></body></html>`
	entities := extractRels(t, "page.html", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 0 {
		t.Fatalf("IMPORTS count = %d, want 0 for inline script", len(rels))
	}
}

// ---- IMPORTS — empty href/src is not an import ----------------------------

func TestRelationships_Imports_EmptyAttrNotEmitted(t *testing.T) {
	src := `<html><body><img src="" alt="broken"></body></html>`
	entities := extractRels(t, "page.html", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 0 {
		t.Fatalf("IMPORTS count = %d, want 0 for empty src", len(rels))
	}
}

// ---- CALLS / CONTAINS — not applicable to HTML ----------------------------
//
// HTML templates do not model functions, methods, or class containment, so
// the html extractor intentionally does not emit CALLS or CONTAINS edges.
// These tests pin that behaviour so accidental additions trip a regression.

func TestRelationships_Calls_NotEmitted(t *testing.T) {
	src := `<html><body><button onclick="handleClick()">Go</button></body></html>`
	entities := extractRels(t, "page.html", src)
	if rels := relsByKind(entities, "CALLS"); len(rels) != 0 {
		t.Fatalf("CALLS count = %d, want 0 (not applicable to HTML)", len(rels))
	}
}

func TestRelationships_Contains_NotEmitted(t *testing.T) {
	src := `<html><body><form action="/submit"><input name="email"></form></body></html>`
	entities := extractRels(t, "page.html", src)
	if rels := relsByKind(entities, "CONTAINS"); len(rels) != 0 {
		t.Fatalf("CONTAINS count = %d, want 0 (not applicable to HTML)", len(rels))
	}
}
