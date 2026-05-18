package html_test

import (
	"context"
	"strings"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tshtml "github.com/smacker/go-tree-sitter/html"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/html"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func parseHTML(t *testing.T, src string) *sitter.Tree {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(tshtml.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func extractEntities(t *testing.T, path, src string, tree *sitter.Tree) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("html")
	if !ok {
		t.Fatal("html extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "html",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return entities
}

func findBySubtype(entities []types.EntityRecord, subtype string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range entities {
		if e.Subtype == subtype {
			out = append(out, e)
		}
	}
	return out
}

func findByKindAndName(entities []types.EntityRecord, kind, name string) *types.EntityRecord {
	for i := range entities {
		if entities[i].Kind == kind && entities[i].Name == name {
			return &entities[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func TestHTMLExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("html")
	if !ok {
		t.Fatal("html extractor not registered under key 'html'")
	}
}

func TestHTMLExtractor_Language(t *testing.T) {
	ext, _ := extractor.Get("html")
	if ext.Language() != "html" {
		t.Errorf("expected Language()='html', got %q", ext.Language())
	}
}

// ---------------------------------------------------------------------------
// Empty / nil input
// ---------------------------------------------------------------------------

func TestHTMLExtractor_EmptyContent(t *testing.T) {
	ext, _ := extractor.Get("html")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "index.html",
		Content:  []byte{},
		Language: "html",
	})
	if err != nil {
		t.Fatalf("unexpected error on empty content: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities for empty content, got %d", len(entities))
	}
}

func TestHTMLExtractor_NilTree_InlineParse(t *testing.T) {
	// When Tree is nil but Content is non-empty, extractor must parse inline.
	ext, _ := extractor.Get("html")
	src := `<html><body><form action="/inline-parse"></form></body></html>`
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "index.html",
		Content:  []byte(src),
		Language: "html",
		Tree:     nil, // triggers inline parse
	})
	if err != nil {
		t.Fatalf("unexpected error on nil tree + inline parse: %v", err)
	}
	found := findByKindAndName(entities, "SCOPE.Operation", "/inline-parse")
	if found == nil {
		t.Errorf("expected form entity from inline parse, entities: %v", entities)
	}
}

// ---------------------------------------------------------------------------
// AC1: Form element → SCOPE.Operation with action
// ---------------------------------------------------------------------------

func TestHTMLExtractor_Form_WithAction(t *testing.T) {
	src := `<html><body><form action="/api/login" method="post"><input type="text"></form></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	forms := findBySubtype(entities, "form")
	if len(forms) == 0 {
		t.Fatal("expected at least one form entity")
	}
	found := findByKindAndName(entities, "SCOPE.Operation", "/api/login")
	if found == nil {
		t.Errorf("expected SCOPE.Operation with Name='/api/login', entities: %v", entities)
		return
	}
	if found.Language != "html" {
		t.Errorf("expected Language='html', got %q", found.Language)
	}
	if found.QualityScore < 0.5 {
		t.Errorf("expected QualityScore >= 0.5, got %f", found.QualityScore)
	}
}

func TestHTMLExtractor_Form_NoAction(t *testing.T) {
	src := `<html><body><form method="post"><input></form></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.Operation", "form")
	if found == nil {
		t.Error("expected SCOPE.Operation with Name='form' when no action attribute")
	}
}

func TestHTMLExtractor_Form_MultipleFormsAllExtracted(t *testing.T) {
	src := `<html><body>
<form action="/login"></form>
<form action="/register"></form>
<form action="/search"></form>
</body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	forms := findBySubtype(entities, "form")
	if len(forms) != 3 {
		t.Errorf("expected 3 form entities, got %d", len(forms))
	}
}

// ---------------------------------------------------------------------------
// AC2: Script include → SCOPE.Component
// ---------------------------------------------------------------------------

func TestHTMLExtractor_ScriptInclude(t *testing.T) {
	src := `<html><head><script src="/js/app.js"></script></head></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.Component", "/js/app.js")
	if found == nil {
		t.Errorf("expected SCOPE.Component with Name='/js/app.js', got entities: %v", entities)
		return
	}
	if found.Subtype != "script_include" {
		t.Errorf("expected Subtype='script_include', got %q", found.Subtype)
	}
	if found.QualityScore < 0.5 {
		t.Errorf("expected QualityScore >= 0.5, got %f", found.QualityScore)
	}
}

func TestHTMLExtractor_ScriptInline_NotExtracted(t *testing.T) {
	// Inline scripts (no src) should not produce a script_include entity.
	src := `<html><head><script>console.log("hello")</script></head></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	scripts := findBySubtype(entities, "script_include")
	if len(scripts) != 0 {
		t.Errorf("expected 0 script_include entities for inline script, got %d", len(scripts))
	}
}

func TestHTMLExtractor_MultipleScriptIncludes(t *testing.T) {
	src := `<html><head>
<script src="/js/vendor.js"></script>
<script src="/js/app.js"></script>
</head></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	scripts := findBySubtype(entities, "script_include")
	if len(scripts) != 2 {
		t.Errorf("expected 2 script_include entities, got %d", len(scripts))
	}
}

// ---------------------------------------------------------------------------
// AC2: Stylesheet link → SCOPE.Component
// ---------------------------------------------------------------------------

func TestHTMLExtractor_StylesheetInclude(t *testing.T) {
	src := `<html><head><link rel="stylesheet" href="/styles/main.css"></head></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.Component", "/styles/main.css")
	if found == nil {
		t.Errorf("expected SCOPE.Component with Name='/styles/main.css', got entities: %v", entities)
		return
	}
	if found.Subtype != "style_include" {
		t.Errorf("expected Subtype='style_include', got %q", found.Subtype)
	}
}

func TestHTMLExtractor_LinkNotStylesheet_NotExtracted(t *testing.T) {
	// <link rel="icon"> should not produce a style_include entity.
	src := `<html><head><link rel="icon" href="/favicon.ico"></head></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	styles := findBySubtype(entities, "style_include")
	if len(styles) != 0 {
		t.Errorf("expected 0 style_include for rel=icon, got %d", len(styles))
	}
}

// ---------------------------------------------------------------------------
// Mustache / template expressions
// ---------------------------------------------------------------------------

func TestHTMLExtractor_MustacheWithDot(t *testing.T) {
	src := "<html><body><span>{{ user.name }}</span></body></html>"
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.Pattern", "user.name")
	if found == nil {
		t.Errorf("expected SCOPE.Pattern with Name='user.name', got entities: %v", entities)
		return
	}
	if found.Subtype != "template_expr" {
		t.Errorf("expected Subtype='template_expr', got %q", found.Subtype)
	}
}

func TestHTMLExtractor_MustacheWithFilter(t *testing.T) {
	src := "<html><body><p>{{ greeting | uppercase }}</p></body></html>"
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.Pattern", "greeting | uppercase")
	if found == nil {
		t.Errorf("expected SCOPE.Pattern with Name='greeting | uppercase', got entities: %v", entities)
	}
}

func TestHTMLExtractor_MustacheSimpleVariable_NotExtracted(t *testing.T) {
	// Simple variable references (no dot, no filter) should be skipped.
	src := "<html><body><span>{{ simple }}</span></body></html>"
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.Pattern", "simple")
	if found != nil {
		t.Errorf("expected simple variable '{{ simple }}' to be skipped, but found entity")
	}
}

func TestHTMLExtractor_MustacheMultipleInSameText(t *testing.T) {
	src := "<html><body><div>{{ user.name }} and {{ item.value | format }}</div></body></html>"
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	exprs := findBySubtype(entities, "template_expr")
	if len(exprs) < 2 {
		t.Errorf("expected at least 2 template_expr entities, got %d", len(exprs))
	}
}

// ---------------------------------------------------------------------------
// Custom elements
// ---------------------------------------------------------------------------

func TestHTMLExtractor_CustomElement_LowercaseHyphen(t *testing.T) {
	src := `<html><body><my-component :data="items"></my-component></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.Component", "my-component")
	if found == nil {
		t.Errorf("expected SCOPE.Component with Name='my-component', got entities: %v", entities)
		return
	}
	if found.Subtype != "component" {
		t.Errorf("expected Subtype='component', got %q", found.Subtype)
	}
}

func TestHTMLExtractor_CustomElement_PascalCaseHyphen(t *testing.T) {
	src := `<html><body><CustomWidget-v2 class="main"></CustomWidget-v2></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.Component", "CustomWidget-v2")
	if found == nil {
		t.Errorf("expected SCOPE.Component with Name='CustomWidget-v2', got entities: %v", entities)
		return
	}
	if found.Subtype != "component" {
		t.Errorf("expected Subtype='component', got %q", found.Subtype)
	}
}

func TestHTMLExtractor_StandardTag_NotCustomElement(t *testing.T) {
	// Standard HTML tags like <div>, <span> should NOT produce component entities.
	src := `<html><body><div class="container"><span>text</span></div></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	comps := findBySubtype(entities, "component")
	if len(comps) != 0 {
		t.Errorf("expected 0 component entities for standard tags, got %d: %v", len(comps), comps)
	}
}

// ---------------------------------------------------------------------------
// Vue directives
// ---------------------------------------------------------------------------

func TestHTMLExtractor_VueDirective_VPrefix(t *testing.T) {
	src := `<html><body><div v-if="show" v-for="item in items"></div></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	directives := findBySubtype(entities, "directive")
	if len(directives) < 2 {
		t.Errorf("expected at least 2 directive entities, got %d", len(directives))
	}
	found := findByKindAndName(entities, "SCOPE.Pattern", "v-if")
	if found == nil {
		t.Error("expected SCOPE.Pattern with Name='v-if'")
	}
}

func TestHTMLExtractor_VueDirective_AtPrefix(t *testing.T) {
	src := `<html><body><button @click="handleClick">Click</button></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.Pattern", "@click")
	if found == nil {
		t.Errorf("expected SCOPE.Pattern with Name='@click', got entities: %v", entities)
		return
	}
	if found.Subtype != "directive" {
		t.Errorf("expected Subtype='directive', got %q", found.Subtype)
	}
}

// ---------------------------------------------------------------------------
// Angular directives
// ---------------------------------------------------------------------------

func TestHTMLExtractor_AngularDirective(t *testing.T) {
	src := `<html><body><p ng-model="user.email">Hello</p></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.Pattern", "ng-model")
	if found == nil {
		t.Errorf("expected SCOPE.Pattern with Name='ng-model', got entities: %v", entities)
		return
	}
	if found.Subtype != "directive" {
		t.Errorf("expected Subtype='directive', got %q", found.Subtype)
	}
}

func TestHTMLExtractor_AngularMultipleDirectives(t *testing.T) {
	src := `<html><body><section ng-controller="MainCtrl" ng-class="{ active: isActive }"></section></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	ngDirectives := 0
	for _, e := range entities {
		if e.Subtype == "directive" && strings.HasPrefix(e.Name, "ng-") {
			ngDirectives++
		}
	}
	if ngDirectives < 2 {
		t.Errorf("expected at least 2 ng- directive entities, got %d", ngDirectives)
	}
}

// ---------------------------------------------------------------------------
// Quality score
// ---------------------------------------------------------------------------

func TestHTMLExtractor_AllEntitiesHaveQualityScore(t *testing.T) {
	src := `<!DOCTYPE html>
<html>
<head>
  <link rel="stylesheet" href="/styles/main.css">
  <script src="/js/app.js"></script>
</head>
<body>
  <form action="/login"></form>
  <my-widget></my-widget>
  <div v-if="show">{{ user.name }}</div>
  <p ng-model="email"></p>
</body>
</html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	if len(entities) == 0 {
		t.Fatal("expected entities from full sample, got none")
	}
	for _, e := range entities {
		if e.QualityScore < 0.5 {
			t.Errorf("entity %q (kind=%s subtype=%s) has QualityScore=%f < 0.5",
				e.Name, e.Kind, e.Subtype, e.QualityScore)
		}
		if e.Language != "html" {
			t.Errorf("entity %q has Language=%q, expected 'html'", e.Name, e.Language)
		}
	}
}

// ---------------------------------------------------------------------------
// AC3: Registration — extractor registered under "html"
// ---------------------------------------------------------------------------

func TestHTMLExtractor_InvokedByPipeline(t *testing.T) {
	ext, ok := extractor.Get("html")
	if !ok {
		t.Fatal("html extractor not found in registry")
	}
	src := `<html><body><form action="/test"></form></body></html>`
	tree := parseHTML(t, src)
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "pipeline.html",
		Content:  []byte(src),
		Language: "html",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	forms := findBySubtype(entities, "form")
	if len(forms) == 0 {
		t.Error("expected pipeline to produce form entities via 'html' registered extractor")
	}
}

// ---------------------------------------------------------------------------
// Line numbers
// ---------------------------------------------------------------------------

func TestHTMLExtractor_FormLineNumbers(t *testing.T) {
	src := "<!DOCTYPE html>\n<html>\n<body>\n<form action=\"/submit\"></form>\n</body>\n</html>"
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.Operation", "/submit")
	if found == nil {
		t.Fatal("expected form entity with name='/submit'")
	}
	if found.StartLine < 1 {
		t.Errorf("expected StartLine >= 1, got %d", found.StartLine)
	}
	if found.EndLine < found.StartLine {
		t.Errorf("expected EndLine >= StartLine, got EndLine=%d StartLine=%d", found.EndLine, found.StartLine)
	}
}

// ---------------------------------------------------------------------------
// Parse-error resilience: malformed HTML — never panic
// ---------------------------------------------------------------------------

func TestHTMLExtractor_MalformedHTML_NoPanic(t *testing.T) {
	malformed := `<html><body><form action="/x"><div <unclosed></body></html>`
	tree := parseHTML(t, malformed)
	// Should not panic, error nodes skipped gracefully.
	entities := extractEntities(t, "test.html", malformed, tree)
	_ = entities
}

// ---------------------------------------------------------------------------
// Inline parse (no pre-parsed tree) — extractor parses inline when Tree==nil
// ---------------------------------------------------------------------------

func TestHTMLExtractor_WithPreParsedTree(t *testing.T) {
	// When Tree is pre-supplied, extractor reuses it without re-parsing.
	src := `<html><body><form action="/pre-parsed"></form></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "pre-parsed.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.Operation", "/pre-parsed")
	if found == nil {
		t.Errorf("expected form entity from pre-parsed tree, entities: %v", entities)
	}
}

// ---------------------------------------------------------------------------
// Jinja2 directive extraction
// ---------------------------------------------------------------------------

func TestHTMLExtractor_Jinja2_Block(t *testing.T) {
	src := "{% block content %}Hello{% endblock %}"
	tree := parseHTML(t, src)
	entities := extractEntities(t, "tmpl.html", src, tree)

	found := findBySubtype(entities, "jinja_directive")
	if len(found) == 0 {
		t.Fatalf("expected jinja_directive entities, got none. all entities: %v", entities)
	}
	var names []string
	for _, e := range found {
		names = append(names, e.Name)
	}
	// Should have block:content but NOT endblock (closing tags suppressed).
	hasBlock := false
	hasEndblock := false
	for _, n := range names {
		if strings.HasPrefix(n, "block") {
			hasBlock = true
		}
		if strings.HasPrefix(n, "endblock") {
			hasEndblock = true
		}
	}
	if !hasBlock {
		t.Errorf("expected block directive, names: %v", names)
	}
	if hasEndblock {
		t.Errorf("endblock closing directive should be suppressed, names: %v", names)
	}
}

func TestHTMLExtractor_Jinja2_Extends(t *testing.T) {
	src := `{% extends "base.html" %}`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "tmpl.html", src, tree)

	found := findBySubtype(entities, "jinja_directive")
	if len(found) == 0 {
		t.Fatal("expected jinja_directive for extends")
	}
	if !strings.HasPrefix(found[0].Name, "extends") {
		t.Errorf("expected name starting with 'extends', got %q", found[0].Name)
	}
	if found[0].Kind != "SCOPE.Component" {
		t.Errorf("expected Kind=SCOPE.Component, got %q", found[0].Kind)
	}
}

func TestHTMLExtractor_Jinja2_Include(t *testing.T) {
	src := `{% include "partials/header.html" %}`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "tmpl.html", src, tree)

	found := findBySubtype(entities, "jinja_directive")
	if len(found) == 0 {
		t.Fatal("expected jinja_directive for include")
	}
	if !strings.HasPrefix(found[0].Name, "include") {
		t.Errorf("expected name starting with 'include', got %q", found[0].Name)
	}
}

func TestHTMLExtractor_Jinja2_Macro(t *testing.T) {
	src := `{% macro render_field(field) %}{{ field.label }}{% endmacro %}`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "tmpl.html", src, tree)

	found := findBySubtype(entities, "jinja_directive")
	hasMacro := false
	hasEndmacro := false
	for _, e := range found {
		if strings.HasPrefix(e.Name, "macro") {
			hasMacro = true
		}
		if strings.HasPrefix(e.Name, "endmacro") {
			hasEndmacro = true
		}
	}
	if !hasMacro {
		t.Errorf("expected macro directive, entities: %v", found)
	}
	if hasEndmacro {
		t.Errorf("endmacro should be suppressed, entities: %v", found)
	}
}

func TestHTMLExtractor_Jinja2_ForIf_OpenOnly(t *testing.T) {
	src := "{% for item in items %}{{ item.name }}{% endfor %}\n{% if user.logged_in %}Hi{% endif %}"
	tree := parseHTML(t, src)
	entities := extractEntities(t, "tmpl.html", src, tree)

	directives := findBySubtype(entities, "jinja_directive")
	hasFor := false
	hasIf := false
	hasEndfor := false
	hasEndif := false
	for _, e := range directives {
		switch {
		case strings.HasPrefix(e.Name, "for"):
			hasFor = true
		case strings.HasPrefix(e.Name, "if"):
			hasIf = true
		case strings.HasPrefix(e.Name, "endfor"):
			hasEndfor = true
		case strings.HasPrefix(e.Name, "endif"):
			hasEndif = true
		}
	}
	if !hasFor {
		t.Error("expected for directive")
	}
	if !hasIf {
		t.Error("expected if directive")
	}
	if hasEndfor {
		t.Error("endfor should be suppressed")
	}
	if hasEndif {
		t.Error("endif should be suppressed")
	}
}

func TestHTMLExtractor_Jinja2_NoDirectives_NoEntities(t *testing.T) {
	src := `<html><body><p>No jinja here</p></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "plain.html", src, tree)

	directives := findBySubtype(entities, "jinja_directive")
	if len(directives) != 0 {
		t.Errorf("expected 0 jinja_directive entities for plain HTML, got %d", len(directives))
	}
}

