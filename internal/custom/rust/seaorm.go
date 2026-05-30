package rust

// seaorm.go — custom extractor for the SeaORM async ORM (Rust).
//
// Detects and emits entities for:
//
//   - #[derive(Clone, Debug, PartialEq, DeriveEntityModel)] + #[sea_orm(table_name = "...")] →
//     SCOPE.Component (subtype="orm_model")
//   - DeriveRelation enum variants with #[sea_orm(has_many / belongs_to = "...")] →
//     SCOPE.Pattern (subtype="orm_relationship")
//   - sea-orm-migration MigrationTrait impl blocks → SCOPE.Component (subtype="migration")
//
// Honesty:
//
//	partial — heuristic regex match on source text. Does NOT perform
//	full semantic analysis, import-resolution, or macro expansion.
//	Fixtures prove the detection surface; full cross-entity linking
//	requires import-graph analysis beyond this scanner.
//
// Issue #3269 — lang.rust.orm.seaorm ORM build.

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

func init() {
	extractor.Register("custom_rust_seaorm", &rustSeaORMExtractor{})
}

type rustSeaORMExtractor struct{}

func (e *rustSeaORMExtractor) Language() string { return "custom_rust_seaorm" }

// ---------------------------------------------------------------------------
// Regex catalog
// ---------------------------------------------------------------------------

var (
	// #[derive(...DeriveEntityModel...)]
	reSeaOrmEntityDerive = regexp.MustCompile(
		`#\[derive\([^)]*\bDeriveEntityModel\b[^)]*\)\]`,
	)

	// #[sea_orm(table_name = "users")]
	reSeaOrmTableName = regexp.MustCompile(
		`#\[sea_orm\([^)]*table_name\s*=\s*"([^"]+)"[^)]*\)\]`,
	)

	// Entity Model struct name (pub struct Model)
	reSeaOrmModel = regexp.MustCompile(`\bpub\s+struct\s+(\w+)`)

	// DeriveRelation enum
	reSeaOrmRelationEnum = regexp.MustCompile(
		`#\[derive\([^)]*\bDeriveRelation\b[^)]*\)\]\s*(?:pub\s+)?enum\s+(\w+)`,
	)

	// Relation variant annotations:
	// #[sea_orm(has_many = "super::post::Entity")]
	// #[sea_orm(belongs_to = "super::user::Entity", from = "Column::UserId", to = "super::user::Column::Id")]
	reSeaOrmRelationAttr = regexp.MustCompile(
		`#\[sea_orm\([^)]*\b(has_many|belongs_to|has_one)\s*=\s*"([^"]+)"[^)]*\)\]`,
	)

	// sea-orm-migration: impl MigrationTrait for MigrationName
	reSeaOrmMigration = regexp.MustCompile(
		`\bimpl\s+MigrationTrait\s+for\s+(\w+)`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *rustSeaORMExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_seaorm_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "rust" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. DeriveEntityModel struct → orm_model entity.
	//    Scan for the derive attribute, then look forward for:
	//    (a) an optional #[sea_orm(table_name = "...")] attribute,
	//    (b) the struct declaration.
	entityDeriveMatches := reSeaOrmEntityDerive.FindAllStringIndex(src, -1)
	for _, dm := range entityDeriveMatches {
		// Look ahead ~600 chars for table_name attr and struct name.
		tail := src[dm[1]:]
		if len(tail) > 600 {
			tail = tail[:600]
		}

		tableName := ""
		if tnMatch := reSeaOrmTableName.FindStringSubmatch(tail); tnMatch != nil {
			tableName = tnMatch[1]
		}

		structMatch := reSeaOrmModel.FindStringSubmatchIndex(tail)
		if structMatch == nil {
			continue
		}
		structName := tail[structMatch[2]:structMatch[3]]
		if structName == "" {
			continue
		}

		modelKey := structName
		if tableName != "" {
			modelKey = tableName
		}

		line := lineOf(src, dm[0])
		ent := makeEntity("seaorm:model:"+modelKey, "SCOPE.Component", "orm_model",
			file.Path, file.Language, line)
		setProps(&ent,
			"framework", "seaorm",
			"struct_name", structName,
			"table_name", tableName,
			"provenance", "INFERRED_FROM_SEAORM_DERIVE_ENTITY_MODEL",
		)
		add(ent)
	}

	// 2. DeriveRelation enum → parse each variant's sea_orm attribute
	//    to emit orm_relationship patterns.
	for _, m := range reSeaOrmRelationEnum.FindAllStringSubmatchIndex(src, -1) {
		enumName := src[m[2]:m[3]]

		// Find the enum body: scan forward for { ... }
		bodyStart := strings.Index(src[m[1]:], "{")
		if bodyStart < 0 {
			continue
		}
		bodyStart += m[1]
		// Find matching closing brace (shallow: assume no nested braces in enum body).
		bodyEnd := strings.Index(src[bodyStart:], "}")
		if bodyEnd < 0 {
			continue
		}
		bodyEnd += bodyStart
		body := src[bodyStart : bodyEnd+1]

		// Within the enum body, find each sea_orm relation attribute.
		for _, rm := range reSeaOrmRelationAttr.FindAllStringSubmatchIndex(body, -1) {
			relType := body[rm[2]:rm[3]]      // has_many | belongs_to | has_one
			targetEntity := body[rm[4]:rm[5]] // e.g. "super::post::Entity"
			// Extract the short entity name (last segment before ::Entity or ::Model).
			targetShort := targetEntity
			if idx := strings.LastIndex(targetEntity, "::"); idx >= 0 {
				targetShort = targetEntity[idx+2:]
			}
			name := "seaorm:relation:" + enumName + ":" + relType + ":" + targetShort
			ent := makeEntity(name, "SCOPE.Pattern", "orm_relationship",
				file.Path, file.Language, lineOf(src, bodyStart+rm[0]))
			setProps(&ent,
				"framework", "seaorm",
				"enum_name", enumName,
				"relation_type", relType,
				"target_entity", targetEntity,
				"provenance", "INFERRED_FROM_SEAORM_DERIVE_RELATION",
			)
			add(ent)
		}
	}

	// 3. impl MigrationTrait for M → migration entity
	for _, m := range reSeaOrmMigration.FindAllStringSubmatchIndex(src, -1) {
		migName := src[m[2]:m[3]]
		ent := makeEntity("seaorm:migration:"+migName, "SCOPE.Component", "migration",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "seaorm",
			"migration_name", migName,
			"provenance", "INFERRED_FROM_SEAORM_MIGRATION_TRAIT",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
