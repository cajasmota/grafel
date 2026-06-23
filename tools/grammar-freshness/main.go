// Command grammar-freshness is the A2 freshness alarm (epic #5359, milestone
// 0.1.4). It reads the committed grammars.lock manifest and, for each
// grammar-backed language, queries its upstream tree-sitter grammar repo's
// latest release/tag (falling back to the default-branch latest commit) and
// reports which grammars have moved ahead of the bundled smacker snapshot.
//
// Why per-grammar and not per-dependency: the smacker/go-tree-sitter binding
// that bundles every grammar is unmaintained (at its own upstream HEAD since
// 2024-08-27, see docs/grammar-freshness-audit.md). Renovate on the dep (A1)
// therefore finds nothing newer. This tool tracks each upstream
// tree-sitter/tree-sitter-<lang> repo INDEPENDENTLY of the dead binding, so it
// is THE freshness alarm. It is expected to report most/all 28 grammars stale
// until the B2 migration lands — that is correct and intended.
//
// Standalone dev tool: zero imports from internal/ packages; net/http + stdlib
// only. The upstream-version source is injected so tests never hit the network.
//
// Usage:
//
//	go run ./tools/grammar-freshness [-lock grammars.lock] [-format table|markdown]
//
// Exit code is non-zero ONLY on hard errors (unreadable manifest, total API
// failure). Finding stale grammars is reported, not a failure — the CI job
// inspects stdout and opens/updates a tracking issue.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run is split out from main so tests can drive it with custom argv, a custom
// upstream source, and captured output.
func run(argv []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("grammar-freshness", flag.ContinueOnError)
	fs.SetOutput(stderr)
	lockPath := fs.String("lock", "grammars.lock", "path to the grammars.lock manifest")
	format := fs.String("format", "table", "output format: table | markdown")
	timeoutS := fs.Int("timeout", 30, "per-request HTTP timeout in seconds")
	if err := fs.Parse(argv); err != nil {
		return err
	}

	lock, err := loadLock(*lockPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	src := &githubSource{
		client: &http.Client{Timeout: time.Duration(*timeoutS) * time.Second},
		token:  firstEnv("GITHUB_TOKEN", "GH_TOKEN"),
	}

	ctx := context.Background()
	report := check(ctx, lock, src)

	switch *format {
	case "table":
		writeTable(stdout, report)
	case "markdown":
		writeMarkdown(stdout, report)
	default:
		return fmt.Errorf("unknown -format %q (want table|markdown)", *format)
	}

	// A hard error only if EVERY grammar was unreachable — that means the API
	// is down or the token is bad, which the CI job should surface as a failure
	// rather than silently "no stale grammars".
	if len(report.Grammars) > 0 && report.Errored == len(report.Grammars) {
		return fmt.Errorf("all %d upstream lookups failed (API down or bad token?)", report.Errored)
	}
	return nil
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
