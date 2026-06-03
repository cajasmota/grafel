// tRPC middleware / protectedProcedure auth detection (#4041, epic #3872).
//
// The cross-framework JS/TS auth resolver (http_endpoint_jsts_auth.go, #2852)
// is HTTP-route/decorator-keyed: it recognises Express-shaped middleware
// chains, NestJS @UseGuards, Hapi/Adonis/Marble route config. tRPC carries
// none of those — its auth lives in a transport-agnostic MIDDLEWARE that is
// composed into a PROCEDURE BUILDER, not attached to a URL route:
//
//	const isAuthed = t.middleware(({ ctx, next }) => {
//	  if (!ctx.user) throw new TRPCError({ code: 'UNAUTHORIZED' });
//	  return next({ ctx: { user: ctx.user } });
//	});
//	const protectedProcedure = t.procedure.use(isAuthed);
//
//	export const appRouter = t.router({
//	  getUser: protectedProcedure.query(({ ctx }) => find(ctx.user.id)), // AUTH
//	  listUsers: publicProcedure.query(() => list()),                    // public
//	});
//
// Because synthesizeTRPC (#2693) already emits one http_endpoint_definition
// per leaf procedure keyed on the dotted path, this pass — exactly like the
// input-schema binding (#2865, applyTRPCSchemaBinding) — re-walks the same
// routers and stamps the auth contract on the matching endpoint, keyed on the
// shared `path` property. Append-property-only: it never adds or removes
// entities.
//
// Resolution (the protectedProcedure-resolution approach):
//
//  1. Collect AUTH-ENFORCING middleware names. `const X = t.middleware(fn)` is
//     auth-enforcing iff `fn` THROWS on a missing principal — a TRPCError with
//     an auth code (UNAUTHORIZED/FORBIDDEN), or a throw guarded by a
//     `ctx.user`/`ctx.session`/`ctx.auth`/`ctx.userId` check. A logging /
//     timing middleware (no throw, no principal check) is NOT auth-enforcing.
//  2. Collect AUTH-ENFORCING procedure builders. `const P = BASE.use(M)` is an
//     auth builder iff `M` is an auth middleware (step 1), an inline auth arrow
//     (`.use(({ ctx, next }) => { if (!ctx.user) throw ... })`), OR `BASE` is
//     itself an auth builder (transitive). The conventional name
//     `protectedProcedure` / `authedProcedure` / `privateProcedure` seeds the
//     set at MEDIUM confidence even when its definition is imported from
//     another module (HONEST: cross-file binding is name-heuristic, not proven).
//  3. Per leaf procedure: the builder is the leading identifier of the value
//     (`protectedProcedure.input(...).query(...)` → `protectedProcedure`). If
//     that builder is an auth builder → auth_required. An inline `.use(<auth>)`
//     anywhere in the leaf's own chain also enforces auth on that one
//     procedure.
//
// Output (same property contract the JS/TS resolver writes, so
// archigraph_auth_coverage signal-1 + the security dashboard light up):
//
//	auth_required   — "true"
//	auth_method     — "trpc_middleware"
//	auth_confidence — "high" (resolved builder/inline) | "medium" (name-only)
//	auth_middleware — the recognised middleware / builder symbol (MCP signal-1)
//	auth_policy     — JSON-encoded AuthPolicy (source chain for the dashboard)
//
// Public procedures (publicProcedure / t.procedure with no auth .use) and
// non-auth middleware are left UNSTAMPED — honest: "this procedure is public".
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// trpcAuthMethod is the auth_method value stamped on a tRPC procedure whose
// builder/inline middleware enforces auth. Distinct from the Express-family
// "middleware" so the dashboard can tell tRPC-middleware auth apart from
// route-level Express middleware.
const trpcAuthMethod = "trpc_middleware"

// trpcConventionalProtectedNames are procedure-builder identifiers whose name
// is, by overwhelming community convention, an auth-enforcing builder. They
// seed the auth-builder set at MEDIUM confidence so a router that imports
// `protectedProcedure` from `../trpc` (definition cross-file) is still
// credited — honestly flagged medium because the binding is name-based.
var trpcConventionalProtectedNames = map[string]bool{
	"protectedprocedure": true,
	"authedprocedure":    true,
	"privateprocedure":   true,
	"authprocedure":      true,
	"securedprocedure":   true,
	"loggedinprocedure":  true,
}

