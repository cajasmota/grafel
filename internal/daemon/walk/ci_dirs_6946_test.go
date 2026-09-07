package walk

import (
	"strings"
	"testing"
)

// TestWalkRepo_CIDirsAreWalked_6946 grades BOTH directions of the #6946
// change: the CI-definition dirs that came off hardcodedSkipDirs must now be
// walked, and the tool-agent dirs that stayed on it must still be skipped.
//
// The skip mattered because hardcodedSkip runs BEFORE the ignore stack
// (walker.go), so a `.github` on the list is not a default a repo can undo —
// it is an unconditional erasure. Every GitHub Actions rule in
// internal/engine/rules/cicd/frameworks/github_actions.yaml was therefore
// unreachable on any repo that stores its workflows where GitHub requires
// them. See TestGitHubActionsFileConventionsAreReachable_6946 in
// internal/engine for the other half of that claim.
func TestWalkRepo_CIDirsAreWalked_6946(t *testing.T) {
	root := t.TempDir()

	// Must be walked (#6946).
	wantWalked := []string{
		".github/workflows/ci.yml",
		".github/workflows/release.yaml",
		".github/actions/setup/action.yml",
		".github/dependabot.yml",
		".gitlab/ci-templates/build.yml",
		".circleci/config.yml",
		"src/main.go",
	}
	// Must still be skipped: tool-agent config that is not source, and the
	// build/VCS dirs. If one of these ever leaks the un-skip was too wide.
	wantSkipped := []string{
		".claude/settings.json",
		".cursor/rules/foo.json",
		".windsurf/skills/coding.md",
		".husky/pre-commit",
		".devcontainer/devcontainer.json",
		"node_modules/pkg/index.js",
	}
	for _, p := range append(append([]string{}, wantWalked...), wantSkipped...) {
		mkfile(t, root, p, "x")
	}

	files, _, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	got := make(map[string]bool, len(files))
	for _, f := range files {
		got[f] = true
	}

	for _, want := range wantWalked {
		if !got[want] {
			t.Errorf("#6946: %q was not walked; files=%v", want, files)
		}
	}
	for _, notWant := range wantSkipped {
		if got[notWant] {
			t.Errorf("%q leaked into the walk results — the #6946 un-skip is too wide", notWant)
		}
	}

	// The exported predicate the watcher and the RSS scanner consult must
	// agree with the walker, or a repo would be walked and then never
	// re-indexed on change (the walker/watcher divergence class of #6934).
	for _, base := range []string{".github", ".gitlab", ".circleci"} {
		if IsHardcodedSkip(base) {
			t.Errorf("IsHardcodedSkip(%q) = true, want false (#6946) — the watcher would still "+
				"drop the event for a directory the walker now indexes", base)
		}
	}
	for _, base := range []string{".claude", ".cursor", ".windsurf", ".husky", ".devcontainer", "node_modules"} {
		if !IsHardcodedSkip(base) {
			t.Errorf("IsHardcodedSkip(%q) = false, want true — the #6946 un-skip removed more "+
				"than the three CI dirs", base)
		}
	}
}

// TestWalkRepo_CIDirsStillHonourIgnoreStack_6946 pins the reason the skip was
// wrong rather than merely unhelpful: the hard-coded layer runs before the
// ignore stack and so cannot be overridden, while an un-skipped directory is
// governed by the repo's own .gitignore/.grafelignore like anything else.
//
// Without this a "fix" that hard-coded `.github` as always-walked would pass
// the test above and still take the choice away from the user.
func TestWalkRepo_CIDirsStillHonourIgnoreStack_6946(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, ".github/workflows/ci.yml", "name: ci")
	mkfile(t, root, ".circleci/config.yml", "version: 2.1")
	mkfile(t, root, "src/main.go", "package main")
	writeIgnoreFile(t, root, ".grafelignore", ".github/\n")

	files, _, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	for _, f := range files {
		if strings.HasPrefix(f, ".github/") {
			t.Errorf("%q was walked despite a .grafelignore entry — an un-skipped dir must "+
				"remain governed by the ignore stack (#6946)", f)
		}
	}
	got := make(map[string]bool, len(files))
	for _, f := range files {
		got[f] = true
	}
	// Negative control: the ignore file named only .github, so .circleci must
	// still be walked. Without this row a walker that skipped every CI dir
	// again would pass.
	if !got[".circleci/config.yml"] {
		t.Errorf(".circleci/config.yml was not walked; the .grafelignore named .github only; files=%v", files)
	}
	if !got["src/main.go"] {
		t.Errorf("src/main.go was not walked; files=%v", files)
	}

}
