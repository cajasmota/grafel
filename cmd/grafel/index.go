package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cajasmota/grafel/internal/algorithms"
	"github.com/cajasmota/grafel/internal/classifier"
	"github.com/cajasmota/grafel/internal/coverage"
	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/extract"
	"github.com/cajasmota/grafel/internal/daemon/walk"
	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/enrichment"
	"github.com/cajasmota/grafel/internal/external"
	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors"
	bazelextract "github.com/cajasmota/grafel/internal/extractors/bazel"
	configextract "github.com/cajasmota/grafel/internal/extractors/config"
	"github.com/cajasmota/grafel/internal/extractors/cross"
	"github.com/cajasmota/grafel/internal/extractors/cross/ormlink"
	jsextract "github.com/cajasmota/grafel/internal/extractors/javascript"
	mageextract "github.com/cajasmota/grafel/internal/extractors/mage"
	pyextr "github.com/cajasmota/grafel/internal/extractors/python"
	taskextract "github.com/cajasmota/grafel/internal/extractors/task"
	"github.com/cajasmota/grafel/internal/generated"
	"github.com/cajasmota/grafel/internal/gitmeta"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	idiff "github.com/cajasmota/grafel/internal/indexer/diff"
	"github.com/cajasmota/grafel/internal/indexstate"
	"github.com/cajasmota/grafel/internal/ingest"
	"github.com/cajasmota/grafel/internal/install/detect"
	"github.com/cajasmota/grafel/internal/memtrace"
	"github.com/cajasmota/grafel/internal/module"
	"github.com/cajasmota/grafel/internal/process"
	"github.com/cajasmota/grafel/internal/progress"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/types"
	"github.com/cajasmota/grafel/internal/version"
)

// Pass names accepted by the --skip-pass flag.
const (
	PassExtract       = "extract"        // Pass 1: per-language AST extraction
	PassFramework     = "framework"      // Pass 2.5: YAML-driven framework rules
	PassCrossLang     = "cross-lang"     // Pass 3: cross-language extractors
	PassGraphAlgo     = "graph-algo"     // Pass 4: placeholder for PORT-4
	PassBuildDocument = "build-document" // Pass 5: assemble graph.Document
	PassRenameDetect  = "rename-detect"  // Pass 5.5: detect entity renames across rebuilds (#1344)
	PassEnrichment    = "enrichment"     // Pass 6: emit enrichment candidates
	PassProcessFlow   = "process-flow"   // Pass 7: process-flow BFS over CALLS (#724)
	PassEventFlow     = "event-flow"     // Pass 7.5: event-flow pub/sub walk (#1944 Phase 1)
	PassModuleAgg     = "module-agg"     // Pass 8: module-level aggregation (#1383)
	PassCommitCouple  = "commit-couple"  // Pass 8.5: VCS-derived COMMIT_COUPLED soft edges (#21)
	PassCoupling      = "coupling"       // Pass 8.6: structural Ca/Ce/instability per Module (#3634)
	PassDepHygiene    = "dep-hygiene"    // Pass 8.7: persist deplinker used/unused status onto deps (#3640)
	PassSharedDB      = "shared-db"      // Pass 8.8: shared-database cross-service coupling (#3628 area #13)
	PassLibBoundary   = "lib-boundary"   // Pass 8.9: first_party/third_party boundary on DEPENDS_ON edges (#3638)
	PassMigrationSeq  = "migration-seq"  // Pass 8.10: DB-migration ordering metadata + Alembic PRECEDES (#3639)
	PassMigrationOps  = "migration-ops"  // Pass 8.11: per-migration schema-op MODIFIES_TABLE edges (#3628 [schema])
	PassCoverage      = "coverage"       // Pass 8.12: LCOV line-coverage + static test-reachability stamping (#5061 / #5036 / #5037)
	PassEmbed         = "embed"          // Pass 9: semantic embeddings sidecar (#461 / ADR-0019)
	PassTestsWalkUp   = "tests-walkup"   // Pass 3.5: derive TESTS edges via helper walk-up
)

// memtraceLogOnce guards memtraceLogf so a chatty failure mode (e.g. a
// GRAFEL_MEMTRACE_DIR that goes unwritable partway through a long index)
// logs at most once, matching the best-effort contract in internal/memtrace.
var memtraceLogOnce sync.Once

// memtraceLogf is the best-effort logger passed to memtrace.Start: a single
// stderr line, at most once per process, never a fatal error. memtrace is
// purely diagnostic and must never affect the index result.
func memtraceLogf(format string, args ...any) {
	memtraceLogOnce.Do(func() {
		fmt.Fprintf(os.Stderr, "grafel: "+format+"\n", args...)
	})
}

// resolvePassTimer instruments the resolving_refs window (#5954).
//
// PROBLEM IT SOLVES. resolving_refs was a single phase label covering ~52% of
// cold-index wall time across a dozen unrelated passes. Nobody could tell how
// that time split between per-FILE work (which a content-addressed per-file
// extraction cache would eliminate) and GLOBAL work (which it would not) — and
// that split is exactly what decides whether such a cache is worth building.
//
// ZERO-RISK CONTRACT. This is observability only; it must not be able to change
// the graph or the UI.
//   - enter() calls Tracker.SubPhase, which stamps CurrentPhase WITHOUT
//     publishing an Event, so the SSE/CLI/wizard streams are unchanged. The
//     memtrace sampler reads CurrentPhase, so its phase= tag gets the split for
//     free — no new plumbing, no new file.
//   - The exact per-pass wall times are only printed when memtrace is already
//     enabled (GRAFEL_MEMTRACE_DIR set). In a normal run this type does nothing
//     but a few atomic stores, and stderr is byte-identical to before. The
//     printed line exists because the sampler polls at 250ms, which is too
//     coarse to attribute a sub-second pass.
type resolvePassTimer struct {
	trk    *progress.Tracker
	traced bool
	label  string
	since  time.Time
	rows   []string
}

func newResolvePassTimer(trk *progress.Tracker) *resolvePassTimer {
	return &resolvePassTimer{
		trk:    trk,
		traced: os.Getenv(memtrace.DirEnv) != "",
	}
}

// enter closes out the pass that was running (if any) and stamps label as the
// active sub-phase. Safe on a nil receiver and a nil tracker.
func (r *resolvePassTimer) enter(label string) {
	if r == nil {
		return
	}
	r.closeCurrent()
	r.label = label
	r.since = time.Now()
	r.trk.SubPhase(label)
}

// closeCurrent records the elapsed time of the in-flight pass.
func (r *resolvePassTimer) closeCurrent() {
	if r.label == "" {
		return
	}
	if r.traced {
		r.rows = append(r.rows, fmt.Sprintf("%s=%.3fs", r.label, time.Since(r.since).Seconds()))
	}
	r.label = ""
}

// finish closes the last pass and, under memtrace, emits one machine-greppable
// stderr line with every pass's wall time. Call it immediately before the next
// PhaseStart.
func (r *resolvePassTimer) finish() {
	if r == nil {
		return
	}
	r.closeCurrent()
	if r.traced && len(r.rows) > 0 {
		fmt.Fprintf(os.Stderr, "grafel: resolve-pass-timing %s\n", strings.Join(r.rows, " "))
	}
	r.rows = nil
}

// indexParseCoreBudget returns the number of cores the BACKGROUND indexing
// path is allowed to spend on tree-sitter parsing: 25% of machine capacity,
// minimum 1.
//
// Thin named wrapper over process.IndexCoreBudget(), which is the ONE
// definition of the 25% rule in the tree (#5960). #5954 computed it inline here
// "pending a canonical shared helper"; this is that helper, so the duplicate is
// gone. Behaviour is unchanged — still max(1, NumCPU/4) — and the interactive
// exemption still lives in ensureParseConcurrencyDefault, not here. The wrapper
// is kept because the name documents WHICH budget this call site means.
//
// See process.IndexCoreBudget for the policy rationale: dynamic rather than the
// old static 3-core cap, so a 32-core box indexes faster than a 4-core one
// while every machine keeps 75% of its capacity. NumCPU, not GOMAXPROCS: this
// budget governs C parsing, and GOMAXPROCS is neither the machine's capacity
// nor a bound on cgo.
func indexParseCoreBudget() int {
	return process.IndexCoreBudget()
}

// ensureParseConcurrencyDefault installs the in-process tree-sitter parse
// concurrency cap for processes that are NOT the daemon. It never lowers or
// overrides a cap that is already installed.
//
// WHY THIS IS REQUIRED (#5954). Removing the global parseMu means N extraction
// workers can now sit inside ts_parser_parse simultaneously. Two bounds that
// look like they would contain that do NOT:
//
//   - The #5630 parse gate was daemon-only. indexstate.SetParseConcurrency had
//     exactly one production caller (daemon.go), and parsegate.go treats cap<=0
//     as UNBOUNDED — so for `grafel index-internal` (the child the scheduler /
//     rebuild path fork-execs) AcquireParseSlot was a no-op.
//   - GOMAXPROCS does not bound cgo. A goroutine inside a cgo call parks in
//     _Gsyscall and the runtime hands its P to another goroutine, so N
//     concurrent cgo calls occupy N OS threads regardless of GOMAXPROCS. Since
//     the extract CPU budget is implemented purely as GOMAXPROCS=<n> in the
//     child's env (sched/subprocess_runner.go, extract/coordinator.go),
//     GRAFEL_EXTRACT_GOMAXPROCS=2 would have given no protection at all
//     against 8 concurrent ts_parser_parse (i.workers is 8).
//
// The parse gate DOES bound cgo, because it is a counting semaphore acquired on
// the Go side and held ACROSS the cgo call (AcquireParseSlot in
// treesitter.parseOfficial, and — since #5954 — around every extractor-level
// parse that bypasses the factory). At most `cap` goroutines can be inside
// ts_parser_parse at once, whatever the scheduler does with Ps and threads.
//
// INTERACTIVE IS EXEMPT. A foreground rebuild is user-initiated and awaited, so
// it may use the whole machine; capping it would only make the human wait. The
// budget applies to the background/CLI-index path, which is the one that runs
// unasked while the developer is working. This mirrors the same Interactive
// signal the extract coordinator already uses (CoordinatorConfig.Interactive).
// Returns the cap now in force (0 = unbounded) and whether THIS call installed
// it — the caller logs only in the latter case, so a run inside the daemon does
// not re-announce the daemon's own ceiling as if it were the index budget.
func ensureParseConcurrencyDefault(interactive bool) (inForce int, installed bool) {
	if existing := indexstate.ParseConcurrencyCap(); existing > 0 {
		return existing, false
	}
	if interactive {
		return 0, false
	}
	budget := indexParseCoreBudget()
	indexstate.SetParseConcurrency(budget)
	return budget, true
}

// allPassNames is used to validate --skip-pass entries.
var allPassNames = []string{
	PassExtract, PassFramework, PassCrossLang, PassTestsWalkUp, PassGraphAlgo, PassBuildDocument, PassRenameDetect, PassEnrichment, PassProcessFlow, PassEventFlow, PassModuleAgg, PassCommitCouple, PassCoupling, PassDepHygiene, PassSharedDB, PassLibBoundary, PassMigrationSeq, PassMigrationOps, PassCoverage, PassEmbed,
}

// fileTask carries one repo-relative path and its absolute counterpart
// through the Pass 1 worker pool.
type fileTask struct {
	relPath string
	absPath string
}

// classifiedFile is a file that survived classification — extractors will
// be run against it in Pass 1 and Pass 3.
//
// Deliberately carries NO tree-sitter parse tree (#5954). Trees are
// CGo-allocated (~19.7 bytes of C heap per source byte, ~89 B/node) and
// go-tree-sitter@v0.24.0 installs no finalizer, so a tree retained here is a
// tree held for the life of the process — retaining one per classified file
// cost ~1.4-1.6 GB of peak RSS on a 24k-file corpus. Pass 1 closes each tree
// the moment its extractor returns (see runPass1ExtractWithProgress); Pass 3
// cross-language extractors read only Path/Content/Language and never touch
// a tree. Do not re-add a tree field here.
type classifiedFile struct {
	relPath  string
	absPath  string
	language string
	content  []byte
}

// Indexer owns the pass-by-pass orchestration. Constructing a fresh Indexer
// per Index() call keeps state (counters, configuration) local.
type Indexer struct {
	repoTag string
	// absRepo is the on-disk root of the repo this run is indexing. Set by
	// Run; empty when buildDocument is driven directly by a unit test.
	// Consumed by the #6450 index-time base-URL constant fold, which needs
	// to read source files for the substrate sniffer.
	absRepo    string
	classifier *classifier.Classifier
	parser     *treesitter.ParserFactory
	detector   *engine.Detector
	skipPasses map[string]bool
	workers    int

	// enableRepairCandidates toggles ADR-0015 phase-1 repair_edge emission
	// (issue #544). Default false during phase-1 rollout — the reader
	// (#545) lands before the writer is flipped on by default in #546.
	enableRepairCandidates bool

	// enableRepairApply toggles ADR-0015 phase-1 repair.json read+apply
	// (issue #545). Default false during phase-1 rollout. When true, the
	// indexer reads <repo>/.grafel/repair.json BEFORE the final
	// disposition reclassification pass and rewrites edges per the
	// trust-model rules in docs/specs/repair-trust-model.md.
	enableRepairApply bool

	// BEHAVIOUR AND PERSISTENCE ARE DELIBERATELY SEPARATE FIELDS (#6207). Until
	// #6207 a single pair set together by WithIncremental gated BOTH halves at
	// once, and the conflation was the defect: there was no way to say "record
	// what this run indexed" without also saying "index only the delta".
	//
	// incremental + incrementalStateDir gate diff-aware BEHAVIOUR (issue
	// #1339): load the manifest from incrementalStateDir, filter the walk down
	// to changed files, stream in the previous graph from the same dir and
	// carry forward the unchanged-file entities. Both are needed; neither is
	// meaningful alone. Set only by WithIncremental.
	//
	// persistManifest gates manifest PERSISTENCE on its own, with no state dir
	// of its own — see WithManifestPersist for why it takes no directory.
	// persistManifest && !incremental is the scheduler's fallback full index:
	// index EVERYTHING, then record ground truth for every file. That is not a
	// contradiction — it is what a full reindex has just earned the right to
	// write.
	incremental         bool
	incrementalStateDir string // directory that holds file-index.json + graph.fb
	persistManifest     bool

	// walkedFiles + diffManifest are handed from Run to commitManifest. They
	// exist because the manifest can only be written once graph.fb is durably
	// on disk, and that write happens in Index AFTER Run has returned — see
	// commitManifest for the failure mode that ordering prevents.
	walkedFiles  []string
	diffManifest *idiff.Manifest

	// walkStamps is the manifest entry for every file this run examined,
	// captured AT THE MOMENT the walk looked at it (#6212). It is what
	// commitManifest stamps from, and the reason commitManifest hashes nothing.
	//
	// Written by the extraction workers (classifyAndReadWithProgress) and by the
	// subprocess-extract branch, both in Run; read once, single-threaded, in
	// commitManifest. The mutex covers the concurrent writers only.
	walkStampMu sync.Mutex
	walkStamps  map[string]idiff.FileEntry

	// generatedSet resolves which source files are machine-generated (#6329).
	// Created in Run once absRepo is known; see mergePassRecords, which is the
	// one place the flag is applied to a full-index run.
	generatedSet *generated.Set

	// failedExtractions names the walked files whose Pass-1 extraction returned
	// an error this run (#6209). i.stats.failed counts them; only a NAME can be
	// acted on, because the manifest entry that has to be qualified is per-file.
	//
	// Written by the extraction workers, read once in commitManifest, which
	// hands it to diff.ApplyStampsAndFailures so those files' stamps carry a
	// consecutive-failure count instead of passing for successfully indexed.
	//
	// TWO FAILURE CLASSES ARE DELIBERATELY NOT IN HERE.
	//
	// A file that could not be READ is never stamped in the first place (see the
	// osReadFile branch in classifyAndReadWithProgress), so it already
	// re-presents as new on every pass — over-reporting, never staleness — and
	// marking it would be no improvement. It also could not be bounded: with no
	// bytes there is no stamp to hang a count on.
	//
	// The GRAFEL_SUBPROC_EXTRACT path reports only extract.Result.Failed, a
	// COUNT — the children never send the paths back. Naming them needs an IPC
	// field, exactly as the hash-return note on stampUnreadFiles says, and that
	// path is off by default.
	failedMu          sync.Mutex
	failedExtractions map[string]bool

	// incrementalCarryForwardEntities holds the previous-graph entities sourced
	// from UNCHANGED files during an incremental reindex. buildDocument seeds the
	// resolver index with their (name, kind) → stable-ID identity so that
	// bare-name edge endpoints emitted by the freshly re-extracted changed files
	// — most importantly the NestJS/Angular constructor-DI INJECTED_INTO edge,
	// whose FromID is the provider *name* ("FooService") attached to the consumer
	// (controller) record — resolve to the provider's stable entity ID even when
	// the provider file is out of the changed-file extraction scope. Without this
	// the edge's FromID stays a bare name, never matches survivingIDs in
	// mergeIncrementalPrevDoc, and the controller's inbound DI edge is silently
	// dropped from the persisted graph (deploy-9 REFUTED item-2). The IDs are
	// stable across runs (EntityID = hash(repo,kind,name,sourceFile)), so an
	// edge rewritten to a carried-forward entity's ID points at a node that
	// mergeIncrementalPrevDoc re-adds to the merged document. Nil on full runs.
	incrementalCarryForwardEntities []types.EntityRecord

	// exportFB is a deprecated no-op field retained for back-compat with
	// existing callers that pass WithExportFB(true). graph.fb is now
	// always written; setting exportFB has no additional effect.
	// Removed in the next major release (issue #808 / ADR-0016 flip-day).
	exportFB bool // DEPRECATED: always-on since #808; kept for back-compat

	// exportJSON enables emission of graph.json alongside graph.fb.
	// By default only graph.fb is written (ADR-0016 flip-day).
	// Pass --export-json to also emit graph.json (useful for FB validation).
	exportJSON bool

	// customExtractors, when true, dispatches the internal/custom/**
	// framework extractors on the in-process extract path for THIS Indexer
	// only — the same body the GRAFEL_INPROC_CUSTOM_EXTRACTORS env gate
	// controls process-wide (see inproc_custom.go and the call site in
	// classifyAndReadWithProgress). Either switch enables it; neither
	// disables the other. Set by WithCustomExtractors.
	customExtractors bool

	// printSkipped, when true, emits one [skip] line to stderr for each
	// directory that was skipped at walk-time (issue #805). Shows which
	// rule caused the skip (.gitignore, hardcoded, .grafelignore).
	printSkipped bool

	// additionalSkipDirs is the per-group fleet.json additional_skip_dirs
	// list; merged into the hard-coded skip list at walk-time.
	additionalSkipDirs []string

	// publisher receives structured progress events at every pipeline phase
	// boundary and at every TickEveryNFiles interval during AST extraction.
	// Defaults to progress.NoOpPublisher so callers that do not wire a sink
	// pay zero overhead.
	publisher progress.Publisher
	groupSlug string // forwarded to every Event.GroupSlug
	repoSlug  string // forwarded to every Event.RepoSlug; defaults to repoTag
	runToken  string // forwarded to every Event.RunToken (#5937); "" = no per-run token

	// Statistics — populated as passes run, surfaced in the final summary.
	stats indexerStats

	// Resolver state retained between buildDocument and post-synthesis
	// reclassification. The resolver tags every endpoint with a Disposition
	// (VERIFY-2-PREP / issue #56); after external.Synthesize rewrites bare
	// names to "ext:<pkg>" we re-walk the final edge set to reclassify any
	// stubs that became external placeholders.
	resolveIdx        *resolve.Index
	resolveStats      resolve.Stats
	finalDispositions resolve.Stats

	// moduleMarkers is built from the walked file list before extraction and
	// used by buildDocument to derive Properties["module"] for every entity.
	// Issue #1381 — module extraction via path rollup.
	moduleMarkers module.MarkerSet

	// ingestDocs enables deterministic markdown documentation ingestion
	// (#4306, Layer 1 of epic #4294). OFF by default. When true, after the
	// code graph is built the indexer discovers in-repo *.md files, parses
	// each into a SCOPE.MarkdownDocument node + heading-delimited SCOPE.Section
	// nodes (CONTAINS hierarchy), and links section text to code entities by
	// EXACT identifier-token match (MENTIONS edges). Fully deterministic — no
	// LLM calls, no network. When false this whole subsystem is skipped and
	// adds zero overhead. Also honored via env GRAFEL_INGEST_DOCS=1|true.
	ingestDocs bool

	// singleModuleLabel, when non-empty, forces every entity in this repo into
	// ONE module row instead of the per-directory path rollup. It is set for
	// PLAIN (non-monorepo) repos so the per-module progress + Group-by-Module
	// graph treat the repo as a single unit (issue #1628). For TRUE monorepos
	// it stays empty and module.Derive's per-package rollup applies.
	singleModuleLabel string

	// interactive marks this index run as an explicit, user-triggered
	// foreground rebuild (`grafel rebuild` / `grafel index`) rather
	// than a background watch/churn-triggered reindex (#5135). It is forwarded
	// to the subprocess extract coordinator (when GRAFEL_SUBPROC_EXTRACT=1)
	// so the foreground rebuild runs at the higher GRAFEL_REBUILD_GOMAXPROCS
	// cap (default = host cores) instead of the throttled background
	// GRAFEL_EXTRACT_GOMAXPROCS cap (default 1 since #5960). Wired via
	// WithInteractive.
	interactive bool

	// coverageCfg is the per-group coverage-ingestion config (#5061/#5036).
	// Zero value means "discover the default report path" (coverage/lcov.info);
	// a populated Config honors its explicit report_paths globs. The coverage
	// enrichment pass (Pass 8.12) is a strict no-op when no LCOV report is
	// found — only the static test-reachability sub-pass (#5037) then runs,
	// since it needs no external report. Wired via WithCoverage.
	coverageCfg coverage.Config
}

// IndexOption configures optional behaviour on the Indexer. Used as a
// functional-option list on Index() so existing callers don't have to thread
// new parameters through every site.
type IndexOption func(*Indexer)

// WithRepairCandidates toggles ADR-0015 phase-1 repair_edge emission
// (issue #544). When true the indexer appends repair_edge entries to
// <repo>/.grafel/enrichment-candidates.json for every bug-extractor /
// bug-resolver disposition. Default is false.
func WithRepairCandidates(enabled bool) IndexOption {
	return func(i *Indexer) { i.enableRepairCandidates = enabled }
}

// WithRepairApply toggles ADR-0015 phase-1 repair.json apply (issue #545).
// When true the indexer reads <repo>/.grafel/repair.json before the
// final disposition reclassification and applies allowlisted rewrites
// (bind_to_entity / reclassify_* / abandon) per the trust model. Default
// is false; pair with --enable-repair-candidates for the full
// emit → human/agent writes → apply loop.
func WithRepairApply(enabled bool) IndexOption {
	return func(i *Indexer) { i.enableRepairApply = enabled }
}

// WithExportFB is a deprecated no-op. graph.fb is now always written
// (ADR-0016 flip-day, issue #808). The flag is kept for back-compat
// and will be removed in the next major release.
//
// Deprecated: graph.fb is the default; use WithExportJSON(true) if you also need graph.json.
func WithExportFB(enabled bool) IndexOption {
	return func(i *Indexer) {
		if enabled {
			fmt.Fprintf(os.Stderr,
				"grafel: --export-fb is deprecated; graph.fb is now written by default (ADR-0016 flip-day). Use --export-json if you also need graph.json.\n")
		}
		i.exportFB = enabled // stored but unused
	}
}

// WithExportJSON enables emission of graph.json alongside graph.fb.
// By default, only graph.fb is written (ADR-0016 flip-day). Pass this to
// also emit graph.json for backward compatibility or validation purposes.
// Default is false (FB-only to save ~7 MB per repo).
func WithExportJSON(export bool) IndexOption {
	return func(i *Indexer) { i.exportJSON = export }
}

// WithCustomExtractors dispatches the internal/custom/** framework
// extractors on the in-process extract path for this Index call only.
//
// It is the per-call equivalent of setting GRAFEL_INPROC_CUSTOM_EXTRACTORS;
// the call site in classifyAndReadWithProgress ORs the two, so an in-process
// caller can opt in without mutating process environment shared with other
// tests running in the same binary.
//
// The default stays OFF. inProcCustomExtractors in inproc_custom.go gives the
// qualitative rationale; the cost figures are at the gate itself, index.go:3944-3945
// (+17.5% wall, +18.2% TotalAlloc, never measured at corpus scale). The one
// caller that opts in is `grafel quality` over a golden fixture — see
// qualityIndexOptions in quality.go.
func WithCustomExtractors(enabled bool) IndexOption {
	return func(i *Indexer) { i.customExtractors = enabled }
}

// WithPrintSkipped enables the --print-skipped flag. When true each
// directory skipped at walk-time is printed to stderr with its rule.
func WithPrintSkipped(enabled bool) IndexOption {
	return func(i *Indexer) { i.printSkipped = enabled }
}

// WithIngestDocs toggles deterministic markdown documentation ingestion
// (#4306, Layer 1 of epic #4294). Default OFF. When true the indexer discovers
// in-repo *.md files and emits SCOPE.MarkdownDocument + SCOPE.Section nodes
// (CONTAINS hierarchy) plus exact-match MENTIONS edges to code entities. Fully
// deterministic — no LLM calls, no network. Wired from the --ingest-docs CLI
// flag; also honored via env GRAFEL_INGEST_DOCS.
func WithIngestDocs(enabled bool) IndexOption {
	return func(i *Indexer) { i.ingestDocs = enabled }
}

// ingestDocsEnvEnabled reports whether GRAFEL_INGEST_DOCS requests doc
// ingestion (accepts "1", "true", "yes", "on"; case-insensitive).
// renameWorkBudget returns the Phase-2 work budget for the rename-detection
// pass (#6087), in Levenshtein DP cells.
//
// GRAFEL_RENAME_WORK_BUDGET overrides algorithms.DefaultRenameWorkBudget. It
// exists for two reasons: an operator who hits truncation on a genuinely huge
// but legitimate delta can raise the ceiling without a rebuild, and tests can
// drive the truncation path in milliseconds instead of needing a fixture large
// enough to burn four billion work units. Unparseable or non-positive values
// fall back to the default rather than disabling the bound — there is no
// unbounded mode.
func renameWorkBudget() int64 {
	raw := strings.TrimSpace(os.Getenv("GRAFEL_RENAME_WORK_BUDGET"))
	if raw == "" {
		return algorithms.DefaultRenameWorkBudget
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		fmt.Fprintf(os.Stderr,
			"grafel: ignoring invalid GRAFEL_RENAME_WORK_BUDGET=%q; using default %d\n",
			raw, algorithms.DefaultRenameWorkBudget)
		return algorithms.DefaultRenameWorkBudget
	}
	return v
}

func ingestDocsEnvEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GRAFEL_INGEST_DOCS"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// WithAdditionalSkipDirs extends the hard-coded walk-time skip list
// with per-group names from fleet.json's additional_skip_dirs field.
func WithAdditionalSkipDirs(dirs []string) IndexOption {
	return func(i *Indexer) { i.additionalSkipDirs = dirs }
}

// WithPublisher wires a progress.Publisher into the indexer. The publisher
// receives one Event per pipeline phase boundary, per N=TickEveryNFiles
// files during AST extraction, and per algorithm entry/exit. Defaults to
// progress.NoOpPublisher when not set.
func WithPublisher(pub progress.Publisher) IndexOption {
	return func(i *Indexer) { i.publisher = pub }
}

// WithProgressSlugs sets the group and repo slug forwarded on every progress
// event. Call this alongside WithPublisher when the indexer is running inside
// a daemon rebuild (where the group and slug are known).
func WithProgressSlugs(groupSlug, repoSlug string) IndexOption {
	return func(i *Indexer) {
		i.groupSlug = groupSlug
		i.repoSlug = repoSlug
	}
}

// WithRunToken sets the per-run identity (RebuildArgs.ProgressToken, when the
// caller has one) forwarded to Event.RunToken on every progress event this
// indexer's Tracker emits (#5937). Call alongside WithPublisher/
// WithProgressSlugs; the default "" leaves RunToken empty on every event,
// matching pre-#5937 behaviour for runs with no token (e.g. background/
// watcher reindexes).
func WithRunToken(tok string) IndexOption {
	return func(i *Indexer) { i.runToken = tok }
}

// WithIncremental enables diff-aware re-indexing (issue #1339). The indexer
// loads `.grafel/file-index.json` from stateDir, filters the walked file
// list down to only files whose SHA-256 content hash changed since the last
// successful run, and updates the manifest after writing. When the manifest is
// absent or empty every file is processed (equivalent to a full rebuild).
//
// Pass stateDir = daemon.StateDirForRepo(repoPath) to use the standard per-repo
// state directory.
func WithIncremental(stateDir string) IndexOption {
	return func(i *Indexer) {
		i.incremental = true
		i.incrementalStateDir = stateDir
		i.persistManifest = true
	}
}

// WithManifestPersist makes this run record what it indexed — a complete
// file-hash manifest — WITHOUT enabling diff-aware re-indexing (#6207). Every
// file is walked, parsed and indexed exactly as on any full index; the only
// thing that changes is that the next incremental pass is left a baseline.
//
// IT TAKES NO DIRECTORY, ON PURPOSE. The manifest is written beside the graph
// this run produced — filepath.Dir(outPath), the same expression Index uses for
// every other artifact — and a caller cannot point it somewhere else. An
// earlier version of this option took a state dir and the scheduler passed it
// the ref captured at ENQUEUE time, while the graph landed under whatever HEAD
// resolved to at INDEX time; a `git checkout` in that window put the manifest
// and the graph it describes in different ref directories, leaving one branch
// with no baseline and the other with a manifest stamped from the wrong branch.
// The manifest describes the graph; it is not separately addressable.
//
// This option exists because WithIncremental used to be the only way to get a
// manifest written at all, and it buys persistence at the price of behaviour.
// The scheduler's fallback full index needs exactly the opposite trade: it fell
// back BECAUSE the delta could not be trusted incrementally, so it must not be
// diff-aware — but it is precisely the run whose output the manifest should
// describe, because it has just re-established ground truth for the whole repo.
//
// Before this option existed, daemonSchedulerIndex set nothing, so the fallback
// wrote no manifest and the manifest on the scheduler path was advanced only by
// TryIncremental — including on the passes where it declined to do any work,
// which is why the reject path had to carry loop guards (#5667, #5668) to
// compensate.
func WithManifestPersist() IndexOption {
	return func(i *Indexer) {
		i.persistManifest = true
	}
}

// WithCoverage wires the per-group coverage-ingestion config (#5061) into the
// coverage enrichment pass (Pass 8.12). cfg mirrors SonarQube's report-path
// model: grafel does NOT run tests, it ingests the LCOV report CI already
// emits (see internal/coverage.Config). When cfg is the zero value the pass
// discovers the conventional coverage/lcov.info under the repo root; when no
// report is found the LCOV sub-pass no-ops and only static test-reachability
// (#5037) runs. Pass --skip-pass=coverage to disable the whole pass.
func WithCoverage(cfg coverage.Config) IndexOption {
	return func(i *Indexer) { i.coverageCfg = cfg }
}

// WithInteractive marks an index run as an explicit, user-triggered foreground
// rebuild rather than a background watch/churn-triggered reindex (#5135). When
// true (and GRAFEL_SUBPROC_EXTRACT=1), the subprocess extract coordinator
// runs at the higher GRAFEL_REBUILD_GOMAXPROCS cap (default = host cores)
// and wider fan-out, instead of the throttled background extract cap
// (GRAFEL_EXTRACT_GOMAXPROCS, default 1 since #5960). Set by the daemon's
// rebuild RPC path and by
// the `grafel index` CLI; left false on the scheduler's reactive reindex.
func WithInteractive(enabled bool) IndexOption {
	return func(i *Indexer) { i.interactive = enabled }
}

type indexerStats struct {
	files     int
	processed int
	extracted int
	skipped   int
	failed    int
	pass1Rels int
	pass2Rels int
	pass3Rels int

	// Per-extractor relationship counters (PORT-2-FIX-2 / issue #25):
	// pass1RelsByLang["python"]    = 1234
	// pass3RelsByExt["httpclient"] = 56
	pass1RelsByLang map[string]int
	pass3RelsByExt  map[string]int

	// parseCanary accumulates per-language ERROR-node stats across the run
	// for the A4 parse-error canary (#5414). Both the in-process and the
	// subprocess paths fold their parses in here; the sidecar build runs
	// baseline-vs-current spike detection on the snapshot.
	parseCanary *treesitter.ParseErrorCanary

	// PORT-EXT: external-entity synthesis counters (Pass 4.5).
	extSynth external.Stats

	// Pass1Plumbed counters (issue #2447): track how many files had
	// FileInput.Pass1Entities non-empty (True) vs empty (False) when
	// Detector.Detect was called for Pass 2.5.
	//
	// Heterogeneous-repo semantics (issue #2464): runPass25FrameworkRules
	// runs Pass 2.5 against ALL classified files regardless of language.
	// Non-Django files (Go, JS, TypeScript, etc.) never produce
	// SCOPE.Schema(subtype=field) entities in Pass 1, so they legitimately
	// contribute to FalseCount. A non-zero FalseCount is therefore EXPECTED
	// on any multi-language repository and does NOT indicate a plumbing bug.
	pass1PlumbedTrue  int
	pass1PlumbedFalse int

	// unsupportedExt aggregates, by extension, the files the classifier
	// dropped because no extractor claims them (#6338). Counted at the point
	// the walk makes that decision — nothing downstream ever sees these files,
	// so there is nowhere else it could be counted. Aggregated, never a file
	// list: the originating report was 672 .vb files.
	unsupportedExt *classifier.UnsupportedTally
}

// Index walks repoPath, runs the orchestrated passes, and writes the
// resulting entity/relationship graph to outPath (or the default
// <repo>/.grafel/graph.json). repoTag is stored on every entity; an
// empty value falls back to filepath.Base(repoPath). skipPasses is the
// (possibly empty) set of pass names to skip — see allPassNames. When
// pretty is true, graph.json (and the graph-stats.json sidecar) are
// indented for human readability; the default is minified JSON.
// enrichmentOrderHook is a test-only observability hook invoked at
// enrichment lifecycle boundaries ("graph_written", "enrichment_started",
// "enrichment_done") so tests can assert graph.fb is written strictly
// BEFORE Pass 6 enrichment-candidate emission runs or completes (#5720).
// Production code always uses the no-op default; tests swap it out and
// restore it afterward.
var enrichmentOrderHook = func(stage string) {}

// enrichmentEmittersForBG returns the CandidateEmitter set used by
// runPass6EmitEnrichmentCandidatesBG. Test-only override seam (defaults to
// enrichment.DefaultEmitters): tests can swap this to inject a panicking
// emitter and assert the background trickle path cleans up its temp file
// even when it never reaches Close() (#5739 item d).
var enrichmentEmittersForBG = enrichment.DefaultEmitters

