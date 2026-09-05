package links

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/substrate"
)

// unreadable_source_count_6839_test.go — #6839 arm 3 (group C).
//
// The nine remaining hardened source reads in this package keep their skip;
// what this file pins is that the skip is RECORDED. safeio's package doc
// forbids the silence, not the non-abort, so the terminal state is legitimate
// only while it is "bounded and REPORTED".
//
// Every assertion below lands on something a caller can actually see:
// PassResult.UnreadableSourceFiles and, for the whole set at once, the
// `unreadable_source_files` field of <group>-link-pass-stats.json. Arm 2's
// field shipped read by nothing and its deletion mutant stayed ALIVE until a
// test asserted the SERIALISED name; that is the mistake this file exists not
// to repeat.

// unreadableCountFixtureSrc is Go source that every group-C pass can sniff
// something out of (or at least read): a function with a definition, a use, a
// template literal and an HTTP-ish call. What it yields does not matter — the
// count is taken at the READ, above every sniffer's result.
const unreadableCountFixtureSrc = `package h

import "net/http"

func HandleX(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("id")
	_ = q
	http.Get("http://example.test/things/" + q)
}
`

// countFixtureGraph is the one-repo, one-file graph the table shares. Only the
// on-disk state of src/handler.go differs between the two directions.
func countFixtureGraph(repo, root string) repoGraph {
	return repoGraph{
		Repo:     repo,
		FileRoot: root,
		Entities: []entityNode{
			{
				ID: "fn1", Name: "HandleX", Kind: "SCOPE.Function",
				SourceFile: "src/handler.go", StartLine: 5, EndLine: 9,
			},
			// TWO function entities in ONE file, deliberately. The complexity
			// pass reads per ENTITY through a per-file cache, and its site
			// comment claims the cache holds the count to one per file rather
			// than one per entity in it. With a single entity that claim is
			// prose no test observes: deleting `cache[rel] = ""` on the
			// failure arm survives. With two, the mutant reports 2 and this
			// fixture reports 1. Every other group-C pass iterates a fileSet,
			// so the second entity is inert for them and the expected count
			// stays 1 across the whole table.
			{
				ID: "fn2", Name: "HandleY", Kind: "SCOPE.Function",
				SourceFile: "src/handler.go", StartLine: 11, EndLine: 15,
			},
		},
	}
}

// plantDirectoryAt makes rel unreadable BY CONSTRUCTION, with no FIFO and no
// socket: it creates a DIRECTORY where the pass expects a file. safeio.Open
// stats before it opens and refuses any non-regular file with ErrNotRegular,
// so the read fails without depending on file permissions (which do not stop
// root) and without leaving anything outside t.TempDir().
func plantDirectoryAt(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
		t.Fatalf("plant directory at %s: %v", rel, err)
	}
}

// groupCPasses is the derived site list: every hardened source read in this
// package EXCEPT string_pass (group A, propagates by design) and
// reachability.go (group B, fixed in #6863). Nine reads across eight passes —
// data_flow owns two of them, and both are covered here (the in-file read by
// this table, the cross-file callee read by
// TestCrossFileFlowUnreadableCalleeIsCounted).
var groupCPasses = []struct {
	name string
	run  func(t *testing.T, graphs []repoGraph, paths Paths) PassResult
}{
	{"constant_propagation", func(t *testing.T, graphs []repoGraph, paths Paths) PassResult {
		res, err := runConstantPropagationPass(graphs, paths, nil)
		if err != nil {
			t.Fatalf("runConstantPropagationPass: %v", err)
		}
		return res
	}},
	{"effect_propagation", func(t *testing.T, graphs []repoGraph, paths Paths) PassResult {
		res, err := runEffectPropagationPass(graphs, paths, nil)
		if err != nil {
			t.Fatalf("runEffectPropagationPass: %v", err)
		}
		return res
	}},
	{"taint_flow", func(t *testing.T, graphs []repoGraph, paths Paths) PassResult {
		res, err := runTaintFlowPass(graphs, paths, nil)
		if err != nil {
			t.Fatalf("runTaintFlowPass: %v", err)
		}
		return res
	}},
	{"payload_drift", func(t *testing.T, graphs []repoGraph, paths Paths) PassResult {
		res, err := runPayloadDriftPass("g", graphs, paths)
		if err != nil {
			t.Fatalf("runPayloadDriftPass: %v", err)
		}
		return res
	}},
	{"def_use", func(t *testing.T, graphs []repoGraph, paths Paths) PassResult {
		res, err := runDefUsePass(graphs, paths)
		if err != nil {
			t.Fatalf("runDefUsePass: %v", err)
		}
		return res
	}},
	{"template_patterns", func(t *testing.T, graphs []repoGraph, paths Paths) PassResult {
		res, err := runTemplatePatternPass(graphs, paths)
		if err != nil {
			t.Fatalf("runTemplatePatternPass: %v", err)
		}
		return res
	}},
	{"complexity", func(t *testing.T, graphs []repoGraph, paths Paths) PassResult {
		res, err := runComplexityPass(graphs, paths)
		if err != nil {
			t.Fatalf("runComplexityPass: %v", err)
		}
		return res
	}},
	{"data_flow", func(t *testing.T, graphs []repoGraph, paths Paths) PassResult {
		res, err := runDataFlowPass(graphs, paths, nil)
		if err != nil {
			t.Fatalf("runDataFlowPass: %v", err)
		}
		return res
	}},
}

