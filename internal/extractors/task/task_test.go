package task

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func TestIsTaskfile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"Taskfile.yml", true},
		{"Taskfile.yaml", true},
		{"taskfile.yml", true},
		{"taskfile.yaml", true},
		{"sub/Taskfile.yml", true},
		{"Taskfile.txt", false},
		{"docker-compose.yml", false},
		{"Taskfile", false},
	}
	for _, c := range cases {
		if got := IsTaskfile(c.path); got != c.want {
			t.Errorf("IsTaskfile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestParseTaskfile(t *testing.T) {
	src := []byte(`version: '3'

includes:
  docker: ./docker/Taskfile.yml
  ci:
    taskfile: ./ci/Taskfile.yml
    dir: ./ci

tasks:
  default:
    cmds:
      - task: build

  build:
    deps: [generate, lint]
    cmds:
      - go build ./...

  lint:
    cmds:
      - golangci-lint run

  generate:
    cmds:
      - go generate ./...

  release:
    deps:
      - task: build
        vars:
          OS: linux
    cmds:
      - echo done
`)
	tf, ok := ParseTaskfile(src)
	if !ok {
		t.Fatal("ParseTaskfile returned ok=false")
	}

	// Includes.
	incByNS := map[string]string{}
	for _, inc := range tf.Includes {
		incByNS[inc.Namespace] = inc.Path
	}
	if incByNS["docker"] != "./docker/Taskfile.yml" {
		t.Errorf("docker include path = %q", incByNS["docker"])
	}
	if incByNS["ci"] != "./ci/Taskfile.yml" {
		t.Errorf("ci include path = %q (want taskfile: value)", incByNS["ci"])
	}

	deps := map[string][]string{}
	for _, tk := range tf.Tasks {
		deps[tk.Name] = tk.Deps
	}
	if len(tf.Tasks) != 5 {
		t.Errorf("task count = %d, want 5", len(tf.Tasks))
	}
	// default: { task: build } cmd.
	if got := deps["default"]; len(got) != 1 || got[0] != "build" {
		t.Errorf("default deps = %v, want [build]", got)
	}
	// build: deps: [generate, lint].
	bset := map[string]bool{}
	for _, d := range deps["build"] {
		bset[d] = true
	}
	if !bset["generate"] || !bset["lint"] {
		t.Errorf("build deps = %v, want generate+lint", deps["build"])
	}
	// release: deps: [{ task: build, vars: ... }].
	if got := deps["release"]; len(got) != 1 || got[0] != "build" {
		t.Errorf("release deps = %v, want [build]", got)
	}
}

func TestParseTaskfile_NotATaskfile(t *testing.T) {
	if _, ok := ParseTaskfile([]byte("foo: bar\nbaz: 1\n")); ok {
		t.Error("a YAML without tasks:/includes: should return ok=false")
	}
	if _, ok := ParseTaskfile([]byte(":::not yaml:::")); ok {
		t.Error("invalid YAML should return ok=false")
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Taskfile.yml", `version: '3'

includes:
  sub: ./sub/Taskfile.yml

tasks:
  build:
    deps: [lint]
    cmds:
      - go build ./...
  lint:
    cmds:
      - golangci-lint run
  ci:
    cmds:
      - task: build
      - task: sub:deploy
`)

	ents, rels, err := Discover(context.Background(), root, []string{"Taskfile.yml"})
	if err != nil {
		t.Fatal(err)
	}

	var taskfiles, tasks int
	idByName := map[string]string{}
	var fileEnt *types.EntityRecord
	for i := range ents {
		e := &ents[i]
		switch e.Subtype {
		case "taskfile":
			taskfiles++
			fileEnt = e
		case "task":
			tasks++
			idByName[e.Name] = e.ID
			if e.Kind != string(types.EntityKindOperation) {
				t.Errorf("task kind = %q, want SCOPE.Operation", e.Kind)
			}
		}
	}
	if taskfiles != 1 {
		t.Errorf("taskfiles = %d, want 1", taskfiles)
	}
	if tasks != 3 {
		t.Errorf("tasks = %d, want 3", tasks)
	}

	// CONTAINS (3) + IMPORTS (1) on the file entity.
	var contains, imports int
	for _, r := range fileEnt.Relationships {
		switch r.Kind {
		case "CONTAINS":
			contains++
		case "IMPORTS":
			imports++
			if r.Properties["include_namespace"] != "sub" {
				t.Errorf("import namespace = %q, want sub", r.Properties["include_namespace"])
			}
		}
	}
	if contains != 3 {
		t.Errorf("CONTAINS edges = %d, want 3", contains)
	}
	if imports != 1 {
		t.Errorf("IMPORTS edges = %d, want 1", imports)
	}

	// build→lint (resolved), ci→build (resolved), ci→sub:deploy (synthetic ext).
	var buildLint, ciBuild, ciExt bool
	for _, r := range rels {
		if r.Kind != RelationshipKindTaskDependsOn {
			t.Errorf("unexpected rel kind %q", r.Kind)
		}
		switch {
		case r.FromID == idByName["build"] && r.ToID == idByName["lint"]:
			buildLint = true
		case r.FromID == idByName["ci"] && r.ToID == idByName["build"]:
			ciBuild = true
		case r.FromID == idByName["ci"] && r.Properties["dep_task"] == "sub:deploy":
			ciExt = true
		}
	}
	if !buildLint {
		t.Error("missing build → lint edge")
	}
	if !ciBuild {
		t.Error("missing ci → build edge")
	}
	if !ciExt {
		t.Error("missing ci → sub:deploy (external) edge")
	}

	// Determinism.
	ents2, _, _ := Discover(context.Background(), root, []string{"Taskfile.yml"})
	for i := range ents {
		if ents[i].ID != ents2[i].ID {
			t.Errorf("non-deterministic ID at %d", i)
		}
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
