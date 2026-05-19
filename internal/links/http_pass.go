package links

// http_pass.go implements the cross-repo HTTP route ↔ fetch matcher.
//
// Foundation:
//   - #534 Phase 1 emits a synthetic `http_endpoint` entity on the
//     producer side (backend that SERVES the route).
//   - #534 Phase 2 attaches an IMPLEMENTS edge from the handler to its
//     synthetic endpoint.
//   - #533 Phase 1 emits a synthetic `http_endpoint` on the consumer
//     side (frontend / RN / Node client that CALLS the route) and
//     records the call-site as a `source_caller` property on the
//     synthetic.
//
// This pass closes the loop. For every synthetic-entity Name shared by
// ≥ 2 repos in the group, we look for at least one producer-side and
// one consumer-side appearance and emit a cross-repo CALLS link from
// the consumer's caller → the producer's handler. The link carries
//
//	relation   = "calls"
//	method     = "http"
//	channel    = "http"
//	identifier = "http:<VERB>:<canonical-path>"
//
// so dashboards and graph queries can filter for typed-HTTP edges
// independently of the structural import-pass output.
//
// Producer / consumer disambiguation
// ----------------------------------
// Each synthetic carries a `pattern_type` property set during emission
// in #533 / #534:
//
//	"http_endpoint_synthesis"         → producer side
//	"http_endpoint_client_synthesis"  → consumer side
//
// When the property is missing or ambiguous, we fall back to the
// attached edges:
//
//	IMPLEMENTS edge from handler → endpoint  → producer
//	CALLS edge from caller → endpoint        → consumer
//
// When neither signal is present we treat the synthetic as
// producer-side by default — this matches the framework prior (most
// http_endpoint synthetics in practice come from the producer-side
// scan).
//
// Verb wildcarding
// ----------------
// Django emits route synthetics with `verb=ANY` because its URL
// configuration wires HTTP methods at the View / ViewSet level rather
// than the URL level. An `ANY`-verb endpoint MUST be matched against
// any specific verb (`GET`, `POST`, …) from the opposite side; we
// canonicalise pairs by collapsing `ANY` to the partner's verb when
// emitting the identifier.
//
// Idempotency
// -----------
// The pass is method-segregated (MethodHTTP). Re-running it replaces
// every entry whose method is "http" while leaving links from
// import_pass, label_pass, and string_pass intact.

import (
	"sort"
	"strings"
)

// MethodHTTP identifies this pass's emissions in links.json.
const MethodHTTP = "http"

// httpChannel is the channel string used on every emitted link.
const httpChannel = "http"

// patternTypeProducer / patternTypeConsumer match the values set by
// the synthesis pass in internal/engine/http_endpoint_synthesis.go.
const (
	patternTypeProducer = "http_endpoint_synthesis"
	patternTypeConsumer = "http_endpoint_client_synthesis"
)

// httpSide labels a synthetic as producer or consumer.
type httpSide int

const (
	sideUnknown httpSide = iota
	sideProducer
	sideConsumer
)

// httpEndpointHit collects everything we need to emit a cross-repo
// link for one synthetic-entity appearance in one repo.
type httpEndpointHit struct {
	repo string
	// stampedID is the synthetic entity's on-disk ID (sha-hashed by
	// the indexer; differs across repos for the same logical endpoint).
	stampedID string
	// name is the canonical `http:<VERB>:<path>` string.
	name string
	// verb is taken from the synthetic's `verb` property (or parsed
	// from name as a fallback).
	verb string
	// canonicalPath is `path` from the synthetic's properties.
	canonicalPath string
	// side is producer / consumer / unknown.
	side httpSide
	// handlerID is the entity ID of the producer-side handler resolved
	// via the IMPLEMENTS edge (handler → endpoint). Empty if no edge.
	handlerID string
	// callerID is the entity ID of the consumer-side caller resolved
	// via a CALLS edge OR via the `source_caller` property fallback.
	// Empty if not resolvable.
	callerID string
	// sourceFile is the synthetic's source file (used for SourceLocations).
	sourceFile string
	// framework comes from the synthetic's `framework` property.
	framework string
}

