// clear_orm.go — Crystal Clear ORM model → table/column/association schema
// synthesis (#4936, follow-up to #4905/#3871).
//
// Clear (https://github.com/anykeyh/clear) is a Crystal ORM + query builder for
// PostgreSQL. A persisted model is a class that mixes in `Clear::Model` and
// declares its columns with the `column` macro:
//
//	class User
//	  include Clear::Model
//	  self.table = "users"
//
//	  column id : Int64, primary: true
//	  column name : String
//	  column email : String?
//
//	  has_many posts : Post
//	  belongs_to account : Account
//	end
//
// What this extractor emits (mirrors the Granite ORM shape — SCOPE.Schema
// entities carrying framework=clear + provenance props):
//   - one SCOPE.Schema/model per class that `include Clear::Model`.
//   - one SCOPE.Schema/table per model. The table identity is the explicit
//     `self.table = "<name>"` assignment when present, otherwise the model class
//     name.
//   - one SCOPE.Schema/column per `column <name> : <Type>[, primary: true]`
//     macro, stamping column_type (nilable `?` trimmed) + the owning model, with
//     primary_key=true on the primary column.
//   - a REFERENCES edge model → referenced model for each `belongs_to` (the FK
//     signal). Clear's `belongs_to <name> : <Class>` typed form names the target
//     explicitly; otherwise the CamelCased name is used.
//   - an association SCOPE.Schema/association entity per belongs_to/has_many/
//     has_one carrying assoc_kind + target.
//   - the `timestamps` macro synthesises created_at/updated_at Time columns.
//
// #5366 deepening — query attribution, migrations, transactions:
//   - a `Model.<verb>` query-builder call site naming an in-file model emits a
//     QUERIES edge model → table stamped with the canonical SQL op.
//   - a Clear migration `create_table/drop_table/alter_table :x` DSL call (or
//     raw schema-op SQL via `.exec`) emits a shared SCOPE.Evolution migration-op.
//   - a `<db>.transaction do … end` block emits a SCOPE.Pattern/
//     transaction_boundary. Shared helpers in orm_query_migration.go.
//
// Registration key: "custom_crystal_clear_orm".
package crystal

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_crystal_clear_orm", &clearORMExtractor{})
}

type clearORMExtractor struct{}

func (e *clearORMExtractor) Language() string { return "custom_crystal_clear_orm" }

var (
	// clearModelRe matches a class that mixes in Clear::Model. Group 1 is the
	// model class name. Clear uses `class T` + `include Clear::Model`, so the
	// include is the model marker (matched separately within the class body).
	clearClassRe = regexp.MustCompile(
		`(?m)^[ \t]*(?:abstract\s+)?class\s+([A-Z]\w*)\b`)

	// clearIncludeRe matches `include Clear::Model` inside a class body.
	clearIncludeRe = regexp.MustCompile(`(?m)^[ \t]*include\s+Clear::Model\b`)

	// clearTableRe matches `self.table = "<name>"`. Group 1 is the table name.
	clearTableRe = regexp.MustCompile(
		`(?m)^[ \t]*self\.table\s*=\s*["']([A-Za-z_]\w*)["']`)

	// clearColumnRe matches `column <name> : <Type>[, opts…]`. Group 1 = name;
	// group 2 = type; group 3 = the option tail (scanned for primary).
	clearColumnRe = regexp.MustCompile(
		`(?m)^[ \t]*column\s+([a-z_]\w*)\s*:\s*([A-Za-z_][\w:]*\??)\s*(,.*)?$`)

	// clearTimestampsRe matches the `timestamps` macro.
	clearTimestampsRe = regexp.MustCompile(`(?m)^[ \t]*timestamps\b`)

	// clearAssocRe matches a belongs_to / has_many / has_one association in
	// Clear's typed form `belongs_to <name> : <Class>`. Group 1 = kind; group 2 =
	// name; group 3 = the optional explicit `: <Class>` target.
	clearAssocRe = regexp.MustCompile(
		`(?m)^[ \t]*(belongs_to|has_many|has_one)\s+:?["']?([a-z_]\w*)["']?(?:\s*:\s*([A-Z][\w:]*))?`)
)

// clearHasModel is a fast pre-filter: the file must reference Clear::Model.
func clearHasModel(content string) bool {
	return strings.Contains(content, "Clear::Model")
}

