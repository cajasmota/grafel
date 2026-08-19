package mcp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// #6329 / #6314 — machine-generated entities rank below authored ones.
//
// WHY A TIER AND NOT A SCORE PENALTY. Two measured facts about the ranking
// path decide this, and both cut against the obvious implementation:
//
//  1. BM25 does not index entity bodies. buildDocTerms (scoring.go) indexes
//     the name, the file stem (weight 1.5), the path directories (0.8), the
//     docstring property (0.6) and discriminator literals (0.5) — Entity.Content
//     is never tokenised. So "skip the bodies of generated files" is not by
//     itself a ranking fix; the ranking fix is the demotion plus, later, the
//     reduced entity count from ProjectDeclarations.
//
//  2. A multiplicative penalty applied inside BM25Index.Search would be erased
//     by FuseRRF, which replaces scores with rank reciprocals. It would have
//     looked correct and tested green on every repository without an
//     embeddings sidecar, and silently done nothing on every repository with
//     one.
//
// The tier is immune to both: it is applied after fusion, at the same sort
// that already floats real entities above shadows.

func genEntity(name string, line int, score float64) scored {
	e := &graph.Entity{
		Name:       name,
		Kind:       string(types.EntityKindClass),
		SourceFile: "api/v1/user.pb.go",
		StartLine:  line,
	}
	e.PropSet(types.EntityGeneratedProperty, "true")
	e.PropSet(types.EntityGeneratedByProperty, "path:*.pb.go")
	return scored{hit: Hit{Entity: e, Score: score}}
}

func authoredEntity(name string, line int, score float64) scored {
	e := &graph.Entity{
		Name:       name,
		Kind:       string(types.EntityKindClass),
		SourceFile: "internal/user/service.go",
		StartLine:  line,
	}
	return scored{hit: Hit{Entity: e, Score: score}}
}

// rerank calls the PRODUCTION comparator. It used to be a copy of the
// anonymous sort closure in handleQueryGraph, which meant the golden below was
// pinning a paraphrase — the two could diverge and both stay green. The
// closure has been extracted to rerankScored precisely so this test drives the
// real thing.
func rerank(all []scored) []scored {
	out := append([]scored(nil), all...)
	rerankScored(out)
	return out
}

// TestRankTier_GeneratedSitsBelowAuthored pins the tier constant's position:
// strictly worse than both authored tiers, strictly better than every noise
// bucket. A generated declaration must stay reachable — demoting it into a
// noise bucket would hide it behind include_noise, and #6329 exists precisely
// because those declarations have to remain in the graph and findable.
func TestRankTier_GeneratedSitsBelowAuthored(t *testing.T) {
	authoredLined := authoredEntity("UserService", 42, 1).hit.Entity
	// A lineless-but-legitimate entity must be an endpoint/resource kind: a
	// lineless Class is classified as a shadow and lands in the noise tiers,
	// which would make this assertion pass for the wrong reason.
	authoredLineless := &graph.Entity{
		Name:       "GET /users",
		Kind:       string(types.EntityKindEndpoint),
		SourceFile: "internal/user/routes.go",
	}
	generatedLined := genEntity("User", 17, 1).hit.Entity

	tLined := rankTier(authoredLined)
	tLineless := rankTier(authoredLineless)
	tGen := rankTier(generatedLined)

	if !(tLined < tGen) {
		t.Errorf("authored lined tier %d is not better than generated tier %d", tLined, tGen)
	}
	if !(tLineless < tGen) {
		t.Errorf("authored lineless tier %d is not better than generated tier %d", tLineless, tGen)
	}
	// Strictly better than the best noise bucket (shadow, tier 4).
	shadow := &graph.Entity{Name: "Shadow", Kind: string(types.EntityKindClass), Subtype: "shadow"}
	if tShadow := rankTier(shadow); tShadow <= tGen {
		t.Errorf("generated tier %d is not better than shadow tier %d; generated declarations must stay reachable", tGen, tShadow)
	}
	// Room left on both sides, as agreed, so a future disposition can be
	// inserted without renumbering.
	if tGen <= tLineless || tGen >= 4 {
		t.Errorf("generated tier %d is not strictly between the authored tiers and the noise tiers", tGen)
	}
}

