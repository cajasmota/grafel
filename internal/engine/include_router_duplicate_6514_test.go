package engine_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

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
// RelationshipRecord — two `Service:app -ROUTES_TO-> Route:router` rows
// identical in every field (RelationshipRecord carries no source-file or line,
// and `framework` is the language key "python", not the rule file). Downstream
// dedup collapsed them, which is why the `python-fastapi-mini` golden fixture
// graded 4/4 with fastapi.yaml's copy disabled.
//
// The duplicate is deleted; fastapi.yaml keeps the rule, because
// `include_router` is FastAPI/APIRouter API.
//
// WHAT THE GUARD HAS TO SURVIVE, and why it is built the way it is:
//
//   - A duplicate does NOT have to be spelled identically to be a duplicate.
//     A rule matching only `include_router(x, prefix=...)` — the canonical
//     Strawberry mount, and the exact line the deleted rule was written for —
//     double-emits on that line while an un-prefixed probe sees nothing. So
//     the behavioural test drives BOTH spellings, and the file-level test
//     classifies rules by whether their regex COVERS a mount line, never by
//     comparing pattern strings.
//   - "The rule is loaded from a rule set named FastAPI" does not locate the
//     rule: a new `fastapi_extras.yaml` declaring `frameworks.name: FastAPI
//     Extras` satisfies a prefix match while the rule has left fastapi.yaml.
//     So the second test asserts the rule FILE, read off disk.
// ---------------------------------------------------------------------------

// the two spellings of a router mount that the surviving rule must cover:
// the plain FastAPI one, and the prefixed Strawberry-on-FastAPI one the
// deleted strawberry copy was written for.
var includeRouterMounts = []struct {
	Name   string
	Source string
	FromID string
	ToID   string
}{
	{
		Name: "unprefixed",
		// Mirrors internal/quality/golden/python-fastapi-mini/src/main.py.
		Source: "from fastapi import FastAPI\n\n" +
			"from app.routers.things import router\n\n" +
			"app = FastAPI(title=\"things-api\")\n\n" +
			"app.include_router(router)\n",
		FromID: "Service:app",
		ToID:   "Route:router",
	},
	{
		Name: "prefixed",
		// The canonical Strawberry mount from the deleted rule's own comment.
		Source: "import strawberry\n" +
			"from fastapi import FastAPI\n" +
			"from strawberry.fastapi import GraphQLRouter\n\n" +
			"schema = strawberry.Schema(query=Query)\n" +
			"graphql_app = GraphQLRouter(schema)\n\n" +
			"app = FastAPI()\n\n" +
			"app.include_router(graphql_app, prefix=\"/graphql\")\n",
		FromID: "Service:app",
		ToID:   "Route:graphql_app",
	},
}

// TestIncludeRouter6514_EmitsExactlyOneRoutesToEdge is the behavioural pin: it
// runs the real embedded rules over a router mount and counts the raw records
// Detect returns, BEFORE any downstream dedup. Pre-dedup is the only place the
// double emission was ever visible.
//
// Exactly-one fails from both sides: 0 when the surviving rule stops firing,
// 2 when any python rule file re-acquires a rule covering the same mount —
// including one spelled differently but covering the same line.
func TestIncludeRouter6514_EmitsExactlyOneRoutesToEdge(t *testing.T) {
	rules, err := engine.LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	d := engine.New(rules)

	for _, mount := range includeRouterMounts {
		t.Run(mount.Name, func(t *testing.T) {
			res, err := d.Detect(context.Background(), extractor.FileInput{
				Path:     "main.py",
				Language: "python",
				Content:  []byte(mount.Source),
			})
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}

			var got []string
			for _, rel := range res.Relationships {
				if rel.Kind == "ROUTES_TO" && rel.FromID == mount.FromID && rel.ToID == mount.ToID {
					got = append(got, fmt.Sprintf("%s -%s-> %s %v", rel.FromID, rel.Kind, rel.ToID, rel.Properties))
				}
			}

			switch len(got) {
			case 1:
				// The one edge the mount is supposed to produce.
			case 0:
				t.Errorf("no `%s -ROUTES_TO-> %s` record emitted for the %s mount.\n"+
					"The include_router relationship rule in "+
					"internal/engine/rules/python/frameworks/fastapi.yaml no longer fires on "+
					"this spelling. Every relationship Detect returned:\n  %s",
					mount.FromID, mount.ToID, mount.Name,
					strings.Join(allRelStrings(res.Relationships), "\n  "))
			default:
				t.Errorf("%d identical `%s -ROUTES_TO-> %s` records emitted for a single %s "+
					"mount; expected exactly 1.\nA second python rule file carries a rule "+
					"covering this line (#6514) — it need not be spelled like fastapi.yaml's "+
					"to be a duplicate. Downstream dedup hides this, which is what made each "+
					"copy individually undeletable-yet-untested. Records:\n  %s",
					len(got), mount.FromID, mount.ToID, mount.Name, strings.Join(got, "\n  "))
			}
		})
	}
}

