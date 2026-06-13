// validators.go — #5130 (follow-up #5049). The validation TAIL deferred from
// #5049 / shipped-base #5127 (DataAnnotations field chips):
//
//   1. Validus validator pipelines — the `validate { ... }` / `validator { ... }`
//      computation expression and the `Check.String.*` / `Check.Int.*` /
//      `ValidatorGroup(...)` combinators (Validus, https://github.com/pimbrouwers/Validus).
//   2. FsToolkit.ErrorHandling — the `validation { ... }` computation expression
//      and `Result`/`Validation` applicative accumulation
//      (FsToolkit.ErrorHandling, https://github.com/demystifyfp/FsToolkit.ErrorHandling).
//   3. Custom DataAnnotations validators — `[<CustomValidation(typeof<...>, "Method")>]`
//      on a record field, and a type that implements `IValidatableObject`
//      (its `Validate` member is the custom validator).
//   4. Nested-record validation — a record field whose annotated type is another
//      record defined in the SAME file is treated as a nested validated object,
//      materialising an owner→nested VALIDATES edge (via=nested_model), the F#
//      analog of `[<ValidateComplexType>]` recursive DataAnnotations validation.
//
// All four map onto the existing validation-rule shape used by the other
// languages (JS class-validator / express-validator #2904, Java Bean-Validation
// #4872): a VALIDATES RelationshipRecord whose Properties carry `library`,
// `via`, and `line` (and, for the pipeline CEs, the recognised combinators).
// Pipeline edges are emitted from the enclosing SCOPE.Operation; nested + custom
// type-level edges are emitted from the owning SCOPE.Component type.
//
// HONEST SCOPE: these are regex head-symbol heuristics (no F# type/CE
// resolution — consistent with the rest of the fsharp extractor, which has no
// tree-sitter F# grammar). A validator pipeline is recognised by the CE head
// (`validate`/`validation`) and/or the presence of >=1 recognised combinator;
// the edge target is a synthetic `validator:<library>` stub (no stub entity is
// emitted, mirroring the raw-string CALLS-target convention already in use).
package fsharp

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// Validator library identifiers used as the VALIDATES edge target stub and the
// `library` Property value.
const (
	fsValidusLib        = "validus"
	fsFsToolkitLib      = "fstoolkit"
	fsDataAnnotationLib = "dataannotations"
)

// fsValidusCombinatorRE matches a Validus combinator head — `Check.String.*`,
// `Check.Int.*`, `Check.Guid.*`, ... and the bare `validateField` /
// `ValidatorGroup` entry points. The captured group is the combinator text used
// to stamp `combinators` on the edge.
var fsValidusCombinatorRE = regexp.MustCompile(`\b(Check\.[A-Za-z]+\.[A-Za-z]+|validateField|ValidatorGroup)\b`)

// fsFsToolkitCombinatorRE matches the FsToolkit.ErrorHandling applicative
// accumulation surface — `Validation.*` combinators and the `and!`/`let!`
// applicative bind operators inside a `validation { ... }` CE.
var fsFsToolkitCombinatorRE = regexp.MustCompile(`\b(Validation\.[A-Za-z]+|Result\.[A-Za-z]+)\b`)

// fsCustomValidationAttrRE matches a `[<CustomValidation(typeof<Owner>, "Method")>]`
// field attribute, capturing the validator type and the method name.
var fsCustomValidationAttrRE = regexp.MustCompile(`CustomValidation\s*\(\s*typeof<\s*([A-Za-z_][\w.]*)\s*>\s*,\s*"([^"]+)"`)

// fsCEHeadRE captures a computation-expression head at a clause position
// (`validate {` / `validation {`). The head must be preceded by `=`, `(`, a
// clause boundary, or the start of a line so a record `{` is not mistaken for a
// CE body.
var fsCEHeadRE = regexp.MustCompile(`(?:^|[=(>|;,]|\blet\b|\breturn\b)\s*([A-Za-z_]\w*)\s*\{`)

