// Package enrichment — docgen→graph repair feedback loop (#1659).
//
// When the generate-docs skill reads source code and reasons about the
// codebase it has richer context than the static extractor. This module
// defines the schema, persistence, and apply path for "repair candidates"
// the docgen skill emits back to the graph so Fidelity climbs over
// successive runs.
//
// # Lifecycle
//
//  1. A docgen writer discovers something the static extractor missed
//     (e.g. a reference that resolves by reading the source, a flow that
//     is identical to another, an entity whose kind is wrong).
//  2. The writer appends a DocgenRepairCandidate record to
//     <stateDir>/docgen-repairs.jsonl (append-only, one JSON object per
//     line).
//  3. On the next daemon load (or via archigraph_apply_docgen_repairs),
//     ReadDocgenRepairs loads the file and ApplyDocgenRepairs splits it:
//     - Confidence ≥ HighConfidenceThreshold → applied immediately as
//     graph enrichment enrichment-resolutions.json or a new edge.
//     - Confidence < HighConfidenceThreshold → written to
//     docgen-repairs-pending.json for human review.
//
// # Schema contract
//
// DocgenRepairCandidate is the on-disk record shape emitted by the skill.
// It is also what archigraph_apply_docgen_repairs returns in its response
// so the skill can confirm which repairs landed.
package enrichment

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// HighConfidenceThreshold is the minimum confidence for a repair to be
// applied automatically. Repairs below this threshold are queued as
// pending for human review. This mirrors the docgen grind owner directive.
const HighConfidenceThreshold = 0.8

// Repair candidate types (the "type" field on DocgenRepairCandidate).
const (
	// DocgenRepairResolveRef — the writer can resolve an unresolved stub to
	// a real entity ID or qualified name. Becomes a new edge or ToID rewrite.
	DocgenRepairResolveRef = "resolve_ref"
	// DocgenRepairAddEdge — the writer discovered a dependency that the
	// static extractor missed (e.g. a dynamic-dispatch call it could reason
	// about from source context).
	DocgenRepairAddEdge = "add_edge"
	// DocgenRepairFixKind — the writer believes an entity's Kind is wrong
	// (e.g. classified as Operation but it is actually a Service).
	DocgenRepairFixKind = "fix_kind"
	// DocgenRepairLabelExternal — the writer recognises an unresolved stub as
	// a well-known external library so it can be classified ext:<module>.
	DocgenRepairLabelExternal = "label_external"
	// DocgenRepairMergeFlow — the writer identified two flow entities that
	// represent the same business flow (dedup by enrichment merge).
	DocgenRepairMergeFlow = "merge_flow"
)

// allowedDocgenRepairTypes is the closed set enforced by Validate.
var allowedDocgenRepairTypes = map[string]bool{
	DocgenRepairResolveRef:    true,
	DocgenRepairAddEdge:       true,
	DocgenRepairFixKind:       true,
	DocgenRepairLabelExternal: true,
	DocgenRepairMergeFlow:     true,
}

