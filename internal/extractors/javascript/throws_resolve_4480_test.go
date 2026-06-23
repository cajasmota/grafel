package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/external"
	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/javascript"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// throws_resolve_4480_test.go — in-pipeline live-fire for #4480.
//
// Runs the REAL JS/TS extractor + REAL resolver + REAL external synthesis on
// byte-copies of representative NestJS/TS services that throw an exception, and
// asserts the duplicate-exception-node bug is fixed:
//
//   BEFORE (bug): two nodes for one exception — the synthetic
//   SCOPE.ExceptionType `exception:<Type>` (the THROWS edge lands here) AND the
//   REAL exception class entity (a declared SCOPE.Class/Component, or an
//   imported `ext:<Type>` external class).
//
//   AFTER (fix): exactly ONE exception node (the real class) carrying the
//   THROWS edge; the synthetic SCOPE.ExceptionType node is dropped.
//
// Final live confirmation (one node in the core-backend-v3 flow view) needs a
// reindex = a user-authorised deploy and is DEFERRED. These tests prove the fix
// in the extract→resolve→synthesise pipeline byte-for-byte.

const repoTag4480 = "core-backend-v3"

// stampGraphIDs mirrors cmd/grafel.(*Indexer).stampEntityIDs: it assigns
// every record the deterministic graph.EntityID the resolver and buildDocument
// rely on, so byQualifiedName binds THROWS edges to the synthetic node's real
// graph id (matching production).
func stampGraphIDs(recs []types.EntityRecord) {
	for i := range recs {
		recs[i].ID = graph.EntityID(repoTag4480, recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
}

// assembleDoc4480 mirrors the essential parts of buildDocument: resolve embedded
// references, then flatten EntityRecords + their (now hex-resolved) embedded
// relationships into a graph.Document.
func assembleDoc4480(recs []types.EntityRecord) *graph.Document {
	stampGraphIDs(recs)
	idx := resolve.BuildIndex(recs)
	resolve.ReferencesEmbedded(recs, idx)

	doc := &graph.Document{Repo: repoTag4480}
	seenE := map[string]bool{}
	seenR := map[string]bool{}
	for k := range recs {
		r := &recs[k]
		if !seenE[r.ID] {
			seenE[r.ID] = true
			doc.Entities = append(doc.Entities, graph.Entity{
				ID:            r.ID,
				Name:          r.Name,
				QualifiedName: r.QualifiedName,
				Kind:          r.Kind,
				Subtype:       r.Subtype,
				SourceFile:    r.SourceFile,
				Language:      r.Language,
				Properties:    r.Properties,
			})
		}
		for j := range r.Relationships {
			rel := &r.Relationships[j]
			from := rel.FromID
			if from == "" {
				from = r.ID
			}
			id := graph.RelationshipID(from, rel.ToID, rel.Kind)
			if seenR[id] {
				continue
			}
			seenR[id] = true
			doc.Relationships = append(doc.Relationships, graph.Relationship{
				ID:         id,
				FromID:     from,
				ToID:       rel.ToID,
				Kind:       rel.Kind,
				Properties: rel.Properties,
			})
		}
	}
	return doc
}

func extractTSFixture4480(t *testing.T, fixture, repoPath string) []types.EntityRecord {
	t.Helper()
	path := filepath.Join("..", "..", "external", "testdata", fixture)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixture, err)
	}
	tree := parseTS(t, src)
	e := javascript.New()
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path: repoPath, Language: "typescript", Content: src, TSTree: tree,
	})
	if err != nil {
		t.Fatalf("extract %s: %v", fixture, err)
	}
	return ents
}

func throwsEdges(doc *graph.Document) []graph.Relationship {
	var out []graph.Relationship
	for _, r := range doc.Relationships {
		if r.Kind == string(types.RelationshipKindThrows) {
			out = append(out, r)
		}
	}
	return out
}

func entByID(doc *graph.Document, id string) *graph.Entity {
	for i := range doc.Entities {
		if doc.Entities[i].ID == id {
			return &doc.Entities[i]
		}
	}
	return nil
}

