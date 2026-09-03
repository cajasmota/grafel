package engine

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
)

// ---------------------------------------------------------------------------
// #6666 — per-rule `terminator` guard on relationship_rules join windows.
//
// A relationship_rule joins two captures across a positional `[\s\S]{0,N}?`
// window. The window has no notion of scope, so when the SOURCE construct
// repeats in a file the first source pairs with the SECOND source's target:
// one false edge plus one missing edge. `terminator` names a literal string
// the span BETWEEN the two captures may not cross.
//
// Every test below uses the same two-Module VB shape so that the only axis
// that varies between them is stated in each test's own comment.
// ---------------------------------------------------------------------------

// terminatorRuleYAML is the winforms INSTANTIATES rule WITH the guard.
// It is a copy of internal/engine/rules/vbnet/frameworks/winforms.yaml's
// INSTANTIATES rule; the copy exists so these engine-level tests exercise the
// join site under a rule set of exactly one rule, with nothing else firing.
const terminatorRuleYAML = `
file_conventions: []
source_patterns: []
relationship_rules:
  - pattern: "(?m)^[ \\t]*(?:(?:Public|Friend)[ \\t]+)?Module[ \\t]+(\\w+)[\\s\\S]{0,600}?Application\\.Run\\s*\\(\\s*New[ \\t]+([\\w.]+)\\s*\\("
    source_type: Module
    target_type: View
    relationship: INSTANTIATES
    source_group: 1
    target_group: 2
    terminator: "End Module"
custom_extractors: []
`

// noTerminatorRuleYAML is byte-for-byte the same rule with the `terminator`
// key REMOVED. It is the opt-in control: it proves the guard is what changes
// the outcome, not some other edit to the join site.
const noTerminatorRuleYAML = `
file_conventions: []
source_patterns: []
relationship_rules:
  - pattern: "(?m)^[ \\t]*(?:(?:Public|Friend)[ \\t]+)?Module[ \\t]+(\\w+)[\\s\\S]{0,600}?Application\\.Run\\s*\\(\\s*New[ \\t]+([\\w.]+)\\s*\\("
    source_type: Module
    target_type: View
    relationship: INSTANTIATES
    source_group: 1
    target_group: 2
custom_extractors: []
`

// absentTerminatorRuleYAML declares a terminator that never occurs in any
// fixture below. It is the "guard that cannot fire" control.
const absentTerminatorRuleYAML = `
file_conventions: []
source_patterns: []
relationship_rules:
  - pattern: "(?m)^[ \\t]*(?:(?:Public|Friend)[ \\t]+)?Module[ \\t]+(\\w+)[\\s\\S]{0,600}?Application\\.Run\\s*\\(\\s*New[ \\t]+([\\w.]+)\\s*\\("
    source_type: Module
    target_type: View
    relationship: INSTANTIATES
    source_group: 1
    target_group: 2
    terminator: "ZZZ_NEVER_APPEARS_ZZZ"
custom_extractors: []
`

// helperFirstSource: DiagnosticsHelpers is declared FIRST and makes no call;
// Program is declared SECOND and makes the Application.Run call.
const helperFirstSource = `Imports System.Windows.Forms

Module DiagnosticsHelpers
    Public Sub Log(ByVal msg As String)
        System.Diagnostics.Debug.WriteLine(msg)
    End Sub
End Module

Module Program
    Public Sub Main()
        Application.Run(New OrderEntryForm())
    End Sub
End Module
`

// programFirstSource is helperFirstSource with the two Modules SWAPPED. The
// caller is now the FIRST module, so the correct join needs no rescue — but
// the terminator literal "End Module" still occurs in the file, after the
// target capture. This is the over-firing control: a guard that merely asks
// "does the terminator appear anywhere?" emits nothing here and passes every
// test that only checks the bug is fixed.
const programFirstSource = `Imports System.Windows.Forms

Module Program
    Public Sub Main()
        Application.Run(New OrderEntryForm())
    End Sub
End Module

Module DiagnosticsHelpers
    Public Sub Log(ByVal msg As String)
        System.Diagnostics.Debug.WriteLine(msg)
    End Sub
End Module
`

// singleModuleSource has exactly ONE Module. The source construct does not
// repeat, so the defect cannot occur — and the edge must still be emitted.
const singleModuleSource = `Imports System.Windows.Forms

Module Program
    Public Sub Main()
        Application.Run(New OrderEntryForm())
    End Sub
End Module
`

