// migration_prune_test.go — regression coverage for #2706.
//
// Asserts that PruneMigrationEntities drops every container/scope-shaped
// entity anchored to a Django migration file regardless of which
// extractor produced it (per-language FileEntity, file_convention
// glob-based dispatch, framework synthesisers, etc.), while leaving the
// canonical Migration entity intact.

package extractors

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

func TestIsDjangoMigrationFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"core/migrations/0001_initial.py", true},
		{"app/migrations/0042_alter_field.py", true},
		{"some/long/path/to/migrations/9999_squashed.py", true},
		{"migrations/0001_initial.py", true},

		// Non-migration paths.
		{"core/views.py", false},
		{"core/migrations.py", false},               // file named migrations.py (not in a migrations/ dir)
		{"core/migrations/__init__.py", true},       // technically a migration package file — pruned
		{"core/migrations/0001_initial.txt", false}, // not Python
		{"core/migrations_helper/0001.py", false},   // dir is migrations_helper, not migrations
		{"core/migration/0001.py", false},           // singular "migration"
		{"", false},
	}
	for _, c := range cases {
		if got := IsDjangoMigrationFile(c.path); got != c.want {
			t.Errorf("IsDjangoMigrationFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestPruneMigrationEntities_DropsAllContainerKinds(t *testing.T) {
	// Simulate the worst-case state that triggered #2706: every Wave 3-5
	// emission path produces a SCOPE.Component / Class / Operation for the
	// same migration file. After the prune the only migration-anchored
	// entity that survives must be kind="Migration".
	doc := &graph.Document{
		Entities: []graph.Entity{
			// FileEntity from per-language Python extractor (#577).
			{ID: "a1", Name: "core/migrations/0001_initial.py", Kind: "SCOPE.Component", Subtype: "file", SourceFile: "core/migrations/0001_initial.py"},
			// Cross-language hierarchy extractor before #2616 fix.
			{ID: "a2", Name: "Migration", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "core/migrations/0001_initial.py"},
			// SCOPE.Class shape sometimes emitted by other paths.
			{ID: "a3", Name: "Migration", Kind: "SCOPE.Class", SourceFile: "core/migrations/0001_initial.py"},
			// SCOPE.Operation shape (e.g. operations-bound function entity).
			{ID: "a4", Name: "forwards_func", Kind: "SCOPE.Operation", SourceFile: "core/migrations/0001_initial.py"},
			// Bare Class / Operation / Component variants (defensive).
			{ID: "a5", Name: "Migration", Kind: "Class", SourceFile: "core/migrations/0001_initial.py"},
			{ID: "a6", Name: "noop", Kind: "Operation", SourceFile: "core/migrations/0001_initial.py"},
			{ID: "a7", Name: "noop", Kind: "Component", SourceFile: "core/migrations/0001_initial.py"},

			// The intended Migration file-tag — MUST be preserved.
			{ID: "mig", Name: "0001_initial", Kind: "Migration", SourceFile: "core/migrations/0001_initial.py"},

			// Non-migration entities — MUST be preserved.
			{ID: "b1", Name: "User", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "core/models.py"},
			{ID: "b2", Name: "UserViewSet", Kind: "Class", SourceFile: "core/views.py"},
		},
		Relationships: []graph.Relationship{
			// Edge between two pruned migration entities — should be dropped.
			{ID: "r1", FromID: "a1", ToID: "a2", Kind: "CONTAINS"},
			// Edge FROM a non-migration entity TO a pruned migration entity — should be dropped.
			{ID: "r2", FromID: "b1", ToID: "a3", Kind: "DEPENDS_ON"},
			// Edge FROM a pruned migration entity TO a non-migration entity — should be dropped.
			{ID: "r3", FromID: "a4", ToID: "b1", Kind: "CALLS"},
			// Edge between two non-migration entities — should be kept.
			{ID: "r4", FromID: "b1", ToID: "b2", Kind: "DEPENDS_ON"},
			// Edge involving the kept Migration entity — should be kept (its ID is not in removedIDs).
			{ID: "r5", FromID: "mig", ToID: "b1", Kind: "DEPENDS_ON"},
		},
	}

	t.Setenv("ARCHIGRAPH_EMIT_MIGRATION_ENTITIES", "") // ensure prune is active

	ePruned, rPruned := PruneMigrationEntities(doc)

	if ePruned != 7 {
		t.Errorf("entities pruned = %d, want 7", ePruned)
	}
	if rPruned != 3 {
		t.Errorf("relationships pruned = %d, want 3", rPruned)
	}

	// Assert ZERO container-kind migration entities remain.
	for _, e := range doc.Entities {
		if IsDjangoMigrationFile(e.SourceFile) && prunedMigrationKinds[e.Kind] {
			t.Errorf("migration entity survived prune: id=%s kind=%s source=%s", e.ID, e.Kind, e.SourceFile)
		}
	}

	// Assert the canonical Migration entity + non-migration entities survived.
	wantSurvivors := map[string]bool{"mig": true, "b1": true, "b2": true}
	gotSurvivors := make(map[string]bool, len(doc.Entities))
	for _, e := range doc.Entities {
		gotSurvivors[e.ID] = true
	}
	for id := range wantSurvivors {
		if !gotSurvivors[id] {
			t.Errorf("expected entity %s to survive prune, but it was dropped", id)
		}
	}
	if len(gotSurvivors) != len(wantSurvivors) {
		t.Errorf("survivor count = %d, want %d (extra: %v)", len(gotSurvivors), len(wantSurvivors), gotSurvivors)
	}

	// Assert relationship r4 + r5 survived.
	wantRels := map[string]bool{"r4": true, "r5": true}
	for _, r := range doc.Relationships {
		if !wantRels[r.ID] {
			t.Errorf("unexpected relationship survived prune: %s", r.ID)
		}
		delete(wantRels, r.ID)
	}
	for id := range wantRels {
		t.Errorf("expected relationship %s to survive prune, but it was dropped", id)
	}
}

func TestPruneMigrationEntities_OptInBypass(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "a1", Name: "Migration", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "core/migrations/0001_initial.py"},
			{ID: "a2", Name: "User", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "core/models.py"},
		},
	}

	t.Setenv("ARCHIGRAPH_EMIT_MIGRATION_ENTITIES", "1")

	ePruned, rPruned := PruneMigrationEntities(doc)
	if ePruned != 0 || rPruned != 0 {
		t.Errorf("opt-in bypass: pruned=(%d,%d), want (0,0)", ePruned, rPruned)
	}
	if len(doc.Entities) != 2 {
		t.Errorf("opt-in bypass: entities len = %d, want 2", len(doc.Entities))
	}
}

