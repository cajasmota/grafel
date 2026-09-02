// Package extractors — incremental.go implements the S3 incremental
// file-level reindex path (issue #2153 of epic #2149).
//
// # Conservative v1 design (S3 #2167) + follow-up (S3 #2170)
//
// The full-reindex pipeline rewrites graph.fb from scratch on every daemon
// watcher tick. This path instead parses only the changed files, swaps their
// entities in the graph, and atomically re-emits graph.fb.
//
// MEASURED, not aspirational (#6199/#6201). Fixture: a synthetic 3003-file /
// 58.5k-entity Go+Python tree, GOMAXPROCS=4, N=14, medians:
//
//	full reindex                    9,540 ms
//	incremental,   1 changed file   1,576 ms   (6.0x faster)
//	incremental,  50 changed files  1,920 ms   (4.9x)
//	incremental, 200 changed files  2,947 ms   (3.2x)
//	incremental, 500 changed files  5,276 ms   (1.8x)
//
// The header of this file previously claimed ~5 s for the full reindex and
// ~200 ms for a one-file edit. Both were unmeasured and both were wrong by
// roughly a factor of 8; the ~25x speedup claimed at
// internal/extractor/extractor.go was wrong by a factor of 4. The reason the
// numbers are so much flatter than "re-parse one file" suggests is that only
// two phases are O(delta) — see #6199 for the per-phase breakdown of the
// O(repo)/O(graph) fixed tail that dominates every pass.
//
// Correctness guarantee: the opt-in flag (GRAFEL_INCREMENTAL_REINDEX=1)
// is NOT set by default. Four safety valves are applied before attempting a
// partial reindex (#2170 adds env-override limit + main-branch hot-path):
//
//  1. Trigger limit: if more than the effective limit files changed in the
//     debounced batch we fall back to full reindex. The effective limit is:
//     - cfg.IncrementalMaxFiles / GRAFEL_INCREMENTAL_MAX_FILES, honoured
//     verbatim when set (including values below the floors), or
//     - max(floor, walkedFiles/4), floor = 50 on the repo's default branch
//     and 20 on a feature branch (#6201).
//     There is no hard floor of 5: no such clamp exists in effectiveLimit or
//     in ExtractorConfig.EffectiveIncrementalMaxFiles, and
//     GRAFEL_INCREMENTAL_MAX_FILES=1 is honoured as 1. The header claimed one
//     until #6201 deleted the claim rather than the behaviour.
//
//  2. AST-hash gate: files whose content hash (SHA-256) is unchanged since
//     the last manifest stamp are skipped entirely (whitespace-only edits).
//
//  3. Signature-change incremental (#2170): entities whose Signature or key
//     Properties changed trigger a reverse-index look-up for inbound CALLS /
//     REFERENCES edges, which are re-resolved in the scoped pass rather than
//     falling back to full reindex.
//
//  4. Unresolved-relationship safety net: if the scoped resolver encounters
//     a relationship whose target is outside the changed-file set and cannot
//     be re-resolved from the existing graph, we fall back to full reindex
//     and log the reason.
//
// Manifest robustness (#2170):
//   - GC: manifest entries for files that no longer exist are removed before
//     any incremental pass so the deleted-file list stays clean.
//   - Corruption recovery: if LoadManifest returns a malformed manifest we log
//     and fall back to full reindex rather than panicking.
//
// Test coverage — read this before trusting a claim of equivalence (#6033).
// incremental_test.go does NOT compare against a full reindex: running the full
// Index() pipeline from this package's tests would import cmd/grafel and create
// an import cycle. What it verifies is that the incremental path's own output
// tracks source mutations (entities appear/disappear as expected). Full-vs-
// incremental equivalence is only covered at the integration level.
//
// incremental_dupe_test.go carries the relationship-CARDINALITY assertions.
// They exist because every other assertion in this package is existence-only
// ("is edge X present?"), which is blind to duplication — that blindness is
// exactly how #6033 (every pass duplicating the whole surviving edge set)
// survived from #2167 until incremental became default-on in #5231. Any change
// to the merge step in Step 7/8 below must keep those cardinality tests green.
package extractors

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/classifier"
	"github.com/cajasmota/grafel/internal/coverage"
	"github.com/cajasmota/grafel/internal/daemon/walk"
	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/sresolver"
	"github.com/cajasmota/grafel/internal/gitmeta"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/indexer/diff"
	"github.com/cajasmota/grafel/internal/install/detect"
	"github.com/cajasmota/grafel/internal/module"
	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/types"
)

// defaultIncrementalFiles is the FLOOR trigger limit for feature branches
// (S3 #2170). It is a floor, not the limit: see incrementalFilesDivisor.
const defaultIncrementalFiles = 20

// mainBranchIncrementalFiles is the FLOOR trigger limit for the default (main)
// branch. Commits to main tend to be small focused changes.
const mainBranchIncrementalFiles = 50

// incrementalFilesDivisor derives the trigger limit from the size of the repo
// being indexed: limit = max(floor, walkedFiles/incrementalFilesDivisor).
//
// WHY A RATIO AT ALL, and why this one (#6201, measured under #6199).
//
// The point of the trigger limit is to fall back to a full reindex once
// incremental stops being cheaper. That crossover is not a constant: it is
// where (fixed tail + per-file cost x delta) meets the full-reindex cost, and
// BOTH sides scale with the repo. A flat constant can only be right at one repo
// size, and the shipped flat 20/50 was wrong by ~25x.
//
// Measured cost model — fixture: a synthetic 3003-file / 58.5k-entity Go+Python
// tree, GOMAXPROCS=4, N=14, medians (issue #6199's per-phase matrix; the same
// four columns tabulated in this file's header):
//
//	full reindex = 9540 ms
//	incremental  = 1536 ms fixed + 7.42 ms per changed file
//	=> crossover at (9540 - 1536) / 7.42 = ~1078 changed files
//	=> ~0.36 x the walked file count on that fixture
//
// That is an ordinary least-squares fit over all four measured points
// (1/50/200/500 changed files → 1576/1920/2947/5276 ms). Residuals
// +33/+13/-74/+28 ms, worst 2.5%. A two-point fit over the extremes alone
// agrees closely: 7.41 ms/file, intercept 1569 ms, crossover ~1075 files
// (0.358x) — so the number is not an artifact of the fitting method.
//
// #6201's issue body stated 1451 ms + 6.26 ms/file → crossover ~1292 (0.43x),
// and this comment repeated it. It does NOT reproduce the table it was drawn
// from: it under-predicts every measured point, by +119/+156/+244/+695 ms, the
// error growing with the delta — i.e. the slope is ~16% too shallow, which
// inflates the crossover by ~20%. Corrected here rather than propagated,
// because shipping an unreconcilable performance claim inside the fix for
// unreconcilable performance claims would be self-defeating.
//
// Corroborating points from the same matrix, all of which the flat ceiling
// rejected: 50 changed files = 20% of a full reindex; 200 = 31%; 500 = 55%.
//
// The divisor is 4 (limit = 0.25 x repo), NOT the measured 0.36. The margin is
// deliberate and is the honest part of this number:
//
//   - The crossover is one fixture. The fixed tail scales with GRAPH size and
//     the per-file term with FILE size, so a repo with denser entities per file
//     crosses over sooner. 0.25 sits 30% below the measured 0.359, which covers
//     a fixture up to ~1.44x denser in entities per file before the ratio
//     starts losing. (Against the issue's overstated 0.43 the same divisor
//     looked like a 42% margin and 1.7x of headroom; the real figures are 30%
//     and 1.44x. The divisor is unchanged because it is below both crossovers —
//     but the margin is the entire justification for it, so it has to be the
//     real one.)
//   - Peak memory, not just throughput, is a reason the ceiling exists — a
//     large delta can mean a large peak, and that is an independent reason to
//     bound it. Re-checked AT the new ceiling rather than assumed: on a
//     1200-file fixture (ceiling 300) the Go heap is flat across the whole
//     range — HeapSys 93.1 / 94.3 / 93.3 MB and HeapInuse 58.6 / 42.5 / 65.6 MB
//     at 1 / 50 / 300 changed files, with total allocation growing only ~1.6x
//     over a 300x delta increase. That matches #6199's own matrix
//     (257-289 MB from 1 to 500 changed files, vs 465 MB RSS for a full
//     reindex). Memory therefore does not argue for a low ceiling on either
//     fixture — but it is still two fixtures, and the margin buys room for it.
//     RSS was deliberately NOT used: the measuring machine was swapping, which
//     makes RSS an upper bound rather than a footprint.
//
// If you re-measure on a different fixture, change the divisor here and say
// which fixture, rather than re-deriving this from scratch.
const incrementalFilesDivisor = 4

// effectiveLimit returns the trigger-limit for the given repoPath, optional
// ExtractorConfig, and the number of files the repo walk produced.
//
// Priority (issue #2320, ratio added in #6201):
//  1. cfg.IncrementalMaxFiles (when cfg is non-nil and > 0) — Config channel.
//  2. GRAFEL_INCREMENTAL_MAX_FILES env var (backward-compat fallback).
//     Both overrides are honoured verbatim — the ratio is NOT maxed against
//     them. An operator pinning a small number on a large repo (e.g. 5 during
//     an incident, to bound the pass) means 5, and must not silently receive
//     walkedFiles/4 instead. Pinned by
//     TestIncremental_LowOverrideIsHonouredOnLargeRepo, which places the
//     override below the ratio so the two are distinguishable — the earlier
//     override tests all ran on repos small enough that the ratio could not
//     have won anyway, and a reordering of this expression survived them.
//  3. Otherwise max(branch floor, walkedFiles/incrementalFilesDivisor). The
//     floor keeps small repos at their previous behaviour — a 40-file repo
//     would otherwise get a ceiling of 10, which is strictly worse than the
//     20/50 it has today.
//
// THE BRANCH DISTINCTION IS NOW DEAD ABOVE ~200 FILES, deliberately.
// walkedFiles/4 overtakes the feature-branch floor of 20 at 84 walked files and
// the default-branch floor of 50 at 204, so from ~204 files up both branches
// get the identical ceiling and gitmeta.IsDefaultBranch stops affecting the
// outcome. That is not an oversight — it retires a proxy:
//
//   - The trigger limit is a COST gate, not a risk gate. What guards
//     CORRECTNESS on this path is branch-independent: the AST-hash gate, the
//     scoped resolver's unresolved-relationship safety net, and the
//     fall-through to a full reindex on any precondition failure (safety
//     valves 2-4 in this file's header). A 300-file delta is not less safe to
//     patch on main than on a feature branch — it is only more expensive, and
//     the ratio prices exactly that.
//   - The 20/50 split was a stand-in for "how big is a typical delta here",
//     which scales with the repo. Keying that guess to the branch could only
//     ever be right at one repo size — the same mistake as the flat ceiling
//     itself, one level down.
//
// The floors survive only where the ratio is uselessly small, i.e. repos under
// ~200 files, which is precisely the regime where the branch heuristic was
// cheap and harmless to keep. If a future measurement shows feature branches
// genuinely need a tighter bound at scale, express it as a second divisor, not
// as a constant.
func effectiveLimit(repoPath string, cfg *extractor.ExtractorConfig, walkedFiles int) int {
	if n := cfg.EffectiveIncrementalMaxFiles(); n > 0 {
		return n
	}
	floor := defaultIncrementalFiles
	if gitmeta.IsDefaultBranch(repoPath) {
		floor = mainBranchIncrementalFiles
	}
	if scaled := walkedFiles / incrementalFilesDivisor; scaled > floor {
		return scaled
	}
	return floor
}

// IncrementalEnabled reports whether S3 incremental reindex is opt-in active.
// Reads GRAFEL_INCREMENTAL_REINDEX once per call — cheap, no caching needed
// at this level (the scheduler gate is the hot path).
//
// Issue #2320: callers that have an ExtractorConfig should call
// cfg.IsIncrementalEnabled() directly; this function is the backward-compat
// entry point for callers that have not yet been migrated.
func IncrementalEnabled() bool {
	var cfg *extractor.ExtractorConfig // nil → pure env-var path
	return cfg.IsIncrementalEnabled()
}

// Result is the outcome of a TryIncremental call.
type Result struct {
	// Done is true when the incremental patch completed successfully and the
	// caller should NOT fall through to a full reindex.
	Done bool

	// FallbackReason is non-empty when Done=false and the incremental path
	// explicitly decided to fall back (as opposed to encountering an error it
	// could not recover from).
	FallbackReason string

	// ChangedFiles is the number of files that were re-extracted.
	ChangedFiles int

	// Duration is the wall-clock time spent on the incremental pass.
	Duration time.Duration
}

// FileStamp records the per-file hash state used by the AST-hash gate.
type FileStamp struct {
	ContentHash string // hex SHA-256 of raw bytes
	Mtime       int64  // UnixNano — fast first-pass filter
}

// StampFile computes the FileStamp for the file at absPath.
func StampFile(absPath string) (FileStamp, error) {
	info, err := os.Lstat(absPath)
	if err != nil {
		return FileStamp{}, fmt.Errorf("stat %s: %w", absPath, err)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return FileStamp{}, fmt.Errorf("read %s: %w", absPath, err)
	}
	h := sha256.New()
	h.Write(data)
	return FileStamp{
		ContentHash: hex.EncodeToString(h.Sum(nil)),
		Mtime:       info.ModTime().UnixNano(),
	}, nil
}

// frameworkDetectorOnce guards the one-time load of the embedded YAML rule sets
// Pass 2.5 applies to every re-extracted file (#6148, #6150).
//
// The rules are compiled into the binary (engine.LoadAllRules reads an embedded
// FS) and are not repo-scoped, so one Detector serves every incremental run in
// the process; engine.Detector is documented safe for concurrent use.
//
// COST, measured here (go test -bench, GOMAXPROCS=4, 40-declaration file):
//
//	LoadAllRules + New   15.6 ms, 11.6 MiB   ONCE per process
//	Detect, python file   8.7 ms,  0.7 MiB   per CHANGED file
//
// The per-file number is the real one and it is not small next to the language
// extractor. Three things bound it, and none of them is a guard that could be
// added later:
//
//   - It is NOT avoidable by pre-checking "does this file declare a class".
//     Detect's output is also consumed for framework entities that pair with no
//     extractor record at all, and `app = falcon.App()` alone — a line with no
//     class anywhere near it — yields BOTH a Service and a Config record. A file
//     with no class-like declaration is therefore precisely a file whose Detect
//     output such a guard would silently drop again (#6150).
//   - "Only run Detect for languages that have rules" is not a missing
//     mitigation: Detector.Detect already early-outs on an unknown language
//     before it touches the content (detector.go, the `sets, ok :=
//     d.compiled[file.Language]` branch). Those languages pay ~0, not 8.7 ms.
//   - It is bounded by the trigger limit, which since #6201 is
//     max(20|50, walkedFiles/4) rather than a flat 20/50 — so this is now a
//     repo-sized worst case, not a fixed one (a 3000-file repo caps at 750
//     changed files ≈ 6.5 s of Detect). That is accounted for: the 6.26 ms per
//     changed file in #6201's cost model was measured on code that already runs
//     Detect, so the ~0.43x-of-repo crossover the divisor is derived from
//     already carries this term. Above the cap the path falls back to a full
//     rebuild, which pays the same 8.7 ms for EVERY file in the repo.
//
// Content-hash caching across runs would buy nothing: Step 3's AST-hash gate has
// already established that every file reaching here has changed content.
//
// The once is deliberately not retried. The rules are embedded, so a load
// failure is a build-level defect with no transient component, and retrying
// would re-pay 15.6 ms per file to fail identically. It is logged with its
// consequence spelled out, because the graph is otherwise silently the
// pre-#6148 shape until the process restarts.
var (
	frameworkDetectorOnce sync.Once
	frameworkDetectorInst *engine.Detector
)

// writeGraphGen is fbwriter.WriteGraphGen behind a var so a test can land a
// working-tree write at the one point that matters for #6212: after this pass
// read the bytes it extracted, before Step 9 records what it indexed. That is
// the same seam, for the same reason, as cmd/grafel/index.go's — and this is the
// path that actually runs, since TryIncremental's only non-test caller is the
// daemon scheduler.
var writeGraphGen = fbwriter.WriteGraphGenReport

