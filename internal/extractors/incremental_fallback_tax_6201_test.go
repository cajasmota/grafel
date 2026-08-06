package extractors_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// isolateHome redirects every ambient path a pass might touch away from the
// developer's real home. TryIncremental takes repoPath/stateDir explicitly, but
// gitmeta / config lookups underneath it do not, and a test must never be able
// to write to the live daemon state.
func isolateHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
}

// TestIncremental_RejectPath_DoesNotSweepUnchangedFiles is the #6201 regression
// test for the fallback tax.
//
// The too-many-changed reject used to call diff.UpdateManifest(absRepo,
// allFiles, manifest) BEFORE returning — a SHA-256 hash of every file in the
// repo, changed or not — and then throw the result away, because the caller
// immediately runs a full reindex that walks and hashes everything again.
// Measured on a 3003-file fixture: 696 ms of work for a reject, on top of a
// 9,540 ms full reindex, i.e. a rejected attempt cost 7% MORE than never having
// attempted one.
//
// The property pinned here is WORK PERFORMED, not elapsed time. The manifest is
// seeded with a poison stamp for files that are provably CLEAN (committed,
// untouched, and not reported by `git diff --name-only HEAD`). A full-repo hash
// sweep necessarily overwrites those poison stamps with the real content hash;
// a delta-scoped reconcile cannot, because it never opens those files. So the
// surviving poison IS the proof that no full-repo sweep ran.
//
// The second half pins the property that must NOT be lost in the process: the
// #5668 loop guard. The stamps of the files that DID change still have to be
// refreshed and persisted, or the next pass re-reports them as changed,
// re-trips this same fallback, and the daemon loops a full reindex forever.
func TestIncremental_RejectPath_DoesNotSweepUnchangedFiles(t *testing.T) {
	isolateHome(t)
	// Force a low ceiling so the reject fires on a small fixture. This test is
	// about what the reject PATH costs, not about where the ceiling sits.
	t.Setenv("GRAFEL_INCREMENTAL_MAX_FILES", "3")

	repo := t.TempDir()
	stateDir := t.TempDir()

	const nStable = 6
	const nDirty = 5

	// Distinct basename stems throughout: diff.FilterWithGit performs
	// cross-file invalidation by basename stem (moduleBase), so a shared stem
	// would drag a "stable" file into the dirty set and invalidate the fixture.
	stable := make([]string, nStable)
	for i := range stable {
		stable[i] = fmt.Sprintf("stable_%02d.go", i)
		writeFile(t, repo, stable[i], fmt.Sprintf("package p\n\nfunc Stable%02d() {}\n", i))
	}
	dirty := make([]string, nDirty)
	for i := range dirty {
		dirty[i] = fmt.Sprintf("dirty_%02d.go", i)
		writeFile(t, repo, dirty[i], fmt.Sprintf("package p\n\nfunc Dirty%02d() {}\n", i))
	}

	initGitRepo(t, repo)
	gitCommitAll(t, repo, "base")

	buildMinimalGraph(t, stateDir, []graph.Entity{
		{ID: graph.EntityID("test-repo", "SCOPE.Operation", "Stable00", "stable_00.go"),
			Name: "Stable00", Kind: "SCOPE.Operation", SourceFile: "stable_00.go", Language: "go"},
	}, nil)

	// Manifest baseline: correct stamps for everything, pinned at HEAD.
	seedManifest(t, repo, stateDir)

	// Poison the stable entries. They are committed and untouched, so git
	// reports them clean and the change-detector never hashes them — only an
	// indiscriminate full-repo sweep can overwrite these.
	const poison = "poison-stamp-6201"
	m := diff.LoadManifest(stateDir)
	for _, rel := range stable {
		e := m.Files[rel]
		e.SHA256 = poison
		m.Files[rel] = e
	}
	// Record the pre-pass stamps of the dirty files so we can prove they WERE
	// refreshed (the #5668 loop guard).
	preDirty := make(map[string]string, nDirty)
	for _, rel := range dirty {
		preDirty[rel] = m.Files[rel].SHA256
	}
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatalf("save poisoned manifest: %v", err)
	}

	// Dirty 5 files in the working tree → 5 > limit 3 → too-many-changed.
	for i, rel := range dirty {
		writeFile(t, repo, rel, fmt.Sprintf("package p\n\nfunc Dirty%02d() { _ = %d }\n", i, i+1))
	}

	logger := log.New(io.Discard, "", 0)
	res := extractors.TryIncremental(context.Background(), repo, stateDir, logger, nil)
	if res.Done {
		t.Fatalf("expected a too-many-changed fallback, got Done=true")
	}

	after := diff.LoadManifest(stateDir)

	// PROPERTY 1 — no full-repo SHA-256 sweep. Every clean file's poison stamp
	// must have survived: hashing it is work the reject path has no use for.
	swept := 0
	for _, rel := range stable {
		e, ok := after.Files[rel]
		if !ok {
			t.Fatalf("clean file %s was pruned from the manifest by the reject path", rel)
		}
		if e.SHA256 != poison {
			swept++
		}
	}
	if swept != 0 {
		t.Fatalf("reject path hashed %d/%d provably-clean files (poison stamp overwritten) — "+
			"the too-many-changed check still runs AFTER the full-repo SHA-256 sweep it "+
			"immediately discards (#6201)", swept, nStable)
	}

	// PROPERTY 2 — the #5668 loop guard survives. The files that really did
	// change must have their stamps refreshed and persisted, or the next pass
	// re-trips this fallback forever.
	for _, rel := range dirty {
		e, ok := after.Files[rel]
		if !ok {
			t.Fatalf("changed file %s missing from the reconciled manifest", rel)
		}
		if e.SHA256 == preDirty[rel] || e.SHA256 == "" {
			t.Fatalf("changed file %s kept its stale stamp %q — the #5668 reindex loop guard "+
				"was dropped along with the sweep", rel, e.SHA256)
		}
	}
}

