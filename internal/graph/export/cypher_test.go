package export

import (
	"strings"
	"testing"
)

func TestWriteCypher_Statements(t *testing.T) {
	var sb strings.Builder
	if err := WriteCypher(&sb, sampleDoc()); err != nil {
		t.Fatalf("WriteCypher: %v", err)
	}
	out := sb.String()
	lines := nonCommentLines(out)

	// 2 CREATE nodes + 1 MATCH/CREATE edge = 3 statement lines.
	if len(lines) != 3 {
		t.Fatalf("want 3 statements, got %d:\n%s", len(lines), out)
	}

	// Node 1: label from Kind "Class", id property, escaped single quote in file.
	if !strings.HasPrefix(lines[0], "CREATE (n:Class {id: 'n1'") {
		t.Errorf("node1 statement = %q", lines[0])
	}
	if !strings.Contains(lines[0], `file: 'src/order\'s.go'`) {
		t.Errorf("node1 missing escaped single quote in file: %q", lines[0])
	}
	if !strings.Contains(lines[0], "line: 12") {
		t.Errorf("node1 missing line: %q", lines[0])
	}

	// Node 2: Kind "method-impl" -> label sanitized to a valid identifier.
	if !strings.HasPrefix(lines[1], "CREATE (n:method_impl {id: 'n2'") {
		t.Errorf("node2 label not sanitized: %q", lines[1])
	}

	// Edge: MATCH on ids, CREATE typed rel upper-cased.
	wantEdge := "MATCH (a {id: 'n2'}), (b {id: 'n1'}) CREATE (a)-[:CALLS]->(b);"
	if lines[2] != wantEdge {
		t.Errorf("edge statement = %q, want %q", lines[2], wantEdge)
	}

	// Every statement must terminate with a semicolon.
	for i, l := range lines {
		if !strings.HasSuffix(l, ";") {
			t.Errorf("statement %d not semicolon-terminated: %q", i, l)
		}
	}
}

func TestCypherEscape(t *testing.T) {
	cases := map[string]string{
		"plain":     "plain",
		"it's":      `it\'s`,
		`a\b`:       `a\\b`,
		"line1\nl2": `line1\nl2`,
		"tab\there": `tab\there`,
		"cr\rhere":  `cr\rhere`,
	}
	for in, want := range cases {
		if got := cypherEscape(in); got != want {
			t.Errorf("cypherEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeIdent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Class", "Class"},
		{"method-impl", "method_impl"},
		{"calls.async", "calls_async"},
		{"", "Entity"},
		{"123", "_123"},
		{"---", "Entity"}, // all-underscore -> fallback
	}
	for _, c := range cases {
		if got := sanitizeIdent(c.in, "Entity"); got != c.want {
			t.Errorf("sanitizeIdent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// nonCommentLines returns non-empty, non-comment statement lines.
func nonCommentLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "//") {
			continue
		}
		out = append(out, l)
	}
	return out
}
