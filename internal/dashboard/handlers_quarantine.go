package dashboard

// handlers_quarantine.go — index-quarantine transparency surface (Q2, #5617).
//
// The self-healing watcher (#5616) auto-quarantines directories that churn
// pathologically so they stop arming reindexes. These endpoints expose that
// set to the dashboard and let an operator override it:
//
//	GET  /api/groups/{group}/quarantine
//	     → list every quarantined dir across the group's repos
//	POST /api/groups/{group}/repos/{repo}/quarantine/unquarantine  {rel}
//	     → manual un-quarantine
//	POST /api/groups/{group}/repos/{repo}/quarantine/pin           {rel, pinned}
//	     → operator override: pin (never auto-heal) / unpin
//
// Source of truth is each repo's persisted <repo>/.grafel/quarantine.json (the
// store the live tracker reloads + rewrites), reused here via the watch
// package's daemon-less file helpers so the surface stays consistent with a
// running daemon.

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cajasmota/grafel/internal/daemon/watch"
	"github.com/cajasmota/grafel/internal/registry"
)

// QuarantineEntry is one quarantined directory in the list response.
type QuarantineEntry struct {
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	Signal string `json:"signal"`
	Detail string `json:"detail"`
	At     string `json:"at"`
	Pinned bool   `json:"pinned"`
}

// QuarantineListReply is returned by GET /api/groups/{group}/quarantine.
type QuarantineListReply struct {
	Group   string            `json:"group"`
	Entries []QuarantineEntry `json:"entries"`
}

// quarantineActionRequest is the body for the un-quarantine / pin endpoints.
type quarantineActionRequest struct {
	Rel    string `json:"rel"`
	Pinned bool   `json:"pinned"`
}

// QuarantineActionReply is returned by the mutating quarantine endpoints.
type QuarantineActionReply struct {
	Repo    string `json:"repo"`
	Rel     string `json:"rel"`
	Action  string `json:"action"`
	Changed bool   `json:"changed"`
}

// handleQuarantineList — GET /api/groups/{group}/quarantine
func (s *Server) handleQuarantineList(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "missing group slug")
		return
	}
	cfg, err := loadGroupConfigBySlug(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	entries := []QuarantineEntry{}
	for _, repo := range cfg.Repos {
		reasons, rerr := watch.ReadQuarantineFile(repo.Path)
		if rerr != nil {
			continue // unreadable file → treat as empty
		}
		for _, rsn := range reasons {
			entries = append(entries, QuarantineEntry{
				Repo:   repo.Slug,
				Path:   rsn.Rel,
				Signal: rsn.Signal,
				Detail: rsn.Detail,
				At:     rsn.At.UTC().Format("2006-01-02T15:04:05Z07:00"),
				Pinned: rsn.Pinned,
			})
		}
	}
	writeJSON(w, http.StatusOK, QuarantineListReply{Group: group, Entries: entries})
}

// handleQuarantineUnquarantine — POST /api/groups/{group}/repos/{repo}/quarantine/unquarantine
func (s *Server) handleQuarantineUnquarantine(w http.ResponseWriter, r *http.Request) {
	group, repoSlug, req, ok := s.parseQuarantineAction(w, r)
	if !ok {
		return
	}
	repoPath, err := repoPathInGroup(group, repoSlug)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	changed, err := watch.UnquarantineFile(repoPath, req.Rel)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditor.OK("quarantine-remove", group, map[string]any{"repo": repoSlug, "rel": req.Rel})
	writeJSON(w, http.StatusOK, QuarantineActionReply{
		Repo: repoSlug, Rel: req.Rel, Action: "unquarantine", Changed: changed,
	})
}

// handleQuarantinePin — POST /api/groups/{group}/repos/{repo}/quarantine/pin
func (s *Server) handleQuarantinePin(w http.ResponseWriter, r *http.Request) {
	group, repoSlug, req, ok := s.parseQuarantineAction(w, r)
	if !ok {
		return
	}
	repoPath, err := repoPathInGroup(group, repoSlug)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	changed, err := watch.SetPinFile(repoPath, req.Rel, req.Pinned)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	action := "unpin"
	if req.Pinned {
		action = "pin"
	}
	s.auditor.OK("quarantine-"+action, group,
		map[string]any{"repo": repoSlug, "rel": req.Rel, "pinned": req.Pinned})
	writeJSON(w, http.StatusOK, QuarantineActionReply{
		Repo: repoSlug, Rel: req.Rel, Action: action, Changed: changed,
	})
}

// parseQuarantineAction decodes path values + body shared by the mutating
// quarantine endpoints. It writes the error response and returns ok=false on
// any validation failure.
func (s *Server) parseQuarantineAction(w http.ResponseWriter, r *http.Request) (group, repo string, req quarantineActionRequest, ok bool) {
	group = r.PathValue("group")
	repo = r.PathValue("repo")
	if group == "" || repo == "" {
		writeErr(w, http.StatusBadRequest, "missing group or repo slug")
		return "", "", req, false
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return "", "", req, false
	}
	if req.Rel == "" {
		writeErr(w, http.StatusBadRequest, "missing 'rel' (directory) in body")
		return "", "", req, false
	}
	return group, repo, req, true
}

// loadGroupConfigBySlug resolves a group slug to its config via the registry.
func loadGroupConfigBySlug(group string) (*registry.GroupConfig, error) {
	groups, err := registry.Groups()
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		if g.Name == group {
			cfg, cerr := registry.LoadGroupConfig(g.ConfigPath)
			if cerr != nil {
				return nil, cerr
			}
			return cfg, nil
		}
	}
	return nil, fmt.Errorf("group %q not found in registry", group)
}

// repoPathInGroup resolves a repo slug within a group to its absolute path.
func repoPathInGroup(group, slug string) (string, error) {
	cfg, err := loadGroupConfigBySlug(group)
	if err != nil {
		return "", err
	}
	for _, repo := range cfg.Repos {
		if repo.Slug == slug {
			return repo.Path, nil
		}
	}
	return "", fmt.Errorf("repo %q not found in group %q", slug, group)
}
