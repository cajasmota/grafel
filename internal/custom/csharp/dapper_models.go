// Package csharp — ORM model extractor for Dapper, LINQ-to-SQL, LinqToDB,
// and NHibernate/FluentNHibernate C# source files.
//
// Patterns extracted:
//
//	Dapper (POCO + attribute annotations + SQL attribution):
//	  - POCO class with [Table("name")] / [Column("name")] → Models/schema
//	  - sql.Query<T>(...) / sql.Execute(...) → query_attribution
//	  - POCO class T referenced in Query<T>(...) → model_extraction (full)
//	    including public auto-property declarations (public TYPE Prop { get; set; })
//	  - SQL string literal in Query<T>("SELECT …") → query_attribution (full)
//	    verb (SELECT/INSERT/UPDATE/DELETE) + table attributed to model T
//	  - Columns in SQL SELECT col1, col2 FROM … / INSERT INTO t (col1, col2)
//	    → schema_extraction (full where determinable, honest-partial otherwise)
//
//	LINQ-to-SQL / LinqToDB:
//	  - [Table] / [Table(Name="...")] class attribute → Models
//	  - [Column] / [Column(Name="...")] property attribute → schema
//	  - [Association(...)] property attribute → relationship_extraction
//
//	NHibernate / FluentNHibernate:
//	  - ClassMap<T> subclass (FluentNHibernate mapping) → Models/schema
//	  - Map(x => x.Prop) / References(x => x.Nav) fluent calls → relationships
//	  - ISession.Query<T> / ISession.Get<T> → query_attribution
//
// Emitted entity kinds:
//
//	SCOPE.Component   — model/table/mapping containers
//	SCOPE.Operation   — queries
//	SCOPE.Pattern     — attribute-annotated schema, relationship markers
package csharp

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
	extractor.Register("custom_csharp_orm_models", &ormModelsExtractor{})
}

type ormModelsExtractor struct{}

func (e *ormModelsExtractor) Language() string { return "custom_csharp_orm_models" }

// ---------------------------------------------------------------------------
// Regexes — Dapper
// ---------------------------------------------------------------------------

var (
	// [Table("users")] or [Table] on a class
	reDapperTable = regexp.MustCompile(
		`\[Table(?:\s*\(\s*(?:Name\s*=\s*)?["']([^"']+)["']\s*\))?\s*\]`,
	)
	// [Column("col_name")] or [Column] on a property
	reDapperColumn = regexp.MustCompile(
		`\[Column(?:\s*\(\s*(?:Name\s*=\s*)?["']([^"']+)["']\s*\))?\s*\]`,
	)
	// Dapper query: conn.Query<T>("sql") / conn.QueryAsync<T>("sql") / db.Execute("sql")
	reDapperQuery = regexp.MustCompile(
		`\.(?:Query|QueryAsync|QueryFirst|QueryFirstOrDefault|QuerySingle|QuerySingleOrDefault|Execute|ExecuteAsync|ExecuteScalar)\s*(?:<\s*(\w+)\s*>)?\s*\(`,
	)
	// POCO class with [Table] — class declaration following the attribute
	reDapperClass = regexp.MustCompile(
		`(?m)class\s+(\w+)\s*(?::\s*[\w\s,<>]+)?\s*\{`,
	)

	// Deep extraction: capture Query/Execute method name, optional type arg T, and
	// the first string argument (SQL literal, single- or double-quoted, verbatim
	// @"…" or interpolated $"…" prefix).
	// Group 1: method name; group 2: type arg T (may be empty); group 3: SQL literal.
	reDapperQueryFull = regexp.MustCompile(
		`\.(Query(?:Async|First(?:OrDefault)?|Single(?:OrDefault)?)?|Execute(?:Async|Scalar(?:Async)?)?)\s*` +
			`(?:<\s*(\w+)\s*>)?\s*\(\s*` +
			`(?:[@$]?"([^"\\]*(?:\\.[^"\\]*)*)"|@?'([^'\\]*(?:\\.[^'\\]*)*)')`,
	)

	// Public auto-property inside a POCO class body:
	//   public [modifier] TYPE PropName { get; set; }
	// Captures: group 1 = property type (may be generic like List<int>), group 2 = property name.
	reDapperProp = regexp.MustCompile(
		`(?m)^\s*(?:public|internal|protected)\s+(?:(?:virtual|override|new|required|static)\s+)*` +
			`([\w<>\[\]?,\s]+?)\s+(\w+)\s*\{\s*get\s*;\s*(?:(?:private|protected|init)\s+)?set\s*;`,
	)
)

