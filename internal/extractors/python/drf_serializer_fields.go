// drf_serializer_fields.go — DRF serializer-field REFERENCES emission.
//
// Issue #2061 — Across upvate, polyglot-platform and client-fixture-de,
// SCOPE.Schema/field entities emitted for DRF serializer field declarations
// were consistently degree-0 orphans (~1 132 across three groups). Post
// #2042 / #2052 / #2057 covered model-side relational shapes (FK / O2O / M2M
// / string FKs / choices / nested ctor refs) and #2008 covered
// SerializerMethodField. The remaining gap is the rest of the DRF serializer
// field surface:
//
//   - `field = serializers.PrimaryKeyRelatedField(queryset=Foo.objects.all())`
//     declares a relation to the Foo model, but no REFERENCES edge was emitted.
//   - `field = FooSerializer(many=True, read_only=True)` (nested serializer)
//     declares a structural dependency on FooSerializer, but no REFERENCES
//     edge was emitted.
//   - `field = serializers.IntegerField(source="contract.id")` declares a
//     source path traversal whose root is an attribute on the Meta.model, but
//     no REFERENCES edge was emitted.
//   - Plain `field = serializers.CharField(...)` inside a ModelSerializer
//     whose `Meta.model = Foo` implicitly binds to Foo's `field` attribute.
//
// This pass runs AFTER applyFrameworkInnerClassProperties (so the parent
// class's `meta_model` property is already stamped from the Meta inner class)
// and AFTER extractClassFields (so the SCOPE.Schema/field entities already
// exist in the slice). It walks the immediate class body once, identifying
// every `<attr> = <call>` assignment, and:
//
//  1. If the call is a `*.PrimaryKeyRelatedField`, `*.HyperlinkedRelatedField`,
//     `*.SlugRelatedField`, or `*.ManyRelatedField`, parse the `queryset=` /
//     `child_relation=` kwarg and emit REFERENCES → target model.
//  2. If the call's leaf type ends in "Serializer" (heuristic for nested
//     serializer reference), emit REFERENCES → that serializer class.
//  3. If the call carries a `source="<path>"` kwarg and the parent serializer
//     carries `Properties["meta_model"]`, emit REFERENCES → the meta_model
//     class. The source path traversal grounds at the meta_model root.
//  4. Otherwise, if the parent class has a Meta.model and the field is a
//     known DRF scalar field type, emit a REFERENCES edge to Meta.model.
//     This binds plain `email = serializers.EmailField()` in `UserSerializer`
//     to the `User` model so the field has at least one non-CONTAINS edge.
//
// Edges target Format-A structural-refs (scope:component:ref:python:<file>:
// <ClassName>) so the resolver binds them via byLocation (same file) or
// byName (cross-file).
//
// The pass is a no-op when:
//   - the class body is nil
//   - no field entities were emitted in the [before, after) window
//   - the parent class does not look like a DRF serializer (we still process
//     it because non-serializer Django models that happen to use a "Serializer"
//     suffix nested reference would also benefit, but the meta_model fallback
//     only fires when `meta_model` is present)

package python

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// drfRelatedFieldTypes — DRF *RelatedField shapes whose target model is
// declared via the `queryset=` kwarg (or `child_relation=` for ManyRelatedField).
var drfRelatedFieldTypes = map[string]struct{}{
	"PrimaryKeyRelatedField":  {},
	"HyperlinkedRelatedField": {},
	"SlugRelatedField":        {},
	"StringRelatedField":      {},
	"ManyRelatedField":        {},
}

// drfScalarSerializerFieldTypes — DRF scalar field types that, when declared
// inside a ModelSerializer with `Meta.model = Foo`, implicitly bind to Foo.
// We use this list (rather than "any *.X" suffix match) to avoid linking
// generic helper calls.
var drfScalarSerializerFieldTypes = map[string]struct{}{
	"BooleanField":          {},
	"NullBooleanField":      {},
	"CharField":             {},
	"EmailField":            {},
	"RegexField":            {},
	"SlugField":             {},
	"URLField":              {},
	"UUIDField":             {},
	"FilePathField":         {},
	"IPAddressField":        {},
	"IntegerField":          {},
	"FloatField":            {},
	"DecimalField":          {},
	"DateField":             {},
	"DateTimeField":         {},
	"TimeField":             {},
	"DurationField":         {},
	"ChoiceField":           {},
	"MultipleChoiceField":   {},
	"FileField":             {},
	"ImageField":            {},
	"ListField":             {},
	"DictField":             {},
	"HStoreField":           {},
	"JSONField":             {},
	"ReadOnlyField":         {},
	"HiddenField":           {},
	"ModelField":            {},
	"SerializerMethodField": {},
}