func Index(repoPath, outPath, repoTag string, skipPasses []string, pretty bool, jsonStats bool, opts ...IndexOption) error {
	absRepo, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("resolve repo path: %w", err)
	}
	info, err := os.Stat(absRepo)
	if err != nil {
		return fmt.Errorf("stat repo: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("repo path %q is not a directory", absRepo)
	}

	if repoTag == "" {
		repoTag = filepath.Base(absRepo)
	}
	// #1626: relocate any pre-existing in-repo `.grafel/` graph
	// artifacts into the external store before resolving output paths,
	// so incremental loads + rename detection see the migrated state and
	// the repo working tree is left clean.
	if migrated, mErr := daemon.MigrateInRepoState(absRepo); mErr != nil {
		fmt.Fprintf(os.Stderr, "grafel: in-repo state migration: %v\n", mErr)
	} else if migrated {
		fmt.Fprintf(os.Stderr, "grafel: migrated in-repo .grafel → %s\n", daemon.StateDirForRepo(absRepo))
	}
	if outPath == "" {
		outPath = daemon.GraphPathForRepo(absRepo)
	}

	skipSet, err := parseSkipPasses(skipPasses)
	if err != nil {
		return err
	}

	cls := classifier.New(nil)
	parser := treesitter.NewParserFactory(nil)

	rules, err := engine.LoadAllRules()
	if err != nil {
		return fmt.Errorf("load engine rules: %w", err)
	}
	detector := engine.New(rules)

	idx := &Indexer{
		repoTag:    repoTag,
		classifier: cls,
		parser:     parser,
		detector:   detector,
		skipPasses: skipSet,
		workers:    8,
		stats: indexerStats{
			pass1RelsByLang: make(map[string]int),
			pass3RelsByExt:  make(map[string]int),
			parseCanary:     treesitter.NewParseErrorCanary(),
			unsupportedExt:  classifier.NewUnsupportedTally(),
		},
	}
	for _, opt := range opts {
		opt(idx)
	}

	// #5954 — bound concurrent in-process tree-sitter parses on the non-daemon
	// path. Placed AFTER the options are applied because the budget depends on
	// idx.interactive: a foreground, user-awaited rebuild is deliberately
	// exempt. See ensureParseConcurrencyDefault.
	//
	// The one-line log mirrors the daemon's own "cpu-tune: in-process parse
	// concurrency cap=N" so the ceiling in force is observable in a child's
	// stderr instead of having to be inferred. Only emitted when this call is
	// what installed the cap.
	if cap, installed := ensureParseConcurrencyDefault(idx.interactive); installed {
		fmt.Fprintf(os.Stderr, "grafel: cpu-tune: in-process parse concurrency cap=%d (25%% of %d cores, #5954)\n",
			cap, runtime.NumCPU())
	}

	// #4306: env fallback for the opt-in markdown-ingestion flag, so the
	// subsystem can be exercised without threading a new field through the
	// daemon RPC proto (deploy-deferred). The CLI --ingest-docs flag and any
	// WithIngestDocs(true) option take precedence; the env var only flips it ON.
	if !idx.ingestDocs && ingestDocsEnvEnabled() {
		idx.ingestDocs = true
	}

	// #5692: measure the extraction pipeline wall-clock so it can be persisted
	// into the graph-stats.json sidecar (extract_ms) for index-timing
	// observability. Pure measurement — no behaviour change to indexing.
	extractStart := time.Now()
	doc, err := idx.Run(context.Background(), absRepo)
	if err != nil {
		return err
	}
	extractMS := time.Since(extractStart).Milliseconds()

	// Phase 0 git metadata (#2088). Capture HEAD ref + SHA + worktree flag
	// and stamp them onto the document BEFORE the rename-detect pass so
	// all on-disk representations (graph.fb and graph.json) carry them.
	// Non-git directories return a zero-value Info; the Document fields stay
	// empty, which is the correct default for old readers.
	{
		gi := gitmeta.Capture(absRepo)
		doc.IndexedRef = gi.Ref
		doc.IndexedSHA = gi.SHA
		doc.IsWorktree = gi.IsWorktree
		// M4 sparse-checkout (#2181): stamp the coverage status so dashboard /
		// MCP readers can surface the "partial" badge without re-probing git.
		// ProbeRepo is cheap (2-3 git config reads with a 2s timeout each);
		// calling it here keeps the gitmeta block self-contained.
		si := gitmeta.ProbeRepo(absRepo)
		doc.CoverageStatus = si.CoverageStatus()
		if gi.SHA != "" {
			fmt.Fprintf(os.Stderr, "grafel: git HEAD %s @ %s (worktree=%v)\n",
				gi.SHA, func() string {
					if gi.Ref == "" {
						return "detached"
					}
					return gi.Ref
				}(), gi.IsWorktree)
		}
	}

	// #6087 — captured here so the truncation flag can be persisted into the
	// graph-stats.json sidecar further down. Zero value (Truncated=false) is
	// the correct default when the pass is skipped or no prior graph exists.
	var renameStats algorithms.RenameStats

	// Pass 5.5 — rename detection (#1344). Load the previous graph from disk
	// and compare it with the freshly-built doc to detect entity renames,
	// moves, and splits. Runs BEFORE the final sort and disk write so the
	// emitted RENAMED_FROM edges are included in graph.fb / graph.json.
	// The pass is append-only and safe to skip with --skip-pass=rename-detect.
	if !skipSet[PassRenameDetect] {
		graphDir := filepath.Dir(outPath)
		if prevDoc, err := graph.LoadGraphFromDir(graphDir); err == nil {
			renameStats = algorithms.DetectRenamesBounded(prevDoc, doc, renameWorkBudget())
			if renameStats.Renames > 0 {
				fmt.Fprintf(os.Stderr,
					"rename-detect: %d rename(s) detected (moves=%d splits=%d)\n",
					renameStats.Renames, renameStats.Moves, renameStats.Splits)
			}
			// #6087 — the pass is work-budgeted. When the budget ran out the
			// result is a PARTIAL rename scan, so say so explicitly: without
			// this line a truncated run is indistinguishable from "no renames
			// existed", which is the confident-wrong-answer failure mode.
			// The same fact is persisted machine-readably into
			// graph-stats.json below, for consumers that are not a human
			// tailing stderr.
			if renameStats.Truncated {
				fmt.Fprintf(os.Stderr,
					"rename-detect: TRUNCATED — work budget %d exhausted after %d unit(s) over %d pair(s); %d added entities not examined, %d candidate pair(s) skipped. "+
						"Rename detection is INCOMPLETE for this run: the prior graph is largely dissimilar to the new one (#6087)\n",
					renameStats.WorkBudget, renameStats.WorkUsed, renameStats.PairsExamined,
					renameStats.AddedSkipped, renameStats.PairsSkipped)
			}
		}
		// If no previous graph exists (first run) or it cannot be loaded,
		// we simply skip rename detection — this is not an error.
	}

	// Carry-forward of Pass-4 community/algorithm attributes (#1620).
	//
	// The daemon's fast reactive re-index runs with --skip-pass=graph-algo so
	// a freshly-saved file becomes queryable within seconds; the full algo
	// pass is debounced and may be cancel/rescheduled by subsequent writes. If
	// we wrote graph.fb with empty community data here, the live graph would be
	// left community-free between the fast index and the (possibly never-firing)
	// algo pass — grafel_clusters would return [] and the docs skill would
	// fall back to dir-derived modules.
	//
	// To avoid that, when the algo pass is skipped we copy the prior graph's
	// per-entity community_id/pagerank/centrality/flags (matched by stable
	// entity ID) plus the aggregate Communities list and AlgorithmStats onto
	// the freshly-built doc. New entities (no prior match) simply stay
	// un-annotated until the next full algo pass runs — never worse than the
	// pre-fix behaviour where ALL entities lost their community.
	if skipSet[PassGraphAlgo] {
		graphDir := filepath.Dir(outPath)
		if prevDoc, perr := graph.LoadGraphFromDir(graphDir); perr == nil && prevDoc != nil {
			carryForwardAlgoAttrs(doc, prevDoc)
		}
	}

	if !skipSet[PassBuildDocument] {
		// Issue #481 — belt-and-braces final sort. Even with every fan-in
		// already sorted, external.Synthesize appends placeholders and Pass 4
		// attaches per-entity attributes via map lookups; resort by canonical
		// IDs so the on-disk bytes are stable across runs of the SAME repo.
		sortDocumentForEmission(doc)

		// ADR-0016 flip-day (#808): always emit graph.fb first.
		// graph.json is emitted alongside unless --skip-json was passed.
		// #5891 gen layout: write a NEW graph.<gen>.fb + flip the `current`
		// pointer instead of overwriting a fixed graph.fb. fbPath is the gen
		// file actually written (used below for the identical-mtime stamp).
		fbPath, fbErr := writeGraphGen(filepath.Dir(outPath), doc)
		if fbErr != nil {
			fmt.Fprintf(os.Stderr, "grafel: graph.fb write failed: %v\n", fbErr)
			// Non-fatal — we still try to write graph.json so the system
			// remains functional. If both fail, the error from graph.json
			// propagates below.
		} else {
			fmt.Fprintf(os.Stderr, "grafel: wrote %s\n", fbPath)
			// #6207 — the manifest is committed HERE, and only here: the graph
			// it describes is now durably on disk and the `current` pointer has
			// been flipped to it. Everything above this line is best-effort
			// work that can still be thrown away.
			idx.commitManifest(absRepo, filepath.Dir(outPath))
		}

		// #5720 — the graph is now loadable/queryable (graph.fb is on disk),
		// so enrichment-candidate emission (Pass 6's expensive part) can run.
		// enrichmentOrderHook fires unconditionally, even when the pass is
		// skipped, so an ordering test can assert "graph.fb written" happens
		// exactly once per index run without depending on enrichment being
		// enabled.
		//
		// Small graphs (<= enrichment.InlineEntityThreshold entities) run
		// inline, synchronously, right here — cheap enough not to bother with
		// a background handoff. Large graphs are hand off to
		// enrichment.DefaultScheduler, which runs them on a silent,
		// single-worker, paced background goroutine that never blocks this
		// function's return; a subsequent index of the SAME repo supersedes
		// (cancels) any still-running job before starting its own, so no
		// orphaned goroutine and no stale-over-fresh write can occur.
		enrichmentOrderHook("graph_written")
		if !skipSet[PassEnrichment] {
			if doc.Stats.Entities <= enrichment.InlineEntityThreshold {
				enrichmentOrderHook("enrichment_started")
				idx.runPass6EmitEnrichmentCandidates(doc, absRepo)
				enrichmentOrderHook("enrichment_done")
			} else {
				schedKey := daemon.StateDirForRepo(absRepo)
				enrichment.DefaultScheduler.Schedule(schedKey, func(ctx context.Context) {
					enrichmentOrderHook("enrichment_started")
					idx.runPass6EmitEnrichmentCandidatesBG(ctx, doc, absRepo)
					enrichmentOrderHook("enrichment_done")
				})
			}
		}

		// #5267: record the canonical absolute source path in the top-level
		// `<root>/root.json` manifest so the orphan-root GC can attribute this
		// root WITHOUT the one-way forward hash map (a removed-from-registry /
		// deleted source path is then still reapable). Best-effort: a failure
		// here never fails the index pass — attribution falls back to the
		// forward map for this root.
		if err := daemon.WriteRootManifest(absRepo); err != nil {
			fmt.Fprintf(os.Stderr, "grafel: root manifest write failed (non-fatal): %v\n", err)
		}

		if idx.exportJSON {
			if err := graph.WriteAtomic(outPath, doc, pretty); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "grafel: wrote %s\n", outPath)

			// #1626: stamp graph.fb and graph.json with an IDENTICAL mtime.
			// These are two encodings of the SAME index pass; letting their
			// mtimes diverge made the daemon's fb-vs-json drift check fire a
			// spurious "drift" every load and (combined with in-repo writes)
			// drove an infinite reindex loop. Same mtime → no drift, ever.
			now := time.Now()
			_ = os.Chtimes(fbPath, now, now)
			_ = os.Chtimes(outPath, now, now)
		}

		// Pass 9 — semantic embeddings sidecar (#461 / ADR-0019). Skipped via
		// --skip-pass=embed, and silently skipped when the configured backend
		// is "disabled" (BM25-only mode) or fails to initialise (e.g. builtin
		// requested in a non-simplego build, or HTTP endpoint unreachable).
		// The pass is incremental: only entities whose embed-text hash has
		// changed since the previous embeddings.bin are re-embedded.
		if !skipSet[PassEmbed] {
			if err := writeEmbeddings(doc, absRepo, filepath.Dir(outPath)); err != nil {
				fmt.Fprintf(os.Stderr, "grafel: embeddings: %v\n", err)
			}
		}

		// A4 parse-error canary (#5414): compute the per-language ERROR-node
		// report (baseline-vs-current spike detection) once, so it can ride
		// along in the stats sidecar and a WARN line is logged on a spike.
		canaryRaw, canarySpiked := idx.buildParseErrorCanary()

		// Sidecar: corpus-level metrics for `grafel doctor` and the MCP
		// `graph_stats` tool. Written on EVERY build, not just when Pass 4
		// (graph-algo) ran (#task-31 / count-drift bug): the daemon's
		// reactive/rebuild reindex always runs with --skip-pass=graph-algo,
		// so doc.AlgorithmStats is nil on that path. Gating the whole
		// sidecar write behind AlgorithmStats != nil meant graph.fb was
		// rewritten with fresh counts but graph-stats.json kept an
		// arbitrarily stale count forever — silently diverging from the
		// graph it's supposed to summarise. The counts (+ extract timing +
		// parse-error canary) are now always refreshed from this build; the
		// algorithm-derived fields (communities/modularity/god
		// nodes/articulation points/runtime) are read-merged from the prior
		// sidecar when Pass 4 didn't run, so a skip-graph-algo reindex can
		// never zero out real algorithm data that a previous full build
		// computed.
		prior, _ := graph.LoadSidecar(filepath.Dir(outPath))
		side := buildStatsSidecar(doc, extractMS, canaryRaw, canarySpiked, prior, deterministicGeneratedAt(), renameStats, idx.stats.unsupportedExt.Counts())
		if err := graph.WriteSidecar(outPath, side, pretty); err != nil {
			fmt.Fprintf(os.Stderr, "grafel: sidecar write failed: %v\n", err)
		}
	}

	if jsonStats {
		w := io.Writer(os.Stdout)
		if capturedStats != nil {
			w = capturedStats
		}
		if err := emitJSONStats(w, idx, doc); err != nil {
			return fmt.Errorf("emit json stats: %w", err)
		}
	}
	return nil
}

// capturedStats is a goroutine-local-ish handoff for the daemon: when
// the daemon calls Index(), it sets this to a buffer the IndexFunc can
// return to the RPC caller. The CLI subcommand leaves it nil and stats
// go to os.Stdout as before. We deliberately do NOT introduce a new
// Index() variant — the existing call sites (and tests) keep working
// unchanged, and the daemon path opts in via setCapturedStats around
// its single call.
//
// Safety: Index() is invoked serially today (Phase A's daemon serializes
// jobs). When the per-repo job queue lands in Phase B, the daemon will
// either thread the buffer through explicitly or move stats capture
// into Indexer state. For Phase A the single-writer assumption holds.
var capturedStats io.Writer

// writeGraphGen is fbwriter.WriteGraphGen behind a var so a test can make the
// graph write FAIL — the failure this file's manifest ordering exists to
// survive (#6207). The write is non-fatal by design, so there is no error
// return to inject through and no other way to reach that branch.
var writeGraphGen = fbwriter.WriteGraphGen

// osStat and osReadFile are the extraction worker's stat and read behind vars,
// so a test can make a working-tree write land at a precise point INSIDE the
// per-file sequence (#6212). One indirect call each per file.
//
// They exist because the two properties the stamp rests on are both about
// ORDER, and order is unobservable from outside the loop:
//
//   - the stat is taken BEFORE the read, so the stamp's mtime can only be
//     staler than its content, never fresher. A test hooks osStat to rewrite
//     the file as a side effect; the read then sees post-write bytes and the
//     stamp must pair them with the PRE-write mtime.
//   - the hash is over the bytes the pipeline RECEIVED, not a re-read of the
//     path. A test hooks osReadFile to return bytes that are not on disk; a
//     re-reading implementation stamps the disk's bytes and dies.
//
// Without these, replacing `content` with a fresh os.ReadFile of the same path
// — same value, different moment — passes the whole suite.
var (
	osStat     = os.Stat
	osReadFile = os.ReadFile
)

// recordWalkStamp records the manifest entry for one walked file, computed at
// the moment the pipeline examined it. Safe to call from the extraction workers.
//
// FIRST WRITE WINS, and that is a correctness rule, not an optimisation. A .py
// file is read TWICE per run — once by the cross-file registry pre-pass in
// runPass1ExtractWithProgress, once by the worker below — and both reads feed
// the graph (the registry drives extractBaseClasses' cross-file INHERITS and the
// DRF bare-Route suppression). If a write lands between them the two halves of
// the graph disagree, and the only stamp that cannot LEAD the graph is the
// EARLIEST one: stamping the first read means a later divergence re-presents the
// file as changed and it is re-extracted, where stamping the second would
// hash-match the disk and freeze the registry's wrong contribution in place
// (#6212).
func (i *Indexer) recordWalkStamp(rel string, e idiff.FileEntry) {
	i.walkStampMu.Lock()
	if i.walkStamps == nil {
		i.walkStamps = make(map[string]idiff.FileEntry)
	}
	if _, seen := i.walkStamps[rel]; !seen {
		i.walkStamps[rel] = e
	}
	i.walkStampMu.Unlock()
}

// stampReadFile records the walk stamp for a file whose bytes the pipeline has
// in hand. The SHA-256 is over exactly THOSE bytes — the ones the document is
// built from — and never over a re-read of the path, which is the whole defect:
// the same path at two moments is not the same bytes (#6212). See
// diff.StampBytes for the stat-before-read requirement.
func (i *Indexer) stampReadFile(rel string, content []byte, statSize int64, statMtime int64) {
	i.recordWalkStamp(rel, idiff.StampBytes(content, statSize, statMtime))
}

// stampUnreadFiles records walk stamps for files whose bytes this process never
// sees at all: the GRAFEL_SUBPROC_EXTRACT path, where the children do the
// reading. Files the classifier SKIPS are hashed inline in the worker instead,
// at the moment of the skip decision — see there for why the timing matters.
//
// The manifest must carry an entry for every walked file or the prune's absence
// re-presents it as new on every pass, which is #5667's immortal-entry loop in
// the other direction. go.mod in the #6207 fixture is exactly this case.
func (i *Indexer) stampUnreadFiles(absRepo string, rels []string) {
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for _, rel := range rels {
		wg.Add(1)
		go func(r string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			entry, err := idiff.HashFile(filepath.Join(absRepo, filepath.FromSlash(r)))
			if err != nil {
				// Unreadable or vanished. Leaving it unstamped matches what
				// UpdateManifest did on a hash error: no entry, so the next
				// pass treats it as new. Over-reporting, never staleness.
				return
			}
			i.recordWalkStamp(r, entry)
		}(rel)
	}
	wg.Wait()
}

// recordFailedExtraction names one file whose Pass-1 extraction failed. Safe to
// call from the extraction workers. See failedExtractions for what is and is
// not routed here.
func (i *Indexer) recordFailedExtraction(rel string) {
	i.failedMu.Lock()
	if i.failedExtractions == nil {
		i.failedExtractions = make(map[string]bool)
	}
	i.failedExtractions[rel] = true
	i.failedMu.Unlock()
}

// extractPass1 is the Pass-1 extraction entry point, indirected through a
// package var for the same reason writeGraphGen and osReadFile are: the
// failure it can return is the thing under test, and there is no fixture source
// file that makes extractors.Extract fail on demand and keeps failing
// deterministically pass after pass. Production always holds
// extractors.Extract; only #6209's bounded-retry test replaces it.
var extractPass1 = extractors.Extract

// commitManifest persists the file-hash manifest describing THIS run: every
// walked file, stamped with the hash the WALK computed, with entries for
// anything no longer in the walk pruned. Best-effort — a write failure is
// logged and never fails the index.
//
// ORDERING IS THE POINT (#6207 review). Index calls this only on the success
// branch of fbwriter.WriteGraphGen, never from Run. Run returns before the
// graph is written, and that write is explicitly non-fatal — it can fail on a
// full disk, on EPERM, on a #5891 generation-pointer flip, or simply not happen
// because the index-internal child was SIGKILLed by a ctx cancel (which
// subprocess_runner is emphatic gives the child no chance to clean up).
//
// Writing the manifest before that point creates a PERMANENT stuck state on the
// daemon's automatic healing path. The manifest would say every file is
// indexed; the graph would still be the previous generation. The next
// scheduler pass computes totalChanged == 0, and the #5710 absent-graph guard
// fires on ABSENCE only — the previous generation's graph.fb is still there and
// `current` still points at it — so TryIncremental reports Done over a
// permanently stale graph. Before #6207 the manifest was simply untouched on
// that path, the same changed set re-presented on the next pass, and it healed
// itself.
//
// The residual window has TWO axes and they point in opposite directions. Do
// not read the first as covering the second.
//
// GENERATION: killed between the graph write and this call, the graph is newer
// than the manifest. That is the safe direction, and the same one #6201's
// scoped reject leaves behind — the files re-read as changed and are
// re-extracted. Over-reporting, never staleness.
//
// CONTENT: on every path that BUILDS a graph, the manifest can only ever be
// older than that graph, never newer (#6212). Nothing is hashed here. Every
// stamp was computed at the moment the pipeline read the file, so it describes
// exactly the bytes the document was built from, whatever the working tree has
// done since. A write landing inside this window re-presents as changed on the
// next pass and is picked up, instead of being stamped as indexed and
// hash-matched away. internal/extractors.TryIncremental's Step 9 holds the same
// invariant by the same means.
//
// ONE SITE IS STILL EXEMPT, and it is not this one: the zero-change pass in
// internal/extractors/incremental.go is the last caller of diff.UpdateManifest
// and still sweeps the whole repo. That pass extracts nothing, so it has no
// bytes to stamp from;
// the sweep is there to HEAL the residuals a #6201 scoped reject leaves behind,
// and deleting it is #6206's job, not this one. Until then a write landing
// inside that pass is stamped without being indexed. Do not read the paragraph
// above as covering it.
//
// graphDir is filepath.Dir(outPath) — the directory the graph was just written
// to. It is the destination whenever the run did not read a manifest from
// somewhere else, so a manifest can never describe a graph in another
// directory. See WithManifestPersist for the ref-divergence that motivates it.
//
// FAILED EXTRACTIONS ARE STAMPED BUT NOT SILENT (#6209, was the KNOWN issue
// noted here by the #6207 review). walkedFiles is the raw WALK, so a file whose
// extraction failed is in it just like one that succeeded, and it is stamped at
// the bytes the walk read. On its own that stamp is a lie the next pass cannot
// see through: the hash matches, the file is classified unchanged, and its
// entities are missing from the graph forever with no signal — strictly worse
// than a file that was never stamped, which re-presents every pass and heals.
//
// The stamp still happens, because dropping it is what turns a
// deterministically unparseable file into a re-parse on every pass for the life
// of the repo. Instead the entry carries a consecutive-failure count and the
// file is forced back into the changed set for the next diff.MaxExtractRetries
// passes. Transient failures heal; deterministic ones cost a bounded number of
// retries and then go quiet until their bytes change.
func (i *Indexer) commitManifest(absRepo, graphDir string) {
	if !i.persistManifest {
		return
	}
	dest := i.incrementalStateDir
	if dest == "" {
		dest = graphDir
	}
	if dest == "" {
		return
	}
	m := i.diffManifest
	if m == nil {
		// A full index never loaded one. Start from whatever is on disk — a
		// full walk stamps every file and the prune drops everything else, so
		// the result is the walk either way; loading only keeps the manifest's
		// own metadata continuous. On a DELTA run the loaded manifest carries
		// the stamps for the files this run did not walk, which is why
		// ApplyStamps overlays rather than replaces.
		m = idiff.LoadManifest(dest)
	}
	i.failedMu.Lock()
	failed := i.failedExtractions
	i.failedMu.Unlock()
	exhausted := idiff.ApplyStampsAndFailures(i.walkStamps, i.walkedFiles, failed, m)
	// The one moment worth a line: this file has now failed on every attempt it
	// is going to get, so its entities are missing from the graph and will stay
	// missing until someone edits it. Silence here is what #6209 was about —
	// the count in the manifest is durable but nothing reads it, so without this
	// the file goes dark with no signal at the exact instant it goes dark.
	for _, rel := range exhausted {
		fmt.Fprintf(os.Stderr, "grafel: %s failed extraction %d time(s); no longer retrying — its "+
			"entities are absent from the graph until the file changes (#6209)\n",
			rel, idiff.MaxExtractRetries+1)
	}
	if err := idiff.SaveManifest(dest, absRepo, m); err != nil {
		fmt.Fprintf(os.Stderr, "grafel: save incremental manifest: %v (non-fatal)\n", err)
	}
}

func setCapturedStats(w io.Writer) (restore func()) {
	prev := capturedStats
	capturedStats = w
	return func() { capturedStats = prev }
}

// JSONStats is the machine-readable per-run summary emitted by the
// indexer when `--json-stats` is set. The shape is intentionally flat so
// downstream harnesses (scripts/verify2/run.sh) can aggregate without
// needing to understand the inner Disposition enum.
type JSONStats struct {
	Repo                 string              `json:"repo"`
	Files                int                 `json:"files"`
	Entities             int                 `json:"entities"`
	Relationships        int                 `json:"relationships"`
	Pass1Rels            int                 `json:"pass1_rels"`
	Pass2Rels            int                 `json:"pass2_rels"`
	Pass3Rels            int                 `json:"pass3_rels"`
	DispositionCounts    map[string]int      `json:"disposition_counts"`
	DispositionSamples   map[string][]string `json:"disposition_samples,omitempty"`
	BugRate              float64             `json:"bug_rate"`
	ResolutionRate       float64             `json:"resolution_rate"`
	ExternalSynthesized  int                 `json:"external_synthesized"`
	ExternalUniqueCount  int                 `json:"external_unique_count"`
	ExternalRelsResolved int                 `json:"external_rels_resolved"`

	// Pass1Plumbed counters (issue #2447): track how many files had
	// FileInput.Pass1Entities non-empty (True) vs empty (False) when
	// Detector.Detect was called for Pass 2.5.
	//
	// Heterogeneous-repo semantics (issue #2464): runPass25FrameworkRules
	// runs Pass 2.5 against ALL classified files regardless of language.
	// Non-Django files (Go, JS, TypeScript, etc.) never produce
	// SCOPE.Schema(subtype=field) entities in Pass 1, so they legitimately
	// contribute to FalseCount. A non-zero FalseCount is therefore EXPECTED
	// on any multi-language repository and does NOT indicate a plumbing bug.
	Pass1PlumbedTrue  int `json:"pass1_plumbed_true"`
	Pass1PlumbedFalse int `json:"pass1_plumbed_false"`
}

