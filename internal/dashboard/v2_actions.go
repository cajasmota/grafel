// v2_actions.go — real REST wrappers over the CLI-only operations (#1512).
//
// Before this file, the Operations (#1444) and Settings (#1436) screens left
// the mutating actions as CLI-hint stubs ("no safe REST trigger"). The owner
// principle: if the CLI can do it, build a REST wrapper that calls the SAME
// internal functions the CLI command calls — never duplicate logic.
//
// Endpoints added here (all v2; v1 routes are UNCHANGED):
//
//	POST  /api/v2/groups/{group}/rebuild                  → async rebuild group
//	POST  /api/v2/groups/{group}/repos/{repo}/rebuild     → async rebuild repo
//	POST  /api/v2/groups/{group}/repos/{repo}/reset       → async wipe+rebuild repo
//	GET   /api/v2/jobs/{id}                               → job status/progress
//	GET   /api/v2/jobs/{id}/stream                        → job SSE feed
//	POST  /api/v2/maintenance/cleanup                     → preview/execute cleanup
//	POST  /api/v2/update/apply                            → run `archigraph update`
//	POST  /api/v2/patterns/{group}/export                 → export approved patterns
//	POST  /api/v2/patterns/{group}/gc                     → gc candidate patterns
//
// Async-job design (rebuild/reset):
//
//	The HTTP handler MUST NOT block on the index — the daemon has to keep
//	serving reads while a rebuild runs (the #1487 serving-mutex invariant).
//	So the handler enqueues an actionJob (see v2_jobs.go), fires the daemon
//	`Rebuild` RPC in a background goroutine (the exact path the CLI `rebuild`
//	command + the v1 /api/groups/{group}/rebuild handler take), and returns
//	202 immediately with the job id. The job tracks the RPC lifecycle; live
//	per-file progress is still available on /api/index-progress/{group}.
//
// Daemon install/uninstall is intentionally NOT wrapped here — see API_V2.md
// §10: it requires launchd privileges that have no safe in-process REST trigger.

package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/cajasmota/archigraph/internal/agentpatterns"
	"github.com/cajasmota/archigraph/internal/daemon/client"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// v2JobAck is the 202 body returned by every async action endpoint.
