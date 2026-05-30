package cpp_test

// typesystem_test.go — value-asserting tests for the deepened C/C++ type
// system extraction (enum / type / interface-analogue / concept). These
// assert specific enumerator names+values, class fields+access, base
// classes, abstract (pure-virtual) detection, and concept names — not
// merely len>0 — so the corresponding TypeSystem coverage cells can flip
// to `full` honestly.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func metaStr(r *types.EntityRecord, key string) string {
	if r == nil || r.Metadata == nil {
		return ""
	}
	s, _ := r.Metadata[key].(string)
	return s
}

func metaBool(r *types.EntityRecord, key string) bool {
	if r == nil || r.Metadata == nil {
		return false
	}
	b, _ := r.Metadata[key].(bool)
	return b
}

func metaMaps(r *types.EntityRecord, key string) []map[string]interface{} {
	if r == nil || r.Metadata == nil {
		return nil
	}
	v, _ := r.Metadata[key].([]map[string]interface{})
	return v
}

// ----------------------------------------------------------------
// enum_extraction
// ----------------------------------------------------------------

func TestEnumEnumeratorsAndValues(t *testing.T) {
	src := `enum Color { Red, Green = 5, Blue };`
	recs, err := extractCPP(src, "color.hpp")
	if err != nil {
		t.Fatal(err)
	}
	e := findByKindAndName(recs, "SCOPE.Schema", "Color")
	if e == nil {
		t.Fatalf("expected Color enum entity, got %+v", recs)
	}
	if e.Subtype != "enum" {
		t.Errorf("expected subtype enum, got %q", e.Subtype)
	}
	if metaBool(e, "scoped") {
		t.Errorf("unscoped enum must not be scoped")
	}
	ens := metaMaps(e, "enumerators")
	if len(ens) != 3 {
		t.Fatalf("expected 3 enumerators, got %d: %v", len(ens), ens)
	}
	want := []struct{ name, val string }{
		{"Red", ""}, {"Green", "5"}, {"Blue", ""},
	}
	for i, w := range want {
		if ens[i]["name"] != w.name {
			t.Errorf("enumerator %d: name = %v, want %q", i, ens[i]["name"], w.name)
		}
		got, _ := ens[i]["value"].(string)
		if got != w.val {
			t.Errorf("enumerator %d (%s): value = %q, want %q", i, w.name, got, w.val)
		}
	}
}

func TestEnumClassScopedUnderlyingType(t *testing.T) {
	src := `enum class Status : int { Active = 1, Inactive };`
	recs, err := extractCPP(src, "status.hpp")
	if err != nil {
		t.Fatal(err)
	}
	e := findByKindAndName(recs, "SCOPE.Schema", "Status")
	if e == nil {
		t.Fatalf("expected Status enum, got %+v", recs)
	}
	if !metaBool(e, "scoped") {
		t.Errorf("`enum class` must be scoped")
	}
	if ut := metaStr(e, "underlying_type"); ut != "int" {
		t.Errorf("underlying_type = %q, want int", ut)
	}
	ens := metaMaps(e, "enumerators")
	if len(ens) != 2 || ens[0]["name"] != "Active" || ens[0]["value"] != "1" {
		t.Errorf("expected Active=1 first enumerator, got %v", ens)
	}
	if ens[1]["name"] != "Inactive" {
		t.Errorf("expected Inactive second enumerator, got %v", ens)
	}
}

// ----------------------------------------------------------------
// type_extraction (class / struct / union with fields + inheritance)
// ----------------------------------------------------------------

func TestClassFieldsAndAccess(t *testing.T) {
	src := `class Widget {
public:
    int width;
private:
    float scale;
};`
	recs, err := extractCPP(src, "widget.hpp")
	if err != nil {
		t.Fatal(err)
	}
	c := findByKindAndName(recs, "SCOPE.Component", "Widget")
	if c == nil {
		t.Fatalf("expected Widget class, got %+v", recs)
	}
	fields := metaMaps(c, "fields")
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d: %v", len(fields), fields)
	}
	if fields[0]["name"] != "width" || fields[0]["access"] != "public" || fields[0]["type"] != "int" {
		t.Errorf("field[0] = %v, want width/public/int", fields[0])
	}
	if fields[1]["name"] != "scale" || fields[1]["access"] != "private" || fields[1]["type"] != "float" {
		t.Errorf("field[1] = %v, want scale/private/float", fields[1])
	}
}

