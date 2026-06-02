package mcp

// mro.go — MRO-aware member resolution for archigraph_get_source /
// archigraph_inspect (epic #3829, ticket #3833 — PR A3).
//
// Problem (the rewrite agent's G2): when a class inherits a member it never
// declares — a DRF `RoleViewSet(ModelViewSet)` inherits `retrieve` from
// `rest_framework.mixins.RetrieveModelMixin`, a Go struct promotes an
// embedded type's method — `get_source` and `inspect` previously resolved the
// entity to the SUBCLASS file (or a bodyless synthetic with only a signature)
// and stopped there. The caller never saw the DEFINING base body, the default
// status, or even an indication that the member was inherited.
//
// resolveMember closes that gap. Given an entity that is an inherited member
// (a bodyless DRF synthetic `SCOPE.Operation`, or a method whose owning class
// declares it only via an inherited_methods annotation), it walks the owning
// class's EXTENDS edges to find the DEFINING class:
//
//   (a) an INDEXED base class in the same repo that declares the member with a
//       real body  -> resolve to that entity's body span (provenance
//       inheritedInRepo), OR
//   (b) an EXTERNAL library base the baseknowledge pack knows (DRF mixins,
//       ...) -> synthesize an "external body" stub from the pack contract
//       (defining class FQN, default status, behaviour, doc URL), provenance
//       inheritedExternal.
//
// HONEST-PARTIAL (quality-first): when the member cannot be tied to a defining
// class — unknown base, not in the pack, no in-repo declaration — resolveMember
// returns an UNRESOLVED result and the callers fall back to the current
// behaviour. It NEVER fabricates a body or a status.
//
// This is a pure read-path resolver; it adds no graph entities or edges. The
// indexer-side INHERITS/OVERRIDES edge is a separate ticket (PR A2 #3832-era);
// this PR is the MCP projection (DEPLOY-DEFERRED: needs a daemon rebuild to
// serve).

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/frameworks/baseknowledge"
	"github.com/cajasmota/archigraph/internal/graph"
)

// memberProvenance classifies how resolveMember tied a member to its body.
type memberProvenance string

const (
	// provExplicit: the entity declares its own body (StartLine span). Not
	// inherited — callers keep the existing behaviour.
	provExplicit memberProvenance = "explicit"
	// provInheritedInRepo: resolved to a base class declared in the indexed
	// repo whose body get_source should return instead of the subclass.
	provInheritedInRepo memberProvenance = "inherited"
	// provInheritedExternal: resolved to an external library base via the
	// baseknowledge pack; the body is a synthesized contract stub.
	provInheritedExternal memberProvenance = "inherited_external"
	// provUnresolved: the member is inherited but no defining class could be
	// found. Honest-partial: callers fall back, never fabricate.
	provUnresolved memberProvenance = "unresolved"
)

// memberResolution is the outcome of resolveMember.
type memberResolution struct {
	// Provenance is the classification above.
	Provenance memberProvenance

	// Member is the inherited member's bare name (e.g. "retrieve"). Empty when
	// the entity was not recognised as a member at all.
	Member string

	// OwningClass is the subclass that inherits the member (e.g.
	// "RoleViewSet"). Empty when not determinable.
	OwningClass string

	// DefiningClass is the FQN of the class that actually defines the member's
	// body — an in-repo base name or the external pack FQN
	// (e.g. "rest_framework.mixins.RetrieveModelMixin"). Empty when unresolved.
	DefiningClass string

	// DefiningEntity is the in-repo entity whose body defines the member, set
	// only for provInheritedInRepo. get_source returns this entity's source.
	DefiningEntity *graph.Entity

	// Contract is the pack member contract, set only for provInheritedExternal.
	Contract *baseknowledge.Member

	// Note explains an unresolved outcome (which base was unknown, etc.).
	Note string
}

// IsInherited reports whether the member resolved to a defining class
// different from the entity itself (in-repo base or external pack).
func (r memberResolution) IsInherited() bool {
	return r.Provenance == provInheritedInRepo || r.Provenance == provInheritedExternal
}

