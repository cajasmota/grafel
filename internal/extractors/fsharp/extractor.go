// Package fsharp implements a regex-based extractor for F# source files.
//
// Extracted entities:
//   - module/namespace declarations → Kind="SCOPE.Component", Subtype="module"|"namespace"
//   - let/let rec/let mutable bindings (functions) → Kind="SCOPE.Operation", Subtype="let"
//   - member function definitions → Kind="SCOPE.Operation", Subtype="member"
//   - type declarations (record, discriminated union, class, interface, struct, alias)
//     → Kind="SCOPE.Component"
//   - open statements → IMPORTS edges
//   - function applications → CALLS edges. Captured call forms: paren `name(`,
//     pipe `|> name`, compose `>> name`, and space-applied `head arg`
//     (F#'s dominant curried-application idiom). Each CALLS edge is stamped with
//     a 1-based FILE-ABSOLUTE `line` Property (#5034; promoted from the prior
//     body-relative convention of #4939).
//   - Module CONTAINS members
//
// No tree-sitter grammar for F# is available in smacker/go-tree-sitter, so
// this extractor parses F# with regular expressions. F# is
// whitespace/indent-sensitive; for entity discovery purposes we rely on
// indentation heuristics similar to the Nim extractor.
//
// Registers itself via init() and is imported by registry_gen.go.
package fsharp

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("fsharp", &Extractor{})
}

// Extractor implements extractor.Extractor for F#.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "fsharp" }

// Regex patterns for F# syntax.
var (
	// module declaration: "module Foo" or "module Foo.Bar" or "module rec Foo"
	moduleRE = regexp.MustCompile(
		`(?m)^([ \t]*)module(?:\s+rec)?\s+([\w.]+)\s*$`,
	)

	// namespace declaration: "namespace Foo" or "namespace Foo.Bar"
	namespaceRE = regexp.MustCompile(
		`(?m)^([ \t]*)namespace\s+([\w.]+)\s*$`,
	)

	// let binding: "let [rec] [mutable] name [<params>] =" or "let name ="
	// Captures indentation and name. Handles generic type params like <'T>.
	letRE = regexp.MustCompile(
		`(?m)^([ \t]*)let(?:\s+rec)?(?:\s+mutable)?\s+([a-zA-Z_][a-zA-Z0-9_']*)\s*(?:<[^>]*>)?\s*(?:[^=\n]*)=`,
	)

	// member: "member [this.]Name" or "member _.Name" or "override this.Name"
	memberRE = regexp.MustCompile(
		`(?m)^([ \t]*)(?:member|override|abstract member|default)\s+(?:[a-zA-Z_][a-zA-Z0-9_']*\.)?([a-zA-Z_][a-zA-Z0-9_']*)\s*(?:<[^>]*>)?\s*(?:[^=\n]*)=`,
	)

	// type declaration: "type Foo =" or "type Foo<'T> ="
	// Matches record, DU, class, interface, struct, alias, exception types.
	typeRE = regexp.MustCompile(
		`(?m)^([ \t]*)type\s+([A-Z][a-zA-Z0-9_']*)\s*(?:<[^>]*>)?\s*(?:\([^)]*\))?\s*=`,
	)

	// type kind after "=" — helps classify subtype
	typeKindRE = regexp.MustCompile(
		`(?m)^([ \t]*)type\s+[A-Z][a-zA-Z0-9_']*\s*(?:<[^>]*>)?\s*(?:\([^)]*\))?\s*=\s*(\{|interface|class|\|)`,
	)

	// open statement: "open Foo" or "open Foo.Bar"
	openRE = regexp.MustCompile(
		`(?m)^[ \t]*open\s+([\w.]+)`,
	)

	// function application call: identifier( or Module.function(
	// Also detects pipe targets: |> identifier or |> Module.name
	callRE = regexp.MustCompile(
		`(?:^|[^\w.'"])([A-Za-z_][A-Za-z0-9_.]*)(?:\s*<[^>]*>)?\s*\(`,
	)

	// pipe operator call: |> Module.name or |> name
	pipeCallRE = regexp.MustCompile(
		`\|>\s*([A-Za-z_][A-Za-z0-9_.]*)`,
	)

	// compose operator: >> Module.name
	composeCallRE = regexp.MustCompile(
		`>>\s*([A-Za-z_][A-Za-z0-9_.]*)`,
	)

	// space-application call: F#'s dominant call idiom is curried application
	// written `head arg1 arg2` (e.g. `createUser "ada"`, `json user next ctx`).
	// These produce no paren/pipe match, so the head symbol is captured here.
	//
	// To stay conservative (F# is whitespace-sensitive and a bare identifier
	// followed by another identifier is ambiguous with type annotations,
	// record fields, etc.) the head must sit at a *call position* — the start
	// of a clause — and be followed by at least one whitespace-separated
	// argument starter. Recognised clause starters:
	//   - line start (after indentation)        ^[ \t]*
	//   - `=` (binding / continuation)           `let x = head arg`
	//   - `(` `[`                                grouping / list element
	//   - `|>` `<|` `->` `;` `,`                 pipe / lambda body / sequencing
	//   - `return` `return!` `yield` `yield!` `do` `do!` `then` `else`
	// The argument starter is a string/char/number literal, an opening paren or
	// bracket, or a lower-case identifier (an upper-case follower is more likely
	// a type/DU-case, so it is excluded to avoid false positives).
	spaceAppRE = regexp.MustCompile(
		`(?m)(?:^[ \t]*|[=([;,]\s*|\|>\s*|<\|\s*|->\s*|\breturn!?\s+|\byield!?\s+|\bdo!?\s+|\bthen\s+|\belse\s+)` +
			`([a-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)` +
			`[ \t]+(?:"|'|@"|\$"|[0-9]|\(|\[|[a-z_])`,
	)
)

