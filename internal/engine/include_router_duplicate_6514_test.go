package engine_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// Issue #6514 — the FastAPI `include_router` rule had a byte-identical twin in
// strawberry_graphql.yaml, and neither copy could be tested.
//
// Detect resolves compiled rule sets by file.Language alone: EVERY python rule
// set runs on EVERY python file, and relationship_rules carry no
// requires_framework gate and no per-file dedup. So both copies matched the
// same `app.include_router(router)` text and each appended its own
// RelationshipRecord — two identical `Service:app -ROUTES_TO-> Route:router`
// rows, with identical properties (`framework` is the language key "python",
// not the rule file). Downstream dedup collapsed them, which is exactly why
// the `python-fastapi-mini` golden fixture graded 4/4 with fastapi.yaml's copy
// disabled: the strawberry twin was silently standing in for it.
//
// The duplicate is deleted; fastapi.yaml keeps the rule, because
// `include_router` is FastAPI/APIRouter API. These tests pin that outcome from
// both sides:
//
//   - EXACTLY ONE record, so deleting the SURVIVING rule fails (the permissive
//     mutant that was green before this test existed), AND re-adding a twin in
//     any other python rule file fails too. A `>= 1` assertion would have been
//     satisfied by the duplication itself and measured nothing.
//   - The rule is loaded from the FastAPI rule set, so "fixing" the duplication
//     by deleting fastapi.yaml's copy instead and living off strawberry's does
//     not pass.
// ---------------------------------------------------------------------------

const includeRouterPattern = `(\w+)\.include_router\s*\(\s*(\w+)`

// TestIncludeRouter6514_EmitsExactlyOneRoutesToEdge is the behavioural pin: it
// runs the real embedded rules over a minimal FastAPI mount and counts the raw
// records Detect returns, BEFORE any downstream dedup. Pre-dedup is the only
// place the double emission was ever visible.
func TestIncludeRouter6514_EmitsExactlyOneRoutesToEdge(t *testing.T) {
	rules, err := engine.LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	d := engine.New(rules)

	// Mirrors internal/quality/golden/python-fastapi-mini/src/main.py.
	const src = `from fastapi import FastAPI

from app.routers.things import router

app = FastAPI(title="things-api")

app.include_router(router)
`

	res, err := d.Detect(context.Background(), extractor.FileInput{
		Path:     "main.py",
		Language: "python",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	var got []string
	for _, rel := range res.Relationships {
		if rel.Kind == "ROUTES_TO" && rel.FromID == "Service:app" && rel.ToID == "Route:router" {
			got = append(got, fmt.Sprintf("%s -%s-> %s %v", rel.FromID, rel.Kind, rel.ToID, rel.Properties))
		}
	}

	switch len(got) {
	case 1:
		// The one edge the mount is supposed to produce.
	case 0:
		t.Errorf("no `Service:app -ROUTES_TO-> Route:router` record emitted for "+
			"`app.include_router(router)`.\nThe include_router relationship rule in "+
			"internal/engine/rules/python/frameworks/fastapi.yaml no longer fires. "+
			"Every relationship Detect returned:\n  %s",
			strings.Join(allRelStrings(res.Relationships), "\n  "))
	default:
		t.Errorf("%d identical `Service:app -ROUTES_TO-> Route:router` records emitted for a "+
			"single `app.include_router(router)`; expected exactly 1.\nA second python rule "+
			"file has re-acquired a duplicate include_router rule (#6514). Downstream dedup "+
			"hides this, which is what made each copy individually undeletable-yet-untested. "+
			"Records:\n  %s",
			len(got), strings.Join(got, "\n  "))
	}
}

// TestIncludeRouter6514_RuleIsLoadedOnlyFromFastAPI pins WHERE the surviving
// rule lives. The behavioural test above counts edges and cannot tell which
// rule file produced one — `framework` on the record is the language key
// "python" for every python rule set, so the two copies were literally
// indistinguishable in their output.
func TestIncludeRouter6514_RuleIsLoadedOnlyFromFastAPI(t *testing.T) {
	rules, err := engine.LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	if len(rules["python"]) == 0 {
		t.Fatalf("no python rule sets loaded; the embedded rule FS is not reaching this test")
	}

	var carriers []string
	for lang, frameworkRules := range rules {
		for _, fr := range frameworkRules {
			for _, rr := range fr.RelationshipRules {
				if rr.Pattern != includeRouterPattern {
					continue
				}
				carriers = append(carriers, fmt.Sprintf("%s/%s (%s:%s -%s-> %s:%s)",
					lang, fr.Frameworks.Name,
					rr.SourceType, groupRef(rr.SourceGroup),
					rr.Relationship,
					rr.TargetType, groupRef(rr.TargetGroup)))
			}
		}
	}
	sort.Strings(carriers)

	if len(carriers) != 1 {
		t.Fatalf("%d rule set(s) declare the include_router pattern %q; expected exactly 1 "+
			"(FastAPI).\n#6514: a second, byte-identical copy makes both copies untestable — "+
			"deleting either one leaves every gate green.\nCarriers:\n  %s",
			len(carriers), includeRouterPattern, strings.Join(carriers, "\n  "))
	}
	if want := "python/FastAPI"; !strings.HasPrefix(carriers[0], want+" ") {
		t.Errorf("the surviving include_router rule is loaded from %q, want a rule set named "+
			"%q. `include_router` is FastAPI/APIRouter API; it belongs in fastapi.yaml, not in "+
			"a framework file that merely mounts onto FastAPI.", carriers[0], want)
	}
}

func groupRef(g int) string { return fmt.Sprintf("g%d", g) }

func allRelStrings(rels []types.RelationshipRecord) []string {
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, fmt.Sprintf("%s -%s-> %s", r.FromID, r.Kind, r.ToID))
	}
	if len(out) == 0 {
		out = append(out, "(none)")
	}
	return out
}
