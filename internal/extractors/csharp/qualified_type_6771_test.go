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
