// Tests for issue #6742 — C# class-hierarchy edges (EXTENDS / IMPLEMENTS).
//
// Before #6742 the C# extractor emitted a hierarchy edge ONLY when the base
// type was declared in the SAME FILE, and always as EXTENDS regardless of
// whether the base was a class or an interface. `class ReportJob : IJob`,
// `class UsersController : ControllerBase` and `class UserService :
// IUserService` therefore produced nothing at all, while Java's structurally
// identical `class SendEmailJob implements Job` produced an IMPLEMENTS edge.
//
// A colon in C# means several different things — a base list, a generic
// constraint (`where T : IFoo`), an enum's underlying type (`enum E : byte`),
// a ternary, a label. The cases below enumerate the base-list forms the
// grammar can produce AND the colon forms that must NOT produce an edge; the
// latter are the over-fire direction that recall alone can never observe.
package csharp_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// csHierarchyEdges returns every EXTENDS/IMPLEMENTS edge hanging off the
// SCOPE.Component entity named owner, as "KIND->target" strings.
func csHierarchyEdges(ents []types.EntityRecord, owner string) []string {
	var out []string
	for _, e := range ents {
		if e.Name != owner || e.Kind != "SCOPE.Component" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "EXTENDS" || r.Kind == "IMPLEMENTS" {
				out = append(out, r.Kind+"->"+r.ToID)
			}
		}
	}
	return out
}

// allCsHierarchyEdges returns every EXTENDS/IMPLEMENTS edge in the record set,
// whatever entity carries it. Used by the negative cases: an over-firing
// predicate that hung a bogus edge off some OTHER entity would slip past an
// owner-scoped assertion.
func allCsHierarchyEdges(ents []types.EntityRecord) []string {
	var out []string
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "EXTENDS" || r.Kind == "IMPLEMENTS" {
				out = append(out, e.Name+" "+r.Kind+"->"+r.ToID)
			}
		}
	}
	return out
}

func hasEdgeStr(got []string, want string) bool {
	for _, g := range got {
		if g == want {
			return true
		}
	}
	return false
}

// TestCsharpHierarchy_ExternalInterfaceImplements is the headline #6742 case:
// the exact shape from csharp-quartz-net-mini / csharp-hangfire-mini, where
// the interface is NOT declared in this file.
func TestCsharpHierarchy_ExternalInterfaceImplements(t *testing.T) {
	src := `
namespace Workers.Jobs
{
    public class ReportJob : IJob
    {
        public async Task Execute(IJobExecutionContext context) { }
    }
}
`
	got := csHierarchyEdges(csExtract(t, src, "jobs/ReportJob.cs"), "ReportJob")
	if !hasEdgeStr(got, "IMPLEMENTS->IJob") {
		t.Fatalf("class ReportJob : IJob must emit IMPLEMENTS->IJob, got %v", got)
	}
}

// TestCsharpHierarchy_ExternalBaseClassExtends — `class UsersController :
// ControllerBase`, the csharp-aspnet-core-mini shape. ControllerBase is a
// framework class, not an interface, so the kind must be EXTENDS.
func TestCsharpHierarchy_ExternalBaseClassExtends(t *testing.T) {
	src := `
namespace Api
{
    public class UsersController : ControllerBase
    {
        public void Get() { }
    }
}
`
	got := csHierarchyEdges(csExtract(t, src, "Controllers/UsersController.cs"), "UsersController")
	if !hasEdgeStr(got, "EXTENDS->ControllerBase") {
		t.Fatalf("class UsersController : ControllerBase must emit EXTENDS->ControllerBase, got %v", got)
	}
	if hasEdgeStr(got, "IMPLEMENTS->ControllerBase") {
		t.Fatalf("ControllerBase is a class, not an interface: %v", got)
	}
}

