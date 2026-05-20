package dashboard

// handlers_repairs.go — Pending queue endpoints for the dashboard (#987).
//
//	GET /api/repairs/{group}      — repair_edge + dynamic_baseurl_endpoint candidates
//	GET /api/enrichments/{group}  — all other enrichment candidates (describe_entity, classify_domain, …)

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/cajasmota/archigraph/internal/daemon"
)

// repairKinds is the closed set of candidate kinds surfaced on the "Repair
// candidates" tab. Everything else lands on the "Enrichment candidates" tab.
var repairKinds = map[string]bool{
	"repair_edge":                true,
	"dynamic_baseurl_endpoint":   true,
}

// pendingCandidateRow is the wire shape shared by both /api/repairs and
// /api/enrichments. The richer Context map is forwarded as-is so the
// dashboard can display subject / proposed-value without a second round-trip.
type pendingCandidateRow struct {
	CandidateID    string         `json:"candidate_id"`
	Repo           string         `json:"repo"`
	Kind           string         `json:"kind"`
	SubjectID      string         `json:"subject_id"`
	Context        map[string]any `json:"context,omitempty"`
	Hint           string         `json:"hint,omitempty"`
	Confidence     float64        `json:"confidence,omitempty"`
	DiscoveredAt   string         `json:"discovered_at,omitempty"`
	AutoResolvable bool           `json:"auto_resolvable"`
}

// handleRepairs — GET /api/repairs/{group}
//
// Returns repair_edge and dynamic_baseurl_endpoint candidates for every repo in
// the group. These are the structurally ambiguous edges that require an agent
// (or a human) to choose a resolution.
func (s *Server) handleRepairs(w http.ResponseWriter, r *http.Request) {
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

	items := []pendingCandidateRow{}
	autoResolvable := 0

	for slug, repo := range grp.Repos {
		if repo == nil || repo.Path == "" {
			continue
		}
		for _, c := range readAllCandidates(repo.Path) {
			if !repairKinds[c.Kind] {
				continue
			}
			ar := c.Confidence >= 0.85
			if ar {
				autoResolvable++
			}
			items = append(items, pendingCandidateRow{
				CandidateID:    c.ID,
				Repo:           slug,
				Kind:           c.Kind,
				SubjectID:      c.SubjectID,
				Context:        c.Context,
				Hint:           c.Hint,
				Confidence:     c.Confidence,
				DiscoveredAt:   c.DiscoveredAt,
				AutoResolvable: ar,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":                 items,
		"total":                 len(items),
		"open_count":            len(items), // alias for total (tests + clients rely on both)
		"auto_resolvable_count": autoResolvable,
	})
}

// handleEnrichments — GET /api/enrichments/{group}
//
// Returns all non-repair enrichment candidates (describe_entity, classify_domain,
// describe_role, name_community, infer_xlang_call, summarize_api, …) for every
// repo in the group.
func (s *Server) handleEnrichments(w http.ResponseWriter, r *http.Request) {
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

	items := []pendingCandidateRow{}

	for slug, repo := range grp.Repos {
		if repo == nil || repo.Path == "" {
			continue
		}
		for _, c := range readAllCandidates(repo.Path) {
			if repairKinds[c.Kind] {
				continue // repair tab handles these
			}
			items = append(items, pendingCandidateRow{
				CandidateID:  c.ID,
				Repo:         slug,
				Kind:         c.Kind,
				SubjectID:    c.SubjectID,
				Context:      c.Context,
				Hint:         c.Hint,
				Confidence:   c.Confidence,
				DiscoveredAt: c.DiscoveredAt,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// candidateRaw is the full on-disk shape of one enrichment-candidates.json entry.
// We parse the Context map so the REST layer can forward it without importing
// internal/enrichment.
type candidateRaw struct {
	ID           string         `json:"id"`
	Kind         string         `json:"kind"`
	SubjectID    string         `json:"subject_id"`
	Context      map[string]any `json:"context,omitempty"`
	Hint         string         `json:"hint,omitempty"`
	Confidence   float64        `json:"confidence,omitempty"`
	DiscoveredAt string         `json:"discovered_at,omitempty"`
}

// readAllCandidates reads every entry from a repo's enrichment-candidates.json.
// Returns nil (not an error) when the file is absent.
func readAllCandidates(repoPath string) []candidateRaw {
	if repoPath == "" {
		return nil
	}
	path := filepath.Join(daemon.StateDirForRepo(repoPath), "enrichment-candidates.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	// Try flat array first.
	var arr []candidateRaw
	if json.Unmarshal(data, &arr) == nil {
		return arr
	}
	// Try {"candidates": [...]} wrapper (v2 schema).
	var obj struct {
		Candidates []candidateRaw `json:"candidates"`
	}
	if json.Unmarshal(data, &obj) == nil {
		return obj.Candidates
	}
	return nil
}

// handleListFindings — GET /api/findings
func (s *Server) handleListFindings(w http.ResponseWriter, r *http.Request) {
	group := r.URL.Query().Get("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	_ = grp

	// Read findings from the memory dir.
	memDir := groupMemoryDir(group)
	findings := readFindingFiles(memDir)

	writeJSON(w, http.StatusOK, map[string]any{
		"findings": findings,
	})
}

// groupMemoryDir returns the memory directory for a group.
func groupMemoryDir(group string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".archigraph", "groups", group+"-memory")
}

// readFindingFiles reads all *.json finding files from a directory.
func readFindingFiles(dir string) []map[string]any {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []map[string]any{}
	}
	var out []map[string]any
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var f map[string]any
		if json.Unmarshal(data, &f) == nil {
			out = append(out, f)
		}
	}
	if out == nil {
		return []map[string]any{}
	}
	return out
}

// handleSource — GET /api/source?node_id=&group=&context_lines=
func (s *Server) handleSource(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	nodeID := q.Get("node_id")
	group := q.Get("group")
	if nodeID == "" || group == "" {
		writeErr(w, http.StatusBadRequest, "node_id and group required")
		return
	}
	contextLines := 20
	if v := q.Get("context_lines"); v != "" {
		if n, err := parseInt(v); err == nil && n >= 0 {
			contextLines = n
		}
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	repo, entity := findEntity(grp, nodeID)
	if entity == nil {
		writeErr(w, http.StatusNotFound, "entity not found: "+nodeID)
		return
	}

	src, err := readSourceLines(entity.SourceFile, repo.Path, entity.StartLine, entity.EndLine, contextLines)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"source":      src,
		"language":    entity.Language,
		"start_line":  entity.StartLine,
		"end_line":    entity.EndLine,
		"source_file": entity.SourceFile,
		"repo":        repo.Slug,
	})
}

// parseInt is a small helper.
func parseInt(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, os.ErrInvalid
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
