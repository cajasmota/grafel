// Tests for gRPC-Go server-interceptor auth detection (#4041, epic #3872).
//
// VALUE-ASSERTING: each positive asserts auth_required=true +
// auth_method=grpc_interceptor on the SPECIFIC gRPC service method served by an
// auth-enforcing interceptor (not len>0). Negatives prove a logging interceptor
// and a no-interceptor server leave the methods UNSTAMPED.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// grpcGoMethodEntity returns the SCOPE.GrpcMethod entity for grpc:Service/Method,
// or nil. Mirrors requireGRPCMethod's key shape.
func grpcGoMethodEntity(ents []types.EntityRecord, service, method string) *types.EntityRecord {
	want := "grpc:" + service + "/" + method
	for i := range ents {
		if ents[i].Kind == grpcMethodKind && ents[i].Name == want {
			return &ents[i]
		}
	}
	return nil
}

// grpcGoServiceEntity returns the SCOPE.GrpcService entity named `service`.
func grpcGoServiceEntity(ents []types.EntityRecord, service string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == grpcServiceKind && ents[i].Name == service {
			return &ents[i]
		}
	}
	return nil
}

// requireGoGRPCAuth asserts the method (and its service) carry the gRPC-Go
// interceptor auth contract: auth_required=true, auth_method=grpc_interceptor,
// auth_middleware set, and a decodable auth_policy.
func requireGoGRPCAuth(t *testing.T, ents []types.EntityRecord, service, method, wantSymbol string) {
	t.Helper()
	m := grpcGoMethodEntity(ents, service, method)
	if m == nil {
		t.Fatalf("expected GrpcMethod grpc:%s/%s; got %v", service, method, ents)
	}
	if got := m.Properties["auth_required"]; got != "true" {
		t.Errorf("method %s/%s: auth_required = %q, want true", service, method, got)
	}
	if got := m.Properties["auth_method"]; got != grpcGoAuthMethod {
		t.Errorf("method %s/%s: auth_method = %q, want %q", service, method, got, grpcGoAuthMethod)
	}
	if got := m.Properties["auth_middleware"]; got != wantSymbol {
		t.Errorf("method %s/%s: auth_middleware = %q, want %q", service, method, got, wantSymbol)
	}
	if got := m.Properties["auth_confidence"]; got != "high" {
		t.Errorf("method %s/%s: auth_confidence = %q, want high", service, method, got)
	}
	policy := DecodeAuthPolicy(m.Properties["auth_policy"])
	if !policy.Required || policy.Method != "middleware" {
		t.Errorf("method %s/%s: auth_policy = %+v, want required middleware", service, method, policy)
	}
	// The service record is credited too.
	if svc := grpcGoServiceEntity(ents, service); svc != nil {
		if svc.Properties["auth_required"] != "true" {
			t.Errorf("service %s: auth_required = %q, want true", service, svc.Properties["auth_required"])
		}
	}
}

// requireNoGoGRPCAuth asserts the method exists but carries NO auth contract —
// honest "this method is public".
func requireNoGoGRPCAuth(t *testing.T, ents []types.EntityRecord, service, method string) {
	t.Helper()
	m := grpcGoMethodEntity(ents, service, method)
	if m == nil {
		t.Fatalf("expected GrpcMethod grpc:%s/%s; got %v", service, method, ents)
	}
	for _, k := range []string{"auth_required", "auth_method", "auth_middleware", "auth_policy"} {
		if v := m.Properties[k]; v != "" {
			t.Errorf("method %s/%s: expected no auth, but %s = %q", service, method, k, v)
		}
	}
}

// TestGRPC_Go_Auth_UnaryInterceptor: a server wired with a hand-rolled auth
// interceptor (reads metadata, returns codes.Unauthenticated) stamps its
// service methods auth_required.
func TestGRPC_Go_Auth_UnaryInterceptor(t *testing.T) {
	src := `package main

import (
    "context"
    pb "example.com/proto/user"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/metadata"
    "google.golang.org/grpc/status"
)

type userServer struct{}

func (s *userServer) GetUser(ctx context.Context, req *pb.UserReq) (*pb.User, error) {
    return &pb.User{}, nil
}

func authInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
    md, ok := metadata.FromIncomingContext(ctx)
    if !ok || len(md["authorization"]) == 0 {
        return nil, status.Error(codes.Unauthenticated, "missing token")
    }
    return handler(ctx, req)
}

func main() {
    srv := grpc.NewServer(grpc.UnaryInterceptor(authInterceptor))
    pb.RegisterUserServiceServer(srv, &userServer{})
    srv.Serve(lis)
}
`
	ents, _ := runGRPCDetect(t, "go", "server.go", src)
	requireGoGRPCAuth(t, ents, "UserService", "GetUser", "authInterceptor")
}

