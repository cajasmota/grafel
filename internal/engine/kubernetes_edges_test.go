package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// k8sFindEdge returns the first relationship matching (fromID, toID, kind), or
// nil. Endpoints are compared exactly — these are the QualifiedName refs the
// resolver binds via byQualifiedName.
func k8sFindEdge(rels []types.RelationshipRecord, fromID, toID, kind string) *types.RelationshipRecord {
	for i := range rels {
		r := rels[i]
		if r.FromID == fromID && r.ToID == toID && r.Kind == kind {
			return &rels[i]
		}
	}
	return nil
}

func k8sRun(path, src string) []types.RelationshipRecord {
	res := applyKubernetesEdges(DetectorPassArgs{
		Lang:    "yaml",
		Path:    path,
		Content: []byte(src),
	})
	return res.Relationships
}

// The headline assertion: a Service (selector app=web) + a Deployment (template
// labels app=web) whose container pulls env from a ConfigMap must produce BOTH
// the Service→Deployment ROUTES_TO edge AND the container→ConfigMap USES edge,
// with the exact endpoint refs.
func TestKubernetesEdges_SelectorMatchAndConfigMapRef(t *testing.T) {
	const path = "k8s/web.yaml"
	src := `
apiVersion: v1
kind: Service
metadata:
  name: web
spec:
  selector:
    app: web
  ports:
    - port: 80
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
        tier: frontend
    spec:
      containers:
        - name: web
          image: nginx:1.25
          env:
            - name: DB_HOST
              valueFrom:
                configMapKeyRef:
                  name: web-config
                  key: db.host
          envFrom:
            - secretRef:
                name: web-secrets
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: web-config
data:
  db.host: postgres
`
	rels := k8sRun(path, src)

	prefix := "k8s/" + path + "#"
	svcRef := prefix + "resource/Service/web"
	deployRef := prefix + "resource/Deployment/web"
	containerRef := prefix + "container/web"
	cmRef := prefix + "resource/ConfigMap/web-config"
	secretRef := prefix + "resource/Secret/web-secrets"

	// (1) Service → Deployment ROUTES_TO (label superset match).
	if e := k8sFindEdge(rels, svcRef, deployRef, "ROUTES_TO"); e == nil {
		t.Fatalf("missing Service→Deployment ROUTES_TO edge (%s → %s); rels=%+v", svcRef, deployRef, rels)
	} else if e.Properties["k8s_edge"] != "selector_match" {
		t.Fatalf("ROUTES_TO edge has wrong k8s_edge tag: %q", e.Properties["k8s_edge"])
	}

	// (2) container → ConfigMap USES (env configMapKeyRef).
	if e := k8sFindEdge(rels, containerRef, cmRef, "USES"); e == nil {
		t.Fatalf("missing container→ConfigMap USES edge (%s → %s); rels=%+v", containerRef, cmRef, rels)
	}

	// (2b) container → Secret USES (envFrom secretRef).
	if e := k8sFindEdge(rels, containerRef, secretRef, "USES"); e == nil {
		t.Fatalf("missing container→Secret USES edge (%s → %s); rels=%+v", containerRef, secretRef, rels)
	}
}

// A selector that is NOT a subset of the pod labels must NOT match.
func TestKubernetesEdges_SelectorNoMatch(t *testing.T) {
	const path = "k8s/mismatch.yaml"
	src := `
apiVersion: v1
kind: Service
metadata:
  name: api
spec:
  selector:
    app: api
    tier: backend
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: api:1
`
	rels := k8sRun(path, src)
	prefix := "k8s/" + path + "#"
	if e := k8sFindEdge(rels, prefix+"resource/Service/api", prefix+"resource/Deployment/api", "ROUTES_TO"); e != nil {
		t.Fatalf("selector {app,tier} must NOT match pod labels {app} — got spurious edge %+v", e)
	}
}

