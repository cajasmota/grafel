package manifest

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func runExtract(t *testing.T, filePath, source string) []types.EntityRecord {
	t.Helper()
	e := &Extractor{}
	records, err := e.Extract(context.Background(), extractor.FileInput{
		Path:    filePath,
		Content: []byte(source),
	})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	return records
}

func depEntities(records []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range records {
		if r.Kind == "SCOPE.Component" {
			out = append(out, r)
		}
	}
	return out
}

func relEntities(records []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range records {
		if r.Kind == "relationship" {
			out = append(out, r)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// package.json
// ---------------------------------------------------------------------------

func TestPackageJSON_Dependencies(t *testing.T) {
	src := `{
  "dependencies": {
    "express": "^4.18.0",
    "lodash": "~4.17.0"
  },
  "devDependencies": {
    "jest": "^29.0.0"
  }
}`
	records := runExtract(t, "project/package.json", src)
	deps := depEntities(records)
	if len(deps) != 3 {
		t.Fatalf("expected 3 dep entities, got %d", len(deps))
	}
	// Check package manager
	for _, d := range deps {
		if d.Properties["package_manager"] != "npm" {
			t.Errorf("package_manager=%q want npm", d.Properties["package_manager"])
		}
	}
	// jest should be dev
	for _, d := range deps {
		if d.Name == "jest" && d.Properties["is_dev"] != "true" {
			t.Errorf("jest should be is_dev=true")
		}
	}
	// express should not be dev
	for _, d := range deps {
		if d.Name == "express" && d.Properties["is_dev"] != "false" {
			t.Errorf("express should be is_dev=false")
		}
	}
}

func TestPackageJSON_EmptyDeps(t *testing.T) {
	src := `{"name": "myapp", "version": "1.0.0"}`
	records := runExtract(t, "package.json", src)
	deps := depEntities(records)
	if len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d", len(deps))
	}
}

func TestPackageJSON_InvalidJSON(t *testing.T) {
	src := `{invalid json`
	records := runExtract(t, "package.json", src)
	if len(records) != 0 {
		t.Errorf("expected 0 records for invalid JSON, got %d", len(records))
	}
}

// ---------------------------------------------------------------------------
// go.mod
// ---------------------------------------------------------------------------

func TestGoMod_BlockRequire(t *testing.T) {
	src := `module github.com/myorg/myapp

go 1.21

require (
	github.com/gin-gonic/gin v1.9.1
	github.com/pkg/errors v0.9.1
)
`
	records := runExtract(t, "go.mod", src)
	deps := depEntities(records)
	if len(deps) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "go_modules" {
			t.Errorf("package_manager=%q want go_modules", d.Properties["package_manager"])
		}
	}
}

func TestGoMod_SingleRequire(t *testing.T) {
	src := `module example.com/app

go 1.20

require github.com/google/uuid v1.6.0
`
	records := runExtract(t, "go.mod", src)
	deps := depEntities(records)
	if len(deps) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(deps))
	}
	if deps[0].Name != "github.com/google/uuid" {
		t.Errorf("name=%q want github.com/google/uuid", deps[0].Name)
	}
}

func TestGoMod_Deduplication(t *testing.T) {
	src := `module app
require (
	github.com/foo/bar v1.0.0
	github.com/foo/bar v1.1.0
)
`
	records := runExtract(t, "go.mod", src)
	deps := depEntities(records)
	if len(deps) != 1 {
		t.Errorf("expected 1 dep after dedup, got %d", len(deps))
	}
}

// ---------------------------------------------------------------------------
// Cargo.toml
// ---------------------------------------------------------------------------

