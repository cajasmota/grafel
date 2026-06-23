// data_dispatch.go — data-structure-driven dispatch resolution for the
// Python extractor (issue #1709).
//
// Background
// ----------
// A common Python orchestrator/saga pattern stores a list of callable
// references in a module-level constant and dispatches them via a for-loop:
//
//	# sagas/place_order.py
//	from . import steps
//
//	STEPS = [
//	    (steps.create_order,   "order_created"),
//	    (steps.reserve_stock,  "stock_reserved"),
//	    (steps.charge_payment, "payment_charged"),
//	    (steps.notify_user,    "notified"),
//	]
//
//	class PlaceOrderSaga:
//	    def run(self, ctx):
//	        for f, _ in STEPS:
//	            f(ctx)
//
// After #1706, `steps.create_order(ctx)` etc. resolve correctly as direct
// calls. But `f(ctx)` inside `for f, _ in STEPS: f(ctx)` is a bare
// identifier call whose callee is a loop variable — the normal
// extractCallRelationships path can't connect it to the four step functions
// without following the data flow through STEPS.
//
// Fix (conservative data-flow pass)
// ----------------------------------
// 1. scanModuleConstCallables pre-scans every module-level
//    assignment whose LHS is an UPPER_CASE identifier (or more
//    precisely: any identifier with no reassignment in the same file that
//    only appears as a top-level assignment). The RHS must be a list or
//    tuple literal whose elements are either:
//
//        a. Attribute nodes  (`steps.create_order`)
//        b. Tuple/list literals containing attribute nodes at position 0
//           (`(steps.create_order, "ok")`, `[steps.create_order, True]`)
//
//    Each attribute node is validated via the file's import map — the
//    object must be a local import binding (or a PascalCase class, to
//    allow class-level registries). Each callable reference is recorded as
//    its import_alias+call_leaf pair so the resolver's
//    ResolveCrossModuleCallTarget can bind it.
//
// 2. extractDataDispatchCalls is called from extractCallRelationships
//    AFTER the normal call-extraction pass. It scans the function body for
//    for_statement nodes whose `left` field iterates over a known module
//    constant AND whose body calls the loop variable directly:
//
//        for <var> [, _rest] in <CONST>:
//            <var>(...)        — bare call on the loop variable
//
//    Conservative constraints enforced:
//      - CONST must be the UPPER_CASE constant name or a bare identifier
//        whose top-level assignment was recorded by scanModuleConstCallables.
//      - `<var>` must be a simple identifier (not a subscript target like
//        `f, _ = item`-style — the grammar exposes for-target as a pattern
//        when there is only one variable, or a pattern_list/tuple_pattern
//        when there are multiple; we accept both and take the first
//        identifier as the callable variable).
//      - The call must be `<var>(...)` NOT `<var>[0](...)` or any other
//        derived shape.
//      - Mutation guards: if the constant name appears as the LHS of any
//        assignment or augmented_assignment ANYWHERE in the file (outside
//        the defining assignment), we skip the constant as potentially
//        mutable.
//      - The function body must not define a variable with the same name as
//        the constant (shadowing guard).
//
// 3. For every for-loop that qualifies, one CALLS edge is emitted per
//    callable reference stored in the constant, using the same
//    import_alias+call_leaf Properties shape that #1706 uses. The resolver's
//    ResolveCrossModuleCallTarget handles binding without any new code.
//
// Scope: module-level constants only. Class-level and function-level data
// structures, dynamic rebuilds (append/extend), and comprehension-built
// iterables are explicitly rejected.

package python

