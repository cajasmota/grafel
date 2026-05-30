package cpp

// validation.go — C++ HTTP framework request validation / DTO extractor.
//
// Covered surfaces:
//
//  1. oatpp DTO macro fields:
//     DTO_FIELD(type, name)                 — declares a request/response field
//     DTO_FIELD(type, name, "json_key")
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
		`(?m)\bDTO_FIELD\s*\(\s*([A-Za-z_]\w*(?:<[^>]+>)?)\s*,\s*([A-Za-z_]\w*)`,
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
		(strings.Contains(src, `["`) && (strings.Contains(src, "body") || strings.Contains(src, " j[")))
	if !hasValidation {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

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
		entities = append(entities, ent)
	}

	// oatpp DTO fields
	for _, m := range reDTOField.FindAllStringSubmatchIndex(src, -1) {
		fieldName := strings.TrimSpace(src[m[4]:m[5]])
		emitParam(fieldName, "oatpp", "INFERRED_FROM_DTO_FIELD", m[0])
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

	return entities, nil
}