// TestRerank_AuthoredOutranksGeneratedRegardlessOfScore is the ordering
// golden. It is deliberately built so BM25 alone would fill the whole top of
// the list with generated hits: user.pb.go's file stem contributes at weight
// 1.5 to a "user" query, which is exactly @manuel1358000's #6314 complaint.
//
// THE ASSERTION IS NO LONGER A TOTAL ORDER, and that is deliberate — see
// rerankScored for why an absolute partition was unsafe in the default output
// paths. What survives is the thing #6314 actually asked for: the authored
// answer must not be buried under a crowd of generated declarations. One
// generated hit — the single strongest match in the set — may precede it. The
// other three must not.
//
// MUTATION TARGET: set the generated tier equal to tier 0 and this must fail.
func TestRerank_AuthoredOutranksGeneratedRegardlessOfScore(t *testing.T) {
	in := []scored{
		genEntity("User", 17, 9.9),           // wins on BM25
		genEntity("UserRequest", 30, 9.8),    // …and so do these
		genEntity("UserReply", 44, 9.7),      //
		authoredEntity("UserService", 42, 1), // loses on BM25 to all of them
	}
	got := rerank(in)
	names := make([]string, len(got))
	for i, s := range got {
		names[i] = s.hit.Entity.Name
	}
	want := []string{"User", "UserService", "UserRequest", "UserReply"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("order = %v, want %v: the authored entity must sort above every "+
				"generated hit except the single strongest match", names, want)
		}
	}
}

// TestRerank_IsAPurePartition is the safety half of the change. Demoting
// generated entities must not reshuffle anything else: the relative order of
// authored entities among themselves, and of generated entities among
// themselves, must be exactly what it was before.
//
// MUTATION TARGET: set the generated tier to 9 and this must still pass — the
// invariant is about the partition, not about the constant's value.
func TestRerank_IsAPurePartition(t *testing.T) {
	in := []scored{
		genEntity("GenHigh", 1, 8),
		authoredEntity("AuthHigh", 10, 7),
		genEntity("GenLow", 2, 3),
		authoredEntity("AuthMid", 11, 5),
		genEntity("GenMid", 3, 6),
		authoredEntity("AuthLow", 12, 2),
	}
	got := rerank(in)

	var authored, gen []string
	for _, s := range got {
		if s.hit.Entity.PropGet(types.EntityGeneratedProperty) == "true" {
			gen = append(gen, s.hit.Entity.Name)
		} else {
			authored = append(authored, s.hit.Entity.Name)
		}
	}
	wantAuthored := []string{"AuthHigh", "AuthMid", "AuthLow"}
	wantGen := []string{"GenHigh", "GenMid", "GenLow"}
	for i := range wantAuthored {
		if authored[i] != wantAuthored[i] {
			t.Fatalf("authored order = %v, want %v (score order must be untouched)", authored, wantAuthored)
		}
	}
	for i := range wantGen {
		if gen[i] != wantGen[i] {
			t.Fatalf("generated order = %v, want %v (score order must be untouched)", gen, wantGen)
		}
	}
	// And the partition itself. It is no longer a TOTAL order — the single
	// strongest match in the set is exempt (here GenHigh at 8, which outscores
	// AuthHigh at 7) — so the invariant is stated over the remainder: after
	// the exempt hit, every authored entity precedes every generated one.
	//
	// This is the shape the review asked for: still an assertion about the
	// demotion, no longer an assertion that the demotion is unconditional.
	if got[0].hit.Entity.Name != "GenHigh" {
		t.Fatalf("first = %q, want the exempt strongest match GenHigh", got[0].hit.Entity.Name)
	}
	seenGen := false
	for _, s := range got[1:] {
		isGen := s.hit.Entity.PropGet(types.EntityGeneratedProperty) == "true"
		if isGen {
			seenGen = true
		} else if seenGen {
			t.Fatalf("authored entity %q sorted after a generated one; the demotion "+
				"must still partition everything below the exempt hit", s.hit.Entity.Name)
		}
	}
}

// TestRankTier_GeneratedNoiseStaysNoise — a generated entity that is ALSO a
// noise entity keeps its noise tier. The generated demotion must not
// accidentally PROMOTE a shadow node out of the noise bucket.
func TestRankTier_GeneratedNoiseStaysNoise(t *testing.T) {
	e := &graph.Entity{Name: "Shadow", Kind: string(types.EntityKindClass), Subtype: "shadow"}
	e.PropSet(types.EntityGeneratedProperty, "true")
	authored := authoredEntity("Real", 1, 1).hit.Entity
	if rankTier(e) <= rankTier(authored) {
		t.Fatal("a generated shadow was promoted above an authored entity")
	}
	plainShadow := &graph.Entity{Name: "Shadow", Kind: string(types.EntityKindClass), Subtype: "shadow"}
	if rankTier(e) != rankTier(plainShadow) {
		t.Fatalf("generated shadow tier %d != plain shadow tier %d; the flag changed a noise classification",
			rankTier(e), rankTier(plainShadow))
	}
}

