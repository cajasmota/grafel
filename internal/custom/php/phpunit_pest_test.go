package php_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/php"
)

// extractRaw4686 runs the named custom extractor and returns the full
// EntityRecord set (the entitySummary helper drops Properties, which this test
// needs to assert e2e_route_calls).
func extractRaw4686(t *testing.T, name string, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

// routeSuite4686 returns the single test_suite entity emitted by the route-hit
// extractor (or nil), and its de-duplicated sorted e2e_route_calls lines.
func routeSuite4686(ents []types.EntityRecord) (*types.EntityRecord, []string) {
	for i := range ents {
		e := &ents[i]
		if e.Subtype == "test_suite" && e.Properties["e2e_route_calls"] != "" {
			lines := strings.Split(e.Properties["e2e_route_calls"], "\n")
			sort.Strings(lines)
			return e, lines
		}
	}
	return nil, nil
}

func hasLine4686(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

// Fixture B: Laravel feature test `$this->getJson('/api/v1/x/get_counts')`
// produces a test_suite carrying `GET /api/v1/x/get_counts`, the shape the
// shared engine pass turns into a TESTS edge to the endpoint.
func TestIssue4686_LaravelGetJson_RouteHit(t *testing.T) {
	src := `<?php
class CountsTest extends TestCase {
    public function test_counts() {
        $response = $this->getJson('/api/v1/x/get_counts');
        $response->assertStatus(200);
    }
}
`
	ents := extractRaw4686(t, "custom_php_phpunit_pest",
		fi("tests/Feature/CountsTest.php", "php", src))
	suite, lines := routeSuite4686(ents)
	if suite == nil {
		t.Fatalf("expected a test_suite with e2e_route_calls; got %d entities", len(ents))
	}
	if !hasLine4686(lines, "GET /api/v1/x/get_counts") {
		t.Fatalf("expected GET /api/v1/x/get_counts; got %v", lines)
	}
}

// Laravel verb helpers, explicit json()/call(), and chained-receiver exclusion.
func TestIssue4686_LaravelVerbVariants_RouteHits(t *testing.T) {
	src := `<?php
class ApiTest extends TestCase {
    public function test_many() {
        $this->get('/api/v1/items');
        $this->postJson('/api/v1/items', ['name' => 'x']);
        $this->delete('/api/v1/items/1');
        $this->json('PUT', '/api/v1/items/1');
        $this->call('PATCH', '/api/v1/items/1');
        // chained getter on a response receiver must NOT be captured as a route
        $body = $response->getContent();
        // named-route helper (expression, not a literal path) must be skipped
        $this->get(route('items.index'));
    }
}
`
	ents := extractRaw4686(t, "custom_php_phpunit_pest",
		fi("tests/Feature/ApiTest.php", "php", src))
	suite, lines := routeSuite4686(ents)
	if suite == nil {
		t.Fatalf("expected a test_suite; got none")
	}
	for _, want := range []string{
		"GET /api/v1/items",
		"POST /api/v1/items",
		"DELETE /api/v1/items/1",
		"PUT /api/v1/items/1",
		"PATCH /api/v1/items/1",
	} {
		if !hasLine4686(lines, want) {
			t.Errorf("expected %q in %v", want, lines)
		}
	}
	for _, l := range lines {
		if strings.Contains(l, "getContent") || strings.Contains(l, "items.index") {
			t.Errorf("unexpected non-route line %q (chained getter / named-route helper leaked)", l)
		}
	}
}

// Symfony functional test: $client->request('GET', '/api/products').
func TestIssue4686_SymfonyClientRequest_RouteHit(t *testing.T) {
	src := `<?php
class ProductControllerTest extends WebTestCase {
    public function testList() {
        $client = static::createClient();
        $client->request('GET', '/api/products');
    }
}
`
	ents := extractRaw4686(t, "custom_php_phpunit_pest",
		fi("tests/ProductControllerTest.php", "php", src))
	_, lines := routeSuite4686(ents)
	if !hasLine4686(lines, "GET /api/products") {
		t.Fatalf("expected GET /api/products from $client->request; got %v", lines)
	}
}

// Pest global helper form: get('/api/v1/x') inside a tests/ file.
func TestIssue4686_PestGlobalHelper_RouteHit(t *testing.T) {
	src := `<?php
it('lists', function () {
    $response = $this->getJson('/api/v1/widgets');
    $response->assertOk();
});
`
	ents := extractRaw4686(t, "custom_php_phpunit_pest",
		fi("tests/Feature/WidgetTest.php", "php", src))
	suite, lines := routeSuite4686(ents)
	if suite == nil {
		t.Fatalf("expected a test_suite; got none")
	}
	if suite.Properties["framework"] != "pest" {
		t.Errorf("expected framework=pest for it()-shaped spec; got %q", suite.Properties["framework"])
	}
	if !hasLine4686(lines, "GET /api/v1/widgets") {
		t.Fatalf("expected GET /api/v1/widgets; got %v", lines)
	}
}

// Honest exclusion: a shape-only test that never calls a route emits NO suite.
func TestIssue4686_ShapeOnlyTest_NoSuite(t *testing.T) {
	src := `<?php
class UnitTest extends TestCase {
    public function test_math() {
        $this->assertEquals(4, 2 + 2);
    }
}
`
	ents := extractRaw4686(t, "custom_php_phpunit_pest",
		fi("tests/Unit/UnitTest.php", "php", src))
	if suite, _ := routeSuite4686(ents); suite != nil {
		t.Fatalf("shape-only test must not emit a route suite; got %q", suite.Name)
	}
}

// A non-test file (a controller) with a `->get(...)` call must never be treated
// as a feature test.
func TestIssue4686_NonTestFile_NoSuite(t *testing.T) {
	src := `<?php
class WidgetController {
    public function index() {
        return $this->repo->get('/internal');
    }
}
`
	ents := extractRaw4686(t, "custom_php_phpunit_pest",
		fi("app/Http/Controllers/WidgetController.php", "php", src))
	if len(ents) != 0 {
		t.Fatalf("non-test file must yield no entities; got %d", len(ents))
	}
}
