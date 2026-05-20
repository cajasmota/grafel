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
)

// Broker entity kinds (suffix after stripping the optional "SCOPE." prefix).
const (
	kindMessageTopic    = "MessageTopic"
	kindQueue           = "Queue"
	kindChannelEvent    = "ChannelEvent"
	kindSubscription    = "Subscription" // GraphQL subscriptions
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

	writeJSON(w, http.StatusOK, collectTopologyResponse(grp))
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

	writeJSON(w, http.StatusOK, collectTopologyResponse(grp))
}

// collectTopologyResponse builds the full topology wire payload from a loaded
// group. All slice fields are initialised to non-nil empty slices so that
// JSON encoding produces [] (not null) when no data exists — fixing the
// frontend error boundary triggered by null nats_subjects / graphql_subscriptions
// on groups with no NATS or GraphQL edges (#944).
func collectTopologyResponse(grp *DashGroup) topologyResponse {
	resp := topologyResponse{
		Topics:               []map[string]any{},
		Queues:               []map[string]any{},
		Channels:             []map[string]any{},
		NatsSubjects:         []map[string]any{},
		GraphQLSubscriptions: []map[string]any{},
		Transforms:           []map[string]any{},
	}

	for _, r := range sortedRepos(grp) {
		if r.Doc == nil {
			continue
		}

		// For each broker entity, collect producers and consumers from edges.
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			kind := dashStripScopePrefix(e.Kind)

			switch kind {
			case kindMessageTopic:
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
				resp.Topics = append(resp.Topics, entry)

			case kindQueue:
				producers, consumers, _ := brokerEdges(r, e.ID)
				broker := e.Properties["broker"]
				entry := map[string]any{
					"id":        dashPrefixedID(r.Slug, e.ID),
					"repo":      r.Slug,
					"label":     e.Name,
					"broker":    broker,
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

			case kindChannelEvent:
				emitters, subscribers := channelEdges(r, e.ID)
				channelType := e.Properties["channel_type"]
				if channelType == "" {
					channelType = inferChannelType(e.Kind)
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

			case kindSubscription:
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

// collectTopology is retained for callers that only need the three core
// buckets (topics, queues, channels). It delegates to collectTopologyResponse
// for consistency.
func collectTopology(grp *DashGroup) (topics, queues, channels []map[string]any) {
	resp := collectTopologyResponse(grp)
	return resp.Topics, resp.Queues, resp.Channels
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

// channelEdges returns emitters and subscribers for a ChannelEvent entity.
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