// ---------------------------------------------------------------------------
// Regexes — LINQ-to-SQL / LinqToDB
// ---------------------------------------------------------------------------

var (
	// [Table] or [Table(Name="tableName")] — shared with Dapper but presence
	// of LinqToDB / L2S namespace usage discriminates the framework.
	// We re-use reDapperTable above and tag by framework via caller context.

	// [Association(ThisKey="...", OtherKey="...")] property attribute
	reLinqAssociation = regexp.MustCompile(
		`\[Association\s*\([^)]*\)\s*\]`,
	)
	// DataContext / DataConnection subclass (L2S / LinqToDB context)
	reLinqContext = regexp.MustCompile(
		`(?m)class\s+(\w+)\s*:\s*(?:DataContext|DataConnection)\b`,
	)
	// Table<T> property on a DataContext (L2S table declaration)
	reLinqTable = regexp.MustCompile(
		`Table\s*<\s*(\w+)\s*>`,
	)
)

// ---------------------------------------------------------------------------
// Regexes — NHibernate / FluentNHibernate
// ---------------------------------------------------------------------------

var (
	// class MyMap : ClassMap<Entity>
	reNHClassMap = regexp.MustCompile(
		`(?m)class\s+(\w+)\s*:\s*ClassMap\s*<\s*(\w+)\s*>`,
	)
	// Map(x => x.PropertyName) — column mapping in ClassMap
	reNHMap = regexp.MustCompile(
		`\.Map\s*\(\s*x\s*=>\s*x\.(\w+)`,
	)
	// References(x => x.Nav) — many-to-one relationship (bare call or chained)
	reNHReferences = regexp.MustCompile(
		`(?:^|\.|\s)References\s*\(\s*x\s*=>\s*x\.(\w+)`,
	)
	// HasMany(x => x.Col) — one-to-many relationship (bare call or chained)
	reNHHasMany = regexp.MustCompile(
		`(?:^|\.|\s)HasMany\s*\(\s*x\s*=>\s*x\.(\w+)`,
	)
	// session.Query<T>() / session.Get<T>()
	reNHQuery = regexp.MustCompile(
		`\.(?:Query|Get|Load|Find|QueryOver)\s*<\s*(\w+)\s*>\s*\(`,
	)
	// ISession usage marker — presence means NHibernate
	reNHSession = regexp.MustCompile(
		`\bISession\b`,
	)
)

// ---------------------------------------------------------------------------
// framework detection helpers
// ---------------------------------------------------------------------------

var (
	reDapperNS     = regexp.MustCompile(`using\s+Dapper\b`)
	reLinqToSQLNS  = regexp.MustCompile(`using\s+System\.Data\.Linq\b`)
	reLinqToDBNS   = regexp.MustCompile(`using\s+LinqToDB\b`)
	reNHibernateNS = regexp.MustCompile(`using\s+(?:NHibernate|FluentNHibernate)\b`)
)

// ---------------------------------------------------------------------------
// SQL-parsing helpers (Dapper deep extraction)
// ---------------------------------------------------------------------------