func TestCargoToml_Dependencies(t *testing.T) {
	src := `[package]
name = "mylib"
version = "0.1.0"

[dependencies]
serde = "1.0"
tokio = { version = "1.0", features = ["full"] }

[dev-dependencies]
mockito = "1.0"
`
	records := runExtract(t, "Cargo.toml", src)
	deps := depEntities(records)
	if len(deps) < 2 {
		t.Fatalf("expected at least 2 deps, got %d", len(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "cargo" {
			t.Errorf("package_manager=%q want cargo", d.Properties["package_manager"])
		}
	}
	for _, d := range deps {
		if d.Name == "mockito" && d.Properties["is_dev"] != "true" {
			t.Errorf("mockito should be is_dev=true")
		}
	}
}

func TestCargoToml_NoDeps(t *testing.T) {
	src := `[package]
name = "empty"
version = "0.0.1"
`
	records := runExtract(t, "Cargo.toml", src)
	deps := depEntities(records)
	if len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d", len(deps))
	}
}

// ---------------------------------------------------------------------------
// pyproject.toml
// ---------------------------------------------------------------------------

func TestPyprojectToml_ProjectDeps(t *testing.T) {
	src := `[project]
name = "myapp"
version = "1.0.0"
dependencies = [
    "requests>=2.28",
    "fastapi>=0.100"
]
`
	records := runExtract(t, "pyproject.toml", src)
	deps := depEntities(records)
	if len(deps) < 2 {
		t.Fatalf("expected at least 2 deps, got %d", len(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "pip" {
			t.Errorf("package_manager=%q want pip", d.Properties["package_manager"])
		}
	}
}

func TestPyprojectToml_PoetryDeps(t *testing.T) {
	src := `[tool.poetry.dependencies]
python = "^3.11"
httpx = "^0.24"
pydantic = "^2.0"

[tool.poetry.dev-dependencies]
pytest = "^7.0"
`
	records := runExtract(t, "pyproject.toml", src)
	deps := depEntities(records)
	// python is skipped, expect: httpx, pydantic, pytest
	names := map[string]bool{}
	for _, d := range deps {
		names[d.Name] = true
	}
	if names["python"] {
		t.Error("python should be skipped")
	}
	if !names["httpx"] {
		t.Error("httpx should be present")
	}
}

// ---------------------------------------------------------------------------
// pom.xml
// ---------------------------------------------------------------------------

func TestPomXML_Dependencies(t *testing.T) {
	src := `<project>
  <dependencies>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-core</artifactId>
      <version>6.0.0</version>
    </dependency>
    <dependency>
      <groupId>junit</groupId>
      <artifactId>junit</artifactId>
      <version>4.13</version>
      <scope>test</scope>
    </dependency>
  </dependencies>
</project>`
	records := runExtract(t, "pom.xml", src)
	deps := depEntities(records)
	if len(deps) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "maven" {
			t.Errorf("package_manager=%q want maven", d.Properties["package_manager"])
		}
	}
	for _, d := range deps {
		if d.Name == "junit:junit" && d.Properties["is_dev"] != "true" {
			t.Errorf("junit should be is_dev=true (scope=test)")
		}
	}
}

func TestPomXML_InvalidXML(t *testing.T) {
	src := `<project><broken`
	records := runExtract(t, "pom.xml", src)
	if len(records) != 0 {
		t.Errorf("expected 0 records for invalid XML, got %d", len(records))
	}
}

// ---------------------------------------------------------------------------
// Non-manifest file
// ---------------------------------------------------------------------------

func TestNonManifest_ReturnsEmpty(t *testing.T) {
	src := `package main\nfunc main() {}`
	records := runExtract(t, "main.go", src)
	if len(records) != 0 {
		t.Errorf("expected 0 records for non-manifest, got %d", len(records))
	}
}

// ---------------------------------------------------------------------------
// Relationship records emitted
// ---------------------------------------------------------------------------

func TestRelationshipsEmitted(t *testing.T) {
	src := `{"dependencies":{"lodash":"4.17.21"}}`
	records := runExtract(t, "package.json", src)
	rels := relEntities(records)
	if len(rels) != 1 {
		t.Fatalf("expected 1 relationship entity, got %d", len(rels))
	}
	r := rels[0].Relationships[0]
	if r.Kind != "DEPENDS_ON" {
		t.Errorf("rel kind=%q want DEPENDS_ON", r.Kind)
	}
	if r.Properties["kind"] != "external_dependency" {
		t.Errorf("rel kind property=%q want external_dependency", r.Properties["kind"])
	}
}

// ---------------------------------------------------------------------------
// IsManifest helper
// ---------------------------------------------------------------------------

func TestIsManifest(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"repo/package.json", true},
		{"repo/go.mod", true},
		{"repo/Cargo.toml", true},
		{"repo/pyproject.toml", true},
		{"repo/pom.xml", true},
		{"repo/main.go", false},
		{"repo/README.md", false},
	}
	for _, c := range cases {
		got := IsManifest(c.path)
		if got != c.want {
			t.Errorf("IsManifest(%q)=%v want %v", c.path, got, c.want)
		}
	}
}
