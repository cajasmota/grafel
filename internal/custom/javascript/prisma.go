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
	extreg.Register("custom_js_prisma", &prismaExtractor{})
}

type prismaExtractor struct{}

func (e *prismaExtractor) Language() string { return "custom_js_prisma" }

var (
	// Prisma schema model definitions
	rePrismaModel = regexp.MustCompile(
		`(?m)^model\s+([A-Z][A-Za-z0-9_]*)\s*\{`,
	)
	// Prisma enum definitions
	rePrismaEnum = regexp.MustCompile(
		`(?m)^enum\s+([A-Z][A-Za-z0-9_]*)\s*\{`,
	)
	// Prisma Client usage: prisma.model.operation()
	rePrismaClientCall = regexp.MustCompile(
		`(?:prisma|db)\s*\.\s*([a-z][A-Za-z0-9_]*)\s*\.\s*(findUnique|findFirst|findMany|create|createMany|update|updateMany|upsert|delete|deleteMany|count|aggregate|groupBy|findUniqueOrThrow|findFirstOrThrow)\s*\(`,
	)
	// PrismaClient instantiation
	rePrismaClientNew = regexp.MustCompile(
		`new\s+PrismaClient\s*\(`,
	)
	// $transaction
	rePrismaTransaction = regexp.MustCompile(
		`(?:prisma|db)\s*\.\s*\$transaction\s*\(`,
	)
	// $extends
	rePrismaExtends = regexp.MustCompile(
		`(?:prisma|db)\s*\.\s*\$extends\s*\(`,
	)
	// Middleware
	rePrismaMiddleware = regexp.MustCompile(
		`(?:prisma|db)\s*\.\s*\$use\s*\(`,
	)
	// Raw SQL DDL ops inside Prisma migration.sql files. Prisma emits
	// migrations as plain SQL under prisma/migrations/<ts>/migration.sql.
	reSQLCreateTable = regexp.MustCompile(
		`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?["` + "`" + `']?([A-Za-z0-9_.]+)["` + "`" + `']?`,
	)
	reSQLDropTable = regexp.MustCompile(
		`(?i)DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?["` + "`" + `']?([A-Za-z0-9_.]+)["` + "`" + `']?`,
	)
	reSQLAlterTable = regexp.MustCompile(
		`(?i)ALTER\s+TABLE\s+["` + "`" + `']?([A-Za-z0-9_.]+)["` + "`" + `']?\s+(ADD|DROP|ALTER|RENAME|MODIFY|CHANGE)`,
	)
	reSQLCreateIndex = regexp.MustCompile(
		`(?i)CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?["` + "`" + `']?([A-Za-z0-9_.]+)["` + "`" + `']?`,
	)
	reSQLDropIndex = regexp.MustCompile(
		`(?i)DROP\s+INDEX\s+(?:IF\s+EXISTS\s+)?["` + "`" + `']?([A-Za-z0-9_.]+)["` + "`" + `']?`,
	)
)

// alterTableOpSubtype maps an ALTER TABLE clause keyword to a schema-change subtype.
func alterTableOpSubtype(clause string) string {
	switch strings.ToUpper(clause) {
	case "ADD":
		return "add_column"
	case "DROP":
		return "drop_column"
	case "ALTER", "MODIFY", "CHANGE":
		return "alter_column"
	case "RENAME":
		return "rename_column"
	default:
		return "alter_table"
	}
}

func (e *prismaExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.prisma_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "prisma"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	// Prisma schema files are not JS/TS but we still extract from .prisma files
	// by checking both JS/TS and .prisma extension. Prisma migrations are raw
	// SQL files under prisma/migrations/<ts>/migration.sql; parse those too.
	isPrismaSchema := strings.HasSuffix(file.Path, ".prisma")
	isPrismaMigration := strings.HasSuffix(file.Path, "migration.sql") &&
		strings.Contains(filepath.ToSlash(file.Path), "migrations/")
	if lang != "typescript" && lang != "javascript" && !isPrismaSchema && !isPrismaMigration {
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

	// Prisma schema models
	for _, m := range rePrismaModel.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "model", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "prisma", "provenance", "INFERRED_FROM_PRISMA_MODEL")
		addEntity(ent)
	}

	// Prisma schema enums
	for _, m := range rePrismaEnum.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "enum", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "prisma", "provenance", "INFERRED_FROM_PRISMA_ENUM")
		addEntity(ent)
	}

	// PrismaClient instantiation
	for _, m := range rePrismaClientNew.FindAllStringIndex(src, -1) {
		ent := makeEntity("PrismaClient", "SCOPE.Service", "client", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "prisma", "provenance", "INFERRED_FROM_PRISMA_CLIENT")
		addEntity(ent)
	}

	// prisma.model.operation() calls
	for _, m := range rePrismaClientCall.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[m[2]:m[3]]
		operation := src[m[4]:m[5]]
		name := fmt.Sprintf("%s.%s", modelName, operation)
		ent := makeEntity(name, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "prisma", "model", modelName, "operation", operation,
			"provenance", "INFERRED_FROM_PRISMA_QUERY")
		addEntity(ent)
	}

	// $transaction
	for _, m := range rePrismaTransaction.FindAllStringIndex(src, -1) {
		ent := makeEntity("$transaction", "SCOPE.Operation", "transaction", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "prisma", "provenance", "INFERRED_FROM_PRISMA_TRANSACTION")
		addEntity(ent)
	}

	// $extends
	for _, m := range rePrismaExtends.FindAllStringIndex(src, -1) {
		ent := makeEntity("$extends", "SCOPE.Component", "extension", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "prisma", "provenance", "INFERRED_FROM_PRISMA_EXTENDS")
		addEntity(ent)
	}

	// $use (middleware)
	for _, m := range rePrismaMiddleware.FindAllStringIndex(src, -1) {
		ent := makeEntity("$use", "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "prisma", "provenance", "INFERRED_FROM_PRISMA_MIDDLEWARE")
		addEntity(ent)
	}

	// Migration SQL DDL ops — only for migration.sql files so we don't treat
	// raw SQL embedded in application TS/JS as Prisma migrations.
	if isPrismaMigration {
		emitSQLMigrationOp := func(subtype, target string, off int) {
			ent := makeEntity(subtype+":"+target, "SCOPE.Evolution", subtype, file.Path, file.Language, lineOf(src, off))
			setProps(&ent, "framework", "prisma", "table", target,
				"provenance", "INFERRED_FROM_PRISMA_MIGRATION_SQL")
			addEntity(ent)
		}
		for _, m := range reSQLCreateTable.FindAllStringSubmatchIndex(src, -1) {
			emitSQLMigrationOp("create_table", src[m[2]:m[3]], m[0])
		}
		for _, m := range reSQLDropTable.FindAllStringSubmatchIndex(src, -1) {
			emitSQLMigrationOp("drop_table", src[m[2]:m[3]], m[0])
		}
		for _, m := range reSQLAlterTable.FindAllStringSubmatchIndex(src, -1) {
			table := src[m[2]:m[3]]
			clause := src[m[4]:m[5]]
			emitSQLMigrationOp(alterTableOpSubtype(clause), table, m[0])
		}
		for _, m := range reSQLCreateIndex.FindAllStringSubmatchIndex(src, -1) {
			emitSQLMigrationOp("create_index", src[m[2]:m[3]], m[0])
		}
		for _, m := range reSQLDropIndex.FindAllStringSubmatchIndex(src, -1) {
			emitSQLMigrationOp("drop_index", src[m[2]:m[3]], m[0])
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
