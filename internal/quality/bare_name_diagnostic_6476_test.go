package quality

import (
	"bytes"
	"encoding/json"
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

// ─────────────────────────────────────────────────────────────────────────────
// Round 2 (#6476). The arms above pin the message for the headline shape; these
// pin the parts of the mechanism that a one-token edit could break silently.
// Each names the mutant it kills.
// ─────────────────────────────────────────────────────────────────────────────

// relResult returns the single RelResult of a one-row fixture.
func relResult(t *testing.T, fix *Fixture, doc *graph.Document) RelationshipResult {
	t.Helper()
	rep := Evaluate(fix, doc)
	if len(rep.RelResults) != 1 {
		t.Fatalf("want exactly one relationship result, got %d", len(rep.RelResults))
	}
	return rep.RelResults[0]
}

// Mutant killed: move the ToBareNameIsEntity arm ahead of the !FromResolved
// arms (its pre-review position).
//
// The fix must not reproduce the failure mode it exists to remove. A row whose
// FROM endpoint was never extracted is not repaired by "use to_name + to_kind"
// — that only touches the TO side — so the missing endpoint is what the reader
// must be told about first.
func TestBareNameRowWithUnresolvedFromReportsTheFromSide_6476(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			// "App" is deliberately absent: the from endpoint did not extract.
			{ID: "sha-vapor", Name: "Vapor", Kind: "SCOPE.External", SourceFile: "Package.swift"},
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

	if !strings.Contains(out, "root cause: from-entity not extracted") {
		t.Fatalf("a missing FROM endpoint outranks the bare-name advice, which cannot repair it:\n%s", out)
	}
	if strings.Contains(out, "to_bare_name") {
		t.Fatalf("bare-name advice would send the reader to the wrong side of this row:\n%s", out)
	}
	// The classification itself is still true of the target — only the
	// report's priority changed. Serialised consumers still see it.
	if rr := relResult(t, fix, doc); !rr.ToBareNameIsEntity {
		t.Fatalf("the target IS an entity name; the flag describes the target, the arm order describes the advice")
	}
}

// Mutant killed: delete the `!bareIsEntityID` term from bareIsEntityName.
//
// TestBareNameEqualToAnEntityIDStaysMatchable_6476 above uses "ext:models",
// which is no entity's Name — so it passes with or without the guard. The guard
// only does work when the SAME string is one entity's ID and another's Name.
func TestBareNameThatIsOneEntitysIDAndAnothersNameStaysMatchable_6476(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			// ID "models" — the literal relByTriple ToID path can reach this.
			{ID: "models", Name: "somethingelse", Kind: "SCOPE.External", SourceFile: "models.py"},
			// …and "models" is ALSO a name, which is what would trip the flag.
			{ID: "sha-x", Name: "models", Kind: "Module", SourceFile: "models.py"},
			{ID: "sha-user", Name: "User", Kind: "Model", SourceFile: "models.py"},
		},
	}
	fix := &Fixture{
		Name: "django-ish",
		ExpectedRelationships: []ExpectedRelationship{
			{FromName: "User", FromKind: "Model", Kind: "EXTENDS",
				ToBareName: "models", MustExist: true},
		},
	}
	out := humanReport(t, fix, doc)

	if strings.Contains(out, "to_bare_name") {
		t.Fatalf("ID-equality makes this row matchable; flagging it is the widening this fix must not do:\n%s", out)
	}
	if !strings.Contains(out, "both endpoints exist") {
		t.Fatalf("a matchable row that missed is the extractor's miss:\n%s", out)
	}
	if rr := relResult(t, fix, doc); rr.ToBareNameIsEntity {
		t.Fatalf("ToBareNameIsEntity must stay false when the bare string is some entity's ID")
	}
}