// fsharpKeywords are tokens the call regex picks up but are not real calls.
var fsharpKeywords = map[string]bool{
	"if": true, "elif": true, "else": true, "then": true,
	"while": true, "for": true, "do": true, "done": true,
	"match": true, "with": true, "when": true,
	"try": true, "finally": true,
	"raise": true, "failwith": true, "failwithf": true,
	"return": true, "yield": true, "and": true, "or": true, "not": true,
	"let": true, "in": true, "fun": true, "function": true,
	"type": true, "open": true, "module": true, "namespace": true,
	"begin": true, "end": true, "inherit": true, "interface": true,
	"member": true, "override": true, "default": true, "abstract": true,
	"static": true, "mutable": true, "rec": true, "new": true,
	"null": true, "true": true, "false": true,
	"async": true, "seq": true, "query": true,
	"upcast": true, "downcast": true, "typeof": true, "typedefof": true,
	"sizeof": true, "nameof": true, "use": true, "using": true,
	// common computation expression keywords
	"async.Return": true, "async.Bind": true, "async.Zero": true,
}

// Extract processes F# source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractFSharp(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "fsharp")
	extractor.TagEntitiesLanguage(out, "fsharp")
	return out, nil
}

func extractFSharp(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	imports := collectOpenStatements(src)
	importEntities := buildImportEntities(filePath, imports)
	entities = append(entities, importEntities...)

	// #5048: active-pattern definitions (`let (|Even|Odd|) n = ...`). The base
	// letRE only matches a plain identifier head, so the banana-clip name is
	// invisible to it; scan separately and emit SCOPE.Pattern entities (+case
	// sub-entities). Track the clip name so the plain let-scanner does not also
	// try to (and fail to) bind it.
	apEntities := extractActivePatterns(src, filePath, imports)
	entities = append(entities, apEntities...)

	// #5077: resolution maps reused across the operation/type passes.
	//   - apCases:        bare case name → dotted case entity Name, so a match
	//     site `| Even ->` emits a USES edge to the active-pattern case.
	//   - ceBuilderTypes: type names that declare the CE builder protocol.
	//   - ceMemberNames:  CE-protocol member names (Bind/Return/...) declared by
	//     any builder, so the matching member operations are re-typed ce_member.
	//   - builderBindings: `let optional = OptionBuilder()` → builder TYPE, so a
	//     CE USES edge to `optional` resolves to the OptionBuilder type entity.
	apCases := collectActivePatternCases(apEntities)
	ceBuilderTypes, ceMemberNames := collectCEBuilderTypes(src)
	builderBindings := collectBuilderBindings(src, ceBuilderTypes)

	// 1. Module/namespace declarations → SCOPE.Component
	seen := make(map[string]bool)
	for _, m := range moduleRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		name := src[m[4]:m[5]]
		key := "module:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "module",
			SourceFile: filePath,
			Language:   "fsharp",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "module " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	for _, m := range namespaceRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		name := src[m[4]:m[5]]
		key := "namespace:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "namespace",
			SourceFile: filePath,
			Language:   "fsharp",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "namespace " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 2. let bindings (functions) → SCOPE.Operation
	letSeen := make(map[string]bool)
	for _, m := range letRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		indent := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		key := indent + ":let:" + name
		if letSeen[key] {
			continue
		}
		letSeen[key] = true

		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractIndentBody(src, m[1], len(indent))
		endLine := startLine + strings.Count(body, "\n")
		calls := collectCalls(body, name, startLine)
		// #5048: computation-expression usage (`async { }` / custom builders)
		// inside the body → USES edges to the builder symbol. #5077: builder
		// symbol resolves to its bound TYPE; computed `( ... ) {` heads captured.
		calls = append(calls, collectCEUsage(body, builderBindings)...)
		// #5077: active-pattern match-SITE edges — `| Even ->` → case sub-entity.
		calls = append(calls, collectMatchSiteEdges(body, filePath, apCases)...)
		// #5130: Validus / FsToolkit.ErrorHandling validator-pipeline VALIDATES
		// edges (`validate { }` / `validation { }` / Check.*/Validation.*).
		calls = append(calls, collectValidatorPipelineEdges(body, startLine)...)

		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Operation",
			Subtype:    "let",
			SourceFile: filePath,
			Language:   "fsharp",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  buildLetSig(src[m[0]:m[1]], name),
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: calls,
		})
	}

	// 3. member definitions → SCOPE.Operation
	memberSeen := make(map[string]bool)
	for _, m := range memberRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		indent := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		// Skip if same name already from let bindings (avoid double-counting)
		if letSeen[indent+":let:"+name] {
			continue
		}
		key := indent + ":member:" + name
		if memberSeen[key] {
			continue
		}
		memberSeen[key] = true

		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractIndentBody(src, m[1], len(indent))
		endLine := startLine + strings.Count(body, "\n")
		calls := collectCalls(body, name, startLine)
		calls = append(calls, collectCEUsage(body, builderBindings)...)
		calls = append(calls, collectMatchSiteEdges(body, filePath, apCases)...)
		// #5130: validator-pipeline VALIDATES edges in member bodies too.
		calls = append(calls, collectValidatorPipelineEdges(body, startLine)...)

		memberProps := map[string]string{
			"imports": strings.Join(imports, ","),
		}
		memberSubtype := "member"
		// #5077: a member that implements the CE builder protocol (Bind/Return/
		// Zero/Combine/...) is re-typed a CE-protocol operation so the builder
		// protocol is queryable. ceMemberNames holds the CE-member names declared
		// by any builder type in this file; ceBuilderMembers gates it to the
		// canonical protocol so an unrelated method named the same is not flipped.
		if ceMemberNames[name] && ceBuilderMembers[name] {
			memberSubtype = "ce_member"
			memberProps["ce_member"] = "true"
			memberProps["ce_protocol_method"] = name
		}

		entities = append(entities, types.EntityRecord{
			Name:          name,
			Kind:          "SCOPE.Operation",
			Subtype:       memberSubtype,
			SourceFile:    filePath,
			Language:      "fsharp",
			StartLine:     startLine,
			EndLine:       endLine,
			Signature:     "member " + name,
			Properties:    memberProps,
			Relationships: calls,
		})
	}

	// #5130: pre-scan the file for the set of RECORD type names so a record
	// field whose type is another in-file record can materialise an owner→nested
	// VALIDATES edge (nested_model_extraction).
	recordTypes := collectRecordTypeNames(src)

	// 4. type declarations → SCOPE.Component
	typeSeen := make(map[string]bool)
	for _, m := range typeRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		name := src[m[4]:m[5]]
		if typeSeen[name] {
			continue
		}
		typeSeen[name] = true

		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractIndentBody(src, m[1], len(src[m[2]:m[3]]))
		endLine := startLine + strings.Count(body, "\n")

		// Determine subtype
		subtype := classifyTypeSubtype(src[m[0]:m[1]], body)

		// Find members/functions that belong to this type (CONTAINS edges)
		var rels []types.RelationshipRecord
		memberRef := make(map[string]bool)
		// Check members declared at higher indentation after this type
		typeIndentLen := len(src[m[2]:m[3]])
		for _, pm := range memberRE.FindAllStringSubmatchIndex(src, -1) {
			if len(pm) < 6 {
				continue
			}
			pmIndentLen := len(src[pm[2]:pm[3]])
			if pmIndentLen <= typeIndentLen {
				continue
			}
			// Member must appear after the type declaration start
			if pm[0] < m[0] {
				continue
			}
			mName := src[pm[4]:pm[5]]
			if memberRef[mName] {
				continue
			}
			memberRef[mName] = true
			ref := extractor.BuildOperationStructuralRef("fsharp", filePath, mName)
			rels = append(rels, types.RelationshipRecord{
				ToID: ref,
				Kind: "CONTAINS",
			})
		}

		// #4942: emit DU cases / record fields as SCOPE.Schema sub-entities,
		// with a type→member CONTAINS edge each.
		memberEnts, memberRels := extractTypeMembers(name, subtype, body, filePath, startLine)
		rels = append(rels, memberRels...)

		// #5130: type-level validation edges — nested-record VALIDATES
		// (nested_model), [<CustomValidation(...)>] field validators, and
		// IValidatableObject custom validators. Only records carry nested/custom
		// field validators; IValidatableObject can sit on any type.
		var fieldRefs []fsFieldRef
		if subtype == "record" {
			for _, mi := range parseRecordFields(body) {
				fieldRefs = append(fieldRefs, fsFieldRef{
					name:       mi.name,
					typ:        mi.typ,
					attrLines:  mi.attrLines,
					lineOffset: mi.lineOffset,
				})
			}
		}
		rels = append(rels, collectTypeValidatorEdges(
			name, filePath, fieldRefs, recordTypes,
			fsTypeImplementsIValidatable(body), startLine,
		)...)

		typeProps := map[string]string{
			"imports": strings.Join(imports, ","),
		}
		// #5048: a type that declares the computation-expression builder member
		// protocol (Bind/Return/Zero/Combine/...) is a CE BUILDER — stamp it so
		// `myBuilder { ... }` USES edges have a recognisable target type.
		if members, ok := detectCEBuilder(body); ok {
			typeProps["ce_builder"] = "true"
			typeProps["ce_builder_members"] = strings.Join(members, ",")
			subtype = "computation_builder"
		}

		entities = append(entities, types.EntityRecord{
			Name:          name,
			Kind:          "SCOPE.Component",
			Subtype:       subtype,
			SourceFile:    filePath,
			Language:      "fsharp",
			StartLine:     startLine,
			EndLine:       endLine,
			Signature:     "type " + name,
			Properties:    typeProps,
			Relationships: rels,
		})
		entities = append(entities, memberEnts...)
	}

	// #5129: Fable + Elmish/Feliz frontend decoration. Import-gated — a no-op
	// for any file that does not `open` Elmish/Feliz/Fable. Mutates the entities
	// in place (re-kinds Model/Msg, tags the MVU triad, re-kinds Feliz components
	// + emits RENDERS, stamps Cmd dispatch USES edges).
	applyElmishFeliz(src, filePath, imports, entities)

	return entities
}

