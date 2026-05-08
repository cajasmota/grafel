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
)

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

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
