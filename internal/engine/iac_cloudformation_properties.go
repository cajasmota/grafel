package engine

import (
	"regexp"
	"strings"
)

// iac_cloudformation_properties.go — epic #4194 (iac_resource_property_extraction).
//
// Stamp a CURATED, bounded allow-list of high-signal SCALAR resource
// configuration properties from a CloudFormation/SAM resource's Properties body
// onto the resource entity's Properties map. This ADDS scalar config alongside
// the existing Ref/GetAtt/Sub/DependsOn edge mining — it does NOT replace it.
//
// CloudFormation templates are YAML or JSON and are parsed here with the same
// regex/string approach the rest of this file uses (no full YAML parser). To
// stay robust and bounded we ONLY stamp a key when:
//   - the key is in cfnCuratedScalarKeys (the allow-list), AND
//   - its value on the same line is a *literal scalar*: a quoted/bare string,
//     an integer/float, or a boolean.
//
// We deliberately SKIP and never stamp:
//   - intrinsic-function values (!Ref, !GetAtt, !Sub, !ImportValue, !FindInMap,
//     and their { "Fn::..." : ... } / { "Ref": ... } long forms) — those are
//     reference edges, mined elsewhere,
//   - block/object/list values (a key whose value continues on following
//     indented lines, or starts with { or [).
//
// Typical stamped count is small (2–5 props on a real resource: e.g. a Lambda
// gets Runtime + MemorySize + Timeout; an EC2 instance gets InstanceType),
// keeping per-resource property fan-out bounded.

// cfnCuratedScalarKeys is the allow-list of high-signal CloudFormation property
// keys we stamp. CloudFormation uses PascalCase property names. Chosen to mirror
// the cross-tool curated set (compute sizing, runtime/engine/version, scaling,
// networking, storage) while staying bounded.
var cfnCuratedScalarKeys = map[string]struct{}{
	// compute sizing / SKU
	"InstanceType":    {},
	"DBInstanceClass": {},
	"CacheNodeType":   {},
	"Size":            {},
	"Tier":            {},
	// memory / timeout (Lambda / SAM)
	"MemorySize": {},
	"Timeout":    {},
	// runtime / engine / version
	"Runtime":       {},
	"Engine":        {},
	"EngineVersion": {},
	// scaling / count / replicas
	"DesiredCapacity": {},
	"MinSize":         {},
	"MaxSize":         {},
	"DesiredCount":    {},
	"MinCapacity":     {},
	"MaxCapacity":     {},
	// networking
	"Port":     {},
	"Protocol": {},
	// storage
	"AllocatedStorage": {},
	"StorageType":      {},
}

// cfnScalarLineRe matches a `Key: value` (YAML) or `"Key": value,` (JSON) line
// capturing the key (group 1) and the raw value text (group 2). The value runs
// to end-of-line; we validate/strip it in cfnLiteralScalarValue.
var cfnScalarLineRe = regexp.MustCompile(`(?m)^[ \t]*["']?([A-Za-z0-9]+)["']?[ \t]*:[ \t]*(.+?)[ \t]*,?[ \t]*$`)

// cfnIntrinsicPrefixes lists value prefixes that indicate an intrinsic function
// (a reference), not a literal scalar. Such values are never stamped.
var cfnIntrinsicPrefixes = []string{
	"!Ref", "!GetAtt", "!Sub", "!ImportValue", "!FindInMap", "!Join",
	"!Select", "!Split", "!Base64", "!Cidr", "!GetAZs", "!If", "!Equals",
	"Fn::", "\"Fn::", "'Fn::",
}

// cfnExtractScalarProperties scans a CloudFormation resource body and returns a
// map of curated scalar property key→value. Returns nil when none match (callers
// should not create an empty map).
func cfnExtractScalarProperties(body string) map[string]string {
	if body == "" {
		return nil
	}
	var props map[string]string
	for _, m := range cfnScalarLineRe.FindAllStringSubmatch(body, -1) {
		key := m[1]
		if _, ok := cfnCuratedScalarKeys[key]; !ok {
			continue
		}
		val, ok := cfnLiteralScalarValue(m[2])
		if !ok {
			continue
		}
		if props == nil {
			props = map[string]string{}
		}
		// First occurrence wins (a resource declares each property once; SAM
		// Globals merge is out of scope for this bounded stamping).
		if _, exists := props[key]; !exists {
			props[key] = val
		}
	}
	return props
}

// cfnLiteralScalarValue validates that raw is a literal scalar (string, number,
// or bool) and returns its cleaned value. Returns ("", false) for intrinsic
// functions, references (a long-form { "Ref": ... } reduces to "{" here), and
// block/collection openers.
func cfnLiteralScalarValue(raw string) (string, bool) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", false // key with no inline value → nested block/object
	}
	// Reject intrinsic-function / reference values.
	for _, p := range cfnIntrinsicPrefixes {
		if strings.HasPrefix(v, p) {
			return "", false
		}
	}
	// Reject collection / object openers and JSON ref objects.
	if v[0] == '{' || v[0] == '[' {
		return "", false
	}
	// Strip a trailing inline comment (YAML).
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	// Strip surrounding quotes (single or double).
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		inner := v[1 : len(v)-1]
		// A quoted value containing ${...} is a Sub-style template — reference.
		if strings.Contains(inner, "${") {
			return "", false
		}
		if inner == "" {
			return "", false
		}
		return inner, true
	}
	// Bare value: must not contain interpolation or whitespace-separated tokens
	// that imply a structure. Allow numbers, booleans, and simple bare strings.
	if strings.Contains(v, "${") {
		return "", false
	}
	// A bare value with internal spaces is unusual for these curated keys and
	// risks capturing fragments — keep it bounded to single-token values.
	if strings.ContainsAny(v, " \t") {
		return "", false
	}
	return v, true
}
