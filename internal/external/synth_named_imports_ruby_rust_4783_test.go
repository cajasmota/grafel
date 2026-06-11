package external

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// #4783 — once the Ruby + Rust extractors stamp the `imported_name`/`local_name`
// (+ `wildcard`) IMPORTS-edge contract, the existing per-symbol external-node
// synthesis (#4515) mints a stable `ext:<pkg>:<Symbol>` node for Ruby/Rust with
// NO change in internal/external/. These tests exercise the synth layer directly
// with hand-built IMPORTS+reference edges that mirror exactly what the extractors
// now emit, proving the end-to-end resolution.

func rbFileEntity(id, file string) graph.Entity {
	return graph.Entity{ID: id, Name: "mod", Kind: "SCOPE.Component", SourceFile: file, Language: "ruby"}
}

func rsFileEntity(id, file string) graph.Entity {
	return graph.Entity{ID: id, Name: "mod", Kind: "SCOPE.Component", SourceFile: file, Language: "rust"}
}

func langMethodEntity(id, file, lang string) graph.Entity {
	return graph.Entity{ID: id, Name: "handler", Kind: "SCOPE.Function", SourceFile: file, Language: lang}
}

// Rust: `use tokio::sync::Mutex;` → the IMPORTS ToID is `tokio::sync::Mutex`
// with imported_name/local_name=Mutex; a bare `Mutex` reference in the file
// resolves to ext:tokio:Mutex. This is the exact end-to-end case named in #4783.
func TestSynthesize_Rust_PerSymbolNode_4783(t *testing.T) {
	const file = "src/main.rs"
	doc := &graph.Document{
		Entities: []graph.Entity{
			rsFileEntity("aaaaaaaaaaaaaaa0", file),
			langMethodEntity("bbbbbbbbbbbbbbb0", file, "rust"),
		},
		Relationships: []graph.Relationship{
			{ID: "imp-1", FromID: "aaaaaaaaaaaaaaa0", ToID: "tokio::sync::Mutex", Kind: "IMPORTS",
				Properties: map[string]string{
					"language": "rust", "import_path": "tokio::sync::Mutex",
					"source_module": "tokio::sync::Mutex",
					"local_name":    "Mutex", "imported_name": "Mutex",
				}},
			{ID: "call-1", FromID: "bbbbbbbbbbbbbbb0", ToID: "Mutex", Kind: "CALLS",
				Properties: map[string]string{"language": "rust"}},
		},
	}
	Synthesize(doc)

	const want = "ext:tokio:Mutex"
	if doc.Relationships[1].ToID != want {
		t.Fatalf("CALLS ToID=%q, want %q", doc.Relationships[1].ToID, want)
	}
	e := findEntity(doc, want)
	if e == nil {
		t.Fatalf("per-symbol node %q not synthesised; entities=%+v", want, doc.Entities)
	}
	if e.Kind != KindExternal || e.Name != "Mutex" || e.Subtype != "symbol" {
		t.Fatalf("node mismatch kind=%q name=%q subtype=%q", e.Kind, e.Name, e.Subtype)
	}
}

// Rust alias-converge: `use tokio::sync::Mutex as M;` — the reference uses the
// LOCAL alias `M` but binds to the imported-keyed ext:tokio:Mutex node.
func TestSynthesize_Rust_AliasConverge_4783(t *testing.T) {
	const file = "src/lib.rs"
	doc := &graph.Document{
		Entities: []graph.Entity{
			rsFileEntity("aaaaaaaaaaaaaaa1", file),
			langMethodEntity("bbbbbbbbbbbbbbb1", file, "rust"),
		},
		Relationships: []graph.Relationship{
			{ID: "imp-1", FromID: "aaaaaaaaaaaaaaa1", ToID: "tokio::sync::Mutex", Kind: "IMPORTS",
				Properties: map[string]string{
					"language": "rust", "import_path": "tokio::sync::Mutex",
					"local_name": "M", "imported_name": "Mutex",
				}},
			{ID: "call-1", FromID: "bbbbbbbbbbbbbbb1", ToID: "M", Kind: "CALLS",
				Properties: map[string]string{"language": "rust"}},
		},
	}
	Synthesize(doc)
	const want = "ext:tokio:Mutex"
	if doc.Relationships[1].ToID != want {
		t.Fatalf("aliased CALLS ToID=%q, want %q", doc.Relationships[1].ToID, want)
	}
}

