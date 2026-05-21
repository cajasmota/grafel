package links

// openapi_pass.go implements the cross-repo OpenAPI spec linker (P5).
//
// Overview
// --------
// When a repository contains an OpenAPI/Swagger spec, the per-repo
// indexer (internal/patterns/openapi_extractor.go) emits one
// `openapi_operation` entity per path+method pair, carrying:
//
//	kind       = "openapi_operation"
//	Properties["method"] = "GET" | "POST" | ...
//	Properties["path"]   = "/api/users/{id}"
//
// This pass treats those entities as the authoritative contract
// definition and creates cross-repo FETCHES links between:
//
//   - producer side: any `http_endpoint` entity in any other repo with
//     pattern_type=http_endpoint_synthesis, whose (verb, path) matches
//     the spec operation (after path-parameter normalisation).
//   - consumer side: any `http_endpoint` entity in any other repo with
//     pattern_type=http_endpoint_client_synthesis, whose (verb, path)
//     matches the spec operation.
//
// For each matched (consumer-caller → producer-handler) pair we emit
// one cross-repo Link with:
//
//	method       = "openapi-spec"
//	relation     = "calls"
//	channel      = "http"
//	identifier   = "http:<VERB>:<canonical-path>"
//	match_quality = "openapi-spec"
//
// This is P5 — it runs after the structural (P1), label (P2), string
// (P3), and direct-HTTP (P4) passes. Method-segregated overwrite means
// re-running it cleanly replaces its own prior output without touching
// P1–P4 entries.
//
// Relation to the direct-HTTP pass (P4)
// --------------------------------------
// P4 matches http_endpoint synthetics by shared canonical Name across
// repos (the `http:<VERB>:<path>` string). P5 uses the OpenAPI spec
// itself as the pivot: it resolves (verb, path) ← spec → (handler,
// caller) even when the two repos never both emit an http_endpoint
// synthetic with the identical name string, e.g. because the spec is
// hosted in a third "api-contracts" repo.
//
// When both P4 and P5 fire for the same consumer→producer pair the
// method-segregated IDs differ (P4: "http"; P5: "openapi-spec") so
// both entries coexist — downstream consumers can filter by method to
// prefer the higher-provenance source.

import (
	"sort"
	"strings"
)

// MethodOpenAPISpec identifies this pass's emissions in links.json.
const MethodOpenAPISpec = "openapi-spec"

// openAPISpecChannel is written to Link.Channel for every P5 emission.
const openAPISpecChannel = "http"

// openAPISpecMatchQuality is written to Link.MatchQuality.
const openAPISpecMatchQuality = "openapi-spec"

// openAPIOperationKind is the entity Kind emitted by the patterns
// openapi_extractor.go for every path+method in a spec file.
const openAPIOperationKind = "openapi_operation"

// opHit collects one openapi_operation entity from a specific repo.
type opHit struct {
	repo       string
	entityID   string
	verb       string // normalised to UPPER
	path       string // raw from Properties["path"]
	sourceFile string
}

