package quality

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors/cross/ormlink"
	"github.com/cajasmota/grafel/internal/graph"
)

// #6476. `toResolved := len(toCands) > 0 || er.ToBareName != ""` reported EVERY
// to_bare_name row as resolved, so report.go's diagnostic switch fell through to
// its default arm and printed
//
//	(both endpoints exist; edge not emitted)
//
// for a row that cannot match anything. That message names the extractor as the
// culprit. For a bare-name row whose target IS an extracted entity the truth is
// the opposite: toCands is only populated when ToName != "", so the sole match
// path left is literal ToID equality, and a resolved target carries a hashed
// entity ID rather than the bare string. The row is unsatisfiable by
// construction — the #6441 fixture defect — and the reader was being sent to
// the wrong file.
//
// These tests drive the real report path (Evaluate -> WriteHuman) rather than
// poking ToResolved, because the defect is what the report SAYS. A
// predicate-level assertion would have passed against the old `||` too.

// bareNameDoc is a miniature of swift-package-mini: the bare-name target is a
// declared, extracted entity, and no edge to it was emitted.
func bareNameDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "sha-app", Name: "App", Kind: "SCOPE.Component", SourceFile: "Package.swift"},
			{ID: "sha-vapor", Name: "Vapor", Kind: "SCOPE.External", SourceFile: "Package.swift"},
		},
		Relationships: []graph.Relationship{},
	}
}

func humanReport(t *testing.T, fix *Fixture, doc *graph.Document) string {
	t.Helper()
	var buf bytes.Buffer
	Evaluate(fix, doc).WriteHuman(&buf)
	return buf.String()
}

func TestBareNameRowWhoseTargetResolvesIsNotReportedAsAnExtractorMiss_6476(t *testing.T) {
	fix := &Fixture{
		Name: "swift-ish",
		ExpectedRelationships: []ExpectedRelationship{
			{FromName: "App", FromKind: "SCOPE.Component", Kind: "DEPENDS_ON",
				ToBareName: "Vapor", MustExist: true},
		},
	}
	out := humanReport(t, fix, bareNameDoc())

	if strings.Contains(out, "both endpoints exist") {
		t.Fatalf("bare-name row whose target resolves must NOT be reported as an extractor miss:\n%s", out)
	}
	if !strings.Contains(out, "to_bare_name") {
		t.Fatalf("diagnostic should name to_bare_name as the root cause:\n%s", out)
	}
	if !strings.Contains(out, "Vapor") {
		t.Fatalf("diagnostic should quote the offending bare name:\n%s", out)
	}
	// The row is still a miss — this fix changes the message, never the score.
	rep := Evaluate(fix, bareNameDoc())
	if rep.RelFound != 0 || rep.RelExpected != 1 {
		t.Fatalf("recall must not move: found=%d expected=%d", rep.RelFound, rep.RelExpected)
	}
}

func TestBareNameRowMatchingNoEntityKeepsTheExtractorDiagnostic_6476(t *testing.T) {
	// The legitimate shape: the bare target names nothing in the graph, so the
	// to-endpoint genuinely was not extracted. The pre-existing message is
	// correct here and must survive — this is the widening guard.
	fix := &Fixture{
		Name: "swift-ish",
		ExpectedRelationships: []ExpectedRelationship{
			{FromName: "App", FromKind: "SCOPE.Component", Kind: "DEPENDS_ON",
				ToBareName: "NotAnEntityAnywhere", MustExist: true},
		},
	}
	out := humanReport(t, fix, bareNameDoc())

	if !strings.Contains(out, "root cause: to-entity not extracted") {
		t.Fatalf("unresolvable bare name should report the to-entity as unextracted:\n%s", out)
	}
	if strings.Contains(out, "to_bare_name") {
		t.Fatalf("a bare name matching nothing is not the #6441 fixture defect:\n%s", out)
	}
}

func TestBareNameEqualToAnEntityIDStaysMatchable_6476(t *testing.T) {
	// A bare name that equals an entity's ID (e.g. `ext:models`) CAN match, via
	// the literal relByTriple ToID path. That row is not the #6441 defect: when
	// it misses, the edge really was not emitted. Flagging it would be the
	// widening mutant of this fix.
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "sha-user", Name: "User", Kind: "Model", SourceFile: "models.py"},
			{ID: "ext:models", Name: "models", Kind: "SCOPE.External", SourceFile: "models.py"},
		},
	}
	fix := &Fixture{
		Name: "django-ish",
		ExpectedRelationships: []ExpectedRelationship{
			{FromName: "User", FromKind: "Model", Kind: "EXTENDS",
				ToBareName: "ext:models", MustExist: true},
		},
	}
	out := humanReport(t, fix, doc)

	if !strings.Contains(out, "both endpoints exist") {
		t.Fatalf("a bare name equal to an entity ID is matchable; the miss is the extractor's:\n%s", out)
	}
}

func TestBareNameMatchingOnlyAPlaceholderAnchorIsNotFlagged_6476(t *testing.T) {
	// Placeholder anchors are not declarations (#6277). A bare name that only
	// collides with one has not resolved to anything a fixture author declared.
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "sha-app", Name: "App", Kind: "SCOPE.Component", SourceFile: "Package.swift"},
			{ID: "sha-anchor", Name: "Vapor", Kind: "SCOPE.External",
				Subtype: ormlink.SubtypeSentinel, SourceFile: "Package.swift"},
		},
	}
	fix := &Fixture{
		Name: "swift-ish",
		ExpectedRelationships: []ExpectedRelationship{
			{FromName: "App", FromKind: "SCOPE.Component", Kind: "DEPENDS_ON",
				ToBareName: "Vapor", MustExist: true},
		},
	}
	out := humanReport(t, fix, doc)

	if strings.Contains(out, "to_bare_name") {
		t.Fatalf("a placeholder anchor is not a declared entity:\n%s", out)
	}
	if !strings.Contains(out, "root cause: to-entity not extracted") {
		t.Fatalf("anchor-only match should read as unextracted:\n%s", out)
	}
}
