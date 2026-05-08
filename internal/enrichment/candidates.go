// Package enrichment defines the agent-driven enrichment pipeline. The
// indexer emits subjective enrichment "candidates" — entries that an
// external agent (via the MCP server) is expected to resolve. Resolutions
// are merged back onto entities on subsequent index runs so previously
// agent-resolved values are preserved.
//
// The split between deterministic enrichment (cyclomatic complexity,
// deprecation markers, test classification, ...) and subjective
// enrichment (one-line description, domain classification, role
// description) is described in issue #15. Deterministic enrichers stay
// inline in internal/enrichers/. Subjective enrichers live here as
// CandidateEmitters that emit Candidate records.
package enrichment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

// Candidate kinds. These are the canonical "kind" string values the agent
// branches on when deciding which prompt to apply.
const (
	KindDescribeEntity = "describe_entity"
	KindClassifyDomain = "classify_domain"
	KindInferXLangCall = "infer_xlang_call"
	KindSummarizeAPI   = "summarize_api"
	KindFlagDeadCode   = "flag_dead_code"
	KindDescribeRole   = "describe_role"
)

// CandidatesSchemaVersion is the integer version of the on-disk
// enrichment-candidates.json schema. Bump on a breaking change.
const CandidatesSchemaVersion = 1

// Candidate is one row in <repo>/.archigraph/enrichment-candidates.json.
// Subject_id is always the local entity id (NOT prefixed with repo).
type Candidate struct {
	ID              string         `json:"id"`
	Kind            string         `json:"kind"`
	SubjectID       string         `json:"subject_id"`
	Context         map[string]any `json:"context,omitempty"`
	PromptTemplate  string         `json:"prompt_template,omitempty"`
	ConfidenceFloor float64        `json:"confidence_floor,omitempty"`
	DiscoveredAt    string         `json:"discovered_at,omitempty"`
}

// Resolution is one row in <repo>/.archigraph/enrichment-resolutions.json.
// When index runs it loads resolutions and writes Value into the matching
// entity's Properties (under the resolution's Kind key) before final emit.
type Resolution struct {
	ID         string  `json:"id"`
	SubjectID  string  `json:"subject_id"`
	Kind       string  `json:"kind"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence,omitempty"`
	Reason     string  `json:"reason,omitempty"`
	ResolvedAt string  `json:"resolved_at,omitempty"`
}

// Rejection is one row in <repo>/.archigraph/enrichment-rejections.json.
// Rejected (subject_id, kind) pairs are skipped on subsequent index runs.
type Rejection struct {
	ID         string `json:"id"`
	SubjectID  string `json:"subject_id"`
	Kind       string `json:"kind"`
	Reason     string `json:"reason,omitempty"`
	RejectedAt string `json:"rejected_at,omitempty"`
}

// CandidateEmitter is implemented by every subjective enrichment source.
// Name returns a stable identifier (used in logs/metrics). EmitFor returns
// zero-or-more candidates for the given entity. The Document is supplied
// so emitters can inspect post-Pass-4 graph signals (community membership,
// centrality, articulation points, etc.).
type CandidateEmitter interface {
	Name() string
	EmitFor(entity *graph.Entity, doc *graph.Document) []Candidate
}

// candidateID is the deterministic ID of a candidate. Stable across runs
// for the same (subject_id, kind) tuple — this is what keeps the emit
// pipeline idempotent.
func candidateID(subjectID, kind string) string {
	h := sha256.New()
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write([]byte(subjectID))
	return "ec:" + hex.EncodeToString(h.Sum(nil))[:16]
}

// nowRFC3339 is a tiny indirection so tests can pin the timestamp via
// the DiscoveredAt field comparator without freezing time globally.
var nowRFC3339 = func() string { return time.Now().UTC().Format(time.RFC3339) }

// ---------------------------------------------------------------------------
// Built-in emitters
// ---------------------------------------------------------------------------

// describeEntityEmitter emits a candidate for any entity that has neither
// a Description-equivalent property already nor an explicit signature, so
// the agent can write a single-sentence description.
type describeEntityEmitter struct{}

func (describeEntityEmitter) Name() string { return KindDescribeEntity }

func (describeEntityEmitter) EmitFor(e *graph.Entity, _ *graph.Document) []Candidate {
	if e == nil || e.Name == "" {
		return nil
	}
	if v, ok := e.Properties["description"]; ok && v != "" {
		return nil
	}
	return []Candidate{{
		ID:        candidateID(e.ID, KindDescribeEntity),
		Kind:      KindDescribeEntity,
		SubjectID: e.ID,
		Context: map[string]any{
			"name":        e.Name,
			"kind":        e.Kind,
			"language":    e.Language,
			"source_file": e.SourceFile,
			"signature":   e.Signature,
		},
		PromptTemplate:  "Describe the {{kind}} {{name}} in one sentence.",
		ConfidenceFloor: 0.6,
		DiscoveredAt:    nowRFC3339(),
	}}
}

