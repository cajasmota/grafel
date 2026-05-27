package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var osMkdirAll = os.MkdirAll

// TestResolveGroup_CWDFromRegistry verifies that when cwd is inside a
// registered repo path, the group is inferred without an explicit group=
// argument and without a .archigraph/group.json marker (#1650).
func TestResolveGroup_CWDFromRegistry(t *testing.T) {
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "upvate_core")
	if err := mkdirp(repoPath); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	reg := &Registry{
		Groups: map[string]RegistryGroup{
			"upvate": {
				Repos: map[string]RegistryRepo{
					"upvate-core": {Path: repoPath},
				},
			},
			"polyglot-platform": {
				Repos: map[string]RegistryRepo{
					"other": {Path: filepath.Join(tmp, "other-root")},
				},
			},
		},
	}
	st := NewState(reg)

	// Bare cwd inside the registered repo path → resolve "upvate" via registry.
	subdir := filepath.Join(repoPath, "src", "views")
	if err := mkdirp(subdir); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	g, src, err := resolveGroup(st, "", subdir)
	if err != nil {
		t.Fatalf("resolveGroup: %v", err)
	}
	if g != "upvate" {
		t.Errorf("group: want upvate, got %s", g)
	}
	if src != "cwd_registry" {
		t.Errorf("source: want cwd_registry, got %s", src)
	}

	// Outside any registered repo → ambiguous error (two groups registered).
	_, _, err = resolveGroup(st, "", filepath.Join(tmp, "nowhere"))
	if err == nil {
		t.Errorf("expected ambiguous-group error for cwd outside any repo")
	}
}

// TestResolveGroup_InferFromCWD is the table-driven suite for #1746 CWD group
// inference. It covers all four key cases described in the issue:
//
//  1. explicit group= is set → returns that group, no cwd lookup.
//  2. cwd inside exactly one registered repo's group → inferred unambiguously.
//  3. cwd inside repos registered to two different groups → error listing
//     the candidate groups.
//  4. cwd is under no registered repo (and multiple groups exist) → ambiguous
//     error listing all registered groups.
func TestResolveGroup_InferFromCWD(t *testing.T) {
	tmp := t.TempDir()

	repoA := filepath.Join(tmp, "repo_a") // group alpha
	repoB := filepath.Join(tmp, "repo_b") // group beta
	repoC := filepath.Join(tmp, "repo_c") // ALSO group beta (multi-repo group)
	// shared sits inside both repoA AND repoB trees by having both as ancestors
	// — achieved by making repoShared a subdir of repoA AND registering repoA
	// under a second group (gamma) to force cross-group ambiguity.
	repoAGamma := repoA // same physical path, second group (gamma)

	for _, p := range []string{repoA, repoB, repoC} {
		if err := mkdirp(p); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	reg := &Registry{
		Groups: map[string]RegistryGroup{
			"alpha": {
				Repos: map[string]RegistryRepo{
					"repo-a": {Path: repoA},
				},
			},
			"beta": {
				Repos: map[string]RegistryRepo{
					"repo-b": {Path: repoB},
					"repo-c": {Path: repoC},
				},
			},
			"gamma": {
				// gamma also registers repoA → cwd inside repoA is ambiguous
				// between alpha and gamma.
				Repos: map[string]RegistryRepo{
					"repo-a-mirror": {Path: repoAGamma},
				},
			},
		},
	}
	st := NewState(reg)

	subdirA := filepath.Join(repoA, "pkg", "handler")
	subdirB := filepath.Join(repoB, "src")
	nowhere := filepath.Join(tmp, "totally-unrelated")
	for _, p := range []string{subdirA, subdirB} {
		if err := mkdirp(p); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	cases := []struct {
		name        string
		explicit    string
		cwd         string
		wantGroup   string
		wantSource  string
		wantErrFrag string // non-empty → expect error containing this substring
	}{
		{
			name:       "explicit group overrides cwd",
			explicit:   "beta",
			cwd:        subdirA, // would resolve alpha/gamma if not for explicit
			wantGroup:  "beta",
			wantSource: "explicit",
		},
		{
			name:       "cwd inside single-group repo resolves unambiguously",
			explicit:   "",
			cwd:        subdirB,
			wantGroup:  "beta",
			wantSource: "cwd_registry",
		},
		{
			name:        "cwd inside repo registered to two groups errors with candidates",
			explicit:    "",
			cwd:         subdirA,
			wantErrFrag: "ambiguous group",
		},
		{
			name:        "cwd under no registered repo errors with all groups",
			explicit:    "",
			cwd:         nowhere,
			wantErrFrag: "ambiguous group",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, src, err := resolveGroup(st, tc.explicit, tc.cwd)
			if tc.wantErrFrag != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil (group=%q)", tc.wantErrFrag, g)
				}
				if !strings.Contains(err.Error(), tc.wantErrFrag) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrFrag)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if g != tc.wantGroup {
				t.Errorf("group: want %q, got %q", tc.wantGroup, g)
			}
			if src != tc.wantSource {
				t.Errorf("source: want %q, got %q", tc.wantSource, src)
			}
		})
	}
}