// wantIncludeRouterRuleFile is where the surviving rule must live, relative to
// the rules root. `include_router` is FastAPI/APIRouter API.
const wantIncludeRouterRuleFile = "python/frameworks/fastapi.yaml"

// TestIncludeRouter6514_RuleLivesOnlyInFastAPIYAML pins WHICH FILE carries the
// rule. The behavioural test above counts edges and cannot say where one came
// from: every python rule set stamps `framework: python`, so the copies were
// indistinguishable in their output.
//
// This reads the rule tree off disk rather than the loaded rule sets, because
// the loaded form has no file identity — asserting on `frameworks.name` instead
// would be satisfied by any new file that declares a FastAPI-ish name.
//
// Membership is decided by BEHAVIOUR, not by pattern text: a rule counts as
// carrying the mount if its regex matches a mount line AND its capture groups
// name the same two endpoints. A re-added duplicate spelled differently is
// therefore caught for free.
func TestIncludeRouter6514_RuleLivesOnlyInFastAPIYAML(t *testing.T) {
	const rulesRoot = "rules"

	var carriers []string
	err := filepath.Walk(rulesRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || (filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var fr engine.FrameworkRule
		if yaml.Unmarshal(body, &fr) != nil {
			// Not a rule-shaped file; the loader is tolerant here too.
			return nil
		}
		for _, rr := range fr.RelationshipRules {
			if rr.Relationship != "ROUTES_TO" {
				continue
			}
			re, compErr := regexp.Compile(rr.Pattern)
			if compErr != nil {
				continue
			}
			for _, mount := range includeRouterMounts {
				m := re.FindStringSubmatch(mount.Source)
				if m == nil ||
					rr.SourceGroup >= len(m) || rr.TargetGroup >= len(m) ||
					rr.SourceType+":"+m[rr.SourceGroup] != mount.FromID ||
					rr.TargetType+":"+m[rr.TargetGroup] != mount.ToID {
					continue
				}
				rel, _ := filepath.Rel(rulesRoot, path)
				carriers = append(carriers, fmt.Sprintf("%s covers the %s mount with %q",
					filepath.ToSlash(rel), mount.Name, rr.Pattern))
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", rulesRoot, err)
	}
	sort.Strings(carriers)

	if len(carriers) != 1 {
		t.Fatalf("%d rule file(s) carry a ROUTES_TO rule covering an `include_router` mount; "+
			"expected exactly 1, %s.\n#6514: a second carrier double-emits the edge and makes "+
			"BOTH copies untestable — deleting either one leaves every gate green. It does not "+
			"have to be spelled like fastapi.yaml's rule to be a duplicate.\nCarriers:\n  %s",
			len(carriers), wantIncludeRouterRuleFile, strings.Join(carriers, "\n  "))
	}
	if !strings.HasPrefix(carriers[0], wantIncludeRouterRuleFile+" ") {
		t.Errorf("the surviving include_router rule lives in %q; it must live in %q.\n"+
			"`include_router` is FastAPI/APIRouter API, so the rule belongs in FastAPI's own "+
			"rule file — not in a framework file that merely mounts onto FastAPI, and not in a "+
			"sibling file that only names itself after FastAPI.",
			carriers[0], wantIncludeRouterRuleFile)
	}
}

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
