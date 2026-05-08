package elixir

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/attribute"

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
	reEctoAssociation = regexp.MustCompile(
		`(?m)(has_one|has_many|belongs_to|many_to_many)\s+:(\w+)`,
	)
	reEctoChangeset = regexp.MustCompile(
		`(?m)def\s+changeset\s*\(`,
	)
	reEctoRepoCall = regexp.MustCompile(
		`(?m)\bRepo\.(get|get!|all|insert|insert!|update|update!|delete|delete!|one|one!|exists\?|aggregate)\b`,
	)
	reEctoQuery = regexp.MustCompile(
		`(?m)\bfrom\s+\w+\s+in\s+\w+`,
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

	// 1. schema "table_name" do -> SCOPE.Schema
	for _, m := range reEctoSchema.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		ent := makeEntity(tableName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_SCHEMA",
			"table_name", tableName)
		add(ent)
	}

	// 2. has_one/has_many/belongs_to/many_to_many -> SCOPE.Component
	for _, m := range reEctoAssociation.FindAllStringSubmatchIndex(src, -1) {
		assocType := src[m[2]:m[3]]
		assocName := src[m[4]:m[5]]
		name := assocType + ":" + assocName
		ent := makeEntity(name, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_ASSOCIATION",
			"association_type", assocType, "association_name", assocName)
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

	// 5. from q in Schema queries -> SCOPE.Operation/query
	queryCount := 0
	for _, m := range reEctoQuery.FindAllStringIndex(src, -1) {
		queryCount++
		name := "ecto_query_" + string(rune('0'+queryCount%10))
		ent := makeEntity(name, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_QUERY",
			"query_type", "from")
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

	// 8. Migration create table -> SCOPE.Schema
	for _, m := range reEctoMigrationCreate.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		ent := makeEntity("migration:"+tableName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ecto", "provenance", "INFERRED_FROM_ECTO_MIGRATION",
			"table_name", tableName)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
