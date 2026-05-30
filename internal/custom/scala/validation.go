// Package scala — field-level DTO and request-validation extraction.
//
// This file deepens the Scala Validation capability from "name-only" DTO
// detection to full field-level modeling, matching the TS/JS bar:
//
//   - dto_extraction:     case class request/response DTOs broken into their
//     individual fields (name + type), Option[T] nullability, circe
//     (@JsonCodec / deriveDecoder) and play-json (Json.format[T]) codec
//     attribution, and @key / @jsonField wire-name overrides.
//   - request_validation: refined types (Refined[String, NonEmpty],
//     Int Refined Positive, MatchesRegex[...]), cats Validated / ValidatedNel
//     validators, accord (validator[T] { p.field is notEmpty }), and octopus.
//     Each emitted entity records the SPECIFIC field + the constraint /
//     refinement applied to it.
//
// Every entity carries a synthetic, non-colliding name and a SCOPE.Pattern /
// SCOPE.Type kind so it never shadows a real class/function node. Issue #3454.
package scala

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// DTO field extraction
// ---------------------------------------------------------------------------

var (
	// case class Name ( ... — match the header + opening paren only; the
	// parameter blob is extracted with a balanced-paren scan (balancedParens)
	// so that parens inside field annotations (e.g. @JsonKey("x")) or default
	// values do not prematurely terminate the constructor.
	valDTOCaseClassRe = regexp.MustCompile(
		`(?ms)\bcase\s+class\s+(\w+)\s*(?:\[[^\]]*\]\s*)?\(`)

	// A single constructor field:  [@anno ...] name : Type [= default]
	// We capture leading annotations (for wire-name overrides), the field name,
	// and the declared type up to a comma / default / end.
	valDTOFieldRe = regexp.MustCompile(
		`(?s)((?:@\w+(?:\([^)]*\))?\s*)*)\b(\w+)\s*:\s*([^,=]+?)\s*(?:=\s*[^,]+)?$`)

	// @key("wire")  (upickle)  /  @JsonKey("wire")  (circe)  /
	// @jsonField("wire") (zio-json)  — wire-name override annotations.
	valWireNameRe = regexp.MustCompile(
		`@(?:key|JsonKey|jsonField|JsonProperty)\s*\(\s*"([^"]+)"\s*\)`)

	// circe codec derivation attached to / near a DTO.
	valCirceJsonCodecRe = regexp.MustCompile(`@JsonCodec\b`)
	valCirceDeriveRe    = regexp.MustCompile(`\bderive(?:Codec|Decoder|Encoder)\s*\[\s*(\w+)\s*\]`)
	valPlayJsonFormatRe = regexp.MustCompile(`\bJson\s*\.\s*(?:format|reads|writes|using)\s*\[\s*(\w+)\s*\]`)
	valZioJsonDeriveRe  = regexp.MustCompile(`\bDeriveJson(?:Codec|Decoder|Encoder)\s*\.\s*gen\s*\[\s*(\w+)\s*\]`)
)

// scalaDTOField is a parsed primary-constructor field.
type scalaDTOField struct {
	Name     string
	Type     string // declared type, with Option[...] preserved
	Nullable bool   // true when declared Option[T] (or Maybe[T])
	WireName string // @key/@JsonKey override, empty if none
}

