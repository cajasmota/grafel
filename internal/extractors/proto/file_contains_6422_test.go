package proto_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #6422 — fileContainsRel addressed EVERY top-level entity through the
// operation form
//
//	scope:operation:method:proto:<file>:<Name>
//
// which is right for a service and wrong for a message or an enum. It only
// LOOKED right because a non-colliding name has nothing to collide with.
//
// graph.EntityID hashes (repo, kind, name, sourceFile) and NOT Subtype, and
// the operation ref addresses purely by (file, name), so for
//
//	message User { … }
//	service S { rpc User(User) returns (User); }
//
// the file → message CONTAINS ref resolved through operationKindFamily
// straight onto the SCOPE.Operation rpc. Two consequences, both silent:
// the edge became a duplicate of the existing service → rpc CONTAINS, and
// the MESSAGE was left with no inbound CONTAINS at all.
//
// The fix is kind-appropriate addressing, not duplicate suppression:
// message/enum take the schema form (messageTypeRef — the same address space
// #6419's type REFERENCES already use), service keeps the operation form.
//
// Every test below uses the COLLISION fixture on purpose. A fixture without a
// name collision passes with the bug fully present.
// ---------------------------------------------------------------------------

// collisionSrc is the minimal shape proto permits and the resolver cannot
// disambiguate under one address space: an rpc and a message of the same name
// in one file, plus a non-colliding enum for contrast.
const collisionSrc = `syntax = "proto3";

message User { string id = 1; }

enum Status { UNKNOWN = 0; }

service S {
  rpc User(User) returns (User);
}
`

// resolveProto6422 runs the fixture through the production pipeline the
// indexer uses — real extractor, real graph.EntityID, real BuildIndex, real
// ReferencesEmbedded — so the assertions below are about what actually lands
// in the graph, not about what the extractor intended.
func resolveProto6422(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	recs := extract(t, path, src)
	if len(recs) == 0 {
		t.Fatalf("extract %s: no records", path)
	}
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6422", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))
	return recs
}

func findRec6422(t *testing.T, recs []types.EntityRecord, kind, subtype, name string) *types.EntityRecord {
	t.Helper()
	for i := range recs {
		if recs[i].Kind == kind && recs[i].Subtype == subtype && recs[i].Name == name {
			return &recs[i]
		}
	}
	t.Fatalf("entity %s/%s %q not found", kind, subtype, name)
	return nil
}

// TestFileContains_MessageIsNotReparentedOntoTheRPC_6422 is the load-bearing
// assertion. It measures the RESOLVED endpoint of the file → message CONTAINS
// edge and requires it to be the message's own id.
//
// Before the fix the ToID resolved to the rpc's id — the message had zero
// inbound CONTAINS and the file's edge was a duplicate of service → rpc.
func TestFileContains_MessageIsNotReparentedOntoTheRPC_6422(t *testing.T) {
	recs := resolveProto6422(t, "c.proto", collisionSrc)

	msg := findRec6422(t, recs, "SCOPE.Schema", "message", "User")
	rpc := findRec6422(t, recs, "SCOPE.Operation", "endpoint", "User")
	if msg.ID == rpc.ID {
		t.Fatalf("message and rpc User share id %q — the fixture cannot distinguish them", msg.ID)
	}

	// Collect every file-anchored CONTAINS edge in the whole extraction.
	// fileContainsRel sets FromID to the file path (a separate, allow-listed
	// defect — see internal/extractors/file_anchored_rels_guard_test.go's
	// proto:fileContainsRel:CONTAINS entry). Leaving FromID EMPTY is NOT the
	// fix here: these edges are appended to the CONTAINED record, so an empty
	// FromID would make assembly stamp the message's own id and the edge
	// would die as a self-loop. #6422 is about the TO side.
	var toMsg, toRPC int
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "CONTAINS" || r.FromID != "c.proto" {
				continue
			}
			switch r.ToID {
			case msg.ID:
				toMsg++
			case rpc.ID:
				toRPC++
			}
		}
	}

	if toMsg != 1 {
		t.Errorf("file → message User CONTAINS edges = %d, want 1: the message is "+
			"addressed through the operation form and binds to the rpc instead (#6422)", toMsg)
	}
	if toRPC != 0 {
		t.Errorf("file → rpc User CONTAINS edges = %d, want 0: the file contains the "+
			"SERVICE, and the service contains the rpc; a file-level edge onto the rpc "+
			"is the reparented message edge (#6422)", toRPC)
	}
}

// TestFileContains_MessageHasInboundContains_6422 states the consequence in
// the terms the graph cares about: after resolution the message must have at
// least one inbound CONTAINS, from the FILE. Asserting merely that "some
// CONTAINS edge exists" would pass with the bug present, since the rpc's
// duplicate is a CONTAINS too.
func TestFileContains_MessageHasInboundContains_6422(t *testing.T) {
	recs := resolveProto6422(t, "c.proto", collisionSrc)
	msg := findRec6422(t, recs, "SCOPE.Schema", "message", "User")

	inbound := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "CONTAINS" && r.ToID == msg.ID {
				inbound++
			}
		}
	}
	if inbound == 0 {
		t.Errorf("message User has NO inbound CONTAINS after resolution — its file "+
			"containment was silently reparented onto the same-named rpc (#6422); id=%q", msg.ID)
	}
}

// TestFileContains_RefFormIsKindAppropriate_6422 pins the emitted wire format
// at the extractor boundary, so a regression is reported at the line that
// causes it rather than three layers downstream. message/enum → schema form,
// service → operation form.
func TestFileContains_RefFormIsKindAppropriate_6422(t *testing.T) {
	entities := extract(t, "c.proto", collisionSrc)

	want := map[string]string{
		"User":   msgTypeRef("c.proto", "User"),
		"Status": msgTypeRef("c.proto", "Status"),
		"S":      extractor.BuildOperationStructuralRef("proto", "c.proto", "S"),
	}
	seen := make(map[string]bool, len(want))

	for i := range entities {
		for _, r := range entities[i].Relationships {
			if r.Kind != "CONTAINS" || r.FromID != "c.proto" {
				continue
			}
			owner := entities[i].Name
			w, ok := want[owner]
			if !ok {
				t.Errorf("unexpected file CONTAINS edge from record %q: %+v", owner, r)
				continue
			}
			if r.ToID != w {
				t.Errorf("file CONTAINS → %s (%s/%s): ToID = %q, want %q",
					owner, entities[i].Kind, entities[i].Subtype, r.ToID, w)
			}
			seen[owner] = true
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("no file CONTAINS edge emitted for top-level %q", name)
		}
	}
}

// NOTE on the filepath.ToSlash alignment the issue also names. messageTypeRef
// now applies filepath.ToSlash like every extractor.Build*StructuralRef
// helper. There is deliberately NO test for it: filepath.ToSlash is the
// IDENTITY function on every non-Windows GOOS, so any assertion written here
// would pass identically with and without the change — vacuous on the only
// platform CI runs. It is a consistency change, graded by reading, not by a
// green test that proves nothing.