// classifyDomainEmitter emits a candidate for entities that look
// architecturally significant — high pagerank or god-node flag — so the
// agent assigns a domain (auth, billing, search, ...).
type classifyDomainEmitter struct{}

func (classifyDomainEmitter) Name() string { return KindClassifyDomain }

func (classifyDomainEmitter) EmitFor(e *graph.Entity, _ *graph.Document) []Candidate {
	if e == nil || e.Name == "" {
		return nil
	}
	if v, ok := e.Properties["domain"]; ok && v != "" {
		return nil
	}
	high := e.IsGodNode
	if e.PageRank != nil && *e.PageRank >= 0.01 {
		high = true
	}
	if !high {
		return nil
	}
	return []Candidate{{
		ID:        candidateID(e.ID, KindClassifyDomain),
		Kind:      KindClassifyDomain,
		SubjectID: e.ID,
		Context: map[string]any{
			"name":        e.Name,
			"kind":        e.Kind,
			"is_god_node": e.IsGodNode,
		},
		PromptTemplate:  "Classify the business domain of {{name}}.",
		ConfidenceFloor: 0.5,
		DiscoveredAt:    nowRFC3339(),
	}}
}

// describeRoleEmitter emits a candidate for clearly architectural nodes
// (god nodes, articulation points) so the agent assigns a role label
// (controller / adapter / policy / orchestrator / ...).
type describeRoleEmitter struct{}

func (describeRoleEmitter) Name() string { return KindDescribeRole }

func (describeRoleEmitter) EmitFor(e *graph.Entity, _ *graph.Document) []Candidate {
	if e == nil || e.Name == "" {
		return nil
	}
	if v, ok := e.Properties["architectural_role"]; ok && v != "" {
		return nil
	}
	if !e.IsGodNode && !e.IsArticulationPt {
		return nil
	}
	return []Candidate{{
		ID:        candidateID(e.ID, KindDescribeRole),
		Kind:      KindDescribeRole,
		SubjectID: e.ID,
		Context: map[string]any{
			"name":                  e.Name,
			"kind":                  e.Kind,
			"is_god_node":           e.IsGodNode,
			"is_articulation_point": e.IsArticulationPt,
		},
		PromptTemplate:  "Describe the architectural role of {{name}} (controller/adapter/policy/orchestrator/...).",
		ConfidenceFloor: 0.5,
		DiscoveredAt:    nowRFC3339(),
	}}
}

// DefaultEmitters returns the built-in emitter set. Callers are free to
// extend this slice; CollectCandidates accepts any []CandidateEmitter.
func DefaultEmitters() []CandidateEmitter {
	return []CandidateEmitter{
		describeEntityEmitter{},
		classifyDomainEmitter{},
		describeRoleEmitter{},
	}
}

// ---------------------------------------------------------------------------
// Collect / merge / persist
// ---------------------------------------------------------------------------

