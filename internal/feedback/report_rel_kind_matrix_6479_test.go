package feedback

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// #6479: nothing in the feedback report was dimensioned on (language ×
// relationship kind). EntitiesByLanguage, EntityKindDist, OrphanByKind,
// OrphanTerminalByKind, OrphanLeafByKind, Resolution and FrameworkHits are all
// keyed on language OR on kind, never on the pair — so a language that emits
// entities but no hierarchy edges at all reported PASS on every check.
//
// These tests pin the REPORT-ONLY matrix that closes that hole. There is no
// gate here by design (settled on the issue): a gate would fire on the eight
// languages that emit no EXTENDS/IMPLEMENTS today and its first act would be to
// acquire a suppression list for a third of its inputs.
//
// Every assertion below reads the RENDERED artefact — including the COUNTS,
// which are the quantitative payload of the table and are checked as their
// rendered range labels, not as map values. Two map reads appear, each as an
// explicit control standing next to a render assertion, never as the
// observation itself.

// relRow returns the rendered matrix row for lang, or "" when the language has
// no row at all. Reading the render (rather than the map) is the point: a
// matrix that is populated but suppressed on the way out is decorative.
func relRow(t *testing.T, out, lang string) string {
	t.Helper()
	// Scope to the matrix section: "| java | 6-20 |" in the Entities-by-language
	// table above would otherwise be mistaken for a matrix row.
	_, section, ok := strings.Cut(out, "### Relationship kinds by language")
	if !ok {
		t.Fatalf("rendered report has no relationship-kind matrix section at all:\n%s", out)
	}
	if head, _, ok := strings.Cut(section, "\n### "); ok {
		section = head
	}
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "| "+lang+" |") {
			return line
		}
	}
	return ""
}

// relEmittedCol returns the second ("relationship kinds emitted") column of a
// rendered matrix row, so a count assertion is made against the rendered range
// label rather than against the map the code keeps about itself.
func relEmittedCol(t *testing.T, row string) string {
	t.Helper()
	cols := strings.Split(strings.Trim(row, "|"), "|")
	if len(cols) != 3 {
		t.Fatalf("matrix row does not have 3 columns: %q", row)
	}
	return strings.TrimSpace(cols[1])
}

// relMissingCol returns the third ("kinds peers emit and this one does not")
// column of a rendered matrix row, so an assertion about a MISSING kind cannot
// be satisfied by the same kind appearing in the emitted column.
func relMissingCol(t *testing.T, row string) string {
	t.Helper()
	cols := strings.Split(strings.Trim(row, "|"), "|")
	if len(cols) != 3 {
		t.Fatalf("matrix row does not have 3 columns: %q", row)
	}
	return strings.TrimSpace(cols[2])
}