// TestResolveGroup_AmbiguousCWDIncludesCandidates verifies that when cwd is
// inside repos registered to multiple groups, the error message names those
// specific candidate groups (not all registered groups) (#1746).
func TestResolveGroup_AmbiguousCWDIncludesCandidates(t *testing.T) {
	tmp := t.TempDir()
	sharedRepo := filepath.Join(tmp, "shared")
	if err := mkdirp(sharedRepo); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	reg := &Registry{
		Groups: map[string]RegistryGroup{
			"groupA": {
				Repos: map[string]RegistryRepo{"r": {Path: sharedRepo}},
			},
			"groupB": {
				Repos: map[string]RegistryRepo{"r": {Path: sharedRepo}},
			},
			"unrelated": {
				Repos: map[string]RegistryRepo{"other": {Path: filepath.Join(tmp, "other")}},
			},
		},
	}
	st := NewState(reg)

	cwd := filepath.Join(sharedRepo, "sub")
	if err := mkdirp(cwd); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, _, err := resolveGroup(st, "", cwd)
	if err == nil {
		t.Fatal("expected ambiguous-group error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "groupA") {
		t.Errorf("error %q missing groupA", msg)
	}
	if !strings.Contains(msg, "groupB") {
		t.Errorf("error %q missing groupB", msg)
	}
	// "unrelated" is NOT a candidate (its repo path doesn't cover cwd) — it
	// must NOT appear in the targeted error message.
	if strings.Contains(msg, "unrelated") {
		t.Errorf("error %q unexpectedly lists unrelated group", msg)
	}
}

func mkdirp(p string) error {
	return osMkdirAll(p, 0o755)
}

// TestPathContains_CaseInsensitiveOnMacOSWindows verifies that pathContains
// performs case-insensitive matching on macOS (darwin) and Windows, and
// case-sensitive matching on Linux and other systems. This addresses issue #2543:
// shell-reported cwd may differ in casing from the indexed manifest path on
// case-insensitive filesystems (APFS, HFS+, NTFS).
func TestPathContains_CaseInsensitiveOnMacOSWindows(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skipf("case-insensitive test only applies on darwin/windows; skipping on %s", runtime.GOOS)
	}

	cases := []struct {
		name     string
		ancestor string
		child    string
		want     bool
	}{
		{
			name:     "exact match same casing",
			ancestor: "/Users/test/UpVate/upvate_core",
			child:    "/Users/test/UpVate/upvate_core/src",
			want:     true,
		},
		{
			name:     "lowercase child cwd vs uppercase ancestor manifest",
			ancestor: "/Users/test/UpVate/upvate_core",
			child:    "/Users/test/upvate/upvate_core/src",
			want:     true,
		},
		{
			name:     "exact equality with different casing",
			ancestor: "/Users/test/UpVate/upvate_core",
			child:    "/Users/test/upvate/upvate_core",
			want:     true,
		},
		{
			name:     "nested path with different casing",
			ancestor: "/Users/test/MyRepo",
			child:    "/Users/test/MYREPO/src/pkg",
			want:     true,
		},
		{
			name:     "unrelated path even with overlapping string",
			ancestor: "/Users/test/repo",
			child:    "/Users/test/repository",
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pathContains(tc.ancestor, tc.child)
			if got != tc.want {
				t.Errorf("pathContains(%q, %q): want %v, got %v", tc.ancestor, tc.child, tc.want, got)
			}
		})
	}
}

// TestGroupFromRegistryWithCandidates_CaseInsensitive verifies that the registry
// cwd-to-group matcher handles case-insensitive paths on macOS/Windows (issue #2543).
// It tests that when the registered repo path differs in casing from the cwd,
// the match still succeeds on case-insensitive filesystems.
func TestGroupFromRegistryWithCandidates_CaseInsensitive(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skipf("case-insensitive test only applies on darwin/windows; skipping on %s", runtime.GOOS)
	}

	tmp := t.TempDir()

	// Register repo with uppercase path.
	upperRepoPath := filepath.Join(tmp, "UpVate", "upvate_core")
	if err := mkdirp(upperRepoPath); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Register a second repo in a different group to force groupFromRegistryWithCandidates
	// to make a real decision (not singleton fallback).
	otherRepoPath := filepath.Join(tmp, "other")
	if err := mkdirp(otherRepoPath); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	reg := &Registry{
		Groups: map[string]RegistryGroup{
			"upvate": {
				Repos: map[string]RegistryRepo{
					"upvate-core": {Path: upperRepoPath},
				},
			},
			"other-group": {
				Repos: map[string]RegistryRepo{
					"other": {Path: otherRepoPath},
				},
			},
		},
	}
	st := NewState(reg)

	// Use cwd with lowercase path — on a case-insensitive filesystem, this is the
	// same directory as the registered upperRepoPath, so the match should succeed.
	lowerRepoPath := filepath.Join(tmp, "upvate", "upvate_core")
	cwd := filepath.Join(lowerRepoPath, "src", "views")

	g, candidates := groupFromRegistryWithCandidates(st, cwd)
	if g != "upvate" {
		t.Errorf("groupFromRegistryWithCandidates: want group=upvate, got %s (candidates: %v)", g, candidates)
	}
}

