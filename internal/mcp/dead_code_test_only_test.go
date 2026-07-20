package mcp

// dead_code_test_only_test.go — value-asserting validation corpus for the
// test_only_referenced dead-code class (#3657, epic #3648).
//
// The fixture is a synthetic graph modelled on the real #3650-3656 orphans:
// production symbols whose ONLY callers live in *_test.go files. The class must
// FLAG those and SPARE the wired siblings (production-called, cmd-wired,
// framework-registered, cross-repo-imported). Assertions are value-based — they
// name the exact symbols that must / must not appear — not len>0.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// buildTestOnlyDoc models the validation corpus from the #3657 spec.
//
// Production entities (production source files):
//   - enrichOrphan        : called ONLY from enricher_test.go        → FLAG
//   - ReferenceExtractor.Extract : called ONLY via a TESTS edge      → FLAG
//   - TryClone            : called ONLY from clone_test.go           → FLAG
//   - applyDjangoSignalPubSub : wired from cmd/index.go (production) → SPARE
//   - bothCaller          : called from prod AND test               → SPARE
//   - registeredHandler   : framework handler, only test caller      → SPARE (exclusion)
//   - exportedAPI         : imported cross-repo, only test caller    → SPARE (exclusion)
//
// Test entities (live in *_test.go — callers, never flagged themselves):
//   - enricherTest, refTest, cloneTest, handlerTest, apiTest, bothTest
func buildTestOnlyDoc() *graph.Document {
	ents := []graph.Entity{
		// ---- production operations ----
		{ID: "enrichOrphan", Name: "enrichOrphan", Kind: "Function", SourceFile: "internal/enrichers/orphan.go"},
		{ID: "refExtract", Name: "ReferenceExtractor.Extract", Kind: "Method", SourceFile: "internal/extractors/references/extractor.go"},
		{ID: "tryClone", Name: "TryClone", Kind: "Function", SourceFile: "daemon/clone/clone.go"},
		{ID: "applyDjango", Name: "applyDjangoSignalPubSub", Kind: "Function", SourceFile: "internal/passes/django.go"},
		{ID: "bothCaller", Name: "computeStuff", Kind: "Function", SourceFile: "internal/svc/compute.go"},
		{ID: "registeredHandler", Name: "handleEvent", Kind: "Function", SourceFile: "internal/svc/handler.go"},
		{ID: "exportedAPI", Name: "ReadSecretValue", Kind: "Function", SourceFile: "internal/api/secrets.go"},
		// production callers
		{ID: "indexMain", Name: "indexMain", Kind: "Function", SourceFile: "cmd/index.go"},
		{ID: "prodCaller", Name: "prodCaller", Kind: "Function", SourceFile: "internal/svc/runner.go"},

		// ---- test entities (live in *_test.go) ----
		{ID: "enricherTest", Name: "TestEnrich", Kind: "Function", SourceFile: "internal/enrichers/orphan_test.go"},
		{ID: "refTest", Name: "TestRef", Kind: "Function", SourceFile: "internal/extractors/references/extractor_test.go"},
		{ID: "cloneTest", Name: "TestClone", Kind: "Function", SourceFile: "daemon/clone/clone_test.go"},
		{ID: "handlerTest", Name: "TestHandler", Kind: "Function", SourceFile: "internal/svc/handler_test.go"},
		{ID: "apiTest", Name: "TestAPI", Kind: "Function", SourceFile: "internal/api/secrets_test.go"},
		{ID: "bothTest", Name: "TestBoth", Kind: "Function", SourceFile: "internal/svc/compute_test.go"},

		// cross-repo consumer marker: an IMPORTS edge carrying imported_name
		// keeps exportedAPI alive as public surface.
		{ID: "extConsumer", Name: "extConsumer", Kind: "File", SourceFile: "other/consumer.go"},
	}

	rels := []graph.Relationship{
		// enrichOrphan: ONLY a test caller → test_only_referenced
		{FromID: "enricherTest", ToID: "enrichOrphan", Kind: "CALLS"},
		// ReferenceExtractor.Extract: ONLY a TESTS edge → test_only_referenced
		{FromID: "refTest", ToID: "refExtract", Kind: "TESTS"},
		// TryClone: ONLY a test caller → test_only_referenced
		{FromID: "cloneTest", ToID: "tryClone", Kind: "CALLS"},

		// applyDjangoSignalPubSub: wired from cmd/index.go (production) → SPARE
		{FromID: "indexMain", ToID: "applyDjango", Kind: "CALLS"},

		// computeStuff: a production caller AND a test caller → SPARE
		{FromID: "prodCaller", ToID: "bothCaller", Kind: "CALLS"},
		{FromID: "bothTest", ToID: "bothCaller", Kind: "CALLS"},

		// handleEvent: ONLY a test caller, but it is a framework handler
		// (name "handle*") → honest exclusion → SPARE
		{FromID: "handlerTest", ToID: "registeredHandler", Kind: "CALLS"},

		// ReadSecretValue: ONLY a test caller, but imported cross-repo → SPARE
		{FromID: "apiTest", ToID: "exportedAPI", Kind: "CALLS"},
		graph.Relationship{FromID: "extConsumer", ToID: "exportedAPI", Kind: "IMPORTS"}.WithProperties(map[string]string{"imported_name": "ReadSecretValue"}),
	}
	return minDoc(ents, rels)
}

