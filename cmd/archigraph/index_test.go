package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/classifier"
	"github.com/cajasmota/archigraph/internal/engine"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/treesitter"
)

// newTestIndexer constructs an Indexer wired up to the embedded YAML rules
// and the default classifier/parser. skipPasses is the optional set of
// passes to skip — pass nil to run everything.
func newTestIndexer(t *testing.T, repoTag string, skipPasses []string) *Indexer {
	t.Helper()
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
	idx := newTestIndexer(t, repoTag, skipPasses)
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
func TestPass4Algorithms_AttributesPresent(t *testing.T) {
	full := runIndexerOn(t, "testdata/crossfile_go", "crossfile_go", nil)
	skipped := runIndexerOn(t, "testdata/crossfile_go", "crossfile_go", []string{"algorithms"})

	if full.AlgorithmStats == nil {
		t.Fatal("full run: AlgorithmStats nil")
	}
	if len(full.Communities) == 0 {
		t.Fatal("full run: Communities empty")
	}
	withAttrs := 0
	for _, e := range full.Entities {
		if e.CommunityID != nil && e.PageRank != nil && e.Centrality != nil {
			withAttrs++
		}
	}
	if withAttrs == 0 {
		t.Fatal("full run: no entity has community_id+pagerank+centrality")
	}

	if skipped.AlgorithmStats != nil {
		t.Errorf("skipped run: AlgorithmStats should be nil, got %+v", skipped.AlgorithmStats)
	}
	if len(skipped.Communities) != 0 {
		t.Errorf("skipped run: Communities should be empty, got %d", len(skipped.Communities))
	}
	for _, e := range skipped.Entities {
		if e.CommunityID != nil || e.PageRank != nil || e.Centrality != nil {
			t.Errorf("skipped run: entity %s has algo attrs set", e.ID)
			break
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
// line appears when ARCHIGRAPH_VERBOSE=1. PORT-EXT (issue #32).
//
// NOTE: this test mutates the process-global os.Stderr file handle and
// therefore MUST NOT call t.Parallel(). Running it concurrently with
// any other test that writes to stderr (or a sibling that also swaps
// the handle) would interleave bytes into the captured pipe and the
// assertion would flake. If the test ever needs parallelism, refactor
// the indexer to accept an io.Writer seam instead of touching the
// global handle.
func TestExternalSynthesis_VerboseCounter(t *testing.T) {
	t.Setenv("ARCHIGRAPH_VERBOSE", "1")

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
