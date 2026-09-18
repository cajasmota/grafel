package nim_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #7213 — A TYPE MEMBER CARRYING A PRAGMA PRODUCED NO ENTITY AT ALL.
//
//	type
//	  Alpha* {.packed.} = object
//	    a*: int
//
// `Alpha` was simply absent. `typeRE` admitted only a generic-parameter list
// between the name and the `=`, and a pragma sits in exactly that position, so
// the member never matched. Siblings were unaffected, so a section came up one
// member short WITHOUT failing loudly — which is why this is graded by EXACT
// SETS and forbidden rows rather than by presence assertions. A recall
// assertion that never sees the entity cannot tell "not extracted" from "not
// present in the fixture".
//
// GRAMMAR, from the Nim distribution's own doc/grammar.txt (nim-lang/Nim, file
// read at measurement time, see the PR body for the clone):
//
//	typeDef = identVisDot genericParamList? pragma? ('=' optInd typeDefValue)?
//
// The generic parameter list comes FIRST and the pragma SECOND; there is no
// production for the reverse order. That is not an inference: nim-lang/Nim ships
// tests/types/told_pragma_syntax2.nim, whose entire content is
//
//	discard """
//	  errormsg: "invalid indentation"
//	"""
//
//	type Bar {.final.} [T] = object
//
// i.e. the compiler's own suite asserts that pragma-before-generics is a
// COMPILE ERROR. The widening here therefore admits `generics? pragma?` in that
// order only, and TestPragmaMember7213_ForbiddenPragmaBeforeGenerics keeps the
// reverse order out.
//
// CORPUS MEASUREMENT (the population, not a hand-written fixture). 4431 .nim
// files across nim-lang/Nim, status-im/nimbus-eth2, treeform/pixie,
// zedeus/nitter and dom96/jester: 6141 type members matched before the fix,
// 6980 after — 839 members, 12.0% of the population, were being dropped. Top
// pragmas by frequency: pure (287), importc (157), final (127), header (89),
// inheritable (69), importcpp (61), acyclic (35). 44 of the gained sites carry
// generic parameters AND a pragma together, and exactly 1 site in 4431 files
// uses the reverse order — the compiler test above, which asserts it is
// invalid.
//
// DELIBERATELY NOT FIXED: a pragma broken across lines
// (`Timespec* {.importc: "struct timespec",\n header: "<time.h>".} = object`,
// idiomatic in Nim's posix wrappers). 361 further sites in the same population,
// 4.9%. Admitting them means letting the pragma body match a newline, and
// `[^}]*` then runs from an unterminated `{.` through any number of following
// declarations to the next `.}` — absorbing them, which is the exact failure
// TestPragmaMember7213_ForbiddenPragmaDoesNotSpanLines forbids. Left for a
// follow-up that can bound the continuation properly.
//
// AXES VARIED, crossed rather than listed:
//
//	pragma count (one / several, comma-separated / several, space-separated)
//	  x pragma arguments (none / `name: "value"` with quotes, colons, commas,
//	    angle brackets, backticks)
//	  x generic parameters (absent / present, before the pragma)
//	  x kind clause (object / ref object / enum / tuple / distinct)
//	  x declaration form (indented section member / inline `type Name ...`)
//	  x base indent (0 / 2 / 4, the last nested inside a proc)
//	  x export marker (present / absent)
//	  x inheritance (`= ref object of Base` carried alongside a pragma)
//	  x position in section (followed by a sibling / last member)
//
// HELD CONSTANT, deliberately: spaces-only indentation (Nim forbids tabs —
// manual, "Lexical Analysis -> Indentation"); ASCII identifiers; one file per
// fixture; the pragma written on a single line (the multi-line form is the
// disclosed gap above); no `{` inside a pragma body (`{.emit: "struct {}".}`
// terminates the `[^}]` run early and is not extracted — pre-existing shape of
// the same class, and NOT graded here).
//
// DERIVED-NOT-EXECUTED. No Nim toolchain exists on this machine, so every
// expected span below is derived from the fixture source plus the indentation
// rule documented on extractIndentBody (body = the lines strictly deeper than
// the declaration's own column, blank lines absorbed, the phantom element after
// a trailing newline dropped) and NEVER from what the extractor emits. Since
// every fixture's pragma is on the declaration's own line, m[1] stays on that
// line and each span equals its pragma-free twin's — asserted absolutely, not
// by comparison.
// ---------------------------------------------------------------------------

