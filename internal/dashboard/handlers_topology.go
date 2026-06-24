package dashboard

// handlers_topology.go — Broker Topology endpoint
//
//	GET /api/topology/{group}
//	GET /api/groups/{group}/topics
//
// Wire contract: every array field in the JSON response MUST marshal as []
// (never null) so the frontend can iterate without a null-guard. This is
// enforced by using topologyResponse (a concrete struct with slice fields
// initialised to empty non-nil slices) instead of a raw map.

import (
	"net/http"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/mcp"
)

// Broker entity kinds (suffix after stripping the optional "SCOPE." prefix).
const (
	kindMessageTopic       = "MessageTopic"
	kindQueue              = "Queue"
	kindChannelEvent       = "ChannelEvent"
	kindSubscription       = "Subscription"       // GraphQL subscriptions
	kindServerlessFunction = "ServerlessFunction" // SCOPE.ServerlessFunction stripped
	// #1116: async task frameworks (Celery, dramatiq, RQ, Sidekiq, Bull, …) emit
	// entities with kind "Task" or "ScheduledJob" (with optional "SCOPE." prefix).
	// After prefix-stripping these must be bucketed into the queues track.
	kindTask         = "Task"
	kindScheduledJob = "ScheduledJob"
)

// topologyResponse is the wire shape for both topology endpoints.
// Every slice field is guaranteed non-nil (JSON [] not null).
type topologyResponse struct {
	Topics               []map[string]any `json:"topics"`
	Queues               []map[string]any `json:"queues"`
	Channels             []map[string]any `json:"channels"`
	NatsSubjects         []map[string]any `json:"nats_subjects"`
	GraphQLSubscriptions []map[string]any `json:"graphql_subscriptions"`
	Transforms           []map[string]any `json:"transforms"`
	Functions            []map[string]any `json:"functions"`
	// BrokerGroups is a top-level summary of all brokers present in the
	// group, sorted alphabetically by broker name (stable).  Each entry
	// contains per-service topic counts, orphan counts, cross-repo topic
	// counts, a health summary, and the last index timestamp.
	BrokerGroups []brokerGroup `json:"broker_groups"`

	// Enrichment operations output (#1103). When an enrichment store is
	// available, candidates flagged disqualified are MOVED OUT of their
	// natural bucket into the matching *_rejected slice; canonical entries
	// receive an "aliases" field listing merged-away peers; an explicit
	// rank promotes entries to the top of their bucket; and a group
	// summary is emitted at the top level so the dashboard sidebar can
	// render LLM-inferred clusters.
	TopicsRejected    []map[string]any         `json:"topics_rejected"`
	QueuesRejected    []map[string]any         `json:"queues_rejected"`
	ChannelsRejected  []map[string]any         `json:"channels_rejected"`
	FunctionsRejected []map[string]any         `json:"functions_rejected"`
	EnrichmentGroups  []EnrichmentGroupSummary `json:"enrichment_groups"`

	// Confidence-floor partition (#1129). Each *_low_confidence slice
	// mirrors its bucket but holds the entries whose confidence score
	// fell below the surface-wide floor (default 0.45 for topology). The
	// UI shows these only when the user opts in to "Show low-signal
	// entries". NoiseRejectedCount is the total across all four buckets;
	// ConfidenceFloor is the effective floor applied (after env override).
	TopicsLowConfidence    []map[string]any `json:"topics_low_confidence"`
	QueuesLowConfidence    []map[string]any `json:"queues_low_confidence"`
	ChannelsLowConfidence  []map[string]any `json:"channels_low_confidence"`
	FunctionsLowConfidence []map[string]any `json:"functions_low_confidence"`
	NoiseRejectedCount     int              `json:"noise_rejected_count"`
	ConfidenceFloor        float64          `json:"confidence_floor"`
}

// brokerServiceStat holds per-service aggregated counts inside a broker group.
type brokerServiceStat struct {
	Name       string `json:"name"`
	TopicCount int    `json:"topic_count"`
}

// brokerHealthSummary breaks down entity health per broker.
type brokerHealthSummary struct {
	Active           int `json:"active"`
	OrphanPublisher  int `json:"orphan_publisher"`
	OrphanSubscriber int `json:"orphan_subscriber"`
	Orphan           int `json:"orphan"`
}

// brokerGroup is one element of topologyResponse.BrokerGroups.
type brokerGroup struct {
	Broker              string              `json:"broker"`
	Count               int                 `json:"count"`
	Services            []brokerServiceStat `json:"services"`
	OrphanPublishers    int                 `json:"orphan_publishers"`
	OrphanSubscribers   int                 `json:"orphan_subscribers"`
	CrossRepoTopicCount int                 `json:"cross_repo_topic_count"`
	HealthSummary       brokerHealthSummary `json:"health_summary"`
	LastIndexTimestamp  string              `json:"last_index_timestamp,omitempty"`
}

