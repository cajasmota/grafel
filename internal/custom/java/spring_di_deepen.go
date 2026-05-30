// spring_di_deepen.go — Spring Boot / Spring WebFlux DI deepening (#3347).
//
// Delivers three new DI capability cells for lang.java.framework.spring-boot
// and lang.java.framework.spring-webflux:
//
//	di_binding_extraction
//	  @Qualifier-specific binding: captures the qualifier name used at the
//	  injection site so the graph records WHICH bean is injected, not just
//	  the type. Also records @ConditionalOnMissingBean-guarded @Bean methods
//	  (the bean is conditional — only registered when no other candidate
//	  exists in the application context).
//
//	di_injection_point
//	  @Value property injection: emits a SCOPE.Pattern for each @Value-
//	  annotated field or constructor parameter, recording the property key
//	  and default value. Injection-point cross-file resolution is tracked as
//	  DEPENDS_ON with injection_kind="value_property"; a genuine full
//	  cross-file dataflow binding (linking the @PropertySource file to every
//	  @Value site) is beyond single-file regex — this cell stays partial.
//
//	di_scope_resolution
//	  In addition to the existing Spring @Scope/@RequestScope/@SessionScope
//	  already handled in spring_boot.go, this file adds:
//	    - @ConditionalOnMissingBean: records the conditional guard on a @Bean.
//	    - Cross-file injection-point tracking (see di_injection_point note).
//
// Spring MVC middleware detection (middleware_coverage → full upgrade #3347):
//
//	Detects three standard Spring MVC middleware interface implementations:
//	  - HandlerInterceptor / WebMvcConfigurer.addInterceptors
//	  - OncePerRequestFilter  (extends OncePerRequestFilter)
//	  - GenericFilterBean     (extends GenericFilterBean)
//
//	Previous middleware_coverage was partial (only java_annotation_params.go
//	surfaced @RequestHeader/@CookieValue param location, not the interceptor /
//	filter class registrations). This pass adds the class-level detection so
//	middleware_coverage can be upgraded to full where the evidence is present.
//
// Framework gate: spring_boot / spring_webflux (same DI model).
package java

import "regexp"

// ── framework gate ────────────────────────────────────────────────────────────

var springDIFrameworks = map[string]bool{
	"spring_boot":    true,
	"spring-boot":    true,
	"springboot":     true,
	"spring_webflux": true,
	"spring-webflux": true,
	"springwebflux":  true,
}

// ── DI deepening regexes ──────────────────────────────────────────────────────

// sdQualifierFieldRE detects @Qualifier("beanName") on a field injection site.
// Group 1 = qualifier name, Group 2 = field type, Group 3 = field name.
var sdQualifierFieldRE = regexp.MustCompile(
	`(?s)@Qualifier\s*\(\s*"([^"]+)"\s*\)` +
		`\s*(?:@\w+(?:\s*\([^()]*(?:\([^()]*\)[^()]*)*\))?\s*)*` +
		`(?:@Autowired\b[^;{(]*?)?` +
		`(?:private|protected|public|)\s+(?:final\s+)?` +
		`(\w+)(?:\s*<[^>]*>)?\s+(\w+)\s*[;=]`)

// sdQualifierParamRE detects @Qualifier on a constructor/setter parameter.
// Group 1 = qualifier name, Group 2 = parameter type, Group 3 = param name.
var sdQualifierParamRE = regexp.MustCompile(
	`@Qualifier\s*\(\s*"([^"]+)"\s*\)\s+` +
		`(\w+)(?:\s*<[^>]*>)?\s+(\w+)` +
		`(?:\s*[,)])`)