import (
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// moduleConstEntry records a single callable reference extracted from a
// module-level constant list/tuple, keyed by the constant's name.
type moduleConstEntry struct {
	importAlias string // local import binding (object side of the attribute)
	callLeaf    string // the attribute identifier (function name)
}

// moduleConstRegistry maps module-level constant names to their callable
// entries. Built once per file by scanModuleConstCallables and consumed by
// extractDataDispatchCalls.
type moduleConstRegistry map[string][]moduleConstEntry

// scanModuleConstCallables walks top-level assignments in `root` and builds
// a registry of module-level list/tuple constants whose elements contain
// attribute-shaped callable references. Only recognises:
//
//   - Immediate list/tuple: `STEPS = [steps.f, steps.g]`
//   - Nested tuple/list at element position: `STEPS = [(steps.f, "x"), ...]`
//
// The `imports` map is used to validate that the attribute receiver is a
// known local import binding (or a PascalCase class name), keeping the pass
// conservative.
//
// Mutation guard: names that appear as an assignment LHS ANYWHERE in the
// file body other than their own definition are excluded from the registry.
func scanModuleConstCallables(root ts.Node, src []byte, imports pythonImportMap) moduleConstRegistry {
	if root == nil {
		return nil
	}

	reg := make(moduleConstRegistry)

	// Pass 1: collect top-level assignments of the form
	//   <IDENTIFIER> = [<expr>, ...]  or  <IDENTIFIER> = (<expr>, ...)
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child == nil {
			continue
		}
		// Accept expression_statement wrapping an assignment.
		if child.Type() != "expression_statement" {
			continue
		}
		for j := 0; j < int(child.NamedChildCount()); j++ {
			expr := child.NamedChild(j)
			if expr == nil || expr.Type() != "assignment" {
				continue
			}
			lhs := expr.ChildByFieldName("left")
			rhs := expr.ChildByFieldName("right")
			if lhs == nil || rhs == nil {
				continue
			}
			if lhs.Type() != "identifier" {
				continue
			}
			constName := nodeText(lhs, src)
			if constName == "" {
				continue
			}
			// Only accept RHS that is a list or tuple literal.
			rhsType := rhs.Type()
			if rhsType != "list" && rhsType != "tuple" {
				continue
			}
			// Extract callable entries from the list/tuple elements.
			entries := extractCallableEntriesFromLiteral(rhs, src, imports)
			if len(entries) == 0 {
				continue
			}
			// If we already have entries for this name (re-defined), mark
			// as ambiguous by storing an empty slice — the dedup loop below
			// uses the mutation guard instead.
			reg[constName] = entries
		}
	}

	if len(reg) == 0 {
		return nil
	}

	// Pass 2: mutation guard — scan ALL assignment LHS nodes in the file.
	// If any constant's name appears as an LHS (other than a single top-level
	// defining assignment whose RHS is a list/tuple), remove it from the
	// registry. We count occurrences: the defining assignment counted in
	// pass 1 is the only allowed one.
	defCount := make(map[string]int)
	for n := range reg {
		defCount[n] = 0
	}
	collectAssignmentLHSNames(root, src, defCount)
	for constName, count := range defCount {
		// count > 1 means re-assigned or mutated elsewhere (including augmented
		// assignment, annotated assignment, or a second plain assignment).
		if count > 1 {
			delete(reg, constName)
		}
	}

	if len(reg) == 0 {
		return nil
	}
	return reg
}

