package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/cajasmota/grafel/internal/daemon/walk"
)

// TestGitHubActionsFileConventionsAreReachable_6946 joins the two halves of
// #6946 in one assertion: the globs the GitHub Actions rule set declares, and
// the repo-relative paths the walker actually emits for a repo laid out the
// way GitHub requires.
//
// Neither half alone catches the bug. matchesFile(".github/workflows/*.yml",
// ".github/workflows/ci.yml") was always true; the walker simply never
// produced that path, because ".github" sat on hardcodedSkipDirs and the
// hard-coded layer runs BEFORE the ignore stack. So every source_pattern in
// github_actions.yaml was dead on a real repo while every unit test around it
// stayed green.
//
// This test fails if `.github` goes back on the skip list, and it fails if the
// globs are rewritten to a shape the walker does not emit — which is the
// "make the rules match somewhere else" non-fix.
func TestGitHubActionsFileConventionsAreReachable_6946(t *testing.T) {
	rulePath := filepath.Join("rules", "cicd", "frameworks", "github_actions.yaml")
	raw, err := os.ReadFile(rulePath)
	if err != nil {
		t.Fatalf("read %s: %v", rulePath, err)
	}
	var rule FrameworkRule
	if err := yaml.Unmarshal(raw, &rule); err != nil {
		t.Fatalf("unmarshal %s: %v", rulePath, err)
	}
	if len(rule.FileConventions) == 0 {
		t.Fatalf("%s declares no file_conventions — this test grades their reachability "+
			"and has nothing to grade", rulePath)
	}

	// A repo laid out the way GitHub requires. One file per declared glob,
	// plus decoys: ordinary YAML that GitHub Actions never reads, at paths a
	// real repo has. They are the specificity half — see the forbidden loop
	// below.
	root := t.TempDir()
	inLayout := []string{
		".github/workflows/ci.yml",
		".github/workflows/release.yaml",
		".github/actions/setup/action.yml",
	}
	decoys := []string{
		"config/app.yml",
		"deploy/values.yaml",
		"src/actions/setup/action.yml",
	}
	for _, rel := range append(append([]string{}, inLayout...), decoys...) {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, []byte("name: ci\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	walked, _, err := walk.WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	// EVERY declared glob is graded, not only the .github/-prefixed ones.
	// Grading the prefixed subset would leave the option-3 non-fix — rewrite
	// the glob to some path the walker already emits, so the rule "fires"
	// while still never seeing a real workflow — silently passing.
	graded, dotGitHub := 0, 0
	for _, fc := range rule.FileConventions {
		if fc.Glob == "" {
			continue
		}
		graded++
		if strings.HasPrefix(fc.Glob, ".github/") {
			dotGitHub++
		}
		cfc := compiledFileConvention{glob: fc.Glob, entityType: fc.EntityType, nameFrom: fc.NameFrom}
		matched := false
		for _, f := range walked {
			if cfc.matchesFile(f) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("#6946: file_convention glob %q matches none of the paths the walker emits "+
				"for a standard GitHub layout (walked=%v) — the rule is unreachable on a real "+
				"repo. If the glob is legitimately new, add a file for it to the layout above.",
				fc.Glob, walked)
		}
	}
	if graded == 0 {
		t.Fatalf("%s declares file_conventions but none carries a glob — the reachability "+
			"claim this test pins was deleted rather than fixed", rulePath)
	}
	// Specificity, the other axis. Reachability alone is satisfied by a glob
	// that matches EVERYTHING: matchesFile falls back to a BASENAME match
	// when a glob carries no "/", so rewriting ".github/workflows/*.yml" to
	// "*.yml" makes every .yml in the repo a GitHub Actions Config — the
	// option-3 non-fix in a spelling that keeps the rules "firing". Grade it
	// by requiring each glob to reject YAML GitHub never reads.
	for _, fc := range rule.FileConventions {
		if fc.Glob == "" {
			continue
		}
		cfc := compiledFileConvention{glob: fc.Glob, entityType: fc.EntityType, nameFrom: fc.NameFrom}
		for _, decoy := range decoys {
			if cfc.matchesFile(decoy) {
				t.Errorf("#6946: file_convention glob %q also matches %q, which GitHub Actions "+
					"never reads. A glob that matches workflow files by accident is not a rule "+
					"that found them.", fc.Glob, decoy)
			}
		}
	}

	if dotGitHub == 0 {
		t.Errorf("%s no longer declares a single .github/ glob. GitHub reads workflows only "+
			"from .github/workflows; a rule set that matches them anywhere else fires on the "+
			"wrong files and still never sees the real ones (#6946)", rulePath)
	}
}
