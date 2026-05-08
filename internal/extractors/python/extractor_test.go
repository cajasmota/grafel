package python_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
	tspython "github.com/smacker/go-tree-sitter/python"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
	// Blank import to trigger init() registration.
	_ "github.com/cajasmota/archigraph/internal/extractors/python"
)

// parse is a test helper that parses Python source with tree-sitter.
func parse(t *testing.T, src []byte) *sitter.Tree {
	t.Helper()
	p := sitter.NewParser()
	p.SetLanguage(tspython.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tree
}

// makeFile builds a FileInput for tests.
func makeFile(src string, tree *sitter.Tree) extractor.FileInput {
	return extractor.FileInput{
		Path:     "test.py",
		Content:  []byte(src),
		Language: "python",
		Tree:     tree,
	}
}

// TestExtract_TwoFunctions verifies that two top-level functions are extracted
// with correct names, kinds, and line numbers.
func TestExtract_TwoFunctions(t *testing.T) {
	src := `def foo():
    pass

def bar(x, y):
    return x + y
`
	tree := parse(t, []byte(src))
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(entities))
	}

	names := map[string]bool{}
	for _, e := range entities {
		names[e.Name] = true
		if e.Kind != "SCOPE.Operation" {
			t.Errorf("entity %q: expected Kind=SCOPE.Operation, got %q", e.Name, e.Kind)
		}
		if e.Subtype != "function" {
			t.Errorf("entity %q: expected Subtype=function, got %q", e.Name, e.Subtype)
		}
		if e.Language != "python" {
			t.Errorf("entity %q: expected Language=python, got %q", e.Name, e.Language)
		}
		if e.StartLine <= 0 {
			t.Errorf("entity %q: expected StartLine > 0, got %d", e.Name, e.StartLine)
		}
		if e.EndLine < e.StartLine {
			t.Errorf("entity %q: EndLine %d < StartLine %d", e.Name, e.EndLine, e.StartLine)
		}
	}
	if !names["foo"] {
		t.Error("expected entity named 'foo'")
	}
	if !names["bar"] {
		t.Error("expected entity named 'bar'")
	}
}

// TestExtract_FunctionLineNumbers verifies exact line numbers for a single function.
func TestExtract_FunctionLineNumbers(t *testing.T) {
	src := `# comment

def greet(name):
    return "hello " + name
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")

	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	e := entities[0]
	if e.Name != "greet" {
		t.Errorf("expected name=greet, got %q", e.Name)
	}
	// greet is defined on line 3
	if e.StartLine != 3 {
		t.Errorf("expected StartLine=3, got %d", e.StartLine)
	}
}

// TestExtract_ClassWithMethods verifies class extraction and method extraction
// with correct kinds and subtypes.
func TestExtract_ClassWithMethods(t *testing.T) {
	src := `class MyService:
    def __init__(self):
        pass

    def process(self, data):
        return data
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")

	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Expect: MyService (Component) + __init__ (Operation/method) + process (Operation/method)
	if len(entities) != 3 {
		t.Fatalf("expected 3 entities, got %d: %v", len(entities), entityNames(entities))
	}

	byName := map[string]interface{}{}
	for _, e := range entities {
		byName[e.Name] = e
	}

	// Class entity
	cls := entities[0] // class appears first in walk order
	for _, e := range entities {
		if e.Name == "MyService" {
			cls = e
		}
	}
	if cls.Kind != "SCOPE.Component" {
		t.Errorf("class MyService: expected Kind=SCOPE.Component, got %q", cls.Kind)
	}
	if cls.StartLine != 1 {
		t.Errorf("class MyService: expected StartLine=1, got %d", cls.StartLine)
	}

	// Methods — issue #45: emitted with class-qualified Name "<Class>.<method>"
	// so two classes can declare same-named methods in the same file without
	// colliding under ComputeID(SourceFile+Kind+Name).
	for _, name := range []string{"MyService.__init__", "MyService.process"} {
		found := false
		for _, e := range entities {
			if e.Name == name {
				found = true
				if e.Kind != "SCOPE.Operation" {
					t.Errorf("method %q: expected Kind=SCOPE.Operation, got %q", name, e.Kind)
				}
				if e.Subtype != "method" {
					t.Errorf("method %q: expected Subtype=method, got %q", name, e.Subtype)
				}
			}
		}
		if !found {
			t.Errorf("expected method %q not found", name)
		}
	}
	_ = byName
}

