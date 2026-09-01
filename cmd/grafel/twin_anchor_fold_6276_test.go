package main

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6276 — the mirror image of #6275's fold rule.
//
// #6275 established one half of the #6104 "both survive" contract: a merge
// facet TWIN is never an eligible fold SURVIVOR, so a base class node can no
// longer be folded INTO its own twin. That is not the whole contract. The base
// class node is the twin's ANCHOR (grafel.twin_of names its id), and a twin
// whose anchor has been folded away into some THIRD record is in exactly the
// state #6275 forbade: the anchor's Kind, span and edges have moved onto a node
// the facet does not name, and the facet's twin_of dangles.
//
// That third record is not hypothetical. python-django-mini's `User` carries
// BOTH a #6104 twin facet (the Django custom pass's SCOPE.Schema/model node,
// twin_of -> the base SCOPE.Component class) AND a Pass 2.5 Detect record of
// bare Kind "Model" (FrameworkClassKindPriority 100) for the same
// (source_file, name). The Detect node is not a twin, so #6275's exclusion does
// not reach it, and the fold-source loop folds the base class node into it —
// deleting the SCOPE.Component identity the facet was minted to coexist with.
//
// So: a fold source that is the anchor of a #6104 twin is not a fold source.
func TestFoldClassHierarchyShadows_TwinAnchorSurvivesForeignFrameworkSurvivor6276(t *testing.T) {
	const (
		srcFile = "users/models.py"
		name    = "User"
	)

	baseID := makeTestID("SCOPE.Component", name, srcFile)
	facetID := makeTestID("SCOPE.Schema", name, srcFile)
	fwID := makeTestID("Model", name, srcFile)
	memberRefID := makeTestID("SCOPE.Schema", "User.email", srcFile)

	base := types.EntityRecord{
		ID:         baseID,
		Kind:       "SCOPE.Component",
		Name:       name,
		SourceFile: srcFile,
		Subtype:    "class",
		StartLine:  15,
		EndLine:    28,
		Relationships: []types.RelationshipRecord{
			{ToID: memberRefID, Kind: "CONTAINS"},
		},
	}
	// The #6104 facet emitted by the Django custom pass, anchored on the base.
	facet := types.EntityRecord{
		ID:         facetID,
		Kind:       "SCOPE.Schema",
		Name:       name,
		SourceFile: srcFile,
		Subtype:    "model",
		StartLine:  15,
		EndLine:    28,
		Properties: map[string]string{types.EntityTwinOfProperty: baseID},
	}
	// Pass 2.5's framework-typed record for the same symbol. NOT a twin, so
	// #6275's survivor exclusion does not apply to it.
	fw := types.EntityRecord{
		ID:         fwID,
		Kind:       "Model",
		Name:       name,
		SourceFile: srcFile,
		StartLine:  15,
		EndLine:    28,
		Properties: map[string]string{"pattern_type": "yaml_driven", "role": "class"},
	}
	member := types.EntityRecord{
		ID: memberRefID, Kind: "SCOPE.Schema", Name: "User.email", SourceFile: srcFile, StartLine: 16,
	}

	idx := dupkindIndexer()
	out, _, _ := idx.foldClassHierarchyShadows(
		[]types.EntityRecord{base, facet, fw, member}, nil)

	byID := make(map[string]types.EntityRecord, len(out))
	for _, r := range out {
		byID[r.ID] = r
	}

	survivedBase, ok := byID[baseID]
	if !ok {
		t.Fatalf("base SCOPE.Component %s was folded away into the framework-typed %q node; "+
			"it is the anchor of a #6104 twin and must survive", baseID, fw.Kind)
	}
	if survivedBase.Kind != "SCOPE.Component" || survivedBase.Subtype != "class" {
		t.Errorf("anchor node = %s/%s; want SCOPE.Component/class (unfolded)",
			survivedBase.Kind, survivedBase.Subtype)
	}
	var hasContains bool
	for _, r := range survivedBase.Relationships {
		if r.Kind == "CONTAINS" && r.ToID == memberRefID {
			hasContains = true
		}
	}
	if !hasContains {
		t.Errorf("anchor SCOPE.Component lost its own CONTAINS edge to %s", memberRefID)
	}

	// The other two nodes are untouched: the framework-typed record keeps its
	// own identity (this is not a fold of Model into the class), and the facet
	// still exists to be anchored.
	if fwRec, ok := byID[fwID]; !ok {
		t.Errorf("framework-typed %q node %s was dropped", fw.Kind, fwID)
	} else if fwRec.Kind != "Model" {
		t.Errorf("framework-typed node Kind = %q; want Model", fwRec.Kind)
	}
	if _, ok := byID[facetID]; !ok {
		t.Errorf("facet SCOPE.Schema %s was dropped", facetID)
	}
}
