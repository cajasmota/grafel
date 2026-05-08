// AST-driven Spring MVC route composition.
//
// The YAML rule engine treats `@RequestMapping("/api")` and `@GetMapping("/orders")`
// as independent regex matches and emits two orphan Route entities — `Route:/api`
// and `Route:/orders`. The real HTTP route is `/api/orders`: the class-level
// prefix composes with each method-level path. Regex-only YAML rules can't do
// that composition because they don't see lexical scope.
//
// This pass walks the tree-sitter Java CST, finds `@RestController` /
// `@Controller` classes carrying a class-level `@RequestMapping`, and emits
// composed `Route:<prefix><method_path>` entities plus the matching
// `ROUTES_TO` relationships. The pass also reports the bare paths it
// "claimed" so the surrounding engine can suppress the duplicate flat Routes
// the YAML rules would otherwise emit (and drop the class-level orphan).
//
// Refs #67.
package engine

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/treesitter"
	"github.com/cajasmota/archigraph/internal/types"
)

// verbAnnotations is the set of method-level Spring MVC mapping annotations
// that this pass composes with the enclosing class-level @RequestMapping
// prefix. @RequestMapping is included so handlers that use the legacy form
// (e.g. `@RequestMapping(value = "/legacy", method = RequestMethod.GET)`)
// also compose correctly.
var verbAnnotations = map[string]bool{
	"GetMapping":     true,
	"PostMapping":    true,
	"PutMapping":     true,
	"DeleteMapping":  true,
	"PatchMapping":   true,
	"RequestMapping": true,
}

// controllerAnnotations marks classes that should be treated as HTTP
// controllers. Plain `@Component` / `@Service` classes are excluded.
var controllerAnnotations = map[string]bool{
	"RestController": true,
	"Controller":     true,
}

// composedSpringRoutes holds the output of the Spring AST pass.
type composedSpringRoutes struct {
	// entities are the composed Route entity records (one per handler).
	entities []types.EntityRecord
	// relationships are the composed ROUTES_TO records pointing to the
	// handler method's Controller entity.
	relationships []types.RelationshipRecord
	// claimedMethodPaths is the set of bare method paths that this pass
	// consumed (e.g. "/orders", "/orders/{id}"). The caller uses this to
	// drop the duplicate orphan Route entities the YAML rules emitted for
	// the same paths inside the same controller class.
	claimedMethodPaths map[string]bool
	// claimedHandlerMethods is the set of handler method names whose
	// ROUTES_TO edge this pass replaced. Used to drop the orphan YAML
	// ROUTES_TO edges that point at uncomposed source Routes.
	claimedHandlerMethods map[string]bool
	// claimedClassPrefixes is the set of class-level @RequestMapping
	// prefixes consumed (e.g. "/api"). Used to drop the orphan class-level
	// Route entity the YAML rules emit from the bare class annotation.
	claimedClassPrefixes map[string]bool
}

// applySpringRouteComposition runs the Spring AST pass on a Java file and
// merges its output with the YAML rules' raw entities/relationships,
// dropping the now-redundant flat Routes and orphan class-level Route.
//
// `lang` lets the engine no-op cleanly for non-Java files.
func applySpringRouteComposition(
	ctx context.Context,
	lang string,
	path string,
	content []byte,
	rawEntities []types.EntityRecord,
	rawRels []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if lang != "java" || len(content) == 0 {
		return rawEntities, rawRels
	}
	if !bytesContainsAny(content, "@RestController", "@Controller") {
		return rawEntities, rawRels
	}

	composed, ok := extractSpringComposedRoutes(ctx, path, content)
	if !ok || len(composed.entities) == 0 {
		return rawEntities, rawRels
	}

	// Drop YAML Route entities whose Name matches a claimed bare method path
	// (we replaced them with the composed version) or matches a claimed
	// class-level prefix (orphan from the bare class annotation).
	filteredEntities := rawEntities[:0:0]
	for _, e := range rawEntities {
		if e.Kind == "Route" && e.SourceFile == path {
			if composed.claimedMethodPaths[e.Name] || composed.claimedClassPrefixes[e.Name] {
				continue
			}
		}
		filteredEntities = append(filteredEntities, e)
	}
	filteredEntities = append(filteredEntities, composed.entities...)

	// Drop YAML ROUTES_TO edges whose target controller method we replaced.
	// The YAML version's FromID is `Route:<bare_path>`; we replaced it with
	// `Route:<prefix><bare_path>`.
	filteredRels := rawRels[:0:0]
	for _, r := range rawRels {
		if r.Kind == "ROUTES_TO" && strings.HasPrefix(r.ToID, "Controller:") {
			method := strings.TrimPrefix(r.ToID, "Controller:")
			if composed.claimedHandlerMethods[method] {
				continue
			}
		}
		filteredRels = append(filteredRels, r)
	}
	filteredRels = append(filteredRels, composed.relationships...)

	return filteredEntities, filteredRels
}

