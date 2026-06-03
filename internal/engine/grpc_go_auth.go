// gRPC-Go server-interceptor auth detection (#4041, epic #3872).
//
// The Go auth sniffers (internal/custom/golang/route_auth.go,
// middleware_auth_extend.go) are HTTP-route/middleware-keyed: they recognise
// gin/echo/chi/fiber router groups guarded by an auth middleware attached to a
// URL route. gRPC-Go carries none of that — a gRPC server enforces auth in a
// transport-level INTERCEPTOR wired into the server at construction time, not
// on any HTTP route:
//
//	func authInterceptor(ctx context.Context, req interface{},
//	    info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
//	    md, ok := metadata.FromIncomingContext(ctx)
//	    if !ok || !validToken(md["authorization"]) {
//	        return nil, status.Error(codes.Unauthenticated, "missing token")
//	    }
//	    return handler(ctx, req)
//	}
//
//	srv := grpc.NewServer(grpc.UnaryInterceptor(authInterceptor))
//	pb.RegisterGreeterServer(srv, &greeterServer{}) // its methods are AUTHed
//
// Because applyGRPCEdges (grpc_edges.go, #725) already emits one
// SCOPE.GrpcService per registered impl and one SCOPE.GrpcMethod per handler
// method, this resolver re-walks the SAME file and stamps the auth contract on
// the service + the methods served by an auth-enforcing interceptor. Same-file,
// signal-based, append-property-only — it never adds or removes entities.
//
// Resolution (the interceptor-resolution approach):
//
//  1. Collect AUTH-ENFORCING interceptor function names. A func is an auth
//     interceptor iff its body BOTH inspects request metadata
//     (`metadata.FromIncomingContext(ctx)`) AND rejects on a failure with a
//     gRPC auth status code — `status.Error(codes.Unauthenticated, ...)` /
//     `codes.PermissionDenied` (or the `status.Errorf` variants). A logging or
//     timing interceptor — no metadata read, no Unauthenticated/PermissionDenied
//     return — is NOT auth-enforcing.
//  2. Recognise the go-grpc-middleware auth helper directly. A
//     `grpc_auth.UnaryServerInterceptor(<authFunc>)` /
//     `grpc_auth.StreamServerInterceptor(<authFunc>)` call IS, by the library's
//     contract, an authentication interceptor — its presence anywhere in a
//     server's option chain enforces auth regardless of the authFunc body
//     (which is frequently defined cross-file).
//  3. Determine whether `grpc.NewServer(...)` wires an auth interceptor. The
//     server options carry it via `grpc.UnaryInterceptor(X)` /
//     `grpc.StreamInterceptor(X)` / `grpc.ChainUnaryInterceptor(X, ...)` /
//     `grpc.ChainStreamInterceptor(...)`. The server enforces auth iff any
//     wired interceptor reference is an auth interceptor name (step 1) OR an
//     inline `grpc_auth.*ServerInterceptor(...)` call (step 2).
//  4. When the file has exactly one server construction that enforces auth, the
//     services registered in that file inherit auth_required, and so do the
//     handler methods detected on those services WITHIN THIS FILE.
//
// HONEST LIMITS (documented, not papered over):
//
//   - Cross-file methods. applyGRPCEdges scans receiver-method declarations in
//     the SAME file as the RegisterXxxServer call. Handler methods declared in
//     a separate file are not stamped — the interceptor→method binding cannot
//     be proven same-file. This is the same same-file boundary the rest of the
//     Go gRPC synthesis already lives within.
//   - Multiple servers. If a file constructs more than one gRPC server and they
//     differ in auth posture, this resolver does not attempt to disambiguate
//     which RegisterXxxServer targets which server; it conservatively only
//     stamps when every server construction in the file enforces auth, so a
//     mixed file leaves the records UNSTAMPED (honest: "not proven").
//
// Output (mirrors stampTRPCAuth so archigraph_auth_coverage signal-1 +
// the security dashboard light up):
//
//	auth_required   — "true"
//	auth_method     — "grpc_interceptor"
//	auth_confidence — "high"
//	auth_middleware — the interceptor symbol (MCP signal-1 key)
//	auth_policy     — JSON-encoded AuthPolicy (source chain for the dashboard)
package engine

import (
	"regexp"
	"strings"
)

// grpcGoAuthMethod is the auth_method value stamped on a gRPC-Go service/method
// served by an auth-enforcing interceptor. Distinct from the Express-family
// "middleware" and the tRPC "trpc_middleware" so the dashboard can tell
// gRPC-interceptor auth apart.
const grpcGoAuthMethod = "grpc_interceptor"

