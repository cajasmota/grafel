package scala_test

import "testing"

// TestScalaGRPC_InterceptorAuth_ScalaPB asserts that a grpc-java ServerInterceptor
// failing with Status.UNAUTHENTICATED, wired via ServerInterceptors.intercept,
// stamps auth_required=true + auth_method=grpc_interceptor + auth_middleware on
// the SPECIFIC guarded gRPC method (#4041, gRPC-Scala slice).
func TestScalaGRPC_InterceptorAuth_ScalaPB(t *testing.T) {
	src := `
package example.grpc
import io.grpc.{ServerInterceptor, ServerCall, ServerCallHandler, Metadata, Status, ServerInterceptors}

class AuthInterceptor extends ServerInterceptor {
  override def interceptCall[Req, Resp](call: ServerCall[Req, Resp], headers: Metadata, next: ServerCallHandler[Req, Resp]) = {
    val token = headers.get(Metadata.Key.of("authorization", Metadata.ASCII_STRING_MARSHALLER))
    if (token == null) {
      call.close(Status.UNAUTHENTICATED.withDescription("missing token"), new Metadata())
    }
    next.startCall(call, headers)
  }
}

trait Greeter extends _root_.scalapb.grpc.AbstractService {
  def sayHello(request: HelloRequest): scala.concurrent.Future[HelloReply]
  def listUsers(request: ListUsersRequest): Future[UserList]
}

object Server {
  val svc = ServerInterceptors.intercept(GreeterGrpc.bindService(impl, ec), new AuthInterceptor)
}
`
	ents := extract(t, "custom_scala_grpc", fi("GreeterGrpc.scala", "scala", src))

	ep, ok := findBySubtype(ents, "endpoint", "RPC /Greeter/sayHello")
	if !ok {
		t.Fatalf("expected RPC /Greeter/sayHello endpoint; got %d entities", len(ents))
	}
	if ep.Props["auth_required"] != "true" {
		t.Errorf("sayHello auth_required = %q, want true", ep.Props["auth_required"])
	}
	if ep.Props["auth_method"] != "grpc_interceptor" {
		t.Errorf("sayHello auth_method = %q, want grpc_interceptor", ep.Props["auth_method"])
	}
	if ep.Props["auth_middleware"] != "AuthInterceptor" {
		t.Errorf("sayHello auth_middleware = %q, want AuthInterceptor", ep.Props["auth_middleware"])
	}

	// The second method of the same guarded service is also credited.
	ep2, ok := findBySubtype(ents, "endpoint", "RPC /Greeter/listUsers")
	if !ok {
		t.Fatalf("expected RPC /Greeter/listUsers endpoint")
	}
	if ep2.Props["auth_required"] != "true" || ep2.Props["auth_middleware"] != "AuthInterceptor" {
		t.Errorf("listUsers auth = %q/%q, want true/AuthInterceptor", ep2.Props["auth_required"], ep2.Props["auth_middleware"])
	}

	// Service entity is also stamped.
	svc, ok := findBySubtype(ents, "grpc_service", "grpc_service:Greeter")
	if !ok {
		t.Fatal("expected grpc_service:Greeter")
	}
	if svc.Props["auth_required"] != "true" {
		t.Errorf("service auth_required = %q, want true", svc.Props["auth_required"])
	}
}

// TestScalaGRPC_ZioTransformAuth asserts zio-grpc transformContextZIO that rejects
// unauthenticated requests credits the guarded RPC methods.
func TestScalaGRPC_ZioTransformAuth(t *testing.T) {
	src := `
package example.grpc
import scalapb.zio_grpc.ZServerInterceptor
import io.grpc.Status
import zio.ZIO

trait ZGreeter[Context] extends scalapb.zio_grpc.ZGeneratedService {
  def sayHello(request: HelloRequest): ZIO[Context, Status, HelloReply]
}

object AuthedGreeter {
  val service = new GreeterImpl().transformContextZIO { rc =>
    ZIO.fromOption(rc.metadata.get(AuthKey))
      .orElseFail(Status.UNAUTHENTICATED.withDescription("no token"))
      .map(token => AuthContext(token))
  }
}
`
	ents := extract(t, "custom_scala_grpc", fi("ZGreeter.scala", "scala", src))

	ep, ok := findBySubtype(ents, "endpoint", "RPC /Greeter/sayHello")
	if !ok {
		t.Fatalf("expected RPC /Greeter/sayHello endpoint; got %d entities", len(ents))
	}
	if ep.Props["auth_required"] != "true" {
		t.Errorf("sayHello auth_required = %q, want true", ep.Props["auth_required"])
	}
	if ep.Props["auth_method"] != "grpc_interceptor" {
		t.Errorf("sayHello auth_method = %q, want grpc_interceptor", ep.Props["auth_method"])
	}
	if ep.Props["auth_middleware"] != "transformContextZIO" {
		t.Errorf("sayHello auth_middleware = %q, want transformContextZIO", ep.Props["auth_middleware"])
	}
}

