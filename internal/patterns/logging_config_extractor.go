package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// loggingConfigExtractor detects logging configuration patterns.
// Matches Python logging_config_extractor.py.
type loggingConfigExtractor struct{}

var (
	lcGoSlogTriggerRE   = regexp.MustCompile(`"log/slog"`)
	lcGoLogTriggerRE    = regexp.MustCompile(`"(?:go\.uber\.org/zap|github\.com/sirupsen/logrus|log)"`)
	lcGoSlogLevelRE     = regexp.MustCompile(`slog\.(?:New|SetDefault).*Level\s*:\s*slog\.(\w+Level)`)
	lcPyLoggingTrigger  = regexp.MustCompile(`import\s+logging`)
	lcPyLevelRE         = regexp.MustCompile(`logging\.(?:basicConfig|setLevel)\s*\([^)]*level\s*=\s*logging\.(\w+)`)
	lcNodeWinstonTrigger = regexp.MustCompile(`require\s*\(\s*['"]winston['"]`)
	lcNodePinoTrigger    = regexp.MustCompile(`require\s*\(\s*['"]pino['"]`)
	lcNodeLevelRE        = regexp.MustCompile(`level\s*:\s*['"](\w+)['"]`)
	lcJavaLog4jRE        = regexp.MustCompile(`(?i)<log4j|LogManager\.getLogger|@Slf4j\b`)
	lcXMLRootLevelRE     = regexp.MustCompile(`<root\s+level\s*=\s*["'](\w+)["']`)
)

func (l *loggingConfigExtractor) Category() string { return "logging_config" }

func (l *loggingConfigExtractor) AppliesTo(src string) bool {
	return lcGoSlogTriggerRE.MatchString(src) ||
		lcGoLogTriggerRE.MatchString(src) ||
		lcPyLoggingTrigger.MatchString(src) ||
		lcNodeWinstonTrigger.MatchString(src) ||
		lcNodePinoTrigger.MatchString(src) ||
		lcJavaLog4jRE.MatchString(src)
}

func (l *loggingConfigExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, lib, level string) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"logging_config_"+lib, "SCOPE.Config", "logging_config", language, 1,
			map[string]string{"kind": "logging_config", "library": lib, "level": level}))
	}

	switch language {
	case "go":
		lib := "log"
		if lcGoSlogTriggerRE.MatchString(src) {
			lib = "slog"
			level := "INFO"
			if m := lcGoSlogLevelRE.FindStringSubmatch(src); m != nil {
				level = strings.TrimSuffix(m[1], "Level")
			}
			emit("go:slog", lib, level)
		} else if strings.Contains(src, "go.uber.org/zap") {
			lib = "zap"
			emit("go:zap", lib, "INFO")
		} else if strings.Contains(src, "sirupsen/logrus") {
			lib = "logrus"
			emit("go:logrus", lib, "INFO")
		} else {
			emit("go:log", lib, "INFO")
		}
	case "python":
		level := "WARNING"
		if m := lcPyLevelRE.FindStringSubmatch(src); m != nil {
			level = m[1]
		}
		emit("py:logging", "logging", level)
	case "javascript", "typescript":
		if lcNodeWinstonTrigger.MatchString(src) {
			level := "info"
			if m := lcNodeLevelRE.FindStringSubmatch(src); m != nil {
				level = m[1]
			}
			emit("node:winston", "winston", level)
		}
		if lcNodePinoTrigger.MatchString(src) {
			level := "info"
			if m := lcNodeLevelRE.FindStringSubmatch(src); m != nil {
				level = m[1]
			}
			emit("node:pino", "pino", level)
		}
	default:
		if lcJavaLog4jRE.MatchString(src) {
			level := "INFO"
			if m := lcXMLRootLevelRE.FindStringSubmatch(src); m != nil {
				level = strings.ToUpper(m[1])
			}
			emit("java:log4j", "log4j", level)
		}
	}

	return results
}

func init() {
	Register(&loggingConfigExtractor{})
}
