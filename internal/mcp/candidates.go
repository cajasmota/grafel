package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/enrichment"
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
		Links []LinkCandidate `json:"links"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}
	return obj.Links
}

// writeLinkCandidates persists the candidate list back to disk with proper Document format.
func writeLinkCandidates(group string, cs []LinkCandidate) error {
	path := defaultLinkCandidatesFile(group)
	if path == "" {
		return fmt.Errorf("no candidates path for group %q", group)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Wrap in Document structure to match the format used by the links package
	doc := struct {
		Version int             `json:"version"`
		Links   []LinkCandidate `json:"links"`
	}{
		Version: 1,
		Links:   cs,
	}
	data, err := json.MarshalIndent(doc, "", "  ")
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
// enrichment-resolutions.json. Delegates to the canonical implementation
// in internal/enrichment to avoid parallel implementations (#1016).
func appendResolution(repoPath string, res EnrichmentResolution) error {
	if repoPath == "" {
		return fmt.Errorf("repo path is empty")
	}
	archigraphDir := daemon.StateDirForRepo(repoPath)
	return enrichment.AppendResolution(archigraphDir, enrichment.Resolution{
		ID:         res.CandidateID,
		SubjectID:  res.NodeID,
		Kind:       res.Kind,
		Value:      res.Value,
		Confidence: res.Confidence,
		Reason:     res.Reason,
	})
}

// appendRejection appends a rejection record to enrichment-rejections.json.
// Delegates to the canonical implementation in internal/enrichment (#1016).
func appendRejection(repoPath string, candidateID, reason string) error {
	if repoPath == "" {
		return fmt.Errorf("repo path is empty")
	}
	archigraphDir := daemon.StateDirForRepo(repoPath)
	return enrichment.AppendRejection(archigraphDir, candidateID, "", "", reason)
}
