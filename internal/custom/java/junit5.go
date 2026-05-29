package java

import (
	"regexp"
	"strings"
)

// JUnit 5 custom extractor: test methods, nested classes, lifecycle, extensions.
// Ported from: junit5_extractor.py

// junit5Frameworks lists all framework identifiers for which the JUnit 5
// extractor is active. Jakarta EE and MicroProfile projects typically use
// JUnit 5 (via Arquillian or plain JUnit) as their test runner — enabling
// tests_linkage for those records (#2996).
var junit5Frameworks = map[string]bool{
	"junit5": true, "junit-jupiter": true, "junit_jupiter": true,
	"junit_5": true, "junit 5": true,
	// Jakarta EE and MicroProfile use JUnit 5 for tests_linkage (#2996).
	"jakarta_ee": true, "jakarta-ee": true, "jakartaee": true,
	"microprofile": true, "eclipse-microprofile": true,
	"open_liberty": true, "payara": true, "helidon": true,
	// Spring Boot and Spring WebFlux projects use JUnit 5 (via @SpringBootTest /
	// @WebFluxTest) for tests_linkage (#2991).
	"spring_boot": true, "spring-boot": true, "springboot": true,
	"spring_webflux": true, "spring-webflux": true, "springwebflux": true,
}

var (
	j5TestMethodRE = regexp.MustCompile(
		`(?s)@(Test|ParameterizedTest|RepeatedTest)\b(?:\s*\([^)]*\))?` +
			`(?:\s*@\w+(?:\s*\([^)]*\))?\s*)*\s*(?:public\s+|protected\s+|package\s+|private\s+)?(?:\w+\s+)*` +
			`void\s+(\w+)\s*\(`)
	j5LifecycleRE = regexp.MustCompile(
		`(?s)@(BeforeAll|BeforeEach|AfterAll|AfterEach)\b(?:\s*\([^)]*\))?` +
			`(?:\s*@\w+(?:\s*\([^)]*\))?\s*)*\s*(?:public\s+|protected\s+|static\s+|private\s+)*` +
			`void\s+(\w+)\s*\(`)
	j5NestedClassRE = regexp.MustCompile(
		`(?s)@Nested\b(?:\s*@\w+(?:\s*\([^)]*\))?\s*)*\s*` +
			`(?:(?:public|protected|private|static|inner)\s+)*class\s+(\w+)`)
	j5ExtendWithRE = regexp.MustCompile(
		`(?s)@ExtendWith\s*\(\s*(?:\{([^}]*)\}|([^)]+))\s*\)`)
	j5DisplayNameRE = regexp.MustCompile(
		`@DisplayName\s*\(\s*\"([^\"]*)\"\s*\)`)
	j5DisabledRE = regexp.MustCompile(
		`@Disabled\b(?:\s*\(\s*\"[^\"]*\"\s*\))?`)
	j5TagRE = regexp.MustCompile(
		`@Tag\s*\(\s*\"([^\"]*)\"\s*\)`)
	j5RepeatedCountRE = regexp.MustCompile(
		`@RepeatedTest\s*\(\s*(?:value\s*=\s*)?(\d+)`)
	j5ValueSourceRE  = regexp.MustCompile(`@ValueSource\s*\([^)]*\)`)
	j5CsvSourceRE    = regexp.MustCompile(`@CsvSource\s*\([^)]*\)`)
	j5MethodSourceRE = regexp.MustCompile(`@MethodSource\s*\(\s*\"([^\"]*)\"\s*\)`)
	j5ClassExtRE     = regexp.MustCompile(`(\w+)\.class`)
)

