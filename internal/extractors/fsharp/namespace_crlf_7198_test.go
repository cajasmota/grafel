package fsharp_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #7198 — `namespaceRE`'s CRLF BEHAVIOUR WAS EXERCISED BY AN EXISTING TEST AND
// GRADED BY NOTHING.
//
// `namespaceRE` (extractor.go, anchor to the NAME not a line number — the
// declaration moves) reads:
//
//	(?m)^([ \t]*)namespace\s+([\w.]+)\s*$
//
// Go's `\s` includes `\r`, and in `(?m)` mode `$` matches before the `\n`
// only — never before the `\r` of a CRLF pair. So the trailing `\s*` is the
// only thing that absorbs that `\r`, and narrowing it to `[ \t]*$` makes the
// anchor fail on every CRLF line: EVERY namespace in EVERY CRLF file stops
// being extracted. That is the same exposure `moduleRE` carries, and the
// comment above `moduleRE` states it for that pattern.
//
// THE GAP THIS FILE CLOSES. #7192 added the package's first CRLF fixture,
// TestLocalModule7151_CRLFSourceMintsBothForms, and graded `moduleRE`'s
// `\s*$` properly. Its fixture is
//
//	src := "namespace N\r\n\r\n" + tc.decl + "\r\n    let x = 1\r\n"
//
// so `namespaceRE` ALREADY RAN ON CRLF INPUT on every row of that test — but
// that test collects modules only (fs7151Modules) and asserts exclusively on
// module entities. Nothing anywhere asserted the namespace. MEASURED: with
// `namespaceRE`'s `\s*$` narrowed to `[ \t]*$` and this file held out, the
// whole package stays GREEN. With this file present, that mutant is DEAD.
// Executed-but-ungraded, not untested — which is why the fix here is
// assertions rather than new fixtures.
//
// SPELLING, following #7192's precedent deliberately: these fixtures use the
// ESCAPE `\r\n` inside an interpreted string literal, NOT a literal CR byte in
// this file. A literal CR is invisible in review; the escape is explicit at
// the call site.
//
// NOT COVERED HERE, stated so the omission is not read as an assertion:
//   - A bare-`\r` (classic-Mac) line ending. It is not CRLF, and `(?m)$` does
//     not treat a lone `\r` as a line boundary at all, so a CR-only file is a
//     different question from this one and gets no row.
//   - A trailing COMMENT after the declaration (`namespace Foo // why`), in
//     either line ending. That does not match today and this file takes no
//     position on whether it should.
//   - The INDENTATION capture `([ \t]*)` is not graded here in any direction;
//     every fixture below is at column 0. (Its value is never read for a
//     namespace record, so this is permissive dead weight rather than a
//     correctness hole.)
//   - The other `regexp.MustCompile` patterns in this package are still
//     unaudited for CRLF sensitivity (#7198's remaining half). A `$` anchor is
//     a filter, not the population.
//
// LEGALITY — DERIVED, NOT EXECUTED. No F# toolchain exists in this
// environment, so no fixture below was compiled. CRLF source is trivially
// valid F#; `namespace Foo Bar` (the forbidden row) is not a namespace
// declaration in any line ending.

// fs7198Namespaces returns every namespace SCOPE.Component, in emission order.
func fs7198Namespaces(ents []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Subtype == "namespace" {
			out = append(out, ents[i])
		}
	}
	return out
}

func fs7198NsNames(ns []types.EntityRecord) []string {
	out := []string{}
	for _, e := range ns {
		out = append(out, e.Name)
	}
	return out
}

// TestNamespace7198_CRLFSourceMintsNamespace grades the trailing `\s*$` of
// `namespaceRE` on CRLF input: the artefact — Name, Subtype, Signature,
// StartLine — and that the absorbed `\r` is not CAPTURED into either string. A
// namespace minted as `"N\r"` passes a presence check and is still wrong.
func TestNamespace7198_CRLFSourceMintsNamespace(t *testing.T) {
	for _, tc := range []struct {
		label     string
		src       string
		name      string
		startLine int
	}{
		{
			label:     "CRLF, bare name",
			src:       "namespace N\r\n\r\nlet x = 1\r\n",
			name:      "N",
			startLine: 1,
		},
		{
			label:     "CRLF, dotted name",
			src:       "namespace Foo.Bar\r\n\r\nlet x = 1\r\n",
			name:      "Foo.Bar",
			startLine: 1,
		},
		{
			label:     "CRLF, declaration below a comment header",
			src:       "// header\r\n\r\nnamespace Foo.Bar\r\n\r\nlet x = 1\r\n",
			name:      "Foo.Bar",
			startLine: 3,
		},
		{
			// FILE TERMINATION, and this is the member that actually crosses
			// that axis: the namespace is the LAST line and the file has NO
			// terminator at all, so `$` must match at end-of-text rather than
			// before a `\n`. A row whose namespace sits on line 1 with the
			// missing newline three lines below does NOT cross it — its local
			// context is byte-identical to the bare row, and it was measured
			// identical under every pattern variant probed. Note this row
			// SURVIVES the `[ \t]*$` narrowing (that anchor still matches at
			// EOF), so it grades the end-of-text anchor and NOT the `\r`
			// absorption; the rows above are the ones that grade the latter.
			label:     "CRLF, namespace is the last line with no terminator",
			src:       "// header\r\nnamespace N",
			name:      "N",
			startLine: 2,
		},
		{
			// ATTRIBUTION CONTROL. The byte-identical declaration with LF
			// endings. If this row fails together with the CRLF rows the
			// defect is in namespace extraction generally; if the CRLF rows
			// fail ALONE it is the `\r` absorption specifically.
			label:     "LF control, dotted name",
			src:       "namespace Foo.Bar\n\nlet x = 1\n",
			name:      "Foo.Bar",
			startLine: 1,
		},
	} {
		t.Run(tc.label, func(t *testing.T) {
			ns := fs7198Namespaces(runFSharp(t, tc.src, "Crlf.fs"))
			if len(ns) != 1 {
				t.Fatalf("%s produced %d namespace entities, want 1: %v — narrowing `namespaceRE`'s "+
					"trailing `\\s*$` to `[ \\t]*$` drops every namespace in every CRLF file",
					tc.label, len(ns), fs7198NsNames(ns))
			}
			e := ns[0]
			if e.Name != tc.name {
				t.Errorf("%s got Name %q, want %q — the `\\r` must be absorbed by the anchor, not captured",
					tc.label, e.Name, tc.name)
			}
			if want := "namespace " + tc.name; e.Signature != want {
				t.Errorf("%s got Signature %q, want %q", tc.label, e.Signature, want)
			}
			if e.Subtype != "namespace" {
				t.Errorf("%s got Subtype %q, want \"namespace\"", tc.label, e.Subtype)
			}
			if e.StartLine != tc.startLine {
				t.Errorf("%s got StartLine %d, want %d", tc.label, e.StartLine, tc.startLine)
			}
			// DOMINATED, kept because #7198 asked for it and it mirrors the
			// module rows — NOT an independent guard. `name` comes from
			// `([\w.]+)` and `\w` cannot match `\r`, and `Signature` is
			// derived from `Name`, so this cannot fire unless the Name
			// equality above fires on the same row. MEASURED with a leaky-name
			// mutant `([\w.]+)` -> `(.+)`: the two lines fired together on
			// every row, never alone.
			if strings.ContainsAny(e.Name+e.Signature, "\r") {
				t.Errorf("%s leaked a CR byte into Name %q / Signature %q", tc.label, e.Name, e.Signature)
			}
		})
	}
}

