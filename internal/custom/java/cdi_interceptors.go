package java

import "regexp"

// CDI interceptor / AOP extractor for jakarta-ee, jaxrs, and quarkus
// (#3082, epic #2847).
//
// CDI uses a different interception model than Spring AOP:
//   - @Interceptor annotates the interceptor implementation class
//     (aspect_extraction analogue).
//   - @AroundInvoke / @AroundConstruct methods inside the interceptor class
//     carry the cross-cutting advice (advice_attribution analogue).
//   - @InterceptorBinding marks a custom qualifier annotation that binds an
//     interceptor to target beans (pointcut_resolution analogue).
//
// Entity mapping (reuses SCOPE.Pattern kind, matching spring_aop.go):
//   - @Interceptor class  → SCOPE.Pattern subtype=aspect   kind=interceptor
//   - @AroundInvoke       → SCOPE.Pattern subtype=advice   kind=around_invoke
//   - @AroundConstruct    → SCOPE.Pattern subtype=advice   kind=around_construct
//   - @InterceptorBinding → SCOPE.Pattern subtype=pointcut kind=interceptor_binding
//
// Covers:
//   advice_attribution    (missing → partial) for jakarta-ee, jaxrs, quarkus
//   aspect_extraction     (missing → partial) for jakarta-ee, jaxrs, quarkus
//   pointcut_resolution   (missing → partial) for jakarta-ee, jaxrs, quarkus

// cdiFrameworks gates the frameworks for which CDI interceptor extraction runs.
var cdiFrameworks = map[string]bool{
	"jakarta_ee": true, "jakarta-ee": true, "jakartaee": true,
	"java_ee": true, "javaee": true,
	"jaxrs": true, "jax-rs": true,
	"quarkus": true,
}

var (
	// cdiInterceptorClassRE matches an @Interceptor-annotated class declaration,
	// capturing the class name (group 1). The (?s) flag lets the annotation and
	// the class keyword span intervening lines.
	cdiInterceptorClassRE = regexp.MustCompile(
		`(?s)@Interceptor\b[^{]*?\bclass\s+(\w+)`)

	// cdiAroundInvokeRE matches an @AroundInvoke method, capturing the method
	// name that follows (group 1 via a separate scan window).
	cdiAroundInvokeRE = regexp.MustCompile(`@AroundInvoke\b`)

	// cdiAroundConstructRE matches an @AroundConstruct method.
	cdiAroundConstructRE = regexp.MustCompile(`@AroundConstruct\b`)

	// cdiInterceptorBindingRE matches an @InterceptorBinding annotation
	// declaration, capturing the annotation type name (group 1). Uses [^;]*?
	// (stop at statement boundary) rather than [^{]*? so it crosses
	// @Target({...}) without being blocked by the inner { character.
	cdiInterceptorBindingRE = regexp.MustCompile(
		`(?s)@InterceptorBinding\b[^;]*?@interface\s+(\w+)`)

	// cdiAdviceMethodNameRE resolves the declared method name immediately after
	// an advice annotation (same window-scan approach as spring_aop.go).
	cdiAdviceMethodNameRE = regexp.MustCompile(
		`(?s)^[^{(]*?` +
			`(?:(?:public|protected|private|final|static|synchronized)\s+)*` +
			`(?:<[^>]*>\s*)?` +
			`(?:[\w.]+(?:\s*<[^>]*>)?(?:\[\])?\s+)` +
			`(\w+)\s*\(`)
)

// canonicalCDIFramework normalises a CDI framework alias to its canonical name.
func canonicalCDIFramework(framework string) string {
	switch framework {
	case "jakarta_ee", "jakarta-ee", "jakartaee", "java_ee", "javaee":
		return "jakarta_ee"
	case "jaxrs", "jax-rs":
		return "jaxrs"
	case "quarkus":
		return "quarkus"
	default:
		return framework
	}
}

// cdiAdviceMethodName extracts the declared method name from source starting
// at offset from (just past the matched annotation end). Mirrors
// aopAdviceMethodName in spring_aop.go.
func cdiAdviceMethodName(source string, from int) string {
	end := from + 400
	if end > len(source) {
		end = len(source)
	}
	window := source[from:end]
	m := cdiAdviceMethodNameRE.FindStringSubmatch(window)
	if m == nil {
		return ""
	}
	return m[1]
}

