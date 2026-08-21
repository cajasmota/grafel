package external

import (
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// #6337 — external synthesis for VB.NET EXTENDS / IMPLEMENTS targets.
//
// Every other arm of classifyExternal is gated on a relKind of IMPORTS or
// CALLS, or on a structural-ref stub prefix, so a hierarchy target had exactly
// one path available to it: the generic dotted-root fallback. That path leaves
// a large fraction of hierarchy edges as raw stubs, which internal/feedback
// counts as bug-extractor, and folds the ones it does resolve into a single
// `ext:System` node — plus one `ext:Windows`, which resolves on the authority
// of the RUST `windows` crate entry in knownExternalPackages.
//
// The denominator, re-measured on the branch by driving this repo's own
// extractor over the 302-file VB.NET corpus (WakeOnLAN + StaxRip +
// display-drivers-uninstaller): 289 EXTENDS + 72 type-level IMPLEMENTS + 137
// member-level IMPLEMENTS = 498 hierarchy edges. An earlier version of this
// comment said 494, which is not reproducible at any stage — raw, deduped by
// (from, to, kind), or with the partial-class RepoRoot merge active all give
// 498, and 498 is also what #6337's own measurement records.
//
// The arm below runs BEFORE that fallback and returns an ecosystem-tagged
// canonical (`dotnet:…`), following the existing `node:` / `docker:` / `gha:`
// convention, so it never consults knownExternalPackages and cannot borrow
// another ecosystem's authority for `Windows`, `IO`, `Text` or `System`.
//
// # Why this cannot mask an extractor bug
//
// Three independent gates, and all three must hold:
//
//  1. UNRESOLVED IS ESTABLISHED FACT, not a guess. `ReferencesEmbeddedWithAllowlist`
//     runs inside buildDocument, which the indexer calls BEFORE Synthesize, and
//     Synthesize's own loop skips any ToID that is already hex or already
//     `ext:`. By the time a stub reaches here in-tree resolution has failed.
//
//  2. THE NAME MUST BE A WELL-FORMED .NET FRAMEWORK TYPE.
//     resolve.VBNetExternalHierarchyTarget consults a curated, corpus-derived
//     table (bare leaves) plus a three-entry root-namespace set (dotted
//     spellings), and rejects malformed spellings outright. A typo (`Fomr`), a
//     misparsed clause (`System.`), or a genuinely-missing in-tree base does
//     not classify and stays a bug edge — which is the whole point: an `ext:`
//     node for a typo is noise that hides a real extractor defect.
//
//  3. NO IN-TREE ENTITY MAY CARRY THE NAME. inTreeNames is built from
//     doc.Entities in Synthesize. A WinForms application that declares its own
//     partial `Panel`, `Label` or `Form` keeps the unresolved edge, because
//     that ambiguity is the partial-class defect and not an external base.
//     For a member-level clause the guard is applied to the TYPE part.
//
// # What gate 3 does and does not have evidence for
//
// An earlier version of this comment said gate 3 is what keeps the 16 measured
// in-tree `IFrameServer.*` member misses visible. IT IS NOT (#6337 round 2).
// Those targets are rejected one gate earlier, by gate 2: `IFrameServer` is not
// in vbExternalBaseTypes, so resolve.VBNetExternalHierarchyTarget returns
// ok=false and gate 3 is never reached. Measured over the whole 302-file
// corpus, gate 3 blocks NOTHING — the corpus contains no case where an
// allowlisted framework name is also declared in-tree.
//
// So gate 3's only evidence is the unit cases in
// synth_vbnet_hierarchy_6337_test.go, and its value is prospective rather than
// demonstrated: it is what makes widening the allowlist a bounded decision,
// because the collision it exists for (a repo-local `Public Class
// BooleanConverter(Of T)` shadowing the BCL type of that name) does occur in
// the corpora even though it does not currently reach this arm.
//
// Gate 3 WAS also a plain map lookup and therefore case-SENSITIVE, while
// VB.NET is not, so an in-tree `Class panel` did not block an `Inherits Panel`.
// That is fixed in round 3 and is no longer a limitation to weigh: the lookup
// folds case for VB.NET entities, and only for those. See inTreeNameSet for the
// design, the corpus measurement behind it and why the fold stops at the
// language boundary. The remaining limitation is the one stated above — gate 3
// blocks nothing on the corpus, so its value is prospective — and that is a
// reason to weigh an allowlist addition carefully, not a reason the residual is
// already safe.
//
// Gate 3 is the one the sibling arm in classifyDispositionLang shipped
// WITHOUT, at the cost described in its comment. It is repeated here rather
// than shared because internal/external has no resolve.Index.
//
// # Why this arm sits above the stdlib stop-list
//
// It is placed immediately above the language-agnostic `stdlibFunction` call
// in classifyExternal, not lower down beside the dotted-root fallback it was
// written to pre-empt. `Exception` is in vbExternalBaseTypes and is a live
// corpus base type, but it is also in the cross-language `stdlibBareNames`
// stop-list (via the Python stdlib-exceptions section). Below the stop-list,
// `Inherits Exception` produced an untagged `ext:Exception` node typed as a
// FUNCTION and shared with Python, Java and JS — the cross-ecosystem
// canonical-name collapse this arm exists to prevent, on the most
// collision-prone name in the table.
//
// The two alternatives are worse. Dropping `Exception` from the table would
// make the metric look the same while leaving the collapsed node in the graph:
// it deletes the evidence, not the defect. Language-gating `stdlibBareNames`
// would fix it for vbnet by changing classification for every ecosystem that
// map serves, including edges whose lang is empty — an unbounded blast radius
// for a VB-only problem.
//
// Moving an arm earlier does carry the risk of stealing edges from the arms it
// jumped over, so the move was made minimal and then measured: it interposes
// against exactly one call, and of the 58 names in vbExternalBaseTypes driven
// through classifyExternal as vbnet EXTENDS edges, `Exception` is the only one
// whose classification changes. Every arm above stdlibFunction keeps the
// precedence it had. The lang gate below still makes this unreachable for any
// language but vbnet.
func vbnetHierarchyExternal(name, relKind, lang string, inTreeNames inTreeNameSet) (canonical, subtype string, ok, block bool) {
	if lang != vbnetLanguage {
		return "", "", false, false
	}
	if relKind != string(types.RelationshipKindExtends) &&
		relKind != string(types.RelationshipKindImplements) {
		return "", "", false, false
	}
	typePart, member, isExternal := resolve.VBNetExternalHierarchyTarget(name)
	if !isExternal {
		// Declining is not the same as passing it on. A framework-rooted but
		// MALFORMED spelling — the shape a truncated `Inherits` clause takes —
		// would otherwise reach the generic dotted-root fallback and become
		// `ext:System`, which IsResolvedToID accepts. Block it so the misparse
		// stays a bug edge and keeps reporting (#6337 round 2).
		return "", "", false, resolve.VBNetHierarchyTargetIsMalformed(name)
	}
	// Gate 3 — an in-tree declaration of the TYPE wins, and the edge stays
	// unresolved so the ambiguity keeps being reported.
	//
	// Keyed on typePart, not on the raw spelling. For a type-level target the
	// two are the same string; for a member-level one, an in-tree entity named
	// `IFoo.Bar` can only exist when `IFoo` is itself in-tree, so a second
	// `inTreeNames[name]` test was strictly redundant. It was written, found to
	// have no killing mutant, and deleted rather than left as decoration.
	//
	// It BLOCKS rather than declines, for the same reason the malformed case
	// above does: a masked dotted target passed on to the dotted-root fallback
	// comes back as `ext:System`, and a mask guard whose output is still a
	// resolved node is not a mask guard.
	if inTreeNames.blocks(typePart) {
		return "", "", false, true
	}
	if member == "" {
		return vbnetExtNamespace + typePart, "type", true, false
	}
	// Member-level `Implements IFoo.Bar`. The `:` symbol-leaf separator is the
	// convention already used by the per-symbol named-import nodes (#4515).
	//
	// THE LEAF IS UNBOUNDED AND STAYS THAT WAY (#6337 round 4, F3). It is
	// checked for identifier SHAPE and nothing else, so `IDisposable.NotAMember`
	// mints `ext:dotnet:IDisposable:NotAMember` exactly as `IDisposable.Dispose`
	// mints the real one. That was raised as a fabrication path alongside the
	// two fixed in round 4, and the honest answer is that it is not fixable
	// here: deciding whether `NotANestedType` is a real nested type, a real
	// interface member or a typo requires a BCL MEMBER index, and this resolver
	// has neither that nor any way to derive one. The only guard available
	// without it is another shape check, which cannot separate the two cases —
	// a guard that guards nothing, and this branch has shipped three of those
	// already.
	//
	// What bounds the damage instead, and why leaving it is not the same as
	// leaving the two that were fixed:
	//
	//   - the TYPE half is still allowlist-validated and mask-guarded, so a
	//     bad leaf lands UNDER a type that was independently established as
	//     external — it cannot invent an ecosystem, only a member of a real
	//     framework type;
	//   - the node is ecosystem-tagged, so it collides with nothing outside
	//     `dotnet:`;
	//   - the two fixed cases turned a MISPARSE into a resolved node, hiding an
	//     extractor defect. A wrong member leaf is a faithful reading of source
	//     that says `Implements IDisposable.NotAMember` — which is either a
	//     compile error in the user's own code or a real member this resolver
	//     has no list of. There is no extractor bug being masked.
	//
	// So this is recorded as a known, measured-unfixable residual rather than
	// guarded. If a BCL member index ever lands, this is the call site.
	return vbnetExtNamespace + typePart + ":" + member, "member", true, false
}

// vbnetExtNamespace is the ecosystem tag. It is NOT `System` or `Windows`:
// those are bare roots that collide with a Go stdlib package and a Rust crate
// respectively inside knownExternalPackages, and an untagged canon would let a
// .NET base type resolve on their authority.
const vbnetExtNamespace = "dotnet:"

// buildInTreeNameSet collects every name a real, indexed entity carries, for
// gate 3 above. Synthesised external placeholders are excluded — they are not
// in-tree declarations, and including them would make a re-run of Synthesize
// (which must be idempotent) reject targets it accepted the first time.
//
// It is deliberately language-AGNOSTIC, mirroring resolve.Index.nameExists at
// the sibling call site. A VB.NET project inside a polyglot repo that happens
// to contain a Go type named `List` therefore keeps its `Inherits List` edge
// dangling. That is the safe direction: a visible unresolved edge is a report,
// a wrongly-synthesised one is a silence.
//
// THE EXCLUSION IS ON Kind, NOT ON SourceFile, and the difference is deliberate
// even though it is unobservable today. A review mutant (#6473, W3 in that
// table's numbering, W2 in the follow-up) added `|| e.SourceFile == ""` here
// and survived the suite — correctly, because it is an EQUIVALENT mutation on
// every document this pipeline can build: types.EntityRecord.Validate
// (types/entity.go:68) makes SourceFile required for a real entity, and the
// only entities in a Document that carry an empty one are the placeholders
// minted at synth.go:447 and :600, both already excluded by Kind. The two
// predicates coincide, so no test can tell them apart without hand-building a
// Document the indexer cannot produce — and a test that can only fail on an
// impossible input is decoration, so none was written (AGENTS.md).
//
// Kind is nonetheless the right key rather than the accidentally-equivalent
// one. SourceFile is already a poor proxy for "synthesised": SCOPE.Package,
// SCOPE.ExceptionType and SCOPE.Template all carry a CONSTANT SYNTHETIC
// SourceFile (types/kinds.go) precisely so their IDs collapse across files, so
// a SourceFile test would misclassify in both directions the moment such an
// entity carried a colliding name. And on a malformed record the safe answer is
// to treat it as in-tree and keep the edge visible, which is what Kind does.
func buildInTreeNameSet(doc *graph.Document) inTreeNameSet {
	set := inTreeNameSet{
		exact:  make(map[string]bool, len(doc.Entities)),
		vbFold: make(map[string]bool),
	}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind == KindExternal || e.Name == "" {
			continue
		}
		set.exact[e.Name] = true
		if e.Language == vbnetLanguage {
			set.vbFold[strings.ToLower(e.Name)] = true
		}
	}
	return set
}

