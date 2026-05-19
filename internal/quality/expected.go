// Package quality implements the extraction-quality benchmark framework.
//
// Where bug-rate (internal/resolve) measures CLASSIFICATION quality — given
// an extracted edge, is its resolver Disposition correct — the quality
// package measures EXTRACTION quality: did we find every entity + edge that
// SHOULD have been extracted, and did our targets bind to the right thing?
//
// Each fixture lives under internal/quality/golden/<name>/ with:
//
//	src/             — small hand-curated source tree
//	expected.json    — hand-verified expected entities + relationships
//
// The harness loads expected.json, runs the production indexer over src/,
// and emits recall / forbidden-hit / target-accuracy metrics.
//
// This is intentionally orthogonal to bug-rate. A repo can score
// bug_rate=0% while still missing half of the real edges — bug-rate only
// scores what was extracted, not what was missed.
package quality

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Fixture is the in-memory representation of an expected.json file.
//
// We deliberately keep this loose — `must_exist` rather than enumerating a
// closed world — because the indexer also legitimately extracts framework-
// detected entities (Routes, Middlewares, etc.) that we don't want to
// over-specify. Recall is therefore "did the must_exist things show up",
// not "is the extracted set exactly this".
type Fixture struct {
	Name                    string                  `json:"fixture_name"`
	Language                string                  `json:"language"`
	Description             string                  `json:"description,omitempty"`
	ExpectedEntities        []ExpectedEntity        `json:"expected_entities"`
	ExpectedRelationships   []ExpectedRelationship  `json:"expected_relationships"`
	ForbiddenRelationships  []ExpectedRelationship  `json:"forbidden_relationships,omitempty"`
}

// ExpectedEntity is a hand-curated assertion about what the indexer SHOULD
// produce. Match() decides whether an extracted graph.Entity satisfies this
// expectation.
//
// MatchBy chooses the field used to identify the entity inside graph.json:
//
//	"name"           — match by Entity.Name (case-sensitive, exact)
//	"qualified_name" — match by Entity.QualifiedName
//	"source_file"    — match by SourceFile + Name (preferred for fixtures
//	                   that have name collisions, e.g. two `Meta` classes)
//
// If MatchBy is empty we default to "name+kind" which is sufficient for
// most small fixtures.
//
// Kind is matched exactly (e.g. "SCOPE.Component", "SCOPE.Operation").
type ExpectedEntity struct {
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name,omitempty"`
	Kind          string `json:"kind"`
	SourceFile    string `json:"source_file,omitempty"`
	MatchBy       string `json:"match_by,omitempty"`
	MustExist     bool   `json:"must_exist"`
	// NiceToHave entities are evaluated but NOT counted as a recall miss
	// when absent. Useful for capabilities we want to track (e.g. signal
	// receivers, custom managers) without holding the fixture hostage to
	// them.
	NiceToHave bool `json:"nice_to_have,omitempty"`
	// Note is free-form prose for the fixture author. Ignored by the harness.
	Note string `json:"note,omitempty"`
}

// ExpectedRelationship is a hand-curated edge assertion. The matcher reads
// FromName / ToName because expected.json is written BEFORE entity IDs
// (which are SHA-truncated content hashes) are known.
//
// To match an extracted relationship, the harness:
//  1. Resolves FromName + FromKind to an Entity ID by looking it up in the
//     extracted graph (Entity.Name match, optionally narrowed by FromFile).
//  2. Same for ToName + ToKind.
//  3. Checks whether the extracted Relationships slice contains an edge
//     with (FromID, ToID, Kind).
//
// When the ToID is expected to be a bare-name external (e.g. CALLS
// django.db.models.Model.objects.filter), the harness also accepts an
// edge whose ToID matches ToBareName directly (no entity lookup required).
type ExpectedRelationship struct {
	FromName     string `json:"from_name"`
	FromKind     string `json:"from_kind,omitempty"`
	FromFile     string `json:"from_file,omitempty"`
	Kind         string `json:"kind"`
	ToName       string `json:"to_name,omitempty"`
	ToKind       string `json:"to_kind,omitempty"`
	ToFile       string `json:"to_file,omitempty"`
	ToBareName   string `json:"to_bare_name,omitempty"`
	MustExist    bool   `json:"must_exist"`
	NiceToHave   bool   `json:"nice_to_have,omitempty"`
	Note         string `json:"note,omitempty"`
}

// LoadFixture reads expected.json from the given fixture directory.
// The fixture's source tree is at <dir>/src/, which the caller passes to
// the indexer separately.
func LoadFixture(dir string) (*Fixture, error) {
	p := filepath.Join(dir, "expected.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	var f Fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	if f.Name == "" {
		return nil, fmt.Errorf("%s: fixture_name is required", p)
	}
	return &f, nil
}

// SourceDir returns the path the harness will hand to the indexer.
// We keep this trivial so callers don't have to know the layout.
func SourceDir(fixtureDir string) string {
	return filepath.Join(fixtureDir, "src")
}
