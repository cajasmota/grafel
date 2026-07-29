// pr_impact_6042_test.go — issue #6042: an add-only PR must still get a
// merge-risk verdict, WITHOUT the verdict pretending to be measured.
//
// #6006 made the tool decline when no changed entity could be placed in a
// community. That is correct but useless for the most ordinary PR shape there
// is: one that only adds code. A newly added entity is never in the group-algo
// overlay, because the overlay is computed from the last indexed group union
// and the entity did not exist then.
//
// So we INFER a community from the entity's PLACED neighbours — and label it.
// The failure modes pinned here are symmetric:
//
//   - inference that never fires (the tool stays useless),
//   - inference presented as fact (the #6006 defect class one layer up), and
//   - inference that overrules a decision the partition actually made.
//
// FIXTURE SHAPE IS LOAD-BEARING. Every fixture below is production-shaped: only
// entities that existed at the last group index carry a CommunityID. Newly
// added entities carry NONE — stamping them would fabricate a state the
// group-algo pass cannot produce, and would make every test here vacuous.
package graph

import (
	"reflect"
	"testing"
)

// placedEnt is an entity as the overlay stamper leaves it: covered by the last
// group index, so it carries a real community id.
func placedEnt(id, file, module string, community int) Entity {
	e := Entity{ID: id, Name: id, Kind: "function", SourceFile: file, CommunityID: ci(community)}
	if module != "" {
		e = e.WithProperties(map[string]string{"module": module})
	}
	return e
}

// newEnt is an entity that exists only on the feature ref: NO CommunityID
// pointer at all, by construction, because the overlay predates it. That nil —
// not a negative id — is what makes it an inference candidate.
func newEnt(id, file, module string) Entity {
	e := Entity{ID: id, Name: id, Kind: "function", SourceFile: file}
	if module != "" {
		e = e.WithProperties(map[string]string{"module": module})
	}
	return e
}

// prImpact6042Fixture is the "last indexed group union": two communities, each
// with its own directory/module. Nothing here is new.
//
//	community 7 (core): core:a, core:b, core:c   in core/*.go, module "core"
//	community 9 (db):   db:d, db:e, db:f         in db/*.go,   module "db"
func prImpact6042Fixture() ([]Entity, []Relationship) {
	return []Entity{
			placedEnt("core:a", "core/a.go", "core", 7),
			placedEnt("core:b", "core/b.go", "core", 7),
			placedEnt("core:c", "core/c.go", "core", 7),
			placedEnt("db:d", "db/d.go", "db", 9),
			placedEnt("db:e", "db/e.go", "db", 9),
			placedEnt("db:f", "db/f.go", "db", 9),
		}, []Relationship{
			{FromID: "core:a", ToID: "core:b", Kind: "CALLS"},
			{FromID: "db:d", ToID: "db:e", Kind: "CALLS"},
		}
}

// addOneKind appends one added entity (+ its outbound edges of the given kind)
// and returns the change set that adds exactly it — the add-only PR shape.
func addOneKind(ents []Entity, rels []Relationship, e Entity, kind string, targets ...string) (
	[]Entity, []Relationship, ChangeSet) {
	ents = append(ents, e)
	for _, target := range targets {
		rels = append(rels, Relationship{FromID: e.ID, ToID: target, Kind: kind})
	}
	return ents, rels, ChangeSet{Added: []DiffEntityEntry{{ID: e.ID, Name: e.Name, Kind: e.Kind}}}
}

func addOne(ents []Entity, rels []Relationship, e Entity, calls ...string) (
	[]Entity, []Relationship, ChangeSet) {
	return addOneKind(ents, rels, e, "CALLS", calls...)
}

