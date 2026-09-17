package dashboard

import (
	"sort"
	"strings"
)

const (
	repositoryTopologyMaxNodes         = 500
	repositoryTopologyMaxEdges         = 2_000
	repositoryTopologyMaxSearchRecords = 100_000
	repositoryTopologyDetailPageSize   = 100
)

func relationshipMatchesQuery(rel repositoryRelationship, query repositoryTopologyQuery) bool {
	if len(query.Channels) > 0 && !containsRepositoryChannel(query.Channels, rel.Channel) {
		return false
	}
	if len(query.Evidence) > 0 && !containsRepositoryEvidence(query.Evidence, rel.Evidence) {
		return false
	}
	if len(query.Repos) > 0 && (!containsRepositoryString(query.Repos, rel.SourceRepo) || !containsRepositoryString(query.Repos, rel.TargetRepo)) {
		return false
	}
	if query.Search != "" && !strings.Contains(rel.SearchText, strings.ToLower(strings.TrimSpace(query.Search))) {
		return false
	}
	return rel.TargetRepo != "" && rel.SourceRepo != rel.TargetRepo
}

func aggregateFilteredRelationships(index repositoryTopologyIndex, query repositoryTopologyQuery) []repositoryTopologyEdge {
	type accumulator struct {
		key       repositoryEdgeKey
		contracts map[string]struct{}
		evidence  repositoryEvidenceCounts
		samples   []repositoryTopologySample
		count     int
	}
	byKey := make(map[repositoryEdgeKey]*accumulator)
	work := 0
	for _, relationship := range index.Relationships {
		work++
		if work > repositoryTopologyMaxSearchRecords && query.Search != "" {
			break
		}
		if !relationshipMatchesQuery(relationship, query) {
			continue
		}
		key := repositoryEdgeKey{Source: relationship.SourceRepo, Target: relationship.TargetRepo, Channel: relationship.Channel}
		item := byKey[key]
		if item == nil {
			item = &accumulator{key: key, contracts: map[string]struct{}{}}
			byKey[key] = item
		}
		item.count++
		contract := relationship.Identifier
		if contract == "" {
			contract = relationship.Label
		}
		if contract != "" {
			item.contracts[contract] = struct{}{}
		}
		incrementRepositoryEvidence(&item.evidence, relationship.Evidence, 1)
		item.samples = append(item.samples, repositoryTopologySample{ID: relationship.ID, Label: relationship.Label, Identifier: relationship.Identifier, Evidence: relationship.Evidence})
	}
	keys := make([]repositoryEdgeKey, 0, len(byKey))
	for key, item := range byKey {
		if item.count >= query.MinCount {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return repositoryEdgeKeyLess(keys[i], keys[j]) })
	edges := make([]repositoryTopologyEdge, 0, len(keys))
	for _, key := range keys {
		item := byKey[key]
		sort.Slice(item.samples, func(i, j int) bool {
			if item.samples[i].Label != item.samples[j].Label {
				return item.samples[i].Label < item.samples[j].Label
			}
			return item.samples[i].ID < item.samples[j].ID
		})
		samples := item.samples
		if len(samples) > repositoryTopologyInlineSampleLimit {
			samples = samples[:repositoryTopologyInlineSampleLimit]
		}
		edges = append(edges, repositoryTopologyEdge{
			ID: repositoryTopologyEdgeID(key), Source: key.Source, Target: key.Target, Channel: key.Channel,
			RelationshipCount: item.count, ContractCount: len(item.contracts), Evidence: item.evidence,
			Labels: repositoryTopologySampleLabels(samples), Samples: append([]repositoryTopologySample(nil), samples...),
			HasMore: len(item.samples) > len(samples),
		})
	}
	return edges
}

func repositoryNeighborhood(edges []repositoryTopologyEdge, focus string, direction repositoryDirection, depth int) map[string]struct{} {
	selected := map[string]struct{}{focus: {}}
	frontier := []string{focus}
	for hop := 0; hop < depth && len(frontier) > 0; hop++ {
		nextSet := map[string]struct{}{}
		for _, current := range frontier {
			for _, edge := range edges {
				if (direction == directionOutbound || direction == directionBoth) && edge.Source == current {
					if _, seen := selected[edge.Target]; !seen {
						nextSet[edge.Target] = struct{}{}
					}
				}
				if (direction == directionInbound || direction == directionBoth) && edge.Target == current {
					if _, seen := selected[edge.Source]; !seen {
						nextSet[edge.Source] = struct{}{}
					}
				}
			}
		}
		frontier = frontier[:0]
		for repo := range nextSet {
			selected[repo] = struct{}{}
			frontier = append(frontier, repo)
		}
		sort.Strings(frontier)
	}
	return selected
}

