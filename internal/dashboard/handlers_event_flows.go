// Dashboard handlers for Event Flows (#1944 Phase 1).
//
// EventFlow entities are emitted by engine.RunEventFlow (Pass 7.5) and
// follow the same Property contract as Process entities so the existing
// flows-DAG renderer (#2028 / flows.tsx) can drive both views with a
// single component. The handlers below mirror the shape of /api/flows
// but read SCOPE.EventFlow entities.
//
// Routes:
//
//	GET /api/event-flows/{group}                — list event-flows
//	GET /api/event-flows/{group}/{eventFlowId}  — full step chain for one flow
//
// Phase-1 scope is intentionally narrow: linear chains, single-channel
// seed, no enrichment frontmatter integration, no cross-stack walker.
// Future phases will layer those on without changing the response
// envelope.
package dashboard

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/cajasmota/grafel/internal/graph"
)

// eventFlowEntityKind is the engine-emitted kind for event-flow chains.
// Duplicated here as a string to avoid an internal/engine import cycle
// from the dashboard package.
const eventFlowEntityKind = "SCOPE.EventFlow"

// stepInEventFlowEdge is the engine-emitted edge kind that links an
// EventFlow entity → each step entity (channel or operation) in chain
// order. Mirrors stepInProcessEdge.
const stepInEventFlowEdge = "STEP_IN_EVENT_FLOW"

// EventFlowListItem is the wire shape of one row in the Event Flows
// list view. Mirrors v2FlowsProcessItem so the React Flows DAG renderer
// can be reused without a second component.
type EventFlowListItem struct {
	EventFlowID  string   `json:"event_flow_id"`
	Repo         string   `json:"repo"`
	Label        string   `json:"label"`
	SeedID       string   `json:"seed_id"`
	SeedName     string   `json:"seed_name"`
	TerminalID   string   `json:"terminal_id"`
	StepCount    int      `json:"step_count"`
	ChannelCount int      `json:"channel_count"`
	ChainLabels  []string `json:"chain_labels"`
	SourceFile   string   `json:"source_file,omitempty"`
	// EntryKind is always "channel" for EventFlows; included for parity
	// with the ProcessFlow list shape so the frontend can route through
	// a single component.
	EntryKind string `json:"entry_kind"`
}

