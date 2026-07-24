// Package resolve — behaviour-neutrality battery for the nameKinds outer-map
// merge (#5954, BuildIndex outer-map compaction increment 1).
//
// The merge folded the two formerly-parallel name-keyed tables
//
//	nameKinds     map[string]kindIDs   (all kinds, SCOPE.* dual-indexed)
//	nameKindsReal map[string]kindIDs   (original kind only)
//
// into ONE map[string]nameKindEntry{base, real}. This file proves the swap is
// behaviourally invisible by rebuilding the PRE-MERGE tables with an
// independent replay of the old writers and asserting:
//
//  1. storage equivalence — for EVERY probed name (present or absent),
//     idx.nameKinds[name].base ≡ legacy nameKinds[name] and
//     idx.nameKinds[name].real ≡ legacy nameKindsReal[name], including the
//     comma-ok presence bit. Since every read site touches the merged map only
//     through .base / .real, identical values here mean identical inputs to
//     every consumer by construction.
//  2. entry-point equivalence — the live resolver entry points are compared
//     against reference implementations driven by the legacy two-map tables.
//  3. the #525 independent-blanking invariant — a real Component and a
//     SCOPE.Component sharing a name blank .base but leave .real intact, so the
//     real id stays recoverable. Asserted directly on the merged value.
//
// Both index construction paths (flat BuildIndex and the M5
// BuildIndexFromModulesOrdered mirror) are checked against the same legacy
// tables.
package resolve

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// ---- legacy (pre-merge) writer replay ---------------------------------------

// legacyNameKindTables replays the exact pre-merge write rules from BuildIndex:
// nameKinds is written unconditionally under every (original + SCOPE-trimmed)
// kind; nameKindsReal is written only when e.Kind != "" and only under the
// original kind. Two separate maps, blanked independently by kindIDs.set.
func legacyNameKindTables(ents []types.EntityRecord) (base, real map[string]kindIDs) {
	base = map[string]kindIDs{}
	real = map[string]kindIDs{}
	for _, e := range ents {
		b := base[e.Name]
		for _, k := range kindsOf(e) {
			if k == "" {
				continue
			}
			b.set(k, e.ID)
		}
		base[e.Name] = b

		if e.Kind != "" {
			r := real[e.Name]
			r.set(e.Kind, e.ID)
			real[e.Name] = r
		}
	}
	return base, real
}

// ---- reference implementations over the legacy two-map layout ----------------

// refLookupByKindHint is a verbatim copy of the pre-merge lookupByKindHint body
// driven by the two legacy tables.
func refLookupByKindHint(base, real map[string]kindIDs, name, relKind string) (string, bool) {
	families := hintKinds(relKind)
	if len(families) == 0 {
		return "", false
	}
	if realBucket := real[name]; realBucket.len() > 0 {
		if id, ok := uniqueMatchInFamily(realBucket, families, false); ok {
			return id, true
		}
	}
	bucket := base[name]
	if bucket.len() == 0 {
		return "", false
	}
	return uniqueMatchInFamily(bucket, families, true)
}

// refLookupUniqueModelByName mirrors the pre-merge django_fk.go probe.
func refLookupUniqueModelByName(real map[string]kindIDs, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	realBucket := real[name]
	if realBucket.len() == 0 {
		return "", false
	}
	id, ok := realBucket.get(string(types.EntityKindModel))
	if !ok || id == "" {
		return "", false
	}
	return id, true
}

// refRealComponentTier1 mirrors the pre-merge tier-1 of
// lookupUniqueRealComponentByName (the only part that reads nameKindsReal).
func refRealComponentTier1(real map[string]kindIDs, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if realBucket := real[name]; realBucket.len() > 0 {
		return uniqueMatchInFamily(realBucket, componentKindFamily, false)
	}
	return "", false
}

// liveRealComponentTier1 is the same tier-1 read against the merged value.
func liveRealComponentTier1(idx Index, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if realBucket := idx.nameKinds[name].real; realBucket.len() > 0 {
		return uniqueMatchInFamily(realBucket, componentKindFamily, false)
	}
	return "", false
}

