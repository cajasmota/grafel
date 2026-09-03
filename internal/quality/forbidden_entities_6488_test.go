package quality

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors/cross/ormlink"
	"github.com/cajasmota/grafel/internal/graph"
)

// #6488 arm B. `Fixture` carried ForbiddenRelationships and no entity
// analogue, so no fixture could state "this entity must NOT exist" and entity
// over-emission was structurally invisible: recall counts what was FOUND, and
// a graph that grows a near-duplicate of every entity it already has scores
// exactly the same recall as one that does not.
//
// These tests grade the mechanism in both directions. The over-fire direction
// is the one that matters most here — a forbidden row that fires on a
// legitimate entity would turn a green fixture red for no defect, and the
// axes that keep it from doing so (kind, source_file, subtype) are the whole
// content of the matcher.

// forbiddenDoc carries a deliberate near-collision set: one name under two
// kinds, one name in two files, and one name under two subtypes. Every axis a
// forbidden row can narrow on has a legitimate entity sitting on the other
// side of it, so a matcher that drops an axis fires on something real.
func forbiddenDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "e1", Name: "Role", Kind: "SCOPE.Schema", Subtype: "enum", SourceFile: "user.proto"},
			{ID: "e2", Name: "Role", Kind: "SCOPE.Operation", Subtype: "endpoint", SourceFile: "user.proto"},
			{ID: "e3", Name: "Address", Kind: "SCOPE.Schema", Subtype: "message", SourceFile: "common.proto"},
			{ID: "e4", Name: "proto_enum:Role", Kind: "SCOPE.Schema", Subtype: "enum", SourceFile: "user.proto"},
		},
	}
}

func TestForbiddenEntity_FiresOnAnExtractedEntity_6488(t *testing.T) {
	fix := &Fixture{
		Name: "tiny",
		ForbiddenEntities: []ExpectedEntity{
			{Name: "Role", Kind: "SCOPE.Schema", SourceFile: "user.proto"},
		},
	}
	rep := Evaluate(fix, forbiddenDoc())
	if got := len(rep.ForbiddenEntityHits); got != 1 {
		t.Fatalf("ForbiddenEntityHits=%d want 1", got)
	}
	h := rep.ForbiddenEntityHits[0]
	if h.MatchedID != "e1" {
		t.Fatalf("MatchedID=%q want e1", h.MatchedID)
	}
	// The diagnostic must name the OFFENDING entity, not merely echo the row:
	// a row may omit source_file or subtype, and "a forbidden entity was
	// found" sends the reader back to the graph to work out which one.
	if h.MatchedSubtype != "enum" || h.MatchedSourceFile != "user.proto" || h.MatchedName != "Role" {
		t.Fatalf("hit does not describe the offending entity: %+v", h)
	}
}

func TestForbiddenEntity_SilentWhenAbsent_6488(t *testing.T) {
	fix := &Fixture{
		Name: "tiny",
		ForbiddenEntities: []ExpectedEntity{
			{Name: "proto_message:Role", Kind: "SCOPE.Schema", SourceFile: "user.proto"},
		},
	}
	rep := Evaluate(fix, forbiddenDoc())
	if got := len(rep.ForbiddenEntityHits); got != 0 {
		t.Fatalf("ForbiddenEntityHits=%d want 0 — the vacuity trap runs the other way "+
			"too: a row naming an entity that does not exist must not fire, or every "+
			"fixture carrying one goes red for no defect: %+v", got, rep.ForbiddenEntityHits)
	}
}

// The permissive direction, and the one recall cannot cover. A matcher that
// keys on Name alone reads "Role" and finds three legitimate entities.
func TestForbiddenEntity_KindNarrows_6488(t *testing.T) {
	fix := &Fixture{
		Name: "tiny",
		ForbiddenEntities: []ExpectedEntity{
			// Role IS extracted — as SCOPE.Schema and as SCOPE.Operation.
			// Neither is a SCOPE.Service, so this row must stay silent.
			{Name: "Role", Kind: "SCOPE.Service", SourceFile: "user.proto"},
		},
	}
	rep := Evaluate(fix, forbiddenDoc())
	if got := len(rep.ForbiddenEntityHits); got != 0 {
		t.Fatalf("ForbiddenEntityHits=%d want 0 — a forbidden row keys on (kind, name), "+
			"so a same-named entity of another kind is a different entity: %+v",
			got, rep.ForbiddenEntityHits)
	}
}

