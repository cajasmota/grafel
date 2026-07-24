// norm_orm.go — Nim Norm ORM model → table/column schema synthesis (#4904).
//
// Norm (https://norm.nim.town) is the de-facto Nim ORM. A persisted model is a
// plain Nim `ref object` that inherits from Norm's `Model` base type; Norm maps
// the object to a database table whose name is, by default, the snake_case
// pluralisation of the type name (Norm lowercases the type name and appends
// nothing structural at the Nim level — the runtime applies the table naming —
// so we record the TYPE NAME as the table identity, which is what the model
// object is keyed by). Each public object field becomes a column; a field typed
// as another Model subtype (or carrying an `{.fk: Other.}` pragma) is a foreign
// key to that model.
//
// Norm model shape:
//
//	import norm/model
//
//	type
//	  User* = ref object of Model
//	    name*: string
//	    email*: string
//	    age*: int
//
//	  Post* = ref object of Model
//	    title*: string
//	    body*: string
//	    author*: User          # FK → User (field typed as a Model subtype)
//
// What this extractor emits (mirrors the PHP/Eloquent + Scala ORM shape —
// SCOPE.Schema entities carrying framework+provenance props):
//   - one SCOPE.Schema/model per `T* = ref object of Model` declaration
//   - one SCOPE.Schema/table per model (table identity = the model type name)
//   - one SCOPE.Schema/column per public object field, with column_type stamped
//   - a REFERENCES edge model → referenced model for a field typed as another
//     model type in the same file (the FK signal), keyed by model name
//
// Deepening (#4932) — this extractor additionally reads:
//   - `{.tableName: "x".}` / `{.dbName: "x".}` pragma table-name overrides on the
//     model header: the table entity is keyed by the override name and the model
//     carries a `table_name` property (table identity is no longer forced to the
//     Nim type name when an override is present).
//   - column-level pragmas on a field: `{.unique.}` → `unique=true`,
//     `{.dbType: "TEXT".}` → `db_type=TEXT`, and an explicit `{.fk: Other.}`
//     pragma → REFERENCES edge to `Other` even when the field is typed as a plain
//     id (e.g. `userId* {.fk: User.}: int64`).
//   - query attribution: a `db.select/insert/update/delete(model, …)` call site
//     referencing a known model emits a QUERIES edge model → its table stamped
//     with the SQL operation (file-local; the model handle must be a recognised
//     model type name).
//   - transaction stamping: a `db.transaction:` block emits a
//     SCOPE.Pattern/transaction_boundary entity (transactional=true), mirroring
//     the Kotlin/Java @Transactional shape.
//
// Honest exclusions / follow-ups (no fabricated schema):
//   - cross-file FK targets (a field typed as a model declared in another file)
//     are recorded as a REFERENCES edge to the bare type name but not resolved
//     to the concrete entity here — the shared resolver handles binding.
//   - Norm migrations (createTables/dropTables/migration procs) and column
//     index pragmas beyond unique/dbType remain follow-ups (#4932 → see PR).
//   - Allographer schema→table/column mapping is covered by allographer_orm.go
//     (#4933); ormin / Debby model→table mapping remains follow-up (#5028).
//
// Registration key: "custom_nim_norm_orm".
package nim

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_nim_norm_orm", &nimNormORMExtractor{})
}

type nimNormORMExtractor struct{}

func (e *nimNormORMExtractor) Language() string { return "custom_nim_norm_orm" }

