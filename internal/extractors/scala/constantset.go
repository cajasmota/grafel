package scala

// constantset.go — value-carrying SCOPE.Enum value-set nodes for Scala
// CONSTANT COLLECTIONS and ENUMERATIONS (#4432, extends #4429 / #4420 /
// epic #4419, ref #4334).
//
// #4429 indexed const collections (Python const maps, TS const-objects, Rails
// enums, Go iota blocks, Ruby constant collections) as SCOPE.Enum value-sets.
// This file generalises the same model to the Scala source-of-truth shapes that
// act as enumerations / lookup tables but were invisible to search_entities and
// could not be diffed by a downstream cross-graph parity-audit:
//
//	// (1) a top-level / object-member const Map literal
//	val Routes = Map("home" -> "/", "admin" -> "/admin")
//
//	// (2) an object holding a GROUP of constant vals
//	object Pages {
//	  val CoreAdmin = "core-admin"
//	  val Billing   = "billing"
//	}
//
//	// (3) a Scala 3 `enum`
//	enum Color { case Red, Green, Blue }
//	enum Planet(mass: Double) { case Mercury extends Planet(1.0) ; ... }
//
//	// (4) a sealed-trait / sealed-abstract-class enumeration realised as a
//	//     set of `case object`s extending the sealed parent
//	sealed trait Status
//	case object Active   extends Status
//	case object Inactive extends Status
//
// Reuses the shared cross-language builder in internal/extractor (EnumEntity /
// EnumMember / StripLiteralQuotes) so Scala constant collections converge on the
// same node model — and the same structured members_json [{key,value,line}] —
// as every other language. NO new Kind is introduced.
//
// Honest-partial: an empty collection / enum / case-object group emits NO node.
// A member whose value is a non-literal expression (a method call, a constant
// reference) still records the member — with its source EXPRESSION TEXT as the
// value (#4432 scope) — rather than being dropped, so the key set stays complete
// for a parity-audit.

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// emitConstantSets is the single entry point invoked once per file from
// Extract. It walks the CST for the four Scala value-set shapes and appends a
// SCOPE.Enum value-set node per detected collection. It is independent of the
// structural walkNode pass (which emits Components / Operations / Schemas), so
// the two never interfere.
func emitConstantSets(root *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	if root == nil {
		return
	}
	emitConstantSetsIn(root, file, out)
}

// emitConstantSetsIn recursively handles each lexical scope. Within a scope it
// collects sealed-trait case-object enumerations (which span sibling
// declarations) once, then per-node handles enum / Map-val / object-const-group
// shapes, recursing into container bodies.
func emitConstantSetsIn(scope *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	// (4) sealed-trait / case-object enumerations are scoped: gather every
	// `case object X extends Parent` declared directly in this scope and group
	// them by Parent, so `sealed trait Status` + N `case object`s become one
	// value-set named Status.
	emitCaseObjectEnumerations(scope, file, out)

	for i := 0; i < int(scope.ChildCount()); i++ {
		child := scope.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "enum_definition":
			if rec, ok := buildEnumValueSet(child, file); ok {
				*out = append(*out, rec)
			}
		case "val_definition", "val_declaration":
			// (1) a `val X = Map(...)` collection literal.
			if rec, ok := buildValMapValueSet(child, file); ok {
				*out = append(*out, rec)
			}
		case "object_definition":
			// (2) an `object` whose body is a group of constant vals. A `case
			// object` is NOT a const group — it is an enumeration member handled
			// by emitCaseObjectEnumerations — so skip those here.
			if !isCaseObject(child) {
				if rec, ok := buildObjectConstGroupValueSet(child, file); ok {
					*out = append(*out, rec)
				}
			}
			// Recurse into the object body for nested collections (e.g. a
			// companion object holding case objects, or a nested Map val).
			if body := templateBodyOf(child); body != nil {
				emitConstantSetsIn(body, file, out)
			}
			continue
		case "class_definition", "case_class_definition", "trait_definition",
			"function_definition", "function_declaration":
			// Recurse into bodies so collections nested in any container are
			// reached.
			if body := templateBodyOf(child); body != nil {
				emitConstantSetsIn(body, file, out)
			}
			continue
		}
		// Generic recursion for wrapper / package nodes (package_clause bodies,
		// blocks) so a Map val inside a package object is reached.
		switch child.Type() {
		case "enum_definition", "val_definition", "val_declaration":
			// already handled above; do not double-recurse.
		default:
			emitConstantSetsIn(child, file, out)
		}
	}
}

