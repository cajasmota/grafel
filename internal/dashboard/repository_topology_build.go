package dashboard

import (
	"sort"
	"strings"
)

const repositoryTopologyInlineSampleLimit = 5

type repositoryEdgeKey struct {
	Source  string
	Target  string
	Channel repositoryChannel
}

type repositoryTopologyRepository struct {
	Slug            string
	PrimaryLanguage string
	EntityCount     int
	ModuleCount     int
	GraphState      string
	Modules         []string
}

type repositoryTopologyIndex struct {
	Repositories    map[string]repositoryTopologyRepository
	Relationships   []repositoryRelationship
	Edges           []repositoryTopologyEdge
	EdgeByKey       map[repositoryEdgeKey]repositoryTopologyEdge
	RelationshipIDs map[repositoryEdgeKey][]int
	Channels        []repositoryChannel
}

type repositoryEdgeAccumulator struct {
	key           repositoryEdgeKey
	relationships []int
	contracts     map[string]struct{}
	evidence      repositoryEvidenceCounts
	samples       []repositoryTopologySample
}

func buildRepositoryTopologyIndex(grp *DashGroup) repositoryTopologyIndex {
	index := repositoryTopologyIndex{
		Repositories:    make(map[string]repositoryTopologyRepository, len(grp.Repos)),
		Relationships:   make([]repositoryRelationship, 0, len(grp.Links)),
		Edges:           []repositoryTopologyEdge{},
		EdgeByKey:       make(map[repositoryEdgeKey]repositoryTopologyEdge),
		RelationshipIDs: make(map[repositoryEdgeKey][]int),
		Channels:        []repositoryChannel{},
	}
	knownRepos := make(map[string]struct{}, len(grp.Repos))
	for slug, repo := range grp.Repos {
		knownRepos[slug] = struct{}{}
		index.Repositories[slug] = repositoryTopologyRepositoryFromDashRepo(slug, repo)
	}

	accumulators := make(map[repositoryEdgeKey]*repositoryEdgeAccumulator)
	channelSet := make(map[repositoryChannel]struct{})
	for _, link := range grp.Links {
		relationship, ok := normalizeRepositoryRelationship(link, knownRepos)
		if !ok {
			continue
		}
		relationshipIndex := len(index.Relationships)
		index.Relationships = append(index.Relationships, relationship)
		if relationship.TargetRepo == "" || relationship.SourceRepo == relationship.TargetRepo {
			continue
		}
		key := repositoryEdgeKey{Source: relationship.SourceRepo, Target: relationship.TargetRepo, Channel: relationship.Channel}
		accumulator := accumulators[key]
		if accumulator == nil {
			accumulator = &repositoryEdgeAccumulator{key: key, contracts: map[string]struct{}{}}
			accumulators[key] = accumulator
		}
		accumulator.relationships = append(accumulator.relationships, relationshipIndex)
		contract := relationship.Identifier
		if contract == "" {
			contract = relationship.Label
		}
		if contract != "" {
			accumulator.contracts[contract] = struct{}{}
		}
		incrementRepositoryEvidence(&accumulator.evidence, relationship.Evidence, 1)
		accumulator.samples = append(accumulator.samples, repositoryTopologySample{
			ID: relationship.ID, Label: relationship.Label, Identifier: relationship.Identifier, Evidence: relationship.Evidence,
		})
		channelSet[relationship.Channel] = struct{}{}
	}

	keys := make([]repositoryEdgeKey, 0, len(accumulators))
	for key := range accumulators {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return repositoryEdgeKeyLess(keys[i], keys[j]) })
	for _, key := range keys {
		accumulator := accumulators[key]
		sort.Slice(accumulator.samples, func(i, j int) bool {
			if accumulator.samples[i].Label != accumulator.samples[j].Label {
				return accumulator.samples[i].Label < accumulator.samples[j].Label
			}
			return accumulator.samples[i].ID < accumulator.samples[j].ID
		})
		samples := accumulator.samples
		if len(samples) > repositoryTopologyInlineSampleLimit {
			samples = samples[:repositoryTopologyInlineSampleLimit]
		}
		edge := repositoryTopologyEdge{
			ID: repositoryTopologyEdgeID(key), Source: key.Source, Target: key.Target, Channel: key.Channel,
			RelationshipCount: len(accumulator.relationships), ContractCount: len(accumulator.contracts), Evidence: accumulator.evidence,
			Labels: repositoryTopologySampleLabels(samples), Samples: append([]repositoryTopologySample(nil), samples...),
			HasMore: len(accumulator.samples) > len(samples),
		}
		index.Edges = append(index.Edges, edge)
		index.EdgeByKey[key] = edge
		index.RelationshipIDs[key] = append([]int(nil), accumulator.relationships...)
	}
	for channel := range channelSet {
		index.Channels = append(index.Channels, channel)
	}
	sort.Slice(index.Channels, func(i, j int) bool { return index.Channels[i] < index.Channels[j] })
	return index
}