var (
	// SQL verb at the start of the statement (case-insensitive).
	reSQLVerb = regexp.MustCompile(`(?i)^\s*(SELECT|INSERT\s+(?:INTO\s+)?|UPDATE|DELETE\s+(?:FROM\s+)?)`)

	// Table name after SELECT … FROM tableName (stops at space, comma, WHERE, JOIN, etc.)
	reSQLFromTable = regexp.MustCompile(`(?i)\bFROM\s+(?:\[?(\w+)\]?\.)?(?:\[?(\w+)\]?)(?:\s+(?:AS\s+)?\w+)?\s*(?:$|\s|,|WHERE|JOIN|INNER|LEFT|RIGHT|ORDER|GROUP|HAVING|LIMIT|OFFSET|;)`)

	// INSERT INTO tableName (…) / INSERT INTO [schema].[tableName]
	reSQLInsertTable = regexp.MustCompile(`(?i)\bINSERT\s+(?:OR\s+\w+\s+)?INTO\s+(?:\[?(\w+)\]?\.)?(?:\[?(\w+)\]?)`)

	// UPDATE tableName SET …
	reSQLUpdateTable = regexp.MustCompile(`(?i)\bUPDATE\s+(?:\[?(\w+)\]?\.)?(?:\[?(\w+)\]?)`)

	// DELETE FROM tableName
	reSQLDeleteTable = regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+(?:\[?(\w+)\]?\.)?(?:\[?(\w+)\]?)`)

	// SELECT col1, col2, … FROM — captures everything between SELECT and FROM
	// (naive: works for simple flat lists, not sub-selects)
	reSQLSelectCols = regexp.MustCompile(`(?is)\bSELECT\s+(.*?)\bFROM\b`)

	// INSERT INTO t (col1, col2) — captures the column list in parens
	reSQLInsertCols = regexp.MustCompile(`(?is)\bINSERT\s+(?:OR\s+\w+\s+)?INTO\s+\S+\s*\(([^)]+)\)`)
)

// csDapperSQLVerb extracts the normalised SQL verb from a raw SQL literal.
func csDapperSQLVerb(sql string) string {
	m := reSQLVerb.FindStringSubmatch(sql)
	if m == nil {
		return ""
	}
	v := strings.ToUpper(strings.Fields(m[1])[0])
	return v
}

// csDapperSQLTable extracts the primary target table from a raw SQL literal.
// Returns (tableName, ok).
func csDapperSQLTable(sql string) (string, bool) {
	verb := csDapperSQLVerb(sql)
	switch verb {
	case "SELECT":
		if m := reSQLFromTable.FindStringSubmatch(sql); m != nil {
			if m[2] != "" {
				return m[2], true
			}
		}
	case "INSERT":
		if m := reSQLInsertTable.FindStringSubmatch(sql); m != nil {
			if m[2] != "" {
				return m[2], true
			}
		}
	case "UPDATE":
		if m := reSQLUpdateTable.FindStringSubmatch(sql); m != nil {
			if m[2] != "" {
				return m[2], true
			}
		}
	case "DELETE":
		if m := reSQLDeleteTable.FindStringSubmatch(sql); m != nil {
			if m[2] != "" {
				return m[2], true
			}
		}
	}
	return "", false
}

// csDapperSQLColumns extracts an explicit column list from a SQL literal.
// Returns nil if the statement uses SELECT * or the list is not determinable.
func csDapperSQLColumns(sql string) []string {
	verb := csDapperSQLVerb(sql)
	var rawList string
	switch verb {
	case "SELECT":
		m := reSQLSelectCols.FindStringSubmatch(sql)
		if m == nil {
			return nil
		}
		rawList = m[1]
		if strings.TrimSpace(rawList) == "*" {
			return nil // wildcard — not determinable
		}
	case "INSERT":
		m := reSQLInsertCols.FindStringSubmatch(sql)
		if m == nil {
			return nil
		}
		rawList = m[1]
	default:
		return nil
	}
	var cols []string
	for _, part := range strings.Split(rawList, ",") {
		part = strings.TrimSpace(part)
		// Strip alias (col AS alias or [col] AS alias) — take left side
		if idx := strings.Index(strings.ToUpper(part), " AS "); idx >= 0 {
			part = strings.TrimSpace(part[:idx])
		}
		// Strip table qualifier (t.col → col)
		if idx := strings.LastIndex(part, "."); idx >= 0 {
			part = part[idx+1:]
		}
		// Strip brackets
		part = strings.Trim(part, "[]`\"")
		if part == "" || part == "*" || strings.Contains(part, "(") {
			// Skip expressions / sub-selects / functions
			continue
		}
		cols = append(cols, part)
	}
	return cols
}