// frameworkDetector returns the shared Detector, or nil if the embedded rules
// failed to load. A nil return degrades this path to its pre-#6148 behaviour
// (classes keep their generic kind, framework-only entities are not emitted)
// rather than failing the incremental run: the rules are an enrichment input,
// not a correctness precondition for re-extraction.
//
// It is a var so a test can drive that degraded path. Nothing else assigns it —
// the #6129 parity gate catches the rules being IGNORED, but nothing else
// asserted that LOSING them degrades instead of panicking.
var frameworkDetector = func() *engine.Detector {
	frameworkDetectorOnce.Do(func() {
		rules, err := engine.LoadAllRules()
		if err != nil {
			log.Printf("incremental: FRAMEWORK RULES UNAVAILABLE: %v — "+
				"every incremental run in this process will leave class entities at their "+
				"generic kind and drop framework-only entities, diverging from a full rebuild "+
				"until the process is restarted", err)
			return
		}
		frameworkDetectorInst = engine.New(rules)
	})
	return frameworkDetectorInst
}

// TryIncremental attempts a file-level incremental reindex for repoPath.
// stateDir is the on-disk directory where graph.fb and file-index.json live.
// logger may be nil (falls back to stderr).
// cfg is optional (nil-safe): when non-nil its IncrementalMaxFiles value
// overrides the env-var / gitmeta heuristic for the trigger limit (issue #2396).
//
// The call flow:
//  1. Load the diff manifest; detect changed files.
//  2. If > maxIncrementalFiles changed → fallback (full reindex).
//  3. AST-hash gate: skip files with identical SHA-256 content hash.
//  4. Load existing graph.Document from stateDir.
//  5. Remove entities (and their outbound relationships) sourced from changed files.
//  6. Re-extract each changed file via the registered language extractor.
//  7. Scoped resolver pass: re-resolve inbound cross-file relationships
//     targeting newly extracted entities.
//  8. Merge new entities/rels into the document, sort, write graph.fb atomically.
//  9. Update the diff manifest.
//
// #6199 — TryIncremental is now a thin wrapper whose only job is to own the
// per-phase trace across EVERY return path (there are 10+ fallback returns in
// tryIncremental, and a trace that misses the fallbacks would miss precisely the
// passes that pay the fixed tail and then throw it away). The pass body moved
// verbatim into tryIncremental; no phase ordering changed.
func TryIncremental(ctx context.Context, repoPath, stateDir string, logger *log.Logger, cfg *extractor.ExtractorConfig) Result {
	t0 := time.Now()
	if logger == nil {
		logger = log.New(os.Stderr, "incremental: ", log.LstdFlags)
	}
	tr := newPhaseTrace(t0)
	var res Result
	var returned bool
	// The emit is DEFERRED, not a plain trailing call, and that is load-bearing.
	// A plain call is only reached on a normal return; a panic unwinds straight
	// past it — and past the stopHeapSampler that only emit performs, stranding
	// the 5 ms runtime.ReadMemStats (stop-the-world) sampler goroutine for the
	// life of the process. That is not hypothetical here: sched.scheduler wraps
	// this call in a recover() precisely because an index can panic for reasons
	// the fbwriter fail-soft does not catch, so the daemon survives the panic
	// and would accumulate one stranded sampler per recovered panic.
	//
	// TestPhaseTrace_EveryReturnIsTraced accepts this shape (a deferred FuncLit
	// whose body calls emit unconditionally) as satisfying the "emit dominates
	// every exit" invariant, and TestPhaseTrace_PanicStillEmitsAndStopsSampler
	// drives a real panic through it.
	defer func() {
		changed := res.ChangedFiles
		if changed == 0 {
			changed = tr.changedFiles
		}
		reason := res.FallbackReason
		if !returned {
			// Unwinding: res is the zero Result, so say so rather than
			// reporting an empty fallback reason that reads like a bug here.
			reason = "panic (unwound past tryIncremental)"
		}
		tr.emit(logger.Printf, repoPath, res.Done, reason, changed, tr.walkedFiles, tr.entities, tr.rels)
	}()
	res = tryIncremental(ctx, repoPath, stateDir, logger, cfg, t0, tr)
	returned = true
	return res
}