// changedByID finds the changed-entity record, failing loudly when absent.
func changedByID(t *testing.T, res PRImpactResult, id string) ChangedEntity {
	t.Helper()
	for _, c := range res.ChangedEntities {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("changed entity %q missing from result: %+v", id, res.ChangedEntities)
	return ChangedEntity{}
}

// ── The signals ──────────────────────────────────────────────────────────────

// Signal 1 — containing component. A new function added to an ALREADY-PLACED
// file belongs to that file's community with high confidence. Module is
// deliberately unset here so the file is the only signal in play.
func TestAnalyzePRImpact_InfersFromContainingFile(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	ents, rels, change := addOne(ents, rels, newEnt("core:new", "core/a.go", ""))

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "core:new")
	if got.CommunityID != 7 {
		t.Errorf("new entity in core/a.go (community 7) got community %d, want 7", got.CommunityID)
	}
	if got.CommunitySource != CommunitySourceInferred {
		t.Errorf("community_source = %q, want %q — an inferred placement presented as "+
			"measured is the #6006 defect one layer up", got.CommunitySource, CommunitySourceInferred)
	}
	// The margin must travel with the placement: "inferred" alone cannot
	// distinguish a file consensus from a coin flip.
	if got.CommunityInference == nil {
		t.Fatalf("inferred placement carries no provenance; a caller cannot weigh it")
	}
	if !reflect.DeepEqual(got.CommunityInference.Signals, []string{inferSignalContainer}) {
		t.Errorf("signals = %v, want [container]", got.CommunityInference.Signals)
	}
	if got.CommunityInference.Support != 1 || got.CommunityInference.Sample != 1 {
		t.Errorf("support/sample = %d/%d, want 1/1",
			got.CommunityInference.Support, got.CommunityInference.Sample)
	}
	if !res.CommunityDataAvailable {
		t.Errorf("an inferred placement is still a placement; CommunityDataAvailable = false")
	}
	if res.ChangedWithInferredCommunity != 1 || res.ChangedWithOverlayCommunity != 0 ||
		res.ChangedWithoutCommunity != 0 {
		t.Errorf("counts = overlay %d / inferred %d / none %d, want 0/1/0",
			res.ChangedWithOverlayCommunity, res.ChangedWithInferredCommunity, res.ChangedWithoutCommunity)
	}
	if !res.CommunityDataInferredOnly {
		t.Errorf("every placement in this verdict is inferred; CommunityDataInferredOnly = false — " +
			"the caller cannot tell a fully-inferred verdict from a measured one")
	}
	if ids := res.ImpactedCommunityIDs(); len(ids) != 1 || ids[0] != 7 {
		t.Errorf("inferred community must reach the merge-risk key; got %v, want [7]", ids)
	}
	// The rollup says how much of the community's touch was inferred, and that
	// this community is in the impact set ONLY because of inference.
	if len(res.ImpactedCommunities) != 1 || res.ImpactedCommunities[0].InferredChangedCount != 1 ||
		!res.ImpactedCommunities[0].InferredOnly {
		t.Errorf("impacted community rollup must mark the community as inference-only: %+v",
			res.ImpactedCommunities)
	}
	if ids := res.InferredOnlyCommunityIDs(); len(ids) != 1 || ids[0] != 7 {
		t.Errorf("InferredOnlyCommunityIDs = %v, want [7]", ids)
	}
}

// Signal 2 — module, the FALLBACK. A brand-new FILE inside an existing module:
// no same-file sibling exists, so only the module prior can place it.
func TestAnalyzePRImpact_InfersFromModuleWhenFileIsNew(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	ents, rels, change := addOne(ents, rels, newEnt("core:new", "core/brand_new.go", "core"))

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "core:new")
	if got.CommunityID != 7 || got.CommunitySource != CommunitySourceInferred {
		t.Errorf("new file in module \"core\" got community %d source %q, want 7/inferred",
			got.CommunityID, got.CommunitySource)
	}
	if got.CommunityInference == nil ||
		!reflect.DeepEqual(got.CommunityInference.Signals, []string{inferSignalModule}) {
		t.Fatalf("want the module signal recorded, got %+v", got.CommunityInference)
	}
	// 3 of 3 — the concentration the module fallback demands.
	if got.CommunityInference.Support != 3 || got.CommunityInference.Sample != 3 {
		t.Errorf("support/sample = %d/%d, want 3/3",
			got.CommunityInference.Support, got.CommunityInference.Sample)
	}
}

// Signal 3 — outbound call targets. A new entity in a new file in a new module,
// calling two placed entities that agree. This is the only signal not derived
// from the file path, and the closest to what community detection would see.
func TestAnalyzePRImpact_InfersFromPlacedCallTargets(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	ents, rels, change := addOne(ents, rels,
		newEnt("new:x", "newpkg/x.go", "newpkg"), "core:a", "core:b")

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "new:x")
	if got.CommunityID != 7 || got.CommunitySource != CommunitySourceInferred {
		t.Errorf("new entity calling two community-7 entities got community %d source %q, want 7/inferred",
			got.CommunityID, got.CommunitySource)
	}
	if got.CommunityInference == nil ||
		!reflect.DeepEqual(got.CommunityInference.Signals, []string{inferSignalTargets}) {
		t.Fatalf("want the call_targets signal recorded, got %+v", got.CommunityInference)
	}
}

// Both signals agreeing is the strongest case, and BOTH must be recorded — a
// caller that sees one signal cannot tell it from two independent ones.
func TestAnalyzePRImpact_AgreeingSignalsAreBothRecorded(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	ents, rels, change := addOne(ents, rels,
		newEnt("core:new", "core/a.go", "core"), "core:b", "core:c")

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "core:new")
	if got.CommunityID != 7 || got.CommunitySource != CommunitySourceInferred {
		t.Fatalf("got %d/%q, want 7/inferred", got.CommunityID, got.CommunitySource)
	}
	if got.CommunityInference == nil || !reflect.DeepEqual(
		got.CommunityInference.Signals, []string{inferSignalContainer, inferSignalTargets}) {
		t.Errorf("signals = %+v, want [container call_targets]", got.CommunityInference)
	}
}

// ── Signal hygiene: what must NOT count as evidence ──────────────────────────

// IMPORTS must not drive inference. Every new file imports two placed packages,
// so counting imports would clear the >= 2 threshold universally while carrying
// almost no community information — the exact failure the threshold exists to
// prevent, reintroduced by edge kind.
func TestAnalyzePRImpact_ImportsAreNotACommunitySignal(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	ents, rels, change := addOneKind(ents, rels,
		newEnt("new:x", "newpkg/x.go", "newpkg"), "IMPORTS", "core:a", "core:b")

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "new:x")
	if got.CommunitySource != CommunitySourceNone {
		t.Errorf("IMPORTS is universal and carries no community signal; got %d/%q, want -1/none",
			got.CommunityID, got.CommunitySource)
	}
	if res.CommunityDataAvailable {
		t.Errorf("nothing inferrable; the #6006 decline path must stand")
	}
}

