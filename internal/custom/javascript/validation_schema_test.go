package javascript_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"

	_ "github.com/cajasmota/grafel/internal/custom/javascript"
)

// schemaEntity returns the first SCOPE.Schema entity with the given name.
func schemaEntity(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Schema" && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func wantField(t *testing.T, e *types.EntityRecord, name, typ string) {
	t.Helper()
	got, ok := e.Properties["field_"+name]
	if !ok {
		t.Fatalf("schema %q missing field_%s (props=%v)", e.Name, name, e.Properties)
	}
	if got != typ {
		t.Fatalf("schema %q field %s: got type %q, want %q", e.Name, name, got, typ)
	}
}

// ---------------------------------------------------------------------------
// Zod
// ---------------------------------------------------------------------------

func TestZodSchema_FieldsAndAcceptsInput(t *testing.T) {
	src := `import { z } from 'zod';
const CreateUser = z.object({ name: z.string(), age: z.number() });

router.post('/users', (req, res) => {
  const data = CreateUser.parse(req.body);
  res.json(data);
});`
	ents := extractFull(t, "custom_js_validation_schema", fi("users.ts", "typescript", src))

	se := schemaEntity(ents, "CreateUser")
	if se == nil {
		t.Fatal("expected SCOPE.Schema CreateUser")
	}
	if se.Properties["library"] != "zod" {
		t.Errorf("library = %q, want zod", se.Properties["library"])
	}
	wantField(t, se, "name", "string")
	wantField(t, se, "age", "number")

	if !hasDTOEdge(ents, "ACCEPTS_INPUT", "Schema:CreateUser") {
		t.Fatal("expected ACCEPTS_INPUT -> Schema:CreateUser")
	}
	if owner := dtoEdgeOwner(ents, "ACCEPTS_INPUT", "Schema:CreateUser"); owner != "POST /users" {
		t.Errorf("edge owner = %q, want 'POST /users'", owner)
	}
}

func TestZodSchema_MiddlewareBinding(t *testing.T) {
	src := `const LoginDto = z.object({ email: z.string(), remember: z.boolean() });
app.post('/login', validate(LoginDto), (req, res) => res.send('ok'));`
	ents := extractFull(t, "custom_js_validation_schema", fi("auth.ts", "typescript", src))
	if !hasDTOEdge(ents, "ACCEPTS_INPUT", "Schema:LoginDto") {
		t.Fatal("expected ACCEPTS_INPUT -> Schema:LoginDto via middleware")
	}
	se := schemaEntity(ents, "LoginDto")
	wantField(t, se, "email", "string")
	wantField(t, se, "remember", "boolean")
}

// Negative: a zod schema never referenced by a route → schema entity exists,
// but no endpoint edge (no false binding).
func TestZodSchema_UnusedNoBinding(t *testing.T) {
	src := `const Orphan = z.object({ x: z.string() });
router.get('/ping', (req, res) => res.send('pong'));`
	ents := extractFull(t, "custom_js_validation_schema", fi("orphan.ts", "typescript", src))
	if schemaEntity(ents, "Orphan") == nil {
		t.Fatal("expected schema entity Orphan to exist")
	}
	if hasDTOEdge(ents, "ACCEPTS_INPUT", "Schema:Orphan") {
		t.Fatal("unused schema must NOT produce an ACCEPTS_INPUT edge")
	}
}

// ---------------------------------------------------------------------------
// Field-membership sub-entities (issue #4606)
// ---------------------------------------------------------------------------

// fieldChild returns the SCOPE.Schema/field sub-entity named "<Class>.<field>".
func fieldChild(ents []types.EntityRecord, qualified string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" && e.Name == qualified {
			return e
		}
	}
	return nil
}

