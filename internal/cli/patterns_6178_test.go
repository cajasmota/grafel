// Regression test for issue #6178 round 4 (L1): resolvePatternsDir
// (internal/cli/patterns.go) was a fourth independent derivation of the
// "-patterns" directory layout — filepath.Join(registry.HomeDir(),
// "groups", groupName+"-patterns") — agreeing with links.PatternsDir today
// only by construction, not by sharing code with it. Not a live bug (it
// already resolved GRAFEL_HOME correctly), but the drift precondition: a
// future change to PatternsDir's layout would silently leave this CLI
// writer behind. Now delegates to links.PatternsDir directly.
package cli

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/links"
	"github.com/cajasmota/grafel/internal/registry"
)

func TestResolvePatternsDir_DerivesFromSharedPatternsDir_6178(t *testing.T) {
	home := withSandboxHome(t)
	repo := filepath.Join(home, "repos", "alpha")
	makeRepo(t, repo)

	const group = "g6178-cli-patterns"
	cfg := &registry.GroupConfig{Name: group}
	cfg.Repos = []registry.Repo{{Slug: "alpha", Path: repo, Stack: registry.StackList{"go"}}}
	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup(group, cfgPath); err != nil {
		t.Fatal(err)
	}

	got, err := resolvePatternsDir(group)
	if err != nil {
		t.Fatalf("resolvePatternsDir: %v", err)
	}

	// Compare against links.PatternsDir directly, not a hand-reconstructed
	// expectation — this is what proves resolvePatternsDir actually SHARES
	// the derivation rather than merely agreeing with it today. If
	// resolvePatternsDir ever reverts to hand-rolling the join (or
	// links.PatternsDir's own layout changes without this call site
	// following), the two diverge and this fails.
	want, err := links.PatternsDir("", group)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("resolvePatternsDir(%q) = %q, want %q (links.PatternsDir's output) — the CLI patterns writer must derive the SAME path as every reader, not independently agree with it", group, got, want)
	}
}
