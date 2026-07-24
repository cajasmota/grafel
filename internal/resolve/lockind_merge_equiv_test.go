// Package resolve — behaviour-neutrality battery for the byLocationKind
// inner-map merge (#5954, BuildIndex outer-map compaction increment 2).
//
// The merge folded the two formerly-parallel location-keyed tables
//
//	byLocationKind     map[file]map[name]kindIDs   (all kinds, SCOPE.* dual-indexed)
//	byLocationKindReal map[file]map[name]kindIDs   (original kind only)
//
// into ONE map[file]map[name]locKindEntry{base, real}. This file proves the
// swap is behaviourally invisible by rebuilding the PRE-MERGE tables with an
// independent replay of the old writers (taken verbatim from refs.go at
// c90466054, i.e. BEFORE this change) and asserting:
//
//  1. storage equivalence — for EVERY probed (file, name) pair, present AND
//     absent, at BOTH map levels:
//     idx.byLocationKind[f][n].base ≡ legacy byLocationKind[f][n] and
//     idx.byLocationKind[f][n].real ≡ legacy byLocationKindReal[f][n],
//     including the comma-ok presence bits. Since every read site touches the
//     merged map only through .base / .real, identical values here mean
//     identical inputs to every consumer by construction.
//  2. entry-point equivalence — the live resolver entry points are compared
//     against reference implementations driven by the legacy two-map tables.
//  3. the #525 independent-blanking invariant — a real Component and a
//     SCOPE.Component sharing a (file, name) blank .base but leave .real
//     intact, so the real id stays recoverable. Asserted directly on the
//     merged value AND end-to-end through lookupLocationKind.
//
// THE TWO-LEVEL PRESENCE ARGUMENT (what makes the merge legal here).
// The pre-merge writer wrote the real tables strictly INSIDE the base write's
// `sourceFile != ""` block, in the SAME loop iteration, further narrowed only
// by `e.Kind != ""`. Hence, per index build:
//
//	keys(byLocationKindReal)      ⊆ keys(byLocationKind)          (outer, file)
//	keys(byLocationKindReal[f])   ⊆ keys(byLocationKind[f])       (inner, name)
//
// No path anywhere writes the real table for a (file, name) that received no
// base write. The merged key set is therefore EXACTLY the old byLocationKind
// key set at both levels, so no comma-ok / nil-bucket read shifts. The
// assertions below re-establish this mechanically rather than by assumption:
// assertLocKindMergedMatchesLegacy fails loudly if any legacy real key (at
// either level) is missing from the legacy base key set.
//
// Both index construction paths (flat BuildIndex and the M5
// BuildIndexFromModulesOrdered mirror) are checked against the same legacy
// tables.
package resolve

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// ---- legacy (pre-merge) writer replay ---------------------------------------

// legacyLocationKindTables replays the exact pre-merge write rules from
// BuildIndex (refs.go @ c90466054):
//
//	if sourceFile != "" {
//	    fileKindBucket := idx.byLocationKind[sourceFile]
//	    if fileKindBucket == nil { ...make... }
//	    nameKindBucketLoc := fileKindBucket[e.Name]
//	    for _, kind := range kinds { if kind == "" { continue }; nameKindBucketLoc.set(kind, e.ID) }
//	    fileKindBucket[e.Name] = nameKindBucketLoc
//	    if e.Kind != "" {
//	        realFileBucket := idx.byLocationKindReal[sourceFile]
//	        if realFileBucket == nil { ...make... }
//	        realNameBucket := realFileBucket[e.Name]
//	        realNameBucket.set(e.Kind, e.ID)
//	        realFileBucket[e.Name] = realNameBucket
//	    }
//	}
//
// Two separate maps, blanked independently by kindIDs.set. This oracle is
// derived from the OLD code only — never from the merged implementation.
func legacyLocationKindTables(ents []types.EntityRecord) (base, real map[string]map[string]kindIDs) {
	base = map[string]map[string]kindIDs{}
	real = map[string]map[string]kindIDs{}
	for _, e := range ents {
		sourceFile := normalizePath(e.SourceFile)
		if sourceFile == "" {
			continue
		}
		fileKindBucket := base[sourceFile]
		if fileKindBucket == nil {
			fileKindBucket = map[string]kindIDs{}
			base[sourceFile] = fileKindBucket
		}
		nameKindBucketLoc := fileKindBucket[e.Name]
		for _, kind := range kindsOf(e) {
			if kind == "" {
				continue
			}
			nameKindBucketLoc.set(kind, e.ID)
		}
		fileKindBucket[e.Name] = nameKindBucketLoc

		if e.Kind != "" {
			realFileBucket := real[sourceFile]
			if realFileBucket == nil {
				realFileBucket = map[string]kindIDs{}
				real[sourceFile] = realFileBucket
			}
			realNameBucket := realFileBucket[e.Name]
			realNameBucket.set(e.Kind, e.ID)
			realFileBucket[e.Name] = realNameBucket
		}
	}
	return base, real
}

