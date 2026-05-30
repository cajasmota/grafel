package hcl

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ----------------------------------------------------------------
// Issue #3527 — deeper Terraform/HCL extraction
//
// Adds, on top of the issue #387 baseline (resource/data/module/provider/
// variable/output/locals + DEPENDS_ON/CALLS/CONTAINS/IMPORTS):
//
//   1. Iteration meta-args: for_each / count recognition on
//      resource/data/module blocks. Records iteration mode in Metadata and
//      emits a USES edge to the iteration source (e.g. var.instances). The
//      each.* / count.* / self.* pseudo-references are suppressed (handled
//      in canonicalRefFromExpression).
//   2. dynamic "x" {} nested blocks: emitted as a child
//      SCOPE.Component / dynamic_block entity (previously dropped by
//      walkBody which only dispatches top-level blocks). Its own for_each
//      meta-arg is recognised.
//   3. Module I/O data-flow edges: a module input that consumes another
//      module's output (vpc_id = module.net.vpc_id) emits a USES data-flow
//      edge module.this → module.net tagged with the input argument name.
//   4. terraform_remote_state: data.terraform_remote_state.X.outputs.Y
//      consumption emits a cross-stack DEPENDS_ON edge (property
//      cross_stack=true) to the remote-state data source, instead of being
//      dropped as a generic data ref.
//   5. terraform {} block: required_providers (provider+version) and
//      backend (type) captured as a SCOPE.Component / terraform_settings
//      entity carrying REQUIRES edges per provider, instead of being
//      dropped. lifecycle / moved / import blocks recognised on resources.
// ----------------------------------------------------------------

// iterationMeta inspects a block body for for_each / count meta-args.
// Returns the iteration mode ("for_each", "count", or "") and the canonical
// reference name of the iteration source if it is an entity reference (e.g.
// "var.instances", "local.subnets", "module.net"); srcRef is "" for literal
// counts like `count = 2`.
func iterationMeta(body *sitter.Node, src []byte) (mode, srcRef string) {
	if body == nil {
		return "", ""
	}
	for i := 0; i < int(body.ChildCount()); i++ {
		attr := body.Child(i)
		if attr == nil || attr.Type() != "attribute" {
			continue
		}
		keyNode := firstChildByType(attr, "identifier")
		if keyNode == nil {
			continue
		}
		key := nodeText(keyNode, src)
		if key != "for_each" && key != "count" {
			continue
		}
		mode = key
		// The first expression child holds the iteration source.
		if expr := firstChildByType(attr, "expression"); expr != nil {
			srcRef = canonicalRefFromExpression(expr, src)
		}
		return mode, srcRef
	}
	return "", ""
}

// applyIterationMeta records iteration metadata on a block entity and appends
// a USES edge to the iteration source when it is a resolvable entity ref.
func applyIterationMeta(rec *types.EntityRecord, body *sitter.Node, src []byte, path, lang, selfRef string) {
	mode, srcRef := iterationMeta(body, src)
	if mode == "" {
		return
	}
	if rec.Metadata == nil {
		rec.Metadata = map[string]interface{}{}
	}
	rec.Metadata["iteration"] = mode
	if srcRef == "" {
		return
	}
	rec.Metadata["iteration_source"] = srcRef
	rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
		FromID: extractor.BuildOperationStructuralRef(lang, path, selfRef),
		ToID:   extractor.BuildOperationStructuralRef(lang, path, srcRef),
		Kind:   "USES",
		Properties: map[string]string{
			"dataflow":  "iteration",
			"meta_arg":  mode,
			"data_flow": "iteration_source",
		},
	})
}

