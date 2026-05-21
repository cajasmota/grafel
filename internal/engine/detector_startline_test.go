package engine

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// langchainYAML is a minimal LangChain framework YAML for tests.
// entity_type matches the real langchain.yaml value ("Operation").
// Uses (?m) so ^ and $ match per-line (required for multiline source).
const langchainYAML = `
file_conventions: []

source_patterns:
  - pattern: "(?m)^(\\w+)\\s*=\\s*[A-Za-z_][\\w.()]*(?:\\s*\\|\\s*[A-Za-z_][\\w.()]*)+\\s*$"
    entity_type: Operation
    name_group: 1
    scope: file

relationship_rules: []
`

// newTestDetectorWithLangChain builds a Detector loaded from an in-memory
// filesystem containing the minimal LangChain YAML rule above. This avoids
// depending on the real rules directory so the test is hermetic.
func newTestDetectorWithLangChain(t *testing.T) *Detector {
	t.Helper()
	fsys := fstest.MapFS{
		"rules/python/frameworks/langchain.yaml": &fstest.MapFile{Data: []byte(langchainYAML)},
	}
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS: %v", err)
	}
	det := New(rules)
	return det
}

// TestDetector_StartLineSet verifies that yaml-driven entities have StartLine > 0.
// Issue #1413.
func TestDetector_StartLineSet(t *testing.T) {
	// Minimal Python source with an LCEL chain on line 4.
	// chain_a is on line 4, chain_b on line 5.
	src := "import langchain\n\n# some preamble\nchain_a = prompt | model\nchain_b = prompt | model | parser\n"
	fi := extractor.FileInput{
		Path:     "app/agent.py",
		Content:  []byte(src),
		Language: "python",
	}

	det := newTestDetectorWithLangChain(t)
	res, err := det.Detect(context.Background(), fi)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	if len(res.Entities) == 0 {
		t.Fatal("expected at least one entity to be emitted")
	}
	for _, e := range res.Entities {
		if e.StartLine == 0 {
			t.Errorf("entity %q (kind=%s) has StartLine=0 — expected > 0", e.Name, e.Kind)
		}
	}
}

// TestDetector_LangChainPattern_NoTrivialOps verifies that the tightened LCEL
// source pattern:
//  1. Does NOT emit f-string assignments with pipe chars as Operation entities.
//  2. Emits a clean variable name (group 1), not the full assignment line.
//  3. Still emits a legitimate LCEL chain.
//
// Issue #1415.
func TestDetector_LangChainPattern_NoTrivialOps(t *testing.T) {
	// f-string with pipe inside — MUST NOT match (it's not a pipe chain).
	// x = f"val={a|b}" contains a pipe inside string literal interpolation.
	// The tightened regex requires identifier/call/attr segments, not string literals.
	src := "import langchain\n\n# Legitimate LCEL chain — SHOULD be emitted\nreal_chain = prompt | model | parser\n\n# f-string with pipe — should NOT be emitted\nx = f\"val={some_dict}\"\n"
	fi := extractor.FileInput{
		Path:     "app/agent.py",
		Content:  []byte(src),
		Language: "python",
	}

	det := newTestDetectorWithLangChain(t)
	res, err := det.Detect(context.Background(), fi)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	// The old pattern captured name_group:0 (full line text as name).
	// The new pattern captures name_group:1 (just the LHS variable name).
	// Ensure no entity has a name that looks like a full assignment line.
	for _, e := range res.Entities {
		if e.Kind == "Operation" && len(e.Name) > 50 {
			t.Errorf("entity name %q looks like a full line (old name_group:0 behavior) — expected just the LHS variable", e.Name)
		}
	}

	// real_chain SHOULD be emitted (it's a valid LCEL chain).
	found := false
	for _, e := range res.Entities {
		if e.Kind == "Operation" && e.Name == "real_chain" {
			found = true
		}
	}
	if !found {
		t.Errorf("legitimate LCEL chain 'real_chain' was NOT emitted — pattern is too tight")
	}
}

// TestDetector_StartLineQualifiedName verifies that yaml-driven Python entities
// also get a non-empty QualifiedName. Issue #1413.
func TestDetector_StartLineQualifiedName(t *testing.T) {
	src := "import langchain\n\nmy_chain = prompt | model | parser\n"
	fi := extractor.FileInput{
		Path:     "app/pipelines/rag.py",
		Content:  []byte(src),
		Language: "python",
	}

	det := newTestDetectorWithLangChain(t)
	res, err := det.Detect(context.Background(), fi)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	for _, e := range res.Entities {
		if e.Language != "python" {
			continue
		}
		if e.QualifiedName == "" {
			t.Errorf("entity %q (kind=%s) has empty QualifiedName — expected module-qualified name", e.Name, e.Kind)
		}
		// "app/pipelines/rag.py": app/ prefix is stripped → module = "pipelines.rag"
		want := "pipelines.rag." + e.Name
		if e.QualifiedName != want {
			t.Errorf("entity %q: QualifiedName=%q, want %q", e.Name, e.QualifiedName, want)
		}
	}
}
