package external

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// #4515 — per-symbol external nodes for NAMED imports. A reference to a named-
// imported framework symbol must bind to a single, stable, package-keyed
// ext:<pkg>:<Symbol> node (not the package-level placeholder), so #4480's
// throws→class retarget has a distinct class node to land on and the imported-
// exception DUPLICATE disappears.

// helper: the file-mirror SCOPE.Component entity an IMPORTS edge's FromID
// points at, plus the caller method that references the symbol.
func tsFileEntity(id, file string) graph.Entity {
	return graph.Entity{ID: id, Name: "mod", Kind: "SCOPE.Component", SourceFile: file, Language: "typescript"}
}

func tsMethodEntity(id, file string) graph.Entity {
	return graph.Entity{ID: id, Name: "handler", Kind: "SCOPE.Function", SourceFile: file, Language: "typescript"}
}

func findEntity(doc *graph.Document, id string) *graph.Entity {
	for i := range doc.Entities {
		if doc.Entities[i].ID == id {
			return &doc.Entities[i]
		}
	}
	return nil
}

// TestSynthesize_NamedImport_PerSymbolNode is the core #4515 case: a
// `throw new NotFoundException()` whose class is named-imported from
// '@nestjs/common' resolves to a single ext:@nestjs/common:NotFoundException
// node (subtype symbol, Name = bare symbol).
func TestSynthesize_NamedImport_PerSymbolNode(t *testing.T) {
	const file = "src/users.controller.ts"
	doc := &graph.Document{
		Entities: []graph.Entity{
			tsFileEntity("aaaaaaaaaaaaaaa0", file),
			tsMethodEntity("bbbbbbbbbbbbbbb0", file),
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "imp-1", FromID: "aaaaaaaaaaaaaaa0", ToID: "@nestjs/common", Kind: "IMPORTS"}.WithProperties(map[string]string{
				"language": "typescript", "import_path": "@nestjs/common",
				"local_name": "NotFoundException", "imported_name": "NotFoundException",
			}),
			// constructor CALLS edge (bare-name stub from `new NotFoundException()`).
			graph.Relationship{ID: "call-1", FromID: "bbbbbbbbbbbbbbb0", ToID: "NotFoundException", Kind: "CALLS"}.WithProperties(map[string]string{"language": "typescript"}),
		},
	}
	Synthesize(doc)

	const want = "ext:@nestjs/common:NotFoundException"
	if doc.Relationships[1].ToID != want {
		t.Fatalf("CALLS ToID=%q, want %q", doc.Relationships[1].ToID, want)
	}
	e := findEntity(doc, want)
	if e == nil {
		t.Fatalf("per-symbol node %q not synthesised; entities=%+v", want, doc.Entities)
	}
	if e.Kind != KindExternal {
		t.Fatalf("kind=%q, want %q", e.Kind, KindExternal)
	}
	if e.Name != "NotFoundException" {
		t.Fatalf("Name=%q, want NotFoundException (needed for #4480 name-keyed resolve)", e.Name)
	}
	if e.Subtype != "symbol" {
		t.Fatalf("subtype=%q, want symbol", e.Subtype)
	}
	// It is an ext: node → NOT a fidelity bug.
	if isBugEdgeToID(doc.Relationships[1].ToID) {
		t.Fatalf("per-symbol node misclassified as a fidelity bug: %q", doc.Relationships[1].ToID)
	}
}

