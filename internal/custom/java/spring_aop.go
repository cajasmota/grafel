package java

import "regexp"

// Spring AOP / AspectJ custom extractor: @Aspect / @Pointcut / advice
// (@Before/@After/@Around/@AfterReturning/@AfterThrowing) extraction for the
// Spring backend frameworks (#3004, epic #2847).
//
// Covers the AOP lane:
//   - aspect_extraction: detect @Aspect-annotated classes and emit a
//     SCOPE.Pattern(subtype=aspect) entity (kind=aspect property).
//   - advice_attribution: detect advice methods inside an @Aspect class and
//     emit a SCOPE.Pattern(subtype=advice) entity carrying the advice_type
//     (before/after/around/after_returning/after_throwing) plus the pointcut
//     expression string; link each advice to its enclosing aspect via OWNS.
//   - pointcut_resolution: detect @Pointcut methods and emit a
//     SCOPE.Pattern(subtype=pointcut) entity carrying the pointcut expression;
//     link advice methods to their named pointcut via REFERENCES when the
//     advice annotation names a pointcut method declared in the same file.
//
// Reuses the SCOPE.Pattern entity Kind (matching transactional.go) so no new
// entity Kind needs registering. Aspect/advice/pointcut role is recorded in the
// `kind` property and in the entity Subtype.

// aopFrameworks gates the frameworks for which Spring AOP extraction runs.
// Spring AOP / AspectJ proxying is idiomatic only on the Spring frameworks;
// other JVM frameworks are intentionally left red (see issue #3004).
// Kotlin Spring Boot uses the same @Aspect/@Pointcut/@Before/@After/@Around
// annotation syntax, so Kotlin is admitted under the same Spring framework IDs.
var aopFrameworks = map[string]bool{
	"spring_boot": true, "spring-boot": true, "springboot": true,
	"spring_webflux": true, "spring-webflux": true, "springwebflux": true,
}

var (
	// aopAspectClassRE matches an @Aspect-annotated class declaration, capturing
	// the class name (group 1). The `(?s)` flag lets the annotation and the class
	// keyword span intervening lines (e.g. an interleaved @Component). Matching
	// stops before the class body `{` so unrelated annotations between @Aspect
	// and `class` are tolerated but a method body is not crossed.
	aopAspectClassRE = regexp.MustCompile(
		`(?s)@Aspect\b[^{]*?\bclass\s+(\w+)`)

	// aopAdviceRE matches an advice annotation with a pointcut expression string,
	// capturing the advice annotation name (group 1) and the expression / named
	// pointcut reference (group 2). Handles the explicit attribute forms
	// @AfterReturning(pointcut="...", returning="x") and
	// @Around(value="...") by anchoring on the first double-quoted string.
	aopAdviceRE = regexp.MustCompile(
		`@(Before|After|Around|AfterReturning|AfterThrowing)\s*\(\s*` +
			`(?:value\s*=\s*|pointcut\s*=\s*)?"([^"]*)"`)

	// aopPointcutRE matches a @Pointcut method declaration, capturing the
	// pointcut expression (group 1) and the declaring method name (group 2).
	// Handles both Java (`void name()`) and Kotlin (`fun name()`) syntax so
	// that Kotlin Spring Boot @Pointcut methods are correctly recognised.
	aopPointcutRE = regexp.MustCompile(
		`@Pointcut\s*\(\s*"([^"]*)"\s*\)\s*` +
			`(?:(?:public|protected|private)\s+)?(?:abstract\s+)?(?:void|fun)\s+(\w+)`)

	// aopAdviceMethodNameRE captures the method name that follows an advice
	// annotation (skipping modifiers / return type) so an advice entity can be
	// named <Aspect>.<method>.
	aopAdviceMethodNameRE = regexp.MustCompile(
		`(?s)^[^{(]*?` +
			`(?:(?:public|protected|private|final|static|synchronized)\s+)*` +
			`(?:<[^>]*>\s*)?` +
			`(?:[\w.]+(?:\s*<[^>]*>)?(?:\[\])?\s+)` +
			`(\w+)\s*\(`)

	// aopPointcutRefRE pulls a bare pointcut method reference out of an advice
	// expression, e.g. "serviceMethods()" or "com.x.Aspect.serviceMethods()".
	// Captures the simple method name (group 1).
	aopPointcutRefRE = regexp.MustCompile(`(?:\b\w+\.)*(\w+)\s*\(\s*\)`)
)

// aopAdviceType normalises an advice annotation name to a snake_case
// advice_type property value.
func aopAdviceType(annotation string) string {
	switch annotation {
	case "Before":
		return "before"
	case "After":
		return "after"
	case "Around":
		return "around"
	case "AfterReturning":
		return "after_returning"
	case "AfterThrowing":
		return "after_throwing"
	default:
		return annotation
	}
}

// canonicalAOPFramework normalises a framework alias to its canonical name for
// the entity `framework` property.
func canonicalAOPFramework(framework string) string {
	switch framework {
	case "spring_boot", "spring-boot", "springboot":
		return "spring_boot"
	case "spring_webflux", "spring-webflux", "springwebflux":
		return "spring_webflux"
	default:
		return framework
	}
}

// aopReferencedPointcut extracts a named pointcut reference from an advice
// pointcut expression. It returns the simple method name when the expression is
// a single bare pointcut reference (the form that resolves to a @Pointcut in
// the same aspect), or "" when the expression is an inline AspectJ designator
// (execution(...), within(...), @annotation(...), etc.) that does not name a
// declared pointcut.
func aopReferencedPointcut(expr string) string {
	// Inline designators contain a designator keyword followed by args; they are
	// not simple pointcut references. Heuristic: a named reference is exactly one
	// `name()` token with nothing else of substance.
	m := aopPointcutRefRE.FindStringSubmatch(expr)
	if m == nil {
		return ""
	}
	name := m[1]
	// Reject AspectJ pointcut designators that share the call() shape.
	switch name {
	case "execution", "within", "withincode", "this", "target", "args",
		"bean", "annotation", "call", "get", "set", "handler",
		"initialization", "preinitialization", "staticinitialization",
		"cflow", "cflowbelow", "adviceexecution":
		return ""
	}
	return name
}

