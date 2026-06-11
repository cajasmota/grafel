package dashboard

// topology_compound.go — Compound (architecture-diagram) topology payload.
//
// Model 1 of the compound-topology epic (#4810 / #4811). This is the SHARED
// FOUNDATION reused by Models 2/3: a provider-/stack-agnostic compound graph
// of the indexed group, returned for a `group_by` lens.
//
//	GET /api/v2/topology/{group}/compound?group_by=infra|modules|tier
//
// The payload is three parallel slices the frontend renders as an
// AWS-architecture-diagram-style compound graph (nested containment zones +
// tier lanes + typed relationship edges):
//
//   - Zones — the containment hierarchy for the requested group_by:
//       infra   → cloud → vpc/cluster → service nesting reconstructed from IaC
//                 resource entities (module/path of cloud resources).
//       modules → package / namespace / monorepo-module nesting reconstructed
//                 from each code entity's source path.
//       tier    → no containment (one flat lane per tier facet).
//   - Nodes — every rendered entity, each carrying a `tier` facet
//       (client·edge·auth·compute·data·messaging·external) derived from the
//       entity kind + effects + IaC resource type.
//   - Edges — typed (reads/writes/invokes/consumes/routes/depends) with a
//       label and an aggregation key, so a COLLAPSED zone can emit ONE summary
//       edge that aggregates all of its members' cross-zone edges of that type.
//
// Names/zones are auto-derived (zero-config); light annotation is a later
// opt-in (Model 1 ships none). Every slice marshals as [] (never null).

import (
	"net/http"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// --- Wire shapes -----------------------------------------------------------

// compoundTier is the lane facet assigned to every node. The frontend lays
// these out left→right in this canonical order.
type compoundTier string

const (
	tierClient    compoundTier = "client"
	tierEdge      compoundTier = "edge"
	tierAuth      compoundTier = "auth"
	tierCompute   compoundTier = "compute"
	tierData      compoundTier = "data"
	tierMessaging compoundTier = "messaging"
	tierExternal  compoundTier = "external"
)

// compoundNode is one rendered entity in the compound graph.
type compoundNode struct {
	ID    string       `json:"id"`    // slug-prefixed entity id (edge join key)
	Label string       `json:"label"` // human-readable name
	Kind  string       `json:"kind"`  // scope-stripped entity kind
	Tier  compoundTier `json:"tier"`  // lane facet
	Repo  string       `json:"repo"`  // owning repo slug
	// ZonePath is the ordered containment chain (outermost → innermost) this
	// node belongs to under the requested group_by. Empty for tier mode (lanes
	// only). Each element is a stable zone id present in Zones.
	ZonePath []string `json:"zone_path"`
}

// compoundZone is one containment box. Zones form a forest via ParentID.
type compoundZone struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	ParentID string `json:"parent_id,omitempty"` // "" for a root zone
	// Kind tags the zone level so the renderer can style nesting depth, e.g.
	// "cloud" / "vpc" / "service" for infra, "module" for modules.
	Kind string `json:"kind"`
	// NodeCount is the number of direct + transitive member nodes — lets a
	// collapsed zone show a member count without the frontend re-walking.
	NodeCount int `json:"node_count"`
}

// compoundEdgeType is the typed-relationship taxonomy. Each maps a set of
// graph relationship kinds + cross-repo link kinds to one architecture verb.
type compoundEdgeType string

const (
	edgeReads    compoundEdgeType = "reads"
	edgeWrites   compoundEdgeType = "writes"
	edgeInvokes  compoundEdgeType = "invokes"
	edgeConsumes compoundEdgeType = "consumes"
	edgeRoutes   compoundEdgeType = "routes"
	edgeDepends  compoundEdgeType = "depends"
)

// compoundEdge is one typed edge between two rendered nodes.
type compoundEdge struct {
	Source string           `json:"source"`
	Target string           `json:"target"`
	Type   compoundEdgeType `json:"type"`
	Label  string           `json:"label"`
	// AggKey is the stable aggregation key (type + source + target). When a
	// zone collapses, the frontend re-keys edges by (collapsed-zone, other-end,
	// type) and emits ONE summary edge per key — see deriveSummaryEdges in the
	// frontend. We expose the per-edge AggKey so that re-keying is purely a
	// client-side fold over the same data (no second fetch).
	AggKey string `json:"agg_key"`
}

// compoundTopologyResponse is the full compound payload.
type compoundTopologyResponse struct {
	GroupBy string         `json:"group_by"`
	Zones   []compoundZone `json:"zones"`
	Nodes   []compoundNode `json:"nodes"`
	Edges   []compoundEdge `json:"edges"`
	// Tiers is the canonical lane order so the frontend never hard-codes it.
	Tiers []compoundTier `json:"tiers"`
}