// extractScalaDTOFields parses every case class in src into field-level entities.
// It returns one SCOPE.Type/dto entity per case class (carrying a fields summary)
// plus one SCOPE.Type/dto_field entity per field so consumers can navigate to a
// specific field + its type + nullability + wire name.
func extractScalaDTOFields(src, framework string, file fileMeta) []types.EntityRecord {
	var out []types.EntityRecord

	// File-level codec attribution maps DTO name -> codec library.
	codecByDTO := scalaCodecAttribution(src)

	for _, m := range valDTOCaseClassRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		// m[1] is the byte just past the opening "(" of the constructor.
		paramBlob := balancedParens(src, m[1])
		line := lineOf(src, m[0])

		fields := parseScalaDTOFields(paramBlob)
		if len(fields) == 0 {
			// Parameterless case class (rare for a DTO) — still record the type
			// so name-only detection is preserved, matching prior behavior.
			ent := makeEntity(name, "SCOPE.Type", "dto", file.Path, file.Language, line)
			setProps(&ent, "framework", framework, "provenance", "CASE_CLASS_DTO")
			if codec := codecByDTO[name]; codec != "" {
				setProps(&ent, "codec", codec)
			}
			out = append(out, ent)
			continue
		}

		// Parent DTO entity with a fields summary + codec attribution.
		ent := makeEntity(name, "SCOPE.Type", "dto", file.Path, file.Language, line)
		setProps(&ent, "framework", framework, "provenance", "CASE_CLASS_DTO",
			"field_count", itoa(len(fields)),
			"fields", scalaFieldSummary(fields))
		if codec := codecByDTO[name]; codec != "" {
			setProps(&ent, "codec", codec)
		}
		var nullable, wired []string
		for _, f := range fields {
			if f.Nullable {
				nullable = append(nullable, f.Name)
			}
			if f.WireName != "" {
				wired = append(wired, f.Name+"="+f.WireName)
			}
		}
		if len(nullable) > 0 {
			setProps(&ent, "nullable_fields", strings.Join(nullable, ","))
		}
		if len(wired) > 0 {
			setProps(&ent, "wire_overrides", strings.Join(wired, ","))
		}
		out = append(out, ent)

		// One entity per field, name-spaced under the DTO.
		for _, f := range fields {
			fe := makeEntity("dto_field:"+name+"."+f.Name, "SCOPE.Type", "dto_field",
				file.Path, file.Language, line)
			setProps(&fe, "framework", framework, "provenance", "CASE_CLASS_DTO_FIELD",
				"dto", name, "field", f.Name, "field_type", f.Type,
				"nullable", boolStr(f.Nullable))
			if f.WireName != "" {
				setProps(&fe, "wire_name", f.WireName)
			}
			if codec := codecByDTO[name]; codec != "" {
				setProps(&fe, "codec", codec)
			}
			out = append(out, fe)
		}
	}
	return out
}

// parseScalaDTOFields splits a primary-constructor parameter blob into fields,
// respecting bracket depth so commas inside Type params (Map[String, Int]) do
// not split a field.
func parseScalaDTOFields(blob string) []scalaDTOField {
	var fields []scalaDTOField
	for _, part := range splitTopLevelCommas(blob) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// Strip leading val/var modifiers, but keep annotations for wire names.
		wire := ""
		if wm := valWireNameRe.FindStringSubmatch(part); wm != nil {
			wire = wm[1]
		}
		m := valDTOFieldRe.FindStringSubmatch(part)
		if m == nil {
			continue
		}
		fname := stripValVar(m[2])
		ftype := strings.TrimSpace(m[3])
		fields = append(fields, scalaDTOField{
			Name:     fname,
			Type:     ftype,
			Nullable: isScalaOptional(ftype),
			WireName: wire,
		})
	}
	return fields
}

// stripValVar removes a leading "val"/"var" keyword that the field regex may
// have captured as the name when present (e.g. `val name: String`).
func stripValVar(name string) string {
	switch name {
	case "val", "var":
		return ""
	}
	return name
}

// isScalaOptional reports whether a declared type denotes an optional/nullable
// field — Option[T], Maybe[T], or scala.Option[T].
func isScalaOptional(t string) bool {
	t = strings.TrimSpace(t)
	return strings.HasPrefix(t, "Option[") ||
		strings.HasPrefix(t, "scala.Option[") ||
		strings.HasPrefix(t, "Maybe[")
}

// scalaFieldSummary renders "name:Type" pairs (comma-separated) for the parent
// DTO entity, so a single property captures the full shape.
func scalaFieldSummary(fields []scalaDTOField) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, f.Name+":"+f.Type)
	}
	return strings.Join(parts, ",")
}

