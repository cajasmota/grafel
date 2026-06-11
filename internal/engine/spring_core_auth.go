// spring_core_auth.go — Spring Security [@PreAuthorize/@Secured/@RolesAllowed]
// method ▸ class ▸ global posture stamping onto the synthesized Spring
// http_endpoint_definition entities (#4750).
//
// synthesizeSpringFromComposed (http_endpoint_synthesis.go) re-emits the AST-
// composed Spring Routes as endpoints but carries only the route path — none of
// the class/method @PreAuthorize/@Secured annotations nor the SecurityFilterChain
// rule, so the authposture spring resolver had nothing structured to decode for
// class/global Spring postures (only method-level flat auth_roles flowed). This
// post-pass re-parses the controller file, reconstructs each handler's composed
// route path, and stamps the STRUCTURED props the resolver reads
// (internal/authposture/spring.go):
//
//	method  → auth_expression / secured
//	class   → spring_class_pre_authorize / spring_class_secured
//	global  → spring_global_authorization (a same-file SecurityFilterChain rule
//	          whose requestMatcher ant-pattern covers the route — the cross-file
//	          config case stays the documented source-scan gap)
//	          + handler_source for the source-scan fallback (#4752).
//
// Mirrors the in-place post-pass pattern applyAspnetCoreAuth / applyLaravelAuth
// use: it only mutates Properties on the spring_mvc/spring endpoints this file
// produced; it never adds or removes entities.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// springHandlerAuth is one handler's annotation context, keyed by composed route
// path so a path-only endpoint (the composed-Routes case) can be matched.
type springHandlerAuth struct {
	methodAnno string
	classAnno  string
}

// springClassDeclScanRe matches a controller class declaration line.
var springClassDeclScanRe = regexp.MustCompile(`\bclass\s+([A-Za-z_]\w*)\b`)

// springVerbMappingScanRe matches a Spring verb mapping annotation and its
// optional inline path arg. Group 1 = the path (may be empty).
var springVerbMappingScanRe = regexp.MustCompile(
	`@(?:Get|Post|Put|Delete|Patch|Request)Mapping\s*(?:\(\s*(?:value\s*=\s*|path\s*=\s*)?"([^"]*)"|)`)

// springClassRequestMappingRe captures a class-level @RequestMapping prefix.
var springClassRequestMappingRe = regexp.MustCompile(
	`@RequestMapping\s*\(\s*(?:value\s*=\s*|path\s*=\s*)?"([^"]*)"`)

// parseSpringHandlerAuth re-scans a Java controller file and returns, per composed
// route path, the method-level and class-level annotation blocks. It reproduces
// the prefix+method-path composition synthesizeSpringFromComposed relies on so
// the endpoint (which carries only the path) can be keyed back to its handler.
func parseSpringHandlerAuth(src string) (map[string]springHandlerAuth, []springFilterChainRule) {
	lines := strings.Split(src, "\n")
	out := map[string]springHandlerAuth{}

	classPrefix := ""
	classAnno := ""
	var annoBuf []string
	flush := func() []string { b := annoBuf; annoBuf = nil; return b }

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@") || trimmed == "" {
			annoBuf = append(annoBuf, trimmed)
			continue
		}
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "/*") {
			continue
		}
		// Class declaration: capture the class-level annotation block + @RequestMapping prefix.
		if springClassDeclScanRe.MatchString(line) && strings.Contains(line, "class ") {
			block := strings.Join(flush(), "\n")
			classAnno = block
			classPrefix = ""
			if m := springClassRequestMappingRe.FindStringSubmatch(block); m != nil {
				classPrefix = m[1]
			}
			continue
		}
		// Method declaration: a line carrying a verb-mapping in the annotation
		// block + a method signature. We treat any non-class declaration with a
		// pending annotation block carrying a verb mapping as a handler.
		block := strings.Join(annoBuf, "\n")
		if m := springVerbMappingScanRe.FindStringSubmatch(block); m != nil {
			methodPath := m[1]
			composed := joinPathFragments(classPrefix, methodPath)
			out[composed] = springHandlerAuth{methodAnno: block, classAnno: classAnno}
		}
		annoBuf = nil
	}

	return out, parseSpringFilterChainRules(src)
}

// applySpringCoreAuth stamps the structured Spring-Security posture onto the
// Spring endpoints emitted for `path` (#4750). Cheap-gated on the file carrying a
// Spring authorization annotation or a SecurityFilterChain.
func applySpringCoreAuth(content, path string, entities []types.EntityRecord, before int) {
	if before >= len(entities) {
		return
	}
	if !strings.Contains(content, "@PreAuthorize") && !strings.Contains(content, "@PostAuthorize") &&
		!strings.Contains(content, "@Secured") && !strings.Contains(content, "@RolesAllowed") &&
		!strings.Contains(content, "SecurityFilterChain") {
		return
	}
	handlers, filterChain := parseSpringHandlerAuth(content)

	for i := before; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind || e.SourceFile != path || e.Properties == nil {
			continue
		}
		fw := e.Properties["framework"]
		if fw != "spring_mvc" && fw != "spring" {
			continue
		}
		ha, ok := handlers[e.Properties["path"]]
		if !ok {
			// Fall back: the resolver's source-scan still fires off any same-file
			// global rule matched to the route prefix.
			if rule := matchSpringFilterChainRule(filterChain, e.Properties["path"]); rule != "" {
				e.Properties["spring_global_authorization"] = rule
			}
			continue
		}
		stamps := ResolveSpringAuthStamps(ha.methodAnno, ha.classAnno,
			matchSpringFilterChainRule(filterChain, e.Properties["path"]))
		stamps.Stamp(e.Properties)
		// #4752 — the handler annotation block as a source body so the spring
		// resolver's source-scan fallback fires live for any annotation shape the
		// structured stamps above don't cover.
		if ha.methodAnno != "" {
			e.Properties["handler_source"] = ha.methodAnno
		}
	}
}
