package engine

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #3593 — wire the dormant CI/infra rule buckets into the live indexer.
//
// The loader buckets every `rules/<dir>/**` tree under its top-level dirname,
// but Detect resolves compiled rule sets by `file.Language`. The classifier
// never emits `cicd`/`ansible`/`kubernetes`/`docker` as a language: CI/Ansible/
// K8s manifests are tagged `yaml`, Dockerfiles `dockerfile`, docker-compose
// files `yaml`. Before the alias added in compile() these buckets' rules could
// never fire on a real file. These tests load the REAL embedded rules (so the
// alias path under test is exercised) and assert that a representative file now
// yields a SPECIFIC named entity — not merely len>0.

// findEntity returns the first entity with the given Kind and Name, or nil.
func findEntity(entities []types.EntityRecord, kind, name string) *types.EntityRecord {
	for i := range entities {
		if entities[i].Kind == kind && entities[i].Name == name {
			return &entities[i]
		}
	}
	return nil
}

// liveDetector builds a Detector from the real embedded rules so the
// dormant-bucket alias wiring in compile() is exercised.
func liveDetector(t *testing.T) *Detector {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules failed: %v", err)
	}
	return New(rules)
}

// TestDormant3593_CICD_GitHubActions verifies the `cicd` bucket now fires on a
// GitHub Actions workflow tagged `yaml`. The workflow `name:` must surface as a
// Service entity and the `uses:` action reference as a Dependency.
func TestDormant3593_CICD_GitHubActions(t *testing.T) {
	const wf = `name: CI Pipeline
on:
  push:
    branches: [main]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
      - name: Run tests
        run: go test ./...
`
	det := liveDetector(t)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     ".github/workflows/ci.yml",
		Content:  []byte(wf),
		Language: "yaml",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if e := findEntity(res.Entities, "Service", "CI Pipeline"); e == nil {
		t.Fatalf("expected Service entity named %q from cicd bucket; got %v",
			"CI Pipeline", kindNames(res.Entities))
	}
	if e := findEntity(res.Entities, "Dependency", "actions/checkout@v4"); e == nil {
		t.Errorf("expected Dependency entity %q (uses: action); got %v",
			"actions/checkout@v4", kindNames(res.Entities))
	}
}

// TestDormant3593_Ansible_Task verifies the `ansible` bucket now fires on a
// playbook tagged `yaml`. A specific task name must surface as a Task entity.
func TestDormant3593_Ansible_Task(t *testing.T) {
	const playbook = `- name: Configure web servers
  hosts: webservers
  tasks:
    - name: Install nginx package
      ansible.builtin.apt:
        name: nginx
        state: present
`
	det := liveDetector(t)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "playbooks/site.yml",
		Content:  []byte(playbook),
		Language: "yaml",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if e := findEntity(res.Entities, "Task", "Install nginx package"); e == nil {
		t.Fatalf("expected Task entity %q from ansible bucket; got %v",
			"Install nginx package", kindNames(res.Entities))
	}
	// The top-level play name must also surface (different entity Kind, same
	// captured text) — proving the play-level source_pattern fires too.
	if e := findEntity(res.Entities, "Service", "Configure web servers"); e == nil {
		t.Errorf("expected Service entity %q (play name); got %v",
			"Configure web servers", kindNames(res.Entities))
	}
}