// CollectCandidates runs every emitter against every entity in doc and
// returns the merged candidate set. Rejected (subject_id, kind) pairs are
// dropped. Output is sorted by (subject_id, kind) for stable diffs.
func CollectCandidates(doc *graph.Document, emitters []CandidateEmitter, rejected map[string]bool) []Candidate {
	if doc == nil {
		return nil
	}
	var out []Candidate
	seen := map[string]bool{}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		for _, em := range emitters {
			for _, c := range em.EmitFor(e, doc) {
				if c.ID == "" || seen[c.ID] {
					continue
				}
				key := c.SubjectID + "|" + c.Kind
				if rejected[key] {
					continue
				}
				seen[c.ID] = true
				out = append(out, c)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SubjectID != out[j].SubjectID {
			return out[i].SubjectID < out[j].SubjectID
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// candidatesPath returns the on-disk path for enrichment-candidates.json.
func candidatesPath(repoArchigraphDir string) string {
	return filepath.Join(repoArchigraphDir, "enrichment-candidates.json")
}

// resolutionsPath returns the on-disk path for enrichment-resolutions.json.
func resolutionsPath(repoArchigraphDir string) string {
	return filepath.Join(repoArchigraphDir, "enrichment-resolutions.json")
}

// rejectionsPath returns the on-disk path for enrichment-rejections.json.
func rejectionsPath(repoArchigraphDir string) string {
	return filepath.Join(repoArchigraphDir, "enrichment-rejections.json")
}

// candidatesEnvelope is the on-disk array form. We accept a bare array
// for forward-compatibility with the MCP candidate readers in
// internal/mcp/candidates.go (which already accept either form).
type candidatesEnvelope struct {
	Version    int         `json:"version"`
	Candidates []Candidate `json:"candidates"`
}

// WriteCandidates persists candidates atomically. The output is sorted +
// versioned so subsequent runs produce byte-identical files when nothing
// has changed.
func WriteCandidates(archigraphDir string, cs []Candidate) error {
	if err := os.MkdirAll(archigraphDir, 0o755); err != nil {
		return fmt.Errorf("enrichment: mkdir %s: %w", archigraphDir, err)
	}
	path := candidatesPath(archigraphDir)
	tmp := path + ".tmp"
	env := candidatesEnvelope{Version: CandidatesSchemaVersion, Candidates: cs}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("enrichment: marshal candidates: %w", err)
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("enrichment: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("enrichment: rename: %w", err)
	}
	return nil
}

// ReadResolutions reads enrichment-resolutions.json. Tolerates both the
// bare array and the {"resolutions": [...]} envelope. Returns nil on
// missing file.
func ReadResolutions(archigraphDir string) []Resolution {
	data, err := os.ReadFile(resolutionsPath(archigraphDir))
	if err != nil {
		return nil
	}
	var arr []Resolution
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr
	}
	var env struct {
		Resolutions []Resolution `json:"resolutions"`
	}
	if err := json.Unmarshal(data, &env); err == nil {
		return env.Resolutions
	}
	// Tolerate the MCP server's existing on-disk shape (CandidateID/NodeID).
	var legacy []struct {
		CandidateID string  `json:"candidate_id"`
		NodeID      string  `json:"node_id"`
		Kind        string  `json:"kind"`
		Value       string  `json:"value"`
		Confidence  float64 `json:"confidence,omitempty"`
		Reason      string  `json:"reason,omitempty"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil
	}
	out := make([]Resolution, 0, len(legacy))
	for _, l := range legacy {
		out = append(out, Resolution{
			ID:         l.CandidateID,
			SubjectID:  l.NodeID,
			Kind:       l.Kind,
			Value:      l.Value,
			Confidence: l.Confidence,
			Reason:     l.Reason,
		})
	}
	return out
}

// ReadRejections reads enrichment-rejections.json. Returns a set keyed by
// "<subject_id>|<kind>" for fast skip lookups during emission. Tolerates
// the MCP server's legacy {candidate_id, reason} shape: when we see one
// of those we record it by candidate_id (no subject/kind known), but we
// also register all on-disk Rejection records by their proper key.
func ReadRejections(archigraphDir string) map[string]bool {
	data, err := os.ReadFile(rejectionsPath(archigraphDir))
	if err != nil {
		return map[string]bool{}
	}
	out := map[string]bool{}
	var rejs []Rejection
	if err := json.Unmarshal(data, &rejs); err == nil {
		for _, r := range rejs {
			if r.SubjectID != "" && r.Kind != "" {
				out[r.SubjectID+"|"+r.Kind] = true
			}
			if r.ID != "" {
				out[r.ID] = true
			}
		}
		return out
	}
	// Legacy shape: array of {candidate_id, reason}. Register by the
	// candidate id only — the indexer also registers candidate-id keys
	// in CollectCandidates' `seen` set semantics, so a re-run of the
	// emitter will produce the same id and we can skip it.
	var legacy []struct {
		CandidateID string `json:"candidate_id"`
	}
	if err := json.Unmarshal(data, &legacy); err == nil {
		for _, l := range legacy {
			if l.CandidateID != "" {
				out[l.CandidateID] = true
			}
		}
	}
	return out
}

// CollectCandidatesSkippingRejected is a convenience wrapper that loads
// the rejections file and filters them out in one call.
func CollectCandidatesSkippingRejected(doc *graph.Document, emitters []CandidateEmitter, archigraphDir string) []Candidate {
	rej := ReadRejections(archigraphDir)
	cands := CollectCandidates(doc, emitters, rej)
	if len(rej) == 0 {
		return cands
	}
	// Also drop candidates whose ID was rejected directly (legacy shape).
	out := cands[:0]
	for _, c := range cands {
		if rej[c.ID] {
			continue
		}
		out = append(out, c)
	}
	return out
}

// ApplyResolutions merges resolved enrichment values into the document
// in-place. The resolution.Value is written to entity.Properties under
// the resolution.Kind key, mirroring internal/mcp/enrichment.go.
func ApplyResolutions(doc *graph.Document, resolutions []Resolution) int {
	if doc == nil {
		return 0
	}
	byID := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}
	applied := 0
	for _, r := range resolutions {
		if r.SubjectID == "" || r.Kind == "" {
			continue
		}
		e, ok := byID[r.SubjectID]
		if !ok {
			continue
		}
		if e.Properties == nil {
			e.Properties = map[string]string{}
		}
		e.Properties[r.Kind] = r.Value
		applied++
	}
	return applied
}
