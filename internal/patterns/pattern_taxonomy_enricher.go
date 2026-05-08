package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// patternTaxonomyEnricher classifies files by architectural pattern.
// Matches Python pattern_taxonomy_enricher.py.
type patternTaxonomyEnricher struct{}

var (
	ptMVCControllersRE = regexp.MustCompile(`(?i)/controllers/`)
	ptMVCViewsRE       = regexp.MustCompile(`(?i)/views/`)
	ptMVCModelsRE      = regexp.MustCompile(`(?i)/models/`)
	ptMVVMRE           = regexp.MustCompile(`(?i)/viewmodels/`)
	ptRepositoryRE     = regexp.MustCompile(`(?i)/repositories?/`)
	ptServiceLayerRE   = regexp.MustCompile(`(?i)/services?/`)
	ptMiddlewareRE     = regexp.MustCompile(`(?i)/middlewares?/`)
	ptHandlerRE        = regexp.MustCompile(`(?i)/handlers?/`)
	ptUseCaseRE        = regexp.MustCompile(`(?i)/use_?cases?/`)
	// Source-level patterns
	ptObserverRE  = regexp.MustCompile(`(?:addEventListener|on\s*\(["']|subscribe\s*\(|EventEmitter)`)
	ptFactoryRE   = regexp.MustCompile(`\b(?:Factory|createInstance|getInstance|newInstance)\s*\(`)
	ptSingletonRE = regexp.MustCompile(`\b(?:getInstance|_instance|__instance)\b`)
	ptBuilderRE   = regexp.MustCompile(`\b(?:\.build\s*\(\s*\)|Builder\s*\(|WithOptions)\b`)

	// Name-suffix taxonomy rules (case-insensitive suffix matching on entity names).
	// Matches Python pattern_taxonomy_enricher.py _NAME_RULES.
	// Priority: GoF > Enterprise.
	ptNameSingletonRE  = regexp.MustCompile(`(?i)Singleton$`)
	ptNameFactoryRE    = regexp.MustCompile(`(?i)(?:Factory|Builder)$`)
	ptNameObserverRE   = regexp.MustCompile(`(?i)(?:Observer|Listener|EventHandler)$`)
	ptNameStrategyRE   = regexp.MustCompile(`(?i)(?:Strategy|Policy)$`)
	ptNameDecoratorRE  = regexp.MustCompile(`(?i)(?:Decorator|Wrapper)$`)
	ptNameRepoRE       = regexp.MustCompile(`(?i)(?:Repository|Repo)$`)
	ptNameServiceRE    = regexp.MustCompile(`(?i)Service$`)
	ptNameDTORE        = regexp.MustCompile(`(?i)(?:DTO|Request|Response)$`)
	ptNameMiddlewareRE = regexp.MustCompile(`(?i)(?:Middleware|Interceptor)$`)

	// Regex to extract entity names from various languages for name-suffix classification.
	// Covers class/struct/message/service/defn declarations.
	ptEntityNameRE = regexp.MustCompile(`(?m)^\s*(?:(?:pub\s+)?(?:abstract\s+)?(?:class|struct|interface|enum|mixin|extension|object|trait)\s+(\w+)|` +
		`(?:message|service)\s+(\w+)|` +
		`\(defn\s+([\w?!<>*+-]+)|` +
		`(?:pub\s+)?const\s+(\w+)\s*=\s*struct|` +
		`(?:def|fun|func|fn|function)\s+([\w?!<>*+-]+))`)
)

func (p *patternTaxonomyEnricher) Category() string { return "pattern_taxonomy" }

func (p *patternTaxonomyEnricher) AppliesTo(src string) bool {
	return true // applies to all files for path-based classification
}

