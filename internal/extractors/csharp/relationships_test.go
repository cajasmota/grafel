// Tests for issue #368 — CSHARP IMPORTS / CALLS / CONTAINS edge emission.
package csharp_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/csharp"
	"github.com/cajasmota/grafel/internal/types"
)

func runCSharp(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("csharp")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Test.cs",
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func csFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func csHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	e := csFind(ents, name, kind)
	if e == nil {
		return false
	}
	for _, r := range e.Relationships {
		if r.Kind == edgeKind && r.ToID == toID {
			return true
		}
	}
	return false
}

// TestCSharp_ContainsClassMethods (#368): class with N methods → N CONTAINS edges
// with structural-ref Format A targets.
func TestCSharp_ContainsClassMethods(t *testing.T) {
	src := `
public class Foo
{
    public void A() {}
    public void B(int x) {}
    public void C() {}
}
`
	ents := runCSharp(t, src)
	foo := csFind(ents, "Foo", "SCOPE.Component")
	if foo == nil {
		t.Fatal("expected Foo component")
	}
	contains := 0
	for _, r := range foo.Relationships {
		if r.Kind == "CONTAINS" {
			contains++
		}
	}
	if contains != 3 {
		t.Errorf("expected 3 CONTAINS edges from Foo, got %d (rels=%+v)", contains, foo.Relationships)
	}
	for _, m := range []string{"Foo.A", "Foo.B", "Foo.C"} {
		want := "scope:operation:method:csharp:Test.cs:" + m
		if !csHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Foo→%s", want)
		}
	}
}

// TestCSharp_ContainsConstructor (#368): constructor declarations are
// containable members too.
func TestCSharp_ContainsConstructor(t *testing.T) {
	src := `
public class Foo
{
    public Foo() {}
    public void Run() {}
}
`
	ents := runCSharp(t, src)
	want := "scope:operation:method:csharp:Test.cs:Foo.Foo"
	if !csHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
		t.Errorf("expected CONTAINS Foo→%s (constructor)", want)
	}
}

// TestCSharp_CallsBareName (#368): a method calling another method emits
// a CALLS edge with the bare method name and dedupes repeated calls.
func TestCSharp_CallsBareName(t *testing.T) {
	src := `
public class A
{
    public void Caller() { Helper(); Helper(); }
    public void Helper() {}
}
`
	ents := runCSharp(t, src)
	if !csHasRel(ents, "A.Caller", "SCOPE.Operation", "CALLS", "Helper") {
		t.Errorf("expected CALLS A.Caller→Helper")
	}
	caller := csFind(ents, "A.Caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected A.Caller operation")
	}
	n := 0
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "Helper" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected dedup CALLS A.Caller→Helper to 1, got %d", n)
	}
}

// TestCSharp_CallsFieldReceiverDottedTarget (#368, mirrors Java #120):
// invocation on a field whose declared type is known emits CALLS with
// target "<FieldType>.<method>".
func TestCSharp_CallsFieldReceiverDottedTarget(t *testing.T) {
	src := `
public class OwnerRepository { public Owner FindById(int id) { return null; } }
public class OwnerController
{
    private OwnerRepository owners;
    public void Show(int id) { owners.FindById(id); }
    public void Show2(int id) { this.owners.FindById(id); }
}
`
	ents := runCSharp(t, src)
	if !csHasRel(ents, "OwnerController.Show", "SCOPE.Operation", "CALLS", "OwnerRepository.FindById") {
		e := csFind(ents, "OwnerController.Show", "SCOPE.Operation")
		var rels []types.RelationshipRecord
		if e != nil {
			rels = e.Relationships
		}
		t.Errorf("expected CALLS Show→OwnerRepository.FindById; got rels=%+v", rels)
	}
	if !csHasRel(ents, "OwnerController.Show2", "SCOPE.Operation", "CALLS", "OwnerRepository.FindById") {
		t.Errorf("expected CALLS Show2→OwnerRepository.FindById (this.owners)")
	}
}

// TestCSharp_CallsParameterReceiverDottedTarget (#368): invocation on a
// method parameter whose declared type is known emits CALLS with
// "<ParamType>.<method>".
func TestCSharp_CallsParameterReceiverDottedTarget(t *testing.T) {
	src := `
public class A
{
    public void Run(OwnerRepository repo) { repo.FindById(1); }
}
`
	ents := runCSharp(t, src)
	if !csHasRel(ents, "A.Run", "SCOPE.Operation", "CALLS", "OwnerRepository.FindById") {
		t.Errorf("expected CALLS Run→OwnerRepository.FindById from parameter receiver")
	}
}