// TestSerializeHits_ExposesGenerated — the flag must be visible through the
// MCP surface, not only in the graph. Without it the feature is unassertable
// where users actually meet it, which is the gap that let #6338 ship a green
// suite over an unusable report.
func TestSerializeHits_ExposesGenerated(t *testing.T) {
	repo := &LoadedRepo{Repo: "grafel"}
	in := []scored{
		{repo: repo, hit: genEntity("User", 17, 9.9).hit},
		{repo: repo, hit: authoredEntity("UserService", 42, 1).hit},
	}
	out := serializeHits(in, false)
	if len(out) != 2 {
		t.Fatalf("got %d rows, want 2", len(out))
	}
	if out[0]["generated"] != true {
		t.Errorf("generated hit row = %v, want generated:true", out[0])
	}
	if _, present := out[1]["generated"]; present {
		t.Errorf("authored hit row carries a generated key: %v; the field must be omitted, not false, "+
			"so the common case costs no tokens in the MCP payload", out[1])
	}
	// Verbose mode carries the provenance so a wrong flag is diagnosable from
	// a user's transcript alone.
	v := serializeHits(in, true)
	if v[0]["generated_by"] != "path:*.pb.go" {
		t.Errorf("verbose row generated_by = %v, want the rule that fired", v[0]["generated_by"])
	}
	if _, present := v[1]["generated_by"]; present {
		t.Errorf("authored verbose row carries generated_by: %v", v[1])
	}
}

// ---------------------------------------------------------------------------
// #6329 review round 2 — the demotion must not bury the strongest match, and
// the default views must not truncate in silence.
// ---------------------------------------------------------------------------

// TestRerank_StrongestGeneratedMatchSurvivesTheDemotion is the adversarial
// shape the partition failed on: a protobuf message name that exists ONLY in
// user.pb.go. weightFileStem = 1.5 puts it top on BM25, and an absolute
// partition drops it below every weak authored match — in a group, below the
// per-repo top 3, i.e. out of the default view entirely and with no signal.
//
// MUTATION TARGET: delete the exemptGeneratedHits exemption from rerankScored
// and this must fail.
func TestRerank_StrongestGeneratedMatchSurvivesTheDemotion(t *testing.T) {
	in := []scored{
		genEntity("UserProfileRequest", 17, 9.9), // the only real answer
		authoredEntity("userHandler", 42, 0.4),
		authoredEntity("userStore", 12, 0.3),
		authoredEntity("userCache", 8, 0.2),
	}
	got := rerank(in)
	if got[0].hit.Entity.Name != "UserProfileRequest" {
		names := make([]string, len(got))
		for i, s := range got {
			names[i] = s.hit.Entity.Name
		}
		t.Fatalf("order = %v; the generated entity outscores every authored hit "+
			"and is the only answer to the query, so it must not be demoted out "+
			"of the default view", names)
	}
}

// TestRerank_ExemptionIsOnlyTheSingleBestHit states the trade-off as a test.
// Exempting EVERY generated hit that outscores the best authored one would
// restore #6314 wholesale — that issue is exactly the case where a crowd of
// generated declarations outranks the authored answer. Only the top one is
// spared; the rest stay demoted, so authored results still surface from row 2.
//
// MUTATION TARGET: make exemptGeneratedHits return every generated entity in
// the run instead of the top-ranked one, and this must fail.
func TestRerank_ExemptionIsOnlyTheSingleBestHit(t *testing.T) {
	in := []scored{
		genEntity("GenA", 1, 9.9),
		genEntity("GenB", 2, 9.8),
		genEntity("GenC", 3, 9.7),
		authoredEntity("AuthOnly", 42, 0.5),
	}
	got := rerank(in)
	if got[0].hit.Entity.Name != "GenA" {
		t.Fatalf("first = %q, want the single strongest generated hit", got[0].hit.Entity.Name)
	}
	if got[1].hit.Entity.Name != "AuthOnly" {
		t.Fatalf("second = %q, want the authored hit; only ONE generated entity "+
			"is exempt from the demotion", got[1].hit.Entity.Name)
	}
}