func tryIncremental(ctx context.Context, repoPath, stateDir string, logger *log.Logger, cfg *extractor.ExtractorConfig, t0 time.Time, tr *phaseTrace) Result {

	// --- Step 1: load manifest + detect changed files ---
	// Manifest robustness (#2170): LoadManifest already returns an empty
	// manifest on corruption (json.Unmarshal error or version mismatch) and
	// logs internally. For an incremental pass a fresh manifest means no
	// known baseline → we cannot safely do incremental → fall back.
	endManifestLoad := tr.span("manifest-load")
	manifest := diff.LoadManifest(stateDir)
	endManifestLoad()
	if manifest == nil {
		// Should never happen given diff.LoadManifest always returns non-nil,
		// but guard defensively.
		logger.Printf("incremental: manifest nil (corruption?) → fall back to full reindex")
		return fallback(t0, "manifest-nil")
	}

	// Walk the repo to get the full file list.
	absRepo, err := filepath.Abs(repoPath)
	if err != nil {
		return fallback(t0, "abs-repo: "+err.Error())
	}
	// --- Pass-start commit capture (#6474) ---
	// Captured HERE, before the walk, because everything downstream describes
	// the tree as it is at THIS instant: walkSourceFiles' file list, the
	// per-file stamps taken from the bytes read during extraction, and the
	// graph built from them. Re-asking git at save time (what
	// diff.SaveManifest does) labels that graph with whatever HEAD happens to
	// be minutes later — a commit whose bytes this pass never read.
	//
	// One capture, three uses: the #5710 head-advance compare below and both
	// manifest saves (zero-change reconcile, and Step 9's success write).
	// Deriving all three from one variable is the point — a second capture
	// would reintroduce the divergence at a different seam.
	//
	// The single consumer that this makes a CORRECTNESS difference to is the
	// head-advance detector: an over-advanced label makes the next pass see
	// HEAD == manifest, find nothing changed, and report Done over a graph
	// built from an earlier commit, with nothing left to re-request the work.
	// The remaining consumers (statusline/dashboard, RPC index status) are
	// reporting surfaces; no automatic reindex is keyed on them.
	passHeadShort, passHeadFull := diff.HeadCommitPair(absRepo)
	passStartCommitHook(passHeadShort, passHeadFull)

	endWalk := tr.span("tree-walk")
	allFiles, irregularReport, walkErr := walkSourceFiles(absRepo)
	endWalk()
	// #6416: say it on the daemon path too. Without this the only channel a
	// skipped FIFO had was the foreground index's stderr.
	if irregularReport != "" {
		logger.Print(irregularReport)
	}
	tr.walkedFiles = len(allFiles)
	if walkErr != nil {
		return fallback(t0, "walk: "+walkErr.Error())
	}

	endGitDiff := tr.span("git-diff-filter")
	changedFiles, _ := diff.FilterWithGit(absRepo, allFiles, manifest)
	endGitDiff()

	// Detect deleted files: files that were in the manifest but no longer
	// appear in the current walk (i.e. they have been deleted from disk).
	allFilesSet := make(map[string]bool, len(allFiles))
	for _, f := range allFiles {
		allFilesSet[f] = true
	}
	var deletedFiles []string
	for rel := range manifest.Files {
		if !allFilesSet[rel] {
			deletedFiles = append(deletedFiles, rel)
		}
	}

	// --- #5710: HEAD-advance detection ---
	// diff.FilterWithGit / diff.GitChangedFiles only ever compute
	// `git diff --name-only HEAD` — working-tree vs the CURRENT HEAD. After a
	// fetch+reset / checkout / pull the working tree already matches the new
	// HEAD, so that diff is empty even though the indexed graph is still
	// pinned at manifest.GitCommit (the commit we last actually indexed).
	// Compare the persisted manifest commit against the repo's current HEAD
	// and, when they differ, union in the commit-RANGE diff so those files
	// enter the changed-file set and flow through the normal trigger-limit /
	// AST-hash-gate machinery below (a large advance correctly trips the
	// too-many-changed full-reindex fallback).
	//
	// headAdvanceUnconfirmed is set when HEAD moved but we could NOT compute
	// the range diff (e.g. manifest.GitCommit is no longer reachable — gc,
	// shallow clone, history rewrite). In that case we must not trust a
	// totalChanged==0 result below: report unresolved-range-diff as an
	// explicit fallback so a full reindex reconciles the graph rather than
	// silently no-op'ing.
	headAdvanceUnconfirmed := false
	endHeadAdv := tr.span("head-advance")
	// #6474: was its own diff.HeadCommit(absRepo) call here — i.e. HEAD as of
	// AFTER the walk and the git diff filter. Now the pass-start capture, so
	// the commit this detector compares against is the same one the manifest
	// will be stamped with.
	currentHead := passHeadShort
	if manifest.GitCommit != "" && currentHead != "" && manifest.GitCommit != currentHead {
		rangeChanged, rErr := diff.GitChangedFilesSince(absRepo, manifest.GitCommit)
		if rErr != nil {
			headAdvanceUnconfirmed = true
			logger.Printf("incremental: head-advance range-diff unconfirmed old=%s new=%s err=%v",
				manifest.GitCommit, currentHead, rErr)
		} else if len(rangeChanged) > 0 {
			seen := make(map[string]bool, len(changedFiles))
			for _, f := range changedFiles {
				seen[f] = true
			}
			for f := range rangeChanged {
				if allFilesSet[f] && !seen[f] {
					changedFiles = append(changedFiles, f)
					seen[f] = true
				}
			}
		}
	}
	endHeadAdv()
	if headAdvanceUnconfirmed {
		// We cannot trust the changed-file accounting when the commit-range
		// diff itself failed to confirm what moved between manifest.GitCommit
		// and currentHead. Force a full-reindex fallback WITHOUT touching the
		// manifest — advancing manifest.GitCommit here (as the pre-fix
		// totalChanged==0/too-many-changed paths unconditionally did) would
		// reproduce the exact #5710 self-conceal bug: the manifest would
		// claim "indexed to HEAD" while the graph never actually caught up,
		// and every subsequent poll would see 0 changes forever.
		return fallback(t0, fmt.Sprintf("head-advance-unconfirmed old=%s new=%s", manifest.GitCommit, currentHead))
	}

	// --- Manifest GC (#2170): eagerly remove entries for deleted files ---
	// Remove them from the manifest NOW so that if we fall back to full reindex
	// or succeed incrementally, the manifest saved at the end does not contain
	// stale entries for files that no longer exist. (The subsequent code also
	// calls `delete(manifest.Files, rel)` per deleted file during entity
	// pruning, but doing it here in a single pass is cleaner.)
	for _, rel := range deletedFiles {
		logger.Printf("incremental: manifest-gc removing deleted entry %s", rel)
		delete(manifest.Files, rel)
	}

	totalChanged := len(changedFiles) + len(deletedFiles)

	// --- Step 2: trigger limit (#2170 raised limits + main-branch hot-path) ---
	// Issue #2396: cfg is now threaded through from the caller so programmatic
	// config overrides the env-var / gitmeta path when non-nil.
	// Issue #6201: the limit is now derived from the walked file count, because
	// the crossover it approximates scales with the repo — see
	// incrementalFilesDivisor for the measurement.
	limit := effectiveLimit(absRepo, cfg, len(allFiles))
	if totalChanged > limit {
		// Reconcile + persist the manifest BEFORE falling back, but at O(delta),
		// not O(repo) (#6201).
		//
		// Both loop guards this path carries are preserved, and neither needs a
		// full-repo sweep:
		//   #5667 — prune entries absent from the gitignore-aware walk, or a
		//           now-ignored file is reported "deleted" forever and re-trips
		//           this fallback on every pass. That is a map walk: free.
		//   #5668 — refresh the STAMPS of the files that actually changed, or
		//           they re-surface as changed next pass and re-trip it too.
		//           That is O(changed), and changedFiles is exactly that set.
		// What is NOT needed is re-hashing the files the change-detector just
		// established are CLEAN. That sweep measured ~220 ms of the 696 ms a
		// reject burned on a 3003-file fixture, and every byte of it was thrown
		// away: the caller's full reindex re-walks and re-hashes the repo
		// immediately afterwards. A rejected attempt cost 7% MORE than never
		// having attempted one (#6201).
		//
		// Log a path SAMPLE so a recurrence is diagnosable, not just a bare
		// count (#5668).
		// THE COMMIT LABEL MUST NOT ADVANCE HERE (#5822 ②).
		//
		// This pass changed no entity: it reconciles the manifest and asks the
		// caller for a full reindex. The persisted graph is therefore still the
		// graph built at manifest.GitCommit, and that is the only value of
		// git_commit that is TRUE. SaveManifest stamps LIVE HEAD, which made
		// the manifest claim "indexed at HEAD" for a pass that indexed nothing;
		// daemon.indexedCommitShortNoGit and daemon.IndexedCommitForRepo both
		// prefer this field over the graph.fb header, so the statusline, the
		// dashboard and grafel_index_status all reported a commit the graph had
		// never contained — and IndexedCommitForRepo reported AtHead=true over
		// it. If the full reindex we are requesting is cancelled,
		// watchdog-killed, or merely queued behind other work, that label stays
		// advanced over the older graph indefinitely.
		//
		// It is also self-concealing, which is the worse half. The #5710
		// head-advance detector above re-surfaces this delta on every later
		// pass precisely BECAUSE manifest.GitCommit trails HEAD. Advancing it
		// here disarms that detector: the next pass sees HEAD == manifest,
		// stamps that match disk, zero changes, and returns Done=true over a
		// graph that was never rebuilt. The reindex is then never requested
		// again by anything.
		//
		// PRESERVING the previous value is the choice, not blanking it. Blank
		// would be the honest answer to "we do not know", but we DO know — the
		// graph is at manifest.GitCommit — and blanking additionally breaks the
		// `manifest.GitCommit != ""` guard the head-advance detector is gated
		// on, silently disabling the same detector by the other route.
		//
		// NOT saving at all would also fix the label, and is wrong: #6201/#5668
		// established that this path MUST persist a reconciled manifest, or the
		// changed files re-surface next pass and re-trip this very reject
		// forever, and #5667's prune of entries absent from the gitignore-aware
		// walk stops happening too. The write stays; only the commit stamp is
		// corrected.
		endMS := tr.span("manifest-scoped-restamp")
		diff.UpdateManifestScoped(absRepo, changedFiles, allFiles, manifest)
		endMS()
		endMSave := tr.span("manifest-save")
		_ = diff.SaveManifestAtCommit(stateDir, manifest, manifest.GitCommit, manifest.GitCommitFull)
		endMSave()
		logger.Printf("incremental: too-many-changed files=%d limit=%d (changed=%d deleted=%d) changed=%v deleted=%v",
			totalChanged, limit, len(changedFiles), len(deletedFiles),
			samplePaths(changedFiles), samplePaths(deletedFiles))
		return fallback(t0, fmt.Sprintf("too-many-changed files=%d limit=%d",
			totalChanged, limit))
	}
	if totalChanged == 0 {
		// --- #5710 (follow-up): absent-graph guard ---
		// A no-op is only SAFE when the ref pin resolves to a MATERIALIZED
		// graph.fb. After a store relocation/recreation (repo moved → new
		// path-keyed store, store hash changed) the ref→graph pin can survive
		// while the NEW store has NO graph.fb at all. HEAD still equals
		// manifest.GitCommit, so the HEAD-advance guard above sees no advance —
		// and the pre-fix code reported success (Done:true) over that absent
		// graph while the working tree was full of source. That is silent
		// success over an empty graph: `grafel index --async` "completes" fast
		// + cheap and leaves 0 entities.
		//
		// The guard fires on ABSENCE only (graph.fb missing), NOT on a
		// present-but-0-entity graph. This is deliberate and loop-proof:
		//   - The real reported case starts with an absent graph.fb in the
		//     fresh store, so !ok still catches it and forces the reindex.
		//   - A forced reindex WRITES a graph.fb (present, even if 0 entities),
		//     so the next cycle sees ok=true → clean no-op. No infinite loop.
		//   - A genuinely codeless repo whose walked files (e.g. .txt / LICENSE
		//     / extensionless — no registered extractor) yield 0 entities keeps
		//     a present-0-entity graph and correctly no-ops. Firing on
		//     0-entities would re-index it every cycle forever (~9min each) on
		//     the hot reactive path — the loop the reviewer reproduced.
		//
		// PersistedStatsFromDir reads the graph.fb header cheaply (no entity
		// materialization) and reports ok=false only when graph.fb is absent
		// or unreadable. When absent AND the walked working-tree set is
		// non-empty, do NOT no-op and do NOT advance the manifest (which would
		// self-conceal the absence on every later poll) — force a full reindex
		// via the same fallback signal the too-many-changed path emits.
		endHdr := tr.span("graph-header-stat")
		_, ok := graph.PersistedStatsFromDir(stateDir)
		endHdr()
		if !ok && len(allFiles) > 0 {
			logger.Printf("incremental: absent-graph-nonempty-tree files=%d → force full reindex", len(allFiles))
			return fallback(t0, fmt.Sprintf("absent-graph-nonempty-tree files=%d", len(allFiles)))
		}
		// Nothing to do — manifest is already up-to-date and the graph is a
		// genuine reflection of the (possibly empty / codeless) tree.
		//
		// READ THIS BEFORE DELETING THIS CALL (#6201). #6199 lists the sweep on
		// a zero-change pass as pure waste and the cheapest remaining win, and
		// on its own that is true. But since #6201 made the too-many-changed
		// rejects re-stamp only the CHANGED files, this full-repo
		// UpdateManifest is also the path that HEALS what a scoped reject left
		// behind: a file that was stale or absent in the manifest but not in
		// the reject's changed set keeps its old stamp until some pass sweeps
		// the whole repo, and this is the only remaining pass that does. The
		// consequence of never healing is over-reporting (those files read as
		// changed and are re-extracted), not a stale graph — so this is a
		// latent cost, not a correctness bug, and it is why the deletion is
		// still worth doing. Just replace the healing when you do: sweep here
		// but skip the SaveManifest, or move the reconcile onto the fallback
		// full index. Deleting it naively makes those entries permanent.
		endMS := tr.span("manifest-sha256-sweep")
		diff.UpdateManifest(absRepo, allFiles, manifest)
		endMS()
		endMSave := tr.span("manifest-save")
		// #6474: the pass-start commit, not live HEAD at save time. This pass
		// CONFIRMED that no walked source file differs between the manifest's
		// commit and passHeadShort, so the existing graph does describe
		// passHeadShort and advancing to it is correct (that is what
		// TestIncremental_ZeroChangePassDoesAdvanceIndexedCommit pins). What
		// must NOT happen is advancing to a commit that landed DURING the pass,
		// whose files were never compared against anything.
		_ = diff.SaveManifestAtCommit(stateDir, manifest, passHeadShort, passHeadFull)
		endMSave()
		return Result{Done: true, Duration: time.Since(t0)}
	}

	// --- Step 3: AST-hash gate ---
	// Skip files where the content hash matches the last stamp (whitespace edits).
	//
	// #6209 — diff.RetryDue IS PART OF THIS GATE, not an optimisation on top of
	// it. This comparison is the SAME hex SHA-256 over the SAME raw bytes that
	// diff.isChanged already made (FileStamp.ContentHash and FileEntry.SHA256
	// are the same value), so it is a second, independent chance to drop a file
	// the change-detector let through. A file whose EXTRACTION failed has
	// unchanged bytes by construction — the failure does not edit it — so
	// without this clause the retry-due union in diff.FilterWithGit puts the
	// file into changedFiles and this gate immediately takes it back out,
	// reallyChanged empties, the pass returns Done=true without writing a
	// manifest, and the scheduler does not fall back. The failure count is then
	// pinned at its current value for the life of the file: the budget never
	// spends, the file is never retried, and #6209 is unfixed on the one path
	// the daemon actually runs.
	endHashGate := tr.span("ast-hash-gate")
	var reallyChanged []string
	for _, rel := range changedFiles {
		abs := filepath.Join(absRepo, filepath.FromSlash(rel))
		stamp, sErr := StampFile(abs)
		if sErr != nil {
			reallyChanged = append(reallyChanged, rel) // be conservative: re-extract on error
			continue
		}
		prev, ok := manifest.Files[rel]
		if !ok || prev.SHA256 != stamp.ContentHash || diff.RetryDue(prev) {
			reallyChanged = append(reallyChanged, rel)
		}
		// else: hash unchanged (whitespace-only) — skip silently
	}

	// Add deleted files to the reallyChanged set so their entities are pruned.
	// Deleted files always count as "really changed" — there's no AST hash to compare.
	// Note: manifest entries for deleted files were already removed during the
	// manifest-GC step above.
	reallyChanged = append(reallyChanged, deletedFiles...)
	endHashGate()
	tr.changedFiles = len(reallyChanged)

	if len(reallyChanged) == 0 {
		// All changes were whitespace-only (or only deletions already absent).
		logger.Printf("incremental: all %d changed file(s) had unchanged AST hash — skipping reindex", len(changedFiles))
		return Result{Done: true, Duration: time.Since(t0)}
	}

	// Re-check trigger limit after whitespace filtering.
	//
	// THIS BRANCH IS UNREACHABLE. Predates #6201; flagged during its review and
	// left in place only so the deletion can be filed on its own.
	//
	// Proof: the gate above established totalChanged <= limit, where
	// totalChanged = len(changedFiles) + len(deletedFiles). The AST-hash loop
	// ranges over changedFiles and appends each element at most once, so after
	// it len(reallyChanged) <= len(changedFiles); the deletedFiles append then
	// makes len(reallyChanged) <= totalChanged. Between the two gates nothing
	// reassigns limit, changedFiles, deletedFiles or totalChanged — the only
	// writes are those appends. Hence len(reallyChanged) <= limit always, and
	// this condition cannot hold. Confirmed empirically too: a panic at this
	// branch head survives internal/extractors/..., internal/indexer/diff/...
	// and the cmd/grafel integration suite (which drives TryIncremental
	// end-to-end) with every package still green.
	//
	// The call below is therefore dead code, NOT a live #5667/#5668 guard —
	// this comment previously claimed it was, which would have told the next
	// reader it runs. The live guard is the reachable branch above.
	if len(reallyChanged) > limit {
		// Same reasoning as the reachable reject above (#5822 ②) — kept in step
		// with it deliberately, so that if the proof of unreachability ever
		// stops holding this branch does not resurrect the defect. No test can
		// pin this, because no input reaches it.
		endMS := tr.span("manifest-scoped-restamp")
		diff.UpdateManifestScoped(absRepo, changedFiles, allFiles, manifest)
		endMS()
		endMSave := tr.span("manifest-save")
		_ = diff.SaveManifestAtCommit(stateDir, manifest, manifest.GitCommit, manifest.GitCommitFull)
		endMSave()
		logger.Printf("incremental: too-many-changed after-hash-gate files=%d limit=%d really=%v",
			len(reallyChanged), limit, samplePaths(reallyChanged))
		return fallback(t0, fmt.Sprintf("too-many-changed after-hash-gate files=%d limit=%d",
			len(reallyChanged), limit))
	}

	// --- Step 4: load existing graph ---
	endGraphLoad := tr.span("graph-materialise")
	doc, loadErr := graph.LoadGraphFromDir(stateDir)
	endGraphLoad()
	if loadErr != nil {
		// No existing graph → can't do incremental.
		return fallback(t0, "load-graph: "+loadErr.Error())
	}

	// --- Step 5: remove old entities + outbound rels for changed files ---
	tr.entities = len(doc.Entities)
	tr.rels = len(doc.Relationships)
	endPrune := tr.span("prune-scans")
	changedSet := make(map[string]bool, len(reallyChanged))
	for _, f := range reallyChanged {
		changedSet[f] = true
	}

	// Capture old entity property hashes for signature-change detection (#2170).
	// We record (qualifiedName → propertiesHash) before removal so that after
	// re-extraction we can detect entities whose Signature or Properties changed.
	oldEntityPropHash := make(map[string]string) // qualifiedName → hash
	for _, e := range doc.Entities {
		if changedSet[e.SourceFile] {
			oldEntityPropHash[entityPropKey(e)] = entityPropertiesHash(e)
		}
	}

	// Collect entity IDs sourced from changed files so we can also prune
	// their outbound relationships. Capture each removed entity's module key
	// too (#5309 layer 2): a fully-deleted file leaves no replacement entity in
	// newEntities, so its module would otherwise be invisible to the
	// affected-module set and its stale CONTAINS edges would survive.
	removedEntityIDs := make(map[string]bool)
	removedModuleKeys := make(map[module.ModuleKey]struct{})
	filteredEntities := doc.Entities[:0]
	for _, e := range doc.Entities {
		if changedSet[e.SourceFile] {
			removedEntityIDs[e.ID] = true
			if e.Kind != module.KindModule {
				removedModuleKeys[entityModuleKey(&e, doc.Repo)] = struct{}{}
			}
		} else {
			filteredEntities = append(filteredEntities, e)
		}
	}
	doc.Entities = filteredEntities

	// Remove outbound relationships from removed entities. Inbound edges
	// from surviving files to removed entities are handled below, after
	// re-extraction reveals which removed-entity IDs are actually re-emitted
	// (entity IDs are deterministic over kind/name/source_file, so re-extracting
	// a file usually re-creates entities with the same ID — keeping inbound
	// cross-file edges valid for free).
	// Track removed relationships so the incremental flow pass (#5309 layer 3)
	// can tell whether the blast radius touched a flow-input edge.
	var removedRels []graph.Relationship
	filteredRels := doc.Relationships[:0]
	for _, r := range doc.Relationships {
		if !removedEntityIDs[r.FromID] {
			filteredRels = append(filteredRels, r)
		} else {
			removedRels = append(removedRels, r)
		}
	}
	doc.Relationships = filteredRels

	// #6090 — snapshot the OUTBOUND prior edges just pruned, before the
	// inbound-dangling pass below starts appending to removedRels. These are the
	// edges the previous (corpus-wide) resolver had already bound; Step 7a
	// replays those bindings onto the fresh edges the isolated re-extraction
	// could not resolve.
	//
	// The three-index form is load-bearing: Step 6a appends the dangling INBOUND
	// edges to removedRels, and a two-index sub-slice would let those appends
	// write into the shared backing array within this snapshot's capacity. Capping
	// cap==len forces the append to copy, so the snapshot stays outbound-only.
	priorOutboundRels := removedRels[:len(removedRels):len(removedRels)]
	endPrune()

	// --- Step 6: re-extract each changed file ---
	cls := classifier.New(nil)

	// #6151 — the re-extraction below MUST parse, exactly as the full path does.
	//
	// This factory is the same one cmd/grafel/index.go:654 builds for the full
	// index, and the parse call below is a deliberate mirror of the full path's
	// (index.go:3441-3456). Before #6151 this loop passed TSTree: nil, and the
	// fourteen tree-sitter-backed extractors that `return nil, nil`
	// unconditionally on a nil tree (csharp, dockerfile, elixir, groovy, java,
	// javascript/typescript, kotlin, lua, php, proto, ruby, rust, scala, swift)
	// re-added nothing after Step 5 had already evicted the file's entities.
	// Total, silent data loss for the edited file, Done=true, no fallback.
	//
	// WHY PARSE HERE rather than teach those fourteen extractors to self-parse:
	//   - One site, not fourteen, and it closes the hole for the 15th extractor
	//     nobody has written yet. A per-extractor fallback only ever covers the
	//     extractors somebody remembered to change.
	//   - PARITY IS THE POINT. The correctness property incremental owes the
	//     caller is "same graph as a full reindex". Reusing the full path's own
	//     parse — same factory, same tsx override, same
	//     `perr == nil && pr != nil` acceptance test — makes the two paths agree
	//     BY CONSTRUCTION, including on the degenerate cases
	//     (ErrHighSyntaxErrorRate returns a nil tree; the full path passes nil
	//     there too, so both paths emit nothing and neither is lying).
	//     Fourteen bespoke self-parses would each be free to disagree.
	//   - Cost. A re-parse is not free (epic #5954), but it is bounded: this
	//     loop runs over reallyChanged, capped at defaultIncrementalFiles=20 /
	//     mainBranchIncrementalFiles=50, and ParserFactory.Parse goes through
	//     indexstate.AcquireParseSlot, so it cannot widen the daemon-wide parse
	//     ceiling. Peak RSS is bounded by ONE live tree at a time: the tree is
	//     Closed at the end of each iteration, not deferred to the end of the
	//     loop. The alternative — a full reindex — parses every file in the
	//     repo, so this is strictly cheaper than the path it prevents.
	parser := treesitter.NewParserFactory(nil)
	endExtract := tr.span("extract-changed-files")

	var newEntities []graph.Entity
	var newRels []graph.Relationship
	// walkStamps is the manifest entry for each re-extracted file, computed from
	// the bytes THIS loop reads (#6212). Step 9 stamps from these instead of
	// re-hashing the repo off disk.
	//
	// This is the daemon's primary path — TryIncremental's only non-test caller
	// is the scheduler — so it is where the defect actually bites: the window
	// from the read below to Step 9 spans the re-extraction, the scoped resolve,
	// the merge, the flow recompute, the canonical sort and all of
	// WriteGraphGen. A developer saving a file inside it had that save stamped
	// as indexed and then hash-matched away on the next pass.
	walkStamps := make(map[string]diff.FileEntry, len(reallyChanged))
	// #6148 — count of generic class records re-typed from the YAML rule sets,
	// logged with the run so a silent regression to zero is visible.
	classKindFolds := 0
	// #6150 — Pass-2.5 standalone relationships bound against the re-extracted
	// file's own records, and the ones no record of that file could bind.
	// Logged with the run: `dropped` climbing while `bound` stays at zero is the
	// signature of a rule set whose endpoints stopped naming in-file records,
	// which is silent in the graph (the rows simply are not there).
	pass25RelsBound, pass25RelsDropped := 0, 0
	// #6461 (MOUNT direction) — standalone Pass-2.5 relationships that
	// bindPass25StubEndpoints refused because one end names an entity OUTSIDE
	// the single file it had evidence about. Re-offered at Step 7c, once the
	// whole post-prune entity set is known.
	var deferredStubs []deferredStubRel
	pass25RelsLateBound := 0
	// #6150 — endpoint→handler outcomes for the re-extracted files.
	// `kept_unresolved` is the cross-file-handler count (#6159): those endpoints
	// survive with source_handler intact but without the IMPLEMENTS bridge, the
	// attribution or the response shape a full rebuild gives them. It is logged
	// because the alternative reading of the same situation — the endpoint being
	// DELETED — is invisible in a graph and was exactly the hazard the
	// file-scoped entry point exists to prevent.
	endpointsResolved, endpointsKeptUnresolved, endpointsSuperseded := 0, 0, 0
	// Endpoint synthetics already carried by the graph that SURVIVED Step 5's
	// eviction, keyed Kind|Name. Built once, from doc.Entities, which at this
	// point holds exactly the entities of the files NOT being re-extracted —
	// so a hit means "another file already owns this endpoint", which is the
	// evidence pruneSupersededEndpointSynthetics needs.
	survivingEndpoints := make(map[string]bool)
	for k := range doc.Entities {
		if e := &doc.Entities[k]; e.Kind == endpointSyntheticKind {
			survivingEndpoints[e.Kind+"|"+e.Name] = true
		}
	}
	// #6094 — one identity per (FromID, ToID, Kind), shared across every file in
	// this re-extraction batch. See convertExtractedRecords for why the triple is
	// a safe identity here and how this scope differs from buildDocument's.
	seenNewRel := make(map[string]bool)

	for _, rel := range reallyChanged {
		abs := filepath.Join(absRepo, filepath.FromSlash(rel))
		// Stat BEFORE the read — see diff.StampBytes for why that ordering is
		// load-bearing rather than incidental.
		statSize, statMtime := int64(-1), int64(0)
		if st, sErr := os.Stat(abs); sErr == nil {
			statSize = st.Size()
			statMtime = st.ModTime().UnixNano()
		}
		content, readErr := os.ReadFile(abs)
		if readErr != nil {
			// File deleted → nothing to extract; entities were already removed.
			// Deliberately UNSTAMPED: with no bytes there is no evidence of what
			// was indexed. A deleted file leaves allFiles and is pruned by
			// ApplyStamps; a transiently unreadable one keeps its old stamp and
			// re-presents as changed. Over-reporting, never staleness.
			logger.Printf("incremental: %s deleted or unreadable — entities removed", rel)
			continue
		}

		// Stamp HERE, on the successful read, and not at any of the `continue`s
		// below (#6212). These are the bytes this pass saw, whatever the
		// pipeline went on to do with them — and every skip below is a file that
		// genuinely CHANGED, so it still needs a refreshed stamp or it
		// re-presents as changed on every pass, counts against the
		// too-many-changed limit and pins the daemon in a reindex loop (#5668).
		walkStamps[rel] = diff.StampBytes(content, statSize, statMtime)

		// Classify to get language.
		cr := cls.ClassifyWithSize(ctx, rel, int64(len(content)))
		if cr.Skip || cr.Language == "" {
			logger.Printf("incremental: %s — classifier returned no language, skipping", rel)
			continue
		}

		ext, ok := Get(cr.Language)
		if !ok {
			logger.Printf("incremental: no extractor for language=%s file=%s", cr.Language, rel)
			continue
		}

		input := extractor.FileInput{
			Path:     rel,
			Content:  content,
			Language: cr.Language,
			RepoRoot: absRepo,
		}

		// #6151 — supply a real tree. Mirrors cmd/grafel/index.go:3441-3456.
		//
		// THIS BLOCK AND THE FULL PATH'S ARE HAND-COPIED, NOT A SHARED HELPER,
		// and they are already NOT identical: the full path additionally sets
		// `Config: &extractorCfg` (#2320, Config-first/env-fallback precedence)
		// and folds pr.ErrorRatio into the #5414 parse canary. Both omissions
		// are pre-existing and outside #6151, but nothing structural stops the
		// two from drifting further. If you touch either, touch both — or lift
		// them into one helper and delete this paragraph.
		//
		// The PLT #537 tsx override below is carried for PARITY, which is the
		// whole correctness argument of this fix. It is NOT load-bearing against
		// #6151, and an earlier version of this comment claiming otherwise was
		// wrong: measured, a one-line JSX sample parses at a 0.0400 error ratio
		// under the plain `typescript` grammar and a realistic React file at
		// 0.0465 with a usable 452-node tree — nowhere near the 10% ceiling that
		// would produce a nil tree. Removing the override does not reintroduce
		// the bug. Keep it because it improves tree fidelity and because the two
		// paths must agree, not because a .tsx file would otherwise be silently
		// emptied.
		parseLang := cr.Language
		if parseLang == "typescript" || parseLang == "javascript" {
			low := strings.ToLower(rel)
			if strings.HasSuffix(low, ".tsx") || strings.HasSuffix(low, ".jsx") {
				parseLang = "tsx"
			}
		}
		// treeRequired: this language HAS a tree-sitter grammar. That is all it
		// means, and the name overstates it — read it as treeAvailable.
		// ErrUnsupportedLanguage is the reliable half: a language with no grammar
		// is a pure source scanner that never wanted a tree, where nil is the
		// normal, correct input and must not raise an alarm.
		//
		// The converse does NOT hold, and the over-approximation is deliberate.
		// Having a grammar does not imply the extractor consumes one:
		// internal/extractors/sql/sql.go never references TSTree at all, yet
		// "sql" is in treesitter's migratedLanguages. So a .sql file whose
		// content happens not to parse falls back to a full reindex that
		// computes the same empty answer — measured, and ocaml has the same
		// shape. That is a wasted reindex, not a wrong graph, and the
		// alternative (a hand-maintained list of extractors that truly need a
		// tree) is exactly the per-extractor bookkeeping this fix exists to
		// avoid getting wrong.
		treeRequired := true
		pr, perr := parser.Parse(ctx, content, parseLang)
		if perr != nil && errors.Is(perr, treesitter.ErrUnsupportedLanguage) {
			treeRequired = false
		}
		if perr == nil && pr != nil {
			input.TSTree = pr.TSTree
		}
		gotTree := input.TSTree != nil

		// #6151 — safeExtract, not ext.Extract. TryIncremental was the only
		// Extract call site in the tree that bypassed the recover() in
		// registry.go:103-109. No extractor panics on a nil tree today, but the
		// daemon runs this on a watcher tick, and a panic here would take down a
		// path whose whole job is to not lose the user's graph.
		records, extErr := safeExtract(ctx, ext, input)
		if input.TSTree != nil {
			// Release the CGo allocation now rather than deferring to the end of
			// the loop: peak RSS must stay at one live tree, not len(reallyChanged).
			input.TSTree.Close()
			input.TSTree = nil
		}
		if extErr != nil {
			// #6151 — NOT non-fatal, and no longer swallowed. Step 5 has already
			// evicted this file's entities ~65 lines above. "Use partial results"
			// on an error meant persisting a graph with the file's entities
			// deleted and its replacements missing, and still reporting Done=true.
			// A failed extraction is exactly what fallback() exists for: the full
			// reindex reconciles the file from scratch, and the reason is logged.
			logger.Printf("incremental: extract %s: %v", rel, extErr)
			// Close the span before leaving it (#6199). This fallback has paid
			// the whole extraction and is about to throw it away, so it is one
			// of the two passes where the extract cost most needs measuring;
			// returning through an open span reported no extract phase at all
			// and left accounted_ms short of total_ms by the dominant phase.
			endExtract()
			return fallback(t0, fmt.Sprintf("extract-error file=%s: %v", rel, extErr))
		}
		// An empty file is excluded deliberately: Parse returns a zero-node
		// result with a nil tree and NO error for empty input, which is the
		// full path's behaviour too. Truncating a file to zero bytes is a real
		// edit with a real (empty) answer, not a failed parse.
		if len(records) == 0 && treeRequired && !gotTree && len(content) > 0 {
			// #6151 — the two failure modes land in the same place, so neither
			// half of this guard is sufficient alone.
			//
			// len(records)==0 on its own cannot distinguish a file that
			// legitimately has no entities from an extractor that bailed, which is
			// why it is paired with the PARSE OUTCOME: we asked for a tree for a
			// language that has a grammar, and did not get one (a malformed file
			// over the #5963 error-ratio ceiling, or a grammar failure). Every one
			// of the fourteen returns (nil, nil) in exactly that state.
			//
			// A full reindex would also emit nothing for such a file, so this
			// costs a redundant reindex in the malformed-file case. THAT COST WAS
			// MEASURED AND IS NEAR ZERO — do not "optimise" this guard away on a
			// hunch that mid-edit files trip it:
			//   - across grafel's own checked-in corpus, 0 of 5863 files in 15
			//     languages produced an unusable tree (0.00%);
			//   - five realistic mid-edit Kotlin states — unclosed brace,
			//     half-typed `fun`, dangling `.`, a `clas` typo, and unresolved
			//     git conflict markers — ALL parsed and returned done=true with no
			//     fallback. Tree-sitter's error recovery is why.
			// The fixture that exercises this branch needs hand-made dense
			// garbage measured at a 12.8% error ratio. Real editing does not
			// reach here.
			logger.Printf("incremental: %s — no records and no usable parse tree (lang=%s): %v",
				rel, parseLang, perr)
			endExtract() // #6199 — see the extract-error return above.
			return fallback(t0, fmt.Sprintf("no-tree-no-records file=%s lang=%s", rel, parseLang))
		}

		// #6148 / #6150 — run Pass 2.5 over this file and reconcile it with the
		// extractor's records.
		//
		// The per-language extractor emits every class as a generic
		// SCOPE.Component, and emits nothing at all for a framework construct
		// that is not a language declaration. On the full path Pass 2.5
		// (engine.Detector over the YAML rule sets) supplies both: a
		// framework-typed record — Controller / Model / View / Service / … — for
		// each class symbol a rule matches, which the #1613 fold collapses the
		// generic node into, plus standalone records for the constructs that have
		// no declaration behind them (a Route from a responder method, a Service
		// from an app object).
		//
		// This path ran the extractor alone. A class in a CHANGED file therefore
		// kept the generic kind while the identical class in an unchanged file
		// kept the typed kind it was carried forward with — the same tree, two
		// answers, decided only by which files the delta touched — and the
		// framework-only records were dropped outright. Entity ids hash the kind,
		// so the class divergence moved every edge incident on it too.
		//
		// The fold is keyed by (source_file, name) and never pairs records across
		// files, so applying it per re-extracted file is the whole of it.
		//
		// Detect's standalone RELATIONSHIPS are consumed here too (#6150), but
		// only after being BOUND — see bindPass25StubEndpoints. They arrive as
		// `Kind:Name` structural refs that the full path binds in a corpus-wide
		// resolver pass this path does not run, and emitting them raw was
		// measured to land `«unbound»Controller:X → «unbound»Service:y` rows
		// plus duplicate DEPENDS_ON: strictly worse than the gap. Binding them
		// against THIS FILE's own records — and dropping the ones that do not
		// bind — is what turns them into an improvement. A YAML relationship_rule
		// matches one line of one file, so both of its endpoints naming records
		// of that file is the ordinary case, not a lucky one.
		//
		// The two endpoint-enrichment passes between the fold and the bind are
		// the full path's, in the full path's order:
		//
		//	Pass 2.7  engine.ApplyResponseShapesCorpus — reads the handler body
		//	          to stamp response_keys / status_codes / response_keys_known
		//	          onto the endpoint. MUST run before the handler resolve
		//	          below, which clears the `source_handler` property it
		//	          navigates by.
		//	Pass 2.8  engine.ResolveHTTPEndpointHandlersFileScoped — binds an
		//	          http_endpoint_definition to its handler: emits the
		//	          IMPLEMENTS bridge, rebinds the endpoint's coordinates onto
		//	          the handler body and stamps attribution="handler_resolved".
		//
		// FILE-SCOPED, and the distinction is not a nicety. The corpus entry
		// point (ResolveHTTPEndpointHandlersWithRepo, what buildDocument calls)
		// DROPS a synthetic whose source_handler matches nothing in the slice —
		// correct when the slice is the whole corpus, catastrophic when it is
		// one file, because then "no candidate anywhere" and "the handler is in
		// another file" become the same condition and every Express-router /
		// Flask-add_url_rule / DRF-router endpoint in the delta is DELETED
		// rather than left unenriched. The file-scoped entry point keeps them,
		// source_handler intact, and counts them in HandlerUnresolvedKept. See
		// its doc comment for why that is the same verdict #2851 and #3426
		// already reach inside the same function.
		//
		// THE REMAINING DIVERGENCE, precisely. A cross-file handler is KEPT but
		// not ENRICHED: no IMPLEMENTS bridge, no attribution, and no response
		// shape (the reader below serves only the file being re-extracted, and
		// the shape pass needs the handler's body). A full rebuild resolves it.
		// That gap is #6159, allow-listed on the parity gate — it is a smaller
		// gap than the deletion above, and unlike the deletion it is not a
		// regression: before this change the endpoint carried none of those
		// properties either. Closing it needs the previous graph's entities as
		// resolution candidates, which is a materially larger change than
		// reconciling one re-extracted file with itself.
		//
		// CANONICAL ORDER FIRST. Both passes disambiguate by FIRST-WRITER-WINS
		// over the slice they are handed (ApplyResponseShapesCorpus' handler
		// index; the resolver's globalIdx / globalMulti / sameFileBareIdx), and
		// the resolver's doc makes it a documented precondition: "`merged` MUST
		// already be sorted in canonical order (#481)". buildDocument satisfies
		// it with sortEntityRecords; the fold's output does NOT — it is
		// deliberately "the extractor's records first, then Detect's". Without
		// this sort the precondition is simply unmet, and with two same-file
		// candidates the two paths can bind different handlers. Sorting AFTER
		// the fold is deliberate: the fold's own last-resort tiebreak reads that
		// emission order, so sorting before it would change which record wins a
		// three-way tie.
		//
		// NOT COVERED END-TO-END, and it was attempted. Deleting this line
		// passes both ./internal/extractors/... and ./cmd/grafel/ — including a
		// fixture built specifically to break it (two same-named `def`s plus a
		// registration naming them, in one re-extracted file: identical
		// divergence sets with and without the sort). The reason is structural:
		// the competing records here come from a per-language extractor, which
		// emits in FILE order, and file order already coincides with canonical
		// order for records sharing (Kind, Name, SourceFile) — StartLine is the
		// tiebreak. Reaching a difference needs a producer that emits two
		// same-key records out of line order, which nothing on this path does
		// today. The guarantee is pinned at unit level instead, as a
		// permutation invariant over the resolve
		// (TestResolveHTTPEndpointHandlersFileScoped_CanonicalOrderMakesTheBindStable),
		// where the order-dependence IS demonstrated: line 10 vs line 50.
		//
		// That probe also found a real defect and it is filed as #6161 — this
		// path has no entity-identity gate, so the two same-named records
		// became two graph rows with one id where a full rebuild emits one.
		if det := frameworkDetector(); det != nil {
			if fwRes, dErr := det.Detect(ctx, input); dErr != nil {
				logger.Printf("incremental: framework detect %s: %v", rel, dErr)
			} else if fwRes != nil {
				var n int
				records, n = engine.FoldFrameworkClassKinds(records, fwRes.Entities)
				classKindFolds += n

				types.SortEntityRecordsCanonical(records)

				fileContent := content
				engine.ApplyResponseShapesCorpus(records, fwRes.Relationships,
					func(p string) []byte {
						if p == rel {
							return fileContent
						}
						return nil
					})

				// OWNERSHIP: this pass consumes `records` and compacts over its
				// backing array — read only the returned slice (see its doc).
				var hStats engine.ResolveHTTPEndpointStats
				records, hStats = engine.ResolveHTTPEndpointHandlersFileScoped(records, doc.Repo)
				endpointsResolved += hStats.HandlerResolved
				endpointsKeptUnresolved += hStats.HandlerUnresolvedKept

				// Ordering note: this runs BEFORE the stub bind below, so a
				// standalone Pass-2.5 relationship whose target is a synthetic
				// pruned here can no longer bind and is counted in
				// pass25_rels_dropped rather than in endpoints_superseded. That
				// is the right net behaviour — the edge would otherwise point
				// at a record that is not being emitted — but the two counters
				// do not add up to a partition of the work, and reading
				// pass25_rels_dropped as "rule sets stopped naming in-file
				// records" is wrong by exactly that amount.
				var superseded int
				records, superseded = pruneSupersededEndpointSynthetics(records, survivingEndpoints)
				endpointsSuperseded += superseded

				var bound int
				var unbindable []types.RelationshipRecord
				records, bound, unbindable = bindPass25StubEndpoints(records, fwRes.Relationships, doc.Repo)
				pass25RelsBound += bound
				pass25RelsDropped += len(unbindable)
				for _, u := range unbindable {
					deferredStubs = append(deferredStubs, deferredStubRel{rel: u, file: rel})
				}
			}
		}

		// Convert types.EntityRecord → graph.Entity (same logic as buildDocument).
		ents, rels := convertExtractedRecords(records, doc.Repo, seenNewRel)
		newEntities = append(newEntities, ents...)
		newRels = append(newRels, rels...)
	}

	// --- Step 6a: inbound-dangling prune (#2719) ---
	// Now that we know which entity IDs are coming back via re-extraction,
	// drop inbound edges to removed entities whose ID is NOT among the
	// re-extracted set. Without this pass, deleting an entity (or renaming it
	// such that its EntityID changes) leaves stale inbound edges pointing at
	// nothing — invisible orphans until a full reindex sweeps them up.
	// Entities re-extracted with the same ID keep their inbound edges intact
	// (entity IDs are deterministic over kind/name/source_file), which
	// preserves the carefully-resolved cross-file CALLS / REFERENCES edges
	// that other files asserted into the previous graph.
	endExtract()
	endPrune2 := tr.span("prune-scans")
	reEmittedIDs := make(map[string]bool, len(newEntities))
	for _, e := range newEntities {
		reEmittedIDs[e.ID] = true
	}
	prunedInbound := doc.Relationships[:0]
	for _, r := range doc.Relationships {
		if removedEntityIDs[r.ToID] && !reEmittedIDs[r.ToID] {
			removedRels = append(removedRels, r) // truly removed → drop the dangling inbound edge
			continue
		}
		prunedInbound = append(prunedInbound, r)
	}
	doc.Relationships = prunedInbound
	endPrune2()

	// --- Step 6b: signature-change detection (#2170) ---
	// For each newly extracted entity, compare its properties hash against the
	// old hash. Entities with changed signatures (arity, parameter types,
	// exported-ness) are collected; we will pass them to the scoped resolver so
	// it can re-resolve inbound CALLS/REFERENCES edges rather than triggering
	// the safety-net full-reindex fallback.
	var signatureChangedIDs []string
	for _, e := range newEntities {
		key := entityPropKey(e)
		oldHash, existed := oldEntityPropHash[key]
		if existed && oldHash != entityPropertiesHash(e) {
			signatureChangedIDs = append(signatureChangedIDs, e.ID)
			logger.Printf("incremental: signature-change detected entity=%s file=%s", e.QualifiedName, e.SourceFile)
		}
	}

	// --- Step 7: scoped resolver pass ---
	// Re-resolve inbound cross-file relationships targeting the newly
	// extracted entities. Uses a lightweight name-index over the full
	// (surviving) entity set.
	//
	// When signature changes are detected, pass them to the resolver so it can
	// re-resolve inbound CALLS/REFERENCES edges for those entities rather than
	// triggering the safety-net fallback (#2170).
	endScoped := tr.span("scoped-resolve")
	scopedResult := sresolver.ResolveScoped(
		newEntities,
		doc.Entities, // existing surviving entities
		newRels,
		doc.Relationships,
		logger,
		sresolver.WithSignatureChangedIDs(signatureChangedIDs),
	)
	endScoped()
	if scopedResult.FallbackRequired {
		logger.Printf("incremental: fallback reason=unresolved-rel target=%s", scopedResult.UnresolvedTarget)
		return fallback(t0, "unresolved-rel target="+scopedResult.UnresolvedTarget)
	}
	// #6033: the two slices have different merge semantics and must not be
	// conflated. UpdatedExistingRelationships is the COMPLETE surviving edge set
	// with the resolver's inbound fixes applied in place — it REPLACES
	// doc.Relationships (appending it would duplicate every survivor, which is
	// exactly what #6033 was: multiplicity 2, 4, 8, 16 … per pass, and stale
	// unbound stub edges left alongside their rewired copies).
	// ResolvedNewRelationships holds only the genuinely new edges; those are
	// appended at Step 8 and are also the correct blast-radius input for the
	// flow / affected-module passes below.
	doc.Relationships = scopedResult.UpdatedExistingRelationships
	newRels = scopedResult.ResolvedNewRelationships

	// --- Step 7a: prior-resolution replay (#6090) ---
	// The scoped resolver's binding ladder does not cover what the corpus-wide
	// one does (it has no member-suffix tier, so entity "T11.Do11" is unreachable
	// from the call-site stub "Do11"), so an outgoing edge of the changed file
	// can come back from this isolated pass with an unresolved bare-name ToID
	// even though a full rebuild binds it. Not a strict subset in the other
	// direction either: the scoped ladder's whole-string tier is last-writer-wins
	// with no ambiguity sentinel, so it can bind where the full resolver
	// deliberately refuses. Step 5 already pruned the prior, RESOLVED copy of
	// that same edge (it was outbound from a re-extracted entity), so the edge
	// is lost until the next full reindex — and the loss is monotone across
	// edits to different files.
	//
	// Replay the previous graph's binding onto the fresh edge. See
	// replayPriorResolution for why this cannot resurrect a deleted call.
	endReplay := tr.span("prior-resolution-replay")
	healed := replayPriorResolution(newRels, priorOutboundRels, doc.Entities, newEntities)
	endReplay()
	if healed > 0 {
		logger.Printf("incremental: prior-resolution replay bound %d unresolved edge endpoint(s) (#6090)", healed)
	}

	// --- Step 7b: cross-file composition (#6461) ────────────────────────────
	// The full path's Pass 2.6 (cmd/grafel/index.go's runDjangoNestedURLConf →
	// engine.ApplyDjangoNestedURLConf) composes an endpoint out of TWO files: a
	// mount file's `path("prefix", include("app.urls"))` and the included
	// file's route declarations. This path ran no equivalent, and Step 5 prunes
	// only by SourceFile — so a composed endpoint, which is attributed to
	// neither of the files whose content produced it (the handler resolver
	// rebinds it onto the HANDLER's file, #2678), was never pruned and never
	// re-derived. Editing the route file left the PRE-EDIT composition in the
	// graph as a ghost.
	//
	// Recompute and reconcile rather than trying to invalidate: the composition
	// is a pure function of the tree, so what it produces IS what a full
	// rebuild holds, and the verdict does not depend on which file was edited.
	// The pass gates itself on "a Python file changed AND the repo has a
	// *urls.py", so a non-Django repo pays one suffix scan.
	//
	// Placed HERE — after the scoped resolver, before the module stamp — for
	// two reasons: the composed entities must be in `newEntities` when
	// stampModuleOnEntities runs (a full rebuild's Pass 8 sees them), and they
	// must be in the blast radius fed to RunFlowsIncremental and
	// affectedModuleSet below, so their SCOPE.Process / CONTAINS layer is
	// re-derived too.
	endCompose := tr.span("cross-file-compose")
	compose := recomposeDjangoURLConf(absRepo, allFiles, reallyChanged, doc, newEntities, logger)
	if len(compose.removedIDs) > 0 {
		survivors := doc.Entities[:0]
		for _, e := range doc.Entities {
			if compose.removedIDs[e.ID] {
				removedEntityIDs[e.ID] = true
				if e.Kind != module.KindModule {
					removedModuleKeys[entityModuleKey(&e, doc.Repo)] = struct{}{}
				}
				continue
			}
			survivors = append(survivors, e)
		}
		doc.Entities = survivors
		// Both directions — see djangoComposeResult.prunesRel, which owns that
		// rule and is pinned by TestDjangoComposeResult_PrunesRelBothDirections.
		keptRels := doc.Relationships[:0]
		for _, r := range doc.Relationships {
			if compose.prunesRel(&r) {
				removedRels = append(removedRels, r)
				continue
			}
			keptRels = append(keptRels, r)
		}
		doc.Relationships = keptRels
	}
	if len(compose.added) > 0 {
		newEntities = append(newEntities, compose.added...)
	}
	if len(compose.addedRels) > 0 {
		newRels = append(newRels, compose.addedRels...)
	}
	endCompose()

	// --- Step 7c: cross-file Pass-2.5 stub bind (#6461, MOUNT direction) ─────
	// Step 7b above fixes the ROUTE-edit direction for COMPOSED ENTITIES. The
	// MOUNT-edit direction fails one level down, on an EDGE, and for the same
	// root cause stated the other way round: pruning is by SourceFile, but a
	// Pass-2.5 standalone relationship emitted while re-extracting the CHANGED
	// file can name a target attributed to a file that was NOT edited. That
	// target is not in the re-extracted records, so bindPass25StubEndpoints —
	// which deliberately has evidence about ONE file only — refuses it and the
	// edge is dropped. Measured on the #6461 mount fixture: editing only
	// `mpmain_mount.py` gave `pass25_rels_bound=0 pass25_rels_dropped=4` and
	// lost `Service/mp_app@mpmain_mount.py → Route/mp_router@mpmarkets_route.py
	// :ROUTES_TO`, an edge a full rebuild holds.
	//
	// Here the whole post-prune entity set IS known — survivors plus everything
	// re-extracted — which is the same evidence base the full path's corpus
	// resolver works from, so binding the target across files is no longer a
	// guess. Two refusals are kept, and they are what stops this from becoming
	// the "wrong bind is worse than a missing row" failure (#6123) the
	// file-scoped binder warns about:
	//
	//   - the TARGET must be globally UNIQUE by `Kind:Name` over that set. An
	//     ambiguous key has no tiebreak here any more than it did in-file.
	//   - the SOURCE must resolve inside the CHANGED file that emitted the stub
	//     — never corpus-wide. Two reasons, and the second is the load-bearing
	//     one: the rule fired on that file, so that file is the only evidence
	//     for the source end; and the resulting edge is owned by a re-extracted
	//     entity, so the FromID-keyed stale-edge eviction (#6094) reclaims it on
	//     the next edit of that file. An edge hung off an UNCHANGED file's
	//     entity would never be evicted and would outlive the rule that made it.
	//
	// ORDERING IS LOAD-BEARING: this MUST run AFTER Step 7b, and the constraint
	// is enforced, not merely documented. Step 7b deletes entities
	// (`compose.removedIDs`) and their edges (`compose.prunesRel`), but the edge
	// filter walks `doc.Relationships` ONLY — never `newRels`. Run this pass
	// first and its target index is built over the PRE-prune entity set, so a
	// stub can bind to an entity 7b is about to delete; the edge then lands in
	// `newRels`, is appended at Step 8 AFTER the relationship prune has already
	// happened, and reaches graph.fb dangling — a row no full rebuild holds.
	// Hoisting the pass would also lose `compose.added` from the target index.
	// `compose.removedIDs` is therefore passed in and refused explicitly: a
	// reordering has to defeat a check to become a defect instead of silently
	// being one. Pinned by
	// TestBindDeferredPass25Stubs_RefusesComposeRemovedTarget.
	endLateBind := tr.span("pass25-late-bind")
	if len(deferredStubs) > 0 {
		lateRels := bindDeferredPass25Stubs(deferredStubs, doc.Entities, newEntities, doc.Relationships, newRels, compose.removedIDs)
		if len(lateRels) > 0 {
			pass25RelsLateBound = len(lateRels)
			pass25RelsDropped -= len(lateRels)
			newRels = append(newRels, lateRels...)
			logger.Printf("incremental: pass2.5 cross-file late-bind rescued %d relationship(s) (#6461)", len(lateRels))
		}
	}
	endLateBind()

	// --- Step 7a: stamp Properties["module"] on new entities (#5309 layer 2) ---
	// The full-rebuild path stamps every sourced entity with a deterministic
	// module label (cmd/grafel/index.go buildDocument) BEFORE the module-agg
	// pass. The incremental path's entityRecordToGraphEntity carries only the
	// extractor-supplied properties, so freshly extracted entities arrive with
	// no "module" key. Stamp them here using the SAME label rule the full path
	// uses — single-module label for a plain repo, else the path-rollup over the
	// package-boundary markers — so the module layer rebuilt below is
	// byte-equivalent to a full rebuild. (Surviving entities keep the label they
	// were stamped with on the previous build.)
	endStamp := tr.span("module-stamp")
	stampModuleOnEntities(newEntities, doc, absRepo, allFiles)
	endStamp()

	// --- Step 8: merge + sort + write ---
	// #6161 — fold, do not append. See mergeEntitiesDeduped.
	doc.Entities = mergeEntitiesDeduped(doc.Entities, newEntities)
	doc.Relationships = append(doc.Relationships, newRels...)

	// --- Step 8·flows: incremental per-repo flow passes (#5309 layer 3) ───────
	// The full path runs RunProcessFlow (Pass 7) + RunEventFlow (Pass 7.5) over
	// the finalized graph, BEFORE module-aggregation (Pass 8). The incremental
	// path previously skipped both, carrying the prior build's Process /
	// EventFlow entities + their ENTRY_POINT_OF / STEP_IN_PROCESS /
	// SEED_OF_EVENT_FLOW / STEP_IN_EVENT_FLOW edges forward — which a code change
	// can staleify. engine.RunFlowsIncremental is blast-radius-scoped: when the
	// change cannot touch a flow input (CALLS / FETCHES / HTTP-boundary / pub-sub
	// edges or any flow-relevant entity — e.g. docs/comment-only changes) the
	// prior flows are already byte-equivalent to a full rebuild and are kept
	// verbatim; otherwise the stale flows are stripped and both walkers re-run
	// over the finalized graph, reproducing exactly what a full rebuild emits.
	//
	// Run BEFORE module-aggregation so the ordering matches the full path: a full
	// rebuild's Pass 8 sees the Process / EventFlow entities Pass 7 emitted and
	// folds them into the module layer (a CONTAINS edge from the `_external`
	// Module node for each). Capture the flow-emitted entities/edges and feed them
	// into the affected-module set so that module layer is re-derived too.
	endFlows := tr.span("flow-recompute")
	flowsRecomputed, flowEntities, flowRels := engine.RunFlowsIncremental(doc, newEntities, removedEntityIDs, newRels, removedRels)
	endFlows()

	// --- Step 8a: incremental module-aggregation (#5309 layer 2) ─────────────
	// The full path runs module.Aggregate (CONTAINS / DEPENDS_ON + Module nodes)
	// as Pass 8 over the finalized graph. The incremental path carries the prior
	// build's module layer forward in doc, which a file change leaves stale:
	// CONTAINS edges to removed entities, Module nodes whose members vanished,
	// DEPENDS_ON weights that moved. Re-run the aggregation scoped to the modules
	// whose membership or cross-module dependencies changed — every other
	// module's nodes/edges are preserved verbatim. The result is byte-equivalent
	// to a full rebuild's module layer without re-aggregating the whole graph.
	//
	// The freshly (re)emitted flow entities/edges (#5309 layer 3) join the
	// affected set so their `_external` Module node + CONTAINS edges are
	// (re-)derived exactly as a full rebuild's Pass 8 would, and so a flow strip
	// that removed the last member of a module triggers that module's re-derive.
	//
	// #6033: the SURVIVOR edges the scoped resolver rewrote must join the blast
	// radius too. Binding an inbound stub ToID can make a cross-module
	// DEPENDS_ON derivable that the previous build could not see — a full build
	// leaves X→"foo" unresolved, so aggregateModules skips it and emits no
	// M2→M3 edge; once this pass binds "foo" to Y in M3 that edge exists, yet
	// neither M2 nor M3 is in the changed file's module set. Without this the
	// module layer silently diverges from a full rebuild until something else
	// happens to touch M2 or M3.
	//
	// These are a blast-radius SIGNAL only: every one of them is already in
	// doc.Relationships via UpdatedExistingRelationships, so they must never be
	// appended to the document (that is exactly the #6033 duplication). Hence
	// they are folded in HERE and not into `newRels`.
	endAgg := tr.span("module-aggregate")
	aggNewEnts := append(append([]graph.Entity(nil), newEntities...), flowEntities...)
	aggNewRels := append(append([]graph.Relationship(nil), newRels...), flowRels...)
	aggNewRels = append(aggNewRels, scopedResult.MutatedExistingRelationships...)
	affectedModules := affectedModuleSet(doc, removedModuleKeys, aggNewEnts, aggNewRels)
	module.AggregateIncremental(doc, affectedModules)
	endAgg()

	// --- Step 8a.9: lib-boundary re-stamp (#5309 layer 3) ────────────────────
	// The full path's Pass 8.9 (engine.ApplyLibBoundary) classifies every
	// DEPENDS_ON edge first_party/third_party from the locality/kind props the
	// extractors already attached. It runs AFTER module-aggregation (the agg
	// pass emits fresh Module→Module DEPENDS_ON edges carrying only a `weight`
	// prop). The incremental path's module-agg likewise emits unstamped
	// DEPENDS_ON edges, so the `boundary` property must be (re)applied here or
	// the freshly (re)emitted edges diverge from a full rebuild — surfaced once
	// the flow Process entities (Pass 7) introduce new `_external`→first-party
	// DEPENDS_ON pairs. The pass is deterministic, idempotent and bounded by the
	// DEPENDS_ON edge count (a pure function of the now-finalized edge set).
	endLib := tr.span("lib-boundary")
	engine.ApplyLibBoundary(doc)
	endLib()

	// --- Step 8a': structural coupling re-stamp (#5309 layer 2) ──────────────
	// The full path's Pass 8.6 (engine.ApplyStructuralCoupling) annotates each
	// Module node with afferent/efferent coupling + instability derived from the
	// DEPENDS_ON edges module-agg just (re)emitted. It is bounded by the module
	// count (not the entity count) and is a pure function of the module graph, so
	// re-running it over the corrected DEPENDS_ON set lands the same ca/ce/
	// instability/coupling_computed properties a full rebuild would — without
	// which freshly re-emitted Module nodes would carry no coupling props and
	// survivors could carry stale ones.
	endCoup := tr.span("structural-coupling")
	engine.ApplyStructuralCoupling(doc)
	endCoup()

	// --- Step 8b: static test-reachability re-stamp (#5309 layer 2) ──────────
	// coverage.Enrich's reachability sub-pass stamps test_reachable /
	// reaching_tests / reach_depth onto production entities from the in-graph
	// TESTS+CALLS edges. It is a deterministic function of the (now finalized)
	// graph with no external dependency, so re-running it after the merge lands
	// the same property set a full rebuild would. New entities get stamped and
	// survivors are refreshed in case a changed edge moved their reachability.
	endCov := tr.span("coverage-bfs")
	coverage.Enrich(doc, absRepo, coverage.Config{})
	endCov()

	// #2706 — belt-and-suspenders prune of Django migration entities.
	// The incremental path bypasses the per-extractor prune gates only
	// indirectly (it calls extractor.Extract which respects them), but the
	// merged `doc.Entities` slice includes survivors carried forward from
	// the previous on-disk graph. If a previous build slipped any migration
	// entities through (e.g. before the per-extractor prune existed, or via
	// a new emission path) they would survive here forever. The central
	// sweep keeps the incremental and full-rebuild paths in lockstep.
	endMig := tr.span("migration-prune")
	ePruned, rPruned := PruneMigrationEntities(doc)
	endMig()
	if ePruned > 0 {
		logger.Printf("incremental: migration-prune dropped %d entities + %d relationships", ePruned, rPruned)
	}

	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)
	doc.GeneratedAt = time.Now().UTC()

	tr.entities = len(doc.Entities)
	tr.rels = len(doc.Relationships)
	endSort := tr.span("canonical-sort")
	sortGraphDocumentForEmission(doc)
	endSort()

	// #5891 gen layout: write graph.<gen>.fb + flip the `current` pointer
	// instead of overwriting graph.fb. This never renames over a possibly-
	// mapped graph.fb (Windows ERROR_USER_MAPPED_FILE). fbPath is the gen
	// file written, passed to the directory-keyed sidecar writer below.
	endWrite := tr.span("graph-remarshal-write")
	fbPath, undeclaredKinds, writeErr := writeGraphGen(stateDir, doc)
	endWrite()
	if writeErr != nil {
		return fallback(t0, "write-graph-fb: "+writeErr.Error())
	}

	// #5442 — refresh the graph-stats.json sidecar so the dashboard group
	// overview and `grafel status` report this repo's real entity count and a
	// real last-indexed time when the group is cold (not loaded in memory).
	// The incremental path does not run Pass-4 graph-algo, so community /
	// modularity / god-node fields are omitted; the counts + timestamp are
	// what those surfaces read. Best-effort: a sidecar write failure never
	// fails the reindex (graph.fb is already written, and the fbreader-header
	// fallback still recovers the counts on a sidecar miss).
	// #5692 — persist the extraction phase wall-clock (extract_ms) so `grafel
	// feedback` can report where incremental reindex time goes. t0 marks the
	// start of this incremental pass; by here the re-extract + scoped resolve +
	// merge are done. Cross-repo link timing lives in a separate link-stats.json
	// owned by the link pass, so nothing is carried forward here.
	side := &graph.GraphStatsSidecar{
		Version:            1,
		ComputedAt:         doc.GeneratedAt,
		TotalFiles:         doc.Stats.Files,
		TotalEntities:      doc.Stats.Entities,
		TotalRelationships: doc.Stats.Relationships,
		ExtractMS:          time.Since(t0).Milliseconds(),
	}
	// #6757 arm C — FRESH, not carried forward (unlike UnsupportedExtensions
	// below). This pass re-serializes the WHOLE document, so every
	// relationship in the graph went back through the write path and this
	// tally is complete for the graph just written. Leaving the fields unset
	// would report a graph full of undeclared kinds as clean.
	side.UndeclaredRelationshipEdges = undeclaredKinds.Edges
	side.UndeclaredRelationshipKindCount = undeclaredKinds.DistinctKinds
	if len(undeclaredKinds.Kinds) > 0 {
		kinds := make(map[string]int, len(undeclaredKinds.Kinds))
		for _, k := range undeclaredKinds.Kinds {
			kinds[k.Kind] = k.Edges
		}
		side.UndeclaredRelationshipKinds = kinds
	}
	// #6338 — CARRIED FORWARD, not recomputed. This pass only ever looks at
	// the files that CHANGED, so it cannot know the repo-wide unsupported
	// tally; writing a fresh struct would zero the full index's count and make
	// `doctor` go silent again after the first incremental reindex — exactly
	// the blind spot this field exists to close. The count goes stale rather
	// than wrong, and the next full index refreshes it.
	if priorSide, sErr := graph.LoadSidecar(filepath.Dir(fbPath)); sErr == nil && priorSide != nil {
		side.UnsupportedExtensions = priorSide.UnsupportedExtensions
	}
	endSide := tr.span("sidecar-write")
	serr := graph.WriteSidecar(fbPath, side, false)
	endSide()
	if serr != nil {
		logger.Printf("incremental: sidecar write failed: %v (non-fatal)", serr)
	}

	// --- Step 9: update manifest ---
	//
	// Stamped from the bytes the re-extraction loop read, NOT re-hashed off disk
	// (#6212). The graph fbwriter.WriteGraphGen just wrote was built from those
	// bytes; anything the working tree has done since belongs to the next pass,
	// and re-hashing here would stamp it as indexed and then hash-match it away.
	//
	// Also O(delta) instead of O(repo): the files this pass did not re-extract
	// keep the stamps they already had, which is exactly what established them
	// as unchanged in the first place (#6201, #6206).
	// The failed-extraction set is EXPLICITLY EMPTY here, and that is a property
	// of this function, not an omission (#6209). The extraction loop above does
	// not tolerate an extractor error at all: it returns fallback() the moment
	// one occurs (see "#6151 — NOT non-fatal"), because Step 5 has already
	// evicted that file's entities and persisting the graph would drop them.
	// Reaching Step 9 therefore means every file in reallyChanged extracted
	// cleanly, and the full reindex the fallback triggers is what records the
	// failure — on the cmd/grafel path, which does mark it.
	//
	// Routed through ApplyStampsAndFailures rather than ApplyStamps anyway, so
	// that if this path ever learns to tolerate a partial failure, the call site
	// is already the one that records it instead of silently stamping it as
	// indexed. A nil set makes this identical to ApplyStamps.
	endMS := tr.span("manifest-apply-stamps")
	_ = diff.ApplyStampsAndFailures(walkStamps, allFiles, nil, manifest)
	endMS()
	endMSave := tr.span("manifest-save")
	// #6474: the pass-start commit, matching the walkStamps applied just above.
	// Those stamps come from the bytes the extraction loop read; live HEAD at
	// this instant may be several commits past them.
	saveErr := diff.SaveManifestAtCommit(stateDir, manifest, passHeadShort, passHeadFull)
	endMSave()
	if saveErr != nil {
		logger.Printf("incremental: save manifest: %v (non-fatal)", saveErr)
	}

	dur := time.Since(t0)
	logger.Printf("incremental: done changed=%d entities=%d rels=%d class_kind_folds=%d pass25_rels_bound=%d pass25_rels_late_bound=%d pass25_rels_dropped=%d endpoints_resolved=%d endpoints_kept_unresolved=%d endpoints_superseded=%d flows_recomputed=%t took=%s",
		len(reallyChanged), len(newEntities), len(newRels), classKindFolds,
		pass25RelsBound, pass25RelsLateBound, pass25RelsDropped, endpointsResolved, endpointsKeptUnresolved, endpointsSuperseded,
		flowsRecomputed, dur.Truncate(time.Millisecond))

	return Result{
		Done:         true,
		ChangedFiles: len(reallyChanged),
		Duration:     dur,
	}
}

