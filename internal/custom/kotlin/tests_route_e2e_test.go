package kotlin

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

func runKtRouteE2E(t *testing.T, path, src string) string {
	t.Helper()
	e := &kotlinTestRouteE2EExtractor{}
	ents, err := e.Extract(context.Background(), extractor.FileInput{
		Path: path, Language: "kotlin", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(ents) == 0 {
		return ""
	}
	if ents[0].Subtype != "test_suite" {
		t.Fatalf("expected test_suite, got %q", ents[0].Subtype)
	}
	return ents[0].Properties["e2e_route_calls"]
}

func TestKotlinRouteE2E_SpringMockMvc(t *testing.T) {
	src := "package t\n" +
		"class XControllerTest {\n" +
		"    @Test fun a() { mockMvc.perform(get(\"/api/v1/x/get_counts\")) }\n" +
		"    @Test fun b() { mockMvc.perform(post(\"/api/v1/x/items\")) }\n" +
		"}\n"
	got := runKtRouteE2E(t, "src/test/kotlin/XControllerTest.kt", src)
	if !strings.Contains(got, "GET /api/v1/x/get_counts") || !strings.Contains(got, "POST /api/v1/x/items") {
		t.Fatalf("MockMvc routes not captured: %q", got)
	}
}

func TestKotlinRouteE2E_WebTestClient(t *testing.T) {
	src := "package t\n" +
		"class XControllerTest {\n" +
		"    @Test fun a() { webTestClient.get().uri(\"/api/v1/x/get_counts\").exchange() }\n" +
		"}\n"
	got := runKtRouteE2E(t, "src/test/kotlin/XControllerTest.kt", src)
	if !strings.Contains(got, "GET /api/v1/x/get_counts") {
		t.Fatalf("WebTestClient route not captured: %q", got)
	}
}

func TestKotlinRouteE2E_KtorClient(t *testing.T) {
	src := "package t\n" +
		"import io.ktor.client.request.*\n" +
		"class XRoutesTest {\n" +
		"    @Test fun a() = testApplication { client.get(\"/api/v1/x/get_counts\") }\n" +
		"    @Test fun b() = testApplication { client.post(\"/api/v1/x/items\") }\n" +
		"}\n"
	got := runKtRouteE2E(t, "src/test/kotlin/XRoutesTest.kt", src)
	if !strings.Contains(got, "GET /api/v1/x/get_counts") || !strings.Contains(got, "POST /api/v1/x/items") {
		t.Fatalf("Ktor client routes not captured: %q", got)
	}
}

func TestKotlinRouteE2E_KtorHandleRequest(t *testing.T) {
	src := "package t\n" +
		"class XRoutesTest {\n" +
		"    @Test fun a() { handleRequest(HttpMethod.Get, \"/api/v1/x/get_counts\") }\n" +
		"}\n"
	got := runKtRouteE2E(t, "src/test/kotlin/XRoutesTest.kt", src)
	if !strings.Contains(got, "GET /api/v1/x/get_counts") {
		t.Fatalf("Ktor handleRequest route not captured: %q", got)
	}
}

// Non-test production controller mentioning a route must NOT mint a suite.
func TestKotlinRouteE2E_ProductionFileSkipped(t *testing.T) {
	src := "package t\n" +
		"class XController {\n" +
		"    fun reg() { get(\"/api/v1/x\") }\n" +
		"}\n"
	got := runKtRouteE2E(t, "src/main/kotlin/XController.kt", src)
	if got != "" {
		t.Fatalf("production file must not produce e2e_route_calls, got %q", got)
	}
}
