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
	"os"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/mcp"
)

// topicEntityRecord is the resolved producer/consumer wire shape — matches
// what the Paths v2 detail panel uses for step entities.
//
// FlowProcessIDs lists prefixed Process entity IDs where this entity appears
// as a STEP_IN_PROCESS step within the same flow that also contains the
// channel (#1943 ↗ flow action).  Empty slice when no such flow exists.
type topicEntityRecord struct {
	EntityID       string   `json:"entity_id"`
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	SourceFile     string   `json:"source_file"`
	StartLine      int      `json:"start_line"`
	Repo           string   `json:"repo"`
	FlowProcessIDs []string `json:"flow_process_ids"`
	// Framework labels the entity's messaging framework (e.g. "inngest") so the
	// detail panel can badge an Inngest function/event distinctly (#5485).
	Framework string `json:"framework,omitempty"`
	// InngestSteps lists the durable step structure of an Inngest function
	// (#5484's inngest_step Operation children, CONTAINED by the Function). Only
	// populated for Inngest functions; empty for everything else (#5485). Lets
	// the topology detail panel show a function's durable steps inline.
	InngestSteps []inngestStepRecord `json:"inngest_steps,omitempty"`
}

// inngestStepRecord is one durable step of an Inngest function — a #5484
// inngest_step Operation. step_kind is one of run/sleep/sleepUntil/
// waitForEvent/invoke; wait_event / invoke_target are populated for the
// cross-function step kinds.
type inngestStepRecord struct {
	StepID       string `json:"step_id"`
	StepKind     string `json:"step_kind"`
	SourceFile   string `json:"source_file,omitempty"`
	StartLine    int    `json:"start_line,omitempty"`
	WaitEvent    string `json:"wait_event,omitempty"`
	InvokeTarget string `json:"invoke_target,omitempty"`
}

// topicDetailResponse is the wire shape for GET /api/topology/{group}/topic/{topicId}.
// All slice fields are guaranteed non-nil (JSON [] not null).
type topicDetailResponse struct {
	ID             string                 `json:"id"`
	Label          string                 `json:"label"`
	Protocol       string                 `json:"protocol"`
	Broker         string                 `json:"broker"`
	Framework      string                 `json:"framework"`
	Schedule       string                 `json:"schedule"`
	Scheduled      bool                   `json:"scheduled"`
	MessageSchema  string                 `json:"message_schema"`
	Repo           string                 `json:"repo"`
	SourceFile     string                 `json:"source_file"`
	StartLine      int                    `json:"start_line"`
	Producers      []topicEntityRecord    `json:"producers"`
	Consumers      []topicEntityRecord    `json:"consumers"`
	Tests          []topicEntityRecord    `json:"tests"`
	RelatedTopics  []string               `json:"related_topics"`
	UsageHistory   []any                  `json:"usage_history"`
	FlowCount      int                    `json:"flow_count"`
	CrossRepo      bool                   `json:"cross_repo"`
	LifecycleState string                 `json:"lifecycle_state"`
	DocsSummary    string                 `json:"docs_summary,omitempty"`
	Enrichment     *EnrichmentFrontmatter `json:"enrichment,omitempty"`
	// DocgenStatus is "enriched" when YAML frontmatter was found for this entity,
	// "stale" when the frontmatter file is older than the topic's last_indexed
	// timestamp, or "pending" when no doc file exists.
	DocgenStatus string `json:"docgen_status"`
	// EnrichmentHealth reports which structured fields are populated.
	// Only present when DocgenStatus == "enriched" or "stale".
	EnrichmentHealth *topicEnrichmentHealth `json:"enrichment_health,omitempty"`
}