func TestForbiddenEntity_SourceFileNarrows_6488(t *testing.T) {
	fix := &Fixture{
		Name: "tiny",
		ForbiddenEntities: []ExpectedEntity{
			// `Address` is legitimately extracted from common.proto. The row
			// forbids it in user.proto — the phantom-duplicate shape — and
			// must not fire on the real one.
			{Name: "Address", Kind: "SCOPE.Schema", SourceFile: "user.proto"},
		},
	}
	rep := Evaluate(fix, forbiddenDoc())
	if got := len(rep.ForbiddenEntityHits); got != 0 {
		t.Fatalf("ForbiddenEntityHits=%d want 0 — the row names user.proto and the only "+
			"Address is in common.proto: %+v", got, rep.ForbiddenEntityHits)
	}
}

// The two subtype semantics, stated as tests rather than only as prose.
//
// A row that states a subtype forbids THAT subtype and no other; a row that
// omits it forbids the (kind, name[, file]) entity whatever subtype it wears.
// Both readings are defensible in the abstract, which is exactly why they are
// pinned: the second is the one that matches `subtype`'s meaning on an
// expected row (#6488 arm A, "empty means don't care"), so a fixture author
// learns one rule.
func TestForbiddenEntity_StatedSubtypeNarrows_6488(t *testing.T) {
	fix := &Fixture{
		Name: "tiny",
		ForbiddenEntities: []ExpectedEntity{
			// Role IS extracted at this (kind, name, file) — wearing subtype
			// "enum". The row forbids the "message" stamp, so it must not
			// fire on the legitimate enum.
			{Name: "Role", Kind: "SCOPE.Schema", SourceFile: "user.proto", Subtype: "message"},
		},
	}
	rep := Evaluate(fix, forbiddenDoc())
	if got := len(rep.ForbiddenEntityHits); got != 0 {
		t.Fatalf("ForbiddenEntityHits=%d want 0 — subtype %q was stated and the extracted "+
			"entity wears %q: %+v", got, "message", "enum", rep.ForbiddenEntityHits)
	}
}

func TestForbiddenEntity_StatedSubtypeFiresOnItsOwnValue_6488(t *testing.T) {
	fix := &Fixture{
		Name: "tiny",
		ForbiddenEntities: []ExpectedEntity{
			{Name: "Role", Kind: "SCOPE.Schema", SourceFile: "user.proto", Subtype: "enum"},
		},
	}
	rep := Evaluate(fix, forbiddenDoc())
	if got := len(rep.ForbiddenEntityHits); got != 1 {
		t.Fatalf("ForbiddenEntityHits=%d want 1 — the stated subtype is the extracted one", got)
	}
	if got := rep.ForbiddenEntityHits[0].MatchedSubtype; got != "enum" {
		t.Fatalf("MatchedSubtype=%q want %q", got, "enum")
	}
}

func TestForbiddenEntity_OmittedSubtypeForbidsEverySubtype_6488(t *testing.T) {
	// Two entities share (kind, name, file) and differ only in Subtype —
	// the shape that exists because graph.EntityID does not hash Subtype.
	doc := &graph.Document{Entities: []graph.Entity{
		{ID: "a", Name: "Role", Kind: "SCOPE.Schema", Subtype: "message", SourceFile: "user.proto"},
		{ID: "b", Name: "Role", Kind: "SCOPE.Schema", Subtype: "enum", SourceFile: "user.proto"},
	}}
	fix := &Fixture{
		Name:              "tiny",
		ForbiddenEntities: []ExpectedEntity{{Name: "Role", Kind: "SCOPE.Schema", SourceFile: "user.proto"}},
	}
	rep := Evaluate(fix, doc)
	if got := len(rep.ForbiddenEntityHits); got != 1 {
		t.Fatalf("ForbiddenEntityHits=%d want 1 — an omitted subtype is \"any\", so the "+
			"row forbids the entity whatever it wears", got)
	}
}

