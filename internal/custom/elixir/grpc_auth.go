package elixir

// grpc_auth.go — gRPC-Elixir (grpc-elixir) interceptor auth detection.
//
// (#4041, epic #3872). The route-keyed Elixir auth sniffers (phoenix.go,
// plug.go, ueberauth.go) are HTTP-endpoint/pipeline-keyed: they emit 0 auth
// signal on a grpc-elixir service, where authentication lives in a TRANSPORT
// SERVER INTERCEPTOR — a module implementing the `GRPC.Server.Interceptor`
// behaviour — not on any Phoenix route or Plug pipeline. The canonical idiom:
//
//	# (a) an interceptor module that rejects an unauthenticated call
//	defmodule MyApp.AuthInterceptor do
//	  use GRPC.Server.Interceptor          # or: @behaviour GRPC.Server.Interceptor
//
//	  def call(req, stream, next, _opts) do
//	    case token(stream) do
//	      nil -> raise GRPC.RPCError, status: :unauthenticated
//	      _   -> next.(req, stream)
//	    end
//	  end
//	end
//
//	# (b) wired into the gRPC endpoint / supervisor interceptor list
//	defmodule MyApp.Endpoint do
//	  use GRPC.Endpoint
//	  intercept MyApp.AuthInterceptor        # GRPC.Endpoint form
//	  run Helloworld.Greeter.Server
//	end
//	# or, in run/2 / GRPC.Server.Supervisor:
//	#   run MyApp.Server, interceptors: [MyApp.AuthInterceptor]
//	#   {GRPC.Server.Supervisor, ..., interceptors: [MyApp.AuthInterceptor]}
//
// resolveGRPCElixirAuth re-scans the SAME file and, when an auth-enforcing
// interceptor module is both PRESENT (its body rejects with a gRPC
// :unauthenticated / :permission_denied status) and WIRED (named in an
// `intercept`/`interceptors: [...]` registration), returns enforced=true plus
// the interceptor module symbol. The Extract loop then stamps
// auth_required/auth_method/auth_middleware/auth_confidence on each gRPC method
// (SCOPE.GrpcMethod) and the server (SCOPE.GrpcService) in the file. Append-
// property-only — it never adds or removes entities.
//
// HONEST LIMITS:
//   - Same-file boundary. The interceptor module definition AND its
//     intercept/interceptors wiring must live in this file. In a real app the
//     `GRPC.Endpoint`/supervisor wiring frequently lives in a different module
//     than the `.pb.ex` service definition; that cross-file binding is not
//     chased here (the same same-file boundary the gRPC-C++/Go slices live
//     within, and the rest of grpc.go's synthesis). Hence the elixir.grpc
//     auth_coverage cell is honest-partial, not full.
//   - A logging/tracing interceptor (no :unauthenticated / :permission_denied
//     reject) is NOT auth-enforcing; a file with no `intercept`/`interceptors:`
//     wiring leaves the methods UNSTAMPED.

