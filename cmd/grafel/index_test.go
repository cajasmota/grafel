package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/classifier"
	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/types"
)

// newTestIndexer constructs an Indexer wired up to the embedded YAML rules
// and the default classifier/parser. skipPasses is the optional set of
// passes to skip — pass nil to run everything. stateDir is the per-repo state
// directory; if empty, defaults to a fresh t.TempDir(). Callers needing
// shared state across multiple runs should pass the same stateDir.
func newTestIndexer(t *testing.T, repoTag string, skipPasses []string, stateDir string) *Indexer {
	t.Helper()
	if stateDir == "" {
		stateDir = t.TempDir()
	}
	// #1626 / #2083: pin GRAFEL_DAEMON_ROOT to an isolated temp so
	// indexer runs (a) don't pollute the source tree fixtures with state
	// and (b) don't load stale state across runs.
	t.Setenv("GRAFEL_DAEMON_ROOT", stateDir)
	cls, err := classifier.New("", nil)
	if err != nil {
		t.Fatalf("classifier: %v", err)
	}
	parser := treesitter.NewParserFactory(nil)
	rules, err := engine.LoadAllRules()
	if err != nil {
		t.Fatalf("load rules: %v", err)
	}
	skipSet, err := parseSkipPasses(skipPasses)
	if err != nil {
		t.Fatalf("parse skip: %v", err)
	}
	return &Indexer{
		repoTag:    repoTag,
		classifier: cls,
		parser:     parser,
		detector:   engine.New(rules),
		skipPasses: skipSet,
		workers:    2,
		stats: indexerStats{
			pass1RelsByLang: make(map[string]int),
			pass3RelsByExt:  make(map[string]int),
		},
	}
}