func shortestRepositoryPath(edges []repositoryTopologyEdge, source, target string) ([]string, []repositoryTopologyEdge, bool) {
	if source == target {
		return []string{source}, []repositoryTopologyEdge{}, true
	}
	outgoing := make(map[string][]repositoryTopologyEdge)
	for _, edge := range edges {
		outgoing[edge.Source] = append(outgoing[edge.Source], edge)
	}
	for repo := range outgoing {
		sort.Slice(outgoing[repo], func(i, j int) bool {
			if outgoing[repo][i].Target != outgoing[repo][j].Target {
				return outgoing[repo][i].Target < outgoing[repo][j].Target
			}
			return outgoing[repo][i].Channel < outgoing[repo][j].Channel
		})
	}
	queue := []string{source}
	seen := map[string]struct{}{source: {}}
	parent := make(map[string]string)
	parentEdge := make(map[string]repositoryTopologyEdge)
	found := false
	for len(queue) > 0 && !found {
		current := queue[0]
		queue = queue[1:]
		for _, edge := range outgoing[current] {
			if _, ok := seen[edge.Target]; ok {
				continue
			}
			seen[edge.Target] = struct{}{}
			parent[edge.Target] = current
			parentEdge[edge.Target] = edge
			if edge.Target == target {
				found = true
				break
			}
			queue = append(queue, edge.Target)
		}
	}
	if !found {
		return []string{}, []repositoryTopologyEdge{}, false
	}
	path := []string{target}
	pathEdges := make([]repositoryTopologyEdge, 0)
	for current := target; current != source; {
		pathEdges = append(pathEdges, parentEdge[current])
		current = parent[current]
		path = append(path, current)
	}
	reverseRepositoryStrings(path)
	reverseRepositoryEdges(pathEdges)
	return path, pathEdges, true
}

func filterRepositoryTopology(index repositoryTopologyIndex, query repositoryTopologyQuery) repositoryTopologyResponse {
	if query.MinCount < 1 {
		query.MinCount = 1
	}
	if query.Depth < 1 {
		query.Depth = 1
	}
	edges := aggregateFilteredRelationships(index, query)
	path := []string{}
	pathFound := false
	keepSinglePathNode := ""

	if query.Source != "" && query.Target != "" {
		var found bool
		path, edges, found = shortestRepositoryPath(edges, query.Source, query.Target)
		pathFound = found
		if !found {
			edges = []repositoryTopologyEdge{}
		} else if len(path) == 1 {
			keepSinglePathNode = path[0]
		}
	} else if query.Focus != "" {
		selected := repositoryNeighborhood(edges, query.Focus, query.Direction, query.Depth)
		edges = filterRepositoryEdgesByNodes(edges, selected)
	}

	preLimitEdges := len(edges)
	if len(edges) > repositoryTopologyMaxEdges {
		edges = append([]repositoryTopologyEdge(nil), edges[:repositoryTopologyMaxEdges]...)
	}
	nodes := buildFilteredRepositoryNodes(index.Repositories, edges)
	if keepSinglePathNode != "" {
		if repo, ok := index.Repositories[keepSinglePathNode]; ok {
			nodes = []repositoryTopologyNode{repositoryTopologyNodeFromRepository(repo)}
		}
	}
	preLimitNodes := len(nodes)
	truncated := preLimitEdges > len(edges)
	if len(nodes) > repositoryTopologyMaxNodes {
		nodes = append([]repositoryTopologyNode(nil), nodes[:repositoryTopologyMaxNodes]...)
		selected := make(map[string]struct{}, len(nodes))
		for _, node := range nodes {
			selected[node.ID] = struct{}{}
		}
		edges = filterRepositoryEdgesByNodes(edges, selected)
		truncated = true
	}
	return repositoryTopologyResponse{
		Nodes: nodes, Edges: edges, Facets: buildRepositoryTopologyFacets(nodes, edges),
		Summary: repositoryTopologySummary{
			RepositoryCount: len(nodes), EdgeCount: len(edges), RelationshipCount: repositoryTopologyRelationshipTotal(edges),
			PreLimitNodes: preLimitNodes, PreLimitEdges: preLimitEdges,
		},
		Limits: repositoryTopologyLimits{
			MaxNodes: repositoryTopologyMaxNodes, MaxEdges: repositoryTopologyMaxEdges,
			InlineSamples: repositoryTopologyInlineSampleLimit, DetailPageSize: repositoryTopologyDetailPageSize,
			MaxSearchRecords: repositoryTopologyMaxSearchRecords,
		},
		Path: path, PathFound: pathFound, Truncated: truncated,
	}
}