// cdiInterceptorInfo is bookkeeping for a detected @Interceptor class.
type cdiInterceptorInfo struct {
	offset int
	ref    string
}

// findEnclosingInterceptor returns the @Interceptor class that most closely
// precedes offset in source.
func findEnclosingInterceptor(source string, offset int, interceptors map[string]cdiInterceptorInfo) string {
	best := ""
	bestOff := -1
	for name, ii := range interceptors {
		if ii.offset <= offset && ii.offset > bestOff {
			best = name
			bestOff = ii.offset
		}
	}
	return best
}

// ExtractCDIInterceptors runs the CDI interceptor / AOP extractor for
// jakarta-ee, jaxrs, and quarkus.
func ExtractCDIInterceptors(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !cdiFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	framework := canonicalCDIFramework(ctx.Framework)
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// 1. aspect_extraction: @Interceptor classes.
	interceptors := make(map[string]cdiInterceptorInfo)
	for _, m := range cdiInterceptorClassRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := "scope:pattern:aspect:" + fp + ":" + className
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Pattern", Subtype: "aspect",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_CDI_INTERCEPTOR", Ref: ref,
			Properties: map[string]any{
				"kind":        "interceptor",
				"interceptor": className,
				"framework":   framework,
			},
		}) {
			interceptors[className] = cdiInterceptorInfo{offset: m[0], ref: ref}
		}
	}

	// 2. pointcut_resolution: @InterceptorBinding annotation-type declarations.
	//    These act as binding qualifiers (CDI's pointcut equivalent).
	for _, m := range cdiInterceptorBindingRE.FindAllStringSubmatchIndex(source, -1) {
		annotName := source[m[2]:m[3]]
		ref := "scope:pattern:pointcut:" + fp + ":" + annotName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: annotName, Kind: "SCOPE.Pattern", Subtype: "pointcut",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_CDI_INTERCEPTOR_BINDING", Ref: ref,
			Properties: map[string]any{
				"kind":      "interceptor_binding",
				"binding":   annotName,
				"framework": framework,
			},
		})
	}

	// Only emit advice entities for files that actually declare an interceptor
	// (same guard as spring_aop.go for @Aspect).
	if len(interceptors) == 0 {
		return result
	}

	// 3. advice_attribution: @AroundInvoke methods.
	for _, m := range cdiAroundInvokeRE.FindAllStringIndex(source, -1) {
		owner := findEnclosingInterceptor(source, m[0], interceptors)
		if owner == "" {
			continue
		}
		methodName := cdiAdviceMethodName(source, m[1])
		name := owner + ".@AroundInvoke"
		if methodName != "" {
			name = owner + "." + methodName
		}
		props := map[string]any{
			"kind":        "advice",
			"advice_type": "around_invoke",
			"interceptor": owner,
			"framework":   framework,
		}
		if methodName != "" {
			props["method"] = methodName
		}
		ref := "scope:pattern:advice:" + fp + ":" + name
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Pattern", Subtype: "advice",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_CDI_AROUND_INVOKE", Ref: ref,
			Properties: props,
		}) {
			// OWNS edge: interceptor owns its advice.
			if ii, ok := interceptors[owner]; ok {
				addRel(&result, seenRels, Relationship{
					SourceRef: ii.ref, TargetRef: ref, RelationshipType: "OWNS",
				})
			}
		}
	}

	// 4. advice_attribution: @AroundConstruct methods.
	for _, m := range cdiAroundConstructRE.FindAllStringIndex(source, -1) {
		owner := findEnclosingInterceptor(source, m[0], interceptors)
		if owner == "" {
			continue
		}
		methodName := cdiAdviceMethodName(source, m[1])
		name := owner + ".@AroundConstruct"
		if methodName != "" {
			name = owner + "." + methodName
		}
		props := map[string]any{
			"kind":        "advice",
			"advice_type": "around_construct",
			"interceptor": owner,
			"framework":   framework,
		}
		if methodName != "" {
			props["method"] = methodName
		}
		ref := "scope:pattern:advice:" + fp + ":" + name
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Pattern", Subtype: "advice",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_CDI_AROUND_CONSTRUCT", Ref: ref,
			Properties: props,
		}) {
			if ii, ok := interceptors[owner]; ok {
				addRel(&result, seenRels, Relationship{
					SourceRef: ii.ref, TargetRef: ref, RelationshipType: "OWNS",
				})
			}
		}
	}

	return result
}
