// Package scala — gRPC service-definition extraction for ScalaPB, zio-grpc and
// fs2-grpc (#3554, epic #3505).
//
// All three Scala gRPC stacks are driven by `protoc` / `scalapbc` code
// generation from a `.proto`. The generated Scala carries the service contract
// as a trait whose methods are the RPCs. The trait shape differs slightly per
// stack but is statically present in the generated source tree (unlike Rust's
// tonic, whose trait lives in build.rs OUT_DIR):
//
//	// ScalaPB grpc (the base async-stub trait):
//	trait Greeter extends _root_.scalapb.grpc.AbstractService {
//	  def sayHello(request: HelloRequest): scala.concurrent.Future[HelloReply]
//	  def listUsers(request: ListReq): Future[UserList]
//	}
//
//	// zio-grpc:
//	trait ZGreeter[Context] extends scalapb.zio_grpc.ZGeneratedService {
//	  def sayHello(request: HelloRequest): ZIO[Context, Status, HelloReply]
//	}
//
//	// fs2-grpc:
//	trait GreeterFs2Grpc[F[_], A] {
//	  def sayHello(request: HelloRequest, ctx: A): F[HelloReply]
//	}
//
// We synthesise one RPC endpoint per `def <rpc>(request: ReqT...): Eff[RespT]`
// method of a recognised gRPC service trait, path /<Service>/<rpc>, verb RPC,
// rpc_protocol=grpc — mirroring the Rust tonic and C/C++ grpc models so the
// cross-stack gRPC view is uniform. The request and response *message* type
// names are recovered and emitted as SCOPE.Schema DTO references. The service
// trait itself is emitted as a SCOPE.Service grpc_service entity. A
// `<Service>Grpc.stub`/`bindService`/`.<rpc>(req)` stub call site is recorded
// as a stub registration.
//
// HONEST LIMIT: the message *field shapes* live in the generated message
// case-class companions; we recover the message type NAMES from the trait
// method signatures (request param type + effect type-argument) but not their
// fields. The service<->binding wiring (`bindService` / `.scheduleAtFixedRate`)
// and cross-file stub usage are file-local. All matching is regex-based.
package scala

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
	extractor.Register("custom_scala_grpc", &scalaGRPCExtractor{})
}

type scalaGRPCExtractor struct{}

func (e *scalaGRPCExtractor) Language() string { return "custom_scala_grpc" }

