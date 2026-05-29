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

// gocql.go: CQL-schema + query-DSL extractor for the gocql Cassandra driver
// (github.com/gocql/gocql, and the gocqlx query-builder layered on top).
//
// Cassandra is schema-ful (CQL has CREATE TABLE), so the honest coverage shape
// differs from the truly schema-less key/value stores:
//
//   - Models / Schema — partial. A `CREATE TABLE [keyspace.]name (...)` literal
//                    embedded in a Go string is parsed into a SCOPE.Schema
//                    table with its column names enumerated as SCOPE.Component
//                    fields. The schema is real CQL, but it is recovered from a
//                    string literal by regex (not a CQL parser, and DDL may
//                    live in external .cql files the Go extractor never sees),
//                    so this is partial not full.
//   - Queries      — partial. `session.Query("...")` / `.Bind(...)` /
//                    gocqlx `qb.Select(...)` call sites are captured with a
//                    coarse CRUD verb sniffed from the leading CQL keyword.
//                    The query text is often built dynamically, so binding a
//                    call to a concrete table from a regex is unreliable.
//   - Relationships— honesty-NA. Cassandra is a wide-column store with no
//                    foreign keys or joins; references are a denormalisation
//                    convention, not a driver concept. Recorded not_applicable.
//   - Migrations   — honesty-NA. gocql ships no migration runner; schema
//                    evolution is applied out-of-band (cqlsh / external tools).
//
// The extractor gates on the gocql import actually being present, so a file
// that merely mentions "cassandra" or contains CQL-looking strings without the
// driver is not poached.

func init() {
	extractor.Register("custom_go_gocql", &gocqlExtractor{})
}

type gocqlExtractor struct{}

func (e *gocqlExtractor) Language() string { return "custom_go_gocql" }

var (
	// Import marker for gocql (and gocqlx, the query-builder layer).
	reImportGocql = regexp.MustCompile(`"github\.com/(?:gocql/gocql|scylladb/gocqlx(?:/v\d+)?)"`)

	// CREATE TABLE [IF NOT EXISTS] [keyspace.]name ( ... ) inside a Go string
	// literal. The table name (last dotted segment) and the parenthesised column
	// body are captured. Case-insensitive; spans newlines.
	reCQLCreateTable = regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([A-Za-z_][\w.]*)\s*\((.*?)\)`)

	// A column definition line inside a CREATE TABLE body: a leading identifier
	// followed by a type token. PRIMARY KEY clauses and bare "PRIMARY"/"KEY"
	// tokens are filtered out by gocqlIsColumnLine.
	reCQLColumn = regexp.MustCompile(`(?im)^\s*([A-Za-z_]\w*)\s+([A-Za-z_][\w<>, ]*?)\s*[,)]?\s*$`)

	// session.Query("CQL ...") / gocqlx qb.Select/Insert/Update/Delete call
	// sites. Only the Query("…") string-literal form yields a CQL verb; the
	// builder forms are captured by their leading verb method.
	reGocqlQueryString = regexp.MustCompile(`(?s)\.Query\(\s*` + "`" + `([^` + "`" + `]*)` + "`" + `|\.Query\(\s*"((?:[^"\\]|\\.)*)"`)

	// gocqlx query-builder entry points: qb.Select("t") / qb.Insert("t") / ...
	reGocqlxBuilder = regexp.MustCompile(`\bqb\.(Select|Insert|Update|Delete)\(`)
)

func (e *gocqlExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.gocql_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "cassandra"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if file.Language != "go" || len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	if !reImportGocql.MatchString(src) {
		return nil, nil
	}

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

	// 1. Schema: CREATE TABLE literals => tables + enumerated columns.
	for _, m := range reCQLCreateTable.FindAllStringSubmatchIndex(src, -1) {
		qualified := src[m[2]:m[3]]
		body := src[m[4]:m[5]]
		// Logical table name = last dotted segment (strip keyspace prefix).
		table := qualified
		if i := strings.LastIndexByte(table, '.'); i >= 0 {
			table = table[i+1:]
		}
		line := lineOf(src, m[0])
		ent := makeEntity("table:"+table, "SCOPE.Schema", "", file.Path, file.Language, line)
		setProps(&ent, "framework", "cassandra", "provenance", "INFERRED_FROM_CQL_CREATE_TABLE",
			"table_name", table, "cql_table", qualified)
		add(ent)

		for _, cm := range reCQLColumn.FindAllStringSubmatch(body, -1) {
			col := cm[1]
			colType := strings.TrimSpace(cm[2])
			if !gocqlIsColumnLine(col, colType) {
				continue
			}
			fieldEnt := makeEntity("field:"+table+"."+col, "SCOPE.Component", "field", file.Path, file.Language, line)
			setProps(&fieldEnt, "framework", "cassandra", "provenance", "INFERRED_FROM_CQL_CREATE_TABLE",
				"model_name", table, "field_name", col, "cql_type", colType)
			add(fieldEnt)
		}
	}

	// 2. Queries: session.Query("CQL") string literals.
	for _, m := range reGocqlQueryString.FindAllStringSubmatchIndex(src, -1) {
		cql := submatch(src, m, 2) // backtick form
		if cql == "" {
			cql = submatch(src, m, 4) // double-quote form
		}
		verb := gocqlVerbKind(cql)
		ent := makeEntity("cql:"+verb+":"+itoa(lineOf(src, m[0])), "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "cassandra", "provenance", "INFERRED_FROM_CASSANDRA_CQL",
			"query_type", verb)
		add(ent)
	}

	// 3. Queries: gocqlx query-builder entry points.
	for _, m := range reGocqlxBuilder.FindAllStringSubmatchIndex(src, -1) {
		verb := strings.ToLower(src[m[2]:m[3]])
		ent := makeEntity("cqlx:"+verb+":"+itoa(lineOf(src, m[0])), "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "cassandra", "provenance", "INFERRED_FROM_CASSANDRA_CQL",
			"query_type", verb, "builder", "gocqlx")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// gocqlIsColumnLine filters non-column lines (PRIMARY KEY clauses, empty type)
// out of a CREATE TABLE body so only real column definitions become fields.
func gocqlIsColumnLine(col, colType string) bool {
	if col == "" || colType == "" {
		return false
	}
	up := strings.ToUpper(col)
	if up == "PRIMARY" || up == "KEY" || up == "WITH" {
		return false
	}
	// "PRIMARY KEY (...)" defined inline starts with PRIMARY; the type token of a
	// real column never begins with the PRIMARY/KEY keywords.
	upType := strings.ToUpper(colType)
	if strings.HasPrefix(upType, "KEY") {
		return false
	}
	return true
}

// gocqlVerbKind sniffs a coarse CRUD verb from the leading keyword of a CQL
// statement so query_type is comparable with the other data-access extractors.
func gocqlVerbKind(cql string) string {
	t := strings.ToLower(strings.TrimSpace(cql))
	switch {
	case strings.HasPrefix(t, "select"):
		return "select"
	case strings.HasPrefix(t, "insert"):
		return "insert"
	case strings.HasPrefix(t, "update"):
		return "update"
	case strings.HasPrefix(t, "delete"):
		return "delete"
	case strings.HasPrefix(t, "create"), strings.HasPrefix(t, "alter"), strings.HasPrefix(t, "drop"):
		return "ddl"
	default:
		return "query"
	}
}
