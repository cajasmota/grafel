// Java JAX-RS / Spring MVC annotation route composition pass.
//
// Problem: Java REST controllers compose their full HTTP routes from
// class-level + method-level annotations:
//
//	@Path("/products")              // JAX-RS class-level
//	public class ProductsController {
//	    @GET
//	    @Path("/{id}")              // JAX-RS method-level
//	    public Product get(...) {}
//	}
//
//	@RequestMapping("/api/users")   // Spring class-level
//	@RestController
//	public class UserController {
//	    @GetMapping("/{id}")        // Spring method-level
//	    public User get(...) {}
//	}
//
// Today archigraph emits SCOPE.Operation entities for each handler method
// but the existing JAX-RS regex in http_endpoint_synthesis.go is too strict
// (requires a tight annotation/class layout) and emits source_handler
// references pointing at a Kind that doesn't exist in the entity table
// (`Controller:method`), so all synthetics get dropped by
// ResolveHTTPEndpointHandlers.
//
// This pass runs AFTER Pass 3 with the full classified file set. It:
//
//  1. Scans every .java file for the class-level @Path or @RequestMapping.
//  2. For every method-level HTTP verb annotation, composes the full route.
//  3. Emits one `http_endpoint` synthetic entity per (verb, path) pair,
//     with `source_handler` set to `SCOPE.Operation:<Class>.<method>` so
//     ResolveHTTPEndpointHandlers can resolve it against the real
//     SCOPE.Operation entity emitted by the Java extractor.
//
// JAX-RS verb annotations recognised:
//
//	@GET @POST @PUT @DELETE @PATCH @HEAD @OPTIONS
//
// Spring verb annotations recognised:
//
//	@GetMapping @PostMapping @PutMapping @DeleteMapping @PatchMapping
//	@RequestMapping(method = RequestMethod.X)
//
// Refs #657.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

// JavaAnnotationFileReader returns the source bytes for a repo-relative
// path, or nil if the file is unavailable.
type JavaAnnotationFileReader func(relPath string) []byte

// javaClassDeclRe matches a Java class declaration and captures the class
// name. We do not anchor on visibility because the parsed Java sources we
// emit synthetics for in archigraph use both `public class` and bare
// `class` declarations.
var javaClassDeclRe = regexp.MustCompile(`(?m)^\s*(?:public\s+|abstract\s+|final\s+|static\s+)*class\s+(\w+)`)

// javaPathAnnotationRe matches @Path("value") OR @Path(value = "..."). The
// captured group is the raw path string.
var javaPathAnnotationRe = regexp.MustCompile(`@Path\s*\(\s*(?:value\s*=\s*)?"([^"]*)"\s*\)`)

// javaRequestMappingRe matches a Spring class-level OR method-level
// @RequestMapping. Captures the entire argument list so we can extract
// both the path and (optionally) the method= keyword.
var javaRequestMappingRe = regexp.MustCompile(`@RequestMapping\s*\(([^)]*)\)`)

// javaSpringVerbMappingRe matches @GetMapping("/x") / @PostMapping(...) etc.
// Captures the verb keyword (group 1) and the argument list (group 2).
var javaSpringVerbMappingRe = regexp.MustCompile(`@(Get|Post|Put|Delete|Patch)Mapping\s*(?:\(([^)]*)\))?`)

// javaJAXRSVerbRe matches a bare JAX-RS verb annotation. Captures the verb.
var javaJAXRSVerbRe = regexp.MustCompile(`@(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b`)

// javaStringArgRe extracts the first quoted string from an argument list
// (covers both `"/foo"` and `value = "/foo"`).
var javaStringArgRe = regexp.MustCompile(`"([^"]*)"`)

// javaMethodMethodArgRe captures the `method = RequestMethod.X` keyword in
// a @RequestMapping argument list.
var javaMethodMethodArgRe = regexp.MustCompile(`method\s*=\s*(?:RequestMethod\s*\.\s*)?(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)`)

// javaConsumesRe / javaProducesRe capture content-type metadata.
var javaConsumesRe = regexp.MustCompile(`@Consumes\s*\(([^)]+)\)`)
var javaProducesRe = regexp.MustCompile(`@Produces\s*\(([^)]+)\)`)

// javaMethodDeclRe matches the start of a Java method declaration so we
// can extract the method name following a block of annotations. We scan
// line-by-line in the file walker; this regex matches one method-decl line.
//
// We accept: modifiers, generic return type, method-name, opening paren.
var javaMethodDeclRe = regexp.MustCompile(`^\s*(?:public|protected|private|static|final|abstract|synchronized|default|\s)+[\w<>\[\],.\s?]+?\s+(\w+)\s*\(`)