// A class-validator request `@Body` DTO must expand to per-field member
// sub-entities (with CONTAINS edges + a parseable Signature) so the dashboard
// /shape resolver can render them — parity with response Schema fields.
func TestClassValidatorDTO_FieldMembers(t *testing.T) {
	src := `import { IsString, IsInt, IsOptional } from 'class-validator';
export class CreateNoteBody {
  @IsString()
  title: string;

  @IsInt()
  @IsOptional()
  priority?: number;
}`
	ents := extractFull(t, "custom_js_validation_schema", fi("note.dto.ts", "typescript", src))

	se := schemaEntity(ents, "CreateNoteBody")
	if se == nil {
		t.Fatal("expected SCOPE.Schema CreateNoteBody")
	}
	// Scalar-prop bag preserved (back-compat).
	wantField(t, se, "title", "string")
	// TS annotation `priority?: number` is authoritative over the @IsInt decorator.
	wantField(t, se, "priority", "number")

	// Field sub-entities exist.
	titleChild := fieldChild(ents, "CreateNoteBody.title")
	if titleChild == nil {
		t.Fatal("expected field sub-entity CreateNoteBody.title")
	}
	if titleChild.Signature == "" {
		t.Fatal("field sub-entity must carry a Signature for the shape resolver")
	}
	priorityChild := fieldChild(ents, "CreateNoteBody.priority")
	if priorityChild == nil {
		t.Fatal("expected field sub-entity CreateNoteBody.priority")
	}
	if priorityChild.Properties["optional"] != "true" {
		t.Errorf("priority should be optional, props=%v", priorityChild.Properties)
	}

	// CONTAINS membership edges bind each field to the owner.
	if !hasContainsTo(ents, titleChild.ID, titleChild.Name) {
		t.Error("expected CONTAINS edge to CreateNoteBody.title")
	}
	if !hasContainsTo(ents, priorityChild.ID, priorityChild.Name) {
		t.Error("expected CONTAINS edge to CreateNoteBody.priority")
	}
}

// A zod object schema also gets field sub-entities (general parity).
func TestZodSchema_FieldMembers(t *testing.T) {
	src := `const CreateUser = z.object({ name: z.string(), age: z.number() });
router.post('/users', (req, res) => { CreateUser.parse(req.body); res.json({}); });`
	ents := extractFull(t, "custom_js_validation_schema", fi("u.ts", "typescript", src))
	nameChild := fieldChild(ents, "CreateUser.name")
	if nameChild == nil {
		t.Fatal("expected field sub-entity CreateUser.name")
	}
	if !hasContainsTo(ents, nameChild.ID, nameChild.Name) {
		t.Error("expected CONTAINS edge to CreateUser.name")
	}
}

// ---------------------------------------------------------------------------
// Nested z.object() → nested schema tree (issue #5496)
// ---------------------------------------------------------------------------

// nestedSchema returns the nested SCOPE.Schema sub-entity named "<dotted.path>".
func nestedSchema(ents []types.EntityRecord, dottedPath string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind == "SCOPE.Schema" && e.Subtype == "nested_schema" && e.Name == dottedPath {
			return e
		}
	}
	return nil
}

// hasNestedSchemaEdge reports whether the parent schema declares a CONTAINS edge
// (member=nested_schema) to the given child id with the expected field path.
func hasNestedSchemaEdge(ents []types.EntityRecord, childID, fieldPath string) bool {
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != string(types.RelationshipKindContains) {
				continue
			}
			if r.ToID == childID && r.Properties["member"] == "nested_schema" &&
				r.Properties["field_path"] == fieldPath {
				return true
			}
		}
	}
	return false
}