// ---- fixture ----------------------------------------------------------------

// mergeEquivFixture = equivFixture() (multi-language representative set + the
// #525 Widget collision) PLUS adversarial shapes that stress the merge:
// kind-less entities (base key written, real key NOT), pure SCOPE.* entities,
// same-name/different-kind, same-(name,kind) collisions in both tiers, and a
// name whose base bucket is blanked while real survives.
func mergeEquivFixture() []types.EntityRecord {
	ents := equivFixture()
	ents = append(ents,
		// Kind-less entity: writes the base key only, never the real key.
		entAt("0000000000000a01", "", "KindlessThing", "app/kindless.py"),
		// Pure SCOPE.* placeholder with no real twin: base gets both
		// "SCOPE.Operation" and "Operation"; real gets "SCOPE.Operation".
		entAt("0000000000000a02", "SCOPE.Operation", "OnlyPlaceholder", "app/p.py"),
		// #525 shape again for an Operation family: real Function + a
		// SCOPE.Operation sharing the name. base["Operation"] stays clean but
		// the pair exercises the cross-tier preference.
		entAt("0000000000000a03", "Function", "HandlerX", "app/h.py"),
		entAt("0000000000000a04", "SCOPE.Operation", "HandlerX", "app/h2.py"),
		// Same (name, kind) twice => blank sentinel in BOTH base and real.
		entAt("0000000000000a05", "Class", "DoubleBooked", "a/one.java"),
		entAt("0000000000000a06", "Class", "DoubleBooked", "a/two.java"),
		// Same name under two different real kinds => no blanking in either.
		entAt("0000000000000a07", "Model", "Mixed", "a/m.py"),
		entAt("0000000000000a08", "View", "Mixed", "a/v.py"),
		// SCOPE.Component whose TRIMMED kind collides with a real Component
		// under a THIRD name — base["Component"] blanks, real keeps both.
		entAt("0000000000000a09", "Component", "Trimmed", "a/c1.ts"),
		entAt("0000000000000a10", "SCOPE.Component", "Trimmed", "a/c2.ts"),
	)
	return ents
}

// probeNames returns every name in the fixture plus names guaranteed absent, so
// the comma-ok presence bit is exercised on both sides.
func probeNames(ents []types.EntityRecord) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range ents {
		if !seen[e.Name] {
			seen[e.Name] = true
			out = append(out, e.Name)
		}
	}
	return append(out, "", "AbsentName", "Widget.NotAKey", "SCOPE.Component")
}

// allHintKinds covers every relKind branch of hintKinds plus an unhinted one.
var allHintKinds = []string{
	"EXTENDS", "IMPLEMENTS", "CALLS", "PUBLISHES_TO",
	"INJECTED_INTO", "BINDS", "GRAPH_RELATES", "REGISTERS",
	"HANDLES_SIGNAL", "FEDERATES", "DEPENDS_ON_SERVICE", "TRIGGERS",
	"ENQUEUES", "HANDLES_COMMAND", "GATED_BY", "DATA_FLOWS_TO", "THROWS",
	"CATCHES", "INSTRUMENTS", "CACHES", "INVALIDATES", "JOINS_CHANNEL",
	"BROADCASTS_TO", "RESOLVED_BY", "VALIDATES", "FETCHES",
	"GRPC_IMPLEMENTS", "GRPC_HANDLES", "HANDLES", "DISCRIMINATES_ON",
	"BRANCHES_ON", "RENDERS", "USES_TRANSLATION", "REFERENCES", "",
}

// ---- 1. storage equivalence -------------------------------------------------

