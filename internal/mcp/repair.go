package mcp

// Repair-tool plumbing for ADR-0015 phase-1 (issues #549 + #550).
//
// list_residuals reads enrichment-candidates.json and surfaces the
// repair_edge entries (kind == "repair_edge") emitted by the indexer
// (internal/enrichment/repair_edge.go). submit_repair appends a Repair
// record to <repo>/.archigraph/repair.json, validating the resolution
// against the ADR-0015 allowlist and writing atomically via tempfile +
// rename so a crashed agent can never leave a half-written file.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/enrichment"
)

// repairSchemaVersion matches docs/specs/repair-v1.schema.json.
const repairSchemaVersion = 1

// repairFileOnDisk is the top-level shape of repair.json. Mirrors
// internal/enrichment.repairFile but redeclared here so we don't depend on
// an unexported symbol.
type repairFileOnDisk struct {
	SchemaVersion int                 `json:"schema_version"`
	GeneratedAt   string              `json:"generated_at,omitempty"`
	Repairs       []enrichment.Repair `json:"repairs"`
}

// allowedRepairResolutions is the closed set enforced by submit_repair.
// Keep in sync with internal/enrichment.allowedResolutionKinds.
var allowedRepairResolutions = map[string]bool{
	enrichment.RepairBindToEntity:         true,
	enrichment.RepairReclassifyAsExternal: true,
	enrichment.RepairReclassifyAsDynamic:  true,
	enrichment.RepairReclassifyAsResolved: true,
	enrichment.RepairAbandon:              true,
}

// readRepairEdgeCandidates returns the repair_edge AND dynamic_baseurl_endpoint
// entries from a repo's enrichment-candidates.json. Both kinds surface in
// archigraph_repairs action=list — repair_edge for structurally ambiguous
// symbol edges (ADR-0015 phase-1) and dynamic_baseurl_endpoint for
// consumer-side HTTP calls whose baseURL is runtime-determined (#708).
//
// We re-parse the file as []enrichment.Candidate (the rich shape with
// Context) because the simplified MCP EnrichmentCandidate struct drops the
// Context map the repair tools depend on.
func readRepairEdgeCandidates(repoPath string) []enrichment.Candidate {
	if repoPath == "" {
		return nil
	}
	path := filepath.Join(daemon.StateDirForRepo(repoPath), "enrichment-candidates.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var arr []enrichment.Candidate
	if err := json.Unmarshal(data, &arr); err != nil {
		// File may use the {"candidates": [...]} wrapper form. Try that
		// before giving up.
		var obj struct {
			Candidates []enrichment.Candidate `json:"candidates"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil {
			return nil
		}
		arr = obj.Candidates
	}
	out := make([]enrichment.Candidate, 0, len(arr))
	for _, c := range arr {
		if c.Kind == enrichment.KindRepairEdge ||
			c.Kind == enrichment.KindDynamicBaseURLEndpoint {
			out = append(out, c)
		}
	}
	return out
}

// readRepairFile reads repair.json. Absent file → empty struct, not error.
func readRepairFile(repoPath string) (repairFileOnDisk, error) {
	path := filepath.Join(daemon.StateDirForRepo(repoPath), "repair.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return repairFileOnDisk{SchemaVersion: repairSchemaVersion, Repairs: []enrichment.Repair{}}, nil
		}
		return repairFileOnDisk{}, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return repairFileOnDisk{SchemaVersion: repairSchemaVersion, Repairs: []enrichment.Repair{}}, nil
	}
	var rf repairFileOnDisk
	if err := json.Unmarshal(data, &rf); err != nil {
		return repairFileOnDisk{}, fmt.Errorf("parse repair.json: %w", err)
	}
	if rf.SchemaVersion == 0 {
		rf.SchemaVersion = repairSchemaVersion
	}
	if rf.Repairs == nil {
		rf.Repairs = []enrichment.Repair{}
	}
	return rf, nil
}

// writeRepairFile writes repair.json atomically: marshal → write to
// <path>.tmp → fsync → rename. A crashed writer never leaves a
// half-written repair.json behind.
func writeRepairFile(repoPath string, rf repairFileOnDisk) error {
	if repoPath == "" {
		return fmt.Errorf("repo path is empty")
	}
	dir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	rf.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return err
	}
	finalPath := filepath.Join(dir, "repair.json")
	tmp, err := os.CreateTemp(dir, "repair.json.tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		// Best-effort cleanup on error path.
		_ = os.Remove(tmpName)
	}()
	if _, err := io.WriteString(tmp, string(data)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, finalPath)
}

// summarizeRepairEdge produces a compact, agent-friendly row for the
// list_residuals output. The full Context is also included so an agent
// pipeline can build a prompt without a second round-trip.
//
// The function handles both repair_edge candidates (ADR-0015 phase-1) and
// dynamic_baseurl_endpoint candidates (#708). Dynamic-baseurl entries carry
// category="cross-repo runtime" in their context; the summary promotes that
// to a top-level field so callers can filter without inspecting the full
// context object.
func summarizeRepairEdge(repo string, c enrichment.Candidate) map[string]any {
	row := map[string]any{
		"candidate_id": c.ID,
		"kind":         c.Kind,
		"subject_id":   c.SubjectID,
		"repo":         repo,
	}
	if c.Context == nil {
		return row
	}

	// repair_edge fields.
	if v, ok := c.Context["edge_id"].(string); ok {
		row["edge_id"] = v
	}
	if v, ok := c.Context["relation"].(string); ok {
		row["relation"] = v
	}
	if v, ok := c.Context["original_stub"].(string); ok {
		row["original_stub"] = v
	}
	if v, ok := c.Context["disposition"].(string); ok {
		row["disposition"] = v
	}
	if v, ok := c.Context["disposition_reason"].(string); ok {
		row["disposition_reason"] = v
	}
	if v, ok := c.Context["from_entity"]; ok {
		row["from_entity"] = v
	}

	// dynamic_baseurl_endpoint fields (#708).
	if v, ok := c.Context["category"].(string); ok {
		row["category"] = v
	}
	if v, ok := c.Context["dynamic_kind"].(string); ok {
		row["dynamic_kind"] = v
	}
	if v, ok := c.Context["path"].(string); ok && c.Kind == enrichment.KindDynamicBaseURLEndpoint {
		row["path"] = v
	}
	if v, ok := c.Context["verb"].(string); ok && c.Kind == enrichment.KindDynamicBaseURLEndpoint {
		row["verb"] = v
	}
	if v, ok := c.Context["source_file"].(string); ok && c.Kind == enrichment.KindDynamicBaseURLEndpoint {
		row["source_file"] = v
	}
	if v, ok := c.Context["static_path_suffix"].(string); ok {
		row["static_path_suffix"] = v
	}
	if v, ok := c.Context["dynamic_prefix_var"].(string); ok {
		row["dynamic_prefix_var"] = v
	}

	// Expose the full context too so callers don't have to fetch the raw
	// file. This is the most expensive field but it's bounded by the
	// emitter's context-window cap (~50 lines).
	row["context"] = c.Context
	return row
}