var (
	// reScalaGRPCServiceTrait matches the head of a generated gRPC service
	// trait. Three accepted shapes:
	//   - any trait extending a recognised gRPC base
	//     (scalapb.grpc.AbstractService / AbstractService / ZGeneratedService /
	//     a *Grpc companion base);
	//   - a trait whose name ends in `Grpc` / `Fs2Grpc` (fs2-grpc / generated
	//     stub trait), which need not extend an explicit base.
	// Capture group 1 = trait name. The optional `[...]` type-param list and the
	// optional `extends <base>` clause are tolerated but not captured here; the
	// body is walked separately.
	reScalaGRPCServiceTrait = regexp.MustCompile(
		`\btrait\s+([A-Za-z_]\w*)\s*(?:\[[^\[\]]*(?:\[[^\]]*\][^\[\]]*)*\])?\s*(?:extends\s+[\w.]*(?:AbstractService|ZGeneratedService|GeneratedService|Fs2Grpc|Grpc)\b|(?:extends\s+[^\n{]*)?)\s*\{`,
	)

	// reScalaGRPCRpcMethod matches one RPC method declaration inside a service
	// trait body:
	//   def sayHello(request: HelloRequest): Future[HelloReply]
	//   def listUsers(request: ListReq, ctx: A): F[UserList]
	//   def stream(request: Req): ZStream[Any, Status, Resp]
	// Capture group 1 = rpc name, group 2 = the request message type (first
	// param type), group 3 = the effect-wrapped response type-argument blob
	// (everything between the effect's `[` and its matching `]`, resolved to the
	// last type argument by scalaGRPCResponseType).
	reScalaGRPCRpcMethod = regexp.MustCompile(
		`\bdef\s+([A-Za-z_]\w*)\s*\(\s*[A-Za-z_]\w*\s*:\s*([A-Za-z_][\w.]*)[^)]*\)\s*:\s*[A-Za-z_][\w.]*\s*\[\s*([^\n]+?)\]`,
	)

	// reScalaGRPCStub matches a generated-stub access / service-binding site:
	//   GreeterGrpc.stub(channel) / GreeterGrpc.blockingStub(channel)
	//   GreeterGrpc.bindService(impl, ec)
	//   GreeterFs2Grpc.bindServiceResource(impl)
	// Capture group 1 = the `<Service>Grpc`/`<Service>Fs2Grpc` companion,
	// group 2 = the accessor (stub / blockingStub / bindService / ...).
	reScalaGRPCStub = regexp.MustCompile(
		`\b([A-Za-z_]\w*(?:Fs2Grpc|Grpc))\s*\.\s*(stub|blockingStub|asyncStub|bindService|bindServiceResource|client)\b`,
	)

	// reScalaGRPCInterceptorClass matches the head of a class/object declaring a
	// grpc-java ServerInterceptor (used by scalapb-grpc & fs2-grpc, which both ride
	// the underlying grpc-java io.grpc.ServerInterceptor). Capture group 1 = the
	// interceptor class/object name. Both `extends ServerInterceptor` and
	// `extends io.grpc.ServerInterceptor` are tolerated.
	reScalaGRPCInterceptorClass = regexp.MustCompile(
		`\b(?:class|object)\s+([A-Za-z_]\w*)\b[^\n{]*\bextends\b[^\n{]*\b(?:io\.grpc\.)?ServerInterceptor\b`,
	)

	// reScalaGRPCZioInterceptor matches a zio-grpc auth point: either a class/object
	// extending ZServerInterceptor, or a `transformContextZIO`/`transformContext`
	// combinator (the zio-grpc context transform where auth rejection lives).
	// Capture group 1 (class arm) = the interceptor name; the transform arm has no
	// capture and is matched separately.
	reScalaGRPCZioInterceptorClass = regexp.MustCompile(
		`\b(?:class|object)\s+([A-Za-z_]\w*)\b[^\n{]*\bextends\b[^\n{]*\bZServerInterceptor\b`,
	)
	reScalaGRPCZioTransform = regexp.MustCompile(
		`\btransformContextZIO\b|\btransformContext\b`,
	)

	// reScalaGRPCWired matches the wiring of an interceptor onto a service /
	// ServerBuilder. Capture group 1 = the referenced interceptor symbol when the
	// arg is `new X` / a bare identifier.
	//   ServerInterceptors.intercept(svc, new AuthInterceptor)
	//   ServerInterceptors.interceptForward(svc, authInterceptor)
	//   builder.intercept(new AuthInterceptor)
	//   .addService(ServerInterceptors.intercept(svc, AuthInterceptor))
	reScalaGRPCWiredIntercept = regexp.MustCompile(
		`\b(?:ServerInterceptors\s*\.\s*intercept(?:Forward)?|\.\s*intercept)\s*\(`,
	)

	// reScalaGRPCAuthReject matches a gRPC auth rejection inside an interceptor /
	// context-transform body. grpc-java spells it Status.UNAUTHENTICATED /
	// PERMISSION_DENIED; zio-grpc spells it Status.unauthenticated /
	// permissionDenied or io.grpc.Status.UNAUTHENTICATED.
	reScalaGRPCAuthReject = regexp.MustCompile(
		`\bUNAUTHENTICATED\b|\bPERMISSION_DENIED\b|\bunauthenticated\b|\bpermissionDenied\b|\bUnauthenticated\b`,
	)
)

