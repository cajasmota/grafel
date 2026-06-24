// avram_orm.go — Crystal Avram (Lucky framework) ORM model → table/column/
// association schema synthesis (#4936, follow-up to #4905/#3871).
//
// Avram (https://github.com/luckyframework/avram) is the ORM that ships with the
// Lucky web framework. A persisted model is a class that inherits from a
// `BaseModel` (itself `< Avram::Model`) and declares its schema inside a
// `table do … end` block:
//
//	class User < BaseModel
//	  table do
//	    primary_key id : Int64
//	    column name : String
//	    column email : String?
//	    has_many posts : Post
//	    belongs_to account : Account
//	    timestamps
//	  end
//	end
//
// The `table do … end` block may carry an explicit name —
// `table :custom_users do` / `table "custom_users" do`.
//
// What this extractor emits (mirrors the Granite ORM shape — SCOPE.Schema
// entities carrying framework=avram + provenance props):
//   - one SCOPE.Schema/model per `class T < BaseModel` (the file must reference
//     Avram::Model so we never misfire on an arbitrary BaseModel).
//   - one SCOPE.Schema/table per model. The table identity is the explicit
//     `table :<name> do` / `table "<name>" do` argument when present, otherwise
//     the model class name.
//   - one SCOPE.Schema/column per `column <name> : <Type>` and per
//     `primary_key <name> : <Type>` (the latter stamped primary_key=true).
//   - a REFERENCES edge model → referenced model for each `belongs_to`.
//   - an association SCOPE.Schema/association entity per belongs_to/has_many/
//     has_one carrying assoc_kind + target.
//   - the `timestamps` macro synthesises created_at/updated_at Time columns.
//
// #5366 deepening — query attribution, migrations, transactions:
//   - a `Model.<verb>` / SaveOperation query-DSL call site naming an in-file
//     model emits a QUERIES edge model → table stamped with the canonical SQL op.
//   - an Avram migration `create_table/drop_table/alter_table :x` DSL call (or
//     raw schema-op SQL via `.exec`) emits a shared SCOPE.Evolution migration-op.
//   - a `<db>.transaction do … end` block emits a SCOPE.Pattern/
//     transaction_boundary. Shared helpers in orm_query_migration.go.
//
// Registration key: "custom_crystal_avram_orm".
package crystal

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_crystal_avram_orm", &avramORMExtractor{})
}

type avramORMExtractor struct{}

func (e *avramORMExtractor) Language() string { return "custom_crystal_avram_orm" }

var (
	// avramModelRe matches `class T < BaseModel`. Group 1 is the model class
	// name. The file-level Avram::Model pre-filter gates this against arbitrary
	// BaseModel subclasses.
	avramModelRe = regexp.MustCompile(
		`(?m)^[ \t]*(?:abstract\s+)?class\s+([A-Z]\w*)\s*<\s*BaseModel\b`)

	// avramTableRe matches the `table [:name | "name"] do` block opener. Group 1
	// is the optional explicit table name.
	avramTableRe = regexp.MustCompile(
		`(?m)^[ \t]*table\s+(?::?["']?([A-Za-z_]\w*)["']?\s+)?do\b`)

	// avramColumnRe matches a `column <name> : <Type>[, opts…]` declaration.
	avramColumnRe = regexp.MustCompile(
		`(?m)^[ \t]*column\s+([a-z_]\w*)\s*:\s*([A-Za-z_][\w:]*\??)\s*(,.*)?$`)

	// avramPrimaryKeyRe matches a `primary_key <name> : <Type>` declaration.
	avramPrimaryKeyRe = regexp.MustCompile(
		`(?m)^[ \t]*primary_key\s+([a-z_]\w*)\s*:\s*([A-Za-z_][\w:]*\??)`)

	// avramTimestampsRe matches the `timestamps` macro.
	avramTimestampsRe = regexp.MustCompile(`(?m)^[ \t]*timestamps\b`)

	// avramAssocRe matches a belongs_to / has_many / has_one association in
	// Avram's typed form `belongs_to <name> : <Class>`. Group 1 = kind; group 2 =
	// name; group 3 = the optional explicit `: <Class>` target.
	avramAssocRe = regexp.MustCompile(
		`(?m)^[ \t]*(belongs_to|has_many|has_one)\s+:?["']?([a-z_]\w*)["']?(?:\s*:\s*([A-Z][\w:]*))?`)
)

// avramHasModel is a fast pre-filter: the file must reference Avram::Model.
func avramHasModel(content string) bool {
	return strings.Contains(content, "Avram::Model")
}

