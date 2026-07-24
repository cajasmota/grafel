package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// Issue #5954 — tree-sitter parse trees must NOT outlive the Pass-1 extractor
// that consumes them.
//
// Trees are CGo-allocated (~19.7 bytes of C heap per source byte, ~89 B/node),
// they are invisible to the Go heap profiler and to GOMEMLIMIT, and
// go-tree-sitter@v0.24.0 installs NO finalizer — an un-Close()d tree is a leak
// for the life of the process. Retaining one tree per classified file (the old
// behaviour) pinned ~1.4-1.6 GB of C heap on a 24k-file corpus, and macOS
// libmalloc never returns that high-water to the OS.
//
// These tests are the guard rail against silently reintroducing retention.

// TestIssue5954_ClassifiedFileHoldsNoParseTree fails if anyone re-adds a
// tree-sitter tree (or any handle onto one) to classifiedFile. classifiedFile
// is retained for the whole run, so a tree stored there is a tree held for the
// whole run — exactly the regression this issue removed.
func TestIssue5954_ClassifiedFileHoldsNoParseTree(t *testing.T) {
	treeIface := reflect.TypeOf((*ts.Tree)(nil)).Elem()
	nodeIface := reflect.TypeOf((*ts.Node)(nil)).Elem()

	rt := reflect.TypeOf(classifiedFile{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.Type == treeIface || f.Type == nodeIface {
			t.Fatalf("classifiedFile.%s is a %s: parse trees/nodes must not be "+
				"retained past Pass 1 (#5954 — CGo memory, no finalizer, "+
				"~19.7 B of C heap per source byte)", f.Name, f.Type)
		}
		if f.Type.Implements(treeIface) || f.Type.Implements(nodeIface) {
			t.Fatalf("classifiedFile.%s (%s) implements a tree-sitter handle "+
				"interface: parse trees/nodes must not be retained past Pass 1 (#5954)",
				f.Name, f.Type)
		}
	}
}

// TestIssue5954_Pass1ClosesTreesWithoutBreakingOutput drives the full indexer
// over a small multi-language fixture and asserts the run is deterministic and
// non-empty.
//
// Value under -race / normally: Pass 1 now Close()s each tree the moment its
// extractor returns, and ts.Tree.Close maps straight onto ts_tree_delete (it is
// NOT idempotent). If any extractor retained a ts.Node past Extract — a Node
// holds a pointer INTO the tree — or if any later pass tried to read the tree,
// this exercise would fault in C or produce diverging output.
//
// The fixture deliberately includes Java/Kotlin/Python sources shaped for the
// Spring and Django route-composition passes, which own their own trees and
// now close them (the three leaks fixed alongside #5954).
func TestIssue5954_Pass1ClosesTreesWithoutBreakingOutput(t *testing.T) {
	repo := t.TempDir()
	files := map[string]string{
		"svc/OrderController.java": `package svc;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/orders")
public class OrderController {
    @GetMapping("/{id}")
    public String get(@PathVariable String id) { return id; }
}
`,
		"svc/UserController.kt": `package svc

import org.springframework.web.bind.annotation.*

@RestController
@RequestMapping("/api/users")
class UserController {
    @GetMapping("/{id}")
    fun get(id: String): String = id
}
`,
		"app/urls.py": `from django.urls import path, include
from rest_framework import routers

router = routers.DefaultRouter()
router.register(r'widgets', WidgetViewSet)

urlpatterns = [
    path("api/", include(router.urls)),
]
`,
		"app/views.py": `class WidgetViewSet:
    def list(self, request):
        return helper()

def helper():
    return 1
`,
		"pkg/main.go": `package pkg

func Helper() int { return 1 }

func Caller() int { return Helper() }
`,
		"web/client.ts": `export async function fetchOrder(id: string) {
  return fetch("/api/orders/" + id);
}
`,
		"lib/thing.rb": `class Thing
  def run
    helper
  end
end
`,
	}
	for rel, src := range files {
		abs := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(src), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	fingerprint := func(doc *graph.Document) (int, int, string) {
		t.Helper()
		if doc == nil {
			t.Fatal("nil document")
		}
		var b []byte
		for _, e := range doc.Entities {
			b = append(b, e.ID...)
			b = append(b, '|')
			b = append(b, e.Kind...)
			b = append(b, '|')
			b = append(b, e.Name...)
			b = append(b, '\n')
		}
		return len(doc.Entities), len(doc.Relationships), string(b)
	}

	e1, r1, fp1 := fingerprint(runIndexerOn(t, repo, "tstree5954", nil))
	if e1 == 0 {
		t.Fatal("no entities extracted — fixture or Pass 1 is broken")
	}
	e2, r2, fp2 := fingerprint(runIndexerOn(t, repo, "tstree5954", nil))

	if e1 != e2 || r1 != r2 {
		t.Fatalf("nondeterministic counts: run1 e=%d r=%d, run2 e=%d r=%d", e1, r1, e2, r2)
	}
	if fp1 != fp2 {
		t.Fatal("nondeterministic entity fingerprint across two runs of the same fixture")
	}
}