// Ruby: `require 'active_record'` → ext:active_record:ActiveRecord; a bare
// `ActiveRecord` reference resolves to the per-symbol node.
func TestSynthesize_Ruby_PerSymbolNode_4783(t *testing.T) {
	const file = "app/models/user.rb"
	doc := &graph.Document{
		Entities: []graph.Entity{
			rbFileEntity("aaaaaaaaaaaaaaa2", file),
			langMethodEntity("bbbbbbbbbbbbbbb2", file, "ruby"),
		},
		Relationships: []graph.Relationship{
			{ID: "imp-1", FromID: "aaaaaaaaaaaaaaa2", ToID: "active_record", Kind: "IMPORTS",
				Properties: map[string]string{
					"language": "ruby", "require_kind": "require",
					"local_name": "ActiveRecord", "imported_name": "ActiveRecord",
				}},
			{ID: "call-1", FromID: "bbbbbbbbbbbbbbb2", ToID: "ActiveRecord", Kind: "CALLS",
				Properties: map[string]string{"language": "ruby"}},
		},
	}
	Synthesize(doc)
	// The reference resolves to a per-symbol node keyed by the imported leaf
	// `ActiveRecord` (the package canon may come from either the classifier's
	// direct disposition of the bare ref or the import index — both share the
	// `ext:<pkg>:ActiveRecord` shape with a distinct symbol leaf).
	got := doc.Relationships[1].ToID
	if !strings.HasPrefix(got, "ext:") || !strings.HasSuffix(got, ":ActiveRecord") {
		t.Fatalf("CALLS ToID=%q, want ext:<gem>:ActiveRecord", got)
	}
	if e := findEntity(doc, got); e == nil || e.Name != "ActiveRecord" || e.Subtype != "symbol" {
		t.Fatalf("ruby per-symbol node missing/mismatched: %+v", e)
	}
}

// Ruby require_relative must NOT mint a per-symbol external node (intra-project).
func TestSynthesize_Ruby_RequireRelative_NoExtNode_4783(t *testing.T) {
	const file = "app/services/foo.rb"
	doc := &graph.Document{
		Entities: []graph.Entity{
			rbFileEntity("aaaaaaaaaaaaaaa3", file),
			langMethodEntity("bbbbbbbbbbbbbbb3", file, "ruby"),
		},
		Relationships: []graph.Relationship{
			{ID: "imp-1", FromID: "aaaaaaaaaaaaaaa3", ToID: "./helper", Kind: "IMPORTS",
				Properties: map[string]string{"language": "ruby", "require_kind": "require_relative"}},
			{ID: "call-1", FromID: "bbbbbbbbbbbbbbb3", ToID: "Helper", Kind: "CALLS",
				Properties: map[string]string{"language": "ruby"}},
		},
	}
	Synthesize(doc)
	if e := findEntity(doc, "ext:helper:Helper"); e != nil {
		t.Fatalf("require_relative must not synthesise an external per-symbol node, got %+v", e)
	}
}

// Cross-package no-collision: the same symbol name from two crates yields two
// distinct per-symbol nodes.
func TestSynthesize_Rust_CrossCrateNoCollision_4783(t *testing.T) {
	const fileA, fileB = "src/a.rs", "src/b.rs"
	doc := &graph.Document{
		Entities: []graph.Entity{
			rsFileEntity("aaaaaaaaaaaaaaa4", fileA),
			langMethodEntity("bbbbbbbbbbbbbbb4", fileA, "rust"),
			rsFileEntity("aaaaaaaaaaaaaaa5", fileB),
			langMethodEntity("bbbbbbbbbbbbbbb5", fileB, "rust"),
		},
		Relationships: []graph.Relationship{
			{ID: "impA", FromID: "aaaaaaaaaaaaaaa4", ToID: "serde::Error", Kind: "IMPORTS",
				Properties: map[string]string{"language": "rust", "import_path": "serde::Error", "local_name": "Error", "imported_name": "Error"}},
			{ID: "callA", FromID: "bbbbbbbbbbbbbbb4", ToID: "Error", Kind: "CALLS",
				Properties: map[string]string{"language": "rust"}},
			{ID: "impB", FromID: "aaaaaaaaaaaaaaa5", ToID: "tokio::Error", Kind: "IMPORTS",
				Properties: map[string]string{"language": "rust", "import_path": "tokio::Error", "local_name": "Error", "imported_name": "Error"}},
			{ID: "callB", FromID: "bbbbbbbbbbbbbbb5", ToID: "Error", Kind: "CALLS",
				Properties: map[string]string{"language": "rust"}},
		},
	}
	Synthesize(doc)
	if doc.Relationships[1].ToID != "ext:serde:Error" {
		t.Fatalf("fileA Error → %q, want ext:serde:Error", doc.Relationships[1].ToID)
	}
	if doc.Relationships[3].ToID != "ext:tokio:Error" {
		t.Fatalf("fileB Error → %q, want ext:tokio:Error", doc.Relationships[3].ToID)
	}
}
