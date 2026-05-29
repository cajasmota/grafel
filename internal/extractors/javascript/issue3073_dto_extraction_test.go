// Package javascript_test — issue #3073: schema-library DTO extraction for the
// Express/Fastify family (Zod / Joi / Yup / AJV).
//
// When a route handler uses a top-level schema variable (e.g.
// `const userSchema = z.object({...})`) the extractor must:
//
//  1. Emit a SCOPE.Schema("dto") entity for the schema variable.
//  2. Emit a VALIDATES edge with via="dto_extraction" from the handler
//     to `dto:<schemaVarName>` when the handler calls a schema usage method
//     (.parse / .safeParse / .validate / .validateSync / .compile).
//
// Tests cover four libraries (Zod, Joi, Yup, AJV), the conservative
// false-positive gate (a plain `obj.validate()` on an unknown receiver must
// NOT emit a dto_extraction edge), and per-framework fixture files.
package javascript_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// hasDTOSchemaEntity checks that an entity with the given name was emitted as
// SCOPE.Schema with subtype="dto".
func hasDTOSchemaEntity(ents []types.EntityRecord, name string) bool {
	for _, e := range ents {
		if e.Name == name && e.Kind == "SCOPE.Schema" && e.Subtype == "dto" {
			return true
		}
	}
	return false
}

// hasDTOExtractionEdge checks that fromName has a VALIDATES edge with
// via="dto_extraction" pointing to `dto:<schemaName>`.
func hasDTOExtractionEdge(ents []types.EntityRecord, fromName, schemaName string) bool {
	return hasValidatesEdge(ents, fromName, "dto:"+schemaName, "dto_extraction")
}

// ---------------------------------------------------------------------------
// Unit tests — individual library patterns
// ---------------------------------------------------------------------------

