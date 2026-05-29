package java

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
)

// Struts custom extractor — route extraction, handler attribution, middleware
// (interceptor chain).
//
// Apache Struts 2 defines routes in two ways:
//  1. Annotation-based: @Action(value="/path") on action class methods, with
//     optional @Result annotations for response mapping.
//  2. XML-based: <action name="X" class="Y" method="Z"> elements inside
//     <package> blocks in struts.xml.
//
// Middleware in Struts is the interceptor chain: classes that implement
// com.opensymphony.xwork2.interceptor.Interceptor and override intercept().
// Struts ships a large default interceptor stack (params, validation, workflow,
// etc.) that applies to every action; custom interceptors are declared in
// struts.xml and wired to actions.
//
// Auth: Struts has no built-in auth layer — projects integrate JAAS or Spring
// Security externally. We detect JAAS (LoginContext / Subject) and Spring
// Security (@PreAuthorize / SecurityContextHolder) markers and emit an
// AuthGuard entity when present.
//
// DI is C (not_applicable for this extractor): Struts 2 supports Spring-plugin
// DI as an optional add-on, not an intrinsic part of the framework. The Spring
// DI extractor handles those bindings separately.
//
// AOP is C (not_applicable): Struts uses its interceptor chain rather than
// AspectJ/Spring AOP. The Spring AOP extractor must NOT fire for
// framework=struts.
//
// Transactions are C (not_applicable): Struts has no transaction management;
// projects use Spring @Transactional or JTA outside the framework.
//
// Coverage cells delivered (#3089):
//   - Routing:    route_extraction                → partial
//   - Auth:       auth_coverage                   → partial
//   - Middleware: middleware_coverage              → partial
//   - DI:         di_binding_extraction, di_injection_point, di_scope_resolution → not_applicable
//   - AOP:        advice_attribution, aspect_extraction, pointcut_resolution     → not_applicable
//   - Transactions: transaction_boundary_extraction, transaction_propagation,
//     transaction_rollback_rules                                                 → not_applicable

// strutsFrameworks is the set of framework identifiers that activate the Struts
// extractor.
var strutsFrameworks = map[string]bool{
	"struts":         true,
	"struts2":        true,
	"struts-2":       true,
	"apache_struts":  true,
	"apache-struts":  true,
	"struts_2":       true,
}

var (
	// @Action(value="/path") — annotation on a method or class.
	// Also matches @Action("/path") shorthand (value is positional).
	// Capture group 1: the path string.
	strutsActionAnnotationRE = regexp.MustCompile(
		`@Action\s*\(\s*(?:value\s*=\s*)?"([^"]+)"`)

	// @Action with method attribute: @Action(value="/path", ...)
	// We already capture the path above; no separate RE needed.

	// @Result(name="...", location="...") — response mapping annotation.
	// Capture group 1: result name, group 2: result location (JSP/redirect).
	strutsResultAnnotationRE = regexp.MustCompile(
		`@Result\s*\(\s*name\s*=\s*"([^"]+)"\s*,\s*(?:location|type)\s*=\s*"([^"]+)"`)

	// Interceptor implementation: class Foo implements Interceptor
	// or extends AbstractInterceptor (the most common extension point).
	// Capture group 1: class name.
	strutsInterceptorImplRE = regexp.MustCompile(
		`(?:public\s+)?(?:abstract\s+)?class\s+(\w+)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*\bInterceptor\b`)

	// AbstractInterceptor subclass (alternative pattern).
	strutsAbstractInterceptorRE = regexp.MustCompile(
		`(?:public\s+)?class\s+(\w+)\s+extends\s+AbstractInterceptor\b`)

	// intercept() method override — strongest signal for a custom interceptor.
	strutsInterceptMethodRE = regexp.MustCompile(
		`(?m)public\s+String\s+intercept\s*\(\s*ActionInvocation\b`)

	// Auth: JAAS integration markers.
	strutsJAASLoginContextRE = regexp.MustCompile(
		`\bLoginContext\b|\bSubject\.doAs\b|\bLoginModule\b`)

	// Auth: Spring Security markers (common in Struts+Spring stacks).
	strutsSpringSecurityRE = regexp.MustCompile(
		`@PreAuthorize\b|@Secured\b|\bSecurityContextHolder\b|\bAuthentication\b`)

	// Auth: custom security interceptor names commonly used with Struts.
	strutsSecurityInterceptorRE = regexp.MustCompile(
		`(?i)(?:security|auth(?:entication|orization)?|login)\s*interceptor`)

	// ActionSupport subclass — the canonical Struts 2 action base class.
	strutsActionSupportRE = regexp.MustCompile(
		`\bextends\s+ActionSupport\b`)

	// @Namespace("/prefix") — package-level namespace annotation.
	strutsNamespaceRE = regexp.MustCompile(
		`@Namespace\s*\(\s*"([^"]+)"`)
)