// grpcGoServerCtorRe matches a `grpc.NewServer(` construction. The opening
// paren sits at the match end so the option list can be captured with
// balanced-paren walking.
var grpcGoServerCtorRe = regexp.MustCompile(`\bgrpc\.NewServer\s*\(`)

// grpcGoInterceptorOptionRe finds each server-option call that wires a
// (chain of) interceptor(s): grpc.UnaryInterceptor / grpc.StreamInterceptor /
// grpc.ChainUnaryInterceptor / grpc.ChainStreamInterceptor. Group 1 is the
// option name; the argument list follows the matched `(`.
var grpcGoInterceptorOptionRe = regexp.MustCompile(
	`\bgrpc\.(UnaryInterceptor|StreamInterceptor|ChainUnaryInterceptor|ChainStreamInterceptor)\s*\(`)

// grpcGoAuthMiddlewareCallRe matches the go-grpc-middleware auth helper
// `grpc_auth.UnaryServerInterceptor(` / `grpc_auth.StreamServerInterceptor(`.
// The package alias is conventionally `grpc_auth` (the import path is
// .../go-grpc-middleware/auth); accept the common `auth.` alias too. Its
// presence IS an authentication interceptor by the library's contract.
var grpcGoAuthMiddlewareCallRe = regexp.MustCompile(
	`\b(?:grpc_auth|auth)\.(?:Unary|Stream)ServerInterceptor\s*\(`)

// grpcGoFuncDeclRe matches a top-level Go function declaration:
// `func <name>(`. Group 1 = the function name. Used to locate candidate
// interceptor functions whose body is then inspected.
var grpcGoFuncDeclRe = regexp.MustCompile(`(?m)^func\s+([A-Za-z_]\w*)\s*\(`)

// grpcGoMetadataReadRe detects an incoming-metadata read — the canonical way a
// gRPC interceptor obtains the request's credentials.
var grpcGoMetadataReadRe = regexp.MustCompile(
	`metadata\.FromIncomingContext\s*\(|metautils\.ExtractIncoming\s*\(|grpc_auth\.AuthFromMD\s*\(`)

// grpcGoAuthRejectRe detects a rejection with a gRPC authentication/authorization
// status code — the decisive evidence the interceptor gates access.
var grpcGoAuthRejectRe = regexp.MustCompile(
	`codes\.(Unauthenticated|PermissionDenied)\b`)

// grpcGoIdentRe extracts a leading Go identifier from an interceptor-option
// argument (`authInterceptor` from `authInterceptor` or
// `loggingInterceptor, authInterceptor`). Group 1 = the identifier.
var grpcGoIdentRe = regexp.MustCompile(`^\s*([A-Za-z_]\w*)`)

// grpcGoAuthResult carries the per-file gRPC-Go auth verdict.
type grpcGoAuthResult struct {
	// enforced is true when every gRPC server constructed in the file wires an
	// auth-enforcing interceptor (and at least one server is constructed).
	enforced bool
	// symbol is the interceptor symbol credited as the auth enforcer (for the
	// auth_middleware signal + the policy source chain).
	symbol string
}

// resolveGoGRPCInterceptorAuth inspects a Go source file and returns whether a
// gRPC server constructed in it enforces auth via an interceptor, plus the
// interceptor symbol that does so. Same-file, signal-based.
func resolveGoGRPCInterceptorAuth(src string) grpcGoAuthResult {
	// Cheap gate: no server construction → nothing to stamp.
	ctorIdx := grpcGoServerCtorRe.FindAllStringIndex(src, -1)
	if len(ctorIdx) == 0 {
		return grpcGoAuthResult{}
	}

	authNames := goAuthInterceptorNames(src)

	enforcedCount := 0
	var symbol string
	for _, ci := range ctorIdx {
		// Capture the balanced option list of this grpc.NewServer(...) call.
		opts, ok := balancedArgs(src, ci[1]-1)
		if !ok {
			continue
		}
		sym, ok := serverOptionsEnforceAuth(opts, authNames)
		if !ok {
			// A server with no auth interceptor → not all servers enforce auth.
			return grpcGoAuthResult{}
		}
		enforcedCount++
		if symbol == "" {
			symbol = sym
		}
	}
	if enforcedCount == 0 {
		return grpcGoAuthResult{}
	}
	return grpcGoAuthResult{enforced: true, symbol: symbol}
}

