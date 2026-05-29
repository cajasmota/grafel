package golang

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ent (entgo.io/ent) is Facebook/Meta's schema-as-code, code-generated ORM.
// Unlike tag-driven ORMs, the source of truth is a set of schema definitions
// under ent/schema/*.go: a struct embedding ent.Schema declares an entity, its
// Fields() method returns a []ent.Field built from field.<Type>("name")
// builders, and its Edges() method returns a []ent.Edge built from
// edge.To/edge.From(...) builders. The generated query builder
// (client.User.Query()/Create()/Update()/Delete()) and auto-migration
// (client.Schema.Create(ctx)) round out the surface.
//
// This extractor recognises that surface from the schema files (the definitive
// signal) plus generated/usage call sites:
//   - Models:        ent.Schema-embedding struct  -> SCOPE.Schema
//   - Columns:       field.<Type>("name")          -> SCOPE.Component (field)
//   - Relationships: edge.To / edge.From(...)      -> SCOPE.Component (relation)
//   - Queries:       client.<E>.Query/Create/...    -> SCOPE.Operation (query)
//   - Migrations:    Schema.Create(ctx) automigrate -> SCOPE.Operation (migration)
func init() {
	extractor.Register("custom_go_ent", &entExtractor{})
}

type entExtractor struct{}

func (e *entExtractor) Language() string { return "custom_go_ent" }