// classifyTypeSubtype determines the F# type subtype from the declaration context.
func classifyTypeSubtype(decl, body string) string {
	// Check for "= {" → record
	if strings.Contains(decl, "= {") || strings.TrimSpace(body) != "" && strings.HasPrefix(strings.TrimSpace(body), "{") {
		return "record"
	}
	// Check for "= |" or body starting with "|" → discriminated union
	if strings.Contains(decl, "= |") {
		return "discriminated_union"
	}
	bodyTrimmed := strings.TrimSpace(body)
	if strings.HasPrefix(bodyTrimmed, "|") {
		return "discriminated_union"
	}
	// Check for interface/class keywords
	if strings.Contains(decl, "interface") || strings.HasPrefix(bodyTrimmed, "interface") {
		return "interface"
	}
	if strings.Contains(decl, "class") || strings.HasPrefix(bodyTrimmed, "class") {
		return "class"
	}
	if strings.Contains(decl, "struct") {
		return "struct"
	}
	// #4942: a pure alias (`type Foo = Bar`, `type Id = int`) is a distinct
	// subtype, not the catch-all "type".
	if isAliasBody(body) {
		return "alias"
	}
	return "type"
}

// buildLetSig builds a signature string for a let binding from the raw declaration.
func buildLetSig(decl, name string) string {
	// Trim whitespace and return a reasonable signature
	sig := strings.TrimSpace(decl)
	if idx := strings.Index(sig, "="); idx >= 0 {
		sig = strings.TrimSpace(sig[:idx])
	}
	if sig == "" {
		return "let " + name
	}
	return sig
}

