package main

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/quality"
)

// #6440: two overloads of one VB.NET member declared in one type in one file
// hashed to a single graph.EntityID, so the #4406 dedup path kept the FIRST
// record, dropped the rest, and unioned the dropped records' edges onto the
// survivor. Silently — that branch has no counter and no log.
//
// vbnet-mini/src/Win32Native.vb has carried the pair since the fixture landed:
// `StructPtr.Dispose(disposing As Boolean)` (:58) and `StructPtr.Dispose()`
// (:79). Nothing asserted it, so the defect was unobservable from the golden
// set and a fix would have landed unobservable too.
//
// This test is the observation. It asserts three things that a single
// assertion could not:
//
//  1. BOTH overloads exist, under DISTINCT names. This is the assertion that
//     fails before the fix and passes after; reverting declName's collision
//     pre-pass kills it.
//  2. The phantom self-CALLS is gone. `Dispose()` calls `Dispose(True)`, so
//     the merge minted a CALLS edge from the survivor to ITSELF — a method
//     that does not call itself.
//  3. A NON-colliding VB member's entity ID is BYTE-IDENTICAL to what it was
//     before the change. This is what makes #6440 option B (a
//     collision-ONLY discriminator) rather than option A (a discriminator on
//     every member): option A would move every VB member's id and force an
//     fbversion bump plus a global cross-language reindex. The id below was
//     recorded from a run at the PARENT commit of the declName change; if a
//     future change moves it, that is an identity migration and needs the
//     bump, not a re-record here.
const vbnetObjSizeStableID = "cfb4b4428aa5b24d"

func TestVBNetOverloadIdentity_6440(t *testing.T) {
	goldenDir, err := filepath.Abs(filepath.Join("..", "..", "internal", "quality", "golden"))
	if err != nil {
		t.Fatalf("resolve golden dir: %v", err)
	}
	fixtureDir := filepath.Join(goldenDir, "vbnet-mini")
	fix, err := quality.LoadFixture(fixtureDir)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	graphPath := filepath.Join(t.TempDir(), "graph.json")
	if err := Index(quality.SourceDir(fixtureDir), graphPath, fix.Name,
		nil /*skip*/, false /*pretty*/, false, /*jsonStats*/
		qualityIndexOptions()...); err != nil {
		t.Fatalf("index fixture: %v", err)
	}
	doc, err := loadDocument(graphPath)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}

	// --- 1. both overloads, distinct names ------------------------------
	byName := map[string]string{} // name -> id, SCOPE.Operation only
	var disposes []string
	for _, e := range doc.Entities {
		if e.Kind != "SCOPE.Operation" {
			continue
		}
		byName[e.Name] = e.ID
		if len(e.Name) >= 17 && e.Name[:17] == "StructPtr.Dispose" {
			disposes = append(disposes, e.Name+" @"+itoa(e.StartLine))
		}
	}
	sort.Strings(disposes)
	if _, ok := byName["StructPtr.Dispose"]; !ok {
		t.Errorf("the FIRST overload must keep the BARE name: byLocation "+
			"(internal/resolve/refs.go:404) is keyed (file, name) and retains a "+
			"bucket only when it is UNIQUE, so a split that leaves no bare-named "+
			"overload dangles every same-file CALLS/CONTAINS ref. got: %q", disposes)
	}
	if _, ok := byName["StructPtr.Dispose()"]; !ok {
		t.Errorf("the no-arg `Sub Dispose()` (Win32Native.vb:79) must be its OWN "+
			"entity, discriminated by its parameter-type list; #6440 merged it "+
			"into `Dispose(disposing As Boolean)` (:58). got: %q", disposes)
	}
	if len(disposes) != 2 {
		t.Errorf("want exactly 2 StructPtr.Dispose operations, got %d: %q", len(disposes), disposes)
	}

	// --- 2. no phantom self-CALLS ---------------------------------------
	idName := map[string]string{}
	for _, e := range doc.Entities {
		idName[e.ID] = e.Name
	}
	for _, r := range doc.Relationships {
		if r.Kind != "CALLS" || r.FromID != r.ToID {
			continue
		}
		if n := idName[r.FromID]; len(n) >= 17 && n[:17] == "StructPtr.Dispose" {
			t.Errorf("phantom self-CALLS on %s: `Dispose()` calls `Dispose(True)`, "+
				"which is a call to the OTHER overload; the #6440 merge turned it "+
				"into a method calling itself", n)
		}
	}

	// --- 3. a non-colliding member's id did NOT move ---------------------
	got := byName["StructPtr.ObjSize"]
	if got == "" {
		t.Fatalf("StructPtr.ObjSize missing entirely")
	}
	if got != vbnetObjSizeStableID {
		t.Errorf("StructPtr.ObjSize entity id moved: got %q want %q. "+
			"#6440 was fixed as option B — a discriminator on COLLIDING members "+
			"only — precisely so that non-colliding VB members keep their ids and "+
			"no fbversion bump / global reindex is needed.", got, vbnetObjSizeStableID)
	}

	// --- 4. the fixture still scores 100%, and no forbidden edge fires ----
	//
	// Absolute, not "the ratchet is green": scripts/quality/ratchet.py can
	// re-record its own floor, so a number recorded there does not survive a
	// revert. These do. The counts move only when expected.json is edited
	// deliberately, which is the point.
	rep := quality.Evaluate(fix, doc)
	if rep.EntityExpected != 22 || rep.EntityFound != 22 {
		var missing []string
		for _, r := range rep.EntityResults {
			if r.Expected.MustExist && !r.Found {
				missing = append(missing, r.Expected.Kind+" "+r.Expected.Name)
			}
		}
		sort.Strings(missing)
		t.Errorf("vbnet-mini must-have entity recall = %d/%d, want 22/22; missing: %q",
			rep.EntityFound, rep.EntityExpected, missing)
	}
	if rep.RelExpected != 33 || rep.RelFound != 33 {
		var missing []string
		for _, r := range rep.RelResults {
			if r.Expected.MustExist && !r.Found {
				missing = append(missing, r.Expected.FromName+" -"+r.Expected.Kind+"-> "+r.Expected.ToName)
			}
		}
		sort.Strings(missing)
		t.Errorf("vbnet-mini must-have relationship recall = %d/%d, want 33/33; missing: %q",
			rep.RelFound, rep.RelExpected, missing)
	}
	if n := len(rep.ForbiddenHits); n != 0 {
		var hits []string
		for _, h := range rep.ForbiddenHits {
			hits = append(hits, h.Expected.FromName+" -"+h.Expected.Kind+"-> "+h.Expected.ToName)
		}
		sort.Strings(hits)
		t.Errorf("%d forbidden relationship(s) fired: %q", n, hits)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
