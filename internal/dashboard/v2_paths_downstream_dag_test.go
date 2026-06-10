package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// makeDownstreamDAGFixture builds the gate fixture (#4349) modeling the real
// upvate shape:
//
//	http_endpoint_definition (GET /inspections)
//	  --IMPLEMENTS (reversed → handler-continuation)-->  Handler (controller)
//	    --CALLS--> Service
//	      --CALLS--> Repository
//	        --CALLS--> Pipeline
//	          --CALLS--> lookupA  --JOINS_COLLECTION--> Class:Inspection   (leaf)
//	          --CALLS--> lookupB  --JOINS_COLLECTION--> Class:Asset        (leaf)
//	          --CALLS--> facetCount --CALLS--> Result   (converge)
//	          --CALLS--> facetData  --CALLS--> Result   (converge — same node, two in-edges)
//	          --CALLS--> eq / gte / in   (aggregation.builder.ts noise → collapse in spine)
//	    --THROWS--> NotFoundError      (side-branch)
//	    --VALIDATES--> InspectionDTO   (side-branch)
//
// The handler is linked to the definition by `handler --IMPLEMENTS--> def`, so
// the builder's reversed-IMPLEMENTS continuation edge crosses the HTTP boundary.
func makeDownstreamDAGFixture() *DashGroup {
	ent := func(id, name, kind, file string) graph.Entity {
		return graph.Entity{ID: id, Name: name, Kind: kind, SourceFile: file, StartLine: 1}
	}
	epEnt := func(id, name, file, verb, path string) graph.Entity {
		return graph.Entity{ID: id, Name: name, Kind: "http_endpoint_definition",
			SourceFile: file, StartLine: 1,
			Properties: map[string]string{"verb": verb, "path": path}}
	}
	rel := func(from, to, kind string) graph.Relationship {
		return graph.Relationship{FromID: from, ToID: to, Kind: kind}
	}

	// entRich is like ent but carries the per-node enrichment data
	// (signature / subtype / docstring / effects) the flow cards surface.
	entRich := func(id, name, kind, file, sig, subtype, doc, effects string) graph.Entity {
		e := ent(id, name, kind, file)
		e.Signature = sig
		e.Subtype = subtype
		e.Properties = map[string]string{}
		if doc != "" {
			e.Properties["docstring"] = doc
		}
		if effects != "" {
			e.Properties["effects"] = effects
		}
		return e
	}

	entities := []graph.Entity{
		epEnt("ep", "GET /inspections", "routers.ts", "GET", "/inspections"),
		ent("handler", "InspectionController.list", "Operation", "inspection.controller.ts"),
		ent("service", "InspectionService.list", "Operation", "inspection.service.ts"),
		entRich("repo", "InspectionRepository.find", "Operation", "inspection.repository.ts",
			"find(filter: Filter, opts: FindOpts): Promise<Inspection[]>", "DataAccess",
			"Finds inspections matching the filter.\nSecond line ignored.", "db_read,db_write"),
		ent("pipeline", "buildPipeline", "Operation", "inspection.repository.ts"),
		ent("lookupA", "lookupInspection", "Operation", "inspection.repository.ts"),
		ent("lookupB", "lookupAsset", "Operation", "inspection.repository.ts"),
		ent("facetCount", "facetCount", "Operation", "inspection.repository.ts"),
		ent("facetData", "facetData", "Operation", "inspection.repository.ts"),
		ent("result", "shapeResult", "Operation", "inspection.repository.ts"),
		// builder noise (aggregation.builder.ts)
		ent("eq", "eq", "Operation", "aggregation.builder.ts"),
		ent("gte", "gte", "Operation", "where.builder.ts"),
		ent("in", "in", "Operation", "where.builder.ts"),
		// semantic-side targets
		ent("err", "NotFoundError", "Class", "errors.ts"),
		ent("dto", "InspectionDTO", "Class", "dto.ts"),
		// joined collections (terminal leaves)
		ent("colInspection", "Inspection", "Collection", "inspection.model.ts"),
		ent("colAsset", "Asset", "Collection", "asset.model.ts"),
	}

	rels := []graph.Relationship{
		// HTTP boundary: handler --IMPLEMENTS--> endpoint def.
		rel("handler", "ep", "IMPLEMENTS"),
		// spine
		rel("handler", "service", "CALLS"),
		rel("service", "repo", "CALLS"),
		rel("repo", "pipeline", "CALLS"),
		rel("pipeline", "lookupA", "CALLS"),
		rel("pipeline", "lookupB", "CALLS"),
		rel("pipeline", "facetCount", "CALLS"),
		rel("pipeline", "facetData", "CALLS"),
		// $facet convergence: both halves call the SAME result shaper.
		rel("facetCount", "result", "CALLS"),
		rel("facetData", "result", "CALLS"),
		// builder noise hanging off the pipeline.
		rel("pipeline", "eq", "CALLS"),
		rel("pipeline", "gte", "CALLS"),
		rel("pipeline", "in", "CALLS"),
		// $lookup → joined collection (terminal).
		rel("lookupA", "colInspection", "JOINS_COLLECTION"),
		rel("lookupB", "colAsset", "JOINS_COLLECTION"),
		// handler side-branches.
		rel("handler", "err", "THROWS"),
		rel("handler", "dto", "VALIDATES"),
	}

	doc := &graph.Document{Repo: "api", Entities: entities, Relationships: rels}
	return &DashGroup{
		Name:  "testgrp",
		Repos: map[string]*DashRepo{"api": {Slug: "api", Path: "/tmp/api", Doc: doc}},
	}
}