const pragPath7213 = "src/domain/pragmas.nim"

// --- admitted shapes --------------------------------------------------------

// prag7213Packed — the issue's shape verbatim, with a pragma-free sibling so
// the section can be graded as a SET rather than as one presence assertion.
const prag7213Packed = "type\n" + // 1
	"  Alpha* {.packed.} = object\n" + // 2
	"    a*: int\n" + // 3
	"  Beta* = object\n" + // 4
	"    b*: int\n" // 5

// prag7213MultiPragma — several pragmas in one block, in BOTH the
// comma-separated and the space-separated spelling Nim accepts (both occur in
// nim-lang/Nim: `{.acyclic, pure, final, shallow.}` and `{.final pure.}`).
// `Level` also drops the export marker, and the kind clause varies enum/object.
const prag7213MultiPragma = "type\n" + // 1
	"  Level {.pure, final.} = enum\n" + // 2
	"    low\n" + // 3
	"    high\n" + // 4
	"  Flag* {.inheritable final.} = object\n" + // 5
	"    f*: bool\n" // 6

// prag7213PragmaArgs — a pragma with ARGUMENTS: colons, quoted strings holding
// angle brackets and a slash, and commas between entries. The literal shape is
// taken from nim-lang/Nim's posix wrappers, collapsed onto one line.
const prag7213PragmaArgs = "type\n" + // 1
	"  Stat* {.importc: \"struct stat\", header: \"<sys/stat.h>\", final, pure.} = object\n" + // 2
	"    st_dev*: int\n" + // 3
	"    st_ino*: int\n" // 4

// prag7213GenericsThenPragma — THE INTERACTION CELL. Generic parameters and a
// pragma occupy the same position, in the one order the grammar allows. `Node`
// also varies the kind clause to `ref object`; `Leaf` is the generics-only
// control in the same section, so a fix that admits a pragma by REPLACING the
// generic group rather than following it is visible here.
const prag7213GenericsThenPragma = "type\n" + // 1
	"  Node*[T] {.acyclic, final.} = ref object\n" + // 2
	"    value*: T\n" + // 3
	"  Leaf*[T] = object\n" + // 4
	"    v*: T\n" // 5

// prag7213InlineNested — the INLINE form (`type Name ... = object` on one line)
// at base indent 2, nested inside a proc, so the declaration's own column is
// neither 0 nor the section-member 2 of the other fixtures. #7190's baseIndentLen
// is read from group 1, which the pragma must not disturb.
const prag7213InlineNested = "proc wrap*() =\n" + // 1
	"  type Cache* {.byref.} = object\n" + // 2
	"    hits*: int\n" // 3

// prag7213InheritWithPragma — a pragma on a member that ALSO inherits. The
// `of Base` clause is located from m[1], the byte just past the kind clause, so
// a pragma changing where the match starts and ends must leave it intact.
const prag7213InheritWithPragma = "type\n" + // 1
	"  Base* {.inheritable.} = object\n" + // 2
	"    id*: int\n" + // 3
	"  Derived* {.final.} = ref object of Base\n" + // 4
	"    extra*: int\n" // 5

// prag7213DistinctTuple — the two remaining kind clauses, with a backtick
// operator inside the pragma body. `Meters` has NO body at all (the next line
// is a sibling at the same column), which is the zero-body span cell.
const prag7213DistinctTuple = "type\n" + // 1
	"  Meters* {.borrow: `+`.} = distinct int\n" + // 2
	"  Pair* {.pure.} = tuple\n" + // 3
	"    x*: int\n" + // 4
	"    y*: int\n" // 5

// --- forbidden shapes -------------------------------------------------------

// prag7213BareBraces — braces that are NOT a pragma. Nim's pragma delimiters
// are the two-character tokens `{.` and `.}`; all three spellings below are
// lexical errors in Nim, and all three are needed because each grades a
// DIFFERENT half of the delimiter pair: `Bogus` has neither dot, `Half` only
// the opening one, `Odd` only the closing one. `Real` is the POSITIVE CONTROL
// inside the same fixture: the run is not vacuous, it produces exactly one
// entity.
const prag7213BareBraces = "type\n" + // 1
	"  Bogus {packed} = object\n" + // 2
	"    a*: int\n" + // 3
	"  Half {.packed} = object\n" + // 4
	"    h*: int\n" + // 5
	"  Odd {packed.} = object\n" + // 6
	"    o*: int\n" + // 7
	"  Real* = object\n" + // 8
	"    b*: int\n" // 9

