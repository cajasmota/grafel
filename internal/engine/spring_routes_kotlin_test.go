// Tests for Spring MVC route composition in Kotlin files.
//
// Refs #1421.
package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tskotlin "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/kotlin"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// sampleKotlinSpringController mirrors the Java fixture in spring_routes_test.go.
const sampleKotlinSpringController = `package io.shipfast.notifications

import org.springframework.web.bind.annotation.*

@RestController
@RequestMapping("/api")
class OrderController {

    @GetMapping("/orders")
    fun listOrders(): List<Order> = emptyList()

    @PostMapping("/orders")
    fun createOrder(@RequestBody o: Order): Order = o

    @PutMapping("/orders/{id}")
    fun updateOrder(@PathVariable id: Long, @RequestBody o: Order): Order = o

    @DeleteMapping("/orders/{id}")
    fun deleteOrder(@PathVariable id: Long) {}

    @PatchMapping("/orders/{id}")
    fun patchOrder(@PathVariable id: Long): Order? = null

    @RequestMapping(value = "/legacy", method = [RequestMethod.GET])
    fun legacy(): String = "ok"
}
`

// sampleKotlinControllerNoClassPrefix exercises the case where the class has
// NO class-level @RequestMapping. Each method carries its own full path.
// The pass must NOT emit endpoints for this class (no class mapping → pass skips).
const sampleKotlinControllerNoClassPrefix = `package io.shipfast.example

import org.springframework.web.bind.annotation.*

@RestController
class NoClassPrefixController {

    @GetMapping("/health")
    fun health(): String = "ok"
}
`

// sampleKotlinControllerWithOutboundCalls exercises RestTemplate / WebClient
// outbound HTTP client calls emitted as http_endpoint_call entities.
const sampleKotlinControllerWithOutboundCalls = `package io.shipfast.svc

import org.springframework.web.bind.annotation.*
import org.springframework.web.client.RestTemplate
import org.springframework.web.reactive.function.client.WebClient

@RestController
@RequestMapping("/notifications")
class NotificationsController(
    private val restTemplate: RestTemplate,
    private val webClient: WebClient,
) {
    @PostMapping("/email")
    fun sendEmail(@RequestBody req: Map<String, Any>): Map<String, Boolean> {
        restTemplate.getForObject("/api/users", String::class.java)
        return mapOf("sent" to true)
    }
}
`

// TestKotlinSpring_ComposedEndpoints verifies that the Kotlin Spring pass
// emits http_endpoint_definition entities with composed paths, correct verbs,
// and ROUTES_TO edges.
func TestKotlinSpring_ComposedEndpoints(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "services/notifications/src/main/kotlin/io/shipfast/OrderController.kt",
		Content:  []byte(sampleKotlinSpringController),
		Language: "kotlin",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	// Collect all http_endpoint_definition IDs.
	defIDs := map[string]bool{}
	for _, e := range result.Entities {
		if e.Kind == httpEndpointDefinitionKind {
			defIDs[e.ID] = true
		}
	}

	wantIDs := []string{
		"http:GET:/api/orders",
		"http:POST:/api/orders",
		"http:PUT:/api/orders/{id}",
		"http:DELETE:/api/orders/{id}",
		"http:PATCH:/api/orders/{id}",
		"http:GET:/api/legacy",
	}
	for _, id := range wantIDs {
		if !defIDs[id] {
			t.Errorf("missing http_endpoint_definition %q; got: %v", id, keyList(defIDs))
		}
	}

	// Verify ROUTES_TO edges exist.
	type rel struct{ from, to string }
	wantRels := map[rel]bool{
		{"http:GET:/api/orders", "SCOPE.Operation:OrderController.listOrders"}:          false,
		{"http:POST:/api/orders", "SCOPE.Operation:OrderController.createOrder"}:        false,
		{"http:PUT:/api/orders/{id}", "SCOPE.Operation:OrderController.updateOrder"}:    false,
		{"http:DELETE:/api/orders/{id}", "SCOPE.Operation:OrderController.deleteOrder"}: false,
		{"http:PATCH:/api/orders/{id}", "SCOPE.Operation:OrderController.patchOrder"}:   false,
		{"http:GET:/api/legacy", "SCOPE.Operation:OrderController.legacy"}:              false,
	}
	for _, r := range result.Relationships {
		if r.Kind != "ROUTES_TO" {
			continue
		}
		key := rel{r.FromID, r.ToID}
		if _, ok := wantRels[key]; ok {
			wantRels[key] = true
		}
	}
	for k, seen := range wantRels {
		if !seen {
			t.Errorf("expected ROUTES_TO %s -> %s not found", k.from, k.to)
		}
	}

	// Verify properties on emitted entities.
	for _, e := range result.Entities {
		if e.Kind != httpEndpointDefinitionKind {
			continue
		}
		if e.Language != "kotlin" {
			t.Errorf("entity %q: Language=%q, want kotlin", e.ID, e.Language)
		}
		if e.Properties["pattern_type"] != "ast_driven" {
			t.Errorf("entity %q: pattern_type=%q, want ast_driven", e.ID, e.Properties["pattern_type"])
		}
		if e.Properties["framework"] != "spring_mvc" {
			t.Errorf("entity %q: framework=%q, want spring_mvc", e.ID, e.Properties["framework"])
		}
	}
}