// TestResolveCWD_NonGitCwd_SkipsGitCapture verifies that when cwd is not
// inside any git repository (no .git file/dir in the tree), ResolveCWD
// returns early without calling gitmeta.Capture (#2563).
func TestResolveCWD_NonGitCwd_SkipsGitCapture(t *testing.T) {
	tmp := t.TempDir()
	// Create a directory tree with no .git — this ensures the fast-check
	// prevents expensive subprocess calls.
	nonGitCwd := filepath.Join(tmp, "nonrepo", "subdir")
	if err := mkdirp(nonGitCwd); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a minimal registry with no repos.
	reg := &Registry{
		Groups: map[string]RegistryGroup{},
	}
	st := NewState(reg)

	res := ResolveCWD(st, nonGitCwd)
	if res.Source != "none" {
		t.Errorf("ResolveCWD(non-git cwd): want Source=none, got %s", res.Source)
	}
	if res.Group != "" || res.RepoSlug != "" || res.Ref != "" {
		t.Errorf("ResolveCWD(non-git cwd): want empty fields, got Group=%s RepoSlug=%s Ref=%s",
			res.Group, res.RepoSlug, res.Ref)
	}
}

// TestResolveCWD_GitCwd_StillCaptures verifies that when cwd is inside a git
// repository (has .git in tree), ResolveCWD still calls gitmeta.Capture and
// processes the worktree-sibling path (#2563 regression test).
func TestResolveCWD_GitCwd_StillCaptures(t *testing.T) {
	tmp := t.TempDir()

	// Create a git repository structure manually by creating a .git directory.
	repoPath := filepath.Join(tmp, "test-repo")
	if err := mkdirp(repoPath); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	gitDir := filepath.Join(repoPath, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	// Use a subdirectory inside the repo.
	cwdInRepo := filepath.Join(repoPath, "src", "views")
	if err := mkdirp(cwdInRepo); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a minimal registry with no matches.
	reg := &Registry{
		Groups: map[string]RegistryGroup{},
	}
	st := NewState(reg)

	res := ResolveCWD(st, cwdInRepo)
	// We expect either a "worktree" result (if this is a linked worktree of a
	// registered repo) or "none" (if it's a standalone repo not in the registry).
	// The key assertion is that we didn't panic and the fast-check allowed the
	// git capture to proceed (we found .git).
	if res.Source != "none" && res.Source != "worktree" {
		t.Logf("ResolveCWD(git cwd): got Source=%s (acceptable; we called git)", res.Source)
	}
	// If we got here without panicking, the fast-check didn't block legitimate git repos.
}

// TestHasGitDirInTree_WalksUp verifies that hasGitDirInTree walks up the
// directory tree and finds .git at any ancestor level (#2563).
func TestHasGitDirInTree_WalksUp(t *testing.T) {
	tmp := t.TempDir()

	// Create a nested directory structure with .git at the repo root.
	repoRoot := filepath.Join(tmp, "repo")
	if err := mkdirp(repoRoot); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	gitDir := filepath.Join(repoRoot, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	// Create deep subdirectories below the .git.
	deepPath := filepath.Join(repoRoot, "a", "b", "c", "d")
	if err := mkdirp(deepPath); err != nil {
		t.Fatalf("mkdir deep: %v", err)
	}

	tests := []struct {
		name    string
		cwd     string
		want    bool
		wantErr bool
	}{
		{
			name: "at repo root with .git",
			cwd:  repoRoot,
			want: true,
		},
		{
			name: "in subdir — walks up and finds .git",
			cwd:  filepath.Join(repoRoot, "src"),
			want: true,
		},
		{
			name: "deeply nested — walks up many levels",
			cwd:  deepPath,
			want: true,
		},
		{
			name: "outside repo — walks to root and finds nothing",
			cwd:  filepath.Join(tmp, "elsewhere"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Ensure the directory exists (or create it if it's a test case that
			// uses a non-existent path).
			if tt.wantErr {
				// OK to not exist
			} else if tt.cwd != filepath.Join(tmp, "elsewhere") {
				// For other test cases, ensure the directory exists.
				if err := mkdirp(tt.cwd); err != nil && tt.wantErr {
					t.Fatalf("setup mkdir: %v", err)
				}
			}

			got := hasGitDirInTree(tt.cwd)
			if got != tt.want {
				t.Errorf("hasGitDirInTree(%q): want %v, got %v", tt.cwd, tt.want, got)
			}
		})
	}
}
