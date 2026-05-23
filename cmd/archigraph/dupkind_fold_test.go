package main

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"
)

// makeTestID computes the same hex entity-ID the indexer would stamp.
func makeTestID(kind, name, sourceFile string) string {
	return graph.EntityID("test_repo", kind, name, sourceFile)
}

// dupkindIndexer returns a minimal Indexer sufficient for fold method calls.
func dupkindIndexer() *Indexer {
	return &Indexer{repoTag: "test_repo"}
}

// ---------------------------------------------------------------------------
// Pattern 1: View + Component fold
// ---------------------------------------------------------------------------

// TestDupKindFold_ViewComponent_ScopeView verifies that a SCOPE.View entity
// is preferred over a sibling SCOPE.Component/class entity (with role="class")
// sharing the same (source_file, name). After the fold exactly ONE node must
// remain and it must be SCOPE.View. Issue #1727.
func TestDupKindFold_ViewComponent_ScopeView(t *testing.T) {
	const (
		srcFile = "src/views/UserListView.ts"
		name    = "UserListView"
	)

	viewID := makeTestID("SCOPE.View", name, srcFile)
	compID := makeTestID("SCOPE.Component", name, srcFile)

	records := []types.EntityRecord{
		{
			ID:         viewID,
			Kind:       "SCOPE.View",
			Name:       name,
			SourceFile: srcFile,
			StartLine:  5,
			Properties: map[string]string{"framework": "some_framework"},
		},
		{
			// SCOPE.Component with role="class" — hierarchy pass annotation
			// or similar extractor that sets the role but not yet in
			// classLikeComponentSubtypes. isFoldSource must return true.
			ID:         compID,
			Kind:       "SCOPE.Component",
			Name:       name,
			SourceFile: srcFile,
			StartLine:  5,
			Subtype:    "", // empty subtype — would NOT match classLikeComponentSubtypes alone
			Properties: map[string]string{"role": "class"},
		},
	}

	idx := dupkindIndexer()
	out, _, stats := idx.foldClassHierarchyShadows(records, nil)

	if stats.ShadowsFolded != 1 {
		t.Errorf("expected 1 fold, got %d; records=%+v", stats.ShadowsFolded, out)
	}

	// Exactly ONE entity with name=UserListView must survive.
	var survivors []types.EntityRecord
	for _, r := range out {
		if r.Name == name {
			survivors = append(survivors, r)
		}
	}
	if len(survivors) != 1 {
		t.Fatalf("expected 1 surviving node for %q, got %d: %+v", name, len(survivors), survivors)
	}
	if survivors[0].Kind != "SCOPE.View" {
		t.Errorf("surviving node should be SCOPE.View, got kind=%s", survivors[0].Kind)
	}
}

// TestDupKindFold_ViewComponent_BareView verifies that the bare "View" kind
// (emitted by Django YAML rules) absorbs a sibling SCOPE.Component/class
// entity for the same (source_file, name). Issue #1727 — regression guard.
func TestDupKindFold_ViewComponent_BareView(t *testing.T) {
	const (
		srcFile = "users/views.py"
		name    = "UserDetailView"
	)

	viewID := makeTestID("View", name, srcFile)
	compID := makeTestID("SCOPE.Component", name, srcFile)

	records := []types.EntityRecord{
		{
			ID:         viewID,
			Kind:       "View",
			Name:       name,
			SourceFile: srcFile,
			StartLine:  10,
			Properties: map[string]string{"framework": "django"},
		},
		{
			// SCOPE.Component/class emitted by the Python AST extractor
			ID:         compID,
			Kind:       "SCOPE.Component",
			Name:       name,
			SourceFile: srcFile,
			Subtype:    "class",
			StartLine:  10,
		},
	}

	idx := dupkindIndexer()
	out, _, stats := idx.foldClassHierarchyShadows(records, nil)

	if stats.ShadowsFolded != 1 {
		t.Errorf("expected 1 fold (bare View absorbs SCOPE.Component/class), got %d", stats.ShadowsFolded)
	}

	var survivors []types.EntityRecord
	for _, r := range out {
		if r.Name == name {
			survivors = append(survivors, r)
		}
	}
	if len(survivors) != 1 {
		t.Fatalf("expected 1 surviving node for %q, got %d: %+v", name, len(survivors), survivors)
	}
	if survivors[0].Kind != "View" {
		t.Errorf("surviving node should be View, got kind=%s", survivors[0].Kind)
	}
}

