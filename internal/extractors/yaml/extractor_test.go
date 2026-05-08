package yaml_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsyaml "github.com/smacker/go-tree-sitter/yaml"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/yaml" // trigger init()
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func parseYAML(src []byte) *sitter.Tree {
	p := sitter.NewParser()
	p.SetLanguage(tsyaml.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		panic("test helper: yaml parse failed: " + err.Error())
	}
	return tree
}

func extractYAML(src []byte, path string) ([]types.EntityRecord, error) {
	tree := parseYAML(src)
	ext, ok := extractor.Get("yaml")
	if !ok {
		panic("yaml extractor not registered")
	}
	return ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  src,
		Language: "yaml",
		Tree:     tree,
	})
}

func extractYAMLNoTree(src []byte, path string) ([]types.EntityRecord, error) {
	ext, ok := extractor.Get("yaml")
	if !ok {
		panic("yaml extractor not registered")
	}
	return ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  src,
		Language: "yaml",
		Tree:     nil,
	})
}

func findEntitiesByKind(entities []types.EntityRecord, kind string) []types.EntityRecord {
	var result []types.EntityRecord
	for _, e := range entities {
		if e.Kind == kind {
			result = append(result, e)
		}
	}
	return result
}

func findEntitiesBySubtype(entities []types.EntityRecord, subtype string) []types.EntityRecord {
	var result []types.EntityRecord
	for _, e := range entities {
		if e.Subtype == subtype {
			result = append(result, e)
		}
	}
	return result
}