func TestPruneMigrationEntities_Idempotent(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "a1", Name: "Migration", Kind: "SCOPE.Component", SourceFile: "core/migrations/0001_initial.py"},
			{ID: "b1", Name: "User", Kind: "SCOPE.Component", SourceFile: "core/models.py"},
		},
	}
	t.Setenv("ARCHIGRAPH_EMIT_MIGRATION_ENTITIES", "")

	e1, _ := PruneMigrationEntities(doc)
	e2, r2 := PruneMigrationEntities(doc) // second call must be a no-op
	if e1 != 1 {
		t.Errorf("first call entities pruned = %d, want 1", e1)
	}
	if e2 != 0 || r2 != 0 {
		t.Errorf("second call pruned = (%d,%d), want (0,0) — prune is not idempotent", e2, r2)
	}
}

func TestPruneMigrationEntities_NilSafe(t *testing.T) {
	t.Setenv("ARCHIGRAPH_EMIT_MIGRATION_ENTITIES", "")
	e, r := PruneMigrationEntities(nil)
	if e != 0 || r != 0 {
		t.Errorf("nil doc: pruned = (%d,%d), want (0,0)", e, r)
	}
}

// TestPruneMigrationEntities_PrunesControllerKind exercises #3173:
// Falcon/CherryPy YAML source_patterns match `class X(...):` in any Python file
// and emit Kind="Controller" entities. On Django migration files this produces
// "class Migration(migrations.Migration):" -> Controller noise. The fix adds
// "Controller" to prunedMigrationKinds so these entities are pruned just like
// SCOPE.Component and Class entities.
func TestPruneMigrationEntities_PrunesControllerKind(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			// Falcon/CherryPy YAML pattern: `class Migration(...):` emits Controller.
			{ID: "c1", Name: "Migration", Kind: "Controller", SourceFile: "core/migrations/0001_initial.py"},
			// The canonical Migration file-tag must survive.
			{ID: "mig", Name: "0001_initial", Kind: "Migration", SourceFile: "core/migrations/0001_initial.py"},
			// Non-migration Controller entity must survive.
			{ID: "ctrl", Name: "UserController", Kind: "Controller", SourceFile: "core/views.py"},
		},
		Relationships: []graph.Relationship{
			// Edge referencing the pruned Controller entity — must be dropped.
			{ID: "r1", FromID: "c1", ToID: "mig", Kind: "DEPENDS_ON"},
			// Edge between surviving entities — must be kept.
			{ID: "r2", FromID: "ctrl", ToID: "mig", Kind: "DEPENDS_ON"},
		},
	}

	t.Setenv("ARCHIGRAPH_EMIT_MIGRATION_ENTITIES", "")

	ePruned, rPruned := PruneMigrationEntities(doc)

	if ePruned != 1 {
		t.Errorf("entities pruned = %d, want 1 (the Controller on migration file)", ePruned)
	}
	if rPruned != 1 {
		t.Errorf("relationships pruned = %d, want 1 (the edge referencing pruned entity)", rPruned)
	}

	// Verify no Controller entity on a migration file survived.
	for _, e := range doc.Entities {
		if IsDjangoMigrationFile(e.SourceFile) && prunedMigrationKinds[e.Kind] {
			t.Errorf("entity on migration file survived prune: id=%s kind=%s", e.ID, e.Kind)
		}
	}

	// Verify exactly the correct entities survived.
	wantIDs := map[string]bool{"mig": true, "ctrl": true}
	gotIDs := make(map[string]bool, len(doc.Entities))
	for _, e := range doc.Entities {
		gotIDs[e.ID] = true
	}
	for id := range wantIDs {
		if !gotIDs[id] {
			t.Errorf("expected entity %s to survive, but it was dropped", id)
		}
	}
	if len(gotIDs) != len(wantIDs) {
		t.Errorf("entity count = %d, want %d (unexpected: %v)", len(gotIDs), len(wantIDs), gotIDs)
	}

	// Verify exactly the correct relationships survived.
	wantRels := map[string]bool{"r2": true}
	for _, r := range doc.Relationships {
		if !wantRels[r.ID] {
			t.Errorf("unexpected relationship survived prune: %s", r.ID)
		}
		delete(wantRels, r.ID)
	}
	for id := range wantRels {
		t.Errorf("expected relationship %s to survive, but it was dropped", id)
	}
}