// inTreeNameSet is gate 3's lookup, and it is TWO sets rather than one because
// exactly one of the languages involved is case-insensitive (#6337 round 3).
//
// VB.NET does not distinguish case: `Class panel` and `Inherits Panel` name the
// SAME type, and the compiler resolves the second to the first. A gate 3 built
// on a plain map lookup therefore gave a semantically WRONG answer for VB — it
// let an in-tree `panel` be shadowed by an `ext:dotnet:Panel` placeholder,
// which is precisely the partial-class ambiguity the guard exists to keep
// visible. Round 2 wrote that down as a known limitation; a review mutant then
// made the lookup case-INSENSITIVE and the whole suite stayed green, so the
// choice was unpinned in both directions at once. It is now a decision, taken
// deliberately and pinned by TestVBNetHierarchyMaskGuardFoldsVBCaseOnly_6337.
//
// THE FOLD IS VB-ONLY, and that is the whole design. The exact set stays
// language-AGNOSTIC, mirroring resolve.Index.nameExists at the sibling call
// site: a VB.NET project inside a polyglot repo that contains a Go type named
// `List` keeps its `Inherits List` edge dangling, because a visible unresolved
// edge is a report and a wrongly-synthesised one is a silence. But folding that
// agnostic set would import every OTHER language's casing into VB's semantics —
// a Go `list`, a Python `form`, a JS `label` would each start blocking a VB
// hierarchy target they have nothing to do with, in languages where `list` and
// `List` really are different types. Those lowercase spellings are common,
// the VB-only corpus cannot bound how often they collide, and suppressing a
// real external base is a recall loss that shows up as a dangling edge nobody
// can explain. So case-folding is applied where the language says it is
// correct, and nowhere else.
//
// MEASURED before choosing, over the 302-file VB.NET corpus (WakeOnLAN +
// StaxRip + display-drivers-uninstaller): 12,497 distinct in-tree names, 106
// distinct unresolved hierarchy targets, 100 of them classified by gate 2.
// Blocked by the exact set: 0 — gate 3 still fires zero times, as the review
// found. Newly blocked by the VB fold: 0. And 0 in-tree names differ from an
// allowlisted base type only by case. So this change moves nothing on the
// corpus and cannot regress the resolution number; it is taken on VB.NET's
// semantics and on making the next allowlist addition a bounded decision, not
// on a measured defect.
//
// COST is O(1) per target, not a scan. The fold index is built in the pass that
// already walks doc.Entities, so the mutant's `strings.EqualFold` loop over all
// 12,497 names — 100 targets x 12,497 comparisons on this corpus, and quadratic
// in a way that grows with the whole graph rather than with VB — is avoided.
type inTreeNameSet struct {
	// exact is every name a real indexed entity carries, in any language.
	exact map[string]bool
	// vbFold is the lowercased name of every real VB.NET entity, and only
	// those. It is what makes `Class panel` block `Inherits Panel`.
	vbFold map[string]bool
}

// blocks reports whether an in-tree declaration claims this name, under the
// case rules of the language that declared it.
func (s inTreeNameSet) blocks(name string) bool {
	return s.exact[name] || s.vbFold[strings.ToLower(name)]
}

// vbnetLanguage is the Language stamp the vbnet extractor puts on every entity
// it emits, and the same value the lang gate at the top of
// vbnetHierarchyExternal tests. Naming it once keeps the two in step: if the
// gate matched a different spelling than the fold index, the fold would be
// silently empty.
const vbnetLanguage = "vbnet"
