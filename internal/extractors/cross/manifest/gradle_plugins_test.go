package manifest

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// pluginDepByName returns the external_dependency entity with the given Name, or nil.
func pluginDepByName(records []types.EntityRecord, name string) *types.EntityRecord {
	for i := range records {
		r := &records[i]
		if r.Kind == "SCOPE.Component" && r.Subtype == "external_dependency" && r.Name == name {
			return r
		}
	}
	return nil
}

// The modern `plugins { id '…' version '…' }` block is parsed into plugin
// dependencies (dependency_kind=plugin) carrying the declared version (#5364).
func TestGradle_PluginsBlock(t *testing.T) {
	src := `plugins {
    id 'java'
    id 'org.springframework.boot' version '3.1.0'
    id("io.spring.dependency-management") version("1.1.0")
}

dependencies {
    implementation 'org.springframework:spring-core:6.0.0'
}
`
	records := runExtract(t, "build.gradle", src)

	boot := pluginDepByName(records, "org.springframework.boot")
	if boot == nil {
		t.Fatalf("expected plugin org.springframework.boot; got %v", depEntities(records))
	}
	if boot.Properties["version"] != "3.1.0" {
		t.Errorf("boot version=%q want 3.1.0", boot.Properties["version"])
	}
	if boot.Properties["dependency_kind"] != "plugin" {
		t.Errorf("boot dependency_kind=%q want plugin", boot.Properties["dependency_kind"])
	}
	if boot.Properties["package_manager"] != "gradle" {
		t.Errorf("boot package_manager=%q want gradle", boot.Properties["package_manager"])
	}

	// Paren/quote-style id with version.
	dm := pluginDepByName(records, "io.spring.dependency-management")
	if dm == nil {
		t.Fatalf("expected plugin io.spring.dependency-management; got %v", depEntities(records))
	}
	if dm.Properties["version"] != "1.1.0" {
		t.Errorf("dependency-management version=%q want 1.1.0", dm.Properties["version"])
	}

	// Versionless core plugin id is still captured (empty version).
	if java := pluginDepByName(records, "java"); java == nil {
		t.Errorf("expected versionless plugin 'java'; got %v", depEntities(records))
	}

	// The regular dependency in the dependencies block still parses alongside.
	if pluginDepByName(records, "org.springframework:spring-core") == nil {
		t.Errorf("expected spring-core dependency alongside plugins; got %v", depEntities(records))
	}
}

// A plugins block with no id entries (or no block) leaves dependency parsing
// untouched — no spurious plugin deps.
func TestGradle_NoPluginsBlock(t *testing.T) {
	src := `dependencies {
    implementation 'com.google.guava:guava:31.0-jre'
}
`
	records := runExtract(t, "build.gradle", src)
	for _, d := range depEntities(records) {
		if d.Properties["dependency_kind"] == "plugin" {
			t.Errorf("unexpected plugin dep %q with no plugins block", d.Name)
		}
	}
}