// A zod schema with a nested z.object, an array-of-objects, an optional nested
// object, and a union branch carrying an object → each nested object expands to
// a child SCOPE.Schema linked to its parent with the dotted field path; a flat
// sibling field still records normally.
func TestZodSchema_NestedObjects(t *testing.T) {
	src := `import { z } from 'zod';
const Profile = z.object({
  name: z.string(),
  address: z.object({ city: z.string(), zip: z.string() }),
  tags: z.array(z.object({ label: z.string() })),
  meta: z.object({ source: z.string() }).optional(),
  contact: z.union([z.string(), z.object({ email: z.string() })]),
});
router.post('/profiles', (req, res) => { Profile.parse(req.body); res.json({}); });`
	ents := extractFull(t, "custom_js_validation_schema", fi("profile.ts", "typescript", src))

	// Flat schema not regressed: top-level scalar + the object-typed fields.
	se := schemaEntity(ents, "Profile")
	if se == nil {
		t.Fatal("expected SCOPE.Schema Profile")
	}
	wantField(t, se, "name", "string")
	wantField(t, se, "address", "object")

	// Direct nested object: Profile.address with its own scalar fields.
	addr := nestedSchema(ents, "Profile.address")
	if addr == nil {
		t.Fatal("expected nested schema Profile.address")
	}
	wantField(t, addr, "city", "string")
	wantField(t, addr, "zip", "string")
	if addr.Properties["parent_schema"] != "Profile" {
		t.Errorf("address parent_schema = %q, want Profile", addr.Properties["parent_schema"])
	}
	if !hasNestedSchemaEdge(ents, addr.ID, "Profile.address") {
		t.Error("expected CONTAINS(nested_schema) edge Profile -> Profile.address")
	}
	// Nested object's own field members emitted (parity with flat schemas).
	if fieldChild(ents, "Profile.address.city") == nil {
		t.Error("expected field member Profile.address.city")
	}

	// Array-of-objects: z.array(z.object({...})) descends.
	tags := nestedSchema(ents, "Profile.tags")
	if tags == nil {
		t.Fatal("expected nested schema Profile.tags (z.array(z.object))")
	}
	wantField(t, tags, "label", "string")

	// Optional nested object: z.object({...}).optional() descends.
	meta := nestedSchema(ents, "Profile.meta")
	if meta == nil {
		t.Fatal("expected nested schema Profile.meta (.optional())")
	}
	wantField(t, meta, "source", "string")

	// Union branch carrying an object: z.union([..., z.object({...})]) descends.
	contact := nestedSchema(ents, "Profile.contact")
	if contact == nil {
		t.Fatal("expected nested schema Profile.contact (z.union object branch)")
	}
	wantField(t, contact, "email", "string")
}

// Deeply nested objects expand recursively with a dotted name path per level.
func TestZodSchema_NestedObjects_Recursive(t *testing.T) {
	src := `const Root = z.object({ a: z.object({ b: z.object({ c: z.string() }) }) });`
	ents := extractFull(t, "custom_js_validation_schema", fi("root.ts", "typescript", src))
	if nestedSchema(ents, "Root.a") == nil {
		t.Fatal("expected nested schema Root.a")
	}
	if nestedSchema(ents, "Root.a.b") == nil {
		t.Fatal("expected nested schema Root.a.b")
	}
	leaf := nestedSchema(ents, "Root.a.b")
	wantField(t, leaf, "c", "string")
}

// A flat schema with no nested objects produces no nested_schema entities.
func TestZodSchema_NoNesting_NoNestedEntities(t *testing.T) {
	src := `const Flat = z.object({ x: z.string(), y: z.number() });`
	ents := extractFull(t, "custom_js_validation_schema", fi("flat.ts", "typescript", src))
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "nested_schema" {
			t.Fatalf("flat schema must not yield nested_schema entities: got %s", e.Name)
		}
	}
	// Flat schema itself still works.
	se := schemaEntity(ents, "Flat")
	wantField(t, se, "x", "string")
	wantField(t, se, "y", "number")
}

// ---------------------------------------------------------------------------
// Joi
// ---------------------------------------------------------------------------

func TestJoiSchema_FieldsAndAcceptsInput(t *testing.T) {
	src := `const CreateProduct = Joi.object({ title: Joi.string(), price: Joi.number(), active: Joi.boolean() });
router.post('/products', (req, res) => {
  const v = CreateProduct.validate(req.body);
  res.json(v);
});`
	ents := extractFull(t, "custom_js_validation_schema", fi("products.js", "javascript", src))
	se := schemaEntity(ents, "CreateProduct")
	if se == nil || se.Properties["library"] != "joi" {
		t.Fatalf("expected joi schema CreateProduct, got %+v", se)
	}
	wantField(t, se, "title", "string")
	wantField(t, se, "price", "number")
	wantField(t, se, "active", "boolean")
	if !hasDTOEdge(ents, "ACCEPTS_INPUT", "Schema:CreateProduct") {
		t.Fatal("expected ACCEPTS_INPUT -> Schema:CreateProduct")
	}
}

