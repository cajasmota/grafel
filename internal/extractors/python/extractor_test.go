package python_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tspython "github.com/smacker/go-tree-sitter/python"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	"github.com/cajasmota/grafel/internal/types"
	// Blank import to trigger init() registration.
	_ "github.com/cajasmota/grafel/internal/extractors/python"
)

// parse parses Python source into a binding-agnostic ts.Tree via the smacker
// adapter. Tests stamp the result on FileInput.TSTree (which the migrated Python
// extractor consumes). Using the smacker adapter keeps these tests linkable in
// the default build while exercising the ts façade (B2 #5418).
func parse(t *testing.T, src []byte) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tspython.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tree
}

// extractPy is a typed helper that parses src and runs the extractor.
func extractPy(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	tree := parse(t, []byte(src))
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "python",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return recs
}

// makeFile builds a FileInput for tests.
func makeFile(src string, tree ts.Tree) extractor.FileInput {
	return extractor.FileInput{
		Path:     "test.py",
		Content:  []byte(src),
		Language: "python",
		TSTree:   tree,
	}
}

// stripFileEntity drops infrastructure-only entities that the extractor emits
// automatically for every source file so legacy tests that count semantic
// entities (functions, classes, …) remain stable. Filtered kinds:
//
//   - SCOPE.Component/file  — per-file entity (#577)
//   - Module/package        — per-package entity (#1884)
func stripFileEntity(entities []types.EntityRecord) []types.EntityRecord {
	out := entities[:0:0]
	for _, e := range entities {
		switch {
		case e.Kind == "SCOPE.Component" && e.Subtype == "file":
			continue
		case e.Kind == string(types.EntityKindModule) && e.Subtype == "package":
			continue
		}
		out = append(out, e)
	}
	return out
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
	entities = stripFileEntity(entities)
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

// TestExtract_QualifiedName verifies that extracted Python entities carry
// a module-path-qualified name. Issue #1413.
func TestExtract_QualifiedName(t *testing.T) {
	src := `def foo():
    pass

class Bar:
    def baz(self):
        pass
`
	tree := parse(t, []byte(src))
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	// Use a file path that resembles a real module path so we can assert
	// the dotted module form.
	fi := extractor.FileInput{
		Path:     "app/orders/handlers.py",
		Content:  []byte(src),
		Language: "python",
		TSTree:   tree,
	}
	entities, err := ext.Extract(context.Background(), fi)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)

	byName := make(map[string]types.EntityRecord)
	for _, e := range entities {
		byName[e.Name] = e
	}

	cases := []struct {
		entityName string
		wantQN     string
	}{
		{"foo", "orders.handlers.foo"},
		{"Bar", "orders.handlers.Bar"},
		{"Bar.baz", "orders.handlers.Bar.baz"},
	}
	for _, tc := range cases {
		e, ok := byName[tc.entityName]
		if !ok {
			t.Errorf("entity %q not found", tc.entityName)
			continue
		}
		if e.QualifiedName != tc.wantQN {
			t.Errorf("entity %q: QualifiedName=%q, want %q", tc.entityName, e.QualifiedName, tc.wantQN)
		}
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
	entities = stripFileEntity(entities)
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
	entities = stripFileEntity(entities)
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
	entities = stripFileEntity(entities)
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
		TSTree:   nil, // nil tree: extractor must handle gracefully
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
		TSTree:   nil, // extractor must parse internally
	})
	if err != nil {
		t.Fatalf("Extract with nil tree: %v", err)
	}
	entities = stripFileEntity(entities)
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
	entities = stripFileEntity(entities)
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	if entities[0].Kind != "SCOPE.Component" {
		t.Errorf("expected Kind=SCOPE.Component, got %q", entities[0].Kind)
	}
}

