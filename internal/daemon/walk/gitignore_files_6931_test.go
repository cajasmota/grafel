package walk

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// --- #6931 / #6922 -----------------------------------------------------------
//
// WalkRepo applied the ignore stack in the d.IsDir() branch only, so a
// file-level .gitignore pattern (`*.pb.go`, `graph.json`, ...) was matched by
// `git check-ignore` and INDEXED by grafel (#6931). The other two ignore
// sources git reads — `.git/info/exclude` and `core.excludesFile` — were read
// by nothing at all (#6922).
//
// The fixture below enumerates the pattern space rather than hand-picking a
// few cases, and asserts the walked set EXACTLY (set equality, not "contains"),
// so over-exclusion is graded as loudly as under-exclusion (#6902).
//
// VARIED, one axis per row:
//   - pattern form: bare name, rooted `/name`, glob, negation `!`,
//     trailing-slash dir-only
//   - pattern SOURCE: root .gitignore, nested .gitignore, .git/info/exclude,
//     core.excludesFile
//   - entry type for one identical name (`dironly.go` as a file vs as a dir)
//   - depth of the matched path (repo root vs nested) for each of the bare,
//     rooted and glob forms
//   - precedence between adjacent sources, in BOTH directions
//
// HELD CONSTANT: file extension (.go) for every row so the extension filter
// can never be the thing doing the work; no sparse checkout; no .grafelignore;
// a single repo root that IS the git top-level.

// writeFile writes p (relative to root) with trivial content.
func writeIgnoreFixtureFile(t *testing.T, root, p, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", abs, err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// ignoreSourceFixture builds a git repo exercising every ignore pattern form
// across all three git ignore sources, and returns (root, wantWalked).
func ignoreSourceFixture(t *testing.T) (string, []string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	mustGit(t, root, "init", "-q")

	// ---- source 1: root .gitignore -------------------------------------
	writeIgnoreFixtureFile(t, root, ".gitignore", strings.Join([]string{
		"bare_ignored.go",    // bare name — matches at ANY depth
		"/rooted_ignored.go", // rooted — repo root ONLY
		"*.gen.go",           // glob
		"!kept.gen.go",       // negation of the glob above
		"dironly.go/",        // trailing slash — DIRECTORIES only
		"!prec_a.go",         // beats .git/info/exclude (precedence, direction 1)
	}, "\n")+"\n")

	// ---- source 2: nested .gitignore -----------------------------------
	writeIgnoreFixtureFile(t, root, "pkg/sub/.gitignore", strings.Join([]string{
		"!bare_ignored.go", // child un-ignores what the parent ignored
		"nested_only.go",   // and ignores something the parent does not
	}, "\n")+"\n")

	// ---- source 3: .git/info/exclude (#6922) ---------------------------
	writeIgnoreFixtureFile(t, root, ".git/info/exclude", strings.Join([]string{
		"excluded_by_info.go",
		"prec_a.go",  // loses to root .gitignore's "!prec_a.go"
		"!prec_b.go", // beats core.excludesFile (precedence, direction 2)
	}, "\n")+"\n")

	// ---- source 4: core.excludesFile (#6922) ---------------------------
	// Set at the LOCAL config level so the test never reads or writes the
	// developer's real global git config.
	globalIgnore := filepath.Join(t.TempDir(), "global_ignore")
	if err := os.WriteFile(globalIgnore, []byte("excluded_by_global.go\nprec_b.go\n"), 0o644); err != nil {
		t.Fatalf("write global ignore: %v", err)
	}
	mustGit(t, root, "config", "core.excludesFile", globalIgnore)

	// ---- the tree ------------------------------------------------------
	for _, p := range []string{
		// must be SKIPPED
		"bare_ignored.go",
		"pkg/bare_ignored.go",
		"rooted_ignored.go",
		"a.gen.go",
		"pkg/b.gen.go",
		"pkg/dironly.go/inside.go",
		"pkg/sub/nested_only.go",
		"excluded_by_info.go",
		"excluded_by_global.go",
		// must be WALKED
		"keep.go",
		"pkg/keep.go",
		"kept.gen.go",
		"dironly.go",
		"nested_only.go",
		"prec_a.go",
		"prec_b.go",
		"pkg/rooted_ignored.go",
		"pkg/sub/bare_ignored.go",
	} {
		writeIgnoreFixtureFile(t, root, p, "package p\n")
	}

	want := []string{
		".gitignore",
		"dironly.go",
		"keep.go",
		"kept.gen.go",
		"nested_only.go",
		"pkg/keep.go",
		"pkg/rooted_ignored.go",
		"pkg/sub/.gitignore",
		"pkg/sub/bare_ignored.go",
		"prec_a.go",
		"prec_b.go",
	}
	sort.Strings(want)
	return root, want
}

func TestWalkRepo_AppliesIgnoreStackToFiles_6931(t *testing.T) {
	root, want := ignoreSourceFixture(t)

	got, _, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	sort.Strings(got)

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("walked set mismatch\n got: %v\nwant: %v\nmissing (over-excluded): %v\nextra (under-excluded): %v",
			got, want, diffSet(want, got), diffSet(got, want))
	}
}