// prag7213UnterminatedPragma — the ABSORPTION shape. `Bad` opens a pragma and
// never closes it on its own line. If the pragma body is allowed to match a
// newline, the run from `{.` reaches the `.}` on line 4 — three lines later —
// and the regex then reads ` = enum` after it, emitting a bogus `Bad` enum and
// consuming `Good` entirely, since FindAll matches do not overlap. `Good` is
// the positive control: it must survive, as an enum, spanning 4-6.
const prag7213UnterminatedPragma = "type\n" + // 1
	"  Bad {. = object\n" + // 2
	"    x*: int\n" + // 3
	"  Good* {.pure.} = enum\n" + // 4
	"    a\n" + // 5
	"    b\n" // 6

// prag7213PragmaBeforeGenerics — nim-lang/Nim's own tests/types/told_pragma_syntax2.nim
// asserts this form is a compile error ("invalid indentation"). `Ok` is the
// positive control: the grammatical order in the same section must still be
// extracted.
const prag7213PragmaBeforeGenerics = "type\n" + // 1
	"  Bar {.final.} [T] = object\n" + // 2
	"    v*: T\n" + // 3
	"  Ok*[T] {.final.} = object\n" + // 4
	"    w*: T\n" // 5

// prag7213PragmaOnOwnLine — the pragma on a CONTINUATION line. The grammar puts
// no `optInd` between the generic parameter list and the pragma, so a newline
// there ends the statement and this is not a type definition at all. Admitting
// it (separating generics from pragma with `\s*` instead of `[ \t]*`) would
// make `Alpha` an `enum` declared on line 2 whose span swallows line 3. `Ok` is
// the positive control.
const prag7213PragmaOnOwnLine = "type\n" + // 1
	"  Alpha*[T]\n" + // 2
	"  {.pure.} = enum\n" + // 3
	"    a\n" + // 4
	"  Ok* {.pure.} = enum\n" + // 5
	"    b\n" // 6

// prag7213PragmaNoKindClause — a pragma does not by itself make a type
// declaration: the `= <kind>` clause is still required, BOTH halves of it.
// Line 2 has the `=` and no kind; line 3 has neither; line 4 has the kind
// keyword but no `=` — grammatically `typeDef` reaches a typeDefValue only
// through `'=' optInd typeDefValue`, so a bare `Epsilon* {.packed.} object` is
// not a definition. `Gamma` is the positive control.
const prag7213PragmaNoKindClause = "type\n" + // 1
	"  Alpha* {.packed.} = 5\n" + // 2
	"  Delta* {.packed.}\n" + // 3
	"  Epsilon* {.packed.} object\n" + // 4
	"type\n" + // 5
	"  Gamma* {.packed.} = object\n" + // 6
	"    g*: int\n" // 7

// prag7213BraceInPragmaBody — THE DOCUMENTED BOUNDARY of the pragma body's
// character class. The body is `[^}\n]*`, so it stops at the first `}`; a
// pragma whose argument contains a literal `}` (`{.emit: "struct {x;}".}`) is
// therefore NOT extracted. That is a recall gap, and it is pinned here rather
// than left accidental: measured over the same 4431-file population, 0 of the
// 839 pragma-carrying type declarations contain a `}` in the pragma body, so
// the gap costs nothing today. A future change that deliberately admits `}`
// must edit THIS row and say why — it must not widen the class silently, which
// is what a `.*` body would do. `Plain` is the positive control.
const prag7213BraceInPragmaBody = "type\n" + // 1
	"  Emit* {.emit: \"struct {x;}\".} = object\n" + // 2
	"    e*: int\n" + // 3
	"  Plain* {.packed.} = object\n" + // 4
	"    p*: int\n" // 5

