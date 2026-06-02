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
// DTO extraction (#3191): Struts binds request parameters to action state in
// two ways:
//   1. Struts 1 — ActionForm subclasses (and Validator/Dyna variants) act as
//      the form-backing DTO; their getter/setter properties are the bound
//      request fields.
//   2. Struts 2 — action classes (ActionSupport subclasses, Action /
//      ModelDriven implementors) bind request params directly onto public
//      setters via OGNL. ModelDriven<T> exposes a separate domain model T.
// We emit a SCOPE.Schema DTO entity for each backing type and BINDS_INPUT
// relationships from the action handler. This is heuristic (regex over public
// setters / `extends ActionForm`), so the cell is `partial`.
//
// Coverage cells delivered (#3089):
//   - Routing:    route_extraction                → partial
//   - Auth:       auth_coverage                   → partial
//   - Middleware: middleware_coverage              → partial
//
// Coverage cells delivered (#3191):
//   - Validation: dto_extraction                  → partial
//   - DI:         di_binding_extraction, di_injection_point, di_scope_resolution → not_applicable
//   - AOP:        advice_attribution, aspect_extraction, pointcut_resolution     → not_applicable
//   - Transactions: transaction_boundary_extraction, transaction_propagation,
//     transaction_rollback_rules                                                 → not_applicable

