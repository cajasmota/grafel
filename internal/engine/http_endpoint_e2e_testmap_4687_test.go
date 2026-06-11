package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"

	// Register the Kotlin route-hit extractor so the test_suite (carrying
	// e2e_route_calls) comes from the REAL extractor, not a hand-built fixture.
	_ "github.com/cajasmota/archigraph/internal/custom/kotlin"
)

// Issue #4687 LIVE-REPRO (resolve side) — Kotlin Spring MockMvc + Ktor tests.
//
// Proves end-to-end that a Kotlin test calling a route by string
// (`mockMvc.perform(get("/api/v1/..."))` / Ktor `client.get("/api/v1/...")`)
// links to the http_endpoint_definition it exercises — the Kotlin slice of the
// all-language program (#4615), generalizing #4351 / #4369 / #4370 / #4371 /
// #4684 / #4685 / #4686. The shared linkE2ERouteTestsToEndpoints pass is
// language-agnostic; only the Kotlin route capture is new.

const ktSpringTestSrc4687 = `package com.app.test
import org.springframework.test.web.servlet.MockMvc
class XControllerTest {
    lateinit var mockMvc: MockMvc
    @Test fun getCounts() {
        mockMvc.perform(get("/api/v1/x/get_counts"))
    }
    @Test fun createItem() {
        mockMvc.perform(post("/api/v1/x/items"))
    }
}
`

const ktKtorTestSrc4687 = `package com.app.test
import io.ktor.client.request.*
class XRoutesTest {
    @Test fun getCounts() = testApplication {
        client.get("/api/v1/x/get_counts")
    }
    @Test fun createItem() = testApplication {
        client.post("/api/v1/x/items")
    }
}
`

func TestIssue4687_KotlinSpringE2ERouteTestsLinkToEndpoints(t *testing.T) {
	defs := []types.EntityRecord{
		def("GET", "/api/v1/x/get_counts"),
		def("POST", "/api/v1/x/items"),
	}
	suite := realSuite(t, "custom_kotlin_tests_route_e2e",
		"src/test/kotlin/com/app/XControllerTest.kt", "kotlin", ktSpringTestSrc4687)

	afterOut, edges := runE2ERouteResolve(t, defs, suite)
	if edges < 2 {
		t.Fatalf("expected >=2 e2e route TESTS edges (GET + POST), got %d", edges)
	}
	targets := edgeTargets(afterOut)
	assertKtRouteEdges(t, targets)
}

func TestIssue4687_KotlinKtorE2ERouteTestsLinkToEndpoints(t *testing.T) {
	defs := []types.EntityRecord{
		def("GET", "/api/v1/x/get_counts"),
		def("POST", "/api/v1/x/items"),
	}
	suite := realSuite(t, "custom_kotlin_tests_route_e2e",
		"src/test/kotlin/com/app/XRoutesTest.kt", "kotlin", ktKtorTestSrc4687)

	afterOut, edges := runE2ERouteResolve(t, defs, suite)
	if edges < 2 {
		t.Fatalf("expected >=2 e2e route TESTS edges (GET + POST), got %d", edges)
	}
	targets := edgeTargets(afterOut)
	assertKtRouteEdges(t, targets)
}

func assertKtRouteEdges(t *testing.T, targets map[string]bool) {
	t.Helper()
	wantGet, wantPost := false, false
	for to := range targets {
		if strings.Contains(to, "GET:/api/v1/x/get_counts") {
			wantGet = true
		}
		if strings.Contains(to, "POST:/api/v1/x/items") {
			wantPost = true
		}
	}
	if !wantGet {
		t.Errorf("expected a TESTS edge to GET /api/v1/x/get_counts; targets=%v", targets)
	}
	if !wantPost {
		t.Errorf("expected a TESTS edge to POST /api/v1/x/items; targets=%v", targets)
	}
}
