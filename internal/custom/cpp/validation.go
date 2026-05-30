package cpp

// validation.go — C++ HTTP framework request validation / DTO extractor.
//
// Covered surfaces:
//
//  1. oatpp DTO macro fields (with type + description):
//     DTO_FIELD(type, name)                 — declares a request/response field
//     DTO_FIELD(type, name, "json_key")
//     DTO_FIELD_INFO(name) { info->description = "..."; } — field documentation
//     The captured field carries its declared C++ type (e.g. String, Int32,
//     Vector<String>) in the field_type property.
//
//  1a. nlohmann/json struct mapping:
//     NLOHMANN_DEFINE_TYPE_INTRUSIVE(User, name, age)
//     NLOHMANN_DEFINE_TYPE_NON_INTRUSIVE(User, name, age)
//     Each mapped member becomes a request/response field of the struct.
//
//  2. Generic JSON body / parameter extraction patterns used across
//     crow, drogon, pistache, cpprestsdk, oatpp, poco, restbed, restinio:
//     - req.getParam("key")  / req.get_param("key")
//     - req["field"] / req.body["field"]
//     - body.get<Type>("field")
//     - j["field"] / nlohmann::json j = ...
//     - value_of<Type> / as<Type>()
//
//  3. Drogon request parameter access:
//     req->getParameter("key")
//     req->getBody()
//     auto j = req->getJsonObject();
//
//  4. cpprestsdk JSON extraction:
//     request.extract_json() / jv["field"]
//
// Each detected validation site emits a SCOPE.Schema/request_param entity
// with the param name and detected framework.
//
// Status: partial (regex/heuristic; no full AST type resolution).

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
	extractor.Register("custom_cpp_validation", &cppValidationExtractor{})
}

type cppValidationExtractor struct{}

func (e *cppValidationExtractor) Language() string { return "custom_cpp_validation" }

var (
	// oatpp DTO_FIELD(String, username) or DTO_FIELD(String, username, "username")
	// capture: (1) type, (2) field name
	reDTOField = regexp.MustCompile(
		`(?m)\bDTO_FIELD\s*\(\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*(?:\s*<[^>]+>)?)\s*,\s*([A-Za-z_]\w*)`,
	)

	// oatpp DTO_FIELD_INFO(field) { info->description = "..."; }
	// capture: (1) field name, (2) description text
	reDTOFieldInfoDesc = regexp.MustCompile(
		`(?ms)\bDTO_FIELD_INFO\s*\(\s*([A-Za-z_]\w*)\s*\).*?info\s*->\s*description\s*=\s*"((?:[^"\\]|\\.)*)"`,
	)

	// nlohmann: NLOHMANN_DEFINE_TYPE_INTRUSIVE(Type, f1, f2, ...)
	// also NON_INTRUSIVE / _WITH_DEFAULT variants.
	// capture: (1) struct type, (2) raw member list
	reNlohmannDefineType = regexp.MustCompile(
		`(?ms)\bNLOHMANN_DEFINE_TYPE(?:_INTRUSIVE|_NON_INTRUSIVE)?(?:_WITH_DEFAULT)?\s*\(\s*([A-Za-z_]\w*)\s*,\s*([^)]*)\)`,
	)

	// Drogon: req->getParameter("key") or req->getJsonObject()["key"]
	// capture: (1) key string
	reDrogonGetParam = regexp.MustCompile(
		`(?m)req\s*(?:->|\.)\s*getParameter\s*\(\s*"([^"]+)"`,
	)

	// Generic: req.getParam("key") / req.get_param("key")
	// capture: (1) key string
	reGenericGetParam = regexp.MustCompile(
		`(?m)\breq\s*(?:->|\.)\s*(?:getParam|get_param|getQueryString|getQueryParam)\s*\(\s*"([^"]+)"`,
	)

	// cpprestsdk: body["field"] / jv["field"] after extract_json()
	// capture: (1) field
	reCppRestJSONField = regexp.MustCompile(
		`(?m)\b(?:body|jv|json_val|j)\s*\[\s*(?:U\s*\(\s*)?"([^"]+)"`,
	)

	// nlohmann JSON: j["field"] / j.at("field") / j.value("field", ...)
	// capture: (1) field
	reNlohmannJSON = regexp.MustCompile(
		`(?m)\b(?:j|req_json|json|body)\s*(?:\[\s*"([^"]+)"\s*\]|\.at\s*\(\s*"([^"]+)"\s*\)|\.value\s*\(\s*"([^"]+)"\s*,)`,
	)

	// POCO: form.get("key", "") or form["key"]
	// capture: (1) key
	rePocoFormGet = regexp.MustCompile(
		`(?m)\bform\s*(?:\.get\s*\(\s*"([^"]+)"\s*,|(?:->|\[)\s*"([^"]+)")`,
	)

	// nlohmann required-field check: j.contains("field") used to validate
	// that a request body carries a field.
	// capture: (1) field
	reNlohmannContains = regexp.MustCompile(
		`(?m)\b(?:j|req_json|json|body)\s*\.\s*contains\s*\(\s*"([^"]+)"\s*\)`,
	)
)