// TestRerank_ExemptionDoesNotRescueNoise — a generated entity that is also a
// shadow stays in its noise tier even when it is the top scorer. The exemption
// restores the AUTHORED tier the entity would otherwise have had; it is not a
// blanket promotion.
func TestRerank_ExemptionDoesNotRescueNoise(t *testing.T) {
	shadow := &graph.Entity{Name: "Shadow", Kind: string(types.EntityKindClass), Subtype: "shadow"}
	shadow.PropSet(types.EntityGeneratedProperty, "true")
	in := []scored{
		{hit: Hit{Entity: shadow, Score: 9.9}},
		authoredEntity("Auth", 42, 0.1),
	}
	got := rerank(in)
	if got[0].hit.Entity.Name != "Auth" {
		t.Fatalf("first = %q, want the authored hit; a generated SHADOW must not "+
			"be exempted out of the noise tiers", got[0].hit.Entity.Name)
	}
}

// TestTruncationNote_SaysWhatWasCut pins the shared formatter. Both default
// views cut rows; before #6329 review round 2 neither said so.
func TestTruncationNote_SaysWhatWasCut(t *testing.T) {
	if n := truncationNote(3, 3, "what", "how"); n != "" {
		t.Errorf("note emitted when nothing was cut: %q", n)
	}
	if n := truncationNote(5, 3, "what", "how"); n != "" {
		t.Errorf("note emitted when shown exceeds total: %q", n)
	}
	n := truncationNote(3, 11, "per-repo view shows the top 3 hits in each repo", "pass full=true")
	for _, want := range []string{"8 of 11 ranked hits omitted", "top 3", "pass full=true"} {
		if !strings.Contains(n, want) {
			t.Errorf("note %q is missing %q", n, want)
		}
	}
}

// truncFixture builds one repo with n entities that all match the query token,
// so every ranked-view cut in the tool is exercised with a known total.
func truncFixture(repo string, n int) *graph.Document {
	doc := &graph.Document{Repo: repo}
	for i := 0; i < n; i++ {
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:         fmt.Sprintf("%s_inspection_%d", repo, i),
			Name:       fmt.Sprintf("inspectionHandler%d", i),
			Kind:       "SCOPE.Function",
			SourceFile: fmt.Sprintf("%s/inspection_%d.go", repo, i),
			StartLine:  i + 1,
		})
	}
	return doc
}

// TestFind_MultiRepoDefaultViewAnnouncesItsTruncation is BLOCKER 3 end to end.
//
// A grafel group with more than one repo is the NORMAL case, and its default
// view keeps the top 3 hits per repo. That cut was SILENT: every hit past the
// third simply did not exist as far as the caller — or a test — could tell.
// Once ranking carries a demotion tier that is not a cosmetic gap, it is the
// mechanism by which a wrongly flagged file disappears instead of dropping a
// few positions, which is the #6338 failure mode this work exists to avoid.
//
// This drives the real tool, not renderPerRepoSummary in isolation, so a
// mutant that deletes the CALL is caught as well as one that empties the note.
//
// MUTATION TARGET: drop perRepoTruncationSuffix from renderPerRepoSummary and
// this must fail.
func TestFind_MultiRepoDefaultViewAnnouncesItsTruncation(t *testing.T) {
	srv := newTestServer(t, truncFixture("alpha", 6), truncFixture("beta", 6))
	out := callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group":      "test",
		"query":      "inspection handler",
		"cross_repo": true,
		// The fixture names are near-identical by construction, so BM25 IDF
		// puts every hit at ~0; the default min_score=0.15 would cull the set
		// down to the always-1 fallback and nothing would be truncated at all.
		"min_score": 0.0,
	})
	if !strings.Contains(out, "truncation_note") {
		t.Fatalf("multi-repo default view truncated to the per-repo top 3 with no "+
			"truncation_note; the cut must never be silent. Output:\n%s", out)
	}
	if !strings.Contains(out, "ranked hits omitted") {
		t.Errorf("truncation note does not say how many hits were omitted:\n%s", out)
	}
}

// TestFind_MultiRepoViewIsSilentWhenNothingWasCut — the note must be evidence,
// not decoration. With 2 hits per repo nothing is truncated and nothing is
// claimed.
func TestFind_MultiRepoViewIsSilentWhenNothingWasCut(t *testing.T) {
	srv := newTestServer(t, truncFixture("alpha", 2), truncFixture("beta", 2))
	out := callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group":      "test",
		"query":      "inspection handler",
		"cross_repo": true,
		// The fixture names are near-identical by construction, so BM25 IDF
		// puts every hit at ~0; the default min_score=0.15 would cull the set
		// down to the always-1 fallback and nothing would be truncated at all.
		"min_score": 0.0,
	})
	if strings.Contains(out, "truncation_note") {
		t.Fatalf("truncation_note emitted when nothing was cut:\n%s", out)
	}
}

