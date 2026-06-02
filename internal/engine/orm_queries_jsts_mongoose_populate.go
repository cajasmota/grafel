// Mongoose / @nestjs/mongoose `ref` + `.populate()` join-equivalent extraction
// for JS/TS (#3844, epic #3837).
//
// MongoDB's dominant application-side "join" in the Mongoose / NestJS world is
// NOT `$lookup` — it is a schema reference field declared with `ref:` plus a
// runtime `.populate('field')` call. The `$lookup` aggregation case is already
// handled by scanJSMongoAggregation (orm_queries_jsts_mongo_agg.go) which emits
// JOINS_COLLECTION edges. This sibling pass brings the ref/populate idiom up to
// PARITY with that contract so the implicit reference join surfaces the same
// way a `$lookup` does — which is what the NestJS rewrite target needs for
// data-join topology comparison against the Django parity oracle.
//
// Two declaration forms feed the ref side:
//
//  1. Classic Mongoose schema literal — a field whose definition object carries
//     `ref: 'Author'`:
//     const BookSchema = new mongoose.Schema({
//     author: { type: Schema.Types.ObjectId, ref: 'Author' },
//     });
//     The owning collection/model is recovered from the schema variable
//     (`BookSchema` → `Book`) or, preferentially, from a `mongoose.model('Book',
//     BookSchema)` registration in the same file.
//
//  2. @nestjs/mongoose decorator — a `@Prop({ ..., ref: 'Author' })` property
//     inside a `@Schema()`-annotated class:
//     @Schema()
//     class Book {
//     @Prop({ type: Types.ObjectId, ref: 'Author' }) author: Author;
//     }
//     The owning model is the decorated class name (`Book`).
//
// The runtime side is a static `.populate('author')` call. The field named in
// the populate must match a declared `ref:` field for the join to be emitted —
// this couples the reference declaration to its actual traversal and keeps the
// edge honest (a `ref:` that is never populated, or a populate of a field with
// no static `ref:`, is not a confirmed application-side join here).
//
// EMITTED EDGE. For each (ref-field, populate-field) match with a STATICALLY
// resolvable `ref:` target and owning model, one JOINS_COLLECTION relationship
// matching the Python/$lookup contract:
//   - FromID = Class:<capitalisedSingular(owning model/collection)>
//   - ToID   = Class:<capitalisedSingular(ref target)>
//   - Properties: pattern_type=mongoose_populate, via=populate, ref_field,
//     ref, plus the populated field.
//
// Downstream shared_db_coupling.go (Pass 8.8) consumes JOINS_COLLECTION the
// same way it does for $lookup.
//
// HONEST LIMIT. A dynamic `ref:` (e.g. `ref: refVar` / `refPath: ...`) or a
// dynamic `.populate(field)` (a bare identifier / member expression rather than
// a string literal) is NOT resolvable statically and yields NO edge — never a
// fabricated join. Resolution is file-local: the owning model and the ref
// target must both be recoverable from the current file.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// mongoosePopulatePatternType tags every edge this pass emits.
const mongoosePopulatePatternType = "mongoose_populate"

// reMongooseModelRegistration captures a `mongoose.model('Book', BookSchema)`
// registration so the owning collection name can be taken from the registered
// model name (authoritative) rather than inferred from the schema variable.
var reMongooseModelRegistration = regexp.MustCompile(
	`(?:mongoose\.)?model\s*(?:<[^>]*>)?\s*\(\s*['"` + "`" + `]([A-Za-z0-9_]+)['"` + "`" + `]\s*,\s*([A-Za-z_$][\w$]*)\s*`,
)

// reMongooseSchemaVar captures a `const BookSchema = new (mongoose.)Schema(`
// schema-variable declaration.
var reMongooseSchemaVar = regexp.MustCompile(
	`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*(?::[^=]+)?=\s*new\s+(?:mongoose\.)?Schema\s*\(`,
)

// reNestSchemaClass captures a `@Schema()` decorated class name in a
// @nestjs/mongoose definition: `@Schema(...) ... class Book {`.
var reNestSchemaClass = regexp.MustCompile(
	`@Schema\s*\([^)]*\)\s*(?:export\s+)?class\s+([A-Za-z_$][\w$]*)`,
)

// reMongoosePopulateField matches a static `.populate('field')` call and
// captures the populated field name. A dynamic populate (bare identifier /
// member access, no string literal) does not match — honest skip.
var reMongoosePopulateField = regexp.MustCompile(
	`\.populate\s*\(\s*['"` + "`" + `]([A-Za-z_$][\w$.]*)['"` + "`" + `]`,
)

