// Package agentpatterns implements the storage layer for agent-learned patterns
// as described in ADR-0018. Patterns are first-class graph entities that store
// codebase-specific recipes, link to real code exemplars, and improve as agents
// apply and correct them.
//
// Storage is per-group JSON at <group>/.archigraph/patterns.json (matching the
// convention of enrichment-resolutions.json and repair.json). FlatBuffers
// migration is deferred to v1.1.
package agentpatterns

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// SchemaVersion is the integer version of the on-disk patterns.json schema.
const SchemaVersion = 1

// Category classifies the nature of a Pattern.
type Category string

const (
	CategoryCode         Category = "code"
	CategoryProcess      Category = "process"
	CategoryTeam         Category = "team"
	CategoryTooling      Category = "tooling"
	CategoryArchitecture Category = "architecture"
)

// Trigger describes when a pattern should be applied.
type Trigger struct {
	NaturalLanguage   string   `json:"natural_language"`
	Keywords          []string `json:"keywords,omitempty"`
	TargetEntityKinds []string `json:"target_entity_kinds,omitempty"`
}

// AntiPattern describes something that must NOT be done when applying a pattern.
type AntiPattern struct {
	DoNot   string `json:"do_not"`
	Reason  string `json:"reason"`
	Private bool   `json:"private"` // if true, excluded from CLAUDE.md export and doc-gen
}

// Scope constrains which repositories, paths, languages, stacks, and entity
// kinds a pattern applies to. An absent (empty) slice is a wildcard.
type Scope struct {
	Repos       []string `json:"repos,omitempty"`
	ModulePaths []string `json:"module_paths,omitempty"`
	Languages   []string `json:"languages,omitempty"`
	Stacks      []string `json:"stacks,omitempty"` // e.g. "go/chi", "python/django"
	EntityKinds []string `json:"entity_kinds,omitempty"`
}

// Pattern is the in-memory representation of one agent-learned pattern. It
// maps 1:1 to an entity in the graph with kind "AgentPattern" (see
// internal/types/kinds.go, EntityKindAgentPattern).
type Pattern struct {
	// ID is sha256(group + trigger.natural_language)[:16].
	ID   string `json:"id"`
	Kind string `json:"kind"` // always "AgentPattern"

	Trigger      Trigger       `json:"trigger"`
	Steps        []string      `json:"steps"`
	AntiPatterns []AntiPattern `json:"anti_patterns,omitempty"`
	Scope        Scope         `json:"scope"`

	Category Category `json:"category"`

	// Confidence is [0.0, 1.0]. New patterns start at 0.4.
	Confidence float64 `json:"confidence"`
	// Observations is the cumulative number of times this pattern has been
	// referenced (applied, rejected, refined).
	Observations int `json:"observations"`

	// LastValidated is the unix timestamp of the last validation event.
	LastValidated int64 `json:"last_validated,omitempty"`
	// LastApplied is the unix timestamp of the last apply event.
	LastApplied int64 `json:"last_applied,omitempty"`

	// IsCandidate is true until convergence + user-approval.
	IsCandidate bool `json:"is_candidate"`
	// ConvergenceCount is the number of independent subagents that proposed
	// this candidate.
	ConvergenceCount int `json:"convergence_count,omitempty"`
	// ProposerSubagents holds identifiers of subagents that contributed
	// observations (for audit).
	ProposerSubagents []string `json:"proposer_subagents,omitempty"`

	// DocumentationURL is populated by Phase 6 doc-gen integration.
	DocumentationURL string `json:"documentation_url,omitempty"`

	// Exemplars holds the entity IDs of canonical examples of this pattern
	// in use. Required on record (≥1). Written as EXEMPLAR edges to the graph.
	Exemplars []string `json:"exemplars,omitempty"`

	// Touches holds entity IDs that this pattern touches (produces TOUCHES edges).
	Touches []string `json:"touches,omitempty"`

	// AntiExemplars holds entity IDs that are counter-examples (ANTI_EXEMPLAR edges).
	AntiExemplars []string `json:"anti_exemplars,omitempty"`

	// RejectReason is populated by action=reject.
	RejectReason string `json:"reject_reason,omitempty"`
	// RejectTimestamp is the unix timestamp of the last reject event.
	RejectTimestamp int64 `json:"reject_timestamp,omitempty"`

	// ApprovalNote is an optional user-facing note set during action=promote.
	ApprovalNote string `json:"approval_note,omitempty"`
}