// TestExtract_QualifiedNamePopulated verifies that the base extractor sets a
// module-path-qualified QualifiedName on all function, class, and method
// entities (issue #1413). makeFile uses path "test.py" → module "test".
func TestExtract_QualifiedNamePopulated(t *testing.T) {
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
	entities = stripFileEntity(entities)

	wantQN := map[string]string{
		"Validator":          "test.Validator",
		"Validator.validate": "test.Validator.validate",
		"standalone":         "test.standalone",
	}
	for _, e := range entities {
		if e.Kind != "SCOPE.Operation" && e.Kind != "SCOPE.Component" {
			continue // only assert on function/class/method entities
		}
		if e.Subtype == "field" || e.Subtype == "module" || e.Subtype == "file" {
			continue
		}
		want, ok := wantQN[e.Name]
		if !ok {
			continue // schema fields etc. are excluded
		}
		if e.QualifiedName != want {
			t.Errorf("entity %q: QualifiedName=%q, want %q", e.Name, e.QualifiedName, want)
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
	entities = stripFileEntity(entities)
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
			TSTree:   nil,
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
	entities = stripFileEntity(entities)

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
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)

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
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)

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
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)

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
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)

	wantMethods := map[string]bool{
		"Outer.Inner.foo":        false,
		"Outer.Inner.Deep.bar":   false,
		"Outer.Sibling.foo":      false,
		"Outer.Sibling.Deep.bar": false,
		"Standalone.foo":         false,
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

// TestExtract_ClassAttrFields_DRFViewSet covers the canonical DRF ViewSet
// pattern that motivated issue #526: class-attribute assignments at class
// scope (e.g. `serializer_class = ArticleSerializer`) must be emitted as
// Field entities so that `self.serializer_class(...)` CALLS edges resolve.
func TestExtract_ClassAttrFields_DRFViewSet(t *testing.T) {
	src := `class ArticleViewSet:
    serializer_class = ArticleSerializer
    queryset = Article.objects.all()
    permission_classes = [IsAuthenticated]
    pagination_class = LimitOffsetPagination

    def list(self, request):
        return self.serializer_class(self.queryset, many=True)
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)
	wantFields := map[string]bool{
		"ArticleViewSet.serializer_class":   false,
		"ArticleViewSet.queryset":           false,
		"ArticleViewSet.permission_classes": false,
		"ArticleViewSet.pagination_class":   false,
	}
	for _, e := range entities {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" {
			if _, want := wantFields[e.Name]; want {
				wantFields[e.Name] = true
			}
			if e.Language != "python" {
				t.Errorf("field %q: expected Language=python, got %q", e.Name, e.Language)
			}
			if e.SourceFile != "test.py" {
				t.Errorf("field %q: expected SourceFile=test.py, got %q", e.Name, e.SourceFile)
			}
			if e.StartLine <= 0 || e.EndLine < e.StartLine {
				t.Errorf("field %q: bad line range %d..%d", e.Name, e.StartLine, e.EndLine)
			}
		}
	}
	for name, seen := range wantFields {
		if !seen {
			t.Errorf("expected field entity %q (got %v)", name, entityNames(entities))
		}
	}
}

// TestExtract_ClassAttrFields_DjangoAndSQLAlchemy covers Django ORM field
// declarations and SQLAlchemy declarative columns — both express schema
// structure as class-attribute assignments, so the extractor must emit
// them as Field entities for graph completeness.
func TestExtract_ClassAttrFields_DjangoAndSQLAlchemy(t *testing.T) {
	src := `class Article(models.Model):
    title = models.CharField(max_length=200)
    body = models.TextField()
    author = models.ForeignKey(User, on_delete=models.CASCADE)

class UserDB(Base):
    __tablename__ = "users"
    id = Column(Integer, primary_key=True)
    email = Column(String, unique=True, nullable=False)

class CommentForm(ModelForm):
    model = Comment
    fields = ['body', 'article']
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)
	want := map[string]bool{
		"Article.title":      false,
		"Article.body":       false,
		"Article.author":     false,
		"UserDB.id":          false,
		"UserDB.email":       false,
		"CommentForm.model":  false,
		"CommentForm.fields": false,
	}
	for _, e := range entities {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" {
			if _, ok := want[e.Name]; ok {
				want[e.Name] = true
			}
		}
		// Dunder __tablename__ must NOT be emitted.
		if e.Name == "UserDB.__tablename__" {
			t.Errorf("dunder field __tablename__ should be skipped, got %+v", e)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("expected field entity %q (got %v)", name, entityNames(entities))
		}
	}
}

// TestExtract_ClassAttrFields_SkipNonClassScope verifies the field walker
// is strictly scoped to the immediate class body — assignments inside
// methods, inside conditionals, and at module level must NOT be emitted
// as Field entities (over-eager binding risk).
func TestExtract_ClassAttrFields_SkipNonClassScope(t *testing.T) {
	src := `module_level = "no"

class Foo:
    class_attr = "yes"

    def method(self):
        method_local = "no"
        self.instance_attr = "no"

    if True:
        conditional_attr = "no - conditional, not stable"
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)
	var fieldNames []string
	for _, e := range entities {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" {
			fieldNames = append(fieldNames, e.Name)
		}
	}
	if len(fieldNames) != 1 || fieldNames[0] != "Foo.class_attr" {
		t.Errorf("expected exactly one field [Foo.class_attr], got %v", fieldNames)
	}
}

// TestExtract_ClassAttrFields_AnnotatedAndTuple exercises PEP 526
// annotated assignments and tuple-pattern LHS — both still resolve to
// bare class attributes.
func TestExtract_ClassAttrFields_AnnotatedAndTuple(t *testing.T) {
	src := `class Config:
    name: str = "default"
    count: int = 0
    a, b = 1, 2
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)
	want := map[string]bool{
		"Config.name":  false,
		"Config.count": false,
		"Config.a":     false,
		"Config.b":     false,
	}
	for _, e := range entities {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" {
			if _, ok := want[e.Name]; ok {
				want[e.Name] = true
			}
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("expected field entity %q (got %v)", name, entityNames(entities))
		}
	}
}

