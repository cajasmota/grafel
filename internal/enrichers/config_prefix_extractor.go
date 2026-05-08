package enrichers

// ConfigPrefixExtractor detects static path prefixes from framework config files.
// Port of Python config_prefix_extractor.py (MX-585).

import (
	"path/filepath"
	"regexp"
	"strings"
)

// ConfigPrefixEntry is one detected config prefix from a file.
type ConfigPrefixEntry struct {
	ConfigKey string
	Value     string
	Framework string
}

var gatedConfigBasenames = map[string]bool{
	"application.properties":   true,
	"application.yml":          true,
	"application.yaml":         true,
	"settings.py":              true,
	"routeserviceprovider.php": true,
	"launchsettings.json":      true,
	"program.cs":               true,
	"routes.rb":                true,
	"main.py":                  true,
	"app.js":                   true,
	"application.conf":         true,
}

var (
	propertiesKVRe      = regexp.MustCompile(`(?m)^([a-z][a-z0-9._-]{0,127})\s*=\s*(.{1,256})\s*$`)
	yamlContextPathRe   = regexp.MustCompile(`(?m)^\s*context-path\s*:\s*(.{1,256})\s*$`)
	yamlRootPathRe      = regexp.MustCompile(`(?m)^\s*root-path\s*:\s*(.{1,256})\s*$`)
	yamlKtorRootPathRe  = regexp.MustCompile(`(?m)^\s*rootPath\s*:\s*(.{1,256})\s*$`)
	djangoScriptNameRe  = regexp.MustCompile(`(?m)^\s*FORCE_SCRIPT_NAME\s*=\s*['"]([^'"]{1,256})['"]\s*$`)
	laravelPrefixRe     = regexp.MustCompile(`['"]prefix['"]\s*=>\s*['"]([^'"]{1,256})['"]`)
	aspnetAppURLRe      = regexp.MustCompile(`"applicationUrl"\s*:\s*"([^"]{1,256})"`)
	aspnetUsePathBaseRe = regexp.MustCompile(`app\.UsePathBase\s*\(\s*['"]([^'"]{1,256})['"]\s*\)`)
	railsScopeRe        = regexp.MustCompile(`scope\s+['"]([^'"]{1,256})['"]\s+do\b`)
	fastapiRootPathRe   = regexp.MustCompile(`FastAPI\s*\([^)]{0,256}root_path\s*=\s*['"]([^'"]{1,256})['"]`)
	fastapiRouterPfxRe  = regexp.MustCompile(`APIRouter\s*\([^)]{0,256}prefix\s*=\s*['"]([^'"]{1,256})['"]`)
	expressUseRe        = regexp.MustCompile(`app\.use\s*\(\s*['"]([^'"]{1,256})['"]\s*,`)
	ktorConfRootPathRe  = regexp.MustCompile(`(?s)deployment\s*\{[^}]*rootPath\s*=\s*['"]?([^\s'"}\n]+)['"]?`)
)

// ConfigPrefixAppliesToFile returns true when the file should be processed.
func ConfigPrefixAppliesToFile(filePath string) bool {
	return gatedConfigBasenames[strings.ToLower(filepath.Base(filePath))]
}

// ExtractConfigPrefixes parses a config file and returns detected prefixes.
func ExtractConfigPrefixes(source, filePath string) []ConfigPrefixEntry {
	if source == "" {
		return nil
	}
	base := strings.ToLower(filepath.Base(filePath))
	switch base {
	case "application.properties":
		return extractFromProperties(source)
	case "application.yml", "application.yaml":
		return extractFromYAML(source, "")
	case "settings.py":
		return extractFromSettingsPy(source)
	case "routeserviceprovider.php":
		return extractFromLaravel(source)
	case "launchsettings.json":
		return extractFromLaunchSettings(source)
	case "program.cs":
		return extractFromProgramCS(source)
	case "routes.rb":
		return extractFromRoutesRb(source)
	case "main.py":
		return extractFromMainPy(source)
	case "app.js":
		return extractFromAppJS(source)
	case "application.conf":
		return extractFromApplicationConf(source)
	}
	return nil
}

func cleanYAMLValue(raw string) string {
	val := strings.TrimSpace(raw)
	if idx := strings.Index(val, " #"); idx != -1 {
		val = strings.TrimSpace(val[:idx])
	}
	if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') ||
		(val[0] == '\'' && val[len(val)-1] == '\'')) {
		val = val[1 : len(val)-1]
	}
	return val
}

