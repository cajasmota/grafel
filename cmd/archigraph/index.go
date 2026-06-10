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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/algorithms"
	"github.com/cajasmota/archigraph/internal/classifier"
	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/extract"
	"github.com/cajasmota/archigraph/internal/daemon/walk"
	"github.com/cajasmota/archigraph/internal/engine"
	"github.com/cajasmota/archigraph/internal/enrichment"
	"github.com/cajasmota/archigraph/internal/external"
	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors"
	bazelextract "github.com/cajasmota/archigraph/internal/extractors/bazel"
	configextract "github.com/cajasmota/archigraph/internal/extractors/config"
	"github.com/cajasmota/archigraph/internal/extractors/cross"
	mageextract "github.com/cajasmota/archigraph/internal/extractors/mage"
	pyextr "github.com/cajasmota/archigraph/internal/extractors/python"
	taskextract "github.com/cajasmota/archigraph/internal/extractors/task"
	"github.com/cajasmota/archigraph/internal/gitmeta"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/fbwriter"
	idiff "github.com/cajasmota/archigraph/internal/indexer/diff"
	"github.com/cajasmota/archigraph/internal/ingest"
	"github.com/cajasmota/archigraph/internal/install/detect"
	"github.com/cajasmota/archigraph/internal/module"
	"github.com/cajasmota/archigraph/internal/progress"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/treesitter"
	"github.com/cajasmota/archigraph/internal/types"
	"github.com/cajasmota/archigraph/internal/version"
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
	PassEmbed         = "embed"          // Pass 9: semantic embeddings sidecar (#461 / ADR-0019)
	PassTestsWalkUp   = "tests-walkup"   // Pass 3.5: derive TESTS edges via helper walk-up
)

// allPassNames is used to validate --skip-pass entries.
var allPassNames = []string{
	PassExtract, PassFramework, PassCrossLang, PassTestsWalkUp, PassGraphAlgo, PassBuildDocument, PassRenameDetect, PassEnrichment, PassProcessFlow, PassEventFlow, PassModuleAgg, PassCommitCouple, PassCoupling, PassDepHygiene, PassSharedDB, PassLibBoundary, PassMigrationSeq, PassMigrationOps, PassEmbed,
}

// fileTask carries one repo-relative path and its absolute counterpart
// through the Pass 1 worker pool.
type fileTask struct {
	relPath string
	absPath string
}

// classifiedFile is a file that survived classification — extractors will
// be run against it in Pass 1 and Pass 3.
type classifiedFile struct {
	relPath  string
	absPath  string
	language string
	content  []byte
	tree     *sitter.Tree
}

// Indexer owns the pass-by-pass orchestration. Constructing a fresh Indexer
// per Index() call keeps state (counters, configuration) local.
type Indexer struct {
	repoTag    string
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
	// indexer reads <repo>/.archigraph/repair.json BEFORE the final
	// disposition reclassification pass and rewrites edges per the
	// trust-model rules in docs/specs/repair-trust-model.md.
	enableRepairApply bool

	// incremental enables diff-aware re-indexing (issue #1339). When true the
	// indexer loads the per-repo file-hash manifest from incrementalStateDir,
	// filters the walk result down to only changed files, and updates the
	// manifest after a successful write. Full rebuild still runs when the
	// manifest is absent or stale.
	incremental         bool
	incrementalStateDir string // directory that holds file-index.json

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

	// printSkipped, when true, emits one [skip] line to stderr for each
	// directory that was skipped at walk-time (issue #805). Shows which
	// rule caused the skip (.gitignore, hardcoded, .archigraphignore).
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
	// adds zero overhead. Also honored via env ARCHIGRAPH_INGEST_DOCS=1|true.
	ingestDocs bool

	// singleModuleLabel, when non-empty, forces every entity in this repo into
	// ONE module row instead of the per-directory path rollup. It is set for
	// PLAIN (non-monorepo) repos so the per-module progress + Group-by-Module
	// graph treat the repo as a single unit (issue #1628). For TRUE monorepos
	// it stays empty and module.Derive's per-package rollup applies.
	singleModuleLabel string
}

// IndexOption configures optional behaviour on the Indexer. Used as a
// functional-option list on Index() so existing callers don't have to thread
// new parameters through every site.
type IndexOption func(*Indexer)

// WithRepairCandidates toggles ADR-0015 phase-1 repair_edge emission
// (issue #544). When true the indexer appends repair_edge entries to
// <repo>/.archigraph/enrichment-candidates.json for every bug-extractor /
// bug-resolver disposition. Default is false.
func WithRepairCandidates(enabled bool) IndexOption {
	return func(i *Indexer) { i.enableRepairCandidates = enabled }
}

// WithRepairApply toggles ADR-0015 phase-1 repair.json apply (issue #545).
// When true the indexer reads <repo>/.archigraph/repair.json before the
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
				"archigraph: --export-fb is deprecated; graph.fb is now written by default (ADR-0016 flip-day). Use --export-json if you also need graph.json.\n")
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
// flag; also honored via env ARCHIGRAPH_INGEST_DOCS.
func WithIngestDocs(enabled bool) IndexOption {
	return func(i *Indexer) { i.ingestDocs = enabled }
}

// ingestDocsEnvEnabled reports whether ARCHIGRAPH_INGEST_DOCS requests doc
// ingestion (accepts "1", "true", "yes", "on"; case-insensitive).
func ingestDocsEnvEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ARCHIGRAPH_INGEST_DOCS"))) {
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

// WithIncremental enables diff-aware re-indexing (issue #1339). The indexer
// loads `.archigraph/file-index.json` from stateDir, filters the walked file
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
	}
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
}

