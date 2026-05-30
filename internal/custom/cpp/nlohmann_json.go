package cpp

// nlohmann_json.go — nlohmann/json (C++) DTO / serialization-mapping extractor.
//
// nlohmann/json maps a C++ struct to/from JSON in two ways:
//
//  1. The intrusive/non-intrusive macros, which list the serialized members:
//
//	    NLOHMANN_DEFINE_TYPE_INTRUSIVE(User, name, age, email)
//	    NLOHMANN_DEFINE_TYPE_NON_INTRUSIVE(Address, street, city)
//	    NLOHMANN_DEFINE_TYPE_NON_INTRUSIVE_WITH_DEFAULT(Config, host, port)
//
//  2. Hand-written free-function pairs:
//
//	    void to_json(json& j, const User& u)   { j = json{{"name", u.name}}; }
//	    void from_json(const json& j, User& u) { j.at("name").get_to(u.name); }
//
// For every mapped type we emit one SCOPE.Schema(subtype="dto") entity carrying
// the serialized field names, plus one SCOPE.Schema(subtype="field") child per
// macro member. The to_json/from_json pair is recorded as the serialization
// binding (whether a type is read-from-JSON, written-to-JSON, or both).
//
// The validation.go extractor independently emits request_param fields for the
// same macro (handler-attribution surface); this extractor owns the *DTO model*
// surface for the dedicated nlohmann/json coverage record. The two are
// complementary and de-duplicated by Kind+Subtype+Name.
//
// Status: full for the DTO type + macro-listed fields (the member list is the
// serialization contract); honest-partial for nested/typed shapes, since member
// C++ types are declared elsewhere in the struct body.

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
	extractor.Register("custom_cpp_nlohmann_json", &cppNlohmannExtractor{})
}

type cppNlohmannExtractor struct{}

func (e *cppNlohmannExtractor) Language() string { return "custom_cpp_nlohmann_json" }

var (
	// NLOHMANN_DEFINE_TYPE[_INTRUSIVE|_NON_INTRUSIVE][_WITH_DEFAULT](Type, m1, m2, ...)
	// Capture 1 = type, 2 = member list.
	reNJDefineType = regexp.MustCompile(
		`(?ms)\bNLOHMANN_DEFINE_TYPE(_INTRUSIVE|_NON_INTRUSIVE)?(?:_WITH_DEFAULT)?\s*\(\s*([A-Za-z_]\w*)\s*,\s*([^)]*)\)`,
	)

	// to_json(json& j, const User& u) / from_json(const json& j, User& u)
	// Capture 1 = to_json|from_json, 2 = the user struct type (the non-json arg).
	reNJToFromJSON = regexp.MustCompile(
		`(?m)\b(to_json|from_json)\s*\(\s*(?:const\s+)?(?:nlohmann::)?(?:ordered_)?json\s*&\s*\w+\s*,\s*(?:const\s+)?([A-Za-z_]\w*)\s*&`,
	)
)

func (e *cppNlohmannExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.cpp_nlohmann_json.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "nlohmann_json"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || (file.Language != "cpp" && file.Language != "c") {
		return nil, nil
	}
	src := string(file.Content)

	// File-signal gate.
	if !strings.Contains(src, "NLOHMANN_DEFINE_TYPE") &&
		!strings.Contains(src, "to_json") &&
		!strings.Contains(src, "from_json") {
		return nil, nil
	}

	var entities []types.EntityRecord
	dtoIdx := make(map[string]int) // type name -> index of its DTO entity
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	ensureDTO := func(typeName string, line int, mapping string) int {
		name := "nlohmann_dto:" + typeName
		if idx, ok := dtoIdx[typeName]; ok {
			return idx
		}
		ent := makeEntity(name, "SCOPE.Schema", "dto", file.Path, file.Language, line)
		setProps(&ent, "framework", "nlohmann_json",
			"provenance", "INFERRED_FROM_NLOHMANN_JSON",
			"dto_name", typeName, "serialization", "nlohmann_json")
		if mapping != "" {
			setProps(&ent, "mapping", mapping)
		}
		seen[ent.Kind+":"+ent.Subtype+":"+ent.Name] = true
		entities = append(entities, ent)
		dtoIdx[typeName] = len(entities) - 1
		return len(entities) - 1
	}

	// 1. NLOHMANN_DEFINE_TYPE macros -> DTO + per-member fields.
	for _, m := range reNJDefineType.FindAllStringSubmatchIndex(src, -1) {
		variant := "" // INTRUSIVE / NON_INTRUSIVE / generic
		if m[2] >= 0 {
			variant = strings.TrimPrefix(src[m[2]:m[3]], "_")
		}
		typeName := src[m[4]:m[5]]
		memberList := src[m[6]:m[7]]
		line := lineOf(src, m[0])

		idx := ensureDTO(typeName, line, "macro")
		if variant != "" {
			entities[idx].Properties["macro_variant"] = variant
		}

		var fields []string
		for _, raw := range strings.Split(memberList, ",") {
			member := strings.TrimSpace(raw)
			if member == "" || !isIdentifier(member) {
				continue
			}
			fields = append(fields, member)
			fldEnt := makeEntity(typeName+"."+member, "SCOPE.Schema", "field", file.Path, file.Language, line)
			setProps(&fldEnt, "framework", "nlohmann_json",
				"provenance", "INFERRED_FROM_NLOHMANN_DEFINE_TYPE",
				"parent_dto", typeName, "field_name", member,
				"serialization", "nlohmann_json")
			add(fldEnt)
		}
		entities[idx].Properties["field_count"] = itoa(len(fields))
		if len(fields) > 0 {
			entities[idx].Properties["fields"] = strings.Join(fields, ",")
		}
	}

	// 2. to_json / from_json free-function pairs -> serialization binding.
	roles := make(map[string]map[string]bool) // type -> {to_json,from_json}
	firstLine := make(map[string]int)
	for _, m := range reNJToFromJSON.FindAllStringSubmatchIndex(src, -1) {
		fn := src[m[2]:m[3]]
		typeName := src[m[4]:m[5]]
		if roles[typeName] == nil {
			roles[typeName] = map[string]bool{}
			firstLine[typeName] = lineOf(src, m[0])
		}
		roles[typeName][fn] = true
	}
	for typeName, r := range roles {
		idx := ensureDTO(typeName, firstLine[typeName], "free_function")
		dir := ""
		switch {
		case r["to_json"] && r["from_json"]:
			dir = "bidirectional"
		case r["to_json"]:
			dir = "serialize"
		case r["from_json"]:
			dir = "deserialize"
		}
		if dir != "" {
			entities[idx].Properties["serialization_direction"] = dir
		}
		entities[idx].Properties["has_free_functions"] = "true"
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// itoa is a tiny strconv.Itoa wrapper kept local to avoid an extra import churn
// in this file (validation.go in the same package already pulls strconv via
// other files; using a helper keeps the import set minimal here).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