// TestScalaGRPC_ZServerInterceptorAuth asserts a zio-grpc ZServerInterceptor class
// rejecting unauthenticated requests credits auth.
func TestScalaGRPC_ZServerInterceptorAuth(t *testing.T) {
	src := `
package example.grpc
import scalapb.zio_grpc.ZServerInterceptor
import io.grpc.Status

class JwtInterceptor extends ZServerInterceptor {
  def interceptCall[Req, Resp](call, headers, next) = {
    if (!valid(headers)) fail(Status.UNAUTHENTICATED)
    else next(call, headers)
  }
}

trait ZGreeter[Context] extends scalapb.zio_grpc.ZGeneratedService {
  def sayHello(request: HelloRequest): ZIO[Context, Status, HelloReply]
}
`
	ents := extract(t, "custom_scala_grpc", fi("ZGreeter.scala", "scala", src))
	ep, ok := findBySubtype(ents, "endpoint", "RPC /Greeter/sayHello")
	if !ok {
		t.Fatalf("expected endpoint; got %d entities", len(ents))
	}
	if ep.Props["auth_required"] != "true" || ep.Props["auth_middleware"] != "JwtInterceptor" {
		t.Errorf("auth = %q/%q, want true/JwtInterceptor", ep.Props["auth_required"], ep.Props["auth_middleware"])
	}
}

// TestScalaGRPC_LoggingInterceptorNoAuth (NEGATIVE): a logging interceptor that
// never rejects UNAUTHENTICATED must NOT credit auth, even though it is wired.
func TestScalaGRPC_LoggingInterceptorNoAuth(t *testing.T) {
	src := `
package example.grpc
import io.grpc.{ServerInterceptor, ServerCall, ServerCallHandler, Metadata, ServerInterceptors}

class LoggingInterceptor extends ServerInterceptor {
  override def interceptCall[Req, Resp](call: ServerCall[Req, Resp], headers: Metadata, next: ServerCallHandler[Req, Resp]) = {
    log.info(s"call ${call.getMethodDescriptor.getFullMethodName}")
    next.startCall(call, headers)
  }
}

trait Greeter extends _root_.scalapb.grpc.AbstractService {
  def sayHello(request: HelloRequest): scala.concurrent.Future[HelloReply]
}

object Server {
  val svc = ServerInterceptors.intercept(GreeterGrpc.bindService(impl, ec), new LoggingInterceptor)
}
`
	ents := extract(t, "custom_scala_grpc", fi("GreeterGrpc.scala", "scala", src))
	ep, ok := findBySubtype(ents, "endpoint", "RPC /Greeter/sayHello")
	if !ok {
		t.Fatalf("expected endpoint")
	}
	if ep.Props["auth_required"] == "true" {
		t.Errorf("NEGATIVE: logging interceptor must NOT credit auth, got auth_required=true")
	}
}

// TestScalaGRPC_UnwiredInterceptorNoAuth (NEGATIVE): a grpc-java auth interceptor
// that is declared but never wired (no ServerInterceptors.intercept / .intercept)
// must NOT credit auth.
func TestScalaGRPC_UnwiredInterceptorNoAuth(t *testing.T) {
	src := `
package example.grpc
import io.grpc.{ServerInterceptor, ServerCall, ServerCallHandler, Metadata, Status}

class AuthInterceptor extends ServerInterceptor {
  override def interceptCall[Req, Resp](call: ServerCall[Req, Resp], headers: Metadata, next: ServerCallHandler[Req, Resp]) = {
    if (bad(headers)) call.close(Status.UNAUTHENTICATED, new Metadata())
    next.startCall(call, headers)
  }
}

trait Greeter extends _root_.scalapb.grpc.AbstractService {
  def sayHello(request: HelloRequest): scala.concurrent.Future[HelloReply]
}
`
	ents := extract(t, "custom_scala_grpc", fi("GreeterGrpc.scala", "scala", src))
	ep, ok := findBySubtype(ents, "endpoint", "RPC /Greeter/sayHello")
	if !ok {
		t.Fatalf("expected endpoint")
	}
	if ep.Props["auth_required"] == "true" {
		t.Errorf("NEGATIVE: unwired auth interceptor must NOT credit auth, got auth_required=true")
	}
}

// TestScalaGRPC_NoInterceptorNoAuth (NEGATIVE): a plain gRPC service with no
// interceptor at all carries no auth.
func TestScalaGRPC_NoInterceptorNoAuth(t *testing.T) {
	src := `
package example.grpc
trait Greeter extends _root_.scalapb.grpc.AbstractService {
  def sayHello(request: HelloRequest): scala.concurrent.Future[HelloReply]
}
`
	ents := extract(t, "custom_scala_grpc", fi("GreeterGrpc.scala", "scala", src))
	ep, ok := findBySubtype(ents, "endpoint", "RPC /Greeter/sayHello")
	if !ok {
		t.Fatalf("expected endpoint")
	}
	if ep.Props["auth_required"] == "true" {
		t.Errorf("NEGATIVE: no interceptor must NOT credit auth, got auth_required=true")
	}
}
