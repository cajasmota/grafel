// field_validations.go — Issue #4871. Per-field VALIDATION constraints for
// Python DTO/model fields, stamped onto the SCOPE.Schema/field entity under
// Properties["validations"] (comma-joined) so the dashboard ShapeTree surfaces
// them as small constraint chips — mirroring the TS class-validator support in
// #4858 (internal/extractors/javascript/class_validator_fields.go).
//
// The dashboard side already exists: internal/dashboard/shape_tree.go reads
// Properties["validations"] (comma-split) into v2ShapeRow.Validations, and
// webui-v2/src/components/ShapeTree.tsx renders the chips. This pass only has
// to populate that property on the Python field entities.
//
// Field-membership entities (SCOPE.Schema/field, Name="<Class>.<attr>") are
// emitted by extractClassFields (#526) for both plain assignments
// (`x = Field(...)`) and PEP 526 annotated assignments
// (`x: str = Field(...)`), so this pass runs right after
// emitDRFSerializerFieldRefs and walks the same class body, matching each
// `<attr> = <value>` / `<attr>: <annotation> = <value>` declaration back to its
// field entity by leaf name.
//
// Covered Python validation forms:
//
//   - Pydantic v1/v2 Field(): max_length / min_length / gt / ge / lt / le /
//     multiple_of / max_digits / pattern / regex / max_items / min_items →
//     MaxLength:120 / Gt:0 / Pattern / … chips. `Field(...)` (required
//     ellipsis) → a `Required` marker.
//   - Annotated[T, Field(...)] — the Field(...) inside the annotation's
//     type_parameter is parsed identically.
//   - Constrained-type constructors conint/constr/confloat/condecimal/conlist/
//     conset/conbytes(gt=, max_length=, …) used as the annotation or value.
//   - Optional[T] / `T | None` annotation → an `Optional` marker.
//   - @field_validator / @validator presence on the class → a `Validated`
//     marker on every field the validator names (cheap, decorator-arg based).
//   - DRF serializers.<Field>(max_length=, min_length=, required=False,
//     allow_null=True, min_value=, max_value=, …) kwargs → constraint chips.
//   - dataclasses.field(metadata={...}) / attrs field validators are detected
//     as a `Validated` marker when an attrs `validator=`/`field(...)` carries
//     a validator.
//
// Chip text is kept terse and comma-free (Properties is comma-joined and the
// dashboard splits on ","): bounds fold their scalar value (`MaxLength:120`,
// `Gt:0`); regex/pattern and option-shaped constraints render as a bare marker
// (`Pattern`, `Optional`, `Required`, `AllowNull`). Only stamped when at least
// one constraint is found; existing Properties are preserved.

package python

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// pydanticFieldKwargChip maps a Pydantic Field()/constrained-type kwarg name to
// the chip prefix used when its value is a simple scalar (folded as
// "Prefix:value"). Kwargs absent from this map but present in
// pydanticFieldMarkerKwarg render as a bare marker instead.
var pydanticFieldKwargChip = map[string]string{
	"max_length":     "MaxLength",
	"min_length":     "MinLength",
	"gt":             "Gt",
	"ge":             "Ge",
	"lt":             "Lt",
	"le":             "Le",
	"multiple_of":    "MultipleOf",
	"max_digits":     "MaxDigits",
	"decimal_places": "DecimalPlaces",
	"max_items":      "MaxItems",
	"min_items":      "MinItems",
}

// pydanticFieldMarkerKwarg maps kwargs whose presence (regardless of value, or
// whose value is non-scalar like a regex) is recorded as a bare marker chip.
var pydanticFieldMarkerKwarg = map[string]string{
	"pattern": "Pattern",
	"regex":   "Pattern",
}

// drfFieldKwargChip maps a DRF serializer field kwarg to a scalar-folding chip
// prefix. Boolean/marker kwargs are handled by drfFieldMarkerKwarg.
var drfFieldKwargChip = map[string]string{
	"max_length": "MaxLength",
	"min_length": "MinLength",
	"max_value":  "Max",
	"min_value":  "Min",
	"max_digits": "MaxDigits",
}

// drfFieldMarkerKwarg maps a DRF boolean/marker kwarg to the chip it produces
// when set truthily (required=False → Optional, allow_null=True → AllowNull,
// read_only=True → ReadOnly, write_only=True → WriteOnly).
type drfBoolChip struct {
	whenTrue  string
	whenFalse string
}

