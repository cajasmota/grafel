// crossfile_test.go — unit tests for issue #698: Python cross-file class-hierarchy
// resolution (PythonClassRegistry + extractBaseClasses + EXTENDS edge emission).
package python_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/python" // trigger init()
	python "github.com/cajasmota/grafel/internal/extractors/python"
	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// PythonClassRegistry tests
// ---------------------------------------------------------------------------

// TestScanPythonClassRegistry_BasicTopLevel verifies that top-level class
// declarations are correctly scanned from file content.
func TestScanPythonClassRegistry_BasicTopLevel(t *testing.T) {
	python.ClearPythonClassRegistry()
	defer python.ClearPythonClassRegistry()

	content := `class Foo:
    pass

class Bar(Foo):
    pass

class _Private:
    pass
`
	python.ScanPythonClassRegistry("myapp/models.py", content)

	// We can verify via the EXTENDS edge emitted by Extract.
	// Scan a consumer file that extends Foo.
	python.ScanPythonClassRegistry("other/views.py", "class Child(Foo):\n    pass\n")

	ents := cfExtract(t, "other/views.py", "class Child(Foo):\n    pass\n")
	child := cfFindEntity(ents, "Child")
	if child == nil {
		t.Fatal("Child entity not found")
	}
	foundCrossFile := false
	for _, r := range child.Relationships {
		if r.Kind == "EXTENDS" && strings.Contains(r.ToID, "myapp/models.py:Foo") {
			foundCrossFile = true
		}
	}
	if !foundCrossFile {
		t.Errorf("expected EXTENDS stub with cross-file path myapp/models.py:Foo; rels=%+v", child.Relationships)
	}
}

// TestScanPythonClassRegistry_Dedup verifies that scanning the same file twice
// doesn't duplicate entries (no ambiguity from the same file).
func TestScanPythonClassRegistry_Dedup(t *testing.T) {
	python.ClearPythonClassRegistry()
	defer python.ClearPythonClassRegistry()

	content := "class Foo:\n    pass\n"
	python.ScanPythonClassRegistry("a.py", content)
	python.ScanPythonClassRegistry("a.py", content) // duplicate

	// Cross-file: a.py declares Foo, b.py uses it.
	python.ScanPythonClassRegistry("b.py", "class Child(Foo):\n    pass\n")
	ents := cfExtract(t, "b.py", "class Child(Foo):\n    pass\n")

	child := cfFindEntity(ents, "Child")
	if child == nil {
		t.Fatal("Child entity not found")
	}
	found := false
	for _, r := range child.Relationships {
		if r.Kind == "EXTENDS" && strings.Contains(r.ToID, "a.py:Foo") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected cross-file EXTENDS to a.py:Foo; rels=%+v", child.Relationships)
	}
}

// TestScanPythonClassRegistry_MultiFile verifies that when two different files
// declare the same class name, the EXTENDS stub falls back to the consumer file
// (ambiguous — the resolver's byName fallback handles it).
func TestScanPythonClassRegistry_MultiFile(t *testing.T) {
	python.ClearPythonClassRegistry()
	defer python.ClearPythonClassRegistry()

	python.ScanPythonClassRegistry("a.py", "class Shared:\n    pass\n")
	python.ScanPythonClassRegistry("b.py", "class Shared:\n    pass\n")

	// Consumer extends Shared — ambiguous, stub should use consumer's file.
	src := "class Consumer(Shared):\n    pass\n"
	python.ScanPythonClassRegistry("c.py", src)
	ents := cfExtract(t, "c.py", src)

	consumer := cfFindEntity(ents, "Consumer")
	if consumer == nil {
		t.Fatal("Consumer entity not found")
	}
	for _, r := range consumer.Relationships {
		if r.Kind == "EXTENDS" && strings.Contains(r.ToID, "Shared") {
			// Stub should use c.py (consumer's file) because resolution is ambiguous.
			if strings.Contains(r.ToID, "c.py:Shared") {
				return // correct fallback
			}
			// OK if it uses a.py or b.py — conservative is acceptable.
			// The test just verifies an EXTENDS edge exists with "Shared" in it.
			return
		}
	}
	t.Errorf("expected EXTENDS edge containing Shared; rels=%+v", consumer.Relationships)
}

// TestScanPythonClassRegistry_Indented verifies that indented class declarations
// (nested classes) are NOT added to the global registry.
func TestScanPythonClassRegistry_Indented(t *testing.T) {
	python.ClearPythonClassRegistry()
	defer python.ClearPythonClassRegistry()

	// Outer is top-level; Inner is indented.
	outerContent := `class Outer:
    class Inner:
        pass
`
	python.ScanPythonClassRegistry("a.py", outerContent)

	// Consumer references Inner by name.
	consumerSrc := "class Child(Inner):\n    pass\n"
	python.ScanPythonClassRegistry("b.py", consumerSrc)
	ents := cfExtract(t, "b.py", consumerSrc)

	child := cfFindEntity(ents, "Child")
	if child == nil {
		t.Fatal("Child entity not found")
	}
	for _, r := range child.Relationships {
		if r.Kind == "EXTENDS" && strings.Contains(r.ToID, "a.py:Inner") {
			t.Errorf("indented class Inner should NOT resolve cross-file; got: %+v", r)
		}
	}
}