// ---------------------------------------------------------------------------
// Yup
// ---------------------------------------------------------------------------

func TestYupSchema_ShapeFieldsAndAcceptsInput(t *testing.T) {
	src := `const SignupSchema = yup.object().shape({ username: yup.string(), age: yup.number() });
app.post('/signup', (req, res) => {
  SignupSchema.validateSync(req.body);
  res.send('ok');
});`
	ents := extractFull(t, "custom_js_validation_schema", fi("signup.js", "javascript", src))
	se := schemaEntity(ents, "SignupSchema")
	if se == nil || se.Properties["library"] != "yup" {
		t.Fatalf("expected yup schema SignupSchema, got %+v", se)
	}
	wantField(t, se, "username", "string")
	wantField(t, se, "age", "number")
	if !hasDTOEdge(ents, "ACCEPTS_INPUT", "Schema:SignupSchema") {
		t.Fatal("expected ACCEPTS_INPUT -> Schema:SignupSchema")
	}
}

// ---------------------------------------------------------------------------
// class-validator
// ---------------------------------------------------------------------------

func TestClassValidator_FieldsFromDecorators(t *testing.T) {
	src := `export class CreateUserDto {
  @IsString()
  name: string;

  @IsInt()
  age: number;

  @IsEmail()
  email: string;
}`
	ents := extractFull(t, "custom_js_validation_schema", fi("create-user.dto.ts", "typescript", src))
	se := schemaEntity(ents, "CreateUserDto")
	if se == nil || se.Properties["library"] != "class-validator" {
		t.Fatalf("expected class-validator schema CreateUserDto, got %+v", se)
	}
	// TS annotation wins for name/age; email annotation is string.
	wantField(t, se, "name", "string")
	wantField(t, se, "age", "number")
	wantField(t, se, "email", "string")
}

// A plain class without any class-validator decorators is NOT treated as a
// validation schema (no false schema entity).
func TestClassValidator_PlainClassSkipped(t *testing.T) {
	src := `export class Helper {
  doThing(): void {}
}`
	ents := extractFull(t, "custom_js_validation_schema", fi("helper.ts", "typescript", src))
	if schemaEntity(ents, "Helper") != nil {
		t.Fatal("plain class must not be emitted as a validation schema")
	}
}

// ---------------------------------------------------------------------------
// RETURNS
// ---------------------------------------------------------------------------

func TestZodSchema_ReturnsEdge(t *testing.T) {
	src := `const UserResponse = z.object({ id: z.number(), name: z.string() });
router.get('/users/:id', (req, res) => {
  return res.json(UserResponse.parse(loadUser(req.params.id)));
});`
	ents := extractFull(t, "custom_js_validation_schema", fi("get-user.ts", "typescript", src))
	if !hasDTOEdge(ents, "RETURNS", "Schema:UserResponse") {
		t.Fatal("expected RETURNS -> Schema:UserResponse")
	}
	se := schemaEntity(ents, "UserResponse")
	wantField(t, se, "id", "number")
}

