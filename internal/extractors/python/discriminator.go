// discriminator.go — Issue #2654.
//
// Extracts discriminator-pattern comparisons from Python function/method bodies
// and stamps them as Properties["discriminators"] on the enclosing entity.
//
// A discriminator comparison is a comparison_operator node where:
//   - operator is == or != (not "is", "is not", "in", "not in")
//   - LHS is a bare identifier
//   - RHS is a primitive literal (integer, float, string, true, false, none)
//
// Emitted format: "var1=val1,var2=val2" (comma-separated, equals-separated pairs).
// Example: `if status == 'paid':` → "status=paid"
//
// Filter rules (avoid noise):
//   - Skip when RHS is another identifier (var-to-var equality)
//   - Skip when LHS is not a plain identifier (attribute access, subscripts, etc.)
//   - Skip reversed forms only when LHS is a literal and RHS is an identifier
//     (these are also captured with correct order)
//
// The function is tolerant: a panic inside the walk is recovered and an empty
// string returned so the primary extraction pipeline is unaffected.
package python

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// pyDiscriminatorHit captures one discriminator-pattern comparison site: the
// variable name, the literal value, and the 1-indexed source line of the
// comparison. Used to emit DISCRIMINATES_ON edges (#2666).
type pyDiscriminatorHit struct {
	varName string
	literal string
	line    int
}

// extractPythonDiscriminators walks the body node (a function/method body block)
// and returns a comma-separated "var=value" string for every discriminator
// comparison found. Returns "" when no discriminators are found or body is nil.
func extractPythonDiscriminators(body ts.Node, src []byte) string {
	hits := extractPythonDiscriminatorHits(body, src)
	if len(hits) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(hits))
	for _, h := range hits {
		pairs = append(pairs, fmt.Sprintf("%s=%s", h.varName, h.literal))
	}
	return strings.Join(pairs, ",")
}

// extractPythonDiscriminatorHits walks the body and returns one
// pyDiscriminatorHit per unique (var, literal) pair found. Used by
// stampPythonDiscriminators (#2666) to emit DISCRIMINATES_ON edges with
// line + literal properties.
func extractPythonDiscriminatorHits(body ts.Node, src []byte) []pyDiscriminatorHit {
	if body == nil {
		return nil
	}
	var hits []pyDiscriminatorHit
	seen := make(map[string]bool)

	defer func() { _ = recover() }()

	nodes := findAll(body, "comparison_operator")
	for _, n := range nodes {
		varName, litVal, ok := discriminatorFromPythonComparison(n, src)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%s=%s", varName, litVal)
		if seen[key] {
			continue
		}
		seen[key] = true
		line := int(n.StartPoint().Row) + 1
		hits = append(hits, pyDiscriminatorHit{varName: varName, literal: litVal, line: line})
	}
	return hits
}

// discriminatorFromPythonComparison inspects a comparison_operator node and
// returns (variableName, literalValue, true) when the node is a discriminator
// pattern. Returns ("", "", false) otherwise.
//
// Tree-sitter Python grammar: comparison_operator children are laid out as:
//
//	child[0] = left operand
//	child[1] = operator token (text is "==", "!=", "is", etc.)
//	child[2] = right operand
//	(child[3] = another operator, child[4] = another operand for chained comparisons)
//
// We only inspect the first comparison pair (child[0..2]).
func discriminatorFromPythonComparison(n ts.Node, src []byte) (varName, litVal string, ok bool) {
	if n == nil || n.Type() != "comparison_operator" {
		return "", "", false
	}
	if n.ChildCount() < 3 {
		return "", "", false
	}

	left := n.Child(0)
	opNode := n.Child(1)
	right := n.Child(2)

	if left == nil || opNode == nil || right == nil {
		return "", "", false
	}

	// Operator must be == or !=
	opText := nodeText(opNode, src)
	if opText != "==" && opText != "!=" {
		return "", "", false
	}

	// Normal form: identifier == literal
	if left.Type() == "identifier" && isPythonDiscriminatorLiteral(right) {
		v := nodeText(left, src)
		l := pythonLiteralValue(right, src)
		if v != "" && l != "" {
			return v, l, true
		}
	}

	// Reversed form: literal == identifier (e.g. `'paid' == status`)
	if right.Type() == "identifier" && isPythonDiscriminatorLiteral(left) {
		v := nodeText(right, src)
		l := pythonLiteralValue(left, src)
		if v != "" && l != "" {
			return v, l, true
		}
	}

	return "", "", false
}

