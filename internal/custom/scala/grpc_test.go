package scala_test

import (
	"testing"
)

// TestScalaGRPC_ScalaPBService asserts that a ScalaPB AbstractService trait
// yields RPC endpoints at /<Service>/<rpc> with the SPECIFIC service + method
// names and request/response message types.
func TestScalaGRPC_ScalaPBService(t *testing.T) {
	src := `
package example.grpc

trait Greeter extends _root_.scalapb.grpc.AbstractService {
  def sayHello(request: HelloRequest): scala.concurrent.Future[HelloReply]
  def listUsers(request: ListUsersRequest): Future[UserList]
}
`
	ents := extract(t, "custom_scala_grpc", fi("GreeterGrpc.scala", "scala", src))

	ep, ok := findBySubtype(ents, "endpoint", "RPC /Greeter/sayHello")
	if !ok {
		t.Fatalf("expected RPC /Greeter/sayHello endpoint; got %d entities", len(ents))
	}
	if ep.Props["grpc_service"] != "Greeter" {
		t.Errorf("grpc_service = %q, want Greeter", ep.Props["grpc_service"])
	}
	if ep.Props["grpc_method"] != "sayHello" {
		t.Errorf("grpc_method = %q, want sayHello", ep.Props["grpc_method"])
	}
	if ep.Props["request_message"] != "HelloRequest" {
		t.Errorf("request_message = %q, want HelloRequest", ep.Props["request_message"])
	}
	if ep.Props["response_message"] != "HelloReply" {
		t.Errorf("response_message = %q, want HelloReply", ep.Props["response_message"])
	}
	if ep.Props["rpc_protocol"] != "grpc" || ep.Props["verb"] != "RPC" {
		t.Errorf("rpc_protocol/verb = %q/%q, want grpc/RPC", ep.Props["rpc_protocol"], ep.Props["verb"])
	}

	// Second RPC.
	ep2, ok := findBySubtype(ents, "endpoint", "RPC /Greeter/listUsers")
	if !ok {
		t.Fatalf("expected RPC /Greeter/listUsers endpoint")
	}
	if ep2.Props["response_message"] != "UserList" {
		t.Errorf("listUsers response_message = %q, want UserList", ep2.Props["response_message"])
	}

	// Service entity.
	if _, ok := findBySubtype(ents, "grpc_service", "grpc_service:Greeter"); !ok {
		t.Error("expected grpc_service:Greeter SCOPE.Service entity")
	}

	// Request/response DTO references.
	dto, ok := findBySubtype(ents, "dto", "grpc_dto:HelloRequest")
	if !ok {
		t.Error("expected grpc_dto:HelloRequest DTO ref")
	} else if dto.Props["grpc_message_role"] != "request" {
		t.Errorf("HelloRequest role = %q, want request", dto.Props["grpc_message_role"])
	}
	if _, ok := findBySubtype(ents, "dto", "grpc_dto:HelloReply"); !ok {
		t.Error("expected grpc_dto:HelloReply DTO ref")
	}
}

// TestScalaGRPC_ZioGrpcService asserts zio-grpc ZIO[Context, Status, Resp]
// effect handling: the response message is the LAST type argument, and a
// leading `Z` on the trait name is stripped for the proto service name.
func TestScalaGRPC_ZioGrpcService(t *testing.T) {
	src := `
import scalapb.zio_grpc.ZGeneratedService

trait ZGreeter[Context] extends scalapb.zio_grpc.ZGeneratedService {
  def sayHello(request: HelloRequest): ZIO[Context, Status, HelloReply]
}
`
	ents := extract(t, "custom_scala_grpc", fi("ZGreeterGrpc.scala", "scala", src))
	ep, ok := findBySubtype(ents, "endpoint", "RPC /Greeter/sayHello")
	if !ok {
		t.Fatalf("expected RPC /Greeter/sayHello (Z-stripped); got %d entities", len(ents))
	}
	if ep.Props["response_message"] != "HelloReply" {
		t.Errorf("response_message = %q, want HelloReply (last ZIO type arg)", ep.Props["response_message"])
	}
	if ep.Props["service_trait"] != "ZGreeter" {
		t.Errorf("service_trait = %q, want ZGreeter", ep.Props["service_trait"])
	}
}

// TestScalaGRPC_Fs2GrpcService asserts fs2-grpc: a `*Fs2Grpc` trait (no explicit
// gRPC base) with an `F[Resp]` effect and a trailing ctx param.
func TestScalaGRPC_Fs2GrpcService(t *testing.T) {
	src := `
trait GreeterFs2Grpc[F[_], A] {
  def sayHello(request: HelloRequest, ctx: A): F[HelloReply]
}
`
	ents := extract(t, "custom_scala_grpc", fi("GreeterFs2Grpc.scala", "scala", src))
	ep, ok := findBySubtype(ents, "endpoint", "RPC /Greeter/sayHello")
	if !ok {
		t.Fatalf("expected RPC /Greeter/sayHello (Fs2Grpc-stripped); got %d entities", len(ents))
	}
	if ep.Props["request_message"] != "HelloRequest" {
		t.Errorf("request_message = %q, want HelloRequest", ep.Props["request_message"])
	}
	if ep.Props["response_message"] != "HelloReply" {
		t.Errorf("response_message = %q, want HelloReply", ep.Props["response_message"])
	}
}

// TestScalaGRPC_StubSite asserts a generated-stub access site is recorded with
// the SPECIFIC companion + accessor.
func TestScalaGRPC_StubSite(t *testing.T) {
	src := `
import example.grpc.GreeterGrpc

object Client {
  val stub = GreeterGrpc.stub(channel)
}
`
	ents := extract(t, "custom_scala_grpc", fi("Client.scala", "scala", src))
	st, ok := findBySubtype(ents, "grpc_stub", "grpc_stub:GreeterGrpc.stub")
	if !ok {
		t.Fatalf("expected grpc_stub:GreeterGrpc.stub component")
	}
	if st.Props["grpc_service"] != "Greeter" {
		t.Errorf("grpc_service = %q, want Greeter", st.Props["grpc_service"])
	}
	if st.Props["grpc_accessor"] != "stub" {
		t.Errorf("grpc_accessor = %q, want stub", st.Props["grpc_accessor"])
	}
}

// TestScalaGRPC_NoMatch asserts a plain Scala file with no gRPC markers emits
// nothing.
func TestScalaGRPC_NoMatch(t *testing.T) {
	src := `
trait UserRepository {
  def find(id: Long): Option[User]
}
object Main extends App { println("hi") }
`
	ents := extract(t, "custom_scala_grpc", fi("UserRepository.scala", "scala", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-gRPC file, got %d", len(ents))
	}
}
