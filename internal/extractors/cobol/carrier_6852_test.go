package cobol

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #6852 — cobol's COPY IMPORTS edge is anchored on the source file's own path.
//
// buildCopyImportEdge (depth.go) stamps FromID = the using file's path. Nothing
// cobol emits is named after the path as a rule — every identifier the package
// captures comes from a regex whose character class is [A-Za-z0-9-] or a near
// neighbour, and NONE of them admits '/' — so the FROM end reached the graph as
// a raw path. extractor.PrependFileCarrier now mints the missing node, but only
// for a file that actually anchors.
//
// SITE CLASSIFICATION. The package has eight FromID assignments in non-test
// sources. Exactly ONE is path-valued:
//
//	depth.go:146      usingFile                        ← the file's own path
//	depth.go:294      fnRef  = sqlFunctionRef(path, …)
//	depth.go:366      fnRef  = sqlFunctionRef(path, …)
//	depth.go:967      fnRef  = sqlFunctionRef(path, …)
//	extractor.go:457  fromRef = sqlFunctionRef(path, …)
//	extractor.go:488  fromRef = sqlFunctionRef(path, …)
//	extractor.go:1004 sqlFunctionRef(path, fnRefName())
//	ims.go:195        fnRef  = sqlFunctionRef(path, …)
//
// The seven non-COPY sites all funnel through sqlFunctionRef, which TAKES the
// path but never IS it: both of its return expressions prefix the path with the
// non-empty literal "scope:operation:", so the result is strictly longer than
// the path and cannot equal it for any input. Pinned by
// TestCobol_OnlyTheCopyEdgeIsPathAnchored_6852.
// ---------------------------------------------------------------------------

// recordsNamed counts the records whose Name is exactly name.
func recordsNamed6852(recs []types.EntityRecord, name string) int {
	n := 0
	for i := range recs {
		if recs[i].Name == name {
			n++
		}
	}
	return n
}

// anchoredRels6852 returns every relationship in recs whose FromID is exactly
// path — the resolution requirement FileCarrierFor's clause 2 tests.
func anchoredRels6852(recs []types.EntityRecord, path string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for i := range recs {
		for j := range recs[i].Relationships {
			if recs[i].Relationships[j].FromID == path {
				out = append(out, recs[i].Relationships[j])
			}
		}
	}
	return out
}

// copyProgram6852 is a minimal, well-formed COBOL program with a PROGRAM-ID and
// one COPY — the shape that anchors. The COPY edge is attached to the program
// entity by addProgramRel, which is a no-op while programIdx < 0, so the
// PROGRAM-ID is load-bearing and not decoration.
const copyProgram6852 = `       IDENTIFICATION DIVISION.
       PROGRAM-ID. PAYROLL.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       COPY EMPREC.
       PROCEDURE DIVISION.
       MAIN-PARA.
           DISPLAY 'HI'.
           GOBACK.
`

// TestCobol_CopyImportsFromEndResolves_6852 is the two-direction resolution
// test. Both depths are driven because nothing cobol emits is named after the
// file or its basename under ANY condition — unlike terraform (basename, root
// resolved by accident) and shell (basename, only when a function is declared)
// — so both cells dangled before the fix.
func TestCobol_CopyImportsFromEndResolves_6852(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
	}{
		{"root", "payroll.cbl"},
		{"nested", "src/cobol/payroll.cbl"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recs := run(t, tc.path, copyProgram6852)

			anchored := anchoredRels6852(recs, tc.path)
			if len(anchored) == 0 {
				t.Fatalf("premise failed: no relationship is anchored on %q, so this "+
					"subtest could pass on an extraction that emits nothing", tc.path)
			}
			for _, r := range anchored {
				if r.Kind != "IMPORTS" {
					t.Fatalf("anchored edge has kind %q, want IMPORTS — the carrier is "+
						"justified by the COPY import and nothing else", r.Kind)
				}
			}

			if got := recordsNamed6852(recs, tc.path); got != 1 {
				t.Fatalf("want exactly 1 record named %q to carry the anchored "+
					"IMPORTS FROM end, got %d — a path-valued FromID resolves only "+
					"when some emitted node carries that exact string as its Name",
					tc.path, got)
			}
		})
	}
}

