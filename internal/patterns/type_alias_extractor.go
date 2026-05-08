package patterns

import (
	"fmt"
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// typeAliasExtractor detects type alias definitions.
// Matches Python type_alias_extractor.py.
type typeAliasExtractor struct{}

var (
	taTypescriptAliasRE  = regexp.MustCompile(`type\s+(\w+)\s*(?:<[^>]*>)?\s*=\s*([^\n;]+)`)
	taTypescriptInterfRE = regexp.MustCompile(`interface\s+(\w+)\s*(?:extends\s+([^{]+))?\s*\{`)
	taKotlinTypealiasRE  = regexp.MustCompile(`typealias\s+(\w+)\s*=\s*(\S+)`)
	taScalaTypeAliasRE   = regexp.MustCompile(`type\s+(\w+)\s*=\s*([^\n]+)`)
	taRustTypeAliasRE    = regexp.MustCompile(`type\s+(\w+)\s*=\s*([^;]+);`)
	taGoTypeSingleAlRE   = regexp.MustCompile(`(?m)^type\s+(\w+)\s*=\s*(\S+)`)
	taGoTypeSingleDefRE  = regexp.MustCompile(`(?m)^type\s+(\w+)\s+(\w+)$`)
)

func (t *typeAliasExtractor) Category() string { return "type_alias" }

func (t *typeAliasExtractor) AppliesTo(src string) bool {
	return taTypescriptAliasRE.MatchString(src) ||
		taKotlinTypealiasRE.MatchString(src) ||
		taScalaTypeAliasRE.MatchString(src) ||
		taRustTypeAliasRE.MatchString(src) ||
		taGoTypeSingleAlRE.MatchString(src) ||
		taGoTypeSingleDefRE.MatchString(src)
}

func (t *typeAliasExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}
	idx := 0

	emit := func(key, aliasName, aliasOf string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		idx++
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("type_alias_%s", aliasName),
			"SCOPE.Component", "type_alias", language, line,
			map[string]string{"kind": "type_alias", "alias_name": aliasName, "alias_of": aliasOf}))
	}

	switch language {
	case "typescript", "javascript":
		for _, m := range taTypescriptAliasRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			aliasOf := src[m[4]:m[5]]
			emit("ts:"+name, name, aliasOf, lineOf(src, m[0]))
		}
		for _, m := range taTypescriptInterfRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			extends := ""
			if m[4] >= 0 {
				extends = src[m[4]:m[5]]
			}
			emit("ts:iface:"+name, name, extends, lineOf(src, m[0]))
		}
	case "kotlin":
		for _, m := range taKotlinTypealiasRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			aliasOf := src[m[4]:m[5]]
			emit("kt:"+name, name, aliasOf, lineOf(src, m[0]))
		}
	case "scala":
		for _, m := range taScalaTypeAliasRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			aliasOf := src[m[4]:m[5]]
			emit("scala:"+name, name, aliasOf, lineOf(src, m[0]))
		}
	case "rust":
		for _, m := range taRustTypeAliasRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			aliasOf := src[m[4]:m[5]]
			emit("rust:"+name, name, aliasOf, lineOf(src, m[0]))
		}
	case "go":
		for _, m := range taGoTypeSingleAlRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			aliasOf := src[m[4]:m[5]]
			emit("go:alias:"+name, name, aliasOf, lineOf(src, m[0]))
		}
		for _, m := range taGoTypeSingleDefRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			underlying := src[m[4]:m[5]]
			emit("go:def:"+name, name, underlying, lineOf(src, m[0]))
		}
	}

	return results
}

func init() {
	Register(&typeAliasExtractor{})
}
