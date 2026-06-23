// prune_import_placeholders.go — Issue #681: eliminate dangling SCOPE.Component
// import-placeholder entities from the Java extractor's emission.
//
// # Problem
//
// Previously, buildImport emitted a separate SCOPE.Component entity per
// import_declaration (Name = local bound name, no Subtype). That entity
// served only as a carrier for the IMPORTS relationship; nothing else
// referenced it because REFERENCES edges from method bodies resolve to the
// real external entity (ext:java:List), not the placeholder. Result:
// 1205 out of 1392 import-placeholder entities on client-fixture-d had
// zero inbound edges — a ~25–35pp contribution to the 55.2% orphan rate.
//
// # Fix (Option 1 from #681)
//
// Attach IMPORTS relationships directly to the per-file SCOPE.Component
// entity (subtype="file", Name=file path) that every extractor emits via
// extractor.FileEntity. The resolver's BuildImportTable reads IMPORTS edges
// from r.Relationships on ANY entity and uses rel.Properties (local_name /
// source_module / imported_name / wildcard) to build the per-file binding
// table — it does not care which entity is the carrier.
//
// This eliminates all import-placeholder entities without losing any
// resolver-visible information. The IMPORTS edge and its Properties are
// preserved; only the redundant SCOPE.Component wrapper entity is dropped.
//
// # Correctness invariants
//
// 1. The file entity (entities[0]) always exists when Extract is called
//    (it is the first entity appended).
// 2. The IMPORTS relationship FromID is still the file path — unchanged.
// 3. resolveImportToIDs (Track B) iterates all entities looking for
//    Kind=="SCOPE.Component" and IMPORTS edges; it will find these on the
//    file entity (which is also SCOPE.Component subtype="file") and rewrite
//    their ToID exactly as before.
// 4. The resolver's BuildImportTable reads rel.FromID (falling back to
//    r.SourceFile) — both still equal the file path.
// 5. Same-package SCOPE.Component entities for classes/interfaces/enums
//    (which have a non-empty Subtype) are NOT affected.
//
// # Tests
//
// See prune_import_placeholders_test.go for:
// - External-import placeholder is NOT emitted (zero dangling entities).
// - IMPORTS edge + properties are present on the file entity.
// - In-package class SCOPE.Component entities are unaffected.
// - Quality fixtures remain 100% recall.

package java

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// attachImportRelationships walks the CST for import_declaration nodes,
// builds an IMPORTS RelationshipRecord for each, and appends them to the
// file-level entity (fileEntity). It replaces the old buildImport path that
// emitted a separate SCOPE.Component placeholder entity per import.
//
// fileEntity must be non-nil. On return, fileEntity.Relationships will
// contain one IMPORTS record per import_declaration node found in root.
// Malformed import nodes (empty text after stripping) are silently dropped.
func attachImportRelationships(root ts.Node, file extractor.FileInput, fileEntity *types.EntityRecord) {
	if root == nil || fileEntity == nil {
		return
	}
	for _, n := range findAllNodes(root, "import_declaration") {
		rel, ok := buildImportRel(n, file)
		if !ok {
			continue
		}
		fileEntity.Relationships = append(fileEntity.Relationships, rel)
	}
}

// buildImportRel constructs a single IMPORTS RelationshipRecord from an
// import_declaration CST node. It carries the same Properties contract as
// the old buildImport entity:
//
//   - local_name    — simple identifier bound in this file (e.g. "List")
//   - source_module — dotted package path (e.g. "java.util")
//   - imported_name — same as local_name for non-static, non-wildcard
//   - wildcard      — "1" for `import com.foo.*;`
//
// For wildcard imports localName is omitted (Properties["local_name"] is
// absent) and wildcard="1" is set, matching the Python extractor contract.
//
// Returns (zero, false) for empty or unparseable import text.
func buildImportRel(node ts.Node, file extractor.FileInput) (types.RelationshipRecord, bool) {
	raw := strings.TrimSpace(string(file.Content[node.StartByte():node.EndByte()]))
	raw = strings.TrimPrefix(raw, "import ")
	_ = strings.HasPrefix(raw, "static ") // isStatic recorded below via leaf
	raw = strings.TrimPrefix(raw, "static ")
	raw = strings.TrimSuffix(raw, ";")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return types.RelationshipRecord{}, false
	}

	props := map[string]string{}
	toID := raw

	switch {
	case strings.HasSuffix(raw, ".*"):
		// Wildcard: source_module is the path with the trailing ".*"
		// stripped. ToID drops the wildcard so synth / resolver don't
		// treat "*" as a leaf identifier.
		mod := strings.TrimSuffix(raw, ".*")
		props["source_module"] = mod
		props["wildcard"] = "1"
		toID = mod
	default:
		// Non-wildcard. local_name = leaf (last dotted segment),
		// source_module = path with the leaf stripped.
		leaf := raw
		mod := raw
		if dot := strings.LastIndexByte(raw, '.'); dot > 0 {
			leaf = raw[dot+1:]
			mod = raw[:dot]
		}
		props["local_name"] = leaf
		props["source_module"] = mod
		props["imported_name"] = leaf
	}

	return types.RelationshipRecord{
		FromID:     file.Path,
		ToID:       toID,
		Kind:       "IMPORTS",
		Properties: props,
	}, true
}
