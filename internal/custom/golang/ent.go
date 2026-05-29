package golang

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

// ent (entgo.io/ent) is a schema-as-code ORM. A schema is a Go struct that
// embeds ent.Schema and declares Fields()/Edges() methods returning typed
// builders. ent's codegen then produces a typed query builder package. This
// extractor reads the *schema* source (the hand-written entity definitions)
// plus typical generated query-builder call sites.
//
// Mapping:
//   - `type X struct { ent.Schema }`              -> SCOPE.Schema (model)
//   - `field.String("name")` inside Fields()      -> SCOPE.Component (field/column)
//   - `edge.To(...)` / `edge.From(...)`           -> SCOPE.Component (relation)
//   - `client.User.Query()` / `.QueryX()`         -> SCOPE.Operation (query)
//   - `client.Schema.Create(ctx)`                 -> SCOPE.Operation (migration)
func init() {
	extractor.Register("custom_go_ent", &entExtractor{})
}

type entExtractor struct{}

func (e *entExtractor) Language() string { return "custom_go_ent" }

var (
	// type User struct { ent.Schema }  — possibly with other embeds, captured
	// up to the closing brace of the (typically tiny) struct body.
	reEntSchemaStruct = regexp.MustCompile(
		`(?ms)type\s+(\w+)\s+struct\s*\{([^}]*\bent\.Schema\b[^}]*)\}`,
	)
	// field.String("name") / field.Int("age") / field.Time("created_at") ...
	// The leading verb is the Go field constructor; the string arg is the
	// column name as ent stores it.
	reEntField = regexp.MustCompile(
		`field\.(\w+)\(\s*"([^"]+)"\s*\)`,
	)
	// edge.To("owner", User.Type) / edge.From("pets", Pet.Type)
	reEntEdge = regexp.MustCompile(
		`edge\.(To|From)\(\s*"([^"]+)"\s*,\s*([\w.]+)`,
	)
	// Typed query builder entry points produced by codegen:
	//   client.User.Query()      -> entity-bound query
	//   client.User.Create()     -> entity-bound mutation
	//   client.User.Update()/Delete()/Get()/GetX()
	reEntClientOp = regexp.MustCompile(
		`(?m)\b[Cc]lient\.(\w+)\.(Query|Create|CreateBulk|Update|UpdateOne|UpdateOneID|Delete|DeleteOne|DeleteOneID|Get|GetX)\(`,
	)
	// Predicate/builder chainers on a typed query: Where/Order/Limit/...
	reEntQueryChainer = regexp.MustCompile(
		`(?m)\.(Where|Order|Limit|Offset|WithEdges|Select|GroupBy|Aggregate|All|First|Only|Count|Exist)\(`,
	)
	// Schema.Create(ctx) is ent's auto-migration entry point. Also
	// client.Schema.Create(ctx, migrate.WithDropIndex(true)).
	reEntAutoMigrate = regexp.MustCompile(
		`(?m)\.Schema\.Create\(`,
	)
	// migrate.WithFoo(...) options passed to Schema.Create.
	reEntMigrateOpt = regexp.MustCompile(
		`migrate\.(With\w+)\(`,
	)
)

