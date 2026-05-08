package java

import "regexp"

// MicroProfile custom extractor: fault tolerance, health, REST client,
// metrics, reactive messaging.
// Ported from: microprofile_extractor.py

var microprofileFrameworks = map[string]bool{
	"quarkus": true, "micronaut": true, "open_liberty": true,
	"payara": true, "helidon": true,
}

var (
	mpRetryRE = regexp.MustCompile(
		`(?s)@Retry\s*(?:\(([^)]*)\))?\s*(?:(?:@\w+(?:\s*\([^)]*\))?)\s*)*` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:<[^>]*>\s*)?` +
			`(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
	mpCircuitBreakerRE = regexp.MustCompile(
		`(?s)@CircuitBreaker\s*(?:\(([^)]*)\))?\s*(?:(?:@\w+(?:\s*\([^)]*\))?)\s*)*` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:<[^>]*>\s*)?` +
			`(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
	mpFallbackRE = regexp.MustCompile(
		`(?s)@Fallback\s*\(([^)]*)\)\s*(?:(?:@\w+(?:\s*\([^)]*\))?)\s*)*` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:<[^>]*>\s*)?` +
			`(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
	mpFallbackMethodRE = regexp.MustCompile(`fallbackMethod\s*=\s*"(\w+)"`)
	mpHealthCheckRE    = regexp.MustCompile(
		`(?s)@(Liveness|Readiness|Startup)\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	mpRegisterRestClientRE = regexp.MustCompile(
		`(?s)@RegisterRestClient\b[^{]*?(?:public\s+)?interface\s+(\w+)`)
	mpIncomingRE = regexp.MustCompile(
		`(?s)@Incoming\s*\(\s*"([^"]+)"\s*\)\s*(?:(?:@\w+(?:\s*\([^)]*\))?)\s*)*` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:<[^>]*>\s*)?` +
			`(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
	mpOutgoingRE = regexp.MustCompile(
		`(?s)@Outgoing\s*\(\s*"([^"]+)"\s*\)\s*(?:(?:@\w+(?:\s*\([^)]*\))?)\s*)*` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:<[^>]*>\s*)?` +
			`(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
	mpCountedRE = regexp.MustCompile(
		`(?s)@Counted\s*(?:\(([^)]*)\))?\s*(?:(?:@\w+(?:\s*\([^)]*\))?)\s*)*` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:<[^>]*>\s*)?` +
			`(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
	mpTimedRE = regexp.MustCompile(
		`(?s)@Timed\s*(?:\(([^)]*)\))?\s*(?:(?:@\w+(?:\s*\([^)]*\))?)\s*)*` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:<[^>]*>\s*)?` +
			`(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
	mpAnnotationParamRE = regexp.MustCompile(`(\w+)\s*=\s*(\d+)`)
)

// ExtractMicroProfile runs the MicroProfile extractor.
func ExtractMicroProfile(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !microprofileFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	fw := ctx.Framework
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// Fault Tolerance: @Retry
	for _, m := range mpRetryRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[4]:m[5]]
		ref := "scope:pattern:mp_fault_tolerance_retry:" + fp + ":" + methodName
		props := map[string]any{"framework": fw, "fault_tolerance_type": "retry"}
		if m[2] >= 0 {
			for _, pm := range mpAnnotationParamRE.FindAllStringSubmatch(source[m[2]:m[3]], -1) {
				props[pm[1]] = pm[2]
			}
		}
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICROPROFILE_FAULT_TOLERANCE", Ref: ref,
			Properties: props,
		})
	}

	// @CircuitBreaker
	for _, m := range mpCircuitBreakerRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[4]:m[5]]
		ref := "scope:pattern:mp_fault_tolerance_circuit_breaker:" + fp + ":" + methodName
		props := map[string]any{"framework": fw, "fault_tolerance_type": "circuit_breaker"}
		if m[2] >= 0 {
			for _, pm := range mpAnnotationParamRE.FindAllStringSubmatch(source[m[2]:m[3]], -1) {
				props[pm[1]] = pm[2]
			}
		}
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICROPROFILE_FAULT_TOLERANCE", Ref: ref,
			Properties: props,
		})
	}

	// @Fallback
	for _, m := range mpFallbackRE.FindAllStringSubmatchIndex(source, -1) {
		paramsStr := ""
		if m[2] >= 0 {
			paramsStr = source[m[2]:m[3]]
		}
		methodName := source[m[4]:m[5]]
		props := map[string]any{"framework": fw, "fault_tolerance_type": "fallback"}
		var fallbackTarget string
		if fm := mpFallbackMethodRE.FindStringSubmatch(paramsStr); fm != nil {
			fallbackTarget = fm[1]
			props["fallback_method"] = fallbackTarget
		}

		ref := "scope:pattern:mp_fault_tolerance_fallback:" + fp + ":" + methodName
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICROPROFILE_FAULT_TOLERANCE", Ref: ref,
			Properties: props,
		}) && fallbackTarget != "" {
			targetRef := "scope:pattern:mp_fallback_target:" + fp + ":" + fallbackTarget
			addRel(&result, seenRels, Relationship{
				SourceRef: ref, TargetRef: targetRef, RelationshipType: "DEPENDS_ON",
				Properties: map[string]string{"kind": "fallback", "fallback_method": fallbackTarget},
			})
		}
	}

	// Health checks
	for _, m := range mpHealthCheckRE.FindAllStringSubmatchIndex(source, -1) {
		kind := source[m[2]:m[3]]
		className := source[m[4]:m[5]]
		ref := "scope:operation:mp_health_" + toLowerCase(kind) + ":" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICROPROFILE_HEALTH", Ref: ref,
			Properties: map[string]any{"framework": fw, "health_type": toLowerCase(kind)},
		})
	}

	// REST Client
	for _, m := range mpRegisterRestClientRE.FindAllStringSubmatchIndex(source, -1) {
		ifaceName := source[m[2]:m[3]]
		ref := "scope:component:mp_rest_client:" + fp + ":" + ifaceName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ifaceName, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICROPROFILE_REST_CLIENT", Ref: ref,
			Properties: map[string]any{"framework": fw, "component_kind": "rest_client"},
		})
	}

	// Reactive Messaging
	incomingChannels := make(map[string]string)
	outgoingChannels := make(map[string]string)
	for _, m := range mpIncomingRE.FindAllStringSubmatchIndex(source, -1) {
		channel := source[m[2]:m[3]]
		methodName := source[m[4]:m[5]]
		ref := "scope:operation:mp_messaging_incoming:" + fp + ":" + methodName + ":" + channel
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICROPROFILE_REACTIVE_MESSAGING", Ref: ref,
			Properties: map[string]any{"framework": fw, "channel": channel, "direction": "incoming"},
		}) {
			incomingChannels[channel] = ref
		}
	}
	for _, m := range mpOutgoingRE.FindAllStringSubmatchIndex(source, -1) {
		channel := source[m[2]:m[3]]
		methodName := source[m[4]:m[5]]
		ref := "scope:operation:mp_messaging_outgoing:" + fp + ":" + methodName + ":" + channel
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_MICROPROFILE_REACTIVE_MESSAGING", Ref: ref,
			Properties: map[string]any{"framework": fw, "channel": channel, "direction": "outgoing"},
		}) {
			outgoingChannels[channel] = ref
		}
	}
	for channel, outRef := range outgoingChannels {
		if inRef, ok := incomingChannels[channel]; ok {
			addRel(&result, seenRels, Relationship{
				SourceRef: outRef, TargetRef: inRef, RelationshipType: "DEPENDS_ON",
				Properties: map[string]string{"kind": "reactive_messaging", "channel": channel},
			})
		}
	}

	// Metrics
	for _, pair := range []struct {
		re   *regexp.Regexp
		kind string
	}{
		{mpCountedRE, "counted"}, {mpTimedRE, "timed"},
	} {
		for _, m := range pair.re.FindAllStringSubmatchIndex(source, -1) {
			methodName := source[m[4]:m[5]]
			ref := "scope:pattern:mp_metric_" + pair.kind + ":" + fp + ":" + methodName
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: methodName, Kind: "SCOPE.Pattern", SourceFile: fp,
				LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_MICROPROFILE_METRICS", Ref: ref,
				Properties: map[string]any{"framework": fw, "metric_type": pair.kind},
			})
		}
	}

	return result
}

func toLowerCase(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