// PatternID computes the deterministic ID for a pattern given its group and
// the trigger's natural_language text.
//
// The ID is stable: same (group, naturalLanguage) input → same 16-char hex
// output, matching the convention used by graph.EntityID and enrichment.
func PatternID(group, naturalLanguage string) string {
	h := sha256.New()
	h.Write([]byte(group))
	h.Write([]byte{0})
	h.Write([]byte(naturalLanguage))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// New returns a new Pattern initialised with sensible defaults. The caller
// should populate Trigger, Steps, etc. before saving.
func New(group string, trigger Trigger, category Category) *Pattern {
	return &Pattern{
		ID:          PatternID(group, trigger.NaturalLanguage),
		Kind:        "AgentPattern",
		Trigger:     trigger,
		Category:    category,
		Confidence:  InitialConfidence,
		IsCandidate: true,
	}
}

// ---------------------------------------------------------------------------
// Storage envelope
// ---------------------------------------------------------------------------

// patternsEnvelope is the on-disk shape of patterns.json.
type patternsEnvelope struct {
	Version  int       `json:"version"`
	Patterns []Pattern `json:"patterns"`
}

// patternsPath returns the canonical path for the group's patterns.json file.
// groupArchigraphDir is the <group>/.archigraph/ directory, matching the
// convention used by enrichment-resolutions.json.
func patternsPath(groupArchigraphDir string) string {
	return filepath.Join(groupArchigraphDir, "patterns.json")
}

// Save atomically writes the pattern slice to <groupArchigraphDir>/patterns.json.
// Patterns are sorted by ID before writing to ensure stable, diffable output.
func Save(groupArchigraphDir string, patterns []Pattern) error {
	if err := os.MkdirAll(groupArchigraphDir, 0o755); err != nil {
		return fmt.Errorf("agentpatterns: mkdir %s: %w", groupArchigraphDir, err)
	}

	// Stable sort by ID so consecutive saves are byte-identical when no data
	// changed (required by the determinism invariant, issue #481).
	sorted := make([]Pattern, len(patterns))
	copy(sorted, patterns)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})

	env := patternsEnvelope{
		Version:  SchemaVersion,
		Patterns: sorted,
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("agentpatterns: marshal: %w", err)
	}

	path := patternsPath(groupArchigraphDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("agentpatterns: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("agentpatterns: rename: %w", err)
	}
	return nil
}

// Load reads <groupArchigraphDir>/patterns.json and returns the pattern slice.
// Returns an empty slice (not nil) if the file does not exist, matching the
// convention of enrichment.ReadResolutions.
func Load(groupArchigraphDir string) ([]Pattern, error) {
	data, err := os.ReadFile(patternsPath(groupArchigraphDir))
	if os.IsNotExist(err) {
		return []Pattern{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("agentpatterns: read: %w", err)
	}

	var env patternsEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("agentpatterns: unmarshal: %w", err)
	}
	if env.Patterns == nil {
		return []Pattern{}, nil
	}
	return env.Patterns, nil
}

// Upsert inserts or replaces a single pattern in the patterns slice (matched by
// ID). Returns the updated slice. Does not persist — callers must call Save.
func Upsert(patterns []Pattern, p Pattern) []Pattern {
	for i, existing := range patterns {
		if existing.ID == p.ID {
			patterns[i] = p
			return patterns
		}
	}
	return append(patterns, p)
}

// ByID returns the pattern with the given ID from the slice, or nil.
func ByID(patterns []Pattern, id string) *Pattern {
	for i := range patterns {
		if patterns[i].ID == id {
			return &patterns[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Time helpers exposed for the scheduler skeleton
// ---------------------------------------------------------------------------

// NowUnix returns the current Unix timestamp. Indirected through a var so
// tests can freeze time without a global clock.
var NowUnix = func() int64 { return time.Now().Unix() }
