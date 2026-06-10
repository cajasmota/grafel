// Package ruby — ActiveRecord ORM extractor.
//
// Handles associations (has_many, belongs_to, has_one, has_and_belongs_to_many,
// has_many :through), schema.rb column declarations, and db/migrate/ migration
// DDL (create_table, add_column, add_index).
//
// Coverage cells flipped (all via `go run ./tools/coverage update`):
//
//	lang.ruby.orm.activerecord
//	  association_extraction    → partial  (heuristic regex, no type inference)
//	  relationship_extraction   → partial
//	  foreign_key_extraction    → partial
//	  schema_extraction         → partial
//	  migration_parsing         → partial
//
//	lang.ruby.orm.rom-rb        association/relationship/foreign_key → partial
//	lang.ruby.orm.sequel        association/relationship/foreign_key → partial
//	lang.ruby.orm.datamapper    association/relationship/foreign_key → partial
//
// Part of #3282.
package ruby

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_ruby_activerecord", &activeRecordExtractor{})
}

// activeRecordExtractor extracts:
//   - ActiveRecord associations       → SCOPE.Pattern / relation
//   - db/schema.rb column definitions → SCOPE.Schema  / column
//   - db/migrate/ DDL operations      → SCOPE.Schema  / migration
type activeRecordExtractor struct{}

func (e *activeRecordExtractor) Language() string { return "custom_ruby_activerecord" }

// ---------------------------------------------------------------------------
// Regular expressions
// ---------------------------------------------------------------------------

