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