// resolveMember performs the MRO walk for entity e in repo lr. It is the
// single shared resolver consumed by handleGetNodeSource and handleGetNode.
//
// Resolution steps:
//  1. Classify the entity as a member and find its (member name, owning class).
//     - A DRF synthetic op (pattern_type=drf_viewset_implicit_method) carries
//     drf_method_origin + viewset_class explicitly.
//     - Any other entity: derive member = leaf of the dotted name, owning
//     class = the prefix; an entity with a real body is provExplicit.
//  2. Find the owning class entity in the repo.
//  3. BFS the owning class's EXTENDS edges. For each base, in declared order:
//     (a) in-repo base entity declaring the member with a body -> resolve;
//     (b) else pack.Member(base, member) -> external stub.
//  4. Nothing matched -> provUnresolved.
func resolveMember(lr *LoadedRepo, e *graph.Entity) memberResolution {
	if lr == nil || lr.Doc == nil || e == nil {
		return memberResolution{Provenance: provUnresolved, Note: "no repo/entity"}
	}

	// #3973 — endpoint→mixin bridge. The rewrite agent navigates via the
	// ENDPOINT (a router-expanded http_endpoint), not the inherited-method
	// stub. An inherited endpoint never reaches classifyMember (it is not a DRF
	// synthetic op nor a dotted-name method), so get_source/neighbors on it
	// never hopped to the defining mixin. The endpoint already carries the
	// resolution it needs (provenance:inherited + defining_class + the
	// ViewSet-qualified drf_view_method), so resolve it directly without an
	// EXTENDS walk (the endpoint owns no EXTENDS edges of its own).
	if res, ok := resolveInheritedEndpoint(lr, e); ok {
		return res
	}

	member, owningName, isBodyless := classifyMember(e)
	if member == "" || owningName == "" {
		// Not a recognisable inherited member — treat as explicit (caller keeps
		// existing behaviour).
		return memberResolution{Provenance: provExplicit}
	}

	// An entity that already has its own real body is explicit UNLESS it is a
	// bodyless synthetic (those carry a signature but no span). A DRF synthetic
	// op is bodyless by construction.
	if !isBodyless && hasRealBody(e) {
		return memberResolution{Provenance: provExplicit, Member: member, OwningClass: owningName}
	}

	owning := findClassEntity(lr, owningName)
	if owning == nil {
		// We know the member name but not the class declaration. Can't walk
		// EXTENDS — but a DRF synthetic still records its origin verb, so try
		// the pack against the recorded mixin set if we have one; otherwise
		// unresolved.
		return memberResolution{
			Provenance:  provUnresolved,
			Member:      member,
			OwningClass: owningName,
			Note:        "owning class entity not found in index",
		}
	}

	reg := baseknowledge.Default()

	// BFS over EXTENDS edges in declared order. The first defining match wins
	// (left-to-right, good enough for the flat framework-mixin case; full C3 is
	// a documented non-goal for v1 per the plan's risk notes).
	visited := map[string]bool{owning.ID: true}
	frontier := extendsBases(lr, owning)
	var unknownBases []string

	for len(frontier) > 0 {
		var next []baseRef
		for _, b := range frontier {
			// (a) in-repo base that declares the member with a body.
			if b.entity != nil {
				if def := classDeclaredMember(lr, b.entity, member); def != nil {
					return memberResolution{
						Provenance:     provInheritedInRepo,
						Member:         member,
						OwningClass:    owningName,
						DefiningClass:  b.name,
						DefiningEntity: def,
					}
				}
			}
			// (b) external library base known to the pack.
			if m, ok := reg.Member(b.name, member); ok {
				contract := m
				defining := contract.DefiningClass
				if defining == "" {
					// The pack matched the base but didn't attribute a deeper
					// defining class (member is owned by the base itself).
					defining = canonicalBaseFQN(reg, b.name)
				}
				return memberResolution{
					Provenance:    provInheritedExternal,
					Member:        member,
					OwningClass:   owningName,
					DefiningClass: defining,
					Contract:      &contract,
				}
			}
			// Record an unknown base for the honest-partial note, then keep
			// walking THROUGH in-repo bases (their own EXTENDS edges).
			if _, known := reg.Lookup(b.name); !known && b.entity == nil {
				unknownBases = append(unknownBases, b.name)
			}
			if b.entity != nil && !visited[b.entity.ID] {
				visited[b.entity.ID] = true
				next = append(next, extendsBases(lr, b.entity)...)
			}
		}
		frontier = next
	}

	note := "no defining class found via EXTENDS or knowledge pack"
	if len(unknownBases) > 0 {
		sort.Strings(unknownBases)
		note = "unrecognised base(s): " + strings.Join(dedupe(unknownBases), ", ")
	}
	return memberResolution{
		Provenance:  provUnresolved,
		Member:      member,
		OwningClass: owningName,
		Note:        note,
	}
}

