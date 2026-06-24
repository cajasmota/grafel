// jennifer_orm.go — Crystal Jennifer ORM model → table/column/association
// schema synthesis (#4936, follow-up to #4905/#3871).
//
// Jennifer (https://github.com/imdrasil/jennifer.cr) is a Crystal ORM + query
// DSL. A persisted model is a class that inherits from `Jennifer::Model::Base`
// and declares its columns inside a `mapping(...)` macro:
//
//	class User < Jennifer::Model::Base
//	  table_name "users"
//
//	  mapping(
//	    id: Primary32,
//	    name: String?,
//	    email: String,
//	  )
//
//	  with_timestamps
//
//	  has_many :posts, Post
//	  belongs_to :account, Account
//	end
//
// What this extractor emits (mirrors the Granite ORM shape — SCOPE.Schema
// entities carrying framework=jennifer + provenance props):
//   - one SCOPE.Schema/model per `class T < Jennifer::Model::Base`.
//   - one SCOPE.Schema/table per model. The table identity is the explicit
//     `table_name "<name>"` macro argument when present, otherwise the model
//     class name (Jennifer's runtime default is the snake_case pluralisation;
//     we record the declared name so the explicit-override case is exact).
//   - one SCOPE.Schema/column per `<name>: <Type>` entry inside the `mapping(…)`
//     macro, stamping column_type (nilable `?` marker trimmed) and the owning
//     model. A `Primary32`/`Primary64`/`primary: true` mapping marks the column
//     primary_key=true.
//   - a REFERENCES edge model → referenced model for each `belongs_to :other`
//     association (the FK signal), keyed by the association name (the explicit
//     `, <Class>` target argument wins over the CamelCased name).
//   - an association SCOPE.Schema/association entity per belongs_to/has_many/
//     has_one/has_and_belongs_to_many carrying assoc_kind + target.
//   - the `with_timestamps` macro synthesises the conventional created_at/
//     updated_at Time audit columns (auto_timestamp=true).
//
// #5366 deepening — query attribution, migrations, transactions:
//   - a `Model.<verb>` query-DSL call site naming an in-file model emits a
//     QUERIES edge model → table stamped with the canonical SQL op.
//   - a Jennifer migration `create_table/drop_table/alter_table :x` DSL call (or
//     raw schema-op SQL via `.exec`) emits a shared SCOPE.Evolution migration-op.
//   - a `<db>.transaction do … end` block emits a SCOPE.Pattern/
//     transaction_boundary. Shared helpers in orm_query_migration.go.
//
// Registration key: "custom_crystal_jennifer_orm".
package crystal

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_crystal_jennifer_orm", &jenniferORMExtractor{})
}

type jenniferORMExtractor struct{}

func (e *jenniferORMExtractor) Language() string { return "custom_crystal_jennifer_orm" }

var (
	// jenniferModelRe matches `class T < Jennifer::Model::Base`. Group 1 is the
	// model class name.
	jenniferModelRe = regexp.MustCompile(
		`(?m)^[ \t]*(?:abstract\s+)?class\s+([A-Z]\w*)\s*<\s*Jennifer::Model::Base\b`)

	// jenniferTableNameRe matches the `table_name "<name>"` / `table_name :name`
	// macro. Group 1 is the table name.
	jenniferTableNameRe = regexp.MustCompile(
		`(?m)^[ \t]*table_name\s+:?["']?([A-Za-z_]\w*)["']?`)

	// jenniferMappingRe matches the whole `mapping( … )` macro body (group 1).
	jenniferMappingRe = regexp.MustCompile(`(?s)\bmapping\s*\(\s*(.*?)\)`)

	// jenniferMappingFieldRe matches one `<name>: <Type>[, opts…]` entry inside a
	// mapping body. Group 1 = field name; group 2 = type token.
	jenniferMappingFieldRe = regexp.MustCompile(
		`(?m)^[ \t]*([a-z_]\w*)\s*:\s*([A-Za-z_][\w:]*\??)(.*)$`)

	// jenniferWithTimestampsRe matches the `with_timestamps` macro.
	jenniferWithTimestampsRe = regexp.MustCompile(`(?m)^[ \t]*with_timestamps\b`)

	// jenniferAssocRe matches a belongs_to / has_many / has_one /
	// has_and_belongs_to_many association. Group 1 = kind; group 2 = name;
	// group 3 = the optional explicit `, <Class>` target argument.
	jenniferAssocRe = regexp.MustCompile(
		`(?m)^[ \t]*(belongs_to|has_many|has_one|has_and_belongs_to_many)\s+:?["']?([a-z_]\w*)["']?(?:\s*,\s*([A-Z][\w:]*))?`)
)

// jenniferHasModel is a fast pre-filter: the file must reference Jennifer's base
// type to be worth scanning.
func jenniferHasModel(content string) bool {
	return strings.Contains(content, "Jennifer::Model::Base")
}

