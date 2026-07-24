// debby_orm.go — Nim Debby ORM model → table/column schema synthesis (#5028).
//
// Debby (https://github.com/guzba/debby) is a Nim ORM in which a persisted model
// is a PLAIN Nim `object` (NOT a `ref object of Model` like Norm — #4904). Debby
// maps the object type to a table whose name is, by default, the type name; each
// public object field becomes a column. The model is registered/created against a
// Debby db handle (`db.createTable(User)` / `db.dropTable(User)`), and a field
// typed as another Debby model (or carrying an `{.fk: Other.}` pragma) is a
// foreign key to that model.
//
// Debby model shape:
//
//	import debby/sqlite
//
//	type
//	  User = object
//	    id: int
//	    name: string
//	    email: string
//
//	  Post = object
//	    id: int
//	    title: string
//	    user: User            # FK → User (field typed as another model)
//	    userId {.fk: User.}: int  # explicit FK pragma on a scalar field
//
//	db.createTable(User)
//	db.createTable(Post)
//
// Because a plain `object` is too generic to scan blindly, this extractor only
// treats an object type as a Debby model when the file imports Debby AND the type
// is referenced by a Debby db operation (createTable/dropTable/insert/get/
// update/delete/query) — the registration is the signal that the object is a
// persisted model. This keeps us from misfiring on arbitrary Nim records.
//
// What this extractor emits (mirrors the Norm/Allographer SCOPE.Schema shape,
// framework=debby):
//   - one SCOPE.Schema/model per registered Debby `object` type
//   - one SCOPE.Schema/table per model (table identity = the model type name)
//   - one SCOPE.Schema/column per public object field, with column_type stamped
//   - a REFERENCES edge model → referenced model for a field typed as another
//     registered model, or an explicit `{.fk: Other.}` pragma
//   - QUERIES edges model → its table for db.<op>(Model[, …]) call sites
//     (insert/get/update/delete/query), one edge per attributed operation
//
// Honest exclusions / follow-ups (no fabricated schema; #5031):
//   - cross-file FK targets carry the bare type name on the REFERENCES edge and
//     resolve via the shared resolver.
//   - Debby index pragmas, withTransaction transaction boundaries, and raw-SQL
//     `db.query(sql(...))` attribution are follow-ups (#5031).
//   - ormin (the other #5028 ORM) is covered by ormin_orm.go.
//
// Registration key: "custom_nim_debby_orm".
package nim

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_nim_debby_orm", &nimDebbyORMExtractor{})
}

type nimDebbyORMExtractor struct{}

func (e *nimDebbyORMExtractor) Language() string { return "custom_nim_debby_orm" }

var (
	// nimDebbyObjectRe matches a plain object type declaration:
	// `User = object`, `Post* = object`. Capture group 1 is the type name
	// (export marker stripped). Deliberately NOT `ref object of Model` — Debby
	// uses plain objects, so this is the distinguishing shape from Norm.
	nimDebbyObjectRe = regexp.MustCompile(
		`(?m)^[ \t]*([A-Z][A-Za-z0-9_]*)\*?\s*=\s*object\b`)

	// nimDebbyFieldRe matches an object field inside a model body, optionally
	// carrying a field pragma block (`{.fk: User.}`). Group 1 is the field name,
	// group 2 the optional pragma body, group 3 the field type.
	nimDebbyFieldRe = regexp.MustCompile(
		`(?m)^[ \t]+([a-z_][A-Za-z0-9_]*)\*?\s*(?:\{\.([^}]*?)\.?\})?\s*:\s*([A-Za-z_][A-Za-z0-9_\[\], ]*)`)

	// nimDebbyFkPragmaRe extracts an explicit FK target from a field pragma
	// (`fk: Other`).
	nimDebbyFkPragmaRe = regexp.MustCompile(`\bfk\s*:\s*([A-Z][A-Za-z0-9_]*)`)

	// nimDebbyRegisterRe matches a Debby db operation that names a model TYPE as
	// its first argument — the registration/usage signal that the object is a
	// persisted model. Group 1 is the operation, group 2 the model type name.
	// Covers createTable/dropTable (registration) and insert/get/update/delete/
	// query (usage), all of which take the model type or instance.
	nimDebbyRegisterRe = regexp.MustCompile(
		`(?m)\b[A-Za-z_][A-Za-z0-9_]*\s*\.\s*(createTable|dropTable|insert|get|update|delete|query)\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)`)
)

// nimDebbyHasDebby is a fast pre-filter: the file must import/reference Debby and
// perform a Debby db operation, so we never misfire on arbitrary Nim objects.
func nimDebbyHasDebby(content string) bool {
	return strings.Contains(content, "debby") &&
		nimDebbyRegisterRe.MatchString(content)
}

