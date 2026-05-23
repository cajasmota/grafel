package enrichment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"
)

// TestRepairEdgeID_StableAcrossRuns asserts that repairEdgeID returns the
// same value for the same (fromID, relation, originalStub) tuple and that
// changing any one input flips the output. Stability is the whole point —
// agents key resolutions on edge_id and we don't want a non-content change
// (e.g. file order) to invalidate every prior decision.
func TestRepairEdgeID_StableAcrossRuns(t *testing.T) {
	a := repairEdgeID("abc123", "CALLS", "self.helper.process_image")
	b := repairEdgeID("abc123", "CALLS", "self.helper.process_image")
	if a != b {
		t.Fatalf("repairEdgeID not stable: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "er:") || len(a) != len("er:")+16 {
		t.Fatalf("repairEdgeID shape wrong: %q", a)
	}
	// Every input matters.
	if a == repairEdgeID("xyz999", "CALLS", "self.helper.process_image") {
		t.Fatalf("repairEdgeID ignores fromID")
	}
	if a == repairEdgeID("abc123", "EXTENDS", "self.helper.process_image") {
		t.Fatalf("repairEdgeID ignores relation")
	}
	if a == repairEdgeID("abc123", "CALLS", "self.helper.other") {
		t.Fatalf("repairEdgeID ignores original_stub")
	}
}

// TestRepairCandidateID_DerivedFromEdgeID asserts the candidate-id is a
// deterministic function of the edge-id alone — readers must be able to
// recompute one from the other without a join.
func TestRepairCandidateID_DerivedFromEdgeID(t *testing.T) {
	e := repairEdgeID("aaaaaaaaaaaaaaaa", "CALLS", "Foo")
	c1 := repairCandidateID(e)
	c2 := repairCandidateID(e)
	if c1 != c2 {
		t.Fatalf("repairCandidateID unstable: %q vs %q", c1, c2)
	}
	if !strings.HasPrefix(c1, "ec:") || len(c1) != len("ec:")+16 {
		t.Fatalf("repairCandidateID shape wrong: %q", c1)
	}
}

// TestFileLineCache_WindowSize ensures the source-context window respects
// the configured before/after bounds and clamps to the file's range without
// panicking on short files.
func TestFileLineCache_WindowSize(t *testing.T) {
	tmp := t.TempDir()
	relPath := "src/sample.py"
	src := strings.Repeat("line\n", 100)
	full := filepath.Join(tmp, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	c := newFileLineCache(tmp)
	before, line, after := c.window(relPath, 50, 25, 25)
	if got := len(before); got != 25 {
		t.Fatalf("before len = %d, want 25", got)
	}
	if got := len(after); got != 25 {
		t.Fatalf("after len = %d, want 25", got)
	}
	if line != "line" {
		t.Fatalf("line = %q, want %q", line, "line")
	}

	// Short-file clamp: window centered on line 2 of a 100-line file
	// should yield 1 line before, 25 after.
	before, _, after = c.window(relPath, 2, 25, 25)
	if got := len(before); got != 1 {
		t.Fatalf("clamp-before len = %d, want 1", got)
	}
	if got := len(after); got != 25 {
		t.Fatalf("clamp-after len = %d, want 25", got)
	}

	// Out-of-range request returns empty rather than panicking.
	before, line, after = c.window(relPath, 9999, 25, 25)
	if len(before) != 0 || line != "" || len(after) != 0 {
		t.Fatalf("out-of-range window not empty: %d/%q/%d", len(before), line, len(after))
	}
}

// TestFileLineCache_Imports verifies the import-line sniffer picks up Python,
// Go and JS top-level imports and ignores plain code.
func TestFileLineCache_Imports(t *testing.T) {
	tmp := t.TempDir()
	relPath := "mod.py"
	src := strings.Join([]string{
		"from django.http import JsonResponse",
		"import os",
		"",
		"def foo():",
		"    require_user()", // not an import line
		"    return 1",
	}, "\n")
	if err := os.WriteFile(filepath.Join(tmp, relPath), []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c := newFileLineCache(tmp)
	imps := c.imports(relPath)
	if len(imps) != 2 {
		t.Fatalf("imports = %v, want 2 lines", imps)
	}
	if !strings.Contains(imps[0], "JsonResponse") {
		t.Fatalf("first import = %q", imps[0])
	}
}

// TestCollectRepairEdgeCandidates_SchemaShape verifies that the candidate
// emitted for a synthetic bug-resolver edge carries every required field
// from docs/specs/enrichment-candidates-v2.schema.json.
func TestCollectRepairEdgeCandidates_SchemaShape(t *testing.T) {
	tmp := t.TempDir()
	srcPath := "users/views.py"
	src := strings.Join([]string{
		"from django.http import JsonResponse",
		"from users.models import User",
		"",
		"class UserListView:",
		"    def get(self, request):",     // line 5 — call site
		"        return JsonResponse({})", // line 6
	}, "\n")
	if err := os.MkdirAll(filepath.Join(tmp, filepath.Dir(srcPath)), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, srcPath), []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Construct a graph with one entity ("UserListView.get") and an
	// ambiguous bare-name "save" that the resolver index will see as
	// having two registered kinds → bug-resolver disposition.
	fromID := "aaaaaaaaaaaaaaaa"
	doc := &graph.Document{
		Version: graph.SchemaVersion,
		Repo:    "testrepo",
		Entities: []graph.Entity{
			{
				ID:         fromID,
				Name:       "UserListView.get",
				Kind:       "SCOPE.Operation",
				SourceFile: srcPath,
				StartLine:  5,
				Language:   "python",
			},
		},
		Relationships: []graph.Relationship{
			{
				ID:     "rel1",
				FromID: fromID,
				ToID:   "save", // bare name, will be ambig in the index
				Kind:   "CALLS",
				Properties: map[string]string{
					"language": "python",
				},
			},
		},
	}

	// Build a resolver index with TWO entities named "save" (different
	// kinds) so the bare-name lookup is ambiguous → bug-resolver.
	resolverEntities := []types.EntityRecord{
		{ID: "1111111111111111", Name: "save", Kind: "Function", SourceFile: "a.py"},
		{ID: "2222222222222222", Name: "save", Kind: "Method", SourceFile: "b.py"},
	}
	ridx := resolve.BuildIndex(resolverEntities)

	cands := CollectRepairEdgeCandidates(doc, RepairEdgeCandidateOptions{
		RepoRoot: tmp,
		Allow:    func(string) bool { return false },
		Resolver: &ridx,
	})
	if len(cands) != 1 {
		t.Fatalf("got %d candidates, want 1", len(cands))
	}
	c := cands[0]
	if c.Kind != KindRepairEdge {
		t.Fatalf("kind = %q, want %q", c.Kind, KindRepairEdge)
	}
	if c.SubjectID != fromID {
		t.Fatalf("subject_id = %q, want %q", c.SubjectID, fromID)
	}
	if !strings.HasPrefix(c.ID, "ec:") {
		t.Fatalf("candidate id shape: %q", c.ID)
	}

	// Round-trip through JSON to make sure the context object is
	// serialisable and contains every required field.
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ctx, ok := got["context"].(map[string]any)
	if !ok {
		t.Fatalf("context missing")
	}
	for _, k := range []string{
		"edge_id", "from_entity", "relation", "original_stub",
		"disposition", "disposition_reason", "candidates",
		"context_window", "extracted_metadata",
	} {
		if _, present := ctx[k]; !present {
			t.Fatalf("context.%s missing; ctx = %#v", k, ctx)
		}
	}
	if got, want := ctx["relation"], "CALLS"; got != want {
		t.Fatalf("relation = %v, want %v", got, want)
	}
	if got, want := ctx["original_stub"], "save"; got != want {
		t.Fatalf("original_stub = %v, want %v", got, want)
	}
	if got, want := ctx["disposition"], "bug-resolver"; got != want {
		t.Fatalf("disposition = %v, want %v", got, want)
	}
	// context_window has the call-site line + bounded slices.
	cw := ctx["context_window"].(map[string]any)
	line := cw["line"].(string)
	if !strings.Contains(line, "def get") {
		t.Fatalf("context_window.line = %q, want substring 'def get'", line)
	}
	before := cw["before"].([]any)
	after := cw["after"].([]any)
	if len(before) > RepairEdgeContextWindowBefore {
		t.Fatalf("before len = %d, exceeds bound %d", len(before), RepairEdgeContextWindowBefore)
	}
	if len(after) > RepairEdgeContextWindowAfter {
		t.Fatalf("after len = %d, exceeds bound %d", len(after), RepairEdgeContextWindowAfter)
	}
	// extracted_metadata.file_imports picks up the two top-of-file imports.
	meta := ctx["extracted_metadata"].(map[string]any)
	imps := meta["file_imports"].([]any)
	if len(imps) != 2 {
		t.Fatalf("file_imports len = %d (%v), want 2", len(imps), imps)
	}
	if meta["language"] != "python" {
		t.Fatalf("language = %v", meta["language"])
	}
}

// TestCollectRepairEdgeCandidates_SkipsResolvedAndExternal verifies that
// edges whose ToID is already a hex entity ID or an "ext:" placeholder are
// silently skipped — those aren't repair targets.
func TestCollectRepairEdgeCandidates_SkipsResolvedAndExternal(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "X", Kind: "Function", SourceFile: "a.py", StartLine: 1, Language: "python"},
		},
		Relationships: []graph.Relationship{
			{FromID: "aaaaaaaaaaaaaaaa", ToID: "bbbbbbbbbbbbbbbb", Kind: "CALLS"}, // already resolved
			{FromID: "aaaaaaaaaaaaaaaa", ToID: "ext:requests", Kind: "CALLS"},     // external
		},
	}
	ridx := resolve.BuildIndex(nil)
	got := CollectRepairEdgeCandidates(doc, RepairEdgeCandidateOptions{
		RepoRoot: "",
		Allow:    func(string) bool { return false },
		Resolver: &ridx,
	})
	if len(got) != 0 {
		t.Fatalf("expected 0 candidates for resolved+external edges, got %d", len(got))
	}
}

