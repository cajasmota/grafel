package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/archigraph/internal/daemon/client"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/quality"
)

// runQuality handles `archigraph quality <fixture-dir>`.
//
// Flow:
//  1. Load <fixture-dir>/expected.json
//  2. Run the production indexer over <fixture-dir>/src/ into a tempdir
//     (ADR-0016: graph.fb is primary; WithExportJSON ensures graph.json
//     is also written so loadDocument and --keep-graph continue to work)
//  3. Load the resulting graph.json
//  4. Diff with internal/quality.Evaluate, emit a human + (optional) JSON report
//
// We deliberately go through the existing Index() entry point — this keeps
// the harness end-to-end honest and lets fixtures detect regressions in
// any pass (extract / framework / cross-lang / resolver / synthesis).
func runQuality(argv []string) error {
	// Subverb dispatch: `quality audit-orphans <path>` routes via the
	// daemon's QualityAudit RPC (ADR-0017 Phase E). The legacy
	// `quality <fixture-dir>` form runs the in-process indexer for
	// golden-fixture CI gating and is preserved untouched.
	if len(argv) >= 1 && (argv[0] == "audit-orphans" || argv[0] == "audit") {
		return runAuditOrphans(argv[1:])
	}
	// `quality bug-rate-corpus <dir>` — composite health score across a corpus.
	if len(argv) >= 1 && argv[0] == "bug-rate-corpus" {
		return runBugRateCorpus(argv[1:])
	}
	fs := flag.NewFlagSet("quality", flag.ContinueOnError)
	jsonOut := fs.String("json", "", "write JSON report to this path (default: stderr-only human summary)")
	keepGraph := fs.Bool("keep-graph", false, "preserve the temp graph.json (path printed on stderr)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("missing <fixture-dir> argument")
	}
	fixtureDir, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("resolve fixture dir: %w", err)
	}

	fix, err := quality.LoadFixture(fixtureDir)
	if err != nil {
		return err
	}

	srcDir := quality.SourceDir(fixtureDir)
	if st, serr := os.Stat(srcDir); serr != nil || !st.IsDir() {
		return fmt.Errorf("fixture src/ missing or not a directory: %s", srcDir)
	}

	// Output to a tempdir so we never pollute the fixture tree.
	// ADR-0016: the indexer now writes graph.fb by default; we pass
	// WithExportJSON so graph.json is also produced (needed by loadDocument
	// and --keep-graph). The graphPath points at graph.json inside tmp.
	tmp, err := os.MkdirTemp("", "archigraph-quality-*")
	if err != nil {
		return fmt.Errorf("mktemp: %w", err)
	}
	if !*keepGraph {
		defer os.RemoveAll(tmp)
	}
	graphPath := filepath.Join(tmp, "graph.json")

	// Run the production indexer. repoTag is set to the fixture name for
	// readability when humans inspect graph.json by hand. We pass an
	// explicit out path so nothing is written under the fixture.
	if err := Index(srcDir, graphPath, fix.Name, nil /*skip*/, false /*pretty*/, false, /*jsonStats*/
		WithExportJSON(true)); err != nil {
		return fmt.Errorf("index fixture src: %w", err)
	}
	if *keepGraph {
		fmt.Fprintf(os.Stderr, "quality: kept graph at %s\n", graphPath)
	}

	doc, err := loadDocument(graphPath)
	if err != nil {
		return err
	}

	rep := quality.Evaluate(fix, doc)
	rep.WriteHuman(os.Stderr)

	if *jsonOut != "" {
		f, ferr := os.Create(*jsonOut)
		if ferr != nil {
			return fmt.Errorf("create json report: %w", ferr)
		}
		defer f.Close()
		if err := rep.WriteJSON(f); err != nil {
			return fmt.Errorf("write json report: %w", err)
		}
		fmt.Fprintf(os.Stderr, "quality: wrote JSON report to %s\n", *jsonOut)
	}

	// Non-zero exit when any must-have miss OR any forbidden hit. This
	// makes the command usable as-is in CI without a wrapper.
	if rep.EntityFound < rep.EntityExpected ||
		rep.RelFound < rep.RelExpected ||
		len(rep.ForbiddenHits) > 0 {
		// Returning an error from a cobra RunE would surface "Error: ..."
		// which is noisier than necessary — exit cleanly with code 2.
		os.Exit(2)
	}
	return nil
}

// runAuditOrphans is the entry point for `archigraph quality audit-orphans`.
//
// Per ADR-0017 Phase E this is a thin daemon RPC — the audit logic lives
// in the daemon (which imports internal/quality/audit). If the daemon is
// not running the command prints the canonical hint and exits non-zero.
//
// Flag surface is unchanged from the previous in-process implementation so
// existing scripts and aliases keep working.
func runAuditOrphans(argv []string) error {
	fs := flag.NewFlagSet("quality audit-orphans", flag.ContinueOnError)
	corpus := fs.String("corpus", "", "treat <path> as a corpus directory containing many indexed repos")
	jsonOut := fs.Bool("json", false, "emit JSON instead of markdown")
	outPath := fs.String("output", "", "write to this file instead of stdout")
	_ = fs.Bool("reindex", false, "(not implemented) re-run indexer before auditing; default trusts graph.json")
	if err := fs.Parse(argv); err != nil {
		return err
	}

	var path string
	corpusMode := false
	switch {
	case *corpus != "":
		path = *corpus
		corpusMode = true
	case fs.NArg() >= 1:
		path = fs.Arg(0)
	default:
		return fmt.Errorf("usage: archigraph quality audit-orphans [--corpus] <path> [--json] [--output FILE]")
	}

	// Resolve to absolute so the daemon (which may have a different cwd)
	// can open the path without ambiguity.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Decide output format up front (mirrors the old in-process logic).
	jsonMode := *jsonOut
	if *outPath != "" && strings.HasSuffix(*outPath, ".json") {
		jsonMode = true
	}
	if *outPath != "" && strings.HasSuffix(*outPath, ".md") {
		jsonMode = false
	}

	// Dial the daemon. Fail fast with the canonical install hint.
	c, err := client.Dial()
	if err != nil {
		if errors.Is(err, client.ErrDaemonNotRunning) {
			return daemonNotRunningErr
		}
		return err
	}
	defer c.Close()

	reply, err := c.QualityAudit(proto.QualityAuditRequest{
		RepoPath: absPath,
		Kind:     "orphans",
		Corpus:   corpusMode,
		JSON:     jsonMode,
	})
	if err != nil {
		return fmt.Errorf("quality audit RPC: %w", err)
	}

	// Write the report.
	var w *os.File = os.Stdout
	if *outPath != "" {
		f, ferr := os.Create(*outPath)
		if ferr != nil {
			return fmt.Errorf("create output: %w", ferr)
		}
		defer f.Close()
		w = f
	}
	if _, err := fmt.Fprint(w, reply.Markdown); err != nil {
		return err
	}
	if *outPath != "" {
		fmt.Fprintf(os.Stderr, "audit-orphans: wrote %s\n", *outPath)
	}
	return nil
}

// loadDocument reads a graph.json file written by Index().
func loadDocument(path string) (*graph.Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read graph.json: %w", err)
	}
	var doc graph.Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse graph.json: %w", err)
	}
	return &doc, nil
}
