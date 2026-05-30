package rust

// diesel.go — custom extractor for the Diesel ORM (Rust).
//
// Detects and emits entities for:
//
//   - table! {} macro declarations → SCOPE.Component (subtype="schema_table")
//   - #[derive(Queryable, Insertable, AsChangeset, ...)] struct annotations →
//     SCOPE.Component (subtype="orm_model") with the derive list in properties
//   - joinable!(table1 -> table2 (fk_col)) → SCOPE.Pattern (subtype="orm_relationship")
//   - #[belongs_to(Parent)] attribute → SCOPE.Pattern (subtype="orm_relationship")
//
// Honesty:
//
//	partial — heuristic regex match on source text. Does NOT perform
//	type-system analysis or resolve schema paths from diesel.toml.
//	Fixtures prove the detection surface; semantic cross-file resolution
//	requires import-graph analysis beyond this scanner.
//
// Issue #3269 — lang.rust.orm.diesel ORM build.

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
	extractor.Register("custom_rust_diesel", &rustDieselExtractor{})
}

type rustDieselExtractor struct{}

func (e *rustDieselExtractor) Language() string { return "custom_rust_diesel" }

// ---------------------------------------------------------------------------
// Regex catalog
// ---------------------------------------------------------------------------

var (
	// table! { users (id) { id -> Integer, name -> Text, } }
	// Captures table name.
	reDieselTable = regexp.MustCompile(
		`\btable!\s*\{\s*(\w+)\s*[\({]`,
	)

	// #[derive(Queryable)] / #[derive(Queryable, Insertable, AsChangeset)]
	// Followed (within a few lines) by struct Name.
	// We capture the derive list and then the struct name via a two-step scan.
	reDieselDerive = regexp.MustCompile(
		`#\[derive\([^)]*\b(?:Queryable|Insertable|AsChangeset|Identifiable|Associations|Selectable)\b[^)]*\)\]`,
	)
	reDieselDeriveList = regexp.MustCompile(
		`#\[derive\(([^)]+)\)\]`,
	)

	// struct Name following a diesel derive
	reStructName = regexp.MustCompile(`\bstruct\s+(\w+)`)

	// joinable!(posts -> users (user_id));
	reDieselJoinable = regexp.MustCompile(
		`\bjoinable!\s*\(\s*(\w+)\s*->\s*(\w+)\s*\(\s*(\w+)\s*\)\s*\)`,
	)

	// #[belongs_to(Parent)] / #[belongs_to(Parent, foreign_key = "parent_id")]
	reDieselBelongsTo = regexp.MustCompile(
		`#\[belongs_to\(\s*(\w+)(?:\s*,\s*[^)]+)?\s*\)\]`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *rustDieselExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_diesel_extractor.extract",
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

	// 1. table! {} macro → schema_table entity
	for _, m := range reDieselTable.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		ent := makeEntity("diesel:schema:"+tableName, "SCOPE.Component", "schema_table",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "diesel",
			"table_name", tableName,
			"provenance", "INFERRED_FROM_DIESEL_TABLE_MACRO",
		)
		add(ent)
	}

	// 2. #[derive(Queryable/Insertable/...)] struct → orm_model entity.
	//    We scan all derive attrs; for each diesel-bearing derive we look
	//    for the next struct declaration within 10 lines.
	deriveMatches := reDieselDerive.FindAllStringIndex(src, -1)
	for _, dm := range deriveMatches {
		// Full attribute text for the derive list.
		attrText := src[dm[0]:dm[1]]
		listMatch := reDieselDeriveList.FindStringSubmatch(attrText)
		deriveList := ""
		if len(listMatch) >= 2 {
			deriveList = listMatch[1]
		}

		// Scan forward from end of derive attr for `struct Name`.
		tail := src[dm[1]:]
		// Limit lookahead to the next ~500 characters (roughly 10 lines).
		if len(tail) > 500 {
			tail = tail[:500]
		}
		structMatch := reStructName.FindStringSubmatchIndex(tail)
		if structMatch == nil {
			continue
		}
		structName := tail[structMatch[2]:structMatch[3]]
		line := lineOf(src, dm[0])
		ent := makeEntity("diesel:model:"+structName, "SCOPE.Component", "orm_model",
			file.Path, file.Language, line)
		setProps(&ent,
			"framework", "diesel",
			"struct_name", structName,
			"derive_traits", strings.TrimSpace(deriveList),
			"provenance", "INFERRED_FROM_DIESEL_DERIVE",
		)
		add(ent)
	}

	// 3. joinable!(table1 -> table2 (fk)) → orm_relationship pattern
	for _, m := range reDieselJoinable.FindAllStringSubmatchIndex(src, -1) {
		fromTable := src[m[2]:m[3]]
		toTable := src[m[4]:m[5]]
		fkCol := src[m[6]:m[7]]
		name := "diesel:joinable:" + fromTable + "->" + toTable
		ent := makeEntity(name, "SCOPE.Pattern", "orm_relationship",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "diesel",
			"from_table", fromTable,
			"to_table", toTable,
			"foreign_key", fkCol,
			"relationship_type", "joinable",
			"provenance", "INFERRED_FROM_DIESEL_JOINABLE_MACRO",
		)
		add(ent)
	}

	// 4. #[belongs_to(Parent)] → orm_relationship pattern
	for _, m := range reDieselBelongsTo.FindAllStringSubmatchIndex(src, -1) {
		parent := src[m[2]:m[3]]
		name := "diesel:belongs_to:" + parent
		ent := makeEntity(name, "SCOPE.Pattern", "orm_relationship",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "diesel",
			"parent_model", parent,
			"relationship_type", "belongs_to",
			"provenance", "INFERRED_FROM_DIESEL_BELONGS_TO",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