// TestExtract_ClassAttrFields_EmitsContainsEdge verifies the class entity
// gets a CONTAINS edge for every emitted SCOPE.Schema/field, mirroring the
// class→method CONTAINS emission. Regression guard for the Django field
// orphan recovery — without this edge, ~56% of django-realworld orphans
// (class-scope attribute declarations) had zero relationships and inflated
// the orphan rate. The stub form is Format A:
// scope:schema:field:python:<file>:<Class>.<attr>, resolved via byLocation.
func TestExtract_ClassAttrFields_EmitsContainsEdge(t *testing.T) {
	src := `class Article(models.Model):
    title = models.CharField(max_length=200)
    body = models.TextField()

    def save(self, *args, **kwargs):
        super().save(*args, **kwargs)
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)
	var classRec *types.EntityRecord
	for i := range entities {
		if entities[i].Kind == "SCOPE.Component" && entities[i].Subtype == "class" && entities[i].Name == "Article" {
			classRec = &entities[i]
			break
		}
	}
	if classRec == nil {
		t.Fatalf("Article class entity not found; got %v", entityNames(entities))
	}
	wantStubs := map[string]bool{
		"scope:schema:field:python:test.py:Article.title":    false,
		"scope:schema:field:python:test.py:Article.body":     false,
		"scope:operation:method:python:test.py:Article.save": false,
	}
	for _, rel := range classRec.Relationships {
		if rel.Kind != "CONTAINS" {
			continue
		}
		if _, want := wantStubs[rel.ToID]; want {
			wantStubs[rel.ToID] = true
		}
	}
	for stub, seen := range wantStubs {
		if !seen {
			t.Errorf("expected CONTAINS edge with ToID=%q on Article; got rels=%+v", stub, classRec.Relationships)
		}
	}
}

// TestExtract_ClassAttrFields_ContainsEdge_DecoratedClass verifies the
// CONTAINS-for-field emission also fires on decorated classes (the second
// emission site in walkNode's decorated_definition branch).
func TestExtract_ClassAttrFields_ContainsEdge_DecoratedClass(t *testing.T) {
	src := `@dataclass
class Config:
    name: str = "default"
    count: int = 0
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)
	var classRec *types.EntityRecord
	for i := range entities {
		if entities[i].Kind == "SCOPE.Component" && entities[i].Subtype == "class" && entities[i].Name == "Config" {
			classRec = &entities[i]
			break
		}
	}
	if classRec == nil {
		t.Fatalf("Config class entity not found on decorated definition; got %v", entityNames(entities))
	}
	wantStubs := map[string]bool{
		"scope:schema:field:python:test.py:Config.name":  false,
		"scope:schema:field:python:test.py:Config.count": false,
	}
	for _, rel := range classRec.Relationships {
		if rel.Kind != "CONTAINS" {
			continue
		}
		if _, want := wantStubs[rel.ToID]; want {
			wantStubs[rel.ToID] = true
		}
	}
	for stub, seen := range wantStubs {
		if !seen {
			t.Errorf("expected CONTAINS edge with ToID=%q on decorated Config; got rels=%+v", stub, classRec.Relationships)
		}
	}
}

// ---------------------------------------------------------------------------
// Issue #757 — inner-class CONTAINS + Meta/Config property propagation
// ---------------------------------------------------------------------------

// TestExtract_DjangoModel_MetaContainsEdge verifies that a Django model with
// an inner class Meta emits:
//  1. A SCOPE.Component/class entity for Order.Meta.
//  2. A CONTAINS edge from Order → Order.Meta on the parent class entity.
//
// This was the gap: Meta was emitted as a dangling entity with no inbound edge.
func TestExtract_DjangoModel_MetaContainsEdge(t *testing.T) {
	src := `class Order(models.Model):
    customer = models.ForeignKey(User, on_delete=models.CASCADE)

    class Meta:
        db_table = "orders"
        ordering = ["-created_at"]
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)

	// 1. Order.Meta entity must exist as SCOPE.Component/class.
	var metaEntity *types.EntityRecord
	for i := range entities {
		if entities[i].Name == "Order.Meta" {
			metaEntity = &entities[i]
		}
	}
	if metaEntity == nil {
		t.Fatalf("Order.Meta entity not found; got %v", entityNames(entities))
	}
	if metaEntity.Kind != "SCOPE.Component" || metaEntity.Subtype != "class" {
		t.Errorf("Order.Meta: expected Kind=SCOPE.Component/Subtype=class, got %s/%s", metaEntity.Kind, metaEntity.Subtype)
	}

	// 2. Order class must have a CONTAINS edge pointing at Order.Meta.
	var orderEntity *types.EntityRecord
	for i := range entities {
		if entities[i].Name == "Order" && entities[i].Kind == "SCOPE.Component" {
			orderEntity = &entities[i]
		}
	}
	if orderEntity == nil {
		t.Fatalf("Order class entity not found; got %v", entityNames(entities))
	}
	wantToID := "scope:component:class:python:test.py:Order.Meta"
	found := false
	for _, rel := range orderEntity.Relationships {
		if rel.Kind == "CONTAINS" && rel.ToID == wantToID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Order: expected CONTAINS edge with ToID=%q; got rels=%+v", wantToID, orderEntity.Relationships)
	}
}

// TestExtract_DjangoModel_MetaAbstract verifies that `abstract = True` inside
// a class Meta propagates `is_abstract=true` onto the parent class entity.
func TestExtract_DjangoModel_MetaAbstract(t *testing.T) {
	src := `class TimestampedModel(models.Model):
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)

    class Meta:
        abstract = True
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)

	var cls *types.EntityRecord
	for i := range entities {
		if entities[i].Name == "TimestampedModel" && entities[i].Kind == "SCOPE.Component" {
			cls = &entities[i]
		}
	}
	if cls == nil {
		t.Fatalf("TimestampedModel entity not found; got %v", entityNames(entities))
	}
	if cls.Properties == nil || cls.Properties["is_abstract"] != "true" {
		t.Errorf("TimestampedModel: expected Properties[is_abstract]=true, got %v", cls.Properties)
	}
}