// collectOpenStatements parses "open" statements and returns unique module paths.
func collectOpenStatements(src string) []string {
	seen := make(map[string]bool)
	var imports []string

	for _, m := range openRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		mod := strings.TrimSpace(m[1])
		// Strip inline comments
		if ci := strings.IndexAny(mod, "//"); ci >= 0 {
			mod = strings.TrimSpace(mod[:ci])
		}
		if mod == "" || seen[mod] {
			continue
		}
		seen[mod] = true
		imports = append(imports, mod)
	}
	return imports
}

// buildImportEntities creates SCOPE.Component stubs carrying IMPORTS edges.
func buildImportEntities(filePath string, imports []string) []types.EntityRecord {
	if len(imports) == 0 {
		return nil
	}
	out := make([]types.EntityRecord, 0, len(imports))
	seen := make(map[string]bool, len(imports))
	for _, mod := range imports {
		if seen[mod] {
			continue
		}
		seen[mod] = true
		out = append(out, types.EntityRecord{
			Name:       importDisplayName(mod),
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   "fsharp",
			Relationships: []types.RelationshipRecord{
				{
					FromID: filePath,
					ToID:   mod,
					Kind:   "IMPORTS",
				},
			},
		})
	}
	return out
}

// importDisplayName returns a short display name for an import path.
// e.g. "Microsoft.FSharp.Collections" → "Collections"
func importDisplayName(mod string) string {
	mod = strings.TrimSpace(mod)
	if dot := strings.LastIndexByte(mod, '.'); dot >= 0 {
		return mod[dot+1:]
	}
	return mod
}

