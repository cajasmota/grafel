package java

import "regexp"

// Micronaut custom extractor: endpoints, DI, AOP, scheduled, HTTP clients.
// Ported from: micronaut_extractor.py

var micronautFrameworks = map[string]bool{
	"micronaut": true, "micronaut-core": true, "micronaut_core": true,
}

var (
	mnControllerRE = regexp.MustCompile(
		`(?s)@Controller\s*(?:\(\s*(?:value\s*=\s*)?\"([^\"]*)\"\s*\))?` +
			`[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	mnBeanClassRE = regexp.MustCompile(
		`(?s)@(Singleton|Prototype)\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	mnHTTPMethodRE = regexp.MustCompile(
		`(?s)@(Get|Post|Put|Delete|Patch|Head|Options)\s*` +
			`(?:\(\s*(?:value\s*=\s*)?\"([^\"]*)\"\s*\))?[^(]*?` +
			`(?:public|protected|private|)\s+(?:static\s+)?(?:<[^>]*>\s*)?` +
			`(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
	mnScheduledRE = regexp.MustCompile(
		`(?s)@Scheduled\s*\(([^)]*)\)(?:\s*@\w+(?:\([^)]*\))?)?\s*` +
			`(?:public|protected|private|)\s+(?:static\s+)?(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
	mnRetryableRE = regexp.MustCompile(
		`(?s)@Retryable\s*\(([^)]*)\)[^(]*?` +
			`(?:public|protected|private|)\s+(?:static\s+)?(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
	mnCircuitBreakerRE = regexp.MustCompile(
		`(?s)@CircuitBreaker\s*\(([^)]*)\)[^(]*?` +
			`(?:public|protected|private|)\s+(?:static\s+)?(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
	mnClientIfaceRE = regexp.MustCompile(
		`(?s)@Client\s*\(\s*(?:value\s*=\s*)?\"([^\"]*)\"[^)]*\)` +
			`[^{]*?(?:public\s+)?interface\s+(\w+)`)
	mnReplacesRE = regexp.MustCompile(
		`(?s)@Replaces\s*\(\s*(?:value\s*=\s*)?(\w+)\.class\s*\)` +
			`[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	mnInjectFieldRE = regexp.MustCompile(
		`(?s)@Inject\b[^;{(]*?(?:private|protected|public|)\s+(?:final\s+)?` +
			`(\w+)(?:\s*<[^>]*>)?\s+\w+\s*;`)
	mnConstructorRE = regexp.MustCompile(
		`(?s)(?:public|protected)\s+(\w+)\s*\(((?:[^)]+))\)\s*(?:throws\s+[^{]*)?\{`)
)

var mnHTTPVerbMap = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Delete": "DELETE",
	"Patch": "PATCH", "Head": "HEAD", "Options": "OPTIONS",
}

var mnPrimitiveTypes = map[string]bool{
	"int": true, "long": true, "double": true, "float": true,
	"boolean": true, "char": true, "byte": true, "short": true,
	"void": true, "String": true, "Integer": true, "Long": true,
	"Double": true, "Float": true, "Boolean": true, "Object": true,
	"List": true, "Map": true, "Set": true, "Collection": true,
	"Optional": true, "HttpRequest": true, "HttpResponse": true,
	"Publisher": true, "Flowable": true, "Single": true,
	"Maybe": true, "Completable": true,
}

// ExtractMicronaut runs the Micronaut extractor.
func ExtractMicronaut(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !micronautFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	classRefs := make(map[string]string)

	// Controllers
	type ctrlInfo struct {
		basePath string
		offset   int
	}
	controllerInfo := make(map[string]ctrlInfo)
	for _, m := range mnControllerRE.FindAllStringSubmatchIndex(source, -1) {
		basePath := ""
		if m[2] >= 0 {
			basePath = source[m[2]:m[3]]
		}
		clsName := source[m[4]:m[5]]
		if _, ok := controllerInfo[clsName]; !ok {
			controllerInfo[clsName] = ctrlInfo{basePath, m[0]}
			classRefs[clsName] = "scope:service:micronaut_bean:" + fp + ":" + clsName
		}
	}

	// Beans
	for _, m := range mnBeanClassRE.FindAllStringSubmatchIndex(source, -1) {
		scopeAnn := source[m[2]:m[3]]
		clsName := source[m[4]:m[5]]
		ref := "scope:service:micronaut_bean:" + fp + ":" + clsName
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: clsName, Kind: "SCOPE.Service", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICRONAUT_BEAN", Ref: ref,
			Properties: map[string]any{"scope": scopeAnn, "framework": "micronaut"},
		}) {
			classRefs[clsName] = ref
		}
	}

	// HTTP endpoints
	foundEndpoints := make(map[string]bool)
	for _, m := range mnHTTPMethodRE.FindAllStringSubmatchIndex(source, -1) {
		verbAnn := source[m[2]:m[3]]
		methodPath := ""
		if m[4] >= 0 {
			methodPath = source[m[4]:m[5]]
		}
		methodName := source[m[6]:m[7]]
		verb := mnHTTPVerbMap[verbAnn]
		if verb == "" {
			verb = verbAnn
		}

		ctrlName := findEnclosingClass(source, m[0])
		if ctrlName == "" {
			ctrlName = "UnknownController"
		}

		key := ctrlName + "." + methodName
		if foundEndpoints[key] {
			continue
		}
		foundEndpoints[key] = true

		basePath := ""
		if ci, ok := controllerInfo[ctrlName]; ok {
			basePath = ci.basePath
		}
		fullPath := joinPaths(basePath, methodPath)

		ref := "scope:operation:micronaut_endpoint:" + fp + ":" + ctrlName + "." + methodName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ctrlName + "." + methodName, Kind: "SCOPE.Operation",
			Subtype: "endpoint", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICRONAUT_CONTROLLER", Ref: ref,
			Properties: map[string]any{
				"http_method": verb, "path": fullPath,
				"controller_class": ctrlName, "framework": "micronaut",
			},
		})
	}

	// Scheduled tasks
	for _, m := range mnScheduledRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[4]:m[5]]
		ownerCls := findEnclosingClass(source, m[0])
		if ownerCls == "" {
			ownerCls = "Unknown"
		}
		ref := "scope:operation:micronaut_scheduled:" + fp + ":" + ownerCls + "." + methodName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerCls + "." + methodName, Kind: "SCOPE.Operation",
			Subtype: "function", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICRONAUT_SCHEDULED", Ref: ref,
			Properties: map[string]any{"framework": "micronaut"},
		})
	}

	// AOP: @Retryable
	for _, m := range mnRetryableRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[4]:m[5]]
		ownerCls := findEnclosingClass(source, m[0])
		if ownerCls == "" {
			ownerCls = "Unknown"
		}
		ref := "scope:pattern:micronaut_retryable:" + fp + ":" + ownerCls + "." + methodName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerCls + "." + methodName, Kind: "SCOPE.Pattern",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICRONAUT_RETRYABLE", Ref: ref,
			Properties: map[string]any{"aop_type": "Retryable", "framework": "micronaut"},
		})
	}

	// AOP: @CircuitBreaker
	for _, m := range mnCircuitBreakerRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[4]:m[5]]
		ownerCls := findEnclosingClass(source, m[0])
		if ownerCls == "" {
			ownerCls = "Unknown"
		}
		ref := "scope:pattern:micronaut_circuit_breaker:" + fp + ":" + ownerCls + "." + methodName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerCls + "." + methodName, Kind: "SCOPE.Pattern",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICRONAUT_CIRCUIT_BREAKER", Ref: ref,
			Properties: map[string]any{"aop_type": "CircuitBreaker", "framework": "micronaut"},
		})
	}

	// HTTP Clients
	for _, m := range mnClientIfaceRE.FindAllStringSubmatchIndex(source, -1) {
		serviceID := source[m[2]:m[3]]
		ifaceName := source[m[4]:m[5]]
		ref := "scope:component:micronaut_client:" + fp + ":" + ifaceName
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: ifaceName, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICRONAUT_HTTP_CLIENT", Ref: ref,
			Properties: map[string]any{
				"service_id": serviceID, "interface_name": ifaceName,
				"framework": "micronaut",
			},
		}) {
			tgt := "scope:dependency:micronaut:" + fp + ":" + serviceID
			addRel(&result, seenRels, Relationship{
				SourceRef: ref, TargetRef: tgt, RelationshipType: "DEPENDS_ON",
				Properties: map[string]string{"kind": "http_client", "service_id": serviceID},
			})
		}
	}

	// @Replaces
	for _, m := range mnReplacesRE.FindAllStringSubmatchIndex(source, -1) {
		replacedType := source[m[2]:m[3]]
		replacingCls := source[m[4]:m[5]]
		ref := "scope:pattern:micronaut_replaces:" + fp + ":" + replacingCls
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: replacingCls, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICRONAUT_REPLACES", Ref: ref,
			Properties: map[string]any{
				"replacing_class": replacingCls, "replaced_type": replacedType,
				"framework": "micronaut",
			},
		}) {
			tgt := "scope:dependency:micronaut:" + fp + ":" + replacedType
			if r, ok := classRefs[replacedType]; ok {
				tgt = r
			}
			addRel(&result, seenRels, Relationship{
				SourceRef: ref, TargetRef: tgt, RelationshipType: "DEPENDS_ON",
				Properties: map[string]string{"kind": "bean_override", "replaced_type": replacedType},
			})
		}
	}

	// @Inject field injection
	for _, m := range mnInjectFieldRE.FindAllStringSubmatchIndex(source, -1) {
		injectedType := source[m[2]:m[3]]
		if mnPrimitiveTypes[injectedType] {
			continue
		}
		ownerCls := findEnclosingClass(source, m[0])
		if ownerCls == "" {
			continue
		}
		ownerRef := classRefs[ownerCls]
		if ownerRef == "" {
			ownerRef = "scope:dependency:micronaut:" + fp + ":" + ownerCls
		}
		tgt := findRefForType(injectedType, fp, "micronaut", &result)
		addRel(&result, seenRels, Relationship{
			SourceRef: ownerRef, TargetRef: tgt, RelationshipType: "DEPENDS_ON",
			Properties: map[string]string{"kind": "inject", "injected_type": injectedType},
		})
	}

	// Constructor injection
	for _, m := range mnConstructorRE.FindAllStringSubmatchIndex(source, -1) {
		ctorClass := source[m[2]:m[3]]
		paramsStr := source[m[4]:m[5]]
		ownerRef := classRefs[ctorClass]
		if ownerRef == "" {
			continue
		}
		for _, pm := range constructorParamRE.FindAllStringSubmatch(paramsStr, -1) {
			injectedType := pm[1]
			if mnPrimitiveTypes[injectedType] {
				continue
			}
			tgt := findRefForType(injectedType, fp, "micronaut", &result)
			addRel(&result, seenRels, Relationship{
				SourceRef: ownerRef, TargetRef: tgt, RelationshipType: "DEPENDS_ON",
				Properties: map[string]string{"kind": "constructor", "injected_type": injectedType},
			})
		}
	}

	return result
}
