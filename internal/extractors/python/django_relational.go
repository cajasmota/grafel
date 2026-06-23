// django_relational.go — Django model relational extraction. Addresses three
// related gaps in the Python extractor surfaced by W2R1 / W4R3 grinds:
//
//   - #1977 — `models.ForeignKey(Target, ...)` field declarations produced
//     no REFERENCES edge to the target Model class. Same for OneToOneField
//     and ManyToManyField. The resulting model graph was a forest of
//     disconnected nodes with no walkable relationships.
//   - #1978 — every field entity emitted by extractClassFields carried only
//     a Name and start_line. `field_type` (CharField/ForeignKey/...) and
//     kwargs (max_length, null, blank, on_delete, related_name, choices,
//     default, ...) were dropped on the floor, blocking API docs prose and
//     downstream type-hint inference.
//   - #1989 — `objects = SomeManager()` and other Manager attachments on
//     Django Model class bodies were invisible: no REFERENCES edge linked
//     the Model to its Manager, so grafel_expand(Model) didn't list
//     the Manager as a neighbour.
//   - #2049 — `ForeignKey('Building', ...)` (string reference) produced a
//     Constraint/External placeholder entity instead of resolving to the
//     real Building Model. The root cause: the resolver's
//     lookupUniqueRealComponentByName fallback fails when the same class
//     name appears in multiple apps (ambiguous), and the byPackageComponent
//     cross-file same-package fallback doesn't fire for the "ref" subtype.
//     Fix: stamp django_fk_string on the REFERENCES edge so the graph-wide
//     ResolveDjangoStringFKRefs pass (internal/resolve/django_fk.go) can
//     use the app_label segment of dotted forms ("auth.User" → app="auth")
//     to narrow the candidate set to models in that Django app's directory,
//     and fall back to byPackageComponent for same-app cross-file FKs.
//
// All three original issues are addressed in a single post-pass over the
// same class body that extractClassFields walks, because they share the
// same input shape: `<attr> = <call_expr>` statements at the class body's
// immediate scope.
//
// Design notes:
//
//   - Field entities are emitted FIRST by extractClassFields, then this
//     function enriches them in-place. We do not re-emit entities.
//   - REFERENCES edges target SCOPE.Component/class entities. For in-file
//     model targets the resolver's byLocation path binds the stub; for
//     cross-file targets the byName fallback binds the stub (provided the
//     target Class.Name is unique in the merged graph — when ambiguous the
//     resolver leaves the stub unbound, which is correct).
//   - String FK forms stamp django_fk_string on the edge (e.g. "Building"
//     or "app_label.Building") so the resolver's late-binding pass can
//     use the app_label hint for cross-app disambiguation.
//   - kwargs are stored in Properties as a flat namespace: `kwarg.<name>`
//     so consumers iterate Properties looking for the prefix. This matches
//     the existing pattern of one-string-value-per-key on Properties
//     (e.g. db_table, ordering set by applyFrameworkInnerClassProperties).
//   - Manager attachment is conservatively detected: only fires when the
//     RHS is a call whose function is a bare identifier starting with a
//     capital letter (heuristic for "<ClassName>()"). This avoids spurious
//     edges from `count = compute_count()` style assignments.

package python

