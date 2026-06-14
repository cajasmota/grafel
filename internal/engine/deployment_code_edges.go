// Explicit infra↔code DEPLOYMENT edges — #4983 (Topology Model 2/3, epic #4810).
//
// Background
//
// Topology Model 2 (#4810) cross-links the Infra lens and the Code/Modules lens.
// It backs the link with (1) shared node identity — the same id appearing in
// both lenses — and (2) typed USAGE edges (READS_FROM / WRITES_TO / FETCHES /
// CALLS …) that join code entities to the datastores/queues they USE. What the
// graph did NOT have, before this pass, was an explicit DEPLOY-time edge: the
// relationship an AWS-architecture diagram most wants to show —
//
//	"this K8s Deployment RUNS this code service",
//	"this Lambda IS this code handler",
//	"this docker-compose service DEPLOYS this image/repo".
//
// applyKubernetesEdges links infra→infra (Service→Deployment, Ingress→Service);
// applyDeploymentTopologyEdges models the reverse-proxy / compose request-flow
// (gateway DEPENDS_ON / ROUTES_TO service); neither connects an IaC COMPUTE
// resource to the CODE service/module it deploys. This pass fills exactly that
// gap with a first-class DEPLOYS edge.
//
// What this pass does
//
// For each infra compute resource it can name the code it runs, it emits:
//
//	infra compute resource  -- DEPLOYS -->  service:<name>   (canonical code node)
//
//	  - Kubernetes workload (Deployment/StatefulSet/DaemonSet/ReplicaSet/Job/
//	    CronJob/Pod/Rollout): every container `image:` whose repository segment
//	    names a service → DEPLOYS from the workload resource node to
//	    service:<imageRepo>. The workload resource node reuses the SAME
//	    `k8s/<file>#resource/<kind>/<name>` id applyKubernetesEdges mints.
//	  - docker-compose service with a local `image:` or a `build:` → DEPLOYS
//	    from the compose `service:<svc>` node to the code `service:<imageRepo>`
//	    (or service:<svc> for a `build:`-only service — the service IS the repo).
//	  - Serverless Framework function (`functions.<name>.handler: src/foo.bar`)
//	    → DEPLOYS from the serverless `aws-lambda:<name>` node to the code
//	    `service:<name>` node.
//
// Canonical collapse
//
// The target is keyed `service:<name>` — the SAME convention
// applyDeploymentTopologyEdges and applyAPIGatewayRoutingEdges use — so the
// DEPLOYS edge lands on the real code/compose service node when one exists, and
// is a declared-but-unwired deploy target otherwise (the honest no-op-friendly
// behaviour the surrounding IaC passes share).
//
// Honesty
//
// Public base images (registry library images: postgres, redis, nginx, node,
// python, …) are NOT a code service the repo owns — they are filtered out so a
// `image: postgres:16` sidecar does not mint a bogus DEPLOYS edge. Image refs
// with a templated/variable repository (${...}, $VAR) are dropped, not guessed.
// Every emitted edge carries `inferred=true` + a `match` provenance so the
// dashboard can style the deploy-time mapping distinctly (#4983 "deployed"
// style) from identity (primary) and typed-usage (linked) cross-links.
//
// Scope guard
//
// Append-only: never modifies or removes existing entities/edges, so it cannot
// regress the surrounding pipeline. Fires only for K8s manifests, docker-compose
// files, and serverless.yml; every other file is a fast no-op.
//
// Closes #4983. Part of #4810.
package engine

import (
	"path/filepath"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
	"gopkg.in/yaml.v3"
)

// deployCodeEdgeKind is the explicit infra→code deployment edge.
var deployCodeEdgeKind = string(types.RelationshipKindDeploys)

// deployCodeBaseImages is the set of well-known public base/sidecar image
// repositories that are NOT a code service the repo owns. A workload running one
// of these is using off-the-shelf infrastructure, not deploying first-party
// code, so no DEPLOYS edge is minted for it. Matched on the repository's final
// path segment (the image "name"), case-insensitively.
var deployCodeBaseImages = map[string]bool{
	"postgres": true, "mysql": true, "mariadb": true, "mongo": true,
	"redis": true, "memcached": true, "rabbitmq": true, "kafka": true,
	"zookeeper": true, "elasticsearch": true, "opensearch": true,
	"nginx": true, "httpd": true, "haproxy": true, "envoy": true,
	"traefik": true, "caddy": true, "consul": true, "vault": true,
	"node": true, "python": true, "golang": true, "openjdk": true,
	"ruby": true, "php": true, "alpine": true, "ubuntu": true, "debian": true,
	"busybox": true, "prometheus": true, "grafana": true, "minio": true,
	"localstack": true, "etcd": true, "nats": true, "clickhouse": true,
	"cassandra": true, "couchdb": true, "influxdb": true,
}

