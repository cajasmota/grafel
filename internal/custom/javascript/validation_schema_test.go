package javascript_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
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