func (e *scalaGRPCExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/scala")
	_, span := tracer.Start(ctx, "indexer.scala_grpc.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "scalapb-grpc"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "scala" {
		return nil, nil
	}

	src := string(file.Content)

	// File-signal gate: require a Scala gRPC marker so the extractor is a no-op
	// on plain Scala / tapir / akka files.
	if !strings.Contains(src, "scalapb") &&
		!strings.Contains(src, "AbstractService") &&
		!strings.Contains(src, "ZGeneratedService") &&
		!strings.Contains(src, "Fs2Grpc") &&
		!strings.Contains(src, "zio_grpc") &&
		!strings.Contains(src, "Grpc") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// File-level gRPC auth verdict: an auth-enforcing interceptor wired onto the
	// server/service guards every RPC method of the services in this file. The
	// interceptor→service binding is file-local (mirrors the rest of the synthesis).
	authInfo := scalaGRPCResolveAuth(src)

	// 1. Service traits → one RPC endpoint per def method + grpc_service entity.
	for _, m := range reScalaGRPCServiceTrait.FindAllStringSubmatchIndex(src, -1) {
		service := src[m[2]:m[3]]
		// Skip obvious non-gRPC traits whose name doesn't end in Grpc and which
		// don't extend a recognised base. The regex already requires one of those
		// two when it matched the `extends ...AbstractService|...Grpc` arm, but the
		// permissive `(?:extends [^\n{]*)?` arm can match a plain trait; gate it
		// here by requiring a gRPC signal in the header slice OR a Grpc-suffixed
		// name.
		header := src[m[0]:m[1]]
		grpcTrait := strings.HasSuffix(service, "Grpc") || strings.HasSuffix(service, "Fs2Grpc") ||
			strings.Contains(header, "AbstractService") ||
			strings.Contains(header, "ZGeneratedService") ||
			strings.Contains(header, "GeneratedService") ||
			strings.Contains(header, "Fs2Grpc")
		if !grpcTrait {
			continue
		}

		bodyStart, bodyEnd := scalaGRPCBlockBody(src, m[1]-1)
		if bodyStart < 0 {
			continue
		}
		body := src[bodyStart:bodyEnd]

		// Canonical service name strips a generated `Grpc`/`Fs2Grpc`/leading `Z`
		// decoration so /<Service>/<rpc> is the proto service name.
		svcName := scalaGRPCCanonicalService(service)

		var rpcCount int
		for _, rm := range reScalaGRPCRpcMethod.FindAllStringSubmatchIndex(body, -1) {
			rpc := body[rm[2]:rm[3]]
			reqType := normalizeScalaType(body[rm[4]:rm[5]])
			respType := scalaGRPCResponseType(body[rm[6]:rm[7]])
			methodOff := bodyStart + rm[0]

			path := "/" + svcName + "/" + rpc
			name := "RPC " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, methodOff))
			setProps(&ent, "framework", "scalapb-grpc",
				"provenance", "INFERRED_FROM_SCALA_GRPC_RPC",
				"http_method", "RPC", "verb", "RPC",
				"route_path", path, "rpc_protocol", "grpc",
				"grpc_service", svcName, "grpc_method", rpc,
				"service_trait", service,
				"request_message", reqType,
				"handler_name", svcName+"."+rpc)
			if respType != "" {
				setProps(&ent, "response_message", respType)
			}
			if authInfo.required {
				scalaGRPCStampAuth(&ent, authInfo)
			}
			add(ent)

			scalaGRPCAddDTO(add, reqType, "request", file, lineOf(src, methodOff))
			if respType != "" {
				scalaGRPCAddDTO(add, respType, "response", file, lineOf(src, methodOff))
			}
			rpcCount++
		}

		// Only emit the service entity when the trait actually declared RPCs —
		// this keeps a non-RPC trait that happened to match the permissive header
		// arm from producing a phantom service.
		if rpcCount == 0 {
			continue
		}
		svcEnt := makeEntity("grpc_service:"+service, "SCOPE.Service", "grpc_service", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&svcEnt, "framework", "scalapb-grpc",
			"provenance", "INFERRED_FROM_SCALA_GRPC_SERVICE",
			"grpc_service", svcName, "service_trait", service,
			"rpc_protocol", "grpc")
		if authInfo.required {
			scalaGRPCStampAuth(&svcEnt, authInfo)
		}
		add(svcEnt)
	}

	// 2. Stub / bindService call sites → SCOPE.Component grpc_stub registration.
	for _, m := range reScalaGRPCStub.FindAllStringSubmatchIndex(src, -1) {
		companion := src[m[2]:m[3]]
		accessor := src[m[4]:m[5]]
		svcName := scalaGRPCCanonicalService(companion)
		ent := makeEntity("grpc_stub:"+companion+"."+accessor, "SCOPE.Component", "grpc_stub", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "scalapb-grpc",
			"provenance", "INFERRED_FROM_SCALA_GRPC_STUB",
			"grpc_service", svcName, "grpc_companion", companion,
			"grpc_accessor", accessor, "rpc_protocol", "grpc")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// scalaGRPCAuthInfo is the resolved file-level gRPC auth verdict for a Scala
// gRPC source file. Scala gRPC auth lives in interceptors that are wired to a
// ServerBuilder / service rather than per-method annotations, so the binding is
// file-local — same scope as the rest of the Scala gRPC synthesis.
type scalaGRPCAuthInfo struct {
	required   bool
	symbol     string // the recognised interceptor symbol (MCP signal-1)
	method     string // auth_method, always grpc_interceptor here
	confidence string
}

// scalaGRPCResolveAuth decides whether the gRPC services in this file are guarded
// by an auth-enforcing interceptor. Two stacks are recognised:
//
//   - grpc-java ServerInterceptor (scalapb-grpc / fs2-grpc ride the underlying
//     io.grpc.ServerInterceptor): a class/object `extends ServerInterceptor`
//     whose body rejects with `Status.UNAUTHENTICATED` / `PERMISSION_DENIED`,
//     AND is actually WIRED via `ServerInterceptors.intercept(...)` /
//     `.intercept(...)`. Presence-without-wiring does NOT credit.
//
//   - zio-grpc: a `ZServerInterceptor` class OR a `transformContextZIO` /
//     `transformContext` combinator whose surrounding window rejects with
//     `Status.unauthenticated` / `permissionDenied`. The transform IS the wiring
//     (it is applied onto the service), so no separate `.intercept` is required.
//
// A non-auth interceptor (logging/tracing — no UNAUTHENTICATED/PERMISSION_DENIED
// reject) and a present-but-unwired grpc-java interceptor both return required=false.
func scalaGRPCResolveAuth(src string) scalaGRPCAuthInfo {
	wired := reScalaGRPCWiredIntercept.MatchString(src)

	// 1. grpc-java ServerInterceptor arm (scalapb-grpc / fs2-grpc).
	for _, m := range reScalaGRPCInterceptorClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		bodyStart, bodyEnd := scalaGRPCBlockBody(src, m[1]-1)
		if bodyStart < 0 {
			continue
		}
		body := src[bodyStart:bodyEnd]
		if !reScalaGRPCAuthReject.MatchString(body) {
			continue // logging/tracing interceptor — no auth reject.
		}
		// Require the interceptor to be wired into a server/service. Without a
		// wiring site an auth interceptor class is dead and credits nothing.
		if !wired {
			continue
		}
		return scalaGRPCAuthInfo{required: true, symbol: name, method: "grpc_interceptor", confidence: "high"}
	}

	// 2. zio-grpc ZServerInterceptor class arm.
	for _, m := range reScalaGRPCZioInterceptorClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		bodyStart, bodyEnd := scalaGRPCBlockBody(src, m[1]-1)
		if bodyStart < 0 {
			continue
		}
		body := src[bodyStart:bodyEnd]
		if !reScalaGRPCAuthReject.MatchString(body) {
			continue
		}
		return scalaGRPCAuthInfo{required: true, symbol: name, method: "grpc_interceptor", confidence: "high"}
	}

	// 3. zio-grpc transformContextZIO arm: the context transform IS the wiring.
	// Require an auth reject inside the window following the transform combinator.
	for _, m := range reScalaGRPCZioTransform.FindAllStringIndex(src, -1) {
		end := m[1] + 400
		if end > len(src) {
			end = len(src)
		}
		window := src[m[0]:end]
		if reScalaGRPCAuthReject.MatchString(window) {
			return scalaGRPCAuthInfo{required: true, symbol: "transformContextZIO", method: "grpc_interceptor", confidence: "high"}
		}
	}

	return scalaGRPCAuthInfo{}
}

