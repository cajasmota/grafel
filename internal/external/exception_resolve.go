package external

import (
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// exception_resolve.go — synthesis-time retarget of THROWS / CATCHES edges
// from the synthetic SCOPE.ExceptionType convergence node to the REAL
// exception class entity when one exists in the graph (#4480).
//
// PROBLEM. Every language extractor that detects a typed throw/catch emits a
// file-agnostic SCOPE.ExceptionType node (Name "exception:<Type>", SourceFile
// "<exception>") so the same type raised in one file and caught in another
// converges on a single node (see internal/extractor/exception_flow.go). But
// the SAME exception is ALSO a real entity: a `throw new NotFoundException()`
// emits a constructor CALLS edge whose target the external-synthesis pass
// materialises as `ext:NotFoundException` (imported 3rd-party class), and a
// locally-declared `class NotFound(Exception)` is an in-repo class entity.
// The graph therefore holds TWO nodes for one exception, and the THROWS edge
// lands on the synthetic one — never on the real class.
//
// FIX (long-term, language-agnostic, merge-stable). After the real class
// entities exist in the document (declared classes are present from extraction;
// imported externals are present after external.Synthesize), walk every
// SCOPE.ExceptionType node. If exactly one NON-synthetic entity shares its
// type name (a declared class/interface, or an `ext:` external class), retarget
// every THROWS / CATCHES edge from the synthetic node to that real entity and
// DROP the synthetic node. When no real entity exists (a genuinely external
// 3rd-party type that was never constructed in-repo, so no `ext:` node was
// synthesised) the synthetic node is kept — exactly one node per exception,
// never both.
//
// Merge-stable: resolution is by NAME through the fully-assembled symbol table
// at synthesis time (mirrors the endpoint→handler #4319 synthesis-time-by-id
// approach), not by any per-batch/per-file ID that could shift across
// incremental re-indexes. The synthetic node's identity (SourceFile+Kind+Name)
// and the real entity's identity are both deterministic, so the retarget is
// idempotent and stable across runs.
//
// Precision-first: if a type name is ambiguous (two distinct real classes with
// the same leaf name) the synthetic node is kept — a wrong retarget would
// mislead error-contract analysis worse than the duplicate node does.

// ExceptionResolveStats reports how the exception-resolve pass touched the doc.
type ExceptionResolveStats struct {
	// Retargeted is the number of THROWS / CATCHES edges repointed from a
	// synthetic SCOPE.ExceptionType node to the real exception class.
	Retargeted int
	// SyntheticDropped is the number of synthetic SCOPE.ExceptionType nodes
	// removed because every inbound edge was retargeted to a real class.
	SyntheticDropped int
	// SyntheticKept is the number of synthetic SCOPE.ExceptionType nodes left
	// in place because no unambiguous real class entity exists for them.
	SyntheticKept int
	// ConstructorCallsUnified is the number of constructor CALLS edges
	// (`new <Type>()` / `raise <Type>(...)` / `throw new <Type>()`) re-pointed
	// from a bare-name dangling target onto the surviving exception node, so a
	// single exception node carries BOTH the `throws` and `calls` (construction)
	// relationships (#4555).
	ConstructorCallsUnified int
}

// candidateKind reports whether kind is a real, declaration-backed entity that
// can legitimately stand in for a thrown exception type — a declared class /
// interface / struct (in-repo exception class) or a synthesised external
// placeholder (imported 3rd-party exception class). Synthetic convergence nodes
// (the SCOPE.ExceptionType node itself) and edge-only scopes are excluded.
func candidateKind(kind string) bool {
	switch kind {
	case KindExternal,
		string(types.EntityKindClass),
		string(types.EntityKindComponent):
		// SCOPE.Class covers declared classes/interfaces/structs across
		// languages; SCOPE.Component is the file/module-level fallback some
		// extractors use for a declared type; SCOPE.External is the
		// synthesised placeholder for an imported 3rd-party exception class.
		return true
	}
	return false
}

// ResolveExceptionTypes retargets THROWS / CATCHES edges to the real exception
// class and drops the now-redundant synthetic SCOPE.ExceptionType node, for
// every exception type that resolves to exactly one real entity. It is
// idempotent and safe on a nil/empty document. MUST run AFTER external.Synthesize
// so imported (`ext:`) exception classes are present.
func ResolveExceptionTypes(doc *graph.Document) ExceptionResolveStats {
	var stats ExceptionResolveStats
	if doc == nil || len(doc.Entities) == 0 {
		return stats
	}

	// Index every synthetic exception-type node by its type name, and collect
	// real-class candidates by bare leaf name. The type name lives in
	// Properties["exception_type"] (set by ExceptionTypeEntity); fall back to
	// stripping the "exception:" Name prefix for robustness against older
	// graphs carried forward by incremental merge.
	type synthNode struct {
		id       string // synthetic node's graph id
		typeName string
	}
	var synthetics []synthNode
	byTypeName := map[string][]string{} // typeName -> candidate real entity ids

	// allEntityIDs lets the constructor-CALLS unification (#4555) tell a real
	// graph entity from a bare-name DANGLING CALLS target. A `new <Type>()`
	// constructor call emits a CALLS edge whose ToID is the bare type name; when
	// no ext:<Type> / declared-class entity was synthesised for it, that ToID
	// resolves to NO entity and is rendered as a phantom node alongside the
	// exception node — the second half of the duplicate this pass removes.
	allEntityIDs := make(map[string]bool, len(doc.Entities))
	for i := range doc.Entities {
		allEntityIDs[doc.Entities[i].ID] = true
	}

	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind == string(types.EntityKindExceptionType) {
			tn := ""
			if e.PropLen() > 0 {
				tn = e.PropGet("exception_type")
			}
			if tn == "" {
				tn = stripExceptionPrefix(e.Name)
			}
			if tn != "" {
				synthetics = append(synthetics, synthNode{id: e.ID, typeName: tn})
			}
			continue
		}
		if candidateKind(e.Kind) && e.Name != "" {
			byTypeName[e.Name] = append(byTypeName[e.Name], e.ID)
		}
	}
	if len(synthetics) == 0 {
		return stats
	}

	// For each synthetic node decide its real target (if unambiguous). Build a
	// remap from synthetic-id -> real-id and the set of synthetic ids to drop.
	remap := map[string]string{} // synthetic id -> real entity id
	drop := map[string]bool{}    // synthetic ids to remove
	// survivorByExc maps every exception-node id to the id that SURVIVES this
	// pass: the real class entity when the node is resolved+dropped, otherwise
	// the exception node itself (kept). typeNameByExc carries the bare type name
	// for the constructor-CALLS unification (#4555) leaf-name match.
	survivorByExc := make(map[string]string, len(synthetics))
	typeNameByExc := make(map[string]string, len(synthetics))
	for _, sn := range synthetics {
		typeNameByExc[sn.id] = sn.typeName
		cands := byTypeName[sn.typeName]
		// Deduplicate candidate ids (a class can appear once; guard anyway) and
		// exclude the synthetic node itself defensively.
		uniq := uniqueExcept(cands, sn.id)
		if len(uniq) != 1 {
			// Zero real candidates -> genuinely external/unresolvable: keep the
			// synthetic node. More than one -> ambiguous leaf name: keep it
			// (precision over a possibly-wrong retarget). The kept node is the
			// survivor that constructor CALLS edges fold onto.
			survivorByExc[sn.id] = sn.id
			stats.SyntheticKept++
			continue
		}
		remap[sn.id] = uniq[0]
		survivorByExc[sn.id] = uniq[0]
		drop[sn.id] = true
		stats.SyntheticDropped++
	}

	// Retarget THROWS / CATCHES edges, AND record which methods THROW each
	// exception node. The throwing-method set is captured from the THROWS edge's
	// ORIGINAL ToID (still the exception-node id at this point) so the
	// constructor-CALLS unification below can require the `new <Type>()` call to
	// originate from a method that also throws that very exception — never
	// folding an unrelated same-named construction onto the exception node.
	//
	// throwsFrom: exception-node-id -> set of throwing-method FromIDs.
	throwsFrom := map[string]map[string]bool{}
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		if r.Kind != string(types.RelationshipKindThrows) &&
			r.Kind != string(types.RelationshipKindCatches) {
			continue
		}
		if r.Kind == string(types.RelationshipKindThrows) {
			if _, isExc := survivorByExc[r.ToID]; isExc && r.FromID != "" {
				if throwsFrom[r.ToID] == nil {
					throwsFrom[r.ToID] = map[string]bool{}
				}
				throwsFrom[r.ToID][r.FromID] = true
			}
		}
		if real, ok := remap[r.ToID]; ok {
			r.ToID = real
			// Recompute the edge id so it stays the deterministic hash of its
			// (FromID, ToID, Kind) — keeping incremental diffs and de-dup stable.
			r.ID = graph.RelationshipID(r.FromID, r.ToID, r.Kind)
			stats.Retargeted++
		}
	}

	// #4555 — constructor-CALLS unification. A `throw new <Type>()` (Java/C#/JS),
	// `raise <Type>(...)` (Python), etc. emits, INDEPENDENTLY of throws-analysis,
	// a CALLS edge to the constructor. When that target was never materialised as
	// a real entity (no ext:<Type> placeholder, no declared class) it dangles as
	// a bare-name ToID == <Type> and the dashboard renders it as a SECOND phantom
	// node next to the exception node — one exception, two nodes.
	//
	// Fold the construction onto the surviving exception node so a SINGLE node
	// carries both the `throws` and the `calls` (construction) relationships.
	// Precision gates, all required: the CALLS target must (a) be a bare name
	// resolving to NO entity (a genuinely dangling stub, not a real class call),
	// (b) equal the simple name of an exception node, and (c) originate from a
	// method that THROWS that same exception node. This is language-general:
	// the bare-name dangling-constructor shape is identical across extractors.
	if len(throwsFrom) > 0 {
		// excByTypeName: bare type name -> set of exception-node survivor targets
		// whose throwing-method set we can consult. Multiple distinct exception
		// nodes can share a leaf name across files; we only fold when exactly one
		// of them is thrown by the calling method (the (c) gate disambiguates).
		excIDsByName := map[string][]string{}
		for id, tn := range typeNameByExc {
			excIDsByName[tn] = append(excIDsByName[tn], id)
		}
		for k := range doc.Relationships {
			r := &doc.Relationships[k]
			if r.Kind != string(types.RelationshipKindCalls) || r.FromID == "" {
				continue
			}
			// (a) dangling bare-name target — not a graph entity, not an
			// already-synthesised ext:* placeholder, not a hex id.
			if r.ToID == "" || allEntityIDs[r.ToID] ||
				isHexID(r.ToID) || strings.HasPrefix(r.ToID, ExtIDPrefix) {
				continue
			}
			// (b)+(c) the bare name matches an exception node thrown by THIS method.
			var target string
			matches := 0
			for _, excID := range excIDsByName[r.ToID] {
				if throwsFrom[excID] != nil && throwsFrom[excID][r.FromID] {
					target = survivorByExc[excID]
					matches++
				}
			}
			if matches != 1 || target == "" || target == r.ToID {
				// No match, or ambiguous (same leaf name thrown by the method for
				// two distinct exception nodes) — leave the edge untouched.
				continue
			}
			r.ToID = target
			r.ID = graph.RelationshipID(r.FromID, r.ToID, r.Kind)
			stats.ConstructorCallsUnified++
		}
	}

	// Drop the now-redundant synthetic nodes, preserving order.
	if len(drop) > 0 {
		kept := doc.Entities[:0]
		for i := range doc.Entities {
			if drop[doc.Entities[i].ID] {
				continue
			}
			kept = append(kept, doc.Entities[i])
		}
		doc.Entities = kept
	}

	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)
	return stats
}

// stripExceptionPrefix returns the bare type name from an "exception:<Type>"
// node Name, or the input unchanged when the prefix is absent.
func stripExceptionPrefix(name string) string {
	const p = "exception:"
	if len(name) > len(p) && name[:len(p)] == p {
		return name[len(p):]
	}
	return name
}

// uniqueExcept returns the de-duplicated ids in s, excluding except, in stable
// sorted order so the chosen target is deterministic across runs.
func uniqueExcept(s []string, except string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(s))
	for _, v := range s {
		if v == "" || v == except || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// compile-time reference so the extractor package's exception constants remain
// the single source of truth for the node Name / target-id shapes this pass
// reverses. (No runtime cost.)
var _ = extractor.ExceptionTypeName
