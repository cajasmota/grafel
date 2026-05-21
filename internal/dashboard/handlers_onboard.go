package dashboard

// handlers_onboard.go — Web onboarding wizard API (#1239)
//
// Three stateless helper endpoints called by the /onboard SPA route.
// They validate paths and detect monorepo structure without touching
// the registry; the final create-group call does the actual write.
//
// Routes registered in server.go:
//
//	POST /api/onboard/check-path      — validate path + suggest group name
//	POST /api/onboard/detect-monorepo — scan for sub-modules
//	POST /api/onboard/create-group    — create registry entry + kick rebuild

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/archigraph/internal/install/detect"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// OnboardCheckPathReq is the request body for POST /api/onboard/check-path.
type OnboardCheckPathReq struct {
	Path string `json:"path"`
}

// OnboardCheckPathReply is returned by POST /api/onboard/check-path.
type OnboardCheckPathReply struct {
	// Valid reports whether the path exists and is a directory.
	Valid bool `json:"valid"`
	// AbsPath is the resolved absolute path (symlinks expanded).
	AbsPath string `json:"abs_path"`
	// SuggestedGroupName is derived from the directory basename.
	SuggestedGroupName string `json:"suggested_group_name"`
	// SuggestedSlug is the URL-safe repo slug (same as basename, lower-cased).
	SuggestedSlug string `json:"suggested_slug"`
	// Stack is the detected language stack ("go", "node", "python", …).
	Stack string `json:"stack"`
	// IsMonorepo is true when the detect package found a monorepo layout.
	IsMonorepo bool `json:"is_monorepo"`
	// HasAgentsMD is true when an AGENTS.md or CLAUDE.md was found at the root.
	HasAgentsMD bool `json:"has_agents_md"`
	// HasArchigraphConfig is true when .archigraph/group.json exists in the repo.
	HasArchigraphConfig bool `json:"has_archigraph_config"`
	// ExistingGroupName is the group name from .archigraph/group.json (if present).
	ExistingGroupName string `json:"existing_group_name,omitempty"`
	// Error is set when Valid is false with a human-readable reason.
	Error string `json:"error,omitempty"`
}

// OnboardDetectMonorepoReq is the request body for POST /api/onboard/detect-monorepo.
type OnboardDetectMonorepoReq struct {
	Path string `json:"path"`
}

// OnboardDetectMonorepoReply is returned by POST /api/onboard/detect-monorepo.
type OnboardDetectMonorepoReply struct {
	// Kind is the monorepo layout: "pnpm", "npm", "turbo", "nx", "lerna", "multi", or "".
	Kind string `json:"kind"`
	// Packages is the list of relative package sub-paths detected.
	Packages []string `json:"packages"`
}

// OnboardRepoSpec describes one repo in the create-group request.
type OnboardRepoSpec struct {
	// Path is the absolute path on disk.
	Path string `json:"path"`
	// Slug is the registry slug (basename by default).
	Slug string `json:"slug"`
	// Modules is a subset of package sub-paths to index (empty = whole repo).
	Modules []string `json:"modules,omitempty"`
}

// OnboardCreateGroupReq is the request body for POST /api/onboard/create-group.
type OnboardCreateGroupReq struct {
	// GroupName is the registry key for this group.
	GroupName string `json:"group_name"`
	// Repos is the list of repos to register (at least one required).
	Repos []OnboardRepoSpec `json:"repos"`
}