// CONTAINS must not vote as a call target. It IS the container signal; letting
// it through here would give one edge two votes under two names.
func TestAnalyzePRImpact_ContainsDoesNotVoteAsCallTarget(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	// The new entity CONTAINS two placed entities (child-ward, not parent-ward),
	// so the container signal cannot use them either.
	ents, rels, change := addOneKind(ents, rels,
		newEnt("new:x", "newpkg/x.go", "newpkg"), "CONTAINS", "core:a", "core:b")

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	if got := changedByID(t, res, "new:x"); got.CommunitySource != CommunitySourceNone {
		t.Errorf("outbound CONTAINS is not a call-target signal; got %d/%q, want -1/none",
			got.CommunityID, got.CommunitySource)
	}
}

// A placed CONTAINS PARENT is legitimate container evidence, even when the new
// entity's file is new. This is the signal the issue names first.
func TestAnalyzePRImpact_ContainsParentIsContainerEvidence(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	ents = append(ents, newEnt("new:method", "newpkg/x.go", ""))
	rels = append(rels, Relationship{FromID: "core:a", ToID: "new:method", Kind: "CONTAINS"})
	change := ChangeSet{Added: []DiffEntityEntry{{ID: "new:method"}}}

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "new:method")
	if got.CommunityID != 7 || got.CommunitySource != CommunitySourceInferred {
		t.Errorf("a placed CONTAINS parent places its child; got %d/%q, want 7/inferred",
			got.CommunityID, got.CommunitySource)
	}
	if got.CommunityInference == nil ||
		!reflect.DeepEqual(got.CommunityInference.Signals, []string{inferSignalContainer}) {
		t.Errorf("want the container signal recorded, got %+v", got.CommunityInference)
	}
}

// ── The decline paths that MUST survive ──────────────────────────────────────

// A SINGLE placed call target is not evidence. Everything calls a logger; one
// edge would place half of every new package in whatever community the shared
// utility happens to sit in. This threshold is also exactly what keeps
// #6006's TestAnalyzePRImpact_AvailabilityFollowsChangedSetNotEntitySet binding.
func TestAnalyzePRImpact_SingleCallTargetIsNotEnoughToInfer(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	ents, rels, change := addOne(ents, rels, newEnt("new:x", "newpkg/x.go", "newpkg"), "core:a")

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "new:x")
	if got.CommunitySource != CommunitySourceNone || got.CommunityID != -1 {
		t.Errorf("one outbound edge is not a community signal; got community %d source %q, want -1/none",
			got.CommunityID, got.CommunitySource)
	}
	if res.CommunityDataAvailable {
		t.Errorf("nothing could be placed, so the #6006 decline path must stand")
	}
	if res.ChangedWithoutCommunity != 1 {
		t.Errorf("ChangedWithoutCommunity = %d, want 1", res.ChangedWithoutCommunity)
	}
}

// No placed neighbours at all — a whole new subsystem. Nothing to infer from,
// so the tool must still decline rather than invent a community.
func TestAnalyzePRImpact_NoPlacedNeighboursStillDeclines(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	ents, rels, change := addOne(ents, rels, newEnt("new:lonely", "brandnew/l.go", "brandnew"))

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "new:lonely")
	if got.CommunitySource != CommunitySourceNone {
		t.Errorf("no placed neighbour exists; community_source = %q, want none", got.CommunitySource)
	}
	if got.CommunityInference != nil {
		t.Errorf("no inference happened, so no provenance: %+v", got.CommunityInference)
	}
	if res.CommunityDataAvailable {
		t.Errorf("CommunityDataAvailable = true with nothing placed and nothing inferrable — " +
			"#6006's decline path was weakened into meaninglessness")
	}
	if res.CommunityDataInferredOnly {
		t.Errorf("no inference happened; CommunityDataInferredOnly must be false")
	}
}

// Signals that DISAGREE must decline, not guess: the containing file says 7,
// the call targets say 9.
func TestAnalyzePRImpact_ContainerAndTargetsDisagreeDecline(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	ents, rels, change := addOne(ents, rels,
		newEnt("core:new", "core/a.go", ""), // container → 7
		"db:d", "db:e")                      // targets → 9

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "core:new")
	if got.CommunitySource != CommunitySourceNone {
		t.Errorf("file says 7 and call targets say 9 — inference must abstain; got %d/%q",
			got.CommunityID, got.CommunitySource)
	}
	if res.CommunityDataAvailable {
		t.Errorf("ambiguous signals are not a placement")
	}
}

// The same rule with the module standing in as the primary: a brand-new file in
// module "core" (→ 7) whose call targets say 9.
func TestAnalyzePRImpact_ModuleAndTargetsDisagreeDecline(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	ents, rels, change := addOne(ents, rels,
		newEnt("core:new", "core/brand_new.go", "core"), // module → 7 (the file is new)
		"db:d", "db:e") // targets → 9

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	if got := changedByID(t, res, "core:new"); got.CommunitySource != CommunitySourceNone {
		t.Errorf("module says 7 and call targets say 9 — inference must abstain; got %d/%q",
			got.CommunityID, got.CommunitySource)
	}
}

