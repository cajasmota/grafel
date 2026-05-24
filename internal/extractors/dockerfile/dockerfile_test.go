package dockerfile_test

import (
	"context"
	"strings"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsdockerfile "github.com/smacker/go-tree-sitter/dockerfile"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/dockerfile"
	"github.com/cajasmota/archigraph/internal/types"
)

func parseForTest(t *testing.T, src string) *sitter.Tree {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(tsdockerfile.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func extractEntities(t *testing.T, path, src string, tree *sitter.Tree) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("dockerfile")
	if !ok {
		t.Fatal("dockerfile extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "dockerfile",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return entities
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func TestDockerfileExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("dockerfile")
	if !ok {
		t.Fatal("dockerfile extractor not registered under key 'dockerfile'")
	}
}

func TestDockerfileExtractor_Language(t *testing.T) {
	ext, _ := extractor.Get("dockerfile")
	if ext.Language() != "dockerfile" {
		t.Errorf("expected Language()='dockerfile', got %q", ext.Language())
	}
}

// ---------------------------------------------------------------------------
// Empty / nil input
// ---------------------------------------------------------------------------

func TestDockerfileExtractor_EmptyContent(t *testing.T) {
	ext, _ := extractor.Get("dockerfile")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Dockerfile",
		Content:  []byte{},
		Language: "dockerfile",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities for empty content, got %d", len(entities))
	}
}

func TestDockerfileExtractor_NilTree(t *testing.T) {
	ext, _ := extractor.Get("dockerfile")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Dockerfile",
		Content:  []byte("FROM ubuntu:22.04\n"),
		Language: "dockerfile",
		Tree:     nil, // nil tree → empty result per spec
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities for nil tree, got %d", len(entities))
	}
}

// ---------------------------------------------------------------------------
// #2063 — single entity per Dockerfile (no orphan instruction entities)
// ---------------------------------------------------------------------------

// TestDockerfileExtractor_SingleEntity_SingleStage verifies that a single-stage
// Dockerfile emits exactly one entity of subtype "dockerfile".
func TestDockerfileExtractor_SingleEntity_SingleStage(t *testing.T) {
	src := `FROM ubuntu:22.04
RUN apt-get update
EXPOSE 8080
ENV PORT=8080
ARG BUILD_VERSION
CMD ["/app/server"]
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	if len(entities) != 1 {
		t.Fatalf("expected exactly 1 entity, got %d: %+v", len(entities), entities)
	}
	e := entities[0]
	if e.Kind != "SCOPE.Component" {
		t.Errorf("expected Kind=SCOPE.Component, got %q", e.Kind)
	}
	if e.Subtype != "dockerfile" {
		t.Errorf("expected Subtype=dockerfile, got %q", e.Subtype)
	}
}

// TestDockerfileExtractor_SingleEntity_MultiStage is the regression test for
// #2063: a 3-stage Dockerfile must emit exactly 1 entity, not 3+ instruction
// entities. This was the root cause of ~118 orphans in polyglot-platform.
func TestDockerfileExtractor_SingleEntity_MultiStage(t *testing.T) {
	src := `FROM golang:1.22 AS deps
RUN go mod download

FROM golang:1.22 AS builder
COPY --from=deps /go/pkg /go/pkg
RUN go build -o /app/bin ./...

FROM ubuntu:22.04 AS runtime
COPY --from=builder /app/bin /usr/local/bin
EXPOSE 9090
ENTRYPOINT ["/usr/local/bin/server"]
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	if len(entities) != 1 {
		t.Fatalf("#2063 regression: expected exactly 1 entity for 3-stage Dockerfile, got %d: %+v",
			len(entities), entities)
	}
}

// ---------------------------------------------------------------------------
// Properties encoding
// ---------------------------------------------------------------------------

// TestDockerfileExtractor_Properties_Stages verifies stages property.
func TestDockerfileExtractor_Properties_Stages(t *testing.T) {
	src := `FROM golang:1.22 AS builder
FROM ubuntu:22.04 AS runtime
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	e := entities[0]
	if e.Properties["stages"] == "" {
		t.Error("expected non-empty stages property")
	}
	stages := strings.Split(e.Properties["stages"], ",")
	wantStages := map[string]bool{"golang:1.22": false, "ubuntu:22.04": false}
	for _, s := range stages {
		if _, ok := wantStages[s]; ok {
			wantStages[s] = true
		}
	}
	for img, found := range wantStages {
		if !found {
			t.Errorf("expected stage %q in properties.stages=%q", img, e.Properties["stages"])
		}
	}
}

// TestDockerfileExtractor_Properties_RunCommands verifies run_commands property.
func TestDockerfileExtractor_Properties_RunCommands(t *testing.T) {
	src := `FROM ubuntu:22.04
RUN apt-get update
RUN apt-get install -y curl
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	e := entities[0]
	if e.Properties["run_commands"] == "" {
		t.Error("expected non-empty run_commands property")
	}
	// Should be a JSON array with 2 entries.
	if !strings.Contains(e.Properties["run_commands"], "apt-get update") {
		t.Errorf("run_commands missing apt-get update: %q", e.Properties["run_commands"])
	}
}

// TestDockerfileExtractor_Properties_ExposedPorts verifies exposed_ports property.
func TestDockerfileExtractor_Properties_ExposedPorts(t *testing.T) {
	src := `FROM ubuntu:22.04
EXPOSE 8080
EXPOSE 9090
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	e := entities[0]
	ports := e.Properties["exposed_ports"]
	if !strings.Contains(ports, "8080") {
		t.Errorf("expected 8080 in exposed_ports, got %q", ports)
	}
	if !strings.Contains(ports, "9090") {
		t.Errorf("expected 9090 in exposed_ports, got %q", ports)
	}
}

// TestDockerfileExtractor_Properties_EnvAndArgs verifies env_vars and build_args.
func TestDockerfileExtractor_Properties_EnvAndArgs(t *testing.T) {
	src := `FROM python:3.11
ARG APP_VERSION=latest
ARG TARGETPLATFORM
ENV PYTHONUNBUFFERED=1
ENV LOG_LEVEL=info
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	e := entities[0]
	args := e.Properties["build_args"]
	for _, want := range []string{"APP_VERSION", "TARGETPLATFORM"} {
		if !strings.Contains(args, want) {
			t.Errorf("expected build_arg %q in properties.build_args=%q", want, args)
		}
	}
	envs := e.Properties["env_vars"]
	for _, want := range []string{"PYTHONUNBUFFERED", "LOG_LEVEL"} {
		if !strings.Contains(envs, want) {
			t.Errorf("expected env var %q in properties.env_vars=%q", want, envs)
		}
	}
}

// TestDockerfileExtractor_Properties_Entrypoint verifies entrypoint property.
func TestDockerfileExtractor_Properties_Entrypoint(t *testing.T) {
	src := `FROM alpine:3.18
ENTRYPOINT ["/entrypoint.sh"]
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	e := entities[0]
	if e.Properties["entrypoint"] == "" {
		t.Error("expected non-empty entrypoint property")
	}
}

// ---------------------------------------------------------------------------
// QualityScore >= 0.6 on all entities
// ---------------------------------------------------------------------------

func TestDockerfileExtractor_QualityScore(t *testing.T) {
	src := `FROM node:18 AS app
RUN npm install
COPY package.json /app/
EXPOSE 3000
ENV NODE_ENV=production
ARG NPM_TOKEN
CMD ["node", "server.js"]
ENTRYPOINT ["/docker-entrypoint.sh"]
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	if len(entities) == 0 {
		t.Fatal("expected at least one entity")
	}
	for _, e := range entities {
		if e.QualityScore < 0.6 {
			t.Errorf("entity %q (subtype=%q): QualityScore=%.2f below 0.6", e.Name, e.Subtype, e.QualityScore)
		}
	}
}

// ---------------------------------------------------------------------------
// Language field on all entities
// ---------------------------------------------------------------------------

func TestDockerfileExtractor_LanguageField(t *testing.T) {
	src := "FROM scratch\n"
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	for _, e := range entities {
		if e.Language != "dockerfile" {
			t.Errorf("entity %q: expected Language='dockerfile', got %q", e.Name, e.Language)
		}
	}
}

// ---------------------------------------------------------------------------
// SourceFile propagation
// ---------------------------------------------------------------------------

func TestDockerfileExtractor_SourceFile(t *testing.T) {
	src := "FROM alpine:3.18\n"
	tree := parseForTest(t, src)
	entities := extractEntities(t, "docker/Dockerfile.prod", src, tree)

	for _, e := range entities {
		if e.SourceFile != "docker/Dockerfile.prod" {
			t.Errorf("entity %q: expected SourceFile='docker/Dockerfile.prod', got %q", e.Name, e.SourceFile)
		}
	}
}

// ---------------------------------------------------------------------------
// FROM without tag (e.g. FROM scratch)
// ---------------------------------------------------------------------------

func TestDockerfileExtractor_FromScratch(t *testing.T) {
	src := "FROM scratch\n"
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	e := entities[0]
	if e.Subtype != "dockerfile" {
		t.Errorf("expected Subtype='dockerfile', got %q", e.Subtype)
	}
	if !strings.Contains(e.Properties["stages"], "scratch") {
		t.Errorf("expected stages to contain 'scratch', got %q", e.Properties["stages"])
	}
}

// ---------------------------------------------------------------------------
// Degenerate: no FROM instruction
// ---------------------------------------------------------------------------

func TestDockerfileExtractor_NoFromInstruction(t *testing.T) {
	// A Dockerfile with only comments and no FROM → 0 entities.
	src := "# only a comment\n"
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	if len(entities) != 0 {
		t.Errorf("expected 0 entities for Dockerfile with no FROM, got %d", len(entities))
	}
}