func TestHTMLExtractor_Jinja2_DirectiveLineNumbers(t *testing.T) {
	src := "line1\n{% block title %}Title{% endblock %}\nline3"
	tree := parseHTML(t, src)
	entities := extractEntities(t, "tmpl.html", src, tree)

	directives := findBySubtype(entities, "jinja_directive")
	if len(directives) == 0 {
		t.Fatal("expected jinja_directive entities")
	}
	for _, d := range directives {
		if d.StartLine != 2 {
			t.Errorf("expected StartLine=2 for directive on line 2, got %d", d.StartLine)
		}
	}
}

// ---------------------------------------------------------------------------
// Form field child extraction
// ---------------------------------------------------------------------------

func TestHTMLExtractor_FormField_Input(t *testing.T) {
	src := `<html><body><form action="/login"><input type="text" name="username"></form></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	fields := findBySubtype(entities, "form_field")
	if len(fields) == 0 {
		t.Fatalf("expected form_field entities, got none. all: %v", entities)
	}
	found := findByKindAndName(entities, "SCOPE.UIComponent", "username")
	if found == nil {
		t.Errorf("expected SCOPE.UIComponent with Name='username', entities: %v", entities)
		return
	}
	if found.Subtype != "form_field" {
		t.Errorf("expected Subtype='form_field', got %q", found.Subtype)
	}
	if found.QualityScore < 0.5 {
		t.Errorf("expected QualityScore >= 0.5, got %f", found.QualityScore)
	}
}

func TestHTMLExtractor_FormField_Select(t *testing.T) {
	src := `<html><body><form><select name="role"><option>User</option></select></form></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.UIComponent", "role")
	if found == nil {
		t.Errorf("expected SCOPE.UIComponent for <select name='role'>, entities: %v", entities)
	}
}

