package fbwriter_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
	fb "github.com/cajasmota/archigraph/internal/graph/fbgraph"
	"github.com/cajasmota/archigraph/internal/graph/fbreader"
	"github.com/cajasmota/archigraph/internal/graph/fbversion"
	"github.com/cajasmota/archigraph/internal/graph/fbwriter"
)

func TestRoundtripSmallGraph(t *testing.T) {
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		Repo:        "fixture-mini",
		Entities: []graph.Entity{
			{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go", StartLine: 10, Properties: map[string]string{"module": "pkg/a"}},
			{ID: "ent0000000000000b", Name: "bar", Kind: "function", SourceFile: "b.go", StartLine: 20, Properties: map[string]string{"module": "pkg/b", "visibility": "public"}},
			{ID: "ent0000000000000c", Name: "Baz", Kind: "type", SourceFile: "c.go", StartLine: 30},
		},
		Relationships: []graph.Relationship{
			{ID: "rel000000000000aa", FromID: "ent0000000000000a", ToID: "ent0000000000000b", Kind: "calls"},
			{ID: "rel000000000000ab", FromID: "ent0000000000000a", ToID: "ent0000000000000c", Kind: "references", Properties: map[string]string{"resolved": "true"}},
			{ID: "rel000000000000bc", FromID: "ent0000000000000b", ToID: "ent0000000000000c", Kind: "calls"},
		},
	}
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)

	out := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(out, doc); err != nil {
		t.Fatalf("write: %v", err)
	}

	r, err := fbreader.Open(out)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	if got := r.Version(); got != fbwriter.FormatVersion {
		t.Errorf("version: got %d want %d", got, fbwriter.FormatVersion)
	}
	if got := r.EntityCount(); got != 3 {
		t.Errorf("entity count: got %d want 3", got)
	}
	if got := r.RelationshipCount(); got != 3 {
		t.Errorf("relationship count: got %d want 3", got)
	}

	ent := r.LookupEntityByID("ent0000000000000b")
	if ent == nil {
		t.Fatal("lookup ent0000000000000b: nil")
	}
	if got := string(ent.Name()); got != "bar" {
		t.Errorf("name: got %q want %q", got, "bar")
	}
	if got := string(ent.SourceFile()); got != "b.go" {
		t.Errorf("source_file: got %q want %q", got, "b.go")
	}
	if got := ent.SourceLine(); got != 20 {
		t.Errorf("source_line: got %d want 20", got)
	}

	// Negative lookup.
	if got := r.LookupEntityByID("does-not-exist"); got != nil {
		t.Errorf("expected nil for missing key, got %+v", got)
	}

	// Relationship traversal by from_id.
	out2 := r.IterateRelationshipsFromID("ent0000000000000a")
	if len(out2) != 2 {
		t.Errorf("rels from a: got %d want 2", len(out2))
	}
}

