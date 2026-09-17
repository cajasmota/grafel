package dashboard

import "testing"

func TestRepositoryTopologyCacheRebuildsWhenSourceVersionChanges(t *testing.T) {
	cache := newRepositoryTopologyCache()
	grp := repositoryTopologyTestGroup("client", "rule")
	grp.sourceVersion = "v1"
	grp.Links = []CrossRepoLink{{Source: "client::a", Target: "rule::b", Channel: "dubbo", Identifier: "dubbo:A"}}
	first := cache.GetOrBuild("test", "", grp)
	if len(first.Relationships) != 1 {
		t.Fatalf("first relationships = %d", len(first.Relationships))
	}

	grp.Links = append(grp.Links, CrossRepoLink{Source: "client::c", Target: "rule::d", Method: "http", Identifier: "http:GET:/d"})
	cached := cache.GetOrBuild("test", "", grp)
	if len(cached.Relationships) != 1 {
		t.Fatalf("same version rebuilt unexpectedly: %d", len(cached.Relationships))
	}

	grp.sourceVersion = "v2"
	rebuilt := cache.GetOrBuild("test", "", grp)
	if len(rebuilt.Relationships) != 2 {
		t.Fatalf("new version relationships = %d, want 2", len(rebuilt.Relationships))
	}
}

func TestRepositoryTopologyCacheInvalidatesGroupRefs(t *testing.T) {
	cache := newRepositoryTopologyCache()
	grp := repositoryTopologyTestGroup("client", "rule")
	grp.sourceVersion = "v1"
	grp.Links = []CrossRepoLink{{Source: "client::a", Target: "rule::b", Channel: "dubbo"}}
	cache.GetOrBuild("test", "main", grp)
	cache.GetOrBuild("test", "release", grp)
	cache.InvalidateGroup("test")
	if len(cache.entries) != 0 {
		t.Fatalf("entries remain after invalidation: %#v", cache.entries)
	}
}