// TestRender_RelKindMatrix_ShowsLanguageWithNoHierarchyEdges is the #6479
// render pin. nim emits CALLS and nothing else; java and python both emit
// EXTENDS, and only java emits IMPLEMENTS. The rendered row for nim must name
// its own kinds AND the kinds its peers emit that it does not — with the peer
// count, so a reader can tell "1 of 2 peers" from "2 of 2 peers" rather than
// being nagged about a kind one outlier happens to emit.
//
// Axes varied: language (3), relationship kind (3), peer-support level for a
// missing kind (2/2 vs 1/2), per-language kind breadth (1 vs 2 vs 3), and
// rendered count magnitude — every countRangeLabel bucket that the fixture can
// reach (1-5, 6-20, 21-100, 100+) appears, asserted on the render, so a
// falsified or constant count cannot pass.
// Held constant: the Report is a literal, so Generate's collection path — and
// with it the source-vs-target attribution and structural-kind axes — is not
// exercised here; TestGenerate_RelKindMatrix_AttributesEdgesToTheirSourceLanguage
// covers both. The zero-edge language axis is covered by
// TestGenerate_RelKindMatrix_LanguageWithNoEdgesKeepsItsRow.
func TestRender_RelKindMatrix_ShowsLanguageWithNoHierarchyEdges(t *testing.T) {
	r := &Report{
		TotalEntities: 500,
		Languages:     []string{"java", "python", "nim"},
		RelKindByLanguage: map[string]map[string]int{
			"java":   {"CALLS": 300, "EXTENDS": 40, "IMPLEMENTS": 12},
			"python": {"CALLS": 120, "EXTENDS": 7},
			"nim":    {"CALLS": 3},
		},
		RelKindUnattributed: map[string]int{"USES": 40},
	}

	var buf bytes.Buffer
	if err := Render(&buf, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()

	row := relRow(t, out, "nim")
	if row == "" {
		t.Fatalf("nim has no row in the rendered relationship-kind matrix — the language this issue is about is invisible.\n%s", out)
	}
	if !strings.Contains(row, "CALLS") {
		t.Errorf("nim row does not report the one kind it does emit (CALLS): %q", row)
	}
	if !strings.Contains(row, "EXTENDS (2/2 peers)") {
		t.Errorf("nim row does not flag EXTENDS as emitted by both peers and not by nim: %q", row)
	}
	if !strings.Contains(row, "IMPLEMENTS (1/2 peers)") {
		t.Errorf("nim row does not flag IMPLEMENTS as emitted by 1 of 2 peers: %q", row)
	}

	// python is missing IMPLEMENTS from exactly one peer — the weaker signal
	// must still be reported, not swallowed by a "most peers emit it" filter.
	prow := relRow(t, out, "python")
	if !strings.Contains(prow, "IMPLEMENTS (1/2 peers)") {
		t.Errorf("python row does not flag IMPLEMENTS (emitted by 1 of 2 peers): %q", prow)
	}
	if strings.Contains(relMissingCol(t, prow), "EXTENDS") {
		t.Errorf("python emits EXTENDS itself and must not be listed as missing it: %q", prow)
	}

	// The COUNTS are part of the artefact, so they are asserted on the render
	// as their range labels. Falsifying countRangeLabel to a constant — the
	// whole table reading "1-5" — must not survive.
	if got, want := relEmittedCol(t, row), "CALLS (1-5)"; got != want {
		t.Errorf("nim emitted column = %q, want %q", got, want)
	}
	// java exercises three different buckets in one row.
	if got, want := relEmittedCol(t, relRow(t, out, "java")), "CALLS (100+), EXTENDS (21-100), IMPLEMENTS (6-20)"; got != want {
		t.Errorf("java emitted column = %q, want %q", got, want)
	}
	if got, want := relEmittedCol(t, prow), "CALLS (100+), EXTENDS (6-20)"; got != want {
		t.Errorf("python emitted column = %q, want %q", got, want)
	}

	// Unattributable edges get their own row and their own count, and take no
	// part in the peer arithmetic (they are not a language).
	urow := relRow(t, out, "_unattributed_")
	if urow == "" {
		t.Fatalf("edges with no attributable source language have no row — they are dropped silently.\n%s", out)
	}
	if got, want := relEmittedCol(t, urow), "USES (21-100)"; got != want {
		t.Errorf("_unattributed_ emitted column = %q, want %q", got, want)
	}
	if got := relMissingCol(t, urow); got != "—" {
		t.Errorf("_unattributed_ is not a language and must carry no peer verdict, got %q", got)
	}

	// java emits every kind observed anywhere: nothing missing.
	jrow := relRow(t, out, "java")
	if got := relMissingCol(t, jrow); got != "—" {
		t.Errorf("java emits every observed kind but its missing column is %q (row %q)", got, jrow)
	}
}

// TestGenerate_RelKindMatrix_KeepsLanguageBelowSuppressionFloor pins the
// deliberate opt-out from the `< 10` suppression that EntitiesByLanguage,
// EntityKindDist, OrphanByKind and FrameworkHits all apply
// (internal/feedback/report.go). A language emitting zero hierarchy edges is by
// definition small on some axis, so inheriting that floor would rebuild #6479
// from the inside: the matrix would be unable to show the thing it exists to
// show.
//
// Axes varied: per-language entity volume (below vs above the floor of 10),
// relationship kind (CALLS vs EXTENDS — structural kinds are varied in
// TestGenerate_RelKindMatrix_AttributesEdgesToTheirSourceLanguage, which owns
// that axis), and language.
// Held constant: single document (doc-count is the #6378 axis, unrelated to a
// per-language edge tally); and every entity carries a language, since the
// language-less path is EntityKindDist's documented exclusion and not a
// dimension of this matrix.
func TestGenerate_RelKindMatrix_KeepsLanguageBelowSuppressionFloor(t *testing.T) {
	var ents []graph.Entity
	var rels []graph.Relationship

	// nim: 3 entities, 2 CALLS edges, no hierarchy edge at all. Deliberately
	// under every `< 10` floor in the file.
	for i := 0; i < 3; i++ {
		ents = append(ents, makeEntity("nim"+string(rune('a'+i)), "n", "SCOPE.Operation", "nim", "n.nim", 1+i))
	}
	rels = append(rels,
		rel6346("nc1", "nima", "nimb", "CALLS"),
		rel6346("nc2", "nimb", "nimc", "CALLS"),
	)

	// java: comfortably above the floor, and it emits EXTENDS.
	for i := 0; i < 12; i++ {
		ents = append(ents, makeEntity("jv"+string(rune('a'+i)), "J", "SCOPE.Class", "java", "J.java", 1+i))
	}
	for i := 0; i < 11; i++ {
		rels = append(rels, rel6346("jx"+string(rune('a'+i)), "jv"+string(rune('a'+i)), "jv"+string(rune('a'+i+1)), "EXTENDS"))
	}

	pe, pr := pad6346()
	ents = append(ents, pe...)
	rels = append(rels, pr...)

	r, err := Generate(context.Background(), []*graph.Document{makeDoc(ents, rels)}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Control: nim IS suppressed out of EntitiesByLanguage. If this ever stops
	// holding the test below stops proving an opt-out.
	if _, ok := r.EntitiesByLanguage["nim"]; ok {
		t.Fatalf("control broken: nim (3 entities) survived the EntitiesByLanguage `< 10` suppression, so this fixture no longer exercises the opt-out")
	}

	if got := r.RelKindByLanguage["nim"]["CALLS"]; got != 2 {
		t.Errorf("RelKindByLanguage[nim][CALLS] = %d, want 2", got)
	}

	var buf bytes.Buffer
	if err := Render(&buf, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()

	row := relRow(t, out, "nim")
	if row == "" {
		t.Fatalf("nim vanished from the rendered matrix — a small language inherited a `< 10` suppression, which is exactly the defect #6479 is about.\n%s", out)
	}
	if !strings.Contains(row, "EXTENDS (1/2 peers)") {
		t.Errorf("nim row does not report that a peer language emits EXTENDS while nim emits none: %q", row)
	}
}

// TestGenerate_RelKindMatrix_LanguageWithNoEdgesKeepsItsRow covers the extreme
// case: a language that contributes entities and sources no relationship of any
// kind. Its row must exist and say so. Seeding the matrix only from observed
// edges would drop it silently — a passing-by-absence exactly like the
// `entity-count-nonzero[nim]: PASS` this issue opens with.
//
// Axes varied: whether a language sources any edge at all (zero vs non-zero).
// Held constant: everything else matches the fixture above, so the only thing
// that changes the outcome is the edge-count axis under test.
func TestGenerate_RelKindMatrix_LanguageWithNoEdgesKeepsItsRow(t *testing.T) {
	var ents []graph.Entity
	var rels []graph.Relationship

	for i := 0; i < 4; i++ {
		ents = append(ents, makeEntity("ml"+string(rune('a'+i)), "M", "SCOPE.Class", "ocaml", "m.ml", 1+i))
	}
	for i := 0; i < 12; i++ {
		ents = append(ents, makeEntity("jv"+string(rune('a'+i)), "J", "SCOPE.Class", "java", "J.java", 1+i))
	}
	for i := 0; i < 11; i++ {
		rels = append(rels, rel6346("jx"+string(rune('a'+i)), "jv"+string(rune('a'+i)), "jv"+string(rune('a'+i+1)), "EXTENDS"))
	}

	pe, pr := pad6346()
	ents = append(ents, pe...)
	rels = append(rels, pr...)

	r, err := Generate(context.Background(), []*graph.Document{makeDoc(ents, rels)}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if _, ok := r.RelKindByLanguage["ocaml"]; !ok {
		t.Fatalf("ocaml sources no edge and has no matrix entry at all: %+v", r.RelKindByLanguage)
	}

	var buf bytes.Buffer
	if err := Render(&buf, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()

	row := relRow(t, out, "ocaml")
	if row == "" {
		t.Fatalf("a language sourcing zero relationships has no rendered row — it passes by absence.\n%s", out)
	}
	if !strings.Contains(row, "_none_") {
		t.Errorf("ocaml sources no edge; its row must say so explicitly: %q", row)
	}
	if !strings.Contains(row, "EXTENDS (1/2 peers)") {
		t.Errorf("ocaml row does not flag EXTENDS as emitted by a peer language: %q", row)
	}
}

// TestGenerate_RelKindMatrix_AttributesEdgesToTheirSourceLanguage owns the two
// axes the first fixtures deliberately hold constant, plus the unattributable
// case. Each is a design claim this PR makes in prose, and prose that no test
// observes is how #6479 happened in the first place:
//
//  1. CROSS-LANGUAGE EDGES are credited to the language of the edge's SOURCE
//     entity, never its target. With every edge intra-language the two rules
//     are indistinguishable, and getting it backwards would mis-credit exactly
//     the interesting edges in this graph — a JS fetch landing on a Go
//     endpoint would report Go as the language emitting the call.
//  2. STRUCTURAL KINDS (CONTAINS / DECLARES) are counted like any other kind.
//     The matrix deliberately does NOT reuse isStructuralEdge: that predicate's
//     CONTAINS/DECLARES-only exclusion is what makes CALLS "semantic" and so
//     lets a language emitting no hierarchy edge look connected — the very
//     classification #6479 is about.
//  3. EDGES WITH NO ATTRIBUTABLE SOURCE — a dangling FromID, or a source entity
//     carrying no Language — are reported in an _unattributed_ row rather than
//     dropped.
//
// Axes varied: cross-language vs intra-language edge; structural vs semantic vs
// hierarchy kind; attributable vs unattributable source, in both of its forms
// (dangling ID and empty Language); per-language entity volume either side of
// the `< 10` floor.
// Held constant: single document (doc count is the #6378 occurrence-vs-unique-ID
// axis and cannot change a per-language edge tally); no HistoryDir (it feeds
// check 2b's priorParticipation only, and nothing this matrix renders reads it).
func TestGenerate_RelKindMatrix_AttributesEdgesToTheirSourceLanguage(t *testing.T) {
	var ents []graph.Entity
	var rels []graph.Relationship

	// nim: below every `< 10` floor. One file container, three operations.
	ents = append(ents, makeEntity("nimfile", "n.nim", "SCOPE.Component", "nim", "n.nim", 1))
	for i := 0; i < 3; i++ {
		ents = append(ents, makeEntity("nim"+string(rune('a'+i)), "n", "SCOPE.Operation", "nim", "n.nim", 10+i))
	}
	rels = append(rels,
		rel6346("nc1", "nima", "nimb", "CALLS"),
		// Structural: nim's ONLY non-CALLS edge. Filtering the matrix through
		// isStructuralEdge blanks it.
		rel6346("nk1", "nimfile", "nima", "CONTAINS"),
	)

	// java: above the floor, emits EXTENDS.
	for i := 0; i < 12; i++ {
		ents = append(ents, makeEntity("jv"+string(rune('a'+i)), "J", "SCOPE.Class", "java", "J.java", 1+i))
	}
	for i := 0; i < 11; i++ {
		rels = append(rels, rel6346("jx"+string(rune('a'+i)), "jv"+string(rune('a'+i)), "jv"+string(rune('a'+i+1)), "EXTENDS"))
	}

	// The cross-language edge: java SOURCES it, nim is its TARGET. REFERENCES
	// appears nowhere else in the fixture, so whichever row carries it names
	// the attribution rule in force.
	rels = append(rels, rel6346("xlang", "jva", "nima", "REFERENCES"))

	// Unattributable, two ways: a source entity with no language at all, and a
	// FromID naming no entity in the graph.
	ents = append(ents, makeEntity("nolang", "x", "SCOPE.Operation", "", "x.txt", 1))
	rels = append(rels,
		rel6346("u1", "nolang", "jva", "USES"),
		rel6346("u2", "ghost-not-in-graph", "jva", "USES"),
	)

	pe, pr := pad6346()
	ents = append(ents, pe...)
	rels = append(rels, pr...)

	r, err := Generate(context.Background(), []*graph.Document{makeDoc(ents, rels)}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	var buf bytes.Buffer
	if err := Render(&buf, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()

	jrow := relRow(t, out, "java")
	nrow := relRow(t, out, "nim")
	if jrow == "" || nrow == "" {
		t.Fatalf("java or nim missing from the rendered matrix\n%s", out)
	}

	// (1) source attribution, pinned in BOTH directions.
	if !strings.Contains(relEmittedCol(t, jrow), "REFERENCES") {
		t.Errorf("the cross-language edge is sourced by java and must be credited to java, row: %q", jrow)
	}
	if strings.Contains(relEmittedCol(t, nrow), "REFERENCES") {
		t.Errorf("nim is only the TARGET of the cross-language edge and must not be credited with emitting it — the matrix is attributing edges to entityLang[ToID], which mis-credits every cross-language edge, row: %q", nrow)
	}

	// (2) structural kinds are counted.
	if !strings.Contains(relEmittedCol(t, nrow), "CONTAINS") {
		t.Errorf("nim's CONTAINS edge is missing from its rendered row — the matrix is filtering through isStructuralEdge, the exact classification #6479 is about: %q", nrow)
	}

	// (3) unattributable edges are reported, not dropped. Count asserted on the
	// render; the map read below is a control, not the observation.
	urow := relRow(t, out, "_unattributed_")
	if urow == "" {
		t.Fatalf("no _unattributed_ row: edges with a dangling or language-less source vanished silently.\n%s", out)
	}
	if got, want := relEmittedCol(t, urow), "USES (1-5)"; got != want {
		t.Errorf("_unattributed_ emitted column = %q, want %q (one language-less source + one dangling FromID)", got, want)
	}
	if got := r.RelKindUnattributed["USES"]; got != 2 {
		t.Errorf("control: RelKindUnattributed[USES] = %d, want 2", got)
	}
}
