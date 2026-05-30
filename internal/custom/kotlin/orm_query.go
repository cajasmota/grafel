// Package kotlin — query extractors for Kotlin ORMs.
//
// This file deepens the Queries/query_attribution capability for the Kotlin
// ORM cells that the schema/relationship extractors in orm_schema.go and the
// JPA migration extractor in jpa_compose_ext.go did not previously cover:
//
//	lang.kotlin.orm.ktorm        Queries/query_attribution
//	lang.kotlin.orm.room         Queries/query_attribution
//	lang.kotlin.orm.spring-data  Queries/query_attribution  (+ Models/schema, Relationships via repository)
//	lang.kotlin.orm.sqldelight   Queries/query_attribution
//	lang.kotlin.orm.mongodb      Queries/query_attribution, Models/model_extraction
//	lang.kotlin.orm.exposed      Queries/query_attribution (DSL select/insert/update/delete)
//
// Each extractor emits SCOPE.Operation entities with subtype="query" carrying
// the concrete query name (derived-method name, @Query SQL, named .sq query,
// or DSL operation + table) so downstream attribution can name the exact query.
//
// Naming: every helper / regex in this file is prefixed `kotlinOrmQuery` /
// `reOrmQuery` to keep the package namespace clean and avoid collisions with
// sibling Kotlin ORM PRs that edit orm_schema.go.
//
// Issue #3433 — Deep-grind Kotlin ORM extraction to the TS/JS bar. Epic #3431.
package kotlin

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
	extractor.Register("custom_kotlin_orm_query", &kotlinOrmQueryExtractor{})
}

// kotlinOrmQueryExtractor emits query-operation entities across all supported
// Kotlin ORMs. A single extractor dispatches per-ORM detection so that one
// file is scanned once regardless of which ORM idioms it contains.
type kotlinOrmQueryExtractor struct{}

func (e *kotlinOrmQueryExtractor) Language() string { return "custom_kotlin_orm_query" }

var (
	// --- Exposed DSL queries -------------------------------------------------
	// Table.select { ... } / Table.selectAll() / Table.slice(...).select(...)
	// Captures: (table, op)
	reOrmQueryExposedSelect = regexp.MustCompile(
		`(?m)\b([A-Z][A-Za-z0-9_]*)\s*\.\s*(select|selectAll|slice)\b`)
	// Table.insert { ... } / Table.batchInsert / Table.update { ... } / Table.deleteWhere { ... }
	// Captures: (table, op)
	reOrmQueryExposedWrite = regexp.MustCompile(
		`(?m)\b([A-Z][A-Za-z0-9_]*)\s*\.\s*(insertIgnore|batchInsert|insert|update|deleteWhere|deleteAll|replace)\b`)

	// --- Ktorm queries -------------------------------------------------------
	// database.from(Employees).select() / .insert(Employees) / .update(Employees) / .delete(Employees)
	// Captures: (verb, table)
	reOrmQueryKtormFrom = regexp.MustCompile(
		`(?m)\b(?:from|insert|insertAndGenerateKey|update|batchUpdate|delete|deleteAll)\s*\(\s*([A-Z][A-Za-z0-9_]*)\s*\)`)
	// Entity sequence ops: database.sequenceOf(Employees).find { ... } / .filter { ... }
	// Captures: (table)
	reOrmQueryKtormSequence = regexp.MustCompile(
		`(?m)\bsequenceOf\s*\(\s*([A-Z][A-Za-z0-9_]*)\s*\)`)

	// --- Room @Dao queries ---------------------------------------------------
	// @Query("SELECT * FROM users WHERE id = :id")  — capture the SQL text.
	reOrmQueryRoomQuery = regexp.MustCompile(
		`@Query\s*\(\s*"([^"]+)"`)
	// @Insert / @Update / @Delete / @Upsert annotated DAO methods.
	// Captures: (annotation, method_name)
	reOrmQueryRoomWrite = regexp.MustCompile(
		`(?ms)@(Insert|Update|Delete|Upsert)\b[^\n]*\n\s*(?:suspend\s+)?fun\s+([a-zA-Z_][A-Za-z0-9_]*)`)

	// --- Spring Data repositories --------------------------------------------
	// interface UserRepository : CrudRepository<User, Long> { ... }
	// Captures: (repo_name, entity_type, id_type)
	reOrmQuerySpringRepo = regexp.MustCompile(
		`(?m)\binterface\s+([A-Z][A-Za-z0-9_]*)\s*:\s*(?:[A-Za-z0-9_]*Repository)\s*<\s*([A-Z][A-Za-z0-9_]*)\s*,\s*([A-Za-z0-9_<>?]+)\s*>`)
	// Derived query methods: fun findByEmailAndStatus(...) / countByActive / existsByName / deleteByX.
	// Captures: (method_name)
	reOrmQuerySpringDerived = regexp.MustCompile(
		`(?m)\bfun\s+((?:find|read|get|query|count|exists|delete|remove|stream)(?:All)?By[A-Z][A-Za-z0-9_]*)\s*\(`)
	// @Query("...") on a repository method (JPQL / native SQL / Mongo JSON).
	reOrmQuerySpringAtQuery = regexp.MustCompile(
		`@Query\s*\(\s*(?:value\s*=\s*)?"([^"]+)"`)

	// --- SQLDelight named queries (.sq) --------------------------------------
	// selectAllUsers:
	// SELECT * FROM users;
	// Captures: (query_label)
	reOrmQuerySqlDelightLabel = regexp.MustCompile(
		`(?m)^([a-zA-Z_][A-Za-z0-9_]*)\s*:\s*$`)

	// --- MongoDB (KMongo / spring-data-mongo) --------------------------------
	// @Document / @Document("collection") model annotation.
	// Captures: (collection_name?) — optional; class detected separately.
	reOrmQueryMongoDocument = regexp.MustCompile(
		`@Document\s*(?:\(\s*(?:collection\s*=\s*)?"([^"]*)"\s*\))?`)
	// collection.find(...) / .findOne(...) / .insertOne(...) / .updateOne(...) / .deleteOne(...) / .aggregate(...)
	// KMongo idioms. Captures: (op)
	reOrmQueryMongoOp = regexp.MustCompile(
		`(?m)\.\s*(findOneById|findOne|findById|find|insertMany|insertOne|updateMany|updateOne|deleteMany|deleteOne|aggregate|countDocuments|replaceOne)\s*\(`)
	// MongoRepository<T, ID> spring-data-mongo repository.
	// Captures: (repo_name, entity_type)
	reOrmQueryMongoRepo = regexp.MustCompile(
		`(?m)\binterface\s+([A-Z][A-Za-z0-9_]*)\s*:\s*(?:Reactive)?MongoRepository\s*<\s*([A-Z][A-Za-z0-9_]*)`)
)

