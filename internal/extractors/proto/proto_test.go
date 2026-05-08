package proto_test

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsproto "github.com/smacker/go-tree-sitter/protobuf"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/proto"
)

func parseForTest(t *testing.T, src string) *sitter.Tree {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(tsproto.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func TestProtoExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("proto")
	if !ok {
		t.Fatal("proto extractor not registered")
	}
}

func TestProtoExtractor_ServiceAndRPC(t *testing.T) {
	src := `syntax = "proto3";

service UserService {
  rpc GetUser(GetUserRequest) returns (User);
  rpc CreateUser(CreateUserRequest) returns (User);
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("proto")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "user.proto",
		Content:  []byte(src),
		Language: "proto",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	services := make(map[string]bool)
	rpcs := make(map[string]bool)
	for _, e := range entities {
		switch {
		case e.Subtype == "service":
			services[e.Name] = true
			if e.Kind != "SCOPE.Service" {
				t.Errorf("service %q: expected Kind=SCOPE.Service, got %q", e.Name, e.Kind)
			}
		case e.Kind == "SCOPE.Operation" && e.Properties["type"] == "rpc":
			// RPCs use Subtype="endpoint" (matches Python parity golden) with
			// properties.type="rpc" to preserve the discriminator. See
			// internal/extractors/proto/proto.go:buildRPC.
			rpcs[e.Name] = true
		}
	}
	if !services["UserService"] {
		t.Error("expected service 'UserService' to be extracted")
	}
	for _, want := range []string{"GetUser", "CreateUser"} {
		if !rpcs[want] {
			t.Errorf("expected rpc %q to be extracted", want)
		}
	}
}

func TestProtoExtractor_MessagesAndEnums(t *testing.T) {
	src := `syntax = "proto3";

message User {
  string id = 1;
  string name = 2;
}

message CreateUserRequest {
  string name = 1;
}

enum Status {
  UNKNOWN = 0;
  ACTIVE = 1;
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("proto")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "types.proto",
		Content:  []byte(src),
		Language: "proto",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	messages := make(map[string]bool)
	enums := make(map[string]bool)
	for _, e := range entities {
		switch e.Subtype {
		case "message":
			messages[e.Name] = true
		case "enum":
			enums[e.Name] = true
		}
	}
	for _, want := range []string{"User", "CreateUserRequest"} {
		if !messages[want] {
			t.Errorf("expected message %q to be extracted", want)
		}
	}
	if !enums["Status"] {
		t.Error("expected enum 'Status' to be extracted")
	}
}

func TestProtoExtractor_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("proto")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.proto",
		Content:  []byte{},
		Language: "proto",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(entities))
	}
}

func TestProtoExtractor_Language(t *testing.T) {
	src := `syntax = "proto3";
message Foo { string id = 1; }
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("proto")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "foo.proto",
		Content:  []byte(src),
		Language: "proto",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Proto entities emit Language="protobuf" to match Python parity golden
	// (fixtures/protobuf/proto__sample.json). The tree-sitter language key is
	// "proto" but the canonical emitted language is "protobuf".
	for _, e := range entities {
		if e.Language != "protobuf" {
			t.Errorf("entity %q: expected Language=protobuf, got %q", e.Name, e.Language)
		}
	}
}