// TestCobol_CarrierShape_6852 pins the carrier's own fields.
//
// cobol TAGS (extractor.go runs TagRelationshipsLanguage + TagEntitiesLanguage),
// so reading Language as evidence of the token passed to PrependFileCarrier
// needs a premise: that TagEntitiesLanguage never touched the carrier. The
// carrier is prepended AFTER both tagging calls, so it is never offered to the
// filler — dockerfile's call-order escape (#6852), not shell's provenance one.
// Properties["language"] is the observable: TagEntitiesLanguage stamps that key
// ONLY on the fill path, and FileEntity does not set it. Its absence is
// therefore the premise, and it is asserted here rather than assumed — moving
// the carrier above the tagging calls turns this subtest red instead of
// silently ungrading the token.
func TestCobol_CarrierShape_6852(t *testing.T) {
	const path = "src/cobol/payroll.cbl"
	recs := run(t, path, copyProgram6852)
	if len(recs) == 0 {
		t.Fatal("no records at all")
	}
	c := recs[0]
	if c.Name != path {
		t.Fatalf("carrier is not at index 0: recs[0].Name = %q, want %q — "+
			"PrependFileCarrier puts it there per the #577 convention", c.Name, path)
	}
	if c.Kind != "SCOPE.Component" || c.Subtype != "file" {
		t.Fatalf("carrier kind/subtype = %q/%q, want SCOPE.Component/file", c.Kind, c.Subtype)
	}
	if c.Language != "cobol" {
		t.Fatalf("carrier Language = %q, want \"cobol\"", c.Language)
	}
	if _, ok := c.Properties["language"]; ok {
		t.Fatalf("carrier carries Properties[\"language\"] = %q — TagEntitiesLanguage "+
			"stamps that key only when it FILLS an empty Language, so its presence "+
			"means the carrier was prepended before the tagging calls and the "+
			"Language assertion above no longer observes the token this package "+
			"passes to PrependFileCarrier", c.Properties["language"])
	}
	if len(c.Relationships) != 0 {
		t.Fatalf("carrier owns %d relationships, want 0 — the COPY edge stays on the "+
			"program entity; re-homing it here would double the edge",
			len(c.Relationships))
	}
	if c.SourceFile != path {
		t.Fatalf("carrier SourceFile = %q, want %q", c.SourceFile, path)
	}
}

// TestCobol_NoCopyGetsNoCarrier_6852 is the over-emission control for
// FileCarrierFor's clause 2 (`!anchored`). An ordinary COBOL program with no
// COPY anchors nothing and must gain no node — an unconditional carrier would
// mint one bare orphan per source file across a whole repo, which no
// recall-shaped assertion can see.
func TestCobol_NoCopyGetsNoCarrier_6852(t *testing.T) {
	const path = "src/cobol/nocopy.cbl"
	const src = `       IDENTIFICATION DIVISION.
       PROGRAM-ID. NOCOPY.
       PROCEDURE DIVISION.
       MAIN-PARA.
           DISPLAY 'HI'.
           GOBACK.
`
	recs := run(t, path, src)
	if len(recs) < 2 {
		t.Fatalf("premise failed: only %d records extracted, so the absence below "+
			"could be an empty extraction rather than a withheld carrier", len(recs))
	}
	if got := len(anchoredRels6852(recs, path)); got != 0 {
		t.Fatalf("premise failed: %d relationships are anchored on %q in a file with "+
			"no COPY — clause 2 is not the reason this file gets no carrier", got, path)
	}
	if got := recordsNamed6852(recs, path); got != 0 {
		t.Fatalf("a file with no COPY gained %d record(s) named %q — the carrier must "+
			"be conditional", got, path)
	}
}

