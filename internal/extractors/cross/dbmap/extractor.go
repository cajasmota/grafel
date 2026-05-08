// Package dbmap implements the cross-language database-access extractor
// (MX-1050).
//
// Scans source files for database operations and emits SCOPE.DataAccess
// entities together with ACCESSES_TABLE relationship edges that link each
// enclosing function to the table it reads or writes.
//
// Supported detection paths:
//
//   - Raw SQL string literals (all languages): SELECT / INSERT / UPDATE /
//     DELETE / TRUNCATE / UPSERT / JOIN clauses are recognised from the
//     FROM/INTO/UPDATE/JOIN tokens, one SCOPE.DataAccess entity per table.
//   - ORM / driver dispatch (import-gated):
//     GORM, database/sql (Go),
//     SQLAlchemy, psycopg2 (Python),
//     Hibernate/JPA (Java),
//     ActiveRecord (Ruby),
//     Ecto (Elixir),
//     Prisma, TypeORM (TypeScript),
//     Sequelize (JavaScript/TypeScript),
//     Diesel (Rust).
//
// Entity kind:         "SCOPE.DataAccess"
// Relationship kind:   "ACCESSES_TABLE" (enclosing function → SCOPE.DataAccess)
//
// OTel span:   indexer.data_access_extract
// Attributes:  language, orm, table_count, file_path
//
// Registration key: "_cross_dbmap"
//
// The extractor short-circuits when no known DB driver / ORM import is
// present in the file, so the hot path on non-DB files is a handful of
// regex matches over the import list only.
package dbmap

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("_cross_dbmap", &Extractor{})
}

// Extractor implements extractor.Extractor for database-access mapping.
type Extractor struct{}

// Language returns the registration key.
func (e *Extractor) Language() string { return "_cross_dbmap" }

// ---------------------------------------------------------------------------
// Constants — operation + kind
// ---------------------------------------------------------------------------

const (
	// KindDataAccess is the EntityRecord.Kind value emitted by this extractor.
	KindDataAccess = "SCOPE.DataAccess"
	// RelAccessesTable is the RelationshipRecord.Kind value emitted.
	RelAccessesTable = "ACCESSES_TABLE"

	OpSelect   = "SELECT"
	OpInsert   = "INSERT"
	OpUpdate   = "UPDATE"
	OpDelete   = "DELETE"
	OpUpsert   = "UPSERT"
	OpTruncate = "TRUNCATE"

	// UnknownTable marks a resolved query where the table name could not
	// be statically determined (dynamic string, variable interpolation).
	UnknownTable = "UNKNOWN"

	// queryPatternMax is the max length stored in the query_pattern property.
	queryPatternMax = 500
)

// ---------------------------------------------------------------------------
// Access record — internal detection result
// ---------------------------------------------------------------------------

// access is the intermediate per-hit struct produced by a detector.
type access struct {
	table     string
	operation string
	orm       string
	pattern   string // sanitised query pattern (may be "")
	// functionQName is the qualified name of the enclosing function, if
	// known. When empty, the ACCESSES_TABLE edge is emitted with a
	// best-effort source-file-scoped ref (see accessBuilder).
	functionQName string
}

// ---------------------------------------------------------------------------
// Entity / relationship builders
// ---------------------------------------------------------------------------

// dataAccessEntityID builds a stable identity string for a SCOPE.DataAccess.
// Format: "scope:dataaccess:<file>#<orm>:<op>:<table>".
func dataAccessEntityID(filePath, orm, op, table string) string {
	return "scope:dataaccess:" + filePath + "#" + orm + ":" + op + ":" + table
}

// functionRef builds the source ref for an ACCESSES_TABLE edge.
// When the enclosing function qualified name is empty we fall back to a
// file-level ref so the edge is still emitted but is clearly degraded.
func functionRef(filePath, qname string) string {
	if qname == "" {
		return "scope:operation:" + filePath + "#_file_scope"
	}
	return "scope:operation:" + filePath + "#" + qname
}