// bytesContainsAny is a cheap pre-filter to avoid parsing files that
// obviously can't contain a Spring controller.
func bytesContainsAny(content []byte, needles ...string) bool {
	s := string(content)
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// extractSpringComposedRoutes parses the Java source, walks the CST, and
// returns composed Spring routes for every HTTP controller class that
// carries a class-level @RequestMapping prefix.
func extractSpringComposedRoutes(ctx context.Context, path string, content []byte) (composedSpringRoutes, bool) {
	out := composedSpringRoutes{
		claimedMethodPaths:    map[string]bool{},
		claimedHandlerMethods: map[string]bool{},
		claimedClassPrefixes:  map[string]bool{},
	}

	factory := treesitter.NewParserFactory(nil)
	pr, err := factory.Parse(ctx, content, "java")
	if err != nil || pr == nil || pr.Tree == nil {
		return out, false
	}

	root := pr.Tree.RootNode()
	walkSpringClasses(root, content, path, &out)
	return out, true
}

// walkSpringClasses traverses the tree, looking for class_declaration nodes
// that carry both a controller annotation and a class-level @RequestMapping.
func walkSpringClasses(node *sitter.Node, src []byte, path string, out *composedSpringRoutes) {
	if node == nil {
		return
	}
	if node.Type() == "class_declaration" {
		processSpringClass(node, src, path, out)
		// Continue recursing — nested classes may also be controllers.
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		walkSpringClasses(node.Child(i), src, path, out)
	}
}

// processSpringClass inspects a class_declaration. If it is a Spring HTTP
// controller (carries @RestController or @Controller) AND has a class-level
// @RequestMapping prefix, every method-level mapping inside its body is
// emitted as a composed Route.
func processSpringClass(class *sitter.Node, src []byte, path string, out *composedSpringRoutes) {
	annos := classLevelAnnotations(class)
	isController := false
	prefix := ""
	hasClassMapping := false
	for _, a := range annos {
		name, arg := annotationNameAndPath(a, src)
		if controllerAnnotations[name] {
			isController = true
		}
		if name == "RequestMapping" {
			hasClassMapping = true
			prefix = arg // may be ""
		}
	}
	if !isController || !hasClassMapping {
		return
	}

	out.claimedClassPrefixes[prefix] = true

	body := class.ChildByFieldName("body")
	if body == nil {
		return
	}

	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		if ch.Type() != "method_declaration" {
			continue
		}
		methodName := nodeFieldText(ch, "name", src)
		if methodName == "" {
			continue
		}
		for _, a := range methodLevelAnnotations(ch) {
			aname, apath := annotationNameAndPath(a, src)
			if !verbAnnotations[aname] {
				continue
			}
			if apath == "" {
				// Verb annotation with no path arg (e.g. @GetMapping
				// alone) — composes to the prefix itself.
				apath = ""
			}
			composedPath := joinRoutePaths(prefix, apath)

			out.claimedMethodPaths[apath] = true
			out.claimedHandlerMethods[methodName] = true

			out.entities = append(out.entities, types.EntityRecord{
				Name:       composedPath,
				Kind:       "Route",
				SourceFile: path,
				Language:   "java",
				Properties: map[string]string{
					"framework":    "java",
					"pattern_type": "ast_driven",
					"http_method":  httpMethodForAnnotation(aname),
				},
				EnrichmentRequired: false,
				EnrichmentStatus:   types.StatusPending,
				QualityScore:       0.7,
			})
			out.relationships = append(out.relationships, types.RelationshipRecord{
				FromID: fmt.Sprintf("Route:%s", composedPath),
				ToID:   fmt.Sprintf("Controller:%s", methodName),
				Kind:   "ROUTES_TO",
				Properties: map[string]string{
					"framework":    "java",
					"pattern_type": "ast_driven",
				},
			})
		}
	}
}