// strutsFrameworks is the set of framework identifiers that activate the Struts
// extractor.
var strutsFrameworks = map[string]bool{
	"struts":        true,
	"struts2":       true,
	"struts-2":      true,
	"apache_struts": true,
	"apache-struts": true,
	"struts_2":      true,
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

	// Auth: the built-in Struts 2 `roles` interceptor reference in struts.xml,
	// e.g. <interceptor-ref name="roles">. This is the canonical declarative
	// role gate. Presence inside a <package>/<action> means those actions require
	// the roles declared in the interceptor's allowedRoles / disallowedRoles
	// params.
	strutsRolesInterceptorRefRE = regexp.MustCompile(
		`(?s)<interceptor-ref\b[^>]*\bname\s*=\s*"roles"[^>]*>(.*?)</interceptor-ref>`)

	// Auth: a <param name="allowedRoles">ADMIN,USER</param> inside the roles
	// interceptor-ref. Capture group 1 = the comma-separated role list.
	strutsAllowedRolesParamRE = regexp.MustCompile(
		`(?s)<param\b[^>]*\bname\s*=\s*"allowedRoles"[^>]*>\s*([^<]*?)\s*</param>`)

	// Auth: @Action with a security interceptor stack referenced via
	// interceptorRefs, e.g.
	//   @Action(value="/admin", interceptorRefs={@InterceptorRef("secureStack")})
	// We treat a referenced stack whose name names auth/security/roles as a gate.
	strutsActionInterceptorRefRE = regexp.MustCompile(
		`(?i)@InterceptorRef\s*\(\s*"([^"]*(?:secure|security|auth|roles|login)[^"]*)"`)

	// ActionSupport subclass — the canonical Struts 2 action base class.
	strutsActionSupportRE = regexp.MustCompile(
		`\bextends\s+ActionSupport\b`)

	// @Namespace("/prefix") — package-level namespace annotation.
	strutsNamespaceRE = regexp.MustCompile(
		`@Namespace\s*\(\s*"([^"]+)"`)

	// ── DTO extraction (#3191) ───────────────────────────────────────────────

	// Struts 1 ActionForm subclass: `class FooForm extends ActionForm` and the
	// common Struts/Validator/Dyna variants. Capture group 1: class name,
	// group 2: the base class actually extended (for provenance/properties).
	strutsActionFormRE = regexp.MustCompile(
		`(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+extends\s+` +
			`(ActionForm|ValidatorForm|ValidatorActionForm|DynaActionForm|DynaValidatorForm)\b`)

	// Struts 2 action base class: `class FooAction extends ActionSupport`
	// (also bare implementors of the Action interface / ModelDriven).
	// Capture group 1: class name, group 2: base/interface.
	strutsActionClassRE = regexp.MustCompile(
		`(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+` +
			`(?:extends\s+(ActionSupport)\b|` +
			`(?:[\w<>, ]+\s+)?implements\s+[^{]*\b(ModelDriven|Action)\b)`)

	// Public setter method — the OGNL field-binding surface for Struts 2
	// actions. Capture group 1: property name (after `set`), group 2: param type.
	strutsSetterRE = regexp.MustCompile(
		`(?m)public\s+void\s+set([A-Z]\w*)\s*\(\s*([\w.<>\[\], ]+?)\s+\w+\s*\)`)

	// ModelDriven<T>.getModel() return type — the bound domain model.
	// Capture group 1: the model type.
	strutsModelDrivenRE = regexp.MustCompile(
		`implements\s+[^{]*\bModelDriven\s*<\s*([\w.]+)\s*>`)

	// ── request_validation (#3256) ────────────────────────────────────────────

	// Struts 2: validate() method override on ActionSupport (or plain Action
	// implementors). This is the canonical programmatic validation hook — the
	// Struts 2 workflow interceptor calls it before execute().
	// Matches:  public void validate() {
	strutsValidateMethodRE = regexp.MustCompile(
		`(?m)public\s+void\s+validate\s*\(\s*\)`)

	// Struts 1: ActionForm.validate(ActionMapping, HttpServletRequest) — the
	// Struts 1 validation callback signature.
	strutsActionFormValidateRE = regexp.MustCompile(
		`(?m)public\s+ActionErrors\s+validate\s*\(`)

	// Struts 2 @Validations annotation (wraps multiple field validators).
	strutsValidationsAnnoRE = regexp.MustCompile(
		`@Validations\s*\(`)

	// Struts 2 @Validation class-level annotation (enables framework validation).
	strutsValidationAnnoRE = regexp.MustCompile(
		`@Validation\b`)

	// Struts 2 field-validator annotations — each covers one constraint type.
	// We match a broad set:  @RequiredStringValidator, @IntRangeFieldValidator,
	// @EmailValidator, @RegexFieldValidator, @StringLengthFieldValidator,
	// @RequiredFieldValidator, @UrlValidator, @CustomValidator.
	strutsFieldValidatorAnnoRE = regexp.MustCompile(
		`@(?:Required(?:String|Field)?Validator|IntRangeFieldValidator|EmailValidator|` +
			`RegexFieldValidator|StringLengthFieldValidator|UrlValidator|CustomValidator|` +
			`ConversionErrorFieldValidator|DateRangeFieldValidator|FieldExpressionValidator|` +
			`ExpressionValidator)\b`)

	// validation.xml: <field name="..."> inside a <validators> block.
	// The Struts 1 validator framework and Struts 2 XML-based validators both
	// use this pattern. Capture group 1: field name.
	strutsValidationXMLFieldRE = regexp.MustCompile(
		`(?m)<field\b[^>]*\bname\s*=\s*"([^"]+)"`)

	// validation.xml: <validator type="..."> at the top level (Struts 2 action-
	// level validator). Capture group 1: validator type.
	strutsValidationXMLValidatorRE = regexp.MustCompile(
		`(?m)<validator\b[^>]*\btype\s*=\s*"([^"]+)"`)
)

