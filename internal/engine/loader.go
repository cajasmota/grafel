package engine

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
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

// LoadAllRulesReport is LoadAllRules plus a RuleLoadReport describing exactly
// which embedded rule files were seen and which of them failed to load.
//
// Use this when you need to assert that nothing was silently dropped; the
// plain LoadAllRules keeps its tolerant behaviour for normal callers.
func LoadAllRulesReport() (map[string][]FrameworkRule, RuleLoadReport, error) {
	return LoadAllRulesFromFSReport(rulesFS, "rules")
}

// RuleLoadFailure records one rule file that was found in the tree but did not
// make it into the engine.
type RuleLoadFailure struct {
	// Path is the file that was lost, relative to the FS root.
	Path string
	// Stage is "read" or "parse".
	Stage string
	// Err is the underlying failure.
	Err error
}

func (f RuleLoadFailure) String() string {
	return fmt.Sprintf("%s %s: %v", f.Stage, f.Path, f.Err)
}

// RuleLoadReport is the inventory of a single rule-loading pass.
//
// Candidates lists every rule-shaped YAML file found in the tree (that is,
// <lang>/<subdir>/<file>.yaml for a supported subdir) and Loaded lists the
// subset that parsed successfully. len(Candidates) != len(Loaded) means rules
// were dropped; Failures says which and why.
type RuleLoadReport struct {
	Candidates []string
	Loaded     []string
	Failures   []RuleLoadFailure
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
	rules, _, err := LoadAllRulesFromFSReport(fsys, rootDir)
	return rules, err
}

// LoadAllRulesFromFSReport is the implementation behind LoadAllRulesFromFS. It
// additionally returns a RuleLoadReport so callers can tell a healthy load from
// one that quietly dropped rule files.
//
// Runtime tolerance is unchanged: a malformed rule file is still skipped rather
// than fatal, and the returned error stays nil. What changed (#6341) is that
// the skip is no longer invisible — every failure is recorded in the report and
// logged, instead of being collected and thrown away.
func LoadAllRulesFromFSReport(fsys fs.FS, rootDir string) (map[string][]FrameworkRule, RuleLoadReport, error) {
	result := make(map[string][]FrameworkRule)
	var report RuleLoadReport

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

		// From here on this file is a rule file we are committed to loading, so
		// it counts towards the inventory whether or not it survives.
		report.Candidates = append(report.Candidates, path)

		data, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			report.Failures = append(report.Failures,
				RuleLoadFailure{Path: path, Stage: "read", Err: readErr})
			return nil
		}

		var rule FrameworkRule
		if unmarshalErr := yaml.Unmarshal(data, &rule); unmarshalErr != nil {
			report.Failures = append(report.Failures,
				RuleLoadFailure{Path: path, Stage: "parse", Err: unmarshalErr})
			return nil
		}

		result[lang] = append(result[lang], rule)
		report.Loaded = append(report.Loaded, path)
		return nil
	})
	if err != nil {
		return nil, report, fmt.Errorf("walking rules directory: %w", err)
	}

	// Load failures stay non-fatal — skipping a bad file and continuing is the
	// long-standing behaviour and changing it is a separate decision. They are
	// no longer silent, though: previously they were collected and discarded
	// (`_ = loadErrors`), so a malformed rule file removed its rules from the
	// engine with no error, no exit code and no log line (#6341, cf. #6330).
	for _, f := range report.Failures {
		log.Printf("engine: rule file skipped, its rules are NOT loaded: %s", f)
	}
	if len(report.Failures) > 0 {
		log.Printf("engine: %d of %d rule files failed to load and were skipped",
			len(report.Failures), len(report.Candidates))
	}

	return result, report, nil
}