// extractIndentBody returns the body text following a declaration line.
// Collects lines that are more indented than baseIndent.
func extractIndentBody(src string, afterPos int, baseIndentLen int) string {
	rest := src[afterPos:]
	lines := strings.Split(rest, "\n")
	if len(lines) == 0 {
		return ""
	}

	var bodyLines []string
	minBodyIndent := baseIndentLen + 2 // F# typically uses 4-space indent, but 2 is minimum

	for i, line := range lines {
		if i == 0 && strings.TrimSpace(line) != "" {
			// Same-line body
			bodyLines = append(bodyLines, line)
			continue
		}
		if strings.TrimSpace(line) == "" {
			bodyLines = append(bodyLines, line)
			continue
		}
		indent := countIndent(line)
		if indent >= minBodyIndent {
			bodyLines = append(bodyLines, line)
		} else if indent <= baseIndentLen && strings.TrimSpace(line) != "" {
			break
		}
	}
	return strings.Join(bodyLines, "\n")
}

// countIndent counts leading spaces/tabs.
func countIndent(line string) int {
	n := 0
	for _, ch := range line {
		if ch == ' ' || ch == '\t' {
			n++
		} else {
			break
		}
	}
	return n
}

// collectCalls extracts CALLS edges from a function body.
//
// bodyStartLine is the 1-based FILE line at which the body's first line sits
// (i.e. the enclosing operation's StartLine). The stamped `line` Property is
// file-absolute: bodyStartLine + (body-relative line - 1), so a clickable
// jump-to-call-site is possible without a separate body-offset lookup (#5034).
func collectCalls(body, callerName string, bodyStartLine int) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	scrubbed := stripStringsAndComments(body)

	seen := make(map[string]bool)
	var out []types.RelationshipRecord

	// addCall records a CALLS edge to target, stamping the FILE-ABSOLUTE 1-based
	// line at which the call site begins (#5034 — promoted from the prior
	// body-relative convention). Duplicate targets keep the FIRST line seen.
	addCall := func(target string, off int) {
		if target == "" || callerName == target {
			return
		}
		if fsharpKeywords[target] {
			return
		}
		// Skip single-letter identifiers (usually type params)
		if len(target) == 1 {
			return
		}
		if seen[target] {
			return
		}
		seen[target] = true
		line := bodyStartLine + strings.Count(scrubbed[:off], "\n")
		out = append(out, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
			Properties: map[string]string{
				"line": strconv.Itoa(line),
			},
		})
	}

	// Regular function calls: name( — the capture-group start is the call-site
	// offset used for line stamping.
	for _, m := range callRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) >= 4 && m[2] >= 0 {
			addCall(scrubbed[m[2]:m[3]], m[2])
		}
	}

	// Pipe operator: |> name or |> Module.name
	for _, m := range pipeCallRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) >= 4 && m[2] >= 0 {
			addCall(scrubbed[m[2]:m[3]], m[2])
		}
	}

	// Compose operator: >> name
	for _, m := range composeCallRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) >= 4 && m[2] >= 0 {
			addCall(scrubbed[m[2]:m[3]], m[2])
		}
	}

	// Space-applied calls: head arg1 arg2 (F#'s dominant idiom). Gated to call
	// positions so it does not fire on prose, type annotations, or record fields.
	// Use a literal-preserving scrub: string/char literal bodies are blanked but
	// the OPENING quote survives as a visible `"`, so a string argument
	// (`createUser "ada"`) still presents an argument-starter to the scanner.
	// Byte offsets are preserved, so head positions line up with `scrubbed`.
	spaceScrubbed := scrubKeepingQuote(body)
	for _, m := range spaceAppRE.FindAllStringSubmatchIndex(spaceScrubbed, -1) {
		if len(m) >= 4 && m[2] >= 0 {
			addCall(spaceScrubbed[m[2]:m[3]], m[2])
		}
	}

	return out
}