// TestGRPC_Go_Auth_ChainUnaryInterceptor: the auth interceptor is one of
// several in grpc.ChainUnaryInterceptor; it is still credited.
func TestGRPC_Go_Auth_ChainUnaryInterceptor(t *testing.T) {
	src := `package main

import (
    "context"
    pb "example.com/proto/order"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/metadata"
    "google.golang.org/grpc/status"
)

type orderServer struct{}

func (s *orderServer) PlaceOrder(ctx context.Context, req *pb.OrderReq) (*pb.Order, error) {
    return &pb.Order{}, nil
}

func loggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
    return handler(ctx, req)
}

func jwtAuth(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
    md, _ := metadata.FromIncomingContext(ctx)
    if !verify(md) {
        return nil, status.Errorf(codes.Unauthenticated, "bad jwt")
    }
    return handler(ctx, req)
}

func main() {
    srv := grpc.NewServer(grpc.ChainUnaryInterceptor(loggingInterceptor, jwtAuth))
    pb.RegisterOrderServiceServer(srv, &orderServer{})
    srv.Serve(lis)
}
`
	ents, _ := runGRPCDetect(t, "go", "server.go", src)
	requireGoGRPCAuth(t, ents, "OrderService", "PlaceOrder", "jwtAuth")
}

// TestGRPC_Go_Auth_GrpcMiddlewareAuth: the go-grpc-middleware
// grpc_auth.UnaryServerInterceptor(authFunc) helper credits auth even though
// the authFunc body is conventionally elsewhere.
func TestGRPC_Go_Auth_GrpcMiddlewareAuth(t *testing.T) {
	src := `package main

import (
    pb "example.com/proto/admin"
    "google.golang.org/grpc"
    grpc_auth "github.com/grpc-ecosystem/go-grpc-middleware/auth"
)

type adminServer struct{}

func (s *adminServer) DeleteUser(ctx context.Context, req *pb.DelReq) (*pb.Empty, error) {
    return &pb.Empty{}, nil
}

func main() {
    srv := grpc.NewServer(
        grpc.UnaryInterceptor(grpc_auth.UnaryServerInterceptor(myAuthFunc)),
    )
    pb.RegisterAdminServiceServer(srv, &adminServer{})
    srv.Serve(lis)
}
`
	ents, _ := runGRPCDetect(t, "go", "server.go", src)
	requireGoGRPCAuth(t, ents, "AdminService", "DeleteUser", "grpc_auth.UnaryServerInterceptor")
}

// TestGRPC_Go_Auth_Negative_LoggingInterceptor: a logging-only interceptor
// (no metadata read, no Unauthenticated) does NOT stamp auth.
func TestGRPC_Go_Auth_Negative_LoggingInterceptor(t *testing.T) {
	src := `package main

import (
    "context"
    "log"
    pb "example.com/proto/user"
    "google.golang.org/grpc"
)

type userServer struct{}

func (s *userServer) GetUser(ctx context.Context, req *pb.UserReq) (*pb.User, error) {
    return &pb.User{}, nil
}

func loggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
    log.Printf("rpc: %s", info.FullMethod)
    return handler(ctx, req)
}

func main() {
    srv := grpc.NewServer(grpc.UnaryInterceptor(loggingInterceptor))
    pb.RegisterUserServiceServer(srv, &userServer{})
    srv.Serve(lis)
}
`
	ents, _ := runGRPCDetect(t, "go", "server.go", src)
	requireNoGoGRPCAuth(t, ents, "UserService", "GetUser")
}

// TestGRPC_Go_Auth_Negative_NoInterceptor: a server with no interceptor option
// at all does NOT stamp auth.
func TestGRPC_Go_Auth_Negative_NoInterceptor(t *testing.T) {
	src := `package main

import (
    "context"
    pb "example.com/proto/user"
    "google.golang.org/grpc"
)

type userServer struct{}

func (s *userServer) GetUser(ctx context.Context, req *pb.UserReq) (*pb.User, error) {
    return &pb.User{}, nil
}

func main() {
    srv := grpc.NewServer()
    pb.RegisterUserServiceServer(srv, &userServer{})
    srv.Serve(lis)
}
`
	ents, _ := runGRPCDetect(t, "go", "server.go", src)
	requireNoGoGRPCAuth(t, ents, "UserService", "GetUser")
}

// TestGRPC_Go_Auth_Negative_MetadataNoReject: an interceptor that reads
// metadata for tracing but never rejects with codes.Unauthenticated /
// PermissionDenied is NOT an auth gate.
func TestGRPC_Go_Auth_Negative_MetadataNoReject(t *testing.T) {
	src := `package main

import (
    "context"
    pb "example.com/proto/user"
    "google.golang.org/grpc"
    "google.golang.org/grpc/metadata"
)

type userServer struct{}

func (s *userServer) GetUser(ctx context.Context, req *pb.UserReq) (*pb.User, error) {
    return &pb.User{}, nil
}

func tracingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
    md, _ := metadata.FromIncomingContext(ctx)
    _ = md["x-trace-id"]
    return handler(ctx, req)
}

func main() {
    srv := grpc.NewServer(grpc.UnaryInterceptor(tracingInterceptor))
    pb.RegisterUserServiceServer(srv, &userServer{})
    srv.Serve(lis)
}
`
	ents, _ := runGRPCDetect(t, "go", "server.go", src)
	requireNoGoGRPCAuth(t, ents, "UserService", "GetUser")
}
