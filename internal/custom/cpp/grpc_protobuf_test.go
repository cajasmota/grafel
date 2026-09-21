package cpp_test

// grpc_protobuf_test.go — value-asserting fixture tests for the gRPC C++
// (grpc++), Protocol Buffers, and nlohmann/json DTO extractors (#3510).
//
// Each test asserts concrete extracted values (service + method names, RPC
// paths, request/response DTO type names, registration sites, proto fields,
// nlohmann field lists) — never len>0.

import "testing"

// propOf returns the value of prop key on the first entity matching kind+name,
// failing the test if no such entity exists.
func propOf(t *testing.T, ents []entitySummary, kind, name, key string) string {
	t.Helper()
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Name == name {
			return ents[i].Props[key]
		}
	}
	t.Fatalf("no %s entity named %q (got %+v)", kind, name, ents)
	return ""
}

// ---------------------------------------------------------------------------
// grpc++ — service impl, RPC methods, req/resp DTOs, registration, client stub
// ---------------------------------------------------------------------------

func TestGrpcUnaryService(t *testing.T) {
	src := `
#include <grpcpp/grpcpp.h>
#include "helloworld.grpc.pb.h"

class GreeterServiceImpl final : public Greeter::Service {
    Status SayHello(ServerContext* context,
                    const HelloRequest* request,
                    HelloReply* reply) override {
        reply->set_message("Hello " + request->name());
        return Status::OK;
    }
};
`
	ents := extract(t, "custom_cpp_grpc", fi("greeter.cc", "cpp", src))

	// Endpoint: RPC /Greeter/SayHello
	ep := findEndpoint(ents, "RPC /Greeter/SayHello")
	if ep == nil {
		t.Fatalf("expected RPC /Greeter/SayHello endpoint, got %+v", ents)
	}
	if got := ep.Props["grpc_service"]; got != "Greeter" {
		t.Errorf("grpc_service = %q, want Greeter", got)
	}
	if got := ep.Props["grpc_method"]; got != "SayHello" {
		t.Errorf("grpc_method = %q, want SayHello", got)
	}
	if got := ep.Props["request_message"]; got != "HelloRequest" {
		t.Errorf("request_message = %q, want HelloRequest", got)
	}
	if got := ep.Props["response_message"]; got != "HelloReply" {
		t.Errorf("response_message = %q, want HelloReply", got)
	}
	if got := ep.Props["handler_name"]; got != "GreeterServiceImpl.SayHello" {
		t.Errorf("handler_name = %q, want GreeterServiceImpl.SayHello", got)
	}

	// Request/response DTOs.
	if !containsEntity(ents, "SCOPE.Schema", "grpc_dto:HelloRequest") {
		t.Error("expected grpc_dto:HelloRequest DTO")
	}
	if !containsEntity(ents, "SCOPE.Schema", "grpc_dto:HelloReply") {
		t.Error("expected grpc_dto:HelloReply DTO")
	}
	if r := propOf(t, ents, "SCOPE.Schema", "grpc_dto:HelloRequest", "grpc_message_role"); r != "request" {
		t.Errorf("HelloRequest role = %q, want request", r)
	}
}

func TestGrpcQualifiedServiceBase(t *testing.T) {
	src := `
class RouteGuideImpl final : public routeguide::RouteGuide::Service {
    Status GetFeature(ServerContext* ctx, const Point* point, Feature* feature) override {
        return Status::OK;
    }
};
`
	ents := extract(t, "custom_cpp_grpc", fi("route.cc", "cpp", src))
	ep := findEndpoint(ents, "RPC /RouteGuide/GetFeature")
	if ep == nil {
		t.Fatalf("expected RPC /RouteGuide/GetFeature, got %+v", ents)
	}
	if got := ep.Props["request_message"]; got != "Point" {
		t.Errorf("request_message = %q, want Point", got)
	}
	if got := ep.Props["response_message"]; got != "Feature" {
		t.Errorf("response_message = %q, want Feature", got)
	}
}

