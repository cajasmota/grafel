package golang_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

// countSubtype returns how many emitted entities carry the given subtype.
func countSubtype(ents []entitySummary, subtype string) int {
	n := 0
	for _, e := range ents {
		if e.Subtype == subtype {
			n++
		}
	}
	return n
}

func hasSubtypeName(ents []entitySummary, subtype, name string) bool {
	for _, e := range ents {
		if e.Subtype == subtype && e.Name == name {
			return true
		}
	}
	return false
}

// propOf extracts a property value from the first emitted entity (these
// extractors emit exactly one entity per file post-#4358).
func propOf(t *testing.T, name string, file extreg.FileInput, key string) string {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("%s not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) == 0 {
		return ""
	}
	return ents[0].Properties[key]
}

// ---------------------------------------------------------------------------
// testify — #4358: one collapsed test_suite per file, no orphan ring.
// ---------------------------------------------------------------------------

func TestTestifySuiteFixture(t *testing.T) {
	f := fixtureInput(t, "testify_suite.go", "go")
	ents := extract(t, "custom_go_testify", f)

	// (a) ORPHAN COLLAPSE: exactly ONE test_suite entity, and ZERO of the old
	// per-suite-struct / per-case / per-assertion / per-suite-run nodes.
	if got := countSubtype(ents, "test_suite"); got != 1 {
		t.Errorf("expected exactly 1 collapsed test_suite, got %d", got)
	}
	for _, e := range ents {
		switch e.Subtype {
		case "test_case", "assertion", "suite_run":
			t.Errorf("orphan noise node still emitted: subtype=%s name=%q (#4358)", e.Subtype, e.Name)
		}
	}
	// (b) The collapsed suite is the file-scoped node.
	if !hasSubtypeName(ents, "test_suite", "testify_suite:testify_suite") {
		t.Errorf("expected file-scoped testify_suite entity, got %+v", ents)
	}
}

func TestTestifySuiteCountsFolded(t *testing.T) {
	f := fixtureInput(t, "testify_suite.go", "go")
	// Counts that were previously O(n) standalone nodes are now properties.
	if got := propOf(t, "custom_go_testify", f, "suite_count"); got != "1" {
		t.Errorf("suite_count=%q, want 1", got)
	}
	if got := propOf(t, "custom_go_testify", f, "test_case_count"); got != "2" {
		t.Errorf("test_case_count=%q, want 2 (TestCreateUser, TestDeleteUser)", got)
	}
	// Two top-level Test funcs: TestUserServiceSuite + TestStandalone.
	if got := propOf(t, "custom_go_testify", f, "test_func_count"); got != "2" {
		t.Errorf("test_func_count=%q, want 2", got)
	}
	// Assertions are folded, not orphaned. The fixture has >=4 assert/require.
	if got := propOf(t, "custom_go_testify", f, "assertion_count"); got == "" || got == "0" {
		t.Errorf("assertion_count=%q, want >=4", got)
	}
}

func TestTestifyNoOpOnNonTestify(t *testing.T) {
	src := `package x
func main() { println("hi") }`
	ents := extract(t, "custom_go_testify", fi("main.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities on non-testify file, got %d", len(ents))
	}
}

func TestTestifyEmitsSingleSuiteWithAssertions(t *testing.T) {
	src := `package x
import "github.com/stretchr/testify/require"
func TestX(t *testing.T) { require.NoError(t, doThing()) }`
	ents := extract(t, "custom_go_testify", fi("x_test.go", "go", src))
	if got := countSubtype(ents, "test_suite"); got != 1 {
		t.Fatalf("expected 1 test_suite, got %d", got)
	}
	if countSubtype(ents, "assertion") != 0 {
		t.Error("assertions must be folded, not emitted as standalone nodes (#4358)")
	}
}

// ---------------------------------------------------------------------------
// ginkgo — #4358: one collapsed test_suite per file.
// ---------------------------------------------------------------------------

func TestGinkgoSpecsFixture(t *testing.T) {
	f := fixtureInput(t, "ginkgo_specs.go", "go")
	ents := extract(t, "custom_go_ginkgo", f)

	if got := countSubtype(ents, "test_suite"); got != 1 {
		t.Errorf("expected 1 collapsed ginkgo test_suite, got %d", got)
	}
	for _, e := range ents {
		if e.Subtype == "test_case" || e.Subtype == "test_hook" {
			t.Errorf("orphan ginkgo noise node still emitted: %s %q (#4358)", e.Subtype, e.Name)
		}
	}
	// Counts folded as properties: 3 containers, 5 specs, 2 hooks.
	if got := propOf(t, "custom_go_ginkgo", f, "container_count"); got != "3" {
		t.Errorf("container_count=%q, want 3", got)
	}
	if got := propOf(t, "custom_go_ginkgo", f, "spec_count"); got != "5" {
		t.Errorf("spec_count=%q, want 5", got)
	}
	if got := propOf(t, "custom_go_ginkgo", f, "hook_count"); got != "2" {
		t.Errorf("hook_count=%q, want 2", got)
	}
}

func TestGinkgoNoOpOnNonGinkgo(t *testing.T) {
	src := `package x
func main() {}`
	ents := extract(t, "custom_go_ginkgo", fi("main.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// gomega — #4358: one collapsed test_suite per file (matchers folded).
// ---------------------------------------------------------------------------

func TestGomegaMatchersFixture(t *testing.T) {
	f := fixtureInput(t, "gomega_matchers.go", "go")
	ents := extract(t, "custom_go_gomega", f)

	if got := countSubtype(ents, "test_suite"); got != 1 {
		t.Errorf("expected 1 collapsed gomega test_suite, got %d", got)
	}
	if countSubtype(ents, "assertion") != 0 {
		t.Error("gomega matchers must be folded, not emitted as standalone nodes (#4358)")
	}
	if got := propOf(t, "custom_go_gomega", f, "assertion_count"); got == "" || got == "0" {
		t.Errorf("assertion_count=%q, want >=8", got)
	}
}

func TestGomegaNoOpOnNonGomega(t *testing.T) {
	src := `package x
func main() { println("ok") }`
	ents := extract(t, "custom_go_gomega", fi("main.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
