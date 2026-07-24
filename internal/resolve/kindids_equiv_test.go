// Package resolve — equivalence battery for the kindIDs compaction (#5954).
//
// The compaction replaced the innermost map[string]string (kind -> id) buckets
// in nameKinds / nameKindsReal / byLocationKind / byLocationKindReal with the
// compact kindIDs. This file proves the swap is BEHAVIOURALLY INVISIBLE:
//
//  1. map-oracle parity — an INDEPENDENT replay of BuildIndex's exact write
//     rules over a real multi-language fixture reconstructs a reference
//     map[name]map[kind]id (and the 3-level location variants). We assert the
//     live kindIDs buckets enumerate an IDENTICAL (kind,id) set for every key.
//  2. entry-point battery — every method that READS these four buckets is
//     probed and asserted against known-correct output, including the #525
//     real-Component-vs-SCOPE.Component-placeholder collision (the gate case).
package resolve

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// ---- oracle replays (independent of kindIDs) --------------------------------

func kindsOf(e types.EntityRecord) []string {
	kinds := []string{e.Kind}
	if t := strings.TrimPrefix(e.Kind, scopeKindPrefix); t != e.Kind && t != "" {
		kinds = append(kinds, t)
	}
	return kinds
}

func setOracle(m map[string]string, kind, id string) {
	if kind == "" {
		return
	}
	if ex, ok := m[kind]; ok && ex != id {
		m[kind] = ""
	} else {
		m[kind] = id
	}
}

func oracleNameKinds(ents []types.EntityRecord) map[string]map[string]string {
	m := map[string]map[string]string{}
	for _, e := range ents {
		b := m[e.Name]
		if b == nil {
			b = map[string]string{}
			m[e.Name] = b
		}
		for _, k := range kindsOf(e) {
			setOracle(b, k, e.ID)
		}
	}
	return m
}

func oracleNameKindsReal(ents []types.EntityRecord) map[string]map[string]string {
	m := map[string]map[string]string{}
	for _, e := range ents {
		if e.Kind == "" {
			continue
		}
		b := m[e.Name]
		if b == nil {
			b = map[string]string{}
			m[e.Name] = b
		}
		setOracle(b, e.Kind, e.ID)
	}
	return m
}

func oracleLocKind(ents []types.EntityRecord, realOnly bool) map[string]map[string]map[string]string {
	m := map[string]map[string]map[string]string{}
	for _, e := range ents {
		sf := normalizePath(e.SourceFile)
		if sf == "" {
			continue
		}
		if realOnly && e.Kind == "" {
			continue
		}
		fb := m[sf]
		if fb == nil {
			fb = map[string]map[string]string{}
			m[sf] = fb
		}
		nb := fb[e.Name]
		if nb == nil {
			nb = map[string]string{}
			fb[e.Name] = nb
		}
		if realOnly {
			setOracle(nb, e.Kind, e.ID)
		} else {
			for _, k := range kindsOf(e) {
				setOracle(nb, k, e.ID)
			}
		}
	}
	return m
}

// liveNameKinds materialises a kindIDs map back into a plain map for comparison.
func liveNameKinds(m map[string]kindIDs) map[string]map[string]string {
	out := map[string]map[string]string{}
	for name, kid := range m {
		b := map[string]string{}
		kid.each(func(k, id string) { b[k] = id })
		out[name] = b
	}
	return out
}

func liveLocKind(m LocationKindIndex) map[string]map[string]map[string]string {
	out := map[string]map[string]map[string]string{}
	for file, fb := range m {
		of := map[string]map[string]string{}
		for name, kid := range fb {
			b := map[string]string{}
			kid.each(func(k, id string) { b[k] = id })
			of[name] = b
		}
		out[file] = of
	}
	return out
}

// ---- fixture ----------------------------------------------------------------

// equivFixture = the multi-language representativeFixture() PLUS the #525
// real-Component-vs-SCOPE.Component collision and a schema-field entity, so the
// oracle parity covers every bucket-populating shape.
func equivFixture() []types.EntityRecord {
	ents := representativeFixture()
	ents = append(ents,
		// #525 gate: real Component "Widget" AND SCOPE.Component "Widget"
		// share the SAME (file, name). The real one must win tier-1.
		entAt("00000000000005a1", "Component", "Widget", "app/widget.py"),
		entAt("00000000000005a2", "SCOPE.Component", "Widget", "app/widget.py"),
		// Force ambigName on Widget so the bare/global path can't resolve it,
		// exercising the nameKindsReal tier-1 preference.
		entAt("00000000000005a3", "Function", "Widget", "other/w.py"),
		// A unique schema field for lookupUniqueSchemaFieldByName.
		entAt("00000000000006f1", "SCOPE.Schema", "Base.parentField", "app/base.java"),
		// A unique Model for lookupUniqueModelByName (Django strategy-3).
		entAt("00000000000007d1", string(types.EntityKindModel), "Building", "app/models.py"),
	)
	return ents
}

// ---- 1. map-oracle parity ---------------------------------------------------