// TestExtract_DjangoModel_MetaDbTable verifies that `db_table = "orders"`
// inside a class Meta propagates `db_table="orders"` onto the parent entity.
func TestExtract_DjangoModel_MetaDbTable(t *testing.T) {
	src := `class Order(models.Model):
    amount = models.DecimalField(max_digits=10, decimal_places=2)

    class Meta:
        db_table = "shop_orders"
        app_label = "shop"
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)

	var cls *types.EntityRecord
	for i := range entities {
		if entities[i].Name == "Order" && entities[i].Kind == "SCOPE.Component" {
			cls = &entities[i]
		}
	}
	if cls == nil {
		t.Fatalf("Order entity not found; got %v", entityNames(entities))
	}
	if cls.Properties == nil {
		t.Fatalf("Order: Properties is nil, expected db_table and app_label")
	}
	if cls.Properties["db_table"] != "shop_orders" {
		t.Errorf("Order: expected Properties[db_table]=shop_orders, got %q", cls.Properties["db_table"])
	}
	if cls.Properties["app_label"] != "shop" {
		t.Errorf("Order: expected Properties[app_label]=shop, got %q", cls.Properties["app_label"])
	}
}

// TestExtract_GenericInnerClass_ContainsEdge verifies that a generic Python
// class with a generic inner class (not Django, no Meta name) still emits a
// CONTAINS edge from the outer class to the inner class. The CONTAINS emission
// is generic; the property propagation is framework-specific via the Meta name.
func TestExtract_GenericInnerClass_ContainsEdge(t *testing.T) {
	src := `class Outer:
    class Inner:
        def method(self):
            pass
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)

	var outerEntity *types.EntityRecord
	for i := range entities {
		if entities[i].Name == "Outer" && entities[i].Kind == "SCOPE.Component" {
			outerEntity = &entities[i]
		}
	}
	if outerEntity == nil {
		t.Fatalf("Outer entity not found; got %v", entityNames(entities))
	}
	wantToID := "scope:component:class:python:test.py:Outer.Inner"
	found := false
	for _, rel := range outerEntity.Relationships {
		if rel.Kind == "CONTAINS" && rel.ToID == wantToID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Outer: expected CONTAINS edge with ToID=%q for generic inner class; got rels=%+v", wantToID, outerEntity.Relationships)
	}
	// The generic Inner class must NOT have any framework properties propagated.
	var innerEntity *types.EntityRecord
	for i := range entities {
		if entities[i].Name == "Outer.Inner" {
			innerEntity = &entities[i]
		}
	}
	if innerEntity == nil {
		t.Fatalf("Outer.Inner entity not found; got %v", entityNames(entities))
	}
	// No is_abstract / db_table — those are Django-specific.
	if outerEntity.Properties != nil {
		if _, hasAbstract := outerEntity.Properties["is_abstract"]; hasAbstract {
			t.Errorf("Outer: unexpected is_abstract property for non-Meta inner class")
		}
	}
}

// TestExtract_PydanticModel_ConfigContainsEdge verifies that a Pydantic model
// with a class Config emits a CONTAINS edge and propagates orm_mode onto parent.
func TestExtract_PydanticModel_ConfigContainsEdge(t *testing.T) {
	src := `class UserSchema(BaseModel):
    id: int
    name: str

    class Config:
        orm_mode = True
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)

	var cls *types.EntityRecord
	for i := range entities {
		if entities[i].Name == "UserSchema" && entities[i].Kind == "SCOPE.Component" {
			cls = &entities[i]
		}
	}
	if cls == nil {
		t.Fatalf("UserSchema entity not found; got %v", entityNames(entities))
	}
	// CONTAINS edge to Config.
	wantToID := "scope:component:class:python:test.py:UserSchema.Config"
	found := false
	for _, rel := range cls.Relationships {
		if rel.Kind == "CONTAINS" && rel.ToID == wantToID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("UserSchema: expected CONTAINS edge with ToID=%q; got rels=%+v", wantToID, cls.Relationships)
	}
	// orm_mode propagation.
	if cls.Properties == nil || cls.Properties["orm_mode"] != "true" {
		t.Errorf("UserSchema: expected Properties[orm_mode]=true, got %v", cls.Properties)
	}
}

// TestExtract_FileEntityContainsTopLevelClass verifies that the file entity
// emits a CONTAINS structural-ref for every top-level class (issue #699b).
// The edge is needed so the graph has a file→class inbound link for each
// class declaration, giving agents a consistent hierarchy path.
func TestExtract_FileEntityContainsTopLevelClass(t *testing.T) {
	src := `class Order(models.Model):
    title = models.CharField(max_length=200)

    def save(self, *args, **kwargs):
        pass

class OrderSerializer:
    pass
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Find the file entity (first entity emitted is always the file entity).
	var fileEnt *types.EntityRecord
	for i := range entities {
		if entities[i].Kind == "SCOPE.Component" && entities[i].Subtype == "file" {
			fileEnt = &entities[i]
			break
		}
	}
	if fileEnt == nil {
		t.Fatalf("file entity not found; entities: %v", entityNames(entities))
	}

	// The file entity must have CONTAINS edges to both top-level classes.
	wantTargets := []string{
		"scope:component:class:python:test.py:Order",
		"scope:component:class:python:test.py:OrderSerializer",
	}
	for _, want := range wantTargets {
		found := false
		for _, rel := range fileEnt.Relationships {
			if rel.Kind == "CONTAINS" && rel.ToID == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("file entity: expected CONTAINS to_id=%q; got rels=%+v", want, fileEnt.Relationships)
		}
	}
}