func (e *clearORMExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "crystal" {
		return nil, nil
	}
	src := string(file.Content)
	if !clearHasModel(src) {
		return nil, nil
	}

	models := collectClearModels(src)
	if len(models) == 0 {
		return nil, nil
	}
	modelNames := make(map[string]bool, len(models))
	for _, m := range models {
		modelNames[m.name] = true
	}
	queryOps := collectCrystalModelQueries(src, modelNames)

	var out []types.EntityRecord
	for _, m := range models {
		tableName := m.table
		if tableName == "" {
			tableName = m.name
		}

		// 1. model entity + belongs_to REFERENCES edges.
		model := newCrystalSchema(m.name, "model", "clear", file.Path, m.line,
			"INFERRED_FROM_CLEAR_MODEL")
		var rels []types.RelationshipRecord
		for _, a := range m.assocs {
			if a.kind != "belongs_to" {
				continue
			}
			target := clearAssocTarget(a)
			rels = append(rels, types.RelationshipRecord{
				ToID: target,
				Kind: "REFERENCES",
				Properties: map[string]string{
					"fk_field": a.name,
					"to_model": target,
				},
			})
		}
		// Query attribution: model → its table, one QUERIES edge per attributed op.
		rels = append(rels, crystalQueryRels(m.name, tableName, queryOps[m.name], "clear")...)
		model.Relationships = rels
		model.ID = model.ComputeID()
		out = append(out, model)

		// 2. table entity.
		table := newCrystalSchema(tableName, "table", "clear", file.Path, m.line,
			"INFERRED_FROM_CLEAR_TABLE")
		table.Properties["model"] = m.name
		table.ID = table.ComputeID()
		out = append(out, table)

		// 3. column entities.
		colSeen := make(map[string]bool)
		for _, c := range m.columns {
			if colSeen[c.name] {
				continue
			}
			colSeen[c.name] = true
			provenance := "INFERRED_FROM_CLEAR_COLUMN"
			if c.auto {
				provenance = "INFERRED_FROM_CLEAR_TIMESTAMPS"
			}
			col := newCrystalSchema(c.name, "column", "clear", file.Path, c.line,
				provenance)
			col.Properties["column_type"] = c.typ
			col.Properties["model"] = m.name
			if c.primary {
				col.Properties["primary_key"] = "true"
			}
			if c.auto {
				col.Properties["auto_timestamp"] = "true"
			}
			col.ID = col.ComputeID()
			out = append(out, col)
		}

		// 4. association entities.
		assocSeen := make(map[string]bool)
		for _, a := range m.assocs {
			key := a.kind + ":" + a.name
			if assocSeen[key] {
				continue
			}
			assocSeen[key] = true
			assoc := newCrystalSchema(a.name, "association", "clear", file.Path, a.line,
				"INFERRED_FROM_CLEAR_ASSOCIATION")
			assoc.Properties["assoc_kind"] = a.kind
			assoc.Properties["model"] = m.name
			assoc.Properties["target"] = clearAssocTarget(a)
			assoc.ID = assoc.ComputeID()
			out = append(out, assoc)
		}
	}

	// 5. transaction boundaries + 6. migration schema ops (#5366).
	out = append(out, collectCrystalTransactions(src, file.Path, "clear")...)
	out = append(out, collectCrystalMigrations(src, file.Path, "clear")...)
	return out, nil
}

// clearAssocTarget resolves the association target model: an explicit typed
// `: <Class>` target wins, otherwise the CamelCased (singularised for plural
// has_many) name.
func clearAssocTarget(a clearAssoc) string {
	if a.target != "" {
		return a.target
	}
	name := a.name
	if a.kind == "has_many" {
		name = graniteSingular(name)
	}
	return camelize(name)
}

type clearModel struct {
	name    string
	table   string
	line    int
	columns []graniteColumn
	assocs  []clearAssoc
}

type clearAssoc struct {
	kind   string
	name   string
	target string
	line   int
}

// collectClearModels finds every class that `include Clear::Model` and the
// self.table/column/association macros in its `end`-terminated body.
func collectClearModels(src string) []clearModel {
	idx := clearClassRe.FindAllStringSubmatchIndex(src, -1)
	if len(idx) == 0 {
		return nil
	}
	var models []clearModel
	for _, m := range idx {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		bodyEnd := graniteClassEnd(src, m[1])
		body := src[m[1]:bodyEnd]

		// Only a class that mixes in Clear::Model is a model.
		if !clearIncludeRe.MatchString(body) {
			continue
		}

		cm := clearModel{name: name, line: startLine}

		if tm := clearTableRe.FindStringSubmatch(body); tm != nil {
			cm.table = tm[1]
		}

		for _, col := range clearColumnRe.FindAllStringSubmatchIndex(body, -1) {
			cname := body[col[2]:col[3]]
			ctyp := strings.TrimSuffix(body[col[4]:col[5]], "?")
			opts := ""
			if col[6] >= 0 {
				opts = body[col[6]:col[7]]
			}
			cm.columns = append(cm.columns, graniteColumn{
				name:    cname,
				typ:     ctyp,
				primary: strings.Contains(opts, "primary"),
				line:    startLine + strings.Count(body[:col[0]], "\n"),
			})
		}

		if tsLoc := clearTimestampsRe.FindStringIndex(body); tsLoc != nil {
			tsLine := startLine + strings.Count(body[:tsLoc[0]], "\n")
			for _, n := range []string{"created_at", "updated_at"} {
				cm.columns = append(cm.columns, graniteColumn{
					name: n, typ: "Time", auto: true, line: tsLine,
				})
			}
		}

		for _, am := range clearAssocRe.FindAllStringSubmatchIndex(body, -1) {
			a := clearAssoc{
				kind: body[am[2]:am[3]],
				name: body[am[4]:am[5]],
				line: startLine + strings.Count(body[:am[0]], "\n"),
			}
			if am[6] >= 0 {
				a.target = body[am[6]:am[7]]
			}
			cm.assocs = append(cm.assocs, a)
		}
		models = append(models, cm)
	}
	return models
}
