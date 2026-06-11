// Field-level partial-stub facet (#4669, epic #4493 / #4419 capability F).
//
// The endpoint-level stub_detector (internal/stubdetector + the MCP tool)
// classifies a handler as a stub by its SIDE-EFFECT PROFILE: an endpoint with
// SOME db_read reads as "implemented". That misses PARTIAL stubs — a handler
// that genuinely reads data but HARDCODES specific response fields:
//
//	GET /clients/get_extras → reads the client row, but returns the cat1/cat5
//	  scheduling fields as constants (#763);
//	checklists → returns part_id: null (#831).
//
// Both have a real db_read, so the any-effect contrast says "implemented" while
// individual response FIELDS are stubbed. This file adds the complementary,
// field-level signal: for a handler's return/response construction, classify
// each field of the constructed object/dict as DERIVED (its value flows from a
// read variable, a call result, request input, or a model attribute) vs
// LITERAL-BOUND (a constant/string/number/bool/null hardcode). The
// literal-bound DATA fields are the partial-stub fields.
//
// Design mirrors the branches facet (branches.go): a per-language analyzer
// registered in a registry, fed the same single-function source window the
// effect sniffers / branch analyzer walk. Python (DRF Response dicts /
// serializer-style dict literals) and JS/TS (NestJS response object literals)
// ship first as the flagship stacks; other languages register their own
// classifier as they land (FieldLiteralAnalyzerFor → honest-partial when none).
//
// Honesty/precision rules (under-flag rather than over-flag):
//   - A field is flagged ONLY when it is UNCONDITIONALLY literal: in a handler
//     with multiple return objects, a field that is literal in one and derived
//     in another is NOT flagged (it is conditionally derived). A field is
//     flagged only when EVERY constructed object that contains it binds it to a
//     literal.
//   - Envelope flags (`success: true`, `ok: true`, `error: null`, `status:
//     "ok"`, `message: "..."`, …) are recognised as transport envelope, not
//     DATA, and excluded from the partial-stub set by default — they are
//     legitimately constant. They are still recorded (with envelope=true) so a
//     caller can see them, but the partial-stub roll-up excludes them.
package substrate

import (
	"regexp"
	"sort"
	"strings"
)

// FieldBinding classifies how a single response-object field's value is
// produced.
type FieldBinding string

const (
	// BindingLiteral — the field is bound to a literal/constant: a quoted
	// string, a numeric literal, a bool (true/false/True/False), or
	// null/None/nil. The partial-stub signal.
	BindingLiteral FieldBinding = "literal"
	// BindingDerived — the field's value flows from a variable, a call result,
	// an attribute/member access, request input, or any non-constant
	// expression. NOT a stub.
	BindingDerived FieldBinding = "derived"
)

// FieldFacet is one field of a constructed response object. It is the unit the
// field-literals facet returns. JSON shape is the public contract consumed by
// the stub_detector tool.
type FieldFacet struct {
	// Field is the response key name (dict key / object property name).
	Field string `json:"field"`
	// Binding is literal vs derived.
	Binding FieldBinding `json:"binding"`
	// LiteralValue is the verbatim literal text when Binding==literal
	// (e.g. `null`, `0`, `5`, `"tbd"`, `true`). Empty for derived fields.
	LiteralValue string `json:"literal_value,omitempty"`
	// Envelope is true when the field is recognised as a transport-envelope
	// flag (success/ok/error/status/message/…) rather than a DATA field — so a
	// constant binding is legitimate and the field is excluded from the
	// partial-stub roll-up. Heuristic; documented at envelopeFieldNames.
	Envelope bool `json:"envelope,omitempty"`
	// Line is the 1-indexed source line of the field assignment, absolute to
	// the file the function lives in (so callers can cross-reference
	// get_source output).
	Line int `json:"line"`
}

// FieldLiteralAnalyzerFn is the per-language contract: given a single
// function's source window and the absolute 1-indexed line it starts at, return
// every response-object field with its binding classification. Stateless and
// pure — identical input yields identical output (deterministic MCP output).
type FieldLiteralAnalyzerFn func(funcSource string, startLine int) []FieldFacet

var fieldLiteralRegistry = map[string]FieldLiteralAnalyzerFn{}

// RegisterFieldLiteralAnalyzer installs a per-language field-literal analyzer.
// Mirrors RegisterBranchAnalyzer so a language can ship field classification
// independently.
func RegisterFieldLiteralAnalyzer(lang string, fn FieldLiteralAnalyzerFn) {
	if lang == "" || fn == nil {
		return
	}
	fieldLiteralRegistry[lang] = fn
}

// FieldLiteralAnalyzerFor returns the per-language analyzer, or nil when none
// is registered (honest-partial: absence of a classifier is reported as "not
// yet supported", never as "no literal fields").
func FieldLiteralAnalyzerFor(lang string) FieldLiteralAnalyzerFn {
	return fieldLiteralRegistry[lang]
}