// TestExtract_FileEntityContainsModuleLevelFunction verifies that the file
// entity emits a CONTAINS edge for every module-level function (issue #699b).
// Module-level functions are the ones with bare names (no dot), distinct from
// methods which carry a "ClassName.method" dotted name.
func TestExtract_FileEntityContainsModuleLevelFunction(t *testing.T) {
	src := `def validate_email(value):
    return "@" in value

def send_notification(user):
    pass

class Mailer:
    def send(self):
        pass
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var fileEnt *types.EntityRecord
	for i := range entities {
		if entities[i].Kind == "SCOPE.Component" && entities[i].Subtype == "file" {
			fileEnt = &entities[i]
			break
		}
	}
	if fileEnt == nil {
		t.Fatalf("file entity not found")
	}

	// Module-level functions must appear as CONTAINS targets.
	wantFunctions := []string{
		"scope:operation:method:python:test.py:validate_email",
		"scope:operation:method:python:test.py:send_notification",
	}
	for _, want := range wantFunctions {
		found := false
		for _, rel := range fileEnt.Relationships {
			if rel.Kind == "CONTAINS" && rel.ToID == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("file entity: expected CONTAINS to_id=%q; got rels=%+v", want, fileEnt.Relationships)
		}
	}

	// Class methods must NOT appear as file-level CONTAINS (only class→method CONTAINS).
	unwanted := "scope:operation:method:python:test.py:Mailer.send"
	for _, rel := range fileEnt.Relationships {
		if rel.Kind == "CONTAINS" && rel.ToID == unwanted {
			t.Errorf("file entity: unexpected CONTAINS to class method %q", unwanted)
		}
	}
}

// TestPascalCaseReceiverCALLSEdges verifies that bare PascalCase identifier
// receivers (e.g. `User.save(...)`) produce qualified CALLS edges
// `User.save` instead of an ambiguous bare `save`. Regression test for
// issue #557: Python dotted-receiver class member binding.
func TestPascalCaseReceiverCALLSEdges(t *testing.T) {
	src := `
def create_user():
    User.objects.create(username="alice")
    User.save(obj)
    Article.objects.filter(title="foo")
    lower_case_var.some_method()
    ALLOWED_HOSTS.append("example.com")
`
	tree := parse(t, []byte(src))
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	// Collect all CALLS ToIDs across all entities.
	calledTargets := map[string]bool{}
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" {
				calledTargets[r.ToID] = true
			}
		}
	}

	// Qualified CALLS edges expected (PascalCase receiver → class name prefix).
	// Note: chained `User.objects.create` — the receiver is `User.objects`
	// which is an attribute access, not a plain identifier, so `create` stays
	// bare-ambiguous. `User.save` has a plain identifier receiver `User`.
	wantQualified := "User.save"
	if !calledTargets[wantQualified] {
		t.Errorf("expected qualified CALLS edge %q; got targets: %v", wantQualified, calledTargets)
	}

	// Lower-case receiver (`lower_case_var`) must NOT produce a qualified edge.
	if calledTargets["lower_case_var.some_method"] {
		t.Errorf("should not produce qualified edge for lowercase receiver lower_case_var")
	}

	// SCREAMING_SNAKE receiver (`ALLOWED_HOSTS`) must NOT produce a qualified edge.
	if calledTargets["ALLOWED_HOSTS.append"] {
		t.Errorf("should not produce qualified edge for SCREAMING_SNAKE receiver ALLOWED_HOSTS")
	}
}

// Issue #749 — Django Model.Meta constraints CONTAINS edges
// ---------------------------------------------------------------------------

// TestExtract_DjangoModel_MetaConstraints verifies that a Django model with
// Meta.constraints = [UniqueConstraint(...), CheckConstraint(...)] emits:
//  1. Two SCOPE.Constraint entities with the correct Name, Kind, and Subtype.
//  2. CONTAINS edges from the parent class to each constraint.
//
// This closes the gap where constraints were either missing from the graph
// entirely or emitted as orphans by the SQLAlchemy ForeignKey YAML rule
// misfiring on Django model files.
func TestExtract_DjangoModel_MetaConstraints(t *testing.T) {
	src := `class Order(models.Model):
    user = models.ForeignKey(User, on_delete=models.CASCADE)
    quantity = models.IntegerField(default=0)

    class Meta:
        db_table = "orders"
        constraints = [
            models.UniqueConstraint(
                fields=["user", "post"],
                name="unique_user_post",
            ),
            models.CheckConstraint(
                condition=models.Q(quantity__gte=0),
                name="check_quantity_gte_zero",
            ),
        ]
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)

	// 1. Both SCOPE.Constraint entities must exist.
	wantConstraints := map[string]string{
		"Order.unique_user_post":        "unique",
		"Order.check_quantity_gte_zero": "check",
	}
	for wantName, wantSubtype := range wantConstraints {
		var found *types.EntityRecord
		for i := range entities {
			if entities[i].Name == wantName {
				found = &entities[i]
				break
			}
		}
		if found == nil {
			t.Errorf("constraint entity %q not found; got %v", wantName, entityNames(entities))
			continue
		}
		if found.Kind != "SCOPE.Constraint" {
			t.Errorf("%q: expected Kind=SCOPE.Constraint, got %q", wantName, found.Kind)
		}
		if found.Subtype != wantSubtype {
			t.Errorf("%q: expected Subtype=%q, got %q", wantName, wantSubtype, found.Subtype)
		}
	}

	// 2. Order class must have CONTAINS edges to each constraint.
	var orderEntity *types.EntityRecord
	for i := range entities {
		if entities[i].Name == "Order" && entities[i].Kind == "SCOPE.Component" {
			orderEntity = &entities[i]
			break
		}
	}
	if orderEntity == nil {
		t.Fatalf("Order class entity not found; got %v", entityNames(entities))
	}
	for wantName := range wantConstraints {
		wantToID := "scope:constraint:python:test.py:" + wantName
		found := false
		for _, rel := range orderEntity.Relationships {
			if rel.Kind == "CONTAINS" && rel.ToID == wantToID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Order: missing CONTAINS edge to %q; got rels=%+v", wantToID, orderEntity.Relationships)
		}
	}
}

