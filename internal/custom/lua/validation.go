// validation.go — Lua validation extractor (dto_extraction, request_validation).
//
// Covers validation detection for Lua web frameworks:
//
//	OpenResty:
//	  - ngx.req.get_post_args() / ngx.req.get_uri_args() — request parameter extraction
//	  - ngx.req.read_body() + ngx.req.get_body_data() — request body reading
//	  - JSON decoding: cjson.decode / cjson.new().decode / json.decode
//	  - Schema validation via lua-resty-jsonschema / lua-schema libraries
//	  - Custom validation patterns: if not arg_x then ngx.exit(400) end
//
//	Lapis:
//	  - lapis.util.check_params / validate_params (Lapis built-in validation)
//	  - capture_errors / yield_error (Lapis error capture pattern)
//	  - assert_error / assert_valid (Lapis assertion helpers)
//	  - lapis.validate library require
//
// All cells are partial: regex heuristics without data-flow analysis.
package lua

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("lua_validation", &luaValidationExtractor{})
}

// luaValidationExtractor detects validation patterns in Lua source files.
type luaValidationExtractor struct{}

func (e *luaValidationExtractor) Language() string { return "lua_validation" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// ngx.req.get_post_args() / ngx.req.get_uri_args()
	reNgxGetArgs = regexp.MustCompile(
		`(?m)\bngx\.req\.get_(?:post|uri)_args\s*\(\s*\)`)

	// ngx.req.read_body() — body ingestion
	reNgxReadBody = regexp.MustCompile(
		`(?m)\bngx\.req\.read_body\s*\(\s*\)`)

	// JSON decode: cjson.decode / json.decode / cjson.new().decode
	reLuaJSONDecode = regexp.MustCompile(
		`(?m)\b(?:cjson|json)\s*(?:\.new\s*\(\s*\)\s*\.)?\.\s*decode\s*\(`)

	// Validation exit: ngx.exit(400) / ngx.exit(ngx.HTTP_BAD_REQUEST)
	reNgxExitValidation = regexp.MustCompile(
		`(?m)\bngx\.exit\s*\(\s*(?:400|ngx\.HTTP_BAD_REQUEST|ngx\.HTTP_UNPROCESSABLE_ENTITY|422|401|ngx\.HTTP_UNAUTHORIZED)\s*\)`)

	// lua-resty-jsonschema: require("resty.jsonschema")
	reLuaJSONSchema = regexp.MustCompile(
		`(?m)\brequire\s*[("']resty\.jsonschema["']?\)?`)

	// Lapis check_params / validate_params
	reLapisCheckParams = regexp.MustCompile(
		`(?m)\b(?:check_params|validate_params)\s*\(`)

	// Lapis capture_errors / yield_error
	reLapisCaptureErrors = regexp.MustCompile(
		`(?m)\b(?:capture_errors|yield_error|assert_error|assert_valid)\s*[\({]`)

	// Lapis validate library: require("lapis.validate")
	reLapisValidate = regexp.MustCompile(
		`(?m)\brequire\s*[("']lapis\.validate["']?\)?`)

	// Lapis params shape: params.field_name access (DTO-like extraction)
	reLapisParams = regexp.MustCompile(
		`(?m)\bparams\s*\.\s*([a-z_]\w*)`)

	// Lapis assert_valid / validate.validate field tuples. Captures the call so
	// the per-field+rule scanner can walk the tuple table.
	//   assert_valid(self.params, { {"name", exists=true}, {"email", ...} })
	//   validate.assert_valid(self.params, { … })
	//   validate.validate(self.params, { … })
	reLapisAssertValidCall = regexp.MustCompile(
		`(?m)\b(?:validate\s*\.\s*)?(?:assert_valid|validate)\s*\(`)

	// A single field-spec tuple inside an assert_valid table:
	//   { "name", exists = true, min_length = 3 }
	//   { "email", matches_pattern = "..." }
	// Group 1 = field name; group 2 = the remainder of the tuple (the rule list).
	reLapisFieldTuple = regexp.MustCompile(
		`\{\s*["']([a-zA-Z_]\w*)["']\s*,\s*([^}]*)\}`)

	// A single validation rule key inside a field tuple: `exists = true`,
	// `min_length = 3`, `matches_pattern = "..."`. Group 1 = rule name.
	reLapisRuleKey = regexp.MustCompile(
		`([a-zA-Z_]\w*)\s*=`)

	// Lapis CSRF: lapis.csrf require, csrf.validate_token / @csrf annotations and
	// the generate_token / assert_token helpers.
	reLapisCSRF = regexp.MustCompile(
		`(?m)\brequire\s*[("']lapis\.csrf["']?\)?|\bcsrf\s*\.\s*(?:validate_token|assert_token|generate_token)\s*\(|@csrf\b`)
)