// DocgenRepairCandidate is one record in docgen-repairs.jsonl.
// Every field with `json:",omitempty"` is optional — only the required
// fields listed in Validate() must be present. The schema is deliberately
// wide so a single type covers all five repair operations.
type DocgenRepairCandidate struct {
	// ID is a deterministic stable identifier derived from
	// (type, source_entity_id, target). Set by NewDocgenRepairCandidate;
	// the writer may omit it and call Validate() which will re-derive it.
	ID string `json:"id,omitempty"`

	// Type is one of the DocgenRepair* constants.
	Type string `json:"type"`

	// SourceEntityID is the local entity ID the repair applies to.
	// Required for all types.
	SourceEntityID string `json:"source_entity_id"`

	// Target is the resolved reference: a hex entity ID, a qualified name
	// like "pkg.Function", an "ext:<module>" string (for label_external),
	// or a merge-target entity ID (for merge_flow). Required for
	// resolve_ref, add_edge, label_external, and merge_flow. Omit for
	// fix_kind.
	Target string `json:"target,omitempty"`

	// EdgeKind is the relationship kind to emit when Type is add_edge
	// (e.g. "CALLS", "IMPORTS"). Optional for add_edge; ignored otherwise.
	EdgeKind string `json:"edge_kind,omitempty"`

	// NewKind is the replacement entity kind for fix_kind. Required when
	// Type == fix_kind.
	NewKind string `json:"new_kind,omitempty"`

	// Confidence is the writer's self-assessed confidence (0–1).
	// Required. Repairs with confidence < HighConfidenceThreshold are
	// queued as pending instead of being applied.
	Confidence float64 `json:"confidence"`

	// Evidence is a short human-readable citation: where in the source the
	// writer read the information. Format: "file.go@line N" or "read source
	// X, saw Y". Required for traceability.
	Evidence string `json:"evidence"`

	// EmittedAt is an RFC 3339 timestamp set by the writer. NewDocgenRepairCandidate
	// sets it automatically; callers may override for testing.
	EmittedAt string `json:"emitted_at,omitempty"`

	// Source identifies the docgen pass that emitted this candidate, e.g.
	// "generate-docs/pass-3a". Optional but useful for attribution.
	Source string `json:"source,omitempty"`
}

// docgenRepairID derives a deterministic stable ID for a (type, source,
// target) triple. Stable across runs for the same inputs so idempotent
// re-emission produces the same ID.
func docgenRepairID(repairType, sourceEntityID, target string) string {
	h := sha256.New()
	h.Write([]byte(repairType))
	h.Write([]byte{0})
	h.Write([]byte(sourceEntityID))
	h.Write([]byte{0})
	h.Write([]byte(target))
	return "dr:" + hex.EncodeToString(h.Sum(nil))[:16]
}