// extractDynamicBlocks walks a block body and emits one
// SCOPE.Component / dynamic_block entity per `dynamic "x" {}` nested block.
// parentRef is the canonical reference of the enclosing block (used to scope
// the dynamic block's name and to draw a CONTAINS edge). The dynamic block's
// own for_each meta-arg is recognised and its content interpolations become
// CALLS edges.
func extractDynamicBlocks(body *sitter.Node, src []byte, path, lang, parentRef string) []types.EntityRecord {
	if body == nil {
		return nil
	}
	var out []types.EntityRecord
	for i := 0; i < int(body.ChildCount()); i++ {
		child := body.Child(i)
		if child == nil || child.Type() != "block" {
			continue
		}
		if blockTypeIdent(child, src) != "dynamic" {
			continue
		}
		labels := blockLabels(child, src)
		if len(labels) < 1 || labels[0] == "" {
			continue
		}
		dynName := labels[0]
		selfRef := parentRef + ".dynamic." + dynName
		start, end := nodeLines(child)
		rec := types.EntityRecord{
			Name:         selfRef,
			Kind:         "SCOPE.Component",
			Subtype:      "dynamic_block",
			SourceFile:   path,
			StartLine:    start,
			EndLine:      end,
			Language:     lang,
			QualityScore: 0.85,
			Metadata: map[string]interface{}{
				"subtype": "dynamic_block",
				"label":   dynName,
				"parent":  parentRef,
			},
		}
		dynBody := blockBody(child)
		if dynBody != nil {
			// Iteration source of the dynamic block itself.
			applyIterationMeta(&rec, dynBody, src, path, lang, selfRef)
			// content {} interpolations → CALLS edges from the dynamic block.
			calls := extractCalls(dynBody, src, path, lang, selfRef, selfRef)
			rec.Relationships = append(rec.Relationships, calls...)
		}
		// CONTAINS edge parent → dynamic block.
		rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
			FromID: extractor.BuildOperationStructuralRef(lang, path, parentRef),
			ToID:   extractor.BuildOperationStructuralRef(lang, path, selfRef),
			Kind:   "CONTAINS",
			Properties: map[string]string{
				"nested": "dynamic",
			},
		})
		out = append(out, rec)
	}
	return out
}

// extractModuleDataFlow inspects a module block body for input arguments whose
// value consumes another module's output (e.g. vpc_id = module.net.vpc_id) and
// returns USES data-flow edges module.this → module.net, tagged with the input
// argument name. This is the headline module-to-module wiring edge: it is
// distinct from the generic CALLS edge (which only records the reference) by
// carrying the consuming input arg in its properties.
func extractModuleDataFlow(body *sitter.Node, src []byte, path, lang, selfRef string) []types.RelationshipRecord {
	if body == nil {
		return nil
	}
	var rels []types.RelationshipRecord
	seen := map[string]struct{}{}
	for i := 0; i < int(body.ChildCount()); i++ {
		attr := body.Child(i)
		if attr == nil || attr.Type() != "attribute" {
			continue
		}
		keyNode := firstChildByType(attr, "identifier")
		if keyNode == nil {
			continue
		}
		argName := nodeText(keyNode, src)
		if argName == "" || argName == "source" || argName == "version" {
			continue
		}
		// Walk the attribute value expression for module.X references.
		var walk func(n *sitter.Node)
		walk = func(n *sitter.Node) {
			if n == nil {
				return
			}
			if n.Type() == "expression" {
				if ref := canonicalRefFromExpression(n, src); strings.HasPrefix(ref, "module.") && ref != selfRef {
					dedup := argName + "→" + ref
					if _, ok := seen[dedup]; !ok {
						seen[dedup] = struct{}{}
						rels = append(rels, types.RelationshipRecord{
							FromID: extractor.BuildOperationStructuralRef(lang, path, selfRef),
							ToID:   extractor.BuildOperationStructuralRef(lang, path, ref),
							Kind:   "USES",
							Properties: map[string]string{
								"dataflow":  "module_io",
								"input_arg": argName,
								"data_flow": "module_output",
							},
						})
					}
				}
			}
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i))
			}
		}
		walk(attr)
	}
	return rels
}

