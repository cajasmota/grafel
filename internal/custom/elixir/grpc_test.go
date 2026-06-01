package elixir_test

import "testing"

// TestGRPCService asserts a GRPC.Service definition with rpc declarations
// yields the service entity plus one GrpcMethod per rpc, capturing method and
// request/response message names.
func TestGRPCService(t *testing.T) {
	src := `
defmodule Helloworld.Greeter.Service do
  use GRPC.Service, name: "helloworld.Greeter"

  rpc :SayHello, HelloRequest, HelloReply
  rpc :LotsOfReplies, HelloRequest, stream(HelloReply)
end
`
	ents := extract(t, "custom_elixir_grpc", fi("greeter.pb.ex", "elixir", src))

	svc := findEntity(ents, "SCOPE.GrpcService", "helloworld.Greeter")
	if svc == nil {
		t.Fatal("expected GrpcService helloworld.Greeter")
	}
	if got := svc.Props["grpc_role"]; got != "definition" {
		t.Errorf("expected grpc_role definition, got %q", got)
	}

	sayHello := findEntity(ents, "SCOPE.GrpcMethod", "grpc:helloworld.Greeter/SayHello")
	if sayHello == nil {
		t.Fatal("expected GrpcMethod grpc:helloworld.Greeter/SayHello")
	}
	if got := sayHello.Props["method_name"]; got != "SayHello" {
		t.Errorf("expected method_name SayHello, got %q", got)
	}
	if got := sayHello.Props["request_message"]; got != "HelloRequest" {
		t.Errorf("expected request_message HelloRequest, got %q", got)
	}
	if got := sayHello.Props["response_message"]; got != "HelloReply" {
		t.Errorf("expected response_message HelloReply, got %q", got)
	}
	if got := sayHello.Props["streaming"]; got != "unary" {
		t.Errorf("expected streaming unary, got %q", got)
	}

	stream := findEntity(ents, "SCOPE.GrpcMethod", "grpc:helloworld.Greeter/LotsOfReplies")
	if stream == nil {
		t.Fatal("expected GrpcMethod LotsOfReplies")
	}
	if got := stream.Props["streaming"]; got != "server_streaming" {
		t.Errorf("expected streaming server_streaming, got %q", got)
	}
	if got := stream.Props["response_message"]; got != "HelloReply" {
		t.Errorf("expected stripped response_message HelloReply, got %q", got)
	}
}

// TestGRPCServer asserts a GRPC.Server module yields a server-role service
// entity whose name is resolved from the `service:` option.
func TestGRPCServer(t *testing.T) {
	src := `
defmodule Helloworld.Greeter.Server do
  use GRPC.Server, service: Helloworld.Greeter.Service

  def say_hello(request, _stream) do
    Helloworld.HelloReply.new(message: "Hello #{request.name}")
  end
end
`
	ents := extract(t, "custom_elixir_grpc", fi("greeter_server.ex", "elixir", src))

	svc := findEntity(ents, "SCOPE.GrpcService", "Helloworld.Greeter")
	if svc == nil {
		t.Fatal("expected GrpcService Helloworld.Greeter (server)")
	}
	if got := svc.Props["grpc_role"]; got != "server" {
		t.Errorf("expected grpc_role server, got %q", got)
	}
}

// TestGRPCStub asserts a GRPC.Stub client module is recorded as a client-role
// service.
func TestGRPCStub(t *testing.T) {
	src := `
defmodule Helloworld.Greeter.Stub do
  use GRPC.Stub, service: Helloworld.Greeter.Service
end
`
	ents := extract(t, "custom_elixir_grpc", fi("greeter_stub.ex", "elixir", src))
	svc := findEntity(ents, "SCOPE.GrpcService", "Helloworld.Greeter")
	if svc == nil {
		t.Fatal("expected client GrpcService Helloworld.Greeter")
	}
	if got := svc.Props["grpc_role"]; got != "client" {
		t.Errorf("expected grpc_role client, got %q", got)
	}
}

func TestGRPCNoMatch(t *testing.T) {
	src := `
defmodule MyApp.Plain do
  def hello, do: :world
end
`
	ents := extract(t, "custom_elixir_grpc", fi("plain.ex", "elixir", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities from plain module, got %d", len(ents))
	}
}
