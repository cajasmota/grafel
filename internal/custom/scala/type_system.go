// Package scala — Scala type-system extractor.
//
// Covers (missing → partial or full):
//
//	Framework records         Cap                          Status
//	─────────────────────────────────────────────────────────────
//	all jvm_backend + play    Type System/type_extraction         partial
//	all jvm_backend + play    Type System/enum_extraction         partial
//	all jvm_backend + play    Type System/interface_extraction    partial
//	all jvm_backend + play    Type System/type_alias_extraction   partial
//
// Scala's type system:
//   - case class Foo(...)              → type_extraction (value object/DTO)
//   - sealed trait / sealed abstract class → enum_extraction (ADT discriminant)
//   - enum Foo { case A, B }           → enum_extraction (Scala 3 enum)
//   - trait Foo                        → interface_extraction
//   - abstract class Foo               → interface_extraction
//   - type Alias = SomeType            → type_alias_extraction
//   - opaque type Alias = T            → type_alias_extraction (Scala 3)
//
// Honest limit: regex-based, file-local. Cross-file ADT hierarchies
// (sealed trait in one file, subclasses in another) are only partially
// detected. Cells are partial.
package scala

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
	extractor.Register("custom_scala_type_system", &scalaTypeSystemExtractor{})
}

type scalaTypeSystemExtractor struct{}

func (e *scalaTypeSystemExtractor) Language() string { return "custom_scala_type_system" }

// ---------------------------------------------------------------------------
// Regexes
// ---------------------------------------------------------------------------

var (
	// type_extraction: case class (value object / DTO)
	// Matches: case class Foo(...) or case class Foo[T](...)
	reCaseClass = regexp.MustCompile(
		`(?m)^\s*(?:final\s+)?case\s+class\s+(\w+)\s*(?:\[[^\]]*\])?\s*(?:\([^)]*\))?`)

	// type_extraction: plain class
	rePlainClass = regexp.MustCompile(
		`(?m)^\s*(?:final\s+|abstract\s+|sealed\s+abstract\s+)?class\s+(\w+)\s*(?:\[[^\]]*\])?`)

	// type_extraction: object (singleton companion)
	reObject = regexp.MustCompile(
		`(?m)^\s*(?:case\s+)?object\s+(\w+)\b`)

	// enum_extraction: sealed trait (Scala 2 ADT discriminant)
	reSealedTrait = regexp.MustCompile(
		`(?m)^\s*sealed\s+(?:abstract\s+)?trait\s+(\w+)\b`)

	// enum_extraction: sealed abstract class (Scala 2 sealed base)
	reSealedAbstractClass = regexp.MustCompile(
		`(?m)^\s*sealed\s+abstract\s+class\s+(\w+)\b`)

	// enum_extraction: Scala 3 enum
	reScala3Enum = regexp.MustCompile(
		`(?m)^\s*enum\s+(\w+)\s*(?:\[[^\]]*\])?\s*(?:extends\s+[^\{]+)?\{`)

	// interface_extraction: trait (not sealed — sealed already caught above)
	reTrait = regexp.MustCompile(
		`(?m)^\s*(?:private\s+|protected\s+)?(?:sealed\s+)?trait\s+(\w+)\b`)

	// interface_extraction: abstract class
	reAbstractClass = regexp.MustCompile(
		`(?m)^\s*(?:private\s+|protected\s+)?abstract\s+class\s+(\w+)\b`)

	// type_alias_extraction: type Alias = SomeType
	reTypeAlias = regexp.MustCompile(
		`(?m)^\s*type\s+(\w+)(?:\[[^\]]*\])?\s*=\s*(.+)`)

	// type_alias_extraction: opaque type (Scala 3)
	reOpaqueType = regexp.MustCompile(
		`(?m)^\s*opaque\s+type\s+(\w+)(?:\[[^\]]*\])?\s*=\s*(.+)`)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *scalaTypeSystemExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/scala")
	_, span := tracer.Start(ctx, "indexer.scala_type_system.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("extractor", "type_system"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "scala" {
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

	// --- type_extraction: case class (value objects / DTOs) ---
	for _, m := range reCaseClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "case_class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "case_class",
			"provenance", "SCALA_CASE_CLASS",
		)
		add(ent)
	}

	// --- type_extraction: plain class ---
	for _, m := range rePlainClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		// skip if sealed abstract class (already caught by enum path)
		ent := makeEntity(name, "SCOPE.Type", "class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "class",
			"provenance", "SCALA_CLASS",
		)
		add(ent)
	}

	// --- type_extraction: object (companion/module) ---
	for _, m := range reObject.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "object", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "object",
			"provenance", "SCALA_OBJECT",
		)
		add(ent)
	}

	// --- enum_extraction: sealed trait (Scala 2 ADT) ---
	for _, m := range reSealedTrait.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "sealed_trait", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "sealed_trait",
			"provenance", "SCALA_SEALED_TRAIT",
			"is_adt", "true",
		)
		add(ent)
	}

	// --- enum_extraction: sealed abstract class ---
	for _, m := range reSealedAbstractClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "sealed_abstract_class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "sealed_abstract_class",
			"provenance", "SCALA_SEALED_ABSTRACT_CLASS",
			"is_adt", "true",
		)
		add(ent)
	}

	// --- enum_extraction: Scala 3 enum ---
	for _, m := range reScala3Enum.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "enum", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "enum",
			"provenance", "SCALA3_ENUM",
			"is_adt", "true",
		)
		add(ent)
	}

	// --- interface_extraction: trait ---
	for _, m := range reTrait.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Interface", "trait", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "trait",
			"provenance", "SCALA_TRAIT",
		)
		add(ent)
	}

	// --- interface_extraction: abstract class ---
	for _, m := range reAbstractClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Interface", "abstract_class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "abstract_class",
			"provenance", "SCALA_ABSTRACT_CLASS",
		)
		add(ent)
	}

	// --- type_alias_extraction: type Alias = ... ---
	for _, m := range reTypeAlias.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "type_alias", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "type_alias",
			"provenance", "SCALA_TYPE_ALIAS",
		)
		add(ent)
	}

	// --- type_alias_extraction: opaque type (Scala 3) ---
	for _, m := range reOpaqueType.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "opaque_type", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "opaque_type",
			"provenance", "SCALA3_OPAQUE_TYPE",
		)
		add(ent)
	}

	return entities, nil
}
