// norm_orm.go — Nim Norm ORM model → table/column schema synthesis (#4904).
//
// Norm (https://norm.nim.town) is the de-facto Nim ORM. A persisted model is a
// plain Nim `ref object` that inherits from Norm's `Model` base type; Norm maps
// the object to a database table whose name is, by default, the snake_case
// pluralisation of the type name (Norm lowercases the type name and appends
// nothing structural at the Nim level — the runtime applies the table naming —
// so we record the TYPE NAME as the table identity, which is what the model
// object is keyed by). Each public object field becomes a column; a field typed
// as another Model subtype (or carrying an `{.fk: Other.}` pragma) is a foreign
// key to that model.
//
// Norm model shape:
//
//	import norm/model
//
//	type
//	  User* = ref object of Model
//	    name*: string
//	    email*: string
//	    age*: int
//
//	  Post* = ref object of Model
//	    title*: string
//	    body*: string
//	    author*: User          # FK → User (field typed as a Model subtype)
//
// What this extractor emits (mirrors the PHP/Eloquent + Scala ORM shape —
// SCOPE.Schema entities carrying framework+provenance props):
//   - one SCOPE.Schema/model per `T* = ref object of Model` declaration
//   - one SCOPE.Schema/table per model (table identity = the model type name)
//   - one SCOPE.Schema/column per public object field, with column_type stamped
//   - a REFERENCES edge model → referenced model for a field typed as another
//     model type in the same file (the FK signal), keyed by model name
//
// Honest exclusions / follow-ups (no fabricated schema):
//   - cross-file FK targets (a field typed as a model declared in another file)
//     are recorded as a REFERENCES edge to the bare type name but not resolved
//     to the concrete entity here — the shared resolver handles binding.
//   - `{.tableName: "x".}` / `{.dbName.}` pragma table-name overrides, index
//     pragmas, `db.select/insert/update` query attribution, and Norm migrations
//     (createTables/dropTables) are deferred to follow-up #4932.
//   - Allographer / ormin / Debby model→table mapping is deferred to #4933.
//
// Registration key: "custom_nim_norm_orm".
package nim

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_nim_norm_orm", &nimNormORMExtractor{})
}

type nimNormORMExtractor struct{}

func (e *nimNormORMExtractor) Language() string { return "custom_nim_norm_orm" }

var (
	// nimNormModelRe matches a Norm model declaration: a type that is a
	// `ref object of Model`. Capture group 1 is the model type name (export
	// marker stripped by the caller). Accepts an optional pragma block before
	// the `=` (e.g. `User* {.tableName: "users".} = ref object of Model`).
	nimNormModelRe = regexp.MustCompile(
		`(?m)^[ \t]*([A-Z][A-Za-z0-9_]*)\*?\s*(?:\{[^}]*\})?\s*=\s*ref\s+object\s+of\s+Model\b`)

	// nimNormFieldRe matches a single object field inside a model body:
	// `name*: string`, `author*: User`, `age: int`. Capture group 1 is the
	// field name (export marker stripped), group 2 the field type (first type
	// token, generics/option wrappers trimmed by normaliseNimFieldType).
	nimNormFieldRe = regexp.MustCompile(
		`(?m)^[ \t]+([a-z_][A-Za-z0-9_]*)\*?\s*:\s*([A-Za-z_][A-Za-z0-9_\[\], ]*)`)
)

// nimNormHasModel is a fast pre-filter: the file must reference Norm's Model
// base type to be worth scanning, so we never misfire on arbitrary Nim objects.
func nimNormHasModel(content string) bool {
	return strings.Contains(content, "of Model") &&
		(strings.Contains(content, "norm") || strings.Contains(content, "Model"))
}

