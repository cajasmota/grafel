// gRPC service definitions + client/server cross-repo edges — #725.
//
// This pass detects gRPC server implementations and client call sites in four
// languages (Java/Kotlin, Go, Python, Node/TypeScript) and emits:
//
//   - SCOPE.GrpcService entities for each server-side service implementation.
//   - SCOPE.GrpcMethod entities for each handler method on that service.
//   - GRPC_IMPLEMENTS edges: handler method → GrpcMethod (server side).
//   - GRPC_HANDLES edges: client call site → GrpcMethod (client side).
//
// Cross-repo matching follows the same strategy used by the HTTP synthesis
// pass (#534) and the Kafka pass (#726): both the client and server side emit
// a GrpcMethod entity keyed by `grpc:<ServiceName>/<MethodName>`. The
// existing import-channel linker will naturally link the two sides when they
// share the same entity ID — no new linker code required.
//
// # Java / Kotlin
//
// Server: classes annotated with @GrpcService (Quarkus Mutiny) or that
// extend ServiceGrpc.ServiceImplBase (standard protobuf-generated stubs).
// Each overriding method is treated as a handler.
//
// Client: ManagedChannelBuilder / Grpc.*Stub construction, followed by
// `.getUser(req)` style call sites on a known stub variable.
//
// Also detects Quarkus @GrpcClient injection: `@GrpcClient("svc") Greeter
// greeter; greeter.hello(req)`.
//
// # Go
//
// Server: `pb.RegisterServiceServer(srv, &Impl{})` call sites. The
// implementation type becomes the GrpcService entity.
//
// Client: `pb.NewServiceClient(conn)` → captured to a variable; then
// `stub.Method(ctx, req)` is the GRPC_HANDLES edge source.
//
// # Python
//
// Server: `pb2_grpc.add_ServiceServicer_to_server(impl, server)`. The
// implementation class becomes the GrpcService entity.
//
// Client: `pb2_grpc.ServiceStub(channel)` → captured; then `stub.Method(req)`
// is the call site.
//
// # Node / TypeScript
//
// Server: `server.addService(proto.Service.service, impl)`. The
// implementation object becomes the GrpcService entity.
//
// Client: `new proto.Service(addr, creds)` → captured; then
// `stub.method(req, cb)` is the call site.
//
// # Beyond the minimum
//
// Streaming variants (unary / server-streaming / client-streaming / bidi)
// are recorded on the GRPC_HANDLES / GRPC_IMPLEMENTS edges via the
// `streaming` property. Detection is heuristic:
//
//   - Go: if the method signature contains `stream.Send` or `stream.Recv`,
//     the edge records the appropriate streaming kind.
//   - Java: if the handler parameter is StreamObserver<Req> (not just
//     StreamObserver<Resp>), it is bidi or client-streaming.
//   - Python / Node: not currently distinguished (default "unary").
//
// gRPC-Gateway: when a Go server method carries `// @grpc-gateway:` or
// `gateway.RegisterServiceHandlerServer` call, the service is also
// marked with `has_gateway=true`.
//
// Server reflection: when `reflection.Register(srv)` appears in the same
// file as a gRPC server registration, GrpcService entities from that file
// are marked `reflection=true`.
//
// Refs #725.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// grpcServiceKind is the entity kind for a gRPC service implementation.
const grpcServiceKind = "SCOPE.GrpcService"

// grpcMethodKind is the entity kind for a single gRPC RPC handler.
const grpcMethodKind = "SCOPE.GrpcMethod"

// grpcImplementsEdgeKind is emitted from a handler method → GrpcMethod
// (server side declares it handles this RPC).
const grpcImplementsEdgeKind = "GRPC_IMPLEMENTS"

// grpcHandlesEdgeKind is emitted from a client call site → GrpcMethod
// (client invokes this RPC).
const grpcHandlesEdgeKind = "GRPC_HANDLES"

// grpcSynthesisSupportsLanguage reports whether applyGRPCEdges can emit
// synthetics for the given language.
func grpcSynthesisSupportsLanguage(lang string) bool {
	switch lang {
	case "java", "kotlin", "go", "python", "javascript", "typescript":
		return true
	default:
		return false
	}
}