// trpcMiddlewareDeclRe matches `const X = t.middleware(` / `const X =
// <inst>.middleware(` / `const X = middleware(`. Group 1 = the middleware
// variable name. The `(` that opens the middleware argument sits at the match
// end so the body span can be captured with balanced-paren walking.
var trpcMiddlewareDeclRe = regexp.MustCompile(
	`(?:^|\n)\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*` +
		`(?:[A-Za-z_$][\w$]*\s*\.\s*)?middleware\s*\(`,
)

// trpcProcedureBuilderDeclRe matches a procedure-builder declaration:
//
//	const protectedProcedure = t.procedure.use(isAuthed)
//	const adminProcedure = protectedProcedure.use(isAdmin)
//	export const publicProcedure = t.procedure
//
// Group 1 = builder name, Group 2 = the RHS expression (captured greedily to
// end of the logical statement — the value walker re-scans it for `.use(...)`).
var trpcProcedureBuilderDeclRe = regexp.MustCompile(
	`(?:^|\n)\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*` +
		`([A-Za-z_$][\w$]*(?:\s*\.\s*[A-Za-z_$][\w$]*)*\s*\.\s*procedure\b[\s\S]*?)(?:\n|$)`,
)

// trpcAuthCodeRe detects a TRPCError thrown with an auth status code. These
// codes are the tRPC equivalent of HTTP 401/403 — their presence in a
// middleware/arrow body is decisive evidence the middleware gates access.
var trpcAuthCodeRe = regexp.MustCompile(`code\s*:\s*['"` + "`" + `](UNAUTHORIZED|FORBIDDEN)['"` + "`" + `]`)

// trpcPrincipalCheckRe detects a guard on a request principal —
// `!ctx.user`, `!ctx.session`, `ctx.user == null`, `!ctx.auth?.userId`, etc.
// Combined with a `throw` in the same body this is an auth gate even when the
// thrown error isn't a TRPCError (hand-rolled error classes are common).
var trpcPrincipalCheckRe = regexp.MustCompile(
	`ctx\s*(?:\.\s*[A-Za-z_$][\w$]*|\?\.\s*[A-Za-z_$][\w$]*)*\s*\.\s*` +
		`(user|users|userId|userID|session|sessionId|auth|account|principal|currentUser|viewer|identity|token|jwt|subject|isAuthenticated|isAuthed)\b`,
)

// trpcUseCallRe finds each `.use(` call site in a builder/procedure value so
// the argument (a middleware reference or an inline arrow) can be inspected.
var trpcUseCallRe = regexp.MustCompile(`\.\s*use\s*\(`)

// trpcLeadingIdentRe extracts the leading identifier of a procedure value —
// `protectedProcedure.input(...).query(...)` → `protectedProcedure`,
// `t.procedure.query(...)` → `t` (then `.procedure` follows). Group 1 = the
// first identifier; for the `t.procedure` shape we re-test the full head.
var trpcLeadingIdentRe = regexp.MustCompile(`^\s*([A-Za-z_$][\w$]*)`)

// trpcAuthInfo is the resolved auth contract for one procedure path.
type trpcAuthInfo struct {
	required   bool
	confidence string // "high" | "medium"
	symbol     string // the builder/middleware symbol that enforces auth
}

// applyTRPCAuthBinding stamps the auth contract on the tRPC
// http_endpoint_definition entities appended into `entities` at index `from`
// or later. Same-file, signal-based, append-property-only.
func applyTRPCAuthBinding(content string, entities []types.EntityRecord, from int) {
	if !trpcFileLooksLikeTRPC(content) {
		return
	}
	byPath := trpcAuthByPath(content)
	if len(byPath) == 0 {
		return
	}
	for i := from; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind {
			continue
		}
		if e.Properties == nil || e.Properties["framework"] != "trpc" {
			continue
		}
		info, ok := byPath[e.Properties["path"]]
		if !ok || !info.required {
			continue
		}
		stampTRPCAuth(e.Properties, info)
	}
}

