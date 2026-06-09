package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// questionCost is the per-question token accounting produced by the benchmark.
type questionCost struct {
	Question string
	// GraphTokens is the estimated token cost of the graph-scoped answer (the
	// archigraph_find payload — the minimal subgraph that answers the question).
	GraphTokens int
	// FileReadTokens is the estimated token cost of the naive baseline: reading
	// every distinct source file the answer touches, in full.
	FileReadTokens int
	// FileCount is the number of distinct files the naive baseline would read.
	FileCount int
	// Note carries a per-row diagnostic (e.g. an error or "no matches").
	Note string
}

// estimateTokens approximates token count as len/4. This mirrors the char/4
// estimator used across the codebase (internal/mcp/render.go,
// internal/docgen, and the daemon's payload_token_estimate), so the numbers
// here line up with the cost the MCP layer actually reports.
func estimateTokens(s string) int { return len(s) / 4 }

// savingsRatio is file-read-tokens / graph-tokens: how many times more
// expensive the naive file-read baseline is than the graph-scoped answer.
// Larger is better (the graph saves more). Returns 0 when graph tokens is 0.
func savingsRatio(graphTokens, fileReadTokens int) float64 {
	if graphTokens <= 0 {
		return 0
	}
	return float64(fileReadTokens) / float64(graphTokens)
}

// aggregateRatio is the corpus-level ratio: the sum of file-read tokens over
// the sum of graph tokens across all rows. Rows with a zero graph cost (errors
// / no matches) are skipped so they don't distort the aggregate. Returns 0
// when nothing measurable contributed.
func aggregateRatio(rows []questionCost) float64 {
	var graph, fileRead int
	for _, r := range rows {
		if r.GraphTokens <= 0 {
			continue
		}
		graph += r.GraphTokens
		fileRead += r.FileReadTokens
	}
	if graph == 0 {
		return 0
	}
	return float64(fileRead) / float64(graph)
}

// sourceLocRe matches a "path:line" location token in the compact find output,
// e.g. "internal/mcp/render.go:52". The path may contain slashes, dots, dashes
// and underscores; the trailing :<line> anchors it to a real source location.
var sourceLocRe = regexp.MustCompile(`([A-Za-z0-9_./-]+\.[A-Za-z0-9]+):[0-9]+`)

// extractSourceFiles returns the distinct, sorted set of source file paths
// referenced by a compact find payload. Each ranked hit renders as
// "<label>  <file>:<line>", so the file is recoverable from the location
// token without a second graph round-trip.
func extractSourceFiles(payload string) []string {
	set := map[string]bool{}
	for _, m := range sourceLocRe.FindAllStringSubmatch(payload, -1) {
		set[m[1]] = true
	}
	files := make([]string, 0, len(set))
	for f := range set {
		files = append(files, f)
	}
	sort.Strings(files)
	return files
}

// fileReadTokens estimates the token cost of reading every given file in full.
// Files that cannot be read (path is repo-relative and we are outside the
// repo, or the file is gone) contribute the perFileFallbackTokens estimate so
// the baseline is never silently understated.
func fileReadTokens(files []string) int {
	total := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			total += perFileFallbackTokens
			continue
		}
		total += estimateTokens(string(b))
	}
	return total
}

// perFileFallbackTokens is the assumed cost of a source file we could not read
// (~300 lines * ~10 tokens/line). Conservative: it keeps the file-read
// baseline from collapsing to zero when run outside the indexed repo.
const perFileFallbackTokens = 3000

// renderMarkdown produces the user-facing report: a per-question table of
// graph-scoped vs file-read tokens with each row's savings ratio, followed by
// the aggregate ratio across the corpus.
func renderMarkdown(group string, rows []questionCost) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Token-economy benchmark — %s\n\n", group)
	b.WriteString("Graph-scoped answer (minimal subgraph via `archigraph_find`) vs the naive ")
	b.WriteString("\"read the relevant files\" baseline. Tokens estimated at char/4. ")
	b.WriteString("Ratio = file-read tokens ÷ graph tokens (higher = more saved by the graph).\n\n")

	b.WriteString("| # | Question | Graph tokens | File-read tokens | Files | Ratio |\n")
	b.WriteString("|---|----------|-------------:|-----------------:|------:|------:|\n")
	for i, r := range rows {
		q := r.Question
		if r.Note != "" {
			q = fmt.Sprintf("%s _(%s)_", q, r.Note)
		}
		ratio := "—"
		if r.GraphTokens > 0 {
			ratio = fmt.Sprintf("%.2fx", savingsRatio(r.GraphTokens, r.FileReadTokens))
		}
		fmt.Fprintf(&b, "| %d | %s | %d | %d | %d | %s |\n",
			i+1, escapePipes(q), r.GraphTokens, r.FileReadTokens, r.FileCount, ratio)
	}

	agg := aggregateRatio(rows)
	b.WriteString("\n")
	if agg > 0 {
		fmt.Fprintf(&b, "**Aggregate:** the naive file-read baseline costs **%.2fx** the tokens of the "+
			"graph-scoped answers across this seed set.\n", agg)
	} else {
		b.WriteString("**Aggregate:** no question produced a measurable graph-scoped answer.\n")
	}
	return b.String()
}

// escapePipes keeps user-supplied question text from breaking the table.
func escapePipes(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