// emitDRFSerializerFieldRefs is the entry point invoked from the class-body
// walker after extractClassFields + applyFrameworkInnerClassProperties.
//
// parameters mirror enrichDjangoModelFieldsAndManagers:
//
//	body         — the class_definition's body sitter node
//	file         — the file under extraction (for SourceFile + Content)
//	parentClass  — dotted-parent name ("UserSerializer" or "Outer.Inner")
//	classIdx     — index into *out of the parent class entity (carries meta_model)
//	before/after — entity-slice window emitted by the body walk
//	out          — entity slice pointer (in/out)
func emitDRFSerializerFieldRefs(
	body ts.Node,
	file extractor.FileInput,
	parentClass string,
	classIdx int,
	before, after int,
	out *[]types.EntityRecord,
) {
	if body == nil || parentClass == "" || out == nil {
		return
	}
	if classIdx < 0 || classIdx >= len(*out) {
		return
	}

	// Build attr → field entity index over the slice window.
	fieldIdx := make(map[string]int, max(0, after-before))
	prefix := parentClass + "."
	for k := before; k < after; k++ {
		e := &(*out)[k]
		if e.Kind != "SCOPE.Schema" || e.Subtype != "field" {
			continue
		}
		if e.SourceFile != file.Path {
			continue
		}
		if !strings.HasPrefix(e.Name, prefix) {
			continue
		}
		leaf := e.Name[len(prefix):]
		if leaf == "" || strings.Contains(leaf, ".") {
			// Skip nested-class field names (e.g. "Foo.Meta.something").
			continue
		}
		fieldIdx[leaf] = k
	}
	if len(fieldIdx) == 0 {
		return
	}

	// The Meta.model class name (if any) — bare leaf, no module prefix.
	metaModel := metaModelLeaf(&(*out)[classIdx])

	// Walk class-body statements once.
	for i := 0; i < int(body.ChildCount()); i++ {
		stmt := body.Child(i)
		if stmt == nil || stmt.Type() != "expression_statement" {
			continue
		}
		for j := 0; j < int(stmt.NamedChildCount()); j++ {
			expr := stmt.NamedChild(j)
			if expr == nil || expr.Type() != "assignment" {
				continue
			}
			lhs := expr.ChildByFieldName("left")
			rhs := expr.ChildByFieldName("right")
			if lhs == nil || rhs == nil {
				continue
			}
			if lhs.Type() != "identifier" || rhs.Type() != "call" {
				continue
			}
			attr := nodeText(lhs, file.Content)
			if attr == "" {
				continue
			}
			idx, ok := fieldIdx[attr]
			if !ok {
				continue
			}
			funcNode := rhs.ChildByFieldName("function")
			if funcNode == nil {
				continue
			}
			funcText := nodeText(funcNode, file.Content)
			leafType := funcText
			if dot := strings.LastIndexByte(leafType, '.'); dot >= 0 {
				leafType = leafType[dot+1:]
			}

			// Skip shapes already handled by other passes:
			//  - Django model fields (FK/O2O/M2M + scalars) — enriched by
			//    enrichDjangoModelFieldsAndManagers (REFERENCES + properties).
			//    Their leafType is in djangoFieldTypes. ChoiceField also overlaps
			//    with djangoFieldTypes via `IntegerField/CharField/...`; we
			//    intentionally let enrichDjangoModelFieldsAndManagers run first.
			//    The class context disambiguates: model fields are inside a
			//    Django Model (no Meta.model property), serializer fields are
			//    inside a Serializer (Meta.model property set).
			//  - SerializerMethodField — covered by emitSerializerMethodFieldLinks
			//    (RESOLVED_BY edge to get_<field>).

			// (1) *RelatedField with queryset= kwarg.
			if _, isRel := drfRelatedFieldTypes[leafType]; isRel {
				if target := extractRelatedFieldTarget(rhs, file.Content); target != "" {
					appendRefEdge(&(*out)[idx], file.Path, target, map[string]string{
						"drf_field_type": leafType,
						"binding":        "queryset",
					})
					continue
				}
			}

			// (2) Nested serializer — leafType ends in "Serializer" and is
			// a bare identifier (not a `serializers.X` call). This rules out
			// `serializers.Serializer` and `serializers.ModelSerializer`
			// (those have funcText with a dot).
			if strings.HasSuffix(leafType, "Serializer") && funcNode.Type() == "identifier" && isCapitalisedIdent(funcText) {
				appendRefEdge(&(*out)[idx], file.Path, leafType, map[string]string{
					"drf_nested_serializer": "true",
				})
				continue
			}

			// (3) source="<path>" kwarg — bind to Meta.model when available.
			if metaModel != "" {
				if src := lookupStringKwarg(rhs, file.Content, "source"); src != "" {
					appendRefEdge(&(*out)[idx], file.Path, metaModel, map[string]string{
						"drf_field_type": leafType,
						"binding":        "source",
						"source_path":    src,
					})
					continue
				}
			}

			// (4) Scalar DRF field in a ModelSerializer — implicit Meta.model bind.
			if metaModel != "" {
				if _, isScalar := drfScalarSerializerFieldTypes[leafType]; isScalar {
					// Skip when this is a Django model field declaration —
					// detected by absence of Meta.model on the parent. Already
					// guarded by metaModel != "" check above.
					appendRefEdge(&(*out)[idx], file.Path, metaModel, map[string]string{
						"drf_field_type": leafType,
						"binding":        "meta_model_implicit",
					})
					continue
				}
			}

			// (5) Plain serializers.Serializer field without Meta.model (#2081 Cat-C).
			//
			// Classes that inherit from serializers.Serializer (not ModelSerializer)
			// often have no Meta inner class at all. Their scalar field declarations
			// have no meaningful external target entity. We emit a weak USES edge
			// pointing at the parent serializer class so the field entity is non-orphan
			// via an outbound structural relationship.
			//
			// For fields whose leafType itself ends in "Serializer" (custom nested
			// serializer reference), rule (2) already fires via funcNode.Type() ==
			// "identifier" check — so those fields have REFERENCES and won't reach
			// here. For all other scalar shapes we emit USES → parentClass.
			//
			// USES_SCHEMA applies when the leafType is a registered graph entity (e.g.
			// a custom field class defined in the same codebase). We detect this by
			// checking whether leafType is capitalised AND not in the known DRF scalar
			// list (which covers all stdlib DRF types). If it is capitalised and
			// unknown to us, it may be a project-local custom field — emit USES_SCHEMA
			// to the leaf type instead so the resolver can bind it.
			if metaModel == "" {
				if _, isScalar := drfScalarSerializerFieldTypes[leafType]; isScalar {
					appendUsesEdge(&(*out)[idx], file.Path, parentClass, map[string]string{
						"drf_field_type": leafType,
						"binding":        "plain_serializer_parent",
					})
					continue
				}
				// Unknown capitalised leaf type that isn't a known DRF scalar — could
				// be a project-local custom field class. Emit USES_SCHEMA → leafType.
				if isCapitalisedIdent(leafType) && !strings.HasSuffix(leafType, "Serializer") {
					appendUsesSchemaEdge(&(*out)[idx], file.Path, leafType, map[string]string{
						"drf_field_type": leafType,
						"binding":        "custom_field_type",
					})
					continue
				}
			}
		}
	}
}