// applyDeploymentCodeEdges is the per-file entry point, registered in
// detector.go after the K8s / compose / serverless passes whose nodes it links.
// Append-only.
func applyDeploymentCodeEdges(args DetectorPassArgs) DetectorPassResult {
	entities := args.Entities
	relationships := args.Relationships
	if len(args.Content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	base := strings.ToLower(filepath.Base(args.Path))
	src := args.Content

	seenEdge := map[string]bool{}
	seenEnt := map[string]bool{}
	for _, e := range entities {
		seenEnt[e.ID] = true
	}
	emitTarget := func(name string) string {
		id := depTopoServiceID(name)
		if !seenEnt[id] {
			seenEnt[id] = true
			entities = append(entities, types.EntityRecord{
				ID:            id,
				Name:          name,
				QualifiedName: id,
				Kind:          string(types.EntityKindService),
				SourceFile:    args.Path,
				Language:      args.Lang,
				Properties: map[string]string{
					"deployment_role": "backend",
					"synthesis":       "deployment_code_edges",
				},
				EnrichmentStatus: types.StatusPending,
				QualityScore:     0.8,
			})
		}
		return id
	}
	emitEdge := func(fromID, toID, match string) {
		if fromID == "" || toID == "" || fromID == toID {
			return
		}
		key := fromID + "|" + toID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: fromID,
			ToID:   toID,
			Kind:   deployCodeEdgeKind,
			Properties: map[string]string{
				"inferred":  "true",
				"match":     match, // "image_repo" | "compose_build" | "serverless_handler"
				"synthesis": "deployment_code_edges",
			},
		})
	}

	switch {
	case args.Lang == "yaml" && k8sHasTopLevelKey(src, "apiVersion") && k8sHasTopLevelKey(src, "kind"):
		applyK8sDeployCodeEdges(args.Path, src, emitTarget, emitEdge)
	case base == "docker-compose.yml" || base == "docker-compose.yaml" ||
		base == "compose.yml" || base == "compose.yaml" ||
		depTopoComposeOverrideRe.MatchString(base):
		applyComposeDeployCodeEdges(src, emitTarget, emitEdge)
	case base == "serverless.yml" || base == "serverless.yaml":
		applyServerlessDeployCodeEdges(src, emitTarget, emitEdge)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// applyK8sDeployCodeEdges emits a DEPLOYS edge from each workload resource node
// to service:<imageRepo> for every first-party container image it runs.
func applyK8sDeployCodeEdges(path string, src []byte, emitTarget func(string) string, emitEdge func(fromID, toID, match string)) {
	docs := k8sParseDocs(src)
	refPrefix := "k8s/" + path + "#"
	for _, d := range docs {
		if !k8sWorkloadKinds[d.kind] || d.name == "" {
			continue
		}
		from := refPrefix + "resource/" + d.kind + "/" + d.name
		for _, img := range k8sWorkloadImages(d.raw) {
			repo := deployImageRepo(img)
			if repo == "" {
				continue
			}
			to := emitTarget(repo)
			emitEdge(from, to, "image_repo")
		}
	}
}

// k8sWorkloadImages returns every container/initContainer `image:` string in a
// workload's pod spec.
func k8sWorkloadImages(raw map[string]interface{}) []string {
	ps := k8sPodSpec(raw)
	if ps == nil {
		// A bare Pod has spec.containers directly (no template).
		ps, _ = k8sDig(raw, "spec").(map[string]interface{})
	}
	if ps == nil {
		return nil
	}
	var out []string
	collect := func(key string) {
		for _, c := range k8sSlice(ps[key]) {
			if cm, ok := c.(map[string]interface{}); ok {
				if img, ok := cm["image"].(string); ok && img != "" {
					out = append(out, img)
				}
			}
		}
	}
	collect("containers")
	collect("initContainers")
	return out
}

// composeDeployFile is the minimal docker-compose shape this pass decodes: a
// service's image and whether it declares a build.
type composeDeployFile struct {
	Services map[string]struct {
		Image string    `yaml:"image"`
		Build yaml.Node `yaml:"build"`
	} `yaml:"services"`
}

// applyComposeDeployCodeEdges emits a DEPLOYS edge from a compose service node to
// the code service it runs: service:<imageRepo> for a first-party `image:`, or
// service:<svc> for a `build:`-only service (the service IS the built repo).
func applyComposeDeployCodeEdges(src []byte, emitTarget func(string) string, emitEdge func(fromID, toID, match string)) {
	var cf composeDeployFile
	if err := yaml.Unmarshal(src, &cf); err != nil || len(cf.Services) == 0 {
		return
	}
	for name, svc := range cf.Services {
		from := depTopoServiceID(name)
		if repo := deployImageRepo(svc.Image); repo != "" && repo != name {
			to := emitTarget(repo)
			emitEdge(from, to, "image_repo")
			continue
		}
		// A `build:` (string or map with a context) means the service builds
		// first-party code in this repo; it deploys the code keyed by its own
		// service name.
		if composeHasBuild(svc.Build) {
			emitEdge(from, depTopoServiceID(name), "compose_build")
		}
	}
}

// composeHasBuild reports whether a compose `build:` node is present and is not
// empty (string short form or mapping long form with a context/dockerfile).
func composeHasBuild(n yaml.Node) bool {
	switch n.Kind {
	case yaml.ScalarNode:
		return strings.TrimSpace(n.Value) != ""
	case yaml.MappingNode:
		return len(n.Content) > 0
	}
	return false
}

// serverlessDeployFile is the minimal serverless.yml shape this pass decodes.
type serverlessDeployFile struct {
	Functions map[string]struct {
		Handler string `yaml:"handler"`
	} `yaml:"functions"`
}

// applyServerlessDeployCodeEdges emits a DEPLOYS edge from the serverless Lambda
// node (aws-lambda:<name>, the id serverless_framework_edges.go mints) to the
// canonical code service:<name> for every function with a static handler.
func applyServerlessDeployCodeEdges(src []byte, emitTarget func(string) string, emitEdge func(fromID, toID, match string)) {
	var sf serverlessDeployFile
	if err := yaml.Unmarshal(src, &sf); err != nil || len(sf.Functions) == 0 {
		return
	}
	for name, fn := range sf.Functions {
		if name == "" || strings.TrimSpace(fn.Handler) == "" {
			continue
		}
		// Templated handler (a ${...} interpolation) is not a real source ref.
		if strings.Contains(fn.Handler, "${") || strings.Contains(fn.Handler, "$(") {
			continue
		}
		from := "aws-lambda:" + name
		to := emitTarget(name)
		emitEdge(from, to, "serverless_handler")
	}
}

// deployImageRepo extracts the service-naming repository segment from a container
// image reference, returning "" when the image is a public base/sidecar image, a
// templated/variable ref, or otherwise not a first-party code service.
//
// Examples:
//
//	myorg/api-service:1.2          -> "api-service"
//	registry.io/team/web:latest    -> "web"
//	api-gateway                    -> "api-gateway"
//	postgres:16                    -> ""   (public base image)
//	${IMAGE}:tag                   -> ""   (templated)
func deployImageRepo(image string) string {
	image = strings.TrimSpace(image)
	if image == "" || strings.Contains(image, "$") || strings.Contains(image, "{") {
		return ""
	}
	// Strip a digest (@sha256:...) and a :tag from the LAST path segment only.
	if i := strings.Index(image, "@"); i >= 0 {
		image = image[:i]
	}
	// Split off registry/namespace path; the repo name is the final segment.
	repoPath := image
	// A :tag may only appear on the final segment, after the last '/'.
	lastSlash := strings.LastIndex(repoPath, "/")
	finalSeg := repoPath[lastSlash+1:]
	if i := strings.Index(finalSeg, ":"); i >= 0 {
		finalSeg = finalSeg[:i]
	}
	finalSeg = strings.TrimSpace(finalSeg)
	if finalSeg == "" {
		return ""
	}
	// An official library image with no namespace (e.g. "postgres:16",
	// "redis") OR any image whose final segment is a known base image name is
	// off-the-shelf infrastructure, not first-party code.
	if deployCodeBaseImages[strings.ToLower(finalSeg)] {
		return ""
	}
	return finalSeg
}
