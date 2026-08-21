package proto_test

import (
	"os"
	"path/filepath"
	"strings"
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

	// Collect every file-level CONTAINS edge in the whole extraction. #6422 is
	// about the TO side; the FROM side was fixed separately by #6518, which
	// re-homed these records from the CONTAINED entity onto the per-file
	// SCOPE.Component/file entity that owns them.
	var toMsg, toRPC int
	for _, r := range fileLevelContains(t, recs, "c.proto") {
		switch r.ToID {
		case msg.ID:
			toMsg++
		case rpc.ID:
			toRPC++
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
	// Since #6518 the edges are owned by the file entity, so the record they
	// hang off no longer names the target; the assertion is set equality on the
	// emitted ToIDs, which pins the ref FORM in both directions — a missing
	// edge and a wrong-form edge are reported separately.
	got := make(map[string]bool)
	for _, r := range fileLevelContains(t, entities, "c.proto") {
		got[r.ToID] = true
	}
	for name, ref := range want {
		if !got[ref] {
			t.Errorf("no file CONTAINS edge emitted for top-level %q in its "+
				"kind-appropriate ref form %q", name, ref)
		}
		delete(got, ref)
	}
	for ref := range got {
		t.Errorf("unexpected file CONTAINS edge to %q — every file-level target "+
			"must use the ref form of its own entity kind (#6422)", ref)
	}
}

// TestMessageTypeRef_UsesSlashSeparators_6422 pins the second half of #6422:
// messageTypeRef omitted the filepath.ToSlash that every
// extractor.Build*StructuralRef helper applies, so on Windows one entity had
// two spellings of its address.
//
// HOW THIS IS NOT VACUOUS, and why an earlier revision of this file wrongly
// claimed it had to be. The path is built with filepath.Join, so it is
// "a/b.proto" on Unix and "a\\b.proto" on Windows, while the EXPECTATION is
// the slash form on both. On Unix the assertion is identity-green; on Windows
// it fails without the fold and passes with it. Windows CI is not
// hypothetical: .github/workflows/test.yml:160 puts windows-latest in the
// pull_request matrix and .github/workflows/windows.yml:37 is a dedicated
// windows-latest job. A mutant dropping filepath.ToSlash therefore dies on
// the Windows leg — which is exactly where the defect lives.
//
// It covers both producers of the schema ref in one pass: the file → message
// CONTAINS this issue is about, and the rpc → message REFERENCES from #6419.
func TestMessageTypeRef_UsesSlashSeparators_6422(t *testing.T) {
	path := filepath.Join("a", "b.proto")
	entities := extract(t, path, collisionSrc)

	// The slash spelling is the ONLY spelling any schema-form ref may carry,
	// on every GOOS. Stated as a literal, not as filepath.ToSlash(path), so
	// the assertion cannot be satisfied by re-deriving the bug.
	wantMsg := "scope:schema:message:proto:a/b.proto:User"
	wantEnum := "scope:schema:message:proto:a/b.proto:Status"

	seen := map[string]int{}
	scanned := 0
	for i := range entities {
		for _, r := range entities[i].Relationships {
			if !strings.HasPrefix(r.ToID, "scope:schema:message:proto:") {
				continue
			}
			scanned++
			seen[r.ToID]++
			if strings.ContainsRune(r.ToID, os.PathSeparator) && os.PathSeparator != '/' {
				t.Errorf("schema ref %q carries the native separator %q; every "+
					"extractor.Build*StructuralRef helper applies filepath.ToSlash "+
					"and messageTypeRef must too (#6422)", r.ToID, os.PathSeparator)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no schema-form ref emitted — the separator assertion never ran")
	}
	if seen[wantMsg] == 0 {
		t.Errorf("no schema ref spelled %q; got %v", wantMsg, seen)
	}
	if seen[wantEnum] == 0 {
		t.Errorf("no schema ref spelled %q; got %v", wantEnum, seen)
	}
}
