package dashboard

// handlers_topology.go — Broker Topology endpoint
//
//	GET /api/topology/{group}
//	GET /api/groups/{group}/topics

import (
	"net/http"
	"strings"
)

// Broker entity kinds.
const (
	kindMessageTopic = "MessageTopic"
	kindQueue        = "Queue"
	kindChannelEvent = "ChannelEvent"
)

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

	topics, queues, channels := collectTopology(grp)

	writeJSON(w, http.StatusOK, map[string]any{
		"topics":   topics,
		"queues":   queues,
		"channels": channels,
	})
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

	topics, queues, channels := collectTopology(grp)

	writeJSON(w, http.StatusOK, map[string]any{
		"topics":   topics,
		"queues":   queues,
		"channels": channels,
	})
}

// collectTopology extracts broker topology from a loaded group.
func collectTopology(grp *DashGroup) (topics, queues, channels []map[string]any) {
	topics = []map[string]any{}
	queues = []map[string]any{}
	channels = []map[string]any{}

	for _, r := range sortedRepos(grp) {
		if r.Doc == nil {
			continue
		}

		// Build a quick index: entity local-ID -> entity index.
		idIdx := map[string]int{}
		for i := range r.Doc.Entities {
			idIdx[r.Doc.Entities[i].ID] = i
		}

		// For each broker entity, collect producers and consumers from edges.
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			kind := dashStripScopePrefix(e.Kind)

			switch kind {
			case kindMessageTopic:
				producers, consumers, transformsTo := brokerEdges(r, e.ID)
				entry := map[string]any{
					"id":           dashPrefixedID(r.Slug, e.ID),
					"repo":         r.Slug,
					"label":        e.Name,
					"broker":       e.Properties["broker"],
					"producers":    producers,
					"consumers":    consumers,
					"transforms_to": transformsTo,
				}
				topics = append(topics, entry)

			case kindQueue:
				producers, consumers, _ := brokerEdges(r, e.ID)
				entry := map[string]any{
					"id":        dashPrefixedID(r.Slug, e.ID),
					"repo":      r.Slug,
					"label":     e.Name,
					"broker":    e.Properties["broker"],
					"producers": producers,
					"consumers": consumers,
				}
				queues = append(queues, entry)

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
				channels = append(channels, entry)
			}
		}
	}
	return
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
