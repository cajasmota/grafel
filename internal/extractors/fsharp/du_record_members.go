// Package fsharp: discriminated-union case + record-field sub-entity emission.
//
// #4942 (follow-up #4906): classifyTypeSubtype already distinguishes
// record / discriminated_union / interface / class / struct, but the
// individual DU CASES (`Circle | Rectangle`) and record FIELDS were not
// emitted as their own entities — they were invisible in the graph. This file
// parses a type body and emits each case / field as a SCOPE.Schema/field
// sub-entity (dotted Name "<Type>.<member>"), mirroring the Rust
// struct-field / enum-variant precedent (internal/extractors/rust/struct_fields.go),
// and attaches a type→member CONTAINS edge via BuildSchemaFieldStructuralRef
// so the sub-entities are never orphans.
//
// It also recognises a pure type ALIAS (`type Foo = Bar`) so classifyTypeSubtype
// can return "alias" instead of the catch-all "type".
package fsharp

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractTypeMembers parses the body of a type declaration and returns the
// DU-case / record-field sub-entities plus the type→member CONTAINS edges to
// attach to the owner Component. `subtype` is the classifyTypeSubtype result so
// only record/discriminated_union bodies are scanned (class/interface/struct
// members already flow through the member CONTAINS path).
func extractTypeMembers(
	owner, subtype, body, filePath string,
	startLine int,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	var names []memberInfo
	switch subtype {
	case "record":
		names = parseRecordFields(body)
	case "discriminated_union":
		names = parseDUCases(body)
	default:
		return nil, nil
	}
	if len(names) == 0 {
		return nil, nil
	}

	memberSubtype := "field"
	if subtype == "discriminated_union" {
		memberSubtype = "du_case"
	}

	seen := make(map[string]bool, len(names))
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	for _, mi := range names {
		if mi.name == "" || seen[mi.name] {
			continue
		}
		seen[mi.name] = true
		dotted := owner + "." + mi.name
		line := startLine + mi.lineOffset
		sig := mi.name
		if mi.typ != "" {
			sig = mi.name + ": " + mi.typ
		}
		props := map[string]string{
			"member_name":  mi.name,
			"member_type":  mi.typ,
			"parent_class": owner,
		}
		// #5049: stamp DataAnnotations validation chips so the dashboard
		// ShapeTree renders them (Properties["validations"], comma-joined).
		if len(mi.validations) > 0 {
			props["validations"] = strings.Join(mi.validations, ",")
		}
		ents = append(ents, types.EntityRecord{
			Name:       dotted,
			Kind:       "SCOPE.Schema",
			Subtype:    memberSubtype,
			SourceFile: filePath,
			Language:   "fsharp",
			StartLine:  line,
			EndLine:    line,
			Signature:  sig,
			Properties: props,
		})
		rels = append(rels, types.RelationshipRecord{
			ToID: extractor.BuildSchemaFieldStructuralRef("fsharp", filePath, dotted),
			Kind: "CONTAINS",
		})
	}
	return ents, rels
}

// memberInfo holds a parsed DU case / record field.
type memberInfo struct {
	name        string
	typ         string   // payload type (DU `of T`) or field type annotation
	lineOffset  int      // line offset relative to the type declaration start
	validations []string // #5049: DataAnnotations chips from preceding [<...>] attributes
	attrLines   []string // #5130: raw [<...>] attribute lines (custom-validator detection)
}