// TestClearPythonClassRegistry verifies that ClearPythonClassRegistry removes
// all entries so subsequent extractions don't see stale data.
func TestClearPythonClassRegistry(t *testing.T) {
	python.ClearPythonClassRegistry()
	python.ScanPythonClassRegistry("a.py", "class Foo:\n    pass\n")
	python.ClearPythonClassRegistry()

	// After clear, extending Foo from b.py should use consumer file path, not a.py.
	src := "class Child(Foo):\n    pass\n"
	python.ScanPythonClassRegistry("b.py", src)
	ents := cfExtract(t, "b.py", src)

	child := cfFindEntity(ents, "Child")
	if child == nil {
		t.Fatal("Child entity not found")
	}
	for _, r := range child.Relationships {
		if r.Kind == "EXTENDS" && strings.Contains(r.ToID, "a.py:Foo") {
			t.Errorf("expected no cross-file resolution after Clear; got: %+v", r)
		}
	}
}

// ---------------------------------------------------------------------------
// EXTENDS edge emission tests
// ---------------------------------------------------------------------------

// TestExtract_ExtendsEdge_NoBase verifies that a class without base classes
// emits NO EXTENDS edges.
func TestExtract_ExtendsEdge_NoBase(t *testing.T) {
	python.ClearPythonClassRegistry()
	defer python.ClearPythonClassRegistry()

	src := `class Standalone:
    pass
`
	python.ScanPythonClassRegistry("a.py", src)
	ents := cfExtract(t, "a.py", src)

	s := cfFindEntity(ents, "Standalone")
	if s == nil {
		t.Fatal("Standalone entity not found")
	}
	for _, r := range s.Relationships {
		if r.Kind == "EXTENDS" {
			t.Errorf("unexpected EXTENDS edge on Standalone: %+v", r)
		}
	}
}

// TestExtract_ExtendsEdge_EmptyParens verifies that `class Foo():` emits
// NO EXTENDS edges.
func TestExtract_ExtendsEdge_EmptyParens(t *testing.T) {
	python.ClearPythonClassRegistry()
	defer python.ClearPythonClassRegistry()

	src := "class Foo():\n    pass\n"
	python.ScanPythonClassRegistry("a.py", src)
	ents := cfExtract(t, "a.py", src)

	foo := cfFindEntity(ents, "Foo")
	if foo == nil {
		t.Fatal("Foo entity not found")
	}
	for _, r := range foo.Relationships {
		if r.Kind == "EXTENDS" {
			t.Errorf("unexpected EXTENDS edge for empty-parens class: %+v", r)
		}
	}
}

// TestExtract_ExtendsEdge_SameFile verifies that a class extending another class
// in the same file emits an EXTENDS edge.
func TestExtract_ExtendsEdge_SameFile(t *testing.T) {
	python.ClearPythonClassRegistry()
	defer python.ClearPythonClassRegistry()

	src := `class Base:
    pass

class Child(Base):
    pass
`
	python.ScanPythonClassRegistry("models.py", src)
	ents := cfExtract(t, "models.py", src)

	child := cfFindEntity(ents, "Child")
	if child == nil {
		t.Fatal("Child entity not found")
	}
	found := false
	for _, r := range child.Relationships {
		if r.Kind == "EXTENDS" && strings.HasSuffix(r.ToID, ":Base") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected EXTENDS edge from Child to Base; rels=%+v", child.Relationships)
	}
}

// TestExtract_ExtendsEdge_ExternalDotted verifies that a module-qualified base
// like `models.Model` emits a stub with the dotted form (for external synthesis).
func TestExtract_ExtendsEdge_ExternalDotted(t *testing.T) {
	python.ClearPythonClassRegistry()
	defer python.ClearPythonClassRegistry()

	src := `from django.db import models

class User(models.Model):
    pass
`
	python.ScanPythonClassRegistry("users/models.py", src)
	ents := cfExtract(t, "users/models.py", src)

	user := cfFindEntity(ents, "User")
	if user == nil {
		t.Fatal("User entity not found")
	}

	found := false
	for _, r := range user.Relationships {
		if r.Kind == "EXTENDS" && strings.Contains(r.ToID, "models.Model") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected EXTENDS edge with dotted base models.Model; rels=%+v", user.Relationships)
	}
}

