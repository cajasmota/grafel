// Command embed-verify is an end-to-end semantic-search smoke probe (#461).
// It indexes a fixture in isolation (no daemon, no live :47274 port), writes
// graph.fb + embeddings.bin, then runs a BM25-only and a BM25+RRF semantic
// query against the same question and prints the top hits + source for each.
// Build with `-tags simplego` to exercise the bundled MiniLM backend.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/embed"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/mcp"
)

func main() {
	var (
		repo  = flag.String("repo", "", "repo path to index")
		query = flag.String("q", "where do we handle authentication", "query text")
	)
	flag.Parse()
	if *repo == "" {
		fmt.Fprintln(os.Stderr, "usage: embed-verify -repo <path> [-q <question>]")
		os.Exit(2)
	}
	if err := run(*repo, *query); err != nil {
		fmt.Fprintln(os.Stderr, "ERR:", err)
		os.Exit(1)
	}
}

func run(repo, query string) error {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	stateDir := daemon.StateDirForRepo(absRepo)
	graphPath := daemon.GraphPathForRepo(absRepo)
	_ = os.MkdirAll(stateDir, 0o755)

	// NOTE: this is the trimmed indexing path — for the smoke probe we
	// re-implement extract→build skipping all the optional passes the
	// real indexer chains, by calling the public Index entrypoint of
	// cmd/archigraph via go run. To keep this tool self-contained we
	// instead build a synthetic Document from the file tree below.
	// The real indexer runs via `archigraph index` (daemon RPC).
	doc, err := buildSyntheticDoc(absRepo)
	if err != nil {
		return fmt.Errorf("synth doc: %w", err)
	}
	fmt.Printf("repo=%s entities=%d stateDir=%s\n", absRepo, len(doc.Entities), stateDir)

	// Write graph.json so the MCP state loader can pick it up.
	if err := graph.WriteAtomic(graphPath, doc, true); err != nil {
		return fmt.Errorf("write graph.json: %w", err)
	}

	// Pass 9: embed.
	cfg, _ := embed.LoadConfig()
	ctx := context.Background()
	be, err := embed.NewBackend(ctx, cfg)
	if err != nil {
		return fmt.Errorf("backend: %w", err)
	}
	defer be.Close()
	fmt.Printf("backend=%s dims=%d\n", be.Name(), be.Dims())
	t0 := time.Now()
	store, res, err := embed.EmbedDocument(ctx, doc, absRepo, stateDir, be)
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}
	fmt.Printf("embed: total=%d embedded=%d reused=%d took=%s\n", res.Total, res.Embedded, res.Reused, time.Since(t0).Round(time.Millisecond))

	fi, err := os.Stat(embed.StorePath(stateDir))
	if err == nil {
		fmt.Printf("embeddings.bin size=%d bytes\n", fi.Size())
	}

	// Query side: BM25 alone, then BM25+semantic via RRF.
	bm := mcp.BuildBM25(doc)
	bmHits := bm.Search(query, 5)
	fmt.Printf("\nQUERY: %q\n", query)
	fmt.Println("--- BM25-only top 5 ---")
	if len(bmHits) == 0 {
		fmt.Println("  (no BM25 hits)")
	}
	for i, h := range bmHits {
		fmt.Printf("  %d. %s\t[%s]\tscore=%.4f\n", i+1, h.Entity.Name, h.Entity.SourceFile, h.Score)
	}

	t1 := time.Now()
	qvecs, err := be.Embed(ctx, []string{query})
	if err != nil {
		return fmt.Errorf("query embed: %w", err)
	}
	queryLatency := time.Since(t1)
	semIDs := store.Search(qvecs[0], 5)
	byID := map[string]*graph.Entity{}
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}
	semHits := make([]mcp.Hit, 0, len(semIDs))
	for _, s := range semIDs {
		if e, ok := byID[s.ID]; ok {
			semHits = append(semHits, mcp.Hit{Entity: e, Score: s.Score, Source: "semantic"})
		}
	}
	fmt.Printf("\n--- semantic top 5 (query embed latency=%s) ---\n", queryLatency.Round(time.Millisecond))
	for i, h := range semHits {
		fmt.Printf("  %d. %s\t[%s]\tcosine=%.4f\n", i+1, h.Entity.Name, h.Entity.SourceFile, h.Score)
	}

	fused := mcp.FuseRRF(bmHits, semHits)
	fmt.Println("\n--- RRF-fused top 5 ---")
	for i, h := range fused {
		if i >= 5 {
			break
		}
		fmt.Printf("  %d. %s\t[%s]\tscore=%.4f\tsource=%s\n", i+1, h.Entity.Name, h.Entity.SourceFile, h.Score, h.Source)
	}
	return nil
}

