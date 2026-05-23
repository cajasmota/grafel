package config

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// writeFixture writes name relative to dir with content. Helper for tests.
func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	abs := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", abs, err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
}

func runDiscover(t *testing.T, dir string, files []string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	ents, rels, err := Discover(context.Background(), dir, files)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	return ents, rels
}

// findBySource returns the first entity whose SourceFile matches.
func findBySource(es []types.EntityRecord, source string) *types.EntityRecord {
	for i := range es {
		if es[i].SourceFile == source {
			return &es[i]
		}
	}
	return nil
}

func TestDiscover_PyProjectToml(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "pyproject.toml", `
[project]
name = "client-fixture-de"
version = "0.1.0"
dependencies = [
  "fastapi>=0.110",
  "pydantic~=2.0",
  "celery[redis]>=5.3",
]

[tool.pytest.ini_options]
addopts = "-ra"
`)
	ents, rels := runDiscover(t, dir, []string{"pyproject.toml"})
	if len(ents) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(ents))
	}
	e := ents[0]
	if e.Kind != string(types.EntityKindConfig) {
		t.Errorf("Kind=%q want SCOPE.Config", e.Kind)
	}
	if e.Subtype != "python_project" {
		t.Errorf("Subtype=%q", e.Subtype)
	}
	if e.Properties["format"] != "toml" {
		t.Errorf("format=%q", e.Properties["format"])
	}
	deps := e.Properties["dependencies"]
	for _, want := range []string{"fastapi", "pydantic", "celery"} {
		if !strings.Contains(deps, want) {
			t.Errorf("dependencies missing %q: %q", want, deps)
		}
	}
	if len(rels) == 0 {
		t.Fatalf("expected DEPENDS_ON_CONFIG / CONFIGURES edges")
	}
	var sawDepends, sawConfigures bool
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindDependsOnConfig) {
			sawDepends = true
		}
		if r.Kind == string(types.RelationshipKindConfigures) {
			sawConfigures = true
		}
	}
	if !sawDepends || !sawConfigures {
		t.Errorf("missing edges: depends=%v configures=%v", sawDepends, sawConfigures)
	}
}

func TestDiscover_PackageJSON_ScriptsAndDeps(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "frontend/package.json", `{
  "name": "client-fixture-de-web",
  "version": "1.2.3",
  "scripts": { "dev": "vite", "build": "vite build", "lint": "eslint ." },
  "dependencies": { "react": "^18.2.0", "react-dom": "^18.2.0" },
  "devDependencies": { "vite": "^5.0.0", "typescript": "^5.0.0" }
}`)
	ents, _ := runDiscover(t, dir, []string{"frontend/package.json"})
	e := findBySource(ents, "frontend/package.json")
	if e == nil {
		t.Fatalf("entity not emitted")
	}
	if e.Properties["project_name"] != "client-fixture-de-web" {
		t.Errorf("project_name=%q", e.Properties["project_name"])
	}
	scripts := e.Properties["scripts"]
	for _, s := range []string{"build", "dev", "lint"} {
		if !strings.Contains(scripts, s) {
			t.Errorf("scripts missing %q: %q", s, scripts)
		}
	}
	deps := e.Properties["dependencies"]
	if !strings.Contains(deps, "react") || !strings.Contains(deps, "vite (dev)") {
		t.Errorf("dependencies wrong: %q", deps)
	}
}

func TestDiscover_PomXML(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "pom.xml", `<?xml version="1.0"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <groupId>com.client.fixturede</groupId>
  <artifactId>fixturede-api</artifactId>
  <version>0.1.0</version>
  <dependencies>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-web</artifactId>
      <version>3.2.0</version>
    </dependency>
    <dependency>
      <groupId>org.postgresql</groupId>
      <artifactId>postgresql</artifactId>
    </dependency>
  </dependencies>
</project>`)
	ents, _ := runDiscover(t, dir, []string{"pom.xml"})
	e := findBySource(ents, "pom.xml")
	if e == nil {
		t.Fatalf("pom entity missing")
	}
	if e.Subtype != "maven_project" {
		t.Errorf("subtype=%q", e.Subtype)
	}
	if e.Properties["project_name"] != "fixturede-api" {
		t.Errorf("project_name=%q", e.Properties["project_name"])
	}
	deps := e.Properties["dependencies"]
	if !strings.Contains(deps, "org.springframework.boot:spring-boot-starter-web") {
		t.Errorf("deps missing spring: %q", deps)
	}
	if !strings.Contains(deps, "org.postgresql:postgresql") {
		t.Errorf("deps missing postgresql: %q", deps)
	}
}