// stampModuleOnEntities stamps Properties["module"] on each entity in ents that
// does not already carry one, matching the full-rebuild path's labeling
// (cmd/grafel/index.go buildDocument):
//
//   - PLAIN repo → one label for every sourced entity. The full path uses
//     repoSlug-or-repoTag; we recover that label from the existing graph (in a
//     plain repo every sourced entity already shares one "module" value).
//   - MONOREPO  → the deterministic path rollup over the package-boundary
//     markers, via module.Derive.
//
// Sourceless synthetic entities are stamped "_external", mirroring the full
// path's post-assembly _external sweep.
func stampModuleOnEntities(ents []graph.Entity, doc *graph.Document, absRepo string, allFiles []string) {
	if len(ents) == 0 {
		return
	}

	// Determine the plain-repo single label, matching the full-rebuild path
	// (cmd/grafel/index.go Run): a PLAIN (non-monorepo) repo forces every sourced
	// entity into ONE module label == repoSlug-or-repoTag (issue #1628); a TRUE
	// monorepo (>1 workspace package) uses the per-package path rollup.
	//
	// The full path's label is repoSlug (falling back to repoTag); the
	// incremental path only has doc.Repo (the repoTag), which equals the full
	// path's label whenever repoSlug is empty or equal to repoTag — the normal
	// case. We cross-check against the existing graph: in a plain repo every
	// sourced survivor already shares one "module" value, which is the
	// authoritative label the previous full build stamped. Prefer that recovered
	// label (exact, even when repoSlug != repoTag); fall back to doc.Repo.
	plainLabel := ""
	if mono, derr := detect.DetectMonorepo(absRepo); derr != nil || mono.Kind == detect.KindNone || len(mono.Packages) <= 1 {
		// Plain repo. Recover the exact label the previous build used, if any
		// sourced survivor carries one; otherwise use the repo tag.
		single, multiple := "", false
		for k := range doc.Entities {
			e := &doc.Entities[k]
			if e.Kind == module.KindModule || e.SourceFile == "" || e.PropLen() == 0 {
				continue
			}
			m, ok := e.PropLookup("module")
			if !ok || m == "" || m == "_external" {
				continue
			}
			if single == "" {
				single = m
			} else if single != m {
				multiple = true
				break
			}
		}
		switch {
		case single != "" && !multiple:
			plainLabel = single
		case doc.Repo != "":
			plainLabel = doc.Repo
		}
	}

	// Markers for the monorepo path. BuildMarkerSet expects repo-relative
	// forward-slash paths — exactly what walkSourceFiles produced into allFiles.
	markers := module.BuildMarkerSet(allFiles)

	for i := range ents {
		e := &ents[i]
		if e.PropLen() > 0 {
			if v, ok := e.PropLookup("module"); ok && v != "" {
				continue // extractor-supplied label preserved
			}
		}
		var label string
		switch {
		case e.SourceFile == "":
			label = "_external"
		case plainLabel != "":
			label = plainLabel
		default:
			label = module.Derive(e.SourceFile, markers)
		}
		if e.PropLen() == 0 {
			e.PropsReplace(map[string]string{})
		}
		e.PropSet("module", label)
	}
}