import (
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// djangoFieldTypes is the set of model-field constructor names that the
// extractor recognises as "Django model field declarations". Used to gate
// field_type / kwargs property stamping. The list is intentionally inclusive
// so custom field subclasses living under a `<module>.` prefix still hit when
// the leaf matches (the matcher uses HasSuffix on the call function text).
var djangoFieldTypes = map[string]struct{}{
	"AutoField":                 {},
	"BigAutoField":              {},
	"BigIntegerField":           {},
	"BinaryField":               {},
	"BooleanField":              {},
	"CharField":                 {},
	"DateField":                 {},
	"DateTimeField":             {},
	"DecimalField":              {},
	"DurationField":             {},
	"EmailField":                {},
	"FileField":                 {},
	"FilePathField":             {},
	"FloatField":                {},
	"ForeignKey":                {},
	"GenericIPAddressField":     {},
	"ImageField":                {},
	"IntegerField":              {},
	"JSONField":                 {},
	"ManyToManyField":           {},
	"OneToOneField":             {},
	"PositiveBigIntegerField":   {},
	"PositiveIntegerField":      {},
	"PositiveSmallIntegerField": {},
	"SlugField":                 {},
	"SmallAutoField":            {},
	"SmallIntegerField":         {},
	"TextField":                 {},
	"TimeField":                 {},
	"URLField":                  {},
	"UUIDField":                 {},
	// django.contrib.postgres common types.
	"ArrayField":  {},
	"HStoreField": {},
	"RangeField":  {},
}

// djangoRelationalFieldTypes is the subset of field types that carry a
// target-model positional argument and therefore generate REFERENCES edges.
var djangoRelationalFieldTypes = map[string]struct{}{
	"ForeignKey":      {},
	"OneToOneField":   {},
	"ManyToManyField": {},
}

// enrichDjangoModelFieldsAndManagers post-processes the class body after
// extractClassFields has emitted SCOPE.Schema/field entities. It walks the
// body once and:
//
//  1. For each `<attr> = <FieldType>(...)` statement that matches a Django
//     model-field constructor: locate the field entity emitted for <attr>
//     and stamp Properties["field_type"] + Properties["kwarg.<name>"] for
//     each keyword argument. For relational fields (ForeignKey /
//     OneToOneField / ManyToManyField) extract the first positional
//     argument and append a REFERENCES edge from the field entity to a
//     component-class structural-ref of the target Model.
//
//  2. For each `<attr> = <ClassName>(...)` statement where the call's
//     function is a bare capitalised identifier: emit a REFERENCES edge
//     from the parent Model class entity to the named class. Captures the
//     Django Manager / custom-queryset attachment shape:
//     `objects = UserManager()` / `events = EventManager()`.
//
// The function is a no-op when body is nil, parentClass is empty, or no
// field entities were emitted in the [before, after) window.
//
// parameters:
//
//	body         — the class_definition's body sitter node
//	file         — the file under extraction (for SourceFile + Content)
//	parentClass  — the dotted-parent name extractClassFields used to qualify
//	               entity Names ("<parentClass>.<attr>")
//	classIdx     — index into *out of the parent class entity (for emitting
//	               Model→Manager REFERENCES edges)
//	before       — len(*out) before walkNode descended into the class body
//	after        — len(*out) immediately before this function is invoked
//	out          — entity slice pointer (in/out)
func enrichDjangoModelFieldsAndManagers(
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
	// Build a quick lookup from attribute leaf name → field entity index.
	// Only consider entities the class-body walk just appended ([before, after))
	// that are SCOPE.Schema/field on this file with the expected qualified
	// Name shape.
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
		if leaf == "" {
			continue
		}
		fieldIdx[leaf] = k
	}

	// Walk class-body statements once.
	managerSeen := make(map[string]bool)
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
			if lhs.Type() != "identifier" {
				continue
			}
			if rhs.Type() != "call" {
				continue
			}
			attr := nodeText(lhs, file.Content)
			if attr == "" {
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

			// (1) Django model-field declaration?
			if _, isField := djangoFieldTypes[leafType]; isField {
				idx, ok := fieldIdx[attr]
				if !ok {
					continue
				}
				stampDjangoFieldProperties(&(*out)[idx], leafType, rhs, file.Content)
				// (1b) Relational field → REFERENCES edge.
				if _, isRel := djangoRelationalFieldTypes[leafType]; isRel {
					if targetName, rawFKString, isSelf := extractRelationalTargetName(rhs, file.Content, parentClass); targetName != "" {
						props := map[string]string{
							"django_rel": leafType,
							"self_ref":   boolToString(isSelf),
						}
						// #2049 — stamp the raw string FK value on the edge so the
						// graph-wide ResolveDjangoStringFKRefs pass can use the
						// app_label segment of dotted forms for cross-app resolution.
						// Only set for string literal forms; identifier/attribute forms
						// resolve accurately via byLocation and don't need the hint.
						if rawFKString != "" {
							props["django_fk_string"] = rawFKString
						}
						(*out)[idx].Relationships = append((*out)[idx].Relationships,
							types.RelationshipRecord{
								ToID:       buildDjangoModelClassRef(file.Path, targetName),
								Kind:       "REFERENCES",
								Properties: props,
							})

						// GRAPH_RELATES model↔model edge with cardinality, hung off
						// the owning model class node (parallel to the field-level
						// REFERENCES edge above). ForeignKey → many_to_one,
						// OneToOneField → one_to_one, ManyToManyField → many_to_many.
						if card := djangoRelCardinality(leafType); card != "" {
							(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
								types.RelationshipRecord{
									FromID: buildDjangoModelClassRef(file.Path, parentClass),
									ToID:   buildDjangoModelClassRef(file.Path, targetName),
									Kind:   string(types.RelationshipKindGraphRelates),
									Properties: map[string]string{
										"django_rel":  leafType,
										"cardinality": card,
										"field_name":  attr,
										"self_ref":    boolToString(isSelf),
									},
								})
						}
					}
				}
				continue
			}

			// (2) Manager attachment: `objects = UserManager()` or any
			// `<attr> = <Capitalised>(...)` shape with a bare-identifier
			// function. Skip when the function is module-qualified
			// (`models.X`) — those are field decls handled above (or
			// model-internal helpers we don't want to over-link).
			if funcNode.Type() != "identifier" {
				continue
			}
			if !isCapitalisedIdent(funcText) {
				continue
			}
			if managerSeen[funcText] {
				continue
			}
			managerSeen[funcText] = true
			(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
				types.RelationshipRecord{
					ToID: buildDjangoModelClassRef(file.Path, funcText),
					Kind: "REFERENCES",
					Properties: map[string]string{
						"django_attachment": "manager",
						"manager_attr":      attr,
					},
				})
		}
	}
}

