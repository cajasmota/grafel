// hierarchy.go — Pony conformance topology: `class Foo is Bar` → IMPLEMENTS
// (#6370).
//
// Before this, pony emitted NO hierarchy edge by any of the three paths a
// language can get one, all three verified for pony specifically rather than
// inherited from the issue's list:
//
//  1. this package contained no "EXTENDS"/"IMPLEMENTS" literal at all;
//  2. "pony" is absent from `supportedLanguages` in
//     `internal/extractors/cross/hierarchy/extractor.go`;
//  3. there is no `internal/engine/rules/pony/` directory, and the only
//     rule-tree hit for the string "pony" is
//     `internal/engine/rules/python/orms/pony_orm.yaml`, which is the *Python*
//     Pony ORM and emits no hierarchy relationship.
//
// `typeDeclarationRE` stopped at the declared name and threw the `is` clause
// away, so "what implements this trait" returned empty — indistinguishable
// from "nothing does".
//
// # Why the edge is IMPLEMENTS and why there is no kind ladder
//
// Pony has exactly ONE subtyping construct. The `is` clause (the "provides"
// list) states that the declared type conforms to the named traits or
// interfaces; there is no separate class-extension form, no state is
// inherited, and an `actor`, `class`, `struct`, `primitive`, `interface` and
// `trait` all spell conformance the same way. So there is nothing here to
// select between, and — unlike `internal/extractors/csharp/hierarchy.go`,
// whose ladder is pinned as wrong in both directions — no ladder to get wrong.
//
// # Why the edge is emitted HERE and not by registering pony in cross/hierarchy
//
// That pass invents its own graph nodes: for every type it addEntity's a
// SCOPE.Component for the type AND another for each named parent. extractPony
// already emits one SCOPE.Component per pony type, so registering pony there
// would mint a duplicate component per type with the edges anchored on the
// pass's own node rather than on the one the rest of the pony graph uses.
// That is why #6335 emitted F#'s edges from the F# extractor, #6437 groovy's
// from the groovy one, and #6804 nim's from the nim one.
// TestPonyHierarchy_NoDuplicateComponents guards both halves.
//
// # Pony's `is` is overloaded, and the ANCHOR is the only thing that separates
// # the meanings
//
// `is` is three different things in Pony: the conformance clause of a type
// declaration, the identity-comparison operator (`if a is b then`), and part
// of the `type Alias is X` alias form. The token itself carries no information
// about which.
//
// provideEdges NEVER searches for `is`. It is handed the byte offset at which
// typeDeclarationRE's match ENDED — the position immediately after the
// declared type NAME — skips an optional generic parameter list from exactly
// there, and then requires `[ \t]+is[ \t]+` at exactly that position. Every
// other `is` in the file is excluded STRUCTURALLY by position rather than by a
// blocklist:
//
//   - the identity operator (`a is b`) appears inside a method body. The bound
//     the code actually guarantees is narrower than "it can never be reached",
//     and the narrow version is the true one: the anchor reads from the offset
//     just past the declared NAME and `[ \t]+` never matches a newline, so it
//     can only ever see the REMAINDER OF THE DECLARATION LINE. A body sits on a
//     later line and is therefore out of reach. What that does NOT rule out is
//     a declaration whose own line is unusual: typeDeclarationRE separates the
//     keyword from the name with `\s+`, which DOES cross newlines, so
//     `class\n  Foo is Bar` yields Foo -> Bar. Contrived, and arguably right,
//     but it is a case the word "never" would have hidden;
//   - an `is` inside a string literal or after a `//` in a method body is on
//     that same later line, so the same bound excludes it;
//   - a `//`-commented declaration (`// class Foo is Bar`) is not matched by
//     typeDeclarationRE at all — that regex is `(?m)^(actor|class|…)` with NO
//     leading-whitespace allowance, so a comment marker before the keyword
//     makes the line structurally unmatchable and provideEdges is never even
//     called for it;
//   - `type Alias is X` is not a conformance relation (it is a type alias, and
//     its right-hand side is commonly a `|` union). The alias form is handled
//     by typeAliasRE, a different loop that does not call this file at all, so
//     aliases are excluded by call-site rather than by a filter.
//     TestPonyHierarchy_TypeAliasIsProducesNoEdge pins that.
//
// What the anchor does NOT guard against, stated rather than left to be
// discovered: this extractor has NO comment or string awareness of its own,
// and this comment claims none. The anchor decides which `is` belongs to a
// declaration; it has no opinion on whether the DECLARATION is real. A
// `class Foo is Bar` written at column 0 inside a `/* … */` block comment, or
// inside a `"""…"""` docstring — whether that docstring is a type's own or sits
// inside a method body — is line-initial, so typeDeclarationRE matches it and
// this file emits the edge. The entity half of that is pre-existing; the edge
// half arrives with #6370, so ALL THREE shapes are pinned, each as its own row,
// by TestPonyHierarchy_DeclarationInsideCommentOrDocstringOverFires_KnownDivergence.
// Two of the three were asserted nowhere in this change's first draft while
// this comment named them, which is the defect class the pin exists to stop.
package pony

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// ponyIsAnchorRE matches the conformance keyword at the START of the text it is
// given — see the package header for why anchoring, not searching, is what
// keeps Pony's three meanings of `is` apart. It deliberately carries no
// capture: the provides list is parsed by hand because `&` nests inside
// parentheses and generic arguments, which a regex here cannot bracket.
var ponyIsAnchorRE = regexp.MustCompile(`^[ \t]+is[ \t]+`)