// ---- reference implementations over the legacy two-map layout ----------------

// refLookupLocationKind is a verbatim copy of the pre-merge lookupLocationKind
// body driven by the two legacy tables — including the original ordering, where
// the tier-1 real probe ran BEFORE the base file-bucket nil check.
func refLookupLocationKind(base, real map[string]map[string]kindIDs, filePath, name string, families []string) (string, bool) {
	if len(families) == 0 {
		return "", false
	}
	if realFileBucket := real[filePath]; realFileBucket != nil {
		if realNameBucket := realFileBucket[name]; realNameBucket.len() > 0 {
			if id, ok := uniqueMatchInFamily(realNameBucket, families, false); ok {
				return id, true
			}
		}
	}
	fileBucket := base[filePath]
	if fileBucket == nil {
		return "", false
	}
	nameBucket := fileBucket[name]
	if nameBucket.len() == 0 {
		return "", false
	}
	return uniqueMatchInFamily(nameBucket, families, true)
}

// refRealComponentScopeScan is a verbatim copy of the pre-merge
// byLocationKind scan inside lookupUniqueRealComponentByName (the part this
// change touches; the preceding nameKinds tier is untouched by increment 2).
func refRealComponentScopeScan(base map[string]map[string]kindIDs, name string) (string, bool) {
	scopeKind := scopeKindPrefix + "Component"
	var match string
	for _, fileBucket := range base {
		nameBucket := fileBucket[name]
		id, _ := nameBucket.get(scopeKind)
		if id == "" {
			continue
		}
		if match != "" && match != id {
			return "", false
		}
		match = id
	}
	if match != "" {
		return match, true
	}
	return "", false
}

// refLookupUniqueSchemaFieldByName is a verbatim copy of the pre-merge body.
func refLookupUniqueSchemaFieldByName(base map[string]map[string]kindIDs, fieldName string) (string, bool) {
	if fieldName == "" {
		return "", false
	}
	var match string
	for _, fileBucket := range base {
		for entityName, kindBucket := range fileBucket {
			leaf := entityName
			if dot := strings.LastIndexByte(entityName, '.'); dot >= 0 {
				leaf = entityName[dot+1:]
			}
			if leaf != fieldName {
				continue
			}
			for _, fam := range schemaKindFamily {
				id, _ := kindBucket.get(fam)
				if id == "" {
					continue
				}
				if match != "" && match != id {
					return "", false
				}
				match = id
			}
		}
	}
	if match != "" {
		return match, true
	}
	return "", false
}

// refBareLocalityRealTier is a verbatim copy of the pre-merge
// byLocationKindReal tier inside lookupBareWithLocality.
func refBareLocalityRealTier(real map[string]map[string]kindIDs, callerFile, name string, families []string) (string, bool) {
	if callerFile == "" || len(families) == 0 {
		return "", false
	}
	if fileBucket := real[callerFile]; fileBucket != nil {
		nameBucket := fileBucket[name]
		var match string
		ambig := false
		for _, k := range families {
			if strings.HasPrefix(k, scopeKindPrefix) {
				continue
			}
			id, _ := nameBucket.get(k)
			if id == "" {
				continue
			}
			if match != "" && match != id {
				ambig = true
				break
			}
			match = id
		}
		if !ambig && match != "" {
			return match, true
		}
	}
	return "", false
}

