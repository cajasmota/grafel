package dockerfile_test

import (
	"context"
	"strings"
	"testing"

	tsdockerfile "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/dockerfile"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/dockerfile"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/types"
)

func parseForTest(t *testing.T, src string) ts.Tree {
	t.Helper()
	parser, err := tsofficial.New().NewParser(tsdockerfile.Language())
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func extractEntities(t *testing.T, path, src string, tree ts.Tree) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("dockerfile")
	if !ok {
		t.Fatal("dockerfile extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "dockerfile",
		TSTree:   tree,
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

// TestDockerfileExtractor_NilTreeSelfParses6154 replaces the old
// TestDockerfileExtractor_NilTree, which asserted "nil tree -> 0 entities".
//
// #6154 — THAT ASSERTION WAS PINNING DEAD CODE. Extract's guard bailed on
// `file.TSTree == nil || len(Content) == 0`, so the `if tree == nil { …
// AcquireParseSlot … parser.Parse … }` fallback below it could never run: the
// only input that reaches it is a nil tree, and a nil tree returned first. The
// block was unreachable while READING as coverage — it made dockerfile appear
// simultaneously in the audit list of extractors that bail on a nil tree and in
// the list that already self-parse. Both were literally true; the second was
// dead, and the ambiguity cost real time in #6151, where the extractor set was
// miscounted three times.
//
// The other six AcquireParseSlot self-parse extractors (python, golang, html,
// hcl, cpp, yaml) guard on empty content ONLY, so their fallbacks are live.
// dockerfile now matches them, which makes the fallback reachable rather than
// deleting it — the option the issue preferred, since the incremental path can
// legitimately hand over content without a tree.
//
// Asserted by EQUIVALENCE, not by a count: self-parsing must produce the same
// entity content as the pre-parsed path. A count-only assertion would pass on a
// fallback that parsed garbage.
//
// WHAT THIS EQUIVALENCE DOES AND DOES NOT COVER. parseForTest builds the same
// adapter and grammar the fallback does (tsofficial + tsdockerfile), so the two
// sides run the SAME parser — this pins that the fallback WIRES the parser
// correctly and walks the resulting tree the same way, not that the official
// binding agrees with the production ParserFactory path. The factory adds the
// parse watchdog and the shared gate; the fallback's use of the gate is asserted
// separately by the AcquireParseSlot wiring, and its nil-tree contract by
// nil_tree_6154_internal_test.go.
//
// The property comparison is over the WHOLE map rather than a named subset. An
// earlier revision listed three keys, one of which (`base_image`) the extractor
// never emits — a comparison of "" against "" that looked like coverage and was
// not. Comparing every key cannot go vacuous that way, and the non-vacuity check
// below pins that the map is non-trivial in the first place.
func TestDockerfileExtractor_NilTreeSelfParses6154(t *testing.T) {
	// Exercises every instruction family the entity folds into properties
	// (#2063): multi-stage FROM, RUN, COPY --from, EXPOSE, ENV, ARG, ENTRYPOINT.
	const src = `FROM golang:1.22 AS build
ARG VERSION=1.0
RUN go build -o /app ./...
FROM ubuntu:22.04
COPY --from=build /app /usr/bin/app
ENV MODE=prod
EXPOSE 8080
ENTRYPOINT ["/usr/bin/app"]
`

	selfParsed := extractEntities(t, "Dockerfile", src, nil)
	if len(selfParsed) == 0 {
		t.Fatal("nil tree + non-empty content produced no entities — the self-parse fallback is " +
			"still unreachable (#6154)")
	}

	preParsed := extractEntities(t, "Dockerfile", src, parseForTest(t, src))
	if len(preParsed) != len(selfParsed) {
		t.Fatalf("self-parse produced %d entities, pre-parsed path produced %d — the fallback is "+
			"reachable but not equivalent", len(selfParsed), len(preParsed))
	}

	for i := range preParsed {
		got, want := selfParsed[i], preParsed[i]
		if got.Name != want.Name || got.Kind != want.Kind || got.Subtype != want.Subtype {
			t.Errorf("entity %d: self-parse gave %s/%s/%s, pre-parsed gave %s/%s/%s",
				i, got.Kind, got.Subtype, got.Name, want.Kind, want.Subtype, want.Name)
		}
		if got.SourceFile != want.SourceFile {
			t.Errorf("entity %d: SourceFile %q vs %q", i, got.SourceFile, want.SourceFile)
		}
		// The instruction detail is folded into properties (#2063), so this is
		// where a wrongly-parsed tree would actually show up. Compare the whole
		// map in both directions — a one-sided loop misses keys the self-parse
		// path failed to emit at all.
		if len(got.Properties) != len(want.Properties) {
			t.Errorf("entity %d: %d properties self-parsed vs %d pre-parsed (%v vs %v)",
				i, len(got.Properties), len(want.Properties), got.Properties, want.Properties)
		}
		for k, wv := range want.Properties {
			if gv, ok := got.Properties[k]; !ok {
				t.Errorf("entity %d: property %q missing on the self-parse path (want %q)", i, k, wv)
			} else if gv != wv {
				t.Errorf("entity %d: property %q = %q self-parsed, %q pre-parsed", i, k, gv, wv)
			}
		}
		for k := range got.Properties {
			if _, ok := want.Properties[k]; !ok {
				t.Errorf("entity %d: property %q only on the self-parse path", i, k)
			}
		}
		// NON-VACUITY: the instruction-derived properties must actually be
		// populated, or the whole comparison above comes down to two identical
		// near-empty maps.
		for _, key := range []string{"stages", "exposed_ports", "env_vars", "build_args", "entrypoint"} {
			if want.Properties[key] == "" {
				t.Errorf("fixture no longer populates %q — the property equivalence above is "+
					"comparing empty against empty for that key", key)
			}
		}
		if len(got.Relationships) != len(want.Relationships) {
			t.Errorf("entity %d: %d relationships self-parsed vs %d pre-parsed",
				i, len(got.Relationships), len(want.Relationships))
			continue
		}
		for j := range want.Relationships {
			if got.Relationships[j].ToID != want.Relationships[j].ToID ||
				got.Relationships[j].Kind != want.Relationships[j].Kind {
				t.Errorf("entity %d rel %d: %s->%s self-parsed, %s->%s pre-parsed", i, j,
					got.Relationships[j].Kind, got.Relationships[j].ToID,
					want.Relationships[j].Kind, want.Relationships[j].ToID)
			}
		}
	}
}

// TestDockerfileExtractor_NilTreeEmptyContent6154 keeps the other half of the
// old guard: content is still the thing that decides whether there is anything
// to do, and a nil tree with no content must stay a no-op rather than parsing
// an empty buffer.
func TestDockerfileExtractor_NilTreeEmptyContent6154(t *testing.T) {
	ext, _ := extractor.Get("dockerfile")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Dockerfile",
		Content:  nil,
		Language: "dockerfile",
		TSTree:   nil,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities for nil tree + no content, got %d", len(entities))
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