// OnboardCreateGroupReply is returned by POST /api/onboard/create-group.
type OnboardCreateGroupReply struct {
	// Group is the created group name.
	Group string `json:"group"`
	// ReposAdded is the number of repos registered.
	ReposAdded int `json:"repos_added"`
	// ProgressToken can be passed to /api/index-progress/{group} to track indexing.
	ProgressToken string `json:"progress_token"`
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/onboard/check-path
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleOnboardCheckPath(w http.ResponseWriter, r *http.Request) {
	var req OnboardCheckPathReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Path == "" {
		writeErr(w, http.StatusBadRequest, "path required")
		return
	}

	// Resolve absolute path (expand ~ and relative refs).
	abs, err := expandPath(req.Path)
	if err != nil {
		writeJSON(w, http.StatusOK, OnboardCheckPathReply{
			Valid: false,
			Error: fmt.Sprintf("cannot resolve path: %v", err),
		})
		return
	}

	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		msg := "path does not exist or is not a directory"
		if err != nil {
			msg = err.Error()
		}
		writeJSON(w, http.StatusOK, OnboardCheckPathReply{
			Valid:   false,
			AbsPath: abs,
			Error:   msg,
		})
		return
	}

	base := filepath.Base(abs)
	slug := slugify(base)
	stack := detect.Stack(abs)

	mono, _ := detect.DetectMonorepo(abs)
	isMonorepo := mono.Kind != detect.KindNone

	hasAgentsMD := fileExists(abs, "AGENTS.md") || fileExists(abs, "CLAUDE.md") || fileExists(abs, "GEMINI.md")
	hasArchigraphConfig := fileExists(abs, ".archigraph/group.json")

	var existingGroup string
	if hasArchigraphConfig {
		existingGroup = readManifestGroup(filepath.Join(abs, ".archigraph", "group.json"))
	}

	writeJSON(w, http.StatusOK, OnboardCheckPathReply{
		Valid:               true,
		AbsPath:             abs,
		SuggestedGroupName:  base,
		SuggestedSlug:       slug,
		Stack:               stack,
		IsMonorepo:          isMonorepo,
		HasAgentsMD:         hasAgentsMD,
		HasArchigraphConfig: hasArchigraphConfig,
		ExistingGroupName:   existingGroup,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/onboard/detect-monorepo
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleOnboardDetectMonorepo(w http.ResponseWriter, r *http.Request) {
	var req OnboardDetectMonorepoReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Path == "" {
		writeErr(w, http.StatusBadRequest, "path required")
		return
	}

	abs, err := expandPath(req.Path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("cannot resolve path: %v", err))
		return
	}

	mono, _ := detect.DetectMonorepo(abs)
	writeJSON(w, http.StatusOK, OnboardDetectMonorepoReply{
		Kind:     string(mono.Kind),
		Packages: mono.Packages,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/onboard/create-group
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleOnboardCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req OnboardCreateGroupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.GroupName == "" {
		writeErr(w, http.StatusBadRequest, "group_name required")
		return
	}
	if len(req.Repos) == 0 {
		writeErr(w, http.StatusBadRequest, "at least one repo required")
		return
	}

	// 1. Create the group registry entry.
	_, err := s.registry.CreateGroup(req.GroupName)
	if err != nil {
		writeErr(w, http.StatusConflict, fmt.Sprintf("create group: %v", err))
		return
	}

	// 2. Register each repo.
	added := 0
	for _, spec := range req.Repos {
		abs, pathErr := expandPath(spec.Path)
		if pathErr != nil {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("bad path %q: %v", spec.Path, pathErr))
			return
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
		if err := s.registry.AddRepo(req.GroupName, repo); err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("add repo %q: %v", slug, err))
			return
		}
		added++
	}

	// 3. Kick an async rebuild — best-effort; wizard still succeeds if daemon is
	//    not running (the user can start it and indexing will begin on connect).
	token := fmt.Sprintf("onboard-%s", req.GroupName)
	go func() {
		_ = s.tryDispatchRebuildAsync(req.GroupName, token)
	}()

	writeJSON(w, http.StatusCreated, OnboardCreateGroupReply{
		Group:         req.GroupName,
		ReposAdded:    added,
		ProgressToken: token,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// expandPath resolves ~ and returns an absolute path.
func expandPath(p string) (string, error) {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[2:])
	}
	return filepath.Abs(p)
}

// slugify converts a directory basename into a URL-safe lowercase slug.
func slugify(name string) string {
	s := strings.ToLower(name)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else if r == '_' || r == ' ' || r == '.' {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// fileExists returns true when a file (relative to root) exists.
func fileExists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, rel))
	return err == nil
}

// readManifestGroup reads the "group" field from .archigraph/group.json.
func readManifestGroup(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var m struct {
		Group string `json:"group"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return m.Group
}

// tryDispatchRebuildAsync attempts a daemon rebuild RPC without blocking.
// Returns an error only for logging; the wizard caller ignores it.
func (s *Server) tryDispatchRebuildAsync(group, _ string) error {
	groups, err := registry.Groups()
	if err != nil {
		return err
	}
	found := false
	for _, g := range groups {
		if g.Name == group {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("group %q not found after creation", group)
	}
	// Re-use the existing dispatchRebuild path via a no-op ResponseWriter.
	s.dispatchRebuild(&discardResponseWriter{}, group, "", false)
	return nil
}

// discardResponseWriter is an http.ResponseWriter that silently drops output.
// Used when triggering a rebuild from a goroutine after the HTTP response has
// already been sent.
type discardResponseWriter struct{ header http.Header }

func (d *discardResponseWriter) Header() http.Header {
	if d.header == nil {
		d.header = make(http.Header)
	}
	return d.header
}
func (d *discardResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (d *discardResponseWriter) WriteHeader(_ int)           {}