// affectedModuleSet computes the blast radius of a reindex in module-key terms:
// the modules whose membership or cross-module dependencies could have changed.
// This is the union of:
//
//   - the modules of every re-extracted (new) entity — their membership and the
//     cross-module edges they originate changed;
//   - the modules of every removed entity — CONTAINS/membership shrank and the
//     edges they originated vanished;
//   - both endpoint modules of every newly added relationship — a new
//     cross-module edge moves a DEPENDS_ON weight.
//
// Returned as the ModuleKey set AggregateIncremental scopes its strip+rebuild
// to. doc must already hold the merged (post-Step-8) entity set so endpoint
// module lookups resolve.
func affectedModuleSet(doc *graph.Document, removedModuleKeys map[module.ModuleKey]struct{}, newEnts []graph.Entity, newRels []graph.Relationship) map[module.ModuleKey]struct{} {
	// id → module key over the merged graph (used to resolve relationship
	// endpoints, including survivors).
	idMod := make(map[string]module.ModuleKey, len(doc.Entities))
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.Kind == module.KindModule {
			continue
		}
		idMod[e.ID] = entityModuleKey(e, doc.Repo)
	}

	affected := make(map[module.ModuleKey]struct{})
	add := func(mk module.ModuleKey) { affected[mk] = struct{}{} }

	for i := range newEnts {
		add(entityModuleKey(&newEnts[i], doc.Repo))
	}
	// Removed entities (incl. fully-deleted files with no replacement): their
	// module's membership shrank, so its CONTAINS / DEPENDS_ON must be re-derived.
	for mk := range removedModuleKeys {
		add(mk)
	}

	for i := range newRels {
		r := &newRels[i]
		if mk, ok := idMod[r.FromID]; ok {
			add(mk)
		}
		if mk, ok := idMod[r.ToID]; ok {
			add(mk)
		}
	}
	return affected
}

