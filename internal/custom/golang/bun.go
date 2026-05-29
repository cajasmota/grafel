package golang

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// bun (github.com/uptrace/bun) is a struct-tag-driven SQL-first ORM. Models
// embed bun.BaseModel with a `bun:"table:..."` tag; columns and relations are
// declared through `bun:"..."` struct tags; queries are built with a fluent
// builder (db.NewSelect().Model(&x).Where(...)). Migrations use the
// migrate.Migrations registry / .Up/.Down funcs.
//
// Mapping:
//   - struct with `bun.BaseModel` or `bun:"table:..."`   -> SCOPE.Schema (model)
//   - field with `bun:"col,..."` tag                     -> SCOPE.Component (field)
//   - field with `bun:"rel:..."` / `bun:"m2m:..."` tag   -> SCOPE.Component (relation)
//   - db.NewSelect()/NewInsert()/...                     -> SCOPE.Operation (query)
//   - migrations.Register / Migrations.Up                -> SCOPE.Operation (migration)
func init() {
	extractor.Register("custom_go_bun", &bunExtractor{})
}

type bunExtractor struct{}

func (e *bunExtractor) Language() string { return "custom_go_bun" }

var (
	// A struct body that contains either a bun.BaseModel embed or any bun tag.
	reBunStruct = regexp.MustCompile(
		"(?ms)type\\s+(\\w+)\\s+struct\\s*\\{(.*?)\\n\\}",
	)
	// bun.BaseModel embed, optionally carrying a `bun:"table:...,alias:..."` tag.
	reBunBaseModel = regexp.MustCompile(
		"bun\\.BaseModel(?:\\s+`[^`]*bun:\"([^\"]*)\"[^`]*`)?",
	)
	// table:<name> inside a bun tag.
	reBunTableTag = regexp.MustCompile(`(?:^|,)\s*table:([A-Za-z0-9_.]+)`)
	// A struct field line carrying a bun tag.
	//   Name string `bun:"name,notnull"`
	reBunFieldTag = regexp.MustCompile(
		"(?m)^\\s*(\\w+)\\s+([\\w\\.\\[\\]\\*]+)\\s+`[^`]*bun:\"([^\"]*)\"[^`]*`",
	)
	// rel:has-one / rel:has-many / rel:belongs-to inside a bun tag.
	reBunRelTag = regexp.MustCompile(`(?:^|,)\s*rel:([a-z-]+)`)
	// m2m:<join_table> inside a bun tag.
	reBunM2MTag = regexp.MustCompile(`(?:^|,)\s*m2m:([A-Za-z0-9_]+)`)
	// join:<fk>=<pk> inside a bun tag.
	reBunJoinTag = regexp.MustCompile(`(?:^|,)\s*join:([A-Za-z0-9_=.]+)`)
	// Query-builder entry points.
	reBunQueryBuilder = regexp.MustCompile(
		`(?m)\.(NewSelect|NewInsert|NewUpdate|NewDelete|NewCreateTable|NewDropTable|NewTruncateTable|NewAddColumn|NewDropColumn|NewCreateIndex|NewRaw)\(`,
	)
	// .Model(&x) / .Model((*X)(nil)) binds a query to a model.
	reBunModelBind = regexp.MustCompile(
		`\.Model\(\s*(?:&\s*(\w+)|\(\*(\w+)\)\(nil\))`,
	)
	// migrate.NewMigrations() registry construction.
	reBunMigrations = regexp.MustCompile(`migrate\.NewMigrations\(`)
	// migrations.MustRegister(...) / .Register(...) registration.
	reBunMigRegister = regexp.MustCompile(`(?m)\b(\w*[Mm]igrations?)\.(MustRegister|Register)\(`)
	// CreateTable/DropTable model-driven schema ops via the query builder
	// already covered above (NewCreateTable...); migration files also exist.
	reBunMigrationFile = regexp.MustCompile(
		`^(\d+)_([A-Za-z0-9_\-]+)\.(up|down)\.sql$`,
	)
)

