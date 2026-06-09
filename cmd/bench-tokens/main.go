// cmd/bench-tokens is a user-facing token-economy benchmark (#4293).
//
// For each seed question it builds the minimal subgraph that answers the
// question (reusing the in-process MCP server's archigraph_find handler, which
// runs the same ranked-BFS subgraph extraction the live tools use) and
// estimates its token cost. It then estimates the token cost of the naive
// "just read the relevant files" baseline — reading every distinct source file
// touched by the matched entities — and emits a markdown table comparing the
// two, per question plus an aggregate ratio.
//
// The token estimator is char/4, the same approximation used throughout the
// codebase (internal/mcp/render.go estimateTokens, the daemon's
// payload_token_estimate, and internal/docgen). Reusing it keeps this
// benchmark's numbers comparable to the cost the MCP layer actually reports.
//
// Usage:
//
//	go run ./cmd/bench-tokens -group upvate
//	go run ./cmd/bench-tokens -group upvate -questions seeds.txt -out docs/benches/tokens.md
//
// -questions, when given, is a newline-delimited file of seed questions (blank
// lines and #-comments ignored). Otherwise a small built-in seed set is used.
//
// This command talks to the live registry/graph and is therefore
// deploy-deferred: it is exercised manually, not in CI. The token-comparison
// math it relies on lives in pure helpers (see token_economy.go) and is
// unit-tested.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	mcpapi "github.com/mark3labs/mcp-go/mcp"

	"github.com/cajasmota/archigraph/internal/mcp"
)

func main() {
	group := flag.String("group", "upvate", "group name to bench against")
	questionsPath := flag.String("questions", "", "path to a newline-delimited seed-questions file (default: built-in seed set)")
	depth := flag.Int("depth", 3, "subgraph depth passed to archigraph_find")
	tokenBudget := flag.Int("token-budget", 1200, "token_budget passed to archigraph_find (graph-scoped payload cap)")
	out := flag.String("out", "", "output markdown path (default: stdout)")
	flag.Parse()

	questions, err := loadQuestions(*questionsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load questions:", err)
		os.Exit(1)
	}

	srv, err := mcp.NewServer(mcp.Config{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "boot mcp server:", err)
		os.Exit(1)
	}

	rows := make([]questionCost, 0, len(questions))
	for _, q := range questions {
		row := measureQuestion(srv, *group, q, *depth, *tokenBudget)
		rows = append(rows, row)
		fmt.Fprintf(os.Stderr, "%-50s graph=%6d file-read=%7d ratio=%5.2fx\n",
			truncate(q, 50), row.GraphTokens, row.FileReadTokens, savingsRatio(row.GraphTokens, row.FileReadTokens))
	}

	md := renderMarkdown(*group, rows)
	if *out != "" {
		if err := os.WriteFile(*out, []byte(md), 0644); err != nil {
			fmt.Fprintln(os.Stderr, "write:", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "wrote", *out)
		return
	}
	fmt.Print(md)
}

// defaultQuestions is the built-in seed set: a few worked examples that cover
// distinct query shapes (locate, neighbourhood, flow).
var defaultQuestions = []string{
	"where is authentication middleware defined",
	"how does the order service place an order",
	"what validates an incoming contract proposal",
}

// loadQuestions reads the seed set from path, or returns the built-in default
// when path is empty. Blank lines and #-prefixed comments are skipped.
func loadQuestions(path string) ([]string, error) {
	if path == "" {
		return defaultQuestions, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var qs []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		qs = append(qs, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(qs) == 0 {
		return nil, fmt.Errorf("%s contained no questions", path)
	}
	return qs, nil
}

// measureQuestion runs archigraph_find for one seed question (the graph-scoped
// answer), then computes the naive file-read baseline from the matched
// entities' distinct source files. It reuses the live find handler so the
// graph-scoped payload is byte-identical to what the MCP tool would return.
func measureQuestion(srv *mcp.Server, group, question string, depth, tokenBudget int) questionCost {
	row := questionCost{Question: question}

	tool := srv.MCP.GetTool("archigraph_find")
	if tool == nil {
		row.Note = "archigraph_find not registered"
		return row
	}
	req := mcpapi.CallToolRequest{}
	req.Params.Name = "archigraph_find"
	req.Params.Arguments = map[string]any{
		"question":     question,
		"group":        group,
		"depth":        float64(depth),
		"token_budget": float64(tokenBudget),
	}

	res, err := tool.Handler(context.Background(), req)
	if err != nil {
		row.Note = "find error: " + err.Error()
		return row
	}
	payload := resultText(res)
	row.GraphTokens = estimateTokens(payload)

	// Naive baseline: an agent without the graph reads the source files that
	// contain the answer. We approximate "the answer" by the files of the
	// entities the graph surfaced, deduplicated, and read in full.
	files := extractSourceFiles(payload)
	row.FileCount = len(files)
	row.FileReadTokens = fileReadTokens(files)
	return row
}

// resultText concatenates the text content of an MCP tool result.
func resultText(res *mcpapi.CallToolResult) string {
	if res == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcpapi.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
