package enrichers

// ConfigConsumerExtractor detects config key consumption patterns in source code.
// Port of Python config_consumer_extractor.py (MX-600).

import (
	"fmt"
	"regexp"
	"strings"
)

// ConfigKeyMatch represents a detected config key consumption.
type ConfigKeyMatch struct {
	KeyName  string
	Pattern  string
	FilePath string
}

var configConsumerPatterns = []struct {
	Re    *regexp.Regexp
	Label string
}{
	{regexp.MustCompile(`@Value\s*\(\s*["']\$\{([A-Za-z0-9._\-]{1,256})(?::[^}]{0,64})?\}["']\s*\)`), "spring_value"},
	{regexp.MustCompile(`process\.env\.([A-Z][A-Z0-9_]{0,127})\b`), "node_process_env"},
	{regexp.MustCompile(`process\.env\s*\[\s*["']([A-Za-z][A-Za-z0-9_]{0,127})["']\s*\]`), "node_process_env"},
	{regexp.MustCompile(`os\.getenv\s*\(\s*["']([A-Za-z][A-Za-z0-9_]{0,127})["']`), "python_os_getenv"},
	{regexp.MustCompile(`os\.environ\s*\[\s*["']([A-Za-z][A-Za-z0-9_]{0,127})["']\s*\]`), "python_os_environ"},
	{regexp.MustCompile(`os\.environ\.get\s*\(\s*["']([A-Za-z][A-Za-z0-9_]{0,127})["']`), "python_os_environ"},
	{regexp.MustCompile(`viper\.Get(?:String|Bool|Int|Int64|Float64|Duration|StringSlice|StringMap|SizeInBytes)?\s*\(\s*["']([A-Za-z][A-Za-z0-9._\-]{0,127})["']\s*\)`), "go_viper"},
	{regexp.MustCompile(`@ConfigProperty\s*\([^)]{0,128}name\s*=\s*["']([A-Za-z][A-Za-z0-9._\-]{0,127})["']`), "microprofile_config_property"},
	{regexp.MustCompile(`(?:_?[Cc]onfig(?:uration)?)\s*\[\s*["']([A-Za-z][A-Za-z0-9:._\-]{0,127})["']\s*\]`), "csharp_configuration"},
	{regexp.MustCompile(`(?:_?[Cc]onfig(?:uration)?)\s*\.GetValue\s*<[^>]{1,64}>\s*\(\s*["']([A-Za-z][A-Za-z0-9:._\-]{0,127})["']`), "csharp_configuration"},
}

// ExtractConfigKeys scans source for config key consumption patterns.
func ExtractConfigKeys(source, filePath string) []ConfigKeyMatch {
	if source == "" {
		return nil
	}
	seen := make(map[string]bool)
	var results []ConfigKeyMatch
	for _, pat := range configConsumerPatterns {
		for _, m := range pat.Re.FindAllStringSubmatch(source, -1) {
			if len(m) < 2 {
				continue
			}
			keyName := strings.TrimSpace(m[1])
			if keyName == "" || seen[keyName] {
				continue
			}
			seen[keyName] = true
			results = append(results, ConfigKeyMatch{KeyName: keyName, Pattern: pat.Label, FilePath: filePath})
		}
	}
	return results
}

// ConfigKeyRef builds the canonical entity ref for a config key.
func ConfigKeyRef(filePath, keyName string) string {
	return fmt.Sprintf("scope:pattern:config_key:%s:%s", filePath, keyName)
}

// ConfigKeyEntityName converts a config key name to an entity name.
func ConfigKeyEntityName(keyName string) string {
	s := strings.ReplaceAll(keyName, ".", "_")
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ":", "_")
	return "config_key_" + s
}