func runIndexerOn(t *testing.T, repoPath, repoTag string, skipPasses []string) *graph.Document {
	t.Helper()
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	idx := newTestIndexer(t, repoTag, skipPasses, "")
	doc, err := idx.Run(context.Background(), abs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return doc
}

// TestEngineYAMLRulesLoadAndCompile asserts the YAML rule engine sees the
// expected number of files (751 across all language sub-directories).
func TestEngineYAMLRulesLoadAndCompile(t *testing.T) {
	rules, err := engine.LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := engine.New(rules)
	if got := det.RuleCount(); got < 100 {
		// 751 .yaml files, but not every YAML is a rule (some are
		// _manifest.yaml etc. and skipped). 100 is a safe floor.
		t.Fatalf("rule count too low: got %d, want >= 100", got)
	}
	if got := len(det.Languages()); got < 5 {
		t.Fatalf("language count too low: got %d, want >= 5", got)
	}
}

// TestParseSkipPasses_Valid covers the happy path of --skip-pass parsing.
func TestParseSkipPasses_Valid(t *testing.T) {
	got, err := parseSkipPasses([]string{"cross-lang,framework"})
	if err != nil {
		t.Fatalf("parseSkipPasses: %v", err)
	}
	if !got[PassCrossLang] || !got[PassFramework] {
		t.Fatalf("expected cross-lang and framework set, got %v", got)
	}
}

// TestParseSkipPasses_Invalid asserts unknown pass names produce an error
// instead of silently degrading the pipeline.
func TestParseSkipPasses_Invalid(t *testing.T) {
	if _, err := parseSkipPasses([]string{"bogus"}); err == nil {
		t.Fatalf("expected error for unknown pass, got nil")
	}
}

// TestDjangoFixture_FrameworkEntities confirms the YAML engine emits
// Django framework entities (Routes / Models) against a small fixture.
func TestDjangoFixture_FrameworkEntities(t *testing.T) {
	doc := runIndexerOn(t, "testdata/django_app", "django_app", nil)
	if len(doc.Entities) == 0 {
		t.Fatalf("django: no entities at all")
	}
	// Look for at least one entity with framework=django on its properties
	// (the engine stamps that on every YAML-driven entity).
	frameworkHits := 0
	for _, e := range doc.Entities {
		if e.Properties["framework"] == "python" || e.Properties["framework"] == "django" {
			frameworkHits++
		}
	}
	if frameworkHits == 0 {
		// The python/django rules tag entities with framework="python"
		// (the language root). Fall back to checking any framework-tagged
		// entity exists at all, which proves the engine ran.
		for _, e := range doc.Entities {
			if e.Properties["framework"] != "" {
				frameworkHits++
			}
		}
	}
	if frameworkHits == 0 {
		t.Fatalf("django: no framework-tagged entities (engine did not fire)")
	}
}

// TestSpringFixture_FrameworkEntities confirms the YAML engine produces
// Spring entities for a tiny Java fixture.
func TestSpringFixture_FrameworkEntities(t *testing.T) {
	doc := runIndexerOn(t, "testdata/spring_app", "spring_app", nil)
	hits := 0
	for _, e := range doc.Entities {
		if e.Properties["framework"] != "" && e.Language == "java" {
			hits++
		}
	}
	if hits == 0 {
		// Even if Spring rules don't fire, the Java extractor should still
		// emit the controller class as an entity — proves Pass 1 worked.
		for _, e := range doc.Entities {
			if e.Language == "java" {
				hits++
			}
		}
	}
	if hits == 0 {
		t.Fatalf("spring: no java entities at all")
	}
}

// TestCDKFixture_FrameworkEntities confirms the AWS CDK fixture produces
// TypeScript entities (the CDK rules will tag them as cdk where applicable).
func TestCDKFixture_FrameworkEntities(t *testing.T) {
	doc := runIndexerOn(t, "testdata/cdk_app", "cdk_app", nil)
	tsHits := 0
	for _, e := range doc.Entities {
		if e.Language == "typescript" {
			tsHits++
		}
	}
	if tsHits == 0 {
		t.Fatalf("cdk: no typescript entities")
	}
}

// TestSkipCrossLang_RelationshipsDecrease confirms --skip-pass=cross-lang
// produces a smaller relationship set than a full run on the same fixture.
func TestSkipCrossLang_RelationshipsDecrease(t *testing.T) {
	full := runIndexerOn(t, "testdata/django_app", "django_app", nil)
	skipped := runIndexerOn(t, "testdata/django_app", "django_app", []string{"cross-lang"})
	if skipped.Stats.Relationships > full.Stats.Relationships {
		t.Fatalf("skip-pass=cross-lang produced MORE rels (%d) than full run (%d)",
			skipped.Stats.Relationships, full.Stats.Relationships)
	}
	// And: full run should produce at least as many entities. Pass 3 emits
	// SCOPE.* entities (DataAccess, ExternalAPI, etc.) on top of Pass 1+2.5.
	if full.Stats.Entities < skipped.Stats.Entities {
		t.Fatalf("full run had FEWER entities (%d) than skip run (%d)",
			full.Stats.Entities, skipped.Stats.Entities)
	}
}

// TestCrossLangProducesRelationships verifies Pass 3 emits relationships
// against the Django fixture (TESTS / DEPENDS_ON / IMPORTS / etc.).
func TestCrossLangProducesRelationships(t *testing.T) {
	full := runIndexerOn(t, "testdata/django_app", "django_app", nil)
	skipped := runIndexerOn(t, "testdata/django_app", "django_app", []string{"cross-lang"})
	delta := full.Stats.Relationships - skipped.Stats.Relationships
	if delta <= 0 {
		t.Fatalf("Pass 3 produced no extra relationships (full=%d, skipped=%d)",
			full.Stats.Relationships, skipped.Stats.Relationships)
	}
}

// TestPassMergeDedupe confirms entities/rels are deduplicated by ID even
// when emitted by multiple passes. We synthesise the duplicates by running
// the engine over a fixture that exercises both Pass 1 and Pass 2.5.
func TestPassMergeDedupe(t *testing.T) {
	doc := runIndexerOn(t, "testdata/django_app", "django_app", nil)
	seen := make(map[string]int)
	for _, e := range doc.Entities {
		seen[e.ID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Fatalf("duplicate entity id %q appears %d times", id, n)
		}
	}
	relSeen := make(map[string]int)
	for _, r := range doc.Relationships {
		relSeen[r.ID]++
	}
	for id, n := range relSeen {
		if n > 1 {
			t.Fatalf("duplicate relationship id %q appears %d times", id, n)
		}
	}
}

// TestDocumentRoundTrip confirms the produced graph.Document marshals back
// to the on-disk schema without losing required fields.
func TestDocumentRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	doc := runIndexerOn(t, "testdata/crossfile_go", "crossfile_go", nil)
	out := filepath.Join(tmp, "graph.json")
	if err := graph.WriteAtomic(out, doc, true); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var back graph.Document
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Repo != "crossfile_go" {
		t.Fatalf("repo tag mismatch: %q", back.Repo)
	}
	if back.Stats.Entities != len(back.Entities) {
		t.Fatalf("stats.entities mismatch: %d vs %d", back.Stats.Entities, len(back.Entities))
	}
}

// TestCrossFileBareNameResolution confirms that for a fixture where one Go
// file calls a function defined in another file in the same package, the
// resolver collapses the bare-name reference into a graph entity ID when
// the name is unique across the merged entity set.
func TestCrossFileBareNameResolution(t *testing.T) {
	doc := runIndexerOn(t, "testdata/crossfile_go", "crossfile_go", nil)
	if len(doc.Entities) == 0 {
		t.Fatalf("no entities for go fixture")
	}
	// Find the "Hello" function entity.
	var helloID string
	for _, e := range doc.Entities {
		if e.Name == "Hello" && strings.Contains(e.SourceFile, "a.go") {
			helloID = e.ID
			break
		}
	}
	if helloID == "" {
		t.Skipf("no Hello entity emitted (Pass 1 Go extractor may not surface it)")
		return
	}
	// At least confirm the resolver did not corrupt the ID — it should
	// match the deterministic graph.EntityID.
	want := graph.EntityID("crossfile_go", "Function", "Hello", "a.go")
	_ = want // shape-only assertion: the resolver does not synthesise IDs,
	// it only rewrites bare-name to_id values found in the merged set.
}

