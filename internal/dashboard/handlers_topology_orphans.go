package dashboard

// handlers_topology_orphans.go — Topology v2: orphan publisher (#1136) and
// orphan subscriber (#1137) detectors.
//
// Orphan publishers:
//
//	GET /api/topology/{group}/orphan-publishers
//
// Returns every topic/queue/channel entity that has at least one producer
// (PUBLISHES_TO / WS_EMITS / STREAMS_TO / GRAPHQL_PUBLISHES / WRITES_TO)
// but zero consumers (SUBSCRIBES_TO / WS_SUBSCRIBES_TO / STREAMS_FROM /
// GRAPHQL_SUBSCRIBES / READS_FROM) anywhere in the group.
//
// These are "fire-and-forget" message producers with no known listener —
// a likely dead-letter or integration gap.
//
// Orphan subscribers:
//
//	GET /api/topology/{group}/orphan-subscribers
//
// Returns every topic/queue/channel entity that has at least one consumer
// but zero producers anywhere in the group. These are listeners waiting for
// messages that no known code publishes — typically dead consumers or
// misconfigured queue bindings.
//
// Detection algorithms:
//  1. Walk all entities in the group; keep only those whose topology bucket
//     is topic, queue, channel, or subscription.
//  2. For each such entity collect producers and consumers using the same
//     brokerEdges / channelEdges helpers used by collectTopologyResponse.
//  3. Orphan publisher: len(producers) > 0 && len(consumers) == 0.
//  4. Orphan subscriber: len(consumers) > 0 && len(producers) == 0.
//  5. Entities with zero producers AND zero consumers are NOT emitted by
//     either endpoint.
//
// Orphan-publisher wire shape:
//
//	{
//	  "orphan_publishers": [
//	    {
//	      "id":        "repo::entityId",
//	      "label":     "orders.created",
//	      "broker":    "rabbitmq",
//	      "framework": "",
//	      "repo":      "backend",
//	      "producers": ["repo::entityId1"],
//	      "reason":    "no_subscriber_found"
//	    }
//	  ],
//	  "total": N
//	}
//
// Orphan-subscriber wire shape:
//
//	{
//	  "orphan_subscribers": [
//	    {
//	      "id":               "repo::entityId",
//	      "label":            "orders.created",
//	      "broker":           "rabbitmq",
//	      "framework":        "",
//	      "repo":             "backend",
//	      "consumers":        ["repo::entityId1"],
//	      "reason":           "no_publisher_found",
//	      "last_message_seen": null
//	    }
//	  ],
//	  "total": N
//	}
//
// All array fields marshal as [] (never null). last_message_seen is always
// null today but the field is reserved for future timestamp population.

import (
	"net/http"
	"sort"
)

// OrphanPublisherRow is one entity returned by the orphan-publisher endpoint.
type OrphanPublisherRow struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Broker    string   `json:"broker"`
	Framework string   `json:"framework"`
	Repo      string   `json:"repo"`
	Producers []string `json:"producers"`
	Reason    string   `json:"reason"`
}

// orphanPublisherReason is the reason value surfaced to callers.
const reasonNoSubscriberFound = "no_subscriber_found"

// handleOrphanPublishers — GET /api/topology/{group}/orphan-publishers
func (s *Server) handleOrphanPublishers(w http.ResponseWriter, r *http.Request) {
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

	rows := collectOrphanPublishers(grp)

	writeJSON(w, http.StatusOK, map[string]any{
		"orphan_publishers": rows,
		"total":             len(rows),
	})
}

// collectOrphanPublishers runs the orphan-publisher detection pass against a
// loaded group. Extracted so unit tests can call it without HTTP scaffolding.
func collectOrphanPublishers(grp *DashGroup) []OrphanPublisherRow {
	var rows []OrphanPublisherRow

	for _, r := range sortedRepos(grp) {
		if r.Doc == nil {
			continue
		}

		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			bucket := classifyTopologyBucket(e.Kind, e.Name, e.PropsSnapshot())

			// Only inspect buckets that use broker/channel semantics.
			switch bucket {
			case "topic", "queue", "channel", "subscription":
				// handled below
			default:
				continue
			}

			var producers, consumers []string

			switch bucket {
			case "topic", "queue":
				producers, consumers, _ = brokerEdges(r, e.ID)
			case "channel":
				producers, consumers = channelEdges(r, e.ID)
			case "subscription":
				producers, consumers = graphqlSubEdges(r, e.ID)
			}

			// Emit only when there is at least one producer and zero consumers.
			if len(producers) == 0 || len(consumers) > 0 {
				continue
			}

			broker := e.PropGet("broker")
			if broker == "" {
				broker = inferBrokerFromName(e.Name)
			}
			framework := e.PropGet("framework")

			row := OrphanPublisherRow{
				ID:        dashPrefixedID(r.Slug, e.ID),
				Label:     e.Name,
				Broker:    broker,
				Framework: framework,
				Repo:      r.Slug,
				Producers: producers,
				Reason:    reasonNoSubscriberFound,
			}
			rows = append(rows, row)
		}
	}

	// Stable deterministic sort: repo → label.
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		return a.Label < b.Label
	})

	if rows == nil {
		rows = []OrphanPublisherRow{}
	}
	return rows
}

