package dashboard

// handlers_topology_detail.go — per-topic detail endpoint
//
//	GET /api/topology/{group}/topic/{topicId}
//
// Returns the full detail record for one topology entity (topic, queue,
// channel, NATS subject, scheduled job, or serverless function).  The
// topicId uses the same "repo::localID" prefixed format as every other
// dashboard detail endpoint.

import (
	"net/http"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/mcp"
)

// topicEntityRecord is the resolved producer/consumer wire shape — matches
// what the Paths v2 detail panel uses for step entities.
type topicEntityRecord struct {
	EntityID   string `json:"entity_id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	SourceFile string `json:"source_file"`
	StartLine  int    `json:"start_line"`
	Repo       string `json:"repo"`
}

// topicDetailResponse is the wire shape for GET /api/topology/{group}/topic/{topicId}.
// All slice fields are guaranteed non-nil (JSON [] not null).
type topicDetailResponse struct {
	ID             string              `json:"id"`
	Label          string              `json:"label"`
	Protocol       string              `json:"protocol"`
	Broker         string              `json:"broker"`
	Framework      string              `json:"framework"`
	Schedule       string              `json:"schedule"`
	Scheduled      bool                `json:"scheduled"`
	MessageSchema  string              `json:"message_schema"`
	Repo           string              `json:"repo"`
	SourceFile     string              `json:"source_file"`
	StartLine      int                 `json:"start_line"`
	Producers      []topicEntityRecord `json:"producers"`
	Consumers      []topicEntityRecord `json:"consumers"`
	Tests          []topicEntityRecord `json:"tests"`
	RelatedTopics  []string            `json:"related_topics"`
	UsageHistory   []any               `json:"usage_history"`
	FlowCount      int                 `json:"flow_count"`
	CrossRepo      bool                `json:"cross_repo"`
	LifecycleState string              `json:"lifecycle_state"`
	DocsSummary    string              `json:"docs_summary,omitempty"`
	Enrichment     *EnrichmentFrontmatter `json:"enrichment,omitempty"`
}

// handleTopicDetail — GET /api/topology/{group}/topic/{topicId}
func (s *Server) handleTopicDetail(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	topicID := r.PathValue("topicId")
	if group == "" || topicID == "" {
		writeErr(w, http.StatusBadRequest, "group and topicId required")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	detail, found := buildTopicDetail(grp, group, topicID)
	if !found {
		writeErr(w, http.StatusNotFound, "topic not found: "+topicID)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// buildTopicDetail constructs the full detail payload for a single topology
// entity identified by its prefixed ID.  Returns (detail, true) when found.
func buildTopicDetail(grp *DashGroup, groupName, topicID string) (topicDetailResponse, bool) {
	repoHint, localID := dashSplitPrefixed(topicID)

	// --- Locate the topic entity ---
	var topicRepoSlug string
	var topicEnt *graph.Entity

	for _, r := range sortedRepos(grp) {
		if repoHint != "" && r.Slug != repoHint {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			bucket := classifyTopologyBucket(e.Kind, e.Name, e.Properties)
			if bucket == "" {
				continue
			}
			if e.ID == localID || dashPrefixedID(r.Slug, e.ID) == topicID {
				topicRepoSlug = r.Slug
				topicEnt = e
				break
			}
		}
		if topicEnt != nil {
			break
		}
	}

	if topicEnt == nil {
		return topicDetailResponse{}, false
	}

	// --- Infer broker, protocol, framework ---
	broker := topicEnt.Properties["broker"]
	if broker == "" {
		broker = inferBrokerFromName(topicEnt.Name)
	}
	framework := topicEnt.Properties["framework"]
	protocol := inferProtocol(topicEnt.Kind, topicEnt.Name, topicEnt.Properties, broker)

	// --- Schedule fields ---
	stripped := dashStripScopePrefix(topicEnt.Kind)
	scheduled := stripped == kindScheduledJob
	schedule := topicEnt.Properties["schedule"]

	// --- Message schema ---
	messageSchema := topicEnt.Properties["schema"]

	// --- Resolve producers and consumers ---
	// Scan ALL repos: cross-repo extractors may store relationships in any repo's
	// doc whose edges reference the topic entity by its local ID.
	rawProducerIDs, rawConsumerIDs := collectAllProducerConsumerIDs(grp, topicEnt.ID, dashPrefixedID(topicRepoSlug, topicEnt.ID))

	// Deduplicate SUBSCRIBES_TO which may add same ID twice (from→to and to→from).
	rawConsumerIDs = dedupStrings(rawConsumerIDs)
	rawProducerIDs = dedupStrings(rawProducerIDs)

	producers := resolveEntityRecords(grp, topicRepoSlug, rawProducerIDs)
	consumers := resolveEntityRecords(grp, topicRepoSlug, rawConsumerIDs)

	// Lifecycle is derived from how many edges exist, regardless of whether the
	// peer entities resolve to known entity records.
	nRawProducers := len(rawProducerIDs)
	nRawConsumers := len(rawConsumerIDs)

	// --- Tests: entities that TESTS this topic ---
	tests := resolveTestEntities(grp, topicRepoSlug, topicEnt.ID, topicID)

	// --- Related topics: topics sharing a producer or consumer ---
	relatedTopics := findRelatedTopics(grp, topicRepoSlug, topicEnt.ID, rawProducerIDs, rawConsumerIDs)

	// --- Flow count: number of Process flows whose STEP_IN_PROCESS chain includes this topic ---
	flowCount := countFlowsTraversing(grp, topicEnt.ID, dashPrefixedID(topicRepoSlug, topicEnt.ID))

	// --- Cross-repo: any producer or consumer in a different repo? ---
	crossRepo := isCrossRepo(topicRepoSlug, producers, consumers)

	// --- Lifecycle state ---
	lifecycleState := computeLifecycleState(nRawProducers, nRawConsumers)

	// --- Enrichment ---
	docgenState, _ := mcp.LoadDocgenState(groupName)
	entry := map[string]any{}
	if groupName != "" {
		applyTopologyEnrichment(entry, groupName, topicEnt.ID, docgenState)
	}
	docsSummary, _ := entry["docs_summary"].(string)
	var enrichment *EnrichmentFrontmatter
	if fm, ok := entry["enrichment"].(*EnrichmentFrontmatter); ok {
		enrichment = fm
	}

	resp := topicDetailResponse{
		ID:             dashPrefixedID(topicRepoSlug, topicEnt.ID),
		Label:          topicEnt.Name,
		Protocol:       protocol,
		Broker:         broker,
		Framework:      framework,
		Schedule:       schedule,
		Scheduled:      scheduled,
		MessageSchema:  messageSchema,
		Repo:           topicRepoSlug,
		SourceFile:     topicEnt.SourceFile,
		StartLine:      topicEnt.StartLine,
		Producers:      producers,
		Consumers:      consumers,
		Tests:          tests,
		RelatedTopics:  relatedTopics,
		UsageHistory:   []any{},
		FlowCount:      flowCount,
		CrossRepo:      crossRepo,
		LifecycleState: lifecycleState,
		DocsSummary:    docsSummary,
		Enrichment:     enrichment,
	}
	return resp, true
}

// collectAllProducerConsumerIDs scans every repo in the group for relationships
// that reference the topic entity (by local ID or full prefixed ID) and returns
// all producer/consumer local IDs.  This handles cross-repo extractors that
// store edges in a different repo's document.
func collectAllProducerConsumerIDs(grp *DashGroup, localEntityID, prefixedID string) (producers, consumers []string) {
	for _, r := range sortedRepos(grp) {
		for _, rel := range r.Doc.Relationships {
			isTarget := rel.ToID == localEntityID || dashPrefixedID(r.Slug, rel.ToID) == prefixedID
			isSource := rel.FromID == localEntityID || dashPrefixedID(r.Slug, rel.FromID) == prefixedID
			switch rel.Kind {
			case "PUBLISHES_TO", "WRITES_TO":
				if isTarget {
					producers = append(producers, rel.FromID)
				}
			case "SUBSCRIBES_TO":
				if isTarget {
					consumers = append(consumers, rel.FromID)
				}
				if isSource {
					consumers = append(consumers, rel.ToID)
				}
			case "READS_FROM":
				if isTarget {
					consumers = append(consumers, rel.FromID)
				}
			}
		}
	}
	return
}

// resolveProducerConsumerIDs returns the raw local IDs for producers and
// consumers of the given entity from a single repo's relationship list.
// Used internally by findRelatedTopics which needs per-repo edge data.
func resolveProducerConsumerIDs(doc *graph.Document, entityID string) (producers, consumers []string) {
	for _, rel := range doc.Relationships {
		switch rel.Kind {
		case "PUBLISHES_TO", "WRITES_TO":
			if rel.ToID == entityID {
				producers = append(producers, rel.FromID)
			}
		case "SUBSCRIBES_TO":
			if rel.ToID == entityID {
				consumers = append(consumers, rel.FromID)
			}
			if rel.FromID == entityID {
				consumers = append(consumers, rel.ToID)
			}
		case "READS_FROM":
			if rel.ToID == entityID {
				consumers = append(consumers, rel.FromID)
			}
		}
	}
	return
}

// resolveEntityRecords turns a list of local entity IDs (within ownerRepo) into
// full topicEntityRecord values.  Cross-repo matches are searched by scanning
// all repos in the group (the IDs stored in producer/consumer edges are always
// local to the repo that contains the relationship).
func resolveEntityRecords(grp *DashGroup, ownerRepo string, localIDs []string) []topicEntityRecord {
	if len(localIDs) == 0 {
		return []topicEntityRecord{}
	}
	out := make([]topicEntityRecord, 0, len(localIDs))
	for _, localID := range localIDs {
		found := false
		// First try the owning repo for speed.
		if r, ok := grp.Repos[ownerRepo]; ok && r.Doc != nil {
			for i := range r.Doc.Entities {
				e := &r.Doc.Entities[i]
				if e.ID == localID {
					out = append(out, entityToRecord(ownerRepo, e))
					found = true
					break
				}
			}
		}
		if found {
			continue
		}
		// Search all repos.
		for _, r := range sortedRepos(grp) {
			if r.Slug == ownerRepo {
				continue
			}
			for i := range r.Doc.Entities {
				e := &r.Doc.Entities[i]
				if e.ID == localID {
					out = append(out, entityToRecord(r.Slug, e))
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}
	return out
}

func entityToRecord(repo string, e *graph.Entity) topicEntityRecord {
	return topicEntityRecord{
		EntityID:   dashPrefixedID(repo, e.ID),
		Name:       e.Name,
		Kind:       dashStripScopePrefix(e.Kind),
		SourceFile: e.SourceFile,
		StartLine:  e.StartLine,
		Repo:       repo,
	}
}

// resolveTestEntities finds entities that have a TESTS relationship pointing at
// the topic entity.
func resolveTestEntities(grp *DashGroup, ownerRepo, localEntityID, prefixedID string) []topicEntityRecord {
	out := []topicEntityRecord{}
	for _, r := range sortedRepos(grp) {
		for _, rel := range r.Doc.Relationships {
			if rel.Kind != "TESTS" {
				continue
			}
			if rel.ToID != localEntityID && dashPrefixedID(r.Slug, rel.ToID) != prefixedID {
				continue
			}
			// Resolve the test entity.
			for i := range r.Doc.Entities {
				e := &r.Doc.Entities[i]
				if e.ID == rel.FromID {
					out = append(out, entityToRecord(r.Slug, e))
					break
				}
			}
		}
	}
	return out
}

// findRelatedTopics returns prefixed IDs of other topology entities that share
// at least one producer or consumer with the current entity.
func findRelatedTopics(grp *DashGroup, ownerRepo, localEntityID string, producerIDs, consumerIDs []string) []string {
	peerSet := make(map[string]struct{})
	for pid := range producerIDs {
		_ = pid // iterate index
	}

	// Build a set of connected entity IDs for fast lookup.
	connectedSet := make(map[string]struct{}, len(producerIDs)+len(consumerIDs))
	for _, id := range producerIDs {
		connectedSet[id] = struct{}{}
	}
	for _, id := range consumerIDs {
		connectedSet[id] = struct{}{}
	}

	for _, r := range sortedRepos(grp) {
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if e.ID == localEntityID && r.Slug == ownerRepo {
				continue // skip self
			}
			bucket := classifyTopologyBucket(e.Kind, e.Name, e.Properties)
			if bucket == "" {
				continue
			}
			// Check whether this topology entity shares any connected entity.
			peers, peerConsumers := resolveProducerConsumerIDs(r.Doc, e.ID)
			for _, id := range append(peers, peerConsumers...) {
				if _, ok := connectedSet[id]; ok {
					peerSet[dashPrefixedID(r.Slug, e.ID)] = struct{}{}
					break
				}
			}
		}
	}

	out := make([]string, 0, len(peerSet))
	for id := range peerSet {
		out = append(out, id)
	}
	return out
}

// countFlowsTraversing counts how many Process entities have at least one
// STEP_IN_PROCESS edge whose target entity is (or refers to) the topic entity.
func countFlowsTraversing(grp *DashGroup, localEntityID, prefixedID string) int {
	processSet := make(map[string]struct{})
	for _, r := range sortedRepos(grp) {
		for _, rel := range r.Doc.Relationships {
			if rel.Kind != stepInProcessEdge {
				continue
			}
			if rel.ToID == localEntityID || dashPrefixedID(r.Slug, rel.ToID) == prefixedID {
				processSet[dashPrefixedID(r.Slug, rel.FromID)] = struct{}{}
			}
		}
	}
	return len(processSet)
}

// isCrossRepo returns true when any resolved producer or consumer entity
// belongs to a different repo than ownerRepo.
func isCrossRepo(ownerRepo string, producers, consumers []topicEntityRecord) bool {
	for _, rec := range producers {
		if rec.Repo != ownerRepo {
			return true
		}
	}
	for _, rec := range consumers {
		if rec.Repo != ownerRepo {
			return true
		}
	}
	return false
}

// computeLifecycleState derives the lifecycle label from the presence of
// producers and consumers.
//
//   - active            — has both producers AND consumers
//   - orphan_publisher  — has producers but no consumers
//   - orphan_subscriber — has consumers but no producers
//   - orphan            — neither
func computeLifecycleState(nProducers, nConsumers int) string {
	switch {
	case nProducers > 0 && nConsumers > 0:
		return "active"
	case nProducers > 0:
		return "orphan_publisher"
	case nConsumers > 0:
		return "orphan_subscriber"
	default:
		return "orphan"
	}
}

// inferProtocol derives a human-readable protocol name from entity kind /
// name / properties, useful for the UI badge.
func inferProtocol(kind, name string, props map[string]string, broker string) string {
	stripped := dashStripScopePrefix(kind)
	switch stripped {
	case kindMessageTopic:
		if broker != "" {
			return broker
		}
		return "message_topic"
	case kindQueue, kindTask, kindScheduledJob:
		if fw := props["framework"]; fw != "" {
			return fw
		}
		if broker != "" {
			return broker
		}
		return "queue"
	case kindChannelEvent:
		if ct := props["channel_type"]; ct != "" {
			return ct
		}
		return "channel"
	case kindSubscription:
		return "graphql_subscription"
	case kindServerlessFunction:
		if p := props["provider"]; p != "" {
			return p
		}
		return "serverless"
	}
	switch {
	case strings.HasPrefix(name, "channel:redis-pubsub:"):
		return "redis_pubsub"
	case strings.HasPrefix(name, "stream:redis:"):
		return "redis_stream"
	case strings.HasPrefix(name, "aws-lambda:"):
		return "aws-lambda"
	case strings.HasPrefix(name, "gcp-cloudfunction:"):
		return "gcp-cloudfunction"
	case strings.HasPrefix(name, "azure-function:"):
		return "azure-function"
	}
	return "unknown"
}

// dedupStrings returns a copy of sl with duplicates removed, preserving order.
func dedupStrings(sl []string) []string {
	seen := make(map[string]struct{}, len(sl))
	out := make([]string, 0, len(sl))
	for _, s := range sl {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
