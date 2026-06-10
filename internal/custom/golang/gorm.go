package golang

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/lifecycle"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_go_gorm", &gormExtractor{})
}

type gormExtractor struct{}

func (e *gormExtractor) Language() string { return "custom_go_gorm" }

var (
	reGORMOpen = regexp.MustCompile(
		`(?m)(\w+)\s*,\s*\w+\s*:?=\s*gorm\.Open\s*\(`,
	)
	reGORMAutoMigrate = regexp.MustCompile(
		`(?m)\.AutoMigrate\s*\(\s*&?([^)]+)\)`,
	)
	reGORMAutoMigrateType = regexp.MustCompile(
		`&?(\w+)\s*\{`,
	)
	reGORMModel = regexp.MustCompile(
		`(?m)type\s+(\w+)\s+struct\s*\{[^}]*\bgorm\.Model\b`,
	)
	reGORMQuery = regexp.MustCompile(
		`(?m)\.(?:Model|Table)\s*\(\s*(?:&?(\w+)\s*\{|"([^"]+)"\s*)`,
	)
	reGORMCreate = regexp.MustCompile(
		`(?m)db\.Create\s*\(\s*&?(\w+)`,
	)
	reGORMFind = regexp.MustCompile(
		`(?m)db\.(?:Find|First|Last|Take)\s*\(\s*&?(\w+)`,
	)
	reGORMScope = regexp.MustCompile(
		`(?m)\.Scopes\s*\(\s*(\w+)`,
	)

	// --- Models: struct declarations + field-level `gorm:"..."` tags. ---
	// Captures a struct type whose body carries at least one gorm tag, so we
	// recognise plain GORM models that do NOT embed gorm.Model.
	reGORMStructWithTag = regexp.MustCompile(
		"(?ms)type\\s+(\\w+)\\s+struct\\s*\\{(.*?)\\n\\}",
	)
	// One struct field line carrying a gorm struct tag.
	//   Name string `gorm:"column:name;type:varchar(255);not null"`
	reGORMFieldTag = regexp.MustCompile(
		"(?m)^\\s*(\\w+)\\s+([\\w\\.\\[\\]\\*]+)\\s+`[^`]*gorm:\"([^\"]*)\"[^`]*`",
	)
	// Any struct field line `Name Type` (with or without a trailing tag),
	// used to recover untagged association fields (#4367) such as
	//   Customer Customer
	//   Items    []Item
	// that carry no gorm tag and are therefore invisible to reGORMFieldTag.
	// Group 1 = field name, group 2 = field type (slice/pointer/pkg-qualified).
	reGORMAssocField = regexp.MustCompile(
		"(?m)^\\s*([A-Z]\\w*)\\s+([\\w\\.\\[\\]\\*]+)\\s*(?:`[^`]*`)?\\s*$",
	)
	// column:<name> inside a gorm tag.
	reGORMColumnTag = regexp.MustCompile(`(?:^|;)\s*column:([A-Za-z0-9_]+)`)
	// type:<sqltype> inside a gorm tag.
	reGORMTypeTag = regexp.MustCompile(`(?:^|;)\s*type:([^;]+)`)

	// --- Relationships: association struct tags + inferred edges. ---
	// foreignKey:/references: tags signal an explicit association mapping.
	reGORMForeignKeyTag = regexp.MustCompile(`(?:^|;)\s*foreignKey:([A-Za-z0-9_]+)`)
	reGORMReferencesTag = regexp.MustCompile(`(?:^|;)\s*references:([A-Za-z0-9_]+)`)
	reGORMManyToMany    = regexp.MustCompile(`(?:^|;)\s*many2many:([A-Za-z0-9_]+)`)

	// --- Queries: broad chainer recognition (db.Where/Joins/Preload/...). ---
	reGORMChainer = regexp.MustCompile(
		`(?m)\.(Where|Joins|Preload|Select|Order|Group|Having|Limit|Offset|Or|Not|Distinct|Pluck|Count|Save|Update|Updates|Delete)\s*\(`,
	)

	// --- Migrations: bare migrator ops + file-based migration filenames. ---
	reGORMMigratorOp = regexp.MustCompile(
		`(?m)\.Migrator\(\)\.(CreateTable|DropTable|CreateIndex|DropIndex|AddColumn|DropColumn|RenameColumn|CreateConstraint)\s*\(`,
	)
	// File-based migrations such as 000123_add_users.up.sql.
	reGORMMigrationFile = regexp.MustCompile(
		`^(\d+)_([A-Za-z0-9_\-]+)\.(up|down)\.sql$`,
	)
)