// TestExtract_DecoratedFunction verifies a @decorator-wrapped function
// is extracted with correct kind and no decorator in Properties.
func TestExtract_DecoratedFunction(t *testing.T) {
	src := `@app.get("/health")
async def health_check():
    return {"status": "ok"}
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")

	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	e := entities[0]
	if e.Name != "health_check" {
		t.Errorf("expected name=health_check, got %q", e.Name)
	}
	if e.Kind != "SCOPE.Operation" {
		t.Errorf("expected Kind=SCOPE.Operation, got %q", e.Kind)
	}
	// Base extractor does NOT emit decorator info in Properties.
	// Framework extractors handle that in later passes.
	if e.Properties != nil {
		if _, hasDecorators := e.Properties["decorators"]; hasDecorators {
			t.Error("base extractor must not emit 'decorators' in Properties")
		}
	}
}

// TestExtract_EmptyFile verifies that an empty file returns zero entities and no error.
func TestExtract_EmptyFile(t *testing.T) {
	src := ""
	ext, _ := extractor.Get("python")

	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.py",
		Content:  []byte(src),
		Language: "python",
		Tree:     nil, // nil tree: extractor must handle gracefully
	})
	if err != nil {
		t.Fatalf("Extract on empty file: unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities for empty file, got %d", len(entities))
	}
}

// TestExtract_MalformedPython verifies that a file with syntax errors returns
// partial results (not a panic or fatal error).
func TestExtract_MalformedPython(t *testing.T) {
	// Deliberately malformed: unclosed def, missing colon.
	src := `def foo()
    pass

class Bar:
    def ok(self):
        return 1
`
	// tree-sitter is error-tolerant, so parse still succeeds.
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")

	// Must not panic.
	var entities []interface{}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Extract panicked on malformed input: %v", r)
			}
		}()
		result, err := ext.Extract(context.Background(), makeFile(src, tree))
		if err != nil {
			// Partial extraction error is acceptable.
			t.Logf("Extract returned non-nil error (acceptable for malformed input): %v", err)
		}
		for _, e := range result {
			entities = append(entities, e)
		}
	}()
	// We expect at least some entities (tree-sitter is forgiving).
	t.Logf("malformed file: extracted %d entities", len(entities))
}

// TestExtract_NilTree verifies that a nil tree triggers internal parsing.
func TestExtract_NilTree(t *testing.T) {
	src := `def standalone():
    pass
`
	ext, _ := extractor.Get("python")

	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "standalone.py",
		Content:  []byte(src),
		Language: "python",
		Tree:     nil, // extractor must parse internally
	})
	if err != nil {
		t.Fatalf("Extract with nil tree: %v", err)
	}
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	if entities[0].Name != "standalone" {
		t.Errorf("expected name=standalone, got %q", entities[0].Name)
	}
}

// TestExtract_LargeFile verifies that a large Python file (>1 MB) is processed
// within 30 seconds and does not panic.
func TestExtract_LargeFile(t *testing.T) {
	// Build a source file slightly over 1 MB.
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		// Each function is ~220 bytes.
		sb.WriteString("def func_")
		for _, ch := range "abcdefghijklmnopq" {
			sb.WriteRune(ch)
		}
		sb.WriteString("_")
		sb.WriteString(string(rune('a' + i%26)))
		sb.WriteString("(x, y, z):\n")
		sb.WriteString("    \"\"\"Function docstring for testing.\"\"\"\n")
		sb.WriteString("    result = x + y + z\n")
		sb.WriteString("    return result\n\n")
	}
	src := sb.String()

	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")

	done := make(chan struct{})
	var entityCount int
	go func() {
		defer close(done)
		entities, err := ext.Extract(context.Background(), makeFile(src, tree))
		if err != nil {
			t.Errorf("Extract on large file: %v", err)
		}
		entityCount = len(entities)
	}()

	select {
	case <-done:
		t.Logf("large file: extracted %d entities from %d bytes", entityCount, len(src))
	case <-time.After(30 * time.Second):
		t.Fatal("Extract timed out on large file (>30s)")
	}
}

// TestExtract_ClassNoMethods verifies a class with no methods produces exactly
// one entity (the class itself).
func TestExtract_ClassNoMethods(t *testing.T) {
	src := `class Empty:
    pass
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")

	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	if entities[0].Kind != "SCOPE.Component" {
		t.Errorf("expected Kind=SCOPE.Component, got %q", entities[0].Kind)
	}
}

