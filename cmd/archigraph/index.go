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

	"github.com/cajasmota/archigraph/internal/classifier"
	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/extract"
	"github.com/cajasmota/archigraph/internal/daemon/walk"
	"github.com/cajasmota/archigraph/internal/engine"
	"github.com/cajasmota/archigraph/internal/enrichment"
	"github.com/cajasmota/archigraph/internal/external"
	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors"
	"github.com/cajasmota/archigraph/internal/extractors/cross"
	pyextr "github.com/cajasmota/archigraph/internal/extractors/python"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/fbwriter"
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
	PassEnrichment    = "enrichment"     // Pass 6: emit enrichment candidates
	PassProcessFlow   = "process-flow"   // Pass 7: process-flow BFS over CALLS (#724)
)

// allPassNames is used to validate --skip-pass entries.
var allPassNames = []string{
	PassExtract, PassFramework, PassCrossLang, PassGraphAlgo, PassBuildDocument, PassEnrichment, PassProcessFlow,
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
	publisher  progress.Publisher
	groupSlug  string // forwarded to every Event.GroupSlug
	repoSlug   string // forwarded to every Event.RepoSlug; defaults to repoTag

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

	doc, err := idx.Run(context.Background(), absRepo)
	if err != nil {
		return err
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
		}

		// Sidecar: corpus-level metrics for `archigraph doctor` and the future
		// MCP `graph_stats` tool. Only written when Pass 4 actually ran.
		if doc.AlgorithmStats != nil {
			side := &graph.GraphStatsSidecar{
				Version:            1,
				ComputedAt:         deterministicGeneratedAt(),
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
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(js)
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

	walkOpts := &walk.Options{
		AdditionalSkipDirs: i.additionalSkipDirs,
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
		pass2Records, pass2Rels, err = i.runPass25FrameworkRules(ctx, absRepo, classified)
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
		for k := range classified {
			cf := &classified[k]
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
	return doc, nil
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
func (i *Indexer) runPass25FrameworkRules(ctx context.Context, absRepo string, classified []classifiedFile) ([]types.EntityRecord, []types.RelationshipRecord, error) {
	if i.skipPasses[PassFramework] {
		return nil, nil, nil
	}

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
	return engine.ApplyDjangoAdminRoutes(pyPaths, reader)
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
	if httpEndpointStats.Synthetics > 0 {
		fmt.Fprintf(os.Stderr,
			"http-endpoint-resolve: synthetics=%d handler_resolved=%d handler_dropped=%d no_handler_prop=%d caller_resolved=%d caller_unresolved=%d calls_linked=%d calls_unresolved=%d\n",
			httpEndpointStats.Synthetics,
			httpEndpointStats.HandlerResolved,
			httpEndpointStats.HandlerDropped,
			httpEndpointStats.NoHandlerProp,
			httpEndpointStats.CallerResolved,
			httpEndpointStats.CallerUnresolved,
			httpEndpointStats.CallsLinked,
			httpEndpointStats.CallsUnresolved)
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

	idx := resolve.BuildIndex(merged)
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

	entities := make([]graph.Entity, 0, len(merged))
	relationships := make([]graph.Relationship, 0)

	seenEntity := make(map[string]bool, len(merged))
	seenRel := make(map[string]bool)

	for k := range merged {
		r := &merged[k]
		id := graph.EntityID(i.repoTag, r.Kind, r.Name, r.SourceFile)
		if !seenEntity[id] {
			seenEntity[id] = true
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
	for _, cf := range classified {
		if cf.language != "java" {
			continue
		}
		contentByPath[cf.relPath] = cf.content
		javaPaths = append(javaPaths, cf.relPath)
	}
	if len(javaPaths) == 0 {
		return nil
	}
	reader := func(relPath string) []byte {
		return contentByPath[relPath]
	}
	return engine.ApplyJavaAnnotationRoutes(javaPaths, reader)
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
