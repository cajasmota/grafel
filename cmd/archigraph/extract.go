package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/daemon/extract"
)

// subprocExtract reports whether the indexer should route per-file
// passes (Pass 1, 2.5, 3) through the Phase F subprocess coordinator.
// Gated on ARCHIGRAPH_SUBPROC_EXTRACT=1 during the rollout so the
// in-process path stays the default until benchmarks + quality
// fixtures confirm byte-identical output.
func subprocExtract() bool {
	v := strings.TrimSpace(os.Getenv("ARCHIGRAPH_SUBPROC_EXTRACT"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// subprocConcurrency overrides the coordinator's default subprocess
// fan-out (NumCPU/2 capped at 4). Zero means use the default.
func subprocConcurrency() int {
	v := strings.TrimSpace(os.Getenv("ARCHIGRAPH_SUBPROC_CONCURRENCY"))
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// subprocBatchSize overrides the coordinator's default batch size of
// 80 files per subprocess. Zero means use the default.
func subprocBatchSize() int {
	v := strings.TrimSpace(os.Getenv("ARCHIGRAPH_SUBPROC_BATCH_SIZE"))
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// skipPassNames flattens the indexer's skip-set into the slice the
// coordinator forwards to each subprocess via --skip-pass.
func skipPassNames(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		if set[k] {
			out = append(out, k)
		}
	}
	return out
}

// runExtractSubprocess implements the `archigraph extract` hidden
// subcommand. It is the entrypoint forked by the daemon-side
// coordinator (Phase F): same binary, different argv, short-lived
// process. Memory bound per spec: ~80-150MB per subprocess.
//
// argv is the slice AFTER the subcommand name was stripped by the
// cobra-side hook (see internal/cli/extract.go).
func runExtractSubprocess(argv []string) error {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var (
		repo       = fs.String("repo", "", "absolute repo root path")
		lang       = fs.String("lang", "", "single language filter (optional)")
		batch      = fs.String("batch", "", "path to newline-delimited batch file")
		batchID    = fs.String("batch-id", "", "label propagated into stats")
		skipPasses = fs.String("skip-pass", "", "comma-separated pass names to skip")
		drfNames   = fs.String("drf-names", "", "path to coordinator-written DRF register-name file (#1292)")
		ormFields  = fs.String("orm-fields", "", "path to coordinator-written ORM field-name file (#2505)")
	)
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *repo == "" || *batch == "" {
		return fmt.Errorf("extract: --repo and --batch are required")
	}

	skipSet := map[string]bool{}
	for _, p := range strings.Split(*skipPasses, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			skipSet[p] = true
		}
	}

	return extract.Run(context.Background(), extract.SubprocessOptions{
		RepoRoot:      *repo,
		Language:      *lang,
		BatchPath:     *batch,
		BatchID:       *batchID,
		Output:        os.Stdout,
		SkipPasses:    skipSet,
		DRFNamesPath:  *drfNames,
		ORMFieldsPath: *ormFields,
	})
}
