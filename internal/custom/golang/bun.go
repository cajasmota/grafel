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

// bun (github.com/uptrace/bun) is a lightweight SQL-first ORM. Its mapping is
// driven entirely by struct field tags:
//   - `bun:"table:users,alias:u"`  on an embedded bun.BaseModel declares the
//     table (Model).
//   - `bun:"id,pk,autoincrement"`   on a scalar field declares a column.
//   - `bun:"rel:has-one|belongs-to|has-many|m2m,join:..."` declares a
//     relationship.
//
// Queries use a fluent builder (db.NewSelect()/NewInsert()/NewUpdate()/...),
// and migrations use the bun migrate API (migrate.NewMigrations(),
// migrations.MustRegister / Migrator.Migrate / .Up / .Down).
//
// This extractor recognises that surface:
//   - Models:        struct + bun:"table:..."         -> SCOPE.Schema
//   - Columns:       field + bun:"col,..."            -> SCOPE.Component (field)
//   - Relationships: field + bun:"rel:...,join:..."   -> SCOPE.Component (relation)
//   - Queries:       db.New<Verb>()                   -> SCOPE.Operation (query)
//   - Migrations:    migrate.* / Migrator.* / Up/Down -> SCOPE.Operation (migration)
func init() {
	extractor.Register("custom_go_bun", &bunExtractor{})
}

type bunExtractor struct{}

func (e *bunExtractor) Language() string { return "custom_go_bun" }