func TestGrpcServerStreaming(t *testing.T) {
	src := `
class RouteGuideImpl final : public RouteGuide::Service {
    Status ListFeatures(ServerContext* ctx, const Rectangle* rect,
                        ServerWriter<Feature>* writer) override {
        return Status::OK;
    }
};
`
	ents := extract(t, "custom_cpp_grpc", fi("route.cc", "cpp", src))
	ep := findEndpoint(ents, "RPC /RouteGuide/ListFeatures")
	if ep == nil {
		t.Fatalf("expected RPC /RouteGuide/ListFeatures, got %+v", ents)
	}
	if got := ep.Props["streaming"]; got != "server_streaming" {
		t.Errorf("streaming = %q, want server_streaming", got)
	}
	if got := ep.Props["request_message"]; got != "Rectangle" {
		t.Errorf("request_message = %q, want Rectangle", got)
	}
	if got := ep.Props["response_message"]; got != "Feature" {
		t.Errorf("response_message = %q, want Feature", got)
	}
}

// TestGrpcClientStreaming pins the ServerReader (client-streaming) arm of
// cppGrpcParseArgs. What distinguishes client-streaming from the other two
// kinds is the DIRECTION the streamed type is assigned in: for ServerReader the
// type inside the angle brackets is the REQUEST (for ServerWriter it is the
// RESPONSE), and the plain message pointer carries the other side. The fixture
// therefore uses two distinct type names so a swapped assignment in that arm
// cannot satisfy the assertions.
func TestGrpcClientStreaming(t *testing.T) {
	src := `
class RouteGuideImpl final : public RouteGuide::Service {
    Status RecordRoute(ServerContext* ctx, ServerReader<Point>* reader,
                       RouteSummary* summary) override {
        return Status::OK;
    }
};
`
	ents := extract(t, "custom_cpp_grpc", fi("route.cc", "cpp", src))
	ep := findEndpoint(ents, "RPC /RouteGuide/RecordRoute")
	if ep == nil {
		t.Fatalf("expected RPC /RouteGuide/RecordRoute, got %+v", ents)
	}
	if got := ep.Props["streaming"]; got != "client_streaming" {
		t.Errorf("streaming = %q, want client_streaming", got)
	}
	// Streamed-in type is the request; the plain out-pointer is the response.
	if got := ep.Props["request_message"]; got != "Point" {
		t.Errorf("request_message = %q, want Point", got)
	}
	if got := ep.Props["response_message"]; got != "RouteSummary" {
		t.Errorf("response_message = %q, want RouteSummary", got)
	}
	// The DTO roles follow the same direction.
	if r := propOf(t, ents, "SCOPE.Schema", "grpc_dto:Point", "grpc_message_role"); r != "request" {
		t.Errorf("Point role = %q, want request", r)
	}
	if r := propOf(t, ents, "SCOPE.Schema", "grpc_dto:RouteSummary", "grpc_message_role"); r != "response" {
		t.Errorf("RouteSummary role = %q, want response", r)
	}
}

func TestGrpcBidiStreaming(t *testing.T) {
	src := `
class RouteGuideImpl final : public RouteGuide::Service {
    Status RouteChat(ServerContext* ctx,
                     ServerReaderWriter<RouteNote, RouteNote>* stream) override {
        return Status::OK;
    }
    Status Converse(ServerContext* ctx,
                    ServerReaderWriter<ChatReply, ChatRequest>* stream) override {
        return Status::OK;
    }
};
`
	ents := extract(t, "custom_cpp_grpc", fi("route.cc", "cpp", src))
	ep := findEndpoint(ents, "RPC /RouteGuide/RouteChat")
	if ep == nil {
		t.Fatalf("expected RPC /RouteGuide/RouteChat, got %+v", ents)
	}
	if got := ep.Props["streaming"]; got != "bidi_streaming" {
		t.Errorf("streaming = %q, want bidi_streaming", got)
	}
	// RouteChat streams the same message type both ways, so it cannot tell a
	// correct ServerReaderWriter<Response, Request> assignment from a swapped or
	// a missing one. Converse uses two distinct type names, which does.
	ep2 := findEndpoint(ents, "RPC /RouteGuide/Converse")
	if ep2 == nil {
		t.Fatalf("expected RPC /RouteGuide/Converse, got %+v", ents)
	}
	if got := ep2.Props["streaming"]; got != "bidi_streaming" {
		t.Errorf("Converse streaming = %q, want bidi_streaming", got)
	}
	if got := ep2.Props["response_message"]; got != "ChatReply" {
		t.Errorf("Converse response_message = %q, want ChatReply", got)
	}
	if got := ep2.Props["request_message"]; got != "ChatRequest" {
		t.Errorf("Converse request_message = %q, want ChatRequest", got)
	}
}