func TestForbiddenEntity_ReportNamesTheOffender_6488(t *testing.T) {
	fix := &Fixture{
		Name:              "tiny",
		ForbiddenEntities: []ExpectedEntity{{Name: "Role", Kind: "SCOPE.Schema", SourceFile: "user.proto"}},
	}
	rep := Evaluate(fix, forbiddenDoc())

	var buf bytes.Buffer
	rep.WriteHuman(&buf)
	out := buf.String()
	for _, want := range []string{"FORBIDDEN entities", "Role", "SCOPE.Schema", "user.proto", "enum"} {
		if !strings.Contains(out, want) {
			t.Fatalf("WriteHuman output does not name %q — a bare \"forbidden entity found\" "+
				"is not actionable:\n%s", want, out)
		}
	}

	var jb bytes.Buffer
	if err := rep.WriteJSON(&jb); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	js := jb.String()
	if !strings.Contains(js, `"forbidden_entity_hits": 1`) {
		t.Fatalf("JSON report does not carry forbidden_entity_hits — the shell/python gate "+
			"reads the JSON, not the human summary:\n%s", js)
	}
	if !strings.Contains(js, `"got_subtype": "enum"`) {
		t.Fatalf("JSON report does not carry the offending subtype:\n%s", js)
	}
}

// --- loading + validation -------------------------------------------------

func writeFixtureJSON_6488(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "expected.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadFixture_ParsesForbiddenEntities_6488(t *testing.T) {
	dir := writeFixtureJSON_6488(t, `{
	  "fixture_name": "t",
	  "expected_entities": [],
	  "expected_relationships": [],
	  "asserts_no_relationships": true,
	  "forbidden_entities": [
	    { "name": "Role", "kind": "SCOPE.Schema", "source_file": "user.proto", "subtype": "message" }
	  ]
	}`)
	fix, err := LoadFixture(dir)
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}
	if len(fix.ForbiddenEntities) != 1 {
		t.Fatalf("ForbiddenEntities=%d want 1", len(fix.ForbiddenEntities))
	}
	if got := fix.ForbiddenEntities[0].Subtype; got != "message" {
		t.Fatalf("subtype=%q want %q — the key is read, not merely accepted", got, "message")
	}
}

// A forbidden row is not a recall row, and ExpectedEntity carries must_exist /
// nice_to_have because it is shared with the recall path. A forbidden row that
// sets either states a contradiction the harness would silently ignore.
func TestLoadFixture_RejectsMustExistOnAForbiddenEntity_6488(t *testing.T) {
	for _, key := range []string{`"must_exist": true`, `"nice_to_have": true`} {
		dir := writeFixtureJSON_6488(t, `{
		  "fixture_name": "t",
		  "expected_entities": [],
		  "expected_relationships": [],
		  "asserts_no_relationships": true,
		  "forbidden_entities": [ { "name": "Role", "kind": "SCOPE.Schema", `+key+` } ]
		}`)
		if _, err := LoadFixture(dir); err == nil {
			t.Fatalf("LoadFixture accepted a forbidden entity row with %s", key)
		}
	}
}

func TestLoadFixture_RejectsNamelessForbiddenEntity_6488(t *testing.T) {
	dir := writeFixtureJSON_6488(t, `{
	  "fixture_name": "t",
	  "expected_entities": [],
	  "expected_relationships": [],
	  "asserts_no_relationships": true,
	  "forbidden_entities": [ { "kind": "SCOPE.Schema" } ]
	}`)
	if _, err := LoadFixture(dir); err == nil {
		t.Fatal("LoadFixture accepted a forbidden entity row with no name — such a row " +
			"can never match and is decorative")
	}
}

