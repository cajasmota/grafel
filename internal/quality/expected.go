// Package quality implements the extraction-quality benchmark framework.
//
// Where bug-rate (internal/resolve) measures CLASSIFICATION quality — given
// an extracted edge, is its resolver Disposition correct — the quality
// package measures EXTRACTION quality: did we find every entity + edge that
// SHOULD have been extracted, and did our targets bind to the right thing?
//
// Each fixture lives under internal/quality/golden/<name>/ with:
//
//	src/             — small hand-curated source tree
//	expected.json    — hand-verified expected entities + relationships
//
// The harness loads expected.json, runs the production indexer over src/,
// and emits recall / forbidden-hit / target-accuracy metrics.
//
// This is intentionally orthogonal to bug-rate. A repo can score
// bug_rate=0% while still missing half of the real edges — bug-rate only
// scores what was extracted, not what was missed.
package quality

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Fixture is the in-memory representation of an expected.json file.
//
// We deliberately keep this loose — `must_exist` rather than enumerating a
// closed world — because the indexer also legitimately extracts framework-
// detected entities (Routes, Middlewares, etc.) that we don't want to
// over-specify. Recall is therefore "did the must_exist things show up",
// not "is the extracted set exactly this".
type Fixture struct {
	Name                   string                 `json:"fixture_name"`
	Language               string                 `json:"language"`
	Description            string                 `json:"description,omitempty"`
	ExpectedEntities       []ExpectedEntity       `json:"expected_entities"`
	ExpectedRelationships  []ExpectedRelationship `json:"expected_relationships"`
	ForbiddenRelationships []ExpectedRelationship `json:"forbidden_relationships,omitempty"`
	// ForbiddenEntities is the entity analogue of ForbiddenRelationships
	// (#6488 arm B): each row names an entity that must NOT be in the graph,
	// and a match is a hard quality regression exactly as a forbidden edge is.
	//
	// It exists because recall is structurally incapable of seeing entity
	// OVER-emission. Recall counts what was found against what was asked for,
	// so a graph that grows a near-duplicate of every entity it already has
	// scores the same 100% as one that does not — the same one-directional
	// blindness forbidden_relationships was added to close on the edge side.
	//
	// Rows reuse ExpectedEntity so a fixture author learns one shape and one
	// matcher: name/kind, optionally narrowed by source_file, qualified_name
	// or subtype, resolved by the same resolveEntity the recall path uses.
	// `must_exist` and `nice_to_have` are recall-only concepts and are
	// rejected at load rather than silently ignored.
	//
	// Subtype semantics, stated because both readings are defensible: a row
	// that STATES a subtype forbids that subtype and no other, and a row that
	// omits it forbids the named entity whatever subtype it wears. That is the
	// same "empty means don't care" rule an expected row has carried since
	// #6488 arm A, read in the forbidding direction.
	//
	// KNOWN BLIND SPOT — placeholder anchors cannot be forbidden. resolveEntity
	// skips isPlaceholderAnchor candidates on every path (#6277), so a row
	// naming an ormlink sentinel resolves nothing and reports zero hits even
	// when the sentinel is in the graph. Anchor over-emission is therefore
	// OUTSIDE the fence this field builds. That is inherited rather than
	// chosen: reusing one matcher for asserting and forbidding is what keeps
	// "this entity" from meaning two things, and the alternative — a forbidding
	// matcher that sees anchors while the asserting one does not — would let a
	// fixture forbid an entity it could never have expected. Recorded here
	// because it is invisible in a green report, and pinned by
	// TestForbiddenEntity_CannotForbidAPlaceholderAnchor_6488, so a future
	// change to the exclusion is a red test rather than a discovery.
	//
	// Counting: a hit is a ROW THAT FIRED, not an offending entity. One row
	// satisfied by two identical offenders is one hit — resolveEntity resolves
	// one candidate rather than enumerating all of them, which is exactly what
	// keeps it the same matcher the recall path uses.
	ForbiddenEntities []ExpectedEntity `json:"forbidden_entities,omitempty"`
	// AssertsNoRelationships is the explicit, named opt-in that exempts a
	// fixture from the must-have relationship floor (#6490 arm A). It is a
	// positive claim on purpose: only `true` exempts, so neither the absent
	// field nor an empty `expected_relationships` array can ever be read as
	// consent — that reading is the defect this field exists to close.
	AssertsNoRelationships bool `json:"asserts_no_relationships,omitempty"`
}

