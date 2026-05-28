package javascript

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_drizzle", &drizzleExtractor{})
}

type drizzleExtractor struct{}

func (e *drizzleExtractor) Language() string { return "custom_js_drizzle" }

var (
	// export const users = pgTable("users", { ... }) — also mysqlTable/sqliteTable.
	// First group = the JS const binding, second = the SQL table name.
	reDrizzleTable = regexp.MustCompile(
		`(?:export\s+)?const\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(pgTable|mysqlTable|sqliteTable)\s*\(\s*['"]([A-Za-z0-9_.]+)['"]`,
	)
	// export const myEnum = pgEnum("role", [...])
	reDrizzleEnum = regexp.MustCompile(
		`(?:export\s+)?const\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(?:pgEnum|mysqlEnum)\s*\(\s*['"]([A-Za-z0-9_.]+)['"]`,
	)
	// export const usersRelations = relations(users, ({ many }) => ({ ... }))
	reDrizzleRelations = regexp.MustCompile(
		`relations\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)\s*,`,
	)
	// drizzle(client) / drizzle(pool, { schema })
	reDrizzleClient = regexp.MustCompile(
		`\bdrizzle\s*\(`,
	)
)

func (e *drizzleExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.drizzle_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "drizzle"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	// drizzle-kit emits raw SQL migration files (0000_xxx.sql). Parse those in
	// addition to TS/JS schema definitions.
	isSQLMigration := strings.HasSuffix(file.Path, ".sql") &&
		strings.Contains(filepath.ToSlash(file.Path), "migrations/")
	isDrizzleMigrationsDir := strings.Contains(filepath.ToSlash(file.Path), "drizzle/") &&
		strings.HasSuffix(file.Path, ".sql")
	sqlMode := isSQLMigration || isDrizzleMigrationsDir
	if lang != "typescript" && lang != "javascript" && !sqlMode {
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

	if lang == "typescript" || lang == "javascript" {
		// Table model definitions.
		for _, m := range reDrizzleTable.FindAllStringSubmatchIndex(src, -1) {
			binding := src[m[2]:m[3]]
			builder := src[m[4]:m[5]]
			table := src[m[6]:m[7]]
			ent := makeEntity(table, "SCOPE.Schema", "model", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "drizzle", "binding", binding, "builder", builder,
				"table", table, "provenance", "INFERRED_FROM_DRIZZLE_TABLE")
			addEntity(ent)
		}

		// Enum definitions.
		for _, m := range reDrizzleEnum.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[6]:m[7]]
			ent := makeEntity(name, "SCOPE.Schema", "enum", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "drizzle", "provenance", "INFERRED_FROM_DRIZZLE_ENUM")
			addEntity(ent)
		}

		// Relations.
		for _, m := range reDrizzleRelations.FindAllStringSubmatchIndex(src, -1) {
			model := src[m[2]:m[3]]
			ent := makeEntity("relations:"+model, "SCOPE.Pattern", "relation", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "drizzle", "model", model,
				"provenance", "INFERRED_FROM_DRIZZLE_RELATIONS")
			addEntity(ent)
		}

		// drizzle() client.
		for _, m := range reDrizzleClient.FindAllStringIndex(src, -1) {
			ent := makeEntity("drizzle", "SCOPE.Service", "database", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "drizzle", "provenance", "INFERRED_FROM_DRIZZLE_CLIENT")
			addEntity(ent)
		}
	}

	// SQL migration DDL ops.
	if sqlMode {
		emit := func(subtype, target string, off int) {
			ent := makeEntity(subtype+":"+target, "SCOPE.Evolution", subtype, file.Path, file.Language, lineOf(src, off))
			setProps(&ent, "framework", "drizzle", "table", target,
				"provenance", "INFERRED_FROM_DRIZZLE_MIGRATION_SQL")
			addEntity(ent)
		}
		for _, m := range reSQLCreateTable.FindAllStringSubmatchIndex(src, -1) {
			emit("create_table", src[m[2]:m[3]], m[0])
		}
		for _, m := range reSQLDropTable.FindAllStringSubmatchIndex(src, -1) {
			emit("drop_table", src[m[2]:m[3]], m[0])
		}
		for _, m := range reSQLAlterTable.FindAllStringSubmatchIndex(src, -1) {
			emit(alterTableOpSubtype(src[m[4]:m[5]]), src[m[2]:m[3]], m[0])
		}
		for _, m := range reSQLCreateIndex.FindAllStringSubmatchIndex(src, -1) {
			emit("create_index", src[m[2]:m[3]], m[0])
		}
		for _, m := range reSQLDropIndex.FindAllStringSubmatchIndex(src, -1) {
			emit("drop_index", src[m[2]:m[3]], m[0])
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