func TestKindIDs_IndexMapParity(t *testing.T) {
	ents := equivFixture()
	idx := BuildIndex(ents)

	if got, want := liveNameKinds(idx.nameKinds), oracleNameKinds(ents); !reflect.DeepEqual(got, want) {
		t.Fatalf("nameKinds diverges from oracle:\n live=%v\noracle=%v", got, want)
	}
	if got, want := liveNameKinds(idx.nameKindsReal), oracleNameKindsReal(ents); !reflect.DeepEqual(got, want) {
		t.Fatalf("nameKindsReal diverges from oracle:\n live=%v\noracle=%v", got, want)
	}
	if got, want := liveLocKind(idx.byLocationKind), oracleLocKind(ents, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("byLocationKind diverges from oracle:\n live=%v\noracle=%v", got, want)
	}
	if got, want := liveLocKind(idx.byLocationKindReal), oracleLocKind(ents, true); !reflect.DeepEqual(got, want) {
		t.Fatalf("byLocationKindReal diverges from oracle:\n live=%v\noracle=%v", got, want)
	}
}

// ---- 2. entry-point battery -------------------------------------------------

func TestKindIDs_EntryPointBattery(t *testing.T) {
	idx := BuildIndex(equivFixture())

	// lookupByKindHint via LookupStatusHint — #525 gate: EXTENDS "Widget"
	// must bind to the REAL Component (5a1), never the SCOPE.Component
	// placeholder (5a2), even though Widget is ambiguous globally.
	if id, st := idx.LookupStatusHint("Widget", "EXTENDS"); id != "00000000000005a1" || st != statusRewritten {
		t.Fatalf("#525 lookupByKindHint(Widget,EXTENDS) = (%q,%d), want (5a1,rewritten)", id, st)
	}

	// lookupLocationKind — same-file (file,name) kind-disambiguated real
	// preference (#525): structural EXTENDS to the class in its own file.
	if id, ok := idx.lookupLocationKind("app/widget.py", "Widget", componentKindFamily); !ok || id != "00000000000005a1" {
		t.Fatalf("#525 lookupLocationKind = (%q,%v), want (5a1,true)", id, ok)
	}

	// lookupStructural — Format A structural ref resolving to the real class.
	stub := "scope:component:class:python:app/widget.py:Widget"
	if id, st, handled := idx.lookupStructural(stub); !handled || id != "00000000000005a1" || st != statusRewritten {
		t.Fatalf("#525 lookupStructural(%s) = (%q,%d,handled=%v), want (5a1,rewritten,true)", stub, id, st, handled)
	}

	// lookupUniqueRealComponentByName — unique real component by bare name.
	if id, ok := idx.lookupUniqueRealComponentByName("Widget"); !ok || id != "00000000000005a1" {
		t.Fatalf("lookupUniqueRealComponentByName(Widget) = (%q,%v), want (5a1,true)", id, ok)
	}

	// lookupUniqueSchemaFieldByName — unique SCOPE.Schema leaf.
	if id, ok := idx.lookupUniqueSchemaFieldByName("parentField"); !ok || id != "00000000000006f1" {
		t.Fatalf("lookupUniqueSchemaFieldByName(parentField) = (%q,%v), want (6f1,true)", id, ok)
	}

	// lookupUniqueModelByName — unique SCOPE.Model by bare name (Django).
	if id, ok := idx.lookupUniqueModelByName("Building"); !ok || id != "00000000000007d1" {
		t.Fatalf("lookupUniqueModelByName(Building) = (%q,%v), want (7d1,true)", id, ok)
	}

	// nameExists — must find every name that has any kind bucket.
	for _, n := range []string{"Widget", "Run", "Config", "Building", "Base.parentField"} {
		if !idx.nameExists(n) {
			t.Fatalf("nameExists(%q) = false, want true", n)
		}
	}
	if idx.nameExists("NoSuchNameEver") {
		t.Fatalf("nameExists(absent) = true, want false")
	}

	// DiagnoseBugResolver — KindsPresent must enumerate the kinds registered
	// under a name (drawn from the nameKinds bucket via each()).
	diag := idx.DiagnoseBugResolver("Widget:Widget", "EXTENDS")
	kp := map[string]bool{}
	for _, k := range diag.KindsPresent {
		kp[k] = true
	}
	for _, want := range []string{"Component", "SCOPE.Component", "Function"} {
		if !kp[want] {
			t.Fatalf("DiagnoseBugResolver KindsPresent missing %q; got %v", want, diag.KindsPresent)
		}
	}
}

// TestKindIDs_CollisionSentinelPreserved proves that a genuine (name,kind)
// collision blanks to the "" sentinel and that a hint over that name yields no
// false bind — the ambiguity semantics the resolver depends on.
func TestKindIDs_CollisionSentinelPreserved(t *testing.T) {
	// Two DIFFERENT entities share (name=Dup, kind=Function) => blank sentinel.
	ents := []types.EntityRecord{
		ent("aaaaaaaaaaaaaaaa", "Function", "Dup"),
		ent("bbbbbbbbbbbbbbbb", "Function", "Dup"),
	}
	idx := BuildIndex(ents)
	// nameKinds[Dup][Function] must be present-but-blank.
	id, present := idx.nameKinds["Dup"].get("Function")
	if !present {
		t.Fatalf("collision entry absent; want present blank sentinel")
	}
	if id != "" {
		t.Fatalf("collision entry = %q, want blank sentinel", id)
	}
	// A CALLS hint over the ambiguous name must NOT bind.
	if bindID, st := idx.LookupStatusHint("Dup", "CALLS"); st == statusRewritten {
		t.Fatalf("ambiguous Dup bound to %q (status rewritten); want no bind", bindID)
	}
}
