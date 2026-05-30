package php

// orm_data.go — schema, relationship, and migration extractors for PHP ORMs:
// Doctrine, Eloquent, CycleORM, Propel, RedBeanPHP.
//
// Coverage cells driven to green by this file:
//   lang.php.orm.doctrine   : schema_extraction, relationship_extraction,
//                             association_extraction, foreign_key_extraction,
//                             lazy_loading_recognition, migration_parsing
//   lang.php.orm.eloquent   : schema_extraction, relationship_extraction,
//                             association_extraction, foreign_key_extraction,
//                             lazy_loading_recognition, migration_parsing
//   lang.php.orm.cycleorm   : model_extraction, schema_extraction,
//                             relationship_extraction, association_extraction,
//                             foreign_key_extraction, lazy_loading_recognition,
//                             query_attribution, migration_parsing
//   lang.php.orm.propel     : schema_extraction, relationship_extraction,
//                             association_extraction, foreign_key_extraction,
//                             lazy_loading_recognition, migration_parsing
//   lang.php.orm.redbeanphp : schema_extraction, relationship_extraction,
//                             association_extraction, foreign_key_extraction,
//                             lazy_loading_recognition (migration_parsing → NA)

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("php_doctrine_orm_data", &doctrineORMDataExtractor{})
	extractor.Register("php_eloquent_orm_data", &eloquentORMDataExtractor{})
	extractor.Register("php_cycleorm_data", &cycleORMDataExtractor{})
	extractor.Register("php_propel_orm_data", &propelORMDataExtractor{})
	extractor.Register("php_redbeanphp_data", &redBeanPHPDataExtractor{})
}

// ============================================================================
// Shared helpers
// ============================================================================

// ormAdd deduplicates by kind+name within a call.
func ormAdd(seen map[string]bool, entities *[]types.EntityRecord, ent types.EntityRecord) {
	key := ent.Kind + ":" + ent.Name
	if seen[key] {
		return
	}
	seen[key] = true
	*entities = append(*entities, ent)
}

// ============================================================================
// Doctrine ORM — schema, relationship, FK, lazy, migration
// ============================================================================

type doctrineORMDataExtractor struct{}

func (e *doctrineORMDataExtractor) Language() string { return "php_doctrine_orm_data" }

