package groovy_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/groovy"
	"github.com/cajasmota/grafel/internal/types"
)

// countBySubtype returns entity counts grouped by subtype.
func countBySubtype(entities []types.EntityRecord) map[string]int {
	m := make(map[string]int)
	for _, e := range entities {
		m[e.Subtype]++
	}
	return m
}

// entityNames returns all names that match the given subtype.
func entityNames(entities []types.EntityRecord, subtype string) []string {
	var names []string
	for _, e := range entities {
		if e.Subtype == subtype {
			names = append(names, e.Name)
		}
	}
	return names
}

// hasName returns true if any entity in the list has the given name.
func hasName(entities []types.EntityRecord, name string) bool {
	for _, e := range entities {
		if e.Name == name {
			return true
		}
	}
	return false
}

// hasNameAndSubtype returns true if any entity matches both name and subtype.
func hasNameAndSubtype(entities []types.EntityRecord, name, subtype string) bool {
	for _, e := range entities {
		if e.Name == name && e.Subtype == subtype {
			return true
		}
	}
	return false
}

// --- Grails application class ---

func TestGrailsApplicationClass_MinEntityCount(t *testing.T) {
	// AC1: Grails application class fixture produces >=3 entities.
	src := `package myapp
import grails.boot.GrailsApp
import grails.boot.config.GrailsAutoConfiguration

@GrailsApplication
class Application extends GrailsAutoConfiguration {
    static void main(String[] args) {
        GrailsApp.run(Application, args)
    }
    void afterStart() {
        println "Application started"
    }
    void doWithApplicationContext() {
        println "Spring context ready"
    }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "grails_application_class.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) < 3 {
		t.Fatalf("AC1: expected >=3 entities from Grails application class, got %d: %v", len(entities), entityNames(entities, ""))
	}
}

func TestGrailsApplicationClass_ClassEntity(t *testing.T) {
	src := `@GrailsApplication
class Application extends GrailsAutoConfiguration {
    void main(String[] args) {}
    void afterStart() {}
    void doWithApplicationContext() {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Application.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasNameAndSubtype(entities, "Application", "class") {
		t.Errorf("expected class entity 'Application'; got subtypes: %v", countBySubtype(entities))
	}
}

func TestGrailsApplicationClass_AllowlistKinds(t *testing.T) {
	src := `@GrailsApplication
class Application extends GrailsAutoConfiguration {
    void afterStart() {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Application.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	allowlist := map[string]bool{
		"SCOPE.Component": true,
		"SCOPE.Operation": true,
	}
	for _, e := range entities {
		if !allowlist[e.Kind] {
			t.Errorf("entity %q has Kind %q which is not in allowlist", e.Name, e.Kind)
		}
	}
}

func TestGrailsApplicationClass_MethodEntities(t *testing.T) {
	src := `class Application extends GrailsAutoConfiguration {
    void afterStart() {
        println "started"
    }
    void doWithApplicationContext() {
        println "context ready"
    }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Application.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// afterStart and doWithApplicationContext should be extracted as methods.
	wantMethods := []string{"afterStart", "doWithApplicationContext"}
	for _, m := range wantMethods {
		if !hasNameAndSubtype(entities, m, "method") {
			t.Errorf("expected method entity %q; got: %v", m, entityNames(entities, "method"))
		}
	}
}

// --- Grails lifecycle (BootStrap + service class) ---

func TestGrailsApplicationLifecycle_MinEntityCount(t *testing.T) {
	src := `package myapp

class BootStrap {
    def init = { servletContext ->
        println "init"
    }
    def destroy = {
        println "destroy"
    }
}

class MyGrailsService {
    def serviceMethod() {
        return "service result"
    }
    void doSomething(String input) {
        println input
    }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "grails_application_lifecycle.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) < 2 {
		t.Fatalf("expected >=2 entities from lifecycle fixture, got %d", len(entities))
	}
}

func TestGrailsServiceMethods_KindAndSubtype(t *testing.T) {
	// Grails service methods (def methodName, void methodName) → kind=method.
	src := `class MyGrailsService {
    def serviceMethod() {
        return "result"
    }
    void doSomething(String input) {
        println input
    }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "MyGrailsService.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.Subtype == "method" && e.Kind != "SCOPE.Operation" {
			t.Errorf("method %q: expected Kind=SCOPE.Operation, got %q", e.Name, e.Kind)
		}
	}
	wantMethods := []string{"serviceMethod", "doSomething"}
	for _, m := range wantMethods {
		if !hasNameAndSubtype(entities, m, "method") {
			t.Errorf("expected method %q; got methods: %v", m, entityNames(entities, "method"))
		}
	}
}

// --- Gradle plugin DSL ---

func TestGradlePlugin_ApplyPlugin_BasicDetection(t *testing.T) {
	// AC2: Gradle plugin files detect plugin ID.
	src := `apply plugin: 'java'
apply plugin: 'com.example.my-gradle-plugin'
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "build.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasNameAndSubtype(entities, "java", "plugin_id") {
		t.Errorf("expected plugin_id 'java'; got: %v", entityNames(entities, "plugin_id"))
	}
	if !hasNameAndSubtype(entities, "com.example.my-gradle-plugin", "plugin_id") {
		t.Errorf("expected plugin_id 'com.example.my-gradle-plugin'; got: %v", entityNames(entities, "plugin_id"))
	}
}

func TestGradlePlugin_ApplyPlugin_Kind(t *testing.T) {
	src := `apply plugin: 'groovy'
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "build.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.Subtype == "plugin_id" && e.Kind != "SCOPE.Component" {
			t.Errorf("plugin_id %q: expected Kind=SCOPE.Component, got %q", e.Name, e.Kind)
		}
	}
	if !hasName(entities, "groovy") {
		t.Errorf("expected plugin_id entity 'groovy'; got: %v", entityNames(entities, "plugin_id"))
	}
}