// classifyTopologyBucket maps an entity (by kind + name + properties) to
// one of the topology buckets.  Returns "" when the entity should be
// ignored by the topology surface.
//
// NOTE: `name` is the graph.Entity.Name field (human-readable / canonical
// identifier), NOT the hashed graph.Entity.ID.  Synthetic runtime entities
// emitted by the engine passes (redis_pubsub_edges, serverless_edges, etc.)
// store the semantic prefix in the Name field (e.g. "channel:redis-pubsub:foo",
// "aws-lambda:OrderProcessor") rather than in the hashed ID.
//
// Bucket values: "topic" | "queue" | "channel" | "function" | "subscription"
func classifyTopologyBucket(kind, name string, props map[string]string) string {
	stripped := dashStripScopePrefix(kind)

	// --- Existing kinds (order matters: check specific first) ---
	switch stripped {
	case kindMessageTopic:
		return "topic"
	case kindChannelEvent:
		return "channel"
	case kindServerlessFunction:
		return "function"
	case kindSubscription:
		return "subscription"
	// #1116: async task frameworks (Celery, dramatiq, RQ, Sidekiq, Bull, …) emit
	// Task entities; scheduled-job pass emits ScheduledJob entities. Both belong
	// in the queues bucket so the frontend renders them with the correct icon and
	// framework tag. The framework property is carried through in collectTopologyResponse.
	case kindTask, kindScheduledJob:
		return "queue"
	}

	// --- Name-prefix classification (new runtime extractors, #930 / #925 / #941) ---
	// These synthetic entities use the semantic name as a cross-repo key.
	switch {
	case strings.HasPrefix(name, "channel:redis-pubsub:"):
		return "channel"
	case strings.HasPrefix(name, "stream:redis:"):
		return "queue"
	case strings.HasPrefix(name, "aws-lambda:"),
		strings.HasPrefix(name, "gcp-cloudfunction:"),
		strings.HasPrefix(name, "azure-function:"):
		return "function"
	case strings.HasPrefix(name, "task:"):
		return "queue"
	}

	// --- Properties-based classification (channel_type = pubsub/stream) ---
	if stripped == kindQueue {
		switch props["channel_type"] {
		case "pubsub":
			return "channel" // Redis pub/sub folded into channel track
		}
		return "queue"
	}

	return ""
}

// handleTopology — GET /api/topology/{group}
func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}
	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	docgenState, _ := mcp.LoadDocgenState(group)
	writeJSON(w, http.StatusOK, collectTopologyResponse(grp, group, docgenState))
}

// handleGroupTopics — GET /api/groups/{group}/topics
func (s *Server) handleGroupTopics(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}
	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	docgenState, _ := mcp.LoadDocgenState(group)
	writeJSON(w, http.StatusOK, collectTopologyResponse(grp, group, docgenState))
}

// brokerAccum accumulates per-broker data during collectTopologyResponse so we
// can produce the BrokerGroups summary in a single pass.
type brokerAccum struct {
	count               int
	services            map[string]int // service name → topic/queue count
	orphanPublishers    int
	orphanSubscribers   int
	crossRepoTopicCount int
	healthSummary       brokerHealthSummary
	lastIndexTS         string
	// repoSlugs tracks which repos contributed entities to this broker bucket.
	repoSlugs map[string]struct{}
}

