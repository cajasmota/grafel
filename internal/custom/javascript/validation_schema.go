package javascript

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
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
	// zodScalarSchemaRe: any top-level `const X = z.<factory>(` declaration —
	// `z.string()`, `z.number()`, `z.array(...)`, `z.object({...})`, etc. Group 1
	// = var name, group 2 = the factory method. Used by the #5497 scalar-schema
	// pass to attach a refine/transform/pipe chain to non-object zod schemas; the
	// object pass owns `z.object`.
	zodScalarSchemaRe = regexp.MustCompile(
		`(?m)(?:const|let|var)\s+(?P<name>[A-Za-z_]\w*)\s*(?::\s*[\w.<>\[\] ]+)?\s*=\s*(?:z|zod)\s*\.\s*(?P<coerce>coerce\s*\.\s*)?(?P<kind>[A-Za-z_]\w*)\s*\(`)
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

	// zod field: `name: z.string()`, `age: z.number().int()`. The optional
	// `coerce.` segment (#5498) recognises zod's coercion factory
	// `z.coerce.<type>()` (`z.coerce.number()`, `z.coerce.date()`, …): the
	// captured `kind` is still the underlying scalar type and the `coerce` group
	// marks the field as coerced.
	zodFieldRe = regexp.MustCompile(`(?m)(?P<name>[A-Za-z_]\w*)\s*:\s*(?:z|zod)\s*\.\s*(?P<coerce>coerce\s*\.\s*)?(?P<kind>[A-Za-z_]\w*)\s*\(`)
	// joi field: `name: Joi.string()`.
	joiFieldRe = regexp.MustCompile(`(?m)(?P<name>[A-Za-z_]\w*)\s*:\s*(?:Joi|joi)\s*\.\s*(?P<kind>[A-Za-z_]\w*)\s*\(`)
	// yup field: `name: yup.string()`.
	yupFieldRe = regexp.MustCompile(`(?m)(?P<name>[A-Za-z_]\w*)\s*:\s*(?:yup|Yup)\s*\.\s*(?P<kind>[A-Za-z_]\w*)\s*\(`)
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
		// Subtype is part of the key so a nested sub-schema (subtype=nested_schema)
		// and a same-named flat object-typed field member (subtype=field) — e.g.
		// `Profile.address` exists as both — don't collide (issue #5496).
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
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
			fields := extractSchemaFields(sd.field, body, sd.lib)
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
			// Nested z.object() sub-schemas (issue #5496, zod only): a field whose
			// value is itself a z.object({...}) — including inside z.array(...),
			// z.record(...), .optional()/.nullable() wrappers and z.union([...])
			// branches — is expanded into a child SCOPE.Schema linked to its parent.
			var nested []types.EntityRecord
			// Schema-level custom-validator chain (issue #5497, zod only):
			// `.refine()`/`.superRefine()`/`.transform()`/`.pipe()` chained on the
			// schema after `z.object({...})` are captured as refinement/transform
			// child entities (+ pipe attribute/edge), order-preserved.
			var chainKids []types.EntityRecord
			if sd.lib == "zod" {
				nested = expandNestedZodObjects(&ent, name, body, file.Path, file.Language, lineOf(src, m[0]))
				tail := schemaTailChain(src, openParen)
				chainKids = emitZodSchemaChainEntities(&ent, name, tail, file.Path, file.Language, lineOf(src, m[0]))
			}
			addSchema(ent)
			for _, c := range children {
				addSchema(c)
			}
			for _, n := range nested {
				addSchema(n)
			}
			for _, c := range chainKids {
				addSchema(c)
			}
		}
	}

	// 1.5 Scalar / non-object zod schemas carrying a custom-validator chain
	// (issue #5497). `const Slug = z.string().refine(...)` is a top-level schema
	// whose factory is NOT z.object, so the object pass above never sees it. We
	// emit a SCOPE.Schema entity for it ONLY when it chains a
	// refine/superRefine/transform/pipe link — a bare `z.string()` const carries
	// nothing this issue models and would just add noise, so it is skipped
	// (honest-partial). Object schemas are already handled above (skip them here).
	scalarNameIdx := zodScalarSchemaRe.SubexpIndex("name")
	scalarKindIdx := zodScalarSchemaRe.SubexpIndex("kind")
	scalarCoerceIdx := zodScalarSchemaRe.SubexpIndex("coerce")
	for _, m := range zodScalarSchemaRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2*scalarNameIdx]:m[2*scalarNameIdx+1]]
		factory := src[m[2*scalarKindIdx]:m[2*scalarKindIdx+1]]
		// zod `const X = z.coerce.<type>()` (#5498): a coerced scalar schema.
		coerced := scalarCoerceIdx >= 0 && m[2*scalarCoerceIdx] >= 0
		if factory == "object" {
			continue // handled by the object pass (with field expansion)
		}
		if schemaNames[name] {
			continue // already emitted as an object/class schema
		}
		openParen := m[1] - 1 // header ends at the `(` of `.<factory>(`
		tail := schemaTailChain(src, openParen)
		links := parseZodSchemaChain(tail)
		if len(links) == 0 && !coerced {
			continue // no custom-validator chain and not coerced — nothing to record
		}
		ent := makeEntity(name, "SCOPE.Schema", "validation_schema", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "library", "zod", "pattern_type", "validation_schema",
			"scalar_kind", normalizeScalar(factory),
			"provenance", "INFERRED_FROM_ZOD_SCALAR")
		// Coercion attribute on the scalar schema itself (#5498).
		if coerced {
			setProps(&ent, "coerced", "true", "coercion_type", normalizeScalar(factory))
		}
		applyFieldProps(&ent, nil)
		chainKids := emitZodSchemaChainEntities(&ent, name, tail, file.Path, file.Language, lineOf(src, m[0]))
		addSchema(ent)
		for _, c := range chainKids {
			addSchema(c)
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
	// validations carries the terse chain-constraint chips for zod/joi/yup
	// schema fields (`MaxLength:120`, `Email`, `Min:0`, `Required`, …), folded
	// from the field's method chain by parseChainConstraints (#4976) and stamped
	// onto the field member's Properties["validations"] for the ShapeTree.
	validations []string
	optional    bool
	// coerced records whether the field is declared with zod's coercion factory
	// `z.coerce.<type>()` (#5498). When true, coercionType carries the underlying
	// coerced scalar type (`number`, `date`, `boolean`, …). The field's `typ`
	// remains the coerced (underlying) type — coercion is an attribute on top.
	coerced      bool
	coercionType string
}

