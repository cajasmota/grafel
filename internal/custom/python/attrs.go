package python

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python_attrs", &AttrsExtractor{})
}

// AttrsExtractor extracts attrs / attr class, attribute, and validator patterns
// from Python source files.
//
// It emits SCOPE.Pattern entities for:
//   - @attr.s / @attr.attrs / @attrs.define / @define decorated classes
//   - attrib() / attr.ib() / field() attribute declarations
//   - validator= kwarg on attrib() / field()
//   - @<field>.validator decorator functions
//   - @<field>.default / factory= coercion hints
//
// It deliberately does NOT re-emit the class entity — the base Python extractor
// already emits a SCOPE.Component/class node for every class definition. Every
// entity here carries SCOPE.Pattern so it never shadows that node.
//
// Issue #2985 — attrs schema/constraint/validator extraction.
type AttrsExtractor struct{}

func (e *AttrsExtractor) Language() string { return "python_attrs" }

var (
	// @attr.s / @attr.attrs / @attr.define / @define / @attrs.define / @attr.mutable / @attr.frozen
	attrsClassDecoratorRe = regexp.MustCompile(
		`(?m)^[ \t]*@(?:attr\.s|attr\.attrs|attrs\.define|attr\.define|attrs\.mutable|attrs\.frozen|attr\.mutable|attr\.frozen|define|mutable|frozen)\s*(?:\([^)]*\))?\s*\n` +
			`(?:[ \t]*@\w[\w.]*\s*(?:\([^)]*\))?\s*\n)*` +
			`[ \t]*class\s+(\w+)\s*`)

	// @dataclass / @dataclasses.dataclass / @dataclass(frozen=True) — stdlib
	// dataclasses share the annotated-field body shape with attrs, so the same
	// DTO field-member emission applies (issue #4714).
	dataclassDecoratorRe = regexp.MustCompile(
		`(?m)^[ \t]*@(?:dataclasses\.)?dataclass\s*(?:\([^)]*\))?\s*\n` +
			`(?:[ \t]*@\w[\w.]*\s*(?:\([^)]*\))?\s*\n)*` +
			`[ \t]*class\s+(\w+)\s*`)

	// x = attr.ib() / x = attrib() / x = attr.attrib() / x = field()
	attrsAttribRe = regexp.MustCompile(
		`(?m)^[ \t]+(\w+)\s*(?::\s*\S+\s*)?=\s*(?:attr\.ib|attrib|attr\.attrib|field|attrs\.field)\s*\(([^)]*)\)`)

	// @x.validator  (where x is an attribute name)
	attrsFieldValidatorRe = regexp.MustCompile(
		`(?m)^[ \t]*@(\w+)\.validator\s*\n` +
			`(?:[ \t]*@\w[\w.]*\s*(?:\([^)]*\))?\s*\n)*` +
			`[ \t]*(?:async\s+)?def\s+(\w+)\s*\(`)

	// validator= kwarg inside attrib()/field() — may reference a function,
	// validators.instance_of(), validators.and_(), etc.
	attrsValidatorKwargRe = regexp.MustCompile(`\bvalidator\s*=\s*([^\s,)][^,)]*[^\s,)]|[^\s,)])`)

	// factory= / default=Factory(...) — coercion/default shapes
	attrsFactoryRe = regexp.MustCompile(`\bfactory\s*=\s*(\w[\w.]*)`)

	// converter= kwarg — type coercion
	attrsConverterRe = regexp.MustCompile(`\bconverter\s*=\s*(\w[\w.]*)`)

	// Constraint validators — match validators.instance_of(X), validators.in_([...]),
	// validators.and_(...), and their attr.validators.* / attrs.validators.* prefixed forms.
	// Note: closing ")" is optional because the attrib regex may truncate nested parens.
	attrsInstanceOfRe = regexp.MustCompile(`(?:attr(?:s)?\.)?validators\.instance_of\(\s*(\w[\w.]*)`)
	attrsInRe         = regexp.MustCompile(`(?:attr(?:s)?\.)?validators\.in_\(\s*(\[[^\]]*\]|[^\s,)]+)`)
	attrsAndRe        = regexp.MustCompile(`(?:attr(?:s)?\.)?validators\.and_\(`)
)

// attrsReferenced reports whether the source likely uses the attrs library or
// stdlib dataclasses (both share the annotated-field DTO body shape, #4714).
func attrsReferenced(source string) bool {
	if strings.Contains(source, "dataclass") {
		return true
	}
	return strings.Contains(source, "attrs") ||
		strings.Contains(source, "attr") && (strings.Contains(source, "attr.s") ||
			strings.Contains(source, "attr.ib") ||
			strings.Contains(source, "attrib") ||
			strings.Contains(source, "attr.define") ||
			strings.Contains(source, "@define") ||
			strings.Contains(source, "import attr"))
}