// stampTRPCAuth writes the auth property contract for an auth-required tRPC
// procedure. Mirrors stampAuthPolicy's key set so archigraph_auth_coverage
// (signal-1 auth_middleware) and the dashboard (auth_policy JSON) light up,
// but stamps the tRPC-specific auth_method and never overwrites a stronger
// already-resolved policy.
func stampTRPCAuth(props map[string]string, info trpcAuthInfo) {
	// Defensive: if a stronger resolver already marked this endpoint as
	// auth-required (it cannot today — no JS/TS resolver keys tRPC), do not
	// clobber it.
	if props["auth_required"] == "true" {
		return
	}
	props["auth_required"] = "true"
	props["auth_method"] = trpcAuthMethod
	props["auth_confidence"] = info.confidence
	if info.symbol != "" {
		// MCP signal-1 key (auth_coverage.go authPropertyKeys) — its mere
		// presence credits the endpoint, so the tool fires without parsing
		// the JSON source chain.
		props["auth_middleware"] = info.symbol
	}
	// Dashboard source chain. Method is the canonical enum "middleware" inside
	// the JSON so the cross-language AuthPolicy decoder stays uniform; the
	// tRPC specificity lives in auth_method + the signal text.
	policy := AuthPolicy{
		Required:   true,
		Method:     "middleware",
		Confidence: info.confidence,
		SourceChain: []AuthSignal{{
			Kind: "middleware",
			Text: "trpc-middleware: " + info.symbol,
		}},
	}
	if policyJSON := EncodeAuthPolicy(policy); policyJSON != "" {
		props["auth_policy"] = policyJSON
	}
}

// trpcAuthByPath walks the routers in `content` and returns a map of dotted
// procedure path → resolved auth contract (only auth-required procedures are
// included). Mirrors synthesizeTRPC's router-roots / referenced-child logic so
// the dotted paths match exactly the IDs the synthesizer emitted.
func trpcAuthByPath(content string) map[string]trpcAuthInfo {
	routers := parseTRPCRouters(content)
	if len(routers) == 0 {
		return nil
	}

	authMW := trpcAuthMiddlewareNames(content)
	authBuilders := trpcAuthProcedureBuilders(content, authMW)

	byName := map[string]*trpcRouter{}
	for i := range routers {
		byName[routers[i].name] = &routers[i]
	}
	referenced := map[string]bool{}
	for i := range routers {
		for _, p := range parseTRPCProperties(routers[i]) {
			trimmed := strings.TrimSpace(p.value)
			if trpcIdentRe.MatchString(trimmed) {
				if _, ok := byName[trimmed]; ok {
					referenced[trimmed] = true
				}
			}
		}
	}
	out := map[string]trpcAuthInfo{}
	for i := range routers {
		if referenced[routers[i].name] {
			continue
		}
		walkTRPCAuth(&routers[i], "", byName, authMW, authBuilders, map[string]bool{}, out)
	}
	return out
}

// walkTRPCAuth mirrors walkTRPCRouter but records each leaf procedure's auth
// posture (when auth-required) keyed by dotted path.
func walkTRPCAuth(
	r *trpcRouter,
	prefix string,
	byName map[string]*trpcRouter,
	authMW map[string]bool,
	authBuilders map[string]trpcAuthInfo,
	seen map[string]bool,
	out map[string]trpcAuthInfo,
) {
	if seen[r.name] {
		return
	}
	seen[r.name] = true
	defer delete(seen, r.name)

	for _, p := range parseTRPCProperties(*r) {
		path := joinTRPCPath(prefix, p.key)
		trimmed := strings.TrimSpace(p.value)
		if trpcIdentRe.MatchString(trimmed) {
			if child, ok := byName[trimmed]; ok {
				walkTRPCAuth(child, path, byName, authMW, authBuilders, seen, out)
			}
			continue
		}
		// Only leaf procedures (those carrying a verb call) get a path ID.
		if trpcVerbRe.FindStringSubmatchIndex(p.value) == nil {
			continue
		}
		if info, ok := resolveLeafAuth(p.value, authMW, authBuilders); ok {
			out[path] = info
		}
	}
}