// NewDocgenRepairCandidate constructs and validates a DocgenRepairCandidate,
// deriving its stable ID. Returns an error when required fields are missing.
func NewDocgenRepairCandidate(
	repairType, sourceEntityID, target, edgeKind, newKind string,
	confidence float64,
	evidence, source string,
) (DocgenRepairCandidate, error) {
	c := DocgenRepairCandidate{
		Type:           repairType,
		SourceEntityID: sourceEntityID,
		Target:         target,
		EdgeKind:       edgeKind,
		NewKind:        newKind,
		Confidence:     confidence,
		Evidence:       evidence,
		Source:         source,
		EmittedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	c.ID = docgenRepairID(repairType, sourceEntityID, target)
	if err := c.Validate(); err != nil {
		return DocgenRepairCandidate{}, err
	}
	return c, nil
}

// Validate checks required fields and returns a descriptive error when any
// constraint is violated. Callers that set fields manually should call this
// before appending to the JSONL file.
func (c *DocgenRepairCandidate) Validate() error {
	if !allowedDocgenRepairTypes[c.Type] {
		return fmt.Errorf("docgen repair: unknown type %q", c.Type)
	}
	if strings.TrimSpace(c.SourceEntityID) == "" {
		return fmt.Errorf("docgen repair: source_entity_id is required")
	}
	if c.Confidence < 0 || c.Confidence > 1 {
		return fmt.Errorf("docgen repair: confidence must be in [0, 1], got %.3f", c.Confidence)
	}
	if strings.TrimSpace(c.Evidence) == "" {
		return fmt.Errorf("docgen repair: evidence is required")
	}
	// Type-specific required fields.
	switch c.Type {
	case DocgenRepairResolveRef, DocgenRepairAddEdge, DocgenRepairLabelExternal, DocgenRepairMergeFlow:
		if strings.TrimSpace(c.Target) == "" {
			return fmt.Errorf("docgen repair: target is required for type %q", c.Type)
		}
	case DocgenRepairFixKind:
		if strings.TrimSpace(c.NewKind) == "" {
			return fmt.Errorf("docgen repair: new_kind is required for type fix_kind")
		}
	}
	// Derive ID if missing (allows callers who set fields manually to get
	// Validate() to fill in the ID rather than failing).
	if c.ID == "" {
		c.ID = docgenRepairID(c.Type, c.SourceEntityID, c.Target)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Persistence helpers
// ---------------------------------------------------------------------------

// docgenRepairsPath returns the JSONL file path for a repo state dir.
func docgenRepairsPath(stateDir string) string {
	return filepath.Join(stateDir, "docgen-repairs.jsonl")
}

// docgenRepairsPendingPath returns the pending-review JSON file path.
func docgenRepairsPendingPath(stateDir string) string {
	return filepath.Join(stateDir, "docgen-repairs-pending.json")
}

// AppendDocgenRepair appends one candidate to docgen-repairs.jsonl.
// The file is created if absent. Thread-safety: file-level locking is not
// used — the caller is expected to serialize writes (a single docgen pass
// is always single-writer for a given stateDir).
func AppendDocgenRepair(stateDir string, c DocgenRepairCandidate) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("docgen repair: mkdir %s: %w", stateDir, err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("docgen repair: marshal: %w", err)
	}
	f, err := os.OpenFile(docgenRepairsPath(stateDir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("docgen repair: open jsonl: %w", err)
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s\n", data)
	return err
}

// ReadDocgenRepairs reads all candidates from docgen-repairs.jsonl.
// Missing file returns (nil, nil). Malformed lines are skipped with a
// count returned so the caller can surface warnings.
func ReadDocgenRepairs(stateDir string) (candidates []DocgenRepairCandidate, skipped int, err error) {
	f, openErr := os.Open(docgenRepairsPath(stateDir))
	if openErr != nil {
		if os.IsNotExist(openErr) {
			return nil, 0, nil
		}
		return nil, 0, openErr
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var c DocgenRepairCandidate
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			skipped++
			continue
		}
		candidates = append(candidates, c)
	}
	return candidates, skipped, scanner.Err()
}

// ---------------------------------------------------------------------------
// Apply path
// ---------------------------------------------------------------------------

// DocgenRepairStats is the result of one ApplyDocgenRepairs run.
type DocgenRepairStats struct {
	// Applied is the count of high-confidence repairs that were applied.
	Applied int `json:"applied"`
	// Queued is the count of low-confidence repairs queued for review.
	Queued int `json:"queued"`
	// Skipped is the count of malformed or duplicate records.
	Skipped int `json:"skipped"`
	// FidelityBefore is the estimated fidelity (0–1) before applying.
	// Computed as 1 − (bug_edges / total_edges) using the entity/rel counts
	// in the graph. Nil when the graph is not loaded.
	FidelityBefore *float64 `json:"fidelity_before,omitempty"`
	// FidelityAfter is the estimated fidelity after applying. The delta
	// (FidelityAfter − FidelityBefore) is the measurable lift from this run.
	FidelityAfter *float64 `json:"fidelity_after,omitempty"`
	// RepairsApplied is the full list of applied candidates (for audit).
	RepairsApplied []DocgenRepairCandidate `json:"repairs_applied"`
	// RepairsQueued is the full list of pending candidates.
	RepairsQueued []DocgenRepairCandidate `json:"repairs_queued"`
}

// ApplyDocgenRepairsToResolutions reads docgen-repairs.jsonl from stateDir,
// partitions by confidence, writes high-confidence repairs into
// enrichment-resolutions.json (via append-merge), and writes
// low-confidence repairs into docgen-repairs-pending.json.
//
// totalEdges and bugEdges drive the fidelity before/after calculation.
// Pass (0, 0) when graph counts are unavailable — fidelity fields will be
// omitted from the stats.
//
// The function is idempotent: candidates whose ID is already present in
// enrichment-resolutions.json are not re-applied. The JSONL file is left
// intact after a successful run so the daemon can re-run on reload without
// losing the record of what the docgen pass discovered.
func ApplyDocgenRepairsToResolutions(stateDir string, totalEdges, bugEdges int) (DocgenRepairStats, error) {
	stats := DocgenRepairStats{
		RepairsApplied: []DocgenRepairCandidate{},
		RepairsQueued:  []DocgenRepairCandidate{},
	}

	// Fidelity before.
	if totalEdges > 0 {
		f := 1.0 - float64(bugEdges)/float64(totalEdges)
		stats.FidelityBefore = &f
	}

	candidates, skipped, err := ReadDocgenRepairs(stateDir)
	if err != nil {
		return stats, err
	}
	stats.Skipped = skipped

	if len(candidates) == 0 {
		if totalEdges > 0 {
			f := 1.0 - float64(bugEdges)/float64(totalEdges)
			stats.FidelityAfter = &f
		}
		return stats, nil
	}

	// Load existing resolutions to avoid re-applying.
	existing := loadExistingResolutionIDs(stateDir)

	var highConf, lowConf []DocgenRepairCandidate
	for _, c := range candidates {
		if existing[c.ID] {
			stats.Skipped++
			continue
		}
		if c.Confidence >= HighConfidenceThreshold {
			highConf = append(highConf, c)
		} else {
			lowConf = append(lowConf, c)
		}
	}

	// Write high-confidence as resolutions.
	if len(highConf) > 0 {
		if err := mergeDocgenRepairsIntoResolutions(stateDir, highConf); err != nil {
			return stats, fmt.Errorf("docgen repair apply: %w", err)
		}
		stats.Applied = len(highConf)
		stats.RepairsApplied = highConf
	}

	// Write low-confidence as pending.
	if len(lowConf) > 0 {
		if err := writePendingRepairs(stateDir, lowConf); err != nil {
			return stats, fmt.Errorf("docgen repair pending write: %w", err)
		}
		stats.Queued = len(lowConf)
		stats.RepairsQueued = lowConf
	}

	// Fidelity after: each applied resolve_ref / add_edge / label_external
	// repair reduces the effective bug count by 1 (conservative estimate).
	if totalEdges > 0 {
		repairLift := countEdgeRepairs(highConf)
		adjusted := bugEdges - repairLift
		if adjusted < 0 {
			adjusted = 0
		}
		f := 1.0 - float64(adjusted)/float64(totalEdges)
		stats.FidelityAfter = &f
	}

	return stats, nil
}

// countEdgeRepairs returns the number of high-confidence repairs that
// directly reduce unresolved-edge count (resolve_ref, add_edge, label_external).
func countEdgeRepairs(repairs []DocgenRepairCandidate) int {
	n := 0
	for _, r := range repairs {
		switch r.Type {
		case DocgenRepairResolveRef, DocgenRepairAddEdge, DocgenRepairLabelExternal:
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Resolution file helpers
// ---------------------------------------------------------------------------

// resolutionOnDisk is the shape we write for docgen-sourced resolutions.
// We reuse the existing enrichment.Resolution type but write through this
// intermediate to keep the docgen-→-resolution translation in one place.
type resolutionOnDisk struct {
	ID         string  `json:"id"`
	SubjectID  string  `json:"subject_id"`
	Kind       string  `json:"kind"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence,omitempty"`
	Reason     string  `json:"reason,omitempty"`
	ResolvedAt string  `json:"resolved_at,omitempty"`
	Source     string  `json:"source,omitempty"`
}

// docgenRepairToResolution converts a DocgenRepairCandidate into the
// resolutionOnDisk shape used by enrichment-resolutions.json.
func docgenRepairToResolution(c DocgenRepairCandidate) resolutionOnDisk {
	kind := "docgen_" + c.Type
	value := c.Target
	if c.Type == DocgenRepairFixKind {
		kind = "docgen_fix_kind"
		value = c.NewKind
	}
	return resolutionOnDisk{
		ID:         c.ID,
		SubjectID:  c.SourceEntityID,
		Kind:       kind,
		Value:      value,
		Confidence: c.Confidence,
		Reason:     c.Evidence,
		ResolvedAt: time.Now().UTC().Format(time.RFC3339),
		Source:     c.Source,
	}
}

// loadExistingResolutionIDs reads enrichment-resolutions.json and returns
// the set of IDs already present so we can skip duplicates.
func loadExistingResolutionIDs(stateDir string) map[string]bool {
	path := filepath.Join(stateDir, "enrichment-resolutions.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]bool{}
	}
	// Try array form first.
	var arr []resolutionOnDisk
	if json.Unmarshal(data, &arr) == nil {
		out := make(map[string]bool, len(arr))
		for _, r := range arr {
			if r.ID != "" {
				out[r.ID] = true
			}
		}
		return out
	}
	// Try envelope form.
	var env struct {
		Resolutions []resolutionOnDisk `json:"resolutions"`
	}
	if json.Unmarshal(data, &env) == nil {
		out := make(map[string]bool, len(env.Resolutions))
		for _, r := range env.Resolutions {
			if r.ID != "" {
				out[r.ID] = true
			}
		}
		return out
	}
	return map[string]bool{}
}

// mergeDocgenRepairsIntoResolutions appends docgen-sourced resolutions to
// enrichment-resolutions.json, preserving existing entries. The write is
// atomic via tmp-file + rename.
func mergeDocgenRepairsIntoResolutions(stateDir string, repairs []DocgenRepairCandidate) error {
	path := filepath.Join(stateDir, "enrichment-resolutions.json")

	// Load existing.
	var existing []resolutionOnDisk
	if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &existing) != nil {
			var env struct {
				Resolutions []resolutionOnDisk `json:"resolutions"`
			}
			if json.Unmarshal(data, &env) == nil {
				existing = env.Resolutions
			}
		}
	}

	// Append new.
	for _, c := range repairs {
		existing = append(existing, docgenRepairToResolution(c))
	}

	// Sort for determinism.
	sort.SliceStable(existing, func(i, j int) bool {
		if existing[i].SubjectID != existing[j].SubjectID {
			return existing[i].SubjectID < existing[j].SubjectID
		}
		return existing[i].Kind < existing[j].Kind
	})

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// writePendingRepairs writes low-confidence candidates to
// docgen-repairs-pending.json. The existing file is replaced (not merged) —
// callers are responsible for managing the queue lifecycle.
func writePendingRepairs(stateDir string, repairs []DocgenRepairCandidate) error {
	type pendingEnvelope struct {
		UpdatedAt string                  `json:"updated_at"`
		Pending   []DocgenRepairCandidate `json:"pending"`
	}
	// Merge with existing pending so we don't lose items from prior runs.
	var existing []DocgenRepairCandidate
	existingPath := docgenRepairsPendingPath(stateDir)
	if data, err := os.ReadFile(existingPath); err == nil {
		var env pendingEnvelope
		if json.Unmarshal(data, &env) == nil {
			existing = env.Pending
		}
	}
	// Deduplicate by ID.
	seen := make(map[string]bool, len(existing))
	for _, r := range existing {
		seen[r.ID] = true
	}
	for _, r := range repairs {
		if !seen[r.ID] {
			existing = append(existing, r)
			seen[r.ID] = true
		}
	}

	env := pendingEnvelope{
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Pending:   existing,
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	tmp := existingPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, existingPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ReadPendingDocgenRepairs loads the current pending-review queue.
func ReadPendingDocgenRepairs(stateDir string) ([]DocgenRepairCandidate, error) {
	data, err := os.ReadFile(docgenRepairsPendingPath(stateDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var env struct {
		Pending []DocgenRepairCandidate `json:"pending"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	return env.Pending, nil
}