// TestCobol_CopyOutsideAProgramGetsNoCarrier_6852 is a SECOND clause-2 control
// reaching the same return by a different route, and it is the one that would
// be missed by reading "no COPY" as the only way not to anchor. A copybook
// (.cpy) that itself COPYs another has a COPY directive and emits the copybook
// placeholder entity for it — but addProgramRel drops the IMPORTS edge on the
// floor while programIdx < 0, so there is no anchored relationship and no
// carrier is due.
func TestCobol_CopyOutsideAProgramGetsNoCarrier_6852(t *testing.T) {
	const path = "copybooks/outer.cpy"
	const src = `      * a copybook that includes another copybook
       01  OUTER-REC.
       COPY EMPREC.
           05  OUTER-ID    PIC X(8).
`
	recs := run(t, path, src)
	if recordsNamed6852(recs, "EMPREC") != 1 {
		t.Fatalf("premise failed: the COPY directive was not parsed at all (no EMPREC "+
			"placeholder), so this file proves nothing about the anchoring clause; "+
			"records: %s", namesOf6852(recs))
	}
	if got := len(anchoredRels6852(recs, path)); got != 0 {
		t.Fatalf("premise failed: %d relationships anchored on %q — a COPY outside a "+
			"PROGRAM-ID is supposed to emit no IMPORTS edge", got, path)
	}
	if got := recordsNamed6852(recs, path); got != 0 {
		t.Fatalf("a copybook whose COPY emits no edge gained %d record(s) named %q",
			got, path)
	}
}

// TestCobol_ManyCopiesYieldExactlyOneCarrier_6852 pins the multiplicity: cobol's
// anchored site emits N edges (one per distinct copybook), and N of them must
// still produce exactly ONE carrier.
func TestCobol_ManyCopiesYieldExactlyOneCarrier_6852(t *testing.T) {
	const path = "src/cobol/many.cbl"
	const src = `       IDENTIFICATION DIVISION.
       PROGRAM-ID. MANY.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       COPY EMPREC.
       COPY TAXRULES.
       COPY DEPTREC.
       PROCEDURE DIVISION.
       MAIN-PARA.
           GOBACK.
`
	recs := run(t, path, src)
	if got := len(anchoredRels6852(recs, path)); got != 3 {
		t.Fatalf("premise failed: want 3 anchored IMPORTS (one per COPY), got %d — "+
			"the one-carrier assertion below is only interesting for N > 1", got)
	}
	if got := recordsNamed6852(recs, path); got != 1 {
		t.Fatalf("3 COPY directives yielded %d records named %q, want exactly 1 — "+
			"N anchored edges share one carrier", got, path)
	}
}

// TestCobol_EmptyPathGetsNoCarrier_6852 drives FileCarrierFor's clause 1 through
// the production Extract. An empty path makes the anchoring test degenerate —
// the COPY edge's FromID is "" too, so clause 2 would be satisfied trivially —
// and clause 1 is the only thing that rejects it. This is the third distinct
// return path.
func TestCobol_EmptyPathGetsNoCarrier_6852(t *testing.T) {
	recs := run(t, "", copyProgram6852)
	anchored := anchoredRels6852(recs, "")
	if len(anchored) == 0 {
		t.Fatal("premise failed: no relationship carries an empty FromID, so clause 1 " +
			"is not what this file exercises")
	}
	if got := recordsNamed6852(recs, ""); got != 0 {
		t.Fatalf("an empty path minted %d nameless carrier(s) — clause 1 must reject "+
			"before the trivially-satisfied anchoring test", got)
	}
}

