// resolvers.go — the PLUGGABLE auth-posture resolver registry (epic #4419
// all-framework mandate). Each Resolver maps ONE framework's native auth signal
// into the shared {Kind, Literal} vocabulary defined in authposture.go.
//
// A Resolver receives a Signal — the framework-neutral bundle of evidence the
// MCP tool harvested from a graph entity (its auth-posture properties plus, when
// available, the raw source body of the relevant method). The resolver inspects
// whichever fields its framework stamps and returns a Posture (or ok=false when
// the signal is not its framework).
//
// Sequencing per the epic: FLAGSHIP resolvers (Django DRF + NestJS) are
// implemented in full here; every other framework is registered as a STUB that
// reports ok=false so the registry shape is fixed and the follow-up tickets
// (Spring/Rails/FastAPI/Laravel/ASP.NET/Go/Phoenix — ref #4419) drop in without
// touching the diff core. Flagship-first sequencing is allowed; flagship-ONLY is
// not acceptance — hence the stubs are real registry members, not absent.
package authposture

import (
	"sort"
	"strings"
)

// Signal is the framework-neutral evidence bundle a Resolver inspects. It is
// assembled by the MCP tool from a graph entity. Not every field is populated
// for every entity — a resolver reads only the fields its framework stamps.
type Signal struct {
	// Framework, when non-empty, is the entity's declared framework hint
	// (e.g. "django", "nestjs"). Resolvers may use it to disambiguate, but
	// MUST NOT rely on it exclusively — most graphs do not stamp it, so
	// resolvers key off their characteristic property/source signatures.
	Framework string

	// Props is the entity's property map (auth_required, auth_guard,
	// permission_classes, has_get_permissions, get_permissions_classes, …).
	Props map[string]string

	// Source is the raw source body of the auth-bearing method/handler when the
	// MCP tool could resolve it (e.g. the Django get_permissions body). Empty
	// when unavailable — resolvers degrade to property-only decoding.
	Source string

	// Action, for ViewSet-style frameworks, is the DRF action name the posture
	// is being resolved FOR (e.g. "list", "create"). Empty for flat handlers.
	Action string
}

// prop returns a trimmed property value (empty when absent).
func (s Signal) prop(k string) string { return strings.TrimSpace(s.Props[k]) }

// hasProp reports whether a property key is present (even if empty) — mirrors
// the extractor's has_* marker convention.
func (s Signal) hasProp(k string) bool { _, ok := s.Props[k]; return ok }

// Resolver maps a framework's auth Signal to a Posture. Resolve returns
// ok=false when the Signal does not belong to this resolver's framework (so the
// registry can try the next one) and ok=true with a Posture (possibly
// KindUnknown) when it recognises the framework but cannot fully classify.
type Resolver interface {
	// Name is the framework slug (e.g. "django-drf", "nestjs").
	Name() string
	// Resolve attempts to classify the signal. ok=false ⇒ not my framework.
	Resolve(sig Signal) (Posture, bool)
}

// Registry is the ordered set of resolvers. Resolve tries each in registration
// order and returns the first ok=true posture. Order matters only for
// frameworks with overlapping signatures; flagship resolvers are registered
// first.
type Registry struct {
	resolvers []Resolver
}

// NewRegistry builds the default registry with the flagship resolvers wired and
// every other framework registered as an explicit stub (ref #4419 follow-ups).
func NewRegistry() *Registry {
	return &Registry{resolvers: []Resolver{
		// Flagship — fully implemented.
		djangoDRFResolver{},
		nestJSResolver{},
		// Implemented (effective_contract framework wave #4708/#4709/#4710).
		springSecurityResolver{}, // #4708 — @PreAuthorize/@Secured/@RolesAllowed
		fastAPIResolver{},        // #4709 — Depends(auth)/Security scopes
		expressResolver{},        // #4710 — passport/requireAuth/role middleware
		// Stubs — registry members so the shape is fixed; each returns
		// ok=false until its follow-up ticket lands. NOT flagship-only.
		stubResolver{name: "rails"},         // ref #4419 — Pundit/CanCanCan/before_action
		stubResolver{name: "flask"},         // ref #4419 — decorators/before_request
		stubResolver{name: "laravel"},       // ref #4419 — middleware/Gates/Policies
		stubResolver{name: "aspnet"},        // ref #4419 — [Authorize]/policies
		stubResolver{name: "go-middleware"}, // ref #4419 — middleware chains
		stubResolver{name: "phoenix"},       // ref #4419 — plugs
	}}
}

// Resolve runs the registry over a signal, preferring a resolver whose Name
// matches sig.Framework when that hint is present, else first-ok wins.
func (r *Registry) Resolve(sig Signal) (Posture, string) {
	// Honour an explicit framework hint first, if any resolver claims it.
	if fw := strings.ToLower(strings.TrimSpace(sig.Framework)); fw != "" {
		for _, res := range r.resolvers {
			if frameworkMatches(res.Name(), fw) {
				if p, ok := res.Resolve(sig); ok {
					p.Framework = res.Name()
					return p, res.Name()
				}
			}
		}
	}
	for _, res := range r.resolvers {
		if p, ok := res.Resolve(sig); ok {
			p.Framework = res.Name()
			return p, res.Name()
		}
	}
	return Posture{Kind: KindUnknown, Detail: "no resolver recognised the auth signal"}, ""
}

// Frameworks returns the registered resolver names (for provenance / tests).
func (r *Registry) Frameworks() []string {
	out := make([]string, 0, len(r.resolvers))
	for _, res := range r.resolvers {
		out = append(out, res.Name())
	}
	sort.Strings(out)
	return out
}

// frameworkMatches loosely matches a resolver name against a framework hint
// ("django" ↔ "django-drf", "nest" ↔ "nestjs").
func frameworkMatches(resolverName, hint string) bool {
	rn := strings.ToLower(resolverName)
	return strings.Contains(rn, hint) || strings.Contains(hint, strings.Split(rn, "-")[0])
}

// stubResolver is a registered-but-inert resolver for a framework whose posture
// decode is a follow-up ticket. It always declines so the diff core reports an
// honest kind_mismatch rather than a false equivalent for unsupported stacks.
type stubResolver struct{ name string }

func (s stubResolver) Name() string                   { return s.name }
func (s stubResolver) Resolve(Signal) (Posture, bool) { return Posture{}, false }
