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
	"strings"

	"github.com/cajasmota/archigraph/internal/mcp"
)

// Broker entity kinds (suffix after stripping the optional "SCOPE." prefix).
const (
	kindMessageTopic       = "MessageTopic"
	kindQueue              = "Queue"
	kindChannelEvent       = "ChannelEvent"
	kindSubscription       = "Subscription"       // GraphQL subscriptions
	kindServerlessFunction = "ServerlessFunction" // SCOPE.ServerlessFunction stripped
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
	}

	for _, r := range sortedRepos(grp) {
		if r.Doc == nil {
			continue
		}

		// For each entity, classify into a topology bucket and collect edges.
		// classifyTopologyBucket uses the entity Name (not hashed ID) because
		// synthetic runtime entities store the semantic prefix in the Name field.
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			bucket := classifyTopologyBucket(e.Kind, e.Name, e.Properties)

			switch bucket {
			case "topic":
				producers, consumers, transformsTo := brokerEdges(r, e.ID)
				entry := map[string]any{
					"id":            dashPrefixedID(r.Slug, e.ID),
					"repo":          r.Slug,
					"label":         e.Name,
					"broker":        e.Properties["broker"],
					"producers":     producers,
					"consumers":     consumers,
					"transforms_to": transformsTo,
				}
				// Enrich topic with frontmatter when available.
				if groupName != "" {
					applyTopologyEnrichment(entry, groupName, e.ID, docgenState)
				}
				resp.Topics = append(resp.Topics, entry)

			case "queue":
				producers, consumers, _ := brokerEdges(r, e.ID)
				broker := e.Properties["broker"]
				if broker == "" {
					// Fall back to inferring from the entity Name prefix.
					broker = inferBrokerFromName(e.Name)
				}
				// Async task queues carry framework info instead of a broker name.
				framework := e.Properties["framework"]
				entry := map[string]any{
					"id":        dashPrefixedID(r.Slug, e.ID),
					"repo":      r.Slug,
					"label":     e.Name,
					"broker":    broker,
					"framework": framework,
					"producers": producers,
					"consumers": consumers,
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

	return resp
}

// brokerEdges returns producers, consumers, and TRANSFORMS targets for a
// MessageTopic or Queue entity.
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
func applyTopologyEnrichment(entry map[string]any, group, entityID string, docgenState *mcp.DocgenState) {
	if docgenState == nil || docgenState.GeneratedPaths == nil {
		return
	}
	for _, docPath := range docgenState.GeneratedPaths {
		if !strings.Contains(docPath, entityID) &&
			!strings.Contains(strings.ToLower(docPath), "topic") &&
			!strings.Contains(strings.ToLower(docPath), "topology") {
			continue
		}
		fullPath := getDocFilePath(group, docPath)
		fm, fallback := extractEnrichmentFromFile(fullPath)
		if fm != nil && fm.HasData() {
			entry["docs_summary"] = fm.Summary
			entry["group"] = fm.Group
			entry["group_label"] = fm.GroupLabel
			entry["rank"] = fm.Rank
			entry["gaps"] = fm.Gaps
			entry["disqualified"] = fm.Disqualified
			entry["enrichment"] = fm
			return
		}
		if fallback != "" {
			entry["docs_summary"] = fallback
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