// Dynamic / computed schema (built in a function from a variable) yields no
// concrete schema entity and no binding — honest-partial.
func TestDynamicSchema_NoBinding(t *testing.T) {
	src := `function makeSchema(fields) { return z.object(fields); }
router.post('/dyn', (req, res) => {
  makeSchema(cfg).parse(req.body);
  res.send('ok');
});`
	ents := extractFull(t, "custom_js_validation_schema", fi("dyn.ts", "typescript", src))
	// No top-level named schema → no ACCEPTS_INPUT edge.
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "ACCEPTS_INPUT" {
				t.Fatalf("dynamic schema must not bind: got edge %s -> %s", r.Kind, r.ToID)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Zod .refine() / .superRefine() / .transform() / .pipe() (issue #5497)
// ---------------------------------------------------------------------------

// refinementEntity returns the SCOPE.Schema/zod_refinement sub-entity by name.
func refinementEntity(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind == "SCOPE.Schema" && e.Subtype == "zod_refinement" && e.Name == name {
			return e
		}
	}
	return nil
}

// transformEntity returns the SCOPE.Schema/zod_transform sub-entity by name.
func transformEntity(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind == "SCOPE.Schema" && e.Subtype == "zod_transform" && e.Name == name {
			return e
		}
	}
	return nil
}

// A scalar zod schema with a single `.refine(fn, "msg")` → a zod_refinement
// entity carrying the literal message, linked to its host schema by a CONTAINS
// (member=refinement) edge.
func TestZodRefine_ScalarWithMessage(t *testing.T) {
	src := `import { z } from 'zod';
const Slug = z.string().refine(v => v.length > 3, "too short");`
	ents := extractFull(t, "custom_js_validation_schema", fi("slug.ts", "typescript", src))

	if schemaEntity(ents, "Slug") == nil {
		t.Fatal("expected SCOPE.Schema Slug")
	}
	ref := refinementEntity(ents, "Slug.refine#1")
	if ref == nil {
		t.Fatal("expected zod_refinement Slug.refine#1")
	}
	if ref.Properties["refinement_kind"] != "refine" {
		t.Errorf("refinement_kind = %q, want refine", ref.Properties["refinement_kind"])
	}
	if ref.Properties["message"] != "too short" {
		t.Errorf("message = %q, want 'too short' (props=%v)", ref.Properties["message"], ref.Properties)
	}
	if ref.Properties["parent_schema"] != "Slug" {
		t.Errorf("parent_schema = %q, want Slug", ref.Properties["parent_schema"])
	}
	if !hasContainsTo(ents, ref.ID, ref.Name) {
		t.Error("expected CONTAINS edge Slug -> Slug.refine#1")
	}
}

// `.superRefine((v,ctx)=>{})` → a zod_refinement with refinement_kind=superRefine
// (and no message — superRefine has no message argument).
func TestZodSuperRefine(t *testing.T) {
	src := `const Form = z.object({ a: z.string(), b: z.string() }).superRefine((v, ctx) => {});`
	ents := extractFull(t, "custom_js_validation_schema", fi("form.ts", "typescript", src))
	ref := refinementEntity(ents, "Form.refine#1")
	if ref == nil {
		t.Fatal("expected zod_refinement Form.refine#1")
	}
	if ref.Properties["refinement_kind"] != "superRefine" {
		t.Errorf("refinement_kind = %q, want superRefine", ref.Properties["refinement_kind"])
	}
	if _, ok := ref.Properties["message"]; ok {
		t.Errorf("superRefine should carry no message, got %q", ref.Properties["message"])
	}
	if !hasContainsTo(ents, ref.ID, ref.Name) {
		t.Error("expected CONTAINS edge Form -> Form.refine#1")
	}
}

// `z.object({}).transform(o => ({...o}))` → a zod_transform entity linked to its
// host schema by a CONTAINS (member=transform) edge.
func TestZodTransform(t *testing.T) {
	src := `const Norm = z.object({ name: z.string() }).transform(o => ({ ...o, name: o.name.trim() }));`
	ents := extractFull(t, "custom_js_validation_schema", fi("norm.ts", "typescript", src))
	tr := transformEntity(ents, "Norm.transform#1")
	if tr == nil {
		t.Fatal("expected zod_transform Norm.transform#1")
	}
	if tr.Properties["pattern_type"] != "transform" {
		t.Errorf("pattern_type = %q, want transform", tr.Properties["pattern_type"])
	}
	if !hasContainsTo(ents, tr.ID, tr.Name) {
		t.Error("expected CONTAINS edge Norm -> Norm.transform#1")
	}
}

// `.pipe(z.number())` records a pipe_target attribute on the host schema; an
// inline factory target yields the attribute but (correctly) no named REFERENCES
// edge. `.pipe(OtherSchema)` records a REFERENCES edge to the named schema.
func TestZodPipe(t *testing.T) {
	src := `const Other = z.number();
const Coerced = z.string().pipe(z.number());
const Chained = z.string().pipe(Other);`
	ents := extractFull(t, "custom_js_validation_schema", fi("pipe.ts", "typescript", src))

	coerced := schemaEntity(ents, "Coerced")
	if coerced == nil {
		t.Fatal("expected SCOPE.Schema Coerced")
	}
	if got := coerced.Properties["pipe_target"]; got != "z.number" {
		t.Errorf("Coerced pipe_target = %q, want z.number", got)
	}

	chained := schemaEntity(ents, "Chained")
	if chained == nil {
		t.Fatal("expected SCOPE.Schema Chained")
	}
	if got := chained.Properties["pipe_target"]; got != "Other" {
		t.Errorf("Chained pipe_target = %q, want Other", got)
	}
	// REFERENCES edge to the named target schema.
	found := false
	for _, r := range chained.Relationships {
		if r.Kind == "REFERENCES" && r.ToID == "Schema:Other" &&
			r.Properties["ref_kind"] == "zod_pipe" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected REFERENCES(zod_pipe) Chained -> Schema:Other, rels=%v", chained.Relationships)
	}
}

// Two chained `.refine()`s → two refinement entities, order preserved by
// chain_index, each with its own message.
func TestZodRefine_MultipleChained_OrderPreserved(t *testing.T) {
	src := `const Pwd = z.string()
  .refine(v => v.length >= 8, "too short")
  .refine(v => /[0-9]/.test(v), { message: "needs a digit" });`
	ents := extractFull(t, "custom_js_validation_schema", fi("pwd.ts", "typescript", src))

	r1 := refinementEntity(ents, "Pwd.refine#1")
	r2 := refinementEntity(ents, "Pwd.refine#2")
	if r1 == nil || r2 == nil {
		t.Fatalf("expected two refinements, got r1=%v r2=%v", r1, r2)
	}
	if r1.Properties["chain_index"] != "1" || r2.Properties["chain_index"] != "2" {
		t.Errorf("chain order: r1=%q r2=%q", r1.Properties["chain_index"], r2.Properties["chain_index"])
	}
	if r1.Properties["message"] != "too short" {
		t.Errorf("r1 message = %q, want 'too short'", r1.Properties["message"])
	}
	// Object-form message `{ message: "needs a digit" }`.
	if r2.Properties["message"] != "needs a digit" {
		t.Errorf("r2 message = %q, want 'needs a digit'", r2.Properties["message"])
	}
}

// A refinement + transform chained together → both entities, order preserved.
func TestZodRefineThenTransform_Chained(t *testing.T) {
	src := `const Tag = z.string().refine(v => v.length > 0, "empty").transform(v => v.toLowerCase());`
	ents := extractFull(t, "custom_js_validation_schema", fi("tag.ts", "typescript", src))
	r := refinementEntity(ents, "Tag.refine#1")
	tr := transformEntity(ents, "Tag.transform#2")
	if r == nil {
		t.Fatal("expected refinement Tag.refine#1")
	}
	if tr == nil {
		t.Fatal("expected transform Tag.transform#2")
	}
	if r.Properties["message"] != "empty" {
		t.Errorf("refine message = %q, want 'empty'", r.Properties["message"])
	}
}

// Negative: a zod schema with NO refine/transform/pipe still works (field
// extraction + binding intact) and emits no refinement/transform entities.
func TestZodSchema_NoRefine_StillWorks(t *testing.T) {
	src := `const Plain = z.object({ name: z.string(), age: z.number() });
router.post('/p', (req, res) => { Plain.parse(req.body); res.json({}); });`
	ents := extractFull(t, "custom_js_validation_schema", fi("plain.ts", "typescript", src))

	se := schemaEntity(ents, "Plain")
	if se == nil {
		t.Fatal("expected SCOPE.Schema Plain")
	}
	wantField(t, se, "name", "string")
	if !hasDTOEdge(ents, "ACCEPTS_INPUT", "Schema:Plain") {
		t.Fatal("expected ACCEPTS_INPUT -> Schema:Plain")
	}
	for _, e := range ents {
		if e.Subtype == "zod_refinement" || e.Subtype == "zod_transform" {
			t.Fatalf("no-refine schema must not emit %s entity %q", e.Subtype, e.Name)
		}
	}
	if _, ok := se.Properties["pipe_target"]; ok {
		t.Errorf("no-pipe schema must not carry pipe_target, got %q", se.Properties["pipe_target"])
	}
}