// TestSynthesize_NamedImport_DedupSameSymbol confirms two files importing +
// referencing the same symbol from the same package converge on ONE node.
func TestSynthesize_NamedImport_DedupSameSymbol(t *testing.T) {
	const fileA, fileB = "src/a.controller.ts", "src/b.controller.ts"
	doc := &graph.Document{
		Entities: []graph.Entity{
			tsFileEntity("aaaaaaaaaaaaaaa1", fileA),
			tsMethodEntity("bbbbbbbbbbbbbbb1", fileA),
			tsFileEntity("aaaaaaaaaaaaaaa2", fileB),
			tsMethodEntity("bbbbbbbbbbbbbbb2", fileB),
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "impA", FromID: "aaaaaaaaaaaaaaa1", ToID: "@nestjs/common", Kind: "IMPORTS"}.WithProperties(map[string]string{"language": "typescript", "import_path": "@nestjs/common", "local_name": "NotFoundException", "imported_name": "NotFoundException"}),
			graph.Relationship{ID: "callA", FromID: "bbbbbbbbbbbbbbb1", ToID: "NotFoundException", Kind: "CALLS"}.WithProperties(map[string]string{"language": "typescript"}),
			graph.Relationship{ID: "impB", FromID: "aaaaaaaaaaaaaaa2", ToID: "@nestjs/common", Kind: "IMPORTS"}.WithProperties(map[string]string{"language": "typescript", "import_path": "@nestjs/common", "local_name": "NotFoundException", "imported_name": "NotFoundException"}),
			graph.Relationship{ID: "callB", FromID: "bbbbbbbbbbbbbbb2", ToID: "NotFoundException", Kind: "CALLS"}.WithProperties(map[string]string{"language": "typescript"}),
		},
	}
	Synthesize(doc)

	const want = "ext:@nestjs/common:NotFoundException"
	if doc.Relationships[1].ToID != want || doc.Relationships[3].ToID != want {
		t.Fatalf("both calls should resolve to %q; got %q and %q", want, doc.Relationships[1].ToID, doc.Relationships[3].ToID)
	}
	count := 0
	for i := range doc.Entities {
		if doc.Entities[i].ID == want {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly ONE per-symbol node, got %d", count)
	}
}

// TestSynthesize_NamedImport_CrossPackageNoCollision confirms the SAME symbol
// name imported from DIFFERENT packages yields DISTINCT nodes (package-keyed).
func TestSynthesize_NamedImport_CrossPackageNoCollision(t *testing.T) {
	const fileA, fileB = "src/a.ts", "src/b.ts"
	doc := &graph.Document{
		Entities: []graph.Entity{
			tsFileEntity("aaaaaaaaaaaaaaa3", fileA),
			tsMethodEntity("bbbbbbbbbbbbbbb3", fileA),
			tsFileEntity("aaaaaaaaaaaaaaa4", fileB),
			tsMethodEntity("bbbbbbbbbbbbbbb4", fileB),
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "impA", FromID: "aaaaaaaaaaaaaaa3", ToID: "@nestjs/common", Kind: "IMPORTS"}.WithProperties(map[string]string{"language": "typescript", "import_path": "@nestjs/common", "local_name": "NotFoundException", "imported_name": "NotFoundException"}),
			graph.Relationship{ID: "callA", FromID: "bbbbbbbbbbbbbbb3", ToID: "NotFoundException", Kind: "CALLS"}.WithProperties(map[string]string{"language": "typescript"}),
			graph.Relationship{ID: "impB", FromID: "aaaaaaaaaaaaaaa4", ToID: "other-pkg", Kind: "IMPORTS"}.WithProperties(map[string]string{"language": "typescript", "import_path": "other-pkg", "local_name": "NotFoundException", "imported_name": "NotFoundException"}),
			graph.Relationship{ID: "callB", FromID: "bbbbbbbbbbbbbbb4", ToID: "NotFoundException", Kind: "CALLS"}.WithProperties(map[string]string{"language": "typescript"}),
		},
	}
	Synthesize(doc)

	wantA := "ext:@nestjs/common:NotFoundException"
	wantB := "ext:other-pkg:NotFoundException"
	if doc.Relationships[1].ToID != wantA {
		t.Fatalf("fileA call ToID=%q, want %q", doc.Relationships[1].ToID, wantA)
	}
	if doc.Relationships[3].ToID != wantB {
		t.Fatalf("fileB call ToID=%q, want %q", doc.Relationships[3].ToID, wantB)
	}
	if findEntity(doc, wantA) == nil || findEntity(doc, wantB) == nil {
		t.Fatalf("expected DISTINCT per-package nodes %q and %q", wantA, wantB)
	}
}

