// Command grammar-freshness is the A2 freshness alarm (epic #5359, milestone
// 0.1.4). It reads the committed grammars.lock manifest, resolves each
// grammar's ACTUAL bundled version from this repo, queries that grammar's
// upstream repo for its latest release/commit, and reports which grammars have
// genuinely fallen behind.
//
// Where "bundled" comes from (#6749). It used to be a single constant: the
// pinned_date of a smacker/go-tree-sitter binding that is no longer a
// dependency of this repo at all. Every row therefore reported the same date,
// so the verdict could not change no matter what anyone upgraded — kotlin was
// reported ~23 months behind while pinned at the newest upstream release. The
// bundled version is now derived per grammar from go.mod (applying replace
// directives) and from the vendored provenance headers under
// internal/treesitter/ts/grammars/. A grammar whose pin cannot be resolved is
// reported UNKNOWN; it is never defaulted to a constant.
//
// Comparison shape. A module version like v0.23.6 is a RELEASE, not a date, so
// release pins are compared release-to-release. Only a pseudo-version or branch
// pin (which is a commit) falls back to a commit-date comparison.
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
	goModPath := fs.String("gomod", "go.mod", "path to go.mod, the source of per-grammar module pins")
	vendorRoot := fs.String("vendored", "internal/treesitter/ts/grammars",
		"root of the vendored grammar packages, the source of vendored pins")
	format := fs.String("format", "table", "output format: table | markdown")
	timeoutS := fs.Int("timeout", 30, "per-request HTTP timeout in seconds")
	if err := fs.Parse(argv); err != nil {
		return err
	}

	lock, err := loadLock(*lockPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	// A declared bundling module that is not a dependency is the #6749 defect
	// itself; refuse to run rather than report its date as every grammar's.
	if err := validateBinding(lock.Binding, *goModPath); err != nil {
		return err
	}

	pins, err := loadPins(*goModPath, *vendorRoot)
	if err != nil {
		return fmt.Errorf("resolve bundled versions: %w", err)
	}

	src := &githubSource{
		client: &http.Client{Timeout: time.Duration(*timeoutS) * time.Second},
		token:  firstEnv("GITHUB_TOKEN", "GH_TOKEN"),
	}

	ctx := context.Background()
	report := check(ctx, lock, pins, src)

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
