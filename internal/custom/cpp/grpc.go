package cpp

// grpc.go — gRPC C++ (grpc++) service / RPC extractor.
//
// A gRPC C++ service is implemented by subclassing the generated
// `<Service>::Service` base and overriding each RPC method. The method
// signature carries the request and response message types:
//
//	class GreeterServiceImpl final : public Greeter::Service {
//	    Status SayHello(ServerContext* ctx,
//	                    const HelloRequest* req,
//	                    HelloReply* resp) override { ... }
//	};
//
//	ServerBuilder builder;
//	builder.RegisterService(&service);              // registration
//
//	auto stub = Greeter::NewStub(channel);          // client stub
//	stub->SayHello(&context, request, &reply);
//
// Each overridden RPC method maps to a synthetic endpoint with verb RPC and the
// canonical gRPC path /<Service>/<Method>. The request/response message types
// are emitted as SCOPE.Schema DTO references. `RegisterService(&svc)` is the
// registration site; `<Service>::NewStub(channel)` is the client-stub site.
//
// Streaming variants are recognised via the gRPC reader/writer stream argument
// types (ServerWriter / ServerReader / ServerReaderWriter), and the streaming
// kind is stamped on the endpoint.
//
// HONEST LIMIT: the service base trait and the message structs are emitted by
// protoc into *.pb.h / *.grpc.pb.h. We recover the service name, method names,
// and request/response message *names* from the hand-written service impl and
// the stub call sites; full message field shapes live in the generated structs
// (covered by the protobuf extractor when those headers are present). Hence
// shape-extraction cells are honest-partial.

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
	extractor.Register("custom_cpp_grpc", &cppGrpcExtractor{})
}

type cppGrpcExtractor struct{}

func (e *cppGrpcExtractor) Language() string { return "custom_cpp_grpc" }

var (
	// class Foo final : public Greeter::Service { ... }
	// class Foo : public package::Greeter::Service { ... }
	// Capture group 1 = impl class name, group 2 = fully-qualified service base
	// (e.g. "Greeter::Service" or "routeguide::RouteGuide::Service").
	reCppGrpcServiceImpl = regexp.MustCompile(
		`(?m)\bclass\s+([A-Za-z_]\w*)\b[^{;]*?:\s*(?:public\s+)?([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*::Service)\b`,
	)

	// An overridden RPC method inside the service impl body. gRPC RPC methods
	// return `Status` (grpc::Status) and end with `override`. Captures:
	//   1 = method name
	//   2 = full argument list (parsed further for req/resp/stream types)
	reCppGrpcRpcMethod = regexp.MustCompile(
		`(?m)\b(?:::grpc::|grpc::)?Status\s+([A-Za-z_]\w*)\s*\(([^;{]*?)\)\s*override\b`,
	)

	// builder.RegisterService(&service) / RegisterService(&svc)
	// Capture group 1 = the registered service variable.
	reCppGrpcRegisterService = regexp.MustCompile(
		`(?m)\bRegisterService\s*\(\s*&?\s*([A-Za-z_]\w*)\b`,
	)

	// Greeter::NewStub(channel) / package::Greeter::NewStub(channel)
	// Capture group 1 = fully-qualified service (strip trailing ::NewStub).
	reCppGrpcNewStub = regexp.MustCompile(
		`(?m)\b([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)::NewStub\s*\(`,
	)

	// A `const Foo* name` / `Foo* name` / `const Foo& name` request-or-response
	// argument inside the RPC arg list. Captures the bare message type name.
	reCppGrpcMsgArg = regexp.MustCompile(
		`(?:const\s+)?([A-Za-z_]\w*)\s*[*&]\s*[A-Za-z_]\w*`,
	)

	// Streaming stream-arg type: ServerWriter<T> / ServerReader<T> /
	// ServerReaderWriter<W,R>. Captures the writer/reader kind (group 1).
	reCppGrpcStreamArg = regexp.MustCompile(
		`(?:::grpc::|grpc::)?(ServerReaderWriter|ServerWriter|ServerReader)\s*<`,
	)
)

