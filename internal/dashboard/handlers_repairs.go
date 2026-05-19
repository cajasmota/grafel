package dashboard

// handlers_repairs.go — Repair queue (admin) endpoints
//
//	GET /api/repairs/{group}

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/cajasmota/archigraph/internal/daemon"
)

// handleRepairs — GET /api/repairs/{group}
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

	// Collect repair_edge candidates from all repos in the group.
	type ResidualRow struct {
		ResidualID   string  `json:"residual_id"`
		Repo         string  `json:"repo"`
		Kind         string  `json:"kind"`
		Hint         string  `json:"hint,omitempty"`
		Confidence   float64 `json:"confidence,omitempty"`
		AutoResolvable bool  `json:"auto_resolvable"`
	}

	residuals := []ResidualRow{}
	autoResolvable := 0

	for slug, r := range grp.Repos {
		if r == nil || r.Path == "" {
			continue
		}
		cands := readRepairCandidates(r.Path)
		for _, c := range cands {
			ar := c.Confidence >= 0.85
			if ar {
				autoResolvable++
			}
			residuals = append(residuals, ResidualRow{
				ResidualID:     c.ID,
				Repo:           slug,
				Kind:           c.Kind,
				Hint:           c.Hint,
				Confidence:     c.Confidence,
				AutoResolvable: ar,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"residuals":            residuals,
		"open_count":           len(residuals),
		"auto_resolvable_count": autoResolvable,
	})
}

// candidateRaw is a minimal parse of enrichment-candidates.json entries.
type candidateRaw struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	Hint       string  `json:"hint,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

// readRepairCandidates reads the repair_edge entries from a repo's
// enrichment-candidates.json without importing internal/enrichment.
func readRepairCandidates(repoPath string) []candidateRaw {
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
		return filterRepairKind(arr)
	}
	// Try {"candidates": [...]} wrapper.
	var obj struct {
		Candidates []candidateRaw `json:"candidates"`
	}
	if json.Unmarshal(data, &obj) == nil {
		return filterRepairKind(obj.Candidates)
	}
	return nil
}

func filterRepairKind(cands []candidateRaw) []candidateRaw {
	out := cands[:0]
	for _, c := range cands {
		if c.Kind == "repair_edge" {
			out = append(out, c)
		}
	}
	return out
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
		"source":     src,
		"language":   entity.Language,
		"start_line": entity.StartLine,
		"end_line":   entity.EndLine,
		"source_file": entity.SourceFile,
		"repo":       repo.Slug,
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