// scalaGRPCStampAuth writes the auth property contract onto a gRPC endpoint /
// service entity. Mirrors the cross-language auth key set (auth_required +
// auth_method + auth_middleware MCP signal-1 + auth_confidence) so
// archigraph_auth_coverage fires on the gRPC methods guarded by the interceptor.
func scalaGRPCStampAuth(e *types.EntityRecord, info scalaGRPCAuthInfo) {
	setProps(e,
		"auth_required", "true",
		"auth_method", info.method,
		"auth_middleware", info.symbol, // MCP signal-1 key
		"auth_confidence", info.confidence)
}

// scalaGRPCAddDTO emits a SCOPE.Schema DTO reference for a gRPC message type.
func scalaGRPCAddDTO(add func(types.EntityRecord), msg, role string, file extractor.FileInput, line int) {
	if msg == "" {
		return
	}
	ent := makeEntity("grpc_dto:"+msg, "SCOPE.Schema", "dto", file.Path, file.Language, line)
	setProps(&ent, "framework", "scalapb-grpc",
		"provenance", "INFERRED_FROM_SCALA_GRPC_MESSAGE",
		"dto_name", msg, "grpc_message_role", role, "rpc_protocol", "grpc")
	add(ent)
}

// scalaGRPCCanonicalService normalises a generated service/companion name to the
// proto service name: strip a trailing `Fs2Grpc`/`Grpc` companion suffix and a
// leading `Z` (zio-grpc `ZGreeter` → `Greeter`).
func scalaGRPCCanonicalService(name string) string {
	name = strings.TrimSuffix(name, "Fs2Grpc")
	name = strings.TrimSuffix(name, "Grpc")
	if strings.HasPrefix(name, "Z") && len(name) > 1 {
		// Only strip a leading Z when the next rune is upper-case (ZGreeter), not
		// for an all-name like "Zoo".
		if c := name[1]; c >= 'A' && c <= 'Z' {
			name = name[1:]
		}
	}
	return name
}

// scalaGRPCResponseType resolves the response message type from the effect
// type-argument blob captured for a method, e.g.:
//
//	"HelloReply"                  -> HelloReply        (Future[HelloReply])
//	"Context, Status, HelloReply" -> HelloReply        (ZIO[Context,Status,Reply])
//	"List[User]"                  -> List              (head type)
//
// The response message is the LAST top-level type argument (ZIO/ZStream put the
// success value last; Future/F have a single argument).
func scalaGRPCResponseType(blob string) string {
	args := splitTopLevelCommas(blob)
	last := strings.TrimSpace(args[len(args)-1])
	return normalizeScalaType(last)
}

// scalaGRPCBlockBody returns the [start,end) byte range of the trait body whose
// opening `{` is at or after openBrace. Brace-balanced.
func scalaGRPCBlockBody(src string, openBrace int) (int, int) {
	open := strings.IndexByte(src[openBrace:], '{')
	if open < 0 {
		return -1, -1
	}
	open += openBrace
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