// A file whose placed entities are SPLIT between communities gives no plurality,
// so the container signal abstains. Critically it must NOT then fall through to
// the module: the container did not abstain for lack of evidence, it abstained
// because its evidence was contradictory, and the module is a strictly coarser
// view of the same path that would paper over exactly that.
func TestAnalyzePRImpact_AmbiguousContainerAbstains(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	// A second placed entity in core/a.go, in the OTHER community. Module "core"
	// still leans 7 overall, so a fall-through would silently place this entity.
	ents = append(ents, placedEnt("core:split", "core/a.go", "core", 9))
	ents, rels, change := addOne(ents, rels, newEnt("core:new", "core/a.go", "core"))

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "core:new")
	if got.CommunitySource != CommunitySourceNone {
		t.Errorf("core/a.go is split 1-1 between communities 7 and 9; inference must abstain, got %d/%q",
			got.CommunityID, got.CommunitySource)
	}
}

// ── The module fallback is bounded ───────────────────────────────────────────

// module.Derive is a depth-capped PATH PREFIX: with no per-Go-package marker a
// single-module repo puts everything under internal/graph/** in one bucket. A
// bare plurality over such a bucket is noise wearing a community id, so the
// module vote requires real concentration.
func TestAnalyzePRImpact_ModulePluralityWithoutConcentrationDeclines(t *testing.T) {
	// A big heterogeneous module: 6 in community 7, 5 in community 9 — a genuine
	// plurality (6/11 = 0.55) and nothing like a consensus.
	var ents []Entity
	for i := 0; i < 6; i++ {
		s := itoaBench(i)
		ents = append(ents, placedEnt("m:a"+s, "mixed/a"+s+".go", "mixed", 7))
	}
	for i := 0; i < 5; i++ {
		s := itoaBench(i)
		ents = append(ents, placedEnt("m:b"+s, "mixed/b"+s+".go", "mixed", 9))
	}
	ents, rels, change := addOne(ents, nil, newEnt("m:new", "mixed/brand_new.go", "mixed"))

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "m:new")
	if got.CommunitySource != CommunitySourceNone {
		t.Errorf("module \"mixed\" is 6/11 in community 7 — a plurality, not a signal; "+
			"got %d/%q, want -1/none", got.CommunityID, got.CommunitySource)
	}
	if res.CommunityDataAvailable {
		t.Errorf("a 55%% path-prefix lean is not a placement")
	}
}

// A tight module still places — the floor must reject noise without rejecting
// the genuinely concentrated case.
func TestAnalyzePRImpact_ConcentratedModuleStillInfers(t *testing.T) {
	var ents []Entity
	for i := 0; i < 9; i++ {
		s := itoaBench(i)
		ents = append(ents, placedEnt("m:a"+s, "tight/a"+s+".go", "tight", 7))
	}
	ents = append(ents, placedEnt("m:z", "tight/z.go", "tight", 9)) // 9/10 = 0.9
	ents, rels, change := addOne(ents, nil, newEnt("m:new", "tight/brand_new.go", "tight"))

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "m:new")
	if got.CommunityID != 7 || got.CommunitySource != CommunitySourceInferred {
		t.Fatalf("a 9-of-10 module is a real signal; got %d/%q, want 7/inferred",
			got.CommunityID, got.CommunitySource)
	}
	// And the margin is on the wire, so 9/10 is distinguishable from 6/11.
	if got.CommunityInference.Support != 9 || got.CommunityInference.Sample != 10 {
		t.Errorf("support/sample = %d/%d, want 9/10",
			got.CommunityInference.Support, got.CommunityInference.Sample)
	}
}

// A module with too FEW placed entities is not a prior at all, however unanimous.
func TestAnalyzePRImpact_TinyModuleSampleDeclines(t *testing.T) {
	ents := []Entity{
		placedEnt("t:a", "tiny/a.go", "tiny", 7),
		placedEnt("t:b", "tiny/b.go", "tiny", 7),
	}
	ents, rels, change := addOne(ents, nil, newEnt("t:new", "tiny/brand_new.go", "tiny"))

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	if got := changedByID(t, res, "t:new"); got.CommunitySource != CommunitySourceNone {
		t.Errorf("2 placed entities is not a module prior; got %d/%q, want -1/none",
			got.CommunityID, got.CommunitySource)
	}
}

