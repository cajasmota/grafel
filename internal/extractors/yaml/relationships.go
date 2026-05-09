package yaml

// Relationship helpers for the YAML extractor (Issue #386).
//
// The YAML extractor emits two relationship kinds:
//
//   - CONTAINS: structural parent → child edges. Each flavor knows its
//     own structural hierarchy:
//       GitHub Actions  workflow → job → step | action
//       GitLab CI       file → job
//       Docker Compose  file → service → port; file → volume
//       Kubernetes      file → resource → container | init_container
//       Ansible         play → task | role
//     File-rooted edges use file.Path as FromID; nested edges use the
//     parent's canonical ref (matching the child's QualifiedName scheme).
//
//   - IMPORTS: cross-references that look like dependencies in the
//     container model:
//       GitHub Actions  workflow file → `uses:` action ref (e.g.
//                       actions/checkout@v4) — one IMPORTS per unique action.
//       Docker Compose  service → service named in `depends_on:`.
//
// Every relationship is later tagged Properties["language"]="yaml" via
// extractor.TagRelationshipsLanguage in extractByFlavor.

import (
	"github.com/cajasmota/archigraph/internal/types"
)

// containsRel builds a CONTAINS RelationshipRecord (Properties left nil; the
// resolver-tag pass fills language).
func containsRel(fromID, toID string) types.RelationshipRecord {
	return types.RelationshipRecord{FromID: fromID, ToID: toID, Kind: "CONTAINS"}
}

// importsRel builds an IMPORTS RelationshipRecord with the given importKind
// recorded under Properties["import_kind"] for downstream resolver dispatch.
func importsRel(fromID, toID, importKind string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: fromID,
		ToID:   toID,
		Kind:   "IMPORTS",
		Properties: map[string]string{
			"import_kind":   importKind,
			"source_module": toID,
		},
	}
}

// findEntityIndex returns the index of the first entity whose QualifiedName
// matches ref, or -1.
func findEntityIndex(entities []types.EntityRecord, qualifiedName string) int {
	for i := range entities {
		if entities[i].QualifiedName == qualifiedName {
			return i
		}
	}
	return -1
}