func (e *entExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.ent_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "ent"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "go" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. Schema structs embedding ent.Schema -> SCOPE.Schema (model), plus the
	//    fields/edges declared within the same file scope.
	for _, sm := range reEntSchemaStruct.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[sm[2]:sm[3]]
		line := lineOf(src, sm[0])
		ent := makeEntity(modelName, "SCOPE.Schema", "", file.Path, file.Language, line)
		setProps(&ent, "framework", "ent", "provenance", "INFERRED_FROM_ENT_SCHEMA")
		add(ent)
	}

	// 2. field.X("col") column declarations -> SCOPE.Component (field). ent
	//    field builders are the column definitions for the enclosing schema;
	//    we attribute by enclosing schema (entFieldOwner), falling back to the
	//    single schema in the file when unambiguous.
	owner := entSingleSchemaName(src)
	for _, m := range reEntField.FindAllStringSubmatchIndex(src, -1) {
		goCtor := src[m[2]:m[3]]
		column := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		name := "field:" + entOwnerPrefix(owner) + column
		fieldEnt := makeEntity(name, "SCOPE.Component", "field", file.Path, file.Language, line)
		setProps(&fieldEnt, "framework", "ent", "provenance", "INFERRED_FROM_ENT_FIELD",
			"column_name", column, "ent_builder", goCtor, "sql_type", entFieldSQLType(goCtor))
		if owner != "" {
			setProps(&fieldEnt, "model_name", owner)
		}
		add(fieldEnt)
	}

	// 3. edge.To/edge.From -> SCOPE.Component (relation). ent edges are
	//    explicit, typed relationship declarations: full relationship support.
	for _, m := range reEntEdge.FindAllStringSubmatchIndex(src, -1) {
		dir := src[m[2]:m[3]]      // To | From
		edgeName := src[m[4]:m[5]] // "owner"
		targetRef := src[m[6]:m[7]]
		target := strings.TrimSuffix(targetRef, ".Type")
		if i := strings.LastIndex(target, "."); i >= 0 {
			target = target[i+1:]
		}
		line := lineOf(src, m[0])
		// edge.To declares the owning side; edge.From the inverse (back-ref).
		rel := "has_many"
		if dir == "From" {
			rel = "belongs_to"
		}
		name := "rel:" + entOwnerPrefix(owner) + edgeName
		relEnt := makeEntity(name, "SCOPE.Component", "relation", file.Path, file.Language, line)
		setProps(&relEnt, "framework", "ent", "provenance", "INFERRED_FROM_ENT_EDGE",
			"edge_name", edgeName, "edge_direction", dir, "relationship", rel,
			"target_model", target)
		if owner != "" {
			setProps(&relEnt, "model_name", owner)
		}
		add(relEnt)
	}

	// 4. Typed query-builder call sites -> SCOPE.Operation (query). The entity
	//    is named in the chain (client.<Entity>.Query()), so attribution is
	//    exact: full.
	for _, m := range reEntClientOp.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[m[2]:m[3]]
		op := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		name := "query:" + modelName + "." + op
		ent := makeEntity(name, "SCOPE.Operation", "query", file.Path, file.Language, line)
		setProps(&ent, "framework", "ent", "provenance", "INFERRED_FROM_ENT_CLIENT",
			"model_name", modelName, "query_op", op)
		add(ent)
	}

	// 4b. Builder chainers (Where/Order/Limit/...) -> SCOPE.Operation.
	for _, m := range reEntQueryChainer.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity("chain:"+verb, "SCOPE.Operation", "query", file.Path, file.Language, line)
		setProps(&ent, "framework", "ent", "provenance", "INFERRED_FROM_ENT_CHAIN",
			"query_type", "chain", "chainer", verb)
		add(ent)
	}

	// 5. client.Schema.Create(ctx) -> SCOPE.Operation (migration). ent's
	//    auto-migration. migrate.WithX options recorded as well.
	for _, m := range reEntAutoMigrate.FindAllStringSubmatchIndex(src, -1) {
		line := lineOf(src, m[0])
		ent := makeEntity("migrate:schema_create", "SCOPE.Operation", "migration", file.Path, file.Language, line)
		setProps(&ent, "framework", "ent", "provenance", "INFERRED_FROM_ENT_AUTOMIGRATE",
			"migration_kind", "auto_migrate")
		add(ent)
	}
	for _, m := range reEntMigrateOpt.FindAllStringSubmatchIndex(src, -1) {
		opt := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity("migrate_opt:"+opt, "SCOPE.Operation", "migration", file.Path, file.Language, line)
		setProps(&ent, "framework", "ent", "provenance", "INFERRED_FROM_ENT_MIGRATE_OPT",
			"migration_kind", "migrate_option", "migrate_option", opt)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// entSingleSchemaName returns the schema name when the file declares exactly
// one ent.Schema struct, so fields/edges can be attributed to it. With zero or
// multiple schemas it returns "" (fields stay model-agnostic to avoid
// misattribution).
func entSingleSchemaName(src string) string {
	names := reEntSchemaStruct.FindAllStringSubmatch(src, -1)
	if len(names) == 1 {
		return names[0][1]
	}
	return ""
}

// entOwnerPrefix renders the "Model." prefix for a synthetic entity name, or
// "" when the owner is unknown.
func entOwnerPrefix(owner string) string {
	if owner == "" {
		return ""
	}
	return owner + "."
}

// entFieldSQLType maps an ent field builder verb to a coarse SQL type. ent
// abstracts the concrete dialect type, so this is a best-effort mapping used
// for downstream display only.
func entFieldSQLType(ctor string) string {
	switch ctor {
	case "String", "Text", "UUID", "Enum":
		return "text"
	case "Int", "Int8", "Int16", "Int32", "Int64",
		"Uint", "Uint8", "Uint16", "Uint32", "Uint64":
		return "integer"
	case "Float", "Float32":
		return "real"
	case "Bool":
		return "boolean"
	case "Time":
		return "timestamp"
	case "Bytes":
		return "blob"
	case "JSON":
		return "json"
	default:
		return ""
	}
}