func TestDiscover_EnvNeverLeaksValues(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".env", `DATABASE_URL=postgres://user:supersecretpassword@host/db
API_KEY=abcdef-12345-SECRETKEY
export AWS_SECRET_ACCESS_KEY=very-secret-value
PORT=8080
`)
	writeFixture(t, dir, ".env.production", `STRIPE_KEY=sk_live_DONT_LEAK
SENTRY_DSN=https://secret@sentry.io/123
`)
	ents, _ := runDiscover(t, dir, []string{".env", ".env.production"})
	if len(ents) != 2 {
		t.Fatalf("expected 2 env entities, got %d", len(ents))
	}
	forbidden := []string{
		"supersecretpassword",
		"abcdef-12345-SECRETKEY",
		"very-secret-value",
		"sk_live_DONT_LEAK",
		"https://secret@sentry.io/123",
		"postgres://",
		"8080",
	}
	for _, e := range ents {
		if e.Subtype != "env_vars" {
			t.Errorf("subtype=%q want env_vars", e.Subtype)
		}
		if e.Properties["redaction"] != "names_only" {
			t.Errorf("missing redaction=names_only on %s", e.SourceFile)
		}
		// Check every property value for forbidden substrings.
		for k, v := range e.Properties {
			for _, bad := range forbidden {
				if strings.Contains(v, bad) {
					t.Errorf("SECURITY: env value leaked into Property %q on %s: %q", k, e.SourceFile, v)
				}
			}
		}
		// Names must be present though.
		names := e.Properties["keys_top_level"]
		if !strings.Contains(names, "DATABASE_URL") && !strings.Contains(names, "STRIPE_KEY") {
			t.Errorf("env names missing on %s: %q", e.SourceFile, names)
		}
	}
}

func TestDiscover_Dockerfile(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "Dockerfile", `FROM python:3.11-slim AS base
RUN apt-get update && apt-get install -y curl
COPY . /app
WORKDIR /app
ENV PYTHONUNBUFFERED=1
EXPOSE 8000
CMD ["python", "manage.py", "runserver", "0.0.0.0:8000"]
`)
	writeFixture(t, dir, "Dockerfile.dev", `FROM python:3.11-slim
RUN pip install pytest
`)
	ents, _ := runDiscover(t, dir, []string{"Dockerfile", "Dockerfile.dev"})
	if len(ents) != 2 {
		t.Fatalf("expected 2 dockerfile entities, got %d", len(ents))
	}
	for _, e := range ents {
		if e.Subtype != "docker_image" {
			t.Errorf("subtype=%q", e.Subtype)
		}
		if !strings.Contains(e.Properties["dependencies"], "python:3.11-slim") {
			t.Errorf("FROM not captured in %s: %q", e.SourceFile, e.Properties["dependencies"])
		}
	}
}

func TestDiscover_Makefile(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "Makefile", `.PHONY: build test lint

build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run

ci-deploy: build test
	./scripts/deploy.sh
`)
	ents, _ := runDiscover(t, dir, []string{"Makefile"})
	e := findBySource(ents, "Makefile")
	if e == nil {
		t.Fatalf("Makefile entity missing")
	}
	scripts := e.Properties["scripts"]
	for _, want := range []string{"build", "test", "lint", "ci-deploy"} {
		if !strings.Contains(scripts, want) {
			t.Errorf("scripts missing %q: %q", want, scripts)
		}
	}
}