// Mutant killed: drop the bareIsEntityName term from toResolved
// (`toResolved := len(toCands) > 0 || bareIsEntityID`).
//
// ToResolved is the ONE JSON-visible field this change moves. Asserting only
// the human message leaves it free: the diagnostic arm keys off
// ToBareNameIsEntity, so the sentence is identical either way while
// to_resolved silently reverts to reporting the target as unextracted.
func TestFlaggedBareNameRowSerialisesBothResolvedAndTheFlag_6476(t *testing.T) {
	fix := &Fixture{
		Name: "swift-ish",
		ExpectedRelationships: []ExpectedRelationship{
			{FromName: "App", FromKind: "SCOPE.Component", Kind: "DEPENDS_ON",
				ToBareName: "Vapor", MustExist: true},
		},
	}
	rr := relResult(t, fix, bareNameDoc())
	if !rr.ToResolved {
		t.Fatalf("the bare target DID resolve to an extracted entity; ToResolved must say so")
	}
	if !rr.FromResolved {
		t.Fatalf("the from endpoint extracted; FromResolved must say so")
	}
	if !rr.ToBareNameIsEntity {
		t.Fatalf("the row is the #6441 fixture defect and must be flagged")
	}

	// …and the flag must reach the machine-readable surface, or a consumer
	// reading to_resolved:true still blames the extractor.
	var buf bytes.Buffer
	if err := Evaluate(fix, bareNameDoc()).WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var got struct {
		MissingRelationships []struct {
			ToResolved         bool `json:"to_resolved"`
			ToBareNameIsEntity bool `json:"to_bare_name_is_entity"`
		} `json:"missing_relationships"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON report: %v\n%s", err, buf.String())
	}
	if len(got.MissingRelationships) != 1 {
		t.Fatalf("want one missing relationship in JSON, got %d:\n%s",
			len(got.MissingRelationships), buf.String())
	}
	if !got.MissingRelationships[0].ToResolved {
		t.Fatalf("to_resolved must be true for a bare target that resolved:\n%s", buf.String())
	}
	if !got.MissingRelationships[0].ToBareNameIsEntity {
		t.Fatalf("to_bare_name_is_entity must be serialised, or to_resolved:true reads as an extractor miss:\n%s",
			buf.String())
	}
}

// Mutant killed: also key nameIsEntity by QualifiedName.
//
// The set is deliberately Name-only. A QualifiedName collision does not mean
// the bare string named the entity, and widening the set widens the advice onto
// rows it does not describe.
func TestBareNameMatchingOnlyAQualifiedNameIsNotFlagged_6476(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "sha-app", Name: "App", Kind: "SCOPE.Component", SourceFile: "Package.swift"},
			{ID: "sha-v", Name: "Client", QualifiedName: "Vapor", Kind: "SCOPE.External",
				SourceFile: "Package.swift"},
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
		t.Fatalf("a QualifiedName collision is not a Name match:\n%s", out)
	}
	if !strings.Contains(out, "root cause: to-entity not extracted") {
		t.Fatalf("no entity is NAMED Vapor here:\n%s", out)
	}
}

// Mutant killed: set ToBareNameIsEntity on the literal-ToID match path.
//
// The field's doc says it is false on every path that MATCHED — a row that hit
// is matchable by definition. Nothing in the human report can see this (only
// misses print), so the invariant needs a direct assertion.
func TestMatchedBareNameRowNeverCarriesTheFlag_6476(t *testing.T) {
	doc := bareNameDoc()
	// A stub edge whose ToID is the bare string itself: the row matches.
	doc.Relationships = []graph.Relationship{
		{ID: "r1", FromID: "sha-app", ToID: "Vapor", Kind: "DEPENDS_ON"},
	}
	fix := &Fixture{
		Name: "swift-ish",
		ExpectedRelationships: []ExpectedRelationship{
			{FromName: "App", FromKind: "SCOPE.Component", Kind: "DEPENDS_ON",
				ToBareName: "Vapor", MustExist: true},
		},
	}
	rr := relResult(t, fix, doc)
	if !rr.Found {
		t.Fatalf("the stub edge should match the bare-name row")
	}
	if rr.ToBareNameIsEntity {
		t.Fatalf("a row that MATCHED is matchable by definition; the flag must be false on every hit path")
	}
}

// Finding 4. A class-hierarchy shadow (provenance=INFERRED_FROM_CLASS_HIERARCHY,
// e.g. java-quartz-mini's synthesised SCOPE.Component Job) is a stand-in for a
// declaration that has no source in the fixture. Flagging a row that targets one
// would advise binding the expectation to the stand-in — manufacturing exactly
// the #6277 false positive.
func TestBareNameMatchingOnlyAHierarchyShadowIsNotFlagged_6476(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "sha-job", Name: "SendEmailJob", Kind: "SCOPE.Component",
				SourceFile: "jobs/SendEmailJob.java"},
			graph.Entity{ID: "sha-shadow", Name: "Job", Kind: "SCOPE.Component",
				Subtype: "interface", SourceFile: "jobs/SendEmailJob.java"}.
				WithProperties(map[string]string{"provenance": "INFERRED_FROM_CLASS_HIERARCHY"}),
		},
	}
	fix := &Fixture{
		Name: "java-quartz-ish",
		ExpectedRelationships: []ExpectedRelationship{
			{FromName: "SendEmailJob", FromKind: "SCOPE.Component", Kind: "IMPLEMENTS",
				ToBareName: "Job", MustExist: true},
		},
	}
	out := humanReport(t, fix, doc)

	if strings.Contains(out, "to_bare_name") {
		t.Fatalf("advising a fixture to bind to a synthesised stand-in is the #6277 false positive:\n%s", out)
	}
	if !strings.Contains(out, "root cause: to-entity not extracted") {
		t.Fatalf("a shadow is not an extracted declaration:\n%s", out)
	}
}

// Finding 5, degenerate input. A whitespace-only bare name must classify as
// nothing, even when some entity carries an empty Name.
func TestBlankBareNameIsNotFlagged_6476(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "sha-app", Name: "App", Kind: "SCOPE.Component", SourceFile: "Package.swift"},
			{ID: "sha-blank", Name: "", Kind: "SCOPE.External", SourceFile: "Package.swift"},
		},
	}
	fix := &Fixture{
		Name: "swift-ish",
		ExpectedRelationships: []ExpectedRelationship{
			{FromName: "App", FromKind: "SCOPE.Component", Kind: "DEPENDS_ON",
				ToBareName: "   ", MustExist: true},
		},
	}
	if rr := relResult(t, fix, doc); rr.ToBareNameIsEntity {
		t.Fatalf("a blank bare name matched an entity's blank Name; neither set may hold the empty key")
	}
}

// Finding 5, the overclaim. The relByKindFrom fallback compares with
// strings.EqualFold, so a stub emitted as ToID "vapor" WOULD satisfy a
// to_bare_name of "Vapor". Such a row is matchable and must not be told it can
// "never" hit.
func TestBareNameDifferingOnlyInCaseFromAnEntityIDStaysMatchable_6476(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "sha-app", Name: "App", Kind: "SCOPE.Component", SourceFile: "Package.swift"},
			{ID: "vapor", Name: "Vapor", Kind: "SCOPE.External", SourceFile: "Package.swift"},
		},
	}
	fix := &Fixture{
		Name: "swift-ish",
		ExpectedRelationships: []ExpectedRelationship{
			{FromName: "App", FromKind: "SCOPE.Component", Kind: "DEPENDS_ON",
				ToBareName: "Vapor", MustExist: true},
		},
	}
	if rr := relResult(t, fix, doc); rr.ToBareNameIsEntity {
		t.Fatalf("EqualFold makes ToID %q reachable from bare name %q; the row is matchable", "vapor", "Vapor")
	}
}
