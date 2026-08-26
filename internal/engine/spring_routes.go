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

	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/types"
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
	// claimedHandlerEdges is the set of YAML ROUTES_TO edges this pass
	// replaced, keyed by the QUALIFYING PAIR (bare annotation path, handler
	// method name) — see springHandlerClaimKey. Used to drop the orphan YAML
	// ROUTES_TO edges that point at uncomposed source Routes.
	//
	// #6498 — this was keyed on the bare method name alone and scoped to the
	// file, so two controller classes in one file sharing a handler method
	// name collided: the first class's claim suppressed the second class's
	// legitimate edge, silently deleting a real endpoint from the graph. The
	// YAML edge carries no class (its ToID is `Controller:<bareMethod>`), so
	// the class-distinguishing information available on BOTH sides is the
	// annotation path the edge's FromID (`Route:<barePath>`) is built from.
	claimedHandlerEdges map[string]bool
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
func applySpringRouteComposition(args DetectorPassArgs) DetectorPassResult {
	ctx := args.Ctx
	lang := args.Lang
	path := args.Path
	content := args.Content
	rawEntities := args.Entities
	rawRels := args.Relationships
	if lang != "java" || len(content) == 0 {
		return DetectorPassResult{Entities: rawEntities, Relationships: rawRels}
	}
	if !bytesContainsAny(content, "@RestController", "@Controller") {
		return DetectorPassResult{Entities: rawEntities, Relationships: rawRels}
	}

	composed, ok := extractSpringComposedRoutes(ctx, path, content)
	if !ok || len(composed.entities) == 0 {
		return DetectorPassResult{Entities: rawEntities, Relationships: rawRels}
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
			// No `HasPrefix(r.FromID, "Route:")` guard: every spring_mvc.yaml
			// ROUTES_TO rule declares source_type Route, so the prefix is
			// always present. On a hypothetical edge without it TrimPrefix is
			// a no-op and the resulting key cannot match a claim (claims are
			// keyed on an annotation path), so the guard would be an
			// unreachable, untestable branch.
			barePath := strings.TrimPrefix(r.FromID, "Route:")
			if composed.claimedHandlerEdges[springHandlerClaimKey(barePath, method)] {
				continue
			}
		}
		filteredRels = append(filteredRels, r)
	}
	filteredRels = append(filteredRels, composed.relationships...)

	return DetectorPassResult{Entities: filteredEntities, Relationships: filteredRels}
}

// springHandlerClaimKey builds the key for claimedHandlerEdges: the bare
// annotation path the YAML rules matched, paired with the handler method name.
// Both halves are required — see the claimedHandlerEdges doc comment (#6498).
//
// The two halves are separated rather than concatenated, because bare
// concatenation is ambiguous: ("/ab","c") and ("/a","bc") both flatten to
// "/abc", which would let one class's claim swallow a sibling's only edge.
// TestDetect_SpringRoute_ConcatAmbiguousClaimKey pins exactly that. The
// separator must be a byte that appears in neither half; NUL is one such byte
// for the halves this pass produces, but the test observes the separation, not
// the choice of NUL.
func springHandlerClaimKey(barePath, methodName string) string {
	return barePath + "\x00" + methodName
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
		claimedMethodPaths:   map[string]bool{},
		claimedHandlerEdges:  map[string]bool{},
		claimedClassPrefixes: map[string]bool{},
	}

	factory := treesitter.NewParserFactory(nil)
	pr, err := factory.Parse(ctx, content, "java")
	if err != nil || pr == nil || pr.TSTree == nil {
		return out, false
	}
	// #5954 — tree-sitter trees are CGo-allocated (~19.7 B of C heap per
	// source byte) and go-tree-sitter@v0.24.0 installs no finalizer, so an
	// un-Close()d tree leaks for the life of the process. `out` holds only
	// plain records/strings, never a ts.Node, so closing on return is safe.
	// Same pattern as http_endpoint_client_ast.go.
	defer pr.TSTree.Close()

	root := pr.TSTree.RootNode()
	walkSpringClasses(root, content, path, &out)
	return out, true
}

