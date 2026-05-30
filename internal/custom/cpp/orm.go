package cpp

// orm.go — C++ ORM extractors: ODB, SOCI, and sqlpp11.
//
// Three extractors are registered:
//
//  1. custom_cpp_odb   — ODB pragma-based ORM
//     Detects:
//     - #pragma db object → model entity
//     - #pragma db member → schema (column) entity
//     - #pragma db id / #pragma db column → column annotations
//     - one_to_one / one_to_many / many_to_many values → relationship entities
//     - lazy_ptr / lazy_shared_ptr → lazy-loading annotations (not_applicable:
//       lazy-loading recognition is recorded but ODB itself treats this as the
//       lazy-proxy pattern)
//     - odb::query<T>(...) → query entity
//
//  2. custom_cpp_soci  — SOCI (database access library)
//     Detects:
//     - into() / use() — schema binding, recorded as schema entities
//     - sql << "SELECT …" / sql << "INSERT …" → query entities
//
//  3. custom_cpp_sqlpp11 — sqlpp11 type-safe C++ SQL DSL
//     Detects:
//     - SQLPP_ALIAS_PROVIDER / table struct declarations → schema entities
//     - db(select(…)), db(insert_into(…)), db(update(…)), db(remove_from(…)) → query entities
//
// All three extractors emit:
//   SCOPE.Schema  (subtype="" for model-level, "column" for field-level)
//   SCOPE.Pattern (subtype="relationship") for FK/association edges
//   SCOPE.Operation (subtype="query") for query attribution
//
// Status: partial (regex heuristic, no AST).

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
	extractor.Register("custom_cpp_odb", &odbExtractor{})
	extractor.Register("custom_cpp_soci", &sociExtractor{})
	extractor.Register("custom_cpp_sqlpp11", &sqlpp11Extractor{})
}

// ============================================================================
// ODB extractor
// ============================================================================

type odbExtractor struct{}

func (e *odbExtractor) Language() string { return "custom_cpp_odb" }

var (
	// #pragma db object [session | no_id | ...]
	// Capture: (1) class name on the NEXT non-blank struct/class line
	// We use a two-step approach: find the pragma then look for the class.
	reODBObject = regexp.MustCompile(`(?m)#\s*pragma\s+db\s+object\b`)

	// struct/class name after a #pragma db object block
	// Capture: (1) name
	reODBClassName = regexp.MustCompile(`(?m)(?:class|struct)\s+([A-Za-z_][A-Za-z0-9_]*)`)

	// #pragma db member(field_name) [type(...) | null | not_null | id | column("col")]
	// Capture: (1) member field name, (2) optional annotations
	reODBMember = regexp.MustCompile(`(?m)#\s*pragma\s+db\s+member\s*\(\s*([A-Za-z_][A-Za-z0-9_:]*(?:\s*::\s*[A-Za-z_][A-Za-z0-9_]*)?)\s*\)([^\n]*)`)

	// #pragma db id — marks a field as PK (appears before the field declaration)
	reODBId = regexp.MustCompile(`(?m)#\s*pragma\s+db\s+id\b`)

	// odb::lazy_ptr / odb::lazy_shared_ptr — lazy-loading marker
	reODBLazy = regexp.MustCompile(`(?m)\bodb\s*::\s*lazy_(?:ptr|shared_ptr|weak_ptr)\s*<\s*([A-Za-z_][A-Za-z0-9_:]*)\s*>`)

	// odb::result<T> / odb::query<T>(...) — query
	// Capture: (1) model type
	reODBQuery = regexp.MustCompile(`(?m)\bodb\s*::\s*(?:query|result)\s*<\s*([A-Za-z_][A-Za-z0-9_:]*)\s*>`)

	// one_to_one / one_to_many / many_to_many inside #pragma db member annotations
	reODBRelKind = regexp.MustCompile(`\b(one_to_one|one_to_many|many_to_many|many_to_one)\b`)

	// inverse(<type>::<field>) — back-reference in a relationship
	reODBInverse = regexp.MustCompile(`\binverse\s*\(\s*([A-Za-z_][A-Za-z0-9_:]*)\s*\)`)

	// table("name") on a #pragma db object — explicit table mapping.
	reODBTable = regexp.MustCompile(`\btable\s*\(\s*"([^"]+)"\s*\)`)

	// column("name") in a #pragma db member annotation — explicit column name.
	reODBColumn = regexp.MustCompile(`\bcolumn\s*\(\s*"([^"]+)"\s*\)`)

	// type("SQL TYPE") in a #pragma db member annotation — DB column type.
	reODBType = regexp.MustCompile(`\btype\s*\(\s*"([^"]+)"\s*\)`)

	// id keyword as a standalone annotation token (PK marker on the member line).
	reODBIDToken = regexp.MustCompile(`(^|\s)id(\s|$)`)
)

