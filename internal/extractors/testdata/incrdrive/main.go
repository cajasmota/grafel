// Command incrdrive runs exactly one extractors.TryIncremental pass against an
// already-indexed repo and prints the Result as JSON (issue #6199).
//
// It exists because TryIncremental has exactly one caller in the tree —
// cmd/grafel/daemon.go:1246 — so there is no way to time the incremental path
// without either standing up a daemon (which would mean touching the user's
// live one) or driving it directly. This drives it directly.
//
// One process = one pass, deliberately: a fresh process gives each pass a clean
// heap, so the GRAFEL_PHASE_TRACE heap peak is that pass's own and not the
// high-water mark of every pass before it.
//
//	go build -o bin/incrdrive internal/extractors/testdata/incrdrive/main.go
//	GRAFEL_PHASE_TRACE=/tmp/trace.jsonl bin/incrdrive -repo /tmp/fixture -state /tmp/state
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/cajasmota/grafel/internal/extractors"
)

func main() {
	repo := flag.String("repo", "", "repository path")
	state := flag.String("state", "", "state dir holding graph.fb + file-index.json")
	quiet := flag.Bool("quiet", true, "discard the incremental logger's own output")
	flag.Parse()
	if *repo == "" || *state == "" {
		fmt.Fprintln(os.Stderr, "incrdrive: -repo and -state are required")
		os.Exit(2)
	}

	sink := io.Writer(os.Stderr)
	if *quiet {
		sink = io.Discard
	}
	logger := log.New(sink, "", 0)

	res := extractors.TryIncremental(context.Background(), *repo, *state, logger, nil)

	out := map[string]any{
		"done":            res.Done,
		"fallback_reason": res.FallbackReason,
		"changed_files":   res.ChangedFiles,
		"duration_ms":     res.Duration.Milliseconds(),
	}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
}