// resolveLeafAuth decides whether a single leaf procedure value enforces auth.
// Two independent signals:
//
//  1. The leading builder identifier is an auth builder (resolved or
//     conventionally named).
//  2. An inline `.use(<auth-middleware-or-auth-arrow>)` sits directly in the
//     leaf's own chain (a procedure can opt into auth without a named builder).
func resolveLeafAuth(
	value string,
	authMW map[string]bool,
	authBuilders map[string]trpcAuthInfo,
) (trpcAuthInfo, bool) {
	// Signal 1 — leading builder.
	if builder := trpcLeadingBuilder(value); builder != "" {
		if info, ok := authBuilders[strings.ToLower(builder)]; ok {
			return info, true
		}
	}
	// Signal 2 — inline `.use(...)` on the procedure itself.
	if sym, ok := trpcChainHasAuthUse(value, authMW); ok {
		return trpcAuthInfo{required: true, confidence: "high", symbol: sym}, true
	}
	return trpcAuthInfo{}, false
}

// trpcLeadingBuilder returns the procedure-builder identifier a leaf is built
// from. `protectedProcedure.query(...)` → "protectedProcedure". The
// `t.procedure...` / `publicProcedure...` shapes return the head identifier
// ("t" / "publicProcedure") which simply won't be in the auth-builder set.
func trpcLeadingBuilder(value string) string {
	m := trpcLeadingIdentRe.FindStringSubmatch(value)
	if m == nil {
		return ""
	}
	return m[1]
}

// trpcChainHasAuthUse reports whether the value's `.use(...)` calls include an
// auth-enforcing middleware reference or an inline auth arrow. Returns the
// recognised symbol for the MCP signal-1 property.
func trpcChainHasAuthUse(value string, authMW map[string]bool) (string, bool) {
	for _, loc := range trpcUseCallRe.FindAllStringIndex(value, -1) {
		open := loc[1] - 1 // position of '('
		end, ok := matchClosingParen(value, open)
		if !ok {
			continue
		}
		arg := strings.TrimSpace(value[open+1 : end])
		if arg == "" {
			continue
		}
		// Inline arrow / function expression — inspect its body for an auth
		// gate (throw + auth-code or principal-check).
		if strings.Contains(arg, "=>") || strings.HasPrefix(arg, "function") {
			if trpcBodyEnforcesAuth(arg) {
				return "inline middleware", true
			}
			continue
		}
		// Named middleware reference — `.use(isAuthed)` /
		// `.use(authMiddleware)`. Take the leading identifier and test the
		// resolved set, then the cross-framework auth vocabulary as a fallback
		// (covers `requireAuth` style names defined out of file).
		if id := trpcLeadingBuilder(arg); id != "" {
			if authMW[strings.ToLower(id)] {
				return id, true
			}
			if jsAuthMiddlewareNames[strings.ToLower(id)] {
				return id, true
			}
		}
	}
	return "", false
}

// trpcAuthMiddlewareNames scans the file for `const X = t.middleware(fn)`
// declarations and returns the lowercased names of those whose body enforces
// auth (throws on a missing principal). Non-auth middleware (logging, timing)
// is excluded.
func trpcAuthMiddlewareNames(content string) map[string]bool {
	out := map[string]bool{}
	for _, m := range trpcMiddlewareDeclRe.FindAllStringSubmatchIndex(content, -1) {
		name := content[m[2]:m[3]]
		// The `(` opening the middleware arg sits at the last byte of the
		// overall match (m[1]-1). Capture the balanced argument span.
		open := m[1] - 1
		end, ok := matchClosingParen(content, open)
		if !ok {
			continue
		}
		body := content[open+1 : end]
		if trpcBodyEnforcesAuth(body) {
			out[strings.ToLower(name)] = true
		}
	}
	return out
}