// TestExtract_DjangoModel_MetaConstraints_BareNames verifies that constraints
// expressed without the `models.` prefix (e.g. after `from django.db.models
// import UniqueConstraint`) are also captured.
func TestExtract_DjangoModel_MetaConstraints_BareNames(t *testing.T) {
	src := `from django.db.models import UniqueConstraint, CheckConstraint

class Article(models.Model):
    class Meta:
        constraints = [
            UniqueConstraint(fields=["slug"], name="unique_article_slug"),
            CheckConstraint(condition=Q(rating__gte=0), name="article_rating_gte_zero"),
        ]
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)

	wantConstraints := []string{
		"Article.unique_article_slug",
		"Article.article_rating_gte_zero",
	}
	for _, wantName := range wantConstraints {
		var found bool
		for _, e := range entities {
			if e.Name == wantName && e.Kind == "SCOPE.Constraint" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("bare-name constraint %q not found; got %v", wantName, entityNames(entities))
		}
	}
}

// TestExtract_DjangoModel_MetaConstraints_NoNameArg verifies that constraints
// lacking a `name=` keyword argument are NOT emitted (they cannot be stably
// identified across indexing runs). The test ensures no partial/unnamed
// entities are added.
func TestExtract_DjangoModel_MetaConstraints_NoNameArg(t *testing.T) {
	src := `class Product(models.Model):
    class Meta:
        constraints = [
            models.UniqueConstraint(fields=["sku"]),
        ]
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	entities, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)
	for _, e := range entities {
		if e.Kind == "SCOPE.Constraint" {
			t.Errorf("unexpected SCOPE.Constraint entity for unnamed constraint: %+v", e)
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

// TestExtract_MigrationFilePruned verifies that auto-generated Django migration
// files (#1617 / #2283) emit exactly one Migration-kind entity (plus the
// file-level SCOPE.Component for import resolution) and NOT per-class or
// per-field/operation entities, while a non-migration file with identical
// content is fully extracted.
func TestExtract_MigrationFilePruned(t *testing.T) {
	// Issue #2548: opt-in to migration entity emission for this test.
	t.Setenv("GRAFEL_EMIT_MIGRATION_ENTITIES", "1")

	src := `from django.db import migrations, models


class Migration(migrations.Migration):
    dependencies = [("core", "0041_prev")]

    operations = [
        migrations.AddField(
            model_name="device",
            name="serial",
            field=models.CharField(max_length=64),
        ),
    ]
`
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	// Migration path → exactly 1 Migration entity + 1 file entity; no AST
	// walk entities (class bodies, SCOPE.Schema fields, etc.).
	tree := parse(t, []byte(src))
	migEnts, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: "core/migrations/0042_device_serial.py", Content: []byte(src),
		Language: "python", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("extract migration: %v", err)
	}
	semantic := stripFileEntity(migEnts)
	if len(semantic) != 1 {
		t.Fatalf("migration file should emit exactly 1 semantic (Migration) entity, got %d: %v", len(semantic), semantic)
	}
	migEntity := semantic[0]
	if migEntity.Kind != "Migration" || migEntity.Subtype != "django" {
		t.Errorf("migration entity should be kind=Migration subtype=django, got %s/%s", migEntity.Kind, migEntity.Subtype)
	}
	if migEntity.Name != "0042_device_serial" {
		t.Errorf("migration entity name should be filename stem, got %q", migEntity.Name)
	}
	if migEntity.Properties["op_count"] != "1" {
		t.Errorf("migration entity op_count should be 1, got %q", migEntity.Properties["op_count"])
	}
	if !strings.Contains(migEntity.Properties["operations"], "AddField") {
		t.Errorf("migration entity operations should contain AddField, got %q", migEntity.Properties["operations"])
	}
	if len(migEnts) < 2 {
		t.Fatal("migration file should still emit the file-level entity for import resolution")
	}

	// Same content in a non-migration path → fully extracted (the Migration class).
	tree2 := parse(t, []byte(src))
	normEnts, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: "core/models/device.py", Content: []byte(src),
		Language: "python", TSTree: tree2,
	})
	if err != nil {
		t.Fatalf("extract normal: %v", err)
	}
	if len(stripFileEntity(normEnts)) == 0 {
		t.Error("non-migration file with a class should emit semantic entities")
	}
}