// parseRecordFields parses the body of an F# record type, returning one entry
// per `Name: Type` field. Handles both multi-line and single-line
// (`{ X: int; Y: int }`) record bodies. `mutable` field qualifiers are
// tolerated.
func parseRecordFields(body string) []memberInfo {
	var out []memberInfo
	// #5049: DataAnnotations attributes (`[<Required>]`) precede the field they
	// decorate, often on their own line(s). Accumulate the raw attribute lines
	// seen since the last field and attach them to the next field parsed.
	var pendingAttrs []string
	for lineNo, raw := range strings.Split(body, "\n") {
		line := raw
		// Strip the enclosing braces so a single-line `{ X: int; Y: int }`
		// degrades to a `;`-separated field list.
		line = strings.ReplaceAll(line, "{", "")
		line = strings.ReplaceAll(line, "}", "")
		// A line that carries an attribute group but no field declaration is a
		// standalone attribute line — remember it for the next field. A line
		// that carries both (`[<Required>] Email: string`) is handled inline:
		// fsValidationChips reads the `[<...>]` prefix off the same raw line.
		hasAttr := strings.Contains(line, "[<")
		hasField := strings.ContainsRune(line, ':')
		if hasAttr && !hasField {
			pendingAttrs = append(pendingAttrs, line)
			continue
		}
		for _, seg := range strings.Split(line, ";") {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}
			seg = strings.TrimPrefix(seg, "mutable ")
			colon := strings.IndexByte(seg, ':')
			if colon < 0 {
				continue
			}
			name := strings.TrimSpace(seg[:colon])
			typ := strings.TrimSpace(seg[colon+1:])
			// An inline attribute prefix (`[<Required>] Email`) leaks into the
			// name; collect its chips, then strip it off the field name.
			var inlineAttrs []string
			if strings.Contains(name, "[<") {
				inlineAttrs = []string{name}
				if gt := strings.LastIndex(name, ">]"); gt >= 0 {
					name = strings.TrimSpace(name[gt+2:])
				}
			}
			if !isFieldName(name) {
				continue
			}
			mi := memberInfo{name: name, typ: typ, lineOffset: lineNo}
			if attrs := append(append([]string{}, pendingAttrs...), inlineAttrs...); len(attrs) > 0 {
				mi.validations = fsValidationChips(attrs)
				mi.attrLines = attrs // #5130: kept for custom-validator detection
			}
			out = append(out, mi)
			pendingAttrs = nil
		}
		// A non-attribute, non-field line (blank / comment) clears any dangling
		// attributes so they don't bind to a far-away field.
		if !hasAttr && !hasField {
			pendingAttrs = nil
		}
	}
	return out
}

// parseDUCases parses the body of an F# discriminated union, returning one
// entry per `| Case [of Payload]` case. The leading bar of the first case may
// be omitted in F# (`type T = A | B`), so a bar-less first case on the type
// line is also captured.
func parseDUCases(body string) []memberInfo {
	var out []memberInfo
	for lineNo, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Each physical line may carry several `|`-separated cases.
		for _, seg := range strings.Split(line, "|") {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}
			name, typ := splitDUCase(seg)
			if !isDUCaseName(name) {
				continue
			}
			out = append(out, memberInfo{name: name, typ: typ, lineOffset: lineNo})
		}
	}
	return out
}

// splitDUCase splits a single DU case segment `Circle of float` into its
// case name and (optional) payload type.
func splitDUCase(seg string) (name, typ string) {
	// Trim any trailing member-attribute / `with` augmentation noise.
	if idx := strings.Index(seg, " with"); idx >= 0 {
		seg = seg[:idx]
	}
	fields := strings.Fields(seg)
	if len(fields) == 0 {
		return "", ""
	}
	name = fields[0]
	if i := strings.Index(seg, " of "); i >= 0 {
		typ = strings.TrimSpace(seg[i+len(" of "):])
	}
	return name, typ
}

// isFieldName reports whether tok is a plausible F# record-field identifier
// (must start with a letter or underscore; F# fields are PascalCase by
// convention but lower-case is legal).
func isFieldName(tok string) bool {
	if tok == "" {
		return false
	}
	for i, r := range tok {
		if i == 0 {
			if !(r == '_' || isLetter(r)) {
				return false
			}
			continue
		}
		if !(r == '_' || r == '\'' || isLetter(r) || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// isDUCaseName reports whether tok is a plausible DU case name. DU cases are
// upper-case by F# rule, which also lets us reject stray lowercase tokens that
// leak from multi-line case payloads.
func isDUCaseName(tok string) bool {
	if tok == "" {
		return false
	}
	r := rune(tok[0])
	if !(r >= 'A' && r <= 'Z') {
		return false
	}
	return isFieldName(tok)
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// isAliasBody reports whether a type body is a pure alias target
// (`type Foo = Bar` / `type Id = int` / `type Pair = int * string`) rather than
// a record / DU / class / interface / struct. Used by classifyTypeSubtype to
// distinguish the "alias" subtype from the catch-all "type".
func isAliasBody(body string) bool {
	b := strings.TrimSpace(body)
	if b == "" {
		return false
	}
	// Anything that opens a structured type is not an alias.
	if strings.HasPrefix(b, "{") || strings.HasPrefix(b, "|") {
		return false
	}
	first := strings.Fields(b)
	if len(first) == 0 {
		return false
	}
	switch first[0] {
	case "interface", "class", "struct", "abstract", "inherit", "member", "delegate":
		return false
	}
	// A pure alias body is a single logical type expression — it has no `=`
	// continuation and no newline-separated member block. If subsequent
	// non-blank lines exist, it is a structured body, not an alias.
	lines := strings.Split(b, "\n")
	nonBlank := 0
	for _, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			nonBlank++
		}
	}
	return nonBlank == 1
}
