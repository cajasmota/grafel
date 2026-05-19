package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/quality"
)

// runQuality handles `archigraph quality <fixture-dir>`.
//
// Flow:
//  1. Load <fixture-dir>/expected.json
//  2. Run the production indexer over <fixture-dir>/src/ into a tempdir
//  3. Load the resulting graph.json
//  4. Diff with internal/quality.Evaluate, emit a human + (optional) JSON report
//
// We deliberately go through the existing Index() entry point — this keeps
// the harness end-to-end honest and lets fixtures detect regressions in
// any pass (extract / framework / cross-lang / resolver / synthesis).
func runQuality(argv []string) error {
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

	// Output graph.json to a tempdir so we never pollute the fixture tree.
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
	if err := Index(srcDir, graphPath, fix.Name, nil /*skip*/, false /*pretty*/, false /*jsonStats*/); err != nil {
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
