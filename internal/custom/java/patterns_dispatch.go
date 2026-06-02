package java

// patterns_dispatch.go — wires the 35 Extract*(ctx PatternContext) PatternResult
// pattern extractors into the LIVE custom-extractor pipeline (epic #3584, issue
// #3586).
//
// Background
// ----------
// The 35 ExtractXxx functions in this package historically had ZERO non-test
// callers: PatternContext was only ever constructed in *_test.go, and their
// `func(ctx PatternContext) PatternResult` signature is incompatible with the
// live extractor interface (`Extract(ctx context.Context, file extreg.FileInput)
// ([]types.EntityRecord, error)`). They were therefore dead code — the framework
// detection they implement (Spring DI graphs, JPA associations, Android
// components, JAX-RS routing, bean validation, …) never reached the graph, which
// is why #3585 honest-downgraded 248 capability cells that cited them.
//
// This file is the single registered adapter that re-enables them. It registers
// under `custom_java_patterns`, which dispatches because `customPrefixForLanguage`
// maps `java -> "custom_java_"` and this key carries that prefix (verified by the
// dispatch-parity guard, TestEveryRegisteredCustomKeyDispatches).
//
// What it does on each java FileInput
// -----------------------------------
//  1. Builds a PatternContext from the FileInput (source string, path, "java").
//  2. Detects candidate framework tokens from cheap source markers (the same
//     framework strings the test constructors hand-feed, e.g. sbCtxFw(src,
//     "spring_boot")). Because every ExtractXxx self-gates on ctx.Framework
//     against its own framework set, running each function once per detected
//     candidate is equivalent to the test harness — non-matching frameworks are
//     rejected by the gate at zero extraction cost.
//  3. Runs ALL 35 ExtractXxx functions, collecting their PatternResults.
//  4. Converts SecondaryEntity -> types.EntityRecord (preserving Kind/Subtype/
//     properties/provenance) and Relationship -> an embedded
//     types.RelationshipRecord attached to the SOURCE entity (FromID implicit,
//     ToID = target ref string), exactly as the wired django/rails custom
//     extractors emit edges.
//  5. Dedups by structural ref and returns.

import (
	"context"
	"fmt"
	"strings"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_java_patterns", &javaPatternsExtractor{})
}

type javaPatternsExtractor struct{}

func (e *javaPatternsExtractor) Language() string { return "custom_java_patterns" }

// patternFn is the shared shape of all 35 dead pattern extractors.
type patternFn func(ctx PatternContext) PatternResult

// allPatternExtractors is the authoritative enumeration of every ExtractXxx
// pattern function in this package. Keeping it as an explicit list (rather than
// reflection) makes the wiring auditable: the count here must equal the number
// of `func ExtractXxx(ctx PatternContext) PatternResult` definitions.
var allPatternExtractors = []patternFn{
	ExtractAkkaHTTP,
	ExtractAndroid,
	ExtractBeanValidation,
	ExtractCDIInterceptors,
	ExtractDropwizard,
	ExtractGWT,
	ExtractGWTDataFetching,
	ExtractHelidonFilters,
	ExtractHibernate,
	ExtractJakartaEE,
	ExtractJakartaEEAdvanced,
	ExtractJakartaJaxrsDTO,
	ExtractJavaDIScopeDeepen,
	ExtractJavaMethodSecurity,
	ExtractJavalin,
	ExtractJaxrsFilters,
	ExtractJUnit5,
	ExtractLangChain4J,
	ExtractMicronaut,
	ExtractMicronautAOP,
	ExtractMicroProfile,
	ExtractObservability,
	ExtractPlay,
	ExtractQuarkus,
	ExtractQuartzJava,
	ExtractSpringAOP,
	ExtractSpringBoot,
	ExtractSpringDIDeepen,
	ExtractSpringEcosystem,
	ExtractSpringGraphQL,
	ExtractSpringRequestResponse,
	ExtractSpringWebFlux,
	ExtractStruts,
	ExtractTransactional,
	ExtractVaadin,
	ExtractVertx,
}

// frameworkMarker pairs a canonical framework token (the value the ExtractXxx
// gates accept) with a cheap substring signal that, if present in the source,
// makes that framework a candidate. The token strings mirror exactly what the
// test constructors pass (e.g. sbCtxFw(src, "spring_boot"), bvCtx -> "bean_validation").
//
// Detection is intentionally over-inclusive: a token becoming a candidate only
// means each ExtractXxx gets a chance to run with it; the function's own
// framework gate AND its internal regex signals still decide whether anything is
// emitted. Over-detection costs a few rejected gate checks, never wrong output.
type frameworkMarker struct {
	token  string
	signal string
}

