package enrichers

// ConfigProfileEnricher detects multi-environment config files and computes diff metadata.
// Port of Python config_profile_enricher.py (MX-697).

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

var (
	springProfileRe = regexp.MustCompile(`(?:^|/)application-([A-Za-z0-9_-]+)\.(yml|yaml|properties)$`)
	dotenvProfileRe = regexp.MustCompile(`(?:^|/)\.env\.([A-Za-z0-9_-]+)$`)
	configDirRe     = regexp.MustCompile(`(?:^|/)config/([A-Za-z0-9_-]+)\.(yml|yaml|json)$`)
)

var profileAlias = map[string]string{
	"development": "dev",
	"production":  "prod",
	"staging":     "staging",
	"test":        "test",
	"testing":     "test",
}

var comparePairs = [][2]string{
	{"dev", "prod"},
	{"dev", "staging"},
	{"staging", "prod"},
	{"dev", "test"},
}

var nonProfileNames = map[string]bool{
	"webpack": true, "jest": true, "babel": true,
	"rollup": true, "vite": true, "eslint": true,
}

func canonicalProfile(raw string) string {
	lower := strings.ToLower(raw)
	if alias, ok := profileAlias[lower]; ok {
		return alias
	}
	return lower
}

func detectProfileFile(sourceFile string) (profile, groupKey, format string, ok bool) {
	if m := springProfileRe.FindStringSubmatch(sourceFile); m != nil {
		profile = canonicalProfile(m[1])
		format = "yaml"
		if m[2] == "properties" {
			format = "properties"
		}
		groupKey = "spring:" + filepath.Dir(sourceFile)
		ok = true
		return
	}
	if m := dotenvProfileRe.FindStringSubmatch(sourceFile); m != nil {
		profile = canonicalProfile(m[1])
		format = "env"
		groupKey = "dotenv:" + filepath.Dir(sourceFile)
		ok = true
		return
	}
	if m := configDirRe.FindStringSubmatch(sourceFile); m != nil {
		raw := m[1]
		if nonProfileNames[strings.ToLower(raw)] {
			return
		}
		profile = canonicalProfile(raw)
		format = "yaml"
		if m[2] == "json" {
			format = "json"
		}
		groupKey = "configdir:" + filepath.Dir(sourceFile)
		ok = true
		return
	}
	return
}

// ParseYAMLFlat parses a flat YAML file into key->value using indentation tracking.
func ParseYAMLFlat(content string) map[string]string {
	result := make(map[string]string)
	type stackEntry struct {
		indent int
		key    string
	}
	var stack []stackEntry

	currentPrefix := func() string {
		var parts []string
		for _, e := range stack {
			parts = append(parts, e.key)
		}
		return strings.Join(parts, ".")
	}

	valRe := regexp.MustCompile(`^([ \t]*)([A-Za-z0-9_\-\.]+)\s*:\s*(.+)$`)
	mapRe := regexp.MustCompile(`^([ \t]*)([A-Za-z0-9_\-\.]+)\s*:\s*$`)

	for _, line := range strings.Split(content, "\n") {
		stripped := strings.TrimRight(line, " \t\r")
		if stripped == "" || strings.HasPrefix(strings.TrimSpace(stripped), "#") {
			continue
		}

		if m := valRe.FindStringSubmatch(stripped); m != nil {
			indent := countLeadingSpaces(m[1])
			keyPart := m[2]
			rawValue := strings.TrimSpace(m[3])
			if rawValue == "" || rawValue == "|" || rawValue == ">" || rawValue == "|-" || rawValue == ">-" {
				for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
					stack = stack[:len(stack)-1]
				}
				stack = append(stack, stackEntry{indent, keyPart})
				continue
			}
			if idx := strings.Index(rawValue, " #"); idx != -1 {
				rawValue = strings.TrimSpace(rawValue[:idx])
			}
			if len(rawValue) >= 2 &&
				((rawValue[0] == '"' && rawValue[len(rawValue)-1] == '"') ||
					(rawValue[0] == '\'' && rawValue[len(rawValue)-1] == '\'')) {
				rawValue = rawValue[1 : len(rawValue)-1]
			}
			for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
				stack = stack[:len(stack)-1]
			}
			prefix := currentPrefix()
			fullKey := keyPart
			if prefix != "" {
				fullKey = prefix + "." + keyPart
			}
			result[fullKey] = rawValue
			continue
		}
		if m := mapRe.FindStringSubmatch(stripped); m != nil {
			indent := countLeadingSpaces(m[1])
			keyPart := m[2]
			for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, stackEntry{indent, keyPart})
		}
	}
	return result
}

