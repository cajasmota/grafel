package rust

// sea_query.go — custom extractor for the SeaQuery SQL query builder (Rust).
//
// SeaQuery is a dynamic SQL query builder (distinct from the SeaORM ORM,
// though often used alongside it). It builds statements through a fluent
// `Query::select()/insert()/update()/delete()` API whose target table is an
// `Iden` enum value and whose columns are `Iden` enum variants.
//
// Detects and emits entities for:
//
//   - Query::select()...from(Table)         → SCOPE.Pattern (subtype="query") + table prop
//   - Query::insert().into_table(Table)     → SCOPE.Pattern (subtype="query")
//   - Query::update().table(Table)          → SCOPE.Pattern (subtype="query")
//   - Query::delete().from_table(Table)     → SCOPE.Pattern (subtype="query")
//   - .columns([Table::Col, ...]) / .column(Table::Col)
//                                           → SCOPE.Component (subtype="schema_column")
//   - #[derive(Iden)] enum Foo { Table, Id, ... }
//                                           → SCOPE.Component (subtype="orm_model", iden table)
//
// Honesty:
//
//	partial — heuristic regex match on source text. The table identifier
//	captured is the in-code `Iden` enum name (e.g. `Users`), NOT necessarily
//	the physical SQL table string (which an `impl Iden` / `#[iden = "..."]`
//	may rename). Column lists spread across helper functions or built
//	dynamically are not resolved. Fixtures prove the detection surface;
//	full physical-name resolution requires Iden impl analysis.
//
// Issue #3558 — lang.rust.framework.sea_query (epic #3505).

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
	extractor.Register("custom_rust_sea_query", &rustSeaQueryExtractor{})
}

type rustSeaQueryExtractor struct{}

func (e *rustSeaQueryExtractor) Language() string { return "custom_rust_sea_query" }

// ---------------------------------------------------------------------------
// Regex catalog
// ---------------------------------------------------------------------------

