package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/classifier"
	"github.com/cajasmota/archigraph/internal/engine"
	"github.com/cajasmota/archigraph/internal/enrichment"
	"github.com/cajasmota/archigraph/internal/external"
	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors"
	"github.com/cajasmota/archigraph/internal/extractors/cross"
	"github.com/cajasmota/archigraph/internal/graph"
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
)

// allPassNames is used to validate --skip-pass entries.
var allPassNames = []string{
	PassExtract, PassFramework, PassCrossLang, PassGraphAlgo, PassBuildDocument, PassEnrichment,
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
func Index(repoPath, outPath, repoTag string, skipPasses []string, pretty bool, jsonStats bool) error {
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
		outPath = filepath.Join(absRepo, ".archigraph", "graph.json")
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

	doc, err := idx.Run(context.Background(), absRepo)
	if err != nil {
		return err
	}

	if !skipSet[PassBuildDocument] {
		if err := graph.WriteAtomic(outPath, doc, pretty); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "archigraph: wrote %s\n", outPath)

		// Sidecar: corpus-level metrics for `archigraph doctor` and the future
		// MCP `graph_stats` tool. Only written when Pass 4 actually ran.
		if doc.AlgorithmStats != nil {
			side := &graph.GraphStatsSidecar{
				Version:            1,
				ComputedAt:         time.Now().UTC(),
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
		if err := emitJSONStats(os.Stdout, idx, doc); err != nil {
			return fmt.Errorf("emit json stats: %w", err)
		}
	}
	return nil
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

// emitJSONStats writes a JSONStats record (one line, no trailing whitespace)
// to w. Used by `archigraph index --json-stats`.
func emitJSONStats(w *os.File, idx *Indexer, doc *graph.Document) error {
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

	files, err := walkRepo(absRepo)
	if err != nil {
		return nil, fmt.Errorf("walk repo: %w", err)
	}
	i.stats.files = len(files)
	fmt.Fprintf(os.Stderr, "archigraph: discovered %d candidate files in %s\n", len(files), absRepo)

	// Pass 1 — per-language AST extraction.
	pass1Records, classified, err := i.runPass1Extract(ctx, absRepo, files)
	if err != nil {
		return nil, fmt.Errorf("pass 1: %w", err)
	}
	i.stats.pass1Rels = countEmbeddedRels(pass1Records)

	// Pass 2.5 — YAML-driven framework rules.
	pass2Records, pass2Rels, err := i.runPass25FrameworkRules(ctx, classified)
	if err != nil {
		return nil, fmt.Errorf("pass 2.5: %w", err)
	}
	i.stats.pass2Rels = len(pass2Rels) + countEmbeddedRels(pass2Records)

	// Pass 3 — cross-language extractors.
	pass3Records, err := i.runPass3CrossLang(ctx, classified)
	if err != nil {
		return nil, fmt.Errorf("pass 3: %w", err)
	}
	i.stats.pass3Rels = countEmbeddedRels(pass3Records)

	// Pass 5 — build document (deduped).
	doc := i.buildDocument(pass1Records, pass2Records, pass2Rels, pass3Records)

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
	}

	// Pass 4 — graph algorithms. Conceptually this runs "between" pass 3 and
	// pass 5, but it operates on the merged/deduped entity set so we run it
	// against the assembled Document and attach the per-entity attributes
	// in-place. The pass is intentionally skippable for cheap CI smoke runs.
	if !i.skipPasses[PassGraphAlgo] {
		i.runPass4Algorithms(doc)
	}

	// Pass 6 — enrichment candidate emission (PORT-LLM / issue #15). Runs
	// AFTER Pass 4 so emitters can consult community/centrality/god-node
	// signals. Resolutions from prior runs are merged back onto entity
	// Properties BEFORE candidate emission, so previously agent-resolved
	// values are preserved AND emitters skip already-described entities.
	i.runPass6EmitEnrichmentCandidates(doc, absRepo)

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
		{ToID: stub, ToOriginal: stub},
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
	res := graph.RunAlgorithms(doc.Entities, doc.Relationships)

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
	archigraphDir := filepath.Join(absRepo, ".archigraph")

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
	}

	// 2) Emit candidates. Rejected (subject_id, kind) pairs are dropped.
	cands := enrichment.CollectCandidatesSkippingRejected(
		doc, enrichment.DefaultEmitters(), archigraphDir,
	)

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
	if i.skipPasses[PassExtract] {
		// Even when Pass 1 is skipped we still need to classify+read so
		// downstream passes have file content. Run the worker loop in
		// classification-only mode.
		classified, _ := i.classifyAndRead(ctx, absRepo, files, false)
		return nil, classified, nil
	}
	classified, records := i.classifyAndRead(ctx, absRepo, files, true)
	return records, classified, nil
}

// classifyAndRead is the shared worker pool used by Pass 1. When runExtract
// is true it also dispatches to per-language extractors and accumulates
// EntityRecords. The classifiedFile slice is always populated for files that
// survived classification, so other passes can reuse the parse tree + bytes.
func (i *Indexer) classifyAndRead(ctx context.Context, absRepo string, files []string, runExtract bool) ([]classifiedFile, []types.EntityRecord) {
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
					continue
				}

				content, err := os.ReadFile(t.absPath)
				if err != nil {
					mu.Lock()
					i.stats.failed++
					mu.Unlock()
					continue
				}

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
				}

				if pr, perr := i.parser.Parse(ctx, content, cr.Language); perr == nil && pr != nil {
					file.Tree = pr.Tree
					cf.tree = pr.Tree
				}

				if !runExtract {
					mu.Lock()
					classified = append(classified, cf)
					mu.Unlock()
					continue
				}

				ents, err := extractors.Extract(ctx, file)
				// Per-language relationship count for the verbose breakdown.
				relCount := 0
				for k := range ents {
					relCount += len(ents[k].Relationships)
				}
				mu.Lock()
				i.stats.processed++
				if err != nil {
					i.stats.failed++
				} else {
					i.stats.extracted++
					allRecords = append(allRecords, ents...)
					if i.stats.pass1RelsByLang != nil {
						i.stats.pass1RelsByLang[cr.Language] += relCount
					}
				}
				classified = append(classified, cf)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return classified, allRecords
}