func (e *nimNormORMExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "nim" {
		return nil, nil
	}
	src := string(file.Content)
	if !nimNormHasModel(src) {
		return nil, nil
	}

	models := collectNormModels(src)
	if len(models) == 0 {
		return nil, nil
	}
	// Set of known model names in this file — used to recognise a field whose
	// type is another model as a foreign key.
	modelNames := make(map[string]bool, len(models))
	for _, m := range models {
		modelNames[m.name] = true
	}

	var out []types.EntityRecord
	for _, m := range models {
		// 1. model entity
		model := newNormSchema(m.name, "model", file.Path, m.line,
			"INFERRED_FROM_NORM_MODEL")
		// FK edges → referenced models (fields typed as another model type).
		var rels []types.RelationshipRecord
		for _, f := range m.fields {
			if modelNames[f.typ] && f.typ != m.name {
				rels = append(rels, types.RelationshipRecord{
					ToID: f.typ,
					Kind: "REFERENCES",
					Properties: map[string]string{
						"fk_field": f.name,
						"to_model": f.typ,
					},
				})
			}
		}
		model.Relationships = rels
		model.ID = model.ComputeID()
		out = append(out, model)

		// 2. table entity (table identity = the model type name).
		table := newNormSchema(m.name, "table", file.Path, m.line,
			"INFERRED_FROM_NORM_TABLE")
		table.ID = table.ComputeID()
		out = append(out, table)

		// 3. column entities (one per public object field).
		colSeen := make(map[string]bool)
		for _, f := range m.fields {
			if colSeen[f.name] {
				continue
			}
			colSeen[f.name] = true
			col := newNormSchema(f.name, "column", file.Path, f.line,
				"INFERRED_FROM_NORM_FIELD")
			col.Properties["column_type"] = f.typ
			col.Properties["model"] = m.name
			if modelNames[f.typ] && f.typ != m.name {
				col.Properties["foreign_key"] = "true"
			}
			col.ID = col.ComputeID()
			out = append(out, col)
		}
	}
	return out, nil
}

// normModel is a parsed Norm model with its fields.
type normModel struct {
	name   string
	line   int
	fields []normField
}

type normField struct {
	name string
	typ  string
	line int
}

// collectNormModels finds every `T = ref object of Model` declaration and the
// fields in its indented body.
func collectNormModels(src string) []normModel {
	idx := nimNormModelRe.FindAllStringSubmatchIndex(src, -1)
	if len(idx) == 0 {
		return nil
	}
	lines := strings.Split(src, "\n")
	var models []normModel
	for _, m := range idx {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		modelIndent := leadingIndent(lineAt(lines, startLine))
		fields := collectNormFields(lines, startLine, modelIndent)
		models = append(models, normModel{name: name, line: startLine, fields: fields})
	}
	return models
}

// collectNormFields scans the indented body following a model header for object
// fields. A field line is more indented than the model header; the body ends at
// the first non-blank line indented at or below the model header.
func collectNormFields(lines []string, headerLine, modelIndent int) []normField {
	var fields []normField
	seen := make(map[string]bool)
	for ln := headerLine + 1; ln <= len(lines); ln++ {
		raw := lineAt(lines, ln)
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if leadingIndent(raw) <= modelIndent {
			break // dedent — model body ended
		}
		fm := nimNormFieldRe.FindStringSubmatch(raw)
		if fm == nil {
			continue
		}
		fname := fm[1]
		ftyp := normaliseNimFieldType(fm[2])
		if ftyp == "" || seen[fname] {
			continue
		}
		seen[fname] = true
		fields = append(fields, normField{name: fname, typ: ftyp, line: ln})
	}
	return fields
}

// normaliseNimFieldType reduces a field type expression to its core type name:
// `Option[User]` → `User`, `seq[Post]` → `Post`, `string` → `string`. The
// wrapper (Option/seq) is unwrapped so a wrapped model reference is still
// recognised as a foreign key.
func normaliseNimFieldType(raw string) string {
	t := strings.TrimSpace(raw)
	// Unwrap Option[...] / seq[...] generics to the inner type.
	for {
		open := strings.IndexByte(t, '[')
		if open < 0 {
			break
		}
		close := strings.LastIndexByte(t, ']')
		if close <= open {
			break
		}
		t = strings.TrimSpace(t[open+1 : close])
	}
	// Take the first whitespace-delimited token (drops trailing pragmas/comments).
	if sp := strings.IndexAny(t, " \t"); sp >= 0 {
		t = t[:sp]
	}
	return strings.TrimSpace(t)
}

// newNormSchema builds a SCOPE.Schema entity with the Norm framework + the given
// provenance stamp.
func newNormSchema(name, subtype, path string, line int, provenance string) types.EntityRecord {
	return types.EntityRecord{
		Name:       name,
		Kind:       "SCOPE.Schema",
		Subtype:    subtype,
		SourceFile: path,
		Language:   "nim",
		StartLine:  line,
		EndLine:    line,
		Properties: map[string]string{
			"framework":  "norm",
			"provenance": provenance,
		},
	}
}

// leadingIndent counts leading spaces/tabs of a line (tab counts as 1).
func leadingIndent(line string) int {
	n := 0
	for _, ch := range line {
		if ch == ' ' || ch == '\t' {
			n++
		} else {
			break
		}
	}
	return n
}

// lineAt returns the 1-based line ln from lines, or "" when out of range.
func lineAt(lines []string, ln int) string {
	if ln < 1 || ln > len(lines) {
		return ""
	}
	return lines[ln-1]
}
