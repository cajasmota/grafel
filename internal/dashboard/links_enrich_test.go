package dashboard

// links_enrich_test.go — coverage for #4596.
//
// The /links payload (CrossRepoLink) historically carried only the source/
// target ids plus kind/confidence/channel/method — no readable name or source
// location — so the /links page could not show source names or open a real
// source-peek. enrichLinkEndpoints resolves each endpoint to its entity (via
// the same findEntity lookup the rest of the dashboard uses) and copies the
// name / qualified name / file / start line onto the link.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// TestEnrichLinkEndpoints_PopulatesNameAndFile asserts that a link whose source
// and target both resolve to real entities has its name/qualified-name/file/
// line enrichment fields populated from those entities.
func TestEnrichLinkEndpoints_PopulatesNameAndFile(t *testing.T) {
	grp := &DashGroup{
		Name: "g",
		Repos: map[string]*DashRepo{
			"frontend": {Slug: "frontend", Doc: &graph.Document{
				Repo: "frontend",
				Entities: []graph.Entity{{
					ID: "aaaa000000000000", Name: "fetchThing",
					QualifiedName: "api.fetchThing", Kind: "Operation",
					SourceFile: "src/api/client.ts", StartLine: 42,
				}},
			}},
			"backend": {Slug: "backend", Doc: &graph.Document{
				Repo: "backend",
				Entities: []graph.Entity{{
					ID: "bbbb000000000000", Name: "ThingRoute",
					QualifiedName: "routes.ThingRoute", Kind: "Operation",
					SourceFile: "app/routes.py", StartLine: 7,
				}},
			}},
		},
		Links: []CrossRepoLink{{
			Source: "frontend::aaaa000000000000",
			Target: "backend::bbbb000000000000",
			Kind:   "calls",
			Method: "http",
		}},
	}

	out := enrichLinkEndpoints(grp, grp.Links)
	if len(out) != 1 {
		t.Fatalf("links len = %d; want 1", len(out))
	}
	l := out[0]

	if l.SourceName != "fetchThing" {
		t.Errorf("SourceName = %q; want %q", l.SourceName, "fetchThing")
	}
	if l.SourceQualifiedName != "api.fetchThing" {
		t.Errorf("SourceQualifiedName = %q; want %q", l.SourceQualifiedName, "api.fetchThing")
	}
	if l.SourceFile != "src/api/client.ts" {
		t.Errorf("SourceFile = %q; want %q", l.SourceFile, "src/api/client.ts")
	}
	if l.SourceLine != 42 {
		t.Errorf("SourceLine = %d; want 42", l.SourceLine)
	}

	if l.TargetName != "ThingRoute" {
		t.Errorf("TargetName = %q; want %q", l.TargetName, "ThingRoute")
	}
	if l.TargetQualifiedName != "routes.ThingRoute" {
		t.Errorf("TargetQualifiedName = %q; want %q", l.TargetQualifiedName, "routes.ThingRoute")
	}
	if l.TargetFile != "app/routes.py" {
		t.Errorf("TargetFile = %q; want %q", l.TargetFile, "app/routes.py")
	}
	if l.TargetLine != 7 {
		t.Errorf("TargetLine = %d; want 7", l.TargetLine)
	}

	// The original kind/method must be preserved.
	if l.Kind != "calls" || l.Method != "http" {
		t.Errorf("kind/method mutated: kind=%q method=%q", l.Kind, l.Method)
	}
}

// TestEnrichLinkEndpoints_UnresolvedSourceLeftEmpty pins the #4554/#4558
// finding: a link whose source id maps to a synthetic node with no
// source-derived name/file (e.g. a phantom scope.operation node or a
// bare-external residue), or to no entity at all, leaves the enrichment fields
// empty so the frontend (#4594) falls back to the graph deep-link rather than
// rendering a blank source-peek. The resolvable target is still enriched.
func TestEnrichLinkEndpoints_UnresolvedSourceLeftEmpty(t *testing.T) {
	grp := &DashGroup{
		Name: "g",
		Repos: map[string]*DashRepo{
			"backend": {Slug: "backend", Doc: &graph.Document{
				Repo: "backend",
				Entities: []graph.Entity{
					// Synthetic source node: present in the graph but with no
					// source-derived name or file (the unnamed-source case).
					{ID: "00f991b585f18f21", Name: "", Kind: "SCOPE.Operation", SourceFile: ""},
					// A real, resolvable target.
					{ID: "bbbb000000000000", Name: "ThingRoute", SourceFile: "app/routes.py", StartLine: 7},
				},
			}},
		},
		Links: []CrossRepoLink{{
			Source: "backend::00f991b585f18f21",
			Target: "backend::bbbb000000000000",
			Kind:   "calls",
		}},
	}

	out := enrichLinkEndpoints(grp, grp.Links)
	l := out[0]

	if l.SourceName != "" || l.SourceFile != "" || l.SourceLine != 0 {
		t.Errorf("unnamed synthetic source should leave enrichment empty; got name=%q file=%q line=%d",
			l.SourceName, l.SourceFile, l.SourceLine)
	}
	if l.TargetName != "ThingRoute" || l.TargetFile != "app/routes.py" {
		t.Errorf("resolvable target not enriched: name=%q file=%q", l.TargetName, l.TargetFile)
	}
}