// runPass25FrameworkRules applies the YAML rule engine to every classified
// file. Returns extra entity records (from source_patterns) plus standalone
// relationship records (from relationship_rules).
func (i *Indexer) runPass25FrameworkRules(ctx context.Context, classified []classifiedFile) ([]types.EntityRecord, []types.RelationshipRecord, error) {
	if i.skipPasses[PassFramework] {
		return nil, nil, nil
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
	return entities, rels, nil
}

// runPass3CrossLang runs every registered cross-language extractor against
// every classified file. The cross extractors short-circuit on languages
// they don't handle, so the cost on irrelevant files is small.
//
// This is the critical fix flagged by the PORT-1 review: the
// internal/extractors/cross/* packages had ZERO callers before this pass.
func (i *Indexer) runPass3CrossLang(ctx context.Context, classified []classifiedFile) ([]types.EntityRecord, error) {
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

	// After collecting raw cross-extractor output, resolve bare-name
	// to_id references against the union of Pass 1 + Pass 2.5 + Pass 3
	// entities. Resolution is a best-effort same-name lookup keyed by
	// (kind, name). If no match is found we leave the bare name in place
	// — this preserves the truly-external (stdlib) signal the report calls
	// out — but cross-file user calls now resolve to a stable entity ID.
	return out, nil
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

// buildDocument merges entity records from every pass, dedupes by stable
// graph-entity ID, resolves cross-file CALLS edges, then assembles the
// final on-disk document.
func (i *Indexer) buildDocument(pass1, pass2 []types.EntityRecord, pass2Rels []types.RelationshipRecord, pass3 []types.EntityRecord) *graph.Document {
	merged := make([]types.EntityRecord, 0, len(pass1)+len(pass2)+len(pass3))
	merged = append(merged, pass1...)
	merged = append(merged, pass2...)
	merged = append(merged, pass3...)

	// Stamp deterministic entity IDs onto every record so the resolver can
	// look them up by (kind, name).
	i.stampEntityIDs(merged)

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

	return &graph.Document{
		Version:        graph.SchemaVersion,
		GeneratedAt:    time.Now().UTC(),
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

// walkRepo returns repo-relative file paths, skipping common directories
// that should never be indexed (.git, node_modules, vendor, etc.).
func walkRepo(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		base := d.Name()
		if d.IsDir() {
			if isSkippedDir(base) {
				return filepath.SkipDir
			}
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

func isSkippedDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn",
		"node_modules", "vendor", "__pycache__",
		".archigraph", ".venv", "venv",
		".idea", ".vscode",
		"dist", "build", "target", ".next", ".nuxt",
		"coverage", ".pytest_cache", ".mypy_cache":
		return true
	}
	if strings.HasPrefix(name, ".") && len(name) > 1 {
		// hidden dirs: skip by default (.terraform, .gradle, .m2, etc.)
		return true
	}
	return false
}