// FieldLiteralLanguages returns the slugs of every registered analyzer, sorted.
func FieldLiteralLanguages() []string {
	out := make([]string, 0, len(fieldLiteralRegistry))
	for k := range fieldLiteralRegistry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// PartialStubFields is the public roll-up: from a flat list of per-object field
// facets (possibly spanning several constructed objects in one handler), return
// the DATA fields that are UNCONDITIONALLY literal-bound — the partial-stub
// fields. A field is included only when EVERY occurrence of it across the
// handler's constructed objects is literal-bound (a field derived anywhere is
// excluded) AND it is not an envelope flag. Deterministic (sorted by field).
//
// This is the language-general honesty gate; each language analyzer only has to
// emit per-object facets, and this composes them safely.
func PartialStubFields(facets []FieldFacet) []FieldFacet {
	type agg struct {
		facet      FieldFacet
		allLiteral bool
		seen       bool
	}
	byField := map[string]*agg{}
	for _, f := range facets {
		a := byField[f.Field]
		if a == nil {
			a = &agg{allLiteral: true}
			byField[f.Field] = a
		}
		if !a.seen {
			a.facet = f
			a.seen = true
		}
		if f.Binding != BindingLiteral {
			a.allLiteral = false
		}
	}
	out := make([]FieldFacet, 0, len(byField))
	for _, a := range byField {
		if !a.allLiteral || a.facet.Envelope {
			continue
		}
		out = append(out, a.facet)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

// envelopeFieldNames is the heuristic set of transport-envelope flag keys that
// are legitimately constant and so excluded from the partial-stub roll-up.
// Kept deliberately small + high-precision: only universally-recognised
// envelope keys, never DATA-looking names. A field named here with a literal
// binding is recorded (Envelope=true) but not flagged. When a name could be
// either envelope OR data we err toward DATA (NOT in this set) so we under-flag
// envelopes rather than over-suppress real stubs — except where the value shape
// confirms envelope (see isEnvelopeField).
var envelopeFieldNames = map[string]bool{
	"success": true,
	"ok":      true,
	"status":  true,
	"error":   true,
	"errors":  true,
	"message": true,
	"msg":     true,
	"code":    true,
	"detail":  true,
}

// isEnvelopeField reports whether (name, literalValue) names a transport
// envelope flag rather than a DATA field. The name must be a known envelope key
// AND the literal must be a "flag-shaped" value (bool, a short status string, a
// null error) — a numeric DATA field that merely happens to be named "code"
// with value 5 is NOT treated as envelope. This keeps precision high: we only
// suppress when both the name and the value shape say envelope.
func isEnvelopeField(name, literalValue string) bool {
	if !envelopeFieldNames[strings.ToLower(name)] {
		return false
	}
	v := strings.TrimSpace(literalValue)
	lv := strings.ToLower(v)
	switch lv {
	case "true", "false", "null", "none", "nil", "":
		return true
	}
	// Quoted string status/message (e.g. "ok", "success") — envelope.
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
		return true
	}
	return false
}

// --- shared literal classification --------------------------------------

// literalRHSRe matches a right-hand-side expression that is a bare literal:
// a number, bool, null/None/nil, or a single quoted string with nothing else.
// Anchored and whitespace-tolerant. Used by all language analyzers so the
// literal-vs-derived decision is uniform.
var literalRHSRe = regexp.MustCompile(
	`^\s*(` +
		`-?\d+(?:\.\d+)?` + // number
		`|true|false|True|False` + // bool
		`|null|None|nil|undefined` + // null-ish
		`|"(?:[^"\\]|\\.)*"` + // double-quoted string
		`|'(?:[^'\\]|\\.)*'` + // single-quoted string
		`|` + "`" + `(?:[^` + "`" + `\\$]|\\.)*` + "`" + // template literal w/o interpolation
		`)\s*$`,
)

// classifyFieldRHS classifies a field's right-hand-side expression text as a
// literal (returning its trimmed verbatim value) or derived. A value is literal
// ONLY when the WHOLE RHS is a single bare literal — any variable, call,
// attribute access, operator, or interpolation makes it derived. This is the
// conservative core: when in doubt, derived (no flag).
func classifyFieldRHS(rhs string) (FieldBinding, string) {
	t := strings.TrimSpace(rhs)
	// Strip a trailing comma the field-extraction left on.
	t = strings.TrimRight(t, ", \t")
	// Template literal containing ${...} interpolation is derived.
	if strings.HasPrefix(t, "`") && strings.Contains(t, "${") {
		return BindingDerived, ""
	}
	if literalRHSRe.MatchString(t) {
		return BindingLiteral, t
	}
	return BindingDerived, ""
}
