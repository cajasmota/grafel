package elixir

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
	extractor.Register("custom_elixir_ecto", &ectoExtractor{})
}

type ectoExtractor struct{}

func (e *ectoExtractor) Language() string { return "custom_elixir_ecto" }

var (
	reEctoSchema = regexp.MustCompile(
		`(?m)schema\s+"([^"]+)"\s+do`,
	)
	// reEctoField matches a column declaration inside a schema block:
	//   field :title, :string
	//   field :age, :integer, default: 0
	//   field :published, :boolean, default: false
	// Captures: (1) field name, (2) ecto type.
	reEctoField = regexp.MustCompile(
		`(?m)^\s*field\s+:(\w+)\s*,\s*:(\w+)`,
	)
	// reEctoTimestamps matches the timestamps() macro, which injects
	// inserted_at / updated_at columns.
	reEctoTimestamps = regexp.MustCompile(
		`(?m)^\s*timestamps\b`,
	)
	// reEctoAssociation matches an association macro and its TARGET schema:
	//   belongs_to :user, MyApp.User
	//   has_many :comments, MyApp.Comment
	//   has_one :profile, Profile
	//   many_to_many :tags, Tag, join_through: "posts_tags"
	// Captures: (1) macro, (2) assoc name, (3) target schema module (optional).
	reEctoAssociation = regexp.MustCompile(
		`(?m)(has_one|has_many|belongs_to|many_to_many)\s+:(\w+)(?:\s*,\s*([A-Z][\w.]*))?`,
	)
	// reEctoJoinThrough captures the join_through table of a many_to_many.
	reEctoJoinThrough = regexp.MustCompile(
		`join_through:\s*(?:"([^"]+)"|([A-Z][\w.]*))`,
	)
	// reEctoForeignKey captures an explicit foreign_key: option on an assoc.
	reEctoForeignKey = regexp.MustCompile(
		`foreign_key:\s*:(\w+)`,
	)
	reEctoChangeset = regexp.MustCompile(
		`(?m)def\s+changeset\s*\(`,
	)
	reEctoRepoCall = regexp.MustCompile(
		`(?m)\bRepo\.(get|get!|get_by|get_by!|all|insert|insert!|insert_all|update|update!|update_all|delete|delete!|delete_all|one|one!|exists\?|aggregate|preload|transaction)\b`,
	)
	// reEctoQuery matches the keyword-syntax query and captures the source
	// binding + queried schema:  from u in User, ...
	reEctoQuery = regexp.MustCompile(
		`(?m)\bfrom\s+(\w+)\s+in\s+([A-Z][\w.]*)`,
	)
	// reEctoQueryClause matches the DSL clauses attached to a query so we can
	// record what the query actually does.
	reEctoQueryClause = regexp.MustCompile(
		`\b(where|join|left_join|inner_join|right_join|order_by|group_by|having|select|limit|offset|preload|distinct|on):`,
	)
	reEctoMulti = regexp.MustCompile(
		`(?m)Ecto\.Multi\.new\(\)`,
	)
	reEctoRepo = regexp.MustCompile(
		`(?m)use\s+Ecto\.Repo\b`,
	)
	reEctoModuleDecl = regexp.MustCompile(
		`(?m)^defmodule\s+([\w.]+)`,
	)
	reEctoMigrationCreate = regexp.MustCompile(
		`(?m)create\s+table\s*\(:([a-z_]+)`,
	)
	// reEctoMigrationAdd matches a column addition inside a migration block:
	//   add :name, :string
	//   add :age, :integer, default: 0
	//   add :org_id, references(:orgs)
	// Captures: (1) column name, (2) type-or-references expression head.
	reEctoMigrationAdd = regexp.MustCompile(
		`(?m)^\s*add\s+:(\w+)\s*,\s*:?(\w+)`,
	)
	// reEctoReferences captures the referenced table of a references(:table) FK.
	reEctoReferences = regexp.MustCompile(
		`references\(\s*:(\w+)`,
	)
)