// TestRoundtripCommunityAttrs verifies the Pass-4 community/algorithm
// attributes added in #1620 survive a write→read cycle through graph.fb:
// per-entity community_id/pagerank/flags AND the aggregate Communities list
// + corpus AlgorithmStats. A graph written WITHOUT these (algo skipped) must
// read back with nil pointers / zero communities so the JSON path stays
// byte-equivalent.
func TestRoundtripCommunityAttrs(t *testing.T) {
	cid := 7
	pr := 0.0421
	cen := 1.5
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC),
		Repo:        "fixture-comm",
		Entities: []graph.Entity{
			{
				ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go", StartLine: 1,
				CommunityID: &cid, PageRank: &pr, Centrality: &cen,
				IsGodNode: true, IsArticulationPt: true,
			},
			// No algo attrs — must read back nil/false.
			{ID: "ent0000000000000b", Name: "bar", Kind: "function", SourceFile: "b.go", StartLine: 2},
		},
		Communities: []graph.CommunityResult{
			{ID: 7, Size: 42, Modularity: 0.68, TopEntities: []string{"foo", "bar"}, AutoName: "auth-core"},
			{ID: 9, Size: 13, Modularity: 0.51, AutoName: "billing"},
		},
		AlgorithmStats: &graph.AlgorithmStats{
			LouvainModularity: 0.68, NumCommunities: 2, NumGodNodes: 1,
			NumArticulationPts: 1, RuntimeMS: 1234, DenoisedCommunities: 3,
		},
	}
	doc.Stats.Entities = len(doc.Entities)

	out := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(out, doc); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := graph.LoadGraphFromDir(filepath.Dir(out))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Aggregate communities.
	if len(got.Communities) != 2 {
		t.Fatalf("communities: got %d want 2", len(got.Communities))
	}
	c0 := got.Communities[0]
	if c0.ID != 7 || c0.Size != 42 || c0.AutoName != "auth-core" {
		t.Errorf("community[0] mismatch: %+v", c0)
	}
	if len(c0.TopEntities) != 2 || c0.TopEntities[0] != "foo" {
		t.Errorf("community[0] top_entities mismatch: %+v", c0.TopEntities)
	}
	if got.AlgorithmStats == nil {
		t.Fatal("algorithm_stats: nil")
	}
	if got.AlgorithmStats.NumGodNodes != 1 || got.AlgorithmStats.DenoisedCommunities != 3 || got.AlgorithmStats.RuntimeMS != 1234 {
		t.Errorf("algorithm_stats mismatch: %+v", *got.AlgorithmStats)
	}

	// Per-entity attrs (entity a).
	var ea, eb *graph.Entity
	for i := range got.Entities {
		switch got.Entities[i].ID {
		case "ent0000000000000a":
			ea = &got.Entities[i]
		case "ent0000000000000b":
			eb = &got.Entities[i]
		}
	}
	if ea == nil || eb == nil {
		t.Fatalf("entities not found: a=%v b=%v", ea, eb)
	}
	if ea.CommunityID == nil || *ea.CommunityID != 7 {
		t.Errorf("entity a community_id: got %v want 7", ea.CommunityID)
	}
	if ea.PageRank == nil || *ea.PageRank != 0.0421 {
		t.Errorf("entity a pagerank: got %v want 0.0421", ea.PageRank)
	}
	if !ea.IsGodNode || !ea.IsArticulationPt {
		t.Errorf("entity a flags: god=%v artic=%v", ea.IsGodNode, ea.IsArticulationPt)
	}
	// Entity b had no algo attrs — must be nil/false.
	if eb.CommunityID != nil || eb.PageRank != nil || eb.IsGodNode {
		t.Errorf("entity b should be un-annotated: cid=%v pr=%v god=%v", eb.CommunityID, eb.PageRank, eb.IsGodNode)
	}
}

