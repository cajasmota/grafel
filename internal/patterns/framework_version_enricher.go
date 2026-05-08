package patterns

import (
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// frameworkVersionEnricher detects framework version declarations.
// Matches Python framework_version_enricher.py.
type frameworkVersionEnricher struct{}

var (
	fvSpringParentRE    = regexp.MustCompile(`<parent>[\s\S]*?<artifactId>\s*spring-boot-starter-parent\s*</artifactId>[\s\S]*?</parent>`)
	fvXMLVersionRE      = regexp.MustCompile(`<version>\s*([^<\s]+)\s*</version>`)
	fvGradleSpringRE    = regexp.MustCompile(`org\.springframework\.boot['"]\s+version\s+["']([^"']+)["']`)
	fvDjangoRE          = regexp.MustCompile(`Django==([0-9][^"'\s]+)`)
	fvFastAPIRE         = regexp.MustCompile(`fastapi==([0-9][^"'\s]+)`)
	fvRailsRE           = regexp.MustCompile(`gem ['"]rails['"],\s*['"]([^'"]+)['"]`)
	fvExpressRE         = regexp.MustCompile(`"express":\s*["']([^"']+)["']`)
	fvASPNetTFRE        = regexp.MustCompile(`<TargetFramework>\s*([^<\s]+)\s*</TargetFramework>`)
	fvGoModRE           = regexp.MustCompile(`(?m)^go\s+(\d+\.\d+(?:\.\d+)?)`)
	fvNestJSRE          = regexp.MustCompile(`"@nestjs/core":\s*["']([^"']+)["']`)
	fvNextJSRE          = regexp.MustCompile(`"next":\s*["']([^"']+)["']`)
)

func (f *frameworkVersionEnricher) Category() string { return "framework_version" }

func (f *frameworkVersionEnricher) AppliesTo(src string) bool {
	return fvSpringParentRE.MatchString(src) ||
		fvGradleSpringRE.MatchString(src) ||
		fvDjangoRE.MatchString(src) ||
		fvFastAPIRE.MatchString(src) ||
		fvRailsRE.MatchString(src) ||
		fvExpressRE.MatchString(src) ||
		fvASPNetTFRE.MatchString(src) ||
		fvGoModRE.MatchString(src) ||
		fvNestJSRE.MatchString(src) ||
		fvNextJSRE.MatchString(src)
}

func (f *frameworkVersionEnricher) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(framework, version string) {
		key := framework + ":" + version
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"framework_version_"+framework, "SCOPE.Pattern", "framework_version", language, 1,
			map[string]string{"kind": "framework_version", "framework": framework, "version": version}))
	}

	checks := []struct {
		re        *regexp.Regexp
		framework string
	}{
		{fvGradleSpringRE, "spring-boot"},
		{fvDjangoRE, "django"},
		{fvFastAPIRE, "fastapi"},
		{fvRailsRE, "rails"},
		{fvExpressRE, "express"},
		{fvASPNetTFRE, "aspnet"},
		{fvGoModRE, "go"},
		{fvNestJSRE, "nestjs"},
		{fvNextJSRE, "nextjs"},
	}

	for _, ch := range checks {
		if m := ch.re.FindStringSubmatch(src); m != nil {
			emit(ch.framework, m[1])
		}
	}

	// Spring pom.xml
	if fvSpringParentRE.MatchString(src) {
		block := fvSpringParentRE.FindString(src)
		if m := fvXMLVersionRE.FindStringSubmatch(block); m != nil {
			emit("spring-boot", m[1])
		}
	}

	return results
}

func init() {
	Register(&frameworkVersionEnricher{})
}
