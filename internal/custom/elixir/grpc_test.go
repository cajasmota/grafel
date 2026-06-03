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

// TestGRPCElixirInterceptorAuth asserts that when a file BOTH defines an
// auth-enforcing GRPC.Server.Interceptor (raises GRPC.RPCError :unauthenticated)
// AND wires it via `intercept`/`interceptors:`, the gRPC RPC methods and server
// are stamped auth_required=true + auth_method=grpc_interceptor +
// auth_middleware=<interceptor module> (#4041, gRPC-Elixir slice).
func TestGRPCElixirInterceptorAuth(t *testing.T) {
	// (a) GRPC.Endpoint `intercept` form, `use GRPC.Server.Interceptor`.
	src := `
defmodule MyApp.AuthInterceptor do
  use GRPC.Server.Interceptor

  def call(req, stream, next, _opts) do
    case stream |> GRPC.Stream.get_headers() |> Map.get("authorization") do
      nil -> raise GRPC.RPCError, status: :unauthenticated
      _ -> next.(req, stream)
    end
  end
end

defmodule Helloworld.Greeter.Service do
  use GRPC.Service, name: "helloworld.Greeter"
  rpc :SayHello, HelloRequest, HelloReply
end

defmodule Helloworld.Greeter.Server do
  use GRPC.Server, service: Helloworld.Greeter.Service
  def say_hello(req, _stream), do: req
end

defmodule MyApp.Endpoint do
  use GRPC.Endpoint
  intercept MyApp.AuthInterceptor
  run Helloworld.Greeter.Server
end
`
	ents := extract(t, "custom_elixir_grpc", fi("svc.ex", "elixir", src))

	m := findEntity(ents, "SCOPE.GrpcMethod", "grpc:helloworld.Greeter/SayHello")
	if m == nil {
		t.Fatal("expected GrpcMethod SayHello")
	}
	if got := m.Props["auth_required"]; got != "true" {
		t.Errorf("SayHello: expected auth_required true, got %q", got)
	}
	if got := m.Props["auth_method"]; got != "grpc_interceptor" {
		t.Errorf("SayHello: expected auth_method grpc_interceptor, got %q", got)
	}
	if got := m.Props["auth_middleware"]; got != "MyApp.AuthInterceptor" {
		t.Errorf("SayHello: expected auth_middleware MyApp.AuthInterceptor, got %q", got)
	}
	if got := m.Props["auth_enforcer_kind"]; got != "interceptor" {
		t.Errorf("SayHello: expected auth_enforcer_kind interceptor, got %q", got)
	}
	if got := m.Props["auth_confidence"]; got != "high" {
		t.Errorf("SayHello: expected auth_confidence high, got %q", got)
	}

	// The server entity is also stamped.
	srv := findEntity(ents, "SCOPE.GrpcService", "Helloworld.Greeter")
	if srv == nil || srv.Props["grpc_role"] != "server" {
		t.Fatal("expected server GrpcService Helloworld.Greeter")
	}
	if got := srv.Props["auth_middleware"]; got != "MyApp.AuthInterceptor" {
		t.Errorf("server: expected auth_middleware MyApp.AuthInterceptor, got %q", got)
	}
}

// TestGRPCElixirInterceptorsListAuth asserts the run/2 / GRPC.Server.Supervisor
// `interceptors: [...]` wiring form, with @behaviour and the GRPC.Status helper.
func TestGRPCElixirInterceptorsListAuth(t *testing.T) {
	src := `
defmodule MyApp.TokenInterceptor do
  @behaviour GRPC.Server.Interceptor

  @impl true
  def call(req, stream, next, _opts) do
    unless authorized?(stream) do
      raise GRPC.RPCError, GRPC.Status.unauthenticated()
    end
    next.(req, stream)
  end
end

defmodule Routeguide.RouteGuide.Service do
  use GRPC.Service, name: "routeguide.RouteGuide"
  rpc :GetFeature, Point, Feature
end

defmodule MyApp.Endpoint do
  run Routeguide.RouteGuide.Server, interceptors: [GRPC.Logger.Server, MyApp.TokenInterceptor]
end
`
	ents := extract(t, "custom_elixir_grpc", fi("rg.ex", "elixir", src))
	m := findEntity(ents, "SCOPE.GrpcMethod", "grpc:routeguide.RouteGuide/GetFeature")
	if m == nil {
		t.Fatal("expected GrpcMethod GetFeature")
	}
	if got := m.Props["auth_required"]; got != "true" {
		t.Errorf("GetFeature: expected auth_required true, got %q", got)
	}
	if got := m.Props["auth_middleware"]; got != "MyApp.TokenInterceptor" {
		t.Errorf("GetFeature: expected auth_middleware MyApp.TokenInterceptor, got %q", got)
	}
}

// TestGRPCElixirLoggingInterceptorNoAuth is a NEGATIVE: an interceptor that
// declares the behaviour and is wired, but never rejects with an auth status
// (observational logging), must NOT credit auth.
func TestGRPCElixirLoggingInterceptorNoAuth(t *testing.T) {
	src := `
defmodule MyApp.LoggingInterceptor do
  use GRPC.Server.Interceptor

  def call(req, stream, next, _opts) do
    Logger.info("rpc call")
    next.(req, stream)
  end
end

defmodule Helloworld.Greeter.Service do
  use GRPC.Service, name: "helloworld.Greeter"
  rpc :SayHello, HelloRequest, HelloReply
end

defmodule MyApp.Endpoint do
  use GRPC.Endpoint
  intercept MyApp.LoggingInterceptor
  run Helloworld.Greeter.Server
end
`
	ents := extract(t, "custom_elixir_grpc", fi("log.ex", "elixir", src))
	m := findEntity(ents, "SCOPE.GrpcMethod", "grpc:helloworld.Greeter/SayHello")
	if m == nil {
		t.Fatal("expected GrpcMethod SayHello")
	}
	if got := m.Props["auth_required"]; got != "" {
		t.Errorf("logging interceptor: expected NO auth_required, got %q", got)
	}
	if got := m.Props["auth_middleware"]; got != "" {
		t.Errorf("logging interceptor: expected NO auth_middleware, got %q", got)
	}
}

// TestGRPCElixirNoInterceptorNoAuth is a NEGATIVE: an auth-enforcing interceptor
// is DEFINED but never wired (no intercept / interceptors:), so the methods are
// left unstamped (honest same-file boundary).
func TestGRPCElixirNoInterceptorNoAuth(t *testing.T) {
	src := `
defmodule MyApp.AuthInterceptor do
  use GRPC.Server.Interceptor
  def call(req, stream, next, _opts) do
    raise GRPC.RPCError, status: :unauthenticated
  end
end

defmodule Helloworld.Greeter.Service do
  use GRPC.Service, name: "helloworld.Greeter"
  rpc :SayHello, HelloRequest, HelloReply
end
`
	ents := extract(t, "custom_elixir_grpc", fi("nowire.ex", "elixir", src))
	m := findEntity(ents, "SCOPE.GrpcMethod", "grpc:helloworld.Greeter/SayHello")
	if m == nil {
		t.Fatal("expected GrpcMethod SayHello")
	}
	if got := m.Props["auth_required"]; got != "" {
		t.Errorf("unwired interceptor: expected NO auth_required, got %q", got)
	}
}