// refBareLocalityScopeTier is a verbatim copy of the pre-merge trailing
// byLocationKind SCOPE.* CALLS fallback inside lookupBareWithLocality.
func refBareLocalityScopeTier(base map[string]map[string]kindIDs, callerFile, name string) (string, bool) {
	if callerFile == "" {
		return "", false
	}
	if fileBucket := base[callerFile]; fileBucket != nil {
		nameBucket := fileBucket[name]
		if id, _ := nameBucket.get(scopeKindPrefix + "Operation"); id != "" {
			return id, true
		}
		if id, _ := nameBucket.get(scopeKindPrefix + "Component"); id != "" {
			return id, true
		}
	}
	return "", false
}

// ---- live-side probes mirroring the reference shapes ------------------------

func liveRealComponentScopeScan(idx Index, name string) (string, bool) {
	scopeKind := scopeKindPrefix + "Component"
	var match string
	for _, fileBucket := range idx.byLocationKind {
		id, _ := fileBucket[name].base.get(scopeKind)
		if id == "" {
			continue
		}
		if match != "" && match != id {
			return "", false
		}
		match = id
	}
	if match != "" {
		return match, true
	}
	return "", false
}

func liveBareLocalityRealTier(idx Index, callerFile, name string, families []string) (string, bool) {
	if callerFile == "" || len(families) == 0 {
		return "", false
	}
	if fileBucket := idx.byLocationKind[callerFile]; fileBucket != nil {
		nameBucket := fileBucket[name].real
		var match string
		ambig := false
		for _, k := range families {
			if strings.HasPrefix(k, scopeKindPrefix) {
				continue
			}
			id, _ := nameBucket.get(k)
			if id == "" {
				continue
			}
			if match != "" && match != id {
				ambig = true
				break
			}
			match = id
		}
		if !ambig && match != "" {
			return match, true
		}
	}
	return "", false
}

func liveBareLocalityScopeTier(idx Index, callerFile, name string) (string, bool) {
	if callerFile == "" {
		return "", false
	}
	if fileBucket := idx.byLocationKind[callerFile]; fileBucket != nil {
		nameBucket := fileBucket[name].base
		if id, _ := nameBucket.get(scopeKindPrefix + "Operation"); id != "" {
			return id, true
		}
		if id, _ := nameBucket.get(scopeKindPrefix + "Component"); id != "" {
			return id, true
		}
	}
	return "", false
}

// ---- fixture ----------------------------------------------------------------

// locMergeFixture = mergeEquivFixture() PLUS shapes that specifically stress
// the TWO-LEVEL merge: a file whose every entity is kind-less (so the pre-merge
// byLocationKindReal never created an outer key for it at all), a file mixing
// kind-less and kinded entities at the same name, a same-(file,name,kind)
// collision, and a same-file real/SCOPE.Component collision.
func locMergeFixture() []types.EntityRecord {
	ents := mergeEquivFixture()
	ents = append(ents,
		// Whole file of kind-less entities: legacy byLocationKind has the file
		// key, legacy byLocationKindReal does NOT. The single strongest test of
		// the outer presence-bit argument.
		entAt("0000000000000b01", "", "OnlyKindless", "loc/kindless_only.py"),
		entAt("0000000000000b02", "", "AlsoKindless", "loc/kindless_only.py"),
		// Same (file, name) where one entity is kind-less and one is not:
		// inner base key exists from both, inner real key from one.
		entAt("0000000000000b03", "", "MixedPresence", "loc/mixed.py"),
		entAt("0000000000000b04", "Class", "MixedPresence", "loc/mixed.py"),
		// Same (file, name, kind) twice => blank sentinel in BOTH base and real.
		entAt("0000000000000b05", "Class", "SameFileDup", "loc/dup.java"),
		entAt("0000000000000b06", "Class", "SameFileDup", "loc/dup.java"),
		// #525 shape confined to ONE file: real Component + SCOPE.Component.
		entAt("0000000000000b07", "Component", "LocWidget", "loc/w.py"),
		entAt("0000000000000b08", "SCOPE.Component", "LocWidget", "loc/w.py"),
		// A same-file schema field for lookupUniqueSchemaFieldByName.
		entAt("0000000000000b09", "SCOPE.Schema", "Holder.locField", "loc/h.java"),
		// A file with only a SCOPE.Operation, for the CALLS scope fallback.
		entAt("0000000000000b10", "SCOPE.Operation", "locHandler", "loc/ops.ts"),
	)
	return ents
}