// strutsXMLAction represents a parsed <action> element from struts.xml.
type strutsXMLAction struct {
	Name   string `xml:"name,attr"`
	Class  string `xml:"class,attr"`
	Method string `xml:"method,attr"`
}

// strutsXMLPackage represents a parsed <package> element from struts.xml.
type strutsXMLPackage struct {
	Name      string            `xml:"name,attr"`
	Namespace string            `xml:"namespace,attr"`
	Actions   []strutsXMLAction `xml:"action"`
}

// strutsXMLConfig represents a parsed struts.xml config.
type strutsXMLConfig struct {
	Packages []strutsXMLPackage `xml:"package"`
}

// parseStrutsXML parses a struts.xml document and returns the action mappings.
// Returns nil on parse error (caller falls back to regex).
func parseStrutsXML(content string) []strutsXMLPackage {
	var cfg strutsXMLConfig
	dec := xml.NewDecoder(strings.NewReader(content))
	dec.Strict = false
	// Struts XML uses a DOCTYPE declaration that the stdlib XML parser will
	// reject if entity expansion is attempted. We simply ignore token errors
	// (DOCTYPE/ProcInst) and collect what we can.
	if err := dec.Decode(&cfg); err != nil {
		// Partial parse may have populated some packages — use what we have.
		_ = err
	}
	return cfg.Packages
}