// TestCobol_PathNamedCICSQueueGetsNoSecondCarrier_6852 drives FileCarrierFor's
// clause 3 (`records[i].Name == path`) from well-formed production COBOL.
//
// The route is a CICS temporary-storage queue name. cicsQueueRe's operand class
// is `[A-Za-z0-9$#@][A-Za-z0-9$#@_.-]*` — it admits '.' but NOT '/' — and
// extractCICSQueues stores the operand verbatim (only the dedup key is
// upper-cased). So `READQ TS QUEUE('PAYROLL.cbl')` in a ROOT file PAYROLL.cbl
// emits a SCOPE.Datastore named exactly the path. graph.EntityID does not hash
// Subtype, so a carrier beside it would put two nodes under one id (the
// #6369/#6480 hazard).
//
// The nested subtest is the contrast that stops this passing on a carrier that
// is never emitted at all. It is NOT a reachability statement about nested
// depth: the first version of this arm claimed no Name cobol emits can contain
// '/', and that closure is FALSE — see
// TestCobol_PathNamedDLISegmentGetsNoSecondCarrier_6852, which drives a
// '/'-bearing Name and the nested clause-3 rejection it produces. What is true
// of THIS route is narrower and is a property of its character class:
// cicsQueueRe/cicsMapRe stop at '.', so the queue/map route alone cannot spell
// a nested path.
//
// (selectAssignRe's SECOND group also admits '/', but that is the ASSIGN target,
// which becomes a property, never a Name.)
func TestCobol_PathNamedCICSQueueGetsNoSecondCarrier_6852(t *testing.T) {
	const body = `       IDENTIFICATION DIVISION.
       PROGRAM-ID. PAYROLL.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       COPY EMPREC.
       PROCEDURE DIVISION.
       MAIN-PARA.
           EXEC CICS READQ TS QUEUE('%s') END-EXEC.
           GOBACK.
`
	for _, tc := range []struct {
		name      string
		path      string
		queue     string
		wantNamed int
	}{
		// The queue name IS the path: clause 3 rejects, and the single record
		// named the path is the queue datastore, not a carrier.
		{"root_queue_named_like_the_path", "PAYROLL.cbl", "PAYROLL.cbl", 1},
		// At nested depth the queue name cannot spell the path (no '/'), so the
		// same source anchors and DOES get a carrier.
		{"nested_queue_cannot_spell_the_path", "src/PAYROLL.cbl", "PAYROLL.cbl", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(body, "%s", tc.queue, 1)
			recs := run(t, tc.path, src)
			if len(anchoredRels6852(recs, tc.path)) == 0 {
				t.Fatalf("premise failed: nothing is anchored on %q, so no carrier was "+
					"ever due and the count below is vacuous", tc.path)
			}
			if got := recordsNamed6852(recs, tc.path); got != tc.wantNamed {
				t.Fatalf("want exactly %d record named %q, got %d — records: %s",
					tc.wantNamed, tc.path, got, namesOf6852(recs))
			}
			if tc.name == "root_queue_named_like_the_path" {
				// Assert the single record is the pre-existing queue, not a
				// carrier that displaced it.
				for i := range recs {
					if recs[i].Name == tc.path && recs[i].Subtype != "queue" {
						t.Fatalf("the record named %q has subtype %q, want \"queue\" — "+
							"a carrier was minted beside the path-named datastore",
							tc.path, recs[i].Subtype)
					}
				}
			}
		})
	}
}

// TestCobol_OnlyTheCopyEdgeIsPathAnchored_6852 grades the site classification in
// this file's header: of the eight FromID assignments in the package, only the
// COPY one is path-valued. The seven others funnel through sqlFunctionRef, and
// a source that exercises them must produce edges whose FROM end is a
// structural ref, never the bare path.
//
// This is the sibling-premise direction: an assertion that a set never contains
// a thing is a no-op unless the set is non-empty and the thing is producible, so
// both are asserted.
func TestCobol_OnlyTheCopyEdgeIsPathAnchored_6852(t *testing.T) {
	const path = "src/cobol/depth.cbl"
	const src = `       IDENTIFICATION DIVISION.
       PROGRAM-ID. DEPTH.
       PROCEDURE DIVISION.
       MAIN-PARA.
           EXEC SQL SELECT NAME FROM EMPLOYEE END-EXEC.
           EXEC CICS READQ TS QUEUE('AUDITQ') END-EXEC.
           EXEC CICS SEND MAP('EMPMAP') END-EXEC.
           EXEC DLI GU SEGMENT(PARTROOT) END-EXEC.
           GOBACK.
`
	recs := run(t, path, src)

	kinds := map[string]int{}
	anchored := 0
	total := 0
	for i := range recs {
		for j := range recs[i].Relationships {
			r := recs[i].Relationships[j]
			total++
			kinds[r.Kind]++
			if r.FromID == path {
				anchored++
			}
		}
	}
	if total < 4 {
		t.Fatalf("premise failed: only %d relationships emitted, so \"none of them is "+
			"path-anchored\" is close to vacuous; kinds: %v", total, kinds)
	}
	// The depth passes must actually have fired, or the seven sqlFunctionRef
	// sites are simply not present in this extraction.
	for _, k := range []string{"ACCESSES_TABLE", "READS_FROM", "RENDERS"} {
		if kinds[k] == 0 {
			t.Fatalf("premise failed: no %s edge — the sqlFunctionRef sites this test "+
				"claims to cover did not fire; kinds: %v", k, kinds)
		}
	}
	if anchored != 0 {
		t.Fatalf("%d relationship(s) in a COPY-free file are anchored on %q — the "+
			"header's claim that only buildCopyImportEdge is path-valued is false",
			anchored, path)
	}
	// And no carrier: nothing anchors, so clause 2 rejects.
	if got := recordsNamed6852(recs, path); got != 0 {
		t.Fatalf("a COPY-free file gained %d record(s) named %q", got, path)
	}
}