// TestExtract_DjangoMigrationFixtures pins the #1731 / #2283 migration-entity
// behaviour against on-disk fixtures so any regression is caught at the
// fixture level.
//
//   - django_migration.py.fixture  — lives under core/migrations/ → exactly
//     ONE semantic entity (kind=Migration, subtype=django) plus the file-level
//     SCOPE.Component/file entity for import resolution.
//   - django_models.py.fixture     — lives under core/models/ → fully extracted;
//     at least Device class + two methods emitted.
func TestExtract_DjangoMigrationFixtures(t *testing.T) {
	// Issue #2548: opt-in to migration entity emission for this test.
	t.Setenv("GRAFEL_EMIT_MIGRATION_ENTITIES", "1")

	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	// --- migration fixture: must produce exactly ONE semantic (Migration) entity ---
	migSrc, err := os.ReadFile(filepath.Join("testdata", "django_migration.py.fixture"))
	if err != nil {
		t.Fatalf("read django_migration fixture: %v", err)
	}
	migEnts, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "core/migrations/0042_device_serial_number.py",
		Content:  migSrc,
		Language: "python",
		TSTree:   parse(t, migSrc),
	})
	if err != nil {
		t.Fatalf("extract django_migration fixture: %v", err)
	}
	semantic := stripFileEntity(migEnts)
	if len(semantic) != 1 {
		names := make([]string, 0, len(semantic))
		for _, e := range semantic {
			names = append(names, e.Kind+"/"+e.Subtype+":"+e.Name)
		}
		t.Fatalf("django_migration fixture: expected exactly 1 semantic (Migration) entity, got %d: %v", len(semantic), names)
	}
	migEnt := semantic[0]
	if migEnt.Kind != "Migration" || migEnt.Subtype != "django" {
		t.Errorf("django_migration fixture: semantic entity must be Migration/django, got %s/%s", migEnt.Kind, migEnt.Subtype)
	}
	if migEnt.Name != "0042_device_serial_number" {
		t.Errorf("django_migration fixture: entity name must be filename stem, got %q", migEnt.Name)
	}
	// Fixture has 3 operations: AddField x2, AlterField x1.
	if migEnt.Properties["op_count"] != "3" {
		t.Errorf("django_migration fixture: expected op_count=3, got %q", migEnt.Properties["op_count"])
	}
	ops := migEnt.Properties["operations"]
	for _, opType := range []string{"AddField", "AlterField"} {
		if !strings.Contains(ops, opType) {
			t.Errorf("django_migration fixture: operations should contain %q, got %q", opType, ops)
		}
	}
	if len(migEnts) < 2 {
		t.Fatal("django_migration fixture: file-level entity must be preserved for import resolution")
	}
	fileEnt := migEnts[0]
	if fileEnt.Kind != "SCOPE.Component" || fileEnt.Subtype != "file" {
		t.Errorf("django_migration fixture: entities[0] must be file entity, got %s/%s", fileEnt.Kind, fileEnt.Subtype)
	}

	// --- models fixture: must be fully extracted ---
	modSrc, err := os.ReadFile(filepath.Join("testdata", "django_models.py.fixture"))
	if err != nil {
		t.Fatalf("read django_models fixture: %v", err)
	}
	modEnts, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "core/models.py",
		Content:  modSrc,
		Language: "python",
		TSTree:   parse(t, modSrc),
	})
	if err != nil {
		t.Fatalf("extract django_models fixture: %v", err)
	}
	modSemantic := stripFileEntity(modEnts)
	if len(modSemantic) == 0 {
		t.Fatal("django_models fixture: must emit semantic entities; got none")
	}

	// Assert at least the Device class and its two non-dunder methods are present.
	var (
		hasDeviceClass  bool
		hasStrMethod    bool
		hasGetURLMethod bool
	)
	for _, e := range modSemantic {
		switch {
		case e.Kind == "SCOPE.Component" && e.Subtype == "class" && e.Name == "Device":
			hasDeviceClass = true
		case e.Kind == "SCOPE.Operation" && e.Name == "Device.__str__":
			hasStrMethod = true
		case e.Kind == "SCOPE.Operation" && e.Name == "Device.get_absolute_url":
			hasGetURLMethod = true
		}
	}
	if !hasDeviceClass {
		t.Error("django_models fixture: expected SCOPE.Component/class 'Device'")
	}
	if !hasStrMethod {
		t.Error("django_models fixture: expected SCOPE.Operation 'Device.__str__'")
	}
	if !hasGetURLMethod {
		t.Error("django_models fixture: expected SCOPE.Operation 'Device.get_absolute_url'")
	}
}

