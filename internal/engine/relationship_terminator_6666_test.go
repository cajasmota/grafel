package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

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

// ---------------------------------------------------------------------------
// #6666 review round 2. Everything below exists because an adversarial review
// found five ALIVE mutants on the guard internals and falsified the comment
// that justified the resume rule. Each test names the defect it observes.
// ---------------------------------------------------------------------------

// midLineAnchoredYAML / midLineAnchoredNoTerminatorYAML: a `(?m)^`-anchored
// rule whose accepted match ends MID-LINE.
const midLineAnchoredYAML = `
file_conventions: []
source_patterns: []
relationship_rules:
  - pattern: "(?m)^Module (\\w+)[\\s\\S]{0,60}?New (\\w+)\\("
    source_type: Module
    target_type: View
    relationship: INSTANTIATES
    source_group: 1
    target_group: 2
    terminator: "ZZZ_NEVER_APPEARS_ZZZ"
custom_extractors: []
`

const midLineAnchoredNoTerminatorYAML = `
file_conventions: []
source_patterns: []
relationship_rules:
  - pattern: "(?m)^Module (\\w+)[\\s\\S]{0,60}?New (\\w+)\\("
    source_type: Module
    target_type: View
    relationship: INSTANTIATES
    source_group: 1
    target_group: 2
custom_extractors: []
`

// Test6666_AbsentTerminatorIsIdenticalWhenMatchEndsMidLine is the D1
// regression. The ACCEPT path used to resume at the raw match end, an
// arbitrary mid-line offset; the next iteration re-sliced content there and
// `(?m)^` then matched at a FAKE line start, emitting a match
// FindAllStringSubmatch would never produce. So declaring a terminator that
// never occurs did NOT leave behaviour unchanged — it ADDED an edge.
//
// VARIES: whether the rule declares a (never-occurring) terminator.
// HELD CONSTANT: the pattern and a fixture whose accepted match ends mid-line
// and is immediately followed, on that same line, by text that looks like the
// start of a new match.
//
// Test6666_TerminatorThatNeverOccursChangesNothing asserts the same property
// but on a fixture whose accepted match ends at a line END, which is exactly
// the axis it left open.
func Test6666_AbsentTerminatorIsIdenticalWhenMatchEndsMidLine(t *testing.T) {
	const src = "Module A\nNew F(Module B\nNew G(\n"

	with := terminatorRels(t, midLineAnchoredYAML, src)
	without := terminatorRels(t, midLineAnchoredNoTerminatorYAML, src)

	// `Module B` is not at a line start, so FindAllStringSubmatch cannot pair
	// it with anything. Stated as a literal so the control is not merely
	// "whatever the other one did".
	want := []string{"Module:A -INSTANTIATES-> View:F"}

	if len(without) != len(want) || !hasRel6666(without, want[0]) {
		t.Fatalf("baseline (no terminator) is not what FindAllStringSubmatch produces; "+
			"the fixture no longer isolates the accept-path resume: %v", without)
	}
	if len(with) != len(want) || !hasRel6666(with, want[0]) {
		t.Errorf("declaring a terminator that never occurs changed the result: %v vs %v — the "+
			"accept path is resuming mid-line, so `(?m)^` is matching a position that is not a "+
			"line start", with, without)
	}
}

// sameLineRuleYAML is deliberately NOT `^`-anchored, so two source constructs
// can sit on one line.
const sameLineRuleYAML = `
file_conventions: []
source_patterns: []
relationship_rules:
  - pattern: "BEGIN (\\w+)[\\s\\S]{0,60}?USE (\\w+)"
    source_type: Module
    target_type: View
    relationship: INSTANTIATES
    source_group: 1
    target_group: 2
    terminator: "STOP"
custom_extractors: []
`