func (e *gormExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.gorm_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "gorm"),
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

	// File-based SQL migrations are recognised by filename regardless of
	// language, so handle them before the Go-only gate below.
	base := filepath.Base(file.Path)
	if m := reGORMMigrationFile.FindStringSubmatch(base); m != nil {
		version, slug, direction := m[1], m[2], m[3]
		ent := makeEntity("migration:"+version+"_"+slug+"."+direction, "SCOPE.Schema", "migration", file.Path, file.Language, 1)
		setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_MIGRATION_FILE",
			"migration_version", version, "migration_slug", slug, "migration_direction", direction)
		add(ent)
		span.SetAttributes(attribute.Int("entity_count", len(entities)))
		return entities, nil
	}

	if file.Language != "go" {
		return nil, nil
	}

	// 1. gorm.Open() -> SCOPE.Service
	for _, m := range reGORMOpen.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		ent := makeEntity(varName, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_OPEN")
		add(ent)
	}

	// 2. AutoMigrate models -> SCOPE.Schema (model) + SCOPE.Operation (migration).
	for _, m := range reGORMAutoMigrate.FindAllStringSubmatchIndex(src, -1) {
		argsStr := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		for _, tm := range reGORMAutoMigrateType.FindAllStringSubmatch(argsStr, -1) {
			modelName := tm[1]
			ent := makeEntity(modelName, "SCOPE.Schema", "", file.Path, file.Language, line)
			setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_AUTOMIGRATE")
			add(ent)
			// The AutoMigrate call itself is a migration operation.
			mig := makeEntity("migrate:"+modelName, "SCOPE.Operation", "migration", file.Path, file.Language, line)
			setProps(&mig, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_AUTOMIGRATE",
				"migration_kind", "auto_migrate", "model_name", modelName)
			add(mig)
		}
	}

	// 3. struct with gorm.Model embed -> SCOPE.Schema.
	//    The model entity is deferred (built here, added at the end of the
	//    struct-field pass) so data-lifecycle traits derived from the struct
	//    body (#3628 child) can be stamped onto the same node before it is
	//    emitted. modelEnts maps struct name -> deferred entity; modelEmbeds
	//    records the gorm.Model embed so trait resolution sees it.
	gormModelStructs := make(map[string]bool)
	modelEnts := make(map[string]*types.EntityRecord)
	modelEmbeds := make(map[string]bool)
	// seenFields dedups field/relation emission across the tagged-field loop and
	// the untagged-association pass (#4367), keyed by "<Struct>.<Field>".
	seenFields := make(map[string]bool)
	var modelOrder []string
	for _, m := range reGORMModel.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[m[2]:m[3]]
		gormModelStructs[modelName] = true
		modelEmbeds[modelName] = true
		ent := makeEntity(modelName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_MODEL")
		modelEnts[modelName] = &ent
		modelOrder = append(modelOrder, modelName)
	}

	// 3b. Models / Relationships from struct field tags.
	//     A struct is treated as a GORM model when it either embeds
	//     gorm.Model (handled above) or carries at least one `gorm:"..."`
	//     field tag. Each tagged column becomes a SCOPE.Component (field),
	//     and association tags become SCOPE.Relationship edges.
	for _, sm := range reGORMStructWithTag.FindAllStringSubmatchIndex(src, -1) {
		structName := src[sm[2]:sm[3]]
		body := src[sm[4]:sm[5]]
		structLine := lineOf(src, sm[0])
		fields := reGORMFieldTag.FindAllStringSubmatch(body, -1)
		if len(fields) == 0 {
			continue
		}

		// Promote the struct to a schema even without gorm.Model when it
		// carries gorm field tags; defer the model node like the embed case.
		if _, ok := modelEnts[structName]; !ok {
			ent := makeEntity(structName, "SCOPE.Schema", "", file.Path, file.Language, structLine)
			setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_FIELD_TAGS")
			modelEnts[structName] = &ent
			modelOrder = append(modelOrder, structName)
		}

		// Data-lifecycle trait inputs gathered while iterating fields.
		li := lifecycle.GORMInput{EmbedsGormModel: modelEmbeds[structName]}

		for _, fm := range fields {
			fieldName := fm[1]
			fieldType := fm[2]
			tag := fm[3]

			column := fieldName
			if cm := reGORMColumnTag.FindStringSubmatch(tag); cm != nil {
				column = cm[1]
			}

			// Data-lifecycle trait inputs (#3628 child). A gorm.DeletedAt-typed
			// field marks soft-delete with this field's column; CreatedAt /
			// UpdatedAt presence marks timestamps; all resolved columns feed
			// audit-column detection.
			li.Columns = append(li.Columns, column)
			if strings.HasSuffix(fieldType, "gorm.DeletedAt") {
				li.DeletedAtColumn = column
			}
			switch fieldName {
			case "CreatedAt":
				li.HasCreatedAt = true
			case "UpdatedAt":
				li.HasUpdatedAt = true
			}

			fieldEnt := makeEntity("field:"+structName+"."+fieldName, "SCOPE.Component", "field", file.Path, file.Language, structLine)
			setProps(&fieldEnt, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_FIELD_TAGS",
				"model_name", structName, "field_name", fieldName, "column_name", column, "go_type", fieldType)
			if tm := reGORMTypeTag.FindStringSubmatch(tag); tm != nil {
				setProps(&fieldEnt, "sql_type", strings.TrimSpace(tm[1]))
			}
			// #4367 — CONTAINS membership: the column field belongs to its owning
			// model struct. Hung off the (deferred) model node so the field is a
			// member, not an orphan.
			if me := modelEnts[structName]; me != nil {
				me.Relationships = append(me.Relationships,
					containsFieldEdge(structName, fieldEnt.ID, fieldName, "gorm"))
			}
			add(fieldEnt)
			seenFields[structName+"."+fieldName] = true

			// Relationships: association tags.
			rel := relationshipKind(tag, fieldType, fieldName)
			if rel == "" {
				continue
			}
			target := relationshipTarget(fieldType)
			relEnt := makeEntity("rel:"+structName+"."+fieldName, "SCOPE.Component", "relation", file.Path, file.Language, structLine)
			setProps(&relEnt, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_ASSOCIATION",
				"model_name", structName, "field_name", fieldName, "relationship", rel, "target_model", target)
			if fk := reGORMForeignKeyTag.FindStringSubmatch(tag); fk != nil {
				setProps(&relEnt, "foreign_key", fk[1])
			}
			if ref := reGORMReferencesTag.FindStringSubmatch(tag); ref != nil {
				setProps(&relEnt, "references", ref[1])
			}
			if m2m := reGORMManyToMany.FindStringSubmatch(tag); m2m != nil {
				setProps(&relEnt, "join_table", m2m[1])
			}
			// #4367 — REFERENCES to the target model (the relation field's only
			// outbound semantic edge) + CONTAINS membership from the owner struct.
			if target != "" {
				relEnt.Relationships = append(relEnt.Relationships,
					referencesClassEdge(relEnt.ID, target, "gorm", fieldName))
			}
			if me := modelEnts[structName]; me != nil {
				me.Relationships = append(me.Relationships,
					containsFieldEdge(structName, relEnt.ID, fieldName, "gorm"))
			}
			add(relEnt)
			seenFields[structName+"."+fieldName] = true
		}

		// #4367 — untagged association fields. A bare `Customer Customer` or
		// `Items []Item` struct/slice field has NO gorm tag, so the tag-keyed
		// field loop above never saw it — the relation (and the related model
		// edge) were dropped entirely. Scan the struct body for capitalised
		// struct-ref fields that the loop did not already emit and record them
		// as association relations with a REFERENCES edge to the target model.
		for _, am := range reGORMAssocField.FindAllStringSubmatch(body, -1) {
			fieldName := am[1]
			fieldType := am[2]
			if seenFields[structName+"."+fieldName] {
				continue
			}
			if !isStructRefType(fieldType) {
				continue
			}
			seenFields[structName+"."+fieldName] = true
			rel := relationshipKind("", fieldType, fieldName)
			if rel == "" {
				continue
			}
			target := relationshipTarget(fieldType)
			relEnt := makeEntity("rel:"+structName+"."+fieldName, "SCOPE.Component", "relation", file.Path, file.Language, structLine)
			setProps(&relEnt, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_ASSOCIATION",
				"model_name", structName, "field_name", fieldName, "relationship", rel,
				"target_model", target, "untagged", "true")
			if target != "" {
				relEnt.Relationships = append(relEnt.Relationships,
					referencesClassEdge(relEnt.ID, target, "gorm", fieldName))
			}
			if me := modelEnts[structName]; me != nil {
				me.Relationships = append(me.Relationships,
					containsFieldEdge(structName, relEnt.ID, fieldName, "gorm"))
			}
			add(relEnt)
		}

		// Stamp resolved data-lifecycle traits onto the (deferred) model node.
		if me := modelEnts[structName]; me != nil {
			lifecycle.GORM(li).Stamp(func(kv ...string) { setProps(me, kv...) })
		}
	}

	// Emit deferred GORM model nodes (now carrying lifecycle traits) in
	// discovery order. Structs that embed gorm.Model but carry no gorm field
	// tags never entered the field loop, so resolve their traits from the
	// embed alone here.
	for _, name := range modelOrder {
		me := modelEnts[name]
		if me == nil {
			continue
		}
		if _, stamped := me.Properties["soft_delete"]; !stamped {
			if _, ts := me.Properties["timestamps"]; !ts {
				if modelEmbeds[name] {
					lifecycle.GORM(lifecycle.GORMInput{EmbedsGormModel: true}).
						Stamp(func(kv ...string) { setProps(me, kv...) })
				}
			}
		}
		add(*me)
	}

	// 4. db.Model(&T{}) / db.Table("name") queries -> SCOPE.Operation
	for _, m := range reGORMQuery.FindAllStringSubmatchIndex(src, -1) {
		var modelName string
		if m[2] >= 0 {
			modelName = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			modelName = src[m[4]:m[5]]
		}
		if modelName == "" {
			continue
		}
		ent := makeEntity("query:"+modelName, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_QUERY",
			"model_name", modelName)
		add(ent)
	}

	// 5. db.Create -> SCOPE.Operation
	for _, m := range reGORMCreate.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[m[2]:m[3]]
		ent := makeEntity("create:"+modelName, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_QUERY",
			"query_type", "create", "model_name", modelName)
		add(ent)
	}

	// 6. db.Find/First/Last/Take -> SCOPE.Operation
	for _, m := range reGORMFind.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[m[2]:m[3]]
		ent := makeEntity("find:"+modelName, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_QUERY",
			"query_type", "find", "model_name", modelName)
		add(ent)
	}

	// 6b. Query chainers (Where/Joins/Preload/Select/...) -> SCOPE.Operation.
	//     Heuristic: we capture the chainer verb but cannot reliably bind it
	//     to a concrete model from a regex alone, hence query_attribution
	//     stays partial for free-floating chains.
	for _, m := range reGORMChainer.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		ent := makeEntity("chain:"+verb, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_CHAIN",
			"query_type", "chain", "chainer", verb)
		add(ent)
	}

	// 7. .Scopes(fn) -> SCOPE.Pattern
	for _, m := range reGORMScope.FindAllStringSubmatchIndex(src, -1) {
		scopeFn := src[m[2]:m[3]]
		ent := makeEntity("scope:"+scopeFn, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_SCOPE",
			"scope_func", scopeFn)
		add(ent)
	}

	// 8. Programmatic migrator operations -> SCOPE.Operation (migration).
	for _, m := range reGORMMigratorOp.FindAllStringSubmatchIndex(src, -1) {
		op := src[m[2]:m[3]]
		ent := makeEntity("migrate_op:"+op, "SCOPE.Operation", "migration", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_MIGRATOR",
			"migration_kind", "migrator", "migrator_op", op)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// relationshipKind classifies a GORM association from its struct tag and the
// Go field type. Explicit many2many tags win; otherwise slice types imply
// has_many, pointer/struct types with a foreignKey on the owned side imply
// has_one, and a foreign-key id field plus a struct ref implies belongs_to.
func relationshipKind(tag, fieldType, fieldName string) string {
	if reGORMManyToMany.MatchString(tag) {
		return "many2many"
	}
	hasAssocTag := reGORMForeignKeyTag.MatchString(tag) || reGORMReferencesTag.MatchString(tag)
	isSlice := strings.HasPrefix(fieldType, "[]")
	isStructRef := isStructRefType(fieldType)

	switch {
	case isSlice && isStructRef:
		return "has_many"
	case isStructRef && reGORMReferencesTag.MatchString(tag):
		// references: on a singular struct ref => the other side is owned.
		return "has_one"
	case isStructRef && hasAssocTag:
		return "belongs_to"
	case isStructRef:
		// Bare singular struct ref with no tag: most commonly belongs_to.
		return "belongs_to"
	default:
		return ""
	}
}

// relationshipTarget returns the referenced model name from a Go field type,
// stripping slice/pointer decorations and package qualifiers.
func relationshipTarget(fieldType string) string {
	t := strings.TrimPrefix(fieldType, "[]")
	t = strings.TrimPrefix(t, "*")
	if i := strings.LastIndex(t, "."); i >= 0 {
		t = t[i+1:]
	}
	return t
}

// isStructRefType reports whether a field type references another model
// (a capitalised, non-builtin identifier), i.e. an association target rather
// than a scalar column.
func isStructRefType(fieldType string) bool {
	t := relationshipTarget(fieldType)
	if t == "" {
		return false
	}
	switch t {
	case "string", "bool", "byte", "rune", "error",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"float32", "float64", "complex64", "complex128",
		"Time", "DeletedAt", "NullString", "NullInt64", "Decimal":
		return false
	}
	c := t[0]
	return c >= 'A' && c <= 'Z'
}
