package javascript

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
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
	// Prisma model field: lines like `  fieldName  FieldType  @directives`
	// inside a model block. Matches fields with a type (scalar or relation type).
	// Captures: field name (group 1), type (group 2).
	rePrismaModelField = regexp.MustCompile(
		`(?m)^\s{1,4}([a-z][A-Za-z0-9_]*)\s+([A-Za-z][A-Za-z0-9_?[\]]*)\s`,
	)
	// @relation directive — matches @relation(fields: [...], references: [...])
	// with an optional name string as first argument.
	// Group 1 = optional relation name, group 2 = fields list, group 3 = references list.
	rePrismaRelation = regexp.MustCompile(
		`@relation\s*\(\s*(?:"([^"]*?)"\s*,\s*)?fields:\s*\[([^\]]*)\]\s*,\s*references:\s*\[([^\]]*)\]`,
	)
	// @relation without fields/references (back-reference side): @relation("name")
	rePrismaRelationRef = regexp.MustCompile(
		`@relation\s*\(\s*"([^"]*?)"\s*\)`,
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

var (
	// A model block field line: `  fieldName  Type  @directives`. Captures the
	// field name (group 1) and the raw type token (group 2) including any `[]`
	// list or `?` optional suffix.
	rePrismaFieldLine = regexp.MustCompile(
		`(?m)^\s{1,8}([a-z][A-Za-z0-9_]*)\s+([A-Z][A-Za-z0-9_]*(?:\[\]|\?)?)`)
	// Detects a @relation(... fields: [...] ...) on a field line — the FK-owning
	// (many_to_one / one_to_one owner) side of a Prisma relation.
	rePrismaFieldHasFKRel = regexp.MustCompile(`@relation\s*\([^)]*\bfields\s*:`)
)

// prismaModelBlocks returns each model block's name and body for a schema.
func prismaModelBlocks(src string) []struct {
	name string
	body string
} {
	var blocks []struct {
		name string
		body string
	}
	for _, m := range rePrismaModel.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		// Body runs from the opening brace to the matching closing brace at
		// column 0 (Prisma blocks are not nested), best-effort via the next
		// line that is a lone `}`.
		start := m[1]
		end := len(src)
		if i := strings.Index(src[start:], "\n}"); i >= 0 {
			end = start + i + 2
		}
		blocks = append(blocks, struct {
			name string
			body string
		}{name: name, body: src[start:end]})
	}
	return blocks
}