var (
	// nimNormModelRe matches a Norm model declaration: a type that is a
	// `ref object of Model`. Capture group 1 is the model type name (export
	// marker stripped by the caller), group 2 the optional header pragma block
	// body (e.g. `tableName: "users"` from
	// `User* {.tableName: "users".} = ref object of Model`).
	nimNormModelRe = regexp.MustCompile(
		`(?m)^[ \t]*([A-Z][A-Za-z0-9_]*)\*?\s*(?:\{\.([^}]*?)\.?\})?\s*=\s*ref\s+object\s+of\s+Model\b`)

	// nimNormFieldRe matches a single object field inside a model body:
	// `name*: string`, `author*: User`, `age: int`, and pragma-bearing forms
	// `userId* {.fk: User.}: int64` / `email* {.unique.}: string`. Capture group
	// 1 is the field name (export marker stripped), group 2 the optional field
	// pragma block body, group 3 the field type.
	nimNormFieldRe = regexp.MustCompile(
		`(?m)^[ \t]+([a-z_][A-Za-z0-9_]*)\*?\s*(?:\{\.([^}]*?)\.?\})?\s*:\s*([A-Za-z_][A-Za-z0-9_\[\], ]*)`)

	// nimNormTableNameRe extracts a `tableName: "x"` or `dbName: "x"` table-name
	// override from a model header pragma block body.
	nimNormTableNameRe = regexp.MustCompile(`\b(?:tableName|dbName)\s*:\s*"([^"]+)"`)

	// nimNormDbTypeRe extracts an explicit column SQL type from a field pragma
	// (`dbType: "TEXT"`). nimNormFkPragmaRe extracts an explicit FK target type
	// from a field pragma (`fk: Other`). nimNormUniqueRe is the unique marker.
	nimNormDbTypeRe   = regexp.MustCompile(`\bdbType\s*:\s*"([^"]+)"`)
	nimNormFkPragmaRe = regexp.MustCompile(`\bfk\s*:\s*([A-Z][A-Za-z0-9_]*)`)
	nimNormUniqueRe   = regexp.MustCompile(`\bunique\b`)

	// nimNormQueryRe matches a Norm query call site: `db.select(post, …)`,
	// `db.insert(user)`, `dbConn.update(p)`, `db.delete(c)`. Group 1 is the SQL
	// operation, group 2 the first argument identifier (the model handle).
	nimNormQueryRe = regexp.MustCompile(
		`(?m)\b[A-Za-z_][A-Za-z0-9_]*\s*\.\s*(select|insert|update|delete)\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)`)

	// nimNormHandleBindModelRe binds a `let/var x = Model()` (or
	// `var x = @[Model()]` seq) handle to its model type so a variable-handle
	// query `db.select(x, …)` resolves to the model (#4991). Group 1 = the handle
	// identifier, group 2 = the model constructor type name.
	nimNormHandleBindModelRe = regexp.MustCompile(
		`(?m)\b(?:let|var|const)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?::[^\n=]+)?=\s*@?\[?\s*([A-Z][A-Za-z0-9_]*)\s*\(`)

	// nimNormRawSelectRe matches a raw-SQL Norm query whose table is named in a
	// `sql"… from <table> …"` string: `db.select(objs, sql"SELECT * FROM users …")`
	// / `db.rawSelect(objs, sql"… FROM posts …")`. Group 1 = the SQL operation,
	// group 2 = the table name parsed out of the FROM/INTO/UPDATE clause.
	nimNormRawSelectRe = regexp.MustCompile(
		`(?is)\.\s*(?:raw)?(select|insert|update|delete)\s*\([^)]*?sql?\s*["` + "`" + `][^"` + "`" + `]*?\b(?:from|into|update)\s+["` + "`" + `]?([A-Za-z_][A-Za-z0-9_]*)`)

	// nimNormTxRe matches a Norm transaction block header `db.transaction:` /
	// `dbConn.transaction:`. Group 1 is the receiver (the db handle).
	nimNormTxRe = regexp.MustCompile(`(?m)^[ \t]*([A-Za-z_][A-Za-z0-9_]*)\s*\.\s*transaction\s*:`)

	// nimNormProcRe matches a Nim proc/func declaration header so a
	// `db.transaction:` block can be attributed to its enclosing proc (#4991).
	// Group 1 = the proc name.
	nimNormProcRe = regexp.MustCompile(`(?m)^[ \t]*(?:proc|func|method)\s+([A-Za-z_][A-Za-z0-9_]*)`)
)

