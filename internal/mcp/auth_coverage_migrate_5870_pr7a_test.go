// auth_coverage_migrate_5870_pr7a_test.go — deretain-flip PR7a (#5870).
//
// Result-equivalence for the auth-coverage builders, migrated from taking
// *graph.Document (raw doc.Entities/Relationships iteration) to taking
// *LoadedRepo and iterating via forEachEntityBase/forEachRelationship. Each
// asserts the migrated builder on a flag-ON emptied-Doc repo (Reader-sourced)
// is byte-identical to the flag-OFF full-Doc result, plus retired-Reader
// Doc fallback.
package mcp

import (
	"reflect"
	"testing"
)

func TestAuthBuilders_ReaderParity_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)

	type authOut struct {
		policies map[string][]string
		tagged   map[string]bool
		drf      map[string][]drfClassAuth
		defProt  bool
		defEv    string
	}
	compute := func(lr *LoadedRepo) authOut {
		prot, ev := repoDRFDefaultPolicy(lr)
		return authOut{
			policies: buildAuthPoliciesByFile(lr),
			tagged:   buildTaggedAuthIDs(lr),
			drf:      buildDRFClassAuthByFile(lr),
			defProt:  prot,
			defEv:    ev,
		}
	}

	withServeFromMMap(t, false)
	want := compute(docFullRepo(doc))
	// Fixture must exercise the builders.
	if len(want.policies) == 0 {
		t.Fatal("fixture must contain an auth_policy entity")
	}
	if !want.tagged["ep"] {
		t.Fatal("fixture must contain a TAGGED_AS edge to the auth policy")
	}
	if len(want.drf) == 0 || !want.defProt {
		t.Fatal("fixture must exercise DRF class-auth + default policy")
	}

	withServeFromMMap(t, true)
	got := compute(readerEmptiedRepo(t, doc, r))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("auth builders flag-ON(emptied Doc) != flag-OFF\n got=%#v\nwant=%#v", got, want)
	}

	if fb := compute(readerFullRepoRetired(t, doc, r)); !reflect.DeepEqual(fb, want) {
		t.Fatalf("auth builders retired-Reader fallback != flag-OFF")
	}
}
