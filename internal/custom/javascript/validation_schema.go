package javascript

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_validation_schema", &validationSchemaExtractor{})
}

// validationSchemaExtractor extracts request/response validation-schema
// contracts for the JS/TS runtime-validation libraries that define an API's
// shape — Zod, Joi, Yup, and class-validator — and binds each schema to the
// route handler that consumes it via the same ACCEPTS_INPUT / RETURNS contract
// used by the FastAPI / Flask / NestJS / Express DTO emitters (waves 2/5,
// #3629/#3607).
//
// For each detected schema it emits a `SCOPE.Schema` entity carrying the
// captured scalar field names + types as `field_<name>` properties (plus a
// stable `fields` summary), and — when a route handler concretely references
// the schema — an ACCEPTS_INPUT edge endpoint -> `Schema:<name>` (request body)
// or a RETURNS edge (response body). Dynamic / computed schemas and schemas
// never referenced by a route yield a schema entity but no endpoint edge
// (honest-partial: no false bindings).
type validationSchemaExtractor struct{}

func (e *validationSchemaExtractor) Language() string { return "custom_js_validation_schema" }

var (
	// zodSchemaRe: `const CreateUser = z.object({ ... })`. Captures var name and
	// the (possibly chained) initializer up to end of statement.
	zodSchemaRe = regexp.MustCompile(
		`(?m)(?:const|let|var)\s+([A-Za-z_]\w*)\s*(?::\s*[\w.<>\[\] ]+)?\s*=\s*(z|zod)\s*\.\s*object\s*\(`)
	// joiSchemaRe: `const CreateUser = Joi.object({ ... })`.
	joiSchemaRe = regexp.MustCompile(
		`(?m)(?:const|let|var)\s+([A-Za-z_]\w*)\s*(?::\s*[\w.<>\[\] ]+)?\s*=\s*(Joi|joi)\s*\.\s*object\s*\(`)
	// yupSchemaRe: `const CreateUser = yup.object().shape({ ... })` or
	// `yup.object({ ... })`.
	yupSchemaRe = regexp.MustCompile(
		`(?m)(?:const|let|var)\s+([A-Za-z_]\w*)\s*(?::\s*[\w.<>\[\] ]+)?\s*=\s*(yup|Yup)\s*\.\s*object\s*\(\s*\)?\s*(?:\.\s*shape\s*\()?`)

	// classValidatorDtoRe: `class CreateUserDto { ... }` / `export class X {`.
	classValidatorDtoRe = regexp.MustCompile(
		`(?m)(?:export\s+)?(?:abstract\s+)?class\s+([A-Za-z_]\w*)\s*(?:extends\s+[\w.<>, ]+?)?\s*\{`)

	// zod field: `name: z.string()`, `age: z.number().int()`.
	zodFieldRe = regexp.MustCompile(`(?m)([A-Za-z_]\w*)\s*:\s*(?:z|zod)\s*\.\s*([A-Za-z_]\w*)\s*\(`)
	// joi field: `name: Joi.string()`.
	joiFieldRe = regexp.MustCompile(`(?m)([A-Za-z_]\w*)\s*:\s*(?:Joi|joi)\s*\.\s*([A-Za-z_]\w*)\s*\(`)
	// yup field: `name: yup.string()`.
	yupFieldRe = regexp.MustCompile(`(?m)([A-Za-z_]\w*)\s*:\s*(?:yup|Yup)\s*\.\s*([A-Za-z_]\w*)\s*\(`)
	// cvNestedTypeRe: a class-transformer `@Type(() => TargetClass)` thunk,
	// optionally preceded/followed by other decorators, bound to a property name.
	// Group 1 = target class, group 2 = field name. Issue #4328.
	cvNestedTypeRe = regexp.MustCompile(
		`(?m)@Type\s*\(\s*\(\s*\)\s*=>\s*([A-Z][A-Za-z0-9_]*)\s*\)` +
			`(?:\s*@\w+\s*(?:\([^)]*\))?\s*)*\s*` +
			`([A-Za-z_]\w*)\s*[!?]?\s*:`)

	// route header (express/fastify/koa/nest-style decorator or method call).
	vsRouteCallRe = regexp.MustCompile(
		`(?i)(?:app|router|fastify|server|\w+)\s*\.\s*(get|post|put|delete|patch|all|options|head)\s*\(\s*['` + "`" + `"]([^'"` + "`" + ` ]+)['` + "`" + `"]`)
)