// nimNormHasModel is a fast pre-filter: the file must reference Norm's Model
// base type to be worth scanning, so we never misfire on arbitrary Nim objects.
func nimNormHasModel(content string) bool {
	return strings.Contains(content, "of Model") &&
		(strings.Contains(content, "norm") || strings.Contains(content, "Model"))
}

func (e *nimNormORMExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "nim" {
		return nil, nil
	}
	src := string(file.Content)
	if !nimNormHasModel(src) {
		return nil, nil
	}

	models := collectNormModels(src)
	if len(models) == 0 {
		return nil, nil
	}
	// Set of known model names in this file — used to recognise a field whose
	// type is another model as a foreign key.
	modelNames := make(map[string]bool, len(models))
	for _, m := range models {
		modelNames[m.name] = true
	}

	// Resolve `let/var x = Model()` handles so a variable-handle query
	// `db.select(x, …)` binds to its model type (#4991).
	handles := make(map[string]string)
	for _, m := range nimNormHandleBindModelRe.FindAllStringSubmatch(src, -1) {
		if modelNames[m[2]] {
			handles[m[1]] = m[2]
		}
	}
	// tableToModel maps a resolved table identity → its model name so a raw-SQL
	// query naming a table (`sql"… FROM users"`) is attributed to that model.
	tableToModel := make(map[string]string, len(models))
	for _, m := range models {
		tid := m.name
		if m.tableName != "" {
			tid = m.tableName
		}
		tableToModel[tid] = m.name
	}

	// queryOps maps a model name → the set of SQL operations attributed to it by
	// db.<op>(model) / db.<op>(handle) / raw-SQL call sites elsewhere in the file.
	queryOps := collectNormQueries(src, modelNames, handles, tableToModel)

	var out []types.EntityRecord
	for _, m := range models {
		// Table identity: a {.tableName/.dbName.} override wins; else the type name.
		tableID := m.name
		if m.tableName != "" {
			tableID = m.tableName
		}

		// 1. model entity
		model := newNormSchema(m.name, "model", file.Path, m.line,
			"INFERRED_FROM_NORM_MODEL")
		if m.tableName != "" {
			model.Properties["table_name"] = m.tableName
		}
		// FK + query edges.
		var rels []types.RelationshipRecord
		// FK edges → referenced models: a field typed as another model, OR an
		// explicit {.fk: Other.} pragma on a scalar-typed field.
		for _, f := range m.fields {
			target := ""
			switch {
			case f.fkTarget != "" && f.fkTarget != m.name:
				target = f.fkTarget
			case modelNames[f.typ] && f.typ != m.name:
				target = f.typ
			}
			if target == "" {
				continue
			}
			props := types.Props{{K: "fk_field", V: f.name}, {K: "to_model", V: target}}
			if f.fkTarget != "" {
				props.Set("fk_pragma", "true")
			}
			rels = append(rels, types.RelationshipRecord{
				ToID: target, Kind: "REFERENCES", Properties: props,
			})
		}
		// Query attribution: model → its table, one edge per attributed op.
		if ops := queryOps[m.name]; len(ops) > 0 {
			for _, op := range normQueryOpOrder(ops) {
				rels = append(rels, types.RelationshipRecord{
					ToID: tableID,
					Kind: "QUERIES",
					Properties: types.Props{
						{K: "model", V: m.name},
						{K: "operation", V: op},
						{K: "table", V: tableID},
					},
				})
			}
		}
		model.Relationships = rels
		model.ID = model.ComputeID()
		out = append(out, model)

		// 2. table entity (identity = override or model type name).
		table := newNormSchema(tableID, "table", file.Path, m.line,
			"INFERRED_FROM_NORM_TABLE")
		table.Properties["model"] = m.name
		table.ID = table.ComputeID()
		out = append(out, table)

		// 3. column entities (one per public object field).
		colSeen := make(map[string]bool)
		for _, f := range m.fields {
			if colSeen[f.name] {
				continue
			}
			colSeen[f.name] = true
			col := newNormSchema(f.name, "column", file.Path, f.line,
				"INFERRED_FROM_NORM_FIELD")
			col.Properties["column_type"] = f.typ
			col.Properties["model"] = m.name
			if f.dbType != "" {
				col.Properties["db_type"] = f.dbType
			}
			if f.unique {
				col.Properties["unique"] = "true"
			}
			fkTarget := f.fkTarget
			if fkTarget == "" && modelNames[f.typ] && f.typ != m.name {
				fkTarget = f.typ
			}
			if fkTarget != "" && fkTarget != m.name {
				col.Properties["foreign_key"] = "true"
				col.Properties["fk_target"] = fkTarget
			}
			col.ID = col.ComputeID()
			out = append(out, col)
		}
	}

	// 4. transaction boundaries: one SCOPE.Pattern/transaction_boundary per
	// db.transaction: block.
	out = append(out, collectNormTransactions(src, file.Path)...)

	return out, nil
}