// TestCollectRepairEdgeCandidates_NonHexFromID asserts the emitter does NOT
// drop edges whose FromID isn't a stamped hex ID — un-stamped qualified-name
// FromIDs (e.g. "Model:View", "scope:component:file:src/foo.py") still
// produce a repair_edge candidate. This is the #544 follow-up fix: the
// pre-fix emitter silently skipped these because `byID[r.FromID]` was nil,
// which meant 0 candidates emitted on inputs where the #545 reader saw
// residual bug-disposition edges.
func TestCollectRepairEdgeCandidates_NonHexFromID(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			// Note: NO entity for "Model:View" — it's an un-stamped from.
			{ID: "aaaaaaaaaaaaaaaa", Name: "X", Kind: "Function", SourceFile: "a.py", StartLine: 1, Language: "python"},
		},
		Relationships: []graph.Relationship{
			// hex FromID — covered by existing tests, here to assert
			// the two paths produce stable, distinct edge_ids.
			{FromID: "aaaaaaaaaaaaaaaa", ToID: "Foo:Bar", Kind: "CALLS", Properties: map[string]string{"language": "python"}},
			// non-hex FromID — the regression case. Use language "go" so
			// the python-specific SQLAlchemy `Model:Name` short-circuit
			// (DispositionDynamic) does not absorb this edge; we want
			// the classifier to land it on BugExtractor/BugResolver.
			{FromID: "Model:View", ToID: "View:View", Kind: "DEPENDS_ON", Properties: map[string]string{"language": "go"}},
			// scope-prefix from — common in early-pass scope edges.
			{FromID: "scope:component:file:src/manage.py", ToID: "execute_from_command_line", Kind: "CALLS", Properties: map[string]string{"language": "python"}},
		},
	}
	ridx := resolve.BuildIndex(nil)
	got := CollectRepairEdgeCandidates(doc, RepairEdgeCandidateOptions{
		RepoRoot: "",
		Allow:    func(string) bool { return false },
		Resolver: &ridx,
	})
	if len(got) != 3 {
		t.Fatalf("got %d candidates, want 3 (1 hex-from + 2 non-hex-from)", len(got))
	}
	// Each candidate's subject_id matches the relationship's FromID — for
	// non-hex froms, the raw stub flows through unchanged.
	subjects := map[string]bool{}
	for _, c := range got {
		subjects[c.SubjectID] = true
	}
	for _, want := range []string{"aaaaaaaaaaaaaaaa", "Model:View", "scope:component:file:src/manage.py"} {
		if !subjects[want] {
			t.Fatalf("missing subject_id %q in %v", want, subjects)
		}
	}
	// edge_id is stable across re-runs on the same input — the reader
	// (#545) keys on this.
	run := func() map[string]string {
		out := map[string]string{}
		for _, c := range CollectRepairEdgeCandidates(doc, RepairEdgeCandidateOptions{
			RepoRoot: "",
			Allow:    func(string) bool { return false },
			Resolver: &ridx,
		}) {
			ctx := c.Context
			out[c.SubjectID] = ctx["edge_id"].(string)
		}
		return out
	}
	a, b := run(), run()
	for k, va := range a {
		if vb := b[k]; va != vb {
			t.Fatalf("edge_id for %q not stable: %q vs %q", k, va, vb)
		}
		if !strings.HasPrefix(va, "er:") || len(va) != len("er:")+16 {
			t.Fatalf("edge_id shape wrong for %q: %q", k, va)
		}
	}
	// Hex-from and non-hex-from must produce DISTINCT edge_ids — the
	// FromID participates in the hash and changing it must flip the output.
	seen := map[string]bool{}
	for _, eid := range a {
		if seen[eid] {
			t.Fatalf("duplicate edge_id %q across distinct subjects", eid)
		}
		seen[eid] = true
	}
}

