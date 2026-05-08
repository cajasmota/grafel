package java

import "regexp"

// Quarkus custom extractor: JAX-RS endpoints, Panache ORM/Mongo, CDI.
// Ported from: quarkus_extractor.py

var quarkusFrameworks = map[string]bool{"quarkus": true}

var (
	qkClassPathRE = regexp.MustCompile(
		`(?s)@Path\s*\(\s*\"([^\"]*)\"\s*\)[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	qkJAXRSMethodRE = regexp.MustCompile(
		`(?s)@(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b` +
			`(?:\s*@Path\s*\(\s*\"([^\"]*)\"\s*\))?\s*` +
			`[^(]*?(?:public|protected|private|)\s+(?:static\s+)?(?:<[^>]*>\s*)?` +
			`(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
	qkJAXRSMethodAltRE = regexp.MustCompile(
		`(?s)@Path\s*\(\s*\"([^\"]*)\"\s*\)\s*` +
			`@(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b` +
			`[^(]*?(?:public|protected|private|)\s+(?:static\s+)?(?:<[^>]*>\s*)?` +
			`(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
	qkPanacheEntityRE = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+extends\s+` +
			`(PanacheEntity|PanacheEntityBase)\b`)
	qkPanacheMongoEntityRE = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+extends\s+` +
			`(PanacheMongoEntity|PanacheMongoEntityBase|ReactivePanacheMongoEntity|ReactivePanacheMongoEntityBase)\b`)
	qkPanacheRepoRE = regexp.MustCompile(
		`(?s)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+` +
			`(?:extends\s+\w+\s+)?implements\s+[^{]*?(PanacheRepository|PanacheRepositoryBase)\s*<`)
	qkCDIScopedRE = regexp.MustCompile(
		`(?s)@(ApplicationScoped|RequestScoped|Singleton)\b` +
			`[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	qkInjectFieldRE = regexp.MustCompile(
		`(?s)@Inject\b[^;{(]*?(?:private|protected|public|)\s+(?:final\s+)?` +
			`(\w+)(?:\s*<[^>]*>)?\s+\w+\s*;`)
	qkInjectCtorRE = regexp.MustCompile(
		`(?s)@Inject\b[^(]*?(?:public|protected)\s+(\w+)\s*\(((?:[^)]+))\)\s*(?:throws[^{]*)?\{`)
)

// ExtractQuarkus runs the Quarkus extractor.
func ExtractQuarkus(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !quarkusFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)
	allClassRefs := make(map[string]string)

	// JAX-RS class paths
	classPaths := make(map[string]string)
	classOffsets := make(map[string]int)
	for _, m := range qkClassPathRE.FindAllStringSubmatchIndex(source, -1) {
		pathVal := source[m[2]:m[3]]
		clsName := source[m[4]:m[5]]
		if _, ok := classPaths[clsName]; !ok {
			classPaths[clsName] = pathVal
			classOffsets[clsName] = m[0]
		}
	}

	// Endpoints - collect from both regex patterns
	type endpointMatch struct {
		httpVerb   string
		methodPath string
		methodName string
		offset     int
	}
	var endpointMatches []endpointMatch

	// Pattern 1: @GET @Path("...") method
	for _, m := range qkJAXRSMethodRE.FindAllStringSubmatchIndex(source, -1) {
		httpVerb := source[m[2]:m[3]]
		methodPath := ""
		if m[4] >= 0 {
			methodPath = source[m[4]:m[5]]
		}
		methodName := source[m[6]:m[7]]
		endpointMatches = append(endpointMatches, endpointMatch{httpVerb, methodPath, methodName, m[0]})
	}
	// Pattern 2: @Path("...") @GET method
	for _, m := range qkJAXRSMethodAltRE.FindAllStringSubmatchIndex(source, -1) {
		methodPath := source[m[2]:m[3]]
		httpVerb := source[m[4]:m[5]]
		methodName := source[m[6]:m[7]]
		endpointMatches = append(endpointMatches, endpointMatch{httpVerb, methodPath, methodName, m[0]})
	}

	foundMethods := make(map[string]bool)
	for _, em := range endpointMatches {
		httpVerb := em.httpVerb
		methodPath := em.methodPath
		methodName := em.methodName

		ownerName := findOwningPathClass(source, em.offset, classPaths, classOffsets)
		if ownerName == "" {
			ownerName = findEnclosingClass(source, em.offset)
			if ownerName == "" {
				continue
			}
		}

		key := ownerName + "." + methodName
		if foundMethods[key] {
			continue
		}
		foundMethods[key] = true

		basePath := classPaths[ownerName]
		fullPath := joinPaths(basePath, methodPath)
		ref := "scope:operation:quarkus_jaxrs_endpoint:" + fp + ":" + ownerName + "." + methodName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerName + "." + methodName, Kind: "SCOPE.Operation",
			Subtype: "endpoint", SourceFile: fp,
			LineStart: lineOf(source, em.offset), LineEnd: lineOf(source, em.offset),
			Provenance: "INFERRED_FROM_QUARKUS_JAXRS_ENDPOINT", Ref: ref,
			Properties: map[string]any{
				"http_method": httpVerb, "path": fullPath,
				"resource_class": ownerName, "framework": "quarkus",
			},
		})
	}

	// Panache ORM entities — class-like AST constructs (extend PanacheEntity/PanacheEntityBase).
	// MX-1096 (Option A): all class-like constructs → SCOPE.Component (strict component rule).
	// Reverts MX-1092 Schema assignment; aligns with Python convention.
	for _, m := range qkPanacheEntityRE.FindAllStringSubmatchIndex(source, -1) {
		clsName := source[m[2]:m[3]]
		panacheType := source[m[4]:m[5]]
		ref := "scope:component:quarkus_panache_entity:" + fp + ":" + clsName
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: clsName, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_QUARKUS_PANACHE_ENTITY", Ref: ref,
			Properties: map[string]any{"panache_type": panacheType, "framework": "quarkus"},
		}) {
			allClassRefs[clsName] = ref
		}
	}

	// Panache Mongo entities — class-like AST constructs stored in MongoDB collections.
	// MX-1096 (Option A): all class-like constructs → SCOPE.Component (strict component rule).
	// Reverts MX-1092 Schema assignment; aligns with Python convention.
	for _, m := range qkPanacheMongoEntityRE.FindAllStringSubmatchIndex(source, -1) {
		clsName := source[m[2]:m[3]]
		panacheType := source[m[4]:m[5]]
		ref := "scope:component:quarkus_panache_mongo_entity:" + fp + ":" + clsName
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: clsName, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_QUARKUS_PANACHE_MONGO_ENTITY", Ref: ref,
			Properties: map[string]any{"panache_type": panacheType, "framework": "quarkus"},
		}) {
			allClassRefs[clsName] = ref
		}
	}

	// Panache repositories
	for _, m := range qkPanacheRepoRE.FindAllStringSubmatchIndex(source, -1) {
		clsName := source[m[2]:m[3]]
		panacheType := source[m[4]:m[5]]
		ref := "scope:component:quarkus_panache_repository:" + fp + ":" + clsName
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: clsName, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_QUARKUS_PANACHE_REPOSITORY", Ref: ref,
			Properties: map[string]any{"panache_type": panacheType, "framework": "quarkus"},
		}) {
			allClassRefs[clsName] = ref
		}
	}

	// CDI scoped beans
	for _, m := range qkCDIScopedRE.FindAllStringSubmatchIndex(source, -1) {
		scope := source[m[2]:m[3]]
		clsName := source[m[4]:m[5]]
		ref := "scope:service:quarkus_cdi_bean:" + fp + ":" + clsName
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: clsName, Kind: "SCOPE.Service", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_QUARKUS_CDI_BEAN", Ref: ref,
			Properties: map[string]any{"cdi_scope": scope, "framework": "quarkus"},
		}) {
			allClassRefs[clsName] = ref
		}
	}

	// CDI @Inject field injection
	for _, m := range qkInjectFieldRE.FindAllStringSubmatchIndex(source, -1) {
		injectedType := source[m[2]:m[3]]
		if primitiveTypes[injectedType] {
			continue
		}
		ownerCls := findEnclosingClass(source, m[0])
		if ownerCls == "" {
			continue
		}
		ownerRef := allClassRefs[ownerCls]
		if ownerRef == "" {
			ownerRef = "scope:dependency:quarkus:" + fp + ":" + ownerCls
		}
		targetRef := findRefForType(injectedType, fp, "quarkus", &result)
		addRel(&result, seenRels, Relationship{
			SourceRef: ownerRef, TargetRef: targetRef, RelationshipType: "DEPENDS_ON",
			Properties: map[string]string{"injected_type": injectedType, "injection_kind": "cdi_inject"},
		})
	}

	// CDI constructor injection
	for _, m := range qkInjectCtorRE.FindAllStringSubmatchIndex(source, -1) {
		ctorClass := source[m[2]:m[3]]
		paramsStr := source[m[4]:m[5]]
		ownerRef := allClassRefs[ctorClass]
		if ownerRef == "" {
			continue
		}
		for _, pm := range constructorParamRE.FindAllStringSubmatch(paramsStr, -1) {
			injectedType := pm[1]
			if primitiveTypes[injectedType] {
				continue
			}
			targetRef := findRefForType(injectedType, fp, "quarkus", &result)
			addRel(&result, seenRels, Relationship{
				SourceRef: ownerRef, TargetRef: targetRef, RelationshipType: "DEPENDS_ON",
				Properties: map[string]string{"injected_type": injectedType, "injection_kind": "cdi_constructor"},
			})
		}
	}

	return result
}

func findOwningPathClass(source string, offset int,
	classPaths map[string]string, classOffsets map[string]int) string {
	var ownerName string
	for clsName, clsOffset := range classOffsets {
		if clsOffset <= offset {
			if ownerName == "" || clsOffset > classOffsets[ownerName] {
				ownerName = clsName
			}
		}
	}
	return ownerName
}
