package links

import "strings"

// isBareNameExt reports whether id is a bare-name external placeholder
// of the form "ext:<name>" with no module qualifier (no second ":" after
// the prefix). These placeholders are emitted when the extractor sees an
// unresolved bare identifier (e.g. `[].filter()`, `array.split(...)`) and
// the external synthesiser has no module path to attach. Two repos that
// each independently call `[].filter()` therefore both end up pointing
// edges at the same `ext:filter` placeholder, which is NOT a real
// cross-repo reference — it is each repo's own use of a built-in.
//
// Qualified placeholders of the form "ext:<module>:<name>" carry real
// module identity (e.g. `ext:react:useState`) and remain eligible for
// cross-repo linking.
//
// Issue #509: bare-name ext:* matches produced 100% false-positive
// cross-repo links on the client-fixture group (1,114 of 1,114). Filtering
// these out is the precision fix.
func isBareNameExt(id string) bool {
	const prefix = "ext:"
	if !strings.HasPrefix(id, prefix) {
		return false
	}
	rest := id[len(prefix):]
	if rest == "" {
		return true // pathological "ext:" — also bare/empty.
	}
	return !strings.Contains(rest, ":")
}

// runImportPass implements P1: structural cross-repo imports/calls edges.
//
// Idempotent overwrite: every link previously emitted with method=import is
// replaced; entries from other passes survive untouched. Confidence comes
// from ScoreImport (structural — top of the band).
//
// Pair iteration: O(E) over edges, with explicit self-pair skipping and
// per-(source,target,method) dedupe so a graph that mentions the same
// edge twice (e.g. two extractor passes touching the same call site)
// emits exactly one link.
func runImportPass(graphs []repoGraph, paths Paths, rejects map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "import"}

	// Build entity-id → repo map across the whole group. O(N) where N is
	// total entities; replaces what would otherwise be an O(repos × edges)
	// lookup if we re-scanned every repo per edge.
	entRepo := map[string]string{}
	for _, g := range graphs {
		for _, e := range g.Entities {
			// First write wins; structural ids are stable & unique per
			// (repo, kind, name, file) so collision across repos is
			// already disambiguated by the per-repo seed.
			entRepo[e.ID] = g.Repo
		}
	}

	now := discoveredAt()
	var fresh []Link
	emitted := map[string]bool{} // dedupe by link id
	for _, g := range graphs {
		for _, edge := range g.Edges {
			rel := normalizedRelation(edge.Kind)
			if rel != RelationImports && rel != RelationCalls {
				continue
			}
			fromRepo := entRepo[edge.FromID]
			toRepo := entRepo[edge.ToID]
			if fromRepo == "" || toRepo == "" {
				continue
			}
			if fromRepo == toRepo {
				// Self-pair: not a cross-repo edge.
				continue
			}
			// Issue #509: bare-name ext:* placeholders (e.g. "ext:filter",
			// "ext:split") are each repo's own unresolved use of a built-in
			// or stdlib bare identifier. Two repos pointing at the same
			// placeholder ID does NOT mean they share a real symbol — only
			// qualified "ext:<module>:<name>" forms carry that guarantee.
			// Skip either side being a bare-name ext: placeholder.
			if isBareNameExt(edge.FromID) || isBareNameExt(edge.ToID) {
				continue
			}
			source := entityKey(fromRepo, edge.FromID)
			target := entityKey(toRepo, edge.ToID)
			id := MakeID(source, target, MethodImport)
			if emitted[id] {
				continue
			}
			emitted[id] = true
			fresh = append(fresh, Link{
				ID:           id,
				Source:       source,
				Target:       target,
				Relation:     rel,
				Method:       MethodImport,
				Confidence:   ScoreImport(),
				Channel:      nil,
				Identifier:   nil,
				DiscoveredAt: now,
			})
		}
	}

	added, skipped, err := replaceByMethod(paths.Links, newMethodSet(MethodImport), fresh, rejects)
	if err != nil {
		return res, err
	}
	res.LinksAdded = added
	res.Skipped = skipped
	return res, nil
}

// normalizedRelation maps a graph relationship Kind to one of the
// canonical relation values used in links.json. Accepts upper- or
// lowercase forms (extractors emit either).
func normalizedRelation(kind string) string {
	switch kind {
	case "imports", "IMPORTS", "import", "IMPORT":
		return RelationImports
	case "calls", "CALLS", "call", "CALL":
		return RelationCalls
	}
	return ""
}