// buildValMapValueSet inspects a `val X = Map(...)` definition and, when its
// right-hand side is a collection-constructor call (Map / Set / Seq / List /
// Vector / Array) carrying literal entries, returns the SCOPE.Enum value-set
// node for it. kind_hint distinguishes a Map (key→value entries) from a
// sequence (literal elements).
func buildValMapValueSet(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := extractName(node, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	rhs := valDefinitionRHS(node)
	if rhs == nil || rhs.Type() != "call_expression" {
		return types.EntityRecord{}, false
	}
	ctor := callExpressionCalleeName(rhs, file.Content)
	args := childByType(rhs, "arguments")
	if args == nil {
		return types.EntityRecord{}, false
	}

	var members []extractor.EnumMember
	var kindHint string
	switch ctor {
	case "Map", "HashMap", "TreeMap", "ListMap", "SortedMap", "LinkedHashMap":
		members = mapArgMembers(args, file.Content)
		kindHint = "scala_const_map"
	case "Set", "HashSet", "TreeSet", "Seq", "List", "Vector", "Array",
		"IndexedSeq", "SortedSet", "LinkedHashSet":
		members = seqArgMembers(args, file.Content)
		kindHint = "scala_const_seq"
	default:
		return types.EntityRecord{}, false
	}
	if len(members) == 0 {
		return types.EntityRecord{}, false
	}
	return extractor.EnumEntity(
		name, "scala", kindHint, file.Path,
		int(node.StartPoint().Row)+1, int(node.EndPoint().Row)+1, members,
	)
}

// buildObjectConstGroupValueSet aggregates the constant `val` declarations in
// an object's template_body into a single SCOPE.Enum value-set named after the
// object. Each `val NAME = <literal-or-expr>` becomes one member. A val bound
// to a collection literal is its OWN value-set (buildValMapValueSet handles it),
// so it is excluded here. Returns ok=false when the body holds no scalar const
// vals.
//
// kind_hint is "scala_const_object".
func buildObjectConstGroupValueSet(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := extractName(node, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	body := templateBodyOf(node)
	if body == nil {
		return types.EntityRecord{}, false
	}
	var members []extractor.EnumMember
	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() != "val_definition" && ch.Type() != "val_declaration" {
			continue
		}
		memberName := extractName(ch, file.Content)
		if memberName == "" {
			continue
		}
		rhs := valDefinitionRHS(ch)
		// A val bound to a collection-constructor call is its own value-set.
		if rhs != nil && rhs.Type() == "call_expression" {
			if isCollectionCtor(callExpressionCalleeName(rhs, file.Content)) {
				continue
			}
		}
		members = append(members, extractor.EnumMember{
			Name:  memberName,
			Value: scalaScalarValue(rhs, file.Content),
			Line:  int(ch.StartPoint().Row) + 1,
		})
	}
	if len(members) == 0 {
		return types.EntityRecord{}, false
	}
	return extractor.EnumEntity(
		name, "scala", "scala_const_object", file.Path,
		int(node.StartPoint().Row)+1, int(node.EndPoint().Row)+1, members,
	)
}

// buildEnumValueSet builds the value-set for a Scala 3 `enum` declaration. Each
// `case` arm (simple `case Red, Green` or extended `case Mercury extends
// Planet(...)`) becomes a member. When the arm carries constructor arguments,
// the joined argument source text is recorded as the member value so a
// parity-audit can see the per-case payload.
//
// kind_hint is "scala_enum".
func buildEnumValueSet(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := extractName(node, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	body := childByType(node, "enum_body")
	if body == nil {
		return types.EntityRecord{}, false
	}
	var members []extractor.EnumMember
	for i := 0; i < int(body.ChildCount()); i++ {
		defs := body.Child(i)
		if defs == nil || defs.Type() != "enum_case_definitions" {
			continue
		}
		for j := 0; j < int(defs.ChildCount()); j++ {
			arm := defs.Child(j)
			if arm == nil {
				continue
			}
			if arm.Type() != "simple_enum_case" && arm.Type() != "full_enum_case" {
				continue
			}
			caseName := firstIdentifier(arm, file.Content)
			if caseName == "" {
				continue
			}
			members = append(members, extractor.EnumMember{
				Name:  caseName,
				Value: enumCaseValue(arm, file.Content),
				Line:  int(arm.StartPoint().Row) + 1,
			})
		}
	}
	if len(members) == 0 {
		return types.EntityRecord{}, false
	}
	return extractor.EnumEntity(
		name, "scala", "scala_enum", file.Path,
		int(node.StartPoint().Row)+1, int(node.EndPoint().Row)+1, members,
	)
}

// emitCaseObjectEnumerations gathers `case object X extends Parent` (and `case
// class X(...) extends Parent`) declarations that appear directly in scope and
// groups them by the Parent type name. Each Parent that has at least one case
// member becomes a SCOPE.Enum value-set named after the Parent, with one member
// per case object/class. This is the idiomatic pre-Scala-3 sealed-trait
// enumeration.
//
// kind_hint is "scala_sealed_enum".
func emitCaseObjectEnumerations(scope *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	type group struct {
		members   []extractor.EnumMember
		startLine int
		endLine   int
	}
	groups := map[string]*group{}
	var order []string

	for i := 0; i < int(scope.ChildCount()); i++ {
		child := scope.Child(i)
		if child == nil {
			continue
		}
		isObj := child.Type() == "object_definition" && isCaseObject(child)
		isCls := (child.Type() == "case_class_definition") ||
			(child.Type() == "class_definition" && isCaseObject(child))
		if !isObj && !isCls {
			continue
		}
		parent := extendsTypeName(child, file.Content)
		if parent == "" {
			continue
		}
		memberName := extractName(child, file.Content)
		if memberName == "" {
			continue
		}
		g := groups[parent]
		if g == nil {
			g = &group{startLine: int(child.StartPoint().Row) + 1}
			groups[parent] = g
			order = append(order, parent)
		}
		g.members = append(g.members, extractor.EnumMember{
			Name: memberName,
			Line: int(child.StartPoint().Row) + 1,
		})
		if ln := int(child.EndPoint().Row) + 1; ln > g.endLine {
			g.endLine = ln
		}
	}

	for _, parent := range order {
		g := groups[parent]
		if g == nil || len(g.members) == 0 {
			continue
		}
		if rec, ok := extractor.EnumEntity(
			parent, "scala", "scala_sealed_enum", file.Path,
			g.startLine, g.endLine, g.members,
		); ok {
			*out = append(*out, rec)
		}
	}
}

// ---- helpers --------------------------------------------------------------

// templateBodyOf returns the template_body / enum_body / block child of a
// container declaration, or nil when it has no body.
func templateBodyOf(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}
	for _, t := range []string{"template_body", "enum_body", "block"} {
		if b := childByType(node, t); b != nil {
			return b
		}
	}
	return nil
}

// childByType returns the first direct child of node with the given type.
func childByType(node *sitter.Node, typ string) *sitter.Node {
	if node == nil {
		return nil
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch != nil && ch.Type() == typ {
			return ch
		}
	}
	return nil
}

// valDefinitionRHS returns the value node on the right of `=` in a val/var
// definition. A type ascription (`val x: T = expr`) means the value is the
// child following the `=` token, not the type_identifier; this returns the
// node after `=`.
func valDefinitionRHS(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}
	sawEq := false
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch == nil {
			continue
		}
		if sawEq {
			return ch
		}
		if ch.Type() == "=" {
			sawEq = true
		}
	}
	return nil
}

