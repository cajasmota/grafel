package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/resolve"
)

// #7071 — the `resolver: guess-tier binds …` line is the ONLY output this
// whole change produces on a real run, and every stage feeding it was
// graded in internal/resolve while the wiring in index.go was not.
//
// That gap is not theoretical: FormatBindTiers returns "" for an empty
// tally and index.go guards the print on a non-empty string, so a broken
// merge prints NOTHING rather than zeros. Three separate mutants in this
// change have now lived on the reporting path (the References funnel,
// FormatBindTiers' total, MergeBindTiers) — this test is the one that
// closes the last stage.
//
// It also grades FINDING A end-to-end. `ResolveImports` runs BEFORE every
// other resolver, its stats are an ImportResolveStats rather than a
// resolve.Stats, and `MergeBindTierMap` is the SOLE route by which its two
// tiers reach this line. Deleting that one call is invisible to every test
// in internal/resolve.
//
// NOTE: this test mutates the process-global os.Stderr handle and therefore
// MUST NOT call t.Parallel(), for the reasons spelled out on
// TestExternalSynthesis_VerboseCounter above.

// captureIndexerStderr runs the indexer over repoPath and returns
// everything it wrote to stderr.
func captureIndexerStderr(t *testing.T, repoPath, tag string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w

	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 0, 1<<16)
		tmp := make([]byte, 4096)
		for {
			n, err := r.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if err != nil {
				break
			}
		}
		done <- string(buf)
	}()

	_ = runIndexerOn(t, repoPath, tag, nil)

	os.Stderr = origStderr
	w.Close()
	return <-done
}

// TestGuessTierReportLine_ReachesStderr_7071 indexes a repo whose only
// cross-file call can be bound by exactly one rung — the `from x import *`
// wildcard, which is the weakest tier in the enumeration and the one that
// reaches the report ONLY through MergeBindTierMap.
//
// VARIED: nothing. It is a single end-to-end trace, because what it grades
// is that the stages are WIRED, not how any stage behaves — each stage has
// its own unit grading in internal/resolve.
// HELD CONSTANT: everything; the assertion is on the literal substring the
// report line must contain.
func TestGuessTierReportLine_ReachesStderr_7071(t *testing.T) {
	repo := t.TempDir()
	// svc.py declares the target; app.py wildcard-imports it and calls it
	// by bare name. No explicit `from svc import handler` binding exists,
	// so rung 1 cannot answer and rung 2 has no plain `import svc` to
	// scan — the wildcard rung is the only route.
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("svc.py", "def handler():\n    return 1\n")
	write("app.py", "from svc import *\n\n\ndef main():\n    return handler()\n")

	stderr := captureIndexerStderr(t, repo, "bindtier7071")

	const marker = "resolver: guess-tier binds "
	i := strings.Index(stderr, marker)
	if i < 0 {
		t.Fatalf("the guess-tier report line is ABSENT from stderr.\n"+
			"This is the silent failure mode: FormatBindTiers returns \"\" for an empty tally "+
			"and index.go only prints a non-empty line, so a broken merge produces no output "+
			"at all rather than zeros.\nstderr was:\n%s", stderr)
	}
	line := stderr[i+len(marker):]
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	if !strings.Contains(line, string(resolve.BindTierImportWildcard)+"=") {
		t.Fatalf("report line %q does not name the wildcard import tier.\n"+
			"ResolveImports runs ahead of every other resolver and its stats are not a "+
			"resolve.Stats, so MergeBindTierMap in index.go is the SOLE route by which its "+
			"tiers reach this line — deleting that one call is invisible to every test in "+
			"internal/resolve.", line)
	}
	if !strings.Contains(line, "total=") {
		t.Fatalf("report line %q carries no total", line)
	}
}