// TestGroupCPassesCountAnUnreadableSourceFile is the #6839 arm-3 pin. Each
// pass keeps skipping the file — none of them returns an error, which is
// asserted by the runners above — but each must now REPORT the skip.
func TestGroupCPassesCountAnUnreadableSourceFile(t *testing.T) {
	for _, tc := range groupCPasses {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			plantDirectoryAt(t, root, "src/handler.go")

			graphs := []repoGraph{countFixtureGraph("repo-a", root)}
			paths := Paths{Links: filepath.Join(t.TempDir(), "g-links.json")}
			res := tc.run(t, graphs, paths)

			if res.UnreadableSourceFiles != 1 {
				t.Errorf("%s: PassResult.UnreadableSourceFiles: want 1 for one unreadable "+
					"source file, got %d — the skip is still silent", tc.name,
					res.UnreadableSourceFiles)
			}
		})
	}
}

// TestGroupCPassesCountNothingOnAReadableTree is the other direction. A fix
// that reports unconditionally satisfies the test above and is useless.
func TestGroupCPassesCountNothingOnAReadableTree(t *testing.T) {
	for _, tc := range groupCPasses {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "src/handler.go", unreadableCountFixtureSrc)

			graphs := []repoGraph{countFixtureGraph("repo-a", root)}
			paths := Paths{Links: filepath.Join(t.TempDir(), "g-links.json")}
			res := tc.run(t, graphs, paths)

			if res.UnreadableSourceFiles != 0 {
				t.Errorf("%s: PassResult.UnreadableSourceFiles: want 0 on a fully readable "+
					"tree, got %d — the counter fires unconditionally", tc.name,
					res.UnreadableSourceFiles)
			}
		})
	}
}

// TestGroupCPassesDoNotCountAnAbsentFile holds the third case the field's own
// doc distinguishes: a file the graph names but disk does not hold is an
// ANSWER, not a hidden read. Counting it would make the field fire on every
// ordinary re-index over a moved tree and stop meaning anything.
func TestGroupCPassesDoNotCountAnAbsentFile(t *testing.T) {
	for _, tc := range groupCPasses {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir() // exists, but holds no src/handler.go

			graphs := []repoGraph{countFixtureGraph("repo-a", root)}
			paths := Paths{Links: filepath.Join(t.TempDir(), "g-links.json")}
			res := tc.run(t, graphs, paths)

			if res.UnreadableSourceFiles != 0 {
				t.Errorf("%s: PassResult.UnreadableSourceFiles: want 0 for an absent file "+
					"under a root that exists, got %d", tc.name, res.UnreadableSourceFiles)
			}
		})
	}
}