// collectTopologyResponse builds the full topology wire payload from a loaded
// group. All slice fields are initialised to non-nil empty slices so that
// JSON encoding produces [] (not null) when no data exists — fixing the
// frontend error boundary triggered by null nats_subjects / graphql_subscriptions
// on groups with no NATS or GraphQL edges (#944).
//
// groupName and docgenState are optional (pass "" / nil to skip enrichment).
func collectTopologyResponse(grp *DashGroup, groupName string, docgenState *mcp.DocgenState) topologyResponse {
	resp := topologyResponse{
		Topics:               []map[string]any{},
		Queues:               []map[string]any{},
		Channels:             []map[string]any{},
		NatsSubjects:         []map[string]any{},
		GraphQLSubscriptions: []map[string]any{},
		Transforms:           []map[string]any{},
		Functions:            []map[string]any{},
		BrokerGroups:         []brokerGroup{},
		TopicsRejected:       []map[string]any{},
		QueuesRejected:       []map[string]any{},
		ChannelsRejected:     []map[string]any{},
		FunctionsRejected:    []map[string]any{},
		EnrichmentGroups:     []EnrichmentGroupSummary{},
	}

	// brokerAccums accumulates data keyed by broker_canonical for the top-level
	// broker_groups summary. Channels and functions are excluded (they have a
	// different classification axis).
	brokerAccums := map[string]*brokerAccum{}

	// ensureBroker returns (or creates) the accumulator for a canonical broker.
	ensureBroker := func(canonical string) *brokerAccum {
		if a, ok := brokerAccums[canonical]; ok {
			return a
		}
		a := &brokerAccum{
			services:  map[string]int{},
			repoSlugs: map[string]struct{}{},
		}
		brokerAccums[canonical] = a
		return a
	}

	// topicMergeAccum holds the cross-repo merged state for a single
	// SCOPE.MessageTopic (keyed by canonical + name, e.g.
	// "kafka|kafka:payments.settled"). Multiple repos may each carry their own
	// graph-stamped copy of the same topic; we dedup them here so the
	// topology view renders ONE node per (broker, topic_name) instead of N.
	// Fixes #1695.
	type topicMergeAccum struct {
		name      string
		rawBroker string
		canonical string
		// owningService from the first-seen occurrence — used as fallback.
		owningService string
		// producers and consumers keyed by prefixed entity ID for dedup.
		producerSet  map[string]struct{}
		consumerSet  map[string]struct{}
		producerList []string
		consumerList []string
		// transformsTo IDs (already prefixed with repo slug).
		transformsTo []string
		// appearsIn tracks repo slugs that contributed this topic.
		appearsIn    []string
		appearsInSet map[string]struct{}
		// perRepoPrefixedIDs collects all per-repo prefixed entity IDs so the
		// cross-repo link scan (crossRepoIDs) can match against them.
		perRepoPrefixedIDs []string
		// lastIndexTS keeps the newest timestamp across repos.
		lastIndexTS string
		// topicID is the stable cross-repo ID for this merged node.
		topicID string
	}
	// topicByName is the dedup map: (canonical + "|" + name) → accumulator.
	// Using canonical as part of the key ensures that identically-named topics
	// on different brokers (e.g. "A" on rabbitmq vs "A" on sqs) remain separate.
	topicByName := map[string]*topicMergeAccum{}

	for _, r := range sortedRepos(grp) {
		if r.Doc == nil {
			continue
		}

		// Capture the last-index timestamp for this repo to propagate to the
		// broker_groups.  GeneratedAt is zero for test fixtures; emit only when
		// non-zero.
		var repoTS string
		if !r.Doc.GeneratedAt.IsZero() {
			repoTS = r.Doc.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z")
		}

		// For each entity, classify into a topology bucket and collect edges.
		// classifyTopologyBucket uses the entity Name (not hashed ID) because
		// synthetic runtime entities store the semantic prefix in the Name field.
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			bucket := classifyTopologyBucket(e.Kind, e.Name, e.Properties)

			switch bucket {
			case "topic":
				// #1695 — dedup SCOPE.MessageTopic nodes by (canonical, name) across
				// repos. Accumulate producers, consumers, and appearance provenance
				// into topicByName; we emit the merged entries after the loop.
				producers, consumers, transformsTo := brokerEdges(r, e.ID)
				rawBroker := e.Properties["broker"]
				framework := e.Properties["framework"]
				canonical := brokerCanonical(rawBroker, framework)
				svc := owningService(e.Properties, r.Slug)

				dedupKey := canonical + "|" + e.Name
				acc, exists := topicByName[dedupKey]
				if !exists {
					// Stable cross-repo topic ID: hash of just the Name (no repo).
					// Prefix "merged:" distinguishes it from per-repo stamped IDs so
					// the frontend never accidentally resolves it as a graph entity.
					acc = &topicMergeAccum{
						name:          e.Name,
						rawBroker:     rawBroker,
						canonical:     canonical,
						owningService: svc,
						producerSet:   map[string]struct{}{},
						consumerSet:   map[string]struct{}{},
						appearsInSet:  map[string]struct{}{},
						topicID:       "merged:" + canonical + ":" + e.Name,
					}
					topicByName[dedupKey] = acc
				}
				// Track per-repo prefixed entity ID for cross-repo link matching.
				acc.perRepoPrefixedIDs = append(acc.perRepoPrefixedIDs, dashPrefixedID(r.Slug, e.ID))
				// Merge producers (prefixed with repo slug for disambiguation).
				for _, p := range producers {
					if _, seen := acc.producerSet[p]; !seen {
						acc.producerSet[p] = struct{}{}
						acc.producerList = append(acc.producerList, p)
					}
				}
				// Merge consumers.
				for _, c := range consumers {
					if _, seen := acc.consumerSet[c]; !seen {
						acc.consumerSet[c] = struct{}{}
						acc.consumerList = append(acc.consumerList, c)
					}
				}
				// Merge TRANSFORMS edges (deduplicate by destination ID).
				for _, tt := range transformsTo {
					found := false
					for _, existing := range acc.transformsTo {
						if existing == tt {
							found = true
							break
						}
					}
					if !found {
						acc.transformsTo = append(acc.transformsTo, tt)
					}
				}
				// Track repo appearance.
				if _, seen := acc.appearsInSet[r.Slug]; !seen {
					acc.appearsInSet[r.Slug] = struct{}{}
					acc.appearsIn = append(acc.appearsIn, r.Slug)
				}
				// Keep newest index timestamp.
				if repoTS > acc.lastIndexTS {
					acc.lastIndexTS = repoTS
				}

				// Accumulate broker_groups data (one count per topic, not per repo).
				// The health verdict is deferred until after the loop when we know
				// the full merged producer+consumer set.
				a := ensureBroker(canonical)
				a.repoSlugs[r.Slug] = struct{}{}
				if repoTS > a.lastIndexTS {
					a.lastIndexTS = repoTS
				}

			case "queue":
				producers, consumers, _ := brokerEdges(r, e.ID)
				broker := e.Properties["broker"]
				if broker == "" {
					// Fall back to inferring from the entity Name prefix.
					broker = inferBrokerFromName(e.Name)
				}
				// Async task queues carry framework info instead of a broker name.
				framework := e.Properties["framework"]
				canonical := brokerCanonical(broker, framework)
				svc := owningService(e.Properties, r.Slug)
				entry := map[string]any{
					"id":               dashPrefixedID(r.Slug, e.ID),
					"repo":             r.Slug,
					"label":            e.Name,
					"broker":           broker,
					"broker_canonical": canonical,
					"framework":        framework,
					"owning_service":   svc,
					"producers":        producers,
					"consumers":        consumers,
					"producer_refs":    resolvePrefixedEntityRecords(grp, r.Slug, producers),
					"consumer_refs":    resolvePrefixedEntityRecords(grp, r.Slug, consumers),
				}
				// #1116: ScheduledJob entities (emitted by the scheduled-job pass for
				// Celery beat, APScheduler, node-cron, Spring @Scheduled, etc.) carry a
				// "schedule" property. Expose it as `scheduled: true` so the frontend
				// can render the clock icon and filter scheduled vs. on-demand tasks.
				stripped := dashStripScopePrefix(e.Kind)
				if stripped == kindScheduledJob {
					entry["scheduled"] = true
					if e.Properties["schedule"] != "" {
						entry["schedule"] = e.Properties["schedule"]
					}
				}

				// Accumulate broker_groups data (NATS goes into nats_subjects but
				// still counts in broker_groups).
				a := ensureBroker(canonical)
				a.count++
				a.services[svc]++
				a.repoSlugs[r.Slug] = struct{}{}
				if repoTS > a.lastIndexTS {
					a.lastIndexTS = repoTS
				}
				hasProducer := len(producers) > 0
				hasConsumer := len(consumers) > 0
				switch {
				case hasProducer && hasConsumer:
					a.healthSummary.Active++
				case hasProducer && !hasConsumer:
					a.orphanSubscribers++
					a.healthSummary.OrphanSubscriber++
				case !hasProducer && hasConsumer:
					a.orphanPublishers++
					a.healthSummary.OrphanPublisher++
				default:
					a.healthSummary.Orphan++
				}

				// NATS subjects (SCOPE.Queue with broker=nats) are surfaced in
				// the dedicated nats_subjects bucket so the frontend can render
				// them with the correct icon and filter logic. All other queues
				// (RabbitMQ, SQS, Pub/Sub, …) stay in the queues bucket.
				if broker == "nats" {
					resp.NatsSubjects = append(resp.NatsSubjects, entry)
				} else {
					resp.Queues = append(resp.Queues, entry)
				}

			case "channel":
				emitters, subscribers := channelEdges(r, e.ID)
				channelType := e.Properties["channel_type"]
				if channelType == "" {
					channelType = inferChannelType(e.Kind)
					if channelType == "websocket" && strings.HasPrefix(e.Name, "channel:redis-pubsub:") {
						channelType = "redis_pubsub"
					}
				}
				// Normalize redis pub/sub channel_type for frontend protocol matching.
				if channelType == "pubsub" && strings.HasPrefix(e.Name, "channel:redis-pubsub:") {
					channelType = "redis_pubsub"
				}
				entry := map[string]any{
					"id":           dashPrefixedID(r.Slug, e.ID),
					"repo":         r.Slug,
					"label":        e.Name,
					"channel_type": channelType,
					"emitters":     emitters,
					"subscribers":  subscribers,
				}
				resp.Channels = append(resp.Channels, entry)

			case "function":
				invokers, handlers := serverlessEdges(r, e.ID)
				provider := e.Properties["provider"]
				if provider == "" {
					provider = inferProviderFromID(e.ID)
				}
				entry := map[string]any{
					"id":       dashPrefixedID(r.Slug, e.ID),
					"repo":     r.Slug,
					"label":    e.Name,
					"provider": provider,
					"invokers": invokers,
					"handlers": handlers,
				}
				resp.Functions = append(resp.Functions, entry)

			case "subscription":
				// GraphQL subscriptions — emitted by applyGraphQLSubscriptionSynthesis.
				publishers, subscribers := graphqlSubEdges(r, e.ID)
				entry := map[string]any{
					"id":          dashPrefixedID(r.Slug, e.ID),
					"repo":        r.Slug,
					"label":       e.Name,
					"schema_type": e.Properties["schema_type"],
					"return_type": e.Properties["return_type"],
					"publishers":  publishers,
					"subscribers": subscribers,
				}
				resp.GraphQLSubscriptions = append(resp.GraphQLSubscriptions, entry)
			}
		}

		// Collect TRANSFORMS edges into the transforms bucket.
		for _, rel := range r.Doc.Relationships {
			if rel.Kind == "TRANSFORMS" {
				resp.Transforms = append(resp.Transforms, map[string]any{
					"from_id": dashPrefixedID(r.Slug, rel.FromID),
					"to_id":   dashPrefixedID(r.Slug, rel.ToID),
					"repo":    r.Slug,
				})
			}
		}
	}

	// Pre-compute crossRepoIDs from group links so the finalization block can
	// mark topics as cross-repo before the second-pass section runs.
	crossRepoIDsPre := map[string]struct{}{}
	for _, lnk := range grp.Links {
		k := strings.ToUpper(lnk.Kind)
		if k == "PUBLISHES_TO" || k == "SUBSCRIBES_TO" || k == "STREAMS_TO" || k == "STREAMS_FROM" {
			crossRepoIDsPre[lnk.Source] = struct{}{}
			crossRepoIDsPre[lnk.Target] = struct{}{}
		}
	}

	// --- Finalize merged topics (#1695) ---
	// Convert the topicByName dedup map into the Topics slice. Sort by key for
	// deterministic output. Count and health are computed here from the
	// fully-merged producer/consumer sets, so broker_groups reflects the true
	// topology (not a per-repo per-entity count).
	{
		keys := make([]string, 0, len(topicByName))
		for k := range topicByName {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, key := range keys {
			acc := topicByName[key]
			producers := acc.producerList
			consumers := acc.consumerList
			if producers == nil {
				producers = []string{}
			}
			if consumers == nil {
				consumers = []string{}
			}
			transformsTo := acc.transformsTo
			if transformsTo == nil {
				transformsTo = []string{}
			}
			appearsIn := acc.appearsIn
			if appearsIn == nil {
				appearsIn = []string{}
			}
			// Use the first repo in appearsIn as the primary repo for
			// resolvePrefixedEntityRecords (refs are already slug-prefixed).
			primaryRepo := ""
			if len(appearsIn) > 0 {
				primaryRepo = appearsIn[0]
			}
			entry := map[string]any{
				"id":               acc.topicID,
				"repo":             primaryRepo,
				"appears_in":       appearsIn,
				"label":            acc.name,
				"broker":           acc.rawBroker,
				"broker_canonical": acc.canonical,
				"owning_service":   acc.owningService,
				"producers":        producers,
				"consumers":        consumers,
				"producer_refs":    resolvePrefixedEntityRecords(grp, primaryRepo, producers),
				"consumer_refs":    resolvePrefixedEntityRecords(grp, primaryRepo, consumers),
				"transforms_to":    transformsTo,
			}
			resp.Topics = append(resp.Topics, entry)

			// Broker_groups health accounting — one count per merged topic.
			a := ensureBroker(acc.canonical)
			a.count++
			a.services[acc.owningService]++
			hasProducer := len(producers) > 0
			hasConsumer := len(consumers) > 0
			switch {
			case hasProducer && hasConsumer:
				a.healthSummary.Active++
			case hasProducer && !hasConsumer:
				a.orphanSubscribers++
				a.healthSummary.OrphanSubscriber++
			case !hasProducer && hasConsumer:
				a.orphanPublishers++
				a.healthSummary.OrphanPublisher++
			default:
				a.healthSummary.Orphan++
			}

			// Cross-repo detection: check whether any per-repo entity ID for
			// this merged topic appears in the cross-repo link set.
			for _, perRepoID := range acc.perRepoPrefixedIDs {
				if _, isCross := crossRepoIDsPre[perRepoID]; isCross {
					a.crossRepoTopicCount++
					break // count each merged topic at most once per broker
				}
			}
		}
	}

	// --- Second pass: compute cross_repo_topic_count for queues/nats ---
	// Topics are handled in the finalization block above (crossRepoIDsPre).
	// Queues and NATS subjects still need the cross-repo scan here using the
	// same crossRepoIDsPre set (per-repo prefixed entity IDs from grp.Links).
	for _, entry := range resp.Queues {
		id, _ := entry["id"].(string)
		canonical, _ := entry["broker_canonical"].(string)
		if canonical == "" {
			continue
		}
		if _, isCross := crossRepoIDsPre[id]; isCross {
			if a, ok := brokerAccums[canonical]; ok {
				a.crossRepoTopicCount++
			}
		}
	}
	for _, entry := range resp.NatsSubjects {
		id, _ := entry["id"].(string)
		canonical, _ := entry["broker_canonical"].(string)
		if canonical == "" {
			continue
		}
		if _, isCross := crossRepoIDsPre[id]; isCross {
			if a, ok := brokerAccums[canonical]; ok {
				a.crossRepoTopicCount++
			}
		}
	}

	// Build sorted broker_groups slice (alphabetical by broker name).
	brokerNames := make([]string, 0, len(brokerAccums))
	for b := range brokerAccums {
		brokerNames = append(brokerNames, b)
	}
	sort.Strings(brokerNames)

	for _, bname := range brokerNames {
		a := brokerAccums[bname]

		// Build services slice sorted alphabetically.
		svcNames := make([]string, 0, len(a.services))
		for s := range a.services {
			svcNames = append(svcNames, s)
		}
		sort.Strings(svcNames)
		svcs := make([]brokerServiceStat, 0, len(svcNames))
		for _, s := range svcNames {
			svcs = append(svcs, brokerServiceStat{Name: s, TopicCount: a.services[s]})
		}

		resp.BrokerGroups = append(resp.BrokerGroups, brokerGroup{
			Broker:              bname,
			Count:               a.count,
			Services:            svcs,
			OrphanPublishers:    a.orphanPublishers,
			OrphanSubscribers:   a.orphanSubscribers,
			CrossRepoTopicCount: a.crossRepoTopicCount,
			HealthSummary:       a.healthSummary,
			LastIndexTimestamp:  a.lastIndexTS,
		})
	}

	// --- Apply LLM enrichment operations (#1103) -----------------------------
	// merge / disqualify / rank / group across every topology bucket. The
	// EnrichmentOps store is derived from the SAME doc-file frontmatter that
	// applyTopologyEnrichment already reads above; this pass turns the data
	// into actual graph operations on the wire payload.
	if groupName != "" {
		ops := LoadEnrichmentOpsForGroup(groupName, docgenState)
		allKeptIDs := []string{}

		applyBucket := func(bucket []map[string]any) (kept, rejected []map[string]any) {
			k, r, _, _ := ops.ApplyToEntries(bucket)
			for _, e := range k {
				if id := EntryIDOf(e); id != "" {
					allKeptIDs = append(allKeptIDs, id)
				}
			}
			return k, r
		}

		resp.Topics, resp.TopicsRejected = applyBucket(resp.Topics)
		resp.Queues, resp.QueuesRejected = applyBucket(resp.Queues)
		resp.Channels, resp.ChannelsRejected = applyBucket(resp.Channels)
		resp.Functions, resp.FunctionsRejected = applyBucket(resp.Functions)

		// Unified group summary across all kept topology entries.
		resp.EnrichmentGroups = ops.SummarizeGroups(allKeptIDs)
	}

	// --- Apply per-surface confidence floor (#1129) --------------------------
	// Run AFTER the ops pass so explicit disqualifies still partition into
	// *_rejected and explicit ranks lift candidates above the floor. Entries
	// below the floor go into *_low_confidence (still queryable, but hidden
	// from the default UI list).
	//
	// Gated on groupName != "" — the legacy unit-test entry point
	// collectTopology(grp) passes "" and operates on minimal in-memory
	// fixtures whose entries deliberately lack source_file / framework signals;
	// scoring those would force every test fixture to satisfy a real-world
	// signal bar. Production handlers always pass a real group name.
	resp.TopicsLowConfidence = []map[string]any{}
	resp.QueuesLowConfidence = []map[string]any{}
	resp.ChannelsLowConfidence = []map[string]any{}
	resp.FunctionsLowConfidence = []map[string]any{}
	resp.ConfidenceFloor = FloorFor(SurfaceTopology)

	if groupName != "" {
		filterBucket := func(bucket []map[string]any) (kept, low []map[string]any) {
			fr := FilterByConfidence(SurfaceTopology, bucket, nil)
			return fr.Kept, fr.LowConfidence
		}
		resp.Topics, resp.TopicsLowConfidence = filterBucket(resp.Topics)
		resp.Queues, resp.QueuesLowConfidence = filterBucket(resp.Queues)
		resp.Channels, resp.ChannelsLowConfidence = filterBucket(resp.Channels)
		resp.Functions, resp.FunctionsLowConfidence = filterBucket(resp.Functions)
		resp.NoiseRejectedCount =
			len(resp.TopicsLowConfidence) +
				len(resp.QueuesLowConfidence) +
				len(resp.ChannelsLowConfidence) +
				len(resp.FunctionsLowConfidence)
	}

	return resp
}

