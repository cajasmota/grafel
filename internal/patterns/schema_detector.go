package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// schemaDetector detects schema validation library usage.
// Matches Python schema_detector.py.
type schemaDetector struct{}

var schemaImportTokens = []string{
	"pydantic", "marshmallow", "cerberus", "jsonschema", "voluptuous",
	"zod", "yup", "joi", "@hapi/joi", "superstruct",
	"javax.validation", "jakarta.validation", "@NotNull", "@Valid",
	"FluentValidation", "DataAnnotations",
	"json-schema-validator",
}

var (
	schemaPydanticRE    = regexp.MustCompile(`class\s+\w+\s*\(\s*(?:pydantic\.)?BaseModel\s*\)`)
	schemaMarshmallowRE = regexp.MustCompile(`class\s+\w+\s*\(\s*(?:marshmallow\.)?Schema\s*\)`)
	schemaZodRE         = regexp.MustCompile(`z\.\s*(?:object|string|number|array|boolean|enum)\s*\(`)
	schemaYupRE         = regexp.MustCompile(`yup\.\s*(?:object|string|number|array|boolean)\s*\(`)
	schemaJoiRE         = regexp.MustCompile(`Joi\.\s*(?:object|string|number|array|boolean)\s*\(`)
	schemaJavaBeanValRE = regexp.MustCompile(`@(?:NotNull|NotBlank|NotEmpty|Size|Min|Max|Email|Pattern)\b`)
)

func (s *schemaDetector) Category() string { return "schema_validation" }

func (s *schemaDetector) AppliesTo(src string) bool {
	srcLower := strings.ToLower(src)
	for _, tok := range schemaImportTokens {
		if strings.Contains(srcLower, strings.ToLower(tok)) {
			return true
		}
	}
	return schemaZodRE.MatchString(src) ||
		schemaYupRE.MatchString(src) ||
		schemaJoiRE.MatchString(src) ||
		schemaPydanticRE.MatchString(src) ||
		schemaMarshmallowRE.MatchString(src) ||
		schemaJavaBeanValRE.MatchString(src)
}

func (s *schemaDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, library string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			name, "SCOPE.Component", "schema_validation", language, line,
			map[string]string{"kind": "schema_validation", "library": library}))
	}

	if m := schemaPydanticRE.FindStringIndex(src); m != nil {
		emit("pydantic", "schema_pydantic", "pydantic", lineOf(src, m[0]))
	}
	if m := schemaMarshmallowRE.FindStringIndex(src); m != nil {
		emit("marshmallow", "schema_marshmallow", "marshmallow", lineOf(src, m[0]))
	}
	if m := schemaZodRE.FindStringIndex(src); m != nil {
		emit("zod", "schema_zod", "zod", lineOf(src, m[0]))
	}
	if m := schemaYupRE.FindStringIndex(src); m != nil {
		emit("yup", "schema_yup", "yup", lineOf(src, m[0]))
	}
	if m := schemaJoiRE.FindStringIndex(src); m != nil {
		emit("joi", "schema_joi", "joi", lineOf(src, m[0]))
	}
	if m := schemaJavaBeanValRE.FindStringIndex(src); m != nil {
		emit("java:bean_validation", "schema_bean_validation", "jakarta.validation", lineOf(src, m[0]))
	}

	// Generic fallback
	if len(results) == 0 {
		for _, tok := range schemaImportTokens {
			if strings.Contains(strings.ToLower(src), strings.ToLower(tok)) {
				emit("generic:"+tok, "schema_"+tok, tok, 1)
				break
			}
		}
	}

	return results
}

func init() {
	Register(&schemaDetector{})
}
