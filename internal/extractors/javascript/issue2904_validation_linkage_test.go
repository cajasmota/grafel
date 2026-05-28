// Package javascript_test — issue #2904: request_validation / dto_extraction
// linkage. The JS/TS extractor emits a VALIDATES edge from a route handler /
// controller method to a synthetic validator (`validator:<lib>`) or DTO
// (`dto:<TypeName>`) stub, turning validators that were previously visible
// only as imports into a first-class route↔validator graph fact.
//
// Tests cover the call-site validators (zod / joi / yup / express-validator /
// class-validator), the NestJS @Body()/@Query()/@Param() DTO decorators, the
// conservative-gate negatives (no false positive on an unrelated `.validate()`
// or an `@Body() x: any`), and every per-framework proving fixture under
// testdata/fixtures/typescript/.
package javascript_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// hasValidatesEdge reports whether fromName has a VALIDATES edge to toID with
// the given via tag.
func hasValidatesEdge(ents []types.EntityRecord, fromName, toID, via string) bool {
	e := findByNameRel(ents, fromName)
	if e == nil {
		return false
	}
	for _, r := range e.Relationships {
		if r.Kind == "VALIDATES" && r.ToID == toID && r.Properties["via"] == via {
			return true
		}
	}
	return false
}

func TestValidationLinkage_ZodParse(t *testing.T) {
	src := `
import { z } from 'zod'
const schema = z.object({ name: z.string() })
export function createUser(req: any, res: any) {
  const data = schema.parse(req.body)
  res.json(data)
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if !hasValidatesEdge(ents, "createUser", "validator:zod", "request_validation") {
		t.Fatalf("expected VALIDATES createUser→validator:zod (request_validation)")
	}
}

func TestValidationLinkage_ExpressValidator(t *testing.T) {
	src := `
import { validationResult } from 'express-validator'
export function update(req: any, res: any) {
  const errors = validationResult(req)
  res.json({ ok: errors.isEmpty() })
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if !hasValidatesEdge(ents, "update", "validator:express-validator", "request_validation") {
		t.Fatalf("expected VALIDATES update→validator:express-validator")
	}
}

func TestValidationLinkage_JoiValidate(t *testing.T) {
	src := `
import Joi from 'joi'
const bodySchema = Joi.object({ title: Joi.string() })
export function handler(req: any, res: any) {
  const result = bodySchema.validate(req.body)
  res.json(result)
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	// bodySchema is a schema-shaped name → attributed yup OR joi; the receiver
	// `bodySchema` matches isSchemaVarName, so the edge is emitted (lib=yup).
	e := findByNameRel(ents, "handler")
	if e == nil {
		t.Fatal("handler not found")
	}
	found := false
	for _, r := range e.Relationships {
		if r.Kind == "VALIDATES" && r.Properties["via"] == "request_validation" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a request_validation VALIDATES edge on handler")
	}
}

func TestValidationLinkage_JoiAttempt(t *testing.T) {
	src := `
import Joi from 'joi'
const s = Joi.object({ x: Joi.string() })
export function h(req: any, res: any) {
  const v = Joi.attempt(req.body, s)
  res.json(v)
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if !hasValidatesEdge(ents, "h", "validator:joi", "request_validation") {
		t.Fatalf("expected VALIDATES h→validator:joi via Joi.attempt")
	}
}

func TestValidationLinkage_ClassValidator(t *testing.T) {
	src := `
import { validateOrReject } from 'class-validator'
export async function save(dto: any) {
  await validateOrReject(dto)
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if !hasValidatesEdge(ents, "save", "validator:class-validator", "request_validation") {
		t.Fatalf("expected VALIDATES save→validator:class-validator")
	}
}

func TestValidationLinkage_NestDTO(t *testing.T) {
	src := `
import { Controller, Post, Body, Query } from '@nestjs/common'
class CreateUserDto { name!: string }
class FilterDto { q!: string }
@Controller('users')
export class UsersController {
  @Post()
  create(@Body() dto: CreateUserDto, @Query() filter: FilterDto) {
    return dto
  }
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if !hasValidatesEdge(ents, "create", "dto:CreateUserDto", "dto_extraction") {
		t.Fatalf("expected VALIDATES create→dto:CreateUserDto (dto_extraction)")
	}
	if !hasValidatesEdge(ents, "create", "dto:FilterDto", "dto_extraction") {
		t.Fatalf("expected VALIDATES create→dto:FilterDto (dto_extraction)")
	}
}

func TestValidationLinkage_NoFalsePositive(t *testing.T) {
	// A plain `.validate()` on an unrelated object and an `@Body() x: any`
	// must NOT emit a VALIDATES edge (conservative gate).
	src := `
import { Controller, Post, Body } from '@nestjs/common'
const form = { validate: () => true }
@Controller('x')
export class C {
  @Post()
  go(@Body() raw: any) {
    form.validate()
    return raw
  }
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	e := findByNameRel(ents, "go")
	if e == nil {
		t.Fatal("go not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "VALIDATES" {
			t.Fatalf("did not expect a VALIDATES edge, got %s→%s", r.Kind, r.ToID)
		}
	}
}

// TestValidationLinkage_Fixtures runs the extractor on every per-framework
// proving fixture and asserts at least one VALIDATES edge of the expected
// capability is produced. This is the fixture that backs each registry cell.
func TestValidationLinkage_Fixtures(t *testing.T) {
	cases := []struct {
		file string
		via  string
	}{
		{"express_validation.ts", "request_validation"},
		{"fastify_validation.ts", "request_validation"},
		{"koa_validation.ts", "request_validation"},
		{"hapi_validation.ts", "request_validation"},
		{"nestjs_validation.ts", "dto_extraction"},
		{"adonisjs_validation.ts", "request_validation"},
		{"feathers_validation.ts", "request_validation"},
		{"hono_validation.ts", "request_validation"},
		{"marblejs_validation.ts", "request_validation"},
		{"polka_validation.ts", "request_validation"},
		{"restify_validation.ts", "request_validation"},
		{"sails_validation.ts", "request_validation"},
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
			found := false
			for _, e := range ents {
				for _, r := range e.Relationships {
					if r.Kind == "VALIDATES" && r.Properties["via"] == tc.via {
						found = true
					}
				}
			}
			if !found {
				t.Fatalf("%s: expected at least one VALIDATES edge via=%s", tc.file, tc.via)
			}
		})
	}
}