func (e *cppValidationExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.cpp_validation_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "cpp" {
		return nil, nil
	}

	src := string(file.Content)
	hasValidation := strings.Contains(src, "DTO_FIELD") ||
		strings.Contains(src, "getParam") ||
		strings.Contains(src, "get_param") ||
		strings.Contains(src, "getParameter") ||
		strings.Contains(src, "extract_json") ||
		strings.Contains(src, "DTO_FIELD") ||
		strings.Contains(src, "NLOHMANN_DEFINE_TYPE") ||
		(strings.Contains(src, `["`) && (strings.Contains(src, "body") || strings.Contains(src, " j[")))
	if !hasValidation {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	byName := make(map[string]int) // param_name|framework -> index into entities

	emitParam := func(paramName, framework, provenance string, offset int) {
		key := paramName + "|" + framework
		if seen[key] || paramName == "" {
			return
		}
		seen[key] = true
		ent := makeEntity(paramName, "SCOPE.Schema", "request_param", file.Path, file.Language, lineOf(src, offset))
		setProps(&ent,
			"param_name", paramName,
			"framework", framework,
			"provenance", provenance,
		)
		byName[key] = len(entities)
		entities = append(entities, ent)
	}

	// emitField is like emitParam but additionally records the declared C++
	// field type (e.g. String, Int32, Vector<String>) so downstream schema
	// consumers carry the concrete type, matching the TS/JS DTO bar.
	emitField := func(fieldName, fieldType, framework, provenance string, offset int) {
		key := fieldName + "|" + framework
		if !seen[key] {
			emitParam(fieldName, framework, provenance, offset)
		}
		if idx, ok := byName[key]; ok && fieldType != "" {
			entities[idx].Properties["field_type"] = fieldType
		}
	}

	// setFieldProp updates a property on an already-emitted field, if present.
	setFieldProp := func(fieldName, framework, prop, value string) {
		key := fieldName + "|" + framework
		if idx, ok := byName[key]; ok && value != "" {
			entities[idx].Properties[prop] = value
		}
	}

	// oatpp DTO fields (carry declared type)
	for _, m := range reDTOField.FindAllStringSubmatchIndex(src, -1) {
		fieldType := strings.TrimSpace(src[m[2]:m[3]])
		fieldName := strings.TrimSpace(src[m[4]:m[5]])
		emitField(fieldName, normalizeType(fieldType), "oatpp", "INFERRED_FROM_DTO_FIELD", m[0])
	}

	// oatpp DTO_FIELD_INFO descriptions attach to the matching field.
	for _, m := range reDTOFieldInfoDesc.FindAllStringSubmatchIndex(src, -1) {
		fieldName := strings.TrimSpace(src[m[2]:m[3]])
		desc := strings.TrimSpace(src[m[4]:m[5]])
		// The field may not have been emitted yet if INFO precedes FIELD; ensure it exists.
		if _, ok := byName[fieldName+"|oatpp"]; !ok {
			emitParam(fieldName, "oatpp", "INFERRED_FROM_DTO_FIELD_INFO", m[0])
		}
		setFieldProp(fieldName, "oatpp", "description", desc)
	}

	// nlohmann/json NLOHMANN_DEFINE_TYPE struct mapping: each member becomes a field.
	for _, m := range reNlohmannDefineType.FindAllStringSubmatchIndex(src, -1) {
		structType := strings.TrimSpace(src[m[2]:m[3]])
		members := src[m[4]:m[5]]
		for _, raw := range strings.Split(members, ",") {
			member := strings.TrimSpace(raw)
			if member == "" || !isIdentifier(member) {
				continue
			}
			emitField(member, "", "nlohmann", "INFERRED_FROM_NLOHMANN_DEFINE_TYPE", m[0])
			setFieldProp(member, "nlohmann", "struct_type", structType)
		}
	}

	// Drogon request params
	for _, m := range reDrogonGetParam.FindAllStringSubmatchIndex(src, -1) {
		emitParam(strings.TrimSpace(src[m[2]:m[3]]), "drogon", "INFERRED_FROM_REQUEST_PARAM", m[0])
	}

	// Generic get_param / getParam
	for _, m := range reGenericGetParam.FindAllStringSubmatchIndex(src, -1) {
		emitParam(strings.TrimSpace(src[m[2]:m[3]]), "generic", "INFERRED_FROM_REQUEST_PARAM", m[0])
	}

	// cpprestsdk JSON field access
	for _, m := range reCppRestJSONField.FindAllStringSubmatchIndex(src, -1) {
		emitParam(strings.TrimSpace(src[m[2]:m[3]]), "cpprestsdk", "INFERRED_FROM_JSON_FIELD", m[0])
	}

	// nlohmann JSON field access
	for _, m := range reNlohmannJSON.FindAllStringSubmatchIndex(src, -1) {
		for _, gi := range []int{2, 4, 6} {
			if m[gi] >= 0 {
				emitParam(strings.TrimSpace(src[m[gi]:m[gi+1]]), "generic", "INFERRED_FROM_JSON_FIELD", m[0])
				break
			}
		}
	}

	// POCO form extraction
	for _, m := range rePocoFormGet.FindAllStringSubmatchIndex(src, -1) {
		for _, gi := range []int{2, 4} {
			if m[gi] >= 0 {
				emitParam(strings.TrimSpace(src[m[gi]:m[gi+1]]), "poco", "INFERRED_FROM_FORM_PARAM", m[0])
				break
			}
		}
	}

	// nlohmann required-field validation (j.contains("field")) marks the
	// referenced field as explicitly validated.
	for _, m := range reNlohmannContains.FindAllStringSubmatchIndex(src, -1) {
		field := strings.TrimSpace(src[m[2]:m[3]])
		if _, ok := byName[field+"|generic"]; !ok {
			emitParam(field, "generic", "INFERRED_FROM_JSON_FIELD", m[0])
		}
		setFieldProp(field, "generic", "validation", "required")
	}

	return entities, nil
}

// normalizeType collapses internal whitespace inside a C++ type so that
// "Vector < String >" becomes "Vector<String>".
func normalizeType(t string) string {
	return strings.Join(strings.Fields(t), "")
}

// isIdentifier reports whether s is a plain C++ identifier (used to filter
// NLOHMANN_DEFINE_TYPE member lists, which only contain bare member names).
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
