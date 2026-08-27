package main

// incremental_local_scope_compat.go — #6472 backward compatibility for graphs
// written before the React props parameter carried Properties["local_scope"].
//
// THE PROBLEM THIS SOLVES
//
// internal/resolve/refs.go's isLocalBindingKind used to infer that a React
// props parameter is a callable-local from a hardcoded
// `subtype == "component_prop" && props["framework"] == "react"` clause,
// because internal/extractors/javascript/dataflow_react.go did not stamp
// local_scope at all. #6472 stamped the emitter and deleted the clause, so the
// predicate now reads one key and nothing else.
//
// That is correct for records produced by THIS binary. It is wrong for records
// produced by an older one. Path B's incremental reindex (index.go, the
// `cf = append(cf, …)` loop) carries the previous graph's UNCHANGED-file
// entities forward into the resolver index, preserving Subtype and
// PropsSnapshot() verbatim. Immediately after a user upgrades, those carried
// records are exactly `component_prop` + `framework=react` + NO local_scope —
// and the new predicate calls them addressable. The #6467 regression (a props
// parameter capturing the repository-wide byName slot, so an unrelated file's
// import of the same name binds to it instead of reaching the external
// library) would silently return until the next FULL reindex.
//
// WHY THE FIX LIVES HERE AND NOT IN THE PREDICATE
//
// The alternative was to keep a compatibility arm inside isLocalBindingKind.
// That would have left the framework-name check — the weakest usable signal,
// and the thing #6472 exists to remove — permanently in the resolver's
// contract, where it also governs freshly-extracted records that can never
// need it.
//
// Putting it here is provably complete instead of merely broad. There is
// exactly ONE production construction of the resolver index
// (index.go: resolve.BuildIndexFromModulesOrdered / resolve.BuildIndex, fed by
// `indexEntities`), and `indexEntities` is `merged` plus
// `incrementalCarryForwardEntities`. `merged` is this run's freshly extracted
// records, which carry the stamp from the emitter. So the carry-forward slice
// is the only way a pre-#6472 record can reach the predicate, and stamping it
// on the way in closes the whole surface.
//
// It is also self-healing and self-limiting: the moment a file is re-extracted
// its props get the real stamp from the emitter, and a full reindex retires the
// shim's effect entirely for that repo. It can be deleted once graphs written
// before #6472 are no longer expected in the wild.
//
// The rule below is deliberately IDENTICAL to the deleted clause, so a carried
// record resolves exactly as it did on the old binary — no more (angular's
// @Input() and vue's defineProps carry a different framework and are left
// alone, which is what keeps them addressable) and no less.

// needsLegacyLocalScopeStamp reports whether a carried-forward previous-graph
// record predates the #6472 emitter stamp and must have it applied
// retroactively.
//
// subtype is read with the same two-carrier fallback internal/mcp/denoise.go
// uses (#2015): the extractor writes the subtype into both EntityRecord.Subtype
// and Properties["subtype"], and load/conversion paths do not consistently
// repopulate both. A carried record has been through a persist/load round-trip
// by definition, so reading only one carrier here is the same latent hole.
func needsLegacyLocalScopeStamp(subtype string, props map[string]string) bool {
	// Already stamped — written by a post-#6472 binary. Nothing to do.
	if props["local_scope"] == "true" {
		return false
	}
	if subtype == "" {
		subtype = props["subtype"]
	}
	return subtype == "component_prop" && props["framework"] == "react"
}

// applyLegacyLocalScopeStamp returns props with the #6472 stamp added when the
// record needs it. The input map is a PropsSnapshot (an independent copy), so
// mutating it does not touch the previous graph.
func applyLegacyLocalScopeStamp(subtype string, props map[string]string) map[string]string {
	if !needsLegacyLocalScopeStamp(subtype, props) {
		return props
	}
	if props == nil {
		props = make(map[string]string, 1)
	}
	props["local_scope"] = "true"
	return props
}
