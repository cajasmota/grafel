package dashboard

// handlers_flows_quality.go — Flows v2 quality classification endpoints
//
//	GET /api/flows/{group}/dead-ends
//	GET /api/flows/{group}/truncated

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
	Reason     string `json:"reason"` // "no_useful_sink" | "single_step"
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

// ─────────────────────────────────────────────────────────────────────────────
// Truncated flow detector
// ─────────────────────────────────────────────────────────────────────────────

// truncationSeverity maps a truncation reason to an informational severity
// level surfaced in the API response.
//
//	external_dependency  → info  (expected; known SDK / stdlib boundary)
//	unresolved_reference → warn  (likely an indexing gap or dynamic dispatch)
//	unindexed_repo       → error (cross-service edge to a repo never indexed)
var truncationSeverity = map[string]string{
	"unresolved_callee":    "warn",
	"cross_repo_unindexed": "error",
	"dynamic_dispatch":     "info",
}

// TruncatedFlowItem represents a single truncated process flow in the API
// response.
type TruncatedFlowItem struct {
	ProcessID        string `json:"process_id"`
	Label            string `json:"label"`
	EntryName        string `json:"entry_name"`
	StepCount        int    `json:"step_count"`
	Repo             string `json:"repo"`
	Reason           string `json:"reason"`            // "unresolved_callee" | "cross_repo_unindexed" | "dynamic_dispatch"
	Severity         string `json:"severity"`          // "info" | "warn" | "error"
	TruncationStep   string `json:"truncation_step"`   // entity ID of the offending step
	TruncationIndex  int    `json:"truncation_point"`  // 0-based index into the step chain
	UnresolvedTarget string `json:"unresolved_target"` // name/ID of the entity that could not be resolved
	IsTruncated      bool   `json:"is_truncated"`
	CrossStack       bool   `json:"cross_stack"`
}

// truncationCheck holds the result of inspecting a single step for truncation.
type truncationCheck struct {
	reason           string
	truncationStep   string
	truncationIndex  int
	unresolvedTarget string
}

// classifyFlowTruncated inspects all Process entities in a group and returns
// those whose step chain hits an unresolved or external edge mid-stream.
//
// Detection rules (in priority order):
//  1. A step has property dynamic="true"  → dynamic_dispatch
//  2. A step has an outgoing CALLS edge to a ToID that is NOT present in any
//     repo in the group                   → unresolved_callee
//  3. A step has cross_stack="true" AND the terminal entity is absent from all
//     group repos                         → cross_repo_unindexed
func classifyFlowTruncated(grp *DashGroup) []TruncatedFlowItem {
	// Build a set of all known entity IDs across every repo in the group.
	// We key by bare local ID (entities reference each other by local ID within
	// the same repo, and by cross-repo prefixed ID across repos).
	knownLocalIDs := map[flowStepKey]bool{}
	knownBareIDs := map[string]bool{} // bare ID, repo-agnostic lookup
	entityProps := map[flowStepKey]map[string]string{}

	for _, r := range sortedRepos(grp) {
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			k := flowStepKey{r.Slug, e.ID}
			knownLocalIDs[k] = true
			knownBareIDs[e.ID] = true
			if e.Properties != nil {
				entityProps[k] = e.Properties
			}
		}
	}

	// Build per-step outgoing CALLS edges: map (repo, fromID) → []toID
	callEdges := map[flowStepKey][]string{}
	for _, r := range sortedRepos(grp) {
		for _, rel := range r.Doc.Relationships {
			if rel.Kind != "CALLS" {
				continue
			}
			k := flowStepKey{r.Slug, rel.FromID}
			callEdges[k] = append(callEdges[k], rel.ToID)
		}
	}

	var results []TruncatedFlowItem

	for _, r := range sortedRepos(grp) {
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if e.Kind != processEntityKind {
				continue
			}

			sc, _ := strconv.Atoi(e.Properties["step_count"])
			cs := e.Properties["cross_stack"] == "true"
			pid := dashPrefixedID(r.Slug, e.ID)

			// Collect ordered step keys.
			stepKeys := collectStepKeysOrdered(grp, e.ID, pid)
			if len(stepKeys) == 0 {
				continue
			}

			check := findTruncationPoint(stepKeys, entityProps, callEdges, knownLocalIDs, knownBareIDs)
			if check == nil {
				// Fully resolved — not truncated.
				continue
			}

			severity := truncationSeverity[check.reason]
			if severity == "" {
				severity = "warn"
			}

			results = append(results, TruncatedFlowItem{
				ProcessID:        pid,
				Label:            e.Name,
				EntryName:        e.Properties["entry_name"],
				StepCount:        sc,
				Repo:             r.Slug,
				Reason:           check.reason,
				Severity:         severity,
				TruncationStep:   check.truncationStep,
				TruncationIndex:  check.truncationIndex,
				UnresolvedTarget: check.unresolvedTarget,
				IsTruncated:      true,
				CrossStack:       cs,
			})
		}
	}

	return results
}

