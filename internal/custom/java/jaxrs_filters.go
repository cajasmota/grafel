package java

import "regexp"

// JAX-RS + MicroProfile middleware filter extractor — middleware_coverage cells.
//
// JAX-RS defines a portable filter model via ContainerRequestFilter,
// ContainerResponseFilter (server-side) and ClientRequestFilter /
// ClientResponseFilter (client-side). Implementations register by annotating
// the class with @Provider (or through Application.getClasses()).
// MicroProfile builds its REST Client on top of JAX-RS and uses the same
// filter interfaces. Helidon MP / Open Liberty / Payara / Quarkus all support
// this model out of the box.
//
// Coverage cells delivered (#3083):
//   - lang.java.framework.jaxrs       → Middleware/middleware_coverage (missing → partial)
//   - lang.java.framework.microprofile → Middleware/middleware_coverage (missing → partial)
//
// Detected patterns:
//  1. @Provider + implements ContainerRequestFilter  → server request filter
//  2. @Provider + implements ContainerResponseFilter → server response filter
//  3. @Provider + implements ClientRequestFilter     → client request filter
//  4. @Provider anywhere in preceding window + filter impl class
//  5. @PreMatching ContainerRequestFilter variant
//  6. @NameBinding meta-annotation on a custom binding annotation

// jaxrsFilterFrameworks covers vanilla JAX-RS and its MicroProfile layer.
// Helidon is handled by helidon_filters.go; this extractor deliberately
// does NOT gate on helidon to avoid double-counting.
var jaxrsFilterFrameworks = map[string]bool{
	"jaxrs": true, "jax-rs": true,
	"microprofile": true, "eclipse-microprofile": true,
	"open_liberty": true, "payara": true,
}

var (
	// @Provider class implementing a server-side filter (canonical: @Provider before class).
	jaxrsProviderFilterRE = regexp.MustCompile(
		`(?s)@Provider\b[^{]*?` +
			`(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)` +
			`[^{]*?implements\s+[^{]*(ContainerRequestFilter|ContainerResponseFilter|ClientRequestFilter|ClientResponseFilter)\b`)

	// Filter class that implements a filter interface (may have @Provider earlier in the annotation block).
	jaxrsFilterClassRE = regexp.MustCompile(
		`(?s)class\s+(\w+)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*(ContainerRequestFilter|ContainerResponseFilter|ClientRequestFilter|ClientResponseFilter)\b`)

	// @PreMatching specialization of ContainerRequestFilter.
	jaxrsPreMatchingRE = regexp.MustCompile(
		`(?s)@PreMatching\b[^{]*?` +
			`(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)` +
			`[^{]*?implements\s+[^{]*?ContainerRequestFilter\b`)

	// @NameBinding meta-annotation on a custom annotation type.
	jaxrsNameBindingRE = regexp.MustCompile(
		`(?s)@NameBinding\b.*?(?:public\s+)?@interface\s+(\w+)`)

	// Reuse providerAnnotRE from helidon_filters.go (same package).
	// var jaxrsProviderAnnotRE already defined in helidon_filters.go as providerAnnotRE.
)

// jaxrsFilterKind maps the interface name to a human-readable filter kind.
func jaxrsFilterKind(iface string) string {
	switch iface {
	case "ContainerRequestFilter":
		return "container_request_filter"
	case "ContainerResponseFilter":
		return "container_response_filter"
	case "ClientRequestFilter":
		return "client_request_filter"
	case "ClientResponseFilter":
		return "client_response_filter"
	default:
		return toLowerCase(iface)
	}
}

// ExtractJaxrsFilters detects JAX-RS/MicroProfile middleware filter registrations.
func ExtractJaxrsFilters(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !jaxrsFilterFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	fw := ctx.Framework
	seenRefs := make(map[string]bool)

	// 1. Canonical form: @Provider immediately before class + implements <FilterInterface>.
	for _, m := range jaxrsProviderFilterRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		iface := source[m[4]:m[5]]
		ref := "scope:component:jaxrs_filter:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAXRS_PROVIDER_FILTER", Ref: ref,
			Properties: map[string]any{
				"filter_type": jaxrsFilterKind(iface),
				"framework":   fw,
				"middleware":  "jaxrs_provider_filter",
			},
		})
	}

	// 2. Filter class where @Provider may appear in the preceding annotation block.
	for _, m := range jaxrsFilterClassRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		iface := source[m[4]:m[5]]
		ref := "scope:component:jaxrs_filter:" + fp + ":" + className
		if seenRefs[ref] {
			continue
		}
		windowStart := m[0]
		if windowStart > 300 {
			windowStart = m[0] - 300
		} else {
			windowStart = 0
		}
		window := source[windowStart:m[0]]
		if !providerAnnotRE.MatchString(window) {
			continue
		}
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAXRS_PROVIDER_FILTER", Ref: ref,
			Properties: map[string]any{
				"filter_type": jaxrsFilterKind(iface),
				"framework":   fw,
				"middleware":  "jaxrs_provider_filter",
			},
		})
	}

	// 3. @PreMatching filter (subset of ContainerRequestFilter).
	for _, m := range jaxrsPreMatchingRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := "scope:component:jaxrs_filter:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAXRS_PROVIDER_FILTER", Ref: ref,
			Properties: map[string]any{
				"filter_type": "prematching_request_filter",
				"framework":   fw,
				"middleware":  "jaxrs_prematching_filter",
			},
		})
	}

	// 4. @NameBinding custom filter-binding annotation.
	for _, m := range jaxrsNameBindingRE.FindAllStringSubmatchIndex(source, -1) {
		annName := source[m[2]:m[3]]
		ref := "scope:pattern:jaxrs_name_binding:" + fp + ":" + annName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: annName, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_JAXRS_NAME_BINDING", Ref: ref,
			Properties: map[string]any{
				"framework":  fw,
				"middleware": "name_binding",
			},
		})
	}

	return result
}