// TestGroupCUnreadableCountReachesTheStatsSidecar asserts the EMITTED
// ARTEFACT: the serialised `unreadable_source_files` field an operator can
// read, under the exact JSON name it is documented with. Without this, a
// mutant deleting every write to the counter survives on the struct field
// alone — which is precisely how arm 2's field shipped read by nothing.
func TestGroupCUnreadableCountReachesTheStatsSidecar(t *testing.T) {
	root := t.TempDir()
	plantDirectoryAt(t, root, "src/handler.go")

	var results []PassResult
	for _, tc := range groupCPasses {
		graphs := []repoGraph{countFixtureGraph("repo-a", root)}
		paths := Paths{Links: filepath.Join(t.TempDir(), "g-links.json")}
		results = append(results, tc.run(t, graphs, paths))
	}

	statsPath := filepath.Join(t.TempDir(), "g-link-pass-stats.json")
	if err := writeLinkPassStats(statsPath, &RunResult{Group: "g", Results: results}); err != nil {
		t.Fatalf("writeLinkPassStats: %v", err)
	}
	buf, err := os.ReadFile(statsPath)
	if err != nil {
		t.Fatalf("stats sidecar: %v", err)
	}
	var stats struct {
		Passes []struct {
			Pass                  string `json:"pass"`
			UnreadableSourceFiles int    `json:"unreadable_source_files"`
		} `json:"passes"`
	}
	if err := json.Unmarshal(buf, &stats); err != nil {
		t.Fatalf("unmarshal stats sidecar: %v", err)
	}
	if len(stats.Passes) != len(groupCPasses) {
		t.Fatalf("stats sidecar: want %d pass rows, got %d", len(groupCPasses), len(stats.Passes))
	}
	for i, row := range stats.Passes {
		if row.UnreadableSourceFiles != 1 {
			t.Errorf("stats sidecar row %d (%s): unreadable_source_files want 1, got %d — "+
				"the count does not reach the serialised artefact", i, row.Pass,
				row.UnreadableSourceFiles)
		}
	}
}

// TestCrossFileFlowUnreadableCalleeIsCounted covers the data-flow pass's
// SECOND read (resolveCrossFileFlows), which the table above cannot reach: it
// only runs once a boundary resolves to a callee defined in another file.
// Driven directly, with the handler file readable and only the callee file
// planted as a directory, so a fix that counted the handler read twice would
// not pass.
func TestCrossFileFlowUnreadableCalleeIsCounted(t *testing.T) {
	if substrate.DataFlowContinueFor("go") == nil {
		t.Skip("no go data-flow continuation sniffer registered")
	}
	root := t.TempDir()
	writeFile(t, root, "src/handler.go", unreadableCountFixtureSrc)
	plantDirectoryAt(t, root, "src/helper.go")

	g := repoGraph{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: []entityNode{
			{ID: "h", Name: "HandleX", Kind: "SCOPE.Function", SourceFile: "src/handler.go"},
			{ID: "c", Name: "Helper", Kind: "SCOPE.Function", SourceFile: "src/helper.go"},
		},
		Edges: []edgeRef{{FromID: "h", ToID: "c", Kind: "CALLS"}},
	}
	boundaries := []substrate.DataFlowBoundary{{Function: "HandleX", Callee: "Helper", ArgIndex: 0}}

	flows, unreadable := resolveCrossFileFlows(&g, "src/handler.go", root, "go",
		newCrossFileResolver(&g), boundaries)
	if unreadable != 1 {
		t.Errorf("resolveCrossFileFlows: want 1 unreadable callee file, got %d", unreadable)
	}
	if len(flows) != 0 {
		t.Errorf("an unreadable callee cannot yield flows; got %d", len(flows))
	}

	// Other direction: the same call with a readable callee counts nothing.
	root2 := t.TempDir()
	writeFile(t, root2, "src/handler.go", unreadableCountFixtureSrc)
	writeFile(t, root2, "src/helper.go", unreadableCountFixtureSrc)
	g2 := g
	g2.FileRoot = root2
	if _, unreadable2 := resolveCrossFileFlows(&g2, "src/handler.go", root2, "go",
		newCrossFileResolver(&g2), boundaries); unreadable2 != 0 {
		t.Errorf("resolveCrossFileFlows on a readable tree: want 0, got %d", unreadable2)
	}
}