// callExpressionCalleeName returns the simple callee name of a call_expression
// (`Map(...)` → "Map"). For a qualified callee (`immutable.Map(...)`) it returns
// the trailing identifier.
func callExpressionCalleeName(call *sitter.Node, src []byte) string {
	if call == nil {
		return ""
	}
	// The callee is the first child (identifier or field_expression) preceding
	// the arguments node.
	for i := 0; i < int(call.ChildCount()); i++ {
		ch := call.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "identifier", "type_identifier":
			return nodeStr(ch, src)
		case "field_expression":
			return lastIdentifier(ch, src)
		case "arguments":
			return ""
		}
	}
	return ""
}

// isCollectionCtor reports whether name is a recognised collection constructor.
func isCollectionCtor(name string) bool {
	switch name {
	case "Map", "HashMap", "TreeMap", "ListMap", "SortedMap", "LinkedHashMap",
		"Set", "HashSet", "TreeSet", "Seq", "List", "Vector", "Array",
		"IndexedSeq", "SortedSet", "LinkedHashSet":
		return true
	}
	return false
}

// mapArgMembers builds members from a Map(...) arguments node. Each entry is an
// `infix_expression` of the form `key -> value`. Non-literal keys/values record
// their source expression text.
func mapArgMembers(args *sitter.Node, src []byte) []extractor.EnumMember {
	var members []extractor.EnumMember
	for i := 0; i < int(args.ChildCount()); i++ {
		entry := args.Child(i)
		if entry == nil {
			continue
		}
		var key, valNode *sitter.Node
		switch entry.Type() {
		case "infix_expression":
			// key -> value  (operator_identifier "->" / "→")
			named := namedChildren(entry)
			if len(named) >= 2 {
				key = named[0]
				valNode = named[len(named)-1]
			}
		case "tuple_expression", "tuple", "tuple_pattern":
			// `("key", value)` tuple form.
			named := namedChildren(entry)
			if len(named) >= 2 {
				key = named[0]
				valNode = named[1]
			}
		default:
			continue
		}
		if key == nil {
			continue
		}
		k := scalaScalarValue(key, src)
		if k == "" {
			continue
		}
		members = append(members, extractor.EnumMember{
			Name:  k,
			Value: scalaScalarValue(valNode, src),
			Line:  int(entry.StartPoint().Row) + 1,
		})
	}
	return members
}