// csDapperPocoBody returns the source text of a named class body (content
// between the first '{' and its matching '}').  Returns "" if not found.
func csDapperPocoBody(src, className string) string {
	// Find "class <name>" ignoring modifiers and inheritance
	classRE := regexp.MustCompile(`(?m)\bclass\s+` + regexp.QuoteMeta(className) + `\b[^{]*\{`)
	loc := classRE.FindStringIndex(src)
	if loc == nil {
		return ""
	}
	start := loc[1] // position just after the opening '{'
	depth := 1
	for i := start; i < len(src) && depth > 0; i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start:i]
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *ormModelsExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.csharp_orm_models_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "csharp" {
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

	// Detect which ORM namespaces are present (controls framework tagging).
	isDapper := reDapperNS.MatchString(src)
	isLinqToSQL := reLinqToSQLNS.MatchString(src)
	isLinqToDB := reLinqToDBNS.MatchString(src)
	isNH := reNHibernateNS.MatchString(src) || reNHClassMap.MatchString(src) || reNHSession.MatchString(src)

	// -------------------------------------------------------------------------
	// Dapper POCO models
	// -------------------------------------------------------------------------

	if isDapper || reDapperTable.MatchString(src) {
		// [Table] attribute → model_extraction
		for _, m := range reDapperTable.FindAllStringSubmatchIndex(src, -1) {
			tableName := ""
			if m[2] >= 0 {
				tableName = src[m[2]:m[3]]
			}
			name := "dapper:table:" + tableName
			ent := makeEntity(name, "SCOPE.Component", "model_extraction", file.Path, "csharp", lineOf(src, m[0]))
			setProps(&ent, "framework", "dapper", "provenance", "INFERRED_FROM_DAPPER_TABLE",
				"table_name", tableName)
			add(ent)
		}

		// [Column] attribute → schema_extraction
		for _, m := range reDapperColumn.FindAllStringSubmatchIndex(src, -1) {
			colName := ""
			if m[2] >= 0 {
				colName = src[m[2]:m[3]]
			}
			name := "dapper:column:" + colName + ":" + file.Path + ":" + itoa(lineOf(src, m[0]))
			ent := makeEntity(name, "SCOPE.Pattern", "schema_extraction", file.Path, "csharp", lineOf(src, m[0]))
			setProps(&ent, "framework", "dapper", "provenance", "INFERRED_FROM_DAPPER_COLUMN",
				"column_name", colName)
			add(ent)
		}

		// POCO class declarations in files with [Table] → model_extraction
		if reDapperTable.MatchString(src) {
			for _, m := range reDapperClass.FindAllStringSubmatchIndex(src, -1) {
				name := src[m[2]:m[3]]
				if csharpPrimitives[name] {
					continue
				}
				ent := makeEntity("dapper:poco:"+name, "SCOPE.Component", "model_extraction", file.Path, "csharp", lineOf(src, m[0]))
				setProps(&ent, "framework", "dapper", "provenance", "INFERRED_FROM_DAPPER_POCO")
				add(ent)
			}
		}
	}

	// -------------------------------------------------------------------------
	// Dapper deep extraction: Query<T>("SQL") → full model_extraction +
	// query_attribution (verb+table attributed to T) + schema_extraction
	// (columns from SELECT/INSERT column lists).
	// -------------------------------------------------------------------------

	if isDapper || reDapperQueryFull.MatchString(src) {
		for _, m := range reDapperQueryFull.FindAllStringSubmatchIndex(src, -1) {
			line := lineOf(src, m[0])

			// method name (group 1)
			methodName := ""
			if m[2] >= 0 {
				methodName = src[m[2]:m[3]]
			}

			// type arg T (group 2) — may be absent for non-generic methods
			entityType := ""
			if m[4] >= 0 {
				entityType = src[m[4]:m[5]]
			}

			// SQL literal — prefer double-quoted group 3, fall back to single-quoted group 4
			sqlLit := ""
			if m[6] >= 0 {
				sqlLit = src[m[6]:m[7]]
			} else if m[8] >= 0 {
				sqlLit = src[m[8]:m[9]]
			}

			// -----------------------------------------------------------------
			// query_attribution: emit verb + table on the Operation entity
			// -----------------------------------------------------------------
			verb := csDapperSQLVerb(sqlLit)
			table, hasTable := csDapperSQLTable(sqlLit)

			qName := "dapper:query:" + entityType + ":" + file.Path + ":" + itoa(line)
			qEnt := makeEntity(qName, "SCOPE.Operation", "query_attribution", file.Path, "csharp", line)
			setProps(&qEnt, "framework", "dapper", "provenance", "INFERRED_FROM_DAPPER_QUERY_FULL",
				"entity_type", entityType,
				"method", methodName,
				"sql_verb", verb,
			)
			if hasTable {
				setProps(&qEnt, "sql_table", table)
			}
			if sqlLit != "" {
				// Store a truncated SQL snippet for debugging (max 200 chars)
				snippet := sqlLit
				if len(snippet) > 200 {
					snippet = snippet[:200] + "…"
				}
				setProps(&qEnt, "sql_snippet", snippet)
			}
			add(qEnt)

			// -----------------------------------------------------------------
			// model_extraction: emit the POCO type T referenced by the call,
			// then scan its class body for public auto-properties.
			// -----------------------------------------------------------------
			if entityType != "" && !csharpPrimitives[entityType] {
				mEnt := makeEntity("dapper:model:"+entityType, "SCOPE.Component", "model_extraction", file.Path, "csharp", line)
				setProps(&mEnt, "framework", "dapper", "provenance", "INFERRED_FROM_DAPPER_QUERY_TYPE")
				if hasTable {
					setProps(&mEnt, "table_name", table)
				}
				add(mEnt)

				// Scan the class body for public auto-properties
				body := csDapperPocoBody(src, entityType)
				if body != "" {
					for _, pm := range reDapperProp.FindAllStringSubmatchIndex(body, -1) {
						propType := strings.TrimSpace(body[pm[2]:pm[3]])
						propName := strings.TrimSpace(body[pm[4]:pm[5]])
						if csharpPrimitives[propName] {
							continue
						}
						pEnt := makeEntity(
							"dapper:prop:"+entityType+"."+propName,
							"SCOPE.Pattern", "model_extraction",
							file.Path, "csharp", line,
						)
						setProps(&pEnt, "framework", "dapper",
							"provenance", "INFERRED_FROM_DAPPER_POCO_PROP",
							"model", entityType,
							"property_name", propName,
							"property_type", propType,
						)
						add(pEnt)
					}
				}
			}

			// -----------------------------------------------------------------
			// schema_extraction: columns from SQL SELECT / INSERT column lists
			// -----------------------------------------------------------------
			if sqlLit != "" {
				cols := csDapperSQLColumns(sqlLit)
				for _, col := range cols {
					colEnt := makeEntity(
						"dapper:sql_col:"+col+":"+file.Path+":"+itoa(line),
						"SCOPE.Pattern", "schema_extraction",
						file.Path, "csharp", line,
					)
					setProps(&colEnt, "framework", "dapper",
						"provenance", "INFERRED_FROM_DAPPER_SQL_COLUMN",
						"column_name", col,
					)
					if entityType != "" {
						setProps(&colEnt, "model", entityType)
					}
					if hasTable {
						setProps(&colEnt, "table_name", table)
					}
					add(colEnt)
				}
			}
		}
	}

	// -------------------------------------------------------------------------
	// LINQ-to-SQL / LinqToDB
	// -------------------------------------------------------------------------

	if isLinqToSQL || isLinqToDB || reLinqContext.MatchString(src) {
		fwName := "linqtodb"
		if isLinqToSQL {
			fwName = "linq-to-sql"
		}

		// DataContext / DataConnection subclass → model_extraction (context)
		for _, m := range reLinqContext.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			ent := makeEntity(fwName+":context:"+name, "SCOPE.Component", "model_extraction", file.Path, "csharp", lineOf(src, m[0]))
			setProps(&ent, "framework", fwName, "provenance", "INFERRED_FROM_LINQ_CONTEXT")
			add(ent)
		}

		// [Table] attribute on classes → model_extraction
		for _, m := range reDapperTable.FindAllStringSubmatchIndex(src, -1) {
			tableName := ""
			if m[2] >= 0 {
				tableName = src[m[2]:m[3]]
			}
			name := fwName + ":table:" + tableName
			ent := makeEntity(name, "SCOPE.Component", "model_extraction", file.Path, "csharp", lineOf(src, m[0]))
			setProps(&ent, "framework", fwName, "provenance", "INFERRED_FROM_LINQ_TABLE",
				"table_name", tableName)
			add(ent)
		}

		// [Column] attribute → schema_extraction
		for _, m := range reDapperColumn.FindAllStringSubmatchIndex(src, -1) {
			colName := ""
			if m[2] >= 0 {
				colName = src[m[2]:m[3]]
			}
			name := fwName + ":column:" + colName + ":" + file.Path + ":" + itoa(lineOf(src, m[0]))
			ent := makeEntity(name, "SCOPE.Pattern", "schema_extraction", file.Path, "csharp", lineOf(src, m[0]))
			setProps(&ent, "framework", fwName, "provenance", "INFERRED_FROM_LINQ_COLUMN",
				"column_name", colName)
			add(ent)
		}

		// Table<T> property → schema entity
		for _, m := range reLinqTable.FindAllStringSubmatchIndex(src, -1) {
			entityType := src[m[2]:m[3]]
			if csharpPrimitives[entityType] {
				continue
			}
			name := fwName + ":table_prop:" + entityType
			ent := makeEntity(name, "SCOPE.Component", "schema_extraction", file.Path, "csharp", lineOf(src, m[0]))
			setProps(&ent, "framework", fwName, "provenance", "INFERRED_FROM_LINQ_TABLE_PROP",
				"entity_type", entityType)
			add(ent)
		}

		// [Association] attribute → relationship_extraction
		for _, m := range reLinqAssociation.FindAllStringIndex(src, -1) {
			name := fwName + ":association:" + file.Path + ":" + itoa(lineOf(src, m[0]))
			ent := makeEntity(name, "SCOPE.Pattern", "relationship_extraction", file.Path, "csharp", lineOf(src, m[0]))
			setProps(&ent, "framework", fwName, "provenance", "INFERRED_FROM_LINQ_ASSOCIATION")
			add(ent)
		}
	}

	// -------------------------------------------------------------------------
	// NHibernate / FluentNHibernate
	// -------------------------------------------------------------------------

	if isNH {
		// ClassMap<Entity> subclass → Models / schema
		for _, m := range reNHClassMap.FindAllStringSubmatchIndex(src, -1) {
			mapClass := src[m[2]:m[3]]
			entityClass := src[m[4]:m[5]]
			ent := makeEntity("nhibernate:classmap:"+mapClass, "SCOPE.Component", "model_extraction", file.Path, "csharp", lineOf(src, m[0]))
			setProps(&ent, "framework", "nhibernate", "provenance", "INFERRED_FROM_NH_CLASSMAP",
				"mapping_class", mapClass, "entity_class", entityClass)
			add(ent)

			// Also emit schema_extraction for the mapped entity
			entSchema := makeEntity("nhibernate:schema:"+entityClass, "SCOPE.Component", "schema_extraction", file.Path, "csharp", lineOf(src, m[0]))
			setProps(&entSchema, "framework", "nhibernate", "provenance", "INFERRED_FROM_NH_CLASSMAP",
				"entity_class", entityClass)
			add(entSchema)
		}

		// Map(x => x.Prop) → schema_extraction (column mapping)
		for _, m := range reNHMap.FindAllStringSubmatchIndex(src, -1) {
			prop := src[m[2]:m[3]]
			name := "nhibernate:map:" + prop + ":" + file.Path + ":" + itoa(lineOf(src, m[0]))
			ent := makeEntity(name, "SCOPE.Pattern", "schema_extraction", file.Path, "csharp", lineOf(src, m[0]))
			setProps(&ent, "framework", "nhibernate", "provenance", "INFERRED_FROM_NH_MAP",
				"property", prop)
			add(ent)
		}

		// References(x => x.Nav) → relationship_extraction (many-to-one)
		for _, m := range reNHReferences.FindAllStringSubmatchIndex(src, -1) {
			nav := src[m[2]:m[3]]
			name := "nhibernate:references:" + nav
			ent := makeEntity(name, "SCOPE.Pattern", "relationship_extraction", file.Path, "csharp", lineOf(src, m[0]))
			setProps(&ent, "framework", "nhibernate", "provenance", "INFERRED_FROM_NH_REFERENCES",
				"navigation_property", nav)
			add(ent)
		}

		// HasMany(x => x.Col) → relationship_extraction (one-to-many)
		for _, m := range reNHHasMany.FindAllStringSubmatchIndex(src, -1) {
			nav := src[m[2]:m[3]]
			name := "nhibernate:hasmany:" + nav
			ent := makeEntity(name, "SCOPE.Pattern", "relationship_extraction", file.Path, "csharp", lineOf(src, m[0]))
			setProps(&ent, "framework", "nhibernate", "provenance", "INFERRED_FROM_NH_HAS_MANY",
				"navigation_property", nav)
			add(ent)
		}

		// session.Query<T>() etc → query_attribution
		for _, m := range reNHQuery.FindAllStringSubmatchIndex(src, -1) {
			entityType := src[m[2]:m[3]]
			if csharpPrimitives[entityType] {
				continue
			}
			name := "nhibernate:query:" + entityType + ":" + file.Path + ":" + itoa(lineOf(src, m[0]))
			ent := makeEntity(name, "SCOPE.Operation", "query_attribution", file.Path, "csharp", lineOf(src, m[0]))
			setProps(&ent, "framework", "nhibernate", "provenance", "INFERRED_FROM_NH_QUERY",
				"entity_type", entityType)
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
