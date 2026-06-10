package external

import (
	"sort"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"
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

	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind == string(types.EntityKindExceptionType) {
			tn := ""
			if e.Properties != nil {
				tn = e.Properties["exception_type"]
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
	for _, sn := range synthetics {
		cands := byTypeName[sn.typeName]
		// Deduplicate candidate ids (a class can appear once; guard anyway) and
		// exclude the synthetic node itself defensively.
		uniq := uniqueExcept(cands, sn.id)
		if len(uniq) != 1 {
			// Zero real candidates -> genuinely external/unresolvable: keep the
			// synthetic node. More than one -> ambiguous leaf name: keep it
			// (precision over a possibly-wrong retarget).
			stats.SyntheticKept++
			continue
		}
		remap[sn.id] = uniq[0]
		drop[sn.id] = true
		stats.SyntheticDropped++
	}
	if len(remap) == 0 {
		return stats
	}

	// Retarget THROWS / CATCHES edges. Only these two kinds point at the
	// synthetic exception-type node, but gate on kind so an unrelated edge that
	// happens to reference the id (none today) is never silently moved.
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		if r.Kind != string(types.RelationshipKindThrows) &&
			r.Kind != string(types.RelationshipKindCatches) {
			continue
		}
		if real, ok := remap[r.ToID]; ok {
			r.ToID = real
			// Recompute the edge id so it stays the deterministic hash of its
			// (FromID, ToID, Kind) — keeping incremental diffs and de-dup stable.
			r.ID = graph.RelationshipID(r.FromID, r.ToID, r.Kind)
			stats.Retargeted++
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
