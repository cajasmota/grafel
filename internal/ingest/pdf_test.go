package ingest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

func readFixture(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "repo", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return b
}

// TestParsePDF_TextLayer parses the committed text-layer fixture and asserts it
// produces the same Document/Section shape as markdown: a title, sections with
// headings derived by font-size / numbered heuristics, page metadata, and body
// text in which code symbols appear as whole tokens.
func TestParsePDF_TextLayer(t *testing.T) {
	doc, sections, err := ParsePDF("docs/design.pdf", readFixture(t, "docs/design.pdf"))
	if err != nil {
		t.Fatalf("ParsePDF error: %v", err)
	}
	if doc.RelPath != "docs/design.pdf" {
		t.Errorf("RelPath = %q", doc.RelPath)
	}
	if doc.Title == "" {
		t.Errorf("expected a derived title, got empty")
	}
	if doc.Note != "" {
		t.Errorf("text-layer PDF should have no note, got %q", doc.Note)
	}
	if len(sections) == 0 {
		t.Fatalf("expected sections from a text-layer PDF, got 0")
	}
	// Every section carries a page number (>=1) and a valid span.
	for _, s := range sections {
		if s.Page < 1 {
			t.Errorf("section %q has page %d, want >=1", s.HeadingText, s.Page)
		}
		if s.StartLine < 1 || s.EndLine < s.StartLine {
			t.Errorf("section %q bad span %d-%d", s.HeadingText, s.StartLine, s.EndLine)
		}
	}
	// A code symbol named in the PDF must survive as a whole identifier token in
	// some section body (so the MENTIONS linker can resolve it). This guards the
	// word-spacing extraction (no "OrderServiceDesign" gluing).
	if !mentionsToken(sections, "OrderService") {
		t.Errorf("expected the token OrderService in a section body; bodies = %v", bodies(sections))
	}
}