// appendUsesEdge appends a USES edge from the field entity to the target
// class (e.g. the parent serializer). Used for plain serializers.Serializer
// fields that have no Meta.model binding target (#2081 Cat-C).
func appendUsesEdge(field *types.EntityRecord, filePath, targetClass string, props map[string]string) {
	if field == nil || targetClass == "" {
		return
	}
	toID := buildDjangoModelClassRef(filePath, targetClass)
	for _, r := range field.Relationships {
		if r.Kind == "USES" && r.ToID == toID {
			return
		}
	}
	field.Relationships = append(field.Relationships,
		types.RelationshipRecord{
			ToID:       toID,
			Kind:       "USES",
			Properties: props,
		})
}

// appendUsesSchemaEdge appends a USES_SCHEMA edge from the field entity to a
// custom field type class. Used when a plain serializers.Serializer field uses
// a project-local custom field class (e.g. MoneyField, PhoneField) that may
// itself be a graph entity (#2081 Cat-C).
func appendUsesSchemaEdge(field *types.EntityRecord, filePath, targetClass string, props map[string]string) {
	if field == nil || targetClass == "" {
		return
	}
	toID := buildDjangoModelClassRef(filePath, targetClass)
	for _, r := range field.Relationships {
		if r.Kind == "USES_SCHEMA" && r.ToID == toID {
			return
		}
	}
	field.Relationships = append(field.Relationships,
		types.RelationshipRecord{
			ToID:       toID,
			Kind:       "USES_SCHEMA",
			Properties: props,
		})
}

// metaModelLeaf returns the bare class name of the parent serializer's
// `Meta.model = X` property, or "" when none is set. The property is stamped
// by applyFrameworkInnerClassProperties under Properties["meta_model"] and
// may include a module prefix (e.g. `models.User`) or quotes; strip both.
func metaModelLeaf(cls *types.EntityRecord) string {
	if cls == nil || cls.Properties == nil {
		return ""
	}
	v := strings.TrimSpace(cls.Properties["meta_model"])
	v = stripQuotes(v)
	if dot := strings.LastIndexByte(v, '.'); dot >= 0 {
		v = v[dot+1:]
	}
	return v
}

