package links

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
