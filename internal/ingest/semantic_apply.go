// Package ingest — Layer 2 (agent-driven semantic) doc ingestion: the APPLY
// step (issue #4309, epic #4294).
//
// This is the apply half of the emit→apply candidate pattern begun in
// semantic.go. An EXTERNAL agent read a SemanticBundle (emitted by
// EmitDocumentBundle), ran its own LLM, and wrote back a SemanticRunResult. THIS
// file reads that result, VALIDATES it against the closed-set schema and the
// current graph (rejecting malformed or unfounded results — honest partial),
// and produces the SCOPE.DesignDecision nodes + CONTAINS/RATIONALE_FOR edges to
// splice into the graph.
//
// grafel makes NO LLM call here — it only validates and applies what the
// agent produced, exactly like docgen.ApplyResult and
// enrichment.ApplyDocgenRepairsToResolutions.
//
// IDEMPOTENCY: node and edge IDs are derived deterministically from stable
// identity (section ID + decision class + summary for nodes; from/to/kind for
// edges, via graph.RelationshipID), so re-applying the same result produces the
// same IDs and a caller that dedups by ID never doubles up.
package ingest

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// SemanticRunResult is the envelope an external agent writes back after
// classifying a bundle's sections. Its Version + PromptHash must match the
// bundle it was produced from so the apply step can reject a stale/mismatched
// result. Mirrors docgen.LLMRunResult.
type SemanticRunResult struct {
	// Version must equal the bundle's SemanticBundleVersion.
	Version string `json:"version"`
	// PromptHash must equal SemanticBundle.PromptHash.
	PromptHash string `json:"prompt_hash"`
	// DocumentID echoes the bundle's DocumentID.
	DocumentID string `json:"document_id"`
	// SectionResults holds one result per classified section. A section the
	// agent classified as None may be omitted or included with class None; both
	// are accepted and produce no node.
	SectionResults []SemanticSectionResult `json:"section_results"`
	// FilledAt is the RFC3339 timestamp the agent finished (informational).
	FilledAt string `json:"filled_at"`
}

// SemanticSectionResult is the agent's classification of one section.
type SemanticSectionResult struct {
	// SectionID must match a SemanticSectionPrompt.SectionID in the bundle.
	SectionID string `json:"section_id"`
	// Class is the agent's chosen classification (closed set).
	Class SemanticClass `json:"class"`
	// Summary is the agent's distilled claim (required for non-None classes).
	Summary string `json:"summary"`
	// RationaleTargetIDs are the code-entity IDs this claim justifies. Each MUST
	// be one of the section's bundle MentionTargets (grounding) AND exist in the
	// current graph; unfounded IDs are rejected.
	RationaleTargetIDs []string `json:"rationale_target_ids"`
}

// ApplyStats summarises an apply run. Mirrors the per-repo accounting returned
// by enrichment.ApplyDocgenRepairsToResolutions.
type ApplyStats struct {
	// DecisionsCreated is the number of SCOPE.DesignDecision nodes produced.
	DecisionsCreated int `json:"decisions_created"`
	// RationaleEdges is the number of RATIONALE_FOR edges produced.
	RationaleEdges int `json:"rationale_edges"`
	// AnchorEdges is the number of Section→DesignDecision CONTAINS edges.
	AnchorEdges int `json:"anchor_edges"`
	// SectionsClassified is the number of non-None sections accepted.
	SectionsClassified int `json:"sections_classified"`
	// SectionsNone is the number of sections the agent classified as None.
	SectionsNone int `json:"sections_none"`
	// RejectedTargets is the number of cited target IDs dropped because they
	// were not grounded mention-targets or did not exist in the graph. A
	// non-zero value is an HONEST PARTIAL: the decision node is still created,
	// but the unfounded edge is omitted rather than corrupting the graph.
	RejectedTargets int `json:"rejected_targets"`
}