func prag7213Corpus() map[string]string {
	return map[string]string{
		"7213/packed":               prag7213Packed,
		"7213/multiPragma":          prag7213MultiPragma,
		"7213/pragmaArgs":           prag7213PragmaArgs,
		"7213/genericsThenPragma":   prag7213GenericsThenPragma,
		"7213/inlineNested":         prag7213InlineNested,
		"7213/inheritWithPragma":    prag7213InheritWithPragma,
		"7213/distinctTuple":        prag7213DistinctTuple,
		"7213/bareBraces":           prag7213BareBraces,
		"7213/unterminatedPragma":   prag7213UnterminatedPragma,
		"7213/pragmaBeforeGenerics": prag7213PragmaBeforeGenerics,
		"7213/pragmaOnOwnLine":      prag7213PragmaOnOwnLine,
		"7213/pragmaNoKindClause":   prag7213PragmaNoKindClause,
		"7213/braceInPragmaBody":    prag7213BraceInPragmaBody,
	}
}

// prag7213Names returns the sorted names of every entity of `kind`. Comparing
// SETS is what makes this file able to see the defect at all: a missing member
// and an over-matched one are both differences from the expected set, while a
// per-entity presence assertion can only see the first and a per-entity
// property assertion can see neither.
func prag7213Names(ents []types.EntityRecord, kind string) []string {
	var out []string
	for i := range ents {
		if ents[i].Kind == kind {
			out = append(out, ents[i].Name)
		}
	}
	sort.Strings(out)
	return out
}

func prag7213WantComponents(t *testing.T, src string, want ...string) []types.EntityRecord {
	t.Helper()
	ents := band7185Run(t, src, pragPath7213)
	got := prag7213Names(ents, "SCOPE.Component")
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("SCOPE.Component set = [%s], want [%s]", strings.Join(got, ","), strings.Join(want, ","))
	}
	return ents
}

func prag7213Want(t *testing.T, ents []types.EntityRecord, name, subtype string, start, end int) {
	t.Helper()
	e := band7185Get(t, ents, name, "SCOPE.Component")
	if e.Subtype != subtype {
		t.Errorf("%s: subtype %q, want %q", name, e.Subtype, subtype)
	}
	if e.StartLine != start || e.EndLine != end {
		t.Errorf("%s: span %d-%d, want %d-%d", name, e.StartLine, e.EndLine, start, end)
	}
}

// --- the recall rows --------------------------------------------------------

func TestPragmaMember7213_PackedMemberIsExtracted(t *testing.T) {
	ents := prag7213WantComponents(t, prag7213Packed, "Alpha", "Beta")
	prag7213Want(t, ents, "Alpha", "object", 2, 3)
	prag7213Want(t, ents, "Beta", "object", 4, 5)
}

func TestPragmaMember7213_MultiplePragmas(t *testing.T) {
	ents := prag7213WantComponents(t, prag7213MultiPragma, "Level", "Flag")
	prag7213Want(t, ents, "Level", "enum", 2, 4)
	prag7213Want(t, ents, "Flag", "object", 5, 6)
}

func TestPragmaMember7213_PragmaWithArguments(t *testing.T) {
	ents := prag7213WantComponents(t, prag7213PragmaArgs, "Stat")
	prag7213Want(t, ents, "Stat", "object", 2, 4)
}

func TestPragmaMember7213_GenericParametersAndPragmaTogether(t *testing.T) {
	ents := prag7213WantComponents(t, prag7213GenericsThenPragma, "Node", "Leaf")
	prag7213Want(t, ents, "Node", "ref object", 2, 3)
	prag7213Want(t, ents, "Leaf", "object", 4, 5)
}

func TestPragmaMember7213_InlineFormNestedInProc(t *testing.T) {
	ents := prag7213WantComponents(t, prag7213InlineNested, "Cache")
	prag7213Want(t, ents, "Cache", "object", 2, 3)
	// The enclosing proc is unaffected — a pragma inside its body must not
	// disturb procRE's own span.
	w := band7185Get(t, ents, "wrap", "SCOPE.Operation")
	if w.StartLine != 1 || w.EndLine != 3 {
		t.Errorf("wrap: span %d-%d, want 1-3", w.StartLine, w.EndLine)
	}
}

func TestPragmaMember7213_InheritanceEdgeSurvivesPragma(t *testing.T) {
	ents := prag7213WantComponents(t, prag7213InheritWithPragma, "Base", "Derived")
	prag7213Want(t, ents, "Base", "object", 2, 3)
	prag7213Want(t, ents, "Derived", "ref object", 4, 5)
	d := band7185Get(t, ents, "Derived", "SCOPE.Component")
	found := false
	for _, r := range d.Relationships {
		if r.Kind == "EXTENDS" && strings.Contains(r.ToID, "Base") {
			found = true
		}
	}
	if !found {
		t.Errorf("Derived: no EXTENDS edge to Base; got %+v", d.Relationships)
	}
}