func countExceptionTypeNodes(doc *graph.Document, name string) int {
	n := 0
	for i := range doc.Entities {
		if doc.Entities[i].Kind == string(types.EntityKindExceptionType) &&
			doc.Entities[i].Name == name {
			n++
		}
	}
	return n
}

// realClassID returns the id of a non-synthetic entity with the given Name, or
// "" if none/ambiguous.
func realClassID(doc *graph.Document, name string) string {
	var id string
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Name != name || e.Kind == string(types.EntityKindExceptionType) {
			continue
		}
		if id != "" && id != e.ID {
			return "" // ambiguous
		}
		id = e.ID
	}
	return id
}

// --- Case 1: locally-declared exception class (real SCOPE.Class entity) ------
//
// `class AppNotFoundError extends Error {}` + `throw new AppNotFoundError(...)`.
// The declared class is a real graph entity; this is the cleanest fully-real
// reproduction of the duplicate-node bug across the extract→resolve→synth path.

func TestThrows4480_LocalClass_BeforeFix_RED(t *testing.T) {
	doc := assembleDoc4480(extractTSFixture4480(t, "local_exception_4480.ts", "src/widget.service.ts"))
	external.Synthesize(doc) // NO ResolveExceptionTypes — pre-fix snapshot.

	// Two nodes exist for one exception: the synthetic ExceptionType AND the
	// real declared class.
	if got := countExceptionTypeNodes(doc, "exception:AppNotFoundError"); got != 1 {
		t.Fatalf("pre-fix: want 1 synthetic exception node, got %d", got)
	}
	if realClassID(doc, "AppNotFoundError") == "" {
		t.Fatal("pre-fix: expected the real declared AppNotFoundError class entity to exist")
	}
	// THROWS lands on the synthetic node (the bug).
	landsOnSynthetic := false
	for _, e := range throwsEdges(doc) {
		if tgt := entByID(doc, e.ToID); tgt != nil && tgt.Kind == string(types.EntityKindExceptionType) {
			landsOnSynthetic = true
		}
	}
	if !landsOnSynthetic {
		t.Fatal("pre-fix: expected THROWS to land on the synthetic ExceptionType node")
	}
}

func TestThrows4480_LocalClass_AfterFix_GREEN(t *testing.T) {
	doc := assembleDoc4480(extractTSFixture4480(t, "local_exception_4480.ts", "src/widget.service.ts"))
	external.Synthesize(doc)
	stats := external.ResolveExceptionTypes(doc)

	realID := realClassID(doc, "AppNotFoundError")
	if realID == "" {
		t.Fatal("after-fix: real AppNotFoundError class entity must remain")
	}
	// Synthetic node dropped — exactly ONE node for the exception.
	if got := countExceptionTypeNodes(doc, "exception:AppNotFoundError"); got != 0 {
		t.Fatalf("after-fix: synthetic node must be dropped, found %d", got)
	}
	// THROWS now targets the real class; no THROWS targets a synthetic node.
	throws := throwsEdges(doc)
	if len(throws) == 0 {
		t.Fatal("after-fix: THROWS edge disappeared")
	}
	landsOnReal := false
	for _, e := range throws {
		if e.ToID == realID {
			landsOnReal = true
		}
		if tgt := entByID(doc, e.ToID); tgt != nil && tgt.Kind == string(types.EntityKindExceptionType) {
			t.Fatalf("after-fix: a THROWS edge still targets a synthetic node (%s)", tgt.Name)
		}
	}
	if !landsOnReal {
		t.Fatalf("after-fix: THROWS must target the real class %s", realID)
	}
	if stats.Retargeted < 1 || stats.SyntheticDropped < 1 {
		t.Fatalf("after-fix: want retargeted>=1 & dropped>=1, got %+v", stats)
	}
}

// --- Case 2: imported external exception class (live upvate-v3 shape) ---------
//
// On the live graph the ticket observed `NotFoundException` as TWO nodes: the
// synthetic SCOPE.ExceptionType AND the imported class materialised as a real
// `ext:NotFoundException` external entity. We reproduce that exact shape by
// running the real extractor on the byte-copied core-backend-v3 service (which
// emits the synthetic node + the `new NotFoundException()` constructor CALLS
// edge) and adding the imported-class external entity the live indexer carried,
// then assert the fix collapses it to one node with THROWS on the real class.