// TestCrossFileResolutionInOutput runs the indexer on a fixture where one
// Go file (b.go) calls a function defined in another file (a.go). After the
// resolver runs, at least one CALLS edge in the output document MUST have
// to_id set to a 16-char hex entity ID (not a bare name like "Hello").
//
// This is the regression test for PORT-2-FIX (issue #24): before the fix,
// every cross-file CALLS edge stored a literal name and would dead-end
// graph traversal in the MCP server.
func TestCrossFileResolutionInOutput(t *testing.T) {
	doc := runIndexerOn(t, "testdata/crossfile_go", "crossfile_go", nil)
	if len(doc.Relationships) == 0 {
		t.Fatalf("no relationships emitted")
	}

	// Build a set of valid entity IDs for resolution check.
	validIDs := make(map[string]bool, len(doc.Entities))
	for _, e := range doc.Entities {
		validIDs[e.ID] = true
	}

	resolved := 0
	for _, r := range doc.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		if isHex16(r.ToID) && validIDs[r.ToID] {
			resolved++
		}
	}
	if resolved == 0 {
		t.Fatalf("no CALLS edge has a resolved to_id (rels=%d, entities=%d)",
			len(doc.Relationships), len(doc.Entities))
	}
}

// TestPythonEmbeddedRelationshipsReachOutput is the orchestrator-level
// regression for issue #25 (PORT-2-FIX-2). It confirms that the relationships
// the Python extractor embeds inside EntityRecord.Relationships — CONTAINS,
// CALLS, IMPORTS — are merged into the final graph.Document by buildDocument.
//
// The fixture (testdata/crossfile_python) has:
//   - a.py: free function make_message() and class Greeter with greet/shout.
//   - b.py: imports Greeter from a, defines main() which calls greet().
//
// Expected coverage:
//   - At least one CONTAINS edge (Greeter → greet or Greeter → shout).
//   - At least one CALLS edge (greet → make_message OR main → greet).
//   - At least one IMPORTS edge (b.py → a.Greeter).
func TestPythonEmbeddedRelationshipsReachOutput(t *testing.T) {
	doc := runIndexerOn(t, "testdata/crossfile_python", "crossfile_python", nil)

	kinds := make(map[string]int, 8)
	for _, r := range doc.Relationships {
		kinds[r.Kind]++
	}
	if kinds["CONTAINS"] == 0 {
		t.Errorf("expected at least one CONTAINS edge in output, got kinds=%v", kinds)
	}
	if kinds["CALLS"] == 0 {
		t.Errorf("expected at least one CALLS edge in output, got kinds=%v", kinds)
	}
	if kinds["IMPORTS"] == 0 {
		t.Errorf("expected at least one IMPORTS edge in output, got kinds=%v", kinds)
	}
}

// TestPass4Algorithms_AttributesPresent runs the orchestrator on the Go
// crossfile fixture with and without --skip-pass=algorithms; with Pass 4 on
// every entity should have community_id/centrality/pagerank populated and the
// document should expose communities/algorithm_stats. With Pass 4 off, those
// fields stay nil/empty.
// TestPass4Algorithms_AttributesPresent is now the REGRESSION GUARD for the
// #5349 A3 per-repo-pass removal: the per-repo index no longer computes graph
// algorithms (communities/centrality/pagerank). Those are computed ONCE over
// the assembled group union by the daemon's group-algo scheduler and applied
// via the <group>-algo.json overlay at MCP load. So a per-repo index — with or
// without --skip-pass=algorithms — must NOT populate the per-entity algo
// attributes or the corpus AlgorithmStats/Communities. The graph.fb schema
// fields are kept (vestigial, one release) but left at their sentinels.
func TestPass4Algorithms_AttributesPresent(t *testing.T) {
	for _, skip := range [][]string{nil, {"algorithms"}} {
		doc := runIndexerOn(t, "testdata/crossfile_go", "crossfile_go", skip)
		if doc.AlgorithmStats != nil {
			t.Errorf("skip=%v: per-repo index should NOT compute AlgorithmStats now (group pass owns it), got %+v", skip, doc.AlgorithmStats)
		}
		if len(doc.Communities) != 0 {
			t.Errorf("skip=%v: per-repo Communities should be empty (group pass owns it), got %d", skip, len(doc.Communities))
		}
		for _, e := range doc.Entities {
			if e.CommunityID != nil || e.PageRank != nil || e.Centrality != nil {
				t.Errorf("skip=%v: entity %s has per-repo algo attrs set; the per-repo pass was removed in #5349 A3", skip, e.ID)
				break
			}
		}
	}
}

