package external

import (
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// #6337 — external synthesis for VB.NET EXTENDS / IMPLEMENTS targets.
//
// Every other arm of classifyExternal is gated on a relKind of IMPORTS or
// CALLS, or on a structural-ref stub prefix, so a hierarchy target had exactly
// one path available to it: the generic dotted-root fallback. Measured on the
// 302-file VB.NET corpus (WakeOnLAN + StaxRip + display-drivers-uninstaller)
// that path leaves 206 of 494 hierarchy edges as raw stubs, which
// internal/feedback counts as bug-extractor, and folds the 75 it does resolve
// into a single `ext:System` node — plus one `ext:Windows`, which resolves on
// the authority of the RUST `windows` crate entry in knownExternalPackages.
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
//  2. THE NAME MUST BE A .NET FRAMEWORK TYPE. resolve.VBNetExternalHierarchyTarget
//     consults a curated, corpus-derived table that is pinned in both
//     directions by TestVBExternalBaseTypesAreLoadBearing. A typo (`Fomr`), a
//     misparsed clause, or a genuinely-missing in-tree base is not in it and
//     stays a bug edge — which is the whole point: an `ext:` node for a typo
//     is noise that hides a real extractor defect.
//
//  3. NO IN-TREE ENTITY MAY CARRY THE NAME. inTreeNames is built from
//     doc.Entities in Synthesize. A WinForms application that declares its own
//     partial `Panel`, `Label` or `Form` keeps the unresolved edge, because
//     that ambiguity is the partial-class defect and not an external base.
//     For a member-level clause the guard is applied to the TYPE part, which
//     is what keeps the 16 measured in-tree `IFrameServer.*` member misses
//     visible: `IFrameServer` is an in-tree interface, so those edges stay
//     dangling and keep reporting the resolver-routing defect they represent.
//
// Gate 3 is the load-bearing one and it is the one the sibling arm in
// classifyDispositionLang shipped WITHOUT, at the cost described in its
// comment. It is repeated here rather than shared because internal/external
// has no resolve.Index.
func vbnetHierarchyExternal(name, relKind, lang string, inTreeNames map[string]bool) (canonical, subtype string, ok bool) {
	if lang != "vbnet" {
		return "", "", false
	}
	if relKind != string(types.RelationshipKindExtends) &&
		relKind != string(types.RelationshipKindImplements) {
		return "", "", false
	}
	typePart, member, isExternal := resolve.VBNetExternalHierarchyTarget(name)
	if !isExternal {
		return "", "", false
	}
	// Gate 3 — an in-tree declaration of the TYPE wins, and the edge stays
	// unresolved so the ambiguity keeps being reported.
	//
	// Keyed on typePart, not on the raw spelling. For a type-level target the
	// two are the same string; for a member-level one, an in-tree entity named
	// `IFoo.Bar` can only exist when `IFoo` is itself in-tree, so a second
	// `inTreeNames[name]` test was strictly redundant. It was written, found to
	// have no killing mutant, and deleted rather than left as decoration.
	if inTreeNames[typePart] {
		return "", "", false
	}
	if member == "" {
		return vbnetExtNamespace + typePart, "type", true
	}
	// Member-level `Implements IFoo.Bar`. The `:` symbol-leaf separator is the
	// convention already used by the per-symbol named-import nodes (#4515).
	return vbnetExtNamespace + typePart + ":" + member, "member", true
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
func buildInTreeNameSet(doc *graph.Document) map[string]bool {
	names := make(map[string]bool, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind == KindExternal || e.Name == "" {
			continue
		}
		names[e.Name] = true
	}
	return names
}