type v2JobAck struct {
	JobID         string `json:"job_id"`
	Op            string `json:"op"`
	Group         string `json:"group"`
	Repo          string `json:"repo,omitempty"`
	Status        string `json:"status"`
	ProgressToken string `json:"progress_token"`
	// StreamURL / StatusURL are convenience hrefs for the frontend.
	StatusURL string `json:"status_url"`
	StreamURL string `json:"stream_url"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Async rebuild / reset
// ─────────────────────────────────────────────────────────────────────────────

// rebuildRunner runs the actual rebuild for the given args and returns the
// daemon reply. The default implementation dials the daemon and fires the
// `Rebuild` RPC (the exact path the CLI `rebuild` command takes). Overridable
// in tests so the async lifecycle can be exercised without a live daemon.
type rebuildRunner func(args proto.RebuildArgs) (proto.RebuildReply, error)

// defaultRebuildRunner dials the daemon and fires the Rebuild RPC.
func defaultRebuildRunner(args proto.RebuildArgs) (proto.RebuildReply, error) {
	bg, err := client.Dial()
	if err != nil {
		return proto.RebuildReply{}, fmt.Errorf("daemon dial failed: %w", err)
	}
	defer bg.Close()
	return bg.Rebuild(args)
}

// dispatchV2Rebuild is the shared async path for the v2 rebuild/reset
// endpoints. It mirrors the v1 dispatchRebuild logic (registry validation +
// daemon probe + background RPC) but wraps the lifecycle in an actionJob so
// the v2 screens get a pollable/streamable job id.
func (s *Server) dispatchV2Rebuild(w http.ResponseWriter, group, repo string, wipe bool) {
	// Validate the group exists — call the same registry func the CLI uses.
	if _, err := groupConfigPath(group); err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	// Probe the daemon before accepting so we fail fast with a clear 503.
	// Skipped when a test runner is injected.
	if s.rebuildRunner == nil {
		probe, err := client.Dial()
		if err != nil {
			writeV2Err(w, http.StatusServiceUnavailable, "unavailable",
				"daemon not reachable — run 'archigraph start' first")
			return
		}
		probe.Close()
	}

	op := "rebuild"
	if wipe {
		op = "reset"
	}
	token := fmt.Sprintf("web-%d", time.Now().UnixMilli())
	job := s.actionJobs.create(op, group, repo, token)

	args := proto.RebuildArgs{
		Group:         group,
		Slug:          repo,
		Wipe:          wipe,
		ProgressToken: token,
	}

	// Fire the RPC asynchronously. The handler returns 202 immediately; this
	// goroutine owns the job lifecycle and never touches the HTTP response.
	go s.runRebuildJob(job.ID, args)

	// Audit identically to the v1 handler.
	s.auditor.OK(op, group, map[string]any{"repo": repo, "wipe": wipe, "progress_token": token, "job_id": job.ID})

	ack := v2JobAck{
		JobID:         job.ID,
		Op:            op,
		Group:         group,
		Repo:          repo,
		Status:        actionJobQueued,
		ProgressToken: token,
		StatusURL:     "/api/v2/jobs/" + job.ID,
		StreamURL:     "/api/v2/jobs/" + job.ID + "/stream",
	}
	writeV2JSON(w, http.StatusAccepted, v2OK(ack))
}

// runRebuildJob executes the daemon Rebuild RPC and tracks the actionJob
// status through its lifecycle. Runs entirely in a background goroutine.
func (s *Server) runRebuildJob(jobID string, args proto.RebuildArgs) {
	now := time.Now().UnixMilli()
	s.actionJobs.update(jobID, func(j *actionJob) {
		j.Status = actionJobRunning
		j.StartedAt = &now
		j.Progress = 5
		j.Message = "indexing started"
	})

	runner := s.rebuildRunner
	if runner == nil {
		runner = defaultRebuildRunner
	}
	reply, err := runner(args)
	if err != nil {
		s.markJobFailed(jobID, err.Error())
		return
	}

	fin := time.Now().UnixMilli()
	s.actionJobs.update(jobID, func(j *actionJob) {
		j.Status = actionJobDone
		j.Progress = 100
		j.FinishedAt = &fin
		j.Message = fmt.Sprintf("rebuilt %d repo(s): %d entities, %d relationships",
			len(reply.Repos), reply.TotalEntities, reply.TotalRels)
		if reply.Warning != "" {
			j.Message += " (warning: " + reply.Warning + ")"
		}
	})
}

func (s *Server) markJobFailed(jobID, msg string) {
	fin := time.Now().UnixMilli()
	s.actionJobs.update(jobID, func(j *actionJob) {
		j.Status = actionJobFailed
		j.FinishedAt = &fin
		j.Error = msg
	})
}

// handleV2RebuildGroupAsync — POST /api/v2/groups/{group}/rebuild (real).
func (s *Server) handleV2RebuildGroupAsync(w http.ResponseWriter, r *http.Request) {
	s.dispatchV2Rebuild(w, r.PathValue("group"), "", false)
}

// handleV2RebuildRepoAsync — POST /api/v2/groups/{group}/repos/{repo}/rebuild (real).
func (s *Server) handleV2RebuildRepoAsync(w http.ResponseWriter, r *http.Request) {
	s.dispatchV2Rebuild(w, r.PathValue("group"), r.PathValue("repo"), false)
}

// handleV2ResetRepoAsync — POST /api/v2/groups/{group}/repos/{repo}/reset (real).
func (s *Server) handleV2ResetRepoAsync(w http.ResponseWriter, r *http.Request) {
	s.dispatchV2Rebuild(w, r.PathValue("group"), r.PathValue("repo"), true)
}

// ─────────────────────────────────────────────────────────────────────────────
// Maintenance cleanup — wraps `archigraph cleanup`
// ─────────────────────────────────────────────────────────────────────────────

// handleV2Cleanup — POST /api/v2/maintenance/cleanup
//
// Body: { "dry_run": true } previews orphaned registry entries (default);
// { "dry_run": false } removes them. Wraps the same registry scan the v1
// /api/cleanup handler and the `archigraph cleanup` command perform.
func (s *Server) handleV2Cleanup(w http.ResponseWriter, r *http.Request) {
	req := struct {
		DryRun bool `json:"dry_run"`
	}{DryRun: true}
	_ = json.NewDecoder(r.Body).Decode(&req)

	reg, err := registry.Load()
	if err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", "registry: "+err.Error())
		return
	}

	orphaned := []CleanupOrphan{}
	for _, g := range reg.Groups {
		if !configExists(g.ConfigPath) {
			orphaned = append(orphaned, CleanupOrphan{Name: g.Name, ConfigPath: g.ConfigPath})
		}
	}

	reply := CleanupReply{DryRun: req.DryRun, Orphaned: orphaned}
	if len(orphaned) == 0 {
		reply.Message = "No orphaned registry entries found"
		writeV2JSON(w, http.StatusOK, v2OK(reply))
		return
	}
	if req.DryRun {
		reply.Message = fmt.Sprintf("%d orphaned %s found — POST with dry_run:false to remove",
			len(orphaned), maintenancePluralEntry(len(orphaned)))
		writeV2JSON(w, http.StatusOK, v2OK(reply))
		return
	}

	orphanSet := make(map[string]struct{}, len(orphaned))
	for _, o := range orphaned {
		orphanSet[o.Name] = struct{}{}
	}
	var kept []registry.GroupRef
	for _, g := range reg.Groups {
		if _, bad := orphanSet[g.Name]; !bad {
			kept = append(kept, g)
		}
	}
	reg.Groups = kept
	if err := registry.Save(reg); err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", "registry save: "+err.Error())
		return
	}
	reply.Removed = len(orphaned)
	reply.Message = fmt.Sprintf("Removed %d orphaned %s", len(orphaned), maintenancePluralEntry(len(orphaned)))
	s.auditor.OK("cleanup", "", map[string]any{"removed": len(orphaned)})
	writeV2JSON(w, http.StatusOK, v2OK(reply))
}

// ─────────────────────────────────────────────────────────────────────────────
// Update apply — wraps `archigraph update`
// ─────────────────────────────────────────────────────────────────────────────

// v2UpdateApplyReply is the result of running `archigraph update`.
type v2UpdateApplyReply struct {
	ExitCode int      `json:"exit_code"`
	Output   []string `json:"output"`
	Applied  bool     `json:"applied"`
}

// handleV2UpdateApply — POST /api/v2/update/apply
//
// Runs `archigraph update` as a subprocess (so this daemon is not replaced
// mid-request) via the SAME defaultUpdateRunner the v1 SSE handler uses, and
// returns the captured output in one JSON envelope. The version check is
// already live at GET /api/updates/check.
func (s *Server) handleV2UpdateApply(w http.ResponseWriter, r *http.Request) {
	out, runErr := defaultUpdateRunner(r.Context(), []string{"update"})

	lines := splitLines(string(out))
	if lines == nil {
		lines = []string{}
	}
	reply := v2UpdateApplyReply{Output: lines}
	reply.ExitCode, reply.Applied = exitCodeFromErr(runErr)
	s.auditor.OK("update", "", map[string]any{"exit_code": reply.ExitCode})
	writeV2JSON(w, http.StatusOK, v2OK(reply))
}

// ─────────────────────────────────────────────────────────────────────────────
// Patterns export / gc — wraps `archigraph patterns export|gc`
// ─────────────────────────────────────────────────────────────────────────────

// handleV2PatternExport — POST /api/v2/patterns/{group}/export
//
// Body: { "file": "/abs/CLAUDE.md" } or { "repo": "/abs/repo" }. Calls the
// SAME agentpatterns.UpsertFile path the v1 handler + CLI use.
func (s *Server) handleV2PatternExport(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	res, status, errMsg := s.exportPatterns(group, r)
	if errMsg != "" {
		code := "internal_error"
		if status == http.StatusBadRequest {
			code = "bad_request"
		}
		writeV2Err(w, status, code, errMsg)
		return
	}
	writeV2JSON(w, http.StatusOK, v2OK(res))
}

// handleV2PatternGC — POST /api/v2/patterns/{group}/gc
//
// Body: { "dry_run": true } (default) previews; false prunes. Calls the SAME
// agentpatterns gc path the v1 handler + CLI use.
func (s *Server) handleV2PatternGC(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	res, status, errMsg := s.gcPatterns(group, r)
	if errMsg != "" {
		writeV2Err(w, status, "internal_error", errMsg)
		return
	}
	writeV2JSON(w, http.StatusOK, v2OK(res))
}

// ─────────────────────────────────────────────────────────────────────────────
// Small helpers
// ─────────────────────────────────────────────────────────────────────────────

// configExists reports whether a group config file is present on disk.
func configExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// exitCodeFromErr maps a subprocess error to (exitCode, applied).
func exitCodeFromErr(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), false
	}
	return 1, false
}

// exportPatterns runs the pattern-export logic (same agentpatterns.UpsertFile
// path the CLI uses) and returns (result, httpStatus, errMsg).
func (s *Server) exportPatterns(group string, r *http.Request) (map[string]any, int, string) {
	var req struct {
		File string `json:"file"`
		Repo string `json:"repo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, http.StatusBadRequest, "invalid JSON: " + err.Error()
	}
	target := req.File
	if target == "" {
		if req.Repo == "" {
			return nil, http.StatusBadRequest, "pass file or repo in request body"
		}
		target = filepath.Join(req.Repo, "CLAUDE.md")
	}

	dir := groupPatternsDir(group)
	patterns, err := loadAgentPatterns(dir)
	if err != nil {
		return nil, http.StatusInternalServerError, "load patterns: " + err.Error()
	}
	if err := agentpatterns.UpsertFile(target, patterns, agentpatterns.ExportOptions{}); err != nil {
		return nil, http.StatusInternalServerError, "export: " + err.Error()
	}
	approved := 0
	for _, p := range patterns {
		if !p.IsCandidate {
			approved++
		}
	}
	s.auditor.OK("patterns_export", group, map[string]any{"target": target, "exported": approved})
	return map[string]any{"exported": approved, "target": target}, http.StatusOK, ""
}