// entityModuleKey mirrors module.AggregateIncremental's per-entity key
// derivation: Properties["module"] (default "_external") + Properties["repo"]
// (default docRepo).
func entityModuleKey(e *graph.Entity, docRepo string) module.ModuleKey {
	mod := "_external"
	repo := docRepo
	if e.PropLen() > 0 {
		if v, ok := e.PropLookup("module"); ok && v != "" {
			mod = v
		}
		if v, ok := e.PropLookup("repo"); ok && v != "" {
			repo = v
		}
	}
	return module.NewModuleKey(repo, mod)
}

// fallback returns a Result with Done=false and the given reason.
func fallback(t0 time.Time, reason string) Result {
	return Result{
		Done:           false,
		FallbackReason: reason,
		Duration:       time.Since(t0),
	}
}

// samplePaths returns up to 10 paths for diagnostic logging at the
// too-many-changed fallback, so a recurrence shows WHICH files tripped it
// instead of just a count (#5668), without flooding the log on a large
// changeset.
func samplePaths(s []string) []string {
	const n = 10
	if len(s) <= n {
		return s
	}
	return append(append([]string{}, s[:n]...), fmt.Sprintf("…+%d more", len(s)-n))
}

// walkSourceFiles returns repo-relative forward-slash paths for all source
// files under absRepo, using the SAME gitignore/.grafelignore-aware walker the
// full indexer uses (walk.WalkRepo). This is deliberate: the incremental
// change-detector and the full index must agree on which files exist.
//
// Previously this was a hand-rolled filepath.WalkDir with only a small
// hardcoded directory denylist and NO .gitignore handling. The full index
// (walk.WalkRepo) excluded gitignored build-artifact directories (e.g.
// ios/Pods, android/**/.cxx), but this walker did not — so those gitignored
// files entered the change manifest and, because build tooling constantly
// regenerates/deletes them, were counted as "changed" on every poll. With the
// HEAD static, that perpetually tripped the too-many-changed full-reindex
// fallback (incremental.go ~line 233), pinning daemon CPU in an endless
// reindex loop (#5665). Delegating to walk.WalkRepo makes both paths honor the
// same ignore rules, so gitignored churn can no longer drive reindexing.
//
// It also returns the walker's irregular-file report (#6416). The walker skips
// non-regular entries — a FIFO named `Hang.vb` would otherwise block the
// reading worker forever — and the foreground `grafel index` prints that skip
// unconditionally. The daemon path reached the SAME walker through this
// function and dropped `skipped` on the floor, so a watcher-triggered reindex
// dropped the file with no stderr line and no doctor entry anywhere: the
// #6338 lesson was satisfied only for the path most users do not hit. The
// report is threaded out here so tryIncremental can log it.
func walkSourceFiles(absRepo string) ([]string, string, error) {
	// Mirror the full indexer: probe sparse-checkout state so a partial
	// working tree is walked consistently. ProbeRepo is best-effort and
	// returns a zero-value (no sparse filtering) when the repo isn't sparse.
	sparse := gitmeta.ProbeRepo(absRepo)
	files, skipped, err := walk.WalkRepo(absRepo, &walk.Options{Sparse: &sparse})
	return files, walk.IrregularSkipReport(skipped), err
}

// entityRecordToGraphEntity converts a types.EntityRecord produced by an
// extractor into a graph.Entity. Mirrors the buildDocument pass in cmd/grafel/index.go
// without importing that package (avoids a cmd → internal cycle).
func entityRecordToGraphEntity(r types.EntityRecord, repoTag string) graph.Entity {
	// #6150 — DERIVE, always. The record's own ID is deliberately ignored, which
	// is what buildDocument does (its assembly loop computes
	// graph.EntityID(repoTag, Kind, Name, SourceFile) unconditionally and never
	// reads r.ID). Honouring r.ID here made the two paths disagree on the
	// IDENTITY of an entity, not just its content: engine's http_endpoint
	// synthesis stamps ID with the routable form of the endpoint — literally
	// "http:GET:/things" (#1725, the same string it uses as QualifiedName) — so
	// an endpoint in a re-extracted file carried a raw routable string where a
	// full rebuild had a deterministic hex.
	//
	// Ids are the join key for every edge endpoint, for the FlatBuffers `(key)`
	// binary search behind LookupEntityByID (#5974, which needs ids in canonical
	// order), and for the flow passes' entry_id / chain / branches_dag
	// properties — the last of which is how it surfaced: the incremental
	// SCOPE.Process node described its entry as "http:GET:/cpthings".
	//
	// A content-keyed parity comparison CANNOT see this on its own (it keys
	// entities and edge endpoints by kind/name/source_file precisely so that
	// ids are not compared), and the incremental graph is internally consistent
	// with the wrong id, so nothing downstream of it dangles. It surfaced only
	// because a flow property happens to embed the id as text.
	id := graph.EntityID(repoTag, r.Kind, r.Name, r.SourceFile)
	// #6275 — r.Properties (and therefore any grafel.twin_of a #6104 merge
	// facet carries) is copied VERBATIM here, with no equivalent of
	// cmd/grafel/index.go's stampEntityIDs remap (old ComputeID() -> this
	// freshly computed `id`). That is harmless TODAY only because the sole
	// twin_of writer, internal/extractors/custom_dispatch.go's
	// enrichFromTwin, is reachable exclusively from the full-index path
	// (MergeWithCustom has no caller on this incremental path — see
	// classfold.go's FoldFrameworkClassKinds doc comment, "TryIncremental
	// runs no cross extractors at all"). If anything ever starts stamping
	// twin_of from code reachable here, the #6275 orphaned-anchor bug comes
	// back silently: this function would need the same pre-stamp-id ->
	// final-id remap stampEntityIDs performs.
	return graph.Entity{
		ID:            id,
		Name:          r.Name,
		QualifiedName: r.QualifiedName,
		Kind:          r.Kind,
		Subtype:       r.Subtype,
		SourceFile:    r.SourceFile,
		StartLine:     r.StartLine,
		EndLine:       r.EndLine,
		Language:      r.Language,
		Signature:     r.Signature,
		Tags:          r.Tags,

		Confidence: r.Confidence, // Phase 1C (#2769) — propagates extractor stamp.
	}.WithProperties(r.Properties)
}

