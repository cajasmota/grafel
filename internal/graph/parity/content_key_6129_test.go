// Package parity — content_key_6129_test.go
//
// Direct coverage for Options.ContentKeyedIdentity (#6129): the endpoint keying
// that makes a REBOUND edge — one that resolves to a different entity rather
// than disappearing — visible as a LOST/INVENTED pair naming both targets.
//
// The end-to-end gate lives in cmd/grafel/incremental_content_parity_6129_test.go;
// these cases pin the comparator behaviour it depends on, independently of the
// indexer.
package parity

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func ckEnt(id, kind, name, file string) graph.Entity {
	return graph.Entity{ID: id, Kind: kind, Name: name, SourceFile: file}
}

func ckRel(from, to, kind string) graph.Relationship {
	return graph.Relationship{ID: graph.RelationshipID(from, to, kind), FromID: from, ToID: to, Kind: kind}
}

// ckDoc builds a document with three entities and one CALLS edge from `caller`
// to whichever of the two targets `to` names.
func ckDoc(to string) *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			ckEnt("aaaaaaaaaaaaaaa1", "SCOPE.Operation", "caller", "caller.go"),
			ckEnt("aaaaaaaaaaaaaaa2", "SCOPE.Operation", "wanted", "wanted.go"),
			ckEnt("aaaaaaaaaaaaaaa3", "SCOPE.External", "wanted", ""),
		},
		Relationships: []graph.Relationship{ckRel("aaaaaaaaaaaaaaa1", to, "CALLS")},
	}
}

// TestContentKeyedIdentity_RebindIsLostPlusInvented_6129 is the core case: both
// graphs hold exactly one CALLS edge from the same source, both endpoints
// resolve to a real entity, and both graphs have identical entity and edge
// counts and zero dangling endpoints. Only the BOUND TARGET differs.
//
// This is the shape #6037/#6094/#6123/#6129 all shared and that every aggregate
// metric reports as healthy.
func TestContentKeyedIdentity_RebindIsLostPlusInvented_6129(t *testing.T) {
	full := ckDoc("aaaaaaaaaaaaaaa2") // binds the real in-repo operation
	inc := ckDoc("aaaaaaaaaaaaaaa3")  // binds the SCOPE.External placeholder

	rep := CompareWithOptions(full, inc, Options{ContentKeyedIdentity: true})
	if rep.Equivalent {
		t.Fatalf("a rebound edge compared EQUIVALENT — the comparator is blind to the "+
			"exact divergence class #6129 is about:\n%s", rep.String())
	}
	if len(rep.RelsOnlyInA) != 1 || len(rep.RelsOnlyInB) != 1 {
		t.Fatalf("want exactly one LOST and one INVENTED edge, got %d/%d:\n%s",
			len(rep.RelsOnlyInA), len(rep.RelsOnlyInB), rep.String())
	}
	// The diff must NAME both targets, not print two hex ids.
	if !strings.Contains(rep.RelsOnlyInA[0], "SCOPE.Operation/wanted@wanted.go") {
		t.Errorf("LOST key does not name the full rebuild's target: %q", rep.RelsOnlyInA[0])
	}
	if !strings.Contains(rep.RelsOnlyInB[0], "SCOPE.External/wanted@") {
		t.Errorf("INVENTED key does not name the incremental result's target: %q", rep.RelsOnlyInB[0])
	}
}

// TestContentKeyedIdentity_UnboundEndpointIsDistinctFromBound_6129 pins that an
// endpoint binding to NOTHING never compares equal to one binding to a real
// entity — the missed-bind half of the divergence set, kept distinct from the
// mis-bind half above.
func TestContentKeyedIdentity_UnboundEndpointIsDistinctFromBound_6129(t *testing.T) {
	full := ckDoc("aaaaaaaaaaaaaaa2")
	inc := ckDoc("some.dotted.stub") // verbatim stub, resolves to nothing

	rep := CompareWithOptions(full, inc, Options{ContentKeyedIdentity: true})
	if rep.Equivalent {
		t.Fatalf("an unresolved endpoint compared EQUIVALENT to a resolved one:\n%s", rep.String())
	}
	if len(rep.RelsOnlyInB) != 1 || !strings.Contains(rep.RelsOnlyInB[0], "«unbound»some.dotted.stub") {
		t.Fatalf("INVENTED key should carry the verbatim unbound endpoint, got %v:\n%s",
			rep.RelsOnlyInB, rep.String())
	}
}

// TestContentKeyedIdentity_IdenticalGraphsAreEquivalent_6129 is the
// false-positive guard: the option must not manufacture divergences. Without
// this, the two cases above could be satisfied by a comparator that reports
// everything as different.
func TestContentKeyedIdentity_IdenticalGraphsAreEquivalent_6129(t *testing.T) {
	rep := CompareWithOptions(ckDoc("aaaaaaaaaaaaaaa2"), ckDoc("aaaaaaaaaaaaaaa2"),
		Options{ContentKeyedIdentity: true})
	if !rep.Equivalent {
		t.Fatalf("identical graphs compared NON-equivalent under content keying:\n%s", rep.String())
	}
}

// TestContentKeyedIdentity_IsMultiset_6129 confirms the #6037 multiset property
// survives content keying: a duplicated edge is not folded into its twin by the
// change of key.
func TestContentKeyedIdentity_IsMultiset_6129(t *testing.T) {
	full := ckDoc("aaaaaaaaaaaaaaa2")
	inc := ckDoc("aaaaaaaaaaaaaaa2")
	inc.Relationships = append(inc.Relationships, inc.Relationships[0])

	rep := CompareWithOptions(full, inc, Options{ContentKeyedIdentity: true})
	if rep.Equivalent || len(rep.RelMultiplicityDiffs) != 1 {
		t.Fatalf("a duplicated edge was not reported as a multiplicity divergence "+
			"(equivalent=%v, mult diffs=%d):\n%s",
			rep.Equivalent, len(rep.RelMultiplicityDiffs), rep.String())
	}
}

// TestContentKeyedIdentity_IsOptIn_6129 pins that the new option changes nothing
// by default: the pre-existing id-keyed behaviour is untouched, so every
// existing caller of Compare / CompareWithOptions is unaffected.
func TestContentKeyedIdentity_IsOptIn_6129(t *testing.T) {
	full := ckDoc("aaaaaaaaaaaaaaa2")
	inc := ckDoc("aaaaaaaaaaaaaaa3")

	rep := Compare(full, inc)
	if rep.Equivalent {
		t.Fatalf("default (id-keyed) comparison should still see the rebind as a diff:\n%s", rep.String())
	}
	// ...but it reports it in raw hex, which is the legibility gap the option closes.
	if len(rep.RelsOnlyInA) != 1 || !strings.Contains(rep.RelsOnlyInA[0], "aaaaaaaaaaaaaaa2") {
		t.Fatalf("default keying should report the verbatim endpoint id, got %v", rep.RelsOnlyInA)
	}
}
