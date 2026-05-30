package rust

// fw_validation.go — DTO + request validation extractors for Rust HTTP frameworks.
//
// Covers dto_extraction and request_validation for:
//   - actix, axum, gotham, hyper, poem, rocket, salvo, tide, tower, warp
//
// Detection surface:
//   dto_extraction:
//     - #[derive(Deserialize)] structs (serde — the lingua franca for HTTP DTOs)
//     - #[derive(Validate)] structs (validator crate)
//     - #[derive(serde::Deserialize)] full-path variant
//
//   request_validation:
//     - #[validate(...)] field attributes (validator crate field-level rules)
//     - validator::Validate usage (trait impl or .validate() call)
//     - actix: web::Json<T>, web::Query<T>, web::Form<T> extractor types
//     - axum: Json<T>, Query<T>, Form<T>, Path<T> extractor types
//     - rocket: Json<T>, Form<T>, request guard impls (impl FromRequest)
//     - poem: #[handler] params that are Deserialize types
//     - tower/hyper: body deserialization via serde_json::from_slice / from_str
//     - salvo: #[extract] / Extractible derive
//     - gotham/tide/warp: Json body extraction primitives
//
// Honesty:
//
//	partial — heuristic regex match on source text. We detect structures likely
//	used as request DTOs but cannot prove type-system flow. Fixtures prove the
//	detection surface. Full semantic confirmation requires import-graph analysis.
//
// Issue #3267 — lang.rust.framework.* Validation cells.

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_rust_validation", &rustValidationExtractor{})
}

type rustValidationExtractor struct{}

// itoa is a local int→string helper to avoid pulling fmt for one conversion.
func itoa(n int) string { return strconv.Itoa(n) }

func (e *rustValidationExtractor) Language() string { return "custom_rust_validation" }

// ---------------------------------------------------------------------------
// Shared regex catalog
// ---------------------------------------------------------------------------