// prismaModularSiblings returns the union of model and enum names declared in
// EVERY `.prisma` file that shares the schema folder with the file at relPath,
// realizing Prisma's modular split-schema layout (the `prismaSchemaFolder`
// preview feature: `prisma/schema/*.prisma`, one domain per file). All sibling
// files form ONE logical schema, so a `model Post` in `post.prisma` can carry
// `author User @relation(...)` where `model User` lives in `user.prisma`.
//
// Detection is structural: the file lives in a directory that holds more than
// one `.prisma` file (the canonical layout is a `prisma/schema/` folder, but we
// don't hardcode the name — any multi-`.prisma` folder is treated as a split
// schema). The single-`schema.prisma` case yields no siblings, so the union
// collapses to just the in-file symbol table (one file = the union of one).
//
// Returns (modelNames, enumNames). Both are nil when repoRoot is empty (direct
// fixture tests that don't set RepoRoot), when the directory can't be read, or
// when the file is the only `.prisma` file in its folder.
func prismaModularSiblings(repoRoot, relPath string) (models, enums map[string]bool) {
	if repoRoot == "" {
		return nil, nil
	}
	dirAbs := filepath.Dir(filepath.Join(repoRoot, filepath.FromSlash(relPath)))
	entries, err := os.ReadDir(dirAbs)
	if err != nil {
		return nil, nil
	}
	var prismaFiles []string
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		if strings.HasSuffix(ent.Name(), ".prisma") {
			prismaFiles = append(prismaFiles, ent.Name())
		}
	}
	// A lone `.prisma` file in the folder is the single-file case; the caller's
	// in-file symbol table already covers it. Only a multi-file folder is a
	// modular split schema worth unioning.
	if len(prismaFiles) < 2 {
		return nil, nil
	}
	base := filepath.Base(relPath)
	models = make(map[string]bool)
	enums = make(map[string]bool)
	for _, name := range prismaFiles {
		if name == base {
			continue // siblings only; the current file is unioned by the caller
		}
		data, err := os.ReadFile(filepath.Join(dirAbs, name))
		if err != nil {
			continue
		}
		sib := string(data)
		for _, m := range rePrismaModel.FindAllStringSubmatch(sib, -1) {
			models[m[1]] = true
		}
		for _, m := range rePrismaEnum.FindAllStringSubmatch(sib, -1) {
			enums[m[1]] = true
		}
	}
	return models, enums
}

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
	tracer := otel.Tracer("grafel/custom/javascript")
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

	// Prisma schema models. Track the entities-slice index of each model node so
	// the relation-field scan below can hang GRAPH_RELATES edges off it, and the
	// set of known model names so targets resolve in-file.
	modelIdx := make(map[string]int)
	knownModels := make(map[string]bool)
	knownEnums := make(map[string]bool)
	// Modular split schema (`prismaSchemaFolder`): a `prisma/schema/*.prisma`
	// folder is ONE logical schema. Seed the symbol table with model/enum names
	// declared in SIBLING `.prisma` files so cross-file @relation targets, field
	// types, and enum references resolve. Empty (nil) for the single-file case —
	// then the union is just this file's own symbols, added below.
	if isPrismaSchema {
		sibModels, sibEnums := prismaModularSiblings(file.RepoRoot, file.Path)
		for name := range sibModels {
			knownModels[name] = true
		}
		for name := range sibEnums {
			knownEnums[name] = true
		}
	}
	for _, m := range rePrismaModel.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "model", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "prisma", "provenance", "INFERRED_FROM_PRISMA_MODEL")
		if !seen[fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.Subtype)] {
			modelIdx[name] = len(entities)
			knownModels[name] = true
		}
		addEntity(ent)
	}

	// Prisma schema enums
	for _, m := range rePrismaEnum.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		knownEnums[name] = true
		ent := makeEntity(name, "SCOPE.Schema", "enum", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "prisma", "provenance", "INFERRED_FROM_PRISMA_ENUM")
		addEntity(ent)
	}

	// Prisma model field definitions — emit SCOPE.Component "field" entities for
	// schema_extraction. Only inside .prisma files. Each field is a CONTAINS member
	// of its owning model (issue #4365); a field whose type names another model
	// also carries a REFERENCES edge to that model (the relation target type).
	if isPrismaSchema {
		// Map each model field-line global offset to its owning model name by
		// walking the model blocks. modelFieldOwner returns the owner for an offset.
		type blockSpan struct {
			name       string
			start, end int
		}
		var spans []blockSpan
		for _, m := range rePrismaModel.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			start := m[1]
			end := len(src)
			if i := strings.Index(src[start:], "\n}"); i >= 0 {
				end = start + i + 2
			}
			spans = append(spans, blockSpan{name: name, start: start, end: end})
		}
		ownerOf := func(off int) (string, bool) {
			for _, s := range spans {
				if off >= s.start && off < s.end {
					return s.name, true
				}
			}
			return "", false
		}
		for _, m := range rePrismaModelField.FindAllStringSubmatchIndex(src, -1) {
			owner, hasOwner := ownerOf(m[0])
			// Only emit fields that live inside a model block. Lines inside the
			// datasource/generator blocks (`generator client {`, `model = "..."`)
			// otherwise leak in as standalone orphan "field" entities (#4365).
			if !hasOwner {
				continue
			}
			fieldName := src[m[2]:m[3]]
			fieldType := src[m[4]:m[5]]
			ent := makeEntity(fieldName, "SCOPE.Component", "field", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "prisma", "field_type", fieldType,
				"provenance", "INFERRED_FROM_PRISMA_FIELD")
			setProps(&ent, "owner_model", owner)
			// Relation field: base type names another model (incl. self-relations).
			// Capture the target as a REFERENCES edge so the field is not a semantic
			// orphan.
			baseType := strings.TrimSuffix(strings.TrimSuffix(fieldType, "[]"), "?")
			if knownModels[baseType] {
				setProps(&ent, "target_model", baseType)
				ent.Relationships = append(ent.Relationships,
					referencesClassEdge(ent.ID, baseType, "prisma", fieldName))
			} else if knownEnums[baseType] {
				// Enum-typed field — resolves to the enum node (possibly in a
				// sibling `.prisma` file under the modular split layout). Record
				// a REFERENCES edge so the cross-file enum is not a dangling type.
				setProps(&ent, "target_enum", baseType)
				ent.Relationships = append(ent.Relationships,
					referencesClassEdge(ent.ID, baseType, "prisma", fieldName))
			}
			entities[modelIdx[owner]].Relationships = append(entities[modelIdx[owner]].Relationships,
				containsFieldEdge(owner, ent.ID, fieldName, "prisma"))
			addEntity(ent)
		}

		// @relation(fields: [...], references: [...]) — emit relation + foreign_key entities.
		for _, m := range rePrismaRelation.FindAllStringSubmatchIndex(src, -1) {
			relName := ""
			if m[2] >= 0 {
				relName = src[m[2]:m[3]]
			}
			fields := strings.TrimSpace(src[m[4]:m[5]])
			references := strings.TrimSpace(src[m[6]:m[7]])
			name := "relation"
			if relName != "" {
				name = "relation:" + relName
			}
			ent := makeEntity(name, "SCOPE.Pattern", "relation", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "prisma", "fields", fields, "references", references,
				"provenance", "INFERRED_FROM_PRISMA_RELATION")
			addEntity(ent)
			// Also emit a foreign_key entity for the FK column side.
			fkEnt := makeEntity("fk:"+fields, "SCOPE.Component", "foreign_key", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&fkEnt, "framework", "prisma", "fk_fields", fields, "ref_fields", references,
				"provenance", "INFERRED_FROM_PRISMA_RELATION_FK")
			addEntity(fkEnt)
		}

		// Back-reference side of a named @relation (no fields/references).
		for _, m := range rePrismaRelationRef.FindAllStringSubmatchIndex(src, -1) {
			relName := src[m[2]:m[3]]
			ent := makeEntity("relation:"+relName, "SCOPE.Pattern", "relation", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "prisma", "relation_name", relName,
				"provenance", "INFERRED_FROM_PRISMA_RELATION_REF")
			addEntity(ent)
		}

		// GRAPH_RELATES model↔model edges with cardinality. Scan each model
		// block's fields; a field whose (base) type names another model is a
		// relation field:
		//
		//	orders   Order[]                              → one_to_many
		//	user     User    @relation(fields:[uId], ...) → many_to_one  (FK owner)
		//	profile  Profile?                             → one_to_one   (back side)
		//
		// FromID/ToID use the Class:<Model> convention so the resolver byName
		// index links them to the model nodes. knownModels is the UNION of this
		// file's models and those declared in SIBLING `.prisma` files under the
		// modular split layout (prismaSchemaFolder), so a relation whose target
		// model lives in another file of the same `prisma/schema/` folder
		// resolves cross-file. A capitalized type that is neither a model nor an
		// enum (e.g. a composite `type`) is still skipped — not a model relation.
		for _, blk := range prismaModelBlocks(src) {
			ownerIdx, ok := modelIdx[blk.name]
			if !ok {
				continue
			}
			for _, fm := range rePrismaFieldLine.FindAllStringSubmatchIndex(blk.body, -1) {
				fieldName := blk.body[fm[2]:fm[3]]
				rawType := blk.body[fm[4]:fm[5]]
				isList := strings.HasSuffix(rawType, "[]")
				baseType := strings.TrimSuffix(strings.TrimSuffix(rawType, "[]"), "?")
				if !knownModels[baseType] {
					continue // scalar/enum/composite type — not a model relation
				}
				// Determine cardinality. The list side is always one_to_many.
				// The singular side is many_to_one when this field owns the FK
				// (@relation(fields:[...])), else one_to_one (back-reference of a
				// 1:1, or the singular back side).
				lineText := blk.body[lineStart(blk.body, fm[0]):lineEnd(blk.body, fm[1])]
				var card string
				if isList {
					card = "one_to_many"
				} else if rePrismaFieldHasFKRel.MatchString(lineText) {
					card = "many_to_one"
				} else {
					card = "one_to_one"
				}
				entities[ownerIdx].Relationships = append(entities[ownerIdx].Relationships,
					types.RelationshipRecord{
						FromID: "Class:" + blk.name,
						ToID:   "Class:" + baseType,
						Kind:   string(types.RelationshipKindGraphRelates),
						Properties: types.Props{
							{K: "cardinality", V: card},
							{K: "field_name", V: fieldName},
							{K: "framework", V: "prisma"},
							{K: "provenance", V: "INFERRED_FROM_PRISMA_RELATION_FIELD"},
							{K: "target_model", V: baseType},
						},
					})
			}
		}
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
