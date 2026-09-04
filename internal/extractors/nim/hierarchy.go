// hierarchy.go — Nim inheritance topology: `type X = ref object of Y` → EXTENDS
// (#6370).
//
// Before this, nim emitted NO hierarchy edge by any of the three paths a
// language can get one: it is absent from `supportedLanguages` in
// `internal/extractors/cross/hierarchy/extractor.go`, it has no YAML rule pack
// under `internal/engine/rules/` (there is no `nim` directory there at all, and
// `_engine/hierarchy_extractor.yaml` lists 14 languages, nim not among them),
// and `typeRE` in nim.go stopped at the `object` keyword and threw the `of Y`
// clause away. "What extends this type" returned empty, which is
// indistinguishable from "nothing does".
//
// # Why the edge is EXTENDS and why there is no kind ladder
//
// Nim object inheritance has exactly one form. `type X = ref object of Y` (and
// `object of Y` without `ref`) makes X a subtype of the object type Y,
// inheriting its fields; Nim has no interface/protocol construct that could be
// IMPLEMENTS — `concept` is a structural constraint that is never named in an
// `of` clause. So, unlike `internal/extractors/csharp/hierarchy.go`, there is
// nothing here to select between and no ladder to get wrong in either
// direction.
//
// # Why the edge is emitted HERE and not by registering nim in cross/hierarchy
//
// That pass invents its own graph nodes: for every type it addEntity's a
// SCOPE.Component for the type AND another for each parent. extractNim already
// emits one SCOPE.Component per nim type, so registering nim there would mint a
// duplicate component per type, with the edges anchored on the pass's own node
// rather than on the one the rest of the nim graph uses — plus a synthesised
// component for bases like RootObj and CatchableError, which live in Nim's
// stdlib outside the tree and would be permanent orphans. That is why #6335
// emitted F#'s edges from the F# extractor and #6437 groovy's from the groovy
// one. TestNimHierarchy_NoDuplicateComponents guards both halves.
//
// # Nim's `of` is overloaded, and the ANCHOR is the only thing that disambiguates
//
// `of` is four different things in Nim: the inheritance clause, a `case`
// statement branch (`of nkInt:`), an object-variant branch inside an object
// body, and the `x of T` runtime type test. The token itself carries no
// information about which.
//
// baseOfEdge never searches for `of`. It is handed the byte offset at which
// typeRE's match ENDED — i.e. the position immediately after the `object` /
// `ref object` keyword of a type declaration — and anchors `^[ \t]*of` there,
// on that same line. Nothing else in the language can occupy that position, so
// every other `of` in the file is excluded structurally rather than by a
// blocklist: a `case`/variant branch sits on a later line (excluded by `[ \t]*`
// never matching a newline), an `x of T` test sits inside a proc body, and a
// `#` comment or a string literal after the keyword pushes the `of` past a
// non-`of` character. The extractor has NO comment or string awareness of its
// own, and this comment claims none — the anchor is the whole guard.
// TestNimHierarchy_OverloadedOfDoesNotFire enumerates the four and pins it.
//
// The limit of that guard, stated rather than left to be discovered: the anchor
// decides which `of` belongs to a declaration, and has no opinion on whether the
// DECLARATION is real. A whole `type X = ref object of Y` inside a `"""…"""`
// string literal is line-initial, so typeRE matches it and this file emits the
// edge. The entity half of that is pre-existing; the edge half arrives with
// #6370, so it is pinned as the deliberate known-divergence test
// TestNimHierarchy_InsideTripleQuotedStringOverFires_KnownDivergence — mutating
// toward the fix (blanking triple-quoted regions before extraction) fires it and
// nothing else in the package moves. A `#`-commented declaration is NOT in that
// class: typeRE's own `(?m)^[ \t]*` anchor makes it structurally unmatchable.
package nim

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// nimObjectOfRE matches an inheritance clause at the START of the text it is
// given — see the package header for why anchoring, not searching, is what
// keeps Nim's four meanings of `of` apart.
//
// Group 1 is the written base name including any module qualifier
// (`system.Exception`); group 2 is an optional generic argument list
// (`[T, U]`), matched so it can be reported and erased rather than silently
// truncated. `[^\]\n]*` keeps the arguments on one line: nothing about a base
// type continues onto the next.
var nimObjectOfRE = regexp.MustCompile(
	`^[ \t]*of[ \t]+((?:[A-Za-z_\x{0080}-\x{FFFF}][A-Za-z0-9_\x{0080}-\x{FFFF}]*\.)*` +
		`[A-Za-z_\x{0080}-\x{FFFF}][A-Za-z0-9_\x{0080}-\x{FFFF}]*)(\[[^\]\n]*\])?`)

