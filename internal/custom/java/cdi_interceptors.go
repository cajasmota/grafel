package java

import "regexp"

// CDI Interceptor / AOP extractor: @Interceptor / @AroundInvoke / @InterceptorBinding
// for jakarta-ee, jaxrs, and quarkus frameworks (issue #3082).
//
// Covers the CDI interceptor lane across three frameworks:
//   - aspect_extraction:    detect @Interceptor-annotated classes and emit a
//     SCOPE.Pattern(subtype=aspect) entity (kind=cdi_interceptor).
//   - advice_attribution:   detect @AroundInvoke / @AroundConstruct methods inside
//     an @Interceptor class and emit SCOPE.Pattern(subtype=advice) entities carrying
//     the advice_type (around_invoke / around_construct), linked to the interceptor
//     class via OWNS edges.
//   - pointcut_resolution:  detect @InterceptorBinding annotation declarations and
//     emit SCOPE.Pattern(subtype=pointcut) entities (kind=interceptor_binding),
//     representing the binding that selects the interceptors to apply.
//
// CDI interceptors use @InterceptorBinding as a selector mechanism rather than
// AspectJ pointcut expressions. The pointcut_resolution cell is evidenced by
// detecting @InterceptorBinding-annotated annotation type declarations and by
// recording the interceptor_binding_type on @Interceptor classes that carry them.
//
// Reuses the SCOPE.Pattern entity Kind (matching transactional.go and spring_aop.go)
// so no new entity Kind registration is required.

// cdiFrameworks gates the frameworks for which CDI interceptor extraction runs.
// MicroProfile is a CDI-based spec built on Jakarta EE; its interceptor model
// is identical to Jakarta EE CDI, so it shares the same extractor (#3175).
var cdiFrameworks = map[string]bool{
	"jakarta_ee": true, "jakarta-ee": true, "jakartaee": true,
	"java_ee": true, "javaee": true,
	"jaxrs": true, "jax-rs": true, "jax_rs": true,
	"quarkus":      true,
	"microprofile": true, "micro_profile": true, "micro-profile": true,
}

var (
	// cdiInterceptorClassRE detects a class annotated with @Interceptor (CDI),
	// capturing the class name (group 1). The (?s) flag allows the annotation
	// and class keyword to span intervening lines.
	cdiInterceptorClassRE = regexp.MustCompile(
		`(?s)@Interceptor\b[^{]*?\bclass\s+(\w+)`)

	// cdiAroundInvokeRE detects an @AroundInvoke-annotated method, used to
	// intercept business method invocations.
	cdiAroundInvokeRE = regexp.MustCompile(
		`(?m)@AroundInvoke\b`)

	// cdiAroundConstructRE detects an @AroundConstruct-annotated method, used to
	// intercept constructor invocations.
	cdiAroundConstructRE = regexp.MustCompile(
		`(?m)@AroundConstruct\b`)

	// cdiAroundInvokeMethodRE captures the method name following @AroundInvoke.
	// Scans up to 400 chars after the annotation to find the method signature.
	cdiAroundInvokeMethodRE = regexp.MustCompile(
		`(?s)@AroundInvoke\b\s*` +
			`(?:(?:public|protected|private|final|static|synchronized)\s+)*` +
			`(?:<[^>]*>\s*)?` +
			`(?:[\w.]+(?:\s*<[^>]*>)?(?:\[\])?\s+)` +
			`(\w+)\s*\(`)

	// cdiAroundConstructMethodRE captures the method name following @AroundConstruct.
	cdiAroundConstructMethodRE = regexp.MustCompile(
		`(?s)@AroundConstruct\b\s*` +
			`(?:(?:public|protected|private|final|static|synchronized)\s+)*` +
			`(?:<[^>]*>\s*)?` +
			`(?:[\w.]+(?:\s*<[^>]*>)?(?:\[\])?\s+)` +
			`(\w+)\s*\(`)

	// cdiInterceptorBindingRE detects a custom @InterceptorBinding annotation type
	// declaration, capturing the annotation type name (group 1).
	// Example:
	//   @InterceptorBinding
	//   @Retention(RUNTIME)
	//   @Target({METHOD, TYPE})
	//   public @interface Logged {}
	// The inner `(?:[^{]|\{[^}]*\})*?` tolerates `@Target({...})` blocks which
	// contain braces that `[^{]*?` would stop at prematurely.
	cdiInterceptorBindingRE = regexp.MustCompile(
		`(?s)@InterceptorBinding\b(?:[^{]|\{[^}]*\})*?@interface\s+(\w+)`)

	// cdiClassInterceptorBindingRE detects an interceptor class that is itself
	// annotated with a custom binding annotation (the binding reference on the
	// interceptor class), capturing the binding annotation name (group 1) and the
	// interceptor class name (group 2). A heuristic: scan for any capitalised
	// annotation followed, eventually, by @Interceptor then class.
	// This RE finds annotations on the same block as @Interceptor.
	cdiClassBindingAnnotationRE = regexp.MustCompile(
		`(?s)(@\w+)\s*(?:\([^)]*\)\s*)?@Interceptor\b[^{]*?\bclass\s+(\w+)`)
)

// canonicalCDIFramework normalises a framework alias to its canonical name.
func canonicalCDIFramework(framework string) string {
	switch framework {
	case "jakarta_ee", "jakarta-ee", "jakartaee", "java_ee", "javaee":
		return "jakarta_ee"
	case "jaxrs", "jax-rs", "jax_rs":
		return "jaxrs"
	case "quarkus":
		return "quarkus"
	default:
		return framework
	}
}