// reNestPropRef matches a `@Prop({ ... ref: 'Author' ... }) fieldName` NestJS
// decorator and captures (refTarget, fieldName). The decorator object must
// carry a static string `ref:`; the property name follows on the same/next
// tokens. refPath / dynamic refs are not matched.
var reNestPropRef = regexp.MustCompile(
	`@Prop\s*\(\s*\{[^}]*\bref\s*:\s*['"` + "`" + `]([A-Za-z0-9_]+)['"` + "`" + `][^}]*\}\s*\)\s*(?:readonly\s+)?([A-Za-z_$][\w$]*)`,
)

// reStaticRefValue matches a static string `ref: 'Author'` value (used to gate
// out dynamic refs when scanning a schema field object).
var reStaticRefValue = regexp.MustCompile(
	`\bref\s*:\s*['"` + "`" + `]([A-Za-z0-9_]+)['"` + "`" + `]`,
)

// mongooseRefField is a declared reference field: the property name, its `ref:`
// target collection/model, and the owning model the field belongs to.
type mongooseRefField struct {
	field string // schema field / property name (e.g. "author")
	ref   string // ref target model name (e.g. "Author")
	owner string // owning model/collection name (e.g. "Book")
}

// scanJSMongoosePopulateJoins finds Mongoose / @nestjs/mongoose reference
// fields (`ref:` in a schema literal or a `@Prop`) that are actually traversed
// by a static `.populate('field')`, and emits one JOINS_COLLECTION edge per
// confirmed (owner → ref) application-side join, matching the $lookup contract.
func scanJSMongoosePopulateJoins(
	src string,
	path string,
	lang string,
	emitJoin func(rel types.RelationshipRecord),
) {
	// Gate: only run where a mongoose surface is plausible. @nestjs/mongoose
	// re-exports Schema/Prop from 'mongoose', so the mongoose import gate also
	// covers the Nest decorator form.
	if !mentionsMongooseSequelize(src) {
		return
	}

	// The populated fields actually traversed at runtime (static literals only).
	populated := mongoosePopulatedFields(src)
	if len(populated) == 0 {
		// No static populate traversal → no confirmed reference join to emit.
		return
	}

	// Collect declared ref fields from both forms.
	refFields := mongooseCollectSchemaRefs(src)
	refFields = append(refFields, mongooseCollectNestPropRefs(src)...)
	if len(refFields) == 0 {
		return
	}

	seen := make(map[string]bool)
	for _, rf := range refFields {
		if rf.ref == "" || rf.owner == "" || rf.field == "" {
			continue
		}
		if !populated[rf.field] {
			// Declared but never populated → not a confirmed traversal here.
			continue
		}
		from := capitalisedSingular(rf.owner)
		to := capitalisedSingular(rf.ref)
		if from == "" || to == "" {
			continue
		}
		key := from + "->" + to + ":" + rf.field
		if seen[key] {
			continue
		}
		seen[key] = true
		emitJoin(types.RelationshipRecord{
			FromID: fmt.Sprintf("Class:%s", from),
			ToID:   fmt.Sprintf("Class:%s", to),
			Kind:   string(types.RelationshipKindJoinsCollection),
			Properties: map[string]string{
				"pattern_type": mongoosePopulatePatternType,
				"via":          "populate",
				"ref":          rf.ref,
				"ref_field":    rf.field,
			},
		})
	}
}

// mongoosePopulatedFields returns the set of field names traversed by a static
// `.populate('field')` call. Nested paths (`author.publisher`) contribute their
// first segment so a top-level ref field still matches.
func mongoosePopulatedFields(src string) map[string]bool {
	out := make(map[string]bool)
	for _, m := range reMongoosePopulateField.FindAllStringSubmatch(src, -1) {
		field := m[1]
		if i := strings.IndexByte(field, '.'); i >= 0 {
			field = field[:i]
		}
		if field != "" {
			out[field] = true
		}
	}
	return out
}

// mongooseCollectSchemaRefs finds, for every classic `new Schema({...})`
// literal, the fields declared with a static `ref:` and binds them to the
// owning model (resolved from a `model('Name', SchemaVar)` registration, else
// the schema variable name with a trailing "Schema"/"Model" stripped).
func mongooseCollectSchemaRefs(src string) []mongooseRefField {
	// Map schema-variable → registered model name.
	schemaToModel := make(map[string]string)
	for _, m := range reMongooseModelRegistration.FindAllStringSubmatch(src, -1) {
		modelName, schemaVar := m[1], m[2]
		schemaToModel[schemaVar] = modelName
	}

	var out []mongooseRefField
	for _, loc := range reMongooseSchemaVar.FindAllStringSubmatchIndex(src, -1) {
		schemaVar := src[loc[2]:loc[3]]
		owner := schemaToModel[schemaVar]
		if owner == "" {
			owner = mongooseModelFromSchemaVar(schemaVar)
		}
		if owner == "" {
			continue
		}
		// Parse the schema-definition object that opens at the `(` after
		// `Schema`. loc[1] is the byte just past the matched `(`.
		body, ok := mongooseSchemaObjectBody(src, loc[1]-1)
		if !ok {
			continue
		}
		for _, kv := range mongoAggTopLevelKeys(body) {
			ref := mongooseStaticRefOf(kv.val)
			if ref == "" {
				continue
			}
			out = append(out, mongooseRefField{field: kv.key, ref: ref, owner: owner})
		}
	}
	return out
}

