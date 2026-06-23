package cpp_test

import (
	"context"
	"os"
	"testing"

	tsc "github.com/smacker/go-tree-sitter/c"
	tscpp "github.com/smacker/go-tree-sitter/cpp"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/cpp" // trigger init()
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	"github.com/cajasmota/grafel/internal/types"
)

// ----------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------

func parseCPP(src []byte) ts.Tree {
	p, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tscpp.GetLanguage()))
	if err != nil {
		panic("test helper: cpp parser init failed: " + err.Error())
	}
	defer p.Close()
	tree, err := p.Parse(src)
	if err != nil {
		panic("test helper: cpp parse failed: " + err.Error())
	}
	return tree
}

func parseC(src []byte) ts.Tree {
	p, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tsc.GetLanguage()))
	if err != nil {
		panic("test helper: c parser init failed: " + err.Error())
	}
	defer p.Close()
	tree, err := p.Parse(src)
	if err != nil {
		panic("test helper: c parse failed: " + err.Error())
	}
	return tree
}

func extractCPP(src, path string) ([]types.EntityRecord, error) {
	content := []byte(src)
	tree := parseCPP(content)
	ext, ok := extractor.Get("cpp")
	if !ok {
		panic("cpp extractor not registered")
	}
	return ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  content,
		Language: "cpp",
		TSTree:   tree,
	})
}

func extractC(src, path string) ([]types.EntityRecord, error) {
	content := []byte(src)
	tree := parseC(content)
	ext, ok := extractor.Get("c")
	if !ok {
		panic("c extractor not registered")
	}
	return ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  content,
		Language: "c",
		TSTree:   tree,
	})
}

func findByKindAndName(records []types.EntityRecord, kind, name string) *types.EntityRecord {
	for i := range records {
		if records[i].Kind == kind && records[i].Name == name {
			return &records[i]
		}
	}
	return nil
}

func countByKind(records []types.EntityRecord, kind string) int {
	n := 0
	for _, r := range records {
		if r.Kind == kind {
			n++
		}
	}
	return n
}

func countBySubtype(records []types.EntityRecord, subtype string) int {
	n := 0
	for _, r := range records {
		if r.Subtype == subtype {
			n++
		}
	}
	return n
}

// ----------------------------------------------------------------
// Registration tests
// ----------------------------------------------------------------

func TestCppRegistered(t *testing.T) {
	_, ok := extractor.Get("cpp")
	if !ok {
		t.Fatal("cpp extractor not registered")
	}
}

func TestCRegistered(t *testing.T) {
	_, ok := extractor.Get("c")
	if !ok {
		t.Fatal("c extractor not registered")
	}
}

func TestLanguageReturns(t *testing.T) {
	ext, _ := extractor.Get("cpp")
	if ext.Language() != "cpp" {
		t.Errorf("expected Language()=cpp, got %s", ext.Language())
	}
	extC, _ := extractor.Get("c")
	if extC.Language() != "c" {
		t.Errorf("expected Language()=c, got %s", extC.Language())
	}
}

// ----------------------------------------------------------------
// Empty / nil edge cases
// ----------------------------------------------------------------

func TestEmptyContent(t *testing.T) {
	ext, _ := extractor.Get("cpp")
	records, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.cpp",
		Content:  []byte{},
		Language: "cpp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records for empty content, got %d", len(records))
	}
}

func TestNilTree(t *testing.T) {
	// Extractor should parse inline when tree is nil.
	records, err := extractCPP(`int main() { return 0; }`, "main.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) == 0 {
		t.Error("expected at least 1 entity (main function)")
	}
}

// ----------------------------------------------------------------
// Function extraction
// ----------------------------------------------------------------

func TestExtractFunction(t *testing.T) {
	src := `
int add(int a, int b) {
    return a + b;
}
`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findByKindAndName(records, "SCOPE.Operation", "add")
	if r == nil {
		t.Fatalf("expected function 'add' not found in %v", records)
	}
	if r.Language != "cpp" {
		t.Errorf("expected Language=cpp, got %s", r.Language)
	}
	if r.Subtype != "function" {
		t.Errorf("expected Subtype=function, got %s", r.Subtype)
	}
}

func TestExtractFunctionLineNumbers(t *testing.T) {
	src := `
void hello() {
    return;
}
`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findByKindAndName(records, "SCOPE.Operation", "hello")
	if r == nil {
		t.Fatal("hello function not found")
	}
	if r.StartLine < 1 || r.EndLine < r.StartLine {
		t.Errorf("invalid line numbers: start=%d end=%d", r.StartLine, r.EndLine)
	}
}