// isInheritedEndpointEntity reports whether e is a router-expanded
// http_endpoint that the engine tagged as inheriting its handler verb from a
// base/mixin (provenance:inherited, #3831). These carry defining_class + the
// ViewSet-qualified drf_view_method but no body and no EXTENDS edges of their
// own — the inheritance fact lives entirely in their properties.
func isInheritedEndpointEntity(e *graph.Entity) bool {
	if e == nil || e.Properties == nil {
		return false
	}
	if !isEndpointEntity(e) {
		return false
	}
	return e.Properties["provenance"] == drfRouteProvInherited
}

// isEndpointEntity reports whether e is an http_endpoint route entity,
// tolerating the scope-prefixed and bare spellings the index uses plus the
// DRF router-expansion pattern tag.
func isEndpointEntity(e *graph.Entity) bool {
	if isHTTPEndpointKind(e.Kind) {
		return true
	}
	return e.Properties != nil && e.Properties["pattern_type"] == "drf_router_expanded"
}

// drfRouteProvInherited mirrors the engine's drfProvInherited route-provenance
// tag value (internal/engine/django_drf_actions.go). Duplicated here as a
// const so the MCP read path recognises an inherited endpoint without a
// cross-package import of an engine-internal symbol.
const drfRouteProvInherited = "inherited"

// resolveInheritedEndpoint bridges an inherited http_endpoint to the contract /
// body of the mixin verb that backs it (#3973). Unlike a method stub, the
// endpoint carries the resolution explicitly:
//   - defining_class:  the implementing mixin (FQN or bare leaf, e.g.
//     "rest_framework.mixins.ListModelMixin" / "ListModelMixin").
//   - drf_view_method: the ViewSet-qualified handler ("UserProfileViewSet.list")
//     or, for an ANY catch-all, just the ViewSet name.
//
// Resolution, mirroring resolveMember's EXTENDS-walk outcomes:
//   - in-repo defining class that declares the verb with a body -> resolve to
//     that entity (provInheritedInRepo), get_source returns the base body.
//   - external mixin known to the pack -> provInheritedExternal with the pack
//     contract, get_source synthesizes the contract stub.
//   - honest-partial: defining_class missing/unknown OR member underivable ->
//     (zero, false) so resolveMember falls through to the normal path (which,
//     for an endpoint, is provExplicit — its own real body unchanged).
//
// The second return is false when e is NOT an inherited endpoint, so the
// negative case (explicit endpoint) is untouched.
func resolveInheritedEndpoint(lr *LoadedRepo, e *graph.Entity) (memberResolution, bool) {
	if !isInheritedEndpointEntity(e) {
		return memberResolution{}, false
	}
	defining := e.Properties["defining_class"]
	viewMethod := e.Properties["drf_view_method"]
	if defining == "" || viewMethod == "" {
		// honest-partial: tagged inherited but no defining class / no handler
		// to attribute. Fall through — never fabricate.
		return memberResolution{}, false
	}
	// drf_view_method is "ViewSet.verb" for a verb route; an ANY catch-all
	// records just the ViewSet with no verb. Without a verb we cannot name a
	// member contract — fall through honestly.
	member := leafAfterDot(viewMethod)
	owning := prefixBeforeDot(viewMethod)
	if owning == "" || member == "" || member == viewMethod {
		return memberResolution{}, false
	}

	// (a) in-repo defining class that declares the verb with a real body.
	if def := findClassEntity(lr, defining); def != nil {
		if m := classDeclaredMember(lr, def, member); m != nil {
			return memberResolution{
				Provenance:     provInheritedInRepo,
				Member:         member,
				OwningClass:    owning,
				DefiningClass:  defining,
				DefiningEntity: m,
			}, true
		}
	}

	// (b) external mixin known to the baseknowledge pack.
	reg := baseknowledge.Default()
	if m, ok := reg.Member(defining, member); ok {
		contract := m
		dc := contract.DefiningClass
		if dc == "" {
			dc = canonicalBaseFQN(reg, defining)
		}
		return memberResolution{
			Provenance:    provInheritedExternal,
			Member:        member,
			OwningClass:   owning,
			DefiningClass: dc,
			Contract:      &contract,
		}, true
	}

	// honest-partial: tagged inherited with a defining class neither indexed
	// nor in the pack. Fall through to the normal path rather than fabricate.
	return memberResolution{}, false
}