var (
	// #[derive(...Deserialize...)] — serde DTO structs
	reDeserialize = regexp.MustCompile(
		`#\[derive\([^)]*\b(?:Deserialize|serde::Deserialize)\b[^)]*\)\]`,
	)

	// #[derive(...Validate...)] — validator crate
	reValidateDerive = regexp.MustCompile(
		`#\[derive\([^)]*\bValidate\b[^)]*\)\]`,
	)

	// derive list for attribute extraction
	reAnyDerive = regexp.MustCompile(`#\[derive\(([^)]+)\)\]`)

	// struct Name after a derive
	reStructNameV = regexp.MustCompile(`\bstruct\s+(\w+)`)

	// #[validate(length(min=1), email)] field rules
	reValidateAttr = regexp.MustCompile(
		`#\[validate\s*\([^\)]*\)\]`,
	)

	// .validate() call (validator crate trait method)
	reValidateCall = regexp.MustCompile(
		`\.validate\s*\(\s*\)`,
	)

	// actix: web::Json<T>, web::Query<T>, web::Form<T>, web::Path<T>
	reActixExtractorV = regexp.MustCompile(
		`web::(Json|Query|Form|Path)\s*<\s*([A-Za-z_]\w*)`,
	)

	// axum: Json<T>, Query<T>, Form<T>, Path<T> (not prefixed web::)
	reAxumExtractorV = regexp.MustCompile(
		`(?:^|[^::\w])(Json|Query|Form|Path)\s*<\s*([A-Za-z_]\w*)`,
	)

	// rocket: Json<T>, Form<T>, MsgPack<T>
	reRocketExtractorV = regexp.MustCompile(
		`(?:Json|Form|MsgPack|FromForm)\s*<\s*([A-Za-z_]\w*)`,
	)

	// rocket: impl FromRequest for T (request guard)
	reRocketFromRequest = regexp.MustCompile(
		`impl\s+(?:<[^>]*>\s+)?FromRequest(?:<[^>]*>)?\s+for\s+(\w+)`,
	)

	// salvo: #[extract] on a struct field, or Extractible derive
	reSalvoExtractible = regexp.MustCompile(
		`#\[derive\([^)]*\bExtractible\b[^)]*\)\]`,
	)

	// salvo: #[salvo(extract(...))]
	reSalvoExtractAttr = regexp.MustCompile(
		`#\[salvo\s*\(\s*extract`,
	)

	// poem: #[oai(validator(...))]  or poem_openapi derive with Validate
	rePoemOAIValidator = regexp.MustCompile(
		`#\[oai\s*\([^\)]*validator`,
	)

	// serde_json::from_slice / from_str — body deserialization (hyper, tower, raw)
	reSerdeJsonDeser = regexp.MustCompile(
		`serde_json::from_(?:slice|str|reader|value)\s*[:<]`,
	)

	// gotham: extract body / serde-based patterns
	reGothamJsonBody = regexp.MustCompile(
		`Body\s*::\s*to_bytes|serde_json::from_slice`,
	)

	// tide: req.body_json::<T>()
	reTideBodyJson = regexp.MustCompile(
		`req\.body_json\s*::\s*<([A-Za-z_]\w*)>`,
	)

	// warp: warp::body::json() (filter combinator)
	reWarpBodyJson = regexp.MustCompile(
		`warp::body::json\s*\(\s*\)`,
	)

	// tower/hyper: hyper::body::to_bytes / body deserialization
	reHyperBodyDeser = regexp.MustCompile(
		`hyper::body::to_bytes|body::to_bytes|serde_json::from_slice`,
	)

	// ---- deep field/constraint parsing (#3413) ----

	// container-level #[serde(rename_all = "camelCase")]
	reSerdeRenameAll = regexp.MustCompile(
		`#\[serde\s*\([^)]*\brename_all\s*=\s*"([^"]+)"`,
	)

	// field-level #[serde(rename = "x")]
	reSerdeFieldRename = regexp.MustCompile(
		`#\[serde\s*\([^)]*\brename\s*=\s*"([^"]+)"`,
	)

	// field-level serde flags: default / skip / flatten
	reSerdeDefault = regexp.MustCompile(`#\[serde\s*\([^)]*\bdefault\b`)
	reSerdeSkip    = regexp.MustCompile(`#\[serde\s*\([^)]*\bskip(?:_serializing|_deserializing)?\b`)
	reSerdeFlatten = regexp.MustCompile(`#\[serde\s*\([^)]*\bflatten\b`)

	// a single struct field:  [pub] name: Type,
	// captures (1) field name, (2) type up to the line/field terminator.
	reStructField = regexp.MustCompile(
		`(?m)^\s*(?:pub(?:\s*\([^)]*\))?\s+)?([a-z_]\w*)\s*:\s*([^,\n}]+)`,
	)

	// each #[validate(...)] attribute, capturing the inner rule list.
	reValidateInner = regexp.MustCompile(
		`#\[validate\s*\(([^\]]*)\)\]`,
	)
)

// ---------------------------------------------------------------------------
// Deep struct-body parsing: fields, serde attrs, validator constraints (#3413)
// ---------------------------------------------------------------------------

// dtoField is one parsed struct field with its serde + validator metadata.
type dtoField struct {
	name        string
	typ         string
	serdeRename string // explicit #[serde(rename="...")]
	wireName    string // effective JSON key after rename / rename_all
	hasDefault  bool
	skip        bool
	flatten     bool
	constraints []valConstraint
	offset      int // absolute offset of the field within src
}

// valConstraint is one validator-crate rule with its specific bound(s).
type valConstraint struct {
	kind  string // length | email | range | url | regex | custom | contains | must_match | nested | phone | credit_card | non_control_character | ip
	min   string // for length/range
	max   string // for length/range
	value string // regex path, custom fn, contains needle, must_match peer, etc.
}

// rustStructBody returns the brace-delimited body of the struct that begins at
// or after startOff, plus the absolute offset where that body starts. Returns
// ok=false if no balanced body is found.
func rustStructBody(src string, startOff int) (body string, bodyStart int, ok bool) {
	open := strings.IndexByte(src[startOff:], '{')
	if open < 0 {
		return "", 0, false
	}
	open += startOff
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open+1 : i], open + 1, true
			}
		}
	}
	return "", 0, false
}