// TestCsharpHierarchy_BaseClassThenInterfaces — `class C : B, IA` splits into
// one EXTENDS and one IMPLEMENTS. C# permits at most one base class and it
// must come first, so every entry after the first is an interface by language
// rule, not by naming convention.
func TestCsharpHierarchy_BaseClassThenInterfaces(t *testing.T) {
	src := `
namespace App
{
    public class BaseEntity { }
    public class Account : BaseEntity, IAudited, ISoftDelete { }
}
`
	got := csHierarchyEdges(csExtract(t, src, "Models/Account.cs"), "Account")
	for _, want := range []string{"EXTENDS->BaseEntity", "IMPLEMENTS->IAudited", "IMPLEMENTS->ISoftDelete"} {
		if !hasEdgeStr(got, want) {
			t.Errorf("missing %s, got %v", want, got)
		}
	}
	if hasEdgeStr(got, "EXTENDS->IAudited") || hasEdgeStr(got, "EXTENDS->ISoftDelete") {
		t.Errorf("non-first base-list entries are interfaces by C# language rule: %v", got)
	}
}

// TestCsharpHierarchy_InterfacesOnly — `class C : IA, IB`, no base class.
func TestCsharpHierarchy_InterfacesOnly(t *testing.T) {
	src := `public class C : IA, IB { }`
	got := csHierarchyEdges(csExtract(t, src, "C.cs"), "C")
	for _, want := range []string{"IMPLEMENTS->IA", "IMPLEMENTS->IB"} {
		if !hasEdgeStr(got, want) {
			t.Errorf("missing %s, got %v", want, got)
		}
	}
}

// TestCsharpHierarchy_InFileInterfaceIsImplementsNotExtends — the in-file
// case the pre-#6742 code got WRONG: it emitted EXTENDS for an in-file
// interface because it never asked what the base was declared as.
func TestCsharpHierarchy_InFileInterfaceIsImplementsNotExtends(t *testing.T) {
	src := `
namespace App
{
    public interface IUserService { Task Get(); }
    public class UserService : IUserService { public Task Get() { return null; } }
}
`
	got := csHierarchyEdges(csExtract(t, src, "Services/UserService.cs"), "UserService")
	if !hasEdgeStr(got, "IMPLEMENTS->IUserService") {
		t.Fatalf("an in-file interface base must be IMPLEMENTS, got %v", got)
	}
	if hasEdgeStr(got, "EXTENDS->IUserService") {
		t.Fatalf("an in-file interface base must NOT be EXTENDS, got %v", got)
	}
}

// TestCsharpHierarchy_InFileBaseClassStaysExtends pins the #4854 behaviour the
// dashboard shape walker depends on: an in-file base CLASS keeps EXTENDS.
func TestCsharpHierarchy_InFileBaseClassStaysExtends(t *testing.T) {
	src := `
namespace App
{
    public class BaseEntity { public int Id { get; set; } }
    public class Account : BaseEntity { public string Owner { get; set; } }
}
`
	got := csHierarchyEdges(csExtract(t, src, "Models/Account.cs"), "Account")
	if !hasEdgeStr(got, "EXTENDS->BaseEntity") {
		t.Fatalf("an in-file base class must stay EXTENDS, got %v", got)
	}
}

// TestCsharpHierarchy_GenericBaseStripsTypeArguments — `class C : Base<T>`
// targets the bare `Base`, mirroring Java's `extends List<Owner>` → "List".
func TestCsharpHierarchy_GenericBaseStripsTypeArguments(t *testing.T) {
	src := `public class Repo : BaseRepository<User> { }`
	got := csHierarchyEdges(csExtract(t, src, "Repo.cs"), "Repo")
	if !hasEdgeStr(got, "EXTENDS->BaseRepository") {
		t.Fatalf("generic base must strip its type arguments, got %v", got)
	}
}

// TestCsharpHierarchy_GenericConstraintIsNotABaseList is THE over-fire guard.
// `where T : IFoo` is a generic constraint, not a base list. A predicate
// widened to "any identifier after a colon" would emit IMPLEMENTS->IFoo here
// and recall would not notice, because nothing expected went missing.
func TestCsharpHierarchy_GenericConstraintIsNotABaseList(t *testing.T) {
	src := `
namespace App
{
    public class Handler<T> where T : IFoo, new()
    {
        public void Run() { }
    }
}
`
	got := allCsHierarchyEdges(csExtract(t, src, "Handler.cs"))
	if len(got) != 0 {
		t.Fatalf("a generic constraint is not a base list; expected zero hierarchy edges, got %v", got)
	}
}