func buildFilteredRepositoryNodes(repositories map[string]repositoryTopologyRepository, edges []repositoryTopologyEdge) []repositoryTopologyNode {
	type metrics struct {
		inbound, outbound int
		connected         map[string]struct{}
		evidence          repositoryEvidenceCounts
	}
	byRepo := make(map[string]*metrics)
	for _, edge := range edges {
		if byRepo[edge.Source] == nil {
			byRepo[edge.Source] = &metrics{connected: map[string]struct{}{}}
		}
		if byRepo[edge.Target] == nil {
			byRepo[edge.Target] = &metrics{connected: map[string]struct{}{}}
		}
		byRepo[edge.Source].outbound += edge.RelationshipCount
		byRepo[edge.Target].inbound += edge.RelationshipCount
		byRepo[edge.Source].connected[edge.Target] = struct{}{}
		byRepo[edge.Target].connected[edge.Source] = struct{}{}
		addRepositoryEvidenceCounts(&byRepo[edge.Source].evidence, edge.Evidence)
		addRepositoryEvidenceCounts(&byRepo[edge.Target].evidence, edge.Evidence)
	}
	slugs := make([]string, 0, len(byRepo))
	for slug := range byRepo {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	nodes := make([]repositoryTopologyNode, 0, len(slugs))
	for _, slug := range slugs {
		repo, ok := repositories[slug]
		if !ok {
			continue
		}
		node := repositoryTopologyNodeFromRepository(repo)
		node.InboundRelationships = byRepo[slug].inbound
		node.OutboundRelationships = byRepo[slug].outbound
		node.ConnectedRepositories = len(byRepo[slug].connected)
		node.Evidence = byRepo[slug].evidence
		nodes = append(nodes, node)
	}
	return nodes
}

func repositoryTopologyNodeFromRepository(repo repositoryTopologyRepository) repositoryTopologyNode {
	return repositoryTopologyNode{
		ID: repo.Slug, Repository: repo.Slug, Label: repo.Slug, PrimaryLanguage: repo.PrimaryLanguage,
		EntityCount: repo.EntityCount, ModuleCount: repo.ModuleCount, GraphState: repo.GraphState,
		Modules: append([]string(nil), repo.Modules...),
	}
}

func buildRepositoryTopologyFacets(nodes []repositoryTopologyNode, edges []repositoryTopologyEdge) repositoryTopologyFacets {
	channelCounts := map[string]int{}
	repositoryCounts := map[string]int{}
	evidenceCounts := map[string]int{}
	for _, edge := range edges {
		channelCounts[string(edge.Channel)] += edge.RelationshipCount
		repositoryCounts[edge.Source] += edge.RelationshipCount
		repositoryCounts[edge.Target] += edge.RelationshipCount
		evidenceCounts[string(evidenceConfirmed)] += edge.Evidence.Confirmed
		evidenceCounts[string(evidenceInferred)] += edge.Evidence.Inferred
		evidenceCounts[string(evidenceDangling)] += edge.Evidence.Dangling
		evidenceCounts[string(evidenceAmbiguous)] += edge.Evidence.Ambiguous
		evidenceCounts[string(evidenceExternal)] += edge.Evidence.External
	}
	return repositoryTopologyFacets{
		Channels: repositoryFacetSlice(channelCounts), Repositories: repositoryFacetSlice(repositoryCounts), Evidence: repositoryFacetSlice(evidenceCounts),
	}
}

func repositoryFacetSlice(counts map[string]int) []repositoryTopologyFacet {
	values := make([]string, 0, len(counts))
	for value, count := range counts {
		if count > 0 {
			values = append(values, value)
		}
	}
	sort.Strings(values)
	facets := make([]repositoryTopologyFacet, 0, len(values))
	for _, value := range values {
		facets = append(facets, repositoryTopologyFacet{Value: value, Count: counts[value]})
	}
	return facets
}

func filterRepositoryEdgesByNodes(edges []repositoryTopologyEdge, nodes map[string]struct{}) []repositoryTopologyEdge {
	filtered := make([]repositoryTopologyEdge, 0, len(edges))
	for _, edge := range edges {
		_, sourceOK := nodes[edge.Source]
		_, targetOK := nodes[edge.Target]
		if sourceOK && targetOK {
			filtered = append(filtered, edge)
		}
	}
	return filtered
}

func repositoryTopologyRelationshipTotal(edges []repositoryTopologyEdge) int {
	total := 0
	for _, edge := range edges {
		total += edge.RelationshipCount
	}
	return total
}

func addRepositoryEvidenceCounts(target *repositoryEvidenceCounts, source repositoryEvidenceCounts) {
	target.Confirmed += source.Confirmed
	target.Inferred += source.Inferred
	target.Dangling += source.Dangling
	target.Ambiguous += source.Ambiguous
	target.External += source.External
}

func containsRepositoryChannel(values []repositoryChannel, want repositoryChannel) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsRepositoryEvidence(values []repositoryEvidence, want repositoryEvidence) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsRepositoryString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func reverseRepositoryStrings(values []string) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reverseRepositoryEdges(values []repositoryTopologyEdge) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
