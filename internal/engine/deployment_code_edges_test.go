// Tests for the explicit infra↔code DEPLOYMENT edges pass — #4983 (epic #4810).
//
// Value-asserting: every positive test asserts a SPECIFIC DEPLOYS edge between
// two concrete, canonically-keyed nodes (a named infra resource → a code
// `service:<name>` node) — never `len > 0`. Negative tests assert the absence of
// any DEPLOYS edge for off-the-shelf / templated / non-manifest inputs.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func runDeployCodeDetect(t *testing.T, lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	res := applyDeploymentCodeEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	return res.Entities, res.Relationships
}

func deployCodeHasEdge(rels []types.RelationshipRecord, fromID, toID string) bool {
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindDeploys) && r.FromID == fromID && r.ToID == toID {
			return true
		}
	}
	return false
}

func deployCodeAnyEdge(rels []types.RelationshipRecord) bool {
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindDeploys) {
			return true
		}
	}
	return false
}

func deployCodeEdge(rels []types.RelationshipRecord, fromID, toID string) *types.RelationshipRecord {
	for i := range rels {
		if rels[i].Kind == string(types.RelationshipKindDeploys) && rels[i].FromID == fromID && rels[i].ToID == toID {
			return &rels[i]
		}
	}
	return nil
}

// TestK8sWorkload_FirstPartyImage_Deploys is the headline assertion: a
// Deployment running a first-party image yields a DEPLOYS edge from the workload
// resource node to the canonical code service node keyed by the image repo.
func TestK8sWorkload_FirstPartyImage_Deploys(t *testing.T) {
	src := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: prod
spec:
  template:
    spec:
      containers:
        - name: api
          image: myorg/api-service:1.4.2
`
	ents, rels := runDeployCodeDetect(t, "yaml", "k8s/api.yaml", src)
	from := "k8s/k8s/api.yaml#resource/Deployment/api"
	to := "service:api-service"
	e := deployCodeEdge(rels, from, to)
	if e == nil {
		t.Fatalf("expected DEPLOYS %s -> %s; rels=%v", from, to, rels)
	}
	if e.Properties["inferred"] != "true" || e.Properties["match"] != "image_repo" {
		t.Fatalf("expected inferred=true match=image_repo; got %v", e.Properties)
	}
	// The canonical code service target node must be minted.
	var found bool
	for _, en := range ents {
		if en.ID == to {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected target service node %s to be minted", to)
	}
}

// TestK8sWorkload_BaseImage_NoOp guards that an off-the-shelf base/sidecar image
// (postgres) never mints a DEPLOYS edge — it is not first-party code.
func TestK8sWorkload_BaseImage_NoOp(t *testing.T) {
	src := `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: db
spec:
  template:
    spec:
      containers:
        - name: db
          image: postgres:16
`
	_, rels := runDeployCodeDetect(t, "yaml", "k8s/db.yaml", src)
	if deployCodeAnyEdge(rels) {
		t.Fatalf("expected NO DEPLOYS edge for a postgres base image; rels=%v", rels)
	}
}

// TestCompose_FirstPartyImage_Deploys: a compose service with a first-party
// image yields a DEPLOYS edge from the compose service node to the image repo's
// code service node.
func TestCompose_FirstPartyImage_Deploys(t *testing.T) {
	src := `
services:
  web:
    image: registry.example.com/team/web-frontend:latest
  cache:
    image: redis:7
`
	_, rels := runDeployCodeDetect(t, "yaml", "docker-compose.yml", src)
	if !deployCodeHasEdge(rels, "service:web", "service:web-frontend") {
		t.Fatalf("expected DEPLOYS service:web -> service:web-frontend; rels=%v", rels)
	}
	// redis is a base image — no edge for the cache service.
	if deployCodeHasEdge(rels, "service:cache", "service:redis") {
		t.Fatalf("did not expect a DEPLOYS edge for the redis cache service; rels=%v", rels)
	}
}

// TestCompose_BuildOnly_NoSelfLoop: a compose service that BUILDS first-party
// code (no image override) IS itself the code node keyed by its own service
// name — the compose service node and the code service node are the SAME id, so
// the identity cross-link already covers it and no self-loop DEPLOYS edge is
// minted (emitEdge rejects from==to). This guards against a degenerate self-edge.
func TestCompose_BuildOnly_NoSelfLoop(t *testing.T) {
	src := `
services:
  worker:
    build: ./worker
`
	_, rels := runDeployCodeDetect(t, "yaml", "compose.yaml", src)
	if deployCodeAnyEdge(rels) {
		t.Fatalf("build-only service must not emit a self-loop DEPLOYS edge; rels=%v", rels)
	}
}

// TestCompose_BuildWithImageRename_Deploys: a build-only service whose image is
// renamed to a different repo deploys that distinct code node.
func TestCompose_BuildWithImageRename_Deploys(t *testing.T) {
	src := `
services:
  worker:
    image: myorg/order-processor
    build: ./worker
`
	_, rels := runDeployCodeDetect(t, "yaml", "compose.yaml", src)
	if !deployCodeHasEdge(rels, "service:worker", "service:order-processor") {
		t.Fatalf("expected DEPLOYS service:worker -> service:order-processor; rels=%v", rels)
	}
}

// TestServerless_Handler_Deploys: a serverless function with a static handler
// yields a DEPLOYS edge from the Lambda node to the canonical code service node.
func TestServerless_Handler_Deploys(t *testing.T) {
	src := `
service: orders
functions:
  createOrder:
    handler: src/orders/create.handler
  templated:
    handler: ${self:custom.handlerBase}.run
`
	_, rels := runDeployCodeDetect(t, "yaml", "serverless.yml", src)
	if !deployCodeHasEdge(rels, "aws-lambda:createOrder", "service:createOrder") {
		t.Fatalf("expected DEPLOYS aws-lambda:createOrder -> service:createOrder; rels=%v", rels)
	}
	// Templated handler is dropped, not guessed.
	if deployCodeHasEdge(rels, "aws-lambda:templated", "service:templated") {
		t.Fatalf("did not expect an edge for the templated-handler function; rels=%v", rels)
	}
}

// TestNonManifest_NoOp: an ordinary source file is a fast no-op.
func TestNonManifest_NoOp(t *testing.T) {
	_, rels := runDeployCodeDetect(t, "go", "main.go", "package main\nfunc main() {}\n")
	if deployCodeAnyEdge(rels) {
		t.Fatalf("expected no DEPLOYS edges for a non-manifest file; rels=%v", rels)
	}
}

// TestDeployImageRepo unit-checks the image-repo extraction heuristic.
func TestDeployImageRepo(t *testing.T) {
	cases := map[string]string{
		"myorg/api-service:1.2":          "api-service",
		"registry.io/team/web:latest":    "web",
		"api-gateway":                    "api-gateway",
		"api-gateway:v2":                 "api-gateway",
		"postgres:16":                    "",
		"redis":                          "",
		"${IMAGE}:tag":                   "",
		"":                               "",
		"library/nginx:1.25":             "",
		"ghcr.io/acme/billing@sha256:ab": "billing",
	}
	for in, want := range cases {
		if got := deployImageRepo(in); got != want {
			t.Errorf("deployImageRepo(%q) = %q, want %q", in, got, want)
		}
	}
}