var (
	// A struct carrying bun mapping. We capture every struct body and then
	// inspect its fields for bun tags (table marker and/or column/rel tags).
	reBunStruct = regexp.MustCompile(
		`(?ms)type\s+(\w+)\s+struct\s*\{(.*?)\n\}`,
	)
	// A single struct field line carrying a `bun:"..."` tag. Group 1 = field
	// name (may be empty for an embedded type line like `bun.BaseModel`),
	// group 2 = field type, group 3 = tag body.
	reBunFieldTag = regexp.MustCompile(
		"(?m)^\\s*(\\w+)?\\s*([\\w\\.\\[\\]\\*]+)?\\s*`[^`]*bun:\"([^\"]*)\"[^`]*`",
	)
	// table:<name> inside a bun tag (declared on an embedded bun.BaseModel).
	reBunTable = regexp.MustCompile(`(?:^|,)\s*table:([A-Za-z0-9_.]+)`)
	// rel:<kind> inside a bun tag (has-one | belongs-to | has-many | m2m).
	reBunRel = regexp.MustCompile(`(?:^|,)\s*rel:([a-z0-9-]+)`)
	// join:<a>=<b> inside a bun tag — the FK / join-key mapping.
	reBunJoin = regexp.MustCompile(`(?:^|,)\s*join:([A-Za-z0-9_=.]+)`)
	// m2m:<join_table> inside a bun tag.
	reBunM2MTable = regexp.MustCompile(`(?:^|,)\s*m2m:([A-Za-z0-9_]+)`)

	// Fluent query builder: db.NewSelect()/NewInsert()/NewUpdate()/NewDelete()
	// /NewCreateTable()/NewDropTable()/NewRaw()/NewValues()/NewMerge().
	reBunQuery = regexp.MustCompile(
		`(?m)\.New(Select|Insert|Update|Delete|Raw|Values|Merge|AddColumn|DropColumn|CreateIndex|DropIndex)\s*\(`,
	)
	// .Model(&T{}) / .Model((*T)(nil)) binds a query to a concrete model.
	reBunQueryModel = regexp.MustCompile(
		`\.Model\s*\(\s*(?:&(\w+)\s*\{|\(\*(\w+)\)\s*\(\s*nil\s*\))`,
	)

	// DDL builders that double as schema migrations.
	reBunDDL = regexp.MustCompile(
		`(?m)\.New(CreateTable|DropTable|CreateIndex|DropIndex|AddColumn|DropColumn|Truncate)\s*\(`,
	)
	// bun migrate API: migrate.NewMigrations(), migrations.MustRegister(...),
	// Migrator.Migrate / .Init / .Rollback / .Up / .Down.
	reBunMigrateAPI = regexp.MustCompile(
		`(?m)\b(?:migrate\.NewMigrations|\w*[Mm]igrations?\.MustRegister|\w*[Mm]igrator\.(?:Migrate|Init|Rollback|Up|Down))\s*\(`,
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

	if len(file.Content) == 0 || file.Language != "go" {
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

	// 1. Models / Columns / Relationships from struct field tags.
	for _, sm := range reBunStruct.FindAllStringSubmatchIndex(src, -1) {
		structName := src[sm[2]:sm[3]]
		body := src[sm[4]:sm[5]]
		structLine := lineOf(src, sm[0])
		fields := reBunFieldTag.FindAllStringSubmatch(body, -1)
		if len(fields) == 0 {
			continue
		}

		// Locate the table name, declared on the embedded bun.BaseModel line.
		tableName := ""
		for _, fm := range fields {
			if tm := reBunTable.FindStringSubmatch(fm[3]); tm != nil {
				tableName = tm[1]
			}
		}

		schemaEnt := makeEntity(structName, "SCOPE.Schema", "", file.Path, file.Language, structLine)
		setProps(&schemaEnt, "framework", "bun", "provenance", "INFERRED_FROM_BUN_MODEL")
		if tableName != "" {
			setProps(&schemaEnt, "table_name", tableName)
		}
		add(schemaEnt)

		for _, fm := range fields {
			fieldName := fm[1]
			fieldType := fm[2]
			tag := fm[3]

			// The bun.BaseModel embed line and pure table markers are not
			// columns. Skip lines whose tag carries table: and no column name.
			if reBunTable.MatchString(tag) {
				continue
			}

			rel := reBunRel.FindStringSubmatch(tag)
			m2m := reBunM2MTable.FindStringSubmatch(tag)
			if rel != nil || m2m != nil {
				// Relationship field. bun expresses m2m either as
				// rel:m2m or as a bare m2m:<join_table> tag (no rel:).
				relKind := "many2many"
				if rel != nil {
					relKind = bunRelKind(rel[1])
				}
				target := relationshipTargetLocal(fieldType)
				relEnt := makeEntity("rel:"+structName+"."+fieldName, "SCOPE.Component", "relation", file.Path, file.Language, structLine)
				setProps(&relEnt, "framework", "bun", "provenance", "INFERRED_FROM_BUN_RELATION",
					"model_name", structName, "field_name", fieldName,
					"relationship", relKind, "target_model", target)
				if jm := reBunJoin.FindStringSubmatch(tag); jm != nil {
					setProps(&relEnt, "join", jm[1])
				}
				if m2m != nil {
					setProps(&relEnt, "join_table", m2m[1])
				}
				add(relEnt)
				continue
			}

			if fieldName == "" {
				continue
			}
			// Scalar column. The first comma-token of the tag is the column
			// name (unless it is a flag like ",pk"); fall back to field name.
			column := bunColumnName(tag, fieldName)
			fieldEnt := makeEntity("field:"+structName+"."+fieldName, "SCOPE.Component", "field", file.Path, file.Language, structLine)
			setProps(&fieldEnt, "framework", "bun", "provenance", "INFERRED_FROM_BUN_FIELD_TAG",
				"model_name", structName, "field_name", fieldName,
				"column_name", column, "go_type", fieldType)
			if bunHasFlag(tag, "pk") {
				setProps(&fieldEnt, "primary_key", "true")
			}
			add(fieldEnt)
		}
	}

	// 2. Queries: db.New<Verb>() builders, bound to a model where possible.
	for _, m := range reBunQuery.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		model := bunQueryModel(src, m[1])
		name := "query:" + verb
		if model != "" {
			name = "query:" + verb + ":" + model
		}
		q := makeEntity(name, "SCOPE.Operation", "query", file.Path, file.Language, line)
		setProps(&q, "framework", "bun", "provenance", "INFERRED_FROM_BUN_QUERY",
			"query_verb", verb)
		if model != "" {
			setProps(&q, "model_name", model)
		}
		add(q)
	}

	// 3. Migrations: DDL builders + bun migrate API.
	for _, m := range reBunDDL.FindAllStringSubmatchIndex(src, -1) {
		op := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		mig := makeEntity("migrate_ddl:"+op, "SCOPE.Operation", "migration", file.Path, file.Language, line)
		setProps(&mig, "framework", "bun", "provenance", "INFERRED_FROM_BUN_DDL",
			"migration_kind", "ddl", "ddl_op", op)
		add(mig)
	}
	for _, m := range reBunMigrateAPI.FindAllStringSubmatchIndex(src, -1) {
		call := strings.TrimSpace(src[m[0]:m[1]])
		call = strings.TrimSuffix(call, "(")
		call = strings.TrimSpace(call)
		line := lineOf(src, m[0])
		mig := makeEntity("migrate_api:"+call, "SCOPE.Operation", "migration", file.Path, file.Language, line)
		setProps(&mig, "framework", "bun", "provenance", "INFERRED_FROM_BUN_MIGRATE_API",
			"migration_kind", "migrate_api", "migrate_call", call)
		add(mig)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// bunRelKind normalises a bun rel tag value into the shared relationship
// vocabulary (has_one/belongs_to/has_many/many2many).
func bunRelKind(v string) string {
	switch v {
	case "has-one":
		return "has_one"
	case "belongs-to":
		return "belongs_to"
	case "has-many":
		return "has_many"
	case "m2m":
		return "many2many"
	default:
		return v
	}
}

// bunColumnName extracts the explicit column name from a bun tag. The column
// name is the first comma-separated token when it is a bare identifier (not a
// known flag); otherwise the Go field name is used as the implicit column.
func bunColumnName(tag, fieldName string) string {
	first := tag
	if i := strings.Index(tag, ","); i >= 0 {
		first = tag[:i]
	}
	first = strings.TrimSpace(first)
	switch first {
	case "", "pk", "autoincrement", "nullzero", "notnull", "scanonly",
		"soft_delete", "unique", "-", "rel", "type":
		return fieldName
	}
	// A leading "column:" style is not used by bun; the bare token is the name.
	if isBunIdent(first) {
		return first
	}
	return fieldName
}

// bunHasFlag reports whether a comma-separated bun tag carries a bare flag.
func bunHasFlag(tag, flag string) bool {
	for _, tok := range strings.Split(tag, ",") {
		if strings.TrimSpace(tok) == flag {
			return true
		}
	}
	return false
}

// isBunIdent reports whether s is a plain column identifier (letters, digits,
// underscores), distinguishing a column name from a key:value option.
func isBunIdent(s string) bool {
	if s == "" || strings.Contains(s, ":") {
		return false
	}
	for _, r := range s {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// bunQueryModel scans forward from a NewSelect()/... call for a chained
// .Model(&T{}) / .Model((*T)(nil)) within a bounded window and returns the
// model name, or "" when the query is not bound to a concrete type here.
func bunQueryModel(src string, off int) string {
	end := off + 200
	if end > len(src) {
		end = len(src)
	}
	window := src[off:end]
	// Stop at a statement boundary so we don't bind a later query's model.
	// In Go, a newline ends the statement for a single-line builder chain;
	// an explicit ';' also ends it. Take the earliest boundary.
	cut := len(window)
	if i := strings.IndexByte(window, '\n'); i >= 0 && i < cut {
		cut = i
	}
	if i := strings.IndexByte(window, ';'); i >= 0 && i < cut {
		cut = i
	}
	window = window[:cut]
	if m := reBunQueryModel.FindStringSubmatch(window); m != nil {
		if m[1] != "" {
			return m[1]
		}
		return m[2]
	}
	return ""
}

// relationshipTargetLocal strips slice/pointer/package decorations from a Go
// field type to recover the referenced model name. (File-local to avoid
// editing the shared gorm helper.)
func relationshipTargetLocal(fieldType string) string {
	t := strings.TrimPrefix(fieldType, "[]")
	t = strings.TrimPrefix(t, "*")
	if i := strings.LastIndex(t, "."); i >= 0 {
		t = t[i+1:]
	}
	return t
}