// threeModuleSource has TWO non-calling Modules before the caller. It varies
// only the NUMBER of rejected candidates; if the join site resumes at the
// wrong offset after a rejection it will find the caller once, twice, or
// never — one skipped candidate cannot tell those apart.
const threeModuleSource = `Imports System.Windows.Forms

Module DiagnosticsHelpers
    Public Sub Log(ByVal msg As String)
    End Sub
End Module

Module StringHelpers
    Public Sub Trim2(ByVal msg As String)
    End Sub
End Module

Module Program
    Public Sub Main()
        Application.Run(New OrderEntryForm())
    End Sub
End Module
`

func terminatorRels(t *testing.T, ruleYAML, content string) []string {
	t.Helper()
	fsys := buildTestFS("vbnet", "winforms", ruleYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}
	res, err := New(rules).Detect(context.Background(), extractor.FileInput{
		Path:     "src/Program.vb",
		Content:  []byte(content),
		Language: "vbnet",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	var out []string
	for _, r := range res.Relationships {
		out = append(out, r.FromID+" -"+r.Kind+"-> "+r.ToID)
	}
	return out
}

func hasRel6666(rels []string, want string) bool {
	for _, r := range rels {
		if r == want {
			return true
		}
	}
	return false
}

// Test6666_TerminatorBlocksCrossModuleJoin is the defect itself.
// VARIES: whether the rule declares `terminator: "End Module"`.
// HELD CONSTANT: the pattern, the capture groups, and the fixture.
func Test6666_TerminatorBlocksCrossModuleJoin(t *testing.T) {
	const good = "Module:Program -INSTANTIATES-> View:OrderEntryForm"
	const bad = "Module:DiagnosticsHelpers -INSTANTIATES-> View:OrderEntryForm"

	// Control: WITHOUT the terminator the engine still mis-pairs, exactly as
	// it did before this change. If this leg goes green-by-accident the guard
	// is no longer opt-in and 18 other rules changed behaviour silently.
	rels := terminatorRels(t, noTerminatorRuleYAML, helperFirstSource)
	if !hasRel6666(rels, bad) || hasRel6666(rels, good) {
		t.Errorf("rule WITHOUT terminator no longer mis-pairs — the guard is not opt-in, "+
			"so every windowed rule in the tree just changed behaviour; got %v", rels)
	}

	// The fix.
	rels = terminatorRels(t, terminatorRuleYAML, helperFirstSource)
	if hasRel6666(rels, bad) {
		t.Errorf("terminator did not block the cross-module join: %v", rels)
	}
	if !hasRel6666(rels, good) {
		t.Errorf("terminator blocked the bad join but the correct edge was not recovered — "+
			"rejecting a match must resume the search, not consume the region; got %v", rels)
	}
	if len(rels) != 1 {
		t.Errorf("expected exactly one INSTANTIATES edge, got %d: %v", len(rels), rels)
	}
}

// Test6666_TerminatorDoesNotBlockLegitimateJoin is the over-firing control.
// VARIES: the ORDER of the two Modules (caller first vs caller second).
// HELD CONSTANT: the rule, and the fact that "End Module" occurs in the file.
func Test6666_TerminatorDoesNotBlockLegitimateJoin(t *testing.T) {
	rels := terminatorRels(t, terminatorRuleYAML, programFirstSource)
	const good = "Module:Program -INSTANTIATES-> View:OrderEntryForm"
	if !hasRel6666(rels, good) {
		t.Errorf("the terminator literal occurs in this file only AFTER the target capture, so "+
			"it must not block the join; the guard is testing the whole file or the whole match "+
			"instead of the span between the captures; got %v", rels)
	}
	if len(rels) != 1 {
		t.Errorf("expected exactly one edge, got %d: %v", len(rels), rels)
	}
}

// Test6666_TerminatorLeavesNonRepeatingSourceAlone is the recall control for
// the ordinary case.
// VARIES: nothing but the fixture's module count (one).
// HELD CONSTANT: the rule (terminator declared).
func Test6666_TerminatorLeavesNonRepeatingSourceAlone(t *testing.T) {
	rels := terminatorRels(t, terminatorRuleYAML, singleModuleSource)
	const good = "Module:Program -INSTANTIATES-> View:OrderEntryForm"
	if !hasRel6666(rels, good) {
		t.Errorf("a single-Module file has no repetition and no intervening terminator, so the "+
			"edge must be emitted unchanged; got %v", rels)
	}
	if len(rels) != 1 {
		t.Errorf("expected exactly one edge, got %d: %v", len(rels), rels)
	}
}

// Test6666_TerminatorThatNeverOccursChangesNothing pins that declaring a
// terminator is not itself what alters the outcome.
// VARIES: the terminator STRING (a literal absent from the fixture).
// HELD CONSTANT: fixture and pattern.
func Test6666_TerminatorThatNeverOccursChangesNothing(t *testing.T) {
	with := terminatorRels(t, absentTerminatorRuleYAML, helperFirstSource)
	without := terminatorRels(t, noTerminatorRuleYAML, helperFirstSource)
	if len(with) != len(without) {
		t.Fatalf("a terminator absent from the file changed the edge count: %v vs %v", with, without)
	}
	for i := range with {
		if with[i] != without[i] {
			t.Errorf("a terminator absent from the file changed the edges: %v vs %v", with, without)
			break
		}
	}
}

// Test6666_TerminatorResumesPastEveryRejectedCandidate.
// VARIES: the number of non-calling Modules preceding the caller (two).
// HELD CONSTANT: the rule and the caller's own text.
func Test6666_TerminatorResumesPastEveryRejectedCandidate(t *testing.T) {
	rels := terminatorRels(t, terminatorRuleYAML, threeModuleSource)
	const good = "Module:Program -INSTANTIATES-> View:OrderEntryForm"
	if !hasRel6666(rels, good) {
		t.Errorf("with TWO rejected candidates the correct edge was lost; the join site stops "+
			"retrying after the first rejection: %v", rels)
	}
	for _, r := range rels {
		if r != good {
			t.Errorf("unexpected extra edge %q — a rejected candidate was re-matched or a "+
				"non-caller was paired: %v", r, rels)
		}
	}
}

// Test6666_TerminatorIsALiteralNotAScopeParser asserts the guard's REAL
// strength, so no comment can claim more than the code does. "End Module"
// inside a `'` comment between the two captures DOES block the join: the
// engine matches raw text and has no VB comment stripper (the same limitation
// winforms.yaml already records for its other patterns). This is a false
// NEGATIVE, and it is recorded rather than assumed away.
func Test6666_TerminatorIsALiteralNotAScopeParser(t *testing.T) {
	const commentedTerminator = `Imports System.Windows.Forms

Module Program
    Public Sub Main()
        ' End Module   <- a comment, not a real block terminator
        Application.Run(New OrderEntryForm())
    End Sub
End Module
`
	rels := terminatorRels(t, terminatorRuleYAML, commentedTerminator)
	const good = "Module:Program -INSTANTIATES-> View:OrderEntryForm"
	if hasRel6666(rels, good) {
		t.Errorf("the terminator now ignores `'` comments — it has become more than a literal "+
			"substring test, and the LIMITATION note in winforms.yaml and in schema.go's "+
			"Terminator doc is now wrong and must be tightened; got %v", rels)
	}

	// Positive control: the identical file with the comment removed DOES emit
	// the edge, so the negative above is the comment talking and not a broken
	// fixture.
	const noComment = `Imports System.Windows.Forms

Module Program
    Public Sub Main()
        Application.Run(New OrderEntryForm())
    End Sub
End Module
`
	rels = terminatorRels(t, terminatorRuleYAML, noComment)
	if !hasRel6666(rels, good) {
		t.Fatalf("positive control failed: the same file without the comment emitted no edge, "+
			"so the negative case above proves nothing; got %v", rels)
	}
}

// Test6666_TerminatorIsCaseSensitive pins the second stated limitation: the
// comparison is a byte-literal one, and VB keywords are case-insensitive, so
// `end module` does NOT block. Documented in schema.go and winforms.yaml.
func Test6666_TerminatorIsCaseSensitive(t *testing.T) {
	const lowerCase = `Imports System.Windows.Forms

Module DiagnosticsHelpers
    Public Sub Log(ByVal msg As String)
    End Sub
end module

Module Program
    Public Sub Main()
        Application.Run(New OrderEntryForm())
    End Sub
End Module
`
	rels := terminatorRels(t, terminatorRuleYAML, lowerCase)
	const bad = "Module:DiagnosticsHelpers -INSTANTIATES-> View:OrderEntryForm"
	if !hasRel6666(rels, bad) {
		t.Errorf("`end module` blocked the join, so the terminator is no longer a case-sensitive "+
			"byte literal; the doc comments on Terminator and in winforms.yaml understate it: %v",
			rels)
	}
}

// ---------------------------------------------------------------------------
// Synthetic rules that isolate WHICH span the guard tests.
//
// The winforms rule cannot distinguish "the span between the captures" from
// "the whole match", because its source capture sits at the match start and
// its target capture at the match end. A guard that tested the whole match
// would behave identically on every VB fixture above and survive. These rules
// put text on BOTH sides of the captures so the two are separable.
// ---------------------------------------------------------------------------

// spanRuleYAML matches PRE ... BEGIN <src> ... USE <tgt> ... DONE. Only the
// text between <src> and <tgt> is the join window; the PRE.. and ..DONE tails
// are inside the match but outside the window.
const spanRuleYAML = `
file_conventions: []
source_patterns: []
relationship_rules:
  - pattern: "PRE[\\s\\S]{0,80}?BEGIN (\\w+)[\\s\\S]{0,200}?USE (\\w+)[\\s\\S]{0,80}?DONE"
    source_type: Module
    target_type: View
    relationship: INSTANTIATES
    source_group: 1
    target_group: 2
    terminator: "STOP"
custom_extractors: []
`

// reversedGroupsRuleYAML has the TARGET capture textually FIRST, as the
// winforms HANDLES rule does (source_group: 2). The guard must take the span
// in the order the captures actually appear, not the order they are numbered.
const reversedGroupsRuleYAML = `
file_conventions: []
source_patterns: []
relationship_rules:
  - pattern: "USE (\\w+)[\\s\\S]{0,200}?BY (\\w+)"
    source_type: Module
    target_type: View
    relationship: INSTANTIATES
    source_group: 2
    target_group: 1
    terminator: "STOP"
custom_extractors: []
`

// Test6666_TerminatorTestsTheSpanBetweenCaptures kills the "check the whole
// match" reading of the guard.
// VARIES: WHERE the terminator sits relative to the two captures — outside
// them but inside the match, vs between them.
// HELD CONSTANT: the rule, the captures, and the fact that "STOP" occurs.
func Test6666_TerminatorTestsTheSpanBetweenCaptures(t *testing.T) {
	// STOP appears twice inside the match — once before the source capture,
	// once after the target capture — and never between them.
	const outsideSpan = `PRE
STOP
BEGIN alpha
USE widget
STOP
DONE
`
	rels := terminatorRels(t, spanRuleYAML, outsideSpan)
	const good = "Module:alpha -INSTANTIATES-> View:widget"
	if !hasRel6666(rels, good) {
		t.Errorf("STOP occurs only OUTSIDE the two captures, so the join window is clean and "+
			"the edge must be emitted; the guard is scanning the whole match (or the whole "+
			"file) instead of the span between the captures: %v", rels)
	}

	// Same rule, same literal, moved BETWEEN the captures: now it must block.
	const insideSpan = `PRE
BEGIN alpha
STOP
USE widget
DONE
`
	rels = terminatorRels(t, spanRuleYAML, insideSpan)
	if hasRel6666(rels, good) {
		t.Errorf("STOP sits BETWEEN the two captures and the join was still emitted; the guard "+
			"is not testing the join window at all: %v", rels)
	}
}

// Test6666_TerminatorHandlesReversedCaptureOrder.
// VARIES: whether the terminator sits between the two captures.
// HELD CONSTANT: a rule whose source_group is the LATER capture.
func Test6666_TerminatorHandlesReversedCaptureOrder(t *testing.T) {
	const good = "Module:beta -INSTANTIATES-> View:widget"

	rels := terminatorRels(t, reversedGroupsRuleYAML, "USE widget BY beta\n")
	if !hasRel6666(rels, good) {
		t.Fatalf("positive control failed: a clean reversed-group match emitted nothing, so the "+
			"negative below would prove nothing: %v", rels)
	}

	rels = terminatorRels(t, reversedGroupsRuleYAML, "USE widget STOP BY beta\n")
	if hasRel6666(rels, good) {
		t.Errorf("the terminator lies between the captures — target first, source second — and "+
			"the join was still emitted; the span is being taken in group-number order rather "+
			"than textual order: %v", rels)
	}
}