// canonicalTiers is the fixed left→right lane order.
var canonicalTiers = []compoundTier{
	tierClient, tierEdge, tierAuth, tierCompute, tierData, tierMessaging, tierExternal,
}

// --- Handler ---------------------------------------------------------------

// handleV2TopologyCompound — GET /api/v2/topology/{group}/compound
func (s *Server) handleV2TopologyCompound(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	groupBy := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("group_by")))
	switch groupBy {
	case "infra", "modules", "tier":
	case "":
		groupBy = "modules"
	default:
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group_by must be infra|modules|tier")
		return
	}
	writeV2JSON(w, http.StatusOK, v2OK(collectCompoundTopology(grp, groupBy)))
}

// --- Builder ---------------------------------------------------------------

// renderableKind reports whether an entity kind is worth placing on the
// architecture diagram. We keep the architecturally-significant kinds (the
// nouns of a system) and drop the fine-grained code symbols (locals, params,
// imports, plain functions) that would turn the canvas into a hairball.
func renderableKind(stripped string) bool {
	switch stripped {
	case "HTTPEndpoint", "Endpoint", "Route", "Controller", "Service",
		"Component", "Datastore", "Table", "Collection", "Model",
		"MessageTopic", "Queue", "ChannelEvent", "Subscription",
		"ServerlessFunction", "InfraResource", "ExternalService",
		"AuthGuard", "Middleware", "Class", "Module":
		return true
	}
	return false
}

// nodeTier classifies an entity into a tier lane from its kind, effects, and
// (for IaC) its resource category. The taxonomy is provider-agnostic.
func nodeTier(stripped string, props map[string]string) compoundTier {
	// IaC resource category is the strongest signal when present.
	switch strings.ToLower(props["resource_category"]) {
	case "datastore", "cache", "storage":
		return tierData
	case "queue", "topic", "stream":
		return tierMessaging
	case "compute", "function":
		return tierCompute
	case "network":
		return tierEdge
	case "secret":
		return tierAuth
	}

	// Effects refine a few ambiguous code kinds. We only let an effect RECLASSIFY
	// a node that the kind switch would otherwise drop on the compute lane: a
	// plain Class/Module/Component whose sole effect is messaging reads as the
	// messaging lane (e.g. a thin producer/consumer wrapper). Store entities are
	// already caught by kind below, so db effects do NOT reclassify here.
	eff := strings.ToLower(props["effects"])
	switch stripped {
	case "Class", "Module", "Component":
		if strings.Contains(eff, "queue") || strings.Contains(eff, "topic") ||
			strings.Contains(eff, "publish") || strings.Contains(eff, "subscribe") {
			return tierMessaging
		}
	}

	switch stripped {
	case "HTTPEndpoint", "Endpoint", "Route", "Controller":
		return tierEdge
	case "AuthGuard", "Middleware":
		if stripped == "AuthGuard" {
			return tierAuth
		}
		return tierEdge
	case "Datastore", "Table", "Collection", "Model":
		return tierData
	case "MessageTopic", "Queue", "ChannelEvent", "Subscription":
		return tierMessaging
	case "ExternalService":
		return tierExternal
	case "ServerlessFunction", "Service", "Component", "Class", "Module":
		return tierCompute
	case "InfraResource":
		return tierCompute
	}
	// External / cross-repo targets default to external; everything else is
	// compute (the safe middle lane).
	if props["external"] == "true" || props["cross_repo"] == "true" {
		return tierExternal
	}
	return tierCompute
}

// edgeTypeForKind maps a relationship/link kind to a typed architecture verb.
// Returns ("", false) when the kind is not an architecture-significant edge.
func edgeTypeForKind(kind string) (compoundEdgeType, bool) {
	switch strings.ToUpper(kind) {
	case "READS_FROM", "DB_READ", "CACHE_READ", "QUERIES":
		return edgeReads, true
	case "WRITES_TO", "DB_WRITE", "CACHE_WRITE", "PERSISTS":
		return edgeWrites, true
	case "CALLS", "INVOKES", "HANDLES", "HTTP_CALL", "RPC_CALL", "REQUESTS":
		return edgeInvokes, true
	case "PUBLISHES_TO", "SUBSCRIBES_TO", "CONSUMES", "PRODUCES",
		"STREAMS_TO", "STREAMS_FROM", "TRIGGERS", "WS_EMITS", "WS_SUBSCRIBES_TO":
		return edgeConsumes, true
	case "ROUTES_TO", "ROUTES", "FORWARDS_TO", "PROXIES":
		return edgeRoutes, true
	case "DEPENDS_ON", "USES", "IMPORTS", "INSTANTIATES", "INJECTED_INTO",
		"REFERENCES":
		return edgeDepends, true
	}
	return "", false
}

