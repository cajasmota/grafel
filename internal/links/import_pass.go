package links

// runImportPass implements P1: structural cross-repo imports/calls edges.
//
// Idempotent overwrite: every link previously emitted with method=import is
// replaced; entries from other passes survive untouched. Confidence is
// hard-coded to 1.0 (structural) and channel is nil.
func runImportPass(graphs []repoGraph, paths Paths, rejects map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "import"}

	// Build entity-id → repo map across the whole group.
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
				continue
			}
			source := entityKey(fromRepo, edge.FromID)
			target := entityKey(toRepo, edge.ToID)
			fresh = append(fresh, Link{
				ID:           MakeID(source, target, MethodImport),
				Source:       source,
				Target:       target,
				Relation:     rel,
				Method:       MethodImport,
				Confidence:   1.0,
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