func TestLoadFixture_RejectsQualifiedNamePlusSourceFileOnAForbiddenEntity_6488(t *testing.T) {
	// The same ambiguity the expected-entity loop already rejects: the
	// qualified-name path returns before source_file is ever consulted, so
	// the row LOOKS like it asserts a location and does not.
	dir := writeFixtureJSON_6488(t, `{
	  "fixture_name": "t",
	  "expected_entities": [],
	  "expected_relationships": [],
	  "asserts_no_relationships": true,
	  "forbidden_entities": [
	    { "name": "Role", "qualified_name": "pkg.Role", "kind": "SCOPE.Schema",
	      "match_by": "qualified_name", "source_file": "user.proto" }
	  ]
	}`)
	if _, err := LoadFixture(dir); err == nil {
		t.Fatal("LoadFixture accepted match_by:qualified_name + source_file on a " +
			"forbidden entity row")
	}
}

// forbidden_entities must not satisfy the #6490 must-have relationship floor,
// for the same reason forbidden_relationships does not: a forbidden row goes
// red when something APPEARS and can never go red when extraction stops.
func TestForbiddenEntitiesDoNotSatisfyTheRelationshipFloor_6488(t *testing.T) {
	dir := writeFixtureJSON_6488(t, `{
	  "fixture_name": "t",
	  "expected_entities": [],
	  "expected_relationships": [],
	  "forbidden_entities": [ { "name": "Role", "kind": "SCOPE.Schema" } ]
	}`)
	if _, err := LoadFixture(dir); err == nil {
		t.Fatal("a fixture whose only assertion is a forbidden entity was accepted " +
			"without the explicit asserts_no_relationships opt-in")
	}
}

// --- non-vacuity on the real corpus --------------------------------------

// The field exists to be USED. A forbidden_entities implementation that no
// golden fixture exercises reproduces the exact defect #6488 describes, so the
// real rows are pinned here: deleting one from expected.json fails this test
// rather than silently reducing the fixture's coverage to nothing.
func TestProtoMiniCarriesRealForbiddenEntityRows_6488(t *testing.T) {
	fix, err := LoadFixture(filepath.Join("golden", "proto-mini"))
	if err != nil {
		t.Fatalf("LoadFixture(proto-mini): %v", err)
	}
	var withSubtype, withoutSubtype bool
	for _, fe := range fix.ForbiddenEntities {
		switch {
		case fe.Name == "Role" && fe.Kind == "SCOPE.Schema" &&
			fe.SourceFile == "user.proto" && fe.Subtype == "message":
			withSubtype = true
		case fe.Name == "proto_message:Role" && fe.Kind == "SCOPE.Schema" &&
			fe.SourceFile == "user.proto" && fe.Subtype == "":
			withoutSubtype = true
		}
	}
	if !withSubtype {
		t.Error("proto-mini lost its subtype-bearing forbidden entity row " +
			"(Role/SCOPE.Schema/user.proto/message) — the enum-stamped-as-a-message " +
			"defect it grades is invisible to every other assertion in the corpus: " +
			"Subtype is not hashed into graph.EntityID, so the flip moves no id, " +
			"breaks no edge, and leaves entity+relationship recall at 100%")
	}
	if !withoutSubtype {
		t.Error("proto-mini lost its subtype-free forbidden entity row " +
			"(proto_message:Role/SCOPE.Schema/user.proto) — the near-duplicate " +
			"framework family it bounds could grow without any fixture noticing")
	}
}

// --- review follow-ups (#6488 arm B) --------------------------------------

// The THIRD decorative shape, and the one that was inside the guard against
// decorative rows. resolveEntity keys on byKindName[Kind+"\x00"+Name], so a
// row with a name and no kind searches for an entity whose Kind is the empty
// string. It loads clean, it reads as an assertion, and it can never fire.
func TestLoadFixture_RejectsKindlessForbiddenEntity_6488(t *testing.T) {
	dir := writeFixtureJSON_6488(t, `{
	  "fixture_name": "t",
	  "expected_entities": [],
	  "expected_relationships": [],
	  "asserts_no_relationships": true,
	  "forbidden_entities": [ { "name": "Role" } ]
	}`)
	if _, err := LoadFixture(dir); err == nil {
		t.Fatal("LoadFixture accepted a forbidden entity row with no kind — the " +
			"(kind, name) lookup can never match it, so the row is decorative")
	}
}