// convertExtractedRecords converts one file's extractor output into graph
// entities and relationships, mirroring the assembly loop in cmd/grafel/index.go
// (buildDocument). seenRel carries the (FromID, ToID, Kind) identities already
// emitted and is MUTATED — callers share one map across the whole re-extraction
// batch.
//
// Two behaviours here are the #6094 fix, and both must be preserved:
//
// OWNER-ID SUBSTITUTION. A record-embedded edge with no explicit FromID is
// OWNED by the record carrying it, so the owning entity's ID is substituted (see
// relRecordToGraphRel). Leaving FromID empty makes the edge invisible to every
// FromID-keyed consumer: the stale-edge eviction in TryIncremental drops an old
// edge only when removedEntityIDs[r.FromID], so an empty FromID matched nothing
// and each pass appended another copy — the unbounded accumulation in #6094.
//
// THE seenRel GUARD. An extractor may emit the same owned edge more than once
// for one file — e.g. Go's `import "strings"` plus `import s2 "strings"` yields
// two SCOPE.Component records that both emit `<file> -IMPORTS-> ext:strings`.
// The full path suppresses the repeat and this path must agree, or the
// incremental graph carries a row the full rebuild does not.
//
// WHY (FromID, ToID, Kind) IS A SAFE IDENTITY HERE, AND ONLY HERE.
// (from, to, kind) is NOT a unique relationship key in general: several engine
// passes deliberately salt the relationship ID so edges sharing a triple stay
// distinct (internal/engine/migration_schema_ops.go, phantom_edges.go,
// process_flow.go, event_flow.go, internal/links, internal/graph). Collapsing on
// the triple would destroy those. It is safe in THIS function because
// types.RelationshipRecord has no ID field at all (internal/types/relationship.go)
// — a record-embedded edge cannot carry a salted ID — and because those salted
// producers are engine passes that never reach this loop, which only consumes
// `ext.Extract` output. Kind is therefore load-bearing in the key: two edges
// between the same pair of endpoints under different kinds are distinct edges and
// both must survive.
//
// SCOPE DIFFERENCE FROM buildDocument. buildDocument's seenRel spans the whole
// corpus; this one spans only the re-extracted batch, and is NOT seeded from the
// surviving doc.Relationships. The asymmetry is harmless because the two maps
// guard disjoint row sets: every entity sourced from a changed file — and, via
// owner-ID substitution, every edge owned by one — is evicted from
// doc.Relationships before this runs, so a surviving row and a freshly emitted
// row cannot share a (FromID, ToID, Kind) identity unless the survivor is
// inbound from an UNCHANGED file, and inbound rows are not re-emitted here at
// all. Seeding from the survivors would additionally risk suppressing a
// legitimately re-emitted edge, so it is deliberately not done.
//
// THE ENTITY FOLD (#6161). Entities were appended unconditionally here while
// the relationship guard three lines below already existed, so two records
// deriving the same graph.EntityID became two rows. That breaks the invariant
// internal/graph/emission_order.go:32 states verbatim — "Entity IDs are unique,
// so ID alone is a total order — no secondary keys" — which SortDocumentForEmission
// relies on and which LookupEntityByID's bare FlatBuffers binary search relies
// on harder: where it is false, one row is returned arbitrarily and the other is
// permanently unreachable while still occupying a slot and a count.
//
// The collision is EXPECTED, not erroneous. graph.EntityID is
// sha256(repo, kind, name, source_file) and excludes StartLine, so every
// construct declaring one name twice in one file collides by construction: Java
// method overloads (custom_dispatch.go:507 documents exactly this), C#/VB
// partial classes and methods, C++/TypeScript overload declarations, Python
// @overload / @singledispatch / `def` under `if TYPE_CHECKING`, Ruby reopened
// classes and attr_accessor-generated methods. A bare `if seen { continue }`
// would therefore DISCARD the second overload's whole edge set — strictly worse
// than the duplication, which is at least visible in a count. Hence a fold: gate
// the row, gap-fill via foldDuplicateEntity, and let the relationship loop run
// for every record so the duplicate's edges anchor to the survivor's id.
//
// FOLD SCOPE — THIS ONE IS PER-FILE AND THAT IS NOT SUFFICIENT ON ITS OWN.
// entityPos is per-call, whereas seenRel spans the whole batch, and unlike
// seenRel's scope the difference here is NOT harmless. An entity id can collide
// across files, because synthesised entities carry a PLACEHOLDER source file
// ("<exception>") rather than a real one, and it can collide with the previous
// graph, because such an entity is never evicted by Step 5. Both of those are
// folded at the Step 8 merge instead — see mergeEntitiesDeduped, which is
// survivor-aware and is what actually guarantees the invariant on the written
// graph.
//
// What this fold buys, given that: the duplicate never enters newEntities at
// all, so the scoped resolver, the signature-change scan, the module stamp and
// the flow blast-radius all see one row rather than two. It is also the fold
// that keeps the OVERLOAD case honest, since that one is same-file by
// definition.
//
// buildDocument's equivalent gate is corpus-wide because ITS input genuinely is
// (merged spans every file), so it needs no second fold downstream.
func convertExtractedRecords(records []types.EntityRecord, repoTag string, seenRel map[string]bool) ([]graph.Entity, []graph.Relationship) {
	ents := make([]graph.Entity, 0, len(records))
	var rels []graph.Relationship
	// #6161 — entityPos maps a derived graph.EntityID → its index in `ents`, so a
	// later record deriving the SAME id gap-fills onto the already-emitted
	// survivor instead of appending a second row. See the ENTITY FOLD note above.
	entityPos := make(map[string]int, len(records))
	for _, rec := range records {
		e := entityRecordToGraphEntity(rec, repoTag)
		if pos, dup := entityPos[e.ID]; dup {
			foldDuplicateEntity(&ents[pos], e)
		} else {
			ents = append(ents, e)
			entityPos[e.ID] = len(ents) - 1
		}
		// NOT inside the else. The relationship loop runs for EVERY record,
		// survivor or duplicate, exactly as buildDocument's does — e.ID is the
		// derived id and is identical for both, so a duplicate's owned edges
		// anchor to the survivor and nothing is orphaned by the fold.
		for _, relRec := range rec.Relationships {
			r := relRecordToGraphRel(relRec, e.ID)
			if seenRel[r.ID] {
				continue
			}
			seenRel[r.ID] = true
			rels = append(rels, r)
		}
	}
	return ents, rels
}

// mergeEntitiesDeduped merges the freshly extracted entities into the surviving
// ones under a single-row-per-graph.EntityID rule (#6161).
//
// WHY THE SEAM-LEVEL FOLD IN convertExtractedRecords IS NOT ENOUGH. That one is
// per-file, and two of the three ways this invariant breaks reach across files:
//
//	PLACEHOLDER SOURCE FILES. A synthesised entity is not anchored in a real file.
//	It is stamped with a SENTINEL, and there are SEVEN of them in the tree, not
//	one: "<config>", "<exception>", "<external-service>", "<translation-key>" and
//	"<template>" (internal/extractor/synthetic_source.go), plus "<package>"
//	(cross/manifest/extractor.go) and "<panache-dsl-runtime>" (java/panache.go).
//	Since graph.EntityID hashes (repo, kind, name, source_file), every file that
//	names the same config key / exception / package derives the SAME id, so two
//	changed files in ONE batch each contribute a copy that no per-file fold can
//	see. This function is kind-agnostic and covers all seven; "<config>" is the
//	highest-volume of them in a real corpus, and "<exception>" is merely the one
//	the parity fixture happens to reach.
//
//	THE PREVIOUS GRAPH'S COPY. Worse, and the reason this is a merge-side fold
//	rather than a batch-side one: Step 5 evicts entities sourced from a CHANGED
//	file, and no sentinel is a file, so the prior copy always survives and the new
//	one was appended beside it. Every pass added one more — measured x2, x3, x4,
//	x5 across successive edits to one handler, with the graph's total entity count
//	climbing in step (13 → 16 over three passes on a five-file fixture), so this
//	leaked a ROW per pass into the written graph rather than merely multiplying
//	one node. That is #6094's unbounded accumulation, on entities instead of
//	edges, cleared only by a full reindex.
//
// SURVIVOR WINS, AND THAT IS THE CORRECT WAY ROUND. A colliding pair means the
// two records agree on (repo, kind, name, source_file), i.e. they ARE the same
// entity. If that source file had changed, Step 5 would have evicted the
// survivor and there would be no collision; so a collision implies the survivor
// comes from an unchanged file (or from nowhere), which makes it the
// authoritative row. The incoming copy only gap-fills, exactly as
// buildDocument's dedup branch does. No edge is orphaned either way, because
// both rows carry the same id and every edge endpoint is that id.
//
// THE SURVIVING SET IS FOLDED TOO, in place via the `existing[:0]` filter idiom
// already used for the inbound-dangling prune above. A graph written by an
// earlier build already contains accumulated copies; without this the growth
// stops but the damage stays until a full reindex, and the uniqueness invariant
// SortDocumentForEmission and LookupEntityByID depend on would still be false on
// the very next write.
//
// AND THAT IS WHY THERE IS NO `len(incoming) == 0` EARLY RETURN. It looks free —
// a deletion-only pass extracts nothing — but it is exactly the pass on which a
// legacy graph would silently keep its accumulated rows. The scan is one map
// build over the entity set; the repair is the point.
//
// The size hint is len(existing), not len(existing)+len(incoming): incoming is
// the re-extracted delta and is small next to the corpus, and hinting the sum
// roughly doubles the bucket count for no benefit. At 500k entities this map is
// ~16MB, transient, and sits beside an entity slice an order of magnitude
// larger — it is not a walk-back of #5954's peak-RSS work.
func mergeEntitiesDeduped(existing, incoming []graph.Entity) []graph.Entity {
	pos := make(map[string]int, len(existing))
	out := existing[:0]
	fold := func(e graph.Entity) {
		if p, dup := pos[e.ID]; dup {
			foldDuplicateEntity(&out[p], e)
			return
		}
		out = append(out, e)
		pos[e.ID] = len(out) - 1
	}
	// Safe to write into existing's backing array while reading it: the write
	// index never runs ahead of the read index.
	for _, e := range existing {
		fold(e)
	}
	for _, e := range incoming {
		fold(e)
	}
	return out
}

// foldDuplicateEntity merges a duplicate record's entity into the survivor that
// already occupies its graph.EntityID (#6161).
//
// It is a port of buildDocument's dedup branch (cmd/grafel/index.go, issue
// #4406) and the two must stay in step: where they disagree, the full path and
// the incremental path disagree about what an entity IS, which is a worse
// defect than the duplication this fold removes.
//
// GAP-FILL, NEVER OVERRIDE. The survivor's own values always stand; the
// duplicate only supplies fields the survivor left empty. The dropped record is
// frequently the carrier of base-only state the survivor lacks — most
// critically the module-qualified QualifiedName that drives byQualifiedName
// resolution and cross-repo joins (the live-graph half of #4402, the same shape
// #4405 fixed at the MergeWithCustom boundary).
//
// LINE SPANS ARE FILLED, NOT UNIONED. Only a ZERO span is filled. Extending the
// survivor's span to cover the duplicate's is what custom_dispatch.go:507
// warns about: "Java method overloads give two declarations the same (Kind,
// Name, SourceFile) at different lines", and a blind span union there invented a
// third span covering neither declaration.
//
// METADATA IS DELIBERATELY NOT FOLDED, even though buildDocument folds it.
// entityRecordToGraphEntity does not carry EntityRecord.Metadata onto the entity
// at all on this path (buildDocument does — a pre-existing divergence, outside
// #6161, tracked as #6251). Folding it here would make an entity that HAPPENS to
// have a duplicate carry the duplicate's metadata while carrying none of its
// own, which is stranger than carrying none at all.
//
// BEFORE YOU FIX #6251, KNOW THAT IT IS STACKED WITH #6245 AND THE TWO MASK EACH
// OTHER. Metadata also has no FlatBuffers slot (#6245), so even on Path B the
// value dies at the storage boundary. Fixing the Path A drop alone changes
// nothing observable; fixing the slot alone EXPOSES the Path A drop as a new
// full-vs-incremental divergence (layer_confidence present after a full index,
// gone after an incremental one). They have to land together, and this fold
// gains its Metadata clause in that same change — not before, or it would fill
// gaps in a field nothing else on this path populates.
func foldDuplicateEntity(surv *graph.Entity, dup graph.Entity) {
	if surv.QualifiedName == "" && dup.QualifiedName != "" {
		surv.QualifiedName = dup.QualifiedName
	}
	if surv.Subtype == "" && dup.Subtype != "" {
		surv.Subtype = dup.Subtype
	}
	if surv.Signature == "" && dup.Signature != "" {
		surv.Signature = dup.Signature
	}
	if surv.Language == "" && dup.Language != "" {
		surv.Language = dup.Language
	}
	if surv.StartLine == 0 && dup.StartLine != 0 {
		surv.StartLine = dup.StartLine
	}
	if surv.EndLine == 0 && dup.EndLine != 0 {
		surv.EndLine = dup.EndLine
	}
	if len(dup.Tags) > 0 {
		seenTag := make(map[string]bool, len(surv.Tags)+len(dup.Tags))
		for _, t := range surv.Tags {
			seenTag[t] = true
		}
		for _, t := range dup.Tags {
			if !seenTag[t] {
				seenTag[t] = true
				surv.Tags = append(surv.Tags, t)
			}
		}
	}
	if dup.PropLen() > 0 {
		if surv.PropLen() == 0 {
			surv.PropsReplace(make(map[string]string, dup.PropLen()))
		}
		dup.PropRange(func(k, v string) bool {
			if _, exists := surv.PropLookup(k); !exists {
				surv.PropSet(k, v)
			}
			return true
		})
	}
}

// endpointSyntheticKind is the kind engine's http_endpoint synthesis settles on
// for a route DEFINITION after the #1217 migration inside the resolve pass.
const endpointSyntheticKind = "http_endpoint_definition"