func (e *kotlinOrmQueryExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_orm_query.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("extractor", "orm_query"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	isSq := strings.HasSuffix(file.Path, ".sq") || strings.HasSuffix(file.Path, ".sqm")
	if file.Language != "kotlin" && !isSq {
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

	if isSq {
		kotlinOrmQueryExtractSQLDelight(src, file.Path, add)
		span.SetAttributes(attribute.Int("entity_count", len(entities)))
		return entities, nil
	}

	kotlinOrmQueryExtractExposed(src, file.Path, add)
	kotlinOrmQueryExtractKtorm(src, file.Path, add)
	kotlinOrmQueryExtractRoom(src, file.Path, add)
	kotlinOrmQueryExtractSpringData(src, file.Path, add)
	kotlinOrmQueryExtractMongo(src, file.Path, add)

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// kotlinOrmQueryEmit constructs a SCOPE.Operation/query entity.
func kotlinOrmQueryEmit(src, name, orm, path string, offset int, extra ...string) types.EntityRecord {
	line := lineOf(src, offset)
	ent := makeEntity(name, "SCOPE.Operation", "query", path, "kotlin", line)
	props := append([]string{"orm", orm}, extra...)
	setProps(&ent, props...)
	return ent
}

// kotlinOrmQueryExtractExposed handles Jetbrains Exposed DSL queries.
func kotlinOrmQueryExtractExposed(src, path string, add func(types.EntityRecord)) {
	if !strings.Contains(src, "select") && !strings.Contains(src, "insert") &&
		!strings.Contains(src, "update") && !strings.Contains(src, "delete") {
		return
	}
	for _, m := range reOrmQueryExposedSelect.FindAllStringSubmatchIndex(src, -1) {
		table := src[m[2]:m[3]]
		op := src[m[4]:m[5]]
		name := "exposed:" + table + "." + op
		add(kotlinOrmQueryEmit(src, name, "exposed", path, m[0],
			"table", table, "operation", op,
			"provenance", "INFERRED_FROM_EXPOSED_SELECT"))
	}
	for _, m := range reOrmQueryExposedWrite.FindAllStringSubmatchIndex(src, -1) {
		table := src[m[2]:m[3]]
		op := src[m[4]:m[5]]
		name := "exposed:" + table + "." + op
		add(kotlinOrmQueryEmit(src, name, "exposed", path, m[0],
			"table", table, "operation", op,
			"provenance", "INFERRED_FROM_EXPOSED_WRITE"))
	}
}

// kotlinOrmQueryExtractKtorm handles Ktorm query DSL.
func kotlinOrmQueryExtractKtorm(src, path string, add func(types.EntityRecord)) {
	if !strings.Contains(src, "ktorm") && !strings.Contains(src, "sequenceOf") &&
		!strings.Contains(src, ".from(") && !strings.Contains(src, "database.") {
		return
	}
	for _, m := range reOrmQueryKtormFrom.FindAllStringSubmatchIndex(src, -1) {
		table := src[m[2]:m[3]]
		// Derive the verb from the leading token captured by the alternation.
		verb := strings.TrimSpace(src[m[0]:m[2]])
		verb = strings.TrimRight(verb, " \t(")
		name := "ktorm:" + verb + "(" + table + ")"
		add(kotlinOrmQueryEmit(src, name, "ktorm", path, m[0],
			"table", table, "operation", verb,
			"provenance", "INFERRED_FROM_KTORM_QUERY"))
	}
	for _, m := range reOrmQueryKtormSequence.FindAllStringSubmatchIndex(src, -1) {
		table := src[m[2]:m[3]]
		name := "ktorm:sequenceOf(" + table + ")"
		add(kotlinOrmQueryEmit(src, name, "ktorm", path, m[0],
			"table", table, "operation", "sequenceOf",
			"provenance", "INFERRED_FROM_KTORM_SEQUENCE"))
	}
}

// kotlinOrmQueryExtractRoom handles Room @Dao query/write methods.
func kotlinOrmQueryExtractRoom(src, path string, add func(types.EntityRecord)) {
	if !strings.Contains(src, "@Query") && !strings.Contains(src, "@Insert") &&
		!strings.Contains(src, "@Update") && !strings.Contains(src, "@Delete") &&
		!strings.Contains(src, "@Upsert") && !strings.Contains(src, "@Dao") {
		return
	}
	// Room @Query SQL — but only when this is a Room DAO file, not a spring
	// repository (both use @Query). Room files import androidx.room or use @Dao.
	isRoom := strings.Contains(src, "androidx.room") || strings.Contains(src, "@Dao")
	if isRoom {
		for _, m := range reOrmQueryRoomQuery.FindAllStringSubmatchIndex(src, -1) {
			sql := strings.TrimSpace(src[m[2]:m[3]])
			name := "room:@Query:" + kotlinOrmQueryTruncate(sql)
			add(kotlinOrmQueryEmit(src, name, "room", path, m[0],
				"sql", kotlinOrmQueryTruncate(sql),
				"provenance", "INFERRED_FROM_ROOM_QUERY"))
		}
	}
	for _, m := range reOrmQueryRoomWrite.FindAllStringSubmatchIndex(src, -1) {
		annotation := src[m[2]:m[3]]
		method := src[m[4]:m[5]]
		name := "room:@" + annotation + ":" + method
		add(kotlinOrmQueryEmit(src, name, "room", path, m[0],
			"operation", annotation, "method", method,
			"provenance", "INFERRED_FROM_ROOM_WRITE"))
	}
}

// kotlinOrmQueryExtractSpringData handles Spring Data repositories + derived
// queries + @Query. Also emits model/relationship signal by recording the
// repository's entity type.
func kotlinOrmQueryExtractSpringData(src, path string, add func(types.EntityRecord)) {
	hasRepo := strings.Contains(src, "Repository")
	if !hasRepo {
		return
	}
	// MongoRepository is handled by the Mongo pass; skip if this is a pure
	// Mongo repository file with no relational repository.
	for _, m := range reOrmQuerySpringRepo.FindAllStringSubmatchIndex(src, -1) {
		repo := src[m[2]:m[3]]
		entity := src[m[4]:m[5]]
		idType := src[m[6]:m[7]]
		name := "spring-data:" + repo + "<" + entity + "," + idType + ">"
		add(kotlinOrmQueryEmit(src, name, "spring-data", path, m[0],
			"repository", repo, "entity", entity, "id_type", idType,
			"provenance", "INFERRED_FROM_SPRING_REPOSITORY"))
	}
	for _, m := range reOrmQuerySpringDerived.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		name := "spring-data:derived:" + method
		add(kotlinOrmQueryEmit(src, name, "spring-data", path, m[0],
			"method", method, "query_kind", "derived",
			"provenance", "INFERRED_FROM_SPRING_DERIVED_QUERY"))
	}
	// @Query only counts as spring-data when no Room DAO is present (Room is
	// already handled above and owns androidx.room files).
	if !strings.Contains(src, "androidx.room") && !strings.Contains(src, "@Dao") {
		for _, m := range reOrmQuerySpringAtQuery.FindAllStringSubmatchIndex(src, -1) {
			q := strings.TrimSpace(src[m[2]:m[3]])
			name := "spring-data:@Query:" + kotlinOrmQueryTruncate(q)
			add(kotlinOrmQueryEmit(src, name, "spring-data", path, m[0],
				"query", kotlinOrmQueryTruncate(q), "query_kind", "annotated",
				"provenance", "INFERRED_FROM_SPRING_AT_QUERY"))
		}
	}
}

// kotlinOrmQueryExtractMongo handles MongoDB (KMongo + spring-data-mongo).
func kotlinOrmQueryExtractMongo(src, path string, add func(types.EntityRecord)) {
	hasMongo := strings.Contains(src, "@Document") || strings.Contains(src, "MongoRepository") ||
		strings.Contains(src, "kmongo") || strings.Contains(src, "MongoCollection") ||
		strings.Contains(src, "getCollection")
	if !hasMongo {
		return
	}
	// @Document model declarations.
	for _, m := range reOrmQueryMongoDocument.FindAllStringSubmatchIndex(src, -1) {
		collection := ""
		if m[2] >= 0 {
			collection = src[m[2]:m[3]]
		}
		name := "mongodb:@Document"
		if collection != "" {
			name += ":" + collection
		} else {
			name += ":" + path
		}
		ent := makeEntity(name, "SCOPE.Model", "document", path, "kotlin", lineOf(src, m[0]))
		props := []string{"orm", "mongodb", "provenance", "INFERRED_FROM_MONGO_DOCUMENT"}
		if collection != "" {
			props = append(props, "collection", collection)
		}
		setProps(&ent, props...)
		add(ent)
	}
	// MongoRepository<T, ID>.
	for _, m := range reOrmQueryMongoRepo.FindAllStringSubmatchIndex(src, -1) {
		repo := src[m[2]:m[3]]
		entity := src[m[4]:m[5]]
		name := "mongodb:" + repo + "<" + entity + ">"
		add(kotlinOrmQueryEmit(src, name, "mongodb", path, m[0],
			"repository", repo, "entity", entity,
			"provenance", "INFERRED_FROM_MONGO_REPOSITORY"))
	}
	// KMongo / driver operations.
	for _, m := range reOrmQueryMongoOp.FindAllStringSubmatchIndex(src, -1) {
		op := src[m[2]:m[3]]
		name := "mongodb:op:" + op
		add(kotlinOrmQueryEmit(src, name, "mongodb", path, m[0],
			"operation", op,
			"provenance", "INFERRED_FROM_MONGO_OPERATION"))
	}
}

// kotlinOrmQueryExtractSQLDelight handles named queries inside .sq files.
//
// SQLDelight .sq files declare named queries with a label line followed by SQL:
//
//	selectAllUsers:
//	SELECT * FROM users;
//
//	insertUser:
//	INSERT INTO users(id, name) VALUES (?, ?);
func kotlinOrmQueryExtractSQLDelight(src, path string, add func(types.EntityRecord)) {
	lines := strings.Split(src, "\n")
	offset := 0
	for i, ln := range lines {
		lineStart := offset
		offset += len(ln) + 1
		mm := reOrmQuerySqlDelightLabel.FindStringSubmatch(strings.TrimRight(ln, " \t"))
		if mm == nil {
			continue
		}
		label := mm[1]
		// The next non-blank line must look like a SQL statement for this to be
		// a named query (avoids matching arbitrary "word:" lines).
		sqlVerb := ""
		for j := i + 1; j < len(lines) && j < i+4; j++ {
			cand := strings.TrimSpace(lines[j])
			if cand == "" {
				continue
			}
			upper := strings.ToUpper(cand)
			for _, verb := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "WITH"} {
				if strings.HasPrefix(upper, verb) {
					sqlVerb = verb
				}
			}
			break
		}
		if sqlVerb == "" {
			continue
		}
		name := "sqldelight:" + label
		add(kotlinOrmQueryEmit(src, name, "sqldelight", path, lineStart,
			"query_label", label, "sql_verb", sqlVerb,
			"provenance", "INFERRED_FROM_SQLDELIGHT_NAMED_QUERY"))
	}
}

// kotlinOrmQueryTruncate bounds a captured SQL/JPQL string for use in entity
// names and properties.
func kotlinOrmQueryTruncate(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}