func TestClassInheritance(t *testing.T) {
	src := `struct Derived : public Base, private Mixin { int z; };`
	recs, err := extractCPP(src, "derived.hpp")
	if err != nil {
		t.Fatal(err)
	}
	c := findByKindAndName(recs, "SCOPE.Component", "Derived")
	if c == nil {
		t.Fatalf("expected Derived, got %+v", recs)
	}
	bases := metaMaps(c, "bases")
	if len(bases) != 2 {
		t.Fatalf("expected 2 base classes, got %d: %v", len(bases), bases)
	}
	if bases[0]["name"] != "Base" || bases[0]["access"] != "public" {
		t.Errorf("base[0] = %v, want Base/public", bases[0])
	}
	if bases[1]["name"] != "Mixin" || bases[1]["access"] != "private" {
		t.Errorf("base[1] = %v, want Mixin/private", bases[1])
	}
}

func TestUnionFields(t *testing.T) {
	src := `union Value { int i; float f; };`
	recs, err := extractCPP(src, "value.hpp")
	if err != nil {
		t.Fatal(err)
	}
	u := findByKindAndName(recs, "SCOPE.Component", "Value")
	if u == nil {
		t.Fatalf("expected Value union, got %+v", recs)
	}
	if u.Subtype != "union" {
		t.Errorf("expected subtype union, got %q", u.Subtype)
	}
	fields := metaMaps(u, "fields")
	if len(fields) != 2 || fields[0]["name"] != "i" || fields[1]["name"] != "f" {
		t.Errorf("expected union members i,f, got %v", fields)
	}
}

// ----------------------------------------------------------------
// interface_extraction (abstract class with pure-virtual / C++20 concept)
// ----------------------------------------------------------------

func TestAbstractClassDetection(t *testing.T) {
	src := `class Shape {
public:
    virtual double area() const = 0;
    virtual void draw() = 0;
};`
	recs, err := extractCPP(src, "shape.hpp")
	if err != nil {
		t.Fatal(err)
	}
	c := findByKindAndName(recs, "SCOPE.Component", "Shape")
	if c == nil {
		t.Fatalf("expected Shape, got %+v", recs)
	}
	if !metaBool(c, "abstract") {
		t.Errorf("Shape with pure-virtual methods must be abstract; meta=%v", c.Metadata)
	}
}

func TestConcreteClassNotAbstract(t *testing.T) {
	src := `class Circle { public: double area() const { return 3.14; } };`
	recs, err := extractCPP(src, "circle.hpp")
	if err != nil {
		t.Fatal(err)
	}
	c := findByKindAndName(recs, "SCOPE.Component", "Circle")
	if c == nil {
		t.Fatalf("expected Circle, got %+v", recs)
	}
	if metaBool(c, "abstract") {
		t.Errorf("Circle has no pure-virtual methods; must not be abstract")
	}
}

func TestConceptExtraction(t *testing.T) {
	src := `template<class T> concept Addable = requires(T a) { a + a; };`
	recs, err := extractCPP(src, "concepts.hpp")
	if err != nil {
		t.Fatal(err)
	}
	c := findByKindAndName(recs, "SCOPE.Schema", "Addable")
	if c == nil {
		t.Fatalf("expected Addable concept, got %+v", recs)
	}
	if c.Subtype != "concept" {
		t.Errorf("expected subtype concept, got %q", c.Subtype)
	}
	params, _ := c.Metadata["template_params"].([]string)
	if len(params) != 1 || params[0] != "T" {
		t.Errorf("expected template param [T], got %v", params)
	}
}

// ----------------------------------------------------------------
// enum value extraction also works in C
// ----------------------------------------------------------------

func TestEnumInCFile(t *testing.T) {
	src := `enum LogLevel { DEBUG, INFO = 10, WARN };`
	recs, err := extractC(src, "log.h")
	if err != nil {
		t.Fatal(err)
	}
	e := findByKindAndName(recs, "SCOPE.Schema", "LogLevel")
	if e == nil {
		t.Fatalf("expected LogLevel enum in C, got %+v", recs)
	}
	ens := metaMaps(e, "enumerators")
	if len(ens) != 3 || ens[1]["name"] != "INFO" || ens[1]["value"] != "10" {
		t.Errorf("expected INFO=10 in C enum, got %v", ens)
	}
}