// strutsDTOSkipProps lists setter property names that are framework plumbing
// rather than bound request fields, so they are not surfaced as DTO fields.
var strutsDTOSkipProps = map[string]bool{
	"servletRequest":  true,
	"servletResponse": true,
	"session":         true,
	"application":     true,
	"request":         true,
	"response":        true,
	"parameters":      true,
	"servletContext":  true,
	"pageContext":     true,
	"actionErrors":    true,
	"actionMessages":  true,
	"fieldErrors":     true,
}

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
			!strings.Contains(ctx.Source, "ActionForm") &&
			!strings.Contains(ctx.Source, "ModelDriven") &&
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

	// File-level auth posture for annotation routes (#3862). Struts has no
	// intrinsic auth; protection comes from a security/roles interceptor in the
	// stack. We recognise an @InterceptorRef naming a secure/auth/roles stack as
	// a gate (low confidence — the stack contents live in struts.xml). Honest-
	// partial: an action file with no such ref stamps nothing.
	annotationAuth := authStamp{}
	if m := strutsActionInterceptorRefRE.FindStringSubmatch(ctx.Source); m != nil {
		annotationAuth = authStamp{
			required: true, method: "middleware", confidence: "low",
			guard: "interceptor:" + m[1],
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

		props := map[string]any{
			"http_verb":  "ANY",
			"path":       fullPath,
			"framework":  "struts",
			"route_type": "annotation",
		}
		annotationAuth.stamp(props)

		e := SecondaryEntity{
			Name:       fullPath,
			Kind:       "Route",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_STRUTS_ACTION_ANNOTATION",
			Ref:        ref,
			Properties: props,
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
		// Resolve the file-level roles-interceptor posture: a <interceptor-ref
		// name="roles"> with an allowedRoles param gates the package's actions.
		xmlAuth := strutsXMLRolesAuth(ctx.Source)

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

				props := map[string]any{
					"http_verb":     "ANY",
					"path":          fullPath,
					"framework":     "struts",
					"route_type":    "xml_config",
					"action_class":  action.Class,
					"action_method": method,
					"package_name":  pkg.Name,
					"namespace":     ns,
				}
				xmlAuth.stamp(props)

				e := SecondaryEntity{
					Name:       fullPath,
					Kind:       "Route",
					SourceFile: ctx.FilePath,
					LineStart:  1,
					Provenance: "INFERRED_FROM_STRUTS_XML_ACTION",
					Ref:        ref,
					Properties: props,
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

		// XML files may contain validation descriptors (validation.xml).
		// request_validation: <field> / <validator> elements.
		extractStrutsRequestValidation(ctx, &result, seen)

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
	// DTO extraction (#3191): ActionForm (Struts 1) + action field-binding
	// (Struts 2). Emits SCOPE.Schema DTO entities and BINDS_INPUT relationships.
	// -------------------------------------------------------------------------
	extractStrutsDTO(ctx, &result, seen, seenRels)

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

	// request_validation: validate() overrides, @Validations/@Validation,
	// field-validator annotations, and XML validation descriptors.
	extractStrutsRequestValidation(ctx, &result, seen)

	return result
}

// strutsXMLRolesAuth resolves the auth posture declared by a Struts 2 `roles`
// interceptor in a struts.xml document. A <interceptor-ref name="roles"> gates
// the actions in its package; an allowedRoles param names the required roles.
// Returns the zero authStamp (method == "") when no roles interceptor is
// present, so a struts.xml with no role gate stamps nothing.
func strutsXMLRolesAuth(content string) authStamp {
	m := strutsRolesInterceptorRefRE.FindStringSubmatch(content)
	if m == nil {
		return authStamp{}
	}
	var roles []string
	if pm := strutsAllowedRolesParamRE.FindStringSubmatch(m[1]); pm != nil {
		for _, r := range strings.Split(pm[1], ",") {
			if r = strings.TrimSpace(r); r != "" {
				roles = append(roles, r)
			}
		}
	}
	return authStamp{
		required: true, method: "middleware", confidence: "high",
		guard: "roles-interceptor", roles: roles,
	}
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

// extractStrutsDTO detects form-backing DTO types and request-binding fields
// for both Struts 1 (ActionForm) and Struts 2 (ActionSupport / Action /
// ModelDriven) and records them on result.
//
// Emitted records:
//   - SCOPE.Schema DTO entity per ActionForm subclass / action class that
//     binds request parameters (provenance INFERRED_FROM_STRUTS_*).
//   - BINDS_INPUT relationship from the DTO entity to each bound field
//     (the OGNL/getter-setter property surface).
//   - For ModelDriven<T>: a SCOPE.Schema entity for the model type T plus a
//     BINDS_MODEL relationship from the action to the model.
func extractStrutsDTO(
	ctx PatternContext,
	result *PatternResult,
	seen map[string]bool,
	seenRels map[relKey]bool,
) {
	src := ctx.Source
	fp := ctx.FilePath

	// Collect the public-setter-bound field properties for the file once.
	// Each entry maps the setter offset to its (property, type).
	type setterProp struct {
		prop   string
		typ    string
		offset int
	}
	var setters []setterProp
	for _, m := range strutsSetterRE.FindAllStringSubmatchIndex(src, -1) {
		prop := src[m[2]:m[3]]
		typ := strings.TrimSpace(src[m[4]:m[5]])
		// Lower-case the leading character to get the bean property name.
		lcProp := strings.ToLower(prop[:1]) + prop[1:]
		if strutsDTOSkipProps[lcProp] {
			continue
		}
		setters = append(setters, setterProp{prop: lcProp, typ: typ, offset: m[0]})
	}

	// emitDTO records a DTO Schema entity + its bound fields. base describes the
	// detected backing-type kind for provenance/properties.
	emitDTO := func(className string, classOffset int, formKind, provenance string) {
		dtoRef := fmt.Sprintf("scope:schema:struts_dto:%s:%s", fp, className)
		addEntity(result, seen, SecondaryEntity{
			Name:       className,
			Kind:       "SCOPE.Schema",
			SourceFile: fp,
			LineStart:  lineOf(src, classOffset),
			Provenance: provenance,
			Ref:        dtoRef,
			Properties: map[string]any{
				"kind":      "dto",
				"framework": "struts",
				"form_kind": formKind,
			},
		})

		// Bind the public setters that belong to this class (the next class
		// declaration after classOffset bounds the field set).
		nextClass := len(src)
		for _, m := range classDeclRE.FindAllStringSubmatchIndex(src, -1) {
			if m[0] > classOffset && m[0] < nextClass {
				nextClass = m[0]
			}
		}
		for _, s := range setters {
			if s.offset <= classOffset || s.offset >= nextClass {
				continue
			}
			fieldRef := fmt.Sprintf("scope:field:struts_dto:%s:%s:%s", fp, className, s.prop)
			addEntity(result, seen, SecondaryEntity{
				Name:       className + "." + s.prop,
				Kind:       "SCOPE.Field",
				SourceFile: fp,
				LineStart:  lineOf(src, s.offset),
				Provenance: "INFERRED_FROM_STRUTS_FIELD_BINDING",
				Ref:        fieldRef,
				Properties: map[string]any{
					"framework":  "struts",
					"field_name": s.prop,
					"field_type": s.typ,
					"dto":        className,
				},
			})
			addRel(result, seenRels, Relationship{
				SourceRef:        dtoRef,
				TargetRef:        fieldRef,
				RelationshipType: "BINDS_INPUT",
				Properties: map[string]string{
					"framework": "struts",
					"via":       "ognl_setter",
				},
			})
		}
	}

	// Struts 1: ActionForm subclasses (and Validator/Dyna variants).
	for _, m := range strutsActionFormRE.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		base := src[m[4]:m[5]]
		emitDTO(className, m[0], base, "INFERRED_FROM_STRUTS_ACTIONFORM")
	}

	// Struts 2: ActionSupport / Action / ModelDriven implementors.
	for _, m := range strutsActionClassRE.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		// Determine the base/interface that matched (group 2 = ActionSupport,
		// group 3 = ModelDriven/Action).
		formKind := "action"
		if m[4] >= 0 {
			formKind = src[m[4]:m[5]]
		} else if m[6] >= 0 {
			formKind = src[m[6]:m[7]]
		}
		emitDTO(className, m[0], formKind, "INFERRED_FROM_STRUTS_ACTION_BINDING")
	}

	// ModelDriven<T>: surface the domain model type as its own DTO and link it.
	for _, m := range strutsModelDrivenRE.FindAllStringSubmatchIndex(src, -1) {
		modelType := src[m[2]:m[3]]
		// Use the simple class name for the DTO entity.
		if dot := strings.LastIndex(modelType, "."); dot >= 0 {
			modelType = modelType[dot+1:]
		}
		if modelType == "" {
			continue
		}
		modelRef := fmt.Sprintf("scope:schema:struts_dto:%s:%s", fp, modelType)
		addEntity(result, seen, SecondaryEntity{
			Name:       modelType,
			Kind:       "SCOPE.Schema",
			SourceFile: fp,
			LineStart:  lineOf(src, m[0]),
			Provenance: "INFERRED_FROM_STRUTS_MODELDRIVEN",
			Ref:        modelRef,
			Properties: map[string]any{
				"kind":      "dto",
				"framework": "struts",
				"form_kind": "model_driven",
			},
		})
		// Link the enclosing action class to the model it drives.
		action := findEnclosingClass(src, m[0])
		if action != "" {
			actionRef := fmt.Sprintf("scope:schema:struts_dto:%s:%s", fp, action)
			addRel(result, seenRels, Relationship{
				SourceRef:        actionRef,
				TargetRef:        modelRef,
				RelationshipType: "BINDS_MODEL",
				Properties: map[string]string{
					"framework": "struts",
					"via":       "model_driven",
				},
			})
		}
	}
}

// extractStrutsRequestValidation detects Struts request-validation patterns:
//
//   - Struts 2: public void validate() override on ActionSupport / Action
//     (programmatic validation hook called by the workflow interceptor).
//   - Struts 2: @Validations / @Validation class-level annotations and
//     per-field validator annotations (@RequiredStringValidator, etc.).
//   - Struts 1: ActionForm.validate(ActionMapping, HttpServletRequest)
//     override — the Struts 1 validation callback.
//   - XML-based: <field name="..."> / <validator type="..."> in
//     validation.xml / *-validation.xml descriptors.
//
// Each detected validation site emits a SCOPE.Operation entity with subtype
// "validation" and provenance INFERRED_FROM_STRUTS_VALIDATION_*, providing
// the request_validation capability signal for the struts registry record.
func extractStrutsRequestValidation(ctx PatternContext, result *PatternResult, seen map[string]bool) {
	src := ctx.Source
	fp := ctx.FilePath
	isXML := strings.HasSuffix(fp, ".xml")

	if isXML {
		// XML validation descriptors: <field name="..."> and <validator type="...">
		for _, m := range strutsValidationXMLFieldRE.FindAllStringSubmatchIndex(src, -1) {
			fieldName := src[m[2]:m[3]]
			ref := fmt.Sprintf("struts:validation:xml_field:%s:%s", fieldName, fp)
			addEntity(result, seen, SecondaryEntity{
				Name:       fieldName,
				Kind:       "SCOPE.Operation",
				Subtype:    "validation",
				SourceFile: fp,
				LineStart:  lineOf(src, m[0]),
				Provenance: "INFERRED_FROM_STRUTS_VALIDATION_XML_FIELD",
				Ref:        ref,
				Properties: map[string]any{
					"framework":       "struts",
					"validation_kind": "xml_field",
					"field_name":      fieldName,
				},
			})
		}
		for _, m := range strutsValidationXMLValidatorRE.FindAllStringSubmatchIndex(src, -1) {
			validatorType := src[m[2]:m[3]]
			ref := fmt.Sprintf("struts:validation:xml_validator:%s:%s:%d", validatorType, fp, lineOf(src, m[0]))
			addEntity(result, seen, SecondaryEntity{
				Name:       validatorType,
				Kind:       "SCOPE.Operation",
				Subtype:    "validation",
				SourceFile: fp,
				LineStart:  lineOf(src, m[0]),
				Provenance: "INFERRED_FROM_STRUTS_VALIDATION_XML_VALIDATOR",
				Ref:        ref,
				Properties: map[string]any{
					"framework":       "struts",
					"validation_kind": "xml_validator",
					"validator_type":  validatorType,
				},
			})
		}
		return
	}

	// Java source: Struts 2 validate() override.
	for _, m := range strutsValidateMethodRE.FindAllStringSubmatchIndex(src, -1) {
		enclosing := findEnclosingClass(src, m[0])
		name := "validate"
		if enclosing != "" {
			name = enclosing + ".validate"
		}
		ref := fmt.Sprintf("struts:validation:validate_method:%s:%s", name, fp)
		props := map[string]any{
			"framework":       "struts",
			"validation_kind": "validate_override",
		}
		if enclosing != "" {
			props["action_class"] = enclosing
		}
		addEntity(result, seen, SecondaryEntity{
			Name:       name,
			Kind:       "SCOPE.Operation",
			Subtype:    "validation",
			SourceFile: fp,
			LineStart:  lineOf(src, m[0]),
			Provenance: "INFERRED_FROM_STRUTS_VALIDATE_METHOD",
			Ref:        ref,
			Properties: props,
		})
	}

	// Struts 1: ActionForm.validate() override.
	for _, m := range strutsActionFormValidateRE.FindAllStringSubmatchIndex(src, -1) {
		enclosing := findEnclosingClass(src, m[0])
		name := "validate"
		if enclosing != "" {
			name = enclosing + ".validate"
		}
		ref := fmt.Sprintf("struts:validation:actionform_validate:%s:%s", name, fp)
		props := map[string]any{
			"framework":       "struts",
			"validation_kind": "actionform_validate",
		}
		if enclosing != "" {
			props["form_class"] = enclosing
		}
		addEntity(result, seen, SecondaryEntity{
			Name:       name,
			Kind:       "SCOPE.Operation",
			Subtype:    "validation",
			SourceFile: fp,
			LineStart:  lineOf(src, m[0]),
			Provenance: "INFERRED_FROM_STRUTS_ACTIONFORM_VALIDATE",
			Ref:        ref,
			Properties: props,
		})
	}

	// Struts 2: @Validations annotation (class or method level).
	for _, m := range strutsValidationsAnnoRE.FindAllStringSubmatchIndex(src, -1) {
		enclosing := findEnclosingClass(src, m[0])
		ref := fmt.Sprintf("struts:validation:validations_anno:%s:%d", fp, lineOf(src, m[0]))
		props := map[string]any{
			"framework":       "struts",
			"validation_kind": "validations_annotation",
		}
		if enclosing != "" {
			props["action_class"] = enclosing
		}
		addEntity(result, seen, SecondaryEntity{
			Name:       "@Validations",
			Kind:       "SCOPE.Operation",
			Subtype:    "validation",
			SourceFile: fp,
			LineStart:  lineOf(src, m[0]),
			Provenance: "INFERRED_FROM_STRUTS_VALIDATIONS_ANNOTATION",
			Ref:        ref,
			Properties: props,
		})
	}

	// Struts 2: @Validation annotation (class-level framework-enable flag).
	if strutsValidationAnnoRE.MatchString(src) {
		m := strutsValidationAnnoRE.FindStringIndex(src)
		enclosing := findEnclosingClass(src, m[0])
		ref := fmt.Sprintf("struts:validation:validation_anno:%s", fp)
		props := map[string]any{
			"framework":       "struts",
			"validation_kind": "validation_annotation",
		}
		if enclosing != "" {
			props["action_class"] = enclosing
		}
		addEntity(result, seen, SecondaryEntity{
			Name:       "@Validation",
			Kind:       "SCOPE.Operation",
			Subtype:    "validation",
			SourceFile: fp,
			LineStart:  lineOf(src, m[0]),
			Provenance: "INFERRED_FROM_STRUTS_VALIDATION_ANNOTATION",
			Ref:        ref,
			Properties: props,
		})
	}

	// Struts 2: field-validator annotations (@RequiredStringValidator, etc.).
	for _, m := range strutsFieldValidatorAnnoRE.FindAllStringIndex(src, -1) {
		annoName := src[m[0]:m[1]]
		enclosing := findEnclosingClass(src, m[0])
		ref := fmt.Sprintf("struts:validation:field_validator:%s:%s:%d", annoName, fp, lineOf(src, m[0]))
		props := map[string]any{
			"framework":       "struts",
			"validation_kind": "field_validator_annotation",
			"validator_type":  annoName,
		}
		if enclosing != "" {
			props["action_class"] = enclosing
		}
		addEntity(result, seen, SecondaryEntity{
			Name:       annoName,
			Kind:       "SCOPE.Operation",
			Subtype:    "validation",
			SourceFile: fp,
			LineStart:  lineOf(src, m[0]),
			Provenance: "INFERRED_FROM_STRUTS_FIELD_VALIDATOR_ANNOTATION",
			Ref:        ref,
			Properties: props,
		})
	}
}
