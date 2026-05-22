package links

// topic_pass.go implements the cross-repo message-topic publisher↔subscriber
// matcher (P7).
//
// Design
// ------
// The Kafka/SNS/SQS/EventBridge/Redis engine passes emit synthetic
// SCOPE.MessageTopic entities keyed by a broker-prefixed topic name:
//
//	Entity{Kind: "SCOPE.MessageTopic", Name: "kafka:orders.placed"}
//	Entity{Kind: "SCOPE.MessageTopic", Name: "sns:payments.settled"}
//	Entity{Kind: "SCOPE.MessageTopic", Name: "sqs:inventory-reserved-queue"}
//	Entity{Kind: "SCOPE.MessageTopic", Name: "event:eventbridge:orders:orders.placed"}
//	Entity{Kind: "SCOPE.MessageTopic", Name: "redis:orders.placed"}
//
// On the publisher side, a PUBLISHES_TO edge points from the producer
// function/method to the MessageTopic entity.
// On the subscriber side, a SUBSCRIBES_TO edge points from the consumer
// function/method to the MessageTopic entity.
//
// Cross-repo identity: the Name field is already normalised by the engine
// pass so the same topic Name appears in every repo that touches it. P7
// joins by Name, exactly like P4 joins http_endpoint synthetics by Name.
//
// Emits one link per (topicName, publisherRepo, subscriberRepo) triple — i.e.
// one edge per distinct topic per repo-pair. Two different topics flowing
// between the same repo-pair produce two separate edges (#1474). Within a
// single topic the representative-per-side entity selection still collapses
// the publisher×subscriber entity cartesian to O(1) per repo-pair (#1453).
//
//	relation   = "publishes_to"
//	method     = "topic"
//	channel    = broker name (kafka / sns / sqs / eventbridge / redis / ...)
//	identifier = topic name
//
// Idempotency: method-segregated overwrite on MethodTopic. Re-running P7
// replaces every entry whose method is "topic" while leaving P1–P6 intact.

import (
	"sort"
	"strings"
)

// MethodTopic identifies this pass's emissions in links.json.
const MethodTopic = "topic"

// RelationPublishesTo is the relation written on publisher→subscriber links.
// More precise than RelationCalls for message-bus flows.
const RelationPublishesTo = "publishes_to"

// topicMessageTopicKind is the entity kind emitted by the Kafka/SNS/SQS/
// EventBridge engine passes.
const topicMessageTopicKind = "SCOPE.MessageTopic"

// topicQueueKind is the entity kind emitted by the Redis pub/sub + Streams
// engine pass (redis_pubsub_edges.go). These carry a broker-prefixed Name
// (`channel:redis-pubsub:<name>` / `stream:redis:<name>`) plus PUBLISHES_TO /
// SUBSCRIBES_TO edges, exactly like SCOPE.MessageTopic, but use a distinct
// kind. Before #1489 P7 ignored them entirely, so a Redis publisher and
// subscriber in different repos sharing an identical channel Name were never
// paired — the real polyglot fixture's
// notifications→{tracking-ws,realtime-dashboard}
// `channel:redis-pubsub:notifications.push` links silently dropped. P7 now
// treats SCOPE.Queue identically: the canonical Name already matches across
// repos, so it joins on Name with no new matching logic (#1489).
const topicQueueKind = "SCOPE.Queue"

// isTopicEntityKind reports whether a graph entity participates in the P7
// publisher↔subscriber join. Both the broker MessageTopic synthetics and the
// Redis pub/sub Queue synthetics qualify (#1489).
func isTopicEntityKind(kind string) bool {
	return kind == topicMessageTopicKind || kind == topicQueueKind
}

// topicEmissionCapPerName bounds the number of cross-repo edges a single
// topic Name may emit. #1454 already collapsed the per-repo-pair entity
// product to one representative each, leaving the emission at O(R²) repo
// pairs per topic. This belt-and-suspenders cap defends the link pass (and
// therefore group-load) against a future re-index that restores a dense
// fan-out topology — e.g. a hub topic touched by every repo in a large
// group as both publisher and subscriber — where R² alone could still
// produce tens of thousands of edges from one Name. The cap keeps the pass
// terminating in bounded time regardless of fixture growth (#1456).
//
// Sized generously: a legitimate hot topic linking up to ~32 distinct
// publisher/subscriber repo-pairs is preserved in full; only pathological
// fan-outs beyond that are truncated. Mirrors labelEmissionCap in
// label_pass.go.
const topicEmissionCapPerName = 1024

