package hcl

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ----------------------------------------------------------------
// Issue #387 — IMPORTS, CALLS, CONTAINS edges for HCL/Terraform
// ----------------------------------------------------------------
//
// IMPORTS:
//   - module block: source = "..."   → file IMPORTS source value
//   - provider block: provider "x"   → file IMPORTS provider name
//
// CONTAINS:
//   - file-level SCOPE.Component / file entity carries one CONTAINS edge per
//     top-level block. ToID is BuildOperationStructuralRef("hcl", path, name)
//     where name is the canonical block reference name:
//       resource   → "<type>.<name>"
//       data       → "data.<type>.<name>"
//       module     → "module.<name>"
//       provider   → "provider.<name>"
//       variable   → "var.<name>"
//       output     → "output.<name>"
//       locals key → "local.<key>"
//
// CALLS:
//   - per resource/data/module body, every interpolation reference
//     (variable_expr + get_attr chain) becomes a CALLS edge whose ToID is
//     the canonical reference name (matching the CONTAINS naming above so
//     the resolver can join them).

// blockReferenceName returns the canonical reference name for a top-level
// block, used as both the CONTAINS structural-ref base and the CALLS target
// resolution form. Returns "" if the block is unrecognised.
func blockReferenceName(blockType string, labels []string, localKey string) string {
	switch blockType {
	case "resource":
		if len(labels) >= 2 {
			return labels[0] + "." + labels[1]
		}
	case "data":
		if len(labels) >= 2 {
			return "data." + labels[0] + "." + labels[1]
		}
	case "module":
		if len(labels) >= 1 {
			return "module." + labels[0]
		}
	case "provider":
		if len(labels) >= 1 {
			return "provider." + labels[0]
		}
	case "variable":
		if len(labels) >= 1 {
			return "var." + labels[0]
		}
	case "output":
		if len(labels) >= 1 {
			return "output." + labels[0]
		}
	case "locals":
		if localKey != "" {
			return "local." + localKey
		}
	}
	return ""
}

// emitFileLevelRelationships scans the body of a parsed HCL file and emits a
// SCOPE.Component / file entity carrying CONTAINS edges to every top-level
// block, plus IMPORTS edges for module sources and provider blocks. Returns
// nil if the file has no top-level blocks.
func emitFileLevelRelationships(root *sitter.Node, src []byte, path, lang string) *types.EntityRecord {
	if root == nil {
		return nil
	}
	var body *sitter.Node
	if root.Type() == "config_file" {
		body = firstChildByType(root, "body")
	} else if root.Type() == "body" {
		body = root
	}
	if body == nil {
		return nil
	}

	var rels []types.RelationshipRecord
	count := int(body.ChildCount())
	for i := 0; i < count; i++ {
		child := body.Child(i)
		if child == nil || child.Type() != "block" {
			continue
		}
		blockType := blockTypeIdent(child, src)
		if blockType == "" || blockType == "terraform" {
			continue
		}
		labels := blockLabels(child, src)

		// CONTAINS edges per block.
		switch blockType {
		case "locals":
			// One CONTAINS per attribute key.
			lbody := blockBody(child)
			if lbody == nil {
				continue
			}
			for j := 0; j < int(lbody.ChildCount()); j++ {
				attr := lbody.Child(j)
				if attr == nil || attr.Type() != "attribute" {
					continue
				}
				keyNode := firstChildByType(attr, "identifier")
				if keyNode == nil {
					continue
				}
				key := nodeText(keyNode, src)
				if key == "" {
					continue
				}
				ref := blockReferenceName("locals", nil, key)
				rels = append(rels, types.RelationshipRecord{
					FromID: path,
					ToID:   extractor.BuildOperationStructuralRef(lang, path, ref),
					Kind:   "CONTAINS",
				})
			}
		default:
			ref := blockReferenceName(blockType, labels, "")
			if ref == "" {
				continue
			}
			rels = append(rels, types.RelationshipRecord{
				FromID: path,
				ToID:   extractor.BuildOperationStructuralRef(lang, path, ref),
				Kind:   "CONTAINS",
			})
		}

		// IMPORTS edges.
		switch blockType {
		case "module":
			if bb := blockBody(child); bb != nil {
				if source := attributeStringValue(bb, "source", src); source != "" {
					rels = append(rels, types.RelationshipRecord{
						FromID: path,
						ToID:   source,
						Kind:   "IMPORTS",
						Properties: map[string]string{
							"import_kind":   "module",
							"source_module": source,
							"imported_name": source,
							// Issue #44 — tag language so the resolver's
							// dynamic-pattern catalog (hclDynamicPatterns)
							// matches relative-path module sources like
							// `../../` even on the non-embedded
							// classification path and in diagnostic dumps.
							"language": lang,
						},
					})
				}
			}
		case "provider":
			if len(labels) >= 1 && labels[0] != "" {
				rels = append(rels, types.RelationshipRecord{
					FromID: path,
					ToID:   labels[0],
					Kind:   "IMPORTS",
					Properties: map[string]string{
						"import_kind":   "provider",
						"source_module": labels[0],
						"imported_name": labels[0],
						// Issue #44 — see module branch above.
						"language": lang,
					},
				})
			}
		}
	}

	if len(rels) == 0 {
		return nil
	}

	// Derive a friendly file name for Name.
	name := path
	if slash := strings.LastIndexByte(path, '/'); slash >= 0 {
		name = path[slash+1:]
	}

	return &types.EntityRecord{
		Name:          name,
		Kind:          "SCOPE.Component",
		Subtype:       "file",
		SourceFile:    path,
		Language:      lang,
		QualityScore:  0.7,
		Relationships: rels,
		Metadata:      map[string]interface{}{"subtype": "file"},
	}
}