// extractRemoteStateDeps scans a block body for
// data.terraform_remote_state.<name>.outputs.<key> consumption and returns a
// cross-stack DEPENDS_ON edge to the remote-state data source (one per distinct
// remote-state name). These are deliberately NOT generic data-source CALLS
// edges: they wire one Terraform stack to another stack's published outputs.
func extractRemoteStateDeps(body *sitter.Node, src []byte, path, lang, fromRef string) []types.RelationshipRecord {
	if body == nil {
		return nil
	}
	var rels []types.RelationshipRecord
	seen := map[string]struct{}{}

	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if n.Type() == "expression" {
			if name := remoteStateName(n, src); name != "" {
				if _, ok := seen[name]; !ok {
					seen[name] = struct{}{}
					target := "data.terraform_remote_state." + name
					rels = append(rels, types.RelationshipRecord{
						FromID: extractor.BuildOperationStructuralRef(lang, path, fromRef),
						ToID:   extractor.BuildOperationStructuralRef(lang, path, target),
						Kind:   "DEPENDS_ON",
						Properties: map[string]string{
							"cross_stack":  "true",
							"remote_state": name,
						},
					})
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(body)
	return rels
}

// remoteStateName returns the remote-state name if the expression is a
// data.terraform_remote_state.<name>.* reference, else "".
func remoteStateName(expr *sitter.Node, src []byte) string {
	parts := referenceParts(expr, src)
	if len(parts) >= 3 && parts[0] == "data" && parts[1] == "terraform_remote_state" {
		return parts[2]
	}
	return ""
}

// referenceParts returns the dotted identifier parts of a reference expression
// (variable_expr + get_attr chain). Shared shape with
// canonicalRefFromExpression but returns the raw parts.
func referenceParts(expr *sitter.Node, src []byte) []string {
	if expr == nil {
		return nil
	}
	var parts []string
	for i := 0; i < int(expr.ChildCount()); i++ {
		child := expr.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "variable_expr":
			if id := firstChildByType(child, "identifier"); id != nil {
				parts = append(parts, nodeText(id, src))
			}
		case "get_attr":
			if id := firstChildByType(child, "identifier"); id != nil {
				parts = append(parts, nodeText(id, src))
			}
		}
	}
	return parts
}

// ----------------------------------------------------------------
// terraform {} settings block
// ----------------------------------------------------------------

// extractTerraformBlock turns a `terraform {}` settings block into a
// SCOPE.Component / terraform_settings entity capturing required_providers
// (provider source + version) and backend (type). Returns (record, true) when
// the block carries at least one provider requirement or a backend; otherwise
// (zero, false) so empty/version-only terraform blocks stay metadata-only.
func extractTerraformBlock(n *sitter.Node, src []byte, path, lang string, start, end int) ([]types.EntityRecord, bool) {
	body := blockBody(n)
	if body == nil {
		return nil, false
	}

	rec := types.EntityRecord{
		Name:         "terraform.settings",
		Kind:         "SCOPE.Component",
		Subtype:      "terraform_settings",
		SourceFile:   path,
		StartLine:    start,
		EndLine:      end,
		Language:     lang,
		QualityScore: 0.8,
		Metadata:     map[string]interface{}{"subtype": "terraform_settings"},
	}

	var providers []string
	hasContent := false

	for i := 0; i < int(body.ChildCount()); i++ {
		child := body.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "attribute":
			keyNode := firstChildByType(child, "identifier")
			if keyNode != nil && nodeText(keyNode, src) == "required_version" {
				if v := extractStringFromAttr(child, src); v != "" {
					rec.Metadata["required_version"] = v
				}
			}
		case "block":
			bt := blockTypeIdent(child, src)
			switch bt {
			case "required_providers":
				rps := parseRequiredProviders(child, src)
				for name, ver := range rps {
					providers = append(providers, name)
					hasContent = true
					// IMPORTS edge per required provider; reuses the provider
					// IMPORTS convention so the resolver binds the provider
					// name through the same dynamic-pattern catalog.
					rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
						FromID: extractor.BuildOperationStructuralRef(lang, path, "terraform.settings"),
						ToID:   name,
						Kind:   "IMPORTS",
						Properties: map[string]string{
							"import_kind":   "required_provider",
							"source_module": name,
							"imported_name": name,
							"provider":      name,
							"version":       ver,
							"language":      lang,
						},
					})
				}
			case "backend":
				labels := blockLabels(child, src)
				if len(labels) >= 1 && labels[0] != "" {
					rec.Metadata["backend"] = labels[0]
					hasContent = true
				}
			}
		}
	}

	if !hasContent {
		return nil, false
	}
	if len(providers) > 0 {
		rec.Metadata["required_providers"] = strings.Join(providers, ",")
	}
	return []types.EntityRecord{rec}, true
}