// collectNormQueries scans db.<op>(…) call sites and returns, per model name,
// the set of SQL operations (select/insert/update/delete) attributed to it.
// Three first-argument forms are attributed (#4991):
//   - a recognised model TYPE — `db.select(User, …)`;
//   - a variable handle bound to a model — `var u = User()` … `db.select(u, …)`,
//     resolved via the `handles` map;
//   - a raw-SQL query naming a table — `db.select(objs, sql"… FROM users …")` /
//     `db.rawSelect(…)`, resolved via `tableToModel`.
//
// A handle/table that resolves to no known model is intentionally skipped.
func collectNormQueries(
	src string,
	modelNames map[string]bool,
	handles map[string]string,
	tableToModel map[string]string,
) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	add := func(model, op string) {
		if out[model] == nil {
			out[model] = map[string]bool{}
		}
		out[model][op] = true
	}
	for _, m := range nimNormQueryRe.FindAllStringSubmatch(src, -1) {
		op, arg := m[1], m[2]
		switch {
		case modelNames[arg]:
			add(arg, op) // model-typed first argument
		case handles[arg] != "":
			add(handles[arg], op) // variable handle bound to a model
		}
	}
	// Raw-SQL queries: resolve the FROM/INTO/UPDATE table to its model.
	for _, m := range nimNormRawSelectRe.FindAllStringSubmatch(src, -1) {
		op, table := m[1], m[2]
		if model := tableToModel[table]; model != "" {
			add(model, op)
		}
	}
	return out
}

// normQueryOpOrder returns the operations in a stable order for deterministic
// edge emission.
func normQueryOpOrder(ops map[string]bool) []string {
	var out []string
	for _, op := range []string{"select", "insert", "update", "delete"} {
		if ops[op] {
			out = append(out, op)
		}
	}
	return out
}

