// depth.go — base-extractor depth additions for the Crystal regex extractor
// (#4937, follow-up to #4905). Adds:
//
//   - enum declarations  → SCOPE.Enum value-set node carrying its members
//     (reuses the shared cross-language extractor.EnumEntity builder so Crystal
//     enums converge on the same node model as Python/TS/Java/Go/Ruby/C#).
//   - alias declarations → SCOPE.Component (subtype "alias") carrying the
//     aliased target as a Property plus a REFERENCES edge to the target type.
//   - Type.method receiver resolution → a `receiver_type` Property on every
//     method/macro owned by a class/struct/module, and class-qualified
//     `Type.method` CALLS targets for invocations whose receiver root is a
//     PascalCase constant naming a type.
//   - macro-generated methods → a `macro_generated` marker Property on the
//     macro plus a count of the `def`/`define_method` heads its body emits
//     (visible to downstream consumers that the plain def scanner skips because
//     they live inside macro `{% for %}` / interpolation bodies).
//   - Spectator/spec test linkage → ONE SCOPE.Operation (subtype "test_suite")
//     per `*_spec.cr` whose `describe`/`it`/`context` blocks name a subject
//     type, with a name-affinity TESTS edge to that subject class.
//
// Honest-partial throughout: a construct that cannot be statically named emits
// no node/edge (no fabricated members, no guessed subjects).
package crystal

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// enum  —  `enum Color` … `end`
// ---------------------------------------------------------------------------

var (
	// enumRE matches an `enum Name` (optionally `enum Name : Int32`) header.
	// Group 1: enum type name.
	enumRE = regexp.MustCompile(`(?m)^[ \t]*enum\s+([A-Z][\w:]*)(?:\s*:\s*[\w:]+)?\s*(?:\n|$)`)

	// enumMemberRE matches one member line inside an enum body:
	//   Red
	//   Green = 2
	//   Blue = 0x0000FF
	// Group 1: member name; Group 2 (optional): literal value.
	enumMemberRE = regexp.MustCompile(`(?m)^[ \t]*([A-Z][A-Za-z0-9_]*)\s*(?:=\s*([^\n#]+?))?\s*(?:#.*)?$`)
)

// extractEnums scans src for `enum Name ... end` blocks and returns one
// SCOPE.Enum value-set entity per enum (with its statically-known members).
func extractEnums(src, filePath string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, m := range enumRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		bodyStart := m[1]
		bodyEnd := findEndKeyword(src, bodyStart)
		endLine := strings.Count(src[:bodyEnd], "\n") + 1
		if bodyStart > bodyEnd {
			bodyStart = bodyEnd
		}
		body := src[bodyStart:bodyEnd]

		members := parseEnumMembers(body)
		rec, ok := extractor.EnumEntity(
			name, "crystal", "crystal_enum", filePath,
			startLine, endLine, members,
		)
		if !ok {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// parseEnumMembers walks an enum body and returns its members in declaration
// order. A bare `Red` is a value-less member; `Green = 2` carries the literal.
// Lines that are method defs, macros, comments, or whitespace are skipped so a
// member-method (`def to_s`) inside the enum is not mistaken for a member.
func parseEnumMembers(body string) []extractor.EnumMember {
	var members []extractor.EnumMember
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Stop counting members once a method/macro body begins — Crystal allows
		// `def`/`macro` inside an enum, and anything after is not a member list.
		if strings.HasPrefix(line, "def ") || strings.HasPrefix(line, "macro ") ||
			line == "private" || strings.HasPrefix(line, "private ") {
			break
		}
		mm := enumMemberRE.FindStringSubmatch(raw)
		if mm == nil {
			continue
		}
		val := extractor.StripLiteralQuotes(strings.TrimSpace(mm[2]))
		members = append(members, extractor.EnumMember{
			Name:  mm[1],
			Value: val,
		})
	}
	return members
}

// ---------------------------------------------------------------------------
// alias  —  `alias Name = Type`
// ---------------------------------------------------------------------------

// aliasRE matches `alias Name = Type` (the target may be a union, generic, or
// proc type; we capture the whole RHS up to end-of-line).
// Group 1: alias name; Group 2: aliased target expression.
var aliasRE = regexp.MustCompile(`(?m)^[ \t]*alias\s+([A-Z][\w:]*)\s*=\s*([^\n#]+?)\s*(?:#.*)?$`)

// extractAliases scans src for `alias Name = Type` declarations and returns one
// SCOPE.Component(subtype="alias") per alias, carrying the aliased target as a
// Property and a REFERENCES edge to the (first) named target type.
func extractAliases(src, filePath string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, m := range aliasRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		target := strings.TrimSpace(src[m[4]:m[5]])
		if name == "" || target == "" {
			continue
		}
		startLine := strings.Count(src[:m[0]], "\n") + 1
		rec := types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "alias",
			SourceFile: filePath,
			Language:   "crystal",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "alias " + name + " = " + target,
			Properties: map[string]string{
				"alias_target": target,
			},
		}
		// REFERENCES edge to the primary named type in the target expression
		// (the first PascalCase identifier — e.g. `Array(String)` → Array,
		// `Int32 | String` → Int32). Honest: a primitive-only / builtin target
		// still records the target Property; the edge is added only when a
		// PascalCase type name can be isolated.
		if t := primaryTypeName(target); t != "" {
			rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
				ToID: t,
				Kind: string(types.RelationshipKindReferences),
				Properties: map[string]string{
					"alias": name,
				},
			})
		}
		out = append(out, rec)
	}
	return out
}

