package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cajasmota/archigraph/internal/daemon"
)

// LinkCandidate is one row in <group>-link-candidates.json.
type LinkCandidate struct {
	ID         string  `json:"id"`
	Source     string  `json:"source"`
	Target     string  `json:"target"`
	Kind       string  `json:"kind"`
	Channel    string  `json:"channel,omitempty"`
	Method     string  `json:"method,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Reason     string  `json:"reason,omitempty"`
}

// EnrichmentCandidate is one row in <repo>/.archigraph/enrichment-candidates.json.
type EnrichmentCandidate struct {
	ID     string `json:"id"`
	NodeID string `json:"node_id"`
	Kind   string `json:"kind"`
	Hint   string `json:"hint,omitempty"`
}

// readLinkCandidates loads and returns the link candidates for a group.
func readLinkCandidates(group string) []LinkCandidate {
	path := defaultLinkCandidatesFile(group)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var arr []LinkCandidate
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr
	}
	var obj struct {
		Candidates []LinkCandidate `json:"candidates"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}
	return obj.Candidates
}

// writeLinkCandidates persists the candidate list back to disk (array form).
func writeLinkCandidates(group string, cs []LinkCandidate) error {
	path := defaultLinkCandidatesFile(group)
	if path == "" {
		return fmt.Errorf("no candidates path for group %q", group)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// readEnrichmentCandidates loads enrichment candidates for a repo path.
func readEnrichmentCandidates(repoPath string) []EnrichmentCandidate {
	if repoPath == "" {
		return nil
	}
	path := filepath.Join(daemon.StateDirForRepo(repoPath), "enrichment-candidates.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var arr []EnrichmentCandidate
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr
	}
	var obj struct {
		Candidates []EnrichmentCandidate `json:"candidates"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}
	return obj.Candidates
}

// writeEnrichmentCandidates persists candidates to a repo path (array form).
func writeEnrichmentCandidates(repoPath string, cs []EnrichmentCandidate) error {
	path := filepath.Join(daemon.StateDirForRepo(repoPath), "enrichment-candidates.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// appendLink appends a link to the group's links file (array form), creating
// the file if needed.
func appendLink(group string, link CrossRepoLink) error {
	path := defaultLinksFile(group)
	if path == "" {
		return fmt.Errorf("no links path for group %q", group)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	links, _ := readLinks(path)
	links = append(links, link)
	data, err := json.MarshalIndent(links, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// appendResolution appends an enrichment resolution to the repo's
// enrichment-resolutions.json (array form).
func appendResolution(repoPath string, res EnrichmentResolution) error {
	if repoPath == "" {
		return fmt.Errorf("repo path is empty")
	}
	path := filepath.Join(daemon.StateDirForRepo(repoPath), "enrichment-resolutions.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	cur := readResolutions(repoPath)
	cur = append(cur, res)
	data, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// appendRejection appends a rejection record to enrichment-rejections.json.
func appendRejection(repoPath string, candidateID, reason string) error {
	if repoPath == "" {
		return fmt.Errorf("repo path is empty")
	}
	path := filepath.Join(daemon.StateDirForRepo(repoPath), "enrichment-rejections.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	type rej struct {
		CandidateID string `json:"candidate_id"`
		Reason      string `json:"reason"`
	}
	var cur []rej
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &cur)
	}
	cur = append(cur, rej{CandidateID: candidateID, Reason: reason})
	data, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