// collectCompoundTopology builds the compound payload for the requested
// group_by lens. group_by ∈ {infra, modules, tier}.
func collectCompoundTopology(grp *DashGroup, groupBy string) compoundTopologyResponse {
	resp := compoundTopologyResponse{
		GroupBy: groupBy,
		Zones:   []compoundZone{},
		Nodes:   []compoundNode{},
		Edges:   []compoundEdge{},
		Tiers:   append([]compoundTier{}, canonicalTiers...),
	}

	// zoneByID dedups zones across repos; zoneOrder keeps first-seen order so
	// output is deterministic after a final sort.
	zoneByID := map[string]*compoundZone{}
	ensureZone := func(id, label, parentID, kind string) {
		if id == "" {
			return
		}
		if _, ok := zoneByID[id]; !ok {
			zoneByID[id] = &compoundZone{ID: id, Label: label, ParentID: parentID, Kind: kind}
		}
	}

	// renderedIDs is the set of slug-prefixed node ids we actually emit, so the
	// edge pass can drop edges whose endpoints aren't on the canvas.
	renderedIDs := map[string]struct{}{}
	// zonePathByNode lets the edge pass know cross-zone-ness if ever needed; we
	// keep it for completeness / Model-2 reuse.
	zonePathByNode := map[string][]string{}

	for _, r := range sortedRepos(grp) {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			stripped := dashStripScopePrefix(e.Kind)
			if !renderableKind(stripped) {
				continue
			}
			id := dashPrefixedID(r.Slug, e.ID)
			tier := nodeTier(stripped, e.Properties)
			zonePath := zonePathFor(groupBy, r.Slug, stripped, e, ensureZone)

			renderedIDs[id] = struct{}{}
			zonePathByNode[id] = zonePath
			resp.Nodes = append(resp.Nodes, compoundNode{
				ID:       id,
				Label:    entityLabel(e),
				Kind:     stripped,
				Tier:     tier,
				Repo:     r.Slug,
				ZonePath: zonePath,
			})
		}
	}

	// --- Edges: intra-repo relationships + cross-repo links. ---------------
	seenEdge := map[string]struct{}{}
	addEdge := func(src, tgt, kind string) {
		if src == "" || tgt == "" || src == tgt {
			return
		}
		if _, ok := renderedIDs[src]; !ok {
			return
		}
		if _, ok := renderedIDs[tgt]; !ok {
			return
		}
		et, ok := edgeTypeForKind(kind)
		if !ok {
			return
		}
		aggKey := string(et) + "\x00" + src + "\x00" + tgt
		if _, dup := seenEdge[aggKey]; dup {
			return
		}
		seenEdge[aggKey] = struct{}{}
		resp.Edges = append(resp.Edges, compoundEdge{
			Source: src,
			Target: tgt,
			Type:   et,
			Label:  string(et),
			AggKey: aggKey,
		})
	}

	for _, r := range sortedRepos(grp) {
		if r.Doc == nil {
			continue
		}
		for j := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[j]
			addEdge(dashPrefixedID(r.Slug, rel.FromID), dashPrefixedID(r.Slug, rel.ToID), rel.Kind)
		}
	}
	// Cross-repo links join already-prefixed ids (graphstate stamps them).
	for _, lnk := range grp.Links {
		addEdge(lnk.Source, lnk.Target, lnk.Kind)
	}

	// --- Finalize zone node counts (direct + transitive). ------------------
	// Increment every ancestor on each node's zone path.
	for _, n := range resp.Nodes {
		for _, zid := range n.ZonePath {
			if z := zoneByID[zid]; z != nil {
				z.NodeCount++
			}
		}
	}

	// Emit zones sorted by (parent depth via id) deterministically.
	zoneIDs := make([]string, 0, len(zoneByID))
	for id := range zoneByID {
		zoneIDs = append(zoneIDs, id)
	}
	sort.Strings(zoneIDs)
	for _, id := range zoneIDs {
		resp.Zones = append(resp.Zones, *zoneByID[id])
	}

	// Deterministic node + edge order.
	sort.SliceStable(resp.Nodes, func(i, j int) bool { return resp.Nodes[i].ID < resp.Nodes[j].ID })
	sort.SliceStable(resp.Edges, func(i, j int) bool { return resp.Edges[i].AggKey < resp.Edges[j].AggKey })

	return resp
}

