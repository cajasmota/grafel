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

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
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

	// ceComputedInvokeRE matches a COMPUTED computation-expression head — a
	// parenthesised builder expression immediately followed by an opening brace,
	// e.g. `(mkBuilder ()) { ... }` or `(StateBuilder<int>()) { ... }`. The bare
	// `ident {` form (ceInvokeRE) misses these because the head is an expression,
	// not an identifier. Captures the FIRST identifier inside the parens (1) as
	// the builder symbol (the factory / constructor being applied). The `\(\s*`
	// prefix plus a `\)` before `{` distinguishes a computed CE head from a
	// record-update head (`{ x with ... }` has no leading `( ... )`).
	ceComputedInvokeRE = regexp.MustCompile(
		`(?m)\(\s*([A-Za-z_][A-Za-z0-9_.]*)[^()]*\([^()]*\)\s*\)[ \t]*\{`,
	)

	// ceBindRE matches the CE bind-point keywords inside a body: let! / do! /
	// return! / yield! / use! / match! / and!. Captures the keyword (1).
	ceBindRE = regexp.MustCompile(
		`(?m)\b(let!|do!|return!|yield!|use!|match!|and!)`,
	)

	// ceBuilderBindRE matches a builder let-binding `let optional = OptionBuilder()`
	// — a let bound to a constructor application of an upper-case type whose name
	// ends in `Builder`, OR any upper-case `Foo()` constructor head. Captures the
	// bound name (1) and the builder TYPE (2) so a CE USES edge to `optional` can
	// be resolved to the OptionBuilder type. Restricted to a single-line RHS.
	ceBuilderBindRE = regexp.MustCompile(
		`(?m)^[ \t]*let\s+([a-zA-Z_][a-zA-Z0-9_']*)\s*=\s*([A-Z][A-Za-z0-9_']*)\s*(?:<[^>]*>)?\s*\(`,
	)

	// matchSiteRE matches a match-arm head `| CaseName` (optionally with a payload
	// binding, e.g. `| Even -> ...` or `| Positive n -> ...`). Captures the case
	// NAME (1). Used to resolve match arms against known active-pattern cases so a
	// match site emits a USES edge to the case sub-entity. The leading `|` plus an
	// upper-case head distinguishes a match arm from a pipe operator (`|>`), a DU
	// declaration bar, or an or-pattern continuation in a literal.
	matchSiteRE = regexp.MustCompile(
		`(?m)(?:^|[^|>])\|\s*([A-Z][A-Za-z0-9_']*)\b`,
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

// collectActivePatternCases returns a map of bare case name → dotted case entity
// Name across all active-pattern definitions in the source (`Even` →
// `(|Even|Odd|).Even`), so a match site `| Even ->` can resolve to the case
// sub-entity (#5077). Built from the already-extracted active-pattern entities.
func collectActivePatternCases(apEntities []types.EntityRecord) map[string]string {
	out := make(map[string]string)
	for _, e := range apEntities {
		if e.Kind != "SCOPE.Schema" || e.Subtype != "active_pattern_case" {
			continue
		}
		bare := e.Properties["member_name"]
		if bare == "" {
			continue
		}
		// First definition wins on a name collision (rare across active patterns).
		if _, dup := out[bare]; !dup {
			out[bare] = e.Name
		}
	}
	return out
}

// collectCEBuilderTypes returns the set of type NAMES that declare the CE builder
// protocol, plus a set of CE-member names (Bind/Return/...) declared by ANY
// builder in the file. The member set drives ce_member re-typing of the
// individual builder-protocol operations (#5077); the type set lets builder
// let-bindings resolve (`let optional = OptionBuilder()`).
func collectCEBuilderTypes(src string) (types_ map[string]bool, ceMembers map[string]bool) {
	types_ = make(map[string]bool)
	ceMembers = make(map[string]bool)
	for _, m := range typeRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		name := src[m[4]:m[5]]
		body := extractIndentBody(src, m[1], len(src[m[2]:m[3]]))
		members, ok := detectCEBuilder(body)
		if !ok {
			continue
		}
		types_[name] = true
		for _, mem := range members {
			ceMembers[mem] = true
		}
	}
	return types_, ceMembers
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

// collectBuilderBindings scans the whole source for builder let-bindings
// `let optional = OptionBuilder()` and returns a map bound-name → builder type
// (`optional` → `OptionBuilder`). The set of recognised CE builder TYPES (those
// stamped ce_builder during the type pass) is passed in so an arbitrary
// `let x = Foo ()` is only recorded when Foo is a known builder type. This lets
// a CE USES edge to `optional` resolve its target to the OptionBuilder type
// entity (#5077). De-duplicated per bound name (first binding wins).
func collectBuilderBindings(src string, builderTypes map[string]bool) map[string]string {
	scrubbed := stripStringsAndComments(src)
	out := make(map[string]string)
	for _, m := range ceBuilderBindRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) < 3 {
			continue
		}
		bound, typ := m[1], m[2]
		if _, dup := out[bound]; dup {
			continue
		}
		// Record when the RHS type is a known CE builder OR its name ends in
		// "Builder" (the F# naming convention) — both are strong CE signals.
		if builderTypes[typ] || strings.HasSuffix(typ, "Builder") {
			out[bound] = typ
		}
	}
	return out
}