// TestFind_CompactViewAnnouncesItsSeedTruncation is the single-repo half of
// the same defect: the compact path keeps the first 10 ranked hits and said
// nothing about the rest.
//
// MUTATION TARGET: remove the keepNote case from the TruncatedNote switch and
// this must fail.
func TestFind_CompactViewAnnouncesItsSeedTruncation(t *testing.T) {
	srv := newTestServer(t, truncFixture("solo", 25))
	out := callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group": "test",
		"query": "inspection handler",
		// kind_filter is not incidental. Plain BM25 is capped at 10 hits per
		// repo at the source, so on a single repo the compact seed cut can
		// never fire from text ranking alone — it is reachable only through
		// enumerateByKind, which folds in every in-scope entity of the kind
		// and disables the min_score cull. That is the path this pins.
		"kind_filter": "SCOPE.Function",
		"min_score":   0.0,
	})
	if !strings.Contains(out, "ranked hits omitted") {
		t.Fatalf("compact view kept the top %d of 25 hits with no truncation note. Output:\n%s",
			compactSeedLimit, out)
	}
}

// ---------------------------------------------------------------------------
// #6329 review round 3 — the demotion has to survive the DEFAULT view, and the
// exemption has to survive FuseRRF.
// ---------------------------------------------------------------------------

// atRepo attaches a repo to a scored hit and gives the entity a unique id so
// the TOON renderer can address it.
func atRepo(sc scored, r *LoadedRepo) scored {
	sc.repo = r
	sc.hit.Entity.ID = r.Repo + "_" + sc.hit.Entity.Name
	return sc
}

// rankedMultiRepoFixture is the reviewer's repro for the per-repo re-sort: one
// exempt generated hit, one demoted generated hit that still outscores every
// authored hit, and three authored hits — all in one repo.
//
// After rerankScored the order is [gen_top auth_a auth_b auth_c gen_2]. The
// per-repo view keeps 3. gen_2 is tier-demoted to LAST, so it must not appear.
func rankedMultiRepoFixture() ([]scored, *LoadedGroup) {
	alpha := &LoadedRepo{Repo: "alpha"}
	beta := &LoadedRepo{Repo: "beta"}
	all := []scored{
		atRepo(genEntity("gen_top", 1, 9.9), alpha),
		atRepo(genEntity("gen_2", 2, 5.0), alpha),
		atRepo(authoredEntity("auth_a", 10, 1.0), alpha),
		atRepo(authoredEntity("auth_b", 11, 0.9), alpha),
		atRepo(authoredEntity("auth_c", 12, 0.8), alpha),
		atRepo(authoredEntity("beta_only", 13, 0.7), beta),
	}
	rerankScored(all)
	lg := &LoadedGroup{Name: "test", Repos: map[string]*LoadedRepo{"alpha": alpha, "beta": beta}}
	return all, lg
}

// TestPerRepoSummary_PreservesTheRankedOrder is BLOCKER A.
//
// renderPerRepoSummary used to re-sort each repo's hits by RAW SCORE, throwing
// away the tier ordering rerankScored had just established. In the default
// multi-repo view — the most-travelled output path in the tool — the generated
// demotion therefore did nothing at all: a tier-demoted generated hit rendered
// at position 2 and the AUTHORED hits were the ones truncated away. That is
// #6314 verbatim, in the default view, after the fix.
//
// MUTATION TARGET: restore the per-repo `sort.SliceStable(hits, ... Score >)`
// in either path and this must fail.
func TestPerRepoSummary_PreservesTheRankedOrder(t *testing.T) {
	all, lg := rankedMultiRepoFixture()
	out := renderPerRepoSummary(all, lg)
	if strings.Contains(out, "gen_2") {
		t.Fatalf("the tier-demoted generated hit gen_2 appears in the per-repo top %d; "+
			"the view must slice the ranked order, not re-sort by raw score:\n%s",
			perRepoSummaryLimit, out)
	}
	for _, want := range []string{"gen_top", "auth_a", "auth_b"} {
		if !strings.Contains(out, want) {
			t.Fatalf("per-repo view is missing %q; the authored hits must not be the "+
				"ones truncated away:\n%s", want, out)
		}
	}
}

