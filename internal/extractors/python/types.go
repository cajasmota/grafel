// Python Type System extraction (issue #2989).
//
// Mirrors the TypeScript type-extraction precedent (issue #1343) so the two
// stacks emit comparable SCOPE.Schema entities and cross-stack type-drift
// detection (Python TypedDict ↔ TypeScript interface) can join on the same
// kind/subtype taxonomy.
//
// The pass is append-and-annotate only — it never removes or re-keys entities
// emitted by the primary walk:
//
//   - class X(Protocol):            → stamp the existing class entity with
//     pattern_type="protocol" + protocol_methods
//     (capability: interface_extraction)
//   - class X(Enum/IntEnum/StrEnum/ → stamp pattern_type="enum" + enum_members
//     Flag/IntFlag, incl. enum.Enum)  (capability: enum_extraction)
//   - class X(TypedDict):           → stamp pattern_type="typed_dict" + fields
//   - @dataclass class X / NamedTuple
//     → stamp pattern_type="dataclass"/"named_tuple"
//   - fields  (capability: type_extraction)
//   - X = Union[...] / X: TypeAlias = ...
//     / type X = ...  (PEP 695)     → emit a NEW SCOPE.Schema/type_alias entity
//     (capability: type_alias_extraction)
//
// Stamping (rather than synthesising duplicate class nodes) keeps the entity
// graph deduplicated: the primary walk already emitted exactly one
// SCOPE.Component/class per Protocol/Enum/TypedDict/dataclass; this pass only
// adds queryable Properties so archigraph_inspect can surface the type
// contract and archigraph_find can rank type names. Type aliases are the one
// shape with no pre-existing entity, so they are emitted fresh as SCOPE.Schema.
package python