// ExtractStruts runs the Struts extractor for route, interceptor (middleware),
// and auth patterns.
func ExtractStruts(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !strutsFrameworks[ctx.Framework] {
		return result
	}

	// Quick-exit: no Struts signals in this file.
	isXML := strings.HasSuffix(ctx.FilePath, ".xml")
	if !isXML {
		if !strings.Contains(ctx.Source, "struts2") &&
			!strings.Contains(ctx.Source, "Struts") &&
			!strings.Contains(ctx.Source, "ActionSupport") &&
			!strings.Contains(ctx.Source, "opensymphony") &&
			!strings.Contains(ctx.Source, "@Action") &&
			!strings.Contains(ctx.Source, "Interceptor") {
			return result
		}
	}

	seen := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// -------------------------------------------------------------------------
	// Namespace detection (annotation-level prefix)
	// -------------------------------------------------------------------------
	namespace := ""
	if m := strutsNamespaceRE.FindStringSubmatch(ctx.Source); len(m) >= 2 {
		namespace = m[1]
		if namespace == "/" {
			namespace = ""
		}
	}

	// -------------------------------------------------------------------------
	// Route extraction: @Action annotation
	// -------------------------------------------------------------------------
	for _, idx := range strutsActionAnnotationRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 4 {
			continue
		}
		rawPath := ctx.Source[idx[2]:idx[3]]
		fullPath := joinStrutsPath(namespace, rawPath)
		ref := fmt.Sprintf("struts:route:GET:%s:%s", fullPath, ctx.FilePath)

		e := SecondaryEntity{
			Name:       fullPath,
			Kind:       "Route",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_STRUTS_ACTION_ANNOTATION",
			Ref:        ref,
			Properties: map[string]any{
				"http_verb":  "ANY",
				"path":       fullPath,
				"framework":  "struts",
				"route_type": "annotation",
			},
		}
		addEntity(&result, seen, e)

		// Handler attribution: find the enclosing class name.
		enclosingClass := findEnclosingClass(ctx.Source, idx[0])
		if enclosingClass != "" {
			handlerRef := fmt.Sprintf("struts:handler:%s:%s", enclosingClass, ctx.FilePath)
			handler := SecondaryEntity{
				Name:       enclosingClass,
				Kind:       "Handler",
				SourceFile: ctx.FilePath,
				LineStart:  lineOf(ctx.Source, idx[0]),
				Provenance: "INFERRED_FROM_STRUTS_ACTION_HANDLER",
				Ref:        handlerRef,
				Properties: map[string]any{
					"framework":    "struts",
					"handler_type": "action_class",
					"path":         fullPath,
				},
			}
			addEntity(&result, seen, handler)
			addRel(&result, seenRels, Relationship{
				SourceRef:        ref,
				TargetRef:        handlerRef,
				RelationshipType: "HANDLED_BY",
				Properties:       map[string]string{"framework": "struts"},
			})
		}
	}

	// -------------------------------------------------------------------------
	// Route extraction: struts.xml <action> elements (XML-only path)
	// -------------------------------------------------------------------------
	if isXML {
		pkgs := parseStrutsXML(ctx.Source)
		for _, pkg := range pkgs {
			ns := pkg.Namespace
			if ns == "/" {
				ns = ""
			}
			for _, action := range pkg.Actions {
				rawPath := "/" + action.Name
				fullPath := joinStrutsPath(ns, rawPath)
				method := action.Method
				if method == "" {
					method = "execute"
				}
				ref := fmt.Sprintf("struts:route:xml:%s:%s", fullPath, ctx.FilePath)

				e := SecondaryEntity{
					Name:       fullPath,
					Kind:       "Route",
					SourceFile: ctx.FilePath,
					LineStart:  1,
					Provenance: "INFERRED_FROM_STRUTS_XML_ACTION",
					Ref:        ref,
					Properties: map[string]any{
						"http_verb":      "ANY",
						"path":           fullPath,
						"framework":      "struts",
						"route_type":     "xml_config",
						"action_class":   action.Class,
						"action_method":  method,
						"package_name":   pkg.Name,
						"namespace":      ns,
					},
				}
				addEntity(&result, seen, e)

				if action.Class != "" {
					// Derive simple class name from fully qualified name.
					className := action.Class
					if dot := strings.LastIndex(className, "."); dot >= 0 {
						className = className[dot+1:]
					}
					handlerRef := fmt.Sprintf("struts:handler:xml:%s:%s:%s", className, method, ctx.FilePath)
					handler := SecondaryEntity{
						Name:       className + "." + method,
						Kind:       "Handler",
						SourceFile: ctx.FilePath,
						LineStart:  1,
						Provenance: "INFERRED_FROM_STRUTS_XML_HANDLER",
						Ref:        handlerRef,
						Properties: map[string]any{
							"framework":    "struts",
							"handler_type": "action_method",
							"path":         fullPath,
							"class":        action.Class,
							"method":       method,
						},
					}
					addEntity(&result, seen, handler)
					addRel(&result, seenRels, Relationship{
						SourceRef:        ref,
						TargetRef:        handlerRef,
						RelationshipType: "HANDLED_BY",
						Properties:       map[string]string{"framework": "struts"},
					})
				}
			}
		}

		// XML files have no interceptor or auth content — return early.
		return result
	}

	// -------------------------------------------------------------------------
	// Middleware: Interceptor implementations
	// -------------------------------------------------------------------------
	// Pattern 1: implements Interceptor
	for _, idx := range strutsInterceptorImplRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 4 {
			continue
		}
		className := ctx.Source[idx[2]:idx[3]]
		ref := fmt.Sprintf("struts:interceptor:%s:%s", className, ctx.FilePath)
		e := SecondaryEntity{
			Name:       className,
			Kind:       "Middleware",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_STRUTS_INTERCEPTOR",
			Ref:        ref,
			Properties: map[string]any{
				"framework":        "struts",
				"middleware_type":  "interceptor",
				"interceptor_impl": "Interceptor",
			},
		}
		addEntity(&result, seen, e)
	}

	// Pattern 2: extends AbstractInterceptor
	for _, idx := range strutsAbstractInterceptorRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 4 {
			continue
		}
		className := ctx.Source[idx[2]:idx[3]]
		ref := fmt.Sprintf("struts:interceptor:%s:%s", className, ctx.FilePath)
		e := SecondaryEntity{
			Name:       className,
			Kind:       "Middleware",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_STRUTS_INTERCEPTOR",
			Ref:        ref,
			Properties: map[string]any{
				"framework":        "struts",
				"middleware_type":  "interceptor",
				"interceptor_impl": "AbstractInterceptor",
			},
		}
		addEntity(&result, seen, e)
	}

	// Pattern 3: intercept(ActionInvocation) method override alone
	// (catches interceptor subclasses that extend a third-party base class).
	if strutsInterceptMethodRE.MatchString(ctx.Source) {
		// Only emit a generic interceptor entity if we didn't already emit one
		// for the class above (dedup by ref).
		className := findEnclosingClass(ctx.Source,
			strutsInterceptMethodRE.FindStringIndex(ctx.Source)[0])
		if className != "" {
			ref := fmt.Sprintf("struts:interceptor:%s:%s", className, ctx.FilePath)
			e := SecondaryEntity{
				Name:       className,
				Kind:       "Middleware",
				SourceFile: ctx.FilePath,
				LineStart:  lineOf(ctx.Source, strutsInterceptMethodRE.FindStringIndex(ctx.Source)[0]),
				Provenance: "INFERRED_FROM_STRUTS_INTERCEPTOR",
				Ref:        ref,
				Properties: map[string]any{
					"framework":        "struts",
					"middleware_type":  "interceptor",
					"interceptor_impl": "intercept_override",
				},
			}
			addEntity(&result, seen, e)
		}
	}

	// -------------------------------------------------------------------------
	// Auth: JAAS / Spring Security markers
	// -------------------------------------------------------------------------
	if strutsJAASLoginContextRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("struts:auth:jaas:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "jaas_auth",
			Kind:       "AuthGuard",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, strutsJAASLoginContextRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_STRUTS_AUTH_JAAS",
			Ref:        ref,
			Properties: map[string]any{
				"framework": "struts",
				"auth_type": "jaas",
				"auth_hook": "LoginContext",
			},
		}
		addEntity(&result, seen, e)
	}

	if strutsSpringSecurityRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("struts:auth:spring_security:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "spring_security_auth",
			Kind:       "AuthGuard",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, strutsSpringSecurityRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_STRUTS_AUTH_SPRING_SECURITY",
			Ref:        ref,
			Properties: map[string]any{
				"framework": "struts",
				"auth_type": "spring_security",
				"auth_hook": "SecurityContextHolder/@PreAuthorize",
			},
		}
		addEntity(&result, seen, e)
	}

	if strutsSecurityInterceptorRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("struts:auth:security_interceptor:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "security_interceptor",
			Kind:       "AuthGuard",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, strutsSecurityInterceptorRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_STRUTS_AUTH_INTERCEPTOR",
			Ref:        ref,
			Properties: map[string]any{
				"framework": "struts",
				"auth_type": "security_interceptor",
				"auth_hook": "named_security_interceptor",
			},
		}
		addEntity(&result, seen, e)
	}

	return result
}

// joinStrutsPath joins a namespace prefix with an action path, ensuring
// exactly one slash between them and a leading slash on the result.
func joinStrutsPath(namespace, path string) string {
	ns := strings.TrimRight(namespace, "/")
	p := path
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if ns == "" {
		return p
	}
	return ns + p
}
