// field_validations.go — Issue #4872. Per-field Bean Validation constraints for
// Java DTO/record/class fields, stamped onto the SCOPE.Schema/field entity under
// Properties["validations"] (comma-joined) so the dashboard ShapeTree surfaces
// them as small constraint chips — mirroring the TS class-validator support in
// #4858 (internal/extractors/javascript/class_validator_fields.go) and the
// Python field-validation support in #4871
// (internal/extractors/python/field_validations.go).
//
// The dashboard side already exists: internal/dashboard/shape_tree.go reads
// Properties["validations"] (comma-split) into v2ShapeRow.Validations, and
// webui-v2/src/components/ShapeTree.tsx renders the chips. This pass only has
// to populate that property on the Java field entities.
//
// Java DTO fields carry Bean Validation annotations from BOTH the legacy
// javax.validation.constraints.* package and the modern
// jakarta.validation.constraints.* package, e.g.
//
//	public class CreateUserDto {
//	    @NotNull
//	    @Size(max = 120)
//	    @Email
//	    private String email;
//	}
//
// These annotations are already preserved in the field signature (buildField
// keeps the raw declaration), but they were NOT routed into the unified
// `validations` property that drives the chips. This pass reuses the same
// tree-sitter `modifiers` → annotation children that javaFieldHasInjectAnnotation
// already walks, classifies each recognised Bean Validation annotation, and
// stamps the terse chip list.
//
// Covered Bean Validation annotations (javax.* and jakarta.*):
//
//   - @NotNull → Required; @NotEmpty → NotEmpty; @NotBlank → NotBlank;
//     @Null → Null.
//   - @Size(min=, max=) → Size:min..max (or MinLength:/MaxLength: when only one
//     bound is present).
//   - @Min / @DecimalMin → Min:value; @Max / @DecimalMax → Max:value.
//   - @Pattern(regexp=) → Pattern; @Email → Email.
//   - @Positive → Positive; @PositiveOrZero → PositiveOrZero; @Negative →
//     Negative; @NegativeOrZero → NegativeOrZero; @Digits → Digits.
//   - @Past → Past; @PastOrPresent → PastOrPresent; @Future → Future;
//     @FutureOrPresent → FutureOrPresent.
//   - @AssertTrue → AssertTrue; @AssertFalse → AssertFalse.
//   - @Valid → Valid (nested validation marker).
//
// Chip text is kept terse and comma-free (Properties is comma-joined and the
// dashboard splits on ","): bounds fold their scalar value (`Max:120`,
// `Min:0`), @Size folds into `Size:0..120` / `MaxLength:120`; the rest render
// as a bare marker (`Required`, `Email`, `Pattern`). Only stamped when at least
// one constraint is found; existing Properties are preserved.

package java