// classifyMember extracts (memberName, owningClassName, isBodylessSynthetic)
// from an entity. Returns empty member when the entity is not a class member.
func classifyMember(e *graph.Entity) (member, owning string, bodyless bool) {
	// DRF synthetic implicit method: the engine stamps the origin verb and the
	// owning ViewSet explicitly. These are bodyless by construction.
	if e.Properties["pattern_type"] == "drf_viewset_implicit_method" {
		m := e.Properties["drf_method_origin"]
		o := e.Properties["viewset_class"]
		if m == "" {
			// Fall back to the dotted name leaf.
			m = leafAfterDot(e.Name)
		}
		if o == "" {
			o = prefixBeforeDot(e.Name)
		}
		return m, o, true
	}

	// A general method entity: name is "Owner.member" (qualified) — split it.
	name := e.QualifiedName
	if name == "" {
		name = e.Name
	}
	if !strings.Contains(name, ".") {
		return "", "", false
	}
	return leafAfterDot(name), prefixBeforeDot(name), false
}

// hasRealBody reports whether the entity carries a usable source span.
func hasRealBody(e *graph.Entity) bool {
	return e.StartLine > 0 && e.EndLine >= e.StartLine
}

// baseRef is a resolved inheritance target: the written base name plus, when
// the base is declared in the indexed repo, the in-repo entity. `viaImplements`
// records that the edge was an IMPLEMENTS (interface / trait) rather than an
// EXTENDS — used for interface-default-method resolution (#3839).
type baseRef struct {
	name          string        // base class name as written / FQN if available
	entity        *graph.Entity // non-nil when the base is an indexed class
	viaImplements bool          // true when reached via IMPLEMENTS (interface default)
}

// extendsBases returns the inheritance bases of class entity c, in edge order.
//
// It walks BOTH the EXTENDS edges (superclass / Go struct embedding) AND the
// IMPLEMENTS edges (interface / trait), so the MRO resolver can promote:
//   - a Go struct's embedded-type methods (EXTENDS kind=embedded_struct, #3839),
//   - a Java/Kotlin class's inherited interface DEFAULT methods (IMPLEMENTS to an
//     interface whose method carries a real body, #3839).
//
// Following IMPLEMENTS is safe because the actual member resolution
// (classDeclaredMember) requires the base member to have a REAL BODY: an
// abstract interface method (no body, the common case) simply won't match, so
// only genuine default-method bodies resolve. An external/abstract interface
// with no indexed body falls through to the honest-unresolved path — never
// fabricated.
//
// The base name prefers the edge's base_name property (the dotted FQN, PR A1)
// and falls back to the ToID leaf / target entity name.
func extendsBases(lr *LoadedRepo, c *graph.Entity) []baseRef {
	adj := lr.getAdjacency()
	rels := lr.Doc.Relationships
	var out []baseRef
	for _, ed := range adj.Outgoing(c.ID) {
		if ed.kind != "EXTENDS" && ed.kind != "IMPLEMENTS" {
			continue
		}
		name := ""
		if ed.relIdx >= 0 && ed.relIdx < len(rels) {
			name = rels[ed.relIdx].Properties["base_name"]
		}
		target := lr.LabelIndex.ByID[ed.target]
		if name == "" {
			if target != nil && target.QualifiedName != "" {
				name = target.QualifiedName
			} else if target != nil {
				name = target.Name
			} else {
				name = leafAfterColon(ed.target)
			}
		}
		out = append(out, baseRef{name: name, entity: target, viaImplements: ed.kind == "IMPLEMENTS"})
	}
	return out
}

