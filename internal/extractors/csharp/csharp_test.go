package csharp_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	tscsharp "github.com/smacker/go-tree-sitter/csharp"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/csharp"
	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
)

// parseForTest parses C# source into a ts.Tree via the smacker adapter. Tests
// stamp the result on FileInput.TSTree (which the migrated C# extractor
// consumes); the smacker adapter keeps these tests linkable in the default build
// while exercising the ts façade (B2 #5418).
func parseForTest(t *testing.T, src string) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tscsharp.GetLanguage()))
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

func TestCSharpExtractor_BasicExtraction(t *testing.T) {
	src := `
using System.Collections.Generic;
using Microsoft.AspNetCore.Mvc;

namespace SampleApi
{
    public interface IUserService
    {
        User GetById(int id);
    }

    public class UserService : IUserService
    {
        public User GetById(int id)
        {
            return null;
        }

        public void Create(string name)
        {
        }
    }
}
`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("csharp")
	if !ok {
		t.Fatal("csharp extractor not registered")
	}

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "service.cs",
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var classes, interfaces, methods, imports int
	for _, e := range got {
		switch {
		case e.Kind == "SCOPE.Component" && e.Subtype == "class":
			classes++
		case e.Kind == "SCOPE.Component" && e.Subtype == "interface":
			interfaces++
		case e.Kind == "SCOPE.Operation":
			methods++
		case e.Kind == "SCOPE.Component" && len(e.Relationships) > 0:
			imports++
		}
	}

	if classes == 0 {
		t.Error("expected at least one class entity")
	}
	if interfaces == 0 {
		t.Error("expected at least one interface entity")
	}
	if methods == 0 {
		t.Error("expected at least one method entity")
	}
	if imports == 0 {
		t.Error("expected at least one import entity")
	}
}

func TestCSharpExtractor_ClassEntity(t *testing.T) {
	src := `
public class Foo
{
    public void Bar() {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("csharp")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "foo.cs",
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Foo" && e.Kind == "SCOPE.Component" && e.Subtype == "class" {
			found = true
			if e.SourceFile != "foo.cs" {
				t.Errorf("expected source_file foo.cs, got %s", e.SourceFile)
			}
			if e.Language != "csharp" {
				t.Errorf("expected language csharp, got %s", e.Language)
			}
			if e.StartLine == 0 {
				t.Error("expected non-zero start_line")
			}
		}
	}
	if !found {
		t.Error("expected entity Foo with Kind=SCOPE.Component Subtype=class")
	}
}

func TestCSharpExtractor_InterfaceEntity(t *testing.T) {
	src := `
public interface IRepository
{
    void Save();
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("csharp")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "repo.cs",
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "IRepository" && e.Kind == "SCOPE.Component" && e.Subtype == "interface" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity IRepository with Kind=SCOPE.Component Subtype=interface")
	}
}

func TestCSharpExtractor_MethodEntity(t *testing.T) {
	src := `
public class Svc
{
    public string GetName(int id) { return ""; }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("csharp")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "svc.cs",
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		// Issue #368 parity: methods declared inside a class/interface/struct
		// emit Name="<EnclosingType>.<member>" so sibling-type same-named
		// members produce distinct ComputeID values (#65 parity with Java).
		if e.Name == "Svc.GetName" && e.Kind == "SCOPE.Operation" && e.Subtype == "method" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity Svc.GetName with Kind=SCOPE.Operation Subtype=method")
	}
}

func TestCSharpExtractor_ImportRelationship(t *testing.T) {
	src := `
using System.Collections.Generic;
using Microsoft.AspNetCore.Mvc;

public class Foo {}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("csharp")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "imports.cs",
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	importTargets := map[string]bool{}
	for _, e := range got {
		for _, rel := range e.Relationships {
			if rel.Kind == "IMPORTS" {
				importTargets[rel.ToID] = true
			}
		}
	}

	if !importTargets["System.Collections.Generic"] {
		t.Error("expected IMPORTS relationship for System.Collections.Generic")
	}
	if !importTargets["Microsoft.AspNetCore.Mvc"] {
		t.Error("expected IMPORTS relationship for Microsoft.AspNetCore.Mvc")
	}
}

func TestCSharpExtractor_EmptyFile(t *testing.T) {
	tree := parseForTest(t, "")
	ext, _ := extractor.Get("csharp")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.cs",
		Content:  []byte(""),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for empty file, got %d", len(got))
	}
}

func TestCSharpExtractor_NilTree(t *testing.T) {
	ext, _ := extractor.Get("csharp")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "nil.cs",
		Content:  []byte("public class Foo {}"),
		Language: "csharp",
		TSTree:   nil,
	})
	if err != nil {
		t.Fatalf("unexpected error on nil tree: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for nil tree, got %d", len(got))
	}
}

func TestCSharpExtractor_MissingGrammarReturnsErrUnsupportedLanguage(t *testing.T) {
	factory := treesitter.NewParserFactory(nil)
	_, err := factory.Parse(context.Background(), []byte("public class Foo {}"), "dart")
	if err == nil {
		t.Fatal("expected ErrUnsupportedLanguage for dart, got nil")
	}
	if !errors.Is(err, treesitter.ErrUnsupportedLanguage) {
		t.Errorf("expected ErrUnsupportedLanguage, got: %v", err)
	}
}

func TestCSharpExtractor_LineNumbers(t *testing.T) {
	src := `public class Alpha
{
    public void Method1() {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("csharp")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "lines.cs",
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Name == "Alpha" {
			if e.StartLine < 1 {
				t.Errorf("expected StartLine >= 1, got %d", e.StartLine)
			}
			if e.EndLine < e.StartLine {
				t.Errorf("expected EndLine >= StartLine, got start=%d end=%d", e.StartLine, e.EndLine)
			}
		}
	}
}

func TestCSharpExtractor_CallEdge_HasLineProperty(t *testing.T) {
	src := `public class Helper {
    public void DoIt() { }
}

public class Caller {
    public void Run() {
        new Helper().DoIt();
    }
}`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("csharp")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.cs",
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find Caller.Run and check for CALLS edge with line property
	for _, e := range got {
		if e.Name == "Caller.Run" && e.Kind == "SCOPE.Operation" {
			for _, rel := range e.Relationships {
				if rel.Kind == "CALLS" && rel.ToID == "Helper" {
					lineStr, ok := rel.Properties["line"]
					if !ok {
						t.Fatal("CALLS edge missing Properties[\"line\"]")
					}
					if lineStr == "" {
						t.Error("Properties[\"line\"] is empty")
					}
					// Parse as integer to verify it's valid
					line, err := strconv.Atoi(lineStr)
					if err != nil {
						t.Errorf("Properties[\"line\"] = %q, not a valid integer: %v", lineStr, err)
					}
					if line <= 0 {
						t.Errorf("Properties[\"line\"] = %d, expected positive", line)
					}
					return
				}
			}
			t.Fatal("no CALLS edge found for Caller.Run")
		}
	}
	t.Fatal("entity Caller.Run not found")
}