// sdConditionalMissingBeanRE detects @ConditionalOnMissingBean on a @Bean method.
// Group 1 = (optional) type inside the annotation, Group 2 = method name.
var sdConditionalMissingBeanRE = regexp.MustCompile(
	`(?s)@ConditionalOnMissingBean\s*(?:\(([^)]*)\))?\s*` +
		`(?:@\w+(?:\([^)]*\))?\s*)*` +
		`@Bean\b[^;{]*?\s+(?:public\s+|protected\s+|private\s+)?(?:static\s+)?` +
		`(?:<[^>]*>\s+)?\w+(?:\s*<[^>]*>)?\s+(\w+)\s*\(`)

// sdValueFieldRE detects @Value("${prop.key:default}") on a field.
// Group 1 = the expression inside @Value("..."), Group 2 = field type, Group 3 = field name.
var sdValueFieldRE = regexp.MustCompile(
	`@Value\s*\(\s*"([^"]+)"\s*\)` +
		`\s*(?:@\w+(?:\s*\([^()]*(?:\([^()]*\)[^()]*)*\))?\s*)*` +
		`(?:private|protected|public|)\s+(?:final\s+)?` +
		`(\w+)(?:\s*<[^>]*>)?\s+(\w+)\s*[;=]`)

// sdValueParamRE detects @Value("${...}") on a constructor parameter.
// Group 1 = the expression, Group 2 = param type, Group 3 = param name.
var sdValueParamRE = regexp.MustCompile(
	`@Value\s*\(\s*"([^"]+)"\s*\)\s+` +
		`(\w+)(?:\s*<[^>]*>)?\s+(\w+)` +
		`(?:\s*[,)])`)

// ── Spring MVC middleware regexes ─────────────────────────────────────────────

// sdHandlerInterceptorRE detects classes implementing HandlerInterceptor.
// Group 1 = class name.
var sdHandlerInterceptorRE = regexp.MustCompile(
	`(?s)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)` +
		`[^{]*\bimplements\b[^{]*\bHandlerInterceptor\b`)

// sdWebMvcConfigurerRE detects classes implementing WebMvcConfigurer (interceptor registration).
// Group 1 = class name.
var sdWebMvcConfigurerRE = regexp.MustCompile(
	`(?s)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)` +
		`[^{]*\bimplements\b[^{]*\bWebMvcConfigurer\b`)

// sdOncePerRequestFilterRE detects classes extending OncePerRequestFilter.
// Group 1 = class name.
var sdOncePerRequestFilterRE = regexp.MustCompile(
	`(?s)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)` +
		`[^{]*\bextends\b\s+OncePerRequestFilter\b`)

// sdGenericFilterBeanRE detects classes extending GenericFilterBean.
// Group 1 = class name.
var sdGenericFilterBeanRE = regexp.MustCompile(
	`(?s)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)` +
		`[^{]*\bextends\b\s+GenericFilterBean\b`)

// sdAddInterceptorsRE detects an addInterceptors(InterceptorRegistry) override
// to confirm a WebMvcConfigurer is used for interceptor registration.
var sdAddInterceptorsRE = regexp.MustCompile(
	`\bvoid\s+addInterceptors\s*\(\s*InterceptorRegistry\b`)

// ── Extractor ─────────────────────────────────────────────────────────────────