func TestHTMLExtractor_FormField_Textarea(t *testing.T) {
	src := `<html><body><form><textarea name="bio"></textarea></form></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.UIComponent", "bio")
	if found == nil {
		t.Errorf("expected SCOPE.UIComponent for <textarea name='bio'>, entities: %v", entities)
	}
}

func TestHTMLExtractor_FormField_Button(t *testing.T) {
	src := `<html><body><form><button type="submit" name="submit">Go</button></form></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.UIComponent", "submit")
	if found == nil {
		t.Errorf("expected SCOPE.UIComponent for <button name='submit'>, entities: %v", entities)
	}
}

func TestHTMLExtractor_FormField_MultipleFields(t *testing.T) {
	src := `<html><body><form action="/register">
  <input type="text" name="username">
  <input type="email" name="email">
  <input type="password" name="password">
  <button type="submit" name="submit">Register</button>
</form></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	fields := findBySubtype(entities, "form_field")
	if len(fields) < 4 {
		t.Errorf("expected at least 4 form_field entities, got %d: %v", len(fields), fields)
	}
}

func TestHTMLExtractor_FormField_FallbackToID(t *testing.T) {
	// When name attr is absent, fall back to id attribute.
	src := `<html><body><form><input type="text" id="myfield"></form></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.UIComponent", "myfield")
	if found == nil {
		t.Errorf("expected SCOPE.UIComponent with Name='myfield' (fallback to id), entities: %v", entities)
	}
}

