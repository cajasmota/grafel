package engine

import (
	"regexp"
	"strings"
)

// iac_code_properties.go — epic #4194 (iac_resource_property_extraction).
//
// Stamp a CURATED, bounded allow-list of high-signal SCALAR resource
// configuration properties from the props/args body of a CODE-FIRST IaC
// resource (AWS CDK constructs, Pulumi resources — TypeScript & Python) onto
// the resource entity's Properties map. This ADDS scalar config alongside the
// existing reference-edge mining (props_ref / output_ref / depends_on) — it
// does NOT replace it.
//
// Code-first IaC declares resource config as object-literal properties (TS:
// `{ memorySize: 512, runtime: ... }`) or keyword arguments (Python:
// `memory_size=512, runtime=...`). We parse these with the same regex/string
// approach the surrounding edge miners use (no full TS/Py AST). To stay robust
// and bounded we ONLY stamp a key when:
//   - the key (lower-cased, after stripping a leading `_`) is in
//     iacCodeCuratedScalarKeys (the allow-list), AND
//   - its value is a *literal scalar*: a quoted string, an integer/float, or a
//     boolean.
//
// We deliberately SKIP and never stamp:
//   - construct/enum/Output references (`lambda.Runtime.NODEJS_20_X`,
//     `Duration.seconds(30)`, `bucket.arn`, `role.arn`) — those are reference
//     edges, mined elsewhere,
//   - object / array / template-interpolation values.
//
// Both TS camelCase (`memorySize`, `instanceType`) and Python snake_case
// (`memory_size`, `instance_type`) spellings are accepted; the stamped key is
// preserved tool-native (as written in source) so a consumer sees exactly what
// the author wrote. Typical stamped count is small (2–5 props on a real
// resource), keeping per-resource property fan-out bounded.

// iacCodeCuratedScalarKeys is the allow-list of high-signal scalar config keys
// we stamp, normalized to lower-case with underscores removed so a single set
// matches both `memorySize`/`memory_size` and `instanceType`/`instance_type`.
// It mirrors the cross-tool curated set established for Terraform/CFN (#4230):
// compute sizing/SKU, memory/timeout, runtime/engine/version, scaling,
// networking, storage.
var iacCodeCuratedScalarKeys = map[string]struct{}{
	// compute sizing / SKU
	"instancetype":  {},
	"machinetype":   {},
	"size":          {},
	"sku":           {},
	"tier":          {},
	"instanceclass": {},
	"nodetype":      {},
	"vmsize":        {},
	// memory / timeout
	"memorysize": {},
	"memory":     {},
	"timeout":    {},
	// runtime / engine / version
	"runtime":       {},
	"engine":        {},
	"engineversion": {},
	"version":       {},
	// serverless function entrypoint
	"handler": {},
	// AWS resource identity / mode flags (#5501: Pulumi-AWS uplift)
	"bucket":                   {}, // S3 bucket name (literal)
	"billingmode":              {}, // DynamoDB PAY_PER_REQUEST / PROVISIONED
	"fifoqueue":                {}, // SQS FIFO flag (bool)
	"streamviewtype":           {}, // DynamoDB stream view (NEW_AND_OLD_IMAGES …)
	"visibilitytimeoutseconds": {}, // SQS visibility timeout
	"readcapacity":             {}, // DynamoDB throughput
	"writecapacity":            {},
	// scaling / count / replicas
	"count":           {},
	"desiredcapacity": {},
	"desiredcount":    {},
	"minsize":         {},
	"maxsize":         {},
	"mincapacity":     {},
	"maxcapacity":     {},
	"replicas":        {},
	// networking
	"port":     {},
	"protocol": {},
	// storage
	"allocatedstorage": {},
	"storagetype":      {},
}

// iacCodeKeyMatches reports whether a source-written key (e.g. "memorySize" or
// "memory_size") is in the curated allow-list, after case/underscore folding.
func iacCodeKeyMatches(rawKey string) bool {
	norm := strings.ToLower(strings.ReplaceAll(rawKey, "_", ""))
	_, ok := iacCodeCuratedScalarKeys[norm]
	return ok
}

