package javascript

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_sequelize", &sequelizeExtractor{})
}

type sequelizeExtractor struct{}

func (e *sequelizeExtractor) Language() string { return "custom_js_sequelize" }

var (
	// sequelize.define("ModelName", {...})
	reSequelizeDefine = regexp.MustCompile(
		`(?:\w+)\.define\s*\(\s*['"]([A-Za-z0-9_]+)['"]`,
	)
	// class User extends Model {}
	reSequelizeClassExtends = regexp.MustCompile(
		`class\s+([A-Z][A-Za-z0-9_]*)\s+extends\s+Model\b`,
	)
	// ModelName.init({...}, { sequelize, ... })
	reSequelizeModelInit = regexp.MustCompile(
		`([A-Z][A-Za-z0-9_]*)\s*\.\s*init\s*\(\s*\{`,
	)
	// Model.findAll / Model.findOne / etc.
	reSequelizeQuery = regexp.MustCompile(
		`([A-Z][A-Za-z0-9_]*)\s*\.\s*(findAll|findOne|findByPk|findOrCreate|findAndCountAll|create|bulkCreate|update|destroy|count|max|min|sum|upsert)\s*\(`,
	)
	// Model.hasMany / belongsTo / hasOne / belongsToMany
	reSequelizeAssociation = regexp.MustCompile(
		`([A-Z][A-Za-z0-9_]*)\s*\.\s*(hasMany|belongsTo|hasOne|belongsToMany)\s*\(\s*([A-Z][A-Za-z0-9_]*)`,
	)
	// new Sequelize(...) / Sequelize.authenticate
	reSequelizeInstance = regexp.MustCompile(
		`new\s+Sequelize\s*\(`,
	)
	// Hooks: Model.addHook / Model.beforeCreate / Model.afterCreate / etc.
	reSequelizeHook = regexp.MustCompile(
		`([A-Z][A-Za-z0-9_]*)\s*\.\s*(addHook|beforeCreate|afterCreate|beforeUpdate|afterUpdate|beforeDestroy|afterDestroy|beforeFind|afterFind|beforeBulkCreate|afterBulkCreate)\s*\(`,
	)
	// Column definition key inside define({}) or Model.init({}) schema object.
	// Matches `  fieldName: {` patterns that introduce a column definition block.
	// Group 1 = field name.
	reSequelizeColumnDef = regexp.MustCompile(
		`(?m)^\s{2,8}([a-z][A-Za-z0-9_]*)\s*:\s*\{`,
	)
	// DataTypes.XXX inside a column-definition block — confirms the object is
	// a Sequelize column spec (not just any nested object literal).
	reSequelizeDataType = regexp.MustCompile(
		`DataTypes\s*\.\s*([A-Z][A-Za-z0-9_]*)`,
	)
	// references: { model: 'X', key: 'y' } — FK column definition.
	// Group 1 = referenced model name.
	reSequelizeColumnRef = regexp.MustCompile(
		`references\s*:\s*\{\s*model\s*:\s*['"]([A-Za-z0-9_]+)['"]`,
	)
	// Migration schema-change ops via the queryInterface inside up()/down().
	// First captured group = method, second = the table name string literal.
	reSequelizeMigrationOp = regexp.MustCompile(
		`queryInterface\s*\.\s*(createTable|dropTable|renameTable|addColumn|removeColumn|changeColumn|renameColumn|addIndex|removeIndex|addConstraint|removeConstraint|bulkInsert|bulkDelete)\s*\(\s*['"]([A-Za-z0-9_.]+)['"]`,
	)
)

// sequelizeMigrationOpSubtype normalizes a queryInterface method to a shared
// schema-change op subtype.
func sequelizeMigrationOpSubtype(method string) string {
	switch method {
	case "createTable":
		return "create_table"
	case "dropTable":
		return "drop_table"
	case "renameTable":
		return "rename_table"
	case "addColumn":
		return "add_column"
	case "removeColumn":
		return "drop_column"
	case "changeColumn":
		return "alter_column"
	case "renameColumn":
		return "rename_column"
	case "addIndex":
		return "create_index"
	case "removeIndex":
		return "drop_index"
	case "addConstraint":
		return "add_constraint"
	case "removeConstraint":
		return "drop_constraint"
	case "bulkInsert":
		return "data_insert"
	case "bulkDelete":
		return "data_delete"
	default:
		return "schema_change"
	}
}

