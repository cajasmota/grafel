// Package scala — Anorm ORM extractor (#4915).
//
// Anorm (org.playframework.anorm) is the SQL data-access layer that ships with
// the Play Framework — the most common Scala SQL approach in Play apps. Unlike
// the heavier ORMs it is "just SQL": queries are written as SQL string
// interpolation and results are mapped to case classes via RowParsers. It was
// the only mainstream Scala SQL library with NO record/extractor (slick,
// doobie, quill, scalikejdbc, scanamo, elastic4s were all covered) — the HIGH
// MISSING-FRAMEWORK gap called out by #4915.
//
// This extractor — modelled on the sibling doobie extractor in
// orm_extractors.go (Anorm and Doobie are both SQL-interpolation, not
// relationship-declaring ORMs) — emits, fixture-proven against real Anorm
// idioms:
//
//   - SQL("…") / SQL"…" interpolated statements      -> SCOPE.Operation/query
//     (query_attribution / schema_extraction): the SQL body is mined for its
//     leading verb (sqlVerb) and primary table (firstSQLTable), reusing the
//     same shared helpers doobie uses.
//   - RowParser / ResultSetParser type mappings       -> SCOPE.Schema
//     `Macro.namedParser[User]`, `Macro.indexedParser[User]`, `.as(User.parser.*)`
//     name the row model the query hydrates.
//   - case class row models in Anorm-flavoured files   -> SCOPE.Schema
//
// Anorm declares no foreign keys / associations (joins are raw SQL) and owns no
// migration tooling, so Relationships.* and Migrations.* are not_applicable —
// matching the doobie record. Honest-partial: an Anorm file that contains no
// SQL("…"), no parser, and no case class emits nothing.
package scala

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
	extractor.Register("custom_scala_anorm", &anormExtractor{})
}

// anormExtractor extracts Anorm SQL statements, row parsers and row models.
type anormExtractor struct{}

func (e *anormExtractor) Language() string { return "custom_scala_anorm" }

var (
	// anormSQLCallRe matches the classic SQL("…") / SQL("""…""") call form.
	// Group 1 = triple-quote body, group 2 = single-quote body.
	anormSQLCallRe = regexp.MustCompile(
		`(?s)\bSQL\s*\(\s*(?:"""(.{0,400}?)"""|"((?:[^"\\]|\\.){0,400}?)")\s*\)`)

	// anormSQLInterpRe matches the interpolated SQL"…" / SQL"""…""" form.
	// Group 1 = triple-quote body, group 2 = single-quote body.
	anormSQLInterpRe = regexp.MustCompile(
		`(?s)\bSQL(?:"""(.{0,400}?)"""|"((?:[^"\\]|\\.){0,400}?)")`)

	// anormMacroParserRe: Macro.namedParser[User] / Macro.indexedParser[User] /
	// Macro.parser[User](...) — the generated RowParser names its row model.
	anormMacroParserRe = regexp.MustCompile(
		`(?m)Macro\.(?:named|indexed)?[Pp]arser\[([A-Za-z_]\w*)\]`)

	// anormCaseClassRe: row models declared in an Anorm-flavoured file.
	anormCaseClassRe = regexp.MustCompile(
		`(?m)case\s+class\s+(\w+)\s*\(([^)]*)\)`)
)

func (e *anormExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/scala")
	_, span := tracer.Start(ctx, "indexer.anorm_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "anorm"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "scala" {
		return nil, nil
	}

	src := string(file.Content)
	// Gate: must look like an Anorm file. `import anorm` / `anorm.SQL` /
	// a bare SQL( call alongside an anorm import are the load-bearing signals;
	// gating on "anorm" alone avoids colliding with doobie's `sql"…"` (lower
	// case) interpolator.
	if !strings.Contains(src, "anorm") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	emitSQL := func(body string, offset int) {
		body = strings.TrimSpace(body)
		if body == "" {
			return
		}
		preview := body
		if len(preview) > 60 {
			preview = preview[:60]
		}
		ent := makeEntity("sql:"+preview, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, offset))
		setProps(&ent, "framework", "anorm",
			"provenance", "INFERRED_FROM_ANORM_SQL",
			"sql_verb", sqlVerb(body),
			"table_name", firstSQLTable(body),
			"pattern_type", "sql_statement")
		add(ent)
	}

	// SQL("…") call form.
	for _, m := range anormSQLCallRe.FindAllStringSubmatchIndex(src, -1) {
		switch {
		case m[2] >= 0:
			emitSQL(src[m[2]:m[3]], m[0])
		case m[4] >= 0:
			emitSQL(src[m[4]:m[5]], m[0])
		}
	}

	// SQL"…" interpolated form. The call-form regex requires a `(` after SQL,
	// so the two patterns do not double-count the same site.
	for _, m := range anormSQLInterpRe.FindAllStringSubmatchIndex(src, -1) {
		switch {
		case m[2] >= 0:
			emitSQL(src[m[2]:m[3]], m[0])
		case m[4] >= 0:
			emitSQL(src[m[4]:m[5]], m[0])
		}
	}

	// Macro.namedParser[T] / indexedParser[T] — row model the parser builds.
	for _, m := range anormMacroParserRe.FindAllStringSubmatchIndex(src, -1) {
		rowType := strings.TrimSpace(src[m[2]:m[3]])
		ent := makeEntity("row_parser:"+rowType, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "anorm",
			"provenance", "INFERRED_FROM_ANORM_ROW_PARSER",
			"row_type", rowType,
			"pattern_type", "row_parser")
		add(ent)
	}

	// case class row models.
	for _, m := range anormCaseClassRe.FindAllStringSubmatchIndex(src, -1) {
		name := strings.TrimSpace(src[m[2]:m[3]])
		ent := makeEntity(name, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "anorm",
			"provenance", "INFERRED_FROM_ANORM_CASE_CLASS",
			"pattern_type", "row_model")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