// TestSyntheticFromEntity_KindHints exercises the raw-FromID → synthetic
// graph.Entity parser. The Kind / SourceFile fields are best-effort hints
// for the agent and shouldn't crash on unusual stub shapes.
func TestSyntheticFromEntity_KindHints(t *testing.T) {
	cases := []struct {
		raw      string
		wantKind string
		wantName string
		wantFile string
	}{
		{"scope:component:file:src/manage.py", "file", "src/manage.py", "src/manage.py"},
		{"scope:component:class:python:src/users/views.py:UserView", "class", "UserView", "src/users/views.py"},
		{"Model:View", "model", "View", ""},
		{"Route:User", "route", "User", ""},
		{"bare_name_no_colon", "", "bare_name_no_colon", ""},
	}
	for _, tc := range cases {
		got := syntheticFromEntity(tc.raw)
		if got.ID != tc.raw {
			t.Errorf("%s: ID = %q, want %q", tc.raw, got.ID, tc.raw)
		}
		if got.Kind != tc.wantKind {
			t.Errorf("%s: Kind = %q, want %q", tc.raw, got.Kind, tc.wantKind)
		}
		if got.Name != tc.wantName {
			t.Errorf("%s: Name = %q, want %q", tc.raw, got.Name, tc.wantName)
		}
		if got.SourceFile != tc.wantFile {
			t.Errorf("%s: SourceFile = %q, want %q", tc.raw, got.SourceFile, tc.wantFile)
		}
	}
}