func (e *avramORMExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "crystal" {
		return nil, nil
	}
	src := string(file.Content)
	if !avramHasModel(src) {
		return nil, nil
	}

	models := collectAvramModels(src)
	if len(models) == 0 {
		return nil, nil
	}
	// Known-model set + query attribution: a `Model.<verb>` / `SaveModel.create`
	// call site naming an in-file model attributes a canonical SQL op to it.
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
		model := newCrystalSchema(m.name, "model", "avram", file.Path, m.line,
			"INFERRED_FROM_AVRAM_MODEL")
		var rels []types.RelationshipRecord
		for _, a := range m.assocs {
			if a.kind != "belongs_to" {
				continue
			}
			target := avramAssocTarget(a)
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
		rels = append(rels, crystalQueryRels(m.name, tableName, queryOps[m.name], "avram")...)
		model.Relationships = rels
		model.ID = model.ComputeID()
		out = append(out, model)

		// 2. table entity.
		table := newCrystalSchema(tableName, "table", "avram", file.Path, m.line,
			"INFERRED_FROM_AVRAM_TABLE")
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
			provenance := "INFERRED_FROM_AVRAM_COLUMN"
			if c.auto {
				provenance = "INFERRED_FROM_AVRAM_TIMESTAMPS"
			}
			col := newCrystalSchema(c.name, "column", "avram", file.Path, c.line,
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
			assoc := newCrystalSchema(a.name, "association", "avram", file.Path, a.line,
				"INFERRED_FROM_AVRAM_ASSOCIATION")
			assoc.Properties["assoc_kind"] = a.kind
			assoc.Properties["model"] = m.name
			assoc.Properties["target"] = avramAssocTarget(a)
			assoc.ID = assoc.ComputeID()
			out = append(out, assoc)
		}
	}

	// 5. transaction boundaries + 6. migration schema ops (#5366).
	out = append(out, collectCrystalTransactions(src, file.Path, "avram")...)
	out = append(out, collectCrystalMigrations(src, file.Path, "avram")...)
	return out, nil
}

// avramAssocTarget resolves the association target model: an explicit typed
// `: <Class>` target wins, otherwise the CamelCased (singularised for plural
// has_many) name.
func avramAssocTarget(a avramAssoc) string {
	if a.target != "" {
		return a.target
	}
	name := a.name
	if a.kind == "has_many" {
		name = graniteSingular(name)
	}
	return camelize(name)
}

type avramModel struct {
	name    string
	table   string
	line    int
	columns []graniteColumn
	assocs  []avramAssoc
}

type avramAssoc struct {
	kind   string
	name   string
	target string
	line   int
}

// collectAvramModels finds every `class T < BaseModel` and the table do … end
// block's column/primary_key/association macros in its body.
func collectAvramModels(src string) []avramModel {
	idx := avramModelRe.FindAllStringSubmatchIndex(src, -1)
	if len(idx) == 0 {
		return nil
	}
	var models []avramModel
	for _, m := range idx {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		bodyEnd := graniteClassEnd(src, m[1])
		body := src[m[1]:bodyEnd]

		am := avramModel{name: name, line: startLine}

		if tm := avramTableRe.FindStringSubmatch(body); tm != nil {
			am.table = tm[1] // may be empty (anonymous `table do`)
		}

		// primary_key declarations (stamped primary).
		for _, pk := range avramPrimaryKeyRe.FindAllStringSubmatchIndex(body, -1) {
			am.columns = append(am.columns, graniteColumn{
				name:    body[pk[2]:pk[3]],
				typ:     strings.TrimSuffix(body[pk[4]:pk[5]], "?"),
				primary: true,
				line:    startLine + strings.Count(body[:pk[0]], "\n"),
			})
		}

		// column declarations.
		for _, col := range avramColumnRe.FindAllStringSubmatchIndex(body, -1) {
			am.columns = append(am.columns, graniteColumn{
				name: body[col[2]:col[3]],
				typ:  strings.TrimSuffix(body[col[4]:col[5]], "?"),
				line: startLine + strings.Count(body[:col[0]], "\n"),
			})
		}

		if tsLoc := avramTimestampsRe.FindStringIndex(body); tsLoc != nil {
			tsLine := startLine + strings.Count(body[:tsLoc[0]], "\n")
			for _, n := range []string{"created_at", "updated_at"} {
				am.columns = append(am.columns, graniteColumn{
					name: n, typ: "Time", auto: true, line: tsLine,
				})
			}
		}

		for _, as := range avramAssocRe.FindAllStringSubmatchIndex(body, -1) {
			a := avramAssoc{
				kind: body[as[2]:as[3]],
				name: body[as[4]:as[5]],
				line: startLine + strings.Count(body[:as[0]], "\n"),
			}
			if as[6] >= 0 {
				a.target = body[as[6]:as[7]]
			}
			am.assocs = append(am.assocs, a)
		}
		models = append(models, am)
	}
	return models
}