func TestDiscover_GoMod(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "go.mod", `module github.com/cajasmota/example

go 1.22

require (
	github.com/spf13/cobra v1.8.0
	github.com/stretchr/testify v1.9.0
)

require github.com/google/uuid v1.5.0
`)
	ents, _ := runDiscover(t, dir, []string{"go.mod"})
	e := findBySource(ents, "go.mod")
	if e == nil {
		t.Fatalf("go.mod entity missing")
	}
	if e.Properties["project_name"] != "github.com/cajasmota/example" {
		t.Errorf("project_name=%q", e.Properties["project_name"])
	}
	deps := e.Properties["dependencies"]
	for _, want := range []string{"github.com/spf13/cobra", "github.com/stretchr/testify", "github.com/google/uuid"} {
		if !strings.Contains(deps, want) {
			t.Errorf("deps missing %q: %q", want, deps)
		}
	}
}

func TestDiscover_BuildGradle(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "build.gradle", `plugins {
    id 'org.springframework.boot' version '3.2.0'
    id 'java'
}

dependencies {
    implementation 'org.springframework.boot:spring-boot-starter-web'
    implementation "org.postgresql:postgresql:42.7.0"
    testImplementation 'org.junit.jupiter:junit-jupiter:5.10.0'
}
`)
	ents, _ := runDiscover(t, dir, []string{"build.gradle"})
	e := findBySource(ents, "build.gradle")
	if e == nil {
		t.Fatalf("build.gradle entity missing")
	}
	deps := e.Properties["dependencies"]
	if !strings.Contains(deps, "spring-boot-starter-web") {
		t.Errorf("deps missing spring: %q", deps)
	}
	plugins := e.Properties["plugins"]
	if !strings.Contains(plugins, "org.springframework.boot") {
		t.Errorf("plugins missing: %q", plugins)
	}
}

func TestDiscover_ApplicationProperties(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "src/main/resources/application.properties", `server.port=8080
spring.datasource.url=jdbc:postgresql://localhost/db
spring.jpa.hibernate.ddl-auto=update
`)
	ents, _ := runDiscover(t, dir, []string{"src/main/resources/application.properties"})
	e := findBySource(ents, "src/main/resources/application.properties")
	if e == nil {
		t.Fatalf("entity missing")
	}
	if e.Subtype != "spring_properties" {
		t.Errorf("subtype=%q", e.Subtype)
	}
	keys := e.Properties["keys_top_level"]
	for _, want := range []string{"server.port", "spring.datasource.url"} {
		if !strings.Contains(keys, want) {
			t.Errorf("keys missing %q: %q", want, keys)
		}
	}
}

func TestDiscover_NonConfigFilesIgnored(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "src/main.go", `package main`)
	writeFixture(t, dir, "src/lib.py", `def f(): pass`)
	writeFixture(t, dir, "README.md", `hello`)
	ents, _ := runDiscover(t, dir, []string{"src/main.go", "src/lib.py", "README.md"})
	if len(ents) != 0 {
		t.Errorf("expected 0 entities, got %d (%v)", len(ents), ents)
	}
}

func TestDiscover_RequirementsVariants(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "requirements.txt", "requests\nfastapi>=0.110\n")
	writeFixture(t, dir, "requirements-dev.txt", "pytest\nblack\n")
	writeFixture(t, dir, "requirements-test.txt", "pytest-mock\n")
	ents, _ := runDiscover(t, dir, []string{
		"requirements.txt", "requirements-dev.txt", "requirements-test.txt",
	})
	if len(ents) != 3 {
		t.Fatalf("expected 3 entities, got %d", len(ents))
	}
	got := map[string]string{}
	for _, e := range ents {
		got[e.SourceFile] = e.Properties["dependencies"]
	}
	if !strings.Contains(got["requirements-dev.txt"], "pytest") {
		t.Errorf("dev: %q", got["requirements-dev.txt"])
	}
}

func TestDiscover_DeterministicOrder(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "a/package.json", `{"name":"a"}`)
	writeFixture(t, dir, "b/package.json", `{"name":"b"}`)
	writeFixture(t, dir, "c/Dockerfile", "FROM alpine\n")
	files := []string{"c/Dockerfile", "a/package.json", "b/package.json"}
	ents1, rels1 := runDiscover(t, dir, files)
	// Shuffle input order; output must remain identical.
	files2 := []string{"b/package.json", "c/Dockerfile", "a/package.json"}
	ents2, rels2 := runDiscover(t, dir, files2)
	if len(ents1) != len(ents2) || len(rels1) != len(rels2) {
		t.Fatalf("len mismatch")
	}
	for i := range ents1 {
		if ents1[i].QualifiedName != ents2[i].QualifiedName {
			t.Errorf("entity order drift at %d: %q vs %q", i, ents1[i].QualifiedName, ents2[i].QualifiedName)
		}
	}
	for i := range rels1 {
		if rels1[i].FromID != rels2[i].FromID || rels1[i].ToID != rels2[i].ToID {
			t.Errorf("rel order drift at %d", i)
		}
	}
}