func TestPragmaMember7213_DistinctAndTupleKinds(t *testing.T) {
	ents := prag7213WantComponents(t, prag7213DistinctTuple, "Meters", "Pair")
	prag7213Want(t, ents, "Meters", "distinct", 2, 2)
	prag7213Want(t, ents, "Pair", "tuple", 3, 5)
}

// --- the forbidden rows, each with its positive control ---------------------

// FORBIDDEN: braces that are not Nim pragma delimiters. Kills a widening to
// `\{[^}\n]*\}` (line 2) and one to `\{\.[^}\n]*\}` (line 4).
func TestPragmaMember7213_ForbiddenBareBraces(t *testing.T) {
	ents := prag7213WantComponents(t, prag7213BareBraces, "Real")
	// POSITIVE CONTROL: the surviving member proves the fixture reaches the
	// extractor and yields records, so the two absences above are enforced
	// rather than merely unreachable.
	prag7213Want(t, ents, "Real", "object", 8, 9)
}

// FORBIDDEN: the pragma body may not cross a line, because an unterminated `{.`
// then absorbs every declaration up to the next `.}`. Kills `[^}\n]` -> `[^}]`.
func TestPragmaMember7213_ForbiddenPragmaDoesNotSpanLines(t *testing.T) {
	ents := prag7213WantComponents(t, prag7213UnterminatedPragma, "Good")
	// POSITIVE CONTROL: `Good` is exactly the member a newline-permitting
	// pragma body swallows, so this assertion fires in the same mutant the set
	// comparison above does — from the other direction.
	prag7213Want(t, ents, "Good", "enum", 4, 6)
}

// FORBIDDEN: pragma before the generic parameter list. nim-lang/Nim's
// tests/types/told_pragma_syntax2.nim asserts the Nim compiler rejects it.
func TestPragmaMember7213_ForbiddenPragmaBeforeGenerics(t *testing.T) {
	ents := prag7213WantComponents(t, prag7213PragmaBeforeGenerics, "Ok")
	// POSITIVE CONTROL: the grammatical order in the very same section.
	prag7213Want(t, ents, "Ok", "object", 4, 5)
}

// FORBIDDEN: a newline between the generic parameter list and the pragma. Kills
// a separator widened from `[ \t]*` to `\s*`.
func TestPragmaMember7213_ForbiddenPragmaOnContinuationLine(t *testing.T) {
	ents := prag7213WantComponents(t, prag7213PragmaOnOwnLine, "Ok")
	prag7213Want(t, ents, "Ok", "enum", 5, 6)
}

// FORBIDDEN: a pragma does not replace the `= <kind>` clause.
func TestPragmaMember7213_ForbiddenPragmaWithoutKindClause(t *testing.T) {
	ents := prag7213WantComponents(t, prag7213PragmaNoKindClause, "Gamma")
	prag7213Want(t, ents, "Gamma", "object", 6, 7)
}

// FORBIDDEN: the pragma body stops at the first `}`. Kills a body widened from
// `[^}\n]*` to `.*`, which is the one remaining single-line widening of the
// character class.
func TestPragmaMember7213_ForbiddenBraceInsidePragmaBody(t *testing.T) {
	ents := prag7213WantComponents(t, prag7213BraceInPragmaBody, "Plain")
	// POSITIVE CONTROL: a well-formed pragma in the same section is extracted,
	// so the absence of `Emit` is enforced rather than merely unreachable.
	prag7213Want(t, ents, "Plain", "object", 4, 5)
}