// TestCSharp_CallsLocalVarReceiverDottedTarget (#368): a typed local
// declaration binds the variable's type so a follow-up call resolves
// to "<Type>.<method>".
func TestCSharp_CallsLocalVarReceiverDottedTarget(t *testing.T) {
	src := `
public class A
{
    public void Run() {
        OwnerRepository r = new OwnerRepository();
        r.FindById(1);
    }
}
`
	ents := runCSharp(t, src)
	if !csHasRel(ents, "A.Run", "SCOPE.Operation", "CALLS", "OwnerRepository.FindById") {
		t.Errorf("expected CALLS Run→OwnerRepository.FindById from local-var receiver")
	}
}

// TestCSharp_CallsStaticReceiverDottedTarget (#368): PascalCase bare
// receiver (likely a Type identifier — `Math.Max(...)`) is kept dotted
// so the resolver's byKind/byName can rebind cross-file.
func TestCSharp_CallsStaticReceiverDottedTarget(t *testing.T) {
	src := `
public class A
{
    public void Run() { Math.Max(1, 2); }
}
`
	ents := runCSharp(t, src)
	if !csHasRel(ents, "A.Run", "SCOPE.Operation", "CALLS", "Math.Max") {
		t.Errorf("expected CALLS Run→Math.Max (static receiver)")
	}
}

// TestCSharp_CallsObjectCreation (#368): `new Foo(...)` emits CALLS to
// the constructed type's leaf identifier.
func TestCSharp_CallsObjectCreation(t *testing.T) {
	src := `
public class A
{
    public void Run() { var x = new Owner(); }
}
`
	ents := runCSharp(t, src)
	if !csHasRel(ents, "A.Run", "SCOPE.Operation", "CALLS", "Owner") {
		e := csFind(ents, "A.Run", "SCOPE.Operation")
		var rels []types.RelationshipRecord
		if e != nil {
			rels = e.Relationships
		}
		t.Errorf("expected CALLS Run→Owner from object_creation_expression; got rels=%+v", rels)
	}
}

// TestCSharp_CallsImplicitVarNewReceiver (#4685): an implicitly-typed
// `var c = new XController(svc)` local binds `c` to XController from its
// object-creation initialiser so `c.GetCounts()` resolves to the class method
// — the xUnit/NUnit unit-test idiom. Mirrors Java #4717 newExprClassName.
func TestCSharp_CallsImplicitVarNewReceiver(t *testing.T) {
	src := `
public class XController { public void GetCounts() {} }
public class T
{
    public void Fact() {
        var c = new XController(svc);
        c.GetCounts();
    }
}
`
	ents := runCSharp(t, src)
	if !csHasRel(ents, "T.Fact", "SCOPE.Operation", "CALLS", "XController.GetCounts") {
		e := csFind(ents, "T.Fact", "SCOPE.Operation")
		var rels []types.RelationshipRecord
		if e != nil {
			rels = e.Relationships
		}
		t.Errorf("expected CALLS Fact→XController.GetCounts from `var c = new XController(...)`; got rels=%+v", rels)
	}
}

// TestCSharp_CallsTargetTypedNewReceiver (#4685): target-typed `new(...)`
// paired with an explicit declared type still types the local.
func TestCSharp_CallsTargetTypedNewReceiver(t *testing.T) {
	src := `
public class XController { public void GetCounts() {} }
public class T
{
    public void Fact() {
        XController c = new(svc);
        c.GetCounts();
    }
}
`
	ents := runCSharp(t, src)
	if !csHasRel(ents, "T.Fact", "SCOPE.Operation", "CALLS", "XController.GetCounts") {
		t.Errorf("expected CALLS Fact→XController.GetCounts from target-typed `new(...)`")
	}
}

// TestCSharp_CallsDIGetRequiredServiceReceiver (#4685): a DI service-resolution
// `var c = sp.GetRequiredService<XController>()` binds `c` to the generic type
// argument — the WebApplicationFactory/IServiceProvider idiom.
func TestCSharp_CallsDIGetRequiredServiceReceiver(t *testing.T) {
	src := `
public class XController { public void GetCounts() {} }
public class T
{
    public void Fact() {
        var c = _factory.Services.GetRequiredService<XController>();
        c.GetCounts();
    }
}
`
	ents := runCSharp(t, src)
	if !csHasRel(ents, "T.Fact", "SCOPE.Operation", "CALLS", "XController.GetCounts") {
		e := csFind(ents, "T.Fact", "SCOPE.Operation")
		var rels []types.RelationshipRecord
		if e != nil {
			rels = e.Relationships
		}
		t.Errorf("expected CALLS Fact→XController.GetCounts from GetRequiredService<T>(); got rels=%+v", rels)
	}
}