// goAuthInterceptorNames returns the set of function names in `src` that are
// auth-enforcing interceptors: their body BOTH reads incoming metadata AND
// rejects with a gRPC auth status code.
func goAuthInterceptorNames(src string) map[string]bool {
	names := map[string]bool{}
	decls := grpcGoFuncDeclRe.FindAllStringSubmatchIndex(src, -1)
	for i, d := range decls {
		name := src[d[2]:d[3]]
		// Body span: from this decl to the next top-level func (or EOF).
		end := len(src)
		if i+1 < len(decls) {
			end = decls[i+1][0]
		}
		body := src[d[0]:end]
		if grpcGoMetadataReadRe.MatchString(body) && grpcGoAuthRejectRe.MatchString(body) {
			names[name] = true
		}
	}
	return names
}

// serverOptionsEnforceAuth reports whether the captured grpc.NewServer option
// list wires an auth-enforcing interceptor, returning the crediting symbol.
//
//   - An inline grpc_auth.UnaryServerInterceptor(...) / StreamServerInterceptor(...)
//     call IS auth by the go-grpc-middleware contract.
//   - Otherwise, any interceptor reference passed to grpc.UnaryInterceptor /
//     StreamInterceptor / ChainUnaryInterceptor / ChainStreamInterceptor whose
//     leading identifier is a known auth-interceptor name (authNames) enforces auth.
func serverOptionsEnforceAuth(opts string, authNames map[string]bool) (string, bool) {
	// go-grpc-middleware auth helper — decisive on its own.
	if loc := grpcGoAuthMiddlewareCallRe.FindStringIndex(opts); loc != nil {
		// Credit the helper call as the symbol (trim the trailing `(`).
		return strings.TrimRight(strings.TrimSpace(opts[loc[0]:loc[1]]), "("), true
	}
	// Walk each interceptor-wiring option and inspect its argument identifiers.
	for _, oi := range grpcGoInterceptorOptionRe.FindAllStringIndex(opts, -1) {
		args, ok := balancedArgs(opts, oi[1]-1)
		if !ok {
			continue
		}
		for _, a := range splitTopLevelArgs(args) {
			m := grpcGoIdentRe.FindStringSubmatch(a)
			if m == nil {
				continue
			}
			if authNames[m[1]] {
				return m[1], true
			}
		}
	}
	return "", false
}

// balancedArgs returns the substring inside the parentheses whose opening `(`
// is at index openParen in s, walking balanced parens (ignoring those inside
// double-quoted strings, single-quoted runes and backtick raw strings). The
// returned string excludes the outer parens. ok is false if unbalanced.
func balancedArgs(s string, openParen int) (string, bool) {
	if openParen < 0 || openParen >= len(s) || s[openParen] != '(' {
		return "", false
	}
	depth := 0
	i := openParen
	for i < len(s) {
		c := s[i]
		switch c {
		case '"', '\'', '`':
			// Skip a quoted span.
			j := skipGoQuoted(s, i)
			i = j
			continue
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[openParen+1 : i], true
			}
		}
		i++
	}
	return "", false
}

// skipGoQuoted returns the index just past the quoted span that starts at i
// (s[i] is the opening quote). Handles backslash escapes for " and '; backtick
// raw strings have no escapes.
func skipGoQuoted(s string, i int) int {
	quote := s[i]
	i++
	for i < len(s) {
		c := s[i]
		if c == '\\' && quote != '`' {
			i += 2
			continue
		}
		if c == quote {
			return i + 1
		}
		i++
	}
	return len(s)
}

// grpcGoAuthProps returns the property map to merge onto a gRPC-Go service /
// method entity that an auth interceptor protects. Mirrors stampTRPCAuth's key
// set: the bare auth_middleware key credits the entity for archigraph_auth_coverage
// signal-1; auth_policy carries the dashboard source chain.
func grpcGoAuthProps(symbol string) map[string]string {
	props := map[string]string{
		"auth_required":   "true",
		"auth_method":     grpcGoAuthMethod,
		"auth_confidence": "high",
	}
	if symbol != "" {
		props["auth_middleware"] = symbol
	}
	policy := AuthPolicy{
		Required:   true,
		Method:     "middleware",
		Confidence: "high",
		SourceChain: []AuthSignal{{
			Kind: "middleware",
			Text: "grpc-interceptor: " + symbol,
		}},
	}
	if policyJSON := EncodeAuthPolicy(policy); policyJSON != "" {
		props["auth_policy"] = policyJSON
	}
	return props
}