// The exemption, with a positive control so the rejection above cannot be
// widened into "kind is always required". match_by:qualified_name resolves
// through byQName and legitimately never consults Kind.
func TestForbiddenEntity_QualifiedNameRowNeedsNoKind_6488(t *testing.T) {
	dir := writeFixtureJSON_6488(t, `{
	  "fixture_name": "t",
	  "expected_entities": [],
	  "expected_relationships": [],
	  "asserts_no_relationships": true,
	  "forbidden_entities": [
	    { "name": "Role", "qualified_name": "pkg.Role", "match_by": "qualified_name" }
	  ]
	}`)
	fix, err := LoadFixture(dir)
	if err != nil {
		t.Fatalf("LoadFixture rejected a kind-less qualified_name row: %v", err)
	}
	doc := &graph.Document{Entities: []graph.Entity{
		{ID: "q1", Name: "Role", QualifiedName: "pkg.Role", Kind: "SCOPE.Schema",
			Subtype: "enum", SourceFile: "user.proto"},
	}}
	rep := Evaluate(fix, doc)
	if n := len(rep.ForbiddenEntityHits); n != 1 {
		t.Fatalf("ForbiddenEntityHits=%d want 1 — the exemption is only defensible "+
			"if the exempted row can actually fire", n)
	}
}

// A forbidden row whose axes are IDENTICAL to a must_exist row is a
// contradiction the harness would otherwise honour twice: the fixture scores
// 1/1 recall and goes red in the same run. The load error already claims "a
// row cannot both be required and be forbidden"; this is the case that
// sentence describes.
func TestLoadFixture_RejectsForbiddenRowDuplicatingAnExpectedRow_6488(t *testing.T) {
	dir := writeFixtureJSON_6488(t, `{
	  "fixture_name": "t",
	  "expected_entities": [
	    { "name": "Role", "kind": "SCOPE.Schema", "source_file": "user.proto", "must_exist": true }
	  ],
	  "expected_relationships": [],
	  "asserts_no_relationships": true,
	  "forbidden_entities": [
	    { "name": "Role", "kind": "SCOPE.Schema", "source_file": "user.proto" }
	  ]
	}`)
	if _, err := LoadFixture(dir); err == nil {
		t.Fatal("LoadFixture accepted a forbidden row identical to a must_exist row")
	}
}

// The negative control for the check above, and it is proto-mini's own shape:
// FE1 forbids Role/SCOPE.Schema/user.proto with subtype "message" while an
// expected row requires the same (kind, name, file) with NO subtype. That is
// not a contradiction — it is the whole point of the subtype axis — so a
// duplicate check keyed on anything less than every matching axis would
// reject the corpus's first real row.
func TestLoadFixture_AllowsAForbiddenRowThatDiffersOnlyBySubtype_6488(t *testing.T) {
	dir := writeFixtureJSON_6488(t, `{
	  "fixture_name": "t",
	  "expected_entities": [
	    { "name": "Role", "kind": "SCOPE.Schema", "source_file": "user.proto", "must_exist": true }
	  ],
	  "expected_relationships": [],
	  "asserts_no_relationships": true,
	  "forbidden_entities": [
	    { "name": "Role", "kind": "SCOPE.Schema", "source_file": "user.proto", "subtype": "message" }
	  ]
	}`)
	if _, err := LoadFixture(dir); err != nil {
		t.Fatalf("LoadFixture rejected the subtype-narrowed pair proto-mini ships: %v", err)
	}
}

// Every firing row is reported, not just the first. The gate trips either
// way, so this is not a correctness hole — it is the difference between an
// operator fixing over-emission seeing the whole list and seeing one row per
// run.
func TestForbiddenEntity_ReportsEveryFiringRow_6488(t *testing.T) {
	fix := &Fixture{
		Name: "tiny",
		ForbiddenEntities: []ExpectedEntity{
			{Name: "Role", Kind: "SCOPE.Schema", SourceFile: "user.proto"},
			{Name: "Address", Kind: "SCOPE.Schema", SourceFile: "common.proto"},
			{Name: "proto_enum:Role", Kind: "SCOPE.Schema", SourceFile: "user.proto"},
		},
	}
	rep := Evaluate(fix, forbiddenDoc())
	if n := len(rep.ForbiddenEntityHits); n != 3 {
		t.Fatalf("ForbiddenEntityHits=%d want 3 — a loop that stops at the first hit "+
			"hides the rest of the over-emission: %+v", n, rep.ForbiddenEntityHits)
	}
	var buf bytes.Buffer
	rep.WriteHuman(&buf)
	for _, want := range []string{"Role", "Address", "proto_enum:Role"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("WriteHuman does not name the offender %q:\n%s", want, buf.String())
		}
	}
}

