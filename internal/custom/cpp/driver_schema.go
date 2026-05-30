package cpp

// driver_schema.go — raw C++ database driver schema + query extraction.
//
// Covers: libpqxx (PostgreSQL), mongocxx (MongoDB), mysql-connector-cpp.
//
// Strategy (heuristic/partial):
//
//  1. Schema extraction — scan for embedded SQL CREATE TABLE statements inside
//     string literals passed to execute() / exec() / exec_params() calls.
//     Emits SCOPE.Schema (model) and SCOPE.Schema/column entities.
//
//  2. Query attribution — scan for SQL verb strings in execute-style calls.
//     Emits SCOPE.Operation/query entities.
//
//  3. mongocxx collection access — db["collection"] / db.collection("name")
//     pattern → emits a SCOPE.Schema entity for the collection name.
//
// Status: partial (regex over string literals; no AST).

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_cpp_driver_schema", &cppDriverSchemaExtractor{})
}

type cppDriverSchemaExtractor struct{}

func (e *cppDriverSchemaExtractor) Language() string { return "custom_cpp_driver_schema" }

var (
	// Driver import/include detection — gate the extractor.
	// Matches #include <pqxx/...>, #include <mongocxx/...>, #include <mysql/...>
	// or #include <mysql_driver.h> / #include <cppconn/...>
	reCppDriverInclude = regexp.MustCompile(
		`(?m)#\s*include\s+[<"](?:pqxx/|mongocxx/|bsoncxx/|mysql/|mysql_driver|cppconn/)[^>"]*[>"]`)

	// Execute-style calls: .exec("SQL") / .exec_params("SQL") / .execute("SQL") / .query("SQL")
	// Captures the opening quote so we can read the SQL literal.
	reCppDriverExec = regexp.MustCompile(
		`(?m)\.(?:exec|exec_params|exec0|exec1|execute|query|prepare)\s*\(\s*(?:R"[^(]*\(|[rRuU]*")(.*?)(?:"|\)")`)

	// Free-function exec-style calls: mysql_query(conn, "SQL") / mysql_real_query(...).
	// MySQL's C API uses free functions rather than methods, so the method regex
	// above misses them. Captures: (1) SQL string literal (first string arg).
	reCppDriverFreeExec = regexp.MustCompile(
		`(?m)\b(?:mysql_query|mysql_real_query)\s*\(\s*[A-Za-z_][A-Za-z0-9_]*\s*,\s*(?:R"[^(]*\(|[rRuU]*")(.*?)(?:"|\)")`)

	// CREATE TABLE detection inside a SQL string.
	// Captures: (1) table name
	reCppCreateTable = regexp.MustCompile(
		`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?[` + "`" + `"']?([A-Za-z_][A-Za-z0-9_]*)[` + "`" + `"']?\s*\(`)

	// Column definition at the start of a top-level CREATE TABLE body segment.
	// The body is split on top-level commas (see splitTopLevelCommas) so a single
	// inline DDL string yields one segment per column/constraint.
	// Captures: (1) column name, (2) type
	reCppSQLColumn = regexp.MustCompile(
		`^\s*[` + "`" + `"']?([A-Za-z_][A-Za-z0-9_]*)[` + "`" + `"']?\s+([A-Za-z][A-Za-z0-9_]*)`)

	// SQL constraint lead keywords to skip.
	cppSQLConstraintLead = map[string]bool{
		"PRIMARY": true, "FOREIGN": true, "UNIQUE": true, "CONSTRAINT": true,
		"CHECK": true, "KEY": true, "INDEX": true,
	}

	// SQL verb for query attribution.
	reCppSQLVerb = regexp.MustCompile(`(?i)^\s*(SELECT|INSERT|UPDATE|DELETE|CREATE|DROP|ALTER|TRUNCATE)\b`)

	// mongocxx collection access:
	//   db["collection_name"] or db.collection("collection_name")
	// Captures: (1) collection name
	reMongocxxCollection = regexp.MustCompile(
		`(?m)(?:db\s*\[\s*"([A-Za-z_][A-Za-z0-9_]*)"\s*\]|\.collection\s*\(\s*"([A-Za-z_][A-Za-z0-9_]*)"\s*\))`)

	// mongocxx CRUD/aggregate operations on a collection handle:
	//   coll.insert_one(...) / coll.find(...) / coll.update_one(...) /
	//   coll.delete_many(...) / coll.aggregate(...) etc.
	// Captures: (1) operation method name → mapped to a canonical Mongo verb.
	reMongocxxOp = regexp.MustCompile(
		`(?m)\.(insert_one|insert_many|find|find_one|find_one_and_update|find_one_and_delete|` +
			`update_one|update_many|replace_one|delete_one|delete_many|count_documents|` +
			`aggregate|bulk_write|distinct)\s*\(`)
)

// mongoVerbFromMethod maps a mongocxx collection method to a canonical verb
// used as the redis-style query verb on the emitted SCOPE.Operation entity.
var mongoVerbFromMethod = map[string]string{
	"insert_one": "INSERT", "insert_many": "INSERT",
	"find": "FIND", "find_one": "FIND",
	"find_one_and_update": "UPDATE", "find_one_and_delete": "DELETE",
	"update_one": "UPDATE", "update_many": "UPDATE", "replace_one": "UPDATE",
	"delete_one": "DELETE", "delete_many": "DELETE",
	"count_documents": "COUNT", "aggregate": "AGGREGATE",
	"bulk_write": "BULK_WRITE", "distinct": "DISTINCT",
}