// runHTTPPass implements the cross-repo HTTP route ↔ fetch matcher.
// See the file header for the contract.
func runHTTPPass(graphs []repoGraph, paths Paths, rejects map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "http"}

	if len(graphs) < 2 {
		// Method-segregated overwrite still runs so a previous group of
		// ≥ 2 repos that shrunk to 1 cleans up its prior HTTP entries.
		_, _, err := replaceByMethod(paths.Links, newMethodSet(MethodHTTP), nil, rejects)
		return res, err
	}

	// Pre-compute the per-repo IMPLEMENTS / CALLS edges that target each
	// http_endpoint synthetic. We key by the local stampedID so we can
	// look the edges up while iterating entities.
	type endpointEdges struct {
		implementsFrom []string // handler entity IDs (producer side)
		callsFrom      []string // caller entity IDs (consumer side)
	}
	// repo → endpointEntityID → endpointEdges
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

	// Index: synthetic name → repo → []hit.
	hits := map[string]map[string][]*httpEndpointHit{}
	for _, g := range graphs {
		edgesForRepo := edgesByEndpoint[g.Repo]
		// Index entities-by-(kind,name,file) so source_caller refs can
		// be resolved to stamped entity IDs in the same file.
		type entKey struct{ kind, name, file string }
		entIDByKey := map[entKey]string{}
		for _, e := range g.Entities {
			if e.Kind == httpEndpointKindLink {
				continue
			}
			k := entKey{e.Kind, e.Name, e.SourceFile}
			if _, ok := entIDByKey[k]; !ok {
				entIDByKey[k] = e.ID
			}
		}
		for _, e := range g.Entities {
			if e.Kind != httpEndpointKindLink {
				continue
			}
			if e.Name == "" {
				continue
			}
			hit := &httpEndpointHit{
				repo:       g.Repo,
				stampedID:  e.ID,
				name:       e.Name,
				sourceFile: e.SourceFile,
			}
			if e.Properties != nil {
				hit.verb = e.Properties["verb"]
				hit.canonicalPath = e.Properties["path"]
				hit.framework = e.Properties["framework"]
				switch e.Properties["pattern_type"] {
				case patternTypeProducer:
					hit.side = sideProducer
				case patternTypeConsumer:
					hit.side = sideConsumer
				}
				// Resolve source_caller (consumer side) to a stamped
				// entity ID in the same file. Falls through silently
				// when missing or unresolvable.
				if ref := e.Properties["source_caller"]; ref != "" {
					if kind, name, ok := splitKindNameRef(ref); ok {
						if id := entIDByKey[entKey{kind, name, e.SourceFile}]; id != "" {
							hit.callerID = id
						}
					}
				}
			}
			// Fallbacks: derive verb / path from the canonical name if
			// the properties weren't populated for some reason.
			if hit.verb == "" || hit.canonicalPath == "" {
				if v, p, ok := parseHTTPName(e.Name); ok {
					if hit.verb == "" {
						hit.verb = v
					}
					if hit.canonicalPath == "" {
						hit.canonicalPath = p
					}
				}
			}
			// Edge-based side resolution.
			if ee := edgesForRepo[e.ID]; ee != nil {
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
			// Final default: when no signal at all is available, treat
			// as producer-side. Most http_endpoint synthetics in
			// practice come from the producer-side scan.
			if hit.side == sideUnknown {
				hit.side = sideProducer
			}
			byRepo := hits[e.Name]
			if byRepo == nil {
				byRepo = map[string][]*httpEndpointHit{}
				hits[e.Name] = byRepo
			}
			byRepo[g.Repo] = append(byRepo[g.Repo], hit)
		}
	}

	// Verb wildcarding: build a parallel index from (path) → []hit so
	// `ANY`-verb endpoints can be matched against any specific-verb
	// endpoint with the same path.
	byPath := map[string][]*httpEndpointHit{}
	for _, byRepo := range hits {
		for _, perRepo := range byRepo {
			for _, h := range perRepo {
				if h.canonicalPath == "" {
					continue
				}
				byPath[h.canonicalPath] = append(byPath[h.canonicalPath], h)
			}
		}
	}

	now := discoveredAt()
	emitted := map[string]bool{}
	var fresh []Link

	// Deterministic iteration order: sort names.
	names := make([]string, 0, len(hits))
	for n := range hits {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		byRepo := hits[name]
		// Producers & consumers for this exact name.
		producers, consumers := splitSidesAcrossRepos(byRepo)

		// Verb wildcarding: if `name` has verb=ANY, also pull in any
		// specific-verb hits with the same path; conversely if `name`
		// has a specific verb, also pull in ANY-verb hits with the same
		// path. The wildcarded counterpart contributes producer /
		// consumer hits keyed by ITS own repo set.
		if verb, p, ok := parseHTTPName(name); ok {
			for _, h := range byPath[p] {
				if h.name == name {
					continue
				}
				hVerb, _, _ := parseHTTPName(h.name)
				if !verbsCompatible(verb, hVerb) {
					continue
				}
				if h.side == sideProducer {
					producers = appendUnique(producers, h)
				} else if h.side == sideConsumer {
					consumers = appendUnique(consumers, h)
				}
			}
		}

		if len(producers) == 0 || len(consumers) == 0 {
			continue
		}
		// Emit one link per (consumer-repo, producer-repo) cross-repo
		// pair. Within a repo-pair we pick the first hit on each side
		// (sorted by file then stampedID for determinism).
		sort.SliceStable(producers, func(i, j int) bool { return less(producers[i], producers[j]) })
		sort.SliceStable(consumers, func(i, j int) bool { return less(consumers[i], consumers[j]) })

		// Group by repo (first hit per repo, per side).
		firstByRepo := func(hh []*httpEndpointHit) map[string]*httpEndpointHit {
			out := map[string]*httpEndpointHit{}
			for _, h := range hh {
				if _, ok := out[h.repo]; !ok {
					out[h.repo] = h
				}
			}
			return out
		}
		producerRepos := firstByRepo(producers)
		consumerRepos := firstByRepo(consumers)

		consumerRepoNames := sortedKeys(consumerRepos)
		producerRepoNames := sortedKeys(producerRepos)

		for _, cRepo := range consumerRepoNames {
			for _, pRepo := range producerRepoNames {
				if cRepo == pRepo {
					continue // never emit a self-pair as a cross-repo edge
				}
				c := consumerRepos[cRepo]
				p := producerRepos[pRepo]

				// Source / target: consumer's caller → producer's handler.
				// If either side hasn't resolved to a real entity, fall
				// back to the synthetic stampedID so the link still
				// points at something meaningful.
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
				id := MakeID(source, target, MethodHTTP)
				if emitted[id] {
					continue
				}
				emitted[id] = true

				ident := canonicalIdentifier(c, p)
				ch := httpChannel
				link := Link{
					ID:           id,
					Source:       source,
					Target:       target,
					Relation:     RelationCalls,
					Method:       MethodHTTP,
					Confidence:   ScoreImport(),
					Channel:      &ch,
					Identifier:   &ident,
					DiscoveredAt: now,
					SourceLocations: [][]string{
						{c.sourceFile},
						{p.sourceFile},
					},
				}
				fresh = append(fresh, link)
			}
		}
	}

	added, skipped, err := replaceByMethod(paths.Links, newMethodSet(MethodHTTP), fresh, rejects)
	if err != nil {
		return res, err
	}
	res.LinksAdded = added
	res.Skipped = skipped
	return res, nil
}

