package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/cajasmota/archigraph/internal/registry"
)

// handleListRegistry — GET /api/registry. Returns every registered group
// plus its repo slugs. Empty-but-present registry returns [].
func (s *Server) handleListRegistry(w http.ResponseWriter, _ *http.Request) {
	groups, err := s.registry.ListGroups()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if groups == nil {
		groups = []GroupSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

// handleGroupGraph — GET /api/groups/{group}/graph. Returns an envelope
// containing each repo's graph.json. The envelope is JSON-shaped so the
// SPA can stream-decode and so we can later attach communities/links
// without breaking clients.
func (s *Server) handleGroupGraph(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}
	body, err := s.registry.GroupGraph(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// handleRepoGraph — GET /api/groups/{group}/repos/{repo}/graph. Streams
// the repo's graph.json verbatim.
func (s *Server) handleRepoGraph(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	repo := r.PathValue("repo")
	if group == "" || repo == "" {
		writeErr(w, http.StatusBadRequest, "group and repo required")
		return
	}
	body, err := s.registry.RepoGraph(group, repo)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// createGroupReq is the request body for POST /api/admin/groups.
type createGroupReq struct {
	Name string `json:"name"`
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req createGroupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	out, err := s.registry.CreateGroup(req.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// addRepoReq is the request body for POST /api/admin/groups/{group}/repos.
// Mirrors registry.Repo so the SPA can post the same shape it reads back.
type addRepoReq struct {
	Slug     string   `json:"slug"`
	Path     string   `json:"path"`
	Stack    string   `json:"stack,omitempty"`
	CloneURL string   `json:"clone_url,omitempty"`
	Modules  []string `json:"modules,omitempty"`
}

func (s *Server) handleAddRepo(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}
	var req addRepoReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	repo := registry.Repo{
		Slug:     req.Slug,
		Path:     req.Path,
		Stack:    req.Stack,
		CloneURL: req.CloneURL,
		Modules:  req.Modules,
	}
	if err := s.registry.AddRepo(group, repo); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"group": group, "repo": repo})
}