// TestExtract_QualifiedNameNull verifies that the base extractor sets an empty
// QualifiedName for all entities (not a qualified path like ClassName.method).
// The golden fixture has qualified_name=null for all entities.
func TestExtract_QualifiedNameNull(t *testing.T) {
	src := `class Validator:
    def validate(self):
        return True

def standalone():
    pass
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")

	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, e := range entities {
		if e.QualifiedName != "" {
			t.Errorf("entity %q: expected empty QualifiedName (null in JSON), got %q", e.Name, e.QualifiedName)
		}
	}
}

// TestExtract_Signature verifies that function and class signatures are populated.
func TestExtract_Signature(t *testing.T) {
	src := `class Foo:
    pass

def add(a: int, b: int) -> int:
    return a + b
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")

	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(entities))
	}

	for _, e := range entities {
		if e.Signature == "" {
			t.Errorf("entity %q: expected non-empty Signature", e.Name)
		}
		if e.Name == "Foo" && e.Signature != "class Foo" {
			t.Errorf("class Foo: expected Signature='class Foo', got %q", e.Signature)
		}
		if e.Name == "add" && !strings.HasPrefix(e.Signature, "def add") {
			t.Errorf("func add: expected Signature to start with 'def add', got %q", e.Signature)
		}
	}
}

// TestExtract_BinaryContent verifies that binary content labeled as Python
// returns gracefully (empty entities, no panic).
func TestExtract_BinaryContent(t *testing.T) {
	// Binary-like content.
	binary := []byte{0xFF, 0xFE, 0x00, 0x01, 0xDE, 0xAD, 0xBE, 0xEF}
	ext, _ := extractor.Get("python")

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Extract panicked on binary input: %v", r)
			}
		}()
		entities, err := ext.Extract(context.Background(), extractor.FileInput{
			Path:     "binary.py",
			Content:  binary,
			Language: "python",
			Tree:     nil,
		})
		if err != nil {
			t.Logf("binary input returned error (acceptable): %v", err)
		}
		t.Logf("binary input: extracted %d entities", len(entities))
	}()
}

// TestExtract_Language verifies that the Language() method returns "python".
func TestExtract_Language(t *testing.T) {
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	if ext.Language() != "python" {
		t.Errorf("Language() = %q, want %q", ext.Language(), "python")
	}
}

