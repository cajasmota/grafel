// Request-validation / DTO-extraction linkage for JS/TS backend HTTP
// frameworks (#2904).
//
// Validator libraries (zod, joi/@hapi/joi, yup, express-validator,
// class-validator) were previously visible only as imports — there was no
// graph edge tying the validator to the route handler that uses it, so the
// coverage matrix's Validation column was all-`—` (untracked) for every
// JS/TS backend. This file builds that linkage:
//
//   - Call-site validation (request_validation): inside a handler body a
//     call like `Schema.parse(req.body)` / `schema.safeParse(...)` (zod),
//     `schema.validate(...)` (joi/yup), `Joi.attempt(...)`,
//     `validationResult(req)` / `check('x')` / `body('x')`
//     (express-validator), or `validate(dto)` / `validateOrReject(dto)`
//     (class-validator) emits a VALIDATES edge from the enclosing
//     operation to a synthetic `validator:<lib>` stub.
//
//   - Parameter-decorator DTO extraction (dto_extraction): a NestJS
//     controller method parameter `@Body() dto: CreateUserDto` (or
//     @Query()/@Param()) emits a VALIDATES edge from the method to a
//     synthetic `dto:<TypeName>` stub.
//
// All emission is append-only and deterministic: identical source yields
// identical edges in stable order. The matched library is attributed on
// the edge so MCP queries can distinguish zod- vs joi- vs Nest-validated
// routes. Detection is intentionally conservative — only well-known
// validator method names / decorator names match, so an ordinary
// `.validate()` on a non-validator object is not over-claimed (the
// receiver / decorator gate keeps false positives out).
package javascript

import (
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
	sitter "github.com/smacker/go-tree-sitter"
)

// validatorMethodLibrary maps a recognised validation method name to the
// validator library that owns it. The receiver shape disambiguates the
// libraries that share `validate` (joi vs yup vs class-validator) below;
// the bare-function names (validationResult/check/body, validate/
// validateOrReject) are gated on their own.
//
// zod's `.parse` / `.parseAsync` / `.safeParse[Async]` are zod-specific
// member methods. joi/yup share `.validate` (joi also has `.validateAsync`,
// yup also has `.validateSync`); both are attributed by receiver heuristics.
var zodValidationMethods = map[string]bool{
	"parse":          true,
	"parseAsync":     true,
	"safeParse":      true,
	"safeParseAsync": true,
}

// expressValidatorFuncs are the bare-identifier express-validator entry
// points used inside a handler / middleware chain.
var expressValidatorFuncs = map[string]bool{
	"validationResult": true,
	"check":            true,
	"body":             true,
	"param":            true,
	"query":            true,
	"matchedData":      true,
}

// classValidatorFuncs are the bare-identifier class-validator entry points.
var classValidatorFuncs = map[string]bool{
	"validate":         true,
	"validateOrReject": true,
	"validateSync":     true,
}

// dtoDecorators are the NestJS parameter decorators that bind a typed DTO
// to a controller method (the dto_extraction idiom).
var dtoDecorators = map[string]bool{
	"Body":  true,
	"Query": true,
	"Param": true,
}

// extractValidationEdges inspects a single call_expression and returns a
// VALIDATES RelationshipRecord when the call is a recognised validator
// invocation. Returns nil (and ok=false) for non-validator calls.
//
// FromID is left empty so buildDocument substitutes the enclosing
// operation's entity ID at emit time (the same contract as every other
// per-body relationship emitter). ToID is the synthetic `validator:<lib>`
// stub.
func (x *extractor) extractValidationEdge(call *sitter.Node) (types.RelationshipRecord, bool) {
	if call == nil || call.Type() != "call_expression" {
		return types.RelationshipRecord{}, false
	}
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return types.RelationshipRecord{}, false
	}
	line := strconv.Itoa(int(call.StartPoint().Row) + 1)

	switch fn.Type() {
	case "member_expression":
		prop := x.nodeText(fn.ChildByFieldName("property"))
		obj := fn.ChildByFieldName("object")
		recv := x.nodeText(obj)
		recvLeaf := memberLeaf(recv)
		switch {
		case zodValidationMethods[prop]:
			return validatesEdge("zod", prop, line), true
		case prop == "validate" || prop == "validateAsync":
			// joi: `Joi.<...>.validate(...)` or a schema built from `Joi`.
			if recvMentionsJoi(recv) {
				return validatesEdge("joi", prop, line), true
			}
			// yup / joi schema variable: receiver is a schema-shaped name.
			if isSchemaVarName(recvLeaf) {
				return validatesEdge("yup", prop, line), true
			}
		case prop == "validateSync":
			// yup-specific synchronous validation.
			if isSchemaVarName(recvLeaf) || recvMentionsYup(recv) {
				return validatesEdge("yup", prop, line), true
			}
		case prop == "attempt":
			// joi: `Joi.attempt(value, schema)`.
			if recvMentionsJoi(recv) {
				return validatesEdge("joi", prop, line), true
			}
		}
	case "identifier":
		name := x.nodeText(fn)
		switch {
		case expressValidatorFuncs[name]:
			return validatesEdge("express-validator", name, line), true
		case classValidatorFuncs[name]:
			return validatesEdge("class-validator", name, line), true
		}
	}
	return types.RelationshipRecord{}, false
}