// collectValidatorPipelineEdges scans a let/member body for Validus and
// FsToolkit.ErrorHandling validator pipelines and returns one VALIDATES edge per
// recognised library. The edge points at the `validator:<library>` stub and
// carries the recognised combinators so the validation surface is queryable.
//
// bodyStartLine is the enclosing operation's StartLine, so the stamped `line` is
// file-absolute (matching collectCalls).
func collectValidatorPipelineEdges(body string, bodyStartLine int) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	scrubbed := stripStringsAndComments(body)

	var out []types.RelationshipRecord

	// (1) Validus — a `validate { ... }` / `validator { ... }` CE head OR the
	//     presence of a Check.* / validateField / ValidatorGroup combinator.
	validusCombos := dedupMatches(fsValidusCombinatorRE.FindAllStringSubmatch(scrubbed, -1))
	validusCE := ceHeadPresent(scrubbed, "validate", "validator")
	if len(validusCombos) > 0 || validusCE {
		line := firstSignalLine(scrubbed, bodyStartLine, fsValidusCombinatorRE, "validate", "validator")
		props := map[string]string{
			"library": fsValidusLib,
			"via":     "validator_pipeline",
			"line":    strconv.Itoa(line),
		}
		if len(validusCombos) > 0 {
			props["combinators"] = strings.Join(validusCombos, ",")
		}
		if validusCE {
			props["computation_expression"] = "true"
		}
		out = append(out, types.RelationshipRecord{
			ToID:       "validator:" + fsValidusLib,
			Kind:       string(types.RelationshipKindValidates),
			Properties: props,
		})
	}

	// (2) FsToolkit.ErrorHandling — a `validation { ... }` CE head OR a
	//     Validation.* / Result.* applicative-accumulation combinator.
	ftCombos := dedupMatches(fsFsToolkitCombinatorRE.FindAllStringSubmatch(scrubbed, -1))
	ftCE := ceHeadPresent(scrubbed, "validation")
	// A bare Result.* without the validation CE is ordinary error handling, not
	// applicative VALIDATION accumulation — only claim FsToolkit validation when
	// the `validation { }` CE is present OR a Validation.* combinator is used.
	hasValidationCombo := false
	for _, c := range ftCombos {
		if strings.HasPrefix(c, "Validation.") {
			hasValidationCombo = true
			break
		}
	}
	if ftCE || hasValidationCombo {
		line := firstSignalLine(scrubbed, bodyStartLine, fsFsToolkitCombinatorRE, "validation")
		props := map[string]string{
			"library": fsFsToolkitLib,
			"via":     "validator_pipeline",
			"line":    strconv.Itoa(line),
		}
		if len(ftCombos) > 0 {
			props["combinators"] = strings.Join(ftCombos, ",")
		}
		if ftCE {
			props["computation_expression"] = "true"
		}
		out = append(out, types.RelationshipRecord{
			ToID:       "validator:" + fsFsToolkitLib,
			Kind:       string(types.RelationshipKindValidates),
			Properties: props,
		})
	}

	return out
}

// ceHeadPresent reports whether any of the named CE builders is used as a
// `name { ... }` head at a clause position in the scrubbed body.
func ceHeadPresent(scrubbed string, heads ...string) bool {
	want := make(map[string]bool, len(heads))
	for _, h := range heads {
		want[h] = true
	}
	for _, m := range fsCEHeadRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) >= 2 && want[m[1]] {
			return true
		}
	}
	return false
}

// firstSignalLine returns the file-absolute line of the first combinator match
// or CE head in the scrubbed body, defaulting to bodyStartLine.
func firstSignalLine(scrubbed string, bodyStartLine int, combo *regexp.Regexp, heads ...string) int {
	best := -1
	if loc := combo.FindStringIndex(scrubbed); loc != nil {
		best = loc[0]
	}
	for _, m := range fsCEHeadRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) < 4 {
			continue
		}
		head := scrubbed[m[2]:m[3]]
		for _, h := range heads {
			if head == h && (best < 0 || m[2] < best) {
				best = m[2]
			}
		}
	}
	if best < 0 {
		return bodyStartLine
	}
	return bodyStartLine + strings.Count(scrubbed[:best], "\n")
}