// mongooseStaticRefOf returns the static `ref:` target inside a schema-field
// value object, or "" when the field has no ref or a dynamic ref (a non-string
// value such as `ref: refVar`). A `ref:` whose value is not a string literal is
// rejected by the string-anchored regex, so dynamic refs honestly yield no
// join.
func mongooseStaticRefOf(fieldVal string) string {
	m := reStaticRefValue.FindStringSubmatch(fieldVal)
	if m == nil {
		return ""
	}
	return m[1]
}

// mongooseCollectNestPropRefs finds `@Prop({ ref: 'X' }) field` declarations
// and binds each to its enclosing `@Schema()` class (the owning model). A Prop
// declared outside any @Schema class is skipped (no owner).
func mongooseCollectNestPropRefs(src string) []mongooseRefField {
	classSpans := mongooseNestSchemaClassSpans(src)
	if len(classSpans) == 0 {
		return nil
	}
	var out []mongooseRefField
	for _, m := range reNestPropRef.FindAllStringSubmatchIndex(src, -1) {
		ref := src[m[2]:m[3]]
		field := src[m[4]:m[5]]
		owner := mongooseOwnerClassAt(classSpans, m[0])
		if owner == "" {
			continue
		}
		out = append(out, mongooseRefField{field: field, ref: ref, owner: owner})
	}
	return out
}

// mongooseNestSchemaClass is the [start,end) byte span of a `@Schema()` class
// body together with its model name.
type mongooseNestSchemaClass struct {
	name  string
	start int
	end   int
}

// mongooseNestSchemaClassSpans locates every `@Schema()`-decorated class and
// computes its body span (the matching-brace range of the class block) so a
// @Prop can be attributed to the correct owning model.
func mongooseNestSchemaClassSpans(src string) []mongooseNestSchemaClass {
	var spans []mongooseNestSchemaClass
	for _, m := range reNestSchemaClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		// Find the class body `{` after the match end.
		i := m[1]
		for i < len(src) && src[i] != '{' {
			i++
		}
		if i >= len(src) {
			continue
		}
		end := mongooseMatchBrace(src, i)
		if end < 0 {
			end = len(src)
		}
		spans = append(spans, mongooseNestSchemaClass{name: name, start: i, end: end})
	}
	return spans
}

// mongooseOwnerClassAt returns the model name of the @Schema class whose body
// contains `pos`, or "" if none.
func mongooseOwnerClassAt(spans []mongooseNestSchemaClass, pos int) string {
	for _, s := range spans {
		if pos >= s.start && pos < s.end {
			return s.name
		}
	}
	return ""
}

// mongooseModelFromSchemaVar derives a model name from a schema variable when
// there is no explicit `model()` registration: `BookSchema` → `Book`,
// `bookSchema` → `Book`, `UserModel` → `User`. Returns "" if nothing
// meaningful remains.
func mongooseModelFromSchemaVar(v string) string {
	base := v
	switch {
	case strings.HasSuffix(base, "Schema") && len(base) > len("Schema"):
		base = base[:len(base)-len("Schema")]
	case strings.HasSuffix(base, "Model") && len(base) > len("Model"):
		base = base[:len(base)-len("Model")]
	}
	if base == "" {
		return ""
	}
	return strings.ToUpper(base[:1]) + base[1:]
}

// mongooseSchemaObjectBody returns the body text inside the first object
// literal argument of a `new Schema(` call, given the index of that call's `(`.
// It skips whitespace to the `{`, then returns the balanced-brace, string-aware
// contents. Reports ok=false when the first argument is not an object literal
// (e.g. a variable) — honest skip.
func mongooseSchemaObjectBody(src string, openParen int) (string, bool) {
	if openParen < 0 || openParen >= len(src) || src[openParen] != '(' {
		return "", false
	}
	i := openParen + 1
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
		i++
	}
	if i >= len(src) || src[i] != '{' {
		return "", false
	}
	end := mongooseMatchBrace(src, i)
	if end < 0 {
		return "", false
	}
	return src[i+1 : end], true
}

// mongooseMatchBrace returns the index of the `}` matching the `{` at `open`,
// string-aware (single/double/backtick quotes with escapes), or -1 if
// unbalanced.
func mongooseMatchBrace(src string, open int) int {
	if open >= len(src) || src[open] != '{' {
		return -1
	}
	depth := 0
	inStr := byte(0)
	for i := open; i < len(src); i++ {
		c := src[i]
		if inStr != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			inStr = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
