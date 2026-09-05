package repowalk_test

import (
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/repowalk"
)

// internalRoot returns <repo>/internal, derived from this file's own location
// at <repo>/internal/repowalk/repowalk_test.go.
//
// The lower-bound walk is rooted at internal/ rather than at the repository
// root on purpose: internal/ contains no `.claude`, no `.git` and no
// `node_modules`, so the walk below needs no exclusion list of its own beyond
// the one literal it hard-codes. A lower bound that used SkippedDir to decide
// where to descend could not see a SkippedDir that skips too much — it would
// prune the very directories it is supposed to prove are inspected, find
// nothing missing, and pass. That is #6834's defect, inside the test for it.
func internalRoot(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(here))
}

// sourceDirNames returns the base name of every directory under internal/ that
// directly contains at least one non-test .go file, along with one example
// path per name for the failure message.
//
// `testdata` is excluded by a hard-coded literal, not by SkippedDir: fixtures
// there are deliberate, some of them deliberately invalid, and they are not
// source this repository's guards are meant to inspect.
func sourceDirNames(t *testing.T) map[string]string {
	t.Helper()
	root := internalRoot(t)
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		dir := filepath.Dir(path)
		base := filepath.Base(dir)
		if _, seen := out[base]; !seen {
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				rel = path
			}
			out[base] = filepath.ToSlash(rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// TestSkippedDir_NeverHidesRepositorySource is the LOWER BOUND: it fails if the
// exclusion list grows to cover a directory the guards are supposed to inspect.
//
// Every other test of an exclusion list in this repository grades the recall
// direction — "is the foreign tree skipped?" — and a recall-shaped suite is
// blind to over-firing. Widening this switch with real package names silently
// narrows five guards at once: they then report a clean tree they never looked
// at, which is the most direct cause of #6834's whole defect class.
func TestSkippedDir_NeverHidesRepositorySource(t *testing.T) {
	dirs := sourceDirNames(t)

	// Layer 2/3 of the vacuity check: a count floor alone catches only "read
	// anything". These anchors pin that the walk read the RIGHT tree — an
	// inverted or misrooted filter scans plenty of files and clears a floor.
	for _, want := range []string{
		"engine", "graph", "extractors", "atomicfile",
		"registry", "entkinds", "relkinds", "types", "repowalk",
	} {
		if _, ok := dirs[want]; !ok {
			t.Fatalf("walk of internal/ found no non-test Go source in a directory named %q; "+
				"the walk is not binding the repository, so anything it reports is meaningless", want)
		}
	}
	if len(dirs) < 40 {
		t.Fatalf("walk of internal/ found only %d directories holding non-test Go source; "+
			"this repository has far more, so the walk is not binding the tree", len(dirs))
	}

	var hidden []string
	for name, example := range dirs {
		if repowalk.SkippedDir(name) {
			hidden = append(hidden, name+" (e.g. internal/"+example+")")
		}
	}
	sort.Strings(hidden)
	if len(hidden) > 0 {
		t.Errorf("SkippedDir excludes %d directories that hold this repository's own "+
			"non-test Go source:\n  %s\n\n"+
			"Every guard built on SkippedDir walks past them and reports a clean tree it "+
			"never read. An exclusion list may only name trees that are foreign to this "+
			"repository (.git, vendored deps, fixtures, build output) — never a package "+
			"the guards exist to inspect.",
			len(hidden), strings.Join(hidden, "\n  "))
	}
}

// skipped is the exact set SkippedDir is expected to name. Kept here rather
// than read back out of the production switch: a test that derives its
// expectation from the code under test asserts nothing.
var skipped = []string{".git", ".claude", "node_modules", "vendor", "testdata", "dist", "build"}

// TestSkippedDir_SkipsEveryForeignTree is the recall direction: each foreign
// tree is still excluded. Without this, deleting the switch body passes the
// lower bound above trivially.
func TestSkippedDir_SkipsEveryForeignTree(t *testing.T) {
	for _, name := range skipped {
		if !repowalk.SkippedDir(name) {
			t.Errorf("SkippedDir(%q) = false; this tree is foreign to the repository and "+
				"walking it parses source that is not ours", name)
		}
	}
}

// TestSkippedDir_MatchesTheWholeNameOnly enumerates the space around each
// skipped name rather than hand-picking attacks, and requires that only the
// exact name is skipped.
//
// This is the over-firing direction at the string level. A `.claude` case
// relaxed to strings.Contains(name, "claude") — the mutant that was ALIVE on
// the whole registry package — takes `.claude-backup`, `claudex` and
// `my-claude` with it, and each of those is an ordinary directory that may hold
// source. Prefix, suffix, case-insensitive and dot-stripping relaxations are
// covered by the same generated space.
// upperFirst capitalises the first byte. The skipped names are ASCII, so this
// is enough to generate the case-insensitivity attack without pulling in
// strings.Title's deprecated Unicode word-boundary behaviour.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func TestSkippedDir_MatchesTheWholeNameOnly(t *testing.T) {
	exact := map[string]bool{}
	for _, name := range skipped {
		exact[name] = true
	}

	var variants []string
	for _, name := range skipped {
		bare := strings.TrimPrefix(name, ".")
		for _, v := range []string{
			name + "x",
			"x" + name,
			name + "2",
			name + "-backup",
			name + "_old",
			name + "s",
			"my-" + bare,
			bare + "-my",
			bare,
			"." + name,
			name + ".",
			strings.ToUpper(name),
			strings.ToUpper(bare),
			upperFirst(bare),
			bare + "/x",
			strings.ReplaceAll(name, "_", "-"),
		} {
			if v == "" || exact[v] {
				continue
			}
			variants = append(variants, v)
		}
	}
	if len(variants) < 80 {
		t.Fatalf("generated only %d name variants from %d skipped names; the fixture space "+
			"collapsed and this test is not attacking anything", len(variants), len(skipped))
	}

	var over []string
	for _, v := range variants {
		if repowalk.SkippedDir(v) {
			over = append(over, v)
		}
	}
	sort.Strings(over)
	if len(over) > 0 {
		t.Errorf("SkippedDir matches %d names that are NOT one of the %v it may skip:\n  %s\n\n"+
			"SkippedDir must compare the whole base name. A prefix/suffix/substring or "+
			"case-insensitive relaxation quietly excludes ordinary directories, and every "+
			"guard built on it then walks past real source.",
			len(over), skipped, strings.Join(over, "\n  "))
	}
}