// applyRenameAll converts a Rust field name per a serde rename_all convention.
func applyRenameAll(name, convention string) string {
	switch convention {
	case "camelCase":
		return snakeToCamel(name, false)
	case "PascalCase":
		return snakeToCamel(name, true)
	case "SCREAMING_SNAKE_CASE":
		return strings.ToUpper(name)
	case "kebab-case":
		return strings.ReplaceAll(name, "_", "-")
	case "SCREAMING-KEBAB-CASE":
		return strings.ToUpper(strings.ReplaceAll(name, "_", "-"))
	case "lowercase":
		return strings.ToLower(strings.ReplaceAll(name, "_", ""))
	case "UPPERCASE":
		return strings.ToUpper(strings.ReplaceAll(name, "_", ""))
	case "snake_case":
		return name
	default:
		return name
	}
}

// snakeToCamel converts snake_case to camelCase (upperFirst=false) or
// PascalCase (upperFirst=true).
func snakeToCamel(s string, upperFirst bool) string {
	parts := strings.Split(s, "_")
	var b strings.Builder
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i == 0 && !upperFirst {
			b.WriteString(p)
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	return b.String()
}

// parseValidateRules parses the inner text of a single #[validate(...)] attr
// into zero or more typed constraints. Handles:
//
//	length(min = 1, max = 20)   range(min = 0.0, max = 100.0)
//	email   url   phone   credit_card   non_control_character
//	regex = "PATH" / regex(path = "PATH")
//	custom = "fn" / custom(function = "fn")
//	contains = "x" / contains(pattern = "x")
//	must_match = "other" / must_match(other = "field")
//	nested  (bare — recurse into the field's own type)
func parseValidateRules(inner string) []valConstraint {
	var out []valConstraint
	for _, rule := range splitTopLevel(inner) {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}
		head := rule
		var args string
		if p := strings.IndexByte(rule, '('); p >= 0 {
			head = strings.TrimSpace(rule[:p])
			if c := strings.LastIndexByte(rule, ')'); c > p {
				args = rule[p+1 : c]
			}
		} else if eq := strings.IndexByte(rule, '='); eq >= 0 {
			// keyword form: regex = "...", custom = "...", contains = "..."
			head = strings.TrimSpace(rule[:eq])
			args = strings.TrimSpace(rule[eq+1:])
		}
		c := valConstraint{kind: head}
		switch head {
		case "length", "range":
			c.min = kwArg(args, "min")
			c.max = kwArg(args, "max")
		case "regex":
			c.value = firstStringArg(args, "path")
		case "custom":
			c.value = firstStringArg(args, "function")
		case "contains":
			c.value = firstStringArg(args, "pattern")
		case "must_match":
			c.value = firstStringArg(args, "other")
		case "email", "url", "phone", "credit_card",
			"non_control_character", "ip", "nested", "required":
			// no bound
		default:
			// unknown rule kind — still record it by name
		}
		out = append(out, c)
	}
	return out
}

// splitTopLevel splits a comma-separated rule list, ignoring commas nested
// inside parentheses (e.g. length(min = 1, max = 20)).
func splitTopLevel(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// kwArg extracts an unquoted keyword argument value: kwArg("min = 1, max = 20",
// "max") == "20". Returns "" when absent.
func kwArg(args, key string) string {
	for _, part := range strings.Split(args, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, key) {
			rest := strings.TrimSpace(part[len(key):])
			if strings.HasPrefix(rest, "=") {
				return strings.TrimSpace(strings.Trim(strings.TrimSpace(rest[1:]), `"`))
			}
		}
	}
	return ""
}

// firstStringArg returns the value of a quoted argument. It accepts both the
// bare form (`"foo"`) and the keyword form (`path = "foo"`). key names the
// keyword for the keyword form.
func firstStringArg(args, key string) string {
	args = strings.TrimSpace(args)
	// keyword form: key = "value"
	if v := kwArg(args, key); v != "" {
		return v
	}
	// bare quoted form
	if i := strings.IndexByte(args, '"'); i >= 0 {
		if j := strings.IndexByte(args[i+1:], '"'); j >= 0 {
			return args[i+1 : i+1+j]
		}
	}
	// bare identifier form: custom = my_fn (no quotes) — args already stripped
	return strings.Trim(args, `"`)
}

