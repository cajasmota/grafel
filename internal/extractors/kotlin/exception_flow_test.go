package kotlin_test

import (
	"context"
	"os"
	"testing"

	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tskotlin "github.com/smacker/go-tree-sitter/kotlin"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/kotlin"
	"github.com/cajasmota/grafel/internal/types"
)

// extractKotlinRaw parses Kotlin source with the real grammar and runs the
// registered extractor, returning the resolved entity records.
func extractKotlinRaw(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("kotlin")
	if !ok {
		t.Fatal("kotlin extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Demo.kt",
		Content:  []byte(src),
		Language: "kotlin",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return recs
}

// ktExcEdge reports whether some entity named fromName carries a relationship
// of the given Kind (THROWS/CATCHES) pointing at the shared exception-type node
// for typeName — asserting the SPECIFIC handled/raised type, not len>0.
func ktExcEdge(recs []types.EntityRecord, fromName, kind, typeName string) bool {
	want := extractor.ExceptionTypeTargetID(typeName)
	for i := range recs {
		if recs[i].Name != fromName {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == kind && r.ToID == want {
				return true
			}
		}
	}
	return false
}

// ktExcNodeCount counts shared exception-type nodes for typeName (must be 1
// after convergence — the flagship invariant).
func ktExcNodeCount(recs []types.EntityRecord, typeName string) int {
	want := extractor.ExceptionTypeName(typeName)
	c := 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExceptionType) && recs[i].Name == want {
			c++
		}
	}
	return c
}

// ktExcNode returns the shared exception-type node for typeName (or nil),
// asserting flagship-shape parity on its fields.
func ktExcNode(recs []types.EntityRecord, typeName string) *types.EntityRecord {
	want := extractor.ExceptionTypeName(typeName)
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExceptionType) && recs[i].Name == want {
			return &recs[i]
		}
	}
	return nil
}

// TestKotlinExceptionFlow_ThrowAndCatchConverge: a typed `throw X()` in one
// function and a typed `catch (e: X)` in another converge on ONE shared node,
// matching the flagship convergence invariant.
func TestKotlinExceptionFlow_ThrowAndCatchConverge(t *testing.T) {
	src := `
class Repo {
    fun read(): User {
        throw NotFoundException("missing")
    }

    fun caller() {
        try {
            read()
        } catch (e: NotFoundException) {
            handle(e)
        }
    }
}
`
	recs := extractKotlinRaw(t, src)

	if !ktExcEdge(recs, "read", "THROWS", "NotFoundException") {
		t.Errorf("missing THROWS(read -> NotFoundException)")
	}
	if !ktExcEdge(recs, "caller", "CATCHES", "NotFoundException") {
		t.Errorf("missing CATCHES(caller -> NotFoundException)")
	}
	if n := ktExcNodeCount(recs, "NotFoundException"); n != 1 {
		t.Fatalf("throw + catch of NotFoundException must converge on ONE node, got %d", n)
	}

	// Flagship-shape parity on the shared node.
	node := ktExcNode(recs, "NotFoundException")
	if node == nil {
		t.Fatal("no NotFoundException exception-type node")
	}
	if node.Subtype != "exception_type" {
		t.Errorf("subtype = %q, want exception_type", node.Subtype)
	}
	if node.SourceFile != extractor.ExceptionTypeSourceFile {
		t.Errorf("source_file = %q, want synthetic %q", node.SourceFile, extractor.ExceptionTypeSourceFile)
	}
	if node.QualifiedName != extractor.ExceptionTypeTargetID("NotFoundException") {
		t.Errorf("qualified_name = %q, want %q", node.QualifiedName, extractor.ExceptionTypeTargetID("NotFoundException"))
	}
	if node.Properties["exception_type"] != "NotFoundException" {
		t.Errorf("exception_type prop = %q, want NotFoundException", node.Properties["exception_type"])
	}
}

