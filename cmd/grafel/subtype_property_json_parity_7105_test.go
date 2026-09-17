package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// #7105 — graph.json and graph.fb must agree about Properties["subtype"].
//
// THIS IS THE ROW THAT MAKES THE DERIVATION'S OWN DOCSTRING TRUE BY TEST.
// graph.json is written by graph.WriteAtomic, which normalises NOTHING and
// never goes near fbwriter; it carries the derived key only because some
// normaliser has already mutated the SHARED doc pointer in place. Nothing else
// in the repo exercises the JSON encoding of this field, and the agreement
// between the two encodings is exactly the reason fbwriter.buildEntity (the
// .fb-only leaf) was rejected as the chokepoint.
//
// The two normalisation sites are MUTUALLY MASKING, so do not read a green run
// here as evidence that either one is load-bearing: deleting index.go:999
// alone leaves this test green (mutant F), deleting both fbwriter calls alone
// leaves it green (mutant G), and deleting both fails it (mutant H). It is the
// compound that this test grades — which is precisely what nothing observed
// before it existed.
//
// End-to-end through the real indexer over a real fixture, asserting on the
// two emitted artefacts, with a non-vacuity floor so the walk cannot pass by
// finding nothing.
func TestGraphJSONAndFBAgreeOnDerivedSubtype_7105(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "graph.json")

	if err := Index("testdata/crossfile_go", outPath, "test-repo", nil, false, false,
		WithExportJSON(true)); err != nil {
		t.Fatalf("#7105: Index with --export-json: %v", err)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("#7105: graph.json not readable: %v", err)
	}
	var jsonDoc graph.Document
	if err := json.Unmarshal(raw, &jsonDoc); err != nil {
		t.Fatalf("#7105: graph.json does not decode: %v", err)
	}
	fbDoc, err := graph.LoadGraphFromDir(tmp)
	if err != nil {
		t.Fatalf("#7105: graph.fb not loadable: %v", err)
	}

	// --- graph.json carries the derived key, on its own, for every entity
	//     with a canonical Subtype ---
	withSubtype, derived := 0, 0
	for i := range jsonDoc.Entities {
		e := &jsonDoc.Entities[i]
		if e.Subtype == "" {
			if v, ok := e.PropLookup("subtype"); ok {
				t.Fatalf("#7105 FORBIDDEN: graph.json entity %s has an EMPTY canonical Subtype but carries properties[\"subtype\"] = %q", e.ID, v)
			}
			continue
		}
		withSubtype++
		got, ok := e.PropLookup("subtype")
		if !ok {
			t.Fatalf("#7105: graph.json entity %s has Subtype %q but NO properties[\"subtype\"]. graph.WriteAtomic normalises nothing, so this key is present only because the producer normalised the shared doc before either encoder ran (cmd/grafel/index.go:999)", e.ID, e.Subtype)
		}
		if got != e.Subtype {
			t.Fatalf("#7105: graph.json entity %s: properties[\"subtype\"] = %q, canonical Subtype = %q", e.ID, got, e.Subtype)
		}
		derived++
	}
	if withSubtype == 0 {
		t.Fatalf("#7105: NON-VACUITY FLOOR: the crossfile_go fixture produced 0 entities with a non-empty Subtype, so this test asserted nothing. %d entities in graph.json", len(jsonDoc.Entities))
	}

	// --- the two encodings agree, entity for entity ---
	fbSubtypeProp := make(map[string]string, len(fbDoc.Entities))
	fbHasProp := make(map[string]bool, len(fbDoc.Entities))
	for i := range fbDoc.Entities {
		e := &fbDoc.Entities[i]
		if v, ok := e.PropLookup("subtype"); ok {
			fbSubtypeProp[e.ID] = v
			fbHasProp[e.ID] = true
		}
	}
	for i := range jsonDoc.Entities {
		e := &jsonDoc.Entities[i]
		jv, jok := e.PropLookup("subtype")
		fv, fok := fbSubtypeProp[e.ID], fbHasProp[e.ID]
		if jok != fok || jv != fv {
			t.Fatalf("#7105: the two encodings of the SAME index pass disagree for entity %s: graph.json (present=%t, %q) vs graph.fb (present=%t, %q). These files are deliberately mtime-matched as one pass (#1626)", e.ID, jok, jv, fok, fv)
		}
	}

	t.Logf("#7105: %d/%d graph.json entities carry a canonical Subtype; all %d agree with graph.fb",
		withSubtype, len(jsonDoc.Entities), derived)
}