// TestRoundtripGitMeta verifies that Phase-0 git metadata (#2088) survives a
// write→read cycle through graph.fb, and that an old graph written without
// these fields reads back with the zero-value defaults (empty string / false).
func TestRoundtripGitMeta(t *testing.T) {
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
		Repo:        "fixture-gitmeta",
		Entities:    []graph.Entity{{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go"}},
		// Phase 0 fields.
		IndexedRef: "feat/my-feature",
		IndexedSHA: "abc123def456",
		IsWorktree: true,
	}
	doc.Stats.Entities = 1
	out := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(out, doc); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := graph.LoadGraphFromDir(filepath.Dir(out))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.IndexedRef != "feat/my-feature" {
		t.Errorf("IndexedRef: got %q want %q", got.IndexedRef, "feat/my-feature")
	}
	if got.IndexedSHA != "abc123def456" {
		t.Errorf("IndexedSHA: got %q want %q", got.IndexedSHA, "abc123def456")
	}
	if !got.IsWorktree {
		t.Error("IsWorktree: got false want true")
	}

	// Test zero-value defaults: write a doc WITHOUT git meta, verify "" / false.
	docNoMeta := &graph.Document{
		Version:     1,
		GeneratedAt: time.Now().UTC(),
		Repo:        "fixture-nometa",
		Entities:    []graph.Entity{{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go"}},
	}
	docNoMeta.Stats.Entities = 1
	out2 := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(out2, docNoMeta); err != nil {
		t.Fatalf("write no-meta: %v", err)
	}
	got2, err := graph.LoadGraphFromDir(filepath.Dir(out2))
	if err != nil {
		t.Fatalf("load no-meta: %v", err)
	}
	if got2.IndexedRef != "" || got2.IndexedSHA != "" || got2.IsWorktree {
		t.Errorf("zero-value defaults: ref=%q sha=%q wt=%v", got2.IndexedRef, got2.IndexedSHA, got2.IsWorktree)
	}
}

// TestRoundtripCoverageStatus verifies that the M4 sparse-checkout
// coverage_status field (#2181) survives a write→read cycle through graph.fb,
// and that a graph written without it reads back with the zero-value default
// ("" — treated as "full" by readers).
func TestRoundtripCoverageStatus(t *testing.T) {
	// Case 1: partial coverage (sparse checkout).
	docPartial := &graph.Document{
		Version:        1,
		GeneratedAt:    time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC),
		Repo:           "fixture-sparse",
		Entities:       []graph.Entity{{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "services/payments/a.go"}},
		CoverageStatus: "partial",
	}
	docPartial.Stats.Entities = 1
	out := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(out, docPartial); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	gotPartial, err := graph.LoadGraphFromDir(filepath.Dir(out))
	if err != nil {
		t.Fatalf("load partial: %v", err)
	}
	if gotPartial.CoverageStatus != "partial" {
		t.Errorf("CoverageStatus: got %q want %q", gotPartial.CoverageStatus, "partial")
	}

	// Case 2: full coverage (field absent — default).
	docFull := &graph.Document{
		Version:     1,
		GeneratedAt: time.Now().UTC(),
		Repo:        "fixture-full",
		Entities:    []graph.Entity{{ID: "ent0000000000000a", Name: "bar", Kind: "function", SourceFile: "main.go"}},
		// CoverageStatus intentionally omitted → should read back as "".
	}
	docFull.Stats.Entities = 1
	out2 := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(out2, docFull); err != nil {
		t.Fatalf("write full: %v", err)
	}
	gotFull, err := graph.LoadGraphFromDir(filepath.Dir(out2))
	if err != nil {
		t.Fatalf("load full: %v", err)
	}
	if gotFull.CoverageStatus != "" {
		t.Errorf("CoverageStatus default: got %q want empty string", gotFull.CoverageStatus)
	}
}

// TestRoundtripLanguage verifies that Entity.Language survives a write→read
// cycle through graph.fb (issue #2341, refined by #2370). Before #2341,
// Language was never serialized; #2341 tunneled it via Properties["language"];
// #2370 retires the tunnel and persists it via the dedicated FB slot.
func TestRoundtripLanguage(t *testing.T) {
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		Repo:        "fixture-language",
		Entities: []graph.Entity{
			// Entity with Language set but NOT in Properties — the common case
			// produced by extractors that set entity.Language directly.
			{ID: "ent0000000000000a", Name: "MyView", Kind: "class",
				SourceFile: "views.py", Language: "python"},
			// Entity with Language already mirrored in Properties (extractor
			// overrides must be preserved, not doubled).
			{ID: "ent0000000000000b", Name: "handler", Kind: "function",
				SourceFile: "main.go", Language: "go",
				Properties: map[string]string{"language": "go", "module": "cmd/main"}},
			// Entity with no Language and no recognized extension (synthetic).
			// Must round-trip with Language="" (no spurious tag injected).
			{ID: "ent0000000000000c", Name: "ext:requests", Kind: "external",
				SourceFile: ""},
		},
	}
	doc.Stats.Entities = len(doc.Entities)

	out := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(out, doc); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := graph.LoadGraphFromDir(filepath.Dir(out))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	byID := make(map[string]*graph.Entity, len(got.Entities))
	for i := range got.Entities {
		e := &got.Entities[i]
		byID[e.ID] = e
	}

	// entity a: Language="python" must survive the roundtrip.
	ea := byID["ent0000000000000a"]
	if ea == nil {
		t.Fatal("entity a not found after roundtrip")
	}
	if ea.Language != "python" {
		t.Errorf("entity a Language: got %q want %q", ea.Language, "python")
	}
	// #2370: Properties["language"] is no longer tunneled by the FB writer.
	// Entity a was written with Properties unset for "language", so on read
	// back the props map must not contain a synthesized "language" key.
	if _, present := ea.Properties["language"]; present {
		t.Errorf("entity a Properties[language]: unexpectedly present after #2370 retired the property-tunnel; props=%v",
			ea.Properties)
	}

	// entity b: Language="go" already in Properties — must not be duplicated
	// or overwritten.
	eb := byID["ent0000000000000b"]
	if eb == nil {
		t.Fatal("entity b not found after roundtrip")
	}
	if eb.Language != "go" {
		t.Errorf("entity b Language: got %q want %q", eb.Language, "go")
	}
	if eb.Properties["language"] != "go" {
		t.Errorf("entity b Properties[language]: got %q want %q",
			eb.Properties["language"], "go")
	}
	// Other properties must be preserved.
	if eb.Properties["module"] != "cmd/main" {
		t.Errorf("entity b Properties[module]: got %q want %q",
			eb.Properties["module"], "cmd/main")
	}

	// entity c: no Language, no source file — must read back with Language="".
	ec := byID["ent0000000000000c"]
	if ec == nil {
		t.Fatal("entity c not found after roundtrip")
	}
	if ec.Language != "" {
		t.Errorf("entity c Language: got %q want empty", ec.Language)
	}
}

// TestRoundtripNoAlgoData verifies a graph written with NO community data
// (the --skip-pass=graph-algo case) reads back with zero communities, nil
// AlgorithmStats, and un-annotated entities (#1620 — old-file compatibility).
func TestRoundtripNoAlgoData(t *testing.T) {
	doc := &graph.Document{
		Version: 1, GeneratedAt: time.Now().UTC(), Repo: "fixture-noalgo",
		Entities: []graph.Entity{{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go"}},
	}
	doc.Stats.Entities = 1
	out := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(out, doc); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := graph.LoadGraphFromDir(filepath.Dir(out))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Communities) != 0 {
		t.Errorf("communities: got %d want 0", len(got.Communities))
	}
	if got.AlgorithmStats != nil {
		t.Errorf("algorithm_stats: got %+v want nil", got.AlgorithmStats)
	}
	if got.Entities[0].CommunityID != nil {
		t.Errorf("entity community_id: got %v want nil", got.Entities[0].CommunityID)
	}
}

// TestLanguageSlotInRawFB verifies that #2370's dedicated `language` slot is
// actually written into the on-disk FlatBuffer — not just exposed through the
// high-level loader. We open the raw FB and inspect Entity.Language() directly,
// which catches any future regression where the writer silently stops emitting
// the slot.
func TestLanguageSlotInRawFB(t *testing.T) {
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		Repo:        "fixture-language-slot",
		Entities: []graph.Entity{
			{ID: "ent0000000000000a", Name: "Foo", Kind: "class",
				SourceFile: "foo.py", Language: "python"},
			{ID: "ent0000000000000b", Name: "Bar", Kind: "function",
				SourceFile: "bar.rs", Language: "rust"},
			// Entity with no language — slot must be absent (empty string).
			{ID: "ent0000000000000c", Name: "Baz", Kind: "external", SourceFile: ""},
		},
	}
	doc.Stats.Entities = len(doc.Entities)

	out := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(out, doc); err != nil {
		t.Fatalf("write: %v", err)
	}

	r, err := fbreader.Open(out)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	want := map[string]string{
		"ent0000000000000a": "python",
		"ent0000000000000b": "rust",
		"ent0000000000000c": "",
	}
	for i := 0; i < r.EntityCount(); i++ {
		ent := r.EntityAt(i)
		if ent == nil {
			t.Fatalf("entity %d: nil", i)
		}
		id := string(ent.Id())
		got := string(ent.Language())
		if got != want[id] {
			t.Errorf("entity %s Language(): got %q want %q", id, got, want[id])
		}
	}
}

// TestLoaderRejectsOldFormatVersion verifies that the loader fails loudly with
// the reindex-required error when graph.fb carries an older FormatVersion
// (#2370). archigraph is pre-1.0; there is no compat path.
func TestLoaderRejectsOldFormatVersion(t *testing.T) {
	// Build a minimal graph.fb in-memory with Version=2 (the pre-#2370 format).
	doc := &graph.Document{
		Version: 1, GeneratedAt: time.Now().UTC(), Repo: "fixture-old-version",
		Entities: []graph.Entity{{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go"}},
	}
	doc.Stats.Entities = 1
	buf, err := fbwriter.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Patch the on-disk Graph.version field down to 2 to simulate an old file.
	root := fb.GetRootAsGraph(buf, 0)
	// MutateVersion is generated by flatc for primitive fields with the "mutable" attribute.
	// If flatc ever stops emitting Mutate* accessors, this test must be rewritten to build
	// the old-format file via direct byte manipulation.
	if !root.MutateVersion(2) {
		t.Fatalf("MutateVersion(2) returned false — slot missing?")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "graph.fb")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, loadErr := graph.LoadGraphFromDir(dir)
	if loadErr == nil {
		t.Fatal("expected reindex error, got nil")
	}
	msg := loadErr.Error()
	// Spot-check the operative phrases — verbatim error string is part of the
	// loader contract and must point the user at `archigraph index`.
	for _, want := range []string{
		fmt.Sprintf("graph.fb format version 2 is older than required version %d", fbversion.Version),
		"please reindex",
		"archigraph index <repo>",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q\nfull message: %s", want, msg)
		}
	}
}

// TestRoundtripSignature guards #4881: the Entity `signature` slot must
// round-trip through the binary graph.fb path. Before the fix the FB Entity
// table had no signature field, so every entity's Signature was silently
// dropped on write — which emptied SCOPE.Schema field TYPES in the dashboard.
// Entities with an empty signature must read back empty (no spurious value).
func TestRoundtripSignature(t *testing.T) {
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC),
		Repo:        "fixture-sig",
		Entities: []graph.Entity{
			{ID: "ent0000000000000a", Name: "Dto.id", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: "a.ts", StartLine: 1, Signature: "id: number"},
			{ID: "ent0000000000000b", Name: "Dto.type", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: "a.ts", StartLine: 2, Signature: "type: string | null"},
			// No signature — must read back empty.
			{ID: "ent0000000000000c", Name: "bare", Kind: "function", SourceFile: "b.ts", StartLine: 3},
		},
	}
	doc.Stats.Entities = len(doc.Entities)

	out := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(out, doc); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := graph.LoadGraphFromDir(filepath.Dir(out))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	sigByID := map[string]string{}
	for i := range got.Entities {
		sigByID[got.Entities[i].ID] = got.Entities[i].Signature
	}
	if sigByID["ent0000000000000a"] != "id: number" {
		t.Errorf("field signature lost: got %q want %q", sigByID["ent0000000000000a"], "id: number")
	}
	if sigByID["ent0000000000000b"] != "type: string | null" {
		t.Errorf("nullable field signature lost: got %q want %q", sigByID["ent0000000000000b"], "type: string | null")
	}
	if sigByID["ent0000000000000c"] != "" {
		t.Errorf("empty signature should stay empty: got %q", sigByID["ent0000000000000c"])
	}
}
