// Command embed-bench is the embedding-quality benchmark harness for #462.
//
// It loads an already-indexed graph (graph.fb in a state directory) plus a
// queries.json test set, runs each configured embedding backend against the
// queries, and emits a Markdown report comparing Recall@5 / Recall@10 / MRR,
// indexing time, on-disk vector size, and query latency. A BM25-only run is
// included as a baseline floor.
//
// Backends:
//
//   - bm25       — BM25-only baseline (#460).
//   - builtin    — bundled all-MiniLM-L6-v2 via hugot+simplego (#461, requires
//     building this tool with `-tags simplego`).
//   - http:<url> — any OpenAI-compatible /v1/embeddings endpoint. The model is
//     taken from $ARCHIGRAPH_EMBEDDING_MODEL (default
//     "nomic-embed-text"). Examples:
//     http:http://127.0.0.1:11434/v1       (Ollama)
//     http:https://api.openai.com/v1       (OpenAI; needs $OPENAI_API_KEY)
//
// HTTP backends that are unreachable are SKIPPED rather than failing the run —
// the report records "skipped: no endpoint" so the harness is portable to
// developer machines without Ollama / OpenAI access.
//
// Usage:
//
//	embed-bench \
//	    -state-dir ~/.archigraph/store/<slug> \
//	    -repo-root /path/to/source \
//	    -queries  docs/verify2/embedding-harness/queries.json \
//	    -backends bm25,builtin,http:http://127.0.0.1:11434/v1 \
//	    -out      docs/verify2/embedding-backend-benchmark.md
//
// The harness writes per-backend embeddings to a temp dir so it does NOT
// touch the daemon's live ~/.archigraph/store/<slug>/embeddings.bin sidecar.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/embed"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/mcp"
)

// queryMatcher matches a graph entity by SourceFile suffix/substring and Name.
// This is robust against ID-scheme churn and allows hand-authored ground truth.
type queryMatcher struct {
	SourceFile string `json:"source_file"`
	Name       string `json:"name"`
}

type queryCase struct {
	ID       string         `json:"id"`
	Question string         `json:"question"`
	Truth    []queryMatcher `json:"truth"`
}

type querySet struct {
	Version int         `json:"version"`
	Corpus  string      `json:"corpus"`
	Queries []queryCase `json:"queries"`
}

// backendSpec is the parsed -backends element ("bm25", "builtin", "http:<url>").
type backendSpec struct {
	kind string // "bm25" | "builtin" | "http"
	url  string // for http
	tag  string // human label
}

// perBackendResult is the per-query outcome aggregate for one backend.
type perBackendResult struct {
	tag          string
	skipped      bool
	skipReason   string
	dims         int
	totalEnts    int    // entities considered embeddable
	embedded     int    // entities actually embedded
	embedSecs    float64
	queryLatency time.Duration // mean per-query encode + search
	vectorBytes  int64         // on-disk embeddings.bin size
	memRSSDelta  int64         // approximate Alloc bytes growth during embed

	perQuery map[string]queryOutcome // by query id
}

type queryOutcome struct {
	hits        []hitRow // top-10 best hits, in rank order
	recallAt5   float64
	recallAt10  float64
	mrr         float64
	truthCount  int
	encodeMicro int64 // µs to encode the query (vector backends only)
	searchMicro int64 // µs to run BM25 + vector cosine sort
}

type hitRow struct {
	rank       int
	name       string
	sourceFile string
	score      float64
	matched    bool
}

func main() {
	var (
		stateDir   = flag.String("state-dir", "", "directory containing graph.fb (e.g. ~/.archigraph/store/<slug>)")
		repoRoot   = flag.String("repo-root", "", "absolute path to the source repo (for snippet extraction)")
		queriesIn  = flag.String("queries", "docs/verify2/embedding-harness/queries.json", "queries.json path")
		backendsIn = flag.String("backends", "bm25,builtin", "comma-separated backend specs: bm25 | builtin | http:<base-url>")
		outPath    = flag.String("out", "docs/verify2/embedding-backend-benchmark.md", "markdown report output path")
		topK       = flag.Int("topk", 10, "how many top hits to retain per query (>=10 for Recall@10)")
	)
	flag.Parse()

	if *stateDir == "" || *repoRoot == "" {
		fmt.Fprintln(os.Stderr, "usage: embed-bench -state-dir <dir> -repo-root <dir> -queries <file> -backends bm25,builtin[,http:<url>] -out <md>")
		os.Exit(2)
	}
	if err := run(*stateDir, *repoRoot, *queriesIn, *backendsIn, *outPath, *topK); err != nil {
		fmt.Fprintln(os.Stderr, "embed-bench: ", err)
		os.Exit(1)
	}
}

