// Package export provides pure, streaming serializers that turn an in-memory
// graph.Document into static interchange formats for offline analysis and
// visualization. The serializers hold no global state and never read the
// network or filesystem — callers own the io.Writer.
//
// Scope (issue #4291): GraphML (XML, GraphML 1.0 namespace), Cypher (Neo4j
// CREATE statements), a deterministic static SVG, and a self-contained,
// dependency-free HTML viewer (embeds the graph JSON + an inline SVG + a tiny
// filter script).
package export

import (
	"bufio"
	"fmt"
	"io"

	"github.com/cajasmota/archigraph/internal/graph"
)

// graphmlKeys declares the <key> elements (the GraphML attribute schema) once
// at the top of the document. Node and edge attributes are referenced by these
// ids from each <data> element.
var graphmlNodeKeys = []struct{ id, name, typ string }{
	{"name", "name", "string"},
	{"kind", "kind", "string"},
	{"file", "file", "string"},
	{"line", "line", "int"},
}

var graphmlEdgeKeys = []struct{ id, name, typ string }{
	{"ekind", "kind", "string"},
}

// WriteGraphML streams doc as a GraphML 1.0 document to w. The output is a
// standard <graphml>/<graph>/<node>/<edge> tree: each entity becomes a <node>
// carrying name/kind/file/line data, and each relationship becomes a directed
// <edge> carrying its kind. Edges whose endpoints are not present as nodes are
// still emitted (GraphML readers tolerate dangling references); callers that
// need a closed graph should filter beforehand.
//
// All attribute values are XML-escaped. The writer is buffered internally and
// flushed before returning so no giant string is materialized.
func WriteGraphML(w io.Writer, doc *graph.Document) error {
	bw := bufio.NewWriter(w)

	if _, err := io.WriteString(bw, xmlHeader); err != nil {
		return err
	}

	// Attribute key declarations.
	for _, k := range graphmlNodeKeys {
		if _, err := fmt.Fprintf(bw,
			"  <key id=%q for=\"node\" attr.name=%q attr.type=%q/>\n",
			k.id, k.name, k.typ); err != nil {
			return err
		}
	}
	for _, k := range graphmlEdgeKeys {
		if _, err := fmt.Fprintf(bw,
			"  <key id=%q for=\"edge\" attr.name=%q attr.type=%q/>\n",
			k.id, k.name, k.typ); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(bw, "  <graph id=\"G\" edgedefault=\"directed\">\n"); err != nil {
		return err
	}

	// Nodes.
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if _, err := fmt.Fprintf(bw, "    <node id=%q>\n", xmlEscape(e.ID)); err != nil {
			return err
		}
		if err := graphmlData(bw, "name", e.Name); err != nil {
			return err
		}
		if err := graphmlData(bw, "kind", e.Kind); err != nil {
			return err
		}
		if err := graphmlData(bw, "file", e.SourceFile); err != nil {
			return err
		}
		if err := graphmlData(bw, "line", fmt.Sprintf("%d", e.StartLine)); err != nil {
			return err
		}
		if _, err := io.WriteString(bw, "    </node>\n"); err != nil {
			return err
		}
	}

	// Edges.
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		id := r.ID
		if id == "" {
			id = fmt.Sprintf("e%d", i)
		}
		if _, err := fmt.Fprintf(bw,
			"    <edge id=%q source=%q target=%q>\n",
			xmlEscape(id), xmlEscape(r.FromID), xmlEscape(r.ToID)); err != nil {
			return err
		}
		if err := graphmlEdgeData(bw, "ekind", r.Kind); err != nil {
			return err
		}
		if _, err := io.WriteString(bw, "    </edge>\n"); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(bw, "  </graph>\n</graphml>\n"); err != nil {
		return err
	}
	return bw.Flush()
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
`

// graphmlData writes a node <data key=...>value</data> line.
func graphmlData(w io.Writer, key, value string) error {
	_, err := fmt.Fprintf(w, "      <data key=%q>%s</data>\n", key, xmlEscape(value))
	return err
}

// graphmlEdgeData writes an edge <data key=...>value</data> line.
func graphmlEdgeData(w io.Writer, key, value string) error {
	_, err := fmt.Fprintf(w, "      <data key=%q>%s</data>\n", key, xmlEscape(value))
	return err
}

// xmlEscape escapes the five XML predefined entities. It is deliberately
// minimal and allocation-light: it scans for any character that needs
// escaping and only builds a new string when one is found.
func xmlEscape(s string) string {
	needs := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&', '<', '>', '"', '\'':
			needs = true
		}
		if needs {
			break
		}
	}
	if !needs {
		return s
	}
	buf := make([]byte, 0, len(s)+16)
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '&':
			buf = append(buf, "&amp;"...)
		case '<':
			buf = append(buf, "&lt;"...)
		case '>':
			buf = append(buf, "&gt;"...)
		case '"':
			buf = append(buf, "&quot;"...)
		case '\'':
			buf = append(buf, "&apos;"...)
		default:
			buf = append(buf, c)
		}
	}
	return string(buf)
}
