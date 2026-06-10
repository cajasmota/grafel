// activerecord_deep.go — deep ActiveRecord extraction.
//
// This file raises the ActiveRecord extraction to the TS/JS (TypeORM / Prisma)
// quality bar. Where the existing heuristics in activerecord.go emit shallow
// "≥1 entity" markers, the helpers here emit *value-asserting* entities that
// carry the real shape of the schema:
//
//   - Models:        link a model class (User < ApplicationRecord) to its table
//     by Rails convention (User → users), and extract its columns
//     from db/schema.rb / migrations (AR models don't declare
//     columns in the class body).
//   - Associations:  has_many/belongs_to/has_one/HABTM incl. :through, :source,
//     :class_name, :foreign_key, polymorphic (polymorphic:true / as:)
//     with target-model inference and option capture.
//   - Foreign keys:  belongs_to convention (x_id), explicit foreign_key:,
//     add_foreign_key / t.references / t.belongs_to / add_reference.
//   - Migrations:    db/migrate/*.rb create_table / add_column / add_index /
//     add_reference / add_foreign_key / change_column / remove_*
//     normalized to shared SCOPE.Evolution op subtypes, plus
//     t.<type> columns *inside* a create_table block.
//
// Part of the deep-ruby-activerecord work (TS/JS bar parity).
package ruby

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/lifecycle"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Deep regexes (named *Deep to avoid clashing with the shallow ones).
// ---------------------------------------------------------------------------

var (
	// Model class: `class User < ApplicationRecord` / `< ActiveRecord::Base`.
	reARModelClass = regexp.MustCompile(
		`(?m)^\s*class\s+([A-Z][A-Za-z0-9_:]*)\s*<\s*(ApplicationRecord|ActiveRecord::Base)\b`,
	)

	// Association macro with the full trailing option string captured (group 3),
	// so we can mine :through/:source/:class_name/:foreign_key/polymorphic/as.
	reARAssociationDeep = regexp.MustCompile(
		`(?m)^\s*(has_many|belongs_to|has_one|has_and_belongs_to_many)\s+:([a-z_][a-z0-9_]*)\s*(,[^#\n]*)?$`,
	)

	// schema.rb / migration: create_table "name" do |t|  (also :name form).
	reARCreateTableBlock = regexp.MustCompile(
		`^\s*create_table\s+["':]([a-z_][a-z0-9_]*)["']?`,
	)

	// A typed column line inside a create_table block: t.string "email", null: false
	// Group 1 = type, group 2 = name, group 3 = trailing options.
	reARTypedColumn = regexp.MustCompile(
		`^\s*t\.(string|integer|bigint|float|decimal|numeric|boolean|text|date|datetime|timestamp|time|binary|blob|json|jsonb|uuid|hstore|inet|cidr|macaddr|citext|ltree|virtual|primary_key)\s+["':]([a-z_][a-z0-9_]*)["']?\s*(,[^#\n]*)?`,
	)

	// t.references / t.belongs_to "user", polymorphic: true  (inside create_table).
	reARTReference = regexp.MustCompile(
		`^\s*t\.(references|belongs_to)\s+["':]([a-z_][a-z0-9_]*)["']?\s*(,[^#\n]*)?`,
	)

	// t.timestamps — shorthand for created_at/updated_at.
	reARTimestamps = regexp.MustCompile(`^\s*t\.timestamps\b`)

	// add_column :table, :col, :type[, options]
	reARAddColumnDeep = regexp.MustCompile(
		`^\s*add_column\s+["':]([a-z_][a-z0-9_]*)["']?\s*,\s*["':]([a-z_][a-z0-9_]*)["']?\s*,\s*["':]?([a-z_][a-z0-9_]*)["']?\s*(,[^#\n]*)?`,
	)

	// remove_column :table, :col
	reARRemoveColumn = regexp.MustCompile(
		`^\s*remove_column\s+["':]([a-z_][a-z0-9_]*)["']?\s*,\s*["':]([a-z_][a-z0-9_]*)["']?`,
	)

	// change_column :table, :col, :type
	reARChangeColumn = regexp.MustCompile(
		`^\s*change_column\s+["':]([a-z_][a-z0-9_]*)["']?\s*,\s*["':]([a-z_][a-z0-9_]*)["']?`,
	)

	// add_index :table, :col_or_[cols]
	reARAddIndexDeep = regexp.MustCompile(
		`^\s*add_index\s+["':]([a-z_][a-z0-9_]*)["']?\s*,\s*(.+)`,
	)

	// add_reference / add_belongs_to :table, :ref[, options]
	reARAddReferenceDeep = regexp.MustCompile(
		`^\s*(?:add_reference|add_belongs_to)\s+["':]([a-z_][a-z0-9_]*)["']?\s*,\s*["':]([a-z_][a-z0-9_]*)["']?\s*(,[^#\n]*)?`,
	)

	// add_foreign_key "from_table", "to_table"[, options]
	reARAddForeignKey = regexp.MustCompile(
		`^\s*add_foreign_key\s+["':]([a-z_][a-z0-9_]*)["']?\s*,\s*["':]([a-z_][a-z0-9_]*)["']?\s*(,[^#\n]*)?`,
	)

	// drop_table :name
	reARDropTable = regexp.MustCompile(
		`^\s*drop_table\s+["':]([a-z_][a-z0-9_]*)["']?`,
	)

	// Option scanners (work on the trailing-options string of an association).
	reOptClassName  = regexp.MustCompile(`class_name:\s*["']([A-Za-z0-9_:]+)["']|:class_name\s*=>\s*["']([A-Za-z0-9_:]+)["']`)
	reOptForeignKey = regexp.MustCompile(`foreign_key:\s*["':]([a-z_][a-z0-9_]*)["']?|:foreign_key\s*=>\s*["':]([a-z_][a-z0-9_]*)["']?`)
	reOptThrough    = regexp.MustCompile(`through:\s*:([a-z_][a-z0-9_]*)|:through\s*=>\s*:([a-z_][a-z0-9_]*)`)
	reOptSource     = regexp.MustCompile(`source:\s*:([a-z_][a-z0-9_]*)|:source\s*=>\s*:([a-z_][a-z0-9_]*)`)
	reOptAs         = regexp.MustCompile(`as:\s*:([a-z_][a-z0-9_]*)|:as\s*=>\s*:([a-z_][a-z0-9_]*)`)
	reOptPolymorph  = regexp.MustCompile(`polymorphic:\s*true|:polymorphic\s*=>\s*true`)
	reOptNull       = regexp.MustCompile(`null:\s*(true|false)`)
	reOptDefault    = regexp.MustCompile(`default:\s*("(?:[^"]*)"|'(?:[^']*)'|[A-Za-z0-9_.\[\]{}]+)`)
	reOptLimit      = regexp.MustCompile(`limit:\s*(\d+)`)
	reOptToTable    = regexp.MustCompile(`to_table:\s*:?["']?([a-z_][a-z0-9_]*)["']?`)
)