func run(stateDir, repoRoot, queriesPath, backendsCSV, outPath string, topK int) error {
	if topK < 10 {
		topK = 10
	}

	// 1. Load graph (graph.fb / graph.json).
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		return fmt.Errorf("load graph: %w", err)
	}
	fmt.Fprintf(os.Stderr, "loaded graph: entities=%d relationships=%d\n", len(doc.Entities), len(doc.Relationships))

	// 2. Load queries.
	qs, err := loadQueries(queriesPath)
	if err != nil {
		return fmt.Errorf("load queries: %w", err)
	}
	fmt.Fprintf(os.Stderr, "loaded %d queries from %s\n", len(qs.Queries), queriesPath)

	// 3. Parse backend specs.
	specs, err := parseBackends(backendsCSV)
	if err != nil {
		return err
	}

	// 4. Build BM25 once (every backend uses the BM25-only column as a baseline
	//    in the report, plus the actual vector backends search BM25 separately
	//    for fused-RRF-style sanity — but #462 measures backends in isolation;
	//    RRF fusion is a follow-up benchmark).
	bm := mcp.BuildBM25(doc)

	// 5. For each backend, run the benchmark.
	results := make([]*perBackendResult, 0, len(specs))
	for _, sp := range specs {
		fmt.Fprintf(os.Stderr, "\n=== backend: %s ===\n", sp.tag)
		r := runBackend(sp, doc, bm, qs, repoRoot, topK)
		results = append(results, r)
	}

	// 6. Emit report.
	if err := writeReport(outPath, doc, qs, results, stateDir, repoRoot); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	fmt.Fprintf(os.Stderr, "\nreport: %s\n", outPath)
	return nil
}

// loadQueries reads queries.json.
func loadQueries(path string) (*querySet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var qs querySet
	if err := json.Unmarshal(data, &qs); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for i, q := range qs.Queries {
		if q.Question == "" {
			return nil, fmt.Errorf("query %d (%q) missing question", i, q.ID)
		}
		if len(q.Truth) == 0 {
			return nil, fmt.Errorf("query %d (%q) has no ground truth", i, q.ID)
		}
	}
	return &qs, nil
}