func (e *cppDriverSchemaExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.cpp_driver_schema.extract",
		trace.WithAttributes(
			attribute.String("file_path", file.Path),
			attribute.String("framework", "cpp_driver"),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "cpp" && file.Language != "c" {
		return nil, nil
	}
	src := string(file.Content)

	// Gate: must include a recognised C++ DB driver header.
	if !reCppDriverInclude.MatchString(src) {
		return nil, nil
	}

	isMongo := strings.Contains(src, "mongocxx") || strings.Contains(src, "bsoncxx")

	var out []types.EntityRecord

	// --- mongocxx: collection access → model entity ---
	if isMongo {
		seenColl := map[string]bool{}
		for _, m := range reMongocxxCollection.FindAllStringSubmatchIndex(src, -1) {
			collName := ""
			if m[2] >= 0 {
				collName = src[m[2]:m[3]]
			} else if m[4] >= 0 {
				collName = src[m[4]:m[5]]
			}
			if collName == "" || seenColl[collName] {
				continue
			}
			seenColl[collName] = true
			lineNum := lineOf(src, m[0])
			ent := makeEntity(collName, "SCOPE.Schema", "", file.Path, "cpp", lineNum)
			setProps(&ent,
				"framework", "mongocxx",
				"provenance", "INFERRED_FROM_MONGOCXX_COLLECTION",
				"pattern_type", "collection",
				"collection_name", collName,
			)
			out = append(out, ent)
		}

		// --- mongocxx: CRUD/aggregate operation → query entity ---
		for _, m := range reMongocxxOp.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			verb := mongoVerbFromMethod[method]
			if verb == "" {
				verb = strings.ToUpper(method)
			}
			lineNum := lineOf(src, m[0])
			queryName := "mongo:" + verb + "@L" + strconv.Itoa(lineNum)
			ent := makeEntity(queryName, "SCOPE.Operation", "query", file.Path, "cpp", lineNum)
			setProps(&ent,
				"framework", "mongocxx",
				"provenance", "INFERRED_FROM_MONGOCXX_OP",
				"mongo_verb", verb,
				"method_name", method,
			)
			out = append(out, ent)
		}
	}

	// --- SQL-based drivers: exec("SQL") / mysql_query(conn,"SQL") → schema + query ---
	processSQL := func(sqlText string, lineNum int) {
		// Query attribution
		if verbM := reCppSQLVerb.FindStringSubmatch(sqlText); verbM != nil {
			verb := strings.ToUpper(verbM[1])
			queryName := verb + "@L" + strconv.Itoa(lineNum)
			ent := makeEntity(queryName, "SCOPE.Operation", "query", file.Path, "cpp", lineNum)
			setProps(&ent,
				"framework", "cpp_driver",
				"provenance", "INFERRED_FROM_CPP_DRIVER_EXEC",
				"sql_verb", verb,
				"sql_text", truncate(sqlText, 120),
			)
			out = append(out, ent)
		}

		// Schema extraction from CREATE TABLE
		ctm := reCppCreateTable.FindStringSubmatchIndex(sqlText)
		if ctm == nil {
			return
		}
		tableName := sqlText[ctm[2]:ctm[3]]
		tableEnt := makeEntity(tableName, "SCOPE.Schema", "", file.Path, "cpp", lineNum)
		setProps(&tableEnt,
			"framework", "cpp_driver",
			"provenance", "INFERRED_FROM_CPP_DRIVER_CREATE_TABLE",
			"pattern_type", "table",
			"table_name", tableName,
		)
		out = append(out, tableEnt)

		// Extract column definitions from the CREATE TABLE body. ctm[1] points at
		// the opening '(' match end; walk to the matching ')' (paren-balanced) so
		// inline types like VARCHAR(255)/DECIMAL(10,2) don't truncate the body.
		bodyStart := ctm[1]
		if bodyStart >= len(sqlText) {
			return
		}
		body := balancedParenBody(sqlText[bodyStart-1:])

		for _, segment := range splitTopLevelCommas(body) {
			cm := reCppSQLColumn.FindStringSubmatch(segment)
			if cm == nil {
				continue
			}
			colName := cm[1]
			colType := strings.ToUpper(cm[2])
			if cppSQLConstraintLead[colType] {
				continue
			}
			colEnt := makeEntity(tableName+"."+colName, "SCOPE.Schema", "column", file.Path, "cpp", lineNum)
			setProps(&colEnt,
				"framework", "cpp_driver",
				"provenance", "INFERRED_FROM_CPP_DRIVER_CREATE_TABLE",
				"pattern_type", "column",
				"column_name", colName,
				"column_type", colType,
				"parent_table", tableName,
			)
			out = append(out, colEnt)
		}
	}

	for _, m := range reCppDriverExec.FindAllStringSubmatchIndex(src, -1) {
		if m[2] < 0 {
			continue
		}
		processSQL(src[m[2]:m[3]], lineOf(src, m[0]))
	}
	for _, m := range reCppDriverFreeExec.FindAllStringSubmatchIndex(src, -1) {
		if m[2] < 0 {
			continue
		}
		processSQL(src[m[2]:m[3]], lineOf(src, m[0]))
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// balancedParenBody takes a string beginning at an opening '(' and returns the
// inner text up to (but excluding) the matching ')', respecting nesting. If the
// string does not start with '(' or no balance is found, it returns the input
// minus the leading paren. Used to isolate a CREATE TABLE column list that may
// contain parenthesised types such as VARCHAR(255) or DECIMAL(10,2).
func balancedParenBody(s string) string {
	if len(s) == 0 || s[0] != '(' {
		return s
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i]
			}
		}
	}
	return s[1:]
}

// splitTopLevelCommas splits a CREATE TABLE column list on commas that are not
// nested inside parentheses, so DECIMAL(10,2) stays a single segment.
func splitTopLevelCommas(s string) []string {
	var segments []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				segments = append(segments, s[start:i])
				start = i + 1
			}
		}
	}
	segments = append(segments, s[start:])
	return segments
}