// namesOf6852 renders record names for failure messages.
func namesOf6852(recs []types.EntityRecord) string {
	var b strings.Builder
	for i := range recs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(recs[i].Name)
	}
	return b.String()
}

// TestCobol_CarrierPlacementDoesNotShiftTheTrailingFlush_6852 grades the SECOND
// placement conjunct — the one about entity INDICES, which is independent of
// the tagging-order conjunct and is graded by nothing else.
//
// extractCOBOL keeps six indices into the entity slice (programIdx,
// currentParagraphIdx and the cicsQueueIdx / cicsMapIdx / dialectScreenIdx /
// fileEntityIdx name→index maps). Most are consumed as they are produced, but
// flushExecBlock() and flushSelect() run AFTER the scan loop, at the bottom of
// extractCOBOL, to tolerate a trailing EXEC block that never got its END-EXEC —
// and flushExecBlock reads currentParagraphIdx/programIdx to decide which
// record owns the CICS effect edge.
//
// An index-0 insertion made before those flushes shifts every one of them by
// one and re-homes the edge onto the PRECEDING record, silently and with
// nothing red — lua's #6852 hazard, in a language that has six such indices
// rather than one. The carrier is therefore prepended outside extractCOBOL
// entirely, after every index consumer has run.
//
// This fixture is production-reachable rather than contrived: mainframe source
// arrives truncated often enough that the flush exists for it, and the block
// below is exactly what the comment at that call site describes.
func TestCobol_CarrierPlacementDoesNotShiftTheTrailingFlush_6852(t *testing.T) {
	const path = "src/cobol/trunc.cbl"
	// No END-EXEC and no closing period: the READQ is flushed by the trailing
	// flushExecBlock() at the bottom of extractCOBOL.
	const src = `       IDENTIFICATION DIVISION.
       PROGRAM-ID. TRUNC.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       COPY EMPREC.
       PROCEDURE DIVISION.
       MAIN-PARA.
           EXEC CICS READQ TS QUEUE('AUDITQ')
`
	recs := run(t, path, src)

	if got := recordsNamed6852(recs, path); got != 1 {
		t.Fatalf("premise failed: %d carriers for %q — this test is about a slice that "+
			"HAS been shifted by an insertion, so it is vacuous without one", got, path)
	}

	var owner string
	found := 0
	for i := range recs {
		for j := range recs[i].Relationships {
			if recs[i].Relationships[j].Kind == "READS_FROM" {
				found++
				owner = recs[i].Name
			}
		}
	}
	if found != 1 {
		t.Fatalf("premise failed: %d READS_FROM edges, want 1 — the trailing "+
			"flushExecBlock did not fire, so no index was consumed after the scan "+
			"loop and this test grades nothing; records: %s", found, namesOf6852(recs))
	}
	if owner != "MAIN-PARA" {
		t.Fatalf("the flushed CICS queue edge is owned by %q, want \"MAIN-PARA\" — "+
			"an index-0 insertion made while currentParagraphIdx was still live "+
			"shifted the slice and re-homed the edge onto the preceding record",
			owner)
	}
}