// Index walks repoPath, runs the orchestrated passes, and writes the
// resulting entity/relationship graph to outPath (or the default
// <repo>/.archigraph/graph.json). repoTag is stored on every entity; an
// empty value falls back to filepath.Base(repoPath). skipPasses is the
// (possibly empty) set of pass names to skip — see allPassNames. When
// pretty is true, graph.json (and the graph-stats.json sidecar) are
// indented for human readability; the default is minified JSON.
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
	// #1626: relocate any pre-existing in-repo `.archigraph/` graph
	// artifacts into the external store before resolving output paths,
	// so incremental loads + rename detection see the migrated state and
	// the repo working tree is left clean.
	if migrated, mErr := daemon.MigrateInRepoState(absRepo); mErr != nil {
		fmt.Fprintf(os.Stderr, "archigraph: in-repo state migration: %v\n", mErr)
	} else if migrated {
		fmt.Fprintf(os.Stderr, "archigraph: migrated in-repo .archigraph → %s\n", daemon.StateDirForRepo(absRepo))
	}
	if outPath == "" {
		outPath = daemon.GraphPathForRepo(absRepo)
	}

	skipSet, err := parseSkipPasses(skipPasses)
	if err != nil {
		return err
	}

	cls, err := classifier.New("", nil)
	if err != nil {
		return fmt.Errorf("init classifier: %w", err)
	}
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
		},
	}
	for _, opt := range opts {
		opt(idx)
	}

	// #4306: env fallback for the opt-in markdown-ingestion flag, so the
	// subsystem can be exercised without threading a new field through the
	// daemon RPC proto (deploy-deferred). The CLI --ingest-docs flag and any
	// WithIngestDocs(true) option take precedence; the env var only flips it ON.
	if !idx.ingestDocs && ingestDocsEnvEnabled() {
		idx.ingestDocs = true
	}

	doc, err := idx.Run(context.Background(), absRepo)
	if err != nil {
		return err
	}

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
			fmt.Fprintf(os.Stderr, "archigraph: git HEAD %s @ %s (worktree=%v)\n",
				gi.SHA, func() string {
					if gi.Ref == "" {
						return "detached"
					}
					return gi.Ref
				}(), gi.IsWorktree)
		}
	}

	// Pass 5.5 — rename detection (#1344). Load the previous graph from disk
	// and compare it with the freshly-built doc to detect entity renames,
	// moves, and splits. Runs BEFORE the final sort and disk write so the
	// emitted RENAMED_FROM edges are included in graph.fb / graph.json.
	// The pass is append-only and safe to skip with --skip-pass=rename-detect.
	if !skipSet[PassRenameDetect] {
		stateDir := filepath.Dir(outPath)
		if prevDoc, err := graph.LoadGraphFromDir(stateDir); err == nil {
			renameStats := algorithms.DetectRenames(prevDoc, doc)
			if renameStats.Renames > 0 {
				fmt.Fprintf(os.Stderr,
					"rename-detect: %d rename(s) detected (moves=%d splits=%d)\n",
					renameStats.Renames, renameStats.Moves, renameStats.Splits)
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
	// algo pass — archigraph_clusters would return [] and the docs skill would
	// fall back to dir-derived modules.
	//
	// To avoid that, when the algo pass is skipped we copy the prior graph's
	// per-entity community_id/pagerank/centrality/flags (matched by stable
	// entity ID) plus the aggregate Communities list and AlgorithmStats onto
	// the freshly-built doc. New entities (no prior match) simply stay
	// un-annotated until the next full algo pass runs — never worse than the
	// pre-fix behaviour where ALL entities lost their community.
	if skipSet[PassGraphAlgo] {
		stateDir := filepath.Dir(outPath)
		if prevDoc, perr := graph.LoadGraphFromDir(stateDir); perr == nil && prevDoc != nil {
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
		fbPath := filepath.Join(filepath.Dir(outPath), "graph.fb")
		if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
			fmt.Fprintf(os.Stderr, "archigraph: graph.fb write failed: %v\n", err)
			// Non-fatal — we still try to write graph.json so the system
			// remains functional. If both fail, the error from graph.json
			// propagates below.
		} else {
			fmt.Fprintf(os.Stderr, "archigraph: wrote %s\n", fbPath)
		}

		if idx.exportJSON {
			if err := graph.WriteAtomic(outPath, doc, pretty); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "archigraph: wrote %s\n", outPath)

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
				fmt.Fprintf(os.Stderr, "archigraph: embeddings: %v\n", err)
			}
		}

		// Sidecar: corpus-level metrics for `archigraph doctor` and the future
		// MCP `graph_stats` tool. Only written when Pass 4 actually ran.
		if doc.AlgorithmStats != nil {
			side := &graph.GraphStatsSidecar{
				Version:            1,
				ComputedAt:         deterministicGeneratedAt(),
				TotalFiles:         doc.Stats.Files,
				TotalEntities:      doc.Stats.Entities,
				TotalRelationships: doc.Stats.Relationships,
				Communities:        doc.AlgorithmStats.NumCommunities,
				Modularity:         doc.AlgorithmStats.LouvainModularity,
				GodNodes:           doc.AlgorithmStats.NumGodNodes,
				ArticulationPoints: doc.AlgorithmStats.NumArticulationPts,
				RuntimeMS:          doc.AlgorithmStats.RuntimeMS,
			}
			if err := graph.WriteSidecar(outPath, side, pretty); err != nil {
				fmt.Fprintf(os.Stderr, "archigraph: sidecar write failed: %v\n", err)
			}
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

// emitJSONStats writes a JSONStats record to w. Used by `archigraph index
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

// Run executes the orchestrated pipeline. Each pass is a named method so
// callers (and tests) can reason about per-pass output independently.
func (i *Indexer) Run(ctx context.Context, absRepo string) (*graph.Document, error) {
	start := time.Now()

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

	// M4 sparse-checkout (#2181): probe the repo BEFORE the walk so the
	// walker can filter out files that are not present locally. The result is
	// also stored on the Document so the dashboard can show a "partial" badge.
	sparseInfo := gitmeta.ProbeRepo(absRepo)
	if sparseInfo.IsSparse {
		fmt.Fprintf(os.Stderr,
			"archigraph: sparse-checkout detected (%d pattern(s)) — indexing partial working tree\n",
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
	files, _, err := walk.WalkRepo(absRepo, walkOpts)
	if err != nil {
		trk.Fail(err.Error())
		return nil, fmt.Errorf("walk repo: %w", err)
	}
	i.stats.files = len(files)
	trk.SetFilesTotal(len(files))
	// Emit a second scan event now that we know the total.
	trk.Tick(progress.PhaseScan, len(files), 0, "", 0)
	fmt.Fprintf(os.Stderr, "archigraph: discovered %d candidate files in %s\n", len(files), absRepo)

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
		diffManifest      *idiff.Manifest
		allFiles          = files // original full list for manifest update
		incrementalPrev   *graph.Document
		incrementalChange map[string]bool // changed-file set (forward-slash, relative)
	)
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
				"archigraph: incremental — processing %d of %d files (%.1f%% cache hit)\n",
				diffStats.Changed, diffStats.Total, diffStats.CacheHitRate())
			files = changed
			// #2719: load the previous graph now so the unchanged-file
			// portion can be merged into the current run's doc below. If the
			// load fails we fall back to a full reindex by clearing the
			// changed-set: every file stays in `files` and the rest of the
			// pipeline reindexes everything (the old broken behaviour of
			// silently corrupting the graph is replaced with a safe
			// full-reindex fallback).
			prev, perr := graph.LoadGraphFromDir(i.incrementalStateDir)
			if perr != nil || prev == nil {
				fmt.Fprintf(os.Stderr,
					"archigraph: incremental — previous graph not loadable (%v); falling back to full reindex\n", perr)
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
				cf := make([]types.EntityRecord, 0, len(prev.Entities))
				for ei := range prev.Entities {
					pe := &prev.Entities[ei]
					if pe.SourceFile == "" {
						continue
					}
					if incrementalChange[filepath.ToSlash(pe.SourceFile)] {
						continue
					}
					cf = append(cf, types.EntityRecord{
						ID:         pe.ID,
						Name:       pe.Name,
						Kind:       pe.Kind,
						Subtype:    pe.Subtype,
						SourceFile: pe.SourceFile,
						Properties: pe.Properties,
					})
				}
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
	// ARCHIGRAPH_SUBPROC_EXTRACT=1 during the rollout so the in-process
	// path remains the default until benchmarks + quality fixtures
	// confirm byte-identical output. When enabled, the coordinator
	// fork-execs `archigraph extract` per language-bucketed batch and
	// returns the merged record set (Pass 1 + 2.5 + 3 combined); the
	// daemon then runs everything from buildDocument onward unchanged.
	trk.PhaseStart(progress.PhaseExtractAST, 0, 0)
	if subprocExtract() {
		res, cerr := extract.Coordinate(ctx, absRepo, files, extract.CoordinatorConfig{
			Concurrency: subprocConcurrency(),
			BatchSize:   subprocBatchSize(),
			SkipPasses:  skipPassNames(i.skipPasses),
			Stderr:      os.Stderr,
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
		// Issue #2447: propagate Pass1Plumbed counters from subprocess path.
		i.stats.pass1PlumbedTrue += res.Pass1PlumbedTrueCount
		i.stats.pass1PlumbedFalse += res.Pass1PlumbedFalseCount
		fmt.Fprintf(os.Stderr,
			"archigraph: subproc-extract subprocs=%d peak_rss=%.1fMB entities=%d rels=%d\n",
			res.Subprocesses,
			float64(res.PeakRSSBytes)/(1024*1024),
			len(res.Entities), len(res.Relationships))
		for _, e := range res.NonFatalErrors {
			fmt.Fprintf(os.Stderr, "archigraph: subproc-extract warning: %s\n", e)
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
		pass2Records, pass2Rels, err = i.runPass25FrameworkRules(ctx, absRepo, classified, pass1Records)
		if err != nil {
			trk.Fail(err.Error())
			return nil, fmt.Errorf("pass 2.5: %w", err)
		}
		i.stats.pass2Rels = len(pass2Rels) + countEmbeddedRels(pass2Records)

		// Pass 3 — cross-language extractors.
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
	configEntities, configRels, configErr := configextract.Discover(ctx, absRepo, allFiles)
	if configErr != nil {
		fmt.Fprintf(os.Stderr, "archigraph: config-discover warning: %v\n", configErr)
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
	bazelEntities, bazelRels, bazelErr := bazelextract.Discover(ctx, absRepo, allFiles)
	if bazelErr != nil {
		fmt.Fprintf(os.Stderr, "archigraph: bazel-discover warning: %v\n", bazelErr)
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
	mageEntities, mageRels, mageErr := mageextract.Discover(ctx, absRepo, allFiles)
	if mageErr != nil {
		fmt.Fprintf(os.Stderr, "archigraph: mage-discover warning: %v\n", mageErr)
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
	taskEntities, taskRels, taskErr := taskextract.Discover(ctx, absRepo, allFiles)
	if taskErr != nil {
		fmt.Fprintf(os.Stderr, "archigraph: task-discover warning: %v\n", taskErr)
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
		nestedEntities := runDjangoNestedURLConf(classified)

		// Pass 2.6b — DRF router.register expansion (#703, #705). Emits
		// detail-route ({pk}) and @action endpoints alongside the list
		// route that runDjangoNestedURLConf already produces.
		drfEntities := runDjangoDRFRoutes(classified)
		if len(drfEntities) > 0 {
			fmt.Fprintf(os.Stderr, "archigraph: drf_router_expanded=%d entities\n", len(drfEntities))
		}

		// Issue #792: Deduplicate urlconf_nested_include ANY entries when
		// drf_router_expanded per-verb entries cover the same path.
		deduplicatedNestedEntities := engine.DeduplicateNestedURLConfDRF(nestedEntities, drfEntities)
		if len(nestedEntities) > len(deduplicatedNestedEntities) {
			fmt.Fprintf(os.Stderr, "archigraph: deduped %d urlconf_nested_include entries (drf coverage)\n",
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
			fmt.Fprintf(os.Stderr, "archigraph: deduped %d http_endpoint_synthesis ANY entries (drf coverage)\n", deduped)
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
			fmt.Fprintf(os.Stderr, "archigraph: django_cbv_routes=%d entities\n", len(cbvEntities))
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
			fmt.Fprintf(os.Stderr, "archigraph: django_admin_synthetic=%d entities\n", len(adminEntities))
		}
		pass3Records = append(pass3Records, adminEntities...)

		// Pass 2.6e-vs — DRF ViewSet.as_view({dict}) route synthesis (#2614).
		// Handles the explicit method-map mounting pattern that appears outside
		// router.register() — e.g. the upvate notification routes where
		// NotificationViewSet.as_view({'get': 'list', 'delete': 'delete_all'})
		// is pre-bound to a variable and then wired into urlpatterns. Covers
		// both the pre-bound-variable form and the inline as_view({}) form.
		vsAsViewEntities := runDjangoViewSetAsViewRoutes(classified)
		if len(vsAsViewEntities) > 0 {
			fmt.Fprintf(os.Stderr, "archigraph: drf_viewset_asview_routes=%d entities\n", len(vsAsViewEntities))
		}
		pass3Records = append(pass3Records, vsAsViewEntities...)

		// Pass 2.6e — Celery cross-file dispatch edges (#1617). The per-file
		// scheduled-job pass only links `task.delay()` call sites that share a
		// file with the @shared_task definition. This repo-wide pass resolves
		// dispatch sites across files (tasks/ ← views/ ← signals/ ← services/)
		// and emits CALLS edges so find_callees / find_callers on a task and
		// the flows view show task dispatch. Append-only.
		celeryDispatchRels := runCeleryDispatchEdges(classified)
		if len(celeryDispatchRels) > 0 {
			fmt.Fprintf(os.Stderr, "archigraph: celery_dispatch_edges=%d\n", len(celeryDispatchRels))
		}
		pass2Rels = append(pass2Rels, celeryDispatchRels...)

		// Pass 2.6f — Django custom-signal pub/sub edges (#1617). Models each
		// `sig = Signal()` custom signal as a SCOPE.MessageTopic, with
		// SUBSCRIBES_TO from every @receiver(sig) handler and PUBLISHES_TO from
		// every sig.send()/send_robust() caller, so the signal dispatch surfaces
		// as a publisher → signal → handler diagram in /topology and /flows.
		signalEnts, signalRels := runDjangoSignalPubSub(classified)
		if len(signalEnts) > 0 || len(signalRels) > 0 {
			fmt.Fprintf(os.Stderr, "archigraph: django_signal_pubsub=%d topics %d edges\n",
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
			fmt.Fprintf(os.Stderr, "archigraph: serializer_meta_model_edges=%d\n", len(serializerModelRels))
		}
		pass2Rels = append(pass2Rels, serializerModelRels...)

		// Pass 2.6h — @receiver(sender=Model) → Model HANDLES_SIGNAL edges (#2578).
		// Explicit-sender @receiver decorators link the handler function to the
		// Model class so queries for a Model also surface its signal handlers.
		receiverSenderRels := runReceiverSenderEdges(classified)
		if len(receiverSenderRels) > 0 {
			fmt.Fprintf(os.Stderr, "archigraph: receiver_sender_edges=%d\n", len(receiverSenderRels))
		}
		pass2Rels = append(pass2Rels, receiverSenderRels...)

		// Pass 2.6i — FilterSet.Meta.model → Model REFERENCES edges (#2578).
		// django_filter FilterSet subclasses with Meta.model = X emit a REFERENCES
		// edge so that queries for a Model also return its filter classes.
		filterSetModelRels := runFilterSetMetaModelEdges(classified)
		if len(filterSetModelRels) > 0 {
			fmt.Fprintf(os.Stderr, "archigraph: filterset_meta_model_edges=%d\n", len(filterSetModelRels))
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
		// files (upvate pattern), applyDjangoRouteComposition never fires in
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
			fmt.Fprintf(os.Stderr, "archigraph: tests_multi_hop_http=%d edges\n", len(testsEdges))
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
			fmt.Fprintf(os.Stderr, "archigraph: tests_via_import=%d edges\n", len(importTestsEdges))
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
	releaseClassifiedASTs(classified)
	classified = nil
	runtime.GC()

	// Pass 5 — build document (deduped).
	trk.PhaseStart(progress.PhaseMaterialize, len(files), 0)
	doc := i.buildDocument(pass1Records, pass2Records, pass2Rels, pass3Records)

	// #2719 — incremental merge: when this run processed only the changed
	// subset of files, splice the unchanged-file portion of the previous
	// graph back into doc. Without this the doc that flows into Pass 4
	// (graph algorithms), external synthesis, embeddings, rename detection,
	// and the on-disk write would contain only the changed-file entities —
	// a tiny fraction of the real graph — silently corrupting the persisted
	// state for every CLI `archigraph rebuild <group> --incremental` invocation.
	//
	// Entities sourced from changed files are produced fresh in `doc`; we add
	// every previous entity whose SourceFile is NOT in the changed-set. We
	// also carry forward every previous relationship whose endpoints both
	// survive (either re-emitted in this run, or carried forward from the
	// previous unchanged-file portion). Edges that touched a changed-file
	// entity are dropped here — the resolver passes already attached above
	// produced replacements for them against the fresh entity IDs.
	if incrementalPrev != nil && incrementalChange != nil {
		mergeStats := mergeIncrementalPrevDoc(doc, incrementalPrev, incrementalChange)
		fmt.Fprintf(os.Stderr,
			"archigraph: incremental — carried forward %d entities and %d relationships from previous graph (dropped %d stale edges)\n",
			mergeStats.entitiesAdded, mergeStats.relsAdded, mergeStats.relsDropped)
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
	// ARCHIGRAPH_EMIT_MIGRATION_ENTITIES=1|true bypasses the prune.
	//
	// Runs AFTER mergeIncrementalPrevDoc so that entities carried forward
	// from a previous graph (which may predate the per-extractor prunes or
	// have been emitted by a Wave 3-5 path) are also caught.
	if ePruned, rPruned := extractors.PruneMigrationEntities(doc); ePruned > 0 && verbose() {
		fmt.Fprintf(os.Stderr, "migration-prune: dropped %d entities + %d relationships anchored to Django migration files\n", ePruned, rPruned)
	}

	// #4306 (Layer 1 of epic #4294): deterministic markdown documentation
	// ingestion. OPT-IN (--ingest-docs / ARCHIGRAPH_INGEST_DOCS), default OFF.
	// Runs AFTER buildDocument + incremental merge so the code-entity name
	// index used for exact-mention linking reflects the full, ID-stamped graph,
	// and the emitted doc/section nodes participate in Pass 4 graph algorithms
	// and the on-disk write. Fully deterministic: no LLM calls, no network.
	// When the flag is off this block is skipped entirely (zero overhead).
	if i.ingestDocs {
		mdFiles := ingest.DiscoverMarkdown(allFiles)
		ingRes := ingest.Ingest(absRepo, i.repoTag, mdFiles, doc.Entities)
		doc.Entities = append(doc.Entities, ingRes.Entities...)
		doc.Relationships = append(doc.Relationships, ingRes.Relationships...)
		if verbose() {
			fmt.Fprintf(os.Stderr,
				"ingest-docs: %d markdown files → %d documents, %d sections, %d mentions (deterministic, no LLM)\n",
				ingRes.FilesRead, ingRes.Documents, ingRes.Sections, ingRes.Mentions)
		}
	}
	// Drop the per-pass record slices now that buildDocument has produced
	// the merged + deduped graph.Entity / graph.Relationship slices. These
	// pass-level slices hold a copy of every entity's Properties /
	// Metadata maps and embedded Relationship slices; releasing them
	// before the resolver-classification + Pass 4 algorithms cuts the
	// peak by roughly the merged-set size on entity-dense repos.
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

	// Issue #1381 — stamp module="_external" on synthesised placeholder
	// entities that have no source_file (ext:* nodes from external.Synthesize,
	// and any other synthetic entities that buildDocument could not tag because
	// they were appended after the assembly loop).  EnsureModule skips entities
	// that already carry a "module" key, so well-sourced entities are unaffected.
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.SourceFile == "" {
			if e.Properties == nil {
				e.Properties = map[string]string{"module": "_external"}
			} else if _, ok := e.Properties["module"]; !ok {
				e.Properties["module"] = "_external"
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
		archigraphDir := daemon.StateDirForRepo(absRepo)
		repairs, rerr := enrichment.ReadRepairs(archigraphDir)
		if rerr != nil {
			fmt.Fprintf(os.Stderr,
				"archigraph: repair.json read error (continuing without): %v\n", rerr)
		}
		repairStats := enrichment.ApplyRepairs(doc, repairs,
			enrichment.ApplyRepairsOptions{RepoRoot: absRepo})
		if werr := enrichment.WriteRepairStats(archigraphDir, repairStats); werr != nil {
			fmt.Fprintf(os.Stderr,
				"archigraph: repair_stats.json write failed: %v\n", werr)
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
	// ARCHIGRAPH_VERBOSE=1.
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
			if r.Properties != nil {
				if v, ok := r.Properties["language"]; ok {
					lang = v
				} else if v, ok := r.Properties["lang"]; ok {
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
		// ARCHIGRAPH_BUG_EXTRACTOR_SAMPLES=N (writes N samples). Optional
		// ARCHIGRAPH_BUG_EXTRACTOR_OUT=/path/to/file (defaults to stderr).
		if n := bugExtractorSampleCount(); n > 0 {
			out := os.Stderr
			if p := strings.TrimSpace(os.Getenv("ARCHIGRAPH_BUG_EXTRACTOR_OUT")); p != "" {
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
		// Enabled with ARCHIGRAPH_BUG_RESOLVER_SAMPLES=N. Optional
		// ARCHIGRAPH_BUG_RESOLVER_OUT=/path overrides stderr.
		if n := bugResolverSampleCount(); n > 0 {
			out := os.Stderr
			if p := strings.TrimSpace(os.Getenv("ARCHIGRAPH_BUG_RESOLVER_OUT")); p != "" {
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
				"archigraph: tests-walkup helpers=%d derived=%d high_fan_in_skipped=%d duplicates_suppressed=%d\n",
				walkUpStats.HelperTargets, walkUpStats.DerivedEdges,
				walkUpStats.SkippedHighFanIn, walkUpStats.DuplicatesSuppressed)
		}
		// Re-sync Stats so subsequent passes see the correct edge count.
		doc.Stats.Relationships = len(doc.Relationships)
	}

	// Pass 4 — graph algorithms. Conceptually this runs "between" pass 3 and
	// pass 5, but it operates on the merged/deduped entity set so we run it
	// against the assembled Document and attach the per-entity attributes
	// in-place. The pass is intentionally skippable for cheap CI smoke runs.
	if !i.skipPasses[PassGraphAlgo] {
		// Issue #481 — gonum's BuildGraph assigns int64 node ids in slice
		// order, so sort entities/relationships on canonical ids before the
		// pass. Louvain, betweenness, articulation points, and surprise
		// edges all consume that mapping.
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
		trk.PhaseStart(progress.PhaseAlgorithms, len(files), doc.Stats.Entities)
		i.runPass4AlgorithmsWithProgress(doc, trk)
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
				"archigraph: process-flow entry_candidates=%d entries_used=%d "+
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
				"archigraph: event-flow seed_channels=%d event_flows=%d "+
					"step_edges=%d seed_edges=%d\n",
				efStats.SeedChannels, efStats.EventFlows,
				efStats.StepEdges, efStats.SeedEdges)
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
				"archigraph: module-agg modules=%d contains=%d depends_on=%d\n",
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
				fmt.Fprintf(os.Stderr, "archigraph: commit-couple skipped (%s)\n", ccStats.SkipReason)
			}
		} else if verbose() || ccStats.CoupledEdges > 0 {
			fmt.Fprintf(os.Stderr,
				"archigraph: commit-couple commits=%d candidate_pairs=%d files=%d edges=%d oversize_skipped=%d\n",
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
				fmt.Fprintf(os.Stderr, "archigraph: coupling skipped (no module graph)\n")
			}
		} else if verbose() || cpStats.ModulesAnnotated > 0 {
			fmt.Fprintf(os.Stderr,
				"archigraph: coupling modules=%d depends_on_edges=%d\n",
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
				"archigraph: dep-hygiene declared=%d used=%d unused=%d phantom=%d "+
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
				fmt.Fprintf(os.Stderr, "archigraph: shared-db skipped (no module graph or data access)\n")
			}
		} else if verbose() || sdStats.SharedTables > 0 {
			fmt.Fprintf(os.Stderr,
				"archigraph: shared-db tables=%d shared=%d data_access_annotated=%d coupling_edges=%d\n",
				sdStats.TablesConsidered, sdStats.SharedTables,
				sdStats.DataAccessAnnotated, sdStats.CouplingEdges)
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
				"archigraph: lib-boundary edges=%d first_party=%d third_party=%d "+
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
				"archigraph: migration-seq files=%d entities_annotated=%d precedes_edges=%d\n",
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
				"archigraph: migration-ops considered=%d tables=%d modifies_edges=%d access_converged=%d\n",
				moStats.OpsConsidered, moStats.TablesConverged,
				moStats.ModifiesEdges, moStats.AccessConvergedEdges)
		}
	}

	// Pass 6 — enrichment candidate emission (PORT-LLM / issue #15). Runs
	// AFTER Pass 4 so emitters can consult community/centrality/god-node
	// signals. Resolutions from prior runs are merged back onto entity
	// Properties BEFORE candidate emission, so previously agent-resolved
	// values are preserved AND emitters skip already-described entities.
	i.runPass6EmitEnrichmentCandidates(doc, absRepo)

	// Emit the final done event before the summary log line.
	trk.Done(len(files), doc.Stats.Entities)

	dur := time.Since(start)
	fmt.Fprintf(os.Stderr,
		"archigraph: processed=%d extracted=%d skipped=%d failed=%d "+
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

	// Incremental mode (issue #1339): persist the updated file-hash manifest
	// so the next incremental run can skip unchanged files. We update all
	// files (changed + unchanged) so the manifest stays complete even when
	// only a subset was re-extracted this run. Best-effort: a write failure
	// is logged but never fails the index.
	if i.incremental && diffManifest != nil {
		idiff.UpdateManifest(absRepo, allFiles, diffManifest)
		if err := idiff.SaveManifest(i.incrementalStateDir, absRepo, diffManifest); err != nil {
			fmt.Fprintf(os.Stderr, "archigraph: save incremental manifest: %v (non-fatal)\n", err)
		}
	}

	// Issue #2341 — sanity-check: warn when entities with a recognized source
	// extension have an empty Language tag. A non-zero count here means the
	// indexer pipeline dropped or never set the Language field for these files,
	// which would cause language-filtered queries (archigraph_find --language)
	// to silently skip them. This is non-fatal; the graph is still usable.
	warnEmptyLanguageEntities(doc)

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
		"archigraph: WARNING: %d entities have a recognized source extension but Language=\"\" "+
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
// relationship breakdown to stderr. Controlled by ARCHIGRAPH_VERBOSE=1.
func verbose() bool {
	v := strings.TrimSpace(os.Getenv("ARCHIGRAPH_VERBOSE"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// emitDispositionBreakdown prints the resolver-disposition tally and a few
// representative samples for the bug buckets. Triggered by ARCHIGRAPH_VERBOSE.
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

// bugExtractorSampleCount parses ARCHIGRAPH_BUG_EXTRACTOR_SAMPLES.
// Issue #89 — temporary diagnostic instrumentation, not a production knob.
func bugExtractorSampleCount() int {
	v := strings.TrimSpace(os.Getenv("ARCHIGRAPH_BUG_EXTRACTOR_SAMPLES"))
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
		lang := r.Properties["language"]
		if lang == "" {
			lang = r.Properties["lang"]
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

// bugResolverSampleCount parses ARCHIGRAPH_BUG_RESOLVER_SAMPLES.
// Issue #92 — temporary diagnostic instrumentation, not a production knob.
func bugResolverSampleCount() int {
	v := strings.TrimSpace(os.Getenv("ARCHIGRAPH_BUG_RESOLVER_SAMPLES"))
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
		lang := r.Properties["language"]
		if lang == "" {
			lang = r.Properties["lang"]
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
		fmt.Fprintf(w, "archigraph: %s_rels_by_source: (none)\n", label)
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
	fmt.Fprintf(w, "archigraph: %s_rels_by_source: %s\n", label, strings.Join(parts, " "))
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
	// archigraph_clusters and the graph Community view keep working between
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
func (i *Indexer) runPass4Algorithms(doc *graph.Document) {
	i.runPass4AlgorithmsWithProgress(doc, nil)
}

// runPass4AlgorithmsWithProgress is the instrumented variant of runPass4Algorithms.
// trk may be nil (treated as no-op). Emits an AlgorithmEvent at the entry and
// exit of each named algorithm.
func (i *Indexer) runPass4AlgorithmsWithProgress(doc *graph.Document, trk *progress.Tracker) {
	entityCount := doc.Stats.Entities

	if trk != nil {
		trk.AlgorithmEvent("Louvain+PageRank+Betweenness", entityCount)
	}
	res := graph.RunAlgorithms(doc.Entities, doc.Relationships)
	if trk != nil {
		trk.AlgorithmEvent("ArticulationPoints+SurpriseEdges", entityCount)
	}

	for k := range doc.Entities {
		e := &doc.Entities[k]
		if cid, ok := res.CommunityID[e.ID]; ok {
			cidCopy := cid
			e.CommunityID = &cidCopy
		}
		if c, ok := res.Centrality[e.ID]; ok {
			cCopy := c
			e.Centrality = &cCopy
		}
		if p, ok := res.PageRank[e.ID]; ok {
			pCopy := p
			e.PageRank = &pCopy
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

// runPass6EmitEnrichmentCandidates merges any prior agent-resolved
// enrichment values back onto entity Properties, then runs the registered
// CandidateEmitters and writes the resulting candidate list to
// <repo>/.archigraph/enrichment-candidates.json. The pass is no-op when
// PassEnrichment is in the skip set.
func (i *Indexer) runPass6EmitEnrichmentCandidates(doc *graph.Document, absRepo string) {
	if i.skipPasses[PassEnrichment] {
		return
	}
	if doc == nil {
		return
	}
	archigraphDir := daemon.StateDirForRepo(absRepo)

	// 1) Merge resolutions back onto entities BEFORE emitting. This both
	//    persists agent values across rebuilds and short-circuits emitters
	//    whose "already filled?" check looks at Properties (e.g.
	//    describe_entity skips entities that already have a description).
	resolutions := enrichment.ReadResolutions(archigraphDir)
	if len(resolutions) > 0 {
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

	// 2) Emit entity candidates. Rejected (subject_id, kind) pairs are dropped.
	cands := enrichment.CollectCandidatesSkippingRejected(
		doc, enrichment.DefaultEmitters(), archigraphDir,
	)

	// 2b) Emit name_community candidates (issue #426 Layer 2). One candidate
	//     per community that lacks an agent-resolved name.
	rej := enrichment.ReadRejections(archigraphDir)
	commCands := enrichment.CollectCommunityCandidates(doc, rej)
	if len(commCands) > 0 {
		cands = append(cands, commCands...)
		if verbose() {
			fmt.Fprintf(os.Stderr,
				"enrichment: collected %d name_community candidates\n", len(commCands))
		}
	}

	// 3) ADR-0015 phase-1 (#544) — repair_edge emission. Purely additive;
	//    gated behind --enable-repair-candidates so we can land the
	//    foundation without bumping bug-rate measurement noise.
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

	// 4) Issue #708 — dynamic-baseurl endpoint candidates. Emitted
	//    unconditionally (no resolver required) whenever there are
	//    consumer-side http_endpoint entities whose baseURL is
	//    runtime-determined (runtime_dynamic=true or dynamic_baseurl=true).
	//    These surface in archigraph_repairs action=list so an agent can
	//    annotate them with a curated baseURL hint.
	dynBaseURL := enrichment.CollectDynamicBaseURLCandidates(doc)
	if len(dynBaseURL) > 0 {
		cands = append(cands, dynBaseURL...)
		if verbose() {
			fmt.Fprintf(os.Stderr,
				"enrichment: collected %d dynamic_baseurl_endpoint candidates (#708)\n",
				len(dynBaseURL))
		}
	}

	if err := enrichment.WriteCandidates(archigraphDir, cands); err != nil {
		fmt.Fprintf(os.Stderr, "archigraph: enrichment candidate write failed: %v\n", err)
		return
	}
	// Issue #53: keep this log behind the verbose flag. The emit count is
	// useful for debugging but noisy on every CI run.
	if verbose() {
		fmt.Fprintf(os.Stderr,
			"archigraph: emitted %d enrichment candidates to %s\n",
			len(cands), filepath.Join(archigraphDir, "enrichment-candidates.json"))
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
		if content, err := os.ReadFile(abs); err == nil {
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

	publishTick := func(filesDone int, bytesSeen int64, currentFile string) {
		if trk == nil {
			return
		}
		trk.Tick(progress.PhaseExtractAST, filesDone, bytesSeen, currentFile, 0)
	}

	var wg sync.WaitGroup
	for w := 0; w < i.workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range tasks {
				size := int64(-1)
				if st, err := os.Stat(t.absPath); err == nil {
					size = st.Size()
				}
				cr := i.classifier.ClassifyWithSize(ctx, t.relPath, size)
				if cr.Skip || cr.Language == "" {
					mu.Lock()
					i.stats.skipped++
					mu.Unlock()
					n := int(atomic.AddInt64(&fileCounter, 1))
					if n%progress.TickEveryNFiles == 0 {
						publishTick(n, atomic.LoadInt64(&byteCounter), t.relPath)
					}
					continue
				}

				content, err := os.ReadFile(t.absPath)
				if err != nil {
					mu.Lock()
					i.stats.failed++
					mu.Unlock()
					n := int(atomic.AddInt64(&fileCounter, 1))
					if n%progress.TickEveryNFiles == 0 {
						publishTick(n, atomic.LoadInt64(&byteCounter), t.relPath)
					}
					continue
				}

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
					file.Tree = pr.Tree
					cf.tree = pr.Tree
				}

				if !runExtract {
					mu.Lock()
					classified = append(classified, cf)
					mu.Unlock()
					n := int(atomic.AddInt64(&fileCounter, 1))
					if n%progress.TickEveryNFiles == 0 {
						publishTick(n, atomic.LoadInt64(&byteCounter), t.relPath)
					}
					continue
				}

				ents, extractErr := extractors.Extract(ctx, file)
				relCount := 0
				for k := range ents {
					relCount += len(ents[k].Relationships)
				}
				mu.Lock()
				i.stats.processed++
				if extractErr != nil {
					if errors.Is(extractErr, extractors.ErrNoExtractorForLanguage) {
						i.stats.skipped++
					} else {
						i.stats.failed++
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

				n := int(atomic.AddInt64(&fileCounter, 1))
				if n%progress.TickEveryNFiles == 0 {
					publishTick(n, atomic.LoadInt64(&byteCounter), t.relPath)
				}
			}
		}()
	}
	wg.Wait()
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
				if cf.tree != nil {
					input.Tree = cf.tree
				}
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
	// scaffolding family (88 on upvate, 11.6% of all defs) has no inbound
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
		fmt.Fprintf(os.Stderr, "archigraph: django_admin_scaffolding_pruned=%d endpoints\n", dropped)
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
func (i *Indexer) stampEntityIDs(records []types.EntityRecord) {
	for k := range records {
		r := &records[k]
		if r.Name == "" {
			continue
		}
		r.ID = graph.EntityID(i.repoTag, r.Kind, r.Name, r.SourceFile)
	}
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

// frameworkClassKindPriority ranks framework-typed node kinds by how strongly
// they represent a class/type *declaration* (vs a nested artifact that merely
// shares the class name, e.g. a Django Meta `Constraint`). These are the only
// kinds eligible to be a fold *survivor*. When several share a fold source's
// source_file+name, the highest-priority kind wins. Higher value = stronger
// class signal. SCOPE.Component is intentionally absent: it is the generic AST
// node we fold AWAY into a framework-typed survivor (so a class with a View
// resolves to ONE node), and it is itself the survivor only when no
// framework-typed node exists (handled separately, never as a candidate here).
//
// Both bare kind names (emitted by Java/Django custom extractors) AND their
// "SCOPE."-prefixed forms (emitted by Kotlin, TypeScript, proto, and pattern
// extractors) must appear so that frameworkClassKindPriority[r.Kind] matches
// regardless of which extractor emitted the survivor. Issue #1700.
var frameworkClassKindPriority = map[string]int{
	// Bare names (Java/Django/Spring-boot custom extractors)
	"Model":          100,
	"View":           100,
	"Controller":     100,
	"Service":        100,
	"Middleware":     100,
	"Repository":     100,
	"Worker":         100,
	"Job":            100,
	"Topic":          100,
	"TestClass":      90,
	"Schema":         80,
	"Plugin":         80,
	"Implementation": 80,
	"Interface":      80,
	"Task":           70,

	// SCOPE.-prefixed equivalents (Kotlin extractor, proto extractor, NestJS
	// service_detector, and any other extractor that emits the canonical
	// "SCOPE.<Kind>" form for a class-like entity). Priorities mirror the bare
	// form so that the highest-fidelity named node always beats a generic shadow.
	// Issue #1700.
	"SCOPE.Service":     100,
	"SCOPE.View":        100, // #1727: View+Component fold (SCOPE-prefixed form)
	"SCOPE.Model":       100,
	"SCOPE.UIComponent": 100,
	"SCOPE.GrpcService": 90,
	"SCOPE.Schema":      80,
}

// frameworkClassKindCanonRank is the deterministic tiebreaker used when two or
// more framework-typed nodes share the SAME (source_file, name) AND the SAME
// frameworkClassKindPriority — i.e. one class symbol was double-emitted under
// two equally-strong kinds (the #3172/#3195 DRF/Django family: a Django
// `models.Model` class surfacing as BOTH "Model" AND "Controller"). Both are
// survivor candidates of priority 100, so without a tiebreaker the fold leaves
// two nodes for one class, violating the #1613 "every class → ONE node"
// invariant (TestClassShadowFold_NoLine0Shadows).
//
// Higher value = stronger canonical signal. The ranking prefers a kind that
// names what the class structurally *is* (its declaration role: a data Model, a
// rendered View, a Service, a Repository, a Schema) over a kind that names how
// the class is *dispatched/routed* ("Controller"). A Controller is the role
// most prone to spurious co-emission from route/endpoint synthesis on a class
// that is really a Model/View, so it is deliberately ranked below the structural
// declaration kinds. Kinds absent from this map rank as 0 (only consulted to
// break an exact-priority tie; the priority map remains the primary order).
var frameworkClassKindCanonRank = map[string]int{
	"Model": 5, "SCOPE.Model": 5,
	"View": 5, "SCOPE.View": 5,
	"Repository": 4,
	"Service":    4, "SCOPE.Service": 4,
	"Schema": 3, "SCOPE.Schema": 3,
	"Worker": 2, "Job": 2, "Topic": 2,
	"Middleware": 1,
	"Controller": 0,
}

func isShadowRecord(r *types.EntityRecord) bool {
	return r.Properties["provenance"] == "INFERRED_FROM_CLASS_HIERARCHY"
}

// classLikeComponentSubtypes are the SCOPE.Component subtypes that denote a
// class/type declaration (as opposed to subtype="file"/"import"/"module").
// Language AST subtypes ("class", "struct", …) are the primary set. Framework-
// injected subtypes from NestJS, Angular, Spring-boot, Quarkus, and similar
// extractors are included so that an inferential SCOPE.Component(subtype="service")
// node emitted alongside a real SCOPE.Service node for the same class symbol is
// recognised as a fold source and collapsed into the typed survivor. Issue #1700.
//
// "view" is included so that SCOPE.Component(subtype="view") nodes emitted
// alongside a real SCOPE.View (or bare "View") node for the same class symbol
// are recognised as fold sources. Issue #1727.
var classLikeComponentSubtypes = map[string]bool{
	// Language AST subtypes
	"class": true, "struct": true, "interface": true,
	"protocol": true, "trait": true, "behaviour": true,
	// Framework-injected subtypes (NestJS, Angular, Spring, Quarkus, …)
	"service": true, "controller": true, "repository": true,
	"guard": true, "interceptor": true, "pipe": true,
	"middleware": true, "resolver": true, "gateway": true,
	"worker": true, "job": true, "task": true,
	// View-type subtypes (Django CBV, MVC view layers, …) — #1727
	"view": true,
}

// isFoldSource reports whether r is a class-representation node that should be
// folded into a framework-typed node when one exists for the same symbol:
//   - the INFERRED_FROM_CLASS_HIERARCHY shadow emitted by the hierarchy pass, OR
//   - the generic SCOPE.Component class node emitted by the per-language AST
//     extractor (these two share an EntityID and pre-merge at assembly), OR
//   - a SCOPE.Component carrying Properties["role"]="class" (hierarchy pass
//     annotations on nodes where the language AST subtype is not yet in
//     classLikeComponentSubtypes — e.g. TypeScript/React class components
//     with a framework-injected role tag). Issue #1727.
//
// When NO framework-typed node exists, a fold source is kept as the single
// node for that class (it already carries a real line from its extractor).
func isFoldSource(r *types.EntityRecord) bool {
	if r.Name == "" {
		return false
	}
	if isShadowRecord(r) {
		return true
	}
	if r.Kind != "SCOPE.Component" {
		return false
	}
	// Subtype-based recognition: language AST and framework-injected subtypes.
	if classLikeComponentSubtypes[r.Subtype] {
		return true
	}
	// Role-property recognition: hierarchy pass sets role="class" on SCOPE.Component
	// nodes whose subtype is not yet in the allowlist (e.g. component, vue_component,
	// empty subtype from certain extractors). Issue #1727.
	return r.Properties["role"] == "class"
}

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
}

// filePathStem returns the base filename without extension, lower-cased.
// E.g. "src/components/LoginPage.tsx" → "loginpage".
func filePathStem(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := base[:len(base)-len(ext)]
	return strings.ToLower(stem)
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
	}
	fileBySourceFile := make(map[string]fileEnt)
	for k := range merged {
		r := &merged[k]
		if r.Kind == "SCOPE.Component" && r.Subtype == "file" && r.ID != "" {
			fileBySourceFile[r.SourceFile] = fileEnt{
				idx:  k,
				id:   r.ID,
				stem: filePathStem(r.SourceFile),
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

// buildDocument merges entity records from every pass, dedupes by stable
// graph-entity ID, resolves cross-file CALLS edges, then assembles the
// final on-disk document.
func (i *Indexer) buildDocument(pass1, pass2 []types.EntityRecord, pass2Rels []types.RelationshipRecord, pass3 []types.EntityRecord) *graph.Document {
	merged := make([]types.EntityRecord, 0, len(pass1)+len(pass2)+len(pass3))
	merged = append(merged, pass1...)
	merged = append(merged, pass2...)
	merged = append(merged, pass3...)

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
	var httpEndpointStats engine.ResolveHTTPEndpointStats
	merged, httpEndpointStats = engine.ResolveHTTPEndpointHandlers(merged)
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
	// NOTE (#4331): we intentionally call BuildIndex here, NOT the M5
	// per-module path resolve.BuildIndexFromModules (#2182/#2184). M5 is a
	// scale-only speed optimisation (pre-sized maps for big monorepos) and was
	// built to be edge-set-identical to BuildIndex, but the #4331
	// investigation found a CONCRETE divergence: M5 re-sorts entities by ID
	// within a module, and the platform-variant merge (#1818) in
	// byPackageOperation/byPackageComponent is order-sensitive for 3+
	// mutually-exclusive GOOS variants of the same (pkgDir, name). That yields
	// a different PlatformVariants fan-out topology, which clones a different
	// set of CALLS edges downstream (refs.go ReferencesEmbeddedWithAllowlist).
	// The guard test is TestM5_PlatformVariantParity_KnownDivergence in
	// internal/resolve/symbol_index_parity_test.go. Until M5 preserves
	// extraction order (or the variant merge is made order-independent) and
	// that test asserts parity, BuildIndex stays the production resolver.
	idx := resolve.BuildIndex(indexEntities)
	// #2049 — Django string-FK late-binding: rewrite scope:component:ref:python:*
	// stubs on REFERENCES edges with django_rel set, using app-label-aware
	// byPackageComponent lookup. Runs after BuildIndex (needs the component
	// index) and before ReferencesEmbeddedWithAllowlist (so rewritten hex IDs
	// are seen as resolved and counted correctly).
	djangoFKRewrites := idx.ResolveDjangoStringFKRefs(merged)
	if djangoFKRewrites > 0 {
		fmt.Fprintf(os.Stderr, "resolver: django-string-fk rewrote=%d FK REFERENCES stubs\n", djangoFKRewrites)
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
	allow := resolve.ExternalAllowlist(external.IsKnownExternalPackage)
	embStats := resolve.ReferencesEmbeddedWithAllowlist(merged, idx, allow)
	standStats := resolve.ReferencesWithAllowlist(pass2Rels, idx, allow)
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
	prunedMerged, pruneOrphanRels, pruneStats := resolve.PruneImportPlaceholders(merged)
	merged = prunedMerged
	if pruneStats.Considered > 0 {
		fmt.Fprintf(os.Stderr,
			"import-placeholder-prune: considered=%d pruned=%d rels_hoisted=%d rels_orphaned=%d kept=%d edge_toid_rewrites=%d\n",
			pruneStats.Considered, pruneStats.Pruned, pruneStats.RelsHoisted,
			pruneStats.RelsOrphaned, pruneStats.PlaceholderKept, pruneStats.EdgeToIDRewrites)
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
	// ARCHIGRAPH_DISABLE_1613_FOLD is a verification escape hatch (compare
	// folded vs unfolded graphs on the same binary); unset in production.
	if os.Getenv("ARCHIGRAPH_DISABLE_1613_FOLD") == "" {
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
	// ARCHIGRAPH_DISABLE_1727_FILE_FOLD is the verification escape hatch.
	if os.Getenv("ARCHIGRAPH_DISABLE_1727_FILE_FOLD") == "" {
		var fileStats foldFileComponentStats
		merged, pass2Rels, fileStats = i.foldFileComponentDuplicates(merged, pass2Rels)
		fmt.Fprintf(os.Stderr,
			"foldFileComponentDuplicates: observed=%d folded=%d (edges_repointed=%d)\n",
			fileStats.Observed, fileStats.Folded, fileStats.EdgesRepointed)
	}

	// Pass 3.7 — Bazel resolver overlay (#2183 / M6).
	// Cross-references BAZEL_DEPENDS_ON (declared BUILD deps) against
	// CALLS/IMPORTS (inferred runtime deps) and annotates each declared
	// edge with "declared+used", "declared_unused", or emits a new
	// "undeclared_used" edge for call crossings that lack a BUILD dep.
	// Runs after all entity-ID rewrites and fold passes so IDs are stable.
	{
		mergedSlice := make([]types.EntityRecord, 0, len(merged))
		for k := range merged {
			mergedSlice = append(mergedSlice, merged[k])
		}
		bazelOverlay := resolve.RunBazelOverlay(mergedSlice, pass2Rels)
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
				Properties:    r.Properties,
				Confidence:    r.Confidence, // Phase 1C (#2769).
			})
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
				ID:         relID,
				FromID:     fromID,
				ToID:       toID,
				Kind:       rel.Kind,
				Properties: rel.Properties,
				Confidence: rel.Confidence, // Phase 1C (#2769).
			})
		}
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
			ID:         relID,
			FromID:     fromID,
			ToID:       toID,
			Kind:       rel.Kind,
			Properties: rel.Properties,
			Confidence: rel.Confidence, // Phase 1C (#2769).
		})
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
			ID:         relID,
			FromID:     rel.FromID,
			ToID:       rel.ToID,
			Kind:       rel.Kind,
			Properties: rel.Properties,
		})
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
			ID:         relID,
			FromID:     rel.FromID,
			ToID:       rel.ToID,
			Kind:       rel.Kind,
			Properties: rel.Properties,
		})
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
//   - Adds every prev relationship whose endpoints are both alive — meaning
//     both endpoints are either already in doc or are being carried forward
//     in this same merge. Edges with an endpoint sourced from a changed file
//     are dropped: the per-file extraction pass already re-emitted the
//     authoritative version of those edges against the fresh entity IDs.
//   - Pure-string-keyed relationships pointing at synthetic / external nodes
//     (kind="ext:*", or any entity ID present in doc.Entities) are carried
//     forward normally — the surviving-endpoint check covers them.
//
// The function is intentionally separate from internal/extractors.TryIncremental:
// TryIncremental owns the daemon's fast in-place reindex (Path A), while this
// helper owns the CLI's full-pipeline incremental rebuild (Path B). Sharing
// extraction logic would invert the dependency (cmd → internal/extractors
// already; internal/extractors → cmd would be a cycle), so the merge step is
// duplicated by design — both implementations are exercised by their own
// regression tests in this package and internal/extractors.
func mergeIncrementalPrevDoc(doc *graph.Document, prev *graph.Document, changedFiles map[string]bool) incrementalMergeStats {
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

	// First pass: copy across entities sourced from UNCHANGED files. Track
	// every entity ID that will survive the merge so we can filter
	// relationships in the second pass.
	survivingIDs := make(map[string]bool, len(docEntityIDs)+len(prev.Entities))
	for id := range docEntityIDs {
		survivingIDs[id] = true
	}
	for _, e := range prev.Entities {
		// Entities without a source file (e.g. ext:* synthetics) are
		// regenerated downstream by external.Synthesize against the merged
		// graph; skip them here so we do not double-emit.
		if e.SourceFile == "" {
			continue
		}
		if changedFiles[filepath.ToSlash(e.SourceFile)] {
			continue
		}
		if docEntityIDs[e.ID] {
			continue
		}
		doc.Entities = append(doc.Entities, e)
		survivingIDs[e.ID] = true
		stats.entitiesAdded++
	}

	// Second pass: copy across relationships whose endpoints are both alive
	// in the merged graph. Edges incident to a changed-file entity are
	// dropped — the fresh extraction has already emitted the canonical
	// replacements against the new entity IDs in this run.
	docRelIDs := make(map[string]bool, len(doc.Relationships))
	for _, r := range doc.Relationships {
		docRelIDs[r.ID] = true
	}
	for _, r := range prev.Relationships {
		if !survivingIDs[r.FromID] || !survivingIDs[r.ToID] {
			stats.relsDropped++
			continue
		}
		if docRelIDs[r.ID] {
			continue
		}
		doc.Relationships = append(doc.Relationships, r)
		stats.relsAdded++
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

// releaseClassifiedASTs explicitly drops the tree-sitter parse trees + source
// bytes attached to each classifiedFile entry. Called after the last
// extractor pass (Pass 3 cross-language) finishes. The tree-sitter Tree.Close
// path releases the C-side tree allocation that runtime.GC cannot reclaim
// because the goroutine handle is reference-counted via CGo, not via the Go
// allocator. Setting .content to nil drops the per-file source-byte buffer
// the resolver no longer needs. Issue #633.
func releaseClassifiedASTs(classified []classifiedFile) {
	for k := range classified {
		cf := &classified[k]
		if cf.tree != nil {
			cf.tree.Close()
			cf.tree = nil
		}
		cf.content = nil
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