// ApplyResult is the validated output of an apply run, ready to splice into the
// graph document (append entities + relationships; a caller that dedups by ID
// gets idempotency for free).
type SemanticApplyResult struct {
	Entities      []graph.Entity
	Relationships []graph.Relationship
	Stats         ApplyStats
}

// codeEntityRef is the minimal projection of a graph entity the apply step needs
// to confirm a cited rationale target exists.
type codeEntityRef struct {
	ID   string
	Name string
	Kind string
}

// ApplySemanticResult validates a SemanticRunResult against its bundle and the
// set of code entities currently in the graph, then produces the semantic
// nodes/edges. It returns an error ONLY for envelope-level corruption (version
// mismatch, stale prompt hash, unknown section, malformed class) — those reject
// the WHOLE result with a clear message and produce nothing (no partial
// corruption). Per-target grounding failures are NOT fatal: they are dropped
// and counted in Stats.RejectedTargets (honest partial), so one bad target ID
// does not sink an otherwise-valid decision.
//
// codeEntities is the current graph entity set (used to confirm cited targets
// exist). Doc/Section/DesignDecision nodes are NOT valid rationale targets — a
// rationale edge always points at a CODE entity — so they are excluded.
//
// Idempotent: identical (bundle, result) inputs yield identical entity/edge IDs.
func ApplySemanticResult(bundle SemanticBundle, result SemanticRunResult, codeEntities []graph.Entity) (SemanticApplyResult, error) {
	var out SemanticApplyResult

	// Envelope validation 1: version.
	if result.Version != bundle.Version {
		return out, fmt.Errorf("ingest: semantic apply: result version %q != bundle version %q", result.Version, bundle.Version)
	}
	// Envelope validation 2: prompt hash (stale/mismatched result).
	if result.PromptHash != bundle.PromptHash {
		return out, fmt.Errorf("ingest: semantic apply: stale result: prompt_hash %q != bundle %q", result.PromptHash, bundle.PromptHash)
	}

	// Index the bundle's sections by ID, and each section's grounded targets.
	type secInfo struct {
		prompt  SemanticSectionPrompt
		targets map[string]bool // grounded mention-target IDs for this section
	}
	bySection := make(map[string]secInfo, len(bundle.Sections))
	for _, sp := range bundle.Sections {
		tg := make(map[string]bool, len(sp.MentionTargets))
		for _, t := range sp.MentionTargets {
			tg[t.ID] = true
		}
		bySection[sp.SectionID] = secInfo{prompt: sp, targets: tg}
	}

	// Index code entities for existence checks. Exclude doc-layer nodes: a
	// rationale edge must point at code, never at another doc/section/decision.
	code := make(map[string]codeEntityRef, len(codeEntities))
	for i := range codeEntities {
		e := &codeEntities[i]
		switch e.Kind {
		case string(types.EntityKindMarkdownDocument),
			string(types.EntityKindSection),
			string(types.EntityKindDesignDecision):
			continue
		}
		code[e.ID] = codeEntityRef{ID: e.ID, Name: e.Name, Kind: e.Kind}
	}

	// Process section results in a deterministic order.
	results := append([]SemanticSectionResult(nil), result.SectionResults...)
	sort.SliceStable(results, func(a, b int) bool { return results[a].SectionID < results[b].SectionID })

	seenSection := map[string]bool{}
	for _, sr := range results {
		si, ok := bySection[sr.SectionID]
		if !ok {
			// Envelope-level: the agent referenced a section not in the bundle.
			return SemanticApplyResult{}, fmt.Errorf("ingest: semantic apply: result section_id %q not in bundle", sr.SectionID)
		}
		if seenSection[sr.SectionID] {
			return SemanticApplyResult{}, fmt.Errorf("ingest: semantic apply: duplicate section_id %q in result", sr.SectionID)
		}
		seenSection[sr.SectionID] = true

		// Class must be in the closed set.
		if !IsValidSemanticClass(sr.Class) {
			return SemanticApplyResult{}, fmt.Errorf("ingest: semantic apply: section %q has invalid class %q", sr.SectionID, sr.Class)
		}
		if sr.Class == SemanticClassNone {
			out.Stats.SectionsNone++
			continue
		}

		// Non-None requires a summary.
		summary := strings.TrimSpace(sr.Summary)
		if summary == "" {
			return SemanticApplyResult{}, fmt.Errorf("ingest: semantic apply: section %q class %q requires a non-empty summary", sr.SectionID, sr.Class)
		}

		// Build the DesignDecision node. Identity is deterministic for
		// idempotency: repo + kind + (sectionID|class) + sourceFile.
		decName := fmt.Sprintf("%s:%s", sr.SectionID, sr.Class)
		decID := graph.EntityID(
			bundle.RepoTag,
			string(types.EntityKindDesignDecision),
			decName,
			bundle.DocRelPath,
		)
		out.Entities = append(out.Entities, graph.Entity{
			ID:            decID,
			Name:          string(sr.Class),
			QualifiedName: bundle.RepoTag + "::" + decName,
			Kind:          string(types.EntityKindDesignDecision),
			SourceFile:    bundle.DocRelPath,
			StartLine:     si.prompt.StartLine,
			EndLine:       si.prompt.EndLine,
			Language:      "markdown",
		}.WithProperties(map[string]string{
			"class":      string(sr.Class),
			"summary":    summary,
			"section_id": sr.SectionID,
			"heading":    si.prompt.Heading,
		},
		))
		out.Stats.DecisionsCreated++
		out.Stats.SectionsClassified++

		// Anchor edge: Section → DesignDecision (CONTAINS), mirroring how L1
		// anchors sections under their document.
		out.Relationships = append(out.Relationships, semRel(
			sr.SectionID, decID, string(types.RelationshipKindContains), nil,
		))
		out.Stats.AnchorEdges++

		// Rationale edges: DesignDecision → each VALID cited target. A target is
		// valid iff it was a grounded mention-target for THIS section AND exists
		// in the code graph. Invalid targets are dropped (honest partial), not
		// fatal. Dedup per decision.
		seenTarget := map[string]bool{}
		tids := append([]string(nil), sr.RationaleTargetIDs...)
		sort.Strings(tids)
		for _, tid := range tids {
			if seenTarget[tid] {
				continue
			}
			seenTarget[tid] = true
			if !si.targets[tid] {
				out.Stats.RejectedTargets++ // not a grounded mention-target
				continue
			}
			ce, exists := code[tid]
			if !exists {
				out.Stats.RejectedTargets++ // not in the code graph
				continue
			}
			out.Relationships = append(out.Relationships, semRel(
				decID, tid, string(types.RelationshipKindRationaleFor),
				map[string]string{"target_kind": ce.Kind},
			))
			out.Stats.RationaleEdges++
		}
	}

	// Deterministic output ordering (mirrors Ingest).
	sort.SliceStable(out.Entities, func(a, b int) bool { return out.Entities[a].ID < out.Entities[b].ID })
	sort.SliceStable(out.Relationships, func(a, b int) bool {
		ra, rb := out.Relationships[a], out.Relationships[b]
		if ra.FromID != rb.FromID {
			return ra.FromID < rb.FromID
		}
		if ra.ToID != rb.ToID {
			return ra.ToID < rb.ToID
		}
		return ra.Kind < rb.Kind
	})
	return out, nil
}

// semRel mirrors ingest.mkRel for the semantic edges.
func semRel(from, to, kind string, props map[string]string) graph.Relationship {
	return graph.Relationship{
		ID:     graph.RelationshipID(from, to, kind),
		FromID: from,
		ToID:   to,
		Kind:   kind,
	}.WithProperties(props)
}

// ReadResult reads and unmarshals a SemanticRunResult from disk.
func ReadResult(path string) (SemanticRunResult, error) {
	var r SemanticRunResult
	data, err := os.ReadFile(path) //nolint:gosec // path from a run-dir listing
	if err != nil {
		return r, fmt.Errorf("ingest: read result %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("ingest: unmarshal result %q: %w", path, err)
	}
	return r, nil
}