// stampDjangoFieldProperties writes field_type + kwarg.<name> properties
// onto the SCOPE.Schema/field entity. Existing properties (e.g. set by
// other extractors) are preserved; kwarg properties overwrite on collision.
//
// Kwarg value extraction is best-effort: the raw source text of each
// keyword_argument's value node is captured, with surrounding whitespace
// trimmed and surrounding quotes stripped via stripQuotes. Complex values
// (lists, dicts, tuples, function calls) are kept verbatim so consumers
// can inspect the literal expression even when it isn't a scalar.
//
// For ForeignKey/OneToOneField/ManyToManyField the `on_delete` kwarg often
// arrives as `models.CASCADE` — the leaf segment (after the last ".") is
// what downstream consumers expect, so we additionally normalise on_delete
// by stripping any module prefix on top of the verbatim capture.
func stampDjangoFieldProperties(
	field *types.EntityRecord,
	fieldType string,
	callNode ts.Node,
	src []byte,
) {
	if field == nil || callNode == nil {
		return
	}
	if field.Properties == nil {
		field.Properties = make(map[string]string)
	}
	field.Properties["field_type"] = fieldType

	argsNode := callNode.ChildByFieldName("arguments")
	if argsNode == nil {
		return
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
		if key == "" {
			continue
		}
		val := strings.TrimSpace(nodeText(valNode, src))
		// For on_delete, normalise `models.CASCADE` → `CASCADE` to give
		// consumers a stable leaf segment regardless of how the project
		// imports django.db.models.
		if key == "on_delete" {
			if dot := strings.LastIndexByte(val, '.'); dot >= 0 {
				val = val[dot+1:]
			}
		} else {
			val = stripQuotes(val)
		}
		field.Properties["kwarg."+key] = val
	}
}

// extractRelationalTargetName parses the first positional argument of a
// ForeignKey/OneToOneField/ManyToManyField call and returns the referenced
// target-model name, the raw string literal value (for string-form FKs only),
// and whether this is a self-reference. Supported shapes:
//
//	ForeignKey(Jurisdiction, on_delete=...)        → "Jurisdiction", "", false
//	ForeignKey("Jurisdiction", ...)                → "Jurisdiction", "Jurisdiction", false
//	ForeignKey("app.Jurisdiction", ...)            → "Jurisdiction", "app.Jurisdiction", false
//	ForeignKey("self", ...)                        → parentClass, "self", true
//	ForeignKey(to=Jurisdiction, ...)               → "Jurisdiction", "", false  (keyword form)
//	ForeignKey(to="self", ...)                     → parentClass, "self", true
//
// The rawString return value is non-empty only for string-literal FK forms
// (quoted strings). It carries the unstripped value before app_label removal
// so the caller can stamp it as Properties["django_fk_string"] and the
// graph-wide late-binding resolver pass (ResolveDjangoStringFKRefs) can use
// the app_label segment for cross-app disambiguation (#2049).
//
// String-reference forms with an `app_name.` prefix return only the trailing
// model leaf as className — the resolver's byName fallback matches by bare
// class name across files. Self-references resolve to the enclosing parent
// class so `expand(<Model>)` lists itself as a neighbour where appropriate.
//
// Returns ("", "", false) when no resolvable positional/keyword target is found.
func extractRelationalTargetName(callNode ts.Node, src []byte, parentClass string) (className, rawString string, isSelf bool) {
	argsNode := callNode.ChildByFieldName("arguments")
	if argsNode == nil {
		return "", "", false
	}
	// First, look for a `to=` keyword argument (less common but valid).
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
		if nodeText(keyNode, src) != "to" {
			continue
		}
		return parseTargetExpr(valNode, src, parentClass)
	}
	// Otherwise the first positional (non-punctuation, non-keyword) argument.
	for i := 0; i < int(argsNode.ChildCount()); i++ {
		arg := argsNode.Child(i)
		if arg == nil {
			continue
		}
		switch arg.Type() {
		case "(", ")", ",", "comment":
			continue
		case "keyword_argument":
			// keyword args don't count as the first positional; keep scanning.
			continue
		}
		return parseTargetExpr(arg, src, parentClass)
	}
	return "", "", false
}

