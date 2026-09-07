// Real-git grading for sparse-checkout support (#6964).
//
// The two defects this file exists to pin BOTH failed toward INCLUSION, which
// is why they survived: a sparse repo indexed in full produces a superset, so
// nothing errors and nothing goes missing.
//
//  1. ProbeRepo read `git config --local`, but git >= 2.32 writes
//     core.sparseCheckout into the WORKTREE scope — every sparse repo made by
//     a modern git probed as "not sparse".
//  2. IsPathIncluded stripped the leading slash off cone mode's first pattern
//     `/*`, saw a bare `*`, and returned "included" for every path before the
//     `/src/` restriction was ever reached.
//
// Two rules follow from that, and this file is built around them:
//
//   - Nothing here hand-writes a config value or a pattern file. Every fixture
//     drives `git sparse-checkout set` and lets git write whatever it writes —
//     a hand-written config reproduces defect 1 by construction.
//   - The ground truth is git's own per-path verdict (`git ls-files -v`, where
//     the 'S' tag is skip-worktree, i.e. NOT checked out), so the EXCLUSION
//     direction is asserted for every tracked path. A test that only checks
//     included-is-included passes against both defects at once.
//
// Measured on git 2.50.1 (Apple Git-155), macOS/APFS. Every claim about which
// scope git writes is re-derived at runtime by rgSparseScopes, so a different
// git version reports what it actually does instead of being assumed.
package gitmeta_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/gitmeta"
)

// rgRun runs git in dir and fails the test on error.
func rgRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := rgTry(dir, args...)
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return out
}

// rgTry runs git in dir and returns combined output plus the error.
func rgTry(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// A developer's own git config must not decide what this fixture measures.
	// A path that does not exist, not /dev/null: git reads a missing config
	// file as empty on every platform, while /dev/null has no Windows spelling.
	absent := filepath.Join(dir, ".absent-gitconfig-6964")
	cmd.Env = append(cmd.Environ(),
		"GIT_CONFIG_GLOBAL="+absent,
		"GIT_CONFIG_SYSTEM="+absent,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	b, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(b)), err
}

// rgVersion reports the git under test, so a failure names the version it was
// measured on — the whole defect is a version/scope mismatch.
func rgVersion(t *testing.T, dir string) string {
	t.Helper()
	return rgRun(t, dir, "--version")
}

// rgFixture builds a committed repo with a tree that has content at the root,
// in several top-level directories, and nested two deep — enough for a cone
// set, a nested cone set and a non-cone pattern to each produce a DIFFERENT
// answer.
func rgFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// No -b: the branch name is irrelevant here and -b needs git >= 2.28,
	// which would narrow the versions this file can grade on.
	rgRun(t, dir, "init", "-q", ".")
	for _, f := range rgFixtureFiles {
		writeFileUnder(t, dir, f)
	}
	rgRun(t, dir, "add", "-A")
	rgRun(t, dir, "commit", "-qm", "init")
	return dir
}

// writeFileUnder creates rel (with its parent directories) under dir.
func writeFileUnder(t *testing.T, dir, rel string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

var rgFixtureFiles = []string{
	"root.txt",
	"src/a.go",
	"src/sub/b.go",
	"services/payments/h.go",
	"services/payments/deep/d.go",
	"services/orders/h.go",
	"cmd/m.go",
}

// rgGitVerdicts returns git's own per-path answer for every tracked path:
// true = checked out (included), false = skip-worktree (excluded).
//
// `git ls-files -v` tags each index entry; the 'S' tag is skip-worktree, which
// is exactly what sparse-checkout sets on a path outside the pattern set.
func rgGitVerdicts(t *testing.T, dir string) map[string]bool {
	t.Helper()
	out := rgRun(t, dir, "ls-files", "-v")
	verdicts := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 3 || line[1] != ' ' {
			continue
		}
		verdicts[line[2:]] = line[0] != 'S'
	}
	return verdicts
}

// rgSparseScopes reports, for this git, which config scopes hold
// core.sparseCheckout after `git sparse-checkout set` ran. It is diagnostic:
// it turns "git 2.32 moved the value" from an assumption in a comment into a
// measurement printed by any failing run.
func rgSparseScopes(t *testing.T, dir string) string {
	t.Helper()
	var parts []string
	for _, scope := range []string{"--local", "--worktree", ""} {
		args := []string{"config"}
		if scope != "" {
			args = append(args, scope)
		}
		args = append(args, "--get", "core.sparseCheckout")
		out, err := rgTry(dir, args...)
		name := scope
		if name == "" {
			name = "(no scope flag)"
		}
		if err != nil {
			parts = append(parts, name+"=<unset/error>")
			continue
		}
		parts = append(parts, name+"="+out)
	}
	return strings.Join(parts, " ")
}

