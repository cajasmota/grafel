package extractor

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// enum_valueset.go — shared cross-language helper for enum / value-set
// extraction (epic #3628, data-model area). It mirrors the synthetic-node
// model of exception_flow.go but keeps enums file-scoped (a Status enum in
// orders.py and a Status enum in users.py are DIFFERENT enums, so their
// SourceFile differs and ComputeID does not collapse them).
//
// The capability answers the enum-parity question a rewrite needs:
//
//   - "what values can field X take?" — a SCOPE.Enum value-set node carries
//     the full member list AND each member's literal value, so a Django
//     Python `class Status(IntEnum): ACTIVE = 1; ARCHIVED = 2` is reproduced
//     value-for-value by a NestJS `enum Status { ACTIVE = 1, ARCHIVED = 2 }`.
//   - "which fields are constrained to enum X?" — inbound REFERENCES edges
//     carrying the property ref_kind="field_target_type", emitted from a field
//     record to this node's QualifiedName. C# is the only language that emits
//     them (internal/extractors/csharp/field_type_refs.go, #6984); in every
//     other language a SCOPE.Enum node still has no inbound edge. No TYPED_AS
//     edge exists: that kind was declared, never produced, and deleted by
//     #6906 — see the RETIRED KINDS note in internal/types/kinds.go.
//
// Each language extractor detects the enum shape (Python Enum subclass, TS
// `enum` / string-literal union, Java enum, Go iota const block, Ruby
// ActiveRecord enum, C# enum) and hands the parsed (name, members[], kindHint)
// to EnumEntity here. This file owns ONLY node construction so the value-set
// shape is identical everywhere.
//
// Precision-first / honest-partial: members whose literal value is not
// statically known (computed expressions, `auto()`, bare Go iota positions
// the caller chose not to materialise) are emitted as value-less members —
// the member name is still recorded, but it contributes no "Name=Literal"
// pair to the `values` property. A caller that cannot statically name the
// enum (dynamic class, non-literal TS union) emits NO node.

// EnumMember is one declared member of an enumerated type: its name plus an
// optional statically-known literal value. Value is "" when the source did not
// assign an explicit literal (e.g. a bare Java constant `ACTIVE`, a Go iota
// position the extractor did not resolve, or a Python `auto()` member).
type EnumMember struct {
	Name  string
	Value string // statically-known literal ("" when unknown / auto)
	// Line is the 1-indexed source line of the member declaration. Optional:
	// 0 when the caller does not resolve a per-member line (it is then omitted
	// from the structured members_json so a diff tool never reads a fake 0).
	// #4420: constant-collection members carry their line so a downstream
	// cross-graph parity-audit can locate each key→value pair.
	Line int
}

// constMember is the JSON shape of one structured member in the members_json
// property emitted by EnumEntity (#4420). A downstream diff tool reads the
// literal {key,value} set without re-parsing source. Line is omitted when the
// caller did not resolve it (0).
type constMember struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Line  int    `json:"line,omitempty"`
}

// EnumQualifiedName returns the structural-ref / QualifiedName for an enum
// value-set node. Shape:
//
//	scope:enum:<sourceFile>:<EnumName>
//
// File-scoped so distinct same-named enums in different files stay distinct.
// This value is stored as the entity's QualifiedName (see EnumEntity below), so
// an edge whose ToID equals it binds via the resolver's byQualifiedName
// exact-match tier — the first probe in resolve.Index.LookupStatusHint, ahead
// of the structural, kind and bare-name tiers. The one producer that uses it is
// C#'s field→declared-type pass (csInFileTypeTargets in
// internal/extractors/csharp/field_type_refs.go), which reads the SCOPE.Enum
// record's QualifiedName directly and emits REFERENCES with
// ref_kind="field_target_type".
func EnumQualifiedName(sourceFile, enumName string) string {
	return "scope:enum:" + sourceFile + ":" + enumName
}

// EnumEntity builds the SCOPE.Enum value-set entity for one enumerated type.
// name is the bare enum type name; members are in declaration order; kindHint
// records the source construct ("python_enum", "ts_enum", "ts_literal_union",
// "java_enum", "go_iota", "rails_enum", "csharp_enum"). sourceFile / startLine
// / endLine locate the declaration. Returns ok=false when name is empty or no
// members were parsed (an enum with zero members is not worth a node).
func EnumEntity(
	name, lang, kindHint, sourceFile string,
	startLine, endLine int,
	members []EnumMember,
) (types.EntityRecord, bool) {
	name = strings.TrimSpace(name)
	if name == "" || len(members) == 0 {
		return types.EntityRecord{}, false
	}

	memberNames := make([]string, 0, len(members))
	valuePairs := make([]string, 0, len(members))
	structured := make([]constMember, 0, len(members))
	for _, m := range members {
		mn := strings.TrimSpace(m.Name)
		if mn == "" {
			continue
		}
		memberNames = append(memberNames, mn)
		v := strings.TrimSpace(m.Value)
		if v != "" {
			valuePairs = append(valuePairs, mn+"="+v)
		}
		structured = append(structured, constMember{Key: mn, Value: v, Line: m.Line})
	}
	if len(memberNames) == 0 {
		return types.EntityRecord{}, false
	}

	if startLine <= 0 {
		startLine = 1
	}
	if endLine < startLine {
		endLine = startLine
	}

	props := map[string]string{
		"enum_name":    name,
		"members":      strings.Join(memberNames, ", "),
		"member_count": strconv.Itoa(len(memberNames)),
		"kind_hint":    kindHint,
	}
	if len(valuePairs) > 0 {
		props["values"] = strings.Join(valuePairs, ", ")
	}
	// #4420: structured, enumerable member set so a downstream cross-graph
	// parity-audit can diff the literal {key,value} pairs without re-parsing
	// source. Includes ALL members (value-less members carry value=""), unlike
	// the comma-joined `values` prop which omits value-less members.
	if b, err := json.Marshal(structured); err == nil {
		props["members_json"] = string(b)
	}

	sig := "enum " + name + " { " + strings.Join(memberNames, ", ") + " }"

	e := types.EntityRecord{
		Name:               name,
		QualifiedName:      EnumQualifiedName(sourceFile, name),
		Kind:               string(types.EntityKindEnum),
		Subtype:            "enum",
		Language:           lang,
		SourceFile:         sourceFile,
		StartLine:          startLine,
		EndLine:            endLine,
		Signature:          sig,
		Properties:         props,
		EnrichmentRequired: false,
	}
	e.ID = e.ComputeID()
	return e, true
}

// StripLiteralQuotes removes a single layer of matching surrounding quotes
// (single, double, or backtick) from a literal token, returning the inner
// text. Non-quoted tokens (numbers, bare identifiers) are returned unchanged.
// Used to normalise enum member values so a TS `'active'` and a Python
// `"active"` both record the literal value `active`.
func StripLiteralQuotes(lit string) string {
	s := strings.TrimSpace(lit)
	if len(s) >= 2 {
		switch s[0] {
		case '\'', '"', '`':
			if s[len(s)-1] == s[0] {
				return s[1 : len(s)-1]
			}
		}
	}
	return s
}