// TestSynthesize_NamedImport_AliasConverges confirms `{ Foo as Bar }` keys the
// node by the IMPORTED name and the reference (using the alias) still binds.
func TestSynthesize_NamedImport_AliasConverges(t *testing.T) {
	const file = "src/c.ts"
	doc := &graph.Document{
		Entities: []graph.Entity{
			tsFileEntity("aaaaaaaaaaaaaaa5", file),
			tsMethodEntity("bbbbbbbbbbbbbbb5", file),
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "imp", FromID: "aaaaaaaaaaaaaaa5", ToID: "@nestjs/common", Kind: "IMPORTS"}.WithProperties(map[string]string{"language": "typescript", "import_path": "@nestjs/common", "local_name": "NFE", "imported_name": "NotFoundException"}),
			graph.Relationship{ID: "call", FromID: "bbbbbbbbbbbbbbb5", ToID: "NFE", Kind: "CALLS"}.WithProperties(map[string]string{"language": "typescript"}),
		},
	}
	Synthesize(doc)
	const want = "ext:@nestjs/common:NotFoundException"
	if doc.Relationships[1].ToID != want {
		t.Fatalf("aliased ref ToID=%q, want %q", doc.Relationships[1].ToID, want)
	}
}

// TestSynthesize_NamespaceImport_MemberAccess confirms `import * as nest` +
// `nest.NotFoundException` resolves the statically-recoverable member.
func TestSynthesize_NamespaceImport_MemberAccess(t *testing.T) {
	const file = "src/d.ts"
	doc := &graph.Document{
		Entities: []graph.Entity{
			tsFileEntity("aaaaaaaaaaaaaaa6", file),
			tsMethodEntity("bbbbbbbbbbbbbbb6", file),
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "imp", FromID: "aaaaaaaaaaaaaaa6", ToID: "@nestjs/common", Kind: "IMPORTS"}.WithProperties(map[string]string{"language": "typescript", "import_path": "@nestjs/common", "local_name": "nest", "imported_name": "*", "wildcard": "1"}),
			graph.Relationship{ID: "call", FromID: "bbbbbbbbbbbbbbb6", ToID: "nest.NotFoundException", Kind: "CALLS"}.WithProperties(map[string]string{"language": "typescript"}),
		},
	}
	Synthesize(doc)
	const want = "ext:@nestjs/common:NotFoundException"
	if doc.Relationships[1].ToID != want {
		t.Fatalf("namespace member ToID=%q, want %q", doc.Relationships[1].ToID, want)
	}
}

// TestSynthesize_NamedImport_RelativeNotPerSymbol confirms a symbol imported
// from a RELATIVE (project-internal) path is NOT turned into an ext: node — it
// must stay unresolved (a genuine local binding the resolver owns).
func TestSynthesize_NamedImport_RelativeNotPerSymbol(t *testing.T) {
	const file = "src/e.ts"
	doc := &graph.Document{
		Entities: []graph.Entity{
			tsFileEntity("aaaaaaaaaaaaaaa7", file),
			tsMethodEntity("bbbbbbbbbbbbbbb7", file),
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "imp", FromID: "aaaaaaaaaaaaaaa7", ToID: "./local-exc", Kind: "IMPORTS"}.WithProperties(map[string]string{"language": "typescript", "import_path": "./local-exc", "local_name": "LocalExc", "imported_name": "LocalExc"}),
			graph.Relationship{ID: "call", FromID: "bbbbbbbbbbbbbbb7", ToID: "LocalExc", Kind: "CALLS"}.WithProperties(map[string]string{"language": "typescript"}),
		},
	}
	Synthesize(doc)
	if doc.Relationships[1].ToID == "ext:./local-exc:LocalExc" {
		t.Fatalf("relative-import symbol must NOT become a per-symbol ext node; got %q", doc.Relationships[1].ToID)
	}
}