func (e *ectoExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/elixir")
	_, span := tracer.Start(ctx, "indexer.ecto_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "ecto"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "elixir" {
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

	// 1. schema "table_name" do -> SCOPE.Schema (+ per-column SCOPE.Schema/column)
	for _, m := range reEctoSchema.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		ent := makeEntity(tableName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_SCHEMA",
			"table_name", tableName)
		add(ent)

		// Drill into the schema body for field :name, :type columns.
		body := ectoBlockBody(src, m[0])
		bodyLine := lineOf(src, m[0])
		for _, fm := range reEctoField.FindAllStringSubmatchIndex(body, -1) {
			fieldName := body[fm[2]:fm[3]]
			fieldType := body[fm[4]:fm[5]]
			colLine := bodyLine + strings.Count(body[:fm[0]], "\n")
			col := makeEntity(tableName+"."+fieldName, "SCOPE.Schema", "column", file.Path, file.Language, colLine)
			setProps(&col, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_FIELD",
				"pattern_type", "field", "table_name", tableName,
				"column_name", fieldName, "field_type", fieldType)
			add(col)
		}
		// timestamps() macro -> inserted_at / updated_at columns.
		if reEctoTimestamps.MatchString(body) {
			for _, ts := range []string{"inserted_at", "updated_at"} {
				col := makeEntity(tableName+"."+ts, "SCOPE.Schema", "column", file.Path, file.Language, bodyLine)
				setProps(&col, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_TIMESTAMPS",
					"pattern_type", "field", "table_name", tableName,
					"column_name", ts, "field_type", "naive_datetime")
				add(col)
			}
		}
	}

	// 2. has_one/has_many/belongs_to/many_to_many -> SCOPE.Component (with target schema)
	for _, m := range reEctoAssociation.FindAllStringSubmatchIndex(src, -1) {
		assocType := src[m[2]:m[3]]
		assocName := src[m[4]:m[5]]
		target := ""
		if m[6] >= 0 {
			target = src[m[6]:m[7]]
		}
		name := assocType + ":" + assocName
		ent := makeEntity(name, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_ASSOCIATION",
			"association_type", assocType, "association_name", assocName)
		if target != "" {
			setProps(&ent, "target_schema", target)
		}

		// Inspect the rest of the association line for options.
		lineEnd := strings.IndexByte(src[m[0]:], '\n')
		var optStr string
		if lineEnd < 0 {
			optStr = src[m[0]:]
		} else {
			optStr = src[m[0] : m[0]+lineEnd]
		}
		if assocType == "belongs_to" {
			// belongs_to implies a foreign key: explicit foreign_key: option or
			// the conventional <assoc>_id column.
			fk := assocName + "_id"
			if fkm := reEctoForeignKey.FindStringSubmatch(optStr); fkm != nil {
				fk = fkm[1]
			}
			setProps(&ent, "foreign_key", fk, "owns_fk", "true")
		}
		if assocType == "many_to_many" {
			if jt := reEctoJoinThrough.FindStringSubmatch(optStr); jt != nil {
				join := jt[1]
				if join == "" {
					join = jt[2]
				}
				setProps(&ent, "join_through", join)
			}
		}
		add(ent)
	}

	// 3. changeset/2 -> SCOPE.Operation/function
	changesetCount := 0
	for _, m := range reEctoChangeset.FindAllStringIndex(src, -1) {
		changesetCount++
		name := "changeset"
		if changesetCount > 1 {
			name = "changeset_" + string(rune('0'+changesetCount))
		}
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_CHANGESET")
		add(ent)
	}

	// 4. Repo.* calls -> SCOPE.Operation/query
	for _, m := range reEctoRepoCall.FindAllStringSubmatchIndex(src, -1) {
		callName := src[m[2]:m[3]]
		ent := makeEntity("Repo."+callName, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_QUERY",
			"repo_call", callName)
		add(ent)
	}

	// 5. from q in Schema queries -> SCOPE.Operation/query (with queried schema + clauses)
	for _, m := range reEctoQuery.FindAllStringSubmatchIndex(src, -1) {
		binding := src[m[2]:m[3]]
		schema := src[m[4]:m[5]]
		startLine := lineOf(src, m[0])
		// Capture the query's tail (up to the end of the statement / next blank
		// line) to enumerate the DSL clauses.
		tail := ectoQueryTail(src, m[0])
		var clauses []string
		clauseSeen := map[string]bool{}
		for _, cm := range reEctoQueryClause.FindAllStringSubmatch(tail, -1) {
			c := cm[1]
			if !clauseSeen[c] {
				clauseSeen[c] = true
				clauses = append(clauses, c)
			}
		}
		name := "query:" + schema + "[" + binding + "]"
		ent := makeEntity(name, "SCOPE.Operation", "query", file.Path, file.Language, startLine)
		setProps(&ent, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_QUERY",
			"query_type", "from", "queried_schema", schema, "source_binding", binding)
		if len(clauses) > 0 {
			setProps(&ent, "query_clauses", strings.Join(clauses, ","))
		}
		add(ent)
	}

	// 6. Ecto.Multi.new() -> SCOPE.Pattern
	for _, m := range reEctoMulti.FindAllStringIndex(src, -1) {
		ent := makeEntity("Ecto.Multi", "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_MULTI")
		add(ent)
	}

	// 7. use Ecto.Repo -> SCOPE.Service
	for _, m := range reEctoRepo.FindAllStringIndex(src, -1) {
		prefix := src[:m[0]]
		cm := reEctoModuleDecl.FindAllStringSubmatch(prefix, -1)
		moduleName := "Repo"
		if len(cm) > 0 {
			moduleName = cm[len(cm)-1][1]
		}
		ent := makeEntity(moduleName, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_REPO")
		add(ent)
	}

	// 8. Migration create table -> SCOPE.Schema (+ per-column + FK references)
	for _, m := range reEctoMigrationCreate.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		ent := makeEntity("migration:"+tableName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_MIGRATION",
			"table_name", tableName)
		add(ent)

		body := ectoBlockBody(src, m[0])
		bodyLine := lineOf(src, m[0])
		for _, am := range reEctoMigrationAdd.FindAllStringSubmatchIndex(body, -1) {
			colName := body[am[2]:am[3]]
			typeHead := body[am[4]:am[5]]
			colLine := bodyLine + strings.Count(body[:am[0]], "\n")

			// Determine if this column is a references(...) FK by inspecting the
			// remainder of the add line.
			lineEnd := strings.IndexByte(body[am[0]:], '\n')
			var addLine string
			if lineEnd < 0 {
				addLine = body[am[0]:]
			} else {
				addLine = body[am[0] : am[0]+lineEnd]
			}

			col := makeEntity("migration:"+tableName+"."+colName, "SCOPE.Schema", "column", file.Path, file.Language, colLine)
			if ref := reEctoReferences.FindStringSubmatch(addLine); ref != nil {
				setProps(&col, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_MIGRATION_FK",
					"pattern_type", "foreign_key", "table_name", tableName,
					"column_name", colName, "references_table", ref[1])
			} else {
				setProps(&col, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_MIGRATION_COLUMN",
					"pattern_type", "column", "table_name", tableName,
					"column_name", colName, "field_type", typeHead)
			}
			add(col)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ectoQueryTail returns the source from the `from` keyword forward, bounded by
// the first blank line or ~12 lines, so that clause detection does not bleed
// into unrelated code following the query expression.
func ectoQueryTail(src string, start int) string {
	rest := src[start:]
	lines := strings.SplitAfter(rest, "\n")
	var b strings.Builder
	for i, ln := range lines {
		if i > 0 && strings.TrimSpace(ln) == "" {
			break
		}
		if i >= 12 {
			break
		}
		b.WriteString(ln)
	}
	return b.String()
}
