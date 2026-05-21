package dashboard

// handlers_maintenance.go — Maintenance ops REST surface (#1200)
//
// Exposes per-group and per-repo rebuild / reset actions plus the
// global cleanup command as HTTP endpoints. All mutating ops are async:
// they enqueue the work in the daemon via the existing RPC channel and
// return a 202 Accepted immediately so the frontend can open the
// progress SSE stream.
//
// Routes registered in server.go:
//
//	POST /api/groups/{group}/rebuild             — rebuild all repos in a group
//	POST /api/groups/{group}/repos/{repo}/rebuild  — rebuild one repo
//	POST /api/groups/{group}/reset               — wipe + rebuild all repos (DESTRUCTIVE)
//	POST /api/groups/{group}/repos/{repo}/reset    — wipe + rebuild one repo
//	POST /api/cleanup                            — remove orphaned registry entries
//	GET  /api/cleanup/preview                    — dry-run: list orphaned entries

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon/client"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// MaintenanceAckReply is returned by all async maintenance ops on success.
// Callers should subscribe to /api/index-progress/{group} (SSE) to track
// progress after receiving this ack.
type MaintenanceAckReply struct {
	// Op is the operation that was enqueued: "rebuild" | "reset".
	Op string `json:"op"`
	// Group is the target group slug.
	Group string `json:"group"`
	// Repo is the target repo slug, or empty when the op targets the whole group.
	Repo string `json:"repo,omitempty"`
	// ProgressToken is the token to pass to /api/index-progress to filter
	// events for this specific operation.
	ProgressToken string `json:"progress_token"`
	// EnqueuedAt is the RFC 3339 timestamp of when the op was accepted.
	EnqueuedAt string `json:"enqueued_at"`
}

// CleanupOrphan is a single orphaned registry entry found during cleanup.
type CleanupOrphan struct {
	Name       string `json:"name"`
	ConfigPath string `json:"config_path"`
}