var frameworkMarkers = []frameworkMarker{
	// Spring family.
	{"spring_boot", "org.springframework"},
	{"spring_boot", "@SpringBootApplication"},
	{"spring_boot", "@RestController"},
	{"spring_boot", "@Controller"},
	{"spring_boot", "@Service"},
	{"spring_boot", "@Repository"},
	{"spring_boot", "@Autowired"},
	{"spring_boot", "@Configuration"},
	{"spring_mvc", "@RequestMapping"},
	{"spring_webflux", "reactor.core"},
	{"spring_webflux", "org.springframework.web.reactive"},
	{"spring_webflux", "Mono<"},
	{"spring_webflux", "Flux<"},

	// Spring for GraphQL — annotation-driven GraphQL server.
	{"spring_graphql", "org.springframework.graphql"},
	{"spring_graphql", "@QueryMapping"},
	{"spring_graphql", "@MutationMapping"},
	{"spring_graphql", "@SubscriptionMapping"},
	{"spring_graphql", "@SchemaMapping"},

	// Netflix DGS — annotation-driven GraphQL server.
	{"dgs", "com.netflix.graphql.dgs"},
	{"dgs", "@DgsComponent"},
	{"dgs", "@DgsQuery"},
	{"dgs", "@DgsMutation"},
	{"dgs", "@DgsSubscription"},
	{"dgs", "@DgsData"},

	// JPA / Hibernate.
	{"jpa", "javax.persistence"},
	{"jpa", "jakarta.persistence"},
	{"jpa", "@Entity"},
	{"hibernate", "org.hibernate"},
	{"spring_data_jpa", "org.springframework.data.jpa"},

	// Jakarta / Java EE.
	{"jakarta_ee", "jakarta.ejb"},
	{"jakarta_ee", "jakarta.enterprise"},
	{"jakarta_ee", "jakarta.inject"},
	{"jakarta_ee", "javax.ejb"},
	{"jakarta_ee", "javax.enterprise"},
	{"jakarta_ee", "@Stateless"},
	{"jakarta_ee", "@Stateful"},
	{"jakarta_ee", "@ApplicationScoped"},

	// JAX-RS.
	{"jaxrs", "jakarta.ws.rs"},
	{"jaxrs", "javax.ws.rs"},
	{"jaxrs", "@Path"},

	// MicroProfile.
	{"microprofile", "org.eclipse.microprofile"},
	{"microprofile", "@RegisterRestClient"},

	// Quarkus / Micronaut / Helidon / Dropwizard / Vert.x / Javalin / Akka.
	{"quarkus", "io.quarkus"},
	{"micronaut", "io.micronaut"},
	{"helidon", "io.helidon"},
	{"dropwizard", "io.dropwizard"},
	{"vertx", "io.vertx"},
	{"javalin", "io.javalin"},
	{"akka_http", "akka.http"},

	// LangChain4j.
	{"langchain4j", "dev.langchain4j"},

	// Web frameworks / UI toolkits.
	{"struts", "org.apache.struts"},
	{"struts", "ActionSupport"},
	{"gwt", "com.google.gwt"},
	{"vaadin", "com.vaadin"},
	{"play", "play.mvc"},
	{"play", "play.api"},

	// Android.
	{"android", "android.app"},
	{"android", "androidx."},
	{"android", "android.content"},
	{"android", "android.os.Bundle"},

	// Scheduling / testing / validation.
	{"quartz", "org.quartz"},
	{"junit5", "org.junit.jupiter"},
	{"bean_validation", "jakarta.validation"},
	{"bean_validation", "javax.validation"},
	{"bean_validation", "@Valid"},
	{"bean_validation", "@NotNull"},
}

// detectFrameworks returns the set of candidate framework tokens whose marker
// signal appears in src. A baseline set of always-tried tokens is unioned in so
// that framework-agnostic extractors (e.g. ExtractQuartzJava, which gates only
// on language and self-gates on its own regexes) still get a chance to run even
// when no import marker is present (some sources reference frameworks only via
// annotations the markers above may not enumerate exhaustively).
func detectFrameworks(src string) map[string]bool {
	out := make(map[string]bool)
	for _, m := range frameworkMarkers {
		if strings.Contains(src, m.signal) {
			out[m.token] = true
		}
	}
	return out
}