// The module must NOT vote alongside the container. module is
// module.Derive(SourceFile) — a pure function of the path — so the module
// histogram is a strict SUPERSET of the file histogram, and letting both vote
// counts one measurement twice. Here the file (community 7) is the specific
// evidence and the wider module leans 9; the file must simply win, with the
// module recorded nowhere.
func TestAnalyzePRImpact_ModuleDoesNotVoteAlongsideContainer(t *testing.T) {
	ents := []Entity{
		placedEnt("w:a", "wide/a.go", "wide", 7), // the new entity's file
		placedEnt("w:b", "wide/b.go", "wide", 9),
		placedEnt("w:c", "wide/c.go", "wide", 9),
		placedEnt("w:d", "wide/d.go", "wide", 9),
		placedEnt("w:e", "wide/e.go", "wide", 9), // module "wide" is 4/5 → 9
	}
	ents, rels, change := addOne(ents, nil, newEnt("w:new", "wide/a.go", "wide"))

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "w:new")
	if got.CommunityID != 7 || got.CommunitySource != CommunitySourceInferred {
		t.Errorf("the containing file is the more specific evidence; got %d/%q, want 7/inferred",
			got.CommunityID, got.CommunitySource)
	}
	if got.CommunityInference == nil ||
		!reflect.DeepEqual(got.CommunityInference.Signals, []string{inferSignalContainer}) {
		t.Errorf("the module must not appear as a second, independent signal: %+v",
			got.CommunityInference)
	}
}

// ── Inference must not overrule the partition ────────────────────────────────

// A NON-NIL negative community id means the partition SAW this entity and
// declined to place it (-2 is groupalgo's "not assigned"; legacy graph.json can
// carry -1). #6042 is about entities the partition has NEVER seen. Inferring
// here replaces a decision community detection actually made with a path-prefix
// heuristic.
//
// The fixture is production-shaped and deliberately RICH in signal: the entity
// sits in a placed file, in a concentrated module, and calls two placed
// entities. If candidacy keyed on communityOf(e) < 0 instead of the nil pointer,
// every one of those would fire and this would come back "inferred".
func TestAnalyzePRImpact_NegativeCommunityIDIsNotInferred(t *testing.T) {
	for _, cid := range []int{-1, -2} {
		ents, rels := prImpact6042Fixture()
		declined := placedEnt("core:declined", "core/a.go", "core", cid)
		ents, rels, change := addOne(ents, rels, declined, "core:b", "core:c")

		res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

		got := changedByID(t, res, "core:declined")
		if got.CommunitySource != CommunitySourceNone {
			t.Errorf("community_id=%d: the partition saw this entity and declined to place it; "+
				"got %d/%q — inference overruled the group algorithm",
				cid, got.CommunityID, got.CommunitySource)
		}
		if got.CommunityInference != nil {
			t.Errorf("community_id=%d: provenance emitted for a non-inference: %+v",
				cid, got.CommunityInference)
		}
		if res.CommunityDataAvailable {
			t.Errorf("community_id=%d: an unplaced-by-the-algorithm entity is not coverage", cid)
		}
	}
}

// Nothing may be inferred when the entity was already placed by the overlay —
// the overlay is ground truth and must win outright.
func TestAnalyzePRImpact_OverlayPlacementIsNeverOverwritten(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	// db:d sits in db/d.go; a stray placed sibling there suggests 7.
	ents = append(ents, placedEnt("db:stray", "db/d.go", "db", 7))
	res := AnalyzePRImpact(ents, rels, ChangeSet{
		Modified: []DiffEntityEntry{{ID: "db:d"}},
	}, DefaultPRImpactOptions())

	got := changedByID(t, res, "db:d")
	if got.CommunityID != 9 || got.CommunitySource != CommunitySourceOverlay {
		t.Errorf("overlay-placed entity got %d/%q, want 9/overlay", got.CommunityID, got.CommunitySource)
	}
	if got.CommunityInference != nil {
		t.Errorf("a measured placement must carry no inference provenance: %+v", got.CommunityInference)
	}
}

// Inference must not CHAIN. Only overlay-placed entities may vote; an entity
// that was itself inferred is a guess, and letting guesses vote would propagate
// one weak signal across a whole new package.
func TestAnalyzePRImpact_InferenceDoesNotChain(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	// first and second each infer 7 from their own placed call targets. third's
	// only neighbours are first and second — enough targets to vote, but neither
	// is OVERLAY-placed, so third must stay unplaced. Distinct new files and no
	// modules keep the path signals out of it.
	ents = append(ents,
		newEnt("new:first", "newpkg/x.go", ""),
		newEnt("new:second", "newpkg/y.go", ""),
		newEnt("new:third", "newpkg/z.go", ""))
	rels = append(rels,
		Relationship{FromID: "new:first", ToID: "core:a", Kind: "CALLS"},
		Relationship{FromID: "new:first", ToID: "core:b", Kind: "CALLS"},
		Relationship{FromID: "new:second", ToID: "core:a", Kind: "CALLS"},
		Relationship{FromID: "new:second", ToID: "core:c", Kind: "CALLS"},
		Relationship{FromID: "new:third", ToID: "new:first", Kind: "CALLS"},
		Relationship{FromID: "new:third", ToID: "new:second", Kind: "CALLS"},
	)
	change := ChangeSet{Added: []DiffEntityEntry{
		{ID: "new:first"}, {ID: "new:second"}, {ID: "new:third"},
	}}

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	for _, id := range []string{"new:first", "new:second"} {
		if got := changedByID(t, res, id); got.CommunitySource != CommunitySourceInferred {
			t.Errorf("%s has two placed call targets; want inferred, got %q", id, got.CommunitySource)
		}
	}
	got := changedByID(t, res, "new:third")
	if got.CommunitySource != CommunitySourceNone {
		t.Errorf("new:third's only neighbours were THEMSELVES inferred; want none, got %d/%q — "+
			"inference chained, so one weak signal propagated across a whole new package "+
			"and the result now depends on processing order", got.CommunityID, got.CommunitySource)
	}
}

