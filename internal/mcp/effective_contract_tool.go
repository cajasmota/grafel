package mcp

// effective_contract_tool.go — grafel_effective_contract MCP tool (epic
// #3829, ticket #3836 — T6).
//
// This is the thin SERVING / GROUPING layer over T5's computation
// (effective_contract.go projectEffectiveContract + the engine-stamped
// effective_* props). Given a ViewSet/controller (or a single route/endpoint),
// it gathers that ViewSet's router-expanded routes, projects each into the
// structured per-verb contract, and groups them under the owning ViewSet:
//
//	{ class, framework, handlers: [ {verb, path, kind, source_class,
//	  default_status, error_statuses, serializer, pagination, permissions,
//	  auth_required, behaviour, handler}, ... ] }
//
// It is the single artifact that lets the rewrite agent answer "what is the
// full contract of every verb on this ViewSet?" in one call — preventing the
// #278 defect class (an INHERITED create surfacing kind:inherited,
// source_class:CreateModelMixin, default_status:201, error_statuses:[400] even
// though the ViewSet body is empty).
//
// MRO WIRING (deploy-9 item-4): the tool no longer depends SOLELY on the
// engine-stamped effective_* props. It resolves the same MRO/baseknowledge-pack
// data get_source reads, so it returns a contract wherever get_source can:
//   - BACKFILL: a router-expanded route whose effective_* fields are absent (an
//     index that predates the stamping pass, or an honest-partial stamp) is
//     filled from resolveInheritedEndpoint -> the pack (stamped values always
//     win — never clobbered).
//   - CLASS FALLBACK: when NO router-expanded routes exist for the ViewSet (a
//     real DRF app whose routing the expansion pass did not materialise — the
//     live-daemon empty case), the per-verb contract is synthesized from the
//     ViewSet CLASS entity's EXTENDS edges + the pack, exactly as resolveMember
//     does for get_source.
//
// HONEST-PARTIAL: a verb whose backing route carries no resolvable contract
// field simply omits that field (projectEffectiveContract leaves it zero/empty)
// — nothing is fabricated. A ViewSet with neither router-expanded routes nor a
// pack-resolvable EXTENDS chain returns an empty handlers list with a note, not
// an error.