func (p *patternTaxonomyEnricher) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, taxonomy string) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"taxonomy_"+strings.ReplaceAll(taxonomy, ":", "_"),
			"SCOPE.Pattern", "pattern_taxonomy", language, 1,
			map[string]string{"kind": "pattern_taxonomy", "taxonomy": taxonomy}))
	}

	// Path-based classification
	if ptMVCControllersRE.MatchString(filePath) {
		emit("mvc:controller", "architectural:mvc")
		emit("mvc:layer", "mvc:controller_layer")
	}
	if ptMVCViewsRE.MatchString(filePath) {
		emit("mvc:view", "architectural:mvc")
		emit("mvc:view_layer", "mvc:view_layer")
	}
	if ptMVCModelsRE.MatchString(filePath) {
		emit("mvc:model", "architectural:mvc")
		emit("mvc:model_layer", "mvc:model_layer")
	}
	if ptMVVMRE.MatchString(filePath) {
		emit("mvvm", "architectural:mvvm")
	}
	if ptRepositoryRE.MatchString(filePath) {
		emit("repository", "pattern:repository")
	}
	if ptServiceLayerRE.MatchString(filePath) {
		emit("service_layer", "pattern:service_layer")
	}
	if ptMiddlewareRE.MatchString(filePath) {
		emit("middleware", "pattern:middleware")
	}
	if ptHandlerRE.MatchString(filePath) {
		emit("handler", "pattern:handler")
	}
	if ptUseCaseRE.MatchString(filePath) {
		emit("use_case", "pattern:use_case")
	}

	// Source-level GoF patterns
	if ptObserverRE.MatchString(src) {
		emit("gof:observer", "gof:observer")
	}
	if ptFactoryRE.MatchString(src) {
		emit("gof:factory", "gof:factory")
	}
	if ptSingletonRE.MatchString(src) {
		emit("gof:singleton", "gof:singleton")
	}
	if ptBuilderRE.MatchString(src) {
		emit("gof:builder", "gof:builder")
	}

	// Name-suffix classification: extract entity names from source and classify by suffix.
	// Matches Python pattern_taxonomy_enricher.py _NAME_RULES behavior.
	for _, m := range ptEntityNameRE.FindAllStringSubmatchIndex(src, -1) {
		// Find which capture group matched.
		var entityName string
		matchPos := m[0]
		for g := 2; g < len(m); g += 2 {
			if m[g] >= 0 {
				entityName = src[m[g]:m[g+1]]
				break
			}
		}
		if entityName == "" {
			continue
		}

		taxonomy := classifyNameSuffix(entityName)
		if taxonomy == "" {
			continue
		}

		key := "name:" + taxonomy
		if seen[key] {
			continue
		}
		seen[key] = true

		// Use the line of the matched entity definition for location parity.
		line := lineOf(src, matchPos)
		results = append(results, makeEntity(filePath,
			"pattern_taxonomy:"+taxonomy,
			"SCOPE.Pattern", "pattern_taxonomy", language, line,
			map[string]string{"kind": "pattern_taxonomy", "taxonomy": taxonomy}))
	}

	return results
}

// classifyNameSuffix checks if a name matches any taxonomy suffix rule.
// Returns the taxonomy string (e.g., "enterprise:dto") or empty string.
// Priority: GoF > Enterprise (matches Python ordering).
func classifyNameSuffix(name string) string {
	// GoF patterns
	if ptNameSingletonRE.MatchString(name) {
		return "gof:singleton"
	}
	if ptNameFactoryRE.MatchString(name) {
		return "gof:factory"
	}
	if ptNameObserverRE.MatchString(name) {
		return "gof:observer"
	}
	if ptNameStrategyRE.MatchString(name) {
		return "gof:strategy"
	}
	if ptNameDecoratorRE.MatchString(name) {
		return "gof:decorator"
	}
	// Enterprise patterns
	if ptNameRepoRE.MatchString(name) {
		return "enterprise:repository"
	}
	if ptNameServiceRE.MatchString(name) {
		return "enterprise:service"
	}
	if ptNameDTORE.MatchString(name) {
		return "enterprise:dto"
	}
	if ptNameMiddlewareRE.MatchString(name) {
		return "enterprise:middleware"
	}
	return ""
}

func init() {
	Register(&patternTaxonomyEnricher{})
}
