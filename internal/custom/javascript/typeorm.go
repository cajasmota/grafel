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
	"github.com/cajasmota/archigraph/internal/lifecycle"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_typeorm", &typeormExtractor{})
}

type typeormExtractor struct{}

func (e *typeormExtractor) Language() string { return "custom_js_typeorm" }

var (
	// @Entity() / @Entity('table_name') class FooEntity
	reTypeORMEntity = regexp.MustCompile(
		`@Entity\s*\([^@]*?\)\s*(?:export\s+)?(?:abstract\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	// @ViewEntity class
	reTypeORMViewEntity = regexp.MustCompile(
		`@ViewEntity\s*\([^@]*?\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	// @Column / @PrimaryColumn / @PrimaryGeneratedColumn
	reTypeORMColumn = regexp.MustCompile(
		`@(?:PrimaryGenerated|Primary)?Column\s*\([^)]*\)\s+(\w+)`,
	)
	// @OneToMany / @ManyToOne / @OneToOne / @ManyToMany
	// Uses [^@]* instead of [^)]* to handle arrow functions like (() => Post, post => post.user)
	// that contain nested parentheses. Stops at next decorator (@) instead.
	// Group 1 = relation kind, group 2 = decorator-args blob, group 3 = field name.
	reTypeORMRelation = regexp.MustCompile(
		`@(OneToMany|ManyToOne|OneToOne|ManyToMany)\s*\(([^@]*?)\)\s+(\w+)`,
	)
	// The type-target arrow inside a relation decorator: `() => Order` or
	// `type => Order` or `() => Order` (with optional parens around the param).
	// Captures the referenced entity class name (group 1).
	reTypeORMRelationTarget = regexp.MustCompile(
		`=>\s*([A-Z][A-Za-z0-9_]*)`,
	)
	// lazy: true inside a TypeORM relation decorator options object.
	// Matches the full decorator block so we can extract field name alongside it.
	// Issue #3071 — lazy_loading_recognition for TypeORM.
	reTypeORMLazyRelation = regexp.MustCompile(
		`@(OneToMany|ManyToOne|OneToOne|ManyToMany)\s*\(([^@]*?lazy\s*:\s*true[^@]*?)\)\s+(\w+)`,
	)
	// @Repository / Repository<Entity> usage
	reTypeORMRepository = regexp.MustCompile(
		`getRepository\s*\(\s*([A-Z][A-Za-z0-9_]*)\s*\)|getCustomRepository\s*\(\s*([A-Z][A-Za-z0-9_]*)\s*\)`,
	)
	// DataSource / createConnection / getConnection
	reTypeORMDataSource = regexp.MustCompile(
		`new\s+DataSource\s*\(|createConnection\s*\(|createDataSource\s*\(`,
	)
	// @Migration class
	reTypeORMMigration = regexp.MustCompile(
		`(?:export\s+)?class\s+([A-Za-z0-9_]+)\s+implements\s+[^{]*\bMigrationInterface\b`,
	)
	// @InjectRepository
	reTypeORMInjectRepo = regexp.MustCompile(
		`@InjectRepository\s*\(\s*([A-Z][A-Za-z0-9_]*)\s*\)`,
	)
	// QueryBuilder: createQueryBuilder
	reTypeORMQueryBuilder = regexp.MustCompile(
		`createQueryBuilder\s*\(\s*['"]([A-Za-z0-9_]+)['"]`,
	)
	// Migration schema-change ops inside up()/down(): queryRunner.createTable,
	// addColumn, dropColumn, dropTable, createIndex, etc.
	reTypeORMMigrationOp = regexp.MustCompile(
		`queryRunner\s*\.\s*(createTable|dropTable|renameTable|addColumn|addColumns|dropColumn|dropColumns|changeColumn|renameColumn|createIndex|dropIndex|createForeignKey|dropForeignKey|createPrimaryKey|dropPrimaryKey|createUniqueConstraint|dropUniqueConstraint|createCheckConstraint|dropCheckConstraint)\s*\(`,
	)
	// new Table({ name: "users" }) target inside a migration op.
	reTypeORMMigrationTable = regexp.MustCompile(
		`new\s+Table\s*\(\s*\{\s*name\s*:\s*['"]([A-Za-z0-9_.]+)['"]`,
	)
)

// typeormRelationCardinality maps a TypeORM relation decorator to the shared
// ORM relationship-cardinality vocabulary, used as the `cardinality` prop on
// the GRAPH_RELATES edge from the owning @Entity to the target entity.
//
//	@OneToMany(() => Order, ...)  → one_to_many
//	@ManyToOne(() => User, ...)   → many_to_one
//	@OneToOne(() => Profile, ...) → one_to_one
//	@ManyToMany(() => Tag, ...)   → many_to_many
func typeormRelationCardinality(decorator string) string {
	switch decorator {
	case "OneToMany":
		return "one_to_many"
	case "ManyToOne":
		return "many_to_one"
	case "OneToOne":
		return "one_to_one"
	case "ManyToMany":
		return "many_to_many"
	default:
		return ""
	}
}

// typeormMigrationOpSubtype maps a queryRunner method name to a normalized
// schema-change op subtype shared across ORM migration extractors.
func typeormMigrationOpSubtype(method string) string {
	switch method {
	case "createTable":
		return "create_table"
	case "dropTable":
		return "drop_table"
	case "renameTable":
		return "rename_table"
	case "addColumn", "addColumns":
		return "add_column"
	case "dropColumn", "dropColumns":
		return "drop_column"
	case "changeColumn":
		return "alter_column"
	case "renameColumn":
		return "rename_column"
	case "createIndex":
		return "create_index"
	case "dropIndex":
		return "drop_index"
	case "createForeignKey":
		return "add_foreign_key"
	case "dropForeignKey":
		return "drop_foreign_key"
	case "createPrimaryKey":
		return "add_primary_key"
	case "dropPrimaryKey":
		return "drop_primary_key"
	case "createUniqueConstraint":
		return "add_unique_constraint"
	case "dropUniqueConstraint":
		return "drop_unique_constraint"
	case "createCheckConstraint":
		return "add_check_constraint"
	case "dropCheckConstraint":
		return "drop_check_constraint"
	default:
		return "schema_change"
	}
}

// typeormMigrationOpTable extracts a table name from the first argument of a
// queryRunner migration op. Handles both a bare string literal
// (`dropTable("users")`) and a `new Table({ name: "users" })` object.
func typeormMigrationOpTable(window string) string {
	if m := reTypeORMMigrationTable.FindStringSubmatch(window); m != nil {
		return m[1]
	}
	// Bare string-literal first arg: `(  "users"` or `( 'users'`.
	trimmed := strings.TrimLeft(window, " \t\n\r")
	if len(trimmed) > 0 && (trimmed[0] == '"' || trimmed[0] == '\'') {
		q := trimmed[0]
		if end := strings.IndexByte(trimmed[1:], q); end >= 0 {
			return trimmed[1 : 1+end]
		}
	}
	return ""
}

func (e *typeormExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.typeorm_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "typeorm"),
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

	// DataSource
	for _, m := range reTypeORMDataSource.FindAllStringIndex(src, -1) {
		ent := makeEntity("DataSource", "SCOPE.Service", "database", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "typeorm", "provenance", "INFERRED_FROM_TYPEORM_DATASOURCE")
		addEntity(ent)
	}

	// @Entity classes. Track each class's byte offset and its index in the
	// entities slice so relation decorators below can hang a GRAPH_RELATES edge
	// off the owning @Entity model node.
	type ownerInfo struct {
		name   string
		offset int
		idx    int
	}
	var owners []ownerInfo
	knownEntities := make(map[string]bool)
	entityMatches := reTypeORMEntity.FindAllStringSubmatchIndex(src, -1)
	for i, m := range entityMatches {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "entity", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "typeorm", "provenance", "INFERRED_FROM_TYPEORM_ENTITY")
		// Data-lifecycle traits (#3628 child): scan this entity's class body —
		// from its @Entity decorator to the next @Entity (or EOF) — for the
		// TypeORM soft-delete/timestamp/audit decorators and stamp the resolved
		// traits onto the model node before it is emitted.
		bodyEnd := len(src)
		if i+1 < len(entityMatches) {
			bodyEnd = entityMatches[i+1][0]
		}
		lifecycle.TypeORM(src[m[0]:bodyEnd]).
			Stamp(func(kv ...string) { setProps(&ent, kv...) })
		if !seen[fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.Subtype)] {
			owners = append(owners, ownerInfo{name: name, offset: m[0], idx: len(entities)})
			knownEntities[name] = true
		}
		addEntity(ent)
	}

	// owningEntity returns the @Entity class whose declaration most closely
	// precedes a body offset, and whether one was found.
	owningEntity := func(offset int) (ownerInfo, bool) {
		best := ownerInfo{idx: -1}
		found := false
		for _, o := range owners {
			if o.offset <= offset {
				best = o
				found = true
			}
		}
		return best, found
	}

	// @ViewEntity classes
	for _, m := range reTypeORMViewEntity.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "view_entity", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "typeorm", "provenance", "INFERRED_FROM_TYPEORM_VIEW_ENTITY")
		addEntity(ent)
	}

	// @Column properties
	for _, m := range reTypeORMColumn.FindAllStringSubmatchIndex(src, -1) {
		colName := src[m[2]:m[3]]
		ent := makeEntity(colName, "SCOPE.Component", "column", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "typeorm", "provenance", "INFERRED_FROM_TYPEORM_COLUMN")
		addEntity(ent)
	}

	// @OneToMany / @ManyToOne etc. relations
	for _, m := range reTypeORMRelation.FindAllStringSubmatchIndex(src, -1) {
		relType := src[m[2]:m[3]]
		argsBlob := src[m[4]:m[5]]
		fieldName := src[m[6]:m[7]]
		// The arrow-returned class is the target entity: @OneToMany(() => Order).
		target := ""
		if tm := reTypeORMRelationTarget.FindStringSubmatch(argsBlob); tm != nil {
			target = tm[1]
		}
		name := fmt.Sprintf("%s:%s", relType, fieldName)
		ent := makeEntity(name, "SCOPE.Component", "relation", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "typeorm", "relation_type", relType, "field_name", fieldName,
			"provenance", "INFERRED_FROM_TYPEORM_RELATION")
		if target != "" {
			setProps(&ent, "target_entity", target)
		}
		addEntity(ent)

		// GRAPH_RELATES model↔model edge with cardinality, hung off the owning
		// @Entity model node. Only emitted when the arrow target resolves to a
		// known same-file @Entity class — cross-file targets stay honest-partial
		// (the topology is preserved as the `target_entity` prop above).
		if owner, ok := owningEntity(m[0]); ok && target != "" && knownEntities[target] {
			card := typeormRelationCardinality(relType)
			entities[owner.idx].Relationships = append(entities[owner.idx].Relationships,
				types.RelationshipRecord{
					FromID: "Class:" + owner.name,
					ToID:   "Class:" + target,
					Kind:   string(types.RelationshipKindGraphRelates),
					Properties: map[string]string{
						"framework":     "typeorm",
						"cardinality":   card,
						"relation_type": relType,
						"field_name":    fieldName,
						"provenance":    "INFERRED_FROM_TYPEORM_RELATION",
					},
				})
		}
	}

	// Lazy relations: @OneToMany/@ManyToOne/etc. with { lazy: true } option.
	// Issue #3071 — lazy_loading_recognition for TypeORM.
	for _, m := range reTypeORMLazyRelation.FindAllStringSubmatchIndex(src, -1) {
		relType := src[m[2]:m[3]]
		fieldName := src[m[6]:m[7]]
		name := fmt.Sprintf("lazy:%s:%s", relType, fieldName)
		ent := makeEntity(name, "SCOPE.Pattern", "lazy_relation", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "typeorm", "relation_type", relType, "field_name", fieldName,
			"lazy_loading", "true", "provenance", "INFERRED_FROM_TYPEORM_LAZY_RELATION")
		addEntity(ent)
	}

	// getRepository / getCustomRepository
	for _, m := range reTypeORMRepository.FindAllStringSubmatchIndex(src, -1) {
		var entityName string
		if m[2] >= 0 {
			entityName = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			entityName = src[m[4]:m[5]]
		}
		if entityName == "" {
			continue
		}
		name := "repo:" + entityName
		ent := makeEntity(name, "SCOPE.Component", "repository", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "typeorm", "entity_name", entityName,
			"provenance", "INFERRED_FROM_TYPEORM_REPOSITORY")
		addEntity(ent)
	}

	// @InjectRepository
	for _, m := range reTypeORMInjectRepo.FindAllStringSubmatchIndex(src, -1) {
		entityName := src[m[2]:m[3]]
		name := "inject_repo:" + entityName
		ent := makeEntity(name, "SCOPE.Component", "repository", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "typeorm", "entity_name", entityName,
			"provenance", "INFERRED_FROM_TYPEORM_INJECT_REPOSITORY")
		addEntity(ent)
	}

	// Migrations
	for _, m := range reTypeORMMigration.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "migration", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "typeorm", "provenance", "INFERRED_FROM_TYPEORM_MIGRATION")
		addEntity(ent)
	}

	// QueryBuilder usage
	for _, m := range reTypeORMQueryBuilder.FindAllStringSubmatchIndex(src, -1) {
		alias := src[m[2]:m[3]]
		name := "qb:" + alias
		ent := makeEntity(name, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "typeorm", "alias", alias,
			"provenance", "INFERRED_FROM_TYPEORM_QUERY_BUILDER")
		addEntity(ent)
	}

	// Migration schema-change operations (queryRunner.*). Each op becomes a
	// SCOPE.Evolution entity carrying the normalized change kind so downstream
	// tooling can reconstruct the schema delta a migration applies.
	for _, m := range reTypeORMMigrationOp.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		opSubtype := typeormMigrationOpSubtype(method)
		// Look ahead a small window to capture an inline table name argument.
		window := src[m[1]:min(len(src), m[1]+200)]
		table := typeormMigrationOpTable(window)
		name := opSubtype
		if table != "" {
			name = opSubtype + ":" + table
		}
		ent := makeEntity(name, "SCOPE.Evolution", opSubtype, file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "typeorm", "migration_op", method,
			"provenance", "INFERRED_FROM_TYPEORM_MIGRATION_OP")
		if table != "" {
			setProps(&ent, "table", table)
		}
		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