// extractSchemaFields pulls `name: lib.<kind>()` field declarations out of a
// schema object body, normalizing the kind to a scalar type. For each field it
// also captures the method chain that follows the factory (e.g.
// `z.string().max(120).email()`) and folds the recognised chain constraints
// into terse validation chips (`MaxLength:120`, `Email`, …) consistent with the
// class-validator chip format (#4858 / #4976) so the ShapeTree renders them
// identically for schema-based validators.
func extractSchemaFields(re *regexp.Regexp, body string, lib string) []schemaField {
	var fields []schemaField
	seen := make(map[string]bool)
	nameIdx := re.SubexpIndex("name")
	kindIdx := re.SubexpIndex("kind")
	coerceIdx := re.SubexpIndex("coerce") // -1 for joi/yup (no coerce group)
	for _, m := range re.FindAllStringSubmatchIndex(body, -1) {
		name := body[m[2*nameIdx]:m[2*nameIdx+1]]
		if seen[name] {
			continue
		}
		seen[name] = true
		kind := body[m[2*kindIdx]:m[2*kindIdx+1]]
		// zod `z.coerce.<type>()` coercion (#5498): the `coerce` group matched, so
		// the field's underlying type is `kind` and it carries a coercion attribute.
		coerced := coerceIdx >= 0 && m[2*coerceIdx] >= 0
		// The chain begins at the factory call's opening `(` (one char before the
		// match end) and runs to the end of this field's value (the next
		// top-level comma in the object body, or EOF).
		chain := fieldChain(body, m[1]-1)
		chips := parseChainConstraints(lib, kind, chain)
		optional := chainIsOptional(lib, kind, chips, chain)
		f := schemaField{
			name: name, typ: normalizeScalar(kind), validations: chips, optional: optional,
		}
		if coerced {
			f.coerced = true
			f.coercionType = normalizeScalar(kind)
		}
		fields = append(fields, f)
	}
	return fields
}

// maxNestedSchemaDepth caps the nested-z.object() recursion so a pathologically
// deep (or accidentally self-referential) schema literal can't blow up the
// extractor. 8 levels is far deeper than any realistic request/response DTO.
const maxNestedSchemaDepth = 8

// nestedObjectFieldRe matches a single `field: <value>` member of a zod object
// body whose value *contains* a `z.object(` somewhere in its wrapper chain —
// directly (`field: z.object({...})`), or wrapped in z.array / z.record /
// z.union / .optional() / .nullable() etc. Group 1 = field name. The value's
// extent is recovered separately via fieldChain so the match only needs to
// anchor the field name and confirm an object lives in its value.
var nestedObjectFieldRe = regexp.MustCompile(`(?m)([A-Za-z_]\w*)\s*:\s*(?:z|zod)\s*\.`)