// probeFiles returns every normalized source file in the fixture plus files
// guaranteed absent, so the outer comma-ok / nil-bucket bit is exercised on
// both sides.
func probeFiles(ents []types.EntityRecord) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range ents {
		sf := normalizePath(e.SourceFile)
		if sf != "" && !seen[sf] {
			seen[sf] = true
			out = append(out, sf)
		}
	}
	return append(out, "", "no/such/file.py", "loc/kindless_only.py.bak")
}

// ---- 1. storage equivalence -------------------------------------------------

func assertLocKindMergedMatchesLegacy(t *testing.T, label string, idx Index, ents []types.EntityRecord) {
	t.Helper()
	base, real := legacyLocationKindTables(ents)

	// Re-establish the TWO-LEVEL presence precondition mechanically: every
	// legacy real key must exist in legacy base at BOTH levels. If this ever
	// fails, the merged key set would differ from the old base key set and the
	// comma-ok / nil-bucket semantics at the read sites would shift.
	for file, realFileBucket := range real {
		baseFileBucket, ok := base[file]
		if !ok {
			t.Fatalf("%s: legacy real FILE key %q absent from legacy base — merge precondition violated", label, file)
		}
		for name := range realFileBucket {
			if _, ok := baseFileBucket[name]; !ok {
				t.Fatalf("%s: legacy real NAME key %q in file %q absent from legacy base — merge precondition violated", label, name, file)
			}
		}
	}

	// Outer key set must be exactly the legacy base outer key set.
	if len(idx.byLocationKind) != len(base) {
		t.Fatalf("%s: merged byLocationKind has %d file keys, legacy base has %d", label, len(idx.byLocationKind), len(base))
	}

	names := probeNames(ents)
	for _, file := range probeFiles(ents) {
		gotFileBucket, gotFileOK := idx.byLocationKind[file]
		wantFileBucket, wantFileOK := base[file]
		if gotFileOK != wantFileOK {
			t.Fatalf("%s: outer presence bit for %q = %v, legacy byLocationKind = %v", label, file, gotFileOK, wantFileOK)
		}
		if len(gotFileBucket) != len(wantFileBucket) {
			t.Fatalf("%s: file %q inner key count = %d, legacy = %d", label, file, len(gotFileBucket), len(wantFileBucket))
		}
		realFileBucket := real[file]
		for _, name := range names {
			ent, gotOK := gotFileBucket[name]
			wantBase, wantOK := wantFileBucket[name]
			if gotOK != wantOK {
				t.Fatalf("%s: inner presence bit for (%q,%q) = %v, legacy byLocationKind = %v", label, file, name, gotOK, wantOK)
			}
			if !reflect.DeepEqual(ent.base, wantBase) {
				t.Fatalf("%s: byLocationKind[%q][%q].base = %+v, legacy = %+v", label, file, name, ent.base, wantBase)
			}
			// Absent from legacy real (at either level) => zero value, which is
			// exactly what the old two-step read returned.
			wantReal := realFileBucket[name]
			if !reflect.DeepEqual(ent.real, wantReal) {
				t.Fatalf("%s: byLocationKind[%q][%q].real = %+v, legacy byLocationKindReal = %+v", label, file, name, ent.real, wantReal)
			}
		}
	}
}