func TestThrows4480_ImportedExternal_AfterFix_GREEN(t *testing.T) {
	doc := assembleDoc4480(extractTSFixture4480(t,
		"client_repository_4480.ts",
		"src/modules/clients/repositories/client.repository.ts"))
	external.Synthesize(doc)

	// Reproduce the live shape: the imported NotFoundException class present as
	// a real external entity (kind SCOPE.External), as seen on upvate-v3.
	doc.Entities = append(doc.Entities, graph.Entity{
		ID:            "ext:NotFoundException",
		Name:          "NotFoundException",
		QualifiedName: "NotFoundException",
		Kind:          external.KindExternal,
		Subtype:       "class",
	})

	// Sanity: pre-resolve we have BOTH the synthetic and the real node, with
	// THROWS on the synthetic.
	if countExceptionTypeNodes(doc, "exception:NotFoundException") != 1 {
		t.Fatal("setup: expected the synthetic NotFoundException node")
	}

	stats := external.ResolveExceptionTypes(doc)

	if got := countExceptionTypeNodes(doc, "exception:NotFoundException"); got != 0 {
		t.Fatalf("after-fix: synthetic NotFoundException node must be dropped, found %d", got)
	}
	throws := throwsEdges(doc)
	if len(throws) == 0 {
		t.Fatal("after-fix: THROWS edge disappeared")
	}
	for _, e := range throws {
		if tgt := entByID(doc, e.ToID); tgt != nil && tgt.Kind == string(types.EntityKindExceptionType) {
			t.Fatalf("after-fix: THROWS still targets a synthetic node (%s)", tgt.Name)
		}
		if e.ToID != "ext:NotFoundException" {
			t.Fatalf("after-fix: THROWS must target ext:NotFoundException, got %s", e.ToID)
		}
	}
	if stats.Retargeted < 1 || stats.SyntheticDropped < 1 {
		t.Fatalf("after-fix: want retargeted>=1 & dropped>=1, got %+v", stats)
	}
}

// --- Case 2b (#4555): dangling constructor CALLS unified onto the one node ---
//
// The TRUE live core-backend-v3 shape, reproduced with NO manual entity
// injection. `throw new NotFoundException('Client not found')` produces, fully
// independently, TWO graph artefacts after the real extractor + Synthesize:
//
//   1. the synthetic SCOPE.ExceptionType node `exception:NotFoundException`
//      (id e7cb18d1694fc1d7) carrying the THROWS edge, and
//   2. a DANGLING constructor CALLS edge whose ToID is the bare name
//      `NotFoundException` — no ext:* placeholder is synthesised for it
//      (the import folds to ext:@nestjs/common, not ext:NotFoundException),
//      so it renders as a SECOND phantom node next to the exception node.
//
// One exception, two nodes. #4555 folds the construction CALLS onto the
// surviving exception node so a SINGLE node carries BOTH relationships.

func callsFrom(doc *graph.Document, toID string) []graph.Relationship {
	var out []graph.Relationship
	for _, r := range doc.Relationships {
		if r.Kind == string(types.RelationshipKindCalls) && r.ToID == toID {
			out = append(out, r)
		}
	}
	return out
}

func TestThrows4555_DanglingConstructorCall_BeforeFix_RED(t *testing.T) {
	doc := assembleDoc4480(extractTSFixture4480(t,
		"client_repository_4480.ts",
		"src/modules/clients/repositories/client.repository.ts"))
	external.Synthesize(doc) // NO ResolveExceptionTypes — pre-fix snapshot.

	// Artefact 1: the synthetic exception node carries THROWS.
	if countExceptionTypeNodes(doc, "exception:NotFoundException") != 1 {
		t.Fatal("pre-fix: expected the synthetic NotFoundException node")
	}
	// Artefact 2: a dangling bare-name `NotFoundException` constructor CALLS edge
	// exists (the SECOND node the dashboard renders). No ext:NotFoundException
	// entity backs it.
	dangling := callsFrom(doc, "NotFoundException")
	if len(dangling) == 0 {
		t.Fatal("pre-fix: expected a dangling `new NotFoundException()` CALLS edge")
	}
	if entByID(doc, "NotFoundException") != nil || entByID(doc, "ext:NotFoundException") != nil {
		t.Fatal("pre-fix: the constructor CALLS target must be a bare-name dangling stub (two nodes, one exception)")
	}
}