func (e *cppGrpcExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.cpp_grpc.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "grpc++"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "cpp" {
		return nil, nil
	}
	src := string(file.Content)

	// File-signal gate: require a gRPC marker so the extractor is a no-op on
	// non-gRPC C++ files.
	if !strings.Contains(src, "::Service") &&
		!strings.Contains(src, "RegisterService") &&
		!strings.Contains(src, "NewStub") {
		return nil, nil
	}

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

	// 1. Service impl class -> one RPC endpoint per overridden method.
	for _, m := range reCppGrpcServiceImpl.FindAllStringSubmatchIndex(src, -1) {
		implClass := src[m[2]:m[3]]
		serviceBase := src[m[4]:m[5]]
		// "pkg::Greeter::Service" -> service "Greeter".
		service := cppGrpcServiceName(serviceBase)

		bodyStart, bodyEnd := cppBraceBody(src, m[1])
		if bodyStart < 0 {
			continue
		}
		body := src[bodyStart:bodyEnd]

		for _, rm := range reCppGrpcRpcMethod.FindAllStringSubmatchIndex(body, -1) {
			method := body[rm[2]:rm[3]]
			argList := body[rm[4]:rm[5]]
			methodOff := bodyStart + rm[0]

			reqType, respType, streamKind := cppGrpcParseArgs(argList)

			path := "/" + service + "/" + method
			name := "RPC " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, methodOff))
			setProps(&ent, "framework", "grpc++",
				"provenance", "INFERRED_FROM_GRPC_RPC",
				"http_method", "RPC", "verb", "RPC",
				"route_path", path, "rpc_protocol", "grpc",
				"grpc_service", service, "grpc_method", method,
				"impl_type", implClass,
				"handler_name", implClass+"."+method)
			if reqType != "" {
				setProps(&ent, "request_message", reqType)
			}
			if respType != "" {
				setProps(&ent, "response_message", respType)
			}
			if streamKind != "" {
				setProps(&ent, "streaming", streamKind)
			}
			add(ent)

			if reqType != "" {
				cppGrpcAddDTO(add, reqType, "request", file, lineOf(src, methodOff))
			}
			if respType != "" {
				cppGrpcAddDTO(add, respType, "response", file, lineOf(src, methodOff))
			}
		}
	}

	// 2. RegisterService(&svc) -> SCOPE.Service registration.
	for _, m := range reCppGrpcRegisterService.FindAllStringSubmatchIndex(src, -1) {
		svcVar := src[m[2]:m[3]]
		ent := makeEntity("grpc_service:"+svcVar, "SCOPE.Service", "grpc_service", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "grpc++",
			"provenance", "INFERRED_FROM_GRPC_REGISTER_SERVICE",
			"service_var", svcVar, "registration", "RegisterService")
		add(ent)
	}

	// 3. <Service>::NewStub(channel) -> client stub site.
	for _, m := range reCppGrpcNewStub.FindAllStringSubmatchIndex(src, -1) {
		qualified := src[m[2]:m[3]]
		service := cppGrpcLastSegment(qualified)
		ent := makeEntity("grpc_stub:"+service, "SCOPE.Service", "grpc_stub", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "grpc++",
			"provenance", "INFERRED_FROM_GRPC_NEW_STUB",
			"grpc_service", service, "client_role", "stub")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// cppGrpcServiceName turns a fully-qualified "pkg::Greeter::Service" base into
// the bare service name "Greeter".
func cppGrpcServiceName(base string) string {
	base = strings.TrimSuffix(base, "::Service")
	return cppGrpcLastSegment(base)
}

// cppGrpcLastSegment returns the final ::-separated identifier.
func cppGrpcLastSegment(s string) string {
	if idx := strings.LastIndex(s, "::"); idx >= 0 {
		return s[idx+2:]
	}
	return s
}

// cppGrpcParseArgs parses an RPC method argument list and returns the request
// message type, response message type, and (for streaming RPCs) the stream
// kind. The gRPC C++ unary signature is:
//
//	(ServerContext* ctx, const Req* request, Resp* response)
//
// Streaming forms substitute a ServerWriter<Resp> / ServerReader<Req> /
// ServerReaderWriter<Resp,Req> for one of the message args.
func cppGrpcParseArgs(argList string) (reqType, respType, streamKind string) {
	args := cppGrpcSplitArgs(argList)
	// Drop the leading ServerContext* (or grpc::ServerContext*) argument.
	var msgArgs []string
	for _, a := range args {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if strings.Contains(a, "ServerContext") || strings.Contains(a, "ClientContext") {
			continue
		}
		msgArgs = append(msgArgs, a)
	}

	for _, a := range msgArgs {
		if sm := reCppGrpcStreamArg.FindStringSubmatch(a); sm != nil {
			kind := sm[1]
			streamKind = cppGrpcStreamKind(kind)
			// Extract the streamed message type(s) from the angle brackets.
			inner := cppGrpcAngleInner(a)
			parts := strings.Split(inner, ",")
			switch kind {
			case "ServerWriter":
				// server-streaming: response messages flow out.
				if len(parts) >= 1 {
					respType = cppGrpcBareType(parts[0])
				}
			case "ServerReader":
				// client-streaming: request messages flow in.
				if len(parts) >= 1 {
					reqType = cppGrpcBareType(parts[0])
				}
			case "ServerReaderWriter":
				// bidi: <Response, Request>.
				if len(parts) >= 1 {
					respType = cppGrpcBareType(parts[0])
				}
				if len(parts) >= 2 {
					reqType = cppGrpcBareType(parts[1])
				}
			}
			continue
		}
		// Plain message pointer/reference arg.
		isConst := strings.Contains(a, "const")
		if sm := reCppGrpcMsgArg.FindStringSubmatch(a); sm != nil {
			t := sm[1]
			if isConst && reqType == "" {
				reqType = t
			} else if !isConst && respType == "" {
				respType = t
			} else if reqType == "" {
				reqType = t
			} else if respType == "" {
				respType = t
			}
		}
	}
	return reqType, respType, streamKind
}

// cppGrpcStreamKind maps a gRPC stream-arg type to a canonical streaming label.
func cppGrpcStreamKind(kind string) string {
	switch kind {
	case "ServerWriter":
		return "server_streaming"
	case "ServerReader":
		return "client_streaming"
	case "ServerReaderWriter":
		return "bidi_streaming"
	}
	return ""
}

// cppGrpcAngleInner returns the text between the first '<' and its matching '>'.
func cppGrpcAngleInner(s string) string {
	open := strings.IndexByte(s, '<')
	if open < 0 {
		return ""
	}
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				return s[open+1 : i]
			}
		}
	}
	return s[open+1:]
}