func TestKubernetesEdges_VolumeRefs(t *testing.T) {
	const path = "k8s/vol.yaml"
	src := `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: db
spec:
  template:
    metadata:
      labels:
        app: db
    spec:
      containers:
        - name: db
          image: postgres:16
      volumes:
        - name: config
          configMap:
            name: db-config
        - name: tls
          secret:
            secretName: db-tls
        - name: data
          persistentVolumeClaim:
            claimName: db-data
`
	rels := k8sRun(path, src)
	prefix := "k8s/" + path + "#"
	stsRef := prefix + "resource/StatefulSet/db"

	cases := []struct {
		to   string
		desc string
	}{
		{prefix + "resource/ConfigMap/db-config", "volume configMap"},
		{prefix + "resource/Secret/db-tls", "volume secret"},
		{prefix + "resource/PersistentVolumeClaim/db-data", "volume pvc"},
	}
	for _, c := range cases {
		if e := k8sFindEdge(rels, stsRef, c.to, "USES"); e == nil {
			t.Fatalf("missing %s USES edge (%s → %s); rels=%+v", c.desc, stsRef, c.to, rels)
		}
	}
}

func TestKubernetesEdges_IngressBackend(t *testing.T) {
	const path = "k8s/ing.yaml"
	src := `
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: web-ing
spec:
  rules:
    - host: example.com
      http:
        paths:
          - path: /
            backend:
              service:
                name: web
                port:
                  number: 80
`
	rels := k8sRun(path, src)
	prefix := "k8s/" + path + "#"
	from := prefix + "resource/Ingress/web-ing"
	to := prefix + "resource/Service/web"
	if e := k8sFindEdge(rels, from, to, "ROUTES_TO"); e == nil {
		t.Fatalf("missing Ingress→Service ROUTES_TO edge (%s → %s); rels=%+v", from, to, rels)
	} else if e.Properties["k8s_edge"] != "ingress_backend" {
		t.Fatalf("ingress edge wrong tag: %q", e.Properties["k8s_edge"])
	}
}

func TestKubernetesEdges_HPATarget(t *testing.T) {
	const path = "k8s/hpa.yaml"
	src := `
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: web-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: web
  minReplicas: 2
  maxReplicas: 10
`
	rels := k8sRun(path, src)
	prefix := "k8s/" + path + "#"
	from := prefix + "resource/HorizontalPodAutoscaler/web-hpa"
	to := prefix + "resource/Deployment/web"
	if e := k8sFindEdge(rels, from, to, "DEPENDS_ON"); e == nil {
		t.Fatalf("missing HPA→Deployment DEPENDS_ON edge (%s → %s); rels=%+v", from, to, rels)
	} else if e.Properties["k8s_edge"] != "hpa_target" {
		t.Fatalf("hpa edge wrong tag: %q", e.Properties["k8s_edge"])
	}
}

func TestKubernetesEdges_OwnerReference(t *testing.T) {
	const path = "k8s/owner.yaml"
	src := `
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: web-rs
  ownerReferences:
    - apiVersion: apps/v1
      kind: Deployment
      name: web
spec:
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: web
          image: nginx
`
	rels := k8sRun(path, src)
	prefix := "k8s/" + path + "#"
	from := prefix + "resource/ReplicaSet/web-rs"
	to := prefix + "resource/Deployment/web"
	if e := k8sFindEdge(rels, from, to, "DEPENDS_ON"); e == nil {
		t.Fatalf("missing ownerReference DEPENDS_ON edge (%s → %s); rels=%+v", from, to, rels)
	} else if e.Properties["k8s_edge"] != "owner_reference" {
		t.Fatalf("owner edge wrong tag: %q", e.Properties["k8s_edge"])
	}
}