func assertMergedMatchesLegacy(t *testing.T, label string, idx Index, ents []types.EntityRecord) {
	t.Helper()
	base, real := legacyNameKindTables(ents)

	// Key set: the merged map must carry exactly the legacy base key set
	// (legacy real keys are a strict subset — real is only written on
	// iterations that also write base).
	if len(idx.nameKinds) != len(base) {
		t.Fatalf("%s: merged nameKinds has %d keys, legacy base has %d", label, len(idx.nameKinds), len(base))
	}
	for name := range real {
		if _, ok := base[name]; !ok {
			t.Fatalf("%s: legacy real key %q absent from legacy base — merge precondition violated", label, name)
		}
	}

	for _, name := range probeNames(ents) {
		ent, gotOK := idx.nameKinds[name]
		wantBase, wantOK := base[name]
		if gotOK != wantOK {
			t.Fatalf("%s: presence bit for %q = %v, legacy nameKinds = %v", label, name, gotOK, wantOK)
		}
		if !reflect.DeepEqual(ent.base, wantBase) {
			t.Fatalf("%s: nameKinds[%q].base = %+v, legacy = %+v", label, name, ent.base, wantBase)
		}
		// Absent from legacy real => zero value, which is exactly what the
		// old `idx.nameKindsReal[name]` read returned.
		wantReal := real[name]
		if !reflect.DeepEqual(ent.real, wantReal) {
			t.Fatalf("%s: nameKinds[%q].real = %+v, legacy = %+v", label, name, ent.real, wantReal)
		}
	}
}

func TestNameKindsMerge_StorageEquivalence(t *testing.T) {
	for _, tc := range []struct {
		name string
		ents []types.EntityRecord
	}{
		{"representative", representativeFixture()},
		{"equiv", equivFixture()},
		{"mergeEquiv", mergeEquivFixture()},
	} {
		t.Run(tc.name+"/flat", func(t *testing.T) {
			assertMergedMatchesLegacy(t, "BuildIndex", BuildIndex(tc.ents), tc.ents)
		})
		t.Run(tc.name+"/modules", func(t *testing.T) {
			// M5 mirror (insertModuleEntry) must agree with the same legacy
			// tables, not merely with BuildIndex.
			idx := BuildIndexFromModulesOrdered(tc.ents, ModuleKeyByPkgDir)
			assertMergedMatchesLegacy(t, "BuildIndexFromModulesOrdered", idx, tc.ents)
		})
	}
}

// ---- 2. entry-point equivalence ---------------------------------------------

func TestNameKindsMerge_EntryPointEquivalence(t *testing.T) {
	ents := mergeEquivFixture()
	idx := BuildIndex(ents)
	base, real := legacyNameKindTables(ents)

	for _, name := range probeNames(ents) {
		for _, rk := range allHintKinds {
			gotID, gotOK := idx.lookupByKindHint(name, rk)
			wantID, wantOK := refLookupByKindHint(base, real, name, rk)
			if gotID != wantID || gotOK != wantOK {
				t.Fatalf("lookupByKindHint(%q,%q) = (%q,%v), legacy = (%q,%v)",
					name, rk, gotID, gotOK, wantID, wantOK)
			}
		}

		gotID, gotOK := idx.lookupUniqueModelByName(name)
		wantID, wantOK := refLookupUniqueModelByName(real, name)
		if gotID != wantID || gotOK != wantOK {
			t.Fatalf("lookupUniqueModelByName(%q) = (%q,%v), legacy = (%q,%v)",
				name, gotID, gotOK, wantID, wantOK)
		}

		gotID, gotOK = liveRealComponentTier1(idx, name)
		wantID, wantOK = refRealComponentTier1(real, name)
		if gotID != wantID || gotOK != wantOK {
			t.Fatalf("lookupUniqueRealComponentByName tier-1(%q) = (%q,%v), legacy = (%q,%v)",
				name, gotID, gotOK, wantID, wantOK)
		}

		// nameExists reads the merged presence bit + .base length; the other
		// two tiers (byName / ambigName) are untouched by this change.
		wantExists := false
		if _, ok := idx.byName[name]; ok {
			wantExists = true
		} else if idx.ambigName[name] {
			wantExists = true
		} else if b, ok := base[name]; ok && b.len() > 0 {
			wantExists = true
		}
		if got := idx.nameExists(name); got != wantExists {
			t.Fatalf("nameExists(%q) = %v, legacy = %v", name, got, wantExists)
		}

		// DiagnoseBugResolver's KindsPresent is drawn from the base bucket.
		diag := idx.DiagnoseBugResolver(name+":"+name, "EXTENDS")
		wantKinds := map[string]bool{}
		if b, ok := base[name]; ok {
			b.each(func(k, _ string) { wantKinds[k] = true })
		}
		gotKinds := map[string]bool{}
		for _, k := range diag.KindsPresent {
			gotKinds[k] = true
		}
		if len(wantKinds) > 0 && !reflect.DeepEqual(gotKinds, wantKinds) {
			t.Fatalf("DiagnoseBugResolver(%q).KindsPresent = %v, legacy = %v", name, gotKinds, wantKinds)
		}
	}
}