// iacCodePropAssignRe matches a single `key: value` (TS object property) or
// `key=value` (Python kwarg) assignment, capturing the key (group 1) and the
// raw value up to the next comma / closing brace / newline (group 2). The value
// is validated/cleaned in iacCodeLiteralScalarValue. Keys allow a leading `_`
// (Python `_lambda`-style aliasing never applies to kwarg keys, but harmless).
var iacCodePropAssignRe = regexp.MustCompile(
	`(?m)(?:^|[\s,{(])([A-Za-z_][\w]*)\s*[:=]\s*([^,}\n\r]+)`,
)

// iacCodeExtractScalarProperties scans a CDK/Pulumi props or args body and
// returns a map of curated scalar key→value. Returns nil when none match (the
// caller should not create an empty map). The first occurrence of each key
// wins. Keys are preserved exactly as written in source (tool-native casing).
func iacCodeExtractScalarProperties(body string) map[string]string {
	if body == "" {
		return nil
	}
	var props map[string]string
	for _, m := range iacCodePropAssignRe.FindAllStringSubmatch(body, -1) {
		key := m[1]
		if !iacCodeKeyMatches(key) {
			continue
		}
		val, ok := iacCodeLiteralScalarValue(m[2])
		if !ok {
			continue
		}
		if props == nil {
			props = map[string]string{}
		}
		if _, exists := props[key]; !exists {
			props[key] = val
		}
	}
	return props
}

// iacCodeLiteralScalarValue validates that raw is a literal scalar (quoted
// string, number, or bool) and returns its cleaned value. Returns ("", false)
// for references (enum/construct/Output accessors like `lambda.Runtime.X`,
// `Duration.seconds(30)`, `bucket.arn`), object/array openers, and
// template/interpolated strings.
func iacCodeLiteralScalarValue(raw string) (string, bool) {
	v := strings.TrimSpace(raw)
	// Trim a trailing semicolon that can leak in on the last TS property.
	v = strings.TrimRight(v, ";")
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	// Object / array openers are collections — never scalars.
	if v[0] == '{' || v[0] == '[' {
		return "", false
	}
	// Quoted string (single, double, or backtick).
	if c := v[0]; c == '"' || c == '\'' || c == '`' {
		// Find the matching closing quote of the same kind.
		end := strings.IndexByte(v[1:], c)
		if end < 0 {
			return "", false
		}
		inner := v[1 : 1+end]
		// A template string containing ${...} interpolation is a reference.
		if c == '`' && strings.Contains(inner, "${") {
			return "", false
		}
		if inner == "" {
			return "", false
		}
		return inner, true
	}
	// Bare value. A reference shows up as an identifier with a `.` (member
	// access: `lambda.Runtime.NODEJS_20_X`, `Duration.seconds`, `bucket.arn`) or
	// a `(` (function call: `Duration.seconds(30)`). Reject those — they are
	// edges, not scalars.
	if strings.ContainsAny(v, ".(") {
		return "", false
	}
	// Booleans (TS true/false, Python True/False) → normalize to lower-case.
	switch v {
	case "true", "True":
		return "true", true
	case "false", "False":
		return "false", true
	}
	// Numbers: a clean integer or float literal (optionally signed). Allows
	// underscores in TS/Py numeric separators (512_000).
	if iacCodeNumberRe.MatchString(v) {
		return strings.ReplaceAll(v, "_", ""), true
	}
	// Anything else (bare identifier, expression fragment) is not a clean scalar.
	return "", false
}

// iacCodeNumberRe matches a clean integer or float numeric literal, optionally
// signed, optionally with `_` digit separators (TS 2021 / Python).
var iacCodeNumberRe = regexp.MustCompile(`^[+-]?[0-9][0-9_]*(?:\.[0-9_]+)?$`)