// collectNormTransactions emits a SCOPE.Pattern/transaction_boundary entity per
// `db.transaction:` block header in the file (transactional=true), mirroring the
// Kotlin/Java @Transactional boundary shape. #4991 deepening: the boundary is
// stamped with its enclosing proc (the nearest preceding proc/func/method header
// the block is indented under) and with the set of write operations
// (insert/update/delete) issued inside the transaction body, so the boundary
// records WHAT it wraps, not merely WHERE it opens.
func collectNormTransactions(src, path string) []types.EntityRecord {
	idx := nimNormTxRe.FindAllStringSubmatchIndex(src, -1)
	if len(idx) == 0 {
		return nil
	}
	lines := strings.Split(src, "\n")
	// procAt maps each line to the name of the proc it is lexically inside, by a
	// single forward indent-tracking scan over the proc headers.
	procAt := buildNormProcMap(lines)
	var out []types.EntityRecord
	for _, m := range idx {
		recv := src[m[2]:m[3]]
		line := strings.Count(src[:m[0]], "\n") + 1
		txIndent := leadingIndent(lineAt(lines, line))
		props := map[string]string{
			"framework":     "norm",
			"transactional": "true",
			"db_handle":     recv,
			"provenance":    "INFERRED_FROM_NORM_TRANSACTION",
		}
		if proc := procAt[line]; proc != "" {
			props["enclosing_proc"] = proc
		}
		// In-block writes: scan the indented body of the transaction for
		// db.<insert|update|delete>(…) call sites and flag them on the boundary.
		if writes := normTxWrites(lines, line, txIndent); writes != "" {
			props["writes"] = writes
			props["has_writes"] = "true"
		}
		ent := types.EntityRecord{
			Name:       recv + ".transaction",
			Kind:       "SCOPE.Pattern",
			Subtype:    "transaction_boundary",
			SourceFile: path,
			Language:   "nim",
			StartLine:  line,
			EndLine:    line,
			Properties: props,
		}
		ent.ID = ent.ComputeID()
		out = append(out, ent)
	}
	return out
}

// buildNormProcMap returns, per 1-based line, the name of the proc/func/method
// the line is lexically nested inside (or "" at top level). A proc body is every
// subsequent line indented strictly more than the proc header, up to the next
// line dedented to or below the header.
func buildNormProcMap(lines []string) map[int]string {
	out := make(map[int]string)
	// Single scan: track the innermost open proc by header indent.
	type frame struct {
		name   string
		indent int
	}
	var open *frame
	for ln := 1; ln <= len(lines); ln++ {
		raw := lineAt(lines, ln)
		if strings.TrimSpace(raw) == "" {
			if open != nil {
				out[ln] = open.name
			}
			continue
		}
		ind := leadingIndent(raw)
		if pm := nimNormProcRe.FindStringSubmatch(raw); pm != nil {
			open = &frame{name: pm[1], indent: ind}
			out[ln] = open.name
			continue
		}
		if open != nil && ind > open.indent {
			out[ln] = open.name
		} else {
			open = nil
		}
	}
	return out
}

// normTxWrites scans the indented body of a `db.transaction:` block (starting
// after headerLine, body = lines indented strictly more than txIndent) for
// db.<insert|update|delete>(…) call sites and returns the distinct write ops in
// stable order, comma-joined ("" when the block issues no writes).
func normTxWrites(lines []string, headerLine, txIndent int) string {
	found := map[string]bool{}
	for ln := headerLine + 1; ln <= len(lines); ln++ {
		raw := lineAt(lines, ln)
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if leadingIndent(raw) <= txIndent {
			break // dedent — transaction block ended
		}
		if qm := nimNormQueryRe.FindStringSubmatch(raw); qm != nil {
			switch qm[1] {
			case "insert", "update", "delete":
				found[qm[1]] = true
			}
		}
	}
	var ws []string
	for _, op := range []string{"insert", "update", "delete"} {
		if found[op] {
			ws = append(ws, op)
		}
	}
	return strings.Join(ws, ",")
}

// normModel is a parsed Norm model with its fields.
type normModel struct {
	name      string
	tableName string // {.tableName/.dbName.} override; empty → type name is the table
	line      int
	fields    []normField
}

type normField struct {
	name     string
	typ      string
	dbType   string // {.dbType: "TEXT".}
	fkTarget string // {.fk: Other.}
	unique   bool   // {.unique.}
	line     int
}