// brokerEdges returns producers, consumers, and TRANSFORMS targets for a
// MessageTopic or Queue entity.
//
// #1404: TRIGGERS edges (emitted by the Celery scheduled-job pass) are read
// as consumer / subscriber edges so that the /topology view renders a
// publisher→topic→handler diagram instead of isolated topic circles.
// TRIGGERS direction: SCOPE.ScheduledJob:<jobID> → Function:<handlerName>;
// when FromID matches the entity ID the handler (ToID) is the consumer.
func brokerEdges(r *DashRepo, entityID string) (producers, consumers, transformsTo []string) {
	producers = []string{}
	consumers = []string{}
	transformsTo = []string{}
	for _, rel := range r.Doc.Relationships {
		switch rel.Kind {
		case "PUBLISHES_TO":
			if rel.ToID == entityID {
				producers = append(producers, dashPrefixedID(r.Slug, rel.FromID))
			}
		case "SUBSCRIBES_TO":
			if rel.FromID == entityID {
				consumers = append(consumers, dashPrefixedID(r.Slug, rel.ToID))
			}
			if rel.ToID == entityID {
				consumers = append(consumers, dashPrefixedID(r.Slug, rel.FromID))
			}
		case "TRANSFORMS":
			if rel.FromID == entityID {
				transformsTo = append(transformsTo, dashPrefixedID(r.Slug, rel.ToID))
			}
		case "READS_FROM":
			if rel.ToID == entityID {
				consumers = append(consumers, dashPrefixedID(r.Slug, rel.FromID))
			}
		case "WRITES_TO":
			if rel.ToID == entityID {
				producers = append(producers, dashPrefixedID(r.Slug, rel.FromID))
			}
		// #1404: Celery TRIGGERS — the handler function IS the subscriber/consumer
		// of a Celery task (SCOPE.ScheduledJob). The edge direction is
		// SCOPE.ScheduledJob:<jobID> → Function:<handler>, so FromID is the task
		// entity and ToID is the handler. This mirrors the SUBSCRIBES_TO pattern
		// used by Kafka/RabbitMQ consumers.
		case "TRIGGERS":
			if rel.FromID == entityID {
				consumers = append(consumers, dashPrefixedID(r.Slug, rel.ToID))
			}
		}
	}
	return
}

