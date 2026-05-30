package java

import "regexp"

// Micronaut AOP interceptor + HttpServerFilter middleware extractor.
//
// Covers four cells for lang.java.framework.micronaut (#3084):
//
//	AOP.advice_attribution      (missing → partial)
//	AOP.aspect_extraction       (missing → partial)
//	AOP.pointcut_resolution     (missing → partial)
//	Middleware.middleware_coverage (missing → partial)
//
// Micronaut AOP uses a different model than Spring AOP:
//   - An interceptor class implements MethodInterceptor<T, R> (or its reactive
//     variant) and is bound to targets via @InterceptorBean(SomeAnnotation.class).
//   - The interceptor class itself is annotated with @Singleton (or @Prototype),
//     which makes it a bean, plus optionally @Around to mark it as an around-advice.
//   - The binding annotation (custom @interface annotated with @Around) acts as
//     the pointcut designator.
//
// Micronaut HTTP middleware uses:
//   - HttpServerFilter: interface implemented by filter beans; @Filter("/**") marks
//     the URL pattern.
//   - @ServerFilter (Micronaut 4.x alias for @Filter on the server side).
//
// No new entity Kinds are needed: SCOPE.Pattern (aspect/advice/pointcut) and
// SCOPE.Component (middleware) are already registered.

