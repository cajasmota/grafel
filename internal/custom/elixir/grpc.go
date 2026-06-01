package elixir

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_elixir_grpc", &grpcExtractor{})
}

// grpcExtractor recognises elixir-grpc (https://github.com/elixir-grpc/grpc).
//
// Service module (generated from a .proto):
//
//	defmodule Helloworld.Greeter.Service do
//	  use GRPC.Service, name: "helloworld.Greeter"
//	  rpc :SayHello, HelloRequest, HelloReply
//	end
//
// Server implementation:
//
//	defmodule Helloworld.Greeter.Server do
//	  use GRPC.Server, service: Helloworld.Greeter.Service
//	  def say_hello(request, _stream), do: ...
//	end
//
// Client stub:
//
//	defmodule Helloworld.Greeter.Stub do
//	  use GRPC.Stub, service: Helloworld.Greeter.Service
//	end
//
// Emitted entities:
//   - SCOPE.GrpcService : a `use GRPC.Server` or `use GRPC.Service` module.
//   - SCOPE.GrpcMethod  : each `rpc :Method, Req, Resp` declaration. The
//     cross-repo identity `grpc:<Service>/<Method>` matches the linker
//     convention shared with the other-language gRPC extractors (#725).
type grpcExtractor struct{}

func (e *grpcExtractor) Language() string { return "custom_elixir_grpc" }

var (
	// use GRPC.Server, service: Helloworld.Greeter.Service
	reGRPCServer = regexp.MustCompile(
		`(?m)^\s*use\s+GRPC\.Server\b([^\n]*)`,
	)
	// use GRPC.Service, name: "helloworld.Greeter"
	reGRPCService = regexp.MustCompile(
		`(?m)^\s*use\s+GRPC\.Service\b([^\n]*)`,
	)
	// use GRPC.Stub, service: Helloworld.Greeter.Service
	reGRPCStub = regexp.MustCompile(
		`(?m)^\s*use\s+GRPC\.Stub\b([^\n]*)`,
	)
	// rpc :SayHello, HelloRequest, HelloReply
	// rpc :ServerStream, HelloRequest, stream(HelloReply)
	reGRPCRpc = regexp.MustCompile(
		`(?m)^\s*rpc\s+:(\w+)\s*,\s*([\w.()]+)\s*,\s*([\w.()]+)`,
	)
	// service: Foo.Bar.Service  — option value capture.
	reGRPCServiceOpt = regexp.MustCompile(`service:\s*([A-Z][\w.]+)`)
	// name: "helloworld.Greeter" — option value capture.
	reGRPCNameOpt = regexp.MustCompile(`name:\s*"([^"]+)"`)
)

// grpcServiceName derives the canonical service name for cross-repo identity.
// Prefers the proto `name:` option; otherwise falls back to the module name
// with a trailing ".Service" / ".Server" / ".Stub" suffix stripped.
func grpcServiceName(opts, module string) string {
	if nm := reGRPCNameOpt.FindStringSubmatch(opts); nm != nil {
		return nm[1]
	}
	if so := reGRPCServiceOpt.FindStringSubmatch(opts); so != nil {
		return strings.TrimSuffix(so[1], ".Service")
	}
	module = strings.TrimSuffix(module, ".Service")
	module = strings.TrimSuffix(module, ".Server")
	module = strings.TrimSuffix(module, ".Stub")
	return module
}

func (e *grpcExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/elixir")
	_, span := tracer.Start(ctx, "indexer.grpc_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "grpc"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "elixir" {
		return nil, nil
	}

	src := string(file.Content)

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	moduleAt := func(off int) string {
		if cm := rePhoenixModuleDecl.FindAllStringSubmatch(src[:off], -1); len(cm) > 0 {
			return cm[len(cm)-1][1]
		}
		return ""
	}

	// 1. Server modules -> SCOPE.GrpcService (role=server).
	for _, m := range reGRPCServer.FindAllStringSubmatchIndex(src, -1) {
		opts := src[m[2]:m[3]]
		module := moduleAt(m[0])
		svc := grpcServiceName(opts, module)
		ent := makeEntity(svc, "SCOPE.GrpcService", "server", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "grpc",
			"provenance", "INFERRED_FROM_GRPC_SERVER",
			"grpc_role", "server",
			"service_name", svc,
			"module", module)
		add(ent)
	}

	// 2. Service definition modules -> SCOPE.GrpcService + rpc methods.
	for _, m := range reGRPCService.FindAllStringSubmatchIndex(src, -1) {
		opts := src[m[2]:m[3]]
		module := moduleAt(m[0])
		svc := grpcServiceName(opts, module)
		ent := makeEntity(svc, "SCOPE.GrpcService", "definition", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "grpc",
			"provenance", "INFERRED_FROM_GRPC_SERVICE",
			"grpc_role", "definition",
			"service_name", svc,
			"module", module)
		add(ent)

		// rpc declarations belong to the service definition.
		for _, r := range reGRPCRpc.FindAllStringSubmatchIndex(src, -1) {
			method := src[r[2]:r[3]]
			reqType := strings.TrimSpace(src[r[4]:r[5]])
			respType := strings.TrimSpace(src[r[6]:r[7]])
			id := "grpc:" + svc + "/" + method
			mEnt := makeEntity(id, "SCOPE.GrpcMethod", "rpc", file.Path, file.Language, lineOf(src, r[0]))
			setProps(&mEnt, "framework", "grpc",
				"provenance", "INFERRED_FROM_GRPC_RPC",
				"service_name", svc,
				"method_name", method,
				"request_message", grpcStripStream(reqType),
				"response_message", grpcStripStream(respType),
				"streaming", grpcStreamingMode(reqType, respType))
			add(mEnt)
		}
	}

	// 3. Client stub modules -> SCOPE.GrpcService (role=client).
	for _, m := range reGRPCStub.FindAllStringSubmatchIndex(src, -1) {
		opts := src[m[2]:m[3]]
		module := moduleAt(m[0])
		svc := grpcServiceName(opts, module)
		ent := makeEntity(svc, "SCOPE.GrpcService", "client", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "grpc",
			"provenance", "INFERRED_FROM_GRPC_STUB",
			"grpc_role", "client",
			"service_name", svc,
			"module", module)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// grpcStripStream removes a `stream(...)` wrapper from a message type, leaving
// the bare message name (e.g. `stream(HelloReply)` -> `HelloReply`).
func grpcStripStream(t string) string {
	t = strings.TrimSpace(t)
	if strings.HasPrefix(t, "stream(") && strings.HasSuffix(t, ")") {
		return strings.TrimSpace(t[len("stream(") : len(t)-1])
	}
	return t
}

// grpcStreamingMode classifies an rpc as unary / server_streaming /
// client_streaming / bidi_streaming based on the stream() wrappers.
func grpcStreamingMode(req, resp string) string {
	cs := strings.HasPrefix(strings.TrimSpace(req), "stream(")
	ss := strings.HasPrefix(strings.TrimSpace(resp), "stream(")
	switch {
	case cs && ss:
		return "bidi_streaming"
	case ss:
		return "server_streaming"
	case cs:
		return "client_streaming"
	default:
		return "unary"
	}
}