// topicEnrichmentHealth reports which message_topic enrichment fields are present,
// so the frontend can surface a completeness hint alongside the detail panel.
type topicEnrichmentHealth struct {
	HasSummary            bool `json:"has_summary"`
	HasSchema             bool `json:"has_schema"`
	HasVolumeEstimate     bool `json:"has_volume_estimate"`
	HasTypicalPayloadSize bool `json:"has_typical_payload_size"`
	HasExpectedConsumers  bool `json:"has_expected_consumers"`
	HasGaps               bool `json:"has_gaps"`
	FilledFieldCount      int  `json:"filled_field_count"`
	TotalFieldCount       int  `json:"total_field_count"`
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

	// #5485: for Inngest event topics, surface each producer/consumer function's
	// durable step structure (#5484 inngest_step children) inline so the panel
	// shows the run/sleep/waitForEvent/invoke steps of the event→function chain.
	if framework == "inngest" {
		attachInngestSteps(grp, producers)
		attachInngestSteps(grp, consumers)
	}

	// Populate FlowProcessIDs for each producer/consumer so the ↗ flow action
	// on the topology right panel can deep-link to the relevant flow (#1943).
	topicPrefixed := dashPrefixedID(topicRepoSlug, topicEnt.ID)
	for i := range producers {
		flowIDs := findFlowsForEntityAndTopic(grp,
			// local entity id extracted from prefixed form
			func() string { _, l := dashSplitPrefixed(producers[i].EntityID); return l }(),
			producers[i].EntityID,
			topicEnt.ID, topicPrefixed,
		)
		if len(flowIDs) > 0 {
			producers[i].FlowProcessIDs = flowIDs
		}
	}
	for i := range consumers {
		flowIDs := findFlowsForEntityAndTopic(grp,
			func() string { _, l := dashSplitPrefixed(consumers[i].EntityID); return l }(),
			consumers[i].EntityID,
			topicEnt.ID, topicPrefixed,
		)
		if len(flowIDs) > 0 {
			consumers[i].FlowProcessIDs = flowIDs
		}
	}

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

	// --- Docgen status + stale detection ---
	docgenStatus := "pending"
	var enrichHealth *topicEnrichmentHealth
	if enrichment != nil {
		// Determine stale/enriched by comparing the doc file's mtime against
		// the topic's last_indexed timestamp (r.Doc.GeneratedAt for its repo).
		docgenStatus = "enriched"
		if docPath, ok := entry["_doc_path"].(string); ok && docPath != "" {
			fi, statErr := os.Stat(docPath)
			if statErr == nil {
				var lastIndexed time.Time
				if r, rOK := grp.Repos[topicRepoSlug]; rOK && r.Doc != nil && !r.Doc.GeneratedAt.IsZero() {
					lastIndexed = r.Doc.GeneratedAt
				}
				if !lastIndexed.IsZero() && fi.ModTime().Before(lastIndexed) {
					docgenStatus = "stale"
				}
			}
		}
		enrichHealth = computeEnrichmentHealth(enrichment)
	}

	// If frontmatter carries a schema, prefer it over the entity-derived value
	// (human-edited frontmatter beats inferred graph property).
	if enrichment != nil && enrichment.Schema != "" {
		messageSchema = enrichment.Schema
	}

	// Merge frontmatter related_topics hints into the graph-derived list.
	// (These are display names / slugs from the AI doc; they complement the
	// entity-ID-based relatedTopics already computed above.)
	if enrichment != nil && len(enrichment.ExpectedConsumers) > 0 {
		existing := make(map[string]struct{}, len(relatedTopics))
		for _, rt := range relatedTopics {
			existing[rt] = struct{}{}
		}
		for _, hint := range enrichment.ExpectedConsumers {
			if _, seen := existing[hint]; !seen {
				relatedTopics = append(relatedTopics, hint)
				existing[hint] = struct{}{}
			}
		}
	}

	resp := topicDetailResponse{
		ID:               dashPrefixedID(topicRepoSlug, topicEnt.ID),
		Label:            topicEnt.Name,
		Protocol:         protocol,
		Broker:           broker,
		Framework:        framework,
		Schedule:         schedule,
		Scheduled:        scheduled,
		MessageSchema:    messageSchema,
		Repo:             topicRepoSlug,
		SourceFile:       topicEnt.SourceFile,
		StartLine:        topicEnt.StartLine,
		Producers:        producers,
		Consumers:        consumers,
		Tests:            tests,
		RelatedTopics:    relatedTopics,
		UsageHistory:     []any{},
		FlowCount:        flowCount,
		CrossRepo:        crossRepo,
		LifecycleState:   lifecycleState,
		DocsSummary:      docsSummary,
		Enrichment:       enrichment,
		DocgenStatus:     docgenStatus,
		EnrichmentHealth: enrichHealth,
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

// resolvePrefixedEntityRecords resolves a list of fully-prefixed entity IDs
// ("repo::localID") into rich topicEntityRecord values (name + source ref).
// Used by the topology LIST endpoint so publisher/subscriber rows can show the
// real entity NAME and source_file:line instead of a bare hashed ID (#1583).
//
// Each ID is split into its repo hint + local ID. When an ID carries no repo
// prefix we fall back to the supplied ownerRepo. Unresolvable IDs are emitted
// with a best-effort fallback name derived from the trailing path segment so
// the frontend never has to render a bare hash.
func resolvePrefixedEntityRecords(grp *DashGroup, ownerRepo string, prefixedIDs []string) []topicEntityRecord {
	out := make([]topicEntityRecord, 0, len(prefixedIDs))
	for _, pid := range prefixedIDs {
		repoHint, localID := dashSplitPrefixed(pid)
		searchRepo := repoHint
		if searchRepo == "" {
			searchRepo = ownerRepo
		}
		rec, found := lookupEntityRecord(grp, searchRepo, localID)
		if !found {
			// Last resort: scan all repos by local ID.
			rec, found = lookupEntityRecord(grp, "", localID)
		}
		if found {
			out = append(out, rec)
			continue
		}
		// Fallback: never surface a bare hash. Derive a readable-ish name from
		// the trailing segment; flag kind "unresolved" so the UI can label it.
		name := localID
		if i := strings.LastIndex(name, ":"); i >= 0 && i < len(name)-1 {
			name = name[i+1:]
		}
		out = append(out, topicEntityRecord{
			EntityID: pid,
			Name:     name,
			Kind:     "unresolved",
			Repo:     searchRepo,
		})
	}
	return out
}

// lookupEntityRecord finds an entity by local ID within a specific repo (or all
// repos when repoSlug == "") and returns its resolved record.
func lookupEntityRecord(grp *DashGroup, repoSlug, localID string) (topicEntityRecord, bool) {
	if repoSlug != "" {
		if r, ok := grp.Repos[repoSlug]; ok && r.Doc != nil {
			for i := range r.Doc.Entities {
				e := &r.Doc.Entities[i]
				if e.ID == localID {
					return entityToRecord(repoSlug, e), true
				}
			}
		}
		return topicEntityRecord{}, false
	}
	for _, r := range sortedRepos(grp) {
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if e.ID == localID {
				return entityToRecord(r.Slug, e), true
			}
		}
	}
	return topicEntityRecord{}, false
}

func entityToRecord(repo string, e *graph.Entity) topicEntityRecord {
	return topicEntityRecord{
		EntityID:       dashPrefixedID(repo, e.ID),
		Name:           e.Name,
		Kind:           dashStripScopePrefix(e.Kind),
		SourceFile:     e.SourceFile,
		StartLine:      e.StartLine,
		Repo:           repo,
		FlowProcessIDs: []string{},
		Framework:      e.Properties["framework"],
	}
}

// attachInngestSteps populates InngestSteps on every Inngest function in recs by
// scanning the group for the function's #5484 inngest_step Operation children,
// found via CONTAINS edges (Function → step). No-op for non-Inngest entities.
// This surfaces a durable function's step structure in the topology detail panel
// (#5485): clicking an Inngest event shows each producer/consumer function's
// run/sleep/waitForEvent/invoke steps inline.
func attachInngestSteps(grp *DashGroup, recs []topicEntityRecord) {
	for i := range recs {
		if recs[i].Framework != "inngest" {
			continue
		}
		_, localID := dashSplitPrefixed(recs[i].EntityID)
		steps := collectInngestSteps(grp, recs[i].Repo, localID)
		if len(steps) > 0 {
			recs[i].InngestSteps = steps
		}
	}
}

// collectInngestSteps returns the inngest_step Operation children of the Inngest
// function with local ID funcLocalID in repo funcRepo, resolved via CONTAINS
// edges. Steps are returned in source order (by start line) for stable display.
func collectInngestSteps(grp *DashGroup, funcRepo, funcLocalID string) []inngestStepRecord {
	r, ok := grp.Repos[funcRepo]
	if !ok || r.Doc == nil {
		return nil
	}
	// Build a lookup of step entities by local ID for this repo.
	stepByID := map[string]*graph.Entity{}
	for i := range r.Doc.Entities {
		e := &r.Doc.Entities[i]
		if dashStripScopePrefix(e.Kind) == "Operation" && e.Properties["framework"] == "inngest" {
			stepByID[e.ID] = e
		}
	}
	if len(stepByID) == 0 {
		return nil
	}
	var out []inngestStepRecord
	seen := map[string]bool{}
	for _, rel := range r.Doc.Relationships {
		if rel.Kind != "CONTAINS" || rel.FromID != funcLocalID {
			continue
		}
		e := stepByID[rel.ToID]
		if e == nil || seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		out = append(out, inngestStepRecord{
			StepID:       e.Properties["step_id"],
			StepKind:     e.Properties["step_kind"],
			SourceFile:   e.SourceFile,
			StartLine:    e.StartLine,
			WaitEvent:    e.Properties["wait_event"],
			InvokeTarget: e.Properties["invoke_target"],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartLine < out[j].StartLine })
	return out
}

// findFlowsForEntityAndTopic returns the prefixed Process entity IDs of every
// flow that has:
//   - this entity as a STEP_IN_PROCESS step
//   - the topic entity (by localTopicID or prefixedTopicID) also as a step
//
// Used to populate the #1943 ↗ flow action on publisher/subscriber rows.
func findFlowsForEntityAndTopic(
	grp *DashGroup,
	entityLocalID, entityPrefixed string,
	topicLocalID, topicPrefixed string,
) []string {
	// Build: processID → set of step entity ids (local + prefixed both recorded).
	type stepSet = map[string]struct{}
	processSteps := make(map[string]stepSet) // processLocalID → {stepIDs}
	processRepo := make(map[string]string)   // processLocalID → repoSlug

	for _, r := range sortedRepos(grp) {
		for _, rel := range r.Doc.Relationships {
			if rel.Kind != stepInProcessEdge {
				continue
			}
			pid := rel.FromID
			sid := rel.ToID
			if _, ok := processSteps[pid]; !ok {
				processSteps[pid] = make(stepSet)
				processRepo[pid] = r.Slug
			}
			processSteps[pid][sid] = struct{}{}
			// Also record prefixed versions so we match both forms.
			processSteps[pid][dashPrefixedID(r.Slug, sid)] = struct{}{}
		}
	}

	var out []string
	for pid, steps := range processSteps {
		hasEntity := false
		hasTopic := false
		for sid := range steps {
			if sid == entityLocalID || sid == entityPrefixed {
				hasEntity = true
			}
			if sid == topicLocalID || sid == topicPrefixed {
				hasTopic = true
			}
		}
		if hasEntity && hasTopic {
			slug := processRepo[pid]
			out = append(out, dashPrefixedID(slug, pid))
		}
	}
	return out
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

// computeEnrichmentHealth returns a populated topicEnrichmentHealth for a message_topic
// frontmatter, counting which of the six structured fields are filled.
func computeEnrichmentHealth(fm *EnrichmentFrontmatter) *topicEnrichmentHealth {
	if fm == nil {
		return nil
	}
	const total = 6
	h := &topicEnrichmentHealth{
		HasSummary:            fm.Summary != "",
		HasSchema:             fm.Schema != "",
		HasVolumeEstimate:     fm.VolumeEstimate != "",
		HasTypicalPayloadSize: fm.TypicalPayloadSizeBytes > 0,
		HasExpectedConsumers:  len(fm.ExpectedConsumers) > 0,
		HasGaps:               len(fm.Gaps) > 0,
		TotalFieldCount:       total,
	}
	count := 0
	if h.HasSummary {
		count++
	}
	if h.HasSchema {
		count++
	}
	if h.HasVolumeEstimate {
		count++
	}
	if h.HasTypicalPayloadSize {
		count++
	}
	if h.HasExpectedConsumers {
		count++
	}
	if h.HasGaps {
		count++
	}
	h.FilledFieldCount = count
	return h
}
