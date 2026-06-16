package install_test

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/rulesfiles"
	"github.com/cajasmota/grafel/internal/registry"
)

// applyDryRun runs install.Apply in DryRun mode under an isolated HOME and
// returns the Result. DryRun writes nothing but populates Result the same
// way as a real install, so it is a faithful probe of the per-tool
// enablement wiring.
func applyDryRun(t *testing.T, tools []string) *install.Result {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(home, ".grafel"))

	repo := t.TempDir()
	cfg := &registry.GroupConfig{
		Name:  "demo",
		Repos: []registry.Repo{{Slug: "r", Path: repo}},
		Tools: tools,
	}
	res, err := install.Apply(install.Options{
		Group:   "demo",
		Config:  cfg,
		BinPath: "/usr/local/bin/grafel",
		DryRun:  true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return res
}

// TestApply_DefaultEnablement_AllSixRulesFiles is the back-compat
// regression guard at the Apply boundary: with no Tools the rules-file set
// reported is exactly the historical six.
func TestApply_DefaultEnablement_AllSixRulesFiles(t *testing.T) {
	res := applyDryRun(t, nil)

	var repoPath string
	for p := range res.RulesFiles {
		repoPath = p
	}
	if repoPath == "" {
		t.Fatal("no repo recorded in RulesFiles")
	}
	got := append([]string{}, res.RulesFiles[repoPath]...)
	want := append([]string{}, rulesfiles.Targets...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default rules files = %v, want %v", got, want)
	}
}

// TestApply_RestrictedEnablement_OnlySubset proves a restricted Tools list
// writes only that subset's rules files.
func TestApply_RestrictedEnablement_OnlySubset(t *testing.T) {
	res := applyDryRun(t, []string{"cursor", "copilot"})

	var repoPath string
	for p := range res.RulesFiles {
		repoPath = p
	}
	got := append([]string{}, res.RulesFiles[repoPath]...)
	want := []string{".cursorrules", ".github/copilot-instructions.md"}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("restricted rules files = %v, want %v", got, want)
	}
}
