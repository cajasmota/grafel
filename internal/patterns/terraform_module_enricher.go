package patterns

import (
	"fmt"
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// terraformModuleEnricher detects Terraform module ownership and structure.
// Matches Python terraform_module_enricher.py.
type terraformModuleEnricher struct{}

var (
	tmModuleBlockRE  = regexp.MustCompile(`(?m)^module\s+"([^"]+)"\s*\{`)
	tmSourceRE       = regexp.MustCompile(`source\s*=\s*["']([^"']+)["']`)
	tmVersionRE      = regexp.MustCompile(`version\s*=\s*["']([^"']+)["']`)
	tmOwnerCommentRE = regexp.MustCompile(`#\s*(?:owner|team|owned-by)\s*:\s*(\S+)`)
	tmProviderRE     = regexp.MustCompile(`(?m)^provider\s+"([^"]+)"\s*\{`)
	tmVariableRE     = regexp.MustCompile(`(?m)^variable\s+"([^"]+)"\s*\{`)
)

func (t *terraformModuleEnricher) Category() string { return "terraform_module_ownership" }

func (t *terraformModuleEnricher) AppliesTo(src string) bool {
	return tmModuleBlockRE.MatchString(src) ||
		tmProviderRE.MatchString(src) ||
		tmVariableRE.MatchString(src)
}

func (t *terraformModuleEnricher) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	// Extract module ownership comment if present
	owner := ""
	if m := tmOwnerCommentRE.FindStringSubmatch(src); m != nil {
		owner = m[1]
	}

	// Module blocks
	for _, m := range tmModuleBlockRE.FindAllStringSubmatchIndex(src, -1) {
		modName := src[m[2]:m[3]]
		key := "module:" + modName
		if seen[key] {
			continue
		}
		seen[key] = true

		props := map[string]string{
			"kind":        "terraform_module_ownership",
			"module_name": modName,
		}
		if owner != "" {
			props["owner"] = owner
		}

		// Find source after this module block
		rest := src[m[1]:]
		if sm := tmSourceRE.FindStringSubmatch(rest); sm != nil {
			props["source"] = sm[1]
		}
		if vm := tmVersionRE.FindStringSubmatch(rest); vm != nil {
			props["version"] = vm[1]
		}

		results = append(results, makeEntity(filePath,
			"terraform_module_"+modName, "SCOPE.Component", "terraform_module", language,
			lineOf(src, m[0]), props))
	}

	// Provider blocks
	for idx, m := range tmProviderRE.FindAllStringSubmatchIndex(src, -1) {
		provName := src[m[2]:m[3]]
		key := fmt.Sprintf("provider:%s:%d", provName, idx)
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"terraform_provider_"+provName, "SCOPE.Config", "terraform_provider", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "terraform_module_ownership", "provider": provName}))
	}

	return results
}

func init() {
	Register(&terraformModuleEnricher{})
}
