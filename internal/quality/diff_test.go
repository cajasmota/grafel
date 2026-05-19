package quality

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// makeDoc constructs a tiny graph.Document for diff tests. Keeping the
// helper local makes failure messages easier to read than reaching into a
// testdata file.
func makeDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "e1", Name: "User", Kind: "Model", SourceFile: "users/models.py"},
			{ID: "e2", Name: "UserListView", Kind: "View", SourceFile: "users/views.py"},
			{ID: "e3", Name: "UserListView.get", Kind: "SCOPE.Operation", SourceFile: "users/views.py"},
			{ID: "e4", Name: "User.full_label", Kind: "SCOPE.Operation", SourceFile: "users/models.py"},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "e1", ToID: "ext:models", Kind: "EXTENDS"},
			{ID: "r2", FromID: "e2", ToID: "e3", Kind: "CONTAINS"},
			{ID: "r3", FromID: "e3", ToID: "full_label", Kind: "CALLS"}, // bare-name stub
		},
	}
}

func TestEvaluate_EntityRecall(t *testing.T) {
	fix := &Fixture{
		Name: "tiny",
		ExpectedEntities: []ExpectedEntity{
			{Name: "User", Kind: "Model", SourceFile: "users/models.py", MustExist: true},
			{Name: "UserListView", Kind: "View", SourceFile: "users/views.py", MustExist: true},
			{Name: "MissingThing", Kind: "Model", MustExist: true},
			{Name: "BonusThing", Kind: "Model", MustExist: false, NiceToHave: true},
		},
	}
	rep := Evaluate(fix, makeDoc())
	if rep.EntityExpected != 3 {
		t.Fatalf("EntityExpected=%d want 3", rep.EntityExpected)
	}
	if rep.EntityFound != 2 {
		t.Fatalf("EntityFound=%d want 2", rep.EntityFound)
	}
	if rep.NiceEntityTotal != 1 || rep.NiceEntityFound != 0 {
		t.Fatalf("nice-to-have counters: got %d/%d", rep.NiceEntityFound, rep.NiceEntityTotal)
	}
	got := rep.EntityRecall()
	if got < 0.66 || got > 0.67 {
		t.Fatalf("EntityRecall=%v want ~0.667", got)
	}
}

func TestEvaluate_RelationshipRecall_BareNameAndResolved(t *testing.T) {
	fix := &Fixture{
		Name: "tiny",
		ExpectedRelationships: []ExpectedRelationship{
			// Resolved-endpoint edge.
			{FromName: "UserListView", FromKind: "View", Kind: "CONTAINS",
				ToName: "UserListView.get", ToKind: "SCOPE.Operation", MustExist: true},
			// Bare-name target (ext:models).
			{FromName: "User", FromKind: "Model", Kind: "EXTENDS",
				ToBareName: "ext:models", MustExist: true},
			// Bare-name call target.
			{FromName: "UserListView.get", FromKind: "SCOPE.Operation", Kind: "CALLS",
				ToBareName: "full_label", MustExist: true},
			// Missing edge with both endpoints present.
			{FromName: "User", FromKind: "Model", Kind: "CALLS",
				ToName: "UserListView.get", ToKind: "SCOPE.Operation", MustExist: true},
		},
	}
	rep := Evaluate(fix, makeDoc())
	if rep.RelExpected != 4 {
		t.Fatalf("RelExpected=%d want 4", rep.RelExpected)
	}
	if rep.RelFound != 3 {
		t.Fatalf("RelFound=%d want 3, results=%+v", rep.RelFound, rep.RelResults)
	}
	// The missing edge should have both endpoints resolved — the diagnostic
	// surface depends on this distinction.
	var missing *RelationshipResult
	for i := range rep.RelResults {
		if !rep.RelResults[i].Found {
			missing = &rep.RelResults[i]
		}
	}
	if missing == nil {
		t.Fatal("expected one missing edge")
	}
	if !missing.FromResolved || !missing.ToResolved {
		t.Fatalf("missing edge should have both endpoints resolved: %+v", missing)
	}
}

func TestEvaluate_ForbiddenHits(t *testing.T) {
	fix := &Fixture{
		Name: "tiny",
		ForbiddenRelationships: []ExpectedRelationship{
			// Hit — this edge IS in the doc.
			{FromName: "UserListView", FromKind: "View", Kind: "CONTAINS",
				ToName: "UserListView.get", ToKind: "SCOPE.Operation"},
			// Miss — not present.
			{FromName: "User", FromKind: "Model", Kind: "OWNS",
				ToName: "UserListView", ToKind: "View"},
		},
	}
	rep := Evaluate(fix, makeDoc())
	if got, want := len(rep.ForbiddenHits), 1; got != want {
		t.Fatalf("ForbiddenHits=%d want %d", got, want)
	}
}

func TestReport_WriteHuman_IncludesRootCauseHint(t *testing.T) {
	fix := &Fixture{
		Name: "tiny",
		ExpectedRelationships: []ExpectedRelationship{
			{FromName: "Ghost", FromKind: "Model", Kind: "CALLS",
				ToName: "User", ToKind: "Model", MustExist: true},
		},
	}
	rep := Evaluate(fix, makeDoc())
	var sb strings.Builder
	rep.WriteHuman(&sb)
	out := sb.String()
	if !strings.Contains(out, "from-entity not extracted") {
		t.Fatalf("expected root-cause diagnostic in human output, got:\n%s", out)
	}
}