// TestDupKindFold_ViewComponent_NoOverFold verifies that two DIFFERENT classes
// in the same file (with different names) are NOT folded together. Issue #1727.
func TestDupKindFold_ViewComponent_NoOverFold(t *testing.T) {
	const srcFile = "users/views.py"

	records := []types.EntityRecord{
		{
			ID:         makeTestID("View", "UserListView", srcFile),
			Kind:       "View",
			Name:       "UserListView",
			SourceFile: srcFile,
			StartLine:  5,
		},
		{
			ID:         makeTestID("SCOPE.Component", "UserListView", srcFile),
			Kind:       "SCOPE.Component",
			Name:       "UserListView",
			SourceFile: srcFile,
			Subtype:    "class",
			StartLine:  5,
		},
		{
			// This class has a DIFFERENT name — must NOT be absorbed.
			ID:         makeTestID("SCOPE.Component", "HelperMixin", srcFile),
			Kind:       "SCOPE.Component",
			Name:       "HelperMixin",
			SourceFile: srcFile,
			Subtype:    "class",
			StartLine:  20,
		},
	}

	idx := dupkindIndexer()
	out, _, stats := idx.foldClassHierarchyShadows(records, nil)

	// Only the UserListView SCOPE.Component should be absorbed.
	if stats.ShadowsFolded != 1 {
		t.Errorf("expected exactly 1 fold (UserListView only), got %d", stats.ShadowsFolded)
	}

	// HelperMixin must still be present.
	var helperNodes int
	for _, r := range out {
		if r.Name == "HelperMixin" {
			helperNodes++
		}
	}
	if helperNodes != 1 {
		t.Errorf("HelperMixin should survive the fold unchanged, got %d nodes", helperNodes)
	}
}

// ---------------------------------------------------------------------------
// Pattern 2: File + Component fold
// ---------------------------------------------------------------------------

// TestDupKindFold_FileComponent_BasicFold verifies that a SCOPE.Component/class
// entity whose name matches the file stem is absorbed into the co-located
// SCOPE.Component(subtype="file") entity. Issue #1727.
func TestDupKindFold_FileComponent_BasicFold(t *testing.T) {
	const (
		srcFile  = "src/components/LoginPage.tsx"
		fileID   = "ffff000000000001" // pre-computed sentinel for test
		classID  = "cccc000000000001"
		className = "LoginPage" // matches stem of "LoginPage.tsx"
	)

	records := []types.EntityRecord{
		{
			// File entity: name == source_file path.
			ID:         fileID,
			Kind:       "SCOPE.Component",
			Subtype:    "file",
			Name:       srcFile,
			SourceFile: srcFile,
			StartLine:  0,
		},
		{
			// Class entity: name matches file stem "loginpage".
			ID:         classID,
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			Name:       className,
			SourceFile: srcFile,
			StartLine:  3,
		},
	}

	idx := dupkindIndexer()
	out, _, stats := idx.foldFileComponentDuplicates(records, nil)

	if stats.Folded != 1 {
		t.Errorf("expected 1 file-component fold, got %d; remaining=%+v", stats.Folded, out)
	}

	// The file entity must be the sole survivor.
	var fileEnts, classEnts int
	for _, r := range out {
		if r.Subtype == "file" {
			fileEnts++
		}
		if r.Subtype == "class" {
			classEnts++
		}
	}
	if fileEnts != 1 {
		t.Errorf("expected 1 file entity survivor, got %d", fileEnts)
	}
	if classEnts != 0 {
		t.Errorf("expected 0 class entity survivors (absorbed), got %d", classEnts)
	}
}

// TestDupKindFold_FileComponent_NoOverFold_DifferentName verifies that a class
// with a name that does NOT match the file stem is NOT absorbed. Issue #1727.
func TestDupKindFold_FileComponent_NoOverFold_DifferentName(t *testing.T) {
	const (
		srcFile   = "src/components/LoginPage.tsx"
		fileID    = "ffff000000000002"
		classID1  = "cccc000000000002"
		classID2  = "cccc000000000003"
		className = "FormValidator" // does NOT match "loginpage"
	)

	records := []types.EntityRecord{
		{
			ID:         fileID,
			Kind:       "SCOPE.Component",
			Subtype:    "file",
			Name:       srcFile,
			SourceFile: srcFile,
			StartLine:  0,
		},
		{
			// LoginPage matches stem — should be absorbed.
			ID:         classID1,
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			Name:       "LoginPage",
			SourceFile: srcFile,
			StartLine:  3,
		},
		{
			// FormValidator does NOT match stem — must NOT be absorbed.
			ID:         classID2,
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			Name:       className,
			SourceFile: srcFile,
			StartLine:  40,
		},
	}

	idx := dupkindIndexer()
	out, _, stats := idx.foldFileComponentDuplicates(records, nil)

	// Only LoginPage should be folded; FormValidator must survive.
	if stats.Folded != 1 {
		t.Errorf("expected exactly 1 fold (LoginPage only), got %d", stats.Folded)
	}

	var formEnts int
	for _, r := range out {
		if r.Name == className {
			formEnts++
		}
	}
	if formEnts != 1 {
		t.Errorf("FormValidator should survive unchanged, got %d entities", formEnts)
	}
}