// channelEdges returns emitters and subscribers for a ChannelEvent or Redis
// pub/sub entity.  Redis pub/sub uses PUBLISHES_TO / SUBSCRIBES_TO (same as
// brokers) so we also include those edge kinds here.
func channelEdges(r *DashRepo, entityID string) (emitters, subscribers []string) {
	emitters = []string{}
	subscribers = []string{}
	for _, rel := range r.Doc.Relationships {
		switch rel.Kind {
		case "WS_EMITS", "STREAMS_TO", "GRAPHQL_PUBLISHES":
			if rel.ToID == entityID {
				emitters = append(emitters, dashPrefixedID(r.Slug, rel.FromID))
			}
		case "WS_SUBSCRIBES_TO", "STREAMS_FROM", "GRAPHQL_SUBSCRIBES":
			if rel.ToID == entityID {
				subscribers = append(subscribers, dashPrefixedID(r.Slug, rel.FromID))
			}
		// Redis pub/sub and similar emit PUBLISHES_TO / SUBSCRIBES_TO.
		case "PUBLISHES_TO":
			if rel.ToID == entityID {
				emitters = append(emitters, dashPrefixedID(r.Slug, rel.FromID))
			}
		case "SUBSCRIBES_TO":
			if rel.ToID == entityID {
				subscribers = append(subscribers, dashPrefixedID(r.Slug, rel.FromID))
			}
			if rel.FromID == entityID {
				subscribers = append(subscribers, dashPrefixedID(r.Slug, rel.ToID))
			}
		}
	}
	return
}

