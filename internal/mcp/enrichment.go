package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/cajasmota/grafel/internal/daemon"
)

// EnrichmentResolution is one resolved enrichment entry stored under a repo's
// .grafel/enrichment-resolutions.json. Keys here align with the candidate
// pipeline output.
type EnrichmentResolution struct {
	CandidateID string  `json:"candidate_id"`
	NodeID      string  `json:"node_id"`
	Kind        string  `json:"kind"`
	Value       string  `json:"value"`
	Confidence  float64 `json:"confidence,omitempty"`
	Reason      string  `json:"reason,omitempty"`
}

// readResolutions reads enrichment-resolutions.json (array form) for a repo.
func readResolutions(repoPath string) []EnrichmentResolution {
	if repoPath == "" {
		return nil
	}
	path := filepath.Join(daemon.StateDirForRepo(repoPath), "enrichment-resolutions.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []EnrichmentResolution
	if err := json.Unmarshal(data, &out); err != nil {
		// tolerate {"resolutions":[...]} form too
		var alt struct {
			Resolutions []EnrichmentResolution `json:"resolutions"`
		}
		if err := json.Unmarshal(data, &alt); err != nil {
			return nil
		}
		return alt.Resolutions
	}
	return out
}

// applyResolutions merges resolutions into entity Properties (non-destructive,
// resolution-side wins). Used by tools that need post-enrichment views.
func applyResolutions(repoPath string, lr *LoadedRepo) {
	if lr == nil || lr.Doc == nil {
		return
	}
	for _, r := range readResolutions(repoPath) {
		e, ok := lr.LabelIndex.ByID[r.NodeID]
		if !ok {
			continue
		}
		if e.PropLen() == 0 {
			e.PropsReplace(map[string]string{})
		}
		if r.Kind != "" {
			e.PropSet(r.Kind, r.Value)
		}
	}
}
