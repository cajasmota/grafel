package manifest

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
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

// TestGoMod_IndirectTracking verifies that go.mod `// indirect` markers are
// surfaced so transitive dependencies are distinguishable from direct ones
// (lockfile-style tracking, #3217). Covers both the require(...) block form
// and the single-line require form.
func TestGoMod_IndirectTracking(t *testing.T) {
	src := `module github.com/myorg/myapp

go 1.21

require (
	github.com/gin-gonic/gin v1.9.1
	github.com/bytedance/sonic v1.9.1 // indirect
)

require github.com/leodido/go-urn v1.2.4 // indirect
`
	records := runExtract(t, "go.mod", src)
	deps := depEntities(records)
	byName := map[string]string{}
	for _, d := range deps {
		byName[d.Name] = d.Properties["indirect"]
	}
	if byName["github.com/gin-gonic/gin"] != "false" {
		t.Errorf("gin indirect=%q want false", byName["github.com/gin-gonic/gin"])
	}
	if byName["github.com/bytedance/sonic"] != "true" {
		t.Errorf("sonic (block // indirect) indirect=%q want true", byName["github.com/bytedance/sonic"])
	}
	if byName["github.com/leodido/go-urn"] != "true" {
		t.Errorf("go-urn (single-line // indirect) indirect=%q want true", byName["github.com/leodido/go-urn"])
	}
	// The indirect deps should also carry dependency_kind=indirect.
	for _, d := range deps {
		if d.Name == "github.com/bytedance/sonic" && d.Properties["dependency_kind"] != "indirect" {
			t.Errorf("sonic dependency_kind=%q want indirect", d.Properties["dependency_kind"])
		}
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

// TestPubspecLock_Resolved proves pubspec.lock lockfile parsing recovers the
// fully-resolved dependency tree: exact versions, dev classification from the
// `dependency:` line, transitive deps the manifest never names (marked
// indirect), and that the trailing `sdks:` block is excluded.
func TestPubspecLock_Resolved(t *testing.T) {
	src := `# Generated by pub
packages:
  drift:
    dependency: "direct main"
    description:
      name: drift
      url: "https://pub.dev"
    source: hosted
    version: "2.14.1"
  build_runner:
    dependency: "direct dev"
    source: hosted
    version: "2.4.6"
  meta:
    dependency: transitive
    source: hosted
    version: "1.10.0"
sdks:
  dart: ">=3.0.0 <4.0.0"
`
	deps := depEntities(runExtract(t, "pubspec.lock", src))
	if len(deps) != 3 {
		t.Fatalf("expected 3 locked deps, got %d: %+v", len(deps), deps)
	}
	drift := depByName(deps, "drift")
	if drift == nil {
		t.Fatal("expected drift dep")
	}
	if drift.Properties["version"] != "2.14.1" {
		t.Errorf("drift version=%q want 2.14.1", drift.Properties["version"])
	}
	if drift.Properties["dependency_kind"] != "locked" {
		t.Errorf("drift dependency_kind=%q want locked", drift.Properties["dependency_kind"])
	}
	if drift.Properties["package_manager"] != "pub" {
		t.Errorf("drift package_manager=%q want pub", drift.Properties["package_manager"])
	}
	if d := depByName(deps, "build_runner"); d == nil || d.Properties["is_dev"] != "true" {
		t.Errorf("build_runner should be present and is_dev=true, got %+v", d)
	}
	meta := depByName(deps, "meta")
	if meta == nil || meta.Properties["indirect"] != "true" {
		t.Errorf("transitive dep meta should be indirect=true, got %+v", meta)
	}
	// The `sdks:` block must NOT be parsed as a package.
	if depByName(deps, "dart") != nil {
		t.Error("sdks: block leaked a 'dart' dependency")
	}
}

// ---------------------------------------------------------------------------
// shard.yml / shard.lock (Crystal shards)
// ---------------------------------------------------------------------------

// TestShardYml_Basic proves shards manifest_parsing: dependencies (runtime) and
// development_dependencies (dev) maps yield deps under package_manager=shards,
// reading each shard's nested `version:` line and excluding the top-level
// name/version/crystal project-metadata scalars.
func TestShardYml_Basic(t *testing.T) {
	src := `name: myapp
version: 0.1.0
crystal: ">= 1.0.0"
license: MIT

dependencies:
  kemal:
    github: kemalcr/kemal
    version: ~> 1.1.0
  jennifer:
    github: imdrasil/jennifer.cr

development_dependencies:
  spectator:
    github: crystal-loot/spectator
    version: ~> 0.10.0
`
	deps := depEntities(runExtract(t, "shard.yml", src))
	if len(deps) != 3 {
		t.Fatalf("expected 3 deps (kemal, jennifer, spectator), got %d: %+v", len(deps), deps)
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "shards" {
			t.Errorf("%s package_manager=%q want shards", d.Name, d.Properties["package_manager"])
		}
	}
	kemal := depByName(deps, "kemal")
	if kemal == nil {
		t.Fatal("expected kemal dep")
	}
	if kemal.Properties["version"] != "~> 1.1.0" {
		t.Errorf("kemal version=%q want '~> 1.1.0'", kemal.Properties["version"])
	}
	if kemal.Properties["is_dev"] != "false" {
		t.Errorf("kemal should be is_dev=false")
	}
	// jennifer has no version: line — honest empty version, still emitted.
	jennifer := depByName(deps, "jennifer")
	if jennifer == nil || jennifer.Properties["version"] != "" {
		t.Errorf("jennifer should be present with empty version, got %+v", jennifer)
	}
	if d := depByName(deps, "spectator"); d == nil || d.Properties["is_dev"] != "true" {
		t.Errorf("spectator should be present and is_dev=true, got %+v", d)
	}
	// Project metadata scalars must NOT be parsed as deps.
	if depByName(deps, "crystal") != nil || depByName(deps, "license") != nil {
		t.Error("project-metadata scalar leaked as a dependency")
	}
}

func TestShardYml_Empty(t *testing.T) {
	src := `name: empty
version: 0.0.1
crystal: ">= 1.0.0"
`
	if deps := depEntities(runExtract(t, "shard.yml", src)); len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d", len(deps))
	}
}

// TestShardLock_Resolved proves shards lockfile_parsing: the `shards:` map yields
// the exact resolved versions (kind=locked) and the top-level lockfile-format
// `version:` scalar is excluded.
func TestShardLock_Resolved(t *testing.T) {
	src := `version: 2.0
shards:
  kemal:
    git: https://github.com/kemalcr/kemal.git
    version: 1.1.2
  radix:
    git: https://github.com/luislavena/radix.git
    version: 0.4.1
`
	deps := depEntities(runExtract(t, "shard.lock", src))
	if len(deps) != 2 {
		t.Fatalf("expected 2 locked deps, got %d: %+v", len(deps), deps)
	}
	kemal := depByName(deps, "kemal")
	if kemal == nil {
		t.Fatal("expected kemal dep")
	}
	if kemal.Properties["version"] != "1.1.2" {
		t.Errorf("kemal version=%q want 1.1.2", kemal.Properties["version"])
	}
	if kemal.Properties["dependency_kind"] != "locked" {
		t.Errorf("kemal dependency_kind=%q want locked", kemal.Properties["dependency_kind"])
	}
	if kemal.Properties["package_manager"] != "shards" {
		t.Errorf("kemal package_manager=%q want shards", kemal.Properties["package_manager"])
	}
	// The top-level format `version: 2.0` must NOT be a dependency.
	if depByName(deps, "2.0") != nil {
		t.Error("lockfile-format version leaked as a dependency")
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

// ---------------------------------------------------------------------------
// .csproj — NuGet manifest_parsing (#3263)
// ---------------------------------------------------------------------------

func TestCsprojPackageReference(t *testing.T) {
	src := `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Dapper" Version="2.1.28" />
    <PackageReference Include="Microsoft.Extensions.DependencyInjection" Version="8.0.0" />
    <PackageReference Include="Carter" Version="8.1.0" />
  </ItemGroup>
</Project>`

	records := runExtract(t, "MyApp.csproj", src)
	deps := depEntities(records)
	if len(deps) < 3 {
		t.Fatalf("expected ≥3 dep entities from .csproj, got %d", len(deps))
	}
	byName := map[string]types.EntityRecord{}
	for _, d := range deps {
		byName[d.Name] = d
	}
	for _, pkg := range []string{"Dapper", "Microsoft.Extensions.DependencyInjection", "Carter"} {
		if _, ok := byName[pkg]; !ok {
			t.Errorf("expected package %q in csproj dependencies", pkg)
		}
	}
	if byName["Carter"].Properties["package_manager"] != "nuget" {
		t.Errorf("expected package_manager=nuget, got %q", byName["Carter"].Properties["package_manager"])
	}
}

func TestCsprojIsManifest(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"MyApp.csproj", true},
		{"src/MyLib/MyLib.csproj", true},
		{"packages.lock.json", true},
		{"go.mod", true},
		{"SomeOtherFile.xml", false},
		{"myapp.json", false},
	}
	for _, c := range cases {
		got := IsManifest(c.path)
		if got != c.want {
			t.Errorf("IsManifest(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// packages.lock.json — NuGet lockfile_parsing (#3263)
// ---------------------------------------------------------------------------

func TestNugetLockFile(t *testing.T) {
	src := `{
  "version": 1,
  "dependencies": {
    "net8.0": {
      "Dapper": {
        "type": "Direct",
        "requested": "[2.1.28, )",
        "resolved": "2.1.28",
        "contentHash": "abc123"
      },
      "Microsoft.Extensions.DependencyInjection": {
        "type": "Direct",
        "requested": "[8.0.0, )",
        "resolved": "8.0.0",
        "contentHash": "def456"
      },
      "Newtonsoft.Json": {
        "type": "Transitive",
        "resolved": "13.0.3",
        "contentHash": "ghi789"
      }
    }
  }
}`
	records := runExtract(t, "packages.lock.json", src)
	deps := depEntities(records)
	if len(deps) < 3 {
		t.Fatalf("expected ≥3 dep entities from packages.lock.json, got %d", len(deps))
	}
	byName := map[string]types.EntityRecord{}
	for _, d := range deps {
		byName[d.Name] = d
	}
	for _, pkg := range []string{"Dapper", "Microsoft.Extensions.DependencyInjection", "Newtonsoft.Json"} {
		if _, ok := byName[pkg]; !ok {
			t.Errorf("expected package %q in packages.lock.json dependencies", pkg)
		}
	}
	if byName["Dapper"].Properties["dependency_kind"] != "locked" {
		t.Errorf("expected dependency_kind=locked for Dapper, got %q", byName["Dapper"].Properties["dependency_kind"])
	}
	if byName["Dapper"].Properties["package_manager"] != "nuget" {
		t.Errorf("expected package_manager=nuget, got %q", byName["Dapper"].Properties["package_manager"])
	}
}

func TestCMake_FindPackage(t *testing.T) {
	src := `cmake_minimum_required(VERSION 3.15)
project(MyApp)
find_package(Boost 1.79 REQUIRED COMPONENTS filesystem)
find_package(OpenSSL REQUIRED)
find_package(ZLIB)
`
	records := runExtract(t, "CMakeLists.txt", src)
	deps := depEntities(records)
	byName := map[string]bool{}
	for _, d := range deps {
		byName[d.Name] = true
	}
	for _, pkg := range []string{"Boost", "OpenSSL", "ZLIB"} {
		if !byName[pkg] {
			t.Errorf("expected dep %q from find_package, not found", pkg)
		}
	}
}

func TestCMake_TargetLinkLibraries(t *testing.T) {
	src := `add_executable(myapp main.cpp)
target_link_libraries(myapp PRIVATE Boost::filesystem OpenSSL::SSL pthread)
`
	records := runExtract(t, "CMakeLists.txt", src)
	deps := depEntities(records)
	byName := map[string]bool{}
	for _, d := range deps {
		byName[d.Name] = true
	}
	for _, lib := range []string{"Boost::filesystem", "OpenSSL::SSL", "pthread"} {
		if !byName[lib] {
			t.Errorf("expected dep %q from target_link_libraries, not found", lib)
		}
	}
}

func TestCMake_PackageManager(t *testing.T) {
	src := `find_package(Eigen3 REQUIRED)`
	records := runExtract(t, "CMakeLists.txt", src)
	for _, r := range records {
		if r.Properties["package_manager"] != "" && r.Properties["package_manager"] != "cmake" {
			t.Errorf("package_manager=%q want cmake", r.Properties["package_manager"])
		}
	}
}

func TestCMake_Empty(t *testing.T) {
	src := `cmake_minimum_required(VERSION 3.15)
project(Empty)
add_executable(app main.cpp)
`
	records := runExtract(t, "CMakeLists.txt", src)
	deps := depEntities(records)
	if len(deps) != 0 {
		t.Errorf("expected no dep entities for CMake without find_package/target_link_libraries deps, got %d", len(deps))
	}
}

// ---------------------------------------------------------------------------
// conanfile.txt
// ---------------------------------------------------------------------------

func TestConanfileTxt_Requires(t *testing.T) {
	src := `[requires]
boost/1.79.0
zlib/1.2.13
openssl/3.1.0

[generators]
cmake
`
	records := runExtract(t, "conanfile.txt", src)
	deps := depEntities(records)
	byName := map[string]bool{}
	for _, d := range deps {
		byName[d.Name] = true
	}
	for _, pkg := range []string{"boost", "zlib", "openssl"} {
		if !byName[pkg] {
			t.Errorf("expected dep %q from conanfile.txt [requires], not found", pkg)
		}
	}
}

func TestConanfileTxt_BuildRequires(t *testing.T) {
	src := `[requires]
boost/1.79.0

[build_requires]
cmake/3.25.0
`
	records := runExtract(t, "conanfile.txt", src)
	deps := depEntities(records)
	if len(deps) < 2 {
		t.Errorf("expected at least 2 deps (requires + build_requires), got %d", len(deps))
	}
}

func TestConanfileTxt_Versions(t *testing.T) {
	src := `[requires]
fmt/9.1.0
`
	records := runExtract(t, "conanfile.txt", src)
	deps := depEntities(records)
	for _, d := range deps {
		if d.Name == "fmt" {
			if d.Properties["version"] != "9.1.0" {
				t.Errorf("version=%q want 9.1.0", d.Properties["version"])
			}
			return
		}
	}
	t.Error("dep fmt not found")
}

// ---------------------------------------------------------------------------
// conanfile.py
// ---------------------------------------------------------------------------

func TestConanfilePy_Requires(t *testing.T) {
	src := `from conans import ConanFile

class MyConan(ConanFile):
    name = "myproject"
    requires = "boost/1.79.0", "zlib/1.2.13"
    build_requires = "cmake/3.25.0"
`
	records := runExtract(t, "conanfile.py", src)
	deps := depEntities(records)
	byName := map[string]bool{}
	for _, d := range deps {
		byName[d.Name] = true
	}
	for _, pkg := range []string{"boost", "zlib", "cmake"} {
		if !byName[pkg] {
			t.Errorf("expected dep %q from conanfile.py, not found", pkg)
		}
	}
}

func TestConanfilePy_ListRequires(t *testing.T) {
	src := `from conans import ConanFile

class MyConan(ConanFile):
    requires = [
        "openssl/3.1.0",
        "fmt/9.1.0",
    ]
`
	records := runExtract(t, "conanfile.py", src)
	deps := depEntities(records)
	if len(deps) < 2 {
		t.Errorf("expected at least 2 deps from list-style requires, got %d", len(deps))
	}
}

// ---------------------------------------------------------------------------
// vcpkg.json
// ---------------------------------------------------------------------------

func TestVcpkgJSON_StringDeps(t *testing.T) {
	src := `{
  "name": "myproject",
  "version": "1.0.0",
  "dependencies": [
    "boost",
    "openssl",
    "zlib"
  ]
}`
	records := runExtract(t, "vcpkg.json", src)
	deps := depEntities(records)
	byName := map[string]bool{}
	for _, d := range deps {
		byName[d.Name] = true
	}
	for _, pkg := range []string{"boost", "openssl", "zlib"} {
		if !byName[pkg] {
			t.Errorf("expected dep %q from vcpkg.json, not found", pkg)
		}
	}
}

func TestVcpkgJSON_ObjectDeps(t *testing.T) {
	src := `{
  "name": "myproject",
  "dependencies": [
    { "name": "fmt", "version-gte": "9.1.0" },
    { "name": "nlohmann-json", "version-gte": "3.11.0" },
    "boost"
  ]
}`
	records := runExtract(t, "vcpkg.json", src)
	deps := depEntities(records)
	byName := map[string]string{}
	for _, d := range deps {
		byName[d.Name] = d.Properties["version"]
	}
	if _, ok := byName["fmt"]; !ok {
		t.Error("expected dep fmt from vcpkg.json object-style")
	}
	if _, ok := byName["nlohmann-json"]; !ok {
		t.Error("expected dep nlohmann-json from vcpkg.json object-style")
	}
	if _, ok := byName["boost"]; !ok {
		t.Error("expected dep boost from vcpkg.json string-style")
	}
}

func TestVcpkgJSON_PackageManager(t *testing.T) {
	src := `{"dependencies": ["zlib"]}`
	records := runExtract(t, "vcpkg.json", src)
	for _, r := range records {
		pm := r.Properties["package_manager"]
		if pm != "" && pm != "vcpkg" {
			t.Errorf("package_manager=%q want vcpkg", pm)
		}
	}
}

func TestVcpkgJSON_Empty(t *testing.T) {
	src := `{"name": "empty", "dependencies": []}`
	records := runExtract(t, "vcpkg.json", src)
	deps := depEntities(records)
	if len(deps) != 0 {
		t.Errorf("expected no dep entities for empty dependencies, got %d", len(deps))
	}
}

func TestCMake_NotManifest(t *testing.T) {
	// A file named differently should not be processed
	records := runExtract(t, "CMakeListsCustom.txt", "find_package(Boost REQUIRED)")
	if len(records) != 0 {
		t.Errorf("expected 0 entities for non-manifest filename, got %d", len(records))
	}
}

// ---------------------------------------------------------------------------
// composer.json / composer.lock
// ---------------------------------------------------------------------------

func TestComposerJSON_Dependencies(t *testing.T) {
	src := `{
  "require": {
    "laravel/framework": "^10.0",
    "guzzlehttp/guzzle": "^7.2"
  },
  "require-dev": {
    "phpunit/phpunit": "^10.1",
    "nunomaduro/collision": "^7.0"
  }
}`
	records := runExtract(t, "composer.json", src)
	deps := depEntities(records)
	byName := map[string]string{}
	for _, d := range deps {
		byName[d.Name] = d.Properties["dependency_kind"]
	}
	if byName["laravel/framework"] != "runtime" {
		t.Errorf("expected laravel/framework runtime dep, got %q", byName["laravel/framework"])
	}
	if byName["phpunit/phpunit"] != "dev" {
		t.Errorf("expected phpunit/phpunit dev dep, got %q", byName["phpunit/phpunit"])
	}
	if _, ok := byName["guzzlehttp/guzzle"]; !ok {
		t.Error("expected guzzlehttp/guzzle dep")
	}
}

func TestComposerJSON_SkipsPHPRuntime(t *testing.T) {
	src := `{
  "require": {
    "php": "^8.1",
    "ext-mbstring": "*",
    "symfony/console": "^6.3"
  }
}`
	records := runExtract(t, "composer.json", src)
	deps := depEntities(records)
	for _, d := range deps {
		if d.Name == "php" || d.Name == "ext-mbstring" {
			t.Errorf("should not emit entity for %q (PHP runtime constraint)", d.Name)
		}
	}
	byName := map[string]bool{}
	for _, d := range deps {
		byName[d.Name] = true
	}
	if !byName["symfony/console"] {
		t.Error("expected symfony/console dep")
	}
}

func TestComposerLock_Packages(t *testing.T) {
	src := `{
  "_readme": ["This file is @generated automatically"],
  "packages": [
    {"name": "laravel/framework", "version": "v10.48.0"},
    {"name": "guzzlehttp/guzzle", "version": "7.8.1"}
  ],
  "packages-dev": [
    {"name": "phpunit/phpunit", "version": "10.5.0"}
  ]
}`
	records := runExtract(t, "composer.lock", src)
	deps := depEntities(records)
	byName := map[string]string{}
	for _, d := range deps {
		byName[d.Name] = d.Properties["dependency_kind"]
	}
	if byName["laravel/framework"] != "locked" {
		t.Errorf("expected laravel/framework locked dep, got %q", byName["laravel/framework"])
	}
	if byName["phpunit/phpunit"] != "locked" {
		t.Errorf("expected phpunit/phpunit locked dev dep, got %q", byName["phpunit/phpunit"])
	}
	if _, ok := byName["guzzlehttp/guzzle"]; !ok {
		t.Error("expected guzzlehttp/guzzle dep")
	}
}

func TestComposerJSON_PackageManager(t *testing.T) {
	src := `{"require": {"slim/slim": "^4.0"}}`
	records := runExtract(t, "composer.json", src)
	for _, r := range records {
		pm := r.Properties["package_manager"]
		if pm != "" && pm != "composer" {
			t.Errorf("package_manager=%q want composer", pm)
		}
	}
}

// ---------------------------------------------------------------------------
// build.gradle / build.gradle.kts — Gradle (Java/Kotlin)
// ---------------------------------------------------------------------------

func TestGradle_GroovyDSL(t *testing.T) {
	src := `
dependencies {
    implementation 'org.springframework:spring-core:5.3.0'
    api "io.reactivex:rxjava:2.2.21"
    testImplementation 'junit:junit:4.13.2'
    compileOnly 'org.projectlombok:lombok:1.18.24'
}
`
	records := runExtract(t, "app/build.gradle", src)
	deps := depEntities(records)
	byName := map[string]types.EntityRecord{}
	for _, d := range deps {
		byName[d.Name] = d
	}
	// spring-core: group:artifact name, version, runtime scope, maven-style PM=gradle.
	spring, ok := byName["org.springframework:spring-core"]
	if !ok {
		t.Fatalf("expected org.springframework:spring-core dep, got %v", keysOf(byName))
	}
	if spring.Properties["version"] != "5.3.0" {
		t.Errorf("spring-core version=%q want 5.3.0", spring.Properties["version"])
	}
	if spring.Properties["package_manager"] != "gradle" {
		t.Errorf("spring-core package_manager=%q want gradle", spring.Properties["package_manager"])
	}
	if spring.Properties["dependency_kind"] != "runtime" {
		t.Errorf("spring-core dependency_kind=%q want runtime", spring.Properties["dependency_kind"])
	}
	// junit declared via testImplementation -> dev scope.
	junit, ok := byName["junit:junit"]
	if !ok {
		t.Fatalf("expected junit:junit dep")
	}
	if junit.Properties["is_dev"] != "true" {
		t.Errorf("junit is_dev=%q want true (testImplementation)", junit.Properties["is_dev"])
	}
	if junit.Properties["version"] != "4.13.2" {
		t.Errorf("junit version=%q want 4.13.2", junit.Properties["version"])
	}
	// rxjava declared with double-quotes + api config -> runtime.
	if byName["io.reactivex:rxjava"].Properties["version"] != "2.2.21" {
		t.Errorf("rxjava version=%q want 2.2.21", byName["io.reactivex:rxjava"].Properties["version"])
	}
}

func TestGradle_KotlinDSL(t *testing.T) {
	src := `
dependencies {
    implementation("com.google.guava:guava:31.0-jre")
    testImplementation("org.junit.jupiter:junit-jupiter:5.9.0")
}
`
	records := runExtract(t, "build.gradle.kts", src)
	deps := depEntities(records)
	byName := map[string]types.EntityRecord{}
	for _, d := range deps {
		byName[d.Name] = d
	}
	guava, ok := byName["com.google.guava:guava"]
	if !ok {
		t.Fatalf("expected com.google.guava:guava dep, got %v", keysOf(byName))
	}
	if guava.Properties["version"] != "31.0-jre" {
		t.Errorf("guava version=%q want 31.0-jre", guava.Properties["version"])
	}
	if guava.Properties["package_manager"] != "gradle" {
		t.Errorf("guava package_manager=%q want gradle", guava.Properties["package_manager"])
	}
	if byName["org.junit.jupiter:junit-jupiter"].Properties["is_dev"] != "true" {
		t.Errorf("junit-jupiter should be is_dev=true")
	}
}

// Negative: a quoted "a:b:c" string outside a recognised dependency
// configuration (e.g. a plugin id or a custom function) emits no package.
func TestGradle_IgnoresNonDependencyConfigs(t *testing.T) {
	src := `
plugins {
    id 'java'
}
someCustomTask 'org.evil:not-a-dep:1.0.0'
dependencies {
    implementation 'org.real:dep:1.0.0'
}
`
	records := runExtract(t, "build.gradle", src)
	deps := depEntities(records)
	byName := map[string]types.EntityRecord{}
	for _, d := range deps {
		byName[d.Name] = d
	}
	if _, ok := byName["org.evil:not-a-dep"]; ok {
		t.Errorf("custom-task coordinate must NOT be emitted as a dependency")
	}
	if _, ok := byName["org.real:dep"]; !ok {
		t.Errorf("real implementation dep must be emitted")
	}
}

func TestGradle_IsManifest(t *testing.T) {
	for _, p := range []string{"build.gradle", "app/build.gradle.kts"} {
		if !IsManifest(p) {
			t.Errorf("IsManifest(%q)=false want true", p)
		}
	}
}

// ---------------------------------------------------------------------------
// [sbom] Converged SCOPE.Package node + DEPENDS_ON_PACKAGE edge
// ---------------------------------------------------------------------------

// packageEntities returns the converged SCOPE.Package nodes.
func packageEntities(records []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range records {
		if r.Kind == "SCOPE.Package" {
			out = append(out, r)
		}
	}
	return out
}

// dependsOnPackageRels returns every DEPENDS_ON_PACKAGE edge across records.
func dependsOnPackageRels(records []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range records {
		for _, rel := range r.Relationships {
			if rel.Kind == "DEPENDS_ON_PACKAGE" {
				out = append(out, rel)
			}
		}
	}
	return out
}

func keysOf(m map[string]types.EntityRecord) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestSBOM_PackageNode_NPM(t *testing.T) {
	src := `{
  "dependencies": {"react": "^18.2.0"},
  "devDependencies": {"jest": "^29.0.0"}
}`
	records := runExtract(t, "frontend/package.json", src)
	pkgs := packageEntities(records)
	byName := map[string]types.EntityRecord{}
	for _, p := range pkgs {
		byName[p.Name] = p
	}

	react, ok := byName["package:npm:react"]
	if !ok {
		t.Fatalf("expected converged node package:npm:react, got %v", keysOf(byName))
	}
	// Node identity: file-agnostic synthetic SourceFile so cross-repo converges.
	if react.SourceFile != PackageSourceFile {
		t.Errorf("react SourceFile=%q want %q (synthetic)", react.SourceFile, PackageSourceFile)
	}
	if react.QualifiedName != "scope:package:npm:react" {
		t.Errorf("react QualifiedName=%q want scope:package:npm:react", react.QualifiedName)
	}
	if react.Properties["package"] != "react" {
		t.Errorf("react package prop=%q want react", react.Properties["package"])
	}
	// Version is NOT on the node (lives on the edge) — assert it's absent.
	if _, has := react.Properties["version"]; has {
		t.Errorf("node must NOT carry version (it is edge-scoped for convergence)")
	}

	// Edge: DEPENDS_ON_PACKAGE(project -> package:npm:react) version="^18.2.0" dev=false.
	rels := dependsOnPackageRels(records)
	byTo := map[string]types.RelationshipRecord{}
	for _, r := range rels {
		byTo[r.ToID] = r
	}
	rreact, ok := byTo["scope:package:npm:react"]
	if !ok {
		t.Fatalf("expected DEPENDS_ON_PACKAGE edge to react")
	}
	if rreact.FromID != "scope:component:project:frontend/package.json" {
		t.Errorf("edge FromID=%q want project anchor ref", rreact.FromID)
	}
	if rreact.Properties["version"] != "^18.2.0" {
		t.Errorf("react edge version=%q want ^18.2.0", rreact.Properties["version"])
	}
	if rreact.Properties["dev"] != "false" {
		t.Errorf("react edge dev=%q want false", rreact.Properties["dev"])
	}
	if rreact.Properties["package_manager"] != "npm" {
		t.Errorf("react edge package_manager=%q want npm", rreact.Properties["package_manager"])
	}
	// jest is a devDependency -> dev=true on its edge.
	jest, ok := byTo["scope:package:npm:jest"]
	if !ok {
		t.Fatalf("expected DEPENDS_ON_PACKAGE edge to jest")
	}
	if jest.Properties["dev"] != "true" {
		t.Errorf("jest edge dev=%q want true", jest.Properties["dev"])
	}
}

func TestSBOM_PackageNode_GoMod(t *testing.T) {
	src := `module example.com/app

go 1.21

require github.com/gin-gonic/gin v1.9.0
`
	records := runExtract(t, "go.mod", src)
	pkgs := packageEntities(records)
	var gin *types.EntityRecord
	for i := range pkgs {
		if pkgs[i].Name == "package:go_modules:github.com/gin-gonic/gin" {
			gin = &pkgs[i]
		}
	}
	if gin == nil {
		t.Fatalf("expected converged node for gin")
	}
	if gin.QualifiedName != "scope:package:go_modules:github.com/gin-gonic/gin" {
		t.Errorf("gin QualifiedName=%q", gin.QualifiedName)
	}
	rels := dependsOnPackageRels(records)
	var found bool
	for _, r := range rels {
		if r.ToID == "scope:package:go_modules:github.com/gin-gonic/gin" {
			found = true
			if r.Properties["version"] != "v1.9.0" {
				t.Errorf("gin edge version=%q want v1.9.0", r.Properties["version"])
			}
		}
	}
	if !found {
		t.Fatalf("expected DEPENDS_ON_PACKAGE edge to gin")
	}
}

// Convergence: the SAME ecosystem:name declared in two different manifest
// files produces SCOPE.Package nodes with the IDENTICAL ComputeID — i.e. they
// collapse to one node when merged into the graph (cross-repo SBOM).
func TestSBOM_CrossFileConvergence(t *testing.T) {
	srcA := `{"dependencies": {"lodash": "^4.17.21"}}`
	srcB := `{"dependencies": {"lodash": "^4.17.0"}}`
	recA := runExtract(t, "repo-a/package.json", srcA)
	recB := runExtract(t, "repo-b/package.json", srcB)

	idOf := func(recs []types.EntityRecord) string {
		for _, r := range recs {
			if r.Kind == "SCOPE.Package" && r.Name == "package:npm:lodash" {
				return r.ID
			}
		}
		return ""
	}
	idA := idOf(recA)
	idB := idOf(recB)
	if idA == "" || idB == "" {
		t.Fatalf("missing lodash package node: idA=%q idB=%q", idA, idB)
	}
	if idA != idB {
		t.Errorf("convergence broken: idA=%q idB=%q (same ecosystem:name must share ID)", idA, idB)
	}
}

func TestSBOM_MavenPackageNode(t *testing.T) {
	src := `<project>
  <dependencies>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-core</artifactId>
      <version>5.3.0</version>
    </dependency>
  </dependencies>
</project>`
	records := runExtract(t, "pom.xml", src)
	pkgs := packageEntities(records)
	var found bool
	for _, p := range pkgs {
		if p.Name == "package:maven:org.springframework:spring-core" {
			found = true
			if p.QualifiedName != "scope:package:maven:org.springframework:spring-core" {
				t.Errorf("maven QualifiedName=%q", p.QualifiedName)
			}
		}
	}
	if !found {
		t.Fatalf("expected converged maven node for spring-core, got %d package nodes", len(pkgs))
	}
	rels := dependsOnPackageRels(records)
	var edgeFound bool
	for _, r := range rels {
		if r.ToID == "scope:package:maven:org.springframework:spring-core" {
			edgeFound = true
			if r.Properties["version"] != "5.3.0" {
				t.Errorf("spring-core edge version=%q want 5.3.0", r.Properties["version"])
			}
		}
	}
	if !edgeFound {
		t.Fatalf("expected DEPENDS_ON_PACKAGE edge to spring-core")
	}
}