// TestPerRepoSummary_MarkdownFallbackPreservesTheRankedOrder — the legacy
// markdown path carried the identical re-sort on its own line. A fix applied to
// only one of the two leaves the defect live for whoever sets
// MCP_FIND_FORMAT=markdown.
func TestPerRepoSummary_MarkdownFallbackPreservesTheRankedOrder(t *testing.T) {
	t.Setenv("MCP_FIND_FORMAT", "markdown")
	all, lg := rankedMultiRepoFixture()
	out := renderPerRepoSummary(all, lg)
	if strings.Contains(out, "gen_2") {
		t.Fatalf("markdown fallback rendered the tier-demoted hit gen_2 in the top %d:\n%s",
			perRepoSummaryLimit, out)
	}
	for _, want := range []string{"gen_top", "auth_a", "auth_b"} {
		if !strings.Contains(out, want) {
			t.Fatalf("markdown fallback is missing %q:\n%s", want, out)
		}
	}
}

// TestPerRepoSummary_AgreesWithTheGlowSelection — handleQueryGraph records the
// prefixed ids of the "shown" hits for the dashboard glow by walking `all` in
// ARRIVAL order and keeping the first perRepoSummaryLimit per repo. While the
// renderer re-sorted, the glow highlighted a different set of nodes than the
// text named. Slicing in arrival order makes the two definitionally equal.
func TestPerRepoSummary_AgreesWithTheGlowSelection(t *testing.T) {
	all, lg := rankedMultiRepoFixture()
	out := renderPerRepoSummary(all, lg)

	shownPerRepo := map[string]int{}
	for _, sc := range all {
		if shownPerRepo[sc.repo.Repo] >= perRepoSummaryLimit {
			continue
		}
		shownPerRepo[sc.repo.Repo]++
		if !strings.Contains(out, sc.hit.Entity.Name) {
			t.Fatalf("glow highlights %q but the rendered view does not name it:\n%s",
				sc.hit.Entity.Name, out)
		}
	}
}

// rrfFixtureDoc is one repo with a generated declaration and an authored
// entity that both match "user". It exists so the exemption can be exercised
// through the REAL ranker — BuildBM25 → Search → FuseRRF — rather than against
// hand-written score literals, because the whole defect is that hand-written
// BM25 magnitudes do not resemble what fusion produces.
func rrfFixtureDoc(repo string) *graph.Document {
	gen := graph.Entity{
		ID: repo + "_gen_User", Name: "User", Kind: "SCOPE.Class",
		SourceFile: "api/v1/user.pb.go", StartLine: 17,
	}
	gen.PropSet(types.EntityGeneratedProperty, "true")
	return &graph.Document{Repo: repo, Entities: []graph.Entity{
		gen,
		{ID: repo + "_auth_UserService", Name: "UserService", Kind: "SCOPE.Class",
			SourceFile: "internal/user/service.go", StartLine: 42},
	}}
}

// scoredFrom wraps ranker output as the handler does.
func scoredFrom(r *LoadedRepo, hits []Hit) []scored {
	out := make([]scored, 0, len(hits))
	for _, h := range hits {
		out = append(out, scored{repo: r, hit: h})
	}
	return out
}

// mirroredSemantic returns a semantic hit list in the REVERSE of the BM25
// order. That is not a contrived shape: with two-list RRF the generated and
// authored hits routinely occupy mirrored ranks, and mirrored ranks make the
// fused scores EXACTLY equal (1/(k+1) + 1/(k+2) on both sides).
func mirroredSemantic(bm25 []Hit) []Hit {
	out := make([]Hit, 0, len(bm25))
	for i := len(bm25) - 1; i >= 0; i-- {
		out = append(out, Hit{Entity: bm25[i].Entity, Score: float64(len(bm25) - i)})
	}
	return out
}

func namesOf(all []scored) []string {
	out := make([]string, len(all))
	for i, s := range all {
		out[i] = s.hit.Entity.Name
	}
	return out
}