var drfFieldBoolKwarg = map[string]drfBoolChip{
	"required":    {whenFalse: "Optional"},
	"allow_null":  {whenTrue: "AllowNull"},
	"allow_blank": {whenTrue: "AllowBlank"},
	"read_only":   {whenTrue: "ReadOnly"},
	"write_only":  {whenTrue: "WriteOnly"},
}

// pydanticConstrainedTypes are the Pydantic constrained-type constructors that,
// when used as a field annotation or value, carry constraint kwargs identical
// to Field()'s (gt=, max_length=, …).
var pydanticConstrainedTypes = map[string]struct{}{
	"conint": {}, "constr": {}, "confloat": {}, "condecimal": {},
	"conlist": {}, "conset": {}, "confrozenset": {}, "conbytes": {},
	"condate": {}, "PositiveInt": {}, "NegativeInt": {}, "PositiveFloat": {},
	"NegativeFloat": {},
}

// emitPythonFieldValidations walks the class body once, matching each field
// declaration to its SCOPE.Schema/field entity (emitted by extractClassFields)
// and stamping Properties["validations"] with the recognised constraint chips.
//
// Parameters mirror emitDRFSerializerFieldRefs: the [before, after) window
// bounds the slice region holding this class's field entities.
func emitPythonFieldValidations(
	body ts.Node,
	file extractor.FileInput,
	parentClass string,
	classIdx int,
	before, after int,
	out *[]types.EntityRecord,
) {
	if body == nil || parentClass == "" || out == nil {
		return
	}
	if classIdx < 0 || classIdx >= len(*out) {
		return
	}

	// Build attr → field-entity-index over the window (same shape as the DRF
	// pass — restricted to direct fields of this class, not nested-class names).
	fieldIdx := make(map[string]int, max(0, after-before))
	prefix := parentClass + "."
	for k := before; k < after; k++ {
		e := &(*out)[k]
		if e.Kind != "SCOPE.Schema" || e.Subtype != "field" {
			continue
		}
		if e.SourceFile != file.Path {
			continue
		}
		if !strings.HasPrefix(e.Name, prefix) {
			continue
		}
		leaf := e.Name[len(prefix):]
		if leaf == "" || strings.Contains(leaf, ".") {
			continue
		}
		fieldIdx[leaf] = k
	}
	if len(fieldIdx) == 0 {
		return
	}

	// Pass 1 — collect @field_validator/@validator field targets so plain
	// fields named by a validator get a `Validated` marker even when they
	// carry no inline Field() constraints.
	validated := collectValidatorFieldTargets(body, file.Content)

	// Pass 2 — per field declaration, derive chips and stamp.
	for i := 0; i < int(body.ChildCount()); i++ {
		stmt := body.Child(i)
		if stmt == nil || stmt.Type() != "expression_statement" {
			continue
		}
		for j := 0; j < int(stmt.NamedChildCount()); j++ {
			expr := stmt.NamedChild(j)
			if expr == nil || expr.Type() != "assignment" {
				continue
			}
			lhs := expr.ChildByFieldName("left")
			if lhs == nil || lhs.Type() != "identifier" {
				continue
			}
			attr := nodeText(lhs, file.Content)
			idx, ok := fieldIdx[attr]
			if !ok {
				continue
			}

			var chips []string
			annot := expr.ChildByFieldName("type")
			rhs := expr.ChildByFieldName("right")

			// Optional / X | None marker from the annotation.
			if annot != nil && annotationIsOptional(annot, file.Content) {
				chips = appendChip(chips, "Optional")
			}

			// Constraint-bearing call: either the RHS value (Field(...),
			// serializers.X(...), conint(...)) or a Field()/con*() nested in
			// an Annotated[...] / bare constrained-type annotation.
			for _, call := range constraintCalls(annot, rhs, file.Content) {
				chips = append(chips, callConstraintChips(call, file.Content)...)
			}

			// Validator-named field marker.
			if validated[attr] {
				chips = appendChip(chips, "Validated")
			}

			chips = dedupeChips(chips)
			if len(chips) == 0 {
				continue
			}
			(*out)[idx].Properties = applyValidations((*out)[idx].Properties, chips)
		}
	}
}

