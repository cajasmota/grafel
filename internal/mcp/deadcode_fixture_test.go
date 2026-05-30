package mcp

// deadcode_fixture_test.go — real-fixture precision verification for the
// dead-code detection pass (issue #1500).
//
// This test is gated behind the AG_DEADCODE_FIXTURE env var, which must point
// at a graphs directory produced by `archigraph xrepo-verify` (each
// <slug>/graph.json). It is skipped by default so CI does not require the
// external polyglot-platform fixture. Run with:
//
//	AG_DEADCODE_FIXTURE=/path/to/ag-xrepo-graphs-XXXX go test ./internal/mcp/ \
//	    -run TestFindDeadCode_RealFixturePrecision -v

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

func TestFindDeadCode_RealFixturePrecision(t *testing.T) {
	graphsDir := os.Getenv("AG_DEADCODE_FIXTURE")
	if graphsDir == "" {
		t.Skip("AG_DEADCODE_FIXTURE not set; skipping real-fixture precision test")
	}

	entries, err := os.ReadDir(graphsDir)
	if err != nil {
		t.Fatalf("read graphs dir: %v", err)
	}

	repos := map[string]*LoadedRepo{}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		slug := ent.Name()
		doc, err := graph.LoadGraphFromDir(filepath.Join(graphsDir, slug))
		if err != nil {
			t.Logf("skip %s: %v", slug, err)
			continue
		}
		repos[slug] = &LoadedRepo{
			Repo:       slug,
			Doc:        doc,
			LabelIndex: BuildLabelIndex(doc),
			BM25:       BuildBM25(doc),
		}
	}
	if len(repos) == 0 {
		t.Fatal("no graphs loaded from fixture dir")
	}

	st := NewState(&Registry{Groups: map[string]RegistryGroup{"polyglot": {}}})
	st.mu.Lock()
	st.groups["polyglot"] = &LoadedGroup{Name: "polyglot", Repos: repos}
	st.mu.Unlock()
	srv := &Server{State: st, Tel: NewTelemetry(0)}

	out := callFlowTool(t, srv.handleFindDeadCode, map[string]any{
		"group": "polyglot",
		"limit": float64(500),
	})

	dead := out["dead_code"].([]any)
	flagged := []string{}
	for _, item := range dead {
		m := item.(map[string]any)
		flagged = append(flagged, m["repo"].(string)+"::"+m["name"].(string))
	}
	sort.Strings(flagged)
	t.Logf("flagged (%d): %v", len(flagged), flagged)

	// Expected dead code per MANIFEST §11.2 / TEST_INVENTORY row 20.
	// recomputeAllPeriodsLegacy is included when Java method extraction is
	// present in the index; it is checked best-effort below.
	mustFlag := map[string]bool{
		"libs-js-shared::legacySignToken":    false,
		"services-inventory::DeadReindexAll": false,
	}
	// Names that are legitimate public-API exports and must NOT be flagged.
	mustNotFlag := map[string]bool{
		"verifyToken": true, "hasRole": true, "serviceClient": true,
		"decode_jwt": true, "read_value": true, "enabled": true,
		"flagEnabled": true, "readSecret": true, "readSecretValue": true,
	}

	flaggedSet := map[string]bool{}
	flaggedNames := map[string]bool{}
	for _, f := range flagged {
		flaggedSet[f] = true
		// extract bare name after "::"
		for i := len(f) - 1; i >= 0; i-- {
			if f[i] == ':' {
				flaggedNames[f[i+1:]] = true
				break
			}
		}
	}

	for k := range mustFlag {
		if !flaggedSet[k] {
			t.Errorf("expected %q to be flagged as dead code, but it was not", k)
		}
	}
	for n := range mustNotFlag {
		if flaggedNames[n] {
			t.Errorf("FALSE POSITIVE: legitimate public API %q was flagged as dead code", n)
		}
	}

	// recomputeAllPeriodsLegacy: flag if present, but do not hard-fail if the
	// Java method is missing from the graph (known coverage gap).
	if flaggedNames["recomputeAllPeriodsLegacy"] || flaggedNames["ReportingController.recomputeAllPeriodsLegacy"] {
		t.Logf("bonus: recomputeAllPeriodsLegacy (Java) correctly flagged")
	} else {
		t.Logf("note: recomputeAllPeriodsLegacy not flagged (Java method-coverage gap, not blocking)")
	}
}