func TestGradlePlugin_TaskDeclaration_Basic(t *testing.T) {
	// AC2: Gradle plugin files detect task definitions.
	src := `task clean {
    doLast {
        delete buildDir
    }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "build.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasNameAndSubtype(entities, "clean", "task") {
		t.Errorf("expected task entity 'clean'; got subtypes: %v", countBySubtype(entities))
	}
}

func TestGradlePlugin_TaskDeclaration_Kind(t *testing.T) {
	src := `task myTask {
    doLast { println "done" }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "build.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.Subtype == "task" && e.Kind != "SCOPE.Operation" {
			t.Errorf("task %q: expected Kind=SCOPE.Operation, got %q", e.Name, e.Kind)
		}
	}
}

func TestGradlePlugin_TaskWithType(t *testing.T) {
	// Gradle `task compileGroovy(type: GroovyCompile)` pattern.
	src := `task compileGroovy(type: GroovyCompile) {
    source = sourceSets.main.groovy
}
task buildJar(type: Jar) {
    archiveName = 'my-app.jar'
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "build.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasNameAndSubtype(entities, "compileGroovy", "task") {
		t.Errorf("expected task entity 'compileGroovy'; got: %v", entityNames(entities, "task"))
	}
	if !hasNameAndSubtype(entities, "buildJar", "task") {
		t.Errorf("expected task entity 'buildJar'; got: %v", entityNames(entities, "task"))
	}
}

func TestGradlePlugin_FullFixture_EntityCounts(t *testing.T) {
	// Full gradle_plugin.groovy fixture: 2 apply plugin + 4 task defs.
	// apply plugin: 'java', apply plugin: 'com.example.my-gradle-plugin', apply plugin: 'groovy'
	// task clean, task compileGroovy(type:), task buildJar(type:), task integrationTest(dependsOn:)
	src := `apply plugin: 'java'
apply plugin: 'com.example.my-gradle-plugin'
apply plugin: 'groovy'

task clean {
    doLast {
        delete buildDir
    }
}

task compileGroovy(type: GroovyCompile) {
    source = sourceSets.main.groovy
    destinationDir = file("${buildDir}/classes")
}

task buildJar(type: Jar) {
    archiveName = 'my-app.jar'
    from sourceSets.main.output
}

task integrationTest(dependsOn: compileGroovy) {
    doLast {
        println "Running integration tests"
    }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "gradle_plugin.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	counts := countBySubtype(entities)
	if counts["plugin_id"] < 2 {
		t.Errorf("expected >=2 plugin_id entities, got %d: %v", counts["plugin_id"], entityNames(entities, "plugin_id"))
	}
	if counts["task"] < 4 {
		t.Errorf("expected >=4 task entities, got %d: %v", counts["task"], entityNames(entities, "task"))
	}
}

func TestGradlePlugin_AllowlistCompliant(t *testing.T) {
	src := `apply plugin: 'java'
task clean { doLast { } }
task build(type: Jar) { }
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "build.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	allowlist := map[string]bool{
		"SCOPE.Component": true,
		"SCOPE.Operation": true,
	}
	for _, e := range entities {
		if !allowlist[e.Kind] {
			t.Errorf("entity %q subtype=%q has Kind %q not in allowlist", e.Name, e.Subtype, e.Kind)
		}
	}
}

func TestGradlePlugin_Language(t *testing.T) {
	src := `apply plugin: 'java'
task test { }
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "build.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.Language != "groovy" {
			t.Errorf("entity %q: expected Language=groovy, got %q", e.Name, e.Language)
		}
	}
}

func TestGradlePlugin_NoFalsePositiveApply(t *testing.T) {
	// `apply` with a non-plugin argument should not produce plugin_id entities.
	src := `apply from: 'other.gradle'
apply {
    plugin SomePlugin
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "build.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.Subtype == "plugin_id" {
			t.Errorf("unexpected plugin_id entity %q from non-plugin apply statement", e.Name)
		}
	}
}

func TestGradlePlugin_GradlePropertySignature(t *testing.T) {
	// task entities must have a "task <name>" Signature.
	src := `task myTask {
    doLast { println "hi" }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "build.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.Subtype == "task" && e.Name == "myTask" {
			if e.Signature != "task myTask" {
				t.Errorf("task myTask: expected Signature 'task myTask', got %q", e.Signature)
			}
		}
	}
}

func TestGradlePlugin_PluginIDSignature(t *testing.T) {
	src := `apply plugin: 'com.example.plugin'
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "build.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.Subtype == "plugin_id" && e.Name == "com.example.plugin" {
			expected := "apply plugin: 'com.example.plugin'"
			if e.Signature != expected {
				t.Errorf("plugin_id signature: expected %q, got %q", expected, e.Signature)
			}
		}
	}
}
