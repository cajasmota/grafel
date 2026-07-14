package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/cajasmota/grafel/internal/daemon/sched"
)

// runIndexInternal is the entrypoint for the hidden `grafel index --internal`
// subcommand. It is fork-exec'd by the daemon's subprocess runner (S5 of #2149 /
// issue #2155) to isolate per-reindex memory allocation in a child process. The
// child runs the full Index() pipeline and exits; the daemon reads progress lines
// from the child's stderr and waits for exit.
//
// This is intentionally NOT exposed as a cobra subcommand — it is registered in
// main.go as a raw os.Args intercept (like `xrepo-verify`) so cobra + flag
// parsing overhead is zero and the hidden surface does not appear in --help.
//
// Contract:
//   - exit 0  = index succeeded; graph.fb written to <out> (or default path)
//   - exit 1  = index failed; error message on stderr
//   - stdout  = JSON progress lines (one per pipeline phase)
//   - SIGTERM = clean cancellation; exit non-zero
func runIndexInternal(argv []string) int {
	fs := flag.NewFlagSet("index-internal", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var (
		repo         = fs.String("repo", "", "absolute path of the repository to index (required)")
		ref          = fs.String("ref", "", "git ref captured at enqueue time; passed as hint only (may be empty)")
		out          = fs.String("out", "", "output directory for graph.fb; defaults to daemon store layout")
		repoTag      = fs.String("repo-tag", "", "slug written into every graph entity (defaults to dir basename)")
		skipPasses   = fs.String("skip-pass", "", "comma-separated list of pass names to skip")
		exportJSON   = fs.Bool("export-json", false, "also emit graph.json alongside graph.fb")
		ingestDocs   = fs.Bool("ingest-docs", false, "opt-in: deterministically ingest in-repo *.md and *.pdf files as Document/Section nodes + exact-mention links (no LLM, no network)")
		emitProgress = fs.Bool("emit-progress", false, "stream per-module progress.Event JSON lines on stdout for the parent to republish (rebuild / wizard first-index)")
		groupSlug    = fs.String("group-slug", "", "group slug stamped on progress events when --emit-progress is set")
		interactive  = fs.Bool("interactive", false, "run at the foreground GRAFEL_REBUILD_GOMAXPROCS extract cap (human-awaited rebuild) instead of the background cap")
		incremental  = fs.String("incremental", "", "state dir enabling diff-aware re-indexing (matches WithIncremental); empty = full index")
	)

	if err := fs.Parse(argv); err != nil {
		fmt.Fprintf(os.Stderr, "index-internal: flag parse: %v\n", err)
		return 2
	}

	if *repo == "" {
		fmt.Fprintln(os.Stderr, "index-internal: --repo is required")
		return 2
	}

	// Build skip set.
	var skipList []string
	if *skipPasses != "" {
		for _, p := range strings.Split(*skipPasses, ",") {
			if p = strings.TrimSpace(p); p != "" {
				skipList = append(skipList, p)
			}
		}
	}

	// Wire options. The daemon's subprocess runner always passes --skip-pass=graph-algo
	// for fast reactive reindexes; full algo passes continue to run in-process via
	// daemonSchedulerAlgo (which is not on the hot path that needs memory isolation).
	opts := []IndexOption{
		WithExportJSON(*exportJSON),
		WithIngestDocs(*ingestDocs),
	}
	// Rebuild / wizard first-index (#5729 follow-up): stream per-module progress
	// on stdout so the parent RunSubprocessIndex can republish it into the
	// rebuild's broker / split-mode sidecar. The publisher marshals each
	// progress.Event verbatim; WithProgressSlugs stamps the group/repo identity so
	// republished rows key the SAME (group, repo, module) the in-process path uses
	// (repoSlug == --repo-tag, mirroring the in-process WithProgressSlugs(group,
	// slug) where repoTag is also the slug).
	if *emitProgress {
		opts = append(opts,
			WithPublisher(sched.NewStdoutProgressPublisher(os.Stdout)),
			WithProgressSlugs(*groupSlug, *repoTag))
	}
	// Foreground rebuild: run the extract sub-subprocesses at the higher
	// GRAFEL_REBUILD_GOMAXPROCS cap. Background reactive reindexes leave this off
	// and stay at the throttled GRAFEL_EXTRACT_GOMAXPROCS default.
	if *interactive {
		opts = append(opts, WithInteractive(true))
	}
	// Diff-aware re-indexing when the rebuild path requested it.
	if *incremental != "" {
		opts = append(opts, WithIncremental(*incremental))
	}

	// Emit a JSON start line so the daemon IPC reader can log the start event
	// without any special protocol — a single JSON object per line on stdout.
	fmt.Printf("{\"event\":\"index_start\",\"repo\":%q,\"ref\":%q}\n", *repo, *ref)

	// Use context.Background() — SIGTERM is delivered by the OS directly to this
	// process, and the Go runtime will handle it via signal delivery to any
	// blocking syscalls. The subprocess runner sends SIGTERM on cancel.
	ctx := context.Background()
	_ = ctx // Index() does not yet accept a context; the goroutine stack unwinds on signal.

	outPath := *out
	err := Index(*repo, outPath, *repoTag, skipList, false, false, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "index-internal: %v\n", err)
		fmt.Printf("{\"event\":\"index_error\",\"repo\":%q,\"error\":%q}\n", *repo, err.Error())
		return 1
	}

	fmt.Printf("{\"event\":\"index_done\",\"repo\":%q,\"ref\":%q}\n", *repo, *ref)
	return 0
}