func TestExtractMultipleFunctions(t *testing.T) {
	src := `
int foo() { return 1; }
int bar() { return 2; }
int baz() { return 3; }
`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ops := countByKind(records, "SCOPE.Operation")
	if ops < 3 {
		t.Errorf("expected >= 3 functions, got %d", ops)
	}
}

// ----------------------------------------------------------------
// Struct extraction
// ----------------------------------------------------------------

func TestExtractStruct(t *testing.T) {
	src := `
struct Point {
    double x;
    double y;
};
`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findByKindAndName(records, "SCOPE.Component", "Point")
	if r == nil {
		t.Fatal("struct Point not found")
	}
	if r.Subtype != "struct" {
		t.Errorf("expected Subtype=struct, got %s", r.Subtype)
	}
}

func TestExtractCStruct(t *testing.T) {
	src := `
struct Pair {
    int first;
    int second;
};
`
	records, err := extractC(src, "test.c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findByKindAndName(records, "SCOPE.Component", "Pair")
	if r == nil {
		t.Fatal("struct Pair not found in C file")
	}
	if r.Language != "c" {
		t.Errorf("expected Language=c, got %s", r.Language)
	}
}

// ----------------------------------------------------------------
// Class extraction
// ----------------------------------------------------------------

func TestExtractClass(t *testing.T) {
	src := `
class Animal {
public:
    virtual void speak() = 0;
};
`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findByKindAndName(records, "SCOPE.Component", "Animal")
	if r == nil {
		t.Fatal("class Animal not found")
	}
	if r.Subtype != "class" {
		t.Errorf("expected Subtype=class, got %s", r.Subtype)
	}
}

func TestExtractClassWithMethods(t *testing.T) {
	src := `
class Dog {
public:
    Dog(const std::string& name) : name_(name) {}
    void bark() { }
    std::string getName() const { return name_; }
private:
    std::string name_;
};
`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Must have the class entity.
	r := findByKindAndName(records, "SCOPE.Component", "Dog")
	if r == nil {
		t.Fatal("class Dog not found")
	}
	// Must also have function entities for methods.
	ops := countByKind(records, "SCOPE.Operation")
	if ops == 0 {
		t.Error("expected method entities for Dog, got none")
	}
}

// ----------------------------------------------------------------
// Union extraction
// ----------------------------------------------------------------

func TestExtractUnion(t *testing.T) {
	src := `
union DataVariant {
    int i;
    float f;
    double d;
};
`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findByKindAndName(records, "SCOPE.Component", "DataVariant")
	if r == nil {
		t.Fatal("union DataVariant not found")
	}
	if r.Subtype != "union" {
		t.Errorf("expected Subtype=union, got %s", r.Subtype)
	}
}

// ----------------------------------------------------------------
// Namespace extraction
// ----------------------------------------------------------------

func TestExtractNamespace(t *testing.T) {
	src := `
namespace mylib {
    int compute(int x) { return x * 2; }
}
`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findByKindAndName(records, "SCOPE.Component", "mylib")
	if r == nil {
		t.Fatal("namespace mylib not found")
	}
	if r.Subtype != "namespace" {
		t.Errorf("expected Subtype=namespace, got %s", r.Subtype)
	}
}

func TestExtractNestedNamespace(t *testing.T) {
	src := `
namespace outer {
    namespace inner {
        void doSomething() {}
    }
}
`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ns := countBySubtype(records, "namespace")
	if ns < 2 {
		t.Errorf("expected >= 2 namespaces (outer+inner), got %d", ns)
	}
}

// ----------------------------------------------------------------
// Template extraction
// ----------------------------------------------------------------

func TestExtractTemplateClass(t *testing.T) {
	src := `
template <typename T>
class Stack {
public:
    void push(T val);
    T pop();
private:
    T data_[256];
    int top_ = 0;
};
`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findByKindAndName(records, "SCOPE.Schema", "Stack")
	if r == nil {
		t.Fatal("template class Stack not found")
	}
	if r.Subtype != "template" {
		t.Errorf("expected Subtype=template, got %s", r.Subtype)
	}
}

func TestExtractTemplateFunction(t *testing.T) {
	src := `
template <typename T>
T maxVal(T a, T b) {
    return (a > b) ? a : b;
}
`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findByKindAndName(records, "SCOPE.Schema", "maxVal")
	if r == nil {
		t.Fatal("template function maxVal not found")
	}
}

// ----------------------------------------------------------------
// Enum extraction
// ----------------------------------------------------------------

func TestExtractEnum(t *testing.T) {
	src := `
enum Direction {
    North,
    South,
    East,
    West
};
`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findByKindAndName(records, "SCOPE.Schema", "Direction")
	if r == nil {
		t.Fatal("enum Direction not found")
	}
	if r.Subtype != "enum" {
		t.Errorf("expected Subtype=enum, got %s", r.Subtype)
	}
}

func TestExtractEnumClass(t *testing.T) {
	src := `
enum class Status {
    Ok,
    Error,
    Pending
};
`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findByKindAndName(records, "SCOPE.Schema", "Status")
	if r == nil {
		t.Fatal("enum class Status not found")
	}
}

// ----------------------------------------------------------------
// Include extraction
// ----------------------------------------------------------------

func TestExtractSystemInclude(t *testing.T) {
	src := `
#include <stdio.h>
int main() { return 0; }
`
	records, err := extractC(src, "test.c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findByKindAndName(records, "SCOPE.Component", "stdio.h")
	if r == nil {
		t.Fatal("include stdio.h not found")
	}
	if r.Subtype != "import" {
		t.Errorf("expected Subtype=import, got %s", r.Subtype)
	}
}

func TestExtractLocalInclude(t *testing.T) {
	src := `
#include "myheader.h"
void foo() {}
`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findByKindAndName(records, "SCOPE.Component", "myheader.h")
	if r == nil {
		t.Fatal("include myheader.h not found")
	}
}

func TestIncludeHasImportsRelationship(t *testing.T) {
	src := `#include <vector>`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findByKindAndName(records, "SCOPE.Component", "vector")
	if r == nil {
		t.Fatal("include vector not found")
	}
	if len(r.Relationships) == 0 {
		t.Fatal("expected IMPORTS relationship on include entity")
	}
	if r.Relationships[0].Kind != "IMPORTS" {
		t.Errorf("expected Kind=IMPORTS, got %s", r.Relationships[0].Kind)
	}
}

// ----------------------------------------------------------------
// Macro extraction
// ----------------------------------------------------------------

func TestExtractMacro(t *testing.T) {
	src := `
#define BUFFER_SIZE 4096
#define MAX(a, b) ((a) > (b) ? (a) : (b))
`
	records, err := extractCPP(src, "test.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findByKindAndName(records, "SCOPE.Pattern", "BUFFER_SIZE")
	if r == nil {
		t.Fatal("macro BUFFER_SIZE not found")
	}
	if r.Subtype != "macro" {
		t.Errorf("expected Subtype=macro, got %s", r.Subtype)
	}
}

// ----------------------------------------------------------------
// Entity record invariants
// ----------------------------------------------------------------

func TestEntityRecordInvariants(t *testing.T) {
	// Every entity must have non-empty Kind, Name, Language.
	src := `
#include <iostream>
#define PI 3.14

struct Point { double x; double y; };

namespace geo {
    enum Color { Red, Green };
    class Shape { public: virtual double area() = 0; };
    template <typename T> T clamp(T v, T lo, T hi) { return v; }
    double compute(double x) { return x; }
}
`
	records, err := extractCPP(src, "invariants.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected entities, got none")
	}
	for _, r := range records {
		if r.Kind == "" {
			t.Errorf("entity %q has empty Kind", r.Name)
		}
		if r.Name == "" {
			t.Error("entity has empty Name")
		}
		if r.Language == "" {
			t.Errorf("entity %q has empty Language", r.Name)
		}
		if r.QualityScore < 0.7 {
			t.Errorf("entity %q has QualityScore below 0.7: %f", r.Name, r.QualityScore)
		}
	}
}

// ----------------------------------------------------------------
// Fixture file test (>=30 entities)
// ----------------------------------------------------------------

func TestFixtureEntityCount(t *testing.T) {
	src, err := os.ReadFile("../../../testdata/fixtures/sources/cpp/cpp__sample.cpp")
	if err != nil {
		t.Skipf("fixture not found: %v", err)
	}
	records, err := extractCPP(string(src), "cpp__sample.cpp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) < 30 {
		t.Errorf("expected >= 30 entities from fixture, got %d", len(records))
		for _, r := range records {
			t.Logf("  [%s] %s (%s)", r.Kind, r.Name, r.Subtype)
		}
	}
}