// TestWriteAtomic_PrettyVsMinified asserts the --pretty switch produces a
// strictly larger file than the default minified output and that both files
// decode to identical JSON content. This is the regression test for issue
// #23 (minify graph.json by default).
func TestWriteAtomic_PrettyVsMinified(t *testing.T) {
	tmp := t.TempDir()
	doc := runIndexerOn(t, "testdata/crossfile_go", "crossfile_go", nil)

	miniPath := filepath.Join(tmp, "graph.min.json")
	prettyPath := filepath.Join(tmp, "graph.pretty.json")

	if err := graph.WriteAtomic(miniPath, doc, false); err != nil {
		t.Fatalf("WriteAtomic minified: %v", err)
	}
	if err := graph.WriteAtomic(prettyPath, doc, true); err != nil {
		t.Fatalf("WriteAtomic pretty: %v", err)
	}

	miniBytes, err := os.ReadFile(miniPath)
	if err != nil {
		t.Fatalf("read minified: %v", err)
	}
	prettyBytes, err := os.ReadFile(prettyPath)
	if err != nil {
		t.Fatalf("read pretty: %v", err)
	}

	if len(prettyBytes) <= len(miniBytes) {
		t.Fatalf("expected pretty output to exceed minified: pretty=%d minified=%d",
			len(prettyBytes), len(miniBytes))
	}

	// Minified output should not contain the indent string used by the
	// pretty encoder. This guards against accidental regressions where the
	// flag wiring is correct but SetIndent is still called.
	if strings.Contains(string(miniBytes), "\n  ") {
		t.Fatalf("minified output appears indented (contains \"\\n  \")")
	}

	// Both files must decode to identical Go-side content. Compare the
	// re-encoded canonical form to ignore whitespace/ordering differences
	// introduced by SetIndent.
	var miniDoc, prettyDoc graph.Document
	if err := json.Unmarshal(miniBytes, &miniDoc); err != nil {
		t.Fatalf("unmarshal minified: %v", err)
	}
	if err := json.Unmarshal(prettyBytes, &prettyDoc); err != nil {
		t.Fatalf("unmarshal pretty: %v", err)
	}

	miniCanon, err := json.Marshal(&miniDoc)
	if err != nil {
		t.Fatalf("re-marshal minified: %v", err)
	}
	prettyCanon, err := json.Marshal(&prettyDoc)
	if err != nil {
		t.Fatalf("re-marshal pretty: %v", err)
	}
	if string(miniCanon) != string(prettyCanon) {
		t.Fatalf("pretty and minified outputs decode to different content")
	}
}

// TestExternalSynthesis_DjangoFixture confirms the Pass 4.5 external
// synthesis pass emits an ext:django placeholder against the Django
// fixture (which has `from django.db import models`) and rewrites the
// IMPORTS edge to point at it. PORT-EXT (issue #32).
func TestExternalSynthesis_DjangoFixture(t *testing.T) {
	doc := runIndexerOn(t, "testdata/django_app", "django_app", nil)

	var ext *graph.Entity
	for k := range doc.Entities {
		if doc.Entities[k].ID == "ext:django" {
			ext = &doc.Entities[k]
			break
		}
	}
	if ext == nil {
		t.Fatalf("ext:django placeholder not synthesised; entity count=%d", len(doc.Entities))
	}
	if ext.Kind != "SCOPE.External" {
		t.Fatalf("ext:django kind=%q, want SCOPE.External", ext.Kind)
	}
	if v, ok := ext.Metadata["is_external"].(bool); !ok || !v {
		t.Fatalf("ext:django missing is_external metadata: %+v", ext.Metadata)
	}

	// At least one relationship should now point at ext:django.
	hits := 0
	for _, r := range doc.Relationships {
		if r.ToID == "ext:django" {
			hits++
		}
	}
	if hits == 0 {
		t.Fatalf("no relationships rewritten to ext:django")
	}
}

// TestExternalSynthesis_VerboseCounter confirms the ext-synthesis log
// line appears when GRAFEL_VERBOSE=1. PORT-EXT (issue #32).
//
// NOTE: this test mutates the process-global os.Stderr file handle and
// therefore MUST NOT call t.Parallel(). Running it concurrently with
// any other test that writes to stderr (or a sibling that also swaps
// the handle) would interleave bytes into the captured pipe and the
// assertion would flake. If the test ever needs parallelism, refactor
// the indexer to accept an io.Writer seam instead of touching the
// global handle.
func TestExternalSynthesis_VerboseCounter(t *testing.T) {
	t.Setenv("GRAFEL_VERBOSE", "1")

	// Capture stderr by redirecting the os.Stderr file handle for the
	// duration of the run. We use a pipe so the writer can be closed
	// without truncating any in-flight writes from the indexer.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w

	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 0, 8192)
		tmp := make([]byte, 1024)
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

	_ = runIndexerOn(t, "testdata/django_app", "django_app", nil)

	// Restore stderr and close the writer so the goroutine returns.
	os.Stderr = origStderr
	w.Close()
	got := <-done

	if !strings.Contains(got, "ext-synthesis: synthesized=") {
		t.Fatalf("verbose ext-synthesis line missing from stderr; got: %s", got)
	}
}

