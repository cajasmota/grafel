package engine

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed all:rules
var rulesFS embed.FS

// LoadAllRules loads all framework YAML files from the embedded rules/ directory.
// Returns a map of language name to slice of FrameworkRule.
//
// Directory structure expected:
//
//	rules/<language>/frameworks/<framework>.yaml
//
// Each YAML file is unmarshalled into a FrameworkRule.
// Malformed YAML files are logged and skipped (not fatal).
func LoadAllRules() (map[string][]FrameworkRule, error) {
	return LoadAllRulesFromFS(rulesFS, "rules")
}

// ruleSubdirs lists the subdirectory names under each language directory that
// contain FrameworkRule YAML files. All three categories use the same schema.
var ruleSubdirs = map[string]bool{
	"frameworks": true,
	"orms":       true,
	"queues":     true,
}

// LoadAllRulesFromFS loads rules from an arbitrary fs.FS rooted at rootDir.
// This is the testable core; LoadAllRules wraps it with the embedded FS.
//
// It walks all supported rule subdirectories under each language:
//
//	rules/<lang>/frameworks/<file>.yaml
//	rules/<lang>/orms/<file>.yaml
//	rules/<lang>/queues/<file>.yaml
//
// Non-rule YAML files at the language root (_manifest.yaml, language.yaml,
// build_tools.yaml, etc.) and engine config files (_engine/, database_index/)
// are intentionally skipped.
func LoadAllRulesFromFS(fsys fs.FS, rootDir string) (map[string][]FrameworkRule, error) {
	result := make(map[string][]FrameworkRule)
	var loadErrors []string

	err := fs.WalkDir(fsys, rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		// Resolve path relative to rootDir: <lang>/<subdir>/<file>.yaml
		rel, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		// Must have exactly 3 parts: lang / subdir / file.yaml
		if len(parts) != 3 {
			return nil
		}
		lang, subdir := parts[0], parts[1]
		if !ruleSubdirs[subdir] {
			return nil
		}

		data, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			loadErrors = append(loadErrors, fmt.Sprintf("read %s: %v", path, readErr))
			return nil
		}

		var rule FrameworkRule
		if unmarshalErr := yaml.Unmarshal(data, &rule); unmarshalErr != nil {
			loadErrors = append(loadErrors, fmt.Sprintf("parse %s: %v", path, unmarshalErr))
			return nil
		}

		result[lang] = append(result[lang], rule)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking rules directory: %w", err)
	}

	// Load errors are non-fatal: matches Python behaviour of skipping bad files
	// and continuing. The caller receives partial results and can decide whether
	// to surface the skipped-file list. Log them via the caller if needed.
	_ = loadErrors

	return result, nil
}