// zObjectCallRe locates a `z.object(` / `zod.object(` factory call so the
// recursion can find the object literal regardless of the wrappers around it.
var zObjectCallRe = regexp.MustCompile(`(?:z|zod)\s*\.\s*object\s*\(`)

// expandNestedZodObjects walks the fields of a zod object body and, for every
// field whose value contains a nested `z.object({...})` (reachable through
// z.array(...), z.record(...), z.union([...]) branches, and .optional() /
// .nullable() / .default(...) / .describe(...) chain wrappers), emits a child
// SCOPE.Schema entity named `<parentPath>.<field>` carrying that nested
// object's own scalar fields, plus a CONTAINS edge from the parent schema to
// the child (the same field→schema membership model used for flat fields,
// #4606). The recursion descends so `a.b.c` nesting yields one child schema per
// level with a dotted name path. It is capped at maxNestedSchemaDepth.
//
// Honest-partial: a field whose value has no statically-locatable z.object()
// (dynamic, computed, or a $ref to another named schema) yields no nested child
// — no false nesting.
func expandNestedZodObjects(parent *types.EntityRecord, parentPath, body, filePath, language string, line int) []types.EntityRecord {
	return expandNestedZodObjectsDepth(parent, parentPath, body, filePath, language, line, 1)
}

func expandNestedZodObjectsDepth(parent *types.EntityRecord, parentPath, body, filePath, language string, line, depth int) []types.EntityRecord {
	if depth > maxNestedSchemaDepth || parent == nil {
		return nil
	}
	var out []types.EntityRecord
	seen := make(map[string]bool)
	for _, m := range nestedObjectFieldRe.FindAllStringSubmatchIndex(body, -1) {
		fieldName := body[m[2]:m[3]]
		if seen[fieldName] {
			continue
		}
		// Recover this field's full value text (its method/wrapper chain) so we
		// only look at this field's value, not its siblings'. m[1] points just
		// past the `.` of `z.`; fieldChain wants the position of the value start.
		valueChain := fieldChain(body, m[3])
		objBody, ok := firstNestedObjectBody(valueChain)
		if !ok {
			continue
		}
		seen[fieldName] = true
		childPath := parentPath + "." + fieldName

		// Build the child schema entity. Its scalar fields come from the same
		// flat field extractor used for top-level schemas.
		childFields := extractSchemaFields(zodFieldRe, objBody, "zod")
		child := makeEntity(childPath, "SCOPE.Schema", "nested_schema", filePath, language, line)
		setProps(&child, "library", "zod", "pattern_type", "nested_schema",
			"parent_schema", parentPath, "field_path", childPath, "nesting_depth",
			fmt.Sprintf("%d", depth),
			"provenance", "INFERRED_FROM_ZOD_NESTED_OBJECT")
		applyFieldProps(&child, childFields)
		// Link parent → child via the field-membership CONTAINS edge (same model
		// as flat field members, #4606), so the nested object is owned by its
		// parent and carries the field-name path.
		parent.Relationships = append(parent.Relationships,
			nestedSchemaEdge(parentPath, child.ID, fieldName, childPath))

		// The child's own scalar field members (parity with flat schemas).
		grandFields := emitSchemaFieldMembers(&child, childFields, "zod", filePath, language)

		// Recurse: a nested object may itself contain further nested objects.
		deeper := expandNestedZodObjectsDepth(&child, childPath, objBody, filePath, language, line, depth+1)

		out = append(out, child)
		out = append(out, grandFields...)
		out = append(out, deeper...)
	}
	return out
}

// firstNestedObjectBody returns the body of the first `z.object({...})` found
// inside a field's value chain, descending through any wrapper text that
// precedes it (z.array(/z.record(/z.union([/.optional()/.nullable()/...). It
// returns the balanced contents of the object literal and true, or ("", false)
// when the value contains no locatable z.object() (scalar field, $ref to a
// named schema, or a dynamic/computed value — honest-partial).
func firstNestedObjectBody(valueChain string) (string, bool) {
	loc := zObjectCallRe.FindStringIndex(valueChain)
	if loc == nil {
		return "", false
	}
	// loc[1]-1 is the index of the `(` opening the object() call.
	objArg := balancedObjectBody(valueChain, loc[1]-1)
	// The object literal is the `{...}` inside object(...). Trim to its braces so
	// the field extractor only sees the object's own members. z.object takes the
	// shape literal as its first arg; find the first top-level `{`.
	braceStart := strings.IndexByte(objArg, '{')
	if braceStart < 0 {
		// z.object() with no literal shape (dynamic) — honest-partial.
		return "", false
	}
	objBody := balancedBraceBody(objArg, braceStart)
	return objBody, true
}

