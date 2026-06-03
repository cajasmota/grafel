package engine

import (
	"testing"
)

// TestTRPCAuth_MiddlewareAndProtectedProcedure proves #4041: a tRPC procedure
// built from an auth-enforcing middleware / protectedProcedure builder is
// stamped auth_required=true + auth_method=trpc_middleware, an inline auth
// `.use(...)` is detected per-procedure, and public / non-auth-middleware
// procedures are left unstamped (honest "public").
func TestTRPCAuth_MiddlewareAndProtectedProcedure(t *testing.T) {
	src := readBackendFixture(t, "trpc_auth.ts")
	res := runDetectWS(t, "typescript", "server/router.ts", src)

	// --- POSITIVE: protectedProcedure (t.procedure.use(isAuthed), isAuthed
	// throws TRPCError UNAUTHORIZED on !ctx.user) → auth_required on getUser.
	getUser := trpcDefByPath(res.Entities, "getUser")
	if getUser == nil {
		t.Fatalf("expected tRPC endpoint for getUser; entities=%d", len(res.Entities))
	}
	if getUser.Properties["auth_required"] != "true" {
		t.Errorf("getUser auth_required=%q want true (built from protectedProcedure→isAuthed)", getUser.Properties["auth_required"])
	}
	if getUser.Properties["auth_method"] != "trpc_middleware" {
		t.Errorf("getUser auth_method=%q want trpc_middleware", getUser.Properties["auth_method"])
	}
	if getUser.Properties["auth_confidence"] != "high" {
		t.Errorf("getUser auth_confidence=%q want high (builder resolved in-file)", getUser.Properties["auth_confidence"])
	}
	// MCP signal-1 key must be present so archigraph_auth_coverage credits it.
	if getUser.Properties["auth_middleware"] == "" {
		t.Errorf("getUser auth_middleware property must be set for MCP signal-1, got empty")
	}
	if getUser.Properties["auth_policy"] == "" {
		t.Errorf("getUser auth_policy (dashboard source chain) must be set")
	}

	// --- POSITIVE: inline `.use(({ctx,next})=>{ if(!ctx.session) throw FORBIDDEN })`
	// on the procedure itself → auth on deleteAll without a named builder.
	deleteAll := trpcDefByPath(res.Entities, "deleteAll")
	if deleteAll == nil {
		t.Fatal("expected tRPC endpoint for deleteAll")
	}
	if deleteAll.Properties["auth_required"] != "true" {
		t.Errorf("deleteAll auth_required=%q want true (inline auth .use)", deleteAll.Properties["auth_required"])
	}
	if deleteAll.Properties["auth_method"] != "trpc_middleware" {
		t.Errorf("deleteAll auth_method=%q want trpc_middleware", deleteAll.Properties["auth_method"])
	}

	// --- NEGATIVE: publicProcedure → no auth.
	listUsers := trpcDefByPath(res.Entities, "listUsers")
	if listUsers == nil {
		t.Fatal("expected tRPC endpoint for listUsers")
	}
	if listUsers.Properties["auth_required"] == "true" {
		t.Errorf("listUsers must NOT be auth_required (publicProcedure), got auth_required=true")
	}
	if listUsers.Properties["auth_method"] == "trpc_middleware" {
		t.Errorf("listUsers must NOT carry trpc_middleware auth_method")
	}
	if listUsers.Properties["auth_middleware"] != "" {
		t.Errorf("listUsers must NOT carry auth_middleware, got %q", listUsers.Properties["auth_middleware"])
	}

	// --- NEGATIVE: built from a non-auth (logging) middleware → no auth.
	ping := trpcDefByPath(res.Entities, "ping")
	if ping == nil {
		t.Fatal("expected tRPC endpoint for ping")
	}
	if ping.Properties["auth_required"] == "true" {
		t.Errorf("ping must NOT be auth_required (built from logging middleware), got true")
	}
}

// TestTRPCAuth_ImportedProtectedProcedure proves the cross-file case: an
// imported protectedProcedure (definition not in this file) is credited at
// MEDIUM confidence (name-based binding), while the imported publicProcedure
// is left public.
func TestTRPCAuth_ImportedProtectedProcedure(t *testing.T) {
	src := readBackendFixture(t, "trpc_auth_imported.ts")
	res := runDetectWS(t, "typescript", "server/userRouter.ts", src)

	me := trpcDefByPath(res.Entities, "me")
	if me == nil {
		t.Fatalf("expected tRPC endpoint for me; entities=%d", len(res.Entities))
	}
	if me.Properties["auth_required"] != "true" {
		t.Errorf("me auth_required=%q want true (imported protectedProcedure)", me.Properties["auth_required"])
	}
	if me.Properties["auth_method"] != "trpc_middleware" {
		t.Errorf("me auth_method=%q want trpc_middleware", me.Properties["auth_method"])
	}
	if me.Properties["auth_confidence"] != "medium" {
		t.Errorf("me auth_confidence=%q want medium (cross-file name binding)", me.Properties["auth_confidence"])
	}

	signup := trpcDefByPath(res.Entities, "signup")
	if signup == nil {
		t.Fatal("expected tRPC endpoint for signup")
	}
	if signup.Properties["auth_required"] == "true" {
		t.Errorf("signup must NOT be auth_required (imported publicProcedure)")
	}
}