import (
	"context"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/frameworks/baseknowledge"
	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// effectiveContractGroup is the per-ViewSet wire shape returned by
// grafel_effective_contract: the owning class, its framework, and the
// per-verb effective contracts of its router-expanded routes.
type effectiveContractGroup struct {
	// Class is the owning ViewSet/controller leaf name (the grouping key).
	Class string `json:"class"`
	// Framework is the route framework ("django" for DRF), when known.
	Framework string `json:"framework,omitempty"`
	// Repo is the repo the routes were found in, for cross-repo locating.
	Repo string `json:"repo,omitempty"`
	// Handlers are the per-verb effective contracts, sorted by path then verb.
	Handlers []effectiveContract `json:"handlers"`
}

// effectiveContractResult is the top-level envelope. Groups is one entry per
// owning ViewSet (usually one, but a route/endpoint input could match across
// repos). Note carries the honest-partial / empty-result explanation.
type effectiveContractResult struct {
	Target string                   `json:"target"`
	Groups []effectiveContractGroup `json:"groups"`
	Note   string                   `json:"note,omitempty"`
}

// viewSetNameForRoute returns the owning ViewSet leaf name a router-expanded
// route belongs to, derived from its drf_view_method prop ("ViewSet.method" for
// a verb route, or just "ViewSet" for the ANY catch-all). Empty when the route
// carries no ViewSet attribution.
func viewSetNameForRoute(e *graph.Entity) string {
	dvm := e.PropGet("drf_view_method")
	if dvm == "" {
		return ""
	}
	if owning := prefixBeforeDot(dvm); owning != "" {
		return leafAfterDot(owning)
	}
	// No dot: the ANY catch-all records just the ViewSet name.
	return leafAfterDot(dvm)
}

// resolveEffectiveContractTarget determines the ViewSet leaf name to group by
// from the request's entity_id / qualified_name. Resolution order:
//
//  1. The arg matches a router-expanded route entity → derive its ViewSet.
//  2. The arg matches a class/component entity → use its leaf name.
//  3. The arg matches ANY other entity (e.g. a DRF ViewSet the Python
//     extractor emits as Kind="View" with an empty subtype, so isClassEntity is
//     false for it) → use that entity's leaf name. A resolved entity_id is an
//     explicit user target, so its own name is the ViewSet to group by — this
//     is the #4243 fix: previously a ViewSet entity_id fell through to case 4
//     and grouped by the raw (often hex / repo-prefixed) id, matching nothing.
//  4. The arg matches no entity → treat the raw string's leaf as the ViewSet
//     name (lets callers pass a bare class name that isn't itself indexed as a
//     standalone entity, e.g. when only the routes carry it).
//
// The arg is matched both as given and with any "<repo>::" prefix stripped, so
// a fully-prefixed entity_id (the form grafel_effective_contract is
// normally called with) resolves to its local entity — the LabelIndex is keyed
// by LOCAL id (#4243).
//
// Returns the lower-cased ViewSet leaf to match routes against, plus the
// resolved entity (may be nil for case 4).
func resolveEffectiveContractTarget(lg *LoadedGroup, arg string) (string, *graph.Entity) {
	// Candidate lookup keys: the raw arg, plus its local id if it carries a
	// "<repo>::" prefix (LabelIndex.ByID is keyed by local id, not prefixed).
	keys := []string{arg}
	if _, local := splitPrefixed(arg); local != arg {
		keys = append(keys, local)
	}
	for _, r := range reposToConsider(lg, nil) {
		if r.LabelIndex == nil {
			continue
		}
		for _, key := range keys {
			for _, e := range r.LabelIndex.LookupAll(key) {
				if isRouterExpandedRoute(e) {
					if vs := viewSetNameForRoute(e); vs != "" {
						return strings.ToLower(vs), e
					}
				}
				if isClassEntity(e) {
					return strings.ToLower(leafAfterDot(e.Name)), e
				}
				// Case 3: any other resolved entity (the Kind="View" DRF ViewSet
				// case) — group by its own leaf name. An entity_id the caller
				// passed IS the ViewSet; its name is the grouping key. Router
				// entities are handled above, so this never hijacks a route.
				if e.Name != "" {
					return strings.ToLower(leafAfterDot(e.Name)), e
				}
			}
		}
	}
	// Case 4: no entity matched — fall back to the raw leaf.
	return strings.ToLower(leafAfterDot(arg)), nil
}

// handleEffectiveContract serves grafel_effective_contract. It resolves the
// target ViewSet, gathers its router-expanded routes across the group, projects
// each into a per-verb effective contract, and groups them by owning ViewSet.
func (s *Server) handleEffectiveContract(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	target := argString(req, "entity_id", "")
	if target == "" {
		target = argString(req, "qualified_name", "")
	}
	if target == "" {
		return mcpapi.NewToolResultError("entity_id (or qualified_name) is required"), nil
	}

	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}

	out := computeEffectiveContract(lg, target)
	return jsonResult(out), nil
}