func TestLocKindMerge_StorageEquivalence(t *testing.T) {
	for _, tc := range []struct {
		name string
		ents []types.EntityRecord
	}{
		{"representative", representativeFixture()},
		{"equiv", equivFixture()},
		{"mergeEquiv", mergeEquivFixture()},
		{"locMerge", locMergeFixture()},
	} {
		t.Run(tc.name+"/flat", func(t *testing.T) {
			assertLocKindMergedMatchesLegacy(t, "BuildIndex", BuildIndex(tc.ents), tc.ents)
		})
		t.Run(tc.name+"/modules", func(t *testing.T) {
			// M5 mirror (insertModuleEntry) must agree with the same legacy
			// tables, not merely with BuildIndex.
			idx := BuildIndexFromModulesOrdered(tc.ents, ModuleKeyByPkgDir)
			assertLocKindMergedMatchesLegacy(t, "BuildIndexFromModulesOrdered", idx, tc.ents)
		})
	}
}

// ---- 2. entry-point equivalence ---------------------------------------------

func TestLocKindMerge_EntryPointEquivalence(t *testing.T) {
	ents := locMergeFixture()
	for _, tc := range []struct {
		label string
		idx   Index
	}{
		{"BuildIndex", BuildIndex(ents)},
		{"BuildIndexFromModulesOrdered", BuildIndexFromModulesOrdered(ents, ModuleKeyByPkgDir)},
	} {
		idx := tc.idx
		base, real := legacyLocationKindTables(ents)
		families := [][]string{
			componentKindFamily, operationKindFamily, schemaKindFamily, nil, {},
		}

		for _, file := range probeFiles(ents) {
			for _, name := range probeNames(ents) {
				for _, fam := range families {
					gotID, gotOK := idx.lookupLocationKind(file, name, fam)
					wantID, wantOK := refLookupLocationKind(base, real, file, name, fam)
					if gotID != wantID || gotOK != wantOK {
						t.Fatalf("%s: lookupLocationKind(%q,%q,%v) = (%q,%v), legacy = (%q,%v)",
							tc.label, file, name, fam, gotID, gotOK, wantID, wantOK)
					}
				}

				for _, rk := range allHintKinds {
					fam := hintKinds(rk)
					gotID, gotOK := liveBareLocalityRealTier(idx, file, name, fam)
					wantID, wantOK := refBareLocalityRealTier(real, file, name, fam)
					if gotID != wantID || gotOK != wantOK {
						t.Fatalf("%s: lookupBareWithLocality real tier(%q,%q,%q) = (%q,%v), legacy = (%q,%v)",
							tc.label, file, name, rk, gotID, gotOK, wantID, wantOK)
					}
				}

				gotID, gotOK := liveBareLocalityScopeTier(idx, file, name)
				wantID, wantOK := refBareLocalityScopeTier(base, file, name)
				if gotID != wantID || gotOK != wantOK {
					t.Fatalf("%s: lookupBareWithLocality SCOPE tier(%q,%q) = (%q,%v), legacy = (%q,%v)",
						tc.label, file, name, gotID, gotOK, wantID, wantOK)
				}
			}
		}

		for _, name := range probeNames(ents) {
			gotID, gotOK := liveRealComponentScopeScan(idx, name)
			wantID, wantOK := refRealComponentScopeScan(base, name)
			if gotID != wantID || gotOK != wantOK {
				t.Fatalf("%s: lookupUniqueRealComponentByName scope scan(%q) = (%q,%v), legacy = (%q,%v)",
					tc.label, name, gotID, gotOK, wantID, wantOK)
			}

			// Full entry point, including the untouched nameKinds tier.
			gotID, gotOK = idx.lookupUniqueRealComponentByName(name)
			wantID, wantOK = func() (string, bool) {
				if name == "" {
					return "", false
				}
				if realBucket := idx.nameKinds[name].real; realBucket.len() > 0 {
					if id, ok := uniqueMatchInFamily(realBucket, componentKindFamily, false); ok {
						return id, true
					}
				}
				return refRealComponentScopeScan(base, name)
			}()
			if gotID != wantID || gotOK != wantOK {
				t.Fatalf("%s: lookupUniqueRealComponentByName(%q) = (%q,%v), legacy = (%q,%v)",
					tc.label, name, gotID, gotOK, wantID, wantOK)
			}

			// lookupUniqueSchemaFieldByName probes by LEAF name.
			leaf := name
			if d := strings.LastIndexByte(name, '.'); d >= 0 {
				leaf = name[d+1:]
			}
			for _, probe := range []string{name, leaf, "locField", "parentField", "nope"} {
				gotID, gotOK = idx.lookupUniqueSchemaFieldByName(probe)
				wantID, wantOK = refLookupUniqueSchemaFieldByName(base, probe)
				if gotID != wantID || gotOK != wantOK {
					t.Fatalf("%s: lookupUniqueSchemaFieldByName(%q) = (%q,%v), legacy = (%q,%v)",
						tc.label, probe, gotID, gotOK, wantID, wantOK)
				}
			}
		}
	}
}

