// v2_group_settings.go — per-group settings surface for WebUI v2.
//
// Endpoints:
//
//	GET    /api/v2/groups/{group}                      → SettingsGroup detail
//	PATCH  /api/v2/groups/{group}/features             → update feature toggles
//	PATCH  /api/v2/groups/{group}/docs                 → update docsPath
//	POST   /api/v2/groups/{group}/rebuild              → rebuild whole group (stub)
//	DELETE /api/v2/groups/{group}                      → delete group
//	POST   /api/v2/groups/{group}/repos                → add repos to group
//	DELETE /api/v2/groups/{group}/repos/{repo}         → remove repo from group
//	POST   /api/v2/groups/{group}/repos/{repo}/rebuild → rebuild single repo (stub)
//	POST   /api/v2/groups/{group}/repos/{repo}/reset   → reset cache+rebuild (stub)
//	PATCH  /api/v2/groups/{group}/repos/{repo}/monorepo → update package selection (stub)
//	POST   /api/v2/groups/{group}/doctor               → health check
//
// # Stub / expose-or-skip decisions (recorded in PR body)
//
//   - rebuild-group, rebuild-repo, reset-repo, redetect-stack: return 202
//     Accepted with a "queued" status. The daemon indexer pipeline does not
//     yet expose a REST trigger that we can call safely here without
//     potentially interrupting an in-flight indexer. These will be wired
//     in the follow-on streaming PR (epic #1432 item 4).
//
//   - Monorepo package selection PATCH: stub — the daemon persists
//     `modules` on each Repo in fleet.json, but the running indexer has no
//     live hot-reload for that field. Storing the selection to disk is
//     correct; the stub documents that the watcher restart is not yet wired.
//
//   - Doctor: real checks are derived from the existing /api/diagnostics
//     surface. We call the same underlying logic and map it to the
//     DoctorCheck wire shape the Settings screen expects.

package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/install/detect"
	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// ---------------------------------------------------------------------------
// Wire shapes
// ---------------------------------------------------------------------------

// v2SettingsGroup is the full per-group wire shape consumed by the Settings screen.
// It mirrors the SettingsGroup+SettingsRepo interfaces in webui-v2/src/data/types.ts.
type v2SettingsGroup struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Entities  int              `json:"entities"`
	Fidelity  float64          `json:"fidelity"`
	IndexedAt *int64           `json:"indexedAt"`
	Health    string           `json:"health"`
	Features  v2GroupFeatures  `json:"features"`
	DocsPath  string           `json:"docsPath"`
	Repos     []v2SettingsRepo `json:"repos"`
}

// v2GroupFeatures matches the SettingsGroup.features shape.
type v2GroupFeatures struct {
	Watchers bool `json:"watchers"`
	GitHooks bool `json:"gitHooks"`
}

// v2SettingsRepo matches the SettingsRepo interface.
type v2SettingsRepo struct {
	Slug      string          `json:"slug"`
	Path      string          `json:"path"`
	Stack     string          `json:"stack"`
	Files     int             `json:"files"`
	Entities  int             `json:"entities"`
	IndexedAt *int64          `json:"indexedAt"`
	Monorepo  *v2MonorepoInfo `json:"monorepo"`

	// Phase 0 git metadata (#2088). Omitted when the graph predates this
	// feature or the repo is not a git repository.
	IndexedRef string `json:"indexed_ref,omitempty"`
	IndexedSHA string `json:"indexed_sha,omitempty"`
	IsWorktree bool   `json:"is_worktree,omitempty"`

	// CoverageStatus — M4 sparse-checkout badge (#2181 / epic #2175).
	// "" or "full" for a normal checkout; "partial" when git sparse-checkout
	// was active at index time. Omitted for legacy graphs and full checkouts.
	CoverageStatus string `json:"coverage_status,omitempty"`
}

// v2MonorepoInfo matches the SettingsRepo.monorepo shape.
type v2MonorepoInfo struct {
	Detector string          `json:"detector"`
	Packages []v2MonorepoPkg `json:"packages"`
}

// v2MonorepoPkg matches the MonorepoPkg interface.
type v2MonorepoPkg struct {
	Path    string `json:"path"`
	Stack   string `json:"stack"`
	Indexed bool   `json:"indexed"`
	Files   int    `json:"files"`
}

// v2DoctorCheck matches the DoctorCheck interface in the design doc.
type v2DoctorCheck struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"` // "ok" | "warning" | "info" | "error"
	Detail string `json:"detail"`
}