// collectNormModels finds every `T = ref object of Model` declaration and the
// fields in its indented body.
func collectNormModels(src string) []normModel {
	idx := nimNormModelRe.FindAllStringSubmatchIndex(src, -1)
	if len(idx) == 0 {
		return nil
	}
	lines := strings.Split(src, "\n")
	var models []normModel
	for _, m := range idx {
		name := src[m[2]:m[3]]
		var tableName string
		if m[4] >= 0 { // header pragma block captured
			if pm := nimNormTableNameRe.FindStringSubmatch(src[m[4]:m[5]]); pm != nil {
				tableName = pm[1]
			}
		}
		startLine := strings.Count(src[:m[0]], "\n") + 1
		modelIndent := leadingIndent(lineAt(lines, startLine))
		fields := collectNormFields(lines, startLine, modelIndent)
		models = append(models, normModel{
			name: name, tableName: tableName, line: startLine, fields: fields,
		})
	}
	return models
}

// collectNormFields scans the indented body following a model header for object
// fields. A field line is more indented than the model header; the body ends at
// the first non-blank line indented at or below the model header.
func collectNormFields(lines []string, headerLine, modelIndent int) []normField {
	var fields []normField
	seen := make(map[string]bool)
	for ln := headerLine + 1; ln <= len(lines); ln++ {
		raw := lineAt(lines, ln)
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if leadingIndent(raw) <= modelIndent {
			break // dedent — model body ended
		}
		fm := nimNormFieldRe.FindStringSubmatch(raw)
		if fm == nil {
			continue
		}
		fname := fm[1]
		pragma := fm[2]
		ftyp := normaliseNimFieldType(fm[3])
		if ftyp == "" || seen[fname] {
			continue
		}
		seen[fname] = true
		f := normField{name: fname, typ: ftyp, line: ln}
		if pragma != "" {
			if dm := nimNormDbTypeRe.FindStringSubmatch(pragma); dm != nil {
				f.dbType = dm[1]
			}
			if km := nimNormFkPragmaRe.FindStringSubmatch(pragma); km != nil {
				f.fkTarget = km[1]
			}
			if nimNormUniqueRe.MatchString(pragma) {
				f.unique = true
			}
		}
		fields = append(fields, f)
	}
	return fields
}

// normaliseNimFieldType reduces a field type expression to its core type name:
// `Option[User]` → `User`, `seq[Post]` → `Post`, `string` → `string`. The
// wrapper (Option/seq) is unwrapped so a wrapped model reference is still
// recognised as a foreign key.
func normaliseNimFieldType(raw string) string {
	t := strings.TrimSpace(raw)
	// Unwrap Option[...] / seq[...] generics to the inner type.
	for {
		open := strings.IndexByte(t, '[')
		if open < 0 {
			break
		}
		close := strings.LastIndexByte(t, ']')
		if close <= open {
			break
		}
		t = strings.TrimSpace(t[open+1 : close])
	}
	// Take the first whitespace-delimited token (drops trailing pragmas/comments).
	if sp := strings.IndexAny(t, " \t"); sp >= 0 {
		t = t[:sp]
	}
	return strings.TrimSpace(t)
}

// newNormSchema builds a SCOPE.Schema entity with the Norm framework + the given
// provenance stamp.
func newNormSchema(name, subtype, path string, line int, provenance string) types.EntityRecord {
	return types.EntityRecord{
		Name:       name,
		Kind:       "SCOPE.Schema",
		Subtype:    subtype,
		SourceFile: path,
		Language:   "nim",
		StartLine:  line,
		EndLine:    line,
		Properties: map[string]string{
			"framework":  "norm",
			"provenance": provenance,
		},
	}
}

// leadingIndent counts leading spaces/tabs of a line (tab counts as 1).
func leadingIndent(line string) int {
	n := 0
	for _, ch := range line {
		if ch == ' ' || ch == '\t' {
			n++
		} else {
			break
		}
	}
	return n
}

// lineAt returns the 1-based line ln from lines, or "" when out of range.
func lineAt(lines []string, ln int) string {
	if ln < 1 || ln > len(lines) {
		return ""
	}
	return lines[ln-1]
}