// assertMatchesGit compares grafel's sparse verdict against git's for every
// tracked path, and refuses to pass unless BOTH directions were exercised —
// an all-included comparison is exactly what both defects produce.
func assertMatchesGit(t *testing.T, dir string, si gitmeta.SparseInfo) {
	t.Helper()
	verdicts := rgGitVerdicts(t, dir)
	if len(verdicts) == 0 {
		t.Fatal("fixture premise broken: git reported no tracked paths")
	}
	paths := make([]string, 0, len(verdicts))
	for p := range verdicts {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var wantIn, wantOut int
	for _, p := range paths {
		want := verdicts[p]
		got := gitmeta.IsPathIncluded(si, p)
		if want {
			wantIn++
		} else {
			wantOut++
		}
		if got != want {
			t.Errorf("IsPathIncluded(%q) = %v, git says checked-out=%v\n  patterns=%v cone=%v\n  scopes: %s\n  %s",
				p, got, want, si.Patterns, si.ConeMode, rgSparseScopes(t, dir), rgVersion(t, dir))
		}
	}
	// Without this the whole comparison could be satisfied by "include
	// everything", which is precisely the broken behaviour.
	if wantOut == 0 {
		t.Fatalf("fixture is not grading the EXCLUSION direction: git checked out all %d tracked paths (patterns=%v)", wantIn, si.Patterns)
	}
	if wantIn == 0 {
		t.Fatalf("fixture is not grading the INCLUSION direction: git excluded all %d tracked paths (patterns=%v)", wantOut, si.Patterns)
	}
	t.Logf("%s | %d included / %d excluded | cone=%v patterns=%v | scopes: %s",
		rgVersion(t, dir), wantIn, wantOut, si.ConeMode, si.Patterns, rgSparseScopes(t, dir))
}

// probeAfterSparseSet drives a real `git sparse-checkout set` and returns the
// SparseInfo grafel derives from the repo git just produced. It fails — rather
// than skipping — when the probe does not see the sparse checkout, because
// that state is the defect, not an unsupported environment.
func probeAfterSparseSet(t *testing.T, dir string, setArgs ...string) gitmeta.SparseInfo {
	t.Helper()
	rgRun(t, dir, append([]string{"sparse-checkout", "set"}, setArgs...)...)

	si := gitmeta.ProbeRepo(dir)
	if !si.IsSparse {
		t.Fatalf("ProbeRepo says NOT sparse for a repo `git sparse-checkout set %v` just made sparse — defect 1 (#6964).\n  scopes: %s\n  %s",
			setArgs, rgSparseScopes(t, dir), rgVersion(t, dir))
	}
	if len(si.Patterns) == 0 {
		t.Fatalf("ProbeRepo read no patterns from the file git wrote (scopes: %s)", rgSparseScopes(t, dir))
	}
	return si
}

// TestProbeRepo_SeesASparseCheckoutMadeByRealGit pins defect 1 on its own
// axis: the probe must read the scope this git writes, whichever that is.
func TestProbeRepo_SeesASparseCheckoutMadeByRealGit(t *testing.T) {
	dir := rgFixture(t)
	before := gitmeta.ProbeRepo(dir)
	if before.IsSparse {
		t.Fatal("fixture premise broken: a fresh full checkout probed as sparse")
	}

	si := probeAfterSparseSet(t, dir, "src")
	if !si.ConeMode {
		t.Errorf("`sparse-checkout set src` is cone mode by default on %s, but ConeMode=false (scopes: %s)", rgVersion(t, dir), rgSparseScopes(t, dir))
	}

	// Diagnostic, not an assertion: record which scope THIS git used. On git
	// >= 2.32 --local is empty and the value lives in the worktree scope; on
	// older git it is in --local. `--get` covers both, which is the fix.
	t.Logf("%s | scopes after `sparse-checkout set src`: %s", rgVersion(t, dir), rgSparseScopes(t, dir))
}

// TestProbeRepo_DisableIsSeenAsNotSparse grades the other direction of the
// probe: reading a wider set of scopes must not make a repo look sparse
// forever after it has been un-sparsed.
func TestProbeRepo_DisableIsSeenAsNotSparse(t *testing.T) {
	dir := rgFixture(t)
	if si := probeAfterSparseSet(t, dir, "src"); !si.IsSparse {
		t.Fatal("unreachable: probeAfterSparseSet already asserts this")
	}
	rgRun(t, dir, "sparse-checkout", "disable")
	if si := gitmeta.ProbeRepo(dir); si.IsSparse {
		t.Errorf("ProbeRepo still reports sparse after `sparse-checkout disable` (scopes: %s)", rgSparseScopes(t, dir))
	}
}

// TestSparse_ConeMatchesGit — cone mode, one directory. This is the exact
// shape defect 2 turned into match-everything: git writes `/*`, `!/*/`,
// `/src/`, and the old code returned true on the first line.
func TestSparse_ConeMatchesGit(t *testing.T) {
	dir := rgFixture(t)
	si := probeAfterSparseSet(t, dir, "src")
	if !si.ConeMode {
		t.Fatalf("expected cone mode, got patterns=%v", si.Patterns)
	}
	assertMatchesGit(t, dir, si)
}

// TestSparse_NestedConeMatchesGit — cone mode, a nested directory. A separate
// axis from the single-directory case: git writes the intermediate
// `!/src/*/` line, so a matcher that mishandles the second-level negation
// passes the flat case and fails here.
func TestSparse_NestedConeMatchesGit(t *testing.T) {
	dir := rgFixture(t)
	si := probeAfterSparseSet(t, dir, "services/payments")
	if !si.ConeMode {
		t.Fatalf("expected cone mode, got patterns=%v", si.Patterns)
	}
	assertMatchesGit(t, dir, si)
}

// TestSparse_NonConeMatchesGit — non-cone is a SEPARATE axis, not a variant
// of the cone one: git writes the pattern verbatim rather than the
// `/*` + `!/*/` scaffolding, so neither fixture stands for the other.
func TestSparse_NonConeMatchesGit(t *testing.T) {
	dir := rgFixture(t)
	si := probeAfterSparseSet(t, dir, "--no-cone", "/services/payments/")
	if si.ConeMode {
		t.Fatalf("expected non-cone mode, got patterns=%v", si.Patterns)
	}
	assertMatchesGit(t, dir, si)
}

// TestSparse_NonConeUnanchoredMatchesGit — a non-cone pattern with no leading
// slash. gitignore anchors any pattern containing an inner slash, so
// "services/payments" is root-anchored while a bare name would float; git's
// own verdict decides.
func TestSparse_NonConeUnanchoredMatchesGit(t *testing.T) {
	dir := rgFixture(t)
	si := probeAfterSparseSet(t, dir, "--no-cone", "services/payments", "src/sub")
	assertMatchesGit(t, dir, si)
}

// TestSparse_NonConeDoubleStarMatchesGit — a `**` segment, which matches zero
// or more path components and is the one glob construct a per-component
// matcher gets wrong by default. Non-cone only: git refuses glob characters in
// a cone-mode directory argument.
func TestSparse_NonConeDoubleStarMatchesGit(t *testing.T) {
	dir := rgFixture(t)
	// The matched directory is THREE levels down, so "**" has to swallow two
	// components. With a one-level target, path.Match("**", "src") answers
	// true on its own and a matcher with no "**" support looks correct.
	si := probeAfterSparseSet(t, dir, "--no-cone", "/**/deep/")
	if !gitmeta.IsPathIncluded(si, "services/payments/deep/d.go") {
		t.Errorf("services/payments/deep/d.go must be INCLUDED by %v", si.Patterns)
	}
	if gitmeta.IsPathIncluded(si, "services/payments/h.go") {
		t.Errorf("services/payments/h.go must be EXCLUDED by %v", si.Patterns)
	}
	assertMatchesGit(t, dir, si)
}

// TestSparse_NonConeNegationMatchesGit — negation in a NON-cone file, where a
// later include takes back an earlier exclude. The old code returned false the
// moment any negation matched, ignoring pattern order entirely.
func TestSparse_NonConeNegationMatchesGit(t *testing.T) {
	dir := rgFixture(t)
	si := probeAfterSparseSet(t, dir, "--no-cone", "/*", "!/services/", "!/cmd/")
	if si.ConeMode {
		t.Fatalf("expected non-cone mode, got patterns=%v", si.Patterns)
	}
	assertMatchesGit(t, dir, si)
}

// TestSparse_ConeRootFilesStayIncluded states the single most surprising cone
// rule as its own claim, so it cannot be lost inside a bulk comparison:
// `/*` + `!/*/` keeps root-level FILES and drops root-level DIRECTORIES.
// A matcher that prunes a path once an ancestor directory is excluded would
// still match git on the bulk comparison for `set src`, but gets this wrong.
func TestSparse_ConeRootFilesStayIncluded(t *testing.T) {
	dir := rgFixture(t)
	si := probeAfterSparseSet(t, dir, "src")

	if !gitmeta.IsPathIncluded(si, "root.txt") {
		t.Errorf("root.txt must be INCLUDED by cone patterns %v (git checks it out)", si.Patterns)
	}
	if gitmeta.IsPathIncluded(si, "cmd/m.go") {
		t.Errorf("cmd/m.go must be EXCLUDED by cone patterns %v — this is the direction both #6964 defects fail in", si.Patterns)
	}
	if !gitmeta.IsPathIncluded(si, "src/sub/b.go") {
		t.Errorf("src/sub/b.go must be INCLUDED by cone patterns %v (a cone directory is recursive)", si.Patterns)
	}
}

// TestSparse_ReInclusionUnderAnExcludedDirectory pins a rule that gitignore
// and sparse-checkout do NOT share: gitignore cannot re-include under an
// excluded directory, sparse-checkout can. Measured against git, so it is the
// tool's answer and not a reading of the docs.
func TestSparse_ReInclusionUnderAnExcludedDirectory(t *testing.T) {
	dir := rgFixture(t)
	si := probeAfterSparseSet(t, dir, "--no-cone", "/*", "!/*/", "/services/payments/")

	verdicts := rgGitVerdicts(t, dir)
	if !verdicts["services/payments/h.go"] {
		t.Skipf("this git does not re-include under an excluded directory (%s); nothing to pin", rgVersion(t, dir))
	}
	if !gitmeta.IsPathIncluded(si, "services/payments/h.go") {
		t.Errorf("services/payments/h.go: git checks it out, grafel excludes it (patterns=%v)", si.Patterns)
	}
	if gitmeta.IsPathIncluded(si, "services/orders/h.go") {
		t.Errorf("services/orders/h.go must stay EXCLUDED (patterns=%v)", si.Patterns)
	}
	assertMatchesGit(t, dir, si)
}

// TestSparse_BooleanSpelling — git accepts "1"/"yes"/"on" for a boolean, and
// the probe compared the raw string to "true". This is the permissive
// direction's mirror: a repo that IS sparse being read as full.
func TestSparse_BooleanSpelling(t *testing.T) {
	for _, v := range []string{"true", "1", "yes", "on"} {
		t.Run(v, func(t *testing.T) {
			dir := rgFixture(t)
			// The pattern file still comes from git; only the flag spelling
			// is under test here.
			rgRun(t, dir, "sparse-checkout", "set", "--no-cone", "/src/")
			rgClearWorktreeFlag(t, dir)
			rgRun(t, dir, "config", "--local", "core.sparseCheckout", v)
			if si := gitmeta.ProbeRepo(dir); !si.IsSparse {
				t.Errorf("core.sparseCheckout=%q read as not sparse (scopes: %s)", v, rgSparseScopes(t, dir))
			}
		})
	}
	t.Run("false", func(t *testing.T) {
		dir := rgFixture(t)
		rgRun(t, dir, "sparse-checkout", "set", "--no-cone", "/src/")
		rgClearWorktreeFlag(t, dir)
		rgRun(t, dir, "config", "--local", "core.sparseCheckout", "0")
		if rgAnySparseOnDisk(t, dir) {
			t.Fatalf("fixture premise broken: git still resolves core.sparseCheckout=true (scopes: %s)", rgSparseScopes(t, dir))
		}
		if si := gitmeta.ProbeRepo(dir); si.IsSparse {
			t.Errorf("ProbeRepo reports sparse for core.sparseCheckout=0 (scopes: %s)", rgSparseScopes(t, dir))
		}
	})
}

// rgClearWorktreeFlag removes core.sparseCheckout from the WORKTREE scope, so
// a value written into --local is the one git resolves. Scope precedence is
// system < global < local < worktree; without this the spelling under test is
// outranked by the "true" `sparse-checkout set` wrote, and the subtest passes
// no matter what the probe does.
func rgClearWorktreeFlag(t *testing.T, dir string) {
	t.Helper()
	if out, err := rgTry(dir, "config", "--worktree", "--unset", "core.sparseCheckout"); err != nil {
		// git < 2.32 never wrote it there; nothing to clear.
		t.Logf("no worktree-scoped core.sparseCheckout to clear: %v (%s)", err, out)
	}
	if out, _ := rgTry(dir, "config", "--worktree", "--get", "core.sparseCheckout"); out != "" {
		t.Fatalf("worktree scope still holds core.sparseCheckout=%q", out)
	}
}

// rgAnySparseOnDisk asks git itself whether sparse-checkout is in effect,
// using the same resolution order the probe now uses.
func rgAnySparseOnDisk(t *testing.T, dir string) bool {
	t.Helper()
	out, err := rgTry(dir, "config", "--bool", "--get", "core.sparseCheckout")
	return err == nil && out == "true"
}