// splitSidesAcrossRepos partitions every hit under `byRepo` into
// producer / consumer slices. Hits with sideUnknown were already
// re-classified by runHTTPPass before reaching here (default →
// producer).
func splitSidesAcrossRepos(byRepo map[string][]*httpEndpointHit) (producers, consumers []*httpEndpointHit) {
	for _, hh := range byRepo {
		for _, h := range hh {
			switch h.side {
			case sideProducer:
				producers = append(producers, h)
			case sideConsumer:
				consumers = append(consumers, h)
			}
		}
	}
	return
}

// less is the deterministic ordering for cross-repo hit selection.
func less(a, b *httpEndpointHit) bool {
	if a.repo != b.repo {
		return a.repo < b.repo
	}
	if a.sourceFile != b.sourceFile {
		return a.sourceFile < b.sourceFile
	}
	return a.stampedID < b.stampedID
}

// appendUnique appends h to hh only if no existing entry has the same
// stampedID (within the same repo).
func appendUnique(hh []*httpEndpointHit, h *httpEndpointHit) []*httpEndpointHit {
	for _, x := range hh {
		if x.repo == h.repo && x.stampedID == h.stampedID {
			return hh
		}
	}
	return append(hh, h)
}

// sortedKeys returns the deterministic sorted key list of m.
func sortedKeys(m map[string]*httpEndpointHit) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// parseHTTPName parses `http:<VERB>:<path>` into its parts.
// Returns ok=false when the input doesn't have that shape.
func parseHTTPName(name string) (verb, path string, ok bool) {
	const prefix = "http:"
	if !strings.HasPrefix(name, prefix) {
		return "", "", false
	}
	rest := name[len(prefix):]
	i := strings.IndexByte(rest, ':')
	if i <= 0 || i == len(rest)-1 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

// verbsCompatible reports whether two verbs may refer to the same
// logical endpoint. ANY (case-insensitive) matches everything; otherwise
// the verbs must match case-insensitively.
func verbsCompatible(a, b string) bool {
	a = strings.ToUpper(a)
	b = strings.ToUpper(b)
	if a == "ANY" || b == "ANY" {
		return true
	}
	return a == b
}

// canonicalIdentifier picks the most specific verb available across
// the two sides when emitting the link's `identifier` field. If one
// side has verb=ANY and the other has GET (etc.), prefer the specific
// verb. Falls back to the consumer's verb otherwise.
func canonicalIdentifier(c, p *httpEndpointHit) string {
	verb := strings.ToUpper(c.verb)
	pVerb := strings.ToUpper(p.verb)
	if verb == "ANY" && pVerb != "" {
		verb = pVerb
	}
	if verb == "" {
		verb = pVerb
	}
	path := c.canonicalPath
	if path == "" {
		path = p.canonicalPath
	}
	return "http:" + verb + ":" + path
}

// splitKindNameRef parses "<Kind>:<Name>" into (kind, name). The Kind
// never contains a colon by construction of the synthesis pass.
func splitKindNameRef(ref string) (kind, name string, ok bool) {
	i := strings.IndexByte(ref, ':')
	if i <= 0 || i == len(ref)-1 {
		return "", "", false
	}
	return ref[:i], ref[i+1:], true
}
