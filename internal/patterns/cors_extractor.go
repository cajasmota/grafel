package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// corsExtractor detects CORS middleware configuration across frameworks.
// Matches Python cors_extractor.py.
type corsExtractor struct{}

var (
	corsExpressRE          = regexp.MustCompile(`(?:app|router)\s*\.\s*use\s*\(\s*cors\s*\(\s*(\{[^}]*\})?\s*\)`)
	corsExpressOriginRE    = regexp.MustCompile(`origin\s*:\s*["']([^"']+)["']`)
	corsExpressMethodsRE   = regexp.MustCompile(`methods\s*:\s*["']([^"']+)["']`)
	corsExpressHeadersRE   = regexp.MustCompile(`allowedHeaders\s*:\s*["']([^"']+)["']`)
	corsSpringCrossOriRE   = regexp.MustCompile(`@CrossOrigin\s*(?:\([^)]*\))?`)
	corsSpringOriginsRE    = regexp.MustCompile(`origins\s*=\s*["']([^"']+)["']`)
	corsSpringMethodsRE    = regexp.MustCompile(`allowedMethods\s*=\s*["']([^"']+)["']`)
	corsSpringRegistryRE   = regexp.MustCompile(`addMapping\s*\(\s*["']([^"']+)["']\s*\)`)
	corsSpringAllowedRE    = regexp.MustCompile(`allowedOrigins\s*\(\s*["']([^"']+)["']`)
	corsASPNetAddCorsRE    = regexp.MustCompile(`(?:services|builder\s*\.\s*Services)\s*\.\s*AddCors\s*\(`)
	corsASPNetPolicyNameRE = regexp.MustCompile(`AddPolicy\s*\(\s*["']([^"']+)["']`)
	corsASPNetEnableCorsRE = regexp.MustCompile(`\[EnableCors\s*\(\s*["']([^"']+)["']\s*\)\]`)
	corsFastAPIRE          = regexp.MustCompile(`(?s)add_middleware\s*\(\s*CORSMiddleware\s*,([^)]+)\)`)
	corsFastAPIOriginsRE   = regexp.MustCompile(`(?s)allow_origins\s*=\s*\[([^\]]*)\]`)
	corsFastAPIMethodsRE   = regexp.MustCompile(`(?s)allow_methods\s*=\s*\[([^\]]*)\]`)
	corsStringLiteralRE    = regexp.MustCompile(`["']([^"']+)["']`)
)

var corsImportTokens = []string{
	"CrossOrigin", "CorsRegistry", "CorsConfiguration",
	"AddCors", "EnableCors", "CorsPolicy",
	"CORSMiddleware", "starlette.middleware.cors", "fastapi.middleware.cors",
	"require('cors')", `require("cors")`, "import cors",
}

var corsSourceTokens = []string{
	"cors(", "cors({", "CORSMiddleware", "@CrossOrigin",
	"AddCors(", "EnableCors(", "addMapping(", "allowedOrigins(",
}

func (c *corsExtractor) Category() string { return "cors" }

func (c *corsExtractor) AppliesTo(src string) bool {
	for _, tok := range corsImportTokens {
		if strings.Contains(src, tok) {
			return true
		}
	}
	for _, tok := range corsSourceTokens {
		if strings.Contains(src, tok) {
			return true
		}
	}
	return false
}