// TestIncremental_CeilingScalesWithRepoSize is the #6201 regression test for the
// second defect: the trigger ceiling sat ~25x below the measured crossover.
//
// The shipped ceiling was a flat 20 (feature branch) / 50 (default branch),
// independent of repo size. Refit against the measured medians on a 3003-file /
// 58.5k-entity fixture (see incrementalFilesDivisor for the arithmetic):
// 1536 ms fixed + 7.42 ms per changed file, against a 9,540 ms full reindex —
// incremental stops winning near 1078 changed files, i.e. ~0.36 of the repo.
// The flat constants therefore reject work that is 3-5x cheaper than the
// fallback they trigger.
//
// This pins the proportional term specifically: a 240-file repo must accept a
// 60-file delta, which the flat feature-branch ceiling of 20 rejects. The
// fixture is a plain (non-git) temp dir, so gitmeta.IsDefaultBranch is false and
// the floor is the feature-branch 20 — that makes 60 unambiguously a product of
// the repo-size term and not of a raised constant.
func TestIncremental_CeilingScalesWithRepoSize(t *testing.T) {
	isolateHome(t)
	t.Setenv("GRAFEL_INCREMENTAL_MAX_FILES", "") // no override: exercise the real ceiling

	repo := t.TempDir()
	stateDir := t.TempDir()

	const nFiles = 240
	const nChanged = 60 // == nFiles/4, the derived ceiling; > the flat floor of 20

	for i := 0; i < nFiles; i++ {
		writeFile(t, repo, fmt.Sprintf("f%03d.go", i),
			fmt.Sprintf("package p\n\nfunc F%03d() {}\n", i))
	}

	buildMinimalGraph(t, stateDir, []graph.Entity{
		{ID: graph.EntityID("test-repo", "SCOPE.Operation", "F000", "f000.go"),
			Name: "F000", Kind: "SCOPE.Operation", SourceFile: "f000.go", Language: "go"},
	}, nil)
	seedManifest(t, repo, stateDir)

	for i := 0; i < nChanged; i++ {
		writeFile(t, repo, fmt.Sprintf("f%03d.go", i),
			fmt.Sprintf("package p\n\nfunc F%03d() { _ = %d }\n", i, i+1))
	}

	logger := log.New(io.Discard, "", 0)
	res := extractors.TryIncremental(context.Background(), repo, stateDir, logger, nil)
	if !res.Done {
		t.Fatalf("a %d-file delta on a %d-file repo fell back (reason=%q); the ceiling must "+
			"scale with repo size — the measured crossover is ~0.43x the walked file count, "+
			"not a flat 20/50 (#6201)", nChanged, nFiles, res.FallbackReason)
	}
	if res.ChangedFiles != nChanged {
		t.Fatalf("ChangedFiles = %d, want %d", res.ChangedFiles, nChanged)
	}
}