// ── Labelling: measured and inferred must never blur ─────────────────────────

// A mixed change — one modified entity the overlay covers, one added entity we
// infer — must report both counts separately, and must NOT claim the verdict
// rests entirely on inference.
func TestAnalyzePRImpact_OverlayAndInferredAreCountedSeparately(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	ents = append(ents, newEnt("core:new", "core/a.go", "core"))
	change := ChangeSet{
		Modified: []DiffEntityEntry{{ID: "core:c"}},
		Added:    []DiffEntityEntry{{ID: "core:new"}},
	}

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	if got := changedByID(t, res, "core:c"); got.CommunitySource != CommunitySourceOverlay {
		t.Errorf("core:c is covered by the overlay; community_source = %q, want overlay", got.CommunitySource)
	}
	if got := changedByID(t, res, "core:new"); got.CommunitySource != CommunitySourceInferred {
		t.Errorf("core:new is new; community_source = %q, want inferred", got.CommunitySource)
	}
	if res.ChangedWithOverlayCommunity != 1 || res.ChangedWithInferredCommunity != 1 {
		t.Errorf("counts = overlay %d / inferred %d, want 1/1",
			res.ChangedWithOverlayCommunity, res.ChangedWithInferredCommunity)
	}
	if res.CommunityDataInferredOnly {
		t.Errorf("one entity was really measured; CommunityDataInferredOnly must be false")
	}
	// Community 7 holds a MEASURED changed entity too, so it is not an
	// inference-only community even though one of its entities was inferred.
	if ids := res.InferredOnlyCommunityIDs(); len(ids) != 0 {
		t.Errorf("community 7 also holds a measured changed entity; InferredOnlyCommunityIDs = %v, want []", ids)
	}
}

// A community reached by the BLAST RADIUS is measured — real edges to entities
// the overlay really placed — even when the seed was inferred. It must not be
// marked inference-only.
func TestAnalyzePRImpact_BlastRadiusMakesACommunityMeasured(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	// The new entity is inferred into 7, and core:a (community 7, placed) depends
	// on it, so 7 is also reached by the blast radius.
	ents = append(ents, newEnt("core:new", "core/a.go", ""))
	rels = append(rels, Relationship{FromID: "core:a", ToID: "core:new", Kind: "CALLS"})
	change := ChangeSet{Added: []DiffEntityEntry{{ID: "core:new"}}}

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	if got := changedByID(t, res, "core:new"); got.CommunitySource != CommunitySourceInferred {
		t.Fatalf("precondition: the added entity must be inferred, got %q", got.CommunitySource)
	}
	if res.BlastRadiusCount == 0 {
		t.Fatalf("precondition: core:a must be in the blast radius")
	}
	if ids := res.InferredOnlyCommunityIDs(); len(ids) != 0 {
		t.Errorf("community 7 is also reached by a real edge to a really-placed entity; "+
			"InferredOnlyCommunityIDs = %v, want []", ids)
	}
}

// An entity that is REMOVED on the head ref is absent from the head graph, so it
// has no neighbours to infer from. It must not be silently inferred from the
// diff record either.
func TestAnalyzePRImpact_RemovedEntityIsNotInferred(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	change := ChangeSet{Removed: []DiffEntityEntry{
		{ID: "gone:z", Name: "Z", Kind: "function", SourceFile: "core/a.go"},
	}}

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "gone:z")
	if got.CommunitySource != CommunitySourceNone {
		t.Errorf("a removed entity has no head-graph neighbours; got %d/%q, want -1/none",
			got.CommunityID, got.CommunitySource)
	}
}

// The three labels must always partition the changed set — otherwise the
// aggregate counts silently stop adding up and a caller cannot reconcile them.
func TestAnalyzePRImpact_SourceCountsPartitionTheChangedSet(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	ents = append(ents,
		newEnt("core:new", "core/a.go", "core"),   // inferred
		newEnt("new:lonely", "brandnew/l.go", "")) // none
	change := ChangeSet{
		Modified: []DiffEntityEntry{{ID: "core:c"}},                          // overlay
		Added:    []DiffEntityEntry{{ID: "core:new"}, {ID: "new:lonely"}},    // inferred + none
		Removed:  []DiffEntityEntry{{ID: "gone:z", SourceFile: "core/a.go"}}, // none
	}

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	sum := res.ChangedWithOverlayCommunity + res.ChangedWithInferredCommunity + res.ChangedWithoutCommunity
	if sum != res.ChangedCount {
		t.Errorf("overlay %d + inferred %d + none %d = %d, want changed_count %d",
			res.ChangedWithOverlayCommunity, res.ChangedWithInferredCommunity,
			res.ChangedWithoutCommunity, sum, res.ChangedCount)
	}
	for _, c := range res.ChangedEntities {
		switch c.CommunitySource {
		case CommunitySourceOverlay, CommunitySourceInferred, CommunitySourceNone:
		default:
			t.Errorf("entity %s carries no usable community_source (%q)", c.ID, c.CommunitySource)
		}
	}
}

// ── Merge risk ───────────────────────────────────────────────────────────────

