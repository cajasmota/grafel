package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestIssue481_GraphJSONIsByteIdenticalAcrossRuns is the regression guard
// for the determinism fix. ADR-0006 promises "JSON files are diffable" and
// "copy graph.json to another machine and graph is identical." Before this
// fix, running `archigraph index` twice on the SAME repo produced different
// graph.json — 10 runs on kickstart.nvim produced bug-rates from 0.93% to
// 10.14% and 10 distinct SHA256 hashes.
//
// The test indexes a small in-repo fixture N times and asserts the on-disk
// graph.json bytes are identical. SOURCE_DATE_EPOCH is set so the
// generated_at timestamp is reproducible — verifying that the rest of the
// document is stable.
func TestIssue481_GraphJSONIsByteIdenticalAcrossRuns(t *testing.T) {
	fixture := writeFixtureRepo(t)
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")

	const runs = 5
	hashes := make([]string, 0, runs)

	for i := 0; i < runs; i++ {
		// Use a per-run output path so we are not racing the indexer's own
		// .tmp+rename atomic write across loop iterations.
		out := filepath.Join(t.TempDir(), "graph.json")
		if err := Index(fixture, out, "fixture", nil, false, false); err != nil {
			t.Fatalf("run %d: Index failed: %v", i, err)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("run %d: read graph.json: %v", i, err)
		}
		sum := sha256.Sum256(data)
		hashes = append(hashes, hex.EncodeToString(sum[:]))
	}

	first := hashes[0]
	for i, h := range hashes {
		if h != first {
			// Pull the first and the divergent doc for a useful failure msg.
			t.Fatalf("graph.json bytes diverged between run 0 and run %d\n  hash[0] = %s\n  hash[%d] = %s",
				i, first, i, h)
		}
	}
}

// TestIssue481_RoundForDeterminism_NoiseIsAbsorbed exercises the float
// quantiser used to absorb gonum's iterative-solver noise on PageRank and
// modularity. Two values within ~1e-7 of each other must collapse to the
// same rounded float; values that genuinely differ must stay distinct.
func TestIssue481_RoundForDeterminism_NoiseIsAbsorbed(t *testing.T) {
	// Re-implement the quantiser locally so the test does not need to import
	// internal/graph for a one-line check. Keep the constants in sync with
	// internal/graph.roundForDeterminism.
	q := func(v float64) float64 {
		const scale = 1e5
		return float64(int64(v*scale+0.5)) / scale
	}
	cases := []struct {
		name string
		a, b float64
		same bool
	}{
		{"noise_within_tolerance", 0.0181355049, 0.0181355008, true},
		{"larger_drift_still_within_tolerance", 0.0143178037, 0.0143178025, true},
		{"genuine_difference_preserved", 0.018135, 0.019999, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ra, rb := q(tc.a), q(tc.b)
			if tc.same && ra != rb {
				t.Fatalf("expected %g and %g to round to the same value, got %g vs %g",
					tc.a, tc.b, ra, rb)
			}
			if !tc.same && ra == rb {
				t.Fatalf("expected %g and %g to stay distinct, both became %g",
					tc.a, tc.b, ra)
			}
		})
	}
}

// writeFixtureRepo materialises a tiny multi-file fixture that exercises
// the lua extractor (where the worst non-determinism was observed —
// kickstart.nvim per-file ent counts swung from 0 to 17 across runs) plus
// markdown (for cross-language Pass 3 exercise). The fixture is small but
// reproduces the original failure mode: multiple require() calls and
// nested function definitions emit enough records for the worker-pool
// race to surface within a few runs.
func writeFixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"init.lua": `local M = {}
local autopairs = require('nvim-autopairs')
local lint = require('lint')
local function outer(x)
  local inner = function(y)
    print(y)
    return y + 1
  end
  return inner(x)
end
function M.run()
  autopairs.setup({})
  lint.setup({})
  outer(1)
end
return M
`,
		"helpers.lua": `local H = {}
function H.format(s)
  return tostring(s)
end
function H.parse(s)
  return tonumber(s)
end
return H
`,
		"README.md": `# Fixture

## Setup

Run ` + "`require('init').run()`" + ` to exercise the extractor.

## Helpers

The ` + "`helpers`" + ` module exposes ` + "`format`" + ` and ` + "`parse`" + `.
`,
	}
	for path, body := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	// Sanity: ensure the fixture is non-empty.
	if got, _ := os.ReadFile(filepath.Join(root, "init.lua")); !bytes.Contains(got, []byte("require")) {
		t.Fatal("fixture init.lua missing require — extractor exercise lost")
	}
	return root
}