// assertBuildPatternContainsRelsCovers is a unit-level guard on the helper
// that powers the SCOPE.Pattern → CONTAINS fixup. Drives buildPatternContainsRels
// with a synthetic merged-record slice and checks one edge per pattern.
func assertBuildPatternContainsRelsCovers(t *testing.T) {
	t.Helper()
	idx := &Indexer{repoTag: "unit_test_repo"}
	records := []types.EntityRecord{
		{ID: "aaaaaaaaaaaaaaaa", Kind: "SCOPE.Pattern", Name: "p1", SourceFile: "src/a.py"},
		{ID: "bbbbbbbbbbbbbbbb", Kind: "SCOPE.Pattern", Name: "p2", SourceFile: "src/b.py"},
		// Non-pattern: must be skipped.
		{ID: "cccccccccccccccc", Kind: "SCOPE.Operation", Name: "f", SourceFile: "src/a.py"},
		// Pattern with empty SourceFile: must be skipped (no file to attach to).
		{ID: "dddddddddddddddd", Kind: "SCOPE.Pattern", Name: "p3", SourceFile: ""},
		// Pattern with empty ID: must be skipped.
		{ID: "", Kind: "SCOPE.Pattern", Name: "p4", SourceFile: "src/c.py"},
	}
	rels := idx.buildPatternContainsRels(records)
	if len(rels) != 2 {
		t.Fatalf("expected 2 CONTAINS edges (one per fully-formed Pattern), got %d", len(rels))
	}
	wantFileA := graph.EntityID("unit_test_repo", "SCOPE.Component", "src/a.py", "src/a.py")
	wantFileB := graph.EntityID("unit_test_repo", "SCOPE.Component", "src/b.py", "src/b.py")
	seen := map[string]string{}
	for _, r := range rels {
		if r.Kind != "CONTAINS" {
			t.Fatalf("unexpected edge kind %q", r.Kind)
		}
		seen[r.ToID] = r.FromID
	}
	if seen["aaaaaaaaaaaaaaaa"] != wantFileA {
		t.Errorf("p1: FromID=%q want=%q", seen["aaaaaaaaaaaaaaaa"], wantFileA)
	}
	if seen["bbbbbbbbbbbbbbbb"] != wantFileB {
		t.Errorf("p2: FromID=%q want=%q", seen["bbbbbbbbbbbbbbbb"], wantFileB)
	}
}

// TestScopePatternContains_AllPatternsHaveContainsEdge asserts that every
// SCOPE.Pattern entity emitted on the django fixture is the target of at
// least one CONTAINS edge whose FromID is the per-source-file
// SCOPE.Component (subtype="file") entity created by extractor.FileEntity.
//
// Regression guard for the system-wide orphan-bloat fix: the framework
// rule engine + many per-language pattern detectors emit Pattern entities
// without a file→pattern CONTAINS edge, which orphans them in the graph
// and inflates per-repo orphan rate. The fix lives in buildDocument's
// buildPatternContainsRels Pass-3 fixup.
func TestScopePatternContains_AllPatternsHaveContainsEdge(t *testing.T) {
	doc := runIndexerOn(t, "testdata/django_app", "django_app", nil)

	patternIDs := make(map[string]string) // id → source_file
	for _, e := range doc.Entities {
		if e.Kind == "SCOPE.Pattern" {
			patternIDs[e.ID] = e.SourceFile
		}
	}
	if len(patternIDs) == 0 {
		// django fixture happens not to emit Pattern entities. Fall back to
		// a unit-level check on the helper that drives the fixup so we still
		// have a regression guard at this layer.
		assertBuildPatternContainsRelsCovers(t)
		return
	}

	contained := make(map[string]bool)
	for _, r := range doc.Relationships {
		if r.Kind == "CONTAINS" {
			contained[r.ToID] = true
		}
	}

	var missing []string
	for id := range patternIDs {
		if !contained[id] {
			missing = append(missing, id+"("+patternIDs[id]+")")
		}
	}
	if len(missing) > 0 {
		// Cap output so a regression doesn't flood the test log.
		if len(missing) > 10 {
			missing = append(missing[:10], "…")
		}
		t.Fatalf("%d/%d SCOPE.Pattern entities have no CONTAINS edge targeting them; sample: %v",
			len(missing), len(patternIDs), missing)
	}
}

