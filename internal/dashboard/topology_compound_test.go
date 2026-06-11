package dashboard

// topology_compound_test.go — tests for the Model-1 compound topology payload
// (#4810/#4811): zones per group_by, node tiers, typed/aggregatable edges, and
// the collapsed-zone summary-edge fold.

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// compoundFixture builds a two-repo group with code (endpoint→service→datastore
// + a queue) so every tier and several edge types are exercised, plus an IaC
// resource carrying provider/module/category so the infra lens nests it.
func compoundFixture() *DashGroup {
	api := &graph.Document{
		Entities: []graph.Entity{
			{ID: "ep1", Name: "GET /orders", Kind: "SCOPE.HTTPEndpoint", SourceFile: "api/handlers/orders.go"},
			{ID: "svc1", Name: "OrderService", Kind: "SCOPE.Service", SourceFile: "api/services/order.go"},
			{ID: "db1", Name: "orders", Kind: "SCOPE.Datastore", SourceFile: "api/db/schema.go",
				Properties: map[string]string{"resource_category": "datastore"}},
			{ID: "q1", Name: "order.created", Kind: "SCOPE.MessageTopic", SourceFile: "api/events/topics.go"},
			{ID: "guard1", Name: "AuthGuard", Kind: "SCOPE.AuthGuard", SourceFile: "api/auth/guard.go"},
			// An IaC resource for the infra lens.
			{ID: "ddb", Name: "orders-table", Kind: "SCOPE.InfraResource", SourceFile: "infra/main.tf",
				Properties: map[string]string{
					"provider": "aws", "resource_category": "datastore",
					"module": "data", "service": "orders",
				}},
		},
		Relationships: []graph.Relationship{
			{FromID: "ep1", ToID: "svc1", Kind: "CALLS"},
			{FromID: "svc1", ToID: "db1", Kind: "WRITES_TO"},
			{FromID: "svc1", ToID: "db1", Kind: "READS_FROM"},
			{FromID: "svc1", ToID: "q1", Kind: "PUBLISHES_TO"},
			{FromID: "ep1", ToID: "guard1", Kind: "USES"},
		},
	}
	worker := &graph.Document{
		Entities: []graph.Entity{
			{ID: "w1", Name: "OrderWorker", Kind: "SCOPE.Service", SourceFile: "worker/consumer.go"},
		},
		Relationships: []graph.Relationship{},
	}
	return &DashGroup{
		Name: "g",
		Repos: map[string]*DashRepo{
			"api":    {Slug: "api", Doc: api},
			"worker": {Slug: "worker", Doc: worker},
		},
		// Cross-repo: the worker consumes the api's order.created topic.
		Links: []CrossRepoLink{
			{Source: "worker::w1", Target: "api::q1", Kind: "SUBSCRIBES_TO"},
		},
	}
}

func compoundNodeByID(resp compoundTopologyResponse, id string) *compoundNode {
	for i := range resp.Nodes {
		if resp.Nodes[i].ID == id {
			return &resp.Nodes[i]
		}
	}
	return nil
}

func TestCompoundTopology_Tiers(t *testing.T) {
	resp := collectCompoundTopology(compoundFixture(), "tier")

	if len(resp.Tiers) != 7 {
		t.Fatalf("expected 7 canonical tiers, got %d", len(resp.Tiers))
	}
	// tier mode emits NO containment zones.
	if len(resp.Zones) != 0 {
		t.Errorf("tier mode should have 0 zones, got %d", len(resp.Zones))
	}

	want := map[string]compoundTier{
		"api::ep1":    tierEdge,      // endpoint
		"api::svc1":   tierCompute,   // service
		"api::db1":    tierData,      // datastore
		"api::q1":     tierMessaging, // topic
		"api::guard1": tierAuth,      // auth guard
		"api::ddb":    tierData,      // IaC datastore resource
	}
	for id, tier := range want {
		n := compoundNodeByID(resp, id)
		if n == nil {
			t.Fatalf("node %s not rendered", id)
		}
		if n.Tier != tier {
			t.Errorf("node %s: tier = %q, want %q", id, n.Tier, tier)
		}
	}
}