// ExtractJUnit5 runs the JUnit 5 extractor.
func ExtractJUnit5(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !junit5Frameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// Find outer class
	outerClassMatch := classDeclRE.FindStringSubmatchIndex(source)
	var outerClassName, outerClassRef string
	if outerClassMatch != nil {
		outerClassName = source[outerClassMatch[2]:outerClassMatch[3]]
		outerClassRef = "scope:component:junit5_test_class:" + fp + ":" + outerClassName
	}

	// Nested classes with body spans
	type nestedInfo struct {
		name      string
		ref       string
		bodyStart int
		bodyEnd   int
	}
	var nestedRecords []nestedInfo
	for _, m := range j5NestedClassRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := "scope:component:junit5_nested:" + fp + ":" + className
		if seenRefs[ref] {
			continue
		}
		seenRefs[ref] = true

		props := map[string]any{"framework": "junit5"}
		// Check @DisplayName
		window := source[max(0, m[0]-400):m[0]]
		if dn := j5DisplayNameRE.FindStringSubmatch(window); dn != nil {
			props["display_name"] = dn[1]
		}

		result.Entities = append(result.Entities, SecondaryEntity{
			Name: className, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JUNIT5_NESTED", Ref: ref,
			Properties: props,
		})

		// Compute body span
		bodyOpen := strings.Index(source[m[1]:], "{")
		bodyStart := -1
		bodyEnd := -1
		if bodyOpen >= 0 {
			bodyStart = m[1] + bodyOpen
			depth := 0
			for i := bodyStart; i < len(source); i++ {
				if source[i] == '{' {
					depth++
				} else if source[i] == '}' {
					depth--
					if depth == 0 {
						bodyEnd = i
						break
					}
				}
			}
		}
		nestedRecords = append(nestedRecords, nestedInfo{className, ref, bodyStart, bodyEnd})
	}

	owningRef := func(offset int) string {
		var bestRef string
		bestStart := -1
		for _, nr := range nestedRecords {
			if nr.bodyStart >= 0 && nr.bodyEnd >= 0 &&
				nr.bodyStart < offset && offset < nr.bodyEnd &&
				nr.bodyStart > bestStart {
				bestRef = nr.ref
				bestStart = nr.bodyStart
			}
		}
		if bestRef != "" {
			return bestRef
		}
		return outerClassRef
	}

	// Test methods
	for _, m := range j5TestMethodRE.FindAllStringSubmatchIndex(source, -1) {
		ann := source[m[2]:m[3]]
		methodName := source[m[4]:m[5]]

		oRef := owningRef(m[0])
		ownerLabel := outerClassName
		if oRef != "" {
			parts := strings.Split(oRef, ":")
			ownerLabel = parts[len(parts)-1]
		}

		ref := "scope:operation:junit5_test:" + fp + ":" + ownerLabel + "." + methodName
		if seenRefs[ref] {
			continue
		}
		seenRefs[ref] = true

		props := map[string]any{"framework": "junit5", "test_annotation": ann}

		// Metadata from annotation windows
		winBefore := source[max(0, m[0]-400):m[0]]
		if dn := j5DisplayNameRE.FindStringSubmatch(winBefore); dn != nil {
			props["display_name"] = dn[1]
		}
		if j5DisabledRE.MatchString(winBefore) {
			props["disabled"] = true
		}
		for _, tag := range j5TagRE.FindAllStringSubmatch(winBefore, -1) {
			if props["tags"] == nil {
				props["tags"] = []string{}
			}
			props["tags"] = append(props["tags"].([]string), tag[1])
		}

		if ann == "ParameterizedTest" {
			props["parameterized"] = true
			winAfter := source[m[0]:min(m[0]+300, len(source))]
			if j5ValueSourceRE.MatchString(winAfter) {
				props["source_type"] = "ValueSource"
			} else if j5CsvSourceRE.MatchString(winAfter) {
				props["source_type"] = "CsvSource"
			} else if ms := j5MethodSourceRE.FindStringSubmatch(winAfter); ms != nil {
				props["source_type"] = "MethodSource"
				props["method_source"] = ms[1]
			}
		}
		if ann == "RepeatedTest" {
			props["repeated"] = true
			winAfter := source[m[0]:min(m[0]+300, len(source))]
			if rc := j5RepeatedCountRE.FindStringSubmatch(winAfter); rc != nil {
				props["repeat_count"] = rc[1]
			}
		}

		result.Entities = append(result.Entities, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JUNIT5_TEST", Ref: ref,
			Properties: props,
		})

		if oRef != "" {
			addRel(&result, seenRels, Relationship{
				SourceRef: oRef, TargetRef: ref, RelationshipType: "OWNS",
			})
		}
	}

	// Lifecycle methods
	for _, m := range j5LifecycleRE.FindAllStringSubmatchIndex(source, -1) {
		ann := source[m[2]:m[3]]
		methodName := source[m[4]:m[5]]
		oRef := owningRef(m[0])
		ownerLabel := outerClassName
		if oRef != "" {
			parts := strings.Split(oRef, ":")
			ownerLabel = parts[len(parts)-1]
		}

		ref := "scope:operation:junit5_lifecycle:" + fp + ":" + ownerLabel + "." + methodName + "." + strings.ToLower(ann)
		if seenRefs[ref] {
			continue
		}
		seenRefs[ref] = true

		result.Entities = append(result.Entities, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JUNIT5_LIFECYCLE", Ref: ref,
			Properties: map[string]any{
				"framework": "junit5", "lifecycle_annotation": ann,
			},
		})
		if oRef != "" {
			addRel(&result, seenRels, Relationship{
				SourceRef: oRef, TargetRef: ref, RelationshipType: "OWNS",
			})
		}
	}

	// @ExtendWith
	for _, m := range j5ExtendWithRE.FindAllStringSubmatchIndex(source, -1) {
		raw := ""
		if m[2] >= 0 {
			raw = source[m[2]:m[3]]
		} else if m[4] >= 0 {
			raw = source[m[4]:m[5]]
		}
		for _, ext := range j5ClassExtRE.FindAllStringSubmatch(raw, -1) {
			extName := ext[1]
			ref := "scope:pattern:junit5_extension:" + fp + ":" + extName
			if addEntity(&result, seenRefs, SecondaryEntity{
				Name: extName, Kind: "SCOPE.Pattern", SourceFile: fp,
				LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_JUNIT5_EXTENSION", Ref: ref,
				Properties: map[string]any{
					"framework": "junit5", "extension_class": extName,
				},
			}) {
				if outerClassRef != "" {
					addRel(&result, seenRels, Relationship{
						SourceRef: outerClassRef, TargetRef: ref,
						RelationshipType: "DEPENDS_ON",
						Properties:       map[string]string{"kind": "junit5_extension"},
					})
				}
			}
		}
	}

	return result
}