// TestDTOExtraction_Zod_ObjectSchema verifies that a top-level z.object const
// is emitted as SCOPE.Schema("dto") and that a handler calling schema.parse
// receives a dto_extraction VALIDATES edge.
func TestDTOExtraction_Zod_ObjectSchema(t *testing.T) {
	src := `
import { z } from 'zod'
const createUserSchema = z.object({ name: z.string(), age: z.number() })
export function createUser(req, res) {
  const data = createUserSchema.parse(req.body)
  res.json(data)
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasDTOSchemaEntity(ents, "createUserSchema") {
		t.Errorf("expected SCOPE.Schema(dto) entity for createUserSchema; entities: %v", entityNames(ents))
	}
	if !hasDTOExtractionEdge(ents, "createUser", "createUserSchema") {
		t.Errorf("expected dto_extraction VALIDATES edge createUser→dto:createUserSchema")
	}
}

// TestDTOExtraction_Zod_SafeParse verifies that .safeParse() also triggers the
// dto_extraction edge (not only .parse).
func TestDTOExtraction_Zod_SafeParse(t *testing.T) {
	src := `
import { z } from 'zod'
const loginSchema = z.object({ user: z.string(), pass: z.string() })
export async function login(req, res) {
  const result = loginSchema.safeParse(req.body)
  res.json({ ok: result.success })
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasDTOSchemaEntity(ents, "loginSchema") {
		t.Errorf("expected SCOPE.Schema(dto) entity for loginSchema")
	}
	if !hasDTOExtractionEdge(ents, "login", "loginSchema") {
		t.Errorf("expected dto_extraction VALIDATES edge login→dto:loginSchema")
	}
}

// TestDTOExtraction_Joi_ObjectSchema verifies that a top-level Joi.object const
// is emitted as SCOPE.Schema("dto") and that a handler calling schema.validate
// receives a dto_extraction VALIDATES edge.
func TestDTOExtraction_Joi_ObjectSchema(t *testing.T) {
	src := `
import Joi from 'joi'
const updateSchema = Joi.object({ name: Joi.string(), email: Joi.string() })
export function update(req, res) {
  const { error, value } = updateSchema.validate(req.body)
  res.json(value)
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasDTOSchemaEntity(ents, "updateSchema") {
		t.Errorf("expected SCOPE.Schema(dto) entity for updateSchema")
	}
	if !hasDTOExtractionEdge(ents, "update", "updateSchema") {
		t.Errorf("expected dto_extraction VALIDATES edge update→dto:updateSchema")
	}
}

// TestDTOExtraction_Yup_ObjectSchema verifies that a top-level yup.object const
// is emitted as SCOPE.Schema("dto") and that a handler calling schema.validateSync
// receives a dto_extraction VALIDATES edge.
func TestDTOExtraction_Yup_ObjectSchema(t *testing.T) {
	src := `
import * as yup from 'yup'
const profileSchema = yup.object({ name: yup.string(), age: yup.number() })
export async function saveProfile(req, res) {
  const data = profileSchema.validateSync(req.body)
  res.json(data)
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasDTOSchemaEntity(ents, "profileSchema") {
		t.Errorf("expected SCOPE.Schema(dto) entity for profileSchema")
	}
	if !hasDTOExtractionEdge(ents, "saveProfile", "profileSchema") {
		t.Errorf("expected dto_extraction VALIDATES edge saveProfile→dto:profileSchema")
	}
}

// TestDTOExtraction_Yup_ValidateAsync verifies that yup's async .validate()
// also triggers the dto_extraction edge.
func TestDTOExtraction_Yup_ValidateAsync(t *testing.T) {
	src := `
import * as yup from 'yup'
const itemSchema = yup.object({ title: yup.string(), qty: yup.number() })
export async function createItem(ctx) {
  const validated = await itemSchema.validate(ctx.request.body)
  ctx.body = validated
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasDTOSchemaEntity(ents, "itemSchema") {
		t.Errorf("expected SCOPE.Schema(dto) entity for itemSchema")
	}
	if !hasDTOExtractionEdge(ents, "createItem", "itemSchema") {
		t.Errorf("expected dto_extraction VALIDATES edge createItem→dto:itemSchema")
	}
}

// TestDTOExtraction_AJV_Compile verifies that a top-level ajv.compile const
// is emitted as SCOPE.Schema("dto"). Note: AJV's compile returns a validate fn,
// so only the schema entity emission is asserted (the returned fn call is opaque).
func TestDTOExtraction_AJV_Compile(t *testing.T) {
	src := `
import Ajv from 'ajv'
const ajv = new Ajv()
const validateUser = ajv.compile({ type: 'object', properties: { name: { type: 'string' } } })
export function createUser(req, res) {
  const valid = validateUser(req.body)
  res.json({ ok: valid })
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasDTOSchemaEntity(ents, "validateUser") {
		t.Errorf("expected SCOPE.Schema(dto) entity for validateUser; entities: %v", entityNames(ents))
	}
}

// TestDTOExtraction_MultipleSchemas verifies that multiple schema-lib DTO
// variables in the same file each get their own entity and edge.
func TestDTOExtraction_MultipleSchemas(t *testing.T) {
	src := `
import { z } from 'zod'
const createSchema = z.object({ name: z.string() })
const updateSchema = z.object({ name: z.string(), id: z.number() })
export function create(req, res) {
  const data = createSchema.parse(req.body)
  res.json(data)
}
export function update(req, res) {
  const data = updateSchema.parse(req.body)
  res.json(data)
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasDTOSchemaEntity(ents, "createSchema") {
		t.Errorf("expected SCOPE.Schema(dto) for createSchema")
	}
	if !hasDTOSchemaEntity(ents, "updateSchema") {
		t.Errorf("expected SCOPE.Schema(dto) for updateSchema")
	}
	if !hasDTOExtractionEdge(ents, "create", "createSchema") {
		t.Errorf("expected dto_extraction edge create→dto:createSchema")
	}
	if !hasDTOExtractionEdge(ents, "update", "updateSchema") {
		t.Errorf("expected dto_extraction edge update→dto:updateSchema")
	}
}

// TestDTOExtraction_NoFalsePositive_UnknownReceiver verifies that a
// `.validate()` call on a receiver that was NOT declared as a schema-lib
// variable does NOT emit a dto_extraction edge.
func TestDTOExtraction_NoFalsePositive_UnknownReceiver(t *testing.T) {
	src := `
const formHelper = { validate: () => true }
export function go(req, res) {
  formHelper.validate()
  res.json({ ok: true })
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	e := findByNameRel(ents, "go")
	if e == nil {
		t.Fatal("go entity not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "VALIDATES" && r.Properties["via"] == "dto_extraction" {
			t.Fatalf("unexpected dto_extraction edge on go: %+v", r)
		}
	}
}

// TestDTOExtraction_NoFalsePositive_FunctionScopeSchema verifies that a
// schema variable declared INSIDE a function body (not top-level) does NOT
// produce a SCOPE.Schema("dto") entity or a dto_extraction edge.
func TestDTOExtraction_NoFalsePositive_FunctionScopeSchema(t *testing.T) {
	src := `
import { z } from 'zod'
export function handler(req, res) {
  const schema = z.object({ name: z.string() })
  const data = schema.parse(req.body)
  res.json(data)
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	// The inline schema must NOT be emitted as SCOPE.Schema("dto").
	if hasDTOSchemaEntity(ents, "schema") {
		t.Errorf("schema declared inside function should NOT be SCOPE.Schema(dto)")
	}
	// The handler may still get a request_validation edge (via the existing
	// extractValidationEdge path) but NOT a dto_extraction edge.
	e := findByNameRel(ents, "handler")
	if e != nil {
		for _, r := range e.Relationships {
			if r.Kind == "VALIDATES" && r.Properties["via"] == "dto_extraction" {
				t.Errorf("unexpected dto_extraction edge on handler for inline schema: %+v", r)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Fixture tests — one per framework, proving dto_extraction on real fixtures
// ---------------------------------------------------------------------------

// TestDTOExtraction_Fixtures runs the extractor on every per-framework
// proving fixture and asserts: (a) at least one SCOPE.Schema("dto") entity
// is emitted, and (b) at least one VALIDATES edge with via=dto_extraction is
// produced. These fixtures back the registry cells for dto_extraction across
// the Express/Fastify family.
func TestDTOExtraction_Fixtures(t *testing.T) {
	cases := []struct {
		file string
	}{
		{"express_dto.ts"},
		{"fastify_dto.ts"},
		{"koa_dto.ts"},
		{"hapi_dto.ts"},
		{"hono_dto.ts"},
		{"feathers_dto.ts"},
		{"polka_dto.ts"},
		{"restify_dto.ts"},
		{"marblejs_dto.ts"},
		{"sails_dto.ts"},
	}
	root := filepath.Join("..", "..", "..", "testdata", "fixtures", "typescript")
	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(root, tc.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			src := string(b)
			tree := parseTSRel(t, b)
			ents := runJS(t, src, "typescript", tree)

			// Must have at least one SCOPE.Schema("dto") entity.
			foundSchema := false
			for _, e := range ents {
				if e.Kind == "SCOPE.Schema" && e.Subtype == "dto" {
					foundSchema = true
					break
				}
			}
			if !foundSchema {
				t.Errorf("%s: expected at least one SCOPE.Schema(dto) entity", tc.file)
			}

			// Must have at least one VALIDATES edge with via=dto_extraction.
			foundEdge := false
			for _, e := range ents {
				for _, r := range e.Relationships {
					if r.Kind == "VALIDATES" && r.Properties["via"] == "dto_extraction" {
						foundEdge = true
					}
				}
			}
			if !foundEdge {
				t.Errorf("%s: expected at least one VALIDATES edge via=dto_extraction", tc.file)
			}
		})
	}
}
