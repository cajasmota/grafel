package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// configDetector detects configuration files and config loading patterns.
// Matches Python config_detector.py.
type configDetector struct{}

var (
	configFileRE       = regexp.MustCompile(`(?i)\.(env|yaml|yml|toml|ini|json|conf|config|properties)$`)
	configLoadPyRE     = regexp.MustCompile(`(?:os\.(?:environ|getenv)|dotenv\.load|config\.from_object|app\.config\[)`)
	configLoadGoRE     = regexp.MustCompile(`(?:os\.Getenv|viper\.Get|godotenv\.Load|envconfig\.Process)`)
	configLoadNodeRE   = regexp.MustCompile(`(?:process\.env\.|dotenv\.config|config\.get\()`)
	configLoadJavaRE   = regexp.MustCompile(`(?:@Value\s*\(|@ConfigurationProperties|Environment\.getProperty)`)
	configLoadDotNetRE = regexp.MustCompile(`(?:Configuration\["|IConfiguration|appSettings\.json)`)
	configEnvVarRE     = regexp.MustCompile(`(?:os\.(?:Getenv|getenv|environ\.get)|process\.env\.|System\.getenv)\s*\(\s*["']([^"']+)["']`)
)

func (c *configDetector) Category() string { return "config" }

func (c *configDetector) AppliesTo(src string) bool {
	return configLoadPyRE.MatchString(src) ||
		configLoadGoRE.MatchString(src) ||
		configLoadNodeRE.MatchString(src) ||
		configLoadJavaRE.MatchString(src) ||
		configLoadDotNetRE.MatchString(src)
}

func (c *configDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	// Detect config file type from path
	if configFileRE.MatchString(filePath) {
		ext := ""
		if m := configFileRE.FindStringSubmatch(filePath); m != nil {
			ext = strings.ToLower(m[1])
		}
		key := "config_file:" + ext
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"config_file_"+ext, "SCOPE.Config", "config_file", language, 1,
				map[string]string{"kind": "config", "config_type": ext, "source": "file"}))
		}
	}

	// Detect env var reads
	for idx, m := range configEnvVarRE.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		key := "env_var:" + varName
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("config_env_%s", varName),
			"SCOPE.Config", "env_var", language,
			lineOf(src, m[0]),
			map[string]string{
				"kind":     "config",
				"var_name": varName,
				"source":   "env",
				"index":    fmt.Sprintf("%d", idx),
			}))
	}

	// Config loading patterns
	for _, re := range []*regexp.Regexp{configLoadPyRE, configLoadGoRE, configLoadNodeRE, configLoadJavaRE, configLoadDotNetRE} {
		if re.MatchString(src) {
			key := "config_load:" + re.String()[:20]
			if !seen[key] {
				seen[key] = true
				results = append(results, makeEntity(filePath,
					"config_loader", "SCOPE.Config", "config_loader", language, 1,
					map[string]string{"kind": "config", "source": "loader"}))
			}
		}
	}

	return results
}

func init() {
	Register(&configDetector{})
}
