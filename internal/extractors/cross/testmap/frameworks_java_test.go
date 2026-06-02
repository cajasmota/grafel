package testmap

import "testing"

// Java JUnit deep TESTING linkage (#3855). The shared cross/testmap extractor
// already credits Kotlin JVM frameworks (#3437) via this same detector path;
// these value-asserting tests confirm the capability genuinely fires for Java
// before the registry tests_linkage cells are flipped to cite cross/testmap.

// TestJUnit_DirectCall_HighConfidence — a @Test that instantiates and calls the
// SUT produces a high-confidence TESTS edge test→SUT (the ticket's canonical
// example). Asserts the exact SUT id "UserService".
func TestJUnit_DirectCall_HighConfidence(t *testing.T) {
	src := `package com.example.service;

import org.junit.jupiter.api.Test;

class UserServiceTest {
    @Test
    void create() {
        UserService userService = new UserService();
        userService.create("alice");
    }
}`
	recs := runExtract(t, "src/test/java/com/example/service/UserServiceTest.java", "java", src)
	if recs[0].Properties["test_framework"] != "junit" {
		t.Fatalf("framework=%q, want junit", recs[0].Properties["test_framework"])
	}
	var conf string
	for _, r := range recs {
		if r.Properties["test_function"] != "create" {
			continue
		}
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == "UserService" {
				conf = rel.Properties["confidence"]
			}
		}
	}
	if conf == "" {
		t.Fatalf("no TESTS edge create -> UserService; entities=%d", len(recs))
	}
	if conf != "high" {
		t.Errorf("create -> UserService confidence=%q, want high", conf)
	}
}

// TestJUnit_MethodCallOnInjectedSUT — Spring-style injected field (no `new`),
// direct method call on the SUT instance resolves the method id at high.
func TestJUnit_MethodCallOnInjectedSUT(t *testing.T) {
	src := `package com.example;
import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;

@SpringBootTest
class OrderServiceTest {
    @Test
    void placesOrder() {
        orderService.placeOrder(42);
    }
}`
	recs := runExtract(t, "src/test/java/com/example/OrderServiceTest.java", "java", src)
	if !hasEdgeAny(recs, "placesOrder", "orderService.placeOrder") {
		t.Errorf("MISS: placesOrder -> orderService.placeOrder; rels=%s", relSummary(recs))
	}
}

// TestJUnit_ClassNameSubject — a @Test whose body has no production call (only
// an assertion) must still link to the class-under-test derived from the test
// class name (PaymentServiceTest → PaymentService). This is the describeSubject
// path added in #3855, mirroring the Kotlin/C# detectors.
func TestJUnit_ClassNameSubject(t *testing.T) {
	src := `package com.example;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.assertTrue;

class PaymentServiceTest {
    @Test
    void contextLoads() {
        assertTrue(true);
    }
}`
	recs := runExtract(t, "src/test/java/com/example/PaymentServiceTest.java", "java", src)
	if !hasEdgeAny(recs, "contextLoads", "PaymentService") {
		t.Errorf("MISS class-subject: contextLoads -> PaymentService; rels=%s", relSummary(recs))
	}
}

// TestJUnit_ParameterizedTest — JUnit 5 @ParameterizedTest (previously only
// @Test was detected) is now recognised and resolves its body call.
func TestJUnit_ParameterizedTest(t *testing.T) {
	src := `package com.example;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

class CalculatorTest {
    @ParameterizedTest
    @ValueSource(ints = {1, 2, 3})
    void adds(int n) {
        Calculator.add(n, n);
    }
}`
	recs := runExtract(t, "src/test/java/com/example/CalculatorTest.java", "java", src)
	if len(recs) == 0 {
		t.Fatalf("@ParameterizedTest not detected")
	}
	if !hasEdgeAny(recs, "adds", "Calculator.add") {
		t.Errorf("MISS: adds -> Calculator.add; rels=%s", relSummary(recs))
	}
}

// TestJUnit_MockMvc_NoHTTPClientNoise — a MockMvc HTTP integration test must NOT
// leak the MockMvc/Hamcrest builder chain (andExpect/status/isOk) as production
// TESTS edges. The honest linkage for such a controller IT is the file-name
// convention edge to the controller (low), not the result-matcher verbs. (#3855)
func TestJUnit_MockMvc_NoHTTPClientNoise(t *testing.T) {
	src := `package com.example;
import org.junit.jupiter.api.Test;

class UserControllerIT {
    @Test
    void getsUser() throws Exception {
        mockMvc.perform(get("/users/1"))
               .andExpect(status().isOk())
               .andExpect(jsonPath("$.id").value(1));
    }
}`
	recs := runExtract(t, "src/test/java/com/example/UserControllerIT.java", "java", src)
	// The MockMvc / Hamcrest result-matcher chain verbs are HTTP-test plumbing
	// and must be suppressed. Generic terminal accessors like `value` are NOT
	// blanket-suppressed (they collide with legitimate production method names) —
	// see the honest tests_linkage boundary recorded in the registry: Java
	// linkage is unit-level test→SUT; framework-handler attribution from HTTP
	// integration tests is out of scope.
	noise := map[string]bool{
		"andExpect": true, "status": true, "isOk": true, "perform": true,
		"jsonPath": true, "mockMvc.perform": true,
	}
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && noise[rel.Properties["tested"]] {
				t.Errorf("HTTP-test-client noise leaked as TESTS edge: %q", rel.Properties["tested"])
			}
		}
	}
}

// TestJUnit_RestAssured_NoDSLNoise — REST-assured given/when/then DSL chain must
// not leak its distinctive fluent verbs (statusCode/extract) as production
// edges. The generic terminal `body()` accessor is not blanket-suppressed (it
// collides with legitimate production method names); see the honest
// tests_linkage boundary in the registry.
func TestJUnit_RestAssured_NoDSLNoise(t *testing.T) {
	src := `package com.example;
import org.junit.jupiter.api.Test;
import static io.restassured.RestAssured.given;

class UserApiIT {
    @Test
    void getsUser() {
        given().when().get("/users/1").then().statusCode(200).extract().body();
    }
}`
	recs := runExtract(t, "src/test/java/com/example/UserApiIT.java", "java", src)
	noise := map[string]bool{"statusCode": true, "extract": true}
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && noise[rel.Properties["tested"]] {
				t.Errorf("REST-assured DSL noise leaked: %q", rel.Properties["tested"])
			}
		}
	}
}
