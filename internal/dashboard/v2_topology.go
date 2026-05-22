// v2_topology.go — WebUI v2 topology surface.
//
// Endpoints:
//
//	GET /api/v2/topology/{group}              → full topology payload (v2 envelope)
//	GET /api/v2/topology/{group}/topic/{id}   → channel detail   (v2 envelope)
//
// These are thin wrappers around the existing v1 collectTopologyResponse /
// buildTopicDetail logic. NO logic is duplicated — we call the same
// functions and wrap the result in the v2 { ok, data } envelope so the
// WebUI v2 api client can use requestV2().
//
// Implements #1440 (EPIC #1432).

package dashboard

import (
	"net/http"

	"github.com/cajasmota/archigraph/internal/mcp"
)

// handleV2Topology — GET /api/v2/topology/{group}
//
// Wraps collectTopologyResponse in the v2 envelope.
// Returns the full topology payload: topics, queues, channels, functions,
// broker_groups. All data is static graph extraction — no runtime metrics.
func (s *Server) handleV2Topology(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	docgenState, _ := mcp.LoadDocgenState(group)
	payload := collectTopologyResponse(grp, group, docgenState)
	writeV2JSON(w, http.StatusOK, v2OK(payload))
}

// handleV2TopologyDetail — GET /api/v2/topology/{group}/topic/{topicId}
//
// Delegates to buildTopicDetail (handlers_topology_detail.go) and wraps the
// result in the v2 envelope. No logic is duplicated.
func (s *Server) handleV2TopologyDetail(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	topicID := r.PathValue("topicId")

	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	if topicID == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "topicId required")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	detail, found := buildTopicDetail(grp, group, topicID)
	if !found {
		writeV2Err(w, http.StatusNotFound, "not_found", "topic not found: "+topicID)
		return
	}

	writeV2JSON(w, http.StatusOK, v2OK(detail))
}