// TestIncremental_LowOverrideIsHonouredOnLargeRepo pins the invariant that the
// repo-size ratio must NOT be maxed against an explicit operator override.
//
// effectiveLimit documents the overrides as honoured verbatim, but until this
// test nothing could fail if they were not. Every pre-existing override test
// (incremental_test.go:580, env=5 on 8 files; the reject-path test above, env=3
// on 11 files) runs on a repo small enough that walkedFiles/4 is BELOW the
// override — so `return n` and `return max(n, walkedFiles/4)` are
// indistinguishable there, and a reordering of the expression survives the
// whole suite.
//
// The scenario this protects is concrete: an operator pins
// GRAFEL_INCREMENTAL_MAX_FILES=5 on a 4,000-file repo to bound a pass during an
// incident. If the ratio is ever allowed to win, they silently get 1,000
// instead of 5, and nothing goes red.
//
// Fixture: 240 files (ratio → 60), override → 5, delta → 60. Correct behaviour
// rejects at limit=5. A ratio-wins mutant computes limit=60, does not reject,
// and returns Done — so the assertion is on the DECISION, and the limit value
// is pinned in the fallback reason so a mutant that rejects for a different
// reason cannot pass either.
func TestIncremental_LowOverrideIsHonouredOnLargeRepo(t *testing.T) {
	const nFiles = 240
	const nChanged = 60 // == nFiles/4, the ratio's value; deliberately > override
	const override = 5

	// Both override channels carry the same documented promise, and priority 1
	// (cfg) is not reachable through the env var, so drive each separately.
	for _, tc := range []struct {
		name   string
		useCfg bool
	}{
		{"env_var", false},
		{"cfg_channel", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateHome(t)

			var cfg *extractor.ExtractorConfig
			if tc.useCfg {
				t.Setenv("GRAFEL_INCREMENTAL_MAX_FILES", "")
				cfg = &extractor.ExtractorConfig{IncrementalMaxFiles: override}
			} else {
				t.Setenv("GRAFEL_INCREMENTAL_MAX_FILES", fmt.Sprint(override))
			}

			repo := t.TempDir()
			stateDir := t.TempDir()
			for i := 0; i < nFiles; i++ {
				writeFile(t, repo, fmt.Sprintf("f%03d.go", i),
					fmt.Sprintf("package p\n\nfunc F%03d() {}\n", i))
			}
			buildMinimalGraph(t, stateDir, []graph.Entity{
				{ID: graph.EntityID("test-repo", "SCOPE.Operation", "F000", "f000.go"),
					Name: "F000", Kind: "SCOPE.Operation", SourceFile: "f000.go", Language: "go"},
			}, nil)
			seedManifest(t, repo, stateDir)

			for i := 0; i < nChanged; i++ {
				writeFile(t, repo, fmt.Sprintf("f%03d.go", i),
					fmt.Sprintf("package p\n\nfunc F%03d() { _ = %d }\n", i, i+1))
			}

			res := extractors.TryIncremental(context.Background(), repo, stateDir,
				log.New(io.Discard, "", 0), cfg)

			if res.Done {
				t.Fatalf("a %d-file delta completed under an explicit override of %d on a "+
					"%d-file repo: the repo-size ratio (%d) overrode the operator's pinned "+
					"limit. An override is a ceiling the operator chose; the ratio must never "+
					"raise it (#6201)", nChanged, override, nFiles, nFiles/4)
			}
			want := fmt.Sprintf("limit=%d", override)
			if !strings.Contains(res.FallbackReason, want) {
				t.Fatalf("fallback reason = %q, want it to report %q — the reject must be "+
					"attributed to the operator's override, not to some other limit",
					res.FallbackReason, want)
			}
		})
	}
}