func (c *corsExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	// Express: app.use(cors({...}))
	for idx, m := range corsExpressRE.FindAllStringSubmatchIndex(src, -1) {
		opts := ""
		if m[2] >= 0 {
			opts = src[m[2]:m[3]]
		}
		origin := "*"
		if opts != "" {
			if om := corsExpressOriginRE.FindStringSubmatch(opts); om != nil {
				origin = om[1]
			} else {
				origin = "dynamic"
			}
		}
		key := fmt.Sprintf("express:%d", idx)
		if seen[key] {
			continue
		}
		seen[key] = true
		props := map[string]string{"kind": "cors_policy", "framework": "express", "origin_pattern": origin}
		if mm := corsExpressMethodsRE.FindStringSubmatch(opts); mm != nil {
			props["methods"] = mm[1]
		}
		if hm := corsExpressHeadersRE.FindStringSubmatch(opts); hm != nil {
			props["allowed_headers"] = hm[1]
		}
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("cors_policy_express_%d", idx), "SCOPE.Config", "cors_policy", language,
			lineOf(src, m[0]), props))
	}

	// Spring: @CrossOrigin
	for idx, m := range corsSpringCrossOriRE.FindAllStringSubmatchIndex(src, -1) {
		ann := src[m[0]:m[1]]
		origin := "dynamic"
		if om := corsSpringOriginsRE.FindStringSubmatch(ann); om != nil {
			origin = om[1]
		}
		key := fmt.Sprintf("spring_crossorigin:%d", idx)
		if seen[key] {
			continue
		}
		seen[key] = true
		props := map[string]string{"kind": "cors_policy", "framework": "spring", "origin_pattern": origin}
		if mm := corsSpringMethodsRE.FindStringSubmatch(ann); mm != nil {
			props["methods"] = mm[1]
		}
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("cors_policy_spring_crossorigin_%d", idx), "SCOPE.Config", "cors_policy", language,
			lineOf(src, m[0]), props))
	}

	// Spring: addMapping("/**").allowedOrigins("...")
	for idx, m := range corsSpringRegistryRE.FindAllStringSubmatchIndex(src, -1) {
		mapping := src[m[2]:m[3]]
		origin := "dynamic"
		rest := src[m[1]:]
		if om := corsSpringAllowedRE.FindStringSubmatch(rest); om != nil {
			origin = om[1]
		}
		key := fmt.Sprintf("spring_registry:%d", idx)
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("cors_policy_spring_registry_%d", idx), "SCOPE.Config", "cors_policy", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "cors_policy", "framework": "spring", "origin_pattern": origin, "mapping": mapping}))
	}

	// ASP.NET: builder.Services.AddCors(...)
	for idx, m := range corsASPNetAddCorsRE.FindAllStringSubmatchIndex(src, -1) {
		policyName := "default"
		rest := src[m[1]:]
		if pm := corsASPNetPolicyNameRE.FindStringSubmatch(rest); pm != nil {
			policyName = pm[1]
		}
		key := fmt.Sprintf("aspnet_addcors:%d", idx)
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"cors_policy_aspnet_"+policyName, "SCOPE.Config", "cors_policy", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "cors_policy", "framework": "aspnet", "origin_pattern": "dynamic", "policy_name": policyName}))
	}

	// ASP.NET: [EnableCors("PolicyName")]
	for _, m := range corsASPNetEnableCorsRE.FindAllStringSubmatchIndex(src, -1) {
		policyName := src[m[2]:m[3]]
		key := "aspnet_enablecors:" + policyName
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"cors_policy_aspnet_enable_"+policyName, "SCOPE.Config", "cors_policy", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "cors_policy", "framework": "aspnet", "origin_pattern": "dynamic", "policy_name": policyName}))
	}

	// FastAPI: add_middleware(CORSMiddleware, ...)
	for idx, m := range corsFastAPIRE.FindAllStringSubmatchIndex(src, -1) {
		args := src[m[2]:m[3]]
		origin := "dynamic"
		if om := corsFastAPIOriginsRE.FindStringSubmatch(args); om != nil {
			if lm := corsStringLiteralRE.FindStringSubmatch(om[1]); lm != nil {
				origin = lm[1]
			}
		}
		key := fmt.Sprintf("fastapi:%d", idx)
		if seen[key] {
			continue
		}
		seen[key] = true
		props := map[string]string{"kind": "cors_policy", "framework": "fastapi", "origin_pattern": origin}
		if mm := corsFastAPIMethodsRE.FindStringSubmatch(args); mm != nil {
			var methods []string
			for _, mv := range corsStringLiteralRE.FindAllStringSubmatch(mm[1], -1) {
				methods = append(methods, mv[1])
			}
			if len(methods) > 0 {
				props["methods"] = strings.Join(methods, ",")
			}
		}
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("cors_policy_fastapi_%d", idx), "SCOPE.Config", "cors_policy", language,
			lineOf(src, m[0]), props))
	}

	return results
}

func init() {
	Register(&corsExtractor{})
}