// CleanupReply is returned by POST /api/cleanup and GET /api/cleanup/preview.
type CleanupReply struct {
	DryRun   bool            `json:"dry_run"`
	Orphaned []CleanupOrphan `json:"orphaned"`
	Removed  int             `json:"removed"`
	Message  string          `json:"message"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Rebuild handlers
// ─────────────────────────────────────────────────────────────────────────────

// handleGroupRebuild — POST /api/groups/{group}/rebuild
func (s *Server) handleGroupRebuild(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "missing group slug")
		return
	}
	s.dispatchRebuild(w, group, "", false)
}

// handleRepoRebuild — POST /api/groups/{group}/repos/{repo}/rebuild
func (s *Server) handleRepoRebuild(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	repo := r.PathValue("repo")
	if group == "" || repo == "" {
		writeErr(w, http.StatusBadRequest, "missing group or repo slug")
		return
	}
	s.dispatchRebuild(w, group, repo, false)
}

// ─────────────────────────────────────────────────────────────────────────────
// Reset handlers
// ─────────────────────────────────────────────────────────────────────────────

// handleGroupReset — POST /api/groups/{group}/reset
func (s *Server) handleGroupReset(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "missing group slug")
		return
	}
	s.dispatchRebuild(w, group, "", true)
}

// handleRepoReset — POST /api/groups/{group}/repos/{repo}/reset
func (s *Server) handleRepoReset(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	repo := r.PathValue("repo")
	if group == "" || repo == "" {
		writeErr(w, http.StatusBadRequest, "missing group or repo slug")
		return
	}
	s.dispatchRebuild(w, group, repo, true)
}

// ─────────────────────────────────────────────────────────────────────────────
// Cleanup handlers
// ─────────────────────────────────────────────────────────────────────────────

// handleCleanupPreview — GET /api/cleanup/preview
func (s *Server) handleCleanupPreview(w http.ResponseWriter, _ *http.Request) {
	s.runCleanup(w, true)
}

// handleCleanup — POST /api/cleanup
// Optional ?dry_run=true to preview without removing.
func (s *Server) handleCleanup(w http.ResponseWriter, r *http.Request) {
	dryRun := r.URL.Query().Get("dry_run") == "true"
	s.runCleanup(w, dryRun)
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

// dispatchRebuild is the shared implementation for rebuild and reset endpoints.
// It verifies the group exists, dials the daemon, fires the RPC asynchronously,
// and returns a 202 Accepted with a progress token.
func (s *Server) dispatchRebuild(w http.ResponseWriter, group, repo string, wipe bool) {
	// Verify the group is registered so we return a clear 404 rather than
	// letting the daemon produce an opaque error.
	groups, err := registry.Groups()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "registry: "+err.Error())
		return
	}
	found := false
	for _, g := range groups {
		if g.Name == group {
			found = true
			break
		}
	}
	if !found {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q not found in registry", group))
		return
	}

	// A short unique token the frontend uses to filter SSE events.
	token := fmt.Sprintf("web-%d", time.Now().UnixMilli())

	// Verify the daemon is reachable before returning 202.
	c, err := client.Dial()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable,
			"daemon not reachable — run 'archigraph start' first")
		return
	}
	c.Close() // close probe connection; goroutine will open its own

	op := "rebuild"
	if wipe {
		op = "reset"
	}

	// Fire the RPC asynchronously so the HTTP handler can return 202 right
	// away. The client opens its own connection to avoid cross-goroutine use
	// of net/rpc.Client.
	args := proto.RebuildArgs{
		Group:         group,
		Slug:          repo,
		Wipe:          wipe,
		ProgressToken: token,
	}
	go func() {
		bgClient, dialErr := client.Dial()
		if dialErr != nil {
			return
		}
		defer bgClient.Close()
		_, _ = bgClient.Rebuild(args)
	}()

	// Audit the dispatched operation.
	auditParams := map[string]any{"repo": repo, "wipe": wipe, "progress_token": token}
	s.auditor.OK(op, group, auditParams)

	reply := MaintenanceAckReply{
		Op:            op,
		Group:         group,
		Repo:          repo,
		ProgressToken: token,
		EnqueuedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	writeJSON(w, http.StatusAccepted, reply)
}

// runCleanup scans the registry for entries whose config file no longer
// exists and optionally removes them.
func (s *Server) runCleanup(w http.ResponseWriter, dryRun bool) {
	reg, err := registry.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "registry: "+err.Error())
		return
	}

	var orphaned []CleanupOrphan
	for _, g := range reg.Groups {
		if _, statErr := os.Stat(g.ConfigPath); statErr != nil && os.IsNotExist(statErr) {
			orphaned = append(orphaned, CleanupOrphan{
				Name:       g.Name,
				ConfigPath: g.ConfigPath,
			})
		}
	}
	if orphaned == nil {
		orphaned = []CleanupOrphan{}
	}

	reply := CleanupReply{
		DryRun:   dryRun,
		Orphaned: orphaned,
	}

	if len(orphaned) == 0 {
		reply.Message = "No orphaned registry entries found"
		writeJSON(w, http.StatusOK, reply)
		return
	}

	if dryRun {
		reply.Message = fmt.Sprintf(
			"%d orphaned %s found — POST /api/cleanup to remove",
			len(orphaned), maintenancePluralEntry(len(orphaned)),
		)
		writeJSON(w, http.StatusOK, reply)
		return
	}

	// Build a set of orphan names to remove.
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
	if saveErr := registry.Save(reg); saveErr != nil {
		writeErr(w, http.StatusInternalServerError, "registry save: "+saveErr.Error())
		return
	}

	reply.Removed = len(orphaned)
	reply.Message = fmt.Sprintf("Removed %d orphaned %s",
		len(orphaned), maintenancePluralEntry(len(orphaned)))
	s.auditor.OK("cleanup", "", map[string]any{"removed": len(orphaned)})
	writeJSON(w, http.StatusOK, reply)
}

func maintenancePluralEntry(n int) string {
	if n == 1 {
		return "entry"
	}
	return "entries"
}