func (e *bunExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.bun_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "bun"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
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

	// File-based SQL migrations (commonly used with bun's migrate package).
	if m := bunMigrationFileName(file.Path); m != nil {
		version, slug, direction := m[1], m[2], m[3]
		ent := makeEntity("migration:"+version+"_"+slug+"."+direction, "SCOPE.Schema", "migration", file.Path, file.Language, 1)
		setProps(&ent, "framework", "bun", "provenance", "INFERRED_FROM_BUN_MIGRATION_FILE",
			"migration_version", version, "migration_slug", slug, "migration_direction", direction)
		add(ent)
		span.SetAttributes(attribute.Int("entity_count", len(entities)))
		return entities, nil
	}

	if file.Language != "go" {
		return nil, nil
	}

	// 1. Models + 2. fields + 3. relationships from struct + bun tags.
	for _, sm := range reBunStruct.FindAllStringSubmatchIndex(src, -1) {
		structName := src[sm[2]:sm[3]]
		body := src[sm[4]:sm[5]]
		structLine := lineOf(src, sm[0])

		baseModel := reBunBaseModel.FindStringSubmatch(body)
		fields := reBunFieldTag.FindAllStringSubmatch(body, -1)
		if baseModel == nil && len(fields) == 0 {
			continue // not a bun model
		}

		ent := makeEntity(structName, "SCOPE.Schema", "", file.Path, file.Language, structLine)
		setProps(&ent, "framework", "bun", "provenance", "INFERRED_FROM_BUN_MODEL")
		if baseModel != nil {
			setProps(&ent, "provenance", "INFERRED_FROM_BUN_BASEMODEL")
			if len(baseModel) > 1 && baseModel[1] != "" {
				if tm := reBunTableTag.FindStringSubmatch(baseModel[1]); tm != nil {
					setProps(&ent, "table_name", tm[1])
				}
			}
		}
		add(ent)

		for _, fm := range fields {
			fieldName := fm[1]
			fieldType := fm[2]
			tag := fm[3]

			// Relationship tags first; a rel:/m2m: field is a relation, not a
			// scalar column.
			if relM := reBunRelTag.FindStringSubmatch(tag); relM != nil || reBunM2MTag.MatchString(tag) {
				rel := bunRelKind(tag)
				target := relationshipTarget(fieldType)
				relEnt := makeEntity("rel:"+structName+"."+fieldName, "SCOPE.Component", "relation", file.Path, file.Language, structLine)
				setProps(&relEnt, "framework", "bun", "provenance", "INFERRED_FROM_BUN_REL",
					"model_name", structName, "field_name", fieldName,
					"relationship", rel, "target_model", target)
				if jm := reBunJoinTag.FindStringSubmatch(tag); jm != nil {
					setProps(&relEnt, "join_on", jm[1])
				}
				if mm := reBunM2MTag.FindStringSubmatch(tag); mm != nil {
					setProps(&relEnt, "join_table", mm[1])
				}
				add(relEnt)
				continue
			}

			column := bunColumnName(tag, fieldName)
			fieldEnt := makeEntity("field:"+structName+"."+fieldName, "SCOPE.Component", "field", file.Path, file.Language, structLine)
			setProps(&fieldEnt, "framework", "bun", "provenance", "INFERRED_FROM_BUN_FIELD",
				"model_name", structName, "field_name", fieldName,
				"column_name", column, "go_type", fieldType)
			add(fieldEnt)
		}
	}

	// 4. Query builder entry points -> SCOPE.Operation (query).
	for _, m := range reBunQueryBuilder.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity("query:"+verb, "SCOPE.Operation", "query", file.Path, file.Language, line)
		setProps(&ent, "framework", "bun", "provenance", "INFERRED_FROM_BUN_BUILDER",
			"query_op", verb)
		add(ent)
	}

	// 4b. .Model(&X) binds a query to a model -> SCOPE.Operation, exact
	//     attribution.
	for _, m := range reBunModelBind.FindAllStringSubmatchIndex(src, -1) {
		modelName := submatch(src, m, 2)
		if modelName == "" {
			modelName = submatch(src, m, 4)
		}
		if modelName == "" {
			continue
		}
		line := lineOf(src, m[0])
		ent := makeEntity("model_query:"+modelName, "SCOPE.Operation", "query", file.Path, file.Language, line)
		setProps(&ent, "framework", "bun", "provenance", "INFERRED_FROM_BUN_MODEL_BIND",
			"model_name", modelName)
		add(ent)
	}

	// 5. Migrations: registry construction + registration + builder DDL.
	for _, m := range reBunMigrations.FindAllStringSubmatchIndex(src, -1) {
		line := lineOf(src, m[0])
		ent := makeEntity("migrations:registry", "SCOPE.Operation", "migration", file.Path, file.Language, line)
		setProps(&ent, "framework", "bun", "provenance", "INFERRED_FROM_BUN_MIGRATIONS",
			"migration_kind", "registry")
		add(ent)
	}
	for _, m := range reBunMigRegister.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		ent := makeEntity("migrate_register:"+method, "SCOPE.Operation", "migration", file.Path, file.Language, line)
		setProps(&ent, "framework", "bun", "provenance", "INFERRED_FROM_BUN_MIGRATE_REGISTER",
			"migration_kind", "register", "register_method", method)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// bunMigrationFileName returns [full, version, slug, direction] when the path's
// base name is a NNN_slug.up/down.sql migration, else nil.
func bunMigrationFileName(path string) []string {
	base := path
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	return reBunMigrationFile.FindStringSubmatch(base)
}

// bunColumnName resolves the column name from a bun tag. The first
// comma-separated token (when not an option keyword) is the explicit column
// name; otherwise the field name is used.
func bunColumnName(tag, fieldName string) string {
	parts := strings.Split(tag, ",")
	if len(parts) == 0 {
		return fieldName
	}
	first := strings.TrimSpace(parts[0])
	if first == "" || strings.Contains(first, ":") || first == "-" {
		return fieldName
	}
	return first
}

// bunRelKind classifies a bun relationship tag into a normalized relationship
// kind. m2m wins; otherwise rel:<kind> maps directly.
func bunRelKind(tag string) string {
	if reBunM2MTag.MatchString(tag) {
		return "many2many"
	}
	if m := reBunRelTag.FindStringSubmatch(tag); m != nil {
		switch m[1] {
		case "has-one":
			return "has_one"
		case "has-many":
			return "has_many"
		case "belongs-to":
			return "belongs_to"
		default:
			return m[1]
		}
	}
	return ""
}