// constraintCalls returns the call nodes that may carry validation kwargs for a
// single field declaration: the RHS value when it is a recognised constraint
// call, plus any Field()/constrained-type call embedded in the annotation
// (Annotated[T, Field(...)] or a bare conint(...)/constr(...) annotation).
func constraintCalls(annot, rhs ts.Node, src []byte) []ts.Node {
	var calls []ts.Node
	if rhs != nil && rhs.Type() == "call" {
		calls = append(calls, rhs)
	}
	if annot == nil {
		return calls
	}
	// annot is a `type` node wrapping the real annotation expression.
	inner := annot
	if inner.Type() == "type" && inner.NamedChildCount() > 0 {
		inner = inner.NamedChild(0)
	}
	collectAnnotationCalls(inner, src, &calls)
	return calls
}

// collectAnnotationCalls walks an annotation expression collecting call nodes
// that look like Field(...) or a Pydantic constrained-type constructor. It
// recurses through generic_type / type_parameter so Annotated[str, Field(...)]
// and Optional[conint(gt=0)] are reached.
func collectAnnotationCalls(n ts.Node, src []byte, out *[]ts.Node) {
	if n == nil {
		return
	}
	if n.Type() == "call" {
		if isConstraintCall(n, src) {
			*out = append(*out, n)
		}
		return
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		collectAnnotationCalls(n.NamedChild(i), src, out)
	}
}

// isConstraintCall reports whether a call node is a Pydantic Field(...) or a
// recognised constrained-type constructor.
func isConstraintCall(call ts.Node, src []byte) bool {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return false
	}
	leaf := callLeafName(fn, src)
	if leaf == "Field" {
		return true
	}
	_, ok := pydanticConstrainedTypes[leaf]
	return ok
}

// callConstraintChips derives the constraint chips for a single recognised
// constraint call (Pydantic Field()/con*() or a DRF serializers.X()).
func callConstraintChips(call ts.Node, src []byte) []string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return nil
	}
	leaf := callLeafName(fn, src)
	funcText := nodeText(fn, src)
	isDRF := strings.Contains(funcText, "serializers.") || strings.HasSuffix(leaf, "Field")
	isPydantic := leaf == "Field"
	if _, con := pydanticConstrainedTypes[leaf]; con {
		isPydantic = true
	}

	var chips []string

	// Pydantic Field(...) with a leading ellipsis positional → Required.
	if isPydantic && callHasEllipsisArg(call) {
		chips = appendChip(chips, "Required")
	}

	args := call.ChildByFieldName("arguments")
	if args == nil {
		return chips
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		arg := args.Child(i)
		if arg == nil || arg.Type() != "keyword_argument" {
			continue
		}
		key := nodeText(arg.ChildByFieldName("name"), src)
		val := arg.ChildByFieldName("value")
		if key == "" {
			continue
		}

		if isDRF && !isPydantic {
			if bc, ok := drfFieldBoolKwarg[key]; ok {
				if c := drfBoolChip2chip(bc, val, src); c != "" {
					chips = appendChip(chips, c)
				}
				continue
			}
			if prefix, ok := drfFieldKwargChip[key]; ok {
				if v := scalarText(val, src); v != "" {
					chips = appendChip(chips, prefix+":"+v)
				}
				continue
			}
			continue
		}

		// Pydantic Field()/constrained-type kwargs.
		if prefix, ok := pydanticFieldKwargChip[key]; ok {
			if v := scalarText(val, src); v != "" {
				chips = appendChip(chips, prefix+":"+v)
			}
			continue
		}
		if marker, ok := pydanticFieldMarkerKwarg[key]; ok {
			chips = appendChip(chips, marker)
			continue
		}
	}
	return chips
}

// drfBoolChip2chip maps a DRF boolean kwarg's value to its chip (or "").
func drfBoolChip2chip(bc drfBoolChip, val ts.Node, src []byte) string {
	if val == nil {
		return ""
	}
	switch nodeText(val, src) {
	case "True":
		return bc.whenTrue
	case "False":
		return bc.whenFalse
	}
	return ""
}

// callHasEllipsisArg reports whether the call has a leading `...` positional
// argument (Pydantic Field(..., …) → required).
func callHasEllipsisArg(call ts.Node) bool {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		c := args.Child(i)
		if c == nil {
			continue
		}
		switch c.Type() {
		case "ellipsis":
			return true
		case "(", ",", "comment":
			continue
		default:
			// First non-trivial arg is not an ellipsis.
			return false
		}
	}
	return false
}

