package markdown

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func loadFixture(t *testing.T, name string) extractor.FileInput {
	t.Helper()
	p := filepath.Join("testdata", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return extractor.FileInput{
		Path:     name,
		Content:  b,
		Language: "markdown",
	}
}

func extract(t *testing.T, name string) []types.EntityRecord {
	t.Helper()
	e := &Extractor{}
	out, err := e.Extract(context.Background(), loadFixture(t, name))
	if err != nil {
		t.Fatalf("extract %q: %v", name, err)
	}
	return out
}

func countByKind(ents []types.EntityRecord, kind string) int {
	n := 0
	for _, e := range ents {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func findByQName(ents []types.EntityRecord, qname string) *types.EntityRecord {
	for i := range ents {
		if ents[i].QualifiedName == qname {
			return &ents[i]
		}
	}
	return nil
}

func hasContains(rels []types.RelationshipRecord, toQ string) bool {
	for _, r := range rels {
		if r.Kind == "CONTAINS" && r.ToID == toQ {
			return true
		}
	}
	return false
}

func TestSlugify_Deterministic(t *testing.T) {
	cases := []struct {
		in   string
		line int
		want string
	}{
		{"OrderViewSet (deprecated)", 1, "orderviewset_deprecated"},
		{"runIndex", 1, "runindex"},
		{"Hello, World!", 1, "hello_world"},
		{"   ", 5, "heading_5"},
		{"", 7, "heading_7"},
		{"already_snake", 1, "already_snake"},
	}
	for _, tc := range cases {
		got := slugify(tc.in, tc.line)
		if got != tc.want {
			t.Errorf("slugify(%q, %d) = %q, want %q", tc.in, tc.line, got, tc.want)
		}
		// Determinism: re-run.
		if again := slugify(tc.in, tc.line); again != got {
			t.Errorf("slugify not deterministic for %q", tc.in)
		}
	}
}

func TestSimple(t *testing.T) {
	ents := extract(t, "simple.md")
	// 1 doc + 2 headings + 1 code block.
	if got, want := len(ents), 4; got != want {
		t.Fatalf("entity count: got %d, want %d (entities=%+v)", got, want, ents)
	}
	if countByKind(ents, "SCOPE.Document") != 1 {
		t.Errorf("expected 1 Document")
	}
	if countByKind(ents, "SCOPE.Heading") != 2 {
		t.Errorf("expected 2 Headings")
	}
	if countByKind(ents, "SCOPE.CodeBlock") != 1 {
		t.Errorf("expected 1 CodeBlock")
	}

	doc := findByQName(ents, "simple.md")
	if doc == nil {
		t.Fatalf("doc not found")
	}
	// Doc CONTAINS each heading.
	if !hasContains(doc.Relationships, "simple.md::title") {
		t.Errorf("doc missing CONTAINS->title")
	}
	if !hasContains(doc.Relationships, "simple.md::section") {
		t.Errorf("doc missing CONTAINS->section")
	}

	// H1 CONTAINS H2.
	h1 := findByQName(ents, "simple.md::title")
	if h1 == nil {
		t.Fatalf("h1 not found")
	}
	if !hasContains(h1.Relationships, "simple.md::section") {
		t.Errorf("h1 should contain h2 'section'")
	}

	// H2 CONTAINS code block.
	h2 := findByQName(ents, "simple.md::section")
	if h2 == nil {
		t.Fatalf("h2 not found")
	}
	codeQ := "simple.md::block::L9"
	if !hasContains(h2.Relationships, codeQ) {
		t.Errorf("h2 should contain code block %q; rels=%+v", codeQ, h2.Relationships)
	}

	// Code block subtype is python.
	cb := findByQName(ents, codeQ)
	if cb == nil {
		t.Fatalf("code block not found")
	}
	if cb.Subtype != "python" {
		t.Errorf("code block subtype = %q, want python", cb.Subtype)
	}
}

func TestNested(t *testing.T) {
	ents := extract(t, "nested.md")
	// 1 doc + 4 headings (Top, Middle, Deep, Sibling) + 1 code block.
	if got, want := len(ents), 6; got != want {
		t.Fatalf("entity count: got %d, want %d", got, want)
	}

	top := findByQName(ents, "nested.md::top")
	mid := findByQName(ents, "nested.md::middle")
	deep := findByQName(ents, "nested.md::deep")
	sib := findByQName(ents, "nested.md::sibling")
	if top == nil || mid == nil || deep == nil || sib == nil {
		t.Fatalf("expected all four headings present")
	}

	// Top contains middle and sibling (both H2).
	if !hasContains(top.Relationships, "nested.md::middle") {
		t.Errorf("top should contain middle")
	}
	if !hasContains(top.Relationships, "nested.md::sibling") {
		t.Errorf("top should contain sibling")
	}
	// Top must NOT directly contain deep (H3) — that's middle's child.
	if hasContains(top.Relationships, "nested.md::deep") {
		t.Errorf("top should NOT directly contain deep (h3)")
	}
	// Middle contains deep.
	if !hasContains(mid.Relationships, "nested.md::deep") {
		t.Errorf("middle should contain deep")
	}
	// Sibling has no children.
	for _, r := range sib.Relationships {
		if r.Kind == "CONTAINS" {
			t.Errorf("sibling should not contain anything; got %+v", r)
		}
	}

	// Deep CONTAINS the code block.
	if len(deep.Relationships) == 0 {
		t.Errorf("deep should contain code block")
	}
	found := false
	for _, r := range deep.Relationships {
		if r.Kind == "CONTAINS" && r.ToID == "nested.md::block::L13" {
			found = true
		}
	}
	if !found {
		t.Errorf("deep should CONTAIN code block at L13; got %+v", deep.Relationships)
	}
}

func TestSetextSkipped(t *testing.T) {
	ents := extract(t, "setext_skipped.md")
	// Only one ATX heading should be picked up.
	if c := countByKind(ents, "SCOPE.Heading"); c != 1 {
		t.Errorf("setext should be skipped; got %d headings, want 1", c)
	}
	// And it's the ATX one.
	h := findByQName(ents, "setext_skipped.md::real_atx_h1")
	if h == nil {
		t.Errorf("ATX heading not found")
	}
}

func TestEmptyHeadings_BackticksSlugCorrectly(t *testing.T) {
	ents := extract(t, "empty_headings.md")
	// Two headings: `OrderViewSet` and `runIndex` (deprecated).
	h1 := findByQName(ents, "empty_headings.md::orderviewset")
	if h1 == nil {
		t.Errorf("expected slug 'orderviewset' for `OrderViewSet`")
	}
	h2 := findByQName(ents, "empty_headings.md::runindex_deprecated")
	if h2 == nil {
		t.Errorf("expected slug 'runindex_deprecated' for `runIndex` (deprecated)")
	}

	// REFERENCES edges should be present on both.
	if h1 != nil {
		hasRef := false
		for _, r := range h1.Relationships {
			if r.Kind == "REFERENCES" && r.ToID == "orderviewset" {
				hasRef = true
			}
		}
		if !hasRef {
			t.Errorf("h1 missing REFERENCES->orderviewset; rels=%+v", h1.Relationships)
		}
	}
	if h2 != nil {
		hasRef := false
		for _, r := range h2.Relationships {
			if r.Kind == "REFERENCES" && r.ToID == "runindex" {
				hasRef = true
			}
		}
		if !hasRef {
			t.Errorf("h2 missing REFERENCES->runindex; rels=%+v", h2.Relationships)
		}
	}
}

func TestCodeWithLang(t *testing.T) {
	ents := extract(t, "code_with_lang.md")
	if c := countByKind(ents, "SCOPE.CodeBlock"); c != 3 {
		t.Errorf("want 3 code blocks, got %d", c)
	}
	subtypes := map[string]bool{}
	for _, e := range ents {
		if e.Kind == "SCOPE.CodeBlock" {
			subtypes[e.Subtype] = true
		}
	}
	for _, want := range []string{"python", "json", "unspecified"} {
		if !subtypes[want] {
			t.Errorf("missing code block subtype %q (got %v)", want, subtypes)
		}
	}
}

func TestEmptyContent(t *testing.T) {
	e := &Extractor{}
	out, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.md",
		Content:  []byte{},
		Language: "markdown",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("empty content should produce 0 entities, got %d", len(out))
	}
}

// hasImports checks whether ents contains a SCOPE.Component import-stub
// entity carrying an IMPORTS edge from filePath → toID.
func hasImports(ents []types.EntityRecord, filePath, toID string) bool {
	for _, e := range ents {
		if e.Kind != "SCOPE.Component" || e.Subtype != "import" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" && r.FromID == filePath && r.ToID == toID {
				return true
			}
		}
	}
	return false
}