// Test6666_TerminatorResumptionIsLineGranular observes the resume rule's
// GRANULARITY, which no test observed before: the six-line comment justifying
// it could be contradicted freely and everything stayed green.
//
// VARIES: whether the second source construct is on the SAME line as the
// rejected one, or the next line.
// HELD CONSTANT: the rule, the terminator, and the text of both constructs.
//
// Line-granularity is a real cost — the same-line candidate is LOST, not
// merely reordered — and it is asserted here rather than described.
func Test6666_TerminatorResumptionIsLineGranular(t *testing.T) {
	const good = "Module:b -INSTANTIATES-> View:w"

	// Positive control first: on separate lines the rescue works.
	rels := terminatorRels(t, sameLineRuleYAML, "BEGIN a\nSTOP\nBEGIN b USE w\n")
	if !hasRel6666(rels, good) {
		t.Fatalf("positive control failed: a next-line second source was not rescued, so the "+
			"same-line case below proves nothing: %v", rels)
	}

	// Same two constructs, now on ONE line. Resumption rounds up to the next
	// line start, so `BEGIN b` is skipped and NOTHING is emitted.
	rels = terminatorRels(t, sameLineRuleYAML, "BEGIN a STOP BEGIN b USE w\n")
	if len(rels) != 0 {
		t.Errorf("resumption is no longer line-granular (%v). If this is a deliberate change to "+
			"byte-granular resumption, the accept path must be changed with it and "+
			"Test6666_AbsentTerminatorIsIdenticalWhenMatchEndsMidLine will tell you why that is "+
			"not free; the doc comment on findRelationshipMatches must be updated either way",
			rels)
	}
}

// captureNamesContainTerminatorYAML: the terminator literal "STOP" is a
// substring of names the captures themselves can hold ("STOPPER").
const captureNamesContainTerminatorYAML = sameLineRuleYAML

// Test6666_TerminatorExcludesTheCaptureTextItself pins the exact ENDPOINTS of
// the join span. `Test6666_TerminatorTestsTheSpanBetweenCaptures` only shows
// the span is not the whole match; it cannot tell srcE from srcS, or tgtS from
// tgtE, because its capture text never contains the terminator.
//
// VARIES: which capture's own text contains the terminator literal.
// HELD CONSTANT: the rule and a join window that is clean in every case.
func Test6666_TerminatorExcludesTheCaptureTextItself(t *testing.T) {
	// The SOURCE capture's text contains "STOP". The window between the
	// captures does not. Span must start at the END of the source capture.
	rels := terminatorRels(t, captureNamesContainTerminatorYAML, "BEGIN STOPPER\nUSE widget\n")
	if !hasRel6666(rels, "Module:STOPPER -INSTANTIATES-> View:widget") {
		t.Errorf("the terminator occurs inside the SOURCE capture, not between the captures, and "+
			"the join was blocked — the span starts at the source capture's START instead of its "+
			"END and is swallowing the capture text: %v", rels)
	}

	// The TARGET capture's text contains "STOP". Span must end at the START of
	// the target capture.
	rels = terminatorRels(t, captureNamesContainTerminatorYAML, "BEGIN alpha\nUSE STOPPER\n")
	if !hasRel6666(rels, "Module:alpha -INSTANTIATES-> View:STOPPER") {
		t.Errorf("the terminator occurs inside the TARGET capture and the join was blocked — the "+
			"span ends at the target capture's END instead of its START: %v", rels)
	}

	// Control: the same literal genuinely between the captures still blocks,
	// so the two assertions above are not just "the guard never fires".
	rels = terminatorRels(t, captureNamesContainTerminatorYAML, "BEGIN alpha\nSTOP\nUSE widget\n")
	if hasRel6666(rels, "Module:alpha -INSTANTIATES-> View:widget") {
		t.Errorf("control failed: a terminator squarely between the captures did not block, so "+
			"the two cases above prove nothing: %v", rels)
	}
}

// zeroWidthRuleYAML compiles to a regex that can match the empty string.
const zeroWidthRuleYAML = `
file_conventions: []
source_patterns: []
relationship_rules:
  - pattern: "(x*)(y*)"
    source_type: Module
    target_type: View
    relationship: INSTANTIATES
    source_group: 1
    target_group: 2
    terminator: "STOP"
custom_extractors: []
`