// nestedSchemaEdge builds the parent→child membership edge for a nested
// z.object() sub-schema. It reuses the CONTAINS field-membership kind (the same
// relationship flat field members use, #4606) so the nested object is owned by
// its parent schema and carries the dotted field-name path. ToID is the
// concrete child schema entity ID.
func nestedSchemaEdge(parentSchema, childID, fieldName, fieldPath string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: "Schema:" + parentSchema,
		ToID:   childID,
		Kind:   string(types.RelationshipKindContains),
		Properties: map[string]string{
			"framework":  "zod",
			"member":     "nested_schema",
			"field_name": fieldName,
			"field_path": fieldPath,
			"provenance": "INFERRED_FROM_ZOD_NESTED_OBJECT",
		},
	}
}

// fieldChain returns the schema-field value text starting at the factory call's
// opening `(` (openParenIdx) and ending at the next comma that sits at the same
// brace/paren/bracket depth as the field declaration (i.e. the separator
// between this field and the next), or end of body. This isolates a single
// field's full method chain — `z.string().min(2).max(120)` — from its
// siblings so the chain parser only sees this field's constraints.
func fieldChain(body string, openParenIdx int) string {
	if openParenIdx < 0 || openParenIdx >= len(body) {
		return ""
	}
	depth := 0
	for i := openParenIdx; i < len(body); i++ {
		switch body[i] {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
		case ',':
			if depth <= 0 {
				return body[openParenIdx:i]
			}
		}
	}
	return body[openParenIdx:]
}

// chainMethodRe matches a `.method(args)` or `.method` link in a schema method
// chain. Group 1 = method name, group 2 = the raw argument list (may be empty
// or absent for property-style links like zod `.optional` is always a call, but
// kept permissive).
var chainMethodRe = regexp.MustCompile(`\.\s*([A-Za-z_]\w*)\s*(?:\(([^()]*)\))?`)

// zodChainChips maps a zod chain method to a terse constraint chip. A "" target
// means the method carries a single scalar arg folded as `Chip:<arg>`; methods
// not present here are ignored (kept terse).
var zodChainChips = map[string]string{
	"email": "Email", "uuid": "UUID", "url": "Url", "regex": "Pattern",
	"int": "Int", "positive": "Positive", "negative": "Negative",
	"nonempty": "NotEmpty", "cuid": "Cuid", "datetime": "DateTime", "ip": "IP",
}

// zodScalarArgChips: zod chain methods whose first scalar arg is folded into
// the chip text — `.max(120)` → `MaxLength:120` (for string/array) but the
// label is normalized below.
var zodScalarArgChips = map[string]string{
	"max": "Max", "min": "Min", "length": "Length", "gt": "GreaterThan",
	"gte": "Min", "lt": "LessThan", "lte": "Max", "multipleof": "MultipleOf",
}

// joiChainChips: joi chain methods that carry no scalar bound (flag-style).
var joiChainChips = map[string]string{
	"required": "Required", "email": "Email", "uuid": "UUID", "guid": "UUID",
	"uri": "Url", "pattern": "Pattern", "regex": "Pattern", "integer": "Int",
	"positive": "Positive", "negative": "Negative",
}

// joiScalarArgChips: joi chain methods whose first scalar arg is folded.
var joiScalarArgChips = map[string]string{
	"max": "Max", "min": "Min", "length": "Length", "greater": "GreaterThan",
	"less": "LessThan",
}

// yupChainChips: yup chain methods that carry no scalar bound.
var yupChainChips = map[string]string{
	"required": "Required", "email": "Email", "uuid": "UUID", "url": "Url",
	"matches": "Pattern", "positive": "Positive", "negative": "Negative",
	"integer": "Int",
}

// yupScalarArgChips: yup chain methods whose first scalar arg is folded.
var yupScalarArgChips = map[string]string{
	"max": "Max", "min": "Min", "length": "Length",
}