// TestNamespace7198_CRLFTrailingTokensMintNothing is the FORBIDDEN row, and it
// covers the PERMISSIVE direction — the one a careless CRLF "fix" reaches for.
// Widening the anchor (dropping the `$`, or replacing `\s*$` with `\s*`) makes
// `namespace Foo Bar` mint `Foo`: the anchor is what confines a namespace
// declaration to a line carrying nothing else.
//
// POSITIVE CONTROL, run by hand rather than asserted here, because an absence
// assertion passes identically whether it is ENFORCED or merely UNREACHABLE
// and no mutant can tell those apart. With `namespaceRE`'s `\s*$` replaced by
// `\s*` in the worktree, this test FAILS with
//
//	`namespace Foo Bar` produced [Foo], want none
//
// while every must-have row of TestNamespace7198_CRLFSourceMintsNamespace
// still PASSES — so the row fires, and it fires ALONE.
func TestNamespace7198_CRLFTrailingTokensMintNothing(t *testing.T) {
	src := "namespace Foo Bar\r\n\r\nlet x = 1\r\n"
	if got := fs7198NsNames(fs7198Namespaces(runFSharp(t, src, "Crlf.fs"))); len(got) != 0 {
		t.Errorf("`namespace Foo Bar` produced %v, want none — the trailing anchor must absorb the "+
			"`\\r` and NOTHING ELSE; a namespace declaration owns its whole line", got)
	}
}

// TestNamespace7198_KeywordBoundaryMintsNothing is the second FORBIDDEN row,
// and it grades the LEADING boundary — the separator between the keyword and
// the name — which the trailing-anchor rows above leave completely open.
//
// `namespaceRE` spells that separator `namespace\s+([\w.]+)`. The `+` is what
// makes `namespace` a KEYWORD rather than a prefix: relax it to `\s*` and a
// bare identifier line `namespaceFoo` — an ordinary F# value or module name,
// not a declaration — mints the namespace `Foo`. MEASURED under that mutant,
// before this row existed: the whole package stayed GREEN, and probing the
// extractor directly showed
//
//	"namespaceFoo\n"            pristine -> []     `\s*` -> [Foo]
//	"namespaceFoo\nlet x = 1\n" pristine -> []     `\s*` -> [Foo]
//	"namespace Bar\n"           pristine -> [Bar]  `\s*` -> [Bar]   (unchanged)
//
// so the mutant is reachable, not equivalent, and nothing graded it.
//
// POSITIVE CONTROL, run by hand rather than asserted here, because an absence
// assertion passes identically whether it is ENFORCED or merely UNREACHABLE.
// With `namespace\s+` replaced by `namespace\s*` in the worktree this test
// FAILS on both rows with `produced [Foo], want none`, while every must-have
// row above still PASSES — so it fires, and it fires alone.
//
// Both line endings are carried because this is the package's CRLF file and
// the boundary is orthogonal to the terminator: the LF row is the general
// statement, the CRLF row is the one that shares its bytes with the rows
// above.
func TestNamespace7198_KeywordBoundaryMintsNothing(t *testing.T) {
	for _, tc := range []struct {
		label string
		src   string
	}{
		{"LF", "namespaceFoo\nlet x = 1\n"},
		{"CRLF", "namespaceFoo\r\nlet x = 1\r\n"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			if got := fs7198NsNames(fs7198Namespaces(runFSharp(t, tc.src, "Crlf.fs"))); len(got) != 0 {
				t.Errorf("%s `namespaceFoo` produced %v, want none — `namespace` is a KEYWORD, and the "+
					"mandatory `\\s+` after it is what stops a bare identifier line from minting a namespace",
					tc.label, got)
			}
		})
	}
}