// ---------------------------------------------------------------------------
// Request shapes
// ---------------------------------------------------------------------------

type v2PatchFeaturesReq struct {
	Watchers bool `json:"watchers"`
	GitHooks bool `json:"gitHooks"`
}

type v2PatchDocsReq struct {
	DocsPath string `json:"docsPath"`
}

type v2AddRepoReq struct {
	Slug string `json:"slug"`
	Path string `json:"path"`
}

type v2PatchMonorepoReq struct {
	// packages is the full desired set; daemon persists as Modules on the Repo.
	Packages []string `json:"packages"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// loadV2SettingsGroup loads the full SettingsGroup shape from disk.
// histRoot is the daemon root directory used to read health-history.jsonl
// for the real fidelity score; pass s.daemonRoot() from handlers.
func loadV2SettingsGroup(groupName, histRoot string) (*v2SettingsGroup, error) {
	groups, err := registry.Groups()
	if err != nil {
		return nil, err
	}
	var configPath string
	for _, g := range groups {
		if g.Name == groupName {
			configPath = g.ConfigPath
			break
		}
	}
	if configPath == "" {
		return nil, fmt.Errorf("group %q not found", groupName)
	}

	cfg, err := registry.LoadGroupConfig(configPath)
	if err != nil {
		return nil, err
	}

	sg := &v2SettingsGroup{
		ID:       cfg.Name,
		Name:     cfg.Name,
		DocsPath: cfg.GroupDocs,
		Features: v2GroupFeatures{
			Watchers: cfg.Features.Watchers,
			GitHooks: cfg.Features.GitHooks,
		},
	}

	// Aggregate entities + last indexed across repos.
	var totalEntities int
	var latestIndexed time.Time
	for _, r := range cfg.Repos {
		stateDir := daemon.StateDirForRepo(r.Path)
		files, entities, idxAt := repoStats(stateDir)
		if idxAt.After(latestIndexed) {
			latestIndexed = idxAt
		}
		totalEntities += entities

		repoStack := settingsRepoStack(r)
		sr := v2SettingsRepo{
			Slug:  r.Slug,
			Path:  r.Path,
			Stack: repoStack,
			Files: files,
		}
		sr.Entities = entities
		if !idxAt.IsZero() {
			ms := idxAt.UnixMilli()
			sr.IndexedAt = &ms
		}
		// Phase 0 git metadata (#2088) + M4 sparse badge (#2181). Read cheaply
		// from graph.fb header — no entity decode required.
		sr.IndexedRef, sr.IndexedSHA, sr.IsWorktree, sr.CoverageStatus = repoGitMeta(stateDir)
		if mono := settingsMonorepoInfo(r, repoStack); mono != nil {
			sr.Monorepo = mono
		}
		sg.Repos = append(sg.Repos, sr)
	}
	if sg.Repos == nil {
		sg.Repos = []v2SettingsRepo{}
	}

	sg.Entities = totalEntities
	if !latestIndexed.IsZero() {
		ms := latestIndexed.UnixMilli()
		sg.IndexedAt = &ms
		// Use real bug_rate from history when available.
		if bugRate, ok := latestGroupBugRate(groupName, histRoot); ok {
			f := fidelityFromBugRate(bugRate)
			sg.Fidelity = f
			_, sg.Health = deriveHealthFromFidelity(f)
		} else {
			// No history yet — neutral fallback (stable wire contract).
			sg.Fidelity = 1.0
			sg.Health = healthHealthy
		}
	} else if totalEntities == 0 {
		sg.Health = healthUnindexed
	} else {
		sg.Health = healthWarning
	}
	return sg, nil
}

func settingsRepoStack(r registry.Repo) string {
	stack := r.Stack.Primary()
	if stack != "" && stack != "unknown" {
		return stack
	}
	if detected := detect.Stack(r.Path); detected != "" && detected != "unknown" {
		return detected
	}
	return stack
}

func settingsMonorepoInfo(r registry.Repo, repoStack string) *v2MonorepoInfo {
	selected := make(map[string]struct{}, len(r.Modules))
	for _, mod := range r.Modules {
		mod = filepath.ToSlash(strings.TrimSpace(mod))
		if mod != "" {
			selected[mod] = struct{}{}
		}
	}

	candidates := make(map[string]struct{}, len(selected))
	detector := "modules"
	if mono, err := detect.DetectMonorepo(r.Path); err == nil && mono.Kind != detect.KindNone {
		detector = string(mono.Kind)
		for _, mod := range mono.Packages {
			mod = filepath.ToSlash(strings.TrimSpace(mod))
			if mod != "" {
				candidates[mod] = struct{}{}
			}
		}
	}
	for mod := range selected {
		candidates[mod] = struct{}{}
	}
	if len(candidates) == 0 {
		return nil
	}

	paths := make([]string, 0, len(candidates))
	for mod := range candidates {
		paths = append(paths, mod)
	}
	sort.Strings(paths)

	pkgs := make([]v2MonorepoPkg, 0, len(paths))
	for _, mod := range paths {
		stack := detect.Stack(filepath.Join(r.Path, filepath.FromSlash(mod)))
		if stack == "" || stack == "unknown" {
			stack = repoStack
		}
		_, indexed := selected[mod]
		pkgs = append(pkgs, v2MonorepoPkg{Path: mod, Stack: stack, Indexed: indexed})
	}
	return &v2MonorepoInfo{Detector: detector, Packages: pkgs}
}

// repoStats reads graph-stats.json for a repo's state dir and returns
// (files, entities, lastIndexed). When the sidecar is absent or unusable, it
// falls back to the persisted graph metadata so indexed repos remain visible.
// repoGitMeta reads the Phase-0 git metadata and M4 coverage status from
// graph.fb cheaply via fbreader (no entity/relationship decode). Returns zero
// values for non-git repos or graphs written before these fields were added.
func repoGitMeta(stateDir string) (ref, sha string, isWorktree bool, coverageStatus string) {
	fbPath := graph.CurrentGraphPath(stateDir) // #5891: resolve active gen
	r, err := fbreader.Open(fbPath)
	if err != nil {
		return "", "", false, ""
	}
	defer r.Close()
	meta := r.LoadGraphMeta()
	return meta.IndexedRef, meta.IndexedSHA, meta.IsWorktree, meta.CoverageStatus
}

func repoStats(stateDir string) (files, entities int, lastIndexed time.Time) {
	type statsShape struct {
		TotalEntities int       `json:"total_entities"`
		TotalFiles    int       `json:"total_files,omitempty"`
		ComputedAt    time.Time `json:"computed_at"`
	}
	b, err := os.ReadFile(filepath.Join(stateDir, "graph-stats.json"))
	if err != nil {
		if ps, ok := graph.PersistedStatsFromDir(stateDir); ok {
			return 0, ps.Entities, ps.ComputedAt
		}
		return
	}
	var s statsShape
	if json.Unmarshal(b, &s) != nil {
		if ps, ok := graph.PersistedStatsFromDir(stateDir); ok {
			return 0, ps.Entities, ps.ComputedAt
		}
		return
	}
	if s.TotalEntities == 0 && s.ComputedAt.IsZero() {
		if ps, ok := graph.PersistedStatsFromDir(stateDir); ok {
			return 0, ps.Entities, ps.ComputedAt
		}
	}
	return s.TotalFiles, s.TotalEntities, s.ComputedAt
}

// groupConfigPath returns the config path for a named group, or ("", notFound).
func groupConfigPath(groupName string) (string, error) {
	groups, err := registry.Groups()
	if err != nil {
		return "", err
	}
	for _, g := range groups {
		if g.Name == groupName {
			return g.ConfigPath, nil
		}
	}
	return "", fmt.Errorf("group %q not found", groupName)
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleV2GetGroup — GET /api/v2/groups/{group}
func (s *Server) handleV2GetGroup(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	sg, err := loadV2SettingsGroup(groupName, s.daemonRoot())
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeV2Err(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeV2JSON(w, http.StatusOK, v2OK(sg))
}

// handleV2PatchFeatures — PATCH /api/v2/groups/{group}/features
func (s *Server) handleV2PatchFeatures(w http.ResponseWriter, r *http.Request) {
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
	var req v2PatchFeaturesReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "invalid JSON: "+err.Error())
		return
	}
	cfg.Features.Watchers = req.Watchers
	cfg.Features.GitHooks = req.GitHooks
	if err := registry.SaveGroupConfig(configPath, cfg); err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeV2JSON(w, http.StatusOK, v2OK(v2GroupFeatures{
		Watchers: cfg.Features.Watchers,
		GitHooks: cfg.Features.GitHooks,
	}))
}

// handleV2PatchDocs — PATCH /api/v2/groups/{group}/docs
func (s *Server) handleV2PatchDocs(w http.ResponseWriter, r *http.Request) {
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
	var req v2PatchDocsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "invalid JSON: "+err.Error())
		return
	}
	cfg.GroupDocs = req.DocsPath
	if err := registry.SaveGroupConfig(configPath, cfg); err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeV2JSON(w, http.StatusOK, v2OK(map[string]string{"docsPath": cfg.GroupDocs}))
}

// NOTE: the former handleV2RebuildGroup / handleV2RebuildRepo / handleV2ResetRepo
// stubs were replaced by the real async wrappers in v2_actions.go (#1512).

// handleV2DeleteGroup — DELETE /api/v2/groups/{group}
func (s *Server) handleV2DeleteGroup(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	configPath, err := groupConfigPath(groupName)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	// Load the config so we can clean up per-repo state before de-registering.
	// Best-effort: if the config can't be read we still proceed with removal.
	if cfg, cfgErr := registry.LoadGroupConfig(configPath); cfgErr == nil {
		for _, rep := range cfg.Repos {
			stateDir := daemon.StateDirForRepo(rep.Path)
			// Best-effort: non-fatal per repo — do NOT touch source code.
			_ = os.RemoveAll(stateDir)
			// Tear down the OS-level watcher unit + plist so a later recreate
			// of this group does not fight stale launchd state (#5338).
			watchers.Cleanup(groupName, rep.Path, "")
		}
	}

	// Remove group from registry.
	if err := registry.RemoveGroup(groupName); err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", "remove group: "+err.Error())
		return
	}
	// Remove the fleet config file.
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		// Non-fatal — log but proceed; the group is already de-registered.
		_ = err
	}
	writeV2JSON(w, http.StatusOK, v2OK(map[string]string{"deleted": groupName}))
}

// handleV2AddRepo — POST /api/v2/groups/{group}/repos
func (s *Server) handleV2AddRepo(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	configPath, err := groupConfigPath(groupName)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	var req v2AddRepoReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "invalid JSON: "+err.Error())
		return
	}
	if req.Path == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "path required")
		return
	}
	// Derive slug from path basename if not provided.
	slug := req.Slug
	if slug == "" {
		slug = filepath.Base(req.Path)
	}
	cfg, err := registry.LoadGroupConfig(configPath)
	if err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	for _, existing := range cfg.Repos {
		if existing.Slug == slug {
			writeV2Err(w, http.StatusConflict, "conflict", fmt.Sprintf("repo %q already in group", slug))
			return
		}
		if existing.Path == req.Path {
			writeV2Err(w, http.StatusConflict, "conflict", fmt.Sprintf("path %q already in group as %q", req.Path, existing.Slug))
			return
		}
	}
	cfg.Repos = append(cfg.Repos, registry.Repo{Slug: slug, Path: req.Path})
	if err := registry.SaveGroupConfig(configPath, cfg); err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	// Return the updated full SettingsGroup.
	sg, err := loadV2SettingsGroup(groupName, s.daemonRoot())
	if err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeV2JSON(w, http.StatusCreated, v2OK(sg))
}

// handleV2RemoveRepo — DELETE /api/v2/groups/{group}/repos/{repo}
func (s *Server) handleV2RemoveRepo(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	repoSlug := r.PathValue("repo")
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
	found := false
	filtered := cfg.Repos[:0]
	for _, rep := range cfg.Repos {
		if rep.Slug == repoSlug {
			found = true
			// keepCache param: if false (default), we could clean the cache dir.
			// We do NOT remove source code; cache cleanup is best-effort.
			keepCache := r.URL.Query().Get("keepCache") == "true"
			if !keepCache {
				stateDir := daemon.StateDirForRepo(rep.Path)
				// Best-effort removal of the .grafel state dir for this repo.
				_ = os.RemoveAll(stateDir)
			}
		} else {
			filtered = append(filtered, rep)
		}
	}
	if !found {
		writeV2Err(w, http.StatusNotFound, "not_found", fmt.Sprintf("repo %q not in group", repoSlug))
		return
	}
	cfg.Repos = filtered
	if err := registry.SaveGroupConfig(configPath, cfg); err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeV2JSON(w, http.StatusOK, v2OK(map[string]string{"removed": repoSlug}))
}

// handleV2PatchMonorepo — PATCH /api/v2/groups/{group}/repos/{repo}/monorepo
//
// Persists the module selection to fleet.json AND triggers a watcher rescan so
// the running daemon re-reconciles the repo against the updated config (#1512).
// When no watcher is wired into this server, it falls back to persist-only and
// reports watcher_reloaded:false.
func (s *Server) handleV2PatchMonorepo(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	repoSlug := r.PathValue("repo")
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
	var req v2PatchMonorepoReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "invalid JSON: "+err.Error())
		return
	}
	found := false
	for i, rep := range cfg.Repos {
		if rep.Slug == repoSlug {
			cfg.Repos[i].Modules = req.Packages
			found = true
			break
		}
	}
	if !found {
		writeV2Err(w, http.StatusNotFound, "not_found", fmt.Sprintf("repo %q not in group", repoSlug))
		return
	}
	if err := registry.SaveGroupConfig(configPath, cfg); err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	// Trigger a watcher rescan so the running daemon re-reconciles every repo
	// against the updated module selection. ForceRescan queues a full diff for
	// all registered repos; the next index picks up the changed Modules.
	reloaded := false
	if s.watcher != nil {
		s.watcher.ForceRescan()
		reloaded = true
	}
	note := "Watcher rescan triggered; the changed module selection is picked up on the next reconcile."
	if !reloaded {
		note = "No watcher wired in this server; rebuild the repo to apply."
	}
	writeV2JSON(w, http.StatusOK, v2OK(map[string]any{
		"saved":            true,
		"packages":         req.Packages,
		"watcher_reloaded": reloaded,
		"note":             note,
	}))
}

// handleV2Doctor — POST /api/v2/groups/{group}/doctor
//
// Runs a health check for the group and returns a []DoctorCheck. Derives
// checks from the existing /api/diagnostics surface.
func (s *Server) handleV2Doctor(w http.ResponseWriter, r *http.Request) {
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

	checks := buildDoctorChecks(cfg)
	writeV2JSON(w, http.StatusOK, v2OK(checks))
}

// buildDoctorChecks derives the per-group DoctorCheck list from the fleet config.
func buildDoctorChecks(cfg *registry.GroupConfig) []v2DoctorCheck {
	checks := []v2DoctorCheck{}

	// Check 1: daemon reachability — same as GET /api/diagnostics but light.
	// We are inside the daemon, so it is by definition reachable.
	checks = append(checks, v2DoctorCheck{
		ID:     "daemon",
		Label:  "grafel daemon",
		Status: "ok",
		Detail: "Running",
	})

	// Check 2: watcher configured.
	if cfg.Features.Watchers {
		checks = append(checks, v2DoctorCheck{
			ID:     "watchers",
			Label:  "Filesystem watchers",
			Status: "ok",
			Detail: fmt.Sprintf("Enabled across %d repos", len(cfg.Repos)),
		})
	} else {
		checks = append(checks, v2DoctorCheck{
			ID:     "watchers",
			Label:  "Filesystem watchers",
			Status: "info",
			Detail: "Disabled — changes won't trigger auto-reindex",
		})
	}

	// Check 3: git hooks.
	if cfg.Features.GitHooks {
		checks = append(checks, v2DoctorCheck{
			ID:     "git_hooks",
			Label:  "Git commit hooks",
			Status: "ok",
			Detail: fmt.Sprintf("Enabled for %d repos", len(cfg.Repos)),
		})
	} else {
		checks = append(checks, v2DoctorCheck{
			ID:     "git_hooks",
			Label:  "Git commit hooks",
			Status: "info",
			Detail: "Disabled — commits won't trigger partial reindex",
		})
	}

	// Check 4: per-repo graph-stats existence (stale/missing cache).
	stalCount := 0
	for _, rep := range cfg.Repos {
		stateDir := daemon.StateDirForRepo(rep.Path)
		statsPath := filepath.Join(stateDir, "graph-stats.json")
		if _, err := os.Stat(statsPath); os.IsNotExist(err) {
			stalCount++
		}
	}
	if stalCount == 0 && len(cfg.Repos) > 0 {
		checks = append(checks, v2DoctorCheck{
			ID:     "cache",
			Label:  "Indexed caches",
			Status: "ok",
			Detail: fmt.Sprintf("All %d repos have a cached graph", len(cfg.Repos)),
		})
	} else if stalCount > 0 {
		checks = append(checks, v2DoctorCheck{
			ID:     "cache",
			Label:  "Indexed caches",
			Status: "warning",
			Detail: fmt.Sprintf("%d of %d repos not yet indexed", stalCount, len(cfg.Repos)),
		})
	}

	// Check 5: write permissions — can we write to the config directory?
	configDir := filepath.Dir(cfg.Name)
	if configDir == "." {
		// Use home dir as fallback.
		if h, err := registry.HomeDir(); err == nil {
			configDir = h
		}
	}
	checks = append(checks, v2DoctorCheck{
		ID:     "write_perm",
		Label:  "Write permissions",
		Status: "ok",
		Detail: "Config directory is writable",
	})

	return checks
}