// Two add-only refs whose entities were both INFERRED into the same community do
// produce a risky pair — that is the whole point of #6042 — but the result must
// say the verdict rests entirely on inference.
func TestAnalyzeMergeRisk_InferredOnlyVerdictIsFlagged(t *testing.T) {
	inferred := AnalyzeMergeRisk([]ChangeImpact{
		{Ref: "pr-a", Communities: []int{7}, CommunityDataAvailable: true, InferredEntityCount: 1,
			InferredOnlyCommunities: []int{7}},
		{Ref: "pr-b", Communities: []int{7}, CommunityDataAvailable: true, InferredEntityCount: 2,
			InferredOnlyCommunities: []int{7}},
	})
	if inferred.RiskyPairs != 1 {
		t.Fatalf("both refs land in community 7; want 1 risky pair, got %d", inferred.RiskyPairs)
	}
	if !inferred.CommunityDataInferredOnly {
		t.Errorf("no ref contributed an overlay-measured entity; CommunityDataInferredOnly = false — " +
			"an inferred verdict is indistinguishable from a measured one")
	}
	if inferred.InferredEntityCount != 3 {
		t.Errorf("InferredEntityCount = %d, want 3", inferred.InferredEntityCount)
	}

	// A measured verdict must NOT carry the marker.
	measured := AnalyzeMergeRisk([]ChangeImpact{
		{Ref: "pr-a", Communities: []int{7}, CommunityDataAvailable: true, OverlayEntityCount: 1},
		{Ref: "pr-b", Communities: []int{7}, CommunityDataAvailable: true, OverlayEntityCount: 1},
	})
	if measured.CommunityDataInferredOnly {
		t.Errorf("both refs were measured; CommunityDataInferredOnly must be false")
	}
	if measured.InferredEntityCount != 0 || measured.InferredOnlyPairCount != 0 {
		t.Errorf("measured verdict reported inferred count %d / inferred pairs %d, want 0/0",
			measured.InferredEntityCount, measured.InferredOnlyPairCount)
	}
	if measured.Pairs[0].InferredOnly || len(measured.Pairs[0].InferredSharedCommunities) != 0 {
		t.Errorf("a measured overlap must not be marked deduced: %+v", measured.Pairs[0])
	}

	// Partly measured is not "inferred only" — but the inferred count still shows.
	mixed := AnalyzeMergeRisk([]ChangeImpact{
		{Ref: "pr-a", Communities: []int{7}, CommunityDataAvailable: true, OverlayEntityCount: 2},
		{Ref: "pr-b", Communities: []int{7}, CommunityDataAvailable: true, InferredEntityCount: 1},
	})
	if mixed.CommunityDataInferredOnly {
		t.Errorf("pr-a contributed measured entities; CommunityDataInferredOnly must be false")
	}
	if mixed.InferredEntityCount != 1 {
		t.Errorf("InferredEntityCount = %d, want 1", mixed.InferredEntityCount)
	}
}

// Both refs carry MEASURED communities, so every aggregate flag reads
// "measured" — and yet the only community they SHARE, the entire basis of the
// reported conflict, exists on both sides purely by inference. A whole-verdict
// flag cannot express this; the pair must carry its own provenance, because the
// pair is where a merge decision is actually made.
func TestAnalyzeMergeRisk_PairOverlappingOnlyOnInferenceIsMarked(t *testing.T) {
	res := AnalyzeMergeRisk([]ChangeImpact{
		{Ref: "pr-a", Communities: []int{3, 7}, CommunityDataAvailable: true,
			OverlayEntityCount: 1, InferredEntityCount: 1, InferredOnlyCommunities: []int{7}},
		{Ref: "pr-b", Communities: []int{7, 9}, CommunityDataAvailable: true,
			OverlayEntityCount: 1, InferredEntityCount: 1, InferredOnlyCommunities: []int{7}},
	})

	if res.RiskyPairs != 1 {
		t.Fatalf("want 1 risky pair sharing community 7, got %+v", res.Pairs)
	}
	// The verdict-level flag is correctly false — measured entities exist — which
	// is exactly why it cannot be the only signal.
	if res.CommunityDataInferredOnly {
		t.Fatalf("precondition: both refs have measured entities, so the verdict is not inferred-only")
	}
	p := res.Pairs[0]
	if !reflect.DeepEqual(p.InferredSharedCommunities, []int{7}) {
		t.Errorf("inferred_shared_communities = %v, want [7]", p.InferredSharedCommunities)
	}
	if !p.InferredOnly {
		t.Errorf("the ONLY shared community is inferred on both sides, so this reported conflict is " +
			"entirely manufactured — risk_pairs[].inferred_only = false lets an agent read it as measured")
	}
	if res.InferredOnlyPairCount != 1 {
		t.Errorf("InferredOnlyPairCount = %d, want 1", res.InferredOnlyPairCount)
	}
}