func TestThrows4555_DanglingConstructorCall_AfterFix_GREEN(t *testing.T) {
	doc := assembleDoc4480(extractTSFixture4480(t,
		"client_repository_4480.ts",
		"src/modules/clients/repositories/client.repository.ts"))
	external.Synthesize(doc)
	stats := external.ResolveExceptionTypes(doc)

	if stats.ConstructorCallsUnified < 1 {
		t.Fatalf("after-fix: want >=1 constructor call unified, got %+v", stats)
	}
	// The bare-name dangling CALLS target is gone — folded onto the one node.
	if len(callsFrom(doc, "NotFoundException")) != 0 {
		t.Fatal("after-fix: dangling `NotFoundException` CALLS must be re-pointed")
	}
	// Exactly ONE NotFoundException node survives (the exception node, kept since
	// no real class entity exists), carrying BOTH throws and calls.
	excNodes := 0
	var excID string
	for i := range doc.Entities {
		if doc.Entities[i].Name == "exception:NotFoundException" {
			excNodes++
			excID = doc.Entities[i].ID
		}
	}
	if excNodes != 1 {
		t.Fatalf("after-fix: want exactly 1 exception node, got %d", excNodes)
	}
	// Both a THROWS and a CALLS edge now land on that single node.
	throwsOnNode, callsOnNode := false, false
	for _, r := range doc.Relationships {
		if r.ToID != excID {
			continue
		}
		switch r.Kind {
		case string(types.RelationshipKindThrows):
			throwsOnNode = true
		case string(types.RelationshipKindCalls):
			callsOnNode = true
		}
	}
	if !throwsOnNode || !callsOnNode {
		t.Fatalf("after-fix: single node must carry BOTH throws(%v) and calls(%v)", throwsOnNode, callsOnNode)
	}
}

// --- Case 3: genuinely-external/unresolvable type keeps a single node --------
//
// When NO real class entity exists for the thrown type (truly 3rd-party type
// constructed nowhere in-repo and folded only to its package), the synthetic
// node is KEPT — exactly one node, with the THROWS edge on it. This guards the
// precision invariant: never drop the only node we have.

func TestThrows4480_Unresolvable_KeepsSingleNode(t *testing.T) {
	doc := assembleDoc4480(extractTSFixture4480(t,
		"client_repository_4480.ts",
		"src/modules/clients/repositories/client.repository.ts"))
	external.Synthesize(doc)
	// In this build no real NotFoundException class entity is synthesised
	// (the import folds to ext:@nestjs/common), so the type is unresolvable.
	if realClassID(doc, "NotFoundException") != "" {
		t.Skip("a real NotFoundException entity exists in this build; covered by Case 2")
	}
	stats := external.ResolveExceptionTypes(doc)

	if got := countExceptionTypeNodes(doc, "exception:NotFoundException"); got != 1 {
		t.Fatalf("unresolvable: synthetic node must be KEPT (exactly 1), got %d", got)
	}
	// THROWS still lands on the (single) synthetic node — no orphan, no drop.
	landsOnSynthetic := false
	for _, e := range throwsEdges(doc) {
		if tgt := entByID(doc, e.ToID); tgt != nil && tgt.Kind == string(types.EntityKindExceptionType) {
			landsOnSynthetic = true
		}
	}
	if !landsOnSynthetic {
		t.Fatal("unresolvable: THROWS must still land on the kept synthetic node")
	}
	if stats.SyntheticDropped != 0 {
		t.Fatalf("unresolvable: nothing should be dropped, got %+v", stats)
	}
}