func (e *nimDebbyORMExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "nim" {
		return nil, nil
	}
	src := string(file.Content)
	if !nimDebbyHasDebby(src) {
		return nil, nil
	}

	// registered maps a type name → the set of operations attributed to it.
	registered := collectDebbyOps(src)
	if len(registered) == 0 {
		return nil, nil
	}

	objects := collectDebbyObjects(src)
	// Only objects that are registered/used as Debby models are persisted models.
	var models []debbyModel
	modelNames := make(map[string]bool)
	for _, o := range objects {
		if registered[o.name] != nil {
			models = append(models, o)
			modelNames[o.name] = true
		}
	}
	if len(models) == 0 {
		return nil, nil
	}

	var out []types.EntityRecord
	for _, m := range models {
		// 1. model entity
		model := newDebbySchema(m.name, "model", file.Path, m.line,
			"INFERRED_FROM_DEBBY_MODEL")
		var rels []types.RelationshipRecord
		// FK edges → referenced models.
		for _, f := range m.fields {
			target := ""
			switch {
			case f.fkTarget != "" && f.fkTarget != m.name:
				target = f.fkTarget
			case modelNames[f.typ] && f.typ != m.name:
				target = f.typ
			}
			if target == "" {
				continue
			}
			props := types.Props{{K: "fk_field", V: f.name}, {K: "to_model", V: target}}
			if f.fkTarget != "" {
				props.Set("fk_pragma", "true")
			}
			rels = append(rels, types.RelationshipRecord{
				ToID: target, Kind: "REFERENCES", Properties: props,
			})
		}
		// Query attribution: model → its table, one edge per attributed op.
		for _, op := range debbyOpOrder(registered[m.name]) {
			if op == "createTable" || op == "dropTable" {
				continue // schema registration, not a query
			}
			rels = append(rels, types.RelationshipRecord{
				ToID: m.name,
				Kind: "QUERIES",
				Properties: types.Props{
					{K: "model", V: m.name},
					{K: "operation", V: op},
					{K: "table", V: m.name},
				},
			})
		}
		model.Relationships = rels
		model.ID = model.ComputeID()
		out = append(out, model)

		// 2. table entity (identity = model type name).
		table := newDebbySchema(m.name, "table", file.Path, m.line,
			"INFERRED_FROM_DEBBY_TABLE")
		table.Properties["model"] = m.name
		table.ID = table.ComputeID()
		out = append(out, table)

		// 3. column entities (one per public object field).
		colSeen := make(map[string]bool)
		for _, f := range m.fields {
			if colSeen[f.name] {
				continue
			}
			colSeen[f.name] = true
			col := newDebbySchema(f.name, "column", file.Path, f.line,
				"INFERRED_FROM_DEBBY_FIELD")
			col.Properties["column_type"] = f.typ
			col.Properties["model"] = m.name
			fkTarget := f.fkTarget
			if fkTarget == "" && modelNames[f.typ] && f.typ != m.name {
				fkTarget = f.typ
			}
			if fkTarget != "" && fkTarget != m.name {
				col.Properties["foreign_key"] = "true"
				col.Properties["fk_target"] = fkTarget
			}
			col.ID = col.ComputeID()
			out = append(out, col)
		}
	}
	return out, nil
}

// collectDebbyOps scans db.<op>(Type, …) call sites and returns, per type name,
// the set of operations attributed to it. Only first arguments that name a
// recognised TYPE (capitalised identifier) are attributed — instance handles
// (lowercase) are not, keeping attribution file-local + honest.
func collectDebbyOps(src string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, m := range nimDebbyRegisterRe.FindAllStringSubmatch(src, -1) {
		op, arg := m[1], m[2]
		if arg == "" || arg[0] < 'A' || arg[0] > 'Z' {
			continue // not a type name
		}
		if out[arg] == nil {
			out[arg] = map[string]bool{}
		}
		out[arg][op] = true
	}
	return out
}

// debbyOpOrder returns the operations in a stable order for deterministic edge
// emission.
func debbyOpOrder(ops map[string]bool) []string {
	var out []string
	for _, op := range []string{"createTable", "dropTable", "insert", "get", "update", "delete", "query"} {
		if ops[op] {
			out = append(out, op)
		}
	}
	return out
}

// debbyModel is a parsed Debby model with its fields.
type debbyModel struct {
	name   string
	line   int
	fields []debbyField
}

type debbyField struct {
	name     string
	typ      string
	fkTarget string // {.fk: Other.}
	line     int
}

// collectDebbyObjects finds every `T = object` declaration and the fields in its
// indented body (reusing the shared leadingIndent/lineAt helpers from
// norm_orm.go).
func collectDebbyObjects(src string) []debbyModel {
	idx := nimDebbyObjectRe.FindAllStringSubmatchIndex(src, -1)
	if len(idx) == 0 {
		return nil
	}
	lines := strings.Split(src, "\n")
	var models []debbyModel
	for _, m := range idx {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		objIndent := leadingIndent(lineAt(lines, startLine))
		fields := collectDebbyFields(lines, startLine, objIndent)
		models = append(models, debbyModel{name: name, line: startLine, fields: fields})
	}
	return models
}

// collectDebbyFields scans the indented body following an object header for
// fields. A field line is more indented than the object header; the body ends at
// the first non-blank line indented at or below the object header.
func collectDebbyFields(lines []string, headerLine, objIndent int) []debbyField {
	var fields []debbyField
	seen := make(map[string]bool)
	for ln := headerLine + 1; ln <= len(lines); ln++ {
		raw := lineAt(lines, ln)
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if leadingIndent(raw) <= objIndent {
			break // dedent — object body ended
		}
		fm := nimDebbyFieldRe.FindStringSubmatch(raw)
		if fm == nil {
			continue
		}
		fname := fm[1]
		pragma := fm[2]
		ftyp := normaliseNimFieldType(fm[3])
		if ftyp == "" || seen[fname] {
			continue
		}
		seen[fname] = true
		f := debbyField{name: fname, typ: ftyp, line: ln}
		if pragma != "" {
			if km := nimDebbyFkPragmaRe.FindStringSubmatch(pragma); km != nil {
				f.fkTarget = km[1]
			}
		}
		fields = append(fields, f)
	}
	return fields
}

// newDebbySchema builds a SCOPE.Schema entity with framework=debby and the given
// provenance stamp.
func newDebbySchema(name, subtype, path string, line int, provenance string) types.EntityRecord {
	return types.EntityRecord{
		Name:       name,
		Kind:       "SCOPE.Schema",
		Subtype:    subtype,
		SourceFile: path,
		Language:   "nim",
		StartLine:  line,
		EndLine:    line,
		Properties: map[string]string{
			"framework":  "debby",
			"provenance": provenance,
		},
	}
}
