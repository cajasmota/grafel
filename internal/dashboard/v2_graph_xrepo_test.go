package dashboard

// v2_graph_xrepo_test.go — regression coverage for #1576.
//
// #1576: the served multi-repo graph showed ZERO cross-repo edges. Root cause
// was a slug divergence: the dashboard keys graph NODES by the registry config
// slug (rp.Slug, e.g. "upvate-core") while cross-repo LINK endpoints are
// "<repo>::<id>" where <repo> is doc.Repo — the value stamped at index time.
// The daemon rebuild indexed each repo with an EMPTY repoTag, so doc.Repo fell
// back to the on-disk directory basename ("upvate_core", with an underscore).
// When the wizard slugified the config slug ("upvate_core" -> "upvate-core")
// the two no longer matched, so buildV2Graph's `visible[l.Source] &&
// visible[l.Target]` guard dropped every cross-repo edge.
//
// These tests pin the serving-layer invariant: a cross-repo link whose
// endpoint slugs match the configured repo slugs is emitted as an edge; a link
// whose endpoint slugs DIVERGE from the configured slugs is the bug condition
// and produces no edge. The production fix makes the rebuild stamp doc.Repo
// with the config slug so the matching case is what actually occurs on disk.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// twoRepoGroup builds a DashGroup with two single-entity repos under the given
// config slugs, plus one cross-repo link using the supplied endpoint slugs.
func twoRepoGroup(cfgSlugA, cfgSlugB, linkSrcSlug, linkDstSlug string) *DashGroup {
	docA := &graph.Document{
		Repo:     cfgSlugA,
		Entities: []graph.Entity{{ID: "aaaa000000000000", Name: "fetchThing", Kind: "Operation", SourceFile: "client.ts"}},
	}
	docB := &graph.Document{
		Repo:     cfgSlugB,
		Entities: []graph.Entity{{ID: "bbbb000000000000", Name: "ThingRoute", Kind: "Operation", SourceFile: "routes.py"}},
	}
	return &DashGroup{
		Name: "g",
		Repos: map[string]*DashRepo{
			cfgSlugA: {Slug: cfgSlugA, Doc: docA},
			cfgSlugB: {Slug: cfgSlugB, Doc: docB},
		},
		Links: []CrossRepoLink{{
			Source: linkSrcSlug + "::aaaa000000000000",
			Target: linkDstSlug + "::bbbb000000000000",
			Kind:   "calls",
			Method: "http",
		}},
	}
}

func countCrossRepoEdges(resp v2GraphResponse) int {
	n := 0
	for _, e := range resp.Edges {
		sr := repoOfPrefixed(e.Source)
		tr := repoOfPrefixed(e.Target)
		if sr != tr {
			n++
		}
	}
	return n
}

func repoOfPrefixed(id string) string {
	for i := 0; i+1 < len(id); i++ {
		if id[i] == ':' && id[i+1] == ':' {
			return id[:i]
		}
	}
	return id
}

// TestBuildV2Graph_CrossRepoEdge_SlugsMatch is the post-fix invariant: when the
// link endpoint slugs equal the configured repo slugs, the cross-repo edge is
// served. This is the shape the fixed rebuild produces (doc.Repo == config slug).
func TestBuildV2Graph_CrossRepoEdge_SlugsMatch(t *testing.T) {
	var s Server
	grp := twoRepoGroup("upvate-core-frontend", "upvate-core", "upvate-core-frontend", "upvate-core")
	resp := s.buildV2Graph(sortedRepos(grp), grp, "", false, false)
	if got := countCrossRepoEdges(resp); got != 1 {
		t.Fatalf("cross-repo edges = %d; want 1 (slugs match, edge must be served) — #1576", got)
	}
}

// TestBuildV2Graph_CrossRepoEdge_SlugMismatchDrops reproduces the #1576 bug
// condition: link endpoints use the dir-basename slug (underscores) while the
// config slug is slugified (dashes). The mismatch drops the edge — which is
// exactly what the production fix prevents by stamping doc.Repo with the
// config slug at index time.
func TestBuildV2Graph_CrossRepoEdge_SlugMismatchDrops(t *testing.T) {
	var s Server
	grp := twoRepoGroup("upvate-core-frontend", "upvate-core", "upvate_core_frontend", "upvate_core")
	resp := s.buildV2Graph(sortedRepos(grp), grp, "", false, false)
	if got := countCrossRepoEdges(resp); got != 0 {
		t.Fatalf("cross-repo edges = %d; want 0 for the divergent-slug bug condition — guards the #1576 contract", got)
	}
}