// TestExtract_DuplicateMethodNamesAcrossClasses is the regression test for
// issue #45. Two classes in the same file each declare a `validate` and a
// `save` method. The extractor must emit four DISTINCT method entities with
// class-qualified Names so ComputeID(SourceFile+Kind+Name) produces four
// distinct IDs rather than collapsing the same-named methods into two.
func TestExtract_DuplicateMethodNamesAcrossClasses(t *testing.T) {
	src := `class UserSerializer:
    def validate(self, value):
        return value

    def save(self, value):
        return value


class OrderSerializer:
    def validate(self, value):
        return value

    def save(self, value):
        return value
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")

	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Expected method-entity Names — class-qualified (issue #45).
	wantMethods := map[string]bool{
		"UserSerializer.validate":  false,
		"UserSerializer.save":      false,
		"OrderSerializer.validate": false,
		"OrderSerializer.save":     false,
	}
	methodCount := 0
	for _, e := range entities {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "method" {
			methodCount++
			if _, ok := wantMethods[e.Name]; ok {
				wantMethods[e.Name] = true
			}
		}
	}
	if methodCount != 4 {
		t.Errorf("expected 4 distinct method entities, got %d (names=%v)",
			methodCount, entityNames(entities))
	}
	for name, seen := range wantMethods {
		if !seen {
			t.Errorf("expected method entity %q not found in %v",
				name, entityNames(entities))
		}
	}

	// IDs must be distinct under ComputeID(SourceFile+Kind+Name).
	ids := map[string]string{}
	for _, e := range entities {
		if e.Kind != "SCOPE.Operation" || e.Subtype != "method" {
			continue
		}
		id := e.ComputeID()
		if existing, ok := ids[id]; ok {
			t.Errorf("method ID collision: %q and %q both compute to %s",
				existing, e.Name, id)
		}
		ids[id] = e.Name
	}

	// Each class must own a CONTAINS edge per method (4 total: 2 per class).
	for _, cls := range []string{"UserSerializer", "OrderSerializer"} {
		count := 0
		for _, e := range entities {
			if e.Kind == "SCOPE.Component" && e.Name == cls {
				for _, r := range e.Relationships {
					if r.Kind == "CONTAINS" {
						count++
					}
				}
			}
		}
		if count != 2 {
			t.Errorf("class %s: expected 2 CONTAINS edges, got %d", cls, count)
		}
	}
}

// TestExtract_DuplicateMethodsFromFixture mirrors the inline test above against
// the committed testdata fixture so the on-disk artifact stays in sync with
// the regression contract.
func TestExtract_DuplicateMethodsFromFixture(t *testing.T) {
	path := filepath.Join("testdata", "duplicate_methods.py.fixture")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tree := parse(t, src)
	ext, _ := extractor.Get("python")

	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  src,
		Language: "python",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	methodCount := 0
	for _, e := range entities {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "method" {
			methodCount++
		}
	}
	if methodCount != 4 {
		t.Errorf("fixture: expected 4 method entities, got %d (names=%v)",
			methodCount, entityNames(entities))
	}

	// Issue #70 — strengthen the regression contract: each emitted method
	// must produce a distinct ComputeID(). Counting alone would silently
	// pass if two same-named methods on different classes collided to the
	// same ID under ComputeID(SourceFile+Kind+Name).
	ids := map[string]string{}
	for _, e := range entities {
		if e.Kind != "SCOPE.Operation" || e.Subtype != "method" {
			continue
		}
		id := e.ComputeID()
		if existing, ok := ids[id]; ok {
			t.Errorf("fixture: ComputeID collision: %q and %q both compute to %s",
				existing, e.Name, id)
		}
		ids[id] = e.Name
	}
	if len(ids) != 4 {
		t.Errorf("fixture: expected 4 distinct method ComputeIDs, got %d", len(ids))
	}
}

// TestExtract_ControlFlowMethodsInheritClassQualifier is the regression test
// for issue #70: methods declared inside if/try/with blocks within a class
// body must inherit the enclosing class qualifier (emitted as "Foo.trace",
// not bare "trace"). The walker preserves parentClass through its default
// recursion branch — this test pins that behavior to the on-disk fixture.
func TestExtract_ControlFlowMethodsInheritClassQualifier(t *testing.T) {
	path := filepath.Join("testdata", "control_flow_methods.py.fixture")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tree := parse(t, src)
	ext, _ := extractor.Get("python")

	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  src,
		Language: "python",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Every method declared in the fixture lives inside an if/try/with block
	// nested in a class body. None of them must appear as bare names.
	wantQualified := map[string]bool{
		"Foo.trace":       false,
		"Foo.trace_off":   false,
		"Foo.maybe":       false,
		"Foo.fallback":    false,
		"Foo.with_method": false,
		"Bar.only_in_bar": false,
	}
	forbiddenBare := map[string]bool{
		"trace":       true,
		"trace_off":   true,
		"maybe":       true,
		"fallback":    true,
		"with_method": true,
		"only_in_bar": true,
	}

	for _, e := range entities {
		if e.Kind != "SCOPE.Operation" || e.Subtype != "method" {
			continue
		}
		if forbiddenBare[e.Name] {
			t.Errorf("method %q emitted without class qualifier — expected <Class>.%s",
				e.Name, e.Name)
		}
		if _, ok := wantQualified[e.Name]; ok {
			wantQualified[e.Name] = true
		}
	}
	for name, seen := range wantQualified {
		if !seen {
			t.Errorf("expected qualified method %q not found in %v",
				name, entityNames(entities))
		}
	}

	// CONTAINS edges from each class must reach the methods declared inside
	// its control-flow blocks — proving the walker treated those nested
	// function_definitions as members of the class body.
	wantContains := map[string]int{
		"Foo": 5, // trace, trace_off, maybe, fallback, with_method
		"Bar": 1, // only_in_bar
	}
	for cls, want := range wantContains {
		count := 0
		for _, e := range entities {
			if e.Kind == "SCOPE.Component" && e.Name == cls {
				for _, r := range e.Relationships {
					if r.Kind == "CONTAINS" {
						count++
					}
				}
			}
		}
		if count != want {
			t.Errorf("class %s: expected %d CONTAINS edges, got %d", cls, want, count)
		}
	}
}

// TestExtract_ClassSubtypeLabels is the regression test for issue #46.
// Every declared class_definition must be emitted with Subtype="class".
// Base-class references in the parentheses (e.g. serializers.ModelSerializer)
// are NOT declarations — the Python base extractor must not emit entities for
// them at all, so they cannot end up with subtype="class" via this extractor.
func TestExtract_ClassSubtypeLabels(t *testing.T) {
	path := filepath.Join("testdata", "subtype_labels.py.fixture")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tree := parse(t, src)
	ext, _ := extractor.Get("python")

	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  src,
		Language: "python",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	wantClasses := map[string]bool{
		"UserSerializer": false, // (1) extends external base
		"Base":           false, // (3) stand-alone
		"Child":          false, // (2) extends another declared class
		"EmptyParens":    false, // (4) empty parentheses, no base
	}
	// Names that MUST NOT appear as declared-class entities — these are
	// references to external bases, not declarations.
	forbiddenAsClass := map[string]bool{
		"ModelSerializer":             true,
		"serializers.ModelSerializer": true,
		"serializers":                 true,
	}

	for _, e := range entities {
		if e.Kind != "SCOPE.Component" {
			continue
		}
		// Module-import entities also use Kind=SCOPE.Component but carry
		// Subtype="module" — skip them here.
		if e.Subtype == "module" {
			continue
		}
		if _, ok := wantClasses[e.Name]; ok {
			if e.Subtype != "class" {
				t.Errorf("declared class %q: expected Subtype=%q, got %q",
					e.Name, "class", e.Subtype)
			}
			wantClasses[e.Name] = true
			continue
		}
		if forbiddenAsClass[e.Name] {
			t.Errorf("base-class reference %q must not be emitted by the "+
				"Python base extractor as a declared class (Subtype=%q)",
				e.Name, e.Subtype)
		}
	}
	for name, seen := range wantClasses {
		if !seen {
			t.Errorf("expected declared class %q in entities=%v",
				name, entityNames(entities))
		}
	}
}

// TestExtract_NestedClassesFromFixture is the regression test for issue #68.
// Methods declared inside a nested class must carry the FULL dotted scope
// path in their Name (e.g. "Outer.Inner.foo"), not just the immediate parent.
// This guarantees ComputeID(SourceFile+Kind+Name) is unique across sibling
// nested classes that declare same-named methods, and that Format B
// structural references can address them via the resolver's byMember index.
func TestExtract_NestedClassesFromFixture(t *testing.T) {
	path := filepath.Join("testdata", "nested_classes.py.fixture")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tree := parse(t, src)
	ext, _ := extractor.Get("python")

	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  src,
		Language: "python",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	wantMethods := map[string]bool{
		"Outer.Inner.foo":         false,
		"Outer.Inner.Deep.bar":    false,
		"Outer.Sibling.foo":       false,
		"Outer.Sibling.Deep.bar":  false,
		"Standalone.foo":          false,
	}
	for _, e := range entities {
		if e.Kind != "SCOPE.Operation" || e.Subtype != "method" {
			continue
		}
		if _, want := wantMethods[e.Name]; want {
			wantMethods[e.Name] = true
		}
	}
	for name, seen := range wantMethods {
		if !seen {
			t.Errorf("expected method entity %q (got names=%v)", name, entityNames(entities))
		}
	}

	// Distinct-entity assertion: each fully-qualified Name must appear exactly
	// once and produce a distinct entity (different Name → different ComputeID
	// at emit time). Sibling Inner/Sibling classes share the bare "foo" name;
	// the dotted scope path is what keeps them apart.
	counts := map[string]int{}
	for _, e := range entities {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "method" {
			counts[e.Name]++
		}
	}
	for name, n := range counts {
		if n != 1 {
			t.Errorf("method %q emitted %d times, want 1", name, n)
		}
	}
}

// entityNames returns entity names for test diagnostics.
func entityNames(entities []types.EntityRecord) []string {
	names := make([]string, len(entities))
	for i, e := range entities {
		names[i] = e.Name
	}
	return names
}