// TestRerank_ExemptionSurvivesFuseRRF is BLOCKER B.
//
// The exemption compared hit.Score, and hit.Score is an RRF reciprocal on
// every repo that has an embeddings sidecar. That is the same trap that killed
// the score-penalty design: green without embeddings, inert with them. With
// mirrored ranks the two fused scores are EXACTLY equal, so "strictly
// outscores" can never hold and the exemption silently never fires — even
// though the generated hit is the top-ranked hit in the list.
//
// The rule is now positional: the exemption goes to the repo's top-ranked hit
// when that hit is generated. Position is what fusion preserves; magnitude is
// not.
//
// MUTATION TARGET: make the exemption score-based again (require a strictly
// higher Score) and the sidecar half of this must fail.
func TestRerank_ExemptionSurvivesFuseRRF(t *testing.T) {
	doc := rrfFixtureDoc("alpha")
	repo := &LoadedRepo{Repo: "alpha"}
	bm25 := BuildBM25(doc).Search("user", 10)
	if len(bm25) != 2 || bm25[0].Entity.Name != "User" {
		t.Fatalf("fixture precondition: BM25 order = %v, want the generated hit first", namesOf(scoredFrom(repo, bm25)))
	}

	// A — no sidecar: raw BM25 magnitudes.
	noSidecar := scoredFrom(repo, bm25)
	t.Logf("A (NO sidecar):   %s %f | %s %f",
		noSidecar[0].hit.Entity.Name, noSidecar[0].hit.Score,
		noSidecar[1].hit.Entity.Name, noSidecar[1].hit.Score)
	rerankScored(noSidecar)

	// B — with sidecar: the same two entities through FuseRRF at mirrored ranks.
	fused := FuseRRF(bm25, mirroredSemantic(bm25))
	withSidecar := scoredFrom(repo, fused)
	t.Logf("B (WITH sidecar): %s %f | %s %f",
		withSidecar[0].hit.Entity.Name, withSidecar[0].hit.Score,
		withSidecar[1].hit.Entity.Name, withSidecar[1].hit.Score)
	if withSidecar[0].hit.Score != withSidecar[1].hit.Score {
		t.Fatalf("fixture precondition: fused scores %f/%f are not the exact tie this test is about",
			withSidecar[0].hit.Score, withSidecar[1].hit.Score)
	}
	rerankScored(withSidecar)

	gotA, gotB := namesOf(noSidecar), namesOf(withSidecar)
	if gotA[0] != "User" {
		t.Fatalf("no-sidecar order = %v; the top-ranked hit is generated and must keep its place", gotA)
	}
	if gotB[0] != "User" {
		t.Fatalf("with-sidecar order = %v (no-sidecar was %v); the exemption must fire "+
			"identically regardless of sidecar presence — it cannot depend on score MAGNITUDE, "+
			"which FuseRRF replaces with rank reciprocals", gotB, gotA)
	}
}

// TestRerank_ExemptionIsDecidedPerRepo is the mixed-group half of BLOCKER B.
//
// `all` mixes score scales: the handler appends raw BM25 scores for repos
// without a sidecar and RRF reciprocals for repos with one, into ONE slice.
// A global score comparison across that slice is a comparison of incomparable
// numbers, and it decided the exemption both ways round:
//
//   - repo beta's generated hit tops beta's OWN ranking and is exactly the
//     unreachable-declaration case the exemption exists for — but the global
//     rule denied it, because an unrelated repo on a different scale had a
//     bigger number;
//   - and symmetrically, a generated hit that its own repo ranks BELOW an
//     authored hit could win the exemption on scale alone.
//
// The decision is now per repo, on rank within that repo's ranked run, where
// the scale is by construction uniform.
//
// MUTATION TARGET: compare scores globally again and this must fail.
func TestRerank_ExemptionIsDecidedPerRepo(t *testing.T) {
	alpha := &LoadedRepo{Repo: "alpha"} // no sidecar: raw BM25 magnitudes
	beta := &LoadedRepo{Repo: "beta"}   // sidecar: RRF reciprocals

	all := []scored{
		// alpha, ranked: the authored hit tops its own repo, so alpha's
		// generated hit is NOT the strongest match anywhere and stays demoted.
		atRepo(authoredEntity("alpha_auth", 10, 0.95), alpha),
		atRepo(genEntity("alpha_gen", 1, 0.92), alpha),
		// beta, ranked: the generated hit tops its own repo. Its RRF score is
		// two orders of magnitude below alpha's raw BM25 scores.
		atRepo(genEntity("beta_gen", 2, 0.032787), beta),
		atRepo(authoredEntity("beta_auth", 11, 0.032258), beta),
	}
	rerankScored(all)
	got := namesOf(all)

	// beta_gen tops beta's own ranking, so it keeps its place: no authored hit
	// may precede it inside beta's run.
	posBetaGen, posBetaAuth := rankPosOf(got, "beta_gen"), rankPosOf(got, "beta_auth")
	if posBetaGen > posBetaAuth {
		t.Fatalf("order = %v; beta_gen is the top-ranked hit in its own repo and must "+
			"keep the exemption — an unrelated repo's score scale cannot decide it", got)
	}
	// alpha_gen ranks below an authored hit in its OWN repo, so it stays demoted
	// behind every authored hit.
	posAlphaGen := rankPosOf(got, "alpha_gen")
	for _, name := range []string{"alpha_auth", "beta_auth"} {
		if rankPosOf(got, name) > posAlphaGen {
			t.Fatalf("order = %v; alpha_gen is outranked by an authored hit in its own repo "+
				"and must not be exempted past authored hit %q", got, name)
		}
	}
}