// ExpectedEntity is a hand-curated assertion about what the indexer SHOULD
// produce. Match() decides whether an extracted graph.Entity satisfies this
// expectation.
//
// MatchBy chooses the field used to identify the entity inside graph.json:
//
//	"name"           — match by Entity.Name (case-sensitive, exact)
//	"qualified_name" — match by Entity.QualifiedName
//	"source_file"    — match by SourceFile + Name (preferred for fixtures
//	                   that have name collisions, e.g. two `Meta` classes)
//
// If MatchBy is empty we default to "name+kind" which is sufficient for
// most small fixtures.
//
// Kind is matched exactly (e.g. "SCOPE.Component", "SCOPE.Operation").
type ExpectedEntity struct {
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name,omitempty"`
	Kind          string `json:"kind"`
	SourceFile    string `json:"source_file,omitempty"`
	MatchBy       string `json:"match_by,omitempty"`
	// Subtype, when non-empty, additionally requires the extracted entity's
	// Subtype to equal this string exactly. Empty means "don't care", which is
	// what every pre-#6488 row means and must go on meaning.
	Subtype   string `json:"subtype,omitempty"`
	MustExist bool   `json:"must_exist"`
	// NiceToHave entities are evaluated but NOT counted as a recall miss
	// when absent. Useful for capabilities we want to track (e.g. signal
	// receivers, custom managers) without holding the fixture hostage to
	// them.
	NiceToHave bool `json:"nice_to_have,omitempty"`
	// Note is free-form prose for the fixture author. Ignored by the harness.
	Note string `json:"note,omitempty"`
}

// ExpectedRelationship is a hand-curated edge assertion. The matcher reads
// FromName / ToName because expected.json is written BEFORE entity IDs
// (which are SHA-truncated content hashes) are known.
//
// To match an extracted relationship, the harness:
//  1. Resolves FromName + FromKind to an Entity ID by looking it up in the
//     extracted graph (Entity.Name match, optionally narrowed by FromFile).
//  2. Same for ToName + ToKind.
//  3. Checks whether the extracted Relationships slice contains an edge
//     with (FromID, ToID, Kind).
//
// When the ToID is expected to be a bare-name external (e.g. CALLS
// django.db.models.Model.objects.filter), the harness also accepts an
// edge whose ToID matches ToBareName directly (no entity lookup required).
type ExpectedRelationship struct {
	FromName string `json:"from_name"`
	FromKind string `json:"from_kind,omitempty"`
	FromFile string `json:"from_file,omitempty"`
	// FromBareName is the FROM-side analogue of ToBareName (#6488 arm C): it
	// matches an edge whose FromID is a RAW STRING rather than an entity ID.
	//
	// It exists because such an edge was unassertable by construction, not
	// merely unasserted. Every match path in resolveExpectedEdge used to be
	// nested inside the loop over the from-entity candidates, so a row whose
	// carrier is not an entity had no route to a match at all. The population
	// is real and is the cross-language IMPORTS convention of #120: erlang, nim
	// and groovy anchor include/import edges on the file PATH and emit no
	// extractor.FileEntity carrying it, so nothing in those graphs can be named
	// as the from endpoint (the graph-side repair is #6815).
	//
	// The comparison is exact after trimming the fixture's value — a stray
	// space in expected.json is an authoring slip, a case difference is a
	// different path. A blank (or whitespace-only) value is no candidate at
	// all, so the overwhelming majority of rows, which set this field to
	// nothing, cannot match an edge emitted with an empty FromID.
	//
	// Setting it alongside from_name is legal and means "either": both are
	// candidates, exactly as the row's to side already admits a resolved target
	// and a bare one together.
	//
	// It does NOT make from_resolved true. A row can match through this field
	// while its from endpoint resolves to nothing, and that combination is the
	// finding such a row records — the edge hangs off a carrier that does not
	// exist. See TestMatchedFromBareNameRowStillReportsFromResolvedFalse_6488.
	FromBareName string `json:"from_bare_name,omitempty"`
	Kind         string `json:"kind"`
	ToName       string `json:"to_name,omitempty"`
	ToKind       string `json:"to_kind,omitempty"`
	ToFile       string `json:"to_file,omitempty"`
	ToBareName   string `json:"to_bare_name,omitempty"`
	MustExist    bool   `json:"must_exist"`
	NiceToHave   bool   `json:"nice_to_have,omitempty"`
	Note         string `json:"note,omitempty"`
}