// parseBackends splits "bm25,builtin,http:http://..." into specs.
func parseBackends(csv string) ([]backendSpec, error) {
	parts := strings.Split(csv, ",")
	out := make([]backendSpec, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		switch {
		case p == "bm25":
			out = append(out, backendSpec{kind: "bm25", tag: "bm25"})
		case p == "builtin":
			out = append(out, backendSpec{kind: "builtin", tag: "builtin:all-MiniLM-L6-v2"})
		case strings.HasPrefix(p, "http:"):
			u := strings.TrimPrefix(p, "http:")
			model := os.Getenv("ARCHIGRAPH_EMBEDDING_MODEL")
			if model == "" {
				model = "nomic-embed-text"
			}
			out = append(out, backendSpec{kind: "http", url: u, tag: "http:" + model + "@" + shortHost(u)})
		default:
			return nil, fmt.Errorf("unknown backend spec %q", p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no backends configured")
	}
	return out, nil
}

func shortHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host
}

// runBackend benchmarks one backend end-to-end.
func runBackend(sp backendSpec, doc *graph.Document, bm *mcp.BM25Index, qs *querySet, repoRoot string, topK int) *perBackendResult {
	r := &perBackendResult{tag: sp.tag, perQuery: map[string]queryOutcome{}}

	if sp.kind == "bm25" {
		// BM25-only baseline: no embedding step.
		runBM25Only(r, doc, bm, qs, topK)
		return r
	}

	// Vector backends: probe endpoint first (for http), then embed corpus.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	be, err := buildBackend(ctx, sp)
	if err != nil {
		r.skipped = true
		r.skipReason = err.Error()
		fmt.Fprintf(os.Stderr, "  skipped: %s\n", err)
		return r
	}
	defer be.Close()
	r.dims = be.Dims()

	tmpDir, err := os.MkdirTemp("", "embed-bench-")
	if err != nil {
		r.skipped = true
		r.skipReason = "mktemp: " + err.Error()
		return r
	}
	defer os.RemoveAll(tmpDir)

	var memBefore runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	t0 := time.Now()
	store, embedRes, err := embed.EmbedDocument(ctx, doc, repoRoot, tmpDir, be)
	r.embedSecs = time.Since(t0).Seconds()
	if err != nil {
		r.skipped = true
		r.skipReason = "embed corpus: " + err.Error()
		return r
	}

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	if memAfter.Alloc > memBefore.Alloc {
		r.memRSSDelta = int64(memAfter.Alloc - memBefore.Alloc)
	}

	r.totalEnts = embedRes.Total
	r.embedded = embedRes.Embedded

	if fi, err := os.Stat(embed.StorePath(tmpDir)); err == nil {
		r.vectorBytes = fi.Size()
	}

	// Build entity-id → *Entity for hit resolution.
	byID := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}

	var totalLatency time.Duration
	for _, q := range qs.Queries {
		tEnc := time.Now()
		qvecs, eerr := be.Embed(ctx, []string{q.Question})
		encLat := time.Since(tEnc)
		if eerr != nil || len(qvecs) == 0 {
			r.perQuery[q.ID] = queryOutcome{truthCount: len(q.Truth)}
			continue
		}
		tSearch := time.Now()
		semHits := store.Search(qvecs[0], topK)
		searchLat := time.Since(tSearch)

		hits := make([]hitRow, 0, len(semHits))
		for i, h := range semHits {
			ent := byID[h.ID]
			if ent == nil {
				continue
			}
			hits = append(hits, hitRow{
				rank:       i + 1,
				name:       ent.Name,
				sourceFile: ent.SourceFile,
				score:      h.Score,
				matched:    matchesAny(ent, q.Truth),
			})
		}
		outc := scoreHits(hits, len(q.Truth))
		outc.encodeMicro = encLat.Microseconds()
		outc.searchMicro = searchLat.Microseconds()
		r.perQuery[q.ID] = outc
		totalLatency += encLat + searchLat
	}
	if n := len(qs.Queries); n > 0 {
		r.queryLatency = totalLatency / time.Duration(n)
	}
	return r
}

// buildBackend constructs the embed.Backend for a spec, with a fast
// reachability probe for http backends (skip-gracefully behaviour).
func buildBackend(ctx context.Context, sp backendSpec) (embed.Backend, error) {
	switch sp.kind {
	case "builtin":
		if !embed.BuiltinCompiledIn() {
			return nil, fmt.Errorf("builtin backend not compiled in (rebuild with `-tags simplego`)")
		}
		return embed.NewBackend(ctx, embed.Config{Backend: embed.BackendBuiltin})
	case "http":
		// Probe the endpoint host:port; if it's not listening, skip.
		if err := probeHTTP(sp.url); err != nil {
			return nil, fmt.Errorf("no endpoint: %v", err)
		}
		model := os.Getenv("ARCHIGRAPH_EMBEDDING_MODEL")
		if model == "" {
			model = "nomic-embed-text"
		}
		cfg := embed.Config{
			Backend: embed.BackendHTTP,
			HTTP: embed.HTTPConfig{
				URL:    sp.url,
				Model:  model,
				APIKey: os.Getenv("ARCHIGRAPH_EMBEDDING_API_KEY"),
			},
		}
		be, err := embed.NewBackend(ctx, cfg)
		if err != nil {
			return nil, err
		}
		// Probe-embed once so the backend learns its true dimensionality from
		// the server response (some models — nomic-embed-text=768, jina-v2=768,
		// text-embedding-3-large=3072 — don't match the 384 default). The store
		// is sized by backend.Dims() before the corpus embed loop runs.
		if _, perr := be.Embed(ctx, []string{"probe"}); perr != nil {
			_ = be.Close()
			return nil, fmt.Errorf("probe embed: %v", perr)
		}
		return be, nil
	default:
		return nil, fmt.Errorf("unknown backend kind %q", sp.kind)
	}
}

// probeHTTP tries to TCP-dial the host:port of url, with a 2s timeout, then
// makes a HEAD/GET so we know something is actually listening before we pay
// the embed-corpus cost.
func probeHTTP(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		switch u.Scheme {
		case "https":
			host += ":443"
		default:
			host += ":80"
		}
	}
	c, err := net.DialTimeout("tcp", host, 2*time.Second)
	if err != nil {
		return err
	}
	_ = c.Close()
	// Best-effort GET (some servers 404 on the root; that's still alive).
	cli := &http.Client{Timeout: 3 * time.Second}
	resp, err := cli.Get(u.Scheme + "://" + u.Host + "/")
	if err == nil {
		resp.Body.Close()
	}
	return nil
}