// dedupMatches flattens a [][]string of FindAllStringSubmatch results into a
// de-duplicated, order-preserving list of the first capture group.
func dedupMatches(matches [][]string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		v := m[1]
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// fsCustomFieldValidator parses a single record-field attribute line for a
// `[<CustomValidation(typeof<Owner>, "Method")>]` annotation, returning the
// validator type, method, and whether a match was found. Used to stamp the
// field's `custom_validator` chip and emit a VALIDATES edge.
func fsCustomFieldValidator(attrLines []string) (validatorType, method string, ok bool) {
	for _, line := range attrLines {
		if m := fsCustomValidationAttrRE.FindStringSubmatch(line); m != nil {
			return m[1], m[2], true
		}
	}
	return "", "", false
}

// collectTypeValidatorEdges emits the TYPE-LEVEL validation edges for a record:
//
//   - NESTED MODEL: for each field whose declared type is another record defined
//     in `recordTypes`, an owner→nested VALIDATES edge (via=nested_model) — the
//     F# analog of recursive `[<ValidateComplexType>]` DataAnnotations
//     validation. The edge targets the nested record TYPE entity.
//   - CUSTOM FIELD VALIDATOR: for each field carrying
//     `[<CustomValidation(typeof<T>, "M")>]`, an owner→`validator:dataannotations`
//     VALIDATES edge (via=custom_validation) stamped with the validator type and
//     method.
//   - IValidatableObject: when the type implements IValidatableObject, an
//     owner→`validator:dataannotations` VALIDATES edge (via=ivalidatableobject)
//     — the type's `Validate` member is the custom validator.
//
// `fields` are the parsed record fields (with their resolved type + attribute
// lines); `recordTypes` is the set of record type names defined in the file
// (for nested-model resolution); `implementsIValidatable` is true when the
// type body declares `interface IValidatableObject`.
func collectTypeValidatorEdges(
	owner, filePath string,
	fields []fsFieldRef,
	recordTypes map[string]bool,
	implementsIValidatable bool,
	startLine int,
) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	seenNested := make(map[string]bool)

	for _, f := range fields {
		// Nested-model VALIDATES edge — the field's (stripped) type is a record
		// defined in this file. Optionals / collections of a nested record are
		// also followed (`Address option`, `Address list`, `Address[]`).
		if nested := fsNestedRecordType(f.typ, recordTypes); nested != "" && nested != owner && !seenNested[nested] {
			seenNested[nested] = true
			out = append(out, types.RelationshipRecord{
				ToID: nested,
				Kind: string(types.RelationshipKindValidates),
				Properties: map[string]string{
					"library": fsDataAnnotationLib,
					"via":     "nested_model",
					"field":   f.name,
					"line":    strconv.Itoa(startLine + f.lineOffset),
				},
			})
		}

		// Custom field validator — [<CustomValidation(typeof<T>, "M")>].
		if vt, method, ok := fsCustomFieldValidator(f.attrLines); ok {
			out = append(out, types.RelationshipRecord{
				ToID: "validator:" + fsDataAnnotationLib,
				Kind: string(types.RelationshipKindValidates),
				Properties: map[string]string{
					"library":        fsDataAnnotationLib,
					"via":            "custom_validation",
					"field":          f.name,
					"validator_type": vt,
					"method":         method,
					"line":           strconv.Itoa(startLine + f.lineOffset),
				},
			})
		}
	}

	// IValidatableObject — the type itself is a custom validator.
	if implementsIValidatable {
		out = append(out, types.RelationshipRecord{
			ToID: "validator:" + fsDataAnnotationLib,
			Kind: string(types.RelationshipKindValidates),
			Properties: map[string]string{
				"library": fsDataAnnotationLib,
				"via":     "ivalidatableobject",
				"line":    strconv.Itoa(startLine),
			},
		})
	}

	return out
}

// fsFieldRef is the minimal field shape collectTypeValidatorEdges needs: name,
// resolved type, attribute lines (for custom-validation detection), and the
// line offset for edge line-stamping.
type fsFieldRef struct {
	name       string
	typ        string
	attrLines  []string
	lineOffset int
}

// fsNestedRecordType returns the nested record type name referenced by a field
// type annotation, or "" if the type is not a record defined in this file. It
// strips F# type modifiers — `option`, `list`, `seq`, `array`, `[]`, and the
// `'T` generic application — so `Address option` / `Address list` / `Address[]`
// all resolve to `Address`.
func fsNestedRecordType(typ string, recordTypes map[string]bool) string {
	t := strings.TrimSpace(typ)
	if t == "" {
		return ""
	}
	// Drop trailing array brackets.
	t = strings.TrimSuffix(t, "[]")
	t = strings.TrimSpace(t)
	// Peel a leading or trailing wrapper word (option / list / seq / array).
	fields := strings.Fields(t)
	for _, w := range fields {
		w = strings.TrimSpace(w)
		if recordTypes[w] {
			return w
		}
	}
	// `Address option` shape — already covered by the Fields loop. A generic
	// application `Result<Address>` is intentionally NOT followed (it is not a
	// nested validated object in idiomatic F#).
	return ""
}

// collectRecordTypeNames returns the set of type names in the file classified
// as records (`type Foo = { ... }`), used for nested-model edge resolution. It
// reuses the same typeRE / classifyTypeSubtype machinery as the main type pass
// so the two agree on what a record is.
func collectRecordTypeNames(src string) map[string]bool {
	out := make(map[string]bool)
	for _, m := range typeRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		name := src[m[4]:m[5]]
		body := extractIndentBody(src, m[1], len(src[m[2]:m[3]]))
		if classifyTypeSubtype(src[m[0]:m[1]], body) == "record" {
			out[name] = true
		}
	}
	return out
}

// fsTypeImplementsIValidatable reports whether a type body declares
// `interface IValidatableObject` (with or without the `System.ComponentModel`
// qualifier), marking the type as carrying a custom IValidatableObject.Validate
// validator.
func fsTypeImplementsIValidatable(body string) bool {
	scrubbed := stripStringsAndComments(body)
	for _, line := range strings.Split(scrubbed, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "interface") && strings.Contains(line, "IValidatableObject") {
			return true
		}
	}
	return false
}
