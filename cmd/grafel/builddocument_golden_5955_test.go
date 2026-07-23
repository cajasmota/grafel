package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #5955 — memory-lifetime refactor of buildDocument (free the Bazel
// full-slice copy; drain `merged` during entity conversion). This test is the
// byte-identical correctness bar: it drives buildDocument through a fixture that
// exercises the dedup path, embedded relationships, standalone pass2Rels, AND
// the Bazel resolver overlay (bazel_target entities + declared BAZEL_DEPENDS_ON
// edge + a cross-target CALLS edge that makes it "declared+used"), then hashes a
// canonical JSON serialisation of the resulting Document. The hash MUST be
// identical before and after the refactor.
func buildDocumentGoldenFixture() (pass1 []types.EntityRecord, pass2Rels []types.RelationshipRecord) {
	const (
		repo    = "test_repo"
		srcA    = "svc/a/main.go"
		srcB    = "svc/b/lib.go"
		modFile = "core/models/contract.py"
	)

	funcAID := graph.EntityID(repo, "Function", "callA", srcA)
	funcBID := graph.EntityID(repo, "Function", "targetB", srcB)
	bazelAID := graph.EntityID(repo, "BazelTarget", "a", "svc/a/BUILD")
	bazelBID := graph.EntityID(repo, "BazelTarget", "b", "svc/b/BUILD")
	classID := graph.EntityID(repo, "Class", "Contract", modFile)
	fieldID := graph.EntityID(repo, "Field", "status", modFile)

	pass1 = []types.EntityRecord{
		// Two Bazel targets, distinct packages.
		{
			Kind: "BazelTarget", Name: "a", SourceFile: "svc/a/BUILD",
			Subtype:    "bazel_target",
			Properties: map[string]string{"label": "//svc/a", "bazel_package": "svc/a"},
		},
		{
			Kind: "BazelTarget", Name: "b", SourceFile: "svc/b/BUILD",
			Subtype:    "bazel_target",
			Properties: map[string]string{"label": "//svc/b", "bazel_package": "svc/b"},
		},
		// Cross-package caller/callee — makes the a→b edge "used".
		{
			Kind: "Function", Name: "callA", SourceFile: srcA, StartLine: 3,
			Relationships: []types.RelationshipRecord{
				{FromID: "", ToID: funcBID, Kind: string(types.RelationshipKindCalls)},
			},
		},
		{Kind: "Function", Name: "targetB", SourceFile: srcB, StartLine: 7},
		// Dedup pair — survivor lacks QualifiedName, duplicate carries it.
		{
			Kind: "Class", Name: "Contract", SourceFile: modFile, Subtype: "model", StartLine: 10,
			Properties: map[string]string{"framework": "django"},
			Relationships: []types.RelationshipRecord{
				{FromID: "", ToID: fieldID, Kind: "CONTAINS"},
			},
		},
		{
			Kind: "Class", Name: "Contract", SourceFile: modFile,
			QualifiedName: "core.models.contract.Contract",
			StartLine:     10, EndLine: 42, Language: "python",
			Tags: []string{"persisted"},
			Relationships: []types.RelationshipRecord{
				{FromID: classID, ToID: fieldID, Kind: "DEFINES"},
			},
		},
		{Kind: "Field", Name: "status", SourceFile: modFile, StartLine: 12},
	}

	// Standalone pass2 rels: a declared BAZEL_DEPENDS_ON (a→b) plus a
	// cross-target CALLS edge (funcA→funcB) so the overlay emits a
	// "declared+used" BAZEL_DEP_STATUS annotation.
	pass2Rels = []types.RelationshipRecord{
		{
			FromID: bazelAID, ToID: bazelBID, Kind: "BAZEL_DEPENDS_ON",
			Properties: map[string]string{"dep_label": "//svc/b"},
		},
		{FromID: funcAID, ToID: funcBID, Kind: string(types.RelationshipKindCalls)},
	}
	return pass1, pass2Rels
}

// canonicalDocHash sorts entities and relationships by ID and returns a stable
// sha256 over their JSON encoding (Entity/Relationship MarshalJSON emit
// sorted-key Properties, so the encoding is deterministic).
func canonicalDocHash(t *testing.T, doc *graph.Document) string {
	t.Helper()
	ents := append([]graph.Entity(nil), doc.Entities...)
	rels := append([]graph.Relationship(nil), doc.Relationships...)
	sort.Slice(ents, func(i, j int) bool { return ents[i].ID < ents[j].ID })
	sort.Slice(rels, func(i, j int) bool {
		if rels[i].ID != rels[j].ID {
			return rels[i].ID < rels[j].ID
		}
		if rels[i].FromID != rels[j].FromID {
			return rels[i].FromID < rels[j].FromID
		}
		return rels[i].ToID < rels[j].ToID
	})
	payload := struct {
		Entities []graph.Entity
		Rels     []graph.Relationship
	}{ents, rels}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestBuildDocumentGolden5955(t *testing.T) {
	pass1, pass2Rels := buildDocumentGoldenFixture()
	idx := &Indexer{repoTag: "test_repo"}
	doc := idx.buildDocument(pass1, nil, pass2Rels, nil)

	// Sanity: the fixture must have exercised the Bazel overlay (a
	// BAZEL_DEP_STATUS edge must be present), otherwise target-1 is not
	// covered by this golden.
	var sawBazelStatus bool
	for k := range doc.Relationships {
		if doc.Relationships[k].Kind == "BAZEL_DEP_STATUS" {
			sawBazelStatus = true
			break
		}
	}
	if !sawBazelStatus {
		t.Fatalf("fixture did not produce a BAZEL_DEP_STATUS edge; Bazel overlay path not covered")
	}

	got := canonicalDocHash(t, doc)
	const want = "b6aa628cd2f5afe89641e3d71185ca585ad510b2b42fe06dee60119fe24c9c55"
	if got != want {
		t.Fatalf("Document canonical hash changed: got %s want %s (byte-identical output regression)", got, want)
	}
	t.Logf("BuildDocument5955 canonical hash = %s (entities=%d rels=%d)", got, len(doc.Entities), len(doc.Relationships))
}