// TestDormant3593_Kubernetes_Deployment verifies the `kubernetes` bucket now
// fires on a manifest tagged `yaml`. A Deployment kind must surface as a
// Service entity and the metadata name as a Component.
func TestDormant3593_Kubernetes_Deployment(t *testing.T) {
	const manifest = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
spec:
  template:
    spec:
      containers:
        - name: api
          image: registry.example.com/api:1.2.3
`
	det := liveDetector(t)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "k8s/deployment.yaml",
		Content:  []byte(manifest),
		Language: "yaml",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if e := findEntity(res.Entities, "Service", "Deployment"); e == nil {
		t.Fatalf("expected Service entity %q from kubernetes bucket; got %v",
			"Deployment", kindNames(res.Entities))
	}
	if e := findEntity(res.Entities, "Component", "api-server"); e == nil {
		t.Errorf("expected Component entity %q (metadata.name); got %v",
			"api-server", kindNames(res.Entities))
	}
}

// TestDormant3593_Dockerfile_FromBase verifies the `docker` bucket now fires on
// a Dockerfile tagged `dockerfile`. The FROM base image must surface as a
// Dependency and a multi-stage target as a Component.
func TestDormant3593_Dockerfile_FromBase(t *testing.T) {
	const dockerfile = `FROM golang:1.22 AS builder
WORKDIR /src
COPY . .
RUN go build -o /app ./cmd/api

FROM gcr.io/distroless/base
COPY --from=builder /app /app
EXPOSE 8080
ENTRYPOINT ["/app"]
`
	det := liveDetector(t)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "Dockerfile",
		Content:  []byte(dockerfile),
		Language: "dockerfile",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if e := findEntity(res.Entities, "Dependency", "golang:1.22"); e == nil {
		t.Fatalf("expected Dependency entity %q from docker bucket; got %v",
			"golang:1.22", kindNames(res.Entities))
	}
	if e := findEntity(res.Entities, "Component", "builder"); e == nil {
		t.Errorf("expected Component entity %q (multi-stage target); got %v",
			"builder", kindNames(res.Entities))
	}
}

// TestDormant3593_DockerCompose_Service verifies the docker bucket's
// docker_compose rules fire on a compose file tagged `yaml` (the docker bucket
// is aliased onto yaml as well as dockerfile). The service images surface as
// Dependency entities, proving the compose source_patterns run on yaml-tagged
// files. (The top-level `^\s{2}name:` service-key pattern is line-anchored and
// relies on text-start matching, so we assert on the reliably-firing image:
// capture instead.)
func TestDormant3593_DockerCompose_Service(t *testing.T) {
	const compose = `services:
  web:
    image: nginx:1.25
    ports:
      - "80:80"
  db:
    image: postgres:16
`
	det := liveDetector(t)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "docker-compose.yml",
		Content:  []byte(compose),
		Language: "yaml",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if e := findEntity(res.Entities, "Dependency", "nginx:1.25"); e == nil {
		t.Fatalf("expected Dependency entity %q (image:) from docker_compose rules; got %v",
			"nginx:1.25", kindNames(res.Entities))
	}
	if e := findEntity(res.Entities, "Dependency", "postgres:16"); e == nil {
		t.Errorf("expected Dependency entity %q (image:); got %v",
			"postgres:16", kindNames(res.Entities))
	}
}

// TestDormant3593_AliasMapWiring asserts the alias map resolves each dormant
// bucket onto its concrete classifier language(s) at compile time, and that no
// non-listed flavor bucket (e.g. html_templates) is wired.
func TestDormant3593_AliasMapWiring(t *testing.T) {
	det := liveDetector(t)
	det.once.Do(det.compile)

	// Each alias target must have received the dormant bucket's sets appended.
	for bucket, targets := range dormantBucketAliases {
		src := det.compiled[bucket]
		if len(src) == 0 {
			t.Errorf("dormant bucket %q compiled to 0 rule sets — cannot fire", bucket)
			continue
		}
		for _, target := range targets {
			if len(det.compiled[target]) < len(src) {
				t.Errorf("alias %q→%q: target has fewer sets (%d) than source (%d)",
					bucket, target, len(det.compiled[target]), len(src))
			}
		}
	}

	// html_templates is intentionally NOT aliased (doc-only frameworks).
	if _, ok := dormantBucketAliases["html_templates"]; ok {
		t.Error("html_templates must not be aliased: its frameworks carry no engine schema")
	}
}

// kindNames is a small diagnostic helper rendering (Kind, Name) pairs.
func kindNames(entities []types.EntityRecord) []string {
	out := make([]string, 0, len(entities))
	for _, e := range entities {
		out = append(out, e.Kind+":"+e.Name)
	}
	return out
}
