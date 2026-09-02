// Issue #6771, non-vacuity leg — the fixed qualified_name branch has to be
// reached by a REAL extraction, not only by a unit test calling leafTypeName.
//
// leafTypeName has 15 non-test call sites and they are the load-bearing ones:
// field types, parameter types, property types, local declarations, foreach
// element types and object-creation targets all feed the receiver-type map
// that turns `_sb.Append(...)` into a CALLS edge targeting
// "<ReceiverType>.Append". While leafTypeName returned the namespace root,
// every one of those edges pointed at `System.Append` / `Microsoft.
// LogInformation` — a target that looks like a real dotted type and so was
// never rejected downstream. The cases below drive three distinct call sites
// through the extractor and assert on the emitted edges.
package csharp_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// csCallTargets returns every CALLS target hanging off the named operation.
func csCallTargets(ents []types.EntityRecord, op string) []string {
	var out []string
	for _, e := range ents {
		if e.Kind != "SCOPE.Operation" || e.Name != op {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindCalls) {
				out = append(out, r.ToID)
			}
		}
	}
	return out
}

// TestCsharpQualifiedTypes_ReceiverTypesUseTheLeaf6771 covers a qualified
// FIELD type, a qualified PARAMETER type and a qualified OBJECT-CREATION
// target in one extraction. Each receiver must resolve to the rightmost
// segment of its declared type.
func TestCsharpQualifiedTypes_ReceiverTypesUseTheLeaf6771(t *testing.T) {
	src := `
namespace App
{
    public class Svc
    {
        private System.Text.StringBuilder _sb;

        public void Run(Microsoft.Extensions.Logging.ILogger log)
        {
            _sb.Append("x");
            log.LogInformation("y");
            var made = new System.Text.StringBuilder();
            made.Clear();
        }
    }
}
`
	got := csCallTargets(csExtract(t, src, "Svc.cs"), "Svc.Run")
	for _, want := range []string{
		"StringBuilder.Append",   // field type — csharp.go collectFieldTypes
		"ILogger.LogInformation", // parameter type — collectParamTypes
		"StringBuilder.Clear",    // `new` target bound to a local
	} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing CALLS target %q; got %v", want, got)
		}
	}
	// The over-fire direction: no target may be rooted at a NAMESPACE. These
	// are precisely the values the defect produced, and no positive assertion
	// above can observe their absence.
	for _, g := range got {
		for _, ns := range []string{"System.", "Microsoft.", "Text.", "Extensions.", "Logging."} {
			if strings.HasPrefix(g, ns) {
				t.Errorf("CALLS target %q is rooted at a namespace segment, not a type leaf", g)
			}
		}
	}
}

// TestCsharpQualifiedTypes_NoPunctuationEntityOrTarget6771 is the extraction
// counterpart of the last-resort guard: with the blocklist in place a `:` was
// an admissible type name, so a punctuation token could reach an entity name
// or a relationship target. Nothing emitted from a file dense in colons —
// base list, generic constraint, ternary, label, switch case — may be, or
// contain a segment that is, a non-identifier token.
//
// HONEST LIMIT. Restoring the blocklist guard does NOT make this case fail;
// only TestLeafTypeName_ColonTokenIsNotAType dies. Every path from this source
// to a type name goes through #6742's csBaseTypeNames, whose NamedChild loop
// drops the anonymous ":" before leafTypeName ever sees it, so the guard is
// unreachable from ordinary C# today. That is a property of the CALLERS, not
// of the guard: the unit case is what grades the guard, and this case is the
// standing net for the day a caller hands leafTypeName an anonymous token.
func TestCsharpQualifiedTypes_NoPunctuationEntityOrTarget6771(t *testing.T) {
	src := `
namespace App
{
    public class Holder<T> : Base, IThing where T : IComparable
    {
        public int Pick(int n)
        {
            var v = n > 0 ? 1 : 2;
            switch (n) { case 1: break; default: break; }
        label:
            return v;
        }
    }
}
`
	bad := map[string]bool{":": true, "::": true, ",": true, ";": true, "?": true, "(": true, ")": true, "{": true, "}": true, "=>": true, ".": true, "": true}
	for _, e := range csExtract(t, src, "Holder.cs") {
		for _, seg := range strings.Split(e.Name, ".") {
			if bad[seg] {
				t.Errorf("entity name %q contains a punctuation segment %q", e.Name, seg)
			}
		}
		for _, r := range e.Relationships {
			// Targets are ids or dotted names; check every dotted segment of
			// the trailing name part.
			tail := r.ToID
			if i := strings.LastIndex(tail, ":"); i >= 0 && strings.HasPrefix(tail, "scope:") {
				tail = tail[i+1:]
			}
			for _, seg := range strings.Split(tail, ".") {
				if bad[seg] {
					t.Errorf("%s target %q contains a punctuation segment %q", r.Kind, r.ToID, seg)
				}
			}
		}
	}
}