// emitJSONStats writes a JSONStats record to w. Used by `grafel index
// --json-stats` (writing to os.Stdout) and by the daemon's IndexFunc
// (writing to a bytes.Buffer it can return to the RPC caller).
func emitJSONStats(w io.Writer, idx *Indexer, doc *graph.Document) error {
	counts := make(map[string]int, len(resolve.AllDispositions))
	var total, resolved int
	for _, d := range resolve.AllDispositions {
		n := idx.finalDispositions.DispositionCounts[d]
		counts[d.String()] = n
		total += n
		if d == resolve.DispositionResolved {
			resolved = n
		}
	}
	resRate := 0.0
	if total > 0 {
		resRate = float64(resolved) / float64(total)
	}
	samples := make(map[string][]string, len(resolve.AllDispositions))
	for _, d := range resolve.AllDispositions {
		if s := idx.finalDispositions.DispositionSamples[d]; len(s) > 0 {
			samples[d.String()] = s
		}
	}
	js := JSONStats{
		Repo:                 idx.repoTag,
		Files:                idx.stats.files,
		Entities:             doc.Stats.Entities,
		Relationships:        doc.Stats.Relationships,
		Pass1Rels:            idx.stats.pass1Rels,
		Pass2Rels:            idx.stats.pass2Rels,
		Pass3Rels:            idx.stats.pass3Rels,
		DispositionCounts:    counts,
		DispositionSamples:   samples,
		BugRate:              idx.finalDispositions.BugRate,
		ResolutionRate:       resRate,
		ExternalSynthesized:  idx.stats.extSynth.Synthesized,
		ExternalUniqueCount:  idx.stats.extSynth.UniqueExternals,
		ExternalRelsResolved: idx.stats.extSynth.RelationshipsResolved,
		Pass1PlumbedTrue:     idx.stats.pass1PlumbedTrue,
		Pass1PlumbedFalse:    idx.stats.pass1PlumbedFalse,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(js)
}

// moduleForFile maps a repo-relative file path to the monorepo package root it
// belongs to. It returns the longest package root in pkgRoots that prefixes the
// file (so nested packages win over their parents). When no detected package
// root matches — e.g. a file outside any workspace package — it falls back to
// module.Derive so the file still lands in a sensible module row rather than
// vanishing from the per-module feed. Pure + stateless: safe for concurrent use.
func moduleForFile(currentFile string, pkgRoots []string, markers module.MarkerSet) string {
	f := strings.ReplaceAll(currentFile, "\\", "/")
	f = strings.TrimPrefix(f, "./")
	best := ""
	for _, root := range pkgRoots {
		r := strings.Trim(strings.ReplaceAll(root, "\\", "/"), "/")
		if r == "" || r == "." {
			continue
		}
		if f == r || strings.HasPrefix(f, r+"/") {
			if len(r) > len(best) {
				best = r
			}
		}
	}
	if best != "" {
		return best
	}
	return module.Derive(f, markers)
}

// moduleRowWorthy reports whether a resolved module label deserves its own
// per-module progress bar. Empty (no resolver) and the repo-root sentinel
// (module.RootLabel — stray files at the repo root that belong to no package)
// get no independent row: the wizard has no meaningful bar to show them in, and
// a "_root" row is noise next to the real packages.
func moduleRowWorthy(mod string) bool {
	return mod != "" && mod != module.RootLabel
}

// Run executes the orchestrated pipeline. Each pass is a named method so
// callers (and tests) can reason about per-pass output independently.
func (i *Indexer) Run(ctx context.Context, absRepo string) (*graph.Document, error) {
	start := time.Now()

	// #6329 — the generated-source resolver for this run. It is created here,
	// not lazily at the seam, because absRepo is the only thing it needs and
	// this is the only place that has it. See mergePassRecords.
	i.generatedSet = generated.NewSet(absRepo)

	// #6450 — buildDocument's index-time base-URL constant fold needs the
	// on-disk source root to read files for the substrate sniffer, and Run
	// is the only place that has it. A caller that drives buildDocument
	// directly (unit tests) leaves this empty, which makes the fold an
	// explicit no-op rather than a lie.
	i.absRepo = absRepo

	// Resolve the publisher. Default to NoOp so callers without a sink pay
	// zero overhead (a nil check on every Publish call is more expensive than
	// a virtual dispatch to an empty method).
	pub := i.publisher
	if pub == nil {
		pub = progress.NoOpPublisher{}
	}
	repoSlug := i.repoSlug
	if repoSlug == "" {
		repoSlug = i.repoTag
	}
	trk := progress.NewTracker(pub, i.groupSlug, repoSlug)
	trk.SetRunToken(i.runToken)

	// #5956 memtrace M1/M2: opt-in phase-tagged memstats sampler + heap
	// profiles, gated entirely behind GRAFEL_MEMTRACE_DIR. Start returns nil
	// (no goroutine, no file) when the env var is unset, so this is zero
	// overhead by default. The phase comes from trk.CurrentPhase — the SAME
	// state the UI already reports — so the trace cannot drift from progress
	// events. Best-effort: no error from memtrace can reach the index result.
	memSampler := memtrace.Start("child", trk.CurrentPhase, memtraceLogf)
	defer memSampler.Stop()

	// M4 sparse-checkout (#2181): probe the repo BEFORE the walk so the
	// walker can filter out files that are not present locally. The result is
	// also stored on the Document so the dashboard can show a "partial" badge.
	sparseInfo := gitmeta.ProbeRepo(absRepo)
	if sparseInfo.IsSparse {
		fmt.Fprintf(os.Stderr,
			"grafel: sparse-checkout detected (%d pattern(s)) — indexing partial working tree\n",
			len(sparseInfo.Patterns))
	}

	walkOpts := &walk.Options{
		AdditionalSkipDirs: i.additionalSkipDirs,
		Sparse:             &sparseInfo,
	}
	if i.printSkipped {
		walkOpts.PrintSkipped = os.Stderr
	}

	// Emit a scanning phase event before the walk so the UI shows activity
	// immediately. A second event follows once the walk completes with the
	// real file count.
	trk.PhaseStart(progress.PhaseScan, 0, 0)
	files, walkSkipped, err := walk.WalkRepo(absRepo, walkOpts)
	if err != nil {
		trk.Fail(err.Error())
		return nil, fmt.Errorf("walk repo: %w", err)
	}
	// #6416: non-regular files (FIFOs, devices, sockets) are dropped by the
	// walker's entry-type gate because reading one can block an indexing
	// worker forever. Say so unconditionally — a source-looking file that
	// produced nothing, reported nowhere, is the failure mode #6338 is about.
	if report := walk.IrregularSkipReport(walkSkipped); report != "" {
		fmt.Fprintln(os.Stderr, report)
	}
	i.stats.files = len(files)
	trk.SetFilesTotal(len(files))
	// Emit a second scan event now that we know the total.
	trk.Tick(progress.PhaseScan, len(files), 0, "", 0)
	fmt.Fprintf(os.Stderr, "grafel: discovered %d candidate files in %s\n", len(files), absRepo)

	// Issue #1381 — build package-boundary marker set from the full walked
	// file list.  This is a single O(N) pass over the already-allocated
	// slice; it costs no additional I/O.  The result is stored on the
	// Indexer and consumed by buildDocument to stamp Properties["module"]
	// on every entity.
	i.moduleMarkers = module.BuildMarkerSet(files)

	// Issue #1527 — per-MODULE progress for monorepos. Detect the monorepo
	// package roots once and install a resolver on the tracker so every
	// extraction Tick stamps Event.Module with the file's package root. The UI
	// renders one progress row per module instead of a single aggregate row for
	// the whole repo. Non-monorepos get no resolver (Module stays empty → the
	// UI falls back to a single per-repo row). The resolver is pure + stateless,
	// so it is safe to call from the concurrent extraction workers.
	//
	// Issue #1628 — only TRUE monorepos (a workspace manifest or a real
	// container/multi-package layout) get a per-module breakdown. A PLAIN repo
	// (DetectMonorepo → KindNone) is indexed as a SINGLE unit: we install a
	// resolver that maps every file to one per-repo label, and stamp that same
	// label as Properties["module"] so the Group-by-Module graph does not
	// fragment a plain repo by its top-level directories.
	mono, derr := detect.DetectMonorepo(absRepo)
	isMonorepo := derr == nil && mono.Kind != detect.KindNone && len(mono.Packages) > 1
	if isMonorepo {
		pkgRoots := make([]string, len(mono.Packages))
		copy(pkgRoots, mono.Packages)
		markers := i.moduleMarkers
		trk.SetModuleResolver(func(currentFile string) string {
			return moduleForFile(currentFile, pkgRoots, markers)
		})
	} else {
		// Plain repo → one module row for the whole repo.
		label := repoSlug
		if label == "" {
			label = i.repoTag
		}
		if label == "" {
			label = "_repo"
		}
		i.singleModuleLabel = label
		trk.SetModuleResolver(func(string) string { return label })
	}

	// Incremental mode (issue #1339): filter files down to those whose
	// content hash changed since the last successful index. The manifest is
	// loaded once before filtering; it is written back in saveGraph below
	// only when the index completes successfully.
	//
	// #2719: when files are filtered down to only the changed subset we MUST
	// also load the previous graph here and remember which files are
	// unchanged, so that after the per-pass extraction + buildDocument we can
	// merge the unchanged-file entities + edges back in. Without that merge
	// the resulting graph contains only the changed-file entities and every
	// downstream consumer (algorithms, embeddings, on-disk write) sees a tiny
	// fraction of the real graph.
	var (
		diffManifest *idiff.Manifest
		allFiles     = files // original full list for manifest update
		// incrementalPrev is a STREAM over the previous graph, not a
		// materialised Document (#5954 item 16). LoadGraphFromDir decoded every
		// prior entity and relationship into Go objects up front and held them
		// alive across the whole pipeline, so the prior graph existed twice at
		// the merge point — once in the loaded Document's backing arrays and
		// again in the merged one's. The stream keeps the mmap open instead and
		// decodes a row only while it is being looked at; rows the merge drops
		// are never allocated at all.
		incrementalPrev   *graph.GraphStream
		incrementalChange map[string]bool // changed-file set (forward-slash, relative)
	)
	// The stream owns an mmap for the lifetime of the run; release it on every
	// exit path. Nil-safe, so this is correct on the full-index path too.
	defer func() { _ = incrementalPrev.Close() }()
	// This block is the diff-aware BEHAVIOUR half, and #6207 left its condition
	// exactly as it was: reading a manifest to filter the walk genuinely needs
	// both the flag and a dir to read from. What #6207 changed is the other
	// half — persistence no longer requires passing through here (see
	// commitManifest), so a full index can record what it indexed.
	if i.incremental && i.incrementalStateDir != "" {
		diffManifest = idiff.LoadManifest(i.incrementalStateDir)
		changed, unchanged := idiff.FilterWithGit(absRepo, files, diffManifest)
		if len(unchanged) > 0 {
			diffStats := idiff.Stats{
				Total:     len(files),
				Changed:   len(changed),
				Unchanged: len(unchanged),
			}
			fmt.Fprintf(os.Stderr,
				"grafel: incremental — processing %d of %d files (%.1f%% cache hit)\n",
				diffStats.Changed, diffStats.Total, diffStats.CacheHitRate())
			files = changed
			// #2719: load the previous graph now so the unchanged-file
			// portion can be merged into the current run's doc below. If the
			// load fails we fall back to a full reindex by clearing the
			// changed-set: every file stays in `files` and the rest of the
			// pipeline reindexes everything (the old broken behaviour of
			// silently corrupting the graph is replaced with a safe
			// full-reindex fallback).
			prev, perr := graph.OpenGraphStream(i.incrementalStateDir)
			if perr != nil || prev == nil {
				fmt.Fprintf(os.Stderr,
					"grafel: incremental — previous graph not loadable (%v); falling back to full reindex\n", perr)
				files = allFiles
			} else {
				incrementalPrev = prev
				incrementalChange = make(map[string]bool, len(changed))
				for _, f := range changed {
					incrementalChange[filepath.ToSlash(f)] = true
				}
				// Seed buildDocument's resolver index with the prev-graph
				// entities from UNCHANGED files so cross-file bare-name edge
				// endpoints emitted by the re-extracted changed files (e.g. the
				// constructor-DI INJECTED_INTO provider name on a controller)
				// resolve to their stable provider IDs even though the provider
				// file is out of this run's extraction scope. Only real,
				// source-bearing entities are carried (ext:* synthetics are
				// regenerated downstream and have no stable provider identity).
				//
				// Streamed: only the records that pass the two filters are
				// ever built. Under LoadGraphFromDir the whole prior entity
				// vector was materialised first and this loop then copied a
				// subset out of it.
				cf := make([]types.EntityRecord, 0, prev.EntityCount())
				prev.EachEntity(func(pe graph.Entity) bool {
					if pe.SourceFile == "" {
						return true
					}
					if incrementalChange[filepath.ToSlash(pe.SourceFile)] {
						return true
					}
					// #6119 — QualifiedName is load-bearing here, not cosmetic.
					// resolve.BuildIndex populates idx.byQualifiedName from this
					// field; omitting it meant carried (unchanged-file) entities
					// contributed ZERO entries to that index, which diverged in
					// two directions at once. (1) A stub whose only resolution
					// tier is byQualifiedName could not bind to an unchanged-file
					// entity at all. (2) Worse, byQualifiedName's ambiguity
					// sentinel — a second entity with the same QualifiedName
					// blanks the entry so the stub is left alone — could not see a
					// collision that straddled the changed/unchanged boundary, so
					// the stub bound CONFIDENTLY to the changed-file entity with
					// no signal, where a full rebuild binds a different one.
					// Pinned end-to-end in
					// incremental_carry_qualifiedname_6119_test.go.
					cf = append(cf, types.EntityRecord{
						ID:            pe.ID,
						Name:          pe.Name,
						Kind:          pe.Kind,
						Subtype:       pe.Subtype,
						QualifiedName: pe.QualifiedName,
						SourceFile:    pe.SourceFile,
						Properties:    pe.PropsSnapshot(),
					})
					return true
				})
				i.incrementalCarryForwardEntities = cf
			}
		}
	}

	var (
		pass1Records []types.EntityRecord
		pass2Records []types.EntityRecord
		pass2Rels    []types.RelationshipRecord
		pass3Records []types.EntityRecord
		classified   []classifiedFile
	)

	// Phase F — subprocess extractor path. Gated on
	// GRAFEL_SUBPROC_EXTRACT=1 during the rollout so the in-process
	// path remains the default until benchmarks + quality fixtures
	// confirm byte-identical output. When enabled, the coordinator
	// fork-execs `grafel extract` per language-bucketed batch and
	// returns the merged record set (Pass 1 + 2.5 + 3 combined); the
	// daemon then runs everything from buildDocument onward unchanged.

	// #5954 — per-pass timing inside the composite resolving_refs window. The
	// timer only stamps sub-phase labels (no published events), so this is
	// observability with no behavioural surface. See resolvePassTimer.
	passTimer := newResolvePassTimer(trk)

	trk.PhaseStart(progress.PhaseExtractAST, 0, 0)
	if subprocExtract() {
		// #6212 — the children read the bytes in another process, so this side
		// has no hash to thread out. Take them HERE, before the coordinator
		// starts, rather than at commit time. A write racing the children then
		// leaves the manifest stamping the PRE-write bytes while the graph got
		// the post-write ones: the file re-presents as changed next pass and is
		// re-extracted. Over-reporting, which is the safe direction — the
		// commit-time hash had it the other way round and lost the edit.
		// Threading the real hashes back out of extract.Coordinate would remove
		// even that; it needs an IPC field and this path is off by default
		// (GRAFEL_SUBPROC_EXTRACT).
		i.stampUnreadFiles(absRepo, files)
		res, cerr := extract.Coordinate(ctx, absRepo, files, extract.CoordinatorConfig{
			Concurrency: subprocConcurrency(),
			BatchSize:   subprocBatchSize(),
			SkipPasses:  skipPassNames(i.skipPasses),
			Stderr:      os.Stderr,
			// #5135: foreground rebuilds run at the higher rebuild cap;
			// background scheduler reindexes stay throttled.
			Interactive: i.interactive,
		})
		if cerr != nil {
			trk.Fail(cerr.Error())
			return nil, fmt.Errorf("subprocess extract: %w", cerr)
		}
		// The coordinator merges Pass 1 / 2.5 / 3 entity records into a
		// single stream; surface that as pass1Records so buildDocument
		// sees the same shape it would in-process. Standalone Pass 2.5
		// relationships flow through pass2Rels.
		pass1Records = res.Entities
		pass2Rels = res.Relationships
		i.stats.processed = res.Processed
		i.stats.extracted = res.Extracted
		i.stats.skipped = res.Skipped
		i.stats.failed = res.Failed
		i.stats.pass1Rels = res.Pass1Rels
		i.stats.pass2Rels = res.Pass25Rels + len(res.Relationships)
		i.stats.pass3Rels = res.Pass3Rels
		for k, v := range res.ByLang {
			i.stats.pass1RelsByLang[k] += v
		}
		for k, v := range res.ByCrossExt {
			i.stats.pass3RelsByExt[k] += v
		}
		// A4 canary (#5414): fold the coordinator's merged per-language
		// ERROR-node stats into the run accumulator.
		i.stats.parseCanary.MergeStats(res.ParseErrors)
		// Issue #2447: propagate Pass1Plumbed counters from subprocess path.
		i.stats.pass1PlumbedTrue += res.Pass1PlumbedTrueCount
		i.stats.pass1PlumbedFalse += res.Pass1PlumbedFalseCount
		// #6338 — the coordinator does the classification on this path, so the
		// tally comes back with the rest of its stats.
		i.stats.unsupportedExt.Merge(res.UnsupportedExt)
		fmt.Fprintf(os.Stderr,
			"grafel: subproc-extract subprocs=%d peak_rss=%.1fMB entities=%d rels=%d\n",
			res.Subprocesses,
			float64(res.PeakRSSBytes)/(1024*1024),
			len(res.Entities), len(res.Relationships))
		for _, e := range res.NonFatalErrors {
			fmt.Fprintf(os.Stderr, "grafel: subproc-extract warning: %s\n", e)
		}
		// Emit a single done-with-extraction event covering all files.
		trk.Tick(progress.PhaseExtractAST, len(files), 0, "", len(res.Entities))
	} else {
		// Pass 1 — per-language AST extraction (instrumented with per-file ticks).
		pass1Records, classified, err = i.runPass1ExtractWithProgress(ctx, absRepo, files, trk)
		if err != nil {
			trk.Fail(err.Error())
			return nil, fmt.Errorf("pass 1: %w", err)
		}
		i.stats.pass1Rels = countEmbeddedRels(pass1Records)

		// Pass 2.5 — YAML-driven framework rules.
		trk.PhaseStart(progress.PhaseResolveRefs, len(files), len(pass1Records))
		passTimer.enter(progress.SubPhasePass25FrameworkRules)
		pass2Records, pass2Rels, err = i.runPass25FrameworkRules(ctx, absRepo, classified, pass1Records)
		if err != nil {
			trk.Fail(err.Error())
			return nil, fmt.Errorf("pass 2.5: %w", err)
		}
		i.stats.pass2Rels = len(pass2Rels) + countEmbeddedRels(pass2Records)

		// Pass 3 — cross-language extractors.
		passTimer.enter(progress.SubPhasePass3CrossLang)
		pass3Records, err = i.runPass3CrossLang(ctx, absRepo, classified)
		if err != nil {
			trk.Fail(err.Error())
			return nil, fmt.Errorf("pass 3: %w", err)
		}
		i.stats.pass3Rels = countEmbeddedRels(pass3Records)
	}

	// Pass 3.5 — first-class Config entity discovery (#1885).
	// Walks the pre-classification file list so we capture project-level
	// config files (Dockerfile, Makefile, pyproject.toml, package.json,
	// pom.xml, build.gradle, application.{properties,yml}, .env, …) that
	// the classifier would otherwise drop. The pass is idempotent and
	// always runs against `allFiles` so incremental indexing keeps the
	// Config entities even when no source file changed.
	//
	// NOTE (#5954): passes 3.5 onward run in BOTH the in-process and the
	// subprocess-extract branch, but only the in-process branch has entered
	// PhaseResolveRefs. Under GRAFEL_SUBPROC_EXTRACT=1 the active phase is
	// still extracting_ast, so Tracker.SubPhase DROPS these resolving_refs.*
	// stamps rather than misattributing samples across phases — the labels'
	// "<parent>.<pass>" contract is enforced there, not merely documented.
	// Those passes then read as plain extracting_ast, exactly as before #5954.
	// The alternative (entering PhaseResolveRefs on the subproc path too) would
	// change that path's published event stream, which this change must not do.
	passTimer.enter(progress.SubPhasePass35ConfigDiscovery)
	configEntities, configRels, configErr := configextract.Discover(ctx, absRepo, allFiles)
	if configErr != nil {
		fmt.Fprintf(os.Stderr, "grafel: config-discover warning: %v\n", configErr)
	}
	if len(configEntities) > 0 {
		pass3Records = append(pass3Records, configEntities...)
	}
	if len(configRels) > 0 {
		pass2Rels = append(pass2Rels, configRels...)
	}

	// Pass 3.6 — Bazel BUILD-graph fusion (#2183 / M6).
	// Parses BUILD/BUILD.bazel files as first-class dependency signals,
	// emitting BAZEL_DEPENDS_ON edges between declared Bazel targets.
	// Runs after config discovery so all graph entities are available for
	// the resolver overlay.
	passTimer.enter(progress.SubPhasePass36Bazel)
	bazelEntities, bazelRels, bazelErr := bazelextract.Discover(ctx, absRepo, allFiles)
	if bazelErr != nil {
		fmt.Fprintf(os.Stderr, "grafel: bazel-discover warning: %v\n", bazelErr)
	}
	if len(bazelEntities) > 0 {
		pass3Records = append(pass3Records, bazelEntities...)
	}
	if len(bazelRels) > 0 {
		pass2Rels = append(pass2Rels, bazelRels...)
	}

	// Pass 3.7 — Mage build-graph fusion (#3217).
	// Parses mage-tagged magefile.go / magefiles/*.go via go/parser, emitting
	// one SCOPE.Operation per exported target and MAGE_DEPENDS_ON edges for
	// mg.Deps / mg.SerialDeps / mg.CtxDeps prerequisites.
	passTimer.enter(progress.SubPhasePass37Mage)
	mageEntities, mageRels, mageErr := mageextract.Discover(ctx, absRepo, allFiles)
	if mageErr != nil {
		fmt.Fprintf(os.Stderr, "grafel: mage-discover warning: %v\n", mageErr)
	}
	if len(mageEntities) > 0 {
		pass3Records = append(pass3Records, mageEntities...)
	}
	if len(mageRels) > 0 {
		pass2Rels = append(pass2Rels, mageRels...)
	}

	// Pass 3.8 — Task (taskfile.dev) build-graph fusion (#3217).
	// Parses Taskfile.yml/.yaml, emitting one SCOPE.Operation per task and
	// TASK_DEPENDS_ON edges for deps: prerequisites and { task: <name> } cmds.
	passTimer.enter(progress.SubPhasePass38Task)
	taskEntities, taskRels, taskErr := taskextract.Discover(ctx, absRepo, allFiles)
	if taskErr != nil {
		fmt.Fprintf(os.Stderr, "grafel: task-discover warning: %v\n", taskErr)
	}
	if len(taskEntities) > 0 {
		pass3Records = append(pass3Records, taskEntities...)
	}
	if len(taskRels) > 0 {
		pass2Rels = append(pass2Rels, taskRels...)
	}

	// Pass 2.6 — Django nested URLconf composition.
	// Runs after Pass 3 so the classified slice is still populated with file
	// content. Emits fully-resolved http_endpoint entities for
	// path("prefix", include("module.path")) chains where the per-file
	// passes in Pass 2.5 can only see each file in isolation.
	// Results are appended to pass3Records for buildDocument to merge.
	if !i.skipPasses[PassFramework] {
		passTimer.enter(progress.SubPhasePass26DjangoRoutes)
		nestedEntities := runDjangoNestedURLConf(classified)

		// Pass 2.6b — DRF router.register expansion (#703, #705). Emits
		// detail-route ({pk}) and @action endpoints alongside the list
		// route that runDjangoNestedURLConf already produces.
		drfEntities := runDjangoDRFRoutes(classified)
		if len(drfEntities) > 0 {
			fmt.Fprintf(os.Stderr, "grafel: drf_router_expanded=%d entities\n", len(drfEntities))
		}

		// Issue #792: Deduplicate urlconf_nested_include ANY entries when
		// drf_router_expanded per-verb entries cover the same path.
		deduplicatedNestedEntities := engine.DeduplicateNestedURLConfDRF(nestedEntities, drfEntities)
		if len(nestedEntities) > len(deduplicatedNestedEntities) {
			fmt.Fprintf(os.Stderr, "grafel: deduped %d urlconf_nested_include entries (drf coverage)\n",
				len(nestedEntities)-len(deduplicatedNestedEntities))
		}

		// Issue #1126: Deduplicate http_endpoint_synthesis ANY entries emitted
		// by synthesizeDjangoFromComposed when drf_router_expanded per-verb
		// entries cover the same path. These ANY synthetics come from Pass 2.5
		// (applyHTTPEndpointSynthesis, per-file) and coexist with the
		// concrete-verb entries from Pass 2.6b because they have different IDs
		// (http:ANY:<path> vs http:GET:<path> etc.), inflating the ANY count.
		beforeDedup := len(pass2Records)
		pass2Records = engine.DeduplicateHTTPSynthesisANY(pass2Records, drfEntities)
		if deduped := beforeDedup - len(pass2Records); deduped > 0 {
			fmt.Fprintf(os.Stderr, "grafel: deduped %d http_endpoint_synthesis ANY entries (drf coverage)\n", deduped)
		}

		pass3Records = append(pass3Records, deduplicatedNestedEntities...)
		pass3Records = append(pass3Records, drfEntities...)

		// Pass 2.6c — Django CBV generic-method resolution (#786). Emits
		// per-verb http_endpoint synthetics + SCOPE.Operation synthetics for
		// inherited method handlers on CBVs (TemplateView, ListView, etc.)
		// so the Phase-2 resolver can emit IMPLEMENTS edges and resolve the
		// 179 remaining framework-synth orphans post-#783.
		cbvEntities := runDjangoCBVRoutes(classified)
		if len(cbvEntities) > 0 {
			fmt.Fprintf(os.Stderr, "grafel: django_cbv_routes=%d entities\n", len(cbvEntities))
		}
		pass3Records = append(pass3Records, cbvEntities...)

		// Pass 2.6d — Django admin route synthesis (#801). Emits
		// http_endpoint synthetics for every ModelAdmin registration found
		// in admin.py files (admin.site.register, @admin.register,
		// class FooAdmin(admin.ModelAdmin)). Covers changelist, add,
		// change, delete, history, autocomplete, custom actions, and
		// get_urls() overrides. Also emits site-level routes (login,
		// logout, password_change, jsi18n) once per project.
		adminEntities := runDjangoAdminRoutes(classified)
		if len(adminEntities) > 0 {
			fmt.Fprintf(os.Stderr, "grafel: django_admin_synthetic=%d entities\n", len(adminEntities))
		}
		pass3Records = append(pass3Records, adminEntities...)

		// Pass 2.6e-vs — DRF ViewSet.as_view({dict}) route synthesis (#2614).
		// Handles the explicit method-map mounting pattern that appears outside
		// router.register() — e.g. notification routes where
		// NotificationViewSet.as_view({'get': 'list', 'delete': 'delete_all'})
		// is pre-bound to a variable and then wired into urlpatterns. Covers
		// both the pre-bound-variable form and the inline as_view({}) form.
		vsAsViewEntities := runDjangoViewSetAsViewRoutes(classified)
		if len(vsAsViewEntities) > 0 {
			fmt.Fprintf(os.Stderr, "grafel: drf_viewset_asview_routes=%d entities\n", len(vsAsViewEntities))
		}
		pass3Records = append(pass3Records, vsAsViewEntities...)

		// Pass 2.6e — Celery cross-file dispatch edges (#1617). The per-file
		// scheduled-job pass only links `task.delay()` call sites that share a
		// file with the @shared_task definition. This repo-wide pass resolves
		// dispatch sites across files (tasks/ ← views/ ← signals/ ← services/)
		// and emits CALLS edges so find_callees / find_callers on a task and
		// the flows view show task dispatch. Append-only.
		passTimer.enter(progress.SubPhasePass26DjangoEdges)
		celeryDispatchRels := runCeleryDispatchEdges(classified)
		if len(celeryDispatchRels) > 0 {
			fmt.Fprintf(os.Stderr, "grafel: celery_dispatch_edges=%d\n", len(celeryDispatchRels))
		}
		pass2Rels = append(pass2Rels, celeryDispatchRels...)

		// Pass 2.6f — Django custom-signal pub/sub edges (#1617). Models each
		// `sig = Signal()` custom signal as a SCOPE.MessageTopic, with
		// SUBSCRIBES_TO from every @receiver(sig) handler and PUBLISHES_TO from
		// every sig.send()/send_robust() caller, so the signal dispatch surfaces
		// as a publisher → signal → handler diagram in /topology and /flows.
		signalEnts, signalRels := runDjangoSignalPubSub(classified)
		if len(signalEnts) > 0 || len(signalRels) > 0 {
			fmt.Fprintf(os.Stderr, "grafel: django_signal_pubsub=%d topics %d edges\n",
				len(signalEnts), len(signalRels))
		}
		pass3Records = append(pass3Records, signalEnts...)
		pass2Rels = append(pass2Rels, signalRels...)

		// Pass 2.6g — Serializer.Meta.model → Model REFERENCES edges (#2578).
		// DRF Serializer subclasses whose inner Meta class declares `model = X`
		// emit a REFERENCES edge so that graph queries for a Model X also surface
		// its serializers.
		serializerModelRels := runSerializerMetaModelEdges(classified)
		if len(serializerModelRels) > 0 {
			fmt.Fprintf(os.Stderr, "grafel: serializer_meta_model_edges=%d\n", len(serializerModelRels))
		}
		pass2Rels = append(pass2Rels, serializerModelRels...)

		// Pass 2.6h — @receiver(sender=Model) → Model HANDLES_SIGNAL edges (#2578).
		// Explicit-sender @receiver decorators link the handler function to the
		// Model class so queries for a Model also surface its signal handlers.
		receiverSenderRels := runReceiverSenderEdges(classified)
		if len(receiverSenderRels) > 0 {
			fmt.Fprintf(os.Stderr, "grafel: receiver_sender_edges=%d\n", len(receiverSenderRels))
		}
		pass2Rels = append(pass2Rels, receiverSenderRels...)

		// Pass 2.6i — FilterSet.Meta.model → Model REFERENCES edges (#2578).
		// django_filter FilterSet subclasses with Meta.model = X emit a REFERENCES
		// edge so that queries for a Model also return its filter classes.
		filterSetModelRels := runFilterSetMetaModelEdges(classified)
		if len(filterSetModelRels) > 0 {
			fmt.Fprintf(os.Stderr, "grafel: filterset_meta_model_edges=%d\n", len(filterSetModelRels))
		}
		pass2Rels = append(pass2Rels, filterSetModelRels...)
	}

	// Pass 2.6 — Java JAX-RS / Spring MVC annotation route composition.
	// Runs after Pass 3 (while the classified slice is still live with file
	// content) and emits fully-resolved http_endpoint synthetic entities for
	// every annotated handler method. Fixes #682 (source_handler kind/name
	// mismatch: now emits "SCOPE.Operation:ClassName.methodName") and #683
	// (annotation budget: line-buffer approach handles any number of
	// intervening annotations between @VERB and @Path). Must run before the
	// classified slice is released below. Refs #682, #683.
	if !i.skipPasses[PassFramework] {
		passTimer.enter(progress.SubPhasePass26JavaRoutes)
		javaEntities := runJavaAnnotationRoutes(classified)
		pass3Records = append(pass3Records, javaEntities...)
	}

	// Pass 2.7 — corpus-wide response-shape extraction (#753).
	//
	// The per-file response-shape pass inside applyHTTPEndpointSynthesis
	// only works when the handler lives in the same file as the route
	// registration. Real applications dispatch URLs from a different
	// module (Django urls.py → views.py, DRF router → ViewSets,
	// Express routes → imported controllers). This pass runs after
	// every producer-side synthesizer has populated handler references
	// on http_endpoint entities and resolves them cross-file using the
	// classified file set still live in memory at this point.
	//
	// MUST run before releaseClassifiedASTs (which nils content) and
	// before buildDocument (where ResolveHTTPEndpointHandlers clears
	// the source_handler property post-resolution).
	if !i.skipPasses[PassFramework] && len(classified) > 0 {
		passTimer.enter(progress.SubPhasePass27ResponseShapes)
		// Build a file-content lookup from the classified slice.
		contentByPath := make(map[string][]byte, len(classified))
		allPaths := make([]string, 0, len(classified))
		for k := range classified {
			cf := &classified[k]
			allPaths = append(allPaths, cf.relPath)
			if cf.content != nil {
				contentByPath[cf.relPath] = cf.content
			}
		}
		reader := func(p string) []byte { return contentByPath[p] }

		// Gather the union of records we have so far. Mutating
		// pass3Records' Properties also mutates the shared map under
		// the http_endpoint entities buildDocument will receive.
		shapeStats := engine.ApplyResponseShapesCorpus(
			concatRecords(pass1Records, pass2Records, pass3Records),
			pass2Rels,
			reader,
		)
		if shapeStats.Endpoints > 0 {
			fmt.Fprintf(os.Stderr,
				"response-shape-corpus: endpoints=%d handler_resolved=%d shape_extracted=%d no_handler_found=%d\n",
				shapeStats.Endpoints,
				shapeStats.HandlerResolved,
				shapeStats.ShapeExtracted,
				shapeStats.NoHandlerFound)
		}

		// Pass 2.7b — corpus-wide Sails policy → endpoint auth attribution
		// (#2897). Sails gates actions via config/policies.js policy maps, but
		// the endpoints are synthesised from config/routes.js — a different
		// file — so the per-file auth pass (#2852) cannot join them. This pass
		// parses every config/policies.{js,ts}, resolves each Sails endpoint's
		// controller/action policy chain (action > controller > global '*'),
		// and stamps the config-method, medium-confidence auth_policy contract.
		sailsAuthStats := engine.ApplySailsAuthPolicy(
			concatRecords(pass1Records, pass2Records, pass3Records),
			allPaths,
			reader,
		)
		if sailsAuthStats.PolicyFiles > 0 {
			fmt.Fprintf(os.Stderr,
				"sails-auth-corpus: policy_files=%d endpoints=%d attributed=%d\n",
				sailsAuthStats.PolicyFiles,
				sailsAuthStats.Endpoints,
				sailsAuthStats.Attributed)
		}
	}

	// Pass 2.8 — multi-hop TESTS edges via HTTP client calls (#2549/#2556).
	// Synthesises TESTS edges from test functions to ViewSet/handler entities
	// by following HTTP client call sites through the ROUTES_TO graph.
	// Must run before releaseClassifiedASTs since we need file content access.
	if !i.skipPasses[PassFramework] && len(classified) > 0 {
		passTimer.enter(progress.SubPhasePass28TestsMultiHop)
		// Gather all paths and build the file-content lookup.
		var allPaths []string
		contentByPath := make(map[string][]byte, len(classified))
		for k := range classified {
			cf := &classified[k]
			allPaths = append(allPaths, cf.relPath)
			if cf.content != nil {
				contentByPath[cf.relPath] = cf.content
			}
		}
		reader := func(p string) []byte { return contentByPath[p] }

		// #2570 — supplement pass2Rels with synthetic ROUTES_TO records
		// derived from http_endpoint entities in pass3Records.  When the app
		// separates router.register() and include(router.urls) into different
		// files (a common pattern), applyDjangoRouteComposition never fires in
		// same-file mode and no composed ROUTES_TO land in pass2Rels.  The
		// entities emitted by ApplyDjangoDRFRoutes carry path+source_handler
		// properties that let us reconstruct equivalent ROUTES_TO records here.
		endpointRoutesTo := engine.SynthesiseRoutesToFromEndpoints(
			concatRecords(pass2Records, pass3Records),
		)
		routesForTests := pass2Rels
		if len(endpointRoutesTo) > 0 {
			routesForTests = make([]types.RelationshipRecord, len(pass2Rels)+len(endpointRoutesTo))
			copy(routesForTests, pass2Rels)
			copy(routesForTests[len(pass2Rels):], endpointRoutesTo)
		}

		testsEdges := engine.ApplyTestsMultiHopViaHTTP(allPaths, reader, routesForTests)
		if len(testsEdges) > 0 {
			fmt.Fprintf(os.Stderr, "grafel: tests_multi_hop_http=%d edges\n", len(testsEdges))
			pass2Rels = append(pass2Rels, testsEdges...)
		}

		// #2812 — TESTS edges from test functions to production entities
		// imported into the test module (Django/pytest). Resolves the broad
		// set of helpers + models a test exercises that the per-file testmap
		// extractor's globally-unique-name resolution misses (e.g. ambiguous
		// model names like `Building`/`Device`). Edges originate from the test
		// function entity itself so coverage counts the test as linked.
		importTestsEdges := engine.ApplyTestsViaImports(
			allPaths, reader, concatRecords(pass1Records, pass2Records, pass3Records),
		)
		if len(importTestsEdges) > 0 {
			fmt.Fprintf(os.Stderr, "grafel: tests_via_import=%d edges\n", len(importTestsEdges))
			pass2Rels = append(pass2Rels, importTestsEdges...)
		}
	}

	// Issue #633 — release per-file AST trees + source bytes now that the
	// last consumer (Pass 3 cross-language extractors) has finished. The
	// classified slice is otherwise retained until Run() returns, which on
	// TS-heavy fixtures pinned hundreds of MB of tree-sitter AST nodes
	// across the entire downstream pipeline (resolver, build-document,
	// external-synthesis, Pass 4). tree-sitter trees are CGo-allocated so
	// runtime.GC alone can't reclaim them — Close() is required.
	passTimer.enter(progress.SubPhaseReleaseASTs)
	releaseClassifiedASTs(classified)
	classified = nil
	runtime.GC()
	passTimer.finish()

	// Pass 5 — build document (deduped).
	trk.PhaseStart(progress.PhaseMaterialize, len(files), 0)
	doc := i.buildDocument(&pass1Records, &pass2Records, pass2Rels, &pass3Records)

	// #2719 — incremental merge: when this run processed only the changed
	// subset of files, splice the unchanged-file portion of the previous
	// graph back into doc. Without this the doc that flows into Pass 4
	// (graph algorithms), external synthesis, embeddings, rename detection,
	// and the on-disk write would contain only the changed-file entities —
	// a tiny fraction of the real graph — silently corrupting the persisted
	// state for every CLI `grafel rebuild <group> --incremental` invocation.
	//
	// Entities sourced from changed files are produced fresh in `doc`; we add
	// every previous entity whose SourceFile is NOT in the changed-set. We
	// also carry forward every previous relationship whose endpoints both
	// survive (either re-emitted in this run, or carried forward from the
	// previous unchanged-file portion). Edges that touched a changed-file
	// entity are dropped here — the resolver passes already attached above
	// produced replacements for them against the fresh entity IDs.
	if incrementalPrev != nil && incrementalChange != nil {
		mergeStats := mergeIncrementalPrevSource(doc, incrementalPrev, incrementalChange)
		fmt.Fprintf(os.Stderr,
			"grafel: incremental — carried forward %d entities and %d relationships from previous graph (dropped %d stale edges)\n",
			mergeStats.entitiesAdded, mergeStats.relsAdded, mergeStats.relsDropped)

		// #6160 — strip the PREVIOUS run's flow artefacts out of the merged
		// graph, so the flow walkers below re-derive them exactly once.
		//
		// The merge carries forward every prev entity whose SourceFile was not
		// re-extracted. A SCOPE.Process / SCOPE.EventFlow anchored in an
		// unchanged file therefore survives — and then Pass 7 / 7.5 run over
		// the merged graph and re-derive it, producing a SECOND row under the
		// same id (the walkers' ids are content hashes over the chain, so an
		// unchanged chain re-derives the identical id). Measured on the #6129
		// corpus: SCOPE.Process "http:GET:/cpview → cp_view_handler" twice,
		// and its ENTRY_POINT_OF / STEP_IN_PROCESS edges with it.
		//
		// Stripping here rather than immediately before Pass 7 is deliberate:
		// a full rebuild holds NO flow entity at any point between
		// buildDocument and Pass 7, so this reproduces the full path's state
		// for every pass in between (graph algorithms, external synthesis) and
		// not just for the walkers.
		//
		// It is a DELETION, so it is scoped to the walkers that are actually
		// about to run: with --skip-pass=process-flow / event-flow the
		// corresponding walker never re-emits, and stripping its artefacts
		// would destroy carried-forward rows for good. StripFlows removes
		// exactly the selected walkers' output kinds (#5309 layer 3 encodes
		// that set; Path A has used it via RunFlowsIncremental since).
		runProcessFlow := !i.skipPasses[PassProcessFlow]
		runEventFlow := !i.skipPasses[PassEventFlow]
		if stripEnts, stripRels := engine.StripFlows(doc, runProcessFlow, runEventFlow); stripEnts > 0 || stripRels > 0 {
			doc.Stats.Entities = len(doc.Entities)
			doc.Stats.Relationships = len(doc.Relationships)
			fmt.Fprintf(os.Stderr,
				"grafel: incremental — stripped %d stale flow entities and %d flow edges carried forward from the previous graph (the flow passes re-derive them)\n",
				stripEnts, stripRels)
		}
	}

	// #2706 — belt-and-suspenders prune of Django migration entities.
	// Per-extractor prunes (#2551, #2602, #2616) cover the AST-walk and
	// cross-language hierarchy paths, but every per-language extractor calls
	// extractor.FileEntity at the top of Extract() which unconditionally
	// emits SCOPE.Component(subtype="file") — and Wave 3-5 added new
	// emission paths (file_conventions, framework synthesisers) that don't
	// know about the migration gate. This single sweep at the indexer level
	// drops every container/scope-shaped entity anchored to a migrations/
	// file regardless of which extractor emitted it. Opt-in via
	// GRAFEL_EMIT_MIGRATION_ENTITIES=1|true bypasses the prune.
	//
	// Runs AFTER mergeIncrementalPrevDoc so that entities carried forward
	// from a previous graph (which may predate the per-extractor prunes or
	// have been emitted by a Wave 3-5 path) are also caught.
	if ePruned, rPruned := extractors.PruneMigrationEntities(doc); ePruned > 0 && verbose() {
		fmt.Fprintf(os.Stderr, "migration-prune: dropped %d entities + %d relationships anchored to Django migration files\n", ePruned, rPruned)
	}

	// #4306 (Layer 1 of epic #4294): deterministic markdown documentation
	// ingestion. OPT-IN (--ingest-docs / GRAFEL_INGEST_DOCS), default OFF.
	// Runs AFTER buildDocument + incremental merge so the code-entity name
	// index used for exact-mention linking reflects the full, ID-stamped graph,
	// and the emitted doc/section nodes participate in Pass 4 graph algorithms
	// and the on-disk write. Fully deterministic: no LLM calls, no network.
	// When the flag is off this block is skipped entirely (zero overhead).
	if i.ingestDocs {
		docFiles := ingest.DiscoverDocs(allFiles)
		ingRes := ingest.Ingest(absRepo, i.repoTag, docFiles, doc.Entities)
		doc.Entities = append(doc.Entities, ingRes.Entities...)
		doc.Relationships = append(doc.Relationships, ingRes.Relationships...)
		if verbose() {
			fmt.Fprintf(os.Stderr,
				"ingest-docs: %d doc files (md+pdf) → %d documents, %d sections, %d mentions, %d skipped (deterministic, no LLM)\n",
				ingRes.FilesRead, ingRes.Documents, ingRes.Sections, ingRes.Mentions, ingRes.Skipped)
		}

		// #4308 (Layer 2 of epic #4294): emit per-Section semantic-extraction
		// prompt bundles for the agent-driven path. STILL DETERMINISTIC — no LLM
		// call, no network: this writes self-contained bundle artifacts an
		// EXTERNAL agent picks up (runs its own LLM) and feeds back via
		// grafel_apply_doc_semantics. Persisted under the per-repo state dir,
		// mirroring the docgen run-dir/artifact convention. Best-effort: a write
		// failure never fails the index.
		bundles := ingest.EmitSemanticBundles(absRepo, i.repoTag, docFiles, doc.Entities)
		if len(bundles) > 0 {
			runDir := filepath.Join(daemon.StateDirForRepo(absRepo), "doc-semantics")
			written := 0
			for _, b := range bundles {
				if _, err := ingest.WriteBundle(runDir, b); err == nil {
					written++
				}
			}
			if verbose() {
				fmt.Fprintf(os.Stderr,
					"ingest-docs-l2: emitted %d semantic prompt bundles → %s (agent-driven, no LLM)\n",
					written, runDir)
			}
		}
	}
	// Drop the per-pass record slices now that buildDocument has produced
	// the merged + deduped graph.Entity / graph.Relationship slices. These
	// pass-level slices hold a copy of every entity's Properties /
	// Metadata maps and embedded Relationship slices; releasing them
	// before the resolver-classification + Pass 4 algorithms cuts the
	// peak by roughly the merged-set size on entity-dense repos.
	//
	// #5985 — pass1Records / pass2Records / pass3Records are ALREADY nil here:
	// buildDocument takes them by pointer and releases each one the instant it
	// has copied it into `merged` (mergePassRecords), so the duplicate spine is
	// collectable during buildDocument instead of only after it returns. The
	// assignments below are kept for pass2Rels (still passed by value) and as a
	// belt-and-braces statement of the "dead after buildDocument" contract —
	// nothing between the buildDocument call above and this point may read them.
	pass1Records = nil
	pass2Records = nil
	pass2Rels = nil
	pass3Records = nil
	runtime.GC()

	// Pass 4.5 — external entity synthesis. Runs BEFORE Pass 4 so the
	// synthesised "ext:<name>" placeholders participate in the graph
	// algorithms (community detection, centrality, articulation points).
	// PORT-EXT (issue #32). Idempotent + counter-instrumented.
	extStats := external.Synthesize(doc)
	if verbose() {
		fmt.Fprintf(os.Stderr,
			"ext-synthesis: synthesized=%d relationships_resolved=%d unique_externals=%d\n",
			extStats.Synthesized, extStats.RelationshipsResolved, extStats.UniqueExternals)
	}
	i.stats.extSynth = extStats

	// #5687 — data-layer lineage edges. Connect each SCOPE.DataAccess node to
	// the EXISTING physical table entity it touches (SCOPE.Datastore/Schema),
	// emitting READS_FROM (SELECT) / WRITES_TO (INSERT/UPDATE/DELETE/…) on an
	// EXACT canonical table-name match. De-orphans the data-access/table layer so
	// lineage queries can traverse function → DataAccess → table. Runs at Pass 4.5
	// (before the Pass 4 graph algorithms) so the new edges participate in
	// centrality/community. Append-only, deterministic, idempotent.
	dlStats := external.SynthesizeDataLineageEdges(doc)
	if verbose() || dlStats.EdgesAdded > 0 {
		fmt.Fprintf(os.Stderr,
			"data-lineage: data_access_seen=%d edges=%d reads_from=%d writes_to=%d ambiguous=%d unresolved=%d\n",
			dlStats.DataAccessSeen, dlStats.EdgesAdded, dlStats.ReadsFrom,
			dlStats.WritesTo, dlStats.Ambiguous, dlStats.Unresolved)
	}

	// #4480 — retarget THROWS / CATCHES edges from the synthetic
	// SCOPE.ExceptionType convergence node to the REAL exception class entity
	// (declared in-repo class, or the imported `ext:<Type>` placeholder just
	// synthesised above), and drop the now-redundant synthetic node. Runs
	// AFTER external.Synthesize so imported exception classes are present.
	// Keeps exactly one node per exception with the throws/catches edge on it;
	// the synthetic node survives only for genuinely unresolvable types.
	excStats := external.ResolveExceptionTypes(doc)
	if verbose() {
		fmt.Fprintf(os.Stderr,
			"exception-resolve: retargeted=%d synthetic_dropped=%d synthetic_kept=%d\n",
			excStats.Retargeted, excStats.SyntheticDropped, excStats.SyntheticKept)
	}

	// #5526 — package.json declared-dependency cross-reference. Runs AFTER
	// external.Synthesize so the import-derived ext:<pkg> nodes exist: joins
	// declared deps (package.json), resolved versions (lockfile), and the
	// import graph to stamp used/unused (dead_dependency_candidate) + resolved
	// versions on each declared npm dep and surface versions on ext: nodes.
	xrefStats := external.CrossReferenceManifests(doc)
	if verbose() {
		fmt.Fprintf(os.Stderr,
			"manifest-xref: declared_reconciled=%d dead_candidates=%d versions_resolved=%d ext_enriched=%d\n",
			xrefStats.DeclaredReconciled, xrefStats.DeadCandidates,
			xrefStats.VersionsResolved, xrefStats.ExternalNodesEnriched)
	}

	// Issue #1381 — stamp module="_external" on synthesised placeholder
	// entities that have no source_file (ext:* nodes from external.Synthesize,
	// and any other synthetic entities that buildDocument could not tag because
	// they were appended after the assembly loop).  EnsureModule skips entities
	// that already carry a "module" key, so well-sourced entities are unaffected.
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.SourceFile == "" {
			if e.PropLen() == 0 {
				e.PropsReplace(map[string]string{"module": "_external"})
			} else if _, ok := e.PropLookup("module"); !ok {
				e.PropSet("module", "_external")
			}
		}
	}

	// ADR-0015 phase-1 (#545) — repair.json apply path. Runs BEFORE the
	// final reclassification so the bug-rate measurement that follows
	// already reflects agent-supplied repairs. The disposition classifier
	// core is unchanged — repairs land via the override hook here,
	// mutating ToID + edge properties; classification then sees the
	// rewritten edges as ordinary resolved/external/dynamic endpoints.
	//
	// Default-off (--enable-repair-apply false) so existing bug-rate
	// measurements across the 10-corpus regression set stay unchanged.
	if i.enableRepairApply {
		grafelDir := daemon.StateDirForRepo(absRepo)
		repairs, rerr := enrichment.ReadRepairs(grafelDir)
		if rerr != nil {
			fmt.Fprintf(os.Stderr,
				"grafel: repair.json read error (continuing without): %v\n", rerr)
		}
		repairStats := enrichment.ApplyRepairs(doc, repairs,
			enrichment.ApplyRepairsOptions{RepoRoot: absRepo})
		if werr := enrichment.WriteRepairStats(grafelDir, repairStats); werr != nil {
			fmt.Fprintf(os.Stderr,
				"grafel: repair_stats.json write failed: %v\n", werr)
		}
		if verbose() {
			fmt.Fprintf(os.Stderr,
				"repair-apply: applied=%d rejected=%d stale=%d (ADR-0015 phase-1)\n",
				repairStats.AppliedCount, repairStats.RejectedCount, repairStats.StaleCount)
		}
	}

	// VERIFY-2-PREP / issue #56 — reclassify dispositions over the FINAL
	// edge state (post-external-synthesis) so "ext:<pkg>" endpoints land in
	// ExternalKnown / ExternalUnknown rather than the bug buckets they were
	// initially placed into when they were still bare names. Logged under
	// GRAFEL_VERBOSE=1.
	if i.resolveIdx != nil {
		allow := resolve.ExternalAllowlist(external.IsKnownExternalPackage)
		eps := make([]resolve.EndpointPair, 0, len(doc.Relationships))
		for k := range doc.Relationships {
			r := &doc.Relationships[k]
			// Issue #90 — pass through the relationship's language tag so
			// the final-classification pass routes to the right per-
			// language dynamic-pattern catalog instead of falling through
			// to cross-language only.
			lang := ""
			if r.PropLen() > 0 {
				if v, ok := r.PropLookup("language"); ok {
					lang = v
				} else if v, ok := r.PropLookup("lang"); ok {
					lang = v
				}
			}
			eps = append(eps, resolve.EndpointPair{
				FromID:       r.FromID,
				FromOriginal: r.FromID,
				ToID:         r.ToID,
				ToOriginal:   r.ToID,
				Language:     lang,
			})
		}
		final := i.resolveIdx.ClassifyEndpoints(eps, allow)
		i.finalDispositions = final
		if verbose() {
			emitDispositionBreakdown(os.Stderr, final)
		}
		// Issue #89 — temporary diagnostic instrumentation. Enabled with
		// GRAFEL_BUG_EXTRACTOR_SAMPLES=N (writes N samples). Optional
		// GRAFEL_BUG_EXTRACTOR_OUT=/path/to/file (defaults to stderr).
		if n := bugExtractorSampleCount(); n > 0 {
			out := os.Stderr
			if p := strings.TrimSpace(os.Getenv("GRAFEL_BUG_EXTRACTOR_OUT")); p != "" {
				if f, ferr := os.Create(p); ferr == nil {
					defer f.Close()
					out = f
				} else {
					fmt.Fprintf(os.Stderr, "bug-extractor-samples: cannot open %q: %v\n", p, ferr)
				}
			}
			dumpBugExtractorSamples(out, doc, *i.resolveIdx, allow, n)
		}
		// Issue #92 — temporary bug-resolver diagnostic instrumentation.
		// Enabled with GRAFEL_BUG_RESOLVER_SAMPLES=N. Optional
		// GRAFEL_BUG_RESOLVER_OUT=/path overrides stderr.
		if n := bugResolverSampleCount(); n > 0 {
			out := os.Stderr
			if p := strings.TrimSpace(os.Getenv("GRAFEL_BUG_RESOLVER_OUT")); p != "" {
				if f, ferr := os.Create(p); ferr == nil {
					defer f.Close()
					out = f
				} else {
					fmt.Fprintf(os.Stderr, "bug-resolver-samples: cannot open %q: %v\n", p, ferr)
				}
			}
			dumpBugResolverSamples(out, doc, *i.resolveIdx, allow, n)
		}
		// Issue #633 — release the resolver's lookup tables now that the
		// final classification + optional sample dumps have consumed them.
		// The Index struct holds 10+ string-keyed nested maps sized for the
		// full merged entity set (byKind, byName, nameKinds, nameKindsReal,
		// byLocation, byLocationKind, byLocationKindReal, byMember,
		// byPackageMember, byPackageOperation, byPackageComponent, …) —
		// none of them are needed past this point. Pass 6 enrichment uses
		// resolveIdx only via the optional ADR-0015 repair-edge path; that
		// path is gated behind --enable-repair-candidates and falls back
		// gracefully when resolveIdx is nil.
		if !i.enableRepairCandidates {
			i.resolveIdx = nil
			runtime.GC()
		}
	}

	// Pass 3.5 — TESTS edge walk-up (tests-edge-walk-up from helpers).
	// Runs AFTER the resolver has replaced all stub endpoints with real entity
	// IDs AND after the final reclassification pass, so the CALLS edges are
	// fully resolved when we query the inbound-CALLS index.
	// Derives additional TESTS edges: test_fn → viewset_method when the test
	// directly calls a helper (test_fn → helper) and the helper is called by
	// a viewset / handler (viewset → helper via CALLS). Derived edges are
	// written to doc.Relationships with confidence=0.7 and
	// source=tests-walkup so downstream consumers can distinguish them from
	// explicit testmap edges. Skippable via --skip-pass=tests-walkup.
	if !i.skipPasses[PassTestsWalkUp] {
		walkUpStats := graph.DeriveTestsWalkUp(doc)
		if verbose() || walkUpStats.DerivedEdges > 0 {
			fmt.Fprintf(os.Stderr,
				"grafel: tests-walkup helpers=%d derived=%d high_fan_in_skipped=%d duplicates_suppressed=%d\n",
				walkUpStats.HelperTargets, walkUpStats.DerivedEdges,
				walkUpStats.SkippedHighFanIn, walkUpStats.DuplicatesSuppressed)
		}
		// Re-sync Stats so subsequent passes see the correct edge count.
		doc.Stats.Relationships = len(doc.Relationships)
	}

	// Pass 4 — graph algorithms. REMOVED at per-repo scope (#5349 A3, epic
	// #5350): communities + centrality are now computed ONCE over the assembled
	// group union by the daemon's debounced group-algo scheduler, which writes
	// the <group>-algo.json overlay. Computing them per-repo here was the wrong
	// scope — the pass never saw cross-repo edges — and the values are now
	// authoritatively supplied by the group overlay at MCP load time. The
	// graph.fb per-entity algo FIELDS (community_id/centrality/pagerank/...)
	// are retained (vestigial, one release) but left at their schema sentinels
	// (-2 / 0 / false) rather than recomputed per-repo. A single-repo group is
	// the degenerate one-repo union and still gets algorithms via the group
	// pass. The PassGraphAlgo skip flag is kept for back-compat but the eager
	// per-repo computation no longer runs even when it is unset.
	//
	// runPass4Algorithms / runPass4AlgorithmsWithProgress are retained (unused
	// methods are legal in Go) for one release in case an operator needs to
	// restore the per-repo pass via a future flag.
	//
	// The canonical entity/relationship sort that the old pass performed (issue
	// #481) is PRESERVED here: downstream passes (process-flow BFS, module
	// aggregation, the group-algo assembly) rely on entities/relationships being
	// in canonical-id order. Only the gonum algorithm computation is removed.
	if !i.skipPasses[PassGraphAlgo] {
		sort.SliceStable(doc.Entities, func(a, b int) bool { return doc.Entities[a].ID < doc.Entities[b].ID })
		sort.SliceStable(doc.Relationships, func(a, b int) bool {
			ra, rb := &doc.Relationships[a], &doc.Relationships[b]
			if ra.FromID != rb.FromID {
				return ra.FromID < rb.FromID
			}
			if ra.ToID != rb.ToID {
				return ra.ToID < rb.ToID
			}
			return ra.Kind < rb.Kind
		})
	}

	// #5334 — surface the flow walkers (process-flow + event-flow) as a
	// granular "Computing flows…" phase so the CLI/wizard tail no longer
	// collapses under one "Materializing". One cheap event per the combined
	// flow stage; the coarse PhaseMaterialize (emitted at buildDocument) is
	// retained as the fallback band.
	if !i.skipPasses[PassProcessFlow] || !i.skipPasses[PassEventFlow] {
		trk.Phase(progress.PhaseComputeFlows, "process+event flows", doc.Stats.Entities)
	}

	// Pass 7 — process-flow BFS (#724). Runs AFTER all CALLS edges are
	// finalised (resolver + external synthesis + Pass 4) so the trace
	// algorithm sees the same graph as downstream consumers. Emits
	// SCOPE.Process entities + STEP_IN_PROCESS / ENTRY_POINT_OF edges.
	// Skippable via --skip-pass=process-flow (default on).
	if !i.skipPasses[PassProcessFlow] {
		pfStats := engine.RunProcessFlow(doc, engine.DefaultProcessFlowConfig())
		if verbose() || pfStats.Processes > 0 {
			fmt.Fprintf(os.Stderr,
				"grafel: process-flow entry_candidates=%d entries_used=%d "+
					"processes=%d cross_stack=%d step_edges=%d entry_edges=%d\n",
				pfStats.EntryCandidates, pfStats.EntriesUsed,
				pfStats.Processes, pfStats.CrossStack,
				pfStats.StepEdges, pfStats.EntryEdges)
		}
		// Re-sync Stats so the downstream sidecar + emission see the new
		// entity/edge counts. The final sort below in Index() will fold the
		// process entities into the canonical id ordering.
		doc.Stats.Entities = len(doc.Entities)
		doc.Stats.Relationships = len(doc.Relationships)
	}

	// Pass 7.5 — event-flow pub/sub walker (#1944 Phase 1). Runs after
	// process-flow so EventFlow entities live alongside Process entities
	// in the same final graph. Linear, single-channel-seed walker only:
	// branching, cross-stack channel bridges, and conditional routing
	// are tracked under #1944 Phase 2-5.
	if !i.skipPasses[PassEventFlow] {
		efStats := engine.RunEventFlow(doc, engine.DefaultEventFlowConfig())
		if verbose() || efStats.EventFlows > 0 {
			fmt.Fprintf(os.Stderr,
				"grafel: event-flow seed_channels=%d event_flows=%d "+
					"step_edges=%d seed_edges=%d\n",
				efStats.SeedChannels, efStats.EventFlows,
				efStats.StepEdges, efStats.SeedEdges)
		}
		doc.Stats.Entities = len(doc.Entities)
		doc.Stats.Relationships = len(doc.Relationships)

		// Pass 7.6 — async-trigger edge synthesis (#5686). Runs after the
		// event-flow walk so DELIVERS_TO trigger edges (topic → handler) live
		// alongside the pub/sub edges. Gives async/event-driven handlers an
		// inbound trigger so find_callers / impact_radius / trace no longer
		// dead-end at the async boundary. Append-only, idempotent, CALLS-clean.
		atStats := engine.ApplyAsyncTriggerEdges(doc)
		if verbose() || atStats.DeliversEdges > 0 {
			fmt.Fprintf(os.Stderr,
				"grafel: async-trigger topics=%d delivers_edges=%d\n",
				atStats.Topics, atStats.DeliversEdges)
		}
		doc.Stats.Entities = len(doc.Entities)
		doc.Stats.Relationships = len(doc.Relationships)
	}

	// Pass 8 — module-level aggregation (issue #1383 / EPIC #1380).
	// Runs AFTER all entity/relationship passes are finalised (Pass 7 process-
	// flow is the last mutating pass) so the Module→entity CONTAINS edges and
	// Module→Module DEPENDS_ON edges reflect the final graph topology. Module
	// nodes are stored IN the main document alongside real entities; callers
	// that want only the entity-level graph filter by kind != "Module". The
	// pass is skippable via --skip-pass=module-agg.
	if !i.skipPasses[PassModuleAgg] {
		aggStats := module.Aggregate(doc)
		if verbose() || aggStats.ModuleNodes > 0 {
			fmt.Fprintf(os.Stderr,
				"grafel: module-agg modules=%d contains=%d depends_on=%d\n",
				aggStats.ModuleNodes, aggStats.ContainsEdges, aggStats.DependsOnEdges)
		}
	}

	// Pass 8.5 — commit-coupling soft edges (issue #21). Runs AFTER the main
	// extraction + algorithm + module-agg passes. Mines `git log --name-only`
	// to derive a co-change support count between file pairs and emits
	// COMMIT_COUPLED edges between synthetic File entities for pairs that
	// meet the minimum support threshold (default 5 commits). Pass 4 graph
	// algorithms already ran above on the pre-coupling graph, so this layer
	// does NOT influence community detection / centrality — it is a true
	// soft-edge layer that opt-in consumers can read on demand. Skippable
	// via --skip-pass=commit-couple.
	if !i.skipPasses[PassCommitCouple] {
		ccStats := engine.ApplyCommitCoupling(doc, absRepo, engine.DefaultCommitCouplingConfig())
		if ccStats.Skipped {
			if verbose() {
				fmt.Fprintf(os.Stderr, "grafel: commit-couple skipped (%s)\n", ccStats.SkipReason)
			}
		} else if verbose() || ccStats.CoupledEdges > 0 {
			fmt.Fprintf(os.Stderr,
				"grafel: commit-couple commits=%d candidate_pairs=%d files=%d edges=%d oversize_skipped=%d\n",
				ccStats.TotalCommits, ccStats.CandidatePairs, ccStats.FileEntities,
				ccStats.CoupledEdges, ccStats.SkippedOversizeCommits)
		}
	}

	// Pass 8.6 — structural coupling metrics (issue #3634, epic #3625).
	// Restores the previously-orphaned coupling_score enricher as a live
	// pass. Runs AFTER module-agg (Pass 8) so the Module→Module DEPENDS_ON
	// edges it consumes are present. Annotates each Module entity with
	// afferent coupling (ca), efferent coupling (ce), and instability
	// (I = Ce/(Ca+Ce)) — an architecture-quality signal for rewrite-boundary
	// planning. Distinct from commit-couple (Pass 8.5), which is the
	// temporal/VCS co-change axis. Skippable via --skip-pass=coupling.
	if !i.skipPasses[PassCoupling] {
		cpStats := engine.ApplyStructuralCoupling(doc)
		if cpStats.Skipped {
			if verbose() {
				fmt.Fprintf(os.Stderr, "grafel: coupling skipped (no module graph)\n")
			}
		} else if verbose() || cpStats.ModulesAnnotated > 0 {
			fmt.Fprintf(os.Stderr,
				"grafel: coupling modules=%d depends_on_edges=%d\n",
				cpStats.ModulesAnnotated, cpStats.DependsOnEdges)
		}
	}

	// Pass 8.7 — dependency-hygiene annotation (#3640, epic #3625). Runs
	// AFTER the document is assembled (manifest external_dependency entities +
	// import DEPENDS_ON edges are all present) so the deplinker analysis sees
	// the same graph the dashboard would. Persists usage_status=used|unused
	// onto each declared dependency entity (and its DEPENDS_ON edge), promoting
	// the previously dashboard-only deplinker signal into the graph so
	// find/neighbors/agents can read dependency hygiene. The dashboard keeps
	// calling AnalyzeGroup independently; this pass annotates entities in place
	// (no new entities, no double-emit). Skippable via --skip-pass=dep-hygiene.
	if !i.skipPasses[PassDepHygiene] {
		dhStats := engine.ApplyDependencyHygiene(doc)
		if verbose() || dhStats.EntitiesAnnotated > 0 {
			fmt.Fprintf(os.Stderr,
				"grafel: dep-hygiene declared=%d used=%d unused=%d phantom=%d "+
					"entities_annotated=%d edges_annotated=%d\n",
				dhStats.Declared, dhStats.Used, dhStats.Unused, dhStats.Phantom,
				dhStats.EntitiesAnnotated, dhStats.EdgesAnnotated)
		}
	}

	// Pass 8.8 — shared-database cross-service coupling (#3628 area #13). Runs
	// AFTER module-agg (Pass 8) so synthetic Module nodes exist and every
	// entity carries Properties["module"]. Detects when ≥2 DISTINCT modules
	// access the SAME table/collection (via ACCESSES_TABLE / JOINS_COLLECTION /
	// SCOPE.DataAccess attribution): annotates the shared SCOPE.DataAccess
	// entities with shared/accessor_count/accessor_modules and emits a
	// SHARES_DATA edge between each co-accessing Module pair — the cross-service
	// DATA coupling axis, orthogonal to structural (Pass 8.6) and temporal
	// (Pass 8.5) coupling. Honest: only when the shared table + ≥2 distinct
	// real-module accessors genuinely exist. Skippable via --skip-pass=shared-db.
	if !i.skipPasses[PassSharedDB] {
		sdStats := engine.ApplySharedDataCoupling(doc)
		if sdStats.Skipped {
			if verbose() {
				fmt.Fprintf(os.Stderr, "grafel: shared-db skipped (no module graph or data access)\n")
			}
		} else if verbose() || sdStats.SharedTables > 0 {
			fmt.Fprintf(os.Stderr,
				"grafel: shared-db tables=%d shared=%d data_access_annotated=%d coupling_edges=%d\n",
				sdStats.TablesConsidered, sdStats.SharedTables,
				sdStats.DataAccessAnnotated, sdStats.CouplingEdges)
		}

		// #5204 — cross-SERVICE shared-table coupling. Orthogonal to the
		// intra-repo module SHARES_DATA edges above: groups resolved literal
		// tables across DISTINCT repos/services and emits SHARES_TABLE_WITH when
		// ≥2 services share one physical table with ≥1 writer. Same data-coupling
		// axis, so it rides the same --skip-pass=shared-db flag.
		stStats := engine.ApplySharedTableCouplingEdges(doc)
		if verbose() || stStats.CouplingEdges > 0 {
			fmt.Fprintf(os.Stderr,
				"grafel: shared-table tables=%d shared=%d coupling_edges=%d services_minted=%d\n",
				stStats.TablesConsidered, stStats.SharedTables,
				stStats.CouplingEdges, stStats.ServicesMinted)
		}
	}

	// Pass 8.9 — dependency-boundary annotation (#3638, epic #3625). Restores
	// the previously-orphaned lib_boundary enricher as a live pass. Runs AFTER
	// the document is assembled (manifest external_dependency entities + import
	// DEPENDS_ON edges + code-to-code DEPENDS_ON edges are all present) so the
	// classifier sees the same graph the dashboard would. Annotates each
	// DEPENDS_ON edge with boundary=first_party (internal/local import or
	// repo-internal code dependency) vs third_party (manifest dep or resolved
	// external import) — a rewrite-scope signal. Reuses the locality/kind
	// properties the extractors already attached; does NOT re-parse source.
	// Honest-partial: edges with ambiguous origin are left unannotated.
	// Skippable via --skip-pass=lib-boundary.
	if !i.skipPasses[PassLibBoundary] {
		lbStats := engine.ApplyLibBoundary(doc)
		if verbose() || lbStats.FirstParty > 0 || lbStats.ThirdParty > 0 {
			fmt.Fprintf(os.Stderr,
				"grafel: lib-boundary edges=%d first_party=%d third_party=%d "+
					"ambiguous=%d entities_annotated=%d\n",
				lbStats.EdgesConsidered, lbStats.FirstParty, lbStats.ThirdParty,
				lbStats.Ambiguous, lbStats.EntitiesAnnotated)
		}
	}

	// Pass 8.10 — DB-migration ordering (#3639, epic #3625). Stamps
	// sequence_number/migration_name from migration filenames + emits Alembic
	// down_revision→revision PRECEDES edges. Skippable via --skip-pass=migration-seq.
	if !i.skipPasses[PassMigrationSeq] {
		msStats := engine.ApplyMigrationSequence(doc, engine.DiskMigrationSourceReader(absRepo))
		if verbose() || msStats.EntitiesAnnotated > 0 {
			fmt.Fprintf(os.Stderr,
				"grafel: migration-seq files=%d entities_annotated=%d precedes_edges=%d\n",
				msStats.FilesMatched, msStats.EntitiesAnnotated, msStats.PrecedesEdges)
		}
	}

	// Pass 8.11 — per-migration schema operations (#3628 [schema], epic #3625).
	// Complements migration-seq (apply-ORDER / PRECEDES) with the actual
	// OPERATIONS: for every migration schema-op entity the language extractors
	// already emit (Alembic SCOPE.Schema, Rails/JS SCOPE.Evolution, Django
	// Migration operations JSON, Flyway/Liquibase SQL CREATE TABLE), derive
	// (op, table[, column]) and emit a MODIFIES_TABLE edge to a synthetic
	// SCOPE.Table convergence node keyed by the SAME normalised table key the
	// query→table (ACCESSES_TABLE) axis uses — then rewire matching
	// SCOPE.DataAccess accessors onto that node so "what touches table X"
	// unifies schema evolution + data access on one node. Honest: dynamic
	// table names are skipped. Skippable via --skip-pass=migration-ops.
	if !i.skipPasses[PassMigrationOps] {
		moStats := engine.ApplyMigrationSchemaOps(doc)
		if verbose() || moStats.ModifiesEdges > 0 {
			fmt.Fprintf(os.Stderr,
				"grafel: migration-ops considered=%d tables=%d modifies_edges=%d access_converged=%d\n",
				moStats.OpsConsidered, moStats.TablesConverged,
				moStats.ModifiesEdges, moStats.AccessConvergedEdges)
		}
	}

	// Pass 8.12 — coverage ingestion + static test-reachability (#5061, wiring
	// the pure passes from #5036 + #5037). Runs AFTER all entity/relationship
	// passes are finalised (so the TESTS+CALLS edges the reachability BFS reads,
	// and the source spans the LCOV attribution overlays, reflect the final
	// graph). Stamps coverage_pct/covered_lines/total_lines/coverage_source (from
	// the configured LCOV report) and test_reachable/reaching_tests/reach_depth
	// (from the in-graph call+test edges) onto entity Properties — no new Kinds,
	// no fb-schema change (the Properties map round-trips via both serializers).
	// OPT-IN + strict no-op when no LCOV report is configured/discovered; the
	// reachability sub-pass still runs since it needs no external report.
	// Skippable via --skip-pass=coverage.
	if !i.skipPasses[PassCoverage] {
		covStats := coverage.Enrich(doc, absRepo, i.coverageCfg)
		if covStats.Skipped {
			if verbose() {
				fmt.Fprintf(os.Stderr, "grafel: coverage skipped (%s)\n", covStats.SkipReason)
			}
		} else if verbose() || covStats.LCOVAttributed > 0 || covStats.ReachabilityReachable > 0 {
			fmt.Fprintf(os.Stderr,
				"grafel: coverage report=%q report_files=%d lcov_attributed=%d "+
					"reachability_production=%d reachable=%d\n",
				covStats.ReportPath, covStats.ReportFiles, covStats.LCOVAttributed,
				covStats.ReachabilityProduction, covStats.ReachabilityReachable)
		}
	}

	// Pass 6a — merge agent-resolved enrichment values back onto entity
	// Properties (PORT-LLM / issue #15). This step MUST stay on the index
	// critical path (unlike candidate emission below): the merged values are
	// part of the authoritative graph.fb content, so they need to land before
	// sortDocumentForEmission / the graph.fb write in Index(), or a
	// previously agent-resolved description would silently vanish from the
	// persisted graph until the next index run. It is cheap (a map merge over
	// already-loaded resolutions), unlike the candidate-collection step,
	// which is the part issue #5720 defers off the critical path.
	i.runPass6ApplyResolutions(doc, absRepo)

	// #5720 — enrichment CANDIDATE EMISSION (the expensive part of Pass 6)
	// no longer runs here. It is scheduled by the Index() caller AFTER
	// graph.fb has been persisted (see enrichment.DefaultScheduler.Schedule
	// in cmd/grafel/index.go), either inline for small graphs or on a
	// silent, single-worker, paced background goroutine for large ones. No
	// consumer hard-depends on enrichment-candidates.json (every reader
	// treats it as optional), so deferring it does not change the
	// authoritative graph in any way.

	// #5334 — surface the imminent graph write as a granular "Writing graph…"
	// phase. The actual graph.fb / sidecar write happens in the Index() caller
	// once Run returns (trk is out of scope there), so we emit the boundary here
	// where the document is fully assembled and about to be persisted. Coarse
	// PhaseMaterialize remains the fallback band.
	trk.Phase(progress.PhaseWriteGraph, "graph.fb", doc.Stats.Entities)

	// Emit the final done event before the summary log line.
	trk.Done(len(files), doc.Stats.Entities)

	dur := time.Since(start)
	fmt.Fprintf(os.Stderr,
		"grafel: processed=%d extracted=%d skipped=%d failed=%d "+
			"entities=%d relationships=%d "+
			"pass1_rels=%d pass2.5_rels=%d pass3_rels=%d "+
			"duration=%s\n",
		i.stats.processed, i.stats.extracted, i.stats.skipped, i.stats.failed,
		doc.Stats.Entities, doc.Stats.Relationships,
		i.stats.pass1Rels, i.stats.pass2Rels, i.stats.pass3Rels,
		dur.Round(time.Millisecond))

	if verbose() {
		printRelBreakdown(os.Stderr, i.stats.pass1RelsByLang, "pass1")
		printRelBreakdown(os.Stderr, i.stats.pass3RelsByExt, "pass3")
	}

	// THE MANIFEST IS NOT WRITTEN HERE. Run does not write it at all — it only
	// hands the walk and the loaded manifest to commitManifest, which Index
	// calls after graph.fb is durably on disk. Run returns BEFORE the graph is
	// written, so writing here would publish a manifest describing a graph that
	// might never land. See commitManifest for the stuck state that causes.
	i.walkedFiles = allFiles
	i.diffManifest = diffManifest

	// Issue #2341 — sanity-check: warn when entities with a recognized source
	// extension have an empty Language tag. A non-zero count here means the
	// indexer pipeline dropped or never set the Language field for these files,
	// which would cause language-filtered queries (grafel_find --language)
	// to silently skip them. This is non-fatal; the graph is still usable.
	warnEmptyLanguageEntities(doc)

	// #6329 — THE GENERATED-SOURCE SEAM. THIS MUST REMAIN THE LAST THING THAT
	// TOUCHES doc.Entities IN Run; anything appended after it is unflagged.
	//
	// It is here, and not at the four extraction call sites, because those are
	// call sites and this is a funnel. Run is the single function every entity
	// in a full index exists inside by the time it finishes, on BOTH production
	// paths — in-process, and under GRAFEL_SUBPROC_EXTRACT where the
	// coordinator merges the children's whole envelope stream.
	//
	// THE PLACEMENT WAS DERIVED FROM MEASUREMENT, TWICE, NOT FROM READING THE
	// CODE. The first cut of #6329 stamped in extractors.safeExtract and called
	// that "the one chokepoint all three production paths funnel through"; it
	// covered Pass 1 only, so *.pb.gw.go carried its own pathRules entry while
	// every grpc-gateway endpoint extracted from it in Pass 3 went unstamped —
	// the exact category the issue was filed about. Moving it to
	// mergePassRecords covered Passes 1/2/2.5/3 and still left five entities
	// per generated Django file unflagged, because
	// ResolveHTTPEndpointHandlersWithRepo synthesises more inside buildDocument.
	// Moving it to the end of buildDocument still left them, because Pass 7
	// (process-flow) and Pass 7.5 (event-flow) append SCOPE.Process entities to
	// doc.Entities AFTER buildDocument has returned.
	//
	// The invariant is "every entity attributed to a generated file", so the
	// stamp belongs after the last producer — and only an end-to-end test over
	// all entities could find where that is. TestGeneratedFlag_CoversEveryPass
	// is that test; it is the deliverable, not this call.
	//
	// safeExtract keeps its own stamp for the daemon's incremental path, which
	// re-extracts a single file and never reaches Run. The stamp is idempotent
	// and both sites resolve through internal/generated, so the overlap cannot
	// produce a disagreement.
	i.generatedFileSet().StampEntities(len(doc.Entities), func(k int) (string, generated.PropSetter) {
		return doc.Entities[k].SourceFile, &doc.Entities[k]
	})

	return doc, nil
}

