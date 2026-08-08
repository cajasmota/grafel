package main

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6275 — foldClassHierarchyShadows (#1613) must not fold a base class
// node into a #6104 merge-facet TWIN of that same node. A twin (grafel.twin_of
// set, see internal/types/entity.go's IsMergeTwinAlias) is, by the #6104
// contract, a SECOND representation of the SAME source construct under a
// DIFFERENT Kind that is meant to COEXIST with its anchor — see
// internal/extractors/custom_dispatch.go's Tier B ("BOTH survive"). The #1613
// fold's bySymbol candidate table does not know about that contract: it
// treats any record whose Kind appears in FrameworkClassKindPriority
// (SCOPE.Schema included, priority 80) as a legitimate REPLACEMENT survivor
// for the base SCOPE.Component class node — indistinguishable from a genuine
// framework-typed node (a Django Model, a Spring Controller, ...) that really
// does displace the generic AST node. Folding the base node into its own
// twin erases the base node from the graph (its Kind/qualified_name/edges
// move onto the twin), which is exactly backwards from "both survive" and is
// the root cause of #6275: java-spring-mini's `SCOPE.Component User` loses
// all of its CONTAINS/IMPORTS edges to the twin `SCOPE.Schema User` facet.
func TestFoldClassHierarchyShadows_DoesNotFoldBaseIntoItsOwnTwinFacet(t *testing.T) {
	const (
		srcFile = "com/example/demo/model/User.java"
		name    = "User"
	)

	baseID := makeTestID("SCOPE.Component", name, srcFile)
	facetID := makeTestID("SCOPE.Schema", name, srcFile)

	memberRefID := makeTestID("SCOPE.Schema", "User.email", srcFile)

	base := types.EntityRecord{
		ID:         baseID,
		Kind:       "SCOPE.Component",
		Name:       name,
		SourceFile: srcFile,
		Subtype:    "class",
		StartLine:  7,
		EndLine:    20,
		Relationships: []types.RelationshipRecord{
			// owned CONTAINS edge (empty FromID) — the shape the Java
			// extractor emits from a class to its fields (java.go:450-454).
			{ToID: memberRefID, Kind: "CONTAINS"},
		},
	}
	facet := types.EntityRecord{
		ID:         facetID,
		Kind:       "SCOPE.Schema",
		Name:       name,
		SourceFile: srcFile,
		StartLine:  7,
		EndLine:    20,
		Properties: map[string]string{types.EntityTwinOfProperty: baseID},
	}
	member := types.EntityRecord{
		ID: memberRefID, Kind: "SCOPE.Schema", Name: "User.email", SourceFile: srcFile, StartLine: 8,
	}

	idx := dupkindIndexer()
	out, _, stats := idx.foldClassHierarchyShadows([]types.EntityRecord{base, facet, member}, nil)

	if stats.ShadowsFolded != 0 {
		t.Errorf("ShadowsFolded = %d; want 0 — the base node must not fold into its own #6104 twin", stats.ShadowsFolded)
	}

	byID := make(map[string]types.EntityRecord, len(out))
	for _, r := range out {
		byID[r.ID] = r
	}

	survivedBase, ok := byID[baseID]
	if !ok {
		t.Fatalf("base SCOPE.Component %s was dropped; #6104 requires both twin members to survive", baseID)
	}
	if survivedBase.Kind != "SCOPE.Component" {
		t.Errorf("base node Kind = %q; want SCOPE.Component (unfolded)", survivedBase.Kind)
	}
	var hasContains bool
	for _, r := range survivedBase.Relationships {
		if r.Kind == "CONTAINS" && r.ToID == memberRefID {
			hasContains = true
		}
	}
	if !hasContains {
		t.Errorf("base SCOPE.Component lost its own CONTAINS edge to %s after folding", memberRefID)
	}

	survivedFacet, ok := byID[facetID]
	if !ok {
		t.Fatalf("facet SCOPE.Schema %s was dropped", facetID)
	}
	if survivedFacet.Kind != "SCOPE.Schema" {
		t.Errorf("facet Kind = %q; want SCOPE.Schema (unfolded)", survivedFacet.Kind)
	}
}