// runOpenAPISpecPass implements P5.
func runOpenAPISpecPass(graphs []repoGraph, paths Paths, rejects map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "openapi-spec"}

	if len(graphs) < 2 {
		// Clean up any prior openapi-spec entries even when the group
		// shrinks to one repo.
		_, _, err := replaceByMethod(paths.Links, newMethodSet(MethodOpenAPISpec), nil, rejects)
		return res, err
	}

	// ── Step 1: collect openapi_operation entities, keyed by normalised
	//            (verb, path) so we can cross-reference against http_endpoint
	//            entities from the same or other repos.
	//
	// normalKey → []opHit
	specOps := map[string][]opHit{}
	for _, g := range graphs {
		for _, e := range g.Entities {
			if e.Kind != openAPIOperationKind {
				continue
			}
			if e.Properties == nil {
				continue
			}
			verb := strings.ToUpper(e.Properties["method"])
			path := e.Properties["path"]
			if verb == "" || path == "" {
				continue
			}
			key := normalizePathForIndex(path)
			hit := opHit{
				repo:       g.Repo,
				entityID:   e.ID,
				verb:       verb,
				path:       path,
				sourceFile: e.SourceFile,
			}
			specOps[key] = append(specOps[key], hit)
		}
	}

	if len(specOps) == 0 {
		// No spec files indexed in this group — nothing to do.
		_, _, err := replaceByMethod(paths.Links, newMethodSet(MethodOpenAPISpec), nil, rejects)
		return res, err
	}

	// ── Step 2: collect http_endpoint (producer & consumer) entities,
	//            also keyed by normalised path (with verb for matching).
	//
	// We reuse httpEndpointHit from http_pass.go plus the same
	// producer/consumer disambiguation logic (pattern_type property,
	// edge-based fallback, default-producer).
	//
	// Pre-compute per-repo edge maps (IMPLEMENTS / CALLS) so we can
	// resolve handler / caller IDs quickly.
	type endpointEdges struct {
		implementsFrom []string
		callsFrom      []string
	}
	edgesByEndpoint := map[string]map[string]*endpointEdges{}
	for _, g := range graphs {
		m := map[string]*endpointEdges{}
		edgesByEndpoint[g.Repo] = m
		for _, e := range g.Edges {
			switch strings.ToUpper(e.Kind) {
			case "IMPLEMENTS":
				ee := m[e.ToID]
				if ee == nil {
					ee = &endpointEdges{}
					m[e.ToID] = ee
				}
				ee.implementsFrom = append(ee.implementsFrom, e.FromID)
			case "CALLS":
				ee := m[e.ToID]
				if ee == nil {
					ee = &endpointEdges{}
					m[e.ToID] = ee
				}
				ee.callsFrom = append(ee.callsFrom, e.FromID)
			}
		}
	}

	// normalKey → side → []httpEndpointHit
	type sideMap struct {
		producers []*httpEndpointHit
		consumers []*httpEndpointHit
	}
	byNormPath := map[string]*sideMap{}

	for _, g := range graphs {
		// Build (kind,name,file) → entityID index for source_caller resolution.
		type entKey struct{ kind, name, file string }
		entIDByKey := map[entKey]string{}
		for _, e := range g.Entities {
			// #1217: exclude all three http endpoint kind variants from the
			// entID index so synthetics don't resolve against each other.
			if isHTTPEndpointLink(e.Kind) {
				continue
			}
			k := entKey{e.Kind, e.Name, e.SourceFile}
			if _, ok := entIDByKey[k]; !ok {
				entIDByKey[k] = e.ID
			}
		}

		for _, e := range g.Entities {
			// #1217: match all three http endpoint kind variants.
			if !isHTTPEndpointLink(e.Kind) {
				continue
			}
			if e.Name == "" || e.Properties == nil {
				continue
			}
			verb := strings.ToUpper(e.Properties["verb"])
			path := e.Properties["path"]
			if verb == "" && path == "" {
				// Fall back to parsing from canonical name.
				if v, p, ok := parseHTTPName(e.Name); ok {
					verb = strings.ToUpper(v)
					path = p
				}
			}
			if path == "" {
				continue
			}
			normKey := normalizePathForIndex(path)

			hit := &httpEndpointHit{
				repo:          g.Repo,
				stampedID:     e.ID,
				name:          e.Name,
				verb:          verb,
				canonicalPath: path,
				sourceFile:    e.SourceFile,
				framework:     e.Properties["framework"],
			}

			// Determine side from pattern_type property.
			switch e.Properties["pattern_type"] {
			case patternTypeProducer:
				hit.side = sideProducer
			case patternTypeConsumer:
				hit.side = sideConsumer
			}

			// Resolve source_caller (consumer side).
			if ref := e.Properties["source_caller"]; ref != "" {
				if kind, name, ok := splitKindNameRef(ref); ok {
					if id := entIDByKey[entKey{kind, name, e.SourceFile}]; id != "" {
						hit.callerID = id
					}
				}
			}

			// Edge-based side + handler/caller resolution.
			if ee := edgesByEndpoint[g.Repo][e.ID]; ee != nil {
				if len(ee.implementsFrom) > 0 {
					if hit.side == sideUnknown {
						hit.side = sideProducer
					}
					hit.handlerID = ee.implementsFrom[0]
				}
				if len(ee.callsFrom) > 0 {
					if hit.side == sideUnknown {
						hit.side = sideConsumer
					}
					if hit.callerID == "" {
						hit.callerID = ee.callsFrom[0]
					}
				}
			}

			// Default: treat as producer when no signal.
			if hit.side == sideUnknown {
				hit.side = sideProducer
			}

			sm := byNormPath[normKey]
			if sm == nil {
				sm = &sideMap{}
				byNormPath[normKey] = sm
			}
			switch hit.side {
			case sideProducer:
				sm.producers = append(sm.producers, hit)
			case sideConsumer:
				sm.consumers = append(sm.consumers, hit)
			}
		}
	}

	// ── Step 3: for each openapi_operation, look up producers and
	//            consumers with matching (verb, normPath) and emit links.
	//
	// The spec operation acts as a bridge: the link direction is always
	// consumer-caller → producer-handler (same as P4).
	//
	// For each (spec-repo, normKey) we first filter producers and
	// consumers to those in *different* repos from the spec, then apply
	// the same verb-aware tier selection as pickProducerForConsumer.

	now := discoveredAt()
	emitted := map[string]bool{}
	var fresh []Link

	// Iterate in deterministic order (sorted normPath keys).
	sortedNormKeys := make([]string, 0, len(specOps))
	for k := range specOps {
		sortedNormKeys = append(sortedNormKeys, k)
	}
	sort.Strings(sortedNormKeys)

	for _, normKey := range sortedNormKeys {
		ops := specOps[normKey]
		sm := byNormPath[normKey]
		if sm == nil {
			// No http_endpoint entities for this path at all.
			continue
		}

		for _, op := range ops {
			// Filter producers and consumers to those from repos OTHER
			// than the spec repo. Same-repo links are meaningless here.
			var producers, consumers []*httpEndpointHit
			for _, h := range sm.producers {
				if h.repo == op.repo {
					continue
				}
				if !verbsCompatible(op.verb, h.verb) {
					continue
				}
				producers = append(producers, h)
			}
			for _, h := range sm.consumers {
				if h.repo == op.repo {
					continue
				}
				if !verbsCompatible(op.verb, h.verb) {
					continue
				}
				consumers = append(consumers, h)
			}

			if len(producers) == 0 || len(consumers) == 0 {
				continue
			}

			// Deterministic ordering.
			sort.SliceStable(producers, func(i, j int) bool { return less(producers[i], producers[j]) })
			sort.SliceStable(consumers, func(i, j int) bool { return less(consumers[i], consumers[j]) })

			// Group by repo for cross-product emission.
			producersByRepo := map[string][]*httpEndpointHit{}
			for _, p := range producers {
				producersByRepo[p.repo] = append(producersByRepo[p.repo], p)
			}
			consumerRepos := map[string]*httpEndpointHit{}
			for _, c := range consumers {
				if _, ok := consumerRepos[c.repo]; !ok {
					consumerRepos[c.repo] = c
				}
			}

			cRepoNames := sortedKeys(consumerRepos)
			pRepoNames := make([]string, 0, len(producersByRepo))
			for r := range producersByRepo {
				pRepoNames = append(pRepoNames, r)
			}
			sort.Strings(pRepoNames)

			for _, cRepo := range cRepoNames {
				for _, pRepo := range pRepoNames {
					if cRepo == pRepo {
						continue
					}
					c := consumerRepos[cRepo]
					p, _ := pickProducerForConsumer(c, producersByRepo[pRepo])
					if p == nil {
						continue
					}

					srcID := c.callerID
					if srcID == "" {
						srcID = c.stampedID
					}
					tgtID := p.handlerID
					if tgtID == "" {
						tgtID = p.stampedID
					}
					source := entityKey(c.repo, srcID)
					target := entityKey(p.repo, tgtID)
					id := MakeID(source, target, MethodOpenAPISpec)
					if emitted[id] {
						continue
					}
					emitted[id] = true

					ident := canonicalIdentifier(c, p)
					ch := openAPISpecChannel
					link := Link{
						ID:           id,
						Source:       source,
						Target:       target,
						Relation:     RelationCalls,
						Method:       MethodOpenAPISpec,
						Confidence:   ScoreImport(), // spec-backed = high confidence
						Channel:      &ch,
						Identifier:   &ident,
						DiscoveredAt: now,
						SourceLocations: [][]string{
							{c.sourceFile},
							{op.sourceFile},
							{p.sourceFile},
						},
						MatchQuality: openAPISpecMatchQuality,
					}
					fresh = append(fresh, link)
				}
			}
		}
	}

	added, skipped, err := replaceByMethod(paths.Links, newMethodSet(MethodOpenAPISpec), fresh, rejects)
	if err != nil {
		return res, err
	}
	res.LinksAdded = added
	res.Skipped = skipped
	return res, nil
}
