package java

import "regexp"

// Spring Boot custom extractor: DI graph, request mappings, configuration beans.
// Ported from: spring_boot_extractor.py

var springBootFrameworks = map[string]bool{
	"spring_boot": true, "spring-boot": true, "springboot": true,
	// Spring WebFlux shares the same DI model (@Autowired, @Bean, @Component) as
	// Spring Boot — the same extractor handles di_binding_extraction and
	// di_injection_point for both frameworks (#2991).
	"spring_webflux": true, "spring-webflux": true, "springwebflux": true,
}

// kotlinSpringBootFrameworks is the set of Kotlin framework identifiers that
// map to Spring Boot / Spring WebFlux. Kotlin Spring Boot uses identical
// annotation syntax (@Autowired, @Bean, @Component, @Scope, etc.) so the same
// extractor applies — we just gate on language "kotlin" in addition to "java".
var kotlinSpringBootFrameworks = map[string]bool{
	"spring_boot": true, "spring-boot": true, "springboot": true,
	"spring_webflux": true, "spring-webflux": true, "springwebflux": true,
}

var (
	// actuator_detection: @Endpoint/@ReadOperation/@WriteOperation/@DeleteOperation
	sbActuatorEndpointRE = regexp.MustCompile(
		`(?s)@Endpoint\s*\(\s*id\s*=\s*"([^"]+)"\s*\)[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	sbActuatorOperationRE = regexp.MustCompile(
		`(?s)(@(?:ReadOperation|WriteOperation|DeleteOperation))\b[^{]*?` +
			`(?:public|protected|private|)\s+(?:static\s+)?(?:<[^>]*>\s*)?(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)

	// di_scope_resolution: Spring @Scope / @RequestScope / @SessionScope / @ApplicationScope
	sbScopeAnnotationRE = regexp.MustCompile(
		`(?s)@Scope\s*\(\s*(?:value\s*=\s*)?(?:ConfigurableBeanFactory\.SCOPE_(\w+)|"([^"]+)")\s*\)` +
			`\s*(?:@\w+[^{]*?)*(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	sbRequestScopeRE = regexp.MustCompile(
		`(?s)@RequestScope\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	sbSessionScopeRE = regexp.MustCompile(
		`(?s)@SessionScope\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	sbApplicationScopeRE = regexp.MustCompile(
		`(?s)@ApplicationScope\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)

	sbRestControllerRE = regexp.MustCompile(
		`(?s)@(?:Rest)?Controller\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	sbClassRequestMappingRE = regexp.MustCompile(
		`(?m)@RequestMapping\s*\(\s*(?:value\s*=\s*)?\"([^\"]*)\"\s*\)`)
	sbHTTPMappingRE = regexp.MustCompile(
		`(?s)(@(?:GetMapping|PostMapping|PutMapping|DeleteMapping|PatchMapping|RequestMapping)` +
			`\s*(?:\([^)]*\))?)\s*(?:(?:public|protected|private)\s+)?(?:static\s+)?` +
			`(?:<[^>]*>\s*)?(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
	sbServiceClassRE = regexp.MustCompile(
		`(?s)@Service\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	sbRepositoryClassRE = regexp.MustCompile(
		`(?s)@Repository\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	sbComponentClassRE = regexp.MustCompile(
		`(?s)@Component\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	sbConfigurationClassRE = regexp.MustCompile(
		`(?s)@Configuration\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	sbBeanMethodRE = regexp.MustCompile(
		`(?s)@Bean\b[^;{]*?\s+(?:public\s+|protected\s+|private\s+)?(?:static\s+)?` +
			`(?:<[^>]*>\s+)?\w+(?:\s*<[^>]*>)?\s+(\w+)\s*\(`)
	sbAutowiredFieldRE = regexp.MustCompile(
		`(?s)@Autowired\b[^;{(]*?(?:private|protected|public|)\s+(?:final\s+)?` +
			`(\w+)(?:\s*<[^>]*>)?\s+\w+\s*;`)
	sbAutowiredSetterRE = regexp.MustCompile(
		`(?s)@Autowired\b[^{]*?(?:public|protected|private)\s+void\s+\w+\s*\(\s*` +
			`(\w+)(?:\s*<[^>]*>)?\s+\w+`)
	sbConstructorInjectionRE = regexp.MustCompile(
		`(?s)(?:@Autowired\b[^(]*)?(?:public|protected)\s+(\w+)\s*\(((?:[^)]+))\)\s*(?:throws[^{]*)?\{`)
	sbAnnotationNameRE = regexp.MustCompile(`@(\w+)`)
	sbMappingPathRE    = regexp.MustCompile(`"([^"]*)"`)
	sbRequestMethodRE  = regexp.MustCompile(`method\s*=\s*RequestMethod\.(\w+)`)
)

// stereotypeInfo holds Spring Boot stereotype class metadata.
type stereotypeInfo struct {
	kind   string
	offset int
}

// configInfo holds Spring Boot @Configuration class metadata.
type configInfo struct {
	ref    string
	offset int
}

var sbHTTPMappingVerbs = map[string]string{
	"GetMapping": "GET", "PostMapping": "POST", "PutMapping": "PUT",
	"DeleteMapping": "DELETE", "PatchMapping": "PATCH",
}

// ExtractSpringBoot runs the Spring Boot custom extractor.
// It accepts both Java and Kotlin source files: Kotlin Spring Boot uses the
// same annotations (@Autowired, @Bean, @Component, @Scope, @RestController,
// etc.) with `fun` instead of method declarations and `class` declarations
// that are otherwise syntactically identical to Java for these patterns.
func ExtractSpringBoot(ctx PatternContext) PatternResult {
	var result PatternResult
	if (ctx.Language != "java" && ctx.Language != "kotlin") || !springBootFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// 1. Controllers
	type ctrlInfo struct {
		name   string
		offset int
	}
	var controllers []ctrlInfo
	controllerMap := make(map[string]int)
	for _, m := range sbRestControllerRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		if _, exists := controllerMap[name]; !exists {
			controllerMap[name] = m[0]
			controllers = append(controllers, ctrlInfo{name, m[0]})
		}
	}

	// Class-level base paths
	type pathInfo struct {
		offset int
		path   string
	}
	var basePaths []pathInfo
	for _, m := range sbClassRequestMappingRE.FindAllStringSubmatchIndex(source, -1) {
		basePaths = append(basePaths, pathInfo{m[0], source[m[2]:m[3]]})
	}

	getBasePath := func(classOffset int) string {
		var best string
		for _, bp := range basePaths {
			if bp.offset < classOffset+500 {
				best = bp.path
			}
		}
		return best
	}

	findOwningController := func(offset int) string {
		var owner string
		for _, c := range controllers {
			if c.offset <= offset {
				owner = c.name
			}
		}
		return owner
	}

	// Endpoints
	for _, m := range sbHTTPMappingRE.FindAllStringSubmatchIndex(source, -1) {
		annText := source[m[2]:m[3]]
		methodName := source[m[4]:m[5]]

		owner := findOwningController(m[0])
		if owner == "" {
			continue
		}

		base := getBasePath(controllerMap[owner])
		path := ""
		if pm := sbMappingPathRE.FindStringSubmatch(annText); pm != nil {
			path = pm[1]
		}
		fullPath := joinPaths(base, path)

		annNameMatch := sbAnnotationNameRE.FindStringSubmatch(annText)
		ann := ""
		if annNameMatch != nil {
			ann = annNameMatch[1]
		}
		verb := sbHTTPMappingVerbs[ann]
		if ann == "RequestMapping" {
			verb = "GET"
			snippet := source[m[0]:min(m[0]+200, len(source))]
			if rm := sbRequestMethodRE.FindStringSubmatch(snippet); rm != nil {
				verb = rm[1]
			}
		}

		ref := "scope:operation:spring_boot_endpoint:" + fp + ":" + owner + "." + methodName
		props := map[string]any{
			"http_method": verb, "path": fullPath,
			"controller_class": owner, "framework": "spring_boot",
		}
		if pathVars := extractPathParams(fullPath); len(pathVars) > 0 {
			props["path_params"] = pathVars
		}
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: owner + "." + methodName, Kind: "SCOPE.Operation",
			Subtype: "endpoint", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_BOOT_REQUEST_MAPPING", Ref: ref,
			Properties: props,
		})
	}

	// 2. Stereotype classes
	componentClasses := make(map[string]stereotypeInfo)
	for _, m := range sbServiceClassRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		if _, ok := componentClasses[name]; !ok {
			componentClasses[name] = stereotypeInfo{"service", m[0]}
		}
	}
	for _, m := range sbRepositoryClassRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		if _, ok := componentClasses[name]; !ok {
			componentClasses[name] = stereotypeInfo{"repository", m[0]}
		}
	}
	for _, m := range sbComponentClassRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		if _, ok := componentClasses[name]; !ok {
			componentClasses[name] = stereotypeInfo{"component", m[0]}
		}
	}
	for name, offset := range controllerMap {
		if _, ok := componentClasses[name]; !ok {
			componentClasses[name] = stereotypeInfo{"controller", offset}
		}
	}

	for clsName, info := range componentClasses {
		if info.kind == "controller" {
			continue
		}
		ref := "scope:component:spring_boot_" + info.kind + ":" + fp + ":" + clsName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: clsName, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, info.offset), LineEnd: lineOf(source, info.offset),
			Provenance: "INFERRED_FROM_SPRING_BOOT_STEREOTYPE", Ref: ref,
			Properties: map[string]any{"stereotype": info.kind, "framework": "spring_boot"},
		})
	}

	// 3. Configuration classes + Bean methods
	configClasses := make(map[string]configInfo)
	for _, m := range sbConfigurationClassRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		if _, ok := configClasses[name]; !ok {
			cfgRef := "scope:pattern:spring_boot_config:" + fp + ":" + name
			configClasses[name] = configInfo{cfgRef, m[0]}
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: name, Kind: "SCOPE.Pattern", SourceFile: fp,
				LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_SPRING_BOOT_CONFIGURATION", Ref: cfgRef,
				Properties: map[string]any{"framework": "spring_boot"},
			})
		}
	}

	findOwningConfig := func(offset int) (string, string) {
		var bestName, bestRef string
		for name, ci := range configClasses {
			if ci.offset <= offset {
				if bestName == "" || ci.offset > configClasses[bestName].offset {
					bestName = name
					bestRef = ci.ref
				}
			}
		}
		return bestName, bestRef
	}

	for _, m := range sbBeanMethodRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[2]:m[3]]
		ownerName, ownerRef := findOwningConfig(m[0])
		if ownerName == "" {
			continue
		}
		beanRef := "scope:operation:spring_boot_bean:" + fp + ":" + ownerName + "." + methodName
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerName + "." + methodName, Kind: "SCOPE.Operation",
			Subtype: "function", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_BOOT_BEAN", Ref: beanRef,
			Properties: map[string]any{
				"bean_method": methodName, "config_class": ownerName,
				"framework": "spring_boot",
			},
		}) {
			addRel(&result, seenRels, Relationship{
				SourceRef: ownerRef, TargetRef: beanRef,
				RelationshipType: "OWNS",
			})
		}
	}

	// 4. DI: @Autowired field injection
	for _, m := range sbAutowiredFieldRE.FindAllStringSubmatchIndex(source, -1) {
		injectedType := source[m[2]:m[3]]
		if primitiveTypes[injectedType] {
			continue
		}
		ownerRef := findOwningClassRef(source, m[0], fp, componentClasses, configClasses)
		if ownerRef == "" {
			continue
		}
		targetRef := findRefForType(injectedType, fp, "spring_boot", &result)
		addRel(&result, seenRels, Relationship{
			SourceRef: ownerRef, TargetRef: targetRef,
			RelationshipType: "DEPENDS_ON",
			Properties:       map[string]string{"injected_type": injectedType, "injection_kind": "field"},
		})
	}

	// @Autowired setter injection
	for _, m := range sbAutowiredSetterRE.FindAllStringSubmatchIndex(source, -1) {
		injectedType := source[m[2]:m[3]]
		if primitiveTypes[injectedType] {
			continue
		}
		ownerRef := findOwningClassRef(source, m[0], fp, componentClasses, configClasses)
		if ownerRef == "" {
			continue
		}
		targetRef := findRefForType(injectedType, fp, "spring_boot", &result)
		addRel(&result, seenRels, Relationship{
			SourceRef: ownerRef, TargetRef: targetRef,
			RelationshipType: "DEPENDS_ON",
			Properties:       map[string]string{"injected_type": injectedType, "injection_kind": "setter"},
		})
	}

	// Constructor injection
	for _, m := range sbConstructorInjectionRE.FindAllStringSubmatchIndex(source, -1) {
		ctorClass := source[m[2]:m[3]]
		paramsStr := source[m[4]:m[5]]

		ownerRef := ""
		if info, ok := componentClasses[ctorClass]; ok {
			ownerRef = "scope:component:spring_boot_" + info.kind + ":" + fp + ":" + ctorClass
			if info.kind == "controller" {
				ownerRef = "scope:component:spring_boot_controller:" + fp + ":" + ctorClass
			}
		} else if ci, ok := configClasses[ctorClass]; ok {
			ownerRef = ci.ref
		}
		if ownerRef == "" {
			continue
		}

		for _, pm := range constructorParamRE.FindAllStringSubmatch(paramsStr, -1) {
			injectedType := pm[1]
			if primitiveTypes[injectedType] {
				continue
			}
			targetRef := findRefForType(injectedType, fp, "spring_boot", &result)
			addRel(&result, seenRels, Relationship{
				SourceRef: ownerRef, TargetRef: targetRef,
				RelationshipType: "DEPENDS_ON",
				Properties:       map[string]string{"injected_type": injectedType, "injection_kind": "constructor"},
			})
		}
	}

	// 5. Actuator: @Endpoint classes + @ReadOperation/@WriteOperation/@DeleteOperation methods
	type actuatorEndpointInfo struct {
		endpointID string
		offset     int
		ref        string
	}
	actuatorEndpoints := make(map[string]actuatorEndpointInfo)
	for _, m := range sbActuatorEndpointRE.FindAllStringSubmatchIndex(source, -1) {
		endpointID := source[m[2]:m[3]]
		className := source[m[4]:m[5]]
		ref := "scope:component:spring_actuator_endpoint:" + fp + ":" + className
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_ACTUATOR", Ref: ref,
			Properties: map[string]any{
				"endpoint_id": endpointID, "framework": "spring_actuator",
			},
		}) {
			actuatorEndpoints[className] = actuatorEndpointInfo{endpointID, m[0], ref}
		}
	}

	findOwningActuator := func(offset int) (string, string) {
		var bestName, bestRef string
		for name, info := range actuatorEndpoints {
			if info.offset <= offset {
				if bestName == "" || actuatorEndpoints[bestName].offset < info.offset {
					bestName = name
					bestRef = info.ref
				}
			}
		}
		return bestName, bestRef
	}

	for _, m := range sbActuatorOperationRE.FindAllStringSubmatchIndex(source, -1) {
		annText := source[m[2]:m[3]]
		methodName := source[m[4]:m[5]]
		ownerName, ownerRef := findOwningActuator(m[0])
		if ownerName == "" {
			continue
		}
		opKind := "read"
		switch annText {
		case "@WriteOperation":
			opKind = "write"
		case "@DeleteOperation":
			opKind = "delete"
		}
		opRef := "scope:operation:spring_actuator_op:" + fp + ":" + ownerName + "." + methodName
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerName + "." + methodName, Kind: "SCOPE.Operation",
			Subtype: "function", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_ACTUATOR", Ref: opRef,
			Properties: map[string]any{
				"operation_kind": opKind, "endpoint_class": ownerName,
				"framework": "spring_actuator",
			},
		}) {
			addRel(&result, seenRels, Relationship{
				SourceRef: ownerRef, TargetRef: opRef,
				RelationshipType: "OWNS",
			})
		}
	}

	// 6. DI scope resolution: Spring @Scope / @RequestScope / @SessionScope / @ApplicationScope
	for _, m := range sbScopeAnnotationRE.FindAllStringSubmatchIndex(source, -1) {
		scopeName := ""
		if m[2] >= 0 {
			scopeName = source[m[2]:m[3]] // ConfigurableBeanFactory.SCOPE_<X>
		} else if m[4] >= 0 {
			scopeName = source[m[4]:m[5]] // string literal
		}
		className := source[m[6]:m[7]]
		ref := "scope:component:spring_scoped_bean:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_DI_SCOPE", Ref: ref,
			Properties: map[string]any{"spring_scope": scopeName, "framework": "spring_boot"},
		})
	}
	for _, pair := range []struct {
		re    *regexp.Regexp
		scope string
	}{
		{sbRequestScopeRE, "request"},
		{sbSessionScopeRE, "session"},
		{sbApplicationScopeRE, "application"},
	} {
		for _, m := range pair.re.FindAllStringSubmatchIndex(source, -1) {
			className := source[m[2]:m[3]]
			ref := "scope:component:spring_scoped_bean:" + fp + ":" + className
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: className, Kind: "SCOPE.Component", SourceFile: fp,
				LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_SPRING_DI_SCOPE", Ref: ref,
				Properties: map[string]any{"spring_scope": pair.scope, "framework": "spring_boot"},
			})
		}
	}

	return result
}