// TestCsharpHierarchy_BaseListAndConstraintTogether — the discriminating case:
// `class G<T> : Base<T> where T : IFoo` has BOTH. Exactly one edge must come
// out, to Base. This is the case a widened match crosses.
func TestCsharpHierarchy_BaseListAndConstraintTogether(t *testing.T) {
	src := `
namespace App
{
    public class G<T> : BaseHandler<T> where T : IFoo, IBar
    {
        public void Run() { }
    }
}
`
	got := allCsHierarchyEdges(csExtract(t, src, "G.cs"))
	if len(got) != 1 || got[0] != "G EXTENDS->BaseHandler" {
		t.Fatalf("expected exactly [G EXTENDS->BaseHandler], got %v", got)
	}
}

// TestCsharpHierarchy_EnumUnderlyingTypeIsNotABase — `enum E : byte` parses as
// a base_list holding a predefined_type. It is the enum's storage type, not a
// supertype, and must produce nothing.
//
// NON-VACUITY. This case asserts an ABSENCE, so it is only worth anything if
// the code under test actually runs. It did not, at first: `enum_declaration`
// was a branch of walk() that never stashed a base list, so the enum never
// reached csBaseTypeNames and this test would have passed no matter what the
// allow-list said — a reviewer's "accept predefined_type" mutant survived it.
// The positive control below is the fix: it fails if the enum stopped being
// parsed into an entity at all, which is the way an absence assertion goes
// quietly vacuous. The mutant now dies here and on csharp-hangfire-mini's
// `JobPriority EXTENDS byte` forbidden row.
func TestCsharpHierarchy_EnumUnderlyingTypeIsNotABase(t *testing.T) {
	src := `
namespace App
{
    public enum Status : byte { Active, Closed }
}
`
	ents := csExtract(t, src, "Status.cs")

	// Positive control: the declaration this test is about must be in the
	// record set, or the assertion below is grading an empty extraction.
	var sawEnum bool
	for _, e := range ents {
		if e.Name == "Status" && e.Kind == "SCOPE.Enum" {
			sawEnum = true
			break
		}
	}
	if !sawEnum {
		t.Fatalf("positive control failed: no SCOPE.Enum entity named Status was " +
			"extracted, so the absence assertion below grades nothing")
	}

	if got := allCsHierarchyEdges(ents); len(got) != 0 {
		t.Fatalf("an enum's underlying type is not a supertype, got %v", got)
	}
}

// TestCsharpHierarchy_StatementColonsAreNotBases — a ternary and a switch-case
// label both put an identifier next to a colon inside a method body.
func TestCsharpHierarchy_StatementColonsAreNotBases(t *testing.T) {
	src := `
namespace App
{
    public class Calc
    {
        public int Pick(int n)
        {
            var v = n > 0 ? Alpha : Beta;
            switch (n)
            {
                case 1: return 1;
                default: return v;
            }
        }
    }
}
`
	got := allCsHierarchyEdges(csExtract(t, src, "Calc.cs"))
	if len(got) != 0 {
		t.Fatalf("ternary / case-label colons are not base lists, got %v", got)
	}
}

// TestCsharpHierarchy_Record — `record R(int X) : Rb(X)` wraps the base in a
// primary_constructor_base_type node, a shape the plain identifier walk misses.
func TestCsharpHierarchy_Record(t *testing.T) {
	src := `
namespace App
{
    public record AuditedDto(int Id, string Name) : BaseDto(Id);
}
`
	got := csHierarchyEdges(csExtract(t, src, "AuditedDto.cs"), "AuditedDto")
	if !hasEdgeStr(got, "EXTENDS->BaseDto") {
		t.Fatalf("record positional base must emit EXTENDS->BaseDto, got %v", got)
	}
	if hasEdgeStr(got, "EXTENDS->Id") || hasEdgeStr(got, "IMPLEMENTS->Id") {
		t.Fatalf("the base constructor ARGUMENT is not a supertype, got %v", got)
	}
}