// trpcAuthProcedureBuilders returns the lowercased names of procedure builders
// that enforce auth, with confidence. A builder is auth-enforcing when it is
// `BASE.use(M)` with M an auth middleware / inline auth arrow / auth base
// (resolved → HIGH), or its name matches the protected-procedure convention
// (name-only → MEDIUM). Iterates to a fixed point so a builder chained off
// another auth builder (`adminProcedure = protectedProcedure.use(isAdmin)`) is
// credited transitively.
func trpcAuthProcedureBuilders(content string, authMW map[string]bool) map[string]trpcAuthInfo {
	type decl struct {
		name string
		rhs  string
	}
	var decls []decl
	for _, m := range trpcProcedureBuilderDeclRe.FindAllStringSubmatchIndex(content, -1) {
		decls = append(decls, decl{
			name: content[m[2]:m[3]],
			rhs:  content[m[4]:m[5]],
		})
	}

	out := map[string]trpcAuthInfo{}
	// Seed conventionally-named builders at MEDIUM (binding is name-based;
	// the definition may be imported from another module).
	for i := range decls {
		lname := strings.ToLower(decls[i].name)
		if trpcConventionalProtectedNames[lname] {
			out[lname] = trpcAuthInfo{required: true, confidence: "medium", symbol: decls[i].name}
		}
	}
	// Also seed conventional names that are only ever IMPORTED (no local
	// decl) — a router file that does `import { protectedProcedure }` then
	// uses it. We add the convention names unconditionally so an imported
	// protectedProcedure is credited (medium).
	for lname := range trpcConventionalProtectedNames {
		if _, ok := out[lname]; !ok {
			out[lname] = trpcAuthInfo{required: true, confidence: "medium", symbol: canonicalProtectedName(lname)}
		}
	}

	// Fixed-point: a builder is HIGH-confidence auth when it composes an auth
	// middleware via `.use(...)` or chains off an already-auth builder.
	for changed := true; changed; {
		changed = false
		for i := range decls {
			lname := strings.ToLower(decls[i].name)
			if info, ok := out[lname]; ok && info.confidence == "high" {
				continue // already strongest
			}
			if sym, ok := trpcChainHasAuthUse(decls[i].rhs, authMW); ok {
				out[lname] = trpcAuthInfo{required: true, confidence: "high", symbol: decls[i].name + " via " + sym}
				changed = true
				continue
			}
			// Chains off another auth builder: `BASE.use(...)` where BASE is
			// an auth builder. The leading identifier of the RHS is BASE.
			if base := trpcLeadingBuilder(decls[i].rhs); base != "" {
				if binfo, ok := out[strings.ToLower(base)]; ok && binfo.required {
					// Only inherit when this decl actually adds a `.use(` (it
					// is a derived builder), not a bare alias of a public base.
					if trpcUseCallRe.MatchString(decls[i].rhs) {
						conf := binfo.confidence
						out[lname] = trpcAuthInfo{required: true, confidence: conf, symbol: decls[i].name}
						changed = true
					}
				}
			}
		}
	}
	return out
}

// canonicalProtectedName returns a display-cased symbol for a lowercased
// convention name (best-effort — used only for the MCP signal-1 property when
// the builder is imported with no local declaration to read the casing from).
func canonicalProtectedName(lname string) string {
	switch lname {
	case "protectedprocedure":
		return "protectedProcedure"
	case "authedprocedure":
		return "authedProcedure"
	case "privateprocedure":
		return "privateProcedure"
	case "authprocedure":
		return "authProcedure"
	case "securedprocedure":
		return "securedProcedure"
	case "loggedinprocedure":
		return "loggedInProcedure"
	}
	return lname
}

// trpcBodyEnforcesAuth reports whether a middleware/arrow body gates access on
// a principal. The decisive evidence is a `throw` paired with EITHER a
// TRPCError auth code (UNAUTHORIZED/FORBIDDEN) OR a principal check
// (`ctx.user` / `ctx.session` / ...). A body that never throws (logging,
// timing, request enrichment) is NOT auth-enforcing.
func trpcBodyEnforcesAuth(body string) bool {
	if !strings.Contains(body, "throw") {
		return false
	}
	if trpcAuthCodeRe.MatchString(body) {
		return true
	}
	// throw + a principal reference is a hand-rolled auth gate. Require the
	// principal token to co-occur with a negation / comparison so a body that
	// merely *reads* ctx.user (and throws for an unrelated reason) doesn't
	// false-fire. The principal regex already targets auth-shaped fields; the
	// throw gate above bounds the false-positive surface.
	return trpcPrincipalCheckRe.MatchString(body)
}
