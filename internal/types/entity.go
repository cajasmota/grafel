// Package types defines core entity, relationship, and batch types
// for the archigraph pipeline.
package types

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// EnrichmentStatus typed string for enrichment pipeline state.
type EnrichmentStatus string

const (
	StatusPending  EnrichmentStatus = "pending"
	StatusEnriched EnrichmentStatus = "enriched"
	StatusDegraded EnrichmentStatus = "degraded"
)

// EntityRecord is the central data contract shared by all five Lambda handlers
// and all extractors. Fields match the Python indexer EntityRecord schema.
type EntityRecord struct {
	ID          string `json:"id"`
	OrgID       string `json:"org_id"`
	ProjectID   string `json:"project_id"`
	ProjectSlug string `json:"project_slug"`
	// RepoID is the GitHub repository full name (e.g. "cajasmota/archigraph").
	// Sourced from the ExtractTriggerMessage.RepoURL in the Extract Lambda.
	// Required by the Typesense entity_embeddings schema for tenant isolation.
	RepoID           string           `json:"repo_id,omitempty"`
	Name             string           `json:"name"`
	QualifiedName    string           `json:"qualified_name"`
	Kind             string           `json:"kind"`
	SourceFile       string           `json:"source_file"`
	StartLine        int              `json:"start_line"`
	EndLine          int              `json:"end_line"`
	Language         string           `json:"language"`
	Content          string           `json:"content"`
	Description      string           `json:"description"`
	Domain           string           `json:"domain"`
	Subtype          string           `json:"subtype"`
	Signature        string           `json:"signature"`
	Tags             []string         `json:"tags,omitempty"`
	QualityScore     float64          `json:"quality_score"`
	// Confidence in [0.0, 1.0] reflecting how certain the extraction is.
	// Phase 1C (#2769). Zero/unset reads as 1.0 via EffectiveConfidence —
	// the default semantics for direct-AST extractors that never stamp.
	Confidence       float64          `json:"confidence,omitempty"`
	EnrichmentStatus EnrichmentStatus `json:"enrichment_status"`
	// EnrichmentRequired is the Extract-stage decision: does this entity need LLM enrichment?
	EnrichmentRequired bool                   `json:"enrichment_required"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
	// Properties holds framework-specific attributes.
	Properties    map[string]string    `json:"properties,omitempty"`
	Relationships []RelationshipRecord `json:"relationships,omitempty"`
}

// Validate checks that all required fields are present and values are in range.
// Returns a single error containing all violations found, not just the first.
func (e *EntityRecord) Validate() error {
	var errs []string
	if e.Kind == "" {
		errs = append(errs, "kind is required")
	}
	if e.Name == "" {
		errs = append(errs, "name is required")
	}
	if e.SourceFile == "" {
		errs = append(errs, "source_file is required")
	}
	if e.QualityScore < 0.0 || e.QualityScore > 1.0 {
		errs = append(errs, fmt.Sprintf("quality_score %.4f is out of range [0.0, 1.0]", e.QualityScore))
	}
	if e.Confidence < 0.0 || e.Confidence > 1.0 {
		errs = append(errs, fmt.Sprintf("confidence %.4f is out of range [0.0, 1.0]", e.Confidence))
	}
	if len(errs) > 0 {
		return fmt.Errorf("EntityRecord validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ComputeID generates a deterministic 16-character hex ID from the entity's
// identity fields. Formula: sha256(OrgID+ProjectID+SourceFile+Kind+Name)[:16].
func (e *EntityRecord) ComputeID() string {
	h := sha256.New()
	h.Write([]byte(e.OrgID + e.ProjectID + e.SourceFile + e.Kind + e.Name))
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}