// pruneSupersededEndpointSynthetics drops an UNRESOLVED endpoint synthetic that
// the surviving graph already carries under the same (kind, name) (#6150).
//
// It exists because both obvious verdicts on an endpoint whose handler is not
// in the re-extracted file are wrong, and the two failure modes are opposites:
//
//	DROPPING it destroys a live endpoint whenever nothing else in the graph
//	carries it — the Express-router-plus-imported-controller shape, where the
//	full rebuild leaves the endpoint anchored at the router file and the
//	incremental run is the only producer of it. That is why the pass is called
//	through engine.ResolveHTTPEndpointHandlersFileScoped rather than the corpus
//	entry point, which drops.
//
//	KEEPING it invents a DUPLICATE whenever the full rebuild resolved the same
//	endpoint cross-file: bridgeEndpointToHandler REBINDS the synthetic's
//	source_file onto the handler's body, so the previous graph's copy is
//	anchored at the HANDLER's file, survives the delta untouched, and the
//	re-extracted router file contributes a second, unresolved copy at its own
//	coordinates. Measured on Flask `add_url_rule` + an imported view: the full
//	rebuild has one endpoint at the view's file; the incremental run had two.
//
// The second failure mode PREDATES this work — it reproduces byte-identically
// with the endpoint-resolve pass disabled entirely, because before #6150 this
// path emitted Detect's synthetic unconditionally. It is not a regression
// introduced by the keep guard; the keep guard simply must not perpetuate it.
//
// The path is not short of evidence, which is what makes a third verdict
// possible: it holds the SURVIVING graph. `survivors` is keyed
// `Kind + "|" + Name` over the entities that outlived Step 5's eviction — so an
// unresolved synthetic already carried by one of them is the same endpoint,
// already correctly anchored, and the new copy is redundant. An unresolved
// synthetic with no survivor is the only copy there is, and is kept.
//
// A RESOLVED synthetic is never pruned, and the reason is NOT that it merges
// with the survivor. It does not: entityRecordToGraphEntity derives
// EntityID(repoTag, Kind, Name, SourceFile), and a survivor is by construction
// anchored in a file that was NOT re-extracted, so the SourceFile differs and
// so does the id. The real reason is that a same-named endpoint in a DIFFERENT
// file is a DIFFERENT ROUTE — one a full rebuild also carries, as two rows —
// so pruning the resolved one would delete a route on the strength of a name
// collision.
//
// TWO LIMITS OF THE KEY, both deliberate and both untested by any fixture:
//
//   - It is `Kind|Name`, unqualified by path or repo. Two files legitimately
//     registering the SAME route therefore collapse onto whichever the graph
//     already had. That is the correct answer for the case this exists for (a
//     rebound endpoint IS the same route under a different anchor) and the
//     wrong one for a genuine same-route-twice corpus. Qualifying by path
//     cannot work — the whole point is that the survivor's path differs.
//
//   - It sees only doc.Entities, i.e. files NOT in this delta. An endpoint
//     carried by a file that IS in the delta is not a survivor, which is what
//     makes the no-survivor case reach the keep branch at all.
//
// Returns the filtered slice (compacted in place over the caller's backing
// array, like the resolve pass it follows) and the number pruned.
func pruneSupersededEndpointSynthetics(records []types.EntityRecord, survivors map[string]bool) ([]types.EntityRecord, int) {
	if len(records) == 0 || len(survivors) == 0 {
		return records, 0
	}
	pruned := 0
	out := records[:0]
	for i := range records {
		r := &records[i]
		// source_handler's PRESENCE is the unresolved signal: the resolve pass
		// deletes the property the moment it binds (bridgeEndpointToHandler).
		if r.Kind == endpointSyntheticKind && r.Properties["source_handler"] != "" &&
			survivors[r.Kind+"|"+r.Name] {
			pruned++
			continue
		}
		out = append(out, records[i])
	}
	for i := len(out); i < len(records); i++ {
		records[i] = types.EntityRecord{}
	}
	return out, pruned
}

// bindPass25StubEndpoints binds the `Kind:Name` structural endpoint refs that
// Pass 2.5 produces against ONE re-extracted file's own records (#6150).
//
// Two producers emit them, and both are handled:
//
//   - Detect's STANDALONE relationships (`standalone`) — a YAML
//     `relationship_rules` match, e.g. falcon's `app.add_route(path, Res())`
//     giving `Controller:Res → Service:app` with the REGISTERED_ON kind. These
//     carry no owner, so a bound one is APPENDED to its source record with an
//     EMPTY FromID: that is the "my owner" form relRecordToGraphRel substitutes
//     the owning entity's id for, and it is what makes the edge visible to the
//     FromID-keyed stale-edge eviction (#6094) on the next edit of this file.
//   - EMBEDDED relationships already on the records — notably the endpoint→
//     handler IMPLEMENTS bridge that engine.ResolveHTTPEndpointHandlers appends
//     with `Kind:Name` on BOTH ends (bridgeEndpointToHandler). Those are
//     rewritten in place.
//
// WHY BINDING IS REQUIRED, not a nicety. The full path emits these stubs raw
// and a corpus-wide resolver pass (internal/resolve) rewrites them against the
// stamped entity index. TryIncremental runs the SCOPED resolver instead, which
// indexes the previous persisted graph by name and has no notion of a
// `Kind:Name` structural ref — so an unbound stub reaches graph.fb verbatim.
// Measured, when #6148 first tried emitting them unchanged: rows keyed
// `«unbound»Controller:X → «unbound»Service:y` and duplicate DEPENDS_ON. The
// binder is what makes consuming Detect's relationships an improvement instead
// of a second defect.
//
// WHAT IT REFUSES. Only an endpoint whose whole string is `Kind + ":" + Name`
// of EXACTLY ONE record in the SAME FILE as the record being bound is
// rewritten; everything else is left alone, and a standalone relationship with
// an endpoint that does not bind is DROPPED rather than emitted unbound. Three
// consequences, all deliberate:
//
//   - a bare name ("len"), a dotted import ("cfg.SETTING") and an already
//     stamped hex id contain no matching key, so the scoped resolver — which
//     knows how to bind those corpus-wide — keeps them;
//   - a cross-file target is refused. The alternative is guessing at a name the
//     full path resolves against the whole corpus, and a WRONG bind is worse
//     than a missing row: it improves every count-based and dangling-endpoint
//     metric while making the graph say something false (#6123);
//   - an AMBIGUOUS key (two records, same file, same kind and name) is refused
//     for the same reason. The corpus resolver has an ambiguity sentinel; this
//     has one row of evidence and no way to choose.
//
// The lookup key is the WHOLE `Kind:Name` string, never a split on the first
// colon: `http_endpoint_definition:http:GET:/things` is kind
// "http_endpoint_definition" and name "http:GET:/things", and a splitter would
// look up kind "http_endpoint_definition", name "http" and miss every endpoint.
//
// Returns the records (same slice, mutated in place), the number of standalone
// relationships bound, and the ones it REFUSED — returned rather than merely
// counted so Step 7c can re-offer them to a corpus-wide index (#6461 MOUNT
// direction). Refusing here is still correct: this call has evidence about ONE
// file, and the returned slice is not a promise that any of them will bind.
func bindPass25StubEndpoints(
	records []types.EntityRecord,
	standalone []types.RelationshipRecord,
	repoTag string,
) ([]types.EntityRecord, int, []types.RelationshipRecord) {
	if len(records) == 0 {
		return records, 0, standalone
	}

	// (file, "Kind:Name") → index into records, or ambiguousStubIdx when more
	// than one record of that file carries the pair.
	const ambiguousStubIdx = -1
	type stubKey struct{ file, kindName string }
	idx := make(map[stubKey]int, len(records))
	for i := range records {
		r := &records[i]
		if r.Kind == "" || r.Name == "" || r.SourceFile == "" {
			continue
		}
		k := stubKey{r.SourceFile, r.Kind + ":" + r.Name}
		if _, dup := idx[k]; dup {
			idx[k] = ambiguousStubIdx
			continue
		}
		idx[k] = i
	}
	resolve := func(file, ref string) (string, bool) {
		i, ok := idx[stubKey{file, ref}]
		if !ok || i == ambiguousStubIdx {
			return "", false
		}
		return entityRecordToGraphEntity(records[i], repoTag).ID, true
	}

	// Embedded stubs: rewrite in place, in the owning record's own file.
	for i := range records {
		r := &records[i]
		for j := range r.Relationships {
			e := &r.Relationships[j]
			if id, ok := resolve(r.SourceFile, e.FromID); ok {
				e.FromID = id
			}
			if id, ok := resolve(r.SourceFile, e.ToID); ok {
				e.ToID = id
			}
		}
	}

	// SOURCE index: `Kind:Name` → the single record carrying it, ACROSS FILES.
	// The source end is looked up without a file to key on (a standalone
	// relationship carries no owner), and whichever record wins then decides
	// which file the TARGET is looked up in — so an ambiguous source is two
	// guesses stacked on one coin flip, and it is refused for the same reason
	// the target lookup refuses one. A first-wins source was the asymmetry in
	// this function's first version.
	srcIdxByRef := make(map[string]int, len(records))
	for i := range records {
		r := &records[i]
		if r.Kind == "" || r.Name == "" || r.SourceFile == "" {
			continue
		}
		ref := r.Kind + ":" + r.Name
		if _, dup := srcIdxByRef[ref]; dup {
			srcIdxByRef[ref] = ambiguousStubIdx
			continue
		}
		srcIdxByRef[ref] = i
	}

	// Standalone relationships: bind both ends or defer.
	bound := 0
	var deferred []types.RelationshipRecord
	for _, sr := range standalone {
		// The SOURCE end decides which file's records the target is looked up
		// in, so a relationship rule that fired in file X can only ever bind
		// within X — which is the only file this call has evidence about.
		srcIdx, ok := srcIdxByRef[sr.FromID]
		if !ok || srcIdx == ambiguousStubIdx {
			deferred = append(deferred, sr)
			continue
		}
		toID, tok := resolve(records[srcIdx].SourceFile, sr.ToID)
		if !tok {
			deferred = append(deferred, sr)
			continue
		}
		e := sr
		e.FromID = "" // owned by records[srcIdx]; see relRecordToGraphRel.
		e.ToID = toID
		records[srcIdx].Relationships = append(records[srcIdx].Relationships, e)
		bound++
	}
	return records, bound, deferred
}

// deferredStubRel is one standalone Pass-2.5 relationship that the file-scoped
// binder refused, paired with the repo-relative path of the CHANGED file whose
// Detect run emitted it. The file is carried because the source end must bind
// inside THAT file and nowhere else — see Step 7c.
type deferredStubRel struct {
	rel  types.RelationshipRecord
	file string
}

// bindDeferredPass25Stubs re-offers the refused stubs to a corpus-wide index
// (#6461, MOUNT direction).
//
// `survivors` are the post-prune entities carried forward (including those on
// files that were NOT edited — the whole point), `fresh` are the re-extracted
// ones. Together they are the entity set this build will emit, which is what
// makes a cross-file target lookup evidence rather than a guess.
//
// It returns only relationships that are NEW: anything whose (from, to, kind)
// id already exists among `existingRels` or `freshRels` is skipped, so a stub
// that ALSO bound in-file cannot land twice.
//
// The two refusals — globally-ambiguous target, and a source that does not
// resolve within the emitting file — are the contract; see Step 7c for why
// each is load-bearing.
//
// `composeRemoved` is Step 7b's deletion set. It is normally redundant —
// running after 7b, those ids are already gone from `survivors` — and that is
// exactly why it is a parameter: it turns "call me after 7b" from a comment
// into something a caller cannot quietly get wrong, and a hoisted call site
// binding to a composed entity 7b is deleting produces nothing instead of a
// dangling edge. See Step 7c's ORDERING note.
func bindDeferredPass25Stubs(
	deferred []deferredStubRel,
	survivors []graph.Entity,
	fresh []graph.Entity,
	existingRels []graph.Relationship,
	freshRels []graph.Relationship,
	composeRemoved map[string]bool,
) []graph.Relationship {
	if len(deferred) == 0 {
		return nil
	}

	const ambiguous = "\x00ambiguous"

	// TARGET index: `Kind:Name` → entity id, over EVERY entity being emitted.
	// Ambiguity is sticky: a key seen twice is poisoned for good, so index
	// order cannot decide a bind.
	target := make(map[string]string, len(survivors)+len(fresh))
	addTarget := func(e *graph.Entity) {
		if e.Kind == "" || e.Name == "" || composeRemoved[e.ID] {
			return
		}
		k := string(e.Kind) + ":" + e.Name
		if prev, seen := target[k]; seen {
			if prev != e.ID {
				target[k] = ambiguous
			}
			return
		}
		target[k] = e.ID
	}
	for i := range survivors {
		addTarget(&survivors[i])
	}
	for i := range fresh {
		addTarget(&fresh[i])
	}

	// SOURCE index: (changed file, `Kind:Name`) → entity id, over the
	// RE-EXTRACTED entities only. Survivors are deliberately absent.
	type srcKey struct{ file, kindName string }
	source := make(map[srcKey]string, len(fresh))
	for i := range fresh {
		e := &fresh[i]
		if e.Kind == "" || e.Name == "" || e.SourceFile == "" {
			continue
		}
		k := srcKey{e.SourceFile, string(e.Kind) + ":" + e.Name}
		if prev, seen := source[k]; seen {
			if prev != e.ID {
				source[k] = ambiguous
			}
			continue
		}
		source[k] = e.ID
	}

	seen := make(map[string]struct{}, len(existingRels)+len(freshRels))
	for i := range existingRels {
		seen[existingRels[i].ID] = struct{}{}
	}
	for i := range freshRels {
		seen[freshRels[i].ID] = struct{}{}
	}

	var out []graph.Relationship
	for _, d := range deferred {
		fromID, ok := source[srcKey{d.file, d.rel.FromID}]
		if !ok || fromID == ambiguous {
			continue
		}
		toID, ok := target[d.rel.ToID]
		if !ok || toID == ambiguous {
			continue
		}
		r := d.rel
		r.FromID = ""
		r.ToID = toID
		g := relRecordToGraphRel(r, fromID)
		if _, dup := seen[g.ID]; dup {
			continue
		}
		seen[g.ID] = struct{}{}
		out = append(out, g)
	}
	return out
}

// relRecordToGraphRel converts an embedded types.RelationshipRecord to a
// graph.Relationship, mirroring the full-index assembly loop in
// cmd/grafel/index.go (buildDocument).
//
// ownerID is the ID of the entity record that carried r. A record-embedded edge
// may legitimately omit FromID to mean "from my owner"; the full path
// substitutes the owner's ID in that case and this path must agree, or the edge
// lands with an empty FromID and becomes invisible to every FromID-keyed
// consumer downstream (#6094 duplicate accumulation, #6098 weight shortfall).
func relRecordToGraphRel(r types.RelationshipRecord, ownerID string) graph.Relationship {
	fromID := r.FromID
	if fromID == "" {
		fromID = ownerID
	}
	id := graph.RelationshipID(fromID, r.ToID, r.Kind)
	return graph.Relationship{
		ID:     id,
		FromID: fromID,
		ToID:   r.ToID,
		Kind:   r.Kind,

		Confidence: r.Confidence, // Phase 1C (#2769).
		// Snapshot at the record→graph seam: graph.Relationship keeps its own
		// compact backing (#5850 Phase B) and WithProperties still accepts a
		// plain map, so the intermediate map is transient and never retained.
	}.WithProperties(r.Properties.Snapshot())
}

// sortGraphDocumentForEmission sorts entities and relationships into the
// canonical emission order. #5974: this used to be a local copy that ordered
// entities by (SourceFile, Kind, QualifiedName, Name, StartLine, ID) — NOT the
// ID order the full-index path uses and the FlatBuffers `(key)` binary search
// in fbreader requires, so LookupEntityByID silently missed entities in every
// incrementally written graph. It now delegates to the one shared
// implementation in internal/graph; there is no cmd → internal cycle because
// both producers depend on internal/graph already.
func sortGraphDocumentForEmission(doc *graph.Document) {
	graph.SortDocumentForEmission(doc)
}

// entityPropKey returns a stable string key for an entity used in the
// signature-change map: qualifiedName is preferred, falling back to name.
func entityPropKey(e graph.Entity) string {
	if e.QualifiedName != "" {
		return e.QualifiedName
	}
	return e.Name
}

// entityPropertiesHash computes a short hash of the fields that constitute an
// entity's "signature" for the purpose of signature-change detection (#2170).
// Fields hashed: Signature, Kind, Subtype, and the sorted Properties map.
// The result is a 16-char hex string.
func entityPropertiesHash(e graph.Entity) string {
	h := sha256.New()
	h.Write([]byte(e.Signature))
	h.Write([]byte{0})
	h.Write([]byte(e.Kind))
	h.Write([]byte{0})
	h.Write([]byte(e.Subtype))
	h.Write([]byte{0})
	// Sort property keys for stable hashing.
	keys := make([]string, 0, e.PropLen())
	e.
		PropRange(func(k, v string) bool { keys = append(keys, k); return true })
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte("="))
		h.Write([]byte(e.PropGet(k)))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// passStartCommitHook is a test-only observability seam invoked immediately
// after tryIncremental captures the pass-start commit (#6474), with the short
// and full hashes it captured.
//
// The defect it exists to make falsifiable is timing-dependent: HEAD moving
// BETWEEN the byte read and the manifest save. Nothing in a test can schedule
// that window reliably, so the pass hands it over explicitly — the test's
// callback commits a new HEAD and returns, and the rest of the pass then runs
// with live HEAD genuinely ahead of the commit it read. Mirrors
// cmd/grafel/index.go's enrichmentOrderHook. Production always uses the no-op
// default; tests swap it via SwapPassStartCommitHook and restore it.
var passStartCommitHook = func(short, full string) {}
