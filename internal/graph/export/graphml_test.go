package export

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// sampleDoc builds a tiny in-memory graph that exercises special characters
// requiring escaping in both formats.
func sampleDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{
				ID:         "n1",
				Name:       `Order<Service> & "Co"`,
				Kind:       "Class",
				SourceFile: "src/order's.go",
				StartLine:  12,
			},
			{
				ID:         "n2",
				Name:       "placeOrder",
				Kind:       "method-impl", // non-identifier char -> sanitized for Cypher
				SourceFile: "src/order.go",
				StartLine:  20,
			},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "n2", ToID: "n1", Kind: "calls"},
		},
	}
}

func TestWriteGraphML_ValidXMLStructure(t *testing.T) {
	var sb strings.Builder
	if err := WriteGraphML(&sb, sampleDoc()); err != nil {
		t.Fatalf("WriteGraphML: %v", err)
	}
	out := sb.String()

	// Must be well-formed XML and parse into the GraphML shape.
	type data struct {
		Key   string `xml:"key,attr"`
		Value string `xml:",chardata"`
	}
	type node struct {
		ID   string `xml:"id,attr"`
		Data []data `xml:"data"`
	}
	type edge struct {
		ID     string `xml:"id,attr"`
		Source string `xml:"source,attr"`
		Target string `xml:"target,attr"`
		Data   []data `xml:"data"`
	}
	type gml struct {
		XMLName xml.Name `xml:"graphml"`
		Nodes   []node   `xml:"graph>node"`
		Edges   []edge   `xml:"graph>edge"`
	}
	var parsed gml
	if err := xml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not well-formed GraphML: %v\n%s", err, out)
	}

	if len(parsed.Nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(parsed.Nodes))
	}
	if len(parsed.Edges) != 1 {
		t.Fatalf("want 1 edge, got %d", len(parsed.Edges))
	}

	// Verify the first node's escaped attributes round-tripped to their
	// original literal values after XML decoding.
	n1 := parsed.Nodes[0]
	if n1.ID != "n1" {
		t.Errorf("node id = %q, want n1", n1.ID)
	}
	got := map[string]string{}
	for _, d := range n1.Data {
		got[d.Key] = d.Value
	}
	if got["name"] != `Order<Service> & "Co"` {
		t.Errorf("name data = %q, want literal with < & \"", got["name"])
	}
	if got["file"] != "src/order's.go" {
		t.Errorf("file data = %q", got["file"])
	}
	if got["line"] != "12" {
		t.Errorf("line data = %q, want 12", got["line"])
	}

	// Edge endpoints + kind.
	e := parsed.Edges[0]
	if e.Source != "n2" || e.Target != "n1" {
		t.Errorf("edge endpoints = %s->%s, want n2->n1", e.Source, e.Target)
	}
	if len(e.Data) != 1 || e.Data[0].Value != "calls" {
		t.Errorf("edge data = %+v, want kind=calls", e.Data)
	}

	// Raw escaping must be present in the serialized form (not just after decode).
	if !strings.Contains(out, "&lt;Service&gt;") {
		t.Errorf("raw output missing escaped < >:\n%s", out)
	}
	if !strings.Contains(out, "&amp;") {
		t.Errorf("raw output missing escaped &")
	}
	if !strings.Contains(out, "&apos;") {
		t.Errorf("raw output missing escaped apostrophe")
	}
}

func TestWriteGraphML_HeaderAndKeys(t *testing.T) {
	var sb strings.Builder
	if err := WriteGraphML(&sb, &graph.Document{}); err != nil {
		t.Fatalf("WriteGraphML empty: %v", err)
	}
	out := sb.String()
	if !strings.HasPrefix(out, `<?xml version="1.0"`) {
		t.Errorf("missing XML declaration")
	}
	if !strings.Contains(out, `xmlns="http://graphml.graphdrawing.org/xmlns"`) {
		t.Errorf("missing GraphML namespace")
	}
	for _, want := range []string{
		`<key id="name" for="node"`,
		`<key id="kind" for="node"`,
		`<key id="file" for="node"`,
		`<key id="line" for="node"`,
		`<key id="ekind" for="edge"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing key declaration %q", want)
		}
	}
}

func TestXMLEscape(t *testing.T) {
	cases := map[string]string{
		"plain": "plain",
		"a<b>c": "a&lt;b&gt;c",
		"a&b":   "a&amp;b",
		`"q"`:   "&quot;q&quot;",
		"it's":  "it&apos;s",
		"":      "",
	}
	for in, want := range cases {
		if got := xmlEscape(in); got != want {
			t.Errorf("xmlEscape(%q) = %q, want %q", in, got, want)
		}
	}
}