// findClassEntity finds the class/component entity for the given (possibly
// dotted) class name in the repo. Prefers an exact label/qname match for a
// class-kind entity.
func findClassEntity(lr *LoadedRepo, name string) *graph.Entity {
	leaf := leafAfterDot(name)
	var fallback *graph.Entity
	for _, cand := range []string{name, leaf} {
		for _, e := range lr.LabelIndex.LookupAll(cand) {
			if isClassEntity(e) {
				return e
			}
			if fallback == nil {
				fallback = e
			}
		}
	}
	return fallback
}

// isClassEntity reports whether e is a class/struct/component declaration that
// can host members.
func isClassEntity(e *graph.Entity) bool {
	if e.Kind == "SCOPE.Component" {
		return true
	}
	switch e.Subtype {
	case "class", "struct", "interface":
		return true
	}
	return false
}

// classDeclaredMember finds an in-repo member entity (method) named `member`
// owned by class `cls` that carries a real body. Returns nil when the class
// does not declare the member with a body in the index.
func classDeclaredMember(lr *LoadedRepo, cls *graph.Entity, member string) *graph.Entity {
	clsLeaf := leafAfterDot(cls.Name)
	// Candidate qualified names a member method would carry, matched against
	// the index's by-qualified-name map (members are keyed "Owner.member").
	candidates := []string{clsLeaf + "." + member}
	if cls.QualifiedName != "" && cls.QualifiedName != clsLeaf {
		candidates = append(candidates, cls.QualifiedName+"."+member)
	}
	for _, qn := range candidates {
		// The qualified-name key "Owner.member" already pins the member to its
		// owning class, so we do NOT require the member to live in the same file
		// as the (sub)class entity we walked from. This is load-bearing for
		// cross-file inheritance: a Go struct embedding `*BaseService` declared
		// in another file, or a Java class implementing an interface whose
		// default method body lives in the interface's own file, must still
		// resolve to that out-of-file body (#3839).
		if e := lr.LabelIndex.ByQName[strings.ToLower(qn)]; e != nil &&
			isMemberEntity(e) && hasRealBody(e) {
			return e
		}
	}
	// Fall back to a same-file scan: a member whose leaf matches and whose
	// owning prefix is the class (handles QName-shape variation). This path
	// stays file-scoped because it matches on a bare leaf+prefix (no globally
	// unique key), so a cross-file scan could pull an unrelated same-named
	// method; the ByQName path above already covers the cross-file case.
	for i := range lr.Doc.Entities {
		e := &lr.Doc.Entities[i]
		if e.SourceFile != cls.SourceFile || !isMemberEntity(e) || !hasRealBody(e) {
			continue
		}
		qn := e.QualifiedName
		if qn == "" {
			qn = e.Name
		}
		if leafAfterDot(qn) == member && prefixBeforeDot(qn) == clsLeaf {
			return e
		}
	}
	return nil
}

// isMemberEntity reports whether e is a method/operation that can be a member
// body.
func isMemberEntity(e *graph.Entity) bool {
	if e.Kind == "SCOPE.Operation" {
		return true
	}
	switch e.Subtype {
	case "method", "function":
		return true
	}
	return false
}

// canonicalBaseFQN returns the pack's most-qualified FQN for a base name, so a
// bare `RetrieveModelMixin` lookup reports the dotted defining class.
func canonicalBaseFQN(reg *baseknowledge.Registry, name string) string {
	if c, ok := reg.Lookup(name); ok && len(c.FQNs) > 0 {
		return c.FQNs[0]
	}
	return name
}