// computeEffectiveContract resolves the target ViewSet within lg, gathers its
// router-expanded routes (or class-fallback synthesis), and returns the grouped
// per-verb effective-contract result. Factored out of handleEffectiveContract so
// the dashboard backend can reuse the EXACT same MRO/pack-aware computation
// (#4254) without going through the MCP request envelope.
func computeEffectiveContract(lg *LoadedGroup, target string) effectiveContractResult {
	wantVS, _ := resolveEffectiveContractTarget(lg, target)

	// Gather, per (repo, ViewSet), the projected per-verb contracts of every
	// router-expanded route owned by the target ViewSet. Keyed by repo so a
	// same-named ViewSet in two repos stays distinct.
	type groupKey struct{ repo, class string }
	groups := map[groupKey]*effectiveContractGroup{}
	var order []groupKey

	for _, r := range reposToConsider(lg, nil) {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isRouterExpandedRoute(e) {
				continue
			}
			vs := viewSetNameForRoute(e)
			if vs == "" || strings.ToLower(vs) != wantVS {
				continue
			}
			c, ok := projectEffectiveContract(e)
			if !ok {
				continue
			}
			// #3964 follow-up: when the route carries no stamped per-verb
			// contract (effective_kind/effective_status absent because the
			// index predates the stamping pass, or the stamp was honest-partial),
			// backfill from the SAME MRO/pack resolution get_source uses
			// (resolveInheritedEndpoint). This is what keeps the tool and
			// get_source from disagreeing on an inherited verb's contract.
			backfillEffectiveContractFromMRO(r, e, &c)
			key := groupKey{repo: r.Repo, class: vs}
			g, exists := groups[key]
			if !exists {
				g = &effectiveContractGroup{
					Class:     vs,
					Framework: e.PropGet("framework"),
					Repo:      r.Repo,
				}
				groups[key] = g
				order = append(order, key)
			}
			g.Handlers = append(g.Handlers, c)
		}
	}

	// CLASS FALLBACK (deploy-9 item-4): when NO router-expanded route entities
	// were found for the ViewSet — the live-daemon empty case on a real DRF app
	// whose routing the expansion pass didn't materialise — synthesize the
	// per-verb contract from the ViewSet CLASS entity's EXTENDS edges + the
	// baseknowledge pack, exactly as get_source's resolveMember does. The data
	// the tool needs is the same MRO-reachable data get_source reads; this makes
	// the tool return it wherever get_source can.
	if len(groups) == 0 {
		for _, r := range reposToConsider(lg, nil) {
			if r.Doc == nil {
				continue
			}
			cls := findViewSetClassEntity(r, wantVS)
			if cls == nil {
				continue
			}
			handlers := synthesizeClassEffectiveContracts(r, cls)
			if len(handlers) == 0 {
				continue
			}
			key := groupKey{repo: r.Repo, class: leafAfterDot(cls.Name)}
			g := &effectiveContractGroup{
				Class:     leafAfterDot(cls.Name),
				Framework: classFramework(cls),
				Repo:      r.Repo,
			}
			g.Handlers = handlers
			groups[key] = g
			order = append(order, key)
		}
	}

	out := effectiveContractResult{Target: target}
	// Deterministic group order: repo, then class.
	sort.Slice(order, func(i, j int) bool {
		if order[i].repo != order[j].repo {
			return order[i].repo < order[j].repo
		}
		return order[i].class < order[j].class
	})
	for _, key := range order {
		g := groups[key]
		sortEffectiveContracts(g.Handlers)
		out.Groups = append(out.Groups, *g)
	}

	// FRAMEWORK REGISTRY (#4601): when the DRF projection + class-fallback
	// synthesis produced nothing (a non-DRF stack — e.g. the NestJS acme-v3
	// rewrite), try the pluggable per-framework contract resolvers. They compose
	// the SAME effectiveContract structure (status set, request fields, per-branch
	// response shapes, auth) from signals that already exist on the graph, so the
	// cross-group response_shape_diff / parity tools consume DRF and non-DRF
	// contracts uniformly. DRF behaviour is unchanged: this runs ONLY when DRF
	// resolved nothing.
	if len(out.Groups) == 0 {
		if rgroups, ok := newContractResolverRegistry().resolve(lg, target, wantVS); ok {
			out.Groups = rgroups
		}
	}

	if len(out.Groups) == 0 {
		out.Note = "no effective contract resolvable for \"" + target + "\": no " +
			"router-expanded routes are attributed to this ViewSet, and its class " +
			"entity carries no EXTENDS edge to a framework base the baseknowledge " +
			"pack recognises (ModelViewSet / GenericViewSet / ReadOnlyModelViewSet / " +
			"APIView / ViewSet). Verify the target resolves to the ViewSet itself " +
			"(pass its entity_id or exact class name, not a method or module), that " +
			"it is a DRF ViewSet, and that its base class is one the pack knows."
	}
	return out
}

// backfillEffectiveContractFromMRO fills the per-verb contract fields a
// router-expanded route left empty (the stamp omitted them, or the index
// predates the stamping pass) from the SAME MRO resolution get_source uses:
// resolveInheritedEndpoint -> the baseknowledge pack contract. It never
// OVERWRITES a stamped value — stamped props win — so an index that already
// carries effective_* is byte-identical to before. Honest-partial: when the MRO
// can't resolve the verb, the empty fields stay empty.
func backfillEffectiveContractFromMRO(r *LoadedRepo, e *graph.Entity, c *effectiveContract) {
	// Only inherited routes carry a pack-resolvable framework contract; explicit
	// bodies and @action verbs have no framework default to backfill.
	res, ok := resolveInheritedEndpoint(r, e)
	if !ok || res.Contract == nil {
		return
	}
	applyPackContract(res, c)
}

// applyPackContract maps a pack-resolved memberResolution into the per-verb
// effectiveContract, filling ONLY the fields the caller left zero/empty so a
// stamped value is never clobbered.
func applyPackContract(res memberResolution, c *effectiveContract) {
	m := res.Contract
	if c.Kind == "" {
		c.Kind = "inherited"
	}
	if c.SourceClass == "" {
		c.SourceClass = res.DefiningClass
	}
	if c.Verb == "" {
		c.Verb = m.HTTPVerb
	}
	if c.DefaultStatus == 0 && m.DefaultStatus != baseknowledge.StatusUnknown {
		c.DefaultStatus = m.DefaultStatus
	}
	if len(c.ErrorStatuses) == 0 && len(m.ErrorStatuses) > 0 {
		c.ErrorStatuses = append([]int(nil), m.ErrorStatuses...)
	}
	if c.Behaviour == "" {
		c.Behaviour = m.Behaviour
	}
}