// zodScalarKind maps a zod/joi/yup factory method to a normalized scalar type.
// Unknown factories pass through as-is (lowercased) so e.g. a custom `.uuid()`
// is still recorded as "uuid".
var schemaScalarKind = map[string]string{
	"string": "string", "number": "number", "boolean": "boolean", "bool": "boolean",
	"int": "integer", "integer": "integer", "float": "number", "date": "date",
	"datetime": "date", "bigint": "bigint", "array": "array", "object": "object",
	"enum": "enum", "literal": "string", "email": "string", "uuid": "string",
	"any": "any", "unknown": "any", "mixed": "any", "null": "null",
}

// cvDecoratorType maps a class-validator decorator to a scalar type when no TS
// annotation is present.
var cvDecoratorType = map[string]string{
	"IsString": "string", "IsInt": "integer", "IsNumber": "number",
	"IsBoolean": "boolean", "IsDate": "date", "IsEmail": "string",
	"IsUUID": "string", "IsArray": "array", "IsObject": "object",
	"IsEnum": "enum", "IsPositive": "number", "IsNegative": "number",
}

func (e *validationSchemaExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.js_validation_schema")
	_, span := tracer.Start(ctx, "custom.js_validation_schema")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	lang := strings.ToLower(file.Language)
	if lang != "typescript" && lang != "javascript" {
		return nil, nil
	}
	src := string(file.Content)

	var out []types.EntityRecord
	seen := make(map[string]bool)
	// schemaNames tracks every emitted schema variable/class name so route
	// binding only references concretely-defined schemas.
	schemaNames := make(map[string]bool)

	addSchema := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		schemaNames[ent.Name] = true
		out = append(out, ent)
	}

	// 1. Zod / Joi / Yup object schemas.
	for _, sd := range []struct {
		hdr   *regexp.Regexp
		field *regexp.Regexp
		lib   string
	}{
		{zodSchemaRe, zodFieldRe, "zod"},
		{joiSchemaRe, joiFieldRe, "joi"},
		{yupSchemaRe, yupFieldRe, "yup"},
	} {
		for _, m := range sd.hdr.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			openParen := m[1] - 1 // header ends at the `(` of `.object(`
			body := balancedObjectBody(src, openParen)
			fields := extractSchemaFields(sd.field, body)
			if len(fields) == 0 {
				// A schema with no statically-detectable scalar fields (dynamic /
				// computed). Still emit the schema entity, but it carries no
				// field props — honest-partial.
			}
			ent := makeEntity(name, "SCOPE.Schema", "validation_schema", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "library", sd.lib, "pattern_type", "validation_schema",
				"provenance", "INFERRED_FROM_"+strings.ToUpper(sd.lib)+"_OBJECT")
			applyFieldProps(&ent, fields)
			// Field-membership sub-entities (issue #4606): emit before addSchema
			// so the CONTAINS edges are recorded on the owner.
			children := emitSchemaFieldMembers(&ent, fields, sd.lib, file.Path, file.Language)
			addSchema(ent)
			for _, c := range children {
				addSchema(c)
			}
		}
	}

	// 2. class-validator DTO classes (TypeScript only — decorators).
	if lang == "typescript" {
		for _, m := range classValidatorDtoRe.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			openBrace := m[1] - 1
			body := balancedBraceBody(src, openBrace)
			fields := extractClassValidatorFields(body)
			if len(fields) == 0 {
				// Not a class-validator DTO (no recognised decorators) — skip so
				// we don't emit a schema entity for every plain class.
				continue
			}
			ent := makeEntity(name, "SCOPE.Schema", "validation_schema", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "library", "class-validator", "pattern_type", "validation_schema",
				"provenance", "INFERRED_FROM_CLASS_VALIDATOR")
			applyFieldProps(&ent, fields)
			// Field-membership sub-entities (issue #4606): request `@Body` DTOs and
			// `@Query` object DTOs now expand to their fields, parity with response
			// Schema fields. Emitted before the nested-target edges so all
			// relationships land on the same owner.
			cvChildren := emitSchemaFieldMembers(&ent, fields, "class-validator", file.Path, file.Language)
			// Nested-DTO target edges (issue #4328): `@ValidateNested()
			// @Type(() => AddressDto)` carries a class target in a class-transformer
			// thunk. Without an outbound edge the nested DTO rings. Emit a REFERENCES
			// edge from this DTO to each nested target class so the membership /
			// type topology is preserved and the nested DTO is no longer an orphan.
			for _, tgt := range extractClassValidatorNestedTargets(body) {
				ent.Relationships = append(ent.Relationships,
					referencesClassEdge(ent.ID, tgt.target, "class-validator", tgt.field))
			}
			addSchema(ent)
			for _, c := range cvChildren {
				addSchema(c)
			}
		}
	}

	if len(schemaNames) == 0 {
		span.SetAttributes(attribute.Int("entity_count", len(out)))
		return out, nil
	}

	// 3. Bind schemas to routes that concretely reference them.
	out = append(out, e.bindRoutes(src, file, schemaNames)...)

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// bindRoutes scans each route handler for a concrete reference to one of the
// known schemas and emits ACCEPTS_INPUT (request) / RETURNS (response) edges
// from the route's endpoint entity to `Schema:<name>`.
func (e *validationSchemaExtractor) bindRoutes(src string, file extreg.FileInput, schemaNames map[string]bool) []types.EntityRecord {
	var out []types.EntityRecord
	seen := make(map[string]bool)

	routes := vsRouteCallRe.FindAllStringSubmatchIndex(src, -1)
	for ri, m := range routes {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := src[m[4]:m[5]]
		// Handler region: from this route to the next route header (or EOF).
		regionEnd := len(src)
		if ri+1 < len(routes) {
			regionEnd = routes[ri+1][0]
		}
		region := src[m[0]:regionEnd]

		acc := detectRequestSchemas(region, schemaNames)
		ret := detectResponseSchemas(region, schemaNames)
		if len(acc) == 0 && len(ret) == 0 {
			continue
		}

		epName := method + " " + path
		ent := makeEntity(epName, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "js_http", "http_method", method, "route_path", path,
			"pattern_type", "validation_schema_binding")

		for _, s := range acc {
			key := epName + ":ACCEPTS_INPUT:" + s.name
			if seen[key] {
				continue
			}
			seen[key] = true
			ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
				ToID: "Schema:" + s.name,
				Kind: string(types.RelationshipKindAcceptsInput),
				Properties: map[string]string{
					"match_source": s.source, "schema_type": s.name, "via": "validation_schema",
				},
			})
		}
		for _, s := range ret {
			key := epName + ":RETURNS:" + s.name
			if seen[key] {
				continue
			}
			seen[key] = true
			ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
				ToID: "Schema:" + s.name,
				Kind: string(types.RelationshipKindReturns),
				Properties: map[string]string{
					"match_source": s.source, "schema_type": s.name, "via": "validation_schema",
				},
			})
		}
		if len(ent.Relationships) > 0 {
			out = append(out, ent)
		}
	}
	return out
}

