// hierarchy.go — Blazor component inheritance topology: `@inherits` → EXTENDS,
// `@implements` → IMPLEMENTS (#6370, final arm).
//
// Before this, razor emitted NO hierarchy edge by either of the two paths a
// language can get one: it is absent from `supportedLanguages` in
// `internal/extractors/cross/hierarchy/extractor.go`, and this extractor never
// looked at either directive (`@inherits` and `@implements` had ZERO
// occurrences anywhere in the package — independently verified in #6343's
// Arm 0). "What extends this Blazor component" returned empty, which is
// indistinguishable from "nothing does" — the #6321 silence class.
//
// # Why the edges are emitted HERE and not by registering razor in cross/hierarchy
//
// That pass invents its own graph nodes: for every type it addEntity's a
// SCOPE.Component for the type AND another for each parent (see extractRuby at
// extractor.go:565-579, the same shape extractJTCSharp uses). This extractor
// already emits exactly one SCOPE.UIComponent per .razor file, so registering
// razor there would mint a duplicate component per file, with the edges
// anchored on the pass's own node rather than on the one the rest of the razor
// graph uses (@inject, @using and the CONTAINS edges to event handlers all hang
// off that one component). That is why #6335 emitted F#'s edges from the F#
// extractor and #6437 emitted Groovy's from Groovy's.
//
// NOTHING IN THIS PACKAGE OBSERVES THAT ROUTE, and the claim is narrowed to say
// so. TestRazorHierarchy_NoComponentMintedForHierarchyTarget and the golden
// fixture's forbidden_entities rows both grade the SHAPE the route produces —
// an entity minted from a hierarchy target rather than from a file path, which
// a mutant does produce and both instruments catch. Neither would move if
// "razor" were added to supportedLanguages, because both drive this extractor
// directly. The decision is guarded at the symptom, not at the route.
//
// # Razor is not a class language — what the FROM side is
//
// The other arms of #6370 anchor on a class entity. Razor has none: this
// package's only per-file type-like node is the SCOPE.UIComponent whose name
// `componentNameFromPath` computes from the FILE PATH without reading the file
// (extractor.go). That component is entities[0] and is already the anchor the
// rest of the razor graph uses, so the hierarchy edges are EMBEDDED on it, the
// same record that carries the CONTAINS edges to event handlers.
//
// FromID is deliberately left EMPTY. Graph assembly stamps the owning record's
// own entity id, the only value that anchors the edge on the COMPONENT. A
// non-empty non-hex FromID (e.g. the file path) would be rewritten by
// ReferencesEmbedded onto the FILE entity — the defect fixed in #6295
// (Solidity) and #6298 (Verilog, Astro) and now guarded by
// internal/extractors/file_anchored_rels_guard_test.go (#6367).
//
// ToID is the bare written type name with generic arguments erased, matching
// the Solidity / Crystal / F# / Groovy convention: a Blazor base component or
// interface is essentially always declared in another file (`ComponentBase` and
// `IDisposable` are framework types that are not in the indexed tree at all), so
// a file-pinned structural ref would never bind. A NAMESPACE-QUALIFIED target is
// kept as written and therefore dangles; the measured reason, and the precision
// it is measured at, are in TestRazorHierarchy_QualifiedTargetKeptAsWritten —
// briefly, a bare leaf name mis-binds only when there is exactly ONE same-named
// candidate, and DANGLES when there are several. "Binds to any same-named type
// in any namespace" would over-claim it.
//
// # BOM (#6962)
//
// #6962 records that a UTF-8 BOM defeats `(?m)^@using` at line 1 in this
// package: the BOM sits between the start of the string and the `@`, so `^`
// matches before it and the directive on line 1 is missed. The anchors below
// would inherit that hole verbatim, so they tolerate an optional leading BOM
// explicitly (TestRazorHierarchy_BOMAtLineOne pins it). This is deliberately
// NOT a fix for #6962 — `reUsing` and `reInject` are untouched and still carry
// the defect; #6962 owns the package-wide fix. What is claimed here is only
// that this arm does not add a third anchor with the same hole.
//
// # Population, stated rather than implied
//
// Measured 2026-09-07 on the read-only corpora at
// /Users/jorgecajas/Projects/archigraph-corpora (macOS / APFS / arm64): across
// 105 `.razor` files, 4 files carry `^@inherits` (4 occurrences, all
// `LayoutComponentBase`) and 3 carry `^@implements` (2 `IDisposable`, 1
// `IAsyncDisposable`). Zero occurrences carry leading whitespace and zero of
// the 105 files carry a BOM. That is a small population and it is not
// extrapolated here. The justification is the silence, not the volume.
package razor

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