// TestWalkRepo_IgnoredFilesAreReported pins that a file dropped by the ignore
// stack is REPORTED as a SkipEntry rather than vanishing silently (#6338's
// class of defect), and that the reported rule names the source.
func TestWalkRepo_IgnoredFilesAreReported_6931(t *testing.T) {
	root, _ := ignoreSourceFixture(t)

	_, skipped, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	rules := map[string]string{}
	for _, s := range skipped {
		rel, _ := filepath.Rel(root, s.AbsPath)
		rules[filepath.ToSlash(rel)] = s.Rule
	}
	for _, want := range []struct{ path, source string }{
		{"bare_ignored.go", ".gitignore"},
		{"excluded_by_info.go", "info/exclude"},
		{"excluded_by_global.go", "core.excludesFile"},
		{"pkg/sub/nested_only.go", ".gitignore"},
	} {
		rule, ok := rules[want.path]
		if !ok {
			t.Errorf("%s was dropped but not reported as a SkipEntry", want.path)
			continue
		}
		if !strings.Contains(rule, want.source) {
			t.Errorf("%s reported with rule %q, want it to name %q", want.path, rule, want.source)
		}
	}
}

// TestIgnoreFile_DirOnlyPatternNeverMatchesAFile is the unit-level control for
// the one rule that only becomes observable once the stack is consulted for
// files: `foo/` matches a directory named foo and NOT a file named foo.
func TestIgnoreFile_DirOnlyPatternNeverMatchesAFile_6931(t *testing.T) {
	ig, err := parseIgnoreReader("", ".gitignore", strings.NewReader("dironly.go/\nboth.go\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if skip, _ := ig.MatchPath("dironly.go", true); !skip {
		t.Error("dironly.go/ should match a DIRECTORY named dironly.go")
	}
	if skip, _ := ig.MatchPath("dironly.go", false); skip {
		t.Error("dironly.go/ must NOT match a FILE named dironly.go")
	}
	// A pattern without the trailing slash matches both kinds.
	if skip, _ := ig.MatchPath("both.go", true); !skip {
		t.Error("both.go should match a directory")
	}
	if skip, _ := ig.MatchPath("both.go", false); !skip {
		t.Error("both.go should match a file")
	}
}

func diffSet(a, b []string) []string {
	in := map[string]bool{}
	for _, s := range b {
		in[s] = true
	}
	var out []string
	for _, s := range a {
		if !in[s] {
			out = append(out, s)
		}
	}
	return out
}

// TestInfoExcludeIsSharedWithLinkedWorktrees_6922 pins the git-common-dir
// resolution: a linked worktree has its OWN git dir but shares the main
// clone's .git/info/exclude, and git honours the shared one. Resolving via
// `rev-parse --git-dir` instead would read <main>/.git/worktrees/<name>/info/
// exclude, which does not exist — so the whole layer would silently vanish for
// every worktree lane.
func TestInfoExcludeIsSharedWithLinkedWorktrees_6922(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	main := t.TempDir()
	mustGit(t, main, "init", "-q")
	mustGit(t, main, "config", "user.email", "t@example.com")
	mustGit(t, main, "config", "user.name", "t")
	writeIgnoreFixtureFile(t, main, "seed.go", "package p\n")
	mustGit(t, main, "add", "-A")
	mustGit(t, main, "commit", "-qm", "seed")
	writeIgnoreFixtureFile(t, main, ".git/info/exclude", "excluded_by_info.go\n")

	linked := filepath.Join(t.TempDir(), "wt")
	mustGit(t, main, "worktree", "add", "-q", "-b", "lane", linked)

	writeIgnoreFixtureFile(t, linked, "excluded_by_info.go", "package p\n")
	writeIgnoreFixtureFile(t, linked, "keep.go", "package p\n")

	got, _, err := WalkRepo(linked, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	in := map[string]bool{}
	for _, f := range got {
		in[f] = true
	}
	if in["excluded_by_info.go"] {
		t.Error("linked worktree indexed a file excluded by the main clone's .git/info/exclude")
	}
	if !in["keep.go"] || !in["seed.go"] {
		t.Errorf("over-excluded in a linked worktree: got %v", got)
	}
}

// TestExcludeSourcesAreNotAppliedBelowTopLevel_6922 pins the deliberate scope
// limit documented on gitExcludeIgnoreFiles: patterns in .git/info/exclude are
// anchored at the git top-level, so a walk rooted at a SUBDIRECTORY does not
// apply them. Rewriting them for the child root is what would be needed, and
// the one existing rewrite (rewriteInheritedIgnoreLine, used for .grafelignore)
// drops anchoring — see the doc comment. This test exists so that inheriting
// them later is a deliberate change with a failing test, not a silent drift.
func TestExcludeSourcesAreNotAppliedBelowTopLevel_6922(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	top := t.TempDir()
	mustGit(t, top, "init", "-q")
	writeIgnoreFixtureFile(t, top, ".git/info/exclude", "excluded_by_info.go\n")
	writeIgnoreFixtureFile(t, top, "pkg/excluded_by_info.go", "package p\n")
	writeIgnoreFixtureFile(t, top, "pkg/keep.go", "package p\n")

	got, _, err := WalkRepo(filepath.Join(top, "pkg"), nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	sort.Strings(got)
	want := []string{"excluded_by_info.go", "keep.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("sub-root walk = %v, want %v", got, want)
	}
	// ...and the same tree walked from the top-level DOES apply it, so the
	// difference above is the root choice and nothing else.
	fromTop, _, err := WalkRepo(top, nil)
	if err != nil {
		t.Fatalf("WalkRepo(top): %v", err)
	}
	for _, f := range fromTop {
		if f == "pkg/excluded_by_info.go" {
			t.Error("top-level walk failed to apply .git/info/exclude")
		}
	}
}

// TestGlobalExcludesPath_ConfigThenXDGFallback_6922 grades the resolution order
// for core.excludesFile: the git config value wins, and when it is unset the
// path falls back to git's own default of $XDG_CONFIG_HOME/git/ignore. A
// hardcoded ~/.config/git/ignore would pass the second row and fail the first.
func TestGlobalExcludesPath_ConfigThenXDGFallback_6922(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	mustGit(t, repo, "init", "-q")

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	if got, want := globalExcludesPath(repo), filepath.Join(xdg, "git", "ignore"); got != want {
		t.Errorf("unset core.excludesFile: got %q, want the XDG fallback %q", got, want)
	}

	configured := filepath.Join(t.TempDir(), "mine")
	mustGit(t, repo, "config", "core.excludesFile", configured)
	if got := globalExcludesPath(repo); got != configured {
		t.Errorf("configured core.excludesFile: got %q, want %q", got, configured)
	}
}

// TestExpandTilde_6922 pins the "~/" expansion git performs on
// core.excludesFile. Without it a configured `~/.gitignore_global` is opened
// as a literal directory named "~" and the whole layer silently no-ops.
func TestExpandTilde_6922(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got, want := expandTilde("~/ig"), filepath.Join(home, "ig"); got != want {
		t.Errorf("expandTilde(~/ig) = %q, want %q", got, want)
	}
	if got := expandTilde("/abs/ig"); got != "/abs/ig" {
		t.Errorf("expandTilde must leave an absolute path alone, got %q", got)
	}
	if got := expandTilde("rel~ative/ig"); got != "rel~ative/ig" {
		t.Errorf("expandTilde must only expand a LEADING ~, got %q", got)
	}
}
