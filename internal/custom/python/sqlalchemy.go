package python

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/lifecycle"
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
	// saLazyKwargRe matches a lazy= keyword argument in a relationship() call.
	// Captures the lazy strategy value: "dynamic", "select", "joined",
	// "subquery", "raise", "raise_on_sql", True, False, or "write_only".
	// Issue #2986 — lazy_loading_recognition partial for SQLAlchemy.
	saLazyKwargRe = regexp.MustCompile(`\blazy\s*=\s*["']?([A-Za-z_][A-Za-z0-9_]*)["']?`)
	// saUselistRe captures the uselist= kwarg of a relationship() call. When
	// uselist=False the relationship is scalar (one_to_one / many_to_one);
	// otherwise it is a collection (one_to_many). Used for GRAPH_RELATES
	// cardinality.
	saUselistRe         = regexp.MustCompile(`\buselist\s*=\s*(True|False)\b`)
	saForeignKeyRe      = regexp.MustCompile(`ForeignKey\s*\(\s*["']([^"']+)["']`)
	saAssocTableRe      = regexp.MustCompile(`(?m)^(\w+)\s*=\s*Table\s*\(\s*["']([^"']+)["']`)
	saCreateEngineRe    = regexp.MustCompile(`(?m)(\w+)\s*=\s*create_(?:async_)?engine\s*\(\s*["']([^"']*)["']`)
	saSessionQueryRe    = regexp.MustCompile(`(\w+)\.query\s*\(\s*([A-Z][A-Za-z0-9_]*)\s*\)`)
	saSessionExecuteRe  = regexp.MustCompile(`(\w+)\.execute\s*\(\s*select\s*\(\s*([A-Z][A-Za-z0-9_]*)\s*\)`)
	saSelectCallRe      = regexp.MustCompile(`(?:^|[^\w])select\s*\(\s*([A-Z][A-Za-z0-9_]*)\s*\)`)
	saHybridPropertyRe  = regexp.MustCompile(`(?m)@hybrid_property\s*\n\s*def\s+(\w+)\s*\(`)
	saEventListenRe     = regexp.MustCompile(`(?m)event\.listen\s*\(\s*(\w+)\s*,\s*["'](\w+)["']`)
	saEventListensForRe = regexp.MustCompile(`(?m)@event\.listens_for\s*\(\s*(\w+)\s*,\s*["'](\w+)["']\s*\)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
)

// saBaseIndicators are common base classes for SQLAlchemy declarative models.
// "Model" is intentionally absent: it is too broad a substring and matches
// Pydantic's "BaseModel" as well as Flask-Login's "UserMixin", causing false
// positives. Use "db.Model" (Flask-SQLAlchemy) or rely on the __tablename__/
// Mapped[] body scan below instead. Issue #1501 — within-extractor dedup fix 2/2.
var saBaseIndicators = []string{"Base", "DeclarativeBase", "db.Model"}

// saPydanticBases lists Pydantic base class names that, when present in a
// class's base list, conclusively identify the class as a Pydantic model and
// NOT a SQLAlchemy declarative model. This guard prevents false positives when
// a file imports both sqlalchemy and pydantic (e.g. a shared models.py with
// Pydantic DTOs next to ORM classes).
var saPydanticBases = []string{"BaseModel", "BaseSettings", "RootModel", "GenericModel"}

// isSQLModelTableClass reports whether a class declaration's base list
// identifies a SQLModel DB-table model: the base list must contain "SQLModel"
// AND the keyword argument "table=True". SQLModel schema-only classes (which
// lack "table=True") are NOT DB-table models and must not be emitted as
// SCOPE.Schema ORM entities.
// Issue #2990 — SQLModel schema_extraction partial promotion.
func isSQLModelTableClass(bases string) bool {
	return strings.Contains(bases, "SQLModel") &&
		strings.Contains(bases, "table=True")
}

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

		// Exclude Pydantic models: if any Pydantic base class appears in the
		// base list, the class is definitely NOT a SQLAlchemy model, even if the
		// body contains SQLAlchemy-like patterns (e.g. a shared models.py with
		// Pydantic DTOs). Issue #1501 — prevents BaseModel false positives.
		isPydantic := false
		for _, pb := range saPydanticBases {
			if strings.Contains(bases, pb) {
				isPydantic = true
				break
			}
		}
		if isPydantic {
			continue
		}

		// Check if this looks like a SQLAlchemy / SQLModel model.
		//
		// SQLModel table classes use `class Hero(SQLModel, table=True):`
		// — they match the "SQLModel" base name AND carry the table=True kwarg.
		// Issue #2990 — promote schema_extraction to partial for SQLModel.
		isModel := isSQLModelTableClass(bases)
		if !isModel {
			for _, indicator := range saBaseIndicators {
				if strings.Contains(bases, indicator) {
					isModel = true
					break
				}
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
		framework := "sqlalchemy"
		if isSQLModelTableClass(bases) {
			framework = "sqlmodel"
		}
		props := map[string]string{"framework": framework, "pattern_type": "model", "class_name": className}
		body := extractClassBody(source, idx[0])
		if tm := saTablenameRe.FindStringSubmatch(body); tm != nil {
			props["tablename"] = tm[1]
		}
		// Data-lifecycle traits (#3628 child): soft-delete (deleted_at Column /
		// SoftDeleteMixin), timestamps (created_at+updated_at with a
		// server_default/onupdate signal), audit columns — stamped onto the model
		// node. A plain `deleted` boolean is NOT reported as soft-delete.
		lifecycle.SQLAlchemy(bases, body).Stamp(func(kv ...string) {
			for i := 0; i+1 < len(kv); i += 2 {
				props[kv[i]] = kv[i+1]
			}
		})
		out = append(out, entity(className, "SCOPE.Schema", "", file.Path, line, props))
		modelIdx := len(out) - 1 // index of this model node, for GRAPH_RELATES edges

		// Relationships within the class body
		for _, rIdx := range allMatchesIndex(saRelationshipRe, body) {
			relAttr := body[rIdx[2]:rIdx[3]]
			targetModel := body[rIdx[4]:rIdx[5]]
			relLine := line + strings.Count(body[:rIdx[0]], "\n")
			relProps := map[string]string{
				"framework":    "sqlalchemy",
				"pattern_type": "relationship",
				"target_model": targetModel,
				"parent_class": className,
			}
			argsBlob := ""
			if rIdx[7] >= 0 {
				argsBlob = body[rIdx[6]:rIdx[7]]
			}
			// Issue #2986 — detect lazy= kwarg in the relationship args blob.
			// Captures strategies like "dynamic", "select", "joined", "subquery",
			// "raise", "raise_on_sql", "write_only", True, False.
			if argsBlob != "" {
				if lm := saLazyKwargRe.FindStringSubmatch(argsBlob); lm != nil {
					relProps["lazy_strategy"] = lm[1]
				}
			}
			out = append(out, entity(className+"."+relAttr, "SCOPE.Schema", "", file.Path, relLine, relProps))

			// GRAPH_RELATES model↔model edge with cardinality. SQLAlchemy
			// relationship() default is a collection (one_to_many); uselist=False
			// makes it scalar (one_to_one). The target is a quoted class name that
			// resolves to the model node via the Class:<Name> byName convention.
			card := "one_to_many"
			if um := saUselistRe.FindStringSubmatch(argsBlob); um != nil && um[1] == "False" {
				card = "one_to_one"
			}
			out[modelIdx].Relationships = append(out[modelIdx].Relationships,
				types.RelationshipRecord{
					FromID: "Class:" + className,
					ToID:   "Class:" + targetModel,
					Kind:   string(types.RelationshipKindGraphRelates),
					Properties: map[string]string{
						"framework":    "sqlalchemy",
						"cardinality":  card,
						"field_name":   relAttr,
						"target_model": targetModel,
						"provenance":   "INFERRED_FROM_SQLALCHEMY_RELATIONSHIP",
					},
				})
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
		re    *regexp.Regexp
		qtype string
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
