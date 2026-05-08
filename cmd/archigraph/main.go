package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/cajasmota/archigraph/internal/cli"
)

// main wires the cmd-local index/mcp implementations into the cobra
// dispatch tree owned by internal/cli, then delegates. Index/MCP live
// in this package because they reach into a number of internal packages
// (extractor, classifier, ...) that we don't want to surface from cli.
func main() {
	cli.Execute(cli.Hooks{
		RunIndex: runIndex,
		RunMCP:   runMCP,
		RunLinks: runLinksHook,
	})
}

// runIndex parses flags for the `index` subcommand and runs the indexer.
func runIndex(argv []string) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	out := fs.String("out", "", "output path for graph.json (default: <repo>/.archigraph/graph.json)")
	repoTag := fs.String("repo-tag", "", "repository tag stored on entities (default: dirname of repo path)")
	skip := fs.String("skip-pass", "", "comma-separated list of passes to skip (extract,framework,cross-lang,graph-algo,build-document,enrichment)")
	pretty := fs.Bool("pretty", false, "emit indented JSON for graph.json and graph-stats.json (default: minified)")
	jsonStats := fs.Bool("json-stats", false, "emit per-run statistics (entities, relationships, dispositions, bug-rate) to stdout as JSON")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("missing <repo> argument")
	}
	repoPath := fs.Arg(0)
	var skipPasses []string
	if *skip != "" {
		skipPasses = []string{*skip}
	}
	return Index(repoPath, *out, *repoTag, skipPasses, *pretty, *jsonStats)
}

// runLinksHook is wired into cli.Hooks so the watcher can re-run cross-
// repo link passes whenever a registered repo's graph.json changes.
func runLinksHook(group string) error {
	return cli.RunLinksForGroup(group)
}

// fail prints an error and exits non-zero. Convenience for callers
// outside main() that have nowhere else to report.
func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format, a...)
	if len(format) > 0 && format[len(format)-1] != '\n' {
		fmt.Fprintln(os.Stderr)
	}
	os.Exit(1)
}