// TestCsharpQualifiedTypes_AliasAndPointerTypes6771 pins the two shapes the
// #6771 review caught the first cut silently REGRESSING, and it pins them at
// NON-hierarchy call sites — a field, a parameter and an object-creation
// receiver — because that is where the regression lived and where nothing
// looked. csBaseTypeNames was csQualifiedLeaf's only caller; the other 14
// sites reached leafTypeName's old blocklist, whose set `" <>[]?,"` contains
// neither ":" nor "*", so both shapes were returned VERBATIM:
//
//	main                first cut    now
//	global::Foo.Alpha   Alpha        Foo.Alpha
//	global::Baz.Gamma   Gamma        Baz.Gamma   (+ construction edge restored)
//	Ns.Widget*.Delta    Delta        Widget.Delta
//
// A bare `Alpha` is not merely "less information": it joins the resolver's
// bare-name CALLS class, where it can be rewritten to any unrelated `Alpha` in
// the graph. So the first cut was a PRECISION regression as well as a recall
// loss, and neither direction was observed by any test.
func TestCsharpQualifiedTypes_AliasAndPointerTypes6771(t *testing.T) {
	src := `
namespace App
{
    public unsafe class Svc
    {
        private global::Foo _f;
        private Ns.Widget* _p;
        private global::A.B.Deep _q;
        private global::G<int> _g;
        private int** _pp;

        public void Run(global::Bar param)
        {
            _f.Alpha();
            _p->Delta();
            _q.Eps();
            _g.Zeta();
            param.Beta();
            var made = new global::Baz();
            made.Gamma();
        }
    }
}
`
	got := csCallTargets(csExtract(t, src, "Svc.cs"), "Svc.Run")
	for _, want := range []string{
		"Foo.Alpha",    // alias-qualified FIELD type
		"Bar.Beta",     // alias-qualified PARAMETER type
		"Baz.Gamma",    // alias-qualified OBJECT-CREATION target via a local
		"Baz",          // the construction edge itself, which the first cut lost
		"Widget.Delta", // pointer field over a qualified type
		"Deep.Eps",     // alias inside a qualifier — unaffected, pinned as a control
		"G.Zeta",       // alias over a generic name: type-argument list stripped
	} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing CALLS target %q; got %v", want, got)
		}
	}
	// Over-fire direction. Two distinct failures are excluded here: falling
	// back to a BARE name (the first cut), and leaking the raw alias/pointer
	// punctuation into the target (main). Neither is observable from the
	// positive assertions above.
	for _, g := range got {
		for _, bad := range []string{"Alpha", "Beta", "Gamma", "Delta", "Eps", "Zeta"} {
			if g == bad {
				t.Errorf("CALLS target %q lost its receiver type and fell into the bare-name class", g)
			}
		}
		if strings.ContainsAny(g, ":*") {
			t.Errorf("CALLS target %q leaks alias/pointer punctuation", g)
		}
		if g == "global" || strings.HasPrefix(g, "global.") {
			t.Errorf("CALLS target %q resolved to the `global` alias, not the type", g)
		}
	}
}

// TestCsharpQualifiedTypes_AliasBaseNowEmitsAnEdge6771 backs the claim in
// hierarchy.go that this ONE call site gains a supertype it never had:
// csQualifiedLeaf's blocklist contained ":", so `class C : global::Foo`
// produced no edge at all on main.
func TestCsharpQualifiedTypes_AliasBaseNowEmitsAnEdge6771(t *testing.T) {
	src := `public class C : global::Ns.BaseThing, global::IThing { }`
	got := csHierarchyEdges(csExtract(t, src, "C.cs"), "C")
	for _, want := range []string{"EXTENDS->BaseThing", "IMPLEMENTS->IThing"} {
		if !hasEdgeStr(got, want) {
			t.Errorf("missing %s; got %v", want, got)
		}
	}
}