import (
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// javaBeanValidationMarkers maps a Bean Validation annotation's simple name to
// the bare marker chip it produces. These carry no scalar bound worth folding
// (their arguments, if any, are messages/groups/regexes that would corrupt the
// comma-joined property), so only the annotation identity is recorded.
//
// @NotNull renders as `Required` to match the TS/Python required marker.
var javaBeanValidationMarkers = map[string]string{
	"NotNull":         "Required",
	"NotEmpty":        "NotEmpty",
	"NotBlank":        "NotBlank",
	"Null":            "Null",
	"Email":           "Email",
	"Pattern":         "Pattern",
	"Positive":        "Positive",
	"PositiveOrZero":  "PositiveOrZero",
	"Negative":        "Negative",
	"NegativeOrZero":  "NegativeOrZero",
	"Digits":          "Digits",
	"Past":            "Past",
	"PastOrPresent":   "PastOrPresent",
	"Future":          "Future",
	"FutureOrPresent": "FutureOrPresent",
	"AssertTrue":      "AssertTrue",
	"AssertFalse":     "AssertFalse",
	"Valid":           "Valid",
}

// javaBeanValidationScalar maps a Bean Validation annotation whose `value`
// (single-element) or default argument is a scalar bound to the chip prefix it
// folds into (`@Min(0)` → "Min:0"). @DecimalMin / @DecimalMax carry their bound
// as a string literal but fold identically.
var javaBeanValidationScalar = map[string]string{
	"Min":        "Min",
	"Max":        "Max",
	"DecimalMin": "Min",
	"DecimalMax": "Max",
}

// emitJavaFieldValidations walks every field entity emitted for one class/record
// (in the [before, after) window of out) and stamps Properties["validations"]
// with the Bean Validation chips collected from the matching declaration's
// annotations.
//
// fieldNodes maps the field/record-component leaf name to the AST node carrying
// its annotations (a field_declaration's `modifiers`, or a record
// formal_parameter). It is built by the caller while it walks the class body so
// this pass does not re-traverse the tree.
func emitJavaFieldValidations(
	parentType string,
	before, after int,
	fieldNodes map[string]ts.Node,
	src []byte,
	out *[]types.EntityRecord,
) {
	if parentType == "" || out == nil || len(fieldNodes) == 0 {
		return
	}
	prefix := parentType + "."
	for k := before; k < after; k++ {
		if k < 0 || k >= len(*out) {
			continue
		}
		e := &(*out)[k]
		if e.Kind != "SCOPE.Schema" || e.Subtype != "field" {
			continue
		}
		if !strings.HasPrefix(e.Name, prefix) {
			continue
		}
		leaf := e.Name[len(prefix):]
		if leaf == "" || strings.Contains(leaf, ".") {
			continue
		}
		node, ok := fieldNodes[leaf]
		if !ok || node == nil {
			continue
		}
		chips := javaFieldValidationChips(node, src)
		if len(chips) == 0 {
			continue
		}
		(*out)[k].Properties = applyJavaValidations((*out)[k].Properties, chips)
	}
}

// javaFieldValidationChips collects the Bean Validation chips from the
// annotations on a field declaration or record component. `node` may be a
// field_declaration, a formal_parameter (record component) or any node whose
// direct/`modifiers` children include annotation nodes — the helper finds the
// annotation nodes either way.
func javaFieldValidationChips(node ts.Node, src []byte) []string {
	if node == nil {
		return nil
	}
	var chips []string
	for _, ann := range javaAnnotationNodes(node) {
		nameNode := ann.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		name := simpleAnnotationName(string(src[nameNode.StartByte():nameNode.EndByte()]))
		if name == "" {
			continue
		}
		if name == "Size" {
			if c := javaSizeChip(ann, src); c != "" {
				chips = appendJavaChip(chips, c)
			}
			continue
		}
		if prefix, ok := javaBeanValidationScalar[name]; ok {
			if v := javaAnnotationScalarArg(ann, src); v != "" {
				chips = appendJavaChip(chips, prefix+":"+v)
			} else {
				chips = appendJavaChip(chips, prefix)
			}
			continue
		}
		if marker, ok := javaBeanValidationMarkers[name]; ok {
			chips = appendJavaChip(chips, marker)
			continue
		}
	}
	return dedupeJavaChips(chips)
}

// javaAnnotationNodes returns the annotation / marker_annotation nodes attached
// to a declaration. For a field_declaration / formal_parameter the annotations
// live under a `modifiers` child; for nodes that themselves are `modifiers`
// they are direct children. Both shapes are handled.
func javaAnnotationNodes(node ts.Node) []ts.Node {
	var out []ts.Node
	collect := func(parent ts.Node) {
		for i := 0; i < int(parent.NamedChildCount()); i++ {
			ch := parent.NamedChild(i)
			if ch == nil {
				continue
			}
			if ch.Type() == "marker_annotation" || ch.Type() == "annotation" {
				out = append(out, ch)
			}
		}
	}
	if node.Type() == "modifiers" {
		collect(node)
		return out
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		ch := node.NamedChild(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "modifiers":
			collect(ch)
		case "marker_annotation", "annotation":
			out = append(out, ch)
		}
	}
	return out
}

// simpleAnnotationName strips a fully-qualified annotation name to its leaf
// (`jakarta.validation.constraints.NotNull` → "NotNull").
func simpleAnnotationName(name string) string {
	name = strings.TrimSpace(name)
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		name = name[dot+1:]
	}
	return name
}

