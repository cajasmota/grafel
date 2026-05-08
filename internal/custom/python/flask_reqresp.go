package python

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python_flask_reqresp", &FlaskReqRespExtractor{})
}

// FlaskReqRespExtractor extracts ACCEPTS_INPUT (marshmallow schema.load())
// and RETURNS (return type annotations) patterns from Flask route handlers.
type FlaskReqRespExtractor struct{}

func (e *FlaskReqRespExtractor) Language() string { return "python_flask_reqresp" }

var (
	flrrRouteDecorRe = regexp.MustCompile(
		`(?m)@(\w+)\.route\s*\(\s*(?:r)?["'][^"']*["'][^)]*\)` +
			`(?:\s*\n(?:\s*@\w+(?:\([^)]*\))?\s*\n)*)\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	flrrHTTPVerbDecorRe = regexp.MustCompile(
		`(?m)@(\w+)\.(?:get|post|put|patch|delete|options|head)\s*\(\s*(?:r)?["'][^"']*["'][^)]*\)` +
			`(?:\s*\n(?:\s*@\w+(?:\([^)]*\))?\s*\n)*)\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	flrrReturnAnnotRe = regexp.MustCompile(`\)\s*->\s*([\w\[\], |.]+?)\s*:`)
	flrrSchemaLoadRe  = regexp.MustCompile(`(\w+)(?:\(\))?\s*\.\s*load\s*\(`)
)

var flrrSkipTypes = map[string]bool{
	"int": true, "str": true, "float": true, "bool": true, "bytes": true,
	"None": true, "dict": true, "list": true, "set": true, "tuple": true,
	"Any": true, "object": true, "Optional": true, "List": true, "Dict": true,
	"Response": true, "JSONResponse": true, "Request": true, "request": true,
	"Schema": true, "datetime": true, "date": true, "UUID": true,
}

var flrrGenericSchemaNames = map[string]bool{
	"schema": true, "self": true, "cls": true, "request": true, "response": true,
	"data": true, "payload": true, "body": true, "json": true, "form": true,
	"args": true, "kwargs": true, "result": true, "output": true, "input": true,
	"obj": true, "instance": true, "db": true, "g": true, "session": true,
}

var flrrSchemaSuffixRe = regexp.MustCompile(`(?i)_schema$`)

func (e *FlaskReqRespExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_flask_reqresp")
	_, span := tracer.Start(ctx, "custom.python_flask_reqresp")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	var out []types.EntityRecord
	seenSchemas := make(map[string]bool)

	emitSchema := func(schemaName string, line int, kind string) {
		if seenSchemas[schemaName] {
			return
		}
		seenSchemas[schemaName] = true
		out = append(out, entity(schemaName, "SCOPE.Schema", "", file.Path, line,
			map[string]string{"framework": "flask", "pattern_type": "marshmallow_schema", "kind": kind}))
	}

	// Collect route handlers sorted by position
	type handler struct {
		funcName     string
		openParenEnd int
		matchStart   int
	}
	var handlers []handler

	for _, idx := range allMatchesIndex(flrrRouteDecorRe, source) {
		funcName := source[idx[4]:idx[5]]
		handlers = append(handlers, handler{funcName, idx[1], idx[0]})
	}
	for _, idx := range allMatchesIndex(flrrHTTPVerbDecorRe, source) {
		funcName := source[idx[4]:idx[5]]
		handlers = append(handlers, handler{funcName, idx[1], idx[0]})
	}

	for i, h := range handlers {
		line := lineOf(source, h.matchStart)

		// Extract params block and close paren offset
		_, closeOffset := extractParamsBlock(source, h.openParenEnd)

		// RETURNS: return type annotation
		afterClose := source[closeOffset:min(closeOffset+120, len(source))]
		if ret := flrrReturnAnnotRe.FindStringSubmatch(afterClose); ret != nil {
			dtoName := unwrapType(strings.TrimSpace(ret[1]))
			if dtoName != "" && !flrrSkipTypes[dtoName] {
				emitSchema(dtoName, line, "response")
				out = append(out, entity(h.funcName+":returns:"+dtoName, "SCOPE.Operation", "endpoint", file.Path, line,
					map[string]string{"framework": "flask", "pattern_type": "returns", "schema_type": dtoName, "match_source": "return_type_annotation"}))
			}
		}

		// ACCEPTS_INPUT: marshmallow schema.load() in handler body
		bodyLimit := len(source)
		if i+1 < len(handlers) {
			bodyLimit = handlers[i+1].matchStart
		}
		colonPos := strings.Index(source[closeOffset:min(bodyLimit, len(source))], ":")
		if colonPos == -1 {
			continue
		}
		bodyStart := closeOffset + colonPos + 1
		bodyEnd := min(bodyStart+2000, bodyLimit)
		bodyRegion := source[bodyStart:bodyEnd]

		for _, sm := range flrrSchemaLoadRe.FindAllStringSubmatch(bodyRegion, -1) {
			schemaVar := sm[1]
			if flrrGenericSchemaNames[strings.ToLower(schemaVar)] {
				continue
			}
			if flrrSkipTypes[schemaVar] {
				continue
			}
			canonical := canonicalSchemaName(schemaVar)
			if canonical == "" {
				continue
			}
			emitSchema(canonical, line, "schema")
			out = append(out, entity(h.funcName+":accepts:"+canonical, "SCOPE.Operation", "endpoint", file.Path, line,
				map[string]string{"framework": "flask", "pattern_type": "accepts_input", "schema_var": schemaVar, "schema_type": canonical}))
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

func canonicalSchemaName(varName string) string {
	if varName == "" || flrrGenericSchemaNames[strings.ToLower(varName)] {
		return ""
	}
	// Already PascalCase
	if varName[0] >= 'A' && varName[0] <= 'Z' {
		return varName
	}
	// snake_case: strip _schema suffix, PascalCase, re-append Schema
	stripped := flrrSchemaSuffixRe.ReplaceAllString(varName, "")
	if stripped == "" {
		return ""
	}
	parts := strings.Split(stripped, "_")
	var pascal strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		pascal.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	if pascal.Len() == 0 {
		return ""
	}
	if flrrSchemaSuffixRe.MatchString(varName) {
		return pascal.String() + "Schema"
	}
	return pascal.String()
}
