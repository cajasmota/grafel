package types_test

// endpoint_bare_kind_6776_test.go — #6776 arm B9, the last arm of the B series.
//
// Bare `Endpoint` enters types.AllEntityKinds() as a DISTINCT KIND, not as a
// synonym of SCOPE.Endpoint. That distinction is the whole content of this
// arm, so it is asserted rather than described:
//
//   - It is a produced kind. internal/engine/rules/javascript_typescript/
//     frameworks/electron.yaml:41,46,52 declare it (ipcMain / ipcRenderer /
//     contextBridge) and internal/engine/detector.go writes the declared
//     string verbatim into EntityRecord.Kind, so it reaches the graph exactly
//     as spelled. #6776 exists to make produced kinds valid; this is the last
//     produced kind that was not.
//   - It is NOT the HTTP concept. #6820 was decided (2026-09-06) by fixing the
//     ten consumer sites that keyed on the bare spelling, with no rename and no
//     stored-graph migration: the HTTP panes key on SCOPE.Endpoint, and bare
//     `Endpoint` keeps its spelling as the Electron IPC kind. Membership in the
//     entity-kind vocabulary must therefore NOT drag it into the HTTP
//     vocabulary.
//
// Varies: the SPELLING (bare vs SCOPE.-prefixed) and the VOCABULARY the
// spelling is tested against (entity kinds vs HTTP endpoint kinds).
// Holds constant: both validators, and the fact that both spellings name a
// really-produced kind — so nothing but the spelling/vocabulary pair can move
// an answer.
//
// The rejecting half is the direction that can go wrong silently, and it is
// deliberately duplicated from http_endpoint_kind_forbidden_6894_test.go
// rather than left to it. That file grades IsHTTPEndpointKind's rejections in
// general; this one grades the SPECIFIC hazard this arm introduces — that
// making a kind valid is mistaken for making it an HTTP kind — and names the
// arm in its failure text, so the reader is sent to the right decision.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// TestEndpointBare6776_IsADistinctValidEntityKind
func TestEndpointBare6776_IsADistinctValidEntityKind(t *testing.T) {
	const bare, prefixed = "Endpoint", "SCOPE.Endpoint"

	if got := string(types.EntityKindEndpointBare); got != bare {
		t.Fatalf("EntityKindEndpointBare = %q, want %q", got, bare)
	}
	if got := string(types.EntityKindEndpoint); got != prefixed {
		t.Fatalf("EntityKindEndpoint = %q, want %q — the pair this arm keeps apart", got, prefixed)
	}
	if bare == prefixed {
		t.Fatal("fixture is inert: the two spellings must differ, or 'distinct member' means nothing")
	}

	if !types.IsValidEntityKind(bare) {
		t.Errorf("IsValidEntityKind(%q) = false. electron.yaml:41,46,52 declare it and "+
			"detector.go writes it verbatim into EntityRecord.Kind, so it reaches the graph as a "+
			"kind the vocabulary rejects — the exact condition #6776 exists to end.", bare)
	}
	if !types.IsValidEntityKind(prefixed) {
		t.Errorf("IsValidEntityKind(%q) = false; the prefixed HTTP kind must stay valid, or the "+
			"'two distinct members' claim below is half-vacuous", prefixed)
	}

	// BOTH must be present as separate roster elements. A "fix" that dropped
	// one spelling in favour of the other would satisfy IsValidEntityKind for
	// the survivor and silently orphan every stored graph carrying the other —
	// the rename #6820 and #6776 both rejected.
	var sawBare, sawPrefixed int
	for _, k := range types.AllEntityKinds() {
		switch string(k) {
		case bare:
			sawBare++
		case prefixed:
			sawPrefixed++
		}
	}
	if sawBare != 1 || sawPrefixed != 1 {
		t.Errorf("AllEntityKinds() holds %q %d time(s) and %q %d time(s); want exactly one each — "+
			"they are two members naming two concepts (Electron IPC channel, HTTP entrypoint), "+
			"not one member with two spellings", bare, sawBare, prefixed, sawPrefixed)
	}
}

// TestEndpointBare6776_MembershipIsNotHTTPMembership is the forbidden
// direction, which is the invisible one: a test showing bare `Endpoint` is now
// a valid entity kind cannot see that it also became an HTTP endpoint kind, and
// that would silently undo #6893's repoint of the ten dashboard/OpenAPI
// consumer sites.
func TestEndpointBare6776_MembershipIsNotHTTPMembership(t *testing.T) {
	const bare = "Endpoint"
	if !types.IsValidEntityKind(bare) {
		t.Fatalf("fixture is inert: %q is not a valid entity kind, so this test is not observing "+
			"the hazard this arm introduces", bare)
	}
	// Positive control: the helper must accept something, or the rejection
	// below is satisfied by a function that returns false for everything.
	if !types.IsHTTPEndpointKind(string(types.EntityKindHTTPEndpointDefinition)) {
		t.Fatalf("fixture is inert: IsHTTPEndpointKind rejects %q, so its rejections say nothing",
			types.EntityKindHTTPEndpointDefinition)
	}
	if types.IsHTTPEndpointKind(bare) {
		t.Errorf("IsHTTPEndpointKind(%q) = true. #6776 arm B9 makes this kind a valid ENTITY "+
			"kind; it is an Electron IPC channel and never had a URL. #6820's ruling put the "+
			"HTTP panes on SCOPE.Endpoint precisely so this string stops being read as HTTP — "+
			"admitting it here re-exports IPC channels into the OpenAPI document.", bare)
	}
}