// ParseDotenv parses a .env file.
func ParseDotenv(content string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			continue
		}
		if strings.HasPrefix(stripped, "export ") {
			stripped = strings.TrimSpace(stripped[7:])
		}
		eqIdx := strings.Index(stripped, "=")
		if eqIdx <= 0 {
			continue
		}
		key := strings.TrimSpace(stripped[:eqIdx])
		value := strings.TrimSpace(stripped[eqIdx+1:])
		if len(value) >= 2 &&
			((value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		if key != "" {
			result[key] = value
		}
	}
	return result
}

// ParseProperties parses a Java .properties file.
func ParseProperties(content string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == "" || strings.HasPrefix(stripped, "#") || strings.HasPrefix(stripped, "!") {
			continue
		}
		for _, sep := range []string{"=", ":"} {
			idx := strings.Index(stripped, sep)
			if idx > 0 {
				key := strings.TrimSpace(stripped[:idx])
				value := strings.TrimSpace(stripped[idx+1:])
				if key != "" {
					result[key] = value
				}
				break
			}
		}
	}
	return result
}

// ParseJSONFlat parses a flat JSON object.
func ParseJSONFlat(content string) map[string]string {
	result := make(map[string]string)
	jsonKVRe := regexp.MustCompile(`"([^"]+)"\s*:\s*(?:"([^"]*)"|(-?\d+(?:\.\d+)?))`)
	for _, m := range jsonKVRe.FindAllStringSubmatch(content, -1) {
		key := m[1]
		value := m[2]
		if value == "" {
			value = m[3]
		}
		result[key] = value
	}
	return result
}

func parseContent(content, format string) map[string]string {
	switch format {
	case "properties":
		return ParseProperties(content)
	case "env":
		return ParseDotenv(content)
	case "json":
		return ParseJSONFlat(content)
	default:
		return ParseYAMLFlat(content)
	}
}

// ComputeDiffKeys returns sorted keys that differ between two flat maps.
func ComputeDiffKeys(mapA, mapB map[string]string) []string {
	allKeys := make(map[string]bool)
	for k := range mapA {
		allKeys[k] = true
	}
	for k := range mapB {
		allKeys[k] = true
	}
	var differing []string
	for key := range allKeys {
		if mapA[key] != mapB[key] {
			differing = append(differing, key)
		}
	}
	sort.Strings(differing)
	return differing
}

// EnrichConfigProfiles detects profile pairs and attaches diff_keys metadata.
func EnrichConfigProfiles(entities []types.EntityRecord) []types.EntityRecord {
	type pf struct {
		sourceFile string
		profile    string
		groupKey   string
		format     string
		idx        int
	}
	var profileFiles []pf
	for i := range entities {
		e := &entities[i]
		if e.SourceFile == "" {
			continue
		}
		profile, groupKey, format, ok := detectProfileFile(e.SourceFile)
		if !ok {
			continue
		}
		profileFiles = append(profileFiles, pf{e.SourceFile, profile, groupKey, format, i})
	}
	if len(profileFiles) < 2 {
		return entities
	}
	groups := make(map[string][]pf)
	for _, p := range profileFiles {
		groups[p.groupKey] = append(groups[p.groupKey], p)
	}
	for _, members := range groups {
		if len(members) < 2 {
			continue
		}
		byProfile := make(map[string]pf)
		for _, m := range members {
			byProfile[m.profile] = m
		}
		for _, pair := range comparePairs {
			pfA, okA := byProfile[pair[0]]
			pfB, okB := byProfile[pair[1]]
			if !okA || !okB {
				continue
			}
			contentA := getEntityContent(&entities[pfA.idx])
			contentB := getEntityContent(&entities[pfB.idx])
			if contentA == "" || contentB == "" {
				continue
			}
			mapA := parseContent(contentA, pfA.format)
			mapB := parseContent(contentB, pfB.format)
			diffKeys := ComputeDiffKeys(mapA, mapB)
			var anchor *types.EntityRecord
			if pfA.sourceFile <= pfB.sourceFile {
				anchor = &entities[pfA.idx]
			} else {
				anchor = &entities[pfB.idx]
			}
			if anchor.Metadata == nil {
				anchor.Metadata = make(map[string]interface{})
			}
			anchor.Metadata["diff_keys"] = diffKeys
			anchor.Metadata["profiles_compared"] = []string{pair[0], pair[1]}
			anchor.Metadata["config_profile_enriched"] = true
		}
	}
	return entities
}

func getEntityContent(e *types.EntityRecord) string {
	if e.Metadata != nil {
		for _, key := range []string{"content", "file_content"} {
			if v, ok := e.Metadata[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return e.Content
}
