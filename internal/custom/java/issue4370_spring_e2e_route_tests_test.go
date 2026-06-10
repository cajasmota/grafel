package java_test

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/java"
)

// Issue #4370 LIVE-REPRO (extractor side).
//
// Spring integration tests (MockMvc / WebTestClient / TestRestTemplate / REST
// Assured) call routes by string but produced NO link to the
// http_endpoint_definition they exercise. This mirrors the NestJS/supertest fix
// #4351 and the Python fix #4369: the JUnit/TestNG extractor (ExtractJUnit5)
// now captures every Spring test-client `<verb>("/route")` call and stamps the
// `VERB route` pairs onto the one-per-file test_suite's `e2e_route_calls`
// property — the raw material the shared resolve pass
// (engine.linkE2ERouteTestsToEndpoints) turns into TESTS→http_endpoint_definition
// edges.

func extractJava4370(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_java_patterns")
	if !ok {
		t.Fatal("custom_java_patterns not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: path, Language: "java", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return ents
}

func suiteRouteCalls4370(t *testing.T, ents []types.EntityRecord) []string {
	t.Helper()
	for _, e := range ents {
		if e.Subtype == "test_suite" {
			raw := e.Properties["e2e_route_calls"]
			if raw == "" {
				return nil
			}
			return strings.Split(raw, "\n")
		}
	}
	return nil
}

// TestIssue4370_SpringTestClientCoverage exercises every required Spring test
// client — MockMvc, WebTestClient, TestRestTemplate, REST Assured — asserting
// each verb+route is captured, and that non-path / variable-built routes are
// NOT captured (conservative).
func TestIssue4370_SpringTestClientCoverage(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		want    []string
		notWant []string
	}{
		{
			name: "mockmvc",
			src: `package com.example.web;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.test.web.servlet.MockMvc;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.*;

class InspectionControllerTest {
    @Autowired MockMvc mockMvc;
    @Test
    void createsItem() throws Exception {
        mockMvc.perform(post("/api/v1/inspections/123/items").content("{}"))
               .andExpect(status().isCreated());
    }
    @Test
    void getsOne() throws Exception {
        mockMvc.perform(get("/api/v1/inspections/123"))
               .andExpect(status().isOk());
    }
}
`,
			want: []string{"POST /api/v1/inspections/123/items", "GET /api/v1/inspections/123"},
		},
		{
			name: "webtestclient",
			src: `package com.example.web;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.test.web.reactive.server.WebTestClient;

class InspectionRouterTest {
    @Autowired WebTestClient webTestClient;
    @Test
    void createsItem() {
        webTestClient.post().uri("/inspections/{id}/items", 7).exchange()
                     .expectStatus().isCreated();
    }
    @Test
    void getsOne() {
        webTestClient.get().uri("/inspections/7").exchange()
                     .expectStatus().isOk();
    }
}
`,
			want: []string{"POST /inspections/{id}/items", "GET /inspections/7"},
		},
		{
			name: "testresttemplate",
			src: `package com.example.web;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpMethod;
import org.springframework.http.HttpEntity;

class InspectionIntegrationTest {
    @Autowired TestRestTemplate restTemplate;
    @Test
    void getsOne() {
        restTemplate.getForEntity("/inspections/1", String.class);
    }
    @Test
    void posts() {
        restTemplate.postForObject("/inspections", new HttpEntity<>("{}"), String.class);
    }
    @Test
    void exchanges() {
        restTemplate.exchange("/inspections/5", HttpMethod.DELETE, HttpEntity.EMPTY, Void.class);
    }
}
`,
			want: []string{"GET /inspections/1", "POST /inspections", "DELETE /inspections/5"},
		},
		{
			name: "restassured",
			src: `package com.example.web;
import org.junit.jupiter.api.Test;
import static io.restassured.RestAssured.given;

class InspectionApiTest {
    @Test
    void getsOne() {
        given().when().get("/inspections/9").then().statusCode(200);
    }
    @Test
    void posts() {
        given().body("{}").when().post("/inspections").then().statusCode(201);
    }
}
`,
			want: []string{"GET /inspections/9", "POST /inspections"},
		},
		{
			name: "conservative_non_path_dropped",
			src: `package com.example.web;
import org.junit.jupiter.api.Test;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.web.util.UriComponentsBuilder;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.*;

class BuiltUrlTest {
    MockMvc mockMvc;
    @Test
    void builtUrl() throws Exception {
        String url = UriComponentsBuilder.fromPath("/x").build().toUriString();
        mockMvc.perform(get(url));
        mockMvc.perform(post("/real/path"));
    }
}
`,
			want:    []string{"POST /real/path"},
			notWant: []string{"GET url", "GET x"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents := extractJava4370(t, "src/test/java/com/example/web/"+tc.name+"Test.java", tc.src)
			calls := suiteRouteCalls4370(t, ents)
			joined := strings.Join(calls, "|")
			for _, w := range tc.want {
				found := false
				for _, c := range calls {
					if c == w {
						found = true
					}
				}
				if !found {
					t.Errorf("expected %q captured, got=%v", w, calls)
				}
			}
			for _, nw := range tc.notWant {
				if strings.Contains(joined, nw) {
					t.Errorf("did NOT expect %q captured, got=%v", nw, calls)
				}
			}
		})
	}
}

// TestIssue4370_NonHTTPGetNotCaptured proves a plain map/collection `.get(...)`
// inside a test never produces a phantom route (the route arg is not a
// leading-slash path so it is dropped).
func TestIssue4370_NonHTTPGetNotCaptured(t *testing.T) {
	const src = `package com.example;
import org.junit.jupiter.api.Test;

class CacheTest {
    @Test
    void usesMap() {
        java.util.Map<String,String> m = new java.util.HashMap<>();
        m.put("k", "v");
        m.get("k");
        cache.get("some-key");
    }
}
`
	ents := extractJava4370(t, "src/test/java/com/example/CacheTest.java", src)
	calls := suiteRouteCalls4370(t, ents)
	if len(calls) != 0 {
		t.Errorf("expected NO route calls from non-HTTP gets, got=%v", calls)
	}
}