// extractRelatedFieldTarget parses a *RelatedField call and returns the bare
// target-model class name, or "" when none is resolvable. Shapes recognised:
//
//	PrimaryKeyRelatedField(queryset=Foo.objects.all())     → "Foo"
//	PrimaryKeyRelatedField(queryset=Foo.objects.filter(…)) → "Foo"
//	PrimaryKeyRelatedField(queryset=models.Foo.objects)    → "Foo"
//	SlugRelatedField(queryset=Foo.objects.all(), slug_field="…") → "Foo"
//	HyperlinkedRelatedField(queryset=Foo.objects.all(), view_name="…") → "Foo"
func extractRelatedFieldTarget(callNode ts.Node, src []byte) string {
	argsNode := callNode.ChildByFieldName("arguments")
	if argsNode == nil {
		return ""
	}
	for i := 0; i < int(argsNode.ChildCount()); i++ {
		arg := argsNode.Child(i)
		if arg == nil || arg.Type() != "keyword_argument" {
			continue
		}
		keyNode := arg.ChildByFieldName("name")
		valNode := arg.ChildByFieldName("value")
		if keyNode == nil || valNode == nil {
			continue
		}
		key := nodeText(keyNode, src)
		if key != "queryset" && key != "child_relation" {
			continue
		}
		// Best-effort: extract the leftmost identifier of a chained
		// attribute access. `Foo.objects.all()` → walk down `call.function`
		// (an attribute), then attribute.object until we hit an identifier.
		return leftmostIdentifier(valNode, src)
	}
	return ""
}

// leftmostIdentifier returns the leftmost bare identifier of an attribute /
// call chain (e.g. `Foo.objects.all()` → "Foo"; `models.Foo.objects` → "Foo"
// — the latter takes the bare-model leaf when the leftmost is a module-style
// lowercase identifier).
func leftmostIdentifier(node ts.Node, src []byte) string {
	for node != nil {
		switch node.Type() {
		case "identifier":
			return nodeText(node, src)
		case "call":
			fn := node.ChildByFieldName("function")
			if fn == nil {
				return ""
			}
			node = fn
		case "attribute":
			obj := node.ChildByFieldName("object")
			if obj == nil {
				return ""
			}
			// For `models.Foo.objects`, leftmost is `models` (lowercase).
			// Walk one level and if we land on `models.Foo` (attribute whose
			// object is a lowercase identifier and attribute is Capitalised),
			// return the attribute leaf instead.
			if obj.Type() == "attribute" {
				inner := obj.ChildByFieldName("object")
				attrName := obj.ChildByFieldName("attribute")
				if inner != nil && inner.Type() == "identifier" && attrName != nil {
					innerText := nodeText(inner, src)
					attrText := nodeText(attrName, src)
					if innerText != "" && innerText[0] >= 'a' && innerText[0] <= 'z' &&
						isCapitalisedIdent(attrText) {
						return attrText
					}
				}
			}
			node = obj
		default:
			return ""
		}
	}
	return ""
}

// lookupStringKwarg returns the unquoted value of a string-literal kwarg on
// the given call, or "" when the kwarg is absent or not a string literal.
func lookupStringKwarg(callNode ts.Node, src []byte, name string) string {
	argsNode := callNode.ChildByFieldName("arguments")
	if argsNode == nil {
		return ""
	}
	for i := 0; i < int(argsNode.ChildCount()); i++ {
		arg := argsNode.Child(i)
		if arg == nil || arg.Type() != "keyword_argument" {
			continue
		}
		keyNode := arg.ChildByFieldName("name")
		valNode := arg.ChildByFieldName("value")
		if keyNode == nil || valNode == nil {
			continue
		}
		if nodeText(keyNode, src) != name {
			continue
		}
		if valNode.Type() != "string" {
			return ""
		}
		return stripQuotes(strings.TrimSpace(nodeText(valNode, src)))
	}
	return ""
}

// appendRefEdge appends a REFERENCES edge from the field entity to the
// target class, deduplicated on ToID. Properties carry the binding kind
// so graph audits can isolate the provenance without re-parsing source.
func appendRefEdge(field *types.EntityRecord, filePath, targetClass string, props map[string]string) {
	if field == nil || targetClass == "" {
		return
	}
	toID := buildDjangoModelClassRef(filePath, targetClass)
	for _, r := range field.Relationships {
		if r.Kind == "REFERENCES" && r.ToID == toID {
			return
		}
	}
	field.Relationships = append(field.Relationships,
		types.RelationshipRecord{
			ToID:       toID,
			Kind:       "REFERENCES",
			Properties: props,
		})
}