// primaryTypeNameRE isolates the first PascalCase type identifier in a type
// expression (`Array(String)` → "Array", `MyApp::User` → "MyApp").
var primaryTypeNameRE = regexp.MustCompile(`^[ \t]*([A-Z][A-Za-z0-9_]*)`)

func primaryTypeName(expr string) string {
	if mm := primaryTypeNameRE.FindStringSubmatch(strings.TrimSpace(expr)); mm != nil {
		return mm[1]
	}
	return ""
}

// ---------------------------------------------------------------------------
// Type.method receiver resolution
// ---------------------------------------------------------------------------

// receiverTypeForMethod returns the type name a method/macro should be
// attributed to: its innermost enclosing class/struct/module scope name, or ""
// for a top-level def. The value is stamped as the method's `receiver_type`
// Property so a downstream consumer can resolve a bare `def foo` to
// `Owner.foo` without re-parsing.
func receiverTypeForMethod(scopes []scopeSpan, methodStart, methodEnd int) string {
	if s := enclosingScope(scopes, methodStart, methodEnd); s != nil {
		return s.name
	}
	return ""
}

// knownTypeNames returns the set of class/struct/module names declared in this
// file, so a `Type.method(` call can be class-qualified only when `Type` names
// a real in-file type (avoiding qualifying a module-function or a local var).
func knownTypeNames(scopes []scopeSpan) map[string]bool {
	m := make(map[string]bool, len(scopes))
	for _, s := range scopes {
		m[s.name] = true
	}
	return m
}

// ---------------------------------------------------------------------------
// macro-generated methods
// ---------------------------------------------------------------------------

var (
	// macroDefRE matches a `def`/`define_method` head INSIDE a macro body —
	// including interpolated names (`def {{name}}`) the plain defRE skips
	// because they are not statically named at the source level.
	macroDefRE = regexp.MustCompile(`(?m)^[ \t]*(?:def|define_method)\s`)
	// macroForRE matches a `{% for %}` macro iteration that drives code-gen.
	macroForRE = regexp.MustCompile(`\{%\s*for\b`)
)

// macroGenStats inspects a macro body and reports how many method-generating
// heads it contains and whether it iterates (`{% for %}`). Used to stamp a
// macro entity with `macro_generated`/`generated_method_count` Properties so
// the regex scanner's blind spot (defs synthesised inside macro bodies) is at
// least counted and flagged.
func macroGenStats(body string) (count int, iterates bool) {
	count = len(macroDefRE.FindAllStringIndex(body, -1))
	iterates = macroForRE.MatchString(body)
	return count, iterates
}

// ---------------------------------------------------------------------------
// Spectator / spec test linkage  (named-symbol TESTS edge)
// ---------------------------------------------------------------------------