func (e *jenniferORMExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "crystal" {
		return nil, nil
	}
	src := string(file.Content)
	if !jenniferHasModel(src) {
		return nil, nil
	}

	models := collectJenniferModels(src)
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
		model := newCrystalSchema(m.name, "model", "jennifer", file.Path, m.line,
			"INFERRED_FROM_JENNIFER_MODEL")
		var rels []types.RelationshipRecord
		for _, a := range m.assocs {
			if a.kind != "belongs_to" {
				continue
			}
			target := jenniferAssocTarget(a)
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
		rels = append(rels, crystalQueryRels(m.name, tableName, queryOps[m.name], "jennifer")...)
		model.Relationships = rels
		model.ID = model.ComputeID()
		out = append(out, model)

		// 2. table entity.
		table := newCrystalSchema(tableName, "table", "jennifer", file.Path, m.line,
			"INFERRED_FROM_JENNIFER_TABLE")
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
			provenance := "INFERRED_FROM_JENNIFER_COLUMN"
			if c.auto {
				provenance = "INFERRED_FROM_JENNIFER_TIMESTAMPS"
			}
			col := newCrystalSchema(c.name, "column", "jennifer", file.Path, c.line,
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
			assoc := newCrystalSchema(a.name, "association", "jennifer", file.Path, a.line,
				"INFERRED_FROM_JENNIFER_ASSOCIATION")
			assoc.Properties["assoc_kind"] = a.kind
			assoc.Properties["model"] = m.name
			assoc.Properties["target"] = jenniferAssocTarget(a)
			assoc.ID = assoc.ComputeID()
			out = append(out, assoc)
		}
	}

	// 5. transaction boundaries + 6. migration schema ops (#5366).
	out = append(out, collectCrystalTransactions(src, file.Path, "jennifer")...)
	out = append(out, collectCrystalMigrations(src, file.Path, "jennifer")...)
	return out, nil
}

// jenniferAssocTarget resolves the target model: an explicit `, <Class>` argument
// wins, otherwise the CamelCased (singularised for plural has_many/habtm) name.
func jenniferAssocTarget(a jenniferAssoc) string {
	if a.target != "" {
		return a.target
	}
	name := a.name
	if a.kind == "has_many" || a.kind == "has_and_belongs_to_many" {
		name = graniteSingular(name)
	}
	return camelize(name)
}

type jenniferModel struct {
	name    string
	table   string
	line    int
	columns []graniteColumn
	assocs  []jenniferAssoc
}

type jenniferAssoc struct {
	kind   string
	name   string
	target string
	line   int
}

// collectJenniferModels finds every `class T < Jennifer::Model::Base` and the
// table_name/mapping/association macros in its `end`-terminated body.
func collectJenniferModels(src string) []jenniferModel {
	idx := jenniferModelRe.FindAllStringSubmatchIndex(src, -1)
	if len(idx) == 0 {
		return nil
	}
	var models []jenniferModel
	for _, m := range idx {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		bodyEnd := graniteClassEnd(src, m[1])
		body := src[m[1]:bodyEnd]

		jm := jenniferModel{name: name, line: startLine}

		if tm := jenniferTableNameRe.FindStringSubmatch(body); tm != nil {
			jm.table = tm[1]
		}

		// mapping(…) fields.
		if mm := jenniferMappingRe.FindStringSubmatchIndex(body); mm != nil {
			mapBody := body[mm[2]:mm[3]]
			mapBodyStartLine := startLine + strings.Count(body[:mm[2]], "\n")
			for _, fm := range jenniferMappingFieldRe.FindAllStringSubmatchIndex(mapBody, -1) {
				fname := mapBody[fm[2]:fm[3]]
				ftyp := mapBody[fm[4]:fm[5]]
				rest := ""
				if fm[6] >= 0 {
					rest = mapBody[fm[6]:fm[7]]
				}
				primary := jenniferFieldIsPrimary(ftyp, rest)
				ftyp = jenniferNormaliseType(strings.TrimSuffix(ftyp, "?"))
				jm.columns = append(jm.columns, graniteColumn{
					name:    fname,
					typ:     ftyp,
					primary: primary,
					line:    mapBodyStartLine + strings.Count(mapBody[:fm[0]], "\n"),
				})
			}
		}

		// with_timestamps → created_at/updated_at Time columns.
		if tsLoc := jenniferWithTimestampsRe.FindStringIndex(body); tsLoc != nil {
			tsLine := startLine + strings.Count(body[:tsLoc[0]], "\n")
			for _, n := range []string{"created_at", "updated_at"} {
				jm.columns = append(jm.columns, graniteColumn{
					name: n, typ: "Time", auto: true, line: tsLine,
				})
			}
		}

		for _, am := range jenniferAssocRe.FindAllStringSubmatchIndex(body, -1) {
			a := jenniferAssoc{
				kind: body[am[2]:am[3]],
				name: body[am[4]:am[5]],
				line: startLine + strings.Count(body[:am[0]], "\n"),
			}
			if am[6] >= 0 {
				a.target = body[am[6]:am[7]]
			}
			jm.assocs = append(jm.assocs, a)
		}
		models = append(models, jm)
	}
	return models
}

// jenniferFieldIsPrimary reports whether a mapping field is the primary key: a
// `Primary32`/`Primary64` type alias, or an explicit `primary: true` option.
func jenniferFieldIsPrimary(typ, rest string) bool {
	return strings.HasPrefix(typ, "Primary") || strings.Contains(rest, "primary: true") ||
		strings.Contains(rest, "primary:true")
}

// jenniferNormaliseType maps Jennifer's Primary32/Primary64 mapping aliases to
// their concrete integer types (the runtime mapping); other types pass through.
func jenniferNormaliseType(typ string) string {
	switch typ {
	case "Primary32":
		return "Int32"
	case "Primary64":
		return "Int64"
	default:
		return typ
	}
}