// fetchDAG issues the downstream-dag request with the given query and decodes
// the response payload.
func fetchDAG(t *testing.T, ts *httptest.Server, query string) v2DownstreamDAGResponse {
	t.Helper()
	pathHash := hashStr("/inspections")
	url := ts.URL + "/api/v2/groups/testgrp/paths/" + pathHash + "/downstream-dag"
	if query != "" {
		url += "?" + query
	}
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET downstream-dag: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body struct {
		OK   bool                    `json:"ok"`
		Data v2DownstreamDAGResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Fatalf("ok: want true")
	}
	return body.Data
}

// nodeByName finds a node by display name (test convenience).
func nodeByName(nodes []v2DAGNode, name string) *v2DAGNode {
	for i := range nodes {
		if nodes[i].Name == name {
			return &nodes[i]
		}
	}
	return nil
}

// inEdges counts edges whose `to` is the given prefixed id.
func inEdges(edges []v2DAGEdge, to string) []v2DAGEdge {
	var out []v2DAGEdge
	for _, e := range edges {
		if e.To == to {
			out = append(out, e)
		}
	}
	return out
}

// TestDownstreamDAG_SpineShape asserts the spine-mode DAG: rooted at the
// endpoint, crosses the HTTP boundary to the handler, reaches the two distinct
// collection leaves, dedupes the $facet convergence to one Result node with two
// in-edges, collapses builder noise, and carries the THROWS/VALIDATES branches.
func TestDownstreamDAG_SpineShape(t *testing.T) {
	ts := newPathsTestServer(t, makeDownstreamDAGFixture())
	defer ts.Close()

	dag := fetchDAG(t, ts, "") // default: spine, depth 8, semantic on.

	if dag.Mode != "spine" {
		t.Errorf("mode: want spine, got %q", dag.Mode)
	}
	if dag.RootID != "api::ep" {
		t.Errorf("root_id: want api::ep, got %q", dag.RootID)
	}

	// Root is the endpoint and is first in deterministic order.
	if len(dag.Nodes) == 0 || dag.Nodes[0].ID != "api::ep" {
		t.Fatalf("first node must be the endpoint root; got %+v", dag.Nodes)
	}
	if dag.Nodes[0].Role != "endpoint" {
		t.Errorf("root role: want endpoint, got %q", dag.Nodes[0].Role)
	}

	// Handler reached via the reversed-IMPLEMENTS continuation edge.
	handler := nodeByName(dag.Nodes, "InspectionController.list")
	if handler == nil {
		t.Fatal("handler node missing — HTTP boundary not crossed")
	}
	if handler.Role != "handler" {
		t.Errorf("handler role: want handler, got %q", handler.Role)
	}
	contEdges := inEdges(dag.Edges, handler.ID)
	if len(contEdges) != 1 || contEdges[0].Kind != handlerContEdgeKind {
		t.Errorf("handler in-edge: want one %s edge, got %+v", handlerContEdgeKind, contEdges)
	}

	// Two distinct collection leaves.
	inspCol := nodeByName(dag.Nodes, "Inspection")
	assetCol := nodeByName(dag.Nodes, "Asset")
	if inspCol == nil || assetCol == nil {
		t.Fatalf("both collection leaves must be present; nodes=%v", nodeNames(dag.Nodes))
	}
	if inspCol.ID == assetCol.ID {
		t.Error("collection leaves must be distinct nodes")
	}
	if !inspCol.Terminal || !assetCol.Terminal {
		t.Error("joined collections must be terminal leaves")
	}
	if inspCol.Role != "collection" {
		t.Errorf("collection role: want collection, got %q", inspCol.Role)
	}

	// $facet convergence: ONE Result node with TWO in-edges (from facetCount
	// and facetData) — a real merge, not duplicated subtrees.
	resultNodes := 0
	for _, n := range dag.Nodes {
		if n.Name == "shapeResult" {
			resultNodes++
		}
	}
	if resultNodes != 1 {
		t.Errorf("convergence: Result must appear once, got %d", resultNodes)
	}
	result := nodeByName(dag.Nodes, "shapeResult")
	if got := len(inEdges(dag.Edges, result.ID)); got != 2 {
		t.Errorf("convergence: Result must have 2 in-edges, got %d", got)
	}

	// Builder noise collapsed into the pipeline node, NOT promoted to DAG nodes.
	if n := nodeByName(dag.Nodes, "eq"); n != nil {
		t.Error("builder method eq must be collapsed in spine mode, not a DAG node")
	}
	pipeline := nodeByName(dag.Nodes, "buildPipeline")
	if pipeline == nil {
		t.Fatal("pipeline node missing")
	}
	collapsed := map[string]bool{}
	for _, c := range pipeline.CollapsedChildren {
		collapsed[c.Name] = true
	}
	for _, want := range []string{"eq", "gte", "in"} {
		if !collapsed[want] {
			t.Errorf("builder %q must be in pipeline.collapsed_children; got %v", want, collapsed)
		}
	}

	// THROWS / VALIDATES side-branches present off the handler.
	if e := nodeByName(dag.Nodes, "NotFoundError"); e == nil {
		t.Error("THROWS side-branch (NotFoundError) missing")
	} else if got := inEdges(dag.Edges, e.ID); len(got) != 1 || got[0].Kind != "THROWS" {
		t.Errorf("NotFoundError in-edge: want THROWS, got %+v", got)
	}
	if d := nodeByName(dag.Nodes, "InspectionDTO"); d == nil {
		t.Error("VALIDATES side-branch (InspectionDTO) missing")
	} else if got := inEdges(dag.Edges, d.ID); len(got) != 1 || got[0].Kind != "VALIDATES" {
		t.Errorf("InspectionDTO in-edge: want VALIDATES, got %+v", got)
	}

	// At least the pipeline fan-out is a branch point.
	if dag.BranchCount < 1 {
		t.Errorf("branch_count: want >=1, got %d", dag.BranchCount)
	}
	if dag.Truncation.DepthTruncated || dag.Truncation.FanoutTruncated || dag.Truncation.NodeTruncated {
		t.Errorf("no truncation expected at default depth; got %+v", dag.Truncation)
	}
}

