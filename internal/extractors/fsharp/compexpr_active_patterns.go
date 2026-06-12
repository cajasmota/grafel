// Package fsharp: computation-expression + active-pattern modelling (#5048).
//
// Follow-up from #4942 (which landed DU-case / record-field sub-entities and the
// alias subtype). This file closes two previously-unmodeled F# constructs:
//
//   - ACTIVE PATTERNS — `let (|Even|Odd|) n = ...`, partial `(|Foo|_|)`, and
//     parameterised active patterns. The base let-scanner (letRE in extractor.go)
//     only matches a plain `[a-zA-Z_]` head, so the banana-clip name `(|Even|Odd|)`
//     is invisible to it. We emit the active-pattern definition as a first-class
//     SCOPE.Pattern entity (subtype active_pattern / partial_active_pattern) and
//     each CASE NAME as a SCOPE.Schema/active_pattern_case sub-entity with a
//     definition→case CONTAINS edge, so match-site usage (`| Even -> ...`) has a
//     resolvable target. Mirrors the DU-case sub-entity precedent
//     (du_record_members.go) and reuses the existing SCOPE.Pattern Kind.
//
//   - COMPUTATION EXPRESSIONS — `async { }` / `task { }` and CUSTOM builders
//     (`type FooBuilder() = member _.Bind/Return/...`). A CE block is an
//     effect/control-flow boundary. We (1) recognise a type as a CE BUILDER when
//     its body declares the CE member protocol (Bind/Return/ReturnFrom/Zero/
//     Combine/Delay/Run/Yield/For/While/...), stamping Properties["ce_builder"]
//     and the recognised member set so the builder type and its members are
//     discoverable; and (2) within each operation body, detect CE INVOCATIONS
//     `builder { ... }` plus the bind points `let!` / `do!` / `return!` / `yield!`
//     /`match!` /`use!` and `and!`, emitting a USES edge from the operation to the
//     builder symbol stamped with the in-body bind-point count / kinds.
//
// All new Kinds are existing (SCOPE.Pattern, SCOPE.Schema) and edges existing
// (CONTAINS, USES) — no producer-kind registration needed.
package fsharp

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

var (
	// activePatternRE matches an active-pattern let binding. The banana clip
	// `(|A|B|)` (total) or `(|A|_|)` (partial) heads the binding. Captures the
	// indentation (1) and the raw clip body (2) between the outer `(|` `|)`.
	//   let (|Even|Odd|) n = ...
	//   let (|Positive|_|) n = ...
	//   let rec (|Foo|) x = ...
	activePatternRE = regexp.MustCompile(
		`(?m)^([ \t]*)let(?:\s+rec)?(?:\s+inline)?\s+\(\|([A-Za-z0-9_'|]+(?:\|_)?)\|\)\s*([^=\n]*)=`,
	)

	// ceInvokeRE matches a computation-expression invocation `builder {` — a
	// lower/upper identifier (optionally dotted, e.g. `this.builder`) immediately
	// followed by an opening brace. Captures the builder symbol (1). The trailing
	// `{` distinguishes a CE block from a record literal head (`{ X = 1 }` has no
	// leading identifier).
	ceInvokeRE = regexp.MustCompile(
		`(?m)(?:^[ \t]*|[=([;,]\s*|\|>\s*|->\s*|\breturn\s+|\bdo\s+)([A-Za-z_][A-Za-z0-9_.]*)[ \t]*\{`,
	)

	// ceBindRE matches the CE bind-point keywords inside a body: let! / do! /
	// return! / yield! / use! / match! / and!. Captures the keyword (1).
	ceBindRE = regexp.MustCompile(
		`(?m)\b(let!|do!|return!|yield!|use!|match!|and!)`,
	)
)

// ceBuilderMembers is the canonical computation-expression builder member
// protocol. A type that declares one of these members (with at least Bind or
// Return present) is treated as a CE builder.
var ceBuilderMembers = map[string]bool{
	"Bind": true, "Return": true, "ReturnFrom": true, "Yield": true,
	"YieldFrom": true, "Zero": true, "Combine": true, "Delay": true,
	"Run": true, "For": true, "While": true, "TryWith": true,
	"TryFinally": true, "Using": true, "Source": true, "MergeSources": true,
	"BindReturn": true, "Quote": true,
}