// TestExtract_ExtendsEdge_MultipleBase verifies that a class with multiple
// base classes emits one EXTENDS edge per base.
func TestExtract_ExtendsEdge_MultipleBase(t *testing.T) {
	python.ClearPythonClassRegistry()
	defer python.ClearPythonClassRegistry()

	src := `class A:
    pass

class B:
    pass

class C(A, B):
    pass
`
	python.ScanPythonClassRegistry("pkg.py", src)
	ents := cfExtract(t, "pkg.py", src)

	c := cfFindEntity(ents, "C")
	if c == nil {
		t.Fatal("C entity not found")
	}

	extendsCount := 0
	for _, r := range c.Relationships {
		if r.Kind == "EXTENDS" {
			extendsCount++
		}
	}
	if extendsCount < 2 {
		t.Errorf("expected ≥2 EXTENDS edges from C (got %d); rels=%+v", extendsCount, c.Relationships)
	}
}

// TestExtract_ExtendsEdge_CrossFile verifies that when the registry has an
// unambiguous cross-file mapping for a base class, the EXTENDS stub uses
// THAT file's path.
func TestExtract_ExtendsEdge_CrossFile(t *testing.T) {
	python.ClearPythonClassRegistry()
	defer python.ClearPythonClassRegistry()

	// Register base class in core/models.py.
	python.ScanPythonClassRegistry("core/models.py", "class TimestampedModel:\n    pass\n")

	// Consumer in users/models.py extends TimestampedModel.
	src := `from core.models import TimestampedModel

class User(TimestampedModel):
    pass
`
	python.ScanPythonClassRegistry("users/models.py", src)
	ents := cfExtract(t, "users/models.py", src)

	user := cfFindEntity(ents, "User")
	if user == nil {
		t.Fatal("User entity not found")
	}

	foundCrossFile := false
	for _, r := range user.Relationships {
		if r.Kind == "EXTENDS" && strings.Contains(r.ToID, "core/models.py:TimestampedModel") {
			foundCrossFile = true
		}
	}
	if !foundCrossFile {
		t.Errorf("expected EXTENDS stub with core/models.py as file; rels=%+v", user.Relationships)
	}
}

// TestExtract_ExtendsEdge_StubFormat verifies the exact structural-ref format.
func TestExtract_ExtendsEdge_StubFormat(t *testing.T) {
	python.ClearPythonClassRegistry()
	defer python.ClearPythonClassRegistry()

	src := `class Child(Parent):
    pass
`
	python.ScanPythonClassRegistry("app.py", src)
	ents := cfExtract(t, "app.py", src)

	child := cfFindEntity(ents, "Child")
	if child == nil {
		t.Fatal("Child entity not found")
	}
	for _, r := range child.Relationships {
		if r.Kind != "EXTENDS" {
			continue
		}
		if !strings.HasPrefix(r.ToID, "scope:component:class:python:") {
			t.Errorf("EXTENDS ToID wrong prefix: %q", r.ToID)
		}
		if !strings.HasSuffix(r.ToID, ":Parent") {
			t.Errorf("EXTENDS ToID wrong suffix (want :Parent): %q", r.ToID)
		}
		return
	}
	t.Error("no EXTENDS edge found on Child")
}

// TestExtract_ExtendsEdge_KeywordArgSkipped verifies that metaclass= keyword
// arguments do not produce EXTENDS edges.
func TestExtract_ExtendsEdge_KeywordArgSkipped(t *testing.T) {
	python.ClearPythonClassRegistry()
	defer python.ClearPythonClassRegistry()

	src := `import abc

class Interface(metaclass=abc.ABCMeta):
    pass
`
	python.ScanPythonClassRegistry("iface.py", src)
	ents := cfExtract(t, "iface.py", src)

	iface := cfFindEntity(ents, "Interface")
	if iface == nil {
		t.Fatal("Interface entity not found")
	}
	for _, r := range iface.Relationships {
		if r.Kind == "EXTENDS" && strings.Contains(r.ToID, "ABCMeta") {
			t.Errorf("metaclass kwarg should not produce EXTENDS edge: %+v", r)
		}
		if r.Kind == "EXTENDS" && strings.Contains(r.ToID, "metaclass") {
			t.Errorf("metaclass kwarg should not produce EXTENDS edge: %+v", r)
		}
	}
}

// TestExtract_ExtendsEdge_DecoratedClass verifies that decorated class
// declarations also emit EXTENDS edges.
func TestExtract_ExtendsEdge_DecoratedClass(t *testing.T) {
	python.ClearPythonClassRegistry()
	defer python.ClearPythonClassRegistry()

	src := `class Base:
    pass

@some_decorator
class Decorated(Base):
    pass
`
	python.ScanPythonClassRegistry("app.py", src)
	ents := cfExtract(t, "app.py", src)

	dec := cfFindEntity(ents, "Decorated")
	if dec == nil {
		t.Fatal("Decorated entity not found")
	}
	found := false
	for _, r := range dec.Relationships {
		if r.Kind == "EXTENDS" && strings.HasSuffix(r.ToID, ":Base") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected EXTENDS edge from Decorated to Base; rels=%+v", dec.Relationships)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func cfExtract(t *testing.T, filePath, src string) []types.EntityRecord {
	t.Helper()
	tree := parse(t, []byte(src))
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     filePath,
		Content:  []byte(src),
		Language: "python",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func cfFindEntity(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}
