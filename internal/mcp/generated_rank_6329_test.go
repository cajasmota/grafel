package mcp

import (
	"sort"
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

// rerank mirrors the production sort in tools.go so the golden below exercises
// the real comparator rather than a paraphrase of it.
func rerank(all []scored) []scored {
	out := append([]scored(nil), all...)
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := rankTier(out[i].hit.Entity), rankTier(out[j].hit.Entity)
		if ti != tj {
			return ti < tj
		}
		return out[i].hit.Score > out[j].hit.Score
	})
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

// TestRerank_AuthoredOutranksGeneratedRegardlessOfScore is the ordering golden.
// It is deliberately built so BM25 alone would put the generated hit FIRST:
// user.pb.go's file stem contributes at weight 1.5 to a "user" query, which is
// exactly @manuel1358000's #6314 complaint.
//
// MUTATION TARGET: set the generated tier equal to tier 0 and this must fail.
func TestRerank_AuthoredOutranksGeneratedRegardlessOfScore(t *testing.T) {
	in := []scored{
		genEntity("User", 17, 9.9),           // wins on BM25
		authoredEntity("UserService", 42, 1), // loses on BM25
	}
	got := rerank(in)
	if got[0].hit.Entity.Name != "UserService" {
		t.Fatalf("first hit = %q (score %.1f), want the authored entity despite its lower score",
			got[0].hit.Entity.Name, got[0].hit.Score)
	}
	if got[1].hit.Entity.Name != "User" {
		t.Fatalf("second hit = %q, want the generated entity", got[1].hit.Entity.Name)
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
	// And the partition itself: every authored hit precedes every generated one.
	seenGen := false
	for _, s := range got {
		isGen := s.hit.Entity.PropGet(types.EntityGeneratedProperty) == "true"
		if isGen {
			seenGen = true
		} else if seenGen {
			t.Fatalf("authored entity %q sorted after a generated one; not a clean partition", s.hit.Entity.Name)
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