func TestClassify_KnownBasenames(t *testing.T) {
	cases := []struct {
		path    string
		subtype string
	}{
		{"pyproject.toml", "python_project"},
		{"setup.cfg", "python_project_legacy"},
		{"requirements.txt", "python_requirements"},
		{"requirements-dev.txt", "python_requirements"},
		{".env", "env_vars"},
		{".env.local", "env_vars"},
		{"pom.xml", "maven_project"},
		{"build.gradle", "gradle_project"},
		{"build.gradle.kts", "gradle_project"},
		{"application.properties", "spring_properties"},
		{"application.yml", "spring_yaml"},
		{"application.yaml", "spring_yaml"},
		{"package.json", "node_project"},
		{"tsconfig.json", "typescript_project"},
		{"vite.config.ts", "vite_config"},
		{"next.config.js", "next_config"},
		{".eslintrc.json", "eslint_config"},
		{".prettierrc", "prettier_config"},
		{"go.mod", "go_module"},
		{"Makefile", "makefile"},
		{"Dockerfile", "docker_image"},
		{"Dockerfile.dev", "docker_image"},
		{"docker-compose.yml", "docker_compose"},
		{"quarkus.properties", "quarkus_properties"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			spec, ok := classify(tc.path)
			if !ok {
				t.Fatalf("classify(%q) returned false", tc.path)
			}
			if spec.subtype != tc.subtype {
				t.Errorf("subtype=%q want %q", spec.subtype, tc.subtype)
			}
		})
	}
}

func TestClassify_IgnoredFiles(t *testing.T) {
	for _, p := range []string{
		"src/main.go",
		".envrc",          // direnv — not an env file
		"poetry.lock",     // skip per issue
		".gitignore",      // explicitly skipped
		"foo.txt",
		"random.json",
	} {
		t.Run(p, func(t *testing.T) {
			if _, ok := classify(p); ok {
				t.Errorf("%q should NOT classify as config", p)
			}
		})
	}
}

func TestDiscover_KeyCapMarker(t *testing.T) {
	// Generate a Makefile with many targets to confirm the +N more marker.
	var b strings.Builder
	for i := 0; i < maxKeysPerProperty+10; i++ {
		// target names "tg000", "tg001", … to keep them sortable.
		b.WriteString("tg")
		b.WriteString(padInt(i))
		b.WriteString(":\n\techo\n")
	}
	dir := t.TempDir()
	writeFixture(t, dir, "Makefile", b.String())
	ents, _ := runDiscover(t, dir, []string{"Makefile"})
	if len(ents) != 1 {
		t.Fatalf("len=%d", len(ents))
	}
	scripts := ents[0].Properties["scripts"]
	if !strings.Contains(scripts, "+10 more") {
		t.Errorf("expected '+10 more' marker, got %q", scripts)
	}
}

func padInt(n int) string {
	s := []byte{'0', '0', '0'}
	for i := 2; n > 0 && i >= 0; i-- {
		s[i] = byte('0' + n%10)
		n /= 10
	}
	return string(s)
}

// TestDiscover_EdgeFromDirToConfig confirms the DEPENDS_ON_CONFIG edge
// FromID matches the file's containing directory (module structural ref).
func TestDiscover_EdgeFromDirToConfig(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "services/api/pyproject.toml", `[project]
name = "api"
`)
	_, rels := runDiscover(t, dir, []string{"services/api/pyproject.toml"})
	var froms []string
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindDependsOnConfig) {
			froms = append(froms, r.FromID)
		}
	}
	sort.Strings(froms)
	if len(froms) != 1 || froms[0] != "module:services/api" {
		t.Errorf("DEPENDS_ON_CONFIG FromIDs = %v, want [module:services/api]", froms)
	}
}
