package fish_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/fish"
)

func TestFishExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("fish")
	if !ok {
		t.Fatal("fish extractor not registered")
	}
}

func TestFishExtractor_Functions(t *testing.T) {
	src := `#!/usr/bin/env fish

function hello
    echo "hi"
end

function fish_prompt --description 'prompt'
    echo "> "
end

function mkcd
    mkdir -p $argv[1]
    and cd $argv[1]
end
`
	ext, _ := extractor.Get("fish")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "config.fish",
		Content:  []byte(src),
		Language: "fish",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := map[string]bool{}
	for _, e := range entities {
		if e.Subtype != "function" {
			continue
		}
		names[e.Name] = true
		if e.Kind != "SCOPE.Operation" {
			t.Errorf("entity %q: expected Kind=SCOPE.Operation, got %q", e.Name, e.Kind)
		}
		if e.Language != "fish" {
			t.Errorf("entity %q: expected Language=fish, got %q", e.Name, e.Language)
		}
		if e.Signature == "" {
			t.Errorf("entity %q: expected non-empty signature", e.Name)
		}
		if e.StartLine < 1 {
			t.Errorf("entity %q: StartLine must be >=1, got %d", e.Name, e.StartLine)
		}
		if e.EndLine < e.StartLine {
			t.Errorf("entity %q: EndLine=%d < StartLine=%d", e.Name, e.EndLine, e.StartLine)
		}
	}
	for _, want := range []string{"hello", "fish_prompt", "mkcd"} {
		if !names[want] {
			t.Errorf("expected function %q to be extracted", want)
		}
	}
}

func TestFishExtractor_Completions_LongFlag(t *testing.T) {
	src := `complete --command kubectl --no-files
complete --command git --arguments '(git branch)'
`
	ext, _ := extractor.Get("fish")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "comp.fish",
		Content:  []byte(src),
		Language: "fish",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	comps := map[string]bool{}
	for _, e := range entities {
		if e.Subtype == "completion" {
			comps[e.Name] = true
			if e.Kind != "SCOPE.Operation" {
				t.Errorf("completion %q: expected Kind=SCOPE.Operation, got %q", e.Name, e.Kind)
			}
			if e.Language != "fish" {
				t.Errorf("completion %q: expected Language=fish, got %q", e.Name, e.Language)
			}
		}
	}
	for _, want := range []string{"kubectl", "git"} {
		if !comps[want] {
			t.Errorf("expected completion for %q", want)
		}
	}
}

func TestFishExtractor_Completions_ShortFlag(t *testing.T) {
	src := `complete -c docker -l rm -d 'remove after exit'
complete -c npm -s v -l version
`
	ext, _ := extractor.Get("fish")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "short.fish",
		Content:  []byte(src),
		Language: "fish",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	comps := map[string]bool{}
	for _, e := range entities {
		if e.Subtype == "completion" {
			comps[e.Name] = true
		}
	}
	for _, want := range []string{"docker", "npm"} {
		if !comps[want] {
			t.Errorf("expected completion for %q", want)
		}
	}
}

func TestFishExtractor_Completions_Dedupe(t *testing.T) {
	// Same command declared with both short and long flag — only one entity.
	src := `complete -c git --long-option foo
complete --command git --short-option bar
`
	ext, _ := extractor.Get("fish")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "dup.fish",
		Content:  []byte(src),
		Language: "fish",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	count := 0
	for _, e := range entities {
		if e.Subtype == "completion" && e.Name == "git" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 git completion entity, got %d", count)
	}
}

func TestFishExtractor_NestedBlocks_EndLine(t *testing.T) {
	src := `function outer
    if test -n "$x"
        for item in a b c
            echo $item
        end
    end
end
`
	ext, _ := extractor.Get("fish")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "nested.fish",
		Content:  []byte(src),
		Language: "fish",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) < 1 {
		t.Fatal("expected at least one entity")
	}
	e := entities[0]
	if e.Name != "outer" {
		t.Fatalf("expected outer, got %q", e.Name)
	}
	// The function spans lines 1..7; the matching `end` is line 7.
	if e.EndLine != 7 {
		t.Errorf("expected EndLine=7 for nested function, got %d", e.EndLine)
	}
}

func TestFishExtractor_CommentedFunction_NotExtracted(t *testing.T) {
	src := `# function commented_out
function real_one
    echo "yes"
end
`
	ext, _ := extractor.Get("fish")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "commented.fish",
		Content:  []byte(src),
		Language: "fish",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	names := map[string]bool{}
	for _, e := range entities {
		names[e.Name] = true
	}
	if names["commented_out"] {
		t.Error("commented-out function should not be extracted")
	}
	if !names["real_one"] {
		t.Error("real_one should be extracted")
	}
}

func TestFishExtractor_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("fish")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.fish",
		Content:  []byte{},
		Language: "fish",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(entities))
	}
}

func TestFishExtractor_MalformedInput_NoPanic(t *testing.T) {
	// Unterminated function block — extractor must not raise.
	src := `function broken
    echo "no end keyword"
`
	ext, _ := extractor.Get("fish")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "broken.fish",
		Content:  []byte(src),
		Language: "fish",
	})
	if err != nil {
		t.Fatalf("unexpected error on malformed input: %v", err)
	}
	// We still extract the function name; EndLine falls back to StartLine.
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity on malformed input, got %d", len(entities))
	}
	if entities[0].EndLine < entities[0].StartLine {
		t.Errorf("EndLine %d < StartLine %d", entities[0].EndLine, entities[0].StartLine)
	}
}

func TestFishExtractor_RealWorldFixture(t *testing.T) {
	path := filepath.Join("..", "..", "..", "fixtures", "real-world", "fish", "config.fish")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ext, _ := extractor.Get("fish")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  data,
		Language: "fish",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var fnCount, cmpCount int
	for _, e := range entities {
		switch e.Subtype {
		case "function":
			fnCount++
		case "completion":
			cmpCount++
		}
	}
	if fnCount < 5 {
		t.Errorf("expected >=5 functions in config.fish, got %d", fnCount)
	}
	if cmpCount < 3 {
		t.Errorf("expected >=3 completions in config.fish, got %d", cmpCount)
	}
	t.Logf("config.fish: %d functions, %d completions, %d total entities", fnCount, cmpCount, len(entities))
}

func TestFishExtractor_LanguageMethod(t *testing.T) {
	ext, _ := extractor.Get("fish")
	if got := ext.Language(); got != "fish" {
		t.Errorf("Language() = %q, want %q", got, "fish")
	}
}