// scalaCodecAttribution maps each DTO type name to the JSON codec library that
// derives a codec for it (circe / play-json / zio-json / upickle). @JsonCodec
// is class-attached so it is resolved separately at field-parse time; the
// derive-by-type forms (deriveDecoder[Foo], Json.format[Foo]) name the DTO
// explicitly and are mapped here.
func scalaCodecAttribution(src string) map[string]string {
	out := map[string]string{}
	for _, m := range valCirceDeriveRe.FindAllStringSubmatch(src, -1) {
		out[m[1]] = "circe"
	}
	for _, m := range valPlayJsonFormatRe.FindAllStringSubmatch(src, -1) {
		out[m[1]] = "play-json"
	}
	for _, m := range valZioJsonDeriveRe.FindAllStringSubmatch(src, -1) {
		out[m[1]] = "zio-json"
	}
	// @JsonCodec annotates the immediately-following case class. Attribute it to
	// the next case class name appearing after each annotation site.
	if valCirceJsonCodecRe.MatchString(src) {
		for _, loc := range valCirceJsonCodecRe.FindAllStringIndex(src, -1) {
			rest := src[loc[1]:]
			if cm := valDTOCaseClassRe.FindStringSubmatchIndex(rest); cm != nil {
				dto := rest[cm[2]:cm[3]]
				if _, ok := out[dto]; !ok {
					out[dto] = "circe"
				}
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Request validation: refined / cats / accord / octopus
// ---------------------------------------------------------------------------

var (
	// refined predicate types attached to a field, e.g.
	//   name: String Refined NonEmpty
	//   age: Int Refined Positive
	//   email: String Refined MatchesRegex[...]
	//   port: Refined[Int, Interval.Closed[1024, 65535]]
	valRefinedInfixRe = regexp.MustCompile(
		`(?m)\b(\w+)\s*:\s*\w+\s+Refined\s+([A-Za-z][\w.]*(?:\s*\[[^\]]*\])?)`)
	valRefinedAppliedRe = regexp.MustCompile(
		`(?m)\b(\w+)\s*:\s*Refined\s*\[\s*[^,]+,\s*([^\]]+?)\s*\]`)

	// cats Validated / ValidatedNel field validators:
	//   def validateName(n: String): ValidatedNel[Error, String] = ...
	//   (validateName, validateAge).mapN(...)
	valCatsValidatedRe = regexp.MustCompile(
		`(?m)\bdef\s+(\w+)\s*\([^)]*\)\s*:\s*Validated(?:Nel|Nec)?\s*\[`)
	valCatsImportRe = regexp.MustCompile(`cats\.data\.Validated|cats\.syntax\.validated`)

	// accord:  validator[User] { user => user.name is notEmpty; user.age should be >= 18 }
	// We capture each `subject.field <constraint> <predicate>` clause.
	valAccordBlockRe  = regexp.MustCompile(`\bvalidator\s*\[\s*(\w+)\s*\]\s*\{`)
	valAccordClauseRe = regexp.MustCompile(
		`\b\w+\.(\w+)\s+(?:is|should|must)\s+(?:be\s+)?([A-Za-z][\w]*(?:\s*\([^)]*\))?(?:\s*[<>=!]+\s*[^;\n}]+)?)`)
	valAccordImportRe = regexp.MustCompile(`com\.wix\.accord`)

	// octopus:  implicit val v: Validator[User] = Validator[User].rule(_.email, ...)
	valOctopusRuleRe = regexp.MustCompile(
		`\.rule(?:Field)?\s*\(\s*[^,]*?\.(\w+)\s*,`)
	valOctopusImportRe = regexp.MustCompile(`octopus\b|import\s+octopus`)
)

// extractScalaValidation emits field-level request_validation entities for
// refined types, cats Validated validators, accord, and octopus. Each entity
// records the SPECIFIC field and the constraint/refinement applied.
func extractScalaValidation(src, framework string, file fileMeta) []types.EntityRecord {
	var out []types.EntityRecord

	// --- refined ---------------------------------------------------------
	for _, m := range valRefinedInfixRe.FindAllStringSubmatchIndex(src, -1) {
		field := src[m[2]:m[3]]
		pred := strings.TrimSpace(src[m[4]:m[5]])
		out = append(out, validationEntity(field, "refined", pred, framework, file, lineOf(src, m[0])))
	}
	for _, m := range valRefinedAppliedRe.FindAllStringSubmatchIndex(src, -1) {
		field := src[m[2]:m[3]]
		pred := strings.TrimSpace(src[m[4]:m[5]])
		out = append(out, validationEntity(field, "refined", pred, framework, file, lineOf(src, m[0])))
	}

	// --- cats Validated --------------------------------------------------
	if valCatsImportRe.MatchString(src) || valCatsValidatedRe.MatchString(src) {
		for _, m := range valCatsValidatedRe.FindAllStringSubmatchIndex(src, -1) {
			fn := src[m[2]:m[3]]
			// Field name = validator fn with a "validate" prefix stripped.
			field := strings.TrimPrefix(strings.TrimPrefix(fn, "validate"), "Validate")
			if field == "" {
				field = fn
			}
			field = lowerFirst(field)
			ent := validationEntity(field, "cats-validated", fn, framework, file, lineOf(src, m[0]))
			setProps(&ent, "validator_fn", fn)
			out = append(out, ent)
		}
	}

	// --- accord ----------------------------------------------------------
	if valAccordImportRe.MatchString(src) || valAccordBlockRe.MatchString(src) {
		for _, b := range valAccordBlockRe.FindAllStringSubmatchIndex(src, -1) {
			subject := src[b[2]:b[3]]
			body := accordBlockBody(src, b[1])
			for _, cm := range valAccordClauseRe.FindAllStringSubmatch(body, -1) {
				field := cm[1]
				constraint := strings.TrimSpace(collapseSpaces(cm[2]))
				ent := validationEntity(field, "accord", constraint, framework, file, lineOf(src, b[0]))
				setProps(&ent, "dto", subject)
				out = append(out, ent)
			}
		}
	}

	// --- octopus ---------------------------------------------------------
	if valOctopusImportRe.MatchString(src) {
		for _, m := range valOctopusRuleRe.FindAllStringSubmatchIndex(src, -1) {
			field := src[m[2]:m[3]]
			out = append(out, validationEntity(field, "octopus", "rule", framework, file, lineOf(src, m[0])))
		}
	}

	return out
}

// validationEntity builds a request_validation pattern entity carrying the
// specific field and the constraint/refinement applied to it.
func validationEntity(field, library, constraint, framework string, file fileMeta, line int) types.EntityRecord {
	ent := makeEntity("validate:"+library+":"+field, "SCOPE.Operation", "request_validation",
		file.Path, file.Language, line)
	setProps(&ent, "framework", framework, "provenance", "FIELD_VALIDATION",
		"library", library, "field", field, "constraint", constraint)
	return ent
}

// balancedParens returns the substring from open (the byte just past an opening
// "(") up to its matching ")", respecting nested parens. Used to capture a case
// class primary-constructor parameter list that may contain parens in field
// annotations or default values.
func balancedParens(src string, open int) string {
	depth := 1
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[open:i]
			}
		}
	}
	return src[open:]
}

// accordBlockBody returns the brace-balanced body of an accord validator block
// starting at byte offset open (just past the opening "{").
func accordBlockBody(src string, open int) string {
	depth := 1
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open:i]
			}
		}
	}
	return src[open:]
}

// ---------------------------------------------------------------------------
// small local helpers (namespaced — no collisions with frameworks.go)
// ---------------------------------------------------------------------------

// fileMeta carries the path/language fields the validation extractors need,
// decoupling them from the extractor.FileInput type.
type fileMeta struct {
	Path     string
	Language string
}

// splitTopLevelCommas splits on commas that are not nested inside [] or ().
func splitTopLevelCommas(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[', '(':
			depth++
		case ']', ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] = r[0] - 'A' + 'a'
	}
	return string(r)
}

func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