func TestHTMLExtractor_FormField_FallbackToTagName(t *testing.T) {
	// When neither name nor id is present, fall back to tag name.
	src := `<html><body><form><button>Submit</button></form></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	found := findByKindAndName(entities, "SCOPE.UIComponent", "button")
	if found == nil {
		t.Errorf("expected SCOPE.UIComponent with Name='button' (fallback to tag), entities: %v", entities)
	}
}

func TestHTMLExtractor_FormField_NonFormInputNotExtracted(t *testing.T) {
	// <input> outside a <form> should NOT produce form_field entities.
	src := `<html><body><div><input type="search" name="q"></div></body></html>`
	tree := parseHTML(t, src)
	entities := extractEntities(t, "test.html", src, tree)

	fields := findBySubtype(entities, "form_field")
	if len(fields) != 0 {
		t.Errorf("expected 0 form_field entities for <input> outside <form>, got %d: %v", len(fields), fields)
	}
}

// ---------------------------------------------------------------------------
// Flask Jinja2 register fixture — >=10 entities end-to-end
// ---------------------------------------------------------------------------

func TestHTMLExtractor_FlaskJinja2Register_MinEntityCount(t *testing.T) {
	src := `{% extends "base.html" %}
{% block title %}Register{% endblock %}
{% block content %}
{% include "partials/flash_messages.html" %}
{% macro render_field(field) %}
  <div class="field">{{ field.label }}{{ field() }}</div>
{% endmacro %}
<form action="/register" method="post">
  <input type="text" name="username" id="username">
  <input type="email" name="email" id="email">
  <input type="password" name="password" id="password">
  <select name="role" id="role"><option value="user">User</option></select>
  <textarea name="bio" id="bio"></textarea>
  <button type="submit" name="submit">Register</button>
</form>
{% endblock %}`

	tree := parseHTML(t, src)
	entities := extractEntities(t, "flask_jinja2_register.html", src, tree)

	if len(entities) < 10 {
		t.Errorf("expected >=10 entities from Jinja2 fixture, got %d: %v", len(entities), entities)
	}

	// Verify distribution: jinja_directive, form, form_field all present.
	jinjaDirectives := findBySubtype(entities, "jinja_directive")
	if len(jinjaDirectives) == 0 {
		t.Error("expected jinja_directive entities from fixture")
	}
	forms := findBySubtype(entities, "form")
	if len(forms) == 0 {
		t.Error("expected form entity from fixture")
	}
	formFields := findBySubtype(entities, "form_field")
	if len(formFields) < 4 {
		t.Errorf("expected >=4 form_field entities, got %d", len(formFields))
	}
}

func TestHTMLExtractor_FlaskJinja2Register_AllKindsAllowlistCompliant(t *testing.T) {
	// Every entity Kind emitted by the Jinja2 fixture must be in the 14-type allowlist.
	allowlist := map[string]struct{}{
		"SCOPE.Service":       {},
		"SCOPE.Component":     {},
		"SCOPE.Operation":     {},
		"SCOPE.Pattern":       {},
		"SCOPE.Evolution":     {},
		"SCOPE.Datastore":     {},
		"SCOPE.ExternalAPI":   {},
		"SCOPE.Event":         {},
		"SCOPE.Queue":         {},
		"SCOPE.Schema":        {},
		"SCOPE.ScopeUnknown":  {},
		"SCOPE.Stylesheet":    {},
		"SCOPE.UIComponent":   {},
		"SCOPE.InfraResource": {},
	}

	src := `{% extends "base.html" %}
{% block content %}
<form action="/register" method="post">
  <input type="text" name="username">
  <select name="role"><option>User</option></select>
</form>
{% endblock %}`

	tree := parseHTML(t, src)
	entities := extractEntities(t, "flask_jinja2_register.html", src, tree)

	for _, e := range entities {
		if _, ok := allowlist[e.Kind]; !ok {
			t.Errorf("entity %q has Kind=%q which is not in the graph 14-type allowlist", e.Name, e.Kind)
		}
	}
}

// ---------------------------------------------------------------------------
// Issue #506 — HTML email-template + external-URL noise suppression
// ---------------------------------------------------------------------------

func TestHTMLExtractor_SkipsEmailTemplateFiles(t *testing.T) {
	src := `<!DOCTYPE html><html><body>
<img src="https://controller.client-private.com/img/controller-logo-400px.png">
<a href="https://example.com/reset">Click here</a>
</body></html>`

	cases := []string{
		"src/templates/new_user_login_email.html",
		"src/templates/reset_password_email.html",
		"app/templates/welcome_email.html",
		"emails/order_confirmation.html",
		"email/notification.html",
		"src/templates/account_template.html",
		"some/path/footer_template.html",
		"my.email.html",
	}

	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			tree := parseHTML(t, src)
			entities := extractEntities(t, path, src, tree)
			if len(entities) != 0 {
				t.Fatalf("expected 0 entities for email-template path %q, got %d", path, len(entities))
			}
		})
	}
}

func TestHTMLExtractor_ExtractsNonEmailTemplateHTML(t *testing.T) {
	// Negative control: index.html and pages/*.html must still be extracted.
	src := `<!DOCTYPE html><html><body>
<script src="/src/main.jsx" type="module"></script>
<img src="/logo.png">
</body></html>`

	cases := []string{
		"index.html",
		"public/index.html",
		"src/pages/about.html",
		"docs/contact.html",
	}

	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			tree := parseHTML(t, src)
			entities := extractEntities(t, path, src, tree)
			if len(entities) == 0 {
				t.Fatalf("expected entities for non-email path %q, got 0", path)
			}
		})
	}
}

func TestHTMLExtractor_SkipsExternalURLsInImgScriptLink(t *testing.T) {
	// External URLs (CDN scripts, hotlinked images, external stylesheets)
	// must not produce entities. They cannot resolve to a local repo file
	// and only create bug-extractor `to_id` noise.
	src := `<!DOCTYPE html><html>
<head>
  <link rel="stylesheet" href="https://cdn.example.com/style.css">
  <link rel="stylesheet" href="//cdn.example.com/proto.css">
  <script src="https://cdn.example.com/lib.js"></script>
  <script src="//cdn.example.com/proto.js"></script>
</head>
<body>
  <img src="https://controller.client-private.com/img/logo.png">
  <img src="data:image/png;base64,AAAA">
</body>
</html>`

	tree := parseHTML(t, src)
	entities := extractEntities(t, "index.html", src, tree)

	for _, e := range entities {
		if strings.HasPrefix(e.Name, "http://") ||
			strings.HasPrefix(e.Name, "https://") ||
			strings.HasPrefix(e.Name, "//") ||
			strings.HasPrefix(e.Name, "data:") {
			t.Errorf("external URL leaked into entity: name=%q kind=%q subtype=%q",
				e.Name, e.Kind, e.Subtype)
		}
	}
}

func TestHTMLExtractor_KeepsLocalImgScriptLink(t *testing.T) {
	// Positive control: local refs (relative + root-absolute) must still
	// produce entities.
	src := `<!DOCTYPE html><html>
<head>
  <link rel="stylesheet" href="/css/site.css">
  <link rel="stylesheet" href="./styles/app.css">
  <script src="/src/main.jsx" type="module"></script>
  <script src="js/app.js"></script>
</head>
<body>
  <img src="/logo.png">
  <img src="../assets/banner.svg">
</body>
</html>`

	tree := parseHTML(t, src)
	entities := extractEntities(t, "index.html", src, tree)

	subtypes := map[string]int{}
	for _, e := range entities {
		subtypes[e.Subtype]++
	}
	if subtypes["style_include"] < 2 {
		t.Errorf("expected >=2 style_include entities, got %d", subtypes["style_include"])
	}
	if subtypes["script_include"] < 2 {
		t.Errorf("expected >=2 script_include entities, got %d", subtypes["script_include"])
	}
	if subtypes["image_include"] < 2 {
		t.Errorf("expected >=2 image_include entities, got %d", subtypes["image_include"])
	}
}
