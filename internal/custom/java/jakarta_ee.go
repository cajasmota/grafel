package java

import "regexp"

// Jakarta EE custom extractor: Servlets, JPA, EJB, WebSocket, Validation, Interceptors.
// Ported from: jakarta_ee_extractor.py

var jakartaEEFrameworks = map[string]bool{
	"jakarta_ee": true, "jakarta-ee": true, "jakartaee": true,
	"java_ee": true, "javaee": true,
}

var (
	jeeWebServletRE = regexp.MustCompile(
		`(?s)@WebServlet\s*\(([^)]*)\)[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	jeeWebFilterRE = regexp.MustCompile(
		`(?s)@WebFilter\s*\(([^)]*)\)[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	jeeServletMethodRE = regexp.MustCompile(
		`(?m)(?:protected|public)\s+void\s+(doGet|doPost|doPut|doDelete|doPatch|doHead|doOptions)\s*\(`)
	jeeJPAEntityRE = regexp.MustCompile(
		`(?s)@Entity\b(?:[^{]|\{[^}]*\})*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	jeeJPATableRE = regexp.MustCompile(
		`(?m)@Table\s*\([^)]*name\s*=\s*"([^"]*)"`)
	jeeJPARelationRE = regexp.MustCompile(
		`(?s)@(ManyToOne|OneToMany|ManyToMany|OneToOne)\b[^;]*?` +
			`(?:private|protected|public)\s+(?:(?:List|Set|Collection|Map)\s*<\s*)?(\w+)(?:\s*>)?\s+\w+\s*;`)
	jeeNamedQueryRE = regexp.MustCompile(
		`(?m)@NamedQuery\s*\([^)]*name\s*=\s*"([^"]*)"`)
	jeeEJBSessionRE = regexp.MustCompile(
		`(?s)@(Stateless|Stateful|Singleton)\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	jeeMessageDrivenRE = regexp.MustCompile(
		`(?s)@MessageDriven\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	jeeEJBInjectRE = regexp.MustCompile(
		`(?s)@EJB\b[^;{(]*?(?:private|protected|public)\s+(?:final\s+)?` +
			`(\w+)(?:\s*<[^>]*>)?\s+\w+\s*;`)
	jeeScheduleRE = regexp.MustCompile(
		`(?s)@Schedule\s*\(([^)]*)\)\s*(?:public|protected|private)\s+(?:void|[A-Z]\w*)\s+(\w+)\s*\(`)
	jeeServerEndpointRE = regexp.MustCompile(
		`(?s)@ServerEndpoint\s*\(\s*"([^"]*)"\s*\)[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	jeeWSHandlerRE = regexp.MustCompile(
		`(?s)@(OnMessage|OnOpen|OnClose|OnError)\s*(?:public|protected|private)\s+` +
			`(?:void|[A-Z]\w*(?:\s*<[^>]*>)?)\s+(\w+)\s*\(`)
	jeeInterceptorRE = regexp.MustCompile(
		`(?s)@Interceptor\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	jeeResourceInjectRE = regexp.MustCompile(
		`(?s)@Resource\b[^;{(]*?(?:private|protected|public)\s+(?:final\s+)?` +
			`(\w+)(?:\s*<[^>]*>)?\s+\w+\s*;`)
	jeePersistenceCtxRE = regexp.MustCompile(
		`(?s)@PersistenceContext\b[^;{(]*?(?:private|protected|public)\s+(?:final\s+)?` +
			`(\w+)(?:\s*<[^>]*>)?\s+\w+\s*;`)
	jeeConstraintValidatorRE = regexp.MustCompile(
		`(?s)(?:public\s+)?class\s+(\w+)\s+(?:extends\s+\w+\s+)?` +
			`implements\s+[^{]*ConstraintValidator\s*<\s*(\w+)`)
	jeeURLPatternRE = regexp.MustCompile(`"([^"]*)"`)
)

var jeeServletMethodVerbs = map[string]string{
	"doGet": "GET", "doPost": "POST", "doPut": "PUT",
	"doDelete": "DELETE", "doPatch": "PATCH", "doHead": "HEAD",
	"doOptions": "OPTIONS",
}

// ExtractJakartaEE runs the Jakarta EE extractor.
func ExtractJakartaEE(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !jakartaEEFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// 1. Servlets
	type servletInfo struct {
		url    string
		offset int
	}
	servletClasses := make(map[string]servletInfo)
	for _, m := range jeeWebServletRE.FindAllStringSubmatchIndex(source, -1) {
		url := ""
		if pm := jeeURLPatternRE.FindStringSubmatch(source[m[2]:m[3]]); pm != nil {
			url = pm[1]
		}
		className := source[m[4]:m[5]]
		if _, ok := servletClasses[className]; !ok {
			servletClasses[className] = servletInfo{url, m[0]}
		}
	}
	for className, info := range servletClasses {
		ref := "scope:operation:jakarta_servlet:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Operation", Subtype: "endpoint",
			SourceFile: fp,
			LineStart:  lineOf(source, info.offset), LineEnd: lineOf(source, info.offset),
			Provenance: "INFERRED_FROM_JAKARTA_SERVLET", Ref: ref,
			Properties: map[string]any{"url_pattern": info.url, "framework": "jakarta_ee"},
		})
	}

	// Servlet HTTP methods
	for _, m := range jeeServletMethodRE.FindAllStringSubmatchIndex(source, -1) {
		method := source[m[2]:m[3]]
		ownerClass := findEnclosingClass(source, m[0])
		if ownerClass == "" {
			continue
		}
		verb := jeeServletMethodVerbs[method]
		ref := "scope:operation:jakarta_servlet_method:" + fp + ":" + ownerClass + "." + method
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerClass + "." + method, Kind: "SCOPE.Operation",
			Subtype: "function", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_SERVLET", Ref: ref,
			Properties: map[string]any{"http_method": verb, "framework": "jakarta_ee"},
		})
	}

	// 2. @WebFilter
	for _, m := range jeeWebFilterRE.FindAllStringSubmatchIndex(source, -1) {
		url := ""
		if pm := jeeURLPatternRE.FindStringSubmatch(source[m[2]:m[3]]); pm != nil {
			url = pm[1]
		}
		className := source[m[4]:m[5]]
		ref := "scope:pattern:jakarta_filter:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_FILTER", Ref: ref,
			Properties: map[string]any{"url_pattern": url, "framework": "jakarta_ee"},
		})
	}

	// 3. JPA entities
	type entityInfo struct {
		ref    string
		offset int
	}
	jpaEntities := make(map[string]entityInfo)
	for _, m := range jeeJPAEntityRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := "scope:component:jakarta_jpa_entity:" + fp + ":" + className
		if seenRefs[ref] {
			continue
		}
		seenRefs[ref] = true
		jpaEntities[className] = entityInfo{ref, m[0]}

		// Table name
		snippet := source[max(0, m[0]-600):m[1]]
		var tableName string
		if tm := jeeJPATableRE.FindStringSubmatch(snippet); tm != nil {
			tableName = tm[1]
		}
		props := map[string]any{"framework": "jakarta_ee"}
		if tableName != "" {
			props["table_name"] = tableName
		}
		result.Entities = append(result.Entities, SecondaryEntity{
			Name: className, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_JPA_ENTITY", Ref: ref,
			Properties: props,
		})
	}

	// JPA relations
	for _, m := range jeeJPARelationRE.FindAllStringSubmatchIndex(source, -1) {
		relType := source[m[2]:m[3]]
		targetType := source[m[4]:m[5]]
		ownerClass := findEnclosingClass(source, m[0])
		if ownerClass == "" {
			continue
		}
		ownerInfo, ok := jpaEntities[ownerClass]
		if !ok {
			continue
		}
		targetRef := "scope:component:jakarta_jpa_entity:" + fp + ":" + targetType
		addRel(&result, seenRels, Relationship{
			SourceRef: ownerInfo.ref, TargetRef: targetRef, RelationshipType: "DEPENDS_ON",
			Properties: map[string]string{"kind": "jpa_relation", "relation_type": relType},
		})
	}

	// Named queries
	for _, m := range jeeNamedQueryRE.FindAllStringSubmatchIndex(source, -1) {
		queryName := source[m[2]:m[3]]
		ownerClass := findEnclosingClass(source, m[0])
		if ownerClass == "" {
			ownerClass = "Unknown"
		}
		ref := "scope:operation:jakarta_named_query:" + fp + ":" + ownerClass + "." + queryName
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerClass + "." + queryName, Kind: "SCOPE.Operation",
			Subtype: "function", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_JPA_ENTITY", Ref: ref,
			Properties: map[string]any{"query_name": queryName, "framework": "jakarta_ee"},
		}) {
			if ei, ok := jpaEntities[ownerClass]; ok {
				addRel(&result, seenRels, Relationship{
					SourceRef: ei.ref, TargetRef: ref, RelationshipType: "OWNS",
				})
			}
		}
	}

	// 4. EJB session beans
	for _, m := range jeeEJBSessionRE.FindAllStringSubmatchIndex(source, -1) {
		ejbType := source[m[2]:m[3]]
		className := source[m[4]:m[5]]
		ref := "scope:service:jakarta_ejb:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Service", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_EJB", Ref: ref,
			Properties: map[string]any{"ejb_type": ejbType, "framework": "jakarta_ee"},
		})
	}

	// @MessageDriven
	for _, m := range jeeMessageDrivenRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := "scope:service:jakarta_ejb:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Service", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_EJB", Ref: ref,
			Properties: map[string]any{"ejb_type": "MessageDriven", "framework": "jakarta_ee"},
		})
	}

	// @EJB injection
	for _, m := range jeeEJBInjectRE.FindAllStringSubmatchIndex(source, -1) {
		injectedType := source[m[2]:m[3]]
		if primitiveTypes[injectedType] {
			continue
		}
		ownerClass := findEnclosingClass(source, m[0])
		if ownerClass == "" {
			continue
		}
		ownerRef := "scope:dependency:jakarta:" + fp + ":" + ownerClass
		if ei, ok := jpaEntities[ownerClass]; ok {
			ownerRef = ei.ref
		}
		targetRef := findRefForType(injectedType, fp, "jakarta", &result)
		addRel(&result, seenRels, Relationship{
			SourceRef: ownerRef, TargetRef: targetRef, RelationshipType: "DEPENDS_ON",
			Properties: map[string]string{"kind": "ejb_inject", "injected_type": injectedType},
		})
	}

	// 5. @Schedule
	for _, m := range jeeScheduleRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[4]:m[5]]
		ownerClass := findEnclosingClass(source, m[0])
		if ownerClass == "" {
			ownerClass = "Unknown"
		}
		ref := "scope:operation:jakarta_schedule:" + fp + ":" + ownerClass + "." + methodName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerClass + "." + methodName, Kind: "SCOPE.Operation",
			Subtype: "function", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_SCHEDULE", Ref: ref,
			Properties: map[string]any{"framework": "jakarta_ee"},
		})
	}

	// 6. WebSocket
	for _, m := range jeeServerEndpointRE.FindAllStringSubmatchIndex(source, -1) {
		wsPath := source[m[2]:m[3]]
		className := source[m[4]:m[5]]
		ref := "scope:operation:jakarta_websocket:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Operation", Subtype: "endpoint",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_WEBSOCKET", Ref: ref,
			Properties: map[string]any{"ws_path": wsPath, "framework": "jakarta_ee"},
		})
	}
	for _, m := range jeeWSHandlerRE.FindAllStringSubmatchIndex(source, -1) {
		handlerType := source[m[2]:m[3]]
		methodName := source[m[4]:m[5]]
		ownerClass := findEnclosingClass(source, m[0])
		if ownerClass == "" {
			ownerClass = "Unknown"
		}
		ref := "scope:operation:jakarta_ws_handler:" + fp + ":" + ownerClass + "." + methodName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerClass + "." + methodName, Kind: "SCOPE.Operation",
			Subtype: "function", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_WEBSOCKET", Ref: ref,
			Properties: map[string]any{"handler_type": handlerType, "framework": "jakarta_ee"},
		})
	}

	// 7. Interceptors
	for _, m := range jeeInterceptorRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := "scope:pattern:jakarta_interceptor:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_INTERCEPTOR", Ref: ref,
			Properties: map[string]any{"framework": "jakarta_ee"},
		})
	}

	// 8. Constraint validators
	for _, m := range jeeConstraintValidatorRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := "scope:component:jakarta_validator:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_VALIDATOR", Ref: ref,
			Properties: map[string]any{"framework": "jakarta_ee"},
		})
	}

	// 9. @Resource / @PersistenceContext injection
	for _, re := range []*regexp.Regexp{jeeResourceInjectRE, jeePersistenceCtxRE} {
		for _, m := range re.FindAllStringSubmatchIndex(source, -1) {
			injectedType := source[m[2]:m[3]]
			if primitiveTypes[injectedType] {
				continue
			}
			ownerClass := findEnclosingClass(source, m[0])
			if ownerClass == "" {
				continue
			}
			ownerRef := "scope:dependency:jakarta:" + fp + ":" + ownerClass
			targetRef := findRefForType(injectedType, fp, "jakarta", &result)
			addRel(&result, seenRels, Relationship{
				SourceRef: ownerRef, TargetRef: targetRef, RelationshipType: "DEPENDS_ON",
				Properties: map[string]string{"kind": "resource_inject", "injected_type": injectedType},
			})
		}
	}

	return result
}