// Test6666_TerminatorTerminatesOnZeroWidthMatches observes the anti-stall
// guard, which nothing observed before: deleting it left every test green.
// A zero-width match makes the accept path's resume point equal to the match
// start, so without the +1 the walk never advances.
//
// VARIES: nothing — this is the existence test for termination itself.
// HELD CONSTANT: a rule whose regex matches the empty string everywhere.
func Test6666_TerminatorTerminatesOnZeroWidthMatches(t *testing.T) {
	fsys := buildTestFS("vbnet", "zero", zeroWidthRuleYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}
	det := New(rules)

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		_, derr := det.Detect(context.Background(), extractor.FileInput{
			Path:     "src/Zero.vb",
			Content:  []byte("abc\nabc\n"),
			Language: "vbnet",
		})
		done <- derr
	}()

	select {
	case derr := <-done:
		if derr != nil {
			t.Fatalf("Detect: %v", derr)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Detect did not return within 20s on a regex that matches the empty string — " +
			"the terminator walk is not advancing past a zero-width match")
	}
}

// nonParticipatingGroupYAML: source_group 1 is inside an alternation and can
// fail to participate, yielding offsets of (-1, -1).
const nonParticipatingGroupYAML = `
file_conventions: []
source_patterns: []
relationship_rules:
  - pattern: "(?:BEGIN (\\w+)|OPEN (\\w+))[\\s\\S]{0,60}?USE (\\w+)"
    source_type: Module
    target_type: View
    relationship: INSTANTIATES
    source_group: 1
    target_group: 3
    terminator: "STOP"
custom_extractors: []
`

// outOfRangeGroupYAML names a capture group the pattern does not have.
const outOfRangeGroupYAML = `
file_conventions: []
source_patterns: []
relationship_rules:
  - pattern: "BEGIN (\\w+)[\\s\\S]{0,60}?USE (\\w+)"
    source_type: Module
    target_type: View
    relationship: INSTANTIATES
    source_group: 9
    target_group: 2
    terminator: "STOP"
custom_extractors: []
`

// Test6666_TerminatorSurvivesNonParticipatingAndOutOfRangeGroups makes the
// negative-offset checks in joinSpanCrosses observable. They are PANIC guards,
// not policy: the affected match is discarded downstream by extractGroup
// either way, so the only reachable consequence of removing them is a slice
// with a negative bound.
//
// VARIES: why a capture has no offsets — it did not participate, vs the rule
// file named an index that does not exist.
// HELD CONSTANT: a terminator is declared and the fixture contains it.
func Test6666_TerminatorSurvivesNonParticipatingAndOutOfRangeGroups(t *testing.T) {
	// Group 1 never participates (the OPEN branch matched).
	rels := terminatorRels(t, nonParticipatingGroupYAML, "OPEN a\nSTOP\nUSE w\n")
	if len(rels) != 0 {
		t.Errorf("a match whose source capture did not participate produced an edge: %v", rels)
	}

	// Positive control: the branch that DOES populate group 1 still works, so
	// the assertion above is about the absent group and not a dead rule.
	rels = terminatorRels(t, nonParticipatingGroupYAML, "BEGIN a\nUSE w\n")
	if !hasRel6666(rels, "Module:a -INSTANTIATES-> View:w") {
		t.Fatalf("positive control failed: the participating branch emitted nothing: %v", rels)
	}

	// An index the pattern has no group for.
	rels = terminatorRels(t, outOfRangeGroupYAML, "BEGIN a\nSTOP\nUSE w\n")
	if len(rels) != 0 {
		t.Errorf("an out-of-range source_group produced an edge: %v", rels)
	}
}