// validatesEdge builds a request_validation VALIDATES edge to the
// `validator:<lib>` stub.
func validatesEdge(library, method, line string) types.RelationshipRecord {
	return types.RelationshipRecord{
		ToID: "validator:" + library,
		Kind: string(types.RelationshipKindValidates),
		Properties: map[string]string{
			"library": library,
			"method":  method,
			"line":    line,
			"via":     "request_validation",
		},
	}
}

// extractDTOParamEdges scans a method/function parameter list for NestJS
// `@Body()/@Query()/@Param() x: SomeDto` decorated parameters and returns a
// VALIDATES edge (via=dto_extraction) per distinct DTO type. Returns nil
// when no decorated DTO parameter is present.
//
// The TS grammar shapes a decorated parameter as a `required_parameter`
// (or `optional_parameter`) whose first child is a `decorator` node and
// which carries a `type` annotation. We require BOTH a recognised DTO
// decorator AND a class-shaped (PascalCase, non-primitive) type so a bare
// `@Body() body: any` is not over-claimed.
func (x *extractor) extractDTOParamEdges(params *sitter.Node) []types.RelationshipRecord {
	if params == nil {
		return nil
	}
	var rels []types.RelationshipRecord
	seen := map[string]bool{}
	for i := 0; i < int(params.ChildCount()); i++ {
		p := params.Child(i)
		if p == nil {
			continue
		}
		if p.Type() != "required_parameter" && p.Type() != "optional_parameter" {
			continue
		}
		dec := paramDTODecorator(x, p)
		if dec == "" {
			continue
		}
		tn := p.ChildByFieldName("type")
		if tn == nil {
			continue
		}
		typeName := dtoTypeName(x.nodeText(tn))
		if typeName == "" || seen[typeName] {
			continue
		}
		seen[typeName] = true
		rels = append(rels, types.RelationshipRecord{
			ToID: "dto:" + typeName,
			Kind: string(types.RelationshipKindValidates),
			Properties: map[string]string{
				"library": "nestjs-dto",
				"method":  "@" + dec + "()",
				"dto":     typeName,
				"line":    strconv.Itoa(int(p.StartPoint().Row) + 1),
				"via":     "dto_extraction",
			},
		})
	}
	// Deterministic order by DTO type name.
	sort.Slice(rels, func(i, j int) bool { return rels[i].ToID < rels[j].ToID })
	return rels
}

// paramDTODecorator returns the recognised NestJS DTO decorator name
// (Body/Query/Param) applied to a parameter node, or "" when none.
func paramDTODecorator(x *extractor, param *sitter.Node) string {
	for i := 0; i < int(param.ChildCount()); i++ {
		c := param.Child(i)
		if c == nil || c.Type() != "decorator" {
			continue
		}
		name := decoratorLeafName(x, c)
		if dtoDecorators[name] {
			return name
		}
	}
	return ""
}

// decoratorLeafName returns the leaf identifier of a `decorator` node,
// handling both `@Body` and `@Body()` (call_expression) shapes.
func decoratorLeafName(x *extractor, dec *sitter.Node) string {
	for i := 0; i < int(dec.ChildCount()); i++ {
		c := dec.Child(i)
		if c == nil {
			continue
		}
		switch c.Type() {
		case "identifier":
			return x.nodeText(c)
		case "call_expression":
			if fn := c.ChildByFieldName("function"); fn != nil {
				return memberLeaf(x.nodeText(fn))
			}
		}
	}
	return ""
}

// dtoTypeName normalises a type-annotation string ("`: CreateUserDto`",
// "`: UpdateDto<Partial>`") to its leaf identifier, rejecting primitives
// and lowercase / structural shapes (so `@Body() b: any` is dropped).
func dtoTypeName(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), ":"))
	if idx := strings.IndexAny(s, "<|&[ "); idx >= 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	switch s {
	case "", "string", "number", "boolean", "any", "void", "object", "unknown", "never", "Object":
		return ""
	}
	if s[0] < 'A' || s[0] > 'Z' {
		return ""
	}
	return s
}

// memberLeaf returns the trailing identifier of a (possibly dotted)
// member-expression text, e.g. "Joi.object" → "object", "schema" →
// "schema".
func memberLeaf(s string) string {
	if idx := strings.LastIndex(s, "."); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

// recvMentionsJoi reports whether a receiver expression text references the
// joi entry symbol (`Joi` / `joi`), e.g. `Joi`, `Joi.object()`, or a chain
// rooted at it.
func recvMentionsJoi(recv string) bool {
	root := memberRoot(recv)
	return root == "Joi" || root == "joi"
}

// recvMentionsYup reports whether a receiver expression text references the
// yup entry symbol (`yup` / `Yup`).
func recvMentionsYup(recv string) bool {
	root := memberRoot(recv)
	return root == "yup" || root == "Yup"
}

// memberRoot returns the leading identifier of a dotted member-expression
// text, e.g. "Joi.object().keys" → "Joi".
func memberRoot(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexAny(s, ".(["); idx >= 0 {
		return s[:idx]
	}
	return s
}

// isSchemaVarName reports whether a bare receiver identifier looks like a
// validation-schema variable: it contains "schema" (case-insensitive) or
// ends in "Schema". This gates the shared `.validate()` method so a plain
// `something.validate()` on an unrelated object is not claimed as a
// validator call.
func isSchemaVarName(name string) bool {
	if name == "" {
		return false
	}
	lower := strings.ToLower(name)
	return strings.Contains(lower, "schema") || strings.HasSuffix(name, "Schema")
}
