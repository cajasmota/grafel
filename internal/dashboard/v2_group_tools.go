// v2_group_tools.go — per-group AI-tool selection settings for WebUI v2 (#5257,
// EPIC #5252).
//
// Endpoints:
//
//	GET  /api/v2/groups/{group}/tools  → per-adapter {id, displayName, enabled, detected}
//	PUT  /api/v2/groups/{group}/tools  → set the desired enabled-set, apply the
//	                                     delta in-process, persist GroupConfig.Tools,
//	                                     return a per-tool {id, action, detail} summary
//
// The PUT path reuses install.ApplyToolDelta (#5256) IN-PROCESS: it writes the
// newly-enabled tools' rules files + MCP entries and removes the newly-disabled
// ones, reusing the same rulesfiles + mcpreg primitives `grafel install` uses.
// It NEVER shells out to `grafel install`, never restarts the daemon, and never
// touches OS services — the daemon stays up while a user edits their selection.
//
// Validation: unknown tool IDs → 400. Per-tool artifact failures are reported in
// the summary (action="error") and are NOT fatal to the whole request unless the
// delta apply itself fails (which is reported as 500 with no partial persist).

package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/tooladapter"
	"github.com/cajasmota/grafel/internal/registry"
)

// ---------------------------------------------------------------------------
// Wire shapes
// ---------------------------------------------------------------------------

// v2ToolStatus is one adapter's status in the GET response. It mirrors the
// ToolStatus interface in webui-v2/src/data/types.ts.
type v2ToolStatus struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Enabled     bool   `json:"enabled"`
	Detected    bool   `json:"detected"`
}

// v2ToolsStatusResponse is the GET payload: every registered adapter plus a flag
// recording whether the group has an explicit selection (vs the all-tools
// back-compat default).
type v2ToolsStatusResponse struct {
	Tools    []v2ToolStatus `json:"tools"`
	Explicit bool           `json:"explicit"`
}

// v2PutToolsReq is the PUT request body: the full desired enabled-set of tool
// IDs. An empty (but present) list disables every tool.
type v2PutToolsReq struct {
	Tools []string `json:"tools"`
}

// v2ToolApplyResult is one tool's outcome in the PUT response.
//
// action is one of:
//
//	written   — the tool was newly enabled; its artifacts were (re)written
//	removed   — the tool was newly disabled; its artifacts were removed
//	unchanged — the tool's enabled state did not change
//	error     — applying the tool's delta failed (detail carries the message)
type v2ToolApplyResult struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	Detail string `json:"detail,omitempty"`
}

// v2PutToolsResponse is the PUT payload: the persisted enabled-set plus a
// per-tool summary, in registry order.
type v2PutToolsResponse struct {
	Tools   []string            `json:"tools"`
	Summary []v2ToolApplyResult `json:"summary"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleV2GetTools — GET /api/v2/groups/{group}/tools
//
// Returns every registered adapter with its enabled state (from
// EnabledTools(cfg)) and a best-effort detected flag (from DetectInstalled()).
func (s *Server) handleV2GetTools(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	configPath, err := groupConfigPath(groupName)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	cfg, err := registry.LoadGroupConfig(configPath)
	if err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	enabledSet := map[string]bool{}
	for _, id := range tooladapter.EnabledTools(cfg) {
		enabledSet[id] = true
	}

	tools := make([]v2ToolStatus, 0, len(tooladapter.All()))
	for _, a := range tooladapter.All() {
		tools = append(tools, v2ToolStatus{
			ID:          a.ID(),
			DisplayName: a.DisplayName(),
			Enabled:     enabledSet[a.ID()],
			Detected:    a.DetectInstalled(),
		})
	}

	writeV2JSON(w, http.StatusOK, v2OK(v2ToolsStatusResponse{
		Tools:    tools,
		Explicit: len(cfg.Tools) > 0,
	}))
}

// handleV2PutTools — PUT /api/v2/groups/{group}/tools
//
// Sets the desired enabled-set, computes the delta vs the current effective set,
// applies it in-process via install.ApplyToolDelta (reused from #5256), persists
// GroupConfig.Tools, and returns a per-tool summary.
func (s *Server) handleV2PutTools(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	configPath, err := groupConfigPath(groupName)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	var req v2PutToolsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "invalid JSON: "+err.Error())
		return
	}

	// Validate every requested tool ID (unknown → 400). Normalize case/space.
	next := make([]string, 0, len(req.Tools))
	for _, raw := range req.Tools {
		id := strings.ToLower(strings.TrimSpace(raw))
		if id == "" {
			continue
		}
		if _, ok := tooladapter.Lookup(id); !ok {
			writeV2Err(w, http.StatusBadRequest, "bad_request",
				"unknown tool "+strconvQuote(raw)+"; valid tools: "+strings.Join(tooladapter.AllIDs(), ", "))
			return
		}
		next = append(next, id)
	}
	// Normalize to known IDs in registry order, de-duplicated. An empty list is
	// a valid request meaning "disable everything".
	next = tooladapter.NormalizeSelection(next)

	cfg, err := registry.LoadGroupConfig(configPath)
	if err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	prev := tooladapter.EnabledTools(cfg)

	// Persist the explicit selection first so a partial artifact failure still
	// leaves the recorded config consistent with what the user asked for.
	cfg.Tools = next
	if err := registry.SaveGroupConfig(configPath, cfg); err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", "save group config: "+err.Error())
		return
	}

	bin, _ := os.Executable()
	apply := s.applyToolDelta
	if apply == nil {
		apply = install.ApplyToolDelta
	}
	res, err := apply(cfg, groupName, bin, prev, next, nil)
	if err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", "apply tool delta: "+err.Error())
		return
	}

	writeV2JSON(w, http.StatusOK, v2OK(v2PutToolsResponse{
		Tools:   next,
		Summary: summarizeToolDelta(prev, next, res),
	}))
}

// summarizeToolDelta builds the per-tool {id, action, detail} summary in
// registry order from the delta result. A tool is "written" when newly enabled,
// "removed" when newly disabled, and "unchanged" otherwise. (The current
// in-process ApplyToolDelta returns an error for the whole request rather than
// per-tool failures, so "error" is reserved for future per-tool reporting; the
// summary surface is in place so the frontend contract is stable.)
func summarizeToolDelta(prev, next []string, res *install.ToolDeltaResult) []v2ToolApplyResult {
	enabled := map[string]bool{}
	for _, id := range res.Enabled {
		enabled[id] = true
	}
	disabled := map[string]bool{}
	for _, id := range res.Disabled {
		disabled[id] = true
	}

	out := make([]v2ToolApplyResult, 0, len(tooladapter.All()))
	for _, a := range tooladapter.All() {
		id := a.ID()
		switch {
		case enabled[id]:
			out = append(out, v2ToolApplyResult{ID: id, Action: "written", Detail: "rules + MCP artifacts written"})
		case disabled[id]:
			out = append(out, v2ToolApplyResult{ID: id, Action: "removed", Detail: "artifacts removed"})
		default:
			out = append(out, v2ToolApplyResult{ID: id, Action: "unchanged"})
		}
	}
	return out
}

// strconvQuote is a tiny local helper so we don't pull in strconv just for one
// quoted-string error message.
func strconvQuote(s string) string {
	return "\"" + s + "\""
}