var (
	// Query::select() / insert() / update() / delete() statement kickoff.
	reSeaQueryStmt = regexp.MustCompile(
		`\bQuery::(select|insert|update|delete)\s*\(\s*\)`,
	)

	// .from(Table) — SELECT/DELETE source table (Iden identifier).
	reSeaQueryFrom = regexp.MustCompile(
		`\.from\s*\(\s*([A-Za-z_]\w*)\s*\)`,
	)

	// .into_table(Table) — INSERT target.
	reSeaQueryIntoTable = regexp.MustCompile(
		`\.into_table\s*\(\s*([A-Za-z_]\w*)\s*\)`,
	)

	// .table(Table) — UPDATE target.
	reSeaQueryTable = regexp.MustCompile(
		`\.table\s*\(\s*([A-Za-z_]\w*)\s*\)`,
	)

	// .from_table(Table) — DELETE target (alternate form).
	reSeaQueryFromTable = regexp.MustCompile(
		`\.from_table\s*\(\s*([A-Za-z_]\w*)\s*\)`,
	)

	// .columns([Table::Col, Table::Col2]) — column projection list.
	// Group 1 = the bracket body.
	reSeaQueryColumns = regexp.MustCompile(
		`\.columns\s*\(\s*\[([^\]]+)\]\s*\)`,
	)

	// .column(Table::Col) — single column.
	reSeaQueryColumn = regexp.MustCompile(
		`\.column\s*\(\s*([A-Za-z_]\w*::[A-Za-z_]\w*)\s*\)`,
	)

	// A `Table::Column` Iden path token inside a column list.
	reSeaQueryIdenPath = regexp.MustCompile(
		`([A-Za-z_]\w*)::([A-Za-z_]\w*)`,
	)

	// #[derive(Iden)] enum TableName { ... } — Iden table-definition enum.
	reSeaQueryIdenDerive = regexp.MustCompile(
		`#\[derive\([^)]*\bIden\b[^)]*\)\]\s*(?:pub\s+)?enum\s+(\w+)`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *rustSeaQueryExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_sea_query_extractor.extract",
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
	// Cheap pre-gate: bail unless the SeaQuery surface is present.
	if !strings.Contains(src, "Query::") && !strings.Contains(src, "sea_query") &&
		!strings.Contains(src, "Iden") {
		return nil, nil
	}

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

	// 1. Statement detection — for each Query::<kind>() kickoff, look ahead a
	//    bounded window for the target table (via the kind-appropriate setter)
	//    and emit a `query` pattern carrying the statement kind + table.
	for _, sm := range reSeaQueryStmt.FindAllStringSubmatchIndex(src, -1) {
		stmtKind := src[sm[2]:sm[3]] // select | insert | update | delete
		tail := src[sm[1]:]
		if len(tail) > 800 {
			tail = tail[:800]
		}

		table := ""
		switch stmtKind {
		case "select":
			if mm := reSeaQueryFrom.FindStringSubmatch(tail); mm != nil {
				table = mm[1]
			}
		case "insert":
			if mm := reSeaQueryIntoTable.FindStringSubmatch(tail); mm != nil {
				table = mm[1]
			}
		case "update":
			if mm := reSeaQueryTable.FindStringSubmatch(tail); mm != nil {
				table = mm[1]
			}
		case "delete":
			if mm := reSeaQueryFromTable.FindStringSubmatch(tail); mm != nil {
				table = mm[1]
			} else if mm := reSeaQueryFrom.FindStringSubmatch(tail); mm != nil {
				table = mm[1]
			}
		}

		line := lineOf(src, sm[0])
		nameKey := stmtKind
		if table != "" {
			nameKey = stmtKind + ":" + table
		}
		ent := makeEntity("sea_query:query:"+nameKey, "SCOPE.Pattern", "query",
			file.Path, file.Language, line)
		setProps(&ent,
			"framework", "sea_query",
			"statement_kind", stmtKind,
			"table_name", table,
			"provenance", "INFERRED_FROM_SEA_QUERY_STATEMENT",
		)
		add(ent)

		// Columns referenced inside this statement's window.
		emitColumnsFrom(tail, file, line, add)
	}

	// 2. #[derive(Iden)] enum → table identifier definition (orm_model).
	//    The enum's first/`Table` variant conventionally names the table, but
	//    we record the enum name as the logical table identity.
	for _, m := range reSeaQueryIdenDerive.FindAllStringSubmatchIndex(src, -1) {
		idenName := src[m[2]:m[3]]
		ent := makeEntity("sea_query:iden:"+idenName, "SCOPE.Component", "orm_model",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "sea_query",
			"iden_enum", idenName,
			"table_name", idenName,
			"provenance", "INFERRED_FROM_SEA_QUERY_IDEN_DERIVE",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// emitColumnsFrom scans a statement window for `.columns([...])` and
// `.column(Table::Col)` projections and emits one schema_column entity per
// referenced `Table::Column` Iden path.
func emitColumnsFrom(window string, file extractor.FileInput, line int, add func(types.EntityRecord)) {
	emit := func(table, col string) {
		ent := makeEntity("sea_query:column:"+table+"."+col,
			"SCOPE.Component", "schema_column",
			file.Path, file.Language, line)
		setProps(&ent,
			"framework", "sea_query",
			"table_name", table,
			"column_name", col,
			"provenance", "INFERRED_FROM_SEA_QUERY_COLUMN",
		)
		add(ent)
	}

	for _, cm := range reSeaQueryColumns.FindAllStringSubmatchIndex(window, -1) {
		body := window[cm[2]:cm[3]]
		for _, pm := range reSeaQueryIdenPath.FindAllStringSubmatch(body, -1) {
			emit(pm[1], pm[2])
		}
	}
	for _, cm := range reSeaQueryColumn.FindAllStringSubmatch(window, -1) {
		if pm := reSeaQueryIdenPath.FindStringSubmatch(cm[1]); pm != nil {
			emit(pm[1], pm[2])
		}
	}
}