func countImports(ents []types.EntityRecord) int {
	n := 0
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				n++
			}
		}
	}
	return n
}

func TestImports_RelativeLinksEmitImports(t *testing.T) {
	ents := extract(t, "links.md")

	// Expected: sibling.md, ../parent.md, root.md, after.md
	// Link in fenced code block should be skipped.
	// External (https), mailto, and in-page (#) links should NOT emit IMPORTS.
	wantTargets := []string{"sibling.md", "../parent.md", "root.md", "after.md"}
	for _, tgt := range wantTargets {
		if !hasImports(ents, "links.md", tgt) {
			t.Errorf("missing IMPORTS edge to %q", tgt)
		}
	}

	// Negative: links inside fenced code blocks must not emit.
	if hasImports(ents, "links.md", "should-be-ignored.md") {
		t.Errorf("link inside fenced code block must not emit IMPORTS")
	}

	// Negative: external / mailto / in-page must not emit.
	for _, bad := range []string{"https://example.com", "mailto:foo@bar.com", "#links-doc", "links.md"} {
		if hasImports(ents, "links.md", bad) {
			t.Errorf("unexpected IMPORTS edge to %q", bad)
		}
	}

	// Dedup: duplicate `./sibling.md` only counted once. Total = 4.
	if got := countImports(ents); got != 4 {
		t.Errorf("IMPORTS count = %d, want 4", got)
	}
}

