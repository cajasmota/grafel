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

// ---------------------------------------------------------------------------
// testify
// ---------------------------------------------------------------------------

func TestTestifySuiteFixture(t *testing.T) {
	f := fixtureInput(t, "testify_suite.go", "go")
	ents := extract(t, "custom_go_testify", f)

	// Suite struct that embeds suite.Suite.
	if !hasSubtypeName(ents, "test_suite", "testify_suite:UserServiceSuite") {
		t.Error("expected UserServiceSuite test_suite pattern")
	}
	// suite.Run(t, new(UserServiceSuite)) registration.
	if !hasSubtypeName(ents, "suite_run", "testify_run:UserServiceSuite") {
		t.Error("expected suite_run registration for UserServiceSuite")
	}
	// Receiver-method test cases bound to the suite.
	if !hasSubtypeName(ents, "test_case", "testify_case:UserServiceSuite.TestCreateUser") {
		t.Error("expected TestCreateUser suite test_case")
	}
	if !hasSubtypeName(ents, "test_case", "testify_case:UserServiceSuite.TestDeleteUser") {
		t.Error("expected TestDeleteUser suite test_case")
	}
	// Assertions (assert.* / require.*).
	if countSubtype(ents, "assertion") < 4 {
		t.Errorf("expected >=4 assertions, got %d", countSubtype(ents, "assertion"))
	}
}

func TestTestifySuiteCaseRequiresSuiteEmbed(t *testing.T) {
	// A receiver-method test on a struct that does NOT embed suite.Suite must
	// not be classified as a testify suite case.
	src := `
package x

import "github.com/stretchr/testify/assert"

type Plain struct{}

func (p *Plain) TestNotASuite() {}

func TestFoo(t *testing.T) { assert.Equal(t, 1, 1) }
`
	ents := extract(t, "custom_go_testify", fi("x_test.go", "go", src))
	if hasSubtypeName(ents, "test_case", "testify_case:Plain.TestNotASuite") {
		t.Error("non-suite receiver method must not become a testify test_case")
	}
	if countSubtype(ents, "assertion") != 1 {
		t.Errorf("expected exactly 1 assertion, got %d", countSubtype(ents, "assertion"))
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

func TestTestifyAssertProps(t *testing.T) {
	src := `package x
import "github.com/stretchr/testify/require"
func TestX(t *testing.T) { require.NoError(t, doThing()) }`
	e, ok := extreg.Get("custom_go_testify")
	if !ok {
		t.Fatal("custom_go_testify not registered")
	}
	ents, err := e.Extract(context.Background(), fi("x_test.go", "go", src))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, ent := range ents {
		if ent.Subtype == "assertion" {
			found = true
			if ent.Properties["assertion_pkg"] != "require" || ent.Properties["assertion"] != "NoError" {
				t.Errorf("unexpected assertion props: %v", ent.Properties)
			}
		}
	}
	if !found {
		t.Error("expected an assertion entity")
	}
}

// ---------------------------------------------------------------------------
// ginkgo
// ---------------------------------------------------------------------------

func TestGinkgoSpecsFixture(t *testing.T) {
	f := fixtureInput(t, "ginkgo_specs.go", "go")
	ents := extract(t, "custom_go_ginkgo", f)

	// Containers: Describe + Context + When = 3 test_suite nodes.
	if got := countSubtype(ents, "test_suite"); got != 3 {
		t.Errorf("expected 3 ginkgo containers, got %d", got)
	}
	// Specs: 2 It + 1 Specify + 1 FIt + 1 PIt = 5 test_case nodes.
	if got := countSubtype(ents, "test_case"); got != 5 {
		t.Errorf("expected 5 ginkgo specs, got %d", got)
	}
	// Hooks: BeforeEach + AfterEach = 2 test_hook nodes.
	if got := countSubtype(ents, "test_hook"); got != 2 {
		t.Errorf("expected 2 ginkgo hooks, got %d", got)
	}
}

func TestGinkgoFocusState(t *testing.T) {
	f := fixtureInput(t, "ginkgo_specs.go", "go")
	e, _ := extreg.Get("custom_go_ginkgo")
	ents, _ := e.Extract(context.Background(), f)
	states := map[string]bool{}
	for _, ent := range ents {
		if ent.Subtype == "test_case" {
			states[ent.Properties["focus_state"]] = true
		}
	}
	if !states["focused"] || !states["pending"] || !states["normal"] {
		t.Errorf("expected focused/pending/normal spec states, got %v", states)
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
// gomega
// ---------------------------------------------------------------------------

func TestGomegaMatchersFixture(t *testing.T) {
	f := fixtureInput(t, "gomega_matchers.go", "go")
	ents := extract(t, "custom_go_gomega", f)

	if got := countSubtype(ents, "assertion"); got < 8 {
		t.Errorf("expected >=8 gomega assertions, got %d", got)
	}
}

func TestGomegaMatcherProps(t *testing.T) {
	src := `package x
import . "github.com/onsi/gomega"
func check() { Expect(x).To(Equal(4)) }`
	e, _ := extreg.Get("custom_go_gomega")
	ents, err := e.Extract(context.Background(), fi("x_test.go", "go", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 {
		t.Fatalf("expected 1 assertion, got %d", len(ents))
	}
	p := ents[0].Properties
	if p["matcher"] != "Equal" || p["polarity"] != "To" || p["assertion_entry"] != "Expect" {
		t.Errorf("unexpected gomega props: %v", p)
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