// buildSyntheticDoc walks repo for *.go files and emits one Entity per
// top-level Go function (a deliberately minimal stand-in for the real
// extractor). It pulls each function's leading `// `-docstring into
// Properties["docstring"]. Enough to exercise BM25 + the semantic pipeline.
func buildSyntheticDoc(repoRoot string) (*graph.Document, error) {
	doc := &graph.Document{Version: 1, Repo: repoRoot, GeneratedAt: time.Now()}
	id := 0
	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".go" {
			return err
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		// Scan for `func Name(` headers and accumulate the preceding `// `
		// comment lines as a docstring.
		lines := splitLines(string(data))
		var doclines []string
		for i, ln := range lines {
			t := trimSpace(ln)
			if hasPrefix(t, "//") {
				doclines = append(doclines, trimSpace(stripPrefix(t, "//")))
				continue
			}
			if hasPrefix(t, "func ") {
				name := funcName(t)
				if name != "" {
					id++
					ent := graph.Entity{
						ID:         fmt.Sprintf("e%04d", id),
						Name:       name,
						Kind:       "function",
						SourceFile: rel,
						StartLine:  i + 1,
						EndLine:    findFuncEnd(lines, i),
						Language:   "go",
						Properties: map[string]string{},
					}
					if len(doclines) > 0 {
						ent.Properties["docstring"] = joinNonEmpty(doclines, " ")
					}
					doc.Entities = append(doc.Entities, ent)
				}
				doclines = nil
			} else if t != "" {
				doclines = nil
			}
		}
		return nil
	})
	doc.Stats.Entities = len(doc.Entities)
	return doc, err
}

// --- tiny string helpers (avoid pulling in strings to keep deps minimal) ---
func splitLines(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
func trimSpace(s string) string {
	a, b := 0, len(s)
	for a < b && (s[a] == ' ' || s[a] == '\t') {
		a++
	}
	for b > a && (s[b-1] == ' ' || s[b-1] == '\t' || s[b-1] == '\r') {
		b--
	}
	return s[a:b]
}
func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
func stripPrefix(s, p string) string {
	if hasPrefix(s, p) {
		return s[len(p):]
	}
	return s
}
func funcName(line string) string {
	// "func Name(...)" or "func (recv T) Name(...)"
	if !hasPrefix(line, "func ") {
		return ""
	}
	rest := line[5:]
	if hasPrefix(rest, "(") {
		// method — skip to closing ')'
		i := 1
		for i < len(rest) && rest[i] != ')' {
			i++
		}
		if i >= len(rest)-1 {
			return ""
		}
		rest = trimSpace(rest[i+1:])
	}
	end := 0
	for end < len(rest) && (rest[end] != '(' && rest[end] != ' ') {
		end++
	}
	return rest[:end]
}
func findFuncEnd(lines []string, start int) int {
	depth := 0
	for i := start; i < len(lines); i++ {
		for _, r := range lines[i] {
			if r == '{' {
				depth++
			} else if r == '}' {
				depth--
				if depth == 0 {
					return i + 1
				}
			}
		}
	}
	return start + 1
}
func joinNonEmpty(ss []string, sep string) string {
	out := ""
	for _, s := range ss {
		if s == "" {
			continue
		}
		if out != "" {
			out += sep
		}
		out += s
	}
	return out
}
