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
// The two failure modes this file pins are symmetric:
//
//   - inference that never fires (the tool stays useless), and
//   - inference presented as fact (the #6006 defect class, reintroduced one
//     layer up: a confident answer the caller cannot distinguish from a
//     measured one).
//
// FIXTURE SHAPE IS LOAD-BEARING. Every fixture below is production-shaped: only
// entities that existed at the last group index carry a CommunityID. Newly
// added entities carry NONE — stamping them would fabricate a state the
// group-algo pass cannot produce, and would make every test here vacuous.
package graph

import "testing"

// placedEnt is an entity as the overlay stamper leaves it: covered by the last
// group index, so it carries a real community id.
func placedEnt(id, file, module string, community int) Entity {
	e := Entity{ID: id, Name: id, Kind: "function", SourceFile: file, CommunityID: ci(community)}
	if module != "" {
		e = e.WithProperties(map[string]string{"module": module})
	}
	return e
}

// newEnt is an entity that exists only on the feature ref: no community, by
// construction, because the overlay predates it.
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
//	community 9 (db):   db:d,   db:e             in db/*.go,   module "db"
func prImpact6042Fixture() ([]Entity, []Relationship) {
	return []Entity{
			placedEnt("core:a", "core/a.go", "core", 7),
			placedEnt("core:b", "core/b.go", "core", 7),
			placedEnt("core:c", "core/c.go", "core", 7),
			placedEnt("db:d", "db/d.go", "db", 9),
			placedEnt("db:e", "db/e.go", "db", 9),
		}, []Relationship{
			{FromID: "core:a", ToID: "core:b", Kind: "CALLS"},
			{FromID: "db:d", ToID: "db:e", Kind: "CALLS"},
		}
}

// addOne appends one added entity (+ its outbound edges) and returns the change
// set that adds exactly it — the add-only PR shape.
func addOne(ents []Entity, rels []Relationship, e Entity, calls ...string) ([]Entity, []Relationship, ChangeSet) {
	ents = append(ents, e)
	for _, target := range calls {
		rels = append(rels, Relationship{FromID: e.ID, ToID: target, Kind: "CALLS"})
	}
	return ents, rels, ChangeSet{Added: []DiffEntityEntry{{ID: e.ID, Name: e.Name, Kind: e.Kind}}}
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
// file belongs to that file's community with very high confidence. Module is
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
	// The rollup says how much of the community's touch was inferred.
	if len(res.ImpactedCommunities) != 1 || res.ImpactedCommunities[0].InferredChangedCount != 1 {
		t.Errorf("impacted community rollup must count the inferred entity: %+v", res.ImpactedCommunities)
	}
}

// Signal 2 — module. A brand-new FILE inside an existing module: no same-file
// sibling exists, so only the module prior can place it.
func TestAnalyzePRImpact_InfersFromModuleWhenFileIsNew(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	ents, rels, change := addOne(ents, rels, newEnt("core:new", "core/brand_new.go", "core"))

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "core:new")
	if got.CommunityID != 7 || got.CommunitySource != CommunitySourceInferred {
		t.Errorf("new file in module \"core\" got community %d source %q, want 7/inferred",
			got.CommunityID, got.CommunitySource)
	}
}

// Signal 3 — outbound call targets. A new entity in a new file in a new module,
// calling two placed entities that agree. This is close to what Louvain would
// have decided had it seen the entity.
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
	if res.CommunityDataAvailable {
		t.Errorf("CommunityDataAvailable = true with nothing placed and nothing inferrable — " +
			"#6006's decline path was weakened into meaninglessness")
	}
	if res.CommunityDataInferredOnly {
		t.Errorf("no inference happened; CommunityDataInferredOnly must be false")
	}
}

// Signals that DISAGREE must decline, not guess. Here the containing file says
// community 7 while the module AND the call targets say 9 — the containing
// component is weighted highest, but not highly enough to outweigh both of the
// other signals together.
func TestAnalyzePRImpact_ConflictingSignalsDecline(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	ents, rels, change := addOne(ents, rels,
		newEnt("core:new", "core/a.go", "db"), // file → 7, module → 9
		"db:d", "db:e")                        // targets → 9

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "core:new")
	if got.CommunitySource != CommunitySourceNone {
		t.Errorf("file says 7, module and call targets say 9 — inference must abstain; got %d/%q",
			got.CommunityID, got.CommunitySource)
	}
	if res.CommunityDataAvailable {
		t.Errorf("ambiguous signals are not a placement")
	}
}