// TestParsePDF_MentionsLinkOnPDF reuses the L1 linker on PDF-derived sections and
// asserts a MENTIONS-able resolution to a known code entity — proving the PDF
// parser is a drop-in for the markdown parser in the existing pipeline.
func TestParsePDF_MentionsLinkOnPDF(t *testing.T) {
	_, sections, err := ParsePDF("docs/design.pdf", readFixture(t, "docs/design.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	code := codeEntitiesFixture("repo")
	tuples := make([]NameTuple, 0, len(code))
	for i := range code {
		e := &code[i]
		tuples = append(tuples, NameTuple{Name: e.Name, QualifiedName: e.QualifiedName, ID: e.ID, Kind: e.Kind})
	}
	idx := IndexNames(tuples)
	mentions := LinkMentions(sections, idx)

	wantOrderService := graph.EntityID("repo", string(types.EntityKindClass), "OrderService", "orders/order.go")
	var found bool
	for _, m := range mentions {
		if m.TargetID == wantOrderService {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a MENTIONS resolution to OrderService from the PDF, got %d mentions", len(mentions))
	}
}

// TestParsePDF_NoTextLayer covers a valid PDF with no extractable text (the
// scanned/image-only case): a Document with a note and ZERO sections, no error,
// no crash.
func TestParsePDF_NoTextLayer(t *testing.T) {
	doc, sections, err := ParsePDF("docs/scanned.pdf", readFixture(t, "docs/scanned.pdf"))
	if err != nil {
		t.Fatalf("no-text-layer PDF should not error, got %v", err)
	}
	if len(sections) != 0 {
		t.Errorf("expected 0 sections for a no-text-layer PDF, got %d", len(sections))
	}
	if doc.Note == "" {
		t.Errorf("expected an extraction note for a no-text-layer PDF")
	}
}

// TestParsePDF_NotAPDF covers garbage / non-PDF bytes: a clean error (so the
// caller skips + logs), never a panic.
func TestParsePDF_NotAPDF(t *testing.T) {
	_, _, err := ParsePDF("docs/notreally.pdf", []byte("this is not a pdf file at all"))
	if err == nil {
		t.Fatalf("expected an error for non-PDF input")
	}
}

// TestParsePDF_Empty covers empty input — handled as an error, no crash.
func TestParsePDF_Empty(t *testing.T) {
	_, _, err := ParsePDF("docs/empty.pdf", nil)
	if err == nil {
		t.Fatalf("expected an error for empty input")
	}
}

// TestParsePDF_Deterministic asserts identical bytes yield identical sections,
// IDs (StartLine), headings and page numbers.
func TestParsePDF_Deterministic(t *testing.T) {
	b := readFixture(t, "docs/design.pdf")
	d1, s1, err1 := ParsePDF("docs/design.pdf", b)
	d2, s2, err2 := ParsePDF("docs/design.pdf", b)
	if err1 != nil || err2 != nil {
		t.Fatalf("errors: %v %v", err1, err2)
	}
	if d1.Title != d2.Title || d1.LineCount != d2.LineCount {
		t.Fatalf("doc differs: %+v vs %+v", d1, d2)
	}
	if len(s1) != len(s2) {
		t.Fatalf("section count differs %d vs %d", len(s1), len(s2))
	}
	for i := range s1 {
		if s1[i].HeadingText != s2[i].HeadingText || s1[i].StartLine != s2[i].StartLine ||
			s1[i].Depth != s2[i].Depth || s1[i].Page != s2[i].Page || s1[i].Body != s2[i].Body {
			t.Fatalf("section %d differs:\n %+v\n %+v", i, s1[i], s2[i])
		}
	}
}

// TestIngest_PDFEndToEnd runs the full Ingest dispatch over the PDF fixture and
// asserts doc/section nodes (with language "pdf" and page properties) and a
// MENTIONS edge to a code entity — the same Result contract as markdown.
func TestIngest_PDFEndToEnd(t *testing.T) {
	repoRoot, _ := filepath.Abs(filepath.Join("testdata", "repo"))
	code := codeEntitiesFixture("repo")
	res := Ingest(repoRoot, "repo", []string{"docs/design.pdf"}, code)

	if res.Documents != 1 {
		t.Fatalf("documents = %d, want 1", res.Documents)
	}
	if res.Sections == 0 {
		t.Fatalf("expected sections from the PDF, got 0")
	}
	var docPDF, secWithPage int
	for _, e := range res.Entities {
		switch e.Kind {
		case string(types.EntityKindMarkdownDocument):
			docPDF++
			if e.Language != "pdf" {
				t.Errorf("doc Language = %q, want pdf", e.Language)
			}
		case string(types.EntityKindSection):
			if e.Language != "pdf" {
				t.Errorf("section Language = %q, want pdf", e.Language)
			}
			if e.PropGet("page") != "" {
				secWithPage++
			}
		}
	}
	if docPDF != 1 {
		t.Errorf("pdf doc nodes = %d, want 1", docPDF)
	}
	if secWithPage == 0 {
		t.Errorf("expected at least one section with a page property")
	}
	if res.Mentions == 0 {
		t.Errorf("expected at least one MENTIONS edge from the PDF")
	}
}

// TestDiscoverDocs_IncludesPDF asserts discovery now picks up PDFs alongside
// markdown and still excludes vendored dirs.
func TestDiscoverDocs_IncludesPDF(t *testing.T) {
	in := []string{
		"docs/design.pdf",
		"docs/orders.md",
		"README.markdown",
		"node_modules/dep.pdf",
		"vendor/x/y.pdf",
		"src/main.go",
	}
	got := DiscoverDocs(in)
	want := map[string]bool{"docs/design.pdf": true, "docs/orders.md": true, "README.markdown": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected discovered path %q", g)
		}
	}
}

func mentionsToken(sections []Section, tok string) bool {
	for _, s := range sections {
		for _, t := range identifierTokens(s.Body) {
			if t == tok {
				return true
			}
		}
	}
	return false
}

func bodies(sections []Section) []string {
	var out []string
	for _, s := range sections {
		out = append(out, s.Body)
	}
	return out
}
