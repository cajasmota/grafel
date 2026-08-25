package feedback

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// Fixtures for #6599 — leaf-aware terminality.
//
// Measured on 3 real VB.NET trees (302 .vb files, #6583): SCOPE.Operation
// orphan rate 51.3%, of which `property` is 75.2% of the pool; every property
// that HAS a call site is already wired (0.0% orphan of 485). The per-KIND
// derivation (#6346, report.go "known limitation (2)") cannot express that,
// because SCOPE.Operation obviously participates — methods call things — so
// every leaf sibling of the kind lands in the defect bucket.
//
// These fixtures pin the exemption in BOTH directions: leaves are excused, and
// nothing that is not a leaf is.

// leafFixture builds a group whose SCOPE.Operation kind PARTICIPATES (wired
// methods exist) and which additionally holds unwired leaf-subtype members.
func leafFixture(t *testing.T, leafSubtype string, leafCount int, nonLeafOrphans int) *Report {
	t.Helper()
	var ents []graph.Entity
	var rels []graph.Relationship

	ents = append(ents, makeEntity("file1", "Mod.vb", "SCOPE.Component", "vbnet", "Mod.vb", 1))

	// 10 wired methods: the kind participates, so it is NOT terminal.
	for i := 0; i < 10; i++ {
		id := "m" + string(rune('a'+i))
		ents = append(ents, withSubtype(makeEntity(id, "Meth", "SCOPE.Operation", "vbnet", "Mod.vb", 100+i), "function"))
		rels = append(rels, rel6346("c"+id, "file1", id, "CONTAINS"))
		rels = append(rels, rel6346("k"+id, id, "padAa", "CALLS"))
	}
	// Unwired leaf-subtype members of the SAME kind.
	for i := 0; i < leafCount; i++ {
		id := "p" + string(rune('a'+i))
		ents = append(ents, withSubtype(makeEntity(id, "Prop", "SCOPE.Operation", "vbnet", "Mod.vb", 200+i), leafSubtype))
		rels = append(rels, rel6346("c"+id, "file1", id, "CONTAINS"))
	}
	// Unwired NON-leaf members of the same kind: genuine defect orphans.
	for i := 0; i < nonLeafOrphans; i++ {
		id := "s" + string(rune('a'+i))
		ents = append(ents, withSubtype(makeEntity(id, "Sub", "SCOPE.Operation", "vbnet", "Mod.vb", 300+i), "sub"))
		rels = append(rels, rel6346("c"+id, "file1", id, "CONTAINS"))
	}

	pe, pr := pad6346()
	ents = append(ents, pe...)
	rels = append(rels, pr...)

	r, err := Generate(context.Background(), []*graph.Document{makeDoc(ents, rels)}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return r
}

// TestLeafSubtypeOrphansLeaveTheDefectBucket_6599 is the primary target: a
// property leaf inside a participating kind must not be a defect orphan.
func TestLeafSubtypeOrphansLeaveTheDefectBucket_6599(t *testing.T) {
	r := leafFixture(t, "property", 12, 3)

	ks, ok := r.OrphanByKind["SCOPE.Operation"]
	if !ok {
		t.Fatalf("SCOPE.Operation missing from OrphanByKind: %+v", r.OrphanByKind)
	}
	if ks.OrphanCount != 3 {
		t.Errorf("defect orphans = %d, want 3 (only the non-leaf `sub` orphans; 12 property leaves must be exempt)", ks.OrphanCount)
	}
	// The DENOMINATOR must not move: the exemption belongs to terminality, not
	// to the orphan definition. 10 methods + 12 properties + 3 subs + the 60
	// wired SCOPE.Operation pad entities = 85.
	if ks.Total != 85 {
		t.Errorf("Total = %d, want 85 — the leaf exemption must not remove entities from the denominator", ks.Total)
	}
	leaf, ok := r.OrphanLeafByKind["SCOPE.Operation"]
	if !ok {
		t.Fatalf("SCOPE.Operation missing from OrphanLeafByKind — the exempted leaves were dropped instead of reported: %+v", r.OrphanLeafByKind)
	}
	if leaf.OrphanCount != 12 {
		t.Errorf("leaf orphans = %d, want 12", leaf.OrphanCount)
	}
	if leaf.Total != 85 {
		t.Errorf("leaf-row Total = %d, want 85 (same denominator as the defect row)", leaf.Total)
	}
	// The exempted leaves must NOT enter the terminal bucket: the terminal
	// table is history.go's authoritative "kind never participated" signal.
	if _, bad := r.OrphanTerminalByKind["SCOPE.Operation"]; bad {
		t.Errorf("a PARTICIPATING kind entered OrphanTerminalByKind — history.go reads that table as proof the kind never carried a semantic edge, and that key never expires")
	}
}

// TestSchemaFieldLeavesExemptDespiteParticipation_6599 is the SCOPE.Schema
// half — 1,862 of 2,050 of that kind's orphans are field leaves (#6583).
func TestSchemaFieldLeavesExemptDespiteParticipation_6599(t *testing.T) {
	var ents []graph.Entity
	var rels []graph.Relationship
	ents = append(ents, makeEntity("parent", "Widget", "SCOPE.Class", "go", "w.go", 1))
	for i := 0; i < 12; i++ {
		id := "f" + string(rune('a'+i))
		ents = append(ents, withSubtype(makeEntity(id, "field", "SCOPE.Schema", "go", "w.go", 10+i), "field"))
		rels = append(rels, rel6346("c"+id, "parent", id, "CONTAINS"))
	}
	// One participating non-field SCOPE.Schema member flips the kind out of
	// terminal — that is the #6346 limitation this issue fixes.
	ents = append(ents, makeEntity("schemaDoc", "WidgetSchema", "SCOPE.Schema", "go", "w.go", 40))
	rels = append(rels,
		rel6346("cdoc", "parent", "schemaDoc", "CONTAINS"),
		rel6346("refdoc", "schemaDoc", "parent", "REFERENCES"),
	)
	pe, pr := pad6346()
	ents = append(ents, pe...)
	rels = append(rels, pr...)

	r, err := Generate(context.Background(), []*graph.Document{makeDoc(ents, rels)}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := r.OrphanByKind["SCOPE.Schema"].OrphanCount; got != 0 {
		t.Errorf("SCOPE.Schema defect orphans = %d, want 0 — all 12 unwired members are field leaves", got)
	}
	if got := r.OrphanLeafByKind["SCOPE.Schema"].OrphanCount; got != 12 {
		t.Errorf("SCOPE.Schema leaf orphans = %d, want 12", got)
	}
	if got := r.OrphanByKind["SCOPE.Schema"].Total; got != 13 {
		t.Errorf("SCOPE.Schema Total = %d, want 13 (denominator unchanged)", got)
	}
}

// TestNonLeafSubtypesAreNotExempt_6599 is the PERMISSIVE guard. An exemption
// that reaches beyond the storage leaves makes the orphan rate look healthy by
// excusing entities that genuinely should carry edges — the dangerous
// direction. Every subtype here is a real, non-leaf population that must stay
// in the defect bucket.
func TestNonLeafSubtypesAreNotExempt_6599(t *testing.T) {
	for _, subtype := range []string{"function", "sub", "constructor", "event", "operator", "class", "module", "structure", "interface", "file", "import", "delegate", ""} {
		t.Run("subtype="+subtype, func(t *testing.T) {
			r := leafFixture(t, subtype, 12, 0)
			if got := r.OrphanByKind["SCOPE.Operation"].OrphanCount; got != 12 {
				t.Errorf("subtype %q: defect orphans = %d, want 12 — %q is not a storage leaf and must not be exempt", subtype, got, subtype)
			}
			if got := r.OrphanLeafByKind["SCOPE.Operation"].OrphanCount; got != 0 {
				t.Errorf("subtype %q: leaf orphans = %d, want 0", subtype, got)
			}
		})
	}
}

// TestWiredPropertyStaysInTheDenominator_6599 is the second PERMISSIVE guard.
// Measured: full properties WITH call sites are 0.0% orphaned (0 of 485). They
// are not orphans, so the exemption must never reach them — neither by
// removing them from Total nor by counting them as exempted leaves. A change
// that dropped wired properties from the denominator would inflate the orphan
// rate while looking like a fix.
func TestWiredPropertyStaysInTheDenominator_6599(t *testing.T) {
	var ents []graph.Entity
	var rels []graph.Relationship
	ents = append(ents, makeEntity("file1", "Mod.vb", "SCOPE.Component", "vbnet", "Mod.vb", 1))
	for i := 0; i < 10; i++ {
		id := "m" + string(rune('a'+i))
		ents = append(ents, withSubtype(makeEntity(id, "Meth", "SCOPE.Operation", "vbnet", "Mod.vb", 100+i), "function"))
		rels = append(rels, rel6346("c"+id, "file1", id, "CONTAINS"), rel6346("k"+id, id, "padAa", "CALLS"))
	}
	// 8 properties WITH a call site in their accessor body — wired.
	for i := 0; i < 8; i++ {
		id := "w" + string(rune('a'+i))
		ents = append(ents, withSubtype(makeEntity(id, "Prop", "SCOPE.Operation", "vbnet", "Mod.vb", 200+i), "property"))
		rels = append(rels, rel6346("c"+id, "file1", id, "CONTAINS"), rel6346("k"+id, id, "padAa", "CALLS"))
	}
	// 4 auto-properties — unwired leaves.
	for i := 0; i < 4; i++ {
		id := "u" + string(rune('a'+i))
		ents = append(ents, withSubtype(makeEntity(id, "Auto", "SCOPE.Operation", "vbnet", "Mod.vb", 300+i), "property"))
		rels = append(rels, rel6346("c"+id, "file1", id, "CONTAINS"))
	}
	pe, pr := pad6346()
	ents = append(ents, pe...)
	rels = append(rels, pr...)

	r, err := Generate(context.Background(), []*graph.Document{makeDoc(ents, rels)}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	ks := r.OrphanByKind["SCOPE.Operation"]
	if ks.Total != 82 {
		t.Errorf("Total = %d, want 82 (10 methods + 8 wired properties + 4 auto-properties + 60 pads) — wired properties must stay in the denominator", ks.Total)
	}
	if ks.OrphanCount != 0 {
		t.Errorf("defect orphans = %d, want 0", ks.OrphanCount)
	}
	if got := r.OrphanLeafByKind["SCOPE.Operation"].OrphanCount; got != 4 {
		t.Errorf("leaf orphans = %d, want 4 — only the UNWIRED properties are exempted; a wired one is not an orphan at all", got)
	}
}

// TestLeafExemptionKeepsHistoryJoinKeyIntact_6599 is the one-way-door guard
// (history.go:42-48, :90-91). parseKindParticipation reads participation out
// of the rendered markdown; membership of the terminal table is authoritative
// proof a kind NEVER participated, OR-ed across every stored report and never
// expiring. A participating kind whose leaves are exempted must therefore
// still round-trip as PARTICIPATING.
func TestLeafExemptionKeepsHistoryJoinKeyIntact_6599(t *testing.T) {
	r := leafFixture(t, "property", 12, 3)
	var sb strings.Builder
	if err := Render(&sb, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := sb.String()

	if !strings.Contains(out, defectTableHeader) {
		t.Fatalf("the defect table header %q is gone from the rendered report — that is the history join key", defectTableHeader)
	}
	part := parseKindParticipation(out)
	got, ok := part["SCOPE.Operation"]
	if !ok {
		t.Fatalf("SCOPE.Operation did not round-trip through parseKindParticipation: %+v", part)
	}
	if !got {
		t.Error("SCOPE.Operation round-trips as NEVER PARTICIPATING after the leaf exemption — that is the one-way door in history.go firing `kind-participation-not-regressed` forever on a healthy kind")
	}
}

// TestRender_LeafCaptionIsAccurateAndNotATable_6599 pins the two rendering
// properties that matter.
//
// (1) The Section-2 legend must say that storage leaves are excluded. #6608
// added captions naming each table's unit, scope and floor; after #6599 the
// bucket clause was no longer the whole truth, and a caption that is now wrong
// is worse than no caption.
//
// (2) The leaf list must NOT be a four-column pipe table. history.go's
// kindRowRe matches any "| kind | int | int | pct% |" row inside Section 2 and
// attributes it to the last header it recognised, so a third table would be
// read back as defect or terminal rows — and terminal membership is permanent.
func TestRender_LeafCaptionIsAccurateAndNotATable_6599(t *testing.T) {
	r := leafFixture(t, "property", 12, 3)
	var sb strings.Builder
	if err := Render(&sb, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := sb.String()

	const wantLeafClause = "Unwired STORAGE LEAVES (`field`, `column`, `property`) are excluded from the count too and listed under **Terminal-by-subtype leaves** — they declare state and nothing else, so they have nothing to link."
	if !strings.Contains(out, wantLeafClause) {
		t.Errorf("Section-2 legend does not disclose the leaf exclusion, so the defect table's own caption over-promises what it counts.\nwant substring: %q", wantLeafClause)
	}
	// The list's OWN caption, verbatim. Asserting only that the phrase
	// "**Terminal-by-subtype leaves**" appears somewhere is vacuous — the
	// legend clause above already contains it, so the whole block could be
	// renamed or lose its unit statement with the package still green (this
	// test's first version did exactly that, and mutant M5 survived it).
	const wantLeafCaption = "**Terminal-by-subtype leaves** — unwired entities whose subtype is a storage leaf (`field`, `column`, `property`) inside a kind that DOES carry semantic edges elsewhere. A leaf declares state and nothing else, so its only edge is the structural CONTAINS this metric excludes; it is not an edge gap. Excluded from the orphan defect count above, and still counted in that table's `Total`."
	if !strings.Contains(out, wantLeafCaption) {
		t.Fatalf("the leaf list is missing or its caption is stale — it must name the unit (UNWIRED entities only), the scope (a participating kind) and its relation to the table above.\nwant substring: %q", wantLeafCaption)
	}
	if !strings.Contains(out, "- `SCOPE.Operation`: 12 of 85 (14.1%)") {
		t.Errorf("leaf list row missing or malformed; got section:\n%s", out)
	}

	// Structural: no row of the leaf list may be parseable as a kind row.
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "- `SCOPE.Operation`") {
			continue
		}
		if kindRowRe.MatchString(strings.TrimRight(line, " \t\r")) {
			t.Errorf("leaf list row is shaped like a Section-2 table row and would be parsed back by history.go: %q", line)
		}
	}
	// And the two real headers must both still be exactly what history matches.
	if !strings.Contains(out, defectTableHeader) {
		t.Errorf("defect table header changed — that is the one-way join key %q", defectTableHeader)
	}
}