// serverlessEdges returns invokers (callers) and handlers for a
// ServerlessFunction entity.  Invokers arrive via CALLS edges; handlers via
// HANDLES edges.
func serverlessEdges(r *DashRepo, entityID string) (invokers, handlers []string) {
	invokers = []string{}
	handlers = []string{}
	for _, rel := range r.Doc.Relationships {
		switch rel.Kind {
		case "CALLS":
			if rel.ToID == entityID {
				invokers = append(invokers, dashPrefixedID(r.Slug, rel.FromID))
			}
		case "HANDLES":
			if rel.ToID == entityID {
				handlers = append(handlers, dashPrefixedID(r.Slug, rel.FromID))
			}
		}
	}
	return
}

// graphqlSubEdges returns publishers and subscribers for a GraphQL Subscription
// entity, scanning GRAPHQL_PUBLISHES and GRAPHQL_SUBSCRIBES edges.
func graphqlSubEdges(r *DashRepo, entityID string) (publishers, subscribers []string) {
	publishers = []string{}
	subscribers = []string{}
	for _, rel := range r.Doc.Relationships {
		switch rel.Kind {
		case "GRAPHQL_PUBLISHES":
			if rel.ToID == entityID {
				publishers = append(publishers, dashPrefixedID(r.Slug, rel.FromID))
			}
		case "GRAPHQL_SUBSCRIBES":
			if rel.ToID == entityID {
				subscribers = append(subscribers, dashPrefixedID(r.Slug, rel.FromID))
			}
		}
	}
	return
}