// TestDownstreamDAG_FullModeExpandsBuilders asserts full mode promotes the
// builder methods to real DAG nodes (no collapse).
func TestDownstreamDAG_FullModeExpandsBuilders(t *testing.T) {
	ts := newPathsTestServer(t, makeDownstreamDAGFixture())
	defer ts.Close()

	dag := fetchDAG(t, ts, "mode=full")
	if dag.Mode != "full" {
		t.Fatalf("mode: want full, got %q", dag.Mode)
	}
	for _, want := range []string{"eq", "gte", "in"} {
		n := nodeByName(dag.Nodes, want)
		if n == nil {
			t.Errorf("full mode: builder %q must be a DAG node", want)
			continue
		}
		if len(n.CollapsedChildren) != 0 {
			t.Errorf("full mode: %q must have no collapsed_children", want)
		}
	}
	// Builder nodes must have an in-edge from the pipeline.
	pipeline := nodeByName(dag.Nodes, "buildPipeline")
	eq := nodeByName(dag.Nodes, "eq")
	if pipeline == nil || eq == nil {
		t.Fatal("pipeline/eq nodes missing in full mode")
	}
	if got := inEdges(dag.Edges, eq.ID); len(got) != 1 || got[0].From != pipeline.ID {
		t.Errorf("eq in-edge: want CALLS from pipeline, got %+v", got)
	}
}