// collectCEUsage scans an operation body for computation-expression invocations
// `builder { ... }` (and COMPUTED heads `(mkBuilder ()) { ... }`, #5077) plus
// bind points (let!/do!/return!/...), returning USES edges to the builder
// symbols. Each edge is stamped with the bind-point kinds seen in the body so a
// consumer knows the CE is an effect boundary. Known intrinsic builders
// (async/task/seq/...) are recognised by name even without a local builder type.
// When a builder symbol resolves through bindings (`let optional = OptionBuilder()`)
// the edge is RE-TARGETED to the builder TYPE and stamped ce_builder_type so the
// USES edge points at the resolved builder-type entity rather than the raw
// symbol (#5077). The returned edges are de-duplicated per resolved target.
func collectCEUsage(body string, bindings map[string]string) []types.RelationshipRecord {
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

	// addUsage emits a USES edge for a CE head `symbol`, resolving the target to a
	// bound builder TYPE when known. resolved indicates the head came from the
	// computed `( ... ) {` form.
	addUsage := func(symbol string, off int, computed bool) {
		if symbol == "" || fsharpKeywords[symbol] {
			return
		}
		// Resolve the USES target: a let-bound builder symbol re-targets to its
		// builder TYPE (`optional` → OptionBuilder); otherwise the raw symbol.
		target := symbol
		resolvedType := ""
		if typ, ok := bindings[symbol]; ok {
			target = typ
			resolvedType = typ
		}
		if seen[target] {
			return
		}
		seen[target] = true
		line := 1 + strings.Count(scrubbed[:off], "\n")
		props := types.Props{
			{K: "ce_builder", V: symbol},
			{K: "line", V: strconv.Itoa(line)},
		}
		if resolvedType != "" {
			props.Set("ce_builder_type", resolvedType)
		}
		if computed {
			props.Set("ce_head", "computed")
		}
		if len(bindList) > 0 {
			props.Set("ce_bind_points", strings.Join(bindList, ","))
		}
		out = append(out, types.RelationshipRecord{
			ToID:       target,
			Kind:       "USES",
			Properties: props,
		})
	}

	// Bare identifier head: `builder { ... }`.
	for _, m := range ceInvokeRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		addUsage(scrubbed[m[2]:m[3]], m[2], false)
	}
	// Computed head: `(mkBuilder ()) { ... }` (#5077).
	for _, m := range ceComputedInvokeRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		addUsage(scrubbed[m[2]:m[3]], m[2], true)
	}
	return out
}

// collectMatchSiteEdges scans an operation body for match arms `| CaseName ->`
// and, when CaseName is a KNOWN active-pattern case, emits a USES edge to the
// case sub-entity (#5077). knownCases maps a bare case name to the dotted case
// entity Name (`Even` → `(|Even|Odd|).Even`); refFor builds the resolvable
// structural ref. This closes the active-pattern MATCH-SITE gap: case
// sub-entities already exist, but a match site did not previously reference one.
// Edges are de-duplicated per case. Each is stamped match_site=true + the line.
func collectMatchSiteEdges(body, filePath string, knownCases map[string]string) []types.RelationshipRecord {
	if body == "" || len(knownCases) == 0 {
		return nil
	}
	scrubbed := stripStringsAndComments(body)
	seen := make(map[string]bool)
	var out []types.RelationshipRecord
	for _, m := range matchSiteRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		caseName := scrubbed[m[2]:m[3]]
		dotted, ok := knownCases[caseName]
		if !ok || seen[caseName] {
			continue
		}
		seen[caseName] = true
		line := 1 + strings.Count(scrubbed[:m[2]], "\n")
		out = append(out, types.RelationshipRecord{
			ToID: extractor.BuildSchemaFieldStructuralRef("fsharp", filePath, dotted),
			Kind: "USES",
			Properties: types.Props{
				{K: "active_pattern_case", V: caseName},
				{K: "line", V: strconv.Itoa(line)},
				{K: "match_site", V: "true"},
			},
		})
	}
	return out
}
