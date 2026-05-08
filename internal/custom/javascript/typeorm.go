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
	reTypeORMRelation = regexp.MustCompile(
		`@(OneToMany|ManyToOne|OneToOne|ManyToMany)\s*\([^@]*?\)\s+(\w+)`,
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
)

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

	// @Entity classes
	for _, m := range reTypeORMEntity.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "entity", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "typeorm", "provenance", "INFERRED_FROM_TYPEORM_ENTITY")
		addEntity(ent)
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
		fieldName := src[m[4]:m[5]]
		name := fmt.Sprintf("%s:%s", relType, fieldName)
		ent := makeEntity(name, "SCOPE.Component", "relation", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "typeorm", "relation_type", relType, "field_name", fieldName,
			"provenance", "INFERRED_FROM_TYPEORM_RELATION")
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

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