// seqArgMembers builds members from a Seq/Set/List(...) arguments node. Each
// literal element becomes BOTH the member key and value so the value-set carries
// the literal element set.
func seqArgMembers(args *sitter.Node, src []byte) []extractor.EnumMember {
	var members []extractor.EnumMember
	for i := 0; i < int(args.ChildCount()); i++ {
		el := args.Child(i)
		if el == nil || !el.IsNamed() {
			continue
		}
		lit := scalaScalarValue(el, src)
		if lit == "" {
			continue
		}
		members = append(members, extractor.EnumMember{
			Name:  lit,
			Value: lit,
			Line:  int(el.StartPoint().Row) + 1,
		})
	}
	return members
}

// enumCaseValue returns the source text of an enum arm's constructor arguments
// (`case Mercury extends Planet(1.0)` → "1.0"), or "" for a bare arm. This lets
// a parity-audit compare per-case payloads.
func enumCaseValue(arm *sitter.Node, src []byte) string {
	ext := childByType(arm, "extends_clause")
	if ext == nil {
		return ""
	}
	args := childByType(ext, "arguments")
	if args == nil {
		return ""
	}
	// Strip the surrounding parentheses; keep the inner argument text.
	inner := nodeStr(args, src)
	inner = trimParens(inner)
	return inner
}

// scalaScalarValue returns the statically-known literal of a scalar node
// (string / number / boolean / identifier), or — for any other non-literal node
// (a method call, an interpolation) — the trimmed source EXPRESSION TEXT
// (#4432: non-literal values are recorded, not dropped).
func scalaScalarValue(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "string", "interpolated_string_expression", "character_literal":
		return extractor.StripLiteralQuotes(nodeStr(n, src))
	case "integer_literal", "floating_point_literal", "number":
		return nodeStr(n, src)
	case "boolean_literal", "true", "false", "null_literal", "null", "unit":
		return nodeStr(n, src)
	case "identifier", "type_identifier", "stable_identifier", "field_expression":
		return nodeStr(n, src)
	}
	// Non-literal expression (call, ternary, interpolation, …): keep the source
	// text so the key set stays complete and a parity-audit sees it is dynamic.
	return nodeStr(n, src)
}

// isCaseObject reports whether an object_definition / class_definition node
// carries the `case` modifier (`case object X` / `case class X`).
func isCaseObject(node *sitter.Node) bool {
	if node == nil {
		return false
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "case" {
			return true
		}
		// Some grammar versions fold `case` into a `modifiers`/`case_modifier`
		// wrapper.
		if ch.Type() == "modifiers" || ch.Type() == "case_modifier" {
			if childByType(ch, "case") != nil {
				return true
			}
		}
	}
	return false
}

// extendsTypeName returns the supertype name from a declaration's
// extends_clause (`case object Active extends Status` → "Status").
func extendsTypeName(node *sitter.Node, src []byte) string {
	ext := childByType(node, "extends_clause")
	if ext == nil {
		return ""
	}
	if t := childByType(ext, "type_identifier"); t != nil {
		return nodeStr(t, src)
	}
	// Fallback: first type-ish child.
	for i := 0; i < int(ext.ChildCount()); i++ {
		ch := ext.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "generic_type" || ch.Type() == "stable_type_identifier" {
			return lastIdentifier(ch, src)
		}
	}
	return ""
}

// firstIdentifier returns the first identifier/type_identifier text under node.
func firstIdentifier(node *sitter.Node, src []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "identifier" || ch.Type() == "type_identifier" {
			return nodeStr(ch, src)
		}
	}
	return ""
}

// lastIdentifier returns the trailing identifier of a dotted / field
// expression (`immutable.Map` → "Map").
func lastIdentifier(node *sitter.Node, src []byte) string {
	last := ""
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "identifier" || ch.Type() == "type_identifier" {
			last = nodeStr(ch, src)
		}
	}
	if last == "" {
		return nodeStr(node, src)
	}
	return last
}

// namedChildren returns the named children of a node in order.
func namedChildren(node *sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch != nil && ch.IsNamed() {
			out = append(out, ch)
		}
	}
	return out
}

// nodeStr returns the trimmed source text of a node.
func nodeStr(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	s := node.StartByte()
	e := node.EndByte()
	if int(e) > len(src) || s > e {
		return ""
	}
	return trimSpace(string(src[s:e]))
}

// trimParens removes a single matching surrounding parenthesis pair.
func trimParens(s string) string {
	s = trimSpace(s)
	if len(s) >= 2 && s[0] == '(' && s[len(s)-1] == ')' {
		return trimSpace(s[1 : len(s)-1])
	}
	return s
}

// trimSpace is a tiny local wrapper so this file does not pull strings into
// every helper signature; kept minimal and dependency-free.
func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