// parseDTOFields parses one struct body into ordered fields with their serde
// and validator metadata. renameAll is the container-level rename_all value
// ("" when absent). bodyStart is the absolute offset of the body in src (for
// line numbers).
func parseDTOFields(body string, bodyStart int, renameAll string) []dtoField {
	var out []dtoField

	// We walk field declarations. For each field we look BACK across the
	// preceding attribute lines (which sit between the previous field end and
	// this field's name) to collect its #[serde(...)] / #[validate(...)] attrs.
	matches := reStructField.FindAllStringSubmatchIndex(body, -1)
	prevEnd := 0
	for _, m := range matches {
		name := body[m[2]:m[3]]
		typ := strings.TrimSpace(body[m[4]:m[5]])
		// Skip Rust keywords that the field regex may catch (e.g. `where`).
		if name == "where" || name == "impl" || name == "fn" {
			prevEnd = m[1]
			continue
		}
		// Attribute window: from end of previous field to start of this one.
		attrWin := body[prevEnd:m[0]]
		prevEnd = m[1]

		f := dtoField{name: name, typ: typ, offset: bodyStart + m[0]}

		if rm := reSerdeFieldRename.FindStringSubmatch(attrWin); rm != nil {
			f.serdeRename = rm[1]
		}
		if reSerdeDefault.MatchString(attrWin) {
			f.hasDefault = true
		}
		if reSerdeSkip.MatchString(attrWin) {
			f.skip = true
		}
		if reSerdeFlatten.MatchString(attrWin) {
			f.flatten = true
		}

		switch {
		case f.serdeRename != "":
			f.wireName = f.serdeRename
		case renameAll != "":
			f.wireName = applyRenameAll(name, renameAll)
		default:
			f.wireName = name
		}

		for _, vm := range reValidateInner.FindAllStringSubmatch(attrWin, -1) {
			f.constraints = append(f.constraints, parseValidateRules(vm[1])...)
		}

		out = append(out, f)
	}
	return out
}

// dtoStructsFromSrc scans src for Deserialize or Validate derives and returns
// the struct names, derive list, and source offset for each.
type dtoInfo struct {
	structName  string
	deriveList  string
	offset      int
	hasValidate bool
	hasDeser    bool
	fields      []dtoField // deep-parsed fields (#3413)
}