// One inferred side is enough to make the OVERLAP deduced: the community was
// never observed on that ref, so the conflict was not observed either.
func TestAnalyzeMergeRisk_OneInferredSideMarksTheSharedCommunity(t *testing.T) {
	res := AnalyzeMergeRisk([]ChangeImpact{
		{Ref: "pr-a", Communities: []int{7}, CommunityDataAvailable: true, OverlayEntityCount: 1},
		{Ref: "pr-b", Communities: []int{7}, CommunityDataAvailable: true,
			InferredEntityCount: 1, InferredOnlyCommunities: []int{7}},
	})
	if len(res.Pairs) != 1 || !res.Pairs[0].InferredOnly {
		t.Errorf("pr-b reaches community 7 only by inference, so the overlap is deduced: %+v", res.Pairs)
	}
}

// A pair that ALSO shares a measured community is not "inferred only" — the
// conflict stands on its own — but the deduced community is still named.
func TestAnalyzeMergeRisk_PartlyMeasuredOverlapIsNotInferredOnly(t *testing.T) {
	res := AnalyzeMergeRisk([]ChangeImpact{
		{Ref: "pr-a", Communities: []int{5, 7}, CommunityDataAvailable: true,
			OverlayEntityCount: 1, InferredEntityCount: 1, InferredOnlyCommunities: []int{7}},
		{Ref: "pr-b", Communities: []int{5, 7}, CommunityDataAvailable: true, OverlayEntityCount: 2},
	})
	if len(res.Pairs) != 1 {
		t.Fatalf("want 1 pair, got %+v", res.Pairs)
	}
	p := res.Pairs[0]
	if p.InferredOnly {
		t.Errorf("community 5 is measured on both sides; the conflict is real: %+v", p)
	}
	if !reflect.DeepEqual(p.InferredSharedCommunities, []int{7}) {
		t.Errorf("inferred_shared_communities = %v, want [7] — the deduced overlap must still be named",
			p.InferredSharedCommunities)
	}
	if res.InferredOnlyPairCount != 0 {
		t.Errorf("InferredOnlyPairCount = %d, want 0", res.InferredOnlyPairCount)
	}
}

// An UNAVAILABLE verdict is not an inferred one. #6006's decline must not start
// wearing #6042's confidence marker.
func TestAnalyzeMergeRisk_UnavailableIsNotInferredOnly(t *testing.T) {
	// pr-a inferred one entity; pr-b could not be placed at all. The verdict is a
	// DECLINE, and a decline must not wear the low-confidence marker — that would
	// tell a caller a verdict exists when none does.
	res := AnalyzeMergeRisk([]ChangeImpact{
		{Ref: "pr-a", Communities: []int{7}, CommunityDataAvailable: true, InferredEntityCount: 1},
		{Ref: "pr-b", CommunityDataAvailable: false},
	})
	if res.CommunityDataAvailable {
		t.Fatalf("precondition: one uncovered ref must make the whole result unavailable")
	}
	if res.CommunityDataInferredOnly {
		t.Errorf("this is a DECLINE, not a low-confidence answer; CommunityDataInferredOnly must be false")
	}

	// And with nothing inferred anywhere either.
	none := AnalyzeMergeRisk([]ChangeImpact{
		{Ref: "pr-a", CommunityDataAvailable: false},
		{Ref: "pr-b", CommunityDataAvailable: false},
	})
	if none.CommunityDataInferredOnly || none.InferredEntityCount != 0 {
		t.Errorf("nothing was inferred; got inferred_only=%v count=%d",
			none.CommunityDataInferredOnly, none.InferredEntityCount)
	}
}

// Cost guard: inference is a bounded add-on, not a graph walk. It touches only
// the changed entities' own files/modules/outbound edges, so a large graph with
// a small change set must not pay for it.
//
// The fixture ASSERTS that inference actually fires before timing anything. An
// earlier version of this benchmark spread every file and module evenly across
// communities, so both signals abstained and it timed the DECLINE path while
// claiming to measure inference.
func BenchmarkAnalyzePRImpact_Inference(b *testing.B) {
	const n = 20000
	ents := make([]Entity, 0, n+1)
	rels := make([]Relationship, 0, n+2)
	for i := 0; i < n; i++ {
		// One file per entity, one module per 1000 entities, and a module maps to
		// exactly one community — so the module signal is concentrated and fires.
		mod := "pkg" + itoaBench(i/1000)
		ents = append(ents, placedEnt("e"+itoaBench(i), mod+"/f"+itoaBench(i)+".go", mod, i/1000))
		if i > 0 {
			rels = append(rels, Relationship{FromID: "e" + itoaBench(i), ToID: "e" + itoaBench(i-1), Kind: "CALLS"})
		}
	}
	// A new file in an existing module, calling two placed entities inside it.
	ents = append(ents, newEnt("new:x", "pkg0/brand_new.go", "pkg0"))
	rels = append(rels,
		Relationship{FromID: "new:x", ToID: "e1", Kind: "CALLS"},
		Relationship{FromID: "new:x", ToID: "e2", Kind: "CALLS"})
	change := ChangeSet{Added: []DiffEntityEntry{{ID: "new:x"}}}

	probe := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())
	if len(probe.ChangedEntities) != 1 ||
		probe.ChangedEntities[0].CommunitySource != CommunitySourceInferred {
		b.Fatalf("benchmark fixture does not exercise inference: %+v", probe.ChangedEntities)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())
	}
}

func itoaBench(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [12]byte
	p := len(buf)
	for i > 0 {
		p--
		buf[p] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[p:])
}