// TestKotlinSpring_NoClassPrefix verifies that controllers without a class-level
// @RequestMapping are not processed by the AST pass (the pass requires a class
// prefix to compose). The ShipFast notifications controller pattern always has
// a class-level mapping, but a plain @RestController without one must not panic.
func TestKotlinSpring_NoClassPrefix(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "src/main/kotlin/NoClassPrefixController.kt",
		Content:  []byte(sampleKotlinControllerNoClassPrefix),
		Language: "kotlin",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	// The AST pass requires a class-level @RequestMapping, so no endpoints
	// from our pass. (Other synthesizers may not emit either, but we only
	// assert that the AST pass does not panic or emit garbage.)
	for _, e := range result.Entities {
		if e.Kind == httpEndpointDefinitionKind && e.Properties["pattern_type"] == "ast_driven" {
			t.Errorf("unexpected ast_driven endpoint %q for no-class-prefix controller", e.ID)
		}
	}
}

// TestKotlinSpring_ShipFastNotifications directly exercises the ShipFast
// notifications EmailController and DispatchController fixtures.
func TestKotlinSpring_ShipFastNotifications(t *testing.T) {
	emailController := `package io.shipfast.notifications

import org.springframework.web.bind.annotation.*

@RestController
@RequestMapping("/notifications")
class EmailController {

    @PostMapping("/email")
    fun sendEmail(@RequestBody req: Map<String, Any>): Map<String, Boolean> {
        return mapOf("sent" to true)
    }
}
`
	dispatchController := `package io.shipfast.notifications

import org.springframework.web.bind.annotation.*

@RestController
@RequestMapping("/notifications")
class DispatchController {

    @PostMapping("/dispatch")
    fun dispatch(@RequestBody req: Map<String, Any>): Map<String, Boolean> {
        return mapOf("sent" to true)
    }
}
`
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)

	for _, tc := range []struct {
		name    string
		src     string
		wantIDs []string
	}{
		{
			name:    "EmailController",
			src:     emailController,
			wantIDs: []string{"http:POST:/notifications/email"},
		},
		{
			name:    "DispatchController",
			src:     dispatchController,
			wantIDs: []string{"http:POST:/notifications/dispatch"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := det.Detect(context.Background(), extractor.FileInput{
				Path:     "services/notifications/src/main/kotlin/io/shipfast/notifications/" + tc.name + ".kt",
				Content:  []byte(tc.src),
				Language: "kotlin",
			})
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			defIDs := map[string]bool{}
			for _, e := range result.Entities {
				if e.Kind == httpEndpointDefinitionKind {
					defIDs[e.ID] = true
				}
			}
			for _, id := range tc.wantIDs {
				if !defIDs[id] {
					t.Errorf("missing %q (got: %v)", id, keyList(defIDs))
				}
			}
		})
	}
}

