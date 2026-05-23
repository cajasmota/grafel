// Tests for Spring MVC route composition in Kotlin files.
//
// Refs #1421.
package engine

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
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
		{"http:GET:/api/orders", "Controller:listOrders"}:          false,
		{"http:POST:/api/orders", "Controller:createOrder"}:        false,
		{"http:PUT:/api/orders/{id}", "Controller:updateOrder"}:    false,
		{"http:DELETE:/api/orders/{id}", "Controller:deleteOrder"}: false,
		{"http:PATCH:/api/orders/{id}", "Controller:patchOrder"}:   false,
		{"http:GET:/api/legacy", "Controller:legacy"}:              false,
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
