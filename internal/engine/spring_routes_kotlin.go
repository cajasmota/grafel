// AST-driven Spring MVC route composition for Kotlin files.
//
// Mirrors spring_routes.go (Java) for Kotlin Spring Boot controllers.
// Kotlin uses a different tree-sitter grammar from Java, so this is a
// separate pass that understands Kotlin's annotation CST shape:
//
//	Marker annotation (no args):
//	  annotation → "@" + user_type → type_identifier
//
//	Annotation with args:
//	  annotation → "@" + constructor_invocation → user_type + value_arguments
//	    Positional: value_argument → string_literal
//	    Named:      value_argument → simple_identifier("value"|"path") + "=" + string_literal
//	    Method key: value_argument → simple_identifier("method") + "=" + collection_literal
//	      e.g. [RequestMethod.GET] — extract verb from the collection text.
//
// Class and method bodies follow the same composition rules as the Java pass:
// the class-level @RequestMapping prefix composes with each method-level
// @GetMapping/@PostMapping/... path to produce the canonical route.
//
// Producer-side: emits http_endpoint_definition entities + ROUTES_TO edges.
// Refs #1421.
package engine

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/treesitter"
	"github.com/cajasmota/archigraph/internal/types"
)

// applySpringRouteCompositionKotlin runs the Kotlin Spring AST pass on a
// Kotlin file. It is the Kotlin counterpart to applySpringRouteComposition
// (spring_routes.go). It emits composed http_endpoint_definition entities
// directly (not via an intermediate Route entity) because there is no YAML
// rule layer for Kotlin Spring controllers.
//
// Returns new entities and relationships appended to the inputs.
func applySpringRouteCompositionKotlin(
	ctx context.Context,
	lang string,
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if lang != "kotlin" || len(content) == 0 {
		return entities, relationships
	}
	if !bytesContainsAny(content, "@RestController", "@Controller") {
		return entities, relationships
	}

	newEntities, newRels := extractKotlinSpringEndpoints(ctx, path, content)
	entities = append(entities, newEntities...)
	relationships = append(relationships, newRels...)
	return entities, relationships
}

// extractKotlinSpringEndpoints parses the Kotlin source with tree-sitter and
// walks all class_declaration nodes looking for Spring controllers. For each
// controller it emits one http_endpoint_definition per handler method.
func extractKotlinSpringEndpoints(ctx context.Context, path string, content []byte) ([]types.EntityRecord, []types.RelationshipRecord) {
	factory := treesitter.NewParserFactory(nil)
	pr, err := factory.Parse(ctx, content, "kotlin")
	if err != nil || pr == nil || pr.Tree == nil {
		return nil, nil
	}

	var entities []types.EntityRecord
	var rels []types.RelationshipRecord
	seen := map[string]bool{}

	root := pr.Tree.RootNode()
	walkKotlinClasses(root, content, path, &entities, &rels, seen)
	return entities, rels
}

// walkKotlinClasses does a depth-first walk looking for class_declaration nodes
// and delegating to processKotlinSpringClass for each one.
func walkKotlinClasses(
	node *sitter.Node,
	src []byte,
	path string,
	entities *[]types.EntityRecord,
	rels *[]types.RelationshipRecord,
	seen map[string]bool,
) {
	if node == nil {
		return
	}
	if node.Type() == "class_declaration" {
		processKotlinSpringClass(node, src, path, entities, rels, seen)
		// recurse: nested classes may also be controllers
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		walkKotlinClasses(node.Child(i), src, path, entities, rels, seen)
	}
}