func TestGrpcRegisterServiceAndStub(t *testing.T) {
	src := `
void RunServer() {
    GreeterServiceImpl service;
    ServerBuilder builder;
    builder.RegisterService(&service);
    auto stub = Greeter::NewStub(channel);
}
`
	ents := extract(t, "custom_cpp_grpc", fi("server.cc", "cpp", src))
	if !containsEntity(ents, "SCOPE.Service", "grpc_service:service") {
		t.Errorf("expected grpc_service:service registration, got %+v", ents)
	}
	if r := propOf(t, ents, "SCOPE.Service", "grpc_service:service", "registration"); r != "RegisterService" {
		t.Errorf("registration = %q, want RegisterService", r)
	}
	if !containsEntity(ents, "SCOPE.Service", "grpc_stub:Greeter") {
		t.Errorf("expected grpc_stub:Greeter client stub, got %+v", ents)
	}
	if r := propOf(t, ents, "SCOPE.Service", "grpc_stub:Greeter", "client_role"); r != "stub" {
		t.Errorf("client_role = %q, want stub", r)
	}
}

func TestGrpcNoMatch(t *testing.T) {
	src := `#include <iostream>
int main() { std::cout << "hi"; return 0; }`
	ents := extract(t, "custom_cpp_grpc", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %+v", ents)
	}
}

