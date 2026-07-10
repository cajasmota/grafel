package cli

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/install/detect"
)

// TestMonorepoRepoForChosen_SingleRepoWithModules exercises the mapping
// resolveMonorepoAction uses once the huh multiselect returns the user's
// chosen packages (monorepoRepoForChosen). Regression coverage for D2/D3:
// the wizard must register ONE registry.Repo rooted at the monorepo path
// with the chosen packages recorded as Modules — never one flattened repo
// per package (which produced slugs like "<root>-<module>" and made the
// install hook loop stat a non-existent "<root>/<module>/.git").
func TestMonorepoRepoForChosen_SingleRepoWithModules(t *testing.T) {
	cases := []struct {
		name    string
		chosen  []string
		wantMod []string
	}{
		{
			name:    "single package",
			chosen:  []string{"packages/pkg-a"},
			wantMod: []string{"packages/pkg-a"},
		},
		{
			name:    "several packages, unordered input",
			chosen:  []string{"domains/foo", "packages/pkg-b", "packages/pkg-a"},
			wantMod: []string{"domains/foo", "packages/pkg-a", "packages/pkg-b"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			class := mustClassify(root)

			got := monorepoRepoForChosen(class, tc.chosen)

			if got.Path != class.AbsPath {
				t.Errorf("path = %q, want monorepo root %q", got.Path, class.AbsPath)
			}
			wantSlug := filepath.Base(class.AbsPath)
			if got.Slug != wantSlug {
				t.Errorf("slug = %q, want root-based slug %q (not a per-package slug)", got.Slug, wantSlug)
			}
			gotMod := append([]string(nil), got.Modules...)
			sort.Strings(gotMod)
			if !reflect.DeepEqual(gotMod, tc.wantMod) {
				t.Errorf("modules = %v, want %v", gotMod, tc.wantMod)
			}
			if got.Stack == nil || len(got.Stack) != 1 || got.Stack[0] != detect.Stack(class.AbsPath) {
				t.Errorf("stack = %v, want [%v]", got.Stack, detect.Stack(class.AbsPath))
			}
		})
	}
}

// TestGroupCandidatesAndSingleRepo_UnchangedByMonorepoFix guards against
// regressions in the ActionGroup / non-monorepo mapping paths, which must
// keep their current one-repo-per-path behavior — only the monorepo mapping
// changed.
func TestGroupCandidatesAndSingleRepo_UnchangedByMonorepoFix(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()

	repos := reposFromPaths([]string{a, b})
	if len(repos) != 2 {
		t.Fatalf("got %d repos, want 2 (group/single paths must stay one-repo-per-path)", len(repos))
	}
	if repos[0].Path != a || repos[1].Path != b {
		t.Errorf("paths = [%s %s], want [%s %s]", repos[0].Path, repos[1].Path, a, b)
	}
	if repos[0].Slug != filepath.Base(a) || repos[1].Slug != filepath.Base(b) {
		t.Errorf("slugs = [%s %s], want [%s %s]", repos[0].Slug, repos[1].Slug, filepath.Base(a), filepath.Base(b))
	}
	for _, r := range repos {
		if len(r.Modules) != 0 {
			t.Errorf("repo %s has Modules = %v, want none (not a monorepo mapping)", r.Slug, r.Modules)
		}
	}
}