// walkSpringClasses traverses the tree, looking for class_declaration nodes
// that carry both a controller annotation and a class-level @RequestMapping.
func walkSpringClasses(node ts.Node, src []byte, path string, out *composedSpringRoutes) {
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
func processSpringClass(class ts.Node, src []byte, path string, out *composedSpringRoutes) {
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

	// #6429 — the enclosing class name. The Java symbol extractor lands each
	// handler method QUALIFIED (`OrderController.listOrders`, kind
	// SCOPE.Operation); carrying the class name here is what lets the
	// ROUTES_TO edge below target that real entity instead of a
	// `Controller:<bareMethod>` stub nothing ever resolved, and what lets
	// synthesizeSpringFromComposed qualify `source_handler` so the endpoint
	// binder is unambiguous by construction rather than by file:line rescue.
	className := nodeFieldText(class, "name", src)
	if className == "" {
		// Unreachable for a well-formed class_declaration (see the ROUTES_TO
		// comment below); bail rather than emit `SCOPE.Operation:.method`.
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
			// #6702/F1 — also claim the literal the YAML relationship regex
			// actually captures (the LAST one in the argument list), which is
			// not always the one the AST picked as the path. Both claims are
			// bound to THIS annotation's own handler method: claiming a
			// literal against any other method in the class would let one
			// handler's path suppress a sibling controller's edge (finding A,
			// pinned by TestDetect_SpringRoute_LiteralClaimIsBoundToItsOwnMethod).
			// `apath` is claimed explicitly so the `apath == ""` (bare
			// `@GetMapping`) case still registers.
			out.claimedHandlerEdges[springHandlerClaimKey(apath, methodName)] = true
			if lit, ok := annotationLastStringLiteral(a, src); ok {
				out.claimedHandlerEdges[springHandlerClaimKey(lit, methodName)] = true
			}

			routeProps := map[string]string{
				"framework":    "java",
				"pattern_type": "ast_driven",
				"http_method":  httpMethodForAnnotation(aname),
			}
			// Surface path-variable names (e.g. {id}, {userId}) so the graph
			// records which segments are dynamic without losing the template form.
			if pathParams := extractRoutePathParams(composedPath); pathParams != "" {
				routeProps["path_params"] = pathParams
			}
			// #6429 — carry the handler identity forward on the Route entity so
			// synthesizeSpringFromComposed (which only sees the emitted Route
			// records, not the AST) can stamp `source_handler` at the handler
			// instead of at the route's own path. Kotlin already did this
			// inline in spring_routes_kotlin.go; Java discarded it.
			routeProps["handler_method"] = methodName
			routeProps["handler_class"] = className
			out.entities = append(out.entities, types.EntityRecord{
				Name:               composedPath,
				Kind:               "Route",
				SourceFile:         path,
				Language:           "java",
				Properties:         routeProps,
				EnrichmentRequired: false,
				EnrichmentStatus:   types.StatusPending,
				QualityScore:       0.7,
			})
			// #6429 — target the handler Operation the Java extractor really
			// emits (`SCOPE.Operation:<Class>.<method>`). The old
			// `Controller:<method>` stub matched no entity kind in the graph,
			// so the route→handler hop dangled and refs.go accounted it as
			// runtime-dynamic.
			//
			// No className=="" fallback: walkSpringClasses only descends into
			// `class_declaration`, and a Java class_declaration always carries
			// a `name` field (an anonymous class parses as
			// object_creation_expression and never reaches here). A fallback
			// branch would be unreachable and untestable.
			out.relationships = append(out.relationships, types.RelationshipRecord{
				FromID: fmt.Sprintf("Route:%s", composedPath),
				ToID:   fmt.Sprintf("SCOPE.Operation:%s.%s", className, methodName),
				Kind:   "ROUTES_TO",
				Properties: types.Props{
					{K: "framework", V: "java"},
					{K: "pattern_type", V: "ast_driven"},
				},
			})
		}
	}
}