// ---- 3. the #525 independent-blanking invariant -----------------------------

// TestLocKindMerge_IndependentBlanking525 proves the load-bearing invariant of
// the merge: .base and .real inside ONE inner-map value are blanked
// independently, so a SCOPE.Component placeholder colliding with a real
// Component at the SAME (file, name) destroys the id in .base but NOT in
// .real. If the two ever shared state (or .real were derived from .base by
// filtering SCOPE.*), the real id would be unrecoverable and the location-
// scoped EXTENDS bind would fall through to the placeholder.
func TestLocKindMerge_IndependentBlanking525(t *testing.T) {
	const realID, scopeID = "00000000000005a1", "00000000000005a2"
	const file = "app/widget.py"

	for label, idx := range map[string]Index{
		"flat":    BuildIndex(equivFixture()),
		"modules": BuildIndexFromModulesOrdered(equivFixture(), ModuleKeyByPkgDir),
	} {
		ent := idx.byLocationKind[file]["Widget"]

		// .base: "Component" is dual-indexed by BOTH the real Component (5a1)
		// and the SCOPE.Component's trimmed kind (5a2) => present-but-BLANK.
		if id, present := ent.base.get("Component"); !present || id != "" {
			t.Fatalf("%s: base[Component] = (%q,%v), want blank sentinel present — the real id must be UNRECOVERABLE from base", label, id, present)
		}
		// The real id is therefore only obtainable from .real.
		if id, present := ent.real.get("Component"); !present || id != realID {
			t.Fatalf("%s: real[Component] = (%q,%v), want (%s,true) — independent blanking broken", label, id, present, realID)
		}
		// The placeholder keeps its own untrimmed slot in .real; it must not
		// have blanked the real entity's slot.
		if id, present := ent.real.get("SCOPE.Component"); !present || id != scopeID {
			t.Fatalf("%s: real[SCOPE.Component] = (%q,%v), want (%s,true)", label, id, present, scopeID)
		}
		// End-to-end: the location-scoped tier-1 real pass must bind to the
		// real component, never the placeholder.
		if id, ok := idx.lookupLocationKind(file, "Widget", componentKindFamily); !ok || id != realID {
			t.Fatalf("%s: lookupLocationKind(%s,Widget,component) = (%q,%v), want (%s,true)", label, file, id, ok, realID)
		}
	}

	// Same shape on the increment-2 fixture's own single-file pair.
	for label, idx := range map[string]Index{
		"flat":    BuildIndex(locMergeFixture()),
		"modules": BuildIndexFromModulesOrdered(locMergeFixture(), ModuleKeyByPkgDir),
	} {
		ent := idx.byLocationKind["loc/w.py"]["LocWidget"]
		if id, present := ent.base.get("Component"); !present || id != "" {
			t.Fatalf("%s: LocWidget base[Component] = (%q,%v), want blank sentinel", label, id, present)
		}
		if id, present := ent.real.get("Component"); !present || id != "0000000000000b07" {
			t.Fatalf("%s: LocWidget real[Component] = (%q,%v), want (b07,true)", label, id, present)
		}
		if id, ok := idx.lookupLocationKind("loc/w.py", "LocWidget", componentKindFamily); !ok || id != "0000000000000b07" {
			t.Fatalf("%s: lookupLocationKind(loc/w.py,LocWidget,component) = (%q,%v), want (b07,true)", label, id, ok)
		}
	}
}

