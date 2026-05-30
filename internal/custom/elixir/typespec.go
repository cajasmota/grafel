package elixir

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_elixir_typespec", &typespecExtractor{})
}

type typespecExtractor struct{}

func (e *typespecExtractor) Language() string { return "custom_elixir_typespec" }

// Elixir typespec / behaviour patterns:
//
//	@type name :: ...
//	@typep name :: ...
//	@opaque name :: ...
//	@spec name(arg) :: return
//	@callback name(arg) :: return
//	@type name when ...   (parametric)
//	@behaviour ModuleName
//	defprotocol  (handled by tree-sitter extractor; cited here as interface)

var (
	reTypeDecl = regexp.MustCompile(
		`(?m)^\s*@(type|typep|opaque)\s+([A-Za-z_][\w]*)\s*(?:\([^)]*\))?\s*::`,
	)
	reTypeAlias = regexp.MustCompile(
		`(?m)^\s*@type\s+([A-Za-z_][\w]*)\s*(?:\([^)]*\))?\s*::\s*([A-Za-z_][\w.]+)`,
	)
	reSpec = regexp.MustCompile(
		`(?m)^\s*@spec\s+([A-Za-z_][\w?!]*)\s*\(`,
	)
	reCallback = regexp.MustCompile(
		`(?m)^\s*@callback\s+([A-Za-z_][\w?!]*)\s*\(`,
	)
	reBehaviour = regexp.MustCompile(
		`(?m)^\s*@behaviour\s+([\w.]+)`,
	)
	reDefProtocol = regexp.MustCompile(
		`(?m)^defprotocol\s+([\w.]+)`,
	)
	reModuleDecl = regexp.MustCompile(
		`(?m)^defmodule\s+([\w.]+)`,
	)
)

func (e *typespecExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/elixir")
	_, span := tracer.Start(ctx, "indexer.typespec_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "typespec"),
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

	// 1. @type / @typep / @opaque  → SCOPE.Schema/type
	for _, m := range reTypeDecl.FindAllStringSubmatchIndex(src, -1) {
		typeKind := src[m[2]:m[3]] // "type", "typep", "opaque"
		typeName := src[m[4]:m[5]]
		ent := makeEntity(typeName, "SCOPE.Schema", "type", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "elixir_typespec", "provenance", "INFERRED_FROM_TYPESPEC",
			"type_kind", "@"+typeKind)
		add(ent)
	}

	// 2. @type name :: OtherType  (simple alias) → SCOPE.Schema/type_alias
	for _, m := range reTypeAlias.FindAllStringSubmatchIndex(src, -1) {
		typeName := src[m[2]:m[3]]
		aliasTarget := src[m[4]:m[5]]
		ent := makeEntity(typeName, "SCOPE.Schema", "type_alias", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "elixir_typespec", "provenance", "INFERRED_FROM_TYPE_ALIAS",
			"alias_target", aliasTarget)
		add(ent)
	}

	// 3. @spec name(...) :: ...  → SCOPE.Operation/spec
	for _, m := range reSpec.FindAllStringSubmatchIndex(src, -1) {
		specName := src[m[2]:m[3]]
		ent := makeEntity("spec:"+specName, "SCOPE.Operation", "spec", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "elixir_typespec", "provenance", "INFERRED_FROM_SPEC",
			"spec_name", specName)
		add(ent)
	}

	// 4. @callback name(...) :: ...  → SCOPE.Operation/callback (interface contract)
	for _, m := range reCallback.FindAllStringSubmatchIndex(src, -1) {
		cbName := src[m[2]:m[3]]
		// Use the preceding defmodule as parent context if available.
		prefix := src[:m[0]]
		parentMod := ""
		if mm := reModuleDecl.FindAllStringSubmatch(prefix, -1); len(mm) > 0 {
			parentMod = mm[len(mm)-1][1]
		}
		ent := makeEntity(cbName, "SCOPE.Operation", "callback", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "elixir_typespec", "provenance", "INFERRED_FROM_CALLBACK",
			"callback_name", cbName, "behaviour_module", parentMod)
		add(ent)
	}

	// 5. @behaviour ModuleName  → SCOPE.Component/behaviour_impl
	//    Records that this module implements a behaviour (interface).
	for _, m := range reBehaviour.FindAllStringSubmatchIndex(src, -1) {
		behaviourName := src[m[2]:m[3]]
		prefix := src[:m[0]]
		parentMod := "unknown"
		if mm := reModuleDecl.FindAllStringSubmatch(prefix, -1); len(mm) > 0 {
			parentMod = mm[len(mm)-1][1]
		}
		ent := makeEntity("implements:"+behaviourName, "SCOPE.Component", "behaviour_impl",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "elixir_typespec", "provenance", "INFERRED_FROM_BEHAVIOUR_ATTR",
			"behaviour", behaviourName, "implementing_module", parentMod)
		add(ent)
	}

	// 6. defprotocol  → SCOPE.Component/interface  (protocol = structural interface)
	for _, m := range reDefProtocol.FindAllStringSubmatchIndex(src, -1) {
		protoName := src[m[2]:m[3]]
		ent := makeEntity(protoName, "SCOPE.Component", "interface", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "elixir_typespec", "provenance", "INFERRED_FROM_DEFPROTOCOL")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