// ponyProvidedTypeRE matches ONE leaf of a provides list, whole. Group 1 is an
// optional package-alias qualifier (`c.` in `use c = "collections"` … `c.List`)
// and group 2 the bare type name; group 3 is an optional generic argument list,
// matched so it can be reported and erased rather than silently truncated.
// Being fully anchored at both ends is what rejects anything that is not a
// plain named type — a `|` union member, an arrow type, a capability suffix —
// instead of half-matching it into a wrong edge.
var ponyProvidedTypeRE = regexp.MustCompile(
	`^(?:([A-Za-z_][A-Za-z0-9_]*)\.)?([A-Za-z_][A-Za-z0-9_]*)(\[[^\n]*\])?$`)

// provideEdges returns the IMPLEMENTS relationships declared by one type
// declaration's `is` clause, or nil when the declaration has none.
//
// `src` is the whole file, `afterName` the byte offset just past
// typeDeclarationRE's match (the end of the declared type NAME), `owner` that
// name and `line` its 1-based line.
//
// The records are meant to be EMBEDDED on the owning type's EntityRecord, not
// appended to a standalone slice: only resolve.ReferencesEmbedded supplies the
// parent's file and package dir, which the locality tiers rank on.
//
// FromID is deliberately left EMPTY. The assembly loop stamps the owning
// record's own entity id, the only value that anchors the edge on the TYPE. A
// non-empty non-hex FromID (e.g. the file path) would be rewritten by
// ReferencesEmbedded onto the FILE entity, merging every type in a multi-type
// file onto one node — the defect fixed in #6295 (Solidity) and #6298
// (Verilog, Astro) and now guarded by
// internal/extractors/file_anchored_rels_guard_test.go (#6367).
//
// ToID is the bare type name with any package-alias qualifier and generic
// arguments erased, which is what groovy's and nim's producers do and is right
// for Pony on its own terms: the traits a type most often provides
// (`Stringable`, `Equatable[A]`, `Comparable[A]`) live in Pony's builtin and
// stdlib packages outside any indexed tree, so a file-pinned structural ref
// would never bind. When the written form differs from the bare name a `base`
// property carries it, so the qualifier and the generic arguments are recorded
// rather than lost.
//
// A type naming itself yields no edge: a self-edge is never information and is
// the signature of a mis-attributed owner (#6369). A name repeated within one
// provides list yields ONE edge — `is (A & A)` states one fact.
func provideEdges(src string, afterName int, owner string, line int) []types.RelationshipRecord {
	pos := skipGenericParams(src, afterName)
	loc := ponyIsAnchorRE.FindStringIndex(src[pos:])
	if loc == nil {
		return nil
	}
	// loc[0] is 0 for as long as ponyIsAnchorRE keeps its `^`. Advancing by
	// loc[1] rather than by the match LENGTH is what makes that a property of
	// the regex alone: drop the `^` and this still reads the text after the
	// keyword it found, so the resulting producer is a real forward-searching
	// one rather than an incoherent hybrid that reads from the wrong offset.
	// That is the difference between a mutant that grades the anchor and one
	// that grades nothing.
	pos += loc[1]

	// The clause is read to end of LINE. A CRLF file leaves a trailing "\r"
	// here and it is deliberately NOT trimmed: splitProvides opens with
	// strings.TrimSpace, which already eats it, so a TrimSuffix would be a dead
	// line rather than a guard. That is equivalence IN FACT, not merely under
	// the suite — the same standard that removed the -1 sentinel from
	// skipGenericParams. TestPonyHierarchy_CRLFDeclarationStillConforms is the
	// positive control that the \r really is absorbed.
	rest := src[pos:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	rest = stripTrailingComment(rest)

	var out []types.RelationshipRecord
	seen := make(map[string]bool)
	for _, leaf := range splitProvides(rest) {
		m := ponyProvidedTypeRE.FindStringSubmatch(leaf)
		if m == nil {
			continue
		}
		bare := m[2]
		if bare == owner || seen[bare] {
			continue
		}
		seen[bare] = true

		// types.Props is a key-SORTED slice and Get binary-searches it (#6802);
		// a literal in non-alphabetical key order reads a PRESENT key as absent.
		// Set maintains the order.
		var props types.Props
		props.Set("line", strconv.Itoa(line))
		props.Set("provenance", "pony_is_clause")
		if leaf != bare {
			props.Set("base", leaf)
		}
		out = append(out, types.RelationshipRecord{
			// FromID intentionally empty — see the doc comment above.
			ToID:       bare,
			Kind:       "IMPLEMENTS",
			Properties: props,
		})
	}
	return out
}

// skipGenericParams returns the offset just past a generic parameter list
// starting at `pos` (`class Box[A: Any val] is Sized`). It returns `pos`
// unchanged when no `[` is there AND when the list is unterminated on its own
// line — an unterminated list must not let the anchor drift onto a later line,
// and leaving the offset ON the `[` achieves that, because the anchor demands
// whitespace and `[` is not whitespace.
//
// Returning a distinguished -1 for the unterminated case was tried and removed:
// it is not observable. `pos` and -1 both make provideEdges return nil for every
// input, so the extra branch is a claim no test can check rather than a guard —
// the same reasoning that removed nim's three unreachable guards in #6804.
// TestPonyHierarchy_UnterminatedGenericListEmitsNothing pins the behaviour that
// IS observable.
//
// The scan counts `[`/`]` depth so a nested constraint (`[A: Seq[U8]]`) is
// crossed whole; a plain "find the next ]" would stop inside it and leave the
// anchor pointing at `] is …`.
func skipGenericParams(src string, pos int) int {
	if pos >= len(src) || src[pos] != '[' {
		return pos
	}
	depth := 0
	for i := pos; i < len(src); i++ {
		switch src[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		case '\n':
			return pos
		}
	}
	return pos
}

// stripTrailingComment cuts a provides line at the first `//` or `/*`.
//
// It is NOT string-aware, and does not need to be: the text it is given starts
// immediately after a declaration's `is` keyword and runs to end of line, a
// position where the Pony grammar admits only type names, `&`, parentheses and
// brackets. There is no string literal for a comment marker to hide in. Without
// this, `class Foo is Bar // a note` would fail ponyProvidedTypeRE whole and
// silently emit NOTHING — a false negative, which is why the cut exists.
func stripTrailingComment(s string) string {
	if i := strings.Index(s, "//"); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "/*"); i >= 0 {
		s = s[:i]
	}
	return s
}