// TestLocKindMerge_CollisionSentinelBothFields asserts present-but-blank
// ("",true) stays distinguishable from absent ("",false) in BOTH fields of the
// merged inner value, and that the two-level presence bits behave as argued.
func TestLocKindMerge_CollisionSentinelBothFields(t *testing.T) {
	idx := BuildIndex(locMergeFixture())

	ent := idx.byLocationKind["loc/dup.java"]["SameFileDup"]
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

	// A file of only kind-less entities: the pre-merge byLocationKindReal had
	// NO outer key for it. After the merge the file key exists (it always did
	// in byLocationKind) and every .real bucket is empty — the shape every read
	// site treats identically to the old nil outer bucket.
	fb, ok := idx.byLocationKind["loc/kindless_only.py"]
	if !ok {
		t.Fatalf("kind-less-only file did not create a byLocationKind key")
	}
	for name, e := range fb {
		if e.base.len() != 0 || e.real.len() != 0 {
			t.Fatalf("loc/kindless_only.py[%q] = %+v, want both buckets empty", name, e)
		}
	}
	if _, ok := idx.lookupLocationKind("loc/kindless_only.py", "OnlyKindless", componentKindFamily); ok {
		t.Fatalf("lookupLocationKind on an all-empty entry resolved; want miss")
	}

	// Mixed presence at one (file, name): base carries the Class from the
	// kinded entity, real carries it too, and neither is blanked by the
	// kind-less sibling (which contributes no kind at all).
	mixed := idx.byLocationKind["loc/mixed.py"]["MixedPresence"]
	if id, present := mixed.base.get("Class"); !present || id != "0000000000000b04" {
		t.Fatalf("mixed base[Class] = (%q,%v), want (b04,true)", id, present)
	}
	if id, present := mixed.real.get("Class"); !present || id != "0000000000000b04" {
		t.Fatalf("mixed real[Class] = (%q,%v), want (b04,true)", id, present)
	}
}

// ---- 4. read-path benchmark -------------------------------------------------

// locBenchFixture builds a corpus-shaped entity set spread over many files so
// the location-keyed maps reach realistic cardinality.
func locBenchFixture(n int) []types.EntityRecord {
	return benchFixture(n)
}

func locBenchProbes(ents []types.EntityRecord) []struct{ file, name string } {
	out := make([]struct{ file, name string }, 0, 4096)
	seen := map[string]bool{}
	for _, e := range ents {
		k := e.SourceFile + "\x00" + e.Name
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, struct{ file, name string }{normalizePath(e.SourceFile), e.Name})
		if len(out) == 4096 {
			break
		}
	}
	return out
}

// BenchmarkLookupLocationKind_Merged measures the hot tier-1/tier-2 location
// read path over the merged inner map (one probe instead of two; map[k]struct
// copy-on-read is the one cost the merge adds).
func BenchmarkLookupLocationKind_Merged(b *testing.B) {
	ents := locBenchFixture(200000)
	idx := BuildIndex(ents)
	probes := locBenchProbes(ents)
	b.ReportAllocs()
	b.ResetTimer()
	var sink int
	for i := 0; i < b.N; i++ {
		p := probes[i%len(probes)]
		if _, ok := idx.lookupLocationKind(p.file, p.name, componentKindFamily); ok {
			sink++
		}
		if _, ok := idx.lookupLocationKind(p.file, p.name, operationKindFamily); ok {
			sink++
		}
	}
	_ = sink
}

// BenchmarkLookupLocationKind_LegacyTwoMap is the SAME read path against the
// pre-merge two-map layout, so the merge's read cost can be compared directly
// in one binary.
func BenchmarkLookupLocationKind_LegacyTwoMap(b *testing.B) {
	ents := locBenchFixture(200000)
	base, real := legacyLocationKindTables(ents)
	probes := locBenchProbes(ents)
	b.ReportAllocs()
	b.ResetTimer()
	var sink int
	for i := 0; i < b.N; i++ {
		p := probes[i%len(probes)]
		if _, ok := refLookupLocationKind(base, real, p.file, p.name, componentKindFamily); ok {
			sink++
		}
		if _, ok := refLookupLocationKind(base, real, p.file, p.name, operationKindFamily); ok {
			sink++
		}
	}
	_ = sink
}
