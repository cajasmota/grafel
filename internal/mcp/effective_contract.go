package mcp

// effective_contract.go — per-verb EFFECTIVE CONTRACT projection (epic #3829,
// ticket #3835 — T5).
//
// The DRF expansion pass (internal/engine/django_drf_actions.go) STAMPS the
// per-verb effective contract onto every router-expanded http_endpoint entity:
// it merges the route provenance (#3831), the ViewSet posture (#3864), and the
// baseknowledge pack's per-verb defaults (#3832) into a set of `effective_*`
// (plus serializer_class) properties. This file is the read-path PROJECTION
// that lifts those flat string properties back into a structured per-verb
// record — the single artifact that prevents the #278 defect class:
//
//	{verb, kind: explicit|inherited|action, source_class, default_status,
//	 error_statuses, serializer, pagination, permissions, behaviour}
//
// so an INHERITED `create` route surfaces {kind:inherited,
// source_class:CreateModelMixin, default_status:201, error_statuses:[400]}
// even though the ViewSet body is empty.
//
// T5 owns the COMPUTATION + STAMPING (engine) and this projection helper. T6
// (#3836, archigraph_effective_contract) owns the user-facing MCP tool that
// groups these per-class and returns them; it consumes projectEffectiveContract
// per backing route entity. Keeping the projection here lets T6 be a thin
// grouping/serving layer over a tested computation.
//
// HONEST-PARTIAL: fields the stamp omitted (unknown base, no curated status)
// are simply absent from the projection — never fabricated.

import (
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// effectiveContract is the structured per-verb contract projected from a single
// router-expanded http_endpoint entity's stamped `effective_*` properties.
//
// It is the in-process shape T6 (#3836) serialises. Zero-value optional fields
// (DefaultStatus == 0, empty slices/strings) mean "not resolvable / not
// curated" — the honest-partial signal — NOT a real value.
type effectiveContract struct {
	// Verb is the HTTP method this route handles ("POST", "PATCH", ...).
	Verb string `json:"verb"`
	// Path is the canonical route path ("/api/v1/roles/{pk}").
	Path string `json:"path,omitempty"`
	// Handler is the qualified ViewSet method backing the route
	// ("RoleViewSet.create"), when known.
	Handler string `json:"handler,omitempty"`
	// Kind is the verb taxonomy: "explicit" (overridden in the ViewSet body),
	// "inherited" (from a mixin), or "action" (@action custom route). Empty when
	// the route carries no per-verb contract (the ANY catch-all).
	Kind string `json:"kind,omitempty"`
	// SourceClass is the class that defines the verb's body — the ViewSet for
	// explicit/action verbs, the mixin for inherited verbs.
	SourceClass string `json:"source_class,omitempty"`
	// DefaultStatus is the verb's default success HTTP status (0 = no curated
	// default; honest-partial).
	DefaultStatus int `json:"default_status,omitempty"`
	// ErrorStatuses are the documented non-success statuses the verb can
	// produce (the #278 fact: [400] for create/update). Empty when none curated.
	ErrorStatuses []int `json:"error_statuses,omitempty"`
	// Serializer is the ViewSet's static serializer_class leaf, when declared.
	Serializer string `json:"serializer,omitempty"`
	// Pagination is true when the verb is paginated by the route's effective
	// pagination posture (DRF list with a configured paginator).
	Pagination bool `json:"pagination,omitempty"`
	// Permissions are the resolved permission / auth / throttle class leaves in
	// effect on the route (from the view-scope middleware chain). Empty when the
	// ViewSet declares no posture.
	Permissions []string `json:"permissions,omitempty"`
	// Behaviour is the pack's short behavioural note for the verb (e.g. the
	// is_valid→400 fact). Empty when none curated.
	Behaviour string `json:"behaviour,omitempty"`
	// AuthRequired is true when a non-AllowAny permission or any authentication
	// class is in effect on the route.
	AuthRequired bool `json:"auth_required,omitempty"`
}

// isRouterExpandedRoute reports whether e is a DRF router-expanded route entity
// (the entities stampDRFEffectiveContract writes onto). T6 filters a ViewSet's
// backing routes through this.
func isRouterExpandedRoute(e *graph.Entity) bool {
	return e != nil && e.Properties["pattern_type"] == "drf_router_expanded"
}

// projectEffectiveContract lifts the flat `effective_*` (and posture)
// properties a router-expanded route carries into a structured effectiveContract.
// It is the inverse of engine.stampDRFEffectiveContract: it reads ONLY what the
// stamp wrote, so the projection and the stamp cannot drift on which fields are
// present. Returns (contract, true) for a router-expanded route, (zero, false)
// otherwise.
//
// HONEST-PARTIAL: a property the stamp omitted (unknown base, no curated status)
// is left zero/empty here — never fabricated.
func projectEffectiveContract(e *graph.Entity) (effectiveContract, bool) {
	if !isRouterExpandedRoute(e) {
		return effectiveContract{}, false
	}
	p := e.Properties
	c := effectiveContract{
		Verb:        p["verb"],
		Path:        p["path"],
		Handler:     p["drf_view_method"],
		Kind:        p["effective_kind"],
		SourceClass: p["effective_source_class"],
		Serializer:  p["serializer_class"],
		Behaviour:   p["effective_behaviour"],
	}
	if s := p["effective_status"]; s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			c.DefaultStatus = n
		}
	}
	c.ErrorStatuses = parseIntCSV(p["effective_error_statuses"])
	c.Pagination = p["effective_pagination"] == "true"
	c.AuthRequired = p["auth_required"] == "true"
	c.Permissions = splitNonEmptyCSV(p["middleware_names"])
	return c, true
}

// parseIntCSV parses a comma-separated list of integers ("400,409") into an int
// slice, skipping non-numeric tokens. Returns nil for an empty input.
func parseIntCSV(s string) []int {
	if s == "" {
		return nil
	}
	var out []int
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if n, err := strconv.Atoi(tok); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// splitNonEmptyCSV splits a comma-separated list into a trimmed, non-empty
// slice. Returns nil for an empty input.
func splitNonEmptyCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if tok != "" {
			out = append(out, tok)
		}
	}
	return out
}