// zonePathFor computes the containment chain (outermost → innermost) for a node
// under the requested group_by, creating any zones it crosses via ensureZone.
// Returns nil for tier mode (lanes only, no containment).
func zonePathFor(
	groupBy, slug, stripped string,
	e *graph.Entity,
	ensureZone func(id, label, parentID, kind string),
) []string {
	switch groupBy {
	case "tier":
		return nil

	case "infra":
		return infraZonePath(slug, stripped, e, ensureZone)

	default: // "modules"
		return moduleZonePath(slug, e, ensureZone)
	}
}

// moduleZonePath nests a node by its source-file path: repo → dir → subdir …
// (capped at a sane depth so deep trees stay readable). The repo slug is the
// outermost zone so multi-repo groups read as repo boxes.
func moduleZonePath(
	slug string,
	e *graph.Entity,
	ensureZone func(id, label, parentID, kind string),
) []string {
	repoZone := "repo:" + slug
	ensureZone(repoZone, slug, "", "repo")
	path := []string{repoZone}

	sf := strings.ReplaceAll(strings.TrimSpace(e.SourceFile), "\\", "/")
	sf = strings.TrimPrefix(sf, "./")
	if sf == "" {
		return path
	}
	// Directory segments only (drop the filename).
	dir := sf
	if i := strings.LastIndexByte(dir, '/'); i >= 0 {
		dir = dir[:i]
	} else {
		dir = ""
	}
	if dir == "" {
		return path
	}

	const maxDepth = 3 // repo + up to 3 dir levels keeps nesting legible.
	segs := strings.Split(dir, "/")
	if len(segs) > maxDepth {
		segs = segs[:maxDepth]
	}
	parent := repoZone
	acc := slug
	for _, seg := range segs {
		if seg == "" {
			continue
		}
		acc += "/" + seg
		zid := "mod:" + acc
		ensureZone(zid, seg, parent, "module")
		path = append(path, zid)
		parent = zid
	}
	return path
}

// infraZonePath nests an IaC resource by cloud → module → service. For
// non-IaC entities (code that participates in the infra view), we fall back to
// the repo zone so they still render inside a box. The "cloud" tier is derived
// from the resource provider/tool when present; otherwise a generic
// "infrastructure" root holds module-grouped resources.
func infraZonePath(
	slug, stripped string,
	e *graph.Entity,
	ensureZone func(id, label, parentID, kind string),
) []string {
	props := e.Properties

	// Determine whether this is an IaC resource (vs. code participating in the
	// infra lens). IaC resources carry a provider/tool/resource_category.
	isIaC := false
	switch stripped {
	case "InfraResource", "Datastore", "Queue", "ServerlessFunction", "Component":
		if props["resource_category"] != "" || props["resource_type"] != "" ||
			props["construct_type"] != "" || props["provider"] != "" || props["tool"] != "" {
			isIaC = true
		}
	}

	if !isIaC {
		repoZone := "repo:" + slug
		ensureZone(repoZone, slug, "", "repo")
		return []string{repoZone}
	}

	// Cloud root: provider, else tool, else generic.
	cloud := strings.ToLower(strings.TrimSpace(props["provider"]))
	if cloud == "" {
		cloud = strings.ToLower(strings.TrimSpace(props["tool"]))
	}
	if cloud == "" {
		cloud = "infrastructure"
	}
	cloudZone := "cloud:" + cloud
	ensureZone(cloudZone, cloud, "", "cloud")
	path := []string{cloudZone}

	// Mid-level containment: VPC / cluster / namespace if present, else module.
	mid := strings.TrimSpace(firstNonEmpty(
		props["vpc"], props["cluster"], props["namespace"], props["network"],
	))
	midKind := "network"
	if mid == "" {
		mid = strings.TrimSpace(firstNonEmpty(props["module"], iacModuleOf(slug, e.SourceFile)))
		midKind = "module"
	}
	parent := cloudZone
	if mid != "" {
		midZone := "infra:" + cloud + ":" + mid
		ensureZone(midZone, mid, parent, midKind)
		path = append(path, midZone)
		parent = midZone
	}

	// Innermost: service grouping when the resource declares one.
	if svc := strings.TrimSpace(firstNonEmpty(props["service"], props["stack"])); svc != "" {
		svcZone := parent + ":svc:" + svc
		ensureZone(svcZone, svc, parent, "service")
		path = append(path, svcZone)
	}
	return path
}