// ApplyJavaAnnotationRoutes scans the supplied Java files for JAX-RS or
// Spring MVC annotation patterns and returns a slice of synthetic
// http_endpoint EntityRecords. Caller appends these to the existing
// entity slice; ResolveHTTPEndpointHandlers wires them to the matching
// SCOPE.Operation handlers.
func ApplyJavaAnnotationRoutes(
	javaFiles []string,
	fileReader JavaAnnotationFileReader,
) []types.EntityRecord {
	var out []types.EntityRecord
	seen := map[string]bool{}

	for _, relPath := range javaFiles {
		if !strings.HasSuffix(relPath, ".java") {
			continue
		}
		content := fileReader(relPath)
		if len(content) == 0 {
			continue
		}
		src := string(content)
		// Cheap pre-filter: skip files that have no HTTP annotation.
		if !containsAnyHTTPAnnotation(src) {
			continue
		}

		for _, ep := range extractJavaEndpoints(src, relPath) {
			if seen[ep.ID] {
				continue
			}
			seen[ep.ID] = true
			out = append(out, ep)
		}
	}
	return out
}

// containsAnyHTTPAnnotation reports whether the source likely contains
// JAX-RS or Spring MVC route annotations.
func containsAnyHTTPAnnotation(src string) bool {
	switch {
	case strings.Contains(src, "@Path("):
		return true
	case strings.Contains(src, "@RequestMapping"):
		return true
	case strings.Contains(src, "@GetMapping"),
		strings.Contains(src, "@PostMapping"),
		strings.Contains(src, "@PutMapping"),
		strings.Contains(src, "@DeleteMapping"),
		strings.Contains(src, "@PatchMapping"):
		return true
	case strings.Contains(src, "@GET"),
		strings.Contains(src, "@POST"),
		strings.Contains(src, "@PUT"),
		strings.Contains(src, "@DELETE"),
		strings.Contains(src, "@PATCH"):
		return true
	}
	return false
}

// classFrame holds per-class state during file scan: the class name (for
// handler-reference composition), the class-level path prefix, and
// class-level content-type metadata that method-level routes inherit.
type classFrame struct {
	name           string
	prefix         string
	framework      string // "jaxrs" or "spring" (best-effort)
	classConsumes  string
	classProduces  string
}

// extractJavaEndpoints walks a Java source file and returns the synthetic
// http_endpoint records for every annotated handler method.
//
// Strategy:
//   - Split file into lines.
//   - Maintain a per-class frame. When `class X` is encountered, capture
//     the most recent annotation block above it as the class prefix.
//   - When a method declaration is found, gather the immediately preceding
//     annotation block, parse its verb + optional method-level path, and
//     compose the full route against the current class frame.
//
// This deliberately uses a lightweight line-oriented parser rather than
// the tree-sitter AST because the Java extractor strips annotation
// arguments before producing entities (so we can't reuse extractor output).
func extractJavaEndpoints(src, relPath string) []types.EntityRecord {
	lines := strings.Split(src, "\n")

	var out []types.EntityRecord
	var cur classFrame
	// Buffer of consecutive annotation/blank lines immediately above the
	// current code line. Cleared by any non-annotation, non-blank line.
	var annoBuf []string

	flushAnnoBuf := func() []string {
		buf := annoBuf
		annoBuf = nil
		return buf
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track annotation lines (and blank lines, which can occur between
		// annotations in the wild).
		if strings.HasPrefix(trimmed, "@") || trimmed == "" {
			annoBuf = append(annoBuf, trimmed)
			continue
		}
		// Comment lines: ignore but do not reset annoBuf (devs sometimes
		// put a comment between annotations and the declaration).
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "/*") {
			continue
		}

		// Class declaration?
		if m := javaClassDeclRe.FindStringSubmatch(line); m != nil {
			classAnnos := flushAnnoBuf()
			cur = buildClassFrame(m[1], classAnnos)
			continue
		}

		// Method declaration?
		if m := javaMethodDeclRe.FindStringSubmatch(line); m != nil {
			methodName := m[1]
			methodAnnos := flushAnnoBuf()
			if cur.name == "" {
				// Method declared before any class header (shouldn't happen
				// in valid Java but harmless to skip).
				continue
			}
			eps := buildMethodEndpoints(cur, methodName, methodAnnos, relPath)
			out = append(out, eps...)
			continue
		}

		// Any other code line resets the annotation buffer.
		flushAnnoBuf()
	}
	return out
}

// buildClassFrame parses the annotation block immediately above a class
// declaration and produces the per-class state used when emitting method
// routes.
func buildClassFrame(className string, annos []string) classFrame {
	cf := classFrame{name: className}
	joined := strings.Join(annos, "\n")

	// JAX-RS class-level @Path.
	if m := javaPathAnnotationRe.FindStringSubmatch(joined); m != nil {
		cf.prefix = m[1]
		cf.framework = "jaxrs"
	}
	// Spring class-level @RequestMapping. Path may be the bare quoted
	// arg or `value = "..."`.
	if m := javaRequestMappingRe.FindStringSubmatch(joined); m != nil {
		if sm := javaStringArgRe.FindStringSubmatch(m[1]); sm != nil {
			cf.prefix = sm[1]
			cf.framework = "spring"
		}
	}
	// Class-level Consumes/Produces — captured as raw substring so we
	// can surface as property when emitting method routes.
	if m := javaConsumesRe.FindStringSubmatch(joined); m != nil {
		cf.classConsumes = strings.TrimSpace(m[1])
	}
	if m := javaProducesRe.FindStringSubmatch(joined); m != nil {
		cf.classProduces = strings.TrimSpace(m[1])
	}
	// Spring @RestController / @Controller signals Spring framework even
	// when @RequestMapping is absent at the class level (some endpoints
	// rely on method-level @GetMapping alone).
	if cf.framework == "" {
		if strings.Contains(joined, "@RestController") || strings.Contains(joined, "@Controller") {
			cf.framework = "spring"
		}
	}
	return cf
}