// ---- 3. the #525 independent-blanking invariant -----------------------------

// TestNameKindsMerge_IndependentBlanking525 proves the load-bearing invariant of
// the merge: .base and .real inside ONE map value are blanked independently, so
// a SCOPE.* placeholder colliding with a real entity destroys the id in .base
// but NOT in .real. If the two ever shared state (or .real were derived from
// .base by filtering SCOPE.*), the real id would be unrecoverable and the
// EXTENDS bind would fall through to the placeholder.
func TestNameKindsMerge_IndependentBlanking525(t *testing.T) {
	const realID, scopeID = "00000000000005a1", "00000000000005a2"
	idx := BuildIndex(equivFixture())
	ent := idx.nameKinds["Widget"]

	// .base: "Component" is dual-indexed by BOTH the real Component (5a1) and
	// the SCOPE.Component's trimmed kind (5a2) => present-but-BLANK.
	if id, present := ent.base.get("Component"); !present || id != "" {
		t.Fatalf("base[Component] = (%q,%v), want blank sentinel present — the real id must be UNRECOVERABLE from base", id, present)
	}
	// The real id is therefore only obtainable from .real.
	if id, present := ent.real.get("Component"); !present || id != realID {
		t.Fatalf("real[Component] = (%q,%v), want (%s,true) — independent blanking broken", id, present, realID)
	}
	// The placeholder keeps its own untrimmed slot in .real; it must not have
	// blanked the real entity's slot.
	if id, present := ent.real.get("SCOPE.Component"); !present || id != scopeID {
		t.Fatalf("real[SCOPE.Component] = (%q,%v), want (%s,true)", id, present, scopeID)
	}
	// End-to-end: the tier-1 real pass must bind to the real component.
	if id, ok := idx.lookupByKindHint("Widget", "EXTENDS"); !ok || id != realID {
		t.Fatalf("lookupByKindHint(Widget,EXTENDS) = (%q,%v), want (%s,true)", id, ok, realID)
	}

	// Mirror check on the merged Trimmed pair from mergeEquivFixture: same
	// shape, different names, and the M5 path must agree.
	for label, built := range map[string]Index{
		"flat":    BuildIndex(mergeEquivFixture()),
		"modules": BuildIndexFromModulesOrdered(mergeEquivFixture(), ModuleKeyByPkgDir),
	} {
		e := built.nameKinds["Trimmed"]
		if id, present := e.base.get("Component"); !present || id != "" {
			t.Fatalf("%s: Trimmed base[Component] = (%q,%v), want blank sentinel", label, id, present)
		}
		if id, present := e.real.get("Component"); !present || id != "0000000000000a09" {
			t.Fatalf("%s: Trimmed real[Component] = (%q,%v), want (a09,true)", label, id, present)
		}
		if id, ok := built.lookupByKindHint("Trimmed", "EXTENDS"); !ok || id != "0000000000000a09" {
			t.Fatalf("%s: lookupByKindHint(Trimmed,EXTENDS) = (%q,%v), want (a09,true)", label, id, ok)
		}
	}
}