// LoadFixture reads expected.json from the given fixture directory.
// The fixture's source tree is at <dir>/src/, which the caller passes to
// the indexer separately.
func LoadFixture(dir string) (*Fixture, error) {
	p := filepath.Join(dir, "expected.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	var f Fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	if f.Name == "" {
		return nil, fmt.Errorf("%s: fixture_name is required", p)
	}
	// #6490: a fixture that OMITS `expected_relationships` is not the same
	// claim as one asserting zero, but json.Unmarshal makes them identical —
	// both leave the slice nil, and the grader scores both as a pass. Seven of
	// the twenty-six golden fixtures were in the absent shape, so the golden
	// gate reported "relationships: 0/0 — pass" for languages whose edge
	// extraction it was in fact grading with nothing at all.
	//
	// So the key must be DECLARED. An empty array stays legal on purpose: it
	// is the visible "we assert nothing here" marker, which is a statement a
	// reviewer can see and challenge, unlike an absent key. `null` is rejected
	// with absence because it decodes to the same nil slice — declaring the
	// key while saying nothing with it is the absent shape in disguise.
	//
	// `f.ExpectedRelationships == nil` would in fact separate the two on its
	// own — encoding/json yields a non-nil empty slice for `[]` — so this
	// re-read is not the only way to tell them apart. It is used because it
	// names the key it is checking, which a nil test does not, and because it
	// keeps working if the field ever gains a non-slice type or a custom
	// UnmarshalJSON.
	var declared map[string]json.RawMessage
	if err := json.Unmarshal(raw, &declared); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	if v, ok := declared["expected_relationships"]; !ok || string(v) == "null" {
		return nil, fmt.Errorf("%s: expected_relationships is required — an absent key "+
			"is indistinguishable from asserting zero, so the grader can never fail the "+
			"fixture on relationships; declare real must-have rows, or an explicit "+
			"\"expected_relationships\": [] to state that this fixture deliberately "+
			"asserts none", p)
	}
	// `match_by: qualified_name` and `source_file` on one row state two
	// different intents, and only one of them is honoured: resolveEntity's
	// qualified-name path returns before the file-narrowed lookup is ever
	// consulted, so the source_file is silently ignored. Silently is the
	// problem — the row LOOKS like it asserts a location and does not.
	// Rejecting at load is free today (0 of 423 rows set match_by at all) and
	// keeps the ambiguity from being introduced rather than diagnosed after
	// the fact, which is the failure mode #6464 spent its whole budget on.
	//
	// Scoped to qualified_name deliberately: `match_by: source_file` (and the
	// empty default) both go THROUGH the file-narrowed lookup, so source_file
	// there is honoured, not ignored, and there is nothing to reject.
	//
	// forbidden_entities rows go through the SAME resolveEntity, so they carry
	// the same ambiguity and are checked in the same loop rather than left to
	// be discovered later — a forbidden row that silently ignores its
	// source_file is worse than an expected one, because it fires on an entity
	// in a file the author did not name.
	rejectAmbiguousMatchBy := func(key string, rows []ExpectedEntity) error {
		for i, ee := range rows {
			if ee.MatchBy == "qualified_name" && ee.SourceFile != "" {
				return fmt.Errorf("%s: %s[%d] (%q): match_by "+
					"\"qualified_name\" ignores source_file %q — drop one of the two so the "+
					"row states a single intent", p, key, i, ee.Name, ee.SourceFile)
			}
		}
		return nil
	}
	if err := rejectAmbiguousMatchBy("expected_entities", f.ExpectedEntities); err != nil {
		return nil, err
	}
	if err := rejectAmbiguousMatchBy("forbidden_entities", f.ForbiddenEntities); err != nil {
		return nil, err
	}
	// #6488 arm B. Four ways a forbidden entity row can be decorative, all
	// rejected at load because none of them is visible in a green report — a
	// forbidden row that cannot fire looks exactly like one that is holding,
	// which is the failure this whole arm exists not to reproduce:
	//
	//   - must_exist / nice_to_have are recall-only fields inherited from the
	//     shared ExpectedEntity shape. A forbidden row setting either states a
	//     contradiction ("this must exist and must not exist"), and the
	//     forbidden path ignores them, so the row does not mean what it says.
	//   - a row with neither name nor qualified_name resolves nothing on every
	//     path, so it can never fire.
	//   - a row with no KIND is the same trap one axis over, and it is the one
	//     that survived the first cut of this guard. resolveEntity keys on
	//     byKindName[Kind+"\x00"+Name], so `{"name": "Role"}` looks for an
	//     entity whose Kind is the empty string: it loads clean, reads as an
	//     assertion, and can never match. match_by "qualified_name" is exempt
	//     because that path resolves through byQName and never consults Kind —
	//     it is a row that CAN fire, so rejecting it would be wrong.
	//   - a row identical on every matching axis to a must_exist row is a
	//     contradiction the harness would otherwise honour twice, scoring the
	//     fixture 1/1 on recall and red on forbidden in the same run. Only an
	//     EXACT duplicate is rejected: proto-mini deliberately requires
	//     Role/SCOPE.Schema/user.proto with no subtype and forbids the same
	//     three axes with subtype "message", which is the subtype axis doing
	//     precisely the job it was added for.
	byExpectedAxes := make(map[ExpectedEntity]int, len(f.ExpectedEntities))
	for i, ee := range f.ExpectedEntities {
		if !ee.MustExist {
			continue
		}
		byExpectedAxes[matchAxes(ee)] = i
	}
	for i, fe := range f.ForbiddenEntities {
		if fe.MustExist || fe.NiceToHave {
			return nil, fmt.Errorf("%s: forbidden_entities[%d] (%q): must_exist / "+
				"nice_to_have are recall fields and are ignored on a forbidden row — a "+
				"row cannot both be required and be forbidden; drop the key", p, i, fe.Name)
		}
		if fe.Name == "" && fe.QualifiedName == "" {
			return nil, fmt.Errorf("%s: forbidden_entities[%d]: a forbidden entity row "+
				"needs a name (or a qualified_name with match_by \"qualified_name\") — a "+
				"nameless row resolves nothing and can never fire", p, i)
		}
		if fe.Kind == "" && fe.MatchBy != "qualified_name" {
			return nil, fmt.Errorf("%s: forbidden_entities[%d] (%q): a forbidden entity "+
				"row needs a kind — the lookup is keyed on (kind, name), so a row without "+
				"one searches for an entity whose kind is the empty string and can never "+
				"fire; state the kind, or use match_by \"qualified_name\", which resolves "+
				"without consulting kind", p, i, fe.Name)
		}
		if j, dup := byExpectedAxes[matchAxes(fe)]; dup {
			return nil, fmt.Errorf("%s: forbidden_entities[%d] (%q) is identical on every "+
				"matching axis to expected_entities[%d], which requires it — a row cannot "+
				"both be required and be forbidden, and the fixture would score 1/1 recall "+
				"and go red in the same run; narrow one of the two (subtype and source_file "+
				"both discriminate) or drop one", p, i, fe.Name, j)
		}
	}
	// #6490 arm A, second half. Declaring the key is not yet an assertion: a
	// fixture can declare it and still assert nothing that can ever go red —
	// an empty array, or rows that are all `nice_to_have`. Nine of the
	// twenty-six golden fixtures were in that shape, so `grafel quality`
	// reported "relationships: 0/0 — pass" for six languages (three of them
	// C# fixtures) whose edge extraction it was grading with nothing.
	//
	// The floor is therefore counted in MUST-HAVE rows, not in rows.
	// `nice_to_have` rows are evaluated but never counted as a miss, so a
	// fixture made entirely of them is exactly as unfailable as an empty
	// array; a check written against `len(rows)` sees three rows and waves
	// haskell-warp-mini through.
	//
	// forbidden_relationships deliberately does not count either. A forbidden
	// row goes red when an edge APPEARS — it can never go red when the
	// extractor stops emitting edges, which is the regression this floor
	// exists to catch.
	//
	// AssertsNoRelationships is the only exemption, and it must be positively
	// true. An absent field, an empty array and a `false` all mean "no
	// exemption claimed": the defect being closed here is precisely that an
	// absent or empty value was read as consent, so the opt-in may not be
	// expressible as one.
	mustHave := 0
	for _, er := range f.ExpectedRelationships {
		if er.MustExist {
			mustHave++
		}
	}
	if mustHave == 0 && !f.AssertsNoRelationships {
		return nil, fmt.Errorf("%s: no must-have relationship rows (%d row(s) declared, "+
			"%d forbidden) — a fixture that asserts no relationship cannot go red when "+
			"edge extraction regresses, so it grades its language with nothing; add "+
			"must_exist rows, or set \"asserts_no_relationships\": true to state the "+
			"exemption out loud", p, len(f.ExpectedRelationships), len(f.ForbiddenRelationships))
	}

	return &f, nil
}