// groupZeroTerminatorYAML is the combination that must be refused at load:
// group 0 is the whole match, so the join window is empty by construction and
// the guard could never fire. Note the fixture below contains STOP between the
// two constructs — a working guard WOULD block it — so if the rule were
// loaded, the edge would appear and the guard's uselessness would be invisible.
const groupZeroTerminatorYAML = `
file_conventions: []
source_patterns: []
relationship_rules:
  - pattern: "func boot\\([\\s\\S]{0,200}?USE (\\w+)"
    source_type: Module
    target_type: View
    relationship: INSTANTIATES
    source_group: 0
    target_group: 1
    terminator: "STOP"
custom_extractors: []
`

// groupZeroNoTerminatorYAML is the same rule WITHOUT a terminator. source_group
// 0 on its own is a separate defect (#6788) and stays loadable.
const groupZeroNoTerminatorYAML = `
file_conventions: []
source_patterns: []
relationship_rules:
  - pattern: "func boot\\([\\s\\S]{0,200}?USE (\\w+)"
    source_type: Module
    target_type: View
    relationship: INSTANTIATES
    source_group: 0
    target_group: 1
custom_extractors: []
`

// Test6666_TerminatorWithGroupZeroIsRejectedAtLoad is review defect D2.
// With source_group 0 the source "capture" ends where the match ends, so the
// span between the captures is inverted and joinSpanCrosses returns false
// unconditionally — a guard that silently does nothing, with green tests.
// `internal/engine/rules/swift/frameworks/vapor.yaml` is the one rule in the
// tree with source_group 0, and it is a nominated terminator candidate.
//
// VARIES: whether the group-0 rule also declares a terminator.
// HELD CONSTANT: pattern, fixture, and the presence of STOP between the two
// constructs.
func Test6666_TerminatorWithGroupZeroIsRejectedAtLoad(t *testing.T) {
	const content = "func boot(routes)\nSTOP\nUSE widget\n"

	rels := terminatorRels(t, groupZeroTerminatorYAML, content)
	if len(rels) != 0 {
		t.Errorf("a rule combining `terminator` with capture group 0 was loaded and fired (%v). "+
			"Group 0 is the whole match, so its guard can never block anything — the rule must "+
			"be refused at load rather than shipping a guard that does nothing", rels)
	}

	// Positive control: the SAME rule without a terminator still loads, so the
	// rejection is aimed at the combination and is not a blanket ban on
	// source_group 0 (#6788 owns that separately).
	rels = terminatorRels(t, groupZeroNoTerminatorYAML, content)
	if len(rels) == 0 {
		t.Errorf("source_group 0 without a terminator no longer loads; the load-time rejection "+
			"is too broad and has taken over #6788's territory: %v", rels)
	}
}

// Test6666_TerminatorWorksWithoutATrailingNewline is review suspicion D5:
// on rejection, "no line start remains" abandons the walk. It is only
// reachable when the rejected match begins on a FINAL line with no trailing
// newline, in which case every later source is on that same line and
// line-granular resumption could not have reached it anyway.
//
// VARIES: whether the file ends with a newline.
// HELD CONSTANT: the two-module VB source.
func Test6666_TerminatorWorksWithoutATrailingNewline(t *testing.T) {
	trimmed := strings.TrimRight(helperFirstSource, "\n")
	if strings.HasSuffix(trimmed, "\n") {
		t.Fatal("fixture setup failed: the trailing newline was not removed")
	}
	rels := terminatorRels(t, terminatorRuleYAML, trimmed)
	if !hasRel6666(rels, "Module:Program -INSTANTIATES-> View:OrderEntryForm") {
		t.Errorf("a file with no trailing newline lost the rescued edge — the walk is abandoning "+
			"the search while line starts still remain: %v", rels)
	}

	// The one case where the walk really does stop early: the rejected match
	// begins on the last line and there is no newline after it. Asserted so
	// the limitation is observed, not merely described.
	rels = terminatorRels(t, sameLineRuleYAML, "BEGIN a STOP BEGIN b USE w")
	if len(rels) != 0 {
		t.Errorf("expected nothing: the rejected match begins on a final line with no trailing "+
			"newline, so no line start remains and the same-line candidate is out of reach by "+
			"the documented line-granularity limit; got %v", rels)
	}
}
