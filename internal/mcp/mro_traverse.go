package mcp

// mro_traverse.go — MRO-aware TRAVERSAL for grafel_neighbors / _def_use /
// _trace (epic #3829, ticket #3834 — MRO T4).
//
// Problem (the rewrite agent's G5): an inherited member STUB has an empty call
// graph. A DRF `RoleViewSet.retrieve` synthetic, or a `ChildService.handle` the
// child never redeclares, owns NO outbound CALLS edges — the body lives on the
// base/mixin, not the subclass. So neighbors(out), def_use, and trace all
// DEAD-END at the bodyless node: callees come back empty, and a shortest path
// that arrives at the stub can never reach the real implementation.
//
// T3 (#3833, mro.go) already resolves the stub → its DEFINING member for
// get_source / inspect. T4 reuses that resolver on the TRAVERSAL surfaces:
// when a walk reaches an inherited stub, it follows an INHERITS hop to the
// defining member's body and continues from THAT node's real edges.
//
//   (a) in-repo base member (provInheritedInRepo): the defining method is an
//       indexed entity with a real body and its own CALLS edges. We splice an
//       INHERITS edge stub→defining, so the BFS/Dijkstra continues through the
//       defining member's actual callees. trace/def_use/neighbors reach the
//       real base implementation.
//
//   (b) external mixin (provInheritedExternal): the defining body is NOT in the
//       index — it lives in the library (DRF RetrieveModelMixin, …). HONEST-
//       PARTIAL: we surface ONE INHERITS edge to a synthetic, deterministic
//       contract node (id "inherits-ext:<DefiningClass>.<member>") that carries
//       the pack contract, and mark it external. We DO NOT fabricate call edges
//       from that node — the traversal endpoint is the contract, not invented
//       callees.
//
//   (c) unresolved: no synthetic edge — honest dead-end (the existing
//       behaviour). resolveMember already guarantees no fabrication here.
//
// This is a PURE READ-PATH projection. It adds NO edges to the stored graph and
// needs no reindex to start working (DEPLOY-DEFERRED only for the daemon
// rebuild that serves the new read path). When an indexer producer later
// materialises a real INHERITS edge, these synthetic edges become redundant and
// the helpers below skip stubs that already carry an explicit INHERITS edge.