// classLevelAnnotations returns the modifier annotations attached to a
// class_declaration (the modifiers child holds them).
func classLevelAnnotations(class *sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	for i := 0; i < int(class.ChildCount()); i++ {
		ch := class.Child(i)
		if ch.Type() != "modifiers" {
			continue
		}
		for j := 0; j < int(ch.ChildCount()); j++ {
			gc := ch.Child(j)
			if gc.Type() == "marker_annotation" || gc.Type() == "annotation" {
				out = append(out, gc)
			}
		}
	}
	return out
}

// methodLevelAnnotations is the same shape as classLevelAnnotations, applied
// to a method_declaration.
func methodLevelAnnotations(method *sitter.Node) []*sitter.Node {
	return classLevelAnnotations(method)
}

// annotationNameAndPath returns the annotation's bare name (e.g. "GetMapping")
// and, if present, the first string-literal argument's value (e.g. "/orders").
// Supports `@Foo("/x")`, `@Foo(value="/x")`, `@Foo(path="/x")`, and bare
// `@Foo` (returns empty path).
func annotationNameAndPath(anno *sitter.Node, src []byte) (string, string) {
	if anno == nil {
		return "", ""
	}
	name := nodeFieldText(anno, "name", src)
	// Strip a leading package qualifier like
	// `org.springframework.web.bind.annotation.GetMapping`.
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	args := anno.ChildByFieldName("arguments")
	if args == nil {
		return name, ""
	}
	// arguments node = annotation_argument_list. Its children include
	// either a single string_literal (positional value) or a list of
	// element_value_pair nodes (named args). Walk and pick the first
	// string we see, preferring `value` / `path` keys.
	var positional, byKey string
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		switch ch.Type() {
		case "string_literal":
			if positional == "" {
				positional = stripStringLiteral(nodeText(ch, src))
			}
		case "element_value_pair":
			key := nodeFieldText(ch, "key", src)
			val := ch.ChildByFieldName("value")
			if val == nil {
				continue
			}
			// The value may itself be a string_literal or wrap one.
			if val.Type() == "string_literal" {
				if key == "value" || key == "path" {
					byKey = stripStringLiteral(nodeText(val, src))
				}
			}
		}
	}
	if byKey != "" {
		return name, byKey
	}
	return name, positional
}

// stripStringLiteral removes the surrounding quotes from a Java string
// literal token. Falls back to returning the input unchanged if quotes are
// missing.
func stripStringLiteral(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// joinRoutePaths concatenates a class-level prefix with a method-level
// path, normalising the slash boundary so we don't produce `/api//orders`
// or `/apiorders`. An empty prefix returns the method path verbatim; an
// empty method path returns the prefix verbatim.
func joinRoutePaths(prefix, method string) string {
	switch {
	case prefix == "":
		return method
	case method == "":
		return prefix
	case strings.HasSuffix(prefix, "/") && strings.HasPrefix(method, "/"):
		return prefix + strings.TrimPrefix(method, "/")
	case !strings.HasSuffix(prefix, "/") && !strings.HasPrefix(method, "/"):
		return prefix + "/" + method
	default:
		return prefix + method
	}
}

// httpMethodForAnnotation maps a Spring verb annotation name to its HTTP
// method label. @RequestMapping is method-agnostic and reports "ANY".
func httpMethodForAnnotation(name string) string {
	switch name {
	case "GetMapping":
		return "GET"
	case "PostMapping":
		return "POST"
	case "PutMapping":
		return "PUT"
	case "DeleteMapping":
		return "DELETE"
	case "PatchMapping":
		return "PATCH"
	default:
		return "ANY"
	}
}

// nodeText returns the source text covered by node.
func nodeText(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	return string(src[n.StartByte():n.EndByte()])
}

// nodeFieldText returns the text of node.ChildByFieldName(field), or "" if
// the field is absent.
func nodeFieldText(n *sitter.Node, field string, src []byte) string {
	if n == nil {
		return ""
	}
	c := n.ChildByFieldName(field)
	if c == nil {
		return ""
	}
	return nodeText(c, src)
}
