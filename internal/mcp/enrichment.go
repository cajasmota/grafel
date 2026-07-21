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
//
// ADR-0027 Cutover PR2: this MUTATES entities in place, so it must resolve the
// id against the authoritative Doc.Entities slice (a stable, addressable
// pointer), NOT via LabelIndex.ByID — which now materializes a throwaway heap
// copy per lookup, so a PropSet through it would be lost. Building a one-shot
// id→index over Doc keeps the write-through semantics intact and independent of
// the (now pointer-unstable) index. (This helper currently has no callers; the
// retarget keeps it correct for when one is wired.)
func applyResolutions(repoPath string, lr *LoadedRepo) {
	if lr == nil || lr.Doc == nil {
		return
	}
	res := readResolutions(repoPath)
	if len(res) == 0 {
		return
	}
	pos := make(map[string]int, len(lr.Doc.Entities))
	for i := range lr.Doc.Entities {
		pos[lr.Doc.Entities[i].ID] = i
	}
	for _, r := range res {
		i, ok := pos[r.NodeID]
		if !ok {
			continue
		}
		e := &lr.Doc.Entities[i]
		if e.PropLen() == 0 {
			e.PropsReplace(map[string]string{})
		}
		if r.Kind != "" {
			e.PropSet(r.Kind, r.Value)
		}
	}
}
