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
// Every assertion below reads the RENDERED artefact, not an internal tally,
// except where a counter is checked as an explicit control alongside the render.

// relRow returns the rendered matrix row for lang, or "" when the language has
// no row at all. Reading the render (rather than the map) is the point: a
// matrix that is populated but suppressed on the way out is decorative.
func relRow(t *testing.T, out, lang string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "| "+lang+" |") {
			return line
		}
	}
	return ""
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
// missing kind (2/2 vs 1/2), and per-language kind breadth (1 vs 2 vs 3).
// Held constant: the Report is a literal, so Generate's collection path is not
// exercised here — TestGenerate_* below covers that axis; and counts are all
// well above zero, since the zero-edge language axis is covered by
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
// relationship kind (CALLS vs EXTENDS vs CONTAINS), and language.
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