func findOwningClassRef(source string, offset int, fp string,
	components map[string]stereotypeInfo, configs map[string]configInfo) string {

	type classEntry struct {
		offset int
		ref    string
	}
	var all []classEntry
	for name, info := range components {
		ref := "scope:component:spring_boot_" + info.kind + ":" + fp + ":" + name
		if info.kind == "controller" {
			ref = "scope:component:spring_boot_controller:" + fp + ":" + name
		}
		all = append(all, classEntry{info.offset, ref})
	}
	for _, ci := range configs {
		all = append(all, classEntry{ci.offset, ci.ref})
	}

	var bestRef string
	bestOffset := -1
	for _, e := range all {
		if e.offset <= offset && e.offset > bestOffset {
			bestRef = e.ref
			bestOffset = e.offset
		}
	}
	return bestRef
}

func joinPaths(base, sub string) string {
	if base == "" && sub == "" {
		return "/"
	}
	var path string
	if sub == "" {
		path = base
	} else if base == "" {
		path = sub
	} else {
		path = trimRight(base, "/") + "/" + trimLeft(sub, "/")
	}
	if path == "" {
		return "/"
	}
	if path[0] != '/' {
		path = "/" + path
	}
	return path
}

func trimRight(s, cutset string) string {
	for len(s) > 0 && s[len(s)-1] == cutset[0] {
		s = s[:len(s)-1]
	}
	return s
}

func trimLeft(s, cutset string) string {
	for len(s) > 0 && s[0] == cutset[0] {
		s = s[1:]
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