var (
	// specBlockRE matches a `describe`/`context`/`it`/`pending` block head whose
	// first argument is either a constant subject (`describe MyApp::User`) or a
	// string label (`describe "GET /users"`).
	// Group 1: block keyword; Group 2 (constant) OR Group 3 (string label).
	specBlockRE = regexp.MustCompile(
		`(?m)^[ \t]*(describe|context|it|pending)\s+(?:([A-Z][\w:]*)|"([^"]*)")`,
	)
)

// isCrystalSpecFile reports whether path is a Crystal spec file: `*_spec.cr` or
// any `.cr` under a `/spec/` directory (mirrors the coverage classifier).
func isCrystalSpecFile(path string) bool {
	slashed := "/" + filepath.ToSlash(strings.ToLower(path))
	if strings.Contains(slashed, "/spec/") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(filepath.Base(path)), "_spec.cr")
}

// extractSpecSuite emits one SCOPE.Operation(subtype="test_suite") per Crystal
// spec file whose describe/it/context blocks name a subject type, with a
// name-affinity TESTS edge to that subject class. Spectator and the built-in
// `spec` DSL share the describe/it/context surface, so one matcher covers both.
//
// Subject resolution priority (honest — no edge when nothing resolves):
//  1. the constant of the OUTERMOST `describe Const` block;
//  2. the spec-file-stem → class convention (`user_spec.cr` → `User`).
//
// No-op for non-spec files and for specs with neither a constant describe nor a
// stem-derivable subject.
func extractSpecSuite(src, filePath string) []types.EntityRecord {
	if !isCrystalSpecFile(filePath) {
		return nil
	}
	matches := specBlockRE.FindAllStringSubmatch(src, -1)
	if len(matches) == 0 {
		return nil
	}
	exampleCount := 0
	subject := ""
	for _, mm := range matches {
		kw := mm[1]
		if kw == "it" || kw == "pending" {
			exampleCount++
		}
		// First constant-subject describe/context wins.
		if subject == "" && (kw == "describe" || kw == "context") && mm[2] != "" {
			subject = crystalConstBaseName(mm[2])
		}
	}
	if exampleCount == 0 {
		return nil // a spec with no examples exercises nothing
	}
	if subject == "" {
		subject = crystalSpecStemToType(filePath)
	}

	base := strings.TrimSuffix(filepath.Base(filepath.ToSlash(filePath)), ".cr")
	rec := types.EntityRecord{
		Name:       "spec_suite:" + base,
		Kind:       "SCOPE.Operation",
		Subtype:    "test_suite",
		SourceFile: filePath,
		Language:   "crystal",
		StartLine:  1,
		EndLine:    1,
		Properties: map[string]string{
			"framework":      "spectator",
			"test_framework": "spectator",
			"provenance":     "INFERRED_FROM_CRYSTAL_SPEC_SUITE",
			"example_count":  strconv.Itoa(exampleCount),
		},
	}
	if subject != "" {
		rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
			ToID: "Class:" + subject,
			Kind: string(types.RelationshipKindTests),
			Properties: map[string]string{
				"framework":    "spectator",
				"match_source": "spec_subject_affinity",
				"target_type":  subject,
			},
			Confidence: 0.9,
		})
	}
	return []types.EntityRecord{rec}
}

// crystalConstBaseName returns the final segment of a `::`-qualified Crystal
// constant (`MyApp::User` → "User") so the `Class:<Subject>` stub matches the
// resolver's bare-name index.
func crystalConstBaseName(c string) string {
	if i := strings.LastIndex(c, "::"); i >= 0 {
		return c[i+2:]
	}
	return c
}

// crystalSpecStemToType camelizes a `*_spec.cr` stem into the conventional type
// under test (`user_spec.cr` → "User", `order_service_spec.cr` →
// "OrderService"). Returns "" when the stem has no `_spec` suffix.
func crystalSpecStemToType(path string) string {
	base := strings.TrimSuffix(filepath.Base(filepath.ToSlash(path)), ".cr")
	stem := strings.TrimSuffix(base, "_spec")
	if stem == base || stem == "" {
		return ""
	}
	var b strings.Builder
	for _, part := range strings.Split(stem, "_") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	return b.String()
}