import (
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// grpcElixirAuthMethod is the auth_method value stamped on a grpc-elixir method
// guarded by an auth interceptor. Distinct from the HTTP auth methods so the
// dashboard can tell gRPC-interceptor auth apart, and aligned with the gRPC-Go
// (#4064) / gRPC-C++ (#4068) slices.
const grpcElixirAuthMethod = "grpc_interceptor"

var (
	// A module declaring the GRPC.Server.Interceptor behaviour, via either
	// `use GRPC.Server.Interceptor` or `@behaviour GRPC.Server.Interceptor`.
	reGRPCInterceptorUse = regexp.MustCompile(
		`(?m)^\s*(?:use|@behaviour)\s+GRPC\.Server\.Interceptor\b`)

	// The decisive reject: a gRPC auth/authorization status. An interceptor that
	// fails with :unauthenticated / :permission_denied gates access; one that
	// does not is observational (logging/tracing), not auth. Matches both the
	// `GRPC.RPCError ... status: :unauthenticated` form and the
	// `GRPC.Status.unauthenticated()` helper.
	reGRPCAuthReject = regexp.MustCompile(
		`status:\s*:(?:unauthenticated|permission_denied)\b` +
			`|\bGRPC\.Status\.(?:unauthenticated|permission_denied)\b`)

	// Interceptor wiring (GRPC.Endpoint form): `intercept MyApp.AuthInterceptor`.
	// Group 1 = the wired interceptor module.
	reGRPCIntercept = regexp.MustCompile(
		`(?m)^\s*intercept\s+([A-Z][\w.]*)`)

	// Interceptor wiring (run/2 + GRPC.Server.Supervisor form):
	// `interceptors: [MyApp.AuthInterceptor, GRPC.Logger.Server]`. Group 1 = the
	// full bracket contents, scanned for module names.
	reGRPCInterceptorsList = regexp.MustCompile(
		`interceptors:\s*\[([^\]]*)\]`)

	// A module name token inside an interceptors: [...] list.
	reGRPCModuleToken = regexp.MustCompile(`[A-Z][\w.]*`)
)

// grpcElixirAuthResult carries the per-file grpc-elixir auth verdict.
type grpcElixirAuthResult struct {
	// enforced is true when an auth-enforcing interceptor module is both PRESENT
	// in the file and WIRED into an intercept/interceptors registration.
	enforced bool
	// symbol is the interceptor module name credited as the auth enforcer (the
	// auth_middleware MCP archigraph_auth_coverage signal-1 value).
	symbol string
}

// grpcElixirStampAuth stamps the auth_required / auth_method / auth_confidence /
// auth_middleware (the MCP archigraph_auth_coverage signal-1 key) /
// auth_enforcer_kind properties on a gRPC entity when the file's auth verdict is
// enforced. No-op otherwise — append-property-only, never clobbers.
func grpcElixirStampAuth(e *types.EntityRecord, auth grpcElixirAuthResult) {
	if !auth.enforced {
		return
	}
	setProps(e,
		"auth_required", "true",
		"auth_method", grpcElixirAuthMethod,
		"auth_confidence", "high",
		"auth_middleware", auth.symbol,
		"auth_enforcer_kind", "interceptor",
	)
}

// resolveGRPCElixirAuth inspects a grpc-elixir source file and reports whether
// the services in it are guarded by an auth-enforcing interceptor module, plus
// the interceptor module symbol. Same-file, signal-based, append-property-only.
func resolveGRPCElixirAuth(src string) grpcElixirAuthResult {
	// Collect the set of wired interceptor module names (intercept + interceptors:).
	wired := grpcElixirWiredInterceptors(src)
	if len(wired) == 0 {
		return grpcElixirAuthResult{}
	}
	// For each wired module that is also an auth-enforcing interceptor module
	// DEFINED in this file, credit it. Prefer the first such module in source
	// order for a stable symbol.
	for _, mod := range grpcElixirAuthEnforcingModules(src) {
		if wired[mod] {
			return grpcElixirAuthResult{enforced: true, symbol: mod}
		}
	}
	return grpcElixirAuthResult{}
}

// grpcElixirWiredInterceptors returns the set of interceptor module names wired
// via `intercept X` (GRPC.Endpoint) or `interceptors: [X, ...]` (run/2 /
// GRPC.Server.Supervisor) in the file.
func grpcElixirWiredInterceptors(src string) map[string]bool {
	wired := make(map[string]bool)
	for _, m := range reGRPCIntercept.FindAllStringSubmatch(src, -1) {
		wired[m[1]] = true
	}
	for _, m := range reGRPCInterceptorsList.FindAllStringSubmatch(src, -1) {
		for _, tok := range reGRPCModuleToken.FindAllString(m[1], -1) {
			wired[tok] = true
		}
	}
	return wired
}

// grpcElixirAuthEnforcingModules returns, in source order, the names of modules
// in the file that declare the GRPC.Server.Interceptor behaviour AND whose body
// rejects with a gRPC :unauthenticated / :permission_denied status (i.e. are
// auth-enforcing, not merely observational). A logging interceptor — declaring
// the behaviour but never rejecting with an auth status — is excluded.
func grpcElixirAuthEnforcingModules(src string) []string {
	var out []string
	for _, m := range rePhoenixModuleDecl.FindAllStringSubmatchIndex(src, -1) {
		module := src[m[2]:m[3]]
		// Body = from this module decl up to (but excluding) the next module
		// decl, so a sibling module's reject/behaviour is not attributed here.
		bodyStart := m[1]
		bodyEnd := len(src)
		if next := rePhoenixModuleDecl.FindStringIndex(src[m[1]:]); next != nil {
			bodyEnd = m[1] + next[0]
		}
		body := src[bodyStart:bodyEnd]
		if !reGRPCInterceptorUse.MatchString(body) {
			continue
		}
		if !reGRPCAuthReject.MatchString(body) {
			continue
		}
		out = append(out, module)
	}
	return out
}