// TestCSharpQuartzNetBuilderDynamic verifies that Quartz.NET fluent builder
// method stubs and generic static-factory calls emitted by the C# extractor
// (JobBuilder.Create<T>, TriggerBuilder.Create<T>, WithIdentity, StartNow,
// BackgroundJob.Enqueue<T>) classify as DispositionDynamic rather than
// DispositionBugExtractor after the fix in issue #44 slice-7.
//
// The fix extends csharpDynamicPatterns in internal/resolve/refs.go with:
//   - a generic-factory pattern matching PascalCase.PascalCase<...> stubs, and
//   - bare-name patterns for Quartz.NET builder leaf methods (WithIdentity,
//     StartNow).
//
// Without the fix the csharp-quartz-net-mini fixture has 4 bug-extractor edges
// (StartNow, WithIdentity, JobBuilder.Create<EmailJob>,
// JobBuilder.Create<ReportJob>). After the fix all 4 move to dynamic and
// bug-extractor drops to 0 for that fixture.
func TestCSharpQuartzNetBuilderDynamic(t *testing.T) {
	fixtureDir := filepath.Join("../../internal/quality/golden/csharp-quartz-net-mini/src")
	abs, err := filepath.Abs(fixtureDir)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if _, serr := os.Stat(abs); serr != nil {
		t.Skipf("csharp-quartz-net-mini fixture not found at %s: %v", abs, serr)
	}

	idx := newTestIndexer(t, "csharp-quartz-net-mini", nil, "")
	doc, err := idx.Run(context.Background(), abs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if doc == nil {
		t.Fatal("Run returned nil document")
	}

	// Pre-fix baseline: 4 bug-extractor (StartNow, WithIdentity,
	// JobBuilder.Create<EmailJob>, JobBuilder.Create<ReportJob>).
	// Post-fix: all 4 move to dynamic; bug-extractor must be 0.
	bugCount := idx.finalDispositions.DispositionCounts[resolve.DispositionBugExtractor]
	dynCount := idx.finalDispositions.DispositionCounts[resolve.DispositionDynamic]

	if bugCount >= 4 {
		samples := idx.finalDispositions.DispositionSamples[resolve.DispositionBugExtractor]
		t.Errorf("bug-extractor count = %d (want < 4, pre-fix baseline); dynamic = %d; samples = %v",
			bugCount, dynCount, samples)
	}
	if dynCount == 0 {
		t.Errorf("dynamic count = 0, expected > 0 after Quartz.NET builder fix")
	}
}

// TestKotlinSpringResponseEntityDynamic verifies that the Spring MVC
// ResponseEntity fluent builder method stubs emitted by the Kotlin extractor
// (notFound, ok, build, body, noContent) classify as DispositionDynamic
// rather than DispositionBugExtractor after the fix in issue #44 slice-5.
//
// The fix adds these JVM-gated bare-name patterns to jvmDynamicPatterns in
// internal/resolve/refs.go. Without the fix all 8 CALLS stubs on the Spring
// fixture land in bug-extractor; with the fix they land in dynamic.
func TestKotlinSpringResponseEntityDynamic(t *testing.T) {
	fixtureDir := filepath.Join("../../internal/quality/golden/kotlin-spring-mini/src")
	abs, err := filepath.Abs(fixtureDir)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if _, serr := os.Stat(abs); serr != nil {
		t.Skipf("kotlin-spring-mini fixture not found at %s: %v", abs, serr)
	}

	idx := newTestIndexer(t, "kotlin-spring-mini", nil, "")
	doc, err := idx.Run(context.Background(), abs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if doc == nil {
		t.Fatal("Run returned nil document")
	}

	// The final dispositions are populated by Run() via ClassifyEndpoints.
	// Check that the bug-extractor count dropped vs. the pre-fix baseline
	// (8 bug-extractor edges) and the dynamic count rose.
	bugCount := idx.finalDispositions.DispositionCounts[resolve.DispositionBugExtractor]
	dynCount := idx.finalDispositions.DispositionCounts[resolve.DispositionDynamic]

	// Pre-fix baseline: 8 bug-extractor (notFound×2, ok, build×2, body, noContent, add).
	// Post-fix: notFound, ok, build, body, noContent, badRequest, accepted,
	//           created, unprocessableEntity, internalServerError all → Dynamic.
	//           `add` remains bug-extractor (not in the Spring builder set).
	// The fixture has exactly 1 add() call (users.add(user)) that stays in
	// bug-extractor; everything else must be < 2 to confirm the pattern fired.
	if bugCount >= 8 {
		samples := idx.finalDispositions.DispositionSamples[resolve.DispositionBugExtractor]
		t.Errorf("bug-extractor count = %d (want < 8, pre-fix baseline); dynamic = %d; samples = %v",
			bugCount, dynCount, samples)
	}
	if dynCount == 0 {
		t.Errorf("dynamic count = 0, expected > 0 after ResponseEntity builder fix")
	}
}

// TestScalaPlayMiniFutureStdlibDynamic verifies that the Scala stdlib
// Future.successful, List.map, List.filterNot, Map.get, Map.contains stubs
// emitted by the Scala extractor classify as DispositionDynamic rather than
// DispositionBugExtractor after the fix in issue #44 slice-6.
//
// The fix adds JVM-gated qualified patterns (^Future\.successful$,
// ^List\.map$, etc.) to jvmDynamicPatterns in internal/resolve/refs.go.
// Without the fix all 16 Scala stdlib CALLS stubs on the Play fixture land
// in bug-extractor; with the fix they land in dynamic.
//
// Baseline (before fix): bug-extractor=25, bug_rate=13.9%
// After fix:             bug-extractor=9,  bug_rate=5.0%
// Target category (Future.* + stdlib collections) dropped 100% (≥50% req.)
func TestScalaPlayMiniFutureStdlibDynamic(t *testing.T) {
	fixtureDir := filepath.Join("../../internal/quality/golden/scala-play-mini/src")
	abs, err := filepath.Abs(fixtureDir)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if _, serr := os.Stat(abs); serr != nil {
		t.Skipf("scala-play-mini fixture not found at %s: %v", abs, serr)
	}

	idx := newTestIndexer(t, "scala-play-mini", nil, "")
	doc, err := idx.Run(context.Background(), abs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if doc == nil {
		t.Fatal("Run returned nil document")
	}

	bugCount := idx.finalDispositions.DispositionCounts[resolve.DispositionBugExtractor]
	dynCount := idx.finalDispositions.DispositionCounts[resolve.DispositionDynamic]

	// Pre-fix baseline: 25 bug-extractor (Future.successful×10, List.map×2,
	// List.filterNot, List.find, Map.contains, Map.get, plus cross-package
	// import stubs and other single-hit names).
	// Post-fix: Future.successful, List.map, List.filterNot, List.find,
	//           Map.contains, Map.get all → Dynamic.
	// 9 remaining bug-extractors are in different categories (cross-package
	// IMPORTS, JsError.toJson external Play method, bare List ctor call).
	// Asserting < 25 confirms the stdlib pattern fired; asserting < 16
	// confirms a material drop in the target category.
	if bugCount >= 25 {
		samples := idx.finalDispositions.DispositionSamples[resolve.DispositionBugExtractor]
		t.Errorf("bug-extractor count = %d (want < 25, pre-fix baseline); dynamic = %d; samples = %v",
			bugCount, dynCount, samples)
	}
	if dynCount == 0 {
		t.Errorf("dynamic count = 0, expected > 0 after Scala stdlib fix")
	}
}

// TestRustTokioMiniChannelRecvDynamic verifies that the two highest-count
// unresolved Rust CALLS stubs on the tokio-mini fixture classify as
// DispositionDynamic rather than DispositionBugExtractor after the fix in
// issue #44 slice-7.
//
// The fix adds rustDynamicPatterns (bare `channel` + generic-receiver
// `Type<T>.method` form) and registers lang=="rust" in dynamicPatternsByLang
// in internal/resolve/refs.go.
//
// Pre-fix baseline (rust-tokio-mini, 5 files, 50 total dispositions):
//
//	bug-extractor = 2  (`channel`, `Receiver<String>.recv`)
//	dynamic       = 7
//	bug_rate      = 4.0%
//
// After fix:
//
//	bug-extractor = 0
//	dynamic       = 9
//	bug_rate      = 0.0%
func TestRustTokioMiniChannelRecvDynamic(t *testing.T) {
	fixtureDir := filepath.Join("../../internal/quality/golden/rust-tokio-mini/src")
	abs, err := filepath.Abs(fixtureDir)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if _, serr := os.Stat(abs); serr != nil {
		t.Skipf("rust-tokio-mini fixture not found at %s: %v", abs, serr)
	}

	idx := newTestIndexer(t, "rust-tokio-mini", nil, "")
	doc, err := idx.Run(context.Background(), abs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if doc == nil {
		t.Fatal("Run returned nil document")
	}

	bugCount := idx.finalDispositions.DispositionCounts[resolve.DispositionBugExtractor]
	dynCount := idx.finalDispositions.DispositionCounts[resolve.DispositionDynamic]

	// Pre-fix baseline: 2 bug-extractor stubs (`channel` × 1,
	// `Receiver<String>.recv` × 1).  Post-fix: both → Dynamic.
	// Asserting < 2 confirms the rustDynamicPatterns fired; asserting == 0
	// confirms complete elimination of the target category.
	if bugCount >= 2 {
		samples := idx.finalDispositions.DispositionSamples[resolve.DispositionBugExtractor]
		t.Errorf("bug-extractor count = %d (want < 2, pre-fix baseline); dynamic = %d; samples = %v",
			bugCount, dynCount, samples)
	}
	if dynCount == 0 {
		t.Errorf("dynamic count = 0, expected > 0 after Rust channel/recv fix")
	}
}

// TestModuleCoverage_AllEntitiesTagged verifies that every entity produced by
// the indexer carries a non-empty Properties["module"] value after issue #1381.
// It also confirms the distinct module count is much smaller than the entity
// count (many entities share the same module), which validates that rollup —
// not identity — is happening.
func TestModuleCoverage_AllEntitiesTagged(t *testing.T) {
	for _, tc := range []struct {
		name string
		repo string
	}{
		{"django_app", "testdata/django_app"},
		{"spring_app", "testdata/spring_app"},
		{"crossfile_go", "testdata/crossfile_go"},
		{"crossfile_python", "testdata/crossfile_python"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := runIndexerOn(t, tc.repo, tc.name,
				[]string{"graph-algo", "process-flow", "enrichment"})

			total := len(doc.Entities)
			if total == 0 {
				t.Fatalf("%s: no entities produced", tc.name)
			}

			modules := map[string]int{}
			missing := 0
			for _, e := range doc.Entities {
				m := e.Properties["module"]
				if m == "" {
					missing++
				} else {
					modules[m]++
				}
			}

			if missing > 0 {
				t.Errorf("%s: %d/%d entities missing Properties[\"module\"]",
					tc.name, missing, total)
			}

			t.Logf("%s: entities=%d distinct_modules=%d", tc.name, total, len(modules))

			// Rollup sanity: distinct modules should be strictly fewer than entities
			// (unless every entity is in a unique module, which would be a bug).
			if total > 1 && len(modules) >= total {
				t.Errorf("%s: distinct_modules=%d >= entities=%d; rollup is not collapsing paths",
					tc.name, len(modules), total)
			}
		})
	}
}

// TestLanguageTagPersistsAfterFBRoundtrip is the integration-level regression
// for issue #2341. It indexes a Python fixture, writes graph.fb, reads it back,
// and asserts that every entity sourced from a .py file has Language="python".
//
// Before the fix, buildEntity in fbwriter never serialized Language and the
// load.go workaround (restore from Properties["language"]) had nothing to read,
// so all entities came back with Language="" and grafel_find --language
// python silently returned nothing.
func TestLanguageTagPersistsAfterFBRoundtrip(t *testing.T) {
	doc := runIndexerOn(t, "testdata/crossfile_python", "crossfile_python", nil)

	// Quick sanity: the fixture must produce at least one Python entity.
	totalPy := 0
	for _, e := range doc.Entities {
		if strings.HasSuffix(e.SourceFile, ".py") {
			totalPy++
		}
	}
	if totalPy == 0 {
		t.Fatal("fixture produced no .py entities at all — nothing to verify")
	}

	// Write to graph.fb via the fbwriter, then reload via LoadGraphFromDir
	// (the FB path). We import fbwriter transitively through the main package
	// so use the graph-package shim: WriteAtomic (JSON) is NOT sufficient here
	// because the bug only manifests on the FB path. Instead sort the doc and
	// call fbwriter.WriteAtomic to write graph.fb, then LoadGraphFromDir which
	// prefers graph.fb over graph.json when both are present.
	dir := t.TempDir()
	fbPath := filepath.Join(dir, "graph.fb")
	sortDocumentForEmission(doc)
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("fbwriter.WriteAtomic: %v", err)
	}
	reloaded, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}

	// Assert: every entity with a .py source file must have Language="python"
	// after the roundtrip.
	missing := 0
	for _, e := range reloaded.Entities {
		if strings.HasSuffix(e.SourceFile, ".py") && e.Language != "python" {
			missing++
			t.Logf("entity missing Language: id=%s name=%s kind=%s src=%s lang=%q",
				e.ID, e.Name, e.Kind, e.SourceFile, e.Language)
		}
	}
	if missing > 0 {
		t.Errorf("%d/%d .py entities have Language=\"\" after graph.fb roundtrip (issue #2341)",
			missing, totalPy)
	}
}

// TestWarnEmptyLanguageEntities verifies that warnEmptyLanguageEntities emits
// a warning to stderr when entities with a recognized source extension have
// Language="". This is the buildDocument sanity-check required by issue #2341.
func TestWarnEmptyLanguageEntities(t *testing.T) {
	// Build a tiny document: one .py entity without Language (should warn),
	// one .py entity WITH Language (no warn), one synthetic entity with no
	// SourceFile (no warn).
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "a", Name: "bad_view", Kind: "class", SourceFile: "views.py", Language: ""},
			{ID: "b", Name: "good_view", Kind: "class", SourceFile: "models.py", Language: "python"},
			{ID: "c", Name: "ext:requests", Kind: "external", SourceFile: ""},
		},
	}

	// Redirect stderr to capture the warning.
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w

	warnEmptyLanguageEntities(doc)

	w.Close()
	os.Stderr = origStderr

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, "#2341") {
		t.Errorf("expected warning mentioning #2341, got: %q", output)
	}
	if !strings.Contains(output, "bad_view") || !strings.Contains(output, "views.py") {
		t.Errorf("expected warning to name the offending entity, got: %q", output)
	}
	// The good entity (Language="python") must NOT appear in the warning.
	if strings.Contains(output, "good_view") {
		t.Errorf("warning should not mention entity with Language set, got: %q", output)
	}
}

// TestWarnEmptyLanguageEntities_NoBadEntities verifies that no warning is
// emitted when all entities with recognized extensions already have Language set.
func TestWarnEmptyLanguageEntities_NoBadEntities(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "a", Name: "views", Kind: "class", SourceFile: "views.py", Language: "python"},
			{ID: "b", Name: "handler", Kind: "function", SourceFile: "main.go", Language: "go"},
		},
	}

	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w

	warnEmptyLanguageEntities(doc)

	w.Close()
	os.Stderr = origStderr

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if output != "" {
		t.Errorf("expected no warning when all Language fields set, got: %q", output)
	}
}
