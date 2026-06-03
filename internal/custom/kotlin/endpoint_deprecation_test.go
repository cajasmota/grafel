package kotlin

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Kotlin endpoint_deprecation_versioning port (#4136, epic #3628).
//
// Mirrors the flagship property contract EXACTLY: deprecated / deprecated_since
// / deprecated_replacement / deprecation_source / api_version.
// ---------------------------------------------------------------------------

func runKtDeprecation(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	ents, err := (&kotlinEndpointDeprecationExtractor{}).Extract(context.Background(), extractor.FileInput{
		Path: path, Language: "kotlin", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("endpoint_deprecation extract: %v", err)
	}
	return ents
}

func ktDepByName(ents []types.EntityRecord) map[string]types.EntityRecord {
	out := map[string]types.EntityRecord{}
	for _, e := range ents {
		out[e.Name] = e
	}
	return out
}

func ktDepMust(t *testing.T, byName map[string]types.EntityRecord, name string) types.EntityRecord {
	t.Helper()
	e, ok := byName[name]
	if !ok {
		keys := make([]string, 0, len(byName))
		for k := range byName {
			keys = append(keys, k)
		}
		t.Fatalf("endpoint %q not stamped (got: %v)", name, keys)
	}
	return e
}

// --- Ktor surface -----------------------------------------------------------

// The flagship value-asserting probe, Ktor form: a verb handler with
// @Deprecated("use /api/v2/users") in route /api/v1/users →
// deprecated=true + deprecated_replacement + api_version=1 + deprecation_source.
func TestKtDeprecation_KtorAnnotatedVersionedRoute(t *testing.T) {
	src := `fun Application.module() {
    routing {
        route("/api/v1") {
            @Deprecated("use /api/v2/users")
            get("/users") {
                call.respondText("users")
            }
            get("/health") {
                call.respondText("ok")
            }
        }
    }
}
`
	byName := ktDepByName(runKtDeprecation(t, "App.kt", src))

	dep := ktDepMust(t, byName, "GET /api/v1/users")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["deprecation_source"]; got != "@Deprecated" {
		t.Errorf("deprecation_source=%q, want @Deprecated", got)
	}
	if got := dep.Properties["deprecated_replacement"]; got != "/api/v2/users" {
		t.Errorf("deprecated_replacement=%q, want /api/v2/users", got)
	}
	if got := dep.Properties["api_version"]; got != "1" {
		t.Errorf("api_version=%q, want 1 (path-derived)", got)
	}
	if got := dep.Properties["http_method"]; got != "GET" {
		t.Errorf("http_method=%q, want GET", got)
	}

	// Negative-ish: the non-deprecated sibling under /api/v1 still pins
	// api_version (versioned path) but carries NO deprecated flag.
	live := ktDepMust(t, byName, "GET /api/v1/health")
	if _, ok := live.Properties["deprecated"]; ok {
		t.Fatalf("GET /api/v1/health deprecation leaked, want absent (props: %v)", live.Properties)
	}
	if got := live.Properties["api_version"]; got != "1" {
		t.Errorf("GET /api/v1/health api_version=%q, want 1", got)
	}
}

// ReplaceWith(...) is the canonical Kotlin replacement hint and wins; a
// `since 2.0` message pins deprecated_since.
func TestKtDeprecation_KtorReplaceWithAndSince(t *testing.T) {
	src := `fun Application.module() {
    routing {
        route("/v2") {
            @Deprecated("since 2.0", ReplaceWith("/v3/reports"))
            get("/reports") {
                call.respondText("reports")
            }
        }
    }
}
`
	byName := ktDepByName(runKtDeprecation(t, "App.kt", src))
	dep := ktDepMust(t, byName, "GET /v2/reports")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("deprecated=%q, want true", dep.Properties["deprecated"])
	}
	if got := dep.Properties["deprecated_since"]; got != "2.0" {
		t.Errorf("deprecated_since=%q, want 2.0", got)
	}
	if got := dep.Properties["deprecated_replacement"]; got != "/v3/reports" {
		t.Errorf("deprecated_replacement=%q, want /v3/reports (ReplaceWith wins)", got)
	}
	if got := dep.Properties["api_version"]; got != "2" {
		t.Errorf("api_version=%q, want 2", got)
	}
}

// A Sunset response header written via call.response.header(...) in the Ktor
// handler lambda body is the runtime deprecation signal.
func TestKtDeprecation_KtorSunsetResponseHeader(t *testing.T) {
	src := `fun Application.module() {
    routing {
        get("/payments") {
            call.response.header("Sunset", "Sat, 31 Dec 2025 23:59:59 GMT")
            call.respondText("paid")
        }
    }
}
`
	byName := ktDepByName(runKtDeprecation(t, "App.kt", src))
	dep := ktDepMust(t, byName, "GET /payments")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["deprecation_source"]; got != "Sunset response header" {
		t.Errorf("deprecation_source=%q, want 'Sunset response header'", got)
	}
}

// A KDoc `* @deprecated use /api/v2/x` tag above a Ktor verb handler marks it
// deprecated (the Kotlin doc-comment convention).
func TestKtDeprecation_KtorKDocDeprecated(t *testing.T) {
	src := `fun Application.module() {
    routing {
        /**
         * @deprecated use /api/v2/orders instead
         */
        get("/api/v1/orders") {
            call.respondText("orders")
        }
    }
}
`
	byName := ktDepByName(runKtDeprecation(t, "App.kt", src))
	dep := ktDepMust(t, byName, "GET /api/v1/orders")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["deprecation_source"]; got != "KDoc @deprecated" {
		t.Errorf("deprecation_source=%q, want 'KDoc @deprecated'", got)
	}
	if got := dep.Properties["deprecated_replacement"]; got != "/api/v2/orders" {
		t.Errorf("deprecated_replacement=%q, want /api/v2/orders", got)
	}
	if got := dep.Properties["api_version"]; got != "1" {
		t.Errorf("api_version=%q, want 1", got)
	}
}

// --- Spring-Kotlin surface --------------------------------------------------

// The flagship probe, Spring-Kotlin form: @Deprecated on a @GetMapping handler
// in a @RestController with class @RequestMapping("/api/v1") →
// deprecated + replacement + api_version=1 + source.
func TestKtDeprecation_SpringAnnotatedVersionedRoute(t *testing.T) {
	src := `@RestController
@RequestMapping("/api/v1")
class UserController {
    @Deprecated("use /api/v2/users")
    @GetMapping("/users")
    fun users(): String = "users"

    @GetMapping("/health")
    fun health(): String = "ok"
}
`
	byName := ktDepByName(runKtDeprecation(t, "UserController.kt", src))

	dep := ktDepMust(t, byName, "GET /api/v1/users")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["deprecation_source"]; got != "@Deprecated" {
		t.Errorf("deprecation_source=%q, want @Deprecated", got)
	}
	if got := dep.Properties["deprecated_replacement"]; got != "/api/v2/users" {
		t.Errorf("deprecated_replacement=%q, want /api/v2/users", got)
	}
	if got := dep.Properties["api_version"]; got != "1" {
		t.Errorf("api_version=%q, want 1", got)
	}
	if got := dep.Properties["framework"]; got != "spring-boot" {
		t.Errorf("framework=%q, want spring-boot", got)
	}

	// Non-deprecated sibling: api_version pinned, no deprecation fabricated.
	live := ktDepMust(t, byName, "GET /api/v1/health")
	if _, ok := live.Properties["deprecated"]; ok {
		t.Fatalf("GET /api/v1/health deprecation leaked (props: %v)", live.Properties)
	}
	if got := live.Properties["api_version"]; got != "1" {
		t.Errorf("api_version=%q, want 1", got)
	}
}

// A Deprecation response header via response.setHeader(...) in a Spring-Kotlin
// fun body is the runtime signal.
func TestKtDeprecation_SpringDeprecationResponseHeader(t *testing.T) {
	src := `@RestController
@RequestMapping("/api/v2")
class ReportController {
    @GetMapping("/reports")
    fun reports(response: HttpServletResponse): String {
        response.setHeader("Deprecation", "true")
        return "reports"
    }
}
`
	byName := ktDepByName(runKtDeprecation(t, "ReportController.kt", src))
	dep := ktDepMust(t, byName, "GET /api/v2/reports")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["deprecation_source"]; got != "Deprecation response header" {
		t.Errorf("deprecation_source=%q, want 'Deprecation response header'", got)
	}
	if got := dep.Properties["api_version"]; got != "2" {
		t.Errorf("api_version=%q, want 2", got)
	}
}

// --- Honest-partial negatives -----------------------------------------------

// A versionless, non-deprecated Ktor route is NOT emitted at all (the plain
// route op is left to ktor_routes.go) — neither api_version nor deprecated is
// fabricated.
func TestKtDeprecation_KtorVersionlessNonDeprecatedNotStamped(t *testing.T) {
	src := `fun Application.module() {
    routing {
        get("/status") {
            call.respondText("ok")
        }
    }
}
`
	ents := runKtDeprecation(t, "App.kt", src)
	if len(ents) != 0 {
		t.Fatalf("versionless non-deprecated route stamped %d entities, want 0: %v", len(ents), ents)
	}
}

// A Spring-Kotlin versionless non-deprecated handler is likewise not stamped.
func TestKtDeprecation_SpringVersionlessNonDeprecatedNotStamped(t *testing.T) {
	src := `@RestController
@RequestMapping("/status")
class StatusController {
    @GetMapping("/ping")
    fun ping(): String = "pong"
}
`
	ents := runKtDeprecation(t, "Status.kt", src)
	if len(ents) != 0 {
		t.Fatalf("versionless non-deprecated Spring handler stamped %d, want 0: %v", len(ents), ents)
	}
}

// `/apiv2something` is NOT a version segment — no api_version (segment-anchor
// negative, mirrors the flagship TestAPIVersion_NoFalseSegment).
func TestKtDeprecation_NoFalseVersionSegment(t *testing.T) {
	src := `fun Application.module() {
    routing {
        @Deprecated("gone")
        get("/apiv2something/x") {
            call.respondText("x")
        }
    }
}
`
	byName := ktDepByName(runKtDeprecation(t, "App.kt", src))
	dep := ktDepMust(t, byName, "GET /apiv2something/x")
	if got, ok := dep.Properties["api_version"]; ok {
		t.Fatalf("api_version=%q fabricated on non-segment path, want absent", got)
	}
	if dep.Properties["deprecated"] != "true" {
		t.Errorf("deprecated=%q, want true (annotation still fires)", dep.Properties["deprecated"])
	}
}

// A non-Kotlin file is a no-op.
func TestKtDeprecation_NonKotlinNoOp(t *testing.T) {
	src := `@Deprecated("x") @GetMapping("/api/v1/x")`
	ents, err := (&kotlinEndpointDeprecationExtractor{}).Extract(context.Background(), extractor.FileInput{
		Path: "x.java", Language: "java", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(ents) != 0 {
		t.Fatalf("non-kotlin file stamped %d entities, want 0", len(ents))
	}
}

// --- unit: ktAPIVersionFromPath ---------------------------------------------

func TestKtAPIVersionFromPath(t *testing.T) {
	cases := []struct {
		path   string
		wantV  int
		wantOK bool
	}{
		{"/api/v1/users", 1, true},
		{"/api/v2", 2, true},
		{"/v3/orders", 3, true},
		{"/v10/x", 10, true},
		{"/apiv2something/x", 0, false},
		{"/users", 0, false},
		{"/api/v0/x", 0, false},   // below min
		{"/api/v100/x", 0, false}, // above max
	}
	for _, c := range cases {
		v, ok := ktAPIVersionFromPath(c.path)
		if ok != c.wantOK || (ok && v != c.wantV) {
			t.Errorf("ktAPIVersionFromPath(%q)=(%d,%v), want (%d,%v)", c.path, v, ok, c.wantV, c.wantOK)
		}
	}
}

// --- unit: ktResolveAnnotationDeprecation -----------------------------------

func TestKtResolveAnnotationDeprecation(t *testing.T) {
	cases := []struct {
		name       string
		region     string
		wantDep    bool
		wantSource string
		wantRepl   string
		wantSince  string
	}{
		{"bare @Deprecated", "@Deprecated", true, "@Deprecated", "", ""},
		{"msg replacement", `@Deprecated("use /api/v2/x")`, true, "@Deprecated", "/api/v2/x", ""},
		{"ReplaceWith wins", `@Deprecated("old", ReplaceWith("/v2/y"))`, true, "@Deprecated", "/v2/y", ""},
		{"since msg", `@Deprecated("since 1.5")`, true, "@Deprecated", "", "1.5"},
		{"kdoc only", "* @deprecated use /v2/z", true, "KDoc @deprecated", "/v2/z", ""},
		{"no marker", "// plain comment", false, "", "", ""},
	}
	for _, c := range cases {
		v, ok := ktResolveAnnotationDeprecation(c.region)
		if ok != c.wantDep {
			t.Errorf("%s: ok=%v, want %v", c.name, ok, c.wantDep)
			continue
		}
		if !c.wantDep {
			continue
		}
		if v.source != c.wantSource {
			t.Errorf("%s: source=%q, want %q", c.name, v.source, c.wantSource)
		}
		if v.replacement != c.wantRepl {
			t.Errorf("%s: replacement=%q, want %q", c.name, v.replacement, c.wantRepl)
		}
		if v.since != c.wantSince {
			t.Errorf("%s: since=%q, want %q", c.name, v.since, c.wantSince)
		}
	}
}