// TestSynthesize_NamedImport_ThrowsDedup is the end-to-end #4515+#4480
// acceptance: a named-imported framework exception that is BOTH thrown and
// constructed converges on ONE node after Synthesize → ResolveExceptionTypes.
// The synthetic exception node is dropped (retargeted onto the per-symbol ext
// class), and the constructor CALLS edge lands on the same node — no duplicate.
func TestSynthesize_NamedImport_ThrowsDedup(t *testing.T) {
	const file = "src/users.controller.ts"
	doc := &graph.Document{
		Entities: []graph.Entity{
			tsFileEntity("aaaaaaaaaaaaaaa9", file),
			tsMethodEntity("bbbbbbbbbbbbbbb9", file),
			// synthetic SCOPE.ExceptionType convergence node (#4480 input).
			graph.Entity{ID: "exc-node", Name: "exception:NotFoundException",
				Kind: "SCOPE.ExceptionType", SourceFile: "NotFoundException",
			}.WithProperties(map[string]string{"exception_type": "NotFoundException"}),
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "imp", FromID: "aaaaaaaaaaaaaaa9", ToID: "@nestjs/common", Kind: "IMPORTS"}.WithProperties(map[string]string{"language": "typescript", "import_path": "@nestjs/common", "local_name": "NotFoundException", "imported_name": "NotFoundException"}),
			// constructor CALLS (`new NotFoundException()`).
			graph.Relationship{ID: "call", FromID: "bbbbbbbbbbbbbbb9", ToID: "NotFoundException", Kind: "CALLS"}.WithProperties(map[string]string{"language": "typescript"}),
			// THROWS edge to the synthetic convergence node.
			graph.Relationship{ID: "throw", FromID: "bbbbbbbbbbbbbbb9", ToID: "exc-node", Kind: "THROWS"}.WithProperties(map[string]string{"language": "typescript"}),
		},
	}
	Synthesize(doc)
	ResolveExceptionTypes(doc)

	const want = "ext:@nestjs/common:NotFoundException"
	// The synthetic exception node must be gone (retargeted + dropped).
	if findEntity(doc, "exc-node") != nil {
		t.Fatalf("synthetic exception node survived; want dropped (duplicate)")
	}
	// Both THROWS and the constructor CALLS now point at the single ext node.
	var throwsTo, callsTo string
	for i := range doc.Relationships {
		switch doc.Relationships[i].Kind {
		case "THROWS":
			throwsTo = doc.Relationships[i].ToID
		case "CALLS":
			callsTo = doc.Relationships[i].ToID
		}
	}
	if throwsTo != want {
		t.Fatalf("THROWS retargeted to %q, want %q", throwsTo, want)
	}
	if callsTo != want {
		t.Fatalf("CALLS folded to %q, want %q", callsTo, want)
	}
}

// TestSynthesize_NamedImport_Idempotent confirms a second pass is a no-op.
func TestSynthesize_NamedImport_Idempotent(t *testing.T) {
	const file = "src/f.ts"
	mk := func() *graph.Document {
		return &graph.Document{
			Entities: []graph.Entity{
				tsFileEntity("aaaaaaaaaaaaaaa8", file),
				tsMethodEntity("bbbbbbbbbbbbbbb8", file),
			},
			Relationships: []graph.Relationship{
				graph.Relationship{ID: "imp", FromID: "aaaaaaaaaaaaaaa8", ToID: "@nestjs/common", Kind: "IMPORTS"}.WithProperties(map[string]string{"language": "typescript", "import_path": "@nestjs/common", "local_name": "NotFoundException", "imported_name": "NotFoundException"}),
				graph.Relationship{ID: "call", FromID: "bbbbbbbbbbbbbbb8", ToID: "NotFoundException", Kind: "CALLS"}.WithProperties(map[string]string{"language": "typescript"}),
			},
		}
	}
	doc := mk()
	Synthesize(doc)
	n1 := len(doc.Entities)
	second := Synthesize(doc)
	if second.Synthesized != 0 {
		t.Fatalf("second pass synthesized=%d, want 0 (idempotent)", second.Synthesized)
	}
	if len(doc.Entities) != n1 {
		t.Fatalf("entity count changed on re-run: %d -> %d", n1, len(doc.Entities))
	}
}