var (
	// A schema struct embeds ent.Schema in its body:
	//   type User struct { ent.Schema }
	reEntSchemaStruct = regexp.MustCompile(
		`(?ms)type\s+(\w+)\s+struct\s*\{(.*?)\n\}`,
	)
	reEntSchemaEmbed = regexp.MustCompile(`\bent\.Schema\b`)

	// field.<Type>("column_name") inside a Fields() return slice. The builder
	// type (String/Int/Time/Enum/Bool/...) is the Go field-kind, the string
	// literal is the column name.
	reEntField = regexp.MustCompile(
		`field\.(\w+)\s*\(\s*"([^"]+)"`,
	)

	// edge.To("name", T.Type) / edge.From("name", T.Type). edge.To declares the
	// owning/forward side, edge.From the inverse (back-reference) side.
	reEntEdge = regexp.MustCompile(
		`edge\.(To|From)\s*\(\s*"([^"]+)"\s*,\s*([\w.]+)`,
	)
	// .Unique() on an edge marks a singular (to-one) relationship; its absence
	// on a To-edge implies a to-many. .Ref("...") names the inverse field.
	reEntEdgeUnique = regexp.MustCompile(`\.Unique\s*\(`)
	reEntEdgeRef    = regexp.MustCompile(`\.Ref\s*\(\s*"([^"]+)"`)

	// Generated/typed query builder usage: client.User.Query()/Create()/...
	// or User.Query() on a typed client. Captures the entity then the verb.
	reEntQuery = regexp.MustCompile(
		`(?m)\.(\w+)\.(Query|Create|CreateBulk|Update|UpdateOne|UpdateOneID|Delete|DeleteOne|DeleteOneID|Get|GetX)\s*\(`,
	)

	// Auto-migration: client.Schema.Create(ctx, ...) (and the bare
	// Schema.Create(ctx) form). WithGlobalUniqueID/WithDropColumn are options.
	reEntMigrate = regexp.MustCompile(
		`\.Schema\.Create\s*\(`,
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

	// A file is an ent schema file when it lives under ent/schema/ OR embeds
	// ent.Schema. Schema-driven extraction (Models/Relationships) only fires
	// for such files; query/migration usage can appear anywhere.
	inSchemaDir := entInSchemaDir(file.Path)

	for _, sm := range reEntSchemaStruct.FindAllStringSubmatchIndex(src, -1) {
		structName := src[sm[2]:sm[3]]
		body := src[sm[4]:sm[5]]
		if !reEntSchemaEmbed.MatchString(body) && !inSchemaDir {
			continue
		}
		// Only treat as an ent schema when it embeds ent.Schema (the
		// definitive marker). A struct in ent/schema/ without the embed is a
		// helper type, not an entity.
		if !reEntSchemaEmbed.MatchString(body) {
			continue
		}
		structLine := lineOf(src, sm[0])
		schemaEnt := makeEntity(structName, "SCOPE.Schema", "", file.Path, file.Language, structLine)
		setProps(&schemaEnt, "framework", "ent", "provenance", "INFERRED_FROM_ENT_SCHEMA")
		add(schemaEnt)

		// The Fields()/Edges() bodies are methods on this schema type, defined
		// elsewhere in the file. We scope field/edge extraction to the whole
		// file but attribute to the (single) schema type per ent's
		// one-schema-per-file convention. When multiple schemas share a file
		// (rare), we still attribute fields to the first schema; this is a
		// documented partial.
		_ = body
	}

	// Determine the owning schema for field/edge attribution: ent's convention
	// is one entity schema per file under ent/schema/. Use the first
	// ent.Schema-embedding struct found.
	owner := entOwningSchema(src)

	if owner != "" {
		// Columns from field.<Type>("name").
		for _, m := range reEntField.FindAllStringSubmatchIndex(src, -1) {
			builderType := src[m[2]:m[3]]
			column := src[m[4]:m[5]]
			line := lineOf(src, m[0])
			fieldEnt := makeEntity("field:"+owner+"."+column, "SCOPE.Component", "field", file.Path, file.Language, line)
			setProps(&fieldEnt, "framework", "ent", "provenance", "INFERRED_FROM_ENT_FIELD",
				"model_name", owner, "field_name", column, "column_name", column,
				"ent_field_type", builderType)
			add(fieldEnt)
		}

		// Relationships from edge.To/edge.From("name", T.Type).
		for _, m := range reEntEdge.FindAllStringSubmatchIndex(src, -1) {
			dir := src[m[2]:m[3]]    // To | From
			name := src[m[4]:m[5]]   // edge name
			tgtRaw := src[m[6]:m[7]] // e.g. "Group.Type" or "Group"
			line := lineOf(src, m[0])
			target := tgtRaw
			if i := strings.Index(target, "."); i >= 0 {
				target = target[:i]
			}

			// Classify: edge.From is always the inverse (back-reference) side.
			// edge.To with .Unique() is to-one (has_one); without is to-many
			// (has_many). edge.From with .Unique() is belongs_to; without is
			// the many side of a many2many / inverse has_many.
			tail := entEdgeTail(src, m[1])
			unique := reEntEdgeUnique.MatchString(tail)
			var rel string
			switch {
			case dir == "From" && unique:
				rel = "belongs_to"
			case dir == "From":
				rel = "has_many" // inverse of a to-many
			case dir == "To" && unique:
				rel = "has_one"
			default:
				rel = "has_many"
			}

			relEnt := makeEntity("rel:"+owner+"."+name, "SCOPE.Component", "relation", file.Path, file.Language, line)
			setProps(&relEnt, "framework", "ent", "provenance", "INFERRED_FROM_ENT_EDGE",
				"model_name", owner, "field_name", name, "relationship", rel,
				"target_model", target, "edge_direction", strings.ToLower(dir))
			if rm := reEntEdgeRef.FindStringSubmatch(tail); rm != nil {
				setProps(&relEnt, "inverse_ref", rm[1])
			}
			add(relEnt)
		}
	}

	// Queries: typed builder usage (client.User.Query()/Create()/...).
	for _, m := range reEntQuery.FindAllStringSubmatchIndex(src, -1) {
		entity := src[m[2]:m[3]]
		verb := src[m[4]:m[5]]
		// Skip selectors that are obviously not entity accessors (lowercase
		// receivers like .ctx.Query are unlikely; entity types are exported).
		if entity == "" || !isExportedIdent(entity) {
			continue
		}
		line := lineOf(src, m[0])
		q := makeEntity("query:"+entity+"."+verb, "SCOPE.Operation", "query", file.Path, file.Language, line)
		setProps(&q, "framework", "ent", "provenance", "INFERRED_FROM_ENT_QUERY",
			"model_name", entity, "query_verb", verb)
		add(q)
	}

	// Migrations: Schema.Create(ctx) auto-migration.
	for _, m := range reEntMigrate.FindAllStringSubmatchIndex(src, -1) {
		line := lineOf(src, m[0])
		mig := makeEntity("migrate:ent_schema_create", "SCOPE.Operation", "migration", file.Path, file.Language, line)
		setProps(&mig, "framework", "ent", "provenance", "INFERRED_FROM_ENT_MIGRATE",
			"migration_kind", "auto_migrate")
		add(mig)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// entInSchemaDir reports whether the file path lives under an ent/schema/
// directory, ent's canonical home for entity definitions.
func entInSchemaDir(path string) bool {
	clean := filepath.ToSlash(path)
	return strings.Contains(clean, "ent/schema/")
}

// entOwningSchema returns the name of the first ent.Schema-embedding struct in
// the source, used to attribute field/edge builders (ent's one-schema-per-file
// convention).
func entOwningSchema(src string) string {
	for _, sm := range reEntSchemaStruct.FindAllStringSubmatchIndex(src, -1) {
		body := src[sm[4]:sm[5]]
		if reEntSchemaEmbed.MatchString(body) {
			return src[sm[2]:sm[3]]
		}
	}
	return ""
}

// entEdgeTail returns a bounded slice of source starting at off, used to scan
// the chained options (.Unique()/.Ref("...")) that follow an edge builder.
// The window is capped so options of the *next* edge are not misattributed.
func entEdgeTail(src string, off int) string {
	end := off + 160
	if end > len(src) {
		end = len(src)
	}
	tail := src[off:end]
	// Stop at the next edge.To/edge.From so we don't bleed into a sibling edge.
	if i := strings.Index(tail, "edge."); i >= 0 {
		tail = tail[:i]
	}
	return tail
}

// isExportedIdent reports whether s begins with an uppercase ASCII letter, the
// Go convention for an exported (entity) identifier.
func isExportedIdent(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}