// TestDownstreamDAG_DepthCap asserts the depth cap truncates and flags it.
func TestDownstreamDAG_DepthCap(t *testing.T) {
	ts := newPathsTestServer(t, makeDownstreamDAGFixture())
	defer ts.Close()

	// depth=2: endpoint(0) → handler(1) → service(2). Service's CALLS to repo
	// is past the cap, so the deep pipeline/collections never appear.
	dag := fetchDAG(t, ts, "depth=2")
	if dag.Depth != 2 {
		t.Fatalf("depth: want 2, got %d", dag.Depth)
	}
	if !dag.Truncation.DepthTruncated {
		t.Error("depth_truncated must be set when the depth cap drops children")
	}
	if nodeByName(dag.Nodes, "buildPipeline") != nil {
		t.Error("pipeline must be beyond depth=2 and absent")
	}
	if nodeByName(dag.Nodes, "Inspection") != nil {
		t.Error("collection leaf must be beyond depth=2 and absent")
	}
	// Handler + service ARE within depth 2.
	if nodeByName(dag.Nodes, "InspectionController.list") == nil ||
		nodeByName(dag.Nodes, "InspectionService.list") == nil {
		t.Error("handler+service must be within depth=2")
	}
}

// TestDownstreamDAG_SemanticToggle asserts semantic=0 drops the
// JOINS_COLLECTION / THROWS / VALIDATES edges (CALLS spine only).
func TestDownstreamDAG_SemanticToggle(t *testing.T) {
	ts := newPathsTestServer(t, makeDownstreamDAGFixture())
	defer ts.Close()

	dag := fetchDAG(t, ts, "semantic=0")
	if nodeByName(dag.Nodes, "Inspection") != nil || nodeByName(dag.Nodes, "Asset") != nil {
		t.Error("semantic=0 must drop JOINS_COLLECTION collection leaves")
	}
	if nodeByName(dag.Nodes, "NotFoundError") != nil {
		t.Error("semantic=0 must drop THROWS side-branch")
	}
	if nodeByName(dag.Nodes, "InspectionDTO") != nil {
		t.Error("semantic=0 must drop VALIDATES side-branch")
	}
	// CALLS spine is still intact.
	if nodeByName(dag.Nodes, "buildPipeline") == nil {
		t.Error("CALLS spine must survive semantic=0")
	}
	for _, e := range dag.Edges {
		if e.Kind == "JOINS_COLLECTION" || e.Kind == "THROWS" || e.Kind == "VALIDATES" {
			t.Errorf("semantic=0 leaked a semantic edge: %+v", e)
		}
	}
}

// TestDownstreamDAG_Deterministic asserts two identical requests return
// byte-identical node + edge ordering.
func TestDownstreamDAG_Deterministic(t *testing.T) {
	ts := newPathsTestServer(t, makeDownstreamDAGFixture())
	defer ts.Close()

	a := fetchDAG(t, ts, "")
	b := fetchDAG(t, ts, "")
	if len(a.Nodes) != len(b.Nodes) || len(a.Edges) != len(b.Edges) {
		t.Fatalf("non-deterministic sizes: a=(%d,%d) b=(%d,%d)",
			len(a.Nodes), len(a.Edges), len(b.Nodes), len(b.Edges))
	}
	for i := range a.Nodes {
		if a.Nodes[i].ID != b.Nodes[i].ID {
			t.Errorf("node order differs at %d: %q vs %q", i, a.Nodes[i].ID, b.Nodes[i].ID)
		}
	}
	for i := range a.Edges {
		if a.Edges[i] != b.Edges[i] {
			t.Errorf("edge order differs at %d: %+v vs %+v", i, a.Edges[i], b.Edges[i])
		}
	}
}

// TestDownstreamDAG_VerbDisambiguation asserts ?verb picks the right endpoint
// when a path has multiple verbs.
func TestDownstreamDAG_VerbDisambiguation(t *testing.T) {
	grp := makeDownstreamDAGFixture()
	// Add a POST endpoint for the same path with its own handler.
	doc := grp.Repos["api"].Doc
	doc.Entities = append(doc.Entities,
		graph.Entity{ID: "epPost", Name: "POST /inspections", Kind: "http_endpoint_definition",
			SourceFile: "routers.ts", StartLine: 2, Properties: map[string]string{"verb": "POST", "path": "/inspections"}},
		graph.Entity{ID: "handlerPost", Name: "InspectionController.create", Kind: "Operation",
			SourceFile: "inspection.controller.ts", StartLine: 10},
	)
	doc.Relationships = append(doc.Relationships,
		graph.Relationship{FromID: "handlerPost", ToID: "epPost", Kind: "IMPLEMENTS"})

	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	dag := fetchDAG(t, ts, "verb=POST")
	if dag.Verb != "POST" {
		t.Errorf("verb: want POST, got %q", dag.Verb)
	}
	if dag.RootID != "api::epPost" {
		t.Errorf("root_id: want api::epPost, got %q", dag.RootID)
	}
	if nodeByName(dag.Nodes, "InspectionController.create") == nil {
		t.Error("POST handler must be the crossed handler")
	}
}