// keyList returns the keys of a bool map as a slice for error messages.
func keyList(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// --- #6736 -------------------------------------------------------------------
//
// A Kotlin @RestController that is NOT the last top-level declaration in its
// file emitted zero routes. Every Spring fixture in the corpus — Kotlin and
// Java, 63 of them — happens to put its controller last, so the whole suite was
// blind to the file-position axis.
//
// The measured mechanism is NOT the #6360 ERROR-skipping wrapper: the raw parse
// of the fixture below carries ZERO ERROR nodes, so that wrapper is never even
// applied. It is a tree-sitter-kotlin (fwcd 0.3.8) ambiguity — TWO OR MORE
// consecutive annotations on a top-level declaration are parsed as a chain of
// `prefix_expression` operators, swallowing the declaration into an
// `infix_expression` plus a trailing `lambda_literal`, whenever the declaration
// is not the last construct in the file. The class therefore never appears as a
// `class_declaration` at all: `walkKotlinClasses` finds nothing to process, and
// the Kotlin extractor loses the class entity itself (`kept` degrades from
// `RealController.kept` to a bare top-level `kept`).

const kotlinControllerNotLast6736 = `package io.demo

import org.springframework.web.bind.annotation.*

@RestController
@RequestMapping("/api")
class RealController {

    @GetMapping("/kept")
    fun kept(): String = "ok"
}

fun looseHelper(): String = "helper"

val looseConst: String = "const"

class SecondClass {
    fun unrelated() {}
}
`

const kotlinControllerNotLastPath6736 = "src/main/kotlin/io/demo/RealController.kt"

// TestKotlinSpring6736_ControllerFollowedByDeclarations is the regression pin:
// the controller keeps its route even though a top-level fun, a top-level val
// and a second class follow it in the same file.
func TestKotlinSpring6736_ControllerFollowedByDeclarations(t *testing.T) {
	result := detectKotlin6499(t, kotlinControllerNotLastPath6736, kotlinControllerNotLast6736)

	targets := astDrivenRouteTargets(result)
	got, ok := targets["http:GET:/api/kept"]
	if !ok {
		t.Fatalf("no ast_driven ROUTES_TO from http:GET:/api/kept — the controller was erased "+
			"by the declarations that follow it; got %v", targets)
	}
	const want = "SCOPE.Operation:RealController.kept"
	if got != want {
		t.Errorf("ROUTES_TO target = %q, want %q", got, want)
	}
}

// TestKotlinSpring6736_ControllerNotLastNonVacuity is the non-vacuity control
// for the test above. Without it, "no route" cannot be told apart from "the
// fixture never parsed" — the exact confusion that produced a wrong mechanism
// for this bug twice.
//
// It establishes three things:
//
//  1. On the RAW, un-repaired, un-wrapped parse the fixture has ZERO ERROR
//     nodes. That rules out the #6360 error-skipping wrapper by construction:
//     it is applied only when errNodes > 0.
//  2. On that same raw parse the annotated controller is NOT reachable as a
//     top-level `class_declaration` — it is a `prefix_expression`. This pins the
//     grammar misparse itself, so the repair has a stated premise.
//  3. The identical controller text, with the trailing declarations removed, DOES
//     yield the route. Only its POSITION differs, so a missing route above is a
//     position defect and not a broken fixture.
func TestKotlinSpring6736_ControllerNotLastNonVacuity(t *testing.T) {
	p, err := tsofficial.New().NewParser(tskotlin.Language())
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	defer p.Close()
	tree, err := p.Parse([]byte(kotlinControllerNotLast6736))
	if err != nil {
		t.Fatalf("raw parse failed: %v", err)
	}
	defer tree.Close()
	root := tree.RootNode()

	var countErrs func(n ts.Node) int
	countErrs = func(n ts.Node) int {
		if n == nil {
			return 0
		}
		c := 0
		if n.IsError() {
			c++
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			c += countErrs(n.Child(i))
		}
		return c
	}
	if errs := countErrs(root); errs != 0 {
		t.Fatalf("control failed: the RAW parse has %d ERROR node(s) — this fixture is a "+
			"malformed-source case, not the clean-parse misparse #6736 describes", errs)
	}

	var topTypes []string
	sawClassDecl := false
	sawAnnotatedPrefixExpr := false
	for i := 0; i < int(root.ChildCount()); i++ {
		c := root.Child(i)
		topTypes = append(topTypes, c.Type())
		start, end := int(c.StartByte()), int(c.EndByte())
		covers := strings.Contains(kotlinControllerNotLast6736[start:end], "class RealController")
		if !covers {
			continue
		}
		switch c.Type() {
		case "class_declaration":
			sawClassDecl = true
		case "prefix_expression":
			sawAnnotatedPrefixExpr = true
		}
	}
	if sawClassDecl {
		t.Fatalf("control failed: the RAW parse DOES expose `class RealController` as a top-level "+
			"class_declaration (top children: %v) — the grammar-misparse premise of #6736 no "+
			"longer holds, so this fixture no longer exercises the defect", topTypes)
	}
	if !sawAnnotatedPrefixExpr {
		t.Fatalf("control failed: `class RealController` is neither a class_declaration nor the "+
			"expected misparsed prefix_expression in the RAW tree (top children: %v)", topTypes)
	}

	// The controller half, alone, DOES produce the route. Only position differs.
	idx := strings.Index(kotlinControllerNotLast6736, "\nfun looseHelper")
	if idx < 0 {
		t.Fatalf("control failed: fixture no longer has the trailing declarations")
	}
	onlyTargets := astDrivenRouteTargets(
		detectKotlin6499(t, kotlinControllerNotLastPath6736, kotlinControllerNotLast6736[:idx]))
	if _, ok := onlyTargets["http:GET:/api/kept"]; !ok {
		t.Fatalf("control failed: the controller emits no route even when it IS the last "+
			"declaration — the fixture itself is broken, so a missing route proves nothing; got %v",
			onlyTargets)
	}
}