// A file whose placed entities are SPLIT between communities gives no plurality,
// so the container signal abstains — and with no other signal, the entity is
// unplaced. Without this, a 50/50 file would be decided by map iteration order.
func TestAnalyzePRImpact_AmbiguousContainerAbstains(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	// A second placed entity in core/a.go, in the OTHER community.
	ents = append(ents, placedEnt("core:split", "core/a.go", "", 9))
	ents, rels, change := addOne(ents, rels, newEnt("core:new", "core/a.go", ""))

	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	got := changedByID(t, res, "core:new")
	if got.CommunitySource != CommunitySourceNone {
		t.Errorf("core/a.go is split 1-1 between communities 7 and 9; inference must abstain, got %d/%q",
			got.CommunityID, got.CommunitySource)
	}
}

// Inference must not CHAIN. Only overlay-placed entities may vote; an entity
// that was itself inferred is a guess, and letting guesses vote would propagate
// one weak signal across a whole new package.
func TestAnalyzePRImpact_InferenceDoesNotChain(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	// first and second each infer 7 from their own placed call targets. third's
	// only neighbours are first and second — enough targets to vote, but neither
	// is OVERLAY-placed, so third must stay unplaced. Distinct new files keep the
	// container signal out of it.
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
}

// Nothing may be inferred when the entity was already placed by the overlay —
// the overlay is ground truth and must win outright.
func TestAnalyzePRImpact_OverlayPlacementIsNeverOverwritten(t *testing.T) {
	ents, rels := prImpact6042Fixture()
	// db:d sits in db/d.go but we pretend a stray placed sibling suggests 7.
	ents = append(ents, placedEnt("db:stray", "db/d.go", "db", 7))
	res := AnalyzePRImpact(ents, rels, ChangeSet{
		Modified: []DiffEntityEntry{{ID: "db:d"}},
	}, DefaultPRImpactOptions())

	got := changedByID(t, res, "db:d")
	if got.CommunityID != 9 || got.CommunitySource != CommunitySourceOverlay {
		t.Errorf("overlay-placed entity got %d/%q, want 9/overlay", got.CommunityID, got.CommunitySource)
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

// ── Merge risk ───────────────────────────────────────────────────────────────

// Two add-only refs whose entities were both INFERRED into the same community do
// produce a risky pair — that is the whole point of #6042 — but the result must
// say the verdict rests entirely on inference.
func TestAnalyzeMergeRisk_InferredOnlyVerdictIsFlagged(t *testing.T) {
	inferred := AnalyzeMergeRisk([]ChangeImpact{
		{Ref: "pr-a", Communities: []int{7}, CommunityDataAvailable: true, InferredEntityCount: 1},
		{Ref: "pr-b", Communities: []int{7}, CommunityDataAvailable: true, InferredEntityCount: 2},
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
	if measured.InferredEntityCount != 0 {
		t.Errorf("InferredEntityCount = %d, want 0", measured.InferredEntityCount)
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
func BenchmarkAnalyzePRImpact_Inference(b *testing.B) {
	const n = 20000
	ents := make([]Entity, 0, n+1)
	rels := make([]Relationship, 0, n)
	for i := 0; i < n; i++ {
		f := "pkg" + string(rune('a'+i%20)) + "/f.go"
		ents = append(ents, placedEnt("e"+itoaBench(i), f, "pkg"+string(rune('a'+i%20)), i%50))
		if i > 0 {
			rels = append(rels, Relationship{FromID: "e" + itoaBench(i), ToID: "e" + itoaBench(i-1), Kind: "CALLS"})
		}
	}
	ents = append(ents, newEnt("new:x", "pkga/f.go", "pkga"))
	change := ChangeSet{Added: []DiffEntityEntry{{ID: "new:x"}}}
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