// applyGRPCEdges runs as an append-only pass after the HTTP synthesis pass.
// It emits GrpcService + GrpcMethod entities and GRPC_IMPLEMENTS /
// GRPC_HANDLES edges. It never modifies or removes existing entities or edges.
func applyGRPCEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	path := args.Path
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	if !grpcSynthesisSupportsLanguage(lang) {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(content)

	// Dedup-by-ID sets so we never emit duplicate entities or edges.
	seenEntity := map[string]bool{}
	seenEdge := map[string]bool{}

	emitService := func(serviceName, framework string, props map[string]string) {
		id := grpcServiceKind + ":" + serviceName
		if seenEntity[id] {
			return
		}
		seenEntity[id] = true
		merged := map[string]string{
			"framework":    framework,
			"pattern_type": "grpc_synthesis",
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		entities = append(entities, types.EntityRecord{
			Name:               serviceName,
			Kind:               grpcServiceKind,
			SourceFile:         path,
			Language:           lang,
			Properties:         merged,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	// grpcMethodID returns the canonical cross-repo ID for a gRPC method.
	// Shape: `grpc:ServiceName/MethodName` — identical on client and server
	// so the import-channel linker can join them without additional code.
	grpcMethodID := func(serviceName, methodName string) string {
		return fmt.Sprintf("grpc:%s/%s", serviceName, methodName)
	}

	emitMethod := func(serviceName, methodName, framework string, props map[string]string) {
		methodID := grpcMethodID(serviceName, methodName)
		entityKey := grpcMethodKind + ":" + methodID
		if seenEntity[entityKey] {
			return
		}
		seenEntity[entityKey] = true
		merged := map[string]string{
			"service":      serviceName,
			"method":       methodName,
			"framework":    framework,
			"pattern_type": "grpc_synthesis",
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		entities = append(entities, types.EntityRecord{
			Name:               methodID,
			Kind:               grpcMethodKind,
			SourceFile:         "",
			Language:           lang,
			Properties:         merged,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	emitImplementsEdge := func(handlerQualified, serviceName, methodName, streaming, framework string) {
		methodID := grpcMethodID(serviceName, methodName)
		key := grpcImplementsEdgeKind + "|" + handlerQualified + "|" + methodID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: fmt.Sprintf("SCOPE.Operation:%s", handlerQualified),
			ToID:   fmt.Sprintf("%s:%s", grpcMethodKind, methodID),
			Kind:   grpcImplementsEdgeKind,
			Properties: map[string]string{
				"framework":    framework,
				"pattern_type": "grpc_synthesis",
				"streaming":    streaming,
			},
		})
	}

	emitHandlesEdge := func(callerQualified, callerKind, serviceName, methodName, streaming, framework string) {
		methodID := grpcMethodID(serviceName, methodName)
		key := grpcHandlesEdgeKind + "|" + callerQualified + "|" + methodID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		fromID := fmt.Sprintf("%s:%s", callerKind, callerQualified)
		relationships = append(relationships, types.RelationshipRecord{
			FromID: fromID,
			ToID:   fmt.Sprintf("%s:%s", grpcMethodKind, methodID),
			Kind:   grpcHandlesEdgeKind,
			Properties: map[string]string{
				"framework":    framework,
				"pattern_type": "grpc_synthesis",
				"streaming":    streaming,
			},
		})
	}

	switch lang {
	case "java", "kotlin":
		synthesizeJavaGRPC(src, path, emitService, emitMethod, emitImplementsEdge, emitHandlesEdge)
	case "go":
		synthesizeGoGRPC(src, path, emitService, emitMethod, emitImplementsEdge, emitHandlesEdge)
	case "python":
		synthesizePythonGRPC(src, path, emitService, emitMethod, emitImplementsEdge, emitHandlesEdge)
	case "javascript", "typescript":
		synthesizeNodeGRPC(src, path, emitService, emitMethod, emitImplementsEdge, emitHandlesEdge)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// ---------------------------------------------------------------------------
// Java / Kotlin
// ---------------------------------------------------------------------------
//
// Server patterns:
//   1. @GrpcService (Quarkus Mutiny gRPC): class annotated with @GrpcService
//      extends generated ServiceGrpc.ServiceImplBase (or mutiny variant).
//   2. Direct extends ServiceGrpc.ServiceImplBase (standard protoc-java).
//
// Client patterns:
//   1. @GrpcClient("svc") injection (Quarkus): field annotated with
//      @GrpcClient("name") of type ServiceGrpc.ServiceBlockingStub / etc.
//   2. ManagedChannelBuilder.forAddress(...).build() → newBlockingStub(ch).
//   3. Call sites: `stub.methodName(req)` where stub is a known variable.

// javaGrpcServiceAnnotationRe matches @GrpcService (Quarkus) on a class.
var javaGrpcServiceAnnotationRe = regexp.MustCompile(
	`@(?:io\.quarkus\.grpc\.)?GrpcService\b`)

// javaGrpcImplBaseRe matches `extends SomethingGrpc.SomethingImplBase` (proto-java).
// Group 1: service name (the "Something" part).
var javaGrpcImplBaseRe = regexp.MustCompile(
	`extends\s+(\w+)Grpc\.(?:\w+ImplBase|(?:\w+Grpc\.)?\w+ImplBase)\b`)

// javaGrpcImplBaseSimpleRe is a fallback for `extends ServiceGrpc.ServiceImplBase`.
// Group 1: service name.
var javaGrpcImplBaseSimpleRe = regexp.MustCompile(
	`extends\s+(\w+)Grpc\.\w+`)

// javaGrpcClientAnnotationRe captures @GrpcClient("svc") + field type.
// Group 1: service name (annotation value). Group 2: stub type (may be dotted
// like GreeterGrpc.GreeterBlockingStub). Group 3: field name.
var javaGrpcClientAnnotationRe = regexp.MustCompile(
	`@(?:io\.quarkus\.grpc\.)?GrpcClient\s*\(\s*"([^"\n\r]+)"\s*\)\s*` +
		`(?:\n\s*)?` + // optional newline between annotation and field declaration
		`([\w.]+)\s+(\w+)\s*;`)

// javaManagedChannelRe detects stub construction via ManagedChannelBuilder.
// Pattern: SomeGrpc.newBlockingStub(channel) → captures stub type + variable.
// Group 1: service name. Group 2: stub variable name.
var javaManagedChannelRe = regexp.MustCompile(
	`(\w+)Grpc\.new\w+Stub\s*\([^)]*\)\s*(?:;|=)?\s*` +
		`|(\w+)Grpc\.new\w+Stub\s*\([^)]+\)`)

// javaStubNewRe is a simpler form: `GreeterGrpc.newBlockingStub(ch)`.
// Group 1: service name. Group 2: var name (from assignment).
var javaStubAssignRe = regexp.MustCompile(
	`(\w+)\s*=\s*(\w+)Grpc\.new\w+Stub\s*\(`)

// javaGrpcCallSiteRe matches `stub.methodName(req)` call sites.
// Group 1: stub variable name. Group 2: method name.
var javaGrpcCallSiteRe = regexp.MustCompile(
	`\b(\w+)\s*\.\s*([a-z]\w*)\s*\(`)

// javaGrpcOverrideRe matches `@Override public ... methodName(StreamObserver<Req> req, ...)`.
// Group 1: method name.
var javaGrpcOverrideRe = regexp.MustCompile(
	`@Override\s+(?:public\s+)?(?:void|[\w<>]+)\s+(\w+)\s*\(`)

// javaStreamObserverParam detects bidi/client-streaming by presence of two
// StreamObserver params or request StreamObserver.
var javaStreamObserverBidiRe = regexp.MustCompile(
	`StreamObserver\s*<[^>]+>\s*\w+\s*,\s*StreamObserver\s*<[^>]+>`)

func synthesizeJavaGRPC(
	src, path string,
	emitService func(name, framework string, props map[string]string),
	emitMethod func(serviceName, methodName, framework string, props map[string]string),
	emitImplementsEdge func(handler, service, method, streaming, framework string),
	emitHandlesEdge func(caller, callerKind, service, method, streaming, framework string),
) {
	// Fast pre-filter.
	if !strings.Contains(src, "Grpc") && !strings.Contains(src, "grpc") &&
		!strings.Contains(src, "GrpcService") && !strings.Contains(src, "GrpcClient") {
		return
	}

	// ---- Reflection detection (beyond-minimum) ----
	hasReflection := strings.Contains(src, "reflection.Register") ||
		strings.Contains(src, "ServerReflection")

	// ---- Gateway detection (beyond-minimum) ----
	hasGateway := strings.Contains(src, "GatewayServer") ||
		strings.Contains(src, "grpc-gateway")

	// ---- Determine class name ----
	className := ""
	if m := javaClassDeclRe.FindStringSubmatch(src); len(m) >= 2 {
		className = m[1]
	}

	// ---- Server side: @GrpcService annotation + class name ----
	isServer := javaGrpcServiceAnnotationRe.MatchString(src) ||
		javaGrpcImplBaseSimpleRe.MatchString(src)

	if isServer && className != "" {
		// Extract service name from extends clause.
		serviceName := className
		if m := javaGrpcImplBaseSimpleRe.FindStringSubmatch(src); len(m) >= 2 {
			serviceName = m[1]
		}
		props := map[string]string{}
		if hasReflection {
			props["reflection"] = "true"
		}
		if hasGateway {
			props["has_gateway"] = "true"
		}
		emitService(serviceName, "grpc_java_server", props)

		// Scan for @Override methods — each is a handler.
		for _, m := range javaGrpcOverrideRe.FindAllStringSubmatch(src, -1) {
			if len(m) < 2 {
				continue
			}
			methodName := m[1]
			// Determine streaming variant.
			streaming := "unary"
			// Find the method body window (~500 chars after the match).
			idx := javaGrpcOverrideRe.FindStringIndex(src)
			if idx != nil {
				windowEnd := idx[1] + 600
				if windowEnd > len(src) {
					windowEnd = len(src)
				}
				window := src[idx[1]:windowEnd]
				if javaStreamObserverBidiRe.MatchString(window) {
					streaming = "bidi_streaming"
				} else if strings.Contains(window, "stream.Send") || strings.Contains(window, "responseObserver.onNext") {
					streaming = "server_streaming"
				}
			}
			emitMethod(serviceName, methodName, "grpc_java_server", map[string]string{
				"streaming": streaming,
			})
			handlerQualified := className + "." + methodName
			emitImplementsEdge(handlerQualified, serviceName, methodName, streaming, "grpc_java_server")
		}
	}

	// ---- Client side: @GrpcClient injection ----
	// stubVar → serviceName registry for call-site attribution.
	stubRegistry := map[string]string{} // field name → service name

	for _, m := range javaGrpcClientAnnotationRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 4 {
			continue
		}
		svcAnnotation := m[1] // e.g. "greeter"
		fieldName := m[3]
		// Derive canonical service name from stub type if annotation is short.
		// Prefer service name from stub type (e.g. GreeterGrpc.GreeterBlockingStub → Greeter)
		// since it gives the canonical protobuf service name rather than the runtime alias.
		serviceName := grpcServiceNameFromAnnotation(svcAnnotation)
		stubType := m[2]
		if stubType != "" {
			// Handle dotted types: GreeterGrpc.GreeterBlockingStub → last part before Grpc
			// or from the class qualifier: GreeterGrpc → Greeter.
			if sn := grpcServiceNameFromStubType(stubType); sn != "" {
				serviceName = sn
			}
		}
		stubRegistry[fieldName] = serviceName
		emitService(serviceName, "grpc_java_client", map[string]string{"role": "client"})
	}

	// ManagedChannelBuilder-based stub construction.
	for _, m := range javaStubAssignRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 3 {
			continue
		}
		varName := m[1]
		serviceName := m[2]
		stubRegistry[varName] = serviceName
		emitService(serviceName, "grpc_java_client", map[string]string{"role": "client"})
	}

	if len(stubRegistry) == 0 {
		return
	}

	// Scan call sites.
	enclosingClass := className
	for _, m := range javaGrpcCallSiteRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		stubVar := src[m[2]:m[3]]
		methodName := src[m[4]:m[5]]
		serviceName, ok := stubRegistry[stubVar]
		if !ok {
			continue
		}
		// Skip common non-RPC method names.
		if isJavaGrpcNonCallMethod(methodName) {
			continue
		}
		emitMethod(serviceName, methodName, "grpc_java_client", map[string]string{"streaming": "unary"})
		callerName := enclosingClass
		if callerName == "" {
			callerName = "unknown"
		}
		emitHandlesEdge(callerName, "Service", serviceName, methodName, "unary", "grpc_java_client")
	}
}

// grpcServiceNameFromAnnotation converts a lowercase annotation value like
// "greeter" to a title-cased "Greeter" service name.
func grpcServiceNameFromAnnotation(s string) string {
	if s == "" {
		return "Unknown"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// grpcServiceNameFromStubType extracts the service name from a stub type.
// Examples:
//   - "GreeterGrpc.GreeterBlockingStub" → "Greeter"
//   - "GreeterGrpc" → "Greeter"
//   - "io.demo.GreeterGrpc" → "Greeter"
func grpcServiceNameFromStubType(t string) string {
	// For dotted types, check the qualifier part (before the first dot).
	parts := strings.Split(t, ".")
	for _, part := range parts {
		if idx := strings.Index(part, "Grpc"); idx > 0 {
			return part[:idx]
		}
	}
	return ""
}

// isJavaGrpcNonCallMethod returns true for common method names that are NOT
// gRPC RPC calls (e.g. lifecycle methods on the stub itself).
func isJavaGrpcNonCallMethod(name string) bool {
	switch name {
	case "build", "forAddress", "forTarget", "usePlaintext", "useTransportSecurity",
		"newBlockingStub", "newFutureStub", "newStub", "newMutinyStub",
		"intercept", "withInterceptors", "withDeadline", "withDeadlineAfter",
		"withCallCredentials", "shutdown", "awaitTermination", "isShutdown",
		"isTerminated", "shutdownNow", "getChannel", "getCallOptions":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Go
// ---------------------------------------------------------------------------
//
// Server patterns:
//   pb.RegisterServiceServer(grpcServer, &ServiceImpl{})
//   → GrpcService: ServiceImpl
//   → subsequent method declarations on ServiceImpl with (req *Req, stream *Service_*Server) or (ctx, *Req) → GrpcMethod
//
// Client patterns:
//   client := pb.NewServiceClient(conn)
//   client.GetUser(ctx, &pb.GetUserRequest{}) → GRPC_HANDLES

// goRegisterServerRe captures `pb.RegisterXxxServer(srv, &Impl{})`.
// Group 1: service name. Group 2: implementation type (without &).
var goRegisterServerRe = regexp.MustCompile(
	`\w+\.Register(\w+)Server\s*\([^,]+,\s*&?(\w+)\s*[\{,\)]`)

// goNewClientRe captures `varName := pb.NewXxxClient(conn)`.
// Group 1: variable name. Group 2: service name.
var goNewClientRe = regexp.MustCompile(
	`(\w+)\s*:?=\s*\w+\.New(\w+)Client\s*\(`)

// goClientCallRe captures `stub.Method(ctx, req)`.
// Group 1: stub variable. Group 2: method name.
var goClientCallRe = regexp.MustCompile(
	`\b(\w+)\s*\.\s*([A-Z]\w*)\s*\(`)

// goFuncReceiverRe captures Go method declarations on a specific receiver.
// Group 1: receiver type (without pointer). Group 2: method name.
var goFuncReceiverRe = regexp.MustCompile(
	`(?m)^func\s+\(\s*\w+\s+\*?(\w+)\s*\)\s+(\w+)\s*\(`)

// goStreamingRe detects streaming: look for stream.Send / stream.Recv in
// the function body.
var goStreamSendRe = regexp.MustCompile(`\bstream\.Send\b`)
var goStreamRecvRe = regexp.MustCompile(`\bstream\.Recv\b`)

// goGatewayRe detects grpc-gateway registration.
var goGatewayRe = regexp.MustCompile(
	`gateway\.Register\w+Handler|RegisterGateway\w+`)

// goReflectionRe detects gRPC server reflection.
var goReflectionRe = regexp.MustCompile(
	`reflection\.Register\s*\(|grpc_reflection\.Register`)

func synthesizeGoGRPC(
	src, path string,
	emitService func(name, framework string, props map[string]string),
	emitMethod func(serviceName, methodName, framework string, props map[string]string),
	emitImplementsEdge func(handler, service, method, streaming, framework string),
	emitHandlesEdge func(caller, callerKind, service, method, streaming, framework string),
) {
	if !strings.Contains(src, "grpc") && !strings.Contains(src, "Grpc") &&
		!strings.Contains(src, "pb.Register") && !strings.Contains(src, "pb.New") {
		return
	}

	hasGateway := goGatewayRe.MatchString(src)
	hasReflection := goReflectionRe.MatchString(src)

	// gRPC-Go interceptor auth (#4041). When a server constructed in this file
	// wires an auth-enforcing interceptor, the services it registers — and
	// their handler methods declared in this same file — inherit auth_required.
	// Same-file, signal-based; see grpc_go_auth.go for the resolution + limits.
	auth := resolveGoGRPCInterceptorAuth(src)

	// ---- Server side ----
	// implType → serviceName registry.
	serverRegistry := map[string]string{} // impl type → service name

	for _, m := range goRegisterServerRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 3 {
			continue
		}
		serviceName := m[1]
		implType := m[2]
		serverRegistry[implType] = serviceName
		props := map[string]string{}
		if hasGateway {
			props["has_gateway"] = "true"
		}
		if hasReflection {
			props["reflection"] = "true"
		}
		if auth.enforced {
			for k, v := range grpcGoAuthProps(auth.symbol) {
				props[k] = v
			}
		}
		emitService(serviceName, "grpc_go_server", props)
	}

	// For each registered implementation type, find its methods.
	for implType, serviceName := range serverRegistry {
		for _, m := range goFuncReceiverRe.FindAllStringSubmatchIndex(src, -1) {
			if len(m) < 6 {
				continue
			}
			receiverType := src[m[2]:m[3]]
			if receiverType != implType {
				continue
			}
			methodName := src[m[4]:m[5]]
			// Skip internal/boilerplate methods.
			if isGoGrpcBoilerplateMethod(methodName) {
				continue
			}
			// Determine streaming by scanning the function body (~600 chars).
			streaming := "unary"
			windowEnd := m[1] + 600
			if windowEnd > len(src) {
				windowEnd = len(src)
			}
			body := src[m[1]:windowEnd]
			hasSend := goStreamSendRe.MatchString(body)
			hasRecv := goStreamRecvRe.MatchString(body)
			if hasSend && hasRecv {
				streaming = "bidi_streaming"
			} else if hasSend {
				streaming = "server_streaming"
			} else if hasRecv {
				streaming = "client_streaming"
			}
			methodProps := map[string]string{
				"streaming": streaming,
			}
			if auth.enforced {
				for k, v := range grpcGoAuthProps(auth.symbol) {
					methodProps[k] = v
				}
			}
			emitMethod(serviceName, methodName, "grpc_go_server", methodProps)
			handlerQualified := implType + "." + methodName
			emitImplementsEdge(handlerQualified, serviceName, methodName, streaming, "grpc_go_server")
		}
	}

	// ---- Client side ----
	stubRegistry := map[string]string{} // var name → service name

	for _, m := range goNewClientRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 3 {
			continue
		}
		varName := m[1]
		serviceName := m[2]
		stubRegistry[varName] = serviceName
		emitService(serviceName, "grpc_go_client", map[string]string{"role": "client"})
	}

	if len(stubRegistry) == 0 {
		return
	}

	for _, m := range goClientCallRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		stubVar := src[m[2]:m[3]]
		methodName := src[m[4]:m[5]]
		serviceName, ok := stubRegistry[stubVar]
		if !ok {
			continue
		}
		if isGoGrpcBoilerplateMethod(methodName) {
			continue
		}
		emitMethod(serviceName, methodName, "grpc_go_client", map[string]string{"streaming": "unary"})
		// Enclosing function.
		caller := findEnclosingGoFuncName(src, m[0])
		emitHandlesEdge(caller, "Function", serviceName, methodName, "unary", "grpc_go_client")
	}
}

// isGoGrpcBoilerplateMethod returns true for Go gRPC stub boilerplate.
func isGoGrpcBoilerplateMethod(name string) bool {
	switch name {
	case "Dial", "DialContext", "NewServer", "Serve", "Stop", "GracefulStop",
		"NewConn", "Close", "GetState", "WaitForStateChange", "Connect",
		"ResetConnectBackoff", "GetServiceInfo", "RegisterService",
		"String", "Error", "ProtoMessage", "Reset", "Marshal", "Unmarshal",
		"Size", "MarshalTo", "ProtoSize":
		return true
	}
	return false
}

// findEnclosingGoFuncName searches backward from `offset` to find the nearest
// `func (...) FuncName(` declaration. Falls back to "package".
func findEnclosingGoFuncName(src string, offset int) string {
	start := offset - 4000
	if start < 0 {
		start = 0
	}
	window := src[start:offset]
	matches := goFunctionRe.FindAllStringSubmatch(window, -1)
	if len(matches) == 0 {
		return "package"
	}
	last := matches[len(matches)-1]
	name := last[2]
	if last[1] != "" {
		name = last[1] + "." + name
	}
	return name
}

// ---------------------------------------------------------------------------
// Python
// ---------------------------------------------------------------------------
//
// Server: `pb2_grpc.add_ServiceServicer_to_server(ServiceImpl(), server)`
//         → GrpcService: ServiceImpl
//         Methods: class methods of ServiceImpl that match gRPC pattern.
//
// Client: `stub = pb2_grpc.ServiceStub(channel)`
//         → stub.Method(req) → GRPC_HANDLES

// pyAddServicerRe captures `pb2_grpc.add_XServicer_to_server(impl, server)`.
// Group 1: service name. Group 2: implementation (first arg, may be `Impl()` or just `Impl`).
var pyAddServicerRe = regexp.MustCompile(
	`\w+_pb2_grpc\.add_(\w+)Servicer_to_server\s*\(\s*(\w+)\s*[(),]`)

// pyStubRe captures the module-qualified stub construction form:
//
//	stub = inventory_pb2_grpc.InventoryServiceStub(channel)
//
// Group 1: variable name. Group 2: service name (prefix before "Stub").
var pyStubRe = regexp.MustCompile(
	`(\w+)\s*=\s*\w+_pb2_grpc\.(\w+)Stub\s*\(`)

// pyStubDirectRe captures the directly-imported stub form (cross-file import):
//
//	from inventory_pb2_grpc import InventoryServiceStub
//	stub = InventoryServiceStub(channel)
//
// Group 1: variable name. Group 2: service name (prefix before "Stub").
// Only fires when the file also contains a _pb2_grpc import so we don't
// false-positive on unrelated "SomethingStub" class names.
var pyStubDirectRe = regexp.MustCompile(
	`(\w+)\s*=\s*(\w+)Stub\s*\(`)

// pyMethodCallRe captures `stub.method(req)` call sites.
// Group 1: stub variable. Group 2: method name.
var pyMethodCallRe = regexp.MustCompile(
	`\b(\w+)\s*\.\s*([a-zA-Z]\w*)\s*\(`)

// pyClassDefRe captures `class Foo(Bar):`.
// Group 1: class name. Group 2: parent class.
var pyClassDefRe = regexp.MustCompile(
	`(?m)^class\s+(\w+)\s*\(([^)]*)\)\s*:`)

// pyDefMethodRe captures `def method(self, ...)` in a class body.
// Group 1: method name.
var pyDefMethodRe = regexp.MustCompile(
	`(?m)^\s{4,}def\s+(\w+)\s*\(self`)

// pyServicerBaseRe checks that a base class name contains "Servicer".
var pyServicerBaseRe = regexp.MustCompile(`(\w+)Servicer`)

func synthesizePythonGRPC(
	src, path string,
	emitService func(name, framework string, props map[string]string),
	emitMethod func(serviceName, methodName, framework string, props map[string]string),
	emitImplementsEdge func(handler, service, method, streaming, framework string),
	emitHandlesEdge func(caller, callerKind, service, method, streaming, framework string),
) {
	if !strings.Contains(src, "pb2_grpc") && !strings.Contains(src, "grpc") {
		return
	}

	// ---- Server side ----
	// implClass → serviceName registry.
	serverRegistry := map[string]string{} // impl class → service name

	for _, m := range pyAddServicerRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 3 {
			continue
		}
		serviceName := m[1]
		implClass := strings.TrimSuffix(m[2], "()")
		serverRegistry[implClass] = serviceName
		emitService(serviceName, "grpc_python_server", nil)
	}

	// Also detect classes that extend *Servicer base classes.
	for _, m := range pyClassDefRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 3 {
			continue
		}
		className := m[1]
		bases := m[2]
		if sm := pyServicerBaseRe.FindStringSubmatch(bases); len(sm) >= 2 {
			serviceName := sm[1]
			if _, already := serverRegistry[className]; !already {
				serverRegistry[className] = serviceName
				emitService(serviceName, "grpc_python_server", nil)
			}
		}
	}

	// Extract methods from registered service implementations.
	for implClass, serviceName := range serverRegistry {
		// Find the class block and its methods.
		classPattern := regexp.MustCompile(`(?m)^class\s+` + regexp.QuoteMeta(implClass) + `\s*[\(:]`)
		idx := classPattern.FindStringIndex(src)
		if idx == nil {
			continue
		}
		// Scan the class body (up to 3000 bytes).
		bodyEnd := idx[1] + 3000
		if bodyEnd > len(src) {
			bodyEnd = len(src)
		}
		body := src[idx[1]:bodyEnd]
		for _, mm := range pyDefMethodRe.FindAllStringSubmatch(body, -1) {
			if len(mm) < 2 {
				continue
			}
			methodName := mm[1]
			if methodName == "__init__" || methodName == "__str__" {
				continue
			}
			emitMethod(serviceName, methodName, "grpc_python_server", map[string]string{"streaming": "unary"})
			handlerQualified := implClass + "." + methodName
			emitImplementsEdge(handlerQualified, serviceName, methodName, "unary", "grpc_python_server")
		}
	}

	// ---- Client side ----
	stubRegistry := map[string]string{} // var name → service name

	// Module-qualified form: stub = inventory_pb2_grpc.InventoryServiceStub(ch)
	for _, m := range pyStubRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 3 {
			continue
		}
		varName := m[1]
		serviceName := m[2]
		stubRegistry[varName] = serviceName
		emitService(serviceName, "grpc_python_client", map[string]string{"role": "client"})
	}

	// Directly-imported form (cross-file): stub = InventoryServiceStub(ch)
	// Only applies when the file has a _pb2_grpc import (avoids false positives).
	if strings.Contains(src, "_pb2_grpc") {
		for _, m := range pyStubDirectRe.FindAllStringSubmatch(src, -1) {
			if len(m) < 3 {
				continue
			}
			varName := m[1]
			serviceName := m[2]
			// Skip if already registered by the module-qualified form.
			if _, exists := stubRegistry[varName]; exists {
				continue
			}
			// Skip names that don't look like gRPC service stubs (must end
			// in "Stub" — already guaranteed by the regex — and the prefix
			// must be non-empty and not a local variable name in lower_snake).
			// Convention: protoc-generated stub classes are PascalCase.
			if serviceName == "" || (serviceName[0] >= 'a' && serviceName[0] <= 'z') {
				continue
			}
			stubRegistry[varName] = serviceName
			emitService(serviceName, "grpc_python_client", map[string]string{"role": "client"})
		}
	}

	if len(stubRegistry) == 0 {
		return
	}

	for _, m := range pyMethodCallRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		stubVar := src[m[2]:m[3]]
		methodName := src[m[4]:m[5]]
		serviceName, ok := stubRegistry[stubVar]
		if !ok {
			continue
		}
		if isPythonGrpcNonCallMethod(methodName) {
			continue
		}
		emitMethod(serviceName, methodName, "grpc_python_client", map[string]string{"streaming": "unary"})
		caller := findEnclosingPyName(src, m[0])
		emitHandlesEdge(caller, "Function", serviceName, methodName, "unary", "grpc_python_client")
	}
}

// isPythonGrpcNonCallMethod returns true for stub lifecycle methods.
func isPythonGrpcNonCallMethod(name string) bool {
	switch name {
	case "close", "add_insecure_port", "add_secure_port", "start", "stop",
		"wait_for_termination", "add_generic_rpc_handlers", "add_registered_method_handlers",
		"channel_ready_future", "__init__", "__del__", "__enter__", "__exit__":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Node / TypeScript
// ---------------------------------------------------------------------------
//
// Server: `server.addService(proto.Package.Service.service, impl)`
//         or `server.bindAsync(addr, creds, cb)`
//         Methods: keys of the implementation object.
//
// Client: `const client = new proto.Package.Service(addr, creds)`
//         or `const client = new grpcObj.Service(addr, creds)`
//         Call sites: `client.method(req, cb)`.

// nodeAddServiceRe captures `server.addService(serviceDefinition, impl)`.
// Group 1: service name (last identifier in the definition path).
// Group 2: impl variable or `{` (start of inline object).
var nodeAddServiceRe = regexp.MustCompile(
	`\.addService\s*\(\s*(?:[\w.]+\.)?(\w+)\.service\s*,\s*(\w+|\{)`)

// nodeGrpcClientCtorRe captures `new proto.Service(addr, creds)` or
// `new grpcPkg.Service(addr, creds)`.
// Group 1: variable name. Group 2: service name.
var nodeGrpcClientCtorRe = regexp.MustCompile(
	`(?:const|let|var)\s+(\w+)\s*=\s*new\s+(?:[\w.]+\.)?(\w+)\s*\(`)

// nodeGrpcFactoryClientRe captures the modern TS-first factory-function stub
// forms used by nice-grpc and Connect (connectrpc), which do NOT use `new`:
//
//	const client = createClient(GreeterDefinition, channel);          // nice-grpc
//	const client = createPromiseClient(ElizaService, transport);      // Connect
//	const client = createCallbackClient(ElizaService, transport);     // Connect
//
// Group 1: variable name. Group 2: descriptor identifier (the service
// definition/descriptor passed as the first argument, e.g. GreeterDefinition,
// ElizaService). The canonical protobuf service name is derived from this
// descriptor by stripping a trailing "Definition" or "Service" suffix.
var nodeGrpcFactoryClientRe = regexp.MustCompile(
	`(?:const|let|var)\s+(\w+)\s*=\s*` +
		`(?:await\s+)?` +
		`(?:create(?:Promise|Callback)?Client)\s*\(\s*` +
		`(?:[\w.]+\.)?(\w+)\s*,`)

// grpcServiceNameFromNodeDescriptor derives the canonical protobuf service
// name from a nice-grpc / Connect descriptor identifier. The protoc-gen-* TS
// generators name these `<Service>Definition` (nice-grpc / ts-proto) or
// `<Service>Service` (Connect / protobuf-es). The bare service name (already
// without a suffix) is returned unchanged so it still matches a server emitting
// `grpc:<Service>/<Method>`.
//
//	"GreeterDefinition" → "Greeter"   (nice-grpc / ts-proto)
//	"ElizaService"      → "Eliza"     (Connect / protobuf-es)
//	"Greeter"           → "Greeter"   (already canonical)
func grpcServiceNameFromNodeDescriptor(descriptor string) string {
	if descriptor == "" {
		return ""
	}
	for _, suffix := range []string{"Definition", "Service"} {
		if strings.HasSuffix(descriptor, suffix) && len(descriptor) > len(suffix) {
			return strings.TrimSuffix(descriptor, suffix)
		}
	}
	return descriptor
}

// nodeGrpcCallRe captures `client.method(req, cb)`.
// Group 1: client variable. Group 2: method name.
var nodeGrpcCallRe = regexp.MustCompile(
	`\b(\w+)\s*\.\s*([a-z]\w*)\s*\(`)

// nodeImplObjectRe captures key: function or key: async function entries in
// a literal object — these are the RPC method handlers.
// Group 1: method name.
var nodeImplMethodRe = regexp.MustCompile(
	`\b(\w+)\s*:\s*(?:async\s+)?function\s*\(|(\w+)\s*:\s*\(`)

// nodeGrpcHasMarkerRe checks for any grpc-shaped token. Includes the modern
// TS-first client factories (nice-grpc, Connect/connectrpc) whose import paths
// (e.g. "@connectrpc/connect") do not contain the substring "grpc".
var nodeGrpcHasMarkerRe = regexp.MustCompile(
	`grpc|@grpc/grpc-js|proto\.|createPromiseClient|createCallbackClient|@connectrpc/|connectrpc`)

func synthesizeNodeGRPC(
	src, path string,
	emitService func(name, framework string, props map[string]string),
	emitMethod func(serviceName, methodName, framework string, props map[string]string),
	emitImplementsEdge func(handler, service, method, streaming, framework string),
	emitHandlesEdge func(caller, callerKind, service, method, streaming, framework string),
) {
	if !nodeGrpcHasMarkerRe.MatchString(src) {
		return
	}

	// ---- Server side ----
	for _, m := range nodeAddServiceRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		serviceName := src[m[2]:m[3]]
		implToken := src[m[4]:m[5]]
		emitService(serviceName, "grpc_node_server", nil)

		// Attempt to extract methods from the implementation object.
		// If implToken is `{`, scan the inline object body for method names.
		// Otherwise (it's a variable name), we can't resolve its definition here.
		if implToken == "{" {
			windowEnd := m[1] + 1500
			if windowEnd > len(src) {
				windowEnd = len(src)
			}
			body := src[m[1]:windowEnd]
			for _, mm := range nodeImplMethodRe.FindAllStringSubmatch(body, -1) {
				methodName := mm[1]
				if methodName == "" {
					methodName = mm[2]
				}
				if methodName == "" || isNodeGrpcBoilerplate(methodName) {
					continue
				}
				emitMethod(serviceName, methodName, "grpc_node_server", map[string]string{"streaming": "unary"})
				emitImplementsEdge(serviceName+"."+methodName, serviceName, methodName, "unary", "grpc_node_server")
			}
		}
	}

	// ---- Client side ----
	stubRegistry := map[string]string{} // var name → service name

	for _, m := range nodeGrpcClientCtorRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 3 {
			continue
		}
		varName := m[1]
		serviceName := m[2]
		// Filter out non-gRPC constructors by requiring the context contains
		// grpc, Grpc, or proto hint within a few hundred bytes.
		if !strings.Contains(src, "grpc") && !strings.Contains(src, "proto") {
			continue
		}
		stubRegistry[varName] = serviceName
		emitService(serviceName, "grpc_node_client", map[string]string{"role": "client"})
	}

	// Modern TS-first factory-function stubs (nice-grpc, Connect). These derive
	// the canonical service name from the descriptor argument so the emitted
	// `grpc:<Service>/<Method>` id matches a server side exactly.
	for _, m := range nodeGrpcFactoryClientRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 3 {
			continue
		}
		varName := m[1]
		serviceName := grpcServiceNameFromNodeDescriptor(m[2])
		if serviceName == "" {
			continue
		}
		stubRegistry[varName] = serviceName
		emitService(serviceName, "grpc_node_client", map[string]string{"role": "client"})
	}

	if len(stubRegistry) == 0 {
		return
	}

	for _, m := range nodeGrpcCallRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		stubVar := src[m[2]:m[3]]
		methodName := src[m[4]:m[5]]
		serviceName, ok := stubRegistry[stubVar]
		if !ok {
			continue
		}
		if isNodeGrpcBoilerplate(methodName) {
			continue
		}
		emitMethod(serviceName, methodName, "grpc_node_client", map[string]string{"streaming": "unary"})
		caller := findEnclosingNodeName(src, m[0])
		emitHandlesEdge(caller, "Function", serviceName, methodName, "unary", "grpc_node_client")
	}
}

// isNodeGrpcBoilerplate returns true for non-RPC Node gRPC method names.
func isNodeGrpcBoilerplate(name string) bool {
	switch name {
	case "bind", "bindAsync", "addService", "start", "tryShutdown", "forceShutdown",
		"addProtoService", "close", "getChannel", "waitForReady",
		"makeUnaryRequest", "makeClientStreamRequest", "makeServerStreamRequest",
		"makeBidiStreamRequest", "makeGenericClientStream",
		"toString", "valueOf", "hasOwnProperty", "isPrototypeOf",
		"then", "catch", "finally", "resolve", "reject":
		return true
	}
	return false
}