// runBM25Only fills r for the bm25 baseline backend.
func runBM25Only(r *perBackendResult, doc *graph.Document, bm *mcp.BM25Index, qs *querySet, topK int) {
	r.totalEnts = len(doc.Entities)
	r.embedded = 0
	r.embedSecs = 0
	r.dims = 0
	r.vectorBytes = 0

	var totalLatency time.Duration
	for _, q := range qs.Queries {
		t0 := time.Now()
		bmHits := bm.Search(q.Question, topK)
		lat := time.Since(t0)
		hits := make([]hitRow, 0, len(bmHits))
		for i, h := range bmHits {
			hits = append(hits, hitRow{
				rank:       i + 1,
				name:       h.Entity.Name,
				sourceFile: h.Entity.SourceFile,
				score:      h.Score,
				matched:    matchesAny(h.Entity, q.Truth),
			})
		}
		outc := scoreHits(hits, len(q.Truth))
		outc.searchMicro = lat.Microseconds()
		r.perQuery[q.ID] = outc
		totalLatency += lat
	}
	if n := len(qs.Queries); n > 0 {
		r.queryLatency = totalLatency / time.Duration(n)
	}
}

// matchesAny returns true when an entity satisfies ANY truth matcher.
//
// Name matching is tolerant of archigraph's "Receiver.Method" name convention:
// truth.name="Embed" matches entity.Name in any of {"Embed", "httpBackend.Embed",
// "(*httpBackend).Embed"}. SourceFile is a substring match so test-set authors
// can write paths like "internal/embed/http.go" without worrying about the
// absolute root.
func matchesAny(e *graph.Entity, truths []queryMatcher) bool {
	for _, t := range truths {
		if !nameMatches(e.Name, t.Name) {
			continue
		}
		if t.SourceFile == "" || strings.Contains(e.SourceFile, t.SourceFile) {
			return true
		}
	}
	return false
}

func nameMatches(entityName, truthName string) bool {
	if entityName == truthName {
		return true
	}
	// "Receiver.Method" matches truth "Method".
	if i := strings.LastIndex(entityName, "."); i >= 0 {
		if entityName[i+1:] == truthName {
			return true
		}
	}
	// Some extractors emit "TestFoo -> Bar" for inner-call entities; strip the
	// arrow prefix so a truth "Bar" still matches.
	if i := strings.LastIndex(entityName, "-> "); i >= 0 {
		if strings.TrimSpace(entityName[i+3:]) == truthName {
			return true
		}
	}
	return false
}

// scoreHits computes recall@5, recall@10, MRR for one ranked list.
// recall is judged against the union of truth matchers: a query with 3 truths
// scores 2/3 = 0.666 if two distinct truths are hit in top-K.
func scoreHits(hits []hitRow, truthCount int) queryOutcome {
	out := queryOutcome{hits: hits, truthCount: truthCount}
	if truthCount == 0 {
		return out
	}
	// Track *distinct* truth-matchers covered, not double-counted entity hits.
	matched5 := 0
	matched10 := 0
	var firstMatchRank int
	for _, h := range hits {
		if !h.matched {
			continue
		}
		if h.rank <= 5 {
			matched5++
		}
		if h.rank <= 10 {
			matched10++
		}
		if firstMatchRank == 0 {
			firstMatchRank = h.rank
		}
	}
	// Clamp to truth count: more matched-entity hits than truths can happen when
	// several truth matchers point at the same entity, or several distinct
	// entities all satisfy one matcher. Either way recall is bounded by 1.
	if matched5 > truthCount {
		matched5 = truthCount
	}
	if matched10 > truthCount {
		matched10 = truthCount
	}
	out.recallAt5 = float64(matched5) / float64(truthCount)
	out.recallAt10 = float64(matched10) / float64(truthCount)
	if firstMatchRank > 0 {
		out.mrr = 1.0 / float64(firstMatchRank)
	}
	return out
}