// parseTargetExpr converts the AST node for a relational field's target
// argument into a (className, rawString, isSelfReference) triple.
//
// className is the leaf class name used for the structural-ref ToID.
// rawString is non-empty only for string-literal nodes and carries the
// original quoted value before app_label stripping — used by the caller to
// stamp Properties["django_fk_string"] for the late-binding resolver pass
// (#2049). For identifier and attribute forms rawString is "" because the
// class is already uniquely identified by its Python symbol without extra
// app-label context.
//
// Returns ("", "", false) for shapes we don't recognise (callables,
// conditional expressions, etc.) rather than emitting a malformed REFERENCES
// edge.
func parseTargetExpr(node ts.Node, src []byte, parentClass string) (className, rawString string, isSelf bool) {
	if node == nil {
		return "", "", false
	}
	switch node.Type() {
	case "identifier":
		return nodeText(node, src), "", false
	case "attribute":
		// e.g. `app.Model` as an attribute access — return the leaf.
		txt := nodeText(node, src)
		if dot := strings.LastIndexByte(txt, '.'); dot >= 0 {
			return txt[dot+1:], "", false
		}
		return txt, "", false
	case "string":
		raw := stripQuotes(strings.TrimSpace(nodeText(node, src)))
		if raw == "self" {
			// Use the unqualified parent class so the REFERENCES target
			// matches the byName index entry the parent registers.
			bare := parentClass
			if dot := strings.LastIndexByte(bare, '.'); dot >= 0 {
				bare = bare[dot+1:]
			}
			return bare, "self", true
		}
		if dot := strings.LastIndexByte(raw, '.'); dot >= 0 {
			return raw[dot+1:], raw, false
		}
		return raw, raw, false
	}
	return "", "", false
}

// buildDjangoModelClassRef returns the structural-ref ToID for a REFERENCES
// edge whose target is a Python class entity. The shape matches
// buildPyReferenceTargetID's component branch:
//
//	scope:component:ref:python:<file>:<ClassName>
//
// The resolver's lookupStructural → byLocation path binds the stub when the
// target class lives in the same file; the byName fallback binds the stub
// when the target lives in another file and the bare class name is unique
// across the merged graph. We use the consumer's file path because that is
// what the existing REFERENCES emission convention uses and the resolver
// rewriter strips the file segment when falling back to byName lookup.
func buildDjangoModelClassRef(filePath, className string) string {
	return "scope:component:ref:python:" + filepath.ToSlash(filePath) + ":" + className
}

// djangoRelCardinality maps a Django relational field type to the shared ORM
// relationship-cardinality vocabulary, used as the `cardinality` prop on the
// GRAPH_RELATES edge between the owning model node and the referenced model.
//
//	ForeignKey      → many_to_one   (many rows point at one target)
//	OneToOneField   → one_to_one
//	ManyToManyField → many_to_many
func djangoRelCardinality(fieldType string) string {
	switch fieldType {
	case "ForeignKey":
		return "many_to_one"
	case "OneToOneField":
		return "one_to_one"
	case "ManyToManyField":
		return "many_to_many"
	default:
		return ""
	}
}

// isCapitalisedIdent reports whether s looks like a Python class identifier
// (non-empty, first character ASCII upper-case letter, no dots / parens).
// Used to gate Manager-attachment REFERENCES emission so we don't link
// `count = compute_count()` style assignments where the RHS is a function.
func isCapitalisedIdent(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	if c < 'A' || c > 'Z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		ch := s[i]
		if ch == '.' || ch == '(' || ch == ')' || ch == '[' {
			return false
		}
	}
	return true
}

// boolToString formats a bool as "true" / "false" for Properties storage,
// which is map[string]string.
func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// max returns the larger of two ints. Used to pre-size the field-index map.
// Replaced by builtin in Go 1.21+, kept here for stdlib portability.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