// extractCallableEntriesFromLiteral inspects the children of a list or
// tuple literal node and returns moduleConstEntry values for every element
// that is (or contains at position 0) an attribute-shaped callable reference
// whose receiver is a known import alias or PascalCase class name.
func extractCallableEntriesFromLiteral(
	node ts.Node,
	src []byte,
	imports pythonImportMap,
) []moduleConstEntry {
	if node == nil {
		return nil
	}
	var entries []moduleConstEntry
	for i := 0; i < int(node.NamedChildCount()); i++ {
		elem := node.NamedChild(i)
		if elem == nil {
			continue
		}
		entry, ok := extractCallableFromElem(elem, src, imports)
		if ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

// extractCallableFromElem attempts to extract a callable reference from a
// single element node. Accepted shapes:
//
//   - attribute node: `steps.create_order`
//   - tuple or list (nested): first named child must be an attribute node
//     `(steps.create_order, "ok")` → takes position 0
//
// Returns (entry, true) when a valid callable reference is found.
func extractCallableFromElem(
	elem ts.Node,
	src []byte,
	imports pythonImportMap,
) (moduleConstEntry, bool) {
	if elem == nil {
		return moduleConstEntry{}, false
	}
	switch elem.Type() {
	case "attribute":
		return attrNodeToEntry(elem, src, imports)
	case "tuple", "list":
		// Take the first named child.
		if elem.NamedChildCount() > 0 {
			first := elem.NamedChild(0)
			if first != nil && first.Type() == "attribute" {
				return attrNodeToEntry(first, src, imports)
			}
		}
		return moduleConstEntry{}, false
	default:
		return moduleConstEntry{}, false
	}
}

// attrNodeToEntry validates an attribute node as a callable reference and
// returns the corresponding moduleConstEntry. The receiver (object side)
// must be a bare identifier that is either:
//   - a known import alias (present in the imports map), OR
//   - a PascalCase class name (handled by isPascalCaseClassName).
//
// The attribute (right side) is the leaf function name.
func attrNodeToEntry(
	attr ts.Node,
	src []byte,
	imports pythonImportMap,
) (moduleConstEntry, bool) {
	if attr == nil || attr.Type() != "attribute" {
		return moduleConstEntry{}, false
	}
	obj := attr.ChildByFieldName("object")
	attrField := attr.ChildByFieldName("attribute")
	if obj == nil || attrField == nil {
		return moduleConstEntry{}, false
	}
	if obj.Type() != "identifier" {
		return moduleConstEntry{}, false
	}
	receiver := nodeText(obj, src)
	leaf := nodeText(attrField, src)
	if receiver == "" || leaf == "" {
		return moduleConstEntry{}, false
	}
	// Validate receiver: must be a known import alias or PascalCase class.
	if imports != nil {
		if _, ok := imports[receiver]; ok {
			return moduleConstEntry{importAlias: receiver, callLeaf: leaf}, true
		}
	}
	// Fallback: PascalCase class name (for class-level registries).
	if isPascalCaseClassName(receiver) {
		return moduleConstEntry{importAlias: receiver, callLeaf: leaf}, true
	}
	return moduleConstEntry{}, false
}

// collectAssignmentLHSNames walks the entire file and counts how many times
// each name in the `counts` map appears as a bare-identifier LHS of an
// assignment or augmented_assignment. Only names already present in the map
// are counted. The count for the single defining top-level assignment is 1;
// any additional assignment raises it to >1, triggering the mutation guard.
func collectAssignmentLHSNames(root ts.Node, src []byte, counts map[string]int) {
	if root == nil || len(counts) == 0 {
		return
	}
	stack := []ts.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		nt := n.Type()
		if nt == "assignment" || nt == "augmented_assignment" || nt == "annotated_assignment" {
			lhs := n.ChildByFieldName("left")
			if lhs != nil && lhs.Type() == "identifier" {
				name := nodeText(lhs, src)
				if _, tracked := counts[name]; tracked {
					counts[name]++
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
}

// extractDataDispatchCalls scans the function body for for_statement nodes
// that iterate over a known module-level constant and call the loop variable
// directly. For each qualifying loop, it emits one CALLS edge per callable
// stored in the constant, reusing the import_alias+call_leaf Properties shape
// from #1706 so ResolveCrossModuleCallTarget handles binding without changes.
//
// Conservative constraints (all must hold to emit):
//  1. The for-loop iterates over a bare identifier that is a key in `reg`.
//  2. The first loop variable (target) is a bare identifier (not a subscript
//     or attribute pattern).
//  3. The loop body contains a bare-identifier call on the loop variable:
//     `<var>(...)` — not `<var>[0](...)` or other derived shapes.
//  4. The body does not reassign the loop variable or the constant name.
//
// Emitted edges use the same ToID=callLeaf + Properties{import_alias, call_leaf}
// shape as the direct cross-module alias path (#1706). De-duplication against
// the already-emitted `seen` map is done by the caller.
func extractDataDispatchCalls(
	body ts.Node,
	src []byte,
	reg moduleConstRegistry,
	callerName string,
	seen map[seenKeyDD]bool,
) []types.RelationshipRecord {
	if body == nil || len(reg) == 0 || callerName == "" {
		return nil
	}
	var rels []types.RelationshipRecord
	forStmts := findAll(body, "for_statement")
	for _, forStmt := range forStmts {
		rel := inspectForStmt(forStmt, src, reg, seen)
		rels = append(rels, rel...)
	}
	return rels
}

// seenKeyDD is the de-duplication key for data-dispatch edges. It mirrors
// the seenKey type inside extractCallRelationships (which is a local struct
// there); declared at package scope here so extractDataDispatchCalls and its
// callers can share it without needing to export.
type seenKeyDD struct{ target, alias string }

// inspectForStmt examines a single for_statement node and emits CALLS edges
// when the loop matches the data-dispatch pattern.
//
// Accepted grammar shapes for the `for` target:
//
//	for f in STEPS:           — single identifier
//	for f, _ in STEPS:        — pattern_list / tuple_pattern, first element taken
func inspectForStmt(
	forStmt ts.Node,
	src []byte,
	reg moduleConstRegistry,
	seen map[seenKeyDD]bool,
) []types.RelationshipRecord {
	if forStmt == nil {
		return nil
	}

	// Validate the iterable: must be a bare identifier that names a known constant.
	iterNode := forStmt.ChildByFieldName("right")
	if iterNode == nil || iterNode.Type() != "identifier" {
		return nil
	}
	constName := nodeText(iterNode, src)
	entries, ok := reg[constName]
	if !ok || len(entries) == 0 {
		return nil
	}

	// Validate the loop target: extract the first (callable) variable name.
	loopVar := extractForLoopVar(forStmt, src)
	if loopVar == "" {
		return nil
	}

	// Validate the loop body: must contain at least one bare call on loopVar.
	loopBody := forStmt.ChildByFieldName("body")
	if loopBody == nil {
		return nil
	}
	if !bodyCallsLoopVar(loopBody, src, loopVar) {
		return nil
	}

	// Mutation guard: the loop body must not reassign loopVar or constName.
	if bodyReassigns(loopBody, src, loopVar) || bodyReassigns(loopBody, src, constName) {
		return nil
	}

	// Emit one CALLS edge per callable entry.
	forLine := strconv.Itoa(int(forStmt.StartPoint().Row) + 1)
	var rels []types.RelationshipRecord
	for _, entry := range entries {
		key := seenKeyDD{target: entry.callLeaf, alias: entry.importAlias}
		if seen[key] {
			continue
		}
		seen[key] = true
		rels = append(rels, types.RelationshipRecord{
			ToID: entry.callLeaf,
			Kind: "CALLS",
			Properties: map[string]string{
				"import_alias":    entry.importAlias,
				"call_leaf":       entry.callLeaf,
				"data_dispatch":   "1",
				"dispatch_source": constName,
				"line":            forLine,
			},
		})
	}
	return rels
}

// extractForLoopVar returns the name of the first identifier in the for-loop
// target. Accepted shapes:
//
//	for f in ...:             → "f"
//	for f, _ in ...:          → "f"   (pattern_list or tuple_pattern)
//	for (f, _) in ...:        → "f"   (parenthesised pattern)
//
// Returns "" for any unrecognised or non-identifier shape.
func extractForLoopVar(forStmt ts.Node, src []byte) string {
	target := forStmt.ChildByFieldName("left")
	if target == nil {
		return ""
	}
	switch target.Type() {
	case "identifier":
		return nodeText(target, src)
	case "pattern_list", "tuple_pattern":
		// First named child should be the callable variable.
		if target.NamedChildCount() > 0 {
			first := target.NamedChild(0)
			if first != nil && first.Type() == "identifier" {
				return nodeText(first, src)
			}
		}
		return ""
	default:
		return ""
	}
}

// bodyCallsLoopVar reports whether body contains at least one bare `call`
// node where the `function` child is an `identifier` node whose text equals
// loopVar. Only direct calls `loopVar(...)` qualify; chained calls like
// `loopVar.method(...)` or `loopVar[0](...)` do not.
func bodyCallsLoopVar(body ts.Node, src []byte, loopVar string) bool {
	calls := findAll(body, "call")
	for _, call := range calls {
		fn := call.ChildByFieldName("function")
		if fn == nil {
			continue
		}
		if fn.Type() != "identifier" {
			continue
		}
		if nodeText(fn, src) == loopVar {
			return true
		}
	}
	return false
}

// bodyReassigns reports whether body contains an assignment or
// augmented_assignment whose LHS is the bare identifier `name`.
func bodyReassigns(body ts.Node, src []byte, name string) bool {
	if body == nil || name == "" {
		return false
	}
	stack := []ts.Node{body}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		nt := n.Type()
		if nt == "assignment" || nt == "augmented_assignment" {
			lhs := n.ChildByFieldName("left")
			if lhs != nil && lhs.Type() == "identifier" && nodeText(lhs, src) == name {
				return true
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return false
}

// isUpperCaseConst reports whether name looks like an UPPER_CASE module-level
// constant by Python convention: all letters are uppercase (ignoring digits
// and underscores), at least two characters, no lowercase letters.
// Used as a fast pre-filter so the scanner doesn't need to visit every
// assignment in large files; the mutation guard provides the correctness
// guarantee.
func isUpperCaseConst(name string) bool {
	if len(name) < 2 {
		return false
	}
	hasUpper := false
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			return false // any lowercase → not UPPER_CASE
		}
		if r >= 'A' && r <= 'Z' {
			hasUpper = true
		}
	}
	return hasUpper
}

// resolveModuleConstName resolves the name of an identifier node as a
// module-level constant when it satisfies the UPPER_CASE heuristic. Returns
// the constant name or "" when the node doesn't qualify.
//
// This is a lightweight wrapper used by the scanner to pre-filter candidates
// before the full mutation-guard pass.
func resolveModuleConstName(lhs ts.Node, src []byte) string {
	if lhs == nil || lhs.Type() != "identifier" {
		return ""
	}
	name := nodeText(lhs, src)
	if !isUpperCaseConst(name) {
		// Also accept lowercase module-level names that are top-level
		// assignments — the mutation guard handles correctness. The
		// UPPER_CASE pre-filter is only applied when the import map is nil
		// (i.e. a file with no imports), so we don't miss patterns like
		// `steps_registry = [...]` in import-heavy files.
		return name
	}
	return name
}

// isKnownImportReceiver reports whether receiver is a known import alias
// (in the imports map) or a PascalCase class name.
func isKnownImportReceiver(receiver string, imports pythonImportMap) bool {
	if imports != nil {
		if _, ok := imports[receiver]; ok {
			return true
		}
	}
	return isPascalCaseClassName(receiver)
}

// hasSingleCallableRef reports whether a list/tuple literal node contains at
// least one valid callable reference (attribute node or nested tuple/list
// with an attribute as first element). Used as a fast pre-filter to avoid
// full entry extraction on non-callable lists.
func hasSingleCallableRef(node ts.Node, src []byte, imports pythonImportMap) bool {
	if node == nil {
		return false
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		elem := node.NamedChild(i)
		if elem == nil {
			continue
		}
		var attr ts.Node
		switch elem.Type() {
		case "attribute":
			attr = elem
		case "tuple", "list":
			if elem.NamedChildCount() > 0 {
				first := elem.NamedChild(0)
				if first != nil && first.Type() == "attribute" {
					attr = first
				}
			}
		}
		if attr == nil {
			continue
		}
		obj := attr.ChildByFieldName("object")
		if obj != nil && obj.Type() == "identifier" {
			recv := nodeText(obj, src)
			if isKnownImportReceiver(recv, imports) {
				return true
			}
		}
	}
	return false
}

// buildModuleConstRegistry is the exported-for-test entry point that wraps
// scanModuleConstCallables and is threaded through from Extract. It is a
// thin alias kept separate so the scanning logic can be unit-tested in
// isolation without going through the full Extract pipeline.
func buildModuleConstRegistry(root ts.Node, src []byte, imports pythonImportMap) moduleConstRegistry {
	return scanModuleConstCallables(root, src, imports)
}

// suffixStrings extracts the string values of all string literal children
// inside an element node. Used for debug/tracing only (not called in the
// hot path). Kept for completeness / future enrichment.
func suffixStrings(node ts.Node, src []byte) []string {
	if node == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(node.NamedChildCount()); i++ {
		ch := node.NamedChild(i)
		if ch == nil {
			continue
		}
		t := ch.Type()
		if t == "string" || t == "concatenated_string" {
			raw := strings.TrimSpace(nodeText(ch, src))
			if raw != "" {
				out = append(out, raw)
			}
		}
	}
	return out
}