// writeReport emits the Markdown benchmark report.
func writeReport(path string, doc *graph.Document, qs *querySet, results []*perBackendResult, stateDir, repoRoot string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder

	fmt.Fprintf(&b, "# Embedding backend benchmark — archigraph #462\n\n")
	fmt.Fprintf(&b, "Generated by `cmd/embed-bench` (`-tags simplego`). Re-run on any indexed corpus:\n\n")
	fmt.Fprintf(&b, "```\nembed-bench -state-dir <store/<slug>> -repo-root <repo> \\\n             -queries docs/verify2/embedding-harness/queries.json \\\n             -backends bm25,builtin,http:http://127.0.0.1:11434/v1 \\\n             -out docs/verify2/embedding-backend-benchmark.md\n```\n\n")
	fmt.Fprintf(&b, "Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "- Corpus: `%s`\n", qs.Corpus)
	fmt.Fprintf(&b, "- State dir: `%s`\n", stateDir)
	fmt.Fprintf(&b, "- Repo root: `%s`\n", repoRoot)
	fmt.Fprintf(&b, "- Entities: %d (%d relationships)\n", len(doc.Entities), len(doc.Relationships))
	fmt.Fprintf(&b, "- Queries: %d (see `docs/verify2/embedding-harness/queries.json`)\n\n", len(qs.Queries))

	// --- Cost/quality matrix
	b.WriteString("## Cost vs quality matrix\n\n")
	b.WriteString("| Backend | Status | Recall@5 | Recall@10 | MRR | Embed corpus | Query latency (mean) | Vector size on disk | Dims |\n")
	b.WriteString("|---|---|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, r := range results {
		if r.skipped {
			fmt.Fprintf(&b, "| `%s` | skipped: %s | — | — | — | — | — | — | — |\n", r.tag, escapePipes(r.skipReason))
			continue
		}
		r5, r10, mrr := aggregateMetrics(r, qs)
		fmt.Fprintf(&b, "| `%s` | ok | %.3f | %.3f | %.3f | %s | %s | %s | %d |\n",
			r.tag,
			r5, r10, mrr,
			formatDuration(r.embedSecs),
			r.queryLatency.Round(time.Microsecond),
			formatBytes(r.vectorBytes),
			r.dims,
		)
	}
	b.WriteString("\n")

	// --- Per-query recall table
	b.WriteString("## Per-query Recall@5 (rows = queries, cols = backends)\n\n")
	b.WriteString("| Query |")
	for _, r := range results {
		fmt.Fprintf(&b, " %s |", r.tag)
	}
	b.WriteString("\n|---|")
	for range results {
		b.WriteString("---:|")
	}
	b.WriteString("\n")
	for _, q := range qs.Queries {
		fmt.Fprintf(&b, "| `%s` |", q.ID)
		for _, r := range results {
			if r.skipped {
				b.WriteString(" — |")
				continue
			}
			oc := r.perQuery[q.ID]
			fmt.Fprintf(&b, " %.2f |", oc.recallAt5)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// --- Skipped backends
	any := false
	for _, r := range results {
		if r.skipped {
			any = true
			break
		}
	}
	if any {
		b.WriteString("## Skipped backends\n\n")
		for _, r := range results {
			if r.skipped {
				fmt.Fprintf(&b, "- `%s` — %s\n", r.tag, r.skipReason)
			}
		}
		b.WriteString("\n")
	}

	// --- Recommendation
	b.WriteString("## Recommendation\n\n")
	b.WriteString(recommendation(results, qs))
	b.WriteString("\n")

	// --- Per-query top hits (debug)
	b.WriteString("## Per-query top-5 hits (debug)\n\n")
	for _, q := range qs.Queries {
		fmt.Fprintf(&b, "### `%s` — %s\n\n", q.ID, q.Question)
		b.WriteString("Truth:")
		for _, t := range q.Truth {
			fmt.Fprintf(&b, " `%s::%s`", t.SourceFile, t.Name)
		}
		b.WriteString("\n\n")
		for _, r := range results {
			fmt.Fprintf(&b, "**`%s`**", r.tag)
			if r.skipped {
				fmt.Fprintf(&b, " — skipped\n\n")
				continue
			}
			oc := r.perQuery[q.ID]
			fmt.Fprintf(&b, " — recall@5=%.2f recall@10=%.2f mrr=%.2f\n\n", oc.recallAt5, oc.recallAt10, oc.mrr)
			for _, h := range oc.hits {
				if h.rank > 5 {
					break
				}
				mark := " "
				if h.matched {
					mark = "✓"
				}
				fmt.Fprintf(&b, "  %d. [%s] `%s` (%s) score=%.4f\n", h.rank, mark, h.name, h.sourceFile, h.score)
			}
			b.WriteString("\n")
		}
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// aggregateMetrics returns mean recall@5 / recall@10 / MRR across queries.
func aggregateMetrics(r *perBackendResult, qs *querySet) (float64, float64, float64) {
	if r.skipped || len(qs.Queries) == 0 {
		return 0, 0, 0
	}
	var s5, s10, smrr float64
	var n int
	for _, q := range qs.Queries {
		oc, ok := r.perQuery[q.ID]
		if !ok {
			continue
		}
		s5 += oc.recallAt5
		s10 += oc.recallAt10
		smrr += oc.mrr
		n++
	}
	if n == 0 {
		return 0, 0, 0
	}
	return s5 / float64(n), s10 / float64(n), smrr / float64(n)
}

// recommendation synthesises a short prose recommendation from the results.
func recommendation(results []*perBackendResult, qs *querySet) string {
	type row struct {
		tag       string
		r5, r10   float64
		mrr       float64
		latency   time.Duration
		skipped   bool
		vecBytes  int64
		embedSecs float64
	}
	var rows []row
	for _, r := range results {
		if r.skipped {
			rows = append(rows, row{tag: r.tag, skipped: true})
			continue
		}
		r5, r10, mrr := aggregateMetrics(r, qs)
		rows = append(rows, row{tag: r.tag, r5: r5, r10: r10, mrr: mrr, latency: r.queryLatency, vecBytes: r.vectorBytes, embedSecs: r.embedSecs})
	}
	// Sort by recall@5 desc.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].r5 > rows[j].r5 })

	var b strings.Builder
	if len(rows) == 0 {
		return "_no results to summarise._"
	}
	top := rows[0]
	if top.skipped {
		return "All backends skipped; cannot recommend a default."
	}
	fmt.Fprintf(&b, "Best Recall@5 on this corpus: **`%s`** at %.3f (Recall@10=%.3f, MRR=%.3f).\n\n", top.tag, top.r5, top.r10, top.mrr)

	// Identify the bundled-default row by tag prefix.
	var builtin *row
	var bm25 *row
	for i := range rows {
		switch {
		case strings.HasPrefix(rows[i].tag, "builtin"):
			builtin = &rows[i]
		case rows[i].tag == "bm25":
			bm25 = &rows[i]
		}
	}
	if builtin != nil && !builtin.skipped {
		flag := ""
		if builtin.r5 < 0.60 {
			flag = " — **FLAGGED**: Recall@5 below the 60% threshold from #462; reconsider bundling MiniLM-L6 vs an alternative backend or model."
		}
		fmt.Fprintf(&b, "Bundled default (`%s`): Recall@5=%.3f, Recall@10=%.3f, MRR=%.3f%s\n\n", builtin.tag, builtin.r5, builtin.r10, builtin.mrr, flag)
	}
	if bm25 != nil && builtin != nil && !bm25.skipped && !builtin.skipped {
		delta := builtin.r5 - bm25.r5
		fmt.Fprintf(&b, "Lift vs BM25-only baseline: Recall@5 delta = **%+.3f** (%s).\n\n", delta, qualitativeDelta(delta))
	}
	if builtin != nil && !builtin.skipped {
		fmt.Fprintf(&b, "Cost: embed corpus once in %s, %s on disk, ~%s mean query latency.\n", formatDuration(builtin.embedSecs), formatBytes(builtin.vecBytes), builtin.latency.Round(time.Microsecond))
	}
	return b.String()
}

func qualitativeDelta(d float64) string {
	switch {
	case d >= 0.10:
		return "clear win for semantic — keep MiniLM bundled"
	case d >= 0.03:
		return "modest win — keep MiniLM bundled, document HTTP alternatives"
	case d > -0.03:
		return "tie — BM25 alone may be sufficient; bundling cost vs benefit is marginal"
	default:
		return "BM25 wins — investigate before bundling MiniLM"
	}
}

func formatBytes(n int64) string {
	if n <= 0 {
		return "—"
	}
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MiB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func formatDuration(secs float64) string {
	if secs <= 0 {
		return "—"
	}
	d := time.Duration(secs * float64(time.Second))
	return d.Round(time.Millisecond).String()
}

func escapePipes(s string) string { return strings.ReplaceAll(s, "|", "\\|") }