// TestNameKindsMerge_CollisionSentinelBothFields asserts present-but-blank
// ("",true) stays distinguishable from absent ("",false) in BOTH fields of the
// merged value — the sentinel semantics every consumer depends on.
func TestNameKindsMerge_CollisionSentinelBothFields(t *testing.T) {
	idx := BuildIndex(mergeEquivFixture())
	ent := idx.nameKinds["DoubleBooked"]

	for field, b := range map[string]kindIDs{"base": ent.base, "real": ent.real} {
		id, present := b.get("Class")
		if !present {
			t.Fatalf("%s[Class] absent; want present blank sentinel", field)
		}
		if id != "" {
			t.Fatalf("%s[Class] = %q, want blank sentinel", field, id)
		}
		if id, present := b.get("NoSuchKind"); present || id != "" {
			t.Fatalf("%s[NoSuchKind] = (%q,%v), want (\"\",false)", field, id, present)
		}
	}

	// A kind-less entity writes the base key but leaves .real empty — the
	// exact shape that made keys(real) a strict subset of keys(base).
	kl, ok := idx.nameKinds["KindlessThing"]
	if !ok {
		t.Fatalf("kind-less entity did not create a nameKinds key")
	}
	if kl.base.len() != 0 || kl.real.len() != 0 {
		t.Fatalf("KindlessThing entry = %+v, want both buckets empty", kl)
	}
}

// ---- 4. read-path benchmark -------------------------------------------------

// benchFixture builds a corpus-shaped entity set: n distinct names, a mix of
// real and SCOPE.* kinds, plus collisions, so the benchmark exercises a map of
// realistic cardinality rather than the tiny correctness fixture.
func benchFixture(n int) []types.EntityRecord {
	kinds := []string{"Component", "Class", "Function", "Method", "Model",
		"View", "Operation", "SCOPE.Component", "SCOPE.Operation"}
	ents := make([]types.EntityRecord, 0, n)
	for i := 0; i < n; i++ {
		name := "Sym" + strconv.Itoa(i%(n*3/4+1))
		id := fmt.Sprintf("%016x", i+1)
		ents = append(ents, entAt(id, kinds[i%len(kinds)], name,
			"pkg"+strconv.Itoa(i%512)+"/f"+strconv.Itoa(i%64)+".go"))
	}
	return ents
}

func benchNames(ents []types.EntityRecord) []string {
	out := make([]string, 0, 4096)
	seen := map[string]bool{}
	for _, e := range ents {
		if !seen[e.Name] {
			seen[e.Name] = true
			out = append(out, e.Name)
		}
		if len(out) == 4096 {
			break
		}
	}
	return out
}

// BenchmarkLookupByKindHint_Merged measures the hot tier-1/tier-2 read path over
// the merged map (map[k]struct copy-on-read is the one cost the merge adds).
func BenchmarkLookupByKindHint_Merged(b *testing.B) {
	ents := benchFixture(200000)
	idx := BuildIndex(ents)
	names := benchNames(ents)
	b.ReportAllocs()
	b.ResetTimer()
	var sink int
	for i := 0; i < b.N; i++ {
		n := names[i%len(names)]
		if _, ok := idx.lookupByKindHint(n, "EXTENDS"); ok {
			sink++
		}
		if _, ok := idx.lookupByKindHint(n, "CALLS"); ok {
			sink++
		}
	}
	_ = sink
}

// BenchmarkLookupByKindHint_LegacyTwoMap is the SAME read path against the
// pre-merge two-map layout, so the merge's read cost can be compared directly
// in one binary.
func BenchmarkLookupByKindHint_LegacyTwoMap(b *testing.B) {
	ents := benchFixture(200000)
	base, real := legacyNameKindTables(ents)
	names := benchNames(ents)
	b.ReportAllocs()
	b.ResetTimer()
	var sink int
	for i := 0; i < b.N; i++ {
		n := names[i%len(names)]
		if _, ok := refLookupByKindHint(base, real, n, "EXTENDS"); ok {
			sink++
		}
		if _, ok := refLookupByKindHint(base, real, n, "CALLS"); ok {
			sink++
		}
	}
	_ = sink
}
