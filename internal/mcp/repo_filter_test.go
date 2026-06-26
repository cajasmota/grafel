package mcp

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// newRepoFilterGroup builds a LoadedGroup whose repos have the given
// (key → abs path) mapping, each with a non-nil Doc so it counts as "indexed".
func newRepoFilterGroup(name string, repos map[string]string) *LoadedGroup {
	lg := &LoadedGroup{Name: name, Repos: map[string]*LoadedRepo{}}
	for key, path := range repos {
		lg.Repos[key] = &LoadedRepo{Repo: key, Path: path, Doc: &graph.Document{}}
	}
	return lg
}

func TestResolveRepoFilter_LenientMatch(t *testing.T) {
	t.Parallel()
	lg := newRepoFilterGroup("g", map[string]string{
		"api":     "/work/proj/api",
		"web":     "/work/proj/web",
		"billing": "/work/proj/billing",
	})

	cases := []struct {
		name  string
		entry string
		want  string // expected resolved repo key
	}{
		{"exact key", "api", "api"},
		{"owner-prefixed", "acme/api", "api"},
		{"deep owner prefix", "github.com/acme/web", "web"},
		{"uppercase", "API", "api"},
		{"mixed case", "Billing", "billing"},
		{"absolute path", "/work/proj/api", "api"},
		{"path suffix / basename", "/somewhere/else/web", "web"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repos, miss := resolveRepoFilter(lg, []string{tc.entry})
			if miss != nil {
				t.Fatalf("entry %q: unexpected miss: %+v", tc.entry, miss)
			}
			if len(repos) != 1 || repos[0].Repo != tc.want {
				t.Fatalf("entry %q: want [%s], got %v", tc.entry, tc.want, repoKeys(repos))
			}
		})
	}
}

func TestResolveRepoFilter_ExactPathWins(t *testing.T) {
	t.Parallel()
	// Two repos whose basenames collide; an exact path must NOT be ambiguous.
	lg := newRepoFilterGroup("g", map[string]string{
		"frontend": "/a/app",
		"backend":  "/b/app",
	})
	repos, miss := resolveRepoFilter(lg, []string{"/a/app"})
	if miss != nil {
		t.Fatalf("exact path should resolve, got miss: %+v", miss)
	}
	if len(repos) != 1 || repos[0].Repo != "frontend" {
		t.Fatalf("want [frontend], got %v", repoKeys(repos))
	}
}

func TestResolveRepoFilter_Wildcard(t *testing.T) {
	t.Parallel()
	lg := newRepoFilterGroup("g", map[string]string{"api": "/p/api", "web": "/p/web"})
	for _, f := range [][]string{nil, {}, {"*"}} {
		repos, miss := resolveRepoFilter(lg, f)
		if miss != nil || len(repos) != 2 {
			t.Fatalf("filter %v: want all 2 repos, got %v miss=%+v", f, repoKeys(repos), miss)
		}
	}
}

func TestResolveRepoFilter_Ambiguous(t *testing.T) {
	t.Parallel()
	// Same basename in two repos → bare basename entry is ambiguous.
	lg := newRepoFilterGroup("g", map[string]string{
		"svc-a": "/a/app",
		"svc-b": "/b/app",
	})
	repos, miss := resolveRepoFilter(lg, []string{"app"})
	if repos != nil {
		t.Fatalf("ambiguous match must not resolve, got %v", repoKeys(repos))
	}
	if miss == nil || len(miss.ambiguous) != 2 {
		t.Fatalf("want ambiguous miss with 2 candidates, got %+v", miss)
	}
	got := repoFilterError(lg, miss)
	if !strings.Contains(got, "ambiguously matched") ||
		!strings.Contains(got, "svc-a") || !strings.Contains(got, "svc-b") {
		t.Fatalf("ambiguous error must list candidates, got: %q", got)
	}
}

func TestRepoFilterError_FilterExcludedAll(t *testing.T) {
	t.Parallel()
	lg := newRepoFilterGroup("g", map[string]string{
		"api": "/p/api",
		"web": "/p/web",
	})
	repos, miss := resolveRepoFilter(lg, []string{"nonexistent-xyz"})
	if len(repos) != 0 || miss == nil {
		t.Fatalf("want empty result + miss, got %v miss=%+v", repoKeys(repos), miss)
	}
	got := repoFilterError(lg, miss)
	if !strings.Contains(got, "matched no repos") {
		t.Fatalf("want filter-excluded message, got: %q", got)
	}
	if !strings.Contains(got, "Available repos: api, web") {
		t.Fatalf("error must list available repos, got: %q", got)
	}
}

func TestRepoFilterError_SuggestsClosest(t *testing.T) {
	t.Parallel()
	lg := newRepoFilterGroup("g", map[string]string{
		"billing": "/p/billing",
		"api":     "/p/api",
	})
	_, miss := resolveRepoFilter(lg, []string{"billng"}) // typo of billing (1 edit)
	if miss == nil {
		t.Fatal("want a miss for a typo'd entry")
	}
	got := repoFilterError(lg, miss)
	if !strings.Contains(got, `Did you mean "billing"?`) {
		t.Fatalf("want closest-match suggestion, got: %q", got)
	}
}

func TestRepoFilterError_EmptyGroupVsFilterExcluded(t *testing.T) {
	t.Parallel()
	// Empty group: no indexed repos at all.
	empty := newRepoFilterGroup("g", nil)
	_, miss := resolveRepoFilter(empty, []string{"api"})
	emptyMsg := repoFilterError(empty, miss)
	if !strings.Contains(emptyMsg, "no indexed repos") ||
		!strings.Contains(emptyMsg, "grafel index") {
		t.Fatalf("empty-group message must explain how to index, got: %q", emptyMsg)
	}

	// Non-empty group, filter excludes all: a DISTINCT message.
	full := newRepoFilterGroup("g", map[string]string{"api": "/p/api"})
	_, miss2 := resolveRepoFilter(full, []string{"nope"})
	fullMsg := repoFilterError(full, miss2)
	if emptyMsg == fullMsg {
		t.Fatalf("empty-group and filter-excluded messages must differ:\n  empty=%q\n  full=%q", emptyMsg, fullMsg)
	}
	if strings.Contains(fullMsg, "no indexed repos") {
		t.Fatalf("filter-excluded message must not claim the group is empty, got: %q", fullMsg)
	}
}

func TestReposToConsider_BackCompatExactStillWorks(t *testing.T) {
	t.Parallel()
	lg := newRepoFilterGroup("g", map[string]string{"api": "/p/api", "web": "/p/web"})
	// Exact filter resolves to exactly the named repo — unchanged semantics.
	repos := reposToConsider(lg, []string{"api"})
	if len(repos) != 1 || repos[0].Repo != "api" {
		t.Fatalf("exact filter must resolve to [api], got %v", repoKeys(repos))
	}
}