// TestDownstreamDAG_NodeEnrichment asserts the per-node flow-card fields
// (#4348/#4350) populate from the resolved entity when present (signature,
// subtype, doc, effects on the repository node; collection on the leaves) and
// are OMITTED — not null-spammed — on a bare node that carries none of them.
func TestDownstreamDAG_NodeEnrichment(t *testing.T) {
	ts := newPathsTestServer(t, makeDownstreamDAGFixture())
	defer ts.Close()

	dag := fetchDAG(t, ts, "")

	// Enriched repository node: signature + subtype + doc + effects all present.
	repo := nodeByName(dag.Nodes, "InspectionRepository.find")
	if repo == nil {
		t.Fatal("repository node missing")
	}
	if repo.Signature != "find(filter: Filter, opts: FindOpts): Promise<Inspection[]>" {
		t.Errorf("signature: got %q", repo.Signature)
	}
	if repo.Subtype != "DataAccess" {
		t.Errorf("subtype: want DataAccess, got %q", repo.Subtype)
	}
	// Doc is the first line only, whitespace-collapsed.
	if repo.Doc != "Finds inspections matching the filter." {
		t.Errorf("doc: got %q", repo.Doc)
	}
	if len(repo.Effects) != 2 || repo.Effects[0] != "db_read" || repo.Effects[1] != "db_write" {
		t.Errorf("effects: want [db_read db_write], got %v", repo.Effects)
	}

	// Bare node (no signature/subtype/doc/effects): every enrichment field omitted.
	bare := nodeByName(dag.Nodes, "InspectionService.list")
	if bare == nil {
		t.Fatal("service node missing")
	}
	if bare.Signature != "" || bare.Subtype != "" || bare.Doc != "" ||
		len(bare.Effects) != 0 || bare.Collection != "" {
		t.Errorf("bare node must omit all enrichment fields, got %+v", bare)
	}

	// Collection-terminal node carries its collection/table name.
	col := nodeByName(dag.Nodes, "Inspection")
	if col == nil {
		t.Fatal("collection leaf missing")
	}
	if col.Collection != "Inspection" {
		t.Errorf("collection: want Inspection, got %q", col.Collection)
	}

	// Enrichment fields must not appear as null/empty keys in the wire JSON of a
	// bare node (omitempty honored). Round-trip the bare node and assert keys absent.
	raw, _ := json.Marshal(bare)
	for _, k := range []string{`"signature"`, `"subtype"`, `"doc"`, `"effects"`, `"collection"`} {
		if strings.Contains(string(raw), k) {
			t.Errorf("bare node JSON must omit %s; got %s", k, raw)
		}
	}
}

// makeExternalDAGFixture extends the base shape with three call targets off the
// service so we can assert external classification (#4598):
//
//	service --CALLS--> Repository.save                 (in-repo: NOT external)
//	service --CALLS--> ext:typeorm  (Kind=External)    (resolved external placeholder)
//	service --CALLS--> getRepository  (unstamped, props source_module=typeorm)
//	                                                   (unresolved external → recovered name + pkg)
//	service --CALLS--> save  (unstamped, name matches in-repo Repository.save's leaf? no)
//
// The in-repo "Repository.save" is a real entity; an unstamped CALLS to a name
// that matches an in-repo definition must NOT be flagged external (resolution
// gap, surfaced not mislabeled).
func makeExternalDAGFixture() *DashGroup {
	grp := makeDownstreamDAGFixture()
	doc := grp.Repos["api"].Doc

	doc.Entities = append(doc.Entities,
		// Resolved external placeholder (as internal/external.Synthesize emits).
		graph.Entity{ID: "ext:typeorm", Name: "typeorm", QualifiedName: "typeorm",
			Kind: "SCOPE.External", Subtype: "package",
			Metadata: map[string]interface{}{"is_external": true}},
		// In-repo definition whose leaf name a later unstamped call will match.
		graph.Entity{ID: "saveImpl", Name: "persistThing", Kind: "Operation",
			SourceFile: "thing.repository.ts", StartLine: 3},
	)
	doc.Relationships = append(doc.Relationships,
		// in-repo CALLS — must NOT be external.
		graph.Relationship{FromID: "service", ToID: "saveImpl", Kind: "CALLS"},
		// resolved external placeholder CALLS — external=true, package=typeorm.
		graph.Relationship{FromID: "service", ToID: "ext:typeorm", Kind: "CALLS"},
		// unstamped external CALLS carrying the package on the edge — external,
		// name recovered from the id leaf, package from source_module.
		graph.Relationship{FromID: "service", ToID: "createConnection", Kind: "CALLS",
			Properties: map[string]string{"source_module": "typeorm"}},
		// unstamped CALLS whose name matches an in-repo definition — false
		// external: must stay non-external (resolution gap).
		graph.Relationship{FromID: "service", ToID: "persistThing", Kind: "CALLS"},
	)
	return grp
}