var (
	// mnAroundAnnotationRE matches a custom annotation type decorated with @Around,
	// capturing the annotation name (group 1). This is the pointcut designator.
	// Uses .{0,300}? to tolerate intervening annotations like @Retention/@Target
	// (which may contain `@` and `{`) while bounding the match window.
	mnAroundAnnotationRE = regexp.MustCompile(
		`(?s)@Around\b.{0,300}?(?:public\s+)?@interface\s+(\w+)`)

	// mnInterceptorBeanRE matches an @InterceptorBean(...) binding on a class,
	// capturing the bound annotation name (group 1) and the class name (group 2).
	mnInterceptorBeanRE = regexp.MustCompile(
		`(?s)@InterceptorBean\s*\(\s*(?:value\s*=\s*)?(\w+)\.class\s*\)` +
			`[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)

	// mnMethodInterceptorClassRE matches a class that implements MethodInterceptor,
	// capturing the class name (group 1). Handles both the generic and raw forms.
	mnMethodInterceptorClassRE = regexp.MustCompile(
		`(?s)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)` +
			`[^{]*?implements\s+[^{]*?MethodInterceptor\b`)

	// mnAroundAdviceMethodRE matches the intercept() method inside an interceptor
	// class (the canonical entry point for Micronaut MethodInterceptor), capturing
	// the method name (group 1).
	mnAroundAdviceMethodRE = regexp.MustCompile(
		`(?m)(?:public|protected)\s+(?:<[^>]*>\s*)?` +
			`(?:\w+(?:\s*<[^>]*>)?(?:\[\])?\s+)` +
			`(intercept)\s*\(`)

	// mnFilterClassRE matches a class that implements HttpServerFilter (or the
	// @Filter/@ServerFilter annotated variant), capturing the class name (group 1).
	// Form 1: @Filter("...") on the class declaration.
	mnFilterAnnotationRE = regexp.MustCompile(
		`(?s)@(?:Filter|ServerFilter)\s*` +
			`(?:\(\s*(?:value\s*=\s*)?\"([^\"]*)\"\s*\)\s*)?` +
			`[^{@]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)

	// mnHttpServerFilterImplRE matches "implements HttpServerFilter" on a class,
	// capturing the class name (group 1).
	mnHttpServerFilterImplRE = regexp.MustCompile(
		`(?s)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)` +
			`[^{]*?implements\s+[^{]*?HttpServerFilter\b`)
)

// ExtractMicronautAOP detects Micronaut AOP interceptors and HttpServerFilter
// middleware, emitting SCOPE.Pattern and SCOPE.Component entities.
// Accepts both Java and Kotlin source: Micronaut Kotlin uses the same
// @Around, @InterceptorBean, and @Filter annotations before class/fun
// declarations (regex patterns are identical in Kotlin).
func ExtractMicronautAOP(ctx PatternContext) PatternResult {
	var result PatternResult
	if (ctx.Language != "java" && ctx.Language != "kotlin") || !micronautFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// ── 1. aspect_extraction / pointcut_resolution ───────────────────────────
	// An @Around-annotated custom @interface is the Micronaut pointcut designator
	// (the binding annotation). Emit as aspect (contains the pointcut) and as
	// pointcut.

	pointcutRefByBinding := make(map[string]string) // bindingAnnotation -> ref

	for _, m := range mnAroundAnnotationRE.FindAllStringSubmatchIndex(source, -1) {
		annName := source[m[2]:m[3]]

		// Emit SCOPE.Pattern(subtype=aspect) for the binding annotation — it
		// plays the role of both the aspect designator and the pointcut in
		// Micronaut's model.
		aspectRef := "scope:pattern:aspect:" + fp + ":" + annName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: annName, Kind: "SCOPE.Pattern", Subtype: "aspect",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICRONAUT_AROUND",
			Ref:        aspectRef,
			Properties: map[string]any{
				"kind":      "aspect",
				"aspect":    annName,
				"framework": "micronaut",
			},
		})

		// Emit SCOPE.Pattern(subtype=pointcut) for the binding annotation — the
		// @Around annotation on the @interface is the pointcut declaration.
		pcRef := "scope:pattern:pointcut:" + fp + ":" + annName
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: annName, Kind: "SCOPE.Pattern", Subtype: "pointcut",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICRONAUT_AROUND_POINTCUT",
			Ref:        pcRef,
			Properties: map[string]any{
				"kind":      "pointcut",
				"pointcut":  annName,
				"framework": "micronaut",
			},
		}) {
			pointcutRefByBinding[annName] = pcRef
			// OWNS: aspect owns its pointcut.
			addRel(&result, seenRels, Relationship{
				SourceRef: aspectRef, TargetRef: pcRef, RelationshipType: "OWNS",
			})
		}
	}

	// ── 2. aspect_extraction: MethodInterceptor implementation ────────────────
	// A class that implements MethodInterceptor is an interceptor (aspect).
	// If it also has @InterceptorBean, record the binding.

	interceptorRefs := make(map[string]string) // className -> ref

	for _, m := range mnMethodInterceptorClassRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := "scope:pattern:aspect:" + fp + ":" + className
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Pattern", Subtype: "aspect",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICRONAUT_METHOD_INTERCEPTOR",
			Ref:        ref,
			Properties: map[string]any{
				"kind":      "aspect",
				"aspect":    className,
				"framework": "micronaut",
			},
		}) {
			interceptorRefs[className] = ref
		}
	}

	// ── 3. pointcut_resolution: @InterceptorBean binding ─────────────────────
	// @InterceptorBean(SomeAnnotation.class) on an interceptor class links it
	// to its binding annotation (the pointcut). Emit an OWNS edge from the
	// interceptor aspect to the pointcut, and a REFERENCES edge from advice.

	for _, m := range mnInterceptorBeanRE.FindAllStringSubmatchIndex(source, -1) {
		bindingAnn := source[m[2]:m[3]]
		className := source[m[4]:m[5]]

		// Ensure the interceptor class entity exists (may have been found above
		// or may only be declared via @InterceptorBean without explicit
		// MethodInterceptor in scope).
		interceptorRef := interceptorRefs[className]
		if interceptorRef == "" {
			interceptorRef = "scope:pattern:aspect:" + fp + ":" + className
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: className, Kind: "SCOPE.Pattern", Subtype: "aspect",
				SourceFile: fp,
				LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_MICRONAUT_INTERCEPTOR_BEAN",
				Ref:        interceptorRef,
				Properties: map[string]any{
					"kind":      "aspect",
					"aspect":    className,
					"framework": "micronaut",
				},
			})
			interceptorRefs[className] = interceptorRef
		}

		// ── 4. advice_attribution: the intercept() method is the around-advice ──
		// Emit a SCOPE.Pattern(subtype=advice) for <ClassName>.intercept.
		adviceName := className + ".intercept"
		adviceRef := "scope:pattern:advice:" + fp + ":" + adviceName
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: adviceName, Kind: "SCOPE.Pattern", Subtype: "advice",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICRONAUT_INTERCEPTOR_BEAN",
			Ref:        adviceRef,
			Properties: map[string]any{
				"kind":        "advice",
				"advice_type": "around",
				"aspect":      className,
				"binding":     bindingAnn,
				"framework":   "micronaut",
			},
		}) {
			// OWNS: interceptor class owns the advice method.
			addRel(&result, seenRels, Relationship{
				SourceRef: interceptorRef, TargetRef: adviceRef, RelationshipType: "OWNS",
			})
			// REFERENCES: advice -> pointcut (the binding annotation).
			if pcRef, ok := pointcutRefByBinding[bindingAnn]; ok {
				addRel(&result, seenRels, Relationship{
					SourceRef: adviceRef, TargetRef: pcRef, RelationshipType: "REFERENCES",
				})
			}
		}
	}

	// ── 5. advice_attribution: intercept() bodies in MethodInterceptor classes ─
	// When we detected a MethodInterceptor class but no @InterceptorBean, we
	// still emit advice entities for intercept() methods found in the source
	// so that advice_attribution has coverage even without explicit binding.
	for _, m := range mnAroundAdviceMethodRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[2]:m[3]] // always "intercept"
		ownerCls := findEnclosingClass(source, m[0])
		if ownerCls == "" {
			continue
		}
		interceptorRef := interceptorRefs[ownerCls]
		if interceptorRef == "" {
			// Not a known interceptor class; skip to avoid noise.
			continue
		}
		adviceName := ownerCls + "." + methodName
		adviceRef := "scope:pattern:advice:" + fp + ":" + adviceName
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: adviceName, Kind: "SCOPE.Pattern", Subtype: "advice",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICRONAUT_INTERCEPT_METHOD",
			Ref:        adviceRef,
			Properties: map[string]any{
				"kind":        "advice",
				"advice_type": "around",
				"aspect":      ownerCls,
				"framework":   "micronaut",
			},
		}) {
			addRel(&result, seenRels, Relationship{
				SourceRef: interceptorRef, TargetRef: adviceRef, RelationshipType: "OWNS",
			})
		}
	}

	// ── 6. middleware_coverage: HttpServerFilter / @Filter ────────────────────

	// Form A: @Filter or @ServerFilter annotated class.
	for _, m := range mnFilterAnnotationRE.FindAllStringSubmatchIndex(source, -1) {
		urlPattern := ""
		if m[2] >= 0 {
			urlPattern = source[m[2]:m[3]]
		}
		className := source[m[4]:m[5]]
		ref := "scope:component:micronaut_filter:" + fp + ":" + className
		props := map[string]any{
			"framework":  "micronaut",
			"middleware": "http_server_filter",
		}
		if urlPattern != "" {
			props["url_pattern"] = urlPattern
		}
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICRONAUT_FILTER", Ref: ref,
			Properties: props,
		})
	}

	// Form B: class implements HttpServerFilter (may lack @Filter annotation).
	for _, m := range mnHttpServerFilterImplRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := "scope:component:micronaut_filter:" + fp + ":" + className
		if seenRefs[ref] {
			continue // already emitted by Form A
		}
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICRONAUT_HTTP_SERVER_FILTER", Ref: ref,
			Properties: map[string]any{
				"framework":  "micronaut",
				"middleware": "http_server_filter",
			},
		})
	}

	return result
}
