package export

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

func TestWriteSVG_WellFormedAndLabeled(t *testing.T) {
	var sb strings.Builder
	if err := WriteSVG(&sb, sampleDoc(), 0); err != nil {
		t.Fatalf("WriteSVG: %v", err)
	}
	out := sb.String()

	// Well-formed XML.
	if err := xml.Unmarshal([]byte(out), new(struct{ XMLName xml.Name })); err != nil {
		t.Fatalf("SVG is not well-formed XML: %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, `<?xml version="1.0"`) {
		t.Errorf("missing XML declaration")
	}
	if !strings.Contains(out, `<svg`) || !strings.Contains(out, `</svg>`) {
		t.Errorf("missing svg root")
	}
	// Node labels present (escaped name + kind).
	if !strings.Contains(out, "placeOrder") {
		t.Errorf("missing node label placeOrder:\n%s", out)
	}
	if !strings.Contains(out, "Class") {
		t.Errorf("missing kind label Class")
	}
	// Escaping applied to the special-char name.
	if !strings.Contains(out, "&amp;") {
		t.Errorf("special chars not XML-escaped")
	}
	// An edge line with an arrowhead marker.
	if !strings.Contains(out, "marker-end=\"url(#arrow)\"") {
		t.Errorf("missing edge with arrowhead")
	}
}

func TestWriteSVG_Deterministic(t *testing.T) {
	var a, b strings.Builder
	if err := WriteSVG(&a, sampleDoc(), 0); err != nil {
		t.Fatal(err)
	}
	if err := WriteSVG(&b, sampleDoc(), 0); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Errorf("SVG output not deterministic")
	}
}

// bigDoc returns a graph with n nodes (ids n0001.. ) and a chain of edges.
func bigDoc(n int) *graph.Document {
	d := &graph.Document{Repo: "big"}
	for i := 0; i < n; i++ {
		d.Entities = append(d.Entities, graph.Entity{
			ID:   fmt.Sprintf("n%04d", i),
			Name: fmt.Sprintf("fn%d", i),
			Kind: "Function",
		})
		if i > 0 {
			d.Relationships = append(d.Relationships, graph.Relationship{
				ID:     fmt.Sprintf("e%04d", i),
				FromID: fmt.Sprintf("n%04d", i-1),
				ToID:   fmt.Sprintf("n%04d", i),
				Kind:   "calls",
			})
		}
	}
	return d
}

func TestWriteSVG_TopNCap(t *testing.T) {
	doc := bigDoc(100)
	var sb strings.Builder
	if err := WriteSVG(&sb, doc, 10); err != nil {
		t.Fatalf("WriteSVG: %v", err)
	}
	out := sb.String()
	// Only 10 node rects should be drawn.
	if got := strings.Count(out, "<rect x="); got != 10 {
		t.Errorf("top-N cap: want 10 node rects, got %d", got)
	}
	if !strings.Contains(out, "top-N cap") {
		t.Errorf("missing top-N hidden-node notice")
	}
}

func TestComputeLayout_TopNDisabled(t *testing.T) {
	doc := bigDoc(20)
	lay := computeLayout(doc, 0)
	if len(lay.nodes) != 20 {
		t.Errorf("topN=0 should keep all nodes, got %d", len(lay.nodes))
	}
	if lay.dropped != 0 {
		t.Errorf("topN=0 should drop nothing, got %d", lay.dropped)
	}
}