// baseOfEdge returns the EXTENDS relationship declared by one type declaration,
// or nil when the declaration has no `of` clause.
//
// `src` is the whole file, `afterKind` the byte offset just past typeRE's
// match (the end of the `object` / `ref object` / `enum` / … keyword), `kind`
// that keyword, `owner` the declared type name and `line` its 1-based line.
//
// Only `object` and `ref object` are considered. `of` is not part of the enum,
// tuple or distinct grammar, so an `of` following one of those keywords is not
// an inheritance clause under any reading; restricting here keeps the single
// EXTENDS kind honest instead of minting an edge whose meaning is unknown.
// TestNimHierarchy_NonObjectKindsNeverExtend pins that with the (invalid)
// sources that would otherwise reach it. `ptr object of Y` is real Nim and is
// NOT handled — typeRE does not recognise `ptr object` at all, so such a type
// is not even an entity; TestNimHierarchy_PtrObjectIsInvisible_KnownDivergence
// asserts that current wrong output on purpose, so whoever widens typeRE is
// told by a failing test that the edge half is theirs too.
//
// The record is meant to be EMBEDDED on the owning type's EntityRecord, not
// appended to a standalone slice: only resolve.ReferencesEmbedded supplies the
// parent's file and package dir, which the locality tiers rank on.
//
// FromID is deliberately left EMPTY. The assembly loop stamps the owning
// record's own entity id, the only value that anchors the edge on the TYPE. A
// non-empty non-hex FromID (e.g. the file path) would be rewritten by
// ReferencesEmbedded onto the FILE entity, merging every type in a multi-type
// file onto one node — the defect fixed in #6295 (Solidity) and #6298 (Verilog,
// Astro) and now guarded by
// internal/extractors/file_anchored_rels_guard_test.go (#6367).
//
// ToID is the bare base type name with any module qualifier and generic
// arguments erased. That is what groovy's collectHierarchyEdges does (its own
// comment credits the convention to Solidity/Crystal/F#; not re-verified here)
// and it is the right answer for Nim on its own terms: bases like RootObj,
// CatchableError and Exception live in Nim's stdlib outside any indexed tree,
// so a file-pinned structural ref would never bind. When the
// written form differs from the bare name a `base` property carries it, so the
// qualifier and the generic arguments are recorded rather than lost.
//
// A type naming itself as its own base yields no edge: a self-edge is never
// information and is the signature of a mis-attributed owner (#6369).
//
// There is deliberately no `owner == ""` guard, no `afterKind` bounds check and
// no `bare == ""` skip. None is reachable: the sole call site is extractNim's
// typeRE loop, whose `name` comes from group 1 (`[A-Z][a-zA-Z0-9_]*`, non-empty
// by construction) and whose `afterKind` is that same match's end index, which
// is always within src; and nimObjectOfRE's final identifier component requires
// at least one character, so `bare` cannot be empty either. Re-introducing all
// three scored ALIVE against the whole package suite, which is the evidence
// they belonged out of the code: a branch no input can reach is not a guard, it
// is a claim no test can observe.
func baseOfEdge(src string, afterKind int, kind, owner string, line int) *types.RelationshipRecord {
	if !isObjectKind(kind) {
		return nil
	}
	m := nimObjectOfRE.FindStringSubmatch(src[afterKind:])
	if m == nil {
		return nil
	}
	written := m[1] + m[2]
	bare := m[1]
	if i := strings.LastIndexByte(bare, '.'); i >= 0 {
		bare = bare[i+1:]
	}
	if bare == owner {
		return nil
	}
	// types.Props is a key-SORTED slice and Get binary-searches it; building it
	// with a literal or append puts "base" after "provenance" and Get("base")
	// then reads as absent while the pair sits in the slice. Set maintains the
	// order.
	var props types.Props
	props.Set("line", strconv.Itoa(line))
	props.Set("provenance", "nim_object_of")
	if written != bare {
		props.Set("base", written)
	}
	return &types.RelationshipRecord{
		// FromID intentionally empty — see the doc comment above.
		ToID:       bare,
		Kind:       "EXTENDS",
		Properties: props,
	}
}

// isObjectKind reports whether typeRE's kind group names an object type. It
// covers the two object forms typeRE recognises; `ptr object` is a third that
// typeRE does not match at all (see baseOfEdge's comment and
// TestNimHierarchy_PtrObjectIsInvisible_KnownDivergence).
func isObjectKind(kind string) bool {
	return kind == "object" || strings.HasPrefix(kind, "ref")
}
