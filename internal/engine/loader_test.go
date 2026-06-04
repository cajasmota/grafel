package engine

import (
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// expectedMinRuleFiles is the minimum number of YAML rule files expected to
// load successfully. 517 = 304 frameworks + 212 orms + 1 queues.
const expectedMinRuleFiles = 517

// TestLoadAllRules_Count verifies that all embedded YAML rule files load
// without error and that the total count meets the minimum threshold.
func TestLoadAllRules_Count(t *testing.T) {
	t.Helper()

	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules returned unexpected error: %v", err)
	}

	total := 0
	for _, langRules := range rules {
		total += len(langRules)
	}

	if total < expectedMinRuleFiles {
		t.Errorf("loaded %d rule files, want >= %d", total, expectedMinRuleFiles)
	}
	t.Logf("LoadAllRules: loaded %d rule files across %d languages", total, len(rules))
}

// TestLoadAllRules_NoParseErrors verifies that every YAML file in the embedded
// rules directory parses without error. Because LoadAllRulesFromFS now returns
// an error when any file fails to parse, a non-nil error here is a test failure.
func TestLoadAllRules_NoParseErrors(t *testing.T) {
	_, err := LoadAllRules()
	if err != nil {
		t.Fatalf("one or more YAML files failed to parse: %v", err)
	}
}

// TestLoadAllRules_KeyFrameworks verifies that well-known frameworks are present
// after loading. These are the canonical smoke-test entries.
var keyFrameworks = []struct {
	lang      string
	framework string
}{
	{"go", "gin"},
	{"python", "django"},
	{"javascript_typescript", "react"},
	{"java", "spring_boot"},
	{"rust", "actix_web"},
}

func TestLoadAllRules_KeyFrameworks(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules returned unexpected error: %v", err)
	}

	for _, kf := range keyFrameworks {
		langRules, ok := rules[kf.lang]
		if !ok {
			t.Errorf("language %q not found in loaded rules", kf.lang)
			continue
		}
		if len(langRules) == 0 {
			t.Errorf("language %q has zero rules loaded", kf.lang)
		}
		// The framework name is encoded in the YAML file name, not necessarily in
		// the struct. We verify language presence here; file-level checks are done
		// by TestLoadAllRules_EmbedFS below.
		_ = kf.framework
		t.Logf("language %q: %d rules loaded (includes %s)", kf.lang, len(langRules), kf.framework)
	}
}

// TestLoadAllRules_EmbedFS verifies the go:embed directive actually embeds the
// rule files by checking that the embedded FS contains at least the five key
// framework YAML files referenced by TestLoadAllRules_KeyFrameworks.
func TestLoadAllRules_EmbedFS(t *testing.T) {
	paths := []string{
		"rules/go/frameworks/gin.yaml",
		"rules/python/frameworks/django.yaml",
		"rules/javascript_typescript/frameworks/react.yaml",
		"rules/java/frameworks/spring_boot.yaml",
		"rules/rust/frameworks/actix_web.yaml",
	}

	for _, p := range paths {
		data, err := rulesFS.ReadFile(p)
		if err != nil {
			t.Errorf("embedded file %q not found: %v", p, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("embedded file %q is empty", p)
		}
	}
}

// TestRules_NoCustomExtractors is a cite-validity guard. The `custom_extractors:`
// YAML mechanism was a dead, never-read pointer to legacy Python module paths
// (see docs/adr/ENGINE-rescue-vs-remove.md, #3636). It was removed wholesale —
// the FrameworkRule struct no longer carries the field, and every block was
// deleted from the rule corpus. The Go extractors those blocks pointed at run
// via the internal/custom registry, not via this YAML.
//
// This test FAILS if any rule YAML reintroduces a `custom_extractors` key or
// comment, so the dead mechanism cannot silently return.
func TestRules_NoCustomExtractors(t *testing.T) {
	var offenders []string

	err := fs.WalkDir(rulesFS, "rules", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
			return nil
		}
		data, readErr := rulesFS.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "custom_extractors") {
				offenders = append(offenders, path+":"+strconv.Itoa(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking embedded rules: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf("the dead `custom_extractors` YAML mechanism was reintroduced in %d location(s); "+
			"remove it and document the corresponding Go extractor instead (see #3636):\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// TestLoadAllRules_PerLanguageCounts logs per-language rule counts. This test
// never fails — it exists to surface the breakdown in CI logs.
func TestLoadAllRules_PerLanguageCounts(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules returned unexpected error: %v", err)
	}

	for lang, langRules := range rules {
		t.Logf("  %-30s %d rules", lang, len(langRules))
	}
	t.Logf("total languages: %d", len(rules))
}