import (
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// enumBaseNames are the stdlib enum base classes whose presence in a class's
// base list marks it as an enumeration (enum_extraction). Matched on the leaf
// identifier so both bare (`Enum`) and dotted (`enum.Enum`) forms qualify.
var enumBaseNames = map[string]struct{}{
	"Enum": {}, "IntEnum": {}, "StrEnum": {}, "Flag": {}, "IntFlag": {}, "ReprEnum": {},
}

// dataclassDecoratorNames are the decorator leaf names that mark a class as a
// dataclass (type_extraction). attrs (`@attr.s` / `@define`) and pydantic are
// intentionally out of scope here — they are handled by dedicated framework
// passes — so we keep this to the stdlib dataclasses decorator.
var dataclassDecoratorNames = map[string]struct{}{
	"dataclass": {},
}

// applyTypeSystemAnnotations is the entry point wired into Extract. It scans
// every top-level (and nested) class_definition / decorated class for a
// Type System shape and stamps pattern_type + structural properties on the
// matching SCOPE.Component/class entity, and emits a fresh SCOPE.Schema entity
// for every module-level type-alias assignment.
//
// Runs after the primary walk so the class entities it annotates already
// exist in *out. Append-only for type aliases; in-place stamp for classes.
func applyTypeSystemAnnotations(root *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	if root == nil {
		return
	}

	// Pass 1 — annotate class-based type constructs in place.
	for _, cls := range findAll(root, "class_definition") {
		annotateTypeClass(cls, file, out)
	}

	// Pass 2 — emit type-alias entities. Both the PEP 695 `type X = ...`
	// statement and the assignment-based forms (`X = Union[...]`,
	// `X: TypeAlias = ...`) are scanned at module scope. Class-body
	// assignments are excluded so a Model field annotated with `TypeAlias`
	// (vanishingly rare, but possible) inside a class body does not surface
	// a stray module-level type alias.
	for _, ts := range findAll(root, "type_alias_statement") {
		if ent, ok := buildPEP695TypeAlias(ts, file); ok {
			*out = append(*out, ent)
		}
	}
	for _, asn := range moduleLevelAssignments(root) {
		if ent, ok := buildAssignmentTypeAlias(asn, file); ok {
			*out = append(*out, ent)
		}
	}
}

// annotateTypeClass inspects one class_definition node, classifies it against
// the Type System taxonomy from its base list (+ decorators), and stamps the
// matching SCOPE.Component/class entity in *out. No-op for ordinary classes.
func annotateTypeClass(cls *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	nameNode := cls.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	bareName := nodeText(nameNode, file.Content)
	if bareName == "" {
		return
	}

	bases := classBaseLeafNames(cls, file.Content)
	if len(bases) == 0 && !classHasDataclassDecorator(cls, file.Content) {
		return
	}

	var patternType string
	props := map[string]string{}

	switch {
	case baseSetContains(bases, "Protocol"):
		// interface_extraction — a structural-typing contract.
		patternType = "protocol"
		if methods := classMethodNames(cls, file.Content); len(methods) > 0 {
			props["protocol_methods"] = strings.Join(methods, ", ")
		}
	case baseSetContainsAny(bases, enumBaseNames):
		// enum_extraction.
		patternType = "enum"
		if members := classEnumMembers(cls, file.Content); len(members) > 0 {
			props["enum_members"] = strings.Join(members, ", ")
		}
	case baseSetContains(bases, "TypedDict"):
		// type_extraction — a typed dict shape.
		patternType = "typed_dict"
		if fields := classAnnotatedFields(cls, file.Content); len(fields) > 0 {
			props["typed_fields"] = strings.Join(fields, ", ")
		}
	case baseSetContains(bases, "NamedTuple"):
		// type_extraction — a named-tuple shape.
		patternType = "named_tuple"
		if fields := classAnnotatedFields(cls, file.Content); len(fields) > 0 {
			props["typed_fields"] = strings.Join(fields, ", ")
		}
	case classHasDataclassDecorator(cls, file.Content):
		// type_extraction — a @dataclass.
		patternType = "dataclass"
		if fields := classAnnotatedFields(cls, file.Content); len(fields) > 0 {
			props["typed_fields"] = strings.Join(fields, ", ")
		}
	default:
		return
	}

	if len(bases) > 0 {
		props["type_bases"] = strings.Join(bases, ", ")
	}
	props["pattern_type"] = patternType

	stampClassTypeProps(out, file.Path, bareName, props)
}

// stampClassTypeProps finds the SCOPE.Component/class entity for bareName in
// the given file and merges the type-system properties onto it. The primary
// walk qualifies nested class names with a dotted parent path, so the match is
// done on the trailing leaf of entity.Name to cover both top-level and nested
// classes. Existing keys are preserved (specialised framework stamps win).
func stampClassTypeProps(out *[]types.EntityRecord, filePath, bareName string, props map[string]string) {
	for i := range *out {
		e := &(*out)[i]
		if e.Kind != "SCOPE.Component" || e.Subtype != "class" || e.SourceFile != filePath {
			continue
		}
		leaf := e.Name
		if dot := strings.LastIndexByte(leaf, '.'); dot >= 0 {
			leaf = leaf[dot+1:]
		}
		if leaf != bareName {
			continue
		}
		if e.Properties == nil {
			e.Properties = map[string]string{}
		}
		for k, v := range props {
			if _, exists := e.Properties[k]; !exists {
				e.Properties[k] = v
			}
		}
		return
	}
}

// classBaseLeafNames returns the leaf identifier of every base class declared
// in the class_definition's argument_list. `enum.Enum` → "Enum",
// `typing.Protocol` → "Protocol", `TypedDict` → "TypedDict". Keyword arguments
// (metaclass=, total=) are skipped. Generic subscripts (`Protocol[T]`) reduce
// to the leaf of the value being subscripted.
func classBaseLeafNames(cls *sitter.Node, src []byte) []string {
	args := cls.ChildByFieldName("superclasses")
	if args == nil {
		// Grammar exposes the base list as an unnamed argument_list child.
		for i := 0; i < int(cls.NamedChildCount()); i++ {
			if c := cls.NamedChild(i); c != nil && c.Type() == "argument_list" {
				args = c
				break
			}
		}
	}
	if args == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(args.NamedChildCount()); i++ {
		arg := args.NamedChild(i)
		if arg == nil {
			continue
		}
		if leaf := baseLeafName(arg, src); leaf != "" {
			out = append(out, leaf)
		}
	}
	return out
}

// baseLeafName reduces a single base-class expression node to its leaf
// identifier. Returns "" for keyword arguments (metaclass=, total=).
func baseLeafName(arg *sitter.Node, src []byte) string {
	switch arg.Type() {
	case "identifier":
		return nodeText(arg, src)
	case "attribute":
		if a := arg.ChildByFieldName("attribute"); a != nil {
			return nodeText(a, src)
		}
	case "subscript":
		// Protocol[T], Generic[T] — leaf of the subscripted value.
		if v := arg.ChildByFieldName("value"); v != nil {
			return baseLeafName(v, src)
		}
	case "keyword_argument":
		return ""
	}
	return ""
}

// classHasDataclassDecorator reports whether the class_definition is wrapped in
// a decorated_definition whose decorator list includes @dataclass (bare or
// `@dataclasses.dataclass`, with or without a call: `@dataclass(frozen=True)`).
func classHasDataclassDecorator(cls *sitter.Node, src []byte) bool {
	parent := cls.Parent()
	if parent == nil || parent.Type() != "decorated_definition" {
		return false
	}
	for i := 0; i < int(parent.NamedChildCount()); i++ {
		dec := parent.NamedChild(i)
		if dec == nil || dec.Type() != "decorator" {
			continue
		}
		if leaf := decoratorLeafName(dec, src); leaf != "" {
			if _, ok := dataclassDecoratorNames[leaf]; ok {
				return true
			}
		}
	}
	return false
}

// decoratorLeafName extracts the leaf identifier of a decorator node, handling
// `@name`, `@mod.name`, and the call forms `@name(...)` / `@mod.name(...)`.
func decoratorLeafName(dec *sitter.Node, src []byte) string {
	// A decorator's named child is the expression after '@'.
	for i := 0; i < int(dec.NamedChildCount()); i++ {
		expr := dec.NamedChild(i)
		if expr == nil {
			continue
		}
		switch expr.Type() {
		case "identifier":
			return nodeText(expr, src)
		case "attribute":
			if a := expr.ChildByFieldName("attribute"); a != nil {
				return nodeText(a, src)
			}
		case "call":
			if fn := expr.ChildByFieldName("function"); fn != nil {
				return decoratorLeafFromExpr(fn, src)
			}
		}
	}
	return ""
}

// decoratorLeafFromExpr reduces a decorator-call function expression to its
// leaf identifier (`dataclass` from `dataclasses.dataclass(...)`).
func decoratorLeafFromExpr(fn *sitter.Node, src []byte) string {
	switch fn.Type() {
	case "identifier":
		return nodeText(fn, src)
	case "attribute":
		if a := fn.ChildByFieldName("attribute"); a != nil {
			return nodeText(a, src)
		}
	}
	return ""
}

// classMethodNames returns the names of methods (function_definition nodes)
// declared directly in the class body — used for protocol_methods.
func classMethodNames(cls *sitter.Node, src []byte) []string {
	body := cls.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(body.NamedChildCount()); i++ {
		child := body.NamedChild(i)
		if child == nil {
			continue
		}
		fn := child
		if child.Type() == "decorated_definition" {
			fn = child.ChildByFieldName("definition")
		}
		if fn != nil && fn.Type() == "function_definition" {
			if n := fn.ChildByFieldName("name"); n != nil {
				out = append(out, nodeText(n, src))
			}
		}
	}
	return out
}

// classEnumMembers returns the names of enum members declared in the class
// body (`RED = 1` → "RED", `auto()` assignments included). Dunder assignments
// and methods are skipped.
func classEnumMembers(cls *sitter.Node, src []byte) []string {
	body := cls.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(body.NamedChildCount()); i++ {
		stmt := body.NamedChild(i)
		if stmt == nil || stmt.Type() != "expression_statement" {
			continue
		}
		for j := 0; j < int(stmt.NamedChildCount()); j++ {
			asn := stmt.NamedChild(j)
			if asn == nil || asn.Type() != "assignment" {
				continue
			}
			left := asn.ChildByFieldName("left")
			if left == nil || left.Type() != "identifier" {
				continue
			}
			name := nodeText(left, src)
			if name == "" || strings.HasPrefix(name, "__") {
				continue
			}
			out = append(out, name)
		}
	}
	return out
}

// classAnnotatedFields returns the names of annotated class-body fields
// (`x: int`, `name: str = ""`) used for TypedDict / NamedTuple / dataclass
// shapes. Only annotated assignments count — a bare `x = 1` is not a field of
// a TypedDict and is skipped.
func classAnnotatedFields(cls *sitter.Node, src []byte) []string {
	body := cls.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(body.NamedChildCount()); i++ {
		stmt := body.NamedChild(i)
		if stmt == nil || stmt.Type() != "expression_statement" {
			continue
		}
		for j := 0; j < int(stmt.NamedChildCount()); j++ {
			asn := stmt.NamedChild(j)
			if asn == nil || asn.Type() != "assignment" {
				continue
			}
			// Annotated form: left has a sibling `type` field.
			if asn.ChildByFieldName("type") == nil {
				continue
			}
			left := asn.ChildByFieldName("left")
			if left == nil || left.Type() != "identifier" {
				continue
			}
			name := nodeText(left, src)
			if name == "" || strings.HasPrefix(name, "__") {
				continue
			}
			out = append(out, name)
		}
	}
	return out
}

// moduleLevelAssignments returns the assignment nodes that live directly at
// module scope (children of the root module's top-level expression_statements),
// excluding any assignment nested inside a class or function body.
func moduleLevelAssignments(root *sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	for i := 0; i < int(root.NamedChildCount()); i++ {
		stmt := root.NamedChild(i)
		if stmt == nil || stmt.Type() != "expression_statement" {
			continue
		}
		for j := 0; j < int(stmt.NamedChildCount()); j++ {
			if c := stmt.NamedChild(j); c != nil && c.Type() == "assignment" {
				out = append(out, c)
			}
		}
	}
	return out
}

// buildAssignmentTypeAlias recognises the assignment-based type-alias shapes
// and, when matched, returns a SCOPE.Schema/type_alias entity:
//
//	X: TypeAlias = <rhs>        — explicit PEP 613 annotation (highest signal)
//	X = Union[...] | Optional[...] | Dict[...] | List[...] | ...  (typing subscript)
//	X = A | B                   — PEP 604 union of bare type names (PascalCase)
//
// A plain `X = SomeValue()` or `X = 3` is NOT a type alias and returns false.
func buildAssignmentTypeAlias(asn *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	left := asn.ChildByFieldName("left")
	if left == nil || left.Type() != "identifier" {
		return types.EntityRecord{}, false
	}
	name := nodeText(left, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	right := asn.ChildByFieldName("right")
	if right == nil {
		return types.EntityRecord{}, false
	}

	annotated := false
	if ann := asn.ChildByFieldName("type"); ann != nil {
		// `X: TypeAlias = ...` — the annotation leaf must be TypeAlias.
		if typeAnnotationLeaf(ann, file.Content) == "TypeAlias" {
			annotated = true
		}
	}

	if !annotated && !rhsLooksLikeTypeExpression(right, file.Content) {
		return types.EntityRecord{}, false
	}

	return newTypeAliasEntity(name, right, asn, file, annotated), true
}

// typeAnnotationLeaf reduces an annotation node (the `type` field of an
// annotated assignment) to its leaf identifier.
func typeAnnotationLeaf(ann *sitter.Node, src []byte) string {
	target := ann
	if ann.Type() == "type" && ann.NamedChildCount() > 0 {
		target = ann.NamedChild(0)
	}
	switch target.Type() {
	case "identifier":
		return nodeText(target, src)
	case "attribute":
		if a := target.ChildByFieldName("attribute"); a != nil {
			return nodeText(a, src)
		}
	}
	return ""
}

// typingAliasHeads are the typing-module constructors that, when used as the
// head of a subscript RHS, unambiguously mark the assignment as a type alias.
var typingAliasHeads = map[string]struct{}{
	"Union": {}, "Optional": {}, "List": {}, "Dict": {}, "Tuple": {}, "Set": {},
	"FrozenSet": {}, "Sequence": {}, "Mapping": {}, "Iterable": {}, "Callable": {},
	"Literal": {}, "Annotated": {}, "Type": {}, "ClassVar": {}, "Final": {},
	"Awaitable": {}, "Coroutine": {}, "AsyncIterable": {}, "AsyncIterator": {},
	"DefaultDict": {}, "OrderedDict": {}, "Deque": {}, "Counter": {}, "ChainMap": {},
}

// rhsLooksLikeTypeExpression reports whether the right-hand side of a bare
// assignment is a type expression worth treating as a type alias. Conservative
// by design: only the typing-module subscript heads and PEP 604 unions of
// PascalCase type names qualify, so runtime-value assignments are never
// misclassified.
func rhsLooksLikeTypeExpression(rhs *sitter.Node, src []byte) bool {
	switch rhs.Type() {
	case "subscript":
		// Union[...], Dict[str, int], etc. — match the subscripted head leaf.
		if v := rhs.ChildByFieldName("value"); v != nil {
			head := baseLeafName(v, src)
			if _, ok := typingAliasHeads[head]; ok {
				return true
			}
		}
	case "binary_operator":
		// PEP 604 union: `A | B`. Require BOTH operands to be PascalCase type
		// names (or subscripts thereof) so `x = a | b` (bitwise int) is excluded.
		if op := rhs.ChildByFieldName("operator"); op != nil && nodeText(op, src) == "|" {
			l := rhs.ChildByFieldName("left")
			r := rhs.ChildByFieldName("right")
			return isTypeOperand(l, src) && isTypeOperand(r, src)
		}
	}
	return false
}

// isTypeOperand reports whether a PEP 604 union operand is a type reference:
// a PascalCase identifier, a None literal, a dotted attribute whose leaf is
// PascalCase, or a subscript over one of those.
func isTypeOperand(n *sitter.Node, src []byte) bool {
	if n == nil {
		return false
	}
	switch n.Type() {
	case "none":
		return true
	case "identifier":
		return isPascalCase(nodeText(n, src))
	case "attribute":
		if a := n.ChildByFieldName("attribute"); a != nil {
			return isPascalCase(nodeText(a, src))
		}
	case "subscript":
		if v := n.ChildByFieldName("value"); v != nil {
			return isTypeOperand(v, src)
		}
	case "binary_operator":
		// Nested union: A | B | C.
		return isTypeOperand(n.ChildByFieldName("left"), src) &&
			isTypeOperand(n.ChildByFieldName("right"), src)
	}
	return false
}

// isPascalCase reports whether s begins with an uppercase ASCII letter — a
// cheap heuristic for "this identifier names a type, not a runtime value".
func isPascalCase(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}

// buildPEP695TypeAlias builds a SCOPE.Schema/type_alias entity for a PEP 695
// `type X = ...` statement (Python 3.12+). These are unambiguous type aliases
// by grammar, so no RHS heuristic is needed.
func buildPEP695TypeAlias(ts *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	// type_alias_statement children: type (lhs name) '=' type (rhs).
	left := ts.ChildByFieldName("left")
	right := ts.ChildByFieldName("right")
	if left == nil {
		// Fallback: first `type` named child holds the alias name.
		for i := 0; i < int(ts.NamedChildCount()); i++ {
			if c := ts.NamedChild(i); c != nil && c.Type() == "type" {
				left = c
				break
			}
		}
	}
	name := typeAnnotationLeaf(left, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	return newTypeAliasEntity(name, right, ts, file, true), true
}

// newTypeAliasEntity constructs the SCOPE.Schema/type_alias EntityRecord shared
// by the PEP 695 and assignment code paths. rhs may be nil (then no type_body
// property is set). The pep613 flag records whether the alias was explicitly
// annotated / PEP 695 (vs inferred from a typing subscript heuristic).
func newTypeAliasEntity(name string, rhs, span *sitter.Node, file extractor.FileInput, explicit bool) types.EntityRecord {
	mod := filePathToModule(file.Path)
	qn := name
	if mod != "" {
		qn = mod + "." + name
	}

	props := map[string]string{
		"kind":         "SCOPE.Schema",
		"subtype":      "type_alias",
		"pattern_type": "type_alias",
	}
	if explicit {
		props["explicit"] = "true"
	}
	if rhs != nil {
		body := nodeText(rhs, file.Content)
		if body != "" && len(body) <= 512 {
			props["type_body"] = body
		}
	}

	start := int(span.StartPoint().Row) + 1
	end := int(span.EndPoint().Row) + 1
	props["line"] = strconv.Itoa(start)

	return types.EntityRecord{
		Name:               name,
		QualifiedName:      qn,
		Kind:               "SCOPE.Schema",
		Subtype:            "type_alias",
		Language:           "python",
		SourceFile:         file.Path,
		StartLine:          start,
		EndLine:            end,
		Signature:          "type " + name,
		Properties:         props,
		EnrichmentRequired: false,
	}
}

// baseSetContains reports whether bases contains the exact leaf name target.
func baseSetContains(bases []string, target string) bool {
	for _, b := range bases {
		if b == target {
			return true
		}
	}
	return false
}

// baseSetContainsAny reports whether any base leaf is a key of set.
func baseSetContainsAny(bases []string, set map[string]struct{}) bool {
	for _, b := range bases {
		if _, ok := set[b]; ok {
			return true
		}
	}
	return false
}