// knownLanguageExtensions maps source-file suffixes (including the dot) to the
// language tag they imply. Used by warnEmptyLanguageEntities to distinguish
// entities whose Language should be known from truly extension-less ones
// (e.g. synthetic ext:* placeholders).
var knownLanguageExtensions = map[string]string{
	".go":    "go",
	".py":    "python",
	".ts":    "typescript",
	".tsx":   "typescript",
	".js":    "javascript",
	".jsx":   "javascript",
	".java":  "java",
	".rb":    "ruby",
	".rs":    "rust",
	".php":   "php",
	".cs":    "csharp",
	".cpp":   "cpp",
	".cc":    "cpp",
	".cxx":   "cpp",
	".c":     "c",
	".h":     "c",
	".hpp":   "cpp",
	".kt":    "kotlin",
	".swift": "swift",
	".scala": "scala",
	".tf":    "hcl",
	".hcl":   "hcl",
	".yaml":  "yaml",
	".yml":   "yaml",
}

// warnEmptyLanguageEntities logs a warning to stderr when entities with a
// recognized source-file extension have an empty Language tag. The language
// slot is canonical in Entity (FlatBuffers, post-PR #2432); extractors must
// tag via extractor.TagEntitiesLanguage. Issue #2341 — prevents regressions
// where extractors forget to tag.
func warnEmptyLanguageEntities(doc *graph.Document) {
	type bad struct{ kind, name, src string }
	var bads []bad
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.Language != "" || e.SourceFile == "" {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.SourceFile))
		if _, known := knownLanguageExtensions[ext]; known {
			bads = append(bads, bad{e.Kind, e.Name, e.SourceFile})
		}
	}
	if len(bads) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr,
		"grafel: WARNING: %d entities have a recognized source extension but Language=\"\" "+
			"— language-filtered queries will silently skip them (issue #2341). "+
			"First 5 examples:\n", len(bads))
	limit := 5
	if len(bads) < limit {
		limit = len(bads)
	}
	for _, b := range bads[:limit] {
		fmt.Fprintf(os.Stderr, "  kind=%s name=%s src=%s\n", b.kind, b.name, b.src)
	}
}

// verbose reports whether the indexer should emit the per-extractor
// relationship breakdown to stderr. Controlled by GRAFEL_VERBOSE=1.
func verbose() bool {
	v := strings.TrimSpace(os.Getenv("GRAFEL_VERBOSE"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// emitDispositionBreakdown prints the resolver-disposition tally and a few
// representative samples for the bug buckets. Triggered by GRAFEL_VERBOSE.
// Issue #56 — categorised bug-rate reporting.
func emitDispositionBreakdown(w *os.File, s resolve.Stats) {
	var total int
	for _, n := range s.DispositionCounts {
		total += n
	}
	fmt.Fprintln(w, "resolver dispositions:")
	if total == 0 {
		fmt.Fprintln(w, "  (no endpoints classified)")
		return
	}
	for _, d := range resolve.AllDispositions {
		n := s.DispositionCounts[d]
		pct := 100 * float64(n) / float64(total)
		marker := ""
		switch d {
		case resolve.DispositionBugExtractor, resolve.DispositionBugResolver:
			if n > 0 {
				marker = "    <- FIX"
			}
		case resolve.DispositionUnclassified:
			if n > 0 {
				marker = "    <- INVESTIGATE"
			}
		}
		fmt.Fprintf(w, "  %-17s %d (%.1f%%)%s\n", d.String()+"=", n, pct, marker)
	}
	fmt.Fprintf(w, "  bug-rate: %.1f%% (target <=1%%)\n", s.BugRate*100)
	for _, d := range []resolve.Disposition{resolve.DispositionBugExtractor, resolve.DispositionBugResolver} {
		samples := s.DispositionSamples[d]
		if len(samples) == 0 {
			continue
		}
		fmt.Fprintf(w, "samples %s:\n", d.String())
		for _, smp := range samples {
			fmt.Fprintf(w, "  - %s\n", smp)
		}
	}
}

// bugExtractorSampleCount parses GRAFEL_BUG_EXTRACTOR_SAMPLES.
// Issue #89 — temporary diagnostic instrumentation, not a production knob.
func bugExtractorSampleCount() int {
	v := strings.TrimSpace(os.Getenv("GRAFEL_BUG_EXTRACTOR_SAMPLES"))
	if v == "" {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n < 0 {
		return 0
	}
	return n
}

// categorizeBugStub returns a coarse category for a bug-extractor stub.
// Categories are diagnostic only — they help us see which fixes will move
// the bug-rate the most. Issue #89.
func categorizeBugStub(stub string) string {
	if stub == "" {
		return "empty"
	}
	if strings.HasPrefix(stub, "scope:") {
		return "structural-ref"
	}
	// "Kind:Name" prefix?
	name := stub
	hasKind := false
	if i := strings.IndexByte(stub, ':'); i > 0 {
		prefix := stub[:i]
		if len(prefix) <= 24 && isAlphaDot(prefix) {
			name = stub[i+1:]
			hasKind = true
		}
	}
	if name == "" {
		return "kind-only"
	}
	dotted := strings.Contains(name, ".")
	if dotted {
		// First segment a known stdlib/third-party root? then this is an
		// import-shaped call we ought to route through external synthesis.
		root := name
		if d := strings.IndexByte(name, '.'); d > 0 {
			root = name[:d]
		}
		if external.IsKnownExternalPackage(root) {
			return "dotted-known-root"
		}
		// Looks-like-receiver.method (lowercase head) — most often a method
		// call on an imported type whose receiver is unresolved.
		if isLowerStart(root) {
			return "dotted-lower-head"
		}
		return "dotted-other"
	}
	// Bare name.
	if hasKind {
		// Kind:BareName
		if isPythonStdlibBareName(name) || isGoFmtBareName(name) {
			return "bare-stdlib-known"
		}
		return "bare-kind-prefixed"
	}
	if isPythonStdlibBareName(name) || isGoFmtBareName(name) {
		return "bare-stdlib-known"
	}
	return "bare-other"
}

func isAlphaDot(s string) bool {
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '.') {
			return false
		}
	}
	return true
}

func isLowerStart(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c >= 'a' && c <= 'z'
}

// pythonStdlibBareNames is a small in-cmd set used only by the diagnostic
// categorizer. Real classification lives in internal/external.
var pythonStdlibBareNames = map[string]struct{}{
	"len": {}, "range": {}, "list": {}, "dict": {}, "set": {}, "tuple": {},
	"print": {}, "open": {}, "isinstance": {}, "type": {}, "str": {}, "int": {},
	"float": {}, "bool": {}, "enumerate": {}, "zip": {}, "map": {}, "filter": {},
	"sorted": {}, "reversed": {}, "any": {}, "all": {}, "sum": {}, "max": {},
	"min": {}, "abs": {}, "iter": {}, "next": {}, "super": {}, "getattr": {},
	"setattr": {}, "hasattr": {}, "callable": {}, "vars": {}, "id": {}, "hash": {},
	"chr": {}, "ord": {}, "repr": {}, "round": {}, "format": {}, "object": {},
	"slice": {}, "frozenset": {}, "property": {},
}

func isPythonStdlibBareName(s string) bool {
	_, ok := pythonStdlibBareNames[s]
	return ok
}

func isGoFmtBareName(s string) bool {
	switch s {
	case "Println", "Printf", "Print", "Sprintf", "Errorf", "Fatal", "Fatalf", "Panic", "Panicf":
		return true
	}
	return false
}

// dumpBugExtractorSamples writes up to n bug-extractor sample rows + a
// category histogram. Issue #89 diagnostic instrumentation. Format is
// tab-separated lines so it can be piped to awk/sort/uniq.
func dumpBugExtractorSamples(w *os.File, doc *graph.Document, ridx resolve.Index, allow resolve.ExternalAllowlist, n int) {
	// Build entity-id → (file, name, lang) lookup for quick context.
	type ent struct{ file, name, lang string }
	byID := make(map[string]ent, len(doc.Entities))
	for k := range doc.Entities {
		e := &doc.Entities[k]
		byID[e.ID] = ent{file: e.SourceFile, name: e.Name, lang: e.Language}
	}

	cats := make(map[string]int)
	written := 0
	fmt.Fprintf(w, "#bug-extractor-samples (issue #89): n=%d\n", n)
	fmt.Fprintf(w, "#cols: kind\tlang\tcategory\tfrom_file\tfrom_name\tto_stub\n")
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		// Only ToID side — that's where bug-extractor lives in practice
		// (FromID is almost always a hex from a real entity).
		stub := r.ToID
		if stub == "" {
			continue
		}
		// Skip already-resolved hex / external placeholders.
		if isHex16(stub) || strings.HasPrefix(stub, "ext:") {
			continue
		}
		// Run the same classifier the resolver uses post-synthesis.
		lang := r.PropGet("language")
		if lang == "" {
			lang = r.PropGet("lang")
		}
		// We need the language-tagged classifier; replicate via a small
		// wrapper — pass the stub as both resolved + original since
		// post-synth no rewrite happened.
		d := classifyForDiag(ridx, stub, lang, allow)
		if d != resolve.DispositionBugExtractor {
			continue
		}
		cat := categorizeBugStub(stub)
		cats[cat]++
		if written < n {
			from := byID[r.FromID]
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				r.Kind, lang, cat, from.file, from.name, stub)
			written++
		}
	}

	// Histogram footer.
	fmt.Fprintln(w, "#category histogram (all bug-extractor edges):")
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(cats))
	total := 0
	for k, v := range cats {
		pairs = append(pairs, kv{k, v})
		total += v
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].v > pairs[j].v })
	for _, p := range pairs {
		pct := 0.0
		if total > 0 {
			pct = 100 * float64(p.v) / float64(total)
		}
		fmt.Fprintf(w, "#  %-22s %6d (%5.2f%%)\n", p.k, p.v, pct)
	}
	fmt.Fprintf(w, "#  %-22s %6d\n", "TOTAL", total)
}