// parseRequiredProviders parses a `required_providers {}` block, returning a
// map of provider-local-name → { source/version } collapsed to its version
// string (empty when absent). It handles the object form:
//
//	aws = { source = "hashicorp/aws", version = "~> 5.0" }
func parseRequiredProviders(block *sitter.Node, src []byte) map[string]string {
	body := blockBody(block)
	if body == nil {
		return nil
	}
	out := map[string]string{}
	for i := 0; i < int(body.ChildCount()); i++ {
		attr := body.Child(i)
		if attr == nil || attr.Type() != "attribute" {
			continue
		}
		keyNode := firstChildByType(attr, "identifier")
		if keyNode == nil {
			continue
		}
		name := nodeText(keyNode, src)
		if name == "" {
			continue
		}
		out[name] = objectAttrString(attr, "version", src)
	}
	return out
}

// objectAttrString finds `key = "..."` inside an attribute's object value and
// returns the string literal, else "". Used for required_providers entries.
func objectAttrString(attr *sitter.Node, key string, src []byte) string {
	obj := findFirstByType(attr, "object")
	if obj == nil {
		return ""
	}
	for i := 0; i < int(obj.ChildCount()); i++ {
		elem := obj.Child(i)
		if elem == nil || elem.Type() != "object_elem" {
			continue
		}
		// object_elem: expression(variable_expr identifier) = expression(...)
		keyExpr := elem.Child(0)
		if keyExpr == nil {
			continue
		}
		if firstIdentText(keyExpr, src) != key {
			continue
		}
		return firstTemplateLiteral(elem, src)
	}
	return ""
}

// firstIdentText returns the text of the first identifier descendant.
func firstIdentText(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	if n.Type() == "identifier" {
		return nodeText(n, src)
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if t := firstIdentText(n.Child(i), src); t != "" {
			return t
		}
	}
	return ""
}

// firstTemplateLiteral returns the first template_literal text under n, but
// only after the `=` sign (i.e. the value side). Simpler: scan all
// template_literal descendants and return the last one, which is the value in
// an object_elem (key side is an identifier, not a string).
func firstTemplateLiteral(n *sitter.Node, src []byte) string {
	var found string
	var walk func(x *sitter.Node)
	walk = func(x *sitter.Node) {
		if x == nil {
			return
		}
		if x.Type() == "template_literal" {
			found = nodeText(x, src)
		}
		for i := 0; i < int(x.ChildCount()); i++ {
			walk(x.Child(i))
		}
	}
	walk(n)
	return found
}

// findFirstByType returns the first descendant (DFS) of n with the given type.
func findFirstByType(n *sitter.Node, typ string) *sitter.Node {
	if n == nil {
		return nil
	}
	if n.Type() == typ {
		return n
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if r := findFirstByType(n.Child(i), typ); r != nil {
			return r
		}
	}
	return nil
}

// hasLifecycleMetaBlocks scans a resource body for lifecycle / moved / import
// nested blocks and returns the list of recognised meta-block names. These are
// recorded in resource Metadata so downstream consumers can see that a resource
// declares lifecycle rules without us synthesising spurious entities.
func hasLifecycleMetaBlocks(body *sitter.Node, src []byte) []string {
	if body == nil {
		return nil
	}
	var found []string
	for i := 0; i < int(body.ChildCount()); i++ {
		child := body.Child(i)
		if child == nil || child.Type() != "block" {
			continue
		}
		switch blockTypeIdent(child, src) {
		case "lifecycle":
			found = append(found, "lifecycle")
		case "provisioner":
			found = append(found, "provisioner")
		}
	}
	return found
}
