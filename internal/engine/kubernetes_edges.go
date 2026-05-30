// Kubernetes cross-resource EDGE synthesis — #3517 (epic #3512).
//
// The yaml extractor (internal/extractors/yaml/extractor.go) already extracts
// K8s resources and their sub-resources well: a Deployment/Service/StatefulSet/
// DaemonSet becomes a SCOPE.Service entity, containers/env vars/ports/selectors/
// volumeMounts become SCOPE.Component / SCOPE.Schema children with CONTAINS
// edges, ConfigMap data keys become SCOPE.Schema config_key entities, and so on.
//
// What it does NOT do is connect resources to each OTHER. A Service that selects
// a Deployment's pods, a container that mounts a ConfigMap/Secret, an Ingress
// whose backend is a Service, an HPA that scales a Deployment — these are the
// edges an architecture graph actually wants, and they were entirely absent.
//
// This post-pass reads the same K8s manifest file the extractor saw (file-scoped:
// K8s manifests are conventionally one logical app per file / kustomize base) and
// synthesises four families of cross-resource edges, addressing each resource by
// the SAME QualifiedName the extractor minted so the resolver's byQualifiedName
// index binds them:
//
//	resource ref  = "k8s/<file>#resource/<Kind>/<name>"
//	container ref  = "k8s/<file>#container/<containerName>"
//	init ref       = "k8s/<file>#init-container/<containerName>"
//
// Edge families (reusing existing RelationshipKinds — no new Kind needed):
//
//  1. Selector → workload (the #1 K8s edge). A Service whose spec.selector
//     {k:v} map is a subset of a workload's spec.template.metadata.labels →
//     ROUTES_TO edge Service→workload. ROUTES_TO is apt: a Service load-balances
//     traffic TO the matching pods.
//  2. env / volume references. A container's env[].valueFrom.configMapKeyRef /
//     secretKeyRef, envFrom[].configMapRef / secretRef, and a pod's
//     volumes[].configMap / secret / persistentVolumeClaim.claimName → USES
//     edge container→ConfigMap/Secret (and workload→PVC). The PVC has no
//     extracted entity of its own, so PVC refs are recorded as a USES edge to
//     the resource ref shape, which binds only when a PVC manifest is present
//     in the same file.
//  3. Ingress backend → Service. An Ingress rule's
//     http.paths[].backend.service.name → ROUTES_TO edge Ingress→Service.
//  4. HPA scaleTargetRef → workload. A HorizontalPodAutoscaler's
//     spec.scaleTargetRef.{kind,name} → DEPENDS_ON edge HPA→workload.
//
// Scope guard: append-only. This pass never modifies or removes existing
// entities or edges, so it cannot regress the yaml extractor's output or the
// surrounding pipeline's bug-rate. Edges whose endpoint resource is absent from
// the file are still emitted (the resolver drops unbound refs); they bind iff
// the target manifest shares the file.
//
// Refs #3517.
package engine

import (
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
	"gopkg.in/yaml.v3"
)

// k8sWorkloadKinds is the set of K8s Kinds whose spec.template.metadata.labels a
// Service selector / HPA / etc. may target.
var k8sWorkloadKinds = map[string]bool{
	"Deployment":            true,
	"StatefulSet":           true,
	"DaemonSet":             true,
	"ReplicaSet":            true,
	"ReplicationController": true,
	"Job":                   true,
	"CronJob":               true,
}

// k8sDoc is one parsed Kubernetes document from a manifest file. Only the fields
// this pass needs are modelled; everything else is ignored by the decoder.
type k8sDoc struct {
	kind string
	name string
	raw  map[string]interface{}
}