// scrubKeepingQuote is like stripStringsAndComments but preserves the OPENING
// quote of each string/char literal as a visible `"`. This lets the
// space-application scanner recognise a string argument (`createUser "ada"`)
// whose body would otherwise be blanked to whitespace. Byte offsets are
// preserved exactly, so head-symbol offsets stay aligned with the standard scrub.
func scrubKeepingQuote(src string) string {
	scrubbed := []byte(stripStringsAndComments(src))
	i := 0
	for i < len(scrubbed) {
		ch := src[i]
		// A literal opens at a quote/char/verbatim/interpolated start where the
		// standard scrub blanked it. Restore a single `"` marker, then let the
		// inner bytes stay blank.
		if (ch == '"' || ch == '\'') && scrubbed[i] == ' ' {
			scrubbed[i] = '"'
		}
		i++
	}
	return string(scrubbed)
}

// stripStringsAndComments replaces string literals and //-line comments
// with spaces so the call scanner doesn't pick up tokens inside them.
func stripStringsAndComments(src string) string {
	out := make([]byte, len(src))
	i := 0
	inStr := byte(0) // 0=none, '"'=double-quote
	inTriple := false
	for i < len(src) {
		ch := src[i]
		if inTriple {
			out[i] = ' '
			if i+2 < len(src) && ch == '"' && src[i+1] == '"' && src[i+2] == '"' {
				out[i+1] = ' '
				out[i+2] = ' '
				i += 3
				inTriple = false
				continue
			}
			i++
			continue
		}
		if inStr != 0 {
			out[i] = ' '
			if ch == '\\' && i+1 < len(src) {
				out[i+1] = ' '
				i += 2
				continue
			}
			if ch == inStr {
				inStr = 0
			}
			i++
			continue
		}
		switch ch {
		case '"':
			// Check for triple-quoted string
			if i+2 < len(src) && src[i+1] == '"' && src[i+2] == '"' {
				out[i] = ' '
				out[i+1] = ' '
				out[i+2] = ' '
				i += 3
				inTriple = true
				continue
			}
			// Check for verbatim string @"..."
			inStr = '"'
			out[i] = ' '
			i++
		case '/':
			// F# line comment: //
			if i+1 < len(src) && src[i+1] == '/' {
				for i < len(src) && src[i] != '\n' {
					out[i] = ' '
					i++
				}
				continue
			}
			out[i] = ch
			i++
		case '(':
			// F# block comment: (* ... *)
			if i+1 < len(src) && src[i+1] == '*' {
				out[i] = ' '
				out[i+1] = ' '
				i += 2
				for i < len(src) {
					if i+1 < len(src) && src[i] == '*' && src[i+1] == ')' {
						out[i] = ' '
						out[i+1] = ' '
						i += 2
						break
					}
					out[i] = ' '
					i++
				}
				continue
			}
			out[i] = ch
			i++
		default:
			out[i] = ch
			i++
		}
	}
	return string(out)
}