// TestDataFlowPassReportsTheCrossFileCalleeRead grades the CALL SITE that
// TestCrossFileFlowUnreadableCalleeIsCounted cannot: `unreadable +=
// xunreadable` in runDataFlowPass. That test drives resolveCrossFileFlows
// directly, so discarding the helper's return value at the call site leaves it
// green — site 9 of 9 would then never provably reach PassResult or the
// sidecar, which is the exact "written but never observed" shape this arm
// exists to end. Testing the helper does not pin the caller.
//
// The fixture is TestDataFlowPass_JSTS_CrossFile_DBWrite's: a handler in
// handlers.ts that calls save() defined in svc.ts. Only svc.ts is planted as a
// directory, so the handler file still reads and the cross-file hop is the
// thing that fails.
//
// EXPECTED 2, and the reason matters. src/svc.ts is unreadable from two
// distinct reads in one run: the pass's own top-level fileSet loop (svc.ts
// owns entities, so it is iterated in its own right) and the cross-file callee
// read inside resolveCrossFileFlows, whose cache is per-call. The counter
// counts READS, not distinct paths — see PassResult.UnreadableSourceFiles.
func TestDataFlowPassReportsTheCrossFileCalleeRead(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/handlers.ts", "\nimport { save } from './svc';\nfunction h(req, res) {\n  save(req.body.x);\n}\n")
	plantDirectoryAt(t, root, "src/svc.ts")

	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: []entityNode{
			{ID: "h", Name: "h", Kind: "function", SourceFile: "src/handlers.ts"},
			{ID: "save", Name: "save", Kind: "function", SourceFile: "src/svc.ts"},
			{ID: "create", Name: "create", Kind: "function", SourceFile: "src/svc.ts"},
		},
		Edges: []edgeRef{{FromID: "h", ToID: "save", Kind: "calls"}},
	}}

	linksPath := filepath.Join(t.TempDir(), "g-links.json")
	res, err := runDataFlowPass(graphs, Paths{Links: linksPath}, nil)
	if err != nil {
		t.Fatalf("runDataFlowPass: %v", err)
	}
	if res.UnreadableSourceFiles != 2 {
		t.Errorf("PassResult.UnreadableSourceFiles: want 2 (the fileSet read of svc.ts plus "+
			"the cross-file callee read of it), got %d — the cross-file count does not reach "+
			"PassResult", res.UnreadableSourceFiles)
	}

	// And it must survive into the serialised artefact from THIS pass run,
	// not only from the direct helper call.
	statsPath := filepath.Join(t.TempDir(), "g-link-pass-stats.json")
	if err := writeLinkPassStats(statsPath, &RunResult{Group: "g", Results: []PassResult{res}}); err != nil {
		t.Fatalf("writeLinkPassStats: %v", err)
	}
	buf, err := os.ReadFile(statsPath)
	if err != nil {
		t.Fatalf("stats sidecar: %v", err)
	}
	var stats struct {
		Passes []struct {
			Pass                  string `json:"pass"`
			UnreadableSourceFiles int    `json:"unreadable_source_files"`
		} `json:"passes"`
	}
	if err := json.Unmarshal(buf, &stats); err != nil {
		t.Fatalf("unmarshal stats sidecar: %v", err)
	}
	if len(stats.Passes) != 1 || stats.Passes[0].UnreadableSourceFiles != 2 {
		t.Errorf("link-pass-stats unreadable_source_files: want 2 for the data_flow pass, got %+v",
			stats.Passes)
	}

	// Other direction, on the same fixture: with svc.ts readable the pass
	// reports nothing. Without this a fix that counted unconditionally at the
	// call site would pass the assertions above.
	root2 := t.TempDir()
	writeFile(t, root2, "src/handlers.ts", "\nimport { save } from './svc';\nfunction h(req, res) {\n  save(req.body.x);\n}\n")
	writeFile(t, root2, "src/svc.ts", "\nexport function save(v) {\n  Model.create({ v });\n}\n")
	graphs2 := graphs
	graphs2[0].FileRoot = root2
	res2, err := runDataFlowPass(graphs2, Paths{Links: filepath.Join(t.TempDir(), "g-links.json")}, nil)
	if err != nil {
		t.Fatalf("runDataFlowPass (readable): %v", err)
	}
	if res2.UnreadableSourceFiles != 0 {
		t.Errorf("readable cross-file tree: want 0, got %d", res2.UnreadableSourceFiles)
	}
}