// TestCollectRepairEdgeCandidates_DeterministicOrdering ensures repeated
// calls on the same doc produce candidates in the same order — readers and
// diff tools depend on byte-stable output.
func TestCollectRepairEdgeCandidates_DeterministicOrdering(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.py"), []byte("x = 1\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "X", Kind: "Function", SourceFile: "a.py", StartLine: 1, Language: "python"},
		},
		Relationships: []graph.Relationship{
			{FromID: "aaaaaaaaaaaaaaaa", ToID: "Foo:Missing", Kind: "CALLS", Properties: map[string]string{"language": "python"}},
			{FromID: "aaaaaaaaaaaaaaaa", ToID: "Bar:AlsoMissing", Kind: "CALLS", Properties: map[string]string{"language": "python"}},
		},
	}
	ridx := resolve.BuildIndex(nil)
	run := func() []string {
		got := CollectRepairEdgeCandidates(doc, RepairEdgeCandidateOptions{
			RepoRoot: tmp,
			Allow:    func(string) bool { return false },
			Resolver: &ridx,
		})
		ids := make([]string, len(got))
		for i, c := range got {
			ids[i] = c.ID
		}
		return ids
	}
	a := run()
	b := run()
	if len(a) != len(b) {
		t.Fatalf("run len mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("ordering not deterministic at %d: %s vs %s", i, a[i], b[i])
		}
	}
}