// callLeafName returns the trailing identifier of a call's function node
// (`serializers.CharField` → "CharField", `Field` → "Field").
func callLeafName(fn ts.Node, src []byte) string {
	if fn == nil {
		return ""
	}
	if fn.Type() == "attribute" {
		if a := fn.ChildByFieldName("attribute"); a != nil {
			fn = a
		}
	}
	if fn.Type() != "identifier" {
		return ""
	}
	return nodeText(fn, src)
}

// annotationIsOptional reports whether an annotation expression denotes an
// Optional value: `Optional[...]`, `Optional`, or a `X | None` / `None | X`
// union.
func annotationIsOptional(annot ts.Node, src []byte) bool {
	text := nodeText(annot, src)
	if strings.Contains(text, "Optional[") || text == "Optional" {
		return true
	}
	// `X | None` union — binary_operator with a `none` operand.
	if strings.Contains(text, "|") && strings.Contains(text, "None") {
		return true
	}
	return false
}

// collectValidatorFieldTargets scans the class body for @field_validator(...) /
// @validator(...) decorated methods and returns the set of field names they
// name as string positional arguments. `@field_validator("name", "email")` →
// {"name","email"}. `@validator("*")` is ignored (too broad for a per-field
// marker).
func collectValidatorFieldTargets(body ts.Node, src []byte) map[string]bool {
	out := map[string]bool{}
	for i := 0; i < int(body.ChildCount()); i++ {
		stmt := body.Child(i)
		if stmt == nil || stmt.Type() != "decorated_definition" {
			continue
		}
		for j := 0; j < int(stmt.ChildCount()); j++ {
			dec := stmt.Child(j)
			if dec == nil || dec.Type() != "decorator" {
				continue
			}
			// decorator → expression (call or attribute/identifier)
			call := firstNamedChild(dec)
			if call == nil || call.Type() != "call" {
				continue
			}
			fn := call.ChildByFieldName("function")
			if fn == nil {
				continue
			}
			name := leafIdentText(fn, src)
			if name != "field_validator" && name != "validator" {
				continue
			}
			args := call.ChildByFieldName("arguments")
			if args == nil {
				continue
			}
			for k := 0; k < int(args.ChildCount()); k++ {
				a := args.Child(k)
				if a == nil || a.Type() != "string" {
					continue
				}
				v := stripQuotes(strings.TrimSpace(nodeText(a, src)))
				if v != "" && v != "*" {
					out[v] = true
				}
			}
		}
	}
	return out
}

// firstNamedChild returns the first named child of a node, or nil.
func firstNamedChild(n ts.Node) ts.Node {
	if n == nil || n.NamedChildCount() == 0 {
		return nil
	}
	return n.NamedChild(0)
}

// leafIdentText returns the trailing identifier of an identifier/attribute
// node (`pydantic.field_validator` → "field_validator").
func leafIdentText(n ts.Node, src []byte) string {
	t := nodeText(n, src)
	if dot := strings.LastIndexByte(t, '.'); dot >= 0 {
		return t[dot+1:]
	}
	return t
}

// scalarText returns the text of a simple scalar value node (integer, float,
// true/false, identifier) suitable for folding into a chip. Compound values
// (strings with commas, regexes, expressions) yield "" so the caller folds a
// bare marker instead. Strings are dropped (they may contain commas which
// would corrupt the comma-joined property).
func scalarText(val ts.Node, src []byte) string {
	if val == nil {
		return ""
	}
	switch val.Type() {
	case "integer", "float", "true", "false", "identifier":
		t := nodeText(val, src)
		if strings.ContainsAny(t, ",") {
			return ""
		}
		return t
	case "unary_operator":
		// e.g. -1 — keep if the operand is numeric.
		t := nodeText(val, src)
		if !strings.ContainsAny(t, ", ") {
			return t
		}
	}
	return ""
}

// appendChip appends a chip if non-empty.
func appendChip(chips []string, c string) []string {
	if c == "" {
		return chips
	}
	return append(chips, c)
}

// dedupeChips removes duplicate chips while preserving first-seen order.
func dedupeChips(chips []string) []string {
	if len(chips) <= 1 {
		return chips
	}
	seen := make(map[string]bool, len(chips))
	out := chips[:0]
	for _, c := range chips {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

// applyValidations stamps the chips onto a field entity's Properties map
// (comma-joined), preserving any existing properties. Mirrors the TS
// applyFieldValidations helper.
func applyValidations(props map[string]string, chips []string) map[string]string {
	if len(chips) == 0 {
		return props
	}
	if props == nil {
		props = map[string]string{}
	}
	props["validations"] = strings.Join(chips, ",")
	return props
}