type schemaRef struct {
	name   string
	source string
}

var (
	// `CreateUser.parse(req.body)` / `.safeParse(req.body)` (zod), or
	// `CreateUser.validate(req.body)` (joi/yup), or `.load(...)`.
	reqParseRe = regexp.MustCompile(
		`([A-Za-z_]\w*)\s*\.\s*(parse|parseAsync|safeParse|safeParseAsync|validate|validateAsync|validateSync)\s*\(\s*req\s*\.\s*body`)
	// middleware form: `validate(CreateUser)` / `zodValidator(CreateUser)` /
	// `validateBody(CreateUser)` / `validateRequest(CreateUser)`.
	reqMiddlewareRe = regexp.MustCompile(
		`(?:validate|validateBody|validateRequest|zodValidator|validator|schemaValidator|celebrate)\s*\(\s*(?:\{[^}]*body\s*:\s*)?([A-Za-z_]\w*)`)
	// response: `Schema.parse(result)` returned, or `res.json(Schema.parse(...))`,
	// or `@Body() / return type` handled elsewhere. We bind RETURNS only when a
	// schema is applied to a non-request value in a return/res position.
	respParseRe = regexp.MustCompile(
		`(?:return\s+|res\s*\.\s*(?:json|send)\s*\(\s*)([A-Za-z_]\w*)\s*\.\s*(parse|safeParse|validateSync)\s*\(`)
)