// ---------------------------------------------------------------------------
// Rails inflection helpers (enough for convention-based linking).
// ---------------------------------------------------------------------------

// singularize converts a (mostly-regular) plural to singular. Handles the
// common irregular and *-ies/*-es/*-s cases that show up in table names.
func singularize(w string) string {
	irr := map[string]string{
		"people": "person", "men": "man", "women": "woman", "children": "child",
		"teeth": "tooth", "feet": "foot", "mice": "mouse", "geese": "goose",
		"data": "datum", "indices": "index", "matrices": "matrix", "media": "medium",
	}
	if s, ok := irr[w]; ok {
		return s
	}
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 3:
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "ses") || strings.HasSuffix(w, "xes") ||
		strings.HasSuffix(w, "zes") || strings.HasSuffix(w, "ches") ||
		strings.HasSuffix(w, "shes"):
		return w[:len(w)-2]
	case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss"):
		return w[:len(w)-1]
	default:
		return w
	}
}

// pluralize is the inverse used to map a model name to its table.
func pluralize(w string) string {
	switch {
	case strings.HasSuffix(w, "y") && len(w) > 1 && !isVowel(w[len(w)-2]):
		return w[:len(w)-1] + "ies"
	case strings.HasSuffix(w, "s") || strings.HasSuffix(w, "x") ||
		strings.HasSuffix(w, "z") || strings.HasSuffix(w, "ch") ||
		strings.HasSuffix(w, "sh"):
		return w + "es"
	default:
		return w + "s"
	}
}

func isVowel(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	}
	return false
}