// topicPublishesEdge / topicSubscribesEdge are matched case-insensitively.
const topicPublishesEdge = "PUBLISHES_TO"
const topicSubscribesEdge = "SUBSCRIBES_TO"

// topicHit collects one MessageTopic appearance in one repo.
type topicHit struct {
	repo       string
	stampedID  string
	name       string
	sourceFile string
	// publisherIDs are entity IDs of publishers (PUBLISHES_TO edges TO this topic).
	publisherIDs []string
	// subscriberIDs are entity IDs of subscribers (SUBSCRIBES_TO edges TO this topic).
	subscriberIDs []string
}

// brokerFromTopicName extracts the broker string from a topic Name for the
// channel field on emitted links. Examples:
//
//	"kafka:orders.placed"           → "kafka"
//	"sns:payments.settled"          → "sns"
//	"sqs:inventory-queue"           → "sqs"
//	"event:eventbridge:src:type"    → "eventbridge"
//	"event:eventgrid:topic:type"    → "eventgrid"
//	"redis:orders.placed"           → "redis"
//	"nats:orders.placed"            → "nats"
func brokerFromTopicName(name string) string {
	// "event:eventbridge:..." and other event-bus forms: second segment is broker.
	if strings.HasPrefix(name, "event:") {
		rest := name[len("event:"):]
		if i := strings.IndexByte(rest, ':'); i > 0 {
			return rest[:i] // "eventbridge", "eventgrid", "cloudevents"
		}
		return "event"
	}
	// Redis pub/sub queue synthetics: "channel:redis-pubsub:<name>" and Redis
	// Streams "stream:redis:<name>". The second segment carries the transport
	// (redis-pubsub / redis); normalise both to the "redis" channel so emitted
	// links read consistently with the rest of the topic pass (#1489).
	if strings.HasPrefix(name, "channel:") || strings.HasPrefix(name, "stream:") {
		rest := name[strings.IndexByte(name, ':')+1:]
		if i := strings.IndexByte(rest, ':'); i > 0 {
			transport := rest[:i]
			if strings.HasPrefix(transport, "redis") {
				return "redis"
			}
			return transport
		}
		return "redis"
	}
	// Simple "broker:..." form (kafka, sns, sqs, redis, nats, pubsub, etc.).
	if i := strings.IndexByte(name, ':'); i > 0 {
		return name[:i]
	}
	return "message"
}

// minString returns the lexicographically smallest string in ids, or "" when
// ids is empty. Used to pick a single deterministic representative entity per
// side so the topic pass emits one cross-repo edge per repo pair instead of
// the full publisher×subscriber product (#1453).
func minString(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	m := ids[0]
	for _, s := range ids[1:] {
		if s < m {
			m = s
		}
	}
	return m
}