// TestDupKindFold_FileComponent_EdgeRepoint verifies that edges pointing to the
// absorbed class entity are re-pointed to the file entity survivor. Issue #1727.
func TestDupKindFold_FileComponent_EdgeRepoint(t *testing.T) {
	const (
		srcFile   = "src/components/Button.tsx"
		fileID    = "ffff000000000010"
		classID   = "cccc000000000010"
		callerID  = "eeee000000000010"
		className = "Button"
	)

	records := []types.EntityRecord{
		{
			ID:         fileID,
			Kind:       "SCOPE.Component",
			Subtype:    "file",
			Name:       srcFile,
			SourceFile: srcFile,
			StartLine:  0,
		},
		{
			ID:         classID,
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			Name:       className,
			SourceFile: srcFile,
			StartLine:  1,
		},
		{
			// A caller that IMPORTS or RENDERS the Button class.
			ID:         callerID,
			Kind:       "SCOPE.Component",
			Subtype:    "file",
			Name:       "src/pages/Home.tsx",
			SourceFile: "src/pages/Home.tsx",
			Relationships: []types.RelationshipRecord{
				{Kind: "IMPORTS", ToID: classID},
			},
		},
	}

	idx := dupkindIndexer()
	out, _, stats := idx.foldFileComponentDuplicates(records, nil)

	if stats.Folded != 1 {
		t.Errorf("expected 1 fold, got %d", stats.Folded)
	}
	if stats.EdgesRepointed == 0 {
		t.Errorf("expected at least 1 edge repointed, got 0")
	}

	// The IMPORTS edge on Home.tsx must now point to the file entity.
	for _, r := range out {
		if r.ID == callerID {
			for _, rel := range r.Relationships {
				if rel.Kind == "IMPORTS" && rel.ToID != fileID {
					t.Errorf("IMPORTS edge ToID should be re-pointed to fileID=%s, got %s",
						fileID, rel.ToID)
				}
			}
		}
	}
}

// TestDupKindFold_FileComponent_NoDanglingEdges verifies the file+component
// fold does not introduce new dangling hex-id endpoints relative to the
// unfolded graph. Issue #1727.
func TestDupKindFold_FileComponent_NoDanglingEdges(t *testing.T) {
	t.Setenv("ARCHIGRAPH_DISABLE_1727_FILE_FOLD", "1")
	unfolded := runIndexerOn(t, "testdata/django_cbv_app", "django_cbv_app", nil)
	t.Setenv("ARCHIGRAPH_DISABLE_1727_FILE_FOLD", "")
	folded := runIndexerOn(t, "testdata/django_cbv_app", "django_cbv_app", nil)

	if d := danglingHexEndpoints(folded); d > danglingHexEndpoints(unfolded) {
		t.Errorf("file-component fold introduced new dangling hex endpoints: folded=%d unfolded=%d",
			d, danglingHexEndpoints(unfolded))
	}
}

// TestDupKindFold_ViewComponent_RoleClass_Integration verifies the View+Component
// fold via a full-indexer run on the django_cbv_app fixture. After the fold, no
// (name, source_file) pair should yield BOTH a View kind entity AND a
// SCOPE.Component entity (except intentional pairs like orm_model_sentinel+Model).
// Issue #1727.
func TestDupKindFold_ViewComponent_Integration(t *testing.T) {
	doc := runIndexerOn(t, "testdata/django_cbv_app", "django_cbv_app", nil)

	type key struct{ name, src string }
	byKey := map[key][]string{}
	for _, e := range doc.Entities {
		if e.Name == "" {
			continue
		}
		k := key{e.Name, e.SourceFile}
		byKey[k] = append(byKey[k], e.Kind)
	}

	// For CBV view classes (UserListView, UserDetailView), verify no View+Component pair.
	viewNames := []string{"UserListView", "UserDetailView"}
	for _, name := range viewNames {
		k := key{name, "users/views.py"}
		kinds := byKey[k]
		hasView := false
		hasComponent := false
		for _, kd := range kinds {
			if kd == "View" || kd == "SCOPE.View" {
				hasView = true
			}
			if kd == "SCOPE.Component" {
				hasComponent = true
			}
		}
		if hasView && hasComponent {
			t.Errorf("%s: View+Component duplicate-kind pair still present after fold; kinds=%v", name, kinds)
		}
	}
}