// gcPatterns runs the candidate-pattern GC (same agentpatterns path as the CLI)
// and returns (result, httpStatus, errMsg).
func (s *Server) gcPatterns(group string, r *http.Request) (map[string]any, int, string) {
	req := struct {
		DryRun bool `json:"dry_run"`
	}{DryRun: true}
	_ = json.NewDecoder(r.Body).Decode(&req)

	dir := groupPatternsDir(group)
	patterns, err := loadAgentPatterns(dir)
	if err != nil {
		return nil, http.StatusInternalServerError, "load patterns: " + err.Error()
	}
	cfg, err := agentpatterns.LoadConfig(dir)
	if err != nil {
		return nil, http.StatusInternalServerError, "load config: " + err.Error()
	}
	cutoff := time.Now().Add(-time.Duration(cfg.CandidateDecayDays) * 24 * time.Hour).Unix()

	var keep, pruned []agentpatterns.Pattern
	for _, p := range patterns {
		if p.IsCandidate && p.LastValidated > 0 && p.LastValidated < cutoff {
			pruned = append(pruned, p)
		} else {
			keep = append(keep, p)
		}
	}
	if !req.DryRun && len(pruned) > 0 {
		if err := agentpatterns.Save(dir, keep); err != nil {
			return nil, http.StatusInternalServerError, "save patterns: " + err.Error()
		}
	}
	pruneRows := make([]map[string]any, 0, len(pruned))
	for _, p := range pruned {
		pruneRows = append(pruneRows, patternToRow(p))
	}
	if !req.DryRun {
		s.auditor.OK("patterns_gc", group, map[string]any{"pruned": len(pruned)})
	}
	return map[string]any{
		"dry_run":              req.DryRun,
		"pruned_count":         len(pruned),
		"pruned":               pruneRows,
		"remaining_count":      len(keep),
		"candidate_decay_days": cfg.CandidateDecayDays,
	}, http.StatusOK, ""
}