func rankPosOf(names []string, want string) int {
	for i, n := range names {
		if n == want {
			return i
		}
	}
	return -1
}

// TestRerank_ExemptionTieBreakIsRankedOrder states the tie-break rule.
//
// Under RRF exact score ties are common, not exotic. The exempt hit is the
// FIRST generated hit in its repo's ranked run — the position the ranker
// assigned. Scores are never compared, so the winner is not an accident of map
// iteration or of which hit happened to be appended first: it is the ranker's
// own order, which is deterministic (BM25 tie-breaks on ascending doc index,
// FuseRRF preserves insertion order through a stable sort).
//
// MUTATION TARGET: pick the LAST generated hit of the run instead of the first
// and this must fail.
func TestRerank_ExemptionTieBreakIsRankedOrder(t *testing.T) {
	repo := &LoadedRepo{Repo: "alpha"}
	in := []scored{
		atRepo(genEntity("gen_first", 1, 0.032787), repo),
		atRepo(genEntity("gen_second", 2, 0.032787), repo), // exact tie
		atRepo(authoredEntity("auth", 10, 0.032258), repo),
	}
	rerankScored(in)
	got := namesOf(in)
	want := []string{"gen_first", "auth", "gen_second"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v: on a score tie the exemption goes to the "+
				"earlier hit in the ranker's order, and exactly one hit is exempt", got, want)
		}
	}
}

// TestRerank_ExemptionFollowsTheRankerNotTheScore — the mirror image of the
// tie-break test, and the replacement for the deleted
// TestRerank_TiesGoToTheAuthoredHit, whose claim ("the exemption requires a
// STRICTLY higher score") was a statement about the score comparison the rule
// no longer performs. When the ranker puts the authored hit first, the generated
// hit is not the repo's strongest match and is demoted, tie or no tie.
func TestRerank_ExemptionFollowsTheRankerNotTheScore(t *testing.T) {
	repo := &LoadedRepo{Repo: "alpha"}
	in := []scored{
		atRepo(authoredEntity("auth", 10, 0.032787), repo),
		atRepo(genEntity("gen", 1, 0.032787), repo), // exact tie, ranked second
	}
	rerankScored(in)
	if got := namesOf(in); got[0] != "auth" {
		t.Fatalf("order = %v, want the authored hit first: the generated hit is not the "+
			"top-ranked hit in its repo", got)
	}
}

// TestRerank_NoiseDoesNotBlockTheExemption — noise is skipped when reading the
// run, in BOTH directions. A shadow must not be able to HOLD the exemption
// (TestRerank_ExemptionDoesNotRescueNoise), and it must not be able to BLOCK
// one either by occupying the top slot. With include_noise=true a shadow can
// rank first, and treating it as an authored hit would silently switch the
// exemption off for the whole repo.
//
// MUTATION TARGET: let a noise hit set haveAuthored and this must fail.
func TestRerank_NoiseDoesNotBlockTheExemption(t *testing.T) {
	repo := &LoadedRepo{Repo: "alpha"}
	shadow := &graph.Entity{ID: "alpha_shadow", Name: "shadow", Kind: string(types.EntityKindClass), Subtype: "shadow"}
	in := []scored{
		{repo: repo, hit: Hit{Entity: shadow, Score: 9.9}},
		atRepo(genEntity("gen", 1, 5.0), repo),
		atRepo(authoredEntity("auth", 10, 4.0), repo),
	}
	rerankScored(in)
	if got := namesOf(in); got[0] != "gen" {
		t.Fatalf("order = %v, want the generated hit first: it is the top-ranked "+
			"NON-NOISE hit in its repo, and a shadow above it must not switch the "+
			"exemption off", got)
	}
}