// TestCsharpHierarchy_PrimaryConstructorBaseArgs — `class P(int x) : B(x)`
// puts a sibling argument_list in the base_list. Only B is a supertype.
func TestCsharpHierarchy_PrimaryConstructorBaseArgs(t *testing.T) {
	src := `
namespace App
{
    public class Child(int x) : Parent(x) { }
}
`
	got := allCsHierarchyEdges(csExtract(t, src, "Child.cs"))
	if len(got) != 1 || got[0] != "Child EXTENDS->Parent" {
		t.Fatalf("expected exactly [Child EXTENDS->Parent], got %v", got)
	}
}

// TestCsharpHierarchy_Struct — a struct cannot have a base class in C#, so
// every base-list entry is an interface no matter how it is spelled.
func TestCsharpHierarchy_Struct(t *testing.T) {
	src := `
namespace App
{
    public struct Money : IComparable, Equatable { }
}
`
	got := csHierarchyEdges(csExtract(t, src, "Money.cs"), "Money")
	for _, want := range []string{"IMPLEMENTS->IComparable", "IMPLEMENTS->Equatable"} {
		if !hasEdgeStr(got, want) {
			t.Errorf("a struct base-list entry is always an interface, missing %s in %v", want, got)
		}
	}
}

// TestCsharpHierarchy_InterfaceExtendsInterface — `interface I : IBase`.
func TestCsharpHierarchy_InterfaceExtendsInterface(t *testing.T) {
	src := `
namespace App
{
    public interface IReadWrite : IReadOnly, IWriteOnly { }
}
`
	got := csHierarchyEdges(csExtract(t, src, "IReadWrite.cs"), "IReadWrite")
	for _, want := range []string{"EXTENDS->IReadOnly", "EXTENDS->IWriteOnly"} {
		if !hasEdgeStr(got, want) {
			t.Errorf("interface inheritance is EXTENDS, missing %s in %v", want, got)
		}
	}
}

// TestCsharpHierarchy_MultiLineBaseList — a base list split across lines is
// the same tree, and must behave identically to the one-line form.
func TestCsharpHierarchy_MultiLineBaseList(t *testing.T) {
	src := `
namespace App
{
    public class Wide :
        BaseThing,
        IAlpha,
        IBeta
    {
    }
}
`
	got := csHierarchyEdges(csExtract(t, src, "Wide.cs"), "Wide")
	for _, want := range []string{"EXTENDS->BaseThing", "IMPLEMENTS->IAlpha", "IMPLEMENTS->IBeta"} {
		if !hasEdgeStr(got, want) {
			t.Errorf("missing %s, got %v", want, got)
		}
	}
}

// TestCsharpHierarchy_NoPunctuationTargets — leafTypeName's last-resort branch
// returns raw text when it "looks like an identifier", and ":" passes that
// filter. Nothing may be emitted whose target is punctuation.
func TestCsharpHierarchy_NoPunctuationTargets(t *testing.T) {
	src := `
namespace App
{
    public class A : B, IC { }
}
`
	for _, e := range allCsHierarchyEdges(csExtract(t, src, "A.cs")) {
		for _, bad := range []string{"->:", "->,", "->(", "->)"} {
			if len(e) >= len(bad) && e[len(e)-len(bad):] == bad {
				t.Errorf("punctuation leaked into a hierarchy target: %q", e)
			}
		}
	}
}

// TestCsharpHierarchy_QualifiedBaseUsesLeafName — `class C : Microsoft.
// AspNetCore.Mvc.ControllerBase` must target the leaf, matching the bare-name
// convention every other C# edge target uses.
func TestCsharpHierarchy_QualifiedBaseUsesLeafName(t *testing.T) {
	src := `public class C : Microsoft.AspNetCore.Mvc.ControllerBase { }`
	got := csHierarchyEdges(csExtract(t, src, "C.cs"), "C")
	if !hasEdgeStr(got, "EXTENDS->ControllerBase") {
		t.Fatalf("a qualified base must target its leaf name, got %v", got)
	}
}

// TestCsharpHierarchy_NoBaseListNoEdges — a plain class emits nothing.
func TestCsharpHierarchy_NoBaseListNoEdges(t *testing.T) {
	src := `
namespace App
{
    public class Plain { public int Id { get; set; } }
}
`
	got := allCsHierarchyEdges(csExtract(t, src, "Plain.cs"))
	if len(got) != 0 {
		t.Fatalf("a class with no base list must emit no hierarchy edge, got %v", got)
	}
}
