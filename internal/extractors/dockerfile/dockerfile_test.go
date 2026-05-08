package dockerfile_test

import (
	"context"
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
// Single-stage Dockerfile
// ---------------------------------------------------------------------------

func TestDockerfileExtractor_SingleStage(t *testing.T) {
	src := `FROM ubuntu:22.04
RUN apt-get update
EXPOSE 8080
ENV PORT=8080
ARG BUILD_VERSION
CMD ["/app/server"]
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	bySubtype := make(map[string][]string)
	for _, e := range entities {
		bySubtype[e.Subtype] = append(bySubtype[e.Subtype], e.Name)
	}

	if len(bySubtype["stage"]) != 1 {
		t.Errorf("expected 1 stage, got %v", bySubtype["stage"])
	}
	if len(bySubtype["stage"]) > 0 && bySubtype["stage"][0] != "ubuntu:22.04" {
		t.Errorf("expected stage 'ubuntu:22.04', got %q", bySubtype["stage"][0])
	}
	if len(bySubtype["run"]) != 1 {
		t.Errorf("expected 1 run entity, got %v", bySubtype["run"])
	}
	if len(bySubtype["port"]) != 1 {
		t.Errorf("expected 1 port entity, got %v", bySubtype["port"])
	}
	if len(bySubtype["port"]) > 0 && bySubtype["port"][0] != "8080" {
		t.Errorf("expected port '8080', got %q", bySubtype["port"][0])
	}
	if len(bySubtype["variable"]) != 1 {
		t.Errorf("expected 1 env variable, got %v", bySubtype["variable"])
	}
	if len(bySubtype["variable"]) > 0 && bySubtype["variable"][0] != "PORT" {
		t.Errorf("expected env var 'PORT', got %q", bySubtype["variable"][0])
	}
	if len(bySubtype["build_arg"]) != 1 {
		t.Errorf("expected 1 build_arg, got %v", bySubtype["build_arg"])
	}
	if len(bySubtype["build_arg"]) > 0 && bySubtype["build_arg"][0] != "BUILD_VERSION" {
		t.Errorf("expected build_arg 'BUILD_VERSION', got %q", bySubtype["build_arg"][0])
	}
	if len(bySubtype["entrypoint"]) != 1 {
		t.Errorf("expected 1 entrypoint entity, got %v", bySubtype["entrypoint"])
	}
	if len(bySubtype["entrypoint"]) > 0 && bySubtype["entrypoint"][0] != "CMD" {
		t.Errorf("expected entrypoint name 'CMD', got %q", bySubtype["entrypoint"][0])
	}
}

// ---------------------------------------------------------------------------
// Multi-stage Dockerfile — stage tagging (AC #2)
// ---------------------------------------------------------------------------

func TestDockerfileExtractor_MultiStage(t *testing.T) {
	src := `FROM golang:1.22 AS builder
RUN go build ./...
COPY . /src

FROM ubuntu:22.04 AS runtime
COPY --from=builder /app/bin /usr/local/bin
EXPOSE 9090
ENTRYPOINT ["/usr/local/bin/server"]
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	// Collect stage aliases from FROM entities.
	stages := make(map[string]bool)
	for _, e := range entities {
		if e.Subtype == "stage" {
			if e.Properties != nil {
				if alias, ok := e.Properties["alias"]; ok && alias != "" {
					stages[alias] = true
				}
			}
		}
	}
	for _, want := range []string{"builder", "runtime"} {
		if !stages[want] {
			t.Errorf("expected stage alias %q in FROM entities", want)
		}
	}

	// Entities after the second FROM should have stage="runtime".
	for _, e := range entities {
		if e.Subtype == "port" && e.Name == "9090" {
			if e.Properties == nil || e.Properties["stage"] != "runtime" {
				t.Errorf("EXPOSE 9090: expected stage='runtime', got %q", e.Properties["stage"])
			}
		}
		if e.Subtype == "run" {
			if e.Properties == nil || e.Properties["stage"] != "builder" {
				t.Errorf("RUN: expected stage='builder', got %q", e.Properties["stage"])
			}
		}
	}
}

// ---------------------------------------------------------------------------
// COPY and ADD instructions
// ---------------------------------------------------------------------------

func TestDockerfileExtractor_CopyAndAdd(t *testing.T) {
	src := `FROM alpine:3.18
COPY src/ /app/src/
ADD config.tar.gz /app/config/
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	copyNames := make(map[string]bool)
	for _, e := range entities {
		if e.Subtype == "copy" {
			copyNames[e.Name] = true
		}
	}
	if !copyNames["COPY"] {
		t.Error("expected COPY entity")
	}
	if !copyNames["ADD"] {
		t.Error("expected ADD entity")
	}
}

// ---------------------------------------------------------------------------
// ENTRYPOINT instruction
// ---------------------------------------------------------------------------

func TestDockerfileExtractor_Entrypoint(t *testing.T) {
	src := `FROM alpine:3.18
ENTRYPOINT ["/entrypoint.sh"]
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	found := false
	for _, e := range entities {
		if e.Subtype == "entrypoint" && e.Name == "ENTRYPOINT" {
			found = true
			if e.Kind != "SCOPE.Operation" {
				t.Errorf("expected Kind=SCOPE.Operation, got %q", e.Kind)
			}
		}
	}
	if !found {
		t.Error("expected ENTRYPOINT entity")
	}
}

// ---------------------------------------------------------------------------
// ARG / ENV extraction
// ---------------------------------------------------------------------------

func TestDockerfileExtractor_ArgAndEnv(t *testing.T) {
	src := `FROM python:3.11
ARG APP_VERSION=latest
ARG TARGETPLATFORM
ENV PYTHONUNBUFFERED=1
ENV LOG_LEVEL=info
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	args := make(map[string]bool)
	envVars := make(map[string]bool)
	for _, e := range entities {
		switch e.Subtype {
		case "build_arg":
			args[e.Name] = true
			if e.Kind != "SCOPE.Schema" {
				t.Errorf("ARG %q: expected Kind=SCOPE.Schema, got %q", e.Name, e.Kind)
			}
		case "variable":
			envVars[e.Name] = true
			if e.Kind != "SCOPE.Schema" {
				t.Errorf("ENV %q: expected Kind=SCOPE.Schema, got %q", e.Name, e.Kind)
			}
		}
	}

	for _, want := range []string{"APP_VERSION", "TARGETPLATFORM"} {
		if !args[want] {
			t.Errorf("expected build_arg %q", want)
		}
	}
	for _, want := range []string{"PYTHONUNBUFFERED", "LOG_LEVEL"} {
		if !envVars[want] {
			t.Errorf("expected env variable %q", want)
		}
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
// Acceptance criteria: at least one entity per distinct instruction type (AC #1)
// ---------------------------------------------------------------------------

func TestDockerfileExtractor_AllInstructionTypes(t *testing.T) {
	src := `FROM ubuntu:22.04
RUN apt-get install -y curl
COPY src/ /app/src/
EXPOSE 80
ENV HOME=/root
ARG DEBIAN_FRONTEND=noninteractive
CMD ["/bin/bash"]
ENTRYPOINT ["/entrypoint.sh"]
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	subtypes := make(map[string]bool)
	for _, e := range entities {
		subtypes[e.Subtype] = true
	}

	for _, want := range []string{"stage", "run", "copy", "port", "variable", "build_arg", "entrypoint"} {
		if !subtypes[want] {
			t.Errorf("expected at least one entity with subtype=%q", want)
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
	if entities[0].Name != "scratch" {
		t.Errorf("expected Name='scratch', got %q", entities[0].Name)
	}
	if entities[0].Subtype != "stage" {
		t.Errorf("expected Subtype='stage', got %q", entities[0].Subtype)
	}
}