func extractFromProperties(source string) []ConfigPrefixEntry {
	var found []ConfigPrefixEntry
	for _, m := range propertiesKVRe.FindAllStringSubmatch(source, -1) {
		key := strings.TrimSpace(m[1])
		value := strings.TrimSpace(m[2])
		switch key {
		case "server.servlet.context-path":
			found = append(found, ConfigPrefixEntry{key, value, "spring_boot"})
		case "quarkus.http.root-path":
			found = append(found, ConfigPrefixEntry{key, value, "quarkus"})
		case "micronaut.server.context-path":
			found = append(found, ConfigPrefixEntry{key, value, "micronaut"})
		case "ktor.deployment.rootPath":
			found = append(found, ConfigPrefixEntry{key, value, "ktor"})
		}
	}
	return found
}

func extractFromYAML(source, ctxFramework string) []ConfigPrefixEntry {
	var found []ConfigPrefixEntry
	isQuarkus := strings.Contains(strings.ToLower(source), "quarkus") ||
		strings.Contains(strings.ToLower(ctxFramework), "quarkus")
	isMicronaut := strings.Contains(strings.ToLower(source), "micronaut") ||
		strings.Contains(strings.ToLower(ctxFramework), "micronaut")
	if m := yamlContextPathRe.FindStringSubmatch(source); m != nil {
		val := cleanYAMLValue(m[1])
		if val != "" {
			switch {
			case isQuarkus:
				found = append(found, ConfigPrefixEntry{"quarkus.http.root-path", val, "quarkus"})
			case isMicronaut:
				found = append(found, ConfigPrefixEntry{"micronaut.server.context-path", val, "micronaut"})
			default:
				found = append(found, ConfigPrefixEntry{"server.servlet.context-path", val, "spring_boot"})
			}
		}
	}
	if m := yamlRootPathRe.FindStringSubmatch(source); m != nil {
		val := cleanYAMLValue(m[1])
		if val != "" {
			if isQuarkus {
				found = append(found, ConfigPrefixEntry{"quarkus.http.root-path", val, "quarkus"})
			} else {
				found = append(found, ConfigPrefixEntry{"server.servlet.context-path", val, "spring_boot"})
			}
		}
	}
	if m := yamlKtorRootPathRe.FindStringSubmatch(source); m != nil {
		val := cleanYAMLValue(m[1])
		if val != "" {
			found = append(found, ConfigPrefixEntry{"ktor.deployment.rootPath", val, "ktor"})
		}
	}
	return found
}

func extractFromSettingsPy(source string) []ConfigPrefixEntry {
	if m := djangoScriptNameRe.FindStringSubmatch(source); m != nil {
		return []ConfigPrefixEntry{{"FORCE_SCRIPT_NAME", m[1], "django"}}
	}
	return nil
}

func extractFromLaravel(source string) []ConfigPrefixEntry {
	if m := laravelPrefixRe.FindStringSubmatch(source); m != nil {
		return []ConfigPrefixEntry{{"prefix", m[1], "laravel"}}
	}
	return nil
}

func extractFromLaunchSettings(source string) []ConfigPrefixEntry {
	if m := aspnetAppURLRe.FindStringSubmatch(source); m != nil {
		return []ConfigPrefixEntry{{"applicationUrl", m[1], "aspnet"}}
	}
	return nil
}

func extractFromProgramCS(source string) []ConfigPrefixEntry {
	var found []ConfigPrefixEntry
	for _, m := range aspnetUsePathBaseRe.FindAllStringSubmatch(source, -1) {
		found = append(found, ConfigPrefixEntry{"UsePathBase", m[1], "aspnet"})
	}
	return found
}

func extractFromRoutesRb(source string) []ConfigPrefixEntry {
	var found []ConfigPrefixEntry
	for _, m := range railsScopeRe.FindAllStringSubmatch(source, -1) {
		found = append(found, ConfigPrefixEntry{"scope", m[1], "rails"})
	}
	return found
}

func extractFromMainPy(source string) []ConfigPrefixEntry {
	var found []ConfigPrefixEntry
	if m := fastapiRootPathRe.FindStringSubmatch(source); m != nil {
		found = append(found, ConfigPrefixEntry{"root_path", m[1], "fastapi"})
	}
	for _, m := range fastapiRouterPfxRe.FindAllStringSubmatch(source, -1) {
		found = append(found, ConfigPrefixEntry{"APIRouter.prefix", m[1], "fastapi"})
	}
	return found
}

func extractFromAppJS(source string) []ConfigPrefixEntry {
	var found []ConfigPrefixEntry
	for _, m := range expressUseRe.FindAllStringSubmatch(source, -1) {
		if strings.HasPrefix(m[1], "/") {
			found = append(found, ConfigPrefixEntry{"app.use", m[1], "express"})
		}
	}
	return found
}

func extractFromApplicationConf(source string) []ConfigPrefixEntry {
	if m := ktorConfRootPathRe.FindStringSubmatch(source); m != nil {
		return []ConfigPrefixEntry{{"ktor.deployment.rootPath", strings.TrimSpace(m[1]), "ktor"}}
	}
	return nil
}