// bugResolverSampleCount parses GRAFEL_BUG_RESOLVER_SAMPLES.
// Issue #92 — temporary diagnostic instrumentation, not a production knob.
func bugResolverSampleCount() int {
	v := strings.TrimSpace(os.Getenv("GRAFEL_BUG_RESOLVER_SAMPLES"))
	if v == "" {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n < 0 {
		return 0
	}
	return n
}

// dumpBugResolverSamples writes up to n bug-resolver sample rows + a
// category histogram. Issue #92 diagnostic instrumentation. Format is
// tab-separated lines so it can be piped to awk/sort/uniq.
//
// Categories come from resolve.DiagnoseBugResolver — see BugResolverDiag
// for the canonical list.
func dumpBugResolverSamples(w *os.File, doc *graph.Document, ridx resolve.Index, allow resolve.ExternalAllowlist, n int) {
	type ent struct{ file, name, lang string }
	byID := make(map[string]ent, len(doc.Entities))
	for k := range doc.Entities {
		e := &doc.Entities[k]
		byID[e.ID] = ent{file: e.SourceFile, name: e.Name, lang: e.Language}
	}

	cats := make(map[string]int)
	written := 0
	fmt.Fprintf(w, "#bug-resolver-samples (issue #92): n=%d\n", n)
	fmt.Fprintf(w, "#cols: rel_kind\tlang\tcategory\tstub_kind\tname\tkinds_present\trel_hint\tfrom_file\tfrom_name\tto_stub\n")
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		stub := r.ToID
		if stub == "" {
			continue
		}
		if isHex16(stub) || strings.HasPrefix(stub, "ext:") {
			continue
		}
		lang := r.PropGet("language")
		if lang == "" {
			lang = r.PropGet("lang")
		}
		d := classifyForDiag(ridx, stub, lang, allow)
		if d != resolve.DispositionBugResolver {
			continue
		}
		diag := ridx.DiagnoseBugResolver(stub, r.Kind)
		cats[diag.Category]++
		if written < n {
			from := byID[r.FromID]
			kinds := strings.Join(diag.KindsPresent, ",")
			if kinds == "" {
				kinds = "-"
			}
			hint := strings.Join(diag.HintFamily, ",")
			if hint == "" {
				hint = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.Kind, lang, diag.Category, diag.StubKind, diag.Name,
				kinds, hint, from.file, from.name, stub)
			written++
		}
	}

	fmt.Fprintln(w, "#category histogram (all bug-resolver edges):")
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(cats))
	total := 0
	for k, v := range cats {
		pairs = append(pairs, kv{k, v})
		total += v
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].v > pairs[j].v })
	for _, p := range pairs {
		pct := 0.0
		if total > 0 {
			pct = 100 * float64(p.v) / float64(total)
		}
		fmt.Fprintf(w, "#  %-22s %6d (%5.2f%%)\n", p.k, p.v, pct)
	}
	fmt.Fprintf(w, "#  %-22s %6d\n", "TOTAL", total)
}

// classifyForDiag is a tiny shim around the resolver classifier so we don't
// expose internals. It mirrors classifyDispositionLang's external/dynamic
// branches and falls back to name-existence in the index.
func classifyForDiag(idx resolve.Index, stub, lang string, allow resolve.ExternalAllowlist) resolve.Disposition {
	// We can use ClassifyEndpoints with a single endpoint; cheaper path
	// is to call into the package's exported classifier — but it isn't
	// exported. ClassifyEndpoints is exported and computes the same thing.
	stats := idx.ClassifyEndpoints([]resolve.EndpointPair{
		{ToID: stub, ToOriginal: stub, Language: lang},
	}, allow)
	for d, n := range stats.DispositionCounts {
		if n > 0 {
			return d
		}
	}
	return resolve.DispositionUnclassified
}

// isHex16 reports whether s is a 16-char lower-hex string — the shape of
// graph.EntityID() output.
func isHex16(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// printRelBreakdown writes a sorted "label rels by source" table to w.
// Empty or nil maps print a single zero-line so the absence of any signal
// is itself observable in the log.
func printRelBreakdown(w *os.File, counts map[string]int, label string) {
	if len(counts) == 0 {
		fmt.Fprintf(w, "grafel: %s_rels_by_source: (none)\n", label)
		return
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if counts[keys[a]] != counts[keys[b]] {
			return counts[keys[a]] > counts[keys[b]]
		}
		return keys[a] < keys[b]
	})
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	fmt.Fprintf(w, "grafel: %s_rels_by_source: %s\n", label, strings.Join(parts, " "))
}

// carryForwardAlgoAttrs copies Pass-4 community/algorithm output from a
// previously-indexed graph (prev) onto a freshly-built graph (cur) whose
// algo pass was skipped (#1620). Per-entity attributes are matched by stable
// entity ID; the aggregate Communities list and AlgorithmStats are copied
// wholesale. Entities present in cur but not prev (newly added since the last
// full index) are left un-annotated until the next full algo pass runs.
//
// This is what keeps the daemon's fast reactive re-index from stripping the
// live graph community-free: instead of overwriting graph.fb with empty algo
// data, the fast path preserves the last-known communities.
func carryForwardAlgoAttrs(cur, prev *graph.Document) {
	if cur == nil || prev == nil || len(prev.Entities) == 0 {
		return
	}
	// Index prior entities by ID for O(1) lookup of their algo attributes.
	prevByID := make(map[string]*graph.Entity, len(prev.Entities))
	for i := range prev.Entities {
		prevByID[prev.Entities[i].ID] = &prev.Entities[i]
	}
	for k := range cur.Entities {
		e := &cur.Entities[k]
		p, ok := prevByID[e.ID]
		if !ok {
			continue
		}
		if p.CommunityID != nil {
			cid := *p.CommunityID
			e.CommunityID = &cid
		}
		if p.Centrality != nil {
			c := *p.Centrality
			e.Centrality = &c
		}
		if p.PageRank != nil {
			pr := *p.PageRank
			e.PageRank = &pr
		}
		e.IsGodNode = p.IsGodNode
		e.IsSurpriseEndpoint = p.IsSurpriseEndpoint
		e.IsArticulationPt = p.IsArticulationPt
	}
	// Carry the aggregate community list + corpus stats forward so
	// grafel_clusters and the graph Community view keep working between
	// full algo passes.
	cur.Communities = prev.Communities
	cur.SurpriseEdges = prev.SurpriseEdges
	if prev.AlgorithmStats != nil {
		as := *prev.AlgorithmStats
		cur.AlgorithmStats = &as
	}
}

// runPass4Algorithms executes the gonum-backed graph-algorithm sweep against
// the deduped entity set inside doc. Per-entity attributes (community_id,
// centrality, pagerank, is_*-flags) are attached in place; corpus aggregates
// are appended to the Document and copied into the graph-stats.json sidecar
// at write time.
// parseCanaryBaselinePath returns the path to the committed per-language
// parse-error baseline. GRAFEL_CANARY_BASELINE overrides it; otherwise it is
// docs/grammar-canary-baseline.json under the grafel source tree (resolved
// from the running binary's repo when available, else relative to cwd). The
// baseline travels with the grammar, not the indexed repo, because it captures
// what the bundled grammars produce — not properties of any one codebase.
func parseCanaryBaselinePath() string {
	if p := os.Getenv("GRAFEL_CANARY_BASELINE"); p != "" {
		return p
	}
	return filepath.Join("docs", "grammar-canary-baseline.json")
}

// buildParseErrorCanary runs the A4 canary (#5414): it snapshots the run's
// per-language ERROR-node stats, loads the committed baseline, classifies each
// language for a spike, logs a WARN line per spiking language, and returns the
// report as raw JSON (for the stats sidecar) plus the top-level spike flag.
// Returns (nil, false) when nothing was parsed (heuristic-only index) so the
// sidecar field is simply omitted.
func (i *Indexer) buildParseErrorCanary() (json.RawMessage, bool) {
	if i.stats.parseCanary == nil {
		return nil, false
	}
	snap := i.stats.parseCanary.Snapshot()
	if len(snap) == 0 {
		return nil, false
	}

	baselinePath := parseCanaryBaselinePath()
	baseline, err := treesitter.LoadBaseline(baselinePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grafel: parse-error canary: baseline load failed: %v\n", err)
		baseline = nil
	}

	rep := treesitter.Classify(snap, baseline, treesitter.ThresholdsFromEnv())

	if rep.Spiked {
		for _, l := range rep.Languages {
			if !l.Spiked {
				continue
			}
			fmt.Fprintf(os.Stderr,
				"grafel: WARN parse-error canary SPIKE language=%s current=%.4f baseline=%.4f delta=%.4f reason=%s files=%d nodes=%d (possible unhandled new syntax / stale grammar — see docs/grammar-freshness-audit.md A4)\n",
				l.Language, l.CurrentRate, l.BaselineRate, l.Delta, l.Reason, l.Files, l.TotalNodes)
		}
	}

	raw, mErr := json.Marshal(rep)
	if mErr != nil {
		fmt.Fprintf(os.Stderr, "grafel: parse-error canary: marshal failed: %v\n", mErr)
		return nil, rep.Spiked
	}
	return json.RawMessage(raw), rep.Spiked
}

func (i *Indexer) runPass4Algorithms(doc *graph.Document) {
	i.runPass4AlgorithmsWithProgress(doc, nil)
}

// runPass4AlgorithmsWithProgress is the instrumented variant of runPass4Algorithms.
// trk may be nil (treated as no-op). Emits an AlgorithmEvent at the entry and
// exit of each named algorithm.
func (i *Indexer) runPass4AlgorithmsWithProgress(doc *graph.Document, trk *progress.Tracker) {
	entityCount := doc.Stats.Entities

	// #5334 — surface the two real algorithm passes as granular phases so the
	// CLI and wizard show "Building communities…" then "Computing centrality…"
	// instead of one coarse "Running algorithms". RunAlgorithms is a single
	// combined call (we don't rewire the individual algorithms), so we emit the
	// community-detection label at entry and the centrality label once the
	// combined pass has produced its results. The coarse PhaseAlgorithms
	// AlgorithmEvent is retained as a fallback for any consumer still keyed on
	// it.
	if trk != nil {
		trk.AlgorithmEvent("Louvain+PageRank+Betweenness", entityCount)
		trk.Phase(progress.PhaseBuildCommunities, "Louvain", entityCount)
	}
	res := graph.RunAlgorithms(doc.Entities, doc.Relationships)
	if trk != nil {
		trk.Phase(progress.PhaseComputeCentrality, "PageRank+Betweenness", entityCount)
		trk.AlgorithmEvent("ArticulationPoints+SurpriseEdges", entityCount)
	}

	for k := range doc.Entities {
		e := &doc.Entities[k]
		if cid, ok := res.CommunityID[e.ID]; ok {
			cidCopy := cid
			e.CommunityID = &cidCopy
		}
		if p, ok := res.PageRank[e.ID]; ok {
			pCopy := p
			e.PageRank = &pCopy
			// #5954 — Centrality is keyed off PageRank membership, not off its
			// own: PageRank covers every entity that took part in the pass,
			// whereas the betweenness map now holds only non-zero scores. An
			// entity in the pass with no betweenness entry scores 0, exactly as
			// the removed zero pre-seed recorded it, so graph.json keeps the
			// explicit "centrality": 0 it has always carried.
			cCopy := res.Centrality[e.ID]
			e.Centrality = &cCopy
		}
		if res.GodNodes[e.ID] {
			e.IsGodNode = true
		}
		if res.SurpriseEndpoints[e.ID] {
			e.IsSurpriseEndpoint = true
		}
		if res.ArticulationPoints[e.ID] {
			e.IsArticulationPt = true
		}
	}

	doc.Communities = res.Communities
	doc.SurpriseEdges = res.SurpriseEdges
	stats := res.Stats
	doc.AlgorithmStats = &stats
}

// runPass6ApplyResolutions merges any prior agent-resolved enrichment values
// back onto entity Properties (and community AgentName). This MUST stay on
// the index critical path — see the call site in Run() for why (#5720): the
// merged values are part of the authoritative graph.fb content. It is cheap
// (a map merge over already-loaded resolutions), unlike candidate collection
// (runPass6EmitEnrichmentCandidates / runPass6EmitEnrichmentCandidatesBG
// below), which is the part #5720 defers off the critical path.
func (i *Indexer) runPass6ApplyResolutions(doc *graph.Document, absRepo string) {
	if i.skipPasses[PassEnrichment] {
		return
	}
	if doc == nil {
		return
	}
	grafelDir := daemon.StateDirForRepo(absRepo)

	resolutions := enrichment.ReadResolutions(grafelDir)
	if len(resolutions) == 0 {
		return
	}
	applied := enrichment.ApplyResolutions(doc, resolutions)
	if verbose() {
		fmt.Fprintf(os.Stderr,
			"enrichment: applied %d resolutions to entities\n", applied)
	}
	// Also apply name_community resolutions so the AgentName field is
	// populated on CommunityResult before candidate emission and before
	// graph.json is written (issue #426).
	appliedComm := enrichment.ApplyCommunityNameResolutions(doc, resolutions)
	if verbose() && appliedComm > 0 {
		fmt.Fprintf(os.Stderr,
			"enrichment: applied %d community name resolutions\n", appliedComm)
	}
}

// runPass6EmitEnrichmentCandidates runs the registered CandidateEmitters and
// writes the resulting candidate list to
// <repo>/.grafel/enrichment-candidates.json in one shot. This is the INLINE
// path (#5720 requirement 6): used only for graphs at or below
// enrichment.InlineEntityThreshold entities, where the one-shot cost is small
// enough to pay synchronously right after graph.fb is written. Larger graphs
// use runPass6EmitEnrichmentCandidatesBG via the Scheduler instead. The pass
// is a no-op when PassEnrichment is in the skip set.
//
// Resolutions must already have been applied (runPass6ApplyResolutions) —
// this function only collects + writes candidates.
func (i *Indexer) runPass6EmitEnrichmentCandidates(doc *graph.Document, absRepo string) {
	if i.skipPasses[PassEnrichment] {
		return
	}
	if doc == nil {
		return
	}
	grafelDir := daemon.StateDirForRepo(absRepo)

	cands := i.collectAllEnrichmentCandidates(doc, absRepo, grafelDir)

	if err := enrichment.WriteCandidates(grafelDir, cands); err != nil {
		fmt.Fprintf(os.Stderr, "grafel: enrichment candidate write failed: %v\n", err)
		return
	}
	// Issue #53: keep this log behind the verbose flag. The emit count is
	// useful for debugging but noisy on every CI run.
	if verbose() {
		fmt.Fprintf(os.Stderr,
			"grafel: emitted %d enrichment candidates to %s\n",
			len(cands), filepath.Join(grafelDir, "enrichment-candidates.json"))
	}
}

// collectAllEnrichmentCandidates runs every candidate source (entity
// emitters, name_community, repair_edge, dynamic_baseurl) in one shot and
// returns the merged slice. Used only by the INLINE (small-graph) path —
// the background trickle path (runPass6EmitEnrichmentCandidatesBG) collects
// entity candidates in chunks instead, but still runs the other (already
// small — one per community/entity-subset, not per-entity) sources in one
// shot at the end.
func (i *Indexer) collectAllEnrichmentCandidates(doc *graph.Document, absRepo, grafelDir string) []enrichment.Candidate {
	cands := enrichment.CollectCandidatesSkippingRejected(
		doc, enrichment.DefaultEmitters(), grafelDir,
	)
	cands = append(cands, i.collectSmallEnrichmentCandidateSources(doc, absRepo, grafelDir)...)
	return cands
}

// collectSmallEnrichmentCandidateSources collects the candidate sources that
// are inherently small (one per community, or gated behind an opt-in flag) —
// safe to always run as a single, un-chunked step even from the background
// trickle path.
func (i *Indexer) collectSmallEnrichmentCandidateSources(doc *graph.Document, absRepo, grafelDir string) []enrichment.Candidate {
	var cands []enrichment.Candidate

	// Emit name_community candidates (issue #426 Layer 2). One candidate per
	// community that lacks an agent-resolved name.
	rej := enrichment.ReadRejections(grafelDir)
	commCands := enrichment.CollectCommunityCandidates(doc, rej)
	if len(commCands) > 0 {
		cands = append(cands, commCands...)
		if verbose() {
			fmt.Fprintf(os.Stderr,
				"enrichment: collected %d name_community candidates\n", len(commCands))
		}
	}

	// ADR-0015 phase-1 (#544) — repair_edge emission. Purely additive; gated
	// behind --enable-repair-candidates so we can land the foundation
	// without bumping bug-rate measurement noise.
	if i.enableRepairCandidates && i.resolveIdx != nil {
		allow := resolve.ExternalAllowlist(external.IsKnownExternalPackage)
		repair := enrichment.CollectRepairEdgeCandidates(doc, enrichment.RepairEdgeCandidateOptions{
			RepoRoot: absRepo,
			Allow:    allow,
			Resolver: i.resolveIdx,
		})
		if len(repair) > 0 {
			cands = append(cands, repair...)
		}
		if verbose() {
			fmt.Fprintf(os.Stderr,
				"enrichment: collected %d repair_edge candidates (ADR-0015 phase-1)\n",
				len(repair))
		}
	}

	// Issue #708 — dynamic-baseurl endpoint candidates. Emitted
	// unconditionally (no resolver required) whenever there are
	// consumer-side http_endpoint entities whose baseURL is
	// runtime-determined (runtime_dynamic=true or dynamic_baseurl=true).
	// These surface in grafel_repairs action=list so an agent can annotate
	// them with a curated baseURL hint.
	dynBaseURL := enrichment.CollectDynamicBaseURLCandidates(doc)
	if len(dynBaseURL) > 0 {
		cands = append(cands, dynBaseURL...)
		if verbose() {
			fmt.Fprintf(os.Stderr,
				"enrichment: collected %d dynamic_baseurl_endpoint candidates (#708)\n",
				len(dynBaseURL))
		}
	}

	return cands
}

// runPass6EmitEnrichmentCandidatesBG is the background-worker counterpart of
// runPass6EmitEnrichmentCandidates (#5720). It streams entity candidates to
// disk in small, paced chunks via enrichment.CollectAndAppendTrickle /
// enrichment.CandidateAppender instead of building one full-graph
// []Candidate slice and calling enrichment.WriteCandidates — that one-shot
// path is exactly what double-materialized the prior sidecar and drove the
// ~11GB peak this issue fixes. Honors ctx: a cancellation (a new index of the
// same repo superseding this run, via enrichment.DefaultScheduler) aborts the
// in-progress appender so the prior on-disk candidates file is left
// untouched rather than partially overwritten.
func (i *Indexer) runPass6EmitEnrichmentCandidatesBG(ctx context.Context, doc *graph.Document, absRepo string) {
	if i.skipPasses[PassEnrichment] {
		return
	}
	if doc == nil {
		return
	}
	grafelDir := daemon.StateDirForRepo(absRepo)

	appender, err := enrichment.NewCandidateAppender(grafelDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grafel: enrichment (bg) appender init failed: %v\n", err)
		return
	}
	// Safety net for a panic mid-trickle (#5739 item d, refs #5736 review):
	// the explicit Abort() calls below cover every known error/cancellation
	// path, but a panic unwinds past all of them and would otherwise leave an
	// orphaned enrichment-candidates.json.trickle.tmp-* in the state dir with
	// no sweep to reclaim it. Abort() is idempotent with Close() (both set
	// a.closed and no-op if already set), so on the normal success path below
	// — where Close() already committed the rename — this deferred Abort() is
	// a harmless no-op, not a second write.
	defer appender.Abort()

	rej := enrichment.ReadRejections(grafelDir)
	err = enrichment.CollectAndAppendTrickle(ctx, doc, enrichmentEmittersForBG(), rej, appender, enrichment.TrickleOptions{})
	if err != nil {
		// Cancelled (superseded) or failed mid-stream — never publish a
		// partial file over whatever was there before.
		appender.Abort()
		if verbose() && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "grafel: enrichment (bg) trickle failed: %v\n", err)
		}
		return
	}

	// The small, non-per-entity candidate sources run as one extra chunk —
	// still cheap even on a huge graph, since these are bounded by
	// community/endpoint count, not entity count.
	if err := ctx.Err(); err != nil {
		appender.Abort()
		return
	}
	extra := i.collectSmallEnrichmentCandidateSources(doc, absRepo, grafelDir)
	if len(extra) > 0 {
		if err := appender.AppendChunk(extra); err != nil {
			appender.Abort()
			fmt.Fprintf(os.Stderr, "grafel: enrichment (bg) trickle failed: %v\n", err)
			return
		}
	}

	if err := appender.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "grafel: enrichment (bg) candidate write failed: %v\n", err)
		return
	}
	if verbose() {
		fmt.Fprintf(os.Stderr,
			"grafel: emitted %d enrichment candidates (background) to %s\n",
			appender.Count(), filepath.Join(grafelDir, "enrichment-candidates.json"))
	}
}

// runPass1Extract runs the per-file AST extractors in parallel. The classified
// slice is also returned for reuse by Pass 2.5 and Pass 3 so we don't pay the
// classification + read + parse cost twice.
func (i *Indexer) runPass1Extract(ctx context.Context, absRepo string, files []string) ([]types.EntityRecord, []classifiedFile, error) {
	return i.runPass1ExtractWithProgress(ctx, absRepo, files, nil)
}

// runPass1ExtractWithProgress is the instrumented variant of runPass1Extract.
// trk may be nil; when non-nil it receives a Tick every progress.TickEveryNFiles
// files processed, with the current repo-relative file path and byte count.
func (i *Indexer) runPass1ExtractWithProgress(ctx context.Context, absRepo string, files []string, trk *progress.Tracker) ([]types.EntityRecord, []classifiedFile, error) {
	if i.skipPasses[PassExtract] {
		// Even when Pass 1 is skipped we still need to classify+read so
		// downstream passes have file content. Run the worker loop in
		// classification-only mode.
		classified, _ := i.classifyAndReadWithProgress(ctx, absRepo, files, false, trk)
		return nil, classified, nil
	}

	// Pre-pass (#698): build the cross-file Python class registry before the
	// per-file extraction runs. Scanning is a lightweight line-based pass (no
	// AST), single-threaded to avoid lock contention on the global registry.
	// This allows extractBaseClasses in the Python extractor to resolve
	// cross-file `class Foo(Bar):` shapes to the correct source file when
	// exactly one project file declares `Bar`.
	//
	// Pre-pass (#1278): build the cross-file DRF register-name registry.
	// Scans every Python file for router.register() basenames so that
	// applyDjangoRouteComposition can suppress bare Route entities produced by
	// the YAML rules even when include(router.urls) lives in a different file.
	pyextr.ClearPythonClassRegistry()
	engine.ClearDRFRegisterNames()
	for _, rel := range files {
		abs := filepath.Join(absRepo, rel)
		if !strings.HasSuffix(strings.ToLower(rel), ".py") {
			continue
		}
		// #6212 — this is the FIRST of two reads of every .py file, and its
		// output reaches the graph (cross-file INHERITS via extractBaseClasses,
		// and DRF bare-Route suppression). So it is a point the graph is built
		// from, and it must be stamped like one.
		//
		// recordWalkStamp is first-wins, so this stamp beats the worker's later
		// read of the same file. That is the correct direction: if a write lands
		// between the two reads, the registry holds pre-write bytes and the
		// entities post-write, and only the EARLIER stamp re-presents the file
		// as changed next pass. Stamping the later read would hash-match the
		// disk and freeze a wrong INHERITS edge in place.
		statSize, statMtime := int64(-1), int64(0)
		if st, serr := osStat(abs); serr == nil {
			statSize = st.Size()
			statMtime = st.ModTime().UnixNano()
		}
		if content, err := osReadFile(abs); err == nil {
			i.stampReadFile(rel, content, statSize, statMtime)
			pyextr.ScanPythonClassRegistry(rel, string(content))
			engine.ScanDRFRegisterNames(content)
		}
	}

	classified, records := i.classifyAndReadWithProgress(ctx, absRepo, files, true, trk)
	return records, classified, nil
}

// classifyAndReadWithProgress is like classifyAndRead but also publishes
// per-file progress ticks via trk (which may be nil for no-op behaviour).
// A tick is published every progress.TickEveryNFiles files to bound the
// event rate on large repos.
func (i *Indexer) classifyAndReadWithProgress(ctx context.Context, absRepo string, files []string, runExtract bool, trk *progress.Tracker) ([]classifiedFile, []types.EntityRecord) {
	// Issue #2320 — build the typed ExtractorConfig once per index run from
	// the process environment. This is stamped on every FileInput so extractors
	// can consult feature toggles via the Config channel (Config wins; env var
	// is the backward-compat fallback). Future work: merge a config file on top.
	extractorCfg := extractor.ConfigFromEnv()

	// Use a shared atomic counter so all workers contribute to the same
	// tick cadence without needing an additional mutex acquisition per file.
	var (
		fileCounter int64 // total files processed (classified + skipped + failed)
		byteCounter int64 // cumulative bytes read
	)

	tasks := make(chan fileTask, len(files))
	for _, rel := range files {
		tasks <- fileTask{relPath: rel, absPath: filepath.Join(absRepo, rel)}
	}
	close(tasks)

	var (
		mu         sync.Mutex
		allRecords []types.EntityRecord
		classified []classifiedFile
	)

	// Per-module tick cadence (per-module progress bars). The bare global
	// fileCounter gate — tick only when the GLOBAL count hits a multiple of
	// TickEveryNFiles — emitted ZERO per-module ticks for a monorepo with fewer
	// than TickEveryNFiles files, and only incidental coverage otherwise (a
	// module got a row only if one of its files happened to land on a global
	// multiple of 20). We instead track each module and emit on its FIRST file
	// (prompt per-module row), on its own N-file cadence (liveness within a big
	// module), or on its LAST file (a module smaller than N still reaches its
	// own 100% mid-run); a final per-module flush after the workers join
	// guarantees every package reaches
	// 100%. Each tick carries the MODULE's own done/total (via TickModule), so
	// the bars advance INDEPENDENTLY like the per-repo fleet bars rather than in
	// lockstep on the aggregate. Guarded by pmMu; only active with a tracker.
	//
	// Precompute per-module TOTALS once from the (post-incremental-filter) file
	// list so each bar knows its own denominator. Files with no meaningful
	// module (repo-root strays → module.RootLabel, or no resolver) are excluded:
	// they get no independent bar (the wizard has no row to show them in).
	var (
		pmMu           sync.Mutex
		moduleCount    = map[string]int{}
		moduleLastFile = map[string]string{}
		moduleTotal    = map[string]int{}
	)
	if trk != nil {
		for _, rel := range files {
			if m := trk.ResolveModule(rel); moduleRowWorthy(m) {
				moduleTotal[m]++
			}
		}
	}
	recordAndMaybeTick := func(bytesSeen int64, currentFile string) {
		if trk == nil {
			return
		}
		mod := trk.ResolveModule(currentFile)
		if !moduleRowWorthy(mod) {
			return
		}
		pmMu.Lock()
		moduleCount[mod]++
		c := moduleCount[mod]
		total := moduleTotal[mod]
		moduleLastFile[mod] = currentFile
		pmMu.Unlock()
		// Coalescing downstream collapses volume, so over-emitting is cheap and
		// safe: prefer guaranteed per-module coverage over minimal event count.
		// Emit on this module's FIRST file, its own N-file cadence, or its last
		// file (so a module smaller than N still reaches its own 100% mid-run).
		if c == 1 || c%progress.TickEveryNFiles == 0 || c == total {
			trk.TickModule(progress.PhaseExtractAST, mod, c, total, bytesSeen, currentFile, 0)
		}
	}

	var wg sync.WaitGroup
	for w := 0; w < i.workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range tasks {
				// #6212 — this stat is also the manifest stamp's size/mtime, and
				// it is taken BEFORE the read on purpose. See diff.StampBytes.
				size := int64(-1)
				statMtime := int64(0)
				if st, err := osStat(t.absPath); err == nil {
					size = st.Size()
					statMtime = st.ModTime().UnixNano()
				}
				statSize := size
				cr := i.classifier.ClassifyWithSize(ctx, t.relPath, size)
				// #6338 — record the "no extractor claimed this extension"
				// skips before the file is dropped. The tally filters on the
				// skip REASON, so vendored/binary/oversized files below are
				// not folded in.
				i.stats.unsupportedExt.Observe(t.relPath, cr)
				if cr.Skip || cr.Language == "" {
					// Hashed INLINE, here, rather than batched after the worker
					// pool joins (#6212). The classifier decided on the size
					// this stat reported, so the stamp has to be taken against
					// the same observation: deferring it to the end of
					// extraction let an oversized file be truncated in between
					// and stamped at its new, small content — which then
					// hash-matches on the next pass and is never extracted even
					// though it is now indexable. Serialising a big hash into a
					// worker is a throughput cost; that was a correctness one.
					if entry, herr := idiff.HashFile(t.absPath); herr == nil {
						i.recordWalkStamp(t.relPath, entry)
					}
					mu.Lock()
					i.stats.skipped++
					mu.Unlock()
					atomic.AddInt64(&fileCounter, 1)
					recordAndMaybeTick(atomic.LoadInt64(&byteCounter), t.relPath)
					continue
				}

				content, err := osReadFile(t.absPath)
				if err != nil {
					// Deliberately UNSTAMPED. With no bytes there is no evidence
					// of what was indexed, and stamping a transient failure from
					// a later successful hash would claim content nothing ever
					// extracted. Unstamped means the file reads as new next pass
					// — over-reporting, never staleness, which is the same rule
					// the hash-error branch in stampUnreadFiles follows.
					mu.Lock()
					i.stats.failed++
					mu.Unlock()
					atomic.AddInt64(&fileCounter, 1)
					recordAndMaybeTick(atomic.LoadInt64(&byteCounter), t.relPath)
					continue
				}

				// The graph is built from THESE bytes, so this is the hash the
				// manifest must carry (#6212). Recorded here rather than
				// re-derived at commit time, which is what made the manifest
				// able to describe content the walk never saw.
				i.stampReadFile(t.relPath, content, statSize, statMtime)

				if size < 0 {
					size = int64(len(content))
				}
				atomic.AddInt64(&byteCounter, size)

				cf := classifiedFile{
					relPath:  t.relPath,
					absPath:  t.absPath,
					language: cr.Language,
					content:  content,
				}

				file := extractor.FileInput{
					Path:     t.relPath,
					Content:  content,
					Language: cr.Language,
					RepoRoot: absRepo,
					// Issue #2320 — populate the typed Config channel so
					// extractors use Config-first / env-fallback precedence.
					Config: &extractorCfg,
				}

				// PLT #537 — route .tsx and .jsx through the tsx grammar
				// (JSX-enabled superset of typescript). Plain `typescript`
				// grammar treats JSX tags as syntax errors, which produced
				// 90%+ ERROR-ratio trees on RN/Expo source files and
				// stopped the JS extractor from reaching function /
				// class declarations inside React components. The entity
				// Language tag stays "typescript" so downstream gating
				// (dynamic patterns, allowlists, hint families) keeps
				// firing under the standard tag.
				parseLang := cr.Language
				if parseLang == "typescript" || parseLang == "javascript" {
					low := strings.ToLower(t.relPath)
					if strings.HasSuffix(low, ".tsx") || strings.HasSuffix(low, ".jsx") {
						parseLang = "tsx"
					}
				}
				if pr, perr := i.parser.Parse(ctx, content, parseLang); perr == nil && pr != nil {
					file.TSTree = pr.TSTree
					// A4 canary (#5414): fold this parse's ERROR-node ratio
					// into the per-language accumulator. Key by the classifier
					// language (not the tsx parse override) so .tsx/.jsx roll
					// up under typescript/javascript. Guarded by mu below.
					mu.Lock()
					i.stats.parseCanary.Observe(cr.Language, pr.ErrorRatio, pr.NodeCount)
					mu.Unlock()
				}

				if !runExtract {
					// #5954 — nothing will consume this tree; release the
					// CGo allocation now (see the close below for why).
					if file.TSTree != nil {
						file.TSTree.Close()
						file.TSTree = nil
					}
					mu.Lock()
					classified = append(classified, cf)
					mu.Unlock()
					atomic.AddInt64(&fileCounter, 1)
					recordAndMaybeTick(atomic.LoadInt64(&byteCounter), t.relPath)
					continue
				}

				ents, extractErr := extractPass1(ctx, file)

				// #5989 — dispatch the custom/framework extractors registered
				// under internal/custom/** (Django/DRF/Celery/Flask/FastAPI/
				// SQLAlchemy + 20 other languages). They register under
				// PREFIXED registry keys ("python_django"), which the exact-key
				// Get(file.Language) lookup inside extractors.Extract above can
				// never match — RunCustomExtractors is the only dispatcher that
				// selects them.
				//
				// SEAM PLACEMENT IS LOAD-BEARING: this must sit AFTER
				// extractors.Extract (so base entities exist to merge into) and
				// BEFORE the TSTree.Close() below, because custom extractors
				// walk the same parse tree. Moving it after the Close is a
				// use-after-free, not merely a no-op.
				//
				// Default-OFF: see inProcCustomExtractors (cmd/grafel/inproc_custom.go)
				// for why, and why this is NOT the same thing as flipping
				// GRAFEL_SUBPROC_EXTRACT.
				//
				// THE MERGE IS NON-DESTRUCTIVE (#6104, fixed). MergeWithCustom
				// used to supersede by NAME alone, so a custom entity whose Name
				// collided with a base entity REPLACED it — 280 entities across 5
				// kinds and 2 languages on the #5989 fixtures. It is now keyed on
				// the graph's own identity (SourceFile, Kind, Name) and enforces
				// two invariants: a merge never loses an entity, and a merge never
				// narrows a span. Measured effects per language now:
				//
				//   python — the Celery task ADDS a SCOPE.Service; the base
				//            Task and SCOPE.Operation are no longer destroyed by
				//            the merge. (Task is subsequently folded into
				//            SCOPE.Service by the #1613 class-shadow fold, which
				//            repoints its edges — a combine, not a drop.)
				//   java   — SCOPE.Operation keeps its full span; only the Subtype
				//            is refined method->endpoint, with the displaced value
				//            retained under types.EntityBaseSubtypeProperty. The
				//            derived http_endpoint_definition is no longer
				//            truncated.
				//   js     — route operations gain real positions (base emits them
				//            at line 0). A widening, never a narrowing.
				//   go, ruby — genuinely inert; identical content tuples.
				//
				// Pinned by TestInProcCustomExtractorsSupersedeIsNonDestructive and
				// TestInProcCustomExtractorsJavaEnrichment. The gate stays
				// default-OFF: its wall/alloc cost (+17.5% wall, +18.2%
				// TotalAlloc) has never been measured at corpus scale, and #6105
				// (synthetic Class:<Name> endpoints resolving to nothing) is still
				// open. Flipping the default is a separate decision.
				//
				// The `file.TSTree != nil` guard is load-bearing beyond avoiding
				// a use-after-free: a subset of custom extractors work on file
				// CONTENT and emit entities even with no parse tree, so without
				// the guard a file that FAILED to parse would still produce
				// custom entities. See
				// TestInProcCustomExtractorsNilTreeGuardIsLoadBearing.
				if (i.customExtractors || inProcCustomExtractors()) && file.TSTree != nil {
					customEnts, customErrs := extractors.RunCustomExtractors(ctx, file)
					if len(customErrs) > 0 && verbose() {
						for _, ce := range customErrs {
							fmt.Fprintf(os.Stderr, "custom extractor: %v\n", ce)
						}
					}
					if len(customEnts) > 0 {
						// MergeWithCustom applies the #4402 supersede
						// semantics (carry base QualifiedName + structural
						// edges onto the surviving custom entity) rather than
						// a naive append; cross-kind dedup still happens
						// downstream in deduplicateEntities.
						ents = extractors.MergeWithCustom(ents, customEnts)
					}
				}

				// #5954 — release the parse tree the moment its only consumer
				// (the Pass-1 language extractor above) has returned. Mirrors
				// internal/daemon/extract/subproc.go:446-453 ("this is what
				// keeps batch RSS bounded", ADR 0023 / #5418).
				//
				// WHY THIS MATTERS: tree-sitter trees are CGo-allocated —
				// invisible to the Go heap profiler and to GOMEMLIMIT — and
				// cost ~19.7 bytes of C heap per source byte (~89 B/node).
				// go-tree-sitter@v0.24.0 installs NO finalizer, so an
				// un-Close()d tree leaks for the life of the process. Holding
				// one tree per classified file (the old behaviour: stash it on
				// classifiedFile, close it only after Pass 3) pinned ~1.4-1.6
				// GB of C heap on a 24k-file corpus, and macOS libmalloc never
				// returns that high-water to the OS. Keeping the live set at
				// one tree per worker keeps it under ~50 MB.
				//
				// INVARIANT: extractors return plain []types.EntityRecord and
				// must not retain a ts.Node past Extract — a Node holds a
				// pointer INTO the tree, so retaining one past Close is a
				// use-after-free. Close() is NOT idempotent (it calls
				// ts_tree_delete unconditionally), hence the nil-out.
				if file.TSTree != nil {
					file.TSTree.Close()
					file.TSTree = nil
				}

				relCount := 0
				for k := range ents {
					relCount += len(ents[k].Relationships)
				}
				mu.Lock()
				i.stats.processed++
				if extractErr != nil {
					// #5989 CAVEAT: `ents` is DISCARDED entirely on this branch —
					// allRecords is only appended in the else below. When the
					// custom-extractor gate is on, that means any file whose base
					// extractor returned an error (notably
					// ErrNoExtractorForLanguage) has already run ~340 custom
					// extractors, allocated their output and merged it, only for
					// all of it to be thrown away here.
					//
					// Two consequences: (a) wasted work proportional to the number
					// of such files, and (b) a trap for anyone who later expects a
					// language to be covered by custom extractors ALONE — that can
					// never work while base failure discards the merged result.
					// Deliberately left as-is: changing it would alter default-path
					// behaviour for base-extractor failures, which is out of scope
					// for a default-off gate.
					if errors.Is(extractErr, extractors.ErrNoExtractorForLanguage) {
						i.stats.skipped++
					} else {
						i.stats.failed++
						// #6209 — the bytes were read and stamped, but nothing
						// came out of them. Name the file so commitManifest can
						// qualify its stamp instead of recording it as indexed.
						// Kept OUT of the ErrNoExtractorForLanguage branch on
						// purpose: an unsupported language is a permanent,
						// correct classification, not a failure to retry.
						i.recordFailedExtraction(t.relPath)
					}
				} else {
					i.stats.extracted++
					allRecords = append(allRecords, ents...)
					if i.stats.pass1RelsByLang != nil {
						i.stats.pass1RelsByLang[cr.Language] += relCount
					}
				}
				classified = append(classified, cf)
				mu.Unlock()

				atomic.AddInt64(&fileCounter, 1)
				recordAndMaybeTick(atomic.LoadInt64(&byteCounter), t.relPath)
			}
		}()
	}
	wg.Wait()

	// Final per-module flush: emit each module at its OWN 100% (moduleTotal /
	// moduleTotal) at end-of-extraction so every package's row reaches complete
	// and survives downstream coalescing even for fast/small repos where no
	// periodic tick fired. NOTE: a monorepo with >256 distinct modules can burst
	// past the sidecar's 256-event buffer here — acceptable: the sidecar
	// coalesces to latest-per-(repo,module) on disk so every module's row still
	// survives, though a dropped final tick may leave a row a hair under 100%.
	if trk != nil {
		bytesSeen := atomic.LoadInt64(&byteCounter)
		pmMu.Lock()
		mods := make([]string, 0, len(moduleLastFile))
		for m := range moduleLastFile {
			mods = append(mods, m)
		}
		sort.Strings(mods)
		for _, m := range mods {
			total := moduleTotal[m]
			trk.TickModule(progress.PhaseExtractAST, m, total, total, bytesSeen, moduleLastFile[m], 0)
		}
		pmMu.Unlock()
	}

	// Issue #481 — worker-pool outputs accumulate in goroutine-scheduling
	// order. Sort by canonical fields so downstream passes (BuildIndex
	// first-writer-wins, dedup) see a stable slice and graph.json is
	// byte-identical across runs.
	sortClassifiedFiles(classified)
	sortEntityRecords(allRecords)
	return classified, allRecords
}