// TestDownstreamDAG_ExternalClassification asserts #4598: external library
// calls are flagged external=true with a package; in-repo calls are NOT; an
// unstamped callee whose name matches an in-repo definition is a resolution
// gap, not external.
func TestDownstreamDAG_ExternalClassification(t *testing.T) {
	ts := newPathsTestServer(t, makeExternalDAGFixture())
	defer ts.Close()

	dag := fetchDAG(t, ts, "depth=24")

	// Resolved external placeholder: external + package.
	ext := nodeByName(dag.Nodes, "typeorm")
	if ext == nil {
		t.Fatalf("external placeholder node missing; nodes=%v", nodeNames(dag.Nodes))
	}
	if !ext.External {
		t.Error("resolved external placeholder must be external=true")
	}
	if ext.Package != "typeorm" {
		t.Errorf("external package: want typeorm, got %q", ext.Package)
	}

	// Unstamped external callee with package on the edge: external + recovered
	// name + package.
	conn := nodeByName(dag.Nodes, "createConnection")
	if conn == nil {
		t.Fatalf("unstamped external callee missing; nodes=%v", nodeNames(dag.Nodes))
	}
	if !conn.External {
		t.Error("unstamped external callee must be external=true")
	}
	if conn.Package != "typeorm" {
		t.Errorf("unstamped external package: want typeorm, got %q", conn.Package)
	}
	if conn.Kind != "external" {
		t.Errorf("unstamped external kind: want external, got %q", conn.Kind)
	}

	// In-repo call: must NOT be external and must have no package.
	repo := nodeByName(dag.Nodes, "persistThing")
	if repo == nil {
		t.Fatalf("in-repo callee missing; nodes=%v", nodeNames(dag.Nodes))
	}
	if repo.External {
		t.Error("in-repo call must NOT be external (false-external)")
	}
	if repo.Package != "" {
		t.Errorf("in-repo call must have no package, got %q", repo.Package)
	}

	// The in-repo InspectionRepository.find node (resolved entity) is not external.
	find := nodeByName(dag.Nodes, "InspectionRepository.find")
	if find == nil {
		t.Fatal("repository node missing")
	}
	if find.External {
		t.Error("resolved in-repo entity must NOT be external")
	}

	// Resolution-gap node is surfaced honestly as "unresolved", not "external".
	if repo.Kind != "unresolved" {
		t.Errorf("false-external node kind: want unresolved, got %q", repo.Kind)
	}

	// Wire JSON of the in-repo node must omit the external/package keys
	// (omitempty) — match on the key token with its colon to avoid colliding
	// with an "external" value elsewhere.
	raw, _ := json.Marshal(repo)
	for _, k := range []string{`"external":`, `"package":`} {
		if strings.Contains(string(raw), k) {
			t.Errorf("in-repo node JSON must omit %s; got %s", k, raw)
		}
	}
}

// TestPackageRoot asserts the package-root reduction across the common module
// path shapes the UI displays.
func TestPackageRoot(t *testing.T) {
	cases := map[string]string{
		"typeorm":            "typeorm",
		"@nestjs/common":     "@nestjs/common",
		"@nestjs/common/foo": "@nestjs/common",
		"node:fs":            "node:fs",
		"django.db.models":   "django",
		"lodash/fp":          "lodash",
		"typeorm:getRepo":    "typeorm",
		"":                   "",
	}
	for in, want := range cases {
		if got := packageRoot(in); got != want {
			t.Errorf("packageRoot(%q): want %q, got %q", in, want, got)
		}
	}
}

func nodeNames(nodes []v2DAGNode) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}