var (
	// Association macros (including has_many :through and with options).
	// Captures: (1) macro name, (2) association symbol name.
	reARAssociation = regexp.MustCompile(
		`(?m)^\s*(has_many|belongs_to|has_one|has_and_belongs_to_many)\s+:([a-z_]+)`,
	)

	// has_many :through — also captures the :through target.
	// e.g.  has_many :tags, through: :taggings
	reARHasManyThrough = regexp.MustCompile(
		`(?m)^\s*has_many\s+:([a-z_]+)[^#\n]*:through\s*=>\s*:([a-z_]+)|` +
			`(?m)^\s*has_many\s+:([a-z_]+)[^#\n]*through:\s*:([a-z_]+)`,
	)

	// belongs_to :owner, foreign_key: "owner_id"  OR  :foreign_key => "owner_id"
	reARForeignKey = regexp.MustCompile(
		`(?m)^\s*(?:belongs_to|has_many|has_one)\s+:([a-z_]+)[^#\n]*` +
			`(?:foreign_key:\s*["':][a-z_]+["']?|:foreign_key\s*=>\s*["':][a-z_]+["']?)`,
	)

	// schema.rb: create_table "table_name" do |t|
	reARSchemaTable = regexp.MustCompile(
		`(?m)^\s*create_table\s+["']([a-z_]+)["']`,
	)

	// schema.rb: t.string :col  /  t.integer "col"
	reARSchemaColumn = regexp.MustCompile(
		`(?m)^\s*t\.(string|integer|bigint|float|decimal|boolean|text|date|datetime|timestamp|time|binary|json|jsonb|uuid|hstore|inet|cidr|macaddr|citext|ltree)\s+["':][a-z_]+["']?`,
	)

	// schema.rb column — also capture the column name (group 2).
	reARSchemaColumnNamed = regexp.MustCompile(
		`(?m)^\s*t\.(string|integer|bigint|float|decimal|boolean|text|date|datetime|timestamp|time|binary|json|jsonb|uuid|hstore|inet|cidr|macaddr|citext|ltree)\s+["':]([a-z_]+)["']?`,
	)

	// Migrations: class MyMigration < ActiveRecord::Migration[X.Y]
	reARMigrationClass = regexp.MustCompile(
		`(?m)^\s*class\s+([A-Z][A-Za-z0-9_]*)\s*<\s*ActiveRecord::Migration`,
	)

	// create_table :table_name / "table_name"
	reARMigCreateTable = regexp.MustCompile(
		`(?m)^\s*create_table\s+["':]([a-z_]+)["']?`,
	)

	// add_column :table, :col, :type
	reARMigAddColumn = regexp.MustCompile(
		`(?m)^\s*add_column\s+["':]([a-z_]+)["']?,\s*["':]([a-z_]+)["']?`,
	)

	// add_index :table, :col_or_cols
	reARMigAddIndex = regexp.MustCompile(
		`(?m)^\s*add_index\s+["':]([a-z_]+)["']?,\s*["':\[]([a-z_,\s:"'\[\]]+)`,
	)

	// add_reference / add_belongs_to → foreign key
	reARMigAddRef = regexp.MustCompile(
		`(?m)^\s*(?:add_reference|add_belongs_to)\s+["':]([a-z_]+)["']?,\s*["':]([a-z_]+)["']?`,
	)

	// ROM::Relation subclass (rom-rb)
	reROMRelation = regexp.MustCompile(
		`(?m)^\s*class\s+([A-Z][A-Za-z0-9_:]*)\s*<\s*(?:ROM::)?Relation`,
	)

	// ROM associations (through rom-rb association DSL)
	reROMAssociation = regexp.MustCompile(
		`(?m)^\s*(has_many|belongs_to|has_one|many_to_many)\s+:([a-z_]+)`,
	)

	// Sequel::Model subclass
	reSequelModel = regexp.MustCompile(
		`(?m)^\s*class\s+([A-Z][A-Za-z0-9_:]*)\s*<\s*(?:Sequel::)?Model`,
	)

	// Sequel associations
	reSequelAssociation = regexp.MustCompile(
		`(?m)^\s*(many_to_one|one_to_many|many_to_many|one_to_one)\s+:([a-z_]+)`,
	)

	// DataMapper::Resource include
	reDataMapperResource = regexp.MustCompile(
		`(?m)include\s+DataMapper::Resource`,
	)

	// DataMapper property / association
	reDataMapperProperty = regexp.MustCompile(
		`(?m)^\s*property\s+:([a-z_]+),\s*([A-Za-z:]+)`,
	)

	reDataMapperAssociation = regexp.MustCompile(
		`(?m)^\s*(has\s+\d+|has\s+n|belongs_to)\s+:([a-z_]+)`,
	)

	// ---------------------------------------------------------------------------
	// Lazy loading recognition — AR
	// ---------------------------------------------------------------------------

	// AR eager loading markers: includes / preload / eager_load
	// e.g.  User.includes(:posts, :comments).where(active: true)
	//       Order.eager_load(:line_items).preload(:customer)
	reARIncludes = regexp.MustCompile(
		`(?m)\.(includes|preload|eager_load)\s*\(\s*:([a-z_]+)`,
	)

	// AR lazy-association access: user.posts  (not intercepted here; marker via
	// presence of association macros without includes — we flag the association
	// as lazy by detecting has_many / has_one without a default scope override).
	// We emit one lazy_marker entity per file that has associations but no
	// eager-load call, as a documentation entity.
	reARLazyMarker = regexp.MustCompile(
		`(?m)^\s*(has_many|has_one|belongs_to)\s+:([a-z_]+)`,
	)

	// ---------------------------------------------------------------------------
	// Sequel migrations
	// ---------------------------------------------------------------------------

	// Sequel.migration { change { ... } }
	reSequelMigration = regexp.MustCompile(
		`(?m)\bSequel\.migration\b`,
	)

	// DB.create_table :name do / DB.alter_table :name do
	reSequelDBOp = regexp.MustCompile(
		`(?m)\bDB\.(create_table|alter_table|drop_table)\s+:([a-z_]+)`,
	)

	// Sequel add_column :name, type
	reSequelAddColumn = regexp.MustCompile(
		`(?m)^\s*add_column\s+:([a-z_]+),\s*([A-Za-z:]+)`,
	)

	// ---------------------------------------------------------------------------
	// DataMapper migrations (dm-migrations gem)
	// ---------------------------------------------------------------------------

	// DataMapper::Migration.new / migration(1, :name) do
	reDataMapperMigration = regexp.MustCompile(
		`(?m)\bDataMapper::Migration\b|^\s*migration\s*\(\s*\d+`,
	)

	// modify_table / create_table in dm-migrations
	reDataMapperMigTable = regexp.MustCompile(
		`(?m)^\s*(create_table|modify_table|drop_table)\s+:([a-z_]+)`,
	)

	// add_column :table, :col, :type (dm-migrations)
	reDataMapperMigColumn = regexp.MustCompile(
		`(?m)^\s*add_column\s+:([a-z_]+),\s*:([a-z_]+),\s*:([a-z_]+)`,
	)

	// ---------------------------------------------------------------------------
	// ROM-rb migrations
	// ---------------------------------------------------------------------------

	// ROM::SQL::Migration / ROM.container(:sql) migration block
	reROMMigration = regexp.MustCompile(
		`(?m)\bROM::SQL::Migration\b|Sequel\.migration\b`,
	)

	// ---------------------------------------------------------------------------
	// Mongoid associations + lazy loading
	// ---------------------------------------------------------------------------

	// Mongoid has_many / belongs_to / has_one / has_and_belongs_to_many
	reMongoidAssociation = regexp.MustCompile(
		`(?m)^\s*(has_many|belongs_to|has_one|has_and_belongs_to_many|embeds_many|embeds_one|embedded_in)\s+:([a-z_]+)`,
	)

	// Mongoid eager loading: includes(:assoc) on a Mongoid criteria
	reMongoidIncludes = regexp.MustCompile(
		`(?m)\.(includes|eager_load)\s*\(\s*:([a-z_]+)`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *activeRecordExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/ruby")
	_, span := tracer.Start(ctx, "indexer.activerecord_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "ruby" {
		return nil, nil
	}

	src := string(file.Content)
	path := strings.ReplaceAll(file.Path, "\\", "/")

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	isSchemaFile := strings.HasSuffix(path, "db/schema.rb") || strings.HasSuffix(path, "db/structure.sql")
	isMigrationFile := strings.Contains(path, "db/migrate/")

	// -------------------------------------------------------------------------
	// 1. ActiveRecord associations (app/models and similar files).
	//    Covers has_many, belongs_to, has_one, has_and_belongs_to_many.
	// -------------------------------------------------------------------------
	if !isSchemaFile && !isMigrationFile {
		// #4367 — owning model class for this file (one model per file is the
		// Rails convention). When known, every association/FK relation entity
		// below carries a CONTAINS edge from the model (so it is a member, not
		// an orphan) and a REFERENCES edge to its target model (so it is not a
		// dead-end). Both stubs resolve via the `Class:<Name>` byName convention.
		ownerModel := ""
		if mm := reARModelClass.FindStringSubmatch(src); mm != nil {
			ownerModel = mm[1]
		}

		for _, m := range reARAssociation.FindAllStringSubmatchIndex(src, -1) {
			assocType := src[m[2]:m[3]]
			assocName := src[m[4]:m[5]]
			name := fmt.Sprintf("%s:%s", assocType, assocName)
			ent := makeEntity(name, "SCOPE.Pattern", "relation", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "activerecord",
				"provenance", "INFERRED_FROM_AR_ASSOCIATION",
				"association_type", assocType,
				"association_name", assocName,
			)
			if ownerModel != "" {
				target := targetModel(assocType, assocName, assocOptions{})
				setProps(&ent, "owner_model", ownerModel, "target_model", target)
				ent.Relationships = append(ent.Relationships,
					containsFieldEdge(ownerModel, ent.ID, assocName, "activerecord"),
					referencesClassEdge(ent.ID, target, "activerecord", assocName))
			}
			add(ent)
		}

		// has_many :through — emit an additional relation entity noting the join model.
		for _, m := range reARHasManyThrough.FindAllStringSubmatchIndex(src, -1) {
			// Two alternations, pick the non-empty capture group pair.
			var assocName, throughName string
			if m[2] != -1 {
				assocName = src[m[2]:m[3]]
				throughName = src[m[4]:m[5]]
			} else if m[6] != -1 {
				assocName = src[m[6]:m[7]]
				throughName = src[m[8]:m[9]]
			} else {
				continue
			}
			name := "has_many_through:" + assocName
			ent := makeEntity(name, "SCOPE.Pattern", "relation", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "activerecord",
				"provenance", "INFERRED_FROM_AR_HAS_MANY_THROUGH",
				"association_type", "has_many_through",
				"association_name", assocName,
				"through_model", throughName,
			)
			if ownerModel != "" {
				target := targetModel("has_many", assocName, assocOptions{})
				setProps(&ent, "owner_model", ownerModel, "target_model", target)
				ent.Relationships = append(ent.Relationships,
					containsFieldEdge(ownerModel, ent.ID, assocName, "activerecord"),
					referencesClassEdge(ent.ID, target, "activerecord", assocName))
			}
			add(ent)
		}

		// belongs_to / has_many / has_one with explicit foreign_key → FK entity.
		for _, m := range reARForeignKey.FindAllStringSubmatchIndex(src, -1) {
			assocName := src[m[2]:m[3]]
			name := "fk:" + assocName
			ent := makeEntity(name, "SCOPE.Pattern", "foreign_key", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "activerecord",
				"provenance", "INFERRED_FROM_AR_FOREIGN_KEY",
				"association_name", assocName,
			)
			if ownerModel != "" {
				ent.Relationships = append(ent.Relationships,
					containsFieldEdge(ownerModel, ent.ID, assocName, "activerecord"))
			}
			add(ent)
		}

		// Deep model + association + foreign-key extraction (TS/JS bar):
		// model→table link, association options (:through/:source/:class_name/
		// :foreign_key/polymorphic/as) with target-model inference, and FK
		// entities (belongs_to x_id convention + explicit foreign_key).
		extractARModelsAndAssociations(src, file, add)
	}

	// -------------------------------------------------------------------------
	// 2. db/schema.rb — table definitions and column declarations.
	// -------------------------------------------------------------------------
	if isSchemaFile {
		currentTable := ""

		// We walk through the schema line by line so columns can be scoped to
		// the current create_table block.
		lines := strings.Split(src, "\n")
		for i, line := range lines {
			lineNum := i + 1

			// create_table → SCOPE.Schema table entity.
			if tm := reARSchemaTable.FindStringSubmatch(line); tm != nil {
				currentTable = tm[1]
				ent := makeEntity("table:"+currentTable, "SCOPE.Schema", "table", file.Path, file.Language, lineNum)
				setProps(&ent,
					"framework", "activerecord",
					"provenance", "INFERRED_FROM_SCHEMA_RB",
					"table_name", currentTable,
				)
				add(ent)
				continue
			}

			// t.<type> :col_name → SCOPE.Schema column entity.
			if cm := reARSchemaColumnNamed.FindStringSubmatch(line); cm != nil && currentTable != "" {
				colType := cm[1]
				colName := cm[2]
				name := currentTable + "." + colName
				ent := makeEntity(name, "SCOPE.Schema", "column", file.Path, file.Language, lineNum)
				setProps(&ent,
					"framework", "activerecord",
					"provenance", "INFERRED_FROM_SCHEMA_RB",
					"table_name", currentTable,
					"column_name", colName,
					"column_type", colType,
				)
				add(ent)
			}
		}

		// Deep schema extraction (TS/JS bar): typed columns with options
		// (null/default/limit), t.references / t.belongs_to → FK column + key,
		// t.timestamps, and table→model linking by Rails convention.
		extractARSchemaDeep(src, file, add)
	}

	// -------------------------------------------------------------------------
	// 3. db/migrate/ — migration class + DDL operations.
	// -------------------------------------------------------------------------
	if isMigrationFile {
		// Migration class declaration → SCOPE.Schema / migration.
		for _, m := range reARMigrationClass.FindAllStringSubmatchIndex(src, -1) {
			className := src[m[2]:m[3]]
			ent := makeEntity("migration:"+className, "SCOPE.Schema", "migration", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "activerecord",
				"provenance", "INFERRED_FROM_AR_MIGRATION_CLASS",
				"migration_class", className,
			)
			add(ent)
		}

		// create_table → SCOPE.Schema table entity.
		for _, m := range reARMigCreateTable.FindAllStringSubmatchIndex(src, -1) {
			tableName := src[m[2]:m[3]]
			ent := makeEntity("create_table:"+tableName, "SCOPE.Schema", "migration", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "activerecord",
				"provenance", "INFERRED_FROM_AR_MIGRATION_CREATE_TABLE",
				"table_name", tableName,
				"ddl_operation", "create_table",
			)
			add(ent)
		}

		// add_column → SCOPE.Schema column entity.
		for _, m := range reARMigAddColumn.FindAllStringSubmatchIndex(src, -1) {
			tableName := src[m[2]:m[3]]
			colName := src[m[4]:m[5]]
			name := "add_column:" + tableName + "." + colName
			ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "activerecord",
				"provenance", "INFERRED_FROM_AR_MIGRATION_ADD_COLUMN",
				"table_name", tableName,
				"column_name", colName,
				"ddl_operation", "add_column",
			)
			add(ent)
		}

		// add_index.
		for _, m := range reARMigAddIndex.FindAllStringSubmatchIndex(src, -1) {
			tableName := src[m[2]:m[3]]
			cols := strings.TrimSpace(src[m[4]:m[5]])
			name := "add_index:" + tableName + "." + cols
			ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "activerecord",
				"provenance", "INFERRED_FROM_AR_MIGRATION_ADD_INDEX",
				"table_name", tableName,
				"index_columns", cols,
				"ddl_operation", "add_index",
			)
			add(ent)
		}

		// add_reference / add_belongs_to → foreign key.
		for _, m := range reARMigAddRef.FindAllStringSubmatchIndex(src, -1) {
			tableName := src[m[2]:m[3]]
			refName := src[m[4]:m[5]]
			name := "add_reference:" + tableName + "." + refName
			ent := makeEntity(name, "SCOPE.Pattern", "foreign_key", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "activerecord",
				"provenance", "INFERRED_FROM_AR_MIGRATION_ADD_REFERENCE",
				"table_name", tableName,
				"reference_name", refName,
				"ddl_operation", "add_reference",
			)
			add(ent)
		}

		// Deep migration parsing (TS/JS bar): normalized SCOPE.Evolution ops
		// (create_table/add_column/drop_column/alter_column/create_index/
		// add_reference/add_foreign_key/drop_table) + typed columns inside
		// create_table blocks + FK entities from add_foreign_key / references.
		extractARMigrationDeep(src, file, add)
	}

	// -------------------------------------------------------------------------
	// 3b. ActiveRecord lazy loading recognition.
	//     Detect eager-load calls (includes/preload/eager_load) as explicit
	//     eager markers. The presence of association macros without eager load
	//     implies lazy loading (AR default).
	// -------------------------------------------------------------------------
	if !isSchemaFile && !isMigrationFile {
		// Eager loading markers.
		for _, m := range reARIncludes.FindAllStringSubmatchIndex(src, -1) {
			eagerType := src[m[2]:m[3]]
			assocName := src[m[4]:m[5]]
			name := "ar_eager:" + eagerType + ":" + assocName
			ent := makeEntity(name, "SCOPE.Pattern", "lazy_marker", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "activerecord",
				"provenance", "INFERRED_FROM_AR_EAGER_LOAD",
				"eager_type", eagerType,
				"association_name", assocName,
				"loading_strategy", "eager",
			)
			add(ent)
		}

		// Lazy loading: any has_many / has_one / belongs_to without eager override
		// implies AR lazy default. Emit one marker per association.
		for _, m := range reARLazyMarker.FindAllStringSubmatchIndex(src, -1) {
			assocType := src[m[2]:m[3]]
			assocName := src[m[4]:m[5]]
			name := "ar_lazy:" + assocType + ":" + assocName
			ent := makeEntity(name, "SCOPE.Pattern", "lazy_marker", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "activerecord",
				"provenance", "INFERRED_FROM_AR_LAZY_ASSOCIATION",
				"association_type", assocType,
				"association_name", assocName,
				"loading_strategy", "lazy",
			)
			add(ent)
		}
	}

	// -------------------------------------------------------------------------
	// 4. ROM (rom-rb) — Relation subclasses and associations.
	//    Heuristic: file references ROM or rom- in content.
	// -------------------------------------------------------------------------
	if strings.Contains(src, "ROM::") || strings.Contains(src, "ROM.container") {
		for _, m := range reROMRelation.FindAllStringSubmatchIndex(src, -1) {
			className := src[m[2]:m[3]]
			ent := makeEntity("rom:"+className, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "rom-rb",
				"provenance", "INFERRED_FROM_ROM_RELATION",
				"relation_class", className,
			)
			add(ent)
		}

		for _, m := range reROMAssociation.FindAllStringSubmatchIndex(src, -1) {
			assocType := src[m[2]:m[3]]
			assocName := src[m[4]:m[5]]
			name := "rom_assoc:" + assocType + ":" + assocName
			ent := makeEntity(name, "SCOPE.Pattern", "relation", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "rom-rb",
				"provenance", "INFERRED_FROM_ROM_ASSOCIATION",
				"association_type", assocType,
				"association_name", assocName,
			)
			add(ent)
		}
	}

	// -------------------------------------------------------------------------
	// 5. Sequel — Model subclasses and associations.
	// -------------------------------------------------------------------------
	if strings.Contains(src, "Sequel::Model") || strings.Contains(src, "sequel") {
		for _, m := range reSequelModel.FindAllStringSubmatchIndex(src, -1) {
			className := src[m[2]:m[3]]
			ent := makeEntity("sequel:"+className, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "sequel",
				"provenance", "INFERRED_FROM_SEQUEL_MODEL",
				"model_class", className,
			)
			add(ent)
		}

		for _, m := range reSequelAssociation.FindAllStringSubmatchIndex(src, -1) {
			assocType := src[m[2]:m[3]]
			assocName := src[m[4]:m[5]]
			name := "sequel_assoc:" + assocType + ":" + assocName
			ent := makeEntity(name, "SCOPE.Pattern", "relation", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "sequel",
				"provenance", "INFERRED_FROM_SEQUEL_ASSOCIATION",
				"association_type", assocType,
				"association_name", assocName,
			)
			add(ent)
		}
	}

	// -------------------------------------------------------------------------
	// 6. DataMapper — Resource include and associations.
	// -------------------------------------------------------------------------
	if reDataMapperResource.MatchString(src) {
		for _, m := range reDataMapperProperty.FindAllStringSubmatchIndex(src, -1) {
			propName := src[m[2]:m[3]]
			propType := src[m[4]:m[5]]
			name := "dm_prop:" + propName
			ent := makeEntity(name, "SCOPE.Schema", "column", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "datamapper",
				"provenance", "INFERRED_FROM_DM_PROPERTY",
				"property_name", propName,
				"property_type", propType,
			)
			add(ent)
		}

		for _, m := range reDataMapperAssociation.FindAllStringSubmatchIndex(src, -1) {
			assocType := strings.TrimSpace(src[m[2]:m[3]])
			assocName := src[m[4]:m[5]]
			name := "dm_assoc:" + assocType + ":" + assocName
			ent := makeEntity(name, "SCOPE.Pattern", "relation", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "datamapper",
				"provenance", "INFERRED_FROM_DM_ASSOCIATION",
				"association_type", assocType,
				"association_name", assocName,
			)
			add(ent)
		}
	}

	// -------------------------------------------------------------------------
	// 7. Mongoid — associations (has_many, belongs_to, embeds_*) + lazy loading.
	//    Heuristic: file contains Mongoid::Document or field :...
	//    Note: routes.go already emits Mongoid field schema entities; this
	//    section adds the association + lazy-loading coverage cells.
	// -------------------------------------------------------------------------
	if strings.Contains(src, "Mongoid::Document") || strings.Contains(src, "Mongoid::") {
		for _, m := range reMongoidAssociation.FindAllStringSubmatchIndex(src, -1) {
			assocType := src[m[2]:m[3]]
			assocName := src[m[4]:m[5]]
			name := fmt.Sprintf("mongoid_assoc:%s:%s", assocType, assocName)
			ent := makeEntity(name, "SCOPE.Pattern", "relation", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "mongoid",
				"provenance", "INFERRED_FROM_MONGOID_ASSOCIATION",
				"association_type", assocType,
				"association_name", assocName,
			)
			add(ent)
		}

		// Eager load markers.
		for _, m := range reMongoidIncludes.FindAllStringSubmatchIndex(src, -1) {
			eagerType := src[m[2]:m[3]]
			assocName := src[m[4]:m[5]]
			name := "mongoid_eager:" + eagerType + ":" + assocName
			ent := makeEntity(name, "SCOPE.Pattern", "lazy_marker", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "mongoid",
				"provenance", "INFERRED_FROM_MONGOID_EAGER_LOAD",
				"eager_type", eagerType,
				"association_name", assocName,
				"loading_strategy", "eager",
			)
			add(ent)
		}

		// Lazy loading: Mongoid associations are lazy by default. Emit a marker
		// per association (mirrors AR section 3b).
		for _, m := range reMongoidAssociation.FindAllStringSubmatchIndex(src, -1) {
			assocType := src[m[2]:m[3]]
			assocName := src[m[4]:m[5]]
			name := "mongoid_lazy:" + assocType + ":" + assocName
			ent := makeEntity(name, "SCOPE.Pattern", "lazy_marker", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "mongoid",
				"provenance", "INFERRED_FROM_MONGOID_LAZY_ASSOCIATION",
				"association_type", assocType,
				"association_name", assocName,
				"loading_strategy", "lazy",
			)
			add(ent)
		}
	}

	// -------------------------------------------------------------------------
	// 8. Sequel migrations.
	//    Detect Sequel.migration blocks and DB DDL operations.
	// -------------------------------------------------------------------------
	isSequelMigFile := strings.Contains(path, "db/migrate/") ||
		strings.Contains(path, "migrations/") ||
		strings.HasSuffix(path, "_migration.rb")

	if reSequelMigration.MatchString(src) || (isSequelMigFile && strings.Contains(src, "Sequel")) {
		// Sequel.migration marker.
		if loc := reSequelMigration.FindStringIndex(src); loc != nil {
			ent := makeEntity("sequel_migration", "SCOPE.Schema", "migration", file.Path, file.Language, lineOf(src, loc[0]))
			setProps(&ent,
				"framework", "sequel",
				"provenance", "INFERRED_FROM_SEQUEL_MIGRATION",
			)
			add(ent)
		}

		// DB.create_table / alter_table / drop_table
		for _, m := range reSequelDBOp.FindAllStringSubmatchIndex(src, -1) {
			op := src[m[2]:m[3]]
			tableName := src[m[4]:m[5]]
			name := fmt.Sprintf("sequel_mig_%s:%s", op, tableName)
			ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "sequel",
				"provenance", "INFERRED_FROM_SEQUEL_DB_OP",
				"ddl_operation", op,
				"table_name", tableName,
			)
			add(ent)
		}

		// add_column inside Sequel migration.
		for _, m := range reSequelAddColumn.FindAllStringSubmatchIndex(src, -1) {
			colName := src[m[2]:m[3]]
			colType := src[m[4]:m[5]]
			name := "sequel_mig_add_col:" + colName
			ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "sequel",
				"provenance", "INFERRED_FROM_SEQUEL_MIGRATION_ADD_COLUMN",
				"column_name", colName,
				"column_type", colType,
				"ddl_operation", "add_column",
			)
			add(ent)
		}
	}

	// Sequel lazy loading: any many_to_one / one_to_many / many_to_many
	// associations are lazy by default (Sequel::Model behaviour).
	if strings.Contains(src, "Sequel::Model") || strings.Contains(src, "sequel") {
		for _, m := range reSequelAssociation.FindAllStringSubmatchIndex(src, -1) {
			assocType := src[m[2]:m[3]]
			assocName := src[m[4]:m[5]]
			name := "sequel_lazy:" + assocType + ":" + assocName
			ent := makeEntity(name, "SCOPE.Pattern", "lazy_marker", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "sequel",
				"provenance", "INFERRED_FROM_SEQUEL_LAZY_ASSOCIATION",
				"association_type", assocType,
				"association_name", assocName,
				"loading_strategy", "lazy",
			)
			add(ent)
		}
	}

	// -------------------------------------------------------------------------
	// 9. DataMapper migrations (dm-migrations).
	// -------------------------------------------------------------------------
	if reDataMapperMigration.MatchString(src) {
		if loc := reDataMapperMigration.FindStringIndex(src); loc != nil {
			ent := makeEntity("dm_migration", "SCOPE.Schema", "migration", file.Path, file.Language, lineOf(src, loc[0]))
			setProps(&ent,
				"framework", "datamapper",
				"provenance", "INFERRED_FROM_DM_MIGRATION",
			)
			add(ent)
		}

		for _, m := range reDataMapperMigTable.FindAllStringSubmatchIndex(src, -1) {
			op := src[m[2]:m[3]]
			tableName := src[m[4]:m[5]]
			name := fmt.Sprintf("dm_mig_%s:%s", op, tableName)
			ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "datamapper",
				"provenance", "INFERRED_FROM_DM_MIGRATION_TABLE",
				"ddl_operation", op,
				"table_name", tableName,
			)
			add(ent)
		}

		for _, m := range reDataMapperMigColumn.FindAllStringSubmatchIndex(src, -1) {
			tableName := src[m[2]:m[3]]
			colName := src[m[4]:m[5]]
			colType := src[m[6]:m[7]]
			name := fmt.Sprintf("dm_mig_add_col:%s.%s", tableName, colName)
			ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "datamapper",
				"provenance", "INFERRED_FROM_DM_MIGRATION_COLUMN",
				"table_name", tableName,
				"column_name", colName,
				"column_type", colType,
				"ddl_operation", "add_column",
			)
			add(ent)
		}
	}

	// DataMapper lazy loading: all DataMapper associations are lazy by default.
	if reDataMapperResource.MatchString(src) {
		for _, m := range reDataMapperAssociation.FindAllStringSubmatchIndex(src, -1) {
			assocType := strings.TrimSpace(src[m[2]:m[3]])
			assocName := src[m[4]:m[5]]
			name := "dm_lazy:" + assocType + ":" + assocName
			ent := makeEntity(name, "SCOPE.Pattern", "lazy_marker", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "datamapper",
				"provenance", "INFERRED_FROM_DM_LAZY_ASSOCIATION",
				"association_type", assocType,
				"association_name", assocName,
				"loading_strategy", "lazy",
			)
			add(ent)
		}
	}

	// -------------------------------------------------------------------------
	// 10. ROM-rb migrations (use Sequel.migration underneath ROM).
	// -------------------------------------------------------------------------
	if (strings.Contains(src, "ROM::") || strings.Contains(src, "ROM.container")) &&
		reROMMigration.MatchString(src) {
		if loc := reROMMigration.FindStringIndex(src); loc != nil {
			ent := makeEntity("rom_migration", "SCOPE.Schema", "migration", file.Path, file.Language, lineOf(src, loc[0]))
			setProps(&ent,
				"framework", "rom-rb",
				"provenance", "INFERRED_FROM_ROM_MIGRATION",
			)
			add(ent)
		}
	}

	// ROM-rb lazy loading: associations are lazy by default.
	if strings.Contains(src, "ROM::") || strings.Contains(src, "ROM.container") {
		for _, m := range reROMAssociation.FindAllStringSubmatchIndex(src, -1) {
			assocType := src[m[2]:m[3]]
			assocName := src[m[4]:m[5]]
			name := "rom_lazy:" + assocType + ":" + assocName
			ent := makeEntity(name, "SCOPE.Pattern", "lazy_marker", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "rom-rb",
				"provenance", "INFERRED_FROM_ROM_LAZY_ASSOCIATION",
				"association_type", assocType,
				"association_name", assocName,
				"loading_strategy", "lazy",
			)
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