// TestKotlinExceptionFlow_TypedCatch: `try { } catch (e: SqlException) { }`
// yields a CATCHES edge naming the SPECIFIC handled exception type.
func TestKotlinExceptionFlow_TypedCatch(t *testing.T) {
	src := `
class Db {
    fun query() {
        try {
            run()
        } catch (e: SqlException) {
            log(e)
        }
    }
}
`
	recs := extractKotlinRaw(t, src)
	if !ktExcEdge(recs, "query", "CATCHES", "SqlException") {
		t.Errorf("missing CATCHES(query -> SqlException)")
	}
}

// TestKotlinExceptionFlow_ResponseStatusException: `throw
// ResponseStatusException(HttpStatus.NOT_FOUND)` is a typed throw — the handled
// type is the thrown exception class.
func TestKotlinExceptionFlow_ResponseStatusException(t *testing.T) {
	src := `
class Ctrl {
    fun get(id: Int) {
        throw ResponseStatusException(HttpStatus.NOT_FOUND)
    }
}
`
	recs := extractKotlinRaw(t, src)
	if !ktExcEdge(recs, "get", "THROWS", "ResponseStatusException") {
		t.Errorf("missing THROWS(get -> ResponseStatusException)")
	}
}

// TestKotlinExceptionFlow_SpringExceptionHandler: a Spring
// `@ExceptionHandler(NotFoundException::class) fun handle(...)` method handles
// NotFoundException — CATCHES on the handler method, naming the SPECIFIC type.
func TestKotlinExceptionFlow_SpringExceptionHandler(t *testing.T) {
	src := `
@RestControllerAdvice
class GlobalErrors {
    @ExceptionHandler(NotFoundException::class)
    fun handle(e: NotFoundException): ResponseEntity<Error> {
        return ResponseEntity.status(404).build()
    }
}
`
	recs := extractKotlinRaw(t, src)
	if !ktExcEdge(recs, "handle", "CATCHES", "NotFoundException") {
		t.Errorf("missing CATCHES(handle -> NotFoundException) from @ExceptionHandler")
	}
}

// TestKotlinExceptionFlow_MultiExceptionHandler: `@ExceptionHandler(A::class,
// B::class)` yields a CATCHES edge for each named type.
func TestKotlinExceptionFlow_MultiExceptionHandler(t *testing.T) {
	src := `
class GlobalErrors {
    @ExceptionHandler(NotFoundException::class, BadRequestException::class)
    fun handle(e: Exception): ResponseEntity<Error> {
        return ResponseEntity.status(500).build()
    }
}
`
	recs := extractKotlinRaw(t, src)
	if !ktExcEdge(recs, "handle", "CATCHES", "NotFoundException") {
		t.Errorf("missing CATCHES(handle -> NotFoundException)")
	}
	if !ktExcEdge(recs, "handle", "CATCHES", "BadRequestException") {
		t.Errorf("missing CATCHES(handle -> BadRequestException)")
	}
}

// TestKotlinExceptionFlow_KtorStatusPages: Ktor `StatusPages {
// exception<AuthException> { ... } }` registers a handler for AuthException —
// CATCHES naming the SPECIFIC handled type.
func TestKotlinExceptionFlow_KtorStatusPages(t *testing.T) {
	src := `
fun Application.configure() {
    install(StatusPages) {
        exception<AuthException> { call, cause ->
            call.respond(HttpStatusCode.Unauthorized)
        }
        exception<NotFoundException> { call, cause ->
            call.respond(HttpStatusCode.NotFound)
        }
    }
}
`
	recs := extractKotlinRaw(t, src)
	if !ktExcEdge(recs, "configure", "CATCHES", "AuthException") {
		t.Errorf("missing CATCHES(configure -> AuthException) from Ktor StatusPages")
	}
	if !ktExcEdge(recs, "configure", "CATCHES", "NotFoundException") {
		t.Errorf("missing CATCHES(configure -> NotFoundException) from Ktor StatusPages")
	}
}

