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
		// Filter to dependency entities only — the manifest extractor now
		// also emits a SCOPE.Component subtype="project" anchor for the
		// manifest file itself (Rust wave-2: enables DEPENDS_ON FromID
		// resolution via byQualifiedName).
		if r.Kind == "SCOPE.Component" && r.Subtype == "external_dependency" {
			out = append(out, r)
		}
	}
	return out
}

// dependsOnRels returns every DEPENDS_ON edge embedded across all records.
// #560: edges are now embedded on the SCOPE.Component for each dep rather
// than on a synthetic "relationship"-kind container entity.
func dependsOnRels(records []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range records {
		for _, rel := range r.Relationships {
			if rel.Kind == "DEPENDS_ON" {
				out = append(out, rel)
			}
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
	// Rust wave-2: the manifest extractor still emits the project
	// anchor (subtype=project) for any recognised-manifest path, even
	// when parsing fails. Filter to dep entities to assert the parse
	// produced nothing.
	if deps := depEntities(records); len(deps) != 0 {
		t.Errorf("expected 0 dep entities for invalid JSON, got %d", len(deps))
	}
}

// ---------------------------------------------------------------------------
// Lockfiles (#2865) — package-lock.json / yarn.lock / pnpm-lock.yaml
// ---------------------------------------------------------------------------

func depByName(deps []types.EntityRecord, name string) *types.EntityRecord {
	for i := range deps {
		if deps[i].Name == name {
			return &deps[i]
		}
	}
	return nil
}

func TestLockfile_PackageLockV3(t *testing.T) {
	// lockfileVersion 3: resolved tree lives under "packages" keyed by
	// install path, including a transitive dep (ms) the manifest never names.
	src := `{
  "name": "myapp",
  "lockfileVersion": 3,
  "packages": {
    "": { "name": "myapp", "version": "1.0.0" },
    "node_modules/express": { "version": "4.18.2" },
    "node_modules/debug": { "version": "4.3.4", "dev": true },
    "node_modules/debug/node_modules/ms": { "version": "2.1.2" }
  }
}`
	deps := depEntities(runExtract(t, "project/package-lock.json", src))
	if len(deps) != 3 {
		t.Fatalf("expected 3 locked deps, got %d: %+v", len(deps), deps)
	}
	express := depByName(deps, "express")
	if express == nil {
		t.Fatal("expected express dep")
	}
	if express.Properties["version"] != "4.18.2" {
		t.Errorf("express version=%q want exact 4.18.2", express.Properties["version"])
	}
	if express.Properties["dependency_kind"] != "locked" {
		t.Errorf("express dependency_kind=%q want locked", express.Properties["dependency_kind"])
	}
	if express.Properties["package_manager"] != "npm" {
		t.Errorf("express package_manager=%q want npm", express.Properties["package_manager"])
	}
	// Nested transitive dep name is recovered from the last node_modules/ segment.
	if depByName(deps, "ms") == nil {
		t.Error("expected transitive dep ms (recovered from nested path)")
	}
	if d := depByName(deps, "debug"); d == nil || d.Properties["is_dev"] != "true" {
		t.Errorf("debug should be present and is_dev=true, got %+v", d)
	}
}

func TestLockfile_PackageLockV1(t *testing.T) {
	src := `{
  "name": "myapp",
  "lockfileVersion": 1,
  "dependencies": {
    "lodash": { "version": "4.17.21" },
    "jest": { "version": "29.5.0", "dev": true }
  }
}`
	deps := depEntities(runExtract(t, "package-lock.json", src))
	if len(deps) != 2 {
		t.Fatalf("expected 2 locked deps, got %d", len(deps))
	}
	if d := depByName(deps, "lodash"); d == nil || d.Properties["version"] != "4.17.21" {
		t.Errorf("lodash exact version not recovered: %+v", d)
	}
}

func TestLockfile_YarnClassic(t *testing.T) {
	src := "# THIS IS AN AUTOGENERATED FILE\n" +
		"# yarn lockfile v1\n\n" +
		"\"@babel/core@^7.0.0\":\n" +
		"  version \"7.22.0\"\n" +
		"  resolved \"https://registry.yarnpkg.com/@babel/core/-/core-7.22.0.tgz\"\n\n" +
		"lodash@^4.17.21, lodash@^4.17.0:\n" +
		"  version \"4.17.21\"\n" +
		"  resolved \"https://registry.yarnpkg.com/lodash/-/lodash-4.17.21.tgz\"\n"
	deps := depEntities(runExtract(t, "yarn.lock", src))
	if len(deps) != 2 {
		t.Fatalf("expected 2 locked deps, got %d: %+v", len(deps), deps)
	}
	if d := depByName(deps, "@babel/core"); d == nil || d.Properties["version"] != "7.22.0" {
		t.Errorf("scoped @babel/core not parsed correctly: %+v", d)
	}
	if d := depByName(deps, "lodash"); d == nil || d.Properties["version"] != "4.17.21" {
		t.Errorf("lodash (multi-descriptor header) not parsed correctly: %+v", d)
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "yarn" {
			t.Errorf("%s package_manager=%q want yarn", d.Name, d.Properties["package_manager"])
		}
	}
}

func TestLockfile_PnpmV6(t *testing.T) {
	src := "lockfileVersion: '6.0'\n\n" +
		"dependencies:\n" +
		"  express:\n" +
		"    specifier: ^4.18.0\n" +
		"    version: 4.18.2\n\n" +
		"packages:\n\n" +
		"  /express@4.18.2:\n" +
		"    resolution: {integrity: sha512-fake}\n" +
		"    dev: false\n\n" +
		"  /@babel/core@7.22.0(react@18.2.0):\n" +
		"    resolution: {integrity: sha512-fake}\n" +
		"    dev: true\n"
	deps := depEntities(runExtract(t, "pnpm-lock.yaml", src))
	if d := depByName(deps, "express"); d == nil || d.Properties["version"] != "4.18.2" {
		t.Errorf("express not parsed from pnpm packages block: %+v", d)
	}
	// Peer-dep suffix is trimmed; scoped name preserved.
	if d := depByName(deps, "@babel/core"); d == nil || d.Properties["version"] != "7.22.0" {
		t.Errorf("@babel/core with peer suffix not parsed correctly: %+v", d)
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "pnpm" {
			t.Errorf("%s package_manager=%q want pnpm", d.Name, d.Properties["package_manager"])
		}
		if d.Properties["dependency_kind"] != "locked" {
			t.Errorf("%s dependency_kind=%q want locked", d.Name, d.Properties["dependency_kind"])
		}
	}
}

func TestLockfile_IsManifest(t *testing.T) {
	for _, n := range []string{"package-lock.json", "yarn.lock", "pnpm-lock.yaml", "npm-shrinkwrap.json"} {
		if !IsManifest("some/dir/" + n) {
			t.Errorf("IsManifest(%q) = false, want true", n)
		}
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
	// Rust wave-2: project anchor is unconditional; assert no deps.
	if deps := depEntities(records); len(deps) != 0 {
		t.Errorf("expected 0 dep entities for invalid XML, got %d", len(deps))
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
	rels := dependsOnRels(records)
	if len(rels) != 1 {
		t.Fatalf("expected 1 DEPENDS_ON edge, got %d", len(rels))
	}
	r := rels[0]
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
		{"repo/requirements.txt", true},
		{"repo/pubspec.yaml", true},
		{"repo/Gemfile", true},
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

// ---------------------------------------------------------------------------
// requirements.txt
// ---------------------------------------------------------------------------

func TestRequirementsTxt_Basic(t *testing.T) {
	src := `requests>=2.28.0
fastapi[all]>=0.100.0
# dev deps below
pytest==7.4.0
`
	records := runExtract(t, "requirements.txt", src)
	deps := depEntities(records)
	if len(deps) < 3 {
		t.Fatalf("expected at least 3 deps, got %d", len(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "pip" {
			t.Errorf("package_manager=%q want pip", d.Properties["package_manager"])
		}
		if d.Properties["dependency_kind"] == "" {
			t.Errorf("dependency_kind empty for %s", d.Name)
		}
	}
}

func TestRequirementsTxt_SkipsComments(t *testing.T) {
	src := `# This is a comment
requests>=2.28
`
	records := runExtract(t, "requirements.txt", src)
	deps := depEntities(records)
	if len(deps) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(deps))
	}
	if deps[0].Name != "requests" {
		t.Errorf("name=%q want requests", deps[0].Name)
	}
}

func TestRequirementsTxt_Empty(t *testing.T) {
	src := `# nothing here`
	records := runExtract(t, "requirements.txt", src)
	if deps := depEntities(records); len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d", len(deps))
	}
}

// ---------------------------------------------------------------------------
// pubspec.yaml
// ---------------------------------------------------------------------------

func TestPubspecYaml_Basic(t *testing.T) {
	src := `name: myapp
version: 1.0.0

dependencies:
  flutter:
    sdk: flutter
  http: ^1.1.0
  provider: ^6.0.0

dev_dependencies:
  flutter_test:
    sdk: flutter
  mockito: ^5.0.0
`
	records := runExtract(t, "pubspec.yaml", src)
	deps := depEntities(records)
	// expect: http, provider, flutter (runtime) + mockito, flutter_test (dev)
	if len(deps) == 0 {
		t.Fatalf("expected deps, got 0")
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "pub" {
			t.Errorf("package_manager=%q want pub", d.Properties["package_manager"])
		}
	}
	// mockito should be dev
	for _, d := range deps {
		if d.Name == "mockito" && d.Properties["is_dev"] != "true" {
			t.Errorf("mockito should be is_dev=true")
		}
	}
	// http should be runtime
	for _, d := range deps {
		if d.Name == "http" && d.Properties["is_dev"] != "false" {
			t.Errorf("http should be is_dev=false")
		}
	}
}

func TestPubspecYaml_Empty(t *testing.T) {
	src := `name: empty
version: 0.0.1
`
	records := runExtract(t, "pubspec.yaml", src)
	if deps := depEntities(records); len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d", len(deps))
	}
}

// ---------------------------------------------------------------------------
// Gemfile
// ---------------------------------------------------------------------------

func TestGemfile_Basic(t *testing.T) {
	src := `source 'https://rubygems.org'
gem 'rails', '~> 7.0'
gem 'pg', '>= 0.18'

group :development, :test do
  gem 'rspec-rails'
  gem 'factory_bot_rails'
end
`
	records := runExtract(t, "Gemfile", src)
	deps := depEntities(records)
	if len(deps) < 2 {
		t.Fatalf("expected at least 2 deps, got %d", len(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "bundler" {
			t.Errorf("package_manager=%q want bundler", d.Properties["package_manager"])
		}
	}
	// rails should be runtime
	for _, d := range deps {
		if d.Name == "rails" && d.Properties["is_dev"] != "false" {
			t.Errorf("rails should be is_dev=false")
		}
	}
	// rspec-rails should be dev
	for _, d := range deps {
		if d.Name == "rspec-rails" && d.Properties["is_dev"] != "true" {
			t.Errorf("rspec-rails should be is_dev=true")
		}
	}
}

func TestGemfile_Empty(t *testing.T) {
	src := `source 'https://rubygems.org'`
	records := runExtract(t, "Gemfile", src)
	if deps := depEntities(records); len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d", len(deps))
	}
}

// ---------------------------------------------------------------------------
// dependency_kind property
// ---------------------------------------------------------------------------

func TestDependencyKind_PackageJSON(t *testing.T) {
	src := `{
  "dependencies": {"react": "^18.0.0"},
  "devDependencies": {"jest": "^29.0.0"},
  "peerDependencies": {"react-dom": "^18.0.0"}
}`
	records := runExtract(t, "package.json", src)
	deps := depEntities(records)
	byName := map[string]types.EntityRecord{}
	for _, d := range deps {
		byName[d.Name] = d
	}

	if byName["react"].Properties["dependency_kind"] != "runtime" {
		t.Errorf("react dependency_kind=%q want runtime", byName["react"].Properties["dependency_kind"])
	}
	if byName["jest"].Properties["dependency_kind"] != "dev" {
		t.Errorf("jest dependency_kind=%q want dev", byName["jest"].Properties["dependency_kind"])
	}
	if byName["react-dom"].Properties["dependency_kind"] != "peer" {
		t.Errorf("react-dom dependency_kind=%q want peer", byName["react-dom"].Properties["dependency_kind"])
	}
}