// javaSizeChip folds a @Size annotation into a chip. With both bounds it yields
// `Size:0..120`; with only max it yields `MaxLength:120`; with only min it
// yields `MinLength:1`. Returns "" when neither bound is a scalar.
func javaSizeChip(ann ts.Node, src []byte) string {
	pairs := javaAnnotationArgPairs(ann, src)
	minV, hasMin := pairs["min"]
	maxV, hasMax := pairs["max"]
	switch {
	case hasMin && hasMax:
		return "Size:" + minV + ".." + maxV
	case hasMax:
		return "MaxLength:" + maxV
	case hasMin:
		return "MinLength:" + minV
	}
	return ""
}

// javaAnnotationScalarArg returns the scalar bound of a single-value annotation
// like @Min(0) / @Max(120) / @DecimalMin("0.0"). It accepts both the implicit
// `value` form (`@Min(0)`) and the named form (`@Min(value = 0)`). String
// literals (DecimalMin/DecimalMax) are unquoted. Returns "" for non-scalar args.
func javaAnnotationScalarArg(ann ts.Node, src []byte) string {
	args := ann.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	// Named form: value = N.
	if pairs := javaAnnotationArgPairs(ann, src); len(pairs) > 0 {
		if v, ok := pairs["value"]; ok {
			return v
		}
	}
	// Implicit single positional form: @Min(0).
	for i := 0; i < int(args.NamedChildCount()); i++ {
		a := args.NamedChild(i)
		if a == nil {
			continue
		}
		if a.Type() == "element_value_pair" {
			continue // handled by the pairs path above
		}
		return javaValidationScalar(a, src)
	}
	return ""
}

// javaAnnotationArgPairs returns the named element_value_pair arguments of an
// annotation as a key→scalar map (`@Size(min = 1, max = 120)` →
// {"min":"1","max":"120"}). Non-scalar values are dropped.
func javaAnnotationArgPairs(ann ts.Node, src []byte) map[string]string {
	args := ann.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	out := map[string]string{}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		p := args.NamedChild(i)
		if p == nil || p.Type() != "element_value_pair" {
			continue
		}
		keyNode := p.ChildByFieldName("key")
		valNode := p.ChildByFieldName("value")
		if keyNode == nil || valNode == nil {
			continue
		}
		key := string(src[keyNode.StartByte():keyNode.EndByte()])
		if v := javaValidationScalar(valNode, src); v != "" {
			out[strings.TrimSpace(key)] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// javaValidationScalar returns the folded text of a scalar annotation argument
// (integer / decimal / string / boolean / bare identifier), unquoting string
// literals and rejecting anything containing a comma (which would corrupt the
// comma-joined property). String literals are kept only when numeric
// (@DecimalMin("0.0")). Compound expressions yield "". Reuses javaLiteralText
// for the literal kinds it recognises.
func javaValidationScalar(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	if n.Type() == "identifier" {
		t := strings.TrimSpace(string(src[n.StartByte():n.EndByte()]))
		if strings.ContainsAny(t, ", ") {
			return ""
		}
		return t
	}
	lit, ok := javaLiteralText(n, src)
	if !ok {
		return ""
	}
	lit = strings.TrimSpace(lit)
	if strings.ContainsAny(lit, ", ") {
		return ""
	}
	// string_literal bounds (DecimalMin/DecimalMax) must be numeric.
	if n.Type() == "string_literal" {
		if _, err := strconv.ParseFloat(lit, 64); err != nil {
			return ""
		}
	}
	return lit
}

// appendJavaChip appends a chip if non-empty.
func appendJavaChip(chips []string, c string) []string {
	if c == "" {
		return chips
	}
	return append(chips, c)
}

// dedupeJavaChips removes duplicate chips while preserving first-seen order.
func dedupeJavaChips(chips []string) []string {
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

// applyJavaValidations stamps the chips onto a field entity's Properties map
// (comma-joined), preserving any existing properties. Mirrors the TS
// applyFieldValidations / Python applyValidations helpers.
func applyJavaValidations(props map[string]string, chips []string) map[string]string {
	if len(chips) == 0 {
		return props
	}
	if props == nil {
		props = map[string]string{}
	}
	props["validations"] = strings.Join(chips, ",")
	return props
}
