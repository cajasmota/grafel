package python

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
	extractor.Register("python_sqlalchemy", &SQLAlchemyExtractor{})
}

// SQLAlchemyExtractor extracts SQLAlchemy patterns: declarative models,
// relationships, session queries, engines, hybrid properties, events,
// and association tables.
type SQLAlchemyExtractor struct{}

func (e *SQLAlchemyExtractor) Language() string { return "python_sqlalchemy" }

var (
	saDeclarativeModelRe = regexp.MustCompile(`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\(([^)]*)\)\s*:`)
	saTablenameRe        = regexp.MustCompile(`__tablename__\s*=\s*["']([^"']+)["']`)
	saMappedAnnotationRe = regexp.MustCompile(`:\s*Mapped\s*\[`)
	saRelationshipRe     = regexp.MustCompile(
		`(?m)^\s+(\w+)\s*(?::\s*Mapped\[[^\]]*\])?\s*=\s*relationship\s*\(\s*["']([A-Za-z_][A-Za-z0-9_]*)["']([^)]*)\)`)
	saForeignKeyRe       = regexp.MustCompile(`ForeignKey\s*\(\s*["']([^"']+)["']`)
	saAssocTableRe       = regexp.MustCompile(`(?m)^(\w+)\s*=\s*Table\s*\(\s*["']([^"']+)["']`)
	saCreateEngineRe     = regexp.MustCompile(`(?m)(\w+)\s*=\s*create_(?:async_)?engine\s*\(\s*["']([^"']*)["']`)
	saSessionQueryRe     = regexp.MustCompile(`(\w+)\.query\s*\(\s*([A-Z][A-Za-z0-9_]*)\s*\)`)
	saSessionExecuteRe   = regexp.MustCompile(`(\w+)\.execute\s*\(\s*select\s*\(\s*([A-Z][A-Za-z0-9_]*)\s*\)`)
	saSelectCallRe       = regexp.MustCompile(`(?:^|[^\w])select\s*\(\s*([A-Z][A-Za-z0-9_]*)\s*\)`)
	saHybridPropertyRe   = regexp.MustCompile(`(?m)@hybrid_property\s*\n\s*def\s+(\w+)\s*\(`)
	saEventListenRe      = regexp.MustCompile(`(?m)event\.listen\s*\(\s*(\w+)\s*,\s*["'](\w+)["']`)
	saEventListensForRe  = regexp.MustCompile(`(?m)@event\.listens_for\s*\(\s*(\w+)\s*,\s*["'](\w+)["']\s*\)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
)

// saBaseIndicators are common base classes for SQLAlchemy declarative models.
var saBaseIndicators = []string{"Base", "DeclarativeBase", "db.Model", "Model"}

func (e *SQLAlchemyExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_sqlalchemy")
	_, span := tracer.Start(ctx, "custom.python_sqlalchemy")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	var out []types.EntityRecord

	// 1. Declarative model classes
	for _, idx := range allMatchesIndex(saDeclarativeModelRe, source) {
		className := source[idx[2]:idx[3]]
		bases := source[idx[4]:idx[5]]

		// Check if this looks like a SQLAlchemy model
		isModel := false
		for _, indicator := range saBaseIndicators {
			if strings.Contains(bases, indicator) {
				isModel = true
				break
			}
		}
		if !isModel {
			// Also check for __tablename__ or Mapped[] in the class body
			body := extractClassBody(source, idx[0])
			if saTablenameRe.MatchString(body) || saMappedAnnotationRe.MatchString(body) {
				isModel = true
			}
		}
		if !isModel {
			continue
		}

		line := lineOf(source, idx[0])
		props := map[string]string{"framework": "sqlalchemy", "pattern_type": "model", "class_name": className}
		body := extractClassBody(source, idx[0])
		if tm := saTablenameRe.FindStringSubmatch(body); tm != nil {
			props["tablename"] = tm[1]
		}
		out = append(out, entity(className, "SCOPE.Schema", "", file.Path, line, props))

		// Relationships within the class body
		for _, rIdx := range allMatchesIndex(saRelationshipRe, body) {
			relAttr := body[rIdx[2]:rIdx[3]]
			targetModel := body[rIdx[4]:rIdx[5]]
			relLine := line + strings.Count(body[:rIdx[0]], "\n")
			out = append(out, entity(className+"."+relAttr, "SCOPE.Schema", "", file.Path, relLine,
				map[string]string{"framework": "sqlalchemy", "pattern_type": "relationship", "target_model": targetModel, "parent_class": className}))
		}

		// ForeignKey references in the class body
		for _, fkIdx := range allMatchesIndex(saForeignKeyRe, body) {
			fkTarget := body[fkIdx[2]:fkIdx[3]]
			fkLine := line + strings.Count(body[:fkIdx[0]], "\n")
			tableName := strings.Split(fkTarget, ".")[0]
			out = append(out, entity(className+".fk:"+fkTarget, "SCOPE.Schema", "", file.Path, fkLine,
				map[string]string{"framework": "sqlalchemy", "pattern_type": "foreign_key", "fk_target": fkTarget, "target_table": tableName, "parent_class": className}))
		}

		// Hybrid properties
		for _, hIdx := range allMatchesIndex(saHybridPropertyRe, body) {
			propName := body[hIdx[2]:hIdx[3]]
			propLine := line + strings.Count(body[:hIdx[0]], "\n")
			out = append(out, entity(className+"."+propName, "SCOPE.Operation", "function", file.Path, propLine,
				map[string]string{"framework": "sqlalchemy", "pattern_type": "hybrid_property", "parent_class": className}))
		}
	}

	// 2. Association tables
	for _, idx := range allMatchesIndex(saAssocTableRe, source) {
		varName := source[idx[2]:idx[3]]
		tableName := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity(varName, "SCOPE.Schema", "", file.Path, line,
			map[string]string{"framework": "sqlalchemy", "pattern_type": "association_table", "tablename": tableName}))
	}

	// 3. create_engine
	for _, idx := range allMatchesIndex(saCreateEngineRe, source) {
		varName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		// Do not log the connection string -- security circuit
		out = append(out, entity(varName, "SCOPE.Service", "", file.Path, line,
			map[string]string{"framework": "sqlalchemy", "pattern_type": "engine"}))
	}

	// 4. Session queries
	queryPatterns := []struct {
		re      *regexp.Regexp
		qtype   string
	}{
		{saSessionQueryRe, "session_query"},
		{saSessionExecuteRe, "session_execute"},
	}
	for _, qp := range queryPatterns {
		for _, idx := range allMatchesIndex(qp.re, source) {
			modelName := source[idx[4]:idx[5]]
			line := lineOf(source, idx[0])
			out = append(out, entity("query:"+modelName, "SCOPE.Operation", "function", file.Path, line,
				map[string]string{"framework": "sqlalchemy", "pattern_type": qp.qtype, "model": modelName}))
		}
	}
	for _, idx := range allMatchesIndex(saSelectCallRe, source) {
		modelName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity("select:"+modelName, "SCOPE.Operation", "function", file.Path, line,
			map[string]string{"framework": "sqlalchemy", "pattern_type": "select", "model": modelName}))
	}

	// 5. Events
	for _, idx := range allMatchesIndex(saEventListenRe, source) {
		target := source[idx[2]:idx[3]]
		eventName := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity("event:"+target+"."+eventName, "SCOPE.Pattern", "", file.Path, line,
			map[string]string{"framework": "sqlalchemy", "pattern_type": "event_listen", "target": target, "event": eventName}))
	}
	for _, idx := range allMatchesIndex(saEventListensForRe, source) {
		target := source[idx[2]:idx[3]]
		eventName := source[idx[4]:idx[5]]
		handlerName := source[idx[6]:idx[7]]
		line := lineOf(source, idx[0])
		out = append(out, entity(handlerName, "SCOPE.Pattern", "", file.Path, line,
			map[string]string{"framework": "sqlalchemy", "pattern_type": "event_listens_for", "target": target, "event": eventName}))
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