// ─────────────────────────────────────────────────────────────────────────────
// Orphan subscriber detector (#1137)
// ─────────────────────────────────────────────────────────────────────────────

// OrphanSubscriberRow is one entity returned by the orphan-subscriber endpoint.
type OrphanSubscriberRow struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Broker          string   `json:"broker"`
	Framework       string   `json:"framework"`
	Repo            string   `json:"repo"`
	Consumers       []string `json:"consumers"`
	Reason          string   `json:"reason"`
	LastMessageSeen *string  `json:"last_message_seen"` // always null today; field reserved
}

// reason constants for the orphan-subscriber endpoint.
const (
	reasonNoPublisherFound        = "no_publisher_found"
	reasonPublisherOnlyInExternal = "publisher_only_in_external_lib"
)

// handleOrphanSubscribers — GET /api/topology/{group}/orphan-subscribers
func (s *Server) handleOrphanSubscribers(w http.ResponseWriter, r *http.Request) {
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

	rows := collectOrphanSubscribers(grp)

	writeJSON(w, http.StatusOK, map[string]any{
		"orphan_subscribers": rows,
		"total":              len(rows),
	})
}

// collectOrphanSubscribers runs the orphan-subscriber detection pass against a
// loaded group. Extracted so unit tests can call it without HTTP scaffolding.
func collectOrphanSubscribers(grp *DashGroup) []OrphanSubscriberRow {
	var rows []OrphanSubscriberRow

	for _, r := range sortedRepos(grp) {
		if r.Doc == nil {
			continue
		}

		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			bucket := classifyTopologyBucket(e.Kind, e.Name, e.PropsSnapshot())

			// Only inspect buckets that use broker/channel semantics.
			switch bucket {
			case "topic", "queue", "channel", "subscription":
				// handled below
			default:
				continue
			}

			var producers, consumers []string

			switch bucket {
			case "topic", "queue":
				producers, consumers, _ = brokerEdges(r, e.ID)
			case "channel":
				// channelEdges returns (emitters, subscribers); map to producers/consumers.
				producers, consumers = channelEdges(r, e.ID)
			case "subscription":
				// graphqlSubEdges returns (publishers, subscribers).
				producers, consumers = graphqlSubEdges(r, e.ID)
			}

			// Emit only when there is at least one consumer and zero producers.
			if len(consumers) == 0 || len(producers) > 0 {
				continue
			}

			broker := e.PropGet("broker")
			if broker == "" {
				broker = inferBrokerFromName(e.Name)
			}
			framework := e.PropGet("framework")

			// Reason classification: if the entity properties hint that the
			// publisher lives in an external library, surface that explicitly;
			// otherwise default to no_publisher_found.
			reason := reasonNoPublisherFound
			if e.PropGet("publisher_source") == "external" ||
				e.PropGet("producer_source") == "external" {
				reason = reasonPublisherOnlyInExternal
			}

			row := OrphanSubscriberRow{
				ID:              dashPrefixedID(r.Slug, e.ID),
				Label:           e.Name,
				Broker:          broker,
				Framework:       framework,
				Repo:            r.Slug,
				Consumers:       consumers,
				Reason:          reason,
				LastMessageSeen: nil, // reserved for future timestamp
			}
			rows = append(rows, row)
		}
	}

	// Stable deterministic sort: repo → label → first consumer label.
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		if a.Label != b.Label {
			return a.Label < b.Label
		}
		// Tertiary: first consumer ID for full stability.
		if len(a.Consumers) > 0 && len(b.Consumers) > 0 {
			return a.Consumers[0] < b.Consumers[0]
		}
		return len(a.Consumers) < len(b.Consumers)
	})

	if rows == nil {
		rows = []OrphanSubscriberRow{}
	}
	return rows
}
