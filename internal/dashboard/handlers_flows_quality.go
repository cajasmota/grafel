package dashboard

// handlers_flows_quality.go — Flows v2 quality classification endpoints
//
//	GET /api/flows/{group}/dead-ends

import (
	"net/http"
	"strconv"
	"strings"
)

// usefulSinkEdgeKinds is the set of outgoing relationship kinds that indicate
// a step produces an observable side effect (DB write, HTTP response, message
// publish, test assertion, render, or state mutation).
var usefulSinkEdgeKinds = map[string]bool{
	// DB writes
	"WRITES_TO":    true,
	"INSERTS_INTO": true,
	"DELETES_FROM": true,
	"UPDATES":      true,
	// Message publishing
	"PUBLISHES_TO": true,
	"PUBLISHES":    true,
	"WS_EMITS":     true,
	"STREAMS_TO":   true,
	// Test assertions
	"ASSERTS": true,
	// State mutation
	"MUTATES_STATE": true,
}

// usefulSinkKindSubstrings lists entity kind substrings that by themselves
// indicate a useful terminal (e.g. an HTTP response handler or test entity).
var usefulSinkKindSubstrings = []string{
	"Response",
	"Handler",
	"Test",
	"Assert",
	"Render",
}

// flowStepKey identifies a step entity by repo slug and local entity ID.
type flowStepKey struct{ repo, id string }

// DeadEndItem represents a single dead-end process flow in the API response.
type DeadEndItem struct {
	ProcessID  string `json:"process_id"`
	Label      string `json:"label"`
	EntryName  string `json:"entry_name"`
	StepCount  int    `json:"step_count"`
	Repo       string `json:"repo"`
	Reason     string `json:"reason"`     // "no_useful_sink" | "single_step"
	CrossStack bool   `json:"cross_stack"`
}

// classifyFlowDeadEnds inspects all Process entities in a group and returns
// those that are considered dead-ends: flows whose step chain contains no
// useful side-effect and flows with 0 or 1 steps.
func classifyFlowDeadEnds(grp *DashGroup) []DeadEndItem {
	// Build an index of entity kind by (repo slug, entity ID) so we can
	// check step entity kinds without re-scanning every time.
	entityKind := map[flowStepKey]string{}
	for _, r := range sortedRepos(grp) {
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			entityKind[flowStepKey{r.Slug, e.ID}] = e.Kind
		}
	}

	// Build a set of relationship kinds outgoing from each entity.
	outEdgeKinds := map[flowStepKey]map[string]bool{}
	for _, r := range sortedRepos(grp) {
		for _, rel := range r.Doc.Relationships {
			k := flowStepKey{r.Slug, rel.FromID}
			if outEdgeKinds[k] == nil {
				outEdgeKinds[k] = map[string]bool{}
			}
			outEdgeKinds[k][rel.Kind] = true
		}
	}

	var results []DeadEndItem

	for _, r := range sortedRepos(grp) {
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if e.Kind != processEntityKind {
				continue
			}

			sc, _ := strconv.Atoi(e.Properties["step_count"])
			cs := e.Properties["cross_stack"] == "true"
			pid := dashPrefixedID(r.Slug, e.ID)

			// Single-step (or zero-step) flows are always classified separately.
			if sc <= 1 {
				results = append(results, DeadEndItem{
					ProcessID:  pid,
					Label:      e.Name,
					EntryName:  e.Properties["entry_name"],
					StepCount:  sc,
					Repo:       r.Slug,
					Reason:     "single_step",
					CrossStack: cs,
				})
				continue
			}

			// Collect step entity keys via STEP_IN_PROCESS edges.
			stepKeys := collectStepKeys(grp, e.ID, pid)

			// Check whether any step has a useful sink.
			if hasUsefulSink(stepKeys, entityKind, outEdgeKinds) {
				continue
			}

			results = append(results, DeadEndItem{
				ProcessID:  pid,
				Label:      e.Name,
				EntryName:  e.Properties["entry_name"],
				StepCount:  sc,
				Repo:       r.Slug,
				Reason:     "no_useful_sink",
				CrossStack: cs,
			})
		}
	}

	return results
}

// collectStepKeys gathers flowStepKey pairs for all steps in a process by
// following STEP_IN_PROCESS edges whose FromID matches the process.
func collectStepKeys(
	grp *DashGroup,
	processLocalID, processPrefixedID string,
) []flowStepKey {
	var steps []flowStepKey
	for _, r := range sortedRepos(grp) {
		for _, rel := range r.Doc.Relationships {
			if rel.Kind != stepInProcessEdge {
				continue
			}
			if rel.FromID != processLocalID &&
				dashPrefixedID(r.Slug, rel.FromID) != processPrefixedID {
				continue
			}
			steps = append(steps, flowStepKey{r.Slug, rel.ToID})
		}
	}
	return steps
}

// hasUsefulSink returns true if any of the given step entities has an outgoing
// useful-sink edge or an entity kind that indicates a useful terminal.
func hasUsefulSink(
	steps []flowStepKey,
	entityKind map[flowStepKey]string,
	outEdgeKinds map[flowStepKey]map[string]bool,
) bool {
	for _, step := range steps {
		// Check entity kind substrings.
		kind := entityKind[step]
		for _, sub := range usefulSinkKindSubstrings {
			if strings.Contains(kind, sub) {
				return true
			}
		}
		// Check outgoing edge kinds.
		for edgeKind := range outEdgeKinds[step] {
			if usefulSinkEdgeKinds[edgeKind] {
				return true
			}
		}
	}
	return false
}

// handleFlowDeadEnds — GET /api/flows/{group}/dead-ends
//
// Returns all Process flows in the group that terminate in a useless sink:
// no DB write, no HTTP response, no message publish, and no test assertion.
// Single-step flows are included with reason "single_step".
func (s *Server) handleFlowDeadEnds(w http.ResponseWriter, r *http.Request) {
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

	deadEnds := classifyFlowDeadEnds(grp)
	if deadEnds == nil {
		deadEnds = []DeadEndItem{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"dead_ends": deadEnds,
		"total":     len(deadEnds),
	})
}