// isPythonDiscriminatorLiteral returns true when n is a tree-sitter Python
// literal node that qualifies as the comparison operand in a discriminator.
// Accepted types (tree-sitter Python grammar node types):
//   - "integer"   — numeric integer literal
//   - "float"     — numeric float literal
//   - "string"    — string literal (single- or double-quoted, raw, etc.)
//   - "true"      — Python True keyword
//   - "false"     — Python False keyword
//   - "none"      — Python None keyword
func isPythonDiscriminatorLiteral(n ts.Node) bool {
	if n == nil {
		return false
	}
	switch n.Type() {
	case "integer", "float",
		"string",
		"true", "false",
		"none":
		return true
	}
	return false
}

// pythonLiteralValue extracts the display value from a Python literal node.
// String literals have their surrounding quotes stripped. Other literals
// return their raw text.
func pythonLiteralValue(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	raw := strings.TrimSpace(nodeText(n, src))
	if raw == "" {
		return ""
	}
	if n.Type() == "string" {
		// Strip surrounding quotes: single ('), double ("), or triple-quoted forms.
		// Handle: 'x', "x", r'x', b"x", etc. Strip any leading prefix (r, b, f, u).
		s := raw
		// Skip prefix characters (r, b, f, u, R, B, F, U) before the quote.
		for len(s) > 0 && (s[0] == 'r' || s[0] == 'R' ||
			s[0] == 'b' || s[0] == 'B' ||
			s[0] == 'f' || s[0] == 'F' ||
			s[0] == 'u' || s[0] == 'U') {
			s = s[1:]
		}
		// Triple-quoted strings.
		if strings.HasPrefix(s, `"""`) && strings.HasSuffix(s, `"""`) && len(s) >= 6 {
			return s[3 : len(s)-3]
		}
		if strings.HasPrefix(s, "'''") && strings.HasSuffix(s, "'''") && len(s) >= 6 {
			return s[3 : len(s)-3]
		}
		// Single-quoted strings.
		if len(s) >= 2 {
			first, last := s[0], s[len(s)-1]
			if (first == '"' && last == '"') ||
				(first == '\'' && last == '\'') {
				return s[1 : len(s)-1]
			}
		}
	}
	return raw
}

// stampPythonDiscriminators stamps Properties["discriminators"] on the entity
// at index idx in the out slice when discriminators are found in body.
// Called immediately after appending a function/method entity.
func stampPythonDiscriminators(body ts.Node, src []byte, out *[]types.EntityRecord, idx int) {
	if body == nil || out == nil || idx < 0 || idx >= len(*out) {
		return
	}
	hits := extractPythonDiscriminatorHits(body, src)
	if len(hits) == 0 {
		return
	}
	e := &(*out)[idx]
	pairs := make([]string, 0, len(hits))
	for _, h := range hits {
		pairs = append(pairs, fmt.Sprintf("%s=%s", h.varName, h.literal))
	}
	if e.Properties == nil {
		e.Properties = make(map[string]string)
	}
	e.Properties["discriminators"] = strings.Join(pairs, ",")
	// #2666 — emit DISCRIMINATES_ON edges to synthetic "var:<varName>" stubs
	// so inspect/find can surface line-precise hits.
	for _, h := range hits {
		e.Relationships = append(e.Relationships, types.RelationshipRecord{
			ToID: "var:" + h.varName,
			Kind: string(types.RelationshipKindDiscriminatesOn),
			Properties: map[string]string{
				"line":    strconv.Itoa(h.line),
				"literal": h.literal,
			},
		})
	}
}