// ExtractSpringAOP runs the Spring AOP / AspectJ extractor.
// Accepts both Java and Kotlin source: Kotlin Spring Boot uses the same
// @Aspect/@Pointcut/@Before/@After/@Around annotation syntax before class/fun
// declarations (patterns are regex-equivalent in Kotlin).
func ExtractSpringAOP(ctx PatternContext) PatternResult {
	var result PatternResult
	if (ctx.Language != "java" && ctx.Language != "kotlin") || !aopFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	framework := canonicalAOPFramework(ctx.Framework)
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// 1. aspect_extraction: @Aspect classes.
	aspects := make(map[string]aspectInfo)
	for _, m := range aopAspectClassRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := "scope:pattern:aspect:" + fp + ":" + className
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Pattern", Subtype: "aspect",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_ASPECT", Ref: ref,
			Properties: map[string]any{
				"kind":      "aspect",
				"aspect":    className,
				"framework": framework,
			},
		}) {
			if _, ok := aspects[className]; !ok {
				aspects[className] = aspectInfo{offset: m[0], ref: ref}
			}
		}
	}

	// Only emit advice / pointcut entities for files that actually declare an
	// aspect; advice annotations are meaningless outside an @Aspect class.
	if len(aspects) == 0 {
		return result
	}

	// 2. pointcut_resolution: @Pointcut methods. Map method name -> ref so advice
	//    can link to a named pointcut declared in the same file.
	pointcutRefByName := make(map[string]string)
	for _, m := range aopPointcutRE.FindAllStringSubmatchIndex(source, -1) {
		expr := source[m[2]:m[3]]
		pcName := source[m[4]:m[5]]
		owner := findEnclosingAspect(source, m[0], aspects)
		name := pcName
		if owner != "" {
			name = owner + "." + pcName
		}
		ref := "scope:pattern:pointcut:" + fp + ":" + name
		props := map[string]any{
			"kind":                "pointcut",
			"pointcut":            pcName,
			"pointcut_expression": expr,
			"framework":           framework,
		}
		if owner != "" {
			props["aspect"] = owner
		}
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Pattern", Subtype: "pointcut",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_POINTCUT", Ref: ref,
			Properties: props,
		}) {
			pointcutRefByName[pcName] = ref
			// OWNS edge: aspect owns its pointcut.
			if owner != "" {
				if ai, ok := aspects[owner]; ok {
					addRel(&result, seenRels, Relationship{
						SourceRef: ai.ref, TargetRef: ref, RelationshipType: "OWNS",
					})
				}
			}
		}
	}

	// 3. advice_attribution: advice methods inside an @Aspect class.
	for _, m := range aopAdviceRE.FindAllStringSubmatchIndex(source, -1) {
		annotation := source[m[2]:m[3]]
		expr := source[m[4]:m[5]]
		owner := findEnclosingAspect(source, m[0], aspects)
		if owner == "" {
			// Advice annotation outside any aspect in this file; skip.
			continue
		}

		// Resolve the advice method name from the text following the annotation.
		methodName := aopAdviceMethodName(source, m[1])
		name := owner + "." + annotation
		if methodName != "" {
			name = owner + "." + methodName
		}

		props := map[string]any{
			"kind":                "advice",
			"advice_type":         aopAdviceType(annotation),
			"pointcut_expression": expr,
			"aspect":              owner,
			"framework":           framework,
		}
		if methodName != "" {
			props["method"] = methodName
		}

		ref := "scope:pattern:advice:" + fp + ":" + name
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Pattern", Subtype: "advice",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_ADVICE", Ref: ref,
			Properties: props,
		}) {
			// OWNS edge: aspect owns its advice.
			if ai, ok := aspects[owner]; ok {
				addRel(&result, seenRels, Relationship{
					SourceRef: ai.ref, TargetRef: ref, RelationshipType: "OWNS",
				})
			}
			// REFERENCES edge: advice -> named pointcut (pointcut_resolution).
			if pcName := aopReferencedPointcut(expr); pcName != "" {
				if pcRef, ok := pointcutRefByName[pcName]; ok {
					addRel(&result, seenRels, Relationship{
						SourceRef: ref, TargetRef: pcRef, RelationshipType: "REFERENCES",
					})
				}
			}
		}
	}

	return result
}

// findEnclosingAspect returns the name of the @Aspect class whose declaration
// most closely precedes offset, or "" if offset is not inside a known aspect.
func findEnclosingAspect(source string, offset int, aspects map[string]aspectInfo) string {
	best := ""
	bestOff := -1
	for name, ai := range aspects {
		if ai.offset <= offset && ai.offset > bestOff {
			best = name
			bestOff = ai.offset
		}
	}
	return best
}

// aspectInfo is the per-aspect bookkeeping used to attribute advice/pointcuts.
type aspectInfo struct {
	offset int
	ref    string
}

// aopAdviceMethodName extracts the method name declared immediately after an
// advice annotation. `from` is the offset just past the matched annotation. It
// scans a bounded window so a malformed annotation cannot run away.
func aopAdviceMethodName(source string, from int) string {
	end := from + 400
	if end > len(source) {
		end = len(source)
	}
	window := source[from:end]
	m := aopAdviceMethodNameRE.FindStringSubmatch(window)
	if m == nil {
		return ""
	}
	return m[1]
}