// processKotlinSpringClass inspects a class_declaration. If the class is a
// Spring controller (carries @RestController or @Controller) AND has a
// class-level @RequestMapping, each function_declaration inside the class_body
// that carries a verb annotation is emitted as an http_endpoint_definition.
func processKotlinSpringClass(
	class *sitter.Node,
	src []byte,
	path string,
	entities *[]types.EntityRecord,
	rels *[]types.RelationshipRecord,
	seen map[string]bool,
) {
	classModifiers := kotlinClassModifiers(class)
	isController := false
	prefix := ""
	hasClassMapping := false

	for _, anno := range classModifiers {
		name, arg := kotlinAnnotationNameAndPath(anno, src)
		if controllerAnnotations[name] {
			isController = true
		}
		if name == "RequestMapping" {
			hasClassMapping = true
			prefix = arg
		}
	}
	if !isController || !hasClassMapping {
		return
	}

	// Find the class_body child.
	var body *sitter.Node
	for i := 0; i < int(class.ChildCount()); i++ {
		ch := class.Child(i)
		if ch.Type() == "class_body" {
			body = ch
			break
		}
	}
	if body == nil {
		return
	}

	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		if ch.Type() != "function_declaration" {
			continue
		}
		// Extract function name from simple_identifier child.
		methodName := ""
		for j := 0; j < int(ch.ChildCount()); j++ {
			gc := ch.Child(j)
			if gc.Type() == "simple_identifier" {
				methodName = nodeText(gc, src)
				break
			}
		}
		if methodName == "" {
			continue
		}

		// Collect method-level annotations from the function's modifiers.
		funcModifiers := kotlinClassModifiers(ch)
		for _, anno := range funcModifiers {
			aname, apath := kotlinAnnotationNameAndPath(anno, src)
			if !verbAnnotations[aname] {
				continue
			}
			verb := httpMethodForAnnotation(aname)
			// For @RequestMapping on a method we also look for an explicit
			// method key (method = [RequestMethod.GET]) to sharpen the verb.
			if aname == "RequestMapping" {
				if mv := kotlinRequestMappingMethod(anno, src); mv != "" {
					verb = mv
				}
			}

			composedPath := joinRoutePaths(prefix, apath)
			canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, composedPath)
			if canonical == "" {
				continue
			}

			id := httproutes.SyntheticID(verb, canonical)
			if seen[id] {
				continue
			}
			seen[id] = true

			// Issue #1725 — populate QualifiedName with the canonical
			// synthetic ID so http_endpoint_definition entities are not
			// empty-qn. Mirrors the producer-side fix in
			// http_endpoint_synthesis.go.
			*entities = append(*entities, types.EntityRecord{
				ID:            id,
				Name:          id,
				QualifiedName: id,
				Kind:          httpEndpointDefinitionKind,
				SourceFile:    path,
				Language:      "kotlin",
				Properties: map[string]string{
					"verb":           verb,
					"path":           canonical,
					"framework":      "spring_mvc",
					"pattern_type":   "ast_driven",
					"source_handler": fmt.Sprintf("Controller:%s", methodName),
					"owning_backend": deriveOwningBackend(path),
				},
				EnrichmentRequired: false,
				EnrichmentStatus:   types.StatusPending,
				QualityScore:       0.8,
			})
			*rels = append(*rels, types.RelationshipRecord{
				FromID: id,
				ToID:   fmt.Sprintf("Controller:%s", methodName),
				Kind:   "ROUTES_TO",
				Properties: map[string]string{
					"framework":    "spring_mvc",
					"pattern_type": "ast_driven",
				},
			})
		}
	}
}

// kotlinClassModifiers returns the annotation children from the modifiers node
// of a class_declaration or function_declaration.
func kotlinClassModifiers(node *sitter.Node) []*sitter.Node {
	if node == nil {
		return nil
	}
	var out []*sitter.Node
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Type() == "modifiers" {
			for j := 0; j < int(ch.ChildCount()); j++ {
				gc := ch.Child(j)
				if gc.Type() == "annotation" {
					out = append(out, gc)
				}
			}
			break // only one modifiers child per declaration
		}
	}
	return out
}

// kotlinAnnotationNameAndPath returns the annotation name and its first
// path-like string literal argument (the positional first arg or the value
// of the `value` or `path` named argument).
//
// Kotlin annotation CST shapes:
//
//	Marker:          annotation → "@" + user_type → type_identifier
//	With args:       annotation → "@" + constructor_invocation → user_type + value_arguments
//	  Positional:    value_argument → string_literal
//	  Named value=:  value_argument → simple_identifier("value") + "=" + string_literal
//	  Named path=:   value_argument → simple_identifier("path") + "=" + string_literal
func kotlinAnnotationNameAndPath(anno *sitter.Node, src []byte) (string, string) {
	if anno == nil || anno.Type() != "annotation" {
		return "", ""
	}

	name := ""
	path := ""

	for i := 0; i < int(anno.ChildCount()); i++ {
		ch := anno.Child(i)
		switch ch.Type() {
		case "user_type":
			// Marker annotation: @RestController, @GetMapping (no args).
			name = kotlinUserTypeName(ch, src)

		case "constructor_invocation":
			// Annotation with args: @RequestMapping("/prefix") or @GetMapping(value="/x").
			// First child of constructor_invocation is the user_type (name).
			for j := 0; j < int(ch.ChildCount()); j++ {
				gc := ch.Child(j)
				switch gc.Type() {
				case "user_type":
					name = kotlinUserTypeName(gc, src)
				case "value_arguments":
					path = kotlinExtractPathFromValueArgs(gc, src)
				}
			}
		}
	}

	// Strip package qualifier if present (e.g. "annotation.GetMapping").
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	return name, path
}