// Non-K8s YAML (a docker-compose file) must be a complete no-op.
func TestKubernetesEdges_NonManifestNoOp(t *testing.T) {
	src := `
services:
  web:
    image: nginx
    ports:
      - "80:80"
`
	rels := k8sRun("docker-compose.yml", src)
	if len(rels) != 0 {
		t.Fatalf("expected no edges for non-manifest YAML, got %+v", rels)
	}
}

// CronJob nests the pod template one layer deeper (spec.jobTemplate.spec.template).
func TestKubernetesEdges_CronJobConfigMapRef(t *testing.T) {
	const path = "k8s/cron.yaml"
	src := `
apiVersion: batch/v1
kind: CronJob
metadata:
  name: reporter
spec:
  jobTemplate:
    spec:
      template:
        metadata:
          labels:
            app: reporter
        spec:
          containers:
            - name: reporter
              image: reporter:1
              envFrom:
                - configMapRef:
                    name: reporter-config
`
	rels := k8sRun(path, src)
	prefix := "k8s/" + path + "#"
	from := prefix + "container/reporter"
	to := prefix + "resource/ConfigMap/reporter-config"
	if e := k8sFindEdge(rels, from, to, "USES"); e == nil {
		t.Fatalf("missing CronJob container→ConfigMap USES edge (%s → %s); rels=%+v", from, to, rels)
	}
}

// ---------------------------------------------------------------------------
// #3551 — namespace scoping + NetworkPolicy / PDB / ServiceMonitor edges
// ---------------------------------------------------------------------------

// Two same-named Services (`web`) live in DIFFERENT namespaces (prod / staging),
// each selecting app=web. Each namespace has a DISTINCTLY-named workload also
// labelled app=web (web-prod / web-staging). Selector matching must stay WITHIN
// a namespace: the prod Service links only web-prod, the staging Service only
// web-staging. No cross-namespace edge may appear. (Distinct workload names give
// distinct ToID refs so the seenEdge dedup does not mask the scoping.)
func TestKubernetesEdges_NamespaceScopedSelector(t *testing.T) {
	const path = "k8s/multi-ns.yaml"
	src := `
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: prod
spec:
  selector:
    app: web
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-prod
  namespace: prod
spec:
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: web
          image: nginx:prod
---
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: staging
spec:
  selector:
    app: web
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-staging
  namespace: staging
spec:
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: web
          image: nginx:staging
`
	rels := k8sRun(path, src)
	prefix := "k8s/" + path + "#"
	svcRef := prefix + "resource/Service/web"
	prodRef := prefix + "resource/Deployment/web-prod"
	stagingRef := prefix + "resource/Deployment/web-staging"

	// The prod Service must link the prod workload (same namespace).
	if e := k8sFindEdge(rels, svcRef, prodRef, "ROUTES_TO"); e == nil {
		t.Fatalf("expected prod Service→web-prod ROUTES_TO edge; rels=%+v", rels)
	}
	// The staging Service must link the staging workload (same namespace).
	if e := k8sFindEdge(rels, svcRef, stagingRef, "ROUTES_TO"); e == nil {
		t.Fatalf("expected staging Service→web-staging ROUTES_TO edge; rels=%+v", rels)
	}
	// EXACTLY two selector_match edges — the within-namespace pairings only. If
	// namespace scoping were absent, each of the two Services would also match
	// the OTHER namespace's workload, yielding 4 edges (2 distinct ToIDs × the
	// collapsed-svcRef × 2 → still 2 distinct after dedup but to BOTH workloads
	// from each svc). The decisive check below asserts NO third/fourth edge and
	// that scoping held.
	selectorEdges := map[string]bool{}
	for _, r := range rels {
		if r.Properties["k8s_edge"] == "selector_match" {
			selectorEdges[r.FromID+"->"+r.ToID] = true
		}
	}
	if len(selectorEdges) != 2 {
		t.Fatalf("expected exactly 2 distinct selector_match edges (prod, staging), got %d: %v", len(selectorEdges), selectorEdges)
	}
}