func repositoryTopologyRepositoryFromDashRepo(slug string, repo *DashRepo) repositoryTopologyRepository {
	result := repositoryTopologyRepository{Slug: slug, GraphState: "ready", Modules: []string{}}
	if repo == nil {
		result.GraphState = "missing"
		return result
	}
	if repo.err != "" {
		result.GraphState = "error"
	}
	if repo.Doc == nil {
		if repo.Reader != nil {
			result.EntityCount = repo.Reader.EntityCount()
		}
		return result
	}
	result.EntityCount = repo.Doc.Stats.Entities
	if result.EntityCount == 0 {
		result.EntityCount = len(repo.Doc.Entities)
	}
	languageCounts := make(map[string]int)
	moduleSet := make(map[string]struct{})
	for _, entity := range repo.Doc.Entities {
		if entity.Language != "" {
			languageCounts[entity.Language]++
		}
		if strings.EqualFold(entity.Kind, "module") || strings.EqualFold(entity.Subtype, "module") {
			name := entity.Name
			if name == "" {
				name = entity.QualifiedName
			}
			if name != "" {
				moduleSet[name] = struct{}{}
			}
		}
	}
	result.PrimaryLanguage = mostCommonRepositoryLanguage(languageCounts)
	for module := range moduleSet {
		result.Modules = append(result.Modules, module)
	}
	sort.Strings(result.Modules)
	result.ModuleCount = len(result.Modules)
	return result
}

func mostCommonRepositoryLanguage(counts map[string]int) string {
	best := ""
	bestCount := 0
	for language, count := range counts {
		if count > bestCount || count == bestCount && (best == "" || language < best) {
			best = language
			bestCount = count
		}
	}
	return best
}

func incrementRepositoryEvidence(counts *repositoryEvidenceCounts, evidence repositoryEvidence, amount int) {
	switch evidence {
	case evidenceConfirmed:
		counts.Confirmed += amount
	case evidenceInferred:
		counts.Inferred += amount
	case evidenceDangling:
		counts.Dangling += amount
	case evidenceAmbiguous:
		counts.Ambiguous += amount
	case evidenceExternal:
		counts.External += amount
	}
}

func repositoryTopologyEdgeID(key repositoryEdgeKey) string {
	return key.Source + "->" + key.Target + ":" + string(key.Channel)
}

func repositoryEdgeKeyLess(left, right repositoryEdgeKey) bool {
	if left.Source != right.Source {
		return left.Source < right.Source
	}
	if left.Target != right.Target {
		return left.Target < right.Target
	}
	return left.Channel < right.Channel
}

func repositoryTopologySampleLabels(samples []repositoryTopologySample) []string {
	seen := make(map[string]struct{}, len(samples))
	labels := make([]string, 0, len(samples))
	for _, sample := range samples {
		if sample.Label == "" {
			continue
		}
		if _, ok := seen[sample.Label]; ok {
			continue
		}
		seen[sample.Label] = struct{}{}
		labels = append(labels, sample.Label)
	}
	return labels
}