func (e *sequelizeExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.sequelize_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "sequelize"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	if lang != "typescript" && lang != "javascript" {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.Subtype)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Sequelize instance
	for _, m := range reSequelizeInstance.FindAllStringIndex(src, -1) {
		ent := makeEntity("Sequelize", "SCOPE.Service", "database", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "sequelize", "provenance", "INFERRED_FROM_SEQUELIZE_INSTANCE")
		addEntity(ent)
	}

	// sequelize.define models
	for _, m := range reSequelizeDefine.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "model", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "sequelize", "provenance", "INFERRED_FROM_SEQUELIZE_DEFINE")
		addEntity(ent)
	}

	// Class extends Model
	classNames := make(map[string]bool)
	for _, m := range reSequelizeClassExtends.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		classNames[name] = true
		ent := makeEntity(name, "SCOPE.Schema", "model", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "sequelize", "provenance", "INFERRED_FROM_SEQUELIZE_CLASS_MODEL")
		addEntity(ent)
	}

	// Model.init() calls (only for known class models)
	for _, m := range reSequelizeModelInit.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if !classNames[name] {
			continue
		}
		ent := makeEntity(name+".init", "SCOPE.Operation", "model_init", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "sequelize", "model_name", name,
			"provenance", "INFERRED_FROM_SEQUELIZE_MODEL_INIT")
		addEntity(ent)
	}

	// Query operations
	for _, m := range reSequelizeQuery.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[m[2]:m[3]]
		operation := src[m[4]:m[5]]
		name := fmt.Sprintf("%s.%s", modelName, operation)
		ent := makeEntity(name, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "sequelize", "model", modelName, "operation", operation,
			"provenance", "INFERRED_FROM_SEQUELIZE_QUERY")
		addEntity(ent)
	}

	// Column definitions: emit SCOPE.Component "column" entities for schema_extraction.
	// We require that the file contains at least one DataTypes reference to confirm
	// this is a schema file, then emit each column-def block entry as a field entity.
	if reSequelizeDataType.MatchString(src) {
		for _, m := range reSequelizeColumnDef.FindAllStringSubmatchIndex(src, -1) {
			fieldName := src[m[2]:m[3]]
			// Skip internal Sequelize option keys that are not column names.
			switch fieldName {
			case "type", "allowNull", "defaultValue", "unique", "primaryKey",
				"autoIncrement", "validate", "get", "set", "references",
				"onDelete", "onUpdate", "comment", "field", "model", "key":
				continue
			}
			ent := makeEntity(fieldName, "SCOPE.Component", "column", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "sequelize", "provenance", "INFERRED_FROM_SEQUELIZE_COLUMN_DEF")
			addEntity(ent)
		}
	}

	// Foreign-key columns: references: { model: 'X' } inside a column def.
	for _, m := range reSequelizeColumnRef.FindAllStringSubmatchIndex(src, -1) {
		refModel := src[m[2]:m[3]]
		ent := makeEntity("fk:"+refModel, "SCOPE.Component", "foreign_key", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "sequelize", "referenced_model", refModel,
			"provenance", "INFERRED_FROM_SEQUELIZE_COLUMN_REFERENCE")
		addEntity(ent)
	}

	// Associations
	for _, m := range reSequelizeAssociation.FindAllStringSubmatchIndex(src, -1) {
		sourceModel := src[m[2]:m[3]]
		assocType := src[m[4]:m[5]]
		targetModel := src[m[6]:m[7]]
		name := fmt.Sprintf("%s.%s(%s)", sourceModel, assocType, targetModel)
		ent := makeEntity(name, "SCOPE.Pattern", "association", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "sequelize", "source_model", sourceModel,
			"association_type", assocType, "target_model", targetModel,
			"provenance", "INFERRED_FROM_SEQUELIZE_ASSOCIATION")
		addEntity(ent)
	}

	// Lifecycle hooks
	for _, m := range reSequelizeHook.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[m[2]:m[3]]
		hookType := src[m[4]:m[5]]
		name := fmt.Sprintf("%s.%s", modelName, hookType)
		ent := makeEntity(name, "SCOPE.Pattern", "lifecycle_hook", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "sequelize", "model_name", modelName, "hook_type", hookType,
			"provenance", "INFERRED_FROM_SEQUELIZE_HOOK")
		addEntity(ent)
	}

	// Migration schema-change operations (queryInterface.*).
	for _, m := range reSequelizeMigrationOp.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		table := src[m[4]:m[5]]
		opSubtype := sequelizeMigrationOpSubtype(method)
		ent := makeEntity(opSubtype+":"+table, "SCOPE.Evolution", opSubtype, file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "sequelize", "migration_op", method, "table", table,
			"provenance", "INFERRED_FROM_SEQUELIZE_MIGRATION_OP")
		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