// forbidden_entity_hits must be serialised even when it is zero. The claim
// "absent means this report predates the field" is only true if a live report
// always carries the key — omitempty would make a clean run indistinguishable
// from a run by a binary that cannot count entity hits at all, and
// ratchet.py's .get(key, 0) reads both as 0.
func TestForbiddenEntityHitsIsSerialisedWhenZero_6488(t *testing.T) {
	rep := Evaluate(&Fixture{Name: "tiny"}, forbiddenDoc())
	var buf bytes.Buffer
	if err := rep.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if !strings.Contains(buf.String(), `"forbidden_entity_hits": 0`) {
		t.Fatalf("a clean report omits forbidden_entity_hits, so a consumer cannot "+
			"tell a zero from a report written before the field existed:\n%s", buf.String())
	}
}

// The JSON is the machine-readable path — ratchet.py and run.sh read it and
// the human summary is read by nobody in CI — so it must not be the weaker
// diagnostic. It carries the offending entity's kind and id, which the row
// may not state at all.
func TestForbiddenEntityJSONCarriesTheOffendersKindAndID_6488(t *testing.T) {
	fix := &Fixture{
		Name: "tiny",
		ForbiddenEntities: []ExpectedEntity{
			{Name: "Role", QualifiedName: "pkg.Role", MatchBy: "qualified_name"},
		},
	}
	doc := &graph.Document{Entities: []graph.Entity{
		{ID: "q1", Name: "Role", QualifiedName: "pkg.Role", Kind: "SCOPE.Schema",
			Subtype: "enum", SourceFile: "user.proto"},
	}}
	rep := Evaluate(fix, doc)
	if n := len(rep.ForbiddenEntityHits); n != 1 {
		t.Fatalf("ForbiddenEntityHits=%d want 1", n)
	}
	var buf bytes.Buffer
	if err := rep.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	// The row states no kind and no file at all, so every one of these can
	// only have come from the extracted entity.
	for _, want := range []string{
		`"got_kind": "SCOPE.Schema"`,
		`"got_id": "q1"`,
		`"got_source_file": "user.proto"`,
		`"got_subtype": "enum"`,
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("JSON forbidden entry is missing %s — it is the diagnostic CI "+
				"reads:\n%s", want, buf.String())
		}
	}
}

// A documented blind spot, pinned so it stays a decision rather than becoming
// folklore. resolveEntity skips placeholder anchors on every path (#6277), and
// forbidden rows reuse it, so an ormlink sentinel cannot be forbidden. Anchor
// over-emission is outside the fence this arm builds. If this test ever goes
// red the exclusion changed, which is exactly when someone should re-read the
// #6277 reasoning rather than discover it from a fixture going red.
func TestForbiddenEntity_CannotForbidAPlaceholderAnchor_6488(t *testing.T) {
	doc := &graph.Document{Entities: []graph.Entity{
		{ID: "a1", Name: "Role", Kind: "SCOPE.Schema",
			Subtype: ormlink.SubtypeSentinel, SourceFile: "user.proto"},
	}}
	fix := &Fixture{
		Name: "tiny",
		ForbiddenEntities: []ExpectedEntity{
			{Name: "Role", Kind: "SCOPE.Schema", SourceFile: "user.proto",
				Subtype: ormlink.SubtypeSentinel},
		},
	}
	if n := len(Evaluate(fix, doc).ForbiddenEntityHits); n != 0 {
		t.Fatalf("ForbiddenEntityHits=%d want 0 — placeholder anchors are excluded "+
			"from resolveEntity on every path, so this is the documented blind spot, "+
			"not a hit; if it now fires, update the doc on Fixture.ForbiddenEntities", n)
	}
}