// POSITIVE CONTROL FOR THE SET COMPARATOR ITSELF. An absence assertion passes
// identically whether it is enforced or simply unreachable, and no mutant of
// nim.go can tell those apart — so the comparator is exercised here on planted
// records, in both directions, on the same record shapes the rows above feed
// it.
func TestPragmaMember7213_SetComparatorFires(t *testing.T) {
	ents := band7185Run(t, prag7213Packed, pragPath7213)
	base := prag7213Names(ents, "SCOPE.Component")
	if strings.Join(base, ",") != "Alpha,Beta" {
		t.Fatalf("baseline must be [Alpha Beta] before planting; got %v", base)
	}

	// (a) an EXTRA entity — the over-matching direction — must be visible.
	extra := append(append([]types.EntityRecord(nil), ents...),
		types.EntityRecord{Name: "Bogus", Kind: "SCOPE.Component"})
	if got := prag7213Names(extra, "SCOPE.Component"); strings.Join(got, ",") == "Alpha,Beta" {
		t.Errorf("planted extra entity is invisible to the comparator: %v", got)
	}

	// (b) a MISSING entity — the recall direction, i.e. #7213 itself.
	var missing []types.EntityRecord
	for _, e := range ents {
		if e.Name != "Alpha" {
			missing = append(missing, e)
		}
	}
	if got := prag7213Names(missing, "SCOPE.Component"); strings.Join(got, ",") == "Alpha,Beta" {
		t.Errorf("planted missing entity is invisible to the comparator: %v", got)
	}

	// (c) the comparator must be quiet on the unperturbed input, on a fixture
	// the baseline above did not use.
	if got := prag7213Names(band7185Run(t, prag7213GenericsThenPragma, pragPath7213), "SCOPE.Component"); strings.Join(got, ",") != "Leaf,Node" {
		t.Errorf("unperturbed genericsThenPragma = %v, want [Leaf Node]", got)
	}
}

// COUNT FLOOR over the whole #7213 corpus. A recall defect is invisible to any
// row that only inspects entities that WERE emitted, so the corpus is also
// graded in aggregate: shrinking a fixture, or a regression that silently drops
// pragma-carrying members again, moves this number.
func TestPragmaMember7213_CorpusRecallFloor(t *testing.T) {
	corpus := prag7213Corpus()
	if len(corpus) < 13 {
		t.Fatalf("corpus floor: %d fixtures, want >= 13 — a shrunken corpus makes these rows vacuous", len(corpus))
	}
	total, withPragma := 0, 0
	for name, src := range corpus {
		ents := band7185Run(t, src, pragPath7213)
		comps := prag7213Names(ents, "SCOPE.Component")
		total += len(comps)
		for _, c := range comps {
			e := band7185Get(t, ents, c, "SCOPE.Component")
			line := strings.Split(src, "\n")[e.StartLine-1]
			if strings.Contains(line, "{.") {
				withPragma++
			}
		}
		if len(comps) == 0 {
			t.Errorf("%s: no components at all — cannot grade", name)
		}
	}
	// 18 components across the 13 fixtures — 2+2+1+2+1+2+2+1+1+1+1+1+1,
	// counted off the fixture sources in prag7213Corpus's literal order — of
	// which 15 carry a pragma on their own declaration line. The three that do
	// not are Beta (packed), Leaf (genericsThenPragma) and Real (bareBraces).
	// Derived by reading the fixtures, not from output.
	if total != 18 {
		t.Errorf("corpus emitted %d components, want 18", total)
	}
	if withPragma != 15 {
		t.Errorf("corpus emitted %d pragma-carrying components, want 15", withPragma)
	}
	t.Logf("#7213 corpus: %d components across %d fixtures, %d carrying a pragma", total, len(corpus), withPragma)
}

// --- the proc site: already correct, asserted rather than assumed -----------

// procRE has admitted a pragma since before #7213: its optional `{...}` group
// sits after the return-type annotation, which is where Nim's grammar puts a
// routine's pragma (`routine = optInd identVis pattern? genericParamList?
// paramListColon pragma? ...`). So the proc site needed NO change here, and
// this row pins that — a future narrowing of procRE would have to answer to it.
func TestPragmaMember7213_ProcSiteAlreadyAdmitsPragmas(t *testing.T) {
	src := "proc simple*(a: int) {.inline.} =\n" + // 1
		"  discard\n" + // 2
		"proc withArgs*(a: int): int {.raises: [], gcsafe.} =\n" + // 3
		"  result = a\n" + // 4
		"proc generic*[T](a: T): T {.inline.} =\n" + // 5
		"  result = a\n" // 6
	ents := band7185Run(t, src, pragPath7213)
	got := prag7213Names(ents, "SCOPE.Operation")
	if strings.Join(got, ",") != "generic,simple,withArgs" {
		t.Errorf("SCOPE.Operation set = %v, want [generic simple withArgs]", got)
	}
}
