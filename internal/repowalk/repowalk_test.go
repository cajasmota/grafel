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

// skipped is the exact set SkippedDir is expected to name, written out here as
// an INDEPENDENT REPLICA rather than read back out of the production switch.
//
// It has two jobs, and the second is the delicate one:
//
//  1. it is the expectation TestSkippedDir_SkipsEveryForeignTree asserts, and
//  2. it is what sourceDirComponents prunes with.
//
// Job 2 must NOT be done with repowalk.SkippedDir. The lower bound below exists
// to catch an exclusion list that grew to cover real source; a walk that pruned
// with the predicate under test would prune exactly the directories it is
// supposed to prove are read, find nothing missing, and pass. That is #6834's
// defect inside the test written to catch it.
//
// The cost of the replica is that widening the production list AND this literal
// together silently narrows the lower bound. That is a deliberate two-place
// edit, visible in the diff, and it is the same trade internal/relkinds and
// internal/types make for their independent floors (#6846).
var skipped = []string{".git", ".claude", "node_modules", "vendor", "testdata", "dist", "build"}

// repoRoot returns the repository root, derived from this file's own location
// at <repo>/internal/repowalk/repowalk_test.go.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
}

// sourceDirComponents returns every directory NAME that lies on the path from
// the repository root to a non-test .go file, mapped to one example path.
//
// Path COMPONENTS, not just the immediate parent directory. The immediate
// parent alone leaves the most destructive widening invisible: `internal`,
// `cmd` and `tools` hold no .go file directly, so an exclusion list widened
// with "internal" hides the entire source tree while a parent-name-only domain
// never mentions it. Components make every directory a guard must descend
// THROUGH part of the lower bound, not just the ones it ends at.
//
// Rooted at the repository root, not at internal/: the guards that consume
// SkippedDir (internal/atomicfile, the two internal/registry sweeps) are
// repoRoot-rooted and do inspect cmd/ and tools/.
func sourceDirComponents(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	prune := map[string]bool{}
	for _, name := range skipped {
		prune[name] = true
	}

	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && prune[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			return nil
		}
		for _, comp := range strings.Split(dir, "/") {
			if _, seen := out[comp]; !seen {
				out[comp] = rel
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// TestSkippedDir_NeverHidesRepositorySource is the LOWER BOUND: it fails if the
// exclusion list grows to cover a directory a guard must descend into to reach
// this repository's own Go source.
//
// Every other test of an exclusion list in this repository grades the recall
// direction — "is the foreign tree skipped?" — and a recall-shaped suite is
// blind to over-firing. Widening this switch with real directory names silently
// narrows five guards at once: they then report a clean tree they never looked
// at, which is the most direct cause of #6834's whole defect class.
func TestSkippedDir_NeverHidesRepositorySource(t *testing.T) {
	dirs := sourceDirComponents(t)

	// Layer 2/3 of the vacuity check: a count floor alone catches only "read
	// anything". These anchors pin that the walk read the RIGHT tree — an
	// inverted or misrooted filter scans plenty of files and clears a floor.
	// The first three are the top-level trunks whose absence from a
	// parent-directory-only domain was the gap this test used to have.
	for _, want := range []string{
		"internal", "cmd", "tools",
		"engine", "graph", "extractors", "atomicfile",
		"registry", "entkinds", "relkinds", "types", "repowalk",
	} {
		if _, ok := dirs[want]; !ok {
			t.Fatalf("walk of the repository found no non-test Go source under any directory "+
				"named %q; the walk is not binding the tree, so anything it reports is meaningless", want)
		}
	}
	if len(dirs) < 150 {
		t.Fatalf("walk of the repository found only %d directory names on the path to non-test "+
			"Go source; this repository has far more, so the walk is not binding the tree", len(dirs))
	}

	var hidden []string
	for name, example := range dirs {
		if repowalk.SkippedDir(name) {
			hidden = append(hidden, name+" (e.g. "+example+")")
		}
	}
	sort.Strings(hidden)
	if len(hidden) > 0 {
		t.Errorf("SkippedDir excludes %d directories that a guard must descend into to reach "+
			"this repository's own non-test Go source:\n  %s\n\n"+
			"Every guard built on SkippedDir walks past them and reports a clean tree it "+
			"never read. An exclusion list may only name trees that are foreign to this "+
			"repository (.git, agent worktrees, vendored deps, fixtures, build output) — "+
			"never a directory the guards exist to inspect, and never one they must pass "+
			"through to reach it.",
			len(hidden), strings.Join(hidden, "\n  "))
	}
}

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

// upperFirst capitalises the first byte. The skipped names are ASCII, so this
// is enough to generate the case-insensitivity attack without pulling in
// strings.Title's deprecated Unicode word-boundary behaviour.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
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