// parseChainConstraints walks a schema field's method chain and returns the
// recognised constraints as terse chips in source order (e.g.
// `["MaxLength:120","Email"]`). The label set is normalized across zod/joi/yup
// so the ShapeTree renders schema constraints with the same chip vocabulary as
// class-validator: `Min`/`Max`/`Length`/`Email`/`Pattern`/`Required`/… For
// `min`/`max`/`length` the chip is `MinLength`/`MaxLength` when the field is a
// string or array (length bound) and `Min`/`Max` for numeric kinds — matching
// the class-validator `@MinLength`/`@Min` split.
func parseChainConstraints(lib, kind, chain string) []string {
	var flags map[string]string
	var args map[string]string
	switch lib {
	case "zod":
		flags, args = zodChainChips, zodScalarArgChips
	case "joi":
		flags, args = joiChainChips, joiScalarArgChips
	case "yup":
		flags, args = yupChainChips, yupScalarArgChips
	default:
		return nil
	}
	lengthKind := isLengthKind(normalizeScalar(kind))
	var out []string
	seen := make(map[string]bool)
	for _, m := range chainMethodRe.FindAllStringSubmatch(chain, -1) {
		method := strings.ToLower(m[1])
		arg := strings.TrimSpace(m[2])
		if label, ok := args[method]; ok {
			// Split Min/Max into MinLength/MaxLength for string/array bounds so the
			// chip matches the class-validator vocabulary.
			if lengthKind {
				switch label {
				case "Min":
					label = "MinLength"
				case "Max":
					label = "MaxLength"
				case "Length":
					label = "Length"
				}
			}
			chip := label
			if v := firstChainScalar(arg); v != "" {
				chip = label + ":" + v
			}
			if !seen[chip] {
				seen[chip] = true
				out = append(out, chip)
			}
			continue
		}
		if label, ok := flags[method]; ok {
			if !seen[label] {
				seen[label] = true
				out = append(out, label)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// isLengthKind reports whether a normalized scalar kind measures a length bound
// (string / array) rather than a numeric magnitude, so `min`/`max` map to
// `MinLength`/`MaxLength` vs `Min`/`Max`.
func isLengthKind(kind string) bool {
	return kind == "string" || kind == "array"
}

// firstChainScalar returns a single numeric/string scalar from a chain method's
// raw argument list, or "" for empty / compound args (objects, regex literals,
// multiple args) so those chips stay bare.
func firstChainScalar(arg string) string {
	if arg == "" {
		return ""
	}
	if strings.ContainsAny(arg, ",{}[]") {
		return ""
	}
	arg = strings.TrimSpace(arg)
	// Numeric literal.
	if numericLiteralRe.MatchString(arg) {
		return arg
	}
	// Simple quoted string → drop quotes.
	if len(arg) >= 2 && (arg[0] == '\'' || arg[0] == '"' || arg[0] == '`') {
		return strings.Trim(arg, "'\"`")
	}
	return ""
}

var numericLiteralRe = regexp.MustCompile(`^-?\d+(?:\.\d+)?$`)

// chainIsOptional reports whether a schema field is declared optional via its
// chain — zod `.optional()`/`.nullish()`, joi without `.required()`, yup
// without `.required()`. Joi/yup default to optional unless `.required()` is
// present; zod defaults to required unless `.optional()`/`.nullish()` present.
func chainIsOptional(lib, kind string, chips []string, chain string) bool {
	hasRequired := false
	for _, c := range chips {
		if c == "Required" {
			hasRequired = true
		}
	}
	switch lib {
	case "zod":
		return strings.Contains(chain, ".optional(") || strings.Contains(chain, ".nullish(")
	case "joi", "yup":
		return !hasRequired
	}
	return false
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
		// zod `z.coerce.<type>()` coercion (#5498): surface the coercion flag at the
		// schema-entity level too, keyed per field, so consumers reading the entity
		// props (not just the expanded field member) see it.
		if f.coerced {
			setProps(ent, "field_"+f.name+"_coerced", "true")
		}
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
		// zod `z.coerce.<type>()` coercion (#5498): flag the coerced field and
		// record the coerced (underlying) scalar type.
		if f.coerced {
			setProps(&child, "coerced", "true")
			if f.coercionType != "" {
				setProps(&child, "coercion_type", f.coercionType)
			}
		}
		if len(annots) > 0 {
			setProps(&child, "validators", strings.Join(annots, " "))
		}
		// Issue #4976: fold zod/joi/yup chain constraints into the per-field
		// `validations` chip list (comma-joined) so the ShapeTree renders schema
		// constraints (`MaxLength:120`, `Email`, `Min:0`, `Required`, …) with the
		// same chip format as class-validator (#4858). class-validator DTO fields
		// get their `validations` chips from the tree-sitter pass
		// (class_validator_fields.go); the schema-chain libraries get them here.
		if len(f.validations) > 0 {
			setProps(&child, "validations", strings.Join(f.validations, ","))
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
