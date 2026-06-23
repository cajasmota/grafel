package shell_test

import (
	"context"
	"testing"

	tsbash "github.com/smacker/go-tree-sitter/bash"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/shell"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
)

func parseForTest(t *testing.T, src string) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tsbash.GetLanguage()))
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

func TestShellExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("shell")
	if !ok {
		t.Fatal("shell extractor not registered")
	}
}

func TestShellExtractor_BasicFunctions(t *testing.T) {
	src := `#!/bin/bash

log() {
    echo "$*"
}

check_prerequisites() {
    command -v docker
}

build_image() {
    docker build -t myapp .
}
`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("shell")
	if !ok {
		t.Fatal("shell extractor not registered")
	}

	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.sh",
		Content:  []byte(src),
		Language: "shell",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) < 3 {
		t.Fatalf("expected at least 3 entities, got %d", len(entities))
	}

	names := make(map[string]bool)
	for _, e := range entities {
		if e.Kind != "SCOPE.Operation" {
			// Issue #380 also emits SCOPE.Component (script + import stubs);
			// this test only validates Operation entities.
			continue
		}
		names[e.Name] = true
		if e.Language != "shell" {
			t.Errorf("entity %q: expected Language=shell, got %q", e.Name, e.Language)
		}
	}
	for _, want := range []string{"log", "check_prerequisites", "build_image"} {
		if !names[want] {
			t.Errorf("expected function %q to be extracted", want)
		}
	}
}

func TestShellExtractor_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("shell")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.sh",
		Content:  []byte{},
		Language: "shell",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities for empty input, got %d", len(entities))
	}
}

func TestShellExtractor_Signatures(t *testing.T) {
	src := `deploy() {
    echo "deploying"
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("shell")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.sh",
		Content:  []byte(src),
		Language: "shell",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) == 0 {
		t.Fatal("expected at least 1 entity")
	}
	var e = entities[0]
	for _, ent := range entities {
		if ent.Kind == "SCOPE.Operation" {
			e = ent
			break
		}
	}
	if e.Name != "deploy" {
		t.Errorf("expected name=deploy, got %q", e.Name)
	}
	if e.Signature != "deploy()" {
		t.Errorf("expected signature=deploy(), got %q", e.Signature)
	}
	if e.StartLine < 1 {
		t.Errorf("expected StartLine >= 1, got %d", e.StartLine)
	}
}

func TestShellExtractor_NoTree_FallbackRegex(t *testing.T) {
	src := `#!/bin/bash
function setup {
    echo "setup"
}
cleanup() {
    rm -f /tmp/test
}
`
	ext, _ := extractor.Get("shell")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.sh",
		Content:  []byte(src),
		Language: "shell",
		TSTree:   nil, // force regex fallback
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) < 1 {
		t.Fatalf("expected at least 1 entity from regex fallback, got %d", len(entities))
	}
}