// synthesizeExternalBody renders a get_source body view for an external,
// pack-resolved member. It is explicitly marked as the DEFINING base contract,
// NOT the subclass body, so the consumer is never misled into thinking it read
// the subclass source.
func synthesizeExternalBody(r memberResolution) string {
	m := r.Contract
	var b strings.Builder
	fmt.Fprintf(&b, "# archigraph: synthesized inherited-member contract (NOT subclass source)\n")
	fmt.Fprintf(&b, "# %s.%s is INHERITED — defined by %s\n", r.OwningClass, r.Member, r.DefiningClass)
	fmt.Fprintf(&b, "# resolved via EXTENDS -> baseknowledge pack (external library base)\n")
	if m != nil {
		if m.HTTPVerb != "" {
			fmt.Fprintf(&b, "# http_verb: %s\n", m.HTTPVerb)
		}
		if m.DefaultStatus != baseknowledge.StatusUnknown {
			fmt.Fprintf(&b, "# default_status: %d\n", m.DefaultStatus)
		}
		if len(m.ErrorStatuses) > 0 {
			parts := make([]string, len(m.ErrorStatuses))
			for i, s := range m.ErrorStatuses {
				parts[i] = fmt.Sprintf("%d", s)
			}
			fmt.Fprintf(&b, "# error_statuses: %s\n", strings.Join(parts, ", "))
		}
		if m.Behaviour != "" {
			fmt.Fprintf(&b, "# behaviour: %s\n", m.Behaviour)
		}
		if m.DocURL != "" {
			fmt.Fprintf(&b, "# doc: %s\n", m.DocURL)
		}
	}
	fmt.Fprintf(&b, "#\n")
	fmt.Fprintf(&b, "# The defining body lives in the external library %s and is not\n", r.DefiningClass)
	fmt.Fprintf(&b, "# part of the indexed source tree; the contract above is from the\n")
	fmt.Fprintf(&b, "# curated baseknowledge pack.\n")
	return b.String()
}

// inspectInheritance builds the archigraph_inspect "inheritance" section for an
// entity, surfacing the MRO resolution so the consumer knows the member is
// inherited and from where (#3833). Returns nil when the entity is not an
// inherited member (explicit bodies and non-members get no section, keeping the
// envelope lean — the existing omit-when-empty convention).
//
// For an UNRESOLVED inherited member it still emits a section marked
// resolved=false with the note, so the consumer learns the member is inherited
// even when we can't name the defining class (honest-partial, not silent).
func inspectInheritance(lr *LoadedRepo, e *graph.Entity) map[string]any {
	res := resolveMember(lr, e)
	switch res.Provenance {
	case provInheritedInRepo:
		out := map[string]any{
			"inherited":      true,
			"resolved":       true,
			"member":         res.Member,
			"owning_class":   res.OwningClass,
			"defining_class": res.DefiningClass,
			"resolved_from":  "extends_in_repo",
		}
		if res.DefiningEntity != nil {
			out["defining_file"] = res.DefiningEntity.SourceFile
			out["defining_line"] = res.DefiningEntity.StartLine
		}
		return out
	case provInheritedExternal:
		out := map[string]any{
			"inherited":      true,
			"resolved":       true,
			"member":         res.Member,
			"owning_class":   res.OwningClass,
			"defining_class": res.DefiningClass,
			"resolved_from":  "baseknowledge_pack",
			"external":       true,
		}
		if m := res.Contract; m != nil {
			if m.HTTPVerb != "" {
				out["http_verb"] = m.HTTPVerb
			}
			if m.DefaultStatus != baseknowledge.StatusUnknown {
				out["default_status"] = m.DefaultStatus
			}
			if len(m.ErrorStatuses) > 0 {
				out["error_statuses"] = m.ErrorStatuses
			}
			if m.Behaviour != "" {
				out["behaviour"] = m.Behaviour
			}
			if m.DocURL != "" {
				out["doc_url"] = m.DocURL
			}
		}
		return out
	case provUnresolved:
		// Only surface a section when the entity actually looks like an
		// inherited member (member name known) — a plain top-level entity that
		// happens to have no body should not grow an inheritance block.
		if res.Member == "" {
			return nil
		}
		return map[string]any{
			"inherited":    true,
			"resolved":     false,
			"member":       res.Member,
			"owning_class": res.OwningClass,
			"note":         res.Note,
		}
	default:
		return nil
	}
}

// --- small string helpers ----------------------------------------------------

func leafAfterDot(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// leafAfterColon returns the trailing colon-separated segment of an EXTENDS
// edge ToID (the structural-ref shape ends with the bare class name).
func leafAfterColon(s string) string {
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func prefixBeforeDot(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		return s[:i]
	}
	return ""
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