// runTopicPass implements P7: cross-repo message-topic publisher↔subscriber
// linker.
func runTopicPass(graphs []repoGraph, paths Paths, rejects map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "topic"}

	if len(graphs) < 2 {
		// Method-segregated overwrite still runs so a previous group of
		// ≥ 2 repos that shrunk to 1 cleans up its prior topic entries.
		_, _, err := replaceByMethod(paths.Links, newMethodSet(MethodTopic), nil, rejects)
		return res, err
	}

	// Pre-compute inbound PUBLISHES_TO / SUBSCRIBES_TO edges per repo,
	// indexed by the topic entity ID they point at.
	type inboundTopicEdge struct {
		fromID string
		kind   string // "PUBLISHES_TO" or "SUBSCRIBES_TO"
	}
	// repo → toEntityID → []inboundTopicEdge
	inboundByRepo := map[string]map[string][]inboundTopicEdge{}
	for _, g := range graphs {
		m := map[string][]inboundTopicEdge{}
		inboundByRepo[g.Repo] = m
		for _, e := range g.Edges {
			upper := strings.ToUpper(e.Kind)
			if upper == topicPublishesEdge || upper == topicSubscribesEdge {
				m[e.ToID] = append(m[e.ToID], inboundTopicEdge{fromID: e.FromID, kind: upper})
			}
		}
	}

	// Index: topic name → repo → hit.
	// One hit per (repo, name) pair — first occurrence wins.
	hitsByName := map[string]map[string]*topicHit{}
	for _, g := range graphs {
		inbound := inboundByRepo[g.Repo]
		for _, e := range g.Entities {
			if !isTopicEntityKind(e.Kind) {
				continue
			}
			if e.Name == "" {
				continue
			}
			byRepo, ok := hitsByName[e.Name]
			if !ok {
				byRepo = map[string]*topicHit{}
				hitsByName[e.Name] = byRepo
			}
			if _, exists := byRepo[g.Repo]; exists {
				continue // first-occurrence wins
			}
			hit := &topicHit{
				repo:       g.Repo,
				stampedID:  e.ID,
				name:       e.Name,
				sourceFile: e.SourceFile,
			}
			for _, ie := range inbound[e.ID] {
				switch ie.kind {
				case topicPublishesEdge:
					hit.publisherIDs = append(hit.publisherIDs, ie.fromID)
				case topicSubscribesEdge:
					hit.subscriberIDs = append(hit.subscriberIDs, ie.fromID)
				}
			}
			byRepo[g.Repo] = hit
		}
	}

	now := discoveredAt()
	emitted := map[string]bool{}
	var fresh []Link

	// Sort names for deterministic output.
	names := make([]string, 0, len(hitsByName))
	for n := range hitsByName {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		byRepo := hitsByName[name]
		if len(byRepo) < 2 {
			continue
		}

		// Collect publisher repos (have PUBLISHES_TO) and subscriber repos
		// (have SUBSCRIBES_TO). A repo can be both.
		var publishers, subscribers []*topicHit
		for _, h := range byRepo {
			if len(h.publisherIDs) > 0 {
				publishers = append(publishers, h)
			}
			if len(h.subscriberIDs) > 0 {
				subscribers = append(subscribers, h)
			}
		}

		if len(publishers) == 0 || len(subscribers) == 0 {
			continue
		}

		sort.Slice(publishers, func(i, j int) bool { return publishers[i].repo < publishers[j].repo })
		sort.Slice(subscribers, func(i, j int) bool { return subscribers[i].repo < subscribers[j].repo })

		broker := brokerFromTopicName(name)

		emittedForName := 0
		for _, pub := range publishers {
			if emittedForName >= topicEmissionCapPerName {
				break
			}
			// Choose a single, deterministic representative publisher entity
			// for this repo. Emitting the FULL (publisher × subscriber) entity
			// product per repo pair is a combinatorial blow-up: a topic touched
			// by R repos with k publish/subscribe ops each produces O(R²·k²)
			// links, which on a grown multi-repo graph (e.g. shipfast's hot
			// topics) explodes into millions of links and stalls the link pass
			// during group-load (#1453). The meaningful cross-repo signal is a
			// single publisher-repo → subscriber-repo edge, so we pick one
			// representative entity per side — mirroring the HTTP pass, which
			// likewise uses the first handler/caller per endpoint (#1453).
			srcID := minString(pub.publisherIDs)
			if srcID == "" {
				continue
			}

			for _, sub := range subscribers {
				if emittedForName >= topicEmissionCapPerName {
					break
				}
				if pub.repo == sub.repo {
					continue // never emit a self-pair as a cross-repo edge
				}

				tgtID := minString(sub.subscriberIDs)
				if tgtID == "" {
					continue
				}

				source := entityKey(pub.repo, srcID)
				target := entityKey(sub.repo, tgtID)
				// Dedup key is (topicName, publisherRepo, subscriberRepo): two
				// distinct topics flowing between the same repo-pair must each emit
				// their own edge. Before #1474 the key was just (source, target,
				// method) — which collapsed when the same representative entity was
				// the minimum publisher/subscriber for two different topic Names,
				// suppressing the second edge (#1474). Including `name` in the ID
				// hash produces a unique key per (topic, repo-pair) while keeping
				// the O(R²) rep-per-side bound intact. Link.Method stays MethodTopic
				// so replaceByMethod segregation is unchanged.
				id := MakeID(source, target, MethodTopic+":"+name)
				if emitted[id] {
					continue
				}
				emitted[id] = true
				emittedForName++

				ident := name
				ch := broker
				fresh = append(fresh, Link{
					ID:           id,
					Source:       source,
					Target:       target,
					Relation:     RelationPublishesTo,
					Method:       MethodTopic,
					Confidence:   ScoreImport(),
					Channel:      &ch,
					Identifier:   &ident,
					DiscoveredAt: now,
					SourceLocations: [][]string{
						{pub.sourceFile},
						{sub.sourceFile},
					},
				})
			}
		}
	}

	added, skipped, err := replaceByMethod(paths.Links, newMethodSet(MethodTopic), fresh, rejects)
	if err != nil {
		return res, err
	}
	res.LinksAdded = added
	res.Skipped = skipped
	return res, nil
}