func TestFindDeadCode_TestOnlyReferenced(t *testing.T) {
	srv := newTestServer(t, buildTestOnlyDoc())

	out := callFlowTool(t, srv.handleFindDeadCode, map[string]any{
		"group": "test",
		"limit": float64(500),
	})

	dead, ok := out["dead_code"].([]any)
	if !ok {
		t.Fatalf("dead_code not an array: %T", out["dead_code"])
	}

	// Index flagged symbols by name → reason.
	flaggedReason := map[string]string{}
	for _, it := range dead {
		m := it.(map[string]any)
		flaggedReason[m["name"].(string)], _ = m["reason"].(string)
	}

	const testOnlyTag = "test_only_referenced"

	// MUST be flagged as test_only_referenced (the known orphans).
	mustFlag := []string{"enrichOrphan", "ReferenceExtractor.Extract", "TryClone"}
	for _, name := range mustFlag {
		reason, present := flaggedReason[name]
		if !present {
			t.Errorf("expected %q to be flagged test_only_referenced, but it was not flagged at all", name)
			continue
		}
		if !contains(reason, testOnlyTag) {
			t.Errorf("expected %q flagged with reason %q, got reason %q", name, testOnlyTag, reason)
		}
	}

	// MUST NOT be flagged (wired siblings + honest exclusions).
	mustNotFlag := map[string]string{
		"applyDjangoSignalPubSub": "wired from cmd/index.go (production caller)",
		"computeStuff":            "has a production caller in addition to a test caller",
		"handleEvent":             "framework handler (honest exclusion)",
		"ReadSecretValue":         "imported cross-repo (live public API)",
	}
	for name, why := range mustNotFlag {
		if _, present := flaggedReason[name]; present {
			t.Errorf("FALSE POSITIVE: %q (%s) must not be flagged, but it was: reason=%q",
				name, why, flaggedReason[name])
		}
	}

	// Test entities themselves are never flagged.
	for _, name := range []string{"TestEnrich", "TestRef", "TestClone", "TestHandler"} {
		if _, present := flaggedReason[name]; present {
			t.Errorf("FALSE POSITIVE: test entity %q must never be flagged", name)
		}
	}
}

// TestIsTestFileMCP guards the per-language test-file predicate.
func TestIsTestFileMCP(t *testing.T) {
	testFiles := []string{
		"internal/enrichers/orphan_test.go",
		"pkg/foo_test.go",
		"app/test_views.py",
		"app/views_test.py",
		"conftest.py",
		"src/components/Button.test.tsx",
		"src/components/Button.spec.ts",
		"spec/models/user_spec.rb",
		"src/test/java/com/x/FooTest.java",
		"src/test/kotlin/FooSpec.kt",
		"Foo.Tests.cs",
		"some/__tests__/helper.js",
		"a/tests/b.go",
	}
	for _, f := range testFiles {
		if !isTestFileMCP(f) {
			t.Errorf("expected %q to be a test file", f)
		}
	}
	prodFiles := []string{
		"internal/enrichers/orphan.go",
		"cmd/index.go",
		"app/views.py",
		"src/components/Button.tsx",
		"daemon/clone/clone.go",
		"",                            // empty path is not a test file
		"internal/testutil/helper.go", // 'test' substring but not a test-file convention
	}
	for _, f := range prodFiles {
		if isTestFileMCP(f) {
			t.Errorf("expected %q to NOT be a test file", f)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