func extractDTOs(src string) []dtoInfo {
	var out []dtoInfo
	seen := make(map[string]bool)

	// Combine: any derive that includes Deserialize OR Validate
	allDerive := reAnyDerive.FindAllStringSubmatchIndex(src, -1)
	for _, dm := range allDerive {
		attrText := src[dm[0]:dm[1]]
		deriveList := src[dm[2]:dm[3]]
		isDeser := strings.Contains(deriveList, "Deserialize") ||
			strings.Contains(deriveList, "Serialize")
		isVal := strings.Contains(deriveList, "Validate")
		if !isDeser && !isVal {
			continue
		}
		// Look ahead for struct name
		tail := src[dm[1]:]
		if len(tail) > 500 {
			tail = tail[:500]
		}
		sm := reStructNameV.FindStringSubmatchIndex(tail)
		if sm == nil {
			continue
		}
		sname := tail[sm[2]:sm[3]]
		if seen[sname] {
			continue
		}
		seen[sname] = true

		// Container-level #[serde(rename_all = "...")] lives in the attribute
		// block between this derive and the struct keyword.
		var renameAll string
		structKwOff := dm[1] + sm[0]
		if structKwOff <= len(src) {
			containerAttrs := src[dm[0]:structKwOff]
			if ra := reSerdeRenameAll.FindStringSubmatch(containerAttrs); ra != nil {
				renameAll = ra[1]
			}
		}

		// Deep-parse the struct body for fields + constraints.
		var fields []dtoField
		if body, bodyStart, ok := rustStructBody(src, structKwOff); ok {
			fields = parseDTOFields(body, bodyStart, renameAll)
		}

		out = append(out, dtoInfo{
			structName:  sname,
			deriveList:  attrText,
			offset:      dm[0],
			hasValidate: isVal,
			hasDeser:    isDeser,
			fields:      fields,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *rustValidationExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_validation_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// -----------------------------------------------------------------------
	// 1. dto_extraction — Deserialize/Validate derived structs
	// -----------------------------------------------------------------------
	for _, dto := range extractDTOs(src) {
		subtype := "dto"
		if dto.hasValidate {
			subtype = "validated_dto"
		}
		ent := makeEntity("dto:"+dto.structName, "SCOPE.Schema", subtype,
			file.Path, file.Language, lineOf(src, dto.offset))
		setProps(&ent,
			"provenance", "INFERRED_FROM_SERDE_DESERIALIZE",
			"struct_name", dto.structName,
			"field_count", itoa(len(dto.fields)),
		)
		add(ent)

		// --- deep field + constraint entities (#3413) ---
		for _, f := range dto.fields {
			fieldEnt := makeEntity(
				"dto_field:"+dto.structName+"."+f.name,
				"SCOPE.Schema", "dto_field",
				file.Path, file.Language, lineOf(src, f.offset))
			setProps(&fieldEnt,
				"provenance", "INFERRED_FROM_SERDE_FIELD",
				"struct_name", dto.structName,
				"field_name", f.name,
				"field_type", f.typ,
				"wire_name", f.wireName,
			)
			if f.serdeRename != "" {
				setProps(&fieldEnt, "serde_rename", f.serdeRename)
			}
			if f.hasDefault {
				setProps(&fieldEnt, "serde_default", "true")
			}
			if f.skip {
				setProps(&fieldEnt, "serde_skip", "true")
			}
			if f.flatten {
				setProps(&fieldEnt, "serde_flatten", "true")
			}
			add(fieldEnt)

			// One SCOPE.Constraint per validator-crate rule on this field,
			// carrying the SPECIFIC field + constraint kind + bound(s).
			for _, c := range f.constraints {
				cName := "validation:" + dto.structName + "." + f.name + ":" + c.kind
				cEnt := makeEntity(cName, "SCOPE.Constraint", "field_constraint",
					file.Path, file.Language, lineOf(src, f.offset))
				setProps(&cEnt,
					"provenance", "INFERRED_FROM_VALIDATE_ATTR",
					"struct_name", dto.structName,
					"field_name", f.name,
					"constraint_kind", c.kind,
				)
				if c.min != "" {
					setProps(&cEnt, "min", c.min)
				}
				if c.max != "" {
					setProps(&cEnt, "max", c.max)
				}
				if c.value != "" {
					setProps(&cEnt, "value", c.value)
				}
				add(cEnt)
			}
		}
	}

	// -----------------------------------------------------------------------
	// 2. request_validation — #[validate(...)] field attributes
	// -----------------------------------------------------------------------
	for _, m := range reValidateAttr.FindAllStringIndex(src, -1) {
		ent := makeEntity("validate_field_attr", "SCOPE.Pattern", "field_validation",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "provenance", "INFERRED_FROM_VALIDATE_ATTR")
		add(ent)
	}

	// .validate() call
	for _, m := range reValidateCall.FindAllStringIndex(src, -1) {
		ent := makeEntity("validate_call", "SCOPE.Pattern", "request_validation",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "provenance", "INFERRED_FROM_VALIDATE_CALL")
		add(ent)
	}

	// -----------------------------------------------------------------------
	// 3. Framework-specific extractor types
	// -----------------------------------------------------------------------

	// actix: web::Json<T> / web::Query<T> / web::Form<T> / web::Path<T>
	for _, m := range reActixExtractorV.FindAllStringSubmatchIndex(src, -1) {
		extKind := src[m[2]:m[3]]
		typeParam := src[m[4]:m[5]]
		name := "actix_extractor:" + extKind + "<" + typeParam + ">"
		ent := makeEntity(name, "SCOPE.Schema", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "actix_web",
			"extractor_kind", extKind,
			"type_param", typeParam,
			"provenance", "INFERRED_FROM_ACTIX_EXTRACTOR",
		)
		add(ent)
	}

	// axum: Json<T>, Query<T>, Form<T>, Path<T> — avoid double-counting web:: prefix
	for _, m := range reAxumExtractorV.FindAllStringSubmatchIndex(src, -1) {
		// Skip actix web:: prefix variants
		if m[0] >= 5 && src[m[0]:m[0]+5] == "web::" {
			continue
		}
		extKind := src[m[2]:m[3]]
		typeParam := src[m[4]:m[5]]
		name := "axum_extractor:" + extKind + "<" + typeParam + ">"
		ent := makeEntity(name, "SCOPE.Schema", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "axum",
			"extractor_kind", extKind,
			"type_param", typeParam,
			"provenance", "INFERRED_FROM_AXUM_EXTRACTOR",
		)
		add(ent)
	}

	// rocket: Json<T>, Form<T>, MsgPack<T>, FromForm<T>
	for _, m := range reRocketExtractorV.FindAllStringSubmatchIndex(src, -1) {
		typeParam := src[m[2]:m[3]]
		name := "rocket_extractor:" + typeParam
		ent := makeEntity(name, "SCOPE.Schema", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "rocket",
			"type_param", typeParam,
			"provenance", "INFERRED_FROM_ROCKET_EXTRACTOR",
		)
		add(ent)
	}

	// rocket: impl FromRequest for T (request guard — validation hook)
	for _, m := range reRocketFromRequest.FindAllStringSubmatchIndex(src, -1) {
		guardType := src[m[2]:m[3]]
		ent := makeEntity("rocket_guard:"+guardType, "SCOPE.Pattern", "request_guard",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "rocket",
			"guard_type", guardType,
			"provenance", "INFERRED_FROM_ROCKET_FROM_REQUEST",
		)
		add(ent)
	}

	// salvo: Extractible derive
	for _, m := range reSalvoExtractible.FindAllStringIndex(src, -1) {
		tail := src[m[1]:]
		if len(tail) > 300 {
			tail = tail[:300]
		}
		sm := reStructNameV.FindStringSubmatchIndex(tail)
		if sm == nil {
			continue
		}
		sname := tail[sm[2]:sm[3]]
		ent := makeEntity("salvo_extractible:"+sname, "SCOPE.Schema", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "salvo",
			"struct_name", sname,
			"provenance", "INFERRED_FROM_SALVO_EXTRACTIBLE",
		)
		add(ent)
	}

	// salvo: #[salvo(extract(...))]
	for _, m := range reSalvoExtractAttr.FindAllStringIndex(src, -1) {
		ent := makeEntity("salvo_extract_attr", "SCOPE.Pattern", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "salvo",
			"provenance", "INFERRED_FROM_SALVO_EXTRACT_ATTR",
		)
		add(ent)
	}

	// poem: #[oai(validator(...))]
	for _, m := range rePoemOAIValidator.FindAllStringIndex(src, -1) {
		ent := makeEntity("poem_oai_validator", "SCOPE.Pattern", "request_validation",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "poem",
			"provenance", "INFERRED_FROM_POEM_OAI_VALIDATOR",
		)
		add(ent)
	}

	// tide: req.body_json::<T>()
	for _, m := range reTideBodyJson.FindAllStringSubmatchIndex(src, -1) {
		typeParam := src[m[2]:m[3]]
		ent := makeEntity("tide_body_json:"+typeParam, "SCOPE.Schema", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "tide",
			"type_param", typeParam,
			"provenance", "INFERRED_FROM_TIDE_BODY_JSON",
		)
		add(ent)
	}

	// warp: warp::body::json()
	for _, m := range reWarpBodyJson.FindAllStringIndex(src, -1) {
		ent := makeEntity("warp_body_json", "SCOPE.Pattern", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "warp",
			"provenance", "INFERRED_FROM_WARP_BODY_JSON",
		)
		add(ent)
	}

	// hyper/tower: serde_json::from_slice body deserialization
	for _, m := range reHyperBodyDeser.FindAllStringIndex(src, -1) {
		ent := makeEntity("hyper_body_deser", "SCOPE.Pattern", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "hyper",
			"provenance", "INFERRED_FROM_HYPER_BODY_DESER",
		)
		add(ent)
	}

	// generic: serde_json deserialization (gotham, tower, etc.)
	for _, m := range reSerdeJsonDeser.FindAllStringIndex(src, -1) {
		ent := makeEntity("serde_json_deser", "SCOPE.Pattern", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"provenance", "INFERRED_FROM_SERDE_JSON_DESER",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