// TestExtract_DjangoMigration_OneEntityPerFile is the canonical regression
// guard for #2283: a Django migration file with 3+ operations must emit
// exactly one kind=Migration entity (plus the file-level SCOPE.Component for
// import resolution). Operations are preserved in entity properties.
func TestExtract_DjangoMigration_OneEntityPerFile(t *testing.T) {
	// Issue #2548: opt-in to migration entity emission for this test.
	t.Setenv("GRAFEL_EMIT_MIGRATION_ENTITIES", "1")

	src := `from django.db import migrations, models


class Migration(migrations.Migration):
    dependencies = [
        ("core", "0043_prev"),
        ("auth", "0001_initial"),
    ]

    operations = [
        migrations.AddField(
            model_name="user",
            name="cognito_id",
            field=models.CharField(max_length=255, blank=True),
        ),
        migrations.RemoveField(
            model_name="user",
            name="legacy_token",
        ),
        migrations.AlterField(
            model_name="user",
            name="email",
            field=models.EmailField(max_length=254, unique=True),
        ),
        migrations.CreateModel(
            name="Profile",
            fields=[
                ("id", models.AutoField(primary_key=True)),
                ("bio", models.TextField(blank=True)),
            ],
        ),
    ]
`
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	path := "accounts/migrations/0044_user_cognito_id.py"
	tree := parse(t, []byte(src))
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: path, Content: []byte(src), Language: "python", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// --- Entity count: exactly 1 Migration + 1 file entity ---
	semantic := stripFileEntity(ents)
	if len(semantic) != 1 {
		t.Fatalf("#2283: expected exactly 1 Migration entity, got %d: %v", len(semantic), semantic)
	}

	// --- Kind guard ---
	got := semantic[0]
	if got.Kind != "Migration" {
		t.Errorf("kind: want Migration, got %q", got.Kind)
	}
	if got.Subtype != "django" {
		t.Errorf("subtype: want django, got %q", got.Subtype)
	}

	// --- Name = filename stem ---
	if got.Name != "0044_user_cognito_id" {
		t.Errorf("name: want 0044_user_cognito_id, got %q", got.Name)
	}

	// --- Operations preserved ---
	if got.Properties["op_count"] != "4" {
		t.Errorf("op_count: want 4, got %q", got.Properties["op_count"])
	}
	ops := got.Properties["operations"]
	for _, opType := range []string{"AddField", "RemoveField", "AlterField", "CreateModel"} {
		if !strings.Contains(ops, opType) {
			t.Errorf("operations: missing %q in %q", opType, ops)
		}
	}
	// Operation model/field metadata is present.
	if !strings.Contains(ops, "cognito_id") {
		t.Errorf("operations: expected field name cognito_id in %q", ops)
	}

	// --- Dependencies preserved ---
	deps := got.Properties["dependencies"]
	if !strings.Contains(deps, "core/0043_prev") {
		t.Errorf("dependencies: expected core/0043_prev in %q", deps)
	}
	if !strings.Contains(deps, "auth/0001_initial") {
		t.Errorf("dependencies: expected auth/0001_initial in %q", deps)
	}

	// --- File entity still present for import resolution ---
	var fileEntCount int
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "file" {
			fileEntCount++
		}
	}
	if fileEntCount != 1 {
		t.Errorf("file entity: want 1, got %d", fileEntCount)
	}
}

// TestPythonExtractor_PreservesAstSymbolName verifies that entity Name equals
// the verbatim AST identifier for every class — including single-character
// class names and classes whose name differs from the enclosing filename stem.
//
// Regression guard for issue #2552: the extractor was replacing Name with an
// inferred display label (PascalCase of the filename stem) instead of the
// literal AST symbol. A file "notes_helper.py" containing "class n:" produced
// entity Name="NoteHelper" — a fabrication that broke chain-of-reference
// lookups because no such symbol existed in the source.
//
// Contract (both cases must hold simultaneously):
//   - Name  == AST symbol verbatim ("Foo" and "b" respectively)
//   - display_name in Properties, if present, must NOT equal Name (it is an
//     auxiliary label, never a replacement)
func TestPythonExtractor_PreservesAstSymbolName(t *testing.T) {
	// Two classes in a file whose stem ("notes_helper") would produce a
	// different PascalCase label ("NotesHelper") via the display_name path.
	// "Foo" matches the expected PascalCase form but is still the verbatim
	// symbol; "b" is a deliberately short single-letter name.
	src := `class Foo:
    pass

class b:
    pass
`
	// Use a filename whose stem differs from both class names so the
	// display_name machinery is exercised.
	const filePath = "core/helper/notes_helper.py"

	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	tree := parse(t, []byte(src))
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     filePath,
		Content:  []byte(src),
		Language: "python",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	entities = stripFileEntity(entities)

	// Collect class entities by name for assertion.
	byName := make(map[string]types.EntityRecord)
	for _, e := range entities {
		if e.Kind == "SCOPE.Component" && e.Subtype == "class" {
			byName[e.Name] = e
		}
	}

	// --- "Foo": well-named class, Name must be exactly "Foo" ---
	foo, ok := byName["Foo"]
	if !ok {
		t.Fatalf("expected class entity with Name=%q; got names: %v", "Foo", classNames(byName))
	}
	if foo.Name != "Foo" {
		t.Errorf("Foo: Name=%q, want %q (AST symbol must be preserved)", foo.Name, "Foo")
	}
	// display_name, if set, must differ from Name (it is an auxiliary label).
	if dn := foo.Properties["display_name"]; dn == foo.Name {
		t.Errorf("Foo: display_name=%q must not equal Name — display_name must be an auxiliary label, not a replacement", dn)
	}

	// --- "b": single-letter class, Name must be exactly "b" ---
	b, ok := byName["b"]
	if !ok {
		t.Fatalf("expected class entity with Name=%q; got names: %v", "b", classNames(byName))
	}
	if b.Name != "b" {
		t.Errorf("b: Name=%q, want %q (single-char AST symbol must be preserved verbatim)", b.Name, "b")
	}
	// display_name, if set, must differ from Name.
	if dn := b.Properties["display_name"]; dn == b.Name {
		t.Errorf("b: display_name=%q must not equal Name", dn)
	}
}

// classNames returns the keys of the byName map as a sorted slice for
// readable test failure messages.
func classNames(m map[string]types.EntityRecord) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	return names
}