// splitProvides splits a provides list into its leaf type names, handling the
// parenthesised intersection form Pony uses for more than one trait:
// `is (Named & Sized)`, and the nesting `is (A & (B & C))`.
//
// The split is depth-aware over both `(` and `[`, so an `&` inside a generic
// argument (`is Foo[(A & B)]`) stays with its leaf rather than cutting it in
// half. A leaf that is itself fully wrapped in parentheses is unwrapped and
// re-split.
func splitProvides(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	depth := 0
	start := 0
	var parts []string
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case '&':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])

	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if inner, ok := unwrapParens(p); ok {
			out = append(out, splitProvides(inner)...)
			continue
		}
		out = append(out, p)
	}
	return out
}

// unwrapParens reports whether `s` is entirely enclosed by ONE matching
// parenthesis pair and, if so, returns its interior. `(A) & (B)` is not
// enclosed by one pair and is returned unchanged, which is what keeps the
// depth-aware split above from collapsing two leaves into one.
func unwrapParens(s string) (string, bool) {
	if len(s) < 2 || s[0] != '(' || s[len(s)-1] != ')' {
		return s, false
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i != len(s)-1 {
				return s, false
			}
		}
	}
	if depth != 0 {
		return s, false
	}
	return s[1 : len(s)-1], true
}