func TestCompoundTopology_ModuleZones(t *testing.T) {
	resp := collectCompoundTopology(compoundFixture(), "modules")

	if len(resp.Zones) == 0 {
		t.Fatal("modules mode must produce containment zones")
	}
	// Repo zones are roots.
	var repoRoots int
	for _, z := range resp.Zones {
		if z.Kind == "repo" && z.ParentID == "" {
			repoRoots++
		}
	}
	if repoRoots != 2 {
		t.Errorf("expected 2 repo root zones, got %d", repoRoots)
	}

	// svc1 lives under api → api/services. Its zone path must start at the repo.
	n := compoundNodeByID(resp, "api::svc1")
	if n == nil || len(n.ZonePath) < 2 {
		t.Fatalf("api::svc1 zone path too shallow: %+v", n)
	}
	if n.ZonePath[0] != "repo:api" {
		t.Errorf("outermost zone = %q, want repo:api", n.ZonePath[0])
	}

	// Node counts roll up to ancestors.
	for _, z := range resp.Zones {
		if z.ID == "repo:api" && z.NodeCount < 5 {
			t.Errorf("repo:api node_count = %d, want >= 5", z.NodeCount)
		}
	}
}

func TestCompoundTopology_InfraZones(t *testing.T) {
	resp := collectCompoundTopology(compoundFixture(), "infra")

	// The IaC resource nests cloud(aws) → module(data) → service(orders).
	n := compoundNodeByID(resp, "api::ddb")
	if n == nil {
		t.Fatal("IaC resource api::ddb not rendered")
	}
	if len(n.ZonePath) < 2 || n.ZonePath[0] != "cloud:aws" {
		t.Fatalf("infra zone path = %v, want to start at cloud:aws", n.ZonePath)
	}
	var sawService bool
	for _, zid := range n.ZonePath {
		for _, z := range resp.Zones {
			if z.ID == zid && z.Kind == "service" && z.Label == "orders" {
				sawService = true
			}
		}
	}
	if !sawService {
		t.Errorf("expected an innermost service zone 'orders' on path %v", n.ZonePath)
	}
}

func TestCompoundTopology_TypedEdges(t *testing.T) {
	resp := collectCompoundTopology(compoundFixture(), "tier")

	got := map[string]compoundEdgeType{}
	for _, e := range resp.Edges {
		got[e.Source+"->"+e.Target] = e.Type
	}
	checks := map[string]compoundEdgeType{
		"api::ep1->api::svc1":    edgeInvokes,  // CALLS
		"api::svc1->api::db1":    edgeWrites,   // WRITES_TO (reads also present)
		"api::svc1->api::q1":     edgeConsumes, // PUBLISHES_TO
		"api::ep1->api::guard1":  edgeDepends,  // USES
		"worker::w1->api::q1":    edgeConsumes, // cross-repo SUBSCRIBES_TO
	}
	for pair, typ := range checks {
		if got[pair] != typ {
			t.Errorf("edge %s: type = %q, want %q", pair, got[pair], typ)
		}
	}

	// Every edge carries a stable aggregation key.
	for _, e := range resp.Edges {
		if e.AggKey == "" || !strings.Contains(e.AggKey, string(e.Type)) {
			t.Errorf("edge %s->%s missing/bad agg_key %q", e.Source, e.Target, e.AggKey)
		}
	}
}

// TestCompoundTopology_CollapsedSummaryEdges verifies the server-side data is
// sufficient for the client fold: collapsing a zone (here repo:api) and
// re-keying its members' cross-zone edges by (collapsed-zone, other-end, type)
// must cover every cross-zone edge of those members. We emulate that fold here.
func TestCompoundTopology_CollapsedSummaryEdges(t *testing.T) {
	resp := collectCompoundTopology(compoundFixture(), "modules")

	// Members of repo:api = all api:: nodes.
	member := map[string]bool{}
	for _, n := range resp.Nodes {
		if n.Repo == "api" {
			member[n.ID] = true
		}
	}

	// Fold: any edge crossing the api boundary becomes a summary edge keyed by
	// (api-zone, other-end, type).
	summary := map[string]int{}
	var crossing int
	for _, e := range resp.Edges {
		si, ti := member[e.Source], member[e.Target]
		if si == ti {
			continue // wholly inside or wholly outside → not a boundary edge.
		}
		crossing++
		var other string
		if si {
			other = e.Target
		} else {
			other = e.Source
		}
		summary["api\x00"+other+"\x00"+string(e.Type)] += 1
	}

	if crossing == 0 {
		t.Fatal("expected at least one cross-zone edge for repo:api (the worker→topic link)")
	}
	if len(summary) == 0 {
		t.Fatal("collapsed zone produced no summary edges")
	}
	// The worker→api::q1 consumes edge must be represented in the summary.
	if summary["api\x00worker::w1\x00consumes"] == 0 {
		t.Errorf("summary edges %v missing the cross-repo consumes edge", summary)
	}
}