// extractCalls walks a block body and returns CALLS relationships for every
// interpolation reference. selfRef (if non-empty) suppresses self-edges.
//
// Issue #44 (HCL) — ToID is emitted as a Format A structural-ref tied to
// the current file path so the resolver can bind via byLocation. Same-file
// refs (the dominant case inside `examples/<x>/main.tf`) resolve cleanly;
// cross-file refs in a multi-file module fall back to the dynamic-pattern
// catalog in internal/resolve/refs.go (hclDynamicPatterns).
func extractCalls(body *sitter.Node, src []byte, path, lang, fromRef, selfRef string) []types.RelationshipRecord {
	if body == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var rels []types.RelationshipRecord

	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if n.Type() == "expression" {
			if ref := canonicalRefFromExpression(n, src); ref != "" {
				if ref != selfRef {
					if _, dup := seen[ref]; !dup {
						seen[ref] = struct{}{}
						rels = append(rels, types.RelationshipRecord{
							// FromID is a structural-ref to the parent block in
							// the current file so the resolver binds via
							// byLocation (the parent entity's Name is the same
							// canonical ref). Empty FromID would otherwise be
							// kept bare and split byName ambiguously.
							FromID: extractor.BuildOperationStructuralRef(lang, path, fromRef),
							ToID:   extractor.BuildOperationStructuralRef(lang, path, ref),
							Kind:   "CALLS",
						})
					}
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	// Walk the body but skip the depends_on attribute (already produces
	// DEPENDS_ON edges, not CALLS).
	for i := 0; i < int(body.ChildCount()); i++ {
		child := body.Child(i)
		if child == nil {
			continue
		}
		if child.Type() == "attribute" {
			keyNode := firstChildByType(child, "identifier")
			if keyNode != nil && nodeText(keyNode, src) == "depends_on" {
				continue
			}
		}
		walk(child)
	}
	return rels
}

// canonicalRefFromExpression returns the canonical reference name for an
// expression node containing a variable_expr + get_attr chain. Returns "" if
// the expression is not a reference (e.g. literal, function call only).
//
// Rules (matching blockReferenceName output):
//
//	parts[0]=var      → "var.<parts[1]>"
//	parts[0]=local    → "local.<parts[1]>"
//	parts[0]=module   → "module.<parts[1]>"
//	parts[0]=data     → "data.<parts[1]>.<parts[2]>"
//	otherwise         → "<parts[0]>.<parts[1]>" (resource ref)
//
// Returns "" if there are fewer than 2 parts.
func canonicalRefFromExpression(expr *sitter.Node, src []byte) string {
	if expr == nil {
		return ""
	}
	var parts []string
	for i := 0; i < int(expr.ChildCount()); i++ {
		child := expr.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "variable_expr":
			id := firstChildByType(child, "identifier")
			if id != nil {
				parts = append(parts, nodeText(id, src))
			}
		case "get_attr":
			id := firstChildByType(child, "identifier")
			if id != nil {
				parts = append(parts, nodeText(id, src))
			}
		}
	}
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "var":
		return "var." + parts[1]
	case "local":
		return "local." + parts[1]
	case "module":
		return "module." + parts[1]
	case "data":
		if len(parts) >= 3 {
			return "data." + parts[1] + "." + parts[2]
		}
		return ""
	default:
		// Resource reference: <type>.<name>
		return parts[0] + "." + parts[1]
	}
}