// buildEntity assembles a SCOPE.DataAccess EntityRecord plus its
// ACCESSES_TABLE edge from the enclosing function.
func buildEntity(filePath, language string, a access) types.EntityRecord {
	entityID := dataAccessEntityID(filePath, a.orm, a.operation, a.table)

	pattern := sanitisePattern(a.pattern)

	props := map[string]string{
		"table":         a.table,
		"operation":     a.operation,
		"orm":           a.orm,
		"query_pattern": pattern,
		"function_ref":  a.functionQName,
		"ref":           entityID,
		"provenance":    "INFERRED_FROM_DB_ACCESS",
	}

	rec := types.EntityRecord{
		Name:         a.operation + " " + a.table,
		Kind:         KindDataAccess,
		SourceFile:   filePath,
		Language:     language,
		Subtype:      a.orm,
		Properties:   props,
		QualityScore: 0.8,
	}

	fromRef := functionRef(filePath, a.functionQName)
	rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
		FromID: fromRef,
		ToID:   entityID,
		Kind:   RelAccessesTable,
		Properties: map[string]string{
			"function_qname": a.functionQName,
			"orm":            a.orm,
			"operation":      a.operation,
			"table":          a.table,
		},
	})

	return rec
}

// sanitisePattern replaces string/number literals in a SQL-ish query with
// "?" placeholders and truncates to queryPatternMax characters.
func sanitisePattern(q string) string {
	if q == "" {
		return ""
	}
	s := strings.TrimSpace(q)
	s = literalStringRE.ReplaceAllString(s, "?")
	s = literalNumberRE.ReplaceAllString(s, "?")
	s = whitespaceRunRE.ReplaceAllString(s, " ")
	if len(s) > queryPatternMax {
		s = s[:queryPatternMax]
	}
	return s
}

var (
	literalStringRE = regexp.MustCompile(`'[^']*'|"[^"]*"`)
	literalNumberRE = regexp.MustCompile(`\b\d+\b`)
	whitespaceRunRE = regexp.MustCompile(`\s+`)
)

// ---------------------------------------------------------------------------
// Extract implements extractor.Extractor
// ---------------------------------------------------------------------------

// Extract scans a source file for database access and emits SCOPE.DataAccess
// entities + ACCESSES_TABLE edges. Files with no recognised ORM/driver
// import return an empty slice (per Behaviour Rule #1 of MX-1050).
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor._cross_dbmap")
	_, span := tracer.Start(ctx, "indexer.data_access_extract")
	defer span.End()

	span.SetAttributes(
		attribute.String("file_path", file.Path),
		attribute.String("language", file.Language),
	)

	source := string(file.Content)
	if source == "" {
		span.SetAttributes(
			attribute.String("orm", ""),
			attribute.Int("table_count", 0),
		)
		return nil, nil
	}

	tokens := extractImportTokens(source)
	orms := selectORMs(tokens)
	if len(orms) == 0 {
		// Rule #1: unrecognised ORM/driver → skip entirely.
		span.SetAttributes(
			attribute.String("orm", ""),
			attribute.Int("table_count", 0),
		)
		return nil, nil
	}

	var accesses []access
	for _, o := range orms {
		accesses = append(accesses, o.detect(source)...)
	}

	// Filter empty tables up-front; set UNKNOWN when we had a detection
	// hit but could not resolve the table.
	out := make([]types.EntityRecord, 0, len(accesses))
	primaryORM := orms[0].name
	for _, a := range accesses {
		if a.table == "" {
			a.table = UnknownTable
		}
		if a.operation == "" {
			a.operation = OpSelect
		}
		out = append(out, buildEntity(file.Path, file.Language, a))
	}

	span.SetAttributes(
		attribute.String("orm", primaryORM),
		attribute.Int("table_count", len(out)),
	)
	return out, nil
}