// camelize converts snake_case to CamelCase (Rails classify, minus pluralize).
func camelize(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// classify maps a (plural) table name to its model class name: users → User.
func classify(table string) string {
	// Drop a leading schema qualifier if present (public.users → users).
	if i := strings.LastIndexByte(table, '.'); i >= 0 {
		table = table[i+1:]
	}
	return camelize(singularize(table))
}

// modelToTable maps a model class to its table by Rails convention: User → users.
// Handles namespaced models (Admin::User → users — Rails uses the demodulized
// name for the table unless table_name_prefix is set).
func modelToTable(model string) string {
	if i := strings.LastIndex(model, "::"); i >= 0 {
		model = model[i+2:]
	}
	return pluralize(underscore(model))
}

// underscore is the inverse of camelize: BlogPost → blog_post.
func underscore(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteByte(c - 'A' + 'a')
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Option extraction
// ---------------------------------------------------------------------------

func firstNonEmptyGroup(m []string) string {
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}

type assocOptions struct {
	className  string
	foreignKey string
	through    string
	source     string
	as         string
	polymorph  bool
}

func parseAssocOptions(opts string) assocOptions {
	var a assocOptions
	if m := reOptClassName.FindStringSubmatch(opts); m != nil {
		a.className = firstNonEmptyGroup(m)
	}
	if m := reOptForeignKey.FindStringSubmatch(opts); m != nil {
		a.foreignKey = firstNonEmptyGroup(m)
	}
	if m := reOptThrough.FindStringSubmatch(opts); m != nil {
		a.through = firstNonEmptyGroup(m)
	}
	if m := reOptSource.FindStringSubmatch(opts); m != nil {
		a.source = firstNonEmptyGroup(m)
	}
	if m := reOptAs.FindStringSubmatch(opts); m != nil {
		a.as = firstNonEmptyGroup(m)
	}
	a.polymorph = reOptPolymorph.MatchString(opts)
	return a
}

// targetModel infers the associated model class for an association.
//   - explicit class_name wins
//   - belongs_to/has_one → singular: User
//   - has_many/HABTM     → singular of the (already-plural) association name
func targetModel(macro, assocName string, o assocOptions) string {
	if o.className != "" {
		return o.className
	}
	switch macro {
	case "has_many", "has_and_belongs_to_many":
		return camelize(singularize(assocName))
	default: // belongs_to, has_one
		return camelize(assocName)
	}
}

// ---------------------------------------------------------------------------
// Deep extraction entry points (called from Extract in activerecord.go)
// ---------------------------------------------------------------------------

// extractARModelsAndAssociations handles app/models files: the model class →
// table link, and rich association + foreign-key entities.
func extractARModelsAndAssociations(src string, file extractor.FileInput, add func(types.EntityRecord)) {
	// Model class → table link (Models / model_extraction + schema link).
	// The model entity is built up-front but added LAST so association loops
	// below can hang GRAPH_RELATES edges off it (one model class per file is the
	// Rails convention reARModelClass keys on).
	var modelName, tableName string
	var modelEnt *types.EntityRecord
	if m := reARModelClass.FindStringSubmatchIndex(src); m != nil {
		modelName = src[m[2]:m[3]]
		tableName = modelToTable(modelName)
		me := makeEntity("model:"+modelName, "SCOPE.Schema", "model", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&me,
			"framework", "activerecord",
			"provenance", "INFERRED_FROM_AR_MODEL_CLASS",
			"model_class", modelName,
			"table_name", tableName,
		)
		// Data-lifecycle traits (#3628 child). soft-delete is observable in the
		// model body (acts_as_paranoid / default_scope deleted_at); timestamps
		// live in the schema, not the class body, so they stay honest-partial
		// here (omitted). Columns referenced via belongs_to (x_by) feed audit
		// detection.
		lifecycle.RailsModelTraits(src, arReferencedColumns(src)).
			Stamp(func(kv ...string) { setProps(&me, kv...) })
		modelEnt = &me
	}

	for _, m := range reARAssociationDeep.FindAllStringSubmatchIndex(src, -1) {
		macro := src[m[2]:m[3]]
		assocName := src[m[4]:m[5]]
		opts := ""
		if m[6] >= 0 {
			opts = src[m[6]:m[7]]
		}
		o := parseAssocOptions(opts)
		target := targetModel(macro, assocName, o)
		ln := lineOf(src, m[0])

		// Rich relationship entity (Relationships / relationship_extraction +
		// association_extraction). Distinct name so it doesn't collide with the
		// shallow `belongs_to:user` relation entity from activerecord.go.
		relName := fmt.Sprintf("assoc:%s:%s", macro, assocName)
		rel := makeEntity(relName, "SCOPE.Pattern", "association", file.Path, file.Language, ln)
		setProps(&rel,
			"framework", "activerecord",
			"provenance", "INFERRED_FROM_AR_ASSOCIATION_DEEP",
			"association_type", macro,
			"association_name", assocName,
			"target_model", target,
		)
		if o.through != "" {
			setProps(&rel, "through", o.through)
		}
		if o.source != "" {
			setProps(&rel, "source", o.source)
		}
		if o.className != "" {
			setProps(&rel, "class_name", o.className)
		}
		if o.foreignKey != "" {
			setProps(&rel, "foreign_key", o.foreignKey)
		}
		if o.polymorph {
			setProps(&rel, "polymorphic", "true")
		}
		if o.as != "" {
			setProps(&rel, "as", o.as)
		}
		if modelName != "" {
			setProps(&rel, "owner_model", modelName)
		}
		// #4367 — field membership + relation target. The association entity was
		// a degree-0 orphan: no owning-model membership, no target-model edge.
		//   • REFERENCES (assoc entity → Class:<target>): the related model is the
		//     association's only outbound semantic edge. Polymorphic associations
		//     have no single concrete target, so they stay honest-partial.
		//   • CONTAINS (Class:<owner> → assoc entity): hung off the owner model
		//     node so the field is a member, not an orphan.
		if target != "" && !o.polymorph {
			rel.Relationships = append(rel.Relationships,
				referencesClassEdge(rel.ID, target, "activerecord", assocName))
		}
		if modelEnt != nil {
			modelEnt.Relationships = append(modelEnt.Relationships,
				containsFieldEdge(modelName, rel.ID, assocName, "activerecord"))
		}
		add(rel)

		// GRAPH_RELATES model↔model edge with cardinality, hung off the owner
		// model node. Polymorphic associations have no single concrete target
		// class, so they stay honest-partial (relation entity only, no edge).
		// `target` already honors class_name: and Rails singular/plural
		// inflection, so the ToID resolves to the real target model node via the
		// Class:<Name> byName convention.
		if modelEnt != nil && target != "" && !o.polymorph {
			if card := arAssocCardinality(macro); card != "" {
				modelEnt.Relationships = append(modelEnt.Relationships,
					types.RelationshipRecord{
						FromID: "Class:" + modelName,
						ToID:   "Class:" + target,
						Kind:   string(types.RelationshipKindGraphRelates),
						Properties: map[string]string{
							"framework":        "activerecord",
							"cardinality":      card,
							"association_type": macro,
							"association_name": assocName,
							"provenance":       "INFERRED_FROM_AR_ASSOCIATION_DEEP",
						},
					})
			}
		}

		// Foreign-key entity (Relationships / foreign_key_extraction).
		// belongs_to defines the FK on *this* model's table (convention x_id),
		// or the explicit foreign_key. has_one/has_many put the FK on the other
		// table; we still record the convention so the column can be resolved.
		fk := foreignKeyForAssoc(macro, assocName, o)
		if fk != "" {
			fkName := fmt.Sprintf("ar_fk:%s:%s", assocName, fk)
			fkEnt := makeEntity(fkName, "SCOPE.Pattern", "foreign_key", file.Path, file.Language, ln)
			setProps(&fkEnt,
				"framework", "activerecord",
				"provenance", "INFERRED_FROM_AR_BELONGS_TO_FK",
				"association_type", macro,
				"association_name", assocName,
				"foreign_key", fk,
				"target_model", target,
			)
			if macro == "belongs_to" && o.foreignKey == "" {
				setProps(&fkEnt, "convention", "true")
			}
			if o.polymorph {
				setProps(&fkEnt, "polymorphic", "true")
				setProps(&fkEnt, "type_column", assocName+"_type")
			}
			// #4367 — FK field membership + relation target (same shape as the
			// association entity above).
			if target != "" && !o.polymorph {
				fkEnt.Relationships = append(fkEnt.Relationships,
					referencesClassEdge(fkEnt.ID, target, "activerecord", assocName))
			}
			if modelEnt != nil {
				modelEnt.Relationships = append(modelEnt.Relationships,
					containsFieldEdge(modelName, fkEnt.ID, assocName, "activerecord"))
			}
			add(fkEnt)
		}
	}

	// Add the model node last so it carries the GRAPH_RELATES edges accumulated
	// above. (Deferred from the top of the function.)
	if modelEnt != nil {
		add(*modelEnt)
	}
}

// reARAuditCol matches conventional audit-column identifiers referenced in a
// model body — e.g. `belongs_to :created_by`, `attribute :updated_by`, or a
// bare `created_by` symbol. Bounded to the closed convention set so arbitrary
// identifiers are never mistaken for audit columns.
var reARAuditCol = regexp.MustCompile(
	`(?m)\b(created_by|updated_by|creator_id|updater_id|deleted_by)\b`,
)

// arReferencedColumns returns the conventional audit-column names referenced
// anywhere in the model body. lifecycle.RailsModelTraits filters these against
// its own audit convention; this just surfaces candidate tokens.
func arReferencedColumns(src string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range reARAuditCol.FindAllStringSubmatch(src, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// arAssocCardinality maps an ActiveRecord association macro to the shared ORM
// relationship-cardinality vocabulary, used as the `cardinality` prop on the
// GRAPH_RELATES edge between the owner model node and the target model node.
//
//	has_many :orders                  → one_to_many
//	belongs_to :user                  → many_to_one
//	has_one :profile                  → one_to_one
//	has_and_belongs_to_many :tags     → many_to_many
func arAssocCardinality(macro string) string {
	switch macro {
	case "has_many":
		return "one_to_many"
	case "belongs_to":
		return "many_to_one"
	case "has_one":
		return "one_to_one"
	case "has_and_belongs_to_many":
		return "many_to_many"
	default:
		return ""
	}
}

// foreignKeyForAssoc returns the FK column an association implies, or "".
func foreignKeyForAssoc(macro, assocName string, o assocOptions) string {
	if o.foreignKey != "" {
		return o.foreignKey
	}
	switch macro {
	case "belongs_to":
		return assocName + "_id"
	case "has_one", "has_many":
		// FK lives on the *other* table by convention <this_singular>_id; we
		// don't know "this" model name reliably here, so only emit when explicit.
		return ""
	default:
		return ""
	}
}

// extractARSchemaDeep parses db/schema.rb: tables, typed columns (with options),
// t.references, t.timestamps, and links each table back to its model class.
func extractARSchemaDeep(src string, file extractor.FileInput, add func(types.EntityRecord)) {
	lines := strings.Split(src, "\n")
	currentTable := ""
	for i, line := range lines {
		ln := i + 1

		if tm := reARCreateTableBlock.FindStringSubmatch(line); tm != nil {
			currentTable = tm[1]
			model := classify(currentTable)
			ent := makeEntity("ar_table:"+currentTable, "SCOPE.Schema", "table", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "activerecord",
				"provenance", "INFERRED_FROM_SCHEMA_RB_DEEP",
				"table_name", currentTable,
				"model_class", model,
			)
			add(ent)
			continue
		}
		if currentTable == "" {
			continue
		}

		// Typed column with options.
		if cm := reARTypedColumn.FindStringSubmatch(line); cm != nil {
			colType := cm[1]
			colName := cm[2]
			opts := cm[3]
			emitSchemaColumn(currentTable, colName, colType, opts, file, ln, add)
			continue
		}

		// t.references / t.belongs_to → adds <name>_id column + FK.
		if rm := reARTReference.FindStringSubmatch(line); rm != nil {
			refName := rm[2]
			opts := rm[3]
			emitReferenceColumn(currentTable, refName, opts, "INFERRED_FROM_SCHEMA_RB_REFERENCE", file, ln, add)
			continue
		}

		// t.timestamps → created_at / updated_at datetime columns.
		if reARTimestamps.MatchString(line) {
			for _, c := range []string{"created_at", "updated_at"} {
				emitSchemaColumn(currentTable, c, "datetime", "null: false", file, ln, add)
			}
			continue
		}
	}
}

func emitSchemaColumn(table, col, typ, opts string, file extractor.FileInput, ln int, add func(types.EntityRecord)) {
	ent := makeEntity("ar_col:"+table+"."+col, "SCOPE.Schema", "column", file.Path, file.Language, ln)
	setProps(&ent,
		"framework", "activerecord",
		"provenance", "INFERRED_FROM_SCHEMA_RB_DEEP",
		"table_name", table,
		"column_name", col,
		"column_type", typ,
		"model_class", classify(table),
	)
	applyColumnOptions(&ent, opts)
	add(ent)
}

func emitReferenceColumn(table, ref, opts, prov string, file extractor.FileInput, ln int, add func(types.EntityRecord)) {
	poly := reOptPolymorph.MatchString(opts)
	fkCol := ref + "_id"
	// The FK column.
	col := makeEntity("ar_col:"+table+"."+fkCol, "SCOPE.Schema", "column", file.Path, file.Language, ln)
	setProps(&col,
		"framework", "activerecord",
		"provenance", prov,
		"table_name", table,
		"column_name", fkCol,
		"column_type", "bigint",
		"model_class", classify(table),
		"is_reference", "true",
	)
	if poly {
		setProps(&col, "polymorphic", "true")
	}
	add(col)

	// The implied foreign key.
	fk := makeEntity("ar_fk:"+table+"."+fkCol, "SCOPE.Pattern", "foreign_key", file.Path, file.Language, ln)
	setProps(&fk,
		"framework", "activerecord",
		"provenance", prov,
		"table_name", table,
		"column_name", fkCol,
		"reference_name", ref,
		"target_model", camelize(ref),
	)
	if poly {
		setProps(&fk, "polymorphic", "true")
		// polymorphic adds a <ref>_type string column too.
		typeCol := makeEntity("ar_col:"+table+"."+ref+"_type", "SCOPE.Schema", "column", file.Path, file.Language, ln)
		setProps(&typeCol,
			"framework", "activerecord",
			"provenance", prov,
			"table_name", table,
			"column_name", ref+"_type",
			"column_type", "string",
			"model_class", classify(table),
			"polymorphic", "true",
		)
		add(typeCol)
	}
	add(fk)
}

func applyColumnOptions(e *types.EntityRecord, opts string) {
	if opts == "" {
		return
	}
	if m := reOptNull.FindStringSubmatch(opts); m != nil {
		setProps(e, "nullable", m[1])
	}
	if m := reOptDefault.FindStringSubmatch(opts); m != nil {
		setProps(e, "default", strings.Trim(m[1], `"'`))
	}
	if m := reOptLimit.FindStringSubmatch(opts); m != nil {
		setProps(e, "limit", m[1])
	}
}

// extractARMigrationDeep parses db/migrate/*.rb migration files into normalized
// SCOPE.Evolution schema-change ops (matching the TypeORM op-subtype taxonomy),
// plus columns declared inside create_table blocks.
func extractARMigrationDeep(src string, file extractor.FileInput, add func(types.EntityRecord)) {
	lines := strings.Split(src, "\n")
	currentTable := "" // active create_table block target
	for i, line := range lines {
		ln := i + 1

		if tm := reARCreateTableBlock.FindStringSubmatch(line); tm != nil {
			currentTable = tm[1]
			emitMigrationOp("create_table", currentTable, file, ln, add, "table_name", currentTable, "model_class", classify(currentTable))
			continue
		}
		if reARDropTable.MatchString(line) {
			tbl := reARDropTable.FindStringSubmatch(line)[1]
			currentTable = ""
			emitMigrationOp("drop_table", tbl, file, ln, add, "table_name", tbl)
			continue
		}

		// Inside a create_table block: typed columns, references, timestamps.
		if currentTable != "" {
			if cm := reARTypedColumn.FindStringSubmatch(line); cm != nil {
				emitMigColumn(currentTable, cm[2], cm[1], cm[3], file, ln, add)
				continue
			}
			if rm := reARTReference.FindStringSubmatch(line); rm != nil {
				emitReferenceColumn(currentTable, rm[2], rm[3], "INFERRED_FROM_AR_MIGRATION_REFERENCE", file, ln, add)
				emitMigrationOp("add_reference", currentTable, file, ln, add,
					"table_name", currentTable, "reference_name", rm[2])
				continue
			}
			if reARTimestamps.MatchString(line) {
				for _, c := range []string{"created_at", "updated_at"} {
					emitMigColumn(currentTable, c, "datetime", "", file, ln, add)
				}
				continue
			}
			// A line with just `end` closes the block (best-effort).
			if strings.TrimSpace(line) == "end" {
				currentTable = ""
			}
		}

		// Standalone DDL ops (outside create_table).
		if m := reARAddColumnDeep.FindStringSubmatch(line); m != nil {
			emitMigColumn(m[1], m[2], m[3], m[4], file, ln, add)
			emitMigrationOp("add_column", m[1]+"."+m[2], file, ln, add,
				"table_name", m[1], "column_name", m[2], "column_type", m[3])
			continue
		}
		if m := reARRemoveColumn.FindStringSubmatch(line); m != nil {
			emitMigrationOp("drop_column", m[1]+"."+m[2], file, ln, add,
				"table_name", m[1], "column_name", m[2])
			continue
		}
		if m := reARChangeColumn.FindStringSubmatch(line); m != nil {
			emitMigrationOp("alter_column", m[1]+"."+m[2], file, ln, add,
				"table_name", m[1], "column_name", m[2])
			continue
		}
		if m := reARAddIndexDeep.FindStringSubmatch(line); m != nil {
			cols := normalizeIndexCols(m[2])
			emitMigrationOp("create_index", m[1]+"."+cols, file, ln, add,
				"table_name", m[1], "index_columns", cols)
			continue
		}
		if m := reARAddReferenceDeep.FindStringSubmatch(line); m != nil {
			emitReferenceColumn(m[1], m[2], m[3], "INFERRED_FROM_AR_MIGRATION_ADD_REFERENCE", file, ln, add)
			emitMigrationOp("add_reference", m[1]+"."+m[2], file, ln, add,
				"table_name", m[1], "reference_name", m[2])
			continue
		}
		if m := reARAddForeignKey.FindStringSubmatch(line); m != nil {
			from, to := m[1], m[2]
			if tm := reOptToTable.FindStringSubmatch(m[3]); tm != nil {
				to = tm[1]
			}
			fk := makeEntity("ar_fk:"+from+"->"+to, "SCOPE.Pattern", "foreign_key", file.Path, file.Language, ln)
			setProps(&fk,
				"framework", "activerecord",
				"provenance", "INFERRED_FROM_AR_MIGRATION_ADD_FOREIGN_KEY",
				"from_table", from,
				"to_table", to,
				"target_model", classify(to),
			)
			if fm := reOptForeignKey.FindStringSubmatch(m[3]); fm != nil {
				setProps(&fk, "foreign_key", firstNonEmptyGroup(fm))
			}
			add(fk)
			emitMigrationOp("add_foreign_key", from+"->"+to, file, ln, add,
				"from_table", from, "to_table", to)
			continue
		}
	}
}

func emitMigColumn(table, col, typ, opts string, file extractor.FileInput, ln int, add func(types.EntityRecord)) {
	ent := makeEntity("ar_migcol:"+table+"."+col, "SCOPE.Schema", "column", file.Path, file.Language, ln)
	setProps(&ent,
		"framework", "activerecord",
		"provenance", "INFERRED_FROM_AR_MIGRATION_COLUMN",
		"table_name", table,
		"column_name", col,
		"column_type", typ,
		"model_class", classify(table),
	)
	applyColumnOptions(&ent, opts)
	add(ent)
}

// emitMigrationOp emits a normalized SCOPE.Evolution schema-change entity.
func emitMigrationOp(op, target string, file extractor.FileInput, ln int, add func(types.EntityRecord), kv ...string) {
	name := op
	if target != "" {
		name = op + ":" + target
	}
	ent := makeEntity("ar_op:"+name, "SCOPE.Evolution", op, file.Path, file.Language, ln)
	setProps(&ent,
		"framework", "activerecord",
		"provenance", "INFERRED_FROM_AR_MIGRATION_OP",
		"ddl_operation", op,
	)
	setProps(&ent, kv...)
	add(ent)
}

func normalizeIndexCols(raw string) string {
	raw = strings.TrimSpace(raw)
	// Cut at a trailing option (`, unique: true`) or comment.
	raw = strings.SplitN(raw, "unique:", 2)[0]
	raw = strings.TrimRight(raw, ", \t")
	raw = strings.Trim(raw, "[]")
	var cols []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `:"'`)
		if p != "" {
			cols = append(cols, p)
		}
	}
	return strings.Join(cols, ",")
}