// TestKotlinExceptionFlow_QualifiedThrowConverges: `throw pkg.Boom(...)`
// converges to the bare class name (single node), cross-language consistent.
func TestKotlinExceptionFlow_QualifiedThrowConverges(t *testing.T) {
	src := `
class S {
    fun a() { throw com.acme.Boom("x") }
    fun b() {
        try { a() } catch (e: Boom) { }
    }
}
`
	recs := extractKotlinRaw(t, src)
	if !ktExcEdge(recs, "a", "THROWS", "Boom") {
		t.Errorf("missing THROWS(a -> Boom) from qualified throw")
	}
	if !ktExcEdge(recs, "b", "CATCHES", "Boom") {
		t.Errorf("missing CATCHES(b -> Boom)")
	}
	if n := ktExcNodeCount(recs, "Boom"); n != 1 {
		t.Fatalf("qualified throw + catch must converge on ONE Boom node, got %d", n)
	}
}

// --- Negatives: precision-first, no fabricated edges. ---

// TestKotlinExceptionFlow_NegativeBareRethrow: `throw e` (re-throw of a
// variable) carries no NEW type token → no THROWS edge.
func TestKotlinExceptionFlow_NegativeBareRethrow(t *testing.T) {
	src := `
class S {
    fun a() {
        try { run() } catch (e: Throwable) { throw e }
    }
}
`
	recs := extractKotlinRaw(t, src)
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "THROWS" {
				t.Errorf("bare re-throw must NOT emit THROWS, got %s -> %s", recs[i].Name, r.ToID)
			}
		}
	}
}

// TestKotlinExceptionFlow_NegativeFactoryThrow: `throw makeError()` (lowercase
// factory call, not a constructor) is a dynamic type → no THROWS edge.
func TestKotlinExceptionFlow_NegativeFactoryThrow(t *testing.T) {
	src := `
class S {
    fun a() { throw makeError("x") }
}
`
	recs := extractKotlinRaw(t, src)
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "THROWS" {
				t.Errorf("factory-call throw must NOT emit THROWS, got %s -> %s", recs[i].Name, r.ToID)
			}
		}
	}
}

// TestKotlinExceptionFlow_NegativeTryNoCatch: a `try { } finally { }` with no
// catch and a plain function emit no error_flow edges.
func TestKotlinExceptionFlow_NegativeTryNoCatch(t *testing.T) {
	src := `
class S {
    fun a() {
        try { run() } finally { cleanup() }
    }
    fun plain(): Int = 1 + 2
}
`
	recs := extractKotlinRaw(t, src)
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "THROWS" || r.Kind == "CATCHES" {
				t.Errorf("try/finally + plain fun must NOT emit error_flow, got %s %s -> %s", recs[i].Name, r.Kind, r.ToID)
			}
		}
	}
	if ktExcNodeCount(recs, "Throwable") != 0 {
		t.Error("no exception-type node expected for try/finally")
	}
}

// TestKotlinExceptionFlow_LiveFixture confirms the pass fires on a REAL
// on-disk .kt file (the Ktor chat sample) through the registered extractor —
// the `} catch (e: Exception) {` at line 41 must yield a CATCHES edge to the
// shared Exception node. Proves live .kt firing, not just synthetic strings.
func TestKotlinExceptionFlow_LiveFixture(t *testing.T) {
	const path = "../../../testdata/fixtures/real-world/kotlin/ktor_chat_application.kt"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("fixture unavailable: %v", err)
	}
	parser, perr := tssmacker.New().NewParser(tssmacker.WrapLanguage(tskotlin.GetLanguage()))
	if perr != nil {
		t.Fatalf("parser init: %v", perr)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ext, _ := extractor.Get("kotlin")
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: path, Content: src, Language: "kotlin", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	want := extractor.ExceptionTypeTargetID("Exception")
	found := false
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "CATCHES" && r.ToID == want {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("live Ktor fixture: missing CATCHES(-> Exception) from real catch (e: Exception)")
	}
	if ktExcNodeCount(recs, "Exception") != 1 {
		t.Fatalf("expected exactly one shared Exception node, got %d", ktExcNodeCount(recs, "Exception"))
	}
}
