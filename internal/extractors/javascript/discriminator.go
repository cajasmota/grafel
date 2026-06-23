// discriminator.go — Issue #2654.
//
// Extracts discriminator-pattern comparisons from JS/TS function/method bodies
// and stamps them as Properties["discriminators"] on the enclosing entity.
//
// A discriminator comparison is a BinaryExpression where:
//   - operator is ===, ==, or !== (and the symmetric reversed forms)
//   - LHS is a bare identifier (not a complex expression, not typeof)
//   - RHS is a primitive literal: string, number, true, false, null
//
// Emitted format: "var1=val1,var2=val2" (comma-separated, equals-separated pairs).
// Example: status===1 and type==='periodic' → "status=1,type=periodic"
//
// Filter rules (avoid noise):
//   - Skip when LHS is a typeof_expression
//   - Skip when RHS is another identifier (var-to-var equality, not a binding)
//   - Skip when LHS is not a plain identifier (complex expressions, member access)
//
// The function is intentionally tolerant: a panic inside the walk is recovered
// and an empty string returned so the primary extraction pipeline is unaffected.
package javascript

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// discriminatorHit captures one discriminator-pattern comparison site: the
// variable name, the literal value, and the 1-indexed source line of the
// binary expression. Used to emit DISCRIMINATES_ON edges (#2666).
type discriminatorHit struct {
	varName string
	literal string
	line    int
}

// extractDiscriminators walks the body node (a function/method/arrow body)
// and returns a comma-separated "var=value" string for every discriminator
// comparison found. Returns "" when no discriminators are found or body is nil.
//
// Signature: func (x *extractor) extractDiscriminators(body ts.Node) string
func (x *extractor) extractDiscriminators(body ts.Node) string {
	hits := x.extractDiscriminatorHits(body)
	if len(hits) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(hits))
	for _, h := range hits {
		pairs = append(pairs, fmt.Sprintf("%s=%s", h.varName, h.literal))
	}
	return strings.Join(pairs, ",")
}

// extractDiscriminatorHits walks the body and returns one discriminatorHit per
// unique (var, literal) pair found. Used by stampDiscriminators (#2666) to
// emit DISCRIMINATES_ON edges with line + literal properties.
func (x *extractor) extractDiscriminatorHits(body ts.Node) []discriminatorHit {
	if body == nil {
		return nil
	}
	var hits []discriminatorHit
	seen := make(map[string]bool)

	defer func() { _ = recover() }()

	nodes := findAllNodes(body, "binary_expression")
	for _, n := range nodes {
		varName, litVal, ok := x.discriminatorFromBinary(n)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%s=%s", varName, litVal)
		if seen[key] {
			continue
		}
		seen[key] = true
		line := int(n.StartPoint().Row) + 1
		hits = append(hits, discriminatorHit{varName: varName, literal: litVal, line: line})
	}
	return hits
}

// discriminatorFromBinary inspects a binary_expression node and returns the
// (variableName, literalValue, true) triple when the node is a discriminator
// pattern. Returns ("", "", false) for non-discriminator expressions.
//
// Detection: walks the children of the binary_expression to find:
//  1. An operator child whose Type() is "===", "==", or "!=="
//  2. A "left" field that is a plain identifier (not typeof, not member access)
//  3. A "right" field that is a primitive literal (string/number/true/false/null)
//
// Symmetric form: if LHS is a literal and RHS is an identifier (e.g. `1 === status`)
// the pair is also captured with (identifier, literal) order.
func (x *extractor) discriminatorFromBinary(n ts.Node) (varName, litVal string, ok bool) {
	if n == nil || n.Type() != "binary_expression" {
		return "", "", false
	}

	// Walk children to find the operator.
	hasDiscrimOp := false
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch == nil {
			continue
		}
		t := ch.Type()
		if t == "===" || t == "==" || t == "!==" {
			hasDiscrimOp = true
			break
		}
	}
	if !hasDiscrimOp {
		return "", "", false
	}

	left := n.ChildByFieldName("left")
	right := n.ChildByFieldName("right")
	if left == nil || right == nil {
		return "", "", false
	}

	// Normal form: identifier === literal
	if left.Type() == "identifier" && isDiscriminatorLiteral(right) {
		varName = x.nodeText(left)
		litVal = x.discriminatorLiteralValue(right)
		if varName != "" && litVal != "" {
			return varName, litVal, true
		}
	}

	// Reversed form: literal === identifier (e.g. `1 === status`)
	if right.Type() == "identifier" && isDiscriminatorLiteral(left) {
		varName = x.nodeText(right)
		litVal = x.discriminatorLiteralValue(left)
		if varName != "" && litVal != "" {
			return varName, litVal, true
		}
	}

	return "", "", false
}

// isDiscriminatorLiteral returns true when n is a primitive literal that
// qualifies as the RHS of a discriminator comparison. Accepted types:
//   - string (and template_string / template_literal)
//   - number
//   - true, false, null
//
// Excluded: typeof_expression, identifiers, call_expression, member_expression,
// template_substitution (complex template), unary_expression (e.g. -1 is not
// excluded but handled via the number branch; !x is excluded because it is not
// a literal). Note: unary_expression where operator is "-" followed by a number
// literal (negative numbers) would be missed; that is an acceptable trade-off
// since negative numeric discriminators are uncommon in real UI code.
func isDiscriminatorLiteral(n ts.Node) bool {
	if n == nil {
		return false
	}
	switch n.Type() {
	case "string", "template_string", "template_literal",
		"number",
		"true", "false",
		"null":
		return true
	}
	return false
}

// discriminatorLiteralValue extracts the display value from a literal node.
// String literals have their surrounding quotes stripped. Other literals
// return their raw text.
func (x *extractor) discriminatorLiteralValue(n ts.Node) string {
	if n == nil {
		return ""
	}
	raw := x.nodeText(n)
	if raw == "" {
		return ""
	}
	switch n.Type() {
	case "string", "template_string", "template_literal":
		// Strip surrounding quotes/backticks.
		if len(raw) >= 2 {
			first, last := raw[0], raw[len(raw)-1]
			if (first == '"' && last == '"') ||
				(first == '\'' && last == '\'') ||
				(first == '`' && last == '`') {
				return raw[1 : len(raw)-1]
			}
		}
	}
	return raw
}

// stampDiscriminators stamps Properties["discriminators"] on the last entity
// appended to x.entities when the discriminators string is non-empty.
// Called immediately after emitting a function/method/arrow entity.
func (x *extractor) stampDiscriminators(body ts.Node) {
	if body == nil || len(x.entities) == 0 {
		return
	}
	hits := x.extractDiscriminatorHits(body)
	if len(hits) == 0 {
		return
	}
	last := &x.entities[len(x.entities)-1]
	// Backward-compat property (#2659): comma-separated "var=value" pairs.
	pairs := make([]string, 0, len(hits))
	for _, h := range hits {
		pairs = append(pairs, fmt.Sprintf("%s=%s", h.varName, h.literal))
	}
	if last.Properties == nil {
		last.Properties = make(map[string]string)
	}
	last.Properties["discriminators"] = strings.Join(pairs, ",")
	// #2666 — emit DISCRIMINATES_ON edges to synthetic "var:<varName>" stubs
	// so inspect/find can surface line-precise hits without scanning the body.
	for _, h := range hits {
		last.Relationships = append(last.Relationships, types.RelationshipRecord{
			ToID: "var:" + h.varName,
			Kind: string(types.RelationshipKindDiscriminatesOn),
			Properties: map[string]string{
				"line":    strconv.Itoa(h.line),
				"literal": h.literal,
			},
		})
	}
}