// TestCSharp_FactoryReturningInterfaceReceiverStaysBare (#4685, NEGATIVE): a
// `var svc = factory.Create()` whose RHS is a plain method call (factory /
// DI-returning-interface) must NOT fabricate a receiver type; the call resolves
// to its bare leaf.
func TestCSharp_FactoryReturningInterfaceReceiverStaysBare(t *testing.T) {
	src := `
public class T
{
    public void Fact() {
        var svc = factory.Create();
        svc.DoThing();
    }
}
`
	ents := runCSharp(t, src)
	if !csHasRel(ents, "T.Fact", "SCOPE.Operation", "CALLS", "DoThing") {
		t.Errorf("expected bare CALLS Fact→DoThing for factory-returning receiver")
	}
	// And must NOT have fabricated a dotted target off the variable name.
	if csHasRel(ents, "T.Fact", "SCOPE.Operation", "CALLS", "var.DoThing") {
		t.Errorf("must not emit fabricated CALLS Fact→var.DoThing")
	}
}

// TestCSharp_ImportsCarryProperties (#368): IMPORTS edges carry the
// metadata the cross-file resolver consumes (mirroring Java #120 /
// Python #93). For `using X.Y;` we expect:
//
//	local_name="Y", source_module="X", imported_name="Y".
func TestCSharp_ImportsCarryProperties(t *testing.T) {
	src := `
using System.Collections.Generic;
using static System.Math;
using A = System.Console;

public class Foo {}
`
	ents := runCSharp(t, src)
	want := map[string]map[string]string{
		"System.Collections.Generic": {
			"local_name":    "Generic",
			"source_module": "System.Collections",
			"imported_name": "Generic",
		},
		"System.Math": {
			"local_name":    "Math",
			"source_module": "System",
			"imported_name": "Math",
		},
		"System.Console": {
			"local_name":    "A",
			"source_module": "System",
			"imported_name": "Console",
		},
	}
	got := map[string]map[string]string{}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			got[r.ToID] = r.Properties
		}
	}
	for to, wantProps := range want {
		gotProps, ok := got[to]
		if !ok {
			t.Errorf("expected IMPORTS edge to=%q; got=%v", to, got)
			continue
		}
		for k, v := range wantProps {
			if gotProps[k] != v {
				t.Errorf("IMPORTS to=%q prop %q: got=%q want=%q (all=%v)",
					to, k, gotProps[k], v, gotProps)
			}
		}
	}
}

// TestCSharp_NoSelfRecursionEdge (#368): a method that calls itself does
// not emit a CALLS edge to its own bare leaf.
func TestCSharp_NoSelfRecursionEdge(t *testing.T) {
	src := `
public class A
{
    public int F(int n) { return F(n - 1); }
}
`
	ents := runCSharp(t, src)
	caller := csFind(ents, "A.F", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected A.F operation")
	}
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "F" {
			t.Errorf("did not expect self-recursion CALLS edge: %+v", r)
		}
	}
}

// TestCSharp_InterfaceContainsMethods (#368): interface methods are
// containable just like class methods.
func TestCSharp_InterfaceContainsMethods(t *testing.T) {
	src := `
public interface IRepo
{
    void Save();
    void Delete(int id);
}
`
	ents := runCSharp(t, src)
	want1 := "scope:operation:method:csharp:Test.cs:IRepo.Save"
	want2 := "scope:operation:method:csharp:Test.cs:IRepo.Delete"
	if !csHasRel(ents, "IRepo", "SCOPE.Component", "CONTAINS", want1) {
		t.Errorf("expected CONTAINS IRepo→%s", want1)
	}
	if !csHasRel(ents, "IRepo", "SCOPE.Component", "CONTAINS", want2) {
		t.Errorf("expected CONTAINS IRepo→%s", want2)
	}
}

// TestCSharp_PropertyReceiverDottedTarget (#368): property declarations
// (`public OwnerRepository Owners { get; set; }`) bind the property
// name to its type so a follow-up `Owners.FindById(...)` resolves to
// "OwnerRepository.FindById".
func TestCSharp_PropertyReceiverDottedTarget(t *testing.T) {
	src := `
public class OwnerController
{
    public OwnerRepository Owners { get; set; }
    public void Show(int id) { Owners.FindById(id); }
}
`
	ents := runCSharp(t, src)
	// Owners is PascalCase so even without the property-binding it would
	// fall through to the static-call shape "Owners.FindById". The
	// property-binding upgrades it to the declared-type form.
	if !csHasRel(ents, "OwnerController.Show", "SCOPE.Operation", "CALLS", "OwnerRepository.FindById") {
		e := csFind(ents, "OwnerController.Show", "SCOPE.Operation")
		var rels []types.RelationshipRecord
		if e != nil {
			rels = e.Relationships
		}
		t.Errorf("expected CALLS Show→OwnerRepository.FindById from property receiver; got rels=%+v", rels)
	}
}