// cppGrpcBareType strips namespace qualifiers / whitespace from a type token,
// returning the final identifier (e.g. " routeguide::Point " -> "Point").
func cppGrpcBareType(s string) string {
	s = strings.TrimSpace(s)
	// Strip any pointer/ref decorations.
	s = strings.TrimRight(s, "*& \t")
	return cppGrpcLastSegment(s)
}

// cppGrpcSplitArgs splits a C++ argument list on top-level commas (ignoring
// commas nested inside <...> template brackets).
func cppGrpcSplitArgs(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<', '(':
			depth++
		case '>', ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// cppGrpcAddDTO emits a SCOPE.Schema DTO reference for a gRPC message type.
func cppGrpcAddDTO(add func(types.EntityRecord), msg, role string, file extractor.FileInput, line int) {
	if msg == "" {
		return
	}
	ent := makeEntity("grpc_dto:"+msg, "SCOPE.Schema", "dto", file.Path, file.Language, line)
	setProps(&ent, "framework", "grpc++",
		"provenance", "INFERRED_FROM_GRPC_MESSAGE",
		"dto_name", msg, "grpc_message_role", role, "rpc_protocol", "grpc")
	add(ent)
}

// cppBraceBody returns the byte range [start,end) of the brace-balanced body
// beginning at the '{' that follows headerEnd. Returns (-1,-1) when no opening
// brace is found.
func cppBraceBody(src string, headerEnd int) (int, int) {
	open := strings.IndexByte(src[headerEnd:], '{')
	if open < 0 {
		return -1, -1
	}
	open += headerEnd
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return open + 1, i
			}
		}
	}
	return -1, -1
}