// buildMethodEndpoints walks the annotation block above a method
// declaration and produces one or more http_endpoint records.
func buildMethodEndpoints(cf classFrame, methodName string, annos []string, relPath string) []types.EntityRecord {
	joined := strings.Join(annos, "\n")

	// Collect method-level paths (may be empty).
	methodPath := ""
	if m := javaPathAnnotationRe.FindStringSubmatch(joined); m != nil {
		methodPath = m[1]
	}

	// Collect verbs.
	var verbs []string
	// JAX-RS bare verbs.
	for _, m := range javaJAXRSVerbRe.FindAllStringSubmatch(joined, -1) {
		verbs = append(verbs, strings.ToUpper(m[1]))
	}
	// Spring specialised mappings (@GetMapping, etc.).
	for _, m := range javaSpringVerbMappingRe.FindAllStringSubmatch(joined, -1) {
		verb := strings.ToUpper(m[1])
		verbs = append(verbs, verb)
		// If the specialised mapping carries an inline path arg, use it.
		if methodPath == "" && len(m) > 2 && m[2] != "" {
			if sm := javaStringArgRe.FindStringSubmatch(m[2]); sm != nil {
				methodPath = sm[1]
			}
		}
	}
	// Method-level @RequestMapping. Captures the verb from the `method=...`
	// keyword (if any) and the path from the first quoted string.
	for _, m := range javaRequestMappingRe.FindAllStringSubmatch(joined, -1) {
		args := m[1]
		// Path.
		if methodPath == "" {
			if sm := javaStringArgRe.FindStringSubmatch(args); sm != nil {
				methodPath = sm[1]
			}
		}
		// Verb(s). If method= is absent the mapping accepts ANY verb.
		methodVerbs := parseRequestMappingMethods(args)
		if len(methodVerbs) == 0 {
			methodVerbs = []string{"ANY"}
		}
		verbs = append(verbs, methodVerbs...)
	}

	if len(verbs) == 0 {
		// No verb annotation found — not a route.
		return nil
	}

	// Method-level Consumes/Produces (override class-level when present).
	methodConsumes := cf.classConsumes
	methodProduces := cf.classProduces
	if m := javaConsumesRe.FindStringSubmatch(joined); m != nil {
		methodConsumes = strings.TrimSpace(m[1])
	}
	if m := javaProducesRe.FindStringSubmatch(joined); m != nil {
		methodProduces = strings.TrimSpace(m[1])
	}

	framework := cf.framework
	if framework == "" {
		// Pure method-level Spring annotation with no class hint.
		framework = "spring"
	}

	canonFW := httproutes.FrameworkJAXRS
	if framework == "spring" {
		canonFW = httproutes.FrameworkSpring
	}

	composed := joinPathFragments(cf.prefix, methodPath)
	canonical := httproutes.Canonicalize(canonFW, composed)
	if canonical == "" {
		return nil
	}

	// Deduplicate verbs in case both @GET and a method=GET were present.
	verbSet := map[string]bool{}
	var uniqueVerbs []string
	for _, v := range verbs {
		if verbSet[v] {
			continue
		}
		verbSet[v] = true
		uniqueVerbs = append(uniqueVerbs, v)
	}

	var out []types.EntityRecord
	for _, verb := range uniqueVerbs {
		id := httproutes.SyntheticID(verb, canonical)
		props := map[string]string{
			"verb":           verb,
			"path":           canonical,
			"framework":      framework,
			"pattern_type":   "java_annotation_routes",
			"source_handler": "SCOPE.Operation:" + cf.name + "." + methodName,
		}
		if methodConsumes != "" {
			props["consumes"] = methodConsumes
		}
		if methodProduces != "" {
			props["produces"] = methodProduces
		}

		out = append(out, types.EntityRecord{
			ID:                 id,
			Name:               id,
			Kind:               httpEndpointKind,
			SourceFile:         relPath,
			Language:           "java",
			Properties:         props,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.85,
		})
	}
	return out
}

// parseRequestMappingMethods extracts every `method = RequestMethod.X` (or
// `method = {RequestMethod.X, RequestMethod.Y}`) value from a
// @RequestMapping argument list. Returns empty when no method keyword is
// present (caller treats this as ANY).
func parseRequestMappingMethods(args string) []string {
	var out []string
	for _, m := range javaMethodMethodArgRe.FindAllStringSubmatch(args, -1) {
		out = append(out, strings.ToUpper(m[1]))
	}
	return out
}

// joinPathFragments is shared with http_endpoint_synthesis.go (defined
// there). We do not redefine it here.