// runPass25FrameworkRules applies the YAML rule engine to every classified
// file. Returns extra entity records (from source_patterns) plus standalone
// relationship records (from relationship_rules).
func (i *Indexer) runPass25FrameworkRules(ctx context.Context, absRepo string, classified []classifiedFile, pass1Records []types.EntityRecord) ([]types.EntityRecord, []types.RelationshipRecord, error) {
	if i.skipPasses[PassFramework] {
		return nil, nil, nil
	}

	// Pass 1 side-channel (issue #2352): group Pass 1 entity records by
	// source file so each Pass 2.5 FileInput can carry the canonical
	// extractor entities for ITS file forward to engine passes (notably
	// applyORMFieldEdges, which previously re-parsed source with a regex
	// because field entities weren't visible inside the detector loop).
	//
	// Only entity kinds the engine passes actually consume are forwarded
	// today — SCOPE.Schema(subtype=field) for ORM field-access. Expanding
	// the filter later is cheap: just relax the predicate.
	var pass1ByFile map[string][]types.EntityRecord
	if len(pass1Records) > 0 {
		pass1ByFile = make(map[string][]types.EntityRecord, len(classified))
		for _, e := range pass1Records {
			if e.SourceFile == "" {
				continue
			}
			if e.Kind != "SCOPE.Schema" || e.Subtype != "field" {
				continue
			}
			pass1ByFile[e.SourceFile] = append(pass1ByFile[e.SourceFile], e)
		}
	}

	// Cross-file ORM field lookup (issue #2448 / Phase B): build a single
	// closure over the union of Pass-1 field entities across the WHOLE
	// repo and attach it to every per-file FileInput below. Engine passes
	// (notably applyORMFieldEdges) consult it when a Django ORM call site
	// references a model defined in a SIBLING file — the canonical
	// `models.py` + `views.py` split. Nil result (no field entities in
	// Pass 1) is valid and leaves per-file detection unchanged.
	crossFileFields := engine.BuildCrossFileFieldLookup(pass1Records)

	// Pre-pass (#845): build the cross-file Java DI registry before the
	// per-file synthesis pass runs. Scanning is cheap (regex, no AST),
	// single-threaded to avoid lock contention on the global registry.
	engine.ClearJavaDIRegistry()
	for _, cf := range classified {
		if cf.language == "java" {
			engine.ScanJavaDIRegistry(string(cf.content))
		}
	}

	var (
		mu       sync.Mutex
		entities []types.EntityRecord
		rels     []types.RelationshipRecord
	)

	// Issue #2447: atomic counters for Pass1Entities plumbing observability.
	// Track how many files had FileInput.Pass1Entities non-empty (True) vs
	// empty (False) when Detector.Detect was called for Pass 2.5.
	//
	// Heterogeneous-repo semantics (issue #2464): non-Django files never
	// produce SCOPE.Schema(subtype=field) entities in Pass 1, so they
	// legitimately contribute to FalseCount. A non-zero FalseCount is
	// therefore EXPECTED on any multi-language repository.
	var plumbedTrue, plumbedFalse atomic.Int64

	work := make(chan classifiedFile, len(classified))
	for _, cf := range classified {
		work <- cf
	}
	close(work)

	var wg sync.WaitGroup
	for w := 0; w < i.workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for cf := range work {
				input := extractor.FileInput{
					Path:            cf.relPath,
					Content:         cf.content,
					Language:        cf.language,
					RepoRoot:        absRepo,
					Pass1Entities:   pass1ByFile[cf.relPath],
					CrossFileFields: crossFileFields,
				}
				// Issue #2447: count plumbed vs unplumbed before Detect.
				if len(input.Pass1Entities) > 0 {
					plumbedTrue.Add(1)
				} else {
					plumbedFalse.Add(1)
				}
				res, err := i.detector.Detect(ctx, input)
				if err != nil || res == nil {
					continue
				}
				if len(res.Entities) == 0 && len(res.Relationships) == 0 {
					continue
				}
				mu.Lock()
				entities = append(entities, res.Entities...)
				rels = append(rels, res.Relationships...)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Fold atomic counters into indexer stats (issue #2447).
	i.stats.pass1PlumbedTrue += int(plumbedTrue.Load())
	i.stats.pass1PlumbedFalse += int(plumbedFalse.Load())
	// Issue #481 — deterministic ordering across runs.
	sortEntityRecords(entities)
	sortRelationshipRecords(rels)
	return entities, rels, nil
}

// runPass3CrossLang runs every registered cross-language extractor against
// every classified file. The cross extractors short-circuit on languages
// they don't handle, so the cost on irrelevant files is small.
//
// This is the critical fix flagged by the PORT-1 review: the
// internal/extractors/cross/* packages had ZERO callers before this pass.
func (i *Indexer) runPass3CrossLang(ctx context.Context, absRepo string, classified []classifiedFile) ([]types.EntityRecord, error) {
	if i.skipPasses[PassCrossLang] {
		return nil, nil
	}
	exts := cross.AllExtractors()
	if len(exts) == 0 {
		return nil, nil
	}

	var (
		mu  sync.Mutex
		out []types.EntityRecord
	)

	work := make(chan classifiedFile, len(classified))
	for _, cf := range classified {
		work <- cf
	}
	close(work)

	var wg sync.WaitGroup
	for w := 0; w < i.workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for cf := range work {
				input := extractor.FileInput{
					Path:     cf.relPath,
					Content:  cf.content,
					Language: cf.language,
					RepoRoot: absRepo,
				}
				// No TSTree is stamped here (#5954): every registered cross
				// extractor reads only Path/Content/Language, and Pass 1 has
				// already closed the tree. See classifiedFile.
				for _, e := range exts {
					ents, err := e.Extractor.Extract(ctx, input)
					if err != nil {
						continue
					}
					if len(ents) == 0 {
						continue
					}
					rc := 0
					for k := range ents {
						rc += len(ents[k].Relationships)
					}
					mu.Lock()
					out = append(out, ents...)
					if i.stats.pass3RelsByExt != nil {
						i.stats.pass3RelsByExt[e.Name] += rc
					}
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	// Issue #481 — deterministic ordering across runs.
	sortEntityRecords(out)

	// After collecting raw cross-extractor output, resolve bare-name
	// to_id references against the union of Pass 1 + Pass 2.5 + Pass 3
	// entities. Resolution is a best-effort same-name lookup keyed by
	// (kind, name). If no match is found we leave the bare name in place
	// — this preserves the truly-external (stdlib) signal the report calls
	// out — but cross-file user calls now resolve to a stable entity ID.
	return out, nil
}

// runDjangoNestedURLConf runs the cross-file Django URLconf composition
// pass over the set of classified files. It builds a content-lookup map
// from repo-relative path to raw bytes, then delegates to
// engine.ApplyDjangoNestedURLConf.
func runDjangoNestedURLConf(classified []classifiedFile) []types.EntityRecord {
	if len(classified) == 0 {
		return nil
	}
	// Build a quick lookup: relPath → content (Python files only).
	contentByPath := make(map[string][]byte, len(classified))
	var pyPaths []string
	for _, cf := range classified {
		if cf.language != "python" {
			continue
		}
		contentByPath[cf.relPath] = cf.content
		pyPaths = append(pyPaths, cf.relPath)
	}
	reader := func(relPath string) []byte {
		return contentByPath[relPath]
	}
	return engine.ApplyDjangoNestedURLConf(pyPaths, reader)
}

// runDjangoDRFRoutes runs the DRF router.register expansion pass over the
// classified files. Emits http_endpoint entities for every DRF CRUD detail
// route and every @action decorated method (#703, #705). Reuses the same
// per-Python-file content cache as runDjangoNestedURLConf.
func runDjangoDRFRoutes(classified []classifiedFile) []types.EntityRecord {
	if len(classified) == 0 {
		return nil
	}
	contentByPath := make(map[string][]byte, len(classified))
	var pyPaths []string
	for _, cf := range classified {
		if cf.language != "python" {
			continue
		}
		contentByPath[cf.relPath] = cf.content
		pyPaths = append(pyPaths, cf.relPath)
	}
	reader := func(relPath string) []byte {
		return contentByPath[relPath]
	}
	return engine.ApplyDjangoDRFRoutes(pyPaths, reader)
}

// runDjangoAdminRoutes runs the Django admin URL synthesis pass (#801).
// Emits http_endpoint synthetics for every ModelAdmin registration found in
// admin.py files: admin.site.register, @admin.register, and class-based
// admin definitions. Also emits site-level routes (login, logout, etc.)
// once per project.
func runDjangoAdminRoutes(classified []classifiedFile) []types.EntityRecord {
	if len(classified) == 0 {
		return nil
	}
	contentByPath := make(map[string][]byte, len(classified))
	var pyPaths []string
	for _, cf := range classified {
		if cf.language != "python" {
			continue
		}
		contentByPath[cf.relPath] = cf.content
		pyPaths = append(pyPaths, cf.relPath)
	}
	reader := func(relPath string) []byte {
		return contentByPath[relPath]
	}
	all := engine.ApplyDjangoAdminRoutes(pyPaths, reader)

	// #1617 — drop the Django admin CRUD scaffolding endpoints. The synthesis
	// pass tags every framework-generated route (changelist/add/change/delete/
	// history/login/logout/…) with scaffolding="true"; only project-authored
	// custom actions and get_urls() overrides carry scaffolding="false". The
	// scaffolding family (commonly ~12% of all defs) has no inbound
	// architectural signal and swamps the real endpoint surface, so we exclude
	// it from the graph here.
	kept := all[:0]
	for _, e := range all {
		if e.Properties != nil && e.Properties["scaffolding"] == "true" {
			continue
		}
		kept = append(kept, e)
	}
	if dropped := len(all) - len(kept); dropped > 0 {
		fmt.Fprintf(os.Stderr, "grafel: django_admin_scaffolding_pruned=%d endpoints\n", dropped)
	}
	return kept
}

// runCeleryDispatchEdges runs the repo-wide Celery cross-file dispatch pass
// (#1617). Collects every @shared_task / @app.task definition across the repo
// and emits a CALLS edge from each `task.delay()` / `.apply_async()` / `.s()`
// call site's enclosing function to the task definition.
func runCeleryDispatchEdges(classified []classifiedFile) []types.RelationshipRecord {
	if len(classified) == 0 {
		return nil
	}
	contentByPath := make(map[string][]byte, len(classified))
	var pyPaths []string
	for _, cf := range classified {
		if cf.language != "python" {
			continue
		}
		contentByPath[cf.relPath] = cf.content
		pyPaths = append(pyPaths, cf.relPath)
	}
	reader := func(relPath string) []byte { return contentByPath[relPath] }
	return engine.ApplyCeleryDispatchEdges(pyPaths, reader)
}

// runDjangoSignalPubSub runs the repo-wide Django custom-signal pub/sub pass
// (#1617). Models each `sig = Signal()` as a SCOPE.MessageTopic with
// SUBSCRIBES_TO from @receiver(sig) handlers and PUBLISHES_TO from sig.send()
// callers.
func runDjangoSignalPubSub(classified []classifiedFile) ([]types.EntityRecord, []types.RelationshipRecord) {
	if len(classified) == 0 {
		return nil, nil
	}
	contentByPath := make(map[string][]byte, len(classified))
	var pyPaths []string
	for _, cf := range classified {
		if cf.language != "python" {
			continue
		}
		contentByPath[cf.relPath] = cf.content
		pyPaths = append(pyPaths, cf.relPath)
	}
	reader := func(relPath string) []byte { return contentByPath[relPath] }
	return engine.ApplyDjangoSignalPubSub(pyPaths, reader)
}

// runDjangoCBVRoutes runs the Django CBV generic-method resolution pass
// over the classified files. Emits per-verb http_endpoint synthetics and
// SCOPE.Operation synthetics for inherited HTTP handler methods on
// class-based views (#786).
func runDjangoCBVRoutes(classified []classifiedFile) []types.EntityRecord {
	if len(classified) == 0 {
		return nil
	}
	contentByPath := make(map[string][]byte, len(classified))
	var pyPaths []string
	for _, cf := range classified {
		if cf.language != "python" {
			continue
		}
		contentByPath[cf.relPath] = cf.content
		pyPaths = append(pyPaths, cf.relPath)
	}
	reader := func(relPath string) []byte {
		return contentByPath[relPath]
	}
	return engine.ApplyDjangoCBVRoutes(pyPaths, reader)
}

// runDjangoViewSetAsViewRoutes runs the DRF ViewSet.as_view({dict}) route
// expansion pass (#2614). Handles the explicit method-map pattern where a
// ViewSet is mounted outside of a router.register() call via either a
// pre-bound variable (_name = VS.as_view({'get': 'list'})) or an inline
// call inside path("...", VS.as_view({'post': 'create'})).
func runDjangoViewSetAsViewRoutes(classified []classifiedFile) []types.EntityRecord {
	if len(classified) == 0 {
		return nil
	}
	contentByPath := make(map[string][]byte, len(classified))
	var pyPaths []string
	for _, cf := range classified {
		if cf.language != "python" {
			continue
		}
		contentByPath[cf.relPath] = cf.content
		pyPaths = append(pyPaths, cf.relPath)
	}
	reader := func(relPath string) []byte {
		return contentByPath[relPath]
	}
	return engine.ApplyDjangoViewSetAsViewRoutes(pyPaths, reader)
}

// runSerializerMetaModelEdges emits REFERENCES edges from DRF Serializer
// classes to the Model class named in their inner Meta.model = X declaration
// (#2578). Repo-wide Python pass.
func runSerializerMetaModelEdges(classified []classifiedFile) []types.RelationshipRecord {
	if len(classified) == 0 {
		return nil
	}
	contentByPath := make(map[string][]byte, len(classified))
	var pyPaths []string
	for _, cf := range classified {
		if cf.language != "python" {
			continue
		}
		contentByPath[cf.relPath] = cf.content
		pyPaths = append(pyPaths, cf.relPath)
	}
	reader := func(relPath string) []byte { return contentByPath[relPath] }
	return engine.ApplySerializerMetaModelEdges(pyPaths, reader)
}

// runReceiverSenderEdges emits HANDLES_SIGNAL edges from @receiver(…,
// sender=Model) handler functions to the named Model class (#2578).
// Repo-wide Python pass.
func runReceiverSenderEdges(classified []classifiedFile) []types.RelationshipRecord {
	if len(classified) == 0 {
		return nil
	}
	contentByPath := make(map[string][]byte, len(classified))
	var pyPaths []string
	for _, cf := range classified {
		if cf.language != "python" {
			continue
		}
		contentByPath[cf.relPath] = cf.content
		pyPaths = append(pyPaths, cf.relPath)
	}
	reader := func(relPath string) []byte { return contentByPath[relPath] }
	return engine.ApplyReceiverSenderEdges(pyPaths, reader)
}

// runFilterSetMetaModelEdges emits REFERENCES edges from django_filter
// FilterSet classes to the Model class named in their inner Meta.model = X
// declaration (#2578). Repo-wide Python pass.
func runFilterSetMetaModelEdges(classified []classifiedFile) []types.RelationshipRecord {
	if len(classified) == 0 {
		return nil
	}
	contentByPath := make(map[string][]byte, len(classified))
	var pyPaths []string
	for _, cf := range classified {
		if cf.language != "python" {
			continue
		}
		contentByPath[cf.relPath] = cf.content
		pyPaths = append(pyPaths, cf.relPath)
	}
	reader := func(relPath string) []byte { return contentByPath[relPath] }
	return engine.ApplyFilterSetMetaModelEdges(pyPaths, reader)
}

// stampEntityIDs computes the deterministic graph entity ID for every
// EntityRecord in the merged slice and writes it into EntityRecord.ID. The
// resolver consumes EntityRecord.ID, so this must run before BuildIndex.
//
// Issue #6275 — also repoints any grafel.twin_of (#6104 merge-facet anchor)
// property that still names a record's PRE-STAMP id. A facet's twin_of is
// written at merge time (internal/extractors/custom_dispatch.go's
// enrichFromTwin, via effectiveID) BEFORE this function ever runs, so when
// the anchor record's ID has not been stamped yet, effectiveID falls back to
// EntityRecord.ComputeID() — a different hash than graph.EntityID computes
// here. Unlike a RelationshipRecord endpoint (which every fold/merge/resolve
// pass downstream already knows how to rekey — see the #4406 dedup-by-ID gap
// fill and the #1613 class-shadow fold's remap maps), a twin_of value is an
// opaque Properties string: nothing else in the pipeline ever looks at it or
// rewrites it. Left alone, it permanently names an id that will never appear
// in the graph, so internal/resolve/symbol_index.go's facet-alias rule
// (#6104, mergeFacetAnchor) can never match the facet to its anchor and the
// anchor's name falls to the ambiguous-name path meant for a genuine
// same-name collision, not an orphaned facet.
//
// Every producer that pre-computes r.ID via ComputeID() (see the many
// `e.ID = e.ComputeID()` call sites across internal/custom and
// internal/extractors) has that value UNCONDITIONALLY overwritten by the loop
// below anyway, so ComputeID() is exactly the right "pre-stamp identity" to
// build the remap from — it is what every record's id looked like at the
// moment any earlier pass (like enrichFromTwin) could have captured it.
//
// PERFORMANCE: a second sha256 (ComputeID()) per record, purely to build a
// remap map, would run on every entity in every corpus even though twin_of is
// rare (only #6104 custom-extractor facets carry it). recordsHaveTwinOf below
// is a single Properties-map lookup per record — no hashing, no allocation —
// so the plain single-hash stamping loop is untouched on the overwhelmingly
// common path where nothing in the batch has a twin_of at all.
func (i *Indexer) stampEntityIDs(records []types.EntityRecord) {
	if !recordsHaveTwinOf(records) {
		for k := range records {
			r := &records[k]
			if r.Name == "" {
				continue
			}
			r.ID = graph.EntityID(i.repoTag, r.Kind, r.Name, r.SourceFile)
		}
		return
	}

	// preStampToFinal maps a record's pre-stamp ComputeID() to the final
	// graph.EntityID() this function assigns it, but only when they differ.
	preStampToFinal := make(map[string]string)
	for k := range records {
		r := &records[k]
		if r.Name == "" {
			continue
		}
		preStampID := r.ComputeID()
		finalID := graph.EntityID(i.repoTag, r.Kind, r.Name, r.SourceFile)
		if preStampID != finalID {
			preStampToFinal[preStampID] = finalID
		}
		r.ID = finalID
	}
	if len(preStampToFinal) == 0 {
		return
	}
	for k := range records {
		r := &records[k]
		if r.Properties == nil {
			continue
		}
		twin, ok := r.Properties[types.EntityTwinOfProperty]
		if !ok || twin == "" {
			continue
		}
		if final, remapped := preStampToFinal[twin]; remapped {
			r.Properties[types.EntityTwinOfProperty] = final
		}
	}
}

// recordsHaveTwinOf reports whether ANY record in the batch carries a
// grafel.twin_of property — the gate for stampEntityIDs's slow (remap-
// building) path. A cheap map-lookup scan, no hashing.
func recordsHaveTwinOf(records []types.EntityRecord) bool {
	for k := range records {
		if records[k].Properties == nil {
			continue
		}
		if v := records[k].Properties[types.EntityTwinOfProperty]; v != "" {
			return true
		}
	}
	return false
}

// buildPatternContainsRels emits one CONTAINS edge per SCOPE.Pattern entity,
// targeting it from the per-source-file SCOPE.Component (subtype="file")
// entity emitted by extractor.FileEntity. Both endpoint IDs are computed
// directly with graph.EntityID so the resolver leaves them untouched
// (isHexID short-circuit). When the source file has no file-level entity
// the edge still points at the canonical file-entity ID — a future fix
// that adds the missing file entity heals the dangling FromID.
//
// Called after stampEntityIDs in buildDocument. Returns nil when records
// contains no SCOPE.Pattern entities (zero-cost on graphs with no patterns).
func (i *Indexer) buildPatternContainsRels(records []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for k := range records {
		r := &records[k]
		if r.Kind != "SCOPE.Pattern" {
			continue
		}
		if r.SourceFile == "" || r.ID == "" {
			continue
		}
		fileID := graph.EntityID(i.repoTag, "SCOPE.Component", r.SourceFile, r.SourceFile)
		out = append(out, types.RelationshipRecord{
			FromID: fileID,
			ToID:   r.ID,
			Kind:   "CONTAINS",
		})
	}
	return out
}

// buildMongoAggStageJoinRels emits the NODE-ANCHORED JOINS_COLLECTION twin for
// every Mongo-aggregation `$lookup` / `$graphLookup` stage entity, with a
// FIRST-CLASS FromID equal to the stage entity's own (already-stamped) graph id
// — `r.ID`. This is the #4244 fix: the per-stage SCOPE.DataAccess node a
// consumer `find`s ("inspections.aggregate@L38#9 $lookup") must be traversable
// to its join target via `node → JOINS_COLLECTION → Class:<from>`. The two
// previous fixes emitted this twin at EXTRACT time with a structural-ref STUB
// FromID and relied on the resolver to rewrite it to the node id; that rewrite
// did NOT land on the node's actual id in production (twin FromID stayed a
// synthetic value ≠ graph.EntityID(<the node>)), so neighbors(<node>) was empty
// live — twice. Emitting here, AFTER stampEntityIDs, lets us use the node's real
// id directly (no stub, no resolver round-trip) — exactly like
// buildPatternContainsRels emits file→Pattern CONTAINS edges.
//
// Targets: the stage entity's `from` property (the top-level lookup target) plus
// any extra targets recorded under join_targets (the Python correlated
// sub-pipeline nested froms). Both endpoints are deterministic — FromID is a
// resolved hex id (resolver leaves it untouched via isHexID), ToID is the
// canonical Class:<from> node the collection-anchored edge already points at.
// Runs after stampEntityIDs so r.ID is populated. Returns nil when no
// aggregation stage entities are present (zero-cost on graphs without Mongo).
func (i *Indexer) buildMongoAggStageJoinRels(records []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for k := range records {
		r := &records[k]
		if r.ID == "" || r.Properties == nil {
			continue
		}
		if r.Kind != string(types.EntityKindDataAccess) {
			continue
		}
		if r.Properties["pattern_type"] != engine.MongoAggPatternType {
			continue
		}
		// Only $lookup / $graphLookup stages carry a join target; other
		// stages ($group, $facet, …) have no `from` and no join_targets.
		var targets []string
		if from := r.Properties["from"]; from != "" {
			targets = append(targets, from)
		}
		if extra := r.Properties[engine.MongoAggStageJoinTargetsKey]; extra != "" {
			for _, t := range strings.Split(extra, ",") {
				if t != "" {
					targets = append(targets, t)
				}
			}
		}
		for _, t := range targets {
			out = append(out, engine.MongoAggStageNodeJoinRel(r.ID, t))
		}
	}
	return out
}

// foldShadowStats reports the result of foldClassHierarchyShadows for the
// stderr log line.
type foldShadowStats struct {
	// ShadowsFolded is the number of INFERRED_FROM_CLASS_HIERARCHY shadows
	// dropped because a real typed node existed for the same source+symbol.
	ShadowsFolded int
	// EdgesRepointed is the number of edge endpoints (FromID/ToID, embedded
	// or standalone) rewritten from a folded shadow's ID to its survivor.
	EdgesRepointed int
	// ShadowsBackfilled is the number of shadows that survived (no real typed
	// node) and now carry a real start_line (>0) from the extractor.
	ShadowsBackfilled int
	// ShadowsStillLine0 is the residual count of surviving shadows that still
	// have start_line==0 (regex could not anchor a line — should be ~0).
	ShadowsStillLine0 int
}

// The class-fold vocabulary (kind priority, canonical rank, class-like
// subtypes) and the two predicates below MOVED to internal/engine (#6148).
//
// They were private to this file while the full rebuild was the only path that
// folded. The incremental path now has to resolve the SAME framework-typed kind
// for a class it re-extracts — a class arriving in a changed file was typed by
// a full rebuild and left as a generic SCOPE.Component by the incremental one —
// and a second copy of these tables would be free to drift from this one. The
// aliases keep every existing reference (including this package's tests)
// pointing at the single definition.
//
// They are ALIASES, not copies: `var x = engine.Y` on a map copies the header,
// so both names address one backing map. Writing through either — a test poking
// an entry, say — mutates engine's state for the whole process. Nothing writes
// to them today, and nothing should; if one ever needs a local override, copy
// it explicitly at the point of use.
var (
	frameworkClassKindPriority  = engine.FrameworkClassKindPriority
	frameworkClassKindCanonRank = engine.FrameworkClassKindCanonRank
	classLikeComponentSubtypes  = engine.ClassLikeComponentSubtypes
)

func isShadowRecord(r *types.EntityRecord) bool { return engine.IsClassHierarchyShadow(r) }

func isFoldSource(r *types.EntityRecord) bool { return engine.IsClassFoldSource(r) }

// foldClassHierarchyShadows folds line-less / generic class nodes into the real
// framework-typed node (View/Model/Controller/…) for the same source_file +
// symbol when one exists, so every class resolves to ONE node with a real line
// span + qualified_name. Issue #1613.
//
// Fold sources (see isFoldSource): the INFERRED_FROM_CLASS_HIERARCHY shadow and
// the generic SCOPE.Component/class AST node. For each:
//   - If a framework-typed node (see frameworkClassKindPriority) exists for the
//     same (SourceFile, Name): DROP the source and remap its stamped ID to the
//     survivor. The survivor keeps its real start_line/end_line/qualified_name/
//     language; any property the source carried that the survivor lacks (e.g.
//     is_abstract) is copied over.
//   - Otherwise the source SURVIVES as the single node for that class — the
//     extractor already stamped a real start_line (+ qualified_name for Python).
//
// After deciding folds, every edge endpoint (embedded EntityRecord.Relationships
// across ALL records, plus the standalone pass2Rels stream) that points at a
// folded source's ID is rewritten to the survivor's ID. Embedded edges OWNED by
// a folded source (those it emitted) are re-homed onto the survivor record so
// they are still surfaced at assembly time. Edges are never dropped.
//
// Runs after stampEntityIDs + the resolver passes so r.ID is populated and
// embedded rel endpoints are already in hex-ID form where resolvable.
func (i *Indexer) foldClassHierarchyShadows(
	merged []types.EntityRecord,
	pass2Rels []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord, foldShadowStats) {
	var stats foldShadowStats

	// remap: folded source ID -> survivor ID. drop: indices of folded-away records.
	// Declared up front because the sibling-survivor fold below (which collapses a
	// double-emitted class's extra framework-typed nodes) populates both.
	remap := make(map[string]string)
	drop := make(map[int]bool)

	// Index framework-typed survivor candidates by (SourceFile, Name).
	type cand struct {
		idx  int
		id   string
		pri  int
		rank int
		ln   int
	}
	bySymbol := make(map[[2]string][]cand)
	for k := range merged {
		r := &merged[k]
		if r.Name == "" {
			continue
		}
		// Issue #6275 — a #6104 merge-facet TWIN (grafel.twin_of set to a
		// DIFFERENT entity's id) is never an eligible fold SURVIVOR, even
		// when its Kind carries a FrameworkClassKindPriority (e.g. the
		// Hibernate custom extractor's SCOPE.Schema twin of a base
		// SCOPE.Component class, priority 80). A twin is a second
		// representation of the SAME source construct that the #6104 merge
		// boundary (internal/extractors/custom_dispatch.go's Tier B)
		// deliberately keeps BOTH nodes for — "both survive" is the whole
		// point. Treating it as a fold survivor here folds the twin's own
		// anchor INTO it, which is the opposite of that contract: the base
		// class node (and every edge it owns) disappears, leaving the twin
		// holding content under the wrong Kind. #6275's java-spring-mini
		// regression is precisely this — SCOPE.Component User's 8
		// CONTAINS/IMPORTS edges end up stranded on SCOPE.Schema User.
		if r.IsMergeTwinAlias() {
			continue
		}
		pri, ok := frameworkClassKindPriority[r.Kind]
		if !ok {
			continue
		}
		key := [2]string{r.SourceFile, r.Name}
		bySymbol[key] = append(bySymbol[key], cand{
			idx: k, id: r.ID, pri: pri, rank: frameworkClassKindCanonRank[r.Kind], ln: r.StartLine,
		})
	}

	// bestCand picks the single canonical survivor among framework-typed
	// candidates for one (file, name). Precedence (strongest first):
	//   1. highest frameworkClassKindPriority (class-declaration strength),
	//   2. highest frameworkClassKindCanonRank (structural-role tiebreaker for
	//      equal priority — Model/View/Service/… beat Controller; see that map),
	//   3. smallest real (>0) start_line (the declaration; 0 == "no line" loses),
	//   4. smallest id (stable, deterministic across runs).
	bestCand := func(cands []cand) cand {
		best := cands[0]
		for _, c := range cands[1:] {
			switch {
			case c.pri != best.pri:
				if c.pri > best.pri {
					best = c
				}
			case c.rank != best.rank:
				if c.rank > best.rank {
					best = c
				}
			case c.ln != best.ln:
				// Prefer a real (>0) smaller line; treat 0 as "no line" (worst).
				if (best.ln == 0 && c.ln > 0) || (c.ln > 0 && c.ln < best.ln) {
					best = c
				}
			default:
				if c.id < best.id {
					best = c
				}
			}
		}
		return best
	}

	// Sibling-survivor fold (#3172/#3195): when one class symbol is double-emitted
	// under several framework-typed kinds (e.g. a Django models.Model class
	// surfacing as BOTH "Model" AND "Controller"), every emission is a survivor
	// CANDIDATE and none is a fold *source*, so the fold-source loop below would
	// never collapse them against each other and the class would resolve to >1
	// node — violating the #1613 invariant. Here we pre-collapse each such symbol
	// to its single canonical survivor (bestCand), dropping the weaker siblings,
	// remapping their stamped IDs onto the survivor, and re-homing their
	// properties + owned edges. After this, bySymbol holds exactly one candidate
	// per symbol so the fold-source loop targets a unique survivor.
	for key, cands := range bySymbol {
		if len(cands) < 2 {
			continue
		}
		win := bestCand(cands)
		sv := &merged[win.idx]
		if sv.Properties == nil {
			sv.Properties = map[string]string{}
		}
		for _, c := range cands {
			if c.idx == win.idx {
				continue
			}
			r := &merged[c.idx]
			drop[c.idx] = true
			if r.ID != "" && r.ID != win.id {
				remap[r.ID] = win.id
			}
			stats.ShadowsFolded++
			for pk, pv := range r.Properties {
				if pk == "provenance" || pk == "ref" {
					continue
				}
				if _, exists := sv.Properties[pk]; !exists {
					sv.Properties[pk] = pv
				}
			}
			for ri := range r.Relationships {
				rel := r.Relationships[ri]
				if rel.FromID == "" || rel.FromID == r.ID {
					rel.FromID = win.id
				}
				sv.Relationships = append(sv.Relationships, rel)
			}
			r.Relationships = nil
		}
		bySymbol[key] = []cand{win}
	}

	// Index non-shadow records by stamped ID so a surviving shadow can detect a
	// sibling AST SCOPE.Component node (same ID) to defer to (#1613): the shadow
	// and the per-language AST class node share an EntityID, so only one is
	// emitted at assembly. We want the AST node (real coordinates, no inference
	// provenance) to win, and we strip the now-misleading shadow provenance from
	// any shadow that survives with real coordinates.
	nonShadowByID := make(map[string]bool)
	for k := range merged {
		r := &merged[k]
		if r.ID != "" && !isShadowRecord(r) {
			nonShadowByID[r.ID] = true
		}
	}

	for k := range merged {
		if drop[k] {
			// Already folded away as a non-canonical sibling survivor.
			continue
		}
		r := &merged[k]
		if !isFoldSource(r) {
			continue
		}
		cands := bySymbol[[2]string{r.SourceFile, r.Name}]
		if len(cands) == 0 {
			// No framework-typed survivor — this node IS the single class node.
			if isShadowRecord(r) {
				// If a sibling AST SCOPE.Component node (same ID) exists, drop
				// the shadow and let the AST node carry the class (it has real
				// coordinates and no inference provenance). Otherwise the shadow
				// is the only node: keep it, but strip the now-misleading
				// INFERRED_FROM_CLASS_HIERARCHY provenance since it points at
				// real source (start_line>0). Record residual stats.
				if nonShadowByID[r.ID] {
					drop[k] = true
					stats.ShadowsFolded++
					// Re-home edges the shadow owns onto the standalone stream
					// with an explicit FromID (== the shared ID the AST sibling
					// also uses) so they are not lost when this record is dropped.
					for ri := range r.Relationships {
						rel := r.Relationships[ri]
						if rel.FromID == "" {
							rel.FromID = r.ID
						}
						pass2Rels = append(pass2Rels, rel)
					}
					r.Relationships = nil
					continue
				}
				if r.StartLine > 0 {
					stats.ShadowsBackfilled++
					delete(r.Properties, "provenance")
				} else {
					stats.ShadowsStillLine0++
				}
			}
			continue
		}
		// Pick the strongest class-like candidate (see bestCand: priority, then
		// canonical-kind rank, then real start_line, then id for stability).
		// Post sibling-survivor fold, cands holds a single winner per symbol.
		best := bestCand(cands)
		if best.id == r.ID {
			// Degenerate (same ID) — leave as-is.
			continue
		}
		drop[k] = true
		remap[r.ID] = best.id
		stats.ShadowsFolded++

		// Copy useful properties the survivor lacks (e.g. is_abstract, role).
		sv := &merged[best.idx]
		if sv.Properties == nil {
			sv.Properties = map[string]string{}
		}
		for pk, pv := range r.Properties {
			if pk == "provenance" || pk == "ref" {
				continue
			}
			if _, exists := sv.Properties[pk]; !exists {
				sv.Properties[pk] = pv
			}
		}
		// Re-home edges the shadow OWNS (emitted on its own record). Their
		// FromID may be "" (meaning the owner's ID) — bind it to the survivor.
		for ri := range r.Relationships {
			rel := r.Relationships[ri]
			if rel.FromID == "" || rel.FromID == r.ID {
				rel.FromID = best.id
			}
			sv.Relationships = append(sv.Relationships, rel)
		}
		r.Relationships = nil
	}

	if len(remap) == 0 && len(drop) == 0 {
		return merged, pass2Rels, stats
	}

	// Re-point every edge endpoint that targets a folded shadow.
	rewrite := func(id string) (string, bool) {
		if nv, ok := remap[id]; ok {
			return nv, true
		}
		return id, false
	}
	for k := range merged {
		if drop[k] {
			continue
		}
		r := &merged[k]
		for ri := range r.Relationships {
			rel := &r.Relationships[ri]
			if nv, ok := rewrite(rel.FromID); ok {
				rel.FromID = nv
				stats.EdgesRepointed++
			}
			if nv, ok := rewrite(rel.ToID); ok {
				rel.ToID = nv
				stats.EdgesRepointed++
			}
		}
	}
	for ri := range pass2Rels {
		rel := &pass2Rels[ri]
		if nv, ok := rewrite(rel.FromID); ok {
			rel.FromID = nv
			stats.EdgesRepointed++
		}
		if nv, ok := rewrite(rel.ToID); ok {
			rel.ToID = nv
			stats.EdgesRepointed++
		}
	}

	// Compact: drop the folded shadow records.
	out := merged[:0]
	for k := range merged {
		if drop[k] {
			continue
		}
		out = append(out, merged[k])
	}
	return out, pass2Rels, stats
}

// foldFileComponentStats reports the result of foldFileComponentDuplicates.
type foldFileComponentStats struct {
	// Observed is the number of SCOPE.Component class-like nodes examined as
	// candidates for folding.
	Observed int
	// Folded is the number of SCOPE.Component class-like nodes collapsed into
	// their co-located SCOPE.Component(subtype="file") sibling.
	Folded int
	// EdgesRepointed is the number of edge endpoints rewritten from a folded
	// class-like component ID to its file-entity survivor ID.
	EdgesRepointed int
	// SpansDonated is the number of positionless file entities that took their
	// span from a surviving stem-named declaration in the same file, because
	// the class-like component that would normally have donated it was already
	// consumed by foldClassHierarchyShadows (#6118).
	SpansDonated int
}

// filePathStem returns the base filename without extension, lower-cased.
// E.g. "src/components/LoginPage.tsx" → "loginpage".
func filePathStem(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := base[:len(base)-len(ext)]
	return strings.ToLower(stem)
}

// componentFileExtensions are the source-file extensions that NAME the
// convention on their own: the ecosystem treats a module file and the
// declaration it exports as the same thing, and the extension exists for
// nothing else. The module system addresses the file, importers name the file,
// and the framework resolves the component through its path. `LoginPage` in
// `LoginPage.tsx` is a second name for that module, so a File+Component pair
// there is a true duplicate and folding it removes a duplicate. Issue #1727.
//
// EVERY OTHER ECOSYSTEM TREATS A FILE AS A CONTAINER, even when it holds
// exactly one declaration. `interface IERC4626` in `IERC4626.sol` is a
// declaration in its own right — it has a signature, an inheritance clause and
// its own importable name — and the file is where it happens to live. Folding
// there does not remove a duplicate, it deletes the declaration: on
// @openzeppelin/contracts@5.2.0 the name==stem rule dropped 42 of 61
// interfaces while leaving all 71 contracts, purely because "contract" is not
// in classLikeComponentSubtypes and "interface" is. Issue #6138.
var componentFileExtensions = map[string]bool{
	".jsx": true, ".tsx": true,
	".vue": true, ".svelte": true, ".astro": true,
}

// moduleFileExtensions are the extensions that carry the component convention
// AND every other kind of module. `.ts` is the extension for
// `src/components/LoginPage.ts` and for `src/domain/IUserRepository.ts` alike,
// so membership says nothing about whether the file IS its declaration and
// allow-listing it wholesale left #6138 live inside TypeScript and JavaScript:
// `class UserRepository` in `UserRepository.ts` was deleted exactly like a
// React component. Issue #6202.
//
// These need a per-file signal instead — see fileIsItsDeclaration.
var moduleFileExtensions = map[string]bool{
	".js": true, ".mjs": true, ".cjs": true,
	".ts": true, ".mts": true, ".cts": true,
}

// fileIsItsDeclaration reports whether the module in sourceFile exists to
// export the single declaration declName, which is what makes the File +
// Component pair a duplicate rather than a container and its contents.
//
// For componentFileExtensions the extension answers it. For
// moduleFileExtensions the answer is the module's DEFAULT EXPORT — the actual
// mechanic behind the React/Vue convention, where a component module is
// `export default LoginPage` while a backend module is `export class
// UserRepository` / `export interface IUserRepository`, a named export
// addressed by its own name. defaultExport is the file entity's
// jsextract.DefaultExportProp, empty when the module has no default export
// naming one of its own declarations.
//
// The absence of the signal denies the fold, so an unresolvable default export
// (`export default connect(...)(LoginPage)`) keeps the declaration's entity.
// That is the direction that loses nothing: an un-folded pair is a duplicate
// the MCP layer can still see, a folded one is a declaration nobody can
// (internal/mcp/denoise.go classifies a subtype="file" component as a
// noiseContainer unconditionally). Issue #6202.
//
// THE NAME COMPARISON IS CASE-SENSITIVE, AND MUST STAY THAT WAY. These are two
// identifiers read out of the SAME source file, where case is the language's
// identity rule and `logger` and `Logger` are two different bindings. The
// singleton-instance convention is ubiquitous:
//
//	export class Logger { … }
//	const logger = new Logger();
//	export default logger;
//
// Under a case-insensitive comparison that module reads as "default-exports its
// class" and `class Logger` is deleted — the exact deletion this change exists
// to stop, on the shape #6202's second row names. The default export there is
// the INSTANCE; the module is a container holding both.
//
// The separate stem-vs-name comparison at the fold decision stays
// case-insensitive, because a filename is not an identifier: `loginPage.ts`
// holding `class LoginPage` is one declaration under two spellings of one name,
// so that fold is still correct and nothing legitimate is lost here.
func fileIsItsDeclaration(sourceFile, declName, defaultExport string) bool {
	ext := strings.ToLower(filepath.Ext(sourceFile))
	if componentFileExtensions[ext] {
		return true
	}
	if !moduleFileExtensions[ext] {
		return false
	}
	return defaultExport != "" && defaultExport == declName
}

// foldFileComponentDuplicates collapses SCOPE.Component class-like nodes into
// their co-located SCOPE.Component(subtype="file") sibling when the class
// entity's name matches the file stem and there is no other distinguishing
// structural reason to keep them separate.
//
// This targets the "File + Component" duplicate-kind pattern reported by iter4
// calibration (Issue #1727): frontend repos with one-class-per-file conventions
// (React/Vue/Svelte components, Angular services, etc.) accumulate 297+ pairs
// where a file-level entity and a same-named class entity share source_file.
//
// Fold rules:
//  1. The survivor must be a SCOPE.Component with subtype="file" (emitted by
//     extractor.FileEntity) carrying Name == SourceFile.
//  2. The fold source must be a SCOPE.Component with a class-like subtype (see
//     classLikeComponentSubtypes) or role="class" property, whose Name
//     case-insensitively matches the file stem of the survivor's SourceFile,
//     AND whose SourceFile matches the survivor's SourceFile.
//  3. Anti-over-fold guard: if the class entity's name does NOT match the file
//     stem — meaning it's a *different* class inside the same file — keep both.
//  4. Convention guard: the file must be one that exists to export this one
//     declaration (fileIsItsDeclaration) — either because its extension names
//     the component convention (#6138) or because the module default-exports
//     the declaration by name (#6202). Elsewhere a file is a container and the
//     declaration is not a duplicate of it, so folding would delete a real
//     declaration.
//
// Runs AFTER foldClassHierarchyShadows (so shadows are already resolved) and
// AFTER stampEntityIDs (so r.ID is populated).
func (i *Indexer) foldFileComponentDuplicates(
	merged []types.EntityRecord,
	pass2Rels []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord, foldFileComponentStats) {
	var stats foldFileComponentStats

	// Index all SCOPE.Component(subtype="file") survivors by SourceFile.
	// These are the canonical per-file module entities emitted by FileEntity().
	type fileEnt struct {
		idx  int
		id   string
		stem string // lower-cased filename without extension
		// defaultExport is the name this module default-exports, stamped by
		// the JS/TS extractor (jsextract.DefaultExportProp). Read here rather
		// than at the fold decision so it is the extractor's answer and never
		// a property this pass copied onto the survivor from an earlier fold.
		// Issue #6202.
		defaultExport string
	}
	fileBySourceFile := make(map[string]fileEnt)
	for k := range merged {
		r := &merged[k]
		if r.Kind == "SCOPE.Component" && r.Subtype == "file" && r.ID != "" {
			fileBySourceFile[r.SourceFile] = fileEnt{
				idx:           k,
				id:            r.ID,
				stem:          filePathStem(r.SourceFile),
				defaultExport: r.Properties[jsextract.DefaultExportProp],
			}
		}
	}

	if len(fileBySourceFile) == 0 {
		return merged, pass2Rels, stats
	}

	// remap: folded class entity ID -> file entity ID.
	remap := make(map[string]string)
	drop := make(map[int]bool)

	for k := range merged {
		r := &merged[k]
		// Must be a SCOPE.Component with a class-like subtype or role="class".
		if r.Kind != "SCOPE.Component" || r.Subtype == "file" {
			continue
		}
		if !classLikeComponentSubtypes[r.Subtype] && r.Properties["role"] != "class" {
			continue
		}
		if r.Name == "" || r.ID == "" {
			continue
		}
		fe, ok := fileBySourceFile[r.SourceFile]
		if !ok {
			continue
		}
		// Count this as an observed candidate for folding.
		stats.Observed++
		// Anti-over-fold: only absorb if the class entity's name matches
		// the file stem (case-insensitive). This ensures a class named
		// "LoginPage" in "LoginPage.tsx" is folded, but a helper class
		// "FormValidator" in the same file is NOT absorbed.
		if strings.ToLower(r.Name) != fe.stem {
			continue
		}
		// Only fold where the file IS the declaration; elsewhere the file is a
		// container and folding deletes a real declaration. Issues #6138, #6202.
		if !fileIsItsDeclaration(r.SourceFile, r.Name, fe.defaultExport) {
			continue
		}
		if fe.id == r.ID {
			// Degenerate: same ID — already the same entity.
			continue
		}
		drop[k] = true
		remap[r.ID] = fe.id
		stats.Folded++

		// Copy useful properties from the class entity to the file entity
		// (e.g. start_line, qualified_name, language) when the file entity
		// lacks them.
		sv := &merged[fe.idx]
		if sv.Properties == nil {
			sv.Properties = map[string]string{}
		}
		for pk, pv := range r.Properties {
			if pk == "provenance" || pk == "ref" || pk == "kind" || pk == "subtype" {
				continue
			}
			if _, exists := sv.Properties[pk]; !exists {
				sv.Properties[pk] = pv
			}
		}
		// Promote real line numbers to the file entity if it has none.
		if sv.StartLine == 0 && r.StartLine > 0 {
			sv.StartLine = r.StartLine
		}
		if sv.EndLine == 0 && r.EndLine > 0 {
			sv.EndLine = r.EndLine
		}
		if sv.QualifiedName == "" && r.QualifiedName != "" {
			sv.QualifiedName = r.QualifiedName
		}
		// Without this the declaration text — `class LoginPage extends
		// React.Component<Props>` and its heritage clause — is unrecoverable
		// once the source record is dropped. Issue #6138.
		if sv.Signature == "" && r.Signature != "" {
			sv.Signature = r.Signature
		}
		// Re-home edges the class entity owns onto the file entity.
		for ri := range r.Relationships {
			rel := r.Relationships[ri]
			if rel.FromID == "" || rel.FromID == r.ID {
				rel.FromID = fe.id
			}
			sv.Relationships = append(sv.Relationships, rel)
		}
		r.Relationships = nil
	}

	// --- Span donation from a surviving stem-named declaration (#6118) ------
	//
	// THE DEFECT THIS CLOSES. The loop above is the ONLY thing that ever gives
	// a SCOPE.Component(subtype="file") entity a position: FileEntity() emits
	// it with no span, and the span is promoted from the class-like
	// SCOPE.Component that shares its file and matches its stem. But this pass
	// runs AFTER foldClassHierarchyShadows, which drops that very class
	// component whenever a framework-typed node exists for the same symbol.
	// When it does, the file entity is left at line 0 — an entity that had a
	// correct position in one configuration loses it in another.
	//
	// That is exactly the shape #6118 measured on the corpus: 187 C# file-level
	// components regressing from a real start line to 0 with
	// GRAFEL_INPROC_CUSTOM_EXTRACTORS on. The gate does not create the defect,
	// it only makes it fire more often — turning on the C# custom extractors
	// emits framework-typed nodes for DbContexts, MassTransit consumers, MediatR
	// handlers, Quartz jobs and so on, so more class components become shadow
	// fold sources and disappear before this pass can read them. The same loss
	// is reachable on the default path wherever a framework-typed node already
	// exists (an ASP.NET controller, a Spring @Service, a Django CBV).
	//
	// The fix is to stop depending on WHICH node survived. A file entity with
	// no position takes its span from any SURVIVING entity in the same file
	// whose name matches the file stem — the class-like component when it is
	// still there, the framework-typed survivor when it is not. This donates a
	// span only; it folds nothing, drops nothing and re-keys nothing, so it
	// cannot interact with the remap/drop bookkeeping below and deliberately
	// sits ahead of the `len(remap) == 0` early return.
	//
	// THE DONOR SET IS DELIBERATELY NARROW. It is exactly "a class-like
	// declaration": a framework-typed class kind (frameworkClassKindPriority —
	// the survivor set foldClassHierarchyShadows folds INTO) or a class-like
	// SCOPE.Component (the fold-source set it folds AWAY). Anything looser is
	// wrong, not merely over-eager: a Go file caller2.go declaring
	// `func Caller2()` has a SCOPE.Operation whose name matches the stem, and
	// letting a function donate its line to the file entity says "this file
	// starts where that function starts", which is false. It also breaks
	// incremental/full-rebuild equivalence, because the incremental path does
	// not run this fold at all — caught by TestDiffReindex_NewCrossFileCall and
	// its four siblings.
	isClassLikeDonor := func(r *types.EntityRecord) bool {
		if _, ok := frameworkClassKindPriority[r.Kind]; ok {
			return true
		}
		return r.Kind == "SCOPE.Component" && r.Subtype != "file" &&
			(classLikeComponentSubtypes[r.Subtype] || r.Properties["role"] == "class")
	}

	// Deterministic donor choice: smallest real start line, then largest end
	// line, then smallest ID.
	for _, fe := range fileBySourceFile {
		sv := &merged[fe.idx]
		if sv.StartLine > 0 || drop[fe.idx] {
			continue
		}
		donor := -1
		for k := range merged {
			if k == fe.idx || drop[k] {
				continue
			}
			r := &merged[k]
			if r.StartLine <= 0 || r.SourceFile != sv.SourceFile {
				continue
			}
			if r.Name == "" || strings.ToLower(r.Name) != fe.stem {
				continue
			}
			if !isClassLikeDonor(r) {
				continue
			}
			if donor < 0 {
				donor = k
				continue
			}
			d := &merged[donor]
			switch {
			case r.StartLine != d.StartLine:
				if r.StartLine < d.StartLine {
					donor = k
				}
			case r.EndLine != d.EndLine:
				if r.EndLine > d.EndLine {
					donor = k
				}
			default:
				if r.ID < d.ID {
					donor = k
				}
			}
		}
		if donor < 0 {
			continue
		}
		d := &merged[donor]
		sv.StartLine = d.StartLine
		if sv.EndLine == 0 && d.EndLine > 0 {
			sv.EndLine = d.EndLine
		}
		stats.SpansDonated++
	}

	if len(remap) == 0 {
		return merged, pass2Rels, stats
	}

	// Re-point every edge endpoint that targets a folded class entity.
	rewrite := func(id string) (string, bool) {
		if nv, ok := remap[id]; ok {
			return nv, true
		}
		return id, false
	}
	for k := range merged {
		if drop[k] {
			continue
		}
		r := &merged[k]
		for ri := range r.Relationships {
			rel := &r.Relationships[ri]
			if nv, ok := rewrite(rel.FromID); ok {
				rel.FromID = nv
				stats.EdgesRepointed++
			}
			if nv, ok := rewrite(rel.ToID); ok {
				rel.ToID = nv
				stats.EdgesRepointed++
			}
		}
	}
	for ri := range pass2Rels {
		rel := &pass2Rels[ri]
		if nv, ok := rewrite(rel.FromID); ok {
			rel.FromID = nv
			stats.EdgesRepointed++
		}
		if nv, ok := rewrite(rel.ToID); ok {
			rel.ToID = nv
			stats.EdgesRepointed++
		}
	}

	// Compact: drop the folded class entity records.
	out := merged[:0]
	for k := range merged {
		if drop[k] {
			continue
		}
		out = append(out, merged[k])
	}
	return out, pass2Rels, stats
}

// useModuleResolveIndex reports whether the M5 per-module resolver index
// (resolve.BuildIndexFromModulesOrdered) should be used in place of the flat
// resolve.BuildIndex. Controlled by GRAFEL_RESOLVE_MODULE_INDEX; DEFAULT
// OFF (#4331). Truthy values: "1", "true", "yes", "on" (case-insensitive).
func useModuleResolveIndex() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GRAFEL_RESOLVE_MODULE_INDEX"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// mergePassRecords concatenates the per-pass entity records into one slice, in
// canonical pass order (pass1, pass2, pass3), and RELEASES each caller-owned
// slice header as soon as its records have been copied.
//
// #5985 — the copy into the merged slice is SHALLOW: every record's Properties
// / Metadata / Tags / embedded Relationships live on unchanged in the merged
// copy. Dropping the caller's header therefore frees only the duplicate spine
// (the per-pass backing arrays — ~165 MB on a large corpus, where `merged`
// itself is ~183 MB), never any entity payload. Before this, both spines stayed
// live for the whole of buildDocument because the caller did not release the
// pass slices until well after buildDocument returned.
//
// A nil pointer means "this pass produced nothing" (unit tests and the daemon's
// single-stream path both rely on that).
func mergePassRecords(pass1, pass2, pass3 *[]types.EntityRecord) []types.EntityRecord {
	n := lenRecords(pass1) + lenRecords(pass2) + lenRecords(pass3)
	merged := make([]types.EntityRecord, 0, n)
	merged = appendAndRelease(merged, pass1)
	merged = appendAndRelease(merged, pass2)
	merged = appendAndRelease(merged, pass3)

	return merged
}

func lenRecords(p *[]types.EntityRecord) int {
	if p == nil {
		return 0
	}
	return len(*p)
}

// appendAndRelease appends *src to dst and then drops the caller's reference to
// src's backing array. See mergePassRecords for why this is safe.
func appendAndRelease(dst []types.EntityRecord, src *[]types.EntityRecord) []types.EntityRecord {
	if src == nil {
		return dst
	}
	dst = append(dst, *src...)
	*src = nil
	return dst
}

// generatedFileSet returns this run's generated-source resolver, creating an
// inert one when buildDocument is driven directly by a unit test that never
// went through Run. An empty root leaves the filename rules working and
// disables the content read, which degrades rather than lying.
func (i *Indexer) generatedFileSet() *generated.Set {
	if i.generatedSet == nil {
		i.generatedSet = generated.NewSet("")
	}
	return i.generatedSet
}

// buildDocument merges entity records from every pass, dedupes by stable
// graph-entity ID, resolves cross-file CALLS edges, then assembles the
// final on-disk document.
func (i *Indexer) buildDocument(pass1, pass2 *[]types.EntityRecord, pass2Rels []types.RelationshipRecord, pass3 *[]types.EntityRecord) *graph.Document {
	merged := mergePassRecords(pass1, pass2, pass3)

	// Issue #481 — the merged slice drives BuildIndex's first-writer-wins
	// disambiguation. Sort by canonical fields so identically-named entities
	// resolve to the same winner across runs of the SAME corpus.
	sortEntityRecords(merged)

	// Issue #534 Phase 2 — resolve synthetic http_endpoint handler
	// references emitted by applyHTTPEndpointSynthesis. Runs BEFORE
	// stampEntityIDs so the appended IMPLEMENTS edges use Kind:Name stubs
	// that the resolver pass (below) rewrites against the merged entity
	// index in the same step it handles every other stub. Unresolved
	// synthetics are dropped here — keeping them would leave orphan
	// http_endpoint nodes in the graph and inflate bug-rate.
	// #6450 — fold intra-repo base-URL constants into consumer-side
	// http_endpoint_call paths BEFORE the call→definition matcher below
	// runs. The matcher joins on the `path` property; until this ran at
	// index time the fold happened only in `grafel group-link`, a different
	// process phase, so a monorepo's `fetch(`${BASE}/things`)` was written
	// to graph.json as UNRESOLVED_FETCH even with the handler right there.
	// Rewrites `path` only — Name and EntityID stay frozen, so the match
	// lands at the matcher's path tier with no entity-ID churn.
	// carriedForward is passed so an INCREMENTAL run can still see the
	// declaring module: `merged` holds only the re-extracted files, and
	// without the prior-graph entities an unrelated edit to the caller file
	// silently un-folds every path a previous full index resolved (#6450
	// review, blocking 1).
	if foldStats := engine.FoldConsumerHTTPBaseURLs(merged, i.absRepo, i.incrementalCarryForwardEntities); foldStats.Candidates > 0 {
		fmt.Fprintf(os.Stderr,
			"http-endpoint-baseurl-fold: candidates=%d folded=%d files_sniffed=%d files_indexed=%d read_cap_hit=%t\n",
			foldStats.Candidates, foldStats.Folded, foldStats.FilesSniffed,
			foldStats.FilesIndexed, foldStats.ReadCapHit)
	}

	var httpEndpointStats engine.ResolveHTTPEndpointStats
	merged, httpEndpointStats = engine.ResolveHTTPEndpointHandlersWithRepo(merged, i.repoTag)
	if httpEndpointStats.DTOHandlerEdgesEmitted > 0 || httpEndpointStats.DTOHandlerEdgesUnresolved > 0 {
		// #1999 — log the DTO↔Handler bidirectional edge counters
		// independently of the main http-endpoint stats line so the
		// number is easy to grep when grinding the unresolved bucket.
		fmt.Fprintf(os.Stderr,
			"http-endpoint-dto-bidirectional: emitted=%d unresolved=%d (REFERENCES edges from DTO → handler for request_body_type/response_body_type properties)\n",
			httpEndpointStats.DTOHandlerEdgesEmitted,
			httpEndpointStats.DTOHandlerEdgesUnresolved)
	}
	if httpEndpointStats.Synthetics > 0 {
		fmt.Fprintf(os.Stderr,
			"http-endpoint-resolve: synthetics=%d handler_resolved=%d handler_dropped=%d no_handler_prop=%d caller_resolved=%d caller_unresolved=%d calls_linked=%d calls_unresolved=%d caller_edges_retargeted=%d\n",
			httpEndpointStats.Synthetics,
			httpEndpointStats.HandlerResolved,
			httpEndpointStats.HandlerDropped,
			httpEndpointStats.NoHandlerProp,
			httpEndpointStats.CallerResolved,
			httpEndpointStats.CallerUnresolved,
			httpEndpointStats.CallsLinked,
			httpEndpointStats.CallsUnresolved,
			httpEndpointStats.CallerEdgesRetargeted)
	}
	// #1217 migration hints: log how many legacy http_endpoint entities were
	// rewritten to the split kinds. These lines appear only when a graph
	// pre-dates #1217 (i.e. still has the old kind string on disk).
	if httpEndpointStats.DefinitionsMigrated > 0 || httpEndpointStats.CallsMigrated > 0 {
		fmt.Fprintf(os.Stderr,
			"http-endpoint-split: %d entities migrated from http_endpoint to http_endpoint_definition, %d to http_endpoint_call\n",
			httpEndpointStats.DefinitionsMigrated,
			httpEndpointStats.CallsMigrated)
	}

	// Stamp deterministic entity IDs onto every record so the resolver can
	// look them up by (kind, name).
	i.stampEntityIDs(merged)

	// Issue #SCOPE-PATTERN-CONTAINS — every SCOPE.Pattern entity must be
	// connected to the file it came from via a CONTAINS edge. The framework
	// rule engine and many per-language pattern detectors emit Pattern
	// entities without one, which leaves them orphaned in the graph and
	// inflates the per-repo orphan rate (the #1 universal orphan source
	// across 23/38 corpora — ~45,000 entities corpus-wide).
	//
	// Fixup runs after stampEntityIDs so we can compute the deterministic
	// IDs for both endpoints directly (no resolver round-trip needed).
	// The file endpoint is the per-source-file SCOPE.Component (subtype="file")
	// emitted by extractor.FileEntity (#577) — its ID is
	//   graph.EntityID(repoTag, "SCOPE.Component", path, path).
	// Pattern entities whose source file has no file entity (rare —
	// non-extracted assets) leave a residual orphan; the FromID still
	// points at the canonical SCOPE.Component(file) ID so a future fix
	// that adds the missing file entity heals these automatically.
	//
	// IMPORTANT: the emitted edges are appended to the output `relationships`
	// slice AFTER the resolver+disposition pass below (see appendPatternContainsRels
	// below the standalone-rel loop). Routing them through the resolver would
	// add N already-resolved hex endpoints to the disposition totals and
	// shift bug-rate (bugs/total). Keeping them out of classification preserves
	// byte-identical bug-rate vs main while still producing the CONTAINS edges.
	patternContainsRels := i.buildPatternContainsRels(merged)
	if len(patternContainsRels) > 0 {
		fmt.Fprintf(os.Stderr,
			"scope-pattern-contains: emitted %d CONTAINS edges (file → SCOPE.Pattern)\n",
			len(patternContainsRels))
	}

	// #4244 — node-anchored $lookup/$graphLookup JOINS_COLLECTION twins. Emitted
	// here, AFTER stampEntityIDs, so each twin's FromID is the stage entity's
	// REAL graph id (r.ID) rather than a structural-ref stub. Appended alongside
	// patternContainsRels below (post-disposition, both endpoints are resolved
	// hex ids so they bypass the resolver/classifier).
	mongoAggStageJoinRels := i.buildMongoAggStageJoinRels(merged)
	if len(mongoAggStageJoinRels) > 0 {
		fmt.Fprintf(os.Stderr,
			"mongo-agg-stage-joins: emitted %d node-anchored JOINS_COLLECTION twins ($lookup node → Class:<from>)\n",
			len(mongoAggStageJoinRels))
	}

	// Resolver pass — rewrite stub-form FromID/ToID values across:
	//   - embedded EntityRecord.Relationships (Pass 1 + Pass 2.5 + Pass 3)
	//   - standalone Pass 2.5 RelationshipRecords (engine output)
	// against the merged entity index. Stubs that are ambiguous (≥2 matches)
	// or unmatched are left in place and counted in the log line below.
	// Issue #93 — import-aware cross-file resolution. Builds a per-file
	// import table from IMPORTS edges and rewrites bare-name CALLS targets
	// to the entity they actually point at. Runs BEFORE BuildIndex so the
	// disposition classifier sees the rewritten ID as already-resolved.
	importTbl := resolve.BuildImportTable(merged)
	importStats := resolve.ResolveImports(merged, importTbl)

	// Go in-tree import resolution: rewrites IMPORTS edges whose ToID is a
	// project-internal Go package path (e.g. "github.com/owner/repo/internal/pkg")
	// to the hex entity ID of a representative file in the imported package
	// directory. Requires Properties["go_pkg_dir"] stamped by the Go extractor
	// when go.mod is present. Runs BEFORE BuildIndex so the disposition
	// classifier sees the rewritten ID as already-resolved.
	goInTreeRewrites := resolve.ResolveGoInTreeImports(merged)
	if goInTreeRewrites > 0 {
		fmt.Fprintf(os.Stderr, "resolver: go-in-tree rewrote=%d IMPORTS targets\n", goInTreeRewrites)
	}

	// Always emit the stats line when the pass had work to do, so a
	// silent-failure regression (considered>0 but rewritten=0 — e.g. the
	// import table failed to build) surfaces in stderr instead of
	// disappearing.
	if importStats.CallsConsidered > 0 {
		note := ""
		if importStats.CallsRewritten == 0 {
			note = " (no candidates resolved — check IMPORTS edges)"
		}
		fmt.Fprintf(os.Stderr, "resolver: import-aware rewrote=%d/%d bare-name CALLS targets%s\n",
			importStats.CallsRewritten, importStats.CallsConsidered, note)
	}
	// Issue #142 — IMPORTS edges with dotted-path ToIDs
	// (`conduit.database.db`) are rewritten via the same per-module
	// reverse index. Surfaced separately so the verify2 harness can
	// attribute the bug-resolver delta on python-flask-realworld.
	if importStats.ImportsConsidered > 0 {
		fmt.Fprintf(os.Stderr, "resolver: import-aware rewrote=%d/%d dotted IMPORTS targets\n",
			importStats.ImportsRewritten, importStats.ImportsConsidered)
	}
	// Issue #422 — PHP FQN-method CALLS targets
	// (`App\Controller\BlogController::list`) emitted by the Symfony
	// YAML cross-extractor. Surfaced separately so the verify2 harness
	// can attribute the bug-resolver delta on php-symfony-* corpora.
	if importStats.PHPFQNMethodConsidered > 0 {
		fmt.Fprintf(os.Stderr, "resolver: import-aware rewrote=%d/%d PHP FQN-method CALLS targets\n",
			importStats.PHPFQNMethodRewritten, importStats.PHPFQNMethodConsidered)
	}
	// Chain-fix: python-references-cross-file. Cross-file REFERENCES
	// targets that the same-file structural-ref pass cannot bind because
	// the entity lives in another file or in an external package. Mirror
	// of the CALLS path above. Surfaced separately so the verify2 harness
	// can attribute the orphan-rate delta on python-* corpora (especially
	// django-realworld and client-fixture-a, where #650's residual is
	// dominated by cross-file references).
	if importStats.ReferencesConsidered > 0 {
		fmt.Fprintf(os.Stderr, "resolver: import-aware rewrote=%d/%d cross-file REFERENCES targets\n",
			importStats.ReferencesRewritten, importStats.ReferencesConsidered)
	}

	// Incremental resolver-scope augmentation (deploy-9 REFUTED item-2):
	// seed the resolver index with the carried-forward (unchanged-file) prev
	// entities so cross-file bare-name edge endpoints emitted by the
	// re-extracted changed files — notably the constructor-DI INJECTED_INTO
	// provider name attached to a controller — resolve to their stable
	// provider IDs. The carry-forward entities are used for index lookups
	// ONLY; they are NOT appended to `merged` (mergeIncrementalPrevDoc
	// re-adds the unchanged-file entities to the persisted document, so
	// adding them here too would double-emit). IDs are stable across runs,
	// so an edge rewritten to a carried-forward entity's ID resolves to a
	// node that survives into the final graph.
	indexEntities := merged
	if len(i.incrementalCarryForwardEntities) > 0 {
		indexEntities = make([]types.EntityRecord, 0, len(merged)+len(i.incrementalCarryForwardEntities))
		indexEntities = append(indexEntities, merged...)
		indexEntities = append(indexEntities, i.incrementalCarryForwardEntities...)
	}
	// #4331 — resolver index construction. The production default is the
	// flat O(N×M) resolve.BuildIndex. The M5 per-module path
	// (resolve.BuildIndexFromModulesOrdered, #2182/#2184) is a scale-only
	// speed optimisation for large monorepos: it partitions entities by
	// package directory and pre-sizes the global maps in one shot. It is
	// edge-set-identical to BuildIndex (the original sort-by-ID divergence on
	// the platform-variant merge #1818, and the missing namespace/kotlin index
	// tables, are both fixed in the ordered path — see
	// internal/resolve/symbol_index.go and the full-Index parity harness in
	// symbol_index_parity_test.go).
	//
	// It is wired BEHIND A DEFAULT-OFF FLAG: GRAFEL_RESOLVE_MODULE_INDEX=1
	// routes through the M5 path; unset/anything-else keeps BuildIndex, so the
	// default is a zero-behaviour-change no-op. Flipping the default to ON
	// requires a live-graph edge-parity diff (reindex a real repo both ways and
	// diff edges) in a deploy window — tracked as follow-up #4901.
	var idx resolve.Index
	if useModuleResolveIndex() {
		fmt.Fprintln(os.Stderr, "resolver: using M5 per-module index (GRAFEL_RESOLVE_MODULE_INDEX=1)")
		idx = resolve.BuildIndexFromModulesOrdered(indexEntities, resolve.ModuleKeyByPkgDir)
	} else {
		idx = resolve.BuildIndex(indexEntities)
	}
	// #2049 — Django string-FK late-binding: rewrite scope:component:ref:python:*
	// stubs on REFERENCES edges with django_rel set, using app-label-aware
	// byPackageComponent lookup. Runs after BuildIndex (needs the component
	// index) and before ReferencesEmbeddedWithAllowlist (so rewritten hex IDs
	// are seen as resolved and counted correctly).
	djangoFKRewrites := idx.ResolveDjangoStringFKRefs(merged)
	if djangoFKRewrites > 0 {
		fmt.Fprintf(os.Stderr, "resolver: django-string-fk rewrote=%d FK REFERENCES stubs\n", djangoFKRewrites)
	}
	// #4379 — Django settings global cross-cutting wiring late-binding. Rewrite
	// the dotted-path USES edges emitted from the synthetic django_settings
	// entity (MIDDLEWARE / AUTHENTICATION_BACKENDS / REST_FRAMEWORK
	// DEFAULT_*_CLASSES) to the real in-repo middleware/auth/permission/renderer
	// class IDs, by QualifiedName and unique leaf-name fallback (recovers the
	// merge-dropped-QualifiedName case). Same ordering as the FK pass: after
	// BuildIndex, before ReferencesEmbeddedWithAllowlist.
	djangoWiringRewrites := idx.ResolveDjangoGlobalWiringRefs(merged)
	if djangoWiringRewrites > 0 {
		fmt.Fprintf(os.Stderr, "resolver: django-global-wiring rewrote=%d settings USES edges\n", djangoWiringRewrites)
	}
	// #4332 — Go cross-package CALLS resolution. Binds `pkg.Func()` edges
	// stamped with go_call_pkg_dir + call_leaf to byPackageOperation[pkgDir][leaf].
	// Runs after BuildIndex (needs the package-operation index) and before
	// ReferencesEmbeddedWithAllowlist so the rewritten hex IDs count as resolved.
	goCrossPkgCallRewrites := idx.ResolveGoCrossPackageCalls(merged)
	if goCrossPkgCallRewrites > 0 {
		fmt.Fprintf(os.Stderr, "resolver: go-cross-package rewrote=%d CALLS targets\n", goCrossPkgCallRewrites)
	}
	// #4373 — Rust cross-module/cross-crate CALLS resolution. Binds
	// `crate::mod::Func()` / `self::`/`super::` / aliased / `Type::method`
	// edges stamped with rust_call_pkg_dirs + call_leaf (+ rust_call_scope)
	// to byPackageOperation / byPackageMember. Same ordering constraints as
	// the Go pass: after BuildIndex, before the embedded-reference resolver.
	rustCrossModCallRewrites := idx.ResolveRustCrossModuleCalls(merged)
	if rustCrossModCallRewrites > 0 {
		fmt.Fprintf(os.Stderr, "resolver: rust-cross-module rewrote=%d CALLS targets\n", rustCrossModCallRewrites)
	}
	// #4374 — C# cross-namespace CALLS resolution. Binds qualified
	// `Ns.Type.method()` / aliased / `using static` / `global::` edges stamped
	// with csharp_call_ns + csharp_call_type + call_leaf to a namespace-keyed
	// member index (C# namespaces are not directory-bound). Same ordering
	// constraints as the Go/Rust passes: after BuildIndex, before the
	// embedded-reference resolver.
	csharpCrossNSCallRewrites := idx.ResolveCSharpCrossNamespaceCalls(merged)
	if csharpCrossNSCallRewrites > 0 {
		fmt.Fprintf(os.Stderr, "resolver: csharp-cross-namespace rewrote=%d CALLS targets\n", csharpCrossNSCallRewrites)
	}
	// #4375 — Kotlin cross-package CALLS resolution. Binds qualified
	// `pkg.Type.method()` / imported-top-level-fn / imported-or-aliased
	// `Type.method()` / same-package companion-member edges stamped with
	// kotlin_call_pkg + kotlin_call_type + call_leaf to a package-keyed
	// member/operation index (Kotlin packages are not directory-bound). Same
	// ordering constraints as the Go/Rust/C# passes: after BuildIndex, before
	// the embedded-reference resolver.
	kotlinCrossPkgCallRewrites := idx.ResolveKotlinCrossPackageCalls(merged)
	if kotlinCrossPkgCallRewrites > 0 {
		fmt.Fprintf(os.Stderr, "resolver: kotlin-cross-package rewrote=%d CALLS targets\n", kotlinCrossPkgCallRewrites)
	}
	// #5782 (ADR-0025) — ChannelBinding late-binding. Rewrite the config-side
	// BINDS / BINDS_TOPIC structural refs emitted by discoverChannelBindings
	// to real entity IDs: BINDS joins by (channel, direction) against
	// @Incoming/@Outgoing reactive-messaging Operations; BINDS_TOPIC joins by
	// the "kafka:<topic>" MessageTopic Name. Runs after BuildIndex (needs the
	// full entity set) and before ReferencesWithAllowlist so the rewritten hex
	// IDs count as resolved; unmatched refs stay unresolved and feed the
	// topology orphan scan.
	channelBindingRewrites := resolve.ResolveChannelBindings(indexEntities, pass2Rels)
	if channelBindingRewrites > 0 {
		fmt.Fprintf(os.Stderr, "resolver: channel-binding rewrote=%d BINDS/BINDS_TOPIC refs\n", channelBindingRewrites)
	}
	allow := resolve.ExternalAllowlist(external.IsKnownExternalPackage)
	embStats := resolve.ReferencesEmbeddedWithAllowlist(merged, idx, allow)
	standStats := resolve.ReferencesWithAllowlist(pass2Rels, idx, allow)
	// RC-A safety net: drop any PUBLISHES_TO edge whose producer-side FromID
	// failed to resolve to a real entity above, rather than letting a
	// dangling/stub source reach the final document (#5751-adjacent event-
	// identity precision fix).
	pass2Rels = resolve.DropUnresolvedPublishesTo(pass2Rels)
	totalStats := resolve.Stats{
		Rewritten:     embStats.Rewritten + standStats.Rewritten,
		Ambiguous:     embStats.Ambiguous + standStats.Ambiguous,
		Unmatched:     embStats.Unmatched + standStats.Unmatched,
		FromRewritten: embStats.FromRewritten + standStats.FromRewritten,
		FromAmbiguous: embStats.FromAmbiguous + standStats.FromAmbiguous,
		FromUnmatched: embStats.FromUnmatched + standStats.FromUnmatched,
		ToRewritten:   embStats.ToRewritten + standStats.ToRewritten,
		ToAmbiguous:   embStats.ToAmbiguous + standStats.ToAmbiguous,
		ToUnmatched:   embStats.ToUnmatched + standStats.ToUnmatched,
	}
	resolve.MergeDispositions(&totalStats, &embStats)
	resolve.MergeDispositions(&totalStats, &standStats)
	// Stash the resolver index + pre-synthesis dispositions on the indexer
	// so the post-synthesis classification step (after external.Synthesize)
	// can reclassify "ext:*" endpoints with the allowlist.
	i.resolveIdx = &idx
	i.resolveStats = totalStats
	fmt.Fprintf(os.Stderr, "resolver: rewrote=%d ambiguous=%d unmatched=%d (from: rw=%d am=%d um=%d) (to: rw=%d am=%d um=%d)\n",
		totalStats.Rewritten, totalStats.Ambiguous, totalStats.Unmatched,
		totalStats.FromRewritten, totalStats.FromAmbiguous, totalStats.FromUnmatched,
		totalStats.ToRewritten, totalStats.ToAmbiguous, totalStats.ToUnmatched)

	// Prune import-placeholder entities (kind=SCOPE.Component
	// subtype="import") emitted by the JS/TS extractor and the
	// cross-language imports extractor. They are pure structural
	// carriers for IMPORTS / DEPENDS_ON edges; after the resolver
	// passes above have rewritten the edge ToID / FromID, the
	// placeholders are orphan-by-construction (root-cause analysis
	// 2026-05-19: 2,583 of fixture-b's 9,390 orphans). The pruner
	// hoists each placeholder's embedded rels onto the file-level
	// SCOPE.Component (subtype="file") entity for the same SourceFile,
	// or returns them as standalone rels when no carrier exists.
	// Cross-repo linker (#566/#570/#578) match targets (file-level
	// entities and qualified ext:<module>:<name>) are untouched.
	// #6131 — the prune's incoming-edge repoint must be able to target an
	// entity that lives in the carried-forward (unchanged-file) portion of an
	// incremental run: `merged` holds only the re-extracted files, but the
	// resolver above already bound this run's IMPORTS edges against
	// `indexEntities`, which includes them. Without this set the repoint would
	// reject those targets as non-surviving and orphan the edge on exactly the
	// path it exists to protect. Empty (nil) on a full rebuild.
	var pruneExtraLive map[string]bool
	if n := len(i.incrementalCarryForwardEntities); n > 0 {
		pruneExtraLive = make(map[string]bool, n)
		for k := range i.incrementalCarryForwardEntities {
			if id := i.incrementalCarryForwardEntities[k].ID; id != "" {
				pruneExtraLive[id] = true
			}
		}
	}
	prunedMerged, pruneOrphanRels, pruneStats := resolve.PruneImportPlaceholdersWithLiveIDs(merged, pruneExtraLive)
	merged = prunedMerged
	if pruneStats.Considered > 0 {
		fmt.Fprintf(os.Stderr,
			"import-placeholder-prune: considered=%d pruned=%d rels_hoisted=%d rels_orphaned=%d kept=%d edge_toid_rewrites=%d ref_repoints=%d module_restores=%d\n",
			pruneStats.Considered, pruneStats.Pruned, pruneStats.RelsHoisted,
			pruneStats.RelsOrphaned, pruneStats.PlaceholderKept, pruneStats.EdgeToIDRewrites,
			pruneStats.PlaceholderRefRepoints, pruneStats.PlaceholderModuleRestores)
	}
	if len(pruneOrphanRels) > 0 {
		// Migrate to the standalone pass2Rels stream so the
		// assembly loop below still surfaces them on the document.
		pass2Rels = append(pass2Rels, pruneOrphanRels...)
	}

	// #1613 — fold class-hierarchy Component shadows into their real typed
	// node. The class-hierarchy pass emits every class as a SCOPE.Component
	// (provenance=INFERRED_FROM_CLASS_HIERARCHY). When a real typed node
	// (View/Model/Controller/struct/…) already exists for the same
	// source_file+symbol, that shadow is a line-less duplicate: drop it and
	// re-point its edges onto the surviving typed node. Shadows with no real
	// typed node keep their (now real, from the extractor) line span +
	// qualified_name and survive as the single node for that class.
	var foldStats foldShadowStats
	// GRAFEL_DISABLE_1613_FOLD is a verification escape hatch (compare
	// folded vs unfolded graphs on the same binary); unset in production.
	if os.Getenv("GRAFEL_DISABLE_1613_FOLD") == "" {
		merged, pass2Rels, foldStats = i.foldClassHierarchyShadows(merged, pass2Rels)
	}
	if foldStats.ShadowsFolded > 0 || foldStats.ShadowsBackfilled > 0 {
		fmt.Fprintf(os.Stderr,
			"class-shadow-fold: folded=%d (edges_repointed=%d) survived_with_real_lines=%d still_line0=%d\n",
			foldStats.ShadowsFolded, foldStats.EdgesRepointed,
			foldStats.ShadowsBackfilled, foldStats.ShadowsStillLine0)
	}

	// #1727 — fold File+Component duplicate-kind pairs: SCOPE.Component class-like
	// nodes whose name matches the file stem of a co-located SCOPE.Component(subtype="file")
	// entity are collapsed into the file entity. This deduplicates the 297+
	// File+Component pairs reported by iter4 calibration on one-class-per-file
	// front-end repos (React/Vue/Svelte components).
	// GRAFEL_DISABLE_1727_FILE_FOLD is the verification escape hatch.
	if os.Getenv("GRAFEL_DISABLE_1727_FILE_FOLD") == "" {
		var fileStats foldFileComponentStats
		merged, pass2Rels, fileStats = i.foldFileComponentDuplicates(merged, pass2Rels)
		fmt.Fprintf(os.Stderr,
			"foldFileComponentDuplicates: observed=%d folded=%d (edges_repointed=%d spans_donated=%d)\n",
			fileStats.Observed, fileStats.Folded, fileStats.EdgesRepointed, fileStats.SpansDonated)
	}

	// Pass 3.7 — Bazel resolver overlay (#2183 / M6).
	// Cross-references BAZEL_DEPENDS_ON (declared BUILD deps) against
	// CALLS/IMPORTS (inferred runtime deps) and annotates each declared
	// edge with "declared+used", "declared_unused", or emits a new
	// "undeclared_used" edge for call crossings that lack a BUILD dep.
	// Runs after all entity-ID rewrites and fold passes so IDs are stable.
	{
		// #5955 — RunBazelOverlay is strictly read-only over its entity input
		// (see internal/resolve/bazel_overlay.go: "The function never mutates the
		// input slices"). It only builds string→string index maps and emits new
		// RelationshipRecords, so we can hand it `merged` directly and skip the
		// former full by-value copy of every EntityRecord (a second live copy of
		// the whole entity set at the subprocess RSS peak).
		bazelOverlay := resolve.RunBazelOverlay(merged, pass2Rels)
		if len(bazelOverlay.AnnotatedRels) > 0 {
			pass2Rels = append(pass2Rels, bazelOverlay.AnnotatedRels...)
			fmt.Fprintf(os.Stderr,
				"bazel-overlay: declared+used=%d declared_unused=%d undeclared_used=%d annotated_edges=%d\n",
				bazelOverlay.Stats.DeclaredUsed,
				bazelOverlay.Stats.DeclaredUnused,
				bazelOverlay.Stats.UndeclaredUsed,
				len(bazelOverlay.AnnotatedRels),
			)
		}
	}

	entities := make([]graph.Entity, 0, len(merged))
	relationships := make([]graph.Relationship, 0)

	seenEntity := make(map[string]bool, len(merged))
	// entityPos maps a survivor's graph ID → its index in `entities` so that a
	// later duplicate (same EntityID) can gap-fill base-only state onto the
	// already-emitted survivor instead of being dropped wholesale (issue #4406).
	entityPos := make(map[string]int, len(merged))
	seenRel := make(map[string]bool)

	for k := range merged {
		r := &merged[k]
		id := graph.EntityID(i.repoTag, r.Kind, r.Name, r.SourceFile)
		if !seenEntity[id] {
			seenEntity[id] = true
			// Issue #1381 — stamp Properties["module"] on every entity at
			// assembly time using the deterministic path-rollup algorithm.
			// EnsureModule is a no-op when the key is already set (extractor
			// overrides are preserved).
			//
			// Issue #1628 — for a PLAIN repo (single-unit mode) force the
			// per-repo label on every sourced entity so the Group-by-Module
			// graph shows one node for the repo instead of fragmenting by
			// top-level directory. Synthetic/sourceless entities are handled
			// by the _external pass below and are left untouched here.
			if i.singleModuleLabel != "" && r.SourceFile != "" {
				if r.Properties == nil {
					r.Properties = map[string]string{}
				}
				r.Properties["module"] = i.singleModuleLabel
			} else {
				r.Properties = module.EnsureModule(r.Properties, r.SourceFile, i.moduleMarkers)
			}
			entities = append(entities, graph.Entity{
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
				Metadata:      r.Metadata,

				Confidence: r.Confidence, // Phase 1C (#2769).
			}.WithProperties(r.Properties))
			entityPos[id] = len(entities) - 1
		} else if pos, ok := entityPos[id]; ok {
			// Issue #4406 — the production dedup-by-ID path. When two
			// EntityRecords collapse to the same graph.EntityID (same
			// kind/name/source-file), the first wins and every later
			// duplicate is dropped. The dropped duplicate is frequently the
			// carrier of base-only state the survivor lacks — most critically
			// the module-qualified QualifiedName that drives byQualifiedName
			// resolution and cross-repo joins (the live-graph half of #4402,
			// the same bug #4405 fixed at the MergeWithCustom boundary).
			//
			// Mirror #4405's supersedeBase gap-fill semantics: carry the
			// duplicate's value onto the survivor ONLY where the survivor left
			// the field empty — never override a value the survivor already
			// provided. The duplicate's edges are unioned by the relationship
			// loop below (it runs for every record, survivor or duplicate, and
			// anchors empty-FromID edges to this same id), so no edge is
			// orphaned by the dedup.
			surv := &entities[pos]
			// Issue #6275 — an ormlink.SubtypeSentinel must never win identity
			// fields over a real (non-sentinel) collision partner. ormlink's own
			// doc comment (internal/extractors/cross/ormlink/extractor.go:46-49)
			// says the sentinel is "intentionally low-quality... so it doesn't
			// compete with the real class entity", but nothing enforced that:
			// #4406's first-writer-wins picks whichever record sorts first
			// (sortEntityRecords orders by StartLine ascending among equal
			// Kind/Name/SourceFile, and the sentinel's StartLine is always 0 —
			// see extractor.go:626 — so it deterministically sorts BEFORE the
			// real class node's real line and becomes the survivor). The
			// sentinel's Subtype and QualifiedName are both NON-empty synthetic
			// values (see extractor.go's SubtypeSentinel / MAPS_TO anchor
			// construction), so the ordinary "only fill empty fields" gap-fill
			// below would keep them forever, permanently mislabeling the
			// now-content-ful survivor as a sentinel. Blanking them here lets
			// the existing gap-fill machinery promote the real record's values,
			// exactly as if the sentinel had arrived second.
			//
			// QualifiedName is blanked ONLY when r actually carries a
			// replacement (r.QualifiedName != ""): QualifiedName drives
			// byQualifiedName resolution and cross-repo joins (#4406's own
			// comment above), so if r has none, blanking survivor's here would
			// leave the gap-fill below with nothing to restore it from — the
			// survivor would end up with NO QualifiedName at all, strictly
			// worse than keeping the sentinel's synthetic one. Subtype has no
			// such asymmetry: this branch's own condition already requires
			// r.Subtype != "", so the gap-fill below always has a real value
			// to promote.
			if surv.Subtype == ormlink.SubtypeSentinel && r.Subtype != "" && r.Subtype != ormlink.SubtypeSentinel {
				surv.Subtype = ""
				if r.QualifiedName != "" {
					surv.QualifiedName = ""
				}
			}
			if surv.QualifiedName == "" && r.QualifiedName != "" {
				surv.QualifiedName = r.QualifiedName
			}
			if surv.Subtype == "" && r.Subtype != "" {
				surv.Subtype = r.Subtype
			}
			if surv.Signature == "" && r.Signature != "" {
				surv.Signature = r.Signature
			}
			if surv.Language == "" && r.Language != "" {
				surv.Language = r.Language
			}
			if surv.StartLine == 0 && r.StartLine != 0 {
				surv.StartLine = r.StartLine
			}
			if surv.EndLine == 0 && r.EndLine != 0 {
				surv.EndLine = r.EndLine
			}
			if len(r.Tags) > 0 {
				seenTag := make(map[string]bool, len(surv.Tags)+len(r.Tags))
				for _, t := range surv.Tags {
					seenTag[t] = true
				}
				for _, t := range r.Tags {
					if !seenTag[t] {
						seenTag[t] = true
						surv.Tags = append(surv.Tags, t)
					}
				}
			}
			if len(r.Properties) > 0 {
				if surv.PropLen() == 0 {
					surv.PropsReplace(make(map[string]string, len(r.Properties)))
				}
				for pk, pv := range r.Properties {
					if _, exists := surv.PropLookup(pk); !exists {
						surv.PropSet(pk, pv)
					}
				}
			}
			if len(r.Metadata) > 0 {
				if surv.Metadata == nil {
					surv.Metadata = make(map[string]interface{}, len(r.Metadata))
				}
				for mk, mv := range r.Metadata {
					if _, exists := surv.Metadata[mk]; !exists {
						surv.Metadata[mk] = mv
					}
				}
			}
		}

		for j := range r.Relationships {
			rel := &r.Relationships[j]
			fromID := rel.FromID
			toID := rel.ToID
			if fromID == "" {
				fromID = id
			}
			relID := graph.RelationshipID(fromID, toID, rel.Kind)
			if seenRel[relID] {
				continue
			}
			seenRel[relID] = true
			relationships = append(relationships, graph.Relationship{
				ID:     relID,
				FromID: fromID,
				ToID:   toID,
				Kind:   rel.Kind,

				Confidence: rel.Confidence, // Phase 1C (#2769).
			}.WithProperties(rel.Properties.Snapshot()))
		}

		// #5955 — drain the record now that it is fully converted. This is the
		// LAST reader of merged: nothing below the assembly loop touches merged
		// (the tail only reads pass2Rels/patternContainsRels/mongoAggStageJoinRels
		// and the already-built entities/relationships slices). Zeroing the slot
		// drops merged's reference to this record's embedded Relationships backing
		// array (never copied into the Document — rels are re-materialised as
		// graph.Relationship above), letting the GC reclaim it incrementally
		// instead of holding every record's rel slice alive until buildDocument
		// returns. The Properties/Tags/Metadata maps survive via the emitted
		// graph.Entity (first-seen) or were already copied onto the survivor
		// (dedup branch), so nil-ing the slot cannot lose output state.
		merged[k] = types.EntityRecord{}
	}

	// Pass 2.5 standalone relationships: synthesise FromID/ToID from the
	// engine's "kind:name" stub strings. We look those up in the merged
	// entity index by name; unmatched stubs are kept as bare strings so the
	// relationship is still surfaced and can be reconciled downstream.
	for j := range pass2Rels {
		rel := &pass2Rels[j]
		fromID := rel.FromID
		toID := rel.ToID
		relID := graph.RelationshipID(fromID, toID, rel.Kind)
		if seenRel[relID] {
			continue
		}
		seenRel[relID] = true
		relationships = append(relationships, graph.Relationship{
			ID:     relID,
			FromID: fromID,
			ToID:   toID,
			Kind:   rel.Kind,

			Confidence: rel.Confidence, // Phase 1C (#2769).
		}.WithProperties(rel.Properties.Snapshot()))
	}

	// SCOPE.Pattern → file CONTAINS fixup (see buildPatternContainsRels
	// above). Injected here, AFTER disposition classification, so the
	// bug-rate stays byte-identical to main while the orphan-rate drops
	// by the count of Pattern entities in the repo. Both endpoints are
	// already deterministic hex IDs computed via graph.EntityID, so they
	// flow straight into the output without rewrite or classification.
	for j := range patternContainsRels {
		rel := &patternContainsRels[j]
		relID := graph.RelationshipID(rel.FromID, rel.ToID, rel.Kind)
		if seenRel[relID] {
			continue
		}
		seenRel[relID] = true
		relationships = append(relationships, graph.Relationship{
			ID:     relID,
			FromID: rel.FromID,
			ToID:   rel.ToID,
			Kind:   rel.Kind,
		}.WithProperties(rel.Properties.Snapshot()))
	}

	// #4244 — node-anchored $lookup JOINS_COLLECTION twins (see
	// buildMongoAggStageJoinRels above). Both endpoints are deterministic hex
	// ids (FromID = the stage node's graph id), so they flow straight into the
	// output without rewrite or classification — same as patternContainsRels.
	for j := range mongoAggStageJoinRels {
		rel := &mongoAggStageJoinRels[j]
		relID := graph.RelationshipID(rel.FromID, rel.ToID, rel.Kind)
		if seenRel[relID] {
			continue
		}
		seenRel[relID] = true
		relationships = append(relationships, graph.Relationship{
			ID:     relID,
			FromID: rel.FromID,
			ToID:   rel.ToID,
			Kind:   rel.Kind,
		}.WithProperties(rel.Properties.Snapshot()))
	}

	return &graph.Document{
		Version:        graph.SchemaVersion,
		GeneratedAt:    deterministicGeneratedAt(),
		Repo:           i.repoTag,
		IndexerVersion: version.String(),
		Stats: graph.Stats{
			Files:         i.stats.files,
			Entities:      len(entities),
			Relationships: len(relationships),
		},
		Entities:      entities,
		Relationships: relationships,
	}
}

// incrementalMergeStats reports what mergeIncrementalPrevDoc carried forward.
type incrementalMergeStats struct {
	entitiesAdded int
	relsAdded     int
	relsDropped   int
}

// mergeIncrementalPrevDoc splices unchanged-file entities and relationships
// from prev into doc when doc was built from a changed-files-only subset
// (issue #2719). The function is path-pure (it mutates doc in place) and:
//
//   - Adds every prev entity whose SourceFile is NOT in changedFiles, unless
//     an entity with the same ID is already present in doc (which would
//     indicate a re-extraction collision from a freshly-extracted file).
//   - Carries a prev relationship forward UNLESS it is genuinely superseded by
//     this run's re-extraction. #6085: the staleness test is "was this edge
//     re-produced by the files we just re-extracted?", NOT "do both endpoints
//     resolve to a live entity row". An edge is dropped iff:
//     (a) its FromID is a prev entity anchored in a CHANGED file — that file's
//     re-extraction re-emitted its outgoing edges, so the prev copy is
//     superseded; or
//     (b) its ToID is a prev entity anchored in a CHANGED file that no longer
//     exists after re-extraction (renamed/deleted) — the edge now points at
//     a destroyed node.
//     Every other prev edge is preserved verbatim, including edges whose
//     endpoint is a synthetic ext:* node or an unresolved bare name. Those
//     endpoints are not entity rows, so the old endpoint-liveness test dropped
//     them wholesale even though a full reindex keeps them (#6085: ~35% of the
//     graph on archigraph — every IMPORTS/CALLS edge into an external package
//     plus every unresolved-target edge).
//   - Carries forward the source-less prev entities (ext:* / _external
//     synthetics) that are still referenced by a surviving edge, so those
//     carried edges do not dangle. external.Synthesize runs after this merge
//     and is idempotent against entity IDs already present, so re-adding them
//     here does not double-emit.
//   - Dedupes carried edges against the fresh ones by (from, to, kind, ID)
//     multiplicity. Before #6085 graph.fb persisted no relationship ID at all,
//     so every prev edge arrived with an empty one, the dedupe never matched,
//     and each incremental run appended duplicates of edges the fresh pass had
//     already emitted (the run-to-run count drift). fbwriter now persists the
//     ID; the key includes it because (from, to, kind) is NOT unique — see
//     graph.RelationshipIDProperty for the producers that rely on that.
//   - Supersedes a carried edge whose relationship ID is one THIS RUN emitted,
//     whatever the two payloads say (#6160). The multiplicity key above is
//     payload-sensitive and so could not see the same edge captured at two
//     different pipeline stages; the fresh row wins, per this function's
//     precedence everywhere else. Supersession is per-ROW and budgeted by how
//     many rows the fresh pass emitted under that ID — one fresh row can never
//     erase a whole family of carried rows sharing it, which is what a
//     pre-#6085 file's unrecoverable salted edges decode to.
//
// The function is intentionally separate from internal/extractors.TryIncremental:
// TryIncremental owns the daemon's fast in-place reindex (Path A), while this
// helper owns the CLI's full-pipeline incremental rebuild (Path B). Sharing
// extraction logic would invert the dependency (cmd → internal/extractors
// already; internal/extractors → cmd would be a cycle), so the merge step is
// duplicated by design — both implementations are exercised by their own
// regression tests in this package and internal/extractors.
func mergeIncrementalPrevDoc(doc *graph.Document, prev *graph.Document, changedFiles map[string]bool) incrementalMergeStats {
	if prev == nil {
		return incrementalMergeStats{}
	}
	return mergeIncrementalPrevSource(doc, docPrevSource{prev}, changedFiles)
}

// prevGraphSource is the merge's read contract over the previous graph. It
// exists so the merge can be driven either by a fully materialised
// *graph.Document (every unit test in this package, and the graph.json
// fallback) or by a *graph.GraphStream that decodes rows straight off the
// mmap and never holds them all at once (#5954 item 16 — the production
// incremental path). The two must be indistinguishable to the merge, which is
// what TestMergeIncrementalPrevSource_StreamMatchesDocument asserts.
//
// Both walks are re-enterable: the indexer streams entities once early (to seed
// the resolver index with unchanged-file identities) and again here.
type prevGraphSource interface {
	EntityCount() int
	RelationshipCount() int
	EachEntity(func(graph.Entity) bool)
	EachRelationship(func(graph.Relationship) bool)
}

// docPrevSource adapts an in-memory Document to prevGraphSource.
type docPrevSource struct{ d *graph.Document }

func (s docPrevSource) EntityCount() int       { return len(s.d.Entities) }
func (s docPrevSource) RelationshipCount() int { return len(s.d.Relationships) }

func (s docPrevSource) EachEntity(visit func(graph.Entity) bool) {
	for i := range s.d.Entities {
		if !visit(s.d.Entities[i]) {
			return
		}
	}
}

func (s docPrevSource) EachRelationship(visit func(graph.Relationship) bool) {
	for i := range s.d.Relationships {
		if !visit(s.d.Relationships[i]) {
			return
		}
	}
}

// mergeIncrementalPrevSource is the merge itself, driven by prevGraphSource.
// See mergeIncrementalPrevDoc's doc comment for the semantics — this function
// owns them; that one is the Document-shaped entry point.
func mergeIncrementalPrevSource(doc *graph.Document, prev prevGraphSource, changedFiles map[string]bool) incrementalMergeStats {
	var stats incrementalMergeStats
	if doc == nil || prev == nil {
		return stats
	}
	// Build a set of entity IDs already present in doc (sourced from
	// changed-file re-extraction). These take precedence over the previous
	// graph — they reflect the current source code.
	docEntityIDs := make(map[string]bool, len(doc.Entities))
	for _, e := range doc.Entities {
		docEntityIDs[e.ID] = true
	}

	// First pass: copy across entities sourced from UNCHANGED files, and
	// record the IDs of prev entities anchored in a CHANGED file — those are
	// the only ones whose incident edges this run could have superseded.
	changedEntityIDs := make(map[string]bool)
	// Source-less prev entities (ext:* / _external synthetics) are held back
	// until the relationship pass has told us which of them a surviving edge
	// still references (#6085).
	var syntheticPrev []graph.Entity
	prev.EachEntity(func(e graph.Entity) bool {
		if e.SourceFile == "" {
			if !docEntityIDs[e.ID] {
				syntheticPrev = append(syntheticPrev, e)
			}
			return true
		}
		if changedFiles[filepath.ToSlash(e.SourceFile)] {
			changedEntityIDs[e.ID] = true
			return true
		}
		if docEntityIDs[e.ID] {
			return true
		}
		doc.Entities = append(doc.Entities, e)
		docEntityIDs[e.ID] = true
		stats.entitiesAdded++
		return true
	})

	// Second pass: copy across every prev relationship that this run did not
	// supersede. See the doc comment for the exact drop predicate (#6085).
	//
	// Dedupe key is (from, to, kind, ID, properties).
	//
	// The ID is load-bearing, NOT redundant: several producers deliberately
	// mint distinct IDs for edges that share one triple (migration ops salted
	// per operation, phantom CALLS salted per HTTP method, process/event steps
	// salted per step index — see graph.RelationshipIDProperty), so a
	// triple-keyed dedupe silently collapses semantically distinct edges.
	//
	// The properties are load-bearing too, for the upgrade path: a graph.fb
	// written before #6085 persisted no identity at all, so those salted
	// siblings all load with the SAME derived ID and are separable only by
	// their payload. Keying on the payload keeps all of them.
	//
	// Anything left over — same endpoints, same kind, same identity, same
	// payload — is the same edge twice, never two edges. Collapsing it is what
	// stops the graph growing: passes that run AFTER this merge append edges
	// that are already in the carried-forward set, and without this the
	// duplicates compound on every incremental run (measured: +380 rows/run,
	// unbounded).
	//
	// An absent ID is normalized to the derived one — exactly what
	// LoadGraphFromDir does — so a prev Document assembled by hand keys the
	// same way as one read off disk, and an ID-less prev edge cannot slip past
	// the dedupe and duplicate an edge the fresh pass already emitted.
	relKey := func(r *graph.Relationship) string {
		id := r.ID
		if id == "" {
			id = graph.RelationshipID(r.FromID, r.ToID, r.Kind)
		}
		var b strings.Builder
		b.WriteString(r.FromID)
		b.WriteByte(0)
		b.WriteString(r.ToID)
		b.WriteByte(0)
		b.WriteString(r.Kind)
		b.WriteByte(0)
		b.WriteString(id)
		props := r.PropsSnapshot()
		if len(props) > 0 {
			keys := make([]string, 0, len(props))
			for k := range props {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				b.WriteByte(0)
				b.WriteString(k)
				b.WriteByte(1)
				b.WriteString(props[k])
			}
		}
		return b.String()
	}
	// relID is relKey's identity half on its own, with the same normalization
	// of an absent ID to the derived one. The gate below and the key above MUST
	// agree on what a row's identity is; two identity rules in one loop is how
	// the next bug gets in.
	relID := func(r *graph.Relationship) string {
		if r.ID != "" {
			return r.ID
		}
		return graph.RelationshipID(r.FromID, r.ToID, r.Kind)
	}

	docRelKeys := make(map[string]bool, len(doc.Relationships))
	// #6160 — the supersession gate, keyed on the relationship ID and scoped
	// to THIS RUN's output. supersedeBudget counts the rows the fresh
	// extraction emitted per ID; prev rows never add to it.
	//
	// The property-sensitive key above cannot see a prev edge that is the SAME
	// edge as a fresh one but was captured at a different pipeline stage: the
	// resolved IMPLEMENTS bridge (post-#2678 rebind,
	// pattern_type=http_endpoint_synthesis_resolved) and the synthesis-time one
	// (pattern_type=http_endpoint_synthesis_time_bridge) carry the same
	// FromID/ToID/Kind and the SAME ID, and differ only in payload — so the
	// dedupe below never fired and both rows persisted under one identity.
	//
	// It is a BUDGET and not a set membership test, and that is load-bearing.
	// A graph.fb written before #6085 persisted no relationship identity, and
	// the decoder normalizes an absent ID to the DERIVED one — so a legacy
	// graph's deliberately salted siblings (process/event steps salted per step
	// index, migration ops salted per operation, see
	// graph.RelationshipIDProperty) arrive here as a FAMILY of rows all
	// carrying one shared, explicit ID, separable only by payload. Against a
	// key-presence gate, one fresh row emitted under that ID would delete the
	// whole family. Supersession is per-ROW: one fresh row supersedes at most
	// one carried row. internal/graph/load.go's decode comment states exactly
	// this property of this merge, and it is the reason a legacy file's
	// unrecoverable salted rows survive a reindex.
	supersedeBudget := make(map[string]int, len(doc.Relationships))
	for k := range doc.Relationships {
		docRelKeys[relKey(&doc.Relationships[k])] = true
		supersedeBudget[relID(&doc.Relationships[k])]++
	}
	// Carried rows that collide with a fresh ID are held back rather than
	// decided on sight: the budget must be spent on the row the fresh pass
	// ACTUALLY duplicates, and that row can arrive anywhere in the stream. A
	// greedy decision gets the count right and the content wrong — it spends
	// the budget on whichever colliding row comes first and then drops the real
	// duplicate as an exact payload match, deleting a row the fresh pass never
	// emitted.
	//
	// This holds rows, which the streaming source (#5954 item 16) otherwise
	// avoids — but only rows whose ID the fresh pass ALSO emitted. For a salted
	// family that requires the owning file to have been re-extracted, and
	// predicate (a) below has already dropped those rows before they get here.
	// In practice the buffer holds a handful of rows or none.
	deferred := make(map[string][]graph.Relationship)
	var deferredOrder []string

	referenced := make(map[string]bool)
	prev.EachRelationship(func(r graph.Relationship) bool {
		// (a) the edge's owning file was re-extracted this run.
		if changedEntityIDs[r.FromID] {
			stats.relsDropped++
			return true
		}
		// (b) the edge's target was destroyed by this run's re-extraction.
		if changedEntityIDs[r.ToID] && !docEntityIDs[r.ToID] {
			stats.relsDropped++
			return true
		}
		// Mark the endpoints BEFORE the dedupe below: an edge the fresh pass
		// already emitted still references its endpoints, and the synthetic
		// node it points at must be carried forward for THAT edge's sake.
		referenced[r.FromID] = true
		referenced[r.ToID] = true
		r.ID = relID(&r)
		key := relKey(&r)
		if docRelKeys[key] {
			// Already in the merged graph — either emitted by this run's
			// extraction, or an exact duplicate row earlier in prev. When the
			// budget is still open this is the fresh row's true duplicate, so
			// spend it here in preference to any deferred sibling.
			if supersedeBudget[r.ID] > 0 {
				supersedeBudget[r.ID]--
			}
			return true
		}
		if supersedeBudget[r.ID] > 0 {
			if _, seen := deferred[r.ID]; !seen {
				deferredOrder = append(deferredOrder, r.ID)
			}
			deferred[r.ID] = append(deferred[r.ID], r)
			return true
		}
		docRelKeys[key] = true
		doc.Relationships = append(doc.Relationships, r)
		stats.relsAdded++
		return true
	})

	// Spend whatever budget the exact-duplicate branch left, then carry the
	// rest of each colliding family forward. deferredOrder keeps this
	// deterministic — map iteration order must not decide which row survives.
	for _, id := range deferredOrder {
		rows := deferred[id]
		superseded := supersedeBudget[id]
		if superseded > len(rows) {
			superseded = len(rows)
		}
		for i := superseded; i < len(rows); i++ {
			key := relKey(&rows[i])
			if docRelKeys[key] {
				continue
			}
			docRelKeys[key] = true
			doc.Relationships = append(doc.Relationships, rows[i])
			stats.relsAdded++
		}
	}

	// Third pass: re-attach the source-less synthetic endpoints that surviving
	// edges still point at, so no carried edge dangles. Synthetics nothing
	// references are left out — external.Synthesize regenerates whatever the
	// current graph actually needs.
	for _, e := range syntheticPrev {
		if !referenced[e.ID] || docEntityIDs[e.ID] {
			continue
		}
		doc.Entities = append(doc.Entities, e)
		docEntityIDs[e.ID] = true
		stats.entitiesAdded++
	}

	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)
	return stats
}

// runJavaAnnotationRoutes runs the Java JAX-RS / Spring MVC annotation
// route composition pass over the set of classified files. Builds a
// content-lookup map from repo-relative path to raw bytes (Java files
// only), then delegates to engine.ApplyJavaAnnotationRoutes.
//
// Fixes #682 (wrong source_handler kind/name) and #683 (annotation budget
// exhaustion in old jaxrsMethodVerbRe regex). Refs #682, #683.
func runJavaAnnotationRoutes(classified []classifiedFile) []types.EntityRecord {
	if len(classified) == 0 {
		return nil
	}
	contentByPath := make(map[string][]byte, len(classified))
	var javaPaths []string
	// #1942 Phase 1 — collect Quarkus signals so the synthesised endpoints
	// carry a resolved auth_policy (annotations → class-level inheritance →
	// Quarkus config-driven permissions → quarkus-security framework default).
	buildDescriptors := map[string]string{}
	propertiesFiles := map[string]string{}
	for _, cf := range classified {
		switch cf.language {
		case "java":
			contentByPath[cf.relPath] = cf.content
			javaPaths = append(javaPaths, cf.relPath)
		}
		base := cf.relPath
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[idx+1:]
		}
		switch base {
		case "pom.xml", "build.gradle", "build.gradle.kts":
			buildDescriptors[cf.relPath] = string(cf.content)
		case "application.properties":
			propertiesFiles[cf.relPath] = string(cf.content)
		}
	}
	if len(javaPaths) == 0 {
		return nil
	}
	reader := func(relPath string) []byte {
		return contentByPath[relPath]
	}
	authCtx := engine.BuildJavaAuthContext(buildDescriptors, propertiesFiles)
	return engine.ApplyJavaAnnotationRoutesWithContext(javaPaths, reader, authCtx)
}

// releaseClassifiedASTs drops the source bytes attached to each
// classifiedFile entry. Called after the last extractor pass (Pass 3
// cross-language) finishes; setting .content to nil drops the per-file
// source-byte buffer the resolver no longer needs. Issue #633.
//
// It no longer closes parse trees: since #5954 classifiedFile carries none.
// Pass 1 closes each tree as soon as its extractor returns, which is what
// keeps the CGo high-water bounded — closing here was far too late (the peak
// had already been paid) and macOS libmalloc never returns the pages anyway.
// Re-adding a close here would also risk a double free: ts.Tree.Close() maps
// straight onto ts_tree_delete and is not idempotent.
func releaseClassifiedASTs(classified []classifiedFile) {
	for k := range classified {
		classified[k].content = nil
	}
}

// concatRecords returns the in-order concatenation of several record
// slices. Used by the corpus-wide response-shape pass (#753) which
// needs to see the full producer-side entity set (handlers + synthetic
// http_endpoints) without modifying the per-pass slices that
// buildDocument later merges in canonical order.
//
// The returned slice shares the underlying EntityRecord values with the
// inputs — embedded Properties maps are reference-shared, so the
// post-pass can mutate them and the changes are visible via the
// original slices.
func concatRecords(slices ...[]types.EntityRecord) []types.EntityRecord {
	total := 0
	for _, s := range slices {
		total += len(s)
	}
	if total == 0 {
		return nil
	}
	out := make([]types.EntityRecord, 0, total)
	for _, s := range slices {
		out = append(out, s...)
	}
	return out
}

// countEmbeddedRels totals the relationships embedded inside EntityRecords.
func countEmbeddedRels(records []types.EntityRecord) int {
	n := 0
	for k := range records {
		n += len(records[k].Relationships)
	}
	return n
}

// passAliases maps user-facing flag values onto the canonical PassXxx constant.
// "algorithms" is accepted as a more readable synonym of "graph-algo".
var passAliases = map[string]string{
	"algorithms": PassGraphAlgo,
}

// parseSkipPasses validates a comma-separated --skip-pass list and returns
// it as a set. Unknown entries are surfaced as an error so typos don't
// silently degrade the pipeline.
func parseSkipPasses(skip []string) (map[string]bool, error) {
	out := make(map[string]bool)
	valid := make(map[string]bool, len(allPassNames))
	for _, n := range allPassNames {
		valid[n] = true
	}
	for _, raw := range skip {
		for _, part := range strings.Split(raw, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			if alias, ok := passAliases[p]; ok {
				p = alias
			}
			if !valid[p] {
				return nil, fmt.Errorf("unknown pass %q (valid: %s)", p, strings.Join(allPassNames, ","))
			}
			out[p] = true
		}
	}
	return out, nil
}

// walkRepo is replaced by walk.WalkRepo (issue #805). This stub remains
// temporarily to avoid breaking any in-package test references; it will
// be removed in a follow-up cleanup pass.
//
// Deprecated: use walk.WalkRepo directly.
func walkRepo(root string) ([]string, error) {
	files, _, err := walk.WalkRepo(root, nil)
	return files, err
}