// detectRequestSchemas finds schema references that validate the request body
// in a handler region.
func detectRequestSchemas(region string, schemaNames map[string]bool) []schemaRef {
	var refs []schemaRef
	add := func(name, source string) {
		if name == "" || !schemaNames[name] {
			return
		}
		for _, r := range refs {
			if r.name == name {
				return
			}
		}
		refs = append(refs, schemaRef{name: name, source: source})
	}
	for _, m := range reqParseRe.FindAllStringSubmatch(region, -1) {
		add(m[1], "req_body_"+m[2])
	}
	for _, m := range reqMiddlewareRe.FindAllStringSubmatch(region, -1) {
		add(m[1], "validation_middleware")
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].name < refs[j].name })
	return refs
}

// detectResponseSchemas finds schema references applied to a response value.
func detectResponseSchemas(region string, schemaNames map[string]bool) []schemaRef {
	var refs []schemaRef
	for _, m := range respParseRe.FindAllStringSubmatch(region, -1) {
		name := m[1]
		if name == "" || !schemaNames[name] {
			continue
		}
		dup := false
		for _, r := range refs {
			if r.name == name {
				dup = true
				break
			}
		}
		if !dup {
			refs = append(refs, schemaRef{name: name, source: "response_" + m[2]})
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].name < refs[j].name })
	return refs
}

// schemaField is a captured field name + normalized type. `validators` carries
// the class-validator decorators (e.g. `@IsString`, `@IsOptional`) that
// annotate the field, and `optional` records whether the field is declared
// optional (`name?:` or `@IsOptional()`). These feed the field-membership
// sub-entities (issue #4606) so the dashboard /shape resolver can expand a
// request/query DTO's fields with parity to response Schema fields.
type schemaField struct {
	name       string
	typ        string
	validators []string
	optional   bool
}

// extractSchemaFields pulls `name: lib.<kind>()` field declarations out of a
// schema object body, normalizing the kind to a scalar type.
func extractSchemaFields(re *regexp.Regexp, body string) []schemaField {
	var fields []schemaField
	seen := make(map[string]bool)
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		fields = append(fields, schemaField{name: name, typ: normalizeScalar(m[2])})
	}
	return fields
}

// cvFieldDecoratorsRe captures the full run of decorators preceding a property
// declaration plus the property name and (optional) TS type annotation. Group 1
// = the raw decorator block (one or more `@X(...)`), group 2 = property name,
// group 3 = the TS type annotation (may be empty). Issue #4606: the per-field
// validator set is needed so request/query DTO fields expand with their
// validators (parity with response Schema annotations).
var cvFieldDecoratorsRe = regexp.MustCompile(
	`(?m)((?:@[A-Za-z]\w*\s*(?:\([^)]*\))?\s*)+)` +
		`([A-Za-z_]\w*)\s*([!?])?\s*(?::\s*([A-Za-z_][\w.<>\[\] ]*?))?\s*[;=\n]`)

// cvDecoratorNameRe pulls each decorator's bare name from a decorator block.
var cvDecoratorNameRe = regexp.MustCompile(`@([A-Za-z]\w*)`)