var (
	// Razor directives are line-leading. `\x{FEFF}?` absorbs a UTF-8 BOM at
	// line 1 — see the BOM section of the package comment (#6962).
	//
	// The trailing `[^\S\n]` (horizontal whitespace only, never a newline)
	// keeps a bare `@inheritsFoo` or `@inherits` alone on its line from
	// matching, and stops the capture at the end of the directive's own line.
	reInherits   = regexp.MustCompile(`(?m)^\x{FEFF}?@inherits[^\S\n]+([^\n]*)`)
	reImplements = regexp.MustCompile(`(?m)^\x{FEFF}?@implements[^\S\n]+([^\n]*)`)

	// A Razor comment: @* … *@. Blanked before the directive scan so a
	// commented-out directive is not a clause.
	reRazorComment = regexp.MustCompile(`(?s)@\*.*?\*@`)
)

// scrubRazorComments blanks the contents of every @* … *@ comment, keeping
// every byte offset (and every newline) where it was so line stamping stays
// exact.
func scrubRazorComments(src string) string {
	locs := reRazorComment.FindAllStringIndex(src, -1)
	if len(locs) == 0 {
		return src
	}
	out := []byte(src)
	for _, loc := range locs {
		for i := loc[0]; i < loc[1]; i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	return string(out)
}

// stripGenerics removes every generic argument list from s, so
// `ComponentBase<TItem>` → `ComponentBase` and `Base<A, B>` → `Base`. The type
// argument text is DROPPED rather than kept, which is what makes the result a
// declared type name and not a per-instantiation one: an edge to
// `RestfulController<Post>` would bind to nothing.
func stripGenerics(s string) string {
	var b strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteByte(c)
			}
		}
	}
	return b.String()
}

// directiveTarget returns the type named by a directive's argument text, or ""
// when there is none. Only the first whitespace-delimited token is taken:
// `@implements IDisposable` is the whole directive, and a second token on the
// line is not part of the name.
//
// The split matters only on MALFORMED input — every well-formed directive has
// exactly one token once generics are erased and comments are blanked — so it
// is pinned by TestRazorHierarchy_FirstTokenOfArgumentWins, which exists
// because a mutant returning the whole trimmed line survived the entire suite
// and the golden fixture without it.
func directiveTarget(arg string) string {
	fields := strings.Fields(stripGenerics(arg))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// collectHierarchyEdges returns the EXTENDS/IMPLEMENTS edges declared by a
// .razor file's directives, in the order EXTENDS-then-IMPLEMENTS.
//
// One component can carry several `@implements` lines (each names one
// interface); Razor permits only one `@inherits`, but the extractor sees text,
// not a compiler, so every match is read and deduped rather than assumed
// unique.
//
// The records are meant to be EMBEDDED on the component's EntityRecord — see
// the package comment for why FromID is empty and why ToID is a bare name.
func collectHierarchyEdges(src, owner string) []types.RelationshipRecord {
	if src == "" || owner == "" {
		return nil
	}
	scrubbed := scrubRazorComments(src)

	var out []types.RelationshipRecord
	seen := map[string]bool{}
	add := func(kind string, re *regexp.Regexp) {
		for _, m := range re.FindAllStringSubmatchIndex(scrubbed, -1) {
			target := directiveTarget(scrubbed[m[2]:m[3]])
			// A self-edge is never information; it is the signature of a
			// mis-attributed owner (#6369).
			if target == "" || target == owner {
				continue
			}
			key := kind + ":" + target
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, types.RelationshipRecord{
				// FromID intentionally empty — see the package comment.
				ToID: target,
				Kind: kind,
				Properties: types.Props{
					{K: "line", V: strconv.Itoa(lineOf(scrubbed, m[0]))},
				},
			})
		}
	}
	add("EXTENDS", reInherits)
	add("IMPLEMENTS", reImplements)
	return out
}
