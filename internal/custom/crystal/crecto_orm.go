// crecto_orm.go — Crystal Crecto ORM model → table/column/association schema
// synthesis (#4936, follow-up to #4905/#3871).
//
// Crecto (https://github.com/Crecto/crecto) is a Crystal ORM modelled on
// Elixir's Ecto. A persisted model is a class that mixes in `Crecto::Model` and
// declares its schema with a `schema "<table>" do … end` block of `field`
// macros:
//
//	class User
//	  include Crecto::Schema
//
//	  schema "users" do
//	    field :name, String
//	    field :email, String
//	    field :age, Int32
//	    has_many :posts, Post
//	    belongs_to :account, Account
//	  end
//	end
//
// Crecto injects an implicit `id` primary key by default (the `primary_key`
// option overrides it); the `schema` block name is the table.
//
// What this extractor emits (mirrors the Granite ORM shape — SCOPE.Schema
// entities carrying framework=crecto + provenance props):
//   - one SCOPE.Schema/model per class carrying a `schema "<table>" do` block
//     (the file must reference Crecto::Schema so we never misfire).
//   - one SCOPE.Schema/table per model, keyed by the `schema "<name>"` argument.
//   - one SCOPE.Schema/column per `field :<name>, <Type>` macro, stamping
//     column_type + the owning model. The implicit `id` primary key is
//     synthesised (primary_key=true) unless the model opts out.
//   - a REFERENCES edge model → referenced model for each `belongs_to`.
//   - an association SCOPE.Schema/association entity per belongs_to/has_many/
//     has_one carrying assoc_kind + target.
//
// #5366 deepening — query attribution, migrations, transactions:
//   - a `Repo.<verb>(Model, …)` call site naming an in-file model (Crecto routes
//     reads through the Repo with the model as the leading argument) emits a
//     QUERIES edge model → table stamped with the canonical SQL op.
//   - a migration `create_table/drop_table/alter_table :x` DSL call (or raw
//     schema-op SQL via `.exec`) emits a shared SCOPE.Evolution migration-op.
//   - a `<db>.transaction do … end` block emits a SCOPE.Pattern/
//     transaction_boundary. Shared helpers in orm_query_migration.go.
//
// Registration key: "custom_crystal_crecto_orm".
package crystal

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_crystal_crecto_orm", &crectoORMExtractor{})
}

type crectoORMExtractor struct{}

func (e *crectoORMExtractor) Language() string { return "custom_crystal_crecto_orm" }

var (
	// crectoClassRe matches a class declaration. Group 1 is the model class name.
	// The model marker (`include Crecto::Schema` + a `schema "…" do` block) is
	// checked within the body.
	crectoClassRe = regexp.MustCompile(
		`(?m)^[ \t]*(?:abstract\s+)?class\s+([A-Z]\w*)\b`)

	// crectoSchemaRe matches the `schema "<table>" do` block opener. Group 1 is
	// the table name.
	crectoSchemaRe = regexp.MustCompile(
		`(?m)^[ \t]*schema\s+["']([A-Za-z_]\w*)["']\s+do\b`)

	// crectoFieldRe matches a `field :<name>, <Type>[, opts…]` macro. Group 1 =
	// field name; group 2 = type token.
	crectoFieldRe = regexp.MustCompile(
		`(?m)^[ \t]*field\s+:?["']?([a-z_]\w*)["']?\s*,\s*([A-Za-z_][\w:]*)`)

	// crectoAssocRe matches a belongs_to / has_many / has_one association.
	// Group 1 = kind; group 2 = name; group 3 = the optional explicit
	// `, <Class>` target argument.
	crectoAssocRe = regexp.MustCompile(
		`(?m)^[ \t]*(belongs_to|has_many|has_one)\s+:?["']?([a-z_]\w*)["']?(?:\s*,\s*([A-Z][\w:]*))?`)

	// crectoPrimaryKeyOptRe matches an explicit `set_primary_key` / `primary_key`
	// override declaration (opts out of the implicit id synthesis).
	crectoPrimaryKeyOptRe = regexp.MustCompile(`(?m)^[ \t]*(?:set_primary_key|primary_key)\b`)

	// crectoRepoQueryRe matches a Crecto Repo query whose FIRST argument names the
	// queried model — `Repo.all(User)`, `Repo.get(User, id)`,
	// `Repo.get_by(User, ...)`, `Repo.delete_all(User)`. Crecto routes every read
	// through the Repo with the model class as the leading argument, so this is the
	// statically-attributable query form (the instance-arg insert/update/delete
	// pass a lowercase changeset/struct that is not a model name → honest skip).
	// Group 1 = Repo verb; group 2 = the model class argument.
	crectoRepoQueryRe = regexp.MustCompile(
		`(?m)\b\w*Repo\s*\.\s*(all|get_by|get|where|query|aggregate|delete_all|update_all)\s*\(\s*([A-Z]\w*)\b`)
)

// collectCrectoRepoQueries attributes a canonical SQL op to a model for each
// `Repo.<verb>(Model, …)` call site naming an in-file model.
func collectCrectoRepoQueries(src string, modelNames map[string]bool) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, m := range crectoRepoQueryRe.FindAllStringSubmatch(src, -1) {
		verb, model := m[1], m[2]
		if !modelNames[model] {
			continue
		}
		op := crystalQueryOp(verb)
		if out[model] == nil {
			out[model] = map[string]bool{}
		}
		out[model][op] = true
	}
	return out
}