// classLevelAnnotations returns the modifier annotations attached to a
// class_declaration (the modifiers child holds them).
func classLevelAnnotations(class ts.Node) []ts.Node {
	var out []ts.Node
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
func methodLevelAnnotations(method ts.Node) []ts.Node {
	return classLevelAnnotations(method)
}

// annotationNameAndPath returns the annotation's bare name (e.g. "GetMapping")
// and, if present, the first string-literal argument's value (e.g. "/orders").
// Supports `@Foo("/x")`, `@Foo(value="/x")`, `@Foo(path="/x")`, and bare
// `@Foo` (returns empty path).
func annotationNameAndPath(anno ts.Node, src []byte) (string, string) {
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

// annotationLastStringLiteral returns the LAST string literal in the
// annotation's argument list, which is the only literal the spring_mvc.yaml
// relationship rules can ever capture.
//
// #6702/F1 — the AST side and the YAML side disagree about which literal is
// the path: annotationNameAndPath prefers the `value=`/`path=` pair, while each
// of the six relationship rules is `[^)]*["']([^"'\n\r]+)["'][^)]*\)` —
// greedy, one capture per annotation, always the LAST literal. On
// `@PostMapping(value = "/things", consumes = "application/json")` that is the
// media type, so the YAML edge is `Route:application/json -> Controller:create`.
// Claiming only the AST's preferred literal left that edge unsuppressed —
// dangling on both ends, since no `Route:application/json` entity exists (the
// entity rule anchors on the FIRST literal) and `Controller:` stubs resolve to
// nothing (#6429).
//
// #6702/finding B — this returns the last literal, NOT every literal. The
// regex emits exactly one edge per annotation, so any earlier literal is
// claimed for something that was never emitted. For
// `@GetMapping(value = {"/a", "/b"})` only `/b` is reachable; claiming `/a` as
// well would swallow a sibling controller's genuine `/a` edge, which is #6498
// re-entering through this widening. Pinned by
// TestDetect_SpringRoute_ArrayValueClaimsOnlyTheReachableLiteral.
//
// The walk is generic rather than node-type-driven so it reaches literals
// nested inside an `element_value_array_initializer` (tree-sitter-java's node
// type for the `{...}` in `value = {"/a", "/b"}`) without naming it.
func annotationLastStringLiteral(anno ts.Node, src []byte) (string, bool) {
	if anno == nil {
		return "", false
	}
	args := anno.ChildByFieldName("arguments")
	if args == nil {
		return "", false
	}
	last, found := "", false
	var walk func(n ts.Node)
	walk = func(n ts.Node) {
		if n == nil {
			return
		}
		if n.Type() == "string_literal" {
			last, found = stripStringLiteral(nodeText(n, src)), true
			return
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(args)
	return last, found
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
func nodeText(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	return string(src[n.StartByte():n.EndByte()])
}

// extractRoutePathParams returns a comma-separated list of path-variable names
// found in the URL template (e.g. "/api/users/{id}/orders/{orderId}" →
// "id,orderId"). Returns an empty string when the path has no path variables.
func extractRoutePathParams(path string) string {
	var params []string
	inBrace := false
	start := 0
	for i := 0; i < len(path); i++ {
		switch path[i] {
		case '{':
			inBrace = true
			start = i + 1
		case '}':
			if inBrace {
				// Strip optional regex constraint after ':'.
				token := path[start:i]
				if j := strings.IndexByte(token, ':'); j >= 0 {
					token = token[:j]
				}
				if token != "" {
					params = append(params, token)
				}
				inBrace = false
			}
		}
	}
	return strings.Join(params, ",")
}

// nodeFieldText returns the text of node.ChildByFieldName(field), or "" if
// the field is absent.
func nodeFieldText(n ts.Node, field string, src []byte) string {
	if n == nil {
		return ""
	}
	c := n.ChildByFieldName(field)
	if c == nil {
		return ""
	}
	return nodeText(c, src)
}