// matchAxes projects an ExpectedEntity onto exactly the fields resolveEntity
// consults, dropping the ones it does not (must_exist, nice_to_have, note).
// Two rows with the same axes name the same set of entities, which is what
// makes one required and the other forbidden a contradiction rather than a
// narrowing (#6488 arm B).
//
// Two things it is deliberately NOT. It is not a whole-row comparison: a
// forbidden row cannot set must_exist at all, so every pair would differ on
// that field and the check would never fire — decorative, which is the defect
// class arm B exists to remove. And it is not a string concatenation, so a
// row whose Name is "a\x00b" cannot collide with one whose Kind ends in "a".
//
// The fields ARE enumerated, so adding a matching axis to ExpectedEntity means
// adding it here too. Forgetting is not silent: two rows that differ only on
// the new axis would project equal and LoadFixture would reject a legitimate
// fixture at load, which is loud and immediate rather than a quiet weakening.
// TestLoadFixture_AllowsAForbiddenRowThatDiffersOnlyBySubtype_6488 is the
// standing proof for the axis most recently added.
func matchAxes(ee ExpectedEntity) ExpectedEntity {
	return ExpectedEntity{
		Name:          ee.Name,
		QualifiedName: ee.QualifiedName,
		Kind:          ee.Kind,
		SourceFile:    ee.SourceFile,
		MatchBy:       ee.MatchBy,
		Subtype:       ee.Subtype,
	}
}

// SourceDir returns the path the harness will hand to the indexer.
// We keep this trivial so callers don't have to know the layout.
func SourceDir(fixtureDir string) string {
	return filepath.Join(fixtureDir, "src")
}