// A sharper cross-namespace test: the Service is in `prod` selecting app=web,
// but the ONLY matching Deployment is in `staging`. With namespace scoping there
// must be NO edge (the prod Service cannot select a staging pod).
func TestKubernetesEdges_NoCrossNamespaceSelector(t *testing.T) {
	const path = "k8s/cross-ns.yaml"
	src := `
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: prod
spec:
  selector:
    app: web
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: staging
spec:
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: web
          image: nginx
`
	rels := k8sRun(path, src)
	prefix := "k8s/" + path + "#"
	svcRef := prefix + "resource/Service/web"
	deployRef := prefix + "resource/Deployment/web"
	if e := k8sFindEdge(rels, svcRef, deployRef, "ROUTES_TO"); e != nil {
		t.Fatalf("prod Service must NOT select staging Deployment across namespaces — got spurious edge %+v", e)
	}
}

// NetworkPolicy spec.podSelector.matchLabels → DEPENDS_ON edge to the matched
// workload in the same namespace.
func TestKubernetesEdges_NetworkPolicyPodSelector(t *testing.T) {
	const path = "k8s/netpol.yaml"
	src := `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: web-allow
  namespace: prod
spec:
  podSelector:
    matchLabels:
      app: web
  policyTypes:
    - Ingress
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: prod
spec:
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: web
          image: nginx
`
	rels := k8sRun(path, src)
	prefix := "k8s/" + path + "#"
	from := prefix + "resource/NetworkPolicy/web-allow"
	to := prefix + "resource/Deployment/web"
	e := k8sFindEdge(rels, from, to, "DEPENDS_ON")
	if e == nil {
		t.Fatalf("missing NetworkPolicy→Deployment DEPENDS_ON edge (%s → %s); rels=%+v", from, to, rels)
	}
	if e.Properties["k8s_edge"] != "networkpolicy_podselector" {
		t.Fatalf("netpol edge wrong tag: %q", e.Properties["k8s_edge"])
	}
}

// PodDisruptionBudget spec.selector → DEPENDS_ON workload (same namespace).
func TestKubernetesEdges_PDBSelector(t *testing.T) {
	const path = "k8s/pdb.yaml"
	src := `
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: web-pdb
  namespace: prod
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app: web
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: prod
spec:
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: web
          image: nginx
`
	rels := k8sRun(path, src)
	prefix := "k8s/" + path + "#"
	from := prefix + "resource/PodDisruptionBudget/web-pdb"
	to := prefix + "resource/Deployment/web"
	if e := k8sFindEdge(rels, from, to, "DEPENDS_ON"); e == nil {
		t.Fatalf("missing PDB→Deployment DEPENDS_ON edge; rels=%+v", rels)
	} else if e.Properties["k8s_edge"] != "pdb_selector" {
		t.Fatalf("pdb edge wrong tag: %q", e.Properties["k8s_edge"])
	}
}

// ServiceMonitor spec.selector → DEPENDS_ON Service (same namespace).
func TestKubernetesEdges_ServiceMonitorSelector(t *testing.T) {
	const path = "k8s/sm.yaml"
	src := `
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: web-sm
  namespace: monitoring
spec:
  selector:
    matchLabels:
      team: web
  endpoints:
    - port: metrics
---
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: monitoring
  labels:
    team: web
spec:
  selector:
    app: web
`
	rels := k8sRun(path, src)
	prefix := "k8s/" + path + "#"
	from := prefix + "resource/ServiceMonitor/web-sm"
	to := prefix + "resource/Service/web"
	if e := k8sFindEdge(rels, from, to, "DEPENDS_ON"); e == nil {
		t.Fatalf("missing ServiceMonitor→Service DEPENDS_ON edge; rels=%+v", rels)
	} else if e.Properties["k8s_edge"] != "servicemonitor_selector" {
		t.Fatalf("servicemonitor edge wrong tag: %q", e.Properties["k8s_edge"])
	}
}