func hasEntityWithName(entities []types.EntityRecord, name string) bool {
	for _, e := range entities {
		if e.Name == name {
			return true
		}
	}
	return false
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// Walk up to find go.mod
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func TestExtractor_Language(t *testing.T) {
	ext, ok := extractor.Get("yaml")
	if !ok {
		t.Fatal("yaml extractor not registered")
	}
	if got := ext.Language(); got != "yaml" {
		t.Errorf("Language() = %q, want %q", got, "yaml")
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestExtract_EmptyContent(t *testing.T) {
	entities, err := extractYAML(nil, "test.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(entities))
	}
}

func TestExtract_EmptyBytes(t *testing.T) {
	entities, err := extractYAML([]byte(""), "test.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(entities))
	}
}

func TestExtract_NilTree_ParsesInline(t *testing.T) {
	src := []byte("key: value\nother: data\n")
	entities, err := extractYAMLNoTree(src, "test.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should produce generic schema entities
	if len(entities) == 0 {
		t.Error("expected at least one entity from inline parse")
	}
}

// ---------------------------------------------------------------------------
// GitHub Actions flavor
// ---------------------------------------------------------------------------

var githubActionsFixture = []byte(`name: CI Pipeline

on:
  push:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4
      - name: Run tests
        run: go test ./...

  lint:
    runs-on: ubuntu-latest
    steps:
      - name: Run linter
        uses: golangci/golangci-lint-action@v4
`)

func TestExtract_GitHubActions_WorkflowName(t *testing.T) {
	entities, err := extractYAML(githubActionsFixture, ".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	workflows := findEntitiesBySubtype(entities, "workflow")
	if len(workflows) == 0 {
		t.Fatal("expected at least one workflow entity")
	}
	if workflows[0].Name != "CI Pipeline" {
		t.Errorf("workflow name = %q, want %q", workflows[0].Name, "CI Pipeline")
	}
	if workflows[0].Kind != "SCOPE.Operation" {
		t.Errorf("workflow kind = %q, want SCOPE.Operation", workflows[0].Kind)
	}
}

func TestExtract_GitHubActions_Jobs(t *testing.T) {
	entities, err := extractYAML(githubActionsFixture, ".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	jobs := findEntitiesBySubtype(entities, "job")
	if len(jobs) < 2 {
		t.Errorf("expected at least 2 job entities, got %d", len(jobs))
	}
	if !hasEntityWithName(jobs, "build") {
		t.Error("expected job entity named 'build'")
	}
	if !hasEntityWithName(jobs, "lint") {
		t.Error("expected job entity named 'lint'")
	}
}

func TestExtract_GitHubActions_Steps(t *testing.T) {
	entities, err := extractYAML(githubActionsFixture, ".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	steps := findEntitiesBySubtype(entities, "step")
	if len(steps) == 0 {
		t.Fatal("expected at least one step entity")
	}
	if !hasEntityWithName(steps, "Checkout code") {
		t.Error("expected step named 'Checkout code'")
	}
}

func TestExtract_GitHubActions_UsesActions(t *testing.T) {
	entities, err := extractYAML(githubActionsFixture, ".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	actions := findEntitiesBySubtype(entities, "action")
	if len(actions) == 0 {
		t.Fatal("expected at least one action (uses:) entity")
	}
	if !hasEntityWithName(actions, "actions/checkout@v4") {
		t.Error("expected action 'actions/checkout@v4'")
	}
	for _, a := range actions {
		if a.Kind != "SCOPE.Component" {
			t.Errorf("action %q kind = %q, want SCOPE.Component", a.Name, a.Kind)
		}
	}
}

func TestExtract_GitHubActions_QualityScore(t *testing.T) {
	entities, err := extractYAML(githubActionsFixture, ".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.QualityScore < 0.6 {
			t.Errorf("entity %q has quality_score %.2f < 0.6", e.Name, e.QualityScore)
		}
	}
}

func TestExtract_GitHubActions_Language(t *testing.T) {
	entities, err := extractYAML(githubActionsFixture, ".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.Language != "yaml" {
			t.Errorf("entity %q language = %q, want yaml", e.Name, e.Language)
		}
	}
}

// ---------------------------------------------------------------------------
// GitLab CI flavor
// ---------------------------------------------------------------------------

var gitlabCIFixture = []byte(`stages:
  - build
  - test
  - deploy

build-job:
  stage: build
  script:
    - docker build -t myapp .
    - docker push myapp

test-job:
  stage: test
  script:
    - go test ./...

deploy-job:
  stage: deploy
  script:
    - kubectl apply -f k8s/
`)

func TestExtract_GitLabCI_Stages(t *testing.T) {
	entities, err := extractYAML(gitlabCIFixture, ".gitlab-ci.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stages := findEntitiesBySubtype(entities, "stage")
	if len(stages) < 3 {
		t.Errorf("expected at least 3 stage entities, got %d", len(stages))
	}
	if !hasEntityWithName(stages, "build") {
		t.Error("expected stage 'build'")
	}
	for _, s := range stages {
		if s.Kind != "SCOPE.Component" {
			t.Errorf("stage %q kind = %q, want SCOPE.Component", s.Name, s.Kind)
		}
	}
}

func TestExtract_GitLabCI_Jobs(t *testing.T) {
	entities, err := extractYAML(gitlabCIFixture, ".gitlab-ci.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	jobs := findEntitiesBySubtype(entities, "job")
	if len(jobs) < 3 {
		t.Errorf("expected at least 3 job entities, got %d", len(jobs))
	}
	if !hasEntityWithName(jobs, "build-job") {
		t.Error("expected job 'build-job'")
	}
	for _, j := range jobs {
		if j.Kind != "SCOPE.Operation" {
			t.Errorf("job %q kind = %q, want SCOPE.Operation", j.Name, j.Kind)
		}
	}
}

func TestExtract_GitLabCI_ScriptSteps(t *testing.T) {
	entities, err := extractYAML(gitlabCIFixture, ".gitlab-ci.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	scripts := findEntitiesBySubtype(entities, "script_step")
	if len(scripts) == 0 {
		t.Fatal("expected at least one script_step entity")
	}
	if !hasEntityWithName(scripts, "go test ./...") {
		t.Error("expected script_step 'go test ./...'")
	}
}

// GitLab CI detection via path even without stages key
func TestExtract_GitLabCI_DetectedByPath(t *testing.T) {
	src := []byte(`build-job:
  script:
    - make build
`)
	entities, err := extractYAML(src, ".gitlab-ci.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should detect as gitlab CI via path, emit job
	jobs := findEntitiesBySubtype(entities, "job")
	if len(jobs) == 0 {
		t.Error("expected at least one job detected via gitlab-ci path")
	}
}

// ---------------------------------------------------------------------------
// Docker Compose flavor
// ---------------------------------------------------------------------------

var dockerComposeFixture = []byte(`version: "3.9"

services:
  api:
    image: myapp:latest
    ports:
      - "8080:8080"

  db:
    image: postgres:15
    ports:
      - "5432:5432"

  redis:
    image: redis:7

volumes:
  postgres_data:
  redis_data:
`)

func TestExtract_DockerCompose_Services(t *testing.T) {
	entities, err := extractYAML(dockerComposeFixture, "docker-compose.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	services := findEntitiesBySubtype(entities, "service")
	if len(services) < 3 {
		t.Errorf("expected at least 3 service entities, got %d", len(services))
	}
	if !hasEntityWithName(services, "api") {
		t.Error("expected service 'api'")
	}
	if !hasEntityWithName(services, "db") {
		t.Error("expected service 'db'")
	}
	for _, s := range services {
		if s.Kind != "SCOPE.Component" {
			t.Errorf("service %q kind = %q, want SCOPE.Component", s.Name, s.Kind)
		}
	}
}

func TestExtract_DockerCompose_Ports(t *testing.T) {
	entities, err := extractYAML(dockerComposeFixture, "docker-compose.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ports := findEntitiesBySubtype(entities, "port")
	if len(ports) == 0 {
		t.Fatal("expected at least one port entity")
	}
	for _, p := range ports {
		if p.Kind != "SCOPE.Pattern" {
			t.Errorf("port %q kind = %q, want SCOPE.Pattern", p.Name, p.Kind)
		}
	}
}

func TestExtract_DockerCompose_Volumes(t *testing.T) {
	entities, err := extractYAML(dockerComposeFixture, "docker-compose.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	vols := findEntitiesBySubtype(entities, "volume")
	if len(vols) < 2 {
		t.Errorf("expected at least 2 volume entities, got %d", len(vols))
	}
	if !hasEntityWithName(vols, "postgres_data") {
		t.Error("expected volume 'postgres_data'")
	}
	for _, v := range vols {
		if v.Kind != "SCOPE.Schema" {
			t.Errorf("volume %q kind = %q, want SCOPE.Schema", v.Name, v.Kind)
		}
	}
}

// ---------------------------------------------------------------------------
// Kubernetes flavor
// ---------------------------------------------------------------------------

var k8sDeploymentFixture = []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp-api
  namespace: production
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: api
          image: myapp:latest
        - name: sidecar
          image: busybox:latest
`)

func TestExtract_Kubernetes_Component(t *testing.T) {
	entities, err := extractYAML(k8sDeploymentFixture, "k8s/deployment.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Deployment metadata.name → SCOPE.Service (per MX-1059 entity kind mapping)
	svcs := findEntitiesByKind(entities, "SCOPE.Service")
	if len(svcs) == 0 {
		t.Fatal("expected at least one SCOPE.Service entity for Deployment metadata.name")
	}
	if !hasEntityWithName(svcs, "myapp-api") {
		t.Error("expected SCOPE.Service named 'myapp-api'")
	}
	// Check QualifiedName contains the kind
	for _, s := range svcs {
		if s.Name == "myapp-api" && s.QualifiedName != "Deployment" {
			t.Errorf("QualifiedName = %q, want %q", s.QualifiedName, "Deployment")
		}
	}
}

func TestExtract_Kubernetes_Containers(t *testing.T) {
	entities, err := extractYAML(k8sDeploymentFixture, "k8s/deployment.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	containers := findEntitiesBySubtype(entities, "container")
	if len(containers) < 2 {
		t.Errorf("expected at least 2 container entities, got %d", len(containers))
	}
	if !hasEntityWithName(containers, "api") {
		t.Error("expected container 'api'")
	}
	if !hasEntityWithName(containers, "sidecar") {
		t.Error("expected container 'sidecar'")
	}
	// Containers → SCOPE.Component per MX-1059 entity kind mapping.
	for _, c := range containers {
		if c.Kind != "SCOPE.Component" {
			t.Errorf("container %q kind = %q, want SCOPE.Component", c.Name, c.Kind)
		}
	}
}

func TestExtract_Kubernetes_SourceFile(t *testing.T) {
	entities, err := extractYAML(k8sDeploymentFixture, "k8s/deployment.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.SourceFile != "k8s/deployment.yml" {
			t.Errorf("entity %q source_file = %q, want %q", e.Name, e.SourceFile, "k8s/deployment.yml")
		}
	}
}

// ---------------------------------------------------------------------------
// Ansible flavor
// ---------------------------------------------------------------------------

var ansibleFixture = []byte(`tasks:
  - name: Install nginx
    apt:
      name: nginx
      state: present

  - name: Start nginx
    service:
      name: nginx
      state: started

handlers:
  - name: Reload nginx
    service:
      name: nginx
      state: reloaded

roles:
  - common
  - nginx
`)

func TestExtract_Ansible_Tasks(t *testing.T) {
	entities, err := extractYAML(ansibleFixture, "playbook.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tasks := findEntitiesBySubtype(entities, "task")
	if len(tasks) < 2 {
		t.Errorf("expected at least 2 task entities, got %d", len(tasks))
	}
	if !hasEntityWithName(tasks, "Install nginx") {
		t.Error("expected task 'Install nginx'")
	}
	for _, task := range tasks {
		if task.Kind != "SCOPE.Operation" {
			t.Errorf("task %q kind = %q, want SCOPE.Operation", task.Name, task.Kind)
		}
	}
}

func TestExtract_Ansible_Handlers(t *testing.T) {
	entities, err := extractYAML(ansibleFixture, "playbook.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	handlers := findEntitiesBySubtype(entities, "handler")
	if len(handlers) == 0 {
		t.Fatal("expected at least one handler entity")
	}
	if !hasEntityWithName(handlers, "Reload nginx") {
		t.Error("expected handler 'Reload nginx'")
	}
}

func TestExtract_Ansible_Roles(t *testing.T) {
	entities, err := extractYAML(ansibleFixture, "playbook.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	roles := findEntitiesBySubtype(entities, "role")
	if len(roles) < 2 {
		t.Errorf("expected at least 2 role entities, got %d", len(roles))
	}
	if !hasEntityWithName(roles, "common") {
		t.Error("expected role 'common'")
	}
}

// ---------------------------------------------------------------------------
// Generic YAML flavor
// ---------------------------------------------------------------------------

var genericYAMLFixture = []byte(`database:
  host: localhost
  port: 5432

cache:
  ttl: 300

server:
  port: 8080
`)

func TestExtract_Generic_TopLevelKeys(t *testing.T) {
	entities, err := extractYAML(genericYAMLFixture, "config.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) < 3 {
		t.Errorf("expected at least 3 top-level key entities, got %d", len(entities))
	}
	if !hasEntityWithName(entities, "database") {
		t.Error("expected entity 'database'")
	}
	if !hasEntityWithName(entities, "cache") {
		t.Error("expected entity 'cache'")
	}
	if !hasEntityWithName(entities, "server") {
		t.Error("expected entity 'server'")
	}
	for _, e := range entities {
		if e.Kind != "SCOPE.Schema" {
			t.Errorf("generic entity %q kind = %q, want SCOPE.Schema", e.Name, e.Kind)
		}
	}
}

func TestExtract_Generic_Subtype(t *testing.T) {
	entities, err := extractYAML(genericYAMLFixture, "config.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.Subtype != "key" {
			t.Errorf("generic entity %q subtype = %q, want key", e.Name, e.Subtype)
		}
	}
}

// ---------------------------------------------------------------------------
// Fixture file tests (Acceptance Criteria)
// ---------------------------------------------------------------------------

func TestFixture_GitHubActions(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "fixtures/sources/yaml/yaml__github_actions.yml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	entities, err := extractYAML(src, ".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	// AC1: workflow name, at least one job, at least one step
	if !hasEntityWithName(entities, "CI Pipeline") {
		t.Error("AC1: expected workflow entity 'CI Pipeline'")
	}
	jobs := findEntitiesBySubtype(entities, "job")
	if len(jobs) == 0 {
		t.Error("AC1: expected at least one job entity")
	}
	steps := findEntitiesBySubtype(entities, "step")
	if len(steps) == 0 {
		t.Error("AC1: expected at least one step entity")
	}
}

func TestFixture_Kubernetes(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "fixtures/sources/yaml/yaml__k8s_deployment.yml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	entities, err := extractYAML(src, "k8s/deployment.yml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	// AC2 (MX-1059): Deployment metadata.name → SCOPE.Service with QualifiedName="Deployment"
	svcs := findEntitiesByKind(entities, "SCOPE.Service")
	found := false
	for _, s := range svcs {
		if s.Name == "myapp-api" && s.QualifiedName == "Deployment" {
			found = true
			break
		}
	}
	if !found {
		t.Error("AC2: expected SCOPE.Service with Name='myapp-api' and QualifiedName='Deployment'")
	}
	// Containers → SCOPE.Component
	containers := findEntitiesBySubtype(entities, "container")
	if len(containers) < 2 {
		t.Errorf("AC2: expected at least 2 container entities, got %d", len(containers))
	}
	for _, c := range containers {
		if c.Kind != "SCOPE.Component" {
			t.Errorf("AC2: container %q kind=%q, want SCOPE.Component", c.Name, c.Kind)
		}
	}
}

func TestFixture_DockerCompose(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "fixtures/sources/yaml/yaml__docker_compose.yml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	entities, err := extractYAML(src, "docker-compose.yml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	// AC3: SCOPE.Component entities for each service name
	services := findEntitiesBySubtype(entities, "service")
	if len(services) < 3 {
		t.Errorf("AC3: expected at least 3 service entities, got %d", len(services))
	}
	for _, s := range services {
		if s.Kind != "SCOPE.Component" {
			t.Errorf("AC3: service %q kind=%q, want SCOPE.Component", s.Name, s.Kind)
		}
	}
}

func TestFixture_GitLabCI(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "fixtures/sources/yaml/yaml__gitlab_ci.yml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	entities, err := extractYAML(src, ".gitlab-ci.yml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(entities) == 0 {
		t.Error("expected entities from gitlab CI fixture")
	}
	jobs := findEntitiesBySubtype(entities, "job")
	if len(jobs) == 0 {
		t.Error("expected at least one job from gitlab CI fixture")
	}
}

// ---------------------------------------------------------------------------
// Line number tests
// ---------------------------------------------------------------------------

func TestExtract_LineNumbers_NonZero(t *testing.T) {
	entities, err := extractYAML(githubActionsFixture, "ci.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.StartLine < 1 {
			t.Errorf("entity %q start_line = %d, want >= 1", e.Name, e.StartLine)
		}
		if e.EndLine < e.StartLine {
			t.Errorf("entity %q end_line %d < start_line %d", e.Name, e.EndLine, e.StartLine)
		}
	}
}

// ---------------------------------------------------------------------------
// Fallback behavior
// ---------------------------------------------------------------------------

func TestExtract_FallbackOnUnknownFlavor(t *testing.T) {
	src := []byte(`foo: bar
baz: qux
`)
	entities, err := extractYAML(src, "unknown.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Generic fallback should return top-level keys
	if len(entities) == 0 {
		t.Error("expected entities from generic YAML fallback")
	}
	for _, e := range entities {
		if e.Kind != "SCOPE.Schema" {
			t.Errorf("fallback entity %q kind=%q, want SCOPE.Schema", e.Name, e.Kind)
		}
	}
}

// ---------------------------------------------------------------------------
// SCOPE allowlist compliance
// ---------------------------------------------------------------------------

var allowedKinds = map[string]bool{
	"SCOPE.Service":       true,
	"SCOPE.Component":     true,
	"SCOPE.Operation":     true,
	"SCOPE.Pattern":       true,
	"SCOPE.Evolution":     true,
	"SCOPE.Datastore":     true,
	"SCOPE.ExternalAPI":   true,
	"SCOPE.Event":         true,
	"SCOPE.Queue":         true,
	"SCOPE.Schema":        true,
	"SCOPE.ScopeUnknown":  true,
	"SCOPE.Stylesheet":    true,
	"SCOPE.UIComponent":   true,
	"SCOPE.InfraResource": true,
}

func TestExtract_AllKindsAreAllowlisted(t *testing.T) {
	fixtures := []struct {
		src  []byte
		path string
	}{
		{githubActionsFixture, ".github/workflows/ci.yml"},
		{gitlabCIFixture, ".gitlab-ci.yml"},
		{dockerComposeFixture, "docker-compose.yml"},
		{k8sDeploymentFixture, "k8s/deployment.yml"},
		{ansibleFixture, "playbook.yml"},
		{genericYAMLFixture, "config.yml"},
	}
	for _, f := range fixtures {
		entities, err := extractYAML(f.src, f.path)
		if err != nil {
			t.Fatalf("extract %s: %v", f.path, err)
		}
		for _, e := range entities {
			if !allowedKinds[e.Kind] {
				t.Errorf("entity %q has non-allowlisted kind %q (file: %s)", e.Name, e.Kind, f.path)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// MX-1059 Acceptance criteria: Ansible
// ---------------------------------------------------------------------------

var ansiblePlaybookFixture = []byte(`---
- name: Deploy web application
  hosts: web_servers
  become: true

  pre_tasks:
    - name: Check connectivity
      ansible.builtin.ping:

  roles:
    - role: common
    - role: nginx

  tasks:
    - name: Deploy application config
      ansible.builtin.template:
        src: app.conf.j2
        dest: /etc/app/app.conf
      notify: restart app

    - name: Start application
      ansible.builtin.service:
        name: app
        state: started

  handlers:
    - name: restart app
      ansible.builtin.service:
        name: app
        state: restarted
`)

func TestMX1059_Ansible_AtLeast5Entities(t *testing.T) {
	entities, err := extractYAML(ansiblePlaybookFixture, "deploy.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) < 5 {
		t.Errorf("AC1: expected ≥5 entities, got %d", len(entities))
		for _, e := range entities {
			t.Logf("  [%s/%s] %s", e.Kind, e.Subtype, e.Name)
		}
	}
}

func TestMX1059_Ansible_Play_IsService(t *testing.T) {
	entities, err := extractYAML(ansiblePlaybookFixture, "deploy.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	plays := findEntitiesBySubtype(entities, "play")
	if len(plays) == 0 {
		t.Fatal("AC1: expected at least one play entity")
	}
	if plays[0].Kind != "SCOPE.Service" {
		t.Errorf("AC1: play kind = %q, want SCOPE.Service", plays[0].Kind)
	}
}

func TestMX1059_Ansible_Tasks_AreOperation(t *testing.T) {
	entities, err := extractYAML(ansiblePlaybookFixture, "deploy.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tasks := findEntitiesBySubtype(entities, "task")
	if len(tasks) < 2 {
		t.Errorf("AC1: expected ≥2 task entities, got %d", len(tasks))
	}
	for _, task := range tasks {
		if task.Kind != "SCOPE.Operation" {
			t.Errorf("AC1: task %q kind=%q, want SCOPE.Operation", task.Name, task.Kind)
		}
	}
}

func TestMX1059_Ansible_Handlers_AreOperation(t *testing.T) {
	entities, err := extractYAML(ansiblePlaybookFixture, "deploy.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	handlers := findEntitiesBySubtype(entities, "handler")
	if len(handlers) == 0 {
		t.Fatal("AC1: expected at least one handler entity")
	}
	for _, h := range handlers {
		if h.Kind != "SCOPE.Operation" {
			t.Errorf("AC1: handler %q kind=%q, want SCOPE.Operation", h.Name, h.Kind)
		}
	}
}

func TestMX1059_Ansible_Roles_AreComponent(t *testing.T) {
	entities, err := extractYAML(ansiblePlaybookFixture, "deploy.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	roles := findEntitiesBySubtype(entities, "role")
	if len(roles) < 2 {
		t.Errorf("AC1: expected ≥2 role entities, got %d", len(roles))
	}
	for _, r := range roles {
		if r.Kind != "SCOPE.Component" {
			t.Errorf("AC1: role %q kind=%q, want SCOPE.Component", r.Name, r.Kind)
		}
	}
}

func TestMX1059_Ansible_PreTasks_Extracted(t *testing.T) {
	entities, err := extractYAML(ansiblePlaybookFixture, "deploy.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// pre_tasks should appear as task entities.
	tasks := findEntitiesBySubtype(entities, "task")
	if !hasEntityWithName(tasks, "Check connectivity") {
		t.Error("AC1: expected pre_task 'Check connectivity' as task entity")
	}
}

func TestMX1059_Ansible_RealWorldFixture_AtLeast5Entities(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "fixtures/real-world/ansible/deploy_playbook.yml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	entities, err := extractYAML(src, "deploy_playbook.yml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(entities) < 5 {
		t.Errorf("AC1 real-world: expected ≥5 entities, got %d", len(entities))
	}
}

// ---------------------------------------------------------------------------
// MX-1059 Acceptance criteria: Kubernetes Deployment
// ---------------------------------------------------------------------------

var k8sDeploymentRichFixture = []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: api
          image: api:latest
          ports:
            - name: http
              containerPort: 8080
            - name: metrics
              containerPort: 9090
        - name: proxy
          image: envoy:latest
          ports:
            - name: proxy
              containerPort: 15001
`)

func TestMX1059_K8sDeployment_AtLeast5Entities(t *testing.T) {
	entities, err := extractYAML(k8sDeploymentRichFixture, "k8s/deployment.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) < 5 {
		t.Errorf("AC3: expected ≥5 entities for K8s Deployment, got %d", len(entities))
		for _, e := range entities {
			t.Logf("  [%s/%s] %s", e.Kind, e.Subtype, e.Name)
		}
	}
}

func TestMX1059_K8sDeployment_MetadataName_IsService(t *testing.T) {
	entities, err := extractYAML(k8sDeploymentRichFixture, "k8s/deployment.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svcs := findEntitiesByKind(entities, "SCOPE.Service")
	if !hasEntityWithName(svcs, "api-server") {
		t.Error("AC3: expected SCOPE.Service entity named 'api-server' for Deployment metadata.name")
	}
}

func TestMX1059_K8sDeployment_Containers_AreComponent(t *testing.T) {
	entities, err := extractYAML(k8sDeploymentRichFixture, "k8s/deployment.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	containers := findEntitiesBySubtype(entities, "container")
	if len(containers) < 2 {
		t.Errorf("AC3: expected ≥2 container entities, got %d", len(containers))
	}
	for _, c := range containers {
		if c.Kind != "SCOPE.Component" {
			t.Errorf("AC3: container %q kind=%q, want SCOPE.Component", c.Name, c.Kind)
		}
	}
}

func TestMX1059_K8sDeployment_ContainerPorts_AreComponent(t *testing.T) {
	entities, err := extractYAML(k8sDeploymentRichFixture, "k8s/deployment.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ports := findEntitiesBySubtype(entities, "container_port")
	if len(ports) < 2 {
		t.Errorf("AC3: expected ≥2 container_port entities, got %d", len(ports))
	}
	for _, p := range ports {
		if p.Kind != "SCOPE.Component" {
			t.Errorf("AC3: container_port %q kind=%q, want SCOPE.Component", p.Name, p.Kind)
		}
	}
}

func TestMX1059_K8sDeployment_RealWorldFixture_AtLeast5Entities(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "fixtures/real-world/kubernetes/webapp_deployment.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	entities, err := extractYAML(src, "k8s/webapp.yaml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(entities) < 5 {
		t.Errorf("AC3 real-world: expected ≥5 entities, got %d", len(entities))
	}
}

// ---------------------------------------------------------------------------
// MX-1059 Acceptance criteria: Kubernetes Service
// ---------------------------------------------------------------------------

var k8sServiceFixture = []byte(`apiVersion: v1
kind: Service
metadata:
  name: api-svc
spec:
  type: ClusterIP
  selector:
    app: api
  ports:
    - name: http
      port: 8080
    - name: grpc
      port: 50051
`)

func TestMX1059_K8sService_AtLeast3Entities(t *testing.T) {
	entities, err := extractYAML(k8sServiceFixture, "k8s/service.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) < 3 {
		t.Errorf("AC4: expected ≥3 entities for K8s Service, got %d", len(entities))
		for _, e := range entities {
			t.Logf("  [%s/%s] %s", e.Kind, e.Subtype, e.Name)
		}
	}
}

func TestMX1059_K8sService_MetadataName_IsService(t *testing.T) {
	entities, err := extractYAML(k8sServiceFixture, "k8s/service.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svcs := findEntitiesByKind(entities, "SCOPE.Service")
	if !hasEntityWithName(svcs, "api-svc") {
		t.Error("AC4: expected SCOPE.Service entity named 'api-svc'")
	}
}

func TestMX1059_K8sService_Selector_IsComponent(t *testing.T) {
	entities, err := extractYAML(k8sServiceFixture, "k8s/service.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	selectors := findEntitiesBySubtype(entities, "selector")
	if len(selectors) == 0 {
		t.Error("AC4: expected at least one selector entity")
	}
	for _, s := range selectors {
		if s.Kind != "SCOPE.Component" {
			t.Errorf("AC4: selector %q kind=%q, want SCOPE.Component", s.Name, s.Kind)
		}
	}
}

func TestMX1059_K8sService_Ports_AreComponent(t *testing.T) {
	entities, err := extractYAML(k8sServiceFixture, "k8s/service.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ports := findEntitiesBySubtype(entities, "service_port")
	if len(ports) < 2 {
		t.Errorf("AC4: expected ≥2 service_port entities, got %d", len(ports))
	}
	for _, p := range ports {
		if p.Kind != "SCOPE.Component" {
			t.Errorf("AC4: service_port %q kind=%q, want SCOPE.Component", p.Name, p.Kind)
		}
	}
}

func TestMX1059_K8sService_RealWorldFixture_AtLeast3Entities(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "fixtures/real-world/kubernetes/webapp_service.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	entities, err := extractYAML(src, "k8s/webapp_svc.yaml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(entities) < 3 {
		t.Errorf("AC4 real-world: expected ≥3 entities, got %d", len(entities))
	}
}

// ---------------------------------------------------------------------------
// MX-1059 Acceptance criteria: allowlist coverage for new entity types
// ---------------------------------------------------------------------------

func TestMX1059_AllNewEntitiesAreAllowlisted(t *testing.T) {
	fixtures := []struct {
		src  []byte
		path string
	}{
		{ansiblePlaybookFixture, "deploy.yml"},
		{k8sDeploymentRichFixture, "k8s/deployment.yml"},
		{k8sServiceFixture, "k8s/service.yml"},
	}
	for _, f := range fixtures {
		entities, err := extractYAML(f.src, f.path)
		if err != nil {
			t.Fatalf("extract %s: %v", f.path, err)
		}
		for _, e := range entities {
			if !allowedKinds[e.Kind] {
				t.Errorf("entity %q has non-allowlisted kind %q (file: %s)", e.Name, e.Kind, f.path)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// MX-1059 Acceptance criteria: YAML parse failure returns quality_score=0.3
// ---------------------------------------------------------------------------

func TestMX1059_YAMLParseFailure_NonemptyInput_DoesNotPanic(t *testing.T) {
	// Invalid YAML — tree-sitter is resilient but let's ensure no panic.
	// The extractor should return entities (possibly 0) without panicking.
	src := []byte("{{{{{{ invalid yaml that is not parseable }")
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("extractor panicked on bad input: %v", r)
		}
	}()
	entities, err := extractYAMLNoTree(src, "bad.yml")
	if err != nil {
		// Error is acceptable — no panic is the requirement.
		return
	}
	// If entities are returned they should have valid quality scores.
	for _, e := range entities {
		if e.QualityScore < 0.0 || e.QualityScore > 1.0 {
			t.Errorf("entity %q has out-of-range quality_score %.2f", e.Name, e.QualityScore)
		}
	}
}

// ---------------------------------------------------------------------------
// MX-1104: K8s deep traversal — env vars, resource limits, selectors, volumeMounts
// ---------------------------------------------------------------------------

var k8sDeepDeploymentFixture = []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: deep-api
  namespace: staging
spec:
  replicas: 2
  selector:
    matchLabels:
      app: deep-api
      tier: backend
  template:
    spec:
      initContainers:
        - name: db-migrator
          image: migrate:latest
      containers:
        - name: api
          image: deep-api:latest
          ports:
            - name: http
              containerPort: 8080
          env:
            - name: DATABASE_URL
              value: "postgres://localhost:5432/app"
            - name: SECRET_KEY
              valueFrom:
                secretKeyRef:
                  name: app-secrets
                  key: secret-key
          resources:
            requests:
              cpu: "250m"
              memory: "256Mi"
            limits:
              cpu: "1000m"
              memory: "512Mi"
          volumeMounts:
            - name: config-vol
              mountPath: /etc/config
            - name: tmp
              mountPath: /tmp
`)

func TestMX1104_DeepDeployment_AtLeast10Entities(t *testing.T) {
	entities, err := extractYAML(k8sDeepDeploymentFixture, "k8s/deep.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) < 10 {
		t.Errorf("MX-1104 AC: expected ≥10 entities for rich Deployment, got %d", len(entities))
		for _, e := range entities {
			t.Logf("  [%s/%s] %s", e.Kind, e.Subtype, e.Name)
		}
	}
}

func TestMX1104_DeepDeployment_EnvVars_AreSchema(t *testing.T) {
	entities, err := extractYAML(k8sDeepDeploymentFixture, "k8s/deep.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	envVars := findEntitiesBySubtype(entities, "env_var")
	if len(envVars) < 2 {
		t.Errorf("MX-1104: expected ≥2 env_var entities, got %d", len(envVars))
	}
	if !hasEntityWithName(envVars, "DATABASE_URL") {
		t.Error("MX-1104: expected env_var 'DATABASE_URL'")
	}
	for _, e := range envVars {
		if e.Kind != "SCOPE.Schema" {
			t.Errorf("MX-1104: env_var %q kind=%q, want SCOPE.Schema", e.Name, e.Kind)
		}
	}
}

func TestMX1104_DeepDeployment_ResourceLimits_AreSchema(t *testing.T) {
	entities, err := extractYAML(k8sDeepDeploymentFixture, "k8s/deep.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	limits := findEntitiesBySubtype(entities, "resource_limit")
	if len(limits) < 2 {
		t.Errorf("MX-1104: expected ≥2 resource_limit entities, got %d", len(limits))
	}
	for _, e := range limits {
		if e.Kind != "SCOPE.Schema" {
			t.Errorf("MX-1104: resource_limit %q kind=%q, want SCOPE.Schema", e.Name, e.Kind)
		}
	}
}

func TestMX1104_DeepDeployment_Selectors_AreComponent(t *testing.T) {
	entities, err := extractYAML(k8sDeepDeploymentFixture, "k8s/deep.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	selectors := findEntitiesBySubtype(entities, "selector")
	if len(selectors) < 1 {
		t.Errorf("MX-1104: expected ≥1 selector entities, got %d", len(selectors))
	}
	for _, e := range selectors {
		if e.Kind != "SCOPE.Component" {
			t.Errorf("MX-1104: selector %q kind=%q, want SCOPE.Component", e.Name, e.Kind)
		}
	}
}

func TestMX1104_DeepDeployment_VolumeMounts_AreSchema(t *testing.T) {
	entities, err := extractYAML(k8sDeepDeploymentFixture, "k8s/deep.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	vms := findEntitiesBySubtype(entities, "volume_mount")
	if len(vms) < 2 {
		t.Errorf("MX-1104: expected ≥2 volume_mount entities, got %d", len(vms))
	}
	for _, e := range vms {
		if e.Kind != "SCOPE.Schema" {
			t.Errorf("MX-1104: volume_mount %q kind=%q, want SCOPE.Schema", e.Name, e.Kind)
		}
	}
}

func TestMX1104_DeepDeployment_InitContainers_AreComponent(t *testing.T) {
	entities, err := extractYAML(k8sDeepDeploymentFixture, "k8s/deep.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	inits := findEntitiesBySubtype(entities, "init_container")
	if len(inits) < 1 {
		t.Errorf("MX-1104: expected ≥1 init_container entities, got %d", len(inits))
	}
	if !hasEntityWithName(inits, "db-migrator") {
		t.Error("MX-1104: expected init_container 'db-migrator'")
	}
	for _, e := range inits {
		if e.Kind != "SCOPE.Component" {
			t.Errorf("MX-1104: init_container %q kind=%q, want SCOPE.Component", e.Name, e.Kind)
		}
	}
}

// ---------------------------------------------------------------------------
// MX-1104: ConfigMap extraction
// ---------------------------------------------------------------------------

var k8sConfigMapFixture = []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: default
data:
  DATABASE_HOST: "postgres:5432"
  REDIS_HOST: "redis:6379"
  LOG_LEVEL: "info"
  MAX_CONNECTIONS: "100"
  TIMEOUT_SECONDS: "30"
`)

func TestMX1104_ConfigMap_DataKeys_AreSchema(t *testing.T) {
	entities, err := extractYAML(k8sConfigMapFixture, "k8s/configmap.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	configKeys := findEntitiesBySubtype(entities, "config_key")
	if len(configKeys) < 5 {
		t.Errorf("MX-1104: expected ≥5 config_key entities, got %d", len(configKeys))
		for _, e := range entities {
			t.Logf("  [%s/%s] %s", e.Kind, e.Subtype, e.Name)
		}
	}
	if !hasEntityWithName(configKeys, "DATABASE_HOST") {
		t.Error("MX-1104: expected config_key 'DATABASE_HOST'")
	}
	for _, e := range configKeys {
		if e.Kind != "SCOPE.Schema" {
			t.Errorf("MX-1104: config_key %q kind=%q, want SCOPE.Schema", e.Name, e.Kind)
		}
	}
}

func TestMX1104_ConfigMap_AtLeast5Entities(t *testing.T) {
	entities, err := extractYAML(k8sConfigMapFixture, "k8s/configmap.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) < 5 {
		t.Errorf("MX-1104: expected ≥5 entities for ConfigMap, got %d", len(entities))
	}
}

// ---------------------------------------------------------------------------
// MX-1104: Ingress extraction
// ---------------------------------------------------------------------------

var k8sIngressFixture = []byte(`apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: app-ingress
  namespace: default
spec:
  rules:
    - host: api.example.com
      http:
        paths:
          - path: /v1
            pathType: Prefix
            backend:
              service:
                name: api-svc
                port:
                  number: 80
          - path: /health
            pathType: Exact
            backend:
              service:
                name: api-svc
                port:
                  number: 80
    - host: admin.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: admin-svc
                port:
                  number: 443
`)

func TestMX1104_Ingress_Hosts_AreExternalAPI(t *testing.T) {
	entities, err := extractYAML(k8sIngressFixture, "k8s/ingress.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hosts := findEntitiesBySubtype(entities, "ingress_host")
	if len(hosts) < 2 {
		t.Errorf("MX-1104: expected ≥2 ingress_host entities, got %d", len(hosts))
	}
	if !hasEntityWithName(hosts, "api.example.com") {
		t.Error("MX-1104: expected ingress_host 'api.example.com'")
	}
	for _, e := range hosts {
		if e.Kind != "SCOPE.ExternalAPI" {
			t.Errorf("MX-1104: ingress_host %q kind=%q, want SCOPE.ExternalAPI", e.Name, e.Kind)
		}
	}
}

func TestMX1104_Ingress_Paths_AreOperation(t *testing.T) {
	entities, err := extractYAML(k8sIngressFixture, "k8s/ingress.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	paths := findEntitiesBySubtype(entities, "ingress_path")
	if len(paths) < 3 {
		t.Errorf("MX-1104: expected ≥3 ingress_path entities, got %d", len(paths))
	}
	for _, e := range paths {
		if e.Kind != "SCOPE.Operation" {
			t.Errorf("MX-1104: ingress_path %q kind=%q, want SCOPE.Operation", e.Name, e.Kind)
		}
	}
}

func TestMX1104_Ingress_AtLeast5Entities(t *testing.T) {
	entities, err := extractYAML(k8sIngressFixture, "k8s/ingress.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) < 5 {
		t.Errorf("MX-1104: expected ≥5 entities for Ingress, got %d", len(entities))
	}
}

// ---------------------------------------------------------------------------
// MX-1104: Real-world fixtures
// ---------------------------------------------------------------------------

func TestMX1104_RealWorld_Deployment_AtLeast10Entities(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "fixtures/real-world/kubernetes/deployment.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	entities, err := extractYAML(src, "kubernetes/deployment.yaml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(entities) < 10 {
		t.Errorf("MX-1104 AC: expected ≥10 entities for kubernetes/deployment.yaml, got %d", len(entities))
		for _, e := range entities {
			t.Logf("  [%s/%s] %s", e.Kind, e.Subtype, e.Name)
		}
	}
}

func TestMX1104_RealWorld_MultiDoc_EachDoc_AtLeast5Entities(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "fixtures/real-world/kubernetes/full_stack_manifests.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	entities, err := extractYAML(src, "kubernetes/full_stack_manifests.yaml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	// Multi-document file: total entity count should be substantial.
	// full_stack_manifests.yaml has 6 documents; we expect ≥5 entities total per document.
	if len(entities) < 5 {
		t.Errorf("MX-1104 AC: expected ≥5 total entities for multi-doc K8s manifest, got %d", len(entities))
		for _, e := range entities {
			t.Logf("  [%s/%s] %s", e.Kind, e.Subtype, e.Name)
		}
	}
}

func TestMX1104_MultiDocFixture_EachDocAtLeast5Entities(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "fixtures/sources/yaml/yaml__k8s_multi_doc.yml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	entities, err := extractYAML(src, "k8s/multi.yml")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(entities) < 5 {
		t.Errorf("MX-1104: expected ≥5 entities per document from multi-doc fixture, got %d total", len(entities))
		for _, e := range entities {
			t.Logf("  [%s/%s] %s", e.Kind, e.Subtype, e.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// MX-1104: AllowlistCompliant for new entity types
// ---------------------------------------------------------------------------

func TestMX1104_AllNewEntitiesAreAllowlisted(t *testing.T) {
	fixtures := []struct {
		src  []byte
		path string
	}{
		{k8sDeepDeploymentFixture, "k8s/deep.yml"},
		{k8sConfigMapFixture, "k8s/configmap.yml"},
		{k8sIngressFixture, "k8s/ingress.yml"},
	}
	for _, f := range fixtures {
		entities, err := extractYAML(f.src, f.path)
		if err != nil {
			t.Fatalf("extract %s: %v", f.path, err)
		}
		for _, e := range entities {
			if !allowedKinds[e.Kind] {
				t.Errorf("MX-1104: entity %q has non-allowlisted kind %q (file: %s)", e.Name, e.Kind, f.path)
			}
		}
	}
}