func TestGrpcWrongLanguage(t *testing.T) {
	src := `class Foo final : public Greeter::Service { Status Bar(ServerContext* c, const Req* r, Resp* p) override; };`
	ents := extract(t, "custom_cpp_grpc", fi("x.c", "c", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should yield no entities, got %+v", ents)
	}
}

// ---------------------------------------------------------------------------
// Protobuf — .proto IDL (messages, fields, enums, services, rpcs)
// ---------------------------------------------------------------------------

func TestProtobufProtoMessageFields(t *testing.T) {
	src := `
syntax = "proto3";
package helloworld;

message HelloRequest {
  string name = 1;
  int32 count = 2;
  repeated string tags = 3;
}
`
	ents := extract(t, "custom_cpp_protobuf", fi("hello.proto", "protobuf", src))

	if !containsEntity(ents, "SCOPE.Schema", "proto_message:HelloRequest") {
		t.Fatalf("expected proto_message:HelloRequest DTO, got %+v", ents)
	}
	// Fields with type + number.
	if !ormFindEntityExists(ents, "SCOPE.Schema", "field", "HelloRequest.name") {
		t.Error("expected HelloRequest.name field")
	}
	if got := propOf(t, ents, "SCOPE.Schema", "HelloRequest.name", "field_type"); got != "string" {
		t.Errorf("name field_type = %q, want string", got)
	}
	if got := propOf(t, ents, "SCOPE.Schema", "HelloRequest.count", "field_number"); got != "2" {
		t.Errorf("count field_number = %q, want 2", got)
	}
	if got := propOf(t, ents, "SCOPE.Schema", "HelloRequest.tags", "field_label"); got != "repeated" {
		t.Errorf("tags field_label = %q, want repeated", got)
	}
}

func TestProtobufProtoServiceRpc(t *testing.T) {
	src := `
service Greeter {
  rpc SayHello (HelloRequest) returns (HelloReply);
  rpc ListFeatures (Rectangle) returns (stream Feature);
}
`
	ents := extract(t, "custom_cpp_protobuf", fi("svc.proto", "protobuf", src))

	if !containsEntity(ents, "SCOPE.Service", "grpc_service:Greeter") {
		t.Fatalf("expected grpc_service:Greeter, got %+v", ents)
	}
	ep := findEndpoint(ents, "RPC /Greeter/SayHello")
	if ep == nil {
		t.Fatalf("expected RPC /Greeter/SayHello, got %+v", ents)
	}
	if got := ep.Props["request_message"]; got != "HelloRequest" {
		t.Errorf("request_message = %q, want HelloRequest", got)
	}
	if got := ep.Props["response_message"]; got != "HelloReply" {
		t.Errorf("response_message = %q, want HelloReply", got)
	}
	ep2 := findEndpoint(ents, "RPC /Greeter/ListFeatures")
	if ep2 == nil {
		t.Fatalf("expected RPC /Greeter/ListFeatures, got %+v", ents)
	}
	if got := ep2.Props["streaming"]; got != "server_streaming" {
		t.Errorf("ListFeatures streaming = %q, want server_streaming", got)
	}
}

// protoStreamKind — the two arms no .proto fixture selected (#7270).
//
// The arms are told apart by WHICH side carries `stream`, so each fixture puts
// a different message type on each side and asserts the label by name rather
// than asserting it is non-empty. A fixture symmetric in either respect would
// not separate one arm from its neighbours.

func TestProtobufProtoClientStreaming(t *testing.T) {
	src := `
service RouteGuide {
  rpc RecordRoute (stream Point) returns (RouteSummary);
}
`
	ents := extract(t, "custom_cpp_protobuf", fi("route.proto", "protobuf", src))
	ep := findEndpoint(ents, "RPC /RouteGuide/RecordRoute")
	if ep == nil {
		t.Fatalf("expected RPC /RouteGuide/RecordRoute, got %+v", ents)
	}
	// stream on the request side only: client_streaming, not server_ or bidi_.
	if got := ep.Props["streaming"]; got != "client_streaming" {
		t.Errorf("streaming = %q, want client_streaming", got)
	}
	if got := ep.Props["request_message"]; got != "Point" {
		t.Errorf("request_message = %q, want Point", got)
	}
	if got := ep.Props["response_message"]; got != "RouteSummary" {
		t.Errorf("response_message = %q, want RouteSummary", got)
	}
}

func TestProtobufProtoBidiStreaming(t *testing.T) {
	src := `
service RouteGuide {
  rpc Chat (stream ChatRequest) returns (stream ChatReply);
}
`
	ents := extract(t, "custom_cpp_protobuf", fi("chat.proto", "protobuf", src))
	ep := findEndpoint(ents, "RPC /RouteGuide/Chat")
	if ep == nil {
		t.Fatalf("expected RPC /RouteGuide/Chat, got %+v", ents)
	}
	// stream on BOTH sides. Both flags are load-bearing: widening the arm's
	// condition to either-flag would label one-sided RPCs bidi as well, which
	// TestProtobufProtoServiceRpc and the client-streaming fixture above
	// reject.
	if got := ep.Props["streaming"]; got != "bidi_streaming" {
		t.Errorf("streaming = %q, want bidi_streaming", got)
	}
	if got := ep.Props["request_message"]; got != "ChatRequest" {
		t.Errorf("request_message = %q, want ChatRequest", got)
	}
	if got := ep.Props["response_message"]; got != "ChatReply" {
		t.Errorf("response_message = %q, want ChatReply", got)
	}
}

func TestProtobufProtoEnum(t *testing.T) {
	src := `
enum Corpus {
  UNIVERSAL = 0;
  WEB = 1;
}
`
	ents := extract(t, "custom_cpp_protobuf", fi("e.proto", "protobuf", src))
	if !containsEntity(ents, "SCOPE.Schema", "proto_enum:Corpus") {
		t.Errorf("expected proto_enum:Corpus, got %+v", ents)
	}
}

func TestProtobufGeneratedHeader(t *testing.T) {
	src := `
#include <google/protobuf/message.h>
namespace helloworld {
class HelloRequest final : public ::google::protobuf::Message {
 public:
  const std::string& name() const;
};
}  // namespace helloworld
`
	ents := extract(t, "custom_cpp_protobuf", fi("hello.pb.h", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "proto_message:HelloRequest") {
		t.Fatalf("expected proto_message:HelloRequest from generated header, got %+v", ents)
	}
	if got := propOf(t, ents, "SCOPE.Schema", "proto_message:HelloRequest", "message_kind"); got != "generated_message" {
		t.Errorf("message_kind = %q, want generated_message", got)
	}
}

func TestProtobufNoMatch(t *testing.T) {
	src := `#include <iostream>
int main() { return 0; }`
	ents := extract(t, "custom_cpp_protobuf", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %+v", ents)
	}
}

// ---------------------------------------------------------------------------
// Protobuf — parity-grind-cpp: rpc_framework Type System caps (#3963).
//
// These tests credit protobuf (the IDL half of gRPC) on the rpc_framework
// "Type System" caps that the .proto extractor genuinely satisfies, mirroring
// the gRPC sibling cells:
//
//   - enum_extraction       .proto `enum` -> SCOPE.Schema/enum (enum_name)
//   - interface_extraction  .proto `service` -> SCOPE.Service/grpc_service
//   - type_extraction       .proto `message` -> SCOPE.Schema/dto + typed fields
//
// Each test value-asserts the exact entity identity AND a distinguishing
// property — never len>0. type_alias_extraction is intentionally NOT credited:
// proto3 IDL has no type-alias construct and the framework-agnostic C++
// type-system extractor emits no alias entity, so it is honest-N/A.
// ---------------------------------------------------------------------------

// enum_extraction: a .proto enum yields a SCOPE.Schema/enum entity whose
// enum_name property is the declared enum name and whose provenance marks it
// as inferred from a proto enum.
func TestProtobufEnumExtractionCap(t *testing.T) {
	src := `
syntax = "proto3";
enum Corpus {
  UNIVERSAL = 0;
  WEB = 1;
  IMAGES = 2;
}
`
	ents := extract(t, "custom_cpp_protobuf", fi("corpus.proto", "protobuf", src))
	e := ormFindEntity(ents, "SCOPE.Schema", "enum", "proto_enum:Corpus")
	if e == nil {
		t.Fatalf("expected SCOPE.Schema/enum proto_enum:Corpus, got %+v", ents)
	}
	if got := e.Props["enum_name"]; got != "Corpus" {
		t.Errorf("enum_name = %q, want Corpus", got)
	}
	if got := e.Props["provenance"]; got != "INFERRED_FROM_PROTO_ENUM" {
		t.Errorf("provenance = %q, want INFERRED_FROM_PROTO_ENUM", got)
	}
}

// interface_extraction: a .proto service declaration yields a SCOPE.Service
// entity (the RPC interface) carrying the service name and rpc_protocol=grpc.
func TestProtobufInterfaceExtractionCap(t *testing.T) {
	src := `
service RouteGuide {
  rpc GetFeature (Point) returns (Feature);
}
`
	ents := extract(t, "custom_cpp_protobuf", fi("route.proto", "protobuf", src))
	e := ormFindEntity(ents, "SCOPE.Service", "grpc_service", "grpc_service:RouteGuide")
	if e == nil {
		t.Fatalf("expected SCOPE.Service/grpc_service grpc_service:RouteGuide, got %+v", ents)
	}
	if got := e.Props["grpc_service"]; got != "RouteGuide" {
		t.Errorf("grpc_service = %q, want RouteGuide", got)
	}
	if got := e.Props["rpc_protocol"]; got != "grpc" {
		t.Errorf("rpc_protocol = %q, want grpc", got)
	}
}

// type_extraction: a .proto message yields a SCOPE.Schema/dto type whose
// dto_name matches, and per-field SCOPE.Schema/field entities carrying the
// declared field_type — the message type shape recovered from the IDL.
func TestProtobufTypeExtractionCap(t *testing.T) {
	src := `
syntax = "proto3";
message Point {
  int32 latitude = 1;
  int32 longitude = 2;
}
`
	ents := extract(t, "custom_cpp_protobuf", fi("point.proto", "protobuf", src))
	dto := ormFindEntity(ents, "SCOPE.Schema", "dto", "proto_message:Point")
	if dto == nil {
		t.Fatalf("expected SCOPE.Schema/dto proto_message:Point, got %+v", ents)
	}
	if got := dto.Props["dto_name"]; got != "Point" {
		t.Errorf("dto_name = %q, want Point", got)
	}
	if got := dto.Props["message_kind"]; got != "proto_message" {
		t.Errorf("message_kind = %q, want proto_message", got)
	}
	// Typed field recovered from the message body.
	fld := ormFindEntity(ents, "SCOPE.Schema", "field", "Point.latitude")
	if fld == nil {
		t.Fatalf("expected SCOPE.Schema/field Point.latitude, got %+v", ents)
	}
	if got := fld.Props["field_type"]; got != "int32" {
		t.Errorf("latitude field_type = %q, want int32", got)
	}
	if got := fld.Props["parent_message"]; got != "Point" {
		t.Errorf("latitude parent_message = %q, want Point", got)
	}
}

// ---------------------------------------------------------------------------
// nlohmann/json — DTO model + fields + free-function serialization binding
// ---------------------------------------------------------------------------

func TestNlohmannDefineTypeIntrusive(t *testing.T) {
	src := `
struct User {
    std::string name;
    int age;
    std::string email;
    NLOHMANN_DEFINE_TYPE_INTRUSIVE(User, name, age, email)
};
`
	ents := extract(t, "custom_cpp_nlohmann_json", fi("user.hpp", "cpp", src))

	if !containsEntity(ents, "SCOPE.Schema", "nlohmann_dto:User") {
		t.Fatalf("expected nlohmann_dto:User, got %+v", ents)
	}
	if got := propOf(t, ents, "SCOPE.Schema", "nlohmann_dto:User", "fields"); got != "name,age,email" {
		t.Errorf("fields = %q, want name,age,email", got)
	}
	if got := propOf(t, ents, "SCOPE.Schema", "nlohmann_dto:User", "field_count"); got != "3" {
		t.Errorf("field_count = %q, want 3", got)
	}
	if got := propOf(t, ents, "SCOPE.Schema", "nlohmann_dto:User", "macro_variant"); got != "INTRUSIVE" {
		t.Errorf("macro_variant = %q, want INTRUSIVE", got)
	}
	// Per-field entities.
	if !ormFindEntityExists(ents, "SCOPE.Schema", "field", "User.name") {
		t.Error("expected User.name field entity")
	}
	if got := propOf(t, ents, "SCOPE.Schema", "User.age", "parent_dto"); got != "User" {
		t.Errorf("User.age parent_dto = %q, want User", got)
	}
}

func TestNlohmannNonIntrusive(t *testing.T) {
	src := `NLOHMANN_DEFINE_TYPE_NON_INTRUSIVE(Address, street, city, zip)`
	ents := extract(t, "custom_cpp_nlohmann_json", fi("addr.hpp", "cpp", src))
	if got := propOf(t, ents, "SCOPE.Schema", "nlohmann_dto:Address", "fields"); got != "street,city,zip" {
		t.Errorf("fields = %q, want street,city,zip", got)
	}
	if got := propOf(t, ents, "SCOPE.Schema", "nlohmann_dto:Address", "macro_variant"); got != "NON_INTRUSIVE" {
		t.Errorf("macro_variant = %q, want NON_INTRUSIVE", got)
	}
}

func TestNlohmannFreeFunctions(t *testing.T) {
	src := `
void to_json(json& j, const Color& c) {
    j = json{{"r", c.r}, {"g", c.g}, {"b", c.b}};
}
void from_json(const json& j, Color& c) {
    j.at("r").get_to(c.r);
}
`
	ents := extract(t, "custom_cpp_nlohmann_json", fi("color.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "nlohmann_dto:Color") {
		t.Fatalf("expected nlohmann_dto:Color, got %+v", ents)
	}
	if got := propOf(t, ents, "SCOPE.Schema", "nlohmann_dto:Color", "serialization_direction"); got != "bidirectional" {
		t.Errorf("serialization_direction = %q, want bidirectional", got)
	}
}

// TestNlohmannSingleDirectionFreeFunctions pins the two one-sided arms of the
// serialization-direction switch (#7270).
//
// The two types are opposites in one file: Outbound has only to_json and
// Inbound only from_json. That asymmetry is what grades the pair — exchanging
// the two arms' labels fails this test, which a fixture carrying a single type
// (or a bidirectional one) could not detect.
func TestNlohmannSingleDirectionFreeFunctions(t *testing.T) {
	src := `
void to_json(json& j, const Outbound& o) {
    j = json{{"id", o.id}};
}
void from_json(const json& j, Inbound& i) {
    j.at("id").get_to(i.id);
}
`
	ents := extract(t, "custom_cpp_nlohmann_json", fi("dirs.cpp", "cpp", src))
	if got := propOf(t, ents, "SCOPE.Schema", "nlohmann_dto:Outbound", "serialization_direction"); got != "serialize" {
		t.Errorf("Outbound serialization_direction = %q, want serialize", got)
	}
	if got := propOf(t, ents, "SCOPE.Schema", "nlohmann_dto:Inbound", "serialization_direction"); got != "deserialize" {
		t.Errorf("Inbound serialization_direction = %q, want deserialize", got)
	}
}

func TestNlohmannNoMatch(t *testing.T) {
	src := `int main() { return 0; }`
	ents := extract(t, "custom_cpp_nlohmann_json", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %+v", ents)
	}
}

// ormFindEntityExists is a small predicate over kind/subtype/name.
func ormFindEntityExists(ents []entitySummary, kind, subtype, name string) bool {
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Subtype == subtype && ents[i].Name == name {
			return true
		}
	}
	return false
}