// kotlinUserTypeName returns the type_identifier text from a user_type node,
// handling optional package qualification (the last type_identifier segment).
func kotlinUserTypeName(userType *sitter.Node, src []byte) string {
	if userType == nil {
		return ""
	}
	// user_type may have multiple type_reference children separated by dots.
	// We want the last type_identifier.
	var last string
	for i := 0; i < int(userType.ChildCount()); i++ {
		ch := userType.Child(i)
		if ch.Type() == "type_identifier" {
			last = nodeText(ch, src)
		}
	}
	return last
}

// kotlinExtractPathFromValueArgs extracts the path/value string literal from
// a value_arguments node. Supports:
//   - positional: value_argument → string_literal
//   - named value=: value_argument → simple_identifier("value") + "=" + string_literal
//   - named path=: value_argument → simple_identifier("path") + "=" + string_literal
func kotlinExtractPathFromValueArgs(args *sitter.Node, src []byte) string {
	if args == nil {
		return ""
	}
	var positional, byKey string
	for i := 0; i < int(args.ChildCount()); i++ {
		va := args.Child(i)
		if va.Type() != "value_argument" {
			continue
		}
		// Inspect children of value_argument.
		key := ""
		var strLit *sitter.Node
		for j := 0; j < int(va.ChildCount()); j++ {
			vc := va.Child(j)
			switch vc.Type() {
			case "simple_identifier":
				// This is a named arg key only when followed by "=" — i.e. key comes
				// before any string_literal. Capture the last identifier before "=".
				key = nodeText(vc, src)
			case "string_literal":
				strLit = vc
			}
		}
		if strLit == nil {
			continue
		}
		val := stripKotlinStringLiteral(nodeText(strLit, src))
		if key == "" {
			// Positional argument.
			if positional == "" {
				positional = val
			}
		} else if key == "value" || key == "path" {
			// Named value= or path= argument.
			byKey = val
		}
	}
	if byKey != "" {
		return byKey
	}
	return positional
}

// kotlinRequestMappingMethod extracts the HTTP verb from a @RequestMapping
// annotation's `method = [RequestMethod.GET]` (or similar) argument.
// Returns an empty string when the argument is absent (caller keeps "ANY").
func kotlinRequestMappingMethod(anno *sitter.Node, src []byte) string {
	if anno == nil {
		return ""
	}
	// Navigate: annotation → constructor_invocation → value_arguments → value_argument with key "method"
	for i := 0; i < int(anno.ChildCount()); i++ {
		ci := anno.Child(i)
		if ci.Type() != "constructor_invocation" {
			continue
		}
		for j := 0; j < int(ci.ChildCount()); j++ {
			gc := ci.Child(j)
			if gc.Type() != "value_arguments" {
				continue
			}
			for k := 0; k < int(gc.ChildCount()); k++ {
				va := gc.Child(k)
				if va.Type() != "value_argument" {
					continue
				}
				key := ""
				for l := 0; l < int(va.ChildCount()); l++ {
					vc := va.Child(l)
					if vc.Type() == "simple_identifier" {
						key = nodeText(vc, src)
					}
					if key == "method" && (vc.Type() == "collection_literal" || vc.Type() == "string_literal") {
						raw := nodeText(vc, src)
						return extractMethodFromCollectionLiteral(raw)
					}
				}
			}
		}
	}
	return ""
}

// extractMethodFromCollectionLiteral extracts the HTTP verb from a Kotlin
// collection_literal like `[RequestMethod.GET]` or `[RequestMethod.POST]`.
// Returns the verb in uppercase or "" when none is recognized.
func extractMethodFromCollectionLiteral(raw string) string {
	verbs := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
	upper := strings.ToUpper(raw)
	for _, v := range verbs {
		if strings.Contains(upper, v) {
			return v
		}
	}
	return ""
}

// stripKotlinStringLiteral removes surrounding double quotes from a Kotlin
// string literal token. Falls back to returning the input unchanged.
func stripKotlinStringLiteral(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
