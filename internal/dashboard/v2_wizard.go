// v2_wizard.go — shared create-group / add-repo scan→detect→index wizard (#1517).
//
// Today create-group (#1451) makes an EMPTY group and Settings add-repo (#1455)
// only registers a repo without indexing. This file adds the thin v2 endpoints
// the shared WebUI v2 wizard drives:
//
//	POST /api/v2/scan/inspect            → resolve + validate a path, detect
//	                                       stack + monorepo layout (no writes)
//	POST /api/v2/groups/from-scan        → create group + register repos +
//	                                       enqueue an async index job (JobAck)
//	POST /api/v2/groups/{group}/repos/scan → register repos into an existing
//	                                       group + enqueue an async index job
//
// Directory input — server-side path, NOT a browser File handle:
//
//	The daemon indexes paths on its OWN filesystem (the Rebuild RPC, the
//	registry, and detect.* all take an absolute path the daemon resolves).
//	The browser File System Access API (showDirectoryPicker) only yields an
//	opaque FileSystemDirectoryHandle with no real on-disk path, so it cannot
//	tell the daemon WHICH directory to index. The wizard therefore sends a
//	PATH STRING that the daemon resolves with the SAME expandPath() the v1
//	onboard endpoints use. showDirectoryPicker is used in the UI only as an
//	optional convenience to prefill the folder *name* hint; the path field is
//	the source of truth. This mirrors how #1512's rebuild endpoint targets a
//	group whose repos are already absolute server paths.
//
// Reuse: scan/inspect wraps the SAME detect.Stack + detect.DetectMonorepo the
// v1 /api/onboard endpoints use; the index step reuses the #1512 actionJob
// infra (s.actionJobs + runRebuildJob) so progress is pollable/streamable via
// /api/v2/jobs/{id} + /api/v2/jobs/{id}/stream.

package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/install/detect"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// v2ScanInspectReq is the request body for POST /api/v2/scan/inspect.
type v2ScanInspectReq struct {
	Path string `json:"path"`
}

// v2ScanInspectReply is the detection preview returned by the scan step.
type v2ScanInspectReply struct {
	// Valid reports whether the path resolved to an existing directory.
	Valid bool `json:"valid"`
	// AbsPath is the resolved absolute path (~ + relative refs expanded).
	AbsPath string `json:"absPath"`
	// SuggestedGroup is derived from the directory basename (slugified).
	SuggestedGroup string `json:"suggestedGroup"`
	// SuggestedSlug is the URL-safe repo slug (basename, lower-cased).
	SuggestedSlug string `json:"suggestedSlug"`
	// Stack is the detected dominant stack ("go", "node", "python", …).
	Stack string `json:"stack"`
	// Monorepo is the detected layout kind ("pnpm","npm","turbo","nx","lerna","multi") or "".
	Monorepo string `json:"monorepo"`
	// Packages is the list of repo-relative package roots for a monorepo.
	Packages []string `json:"packages"`
	// HasAgentsMD is true when AGENTS.md / CLAUDE.md / GEMINI.md exists at the root.
	HasAgentsMD bool `json:"hasAgentsMd"`
	// AlreadyRegistered is the existing group name if .archigraph/group.json exists.
	AlreadyRegistered string `json:"alreadyRegistered,omitempty"`
	// Error is a human-readable reason when Valid is false.
	Error string `json:"error,omitempty"`
}

// v2WizardRepo is one repo the wizard wants registered + indexed.
type v2WizardRepo struct {
	Path    string   `json:"path"`
	Slug    string   `json:"slug,omitempty"`
	Modules []string `json:"modules,omitempty"`
}

// v2FromScanReq is the body for POST /api/v2/groups/from-scan.
type v2FromScanReq struct {
	Name  string         `json:"name"`
	Repos []v2WizardRepo `json:"repos"`
}

// v2ScanReposReq is the body for POST /api/v2/groups/{group}/repos/scan.
type v2ScanReposReq struct {
	Repos []v2WizardRepo `json:"repos"`
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/v2/scan/inspect — resolve + detect (no writes)
// ─────────────────────────────────────────────────────────────────────────────

// handleV2ScanInspect resolves a server-side path and returns a stack +
// monorepo detection preview. It performs NO registry writes; it is the
// "detect" step of the wizard. Wraps the same detect.* helpers the v1 onboard
// endpoints use.
func (s *Server) handleV2ScanInspect(w http.ResponseWriter, r *http.Request) {
	var req v2ScanInspectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Path == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "path required")
		return
	}

	abs, err := expandPath(req.Path)
	if err != nil {
		writeV2JSON(w, http.StatusOK, v2OK(v2ScanInspectReply{
			Valid: false,
			Error: fmt.Sprintf("cannot resolve path: %v", err),
		}))
		return
	}

	info, statErr := os.Stat(abs)
	if statErr != nil || !info.IsDir() {
		msg := "path does not exist or is not a directory"
		if statErr != nil {
			msg = statErr.Error()
		}
		writeV2JSON(w, http.StatusOK, v2OK(v2ScanInspectReply{
			Valid:   false,
			AbsPath: abs,
			Error:   msg,
		}))
		return
	}

	base := filepath.Base(abs)
	mono, _ := detect.DetectMonorepo(abs)
	pkgs := mono.Packages
	if pkgs == nil {
		pkgs = []string{}
	}

	reply := v2ScanInspectReply{
		Valid:          true,
		AbsPath:        abs,
		SuggestedGroup: slugify(base),
		SuggestedSlug:  slugify(base),
		Stack:          detect.Stack(abs),
		Monorepo:       string(mono.Kind),
		Packages:       pkgs,
		HasAgentsMD:    fileExists(abs, "AGENTS.md") || fileExists(abs, "CLAUDE.md") || fileExists(abs, "GEMINI.md"),
	}
	if fileExists(abs, ".archigraph/group.json") {
		reply.AlreadyRegistered = readManifestGroup(filepath.Join(abs, ".archigraph", "group.json"))
	}
	writeV2JSON(w, http.StatusOK, v2OK(reply))
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/v2/groups/from-scan — create group + register repos + index
// ─────────────────────────────────────────────────────────────────────────────