// cdiInterceptorInfo is per-interceptor bookkeeping for advice attribution.
type cdiInterceptorInfo struct {
	offset int
	ref    string
}

// ExtractCDIInterceptors runs the CDI interceptor / AOP extractor for
// jakarta-ee, jaxrs, and quarkus frameworks. Accepts both Java and Kotlin
// source: Kotlin Quarkus uses the same @Interceptor/@AroundInvoke/
// @InterceptorBinding annotations before class/fun declarations (regex
// patterns match identically in both languages).
func ExtractCDIInterceptors(ctx PatternContext) PatternResult {
	var result PatternResult
	if (ctx.Language != "java" && ctx.Language != "kotlin") || !cdiFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	framework := canonicalCDIFramework(ctx.Framework)
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// -------------------------------------------------------------------------
	// 1. aspect_extraction: @Interceptor-annotated classes.
	// -------------------------------------------------------------------------
	interceptors := make(map[string]cdiInterceptorInfo)
	for _, m := range cdiInterceptorClassRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := "scope:pattern:cdi_interceptor:" + fp + ":" + className
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name:       className,
			Kind:       "SCOPE.Pattern",
			Subtype:    "aspect",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]),
			LineEnd:    lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_CDI_INTERCEPTOR",
			Ref:        ref,
			Properties: map[string]any{
				"kind":      "cdi_interceptor",
				"aspect":    className,
				"framework": framework,
			},
		}) {
			interceptors[className] = cdiInterceptorInfo{offset: m[0], ref: ref}
		}
	}

	// Only emit advice entities if this file declares at least one interceptor.
	if len(interceptors) > 0 {
		// -----------------------------------------------------------------------
		// 2. advice_attribution: @AroundInvoke methods.
		// -----------------------------------------------------------------------
		for _, m := range cdiAroundInvokeMethodRE.FindAllStringSubmatchIndex(source, -1) {
			methodName := source[m[2]:m[3]]
			owner := cdiEnclosingInterceptor(source, m[0], interceptors)
			if owner == "" {
				continue
			}
			name := owner + "." + methodName
			ref := "scope:pattern:cdi_advice:" + fp + ":" + name
			if addEntity(&result, seenRefs, SecondaryEntity{
				Name:       name,
				Kind:       "SCOPE.Pattern",
				Subtype:    "advice",
				SourceFile: fp,
				LineStart:  lineOf(source, m[0]),
				LineEnd:    lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_AROUND_INVOKE",
				Ref:        ref,
				Properties: map[string]any{
					"kind":        "advice",
					"advice_type": "around_invoke",
					"method":      methodName,
					"aspect":      owner,
					"framework":   framework,
				},
			}) {
				// OWNS edge: interceptor class owns its advice method.
				if ai, ok := interceptors[owner]; ok {
					addRel(&result, seenRels, Relationship{
						SourceRef:        ai.ref,
						TargetRef:        ref,
						RelationshipType: "OWNS",
					})
				}
			}
		}

		// -----------------------------------------------------------------------
		// 3. advice_attribution: @AroundConstruct methods.
		// -----------------------------------------------------------------------
		for _, m := range cdiAroundConstructMethodRE.FindAllStringSubmatchIndex(source, -1) {
			methodName := source[m[2]:m[3]]
			owner := cdiEnclosingInterceptor(source, m[0], interceptors)
			if owner == "" {
				continue
			}
			name := owner + "." + methodName
			ref := "scope:pattern:cdi_advice:" + fp + ":" + name
			if addEntity(&result, seenRefs, SecondaryEntity{
				Name:       name,
				Kind:       "SCOPE.Pattern",
				Subtype:    "advice",
				SourceFile: fp,
				LineStart:  lineOf(source, m[0]),
				LineEnd:    lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_AROUND_CONSTRUCT",
				Ref:        ref,
				Properties: map[string]any{
					"kind":        "advice",
					"advice_type": "around_construct",
					"method":      methodName,
					"aspect":      owner,
					"framework":   framework,
				},
			}) {
				if ai, ok := interceptors[owner]; ok {
					addRel(&result, seenRels, Relationship{
						SourceRef:        ai.ref,
						TargetRef:        ref,
						RelationshipType: "OWNS",
					})
				}
			}
		}
	}

	// -------------------------------------------------------------------------
	// 4. pointcut_resolution: @InterceptorBinding annotation type declarations.
	// -------------------------------------------------------------------------
	for _, m := range cdiInterceptorBindingRE.FindAllStringSubmatchIndex(source, -1) {
		bindingName := source[m[2]:m[3]]
		ref := "scope:pattern:interceptor_binding:" + fp + ":" + bindingName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name:       bindingName,
			Kind:       "SCOPE.Pattern",
			Subtype:    "pointcut",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]),
			LineEnd:    lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_INTERCEPTOR_BINDING",
			Ref:        ref,
			Properties: map[string]any{
				"kind":      "interceptor_binding",
				"binding":   bindingName,
				"framework": framework,
			},
		})
	}

	return result
}

// cdiEnclosingInterceptor returns the name of the @Interceptor class whose
// declaration most closely precedes offset, or "" if none found.
func cdiEnclosingInterceptor(source string, offset int, interceptors map[string]cdiInterceptorInfo) string {
	best := ""
	bestOff := -1
	for name, info := range interceptors {
		if info.offset <= offset && info.offset > bestOff {
			best = name
			bestOff = info.offset
		}
	}
	return best
}
