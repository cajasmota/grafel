package ingest

import (
	"testing"
)

const sampleDoc = "" +
	"# Order Service\n" +
	"\n" +
	"The OrderService coordinates checkout.\n" +
	"\n" +
	"## Placing an order\n" +
	"\n" +
	"Call placeOrder to submit. It uses validateOrder under the hood.\n" +
	"\n" +
	"```go\n" +
	"// ## not a heading inside a fence\n" +
	"func placeOrder() {}\n" +
	"```\n" +
	"\n" +
	"### Validation\n" +
	"\n" +
	"validateOrder checks the cart.\n" +
	"\n" +
	"## Refunds\n" +
	"\n" +
	"Refund flow is documented elsewhere.\n"

func TestParseDocument_HeadingsAndHierarchy(t *testing.T) {
	doc, sections := ParseDocument("docs/orders.md", []byte(sampleDoc))

	if doc.Title != "Order Service" {
		t.Fatalf("title = %q, want %q", doc.Title, "Order Service")
	}
	if len(sections) != 4 {
		t.Fatalf("got %d sections, want 4: %+v", len(sections), headings(sections))
	}

	want := []struct {
		heading string
		depth   int
		parent  int
	}{
		{"Order Service", 1, -1},
		{"Placing an order", 2, 0},
		{"Validation", 3, 1},
		{"Refunds", 2, 0},
	}
	for i, w := range want {
		s := sections[i]
		if s.HeadingText != w.heading {
			t.Errorf("section %d heading = %q, want %q", i, s.HeadingText, w.heading)
		}
		if s.Depth != w.depth {
			t.Errorf("section %d depth = %d, want %d", i, s.Depth, w.depth)
		}
		if s.ParentIndex != w.parent {
			t.Errorf("section %d parent = %d, want %d (CONTAINS hierarchy by depth)", i, s.ParentIndex, w.parent)
		}
	}
}

func TestParseDocument_LineSpansAndFenceIgnored(t *testing.T) {
	_, sections := ParseDocument("docs/orders.md", []byte(sampleDoc))

	// The "## not a heading inside a fence" line must NOT have produced a
	// section; we already asserted there are exactly 4 sections above.
	for _, s := range sections {
		if s.HeadingText == "not a heading inside a fence" {
			t.Fatalf("fenced '##' line was wrongly parsed as a heading")
		}
	}

	// Validation (### depth 3) is nested under "Placing an order" and must end
	// before "## Refunds" begins. Spot-check the span is sane and ordered.
	val := sections[2]
	if val.HeadingText != "Validation" {
		t.Fatalf("section[2] = %q, want Validation", val.HeadingText)
	}
	if val.StartLine <= 0 || val.EndLine < val.StartLine {
		t.Fatalf("Validation span invalid: start=%d end=%d", val.StartLine, val.EndLine)
	}
	refund := sections[3]
	if val.EndLine >= refund.StartLine {
		t.Fatalf("Validation (end %d) overlaps Refunds (start %d)", val.EndLine, refund.StartLine)
	}
	if got := val.Body; !containsToken(got, "validateOrder") {
		t.Errorf("Validation body missing expected token; body=%q", got)
	}
}

func TestParseDocument_CRLFAndNoHeadings(t *testing.T) {
	doc, sections := ParseDocument("x.md", []byte("just prose\r\nno headings here\r\n"))
	if len(sections) != 0 {
		t.Fatalf("expected 0 sections for heading-less doc, got %d", len(sections))
	}
	if doc.LineCount != 2 {
		t.Fatalf("LineCount = %d, want 2", doc.LineCount)
	}
}

func headings(ss []Section) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.HeadingText
	}
	return out
}

func containsToken(body, tok string) bool {
	for _, t := range identifierTokens(body) {
		if t == tok {
			return true
		}
	}
	return false
}