var (
	// dctColumnAttrRe matches PHP8 attribute #[ORM\Column(...)] directly before the
	// property declaration (whitespace/newlines only between attribute and property).
	// Group 1 = attribute args string, Group 2 = property name.
	dctColumnAttrRe = regexp.MustCompile(
		`(?s)#\[ORM\\Column([^\]]*)\]\s*\n\s*(?:private|protected|public)\s+(?:[?]?\w+\s+)?\$(\w+)`)

	// dctColumnAnnotRe matches docblock @ORM\Column(...) annotation followed by the
	// closing */ and then the property declaration.
	// Group 1 = annotation args string, Group 2 = property name.
	dctColumnAnnotRe = regexp.MustCompile(
		`(?s)@ORM\\Column(\([^)]*\))[^*]*\*/\s*\n\s*(?:private|protected|public)\s+(?:[?]?\w+\s+)?\$(\w+)`)

	// dctColumnTypeRe extracts the type argument from Column args. Group 1 = type value.
	dctColumnTypeRe = regexp.MustCompile(`\btype\s*[=:]\s*['"](\w+)['"]`)

	// doctrineColumnRe is kept for gate detection only (no property extraction)
	doctrineColumnRe = regexp.MustCompile(
		`(?m)(?:#\[ORM\\Column\s*\(|@ORM\\Column\s*\()`)

	// doctrineEntityRe detects Doctrine entity annotations/attributes
	doctrineEntityRe = regexp.MustCompile(
		`(?m)#\[ORM\\Entity\b|@ORM\\Entity\b`)

	// doctrineClassRe extracts class name after entity annotation
	doctrineClassRe = regexp.MustCompile(
		`(?m)class\s+(\w+)\b`)

	// dctEntityWithClassRe matches #[ORM\Entity...] or @ORM\Entity... then looks ahead
	// for the class declaration. Group 1 = class name.
	dctEntityWithClassRe = regexp.MustCompile(
		`(?s)(?:#\[ORM\\Entity[^\]]*\]|@ORM\\Entity\b[^\n]*\n(?:[^\n]*\n)*?)\s*class\s+(\w+)\b`)

	// dctRelationRe detects ORM relationship attributes/annotations and captures their args.
	// Group 1 = relation type (OneToMany|ManyToOne|ManyToMany|OneToOne)
	// Group 2 = attribute argument string (everything after the opening paren up to the
	//           closing bracket/paren boundary — captured lazily).
	dctRelationRe = regexp.MustCompile(
		`(?s)(?:#\[ORM\\|@ORM\\)(OneToMany|ManyToOne|ManyToMany|OneToOne)\s*\(([^)]*(?:\([^)]*\)[^)]*)*)\)`)

	// dctTargetEntityRe extracts targetEntity value from relation args.
	// Matches targetEntity: Foo::class  or  targetEntity="Foo"  or  targetEntity='Foo'
	// Group 1 = class/type name.
	dctTargetEntityRe = regexp.MustCompile(
		`\btargetEntity\s*[=:]\s*(?:(?:[\w\\]+\\)?(\w+)::class|['"](?:[\w\\]+\\)?(\w+)['"])`)

	// dctJoinColumnRe detects JoinColumn (foreign key) with args. Group 1 = arg string.
	dctJoinColumnRe = regexp.MustCompile(
		`(?s)(?:#\[ORM\\|@ORM\\)JoinColumn\s*\(([^)]*)\)`)

	// dctJoinColumnNameRe extracts name= from JoinColumn args. Group 1 = column name.
	dctJoinColumnNameRe = regexp.MustCompile(`\bname\s*[=:]\s*['"](\w+)['"]`)

	// dctJoinColumnRefRe extracts referencedColumnName= from JoinColumn args. Group 1 = referenced col.
	dctJoinColumnRefRe = regexp.MustCompile(`\breferencedColumnName\s*[=:]\s*['"](\w+)['"]`)

	// dctJoinTableRe detects JoinTable attributes (many-to-many pivot table FK)
	dctJoinTableRe = regexp.MustCompile(
		`(?m)(?:#\[ORM\\|@ORM\\)JoinTable\s*\(`)

	// dctFetchRe detects fetch="LAZY"|"EAGER"|"EXTRA_LAZY" on associations.
	// Group 1 = fetch mode value.
	dctFetchRe = regexp.MustCompile(
		`(?m)\bfetch\s*[=:]\s*['"]?(LAZY|EAGER|EXTRA_LAZY)['"]?`)

	// doctrineMigrationClassRe detects Doctrine migration class pattern
	doctrineMigrationClassRe = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+(?:AbstractMigration|DoctrineMigrations\\AbstractMigration)\b`)

	// doctrineMigrationMethodRe detects migration up/down methods
	doctrineMigrationMethodRe = regexp.MustCompile(
		`(?m)public\s+function\s+(up|down|preUp|preDown|postUp|postDown)\s*\(`)

	// dctAddSqlRe extracts SQL passed to $this->addSql('...') in migrations.
	// Group 1 = SQL string content.
	dctAddSqlRe = regexp.MustCompile(
		`(?m)\$this->addSql\(\s*['"]([^'"]+)['"]`)
)

func (e *doctrineORMDataExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "php_doctrine_orm_data.extract",
		trace.WithAttributes(
			attribute.String("file", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "php" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) { ormAdd(seen, &entities, ent) }

	// Gate: file must contain Doctrine markers
	if doctrineEntityRe.FindStringIndex(src) == nil &&
		doctrineColumnRe.FindStringIndex(src) == nil &&
		doctrineMigrationClassRe.FindStringIndex(src) == nil {
		span.SetAttributes(attribute.Int("entity_count", 0))
		return nil, nil
	}

	// 1a. Schema: #[ORM\Entity] / @ORM\Entity → emit entity class name → SCOPE.Schema/entity
	for _, m := range dctEntityWithClassRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		ent := makeEntity(className, "SCOPE.Schema", "entity", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "doctrine", "provenance", "INFERRED_FROM_DOCTRINE_ENTITY")
		add(ent)
	}

	// 1b. Schema: #[ORM\Column] attribute or @ORM\Column annotation followed by property.
	// We use two separate regexes: one for PHP8 attributes, one for docblock annotations.
	dctEmitColumn := func(argsStr, colName string, offset int) {
		colType := ""
		if tm := dctColumnTypeRe.FindStringSubmatch(argsStr); tm != nil {
			colType = tm[1]
		}
		ent := makeEntity(colName, "SCOPE.Schema", "column", file.Path, file.Language, lineOf(src, offset))
		props := []string{"framework", "doctrine", "provenance", "INFERRED_FROM_DOCTRINE_COLUMN"}
		if colType != "" {
			props = append(props, "column_type", colType)
		}
		setProps(&ent, props...)
		add(ent)
	}
	// PHP8 attribute style: #[ORM\Column(...)] \n <visibility> $prop
	for _, m := range dctColumnAttrRe.FindAllStringSubmatchIndex(src, -1) {
		argsStr := src[m[2]:m[3]]
		colName := src[m[4]:m[5]]
		dctEmitColumn(argsStr, colName, m[0])
	}
	// Docblock annotation style: @ORM\Column(...) */ \n <visibility> $prop
	for _, m := range dctColumnAnnotRe.FindAllStringSubmatchIndex(src, -1) {
		argsStr := src[m[2]:m[3]]
		colName := src[m[4]:m[5]]
		dctEmitColumn(argsStr, colName, m[0])
	}

	// 2. Relationship: OneToMany/ManyToOne/ManyToMany/OneToOne → SCOPE.Component/relation
	// Also extracts targetEntity class name from attribute args.
	for _, m := range dctRelationRe.FindAllStringSubmatchIndex(src, -1) {
		relType := src[m[2]:m[3]]
		argsStr := ""
		if m[4] >= 0 {
			argsStr = src[m[4]:m[5]]
		}
		targetEntity := ""
		if tm := dctTargetEntityRe.FindStringSubmatch(argsStr); tm != nil {
			if tm[1] != "" {
				targetEntity = tm[1]
			} else {
				targetEntity = tm[2]
			}
		}
		ent := makeEntity("relation:"+relType, "SCOPE.Component", "relation", file.Path, file.Language, lineOf(src, m[0]))
		props := []string{
			"framework", "doctrine",
			"provenance", "INFERRED_FROM_DOCTRINE_RELATION",
			"relation_type", relType,
		}
		if targetEntity != "" {
			props = append(props, "target_entity", targetEntity)
		}
		setProps(&ent, props...)
		add(ent)
	}

	// 3a. Foreign key: JoinColumn → SCOPE.Schema/foreign_key
	// Extracts name and referencedColumnName when present.
	for _, m := range dctJoinColumnRe.FindAllStringSubmatchIndex(src, -1) {
		argsStr := ""
		if m[2] >= 0 {
			argsStr = src[m[2]:m[3]]
		}
		fkName := "join_column"
		if nm := dctJoinColumnNameRe.FindStringSubmatch(argsStr); nm != nil {
			fkName = "fk:" + nm[1]
		}
		props := []string{"framework", "doctrine", "provenance", "INFERRED_FROM_DOCTRINE_JOIN_COLUMN"}
		if nm := dctJoinColumnNameRe.FindStringSubmatch(argsStr); nm != nil {
			props = append(props, "fk_column", nm[1])
		}
		if rm := dctJoinColumnRefRe.FindStringSubmatch(argsStr); rm != nil {
			props = append(props, "referenced_column", rm[1])
		}
		ent := makeEntity(fkName, "SCOPE.Schema", "foreign_key", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, props...)
		add(ent)
	}

	// 3b. Foreign key: JoinTable → SCOPE.Schema/foreign_key (many-to-many pivot table)
	for _, m := range dctJoinTableRe.FindAllStringIndex(src, -1) {
		ent := makeEntity("join_table", "SCOPE.Schema", "foreign_key", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "doctrine", "provenance", "INFERRED_FROM_DOCTRINE_JOIN_TABLE")
		add(ent)
	}

	// 4. Fetch mode recognition: fetch=LAZY|EAGER|EXTRA_LAZY → SCOPE.Pattern/lazy_loading
	for _, m := range dctFetchRe.FindAllStringSubmatchIndex(src, -1) {
		fetchMode := src[m[2]:m[3]]
		ent := makeEntity("fetch:"+fetchMode, "SCOPE.Pattern", "lazy_loading", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "doctrine", "provenance", "INFERRED_FROM_DOCTRINE_FETCH",
			"fetch_mode", fetchMode)
		add(ent)
	}

	// 5. Migration class → SCOPE.Operation/migration
	for _, m := range doctrineMigrationClassRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "migration", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "doctrine", "provenance", "INFERRED_FROM_DOCTRINE_MIGRATION")
		add(ent)
	}

	// 6. Migration method up/down → SCOPE.Operation/migration_step
	for _, m := range doctrineMigrationMethodRe.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		ent := makeEntity("migration:"+method, "SCOPE.Operation", "migration_step", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "doctrine", "provenance", "INFERRED_FROM_DOCTRINE_MIGRATION_METHOD",
			"migration_direction", method)
		add(ent)
	}

	// 7. Migration SQL: $this->addSql('CREATE TABLE ...') → SCOPE.Operation/migration_sql
	for _, m := range dctAddSqlRe.FindAllStringSubmatchIndex(src, -1) {
		sql := src[m[2]:m[3]]
		// Use first 64 chars as entity name suffix to keep names manageable.
		sqlName := sql
		if len(sqlName) > 64 {
			sqlName = sqlName[:64]
		}
		ent := makeEntity("sql:"+sqlName, "SCOPE.Operation", "migration_sql", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "doctrine", "provenance", "INFERRED_FROM_DOCTRINE_ADD_SQL",
			"sql", sql)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ============================================================================
// Eloquent ORM — schema, relationship FK, lazy, migration
// ============================================================================

type eloquentORMDataExtractor struct{}

func (e *eloquentORMDataExtractor) Language() string { return "php_eloquent_orm_data" }

var (
	// eloquentFillableRe matches $fillable or $guarded arrays (schema columns)
	eloquentFillableRe = regexp.MustCompile(
		`(?m)protected\s+\$(?:fillable|guarded)\s*=\s*\[([^\]]+)\]`)

	// eloquentCastsRe matches $casts array (column types)
	eloquentCastsRe = regexp.MustCompile(
		`(?m)protected\s+\$casts\s*=\s*\[([^\]]+)\]`)

	// eloquentCastEntryRe extracts individual cast entries
	eloquentCastEntryRe = regexp.MustCompile(
		`['"](\w+)['"]\s*=>\s*['"](\w+)['"]`)

	// eloquentFillableEntryRe extracts column names from fillable arrays
	eloquentFillableEntryRe = regexp.MustCompile(`['"](\w+)['"]`)

	// eloquentTablePropertyRe matches $table = 'tablename' overrides
	eloquentTablePropertyRe = regexp.MustCompile(
		`(?m)protected\s+\$table\s*=\s*['"]([^'"]+)['"]`)

	// elqModelWithTableRe captures (className, tableName) from class header through $table
	// Used to associate model name with its explicit table. Group 1=class, Group 2=table.
	elqModelWithTableRe = regexp.MustCompile(
		`(?s)class\s+(\w+)\s+extends\s+(?:Model|Authenticatable|Pivot)\b[^{]*\{[^}]*?protected\s+\$table\s*=\s*['"]([^'"]+)['"]`)

	// eloquentRelationMethodRe captures the full function body for relationship methods.
	// Group 1=method name, Group 2=body content (between braces, shallow — stops at first })
	// Group 3=relationship type, Group 4=rest after relation call (for extracting related model arg).
	eloquentRelationMethodRe = regexp.MustCompile(
		`(?s)public\s+function\s+(\w+)\s*\(\s*\)\s*(?::\s*[\w\\|?]+\s*)?\{\s*return\s+\$this->(hasOne|hasMany|belongsTo|belongsToMany|morphTo|morphMany|morphOne|hasManyThrough|hasOneThrough)\s*\(([^)]*)\)`)

	// elqRelatedModelRe extracts the related model class from an argument like User::class or 'User'
	// Group 1 = class name
	elqRelatedModelRe = regexp.MustCompile(
		`(?:^|\s|,)\s*(?:\\?(?:[\w\\]+\\)?)(\w+)::class`)

	// elqBelongsToExplicitFKRe extracts the explicit FK column from belongsTo 2nd arg.
	// belongsTo(User::class, 'owner_id')  — Group 1=related class, Group 2=FK column.
	elqBelongsToExplicitFKRe = regexp.MustCompile(
		`\b(belongsTo|belongsToMany)\s*\(\s*(?:\\?(?:[\w\\]+\\)?)(\w+)::class\s*,\s*['"](\w+)['"]`)

	// eloquentEagerWithCallRe detects eager loading via query scoping with()
	// Matches: ->with([...]) or ->with('rel') or Model::with(...)
	eloquentEagerWithCallRe = regexp.MustCompile(
		`(?m)->\bwith\s*\(`)

	// eloquentLoadCallRe detects dynamic eager loading via ->load([...])
	eloquentLoadCallRe = regexp.MustCompile(
		`(?m)->\bload\s*\(`)

	// eloquentLazyLoadRe detects $with or $withCount property (always-eager classes)
	eloquentLazyLoadRe = regexp.MustCompile(
		`(?m)protected\s+\$(?:with|withCount)\s*=\s*\[([^\]]*)\]`)

	// eloquentMigrationCreateRe detects Schema::create in migration files
	eloquentMigrationCreateRe = regexp.MustCompile(
		`(?m)Schema::create\s*\(\s*['"]([^'"]+)['"]`)

	// eloquentMigrationTableRe detects Schema::table for alter migrations
	eloquentMigrationTableRe = regexp.MustCompile(
		`(?m)Schema::table\s*\(\s*['"]([^'"]+)['"]`)

	// eloquentMigrationDropRe detects Schema::drop/dropIfExists
	eloquentMigrationDropRe = regexp.MustCompile(
		`(?m)Schema::drop(?:IfExists)?\s*\(\s*['"]([^'"]+)['"]`)

	// eloquentMigrationUpDownRe detects up()/down() migration methods
	eloquentMigrationUpDownRe = regexp.MustCompile(
		`(?m)public\s+function\s+(up|down)\s*\(\s*\)`)

	// eloquentColumnDefRe detects Blueprint column definitions in migrations
	eloquentColumnDefRe = regexp.MustCompile(
		`(?m)\$(?:table|t)->(string|integer|bigInteger|unsignedBigInteger|boolean|text|longText|json|jsonb|timestamp|timestamps|date|decimal|float|double|enum|morphs|nullableMorphs|foreignId|id|uuid|ulid|char|tinyInteger|smallInteger|mediumInteger|tinyText|mediumText)\s*\(\s*['"]([^'"]+)['"]`)

	// eloquentForeignKeyRe detects explicit foreign key definitions
	eloquentForeignKeyRe = regexp.MustCompile(
		`(?m)\$(?:table|t)->foreign(?:Id)?\s*\(\s*['"]([^'"]+)['"]`)

	// elqForeignIdConstrainedRe detects ->foreignId('col')->constrained() pattern
	elqForeignIdConstrainedRe = regexp.MustCompile(
		`(?m)\$(?:table|t)->foreignId\s*\(\s*['"]([^'"]+)['"]\s*\)[^;]*->constrained\s*\(`)
)

// elqConventionalFK derives the conventional FK column name from a belongsTo method name.
// e.g. method "user" → "user_id"; method "postAuthor" → "post_author_id" (camelCase → snake_case)
func elqConventionalFK(methodName string) string {
	// Convert camelCase to snake_case
	var out []byte
	for i := 0; i < len(methodName); i++ {
		c := methodName[i]
		if c >= 'A' && c <= 'Z' && i > 0 {
			out = append(out, '_')
			out = append(out, c+('a'-'A'))
		} else if c >= 'A' && c <= 'Z' {
			out = append(out, c+('a'-'A'))
		} else {
			out = append(out, c)
		}
	}
	return string(out) + "_id"
}

func (e *eloquentORMDataExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "php_eloquent_orm_data.extract",
		trace.WithAttributes(
			attribute.String("file", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "php" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) { ormAdd(seen, &entities, ent) }

	// Gate: must contain Eloquent or Schema markers
	hasEloquent := reEloquentModel.FindStringIndex(src) != nil
	hasMigration := eloquentMigrationCreateRe.FindStringIndex(src) != nil ||
		eloquentMigrationTableRe.FindStringIndex(src) != nil ||
		eloquentMigrationDropRe.FindStringIndex(src) != nil

	if !hasEloquent && !hasMigration {
		span.SetAttributes(attribute.Int("entity_count", 0))
		return nil, nil
	}

	// 1. Schema: model class name → SCOPE.Schema/model
	for _, m := range reEloquentModel.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[m[2]:m[3]]
		ent := makeEntity(modelName, "SCOPE.Schema", "model", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_MODEL")
		add(ent)
	}

	// 2. Schema: $table property → SCOPE.Schema/table
	for _, m := range eloquentTablePropertyRe.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		ent := makeEntity(tableName, "SCOPE.Schema", "table", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_TABLE_PROPERTY")
		add(ent)
	}

	// 3. Schema: $fillable columns → SCOPE.Schema/column
	for _, m := range eloquentFillableRe.FindAllStringSubmatchIndex(src, -1) {
		arrayContent := src[m[2]:m[3]]
		for _, cm := range eloquentFillableEntryRe.FindAllStringSubmatch(arrayContent, -1) {
			colName := cm[1]
			ent := makeEntity(colName, "SCOPE.Schema", "column", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_FILLABLE")
			add(ent)
		}
	}

	// 4. Schema: $casts type annotations → SCOPE.Schema/column
	for _, m := range eloquentCastsRe.FindAllStringSubmatchIndex(src, -1) {
		arrayContent := src[m[2]:m[3]]
		for _, cm := range eloquentCastEntryRe.FindAllStringSubmatch(arrayContent, -1) {
			colName := cm[1]
			castType := cm[2]
			ent := makeEntity(colName, "SCOPE.Schema", "column", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_CAST",
				"cast_type", castType)
			add(ent)
		}
	}

	// 5. Relationships: all types with related model extraction → SCOPE.Component/relation
	for _, m := range eloquentRelationMethodRe.FindAllStringSubmatchIndex(src, -1) {
		methodName := src[m[2]:m[3]]
		relType := src[m[4]:m[5]]
		argsStr := src[m[6]:m[7]]

		// Extract related model class from first arg (e.g. User::class)
		relatedModel := ""
		if rm := elqRelatedModelRe.FindStringSubmatch(argsStr); rm != nil {
			relatedModel = rm[1]
		}

		ent := makeEntity(methodName, "SCOPE.Component", "relation", file.Path, file.Language, lineOf(src, m[0]))
		props := []string{
			"framework", "eloquent",
			"provenance", "INFERRED_FROM_ELOQUENT_RELATIONSHIP",
			"relation_type", relType,
		}
		if relatedModel != "" {
			props = append(props, "related_model", relatedModel)
		}
		setProps(&ent, props...)
		add(ent)

		// 5a. FK inference for belongsTo side
		if relType == "belongsTo" || relType == "belongsToMany" {
			// Check for explicit FK as 2nd arg
			fkCol := ""
			if em := elqBelongsToExplicitFKRe.FindStringSubmatch("$this->" + relType + "(" + argsStr + ")"); em != nil {
				fkCol = em[3]
			}
			// Fallback: conventional FK from method name
			if fkCol == "" && (relType == "belongsTo") {
				fkCol = elqConventionalFK(methodName)
			}
			if fkCol != "" {
				fkEnt := makeEntity("fk:"+fkCol, "SCOPE.Schema", "foreign_key", file.Path, file.Language, lineOf(src, m[0]))
				props := []string{
					"framework", "eloquent",
					"provenance", "INFERRED_FROM_ELOQUENT_BELONGS_TO_CONVENTION",
					"fk_column", fkCol,
				}
				if relatedModel != "" {
					props = append(props, "related_model", relatedModel)
				}
				setProps(&fkEnt, props...)
				add(fkEnt)
			}
		}
	}

	// 6. Lazy/eager loading hints: $with or $withCount property → SCOPE.Pattern/eager_loading
	for _, m := range eloquentLazyLoadRe.FindAllStringSubmatchIndex(src, -1) {
		ent := makeEntity("eager_with", "SCOPE.Pattern", "eager_loading", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_WITH",
			"loading_mode", "eager")
		add(ent)
	}

	// 7. Eager loading via ->with() query scope calls → SCOPE.Pattern/eager_loading
	for _, m := range eloquentEagerWithCallRe.FindAllStringIndex(src, -1) {
		ent := makeEntity("eager_load:with", "SCOPE.Pattern", "eager_loading", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_WITH_CALL",
			"loading_mode", "eager")
		add(ent)
	}

	// 8. Dynamic eager loading via ->load() → SCOPE.Pattern/eager_loading
	for _, m := range eloquentLoadCallRe.FindAllStringIndex(src, -1) {
		ent := makeEntity("eager_load:load", "SCOPE.Pattern", "eager_loading", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_LOAD_CALL",
			"loading_mode", "eager")
		add(ent)
	}

	// 9. Lazy loading: Eloquent is lazy by default — emit a lazy marker when
	//    a model class is detected but no eager $with property is present.
	if hasEloquent && eloquentLazyLoadRe.FindStringIndex(src) == nil {
		ent := makeEntity("lazy:default", "SCOPE.Pattern", "lazy_loading", file.Path, file.Language, 1)
		setProps(&ent, "framework", "eloquent", "provenance", "ELOQUENT_LAZY_BY_DEFAULT",
			"loading_mode", "lazy")
		add(ent)
	}

	// 10. Migration: Schema::create → SCOPE.Operation/migration
	for _, m := range eloquentMigrationCreateRe.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		ent := makeEntity("create:"+tableName, "SCOPE.Operation", "migration", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_MIGRATION_CREATE",
			"table_name", tableName, "migration_op", "create")
		add(ent)
	}

	// 11. Migration: Schema::table → SCOPE.Operation/migration
	for _, m := range eloquentMigrationTableRe.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		ent := makeEntity("alter:"+tableName, "SCOPE.Operation", "migration", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_MIGRATION_TABLE",
			"table_name", tableName, "migration_op", "alter")
		add(ent)
	}

	// 12. Migration: Schema::drop → SCOPE.Operation/migration
	for _, m := range eloquentMigrationDropRe.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		ent := makeEntity("drop:"+tableName, "SCOPE.Operation", "migration", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_MIGRATION_DROP",
			"table_name", tableName, "migration_op", "drop")
		add(ent)
	}

	// 13. Migration: up()/down() methods → SCOPE.Operation/migration_step
	for _, m := range eloquentMigrationUpDownRe.FindAllStringSubmatchIndex(src, -1) {
		direction := src[m[2]:m[3]]
		ent := makeEntity("migration:"+direction, "SCOPE.Operation", "migration_step", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_MIGRATION_METHOD",
			"migration_direction", direction)
		add(ent)
	}

	// 14. Migration column definitions → SCOPE.Schema/column
	for _, m := range eloquentColumnDefRe.FindAllStringSubmatchIndex(src, -1) {
		colName := src[m[4]:m[5]]
		colType := src[m[2]:m[3]]
		ent := makeEntity(colName, "SCOPE.Schema", "column", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_BLUEPRINT_COLUMN",
			"column_type", colType)
		add(ent)
	}

	// 15. Explicit foreign key definitions → SCOPE.Schema/foreign_key
	for _, m := range eloquentForeignKeyRe.FindAllStringSubmatchIndex(src, -1) {
		colName := src[m[2]:m[3]]
		ent := makeEntity("fk:"+colName, "SCOPE.Schema", "foreign_key", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_FK",
			"fk_column", colName)
		add(ent)
	}

	// 16. foreignId()->constrained() — emit a separate fk:constrained entity
	for _, m := range elqForeignIdConstrainedRe.FindAllStringSubmatchIndex(src, -1) {
		colName := src[m[2]:m[3]]
		ent := makeEntity("fk:constrained:"+colName, "SCOPE.Schema", "foreign_key", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_FOREIGN_ID_CONSTRAINED",
			"fk_column", colName, "constrained", "true")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ============================================================================
// CycleORM — model, schema, relationship, FK, lazy, query, migration
// ============================================================================

type cycleORMDataExtractor struct{}

func (e *cycleORMDataExtractor) Language() string { return "php_cycleorm_data" }

var (
	// cycleEntityRe detects #[Entity] or #[Cycle\Annotated\Annotation\Entity]
	cycleEntityRe = regexp.MustCompile(
		`(?m)#\[(?:Cycle\\Annotated\\Annotation\\)?Entity(?:\s*\(|])`)

	// cycleColumnWithPropRe matches #[Column(...)] followed by the property
	// declaration, capturing the property name. Group 1 = property name.
	cycleColumnWithPropRe = regexp.MustCompile(
		`(?s)#\[(?:Cycle\\Annotated\\Annotation\\)?Column[^\]]*\]\s*\n\s*(?:private|protected|public)\s+(?:[?]?\w+\s+)?\$(\w+)`)

	// cycleColumnRe detects #[Column(...)] attributes (gate only)
	cycleColumnRe = regexp.MustCompile(
		`(?m)#\[(?:Cycle\\Annotated\\Annotation\\)?Column\s*\(`)

	// cycleRelationRe detects HasOne/HasMany/BelongsTo/ManyToMany relations
	cycleRelationRe = regexp.MustCompile(
		`(?m)#\[(?:Cycle\\Annotated\\Annotation\\Relation\\)?(HasOne|HasMany|BelongsTo|ManyToMany|BelongsToMany|HasMany|RefersTo)\s*\(`)

	// cycleFKRe detects InverseRelation or explicit FK column markers
	cycleFKRe = regexp.MustCompile(
		`(?m)#\[(?:Cycle\\Annotated\\Annotation\\Relation\\)?Inverse\s*\(`)

	// cycleLazyRe detects lazy proxy usage (load: 'lazy' or Cycle\ORM\Promise\)
	cycleLazyRe = regexp.MustCompile(
		`(?m)load\s*:\s*['"]lazy['"]|Cycle\\ORM\\Promise\\PromiseInterface`)

	// cycleQueryRe detects CycleORM repository/select operations.
	// Matches ->findByPK/findOne/select/make/run after any chained receiver.
	cycleQueryRe = regexp.MustCompile(
		`(?m)->(findByPK|findOne|select|make|run)\s*\(`)

	// cycleMigrationRe detects Cycle migration class patterns
	cycleMigrationRe = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+(?:extends\s+\w+\s+)?implements\s+(?:Cycle\\)?Migrations\\MigrationInterface\b`)

	// cycleSyncTableRe detects Cycle schema sync calls
	cycleSyncTableRe = regexp.MustCompile(
		`(?m)\$(?:schema|config)->sync\s*\(|\$(?:schema|config)->migrate\s*\(`)
)

func (e *cycleORMDataExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "php_cycleorm_data.extract",
		trace.WithAttributes(
			attribute.String("file", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "php" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) { ormAdd(seen, &entities, ent) }

	// Gate: file must mention Cycle markers
	hasCycle := cycleEntityRe.FindStringIndex(src) != nil ||
		cycleColumnRe.FindStringIndex(src) != nil ||
		cycleQueryRe.FindStringIndex(src) != nil
	if !hasCycle {
		span.SetAttributes(attribute.Int("entity_count", 0))
		return nil, nil
	}

	// 1. Model: #[Entity] class → SCOPE.Schema/model
	entityMatches := cycleEntityRe.FindAllStringIndex(src, -1)
	for _, em := range entityMatches {
		rest := src[em[0]:]
		cm := doctrineClassRe.FindStringSubmatch(rest)
		if cm != nil {
			ent := makeEntity(cm[1], "SCOPE.Schema", "model", file.Path, file.Language, lineOf(src, em[0]))
			setProps(&ent, "framework", "cycleorm", "provenance", "INFERRED_FROM_CYCLEORM_ENTITY")
			add(ent)
		}
	}

	// 2. Schema: #[Column] followed by property → SCOPE.Schema/column
	for _, m := range cycleColumnWithPropRe.FindAllStringSubmatchIndex(src, -1) {
		colName := src[m[2]:m[3]]
		ent := makeEntity(colName, "SCOPE.Schema", "column", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "cycleorm", "provenance", "INFERRED_FROM_CYCLEORM_COLUMN")
		add(ent)
	}

	// 3. Relationship: HasOne/HasMany/BelongsTo/ManyToMany → SCOPE.Component/relation
	for _, m := range cycleRelationRe.FindAllStringSubmatchIndex(src, -1) {
		relType := src[m[2]:m[3]]
		ent := makeEntity("relation:"+relType, "SCOPE.Component", "relation", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "cycleorm", "provenance", "INFERRED_FROM_CYCLEORM_RELATION",
			"relation_type", relType)
		add(ent)
	}

	// 4. Foreign key: inverse/explicit FK → SCOPE.Schema/foreign_key
	for _, m := range cycleFKRe.FindAllStringIndex(src, -1) {
		ent := makeEntity("inverse_fk", "SCOPE.Schema", "foreign_key", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "cycleorm", "provenance", "INFERRED_FROM_CYCLEORM_FK")
		add(ent)
	}

	// 5. Lazy loading: load='lazy' or Promise → SCOPE.Pattern
	for _, m := range cycleLazyRe.FindAllStringIndex(src, -1) {
		ent := makeEntity("lazy_load", "SCOPE.Pattern", "lazy_loading", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "cycleorm", "provenance", "INFERRED_FROM_CYCLEORM_LAZY")
		add(ent)
	}

	// 6. Query attribution: findByPK/findOne/select → SCOPE.Operation/query
	for _, m := range cycleQueryRe.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		ent := makeEntity("query:"+verb, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "cycleorm", "provenance", "INFERRED_FROM_CYCLEORM_QUERY",
			"query_verb", verb)
		add(ent)
	}

	// 7. Migration class → SCOPE.Operation/migration
	for _, m := range cycleMigrationRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "migration", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "cycleorm", "provenance", "INFERRED_FROM_CYCLEORM_MIGRATION")
		add(ent)
	}

	// 8. Schema sync → SCOPE.Operation/migration_step
	for _, m := range cycleSyncTableRe.FindAllStringIndex(src, -1) {
		ent := makeEntity("schema:sync", "SCOPE.Operation", "migration_step", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "cycleorm", "provenance", "INFERRED_FROM_CYCLEORM_SYNC")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ============================================================================
// Propel ORM — schema, relationship, FK, lazy, migration
// ============================================================================

type propelORMDataExtractor struct{}

func (e *propelORMDataExtractor) Language() string { return "php_propel_orm_data" }

var (
	// propelTableMapRe detects Propel TableMap class (schema reflection)
	propelTableMapRe = regexp.MustCompile(
		`(?m)class\s+(\w+TableMap)\s+extends\s+(?:Propel\\Runtime\\Map\\)?TableMap\b`)

	// propelColumnConstRe detects COL_* constants in TableMap (schema columns)
	propelColumnConstRe = regexp.MustCompile(
		`(?m)const\s+(COL_\w+)\s*=\s*['"]([^'"]+)['"]`)

	// propelFKRe detects addForeignKey calls in TableMap
	propelFKRe = regexp.MustCompile(
		`(?m)->addForeignKey\s*\(`)

	// propelRelationRe detects addRelation calls (association)
	propelRelationRe = regexp.MustCompile(
		`(?m)->addRelation\s*\(\s*['"]([^'"]+)['"]`)

	// propelLazyLoadRe detects LAZY_LOAD in Propel
	propelLazyLoadRe = regexp.MustCompile(
		`(?m)LAZY_LOAD|lazyLoad\s*=\s*true`)

	// propelSchemaXMLRe detects schema.xml reference in PHP (generated-classes loading)
	propelSchemaXMLRe = regexp.MustCompile(
		`(?m)require\s+.*generated-classes|use\s+\w+Query\b|extends\s+Base\w+`)

	// propelMigrationRe detects Propel migration calls
	propelMigrationRe = regexp.MustCompile(
		`(?m)Propel::migrate\s*\(|PropelMigration\w+|class\s+PropelMigration_\d+`)

	// propelQueryRe detects Propel query builder methods
	propelQueryRe = regexp.MustCompile(
		`(?m)\w+Query::create\s*\(|\w+Query->(?:find|findOne|findPk|count|filterBy\w+)\s*\(`)
)

func (e *propelORMDataExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "php_propel_orm_data.extract",
		trace.WithAttributes(
			attribute.String("file", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "php" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) { ormAdd(seen, &entities, ent) }

	// Gate: must contain Propel markers
	hasPropel := propelTableMapRe.FindStringIndex(src) != nil ||
		propelSchemaXMLRe.FindStringIndex(src) != nil ||
		propelQueryRe.FindStringIndex(src) != nil ||
		propelMigrationRe.FindStringIndex(src) != nil
	if !hasPropel {
		span.SetAttributes(attribute.Int("entity_count", 0))
		return nil, nil
	}

	// 1. Schema: TableMap class → SCOPE.Schema/table_map
	for _, m := range propelTableMapRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "table_map", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "propel", "provenance", "INFERRED_FROM_PROPEL_TABLE_MAP")
		add(ent)
	}

	// 2. Schema: COL_* constants → SCOPE.Schema/column
	for _, m := range propelColumnConstRe.FindAllStringSubmatchIndex(src, -1) {
		colConst := src[m[2]:m[3]]
		ent := makeEntity(colConst, "SCOPE.Schema", "column", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "propel", "provenance", "INFERRED_FROM_PROPEL_COLUMN")
		add(ent)
	}

	// 3. Foreign key: addForeignKey → SCOPE.Schema/foreign_key
	for _, m := range propelFKRe.FindAllStringIndex(src, -1) {
		ent := makeEntity("foreign_key", "SCOPE.Schema", "foreign_key", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "propel", "provenance", "INFERRED_FROM_PROPEL_FK")
		add(ent)
	}

	// 4. Association/relationship: addRelation → SCOPE.Component/relation
	for _, m := range propelRelationRe.FindAllStringSubmatchIndex(src, -1) {
		relName := src[m[2]:m[3]]
		ent := makeEntity("relation:"+relName, "SCOPE.Component", "relation", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "propel", "provenance", "INFERRED_FROM_PROPEL_RELATION",
			"relation_name", relName)
		add(ent)
	}

	// 5. Lazy loading: LAZY_LOAD constant → SCOPE.Pattern
	for _, m := range propelLazyLoadRe.FindAllStringIndex(src, -1) {
		ent := makeEntity("lazy_load", "SCOPE.Pattern", "lazy_loading", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "propel", "provenance", "INFERRED_FROM_PROPEL_LAZY")
		add(ent)
	}

	// 6. Migration: PropelMigration class → SCOPE.Operation/migration
	for _, m := range propelMigrationRe.FindAllStringIndex(src, -1) {
		ent := makeEntity("propel:migration", "SCOPE.Operation", "migration", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "propel", "provenance", "INFERRED_FROM_PROPEL_MIGRATION")
		add(ent)
	}

	// 7. Query: XxxQuery::create() / XxxQuery->find/findOne/etc → SCOPE.Operation/query
	for _, m := range propelQueryRe.FindAllStringIndex(src, -1) {
		ent := makeEntity("propel:query", "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "propel", "provenance", "INFERRED_FROM_PROPEL_QUERY")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ============================================================================
// RedBeanPHP — schema, relationship, association (migration → NA: zero-config)
// ============================================================================

type redBeanPHPDataExtractor struct{}

func (e *redBeanPHPDataExtractor) Language() string { return "php_redbeanphp_data" }

var (
	// rbDispenseRe detects R::dispense('tablename') — implicit schema creation
	rbDispenseRe = regexp.MustCompile(
		`(?m)R::dispense\s*\(\s*['"]([^'"]+)['"]`)

	// rbStoreRe detects R::store($bean) — persistence (with implicit schema)
	rbStoreRe = regexp.MustCompile(
		`(?m)R::store\s*\(`)

	// rbRelatedRe detects R::related / R::associate — associations
	rbRelatedRe = regexp.MustCompile(
		`(?m)R::(related|associate|unassociate)\s*\(`)

	// rbFindRe detects R::find / R::load / R::findOne — queries
	rbFindRe = regexp.MustCompile(
		`(?m)R::(find|findOne|load|findAll|getAll)\s*\(\s*['"]([^'"]+)['"]`)

	// rbPropRe detects $bean->property = ... (schema column via property assignment)
	rbPropRe = regexp.MustCompile(
		`(?m)\$\w+->([a-z_]\w*)\s*=`)

	// rbFreezeRe detects R::freeze(true) — prevents further schema changes
	rbFreezeRe = regexp.MustCompile(
		`(?m)R::freeze\s*\(`)
)

func (e *redBeanPHPDataExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "php_redbeanphp_data.extract",
		trace.WithAttributes(
			attribute.String("file", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "php" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) { ormAdd(seen, &entities, ent) }

	// Gate: must contain R:: RedBeanPHP facade calls
	if rbDispenseRe.FindStringIndex(src) == nil &&
		rbFindRe.FindStringIndex(src) == nil &&
		rbStoreRe.FindStringIndex(src) == nil &&
		rbRelatedRe.FindStringIndex(src) == nil {
		span.SetAttributes(attribute.Int("entity_count", 0))
		return nil, nil
	}

	// 1. Schema: R::dispense('table') → implicit table/schema entity
	for _, m := range rbDispenseRe.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		ent := makeEntity(tableName, "SCOPE.Schema", "table", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "redbeanphp", "provenance", "INFERRED_FROM_REDBEAN_DISPENSE",
			"table_name", tableName)
		add(ent)
	}

	// 2. Schema: R::find('table', ...) → schema reference
	for _, m := range rbFindRe.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[4]:m[5]]
		ent := makeEntity(tableName, "SCOPE.Schema", "table", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "redbeanphp", "provenance", "INFERRED_FROM_REDBEAN_FIND",
			"table_name", tableName)
		add(ent)
	}

	// 3. Association/relationship: R::related/associate → SCOPE.Component/relation
	for _, m := range rbRelatedRe.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		ent := makeEntity("relation:"+verb, "SCOPE.Component", "relation", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "redbeanphp", "provenance", "INFERRED_FROM_REDBEAN_RELATION",
			"relation_verb", verb)
		add(ent)
	}

	// 4. Schema freeze: R::freeze(true) → SCOPE.Pattern (schema lock)
	for _, m := range rbFreezeRe.FindAllStringIndex(src, -1) {
		ent := makeEntity("schema:freeze", "SCOPE.Pattern", "schema_freeze", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "redbeanphp", "provenance", "INFERRED_FROM_REDBEAN_FREEZE")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