// TestCobol_PathNamedDLISegmentGetsNoSecondCarrier_6852 is the SECOND clause-3
// route, and it exists because the first version of this arm asserted a closure
// that is FALSE: that no Name cobol emits can contain '/'.
//
// It can. buildDLISegmentEntity names its record `op + " " + segment`, and
// `segment` comes from segmentFromSSA, which upper-cases, caps the length at 8
// and requires a letter-led first byte — but does NOT reject '/'. The operand it
// slices is either dliUsingArgRe's quoted-literal branch (`['"][^'"]*['"]`) or a
// traced WORKING-STORAGE VALUE via wsValueRe, whose group 3 is `[^'"]*`. Both
// admit '/', so `'A/B.CBL'` survives as the segment name.
//
// The correct claim is the weaker one, and it is DRIVEN here rather than argued:
// such a Name carries a mandatory "<OP> " prefix, so it equals a path only when
// the path is spelled with that prefix — `EXEC A/B.CBL`, a legal on-disk path
// whose directory is "EXEC A". At that path clause 3 fires at NESTED depth,
// which the CICS-operand route (whose character class stops at '.') provably
// cannot reach.
//
// So for cobol clause 3 is reachable at BOTH depths, by two different routes.
// The earlier "root-depth only" reading was an artefact of the false closure.
func TestCobol_PathNamedDLISegmentGetsNoSecondCarrier_6852(t *testing.T) {
	const src = `       IDENTIFICATION DIVISION.
       PROGRAM-ID. PROBE.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       COPY EMPREC.
       PROCEDURE DIVISION.
       MAIN-PARA.
           CALL 'CBLTDLI' USING WS-FUNC, WS-PCB, WS-IOAREA, 'A/B.CBL'.
           GOBACK.
`
	for _, tc := range []struct {
		name        string
		path        string
		wantNamed   int
		wantCarrier bool
	}{
		// The DL/I segment record is named "EXEC A/B.CBL", which IS the path:
		// clause 3 rejects and the one record named the path is that record.
		{"nested_dli_segment_named_like_the_path", "EXEC A/B.CBL", 1, false},
		// Same source, a path the segment name cannot spell: the carrier is due.
		{"nested_segment_cannot_spell_the_path", "src/probe.cbl", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recs := run(t, tc.path, src)
			if len(anchoredRels6852(recs, tc.path)) == 0 {
				t.Fatalf("premise failed: nothing is anchored on %q, so no carrier was "+
					"ever due and the count below is vacuous", tc.path)
			}
			// The DL/I pass must have fired, or this test grades nothing — and
			// there must be exactly ONE such record, since a SECOND one is what
			// a carrier minted beside the path-named record looks like.
			switch n := recordsNamed6852(recs, "EXEC A/B.CBL"); {
			case n == 0:
				t.Fatalf("premise failed: no record named \"EXEC A/B.CBL\" — the DL/I "+
					"segment route did not fire, so the '/'-bearing Name this test is "+
					"about was never produced; records: %s", namesOf6852(recs))
			case n > 1:
				t.Fatalf("%d records named \"EXEC A/B.CBL\", want 1 — a carrier was "+
					"minted beside the path-named DL/I segment record, putting two "+
					"nodes under one entity id; records: %s", n, namesOf6852(recs))
			}
			if got := recordsNamed6852(recs, tc.path); got != tc.wantNamed {
				t.Fatalf("want exactly %d record named %q, got %d — records: %s",
					tc.wantNamed, tc.path, got, namesOf6852(recs))
			}
			gotCarrier := recs[0].Name == tc.path && recs[0].Subtype == "file"
			if gotCarrier != tc.wantCarrier {
				t.Fatalf("carrier emitted = %v, want %v — at %q the record named the "+
					"path is %q/%q", gotCarrier, tc.wantCarrier, tc.path,
					recs[0].Name, recs[0].Subtype)
			}
		})
	}
}