// ExtractSpringDIDeepen runs the Spring DI deepening + MVC middleware extractor.
func ExtractSpringDIDeepen(ctx PatternContext) PatternResult {
	var result PatternResult
	if (ctx.Language != "java" && ctx.Language != "kotlin") || !springDIFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// ── 1. @Qualifier-specific binding ──────────────────────────────────────

	// Field-level @Qualifier injection.
	for _, m := range sdQualifierFieldRE.FindAllStringSubmatchIndex(source, -1) {
		qualName := source[m[2]:m[3]]
		injType := source[m[4]:m[5]]
		fieldName := source[m[6]:m[7]]
		ownerCls := findEnclosingClass(source, m[0])
		ref := "scope:component:spring_qualifier_injection:" + fp + ":" + ownerCls + "." + fieldName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerCls + "." + fieldName, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_QUALIFIER",
			Ref:        ref,
			Properties: map[string]any{
				"qualifier_name": qualName,
				"injected_type":  injType,
				"injection_kind": "qualifier_field",
				"framework":      ctx.Framework,
			},
		})
		// Emit DEPENDS_ON pointing to the qualified bean.
		targetRef := "scope:dependency:spring_bean_qualifier:" + fp + ":" + qualName
		addRel(&result, seenRels, Relationship{
			SourceRef:        ref,
			TargetRef:        targetRef,
			RelationshipType: "DEPENDS_ON",
			Properties: map[string]string{
				"qualifier":      qualName,
				"injected_type":  injType,
				"injection_kind": "qualifier",
			},
		})
	}

	// Parameter-level @Qualifier injection.
	for _, m := range sdQualifierParamRE.FindAllStringSubmatchIndex(source, -1) {
		qualName := source[m[2]:m[3]]
		injType := source[m[4]:m[5]]
		paramName := source[m[6]:m[7]]
		ownerCls := findEnclosingClass(source, m[0])
		ref := "scope:component:spring_qualifier_param:" + fp + ":" + ownerCls + "." + paramName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerCls + "." + paramName, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_QUALIFIER_PARAM",
			Ref:        ref,
			Properties: map[string]any{
				"qualifier_name": qualName,
				"injected_type":  injType,
				"injection_kind": "qualifier_param",
				"framework":      ctx.Framework,
			},
		})
	}

	// ── 2. @ConditionalOnMissingBean ────────────────────────────────────────

	for _, m := range sdConditionalMissingBeanRE.FindAllStringSubmatchIndex(source, -1) {
		condType := ""
		if m[2] >= 0 {
			condType = source[m[2]:m[3]]
		}
		methodName := source[m[4]:m[5]]
		ownerCls := findEnclosingClass(source, m[0])
		ref := "scope:operation:spring_conditional_missing_bean:" + fp + ":" + ownerCls + "." + methodName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerCls + "." + methodName, Kind: "SCOPE.Operation",
			Subtype:    "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_CONDITIONAL_ON_MISSING_BEAN",
			Ref:        ref,
			Properties: map[string]any{
				"conditional_kind": "ConditionalOnMissingBean",
				"missing_type":     condType,
				"bean_method":      methodName,
				"framework":        ctx.Framework,
			},
		})
	}

	// ── 3. @Value property injection ────────────────────────────────────────

	// Field-level @Value.
	for _, m := range sdValueFieldRE.FindAllStringSubmatchIndex(source, -1) {
		expr := source[m[2]:m[3]]
		fieldType := source[m[4]:m[5]]
		fieldName := source[m[6]:m[7]]
		propKey, defaultVal := parseValueExpression(expr)
		ownerCls := findEnclosingClass(source, m[0])
		ref := "scope:component:spring_value_injection:" + fp + ":" + ownerCls + "." + fieldName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerCls + "." + fieldName, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_VALUE_INJECTION",
			Ref:        ref,
			Properties: map[string]any{
				"value_expr":     expr,
				"property_key":   propKey,
				"default_value":  defaultVal,
				"field_type":     fieldType,
				"injection_kind": "value_property",
				"framework":      ctx.Framework,
			},
		})
		targetRef := "scope:dependency:spring_property:" + fp + ":" + propKey
		addRel(&result, seenRels, Relationship{
			SourceRef:        ref,
			TargetRef:        targetRef,
			RelationshipType: "DEPENDS_ON",
			Properties: map[string]string{
				"property_key":   propKey,
				"injection_kind": "value_property",
			},
		})
	}

	// Parameter-level @Value.
	for _, m := range sdValueParamRE.FindAllStringSubmatchIndex(source, -1) {
		expr := source[m[2]:m[3]]
		paramType := source[m[4]:m[5]]
		paramName := source[m[6]:m[7]]
		propKey, defaultVal := parseValueExpression(expr)
		ownerCls := findEnclosingClass(source, m[0])
		ref := "scope:component:spring_value_param:" + fp + ":" + ownerCls + "." + paramName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerCls + "." + paramName, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_VALUE_PARAM",
			Ref:        ref,
			Properties: map[string]any{
				"value_expr":     expr,
				"property_key":   propKey,
				"default_value":  defaultVal,
				"param_type":     paramType,
				"injection_kind": "value_property_param",
				"framework":      ctx.Framework,
			},
		})
	}

	// ── 4. Spring MVC middleware: HandlerInterceptor ─────────────────────────

	for _, m := range sdHandlerInterceptorRE.FindAllStringSubmatchIndex(source, -1) {
		cls := source[m[2]:m[3]]
		ref := "scope:component:spring_handler_interceptor:" + fp + ":" + cls
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: cls, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_HANDLER_INTERCEPTOR",
			Ref:        ref,
			Properties: map[string]any{
				"middleware_type": "HandlerInterceptor",
				"framework":       ctx.Framework,
			},
		})
	}

	// ── 5. Spring MVC middleware: WebMvcConfigurer (interceptor registration) ─

	for _, m := range sdWebMvcConfigurerRE.FindAllStringSubmatchIndex(source, -1) {
		cls := source[m[2]:m[3]]
		// Only emit if the class actually overrides addInterceptors.
		body := source[m[0]:min(m[0]+2000, len(source))]
		if !sdAddInterceptorsRE.MatchString(body) {
			continue
		}
		ref := "scope:component:spring_mvc_configurer:" + fp + ":" + cls
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: cls, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_MVC_CONFIGURER",
			Ref:        ref,
			Properties: map[string]any{
				"middleware_type": "WebMvcConfigurer",
				"framework":       ctx.Framework,
			},
		})
	}

	// ── 6. Spring MVC middleware: OncePerRequestFilter ───────────────────────

	for _, m := range sdOncePerRequestFilterRE.FindAllStringSubmatchIndex(source, -1) {
		cls := source[m[2]:m[3]]
		ref := "scope:component:spring_once_per_request_filter:" + fp + ":" + cls
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: cls, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_ONCE_PER_REQUEST_FILTER",
			Ref:        ref,
			Properties: map[string]any{
				"middleware_type": "OncePerRequestFilter",
				"framework":       ctx.Framework,
			},
		})
	}

	// ── 7. Spring MVC middleware: GenericFilterBean ──────────────────────────

	for _, m := range sdGenericFilterBeanRE.FindAllStringSubmatchIndex(source, -1) {
		cls := source[m[2]:m[3]]
		ref := "scope:component:spring_generic_filter_bean:" + fp + ":" + cls
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: cls, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_GENERIC_FILTER_BEAN",
			Ref:        ref,
			Properties: map[string]any{
				"middleware_type": "GenericFilterBean",
				"framework":       ctx.Framework,
			},
		})
	}

	return result
}

// ── helpers ──────────────────────────────────────────────────────────────────

// parseValueExpression parses a Spring @Value expression of the form
// "${property.key:defaultValue}" or "${property.key}" and returns the key
// and default value (empty string when no default is present).
// It also handles SpEL expressions like "#{config.value}" — for those it
// returns the raw expression as the key and an empty default.
func parseValueExpression(expr string) (key, defaultVal string) {
	// Strip outer ${ } or #{ }.
	trimmed := expr
	if len(trimmed) > 3 && trimmed[0] == '$' && trimmed[1] == '{' && trimmed[len(trimmed)-1] == '}' {
		inner := trimmed[2 : len(trimmed)-1]
		if i := indexByte(inner, ':'); i >= 0 {
			return inner[:i], inner[i+1:]
		}
		return inner, ""
	}
	if len(trimmed) > 3 && trimmed[0] == '#' && trimmed[1] == '{' && trimmed[len(trimmed)-1] == '}' {
		return trimmed, "" // SpEL — return raw
	}
	return expr, ""
}

// indexByte finds the first occurrence of b in s, returning -1 if not found.
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