// extractClassValidatorFields pulls `@IsX() name: type;` fields from a class
// body. The TS annotation refines the type; otherwise the decorator does. The
// full decorator run is captured as the field's validator set, and `name?:` /
// `@IsOptional()` mark the field optional (issue #4606).
func extractClassValidatorFields(body string) []schemaField {
	var fields []schemaField
	seen := make(map[string]bool)
	for _, m := range cvFieldDecoratorsRe.FindAllStringSubmatch(body, -1) {
		decoratorBlock := m[1]
		name := m[2]
		optMark := m[3]
		annot := strings.TrimSpace(m[4])
		if name == "" || seen[name] {
			continue
		}
		validators := cvDecoratorNameRe.FindAllStringSubmatch(decoratorBlock, -1)
		var validatorNames []string
		isValidatorDTO := false
		for _, v := range validators {
			validatorNames = append(validatorNames, "@"+v[1])
			if _, isCV := cvDecoratorType[v[1]]; isCV {
				isValidatorDTO = true
			}
			switch v[1] {
			case "IsString", "IsInt", "IsNumber", "IsBoolean", "IsDate", "IsEmail",
				"IsUUID", "IsArray", "IsObject", "IsEnum", "IsPositive", "IsNegative",
				"IsOptional", "ValidateNested", "Type", "Min", "Max", "Length",
				"MinLength", "MaxLength", "IsNotEmpty", "IsDefined":
				isValidatorDTO = true
			}
		}
		if !isValidatorDTO {
			// A plain property with non-validator decorators — skip so we keep
			// parity with the original IsX-anchored detection (no false fields).
			continue
		}
		seen[name] = true
		// Determine the scalar type: TS annotation wins, else the first
		// type-bearing class-validator decorator.
		typ := ""
		if annot != "" {
			typ = normalizeScalar(annot)
		} else {
			for _, v := range validators {
				if dt, ok := cvDecoratorType[v[1]]; ok {
					typ = dt
					break
				}
			}
		}
		if typ == "" {
			typ = "unknown"
		}
		optional := optMark == "?"
		for _, vn := range validatorNames {
			if vn == "@IsOptional" {
				optional = true
			}
		}
		fields = append(fields, schemaField{
			name: name, typ: typ, validators: validatorNames, optional: optional,
		})
	}
	return fields
}

// cvNestedTarget is a captured `@Type(() => X)` nested-DTO target + the field
// it decorates.
type cvNestedTarget struct {
	target string
	field  string
}

// cvPrimitiveCoercion is the set of class-transformer primitive coercion
// targets — `@Type(() => Number)` etc. coerce a scalar, they are not nested
// DTO references, so they must NOT yield a class REFERENCES edge.
var cvPrimitiveCoercion = map[string]bool{
	"Number": true, "String": true, "Boolean": true, "Date": true,
	"BigInt": true, "Symbol": true,
}

// extractClassValidatorNestedTargets pulls `@Type(() => TargetClass) field:`
// nested-DTO references out of a class body, skipping primitive coercions.
func extractClassValidatorNestedTargets(body string) []cvNestedTarget {
	var out []cvNestedTarget
	seen := make(map[string]bool)
	for _, m := range cvNestedTypeRe.FindAllStringSubmatch(body, -1) {
		target := m[1]
		field := m[2]
		if target == "" || cvPrimitiveCoercion[target] {
			continue
		}
		key := target + ":" + field
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, cvNestedTarget{target: target, field: field})
	}
	return out
}

// normalizeScalar maps a raw factory/type token to a normalized scalar type.
func normalizeScalar(raw string) string {
	raw = strings.TrimSpace(raw)
	// strip generics/array suffixes: `string[]` -> `string`, `Array<X>` -> array.
	if strings.HasSuffix(raw, "[]") {
		return "array"
	}
	if idx := strings.IndexAny(raw, "<| ."); idx >= 0 {
		raw = raw[:idx]
	}
	lower := strings.ToLower(raw)
	if k, ok := schemaScalarKind[lower]; ok {
		return k
	}
	return lower
}

