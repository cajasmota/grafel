// branchconditions.go — Issue #2885.
//
// The discriminator pass (discriminator.go, #2654/#2666) only captures the
// narrow "bare-identifier ===/==/!== primitive-literal" shape. Real-world
// NativeScript-Core view-models (plain TS classes extending Observable) branch
// on MEMBER comparisons with the full set of relational operators, e.g.
//
//	if (this._x !== value) { ... }      // member LHS, not a bare identifier
//	if (this._counter <= 0) { ... }     // relational operator, no literal RHS match
//	const cls = this._mode ? 'a' : 'b'; // ternary
//	switch (this._state) { ... }        // switch
//
// None of those are discriminator-shaped, so the discriminator pass stamped 0
// hits across 19 real @nativescript/app-templates view-models. This pass emits
// a general `branch_condition` signal for `if`/ternary/`switch` controlling
// expressions in a method/function body, capturing those member comparisons.
//
// Output:
//   - Properties["branch_conditions"] : comma-separated normalised condition
//     expressions (e.g. "this._x!==value,this._counter<=0").
//   - one BRANCHES_ON edge (#2885) per unique condition to a synthetic
//     "branch:<expr>" stub, with line/operator/kind properties.
//
// The walk is tolerant: a panic is recovered so the primary pipeline is
// unaffected.
package javascript

import (
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// branchKind classifies the syntactic construct a branch condition came from.
type branchKind string

const (
	branchIf      branchKind = "if"
	branchTernary branchKind = "ternary"
	branchSwitch  branchKind = "switch"
)

// comparisonOps is the set of relational/equality operators that mark an
// expression as a genuine branch condition (as opposed to e.g. a bare boolean
// flag `if (this.enabled)`, which carries no comparison and would only add
// noise). This is deliberately a superset of the discriminator operators
// (which are ===/==/!== only) so member-comparison branches are captured.
var comparisonOps = map[string]bool{
	"===": true, "!==": true, "==": true, "!=": true,
	"<": true, "<=": true, ">": true, ">=": true,
}

// branchHit captures one branch-condition site: the normalised condition text,
// the comparison operator, the construct kind, and the 1-indexed source line.
type branchHit struct {
	expr     string
	operator string
	kind     branchKind
	line     int
}

// extractBranchHits walks a function/method/arrow body and returns one
// branchHit per unique condition expression found in an if/ternary/switch
// whose controlling expression contains a comparison operator.
func (x *extractor) extractBranchHits(body ts.Node) []branchHit {
	if body == nil {
		return nil
	}
	var hits []branchHit
	seen := make(map[string]bool)

	defer func() { _ = recover() }()

	add := func(cond ts.Node, kind branchKind) {
		if cond == nil {
			return
		}
		op, ok := x.branchComparisonOp(cond)
		if !ok {
			// A `switch`/ternary on a bare member or identifier (e.g.
			// `switch (this._status)`, `this._busy ? a : b`) is still a real
			// branch discriminant even without a comparison operator. Capture
			// it with an empty operator. `if` still requires a comparison to
			// avoid noise from bare boolean flags like `if (this.enabled)`.
			if (kind == branchSwitch || kind == branchTernary) && isBranchOperand(cond) {
				op = ""
			} else {
				return
			}
		}
		expr := normalizeBranchExpr(x.nodeText(cond))
		if expr == "" || seen[expr] {
			return
		}
		seen[expr] = true
		hits = append(hits, branchHit{
			expr:     expr,
			operator: op,
			kind:     kind,
			line:     int(cond.StartPoint().Row) + 1,
		})
	}

	for _, n := range findAllNodes(body, "if_statement", "ternary_expression", "switch_statement") {
		switch n.Type() {
		case "if_statement":
			add(unwrapParen(n.ChildByFieldName("condition")), branchIf)
		case "ternary_expression":
			add(unwrapParen(n.ChildByFieldName("condition")), branchTernary)
		case "switch_statement":
			add(unwrapParen(n.ChildByFieldName("value")), branchSwitch)
		}
	}
	return hits
}

// branchComparisonOp returns the comparison operator of a (possibly nested)
// condition expression and whether the condition qualifies as a branch
// comparison. It handles binary comparisons directly, and descends through
// logical && / || compositions so `if (a < b && c !== d)` is recognised by its
// first comparison operand. A bare boolean (`if (this.enabled)`) returns false.
func (x *extractor) branchComparisonOp(n ts.Node) (string, bool) {
	if n == nil {
		return "", false
	}
	switch n.Type() {
	case "parenthesized_expression":
		return x.branchComparisonOp(unwrapParen(n))
	case "binary_expression":
		op := x.binaryOperator(n)
		if comparisonOps[op] {
			return op, true
		}
		// Logical composition (&&, ||): recurse into operands.
		if op == "&&" || op == "||" {
			if o, ok := x.branchComparisonOp(n.ChildByFieldName("left")); ok {
				return o, true
			}
			if o, ok := x.branchComparisonOp(n.ChildByFieldName("right")); ok {
				return o, true
			}
		}
	case "unary_expression":
		// `if (!this._ready)` — negation of a flag is not a comparison.
		return "", false
	}
	return "", false
}

// isBranchOperand reports whether a switch/ternary controlling expression is a
// member access or plain identifier worth recording as a branch discriminant
// (e.g. `this._status`, `mode`). Calls, literals and complex expressions are
// excluded to keep the signal focused on stateful branching.
func isBranchOperand(n ts.Node) bool {
	if n == nil {
		return false
	}
	switch n.Type() {
	case "member_expression", "identifier", "subscript_expression":
		return true
	}
	return false
}

// binaryOperator returns the operator token of a binary_expression node.
func (x *extractor) binaryOperator(n ts.Node) string {
	if n == nil {
		return ""
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "===", "!==", "==", "!=", "<", "<=", ">", ">=", "&&", "||":
			return ch.Type()
		}
	}
	return ""
}

// unwrapParen strips a single layer of parenthesized_expression so callers see
// the inner comparison directly.
func unwrapParen(n ts.Node) ts.Node {
	if n != nil && n.Type() == "parenthesized_expression" {
		// the inner expression is the named child
		for i := 0; i < int(n.ChildCount()); i++ {
			ch := n.Child(i)
			if ch != nil && ch.IsNamed() {
				return ch
			}
		}
	}
	return n
}

// normalizeBranchExpr collapses internal whitespace/newlines so equivalent
// conditions deduplicate cleanly (e.g. "this._x !== value" → "this._x!==value").
func normalizeBranchExpr(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// stampBranchConditions stamps Properties["branch_conditions"] on the last
// entity appended to x.entities and emits BRANCHES_ON edges (#2885). Called
// immediately after stampDiscriminators for each emitted method/function/arrow.
func (x *extractor) stampBranchConditions(body ts.Node) {
	if body == nil || len(x.entities) == 0 {
		return
	}
	hits := x.extractBranchHits(body)
	if len(hits) == 0 {
		return
	}
	last := &x.entities[len(x.entities)-1]
	if last.Properties == nil {
		last.Properties = make(map[string]string)
	}
	exprs := make([]string, 0, len(hits))
	for _, h := range hits {
		exprs = append(exprs, h.expr)
	}
	last.Properties["branch_conditions"] = strings.Join(exprs, ",")
	for _, h := range hits {
		last.Relationships = append(last.Relationships, types.RelationshipRecord{
			ToID: "branch:" + h.expr,
			Kind: string(types.RelationshipKindBranchesOn),
			Properties: map[string]string{
				"line":     strconv.Itoa(h.line),
				"operator": h.operator,
				"kind":     string(h.kind),
			},
		})
	}
}