func (e *AttrsExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_attrs")
	_, span := tracer.Start(ctx, "custom.python_attrs")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)

	if !attrsReferenced(source) {
		return nil, nil
	}

	var out []types.EntityRecord

	// 1. attrs-decorated class definitions — evidence of schema_extraction.
	for _, idx := range allMatchesIndex(attrsClassDecoratorRe, source) {
		className := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		// Capture the decorator form from the source text.
		decoratorText := source[idx[0]:idx[1]]
		decoratorForm := "define"
		for _, form := range []string{"attr.s", "attr.attrs", "attrs.define", "attr.define",
			"attrs.mutable", "attrs.frozen", "attr.mutable", "attr.frozen", "mutable", "frozen", "define"} {
			if strings.Contains(decoratorText, form) {
				decoratorForm = form
				break
			}
		}
		out = append(out, entity(
			"schema_"+className, string(types.EntityKindPattern), "",
			file.Path, line,
			map[string]string{
				"framework":      "attrs",
				"pattern_type":   "attrs_class",
				"class_name":     className,
				"decorator_form": decoratorForm,
			},
		))
	}

	// 2. Attribute declarations (attrib / field / attr.ib).
	for _, idx := range allMatchesIndex(attrsAttribRe, source) {
		attrName := source[idx[2]:idx[3]]
		args := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		props := map[string]string{
			"framework":    "attrs",
			"pattern_type": "attrib",
			"field":        attrName,
		}
		// validator= kwarg inline evidence.
		if m := attrsValidatorKwargRe.FindStringSubmatch(args); m != nil {
			props["validator"] = strings.TrimSpace(m[1])
		}
		// converter= kwarg — type coercion.
		if m := attrsConverterRe.FindStringSubmatch(args); m != nil {
			props["converter"] = strings.TrimSpace(m[1])
		}
		// factory= kwarg.
		if m := attrsFactoryRe.FindStringSubmatch(args); m != nil {
			props["factory"] = strings.TrimSpace(m[1])
		}
		out = append(out, entity(
			"attrib_"+attrName, string(types.EntityKindPattern), "",
			file.Path, line, props,
		))
	}

	// 3. @<field>.validator decorator-style validators.
	for _, idx := range allMatchesIndex(attrsFieldValidatorRe, source) {
		targetAttr := source[idx[2]:idx[3]]
		fnName := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity(
			"validate_"+fnName, string(types.EntityKindPattern), "",
			file.Path, line,
			map[string]string{
				"framework":    "attrs",
				"pattern_type": "field_validator",
				"target_field": targetAttr,
				"validator_fn": fnName,
			},
		))
	}

	// 4. Constraint extraction: validators.instance_of() / in_() / and_()
	// on attrib()/field() validator= kwargs. Emit a dedicated constraint_<field>
	// entity per field so coverage/constraint_extraction can be cited. Issue #3077.
	for _, idx := range allMatchesIndex(attrsAttribRe, source) {
		attrName := source[idx[2]:idx[3]]
		args := source[idx[4]:idx[5]]
		validatorVal := ""
		if m := attrsValidatorKwargRe.FindStringSubmatch(args); m != nil {
			validatorVal = strings.TrimSpace(m[1])
		}
		if validatorVal == "" {
			continue
		}
		props := map[string]string{
			"framework":    "attrs",
			"pattern_type": "constraint",
			"field":        attrName,
		}
		emitConstraint := false
		// Check and_() first — it wraps other validators and contains them as args,
		// so instance_of/in_ would false-match inside an and_() call.
		if attrsAndRe.MatchString(validatorVal) {
			props["constraint_validator"] = "and_"
			emitConstraint = true
		} else if m := attrsInstanceOfRe.FindStringSubmatch(validatorVal); m != nil {
			props["constraint_type"] = strings.TrimSpace(m[1])
			props["constraint_validator"] = "instance_of"
			emitConstraint = true
		} else if m := attrsInRe.FindStringSubmatch(validatorVal); m != nil {
			props["constraint_values"] = strings.TrimSpace(m[1])
			props["constraint_validator"] = "in_"
			emitConstraint = true
		}
		if !emitConstraint {
			continue
		}
		line := lineOf(source, idx[0])
		out = append(out, entity(
			"constraint_"+attrName, string(types.EntityKindPattern), "",
			file.Path, line, props,
		))
	}

	// 5. Field-as-member sub-entities (issue #4714). Each attrs/dataclass class's
	// annotated attributes become `SCOPE.Schema`/field children with a CONTAINS
	// edge to the class, the SAME shape as the Pydantic/DRF/marshmallow/JS DTO
	// field members so cross-framework FIELD-level diffs stay uniform. The owner
	// class node is emitted by the base Python extractor; we hang only members.
	emitDataclassFields := func(re *regexp.Regexp, library string) {
		for _, idx := range allMatchesIndex(re, source) {
			className := source[idx[2]:idx[3]]
			// The decorator regex's match start is the `@…` line; locate the
			// `class` header so the body scan dedents from the class indent.
			classStart := strings.Index(source[idx[0]:idx[1]], "class ")
			if classStart < 0 {
				continue
			}
			classStart += idx[0]
			line := lineOf(source, classStart)
			body := pydModelBody(source, classStart)
			fields := extractAttrsDataclassFields(body)
			out = append(out, emitPyDTOFieldMembers(
				className, fields, library, file.Path, line, nil)...)
		}
	}
	emitDataclassFields(attrsClassDecoratorRe, "attrs")
	emitDataclassFields(dataclassDecoratorRe, "dataclasses")

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