// findViewSetClassEntity returns the ViewSet declaration entity whose leaf name
// (lower-cased) matches wantVS, used by the class-fallback synthesis when no
// router-expanded routes exist for the ViewSet.
//
// It accepts not only isClassEntity nodes (SCOPE.Component / class subtype) but
// any entity that owns at least one EXTENDS/IMPLEMENTS edge — because the Python
// extractor emits a DRF ViewSet as Kind="View" with an empty subtype (so
// isClassEntity is false for it) yet still records the inheritance edge to
// ModelViewSet that the synthesis walks. Restricting to "has an EXTENDS edge"
// keeps this from matching unrelated same-named non-class entities.
func findViewSetClassEntity(r *LoadedRepo, wantVS string) *graph.Entity {
	if r.Doc == nil {
		return nil
	}
	var fallback *graph.Entity
	for i := range r.Doc.Entities {
		e := &r.Doc.Entities[i]
		if strings.ToLower(leafAfterDot(e.Name)) != wantVS {
			continue
		}
		if isClassEntity(e) {
			return e
		}
		if fallback == nil && len(extendsBases(r, e)) > 0 {
			fallback = e
		}
	}
	return fallback
}

// classFramework reports the route framework leaf for a ViewSet class entity,
// from its framework property when set, defaulting to "django" for a
// pack-recognised DRF base. Empty when neither is known.
func classFramework(cls *graph.Entity) string {
	if fw := cls.PropGet("framework"); fw != "" {
		return fw
	}
	return "django"
}

// synthesizeClassEffectiveContracts builds the per-verb effective contract for a
// ViewSet class entity directly from its EXTENDS edges + the baseknowledge pack
// — the same resolution get_source performs on a ViewSet's inherited method
// entity. For each known base (e.g. ModelViewSet, ListModelMixin), every member
// the base contributes is emitted as one inherited per-verb contract. Returns
// nil when the class extends no pack-known base (honest-partial: nothing
// synthesized rather than a fabricated contract).
func synthesizeClassEffectiveContracts(r *LoadedRepo, cls *graph.Entity) []effectiveContract {
	reg := baseknowledge.Default()
	owning := leafAfterDot(cls.Name)
	serializer := cls.PropGet("serializer_class")

	// BFS the EXTENDS graph so a ViewSet that subclasses an in-repo base which
	// itself extends ModelViewSet still resolves the inherited verbs.
	type byVerb struct {
		member        baseknowledge.Member
		definingClass string
	}
	resolved := map[string]byVerb{} // member name -> contract (first match wins)
	visited := map[string]bool{cls.ID: true}
	frontier := extendsBases(r, cls)
	for len(frontier) > 0 {
		var next []baseRef
		for _, b := range frontier {
			for name, m := range reg.MembersOf(b.name) {
				if _, seen := resolved[name]; seen {
					continue
				}
				dc := m.DefiningClass
				if dc == "" {
					dc = canonicalBaseFQN(reg, b.name)
				}
				resolved[name] = byVerb{member: m, definingClass: dc}
			}
			if b.entity != nil && !visited[b.entity.ID] {
				visited[b.entity.ID] = true
				next = append(next, extendsBases(r, b.entity)...)
			}
		}
		frontier = next
	}
	if len(resolved) == 0 {
		return nil
	}

	out := make([]effectiveContract, 0, len(resolved))
	for _, v := range resolved {
		c := effectiveContract{
			Handler:    owning + "." + v.member.Name,
			Serializer: serializer,
		}
		res := memberResolution{
			Provenance:    provInheritedExternal,
			Member:        v.member.Name,
			OwningClass:   owning,
			DefiningClass: v.definingClass,
			Contract:      &v.member,
		}
		applyPackContract(res, &c)
		c.Pagination = v.member.PaginationApplicable
		out = append(out, c)
	}
	return out
}

// sortEffectiveContracts orders per-verb contracts deterministically by path
// then verb so the output is stable across calls.
func sortEffectiveContracts(cs []effectiveContract) {
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].Path != cs[j].Path {
			return cs[i].Path < cs[j].Path
		}
		return cs[i].Verb < cs[j].Verb
	})
}
