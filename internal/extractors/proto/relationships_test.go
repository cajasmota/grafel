package proto_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/proto"
	"github.com/cajasmota/grafel/internal/types"
)

// extract is a small helper that parses src and runs the proto extractor.
func extract(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("proto")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "proto",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return entities
}

func relsByKind(entities []types.EntityRecord, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == kind {
				out = append(out, r)
			}
		}
	}
	return out
}

// ---- IMPORTS ---------------------------------------------------------------

func TestRelationships_Imports_Single(t *testing.T) {
	src := `syntax = "proto3";
import "google/protobuf/empty.proto";

message Foo { string id = 1; }
`
	entities := extract(t, "foo.proto", src)
	imports := relsByKind(entities, "IMPORTS")
	// buildRPC also emits IMPORTS; this fixture has no rpc, so all IMPORTS are file-level.
	if len(imports) != 1 {
		t.Fatalf("IMPORTS count = %d, want 1; got %+v", len(imports), imports)
	}
	if imports[0].FromID != "foo.proto" {
		t.Errorf("IMPORTS FromID = %q, want foo.proto", imports[0].FromID)
	}
	if imports[0].ToID != "google/protobuf/empty.proto" {
		t.Errorf("IMPORTS ToID = %q, want google/protobuf/empty.proto", imports[0].ToID)
	}
}

func TestRelationships_Imports_Public(t *testing.T) {
	src := `syntax = "proto3";
import public "common.proto";

message Foo { string id = 1; }
`
	entities := extract(t, "foo.proto", src)
	imports := relsByKind(entities, "IMPORTS")
	if len(imports) != 1 {
		t.Fatalf("IMPORTS count = %d, want 1", len(imports))
	}
	if imports[0].ToID != "common.proto" {
		t.Errorf("IMPORTS ToID = %q, want common.proto", imports[0].ToID)
	}
	if imports[0].Properties["public"] != "true" {
		t.Errorf("IMPORTS Properties[public] = %q, want true", imports[0].Properties["public"])
	}
}

func TestRelationships_Imports_Multiple(t *testing.T) {
	src := `syntax = "proto3";
import "a.proto";
import public "b.proto";
import "c.proto";

message Foo { string id = 1; }
`
	entities := extract(t, "foo.proto", src)
	imports := relsByKind(entities, "IMPORTS")
	if len(imports) != 3 {
		t.Fatalf("IMPORTS count = %d, want 3", len(imports))
	}
	wants := map[string]bool{"a.proto": false, "b.proto": false, "c.proto": false}
	for _, r := range imports {
		if _, ok := wants[r.ToID]; ok {
			wants[r.ToID] = true
		}
	}
	for k, seen := range wants {
		if !seen {
			t.Errorf("missing IMPORTS edge to %q", k)
		}
	}
}

// ---- CONTAINS: file → top-level ------------------------------------------

func TestRelationships_FileContains_ServiceMessageEnum(t *testing.T) {
	src := `syntax = "proto3";

service S { rpc R(M) returns (M); }
message M { string id = 1; }
enum E { Z = 0; }
`
	entities := extract(t, "x.proto", src)
	contains := relsByKind(entities, "CONTAINS")

	wantFileEdges := map[string]bool{
		"scope:operation:method:proto:x.proto:S": false,
		"scope:operation:method:proto:x.proto:M": false,
		"scope:operation:method:proto:x.proto:E": false,
	}
	for _, r := range contains {
		if r.FromID == "x.proto" {
			if _, ok := wantFileEdges[r.ToID]; ok {
				wantFileEdges[r.ToID] = true
			}
		}
	}
	for ref, seen := range wantFileEdges {
		if !seen {
			t.Errorf("missing file CONTAINS edge to %q", ref)
		}
	}
}

// ---- CONTAINS: service → rpc ---------------------------------------------

func TestRelationships_ServiceContainsRPC(t *testing.T) {
	src := `syntax = "proto3";

service UserService {
  rpc GetUser(Req) returns (Resp);
  rpc CreateUser(Req) returns (Resp);
}
`
	entities := extract(t, "u.proto", src)
	var serviceRec *types.EntityRecord
	for i := range entities {
		if entities[i].Subtype == "service" {
			serviceRec = &entities[i]
		}
	}
	if serviceRec == nil {
		t.Fatal("service entity not found")
	}
	wantRefs := map[string]bool{
		"scope:operation:method:proto:u.proto:GetUser":    false,
		"scope:operation:method:proto:u.proto:CreateUser": false,
	}
	for _, r := range serviceRec.Relationships {
		if r.Kind != "CONTAINS" {
			continue
		}
		if _, ok := wantRefs[r.ToID]; ok {
			wantRefs[r.ToID] = true
		}
	}
	for ref, seen := range wantRefs {
		if !seen {
			t.Errorf("service missing CONTAINS edge to %q", ref)
		}
	}
}

// ---- CONTAINS: message → field -------------------------------------------

func TestRelationships_MessageContainsFields(t *testing.T) {
	src := `syntax = "proto3";

message User {
  string id = 1;
  repeated string tags = 2;
  Status status = 3;
}
`
	entities := extract(t, "u.proto", src)
	var msgRec *types.EntityRecord
	for i := range entities {
		if entities[i].Subtype == "message" && entities[i].Name == "User" {
			msgRec = &entities[i]
		}
	}
	if msgRec == nil {
		t.Fatal("message entity not found")
	}
	wantFields := map[string]bool{"id": false, "tags": false, "status": false}
	for _, r := range msgRec.Relationships {
		if r.Kind != "CONTAINS" {
			continue
		}
		// ToID is a structural-ref ending with the field name
		for f := range wantFields {
			if strings.HasSuffix(r.ToID, ":User#"+f) {
				wantFields[f] = true
			}
		}
	}
	for f, seen := range wantFields {
		if !seen {
			t.Errorf("message User missing CONTAINS edge for field %q", f)
		}
	}
}

// ---- CONTAINS: enum → value -----------------------------------------------

func TestRelationships_EnumContainsValues(t *testing.T) {
	src := `syntax = "proto3";

enum Status {
  UNKNOWN = 0;
  ACTIVE = 1;
  INACTIVE = 2;
}
`
	entities := extract(t, "s.proto", src)
	var enumRec *types.EntityRecord
	for i := range entities {
		if entities[i].Subtype == "enum" && entities[i].Name == "Status" {
			enumRec = &entities[i]
		}
	}
	if enumRec == nil {
		t.Fatal("enum entity not found")
	}
	wantValues := map[string]bool{"UNKNOWN": false, "ACTIVE": false, "INACTIVE": false}
	for _, r := range enumRec.Relationships {
		if r.Kind != "CONTAINS" {
			continue
		}
		for v := range wantValues {
			if strings.HasSuffix(r.ToID, ":Status#"+v) {
				wantValues[v] = true
			}
		}
	}
	for v, seen := range wantValues {
		if !seen {
			t.Errorf("enum Status missing CONTAINS edge for value %q", v)
		}
	}
}

// ---- Empty / no relationships -------------------------------------------

func TestRelationships_NoImports_NoEdges(t *testing.T) {
	src := `syntax = "proto3";
message Foo { string id = 1; }
`
	entities := extract(t, "foo.proto", src)
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				t.Errorf("unexpected IMPORTS edge: %+v", r)
			}
		}
	}
}