import (
	"sync/atomic"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Observability / test hooks for the #5791 read-path fix. These count the two
// operations whose cost the fix collapses so tests can assert by CALL-COUNT
// (the 88s wall-clock outlier is not locally reproducible):
//   - mroBuildCount: full buildMROInbound runs (should be one per contentHash).
//   - baseChainComputeCount: base-chain resolutions that MISSED the per-class
//     memo (the O(classes) witness — pre-fix this was one walk per member).
var (
	mroBuildCount         atomic.Int64
	baseChainComputeCount atomic.Int64
)

// inheritsEdgeKind is the member-granularity inheritance edge kind (#3834).
const inheritsEdgeKind = string(types.RelationshipKindInherits)

// externalContractIDPrefix namespaces the synthetic external-contract node IDs
// so they never collide with real entity IDs and are recognisably synthetic.
const externalContractIDPrefix = "inherits-ext:"

// mroEdge is a synthetic outbound edge produced by the MRO projection. It
// mirrors the shape the traversal handlers consume (target localID + kind),
// plus enough metadata for trace/neighbors to label the hop and, for the
// external case, to render the contract endpoint without a backing entity.
type mroEdge struct {
	// Target is the LOCAL (unprefixed) id of the defining member, OR — for the
	// external case — the synthetic contract node id (externalContractIDPrefix…).
	Target string
	// Kind is always inheritsEdgeKind.
	Kind string
	// External is true for the pack-resolved contract endpoint (no in-repo body).
	External bool
	// DefiningClass / Member describe the resolution for labelling.
	DefiningClass string
	Member        string
	// Contract is the synthetic external node's display entity (external case
	// only); nil for the in-repo case where Target is a real indexed entity.
	Contract *graph.Entity
}

// mroOutboundEdges returns the synthetic INHERITS edge(s) for local entity id
// `local` in repo lr, or nil when the entity is not an inherited stub (explicit
// body, non-member, or unresolved). At most one edge is returned today (a
// member resolves to a single defining body via left-to-right MRO).
//
// The returned edge is what neighbors(out) / trace should ADD to the entity's
// real outbound edges so traversal can hop to the defining body. The caller is
// responsible for continuing the walk from the in-repo target's own edges (the
// target is a normal indexed entity); the external target is a leaf contract.
func mroOutboundEdges(lr *LoadedRepo, local string) []mroEdge {
	if lr == nil || lr.Doc == nil {
		return nil
	}
	e := lr.LabelIndex.ByID(local)
	if e == nil {
		return nil
	}
	// If the graph already carries a real INHERITS edge from this stub, the
	// indexer materialised it — do not double-project.
	if adj := lr.getAdjacency(); adj != nil {
		for _, ed := range adj.Outgoing(local) {
			if ed.kind == inheritsEdgeKind {
				return nil
			}
		}
	}

	res := resolveMember(lr, e)
	switch res.Provenance {
	case provInheritedInRepo:
		if res.DefiningEntity == nil || res.DefiningEntity.ID == local {
			return nil
		}
		return []mroEdge{{
			Target:        res.DefiningEntity.ID,
			Kind:          inheritsEdgeKind,
			External:      false,
			DefiningClass: res.DefiningClass,
			Member:        res.Member,
		}}
	case provInheritedExternal:
		node := externalContractEntity(res)
		return []mroEdge{{
			Target:        node.ID,
			Kind:          inheritsEdgeKind,
			External:      true,
			DefiningClass: res.DefiningClass,
			Member:        res.Member,
			Contract:      node,
		}}
	default:
		// provExplicit / provUnresolved: honest dead-end, no synthetic edge.
		return nil
	}
}

// externalContractID returns the deterministic synthetic id for an external
// pack-resolved contract endpoint.
func externalContractID(definingClass, member string) string {
	return externalContractIDPrefix + definingClass + "." + member
}

// externalContractEntity builds the synthetic, in-memory contract node that
// represents an external (pack-described) defining member. It is NOT stored in
// the graph — it exists only so neighbors/trace can name the resolution
// endpoint. It carries no source span (StartLine 0) and the pack contract on
// its Properties so a consumer can read the default status / behaviour.
func externalContractEntity(res memberResolution) *graph.Entity {
	id := externalContractID(res.DefiningClass, res.Member)
	props := map[string]string{
		"synthetic":      "true",
		"external":       "true",
		"member":         res.Member,
		"defining_class": res.DefiningClass,
		"resolved_from":  "baseknowledge_pack",
	}
	if m := res.Contract; m != nil {
		if m.HTTPVerb != "" {
			props["http_verb"] = m.HTTPVerb
		}
		if m.Behaviour != "" {
			props["behaviour"] = m.Behaviour
		}
		if m.DocURL != "" {
			props["doc_url"] = m.DocURL
		}
	}
	return graph.EntityPtr(graph.Entity{
		ID:            id,
		Name:          res.DefiningClass + "." + res.Member,
		QualifiedName: res.DefiningClass + "." + res.Member,
		Kind:          "SCOPE.External",
		Subtype:       "method",
		Language:      "",
	}.WithProperties(props))
}

// buildMROInbound computes the reverse-INHERITS map for repo lr: for every
// inherited stub that resolves (in-repo) to a defining member, it records the
// defining member's id -> the stub id. Only in-repo defining members are keyed
// — an external pack contract has no in-repo node whose callers a user could
// query. Built once and cached on LoadedRepo (#3834).
func buildMROInbound(lr *LoadedRepo) map[string][]string {
	mroBuildCount.Add(1)
	out := map[string][]string{}
	if lr == nil || lr.Doc == nil {
		return out
	}
	for i := range lr.Doc.Entities {
		e := &lr.Doc.Entities[i]
		if !isMemberEntity(e) {
			continue
		}
		for _, me := range mroOutboundEdges(lr, e.ID) {
			if me.External {
				continue
			}
			out[me.Target] = append(out[me.Target], e.ID)
		}
	}
	return out
}

// mroInboundEdges returns the inherited-stub ids that resolve to in-repo
// defining member `local` (the reverse-INHERITS callers). Empty when `local` is
// not a defining base member that any indexed subclass inherits.
func mroInboundEdges(lr *LoadedRepo, local string) []string {
	if lr == nil {
		return nil
	}
	return lr.getMROInbound()[local]
}

// defUseMRORetarget resolves an inherited-member def_use query to its in-repo
// DEFINING member (#3834). Given the (possibly repo-prefixed) entity_id filter,
// it locates the entity, runs the MRO walk, and — when the entity is an
// inherited stub resolving to an in-repo base member — returns that base
// member's LOCAL id plus the defining class name. Returns ("", "") when the
// entity is not found, is not an inherited stub, or resolves only externally
// (an external pack member has no in-repo def-use chains — honest dead-end).
func defUseMRORetarget(lg *LoadedGroup, entityFilter string) (definingID, definingClass string) {
	if lg == nil || entityFilter == "" {
		return "", ""
	}
	repoHint, local := splitPrefixed(entityFilter)
	tryRepo := func(r *LoadedRepo, id string) (string, string) {
		if r == nil || r.Doc == nil {
			return "", ""
		}
		e := r.LabelIndex.ByID(id)
		if e == nil {
			return "", ""
		}
		res := resolveMember(r, e)
		if res.Provenance == provInheritedInRepo && res.DefiningEntity != nil {
			return res.DefiningEntity.ID, res.DefiningClass
		}
		return "", ""
	}
	if repoHint != "" {
		if r, ok := lg.Repos[repoHint]; ok {
			return tryRepo(r, local)
		}
		return "", ""
	}
	// Unprefixed: try each repo by raw id.
	for _, r := range lg.Repos {
		if id, cls := tryRepo(r, entityFilter); id != "" {
			return id, cls
		}
	}
	return "", ""
}

// isExternalContractID reports whether id is a synthetic external-contract node
// id produced by this projection (so handlers can render it without a backing
// indexed entity).
func isExternalContractID(id string) bool {
	return len(id) >= len(externalContractIDPrefix) && id[:len(externalContractIDPrefix)] == externalContractIDPrefix
}