func (e *javaPatternsExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	// The pattern extractors that allow Kotlin still accept ctx.Language=="java"
	// only for the java markers; Kotlin sources flow through the kotlin custom
	// dispatch. Here we only handle the java language key this extractor is
	// selected under.
	if strings.ToLower(file.Language) != "java" {
		return nil, nil
	}

	src := string(file.Content)
	candidates := detectFrameworks(src)
	if len(candidates) == 0 {
		// No framework signal at all — nothing for these framework-specific
		// extractors to do. (Quartz/JUnit self-gate via their own markers which
		// are part of frameworkMarkers, so an empty candidate set genuinely
		// means "no recognised Java framework in this file".)
		return nil, nil
	}

	// Aggregate results across every (extractor, candidate-framework) pairing.
	var agg PatternResult
	seenEnt := make(map[string]bool)
	seenRel := make(map[relKey]bool)

	for token := range candidates {
		pctx := PatternContext{
			Source:    src,
			Language:  "java",
			Framework: token,
			FilePath:  file.Path,
		}
		for _, fn := range allPatternExtractors {
			res := fn(pctx)
			for _, ent := range res.Entities {
				addEntity(&agg, seenEnt, ent)
			}
			for _, rel := range res.Relationships {
				addRel(&agg, seenRel, rel)
			}
		}
	}

	return patternResultToRecords(&agg, file.Path), nil
}

// patternResultToRecords converts the aggregated PatternResult into the live
// []types.EntityRecord shape. SecondaryEntity becomes an EntityRecord keyed by
// its structural Ref (so cross-entity Relationships resolve), and each
// Relationship is attached as an embedded types.RelationshipRecord on the entity
// whose Ref == Relationship.SourceRef — mirroring how the wired django/rails
// custom extractors emit edges (ToID = target structural ref, FromID implicit
// from the carrying entity; the resolver pass binds the ref later).
//
// Relationships whose SourceRef does not correspond to an emitted entity are
// dropped (the same silent-drop policy the django extractor uses for unresolved
// edges) — there is no carrier entity to hang them on.
func patternResultToRecords(res *PatternResult, filePath string) []types.EntityRecord {
	if res == nil || len(res.Entities) == 0 {
		return nil
	}

	// Build entity records, indexed by structural ref so relationships can be
	// hung on the correct carrier. Index preserves first-seen order.
	records := make([]types.EntityRecord, 0, len(res.Entities))
	byRef := make(map[string]int, len(res.Entities)) // ref -> index in records

	for _, se := range res.Entities {
		rec := makeEntity(se.Name, se.Kind, se.Subtype, fileOr(se.SourceFile, filePath), "java", se.LineStart)
		if se.LineEnd > rec.EndLine {
			rec.EndLine = se.LineEnd
		}
		// Carry provenance + the structural ref so downstream passes can match.
		if se.Provenance != "" {
			rec.Properties["provenance"] = se.Provenance
		}
		if se.Ref != "" {
			rec.Properties["ref"] = se.Ref
		}
		// Merge the extractor-set properties (string-coerced).
		for k, v := range se.Properties {
			rec.Properties[k] = stringifyProp(v)
		}
		byRef[se.Ref] = len(records)
		records = append(records, rec)
	}

	// Attach relationships to their carrier (source) entity.
	for _, r := range res.Relationships {
		idx, ok := byRef[r.SourceRef]
		if !ok {
			continue // no carrier entity for this edge — drop, like django.
		}
		rr := types.RelationshipRecord{
			ToID:       r.TargetRef,
			Kind:       r.RelationshipType,
			Properties: map[string]string{},
		}
		for k, v := range r.Properties {
			rr.Properties[k] = v
		}
		rr.Properties["language"] = "java"
		records[idx].Relationships = append(records[idx].Relationships, rr)
	}

	return records
}

// fileOr returns primary if non-empty, else fallback.
func fileOr(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

// stringifyProp coerces a SecondaryEntity property value (map[string]any) into
// the string form types.EntityRecord.Properties requires. Slices are joined with
// commas (the only non-scalar the Extract* funcs emit is []string path_params).
func stringifyProp(v any) string {
	switch vv := v.(type) {
	case string:
		return vv
	case []string:
		return strings.Join(vv, ",")
	case bool:
		if vv {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", vv))
	}
}