// braceArg returns the first balanced `{ … }` table literal at or after startAt
// in src, including the braces. It is quote-aware (single and double quotes with
// backslash escapes) so a `}` inside a string literal does not close the table
// prematurely. Returns "" when no balanced table is found.
func braceArg(src string, startAt int) string {
	n := len(src)
	i := startAt
	for i < n && src[i] != '{' {
		// Stop if we hit a statement terminator before any table — the call has
		// no table argument (e.g. assert_valid(self.params)).
		if src[i] == ')' || src[i] == ';' || src[i] == '\n' {
			// Allow newlines/whitespace before the table; only bail on ')'/';'.
			if src[i] == ')' || src[i] == ';' {
				return ""
			}
		}
		i++
	}
	if i >= n {
		return ""
	}
	depth := 0
	start := i
	for i < n {
		c := src[i]
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : i+1]
			}
		case '"', '\'':
			q := c
			i++
			for i < n && src[i] != q {
				if src[i] == '\\' {
					i += 2
					continue
				}
				i++
			}
		}
		i++
	}
	return ""
}

// Extract implements extractor.Extractor.
func (e *luaValidationExtractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)

	hasValidation := strings.Contains(src, "get_post_args") || strings.Contains(src, "get_uri_args") ||
		strings.Contains(src, "read_body") || strings.Contains(src, "cjson") ||
		strings.Contains(src, "json.decode") || strings.Contains(src, "ngx.exit") ||
		strings.Contains(src, "jsonschema") || strings.Contains(src, "check_params") ||
		strings.Contains(src, "capture_errors") || strings.Contains(src, "lapis.validate") ||
		strings.Contains(src, "assert_valid") || strings.Contains(src, "yield_error") ||
		strings.Contains(src, "csrf") || strings.Contains(src, "@csrf")
	if !hasValidation {
		return nil, nil
	}

	var out []types.EntityRecord

	// OpenResty: get_post/uri_args
	for _, idx := range reNgxGetArgs.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		arg := src[idx[0]:idx[1]]
		entity := makeEntity("ngx_get_args:"+arg, string(types.EntityKindPattern), "request_input", file.Path, "lua", ln)
		setProps(&entity, "signal", "validation", "framework", "openresty", "kind", "get_args")
		out = append(out, entity)
	}

	// OpenResty: read_body
	for _, idx := range reNgxReadBody.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("ngx_read_body", string(types.EntityKindPattern), "request_input", file.Path, "lua", ln)
		setProps(&entity, "signal", "validation", "framework", "openresty", "kind", "read_body")
		out = append(out, entity)
	}

	// JSON decode (DTO extraction)
	for _, idx := range reLuaJSONDecode.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("json_decode_dto", string(types.EntityKindPattern), "dto_extraction", file.Path, "lua", ln)
		setProps(&entity, "signal", "validation", "library", "cjson", "kind", "json_decode")
		out = append(out, entity)
	}

	// OpenResty: ngx.exit(400) validation guards
	for _, idx := range reNgxExitValidation.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("ngx_exit_validation", string(types.EntityKindPattern), "request_validation", file.Path, "lua", ln)
		setProps(&entity, "signal", "validation", "framework", "openresty", "kind", "error_exit")
		out = append(out, entity)
	}

	// JSON schema validation
	if reLuaJSONSchema.MatchString(src) {
		idx := reLuaJSONSchema.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("resty_jsonschema_import", string(types.EntityKindPattern), "request_validation", file.Path, "lua", ln)
		setProps(&entity, "signal", "validation", "library", "resty.jsonschema", "kind", "schema_validation")
		out = append(out, entity)
	}

	// Lapis: check_params / validate_params
	for _, idx := range reLapisCheckParams.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("lapis_check_params", string(types.EntityKindPattern), "request_validation", file.Path, "lua", ln)
		setProps(&entity, "signal", "validation", "framework", "lapis", "kind", "check_params")
		out = append(out, entity)
	}

	// Lapis: capture_errors / assert_valid
	for _, idx := range reLapisCaptureErrors.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("lapis_capture_errors", string(types.EntityKindPattern), "request_validation", file.Path, "lua", ln)
		setProps(&entity, "signal", "validation", "framework", "lapis", "kind", "capture_errors")
		out = append(out, entity)
	}

	// Lapis: assert_valid / validate field+rule tuples. For each
	// assert_valid(self.params, { {"field", rule=...}, ... }) call we walk the
	// table argument and emit one request_validation entity per (field, rule)
	// pair, capturing the SPECIFIC field name and validation rule (exists,
	// min_length, matches_pattern, …) rather than a single opaque "capture_errors"
	// signal. This is the value-asserting upgrade for request_validation.
	for _, call := range reLapisAssertValidCall.FindAllStringIndex(src, -1) {
		// The table argument follows the open paren — extract the balanced
		// brace-delimited table that holds the field tuples.
		table := braceArg(src, call[1])
		if table == "" {
			continue
		}
		tableOffset := strings.Index(src[call[1]:], table)
		if tableOffset < 0 {
			tableOffset = 0
		}
		base := call[1] + tableOffset
		for _, ft := range reLapisFieldTuple.FindAllStringSubmatchIndex(table, -1) {
			field := table[ft[2]:ft[3]]
			rules := table[ft[4]:ft[5]]
			ln := lineOf(src, base+ft[0])
			seenRule := map[string]bool{}
			emittedRule := false
			for _, rk := range reLapisRuleKey.FindAllStringSubmatch(rules, -1) {
				rule := rk[1]
				if seenRule[rule] {
					continue
				}
				seenRule[rule] = true
				emittedRule = true
				entity := makeEntity("assert_valid:"+field+"."+rule,
					string(types.EntityKindPattern), "request_validation", file.Path, "lua", ln)
				setProps(&entity, "signal", "validation", "framework", "lapis",
					"kind", "field_rule", "field", field, "rule", rule)
				out = append(out, entity)
			}
			if !emittedRule {
				// Field tuple with no recognised rule key (e.g. just { "name" }):
				// still record the field as a validated DTO field.
				ln := lineOf(src, base+ft[0])
				entity := makeEntity("assert_valid:"+field,
					string(types.EntityKindPattern), "dto_field", file.Path, "lua", ln)
				setProps(&entity, "signal", "validation", "framework", "lapis",
					"kind", "field_rule", "field", field)
				out = append(out, entity)
			}
		}
	}

	// Lapis: CSRF token validation (lapis.csrf, csrf.validate_token, @csrf).
	if reLapisCSRF.MatchString(src) {
		idx := reLapisCSRF.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("lapis_csrf", string(types.EntityKindPattern), "request_validation", file.Path, "lua", ln)
		setProps(&entity, "signal", "validation", "framework", "lapis", "kind", "csrf_token")
		out = append(out, entity)
	}

	// Lapis: validate library
	if reLapisValidate.MatchString(src) {
		idx := reLapisValidate.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("lapis_validate_import", string(types.EntityKindPattern), "request_validation", file.Path, "lua", ln)
		setProps(&entity, "signal", "validation", "framework", "lapis", "kind", "validate_lib")
		out = append(out, entity)
	}

	// Lapis: params.field DTO fields
	seenFields := map[string]bool{}
	for _, idx := range reLapisParams.FindAllStringSubmatchIndex(src, -1) {
		field := src[idx[2]:idx[3]]
		if seenFields[field] {
			continue
		}
		seenFields[field] = true
		ln := lineOf(src, idx[0])
		entity := makeEntity("params."+field, string(types.EntityKindPattern), "dto_field", file.Path, "lua", ln)
		setProps(&entity, "signal", "validation", "framework", "lapis", "kind", "dto_field", "field", field)
		out = append(out, entity)
	}

	return out, nil
}
