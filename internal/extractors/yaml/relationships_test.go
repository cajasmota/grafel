package yaml_test

// Issue #386 — port relationships extraction to the YAML extractor.
//
// Contract:
//   - CONTAINS: file → top-level structural entities, and parent → child
//     for nested structures (job → step, service → port, etc.).
//   - IMPORTS: cross-references to external resources where simple to detect:
//        * GitHub Actions `uses: actions/checkout@v4`
//        * Docker Compose `depends_on:` lists
//
// Every relationship is tagged Properties["language"]="yaml" via
// extractor.TagRelationshipsLanguage so the resolver dispatches the YAML
// dynamic-pattern catalog (#90).

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// findRels returns every embedded relationship across the given entity slice
// matching the given Kind.
func findRels(entities []types.EntityRecord, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == kind {
				out = append(out, r)
			}
		}
	}
	return out
}

func relExists(rels []types.RelationshipRecord, fromID, toID string) bool {
	for _, r := range rels {
		if r.FromID == fromID && r.ToID == toID {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// GitHub Actions
// ---------------------------------------------------------------------------

func TestYAML_GHA_ContainsAndImports(t *testing.T) {
	src := []byte(`name: CI
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
      - name: Setup
        uses: actions/setup-go@v5
  lint:
    steps:
      - name: Lint step
        uses: golangci/golangci-lint-action@v4
`)
	entities, err := extractYAML(src, ".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	contains := findRels(entities, "CONTAINS")
	imports := findRels(entities, "IMPORTS")

	// File → workflow CONTAINS the two jobs.
	if !relExists(contains, "github_actions/workflow/CI", "github_actions/job/build") {
		t.Errorf("missing CONTAINS workflow→job(build); got %+v", contains)
	}
	if !relExists(contains, "github_actions/workflow/CI", "github_actions/job/lint") {
		t.Errorf("missing CONTAINS workflow→job(lint); got %+v", contains)
	}

	// job → step
	if !relExists(contains, "github_actions/job/build", "github_actions/step/Checkout") {
		t.Errorf("missing CONTAINS job(build)→step(Checkout); got %+v", contains)
	}

	// job → action (uses)
	if !relExists(contains, "github_actions/job/build", "github_actions/action/actions/checkout@v4") {
		t.Errorf("missing CONTAINS job(build)→action; got %+v", contains)
	}

	// IMPORTS: workflow file imports each unique action.
	if !relExists(imports, ".github/workflows/ci.yml", "actions/checkout@v4") {
		t.Errorf("missing IMPORTS file→actions/checkout@v4; got %+v", imports)
	}
	if !relExists(imports, ".github/workflows/ci.yml", "actions/setup-go@v5") {
		t.Errorf("missing IMPORTS file→actions/setup-go@v5; got %+v", imports)
	}
	if !relExists(imports, ".github/workflows/ci.yml", "golangci/golangci-lint-action@v4") {
		t.Errorf("missing IMPORTS file→golangci-lint-action; got %+v", imports)
	}

	// Language tagging.
	for _, r := range append(contains, imports...) {
		if r.Properties["language"] != "yaml" {
			t.Errorf("relationship %+v missing language=yaml tag", r)
		}
	}
}

// ---------------------------------------------------------------------------
// Docker Compose
// ---------------------------------------------------------------------------

func TestYAML_Compose_ContainsAndDependsOn(t *testing.T) {
	src := []byte(`version: "3.9"
services:
  api:
    image: myapp:latest
    ports:
      - "8080:8080"
    depends_on:
      - db
      - redis
  db:
    image: postgres:15
  redis:
    image: redis:7
volumes:
  data:
`)
	entities, err := extractYAML(src, "docker-compose.yml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	contains := findRels(entities, "CONTAINS")
	imports := findRels(entities, "IMPORTS")

	// File → service CONTAINS edges.
	for _, svc := range []string{"api", "db", "redis"} {
		if !relExists(contains, "docker-compose.yml", "docker_compose/service/"+svc) {
			t.Errorf("missing CONTAINS file→service(%s); got %+v", svc, contains)
		}
	}

	// File → volume CONTAINS edge.
	if !relExists(contains, "docker-compose.yml", "docker_compose/volume/data") {
		t.Errorf("missing CONTAINS file→volume(data); got %+v", contains)
	}

	// service → port (api → 8080).
	if !relExists(contains, "docker_compose/service/api", "docker_compose/port/api/\"8080:8080\"") {
		// Ports values may carry quotes — be lenient and just check prefix-match.
		found := false
		for _, r := range contains {
			if r.FromID == "docker_compose/service/api" &&
				r.Kind == "CONTAINS" &&
				len(r.ToID) > len("docker_compose/port/api/") &&
				r.ToID[:len("docker_compose/port/api/")] == "docker_compose/port/api/" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing CONTAINS api→port; got %+v", contains)
		}
	}

	// IMPORTS: api depends_on db and redis.
	if !relExists(imports, "docker_compose/service/api", "docker_compose/service/db") {
		t.Errorf("missing IMPORTS api→db; got %+v", imports)
	}
	if !relExists(imports, "docker_compose/service/api", "docker_compose/service/redis") {
		t.Errorf("missing IMPORTS api→redis; got %+v", imports)
	}

	// Language tagging.
	for _, r := range append(contains, imports...) {
		if r.Properties["language"] != "yaml" {
			t.Errorf("relationship %+v missing language=yaml tag", r)
		}
	}
}

// ---------------------------------------------------------------------------
// Kubernetes
// ---------------------------------------------------------------------------

func TestYAML_K8s_Contains(t *testing.T) {
	src := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  template:
    spec:
      containers:
        - name: app
          image: nginx
        - name: sidecar
          image: envoy
`)
	entities, err := extractYAML(src, "deploy.yml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	contains := findRels(entities, "CONTAINS")

	// File → resource.
	if !relExists(contains, "deploy.yml", "k8s/resource/web") {
		t.Errorf("missing CONTAINS file→resource(web); got %+v", contains)
	}

	// resource → containers.
	if !relExists(contains, "k8s/resource/web", "k8s/container/app") {
		t.Errorf("missing CONTAINS web→container(app); got %+v", contains)
	}
	if !relExists(contains, "k8s/resource/web", "k8s/container/sidecar") {
		t.Errorf("missing CONTAINS web→container(sidecar); got %+v", contains)
	}
}

// ---------------------------------------------------------------------------
// GitLab CI
// ---------------------------------------------------------------------------

func TestYAML_GitLabCI_Contains(t *testing.T) {
	src := []byte(`stages:
  - build
  - test

build_job:
  stage: build
  script:
    - echo building

test_job:
  stage: test
  script:
    - echo testing
`)
	entities, err := extractYAML(src, ".gitlab-ci.yml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	contains := findRels(entities, "CONTAINS")

	if !relExists(contains, ".gitlab-ci.yml", "gitlab_ci/job/build_job") {
		t.Errorf("missing CONTAINS file→job(build_job); got %+v", contains)
	}
	if !relExists(contains, ".gitlab-ci.yml", "gitlab_ci/job/test_job") {
		t.Errorf("missing CONTAINS file→job(test_job); got %+v", contains)
	}
}

// ---------------------------------------------------------------------------
// Ansible
// ---------------------------------------------------------------------------

func TestYAML_Ansible_Contains(t *testing.T) {
	src := []byte(`---
- name: Configure web
  hosts: webservers
  tasks:
    - name: install nginx
      apt: name=nginx
    - name: start nginx
      service: name=nginx state=started
`)
	entities, err := extractYAML(src, "playbook.yml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	contains := findRels(entities, "CONTAINS")

	if !relExists(contains, "ansible/play/Configure web", "ansible/task/install nginx") {
		t.Errorf("missing CONTAINS play→task(install nginx); got %+v", contains)
	}
	if !relExists(contains, "ansible/play/Configure web", "ansible/task/start nginx") {
		t.Errorf("missing CONTAINS play→task(start nginx); got %+v", contains)
	}
}

// ---------------------------------------------------------------------------
// Issue #424 — manifest IMPORTS for image refs and host-path volume mounts.
// ---------------------------------------------------------------------------

func TestYAML_Compose_ImageImports(t *testing.T) {
	src := []byte(`version: "3.9"
services:
  api:
    image: myapp:latest
  db:
    image: postgres:15
  cache:
    image: redis:alpine
`)
	entities, err := extractYAML(src, "docker-compose.yml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	imports := findRels(entities, "IMPORTS")

	cases := map[string]string{
		"api":   "docker_image:myapp:latest",
		"db":    "docker_image:postgres:15",
		"cache": "docker_image:redis:alpine",
	}
	for svc, want := range cases {
		from := "docker_compose/service/" + svc
		if !relExists(imports, from, want) {
			t.Errorf("missing IMPORTS %s→%s; got %+v", from, want, imports)
		}
	}
}

func TestYAML_Compose_HostPathMountImports(t *testing.T) {
	src := []byte(`version: "3.9"
services:
  api:
    image: myapp:latest
    volumes:
      - ./src:/app/src
      - ../shared:/app/shared
      - /etc/myapp:/etc/myapp:ro
      - ${PWD}/data:/data
      - data_volume:/var/lib/data
volumes:
  data_volume:
`)
	entities, err := extractYAML(src, "docker-compose.yml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	imports := findRels(entities, "IMPORTS")
	from := "docker_compose/service/api"
	for _, want := range []string{
		"host_path:./src",
		"host_path:../shared",
		"host_path:/etc/myapp",
		"host_path:${PWD}/data",
	} {
		if !relExists(imports, from, want) {
			t.Errorf("missing IMPORTS %s→%s; got %+v", from, want, imports)
		}
	}
	// Named-volume sources MUST NOT be classified as host paths.
	for _, r := range imports {
		if r.FromID == from && r.ToID == "host_path:data_volume" {
			t.Errorf("named-volume source emitted as host_path: %+v", r)
		}
	}
}

func TestYAML_K8s_ContainerImageImports(t *testing.T) {
	src := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  template:
    spec:
      initContainers:
        - name: migrate
          image: migrate:latest
      containers:
        - name: app
          image: nginx:1.21
        - name: sidecar
          image: envoy:v1.29.0
`)
	entities, err := extractYAML(src, "deploy.yml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	imports := findRels(entities, "IMPORTS")

	cases := map[string]string{
		"k8s/container/app":          "docker_image:nginx:1.21",
		"k8s/container/sidecar":      "docker_image:envoy:v1.29.0",
		"k8s/init-container/migrate": "docker_image:migrate:latest",
	}
	for from, want := range cases {
		if !relExists(imports, from, want) {
			t.Errorf("missing IMPORTS %s→%s; got %+v", from, want, imports)
		}
	}
}
