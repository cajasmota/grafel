//go:build ignore
// +build ignore

// Source: https://github.com/grpc-ecosystem/go-grpc-middleware (synthetic based on real gRPC interceptor patterns) | License: Apache-2.0

package interceptors

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	authTokenKey = "authorization"
	orgIDKey     = "x-org-id"
	tracer       = "sample-server"
)

// ContextKey is a custom type for context keys to avoid collisions.
type ContextKey string

const (
	OrgIDKey ContextKey = "org_id"
	UserKey  ContextKey = "user"
)

// ============================================================
// Auth Interceptor
// ============================================================

type AuthInterceptor struct {
	jwtValidator JWTValidator
	logger       *zap.Logger
}

type JWTValidator interface {
	Validate(token string) (*Claims, error)
}

type Claims struct {
	OrgID string
	Sub   string
	Role  string
}

func NewAuthInterceptor(validator JWTValidator, logger *zap.Logger) *AuthInterceptor {
	return &AuthInterceptor{jwtValidator: validator, logger: logger}
}

func (a *AuthInterceptor) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		ctx, err := a.authorize(ctx, info.FullMethod)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

func (a *AuthInterceptor) Stream() grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, err := a.authorize(stream.Context(), info.FullMethod)
		if err != nil {
			return err
		}
		return handler(srv, &wrappedStream{stream, ctx})
	}
}

func (a *AuthInterceptor) authorize(ctx context.Context, method string) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	values := md.Get(authTokenKey)
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization token")
	}

	token := values[0]
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}

	claims, err := a.jwtValidator.Validate(token)
	if err != nil {
		a.logger.Warn("invalid token", zap.String("method", method), zap.Error(err))
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}

	ctx = context.WithValue(ctx, OrgIDKey, claims.OrgID)
	ctx = context.WithValue(ctx, UserKey, claims)
	return ctx, nil
}

// ============================================================
// Logging Interceptor
// ============================================================

func LoggingUnaryInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		orgID, _ := ctx.Value(OrgIDKey).(string)

		resp, err := handler(ctx, req)

		duration := time.Since(start)
		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}

		logger.Info("grpc",
			zap.String("method", info.FullMethod),
			zap.String("org_id", orgID),
			zap.Duration("duration", duration),
			zap.String("code", code.String()),
			zap.Error(err),
		)

		return resp, err
	}
}

// ============================================================
// Recovery Interceptor
// ============================================================

func RecoveryUnaryInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				logger.Error("panic recovered",
					zap.String("method", info.FullMethod),
					zap.Any("panic", r),
					zap.ByteString("stack", stack),
				)
				err = status.Errorf(codes.Internal, "internal server error: %v", r)
			}
		}()
		return handler(ctx, req)
	}
}

// ============================================================
// OTel Tracing Interceptor
// ============================================================

func OtelUnaryInterceptor() grpc.UnaryServerInterceptor {
	tr := otel.Tracer(tracer)
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		ctx, span := tr.Start(ctx, info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		orgID, _ := ctx.Value(OrgIDKey).(string)
		if orgID != "" {
			span.SetAttributes(attribute.String("org_id", orgID))
		}
		span.SetAttributes(attribute.String("grpc.method", info.FullMethod))

		resp, err := handler(ctx, req)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return resp, err
	}
}

// ============================================================
// Chain helpers
// ============================================================

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context {
	return w.ctx
}

// ChainUnaryInterceptors composes multiple unary interceptors left-to-right.
func ChainUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		chain := handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			interceptor := interceptors[i]
			next := chain
			chain = func(ctx context.Context, req interface{}) (interface{}, error) {
				return interceptor(ctx, req, info, next)
			}
		}
		return chain(ctx, req)
	}
}
