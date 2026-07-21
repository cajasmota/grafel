package mcp

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