// inferChannelType guesses the channel type from entity properties / kind labels.
func inferChannelType(kind string) string {
	lower := strings.ToLower(kind)
	switch {
	case strings.Contains(lower, "graphql"):
		return "graphql_subscription"
	case strings.Contains(lower, "sse"):
		return "sse"
	default:
		return "websocket"
	}
}

// inferBrokerFromName guesses the broker name from the entity Name prefix.
// Used when the "broker" property is absent (e.g. Redis Streams, task queues).
func inferBrokerFromName(name string) string {
	switch {
	case strings.HasPrefix(name, "stream:redis:"):
		return "redis"
	case strings.HasPrefix(name, "task:dramatiq:"):
		return "dramatiq"
	case strings.HasPrefix(name, "task:rq:"):
		return "rq"
	case strings.HasPrefix(name, "task:celery:"):
		return "celery"
	case strings.HasPrefix(name, "task:hangfire:"):
		return "hangfire"
	case strings.HasPrefix(name, "task:quartz"):
		return "quartz"
	case strings.HasPrefix(name, "task:"):
		return "task-queue"
	default:
		return ""
	}
}

// brokerCanonical normalises a raw broker/framework string into one of the
// recognised canonical values: rabbitmq, redis, sqs, pubsub, nats, celery,
// dramatiq, or "unknown".
func brokerCanonical(broker, framework string) string {
	// Framework takes precedence for async-task entities (Celery, Dramatiq, …).
	switch strings.ToLower(framework) {
	case "celery", "celery_beat":
		return "celery"
	case "dramatiq":
		return "dramatiq"
	case "rq":
		return "rq"
	case "sidekiq":
		return "sidekiq"
	case "bullmq", "bull":
		return "bullmq"
	case "inngest":
		// Inngest event topics carry framework=inngest and no broker name. Map
		// them to their own canonical so the topology bands them under an Inngest
		// group and the frontend can render the event→function→event workflow with
		// Inngest-specific labeling instead of the generic "unknown" bucket (#5485).
		return "inngest"
	case "hangfire":
		return "hangfire"
	case "quartz", "quartz.net":
		return "quartz"
	}
	switch strings.ToLower(broker) {
	case "rabbitmq", "amqp":
		return "rabbitmq"
	case "redis":
		return "redis"
	case "sqs", "aws-sqs":
		return "sqs"
	case "pubsub", "gcp-pubsub", "google-pubsub":
		return "pubsub"
	case "nats":
		return "nats"
	case "celery":
		return "celery"
	case "dramatiq":
		return "dramatiq"
	case "kafka":
		return "kafka"
	case "":
		return "unknown"
	default:
		return strings.ToLower(broker)
	}
}