// applyKubernetesEdges is the entry point, registered in detector.go after the
// other IaC passes. Append-only.
func applyKubernetesEdges(args DetectorPassArgs) DetectorPassResult {
	entities := args.Entities
	relationships := args.Relationships
	if args.Lang != "yaml" || len(args.Content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	src := args.Content
	// Cheap guard: a K8s manifest has top-level apiVersion + kind. Mirror the
	// extractor's detectFlavor heuristic so this pass only fires on manifests.
	if !k8sHasTopLevelKey(src, "apiVersion") || !k8sHasTopLevelKey(src, "kind") {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	docs := k8sParseDocs(src)
	if len(docs) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	refPrefix := "k8s/" + args.Path + "#"
	resourceRef := func(kind, name string) string {
		return refPrefix + "resource/" + kind + "/" + name
	}

	// Index workloads by Kind/name and collect their pod-template labels so the
	// selector-match pass can superset-test against them.
	type workload struct {
		kind   string
		name   string
		labels map[string]string
	}
	var workloads []workload
	for _, d := range docs {
		if !k8sWorkloadKinds[d.kind] || d.name == "" {
			continue
		}
		labels := k8sPodTemplateLabels(d.raw)
		workloads = append(workloads, workload{kind: d.kind, name: d.name, labels: labels})
	}

	seenEdge := map[string]bool{}
	emit := func(fromID, toID, kind, edgeType string) {
		if fromID == "" || toID == "" {
			return
		}
		key := fromID + "|" + toID + "|" + kind
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: fromID,
			ToID:   toID,
			Kind:   kind,
			Properties: map[string]string{
				"language":  "yaml",
				"k8s_edge":  edgeType,
				"synthesis": "kubernetes_edges",
			},
		})
	}

	for _, d := range docs {
		switch {
		case d.kind == "Service":
			// (1) Service.spec.selector → matching workload(s).
			sel := k8sStringMap(k8sDig(d.raw, "spec", "selector"))
			if len(sel) == 0 {
				continue
			}
			from := resourceRef("Service", d.name)
			for _, w := range workloads {
				if k8sLabelsMatch(w.labels, sel) {
					emit(from, resourceRef(w.kind, w.name), string(types.RelationshipKindRoutesTo), "selector_match")
				}
			}

		case d.kind == "Ingress":
			// (3) Ingress backend → Service.
			from := resourceRef("Ingress", d.name)
			for _, svcName := range k8sIngressBackendServices(d.raw) {
				emit(from, resourceRef("Service", svcName), string(types.RelationshipKindRoutesTo), "ingress_backend")
			}

		case d.kind == "HorizontalPodAutoscaler":
			// (4) HPA scaleTargetRef → workload.
			tgtKind, _ := k8sDig(d.raw, "spec", "scaleTargetRef", "kind").(string)
			tgtName, _ := k8sDig(d.raw, "spec", "scaleTargetRef", "name").(string)
			if tgtKind != "" && tgtName != "" {
				emit(resourceRef("HorizontalPodAutoscaler", d.name),
					resourceRef(tgtKind, tgtName),
					string(types.RelationshipKindDependsOn), "hpa_target")
			}

		case k8sWorkloadKinds[d.kind]:
			// (2) env / volume references from a workload's pod template.
			k8sEmitWorkloadRefs(d, refPrefix, resourceRef, emit)
		}
	}

	// (4b) ownerReferences → parent resource (cheap, any kind).
	for _, d := range docs {
		owners := k8sOwnerReferences(d.raw)
		for _, o := range owners {
			if o.kind == "" || o.name == "" || d.name == "" {
				continue
			}
			emit(resourceRef(d.kind, d.name), resourceRef(o.kind, o.name),
				string(types.RelationshipKindDependsOn), "owner_reference")
		}
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// k8sEmitWorkloadRefs emits USES edges for a workload's container env / envFrom
// references and its pod-spec volume references. Container env refs are attached
// to the specific container entity (k8s/<file>#container/<name>); volume refs are
// pod-scoped and attach to the workload resource.
func k8sEmitWorkloadRefs(
	d k8sDoc,
	refPrefix string,
	resourceRef func(kind, name string) string,
	emit func(fromID, toID, kind, edgeType string),
) {
	podSpec := k8sPodSpec(d.raw)
	if podSpec == nil {
		return
	}
	usesKind := string(types.RelationshipKindUses)

	emitContainerRefs := func(containers []interface{}, refSeg string) {
		for _, ci := range containers {
			c, ok := ci.(map[string]interface{})
			if !ok {
				continue
			}
			cName, _ := c["name"].(string)
			if cName == "" {
				continue
			}
			from := refPrefix + refSeg + "/" + cName

			// env[].valueFrom.configMapKeyRef / secretKeyRef
			for _, ei := range k8sSlice(c["env"]) {
				e, ok := ei.(map[string]interface{})
				if !ok {
					continue
				}
				vf, _ := e["valueFrom"].(map[string]interface{})
				if vf == nil {
					continue
				}
				if name := k8sRefName(vf["configMapKeyRef"]); name != "" {
					emit(from, resourceRef("ConfigMap", name), usesKind, "env_configmap_ref")
				}
				if name := k8sRefName(vf["secretKeyRef"]); name != "" {
					emit(from, resourceRef("Secret", name), usesKind, "env_secret_ref")
				}
			}

			// envFrom[].configMapRef / secretRef
			for _, ei := range k8sSlice(c["envFrom"]) {
				e, ok := ei.(map[string]interface{})
				if !ok {
					continue
				}
				if name := k8sRefName(e["configMapRef"]); name != "" {
					emit(from, resourceRef("ConfigMap", name), usesKind, "envfrom_configmap_ref")
				}
				if name := k8sRefName(e["secretRef"]); name != "" {
					emit(from, resourceRef("Secret", name), usesKind, "envfrom_secret_ref")
				}
			}
		}
	}

	emitContainerRefs(k8sSlice(podSpec["containers"]), "container")
	emitContainerRefs(k8sSlice(podSpec["initContainers"]), "init-container")

	// volumes[].configMap / secret / persistentVolumeClaim.claimName — pod-scoped,
	// attach to the workload resource itself.
	workloadRef := resourceRef(d.kind, d.name)
	for _, vi := range k8sSlice(podSpec["volumes"]) {
		v, ok := vi.(map[string]interface{})
		if !ok {
			continue
		}
		if cm, ok := v["configMap"].(map[string]interface{}); ok {
			if name, _ := cm["name"].(string); name != "" {
				emit(workloadRef, resourceRef("ConfigMap", name), usesKind, "volume_configmap_ref")
			}
		}
		if sec, ok := v["secret"].(map[string]interface{}); ok {
			// secret volumes use `secretName`, not `name`.
			if name, _ := sec["secretName"].(string); name != "" {
				emit(workloadRef, resourceRef("Secret", name), usesKind, "volume_secret_ref")
			}
		}
		if pvc, ok := v["persistentVolumeClaim"].(map[string]interface{}); ok {
			if name, _ := pvc["claimName"].(string); name != "" {
				emit(workloadRef, resourceRef("PersistentVolumeClaim", name), usesKind, "volume_pvc_ref")
			}
		}
	}
}

// k8sOwnerRef is a parsed ownerReferences[] entry.
type k8sOwnerRef struct {
	kind string
	name string
}

func k8sOwnerReferences(raw map[string]interface{}) []k8sOwnerRef {
	meta, ok := raw["metadata"].(map[string]interface{})
	if !ok {
		return nil
	}
	var out []k8sOwnerRef
	for _, oi := range k8sSlice(meta["ownerReferences"]) {
		o, ok := oi.(map[string]interface{})
		if !ok {
			continue
		}
		k, _ := o["kind"].(string)
		n, _ := o["name"].(string)
		out = append(out, k8sOwnerRef{kind: k, name: n})
	}
	return out
}

// k8sIngressBackendServices returns every backend Service name referenced by an
// Ingress, covering both networking.k8s.io/v1 (backend.service.name) and the
// legacy extensions/v1beta1 (backend.serviceName) shapes, plus the
// spec.defaultBackend.
func k8sIngressBackendServices(raw map[string]interface{}) []string {
	var out []string
	seen := map[string]bool{}
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	backendService := func(backend map[string]interface{}) {
		if backend == nil {
			return
		}
		// v1: backend.service.name
		if svc, ok := backend["service"].(map[string]interface{}); ok {
			if name, _ := svc["name"].(string); name != "" {
				add(name)
			}
		}
		// v1beta1: backend.serviceName
		if name, _ := backend["serviceName"].(string); name != "" {
			add(name)
		}
	}

	if defBackend, ok := k8sDig(raw, "spec", "defaultBackend").(map[string]interface{}); ok {
		backendService(defBackend)
	}
	for _, ri := range k8sSlice(k8sDig(raw, "spec", "rules")) {
		rule, ok := ri.(map[string]interface{})
		if !ok {
			continue
		}
		httpVal, ok := rule["http"].(map[string]interface{})
		if !ok {
			continue
		}
		for _, pi := range k8sSlice(httpVal["paths"]) {
			p, ok := pi.(map[string]interface{})
			if !ok {
				continue
			}
			if backend, ok := p["backend"].(map[string]interface{}); ok {
				backendService(backend)
			}
		}
	}
	return out
}

// k8sPodSpec returns the pod spec for a workload: spec.template.spec for
// Deployment/StatefulSet/DaemonSet/Job/ReplicaSet/ReplicationController, and
// spec.jobTemplate.spec.template.spec for CronJob.
func k8sPodSpec(raw map[string]interface{}) map[string]interface{} {
	if ps, ok := k8sDig(raw, "spec", "template", "spec").(map[string]interface{}); ok {
		return ps
	}
	// CronJob nests an extra jobTemplate layer.
	if ps, ok := k8sDig(raw, "spec", "jobTemplate", "spec", "template", "spec").(map[string]interface{}); ok {
		return ps
	}
	return nil
}

// k8sPodTemplateLabels returns spec.template.metadata.labels (or the CronJob
// jobTemplate-nested equivalent) as a string map.
func k8sPodTemplateLabels(raw map[string]interface{}) map[string]string {
	if l := k8sStringMap(k8sDig(raw, "spec", "template", "metadata", "labels")); len(l) > 0 {
		return l
	}
	return k8sStringMap(k8sDig(raw, "spec", "jobTemplate", "spec", "template", "metadata", "labels"))
}

// k8sLabelsMatch reports whether selector is a (non-empty) subset of labels —
// the Kubernetes label-selector match semantics: a Service selects a pod when
// every selector key/value is present in the pod's labels.
func k8sLabelsMatch(labels, selector map[string]string) bool {
	if len(selector) == 0 || len(labels) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// YAML traversal helpers (yaml.v3 decodes mappings into map[string]interface{}).
// ---------------------------------------------------------------------------

// k8sParseDocs decodes a (possibly multi-document) manifest into k8sDoc records,
// skipping any document that lacks a kind. Decode errors on one document do not
// abort the rest of the stream.
func k8sParseDocs(src []byte) []k8sDoc {
	dec := yaml.NewDecoder(strings.NewReader(string(src)))
	var docs []k8sDoc
	for {
		var node map[string]interface{}
		err := dec.Decode(&node)
		if err != nil {
			break
		}
		if node == nil {
			continue
		}
		kind, _ := node["kind"].(string)
		if kind == "" {
			continue
		}
		name := ""
		if meta, ok := node["metadata"].(map[string]interface{}); ok {
			name, _ = meta["name"].(string)
		}
		docs = append(docs, k8sDoc{kind: kind, name: name, raw: node})
	}
	return docs
}

// k8sDig walks a nested map[string]interface{} along the given string keys,
// returning the value at the end or nil if any hop is missing / not a map.
func k8sDig(v interface{}, keys ...string) interface{} {
	cur := v
	for _, k := range keys {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

// k8sSlice coerces a value to []interface{} (nil-safe).
func k8sSlice(v interface{}) []interface{} {
	s, _ := v.([]interface{})
	return s
}

// k8sStringMap coerces a map[string]interface{} of scalars to map[string]string.
// Non-string scalar values (ints, bools) are rendered via their YAML scalar
// text so e.g. a numeric label still participates in matching.
func k8sStringMap(v interface{}) map[string]string {
	m, ok := v.(map[string]interface{})
	if !ok || len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		switch t := val.(type) {
		case string:
			out[k] = t
		case bool:
			if t {
				out[k] = "true"
			} else {
				out[k] = "false"
			}
		case int:
			out[k] = strconv.Itoa(t)
		case int64:
			out[k] = strconv.FormatInt(t, 10)
		case float64:
			// YAML ints often decode as float64 via interface{}; render whole
			// numbers without a trailing ".0".
			if t == float64(int64(t)) {
				out[k] = strconv.FormatInt(int64(t), 10)
			}
		}
	}
	return out
}

// k8sRefName extracts the `.name` field from a {name: x, ...} reference object
// (configMapKeyRef / secretKeyRef / configMapRef / secretRef).
func k8sRefName(v interface{}) string {
	m, ok := v.(map[string]interface{})
	if !ok {
		return ""
	}
	name, _ := m["name"].(string)
	return name
}

// k8sHasTopLevelKey reports whether the manifest has a top-level (column-0)
// `key:` line. Mirrors the yaml extractor's containsTopLevelKey gate so this
// pass fires on exactly the files extractKubernetes ran on.
func k8sHasTopLevelKey(src []byte, key string) bool {
	marker := key + ":"
	for _, line := range strings.Split(string(src), "\n") {
		if len(line) == 0 || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == marker || strings.HasPrefix(trimmed, marker+" ") || strings.HasPrefix(trimmed, marker+"\t") {
			return true
		}
	}
	return false
}