// orderedStep pairs a step key with its declared step_index.
type orderedStep struct {
	key   flowStepKey
	index int
}

// collectStepKeysOrdered gathers flowStepKey pairs for all steps in a process
// by following STEP_IN_PROCESS edges, preserving step_index order where
// available. Falls back to insertion order for steps without an index.
func collectStepKeysOrdered(
	grp *DashGroup,
	processLocalID, processPrefixedID string,
) []flowStepKey {
	var indexed []orderedStep

	for _, r := range sortedRepos(grp) {
		for _, rel := range r.Doc.Relationships {
			if rel.Kind != stepInProcessEdge {
				continue
			}
			if rel.FromID != processLocalID &&
				dashPrefixedID(r.Slug, rel.FromID) != processPrefixedID {
				continue
			}
			idx := -1
			if rel.Properties != nil {
				if raw, ok := rel.Properties["step_index"]; ok {
					idx, _ = strconv.Atoi(raw)
				}
			}
			indexed = append(indexed, orderedStep{
				key:   flowStepKey{r.Slug, rel.ToID},
				index: idx,
			})
		}
	}

	// Stable insertion sort by step_index; steps without an index stay at end.
	for i := 1; i < len(indexed); i++ {
		j := i
		for j > 0 && indexed[j].index >= 0 && indexed[j-1].index > indexed[j].index {
			indexed[j-1], indexed[j] = indexed[j], indexed[j-1]
			j--
		}
	}

	keys := make([]flowStepKey, len(indexed))
	for i, s := range indexed {
		keys[i] = s.key
	}
	return keys
}

// findTruncationPoint walks the step list and returns the first step that
// represents a truncation point, or nil if the flow is fully resolved.
func findTruncationPoint(
	steps []flowStepKey,
	entityProps map[flowStepKey]map[string]string,
	callEdges map[flowStepKey][]string,
	knownLocalIDs map[flowStepKey]bool,
	knownBareIDs map[string]bool,
) *truncationCheck {
	for idx, step := range steps {
		props := entityProps[step]

		// Rule 1: dynamic dispatch flag on the step entity.
		if props != nil && props["dynamic"] == "true" {
			target := props["dynamic_target"]
			if target == "" {
				target = step.id
			}
			return &truncationCheck{
				reason:           "dynamic_dispatch",
				truncationStep:   step.id,
				truncationIndex:  idx,
				unresolvedTarget: target,
			}
		}

		// Rule 2: CALLS edge to an entity not present in any repo.
		for _, toID := range callEdges[step] {
			// Check if the toID is known as a bare local ID in any repo.
			if !knownBareIDs[toID] && !strings.HasPrefix(toID, "external:") {
				return &truncationCheck{
					reason:           "unresolved_callee",
					truncationStep:   step.id,
					truncationIndex:  idx,
					unresolvedTarget: toID,
				}
			}
		}

		// Rule 3: cross_stack step whose terminal is absent from all repos.
		if props != nil && props["cross_stack"] == "true" {
			terminalID := props["terminal_id"]
			if terminalID != "" && !knownBareIDs[terminalID] {
				return &truncationCheck{
					reason:           "cross_repo_unindexed",
					truncationStep:   step.id,
					truncationIndex:  idx,
					unresolvedTarget: terminalID,
				}
			}
		}
	}
	return nil
}

// handleFlowTruncated — GET /api/flows/{group}/truncated
//
// Returns all Process flows in the group that hit an external or unresolved
// edge mid-stream: the flow is cut off before reaching its natural sink.
// Each item identifies the step at which the chain breaks (truncation_step,
// truncation_point) and the entity name/ID that could not be resolved
// (unresolved_target), with a severity tag to guide triage priority.
func (s *Server) handleFlowTruncated(w http.ResponseWriter, r *http.Request) {
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

	truncated := classifyFlowTruncated(grp)
	if truncated == nil {
		truncated = []TruncatedFlowItem{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"truncated_flows": truncated,
		"total":           len(truncated),
	})
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
