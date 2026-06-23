package php_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/php"
	"github.com/cajasmota/grafel/internal/types"
)

// extractPHP4686 parses + extracts a PHP source file through the registered
// tree-sitter extractor, the same surface the indexer uses.
func extractPHP4686(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("php")
	if !ok {
		t.Fatal("php extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "php",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return got
}

func findTestScope4686(ents []types.EntityRecord) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Subtype == "test_scope" {
			return &ents[i]
		}
	}
	return nil
}

func scopeCalls4686(scope *types.EntityRecord) []string {
	var got []string
	for _, r := range scope.Relationships {
		if r.Kind == "CALLS" {
			got = append(got, r.ToID)
		}
	}
	return got
}

func contains4686(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// Fixture A (also covers gap 1 local-var receiver typing inside a Pest closure):
// `$c = new XController($svc); $c->getCounts()` inside an it() closure → the
// test_scope owner emits a CALLS edge to the dotted XController.getCounts.
func TestIssue4686_PestClosure_LocalVarReceiver_CALLS(t *testing.T) {
	src := `<?php
it('counts', function () {
    $svc = new CountsService();
    $c = new XController($svc);
    $c->getCounts();
    expect(true)->toBeTrue();
});
`
	ents := extractPHP4686(t, "tests/Feature/CountsTest.php", src)
	scope := findTestScope4686(ents)
	if scope == nil {
		t.Fatalf("expected a test_scope owner entity; got none in %d entities", len(ents))
	}
	got := scopeCalls4686(scope)
	if !contains4686(got, "XController.getCounts") {
		t.Fatalf("expected CALLS XController.getCounts from the it() closure; got %v", got)
	}
	for _, id := range got {
		if id == "expect" || id == "toBeTrue" {
			t.Fatalf("unexpected DSL-noise CALLS edge %q", id)
		}
	}
}

// Fixture C: a second nested it() inside a describe() block is still mined.
func TestIssue4686_PestDescribeNested_CALLS(t *testing.T) {
	src := `<?php
describe('reports', function () {
    it('builds', function () {
        $c = new ReportController();
        $c->build();
    });
});
`
	ents := extractPHP4686(t, "tests/Unit/ReportTest.php", src)
	scope := findTestScope4686(ents)
	if scope == nil {
		t.Fatalf("expected a test_scope owner; got none")
	}
	if !contains4686(scopeCalls4686(scope), "ReportController.build") {
		t.Fatalf("expected CALLS ReportController.build; got %v", scopeCalls4686(scope))
	}
}

// PHPUnit named test methods are already mined by walk() — the scope owner must
// NOT double-emit for them, but the method itself carries the receiver-typed
// CALLS edge.
func TestIssue4686_PHPUnitNamedMethod_NoScopeOwner_ButMethodHasCALLS(t *testing.T) {
	src := `<?php
class CountsTest extends TestCase {
    public function test_counts() {
        $c = new XController();
        $c->getCounts();
    }
}
`
	ents := extractPHP4686(t, "tests/Feature/CountsTest.php", src)
	if scope := findTestScope4686(ents); scope != nil {
		t.Fatalf("PHPUnit named methods must not get a test_scope owner; got %q", scope.Name)
	}
	var found bool
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" && r.ToID == "XController.getCounts" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected test_counts to carry CALLS XController.getCounts")
	}
}

// Negative: a container-resolved receiver (`app()->make(...)`) stays bare — no
// fabricated dotted target, so the scope owner emits no edge for it.
func TestIssue4686_FactoryReceiver_StaysBare_NoScopeEdge(t *testing.T) {
	src := `<?php
it('factory stays bare', function () {
    $c = app()->make(XController::class);
    $c->getCounts();
});
`
	ents := extractPHP4686(t, "tests/Feature/FactoryTest.php", src)
	if scope := findTestScope4686(ents); scope != nil {
		for _, r := range scope.Relationships {
			if r.Kind == "CALLS" && strings.HasSuffix(r.ToID, ".getCounts") {
				t.Fatalf("factory receiver must stay bare; got dotted CALLS %q", r.ToID)
			}
		}
	}
}

// Non-test files never get a scope owner even if they contain it()-shaped calls.
func TestIssue4686_NonTestFile_NoScopeOwner(t *testing.T) {
	src := `<?php
function it($a, $b) {}
it('x', function () { $c = new XController(); $c->getCounts(); });
`
	ents := extractPHP4686(t, "app/Helpers/util.php", src)
	if scope := findTestScope4686(ents); scope != nil {
		t.Fatalf("non-test file must not get a test_scope owner; got %q", scope.Name)
	}
}
