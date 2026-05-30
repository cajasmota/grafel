// Package kotlin — kotlinx.serialization DTO extractor.
//
// Covers record lang.kotlin.framework.kotlinx-serialization:
//   - dto_extraction  (missing → full)
//
// kotlinx.serialization marks serializable payload types with `@Serializable`
// on a (data) class. Each property carries serialization metadata that this
// extractor records by value:
//
//	@Serializable
//	data class User(
//	    val id: Long,
//	    @SerialName("user_name") val name: String,
//	    val age: Int = 0,
//	    @Required val email: String?,
//	    @Transient val cached: String = "",
//	)
//
// For every @Serializable class we emit one SCOPE.Schema(subtype="dto") whose
// properties record, per field:
//   - prop.<f>.type        declared Kotlin type
//   - prop.<f>.nullable    "true"/"false" (trailing `?`)
//   - prop.<f>.wire_name   @SerialName("...") override, else the field name
//   - prop.<f>.default     parsed default expression after `=` (when present)
//   - prop.<f>.required    "true" when @Required present
//   - prop.<f>.transient   "true" when @Transient present (excluded from wire)
//   - prop.<f>.polymorphic "true" when @Polymorphic present
//
// The DTO entity additionally records `serializable=true`, the `properties`
// list and `property_count`. Class-level @SerialName (custom serial name for
// the type) is captured as `serial_name`.
//
// This is gated on the presence of `@Serializable` so it does not double-emit
// with the generic data-class DTO path in validation.go for non-serializable
// classes; for serializable classes it adds richer, serialization-specific
// metadata (required/transient/polymorphic) absent from that path.
package kotlin

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_kotlin_kotlinx_serialization", &kotlinxSerializationExtractor{})
}

type kotlinxSerializationExtractor struct{}

func (e *kotlinxSerializationExtractor) Language() string {
	return "custom_kotlin_kotlinx_serialization"
}

var (
	// @Serializable annotation anchoring a serializable class.
	reKxSerializable = regexp.MustCompile(`@Serializable\b`)

	// A @Serializable class head. Captures the class name (g1). The leading
	// annotation block is allowed to contain @Serializable plus others.
	reKxSerializableClass = regexp.MustCompile(
		`(?m)^[ \t]*(?:@\w+(?:\s*\([^)]*\))?\s*)*@Serializable\b\s*(?:@\w+(?:\s*\([^)]*\))?\s*)*` +
			`(?:data\s+class|class|object|enum\s+class|sealed\s+class)\s+([A-Z][A-Za-z0-9_]*)`,
	)

	// A property declaration with its leading annotation block.
	// g1=anno block, g2=val/var, g3=name, g4=type, g5=default (optional).
	reKxProperty = regexp.MustCompile(
		`(?m)((?:@(?:field:|get:|param:|property:|set:)?\w+(?:\s*\([^)]*\))?\s*)*)` +
			`\b(val|var)\s+([a-z_][A-Za-z0-9_]*)\s*:\s*` +
			`([A-Za-z_][A-Za-z0-9_.]*(?:\s*<[^<>]*>)?\??)` +
			`(?:\s*=\s*([^,)\n]+))?`,
	)

	reKxSerialNameAnno  = regexp.MustCompile(`@SerialName\s*\(\s*"([^"]+)"\s*\)`)
	reKxRequiredAnno    = regexp.MustCompile(`@Required\b`)
	reKxTransientAnno   = regexp.MustCompile(`@Transient\b`)
	reKxPolymorphicAnno = regexp.MustCompile(`@Polymorphic\b`)
)

func (e *kotlinxSerializationExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlinx_serialization.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "kotlinx-serialization"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)
	if !reKxSerializable.MatchString(src) {
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

	heads := reKxSerializableClass.FindAllStringSubmatchIndex(src, -1)
	for i, m := range heads {
		className := src[m[2]:m[3]]
		if kotlinPrimitives[className] {
			continue
		}
		bodyEnd := len(src)
		if i+1 < len(heads) {
			bodyEnd = heads[i+1][0]
		}
		body := src[m[0]:bodyEnd]
		line := lineOf(src, m[0])

		dto := makeEntity(className, "SCOPE.Schema", "dto", file.Path, "kotlin", line)
		setProps(&dto,
			"framework", "kotlinx-serialization",
			"serializable", "true",
			"provenance", "INFERRED_FROM_KOTLINX_SERIALIZABLE",
		)
		// Class-level @SerialName (custom type serial name) — only when it
		// appears before the class keyword in the head match.
		headText := src[m[0]:m[3]]
		if sn := reKxSerialNameAnno.FindStringSubmatch(headText); sn != nil {
			setProps(&dto, "serial_name", sn[1])
		}

		var propList []string
		for _, pm := range reKxProperty.FindAllStringSubmatchIndex(body, -1) {
			annoBlock := body[pm[2]:pm[3]]
			pname := body[pm[6]:pm[7]]
			ptype := strings.TrimSpace(body[pm[8]:pm[9]])
			pdefault := ""
			if pm[10] >= 0 {
				pdefault = strings.TrimSpace(body[pm[10]:pm[11]])
			}
			nullable := strings.HasSuffix(ptype, "?")

			wire := pname
			if w := reKxSerialNameAnno.FindStringSubmatch(annoBlock); w != nil {
				wire = w[1]
			}

			setProps(&dto,
				"prop."+pname+".type", ptype,
				"prop."+pname+".nullable", strconv.FormatBool(nullable),
				"prop."+pname+".wire_name", wire,
			)
			if pdefault != "" {
				dto.Properties["prop."+pname+".default"] = pdefault
			}
			if reKxRequiredAnno.MatchString(annoBlock) {
				dto.Properties["prop."+pname+".required"] = "true"
			}
			if reKxTransientAnno.MatchString(annoBlock) {
				dto.Properties["prop."+pname+".transient"] = "true"
			}
			if reKxPolymorphicAnno.MatchString(annoBlock) {
				dto.Properties["prop."+pname+".polymorphic"] = "true"
			}
			propList = append(propList, pname)
		}
		if len(propList) > 0 {
			setProps(&dto,
				"properties", strings.Join(propList, ","),
				"property_count", strconv.Itoa(len(propList)),
			)
		}
		add(dto)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
