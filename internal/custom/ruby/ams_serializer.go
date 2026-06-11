package ruby

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

// ams_serializer.go — Ruby ActiveModel::Serializer (AMS) DTO FIELD-as-member
// indexing (issue #4715), generalizing the uniform DTO field-member model from
// the JS (javascript/validation_schema.go::emitSchemaFieldMembers, #4635),
// Python (python/dto_field_members.go, #4613), Java (java/spring_dto_fields.go,
// #4613), Go (golang/dto_field_members.go) and C# (csharp/dto_field_members.go)
// emitters.
//
// An `ActiveModel::Serializer` subclass declares its serialized shape via
// `attributes :a, :b, :c` and `attribute :x` macros. Each declared attribute
// becomes a `SCOPE.Schema` subtype=field sub-entity named `<Serializer>.<attr>`,
// carrying the SAME property shape as the other frameworks so the cross-framework
// field-level diff tools + the dashboard /shape ShapeTree resolver treat all
// frameworks uniformly:
//
//	field_name   — the attribute name
//	field_type   — "any" (AMS attributes are dynamically typed; best-effort)
//	parent_class — the owning serializer class name
//	provenance   — INFERRED_FROM_SCHEMA_FIELD_MEMBERSHIP
//	library      — "active_model_serializer"
//
// The child's Signature is the Java-style `<type> <name>` so the shape resolver's
// parseFieldSignature recovers (type, name) uniformly. A CONTAINS edge binds each
// field to its owner serializer via the `Class:<Serializer>` byName fallback.
//
// Also emits a SCOPE.Schema (subtype=dto) node per serializer class so the dto
// node exists for the membership edge to resolve against.

func init() {
	extractor.Register("custom_ruby_ams_serializer", &amsSerializerExtractor{})
}

type amsSerializerExtractor struct{}

func (e *amsSerializerExtractor) Language() string { return "custom_ruby_ams_serializer" }

var (
	// class FooSerializer < ActiveModel::Serializer
	// Also matches the common base `< ApplicationSerializer` only when the class
	// name ends in `Serializer` (conservative — avoids false positives).
	reAmsSerializerClass = regexp.MustCompile(
		`(?m)^\s*class\s+([A-Z][A-Za-z0-9_:]*Serializer)\s*<\s*([A-Z][A-Za-z0-9_:]*)`)

	// attributes :a, :b, :c   (one declaration, comma-separated symbols)
	reAmsAttributes = regexp.MustCompile(
		`(?m)^\s*attributes\s+(:[A-Za-z_]\w*(?:\s*,\s*:[A-Za-z_]\w*)*)`)

	// attribute :x   /   attribute :x, key: :y
	reAmsAttribute = regexp.MustCompile(
		`(?m)^\s*attribute\s+:([A-Za-z_]\w*)`)

	// :symbol token inside an attributes list.
	reAmsSymbol = regexp.MustCompile(`:([A-Za-z_]\w*)`)
)

// amsSerializerReferenced reports whether the source likely declares an AMS
// serializer.
func amsSerializerReferenced(src string) bool {
	return strings.Contains(src, "ActiveModel::Serializer") ||
		(strings.Contains(src, "Serializer") &&
			(strings.Contains(src, "attributes ") || strings.Contains(src, "attribute ")))
}

func (e *amsSerializerExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/ruby")
	_, span := tracer.Start(ctx, "indexer.ruby_ams_serializer_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "ruby" {
		return nil, nil
	}
	src := string(file.Content)
	if !amsSerializerReferenced(src) {
		return nil, nil
	}

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

	for _, m := range reAmsSerializerClass.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		baseClass := src[m[4]:m[5]]
		// Only treat `< X` as an AMS serializer when the base is ActiveModel's
		// serializer or an application serializer base.
		if baseClass != "ActiveModel::Serializer" &&
			!strings.HasSuffix(baseClass, "Serializer") {
			continue
		}
		line := lineOf(src, m[0])

		// The serializer class node (so the membership edge resolves).
		dto := makeEntity(className, "SCOPE.Schema", "dto", file.Path, file.Language, line)
		setProps(&dto,
			"framework", "active_model_serializer",
			"provenance", "INFERRED_FROM_AMS_SERIALIZER",
			"base_class", baseClass)
		add(dto)

		body := amsClassBody(src, m[0])
		ownerLine := line

		var attrNames []string
		// attributes :a, :b, :c
		for _, am := range reAmsAttributes.FindAllStringSubmatch(body, -1) {
			for _, s := range reAmsSymbol.FindAllStringSubmatch(am[1], -1) {
				attrNames = append(attrNames, s[1])
			}
		}
		// attribute :x
		for _, am := range reAmsAttribute.FindAllStringSubmatch(body, -1) {
			attrNames = append(attrNames, am[1])
		}

		fieldSeen := make(map[string]bool)
		for _, attr := range attrNames {
			if attr == "" || fieldSeen[attr] {
				continue
			}
			fieldSeen[attr] = true

			childName := className + "." + attr
			child := makeEntity(childName, "SCOPE.Schema", "field", file.Path, file.Language, ownerLine)
			// Java-style signature `<type> <name>`; AMS attrs are untyped → "any".
			child.Signature = "any " + attr
			setProps(&child,
				"library", "active_model_serializer",
				"pattern_type", "field",
				"field_name", attr,
				"field_type", "any",
				"parent_class", className,
				"provenance", "INFERRED_FROM_SCHEMA_FIELD_MEMBERSHIP")
			child.Relationships = append(child.Relationships,
				containsFieldEdge(className, child.ID, attr, "active_model_serializer"))
			add(child)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// amsClassBody returns the source of the serializer class body, from the class
// declaration up to its matching `end`. Ruby has no braces, so this is a
// best-effort scan: it tracks block-opening keywords (def/do/if/class/...) and
// `end` tokens at line granularity, returning the body up to the class's closing
// `end`. Good enough to scope `attributes`/`attribute` declarations to a class.
func amsClassBody(src string, classDeclStart int) string {
	// Start after the class declaration line.
	nl := strings.IndexByte(src[classDeclStart:], '\n')
	if nl < 0 {
		return src[classDeclStart:]
	}
	bodyStart := classDeclStart + nl + 1
	lines := strings.Split(src[bodyStart:], "\n")
	depth := 1 // we are inside the class
	var body []string
	openRe := regexp.MustCompile(`^\s*(?:class|module|def|if|unless|case|while|until|begin)\b|\bdo\s*(?:\|[^|]*\|)?\s*$`)
	endRe := regexp.MustCompile(`^\s*end\b`)
	for _, ln := range lines {
		if endRe.MatchString(ln) {
			depth--
			if depth == 0 {
				break
			}
			body = append(body, ln)
			continue
		}
		if openRe.MatchString(ln) {
			depth++
		}
		body = append(body, ln)
	}
	return strings.Join(body, "\n")
}