// extractActivePatterns scans the whole source for active-pattern definitions
// and returns the SCOPE.Pattern entities (plus their case sub-entities). Each
// active pattern is a first-class entity; its case names are sub-entities so a
// match site `| Even ->` can resolve to a known case.
func extractActivePatterns(src, filePath string, imports []string) []types.EntityRecord {
	var out []types.EntityRecord
	seen := make(map[string]bool)
	for _, m := range activePatternRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 8 {
			continue
		}
		clip := src[m[4]:m[5]]   // e.g. "Even|Odd" or "Positive|_"
		params := src[m[6]:m[7]] // tokens between `|)` and `=`
		// Name the pattern by its case set, banana-clipped, so it is recognisable
		// and unique: "(|Even|Odd|)".
		name := "(|" + clip + "|)"
		if seen[name] {
			continue
		}
		seen[name] = true

		startLine := strings.Count(src[:m[0]], "\n") + 1
		// Split case names; a trailing `_` marks a PARTIAL active pattern.
		rawCases := strings.Split(clip, "|")
		partial := false
		var cases []string
		for _, c := range rawCases {
			c = strings.TrimSpace(c)
			if c == "_" || c == "" {
				if c == "_" {
					partial = true
				}
				continue
			}
			cases = append(cases, c)
		}
		subtype := "active_pattern"
		if partial {
			subtype = "partial_active_pattern"
		}
		parameterised := strings.TrimSpace(params) != "" &&
			len(strings.Fields(strings.TrimSpace(params))) > 1

		var rels []types.RelationshipRecord
		var caseEnts []types.EntityRecord
		caseSeen := make(map[string]bool)
		for _, c := range cases {
			if caseSeen[c] {
				continue
			}
			caseSeen[c] = true
			dotted := name + "." + c
			rels = append(rels, types.RelationshipRecord{
				ToID: extractor.BuildSchemaFieldStructuralRef("fsharp", filePath, dotted),
				Kind: "CONTAINS",
			})
			caseEnts = append(caseEnts, types.EntityRecord{
				Name:       dotted,
				Kind:       "SCOPE.Schema",
				Subtype:    "active_pattern_case",
				SourceFile: filePath,
				Language:   "fsharp",
				StartLine:  startLine,
				EndLine:    startLine,
				Signature:  c,
				Properties: map[string]string{
					"member_name":  c,
					"parent_class": name,
				},
			})
		}

		props := map[string]string{
			"active_pattern_cases": strings.Join(cases, ","),
			"partial":              strconv.FormatBool(partial),
			"parameterised":        strconv.FormatBool(parameterised),
			"imports":              strings.Join(imports, ","),
		}
		out = append(out, types.EntityRecord{
			Name:          name,
			Kind:          "SCOPE.Pattern",
			Subtype:       subtype,
			SourceFile:    filePath,
			Language:      "fsharp",
			StartLine:     startLine,
			EndLine:       startLine,
			Signature:     "let " + name,
			Properties:    props,
			Relationships: rels,
		})
		out = append(out, caseEnts...)
	}
	return out
}

// detectCEBuilder inspects a type body and, if it declares the CE member
// protocol, returns the recognised member set (sorted) and true. The body is the
// extractIndentBody run for the type. A type qualifies as a builder when it
// declares Bind or Return plus at least one other CE member (so an ordinary type
// with an unrelated `Return` method is not misclassified).
func detectCEBuilder(body string) ([]string, bool) {
	found := make(map[string]bool)
	for _, m := range memberRE.FindAllStringSubmatch(body, -1) {
		if len(m) < 3 {
			continue
		}
		mName := m[2]
		if ceBuilderMembers[mName] {
			found[mName] = true
		}
	}
	if !found["Bind"] && !found["Return"] && !found["Yield"] {
		return nil, false
	}
	if len(found) < 2 {
		return nil, false
	}
	members := make([]string, 0, len(found))
	for k := range found {
		members = append(members, k)
	}
	sort.Strings(members)
	return members, true
}

// collectCEUsage scans an operation body for computation-expression invocations
// `builder { ... }` and bind points (let!/do!/return!/...), returning USES edges
// to the builder symbols. Each edge is stamped with the bind-point kinds seen in
// the body so a consumer knows the CE is an effect boundary. Known intrinsic
// builders (async/task/seq/...) are recognised by name even without a local
// builder type. The returned edges are de-duplicated per builder symbol.
func collectCEUsage(body string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	scrubbed := stripStringsAndComments(body)

	// Bind-point kinds present anywhere in the body (CE effect markers).
	bindKinds := make(map[string]bool)
	for _, m := range ceBindRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) >= 2 {
			bindKinds[m[1]] = true
		}
	}
	var bindList []string
	for k := range bindKinds {
		bindList = append(bindList, k)
	}
	sort.Strings(bindList)

	seen := make(map[string]bool)
	var out []types.RelationshipRecord
	for _, m := range ceInvokeRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		builder := scrubbed[m[2]:m[3]]
		if builder == "" || fsharpKeywords[builder] {
			continue
		}
		// A bare `{` after an identifier that is actually a record-update head
		// (`{ x with ... }`) won't capture an identifier, so this is CE-shaped.
		if seen[builder] {
			continue
		}
		seen[builder] = true
		line := 1 + strings.Count(scrubbed[:m[2]], "\n")
		props := map[string]string{
			"ce_builder": builder,
			"line":       strconv.Itoa(line),
		}
		if len(bindList) > 0 {
			props["ce_bind_points"] = strings.Join(bindList, ",")
		}
		out = append(out, types.RelationshipRecord{
			ToID:       builder,
			Kind:       "USES",
			Properties: props,
		})
	}
	return out
}