// EventFlowStep is one node in the rendered chain. Mirrors the shape of
// the Process-Flow step in handleFlowDetail (subset; Phase 1 omits the
// per-step source-snippet enrichment to keep the handler footprint
// small — JARVIS comet + scrubber only need entity_id, label, repo,
// step_index, and an entity-kind hint).
type EventFlowStep struct {
	EntityID   string `json:"entity_id"`
	Label      string `json:"label"`
	Repo       string `json:"repo"`
	StepIndex  int    `json:"step_index"`
	EntityKind string `json:"entity_kind"`
	SourceFile string `json:"source_file,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	IsChannel  bool   `json:"is_channel"`
}

// handleEventFlowsList — GET /api/event-flows/{group}
func (s *Server) handleEventFlowsList(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}
	q := r.URL.Query()
	limit := 200
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	minSteps := 0
	if v := q.Get("min_steps"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			minSteps = n
		}
	}
	seedFilter := q.Get("seed")

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	var items []EventFlowListItem
	for _, repo := range sortedRepos(grp) {
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			if e.Kind != eventFlowEntityKind {
				continue
			}
			sc, _ := strconv.Atoi(e.PropGet("step_count"))
			if sc < minSteps {
				continue
			}
			seedID := e.PropGet("entry_id")
			if seedFilter != "" && seedID != seedFilter &&
				dashPrefixedID(repo.Slug, seedID) != seedFilter {
				continue
			}
			cc, _ := strconv.Atoi(e.PropGet("channel_count"))
			items = append(items, EventFlowListItem{
				EventFlowID:  dashPrefixedID(repo.Slug, e.ID),
				Repo:         repo.Slug,
				Label:        e.Name,
				SeedID:       seedID,
				SeedName:     e.PropGet("entry_name"),
				TerminalID:   e.PropGet("terminal_id"),
				StepCount:    sc,
				ChannelCount: cc,
				ChainLabels:  splitChainLabels(e.PropGet("chain_labels")),
				SourceFile:   e.SourceFile,
				EntryKind:    "channel",
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].ChannelCount != items[j].ChannelCount {
			return items[i].ChannelCount > items[j].ChannelCount
		}
		if items[i].StepCount != items[j].StepCount {
			return items[i].StepCount > items[j].StepCount
		}
		return items[i].Label < items[j].Label
	})
	if len(items) > limit {
		items = items[:limit]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"event_flows": items,
		"count":       len(items),
	})
}

// handleEventFlowDetail — GET /api/event-flows/{group}/{eventFlowId}
//
// Returns the chain steps + the raw DAG JSON (`branches_dag`) so the
// Flows renderer can draw the scrubber + JARVIS comet animation.
func (s *Server) handleEventFlowDetail(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	flowID := r.PathValue("eventFlowId")
	if group == "" || flowID == "" {
		writeErr(w, http.StatusBadRequest, "group and eventFlowId required")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	repoHint, localID := dashSplitPrefixed(flowID)
	var flowRepoSlug string
	var flowEnt *graph.Entity

	for _, repo := range sortedRepos(grp) {
		if repoHint != "" && repo.Slug != repoHint {
			continue
		}
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			if e.Kind != eventFlowEntityKind {
				continue
			}
			if e.ID == localID || dashPrefixedID(repo.Slug, e.ID) == flowID {
				flowRepoSlug = repo.Slug
				flowEnt = e
				break
			}
		}
		if flowEnt != nil {
			break
		}
	}

	if flowEnt == nil {
		writeErr(w, http.StatusNotFound, "event flow not found: "+flowID)
		return
	}

	// Group-wide entity index so bridge steps that live in a companion
	// repo's Entities slice still resolve (Phase 3 cross-stack walker
	// will populate that scenario; the index is harmless when empty).
	groupEntityIndex := buildGroupEntityIndex(grp)

	type rawEFStep struct {
		EntityID   string
		Repo       string
		Label      string
		EntityKind string
		SourceFile string
		StartLine  int
		StepIndex  int
	}
	var rawSteps []rawEFStep
	for _, repo := range sortedRepos(grp) {
		for _, rel := range repo.Doc.Relationships {
			if rel.Kind != stepInEventFlowEdge {
				continue
			}
			if rel.FromID != flowEnt.ID && dashPrefixedID(repo.Slug, rel.FromID) != flowID {
				continue
			}
			idx, _ := strconv.Atoi(rel.PropGet("step_index"))
			stepIDLocal := rel.ToID
			if hit, ok := groupEntityIndex[stepIDLocal]; ok {
				rawSteps = append(rawSteps, rawEFStep{
					EntityID:   dashPrefixedID(hit.repo, hit.entity.ID),
					Repo:       hit.repo,
					Label:      hit.entity.Name,
					EntityKind: hit.entity.Kind,
					SourceFile: hit.entity.SourceFile,
					StartLine:  hit.entity.StartLine,
					StepIndex:  idx,
				})
			}
		}
	}
	sort.Slice(rawSteps, func(i, j int) bool { return rawSteps[i].StepIndex < rawSteps[j].StepIndex })

	steps := make([]EventFlowStep, 0, len(rawSteps))
	for _, rs := range rawSteps {
		steps = append(steps, EventFlowStep{
			EntityID:   rs.EntityID,
			Label:      rs.Label,
			Repo:       rs.Repo,
			StepIndex:  rs.StepIndex,
			EntityKind: rs.EntityKind,
			SourceFile: rs.SourceFile,
			StartLine:  rs.StartLine,
			IsChannel:  isChannelKind(rs.EntityKind),
		})
	}

	sc, _ := strconv.Atoi(flowEnt.PropGet("step_count"))
	cc, _ := strconv.Atoi(flowEnt.PropGet("channel_count"))

	writeJSON(w, http.StatusOK, map[string]any{
		"event_flow_id": dashPrefixedID(flowRepoSlug, flowEnt.ID),
		"repo":          flowRepoSlug,
		"label":         flowEnt.Name,
		"seed_id":       flowEnt.PropGet("entry_id"),
		"seed_name":     flowEnt.PropGet("entry_name"),
		"terminal_id":   flowEnt.PropGet("terminal_id"),
		"step_count":    sc,
		"channel_count": cc,
		"chain_labels":  splitChainLabels(flowEnt.PropGet("chain_labels")),
		"branches_dag":  flowEnt.PropGet("branches_dag"),
		"steps":         steps,
		"entry_kind":    "channel",
	})
}

// isChannelKind reports whether an entity kind names a pub/sub channel
// (case-insensitive). Mirrors engine.isChannelEntity without depending
// on the engine package. Used by the detail handler to flag steps that
// the UI should render as channel nodes instead of operation nodes.
func isChannelKind(kind string) bool {
	switch kind {
	case "SCOPE.MessageTopic", "SCOPE.EventBusEvent":
		return true
	}
	// Case-insensitive fallback in case an older index stamped the kind
	// in mixed case.
	switch {
	case lcEq(kind, "scope.messagetopic"),
		lcEq(kind, "scope.eventbusevent"):
		return true
	}
	return false
}

// lcEq returns true when `s` lowercased equals `target` (which must
// already be lowercase). Avoids an extra strings import in callers that
// already use case-folded comparisons sparingly.
func lcEq(s, target string) bool {
	if len(s) != len(target) {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != target[i] {
			return false
		}
	}
	return true
}
