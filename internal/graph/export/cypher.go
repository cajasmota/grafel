package export

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/cajasmota/archigraph/internal/graph"
)

// WriteCypher streams doc as a Neo4j Cypher script to w. Each entity becomes a
// node created with a label derived from its Kind and an `id` property carrying
// the stable graph id; each relationship becomes a MATCH...CREATE pair keyed on
// those ids:
//
//	CREATE (n:Function {id: '...', name: '...', kind: '...', file: '...', line: 12});
//	...
//	MATCH (a {id: '...'}), (b {id: '...'}) CREATE (a)-[:CALLS]->(b);
//
// Node creation is emitted first so every MATCH that follows can resolve its
// endpoints. String values are single-quote-escaped; labels and relationship
// types are sanitized to valid Cypher identifiers (Neo4j does not accept
// arbitrary characters in unescaped labels/types). The writer is buffered and
// flushed before returning.
func WriteCypher(w io.Writer, doc *graph.Document) error {
	bw := bufio.NewWriter(w)

	if _, err := io.WriteString(bw,
		"// archigraph static graph export (Cypher)\n"); err != nil {
		return err
	}

	// Nodes.
	for i := range doc.Entities {
		e := &doc.Entities[i]
		label := cypherLabel(e.Kind)
		if _, err := fmt.Fprintf(bw,
			"CREATE (n:%s {id: '%s', name: '%s', kind: '%s', file: '%s', line: %d});\n",
			label,
			cypherEscape(e.ID),
			cypherEscape(e.Name),
			cypherEscape(e.Kind),
			cypherEscape(e.SourceFile),
			e.StartLine,
		); err != nil {
			return err
		}
	}

	// Relationships.
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		relType := cypherRelType(r.Kind)
		if _, err := fmt.Fprintf(bw,
			"MATCH (a {id: '%s'}), (b {id: '%s'}) CREATE (a)-[:%s]->(b);\n",
			cypherEscape(r.FromID),
			cypherEscape(r.ToID),
			relType,
		); err != nil {
			return err
		}
	}

	return bw.Flush()
}

// cypherEscape escapes a string for use inside single-quoted Cypher string
// literals. Backslashes are doubled and single quotes are backslash-escaped;
// control characters that would break a single-line statement are escaped to
// their Cypher escape sequences.
func cypherEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// cypherLabel turns an entity Kind into a valid Cypher node label. Labels must
// start with a letter and contain only letters, digits and underscores;
// anything else is sanitized. Empty/invalid kinds fall back to "Entity".
func cypherLabel(kind string) string {
	return sanitizeIdent(kind, "Entity")
}

// cypherRelType turns a relationship Kind into a valid Cypher relationship
// type. Conventionally upper-cased. Empty/invalid kinds fall back to "RELATED".
func cypherRelType(kind string) string {
	return sanitizeIdent(strings.ToUpper(kind), "RELATED")
}

// sanitizeIdent maps an arbitrary string to a valid Cypher identifier
// (letters, digits, underscores; must not start with a digit). Invalid
// characters become underscores. If the result is empty it returns fallback.
func sanitizeIdent(s, fallback string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range s {
		switch {
		case unicode.IsLetter(r):
			b.WriteRune(r)
		case unicode.IsDigit(r):
			if i == 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" || out == strings.Repeat("_", len(out)) {
		return fallback
	}
	return out
}
