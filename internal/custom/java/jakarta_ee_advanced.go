package java

import "regexp"

// Jakarta EE advanced custom extractor: CDI, Security, Batch, SOAP, JAXB, JSON-B, JSP.
// MicroProfile builds on CDI so this extractor also handles MicroProfile DI
// (di_binding_extraction, di_injection_point, di_scope_resolution) — Refs #2996.
// Ported from: jakarta_ee_advanced_extractor.py

// jakartaEEAdvFrameworks covers Jakarta EE and MicroProfile because MicroProfile
// inherits CDI for its DI model (@Inject, @Produces, @Qualifier, CDI scopes).
var jakartaEEAdvFrameworks = map[string]bool{
	"jakarta_ee": true, "jakarta-ee": true, "jakartaee": true,
	"microprofile": true, "eclipse-microprofile": true,
	// Runtime MicroProfile implementations that share CDI.
	"open_liberty": true, "payara": true, "helidon": true,
}

var (
	// CDI scope annotations — di_scope_resolution for Jakarta EE + MicroProfile (#2996).
	jeeaCDIScopeRE = regexp.MustCompile(
		`(?s)@(ApplicationScoped|RequestScoped|SessionScoped|Dependent|ConversationScoped)\b` +
			`[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	jeeaProducesMethodRE = regexp.MustCompile(
		`(?s)@Produces\b[^;{]*?(?:public|protected|private|)\s+(?:static\s+)?` +
			`(\w+)(?:\s*<[^>]*>)?\s+(\w+)\s*\(`)
	jeeaDisposesMethodRE = regexp.MustCompile(
		`(?s)(?:public|protected|private|)\s+void\s+(\w+)\s*\([^)]*@Disposes\s+(\w+)`)
	jeeaObservesMethodRE = regexp.MustCompile(
		`(?s)(?:public|protected|private|)\s+void\s+(\w+)\s*\([^)]*@(?:Observes|ObservesAsync)\s+(\w+)`)
	jeeaDecoratorClassRE = regexp.MustCompile(
		`(?s)@Decorator\b[^{]*?class\s+(\w+)(?:\s+implements\s+(\w+))?`)
	jeeaQualifierAnnotRE = regexp.MustCompile(
		`(?s)@Qualifier\b.*?@interface\s+(\w+)`)
	jeeaAuthMechanismRE = regexp.MustCompile(
		`(?s)@(BasicAuthenticationMechanismDefinition|FormAuthenticationMechanismDefinition|` +
			`CustomFormAuthenticationMechanismDefinition|OpenIdAuthenticationMechanismDefinition)\b` +
			`[^{]*?class\s+(\w+)`)
	jeeaIdentityStoreRE = regexp.MustCompile(
		`(?s)@(DatabaseIdentityStoreDefinition|LdapIdentityStoreDefinition)\b[^{]*?class\s+(\w+)`)
	jeeaBatchletClassRE = regexp.MustCompile(
		`(?s)class\s+(\w+)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*\bBatchlet\b`)
	jeeaBatchletExtendsRE = regexp.MustCompile(
		`(?s)class\s+(\w+)\s+extends\s+AbstractBatchlet\b`)
	jeeaItemReaderRE = regexp.MustCompile(
		`(?s)class\s+(\w+)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*\bItemReader\b`)
	jeeaItemWriterRE = regexp.MustCompile(
		`(?s)class\s+(\w+)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*\bItemWriter\b`)
	jeeaItemProcessorRE = regexp.MustCompile(
		`(?s)class\s+(\w+)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*\bItemProcessor\b`)
	jeeaWebServiceClassRE = regexp.MustCompile(
		`(?s)@WebService\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?(?:class|interface)\s+(\w+)`)
	jeeaWebMethodRE = regexp.MustCompile(
		`(?s)@WebMethod\b[^;{]*?(?:public|protected|private|)\s+(?:static\s+)?` +
			`(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
	jeeaXmlRootElementRE = regexp.MustCompile(
		`(?s)@XmlRootElement\b.*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	jeeaPostConstructRE = regexp.MustCompile(
		`(?s)@PostConstruct\b[^;{]*?(?:public|protected|private|)\s+void\s+(\w+)\s*\(`)
	jeeaPreDestroyRE = regexp.MustCompile(
		`(?s)@PreDestroy\b[^;{]*?(?:public|protected|private|)\s+void\s+(\w+)\s*\(`)
	jeeaJsonbSerializerRE = regexp.MustCompile(
		`(?s)class\s+(\w+)\s+implements\s+[^{]*\bJsonbSerializer\b`)
	jeeaJsonbDeserializerRE = regexp.MustCompile(
		`(?s)class\s+(\w+)\s+implements\s+[^{]*\bJsonbDeserializer\b`)
	jeeaTagSupportRE = regexp.MustCompile(
		`(?s)class\s+(\w+)\s+extends\s+(?:TagSupport|SimpleTagSupport|BodyTagSupport)\b`)
)

func jeeaRef(kind, subkind, filePath, name string) string {
	return "scope:" + kind + ":jakarta_" + subkind + ":" + filePath + ":" + name
}

// ExtractJakartaEEAdvanced runs the Jakarta EE advanced extractor.
func ExtractJakartaEEAdvanced(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !jakartaEEAdvFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// CDI: @Produces
	producerTypes := make(map[string]string)
	for _, m := range jeeaProducesMethodRE.FindAllStringSubmatchIndex(source, -1) {
		returnType := source[m[2]:m[3]]
		methodName := source[m[4]:m[5]]
		ref := jeeaRef("operation", "cdi_producer", fp, methodName)
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_CDI_PRODUCER", Ref: ref,
			Properties: map[string]any{"return_type": returnType, "framework": "jakarta_ee"},
		}) {
			producerTypes[returnType] = ref
		}
	}

	// CDI: @Disposes
	for _, m := range jeeaDisposesMethodRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[2]:m[3]]
		disposedType := source[m[4]:m[5]]
		ref := jeeaRef("operation", "cdi_disposer", fp, methodName)
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_CDI_PRODUCER", Ref: ref,
			Properties: map[string]any{"disposed_type": disposedType, "framework": "jakarta_ee"},
		}) {
			if producerRef, ok := producerTypes[disposedType]; ok {
				addRel(&result, seenRels, Relationship{
					SourceRef: ref, TargetRef: producerRef, RelationshipType: "DEPENDS_ON",
					Properties: map[string]string{"kind": "disposer"},
				})
			}
		}
	}

	// CDI: @Observes
	for _, m := range jeeaObservesMethodRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[2]:m[3]]
		eventType := source[m[4]:m[5]]
		ref := jeeaRef("operation", "cdi_observer", fp, methodName)
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_CDI_EVENT_OBSERVER", Ref: ref,
			Properties: map[string]any{"event_type": eventType, "framework": "jakarta_ee"},
		})
	}

	// CDI: @Decorator
	for _, m := range jeeaDecoratorClassRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := jeeaRef("pattern", "cdi_decorator", fp, className)
		props := map[string]any{"framework": "jakarta_ee"}
		if m[4] >= 0 {
			props["decorated_interface"] = source[m[4]:m[5]]
		}
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_CDI_DECORATOR", Ref: ref,
			Properties: props,
		})
	}

	// CDI: @Qualifier
	for _, m := range jeeaQualifierAnnotRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		ref := jeeaRef("pattern", "cdi_qualifier", fp, name)
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_CDI_QUALIFIER", Ref: ref,
			Properties: map[string]any{"framework": "jakarta_ee"},
		})
	}

	// Security: auth mechanisms
	for _, m := range jeeaAuthMechanismRE.FindAllStringSubmatchIndex(source, -1) {
		annName := source[m[2]:m[3]]
		className := source[m[4]:m[5]]
		ref := jeeaRef("pattern", "security_auth", fp, className)
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_SECURITY_AUTH", Ref: ref,
			Properties: map[string]any{"auth_mechanism": annName, "framework": "jakarta_ee"},
		})
	}

	// Security: identity stores
	for _, m := range jeeaIdentityStoreRE.FindAllStringSubmatchIndex(source, -1) {
		storeType := source[m[2]:m[3]]
		className := source[m[4]:m[5]]
		ref := jeeaRef("component", "identity_store", fp, className)
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_SECURITY_IDENTITY_STORE", Ref: ref,
			Properties: map[string]any{"store_type": storeType, "framework": "jakarta_ee"},
		})
	}

	// Batch: Batchlet
	for _, re := range []*regexp.Regexp{jeeaBatchletClassRE, jeeaBatchletExtendsRE} {
		for _, m := range re.FindAllStringSubmatchIndex(source, -1) {
			className := source[m[2]:m[3]]
			ref := jeeaRef("service", "batch_batchlet", fp, className)
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: className, Kind: "SCOPE.Service", SourceFile: fp,
				LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_JAKARTA_BATCH_BATCHLET", Ref: ref,
				Properties: map[string]any{"batch_type": "batchlet", "framework": "jakarta_ee"},
			})
		}
	}

	// Batch: ItemReader/Writer/Processor
	for _, pair := range []struct {
		re   *regexp.Regexp
		kind string
	}{
		{jeeaItemReaderRE, "reader"}, {jeeaItemWriterRE, "writer"}, {jeeaItemProcessorRE, "processor"},
	} {
		for _, m := range pair.re.FindAllStringSubmatchIndex(source, -1) {
			className := source[m[2]:m[3]]
			ref := jeeaRef("component", "batch_"+pair.kind, fp, className)
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: className, Kind: "SCOPE.Component", SourceFile: fp,
				LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_JAKARTA_BATCH_" + toUpperCase(pair.kind), Ref: ref,
				Properties: map[string]any{"batch_type": pair.kind, "framework": "jakarta_ee"},
			})
		}
	}

	// SOAP: @WebService
	for _, m := range jeeaWebServiceClassRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := jeeaRef("service", "soap_service", fp, className)
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Service", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_SOAP_SERVICE", Ref: ref,
			Properties: map[string]any{"framework": "jakarta_ee"},
		})
	}

	// SOAP: @WebMethod
	for _, m := range jeeaWebMethodRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[2]:m[3]]
		ownerClass := findEnclosingClass(source, m[0])
		if ownerClass == "" {
			ownerClass = "Unknown"
		}
		ref := jeeaRef("operation", "soap_method", fp, ownerClass+"."+methodName)
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerClass + "." + methodName, Kind: "SCOPE.Operation",
			Subtype: "function", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_SOAP_METHOD", Ref: ref,
			Properties: map[string]any{"framework": "jakarta_ee"},
		})
	}

	// JAXB: @XmlRootElement
	for _, m := range jeeaXmlRootElementRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := jeeaRef("schema", "jaxb_root", fp, className)
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Schema", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAKARTA_JAXB", Ref: ref,
			Properties: map[string]any{"framework": "jakarta_ee"},
		})
	}

	// Lifecycle: @PostConstruct / @PreDestroy
	for _, pair := range []struct {
		re   *regexp.Regexp
		ann  string
		prov string
	}{
		{jeeaPostConstructRE, "PostConstruct", "INFERRED_FROM_JAKARTA_LIFECYCLE_POST_CONSTRUCT"},
		{jeeaPreDestroyRE, "PreDestroy", "INFERRED_FROM_JAKARTA_LIFECYCLE_PRE_DESTROY"},
	} {
		for _, m := range pair.re.FindAllStringSubmatchIndex(source, -1) {
			methodName := source[m[2]:m[3]]
			ownerClass := findEnclosingClass(source, m[0])
			if ownerClass == "" {
				ownerClass = "Unknown"
			}
			ref := jeeaRef("operation", "lifecycle_"+toLowerCase(pair.ann), fp, ownerClass+"."+methodName)
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: ownerClass + "." + methodName, Kind: "SCOPE.Operation",
				Subtype: "function", SourceFile: fp,
				LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: pair.prov, Ref: ref,
				Properties: map[string]any{"lifecycle_annotation": pair.ann, "framework": "jakarta_ee"},
			})
		}
	}

	// JSON-B serializers/deserializers
	for _, pair := range []struct {
		re   *regexp.Regexp
		kind string
	}{
		{jeeaJsonbSerializerRE, "serializer"},
		{jeeaJsonbDeserializerRE, "deserializer"},
	} {
		for _, m := range pair.re.FindAllStringSubmatchIndex(source, -1) {
			className := source[m[2]:m[3]]
			ref := jeeaRef("component", "jsonb_"+pair.kind, fp, className)
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: className, Kind: "SCOPE.Component", SourceFile: fp,
				LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_JAKARTA_JSONB", Ref: ref,
				Properties: map[string]any{"jsonb_type": pair.kind, "framework": "jakarta_ee"},
			})
		}
	}

	// JSP tag support classes (Jakarta EE only)
	if jakartaEEAdvFrameworks["jakarta_ee"] && ctx.Framework == "jakarta_ee" {
		for _, m := range jeeaTagSupportRE.FindAllStringSubmatchIndex(source, -1) {
			className := source[m[2]:m[3]]
			ref := jeeaRef("uicomponent", "jsp_tag", fp, className)
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: className, Kind: "SCOPE.UIComponent", Subtype: "component",
				SourceFile: fp,
				LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_JAKARTA_JSP_TAG", Ref: ref,
				Properties: map[string]any{"framework": "jakarta_ee"},
			})
		}
	}

	// CDI scope resolution — di_scope_resolution for Jakarta EE and MicroProfile (#2996).
	// Captures @ApplicationScoped / @RequestScoped / @SessionScoped / @Dependent /
	// @ConversationScoped on bean classes.
	for _, m := range jeeaCDIScopeRE.FindAllStringSubmatchIndex(source, -1) {
		scope := source[m[2]:m[3]]
		className := source[m[4]:m[5]]
		ref := "scope:component:cdi_scoped_bean:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_CDI_SCOPE", Ref: ref,
			Properties: map[string]any{"cdi_scope": scope, "framework": ctx.Framework},
		})
	}

	_ = seenRels
	return result
}

func toUpperCase(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