func TestImports_ResolveTargetClassification(t *testing.T) {
	cases := []struct {
		name    string
		dir     string
		raw     string
		wantOK  bool
		wantOut string
	}{
		{"relative dot-slash", "docs", "./foo.md", true, "docs/foo.md"},
		{"relative bare", "docs", "foo.md", true, "docs/foo.md"},
		{"relative parent", "docs", "../README.md", true, "README.md"},
		{"absolute path", "docs", "/root.md", true, "root.md"},
		{"strip fragment", "docs", "./foo.md#sec", true, "docs/foo.md"},
		{"strip query", "docs", "./foo.md?x=1", true, "docs/foo.md"},
		{"empty dir bare", "", "foo.md", true, "foo.md"},
		{"http", "docs", "https://example.com", false, ""},
		{"http no slashes", "docs", "http://example.com", false, ""},
		{"mailto", "docs", "mailto:a@b.com", false, ""},
		{"in-page anchor", "docs", "#section", false, ""},
		{"protocol-relative", "docs", "//cdn/x.js", false, ""},
		{"empty", "docs", "", false, ""},
		{"only fragment after strip", "docs", "#", false, ""},
	}
	for _, tc := range cases {
		got, ok := resolveImportTarget(tc.dir, tc.raw)
		if ok != tc.wantOK {
			t.Errorf("%s: ok = %v, want %v", tc.name, ok, tc.wantOK)
			continue
		}
		if ok && got != tc.wantOut {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.wantOut)
		}
	}
}

func TestImports_NoLinksProducesNoStubs(t *testing.T) {
	ents := extract(t, "simple.md")
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "import" {
			t.Errorf("simple.md should produce no import stubs; got %+v", e)
		}
	}
}

func TestRegistration(t *testing.T) {
	if e, ok := extractor.Get("markdown"); !ok {
		t.Errorf("markdown extractor not registered")
	} else if e.Language() != "markdown" {
		t.Errorf("extractor.Language() = %q, want markdown", e.Language())
	}
}