// crectoHasModel is a fast pre-filter: the file must reference Crecto::Schema.
func crectoHasModel(content string) bool {
	return strings.Contains(content, "Crecto::Schema")
}

func (e *crectoORMExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "crystal" {
		return nil, nil
	}
	src := string(file.Content)
	if !crectoHasModel(src) {
		return nil, nil
	}

	models := collectCrectoModels(src)
	if len(models) == 0 {
		return nil, nil
	}
	modelNames := make(map[string]bool, len(models))
	for _, m := range models {
		modelNames[m.name] = true
	}
	queryOps := collectCrectoRepoQueries(src, modelNames)

	var out []types.EntityRecord
	for _, m := range models {
		// 1. model entity + belongs_to REFERENCES edges.
		model := newCrystalSchema(m.name, "model", "crecto", file.Path, m.line,
			"INFERRED_FROM_CRECTO_MODEL")
		var rels []types.RelationshipRecord
		for _, a := range m.assocs {
			if a.kind != "belongs_to" {
				continue
			}
			target := crectoAssocTarget(a)
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
		rels = append(rels, crystalQueryRels(m.name, m.table, queryOps[m.name], "crecto")...)
		model.Relationships = rels
		model.ID = model.ComputeID()
		out = append(out, model)

		// 2. table entity (the schema "<name>" argument).
		table := newCrystalSchema(m.table, "table", "crecto", file.Path, m.line,
			"INFERRED_FROM_CRECTO_TABLE")
		table.Properties["model"] = m.name
		table.ID = table.ComputeID()
		out = append(out, table)

		// 3. column entities, with the implicit id primary key synthesised
		//    unless the model declares an explicit primary-key override.
		colSeen := make(map[string]bool)
		if !m.explicitPK {
			col := newCrystalSchema("id", "column", "crecto", file.Path, m.line,
				"INFERRED_FROM_CRECTO_IMPLICIT_PK")
			col.Properties["column_type"] = "PkeyValue"
			col.Properties["model"] = m.name
			col.Properties["primary_key"] = "true"
			col.ID = col.ComputeID()
			out = append(out, col)
			colSeen["id"] = true
		}
		for _, c := range m.columns {
			if colSeen[c.name] {
				continue
			}
			colSeen[c.name] = true
			col := newCrystalSchema(c.name, "column", "crecto", file.Path, c.line,
				"INFERRED_FROM_CRECTO_FIELD")
			col.Properties["column_type"] = c.typ
			col.Properties["model"] = m.name
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
			assoc := newCrystalSchema(a.name, "association", "crecto", file.Path, a.line,
				"INFERRED_FROM_CRECTO_ASSOCIATION")
			assoc.Properties["assoc_kind"] = a.kind
			assoc.Properties["model"] = m.name
			assoc.Properties["target"] = crectoAssocTarget(a)
			assoc.ID = assoc.ComputeID()
			out = append(out, assoc)
		}
	}

	// 5. transaction boundaries + 6. migration schema ops (#5366).
	out = append(out, collectCrystalTransactions(src, file.Path, "crecto")...)
	out = append(out, collectCrystalMigrations(src, file.Path, "crecto")...)
	return out, nil
}

// crectoAssocTarget resolves the association target model: an explicit
// `, <Class>` argument wins, otherwise the CamelCased (singularised for plural
// has_many) name.
func crectoAssocTarget(a crectoAssoc) string {
	if a.target != "" {
		return a.target
	}
	name := a.name
	if a.kind == "has_many" {
		name = graniteSingular(name)
	}
	return camelize(name)
}

type crectoModel struct {
	name       string
	table      string
	line       int
	explicitPK bool
	columns    []graniteColumn
	assocs     []crectoAssoc
}

type crectoAssoc struct {
	kind   string
	name   string
	target string
	line   int
}

// collectCrectoModels finds every class carrying a `schema "<table>" do` block
// and the field/association macros in its body.
func collectCrectoModels(src string) []crectoModel {
	idx := crectoClassRe.FindAllStringSubmatchIndex(src, -1)
	if len(idx) == 0 {
		return nil
	}
	var models []crectoModel
	for _, m := range idx {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		bodyEnd := graniteClassEnd(src, m[1])
		body := src[m[1]:bodyEnd]

		sm := crectoSchemaRe.FindStringSubmatch(body)
		if sm == nil {
			continue // not a Crecto schema-bearing class
		}

		cm := crectoModel{
			name:       name,
			table:      sm[1],
			line:       startLine,
			explicitPK: crectoPrimaryKeyOptRe.MatchString(body),
		}

		for _, fm := range crectoFieldRe.FindAllStringSubmatchIndex(body, -1) {
			cm.columns = append(cm.columns, graniteColumn{
				name: body[fm[2]:fm[3]],
				typ:  body[fm[4]:fm[5]],
				line: startLine + strings.Count(body[:fm[0]], "\n"),
			})
		}

		for _, as := range crectoAssocRe.FindAllStringSubmatchIndex(body, -1) {
			a := crectoAssoc{
				kind: body[as[2]:as[3]],
				name: body[as[4]:as[5]],
				line: startLine + strings.Count(body[:as[0]], "\n"),
			}
			if as[6] >= 0 {
				a.target = body[as[6]:as[7]]
			}
			cm.assocs = append(cm.assocs, a)
		}
		models = append(models, cm)
	}
	return models
}