// handleV2CreateGroupFromScan creates a NEW group from a scanned path, registers
// the chosen repos, and enqueues an async index job. It returns a 202 JobAck
// (identical shape to the rebuild endpoints) so the wizard can stream progress
// via /api/v2/jobs/{id}/stream. The group + repos are created synchronously
// before the ack so a failure surfaces immediately; only the indexing is async.
func (s *Server) handleV2CreateGroupFromScan(w http.ResponseWriter, r *http.Request) {
	var req v2FromScanReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "name required")
		return
	}
	if len(req.Repos) == 0 {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "at least one repo required")
		return
	}

	// 1. Create the group registry entry (same path the v1 onboard handler uses).
	if _, err := s.registry.CreateGroup(req.Name); err != nil {
		writeV2Err(w, http.StatusConflict, "conflict", "create group: "+err.Error())
		return
	}

	// 2. Register each repo, resolving the server-side path + detecting stack.
	if status, code, msg := s.registerWizardRepos(req.Name, req.Repos); msg != "" {
		writeV2Err(w, status, code, msg)
		return
	}

	// 3. Enqueue the async index job (reuses the #1512 actionJob infra).
	s.enqueueWizardIndex(w, req.Name)
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/v2/groups/{group}/repos/scan — add repos to existing group + index
// ─────────────────────────────────────────────────────────────────────────────

// handleV2ScanRepos registers one or more scanned repos into an EXISTING group
// and enqueues an async index job. Returns 202 JobAck. This is the Settings
// "add repo" path of the shared wizard.
func (s *Server) handleV2ScanRepos(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if _, err := groupConfigPath(group); err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	var req v2ScanReposReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if len(req.Repos) == 0 {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "at least one repo required")
		return
	}
	if status, code, msg := s.registerWizardRepos(group, req.Repos); msg != "" {
		writeV2Err(w, status, code, msg)
		return
	}
	s.enqueueWizardIndex(w, group)
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared helpers
// ─────────────────────────────────────────────────────────────────────────────

// registerWizardRepos resolves + registers each wizard repo into the group.
// Returns ("", "", "") on success or (httpStatus, errCode, message) on failure.
func (s *Server) registerWizardRepos(group string, repos []v2WizardRepo) (int, string, string) {
	for _, spec := range repos {
		if spec.Path == "" {
			return http.StatusBadRequest, "bad_request", "repo path required"
		}
		abs, err := expandPath(spec.Path)
		if err != nil {
			return http.StatusBadRequest, "bad_request", fmt.Sprintf("bad path %q: %v", spec.Path, err)
		}
		if info, statErr := os.Stat(abs); statErr != nil || !info.IsDir() {
			return http.StatusBadRequest, "bad_request", fmt.Sprintf("path %q is not a directory", abs)
		}
		slug := spec.Slug
		if slug == "" {
			slug = slugify(filepath.Base(abs))
		}
		repo := registry.Repo{
			Slug:    slug,
			Path:    abs,
			Stack:   detect.Stack(abs),
			Modules: spec.Modules,
		}
		if err := s.registry.AddRepo(group, repo); err != nil {
			return http.StatusConflict, "conflict", fmt.Sprintf("add repo %q: %v", slug, err)
		}
	}
	return 0, "", ""
}

// enqueueWizardIndex enqueues a rebuild actionJob for the group and writes the
// 202 JobAck. It mirrors dispatchV2Rebuild but is the wizard's index step.
func (s *Server) enqueueWizardIndex(w http.ResponseWriter, group string) {
	token := fmt.Sprintf("wizard-%d", time.Now().UnixMilli())
	job := s.actionJobs.create("rebuild", group, "", token)

	args := proto.RebuildArgs{
		Group:         group,
		ProgressToken: token,
	}
	go s.runRebuildJob(job.ID, args)

	s.auditor.OK("wizard_index", group, map[string]any{"progress_token": token, "job_id": job.ID})

	ack := v2JobAck{
		JobID:         job.ID,
		Op:            "rebuild",
		Group:         group,
		Status:        actionJobQueued,
		ProgressToken: token,
		StatusURL:     "/api/v2/jobs/" + job.ID,
		StreamURL:     "/api/v2/jobs/" + job.ID + "/stream",
	}
	writeV2JSON(w, http.StatusAccepted, v2OK(ack))
}