// applyFieldProps records captured fields on a schema entity: one
// `field_<name>` -> type property per field, plus a stable comma-joined
// `fields` summary and a `field_count`.
func applyFieldProps(ent *types.EntityRecord, fields []schemaField) {
	if len(fields) == 0 {
		setProps(ent, "field_count", "0")
		return
	}
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		setProps(ent, "field_"+f.name, f.typ)
		names = append(names, f.name)
	}
	sort.Strings(names)
	setProps(ent, "fields", strings.Join(names, ","), "field_count", fmt.Sprintf("%d", len(fields)))
}

// emitSchemaFieldMembers emits one `SCOPE.Schema/field` sub-entity per captured
// field of a schema/DTO, plus a CONTAINS edge from the owning schema entity to
// each child — parity with the response Schema field sub-nodes the dashboard
// /shape resolver already expands (shape_tree.go, #4587/#4569). Issue #4606:
// request `@Body` DTOs (CreateNoteBody), `@Query` object DTOs
// (InspectionCountsQuery), and any zod/joi/yup/class-validator schema now carry
// expandable field members instead of being opaque scalar-prop bags.
//
// The child entity's Signature is `[@Validators ...] <type> <name>` so the
// shape resolver's parseFieldSignature recovers (annotations, type, name)
// exactly as it does for Java/response DTO fields. Optional fields prepend an
// `@IsOptional` marker (when not already present) so the resolver infers
// nullability consistently. Each child is returned to the caller, and the
// CONTAINS edge is appended to the owner entity in place.
func emitSchemaFieldMembers(owner *types.EntityRecord, fields []schemaField, library, filePath, language string) []types.EntityRecord {
	if owner == nil || len(fields) == 0 {
		return nil
	}
	var out []types.EntityRecord
	for _, f := range fields {
		annots := append([]string(nil), f.validators...)
		if f.optional {
			hasOpt := false
			for _, a := range annots {
				if a == "@IsOptional" {
					hasOpt = true
					break
				}
			}
			if !hasOpt {
				annots = append(annots, "@IsOptional")
			}
		}
		// Build a Java-style field signature: `[@Ann ...] Type name`.
		var sb strings.Builder
		for _, a := range annots {
			sb.WriteString(a)
			sb.WriteByte(' ')
		}
		typ := f.typ
		if typ == "" {
			typ = "unknown"
		}
		sb.WriteString(typ)
		sb.WriteByte(' ')
		sb.WriteString(f.name)

		childName := owner.Name + "." + f.name
		child := makeEntity(childName, "SCOPE.Schema", "field", filePath, language, owner.StartLine)
		child.Signature = sb.String()
		setProps(&child, "library", library, "pattern_type", "field",
			"field_name", f.name, "field_type", typ, "parent_class", owner.Name,
			"provenance", "INFERRED_FROM_SCHEMA_FIELD_MEMBERSHIP")
		if f.optional {
			setProps(&child, "optional", "true")
		}
		if len(annots) > 0 {
			setProps(&child, "validators", strings.Join(annots, " "))
		}
		owner.Relationships = append(owner.Relationships,
			containsFieldEdge(owner.Name, child.ID, f.name, library))
		out = append(out, child)
	}
	return out
}

// balancedObjectBody returns the substring inside the parentheses starting at
// openParenIdx (the index of `(`), reading balanced `(`/`)`.
func balancedObjectBody(src string, openParenIdx int) string {
	if openParenIdx < 0 || openParenIdx >= len(src) || src[openParenIdx] != '(' {
		return ""
	}
	depth := 0
	for i := openParenIdx; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[openParenIdx+1 : i]
			}
		}
	}
	return src[openParenIdx+1:]
}

// balancedBraceBody returns the substring inside the braces starting at
// openBraceIdx (the index of `{`), reading balanced `{`/`}`.
func balancedBraceBody(src string, openBraceIdx int) string {
	if openBraceIdx < 0 || openBraceIdx >= len(src) || src[openBraceIdx] != '{' {
		return ""
	}
	depth := 0
	for i := openBraceIdx; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[openBraceIdx+1 : i]
			}
		}
	}
	return src[openBraceIdx+1:]
}
