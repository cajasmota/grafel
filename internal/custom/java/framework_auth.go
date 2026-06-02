package java

// framework_auth.go — endpoint-level auth-policy stamping for the JVM web
// frameworks that trail Spring on the auth_coverage capability cell
// (#3862, epic #3854): Javalin, Vert.x, Akka-HTTP, Apache Struts, and
// Netflix DGS.
//
// Spring (and JAX-RS, via internal/engine/java_annotation_routes.go +
// java_auth_policy.go) already stamp a FLAT auth contract directly on each
// synthetic http_endpoint:
//
//	auth_required    "true" | "false"          (string)
//	auth_method      "annotation" | "middleware" | "directive" | ...
//	auth_confidence  "high" | "medium" | "low"
//	auth_guard       framework-specific guard name (read by archigraph_auth_coverage)
//	auth_roles       comma-joined role names      (sorted, deterministic)
//	auth_scopes      comma-joined OAuth2 scopes    (sorted)
//	auth_permissions comma-joined fine-grained permissions (sorted)
//
// This file mirrors that EXACT contract so the trailing frameworks' route /
// endpoint entities carry the same props. Because these extractors emit
// SecondaryEntity.Properties as map[string]any, the helpers below write the
// same string values Spring writes (so a downstream consumer sees an identical
// shape regardless of language).
//
// HONEST-PARTIAL policy: a flat auth posture is stamped ONLY when a concrete,
// statically-visible auth signal applies to the route (an inline role guard, an
// auth directive/handler wrapping the route, a method-level JSR-250 annotation).
// Dynamic / unclear protection (a hand-rolled header check, a named guard whose
// roles we cannot read) is left UNSTAMPED — auth_required is simply absent and
// the endpoint stays "unknown", exactly as Spring leaves an unannotated handler.

import (
	"sort"
	"strings"
)

// authStamp is the resolved, framework-agnostic auth posture for one route.
// It is the trailing-framework analogue of engine.AuthPolicy, reduced to the
// flat fields the dashboard and archigraph_auth_coverage actually read.
type authStamp struct {
	required    bool
	method      string // "annotation" | "middleware" | "directive" | "config"
	confidence  string // "high" | "medium" | "low"
	guard       string // framework guard name (e.g. "JWTAuthHandler", "roles(...)")
	mechanism   string // "jwt" | "basic" | "oauth2" | "" (auth scheme, when known)
	roles       []string
	scopes      []string
	permissions []string
}

// stamp writes the flat auth contract onto a route/endpoint entity's property
// map, mirroring the Spring / JAX-RS flat field set exactly. It is a no-op when
// the posture carries no signal (method == ""), leaving the endpoint "unknown".
//
// required=false (an explicit public marker, e.g. @PermitAll) is still stamped
// so the dashboard can render "[Public]" rather than "[Auth: unknown]".
func (a authStamp) stamp(props map[string]any) {
	if props == nil || a.method == "" {
		return
	}
	if a.required {
		props["auth_required"] = "true"
	} else {
		props["auth_required"] = "false"
	}
	props["auth_method"] = a.method
	if a.confidence != "" {
		props["auth_confidence"] = a.confidence
	}
	// auth_guard is the key archigraph_auth_coverage reads to count an endpoint
	// as covered (see internal/mcp/auth_coverage.go authPropertyKeys). Only set
	// it for protected endpoints — a public endpoint is NOT covered.
	if a.required && a.guard != "" {
		props["auth_guard"] = a.guard
	}
	if a.mechanism != "" {
		props["auth_mechanism"] = a.mechanism
	}
	if len(a.roles) > 0 {
		r := dedupSorted(a.roles)
		props["auth_roles"] = strings.Join(r, ",")
	}
	if len(a.scopes) > 0 {
		s := dedupSorted(a.scopes)
		props["auth_scopes"] = strings.Join(s, ",")
	}
	if len(a.permissions) > 0 {
		p := dedupSorted(a.permissions)
		props["auth_permissions"] = strings.Join(p, ",")
	}
}

// dedupSorted returns a sorted, de-duplicated copy of in (drops empties) so the
// stamped comma-joined fields are deterministic across runs — matching the
// sort.Strings the Spring/JAX-RS stamper applies to roles/scopes/permissions.
func dedupSorted(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// ── Shared role/scope/authority classification (Spring-compatible) ──────────

// authQuotedTokenRE pulls quoted (single- or double-quoted) tokens out of an
// annotation argument list or DSL call. Reused by the per-framework parsers.
//
// (defined as a method-local helper to avoid a package-level regex name clash;
// see the per-framework regexes for the call sites.)

// classifyAuthority splits a Spring-style granted-authority token into the
// roles/scopes/permissions buckets, applying the same prefix conventions the
// Java auth-policy resolver uses (ROLE_ → role, SCOPE_/scope: → scope, else
// permission). Used by frameworks layered on Spring Security (Struts+Spring,
// DGS+Spring Security).
func classifyAuthority(tok string, roles, scopes, perms *[]string) {
	tok = strings.TrimSpace(strings.Trim(tok, `"'`))
	if tok == "" {
		return
	}
	switch {
	case strings.HasPrefix(tok, "ROLE_"):
		*roles = append(*roles, strings.TrimPrefix(tok, "ROLE_"))
	case strings.HasPrefix(tok, "SCOPE_"):
		*scopes = append(*scopes, strings.TrimPrefix(tok, "SCOPE_"))
	case strings.HasPrefix(tok, "scope:"):
		*scopes = append(*scopes, strings.TrimPrefix(tok, "scope:"))
	default:
		*perms = append(*perms, tok)
	}
}