// owningService derives the logical service name for a topology entry.
// Resolution order:
//  1. entity Properties["service"]
//  2. repo slug (the service boundary in a mono-repo group)
func owningService(props map[string]string, repoSlug string) string {
	if svc := props["service"]; svc != "" {
		return svc
	}
	return repoSlug
}

// inferProviderFromID guesses the cloud provider from the entity ID prefix.
func inferProviderFromID(id string) string {
	switch {
	case strings.HasPrefix(id, "aws-lambda:"):
		return "aws-lambda"
	case strings.HasPrefix(id, "gcp-cloudfunction:"):
		return "gcp-cloudfunction"
	case strings.HasPrefix(id, "azure-function:"):
		return "azure-function"
	default:
		return "serverless"
	}
}

// applyTopologyEnrichment reads YAML frontmatter for a topology entity and
// merges enrichment fields (summary, group, rank, gaps, disqualified,
// enrichment) into the entry map. No-op when no doc file exists or when
// docgenState is nil.
//
// Matching strategy (in order):
//  1. Doc path contains the entity ID (exact substring match).
//  2. Doc path contains "topic" or "topology" AND the frontmatter kind is
//     "message_topic" — secondary signal for hashed entity IDs.
//
// When a match is found the resolved absolute file path is stored under
// "_doc_path" so callers can perform stale detection via os.Stat.
func applyTopologyEnrichment(entry map[string]any, group, entityID string, docgenState *mcp.DocgenState) {
	if docgenState == nil || docgenState.GeneratedPaths == nil {
		return
	}

	tryPath := func(fullPath string) bool {
		fm, fallback := extractEnrichmentFromFile(fullPath)
		if fm != nil && fm.HasData() {
			entry["docs_summary"] = fm.Summary
			entry["group"] = fm.Group
			entry["group_label"] = fm.GroupLabel
			entry["rank"] = fm.Rank
			entry["gaps"] = fm.Gaps
			entry["disqualified"] = fm.Disqualified
			entry["enrichment"] = fm
			entry["_doc_path"] = fullPath
			return true
		}
		if fallback != "" {
			entry["docs_summary"] = fallback
			entry["_doc_path"] = fullPath
			return true
		}
		return false
	}

	// Pass 1: entity ID substring match (reliable for non-hashed IDs).
	for _, docPath := range docgenState.GeneratedPaths {
		if !strings.Contains(docPath, entityID) {
			continue
		}
		if tryPath(getDocFilePath(group, docPath)) {
			return
		}
	}

	// Pass 2: topic/topology path + kind == "message_topic" in frontmatter
	// (handles hashed IDs where the path alone cannot be matched by ID).
	for _, docPath := range docgenState.GeneratedPaths {
		lower := strings.ToLower(docPath)
		if !strings.Contains(lower, "topic") && !strings.Contains(lower, "topology") {
			continue
		}
		fullPath := getDocFilePath(group, docPath)
		fm, fallback := extractEnrichmentFromFile(fullPath)
		if fm != nil && fm.HasData() && fm.Kind == "message_topic" {
			entry["docs_summary"] = fm.Summary
			entry["group"] = fm.Group
			entry["group_label"] = fm.GroupLabel
			entry["rank"] = fm.Rank
			entry["gaps"] = fm.Gaps
			entry["disqualified"] = fm.Disqualified
			entry["enrichment"] = fm
			entry["_doc_path"] = fullPath
			return
		}
		if fallback != "" {
			entry["docs_summary"] = fallback
			entry["_doc_path"] = fullPath
			return
		}
	}
}

// collectTopology is a compatibility shim used by the unit tests.  It
// delegates to collectTopologyResponse and unpacks the struct into the
// four slices the tests expect: (topics, queues, channels, functions).
func collectTopology(grp *DashGroup) (
	topics []map[string]any,
	queues []map[string]any,
	channels []map[string]any,
	functions []map[string]any,
) {
	resp := collectTopologyResponse(grp, "", nil)
	return resp.Topics, resp.Queues, resp.Channels, resp.Functions
}