func (e *odbExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.odb_extractor.extract",
		trace.WithAttributes(
			attribute.String("file_path", file.Path),
			attribute.String("framework", "odb"),
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
	if !strings.Contains(src, "#pragma db") && !strings.Contains(src, "odb::") {
		return nil, nil
	}

	var out []types.EntityRecord

	// --- Model extraction: #pragma db object → class/struct immediately after ---
	for _, pragmaIdx := range reODBObject.FindAllStringIndex(src, -1) {
		// The pragma line may carry a table("name") mapping; capture the rest of
		// the pragma line before scanning for the class.
		pragmaLineEnd := strings.IndexByte(src[pragmaIdx[1]:], '\n')
		tableName := ""
		if pragmaLineEnd >= 0 {
			if tm := reODBTable.FindStringSubmatch(src[pragmaIdx[1] : pragmaIdx[1]+pragmaLineEnd]); tm != nil {
				tableName = tm[1]
			}
		}
		// Search for first class/struct after the pragma
		afterPragma := src[pragmaIdx[1]:]
		cm := reODBClassName.FindStringSubmatchIndex(afterPragma)
		if cm == nil {
			continue
		}
		className := afterPragma[cm[2]:cm[3]]
		lineNum := lineOf(src, pragmaIdx[0])
		ent := makeEntity(className, "SCOPE.Schema", "", file.Path, "cpp", lineNum)
		setProps(&ent,
			"framework", "odb",
			"provenance", "INFERRED_FROM_ODB_PRAGMA",
			"pattern_type", "model",
			"class_name", className,
		)
		// Explicit table mapping defaults to the class name when omitted.
		if tableName == "" {
			tableName = className
		}
		setProps(&ent, "table_name", tableName)
		out = append(out, ent)
	}

	// --- Schema extraction: #pragma db member(field) → column entity ---
	for _, m := range reODBMember.FindAllStringSubmatchIndex(src, -1) {
		field := src[m[2]:m[3]]
		// strip class:: prefix if present
		if idx := strings.LastIndex(field, "::"); idx >= 0 {
			field = field[idx+2:]
		}
		field = strings.TrimSpace(field)
		annotations := ""
		if m[4] >= 0 {
			annotations = strings.TrimSpace(src[m[4]:m[5]])
		}
		lineNum := lineOf(src, m[0])
		colName := "member:" + field
		ent := makeEntity(colName, "SCOPE.Schema", "column", file.Path, "cpp", lineNum)
		setProps(&ent,
			"framework", "odb",
			"provenance", "INFERRED_FROM_ODB_PRAGMA",
			"pattern_type", "column",
			"field_name", field,
			"annotations", annotations,
		)
		// Parse the explicit column("…") / type("…") / id annotations so the
		// resolved DB column name + type are first-class properties, not just
		// raw annotation text.
		dbColumn := field // default: ODB maps member → column of the same name
		if cm := reODBColumn.FindStringSubmatch(annotations); cm != nil {
			dbColumn = cm[1]
		}
		setProps(&ent, "column_name", dbColumn)
		if tm := reODBType.FindStringSubmatch(annotations); tm != nil {
			setProps(&ent, "column_type", tm[1])
		}
		if reODBIDToken.MatchString(annotations) {
			setProps(&ent, "is_primary_key", "true")
		}
		out = append(out, ent)

		// --- Relationship extraction from member annotations ---
		if relKindM := reODBRelKind.FindStringSubmatch(annotations); relKindM != nil {
			relKind := relKindM[1]
			targetType := ""
			if invM := reODBInverse.FindStringSubmatch(annotations); invM != nil {
				targetType = invM[1]
			}
			relName := relKind + ":" + field
			relEnt := makeEntity(relName, "SCOPE.Pattern", "relationship", file.Path, "cpp", lineNum)
			setProps(&relEnt,
				"framework", "odb",
				"provenance", "INFERRED_FROM_ODB_PRAGMA",
				"relationship_kind", relKind,
				"field_name", field,
				"target_type", targetType,
			)
			out = append(out, relEnt)
		}
	}

	// --- FK/lazy-loading: odb::lazy_ptr<T> ---
	for _, m := range reODBLazy.FindAllStringSubmatchIndex(src, -1) {
		targetType := src[m[2]:m[3]]
		lineNum := lineOf(src, m[0])
		relName := "lazy_ptr:" + targetType
		ent := makeEntity(relName, "SCOPE.Pattern", "relationship", file.Path, "cpp", lineNum)
		setProps(&ent,
			"framework", "odb",
			"provenance", "INFERRED_FROM_ODB_LAZY",
			"relationship_kind", "lazy_ptr",
			"target_type", targetType,
		)
		out = append(out, ent)
	}

	// --- Query extraction: odb::query<T> / odb::result<T> ---
	seen := map[string]bool{}
	for _, m := range reODBQuery.FindAllStringSubmatchIndex(src, -1) {
		modelType := src[m[2]:m[3]]
		lineNum := lineOf(src, m[0])
		key := "query:" + modelType + ":" + strconv.Itoa(lineNum)
		if seen[key] {
			continue
		}
		seen[key] = true
		ent := makeEntity("query:"+modelType, "SCOPE.Operation", "query", file.Path, "cpp", lineNum)
		setProps(&ent,
			"framework", "odb",
			"provenance", "INFERRED_FROM_ODB_QUERY",
			"model_type", modelType,
		)
		out = append(out, ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// ============================================================================
// SOCI extractor
// ============================================================================

type sociExtractor struct{}

func (e *sociExtractor) Language() string { return "custom_cpp_soci" }

var (
	// soci::session sql(...) or using namespace soci; — presence check
	// into(var) — input binding (schema)
	// use(var)  — output binding (schema)
	// Capture: (1) variable name
	reSOCIInto = regexp.MustCompile(`(?m)\binto\s*\(\s*([A-Za-z_][A-Za-z0-9_.]*)\s*(?:,\s*[^)]+)?\)`)
	reSOCIUse  = regexp.MustCompile(`(?m)\buse\s*\(\s*([A-Za-z_][A-Za-z0-9_.]*)\s*(?:,\s*[^)]+)?\)`)

	// sql << "SQL text" — query
	// Capture: (1) SQL keyword (SELECT/INSERT/UPDATE/DELETE/CREATE)
	reSOCIQuery = regexp.MustCompile(`(?m)sql\s*<<\s*(?:"([^"]*?)"|'([^']*?)')`)

	// Struct-style SOCI type conversion: type_conversion<T>
	reSOCITypeConv = regexp.MustCompile(`(?m)\btype_conversion\s*<\s*([A-Za-z_][A-Za-z0-9_:]*)\s*>`)
)

func (e *sociExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.soci_extractor.extract",
		trace.WithAttributes(
			attribute.String("file_path", file.Path),
			attribute.String("framework", "soci"),
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
	if !strings.Contains(src, "soci") && !strings.Contains(src, "into(") &&
		!strings.Contains(src, "use(") && !strings.Contains(src, "type_conversion") {
		return nil, nil
	}

	var out []types.EntityRecord

	// --- Schema extraction: type_conversion<T> → model entity ---
	for _, m := range reSOCITypeConv.FindAllStringSubmatchIndex(src, -1) {
		typeName := src[m[2]:m[3]]
		lineNum := lineOf(src, m[0])
		ent := makeEntity(typeName, "SCOPE.Schema", "", file.Path, "cpp", lineNum)
		setProps(&ent,
			"framework", "soci",
			"provenance", "INFERRED_FROM_SOCI_TYPE_CONVERSION",
			"pattern_type", "model",
			"class_name", typeName,
		)
		out = append(out, ent)
	}

	// --- Schema extraction: into(var) / use(var) — column bindings ---
	seenBinding := map[string]bool{}
	for _, m := range reSOCIInto.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		lineNum := lineOf(src, m[0])
		key := "into:" + varName
		if seenBinding[key] {
			continue
		}
		seenBinding[key] = true
		ent := makeEntity("binding:"+varName, "SCOPE.Schema", "column", file.Path, "cpp", lineNum)
		setProps(&ent,
			"framework", "soci",
			"provenance", "INFERRED_FROM_SOCI_INTO",
			"pattern_type", "column_binding",
			"direction", "into",
			"variable", varName,
		)
		out = append(out, ent)
	}
	for _, m := range reSOCIUse.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		lineNum := lineOf(src, m[0])
		key := "use:" + varName
		if seenBinding[key] {
			continue
		}
		seenBinding[key] = true
		ent := makeEntity("binding:"+varName, "SCOPE.Schema", "column", file.Path, "cpp", lineNum)
		setProps(&ent,
			"framework", "soci",
			"provenance", "INFERRED_FROM_SOCI_USE",
			"pattern_type", "column_binding",
			"direction", "use",
			"variable", varName,
		)
		out = append(out, ent)
	}

	// --- Query extraction: sql << "..." ---
	for _, m := range reSOCIQuery.FindAllStringSubmatchIndex(src, -1) {
		sqlText := ""
		if m[2] >= 0 {
			sqlText = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			sqlText = src[m[4]:m[5]]
		}
		lineNum := lineOf(src, m[0])
		sqlUpper := strings.ToUpper(strings.TrimSpace(sqlText))
		verb := "QUERY"
		for _, kw := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP"} {
			if strings.HasPrefix(sqlUpper, kw) {
				verb = kw
				break
			}
		}
		queryName := verb + "@L" + strconv.Itoa(lineNum)
		ent := makeEntity(queryName, "SCOPE.Operation", "query", file.Path, "cpp", lineNum)
		setProps(&ent,
			"framework", "soci",
			"provenance", "INFERRED_FROM_SOCI_QUERY",
			"sql_verb", verb,
			"sql_text", truncate(sqlText, 120),
		)
		out = append(out, ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// ============================================================================
// sqlpp11 extractor
// ============================================================================

type sqlpp11Extractor struct{}

func (e *sqlpp11Extractor) Language() string { return "custom_cpp_sqlpp11" }

var (
	// SQLPP_ALIAS_PROVIDER(alias) — table alias macro
	reSQLPP11Alias = regexp.MustCompile(`(?m)\bSQLPP_ALIAS_PROVIDER\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)`)

	// sqlpp::table<...> struct pattern:
	// struct Tab : sqlpp::table<Tab, ...>
	// Capture: (1) table struct name
	reSQLPP11Table = regexp.MustCompile(`(?m)struct\s+([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(?:public\s+)?sqlpp\s*::\s*table\b`)

	// Column definition inside a sqlpp11 table: using colname = ...
	// sqlpp11 column structs look like: struct col_ { ... using _traits = ... }
	reSQLPP11Column = regexp.MustCompile(`(?m)struct\s+([a-z_][a-z0-9_]*_)\s*\{`)

	// Query calls: db(select(...)), db(insert_into(...)), db(update(...)), db(remove_from(...))
	// Capture: (1) operation name
	reSQLPP11Op = regexp.MustCompile(`(?m)\bdb\s*\(\s*(select|insert_into|update|remove_from|truncate)\s*\(`)

	// Multi-table select: sqlpp::select(...)
	reSQLPP11Select = regexp.MustCompile(`(?m)\bsqlpp\s*::\s*(select|insert_into|update|remove_from)\s*\(`)
)

func (e *sqlpp11Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.sqlpp11_extractor.extract",
		trace.WithAttributes(
			attribute.String("file_path", file.Path),
			attribute.String("framework", "sqlpp11"),
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
	if !strings.Contains(src, "sqlpp") && !strings.Contains(src, "SQLPP_ALIAS_PROVIDER") &&
		!strings.Contains(src, "insert_into(") && !strings.Contains(src, "remove_from(") {
		return nil, nil
	}

	var out []types.EntityRecord

	// --- Model extraction: sqlpp::table<...> struct ---
	for _, m := range reSQLPP11Table.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		lineNum := lineOf(src, m[0])
		ent := makeEntity(tableName, "SCOPE.Schema", "", file.Path, "cpp", lineNum)
		setProps(&ent,
			"framework", "sqlpp11",
			"provenance", "INFERRED_FROM_SQLPP11_TABLE",
			"pattern_type", "model",
			"class_name", tableName,
		)
		out = append(out, ent)

		// Scan class body for column structs (heuristic: struct name ends with _)
		body := extractCppClassBody(src, m[0])
		for _, cm := range reSQLPP11Column.FindAllStringSubmatchIndex(body, -1) {
			colStructName := body[cm[2]:cm[3]]
			// The public column name is the struct name with trailing _ stripped
			colName := strings.TrimRight(colStructName, "_")
			colLineNum := lineNum + strings.Count(body[:cm[0]], "\n")
			colEnt := makeEntity(tableName+"."+colName, "SCOPE.Schema", "column", file.Path, "cpp", colLineNum)
			setProps(&colEnt,
				"framework", "sqlpp11",
				"provenance", "INFERRED_FROM_SQLPP11_COLUMN",
				"pattern_type", "column",
				"col_struct", colStructName,
				"parent_table", tableName,
			)
			out = append(out, colEnt)
		}
	}

	// --- Schema extraction: SQLPP_ALIAS_PROVIDER ---
	for _, m := range reSQLPP11Alias.FindAllStringSubmatchIndex(src, -1) {
		alias := src[m[2]:m[3]]
		lineNum := lineOf(src, m[0])
		ent := makeEntity("alias:"+alias, "SCOPE.Schema", "alias", file.Path, "cpp", lineNum)
		setProps(&ent,
			"framework", "sqlpp11",
			"provenance", "INFERRED_FROM_SQLPP11_ALIAS",
			"pattern_type", "alias",
			"alias_name", alias,
		)
		out = append(out, ent)
	}

	// --- Query extraction: db(select/insert_into/update/remove_from) ---
	seenQuery := map[string]bool{}
	emitQuery := func(opName string, lineNum int) {
		key := opName + ":" + strconv.Itoa(lineNum)
		if seenQuery[key] {
			return
		}
		seenQuery[key] = true
		queryName := strings.ToUpper(opName) + "@L" + strconv.Itoa(lineNum)
		ent := makeEntity(queryName, "SCOPE.Operation", "query", file.Path, "cpp", lineNum)
		setProps(&ent,
			"framework", "sqlpp11",
			"provenance", "INFERRED_FROM_SQLPP11_QUERY",
			"sql_verb", strings.ToUpper(opName),
		)
		out = append(out, ent)
	}

	for _, m := range reSQLPP11Op.FindAllStringSubmatchIndex(src, -1) {
		emitQuery(src[m[2]:m[3]], lineOf(src, m[0]))
	}
	for _, m := range reSQLPP11Select.FindAllStringSubmatchIndex(src, -1) {
		emitQuery(src[m[2]:m[3]], lineOf(src, m[0]))
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// ============================================================================
// Shared helpers
// ============================================================================

// extractCppClassBody returns the text of a C++ class/struct body starting at
// classStart, up to the matching closing brace (heuristic brace counting).
// Returns empty string if no opening brace is found within 5 lines.
func extractCppClassBody(source string, classStart int) string {
	// Find the first '{' after classStart
	rest := source[classStart:]
	braceIdx := strings.IndexByte(rest, '{')
	if braceIdx < 0 {
		return ""
	}
	// Check it's within a reasonable distance (≤ 500 chars = ~5 lines)
	if braceIdx > 500 {
		return ""
	}
	start := braceIdx + 1
	depth := 1
	for i := start; i < len(rest); i++ {
		switch rest[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[start:i]
			}
		}
	}
	return rest[start:]
}

// truncate limits a string to maxLen characters, appending "…" when trimmed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
